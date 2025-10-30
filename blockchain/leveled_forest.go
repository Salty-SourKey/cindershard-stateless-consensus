package blockchain

import (
	"fmt"
	"unishard/log"

	"github.com/ethereum/go-ethereum/common"
)

const prunedBlockLimit uint64 = 128

// LevelledForest contains multiple trees (which is a potentially disconnected planar graph).
// Each vertexContainer in the graph has a level (view) and a hash. A vertexContainer can only have one parent
// with strictly smaller level (view). A vertexContainer can have multiple children, all with
// strictly larger level (view).
// A LevelledForest provides the ability to prune all vertices up to a specific level.
// A tree whose root is below the pruning threshold might decompose into multiple
// disconnected subtrees as a result of pruning.
type LeveledForest struct {
	vertices          VertexSet
	verticesAtLevel   map[uint64]VertexList
	LowestLevel       uint64
	LowestPrunedLevel uint64
}

type VertexList []*vertexContainer
type VertexSet map[common.Hash]*vertexContainer

// vertexContainer holds information about a tree vertex. Internally, we distinguish between
//   - FULL container: has non-nil value for vertex.
//     Used for vertices, which have been added to the tree.
//   - EMPTY container: has NIL value for vertex.
//     Used for vertices, which have NOT been added to the tree, but are
//     referenced by vertices in the tree. An empty container is converted to a
//     full container when the respective vertex is added to the tree
type vertexContainer struct {
	id       common.Hash
	level    uint64
	children VertexList

	// the following are only set if the block is actually known
	vertex Vertex
}

// NewLevelledForest initializes a LevelledForest
func NewLeveledForest() *LeveledForest {
	return &LeveledForest{
		vertices:        make(VertexSet),
		verticesAtLevel: make(map[uint64]VertexList),
	}
}

// PruneUpToLevel prunes all blocks UP TO but NOT INCLUDING `level`
func (f *LeveledForest) PruneUpToWorkerBlockLevel(level uint64) ([]Block, int, error) {
	// 1. find committed levels
	// 2. go through each level and prune, if it is not committed, add it to pruned
	var prunedBlockNo int
	forkedBlocks := make([]Block, 0)
	committedLevels := make(map[uint64]bool)
	if level < f.LowestLevel {
		return nil, prunedBlockNo, fmt.Errorf("new lowest level %d cannot be smaller than previous last retained level %d", level, f.LowestLevel)
	}
	// level에 대하여, 실질적 commit 진행
	// 범위: [ Lowest level, commit 대상 level ]
	for l := level; l >= f.LowestLevel && l > 1; {
		// assume each level has only one vertex
		vertex := f.verticesAtLevel[l][0].vertex
		parentID, _ := vertex.ParentWorkerBlock()
		parentVertex, ok := f.GetVertex(parentID)
		if !ok || parentVertex.WorkerBlockLevel() < f.LowestLevel {
			break
		}
		committedLevels[parentVertex.WorkerBlockLevel()] = true
		l = parentVertex.WorkerBlockLevel()
	}
	// level에대하여, Commit된 경우 block 삭제(pruning)
	// 범위: [Lowest Level, level)
	for l := f.LowestLevel; l < level; l++ {
		// find fork blocks
		for _, v := range f.verticesAtLevel[l] { // nil map behaves like empty map when iterating over it
			if !committedLevels[l] && l > 1 {
				if v.vertex != nil {
					log.Debugf("found a forked block, view: %v, id: %x", v.vertex.WorkerBlockLevel(), v.vertex.VertexID())
					forkedBlocks = append(forkedBlocks, v.vertex.GetBlock())
				}
			}
		}
	}

	// level로 LowestLevel 갱신
	f.LowestLevel = level

	if level < prunedBlockLimit {
		return forkedBlocks, prunedBlockNo, nil
	}
	// Unlock TimeLock 까지 Block을 남겨둠
	// 정상적인 경우, 결과: [LowestPrunedLevel ~ level - TIMELOCKFORUNLOCk] 이전 블록까지 커밋된 경우, 전부 삭제
	for l := f.LowestPrunedLevel; l <= level-prunedBlockLimit; l++ {
		// find fork blocks
		for _, v := range f.verticesAtLevel[l] { // nil map behaves like empty map when iterating over it
			prunedBlockNo++
			delete(f.vertices, v.id)
		}
		delete(f.verticesAtLevel, l)
	}

	f.LowestPrunedLevel = level - prunedBlockLimit

	return forkedBlocks, prunedBlockNo, nil
}

func (f *LeveledForest) PruneUpToCoordinationBlockLevel(level uint64) ([]Block, int, error) {
	// 1. find committed levels
	// 2. go through each level and prune, if it is not committed, add it to pruned
	var prunedBlockNo int
	forkedBlocks := make([]Block, 0)
	committedLevels := make(map[uint64]bool)
	if level < f.LowestLevel {
		return nil, prunedBlockNo, fmt.Errorf("new lowest level %d cannot be smaller than previous last retained level %d", level, f.LowestLevel)
	}
	// level에 대하여, 실질적 commit 진행
	// 범위: [ Lowest level, commit 대상 level ]
	for l := level; l >= f.LowestLevel && l > 1; {
		// assume each level has only one vertex
		vertex := f.verticesAtLevel[l][0].vertex
		parentID, _ := vertex.ParentCoordinationBlock()
		parentVertex, ok := f.GetVertex(parentID)
		if !ok || parentVertex.CoordinationBlockLevel() < f.LowestLevel {
			break
		}
		committedLevels[parentVertex.CoordinationBlockLevel()] = true
		l = parentVertex.CoordinationBlockLevel()
	}
	// level에대하여, Commit된 경우 block 삭제(pruning)
	// 범위: [Lowest Level, level)
	for l := f.LowestLevel; l < level; l++ {
		// find fork blocks
		for _, v := range f.verticesAtLevel[l] { // nil map behaves like empty map when iterating over it
			if !committedLevels[l] && l > 1 {
				if v.vertex != nil {
					log.Debugf("found a forked block, view: %v, id: %x", v.vertex.CoordinationBlockLevel(), v.vertex.VertexID())
					forkedBlocks = append(forkedBlocks, v.vertex.GetBlock())
				}
			}
		}
	}

	// level로 LowestLevel 갱신
	f.LowestLevel = level

	if level < prunedBlockLimit {
		return forkedBlocks, prunedBlockNo, nil
	}
	// Unlock TimeLock 까지 Block을 남겨둠
	// 정상적인 경우, 결과: [LowestPrunedLevel ~ level - TIMELOCKFORUNLOCk] 이전 블록까지 커밋된 경우, 전부 삭제
	for l := f.LowestPrunedLevel; l <= level-prunedBlockLimit; l++ {
		// find fork blocks
		for _, v := range f.verticesAtLevel[l] { // nil map behaves like empty map when iterating over it
			prunedBlockNo++
			delete(f.vertices, v.id)
		}
		delete(f.verticesAtLevel, l)
	}

	f.LowestPrunedLevel = level - prunedBlockLimit

	return forkedBlocks, prunedBlockNo, nil
}

// HasVertex returns true iff full vertex exists
func (f *LeveledForest) HasVertex(id common.Hash) bool {
	container, exists := f.vertices[id]
	return exists && !f.isEmptyContainer(container)
}

// isEmptyContainer returns true iff vertexContainer container is empty, i.e. full vertex itself has not been added
func (f *LeveledForest) isEmptyContainer(vertexContainer *vertexContainer) bool {
	return vertexContainer.vertex == nil
}

// GetVertex returns (<full vertex>, true) if the vertex with `id` and `level` was found
// (nil, false) if full vertex is unknown
func (f *LeveledForest) GetVertex(id common.Hash) (Vertex, bool) {
	container, exists := f.vertices[id]
	if !exists || f.isEmptyContainer(container) {
		return nil, false
	}
	return container.vertex, true
}

// GetChildren returns a VertexIterator to iterate over the children
// An empty VertexIterator is returned, if no vertices are known whose parent is `id` , `level`
func (f *LeveledForest) GetChildren(id common.Hash) VertexIterator {
	container := f.vertices[id]
	// if vertex does not exists, container is the default zero value for vertexContainer, which contains a nil-slice for its children
	return newVertexIterator(container.children) // VertexIterator gracefully handles nil slices
}

// GetVerticesAtLevel returns a VertexIterator to iterate over the Vertices at the specified height
func (f *LeveledForest) GetNumberOfChildren(id common.Hash) int {
	container := f.vertices[id] // if vertex does not exists, container is the default zero value for vertexContainer, which contains a nil-slice for its children
	num := 0
	for _, child := range container.children {
		if child.vertex != nil {
			num++
		}
	}
	return num
}

// GetVerticesAtLevel returns a VertexIterator to iterate over the Vertices at the specified height
// An empty VertexIterator is returned, if no vertices are known at the specified `level`
func (f *LeveledForest) GetVerticesAtLevel(level uint64) VertexIterator {
	return newVertexIterator(f.verticesAtLevel[level]) // go returns the zero value for a missing level. Here, a nil slice
}

// AddVertex adds vertex to forest if vertex is within non-pruned levels
// Handles repeated addition of same vertex (keeps first added vertex).
// If vertex is at or below pruning level: method is NoOp.
// UNVALIDATED:
// requires that vertex would pass validity check LevelledForest.VerifyVertex(vertex).
func (f *LeveledForest) AddWorkerBlockVertex(vertex Vertex) {
	if vertex.WorkerBlockLevel() < f.LowestLevel {
		return
	}
	container := f.getOrCreateVertexContainer(vertex.VertexID(), vertex.WorkerBlockLevel())
	if !f.isEmptyContainer(container) { // the vertex was already stored
		return
	}
	// container is empty, i.e. full vertex is new and should be stored in container
	container.vertex = vertex // add vertex to container
	f.registerWitParenthWorkerBlock(container)
}
func (f *LeveledForest) AddCoordinationBlockVertex(vertex Vertex) {
	if vertex.CoordinationBlockLevel() < f.LowestLevel {
		return
	}
	container := f.getOrCreateVertexContainer(vertex.VertexID(), vertex.CoordinationBlockLevel())
	if !f.isEmptyContainer(container) { // the vertex was already stored
		return
	}
	// container is empty, i.e. full vertex is new and should be stored in container
	container.vertex = vertex // add vertex to container
	f.registerWithParentCooridnationBlock(container)
}

func (f *LeveledForest) registerWitParenthWorkerBlock(vertexContainer *vertexContainer) {
	// caution: do not modify this combination of check (a) and (a)
	// Deliberate handling of root vertex (genesis block) whose view is _exactly_ at LowestLevel
	// For this block, we don't care about its parent and the exception is allowed where
	// vertex.level = vertex.Parent().Level = LowestLevel = 0
	if vertexContainer.level <= f.LowestLevel { // check (a)
		return
	}

	_, parentView := vertexContainer.vertex.ParentWorkerBlock()
	if parentView < f.LowestLevel {
		return
	}
	parentContainer := f.getOrCreateVertexContainer(vertexContainer.vertex.ParentWorkerBlock())
	parentContainer.children = append(parentContainer.children, vertexContainer) // append works on nil slices: creates slice with capacity 2
}

func (f *LeveledForest) registerWithParentCooridnationBlock(vertexContainer *vertexContainer) {
	// caution: do not modify this combination of check (a) and (a)
	// Deliberate handling of root vertex (genesis block) whose view is _exactly_ at LowestLevel
	// For this block, we don't care about its parent and the exception is allowed where
	// vertex.level = vertex.Parent().Level = LowestLevel = 0
	if vertexContainer.level <= f.LowestLevel { // check (a)
		return
	}

	_, parentView := vertexContainer.vertex.ParentCoordinationBlock()
	if parentView < f.LowestLevel {
		return
	}
	parentContainer := f.getOrCreateVertexContainer(vertexContainer.vertex.ParentCoordinationBlock())
	parentContainer.children = append(parentContainer.children, vertexContainer) // append works on nil slices: creates slice with capacity 2
}

// getOrCreateVertexContainer returns the vertexContainer if there exists one
// or creates a new vertexContainer and adds it to the internal data structures.
// It errors if a vertex with same id but different Level is already known
// (i.e. there exists an empty or full container with the same id but different level).
func (f *LeveledForest) getOrCreateVertexContainer(id common.Hash, level uint64) *vertexContainer {
	container, exists := f.vertices[id] // try to find vertex container with same ID
	if !exists {                        // if no vertex container found, create one and store it
		container = &vertexContainer{
			id:    id,
			level: level,
		}
		f.vertices[container.id] = container
		vtcs := f.verticesAtLevel[container.level]                   // returns nil slice if not yet present
		f.verticesAtLevel[container.level] = append(vtcs, container) // append works on nil slices: creates slice with capacity 2
	}
	return container
}

// remove highest block from blockchain
func (f *LeveledForest) revertHighestBlock(level uint64) {
	for _, v := range f.verticesAtLevel[level] { // nil map behaves like empty map when iterating over it
		delete(f.vertices, v.id)
	}
	delete(f.verticesAtLevel, level)
}
