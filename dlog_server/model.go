package main

import "time"

type (
	// LogWrapper is for providing custom JSON unmarshaling logic.
	// After json.Unmarshal(input, &LogWrapper{}),
	// LogWrapper.Log is filled POINTER OF actual log content struct (like *TransactionPathLog).
	// LogWrapper.Type is fileed 'log_type' field in JSON body.
	LogWrapper struct {
		Type string
		Log  interface{}
	}

	LogTypeIdentifier struct {
		Type string `json:"log_type"`
	}

	LogHeader struct {
		Timestamp    time.Time `json:"timestamp"`
		NodeId       string    `json:"node_id"`
		ShardId      string    `json:"shard_id"`
		NodeEndpoint string    `json:"node_endpoint"`
	}

	BootingSequenceLog struct {
		LogHeader
		SequenceNumber int `json:"sequence_num"`
	}

	TransactionPathLog struct {
		LogHeader
		TransactionHash string `json:"transaction_hash"`
		Step            string `json:"step"`
	}

	BenchmarkLog struct {
		LogHeader
		Name     string  `json:"name"`
		Duration float64 `json:"duration"`
	}
)
