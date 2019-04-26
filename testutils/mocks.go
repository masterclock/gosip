package testutils

import (
	"errors"
	"fmt"
	"net"

	"github.com/masterclock/gosip/log"
	"github.com/masterclock/gosip/sip"
)

type MockListener struct {
	addr        net.Addr
	connections chan net.Conn
	closed      chan bool
}

func NewMockListener(addr net.Addr) *MockListener {
	return &MockListener{
		addr:        addr,
		connections: make(chan net.Conn),
		closed:      make(chan bool),
	}
}

func (ls *MockListener) Accept() (net.Conn, error) {
	select {
	case <-ls.closed:
		return nil, &net.OpError{"accept", ls.addr.Network(), ls.addr, nil,
			errors.New("listener closed")}
	case conn := <-ls.connections:
		return conn, nil
	}
}

func (ls *MockListener) Close() error {
	defer func() { recover() }()
	close(ls.closed)
	return nil
}

func (ls *MockListener) Addr() net.Addr {
	return ls.addr
}

func (ls *MockListener) Dial(network string, addr net.Addr) (net.Conn, error) {
	select {
	case <-ls.closed:
		return nil, &net.OpError{"dial", addr.Network(), ls.addr, addr,
			errors.New("listener closed")}
	default:
	}

	server, client := net.Pipe()
	ls.connections <- &MockConn{server, addr, server.RemoteAddr()}

	return &MockConn{client, client.LocalAddr(), addr}, nil
}

// TODO implement with channels, all methods to replace net.Pipe in connection_pool_test.go
type MockAddr struct {
	Net  string
	Addr string
}

func (addr *MockAddr) Network() string {
	return addr.Net
}

func (addr *MockAddr) String() string {
	return addr.Addr
}

type MockConn struct {
	net.Conn
	LAddr net.Addr
	RAddr net.Addr
}

func (conn *MockConn) LocalAddr() net.Addr {
	if conn.LAddr == nil {
		return conn.Conn.LocalAddr()
	}
	return conn.LAddr
}

func (conn *MockConn) RemoteAddr() net.Addr {
	if conn.RAddr == nil {
		return conn.RemoteAddr()
	}
	return conn.RAddr
}

type MockTransportLayer struct {
	logger  log.LocalLogger
	InMsgs  chan sip.Message
	InErrs  chan error
	OutMsgs chan sip.Message
	done    chan struct{}
}

func NewMockTransportLayer() *MockTransportLayer {
	return &MockTransportLayer{
		logger:  log.NewSafeLocalLogger(),
		InMsgs:  make(chan sip.Message),
		InErrs:  make(chan error),
		OutMsgs: make(chan sip.Message),
		done:    make(chan struct{}),
	}
}

func (tpl *MockTransportLayer) HostAddr() string {
	return "127.0.0.1"
}

func (tpl *MockTransportLayer) Messages() <-chan sip.Message {
	return tpl.InMsgs
}

func (tpl *MockTransportLayer) Errors() <-chan error {
	return tpl.InErrs
}

func (tpl *MockTransportLayer) Listen(network string, addr string) error {
	return nil
}

func (tpl *MockTransportLayer) Send(msg sip.Message) error {
	tpl.OutMsgs <- msg
	return nil
}

func (tpl *MockTransportLayer) IsReliable(network string) bool {
	return true
}

func (tpl *MockTransportLayer) String() string {
	var addr string
	if tpl == nil {
		addr = "<nil>"
	} else {
		addr = fmt.Sprintf("%p", tpl)
	}

	return fmt.Sprintf("MockTransportLayer %s", addr)
}

func (tpl *MockTransportLayer) Log() log.Logger {
	return tpl.logger.Log()
}

func (tpl *MockTransportLayer) SetLog(logger log.Logger) {
	tpl.logger.SetLog(logger.WithFields(map[string]interface{}{
		"tp-layer": tpl.String(),
	}))
}

func (tpl *MockTransportLayer) Cancel() {
	close(tpl.InMsgs)
	close(tpl.InErrs)
	close(tpl.OutMsgs)
	close(tpl.done)
}

func (tpl *MockTransportLayer) Done() <-chan struct{} {
	return tpl.done
}
