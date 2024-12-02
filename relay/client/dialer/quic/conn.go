package quic

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/quic-go/quic-go"
	log "github.com/sirupsen/logrus"
)

const (
	Network = "quic"
)

type Addr struct {
	addr string
}

func (a Addr) Network() string {
	return Network
}

func (a Addr) String() string {
	return a.addr
}

type Conn struct {
	session    quic.Connection
	remoteAddr Addr
	ctx        context.Context
}

func NewConn(session quic.Connection, serverAddress string) net.Conn {
	return &Conn{
		session:    session,
		remoteAddr: Addr{addr: serverAddress},
		ctx:        context.Background(),
	}
}

func (c *Conn) Read(b []byte) (n int, err error) {
	dgram, err := c.session.ReceiveDatagram(c.ctx)
	if err != nil {
		log.Errorf("failed to read from QUIC session: %v", err)
		return 0, err
	}

	n = copy(b, dgram)
	return n, nil
}

func (c *Conn) Write(b []byte) (int, error) {
	err := c.session.SendDatagram(b)
	if err != nil {
		log.Errorf("failed to write to QUIC stream: %v", err)
		return 0, err
	}
	return len(b), nil
}

func (c *Conn) RemoteAddr() net.Addr {
	if c.session != nil {
		return c.session.RemoteAddr()
	}
	return c.remoteAddr
}

func (c *Conn) LocalAddr() net.Addr {
	if c.session != nil {
		return c.session.LocalAddr()
	}
	return Addr{addr: "unknown"}
}

func (c *Conn) SetReadDeadline(t time.Time) error {
	return fmt.Errorf("SetReadDeadline is not implemented")
}

func (c *Conn) SetWriteDeadline(t time.Time) error {
	return fmt.Errorf("SetWriteDeadline is not implemented")
}

func (c *Conn) SetDeadline(t time.Time) error {

	return nil
}

func (c *Conn) Close() error {
	return c.session.CloseWithError(0, "normal closure")
}
