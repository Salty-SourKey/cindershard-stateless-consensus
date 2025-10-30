package main

import (
	"flag"
	"net/http"
	"strconv"
	"sync"
	"time"

	"unishard/blockbuilder"
	"unishard/config"
	"unishard/coordinator"
	"unishard/crypto"
	"unishard/log"
	"unishard/types"
	"unishard/utils"
	"unishard/worker"
)

var (
	algorithm  = flag.String("algorithm", "pbft", "BFT consensus algorithm")
	id         = flag.Int("id", 0, "NodeID of the node")
	simulation = flag.Bool("sim", true, "simulation mode")
	mode       = flag.String("mode", "coordination", "Select worker or coordination shard") // worker, coordination, gateway
	shard      = flag.Int("shard", 0, "shard that node belongs to (only available on mode is blockbuilder or node)")
)

func initReplica(id types.NodeID, isByz bool, shard types.Shard) {

	if isByz {
		log.Infof("node %v is Byzantine", id)
	}

	// consensus algorithm에 따라 노드를 생성
	switch *algorithm {
	case "pbft":
		r := worker.NewReplica(id, *algorithm, isByz, shard)
		r.Start()
	default:
	}
}

func Init() {
	flag.Parse()
	log.Setup()
	config.Configuration.Load()
	http.DefaultTransport.(*http.Transport).MaxIdleConnsPerHost = 1000
}

func main() {
	Init()
	coordinationShard := types.Shard(0)
	gatewayShard := types.Shard(config.GetConfig().ShardCount + 1)

	// 실험에 사용될 공개키 및 개인키 생성
	errCrypto := crypto.SetKey()
	if errCrypto != nil {
		log.Fatal("Could not generate keys:", errCrypto)
	}

	// 실험에 사용될 각 노드의 tcp 주소 설정
	addrs := config.Configuration.Addrs
	coordinationShardBlockbuilderAddress := addrs[types.Shard(0)][utils.NewNodeID(0)] + "6000"
	gatewayAddress := addrs[types.Shard(config.GetConfig().ShardCount+1)][utils.NewNodeID(0)] + "5999"

	if *simulation {
		var wg sync.WaitGroup
		wg.Add(1)

		// 코디네이션 샤드의 proposer 및 committe 실행
		bb := coordinator.NewInitCoordinationBlockBuilder(coordinationShardBlockbuilderAddress, coordinationShard, gatewayAddress)
		bb.Start()
		time.Sleep(1 * time.Second / 2)
		for id := range config.GetConfig().Addrs[coordinationShard] {
			isByz := false
			go initReplica(id, isByz, coordinationShard)
		}
		time.Sleep(1 * time.Second / 2)

		// 워커 샤드의 proposer 및 committe 실행
		for i := 1; i <= config.GetConfig().ShardCount; i++ {
			shard := types.Shard(i)
			port := strconv.Itoa(6000 + i)
			wbb := blockbuilder.NewInitWorkerBlockBuilder("tcp://127.0.0.1:"+port, shard, coordinationShardBlockbuilderAddress, gatewayAddress)
			wbb.Start()
			time.Sleep(1 * time.Second / 2)
			for id := range config.GetConfig().Addrs[shard] {
				isByz := false
				go initReplica(id, isByz, shard)
			}
			time.Sleep(1 * time.Second / 2)
		}

		// 트랜잭션 전송을 담당하는 게이트웨이 실행
		gw := blockbuilder.NewInitGateway("tcp://127.0.0.1:5999", coordinationShard, coordinationShardBlockbuilderAddress)
		gw.Start()

		wg.Wait()
	} else {
		sh := *shard
		nodeID := *id
		isByz := false
		if *mode == "gateway" {
			log.Debugf("Gateway Start")
			gw := blockbuilder.NewInitGateway(gatewayAddress, gatewayShard, coordinationShardBlockbuilderAddress)
			gw.Start()
			log.Debugf("Gateway ip: %v", gw.GetIP())
		} else {
			// Worker consensus Node
			initReplica(utils.NewNodeID(nodeID), isByz, types.Shard(sh))
			log.Debugf("[Worker Shard %v Init] Worker Node Setup Complete", sh)
			log.Debugf("Setup Finish")
		}

	}
}
