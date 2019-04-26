package gosip

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/masterclock/gosip/log"
	"github.com/masterclock/gosip/sip"
	"github.com/masterclock/gosip/transaction"
	"github.com/masterclock/gosip/transport"
)

const (
	defaultHostAddr = "localhost"
)

// RequestHandler is a callback that will be called on the incoming request
// of the certain method
type RequestHandler func(req sip.Request)

// ServerConfig describes available options
type ServerConfig struct {
	HostAddr   string
	Extensions []string
}

var defaultConfig = &ServerConfig{
	HostAddr:   defaultHostAddr,
	Extensions: make([]string, 0),
}

// Server is a SIP server
type Server struct {
	tp              transport.Layer
	tx              transaction.Layer
	inShutdown      int32
	hwg             *sync.WaitGroup
	hmu             *sync.RWMutex
	requestHandlers map[sip.RequestMethod][]RequestHandler
	extensions      []string
}

// NewServer creates new instance of SIP server.
func NewServer(config *ServerConfig) *Server {
	var hostAddr string

	if config == nil {
		config = defaultConfig
	}

	if config.HostAddr != "" {
		hostAddr = config.HostAddr
	} else {
		hostAddr = defaultHostAddr
	}

	ctx := context.Background()
	tp := transport.NewLayer(hostAddr)
	tx := transaction.NewLayer(tp)
	srv := &Server{
		tp:              tp,
		tx:              tx,
		hwg:             new(sync.WaitGroup),
		hmu:             new(sync.RWMutex),
		requestHandlers: make(map[sip.RequestMethod][]RequestHandler),
		extensions:      config.Extensions,
	}

	go srv.serve(ctx)

	return srv
}

// ListenAndServe starts serving listeners on the provided address
func (srv *Server) Listen(network string, listenAddr string) error {
	if err := srv.tp.Listen(network, listenAddr); err != nil {
		// return immediately
		return err
	}

	return nil
}

func (srv *Server) serve(ctx context.Context) {
	defer srv.Shutdown()

	for {
		select {
		case <-ctx.Done():
			return
		case req := <-srv.tx.Requests():
			if req != nil { // if chan is closed or early exit
				srv.hwg.Add(1)
				go srv.handleRequest(req)
			}
		case res := <-srv.tx.Responses():
			if res != nil {
				log.Warnf("GoSIP server received not matched response: %s", res.Short())
				log.Debug(res.String())
			}
		case err := <-srv.tx.Errors():
			if err != nil {
				log.Errorf("GoSIP server received transaction error: %s", err)
			}
		case err := <-srv.tp.Errors():
			if err != nil {
				log.Error("GoSIP server received transport error: %s", err)
			}
		}
	}
}

func (srv *Server) handleRequest(req sip.Request) {
	defer srv.hwg.Done()

	log.Infof("GoSIP server handles incoming message %s", req.Short())
	log.Debugf("message:\n%s", req)

	srv.hmu.RLock()
	handlers, ok := srv.requestHandlers[req.Method()]
	srv.hmu.RUnlock()

	if ok {
		for _, handler := range handlers {
			handler(req)
		}
	} else if req.IsAck() {
		// nothing to do, just ignore it
	} else {
		log.Warnf("GoSIP server not found handler registered for the request %s", req.Short())

		res := sip.NewResponseFromRequest(req, 405, "Method Not Allowed", "")
		if _, err := srv.Respond(res); err != nil {
			log.Errorf("GoSIP server failed to respond on the unsupported request: %s", err)
		}
	}

	return
}

// Send SIP message
func (srv *Server) Request(req sip.Request) (<-chan sip.Response, error) {
	if srv.shuttingDown() {
		return nil, fmt.Errorf("can not send through stopped server")
	}

	return srv.tx.Request(srv.prepareRequest(req))
}

func (srv *Server) prepareRequest(req sip.Request) sip.Request {
	autoAppendMethods := map[sip.RequestMethod]bool{
		sip.INVITE:   true,
		sip.REGISTER: true,
		sip.REFER:    true,
		sip.NOTIFY:   true,
	}
	if _, ok := autoAppendMethods[req.Method()]; ok {
		hdrs := req.GetHeaders("Allow")
		if len(hdrs) == 0 {
			methods := make([]string, 0)
			for _, method := range srv.getAllowedMethods() {
				methods = append(methods, string(method))
			}
			req.AppendHeader(&sip.GenericHeader{
				HeaderName: "Allow",
				Contents:   strings.Join(methods, ", "),
			})
		}

		hdrs = req.GetHeaders("Supported")
		if len(hdrs) == 0 {
			req.AppendHeader(&sip.SupportedHeader{
				Options: srv.extensions,
			})
		}
	}

	hdrs := req.GetHeaders("User-Agent")
	if len(hdrs) == 0 {
		req.AppendHeader(&sip.GenericHeader{HeaderName: "User-Agent", Contents: "GoSIP"})
	}

	return req
}

func (srv *Server) Respond(res sip.Response) (<-chan sip.Request, error) {
	if srv.shuttingDown() {
		return nil, fmt.Errorf("can not send through stopped server")
	}

	return srv.tx.Respond(srv.prepareResponse(res))
}

func (srv *Server) prepareResponse(res sip.Response) sip.Response {
	autoAppendMethods := map[sip.RequestMethod]bool{
		sip.OPTIONS: true,
	}

	if cseq, ok := res.CSeq(); ok {
		methods := make([]string, 0)
		for _, method := range srv.getAllowedMethods() {
			methods = append(methods, string(method))
		}

		if _, ok := autoAppendMethods[cseq.MethodName]; ok {
			hdrs := res.GetHeaders("Allow")
			if len(hdrs) == 0 {
				res.AppendHeader(&sip.GenericHeader{
					HeaderName: "Allow",
					Contents:   strings.Join(methods, ", "),
				})
			} else {
				allowHeader := hdrs[0].(*sip.GenericHeader)
				allowHeader.Contents = strings.Join(methods, ", ")
			}

			hdrs = res.GetHeaders("Supported")
			if len(hdrs) == 0 {
				res.AppendHeader(&sip.SupportedHeader{
					Options: srv.extensions,
				})
			} else {
				supportedHeader := hdrs[0].(*sip.SupportedHeader)
				supportedHeader.Options = srv.extensions
			}
		}
	}

	return res
}

func (srv *Server) shuttingDown() bool {
	return atomic.LoadInt32(&srv.inShutdown) != 0
}

// Shutdown gracefully shutdowns SIP server
func (srv *Server) Shutdown() {
	if srv.shuttingDown() {
		return
	}

	atomic.AddInt32(&srv.inShutdown, 1)
	defer atomic.AddInt32(&srv.inShutdown, -1)
	// stop transaction layer
	srv.tx.Cancel()
	<-srv.tx.Done()
	// stop transport layer
	srv.tp.Cancel()
	<-srv.tp.Done()
	// wait for handlers
	srv.hwg.Wait()
}

// OnRequest registers new request callback
func (srv *Server) OnRequest(method sip.RequestMethod, handler RequestHandler) error {
	srv.hmu.Lock()
	defer srv.hmu.Unlock()

	handlers, ok := srv.requestHandlers[method]

	if !ok {
		handlers = make([]RequestHandler, 0)
	}

	for _, h := range handlers {
		if &h == &handler {
			return fmt.Errorf("handler already binded to %s method", method)
		}
	}

	srv.requestHandlers[method] = append(srv.requestHandlers[method], handler)

	return nil
}

func (srv *Server) getAllowedMethods() []sip.RequestMethod {
	methods := []sip.RequestMethod{
		sip.INVITE,
		sip.ACK,
		sip.CANCEL,
	}
	added := map[sip.RequestMethod]bool{
		sip.INVITE: true,
		sip.ACK:    true,
		sip.CANCEL: true,
	}

	srv.hmu.RLock()
	for method := range srv.requestHandlers {
		if _, ok := added[method]; !ok {
			methods = append(methods, method)
		}
	}
	srv.hmu.RUnlock()

	return methods
}
