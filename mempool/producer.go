package mempool

import (
	"unishard/message"
)

type Producer struct {
	mempool *MemPool
}

func NewProducer() *Producer {
	return &Producer{
		mempool: NewMemPool(),
	}
}

func (pd *Producer) GeneratePayload(num int) []*message.Transaction {
	return pd.mempool.some(num)
}


func (pd *Producer) AddTxn(txn *message.Transaction) {
	pd.mempool.addNew(txn)
}

func (pd *Producer) CollectTxn(txn *message.Transaction) {
	pd.mempool.addOld(txn)
}

func (pd *Producer) TotalReceivedTxNo() int64 {
	return pd.mempool.totalReceived
}

func (pd *Producer) Size() int {
	return pd.mempool.size()
}
