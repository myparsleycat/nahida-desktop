package gamelink

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

const (
	clientName       = "Nahida Desktop"
	handshakeTimeout = 5 * time.Second
)

var (
	ErrClosed = errors.New("desktop channel closed")
	// ErrIncompatible means the DLL speaks no protocol version this desktop knows; retrying will not help.
	ErrIncompatible = errors.New("desktop channel version not supported")
	// ErrForeignServer means the pipe of a game process is served by another process.
	ErrForeignServer = errors.New("desktop channel pipe served by another process")
)

// RemoteError is an Error message the DLL answered a request with.
type RemoteError struct {
	Code   string
	Detail string
}

func (e *RemoteError) Error() string {
	if e.Detail == "" {
		return "desktop channel: " + e.Code
	}
	return "desktop channel: " + e.Code + ": " + e.Detail
}

// Conn is one handshaken connection to the DLL of one game process. Messages the
// DLL sends on its own go to the handler from the read goroutine, in order.
type Conn struct {
	conn    net.Conn
	pid     uint32
	info    HelloReply
	handler func(Message)

	writeGate chan struct{}
	nextID    atomic.Uint32

	mu       sync.Mutex
	pending  map[uint32]chan Message
	err      error
	done     chan struct{}
	readDone chan struct{}
}

func newConn(ctx context.Context, conn net.Conn, pid uint32, handler func(Message)) (*Conn, error) {
	c := &Conn{
		conn: conn, pid: pid, handler: handler, writeGate: make(chan struct{}, 1),
		pending: map[uint32]chan Message{}, done: make(chan struct{}), readDone: make(chan struct{}),
	}
	info, err := c.handshake(ctx)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	c.info = info
	go c.readLoop()
	return c, nil
}

func (c *Conn) handshake(ctx context.Context) (HelloReply, error) {
	deadline := time.Now().Add(handshakeTimeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	_ = c.conn.SetDeadline(deadline)
	defer func() { _ = c.conn.SetDeadline(time.Time{}) }()
	stop := context.AfterFunc(ctx, func() { _ = c.conn.SetDeadline(time.Unix(1, 0)) })
	defer stop()

	id := c.nextID.Add(1)
	if err := c.write(
		ctx,
		id,
		Hello{Client: clientName, MinVersion: ProtocolVersion, MaxVersion: ProtocolVersion},
	); err != nil {
		return HelloReply{}, contextError(ctx, err)
	}
	frame, err := ReadFrame(c.conn)
	if err != nil {
		return HelloReply{}, contextError(ctx, fmt.Errorf("read hello reply: %w", err))
	}
	m, err := Decode(frame)
	if err != nil {
		return HelloReply{}, fmt.Errorf("read hello reply: %w", err)
	}
	switch reply := m.(type) {
	case HelloReply:
		if frame.RequestID != id || reply.Version != ProtocolVersion {
			return HelloReply{}, fmt.Errorf("%w: hello reply %d for request %d, version %d",
				ErrIncompatible, frame.RequestID, id, reply.Version)
		}
		if reply.PID != c.pid {
			return HelloReply{}, fmt.Errorf("%w: hello from pid %d", ErrForeignServer, reply.PID)
		}
		return reply, nil
	case ErrorReply:
		if reply.Code == ErrorUnsupportedVersion {
			return HelloReply{}, fmt.Errorf("%w: DLL supports %s", ErrIncompatible, reply.Detail)
		}
		return HelloReply{}, &RemoteError{Code: reply.Code, Detail: reply.Detail}
	default:
		return HelloReply{}, fmt.Errorf("%w: %T before hello reply", ErrMalformed, m)
	}
}

func contextError(ctx context.Context, err error) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	return err
}

func (c *Conn) PID() uint32      { return c.pid }
func (c *Conn) Info() HelloReply { return c.info }

// Done is closed once the connection has failed or was closed; Err then says why.
func (c *Conn) Done() <-chan struct{} { return c.done }

func (c *Conn) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}

func (c *Conn) Close() error {
	c.fail(ErrClosed)
	return nil
}

// Request sends a message and waits for the reply with its request ID. An Error
// reply comes back as *RemoteError.
func (c *Conn) Request(ctx context.Context, m Message) (Message, error) {
	id := c.nextID.Add(1)
	if id == 0 {
		id = c.nextID.Add(1)
	}
	reply := make(chan Message, 1)
	c.mu.Lock()
	if c.err != nil {
		err := c.err
		c.mu.Unlock()
		return nil, err
	}
	c.pending[id] = reply
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
	}()

	if err := c.write(ctx, id, m); err != nil {
		return nil, err
	}
	select {
	case r := <-reply:
		if e, ok := r.(ErrorReply); ok {
			return nil, &RemoteError{Code: e.Code, Detail: e.Detail}
		}
		return r, nil
	case <-c.done:
		return nil, c.Err()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (c *Conn) write(ctx context.Context, id uint32, m Message) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	frame, err := Encode(id, m)
	if err != nil {
		return err
	}
	data, err := AppendFrame(nil, frame)
	if err != nil {
		return err
	}
	select {
	case c.writeGate <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	case <-c.done:
		return c.Err()
	}
	defer func() { <-c.writeGate }()
	if err := ctx.Err(); err != nil {
		return err
	}

	// Cancellation during a write may leave a partial frame, so the connection cannot be reused.
	stop := context.AfterFunc(ctx, func() { c.fail(ctx.Err()) })
	defer stop()
	if _, err := c.conn.Write(data); err != nil {
		err = contextError(ctx, fmt.Errorf("write %T: %w", m, err))
		c.fail(err)
		return err
	}
	return nil
}

func (c *Conn) readLoop() {
	defer close(c.readDone)
	for {
		frame, err := ReadFrame(c.conn)
		if err != nil {
			if errors.Is(err, io.EOF) {
				err = ErrClosed
			}
			c.fail(err)
			return
		}
		m, err := Decode(frame)
		if errors.Is(err, ErrUnknownType) {
			continue
		}
		if err != nil {
			c.fail(err)
			return
		}

		if ping, ok := m.(Ping); ok {
			if err := c.write(context.Background(), frame.RequestID, Pong(ping)); err != nil {
				c.fail(err)
				return
			}
			continue
		}
		if frame.RequestID == 0 {
			if c.handler != nil {
				c.handler(m)
			}
			continue
		}
		c.mu.Lock()
		reply := c.pending[frame.RequestID]
		c.mu.Unlock()
		if reply != nil {
			reply <- m
		}
	}
}

func (c *Conn) fail(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.err != nil {
		return
	}
	c.err = err
	_ = c.conn.Close()
	close(c.done)
}
