//go:build windows

package elevated

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"time"

	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"
)

type ServerOptions struct {
	Pipe      string
	Secret    string
	ParentPID uint32
	ParentExe string
	// Handlers is the complete set of operations the helper serves besides the session ones.
	Handlers map[string]Handler
}

// Peer is the verified parent process the helper serves.
type Peer struct {
	UserSID *windows.SID
}

// Handler serves one helper operation. The server cancels Run after Timeout or when the client
// disconnects; Run may apply a shorter deadline that depends on its payload. Classify turns a Run
// failure into the stable code and detail the client receives, and defaults to the error text.
type Handler struct {
	Timeout  time.Duration
	Run      func(ctx context.Context, peer Peer, payload json.RawMessage) (json.RawMessage, error)
	Classify func(error) (code, detail string)
}

func (h Handler) classify(err error) (string, string) {
	if h.Classify == nil {
		return "", err.Error()
	}
	return h.Classify(err)
}

func validateHandlers(handlers map[string]Handler) error {
	for name, handler := range handlers {
		if name == "" || strings.HasPrefix(name, sessionOperationPrefix) {
			return fmt.Errorf("elevated helper operation %q is reserved", name)
		}
		if handler.Run == nil || handler.Timeout <= 0 {
			return fmt.Errorf("elevated helper operation %q needs a handler and a timeout", name)
		}
	}
	return nil
}

var (
	errServerParameters  = errors.New("missing elevated helper session parameters")
	errServerNotElevated = errors.New("elevated helper is not running elevated")
	errServerParent      = errors.New("elevated helper parent could not be verified")
	errServerPipe        = errors.New("elevated helper pipe failed")
	errServerClient      = errors.New("elevated helper client was rejected")
)

// serverExits are the exit codes of a helper whose startup failed, which the client reads when the
// helper exits before it connects. The helper ships inside the client, so both read the same table.
var serverExits = []struct {
	code uint32
	err  error
}{
	{code: 2, err: errServerParameters},
	{code: 3, err: errServerNotElevated},
	{code: 4, err: errServerParent},
	{code: 5, err: errHelperAlreadyRunning},
	{code: 6, err: errServerPipe},
	{code: 7, err: errServerClient},
	{code: 8, err: errServerHardening},
}

// ServerExitCode is the process exit code for a RunServer failure.
func ServerExitCode(err error) int {
	if err == nil {
		return 0
	}
	for _, exit := range serverExits {
		if errors.Is(err, exit.err) {
			return int(exit.code)
		}
	}
	return 1
}

func serverExitReason(code uint32) string {
	for _, exit := range serverExits {
		if exit.code == code {
			return exit.err.Error()
		}
	}
	return ""
}

func RunServer(ctx context.Context, options ServerOptions) error {
	if options.Pipe == "" || options.Secret == "" || options.ParentPID == 0 || options.ParentExe == "" {
		return errServerParameters
	}
	if err := validateHandlers(options.Handlers); err != nil {
		return err
	}
	if !windows.GetCurrentProcessToken().IsElevated() {
		return errServerNotElevated
	}
	parent, err := windows.OpenProcess(
		windows.SYNCHRONIZE|windows.PROCESS_QUERY_INFORMATION,
		false,
		options.ParentPID,
	)
	if err != nil {
		return fmt.Errorf("%w: open parent process: %w", errServerParent, err)
	}
	defer func() { _ = windows.CloseHandle(parent) }()
	parentPath, err := processImagePath(parent)
	if err != nil {
		return fmt.Errorf("%w: read parent process image: %w", errServerParent, err)
	}
	// The client passes its own executable path and the kernel reports the image
	// the parent actually runs. Requiring the two to match keeps the helper from
	// serving a parent that misrepresents itself, without pinning the hardcoded
	// "nahida-desktop.exe" name that rejected renamed portable builds. The pipe
	// secret remains the authentication; this is a consistency check.
	if !equalPath(parentPath, options.ParentExe) {
		return fmt.Errorf(
			"%w: reject parent process image %q, expected %q",
			errServerParent,
			parentPath,
			options.ParentExe,
		)
	}

	// Grant the pipe to the verified parent process user, not the helper token.
	// A standard user who starts the helper with different administrator
	// credentials gets a helper token for that administrator, so an ACL built
	// from the helper SID would leave the ordinary parent process with access
	// denied. The parent PID and image are already verified above.
	parentSID, err := processUserSID(parent)
	if err != nil {
		return fmt.Errorf("%w: read parent process user: %w", errServerParent, err)
	}
	instance, err := lockHelperInstance(parentSID.String())
	if err != nil {
		return err
	}
	defer func() { _ = windows.CloseHandle(instance) }()

	security := "D:P(A;;GA;;;" + parentSID.String() + ")(A;;GA;;;SY)(A;;GA;;;BA)S:(ML;;NW;;;LW)"
	listener, err := winio.ListenPipe(options.Pipe, &winio.PipeConfig{
		SecurityDescriptor: security,
		InputBufferSize:    maxMessageSize,
		OutputBufferSize:   maxMessageSize,
	})
	if err != nil {
		return fmt.Errorf("%w: listen: %w", errServerPipe, err)
	}
	defer func() { _ = listener.Close() }()

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	parentGone := make(chan struct{})
	watchStop, err := windows.CreateEvent(nil, 1, 0, nil)
	if err != nil {
		return fmt.Errorf("create elevated helper parent watch event: %w", err)
	}
	watchDone := make(chan struct{})
	go func() {
		defer close(watchDone)
		event, waitErr := windows.WaitForMultipleObjects([]windows.Handle{parent, watchStop}, false, windows.INFINITE)
		if waitErr != nil || event != windows.WAIT_OBJECT_0 {
			return
		}
		close(parentGone)
		cancel()
		_ = listener.Close()
	}()
	defer func() {
		_ = windows.SetEvent(watchStop)
		<-watchDone
		_ = windows.CloseHandle(watchStop)
	}()
	stopListener := context.AfterFunc(ctx, func() { _ = listener.Close() })
	defer stopListener()
	conn, err := listener.Accept()
	if err != nil {
		select {
		case <-parentGone:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		default:
			return fmt.Errorf("%w: accept: %w", errServerPipe, err)
		}
	}
	defer func() { _ = conn.Close() }()
	clientPID, err := namedPipeProcessID(conn, procGetNamedPipeClientPID)
	if err != nil {
		return fmt.Errorf("%w: read pipe client pid: %w", errServerClient, err)
	}
	if clientPID != options.ParentPID {
		return fmt.Errorf("%w: pid %d, expected %d", errServerClient, clientPID, options.ParentPID)
	}
	return serveHelperConnection(ctx, conn, options.Secret, Peer{UserSID: parentSID}, options.Handlers)
}

// serveHelperConnection reads independently of the current operation so a
// disconnect cancels held keys and loader waits immediately. Operations remain
// sequential, and the server returns only after their cleanup has completed.
func serveHelperConnection(
	ctx context.Context,
	conn net.Conn,
	secret string,
	peer Peer,
	handlers map[string]Handler,
) error {
	ctx, cancel := context.WithCancel(ctx)
	stopConnection := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stopConnection()
	requests := make(chan message)
	readDone := make(chan struct{})
	var readErr error
	go func() {
		defer close(requests)
		defer close(readDone)
		defer cancel()
		for {
			request, err := readMessage(conn)
			if err != nil {
				readErr = err
				return
			}
			select {
			case requests <- request:
			case <-ctx.Done():
				return
			}
		}
	}()
	defer func() {
		cancel()
		_ = conn.Close()
		<-readDone
	}()

	authenticated := false
	for request := range requests {
		if ctx.Err() != nil {
			return nil
		}
		response := message{Version: protocolVersion, ID: request.ID}
		if request.Version != protocolVersion || !secretsEqual(request.Secret, secret) {
			response.Error = "authentication failed"
			_ = writeMessage(conn, response)
			return errors.New("elevated helper authentication failed")
		}
		if !authenticated && request.Operation != operationHello {
			response.Error = "session handshake required"
			_ = writeMessage(conn, response)
			return errors.New("elevated helper handshake missing")
		}

		switch request.Operation {
		case operationHello:
			authenticated = true
			response.OK = true
		case operationPing:
			response.OK = authenticated
		case operationStop:
			response.OK = true
			if err := writeMessage(conn, response); err != nil {
				return err
			}
			return nil
		default:
			handler, ok := handlers[request.Operation]
			if !ok {
				response.Error = "operation is not allowed"
				break
			}
			callCtx, cancel := context.WithTimeout(ctx, handler.Timeout)
			result, err := handler.Run(callCtx, peer, request.Payload)
			cancel()
			if err != nil {
				response.ErrorCode, response.Error = handler.classify(err)
				break
			}
			response.Payload, response.OK = result, true
		}
		if ctx.Err() != nil {
			return nil
		}
		if err := writeMessage(conn, response); err != nil {
			return err
		}
	}
	if errors.Is(readErr, io.EOF) || errors.Is(readErr, net.ErrClosed) || errors.Is(readErr, os.ErrClosed) {
		return nil
	}
	return readErr
}

func processUserSID(process windows.Handle) (*windows.SID, error) {
	var token windows.Token
	if err := windows.OpenProcessToken(process, windows.TOKEN_QUERY, &token); err != nil {
		return nil, err
	}
	defer func() { _ = token.Close() }()

	user, err := token.GetTokenUser()
	if err != nil {
		return nil, err
	}
	return user.User.Sid, nil
}
