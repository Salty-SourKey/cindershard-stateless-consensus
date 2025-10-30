package blockbuilder

import (
	"encoding/gob"
	"encoding/hex"
	"os/exec"
	"strconv"
	"time"
	"unishard/blockchain"
	"unishard/config"
	"unishard/dlog"
	"unishard/httpclient"
	"unishard/log"
	"unishard/message"
	"unishard/node"
	"unishard/transport"
	"unishard/types"
	"unishard/utils"

	"github.com/ethereum/go-ethereum/common"
)

type (
	Gateway struct {
		node.GatewayNode
		httpclient.Client
		CoordinationBuilderTopic   chan interface{}
		WorkerBuilderTopic         chan interface{}
		GatewayTopic               chan interface{}
		workerBuilderList          map[types.Shard]string
		workerBuilderTransports    map[types.Shard]transport.Transport
		workerSharedVariableList   map[types.Shard][]string
		startBlockHeight           map[types.Shard]types.BlockHeight
		startTime                  map[types.Shard]time.Time
		epochDuration              map[types.Shard]float64
		committedTransactionNumber map[types.Shard]int
		localTransactionLatency    map[types.Shard]map[common.Hash]int64
		crossTransactionLatency    map[common.Hash]int64
		sendedTransactionList      map[types.Shard]map[int][]common.Hash
		clientStart                []types.Shard
		clientEnd                  map[types.Shard]bool
		contractRwSet              map[common.Address]map[string]types.RwSet
		dlogger                    *dlog.DistributedLogger
	}
)

func NewInitGateway(ip string, shard types.Shard, coordinationBuilderAddress string) *Gateway {
	gw := new(Gateway)
	gw.dlogger = dlog.NewDistributedLogger("nats://127.0.0.1:4222", "gw", ip, strconv.Itoa(int(shard)))
	gw.dlogger.BootingSequence(0)
	gw.Client = httpclient.NewHTTPClient()
	gw.GatewayNode = node.NewGatewayNode(ip, shard)
	gw.CoordinationBuilderTopic = make(chan interface{}, 128)
	gw.WorkerBuilderTopic = make(chan interface{}, 128)
	gw.GatewayTopic = make(chan interface{}, 128)
	gw.workerBuilderList = make(map[types.Shard]string)
	gw.workerBuilderTransports = make(map[types.Shard]transport.Transport)
	gw.workerSharedVariableList = make(map[types.Shard][]string)
	gw.startBlockHeight = make(map[types.Shard]types.BlockHeight)
	gw.startTime = make(map[types.Shard]time.Time)
	gw.epochDuration = make(map[types.Shard]float64)
	gw.committedTransactionNumber = make(map[types.Shard]int)
	gw.localTransactionLatency = make(map[types.Shard]map[common.Hash]int64)
	for i := 1; i <= config.GetConfig().ShardCount; i++ {
		gw.localTransactionLatency[types.Shard(i)] = make(map[common.Hash]int64)
	}
	gw.crossTransactionLatency = make(map[common.Hash]int64)
	gw.clientStart = make([]types.Shard, 0)
	gw.clientEnd = make(map[types.Shard]bool)
	gw.contractRwSet = make(map[common.Address]map[string]types.RwSet)
	gw.sendedTransactionList = make(map[types.Shard]map[int][]common.Hash)
	for i := 1; i <= config.GetConfig().ShardCount; i++ {
		gw.sendedTransactionList[types.Shard(i)] = make(map[int][]common.Hash)
		gw.sendedTransactionList[types.Shard(i)][0] = make([]common.Hash, 0)
		gw.sendedTransactionList[types.Shard(i)][1] = make([]common.Hash, 0)
	}
	/* Register to gob en/decoder */
	gob.Register(message.WorkerBuilderListRequest{})
	gob.Register(message.WorkerSharedVariableRegisterResponse{})
	gob.Register(message.ClientStart{})
	gob.Register(message.TransactionForm{})
	gob.Register(message.Transaction{})
	gob.Register(blockchain.WorkerBlock{})
	gob.Register(message.Experiment{})

	/* Register message handler */
	gw.Register(message.WorkerBuilderListResponse{}, gw.handleWorkerBuilderListResponse)
	gw.Register(message.WorkerSharedVariableRegisterResponse{}, gw.handleWorkerShardVariableRegisterResponse)
	gw.Register(message.ClientStart{}, gw.handleClientStart)
	gw.Register(message.TransactionForm{}, gw.handleTransactionForm)
	gw.Register(blockchain.WorkerBlock{}, gw.handleWorkerBlock)
	gw.Register(message.Experiment{}, gw.handleExperiment)
	gw.Register(message.Transaction{}, gw.handleTransaction)

	return gw
}

func (gw *Gateway) Start() {
	gw.dlogger.BootingSequence(1)
	go gw.GatewayNode.Run()

	trainCAs, hotelCAs, travelCAs := config.GetCA()

	trainRwSet := config.GetRwSet("train")
	hotelRwSet := config.GetRwSet("hotel")
	travelRwSet := config.GetRwSet("travel")

	for _, ca := range trainCAs {
		gw.contractRwSet[ca[0]] = trainRwSet
	}
	for _, ca := range hotelCAs {
		gw.contractRwSet[ca[0]] = hotelRwSet
	}
	for _, ca := range travelCAs {
		gw.contractRwSet[ca[0]] = travelRwSet
	}

	endFlag := false
	for !endFlag {
		cnt := 0
		oneLocal, oneCross, twoLocal, twoCross, threeLocal, fourLocal, threeCross, fourCross, totalCross := 0, 0, 0, 0, 0, 0, 0, 0, 0
		for {
			// if every cleintEnd is true
			for i := 1; i <= config.GetConfig().ShardCount; i++ {
				if _, exist := gw.clientEnd[types.Shard(i)]; !exist {
					endFlag = false
					break
				}
				if gw.clientEnd[types.Shard(i)] {
					endFlag = true
				} else {
					endFlag = false
					break
				}
			}
			if endFlag {
				cmd := exec.Command("sh", "-c", "kill -9 $(pgrep client)")
				cmd.Run()
				log.GDebugf("stop client")
				gw.calculateExperimentResult()
				break
			}
			select {
			case msg := <-gw.GatewayTopic:
				switch v := (msg).(type) {
				case message.ClientStart:
					gw.clientStart = append(gw.clientStart, v.Shard)
					log.Debugf("[Gateway] Receive ClientStart Message From Shard %v", v.Shard)
					if len(gw.clientStart) == config.GetConfig().ShardCount+1 {
						cmd := exec.Command("sh", "-c", "./client &")
						if err := cmd.Run(); err != nil {
							log.Error("[Gateway] Error executing Client!!!")
						} else {
							log.Debug("[Gateway] Execute Client Success!!!")
						}
					}

				case message.TransactionForm:
					if gw.workerBuilderTransports[types.Shard(1)] == nil {
						str_addr := config.GetConfig().Addrs[types.Shard(1)][utils.NewNodeID(1)] + "4100"
						log.GDebugf("[Gateway] Leader TCP address: %v", str_addr)
						t := transport.NewTransport(str_addr)

						if err := t.Dial(); err != nil {
							panic(err) // TODO: eliminate panic
						}
						log.GDebugf("[Gateway] Gateway transport address: %v", str_addr)
						gw.workerBuilderTransports[types.Shard(1)] = t
					}
					switch {
					case len(v.Data) > 0:
						if v.To.String() == "0x0000000000000000000000000000000000000000" {
							// Handle Contract Deployment
						} else {
							// Handle Smart Contract Transaction
							shardToSend := utils.CalculateShardToSend(append([]common.Address{v.From, v.To}, v.ExternalAddressList...), config.GetConfig().ShardCount)
							isCrossShardTx := len(shardToSend) > 1
							name := hex.EncodeToString(v.Data[:4])
							rwSet := make([]message.RwVariable, 0)
							idx := 0
							rwSet = append(rwSet, message.RwVariable{Address: v.To, Name: name, RwSet: gw.contractRwSet[v.To][name]})
							// Handle Nested External Set
							for _, externalSet := range rwSet[0].ExternalSet {
								externalRwSet, curIdx := gw.getExternalRwSet(idx, v.ExternalAddressList, []types.ExternalElem{externalSet})
								idx = curIdx
								rwSet = append(rwSet, externalRwSet...)
							}
							for i := 0; i < len(rwSet); i++ {
								switch rwSet[i].Name {
								case "c744f4aa":
									// update rwSet for Travel contract bookTrainAndHotel function
									readSet, writeSet := gw.updateSlotForMapping(rwSet[i].RwSet.ReadSet, rwSet[i].RwSet.WriteSet, v.MappingIdx, "0")
									rwSet[i].RwSet.ReadSet = readSet
									rwSet[i].RwSet.WriteSet = writeSet
								case "0332a324":
									// update rwSet for Payment contract verifyDeposit function
									readSet, writeSet := gw.updateSlotForMapping(rwSet[i].RwSet.ReadSet, rwSet[i].RwSet.WriteSet, v.MappingIdx, "0")
									rwSet[i].RwSet.ReadSet = readSet
									rwSet[i].RwSet.WriteSet = writeSet
								case "5259b9ab":
									// update rwSet for Payment contract receivePayment function
									caMappingIndex := utils.CalculateMappingSlotIndex(v.ExternalAddressList[i-2], 0)
									readSet, writeSet := gw.updateSlotForMapping(rwSet[i].RwSet.ReadSet, rwSet[i].RwSet.WriteSet, v.MappingIdx, "0")
									writeSet = append(writeSet, hex.EncodeToString(caMappingIndex))
									rwSet[i].RwSet.ReadSet = readSet
									rwSet[i].RwSet.WriteSet = writeSet
								}
							}

							tx := message.MakeSmartContractTransaction(v, rwSet, isCrossShardTx, utils.NewNodeID(0))
							cnt++
							if len(shardToSend) == 1 {
								switch shardToSend[0] {
								case 1:
									oneLocal++
								case 2:
									twoLocal++
								case 3:
									threeLocal++
								case 4:
									fourLocal++
								}
							} else if len(shardToSend) > 1 {
								totalCross++
								for _, shard := range shardToSend {
									if shard == 1 {
										oneCross++
									}
									if shard == 2 {
										twoCross++
									}
									if shard == 3 {
										threeCross++
									}
									if shard == 4 {
										fourCross++
									}
								}
							}
							for _, shard := range shardToSend {
								if isCrossShardTx {
									gw.sendedTransactionList[shard][1] = append(gw.sendedTransactionList[shard][1], tx.Hash)
									// log.GDebugf("[Gateway] %v Send CrossShardTransaction Hash: %v, From: %v, To: %v, to shard: %v, External: %v, RwSet: %v", cnt, tx.Hash, tx.From, tx.To, shard, v.ExternalAddressList, tx.RwSet)
								} else {
									gw.sendedTransactionList[shard][0] = append(gw.sendedTransactionList[shard][0], tx.Hash)
									// log.GDebugf("[Gateway] %v Send Transaction Hash: %v, From: %v, To: %v, to shard: %v, External: %v, RwSet: %v", cnt, tx.Hash, tx.From, tx.To, shard, v.ExternalAddressList, tx.RwSet)
								}

								gw.workerBuilderTransports[shard].Send(tx)
								// log.GDebugf("[Gateway] %v Send Transaction Hash: %v, From: %v, To: %v, Value: %v, to shard: %v, External: %v, RwSet: %v", cnt, tx.Hash, tx.From, tx.To, tx.Value, shard, v.ExternalAddressList, tx.RwSet)
							}
						}
					default:
						// Handle Transfer Transaction
						shardToSend := utils.CalculateShardToSend([]common.Address{v.From, v.To}, config.GetConfig().ShardCount)
						isCrossShardTx := len(shardToSend) > 1
						tx := message.MakeTransferTransaction(v, isCrossShardTx, utils.NewNodeID(0))
						cnt++
						if len(shardToSend) == 1 {
							if shardToSend[0] == 1 {
								oneLocal++
							} else if shardToSend[0] == 2 {
								twoLocal++
							} else if shardToSend[0] == 3 {
								threeLocal++
							} else if shardToSend[0] == 4 {
								fourLocal++
							}
						} else if len(shardToSend) == 2 {
							totalCross++
							for _, shard := range shardToSend {
								if shard == 1 {
									oneCross++
								} else if shard == 2 {
									twoCross++
								} else if shard == 3 {
									threeCross++
								} else if shard == 4 {
									fourCross++
								}
							}
						}
						for _, shard := range shardToSend {
							if isCrossShardTx {
								gw.sendedTransactionList[shard][1] = append(gw.sendedTransactionList[shard][1], tx.Hash)
								log.GDebugf("[Gateway] %v Send CrossShardTransaction Hash: %v, From: %v, To: %v, Value: %v, to shard: %v, RwSet: %v", cnt, tx.Hash, tx.From, tx.To, tx.Value, shard, tx.RwSet)
							} else {
								gw.sendedTransactionList[shard][0] = append(gw.sendedTransactionList[shard][0], tx.Hash)
								log.GDebugf("[Gateway] %v Send Transaction Hash: %v, From: %v, To: %v, Value: %v, to shard: %v", cnt, tx.Hash, tx.From, tx.To, tx.Value, shard)
							}
							gw.workerBuilderTransports[shard].Send(tx)
							log.GDebugf("[Gateway] %v Send Transaction Hash: %v, From: %v, To: %v, Value: %v, to shard: %v, External: %v, RwSet: %v", cnt, tx.Hash, tx.From, tx.To, tx.Value, shard, v.ExternalAddressList, tx.RwSet)
						}
					}
					if cnt%1000 == 0 {
						log.GDebugf("%v transactions sent", cnt)
					}
				case blockchain.WorkerBlock:
					// Unishard TTA 용 성능 측정 로그
					if gw.startBlockHeight[v.BlockHeader.ShardIdentifier] == 0 {
						if len(v.BlockData.LocalExecutionSequence) > 0 {
							gw.startBlockHeight[v.BlockHeader.ShardIdentifier] = v.BlockHeader.BlockHeight
							gw.startTime[v.BlockHeader.ShardIdentifier] = time.Now()
						}
					} else if v.BlockHeader.BlockHeight <= types.BlockHeight(config.GetConfig().Epoch)+gw.startBlockHeight[v.BlockHeader.ShardIdentifier] {
						if v.BlockHeader.BlockHeight == types.BlockHeight(config.GetConfig().Epoch)+gw.startBlockHeight[v.BlockHeader.ShardIdentifier] {
							gw.clientEnd[v.BlockHeader.ShardIdentifier] = true
							gw.epochDuration[v.BlockHeader.ShardIdentifier] = float64(time.Since(gw.startTime[v.BlockHeader.ShardIdentifier]).Seconds())
						}
						for _, transaction := range v.BlockData.LocalExecutionSequence {
							if transaction.IsCrossShardTx {
								if gw.crossTransactionLatency[transaction.Hash] == 0 {
									gw.crossTransactionLatency[transaction.Hash] = time.Now().UnixMilli() - transaction.Timestamp
								}
							} else {
								gw.localTransactionLatency[v.BlockHeader.ShardIdentifier][transaction.Hash] = time.Now().UnixMilli() - transaction.Timestamp
							}
						}
						gw.committedTransactionNumber[v.BlockHeader.ShardIdentifier] += len(v.BlockData.LocalExecutionSequence)
						log.Debugf("[Gateway] Receive Commit Task Event from Shard %v BlockHeight %v CommittedTransactionNumber %v", v.BlockHeader.ShardIdentifier, v.BlockHeader.BlockHeight-gw.startBlockHeight[v.BlockHeader.ShardIdentifier], len(v.BlockData.LocalExecutionSequence))
					}

					// remaining transaction 용
					for _, transaction := range v.BlockData.LocalExecutionSequence {
						for index, sendedTrasnasction := range gw.sendedTransactionList[v.BlockHeader.ShardIdentifier][0] {
							if sendedTrasnasction == transaction.Hash {
								gw.sendedTransactionList[v.BlockHeader.ShardIdentifier][0] = append(gw.sendedTransactionList[v.BlockHeader.ShardIdentifier][0][:index], gw.sendedTransactionList[v.BlockHeader.ShardIdentifier][0][index+1:]...)
							}
						}
						for index, sendedTrasnasction := range gw.sendedTransactionList[v.BlockHeader.ShardIdentifier][1] {
							if sendedTrasnasction == transaction.Hash {
								gw.sendedTransactionList[v.BlockHeader.ShardIdentifier][1] = append(gw.sendedTransactionList[v.BlockHeader.ShardIdentifier][1][:index], gw.sendedTransactionList[v.BlockHeader.ShardIdentifier][1][index+1:]...)
							}
						}
					}

					// shard := v.BlockHeader.ShardIdentifier
					// blockHeight := v.BlockHeader.BlockHeight
					// numberOfTransaction := len(v.BlockData.LocalExecutionSequence)
					remainingLocalTransaction := len(gw.sendedTransactionList[v.BlockHeader.ShardIdentifier][0])
					remainingCrossTransaction := len(gw.sendedTransactionList[v.BlockHeader.ShardIdentifier][1])
					if cnt >= config.GetConfig().Benchmark.N {
						log.GDebugf("[shard %v] remaining local tx number: %v", v.BlockHeader.ShardIdentifier, remainingLocalTransaction)
						log.GDebugf("[shard %v] remaining cross tx number: %v", v.BlockHeader.ShardIdentifier, remainingCrossTransaction)
					}
					// if remainingTransaction < 20 {
					// 	for _, transactionHash := range gw.sendedTransactionList[v.BlockHeader.ShardIdentifier] {
					// log.GDebugf("ct: %v", transactionHash)
					// 	}
					// }

				// case message.Experiment:
				// 	for _, crossShardLatency := range v.CrossShardTransactionLatency {
				// 		if _, exist := gw.Experiment[crossShardLatency.Hash]; !exist {
				// 			gw.Experiment[crossShardLatency.Hash] = crossShardLatency.Latency
				// 		} else {
				// 			if gw.Experiment[crossShardLatency.Hash] > crossShardLatency.Latency {
				// 				gw.Experiment[crossShardLatency.Hash] = crossShardLatency.Latency
				// 			}
				// 		}
				// 	}

				// 	_, exist := gw.savedExperimentTransactionResult[v.Shard]
				// 	if v.FlagForExperiment && !exist {
				// 		gw.savedExperimentTransactionResult[v.Shard] = v.ExperimentTransactionResult
				// 		// log.Debugf("shard %v saved experiment transaction result with total TPS %v", v.Shard, math.Round((float64(v.ExperimentTransactionResult.TotalTransaction) / float64(v.ExperimentTransactionResult.RunningTime))))
				// 	}
				// 	gw.savedExperiment[v.Shard] = v
				// 	for _, crossLatencyDissection := range v.CrossLatencyDissection {
				// 		gw.crossLatencyDissectionList = append(gw.crossLatencyDissectionList, crossLatencyDissection.Dissection)
				// 	}
				// 	for _, localLatencyDissection := range v.LocalLatencyDissection {
				// 		gw.localLatencyDissectionList = append(gw.localLatencyDissectionList, localLatencyDissection.Dissection)
				// 	}

				default:
					// log.GDebugf("illegal message type : %s", reflect.TypeOf(v).String())
				}
				// gw.once.Do(func() {
				// 	log.ExperimentResultf("SystemTPS,TotalTPS,LocalTPS,CrossTPS,LocalLatency,CrossLatency,ConsensusForISC,ISC,ConsensusForCommit,WaitingTime,LocalConsensusForCommit,LocalWaitingTime")
				// })
				// if cnt >= config.GetConfig().Benchmark.N {
				// 	if cnt == config.GetConfig().Benchmark.N {
				// 		log.GDebugf("finish sending transactions")
				// 	}
				// 	cnt ++
				// 	go func() {
				// 		ticker := time.NewTicker(time.Millisecond * time.Duration(5000))
				// 		<-ticker.C
				// 		totalCrossShardLatency := int64(0)
				// 		for _, latency := range gw.Experiment {
				// 			totalCrossShardLatency += latency
				// 		}
				// 		avgCrossShardLatency := math.Round((float64(totalCrossShardLatency)/1000)/(float64(len(gw.Experiment)))*1000) / 1000
				// 		// log.GDebugf("[Gateway] avg cross latency: %v, one local: %v, cross: %v, two local: %v, cross: %v, three local: %v, cross: %v, four local: %v, cross: %v, total cross: %v", avgCrossShardLatency, oneLocal, oneCross, twoLocal, twoCross, threeLocal, threeCross, fourLocal, fourCross, totalCross)
				// 		systemTPS := 0.0
				// 		sumOfTotalTPS := 0.0
				// 		sumOfLocalTPS := 0.0
				// 		sumOfCrossTPS := 0.0
				// 		sumOfLocalLatency := 0.0
				// 		sumOfCrossLatency := 0.0
				// 		localLatencyDissection := []int64{0, 0}
				// 		crossLatencyDissection := []int64{0, 0, 0, 0, 0, 0, 0, 0}

				// 		for _, dissection := range gw.localLatencyDissectionList {
				// 			localLatencyDissection[0] += dissection[0]
				// 			localLatencyDissection[1] += dissection[1]
				// 		}

				// 		for _, dissection := range gw.crossLatencyDissectionList {
				// 			crossLatencyDissection[0] += dissection[0]
				// 			crossLatencyDissection[1] += dissection[1]
				// 			crossLatencyDissection[2] += dissection[2]
				// 			crossLatencyDissection[3] += dissection[3]
				// 			crossLatencyDissection[4] += dissection[4]
				// 			crossLatencyDissection[5] += dissection[5]
				// 			crossLatencyDissection[6] += dissection[6]
				// 			crossLatencyDissection[7] += dissection[7]
				// 		}

				// 		avgWBBWaitingTime := (float64(crossLatencyDissection[0]) / 1000) / float64(len(gw.crossLatencyDissectionList))
				// 		avgWorkerConsensusTime := (float64(crossLatencyDissection[1]) / 1000) / float64(len(gw.crossLatencyDissectionList))
				// 		avgNetwork1 := (float64(crossLatencyDissection[2]) / 1000) / float64(len(gw.crossLatencyDissectionList))
				// 		avgCBBWaitingTime := (float64(crossLatencyDissection[3]) / 1000) / float64(len(gw.crossLatencyDissectionList))
				// 		avgCoordinationConsensusTime := (float64(crossLatencyDissection[4]) / 1000) / float64(len(gw.crossLatencyDissectionList))
				// 		avgNetwork2 := (float64(crossLatencyDissection[5]) / 1000) / float64(len(gw.crossLatencyDissectionList))
				// 		avgWBBWaitingTime2 := (float64(crossLatencyDissection[6]) / 1000) / float64(len(gw.crossLatencyDissectionList))
				// 		avgWorkerConsensusTime2 := (float64(crossLatencyDissection[7]) / 1000) / float64(len(gw.crossLatencyDissectionList))
				// 		avgLocalWBBWaitingTime := (float64(localLatencyDissection[0]) / 1000) / float64(len(gw.localLatencyDissectionList))
				// 		avgLocalWorkerConsensusTime := (float64(localLatencyDissection[1]) / 1000) / float64(len(gw.localLatencyDissectionList))

				// 		consensusForISC := avgWorkerConsensusTime + avgCoordinationConsensusTime
				// 		ISC := avgNetwork1 + avgNetwork2
				// 		consensusForCommit := avgWorkerConsensusTime2
				// 		waitingTime := avgWBBWaitingTime + avgCBBWaitingTime + avgWBBWaitingTime2

				// 		for i := 1; i <= config.GetConfig().ShardCount; i++ {
				// 			savedExperimentTransactionResult := gw.savedExperimentTransactionResult[types.Shard(i)]
				// 			savedExperiment := gw.savedExperiment[types.Shard(i)]
				// 			systemTPS += math.Round((float64(savedExperimentTransactionResult.TotalTransaction) / savedExperimentTransactionResult.RunningTime))
				// 			sumOfTotalTPS += math.Round((float64(savedExperimentTransactionResult.TotalTransaction) / savedExperimentTransactionResult.RunningTime))
				// 			sumOfLocalTPS += math.Round((float64(savedExperimentTransactionResult.LocalTransaction) / savedExperimentTransactionResult.RunningTime))
				// 			sumOfCrossTPS += math.Round((float64(savedExperimentTransactionResult.CrossTransaction) / savedExperimentTransactionResult.RunningTime))
				// 			sumOfLocalLatency += savedExperiment.LocalLatency
				// 			sumOfCrossLatency += avgCrossShardLatency
				// 		}
				// 		log.ExperimentResultf("%.3f,%.3f,%.3f,%.3f,%.3f,%.3f,%.3f,%.3f,%.3f,%.3f,%.3f,%.3f",
				// 			systemTPS,
				// 			sumOfTotalTPS/float64(config.GetConfig().ShardCount),
				// 			sumOfLocalTPS/float64(config.GetConfig().ShardCount),
				// 			sumOfCrossTPS/float64(config.GetConfig().ShardCount),
				// 			sumOfLocalLatency/float64(config.GetConfig().ShardCount),
				// 			sumOfCrossLatency/float64(config.GetConfig().ShardCount),
				// 			consensusForISC,
				// 			ISC,
				// 			consensusForCommit,
				// 			waitingTime,
				// 			avgLocalWorkerConsensusTime,
				// 			avgLocalWBBWaitingTime,
				// 		)
				// 	}()
				// }

			// drop another topic
			case <-gw.CoordinationBuilderTopic:
			case <-gw.WorkerBuilderTopic:
				continue
			}
		}
	}
}

// For Test
// func (gw *Gateway) SendMessage() {
// 	for _, workerbuilder := range gw.workerBuilderTransports {
// 		workerbuilder.Send(message.CreateBlock{
// 			Message: "Create Block!!!!",
// 		})
// 	}
// 	fmt.Println("Send Create Message To WorkerBlockBuilder")
// }

func (gw *Gateway) handleWorkerBuilderListResponse(msg message.WorkerBuilderListResponse) {
	// C -> G
	gw.GatewayTopic <- msg
}

func (gw *Gateway) handleWorkerShardVariableRegisterResponse(msg message.WorkerSharedVariableRegisterResponse) {
	// W -> G
	gw.GatewayTopic <- msg
}

func (gw *Gateway) handleTransactionForm(msg message.TransactionForm) {
	gw.GatewayTopic <- msg
}

func (gw *Gateway) handleTransaction(msg message.Transaction) {
	gw.GatewayTopic <- msg
}

func (gw *Gateway) handleWorkerBlock(workerBlock blockchain.WorkerBlock) {
	gw.GatewayTopic <- workerBlock
	// log.GDebugf("Received TransactionForm Message: %v", msg)
}

func (gw *Gateway) handleExperiment(msg message.Experiment) {
	gw.GatewayTopic <- msg
}

func (gw *Gateway) handleClientStart(msg message.ClientStart) {
	gw.GatewayTopic <- msg
}

func (gw *Gateway) getExternalRwSet(idx int, externalAddressList []common.Address, externalSet []types.ExternalElem) ([]message.RwVariable, int) {
	rwSet := make([]message.RwVariable, 0)
	curIdx := idx
	for _, externalElem := range externalSet {
		if externalElem.ElemType == "read" {
			rwSet = append(rwSet, message.RwVariable{
				Address: externalAddressList[idx],
				Name:    externalElem.Name,
				RwSet:   types.RwSet{ReadSet: []string{externalElem.Name}, WriteSet: []string{}},
			})
			curIdx++
		} else if externalElem.ElemType == "execute" {
			rwSet = append(rwSet, message.RwVariable{
				Address: externalAddressList[idx],
				Name:    externalElem.Name,
				RwSet:   gw.contractRwSet[externalAddressList[idx]][externalElem.Name],
			})
			curIdx++
			if len(gw.contractRwSet[externalAddressList[idx]][externalElem.Name].ExternalSet) > 0 {
				externalRwSet, _curIdx := gw.getExternalRwSet(curIdx, externalAddressList, gw.contractRwSet[externalAddressList[idx]][externalElem.Name].ExternalSet)
				rwSet = append(rwSet, externalRwSet...)
				curIdx = _curIdx
			}
		}
	}
	return rwSet, curIdx
}

func (gw *Gateway) calculateExperimentResult() {
	totalTPS := 0.0
	localTransactionNum := 0
	averageLocalTransactionLatency := 0.0
	averageCrossTransactionLatency := 0.0

	for i := 1; i <= config.GetConfig().ShardCount; i++ {
		shardTPS := float64(gw.committedTransactionNumber[types.Shard(i)]) / gw.epochDuration[types.Shard(i)]
		totalTPS += shardTPS
	}

	for _, shardLocalLatency := range gw.localTransactionLatency {
		for _, latency := range shardLocalLatency {
			averageLocalTransactionLatency += float64(latency)
			localTransactionNum++
		}
	}
	for _, latency := range gw.crossTransactionLatency {
		averageCrossTransactionLatency += float64(latency)
	}
	averageLocalTransactionLatency = averageLocalTransactionLatency / float64(localTransactionNum) / 1000
	averageCrossTransactionLatency = averageCrossTransactionLatency / float64(len(gw.crossTransactionLatency)) / 1000

	log.Debugf("Experiment Result")
	log.Debugf("Total TPS: %0.3f", totalTPS)
	log.Debugf("Average Local Transaction Latency: %.3f", averageLocalTransactionLatency)
	log.Debugf("Average Cross-shard Transaction Latency: %.3f", averageCrossTransactionLatency)
}

func (gw *Gateway) updateSlotForMapping(readSet []string, writeSet []string, idx []byte, targetIdx string) ([]string, []string) {
	tempReadSet := []string{}
	tempWriteSet := []string{}

	for _, elem := range readSet {
		if elem == targetIdx {
			tempReadSet = append(tempReadSet, hex.EncodeToString(idx))
		} else {
			tempReadSet = append(tempReadSet, elem)
		}
	}
	for _, elem := range writeSet {
		if elem == targetIdx {
			tempWriteSet = append(tempWriteSet, hex.EncodeToString(idx))
		} else {
			tempWriteSet = append(tempWriteSet, elem)
		}
	}
	return tempReadSet, tempWriteSet
}
