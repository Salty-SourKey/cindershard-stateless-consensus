package byzantine

import (
	"fmt"
	"time"

	"unishard/types"
)

type ReportByzantine struct {
	types.Shard
	types.Epoch
	types.View
	PublicKey   types.NodeID
	ByzEvidence bool
	Timestamp   time.Time
}
type ReplaceByzantine struct {
	types.Shard
	types.Epoch
	types.View
	OldPublicKey types.NodeID
	NewPublicKey types.NodeID
	ByzEvidence  bool
	TimeStamp    time.Time
}

type MsgReplaceByzantine struct {
	*ReplaceByzantine
}

// MakeBlock creates an unsigned block for Adding view and epoch
func MakeReportByzantine(publicKey types.NodeID, shard types.Shard, epoch types.Epoch, view types.View) *ReportByzantine {
	m := new(ReportByzantine)
	m.Shard = shard
	m.Epoch = epoch
	m.View = view
	m.PublicKey = publicKey
	m.ByzEvidence = true
	m.Timestamp = time.Now()

	return m
}
func MakeReplaceByzantine(reportByzantine ReportByzantine, newPublicKey types.NodeID) *ReplaceByzantine {
	m := new(ReplaceByzantine)
	m.Shard = reportByzantine.Shard
	m.Epoch = reportByzantine.Epoch
	m.View = reportByzantine.View
	m.OldPublicKey = reportByzantine.PublicKey
	m.NewPublicKey = newPublicKey
	m.ByzEvidence = reportByzantine.ByzEvidence
	m.TimeStamp = time.Now()

	return m
}

func (reportByz ReportByzantine) String() string {
	// id := crypto.GetIDFromPubKey(crypto.Decode(reportByz.PublicKey), reportByz.Shard_num)

	return fmt.Sprintf("Report Byzantine - byzNodeid: [%v] / Shard: %v Epoch: %v View: %v", reportByz.PublicKey, reportByz.Shard, reportByz.Epoch, reportByz.View)
}

func (replaceByz ReplaceByzantine) String() string {
	// oldId := crypto.GetIDFromPubKey(crypto.Decode(replaceByz.OldPublicKey), replaceByz.Shard_num)
	// newId := crypto.GetIDFromPubKey(crypto.Decode(replaceByz.NewPublicKey), replaceByz.Shard_num)

	return fmt.Sprintf("Report Byzantine - change byzNode to newNode [%v] -> [%v] / Shard: %v Epoch: %v View: %v", replaceByz.OldPublicKey, replaceByz.NewPublicKey, replaceByz.Shard, replaceByz.Epoch, replaceByz.View)
}
