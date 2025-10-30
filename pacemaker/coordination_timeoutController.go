package pacemaker

import (
	"sync"

	"unishard/blockchain"
	"unishard/types"
)

type CoordinationTimeoutController struct {
	n        int                                              // the size of the network
	timeouts map[types.View]map[types.NodeID]*CoordinationTMO // keeps track of timeout msgs
	mu       sync.Mutex
}

func NewCoordinationTimeoutController(n int) *CoordinationTimeoutController {
	tcl := new(CoordinationTimeoutController)
	tcl.n = n
	tcl.timeouts = make(map[types.View]map[types.NodeID]*CoordinationTMO)

	return tcl
}

func (tcl *CoordinationTimeoutController) AddTmo(tmo *CoordinationTMO) (bool, *CoordinationTC, *blockchain.CoordinationBlock) {
	tcl.mu.Lock()
	defer tcl.mu.Unlock()
	if tcl.superMajority(tmo.NewView) {
		return false, nil, nil
	}
	_, exist := tcl.timeouts[tmo.NewView]
	if !exist {
		//	first time of receiving the timeout for this view
		tcl.timeouts[tmo.NewView] = make(map[types.NodeID]*CoordinationTMO)
	}
	tcl.timeouts[tmo.NewView][tmo.NodeID] = tmo
	if tcl.superMajority(tmo.NewView) {
		tc, reservedPrepareBlock := NewCoordinationTC(tmo.NewView, tcl.timeouts[tmo.NewView])
		return true, tc, reservedPrepareBlock
	}

	return false, nil, nil
}

func (tcl *CoordinationTimeoutController) AddMjorityTmo(tmo *CoordinationTMO) (bool, *CoordinationTC, *blockchain.CoordinationBlock) {
	tcl.mu.Lock()
	defer tcl.mu.Unlock()
	if tcl.Majority(tmo.NewView) {
		return false, nil, nil
	}
	_, exist := tcl.timeouts[tmo.NewView]
	if !exist {
		//	first time of receiving the timeout for this view
		tcl.timeouts[tmo.NewView] = make(map[types.NodeID]*CoordinationTMO)
	}
	tcl.timeouts[tmo.NewView][tmo.NodeID] = tmo
	if tcl.Majority(tmo.NewView) {
		tc, reservedPrepareBlock := NewCoordinationTC(tmo.NewView, tcl.timeouts[tmo.NewView])
		return true, tc, reservedPrepareBlock
	}

	return false, nil, nil
}

func (tcl *CoordinationTimeoutController) superMajority(view types.View) bool {
	return tcl.total(view) > tcl.n*2/3
}

func (tcl *CoordinationTimeoutController) Majority(view types.View) bool {
	return tcl.total(view) > tcl.n*1/2
}

func (tcl *CoordinationTimeoutController) total(view types.View) int {
	return len(tcl.timeouts[view])
}
