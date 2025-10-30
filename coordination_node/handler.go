package coordination_node

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"time"

	"unishard/blockchain"
	"unishard/byzantine"
	"unishard/log"
	"unishard/message"
	"unishard/pacemaker"
	"unishard/quorum"
	"unishard/types"
	"unishard/utils"
)

/* Message Handlers */

// Block Consensus Process Start
// Send Block completed consensus to blockbuilder at the process end
func (r *Replica) HandleCoordinationBlockWithoutHeader(blockwithoutheader blockchain.CoordinationBlockWithoutHeader) {
	// log.CDebugf("(HandleCoordinationBlockWithoutHeader) Shard: %v, id: %v Receive BlockData", r.Shard(), r.ID())

	r.receivedNo++
	r.startSignal()
	// when get block kicks off the protocol
	if r.pm.GetCurView() == 0 {
		// log.CDebugf("[%v] start protocol ", r.ID())
		r.pm.AdvanceView(0)
	}

	// If Leader, process block consensus. Else drop block
	if !r.IsLeader(r.ID(), r.pm.GetCurView(), r.pm.GetCurEpoch()) {
		return
	}

	// log.CDebugf("test committee sig for block:  %#v", blockwithoutheader)
	json.Marshal(blockwithoutheader)
	// log.CDebugf("coordination block serialized (header hash = %s, seq= %d): %s", crypto.MakeID(en), blockwithoutheader.CoordinationBlockData.SequenceNumber, en)
	// isVerify, err := crypto.PubVerify(
	// 	blockwithoutheader.CommitteeSignature[0],
	// 	crypto.IDToByte(blockwithoutheader.MakeHash(blockwithoutheader.CoordinationBlockData)),
	// )

	// if err != nil {
	// 	log.Errorf("[%v] Blockbuilder signature verify error", r.ID())

	// 	return
	// }
	// if !isVerify {
	// 	log.Errorf("[%v] Invalid Blockbuilder Signature", r.ID())

	// 	return
	// }

	r.proposeCandidate <- &blockwithoutheader
}

func (r *Replica) HandleCoordinationBlock(block blockchain.CoordinationBlock) {
	// log.CDebugf("HandleCoordinationBlock: %#v", block)
	r.receivedNo++
	r.startSignal()
	// when get block kicks off the protocol
	if r.pm.GetCurView() == 0 {
		// log.CDebugf("[%v] start protocol ", r.ID())
		r.pm.AdvanceView(0)
	}

	if !r.IsCommittee(r.ID(), block.CoordinationBlockHeader().Epoch) {
		return
	}

	if block.BlockData.GlobalCoordinationSequence == nil {
		block.BlockData.GlobalCoordinationSequence = blockchain.Sequence{}
	}
	if block.BlockData.GlobalSnapshot == nil {
		block.BlockData.GlobalSnapshot = blockchain.LocalSnapshots{}
	}
	if block.BlockData.GlobalContractBundle == nil {
		block.BlockData.GlobalContractBundle = []*blockchain.LocalContract{}
	}

	r.eventChan <- block
}

func (r *Replica) HandleTxn(m message.Transaction) {
	// isValid, _ := r.Safety.ValidateTransaction(&m)
	// if isValid {
	// log.StateInfo(m)
	// r.pd.AddTxn(&m)
	// }
	r.startSignal()
	// the first leader kicks off the protocol
	if r.pm.GetCurView() == 0 {
		// log.CDebugf("[%v] start protocol ", r.ID())
		r.pm.AdvanceView(0)
	}

	if m.TXType == types.ABORT {
		epoch, view := r.pm.GetCurEpochView()
		if !r.IsLeader(r.ID(), view, epoch) {
			r.Send(r.FindLeaderFor(view, epoch), m)
			return
		}
		// log.Warning(m)
		r.pd.CollectTxn(&m)
	} else {
		r.pd.AddTxn(&m)
	}
}

// hotstuff.go 에서 send를 통해 보내진 vote를 처리하는 handler / GG
func (r *Replica) HandleVote(vote quorum.Collection[quorum.Vote]) {
	r.startSignal()
	if r.State() == types.VIEWCHANGING {
		return
	}

	if !r.IsCommittee(r.ID(), vote.Epoch) {
		return
	}

	// prevent voting last blockheight
	if vote.BlockHeight < r.GetHighBlockHeight() {
		return
	}

	// log.CDebugf("[%v] (HandleVote) received vote from [%v], blockHeight: %v view: %v epoch: %v ID: %x", r.ID(), vote.Publisher, vote.BlockHeight, vote.View, vote.Epoch, vote.BlockID)
	r.eventChan <- vote
}

// hotstuff.go 에서 prepare 단계에서 broadcast를 통해 보내진 vote를 commit하는 핸들러 / GG / 내가만든쿠키
func (r *Replica) HandleCommit(commitMsg quorum.Collection[quorum.Commit]) {
	r.startSignal()
	if r.State() == types.VIEWCHANGING {
		return
	}

	if !r.IsCommittee(r.ID(), commitMsg.Epoch) {
		return
	}

	if commitMsg.BlockHeight < r.GetLastBlockHeight() {
		return
	} //에러케이스 거르는 코드임 위에 코드가.
	// log.CDebugf("[%v] (HandleCommit) received commit from [%v], blockheight: %v view: %v epoch: %v ID: %x", r.ID(), commitMsg.Publisher, commitMsg.BlockHeight, commitMsg.View, commitMsg.Epoch, commitMsg.BlockID)
	r.eventChan <- commitMsg
}

func (r *Replica) HandleAccept(accept blockchain.CoordinationAccept) {
	r.startSignal()

	if r.pm.GetCurView() == 0 {
		// log.CDebugf("[%v] start protocol ", r.ID())
		r.pm.AdvanceView(0)
	}

	commitedBlock := accept.CommittedBlock
	if commitedBlock.CoordinationBlockHeader().BlockHeight < r.GetLastBlockHeight() {
		return
	} //에러케이스 거르는 코드임 위에 코드가.
	r.eventChan <- accept
}

func (r *Replica) HandleTmo(tmo pacemaker.CoordinationTMO) {
	if tmo.View < r.pm.GetCurView() {
		return
	}

	if !r.IsCommittee(r.ID(), tmo.Epoch) {
		return
	}

	// log.CDebugf("[%v] (HandleTmo) received a timeout from %v for view %v", r.ID(), tmo.NodeID, tmo.View)
	r.eventChan <- tmo
}

func (r *Replica) HandleTc(tc pacemaker.CoordinationTC) {
	if !(tc.NewView > r.pm.GetCurView()) {
		return
	}
	// log.CDebugf("[%v] (HandleTc) received a tc from %v for view %v", r.ID(), tc.NodeID, tc.NewView)
	r.eventChan <- tc
}

func (r *Replica) HandleReportByzantine(reportByzantine byzantine.ReportByzantine) {
	// log.CDebugf("[%v] report Byzantine request for public Key %v", r.ID(), reportByzantine.PublicKey)

	r.startSignal()

	r.eventChan <- reportByzantine
}

func (r *Replica) HandleReplaceByzantine(replaceByzantine byzantine.ReplaceByzantine) {
	// log.CDebugf("[%v] replace Byzantine request for old public key %v to new public key %v", r.ID(), replaceByzantine.OldPublicKey, replaceByzantine.NewPublicKey)
	r.startSignal()
	r.eventChan <- replaceByzantine
}

// handleQuery replies a query with the statistics of the node
func (r *Replica) handleQuery(m message.Query) {
	//realAveProposeTime := float64(r.totalProposeDuration.Milliseconds()) / float64(r.processedNo)
	//aveProcessTime := float64(r.totalProcessDuration.Milliseconds()) / float64(r.processedNo)
	//aveVoteProcessTime := float64(r.totalVoteTime.Milliseconds()) / float64(r.roundNo)
	//aveBlockSize := float64(r.totalBlockSize) / float64(r.proposedNo)
	//requestRate := float64(r.pd.TotalReceivedTxNo()) / time.Since(r.startTime).Seconds()
	//committedRate := float64(r.committedNo) / time.Since(r.startTime).Seconds()
	//aveRoundTime := float64(r.totalRoundTime.Milliseconds()) / float64(r.roundNo)
	//aveProposeTime := aveRoundTime - aveProcessTime - aveVoteProcessTime
	latency := float64(r.totalDelay.Milliseconds()) / float64(r.latencyNo)
	r.thrus += fmt.Sprintf("Time: %v s. Throughput: %v txs/s\n", time.Since(r.startTime).Seconds(), float64(r.totalCommittedTx)/time.Since(r.tmpTime).Seconds())
	r.totalCommittedTx = 0
	r.tmpTime = time.Now()
	status := fmt.Sprintf("Latency: %v\n%s", latency, r.thrus)
	//status := fmt.Sprintf("chain status is: %s\nCommitted rate is %v.\nAve. block size is %v.\nAve. trans. delay is %v ms.\nAve. creation time is %f ms.\nAve. processing time is %v ms.\nAve. vote time is %v ms.\nRequest rate is %f txs/s.\nAve. round time is %f ms.\nLatency is %f ms.\nThroughput is %f txs/s.\n", r.Safety.GetChainStatus(), committedRate, aveBlockSize, aveTransDelay, aveCreateDuration, aveProcessTime, aveVoteProcessTime, requestRate, aveRoundTime, latency, throughput)
	//status := fmt.Sprintf("Ave. actual proposing time is %v ms.\nAve. proposing time is %v ms.\nAve. processing time is %v ms.\nAve. vote time is %v ms.\nAve. block size is %v.\nAve. round time is %v ms.\nLatency is %v ms.\n", realAveProposeTime, aveProposeTime, aveProcessTime, aveVoteProcessTime, aveBlockSize, aveRoundTime, latency)
	m.Reply(message.QueryReply{Info: status})
}

// handleRequestLeader replies a Leader of the node
func (r *Replica) handleRequestLeader(m message.RequestLeader) {

	r.startSignal()

	if r.pm.GetCurView() == 0 {
		// log.CDebugf("[%v] start protocol ", r.ID())
		r.pm.AdvanceView(0)
	}

	leader := r.FindLeaderFor(r.pm.GetCurView(), r.pm.GetCurEpoch())
	m.Reply(message.RequestLeaderReply{Leader: fmt.Sprint(leader)})
}

func (r *Replica) handleReportByzantine(m message.ReportByzantine) {

	epoch, view := r.pm.GetCurEpochView()
	// epoch, view := types.Epoch(1), types.View(1)
	committees := r.FindCommitteesFor(epoch)
	log.CDebug(committees)
	rnd := rand.New(rand.NewSource(time.Now().UnixNano()))
	var publicKey types.NodeID
	for {
		publicKey = committees[rnd.Intn(utils.Len(committees))]
		if !r.IsLeader(publicKey, view, epoch) {
			// log.CDebugf("[%v] Old PubKey Node Id: %v", r.ID(), publicKey)
			break
		}
	}
	reportByzantine := byzantine.MakeReportByzantine(publicKey, r.Shard(), epoch, view)

	// replace to Leader of current view
	leader := r.Static.FindLeaderFor(view, epoch)
	if leader == r.ID() {
		r.HandleReportByzantine(*reportByzantine)
	} else {
		r.Send(leader, reportByzantine)
	}
}
