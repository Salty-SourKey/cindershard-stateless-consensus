package node

import (
	"net/http"
	"reflect"
	"sync"

	"unishard/config"
	"unishard/log"
	"unishard/socket"
	"unishard/types"
)

// Node is the primary access point for every replica
// it includes networking, state machine and RESTful API server
type GatewayNode interface {
	socket.BBSocket
	//Database
	GetIP() string
	GetShard() types.Shard
	Run()
	Register(m interface{}, f interface{})
}

// node implements Node interface
type gatewaynode struct {
	ip    string
	shard types.Shard

	socket.BBSocket
	server *http.Server

	//Database
	handles     map[string]reflect.Value
	MessageChan chan interface{}

	sync.RWMutex
}

// NewNode creates a new Node object from configuration
func NewGatewayNode(ip string, shard types.Shard) GatewayNode {
	gn := new(gatewaynode)
	gn.ip = ip
	gn.shard = shard
	gn.BBSocket = socket.NewBBSocket(ip, shard, config.Configuration.Addrs)
	gn.handles = make(map[string]reflect.Value)
	gn.MessageChan = make(chan interface{}, config.Configuration.ChanBufferSize)
	return gn
}

func (gn *gatewaynode) GetIP() string {
	return gn.ip
}

func (gn *gatewaynode) GetShard() types.Shard {
	return gn.shard
}

// Register a handle function for each message type
func (gn *gatewaynode) Register(m interface{}, f interface{}) {
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

	gn.handles[t.String()] = fn
}

// Run start and run the node
func (gn *gatewaynode) Run() {
	// log.GInfof("Gateway %v, %v start running", gn.ip, gn.shard)

	go gn.handle()
	go gn.recv()
	gn.http()
}

// handle receives messages from message channel and calls handle function using refection
func (gn *gatewaynode) handle() {
	for {
		msg := <-gn.MessageChan

		v := reflect.ValueOf(msg)
		if !v.IsValid() {
			//log.Errorf("handler callee is invalid")
			continue
		}

		name := v.Type().String()
		f, exists := gn.handles[name]

		if !exists {
			log.Fatalf("no registered handle function for message type %v", name)
		}
		f.Call([]reflect.Value{v})
	}
}

// recv receives messages from socket and pass to message channel
func (gn *gatewaynode) recv() {
	for {
		m := gn.Recv()
		gn.MessageChan <- m
	}
}
