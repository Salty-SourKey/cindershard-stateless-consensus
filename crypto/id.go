package crypto

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
)

type Encoder struct{}

func NewEncoder() *Encoder {
	return &Encoder{}
}

func (e *Encoder) Encode(val interface{}) ([]byte, error) {
	ret, err := json.Marshal(val)

	return ret, fmt.Errorf("json error: %w", err)
}

func (e *Encoder) Decode(b []byte, val interface{}) error {

	return fmt.Errorf("json error: %w", json.Unmarshal(b, val))
}

func (e *Encoder) MustEncode(val interface{}) []byte {
	b, err := e.Encode(val)
	if errors.Unwrap(err) != nil {
		panic(err)
	}

	return b
}

func (e *Encoder) MustDecode(b []byte, val interface{}) {
	err := e.Decode(b, val)
	if err != nil {
		panic(err)
	}
}

// DefaultEncoder is the default encoder used by Flow.
var DefaultEncoder Encoder = *NewEncoder()

// MakeID creates an ID from the hash of encoded data.
func MakeID(body interface{}) common.Hash {
	data := DefaultEncoder.MustEncode(body)
	hasher := NewSHA3_256()
	hash := hasher.ComputeHash(data)
	return HashToID(hash)
}

func HashToID(hash []byte) common.Hash {
	var id common.Hash
	copy(id[:], hash)
	return id
}

func IDToByte(id common.Hash) []byte {
	return id[:]
}
