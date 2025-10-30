package node

import (
	"net/http"
	"reflect"
	"sync"

	"unishard/blockchain"
	"unishard/config"
	"unishard/crypto"
	"unishard/log"
	"unishard/mempool"
	"unishard/message"
	"unishard/socket"
	"unishard/types"
)

// Node is the primary access point for every replica
// it includes networking, state machine and RESTful API server
type CoordBlockBuilder interface {
	socket.BBSocket
	//Database
	GetIP() string
	GetShard() types.Shard
	GetBlockChain() *blockchain.BlockChain
	AddLocalTransaction(lt *message.Transaction)
	AddNewCrossTransaction(ct *message.Transaction)
	AddSendedCrossTransaction(ct *message.Transaction)
	CreateCoordinationBlockWithoutHeader(blockdata *blockchain.CoordinationBlockData) *blockchain.CoordinationBlockWithoutHeader
	Run()
	Retry(r message.Transaction)
	Register(m interface{}, f interface{})
}

// node implements Node interface
type coordblockbuilder struct {
	ip    string
	shard types.Shard

	socket.BBSocket
	server *http.Server

	//Database
	handles     map[string]reflect.Value
	MessageChan chan interface{}
	TxChan      chan interface{}
	BlockChan   chan interface{}

	sync.RWMutex

	bc    *blockchain.BlockChain
	ltmp  *mempool.Producer
	nctmp *mempool.Producer
	sctmp *mempool.Producer
	srtmp *mempool.Producer
	txcnt int
}

// NewNode creates a new Node object from configuration
func NewCoordinationBlockBuilder(ip string, shard types.Shard) CoordBlockBuilder {
	bb := new(coordblockbuilder)
	bb.ip = ip
	bb.shard = shard
	bb.BBSocket = socket.NewBBSocket(ip, shard, config.Configuration.Addrs)
	bb.handles = make(map[string]reflect.Value)
	bb.MessageChan = make(chan interface{}, config.Configuration.ChanBufferSize)
	bb.bc = blockchain.NewCoordinationBlockchain(bb.shard, config.GetConfig().DefaultBalance)
	bb.ltmp = mempool.NewProducer()  // Local Transaction Mempool
	bb.nctmp = mempool.NewProducer() // New Cross Transaction Mempool from Gateway
	bb.sctmp = mempool.NewProducer() // Cross Transaction Mempool sended to coordination chain
	bb.srtmp = mempool.NewProducer() // Local Sequence Ready Mempool by cross transaction from coordination block
	bb.txcnt = 0

	return bb
}

/* Function */
// Todo: create block at reach certain transaction count

func (bb *coordblockbuilder) GetIP() string {
	return bb.ip
}

func (bb *coordblockbuilder) GetShard() types.Shard {
	return bb.shard
}

func (bb *coordblockbuilder) GetBlockChain() *blockchain.BlockChain {
	return bb.bc
}

func (bb *coordblockbuilder) AddLocalTransaction(lt *message.Transaction) {
	bb.ltmp.AddTxn(lt)
}

func (bb *coordblockbuilder) AddNewCrossTransaction(ct *message.Transaction) {
	bb.nctmp.AddTxn(ct)
}

func (bb *coordblockbuilder) AddSendedCrossTransaction(ct *message.Transaction) {
	bb.sctmp.AddTxn(ct)
}

func (bb *coordblockbuilder) AddSequenceReadyCrossTransaction(srt *message.Transaction) {
	bb.srtmp.AddTxn(srt)
}

func (bb *coordblockbuilder) CreateCoordinationBlockWithoutHeader(blockdata *blockchain.CoordinationBlockData) *blockchain.CoordinationBlockWithoutHeader {
	coordinationBlockWithoutheader := new(blockchain.CoordinationBlockWithoutHeader)
	coordinationBlockWithoutheader.CoordinationBlockData = blockdata
	builderSignature, _ := crypto.PrivSign(crypto.IDToByte(coordinationBlockWithoutheader.MakeHash(blockdata)), nil)
	coordinationBlockWithoutheader.CommitteeSignature = append(coordinationBlockWithoutheader.CommitteeSignature, builderSignature)

	return coordinationBlockWithoutheader
}

func (bb *coordblockbuilder) Retry(r message.Transaction) {
	log.CDebugf("blockbuilder %v retry reqeust %v", bb.shard, r)
	bb.TxChan <- r
}

// Register a handle function for each message type
func (bb *coordblockbuilder) Register(m interface{}, f interface{}) {
	t := reflect.TypeOf(m)
	// log.Errorf("%v", t)
	fn := reflect.ValueOf(f)

	if fn.Kind() != reflect.Func {
		panic("handle function is not func")
	}

	if fn.Type().In(0) != t {
		log.Errorf("%v %v", fn.Type().In(0), t)
		panic("func type is not t")
	}

	if fn.Kind() != reflect.Func || fn.Type().NumIn() != 1 || fn.Type().In(0) != t {
		panic("register handle function error")
	}

	bb.handles[t.String()] = fn
}

// Run start and run the node
func (bb *coordblockbuilder) Run() {
	log.CInfof("BlockBuilder %v, %v start running", bb.ip, bb.shard)

	go bb.handle()
	go bb.recv()
	bb.http()
}

// handle receives messages from message channel and calls handle function using refection
func (bb *coordblockbuilder) handle() {
	for {
		msg := <-bb.MessageChan
		v := reflect.ValueOf(msg)
		name := v.Type().String()
		f, exists := bb.handles[name]
		if !exists {
			log.Fatalf("no registered handle function for message type %v", name)
		}
		f.Call([]reflect.Value{v})
	}
}

// recv receives messages from socket and pass to message channel
func (bb *coordblockbuilder) recv() {
	for {
		m := bb.Recv()
		bb.MessageChan <- m
	}
}
