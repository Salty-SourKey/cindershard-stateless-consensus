package dlog

import (
	"io"
	"time"
	"unishard/log"
	"unishard/message"

	"github.com/rs/zerolog"
)

type DistributedLogger struct {
	zerolog.Logger
	nodeId       string
	shardId      string
	nodeEndpoint string
}

type Benchmark struct {
	dl    *DistributedLogger
	name  string
	start time.Time
}

func NewDistributedLogger(natsAddress string, nodeId string, nodeEndpoint string, shardId string) *DistributedLogger {
	natsWriter := NewNATSWriter()
	writer := io.Writer(natsWriter)
	err := natsWriter.Connect(natsAddress)
	if err != nil {
		log.Errorf("failed to initialize distributed logger, disable it: %s", err)
		writer = io.Discard
	}

	return &DistributedLogger{
		Logger:       zerolog.New(writer).With().Timestamp().Logger(),
		nodeId:       nodeId,
		shardId:      shardId,
		nodeEndpoint: nodeEndpoint,
	}
}

func (dl *DistributedLogger) Event(logType string) *zerolog.Event {
	return dl.Logger.Info().
		Str("log_type", logType).
		Time("timestamp", time.Now()).
		Str("node_id", dl.nodeId).
		Str("shard_id", dl.shardId).
		Str("node_endpoint", dl.nodeEndpoint)
}

func (dl *DistributedLogger) BootingSequence(seqNum int) {
	dl.Event("booting_sequence").
		Int("sequence_num", seqNum).
		Msg("")
}

func (dl *DistributedLogger) TransactionPath(tx *message.Transaction, step string) {
	dl.Event("transaction_path").
		Bytes("transaction_hash", tx.Hash[:]).
		Str("step", step).
		Msg("")
}

func (dl *DistributedLogger) NewBenchmark(name string) *Benchmark {
	return &Benchmark{
		dl:   dl,
		name: name,
	}
}

func (bm *Benchmark) Start() *Benchmark {
	bm.start = time.Now()
	return bm
}

func (bm *Benchmark) End() {
	end := time.Now()
	bm.dl.Event("benchmark").
		Str("name", bm.name).
		Float64("duration", end.Sub(bm.start).Seconds()).
		Msg("")
}
