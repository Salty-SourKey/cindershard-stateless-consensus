package types

type NodeID string
type IDs []NodeID

type Shard uint
type View uint
type BlockHeight uint
type Epoch uint

type EpochView struct {
	Epoch
	View
}

type TransactionType byte

const (
	TRANSFER TransactionType = 0 + iota
	SMARTCONTRACT
	DEPLOY
	ABORT
)

type TransactionForm byte

const (
	NORMALTRANSFER TransactionForm = 0 + iota
	NORMALSMARTCONTRACT
	CROSSSHARDTRANSFER
	CROSSSHARDSMARTCONTRACT
)

type TransactionStatus byte

const (
	PENDING TransactionStatus = 0 + iota
	CONFIRMED
	REJECTED
)

type NodeState byte

const (
	READY NodeState = 0 + iota
	PREPARED
	VIEWCHANGING
	LOCKED
	SPECULATIVEEXECUTION
	FILLHOLE
	FINISHFILLHOLE
	COMMIT
	CONFIRMREQ
	LEADERCHANGING
	COMMITTEECHANGING
	ALLCOMMITTEEECHANGING
	ALLCOMMITEEECHANGED
	PROCESSING
)

type NodeRole byte

const (
	LEADER NodeRole = 0 + iota
	CANDIDATE
	COMMITTEE
	VALIDATOR
)

type QcType byte

const (
	QC QcType = 0 + iota
	CQC
)

type RwData struct {
	ExternalRweSet   []ExternalElem `json:"externalRweSet"`
	Function         string         `json:"function"`
	FunctionSelector string         `json:"functionSelector"`
	ReadSet          []string       `json:"readSet"`
	WriteSet         []string       `json:"writeSet"`
}

type RwSet struct {
	ReadSet     []string
	WriteSet    []string
	ExternalSet []ExternalElem
}

type ExternalElem struct {
	Name     string `json:"name"`
	ElemType string `json:"type"`
}
