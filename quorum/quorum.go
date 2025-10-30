package quorum

import (
	"fmt"
	"sync"
	"unishard/crypto"
	"unishard/log"
	"unishard/types"

	"github.com/ethereum/go-ethereum/common"
)

type (
	Commit byte
	Vote   byte
)

type quorumType interface {
	Vote | Commit
}

type Collection[T quorumType] struct {
	types.Epoch
	types.View
	types.BlockHeight
	Publisher types.NodeID
	BlockID   common.Hash
	crypto.Signature
}
type Quorum[T quorumType] struct {
	total       int
	collections map[common.Hash]map[types.NodeID]*Collection[T]
	mu          sync.Mutex
}

type QC struct {
	Leader types.NodeID
	Epoch  types.Epoch
	View   types.View
	types.BlockHeight
	BlockID common.Hash
	Signers []types.NodeID
	total   int
	crypto.AggSig
	crypto.Signature
	mu sync.Mutex
}

func NewQuorum[T quorumType](n int) *Quorum[T] {
	return &Quorum[T]{
		total:       n,
		collections: make(map[common.Hash]map[types.NodeID]*Collection[T]),
	}
}

func MakeCollection[T quorumType](epoch types.Epoch, view types.View, blockHeight types.BlockHeight, publisher types.NodeID, blockID common.Hash) *Collection[T] {
	sig, err := crypto.PrivSign(crypto.IDToByte(blockID), nil)
	if err != nil {
		log.Fatalf("[%v] has an error when signing a vote", publisher)
		return nil
	}
	return &Collection[T]{
		Epoch:       epoch,
		View:        view,
		BlockHeight: blockHeight,
		Publisher:   publisher,
		BlockID:     blockID,
		Signature:   sig,
	}
}

func (q *Quorum[T]) Add(collection *Collection[T]) (bool, *QC) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.superMajority(collection.BlockID) {
		return false, nil
	}
	_, exist := q.collections[collection.BlockID]
	if !exist {
		//	first time of receiving the vote for this block
		q.collections[collection.BlockID] = make(map[types.NodeID]*Collection[T])
	}
	q.collections[collection.BlockID][collection.Publisher] = collection
	if q.superMajority(collection.BlockID) {
		aggSig, signers, err := q.getSigs(collection.BlockID)
		if err != nil {
			log.Warningf("(Vote-Add) cannot generate a valid qc blockheight %v view %v epoch %v block id %x: %v", collection.BlockHeight, collection.View, collection.Epoch, collection.BlockID, err)
		}
		qc := &QC{
			Epoch:       collection.Epoch,
			View:        collection.View,
			BlockHeight: collection.BlockHeight,
			BlockID:     collection.BlockID,
			AggSig:      aggSig,
			Signers:     signers,
		}
		return true, qc
	}
	return false, nil
}

// Super majority quorum satisfied
func (q *Quorum[T]) superMajority(blockID common.Hash) bool {
	// log.Warning(q.total)
	return q.size(blockID) > q.total*2/3
}

// Super majority quorum satisfied
func (q *Quorum[T]) Majority(blockID common.Hash) bool {
	// log.Warning(q.total)
	return q.size(blockID) > q.total*1/2
}

// Size returns ack size for the block
func (q *Quorum[T]) size(blockID common.Hash) int {
	return len(q.collections[blockID])
}

func (q *Quorum[T]) getSigs(blockID common.Hash) (crypto.AggSig, []types.NodeID, error) {
	var sigs crypto.AggSig
	var signers []types.NodeID
	_, exists := q.collections[blockID]
	if !exists {
		return nil, nil, fmt.Errorf("sigs does not exist, id: %x", blockID)
	}
	for _, collection := range q.collections[blockID] {
		sigs = append(sigs, collection.Signature)
		signers = append(signers, collection.Publisher)
	}

	return sigs, signers, nil
}

func (q *Quorum[T]) Delete(blockID common.Hash) {
	q.mu.Lock()
	defer q.mu.Unlock()
	_, ok := q.collections[blockID]
	if ok {
		delete(q.collections, blockID)
	}
}
