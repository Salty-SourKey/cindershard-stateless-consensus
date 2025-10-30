package blockbuilder

import (
	"encoding/json"
	"reflect"
	"strconv"
	"time"

	"unishard/blockchain"
	"unishard/config"
	"unishard/log"
	"unishard/message"
	"unishard/transport"
	"unishard/types"
	"unishard/utils"
	"unishard/worker"
)

type (
	WorkerBlockBuilder struct {
		worker.WorkerBlockBuilderReplica

		coordinationBuilderAddress        string
		coordinationBlockBuilderTransport transport.Transport
		consensusNodeTransport            map[types.NodeID]transport.Transport
		nodestart                         []types.NodeID
		sharedVariableList                string
	}
)

func NewInitWorkerBlockBuilder(ip string, shard types.Shard, coordinationBuilderAddress string, gatewayAddress string) *WorkerBlockBuilder {
	wbb := WorkerBlockBuilder{
		WorkerBlockBuilderReplica:         *worker.NewWorkerBlockBuilder(ip, shard, gatewayAddress),
		coordinationBuilderAddress:        coordinationBuilderAddress,
		coordinationBlockBuilderTransport: transport.NewTransport(coordinationBuilderAddress),
		consensusNodeTransport:            make(map[types.NodeID]transport.Transport),
		nodestart:                         make([]types.NodeID, 0),
		sharedVariableList:                "Alive",
	}

	return &wbb
}

func (wbb *WorkerBlockBuilder) Start() {
	go wbb.WorkerBlockBuilderReplica.Start()

	err := utils.Retry(wbb.coordinationBlockBuilderTransport.Dial, 100, time.Duration(50)*time.Millisecond)
	if err != nil {
		panic(err)
	}

	// wbb.coordinationBlockBuilderTransport.Send(message.WorkerBuilderRegister{
	// 	SenderShard: wbb.GetShard(),
	// 	Address:     wbb.GetIP(),
	// })
	workerBuilderRegister, _ := json.Marshal(message.WorkerBuilderRegister{
		SenderShard: wbb.GetShard(),
		Address:     wbb.GetIP(),
	})
	if err = wbb.Client.PutCoordinationBlockBuilder("/WorkerBuilderRegister", workerBuilderRegister); err != nil {
		panic(err)
	}
	log.Debugf("register to coordination builder")

	log.Debugf("start worker builder event loop")
	for {
		select {
		case msg := <-wbb.WorkerBlockBuilderReplica.WorkerBuilderTopic:
			if v, ok := msg.(message.WorkerSharedVariableRegisterRequest); ok {
				// t := transport.NewTransport(v.Gateway)
				// err := utils.Retry(t.Dial, 100, time.Duration(50)*time.Millisecond)
				// if err != nil {
				// 	panic(err)
				// }
				// t.Send(message.WorkerSharedVariableRegisterResponse{
				// 	SenderShard: wbb.GetShard(),
				// 	Variables:   wbb.sharedVariableList,
				// })
				if err = wbb.Client.GetGateway("/WorkerSharedVariableRegisterResponse?", "SenderShard="+strconv.Itoa(int(wbb.GetShard()))+"&Variables="+wbb.sharedVariableList); err != nil {
					panic(err)
				}
				log.Debugf("received request of my shared variable from Gateway %s", v.Gateway)
			} else if v, ok := msg.(*blockchain.WorkerConsensusPayload); ok {
				// Send builder message to consensus node
				for _, t := range wbb.consensusNodeTransport {
					t.Send(v)
				}
			} else if v, ok := msg.(message.Experiment); ok {
				wbb.GatewaynodeTransport.Send(v)
			} else {
				log.Debugf("illegal message type : %s", reflect.TypeOf(v).String())
			}

		case msg := <-wbb.WorkerBlockBuilderReplica.ConsensusNodeTopic:
			if v, ok := msg.(message.ConsensusNodeRegister); ok {
				t := transport.NewTransport(v.IP)
				err := utils.Retry(t.Dial, 100, time.Duration(50)*time.Millisecond)
				if err != nil {
					panic(err)
				}
				log.Debugf("Shard: %v, ID: %v, IP: %v Register Complete!!", wbb.GetShard(), v.ConsensusNodeID, v.IP)
				wbb.consensusNodeTransport[v.ConsensusNodeID] = t
				if len(wbb.consensusNodeTransport) == config.GetConfig().CommitteeNumber {
					wbb.GoFlag = true
					log.Debugf("Shard: %v, Start Client", wbb.GetShard())
					wbb.GatewaynodeTransport.Send(message.ClientStart{
						Shard: wbb.GetShard(),
					})
				}
			}

		case msg := <-wbb.WorkerBlockBuilderReplica.CoordinationBuilderTopic:
			if v, ok := msg.(blockchain.WorkerBlock); ok {
				// Send worker block to coordination shard

				// Simulate network delays (needed only for experiments in local network setting)
				// timer := config.GetBetweenShardTimer(wbb.GetShard())
				// <-timer.C

				// for _, ct := range v.BlockData.ReceivedCrossTransaction {
				// 	ct.LatencyDissection.WorkerConsensusTime = time.Now().UnixMilli()
				// }
				wbb.coordinationBlockBuilderTransport.Send(v)
				wbb.GatewaynodeTransport.Send(v)
				// worker -> coordination 브로드캐스트
				log.Debugf("[Broadcasted] send Worker block [%v]", v.BlockHeader.BlockHeight)

			}
		// drop another topic
		case <-wbb.GatewayTopic:
			continue
		}
	}
}
