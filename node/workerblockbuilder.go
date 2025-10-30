package node

import (
	"net/http"
	"reflect"
	"sync"

	"unishard/blockchain"
	"unishard/config"
	"unishard/log"
	"unishard/mempool"
	"unishard/message"
	"unishard/socket"
	"unishard/types"
	"unishard/utils"

	"github.com/ethereum/go-ethereum/common"
)

// Node is the primary access point for every replica
// it includes networking, state machine and RESTful API server
type BlockBuilder interface {
	socket.BBSocket
	//Database
	GetIP() string
	GetShard() types.Shard
	AddLocalTransaction(lt *message.Transaction)
	GetRemainingLocalTx() int
	AddNewCrossTransaction(ct *message.Transaction)
	AddGlobalCoordinationSequence(ct *message.Transaction)
	AddGlobalSnapshot(gs *blockchain.LocalSnapshot)
	AddGlobalContractBundle(gc *blockchain.LocalContract)
	GetRemainingGlobalCoordinationSequence() []*message.Transaction
	GeneratePayload() *blockchain.TxData
	GetBlockHeight() types.BlockHeight
	SetBlockHeight(blockheight types.BlockHeight)
	GetGC() types.BlockHeight
	SetGC(blockheight types.BlockHeight)
	Run()
	Retry(r message.Transaction)
	Register(m interface{}, f interface{})
}

// node implements Node interface
type blockbuilder struct {
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

	LocalMempool                 *mempool.Producer
	CrossMempool                 *mempool.Producer
	GlobalCoordinationSequence   []*message.Transaction
	GlobalSnapshot               []*blockchain.LocalSnapshot
	GlobalContractBundle         []*blockchain.LocalContract
	fdcm                         []*common.Address
	currentBlockHeight           types.BlockHeight
	latestCoordinatorBlockHeight types.BlockHeight
	txcnt                        int
}

type LocalSequence struct {
	CrossTransactions []*message.Transaction
	LocalTransactions []*message.Transaction
}

// NewNode creates a new Node object from configuration
func NewWorkerBlockBuilder(ip string, shard types.Shard) BlockBuilder {
	bb := new(blockbuilder)
	bb.ip = ip
	bb.shard = shard
	bb.BBSocket = socket.NewBBSocket(ip, shard, config.Configuration.Addrs)
	bb.handles = make(map[string]reflect.Value)
	bb.MessageChan = make(chan interface{}, config.Configuration.ChanBufferSize)
	bb.LocalMempool = mempool.NewProducer() // Local Transaction Mempool from Gateway
	bb.CrossMempool = mempool.NewProducer() // New Cross Transaction Mempool from Gateway
	bb.GlobalCoordinationSequence = make([]*message.Transaction, 0)
	bb.GlobalSnapshot = make([]*blockchain.LocalSnapshot, 0)
	bb.GlobalContractBundle = make([]*blockchain.LocalContract, 0)
	bb.fdcm = nil
	bb.currentBlockHeight = 0
	bb.latestCoordinatorBlockHeight = 0
	bb.txcnt = 0

	return bb
}

/* Function */
// Todo: create block at reach certain transaction count

func (bb *blockbuilder) GetIP() string {
	return bb.ip
}

func (bb *blockbuilder) GetShard() types.Shard {
	return bb.shard
}

func (bb *blockbuilder) AddLocalTransaction(lt *message.Transaction) {
	bb.LocalMempool.AddTxn(lt)
}

func (bb *blockbuilder) GetRemainingLocalTx() int {
	return bb.LocalMempool.Size()
}

func (bb *blockbuilder) AddNewCrossTransaction(ct *message.Transaction) {
	bb.CrossMempool.AddTxn(ct)
}

func (bb *blockbuilder) AddGlobalCoordinationSequence(ct *message.Transaction) {
	bb.GlobalCoordinationSequence = append(bb.GlobalCoordinationSequence, ct)
}

func (bb *blockbuilder) AddGlobalSnapshot(gs *blockchain.LocalSnapshot) {
	bb.GlobalSnapshot = append(bb.GlobalSnapshot, gs)
}

func (bb *blockbuilder) AddGlobalContractBundle(gc *blockchain.LocalContract) {
	bb.GlobalContractBundle = append(bb.GlobalContractBundle, gc)
}

func (bb *blockbuilder) GetRemainingGlobalCoordinationSequence() []*message.Transaction {
	return bb.GlobalCoordinationSequence
}

func (bb *blockbuilder) GetBlockHeight() types.BlockHeight {
	return bb.currentBlockHeight
}

func (bb *blockbuilder) SetBlockHeight(blockheight types.BlockHeight) {
	bb.currentBlockHeight = blockheight
}

func (bb *blockbuilder) GetGC() types.BlockHeight {
	return bb.latestCoordinatorBlockHeight
}

func (bb *blockbuilder) SetGC(blockheight types.BlockHeight) {
	bb.latestCoordinatorBlockHeight = blockheight
}

func (bb *blockbuilder) GeneratePayload() *blockchain.TxData {
	globalCoordinationSequence := make([]*message.Transaction, 0)
	globalSnapshot := make([]*blockchain.LocalSnapshot, 0)
	globalContractBundle := make([]*blockchain.LocalContract, 0)

	globalCoordinationSequence = append(globalCoordinationSequence, bb.GlobalCoordinationSequence...)
	if len(bb.GlobalCoordinationSequence) > len(globalCoordinationSequence) {
		bb.GlobalCoordinationSequence = bb.GlobalCoordinationSequence[len(globalCoordinationSequence):]
	} else {
		bb.GlobalCoordinationSequence = []*message.Transaction{}
	}

	globalSnapshot = append(globalSnapshot, bb.GlobalSnapshot...)
	if len(bb.GlobalSnapshot) > len(globalSnapshot) {
		bb.GlobalSnapshot = bb.GlobalSnapshot[len(globalSnapshot):]
	} else {
		bb.GlobalSnapshot = []*blockchain.LocalSnapshot{}
	}

	globalContractBundle = append(globalContractBundle, bb.GlobalContractBundle...)
	if len(bb.GlobalContractBundle) > len(globalContractBundle) {
		bb.GlobalContractBundle = bb.GlobalContractBundle[len(globalContractBundle):]
	} else {
		bb.GlobalContractBundle = []*blockchain.LocalContract{}
	}

	payload := &blockchain.TxData{
		GlobalCoordinationSequence: globalCoordinationSequence,
		ReceivedLocalTransaction:   bb.LocalMempool.GeneratePayload(1024),
		ReceivedCrossTransaction:   bb.CrossMempool.GeneratePayload(1024),
		GlobalSnapshot:             globalSnapshot,
		GlobalContractBundle:       globalContractBundle,
		GC:                         bb.GetGC(),
	}

	return payload
}

func (bb *blockbuilder) CheckLocalCommit(transaction *message.Transaction) bool {
	if transaction.TXType == types.SMARTCONTRACT {
		for _, rwSet := range transaction.RwSet {
			rwSetShard := utils.CalculateShardToSend([]common.Address{rwSet.Address}, config.GetConfig().ShardCount)[0]
			if rwSetShard == bb.shard && len(rwSet.WriteSet) > 0 {
				return true
			}
		}
		fromShard := utils.CalculateShardToSend([]common.Address{transaction.From}, config.GetConfig().ShardCount)[0]
		return fromShard == bb.shard
	} else if transaction.TXType == types.TRANSFER {
		fromShard := utils.CalculateShardToSend([]common.Address{transaction.From}, config.GetConfig().ShardCount)[0]
		toShard := utils.CalculateShardToSend([]common.Address{transaction.To}, config.GetConfig().ShardCount)[0]
		if fromShard == bb.shard || toShard == bb.shard {
			return true
		}
		return false
	}
	return false
}

func (bb *blockbuilder) Retry(r message.Transaction) {
	log.Debugf("blockbuilder %v retry reqeust %v", bb.shard, r)
	bb.TxChan <- r
}

// Register a handle function for each message type
func (bb *blockbuilder) Register(m interface{}, f interface{}) {
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

	bb.handles[t.String()] = fn
}

// Run start and run the node
func (bb *blockbuilder) Run() {
	log.Infof("BlockBuilder %v, %v start running", bb.ip, bb.shard)

	go bb.handle()
	go bb.recv()
	bb.http()
}

// handle receives messages from message channel and calls handle function using refection
func (bb *blockbuilder) handle() {
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
func (bb *blockbuilder) recv() {
	for {
		m := bb.Recv()
		// if config.GetConfig().Strategy == "silence" {
		// 	// perform silence attack
		// 	continue
		// }
		bb.MessageChan <- m
	}
}
