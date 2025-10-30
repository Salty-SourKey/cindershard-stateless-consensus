package snapshot_control_table

import (
	"sync"
	"unishard/blockchain"
	"unishard/types"

	"github.com/ethereum/go-ethereum/common"
)

type (
	SnapshotControlEntity struct {
		Address       common.Address
		Slot          common.Hash
		Value         interface{}
		RTCS          []common.Hash
		GC            types.BlockHeight
		ProtectedFlag bool
	}

	SnapshotControlTable struct {
		Table map[common.Address]map[common.Hash]*SnapshotControlEntity
		Mutex sync.RWMutex
	}
)

// Check if the snapshot corresponding to the given address and slot exists in the SCT
func (sct *SnapshotControlTable) Exist(slot common.Hash, address common.Address) bool {
	defer sct.Mutex.RUnlock()

	sct.Mutex.RLock()
	if _, ea := sct.Table[address]; ea {
		if _, es := sct.Table[address][slot]; es {
			return true
		} else {
			return false
		}
	} else {
		return false
	}
}

// Check the protection flag of the snapshot corresponding to the given address and slot
func (sct *SnapshotControlTable) IsProtected(slot common.Hash, address common.Address) bool {
	if sct.Exist(slot, address) {
		sct.Mutex.RLock()
		pf := sct.Table[address][slot].ProtectedFlag
		sct.Mutex.RUnlock()
		return pf
	}
	return false
}

// Register or update the snapshot in the SCT
func (sct *SnapshotControlTable) UpdateSnapshot(slot common.Hash, address common.Address, value interface{}, rtcs []common.Hash, gc types.BlockHeight, protectedFlag bool) {
	defer sct.Mutex.Unlock()

	sct.Mutex.Lock()
	if _, exist := sct.Table[address]; !exist {
		sct.Table[address] = map[common.Hash]*SnapshotControlEntity{}
	}

	sct.Table[address][slot] = &SnapshotControlEntity{
		Slot:          slot,
		Address:       address,
		Value:         value,
		RTCS:          rtcs,
		GC:            gc,
		ProtectedFlag: protectedFlag,
	}
}

// Mark the snapshots accessed by the given cross-shard transaction as unsafe
func (sct *SnapshotControlTable) ExcludeSnapshot(sn *blockchain.LocalSnapshot) {
	defer sct.Mutex.Unlock()

	sct.Mutex.Lock()
	if _, exist := sct.Table[sn.Address][sn.Slot]; exist {
		sct.Table[sn.Address][sn.Slot].ProtectedFlag = true
	}
}

// Unset the protection flag of the snapshot corresponding to the given address and slot
func (sct *SnapshotControlTable) UnprotectSnapshot(slot common.Hash, address common.Address) {
	defer sct.Mutex.Unlock()

	sct.Mutex.Lock()
	if _, exist := sct.Table[address][slot]; exist {
		sct.Table[address][slot].ProtectedFlag = false
	}
}

// Set the protection flag of the snapshot corresponding to the given address and slot
func (sct *SnapshotControlTable) ProtectSnapshot(slot common.Hash, address common.Address) {
	defer sct.Mutex.Unlock()

	sct.Mutex.Lock()
	if _, exist := sct.Table[address][slot]; exist {
		sct.Table[address][slot].ProtectedFlag = true
	}
}

// Add the given transaction (represented by its hash value) to the RTCS of the snapshot corresponding to the specified address and slot
func (sct *SnapshotControlTable) AppendElementToRTCS(slot common.Hash, address common.Address, txHash common.Hash) {
	defer sct.Mutex.Unlock()

	sct.Mutex.Lock()
	if _, exist := sct.Table[address][slot]; exist {
		sct.Table[address][slot].RTCS = append(sct.Table[address][slot].RTCS, txHash)
	}
}

// Remove the given transaction from the RTCS of the snapshot corresponding to the specified address and slot
func (sct *SnapshotControlTable) RemoveElementFromRTCS(slot common.Hash, address common.Address, txHash common.Hash) {
	defer sct.Mutex.Unlock()

	sct.Mutex.Lock()
	if _, exist := sct.Table[address][slot]; exist {
		for i, h := range sct.Table[address][slot].RTCS {
			if h == txHash {
				sct.Table[address][slot].RTCS = append(sct.Table[address][slot].RTCS[:i], sct.Table[address][slot].RTCS[i+1:]...)
				break
			}
		}
		if len(sct.Table[address][slot].RTCS) == 0 {
			sct.Table[address][slot].ProtectedFlag = false
		}
	}
}

// Return the slots and addresses of snapshots that have an empty RTCS
func (sct *SnapshotControlTable) GetNonEmptyRTCS() (slot []common.Hash, address []common.Address) {
	defer sct.Mutex.Unlock()

	sct.Mutex.Lock()
	for a, s := range sct.Table {
		for sl, e := range s {
			if len(e.RTCS) != 0 {
				slot = append(slot, sl)
				address = append(address, a)
			} else {
				e.ProtectedFlag = false
			}
		}
	}

	return slot, address
}

// Return the snapshot corresponding to the given address and slot
func (sct *SnapshotControlTable) FindSnapshot(slot common.Hash, address common.Address) *blockchain.LocalSnapshot {
	if sct.Exist(slot, address) {
		sct.Mutex.RLock()
		e := sct.Table[address][slot]
		v, ok := e.Value.(string)
		if !ok {
			v = ""
		}
		sct.Mutex.RUnlock()

		return &blockchain.LocalSnapshot{
			Address: e.Address,
			Slot:    e.Slot,
			Value:   v,
			RTCS:    e.RTCS,
		}
	}
	return nil
}
