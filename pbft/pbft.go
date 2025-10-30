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
	"unishard/evm"
	"unishard/evm/state"
	"unishard/evm/vm/runtime"
	"unishard/log"
	"unishard/measure"
	"unishard/mempool"
	"unishard/message"
	"unishard/node"
	"unishard/pacemaker"
	"unishard/quorum"
	"unishard/types"
	"unishard/utils"

	"github.com/ethereum/go-ethereum/common"
	"github.com/holiman/uint256"
)

const FORK = "fork"

var mu sync.Mutex

type PBFT struct {
	node.Node
	*election.Static
	pm                   *pacemaker.Pacemaker
	pd                   *mempool.Producer
	startBlockHeight     types.BlockHeight
	lastVotedBlockHeight types.BlockHeight
	preferredBlockHeight types.BlockHeight
	highQC               *quorum.QC
	highCQC              *quorum.QC
	bc                   *blockchain.BlockChain
	voteQuorum           *quorum.Quorum[quorum.Vote]
	commitQuorum         *quorum.Quorum[quorum.Commit]
	committedBlocks      chan *blockchain.WorkerBlock
	forkedBlocks         chan *blockchain.WorkerBlock
	reservedBlock        chan *blockchain.WorkerBlock
	updatedQC            chan *quorum.QC
	lastCreatedBlockQC   *quorum.QC
	bufferedQCs          map[common.Hash]*quorum.QC
	bufferedCQCs         map[common.Hash]*quorum.QC
	bufferedBlocks       map[types.Epoch]map[types.View]map[types.BlockHeight]*blockchain.ProposedWorkerBlock
	bufferedAccepts      map[types.Epoch]map[types.View]map[types.BlockHeight]*blockchain.Accept
	agreeingBlocks       map[types.Epoch]map[types.View]map[types.BlockHeight]*blockchain.ProposedWorkerBlock
	executedState        map[types.BlockHeight]*state.StateDB
	mu                   sync.Mutex
	gSnapshot            []*blockchain.LocalSnapshot
	gCode                []*blockchain.LocalContract
	fdcm                 []*common.Address
	pbftMeasure          *measure.PBFTMeasure
	// stateOf map[types.BlockHeight]types.BlockStatus
	detectedTmos map[types.NodeID]*pacemaker.TMO
}

// Todo: State Update and make FDCM
func NewPBFT(
	node node.Node,
	pm *pacemaker.Pacemaker,
	pd *mempool.Producer,
	elec *election.Static,
	committedBlocks chan *blockchain.WorkerBlock,
	forkedBlocks chan *blockchain.WorkerBlock,
	reservedBlock chan *blockchain.WorkerBlock) *PBFT {
	pb := new(PBFT)
	pb.Node = node
	pb.Static = elec
	pb.pm = pm
	pb.pd = pd
	pb.bc = blockchain.NewWorkerBlockchain(pb.Shard(), config.GetConfig().DefaultBalance, pb.ID())
	pb.voteQuorum = quorum.NewQuorum[quorum.Vote](config.GetConfig().CommitteeNumber)
	pb.commitQuorum = quorum.NewQuorum[quorum.Commit](config.GetConfig().CommitteeNumber)
	pb.bufferedBlocks = make(map[types.Epoch]map[types.View]map[types.BlockHeight]*blockchain.ProposedWorkerBlock)
	pb.bufferedAccepts = make(map[types.Epoch]map[types.View]map[types.BlockHeight]*blockchain.Accept)
	pb.agreeingBlocks = make(map[types.Epoch]map[types.View]map[types.BlockHeight]*blockchain.ProposedWorkerBlock)
	pb.executedState = make(map[types.BlockHeight]*state.StateDB)
	pb.bufferedQCs = make(map[common.Hash]*quorum.QC)
	pb.highQC = &quorum.QC{View: 0}
	pb.bufferedCQCs = make(map[common.Hash]*quorum.QC)
	pb.highCQC = &quorum.QC{View: 0}
	pb.committedBlocks = committedBlocks
	pb.forkedBlocks = forkedBlocks
	pb.reservedBlock = reservedBlock
	pb.lastCreatedBlockQC = &quorum.QC{View: 0}
	pb.updatedQC = make(chan *quorum.QC, 1000)
	// pb.stateOf = make(map[types.BlockHeight]types.BlockStatus)
	pb.gSnapshot = make([]*blockchain.LocalSnapshot, 0)
	pb.gCode = make([]*blockchain.LocalContract, 0)
	pb.fdcm = nil
	pb.detectedTmos = make(map[types.NodeID]*pacemaker.TMO)
	pb.pbftMeasure = measure.NewPBFTMeasure()

	return pb
}

func (pb *PBFT) GetBlockChain() *blockchain.BlockChain {
	return pb.bc
}

func (pb *PBFT) CreateWorkerBlock(localExecutionSequence []*message.Transaction, keys [][]byte, values [][]byte, merkleProofKeys [][]byte, merkleProofValues [][]byte) *blockchain.WorkerBlock {
	if pb.pbftMeasure.StartTime.IsZero() && len(localExecutionSequence) > 0 {
		pb.pbftMeasure.StartTime = time.Now()
	}
	blockdata := blockchain.CreateWorkerBlockData(localExecutionSequence, keys, values, merkleProofKeys, merkleProofValues)
	workerblockwithoutheader := new(blockchain.WorkerBlockWithoutHeader)
	workerblockwithoutheader.WorkerBlockData = blockdata

	// wait qc from updatedQC channel
	log.Debugf("CreateWorkerBlock %v %v ", pb.highQC.BlockHeight, pb.lastCreatedBlockQC.BlockHeight)
	var qc *quorum.QC
	if pb.highQC.View == 0 {
		qc = pb.highQC
	} else if pb.highQC.BlockHeight == pb.lastCreatedBlockQC.BlockHeight {
		qc = <-pb.updatedQC
	} else {
		qc = pb.highQC
		<-pb.updatedQC
	}
	log.Debugf("CreateWorkerBlock DONE %v %v ", qc.BlockHeight, pb.lastCreatedBlockQC.BlockHeight)

	block := blockchain.CreateWorkerBlock(workerblockwithoutheader, pb.pm.GetCurEpoch(), pb.pm.GetCurView(), pb.bc.GetCurrentStateHash(), qc.BlockID, pb.GetHighBlockHeight(), qc, pb.Shard())
	workerBlock := block.(*blockchain.WorkerBlock)
	pb.lastCreatedBlockQC = workerBlock.QC

	return workerBlock
}

/* Consensus Process */
func (pb *PBFT) ProcessWorkerBlock(blockWithGlobalVariable *blockchain.ProposedWorkerBlock) error {

	// curEpoch := pb.pm.GetCurEpoch()
	curBlockHeight := pb.GetHighBlockHeight()
	block := blockWithGlobalVariable.WorkerBlock
	// localSequence := block.BlockData.LocalExecutionSequence
	pb.gCode = blockWithGlobalVariable.GlobalContractBundle
	pb.gSnapshot = blockWithGlobalVariable.GlobalSnapshot

	// Worker Block Builder의 Coordination Block 수신
	if pb.ID() == pb.FindLeaderFor(block.BlockHeader.View, block.BlockHeader.Epoch) {
		log.Debugf("[%v] [Pre-Prepare] [In progress] received coordination block [%v]", pb.Shard(), block.BlockHeader.BlockHeight)
	}

	if block.BlockHeader.View > pb.pm.GetCurView() {
		utils.AddEntity(pb.bufferedBlocks, block.BlockHeader.Epoch, block.BlockHeader.View, block.BlockHeader.BlockHeight-1, blockWithGlobalVariable)
		return nil
	}

	// 블록 검증 진행
	err := pb.processCertificateBlock(block)
	if err != nil {
		log.Errorf("[%v] (ProcessWorkerBlock) failed certificate block: %v, localSequence: %v", pb.Shard(), err, len(block.WorkerBlockData().LocalExecutionSequence))
		return err
	}

	// 앞선 Block이 오는 경우, Buffer에 보관하여 나중에 처리함
	// Block 순서를 맞추기 위함
	if block.BlockHeader.BlockHeight > curBlockHeight+1 { // 일반 합의의 경우, Vote이전에 받음
		// buffer the block for which it can be processed after processing the block of block height - 1
		utils.AddEntity(pb.bufferedBlocks, block.BlockHeader.Epoch, block.BlockHeader.View, block.BlockHeader.BlockHeight-1, blockWithGlobalVariable)
		// pb.bufferedBlocks[types.Epoch(block.BlockHeader.BlockHeight)-1] = block
		// log.Debugf("[%v] (ProcessWorkerBlock) the block is buffered for large blockheight %v ID %x, localSequence: %v", pb.Shard(), block.BlockHeader.BlockHeight, block.BlockHash, len(block.BlockData.LocalExecutionSequence))
		return nil
	} else if pb.State() == types.VIEWCHANGING { // View Change의 경우
		utils.AddEntity(pb.bufferedBlocks, block.BlockHeader.Epoch, block.BlockHeader.View, block.BlockHeader.BlockHeight, blockWithGlobalVariable)
		// pb.bufferedBlocks[types.Epoch(block.BlockHeader.BlockHeight)] = block
		// log.Debugf("[%v] (ProcessWorkerBlock) the block is buffered for viewchange ID %x, localSequence: %v", pb.Shard(), block.BlockHash, len(block.BlockData.LocalExecutionSequence))
		return nil
	}

	if block.Proposer != pb.ID() {
		// Validate state root
		if block.BlockHeader.StateRoot != pb.bc.GetStateRoot() {
			log.Warningf("[%v] (ProcessWorkerBlock) stateHash not valid blockID %v current statehash %v, received statehash %v", pb.Shard(), block.BlockHeader.BlockHeight, pb.bc.GetStateRoot(), block.BlockHeader.StateRoot)
			return nil
		} else {
			log.Debugf("[%v] (ProcessWorkerBlock) stateHash valid blockID %v current statehash %v", pb.Shard(), block.BlockHeader.BlockHeight, pb.bc.GetStateRoot())
		}

		// Validate merkle proof
		if len(block.BlockData.LocalExecutionSequence) > 0 {
			valid := pb.GetBlockChain().SetPartialState(block.BlockData.Keys, block.BlockData.Values, block.BlockData.MerkleProofKeys, block.BlockData.MerkleProofValues, block.BlockData.LocalExecutionSequence)
			if valid {
				pb.executeTransaction(block.BlockData.LocalExecutionSequence, false)
			} else {
				log.Warningf("[%v] (ProcessWorkerBlock) merkle proof invalid blockID %v", pb.Shard(), block.BlockHeader.BlockHeight)
				return nil
			}
		}
		// Execute
		// pb.executeTransaction(block.BlockData.LocalExecutionSequence, false)

		// Validate state
		// if block.BlockHeader.StateRoot != pb.bc.GetCurrentStateHash() {
		// 	// pb.bc.RevertExpectedState()
		// 	log.Warningf("[%v] (ProcessWorkerBlock) stateHash not valid blockID %v current statehash %v, received statehash %v", pb.Shard(), block.BlockHeader.BlockHeight, pb.bc.GetCurrentStateHash(), block.BlockHeader.StateRoot)
		// 	return nil
		// }
	} else {
		pb.executeTransaction(block.BlockData.LocalExecutionSequence, false)
	}

	// pb.pm.AdvanceView(block.BlockHeader.View)
	// after verify block, set prepared for node to start consensus
	// pb.SetState(types.PREPARED)

	// // 블록 삽입
	// 합의중 블록 대기열에 삽입
	utils.AddEntity(pb.agreeingBlocks, block.BlockHeader.Epoch, block.BlockHeader.View, block.BlockHeader.BlockHeight, blockWithGlobalVariable)

	// pb.agreeingBlocks[block.Epoch][block.View][block.BlockHeight] = block

	// pb.pt.AddPendingTransactions(block.SCPayload)
	// process buffered QC
	// 현재 Block에 대해, qc가 이미 발급된 경우, 바로 certificate 실행
	qc, ok := pb.bufferedQCs[block.BlockHash]
	if ok {
		pb.processCertificateVote(qc)
		delete(pb.bufferedQCs, block.BlockHash)
		vote := quorum.MakeCollection[quorum.Vote](block.BlockHeader.Epoch, block.BlockHeader.View, block.BlockHeader.BlockHeight, pb.ID(), block.BlockHash)
		pb.BroadcastToSome(pb.FindCommitteesFor(block.BlockHeader.Epoch), vote)
		return nil
	}

	shouldVote, err := pb.votingRule(block)
	if err != nil {
		// pb.bc.RevertExpectedState()
		log.Errorf("[%v] (ProcessWorkerBlock) cannot decide whether to vote the block: %v, localSequence: %v", pb.Shard(), err, len(block.BlockData.LocalExecutionSequence))
		return err
	}
	if !shouldVote {
		// pb.bc.RevertExpectedState()
		log.Warningf("[%v] (ProcessWorkerBlock) is not going to vote for block ID %x, localSequence: %v", pb.Shard(), block.BlockHash, len(block.BlockData.LocalExecutionSequence))
		return nil
	}

	// log.Debugf("[%v] (ProcessWorkerBlock) finished processing block from leader %v blockHeight %v view %v epoch %v ID %x, localSequence: %v", pb.Shard(), block.BlockHeader.BlockHeight, block.Proposer, block.BlockHeader.View, block.BlockHeader.Epoch, block.BlockHash, len(block.BlockData.LocalExecutionSequence))

	// Worker Shard Leader의 Worker Block 제안(pre-prepare phase 완료)
	// log.Debugf("[%v] (CreateWorkerBlock) finished making a proposal for blockheight %v view %v epoch %v ID %x, state root: %v, localSequence: %v", pb.Shard(), workerBlock.BlockHeader.BlockHeight, workerBlock.BlockHeader.View, workerBlock.BlockHeader.Epoch, workerBlock.BlockHash, workerBlock.BlockHeader.StateRoot, len(workerBlock.BlockData.LocalExecutionSequence))
	if pb.ID() == pb.FindLeaderFor(block.BlockHeader.View, block.BlockHeader.Epoch) {
		log.Debugf("[%v] [Pre-Prepare] [Complete] proposing worker block [%v]", pb.Shard(), block.BlockHeader.BlockHeight)
	}

	vote := quorum.MakeCollection[quorum.Vote](block.BlockHeader.Epoch, block.BlockHeader.View, block.BlockHeader.BlockHeight, pb.ID(), block.BlockHash)

	// vote is sent to the next leader
	pb.BroadcastToSome(pb.FindCommitteesFor(block.BlockHeader.Epoch), vote)
	pb.ProcessVote(vote)

	return nil
}

func (pb *PBFT) ProcessVote(vote *quorum.Collection[quorum.Vote]) {
	// 현재 view의 STATE를 체크
	// Commit이면, 본 함수 진행하지 않음
	// 현재 view보다 낮을 경우, 다음 view 진행

	voteIsVerified, err := crypto.PubVerify(vote.Signature, crypto.IDToByte(vote.BlockID))
	if err != nil {
		log.Errorf("[%v] (ProcessVote) error in verifying the signature in vote ID %x", pb.ID(), vote.BlockID)
		return
	}
	if !voteIsVerified {
		log.Warningf("[%v] (ProcessVote) received a vote with invalid signature ID %x", pb.ID(), vote.BlockID)
		return
	}

	// Worker Shard Leader의 Vote 수신(prepare phase 진행 중)
	// log.Debugf("[%v] (ProcessVote) processing vote for blockHeight %v view %v epoch %v ID %x from %v", pb.ID(), vote.BlockHeight, vote.View, vote.Epoch, vote.BlockID, vote.Publisher)
	if pb.ID() == pb.FindLeaderFor(vote.View, vote.Epoch) {
		log.Debugf("[%v] [Prepare] [In progress] received prepare vote for block [%v]", pb.Shard(), vote.BlockHeight)
	}

	isBuilt, qc := pb.voteQuorum.Add(vote)
	if !isBuilt {
		// log.Debugf("[%v] (ProcessVote) votes are not sufficient to build a qc ID %x", pb.ID(), vote.BlockID)
		return
	}
	// 진행하던 view의 STATE 업데이트: pprepare -> prepare
	// pb.updateStateOfBlock(vote.BlockHeight, types.PREPARED)

	qc.Leader = pb.ID()

	// buffer the QC if the block has not been received
	_, exist := pb.agreeingBlocks[qc.Epoch][qc.View][qc.BlockHeight]
	if !exist {
		pb.bufferedQCs[qc.BlockID] = qc
		// log.Debugf("[%v] (ProcessVote) the qc is buffered for not arrived block ID %x", pb.ID(), qc.BlockID)
		return
	}

	// Worker Shard Leader의 Quorum 생성 및 Quorum Certificate 전파(prepare phase 완료)
	// log.Debugf("[%v] (ProcessVote) finished processing vote for blockHeight %v view %v epoch %v ID %x", pb.ID(), vote.BlockHeight, vote.View, vote.Epoch, vote.BlockID)
	if pb.ID() == pb.FindLeaderFor(vote.View, vote.Epoch) {
		log.Debugf("[%v] [Prepare] [Complete] formed quorum and broadcasted QC for block [%v]", pb.Shard(), vote.BlockHeight)
	}

	pb.processCertificateVote(qc)

}

func (pb *PBFT) ProcessCommit(commit *quorum.Collection[quorum.Commit]) {

	commitIsVerified, err := crypto.PubVerify(commit.Signature, crypto.IDToByte(commit.BlockID))
	if err != nil {
		log.Errorf("[%v] (ProcessCommit) error in verifying the signature in commit ID %x", pb.ID(), commit.BlockID)
		return
	}
	if !commitIsVerified {
		log.Warningf("[%v] (ProcessCommit) received a commit with invalid signature ID %x", pb.ID(), commit.BlockID)
		return
	}

	// Worker Shard Leader의 Vote 수신(commit phase 진행 중)
	// log.Debugf("[%v] (ProcessCommit) processing commit for blockHeight %v view %v epoch %v ID %x", pb.ID(), commit.BlockHeight, commit.View, commit.Epoch, commit.BlockID)
	if pb.ID() == pb.FindLeaderFor(commit.View, commit.Epoch) {
		log.Debugf("[%v] [Commit] [In progress] received commit vote for block [%v]", pb.Shard(), commit.BlockHeight)
	}

	isBuilt, cqc := pb.commitQuorum.Add(commit)
	if !isBuilt {
		// log.Debugf("[%v] (ProcessCommit) commits are not sufficient to build a cqc ID %x", pb.ID(), commit.BlockID)

		return
	}

	// 진행하던 view의 STATE 업데이트: prepare -> commit
	// pb.updateStateOfBlock(commit.BlockHeight, types.COMMITTED)

	cqc.Leader = pb.ID()
	// buffer the QC if the block has not been received
	// which means qc is not certificated
	_, err = pb.bc.GetBlockByID(cqc.BlockID)
	if err != nil {
		pb.bufferedCQCs[cqc.BlockID] = cqc
		return
	}

	pb.processCertificateCqc(cqc)

}

// Validator가 Commit된 Block을 지속적으로 Sync하기 위해서 필요한 함수
// Committee가 Commit을 완료한 Block에 대하여 Leader가 Validator에게 전송함
func (pb *PBFT) ProcessAccept(accept *blockchain.Accept) {
	if pb.IsCommittee(pb.ID(), accept.CommittedBlock.BlockHeader.Epoch) {
		return
	}
	pb.gCode = accept.GlobalContractBundle
	pb.gSnapshot = accept.GlobalSnapshot
	// block 검증
	block := accept.CommittedBlock
	// log.Debugf("[%v] (ProcessAccept) processing accept commit completed block from leader %v blockHeight %v view %v epoch %v ID %x", pb.Shard(), utils.Node(block.Proposer), block.BlockHeader.BlockHeight, block.BlockHeader.View, block.BlockHeader.Epoch, block.BlockHash)

	curEpoch := pb.pm.GetCurEpoch()
	curBlockHeight := pb.GetHighBlockHeight()

	if block.BlockHeader.Epoch > curEpoch {
		utils.AddEntity(pb.bufferedAccepts, block.BlockHeader.Epoch, block.BlockHeader.View, block.BlockHeader.BlockHeight-1, accept)
		// log.Debugf("[%v] (ProcessAccept) the accept is buffered for large epoch ID %x ", pb.ID(), block.BlockHash)
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
		// log.Debugf("[%v] (ProcessAccept) the accept is buffered for large blockheight ID %x", pb.ID(), block.BlockHash)
		return
	}

	// Execute Transaction
	// longestPathTransactions := block.BlockData.LocalExecutionSequence
	// longestPathTransactions := pb.FindLongestPathTransactions(block.BlockData.LocalExecutionSequence)
	// pb.executeTransaction(longestPathTransactions, false)

	// Validate state
	if block.BlockHeader.StateRoot != pb.bc.GetCurrentStateHash() {
		// pb.bc.RevertExpectedState()
		log.Warningf("[%v] (ProcessWorkerBlock) stateHash not valid blockID %v current statehash %v, received statehash %v", pb.Shard(), block.BlockHeader.BlockHeight, pb.bc.GetCurrentStateHash(), block.BlockHeader.StateRoot)
		return
	}

	// Block 삽입
	pb.bc.AddWorkerBlock(block)

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
			// log.Debugf("[%v] (ProcessAccept) elect new committees %v for new epoch %v", pb.ID(), committes, curEpoch+1)
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
		log.Errorf("[%v] (ProcessAccept) Failed commit BlockHeight %v: %v", pb.ID(), block.CQC.BlockHeight, err)
		return
	}

	// log.Debugf("[%v] (ProcessAccept) finished processing accept commit completed block from leader %v blockHeight %v view %v epoch %v ID %x", pb.ID(), utils.Node(block.Proposer), block.BlockHeader.BlockHeight, block.BlockHeader.View, block.BlockHeader.Epoch, block.BlockHash)
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
		_ = pb.ProcessWorkerBlock(block)
		delete(pb.bufferedBlocks[curEpoch][curView], block.WorkerBlock.BlockHeader.BlockHeight)
	}
}

/* Certificate */
func (pb *PBFT) processCertificateBlock(block *blockchain.WorkerBlock) error {

	// log.Debugf("[%v] (processCertificateBlock) processing certificate block blockHeight: %v view: %v epoch: %v ID: %x", pb.ID(), block.BlockHeader.BlockHeight, block.BlockHeader.View, block.BlockHeader.Epoch, block.BlockHash)
	qc := block.QC
	if qc.BlockHeight < pb.GetHighBlockHeight() {
		return fmt.Errorf("qc is not valid, qc blockheight %v, block blockheight %v, HighBlockHeight %v", qc.BlockHeight, block.BlockHeader.BlockHeight, pb.GetHighBlockHeight())
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
		return fmt.Errorf("received a stale proposal from %v view %v curview %v blockheight %v curBlockheight %v", block.Proposer, block.BlockHeader.View, curView, block.BlockHeader.BlockHeight, curBlockHeight)
	}

	// 블록 서명 검증
	if block.Proposer != pb.ID() {
		blockIsVerified, _ := crypto.PubVerify(block.CommitteeSignature[0], crypto.IDToByte(block.BlockHash))
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

	// log.Debugf("[%v] (processCertificateBlock) finished processing certificate block blockHeight: %v view: %v epoch: %v ID: %x", pb.ID(), block.BlockHeader.BlockHeight, block.BlockHeader.View, block.BlockHeader.Epoch, block.BlockHash)
	return nil
}

func (pb *PBFT) processCertificateVote(qc *quorum.QC) {
	// log.Debugf("[%v] (processCertificateVote) processing quorum certification blockHeight %v view %v epoch %v ID %x", pb.ID(), qc.BlockHeight, qc.View, qc.Epoch, qc.BlockID)
	if qc.BlockHeight < pb.GetHighBlockHeight() {
		return
	}

	blockWithGlobalVariable, exist := pb.agreeingBlocks[qc.Epoch][qc.View][qc.BlockHeight]
	if !exist {
		return
	}
	block := blockWithGlobalVariable.WorkerBlock
	// 블록체인에 블록 삽입
	pb.bc.AddWorkerBlock(block)
	delete(pb.agreeingBlocks[qc.Epoch][qc.View], qc.BlockHeight)

	if pb.IsByz() && config.GetConfig().Strategy == FORK && pb.IsLeader(pb.ID(), qc.View+1, pb.pm.GetCurEpoch()) {
		pb.pm.AdvanceView(qc.View)
		return
	}

	err := pb.updatePreferredBlockHeight(qc)
	if err != nil {
		pb.bufferedQCs[qc.BlockID] = qc
		// log.Debugf("[%v] (processCertificateVote) a qc is buffered for not ready to certificate ID %x: %v", pb.ID(), qc.BlockID, err)
		return
	}

	err = pb.updateLastVotedBlockHeight(qc.BlockHeight)
	if err != nil {
		pb.bufferedQCs[qc.BlockID] = qc
		// log.Debugf("[%v] (processCertificateVote) a qc is buffered for not ready to certificate ID %x: %v", pb.ID(), qc.BlockID, err)
		return
	}

	pb.pm.AdvanceView(qc.View)
	pb.updateHighQC(qc)

	if pb.pm.IsTimeToElect() {
		// newEpoch := pb.pm.UpdateEpoch()
		curEpoch := pb.pm.GetCurEpoch()
		committes := pb.ElectCommittees(block.BlockHash, curEpoch+1)
		if committes != nil {
			// log.Debugf("[%v] (processCertificateVote) elect new committees %v for new epoch %v", pb.ID(), committes, curEpoch+1)
		}
	}

	// after complete voting, set ready for node to start newview
	// pb.SetState(types.READY)
	// log.Debugf("[%v] (processCertificateVote) finished processing quorum certification blockHeight %v view %v epoch %v ID %x", pb.ID(), qc.BlockHeight, qc.View, qc.Epoch, qc.BlockID)

	curEpoch, curView := pb.pm.GetCurEpochView()
	// // print all data inside pb.bufferedBlocks
	// for epoch, views := range pb.bufferedBlocks {
	// 	for view, blocks := range views {
	// 		for height, block := range blocks {
	// 			// log.Debugf("[%v] (processCertificateVote) buffered block epoch %v view %v height %v ID %x", pb.ID(), epoch, view, height, block.WorkerBlock.BlockHash)
	// 		}
	// 	}
	// }

	// Vote 완료 후, 미리 받은 "다음" LockReq 혹은 Block 합의 실행
	if block, ok := pb.bufferedBlocks[curEpoch][curView][qc.BlockHeight]; ok { // 일반합의
		// log.Debugf("[%v] (processCertificateVote) start consensus for buffered block where blockHeight %v view %v epoch %v ID %x", pb.ID(), qc.BlockHeight, qc.View, qc.Epoch, qc.BlockID)
		_ = pb.ProcessWorkerBlock(block)
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

func (pb *PBFT) processCertificateCqc(cqc *quorum.QC) {
	// log.Debugf("[%v] (processCertificateCqc) processing commit quorum certification blockHeight %v view %v epoch %v ID %x", pb.ID(), cqc.BlockHeight, cqc.View, cqc.Epoch, cqc.BlockID)

	if cqc.BlockHeight < pb.GetLastBlockHeight() {
		return
	}

	pb.updateHighCQC(cqc)

	curBlock, _ := pb.bc.GetBlockByID(cqc.BlockID)
	curWorkerBlockBlock := curBlock.(*blockchain.WorkerBlock)

	curWorkerBlockBlock.CQC = cqc

	// If Leader, Accept Message Broadcast To Validator
	if curWorkerBlockBlock.Proposer == pb.ID() {
		accept := &blockchain.Accept{CommittedBlock: curWorkerBlockBlock, GlobalSnapshot: pb.gSnapshot, GlobalContractBundle: pb.gCode, Timestamp: time.Now()}
		validators := pb.FindValidatorsFor(curWorkerBlockBlock.BlockHeader.Epoch)
		pb.BroadcastToSome(validators, accept)
	}

	// log.Debugf("현재뷰[%v] 노드[%v] CQC 체크중, ID: %x", curBlock.CQC.View, pb.ID(), cqc.BlockID)
	// forked blocks are found when pruning

	stateRoot, err := pb.GetBlockChain().GetStateDB().Commit(uint64(cqc.BlockHeight)+1, false)
	if err != nil {
		log.Errorf("[%v] (executeTransaction) failed to commit state: %v", pb.ID(), err)
	}
	pb.GetBlockChain().SetStateRoot(stateRoot)

	err = pb.commitBlock(cqc)
	if err != nil {
		log.Errorf("[%v] (processCertificateCqc) failed commit BlockHeight %v: %v", pb.ID(), cqc.BlockHeight, err)
		return
	}

	if pb.startBlockHeight == 0 && len(curWorkerBlockBlock.BlockData.LocalExecutionSequence) > 0 {
		pb.startBlockHeight = curWorkerBlockBlock.BlockHeader.BlockHeight
	}

	// calculate TPS and local latency
	if curWorkerBlockBlock.Proposer == pb.ID() {
		for _, tx := range curWorkerBlockBlock.BlockData.LocalExecutionSequence {
			if tx.IsCrossShardTx {
				tx.LatencyDissection.WorkerConsensusTime2 = time.Now().UnixMilli()
				pb.DistLogger().TransactionPath(tx, "wbb_execution_consensus")
			} else {
				tx.LatencyDissection.WorkerConsensusTime = time.Now().UnixMilli()
				pb.DistLogger().TransactionPath(tx, "wbb_consensus")
			}
		}
		go pb.pbftMeasure.CalculateMeasurements(curWorkerBlockBlock, pb.Shard())
	}

	// send worker block to gateway
	if curWorkerBlockBlock.Proposer == pb.ID() {
		pb.SendToBlockBuilder(curWorkerBlockBlock)
	}

	// log.Debugf("[%v] (processCertificateCqc) finished processing commit quorum certification blockHeight %v view %v epoch %v ID %x", pb.ID(), cqc.BlockHeight, cqc.View, cqc.Epoch, cqc.BlockID)
}

func (pb *PBFT) commitBlock(cqc *quorum.QC) error {

	if cqc.BlockHeight < 2 {
		return nil
	}

	ok, block, _ := pb.commitRule(cqc)

	if !ok {
		return errors.New("cannot commit by rules")
	}

	// Commit block which has previous block number
	// executedState := pb.executedState[block.BlockHeader.BlockHeight]
	committedBlocks, forkedBlocks, err := pb.bc.CommitWorkerBlock(block.BlockHash, block.BlockHeader.BlockHeight, pb.voteQuorum, pb.commitQuorum)
	// delete(pb.executedState, block.BlockHeader.BlockHeight-1)

	if err != nil {
		return fmt.Errorf("[%v] cannot commit blocks, %v", pb.ID(), err)
	}

	for _, cBlock := range committedBlocks {
		pb.committedBlocks <- cBlock.(*blockchain.WorkerBlock)
	}

	for _, fBlock := range forkedBlocks {
		pb.forkedBlocks <- fBlock.(*blockchain.WorkerBlock)
	}

	// log.Debugf("[%v] (commitBlock) finished committing block blockHeight %v view %v epoch %v ID %x leader %v", pb.ID(), cqc.BlockHeight, cqc.View, cqc.Epoch, cqc.BlockID, cqc.Leader)

	// Worker Shard Leader의 Certificate Quorum 생성 및 Certificate Quorum Certificate 전파(commit phase 완료) -> Block Commit
	if pb.ID() == pb.FindLeaderFor(block.BlockHeader.View, block.BlockHeader.Epoch) {
		log.Debugf("[%v] [Commit] [Complete] formed CQ and for block and committed coordination block [%v]", pb.Shard(), cqc.BlockHeight)
	}

	return nil
}

func (pb *PBFT) votingRule(block *blockchain.WorkerBlock) (bool, error) {
	if block.BlockHeader.BlockHeight <= 2 {
		return true, nil
	}
	parentBlock, err := pb.bc.GetBlockByID(block.BlockHeader.PrevBlockHash)
	parentWorkerBlock := parentBlock.(*blockchain.WorkerBlock)
	if err != nil {
		return false, fmt.Errorf("cannot vote for block: %v", err)
	}
	if (block.BlockHeader.BlockHeight <= pb.lastVotedBlockHeight) || (parentWorkerBlock.BlockHeader.BlockHeight < pb.preferredBlockHeight) {
		return false, nil
	}
	return true, nil
}

func (pb *PBFT) commitRule(cqc *quorum.QC) (bool, *blockchain.WorkerBlock, error) {
	parentBlock, err := pb.bc.GetParentWorkerBlock(cqc.BlockID)
	parentWorkerBlock := parentBlock.(*blockchain.WorkerBlock)
	if err != nil {
		return false, nil, fmt.Errorf("cannot commit any block: %v", err)
	}
	if (parentWorkerBlock.BlockHeader.BlockHeight + 1) == cqc.BlockHeight {
		return true, parentWorkerBlock, nil
	}
	return false, nil, nil
}

func (pb *PBFT) executeTransaction(localSequence []*message.Transaction, flag bool) {
	mu.Lock()
	defer mu.Unlock()
	cfg := evm.SetConfig(pb.GetHighBlockHeight(), string(rune(time.Now().Unix())))
	evmMachine := runtime.NewEnv(cfg, pb.bc.GetStateDB())
	for _, tx := range localSequence {
		if tx.IsCrossShardTx {
			// log.StateInfof("[%v] (executeTransaction) [Consensus Node %v, %v] Execute Cross Transaction: %v", pb.ID(), pb.Shard(), pb.ID(), tx.Hash)
			if tx.TXType == types.TRANSFER {
				err := evm.Transfer(pb.bc.GetStateDB(), tx.From, tx.To, uint256.NewInt(uint64(tx.Value)))
				if err != nil {
					tx.TXType = types.ABORT
					log.Errorf("[%v] [%v] (executeTransaction) cross tranfer transaction execution fail %x: %v", pb.Shard(), pb.ID(), tx.Hash, err)
					continue
				}
				// log.StateInfof("[%v] [%v] (executeTransaction) Execute Cross Transfer Transaction %v, Sender: %v %v, Recipient: %v %v", pb.Shard(), pb.ID(), tx.Hash, tx.From, pb.bc.GetStateDB().GetBalance(tx.From), tx.To, pb.bc.GetStateDB().GetBalance(tx.To))
			} else if tx.TXType == types.SMARTCONTRACT {
				_, _, err := evm.Execute(evmMachine, pb.bc.GetStateDB(), tx.Data, tx.From, tx.To)
				if err != nil {
					tx.TXType = types.ABORT
					log.Errorf("[%v] (executeTransaction) cross smartcontract transaction execution fail %x: %v, balance: %v, train slot: %v, hotel slot: %v, deposit: %v", pb.Shard(), tx.Hash, err, pb.bc.GetStateDB().GetBalance(tx.From), pb.bc.GetStateDB().GetState(tx.ExternalAddressList[0], common.HexToHash("0")), pb.bc.GetStateDB().GetState(tx.ExternalAddressList[2], common.HexToHash("0")), pb.bc.GetStateDB().GetState(tx.ExternalAddressList[1], common.BytesToHash(utils.CalculateMappingSlotIndex(tx.From, 0))))
					continue
				}
				// log.StateInfof("[%v] (executeTransaction) cross smartcontract transaction execution success %x: balance: %v, train slot: %v, hotel slot: %v, deposit: %v", pb.Shard(), tx.Hash, pb.bc.GetStateDB().GetBalance(tx.From), pb.bc.GetStateDB().GetState(tx.ExternalAddressList[0], common.HexToHash("0")), pb.bc.GetStateDB().GetState(tx.ExternalAddressList[2], common.HexToHash("0")), pb.bc.GetStateDB().GetState(tx.ExternalAddressList[1], common.BytesToHash(utils.CalculateMappingSlotIndex(tx.From, 0))))
			}
		} else {
			// log.StateInfof("[%v] (executeTransaction) [Consensus Node %v, %v] Execute Local Transaction: %v", pb.ID(), pb.Shard(), pb.ID(), tx.Hash)
			if tx.TXType == types.TRANSFER {
				err := evm.Transfer(pb.bc.GetStateDB(), tx.From, tx.To, uint256.NewInt(uint64(tx.Value)))
				if err != nil {
					tx.TXType = types.ABORT
					log.Errorf("[%v] (executeTransaction) local tranfer transaction execution fail %x: %v", pb.ID(), tx.Hash, err)
					continue
				}
				// log.StateInfof("(executeTransaction) Execute Local Transfer Transaction Sender: %v %v, Recipient: %v %v", pb.Shard(), tx.From, pb.bc.GetStateDB().GetBalance(tx.From), tx.To, pb.bc.GetStateDB().GetBalance(tx.To))
			} else if tx.TXType == types.SMARTCONTRACT {
				// log.Debugf("Tx data: %x, From: %x, To: %x", tx.Data, tx.From, tx.To)
				_, _, err := evm.Execute(evmMachine, pb.bc.GetStateDB(), tx.Data, tx.From, tx.To)
				if err != nil {
					tx.TXType = types.ABORT
					log.Errorf("[%v] (executeTransaction) local smartcontract transaction execution fail %x: %v, balance: %v, train slot: %v, hotel slot: %v, deposit: %v", pb.Shard(), tx.Hash, err, pb.bc.GetStateDB().GetBalance(tx.From), pb.bc.GetStateDB().GetState(tx.ExternalAddressList[0], common.HexToHash("0")), pb.bc.GetStateDB().GetState(tx.ExternalAddressList[2], common.HexToHash("0")), pb.bc.GetStateDB().GetState(tx.ExternalAddressList[1], common.BytesToHash(utils.CalculateMappingSlotIndex(tx.From, 0))))
					continue
				}
				// log.StateInfof("[%v] (executeTransaction) local smartcontract transaction execution success %x: balance: %v, train slot: %v, hotel slot: %v, deposit: %v", pb.Shard(), tx.Hash, pb.bc.GetStateDB().GetBalance(tx.From), pb.bc.GetStateDB().GetState(tx.ExternalAddressList[0], common.HexToHash("0")), pb.bc.GetStateDB().GetState(tx.ExternalAddressList[2], common.HexToHash("0")), pb.bc.GetStateDB().GetState(tx.ExternalAddressList[1], common.BytesToHash(utils.CalculateMappingSlotIndex(tx.From, 0))))
			} else if tx.TXType == types.DEPLOY {
				_, _, err := evm.Deploy(evmMachine, pb.bc.GetStateDB(), tx.Data, tx.From)
				if err != nil {
					tx.TXType = types.ABORT
					log.Errorf("[%v] (executeTransaction) local deploy transaction execution fail %x: %v", pb.ID(), tx.Hash, err)
					continue
				}
				//log.StateInfof("(executeTransaction) Deploy Smart Contract Sender: %v %v, Contract: %v", pb.Shard(), tx.From, pb.bc.GetStateDB().GetBalance(tx.From), contract_address)
			}
		}
	}
}

func (pb *PBFT) CloseFDCM() {
	fdcm := make([]*common.Address, 0)
	fdcm = append(fdcm, pb.fdcm...)
	if len(pb.fdcm) > len(fdcm) {
		pb.fdcm = pb.fdcm[len(fdcm):]
	} else {
		pb.fdcm = []*common.Address{}
	}
	for _, fAddress := range fdcm {
		pb.bc.GetStateDB().DeleteAccount(*fAddress)
	}
	pb.fdcm = []*common.Address{}
}

/* Timer */
func (pb *PBFT) ProcessRemoteTmo(tmo *pacemaker.TMO) {
	log.Debugf("[%v] (ProcessRemoteTmo) processing remote timeout from %v for newview %v", pb.ID(), tmo.NodeID, tmo.NewView)

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
			// log.Debugf("[%v] (ProcessRemoteTmo) Got more than f+1 message Propose for new view %v", pb.ID(), tmo.NewView)

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
			pb.detectedTmos = make(map[types.NodeID]*pacemaker.TMO)
		}
		return
	}

	// detectedTmos 초기화
	if len(pb.detectedTmos) > 0 {
		pb.detectedTmos = make(map[types.NodeID]*pacemaker.TMO)
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

	// log.Debugf("[%v] (ProcessRemoteTmo) a tc is built for New View %v", pb.ID(), tc.NewView)

	tc.NodeID = pb.ID()

	pb.SetRole(types.LEADER)

	// log.Debugf("[%v] (ProcessRemoteTmo) finished processing remote timeout from %v for newview %v", pb.ID(), tmo.NodeID, tmo.NewView)

	pb.Broadcast(tc)
	pb.ProcessTC(tc)
}

func (pb *PBFT) ProcessLocalTmo(view types.View) {
	log.Debugf("[%v] (ProcessLocalTmo) processing local timeout for view %v", pb.ID(), view)

	// BlockHeightLast = BlockHeight of which block is lastly appended into blockchain
	// BlockHeightPrepared = BlockHeight of which block is lastly prepared
	// BlockHeightLast is always same with BlockHeightPrepared because block is immediately inserted into blockchain after prepared...
	// If different, BlockHeightPrepared shouldn't be in the chain
	pb.SetState(types.VIEWCHANGING)

	tmo := &pacemaker.TMO{
		Shard:               pb.Shard(),
		Epoch:               pb.pm.GetCurEpoch(),
		View:                view,
		NewView:             pb.pm.GetNewView(),
		AnchorView:          pb.pm.GetAnchorView(),
		NodeID:              pb.ID(),
		BlockHeightLast:     pb.GetHighBlockHeight(),
		BlockHeightPrepared: pb.GetHighBlockHeight(),
		BlockLast:           pb.bc.GetBlockByBlockHeight(pb.GetHighBlockHeight()).(*blockchain.WorkerBlock),
		BlockPrepared:       nil,
		HighQC:              pb.GetHighQC(),
	}

	//
	if blockWithGlobalVariable, exist := pb.agreeingBlocks[tmo.Epoch][tmo.View][tmo.BlockHeightLast+1]; exist {
		tmo.BlockHeightPrepared += 1
		tmo.BlockPrepared = blockWithGlobalVariable.WorkerBlock
	}

	log.Debugf("[%v] (ProcessLocalTmo) tmo is built for view %v new view %v anchor view %v last blockheight %v prepared blockheight %v", pb.ID(), tmo.View, tmo.NewView, tmo.AnchorView, tmo.BlockHeightLast, tmo.BlockHeightPrepared)

	pb.BroadcastToSome(pb.FindCommitteesFor(tmo.Epoch), tmo)
	pb.ProcessRemoteTmo(tmo)

	log.Debugf("[%v] (ProcessLocalTmo) finished processing local timeout for view %v", pb.ID(), view)
}

func (pb *PBFT) ProcessTC(tc *pacemaker.TC) {
	log.Debugf("[%v] (ProcessTC) processing timeout certification for new view %v", pb.ID(), tc.NewView)

	pb.SetState(types.READY)

	curEpoch, curView := pb.pm.GetCurEpochView()

	delete(pb.agreeingBlocks[curEpoch][curView], tc.BlockHeightNew)

	pb.pm.UpdateView(tc.NewView - 1)
	pb.pm.UpdateAnchorView(tc.AnchorView)

	log.Debugf("[%v] (ProcessTC) finished processing timeout certification for new view %v", pb.ID(), tc.NewView)

	curEpoch, curView = pb.pm.GetCurEpochView()

	if block, exist := pb.bufferedBlocks[curEpoch][curView][tc.BlockHeightNew]; exist { // 일반 합의에 경우
		_ = pb.ProcessWorkerBlock(block)
		delete(pb.bufferedBlocks[curEpoch][curView], tc.BlockHeightNew)
	} else {
		log.Debugf("[%v] (ProcessTC) no buffered block for new view %v", pb.ID(), tc.NewView)
	}
}

/* Util */
func (pb *PBFT) GetHighQC() *quorum.QC {
	pb.mu.Lock()
	defer pb.mu.Unlock()
	return pb.highQC
}

func (pb *PBFT) GetHighCQC() *quorum.QC {
	pb.mu.Lock()
	defer pb.mu.Unlock()
	return pb.highCQC
}

func (pb *PBFT) GetChainStatus() string {
	chainGrowthRate := pb.bc.GetChainGrowth()
	blockIntervals := pb.bc.GetBlockIntervals()
	return fmt.Sprintf("[%v] The current view is: %v, chain growth rate is: %v, ave block interval is: %v", pb.ID(), pb.pm.GetCurView(), chainGrowthRate, blockIntervals)
}

// Vote가 완료된 가장 높은 BlockHeight
func (pb *PBFT) GetHighBlockHeight() types.BlockHeight {
	pb.mu.Lock()
	defer pb.mu.Unlock()
	return pb.highQC.BlockHeight
}

// 마지막으로 Commit 이 완료된 BlockHeight
func (pb *PBFT) GetLastBlockHeight() types.BlockHeight {
	pb.mu.Lock()
	defer pb.mu.Unlock()
	return pb.highCQC.BlockHeight
}

// 시작 BlockHeight
func (pb *PBFT) GetStartBlockHeight() types.BlockHeight {
	pb.mu.Lock()
	defer pb.mu.Unlock()
	return pb.startBlockHeight
}

func (pb *PBFT) GetLastVotedBlockHeight() types.BlockHeight {
	pb.mu.Lock()
	defer pb.mu.Unlock()
	return pb.lastVotedBlockHeight
}

func (pb *PBFT) updateHighQC(qc *quorum.QC) {
	pb.mu.Lock()
	defer pb.mu.Unlock()
	if qc.BlockHeight > pb.highQC.BlockHeight {
		pb.highQC = qc
	}
}

func (pb *PBFT) updateHighCQC(cqc *quorum.QC) {
	pb.mu.Lock()
	defer pb.mu.Unlock()
	if cqc.BlockHeight > pb.highCQC.BlockHeight {
		pb.highCQC = cqc

		pb.updatedQC <- cqc
		log.Debugf("[%v] (updateHighCQC) updated highCQC to %v", pb.ID(), cqc.BlockHeight)
	}
}

// update block height we have just voted block lastly
func (pb *PBFT) updateLastVotedBlockHeight(targetHeight types.BlockHeight) error {
	if targetHeight < pb.lastVotedBlockHeight {
		return errors.New("target blockHeight is lower than the last voted blockHeight")
	}
	pb.lastVotedBlockHeight = targetHeight
	return nil
}

// preferred block height is what we trully preferred block height
// which means preferred height confirmed by 1 block ahead
func (pb *PBFT) updatePreferredBlockHeight(qc *quorum.QC) error {
	if qc.BlockHeight <= 1 {
		return nil
	}
	_, err := pb.bc.GetBlockByID(qc.BlockID)
	if err != nil {
		return fmt.Errorf("cannot update preferred blockHeight: %v", err)
	}
	parentBlock, err := pb.bc.GetParentWorkerBlock(qc.BlockID)
	parentWorkerBlock := parentBlock.(*blockchain.WorkerBlock)
	if err != nil {
		return fmt.Errorf("cannot update preferred blockHeight: %v", err)
	}
	if parentWorkerBlock.BlockHeader.BlockHeight > pb.preferredBlockHeight {
		pb.preferredBlockHeight = parentWorkerBlock.BlockHeader.BlockHeight
	}

	return nil
}

func (pb *PBFT) ProcessReportByzantine(reportByzantine *byzantine.ReportByzantine) {
}
func (pb *PBFT) ProcessReplaceByzantine(replaceByzantine *byzantine.ReplaceByzantine) {
}
