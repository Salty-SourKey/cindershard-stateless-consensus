package pacemaker

import (
	"sync"

	"unishard/blockchain"
	"unishard/types"
)

type TimeoutController struct {
	n        int                                  // the size of the network
	timeouts map[types.View]map[types.NodeID]*TMO // keeps track of timeout msgs
	mu       sync.Mutex
}

func NewTimeoutController(n int) *TimeoutController {
	tcl := new(TimeoutController)
	tcl.n = n
	tcl.timeouts = make(map[types.View]map[types.NodeID]*TMO)

	return tcl
}

func (tcl *TimeoutController) AddTmo(tmo *TMO) (bool, *TC, *blockchain.WorkerBlock) {
	tcl.mu.Lock()
	defer tcl.mu.Unlock()
	if tcl.superMajority(tmo.NewView) {
		return false, nil, nil
	}
	_, exist := tcl.timeouts[tmo.NewView]
	if !exist {
		//	first time of receiving the timeout for this view
		tcl.timeouts[tmo.NewView] = make(map[types.NodeID]*TMO)
	}
	tcl.timeouts[tmo.NewView][tmo.NodeID] = tmo
	if tcl.superMajority(tmo.NewView) {
		tc, reservedPrepareBlock := NewTC(tmo.NewView, tcl.timeouts[tmo.NewView])
		return true, tc, reservedPrepareBlock
	}

	return false, nil, nil
}

func (tcl *TimeoutController) AddMjorityTmo(tmo *TMO) (bool, *TC, *blockchain.WorkerBlock) {
	tcl.mu.Lock()
	defer tcl.mu.Unlock()
	if tcl.Majority(tmo.NewView) {
		return false, nil, nil
	}
	_, exist := tcl.timeouts[tmo.NewView]
	if !exist {
		//	first time of receiving the timeout for this view
		tcl.timeouts[tmo.NewView] = make(map[types.NodeID]*TMO)
	}
	tcl.timeouts[tmo.NewView][tmo.NodeID] = tmo
	if tcl.Majority(tmo.NewView) {
		tc, reservedPrepareBlock := NewTC(tmo.NewView, tcl.timeouts[tmo.NewView])
		return true, tc, reservedPrepareBlock
	}

	return false, nil, nil
}

func (tcl *TimeoutController) superMajority(view types.View) bool {
	return tcl.total(view) > tcl.n*2/3
}

func (tcl *TimeoutController) Majority(view types.View) bool {
	return tcl.total(view) > tcl.n*1/2
}

func (tcl *TimeoutController) total(view types.View) int {
	return len(tcl.timeouts[view])
}
