package graph_dependency

import (
	"unishard/blockchain"
	"unishard/types"

	"github.com/ethereum/go-ethereum/common"
)

type Set struct {
	TransactionId common.Hash
	Write_set     []string
	Read_set      []string
}

func GenerateSet(global_sequence blockchain.Sequence) []Set {
	set_list := make([]Set, 0)
	for _, tx := range global_sequence {
		if tx.TXType == types.SMARTCONTRACT {
			for _, rwSet := range tx.RwSet {
				var t Set
				t.TransactionId = tx.Hash
				for _, w := range rwSet.WriteSet {
					t.Write_set = append(t.Write_set, rwSet.Address.Hex()+"@"+w)
				}
				for _, r := range rwSet.ReadSet {
					t.Read_set = append(t.Read_set, rwSet.Address.Hex()+"@"+r)
				}
				t.Read_set = append(t.Read_set, tx.From.Hex()+"@"+"0")
				t.Write_set = append(t.Write_set, tx.From.Hex()+"@"+"0")
				set_list = append(set_list, t)
			}
		} else if tx.TXType == types.TRANSFER {
			var t Set
			t.TransactionId = tx.Hash
			t.Read_set = append(t.Read_set, []string{tx.From.Hex() + "@" + "0", tx.To.Hex() + "@" + "0"}...)
			t.Write_set = append(t.Write_set, []string{tx.From.Hex() + "@" + "0", tx.To.Hex() + "@" + "0"}...)
			set_list = append(set_list, t)
		}
	}

	return set_list
}
