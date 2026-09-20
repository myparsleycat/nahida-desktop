//go:build windows

package elevated

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"

	"nahida.live/desktop/internal/platform"
)

type ServerOptions struct {
	Pipe      string
	Secret    string
	ParentPID uint32
}

func RunServer(ctx context.Context, options ServerOptions) error {
	if options.Pipe == "" || options.Secret == "" || options.ParentPID == 0 {
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
	helperPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("read elevated helper image: %w", err)
	}
	expectedParent := filepath.Join(filepath.Dir(helperPath), mainExecutableName)
	if !equalPath(parentPath, expectedParent) {
		return fmt.Errorf("reject parent process image %q, expected %q", parentPath, expectedParent)
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

	parentGone := make(chan struct{})
	go func() {
		_, _ = windows.WaitForSingleObject(parent, windows.INFINITE)
		close(parentGone)
		_ = listener.Close()
	}()
	conn, err := listener.Accept()
	if err != nil {
		select {
		case <-parentGone:
			return nil
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
	go func() {
		select {
		case <-ctx.Done():
		case <-parentGone:
		}
		_ = conn.Close()
	}()

	input := platform.NewInput()
	authenticated := false
	for {
		request, readErr := readMessage(conn)
		if readErr != nil {
			if errors.Is(readErr, net.ErrClosed) || errors.Is(readErr, os.ErrClosed) {
				return nil
			}
			return readErr
		}
		response := message{Version: protocolVersion, ID: request.ID}
		if request.Version != protocolVersion || !secretsEqual(request.Secret, options.Secret) {
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
			result, err := input.SendKeys(callCtx, keyRequest)
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
		default:
			response.Error = "operation is not allowed"
		}
		if err := writeMessage(conn, response); err != nil {
			return err
		}
	}
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
