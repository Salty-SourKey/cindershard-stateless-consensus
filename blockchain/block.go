package blockchain

import (
	"time"

	"unishard/crypto"
	"unishard/message"
	"unishard/quorum"
	"unishard/types"

	"github.com/ethereum/go-ethereum/common"
)

/*
	 	==========================
	     Unishard Block Structure
		==========================
*/

// Sequence is array of TransactionId
type Sequence []*message.Transaction
type LocalSnapshots []*LocalSnapshot

// ExecuteRequestBlock is result of "Coordinate-B" phase.
// Coordination shard sends this block to every worker shard.

type Type byte

const (
	Coordination Type = 0 + iota
	Worker
)

type CoordinationBlockHeader struct {
	Epoch         types.Epoch
	View          types.View
	GC            types.BlockHeight
	StateRoot     common.Hash
	PrevBlockHash common.Hash
	BlockHeight   types.BlockHeight
	Timestamp     time.Time
}

type CoordinationBlockData struct {
	GlobalCoordinationSequence Sequence
	GlobalSnapshot             LocalSnapshots
	GlobalContractBundle       []*LocalContract
}

type CoordinationBlock struct {
	BlockHeader        *CoordinationBlockHeader
	BlockHash          common.Hash
	QC                 *quorum.QC
	CQC                *quorum.QC
	CommitteeSignature []crypto.Signature
	Proposer           types.NodeID
	BlockData          *CoordinationBlockData
}
type Block interface {
	WorkerBlockHeader() *WorkerBlockHeader
	CoordinationBlockHeader() *CoordinationBlockHeader
	QuorumCertificate() *quorum.QC
	CommitQuorumCertificate() *quorum.QC
	WorkerBlockData() *WorkerBlockData
	CoordinationBlockData() *CoordinationBlockData
	GetBlockHash() common.Hash
}

func (wb *WorkerBlock) QuorumCertificate() *quorum.QC {
	return wb.QC
}

func (cb *CoordinationBlock) QuorumCertificate() *quorum.QC {
	return cb.QC
}

func (wb *WorkerBlock) WorkerBlockHeader() *WorkerBlockHeader {
	return wb.BlockHeader
}

func (cb *CoordinationBlock) CoordinationBlockHeader() *CoordinationBlockHeader {
	return cb.BlockHeader
}

func (wb *WorkerBlock) WorkerBlockData() *WorkerBlockData {
	return wb.BlockData
}

func (cb *CoordinationBlock) CoordinationBlockData() *CoordinationBlockData {
	return cb.BlockData
}

func (wb *WorkerBlock) CoordinationBlockHeader() *CoordinationBlockHeader {
	panic("unimplemented")
}

func (cb *CoordinationBlock) WorkerBlockHeader() *WorkerBlockHeader {
	panic("unimplemented")
}

func (wb *WorkerBlock) CoordinationBlockData() *CoordinationBlockData {
	panic("unimplemented")
}

func (cb *CoordinationBlock) WorkerBlockData() *WorkerBlockData {
	panic("unimplemented")
}

func (wb *WorkerBlock) GetBlockHash() common.Hash {
	return wb.BlockHash
}

func (cb *CoordinationBlock) GetBlockHash() common.Hash {
	return cb.BlockHash
}
func (wb *WorkerBlock) CommitQuorumCertificate() *quorum.QC {
	return wb.CQC
}

func (cb *CoordinationBlock) CommitQuorumCertificate() *quorum.QC {
	return cb.CQC
}

type CoordinationBlockWithoutHeader struct {
	CommitteeSignature    []crypto.Signature
	CoordinationBlockData *CoordinationBlockData
}

type ProposedWorkerBlock struct {
	WorkerBlock          *WorkerBlock
	GlobalSnapshot       []*LocalSnapshot
	GlobalContractBundle []*LocalContract
}

type WorkerBlock struct {
	BlockHeader        *WorkerBlockHeader
	BlockHash          common.Hash
	QC                 *quorum.QC
	CQC                *quorum.QC
	CommitteeSignature []crypto.Signature
	Proposer           types.NodeID
	BlockData          *WorkerBlockData
}

type WorkerBlockHeader struct {
	Epoch           types.Epoch
	View            types.View
	StateRoot       common.Hash
	PrevBlockHash   common.Hash
	BlockHeight     types.BlockHeight
	Timestamp       time.Time
	ShardIdentifier types.Shard
}

type TxData struct {
	ReceivedCrossTransaction   []*message.Transaction
	ReceivedLocalTransaction   []*message.Transaction
	GlobalCoordinationSequence []*message.Transaction
	GlobalSnapshot             []*LocalSnapshot
	GlobalContractBundle       []*LocalContract
	GC                         types.BlockHeight
}

type WorkerConsensusPayload struct {
	Data *TxData
}

type WorkerBlockData struct {
	LocalExecutionSequence []*message.Transaction
	Keys                   [][]byte
	Values                 [][]byte
	MerkleProofKeys        [][]byte
	MerkleProofValues      [][]byte
}

type WorkerBlockWithoutHeader struct {
	WorkerBlockData    *WorkerBlockData
	CommitteeSignature []crypto.Signature
	GlobalVariables    *GlobalVariables
}

type WorkerBlockWithoutHeaderWithGlobalVariable struct {
	WorkerBlockWithoutHeader *WorkerBlockWithoutHeader
	GlobalSnapshot           []*LocalSnapshot
	GlobalContractBundle     []*LocalContract
}

type LocalSnapshot struct {
	Address common.Address
	Slot    common.Hash
	Value   string
	RTCS    []common.Hash
}

type LocalContract struct {
	Address common.Address
	Code    []byte
}

type GlobalVariables struct {
	GlobalSnapshot       []*LocalSnapshot
	GlobalContractBundle []*LocalContract
}

// MakeBlock creates an unsigned block
func CreateWorkerBlockData(localExecutionSequence []*message.Transaction, keys [][]byte, values [][]byte, merkleProofKeys [][]byte, merkleProofValues [][]byte) *WorkerBlockData {
	b := new(WorkerBlockData)
	b.LocalExecutionSequence = localExecutionSequence
	b.Keys = keys
	b.Values = values
	b.MerkleProofKeys = merkleProofKeys
	b.MerkleProofValues = merkleProofValues

	return b
}

func CreateWorkerBlock(blockwithoutheader *WorkerBlockWithoutHeader, epoch types.Epoch, view types.View, stateRoot common.Hash, prevBlockHash common.Hash, currentBlockHeight types.BlockHeight, qc *quorum.QC, shard types.Shard) Block {
	blockHeader := &WorkerBlockHeader{
		Epoch:           epoch,
		View:            view,
		StateRoot:       stateRoot,
		PrevBlockHash:   prevBlockHash,
		BlockHeight:     currentBlockHeight + 1,
		ShardIdentifier: shard,
	}
	workerblock := new(WorkerBlock)
	workerblock.BlockHeader = blockHeader
	workerblock.BlockHash = workerblock.MakeHash(workerblock.BlockHeader)
	workerblock.BlockData = blockwithoutheader.WorkerBlockData
	workerblock.CommitteeSignature = blockwithoutheader.CommitteeSignature
	workerblock.QC = qc

	return workerblock
}

func CreateCoordinationBlock(blockwithoutheader *CoordinationBlockWithoutHeader, epoch types.Epoch, view types.View, gc types.BlockHeight, stateroot common.Hash, prevBlockHash common.Hash, currentBlockHeight types.BlockHeight, qc *quorum.QC) Block {
	blockHeader := &CoordinationBlockHeader{
		Epoch:         epoch,
		View:          view,
		GC:            gc,
		StateRoot:     stateroot,
		PrevBlockHash: prevBlockHash,
		BlockHeight:   currentBlockHeight + 1,
	}
	block := new(CoordinationBlock)
	block.BlockHeader = blockHeader
	block.BlockHash = block.MakeHash(block.BlockHeader)
	block.BlockData = blockwithoutheader.CoordinationBlockData
	block.CommitteeSignature = blockwithoutheader.CommitteeSignature
	block.QC = qc

	return block
}

func (wb *CoordinationBlockWithoutHeader) MakeHash(b interface{}) common.Hash {
	return crypto.MakeID(b)
}

func (wb *CoordinationBlockData) MakeHash(b interface{}) common.Hash {
	return crypto.MakeID(b)
}

func (wb *CoordinationBlock) MakeHash(b interface{}) common.Hash {
	return crypto.MakeID(b)
}

func (wb *WorkerConsensusPayload) MakeHash(b interface{}) common.Hash {
	return crypto.MakeID(b)
}

func (wb *WorkerBlockData) MakeHash(b interface{}) common.Hash {
	return crypto.MakeID(b)
}

func (wb *WorkerBlock) MakeHash(b interface{}) common.Hash {
	return crypto.MakeID(b)
}

// type Block struct {
// 	types.View
// 	types.BlockHeight
// 	types.Epoch
// 	QC        *quorum.QC
// 	CQC       *quorum.QC
// 	Proposer  identity.NodeID
// 	Timestamp time.Time
// 	SCPayload []*message.Transaction
// 	StateHash common.Hash
// 	PrevID    common.Hash
// 	Sig       crypto.Signature
// 	ID        common.Hash
// 	Ts        time.Duration
// }

type Request struct {
	Block Block
}

type OrderReq struct {
	Block             Block
	TargetBlockHeight types.BlockHeight
}

type ConfirmReq struct {
	Block *Block
}

type rawBlock struct {
	types.View
	QC       *quorum.QC
	CQC      *quorum.QC
	Proposer types.NodeID
	Payload  []common.Hash
	PrevID   common.Hash
	Sig      crypto.Signature
	ID       common.Hash
}

// MakeBlock creates an unsigned block
// func CreateBlock(view types.View, epoch types.Epoch, qc *quorum.QC, prevID common.Hash, scPayload []*message.Transaction, proposer identity.NodeID, stateHash common.Hash) *Block {
// 	b := new(Block)
// 	b.View = view
// 	b.Epoch = epoch
// 	b.BlockHeight = qc.BlockHeight + 1
// 	b.Proposer = proposer
// 	b.QC = qc
// 	//b.CQC = cqc
// 	b.SCPayload = scPayload
// 	b.PrevID = prevID
// 	b.StateHash = stateHash
// 	b.makeID(proposer)
// 	return b
// }

// func (b *Block) makeID(nodeID identity.NodeID) {
// 	raw := &rawBlock{
// 		View:     b.View,
// 		QC:       b.QC,
// 		CQC:      b.CQC,
// 		Proposer: b.Proposer,
// 		PrevID:   b.PrevID,
// 	}
// 	var payloadIDs []common.Hash
// 	// for _, txn := range b.SCPayload {
// 	// payloadIDs = append(payloadIDs, txn.ID)
// 	// }
// 	raw.Payload = payloadIDs
// 	b.ID = crypto.MakeID(raw)
// 	// TODO: uncomment the following
// 	b.Sig, _ = crypto.PrivSign(crypto.IDToByte(b.ID), nodeID, nil)
// }

type Accept struct {
	CommittedBlock *WorkerBlock
	*quorum.QC
	GlobalSnapshot       []*LocalSnapshot
	GlobalContractBundle []*LocalContract
	Timestamp            time.Time
}

type CoordinationAccept struct {
	CommittedBlock *CoordinationBlock
	*quorum.QC
	Timestamp time.Time
}
