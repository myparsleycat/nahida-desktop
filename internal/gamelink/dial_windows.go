package gamelink

import (
	"context"
	"errors"
	"fmt"
	"net"

	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"
)

// ErrNotListening means the game has no channel pipe yet: the DLL creates it
// after the first Present, and only with [Desktop] channel = 1.
var ErrNotListening = errors.New("desktop channel pipe not found")

// ErrPipeBusy means another client holds the game's only pipe instance.
var ErrPipeBusy = errors.New("desktop channel pipe in use by another client")

func PipeName(pid uint32) string {
	return fmt.Sprintf(`\\.\pipe\nahida-migoto-%d`, pid)
}

// Dial connects to the DLL in the game process pid and completes the handshake.
func Dial(ctx context.Context, pid uint32, handler func(Message)) (*Conn, error) {
	conn, err := dialPipe(ctx, pid)
	if err != nil {
		return nil, err
	}
	return newConn(ctx, conn, pid, handler)
}

func dialPipe(ctx context.Context, pid uint32) (net.Conn, error) {
	conn, err := winio.DialPipeContext(ctx, PipeName(pid))
	if errors.Is(err, windows.ERROR_FILE_NOT_FOUND) {
		return nil, ErrNotListening
	}
	// winio retries until the deadline only while the pipe answers ERROR_PIPE_BUSY.
	if errors.Is(err, context.DeadlineExceeded) {
		return nil, ErrPipeBusy
	}
	if err != nil {
		return nil, err
	}

	// Any process can create a pipe of this name first, so the server must be the game itself.
	server, err := serverProcessID(conn)
	if err == nil && server != pid {
		err = fmt.Errorf("%w: pid %d", ErrForeignServer, server)
	}
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	return conn, nil
}

func serverProcessID(conn net.Conn) (uint32, error) {
	fd, ok := conn.(interface{ Fd() uintptr })
	if !ok {
		return 0, fmt.Errorf("pipe connection %T has no handle", conn)
	}
	var pid uint32
	if err := windows.GetNamedPipeServerProcessId(windows.Handle(fd.Fd()), &pid); err != nil {
		return 0, fmt.Errorf("query pipe server process: %w", err)
	}
	return pid, nil
}
