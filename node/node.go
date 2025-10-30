package node

import (
	"net/http"
	"reflect"
	"strconv"
	"sync"

	"unishard/config"
	"unishard/dlog"
	"unishard/log"
	"unishard/message"
	"unishard/socket"
	"unishard/types"
)

// Node is the primary access point for every replica
// it includes networking, state machine and RESTful API server
type Node interface {
	socket.Socket
	//Database
	ID() types.NodeID
	Shard() types.Shard
	State() types.NodeState
	Role() types.NodeRole
	SetState(state types.NodeState)
	SetRole(role types.NodeRole)
	Run()
	Retry(r message.Transaction)
	Register(m interface{}, f interface{})
	IsByz() bool
	DistLogger() *dlog.DistributedLogger
}

// node implements Node interface
type node struct {
	id    types.NodeID
	shard types.Shard
	state types.NodeState
	role  types.NodeRole

	socket.Socket
	//Database
	MessageChan           chan interface{}
	CrossChainMessageChan chan interface{}
	TxChan                chan interface{}
	handles               map[string]reflect.Value
	server                *http.Server
	isByz                 bool
	totalTxn              int

	sync.RWMutex
	forwards map[string]*message.Transaction
	dlogger  *dlog.DistributedLogger
}

// NewNode creates a new Node object from configuration
func NewNode(id types.NodeID, isByz bool, shard types.Shard) Node {
	dlogger := dlog.NewDistributedLogger("nats://127.0.0.1:4222", string(id), "", strconv.Itoa(int(shard)))
	dlogger.BootingSequence(0)
	return &node{
		id:     id,
		shard:  shard,
		state:  types.READY,
		role:   types.VALIDATOR,
		isByz:  isByz,
		Socket: socket.NewSocket(id, config.Configuration.Addrs, shard),
		//Database:    NewDatabase(),
		MessageChan:           make(chan interface{}, config.Configuration.ChanBufferSize),
		CrossChainMessageChan: make(chan interface{}, config.Configuration.ChanBufferSize),
		TxChan:                make(chan interface{}, config.Configuration.ChanBufferSize),
		handles:               make(map[string]reflect.Value),
		forwards:              make(map[string]*message.Transaction),
		dlogger:               dlogger,
	}
}

func (n *node) ID() types.NodeID {
	return n.id
}

func (n *node) State() types.NodeState {
	n.RWMutex.Lock()
	defer n.RWMutex.Unlock()
	return n.state
}

func (n *node) Role() types.NodeRole {
	n.RWMutex.Lock()
	defer n.RWMutex.Unlock()
	return n.role
}

func (n *node) Shard() types.Shard {
	return n.shard
}

func (n *node) IsByz() bool {
	return n.isByz
}

func (n *node) SetState(state types.NodeState) {
	n.RWMutex.Lock()
	defer n.RWMutex.Unlock()
	n.state = state
}

func (n *node) SetRole(role types.NodeRole) {
	n.RWMutex.Lock()
	defer n.RWMutex.Unlock()
	n.role = role
}

func (n *node) Retry(r message.Transaction) {
	log.Debugf("node %v retry reqeust %v", n.id, r)
	n.MessageChan <- r
}

// Register a handle function for each message type
func (n *node) Register(m interface{}, f interface{}) {
	t := reflect.TypeOf(m)
	fn := reflect.ValueOf(f)

	if fn.Kind() != reflect.Func {
		panic("handle function is not func")
	}

	if fn.Type().In(0) != t {
		panic("func type is not t")
	}

	if fn.Kind() != reflect.Func || fn.Type().NumIn() != 1 || fn.Type().In(0) != t {
		panic("register handle function error")
	}
	n.handles[t.String()] = fn
}

// Run start and run the node
func (n *node) Run() {
	log.Infof("node %v start running", n.id)
	if len(n.handles) > 0 {
		go n.handle()
		go n.handleCrossChain()
		go n.recv()
		go n.txn()
	}

	// running http
	n.http()
}

func (n *node) txn() {
	for {
		tx := <-n.TxChan

		v := reflect.ValueOf(tx)
		name := v.Type().String()
		f, exists := n.handles[name]
		if !exists {
			log.Fatalf("no registered handle function for message type %v", name)
		}

		f.Call([]reflect.Value{v})
		n.totalTxn++
	}
}

// recv receives messages from socket and pass to message channel
func (n *node) recv() {
	for {
		m := n.Recv()
		if n.isByz && config.GetConfig().Strategy == "silence" {
			// perform silence attack
			continue
		}
		switch m := m.(type) {
		case *message.Transaction:
			n.TxChan <- m
			continue
		}
		n.MessageChan <- m
	}
}

// handle receives messages from message channel and calls handle function using refection
func (n *node) handle() {
	for {
		msg := <-n.MessageChan

		v := reflect.ValueOf(msg)

		if !v.IsValid() {
			log.Errorf("handler callee is invalid")
			continue
		}
		name := v.Type().String()
		f, exists := n.handles[name]
		if !exists {
			log.Fatalf("no registered handle function for message type %v", name)
		}
		f.Call([]reflect.Value{v})
	}
}

// handle receives messages from message channel and calls handle function using refection
func (n *node) handleCrossChain() {
	for {
		msg := <-n.CrossChainMessageChan
		v := reflect.ValueOf(msg)
		name := v.Type().String()
		f, exists := n.handles[name]
		if !exists {
			log.Fatalf("no registered handle function for message type %v", name)
		}
		f.Call([]reflect.Value{v})
	}
}

func (n *node) DistLogger() *dlog.DistributedLogger {
	return n.dlogger
}
