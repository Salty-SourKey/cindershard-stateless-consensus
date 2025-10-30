package coordinator

import (
	"encoding/json"
	"sort"
	"strconv"
	"sync"
	"time"

	"unishard/blockchain"
	"unishard/dlog"
	"unishard/httpclient"
	"unishard/message"
	"unishard/node"
	"unishard/snapshot_control_table"
	"unishard/transport"
	"unishard/types"
	"unishard/utils"

	"github.com/ethereum/go-ethereum/common"

	"github.com/samber/lo"
)

type (
	snapshotSafetyResultCondition struct {
		Start      types.BlockHeight
		End        types.BlockHeight
		SnapshotId snapshot_control_table.AddressSlotPair
	}

	CoordBlockBuilderReplica struct {
		node.CoordBlockBuilder
		httpclient.Client
		CoordinationBuilderTopic chan interface{}
		WorkerBuilderTopic       chan interface{}
		GatewayTopic             chan interface{}
		ConsensusNodeTopic       chan interface{}
		gatewaynodeTransport     transport.Transport

		crossShardTransaction                  snapshot_control_table.TransactionRefList
		crossShardTransactionMutex             sync.Mutex
		crossShardTransactionAccumulateHistory []common.Hash
		SmartContractCodes                     map[common.Address][]byte
		smartContractCodesMutex                sync.RWMutex
		snapshotControlTable                   snapshot_control_table.SnapshotControlTable
		blockToRwSetCache                      map[types.BlockHeight]map[string]struct{}
		snapshotSafetyResultCache              map[snapshotSafetyResultCondition]bool

		GoFlag  bool
		dlogger *dlog.DistributedLogger
	}
)

func NewCoordinationBlockBuilderReplica(ip string, shard types.Shard, gatewayAddress string) *CoordBlockBuilderReplica {
	r := new(CoordBlockBuilderReplica)
	r.dlogger = dlog.NewDistributedLogger("nats://127.0.0.1:4222", "cbb", ip, strconv.Itoa(int(shard)))
	r.dlogger.BootingSequence(0)
	r.gatewaynodeTransport = transport.NewTransport(gatewayAddress)
	r.Client = httpclient.NewHTTPClient()
	r.CoordBlockBuilder = node.NewCoordinationBlockBuilder(ip, shard)
	r.CoordinationBuilderTopic = make(chan interface{}, 128)
	r.WorkerBuilderTopic = make(chan interface{}, 128)
	r.GatewayTopic = make(chan interface{}, 128)
	r.ConsensusNodeTopic = make(chan interface{}, 128)
	r.snapshotControlTable = snapshot_control_table.SnapshotControlTable{
		Table: map[common.Address]map[common.Hash]*snapshot_control_table.SnapshotControlEntity{},
		Mutex: sync.RWMutex{},
	}
	r.crossShardTransaction = snapshot_control_table.TransactionRefList{}
	r.crossShardTransactionMutex = sync.Mutex{}
	r.crossShardTransactionAccumulateHistory = []common.Hash{}
	r.SmartContractCodes = map[common.Address][]byte{}
	r.smartContractCodesMutex = sync.RWMutex{}
	r.blockToRwSetCache = map[types.BlockHeight]map[string]struct{}{}
	r.snapshotSafetyResultCache = map[snapshotSafetyResultCondition]bool{}
	r.GoFlag = false

	return r
}

// Initiate BFT consensus on the generated block
func (r *CoordBlockBuilderReplica) ProposeCoordinatorBlock() {
	// Verify that all required local snapshots are available and safe for the cross-shard transactions
	globalCoordinationSequence, globalSnapshot := r.FindSatisfiedCrossTransactions(1000)

	// Mark the used global snapshots as unsafe
	for _, sn := range globalSnapshot {
		r.snapshotControlTable.ExcludeSnapshot(sn)
	}

	// Verify the contract bundle to be used
	globalContractBundle := []*blockchain.LocalContract{}
	gcodeCandidate := []common.Address{}
	for _, txn := range globalCoordinationSequence {
		gcodeCandidate = append(gcodeCandidate, txn.To)
		gcodeCandidate = append(gcodeCandidate, txn.ExternalAddressList...)
	}
	gcodeCandidate = lo.Uniq(gcodeCandidate)

	for _, addr := range gcodeCandidate {
		code := r.GetContractBundle(addr)
		if code != nil {
			globalContractBundle = append(globalContractBundle, &blockchain.LocalContract{
				Address: addr,
				Code:    code,
			})
		}
	}

	// Measure for latency breakdown
	for _, ct := range globalCoordinationSequence {
		ct.LatencyDissection.CBBWaitingTIme = time.Now().UnixMilli()
		r.dlogger.TransactionPath(ct, "cbb_proposed")
	}

	// Generate a coordinator block that includes the global coordination sequence, global snapshot, and global contract bundle
	block := *r.CreateCoordinationBlockWithoutHeader(&blockchain.CoordinationBlockData{
		GlobalCoordinationSequence: globalCoordinationSequence,
		GlobalSnapshot:             globalSnapshot,
		GlobalContractBundle:       globalContractBundle,
	},
	)

	json.Marshal(block)

	r.CoordinationBuilderTopic <- block
}

// Verify all cross-shard transactions received by the coordinator shard,
// ensuring that the required snapshots are all available and safe for each transaction.
// If everything is okay, return the global coordination sequence and the global snapshot
// * if amountLimit != 0, length of returned blockchain.Sequence will be <= amountLimit
func (r *CoordBlockBuilderReplica) FindSatisfiedCrossTransactions(amountLimit int) (blockchain.Sequence, blockchain.LocalSnapshots) {
	satisfiedCrossTransaction := snapshot_control_table.TransactionRefList{}
	globalSnapshot := blockchain.LocalSnapshots{}
	if amountLimit == 0 {
		amountLimit = len(r.crossShardTransaction)
	}

	r.crossShardTransactionMutex.Lock()
	// Identify transactions for which all required snapshots are available and safe using the CheckCrossTransactionSatisfied function,
	// and add those transactions to satisfiedCrossTransaction
	satisfiedCount := 0
	r.crossShardTransaction = lo.Filter(r.crossShardTransaction, func(txn *message.Transaction, index int) bool {
		satisfied, snapshots := r.CheckCrossTransactionSatisfied(txn)
		if satisfied {
			satisfiedCount++
			if satisfiedCount > amountLimit {
				satisfied = false
			}
		}

		if satisfied {
			satisfiedCrossTransaction = append(satisfiedCrossTransaction, txn)
			globalSnapshot = append(globalSnapshot, *snapshots...)
		}
		return !satisfied
	})
	r.crossShardTransactionMutex.Unlock()

	// Remove duplicates from the global snapshot
	globalSnapshot = lo.UniqBy[*blockchain.LocalSnapshot, string](globalSnapshot, func(item *blockchain.LocalSnapshot) string {
		return item.Address.Hex() + item.Slot.Hex()
	})

	// Sort the transactions based on a first-come, first-served basis using their timestamps
	sort.Sort(satisfiedCrossTransaction)

	return blockchain.Sequence(satisfiedCrossTransaction), globalSnapshot
}

// Verify if all snapshots required by a specific transaction are available and safe.
// If all snapshots are available and safe, return true and the snapshots that will be used for the transaction
func (r *CoordBlockBuilderReplica) CheckCrossTransactionSatisfied(ct *message.Transaction) (bool, *blockchain.LocalSnapshots) {
	slot := []common.Hash{}
	addr := []common.Address{}

	// Identify the required snapshots based on the transaction type
	switch ct.TXType {
	case types.TRANSFER:
		slot = append(slot, utils.SlotToKey(0), utils.SlotToKey(0))
		addr = append(addr, ct.From, ct.To)

	case types.SMARTCONTRACT:
		for _, set := range ct.RwSet {
			for _, rv := range set.ReadSet {
				addr = append(addr, set.Address)
				slot = append(slot, common.HexToHash(rv))
			}
		}
		addr = append(addr, ct.From)
		slot = append(slot, utils.SlotToKey(0))
	}

	gs := blockchain.LocalSnapshots{}
	for i := 0; i < len(slot); i++ {
		// Condition 1: Check snapshot availability
		if !r.snapshotControlTable.Exist(slot[i], addr[i]) {
			return false, nil
		}
		// Condition 2: Check snapshot safety
		if r.snapshotControlTable.IsProtected(slot[i], addr[i]) {
			return false, nil
		}

		ls := r.snapshotControlTable.FindSnapshot(slot[i], addr[i])

		// If both conditions are satisfied, add it to the global snapshot
		gs = append(gs, ls)
	}

	return true, &gs
}

// Verify the safety of snapshots by checking the coordinator blocks
// from the coordinator block number referenced by the worker block,
// starting from workerGC + 1 to the latest coordinator block number, and return the results
// (This corresponds to the Pre-sequencing safety check)
func (r *CoordBlockBuilderReplica) CheckSnapshotSafety(workerGC types.BlockHeight, snapshot *blockchain.LocalSnapshot) bool {
	chain := r.GetBlockChain()

	blockHeightStart := workerGC + 1
	blockHeightEnd := types.BlockHeight(chain.GetHighestCommitted())

	targetSnapshotPair := snapshot_control_table.AddressSlotPair{
		Address: snapshot.Address,
		Slot:    snapshot.Slot,
	}

	targetSnapshotCondition := snapshotSafetyResultCondition{
		Start:      blockHeightStart,
		End:        blockHeightEnd,
		SnapshotId: targetSnapshotPair,
	}

	// Check if the targetSnapshotPair exists in the given unsafeList
	isSafe := func(unsafeList map[string]struct{}) bool {
		_, exist := unsafeList[targetSnapshotPair.Address.Hex()+targetSnapshotPair.Slot.Hex()]
		return !exist
	}

	// On a snapshotSafetyResult cache hit, return the result stored in the cache
	result := true
	if result, exist := r.snapshotSafetyResultCache[targetSnapshotCondition]; exist {
		return result
	}

	// On a snapshotSafetyResult cache miss, verify the snapshot's safety
	// by checking the coordinator blocks from workerGC + 1 to the latest coordinator block number
	for h := blockHeightStart; h <= blockHeightEnd; h++ {
		if rwSet, exist := r.blockToRwSetCache[h]; exist {
			// On a cache hit for the coordinator block rwSet, verify the snapshot safety using the rwSet stored in the cache
			if !isSafe(rwSet) {
				result = false
				goto SearchComplete
			}
		} else {
			// On a cache miss, check the rwSet of the coordinator block, store it in the cache,
			// and verify the snapshot safety using the retrieved rwSet
			blockRwSet := map[string]struct{}{}
			block := chain.GetBlockByBlockHeight(h)
			for _, ct := range block.CoordinationBlockData().GlobalCoordinationSequence {
				switch ct.TXType {
				case types.TRANSFER:
					blockRwSet[ct.From.Hex()+utils.SlotToKey(0).Hex()] = struct{}{}
				case types.SMARTCONTRACT:
					for _, set := range ct.RwSet {
						for _, rset := range set.ReadSet {
							blockRwSet[set.Address.Hex()+rset] = struct{}{}
						}
						for _, wset := range set.WriteSet {
							blockRwSet[set.Address.Hex()+wset] = struct{}{}
						}
					}

					blockRwSet[ct.From.Hex()+utils.SlotToKey(0).Hex()] = struct{}{}
				}
			}

			r.blockToRwSetCache[h] = blockRwSet

			if !isSafe(blockRwSet) {
				result = false
				goto SearchComplete
			}
		}
	}
	result = true

	// Store the snapshot safety result in the snapshotSafetyResult cache and return it
SearchComplete:
	r.snapshotSafetyResultCache[targetSnapshotCondition] = result
	return result
}

// Accumulate the cross-shard transactions received from each worker shard
func (r *CoordBlockBuilderReplica) AccumulateCrossTransaction(ReceivedCrossTransaction ...*message.Transaction) {
	for _, ct := range ReceivedCrossTransaction {
		r.crossShardTransactionMutex.Lock()
		if !lo.Contains(r.crossShardTransactionAccumulateHistory, ct.Hash) {
			r.crossShardTransaction = append(r.crossShardTransaction, ct)
			r.crossShardTransactionAccumulateHistory = append(r.crossShardTransactionAccumulateHistory, ct.Hash)
		}
		r.crossShardTransactionMutex.Unlock()
	}
}

// Accumulate the smart contract code received from each worker shard
func (r *CoordBlockBuilderReplica) AccumulateSmartContractBundle(localContractBundle []*blockchain.LocalContract) {
	defer r.smartContractCodesMutex.Unlock()
	r.smartContractCodesMutex.Lock()
	for _, contractCode := range localContractBundle {
		r.SmartContractCodes[contractCode.Address] = contractCode.Code
	}
}

// Return the smart contract code for the given address
func (r *CoordBlockBuilderReplica) GetContractBundle(addr common.Address) []byte {
	defer r.smartContractCodesMutex.RUnlock()
	r.smartContractCodesMutex.RLock()
	if code, exist := r.SmartContractCodes[addr]; exist {
		return code
	} else {
		return nil
	}
}
