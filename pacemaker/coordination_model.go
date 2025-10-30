package pacemaker

import (
	"unishard/blockchain"
	"unishard/crypto"
	"unishard/quorum"
	"unishard/types"
)

type CoordinationTMO struct {
	types.Shard
	types.Epoch
	View                types.View
	NewView             types.View
	AnchorView          types.View
	NodeID              types.NodeID
	BlockHeightLast     types.BlockHeight
	BlockHeightPrepared types.BlockHeight
	BlockLast           *blockchain.CoordinationBlock
	BlockPrepared       *blockchain.CoordinationBlock
	HighQC              *quorum.QC
}

type CoordinationTC struct {
	types.Shard
	types.Epoch
	NodeID         types.NodeID
	NewView        types.View
	AnchorView     types.View
	BlockHeightNew types.BlockHeight
	BlockMax       *blockchain.CoordinationBlock
	crypto.AggSig
	crypto.Signature
}

func NewCoordinationTC(newView types.View, requesters map[types.NodeID]*CoordinationTMO) (*CoordinationTC, *blockchain.CoordinationBlock) {
	// TODO: add crypto

	// set variable
	var (
		shard          types.Shard
		anchorView     types.View
		blockHeightNew types.BlockHeight
		blockMax       *blockchain.CoordinationBlock
		blockPrepared  *blockchain.CoordinationBlock
	)

	var (
		maxAnchorView          types.View
		maxBlockHeightLast     types.BlockHeight
		maxBlockHeightPrepared types.BlockHeight
	)

	for _, tmo := range requesters {
		if maxAnchorView < tmo.AnchorView {
			maxAnchorView = tmo.AnchorView
		}

		if maxBlockHeightLast < tmo.BlockHeightLast {
			maxBlockHeightLast = tmo.BlockHeightLast
			blockMax = tmo.BlockLast
		}

		if maxBlockHeightPrepared < tmo.BlockHeightPrepared {
			maxBlockHeightPrepared = tmo.BlockHeightPrepared
			blockPrepared = tmo.BlockPrepared
		}
	}

	anchorView = maxAnchorView
	blockHeightNew = maxBlockHeightLast + 1

	// if there is prepared block existed
	if maxBlockHeightPrepared < blockHeightNew {
		blockPrepared = nil
	}
	return &CoordinationTC{
		Shard:          shard,
		NewView:        newView,
		AnchorView:     anchorView,
		BlockHeightNew: blockHeightNew,
		BlockMax:       blockMax,
	}, blockPrepared
}
