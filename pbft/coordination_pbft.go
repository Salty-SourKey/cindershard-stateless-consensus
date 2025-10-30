package pbft

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"unishard/blockchain"
	"unishard/byzantine"
	"unishard/config"
	"unishard/crypto"
	"unishard/election"
	"unishard/log"
	"unishard/node"
	"unishard/pacemaker"
	"unishard/quorum"
	"unishard/types"
	"unishard/utils"

	"github.com/ethereum/go-ethereum/common"
)

type CoordinationPBFT struct {
	node.Node
	*election.Static
	pm                   *pacemaker.CoordinationPacemaker
	lastVotedBlockHeight types.BlockHeight
	preferredBlockHeight types.BlockHeight
	highQC               *quorum.QC
	highCQC              *quorum.QC
	bc                   *blockchain.BlockChain
	voteQuorum           *quorum.Quorum[quorum.Vote]
	commitQuorum         *quorum.Quorum[quorum.Commit]
	committedBlocks      chan *blockchain.CoordinationBlock
	forkedBlocks         chan *blockchain.CoordinationBlock
	reservedBlock        chan *blockchain.CoordinationBlock
	bufferedQCs          map[common.Hash]*quorum.QC
	bufferedCQCs         map[common.Hash]*quorum.QC
	updatedQC            chan *quorum.QC
	lastCreatedBlockQC   *quorum.QC
	bufferedBlocks       map[types.Epoch]map[types.View]map[types.BlockHeight]*blockchain.CoordinationBlock
	bufferedAccepts      map[types.Epoch]map[types.View]map[types.BlockHeight]*blockchain.CoordinationAccept
	agreeingBlocks       map[types.Epoch]map[types.View]map[types.BlockHeight]*blockchain.CoordinationBlock
	mu                   sync.Mutex

	// stateOf map[types.BlockHeight]types.BlockStatus
	detectedTmos map[types.NodeID]*pacemaker.CoordinationTMO
}

func NewCoordinationPBFT(
	node node.Node,
	pm *pacemaker.CoordinationPacemaker,
	elec *election.Static,
	committedBlocks chan *blockchain.CoordinationBlock,
	forkedBlocks chan *blockchain.CoordinationBlock,
	reservedBlock chan *blockchain.CoordinationBlock) *CoordinationPBFT {
	pb := new(CoordinationPBFT)
	pb.Node = node
	pb.Static = elec
	pb.pm = pm
	pb.bc = blockchain.NewCoordinationBlockchain(pb.Shard(), config.GetConfig().DefaultBalance)
	pb.voteQuorum = quorum.NewQuorum[quorum.Vote](config.GetConfig().CommitteeNumber)
	pb.commitQuorum = quorum.NewQuorum[quorum.Commit](config.GetConfig().CommitteeNumber)
	pb.bufferedBlocks = make(map[types.Epoch]map[types.View]map[types.BlockHeight]*blockchain.CoordinationBlock)
	pb.bufferedAccepts = make(map[types.Epoch]map[types.View]map[types.BlockHeight]*blockchain.CoordinationAccept)
	pb.agreeingBlocks = make(map[types.Epoch]map[types.View]map[types.BlockHeight]*blockchain.CoordinationBlock)
	pb.bufferedQCs = make(map[common.Hash]*quorum.QC)
	pb.highQC = &quorum.QC{View: 0}
	pb.bufferedCQCs = make(map[common.Hash]*quorum.QC)
	pb.lastCreatedBlockQC = &quorum.QC{View: 0}
	pb.updatedQC = make(chan *quorum.QC, 1)
	pb.highCQC = &quorum.QC{View: 0}
	pb.committedBlocks = committedBlocks
	pb.forkedBlocks = forkedBlocks
	pb.reservedBlock = reservedBlock
	// pb.stateOf = make(map[types.BlockHeight]types.BlockStatus)
	pb.detectedTmos = make(map[types.NodeID]*pacemaker.CoordinationTMO)
	return pb
}

func (pb *CoordinationPBFT) GetBlockChain() *blockchain.BlockChain {
	return pb.bc
}

func (pb *CoordinationPBFT) CreateCoordinationBlock(blockwithoutBlockHeader *blockchain.CoordinationBlockWithoutHeader) *blockchain.CoordinationBlock {

	// revert expected State if prev block is failed
	// pb.bc.RevertExpectedState()

	// sorting payload for executing crosschain at first in order
	// sort.Slice(payload, func(i int, j int) bool {
	// 	return payload[i].TXType > payload[j].TXType
	// })
	// wait qc from updatedQC channel

	var qc *quorum.QC
	if pb.highQC.View == 0 {
		qc = pb.highQC
	} else if pb.highQC.BlockHeight == pb.lastCreatedBlockQC.BlockHeight {
		qc = <-pb.updatedQC
	} else {
		qc = pb.highQC
		<-pb.updatedQC
	}

	block := blockchain.CreateCoordinationBlock(
		blockwithoutBlockHeader,
		pb.pm.GetCurEpoch(),
		pb.pm.GetCurView(),
		pb.GetLastBlockHeight(),
		pb.bc.GetCurrentStateHash(),
		qc.BlockID,
		pb.GetHighBlockHeight(),
		qc,
	)
	pb.lastCreatedBlockQC = block.(*blockchain.CoordinationBlock).QC

	return block.(*blockchain.CoordinationBlock)
}

/* Consensus Process */
func (pb *CoordinationPBFT) ProcessCoordinationBlock(block *blockchain.CoordinationBlock) error {

	curEpoch := pb.pm.GetCurEpoch()
	curBlockHeight := pb.GetHighBlockHeight()

	// log.CDebugf("[%v] (ProcessCoordinationBlock) processing block from leader %v blockHeight %v view %v epoch %v ID %x", pb.ID(), block.Proposer, block.BlockHeader.BlockHeight, block.BlockHeader.View, block.BlockHeader.Epoch, block.BlockHash)

	if block.BlockHeader.Epoch > curEpoch {
		utils.AddEntity(pb.bufferedBlocks, block.BlockHeader.Epoch, block.BlockHeader.View, block.BlockHeader.BlockHeight-1, block)

		return nil
	}

	// Coordination Block Builder의 Worker Block 수신
	// log.CDebugf("[%v] (ProcessCoordinationBlock) processing block start", pb.ID())
	if pb.ID() == "1" {
		log.CDebugf("[%v] [Pre-Prepare] [In progress] received worker block [%v]", pb.ID(), block.BlockHeader.BlockHeight)
	}

	// 블록 검증 진행 (QC 유효성 검증(processCertificateBlock))
	err := pb.processCertificateBlock(block)
	if err != nil {
		log.Errorf("[%v] (ProcessCoordinationBlock) failed certificate block: %v", pb.ID(), err)

		return err
	}

	// 앞선 Block이 오는 경우, Buffer에 보관하여 나중에 처리함
	// Block 순서를 맞추기 위함
	if block.BlockHeader.BlockHeight > curBlockHeight+1 { // 일반 합의의 경우, Vote이전에 받음
		// buffer the block for which it can be processed after processing the block of block height - 1
		utils.AddEntity(pb.bufferedBlocks, block.BlockHeader.Epoch, block.BlockHeader.View, block.BlockHeader.BlockHeight-1, block)
		// pb.bufferedBlocks[types.Epoch(block.BlockHeader.BlockHeight)-1] = block
		return nil
	} else if pb.State() == types.VIEWCHANGING { // View Change의 경우
		utils.AddEntity(pb.bufferedBlocks, block.BlockHeader.Epoch, block.BlockHeader.View, block.BlockHeader.BlockHeight, block)
		// pb.bufferedBlocks[types.Epoch(block.BlockHeader.BlockHeight)] = block
		return nil
	}

	// does not have to process the QC if the replica is the proposer

	// log.StateCDebugf("[%v] blockheight %v stateHash %x", pb.ID(), block.BlockHeight, pb.bc.GetCurrentStateHash())
	if block.BlockHeader.StateRoot != pb.bc.GetCurrentStateHash() {
		// pb.bc.RevertExpectedState()
		log.Warningf("[%v] (ProcessWorkerBlock) stateHash not valid blockID %x", pb.ID(), block.BlockHash)

		return nil
	}

	// after verify block, set prepared for node to start consensus
	// pb.pm.AdvanceView(block.BlockHeader.View)
	pb.SetState(types.PREPARED)

	// // 블록 삽입
	// 합의중 블록 대기열에 삽입
	utils.AddEntity(pb.agreeingBlocks, block.BlockHeader.Epoch, block.BlockHeader.View, block.BlockHeader.BlockHeight, block)

	// pb.agreeingBlocks[block.Epoch][block.View][block.BlockHeight] = block

	// pb.pt.AddPendingTransactions(block.SCPayload)
	// process buffered QC
	// 현재 Block에 대해, qc가 이미 발급된 경우, 바로 certificate 실행
	qc, ok := pb.bufferedQCs[block.BlockHash]
	if ok {
		pb.processCertificateVote(qc)
		delete(pb.bufferedQCs, block.BlockHash)
		vote := quorum.MakeCollection[quorum.Vote](
			block.BlockHeader.Epoch,
			block.BlockHeader.View,
			block.BlockHeader.BlockHeight,
			pb.ID(),
			block.BlockHash,
		)

		pb.BroadcastToSome(pb.FindCommitteesFor(block.BlockHeader.Epoch), vote)

		return nil
	}

	// 투표 규칙(votingRule)에 따라 Vote 메시지 생성 및 브로드캐스트
	shouldVote, err := pb.votingRule(block)
	if err != nil {
		// pb.bc.RevertExpectedState()
		log.Errorf("[%v] (ProcessWorkerBlock) cannot decide whether to vote the block: %v", pb.ID(), err)

		return err
	}
	if !shouldVote {
		// pb.bc.RevertExpectedState()
		log.Warningf("[%v] (ProcessWorkerBlock) is not going to vote for block ID %x", pb.ID(), block.BlockHash)

		return nil
	}

	vote := quorum.MakeCollection[quorum.Vote](
		block.BlockHeader.Epoch,
		block.BlockHeader.View,
		block.BlockHeader.BlockHeight,
		pb.ID(),
		block.BlockHash,
	)

	// Coordination Shard Leader의 Coordination Block 제안
	if pb.ID() == "1" {
		log.CDebugf("[%v] [Pre-Prepare] [Complete] proposing coordination block [%v]", pb.ID(), block.BlockHeader.BlockHeight)
	}

	// vote is sent to the next leader
	pb.BroadcastToSome(pb.FindCommitteesFor(block.BlockHeader.Epoch), vote)
	// 자신의 투표 처리(ProcessVote)
	pb.ProcessVote(vote)

	return nil
}

func (pb *CoordinationPBFT) ProcessVote(vote *quorum.Collection[quorum.Vote]) {
	// 현재 view의 STATE를 체크
	// Commit이면, 본 함수 진행하지 않음
	// 현재 view보다 낮을 경우, 다음 view 진행

	// 투표 메시지 수신 → 서명 검증 → 쿼럼(Quorum) 체크

	voteIsVerified, err := crypto.PubVerify(vote.Signature, crypto.IDToByte(vote.BlockID))
	if err != nil {
		log.Errorf("[%v] (ProcessVote) error in verifying the signature in vote ID %x", pb.ID(), vote.BlockID)

		return
	}
	if !voteIsVerified {
		log.Warningf("[%v] (ProcessVote) received a vote with invalid signature ID %x", pb.ID(), vote.BlockID)

		return
	}

	// Coordination Shard Leader의 Vote 수신(prepare phase 진행 중)
	// log.CDebugf("[%v] (ProcessVote) processing vote for blockHeight %v view %v epoch %v ID %x from %v", pb.ID(), vote.BlockHeight, vote.View, vote.Epoch, vote.BlockID, vote.Publisher)
	log.CDebugf("[%v] [Prepare] [In progress] received prepare vote for block [%v]", pb.ID(), vote.BlockHeight)

	// 충분한 투표가 모이면 QC 생성
	isBuilt, qc := pb.voteQuorum.Add(vote)
	if !isBuilt {
		// log.CDebugf("[%v] (ProcessVote) votes are not sufficient to build a qc ID %x", pb.ID(), vote.BlockID)

		return
	}
	// 진행하던 view의 STATE 업데이트: pprepare -> prepare
	// pb.updateStateOfBlock(vote.BlockHeight, types.PREPARED)

	qc.Leader = pb.ID()

	// 블록이 아직 도착하지 않았으면 QC를 버퍼에 저장
	// buffer the QC if the block has not been received
	_, exist := pb.agreeingBlocks[qc.Epoch][qc.View][qc.BlockHeight]
	if !exist {
		pb.bufferedQCs[qc.BlockID] = qc
		// log.CDebugf("[%v] (ProcessVote) the qc is buffered for not arrived block ID %x", pb.ID(), qc.BlockID)

		return
	}

	// Coordination Shard Leader의 Quorum 생성 및 Quorum Certificate 전파(prepare phase 완료)
	// log.CDebugf("[%v] (ProcessVote) finished processing vote for blockHeight %v view %v epoch %v ID %x", pb.ID(), vote.BlockHeight, vote.View, vote.Epoch, vote.BlockID)
	log.CDebugf("[%v] [Prepare] [Complete] formed quorum and broadcasted QC for block [%v]", pb.ID(), vote.BlockHeight)

	// 도착한 블록이 있으면 processCertificateVote(qc) 호출
	pb.processCertificateVote(qc)

}

func (pb *CoordinationPBFT) ProcessCommit(commit *quorum.Collection[quorum.Commit]) {
	// Commit 메시지 수신 → 서명 검증 → 쿼럼 체크 → CQC 생성

	commitIsVerified, err := crypto.PubVerify(commit.Signature, crypto.IDToByte(commit.BlockID))
	if err != nil {
		log.Errorf("[%v] (ProcessCommit) error in verifying the signature in commit ID %x", pb.ID(), commit.BlockID)

		return
	}
	if !commitIsVerified {
		log.Warningf("[%v] (ProcessCommit) received a commit with invalid signature ID %x", pb.ID(), commit.BlockID)

		return
	}

	// Coordination Shard Leader의 Vote 수신(commit phase 진행 중)
	// log.CDebugf("[%v] (ProcessCommit) processing commit for blockHeight %v view %v epoch %v ID %x", pb.ID(), commit.BlockHeight, commit.View, commit.Epoch, commit.BlockID)
	log.CDebugf("[%v] [Commit] [In progress] received commit vote for block [%v]", pb.ID(), commit.BlockHeight)

	isBuilt, cqc := pb.commitQuorum.Add(commit)
	if !isBuilt {
		// log.CDebugf("[%v] (ProcessCommit) commits are not sufficient to build a cqc ID %x", pb.ID(), commit.BlockID)

		return
	}

	// 진행하던 view의 STATE 업데이트: prepare -> commit
	// pb.updateStateOfBlock(commit.BlockHeight, types.COMMITTED)

	cqc.Leader = pb.ID()
	// buffer the QC if the block has not been received
	// which means qc is not certificated

	// Coordination Shard Leader의 Certificate Quorum 생성 및 Certificate Quorum Certificate 전파(commit phase 완료)
	// log.CDebugf("[%v] (ProcessCommit) finished processing commit for blockHeight %v view %v epoch %v ID %x", pb.ID(), cqc.BlockHeight, cqc.View, cqc.Epoch, cqc.BlockID)
	// log.CDebugf("[%v] [Commit] [Complete] formed CQ and for block [%v]", pb.ID(), cqc.BlockHeight)

	pb.processCertificateCqc(cqc)

}

// Validator가 Commit된 Block을 지속적으로 Sync하기 위해서 필요한 함수
// Committee가 Commit을 완료한 Block에 대하여 Leader가 Validator에게 전송함
func (pb *CoordinationPBFT) ProcessAccept(accept *blockchain.CoordinationAccept) {
	if pb.IsCommittee(pb.ID(), accept.CommittedBlock.BlockHeader.Epoch) {
		return
	}
	// block 검증
	block := accept.CommittedBlock

	curEpoch := pb.pm.GetCurEpoch()
	curBlockHeight := pb.GetHighBlockHeight()

	if block.BlockHeader.Epoch > curEpoch {
		utils.AddEntity(pb.bufferedAccepts, block.BlockHeader.Epoch, block.BlockHeader.View, block.BlockHeader.BlockHeight-1, accept)

		return
	}

	err := pb.processCertificateBlock(block)
	if err != nil {
		log.Errorf("[%v] (ProcessAccept) failed certificate block: %v", pb.ID(), err)

		return
	}

	// 앞선 Block이 오는 경우, Buffer에 보관하여 나중에 처리함
	// Block 순서를 맞추기 위함
	if block.BlockHeader.BlockHeight > curBlockHeight+1 { // 일반 합의의 경우, Vote이전에 받음
		// buffer the block for which it can be processed after processing the block of block height - 1
		utils.AddEntity(pb.bufferedAccepts, block.BlockHeader.Epoch, block.BlockHeader.View, block.BlockHeader.BlockHeight-1, accept)
		// pb.bufferedBlocks[block.BlockHeight-1] = block

		// log.CDebugf("[%v] (ProcessAccept) the accept is buffered for large blockheight ID %x", pb.ID(), block.BlockHash)
		return
	}

	// Payload 실행 및 StateHash 비교
	// does not have to process the QC if the replica is the proposer
	if block.Proposer != pb.ID() {
		// log.StateCDebugf("[%v] blockheight %v stateHash %x", pb.ID(), block.BlockHeight, pb.bc.GetCurrentStateHash())
		if block.BlockHeader.StateRoot != pb.bc.GetCurrentStateHash() {
			// pb.bc.RevertExpectedState()
			log.Warningf("[%v] (ProcessAccept) stateHash not valid ID %x blockHeight %v", pb.ID(), block.BlockHash, block.BlockHeader.BlockHeight)
			return
		}
	}

	// Block 삽입
	pb.bc.AddCoordinationBlock(block)

	// State Update
	// pb.bc.UpdateState()

	// update QC
	_ = pb.updatePreferredBlockHeight(block.CQC)
	_ = pb.updateLastVotedBlockHeight(block.BlockHeader.BlockHeight)
	pb.updateHighQC(block.CQC)

	// Elect Committees, if we should
	if pb.pm.IsTimeToElect() {
		// newEpoch := pb.pm.UpdateEpoch()
		curEpoch := pb.pm.GetCurEpoch()
		committes := pb.ElectCommittees(block.BlockHash, curEpoch+1)
		if committes != nil {
			// log.CDebugf("[%v] (ProcessAccept) elect new committees %v for new epoch %v", pb.ID(), committes, curEpoch+1)
		}
	}

	// update view
	pb.pm.AdvanceView(block.BlockHeader.View)

	// block cqc를 통해 ProcessCertificateCQC 진행
	pb.updateHighCQC(block.CQC)

	// pb.pt.AddProcessedTransactions(block.SCPayload)

	// commit Block
	err = pb.commitBlock(block.CQC)

	if err != nil {
		log.Errorf("[%v] (ProcessAccept) failed commit Block: %v", pb.ID(), err)
		return
	}
	curEpoch, curView := pb.pm.GetCurEpochView()

	// accept 완료 후 다음 Accept 실행
	if accept, ok := pb.bufferedAccepts[curEpoch][curView][block.BlockHeader.BlockHeight]; ok { // Crosschain 합의
		pb.ProcessAccept(accept)
		delete(pb.bufferedAccepts[curEpoch][curView], block.BlockHeader.BlockHeight)
	}

	// Committee 가 아닌 경우 종료
	if !pb.IsCommittee(pb.ID(), curEpoch) {
		return
	}
	// accept 완료 후 다음 LockReq or block 실행
	if block, exist := pb.bufferedBlocks[curEpoch][curView][block.BlockHeader.BlockHeight]; exist { // Crosschain 합의
		_ = pb.ProcessCoordinationBlock(block)
		delete(pb.bufferedBlocks[curEpoch][curView], block.BlockHeader.BlockHeight)
	}
}

/* Certificate */
func (pb *CoordinationPBFT) processCertificateBlock(block *blockchain.CoordinationBlock) error {

	qc := block.QC
	if qc.BlockHeight < pb.GetHighBlockHeight() {
		return fmt.Errorf("qc is not valid, blockheight %v", qc.BlockHeight)
	}

	if block.QC.Leader != pb.ID() { // 리더가 이전에 정족수 이상의 QC를 올바르게 채웠는지에 대한 검증
		quorumIsVerified, _ := crypto.VerifyQuorumSignature(qc.AggSig, qc.BlockID, qc.Signers)
		if !quorumIsVerified {
			return errors.New("received a quorum with invalid signatures")
		}
	}

	curEpoch, curView := pb.pm.GetCurEpochView()
	curBlockHeight := pb.GetHighBlockHeight()

	// block view에 대하여 알맞은 리더가 요청했는지 판단
	if !pb.Static.IsLeader(block.Proposer, block.BlockHeader.View, block.BlockHeader.Epoch) {
		return fmt.Errorf("received a proposal view %v epoch %v from an invalid leader %v", block.BlockHeader.View, block.BlockHeader.Epoch, block.Proposer)
	}

	// 지난 View를 바라보고 있는지 혹은 지난 block Height인지 판단함
	if block.BlockHeader.Epoch < curEpoch || block.BlockHeader.View < curView || block.BlockHeader.BlockHeight < curBlockHeight {
		return fmt.Errorf("received a stale proposal from %v view %v blockheight %v", block.Proposer, block.BlockHeader.View, block.BlockHeader.BlockHeight)
	}

	// 블록 서명 검증
	if block.Proposer != pb.ID() {
		blockIsVerified, _ := crypto.PubVerify(block.CommitteeSignature[1], crypto.IDToByte(block.BlockHash))
		if !blockIsVerified {
			return errors.New("received a block with an invalid signature")
		}
	}

	// QC 체크
	if block.QC == nil {
		return errors.New("the block should contain a QC")
	}

	// Block ID 검사: to do

	if pb.IsByz() && config.GetConfig().Strategy == FORK && pb.IsLeader(pb.ID(), qc.View+1, pb.pm.GetCurEpoch()) {
		pb.pm.AdvanceView(qc.View)
		return nil
	}

	// log.CDebugf("[%v] (processCertificateBlock) finished processing certificate block blockHeight: %v view: %v epoch: %v ID: %x", pb.ID(), block.BlockHeader.BlockHeight, block.BlockHeader.View, block.BlockHeader.Epoch, block.BlockHash)

	return nil
}

func (pb *CoordinationPBFT) processCertificateVote(qc *quorum.QC) {
	// log.CDebugf("[%v] (processCertificateVote) processing quorum certification blockHeight %v view %v epoch %v ID %x", pb.ID(), qc.BlockHeight, qc.View, qc.Epoch, qc.BlockID)

	if qc.BlockHeight < pb.GetHighBlockHeight() {
		return
	}

	block, exist := pb.agreeingBlocks[qc.Epoch][qc.View][qc.BlockHeight]
	if !exist {
		return
	}

	// 블록체인에 블록 삽입
	pb.bc.AddCoordinationBlock(block)
	delete(pb.agreeingBlocks[qc.Epoch][qc.View], qc.BlockHeight)
	// log.CDebugf("[%v] (processCertificateVote) put block into blockchain", pb.ID())
	// pb.bc.UpdateState()

	if pb.IsByz() && config.GetConfig().Strategy == FORK && pb.IsLeader(pb.ID(), qc.View+1, pb.pm.GetCurEpoch()) {
		pb.pm.AdvanceView(qc.View)
		return
	}

	err := pb.updatePreferredBlockHeight(qc)
	if err != nil {
		pb.bufferedQCs[qc.BlockID] = qc
		// log.CDebugf("[%v] (processCertificateVote) a qc is buffered for not ready to certificate ID %x: %v", pb.ID(), qc.BlockID, err)
		return
	}

	err = pb.updateLastVotedBlockHeight(qc.BlockHeight)
	if err != nil {
		pb.bufferedQCs[qc.BlockID] = qc
		// log.CDebugf("[%v] (processCertificateVote) a qc is buffered for not ready to certificate ID %x: %v", pb.ID(), qc.BlockID, err)
		return
	}

	pb.updateHighQC(qc)

	if pb.pm.IsTimeToElect() {
		// newEpoch := pb.pm.UpdateEpoch()
		curEpoch := pb.pm.GetCurEpoch()
		committes := pb.ElectCommittees(block.BlockHash, curEpoch+1)
		if committes != nil {
			// log.CDebugf("[%v] (processCertificateVote) elect new committees %v for new epoch %v", pb.ID(), committes, curEpoch+1)
		}
	}

	pb.pm.AdvanceView(qc.View)

	// after complete voting, set ready for node to start newview
	pb.SetState(types.READY)
	// log.CDebugf("[%v] (processCertificateVote) finished processing quorum certification blockHeight %v view %v epoch %v ID %x", pb.ID(), qc.BlockHeight, qc.View, qc.Epoch, qc.BlockID)

	curEpoch, curView := pb.pm.GetCurEpochView()
	// Vote 완료 후, 미리 받은 "다음" LockReq 혹은 Block 합의 실행
	if block, ok := pb.bufferedBlocks[curEpoch][curView][qc.BlockHeight]; ok { // 일반합의
		_ = pb.ProcessCoordinationBlock(block)
		delete(pb.bufferedBlocks[curEpoch][curView], qc.BlockHeight)
	}

	// Exchange 다음 Leader 에게 전달
	// View가 넘어가 Leader가 바뀌면, 다음 Leader 제외하고 진행
	// if qc.View < curView && !pb.IsLeader(pb.ID(), curView, curEpoch) {
	// 	// pool 전체 retrieve
	// 	lockedTransactionsForPushing := pb.cc.GetAllLockedTransactions()
	// 	if len(lockedTransactionsForPushing) > 0 {
	// 		pb.Send(pb.FindLeaderFor(curView, curEpoch), lockedTransactionsForPushing)
	// 	}
	// }

	if cqc, ok := pb.bufferedCQCs[qc.BlockID]; ok {
		pb.processCertificateCqc(cqc)
		delete(pb.bufferedCQCs, qc.BlockID)
	}

	commit := quorum.MakeCollection[quorum.Commit](qc.Epoch, qc.View, qc.BlockHeight, pb.ID(), qc.BlockID)
	pb.BroadcastToSome(pb.FindCommitteesFor(commit.Epoch), commit)

	pb.ProcessCommit(commit)
}

func (pb *CoordinationPBFT) processCertificateCqc(cqc *quorum.QC) {
	curBlock, _ := pb.bc.GetBlockByID(cqc.BlockID)
	curCoordinationBlock := curBlock.(*blockchain.CoordinationBlock)

	// log.CDebugf("[%v] (processCertificateCqc) processing commit quorum certification blockHeight %v view %v epoch %v ID %x", pb.ID(), cqc.BlockHeight, cqc.View, cqc.Epoch, cqc.BlockID)

	if cqc.BlockHeight < pb.GetLastBlockHeight() {
		// log.CDebugf("[%v] (processCertificateCqc) exited by height: cqc height= %d < Last height= %d", pb.ID(), cqc.BlockHeight, pb.GetLastBlockHeight())
		return
	}

	pb.updateHighCQC(cqc)

	// make sc payload confirm
	// var wg sync.WaitGroup
	// for _, tx := range curBlock.SCPayload {
	// 	// wg.Add(1)
	// 	// go func(tx *message.Transaction) {
	// 	if tx.Status == types.PENDING {
	// 		tx.Status = types.CONFIRMED
	// 	}
	// 	// wg.Done()
	// 	// }(tx)
	// }
	// wg.Wait()

	// pb.pt.DeletePendingTransactions(curBlock.SCPayload)
	// pb.pt.AddProcessedTransactions(curBlock.SCPayload)

	curCoordinationBlock.CQC = cqc

	// If Leader, Accept Message Broadcast To Validator
	if curCoordinationBlock.Proposer == pb.ID() {
		accept := &blockchain.CoordinationAccept{CommittedBlock: curCoordinationBlock, Timestamp: time.Now()}
		validators := pb.FindValidatorsFor(curCoordinationBlock.BlockHeader.Epoch)
		pb.BroadcastToSome(validators, accept)
	}

	// log.CDebugf("현재뷰[%v] 노드[%v] CQC 체크중, ID: %x", curBlock.CQC.View, pb.ID(), cqc.BlockID)
	// forked blocks are found when pruning

	err := pb.commitBlock(cqc)
	if err != nil {
		log.Errorf("[%v] (processCertificateCqc) failed commit Block: %v", pb.ID(), err)
		return
	}

	if curCoordinationBlock.Proposer == pb.ID() {
		pb.SendToBlockBuilder(curBlock)
	}

	// log.CDebugf("[%v] (processCertificateCqc) finished processing commit quorum certification blockHeight %v view %v epoch %v ID %x", pb.ID(), cqc.BlockHeight, cqc.View, cqc.Epoch, cqc.BlockID)
}

func (pb *CoordinationPBFT) commitBlock(cqc *quorum.QC) error {

	if cqc.BlockHeight < 2 {
		return nil
	}

	ok, block, _ := pb.commitRule(cqc)

	if !ok {
		return errors.New("cannot commit by rules")
	}
	// Commit Block which has previous block number
	committedBlocks, forkedBlocks, err := pb.bc.CommitCoordinationBlock(block.BlockHash, block.BlockHeader.BlockHeight, pb.voteQuorum, pb.commitQuorum)
	if err != nil {
		return fmt.Errorf("[%v] cannot commit blocks, %v", pb.ID(), err)
	}

	for _, cBlock := range committedBlocks {
		pb.committedBlocks <- cBlock.(*blockchain.CoordinationBlock)
	}
	for _, fBlock := range forkedBlocks {
		pb.forkedBlocks <- fBlock.(*blockchain.CoordinationBlock)
	}

	// Coordination Shard Leader의 Certificate Quorum 생성 및 Certificate Quorum Certificate 전파(commit phase 완료) -> Block Commit
	log.CDebugf("[%v] [Commit] [Complete] formed CQ and for block and committed coordination block [%v]", pb.ID(), cqc.BlockHeight)

	// log.CDebugf("[%v] (commitBlock) finished committing block blockHeight %v view %v epoch %v ID %x leader %v", pb.ID(), cqc.BlockHeight, cqc.View, cqc.Epoch, cqc.BlockID, cqc.Leader)
	return nil
}

func (pb *CoordinationPBFT) votingRule(block *blockchain.CoordinationBlock) (bool, error) {
	if block.BlockHeader.BlockHeight <= 2 {
		return true, nil
	}
	parentBlock, err := pb.bc.GetBlockByID(block.BlockHeader.PrevBlockHash)
	parentCoordinationBlock := parentBlock.(*blockchain.CoordinationBlock)
	if err != nil {
		return false, fmt.Errorf("cannot vote for block: %v", err)
	}
	if (block.BlockHeader.BlockHeight <= pb.lastVotedBlockHeight) || (parentCoordinationBlock.BlockHeader.BlockHeight < pb.preferredBlockHeight) {
		// log.Debugf("blockheight: %v, lastVotedBlockHeight: %v, parentBlockHeight: %v, preferredBlockHeight: %v", block.BlockHeader.BlockHeight, pb.lastVotedBlockHeight, parentCoordinationBlock.BlockHeader.BlockHeight, pb.preferredBlockHeight)
		return false, nil
	}
	return true, nil
}

func (pb *CoordinationPBFT) commitRule(cqc *quorum.QC) (bool, *blockchain.CoordinationBlock, error) {
	// var (
	// 	parentBlock *blockchain.Block
	// 	err         error
	// )

	// curBlockID := cqc.BlockID
	// curBlockHeight := cqc.BlockHeight

	// for i := 0; i < TIMELOCKFORUNLOCK; i++ {
	// 	parentBlock, err = pb.bc.GetParentBlock(curBlockID)
	// 	if err != nil {
	// 		return false, nil, fmt.Errorf("cannot commit any block: %v", err)
	// 	}

	// 	if parentBlock.BlockHeight+1 != curBlockHeight {
	// 		return false, nil, fmt.Errorf("cannot commit any block: not sequential block it has")
	// 	}

	// 	curBlockID = parentBlock.ID
	// 	curBlockHeight = parentBlock.BlockHeight
	// }

	parentBlock, err := pb.bc.GetParentCoordinationBlock(cqc.BlockID)
	parentCoordinationBlock := parentBlock.(*blockchain.CoordinationBlock)
	if err != nil {
		return false, nil, fmt.Errorf("cannot commit any block: %v", err)
	}
	if (parentCoordinationBlock.BlockHeader.BlockHeight + 1) == cqc.BlockHeight {
		return true, parentCoordinationBlock, nil
	}
	return false, nil, nil
}

/* Timer */
func (pb *CoordinationPBFT) ProcessRemoteTmo(tmo *pacemaker.CoordinationTMO) {
	// log.CDebugf("[%v] (ProcessRemoteTmo) processing remote timeout from %v for newview %v", pb.ID(), tmo.NodeID, tmo.NewView)

	// 현재 View Chainging이 아닐 경우 f+1개 체크
	if pb.State() != types.VIEWCHANGING {
		if !(pb.pm.GetCurView() < tmo.NewView) {
			return
		}

		for id, prevTmo := range pb.detectedTmos {
			// 현재 View 보다 작은 NewView 삭제
			if !(pb.pm.GetCurView() < prevTmo.NewView) {
				delete(pb.detectedTmos, id)
			}
		}

		pb.detectedTmos[tmo.NodeID] = tmo // 로컬에서 f+1 개 확인을 위해 기록

		// check f+1 tmos detected
		if len(pb.detectedTmos) > (config.GetConfig().CommitteeNumber-1)/3 {
			// log.CDebugf("[%v] (ProcessRemoteTmo) Got more than f+1 message Propose for new view %v", pb.ID(), tmo.NewView)

			var maxAnchorView types.View
			// add detectedTmos into TMO
			for _, tmo := range pb.detectedTmos {
				pb.pm.ProcessRemoteTmo(tmo)

				if maxAnchorView < tmo.AnchorView {
					maxAnchorView = tmo.AnchorView
				}
			}

			if pb.pm.GetAnchorView() < maxAnchorView {
				pb.pm.UpdateAnchorView(maxAnchorView)
			}

			// View Change 시작
			pb.pm.ExecuteViewChange(pb.pm.GetNewView())

			// detectedTmos 초기화
			pb.detectedTmos = make(map[types.NodeID]*pacemaker.CoordinationTMO)
		}
		return
	}

	// detectedTmos 초기화
	if len(pb.detectedTmos) > 0 {
		pb.detectedTmos = make(map[types.NodeID]*pacemaker.CoordinationTMO)
	}

	// 조건 D
	// 전달 받은 AnchorView가 나의 AnchorView 보다 높을 경우 Update
	// View-Change 다시 시작
	if tmo.AnchorView > pb.pm.GetAnchorView() {
		pb.pm.UpdateAnchorView(tmo.AnchorView)
		pb.pm.ExecuteViewChange(pb.pm.GetNewView())
		return
	}

	// Majority 체크: 같은 Newview에 대하여 2f+1개
	isBuilt, tc, reservedPrepareBlock := pb.pm.ProcessRemoteTmo(tmo) // tmo majority check
	if !isBuilt {
		return
	}

	tc.Epoch = pb.pm.GetCurEpoch()

	// 중요! Leader만 Majority check 해야 한다...
	if pb.IsLeader(pb.ID(), tc.NewView, tc.Epoch) {
		return
	}

	if reservedPrepareBlock != nil {
		pb.reservedBlock <- reservedPrepareBlock
	}

	// log.CDebugf("[%v] (ProcessRemoteTmo) a tc is built for New View %v", pb.ID(), tc.NewView)

	tc.NodeID = pb.ID()

	pb.SetRole(types.LEADER)

	// log.CDebugf("[%v] (ProcessRemoteTmo) finished processing remote timeout from %v for newview %v", pb.ID(), tmo.NodeID, tmo.NewView)

	pb.Broadcast(tc)
	pb.ProcessTC(tc)
}

func (pb *CoordinationPBFT) ProcessLocalTmo(view types.View) {
	// log.CDebugf("[%v] (ProcessLocalTmo) processing local timeout for view %v", pb.ID(), view)
	// BlockHeightLast = BlockHeight of which block is lastly appended into blockchain
	// BlockHeightPrepared = BlockHeight of which block is lastly prepared
	// BlockHeightLast is always same with BlockHeightPrepared because block is immediately inserted into blockchain after prepared...
	// If different, BlockHeightPrepared shouldn't be in the chain
	pb.SetState(types.VIEWCHANGING)

	tmo := &pacemaker.CoordinationTMO{
		Shard:               pb.Shard(),
		Epoch:               pb.pm.GetCurEpoch(),
		View:                view,
		NewView:             pb.pm.GetNewView(),
		AnchorView:          pb.pm.GetAnchorView(),
		NodeID:              pb.ID(),
		BlockHeightLast:     pb.GetHighBlockHeight(),
		BlockHeightPrepared: pb.GetHighBlockHeight(),
		BlockLast:           pb.bc.GetBlockByBlockHeight(pb.GetHighBlockHeight()).(*blockchain.CoordinationBlock),
		BlockPrepared:       nil,
		HighQC:              pb.GetHighQC(),
	}

	//
	if block, exist := pb.agreeingBlocks[tmo.Epoch][tmo.View][tmo.BlockHeightLast+1]; exist {
		tmo.BlockHeightPrepared += 1
		tmo.BlockPrepared = block
	}

	// log.CDebugf("[%v] (ProcessLocalTmo) tmo is built for view %v new view %v anchor view %v last blockheight %v prepared blockheight %v", pb.ID(), tmo.View, tmo.NewView, tmo.AnchorView, tmo.BlockHeightLast, tmo.BlockHeightPrepared)

	pb.BroadcastToSome(pb.FindCommitteesFor(tmo.Epoch), tmo)
	pb.ProcessRemoteTmo(tmo)

	// log.CDebugf("[%v] (ProcessLocalTmo) finished processing local timeout for view %v", pb.ID(), view)
}

func (pb *CoordinationPBFT) ProcessTC(tc *pacemaker.CoordinationTC) {
	// log.CDebugf("[%v] (ProcessTC) processing timeout certification for new view %v", pb.ID(), tc.NewView)

	// if pb.GetHighBlockHeight() == tc.BlockHeightNew-1 && pb.bc.GetBlockByBlockHeight(pb.GetHighBlockHeight()).ID == tc.BlockMax.ID {
	// 	pb.SetState(types.READY)
	// } else if pb.GetHighBlockHeight() == tc.BlockHeightNew {
	// 	pb.bc.UndoState()
	// 	pb.SetState(types.READY)
	// 	pb.bc.RevertBlock(tc.BlockHeightNew)
	// 	pb.updateHighQC(tc.BlockMax.QC)
	// }

	pb.SetState(types.READY)

	curEpoch, curView := pb.pm.GetCurEpochView()

	delete(pb.agreeingBlocks[curEpoch][curView], tc.BlockHeightNew)

	pb.pm.UpdateView(tc.NewView - 1)
	pb.pm.UpdateAnchorView(tc.AnchorView)

	// log.CDebugf("[%v] (ProcessTC) finished processing timeout certification for new view %v", pb.ID(), tc.NewView)

	curEpoch, curView = pb.pm.GetCurEpochView()

	if block, exist := pb.bufferedBlocks[curEpoch][curView][tc.BlockHeightNew]; exist { // 일반 합의에 경우
		_ = pb.ProcessCoordinationBlock(block)
		delete(pb.bufferedBlocks[curEpoch][curView], tc.BlockHeightNew)
	}

}

/* Util */
func (pb *CoordinationPBFT) GetHighQC() *quorum.QC {
	pb.mu.Lock()
	defer pb.mu.Unlock()
	return pb.highQC
}

func (pb *CoordinationPBFT) GetHighCQC() *quorum.QC {
	pb.mu.Lock()
	defer pb.mu.Unlock()
	return pb.highCQC
}

func (pb *CoordinationPBFT) GetChainStatus() string {
	chainGrowthRate := pb.bc.GetChainGrowth()
	blockIntervals := pb.bc.GetBlockIntervals()
	return fmt.Sprintf("[%v] The current view is: %v, chain growth rate is: %v, ave block interval is: %v", pb.ID(), pb.pm.GetCurView(), chainGrowthRate, blockIntervals)
}

// Vote가 완료된 가장 높은 BlockHeight
func (pb *CoordinationPBFT) GetHighBlockHeight() types.BlockHeight {
	pb.mu.Lock()
	defer pb.mu.Unlock()
	return pb.highQC.BlockHeight
}

// 마지막으로 Commit 이 완료된 BlockHeight
func (pb *CoordinationPBFT) GetLastBlockHeight() types.BlockHeight {
	pb.mu.Lock()
	defer pb.mu.Unlock()
	return pb.highCQC.BlockHeight
}

func (pb *CoordinationPBFT) GetLastVotedBlockHeight() types.BlockHeight {
	pb.mu.Lock()
	defer pb.mu.Unlock()
	return pb.lastVotedBlockHeight
}

func (pb *CoordinationPBFT) updateHighQC(qc *quorum.QC) {
	pb.mu.Lock()
	defer pb.mu.Unlock()
	if qc.BlockHeight > pb.highQC.BlockHeight {
		pb.highQC = qc
		if pb.IsLeader(pb.ID(), pb.pm.GetCurView(), pb.pm.GetCurEpoch()) {
			pb.updatedQC <- qc
		}
	}
}

func (pb *CoordinationPBFT) updateHighCQC(cqc *quorum.QC) {
	pb.mu.Lock()
	defer pb.mu.Unlock()
	if cqc.BlockHeight > pb.highCQC.BlockHeight {
		pb.highCQC = cqc
	}
}

// update block height we have just voted block lastly
func (pb *CoordinationPBFT) updateLastVotedBlockHeight(targetHeight types.BlockHeight) error {
	if targetHeight < pb.lastVotedBlockHeight {
		return errors.New("target blockHeight is lower than the last voted blockHeight")
	}
	pb.lastVotedBlockHeight = targetHeight
	return nil
}

// preferred block height is what we trully preferred block height
// which means preferred height confirmed by 1 block ahead
func (pb *CoordinationPBFT) updatePreferredBlockHeight(qc *quorum.QC) error {
	if qc.BlockHeight <= 1 {
		return nil
	}
	_, err := pb.bc.GetBlockByID(qc.BlockID)
	if err != nil {
		return fmt.Errorf("cannot update preferred blockHeight: %v", err)
	}
	parentBlock, err := pb.bc.GetParentCoordinationBlock(qc.BlockID)
	parentCoordinationBlock := parentBlock.(*blockchain.CoordinationBlock)
	if err != nil {
		return fmt.Errorf("cannot update preferred blockHeight: %v", err)
	}
	if parentCoordinationBlock.BlockHeader.BlockHeight > pb.preferredBlockHeight {
		pb.preferredBlockHeight = parentCoordinationBlock.BlockHeader.BlockHeight
	}
	return nil
}

// func (pb *PBFT) updateStateOfBlock(blockHeight types.BlockHeight, state types.BlockStatus) {
// 	pb.mu.Lock()
// 	pb.stateOf[blockHeight] = state
// 	pb.mu.Unlock()
// }

// func (pb *PBFT) CQCupdatePreferredBlockHeight(cqc *quorum.QC) error {
// 	if cqc.View <= 1 {
// 		return nil
// 	}
// 	_, err := pb.bc.GetBlockByID(cqc.BlockID)
// 	if err != nil {
// 		return fmt.Errorf("cannot update preferred view: %v", err)
// 	}
// 	grandParentBlock, err := pb.bc.GetParentBlock(cqc.BlockID)
// 	if err != nil {
// 		return fmt.Errorf("cannot update preferred view: %v", err)
// 	}
// 	if grandParentBlock.View > pb.preferredView {
// 		pb.preferredView = grandParentBlock.View
// 	}
// 	return nil
// }

func (pb *CoordinationPBFT) ProcessReportByzantine(reportByzantine *byzantine.ReportByzantine) {
}
func (pb *CoordinationPBFT) ProcessReplaceByzantine(replaceByzantine *byzantine.ReplaceByzantine) {
}
