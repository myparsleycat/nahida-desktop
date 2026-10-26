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
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"

	"nahida.live/desktop/internal/appdata"
	"nahida.live/desktop/internal/infra"
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

// helperSession is the lifetime Close cancels to unblock an in-flight pipe read.
// Its fields stay immutable after publish.
type helperSession struct {
	ctx    context.Context
	cancel context.CancelFunc
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

	// session is cancelled by Close without taking mu. Pipe reads watch it and
	// expire the connection, so a manual game launch or a stalled helper
	// response cannot hold shutdown or status queries until the TimeoutSeconds
	// budget runs out. The pipe is half-duplex, so mu still covers a whole
	// exchange; cancellation must not wait for that lock.
	session atomic.Pointer[helperSession]

	// appData locates the per-user folder the embedded helper is extracted into, shared by every
	// distribution channel including portable runs.
	appData *appdata.Store

	// startMu serializes launch attempts. It is separate from mu because it is
	// held across launch, and Close must stay reachable while ShellExecuteExW
	// waits on a UAC prompt.
	startMu sync.Mutex
}

func NewClient() *Client {
	client := &Client{}
	client.replaceSession()
	return client
}

func (c *Client) replaceSession() *helperSession {
	ctx, cancel := context.WithCancel(context.Background())
	session := &helperSession{ctx: ctx, cancel: cancel}
	c.session.Store(session)
	return session
}

// UseDisconnect registers fn to run after the helper connection drops while it
// is still wanted. Passing nil clears the callback.
func (c *Client) UseDisconnect(fn func()) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.onDisconnect = fn
}

// UseAppData sets the app data root the helper is extracted under. It has to be set before the first
// Start, which includes restoring the permissions of that root.
func (c *Client) UseAppData(store *appdata.Store) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.appData = store
}

// Connected reports whether the authenticated helper pipe is live.
func (c *Client) Connected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.conn != nil
}

// ErrBlockedByAntivirus marks a start that failed because antivirus software refused the helper
// executable. Its text is a renderer contract; keep it in sync with use-launch-guard.tsx.
var ErrBlockedByAntivirus = errors.New("ELEVATED_HELPER_BLOCKED_BY_ANTIVIRUS")

// Start enables the helper and connects to it. Launching may wait on a UAC
// prompt, so it runs without holding the state lock; Close can invalidate the
// attempt and the launched helper is discarded when it returns.
func (c *Client) Start(ctx context.Context) error {
	c.mu.Lock()
	c.wanted = true
	c.mu.Unlock()
	return classifyStartError(c.start(ctx))
}

// classifyStartError marks the errors Windows returns when antivirus software blocks or removes the
// helper while it is written, opened, or launched; retrying cannot get past them.
func classifyStartError(err error) error {
	if errors.Is(err, windows.ERROR_VIRUS_INFECTED) || errors.Is(err, windows.ERROR_VIRUS_DELETED) {
		return fmt.Errorf("%w: %w", ErrBlockedByAntivirus, err)
	}
	return err
}

// start launches the helper and connects to it. It holds startMu, not mu, while
// the launch and pipe dial are in flight, and captures generation so a Close
// that lands mid-launch wins.
func (c *Client) start(ctx context.Context) (startErr error) {
	c.startMu.Lock()
	defer c.startMu.Unlock()

	c.mu.Lock()
	if c.conn != nil {
		c.mu.Unlock()
		return nil
	}
	if !c.wanted || ctx.Err() != nil {
		c.mu.Unlock()
		return startCancelled(ctx)
	}

	// A failed teardown retains the process handle. Never launch a replacement
	// until that exact process has exited, even when its pipe is already gone.
	if c.process != 0 {
		if err := c.closeLocked(); err != nil {
			pid := c.pid
			c.mu.Unlock()
			return fmt.Errorf("stop previous elevated helper pid %d: %w", pid, err)
		}
	}
	generation := c.generation
	appData := c.appData
	c.mu.Unlock()

	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return fmt.Errorf("read elevated helper owner: %w", err)
	}
	if err := checkHelperInstance(user.User.Sid.String()); err != nil {
		return err
	}

	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve application executable: %w", err)
	}
	helperDir, err := appData.Resolve(appdata.ElevatedDir)
	if err != nil {
		return fmt.Errorf("resolve elevated helper directory: %w", err)
	}
	helperData, err := bundledHelper()
	if err != nil {
		return fmt.Errorf("prepare elevated helper: %w", err)
	}
	helper, err := prepareHelper(helperDir, fallbackHelperDir(), helperData)
	if err != nil {
		return fmt.Errorf("prepare elevated helper: %w", err)
	}
	info, err := os.Lstat(helper)
	if err != nil {
		return fmt.Errorf("locate elevated helper %q: %w", helper, err)
	}
	if !info.Mode().IsRegular() {
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

	// Keep ownership of failed launches until cleanup confirms process exit.
	// startMu prevents another launch while this deferred teardown runs.
	defer func() {
		if startErr == nil || process == 0 {
			return
		}

		// This helper never got a connection, so it waits in Accept for its parent and would only
		// use up the shutdown grace. stopHelperProcess still confirms the exit.
		_ = windows.TerminateProcess(process, 1)
		c.mu.Lock()
		c.process, c.pid = process, pid
		startErr = helperStartupError(startErr, c.closeLocked())
		c.mu.Unlock()
		startErr = infra.AnnotateError(startErr, infra.Diagnostic{
			Operation: "elevated-helper", Stage: "startup-cleanup",
			Fields: map[string]any{"helperPID": pid, "helperPath": helper, "parentPID": os.Getpid()},
		})
	}()
	if err != nil {
		return err
	}
	processPath, err := processImagePath(process)
	if err != nil {
		return fmt.Errorf("read elevated helper image path: %w", err)
	}
	if !equalPath(processPath, helper) {
		return fmt.Errorf("verify elevated helper image %q, expected %q", processPath, helper)
	}
	if c.invalidated(generation) || ctx.Err() != nil {
		return startCancelled(ctx)
	}

	connectCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	conn, err := dialPipeUntilReady(connectCtx, pipe, process)
	if err != nil {
		return fmt.Errorf("connect elevated helper: %w", err)
	}
	serverPID, err := namedPipeProcessID(conn, procGetNamedPipeServerPID)
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("read elevated helper pipe server pid: %w", err)
	}
	if serverPID != pid {
		_ = conn.Close()
		return fmt.Errorf("verify elevated helper process: pid %d, expected %d", serverPID, pid)
	}

	// Publish and handshake together so SendKeys never observes a half-open
	// connection. The handshake is a local pipe round trip.
	c.mu.Lock()
	if c.generation != generation || !c.wanted || ctx.Err() != nil {
		c.mu.Unlock()
		_ = conn.Close()
		return startCancelled(ctx)
	}
	c.conn, c.process, c.pid, c.secret = conn, process, pid, secret

	// The client now owns the handle, including a failed handshake teardown.
	process = 0
	if _, err := c.callLocked(connectCtx, operationHello, nil); err != nil {
		closeErr := c.closeLocked()
		c.mu.Unlock()
		if ctx.Err() != nil {
			err = errors.Join(ctx.Err(), err)
		}
		return helperStartupError(err, closeErr)
	}
	watchProcess := c.process
	c.mu.Unlock()
	c.watch(watchProcess, generation)
	return nil
}

// startCancelled is the error of a start that Close or the caller's context superseded. A deadline stays a
// deadline, so a start that timed out is not mistaken for one the user cancelled.
func startCancelled(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return context.Canceled
}

// invalidated reports whether a Close superseded the launch identified by
// generation while it was in flight.
func (c *Client) invalidated(generation uint64) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.generation != generation || !c.wanted
}

func dialPipeUntilReady(ctx context.Context, pipe string, process windows.Handle) (net.Conn, error) {
	for {
		if process != 0 {
			event, err := windows.WaitForSingleObject(process, 0)
			if err != nil {
				return nil, fmt.Errorf("check elevated helper startup: %w", err)
			}
			if event == windows.WAIT_OBJECT_0 {
				return nil, helperExitError(process)
			}
		}
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

func helperExitError(process windows.Handle) error {
	var code uint32
	if err := windows.GetExitCodeProcess(process, &code); err != nil {
		return errors.New("elevated helper exited before connecting")
	}
	if reason := serverExitReason(code); reason != "" {
		return fmt.Errorf("elevated helper exited before connecting (exit code %d: %s)", code, reason)
	}
	return fmt.Errorf("elevated helper exited before connecting (exit code %d)", code)
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
	return platform.SamePathFold(left, right)
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

// Caller sends one operation to the helper and returns its result payload.
type Caller interface {
	Call(ctx context.Context, operation string, payload json.RawMessage) (json.RawMessage, error)
}

var _ Caller = (*Client)(nil)

// Call runs operation on the live helper connection. It holds the connection for the whole
// exchange, so calls from different features run one after another.
func (c *Client) Call(ctx context.Context, operation string, payload json.RawMessage) (json.RawMessage, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn == nil {
		return nil, fmt.Errorf("%w: elevated helper is not running", platform.ErrElevatedHelperRequired)
	}
	return c.callLocked(ctx, operation, payload)
}

func (c *Client) HelperImageName() string {
	data, err := bundledHelper()
	if err != nil {
		return ""
	}
	return helperFileName(data)
}

func (c *Client) Close() error {
	// Cancel before taking mu. Call holds mu for the whole helper read, and a
	// read such as an XXMI launch only ends when this session is cancelled or
	// the deadline fires. Waiting for the lock first would stall shutdown.
	if session := c.session.Load(); session != nil {
		session.cancel()
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	c.replaceSession()
	c.wanted = false
	c.generation++
	c.signalWatchLocked()
	if c.conn == nil {
		return c.closeLocked()
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, callErr := c.callLocked(ctx, operationStop, nil)
	closeErr := c.closeLocked()
	return errors.Join(callErr, closeErr)
}

func (c *Client) callLocked(ctx context.Context, operation string, payload json.RawMessage) (json.RawMessage, error) {
	if c.conn == nil {
		return nil, errors.New("elevated helper connection is closed")
	}
	// Parent cancellation and Close both have to unblock a read that already
	// holds mu. SetDeadline is safe beside Read, so the watcher does not take
	// the state lock. A fresh session is installed by Close before session.stop,
	// so this exchange is not aborted by the session it replaces.
	callCtx, cancelCall := context.WithCancel(ctx)
	defer cancelCall()
	session := c.session.Load()
	if session != nil {
		stopSession := context.AfterFunc(session.ctx, cancelCall)
		defer stopSession()
	}
	conn := c.conn
	stopDeadline := context.AfterFunc(callCtx, func() {
		_ = conn.SetDeadline(time.Now())
	})
	defer stopDeadline()
	if err := callCtx.Err(); err != nil {
		return nil, err
	}
	if session != nil && session.ctx.Err() != nil {
		return nil, context.Canceled
	}

	failIO := func(err error) (json.RawMessage, error) {
		closeErr := c.closeLocked()
		if ctxErr := callCtx.Err(); ctxErr != nil {
			return nil, errors.Join(ctxErr, err, closeErr)
		}
		return nil, errors.Join(err, closeErr)
	}
	deadline, ok := callCtx.Deadline()
	if !ok {
		deadline = time.Now().Add(10 * time.Second)
	}
	if err := conn.SetDeadline(deadline); err != nil {
		return failIO(fmt.Errorf("set elevated helper deadline: %w", err))
	}
	// AfterFunc may already have expired the deadline if cancellation won the
	// race. Putting the call deadline on afterwards would hide that and leave
	// the read blocked, so a cancelled call bails before any byte is written.
	if err := callCtx.Err(); err != nil {
		_ = conn.SetDeadline(time.Now())
		return nil, err
	}
	id := c.nextID.Add(1)
	request := message{Version: protocolVersion, ID: id, Operation: operation, Secret: c.secret, Payload: payload}
	if err := writeMessage(conn, request); err != nil {
		return failIO(fmt.Errorf("write elevated helper request: %w", err))
	}
	response, err := readMessage(conn)
	if err != nil {
		return failIO(fmt.Errorf("read elevated helper response: %w", err))
	}
	if response.Version != protocolVersion || response.ID != id {
		return nil, errors.Join(errors.New("invalid elevated helper response"), c.closeLocked())
	}
	if !response.OK {
		return nil, elevatedHelperError(response)
	}
	return response.Payload, nil
}

// remoteError is a failure the helper reported. Its text keeps the stable UPPER_SNAKE code first.
type remoteError struct {
	code    string
	message string
}

func (e *remoteError) Error() string {
	if e.code == "" {
		return "elevated helper: " + e.message
	}
	return e.code + ": elevated helper: " + e.message
}

func elevatedHelperError(response message) error {
	return &remoteError{code: response.ErrorCode, message: response.Error}
}

func (c *Client) closeLocked() error {
	hadConn := c.conn != nil

	// Drop the watchStop reference before the watcher goroutine closes that
	// event. SetEvent is safe while the handle is still open; a later Start
	// must not SetEvent on a recycled handle.
	c.signalWatchLocked()

	var err error
	if c.conn != nil {
		err = c.conn.Close()
	}
	c.conn, c.secret = nil, ""
	if c.process != 0 {
		if stopErr := stopHelperProcess(c.process); stopErr != nil {
			err = errors.Join(err, infra.AnnotateError(stopErr, infra.Diagnostic{
				Operation: "elevated-helper", Stage: "stop-process",
				Fields: map[string]any{"helperPID": c.pid, "cleanupPending": true},
			}))
		} else {
			err = errors.Join(err, windows.CloseHandle(c.process))
			c.process, c.pid = 0, 0
		}
	} else {
		c.pid = 0
	}
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
		return info.hProcess, 0, fmt.Errorf("read elevated helper pid: %w", err)
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
