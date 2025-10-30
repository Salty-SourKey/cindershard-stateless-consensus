package snapshot_control_table

import (
	"unishard/message"

	"github.com/ethereum/go-ethereum/common"
)

type (
	TransactionMap[C any] map[common.Hash][]C
	TransactionRefList    []*message.Transaction
	TransactionKind       int
	AddressSlotPair       struct {
		Address common.Address
		Slot    common.Hash
	}
)

func (trl TransactionRefList) Len() int {
	return len(trl)
}

func (trl TransactionRefList) Less(i, j int) bool {
	return trl[i].Timestamp < trl[j].Timestamp
}

func (trl TransactionRefList) Swap(i, j int) {
	trl[i], trl[j] = trl[j], trl[i]
}

func (pair AddressSlotPair) Equal(i AddressSlotPair) bool {
	if pair.Address != i.Address {
		return false
	}
	if pair.Slot != i.Slot {
		return false
	}
	return true
}

const (
	BalanceTransferOnlyKind TransactionKind = iota
	GeneralSmartContractKind
)
