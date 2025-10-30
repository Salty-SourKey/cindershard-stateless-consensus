package election

import (
	"unishard/log"
	"unishard/types"
	"unishard/utils"

	"github.com/ethereum/go-ethereum/common"
)

type Static struct {
	validators  types.IDs
	validatorNo int
	committees  types.IDs
	committeeNo int
	master      types.NodeID
	shard       types.Shard
}

func NewStatic(master types.NodeID, shard types.Shard, committeeNo int, validatorNo int) *Static {
	return &Static{
		master:      master,
		shard:       shard,
		validatorNo: validatorNo,
		validators:  []types.NodeID{},
		committeeNo: committeeNo,
		committees:  []types.NodeID{},
	}
}

func (st *Static) IsLeader(id types.NodeID, view types.View, epoch types.Epoch) bool {
	return id == st.master
}

func (st *Static) FindLeaderFor(view types.View, epoch types.Epoch) types.NodeID {
	return st.master
}

func (st *Static) IsCommittee(id types.NodeID, epoch types.Epoch) bool {
	for _, ID := range st.committees {
		if ID == id {
			return true
		}
	}
	return false
}

func (st *Static) IsValidator(id types.NodeID, epoch types.Epoch) bool {
	for _, ID := range st.validators {
		if ID == id {
			return true
		}
	}
	return false
}

func (st *Static) FindCommitteesFor(epoch types.Epoch) types.IDs {
	return st.committees
}

func (st *Static) FindValidatorsFor(epoch types.Epoch) types.IDs {
	return st.validators
}

func (st *Static) ElectLeader(id types.NodeID, newView types.View) bool {
	st.master = id
	return true
}

func (st *Static) ElectCommittees(blockHash common.Hash, newEpoch types.Epoch) types.IDs {
	if len(st.committees) == 0 {
		for i := 1; i <= st.committeeNo; i++ {
			st.committees = append(st.committees, utils.NewNodeID(i))
		}
	}
	return st.committees
}

func (st *Static) ElectValidators(blockHash common.Hash, newEpoch types.Epoch) types.IDs {
	if len(st.validators) == 0 {
		for i := st.committeeNo + 1; i <= st.committeeNo+st.validatorNo; i++ {
			st.validators = append(st.validators, utils.NewNodeID(i))
		}
	}
	log.Errorf("ElectValidators: %v, %v", st.validators, st.validatorNo)
	return st.validators
}

func (st *Static) ReplaceCommittee(epoch types.Epoch, bzyantineId types.NodeID, nextCommitteeId types.NodeID) types.IDs {
	for i := 0; i < len(st.committees); i++ {
		if st.committees[i] == bzyantineId {
			st.committees[i] = nextCommitteeId
		}
	}
	return nil
}

func (st *Static) ReplaceValidators(epoch types.Epoch, nextCommitteeId types.NodeID, bzyantineId types.NodeID) types.IDs {
	for i := 0; i < st.validatorNo; i++ {
		if st.validators[i] == nextCommitteeId {
			st.validators[i] = bzyantineId
		}
	}
	return nil
}

func (st *Static) ReplaceAllCommittee(epoch types.Epoch) types.IDs {
	committee := st.committees
	st.committees = st.validators
	st.validators = committee

	st.master = st.committees[0]
	return nil
}
