//go:build windows

package blenderlink

import (
	"errors"
	"fmt"
	"net"

	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"
)

const pipePrefix = `\\.\pipe\nahida-blender-link-`

// listenPipe creates the first instance of a new randomly named pipe. go-winio creates it with the
// FILE_CREATE disposition, which fails when the name exists, and sets FILE_PIPE_REJECT_REMOTE_CLIENTS
// on every instance.
func listenPipe() (net.Listener, string, error) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, "", fmt.Errorf("read process user: %w", err)
	}
	pipe := pipePrefix + randomString(16)
	listener, err := winio.ListenPipe(pipe, &winio.PipeConfig{
		SecurityDescriptor: "D:P(A;;GRGW;;;" + user.User.Sid.String() + ")S:(ML;;NW;;;ME)",
		InputBufferSize:    MaxChunkData,
		OutputBufferSize:   64 << 10,
	})
	if err != nil {
		return nil, pipe, fmt.Errorf("listen on %s: %w", pipe, err)
	}
	return listener, pipe, nil
}

func pipeClientPID(conn net.Conn) (uint32, error) {
	fd, ok := conn.(interface{ Fd() uintptr })
	if !ok {
		return 0, fmt.Errorf("%T is not a pipe", conn)
	}
	var pid uint32
	if err := windows.GetNamedPipeClientProcessId(windows.Handle(fd.Fd()), &pid); err != nil {
		return 0, fmt.Errorf("read pipe client pid: %w", err)
	}
	return pid, nil
}

func isPipeDisconnect(err error) bool {
	return errors.Is(err, windows.ERROR_BROKEN_PIPE) || errors.Is(err, windows.ERROR_NO_DATA) ||
		errors.Is(err, windows.ERROR_PIPE_NOT_CONNECTED)
}
