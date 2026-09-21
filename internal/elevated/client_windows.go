//go:build windows

package elevated

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"

	"nahida.live/desktop/internal/platform"
)

var (
	shell32                   = syscall.NewLazyDLL("shell32.dll")
	kernel32                  = syscall.NewLazyDLL("kernel32.dll")
	procShellExecuteExW       = shell32.NewProc("ShellExecuteExW")
	procGetNamedPipeServerPID = kernel32.NewProc("GetNamedPipeServerProcessId")
	procGetNamedPipeClientPID = kernel32.NewProc("GetNamedPipeClientProcessId")
)

const (
	seeMaskNoCloseProcess = 0x00000040
	swHide                = 0
)

type shellExecuteInfoW struct {
	cbSize       uint32
	fMask        uint32
	hwnd         uintptr
	lpVerb       *uint16
	lpFile       *uint16
	lpParameters *uint16
	lpDirectory  *uint16
	nShow        int32
	hInstApp     uintptr
	lpIDList     uintptr
	lpClass      *uint16
	hkeyClass    uintptr
	dwHotKey     uint32
	hIcon        uintptr
	hProcess     windows.Handle
}

// Client owns one authenticated connection to the elevated helper.
type Client struct {
	mu         sync.Mutex
	conn       net.Conn
	process    windows.Handle
	pid        uint32
	secret     string
	wanted     bool
	generation uint64
	nextID     atomic.Uint64
	watchStop  windows.Handle
	// onDisconnect is called after an unexpected connection loss. It must not
	// take mu; the client invokes it after releasing the state lock.
	onDisconnect func()

	// startMu serializes launch attempts. It is separate from mu because it is
	// held across launch, and Close must stay reachable while ShellExecuteExW
	// waits on a UAC prompt.
	startMu sync.Mutex
}

func NewClient() *Client {
	return &Client{}
}

// UseDisconnect registers fn to run after the helper connection drops while it
// is still wanted. Passing nil clears the callback.
func (c *Client) UseDisconnect(fn func()) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.onDisconnect = fn
}

// Connected reports whether the authenticated helper pipe is live.
func (c *Client) Connected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.conn != nil
}

// Start enables the helper and connects to it. Launching may wait on a UAC
// prompt, so it runs without holding the state lock; Close can invalidate the
// attempt and the launched helper is discarded when it returns.
func (c *Client) Start(ctx context.Context) error {
	c.mu.Lock()
	c.wanted = true
	c.mu.Unlock()
	return c.start(ctx)
}

// start launches the helper and connects to it. It holds startMu, not mu, while
// the launch and pipe dial are in flight, and captures generation so a Close
// that lands mid-launch wins.
func (c *Client) start(ctx context.Context) error {
	c.startMu.Lock()
	defer c.startMu.Unlock()

	c.mu.Lock()
	if c.conn != nil {
		c.mu.Unlock()
		return nil
	}
	if !c.wanted || ctx.Err() != nil {
		c.mu.Unlock()
		return context.Canceled
	}
	generation := c.generation
	c.mu.Unlock()

	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve application executable: %w", err)
	}
	helperDir, err := defaultHelperDir()
	if err != nil {
		return err
	}
	helperData, err := bundledHelper()
	if err != nil {
		return fmt.Errorf("prepare elevated helper: %w", err)
	}
	helper, err := writeHelper(helperDir, helperData)
	if err != nil {
		return fmt.Errorf("prepare elevated helper: %w", err)
	}
	info, err := os.Lstat(helper)
	if err != nil {
		return fmt.Errorf("locate elevated helper %q: %w", helper, err)
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("elevated helper is not a regular file: %q", helper)
	}
	helperPath, err := windows.UTF16PtrFromString(helper)
	if err != nil {
		return fmt.Errorf("encode elevated helper path: %w", err)
	}
	helperFile, err := windows.CreateFile(
		helperPath,
		windows.GENERIC_READ,
		windows.FILE_SHARE_READ,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_ATTRIBUTE_NORMAL,
		0,
	)
	if err != nil {
		return fmt.Errorf("lock elevated helper executable: %w", err)
	}
	defer func() { _ = windows.CloseHandle(helperFile) }()
	if err := verifyHelperHandle(helperFile, helperData); err != nil {
		return err
	}

	secretBytes := make([]byte, 32)
	if _, err := rand.Read(secretBytes); err != nil {
		return fmt.Errorf("create elevated helper secret: %w", err)
	}
	secret := base64.RawURLEncoding.EncodeToString(secretBytes)
	pipe := `\\.\pipe\nahida-elevated-` + strconv.FormatUint(uint64(os.Getpid()), 10) + "-" + secret[:16]
	process, pid, err := launch(helper, pipe, secret, uint32(os.Getpid()), executable)
	if err != nil {
		return err
	}
	processPath, err := processImagePath(process)
	if err != nil {
		discardStartedHelper(process)
		return fmt.Errorf("read elevated helper image path: %w", err)
	}
	if !equalPath(processPath, helper) {
		discardStartedHelper(process)
		return fmt.Errorf("verify elevated helper image %q, expected %q", processPath, helper)
	}
	if c.invalidated(generation) || ctx.Err() != nil {
		discardStartedHelper(process)
		return context.Canceled
	}

	connectCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	conn, err := dialPipeUntilReady(connectCtx, pipe)
	if err != nil {
		discardStartedHelper(process)
		return fmt.Errorf("connect elevated helper: %w", err)
	}
	serverPID, err := namedPipeProcessID(conn, procGetNamedPipeServerPID)
	if err != nil {
		_ = conn.Close()
		discardStartedHelper(process)
		return fmt.Errorf("read elevated helper pipe server pid: %w", err)
	}
	if serverPID != pid {
		_ = conn.Close()
		discardStartedHelper(process)
		return fmt.Errorf("verify elevated helper process: pid %d, expected %d", serverPID, pid)
	}

	// Publish and handshake together so SendKeys never observes a half-open
	// connection. The handshake is a local pipe round trip.
	c.mu.Lock()
	if c.generation != generation || !c.wanted || ctx.Err() != nil {
		c.mu.Unlock()
		_ = conn.Close()
		discardStartedHelper(process)
		return context.Canceled
	}
	c.conn, c.process, c.pid, c.secret = conn, process, pid, secret
	if _, err := c.callLocked(connectCtx, operationHello, nil); err != nil {
		closeErr := c.closeLocked()
		c.mu.Unlock()
		if ctx.Err() != nil {
			return context.Canceled
		}
		return errors.Join(err, closeErr)
	}
	c.mu.Unlock()
	c.watch(process, generation)
	return nil
}

// invalidated reports whether a Close superseded the launch identified by
// generation while it was in flight.
func (c *Client) invalidated(generation uint64) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.generation != generation || !c.wanted
}

func dialPipeUntilReady(ctx context.Context, pipe string) (net.Conn, error) {
	for {
		conn, err := winio.DialPipeContext(ctx, pipe)
		if err == nil {
			return conn, nil
		}
		if !errors.Is(err, windows.ERROR_FILE_NOT_FOUND) {
			return nil, err
		}

		timer := time.NewTimer(25 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

func discardStartedHelper(process windows.Handle) {
	_ = windows.TerminateProcess(process, 1)
	_ = windows.CloseHandle(process)
}

func processImagePath(process windows.Handle) (string, error) {
	buffer := make([]uint16, 32768)
	size := uint32(len(buffer))
	if err := windows.QueryFullProcessImageName(process, 0, &buffer[0], &size); err != nil {
		return "", err
	}
	return windows.UTF16ToString(buffer[:size]), nil
}

func equalPath(left, right string) bool {
	leftPath, leftErr := filepath.Abs(left)
	rightPath, rightErr := filepath.Abs(right)
	return leftErr == nil && rightErr == nil && strings.EqualFold(filepath.Clean(leftPath), filepath.Clean(rightPath))
}

// verifyHelperHandle hashes the bytes read from the already-locked handle and
// compares them with want. Verification is bound to the file object the caller
// holds open without write or delete sharing, so a successful check cannot be
// invalidated by re-resolving the path before the helper is launched.
func verifyHelperHandle(handle windows.Handle, want []byte) error {
	digest := sha256.New()
	buffer := make([]byte, 64*1024)
	for {
		var read uint32
		if err := windows.ReadFile(handle, buffer, &read, nil); err != nil {
			return fmt.Errorf("read elevated helper: %w", err)
		}
		if read == 0 {
			break
		}
		if _, err := digest.Write(buffer[:read]); err != nil {
			return fmt.Errorf("hash elevated helper: %w", err)
		}
	}
	expected := helperDigest(want)
	if !bytes.Equal(digest.Sum(nil), expected[:]) {
		return errors.New("elevated helper contents changed before launch")
	}
	return nil
}

// EnsureReady reports whether the helper connection is live. It does not
// launch or restart the helper; a UAC prompt is reserved for an explicit Start.
func (c *Client) EnsureReady(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn != nil {
		return nil
	}
	return fmt.Errorf("%w: elevated helper is not running", platform.ErrElevatedHelperRequired)
}

// Ping round-trips session.ping on the live connection. A transport failure
// invalidates the connection the same way a failed SendKeys does.
func (c *Client) Ping(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn == nil {
		return fmt.Errorf("%w: elevated helper is not running", platform.ErrElevatedHelperRequired)
	}
	_, err := c.callLocked(ctx, operationPing, nil)
	return err
}

func (c *Client) SendKeys(ctx context.Context, request platform.KeyRequest) (platform.KeyResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn == nil {
		return platform.KeyResult{}, fmt.Errorf(
			"%w: elevated helper is not running",
			platform.ErrElevatedHelperRequired,
		)
	}
	payload, err := json.Marshal(request)
	if err != nil {
		return platform.KeyResult{}, err
	}
	response, err := c.callLocked(ctx, operationKeys, payload)
	if err != nil {
		return platform.KeyResult{}, err
	}
	var result platform.KeyResult
	if err := json.Unmarshal(response, &result); err != nil {
		return platform.KeyResult{}, fmt.Errorf("decode elevated input result: %w", err)
	}
	return result, nil
}

func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.wanted = false
	c.generation++
	c.signalWatchLocked()
	if c.conn == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, callErr := c.callLocked(ctx, operationStop, nil)
	closeErr := c.closeLocked()
	return errors.Join(callErr, closeErr)
}

func (c *Client) callLocked(ctx context.Context, operation string, payload json.RawMessage) (json.RawMessage, error) {
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(10 * time.Second)
	}
	if err := c.conn.SetDeadline(deadline); err != nil {
		return nil, errors.Join(fmt.Errorf("set elevated helper deadline: %w", err), c.closeLocked())
	}
	id := c.nextID.Add(1)
	request := message{Version: protocolVersion, ID: id, Operation: operation, Secret: c.secret, Payload: payload}
	if err := writeMessage(c.conn, request); err != nil {
		return nil, errors.Join(fmt.Errorf("write elevated helper request: %w", err), c.closeLocked())
	}
	response, err := readMessage(c.conn)
	if err != nil {
		return nil, errors.Join(fmt.Errorf("read elevated helper response: %w", err), c.closeLocked())
	}
	if response.Version != protocolVersion || response.ID != id {
		return nil, errors.Join(errors.New("invalid elevated helper response"), c.closeLocked())
	}
	if !response.OK {
		return nil, elevatedHelperError(response)
	}
	return response.Payload, nil
}

// elevatedHelperError rebuilds the stable classification code the server sent
// so errors.Is still resolves and the UPPER_SNAKE prefix stays first, matching
// a local input failure.
func elevatedHelperError(response message) error {
	if response.ErrorCode != "" {
		if sentinel, ok := platform.InputErrorFromCode(response.ErrorCode); ok {
			return fmt.Errorf("%w: elevated helper: %s", sentinel, response.Error)
		}
		return fmt.Errorf("%s: elevated helper: %s", response.ErrorCode, response.Error)
	}
	return fmt.Errorf("elevated helper: %s", response.Error)
}

func (c *Client) closeLocked() error {
	hadConn := c.conn != nil || c.process != 0

	// Drop the watchStop reference before the watcher goroutine closes that
	// event. SetEvent is safe while the handle is still open; a later Start
	// must not SetEvent on a recycled handle.
	c.signalWatchLocked()

	var err error
	if c.conn != nil {
		err = c.conn.Close()
	}
	if c.process != 0 {
		err = errors.Join(err, windows.CloseHandle(c.process))
	}
	c.conn, c.process, c.pid, c.secret = nil, 0, 0, ""
	if hadConn {
		// Transport errors and process death reuse this path without Close, so
		// a replacement Start would otherwise keep the same generation and a
		// delayed watcher could close the new connection.
		c.generation++
		if c.wanted {
			c.scheduleDisconnectLocked()
		}
	}
	return err
}

func (c *Client) scheduleDisconnectLocked() {
	fn := c.onDisconnect
	if fn == nil {
		return
	}
	go fn()
}

func launch(helper, pipe, secret string, parentPID uint32, parentExe string) (windows.Handle, uint32, error) {
	verb, _ := syscall.UTF16PtrFromString("runas")
	file, _ := syscall.UTF16PtrFromString(helper)
	parameters, _ := syscall.UTF16PtrFromString(
		"--pipe " + pipe + " --secret " + secret +
			" --parent-pid " + strconv.FormatUint(uint64(parentPID), 10) +
			" --parent-exe " + syscall.EscapeArg(parentExe),
	)
	directory, _ := syscall.UTF16PtrFromString(filepath.Dir(helper))
	info := shellExecuteInfoW{
		fMask:        seeMaskNoCloseProcess,
		lpVerb:       verb,
		lpFile:       file,
		lpParameters: parameters,
		lpDirectory:  directory,
		nShow:        swHide,
	}
	info.cbSize = uint32(unsafe.Sizeof(info))
	result, _, callErr := procShellExecuteExW.Call(uintptr(unsafe.Pointer(&info)))
	if result == 0 {
		return 0, 0, fmt.Errorf("start elevated helper: %w", callErr)
	}
	pid, err := windows.GetProcessId(info.hProcess)
	if err != nil {
		_ = windows.CloseHandle(info.hProcess)
		return 0, 0, fmt.Errorf("read elevated helper pid: %w", err)
	}
	return info.hProcess, pid, nil
}

func namedPipeProcessID(conn net.Conn, proc *syscall.LazyProc) (uint32, error) {
	fdConn, ok := conn.(interface{ Fd() uintptr })
	if !ok {
		return 0, errors.New("named pipe does not expose a Windows handle")
	}
	var pid uint32
	result, _, callErr := proc.Call(fdConn.Fd(), uintptr(unsafe.Pointer(&pid)))
	if result == 0 {
		return 0, callErr
	}
	return pid, nil
}

func secretsEqual(left, right string) bool {
	return len(left) == len(right) && subtle.ConstantTimeCompare([]byte(left), []byte(right)) == 1
}
