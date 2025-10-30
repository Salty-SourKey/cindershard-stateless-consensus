package worker

import (
	"encoding/gob"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/holiman/uint256"
	"github.com/samber/lo"
	"go.uber.org/atomic"

	"unishard/blockchain"
	"unishard/config"
	"unishard/crypto"
	"unishard/election"
	"unishard/log"
	"unishard/mempool"
	"unishard/message"
	"unishard/node"
	"unishard/pacemaker"
	"unishard/pbft"
	"unishard/quorum"
	"unishard/snapshot_control_table"
	"unishard/types"
	"unishard/utils"
)

// The node structure
type Replica struct {
	node.Node
	*pbft.PBFT
	*election.Static
	snapshotControlTable snapshot_control_table.SnapshotControlTable
	sct                  map[common.Address]map[common.Hash]*blockchain.SnapshotControlTable
	pd                   *mempool.Producer
	pm                   *pacemaker.Pacemaker
	start                chan bool // signal to start the node
	isStarted            atomic.Bool
	isByz                bool
	timer                *time.Timer // timeout for each view
	committedBlocks      chan *blockchain.WorkerBlock
	forkedBlocks         chan *blockchain.WorkerBlock
	reservedBlock        chan *blockchain.WorkerBlock
	eventChan            chan interface{}
	preparingBlock       *blockchain.WorkerBlock // block waiting for enough lockResponse
	reservedPreparBlock  *blockchain.WorkerBlock // reserved Preparing Block from ViewChange
	thrus                string
	lastViewTime         time.Time
	startTime            time.Time
	tmpTime              time.Time
	voteStart            time.Time
	totalDelay           time.Duration
	totalRoundTime       time.Duration
	totalVoteTime        time.Duration
	receivedNo           int
	roundNo              int
	voteNo               int
	totalCommittedTx     int
	latencyNo            int
	processedNo          int
	committedNo          int
	proposeCandidate     chan *blockchain.WorkerConsensusPayload

	// For Change Test
	proposeNewViewSignMap       map[types.BlockHeight][]crypto.Signature
	proposeNewViewQuorumChecker map[types.BlockHeight]int
	acceptNewViewChecker        map[types.BlockHeight]bool

	reportByzantineSignMap       map[types.BlockHeight][]crypto.Signature
	reportByzantineQuorumChecker map[types.BlockHeight]int
	replaceByzantineChecker      map[types.BlockHeight]bool

	proposeNewCommitteeSignMap       map[types.BlockHeight][]crypto.Signature
	proposeNewCommitteeQuorumChecker map[types.BlockHeight]int
	ChangeCommitteeChecker           map[types.BlockHeight]bool
}

// Create a new replica instance (node)
func NewReplica(id types.NodeID, alg string, isByz bool, shard types.Shard) *Replica {
	r := new(Replica)
	r.Node = node.NewNode(id, isByz, shard)
	if isByz {
		log.Debugf("[%v] is Byzantine", r.ID())
	}

	if config.GetConfig().Master == "0" {
		r.Static = election.NewStatic(utils.NewNodeID(1), r.Shard(), config.Configuration.CommitteeNumber, config.Configuration.ValidatorNumber)
	}
	r.snapshotControlTable = snapshot_control_table.SnapshotControlTable{
		Table: map[common.Address]map[common.Hash]*snapshot_control_table.SnapshotControlEntity{},
		Mutex: sync.RWMutex{},
	}
	r.sct = make(map[common.Address]map[common.Hash]*blockchain.SnapshotControlTable)
	r.isByz = isByz
	r.pd = mempool.NewProducer()
	r.pm = pacemaker.NewPacemaker(config.GetConfig().CommitteeNumber)
	r.start = make(chan bool)
	r.eventChan = make(chan interface{})
	r.committedBlocks = make(chan *blockchain.WorkerBlock, 1000)
	r.forkedBlocks = make(chan *blockchain.WorkerBlock, 1000)
	r.reservedBlock = make(chan *blockchain.WorkerBlock)
	r.proposeCandidate = make(chan *blockchain.WorkerConsensusPayload, 1000)
	r.preparingBlock = nil
	r.reservedPreparBlock = nil

	// For Change Test
	r.proposeNewViewSignMap = map[types.BlockHeight][]crypto.Signature{}
	r.proposeNewViewQuorumChecker = make(map[types.BlockHeight]int, 1000)
	r.acceptNewViewChecker = make(map[types.BlockHeight]bool, 1000)
	r.reportByzantineSignMap = map[types.BlockHeight][]crypto.Signature{}
	r.reportByzantineQuorumChecker = make(map[types.BlockHeight]int, 1000)
	r.replaceByzantineChecker = make(map[types.BlockHeight]bool, 1000)
	r.proposeNewCommitteeSignMap = map[types.BlockHeight][]crypto.Signature{}
	r.proposeNewCommitteeQuorumChecker = make(map[types.BlockHeight]int, 1000)
	r.ChangeCommitteeChecker = make(map[types.BlockHeight]bool, 1000)

	/* Register to gob en/decoder */
	gob.Register(blockchain.Accept{})
	gob.Register(blockchain.WorkerConsensusPayload{})
	gob.Register(blockchain.ProposedWorkerBlock{})
	gob.Register(blockchain.WorkerBlock{})
	gob.Register(message.Experiment{})
	gob.Register(blockchain.CoordinationBlock{})
	gob.Register(quorum.Collection[quorum.Vote]{})
	gob.Register(quorum.Collection[quorum.Commit]{})
	gob.Register(pacemaker.TC{})
	gob.Register(pacemaker.TMO{})
	gob.Register(message.ConsensusNodeRegister{})
	gob.Register(message.ProposeNewView{})
	gob.Register(message.AcceptNewView{})
	gob.Register(message.ReportByzantine{})
	gob.Register(message.ReplaceByzantine{})
	gob.Register(message.ProposeNewCommittee{})
	gob.Register(message.ChangeCommittee{})
	gob.Register(message.Transaction{})

	/* Register message handlers */
	r.Register(blockchain.Accept{}, r.HandleAccept)
	r.Register(blockchain.WorkerConsensusPayload{}, r.HandleWorkerConsensusPayload)
	r.Register(blockchain.ProposedWorkerBlock{}, r.HandleProposedWorkerBlock)
	r.Register(quorum.Collection[quorum.Vote]{}, r.HandleVote)
	r.Register(quorum.Collection[quorum.Commit]{}, r.HandleCommit)
	r.Register(pacemaker.TMO{}, r.HandleTmo)
	r.Register(pacemaker.TC{}, r.HandleTc)
	r.Register(message.Transaction{}, r.HandleTxn)
	r.Register(message.Query{}, r.handleQuery)
	r.Register(message.RequestLeader{}, r.handleRequestLeader)
	r.Register(message.ProposeNewView{}, r.HandleProposeNewView)
	r.Register(message.AcceptNewView{}, r.HandleAcceptNewView)
	r.Register(message.ReportByzantine{}, r.HandleReportByzantine)
	r.Register(message.ReplaceByzantine{}, r.HandleReplaceByzantine)
	r.Register(message.ProposeNewCommittee{}, r.HandleProposeNewCommittee)
	r.Register(message.ChangeCommittee{}, r.HandleChangeCommittee)
	r.Register(message.Transaction{}, r.HandleTxn)

	r.PBFT = pbft.NewPBFT(r.Node, r.pm, r.pd, r.Static, r.committedBlocks, r.forkedBlocks, r.reservedBlock)
	return r
}

// Process the received worker consensus payload only if the node is the current leader (block proposer)
func (r *Replica) HandleWorkerConsensusPayload(bm blockchain.WorkerConsensusPayload) {
	r.receivedNo++
	r.startSignal()
	if r.pm.GetCurView() == 0 {
		r.pm.AdvanceView(0)
	}

	r.proposeCandidate <- &bm
}

// Generate a worker block using the received worker consensus payload,
// and then propose (propagate) it to all other nodes
func (r *Replica) ProposeWorkerBlock() {
	ticker := time.NewTicker(800 * time.Millisecond)
	for range ticker.C {
		payload_data := r.pd.GeneratePayload(config.Configuration.BlockSize)
		if config.Configuration.Mode == "Change_AllCommittee" && r.State() == types.ALLCOMMITEEECHANGED {
			r.ReplaceAllCommittee(r.pm.GetCurEpoch())
			if r.IsLeader(r.ID(), r.pm.GetCurView(), r.pm.GetCurEpoch()) {
				r.SetRole(types.LEADER)
			} else if r.IsCommittee(r.ID(), r.pm.GetCurEpoch()) {
				r.SetRole(types.COMMITTEE)
			} else if r.IsValidator(r.ID(), r.pm.GetCurEpoch()) {
				r.SetRole(types.VALIDATOR)
			}
			r.SetState(types.READY)
			log.Errorf("[%v] Leader: %v, Committee: %v, Validator: %v", r.ID(), r.FindLeaderFor(r.pm.GetCurView(), r.pm.GetCurEpoch()), r.FindCommitteesFor(r.pm.GetCurEpoch()), r.FindValidatorsFor(r.pm.GetCurEpoch()))
		}
		if !r.IsLeader(r.ID(), r.pm.GetCurView(), r.pm.GetCurEpoch()) || r.State() == types.LEADERCHANGING || r.State() == types.COMMITTEECHANGING || r.State() == types.ALLCOMMITTEEECHANGING {
			continue
		}

		// Create local execution sequence
		localExecutionSequence := payload_data

		for _, tx := range localExecutionSequence {
			tx.Timestamp = time.Now().UnixMilli()
		}

		rwSet := make([]common.Address, 0)
		for _, tx := range localExecutionSequence {
			if tx.TXType == types.TRANSFER {
				if !utils.Contains(rwSet, tx.From) {
					rwSet = append(rwSet, tx.From)
				}
				if !utils.Contains(rwSet, tx.To) {
					rwSet = append(rwSet, tx.To)
				}
			}
		}

		keys, values, proofDB, err := r.PBFT.GetBlockChain().GetStateDB().GetProofDB(rwSet)
		if err != nil {
			log.Error("Failed to get key-value pairs", "error", err)
			continue
		}

		var merkleProofkeys [][]byte
		var merkleProofvalues [][]byte

		it := proofDB.(ethdb.Database).NewIterator(nil, nil)
		for it.Next() {
			merkleProofkeys = append(merkleProofkeys, it.Key())
			merkleProofvalues = append(merkleProofvalues, it.Value())
		}
		it.Release()

		// Build worker block and propagate it
		block := r.PBFT.CreateWorkerBlock(localExecutionSequence, keys, values, merkleProofkeys, merkleProofvalues)
		block.Proposer = r.ID()
		leaderSignature, _ := crypto.PrivSign(crypto.IDToByte(block.BlockHash), nil)
		block.CommitteeSignature = append(block.CommitteeSignature, leaderSignature)

		workerBlock := blockchain.ProposedWorkerBlock{
			WorkerBlock:          block,
			GlobalSnapshot:       []*blockchain.LocalSnapshot{},
			GlobalContractBundle: []*blockchain.LocalContract{},
		}
		r.proposeBlock(&workerBlock)
	}
}

// Check if the received cross-shard transaction is a local-commit transaction in the respective shard
func (r *Replica) CheckLocalCommit(ct *message.Transaction) bool {
	switch ct.TXType {
	case types.TRANSFER:
		// Check if the 'from' or 'to' address of the fund-transfer transaction belongs to the respective shard
		fromShard := utils.CalculateShardToSend([]common.Address{ct.From}, config.GetConfig().ShardCount)[0]
		toShard := utils.CalculateShardToSend([]common.Address{ct.To}, config.GetConfig().ShardCount)[0]
		if fromShard == r.Shard() || toShard == r.Shard() {
			return true
		}
		return false
	case types.SMARTCONTRACT:
		// Check if the write set address of the smart contract transaction belongs to the respective shard
		for _, rwSet := range ct.RwSet {
			rwSetShard := utils.CalculateShardToSend([]common.Address{rwSet.Address}, config.GetConfig().ShardCount)[0]
			if rwSetShard == r.Shard() && len(rwSet.WriteSet) > 0 {
				return true
			}
		}
		fromShard := utils.CalculateShardToSend([]common.Address{ct.From}, config.GetConfig().ShardCount)[0]
		return fromShard == r.Shard()
	}

	return false
}

// Temporarily replicate the external state variables and contracts contained in the received global snapshot and global contract bundle
func (r *Replica) ReplicateExternalStateVariable(globalSnapshot []*blockchain.LocalSnapshot, globalContractBundle []*blockchain.LocalContract) (externalAddress []*common.Address) {
	stateDB := r.PBFT.GetBlockChain().GetStateDB()
	for _, code := range globalContractBundle {
		if utils.CalculateShardToSend([]common.Address{code.Address}, config.GetConfig().ShardCount)[0] != r.Shard() {
			// Temporarily create external contracts that do not belong to the shard
			stateDB.CreateTemporaryAccount(code.Address)
			stateDB.SetCode(code.Address, code.Code)
			externalAddress = append(externalAddress, &code.Address)
		}
	}
globalSnapshot:
	for _, snapshot := range globalSnapshot {
		if utils.CalculateShardToSend([]common.Address{snapshot.Address}, config.GetConfig().ShardCount)[0] != r.Shard() {
			isForeignSmartContract := func(snapshotAddress common.Address) bool {
				for _, externalAddr := range externalAddress {
					if *externalAddr == snapshotAddress {
						return true
					}
				}
				return false
			}(snapshot.Address)

			if isForeignSmartContract {
				// Update stateDB if the global snapshot contains state variables within external contracts
				for _, externalAddr := range externalAddress {
					if *externalAddr == snapshot.Address {
						stateDB.SetState(snapshot.Address, snapshot.Slot, common.HexToHash(snapshot.Value))
						continue globalSnapshot
					}
				}
			} else {
				// Create temporary account and update stateDB if the global snapshot contains state variables of external user accounts
				stateDB.CreateTemporaryAccount(snapshot.Address)
				externalAddress = append(externalAddress, &snapshot.Address)
				balance, _ := strconv.ParseInt(snapshot.Value, 0, 64)
				stateDB.SetBalance(snapshot.Address, uint256.NewInt(uint64(balance)))
				stateDB.SetNonce(snapshot.Address, 0)
			}
		}
	}

	return externalAddress
}

// Delete the replicated external state variables and contracts
func (r *Replica) DeleteExternalStateVariable(externalAddress []*common.Address) {
	for _, externalAddr := range externalAddress {
		r.PBFT.GetBlockChain().GetStateDB().DeleteAccount(*externalAddr)
	}
}

// Create local snapshot based on the received cross-shard transactions
func (r *Replica) CreateLocalSnapshot(receivedCrossTransaction []*message.Transaction, gc types.BlockHeight) (localSnapshot []*blockchain.LocalSnapshot, localContractBundle []*blockchain.LocalContract) {
	// If the RTCS in the SCT is non-empty and the protected flag is false,
	// include it in the local snapshot and update the SCT with the current state value
	stateDB := r.PBFT.GetBlockChain().GetStateDB()
	slot, addr := r.snapshotControlTable.GetNonEmptyRTCS()
	for i := 0; i < len(slot); i++ {
		if !r.snapshotControlTable.IsProtected(slot[i], addr[i]) {
			currentSnapshot := r.snapshotControlTable.FindSnapshot(slot[i], addr[i])

			// Capture the current value as a snapshot
			currentValue := ""
			if stateDB.GetCode(addr[i]) == nil {
				currentValue = stateDB.GetBalance(addr[i]).String()
			} else {
				currentValue = stateDB.GetState(addr[i], slot[i]).String()
			}

			// Update the SCT and add it to the local snapshot
			r.snapshotControlTable.UpdateSnapshot(slot[i], addr[i], currentValue, currentSnapshot.RTCS, gc, true)
			localSnapshot = append(localSnapshot, r.snapshotControlTable.FindSnapshot(slot[i], addr[i]))
		}
	}

	// Verify the read set of the received cross-shard transaction and include it in the local snapshot
	for _, ct := range receivedCrossTransaction {
		switch ct.TXType {
		case types.TRANSFER:
			// Verify if the 'from' or 'to' address of the fund-transfer transaction belongs to the respective shard,
			// update the SCT for the corresponding address, and add it to the local snapshot
			fromShard := utils.CalculateShardToSend([]common.Address{ct.From}, config.GetConfig().ShardCount)[0]
			toShard := utils.CalculateShardToSend([]common.Address{ct.To}, config.GetConfig().ShardCount)[0]
			if fromShard == r.Shard() {
				rtcs := []common.Hash{}
				if r.snapshotControlTable.Exist(utils.SlotToKey(0), ct.From) {
					currentSnapshot := r.snapshotControlTable.FindSnapshot(utils.SlotToKey(0), ct.From)
					rtcs = append(rtcs, currentSnapshot.RTCS...)
					rtcs = append(rtcs, ct.Hash)
				} else {
					rtcs = append(rtcs, ct.Hash)
				}
				currentValue := stateDB.GetBalance(ct.From).String()
				r.snapshotControlTable.UpdateSnapshot(utils.SlotToKey(0), ct.From, currentValue, rtcs, gc, true)
				localSnapshot = append(localSnapshot, r.snapshotControlTable.FindSnapshot(utils.SlotToKey(0), ct.From))
			}
			if toShard == r.Shard() {
				rtcs := []common.Hash{}
				if r.snapshotControlTable.Exist(utils.SlotToKey(0), ct.To) {
					currentSnapshot := r.snapshotControlTable.FindSnapshot(utils.SlotToKey(0), ct.To)
					rtcs = append(rtcs, currentSnapshot.RTCS...)
					rtcs = append(rtcs, ct.Hash)
				} else {
					rtcs = append(rtcs, ct.Hash)
				}
				currentValue := stateDB.GetBalance(ct.To).String()
				r.snapshotControlTable.UpdateSnapshot(utils.SlotToKey(0), ct.To, currentValue, rtcs, gc, true)
				localSnapshot = append(localSnapshot, r.snapshotControlTable.FindSnapshot(utils.SlotToKey(0), ct.To))
			}
		case types.SMARTCONTRACT:
			// Verify if the read set address of the smart contract transaction belongs to the respective shard,
			// update the SCT for the corresponding address, and add it to the local snapshot
			for _, rwSet := range ct.RwSet {
				snapshotShard := utils.CalculateShardToSend([]common.Address{rwSet.Address}, config.GetConfig().ShardCount)[0]
				if snapshotShard == r.Shard() {
					for _, rs := range rwSet.ReadSet {
						rtcs := []common.Hash{}
						if r.snapshotControlTable.Exist(common.HexToHash(rs), rwSet.Address) {
							currentSnapshot := r.snapshotControlTable.FindSnapshot(common.HexToHash(rs), rwSet.Address)
							rtcs = append(rtcs, currentSnapshot.RTCS...)
							rtcs = append(rtcs, ct.Hash)
						} else {
							rtcs = append(rtcs, ct.Hash)
						}
						currentValue := stateDB.GetState(rwSet.Address, common.HexToHash(rs)).String()
						r.snapshotControlTable.UpdateSnapshot(common.HexToHash(rs), rwSet.Address, currentValue, rtcs, gc, true)
						localSnapshot = append(localSnapshot, r.snapshotControlTable.FindSnapshot(common.HexToHash(rs), rwSet.Address))
					}

					// Add the code of the contracts belonging to the shard
					// from the received smart contract cross-shard transactions to the contract bundle
					localContract := &blockchain.LocalContract{
						Address: rwSet.Address,
						Code:    stateDB.GetContractCode(rwSet.Address),
					}
					localContractBundle = append(localContractBundle, localContract)

				}
			}
			// In the case of a smart contract transaction, gas is consumed from the ct.From address,
			// so update the SCT for this address and add it to the local snapshot
			fromShard := utils.CalculateShardToSend([]common.Address{ct.From}, config.GetConfig().ShardCount)[0]
			if fromShard == r.Shard() {
				rtcs := []common.Hash{}
				if r.snapshotControlTable.Exist(utils.SlotToKey(0), ct.From) {
					currentSnapshot := r.snapshotControlTable.FindSnapshot(utils.SlotToKey(0), ct.From)
					rtcs = append(rtcs, currentSnapshot.RTCS...)
					rtcs = append(rtcs, ct.Hash)
				} else {
					rtcs = append(rtcs, ct.Hash)
				}
				currentValue := stateDB.GetBalance(ct.From).String()
				r.snapshotControlTable.UpdateSnapshot(utils.SlotToKey(0), ct.From, currentValue, rtcs, gc, true)
				localSnapshot = append(localSnapshot, r.snapshotControlTable.FindSnapshot(utils.SlotToKey(0), ct.From))
			}
		}
	}

	// Remove duplicates from the local snapshot
	localSnapshot = lo.UniqBy[*blockchain.LocalSnapshot, string](localSnapshot, func(item *blockchain.LocalSnapshot) string {
		return item.Address.Hex() + item.Slot.Hex()
	})

	// Remove duplicates from the local contract bundle
	localContractBundle = lo.UniqBy[*blockchain.LocalContract, string](localContractBundle, func(item *blockchain.LocalContract) string {
		return item.Address.Hex()
	})

	return localSnapshot, localContractBundle
}

// The following functions are used for BFT consensus within a shard
// They implement PBFT algorithm

func (r *Replica) HandleProposedWorkerBlock(workerBlock blockchain.ProposedWorkerBlock) {
	r.receivedNo++
	r.startSignal()
	if r.pm.GetCurView() == 0 {
		log.Debugf("[%v] start protocol ", r.ID())
		r.pm.AdvanceView(0)
	}
	block := workerBlock.WorkerBlock
	if !r.IsCommittee(r.ID(), block.BlockHeader.Epoch) {
		return
	}
	r.eventChan <- workerBlock
}

func (r *Replica) HandleAccept(accept blockchain.Accept) {
	r.startSignal()
	if r.pm.GetCurView() == 0 {
		r.pm.AdvanceView(0)
	}

	commitedBlock := accept.CommittedBlock
	if commitedBlock.BlockHeader.BlockHeight < r.GetLastBlockHeight() {
		return
	}
	r.eventChan <- accept
}

func (r *Replica) HandleTxn(m message.Transaction) {
	r.startSignal()
	if r.pm.GetCurView() == 0 {
		r.pm.AdvanceView(0)
	}

	if m.TXType == types.ABORT {
		epoch, view := r.pm.GetCurEpochView()
		if !r.IsLeader(r.ID(), view, epoch) {
			r.Send(r.FindLeaderFor(view, epoch), m)
			return
		}
		r.pd.CollectTxn(&m)

	} else {
		r.pd.AddTxn(&m)
	}
}

func (r *Replica) HandleVote(vote quorum.Collection[quorum.Vote]) {
	r.startSignal()
	if r.State() == types.VIEWCHANGING {
		return
	}

	if !r.IsCommittee(r.ID(), vote.Epoch) {
		return
	}

	if vote.BlockHeight < r.GetHighBlockHeight() {
		return
	}

	r.eventChan <- vote
}

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
	}

	r.eventChan <- commitMsg
}

func (r *Replica) HandleTmo(tmo pacemaker.TMO) {
	if tmo.View < r.pm.GetCurView() {
		return
	}

	if !r.IsCommittee(r.ID(), tmo.Epoch) {
		return
	}

	r.eventChan <- tmo
}

func (r *Replica) HandleTc(tc pacemaker.TC) {
	if !(tc.NewView > r.pm.GetCurView()) {
		return
	}
	r.eventChan <- tc
}

func (r *Replica) handleQuery(m message.Query) {
	latency := float64(r.totalDelay.Milliseconds()) / float64(r.latencyNo)
	r.thrus += fmt.Sprintf("Time: %v s. Throughput: %v txs/s\n", time.Since(r.startTime).Seconds(), float64(r.totalCommittedTx)/time.Since(r.tmpTime).Seconds())
	r.totalCommittedTx = 0
	r.tmpTime = time.Now()
	status := fmt.Sprintf("Latency: %v\n%s", latency, r.thrus)
	m.Reply(message.QueryReply{Info: status})
}

// handleRequestLeader replies a Leader of the node (new leader for view change)
func (r *Replica) handleRequestLeader(m message.RequestLeader) {
	r.startSignal()
	if r.pm.GetCurView() == 0 {
		r.pm.AdvanceView(0)
	}

	leader := r.FindLeaderFor(r.pm.GetCurView(), r.pm.GetCurEpoch())
	m.Reply(message.RequestLeaderReply{Leader: fmt.Sprint(leader)})
}

func (r *Replica) processNewView(curEpoch types.Epoch, curView types.View) {

	if !r.IsLeader(r.ID(), curView, curEpoch) {
		return
	}

	r.SetRole(types.LEADER)
	r.eventChan <- types.EpochView{Epoch: curEpoch, View: curView}
}

func (r *Replica) proposeBlock(blockWithGlobalVariable *blockchain.ProposedWorkerBlock) {
	block := blockWithGlobalVariable.WorkerBlock
	log.Debugf("[%v] Propose Worker Block, height: %v, view: %v, epoch: %v", r.ID(), block.BlockHeader.BlockHeight, block.BlockHeader.View, block.BlockHeader.Epoch)
	if !r.IsLeader(r.ID(), r.pm.GetCurView(), r.pm.GetCurEpoch()) {
		return
	}
	r.BroadcastToSome(r.FindCommitteesFor(block.BlockHeader.Epoch), blockWithGlobalVariable)
	_ = r.PBFT.ProcessWorkerBlock(blockWithGlobalVariable)
	r.voteStart = time.Now()
	r.preparingBlock = nil
}

func (r *Replica) processCommittedBlock(block *blockchain.WorkerBlock) {
	r.committedNo++
}

func (r *Replica) processForkedBlock(block *blockchain.WorkerBlock) {}

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

// For Change Test
func (r *Replica) HandleProposeNewView(proposeNewViewMsg message.ProposeNewView) {

	verified, _ := crypto.PubVerify(proposeNewViewMsg.Sign, proposeNewViewMsg.BlockHash[:])
	if !verified {
		log.Errorf("[%v] HandleProposeNewView view: %v msg can not verified", proposeNewViewMsg.CurBlockHeight)
		return
	}

	r.proposeNewViewSignMap[proposeNewViewMsg.CurBlockHeight] = append(r.proposeNewViewSignMap[proposeNewViewMsg.CurBlockHeight], proposeNewViewMsg.Sign)

	r.proposeNewViewQuorumChecker[proposeNewViewMsg.CurBlockHeight]++

	FaultN := (config.Configuration.CommitteeNumber - 1) / 3

	if r.proposeNewViewQuorumChecker[proposeNewViewMsg.CurBlockHeight] >= 2*FaultN+1 {
		// make Accept new view message
		acceptNewViewMsg := message.AcceptNewView{
			CurBlockHeight:          proposeNewViewMsg.CurBlockHeight,
			BlockHash:               proposeNewViewMsg.BlockHash,
			SignBatch:               r.proposeNewViewSignMap[proposeNewViewMsg.CurBlockHeight],
			NextLeaderID:            proposeNewViewMsg.NextLeaderID,
			ProposeNewViewTimeStamp: proposeNewViewMsg.TimeStamp,
		}

		r.BroadcastToSome(r.FindCommitteesFor(r.pm.GetCurEpoch()), acceptNewViewMsg)

		log.Debugf("[%v] Satisfy Condition for View Change, view:  %v", r.ID(), proposeNewViewMsg.CurBlockHeight)
	}

}

func (r *Replica) HandleAcceptNewView(proposeNewViewMsg message.AcceptNewView) {
	if r.acceptNewViewChecker[proposeNewViewMsg.CurBlockHeight] {
		log.Debugf("[%v] HandleAcceptNewView view: %v, already handled", r.ID(), proposeNewViewMsg.CurBlockHeight)
		return
	}

	signBatch := proposeNewViewMsg.SignBatch
	for _, sign := range signBatch {
		verified, _ := crypto.PubVerify(sign, proposeNewViewMsg.BlockHash[:])
		if !verified {
			log.Errorf("[%v] HandleAcceptNewView view: %v msg can not verified", proposeNewViewMsg.CurBlockHeight)
			return
		}
	}

	changeBlockBuilderTime := time.Since(proposeNewViewMsg.ProposeNewViewTimeStamp).Milliseconds()
	log.ChangeTestf("[%v] Change Block Builder Time: %vms", r.ID(), changeBlockBuilderTime)

	// Change Leader
	log.Debugf("[%v] HandleAcceptNewView view: %v, next leader: %v", r.ID(), proposeNewViewMsg.CurBlockHeight, proposeNewViewMsg.NextLeaderID)
	r.ElectLeader(proposeNewViewMsg.NextLeaderID, r.pm.GetCurView())
	if r.ID() == proposeNewViewMsg.NextLeaderID {
		r.SetRole(types.LEADER)
		r.SetState(types.READY)
	} else {
		r.SetRole(types.COMMITTEE)
		r.SetState(types.READY)
	}
	log.Debugf("[%v] Leader: %v Committee: %v", r.ID(), r.FindLeaderFor(r.pm.GetCurView(), r.pm.GetCurEpoch()), r.FindCommitteesFor(r.pm.GetCurEpoch()))

	r.acceptNewViewChecker[proposeNewViewMsg.CurBlockHeight] = true

}

func (r *Replica) HandleReportByzantine(report message.ReportByzantine) {

	verified, _ := crypto.PubVerify(report.Sign, crypto.IDToByte(crypto.MakeID(report.ByzantineNodeID))[:])
	if !verified {
		log.Errorf("[%v] HandleReportByzantine view: %v msg can not verified", report.CurBlockHeight)
		return
	}

	r.reportByzantineSignMap[report.CurBlockHeight] = append(r.reportByzantineSignMap[report.CurBlockHeight], report.Sign)

	r.reportByzantineQuorumChecker[report.CurBlockHeight]++

	FaultN := (config.Configuration.CommitteeNumber - 1) / 3

	if r.reportByzantineQuorumChecker[report.CurBlockHeight] >= 2*FaultN+1 {

		// make Accept new view message
		replaceByzantineMsg := message.ReplaceByzantine{
			CurBlockHeight:           report.CurBlockHeight,
			BlockHash:                report.BlockHash,
			SignBatch:                r.reportByzantineSignMap[report.CurBlockHeight],
			ReplaceByzantineNodeID:   report.ByzantineNodeID,
			ReportByzantineTimeStamp: report.TimeStamp,
		}

		r.BroadcastToSome(r.FindCommitteesFor(r.pm.GetCurEpoch()), replaceByzantineMsg)

		log.Debugf("[%v] Satisfy Condition for Replace Byzantine, view:  %v", r.ID(), replaceByzantineMsg.CurBlockHeight)
	}
}

func (r *Replica) HandleReplaceByzantine(replace message.ReplaceByzantine) {
	if r.replaceByzantineChecker[replace.CurBlockHeight] {
		log.Debugf("[%v] HandleReplaceByzantine view: %v, already handled", r.ID(), replace.CurBlockHeight)
		return
	}

	signBatch := replace.SignBatch
	for _, sign := range signBatch {
		verified, _ := crypto.PubVerify(sign, crypto.IDToByte(crypto.MakeID(replace.ReplaceByzantineNodeID))[:])
		if !verified {
			log.Errorf("[%v] HandleReplaceByzantine view: %v msg can not verified", replace.CurBlockHeight)
			return
		}
	}

	changeCommitteeTime := time.Since(replace.ReportByzantineTimeStamp).Milliseconds()
	log.ChangeTestf("[%v] Change Committee Time: %vms", r.ID(), changeCommitteeTime)

	// Change Committee
	nextCommitteeID := r.FindValidatorsFor(r.pm.GetCurEpoch())[3]
	log.Debugf("[%v] HandleReplaceByzantine view: %v, byzantineId: %v nextCommitteeId: %v", r.ID(), replace.CurBlockHeight, replace.ReplaceByzantineNodeID, nextCommitteeID)
	r.ReplaceCommittee(r.pm.GetCurEpoch(), replace.ReplaceByzantineNodeID, nextCommitteeID)
	r.ReplaceValidators(r.pm.GetCurEpoch(), nextCommitteeID, replace.ReplaceByzantineNodeID)
	r.SetState(types.READY)

	log.Errorf("[%v] Leader: %v, Committee: %v, Validator: %v", r.ID(), r.FindLeaderFor(r.pm.GetCurView(), r.pm.GetCurEpoch()), r.FindCommitteesFor(r.pm.GetCurEpoch()), r.FindValidatorsFor(r.pm.GetCurEpoch()))

	r.replaceByzantineChecker[replace.CurBlockHeight] = true

}

func (r *Replica) HandleProposeNewCommittee(proposeNewCommitteeMsg message.ProposeNewCommittee) {

	verified, _ := crypto.PubVerify(proposeNewCommitteeMsg.Sign, proposeNewCommitteeMsg.BlockHash[:])
	if !verified {
		log.Errorf("[%v] HandleProposeNewCommittee view: %v msg can not verified", proposeNewCommitteeMsg.CurBlockHeight)
		return
	}

	r.proposeNewCommitteeSignMap[proposeNewCommitteeMsg.CurBlockHeight] = append(r.proposeNewCommitteeSignMap[proposeNewCommitteeMsg.CurBlockHeight], proposeNewCommitteeMsg.Sign)

	r.proposeNewCommitteeQuorumChecker[proposeNewCommitteeMsg.CurBlockHeight]++

	FaultN := (config.Configuration.CommitteeNumber - 1) / 3

	if r.proposeNewCommitteeQuorumChecker[proposeNewCommitteeMsg.CurBlockHeight] >= 2*FaultN+1 {

		// make Accept new view message
		changeCommitteeMsg := message.ChangeCommittee{
			CurBlockHeight:           proposeNewCommitteeMsg.CurBlockHeight,
			BlockHash:                proposeNewCommitteeMsg.BlockHash,
			SignBatch:                r.proposeNewViewSignMap[proposeNewCommitteeMsg.CurBlockHeight],
			ChangeCommitteeTimeStamp: proposeNewCommitteeMsg.TimeStamp,
		}

		r.BroadcastToSome(r.FindCommitteesFor(r.pm.GetCurEpoch()), changeCommitteeMsg)
		r.BroadcastToSome(r.FindValidatorsFor(r.pm.GetCurEpoch()), changeCommitteeMsg)

		log.Debugf("[%v] Satisfy Condition for Change Committee, view:  %v", r.ID(), changeCommitteeMsg.CurBlockHeight)
	}

}

func (r *Replica) HandleChangeCommittee(changeCommitteeMsg message.ChangeCommittee) {
	if r.ChangeCommitteeChecker[changeCommitteeMsg.CurBlockHeight] {
		log.Debugf("[%v] HandleChangeCommittee view: %v, already handled", r.ID(), changeCommitteeMsg.CurBlockHeight)
		return
	}

	signBatch := changeCommitteeMsg.SignBatch
	for _, sign := range signBatch {
		verified, _ := crypto.PubVerify(sign, changeCommitteeMsg.BlockHash[:])
		if !verified {
			log.Errorf("[%v] HandleChangeCommittee view: %v msg can not verified", changeCommitteeMsg.CurBlockHeight)
			return
		}
	}

	changeBlockBuilderTime := time.Since(changeCommitteeMsg.ChangeCommitteeTimeStamp).Milliseconds()
	log.ChangeTestf("[%v] Change Committee Time: %vms", r.ID(), changeBlockBuilderTime)

	r.ChangeCommitteeChecker[changeCommitteeMsg.CurBlockHeight] = true
	r.SetState(types.ALLCOMMITEEECHANGED)
}

func (r *Replica) ListenLocalEvent() {
	r.lastViewTime = time.Now()
	r.timer = time.NewTimer(r.pm.GetTimerForView())
	for {
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
				now := time.Now()
				lasts := now.Sub(r.lastViewTime)
				r.totalRoundTime += lasts
				r.roundNo++
				r.lastViewTime = now
				if r.IsCommittee(r.ID(), epoch) {
					r.SetRole(types.COMMITTEE)
				} else {
					r.SetRole(types.VALIDATOR)
				}
				r.processNewView(epoch, view)
				break L
			case <-r.pm.EnteringTmoEvent():
				r.PBFT.ProcessLocalTmo(r.pm.GetCurView())
				break L
			case <-r.timer.C:
				if r.State() == types.VIEWCHANGING {
					r.pm.UpdateNewView(r.pm.GetNewView() + 1)
				}
				r.PBFT.ProcessLocalTmo(r.pm.GetCurView())

				break L
			}
		}
	}
}

func (r *Replica) startSignal() {
	if !r.isStarted.Load() {
		r.startTime = time.Now()
		r.tmpTime = time.Now()
		r.isStarted.Store(true)
		r.start <- true
	}
}

// Start event loop
func (r *Replica) Start() {
	log.Errorf("[%v] Start", r.ID())
	r.Node.DistLogger().BootingSequence(1)
	go r.Run()
	go r.ProposeWorkerBlock()
	r.ElectCommittees(crypto.MakeID(0), 1)
	r.ElectValidators(crypto.MakeID(0), 1)
	if r.IsCommittee(r.ID(), 1) {
		r.SetRole(types.COMMITTEE)
		log.Errorf("[%v] is Committee", r.ID())
	}
	if r.IsValidator(r.ID(), 1) {
		r.SetRole(types.VALIDATOR)
		log.Errorf("[%v] is Validator", r.ID())
	}

	log.Errorf("[%v] Leader: %v, Committee: %v, Validator: %v", r.ID(), r.FindLeaderFor(r.pm.GetCurView(), r.pm.GetCurEpoch()), r.FindCommitteesFor(r.pm.GetCurEpoch()), r.FindValidatorsFor(r.pm.GetCurEpoch()))

	// wait for the start signal
	<-r.start
	r.Node.DistLogger().BootingSequence(2)
	go r.ListenLocalEvent()
	go r.ListenCommittedBlocks()
	for {
		for r.isStarted.Load() {
			if r.PBFT.GetLastBlockHeight() > types.BlockHeight(config.GetConfig().Epoch)+r.PBFT.GetStartBlockHeight() {
				r.isStarted.Store(false)
				break
			}
			event := <-r.eventChan
			switch v := event.(type) {
			case types.EpochView:
			case blockchain.ProposedWorkerBlock:
				_ = r.PBFT.ProcessWorkerBlock(&v)
				r.voteStart = time.Now()
				r.processedNo++
			case quorum.Collection[quorum.Vote]:
				startProcessTime := time.Now()
				r.PBFT.ProcessVote(&v)
				processingDuration := time.Since(startProcessTime)
				r.totalVoteTime += processingDuration
				r.voteNo++
			case quorum.Collection[quorum.Commit]:
				r.PBFT.ProcessCommit(&v)
			case blockchain.Accept:
				r.PBFT.ProcessAccept(&v)
			case pacemaker.TMO:
				r.PBFT.ProcessRemoteTmo(&v)
			case pacemaker.TC:
				r.PBFT.ProcessTC(&v)
			}
		}
	}
}
