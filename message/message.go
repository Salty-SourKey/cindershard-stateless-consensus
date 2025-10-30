package message

import (
	"fmt"
	"time"

	"unishard/crypto"
	"unishard/types"

	"github.com/ethereum/go-ethereum/common"
)

/***************************
 *  Unishard Init Related  *
 ***************************/
type ClientStart struct {
	Shard types.Shard
}
type WorkerBuilderRegister struct {
	SenderShard types.Shard
	Address     string
}

type WorkerBuilderListRequest struct {
	Gateway string
}

type WorkerBuilderListResponse struct {
	Builders map[types.Shard]string
}

type WorkerSharedVariableRegisterRequest struct {
	Gateway string
}

type WorkerSharedVariableRegisterResponse struct {
	SenderShard types.Shard
	Variables   string
}

type ConsensusNodeRegister struct {
	ConsensusNodeID types.NodeID
	IP              string
}

type CreateBlock struct {
	Message string
}

/***************************
 * Client-Replica Messages *
 ***************************/

// Contract Input
type Input struct {
	ContractAddress common.Hash
	Method          string
	Storage         string
	Value           []byte
}

/***************************
 *  Unishard RwSet Related *
 ***************************/
type CrossShardTransactionLatency struct {
	Hash    common.Hash
	Latency int64
}

type LatencyDissection struct {
	Hash       common.Hash
	Dissection []int64
}
type Experiment struct {
	types.Shard
	ExperimentTransactionResult  ExperimentTransactionResult
	CrossShardTransactionLatency []*CrossShardTransactionLatency
	LocalLatency                 float64
	LocalLatencyDissection       []*LatencyDissection
	CrossLatencyDissection       []*LatencyDissection
	FlagForExperiment            bool
}

type ExperimentTransactionResult struct {
	TotalTransaction int
	LocalTransaction int
	CrossTransaction int
	RunningTime      float64
}

type CrossShardTransactionLatencyDissection struct {
	// Coordination phase
	WBBWaitingTime            int64 // Worker BlockBuilder Waiting Time
	WorkerConsensusTime       int64 // Worker Shard Consensus Time
	Network1                  int64 // Network Latency(wbb->cbb)
	CBBWaitingTIme            int64 // Coordinator BlockBuilder Waiting Time
	CoordinationConsensusTime int64 // Coordinator Shard Consensus Time
	Network2                  int64 // Network Latency(cbb->Wbb)
	// Execute phase
	WBBWaitingTime2      int64 // Worker BlockBuilder Waiting Time
	WorkerExecuteStart   int64 // Worker Shard Execute Start Time
	WorkerExecuteEnd     int64 // Worker Shard Execute End Time
	WorkerConsensusTime2 int64 // Worker Shard Consensus Time
}

type RwVariable struct {
	Address common.Address
	Name    string
	types.RwSet
}

// Transaction Format
type TransactionForm struct {
	From                common.Address
	To                  common.Address
	Value               int
	Data                []byte
	ExternalAddressList []common.Address
	MappingIdx          []byte
	Timestamp           int64
}

// Transaction is client reqeust with http response channel
type Transaction struct {
	NodeID types.NodeID // forward by node
	Hash   common.Hash
	// C       chan TransactionReply // reply channel created by request receiver
	TXType types.TransactionType

	From                common.Address
	To                  common.Address
	Value               int
	Data                []byte
	ExternalAddressList []common.Address
	RwSet               []RwVariable
	IsCrossShardTx      bool
	Timestamp           int64
	LatencyDissection   CrossShardTransactionLatencyDissection
}

func MakeTransferTransaction(requestTx TransactionForm, isCrossShardTx bool, nodeID types.NodeID) *Transaction {
	tx := new(Transaction)
	// tx.Command.Value = value
	tx.NodeID = nodeID
	// tx.C = ppFree.Get().(chan TransactionReply)
	tx.From = requestTx.From
	tx.To = requestTx.To
	tx.Value = requestTx.Value
	tx.IsCrossShardTx = isCrossShardTx
	tx.Timestamp = requestTx.Timestamp
	tx.TXType = types.TRANSFER
	tx.makeID()
	return tx
}

func MakeDeployTransaction(requestTx TransactionForm, isCrossShardTx bool, nodeID types.NodeID) *Transaction {
	tx := new(Transaction)
	tx.NodeID = nodeID
	tx.From = requestTx.From
	tx.To = requestTx.To
	tx.Value = requestTx.Value
	tx.Data = requestTx.Data
	tx.IsCrossShardTx = isCrossShardTx
	tx.Timestamp = requestTx.Timestamp
	tx.TXType = types.DEPLOY
	tx.makeID()
	return tx
}

func MakeSmartContractTransaction(requestTx TransactionForm, rwSet []RwVariable, isCrossShardTx bool, nodeID types.NodeID) *Transaction {
	tx := new(Transaction)
	tx.NodeID = nodeID
	tx.From = requestTx.From
	tx.To = requestTx.To
	tx.Value = requestTx.Value
	tx.Data = requestTx.Data
	tx.ExternalAddressList = requestTx.ExternalAddressList
	tx.RwSet = rwSet
	tx.IsCrossShardTx = isCrossShardTx
	tx.Timestamp = requestTx.Timestamp
	tx.TXType = types.SMARTCONTRACT
	tx.makeID()
	return tx
}

type rawTransaction struct {
	From      common.Address
	To        common.Address
	Value     int
	Data      []byte
	TXType    types.TransactionType
	Timestamp int64
	// NodeID    types.NodeID // forward by node
}

func (tx *Transaction) makeID() {
	raw := &rawTransaction{
		From:      tx.From,
		To:        tx.To,
		Value:     tx.Value,
		Data:      tx.Data,
		TXType:    tx.TXType,
		Timestamp: tx.Timestamp,
		// NodeID:    tx.NodeID,
	}

	tx.Hash = crypto.MakeID(raw)
}

func (tx *Transaction) String() string {
	// return fmt.Sprintf("Transaction {cmd=%v nid=%v}", r.Command, r.NodeID)
	var res string
	switch tx.TXType {
	case types.TRANSFER:
		res = fmt.Sprintf("Transfer Transaction Hash: %x From: %v, To: %v, Balance: %v, IsCross: %v, Timestamp: %v", tx.Hash, tx.From, tx.To, tx.Value, tx.IsCrossShardTx, tx.Timestamp)
	// case types.DEPLOY:
	// 	res = fmt.Sprintf("Cross Shard Transfer Transaction Hash: %x From: %v, To: %v, balance: %v", r.Hash, r.From, r.To, r.Value)
	case types.SMARTCONTRACT:
		res = fmt.Sprintf("[%v] SmartContract Transaction Hash: %x To: %v, Data: %v, RwSet: %v, Timestamp: %v", tx.NodeID, tx.Hash, tx.To, tx.Data, tx.RwSet, tx.Timestamp)
	// case types.MESSAGE:
	// 	res = fmt.Sprintf("[%v] BCTX: %v, CETX: %v, VCTX: %v", r.NodeID, r.BlockCreationMSG, r.CommitteeElectionMSG, r.ViewChangeMSG)
	default:
		res = "Not Supported Transaction"
	}
	return res
}

// Query can be used as a special request that directly read the value of key without go through replication protocol in Replica
type Query struct {
	C chan QueryReply
}

func (r *Query) Reply(reply QueryReply) {
	r.C <- reply
}

// QueryReply cid and value of reading key
type QueryReply struct {
	Info string
}

// Request Leader
type RequestLeader struct {
	C chan RequestLeaderReply
}

func (r *RequestLeader) Reply(reply RequestLeaderReply) {
	r.C <- reply
}

// QueryReply cid and value of reading key
type RequestLeaderReply struct {
	Leader string
}

// Request Leader
// type ReportByzantine struct {
// 	C chan ReportByzantineReply
// }

// func (r *ReportByzantine) Reply(reply ReportByzantineReply) {
// 	r.C <- reply
// }

// // QueryReply cid and value of reading key
// type ReportByzantineReply struct {
// 	Leader string
// }

// For Change Test
type ProposeNewView struct {
	CurBlockHeight types.BlockHeight
	Sign           crypto.Signature
	BlockHash      common.Hash
	TimeStamp      time.Time
	NextLeaderID   types.NodeID
}

type AcceptNewView struct {
	CurBlockHeight          types.BlockHeight
	SignBatch               []crypto.Signature
	BlockHash               common.Hash
	NextLeaderID            types.NodeID
	ProposeNewViewTimeStamp time.Time
}

type ReportByzantine struct {
	CurBlockHeight  types.BlockHeight
	Sign            crypto.Signature
	BlockHash       common.Hash
	TimeStamp       time.Time
	ByzantineNodeID types.NodeID
}

type ReplaceByzantine struct {
	CurBlockHeight           types.BlockHeight
	SignBatch                []crypto.Signature
	BlockHash                common.Hash
	ReplaceByzantineNodeID   types.NodeID
	ReportByzantineTimeStamp time.Time
}

type ProposeNewCommittee struct {
	CurBlockHeight types.BlockHeight
	Sign           crypto.Signature
	BlockHash      common.Hash
	TimeStamp      time.Time
}

type ChangeCommittee struct {
	CurBlockHeight           types.BlockHeight
	SignBatch                []crypto.Signature
	BlockHash                common.Hash
	ChangeCommitteeTimeStamp time.Time
}
