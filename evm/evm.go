package evm

import (
	"fmt"
	"math"
	"math/big"
	"time"

	"unishard/evm/core"
	"unishard/evm/state"
	types "unishard/evm/types"
	"unishard/evm/vm"
	"unishard/evm/vm/runtime"
	unitype "unishard/types"

	"github.com/holiman/uint256"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
)

const SHANGHAI_BLOCK_COUNT unitype.BlockHeight = 16830000

// Todo: Change to leveldb using disk file system
func NewPersistantState() (*state.StateDB, error) {
	return state.New(types.EmptyRootHash, state.NewDatabase(rawdb.NewMemoryDatabase()), nil)
}

func NewMemoryState() (*state.StateDB, error) {
	return state.New(types.EmptyRootHash, state.NewDatabase(rawdb.NewMemoryDatabase()), nil)
}

func Transfer(statedb *state.StateDB, sender common.Address, recipient common.Address, value *uint256.Int) error {
	if core.CanTransfer(statedb, sender, value) {
		core.Transfer(statedb, sender, recipient, value)
		statedb.SetNonce(sender, statedb.GetNonce(sender)+1)
		return nil
	} else {
		return fmt.Errorf("not enough user balance")
	}
}

func SetConfig(blockheight unitype.BlockHeight, random_value string) *runtime.Config {
	cfg := new(runtime.Config)
	if cfg.Difficulty == nil {
		cfg.Difficulty = big.NewInt(0)
	}
	if cfg.GasLimit == 0 {
		cfg.GasLimit = math.MaxUint64
	}
	if cfg.GasPrice == nil {
		cfg.GasPrice = new(big.Int)
	}
	if cfg.Value == nil {
		cfg.Value = new(big.Int)
	}
	if cfg.GetHashFn == nil {
		cfg.GetHashFn = func(n uint64) common.Hash {
			return common.BytesToHash(crypto.Keccak256([]byte(new(big.Int).SetUint64(n).String())))
		}
	}
	if cfg.BaseFee == nil {
		cfg.BaseFee = big.NewInt(params.InitialBaseFee)
	}
	if cfg.BlobBaseFee == nil {
		cfg.BlobBaseFee = big.NewInt(params.BlobTxMinBlobGasprice)
	}

	cfg.ChainConfig = params.SepoliaChainConfig

	// Decide evm version
	// EVM version is must more than shanghai because recent solidity compile has PUSH0 opcode
	// cfg.BlockNumber = big.NewInt(0)
	cfg.BlockNumber = big.NewInt(int64(SHANGHAI_BLOCK_COUNT) + int64(blockheight))

	cfg.Time = uint64(time.Now().Unix())
	random := common.BytesToHash([]byte(random_value))
	cfg.Random = &random

	return cfg
}

func Deploy(evm *vm.EVM, statedb *state.StateDB, bytecode []byte, deployer common.Address) (uint64, common.Address, error) {
	creater := vm.AccountRef(deployer)
	_, address, leftOverGas, err := evm.Create(
		creater,
		bytecode,
		statedb.GetBalance(deployer).Uint64()/1e10,
		uint256.NewInt(0),
	)
	if err != nil {
		return 0, common.Address{0}, err
	}

	return leftOverGas, address, nil
}

func Execute(evm *vm.EVM, statedb *state.StateDB, input []byte, caller common.Address, contract_address common.Address) (uint64, []byte, error) {
	sender := vm.AccountRef(caller)
	// fmt.Printf("TestAddress Balance: %v %v\n", caller, statedb.GetBalance(caller))
	result, leftOverGas, err := evm.Call(
		sender,
		contract_address,
		input,
		statedb.GetBalance(caller).Uint64(),
		uint256.NewInt(0),
	)
	if err != nil {
		return 0, []byte{}, err
	}

	return leftOverGas, result, nil
}

func Deposit(evm *vm.EVM, statedb *state.StateDB, input []byte, caller common.Address, contract_address common.Address) (uint64, []byte, error) {
	sender := vm.AccountRef(caller)
	result, leftOverGas, err := evm.Call(
		sender,
		contract_address,
		input,
		statedb.GetBalance(caller).Uint64(),
		uint256.NewInt(20000000),
	)
	if err != nil {
		return 0, []byte{}, err
	}

	return leftOverGas, result, nil
}
