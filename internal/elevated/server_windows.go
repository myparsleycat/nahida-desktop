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
	"time"

	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"

	"nahida.live/desktop/internal/platform"
	"nahida.live/desktop/internal/xxmi/inject"
)

type ServerOptions struct {
	Pipe      string
	Secret    string
	ParentPID uint32
	ParentExe string
}

type serverOperations struct {
	sendKeys   func(context.Context, platform.KeyRequest) (platform.KeyResult, error)
	launchXXMI func(context.Context, inject.LaunchSpec) (inject.LaunchResult, error)
	applyFiles func(context.Context, []FileOp) error
}

func RunServer(ctx context.Context, options ServerOptions) error {
	if options.Pipe == "" || options.Secret == "" || options.ParentPID == 0 || options.ParentExe == "" {
		return errors.New("missing elevated helper session parameters")
	}
	if !windows.GetCurrentProcessToken().IsElevated() {
		return errors.New("elevated helper is not running elevated")
	}
	parent, err := windows.OpenProcess(
		windows.SYNCHRONIZE|windows.PROCESS_QUERY_INFORMATION,
		false,
		options.ParentPID,
	)
	if err != nil {
		return fmt.Errorf("open parent process: %w", err)
	}
	defer func() { _ = windows.CloseHandle(parent) }()
	parentPath, err := processImagePath(parent)
	if err != nil {
		return fmt.Errorf("read parent process image: %w", err)
	}
	// The client passes its own executable path and the kernel reports the image
	// the parent actually runs. Requiring the two to match keeps the helper from
	// serving a parent that misrepresents itself, without pinning the hardcoded
	// "nahida-desktop.exe" name that rejected renamed portable builds. The pipe
	// secret remains the authentication; this is a consistency check.
	if !equalPath(parentPath, options.ParentExe) {
		return fmt.Errorf("reject parent process image %q, expected %q", parentPath, options.ParentExe)
	}

	// Grant the pipe to the verified parent process user, not the helper token.
	// A standard user who starts the helper with different administrator
	// credentials gets a helper token for that administrator, so an ACL built
	// from the helper SID would leave the ordinary parent process with access
	// denied. The parent PID and image are already verified above.
	parentSID, err := processUserSID(parent)
	if err != nil {
		return fmt.Errorf("read parent process user: %w", err)
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
		return fmt.Errorf("listen on elevated helper pipe: %w", err)
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
			return fmt.Errorf("accept elevated helper client: %w", err)
		}
	}
	defer func() { _ = conn.Close() }()
	clientPID, err := namedPipeProcessID(conn, procGetNamedPipeClientPID)
	if err != nil {
		return fmt.Errorf("read elevated helper pipe client pid: %w", err)
	}
	if clientPID != options.ParentPID {
		return fmt.Errorf("reject elevated helper client pid %d, expected %d", clientPID, options.ParentPID)
	}
	input := platform.NewInput()
	return serveHelperConnection(ctx, conn, options.Secret, serverOperations{
		sendKeys: input.SendKeys, launchXXMI: inject.Launch, applyFiles: applyFileOps,
	})
}

// serveHelperConnection reads independently of the current operation so a
// disconnect cancels held keys and loader waits immediately. Operations remain
// sequential, and the server returns only after their cleanup has completed.
func serveHelperConnection(ctx context.Context, conn net.Conn, secret string, operations serverOperations) error {
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
		case operationKeys:
			var keyRequest platform.KeyRequest
			if err := json.Unmarshal(request.Payload, &keyRequest); err != nil {
				response.Error = "invalid input request"
				break
			}
			callCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
			result, err := operations.sendKeys(callCtx, keyRequest)
			cancel()
			if err != nil {
				response.ErrorCode, response.Error = platform.ClassifyInputError(err)
				break
			}
			response.Payload, err = json.Marshal(result)
			if err != nil {
				response.Error = "encode input result"
				break
			}
			response.OK = true
		case operationXXMILaunch:
			var spec inject.LaunchSpec
			if err := json.Unmarshal(request.Payload, &spec); err != nil {
				response.ErrorCode, response.Error = "XXMI_INVALID_LAUNCH", "invalid XXMI launch request"
				break
			}
			callCtx, cancel := context.WithTimeout(ctx, time.Duration(spec.TimeoutSeconds+60)*time.Second)
			result, err := operations.launchXXMI(callCtx, spec)
			cancel()
			if err != nil {
				response.ErrorCode, response.Error = inject.ClassifyError(err), err.Error()
				break
			}
			response.Payload, err = json.Marshal(result)
			if err != nil {
				response.Error = "encode XXMI launch result"
				break
			}
			response.OK = true
		case operationFiles:
			var ops []FileOp
			if err := json.Unmarshal(request.Payload, &ops); err != nil {
				response.Error = "invalid file request"
				break
			}
			callCtx, cancel := context.WithTimeout(ctx, fileOpsTimeout)
			err := operations.applyFiles(callCtx, ops)
			cancel()
			if err != nil {
				response.Error = err.Error()
				break
			}
			response.OK = true
		default:
			response.Error = "operation is not allowed"
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
