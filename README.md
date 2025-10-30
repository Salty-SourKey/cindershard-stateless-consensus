## What is CinderShard?

**CinderShard** is a Byzantine-resilient sharding architecture that simultaneously achieves robustness against single-shard attacks and high scalability by decoupling the two fundamental properties of consensus—safety and liveness—and addressing each independently.

CinderShard adopts Safety-Aware Quorum Amplification, which increases the quorum threshold of intra-shard consensus to probabilistically guarantee safety even when the Byzantine ratio of a shard exceeds the classical one-third bound. Since expanding the quorum size can adversely affect liveness, CinderShard dynamically performs Liveness-Driven Dynamic Reconfiguration, a process that reconfigures the node set through random assignment when a liveness violation is detected at runtime.

To realize CinderShard as a fully functional sharded blockchain, we integrated the Ethereum Virtual Machine (EVM) and Merkle Patricia Trie (MPT) from [Geth](https://github.com/ethereum/go-ethereum) into CinderShard-PBFT, and implemented Practical Byzantine Fault Tolerance (PBFT) as the intra-shard consensus algorithm.

## What is Stateless Consensus?
In CinderShard, a node maintains the root hash of every shard's MPT to participate in a shard's consensus even without locally storing that shard's full ledger or state. This stateless consensus enables a lightweight shard reconfiguration mechanism that eliminates data migration overhead.

The process of stateless consensus is as follows:

1. The leader proposes a block with a list of transactions, its read/write set, and corresponding Merkle proofs.
```go
// GetProofDB returns a Merkle proof database for a set of addresses.
func (s *StateDB) GetProofDB(rwSetAddress []common.Address) ([][]byte, [][]byte, ethdb.KeyValueReader, error) {
	var keys [][]byte
	var values [][]byte
	proofDB := rawdb.NewMemoryDatabase()
	for _, addr := range rwSetAddress {
		addrHash := crypto.Keccak256Hash(addr.Bytes())
		err := s.trie.Prove(addrHash.Bytes(), proofDB)
		if err != nil {
			log.Error("Failed to get proof for address", "address", addr, "error", err)
			return nil, nil, nil, err
		}

		if !utils.Contains(keys, addrHash.Bytes()) {
			keys = append(keys, addrHash.Bytes())
			account, _ := s.trie.GetAccount(addr)
			value, _ := rlp.EncodeToBytes(account)
			values = append(values, value)
		}
	}

	return keys, values, proofDB, nil
}
```
2. After receiving the proposed block, a node verifies the root hash of the block with the locally stored one.
```go
if block.BlockHeader.StateRoot != pb.bc.GetStateRoot() {
    log.Errorf("(ProcessWorkerBlock) stateHash not valid")
    return nil
}
```
3. A node constructs a partial MPT that includes only the state variables required to execute the transaction list inside the block.
```go
func (bc *BlockChain) SetPartialState(keys, values, merkleProofKeys, merkleProofValues [][]byte) bool {
	triedbConfig := &triedb.Config{
		Preimages: true,
		HashDB: &hashdb.Config{
			CleanCacheSize: 256 * 1024 * 1024,
		},
	}
	proofDB := rawdb.NewMemoryDatabase()
	for i, key := range merkleProofKeys {
		proofDB.Put(key, merkleProofValues[i])
	}

	for i, key := range keys {
		proofDB.Put(key, values[i])
	}

	db := rawdb.NewMemoryDatabase()
	tdb := triedb.NewDatabase(proofDB, triedbConfig)

	bc.nodedb = state.NewDatabaseWithNodeDB(db, tdb)
	bc.statedb, _ = state.New(bc.stateRoot, bc.nodedb, nil)
}
```
4. A node executes the transactions using the partial MPT and updates the resulting root hash for a subsequent consensus.

## How to build

1. Install [Go](https://golang.org/dl/).

2. Download CinderShard source code.

3. Build `main` and `client`.
```
cd cindershard-stateless-consensus/bin
go build ../main
go build ../client
```

# How to run

1. ```cd cindershard-stateless-consensus/bin```.
2. Modify `ips.txt` with a set of IPs of each node. The number of IPs equals to the number of nodes. Here, the local IP is `127.0.0.1`.
3. Modify configuration parameters in `config.json`.
4. Run `deploy` and then run `client` using scripts.
```
bash deploy.sh
```
```
bash runClient.sh
```
5. Stop the client and the main.
```
bash stop.sh
```
