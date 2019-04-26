package transaction

import (
	"fmt"

	"github.com/discoviking/fsm"
	"github.com/masterclock/gosip/log"
	"github.com/masterclock/gosip/sip"
	"github.com/masterclock/gosip/transport"
)

type TxKey string

func (key TxKey) String() string {
	return string(key)
}

// Tx is an common SIP transaction
type Tx interface {
	log.LocalLogger
	Init() error
	Key() TxKey
	Origin() sip.Request
	// Receive receives message from transport layer.
	Receive(msg sip.Message) error
	String() string
	Transport() transport.Layer
	Terminate()
	Errors() <-chan error
	Done() <-chan bool
}

type commonTx struct {
	logger   log.LocalLogger
	key      TxKey
	fsm      *fsm.FSM
	origin   sip.Request
	tpl      transport.Layer
	lastResp sip.Response
	errs     chan error
	lastErr  error
	done     chan bool
}

func (tx *commonTx) String() string {
	if tx == nil {
		return "Tx <nil>"
	}

	return fmt.Sprintf("Tx %p [%s]", tx, tx.Origin().Short())
}

func (tx *commonTx) Log() log.Logger {
	return tx.logger.Log()
}

func (tx *commonTx) SetLog(logger log.Logger) {
	tx.logger.SetLog(logger.WithFields(map[string]interface{}{
		"tx": tx.String(),
	}))
}

func (tx *commonTx) Origin() sip.Request {
	return tx.origin
}

func (tx *commonTx) Key() TxKey {
	return tx.key
}

func (tx *commonTx) Transport() transport.Layer {
	return tx.tpl
}

func (tx *commonTx) Errors() <-chan error {
	return tx.errs
}

func (tx *commonTx) Done() <-chan bool {
	return tx.done
}
