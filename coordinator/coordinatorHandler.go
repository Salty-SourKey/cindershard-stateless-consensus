package coordinator

import (
	"encoding/gob"
	"encoding/json"
	"sync"
	"time"

	"unishard/blockchain"
	"unishard/config"
	"unishard/log"
	"unishard/message"
	"unishard/transport"
	"unishard/types"
	"unishard/utils"
)

type (
	CoordinationBlockBuilder struct {
		CoordBlockBuilderReplica
		WorkerBuilderList       map[types.Shard]string
		consensusNodeTransport  map[types.NodeID]transport.Transport
		workerBuilderTransport  map[types.Shard]transport.Transport
		checkSnapshotSafetyLock sync.Mutex
	}
)

func NewInitCoordinationBlockBuilder(ip string, shard types.Shard, gatewayAddress string) *CoordinationBlockBuilder {
	cbb := CoordinationBlockBuilder{
		CoordBlockBuilderReplica: *NewCoordinationBlockBuilderReplica(ip, shard, gatewayAddress),
		WorkerBuilderList:        make(map[types.Shard]string),
		workerBuilderTransport:   make(map[types.Shard]transport.Transport),
		consensusNodeTransport:   make(map[types.NodeID]transport.Transport),
		checkSnapshotSafetyLock:  sync.Mutex{},
	}

	/* Register to gob en/decoder */
	gob.Register(message.WorkerBuilderRegister{})
	gob.Register(message.WorkerBuilderListRequest{})
	gob.Register(message.ConsensusNodeRegister{})
	gob.Register(blockchain.WorkerBlock{})
	gob.Register(blockchain.CoordinationBlock{})
	gob.Register(blockchain.CoordinationBlockWithoutHeader{})
	gob.Register(message.ClientStart{})

	/* Register message handler */
	cbb.Register(message.WorkerBuilderRegister{}, cbb.handleWorkerBuilderRegister)
	cbb.Register(message.WorkerBuilderListRequest{}, cbb.handleWorkerBuilderListRequest)
	cbb.Register(message.ConsensusNodeRegister{}, cbb.handleConsensusNodeRegister)
	cbb.Register(blockchain.WorkerBlock{}, cbb.handleWorkerBlock)
	cbb.Register(blockchain.CoordinationBlock{}, cbb.handleCoordinationBlock)

	return &cbb
}

// Main loop executed by the coordinator shard
func (cbb *CoordinationBlockBuilder) Start() {
	cbb.dlogger.BootingSequence(1)
	go cbb.CoordBlockBuilderReplica.Run()
	err := utils.Retry(cbb.CoordBlockBuilderReplica.gatewaynodeTransport.Dial, 100, time.Duration(50)*time.Millisecond)
	if err != nil {
		panic(err)
	}

	// Generate a block every 1 seconds
	interval := time.NewTicker(time.Millisecond * 300)
	go func() {
		for range interval.C {
			if cbb.GoFlag {
				// Initiate BFT consensus on the generated block
				cbb.CoordBlockBuilderReplica.ProposeCoordinatorBlock()
			}
		}
	}()

	// loop for handling received messages
	for {
		select {
		case msg := <-cbb.WorkerBuilderTopic:
			if v, ok := msg.(blockchain.WorkerBlock); ok {
				// worker -> cooredinartion 수신
				log.CDebugf("[Received] received Worker block [%v] from shard [%v]", v.BlockHeader.BlockHeight, v.BlockHeader.ShardIdentifier)

				go func() {
					bm := cbb.dlogger.NewBenchmark("checkSnapshotSafetyLock").Start()
					json.Marshal(v)
					cbb.checkSnapshotSafetyLock.Lock()
					// Verify the safety of the local snapshot within the received worker block, and if it is safe, add the snapshot to the SCT
					// for _, ls := range v.BlockData.LocalSnapshot {
					// 	safe := cbb.CheckSnapshotSafety(v.BlockHeader.GC, ls)
					// 	if safe {
					// 		cbb.snapshotControlTable.UpdateSnapshot(
					// 			ls.Slot,
					// 			ls.Address,
					// 			ls.Value,
					// 			ls.RTCS,
					// 			v.BlockHeader.BlockHeight,
					// 			false,
					// 		)
					// 	}
					// }
					cbb.checkSnapshotSafetyLock.Unlock()
					// Store the cross-shard transactions and contract code contained in the received worker block
					// cbb.AccumulateCrossTransaction(v.BlockData.ReceivedCrossTransaction...)
					// cbb.AccumulateSmartContractBundle(v.BlockData.LocalContractBundle)
					bm.End()
				}()
			}
		case msg := <-cbb.CoordinationBuilderTopic:
			switch v := (msg).(type) {
			case message.WorkerBuilderRegister:
				log.Debugf("WorkerBuilderRegister: %v", v)
				cbb.WorkerBuilderList[v.SenderShard] = v.Address
				cbb.workerBuilderTransport[v.SenderShard] = transport.NewTransport(v.Address)
				err := utils.Retry(cbb.workerBuilderTransport[v.SenderShard].Dial, 100, time.Duration(50)*time.Millisecond)
				if err != nil {
					panic(err)
				}
			case message.WorkerBuilderListRequest:
				log.Debugf("WorkerBuilderListRequest received")
				WorkerBuilderList, _ := json.Marshal(message.WorkerBuilderListResponse{
					Builders: cbb.WorkerBuilderList,
				})
				if err := cbb.Client.PutGateway("/WorkerBuilderListResponse", WorkerBuilderList); err != nil {
					panic(err)
				}
			case blockchain.CoordinationBlock:
				for _, ct := range v.BlockData.GlobalCoordinationSequence {
					ct.LatencyDissection.CoordinationConsensusTime = time.Now().UnixMilli()
					cbb.dlogger.TransactionPath(ct, "cbb_consensus")
				}
				for _, t := range cbb.workerBuilderTransport {
					t.Send(v)
				}
				// coordination -> worker 송신
				log.CDebugf("[Broadcasted] send Coordination block [%v]", v.BlockHeader.BlockHeight)

			case blockchain.CoordinationBlockWithoutHeader:
				for _, t := range cbb.consensusNodeTransport {
					t.Send(v)
				}
			default:
			}

		case msg := <-cbb.CoordBlockBuilderReplica.ConsensusNodeTopic:
			if v, ok := (msg).(message.ConsensusNodeRegister); ok {
				log.Debugf("Shard: %v, ID: %v, IP: %v Register Complete!!", cbb.GetShard(), v.ConsensusNodeID, v.IP)
				t := transport.NewTransport(v.IP)
				err := utils.Retry(t.Dial, 100, time.Duration(50)*time.Millisecond)
				if err != nil {
					panic(err)
				}
				cbb.consensusNodeTransport[v.ConsensusNodeID] = t
				if len(cbb.consensusNodeTransport) == config.GetConfig().CommitteeNumber {
					cbb.GoFlag = true
					cbb.gatewaynodeTransport.Send(message.ClientStart{
						Shard: cbb.GetShard(),
					})
				}
			}
		case <-cbb.GatewayTopic:
			continue
		}
	}
}

func (r *CoordBlockBuilderReplica) handleWorkerBuilderRegister(msg message.WorkerBuilderRegister) {
	r.CoordinationBuilderTopic <- msg
}

func (r *CoordBlockBuilderReplica) handleWorkerBuilderListRequest(msg message.WorkerBuilderListRequest) {
	r.CoordinationBuilderTopic <- msg
}

func (r *CoordBlockBuilderReplica) handleConsensusNodeRegister(msg message.ConsensusNodeRegister) {
	r.ConsensusNodeTopic <- msg
}

func (r *CoordBlockBuilderReplica) handleWorkerBlock(msg blockchain.WorkerBlock) {
	// Measure for latency breakdown
	// for _, ct := range msg.BlockData.ReceivedCrossTransaction {
	// 	ct.LatencyDissection.Network1 = time.Now().UnixMilli()
	// 	r.dlogger.TransactionPath(ct, "cbb_receive_wb")
	// }
	r.WorkerBuilderTopic <- msg
}

func (r *CoordBlockBuilderReplica) handleCoordinationBlock(msg blockchain.CoordinationBlock) {
	r.GetBlockChain().AddCoordinationBlock(&msg)
	r.CoordinationBuilderTopic <- msg
}
