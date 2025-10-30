package coordination_node

import (
	"encoding/gob"
	"time"

	"go.uber.org/atomic"

	"unishard/blockchain"
	"unishard/byzantine"
	"unishard/config"
	"unishard/crypto"
	"unishard/election"
	"unishard/mempool"
	"unishard/message"
	"unishard/node"
	"unishard/pacemaker"
	"unishard/pbft"
	"unishard/quorum"
	"unishard/types"
	"unishard/utils"
)

type (
	Replica struct {
		node.Node
		*pbft.CoordinationPBFT
		*election.Static
		pd                  *mempool.Producer
		pm                  *pacemaker.CoordinationPacemaker
		start               chan bool // signal to start the node
		isStarted           atomic.Bool
		isByz               bool
		timer               *time.Timer // timeout for each view
		committedBlocks     chan *blockchain.CoordinationBlock
		forkedBlocks        chan *blockchain.CoordinationBlock
		reservedBlock       chan *blockchain.CoordinationBlock
		eventChan           chan interface{}
		preparingBlock      blockchain.Block // block waiting for enough lockResponse
		reservedPreparBlock blockchain.Block // reserved Preparing Block from ViewChange
		/* for monitoring node statistics */
		thrus               string
		lastViewTime        time.Time
		startTime           time.Time
		tmpTime             time.Time
		voteStart           time.Time
		totalCreateDuration time.Duration
		totalDelay          time.Duration
		totalRoundTime      time.Duration
		totalVoteTime       time.Duration
		receivedNo          int
		roundNo             int
		voteNo              int
		totalCommittedTx    int
		latencyNo           int
		proposedNo          int
		processedNo         int
		committedNo         int

		proposeCandidate chan *blockchain.CoordinationBlockWithoutHeader

		// below global parameters are must invalidate per every block, so doesn't remain those as instance property.
		// globalSchedule        Schedule
	}
)

// NewReplica creates a new replica instance
func NewReplica(id types.NodeID, alg string, isByz bool, shard types.Shard) *Replica {
	r := new(Replica)
	r.Node = node.NewNode(id, isByz, shard)
	if isByz {
		// log.CDebugf("[%v] is Byzantine", r.ID())
	}

	if config.GetConfig().Master == "0" {
		r.Static = election.NewStatic(utils.NewNodeID(1), r.Shard(), config.Configuration.CommitteeNumber, config.Configuration.ValidatorNumber)
	}
	r.isByz = isByz
	r.pd = mempool.NewProducer()
	r.pm = pacemaker.NewCoordinationPacemaker(config.GetConfig().CommitteeNumber)
	r.start = make(chan bool)
	r.eventChan = make(chan interface{})
	r.committedBlocks = make(chan *blockchain.CoordinationBlock, 100)
	r.forkedBlocks = make(chan *blockchain.CoordinationBlock, 100)
	r.reservedBlock = make(chan *blockchain.CoordinationBlock)
	r.preparingBlock = nil
	r.reservedPreparBlock = nil
	r.proposeCandidate = make(chan *blockchain.CoordinationBlockWithoutHeader, 100)

	r.Register(blockchain.CoordinationAccept{}, r.HandleAccept)
	r.Register(blockchain.CoordinationBlockWithoutHeader{}, r.HandleCoordinationBlockWithoutHeader)
	r.Register(blockchain.CoordinationBlock{}, r.HandleCoordinationBlock)
	r.Register(quorum.Collection[quorum.Vote]{}, r.HandleVote)
	r.Register(quorum.Collection[quorum.Commit]{}, r.HandleCommit)
	r.Register(pacemaker.CoordinationTMO{}, r.HandleTmo)
	r.Register(pacemaker.CoordinationTC{}, r.HandleTc)
	r.Register(byzantine.ReportByzantine{}, r.HandleReportByzantine)
	r.Register(byzantine.ReplaceByzantine{}, r.HandleReplaceByzantine)
	r.Register(message.Transaction{}, r.HandleTxn)
	r.Register(message.Query{}, r.handleQuery)
	r.Register(message.RequestLeader{}, r.handleRequestLeader)
	r.Register(message.ReportByzantine{}, r.handleReportByzantine)

	gob.Register(blockchain.CoordinationAccept{})
	gob.Register(blockchain.CoordinationBlock{})
	gob.Register(blockchain.CoordinationBlockWithoutHeader{})
	gob.Register(quorum.Collection[quorum.Vote]{})
	gob.Register(quorum.Collection[quorum.Commit]{})
	gob.Register(pacemaker.CoordinationTC{})
	gob.Register(pacemaker.CoordinationTMO{})
	gob.Register(byzantine.ReportByzantine{})
	gob.Register(byzantine.ReplaceByzantine{})
	gob.Register(message.ConsensusNodeRegister{})

	// Is there a better way to reduce the number of parameters?

	r.CoordinationPBFT = pbft.NewCoordinationPBFT(
		r.Node,
		r.pm,
		r.Static,
		r.committedBlocks,
		r.forkedBlocks,
		r.reservedBlock,
	)
	return r
}

/* Processors */
// view start, 블록 보내기 시작 / GG
func (r *Replica) processNewView(curEpoch types.Epoch, curView types.View) {

	// log.CDebugf("[%v] process new round for View: %v Epoch: %v 리더 노드[%v]", r.ID(), curView, curEpoch, r.FindLeaderFor(curView, curEpoch))
	if !r.IsLeader(r.ID(), curView, curEpoch) {
		// Committee Check
		return
	}

	r.SetRole(types.LEADER)
	r.eventChan <- types.EpochView{Epoch: curEpoch, View: curView}
}

func (r *Replica) processCommittedBlock(block blockchain.Block) {
}

func (r *Replica) processForkedBlock(block blockchain.Block) {
}

func (r *Replica) initConsensus(epoch types.Epoch, view types.View) {
	createStart := time.Now()
	block := new(blockchain.CoordinationBlock)
	block.BlockHeader = new(blockchain.CoordinationBlockHeader)
	block.BlockData = new(blockchain.CoordinationBlockData)
	block.QC = new(quorum.QC)
	block.CQC = new(quorum.QC)

	r.proposedNo++
	createEnd := time.Now()
	createDuration := createEnd.Sub(createStart)
	block.BlockHeader.Timestamp = time.Now()
	r.totalCreateDuration += createDuration

	r.proposeBlock(block)
}

func (r *Replica) proposeBlock(block *blockchain.CoordinationBlock) {
	r.BroadcastToSome(r.FindCommitteesFor(block.BlockHeader.Epoch), block)
	_ = r.CoordinationPBFT.ProcessCoordinationBlock(block)
	r.voteStart = time.Now()
	r.preparingBlock = nil
}

/* Listeners */

// ListenCommittedBlocks listens committed blocks and forked blocks from the protocols
func (r *Replica) ListenCommittedBlocks() {
	for {
		select {
		case committedBlock := <-r.committedBlocks:
			r.processCommittedBlock(committedBlock)
		case forkedBlock := <-r.forkedBlocks:
			r.processForkedBlock(forkedBlock)
		case reservedPrepareBlock := <-r.reservedBlock:
			r.reservedPreparBlock = reservedPrepareBlock
		}
	}
}

// ListenLocalEvent listens new view and timeout events
func (r *Replica) ListenLocalEvent() {
	r.lastViewTime = time.Now()
	r.timer = time.NewTimer(r.pm.GetTimerForView())
	for {
		// Committee Check, if Validator, stop timer
		if r.Role() == types.VALIDATOR {
			r.timer.Stop()
		} else {
			switch r.State() {
			case types.READY:
				r.timer.Reset(r.pm.GetTimerForView())
			case types.VIEWCHANGING:
				r.timer.Reset(r.pm.GetTimerForViewChange())
			}
		}
	L:
		for {
			select {
			case EpochView := <-r.pm.EnteringViewEvent():
				var (
					epoch = EpochView.Epoch
					view  = EpochView.View
				)
				if view >= 2 {
					r.totalVoteTime += time.Since(r.voteStart)
				}
				// measure round time
				now := time.Now()
				lasts := now.Sub(r.lastViewTime)
				r.totalRoundTime += lasts
				r.roundNo++
				r.lastViewTime = now
				// if committee, set role committee. else, set role validator
				if r.IsCommittee(r.ID(), epoch) {
					r.SetRole(types.COMMITTEE)
				} else {
					r.SetRole(types.VALIDATOR)
				}
				// log.CDebugf("[%v] the last view %v lasts %v milliseconds", r.ID(), view-1, lasts.Milliseconds())
				// r.eventChan <- EpochView
				r.processNewView(epoch, view)
				break L
			case <-r.pm.EnteringTmoEvent():
				// Not ViewChange State: f + 1 tmo obtained : B cond
				// ViewChange State: higher anchorview tmo obtained : D cond
				// log.CDebugf("[%v] view-change start on for newview %v", r.ID(), newView)
				r.CoordinationPBFT.ProcessLocalTmo(r.pm.GetCurView())
				break L
			case <-r.timer.C:
				// already in viewchange state : C cond
				// log.CDebugf("[%v] timeout occurred start viewchange", r.ID())

				if r.State() == types.VIEWCHANGING {
					r.pm.UpdateNewView(r.pm.GetNewView() + 1)
				}
				r.CoordinationPBFT.ProcessLocalTmo(r.pm.GetCurView())

				break L
			}
		}
	}
}

func (r *Replica) startSignal() {
	if !r.isStarted.Load() {
		r.startTime = time.Now()
		r.tmpTime = time.Now()
		// log.CDebugf("노드[%v] 부팅중", r.ID())
		r.isStarted.Store(true)
		r.start <- true
	}
}

func (r *Replica) limitedProposer() {
	for coordinationBlockWithoutHeader := range r.proposeCandidate {
		block := r.CoordinationPBFT.CreateCoordinationBlock(coordinationBlockWithoutHeader)
		block.Proposer = r.ID()
		leader_signature, _ := crypto.PrivSign(crypto.IDToByte(block.BlockHash), nil)
		block.CommitteeSignature = append(block.CommitteeSignature, leader_signature)

		r.proposeBlock(block)
		// log.CDebugf("[%v] (limitedProposer) block %s proposed, blockHeight %v view %v epoch %v", r.ID(), block.BlockHash, r.CoordinationPBFT.GetLastBlockHeight(), r.pm.GetCurView(), r.pm.GetCurEpoch())
	}
}

// Start event loop
func (r *Replica) Start() {
	r.Node.DistLogger().BootingSequence(1)
	go r.Run()
	go r.limitedProposer()
	r.ElectCommittees(crypto.MakeID(0), 1)
	// log.CDebugf("[%v] elect new committees %v for new epoch 1", r.ID(), committes)
	if r.IsCommittee(r.ID(), 1) {
		r.SetRole(types.COMMITTEE)
	}
	// wait for the start signal
	<-r.start
	r.Node.DistLogger().BootingSequence(2)
	go r.ListenLocalEvent()
	go r.ListenCommittedBlocks()
	for {
		for r.isStarted.Load() {
			event := <-r.eventChan
			switch v := event.(type) {
			case types.EpochView:
				// TODO: 유니샤드는 advance view 동작을 사용하지 않으며 아래 함수가 불리면 중복 view 전진이 발생하여 seq=0인 블럭이 생성됩니다
				//       추후에 advance view 관련 모든 동작을 제거해야합니다.
				//r.initConsensus(v.Epoch, v.View)
			case blockchain.CoordinationBlock:
				// log.CDebugf("start Process CoordinationBlock")
				_ = r.CoordinationPBFT.ProcessCoordinationBlock(&v)
				r.voteStart = time.Now()
				r.processedNo++
			// case blockchain.Block:
			// 	startProcessTime := time.Now()
			// 	r.totalProposeDuration += startProcessTime.Sub(v.Timestamp)
			// 	_ = r.WorkerSafety.ProcessBlock(&v)
			// 	r.totalProcessDuration += time.Since(startProcessTime)
			// 	r.voteStart = time.Now()
			// 	r.processedNo++
			case quorum.Collection[quorum.Vote]:
				startProcessTime := time.Now()
				r.CoordinationPBFT.ProcessVote(&v)
				processingDuration := time.Since(startProcessTime)
				r.totalVoteTime += processingDuration
				r.voteNo++
			case quorum.Collection[quorum.Commit]: // 2, 3, 4
				// startProcessTime := time.Now()
				r.CoordinationPBFT.ProcessCommit(&v) // 2, 3, 4
				// processingDuration := time.Since(startProcessTime)
				// r.totalVoteTime += processingDuration
				// r.voteNo++
			case blockchain.CoordinationAccept:
				r.CoordinationPBFT.ProcessAccept(&v)
			case pacemaker.CoordinationTMO:
				r.CoordinationPBFT.ProcessRemoteTmo(&v)
			case pacemaker.CoordinationTC:
				r.CoordinationPBFT.ProcessTC(&v)
			case byzantine.ReportByzantine:
				r.CoordinationPBFT.ProcessReportByzantine(&v)
			case byzantine.ReplaceByzantine:
				r.CoordinationPBFT.ProcessReplaceByzantine(&v)
			}
		}
	}
}
