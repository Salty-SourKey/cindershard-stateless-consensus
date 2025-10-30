package socket

import (
	"math/rand"
	"strconv"
	"sync"
	"time"

	"unishard/config"
	"unishard/log"
	"unishard/transport"
	"unishard/types"
	"unishard/utils"
)

// Socket integrates all networking interface and fault injections
type Socket interface {
	GetBlockBuilder() transport.Transport

	GetAddresses() map[types.Shard]map[types.NodeID]string

	// Send to BlockBuilder any interface message
	SendToBlockBuilder(m interface{})

	// Send put message to outbound queue
	Send(to types.NodeID, m interface{})

	// MulticastQuorum sends msg to random number of nodes
	MulticastQuorum(quorum int, m interface{})

	// Broadcast send to all peers
	Broadcast(m interface{})

	// Broadcast send to some peers
	BroadcastToSome(some []types.NodeID, m interface{})

	// Recv receives a message
	Recv() interface{}

	Close()

	// Fault injection
	Drop(id types.NodeID, t int)             // drops every message send to NodeID last for t seconds
	Slow(id types.NodeID, d int, t int)      // delays every message send to NodeID for d ms and last for t seconds
	Flaky(id types.NodeID, p float64, t int) // drop message by chance p for t seconds
	Crash(t int)                             // node crash for t seconds
}

type socket struct {
	id           types.NodeID
	shard        types.Shard
	blockbuilder transport.Transport
	addresses    map[types.Shard]map[types.NodeID]string
	nodes        map[types.NodeID]transport.Transport

	crash bool
	drop  map[types.NodeID]bool
	slow  map[types.NodeID]int
	flaky map[types.NodeID]float64

	lock sync.RWMutex // locking map nodes
}

// NewSocket return Socket interface instance given self NodeID, node list, transport and codec name
func NewSocket(id types.NodeID, addrs map[types.Shard]map[types.NodeID]string, shard types.Shard) Socket {
	tmpaddrs := make(map[types.Shard]map[types.NodeID]string)
	tmpaddrs[shard] = make(map[types.NodeID]string)
	for nodeshard, address := range addrs {
		if nodeshard == shard {
			for id, addr := range address {
				port := strconv.Itoa(3999 + int(shard)*100 + utils.Node(id))
				addr = addr + port
				tmpaddrs[shard][id] = addr
			}
		}
	}

	socket := &socket{
		id:           id,
		shard:        shard,
		blockbuilder: transport.NewTransport(tmpaddrs[shard][utils.NewNodeID(0)]),
		addresses:    tmpaddrs,
		nodes:        make(map[types.NodeID]transport.Transport),
		crash:        false,
		drop:         make(map[types.NodeID]bool),
		slow:         make(map[types.NodeID]int),
		flaky:        make(map[types.NodeID]float64),
	}

	socket.nodes[id] = transport.NewTransport(socket.addresses[shard][id])
	socket.nodes[id].Listen()

	return socket
}

func (s *socket) GetBlockBuilder() transport.Transport {
	return s.blockbuilder
}

func (s *socket) GetAddresses() map[types.Shard]map[types.NodeID]string {
	return s.addresses
}

func (s *socket) SendToBlockBuilder(m interface{}) {
	to := utils.NewNodeID(0)
	if s.crash {
		return
	}

	if s.drop[to] {
		return
	}

	if p, ok := s.flaky[to]; ok && p > 0 {
		if rand.Float64() < p {
			return
		}
	}

	if delay, ok := s.slow[to]; ok && delay > 0 {
		timer := time.NewTimer(time.Duration(delay) * time.Millisecond)
		go func() {
			<-timer.C
			s.blockbuilder.Send(m)
		}()

		return
	}

	s.blockbuilder.Send(m)
}

func (s *socket) Send(to types.NodeID, m interface{}) {
	//log.Debugf("node %s send message %+v to %v", s.id, m, to)

	if s.crash {
		return
	}

	if s.drop[to] {
		return
	}

	if p, ok := s.flaky[to]; ok && p > 0 {
		if rand.Float64() < p {
			return
		}
	}

	s.lock.RLock()
	t, exists := s.nodes[to]
	s.lock.RUnlock()
	if !exists {
		s.lock.RLock()
		// shard := config.Configuration.GetShardNumOfId(to)
		address, ok := s.addresses[s.shard][to]
		s.lock.RUnlock()
		if !ok {
			log.Errorf("socket does not have address of node %s", to)
			return
		}
		t = transport.NewTransport(address)
		err := utils.Retry(t.Dial, 100, time.Duration(50)*time.Millisecond)
		if err != nil {
			panic(err)
		}
		s.lock.Lock()
		s.nodes[to] = t
		s.lock.Unlock()
	}

	if delay, ok := s.slow[to]; ok && delay > 0 {
		timer := time.NewTimer(time.Duration(delay) * time.Millisecond)
		go func() {
			<-timer.C
			t.Send(m)
		}()
		return
	}

	t.Send(m)
	//log.Debugf("[%v] message %v is sent to %v", s.id, m, to)
}

func (s *socket) Recv() interface{} {
	s.lock.RLock()
	t := s.nodes[s.id]
	s.lock.RUnlock()
	for {
		m := t.Recv()
		if !s.crash {
			return m
		}
	}
}

func (s *socket) MulticastQuorum(quorum int, m interface{}) {
	//log.Debugf("node %s multicasting message %+v for %d nodes", s.id, m, quorum)
	sent := map[int]struct{}{}
	for i := 0; i < quorum; i++ {
		r := rand.Intn(len(s.addresses)) + 1
		_, exists := sent[r]
		if exists {
			continue
		}
		s.Send(utils.NewNodeID(r), m)
		sent[r] = struct{}{}
	}
}

func (s *socket) Broadcast(m interface{}) {
	latencytimer := time.NewTimer(time.Millisecond * time.Duration(config.GetConfig().InShardLatency))

	<-latencytimer.C

	//log.Debugf("node %s broadcasting message %+v", s.id, m)
	for id := range s.addresses[s.shard] {
		if id == s.id {
			continue
		}
		s.Send(id, m)
	}
	//log.Debugf("node %s done  broadcasting message %+v", s.id, m)
}

func (s *socket) BroadcastToSome(some []types.NodeID, m interface{}) {
	// log.Errorf("BroadcastToSome %v", some)
	for _, id := range some {
		if id == s.id {
			continue
		}

		if _, exist := s.addresses[s.shard][id]; !exist {
			continue
		}
		// random delay among [100, 80, 200, 170, 300]
		delay := []int{100, 80, 200, 170, 300}
		rand.Seed(time.Now().UnixNano())
		randomDelay := delay[rand.Intn(len(delay))]
		timer := time.NewTimer(time.Duration(randomDelay) * time.Millisecond)
		go func(id types.NodeID) {
			<-timer.C
			s.Send(id, m)
		}(id)
	}
	// log.Errorf("node %s done  broadcasting message %+v", s.id, m)
}

func (s *socket) Close() {
	for _, t := range s.nodes {
		t.Close()
	}
}

func (s *socket) Drop(id types.NodeID, t int) {
	s.drop[id] = true
	timer := time.NewTimer(time.Duration(t) * time.Second)
	go func() {
		<-timer.C
		s.drop[id] = false
	}()
}

func (s *socket) Slow(id types.NodeID, delay int, t int) {
	s.slow[id] = delay
	timer := time.NewTimer(time.Duration(t) * time.Second)
	go func() {
		<-timer.C
		s.slow[id] = 0
	}()
}

func (s *socket) Flaky(id types.NodeID, p float64, t int) {
	s.flaky[id] = p
	timer := time.NewTimer(time.Duration(t) * time.Second)
	go func() {
		<-timer.C
		s.flaky[id] = 0
	}()
}

func (s *socket) Crash(t int) {
	s.crash = true
	if t > 0 {
		timer := time.NewTimer(time.Duration(t) * time.Second)
		go func() {
			<-timer.C
			s.crash = false
		}()
	}
}
