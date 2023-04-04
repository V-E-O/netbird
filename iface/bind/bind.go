package bind

import (
	"errors"
	"fmt"
	"github.com/pion/stun"
	"github.com/pion/transport/v2"
	log "github.com/sirupsen/logrus"
	"golang.zx2c4.com/wireguard/conn"
	"net"
	"net/netip"
	"sync"
	"syscall"
)

type ICEBind struct {
	// below fields, initialized on open
	sharedConn net.PacketConn
	udpMux     *UniversalUDPMuxDefault

	// below are fields initialized on creation
	transportNet transport.Net
	mu           sync.Mutex
}

// NewICEBind create a new instance of ICEBind with a given transportNet and an interfaceFilter function.
// The interfaceFilter function is used to exclude interfaces from hole punching (the IPs of that interfaces won't be used as connection candidates)
// The transportNet can be nil.
func NewICEBind(transportNet transport.Net) *ICEBind {
	return &ICEBind{
		transportNet: transportNet,
		mu:           sync.Mutex{},
	}
}

func (b *ICEBind) GetICEMux() (*UniversalUDPMuxDefault, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.udpMux == nil {
		return nil, fmt.Errorf("ICEBind has not been initialized yet")
	}

	return b.udpMux, nil
}

func (b *ICEBind) Open(uport uint16) ([]conn.ReceiveFunc, uint16, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.sharedConn != nil {
		return nil, 0, conn.ErrBindAlreadyOpen
	}

	port := int(uport)
	ipv4Conn, port, err := listenNet("udp4", port)
	if err != nil && !errors.Is(err, syscall.EAFNOSUPPORT) {
		return nil, 0, err
	}
	b.sharedConn = ipv4Conn
	b.udpMux = NewUniversalUDPMuxDefault(UniversalUDPMuxParams{UDPConn: b.sharedConn, Net: b.transportNet})

	portAddr1, err := netip.ParseAddrPort(ipv4Conn.LocalAddr().String())
	if err != nil {
		return nil, 0, err
	}

	log.Infof("opened ICEBind on %s", ipv4Conn.LocalAddr().String())

	return []conn.ReceiveFunc{
			b.makeReceiveIPv4(b.sharedConn),
		},
		portAddr1.Port(), nil
}

func listenNet(network string, port int) (*net.UDPConn, int, error) {
	conn, err := net.ListenUDP(network, &net.UDPAddr{Port: port})
	if err != nil {
		return nil, 0, err
	}

	// Retrieve port.
	laddr := conn.LocalAddr()
	uaddr, err := net.ResolveUDPAddr(
		laddr.Network(),
		laddr.String(),
	)
	if err != nil {
		return nil, 0, err
	}
	return conn, uaddr.Port, nil
}

func parseSTUNMessage(raw []byte) (*stun.Message, error) {
	msg := &stun.Message{
		Raw: append([]byte{}, raw...),
	}
	if err := msg.Decode(); err != nil {
		return nil, err
	}

	return msg, nil
}

func (b *ICEBind) makeReceiveIPv4(c net.PacketConn) conn.ReceiveFunc {
	return func(buff []byte) (int, conn.Endpoint, error) {
		n, endpoint, err := c.ReadFrom(buff)
		if err != nil {
			return 0, nil, err
		}
		e, err := netip.ParseAddrPort(endpoint.String())
		if err != nil {
			return 0, nil, err
		}
		if !stun.IsMessage(buff[:20]) {
			// WireGuard traffic
			return n, (conn.StdNetEndpoint)(netip.AddrPortFrom(e.Addr(), e.Port())), nil
		}

		msg, err := parseSTUNMessage(buff[:n])
		if err != nil {
			return 0, nil, err
		}

		err = b.udpMux.HandleSTUNMessage(msg, endpoint)
		if err != nil {
			return 0, nil, err
		}
		if err != nil {
			log.Warnf("failed to handle packet")
		}

		// discard packets because they are STUN related
		return 0, nil, nil //todo proper return
	}
}

func (b *ICEBind) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	var err1, err2 error
	if b.sharedConn != nil {
		c := b.sharedConn
		b.sharedConn = nil
		err1 = c.Close()
	}

	if b.udpMux != nil {
		m := b.udpMux
		b.udpMux = nil
		err2 = m.Close()
	}

	if err1 != nil {
		return err1
	}

	return err2
}

// SetMark sets the mark for each packet sent through this Bind.
// This mark is passed to the kernel as the socket option SO_MARK.
func (b *ICEBind) SetMark(mark uint32) error {
	return nil
}

func (b *ICEBind) Send(buff []byte, endpoint conn.Endpoint) error {

	nend, ok := endpoint.(conn.StdNetEndpoint)
	if !ok {
		return conn.ErrWrongEndpointType
	}
	addrPort := netip.AddrPort(nend)
	_, err := b.sharedConn.WriteTo(buff, &net.UDPAddr{
		IP:   addrPort.Addr().AsSlice(),
		Port: int(addrPort.Port()),
		Zone: addrPort.Addr().Zone(),
	})
	return err
}

// ParseEndpoint creates a new endpoint from a string.
func (b *ICEBind) ParseEndpoint(s string) (ep conn.Endpoint, err error) {
	e, err := netip.ParseAddrPort(s)
	return asEndpoint(e), err
}

// endpointPool contains a re-usable set of mapping from netip.AddrPort to Endpoint.
// This exists to reduce allocations: Putting a netip.AddrPort in an Endpoint allocates,
// but Endpoints are immutable, so we can re-use them.
var endpointPool = sync.Pool{
	New: func() any {
		return make(map[netip.AddrPort]conn.Endpoint)
	},
}

// asEndpoint returns an Endpoint containing ap.
func asEndpoint(ap netip.AddrPort) conn.Endpoint {
	m := endpointPool.Get().(map[netip.AddrPort]conn.Endpoint)
	defer endpointPool.Put(m)
	e, ok := m[ap]
	if !ok {
		e = conn.Endpoint(conn.StdNetEndpoint(ap))
		m[ap] = e
	}
	return e
}