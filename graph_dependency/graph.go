package graph_dependency

import (
	"github.com/ethereum/go-ethereum/common"
	"golang.org/x/exp/slices"
)

type Graph struct {
	Vertices      map[common.Hash]*Vertex
	LeavingEdges  map[common.Hash]int
	EnteringEdges map[common.Hash]int

	Dependency map[common.Hash][]Vertex
}

type Vertex struct {
	Val      common.Hash // Ex) PubKey
	Idx      int
	Edges    map[common.Hash]*Edge
	ReadSet  []string
	WriteSet []string
}

type Edge struct {
	Vertex *Vertex
}
type Pair struct {
	Key   int
	Value int
}
type PairList []Pair

func (p PairList) Len() int           { return len(p) }
func (p PairList) Less(i, j int) bool { return p[i].Value < p[j].Value }
func (p PairList) Swap(i, j int)      { p[i], p[j] = p[j], p[i] }

func NewGraph() *Graph {
	g := &Graph{Vertices: map[common.Hash]*Vertex{}, LeavingEdges: map[common.Hash]int{}, EnteringEdges: map[common.Hash]int{}}

	return g
}

func (g *Graph) AddVertex(val common.Hash, idx int, writeSet []string, readSet []string) {
	g.Vertices[val] = &Vertex{Val: val, Idx: idx, Edges: map[common.Hash]*Edge{}, WriteSet: writeSet, ReadSet: readSet}
}

func (g *Graph) DrawDependency() {
	for _, vertex_a := range g.Vertices {
		for _, vertex_b := range g.Vertices {
			if vertex_a.Idx < vertex_b.Idx {
				if compareSlice(vertex_a.WriteSet, vertex_b.ReadSet) {
					g.AddEdge(vertex_b.Val, vertex_a.Val)
				}
			}
		}
	}
}

func (g *Graph) AddEdge(srcKey common.Hash, destKey common.Hash) {
	if _, ok := g.Vertices[srcKey]; !ok {
		return
	}
	if _, ok := g.Vertices[destKey]; !ok {
		return
	}

	g.Vertices[srcKey].Edges[destKey] = &Edge{Vertex: g.Vertices[destKey]}
	g.LeavingEdges[srcKey] += 1
	g.EnteringEdges[destKey] += 1
}

func (g *Graph) Neighbors(srcKey common.Hash) []common.Hash {
	result := []common.Hash{}

	if _, ok := g.Vertices[srcKey]; !ok {
		return result
	}

	for _, edge := range g.Vertices[srcKey].Edges {
		result = append(result, edge.Vertex.Val)
	}

	return result
}

func (g *Graph) GetEnteringEdges(val common.Hash) int {
	return g.EnteringEdges[val]
}

func (g *Graph) GetLeavingEdges(val common.Hash) int {
	return g.LeavingEdges[val]
}

func (g *Graph) GetVertices() []common.Hash {
	result := []common.Hash{}
	for _, vertex := range g.Vertices {
		if !slices.Contains(result, vertex.Val) {
			result = append(result, vertex.Val)
		}
	}

	return result
}

func compareSlice(a_list []string, b_list []string) bool {
	for _, a := range a_list {
		for _, b := range b_list {
			if a == b {
				return true
			}
		}
	}
	return false
}
