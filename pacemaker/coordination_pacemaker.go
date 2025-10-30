package pacemaker

import (
	"sync"
	"time"

	"unishard/blockchain"
	"unishard/config"
	"unishard/types"
)

type CoordinationPacemaker struct {
	curView           types.View
	newView           types.View
	anchorView        types.View
	curRound          int
	curEpoch          types.Epoch
	viewChangePeriod  int
	newViewChan       chan types.EpochView
	tmoOccuredChan    chan types.EpochView
	timeoutController *CoordinationTimeoutController
	mu                sync.Mutex
}

func NewCoordinationPacemaker(n int) *CoordinationPacemaker {
	pm := new(CoordinationPacemaker)
	pm.newViewChan = make(chan types.EpochView, 100)
	pm.tmoOccuredChan = make(chan types.EpochView, 100)
	pm.timeoutController = NewCoordinationTimeoutController(n)
	pm.viewChangePeriod = config.GetConfig().ViewChangePeriod
	return pm
}

func (p *CoordinationPacemaker) ProcessRemoteTmo(tmo *CoordinationTMO) (bool, *CoordinationTC, *blockchain.CoordinationBlock) {
	if tmo.View < p.curView {
		return false, nil, nil
	}
	return p.timeoutController.AddTmo(tmo)
}

func (p *CoordinationPacemaker) ProcessRemoteMjorityTmo(tmo *CoordinationTMO) (bool, *CoordinationTC, *blockchain.CoordinationBlock) {
	if tmo.View < p.curView {
		return false, nil, nil
	}
	return p.timeoutController.AddMjorityTmo(tmo)
}

func (p *CoordinationPacemaker) AdvanceView(view types.View) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if view < p.curView {
		return
	}

	if config.GetConfig().RotatingElection == ROTATING_COMMITTEE {
		// Committee 구성이 매번 변경되는 경우
		p.curEpoch += 1
	} else if config.GetConfig().RotatingElection == ROTATING_LEADER {
		// Committee가 Leader를 돌아가며 하는 경우
		if p.curRound%(p.viewChangePeriod*config.GetConfig().CommitteeNumber) == 0 {
			p.curEpoch += 1
		}
	}

	// normal case view updating
	if p.curRound%p.viewChangePeriod == 0 && p.curRound/p.viewChangePeriod+1 > int(view) {
		p.curRound += 1
		p.UpdateView(view)
		return
	}
	p.curRound += 1
	p.newViewChan <- types.EpochView{Epoch: p.curEpoch, View: p.curView} // reset timer for the next view
}

// for viewchange, update new view it will be used
func (p *CoordinationPacemaker) UpdateView(view types.View) {
	if p.mu.TryLock() {
		defer p.mu.Unlock()
	}

	if view < p.curView {
		return
	}

	p.curView = view + 1
	p.newView = (view + 1) + 1
	p.anchorView = view + 1

	p.newViewChan <- types.EpochView{Epoch: p.curEpoch, View: p.curView} // reset timer for the next view
}

func (p *CoordinationPacemaker) AdvanceViewForFillHoleNode(view types.View) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if view < p.curView {
		return
	}

	if config.GetConfig().RotatingElection == ROTATING_COMMITTEE {
		// Committee 구성이 매번 변경되는 경우
		p.curEpoch += 1
	} else if config.GetConfig().RotatingElection == ROTATING_LEADER {
		// Committee가 Leader를 돌아가며 하는 경우
		if p.curRound%(p.viewChangePeriod*config.GetConfig().CommitteeNumber) == 0 {
			p.curEpoch += 1
		}
	}

	// normal case view updating
	if p.curRound%p.viewChangePeriod == 0 && p.curRound/p.viewChangePeriod+1 > int(view) {
		p.curRound += 1
		p.UpdateViewForFillholeNode(view)
		return
	}
	p.curRound += 1
}

func (p *CoordinationPacemaker) UpdateViewForFillholeNode(view types.View) {
	if p.mu.TryLock() {
		defer p.mu.Unlock()
	}

	if view < p.curView {
		return
	}

	p.curView = view + 1
	p.newView = (view + 1) + 1
	p.anchorView = view + 1
}

func (p *CoordinationPacemaker) UpdateEpoch(epoch types.Epoch) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if epoch < p.curEpoch {
		return
	}

	p.curEpoch = epoch
}

func (p *CoordinationPacemaker) ExecuteViewChange(newView types.View) {
	p.tmoOccuredChan <- types.EpochView{Epoch: p.curEpoch, View: newView}
}

func (p *CoordinationPacemaker) EnteringViewEvent() chan types.EpochView {
	return p.newViewChan
}

func (p *CoordinationPacemaker) EnteringTmoEvent() chan types.EpochView {
	return p.tmoOccuredChan
}

func (p *CoordinationPacemaker) UpdateAnchorView(anchorView types.View) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.anchorView = anchorView
}

func (p *CoordinationPacemaker) UpdateNewView(newView types.View) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.newView = newView
}

func (p *CoordinationPacemaker) GetCurView() types.View {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.curView
}
func (p *CoordinationPacemaker) GetNewView() types.View {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.newView
}
func (p *CoordinationPacemaker) GetAnchorView() types.View {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.anchorView
}
func (p *CoordinationPacemaker) GetCurRound() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.curRound
}

func (p *CoordinationPacemaker) GetCurEpoch() types.Epoch {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.curEpoch
}

func (p *CoordinationPacemaker) GetCurEpochView() (types.Epoch, types.View) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.curEpoch, p.curView
}

func (p *CoordinationPacemaker) GetTimerForView() time.Duration {
	return time.Duration(config.GetConfig().Timeout) * time.Millisecond
}

func (p *CoordinationPacemaker) GetTimerForViewChange() time.Duration {
	p.mu.Lock()
	defer p.mu.Unlock()
	return time.Duration(int(p.newView-p.anchorView)*config.GetConfig().ViewChangeTimeout) * time.Millisecond
}

func (p *CoordinationPacemaker) IsTimeToElect() bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Committee 구성이 매번 변경되는 경우
	if config.GetConfig().RotatingElection == ROTATING_COMMITTEE {
		return true
	}

	return p.curRound%(p.viewChangePeriod*config.GetConfig().CommitteeNumber) == 1
}
