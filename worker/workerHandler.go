package worker

import (
	"encoding/gob"
	"strconv"
	"sync"
	"time"

	"unishard/blockchain"
	"unishard/dlog"
	"unishard/httpclient"
	"unishard/log"
	"unishard/message"
	"unishard/node"
	"unishard/transport"
	"unishard/types"
	"unishard/utils"
)

type (
	WorkerBlockBuilderReplica struct {
		node.BlockBuilder
		httpclient.Client
		GatewaynodeTransport     transport.Transport
		CoordinationBuilderTopic chan interface{}
		WorkerBuilderTopic       chan interface{}
		GatewayTopic             chan interface{}
		ConsensusNodeTopic       chan interface{}
		BufferedCoordinatorBlock map[types.BlockHeight]*blockchain.CoordinationBlock
		CoordinationMu           sync.Mutex
		GoFlag                   bool
		RemainingTxNum           int
		dlogger                  *dlog.DistributedLogger
	}
)

// The block proposer of the worker shard
func NewWorkerBlockBuilder(ip string, shard types.Shard, gatewayAddress string) *WorkerBlockBuilderReplica {
	r := new(WorkerBlockBuilderReplica)
	r.dlogger = dlog.NewDistributedLogger("nats://127.0.0.1:4222", "wbb", ip, strconv.Itoa(int(shard)))
	r.dlogger.BootingSequence(0)
	r.GatewaynodeTransport = transport.NewTransport(gatewayAddress)
	r.Client = httpclient.NewHTTPClient()
	r.BlockBuilder = node.NewWorkerBlockBuilder(ip, shard)
	r.CoordinationBuilderTopic = make(chan interface{}, 128)
	r.WorkerBuilderTopic = make(chan interface{}, 128)
	r.GatewayTopic = make(chan interface{}, 128)
	r.ConsensusNodeTopic = make(chan interface{}, 128)
	r.BufferedCoordinatorBlock = make(map[types.BlockHeight]*blockchain.CoordinationBlock)
	r.CoordinationMu = sync.Mutex{}
	r.GoFlag = false
	r.RemainingTxNum = 0

	/* Register to gob en/decoder */
	gob.Register(message.WorkerBuilderRegister{})
	gob.Register(message.WorkerBuilderListRequest{})
	gob.Register(message.WorkerBuilderListResponse{})
	gob.Register(message.WorkerSharedVariableRegisterRequest{})
	gob.Register(message.WorkerSharedVariableRegisterResponse{})
	gob.Register(message.ConsensusNodeRegister{})
	gob.Register(message.Transaction{})
	gob.Register(blockchain.WorkerBlock{})
	gob.Register(message.Experiment{})
	gob.Register(blockchain.CoordinationBlock{})
	gob.Register(blockchain.WorkerConsensusPayload{})
	gob.Register(message.ClientStart{})

	/* Register message handler */
	r.Register(message.WorkerBuilderRegister{}, r.handleWorkerBuilderRegister)
	r.Register(message.WorkerBuilderListRequest{}, r.handleWorkerBuilderListRequest)
	r.Register(message.WorkerBuilderListResponse{}, r.handleWorkerBuilderListResponse)
	r.Register(message.WorkerSharedVariableRegisterRequest{}, r.handleWorkerShardVariableRegisterRequest)
	r.Register(message.WorkerSharedVariableRegisterResponse{}, r.handleWorkerShardVariableRegisterResponse)
	r.Register(message.ConsensusNodeRegister{}, r.handleConsensusRegister)
	r.Register(message.Transaction{}, r.handleTransaction)
	r.Register(blockchain.WorkerBlock{}, r.handleWorkerBlock)
	r.Register(message.Experiment{}, r.handleExperiment)
	r.Register(blockchain.CoordinationBlock{}, r.handleCoordinationBlock)

	return r
}

// Main loop executed by the worker shard
func (r *WorkerBlockBuilderReplica) Start() {
	r.dlogger.BootingSequence(1)
	go r.Run()
	err := utils.Retry(r.GatewaynodeTransport.Dial, 100, time.Duration(50)*time.Millisecond)
	if err != nil {
		panic(err)
	}

	// Generate a block every 1 seconds or when the number of transactions reaches 1,000 or more
	mu := sync.Mutex{}
	interval := time.NewTicker(time.Millisecond * 1000)
	for {
		if r.GoFlag {
			select {
			case <-interval.C:
				mu.Lock()
				r.StartWorkerConsensus()
				mu.Unlock()
			default:
				if mu.TryLock() {
					if r.RemainingTxNum >= 1000 {
						r.StartWorkerConsensus()
						interval.Reset(time.Millisecond * 1000)
						mu.Unlock()
					} else {
						mu.Unlock()
					}
				} else {
					r.RemainingTxNum = r.GetRemainingLocalTx() + len(r.GetRemainingGlobalCoordinationSequence())
				}
			}
		}
	}
}

// Build a worker block and initiate BFT consensus
func (r *WorkerBlockBuilderReplica) StartWorkerConsensus() {
	r.CoordinationMu.Lock()
	defer r.CoordinationMu.Unlock()
	transactionBatch := new(blockchain.WorkerConsensusPayload)

	// Generate a worker block, called "workerBlockPayolad",
	// using the received global coordination sequence, global snapshot,
	// global contract bundle, received local transactions, and received cross-shard transactions
	workerBlockPayload := r.GeneratePayload()
	transactionBatch.Data = workerBlockPayload

	r.WorkerBuilderTopic <- transactionBatch
	r.RemainingTxNum = r.GetRemainingLocalTx() + len(r.GetRemainingGlobalCoordinationSequence())
}

// Upon receiving a transaction request from a client,
// Add the received local transactions and cross-shard transactions to the mempool
func (r *WorkerBlockBuilderReplica) handleTransaction(tx message.Transaction) {
	if !tx.IsCrossShardTx {
		r.AddLocalTransaction(&tx)
	} else {
		r.AddNewCrossTransaction(&tx)
	}
}

// Upon receiving a coordinator block from the coordinator shard,
// Process the received coordinator block
func (r *WorkerBlockBuilderReplica) handleCoordinationBlock(msg blockchain.CoordinationBlock) {
	// Simulate network delays (needed only for experiments in local network setting)
	// timer := config.GetBetweenShardTimer(r.GetShard())
	// <-timer.C

	log.Debugf("[Received] received Coordination block [%v]", msg.BlockHeader.BlockHeight)

	// Measure for latency breakdown
	for _, ct := range msg.BlockData.GlobalCoordinationSequence {
		if ct.LatencyDissection.Network2 == 0 {
			ct.LatencyDissection.Network2 = time.Now().UnixMilli()
			r.dlogger.TransactionPath(ct, "wbb_received")
		}
	}
	r.CoordinationMu.Lock()
	defer r.CoordinationMu.Unlock()

	// If the height of the received coordinator block equals GC+1, proceed with the execution phase using the received coordinator block
	if r.GetGC()+1 == msg.BlockHeader.BlockHeight {
		// Wait with the global coordination sequence, global snapshot, and global contract bundle within the coordinator block
		// until block consensus begins
		for _, ct := range msg.BlockData.GlobalCoordinationSequence {
			r.AddGlobalCoordinationSequence(ct)
		}
		for _, gs := range msg.BlockData.GlobalSnapshot {
			r.AddGlobalSnapshot(gs)
		}
		for _, gc := range msg.BlockData.GlobalContractBundle {
			r.AddGlobalContractBundle(gc)
		}

		// Remove the coordinator block from the buffer if it exists
		if r.BufferedCoordinatorBlock[msg.BlockHeader.BlockHeight+1] != nil {
			r.handleCoordinationBlock(*r.BufferedCoordinatorBlock[msg.BlockHeader.BlockHeight+1])
			delete(r.BufferedCoordinatorBlock, msg.BlockHeader.BlockHeight+1)
		}

		// Set GC to the height of the received coordinator block
		r.SetGC(msg.BlockHeader.BlockHeight)

	} else {
		// If the height of the received coordinator block is not equal to GC+1, store it in the buffer
		r.BufferedCoordinatorBlock[msg.BlockHeader.BlockHeight] = &msg
	}
}

func (r *WorkerBlockBuilderReplica) handleWorkerBlock(msg blockchain.WorkerBlock) {
	r.SetBlockHeight(r.GetBlockHeight() + 1)
	r.CoordinationBuilderTopic <- msg
}

func (r *WorkerBlockBuilderReplica) handleWorkerBuilderRegister(msg message.WorkerBuilderRegister) {
	r.CoordinationBuilderTopic <- msg
}

func (r *WorkerBlockBuilderReplica) handleWorkerBuilderListRequest(msg message.WorkerBuilderListRequest) {
	r.CoordinationBuilderTopic <- msg
}

func (r *WorkerBlockBuilderReplica) handleWorkerBuilderListResponse(msg message.WorkerBuilderListResponse) {
	r.GatewayTopic <- msg
}

func (r *WorkerBlockBuilderReplica) handleWorkerShardVariableRegisterRequest(msg message.WorkerSharedVariableRegisterRequest) {
	r.WorkerBuilderTopic <- msg
}

func (r *WorkerBlockBuilderReplica) handleWorkerShardVariableRegisterResponse(msg message.WorkerSharedVariableRegisterResponse) {
	r.GatewayTopic <- msg
}

func (r *WorkerBlockBuilderReplica) handleConsensusRegister(msg message.ConsensusNodeRegister) {
	r.ConsensusNodeTopic <- msg
}

func (r *WorkerBlockBuilderReplica) handleExperiment(msg message.Experiment) {
	r.WorkerBuilderTopic <- msg
}
