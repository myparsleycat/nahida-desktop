//go:build windows

package elevated

import (
	"context"
	"crypto/rand"
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

const (
	helperExecutableName = "nahida-elevated-helper.exe"
	mainExecutableName   = "nahida-desktop.exe"
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
	mu      sync.Mutex
	conn    net.Conn
	process windows.Handle
	pid     uint32
	secret  string
	enabled bool
	nextID  atomic.Uint64
}

func NewClient() *Client {
	return &Client{}
}

func (c *Client) Start(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.startLocked(ctx)
}

func (c *Client) startLocked(ctx context.Context) error {
	if c.conn != nil {
		c.enabled = true
		return nil
	}

	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve application executable: %w", err)
	}
	helper := filepath.Join(filepath.Dir(executable), helperExecutableName)
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

	secretBytes := make([]byte, 32)
	if _, err := rand.Read(secretBytes); err != nil {
		return fmt.Errorf("create elevated helper secret: %w", err)
	}
	secret := base64.RawURLEncoding.EncodeToString(secretBytes)
	pipe := `\\.\pipe\nahida-elevated-` + strconv.FormatUint(uint64(os.Getpid()), 10) + "-" + secret[:16]
	process, pid, err := launch(helper, pipe, secret, uint32(os.Getpid()))
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

	c.conn, c.process, c.pid, c.secret = conn, process, pid, secret
	if _, err := c.callLocked(connectCtx, operationHello, nil); err != nil {
		return errors.Join(err, c.closeLocked())
	}
	c.enabled = true
	return nil
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

func (c *Client) SendKeys(ctx context.Context, request platform.KeyRequest) (platform.KeyResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn == nil {
		if !c.enabled {
			return platform.KeyResult{}, fmt.Errorf(
				"%w: elevated helper is not running",
				platform.ErrElevatedHelperRequired,
			)
		}
		if err := c.startLocked(ctx); err != nil {
			return platform.KeyResult{}, fmt.Errorf(
				"%w: restart elevated helper: %w",
				platform.ErrElevatedHelperRequired,
				err,
			)
		}
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
	c.enabled = false
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
	var err error
	if c.conn != nil {
		err = c.conn.Close()
	}
	if c.process != 0 {
		err = errors.Join(err, windows.CloseHandle(c.process))
	}
	c.conn, c.process, c.pid, c.secret = nil, 0, 0, ""
	return err
}

func launch(helper, pipe, secret string, parentPID uint32) (windows.Handle, uint32, error) {
	verb, _ := syscall.UTF16PtrFromString("runas")
	file, _ := syscall.UTF16PtrFromString(helper)
	parameters, _ := syscall.UTF16PtrFromString(
		"--pipe " + pipe + " --secret " + secret + " --parent-pid " + strconv.FormatUint(uint64(parentPID), 10),
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
