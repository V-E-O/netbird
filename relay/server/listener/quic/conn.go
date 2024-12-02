package quic

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"github.com/quic-go/quic-go"
)

type Conn struct {
	session   quic.Connection
	closed    bool
	closedMu  sync.Mutex
	ctx       context.Context
	ctxCancel context.CancelFunc
}

func NewConn(session quic.Connection) *Conn {
	ctx, cancel := context.WithCancel(context.Background())
	return &Conn{
		session:   session,
		ctx:       ctx,
		ctxCancel: cancel,
	}
}

func (c *Conn) Read(b []byte) (n int, err error) {
	if c.isClosed() {
		return 0, io.EOF
	}

	dgram, err := c.session.ReceiveDatagram(c.ctx)
	if err != nil {
		return 0, c.ioErrHandling(err)
	}
	// Copy data to b, ensuring we don’t exceed the size of b
	n = copy(b, dgram)
	return n, nil
}

func (c *Conn) Write(b []byte) (int, error) {
	err := c.session.SendDatagram(b)
	return len(b), err
}

func (c *Conn) LocalAddr() net.Addr {
	return c.session.LocalAddr()
}

func (c *Conn) RemoteAddr() net.Addr {
	return c.session.RemoteAddr()
}

func (c *Conn) SetReadDeadline(t time.Time) error {
	return nil
}

func (c *Conn) SetWriteDeadline(t time.Time) error {
	return fmt.Errorf("SetWriteDeadline is not implemented")
}

func (c *Conn) SetDeadline(t time.Time) error {
	return fmt.Errorf("SetDeadline is not implemented")
}

func (c *Conn) Close() error {
	c.closedMu.Lock()
	if c.closed {
		c.closedMu.Unlock()
		return nil
	}
	c.closed = true
	c.closedMu.Unlock()

	c.ctxCancel() // Cancel the context

	sessionErr := c.session.CloseWithError(0, "normal closure")
	return sessionErr
}

func (c *Conn) isClosed() bool {
	c.closedMu.Lock()
	defer c.closedMu.Unlock()
	return c.closed
}

func (c *Conn) ioErrHandling(err error) error {
	if c.isClosed() {
		return io.EOF
	}

	// Handle QUIC-specific errors
	if err == nil {
		return nil
	}

	// Check if the connection was closed remotely
	var appErr *quic.ApplicationError
	if errors.As(err, &appErr) && appErr.ErrorCode == 0 { // 0 is normal closure
		return io.EOF
	}

	return err
}
