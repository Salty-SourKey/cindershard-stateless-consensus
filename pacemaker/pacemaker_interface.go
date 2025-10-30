package pacemaker

import (
	"time"

	"unishard/blockchain"
	"unishard/types"
)

type PaceMaker interface {
	ProcessRemoteTmo(tmo *TMO) (bool, *TC, *blockchain.Block)
	AdvanceView(view types.View)
	UpdateView(view types.View)
	ExecuteViewChange(newView types.View)
	EnteringViewEvent() chan types.EpochView
	EnteringTmoEvent() chan types.EpochView
	UpdateAnchorView(anchorView types.View)
	UpdateNewView(newView types.View)
	GetCurView() types.View
	GetNewView() types.View
	GetAnchorView() types.View
	GetCurRound() int
	GetCurEpoch() types.Epoch
	GetCurEpochView() (types.Epoch, types.View)
	GetTimerForView() time.Duration
	GetTimerForViewChange() time.Duration
	IsTimeToElect() bool
}
