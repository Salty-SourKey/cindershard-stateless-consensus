package blockchain

import "github.com/ethereum/go-ethereum/common"

// BlockContainer wraps a block to implement forest.Vertex
// In addition, it holds some additional properties for efficient processing of blocks
// by the Finalizer
type BlockContainer struct {
	Blocks Block
}

// functions implementing forest.vertex
func (b *BlockContainer) VertexID() common.Hash { return b.Blocks.GetBlockHash() }

func (b *BlockContainer) WorkerBlockLevel() uint64 {
	return uint64(b.Blocks.WorkerBlockHeader().BlockHeight)
}

func (b *BlockContainer) CoordinationBlockLevel() uint64 {
	return uint64(b.Blocks.CoordinationBlockHeader().BlockHeight)
}

func (b *BlockContainer) ParentWorkerBlock() (common.Hash, uint64) {
	return b.Blocks.WorkerBlockHeader().PrevBlockHash, uint64(b.Blocks.QuorumCertificate().BlockHeight)
}
func (b *BlockContainer) ParentCoordinationBlock() (common.Hash, uint64) {
	return b.Blocks.CoordinationBlockHeader().PrevBlockHash, uint64(b.Blocks.QuorumCertificate().BlockHeight)
}

func (b *BlockContainer) GetBlock() Block { return b.Blocks }
