package measure

import (
	"encoding/json"
	"math"
	"time"
	"unishard/blockchain"
	"unishard/log"
	"unishard/message"
	"unishard/types"

	"github.com/ethereum/go-ethereum/common"
)

type PBFTMeasure struct {
	ConflictTransaction            map[common.Hash]types.BlockHeight
	StartTime                      time.Time
	TotalCommittedTx               int
	TotalCommittedLocalTx          int
	TotalCommittedCrossTx          int
	TotalBlockSize                 int
	TotalBlockCount                int
	TotalLocalLatency              int64
	TotalWBBWaitingTime            int64
	TotalWorkerConsensusTime       int64
	TotalNetwork1                  int64
	TotalCBBWaitingTime            int64
	TotalCoordinationConsensusTime int64
	TotalNetwork2                  int64
	TotalWBBWaitingTime2           int64
	TotalWorkerConsensusTime2      int64
	TotalLocalWBBWaitingTime       int64
	TotalLocalWorkerConsensusTime  int64
}

func NewPBFTMeasure() *PBFTMeasure {
	return &PBFTMeasure{
		ConflictTransaction:            make(map[common.Hash]types.BlockHeight),
		StartTime:                      time.Time{},
		TotalCommittedTx:               0,
		TotalCommittedLocalTx:          0,
		TotalCommittedCrossTx:          0,
		TotalBlockSize:                 0,
		TotalBlockCount:                0,
		TotalLocalLatency:              0,
		TotalWBBWaitingTime:            0,
		TotalWorkerConsensusTime:       0,
		TotalNetwork1:                  0,
		TotalCBBWaitingTime:            0,
		TotalCoordinationConsensusTime: 0,
		TotalNetwork2:                  0,
		TotalWBBWaitingTime2:           0,
		TotalWorkerConsensusTime2:      0,
		TotalLocalWBBWaitingTime:       0,
		TotalLocalWorkerConsensusTime:  0,
	}
}

func (measure *PBFTMeasure) CalculateMeasurements(curWorkerBlockBlock *blockchain.WorkerBlock, shard types.Shard) message.Experiment {
	// calculate local latency and latency dissection
	for _, tx := range curWorkerBlockBlock.BlockData.LocalExecutionSequence {
		measure.TotalCommittedTx++
		latency := time.Now().UnixMilli() - tx.Timestamp
		measure.TotalLocalLatency += latency
		// calculate blocksize in bytes by converting curWorkerBlockBlock to json using marshaler
		measure.TotalBlockCount++
		go func() {
			blockSize, _ := json.Marshal(curWorkerBlockBlock)
			// mpk, _ := json.Marshal(curWorkerBlockBlock.BlockData.MerkleProofKeys)
			// mpv, _ := json.Marshal(curWorkerBlockBlock.BlockData.MerkleProofValues)
			// k, _ := json.Marshal(curWorkerBlockBlock.BlockData.Keys)
			// v, _ := json.Marshal(curWorkerBlockBlock.BlockData.Values)

			// measure.TotalBlockSize += len(mpk) + len(mpv)
			measure.TotalBlockSize += len(blockSize)
		}()

		// mpk, _ := json.Marshal(curWorkerBlockBlock.BlockData.MerkleProofKeys)
		// mpv, _ := json.Marshal(curWorkerBlockBlock.BlockData.MerkleProofValues)
		// k, _ := json.Marshal(curWorkerBlockBlock.BlockData.Keys)
		// v, _ := json.Marshal(curWorkerBlockBlock.BlockData.Values)

	}

	// calculate TPS
	totalTPS := math.Round((float64(measure.TotalCommittedTx) / time.Since(measure.StartTime).Seconds()))
	// calculate average latency
	avgLatency := CalculateAverageTimeDifference(measure.TotalLocalLatency, measure.TotalCommittedTx)

	avgBlockSize := (float64(measure.TotalBlockSize) / float64(measure.TotalBlockCount)) / 1_000_000

	log.ShardStatisticf("[%v] total TPS: %v, latency: %v, committed tx num: %v, blocksize: %.2f", shard, totalTPS, avgLatency, len(curWorkerBlockBlock.BlockData.LocalExecutionSequence), avgBlockSize)

	experiment := message.Experiment{}
	return experiment
}

func CalculateAverageTimeDifference(total int64, num int) float64 {
	return math.Round((float64(total)/1000)/float64(num)*1000) / 1000
}

func CalculateLatencyDissection(tx *message.Transaction) (wbbWaitingTime, workerConsensusTime, network1, cbbWaitingTime, coordinationConsensusTime, network2, wbbWaitingTime2, workerConsensusTime2 int64) {

	return tx.LatencyDissection.WBBWaitingTime - tx.Timestamp,
		tx.LatencyDissection.WorkerConsensusTime - tx.LatencyDissection.WBBWaitingTime,
		tx.LatencyDissection.Network1 - tx.LatencyDissection.WorkerConsensusTime,
		tx.LatencyDissection.CBBWaitingTIme - tx.LatencyDissection.Network1,
		tx.LatencyDissection.CoordinationConsensusTime - tx.LatencyDissection.CBBWaitingTIme,
		tx.LatencyDissection.Network2 - tx.LatencyDissection.CoordinationConsensusTime,
		tx.LatencyDissection.WBBWaitingTime2 - tx.LatencyDissection.Network2,
		tx.LatencyDissection.WorkerConsensusTime2 - tx.LatencyDissection.WBBWaitingTime2
}
