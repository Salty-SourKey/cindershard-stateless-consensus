package blockchain

import (
	"fmt"
	"os"
	"strconv"

	"unishard/config"
	"unishard/crypto"
	"unishard/evm"
	"unishard/evm/state"
	"unishard/log"
	"unishard/message"
	"unishard/quorum"
	"unishard/types"
	"unishard/utils"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/triedb"
	"github.com/ethereum/go-ethereum/triedb/hashdb"
	"github.com/holiman/uint256"
)

type BlockChain struct {
	forest           *LeveledForest
	statedb          *state.StateDB
	committedStatedb *state.StateDB
	nodedb           state.Database
	// SCT
	expectedSCT map[common.Address]map[common.Hash]*SnapshotControlTable
	stateRoot   common.Hash
	// longestTailBlock *Block
	// measurement
	highestComitted     int
	committedBlockNo    int
	totalBlockIntervals int
	prunedBlockNo       int
}

type SnapshotControlTable struct {
	Value         string
	RTCS          []common.Hash
	ProtectedFlag bool
}

func NewWorkerBlockchain(shard types.Shard, defaultBalance int, id types.NodeID) *BlockChain {
	bc := new(BlockChain)
	bc.forest = NewLeveledForest()
	bc.expectedSCT = make(map[common.Address]map[common.Hash]*SnapshotControlTable)

	stateRoots := []string{}

	for i := 1; i <= config.GetConfig().ShardCount; i++ {
		shardRoot, _ := os.ReadFile(fmt.Sprintf("../common/statedb/"+strconv.Itoa(config.GetConfig().ShardCount)+"/shard%v_root.txt", i))
		stateRoots = append(stateRoots, string(shardRoot))
	}
	bc.stateRoot = common.HexToHash(stateRoots[int(shard)-1])

	if string(id) == "1" {
		dbObject, err := rawdb.NewLevelDBDatabase("../common/statedb/"+strconv.Itoa(config.GetConfig().ShardCount)+"/"+strconv.Itoa(int(shard))+"-"+string(id), 128, 1024, "", false)
		if err != nil {
			log.Errorf("Error while creating leveldb: %v", err)
		}

		tdb := triedb.NewDatabase(dbObject, nil)
		bc.nodedb = state.NewDatabaseWithNodeDB(dbObject, tdb)
		bc.statedb, _ = state.New(bc.stateRoot, bc.nodedb, nil)
		bc.committedStatedb, _ = state.New(bc.stateRoot, bc.nodedb, nil)
	}
	return bc
}

func NewCoordinationBlockchain(shard types.Shard, defaultBalance int) *BlockChain {
	bc := new(BlockChain)
	bc.forest = NewLeveledForest()
	bc.statedb, _ = evm.NewMemoryState()
	addresses := config.GetAddresses()
	for _, address := range addresses {
		addr := []common.Address{*address}
		if utils.CalculateShardToSend(addr, config.GetConfig().ShardCount)[0] == shard {
			bc.statedb.SetBalance(*address, uint256.NewInt(uint64(defaultBalance)))
			bc.statedb.SetNonce(*address, 0)
			// fmt.Printf("shard: %v, address: %x, balance : %v\n", shard, address, bc.statedb.GetBalance(*address))
		}
	}
	bc.statedb.IntermediateRoot(true)

	return bc
}

func (bc *BlockChain) Exists(id common.Hash) bool {
	return bc.forest.HasVertex(id)
}

func (bc *BlockChain) AddWorkerBlock(block Block) {
	blockContainer := &BlockContainer{block}
	bc.forest.AddWorkerBlockVertex(blockContainer)
}
func (bc *BlockChain) AddCoordinationBlock(block Block) {
	blockContainer := &BlockContainer{block}
	bc.highestComitted = int(block.CoordinationBlockHeader().BlockHeight)
	bc.forest.AddCoordinationBlockVertex(blockContainer)
}

func (bc *BlockChain) GetCurrentStateHash() common.Hash {
	return bc.statedb.GetTrie().Hash()
}

func (bc *BlockChain) GetStateRoot() common.Hash {
	return bc.stateRoot
}

// func (bc *BlockChain) ValidateTransaction(myShard types.Shard, tx *message.Transaction) (bool, error) {
// 	err := bc.statedb.ValidateTransaction(myShard, tx)
// 	if err != nil {
// 		return false, err
// 	}
// 	return true, nil
// }

func (bc *BlockChain) ExecuteTransaction(tx *message.Transaction) error {
	// Update Balance of Sender and Receiver
	return fmt.Errorf("transfer error: %w", evm.Transfer(bc.statedb, tx.From, tx.To, uint256.NewInt(uint64(tx.Value))))
}

func (bc *BlockChain) GetStateDB() *state.StateDB {
	return bc.statedb
}

func (bc *BlockChain) SetPartialState(keys, values, merkleProofKeys, merkleProofValues [][]byte, localExecutionSequence []*message.Transaction) bool {
	triedbConfig := &triedb.Config{
		Preimages: true,
		HashDB: &hashdb.Config{
			CleanCacheSize: 256 * 1024 * 1024,
		},
	}
	proofDB := rawdb.NewMemoryDatabase()
	for i, key := range merkleProofKeys {
		proofDB.Put(key, merkleProofValues[i])
	}

	for i, key := range keys {
		proofDB.Put(key, values[i])
	}

	// Construct triedb.Database with proofDB
	db := rawdb.NewMemoryDatabase()
	tdb := triedb.NewDatabase(proofDB, triedbConfig)

	bc.nodedb = state.NewDatabaseWithNodeDB(db, tdb)
	bc.statedb, _ = state.New(bc.stateRoot, bc.nodedb, nil)

	return (bc.statedb.GetTrie().Hash() == bc.stateRoot)
}

func (bc *BlockChain) GetExpectedSCT() map[common.Address]map[common.Hash]*SnapshotControlTable {
	return bc.expectedSCT
}

func (bc *BlockChain) GetBlockByID(id common.Hash) (Block, error) {
	vertex, exists := bc.forest.GetVertex(id)
	if !exists {
		return nil, fmt.Errorf("the block does not exist, id: %x", id)
	}

	return vertex.GetBlock(), nil
}

func (bc *BlockChain) GetParentWorkerBlock(id common.Hash) (Block, error) {
	vertex, exists := bc.forest.GetVertex(id)
	if !exists {
		return nil, fmt.Errorf("the block does not exist, id: %x", id)
	}
	parentID, _ := vertex.ParentWorkerBlock()
	parentVertex, exists := bc.forest.GetVertex(parentID)
	if !exists {
		return nil, fmt.Errorf("parent block does not exist, id: %x", parentID)
	}

	return parentVertex.GetBlock(), nil
}
func (bc *BlockChain) GetParentCoordinationBlock(id common.Hash) (Block, error) {
	vertex, exists := bc.forest.GetVertex(id)
	if !exists {
		return nil, fmt.Errorf("the block does not exist, id: %x", id)
	}
	parentID, _ := vertex.ParentCoordinationBlock()
	parentVertex, exists := bc.forest.GetVertex(parentID)
	if !exists {

		return nil, fmt.Errorf("parent block does not exist, id: %x", parentID)
	}

	return parentVertex.GetBlock(), nil
}

func (bc *BlockChain) GetStateHashByBlockID(id common.Hash) (common.Hash, error) {
	block, err := bc.GetBlockByID(id)
	if err != nil {
		return crypto.MakeID(nil), err
	}

	return block.GetBlockHash(), nil
}

// CommitBlock prunes blocks and returns committed blocks up to the last committed one and prunedBlocks
func (bc *BlockChain) CommitWorkerBlock(id common.Hash, blockHeight types.BlockHeight, voteQuorum *quorum.Quorum[quorum.Vote], commitQuorum *quorum.Quorum[quorum.Commit]) ([]Block, []Block, error) {
	vertex, ok := bc.forest.GetVertex(id)
	if !ok {
		return nil, nil, fmt.Errorf("cannot find the block, id: %x", id)
	}
	blockHeader := vertex.GetBlock().WorkerBlockHeader()

	committedBlockHeight := blockHeader.BlockHeight

	bc.highestComitted = int(blockHeader.BlockHeight)

	var committedBlocks []Block
	// Block이 정상적으로 합의에 이르렀는지 체크
	// 범위: ( Lowest Level block, Commit 대상 Block ]

	for block := vertex.GetBlock(); uint64(block.WorkerBlockHeader().BlockHeight) > bc.forest.LowestLevel; {
		committedBlocks = append(committedBlocks, block)

		voteQuorum.Delete(block.GetBlockHash())
		commitQuorum.Delete(block.GetBlockHash())

		bc.committedBlockNo++
		bc.totalBlockIntervals += int(blockHeight - blockHeader.BlockHeight)

		vertex, exists := bc.forest.GetVertex(blockHeader.PrevBlockHash)
		if !exists {
			break
		}

		block = vertex.GetBlock()
	}

	// commit 대상 block의 view에 대하여, pruning 진행
	forkedBlocks, prunedNo, err := bc.forest.PruneUpToWorkerBlockLevel(uint64(committedBlockHeight))
	if err != nil {
		return nil, nil, fmt.Errorf("cannot prune the blockchain to the committed block, id: %w", err)
	}
	bc.prunedBlockNo += prunedNo

	// commit executed state
	// bc.committedStatedb = executedState

	return committedBlocks, forkedBlocks, nil
}

func (bc *BlockChain) SetStateRoot(stateRoot common.Hash) {
	bc.stateRoot = stateRoot
	statedb, err := state.New(bc.stateRoot, bc.nodedb, nil)
	if err != nil {
		log.Errorf("failed to create new state db: %v", err)
	}
	bc.statedb = statedb
}
func (bc *BlockChain) CommitCoordinationBlock(id common.Hash, blockHeight types.BlockHeight, voteQuorum *quorum.Quorum[quorum.Vote], commitQuorum *quorum.Quorum[quorum.Commit]) ([]Block, []Block, error) {
	vertex, ok := bc.forest.GetVertex(id)
	if !ok {
		return nil, nil, fmt.Errorf("cannot find the block, id: %x", id)
	}
	blockHeader := vertex.GetBlock().CoordinationBlockHeader()
	committedBlockHeight := blockHeader.BlockHeight
	bc.highestComitted = int(blockHeader.BlockHeight)
	var committedBlocks []Block
	// Block이 정상적으로 합의에 이르렀는지 체크
	// 범위: ( Lowest Level block, Commit 대상 Block ]

	for block := vertex.GetBlock(); uint64(block.CoordinationBlockHeader().BlockHeight) > bc.forest.LowestLevel; {
		committedBlocks = append(committedBlocks, block)

		voteQuorum.Delete(block.GetBlockHash())
		commitQuorum.Delete(block.GetBlockHash())
		bc.committedBlockNo++
		bc.totalBlockIntervals += int(blockHeight - blockHeader.BlockHeight)
		vertex, exists := bc.forest.GetVertex(blockHeader.PrevBlockHash)
		if !exists {
			break
		}
		block = vertex.GetBlock()
	}

	// commit 대상 block의 view에 대하여, pruning 진행
	forkedBlocks, prunedNo, err := bc.forest.PruneUpToCoordinationBlockLevel(uint64(committedBlockHeight))
	if err != nil {
		return nil, nil, fmt.Errorf("cannot prune the blockchain to the committed block, id: %w", err)
	}
	bc.prunedBlockNo += prunedNo

	return committedBlocks, forkedBlocks, nil
}

func (bc *BlockChain) RevertBlock(blockheight types.BlockHeight) {
	bc.forest.revertHighestBlock(uint64(blockheight))
}

func (bc *BlockChain) GetChildrenBlocks(id common.Hash) []Block {
	var blocks []Block
	iterator := bc.forest.GetChildren(id)
	for I := iterator; I.HasNext(); {
		blocks = append(blocks, I.NextVertex().GetBlock())
	}
	return blocks
}

func (bc *BlockChain) GetChainGrowth() float64 {
	return float64(bc.committedBlockNo) / float64(bc.prunedBlockNo+1)
}

func (bc *BlockChain) GetBlockIntervals() float64 {
	return float64(bc.totalBlockIntervals) / float64(bc.committedBlockNo)
}

func (bc *BlockChain) GetHighestCommitted() int {
	return bc.highestComitted
}

func (bc *BlockChain) GetCommittedBlocks() int {
	return bc.committedBlockNo
}

func (bc *BlockChain) GetBlockByBlockHeight(blockHeight types.BlockHeight) Block {
	iterator := bc.forest.GetVerticesAtLevel(uint64(blockHeight))
	return iterator.next.GetBlock()
}
