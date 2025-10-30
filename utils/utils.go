package utils

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"math/big"
	mathRand "math/rand"
	"reflect"
	"strconv"
	"time"

	"unishard/log"
	"unishard/types"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// NewNodeID returns a new NodeID type given two int number of zone and node.
func NewNodeID(node int) types.NodeID {
	if node < 0 {
		node = -node
	}

	return types.NodeID(strconv.Itoa(node))
}

// Node returns Node NodeID component
func Node(i types.NodeID) int {
	s := string(i)
	node, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		log.Errorf("Failed to convert Node %s to int\n", s)

		return 0
	}

	return int(node)
}

func Len(a types.IDs) int { return len(a) }

func SlotToKey(slot uint64) common.Hash {
	key := common.BigToHash(big.NewInt(int64(slot)))
	return key
}

func RemoveSliceIndex[T any](index int, slice []T) []T {
	subSlice := []T{}
	for idx, value := range slice {
		if index == idx {
			continue
		}
		subSlice = append(subSlice, value)
	}
	return subSlice
}

func CalculateShardToSend(addressList []common.Address, shardNum int) []types.Shard {
	shardToSend := []types.Shard{}
	for _, address := range addressList {
		a := new(big.Int)
		a.SetString(address.Hex()[2:], 16)
		shard := types.Shard(a.Mod(a, big.NewInt(int64(shardNum))).Int64() + 1)
		if !Contains(shardToSend, shard) {
			shardToSend = append(shardToSend, shard)
		}
	}

	return shardToSend
}

func CalculateMappingSlotIndex(address common.Address, slot uint64) []byte {
	// left pad address with 0 to 32 bytes
	addressBytes := common.LeftPadBytes(address.Bytes(), 32)

	// left pad slot with 0 to 32 bytes
	slotBytes := make([]byte, 32)
	binary.BigEndian.PutUint64(slotBytes[24:], slot)

	return crypto.Keccak256(addressBytes, slotBytes)
}

func FindIntSlice(slice []int, val int) bool {
	for _, item := range slice {
		if item == val {
			return true
		}
	}
	return false
}

func RandomPick(n int, f int) []int {
	var randomPick []int
	for i := 0; i < f; i++ {
		var randomID int
		exists := true
		for exists {
			s := mathRand.NewSource(time.Now().UnixNano())
			r := mathRand.New(s)
			randomID = r.Intn(n)
			exists = FindIntSlice(randomPick, randomID)
		}
		randomPick = append(randomPick, randomID)
	}
	return randomPick
}

// Max of two int
func Max(a, b int) int {
	if a < b {
		return b
	}
	return a
}

// VMax of a vector
func VMax(v ...int) int {
	max := v[0]
	for _, i := range v {
		if max < i {
			max = i
		}
	}
	return max
}

// Retry function f sleep time between attempts
func Retry(f func() error, attempts int, sleep time.Duration) error {
	var err error
	for i := 0; ; i++ {
		err = f()
		if err == nil {
			return nil
		}

		if i >= attempts-1 {
			break
		}

		// exponential delay
		time.Sleep(sleep * time.Duration(i+1))
	}
	return fmt.Errorf("after %d attempts, last error: %w", attempts, err)
}

// Schedule repeatedly call function with intervals
func Schedule(f func(), delay time.Duration) chan bool {
	stop := make(chan bool)

	go func() {
		for {
			f()
			select {
			case <-time.After(delay):
			case <-stop:
				return
			}
		}
	}()

	return stop
}

func MapRandomKeyGet(mapI interface{}) interface{} {
	keys := reflect.ValueOf(mapI).MapKeys()

	return keys[mathRand.Intn(len(keys))].Interface()
}

func IdentifierFixture() common.Hash {
	var id common.Hash
	_, _ = rand.Read(id[:])
	return id
}

func Map[T, V any](ts []T, fn func(T) V) []V {
	result := make([]V, len(ts))
	for i, t := range ts {
		result[i] = fn(t)
	}
	return result
}

func AddEntity[T any](hashMap map[types.Epoch]map[types.View]map[types.BlockHeight]T, epoch types.Epoch, view types.View, blockHeight types.BlockHeight, v T) {
	if _, ok := hashMap[epoch]; !ok {
		hashMap[epoch] = make(map[types.View]map[types.BlockHeight]T)
		hashMap[epoch][view] = make(map[types.BlockHeight]T)
	} else if _, ok := hashMap[epoch][view]; !ok {
		hashMap[epoch][view] = make(map[types.BlockHeight]T)
	}

	hashMap[epoch][view][blockHeight] = v
}

func Contains(slice interface{}, target interface{}) bool {
	s := reflect.ValueOf(slice)
	for i := 0; i < s.Len(); i++ {
		if reflect.DeepEqual(s.Index(i).Interface(), target) {
			return true
		}
	}
	return false
}

func ContainsHash(commonHash []common.Hash, target common.Hash) bool {
	for _, item := range commonHash {
		if item == target {
			return true
		}
	}
	return false
}
