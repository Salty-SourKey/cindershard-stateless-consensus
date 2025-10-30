package crypto

import (
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/pem"
	"unishard/types"

	"github.com/ethereum/go-ethereum/common"
)

// SigningAlgorithm is an common.Hash for a signing algorithm and curve.

//type SigningAlgorithm string

// String returns the string representation of this signing algorithm.
// func (f SigningAlgorithm) String() string {
//	return [...]string{"UNKNOWN", "BLS_BLS12381", "ECDSA_P256", "ECDSA_SECp256k1"}[f]
//}

const (
	// Supported signing algorithms
	//UnknownSigningAlgorithm SigningAlgorithm = iota
	BLS_BLS12381    = "BLS_BLS12381"
	ECDSA_P256      = "ECDSA_P256"
	ECDSA_SECp256k1 = "ECDSA_SECp256k1"
)

var key *ecdsa.PrivateKey
var pubKey *ecdsa.PublicKey

var builderkeys []PrivateKey
var builderpubKeys []PublicKey

var client_keys []PrivateKey
var client_pubKeys []PublicKey

// PrivateKey is an unspecified signature scheme private key
type PrivateKey interface {
	// Algorithm returns the signing algorithm related to the private key.
	Algorithm() string
	// KeySize return the key size in bytes.
	// KeySize() int
	// Sign generates a signature using the provided hasher.
	Sign([]byte, Hasher) (Signature, error)
	// Encode returns a bytes representation of the private key
	//Encode() ([]byte, error)
	PrivateKey() *ecdsa.PrivateKey
}

// PublicKey is an unspecified signature scheme public key.
type PublicKey interface {
	// Algorithm returns the signing algorithm related to the public key.
	Algorithm() string
	// KeySize return the key size in bytes.
	//KeySize() int
	// Verify verifies a signature of an input message using the provided hasher.
	Verify(Signature, Hash) (bool, error)
	// Encode returns a bytes representation of the public key.
	Encode() ([]byte, error)
	PublicKey() *ecdsa.PublicKey
}

func encode(privateKey *ecdsa.PrivateKey, publicKey *ecdsa.PublicKey) (string, string) {
	x509Encoded, _ := x509.MarshalECPrivateKey(privateKey)
	pemEncoded := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: x509Encoded})

	x509EncodedPub, _ := x509.MarshalPKIXPublicKey(publicKey)
	pemEncodedPub := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: x509EncodedPub})

	return string(pemEncoded), string(pemEncodedPub)
}

func decode(pemEncoded string, pemEncodedPub string) (*ecdsa.PrivateKey, *ecdsa.PublicKey) {
	block, _ := pem.Decode([]byte(pemEncoded))
	x509Encoded := block.Bytes
	privateKey, _ := x509.ParseECPrivateKey(x509Encoded)

	blockPub, _ := pem.Decode([]byte(pemEncodedPub))
	x509EncodedPub := blockPub.Bytes
	genericPublicKey, _ := x509.ParsePKIXPublicKey(x509EncodedPub)
	publicKey := genericPublicKey.(*ecdsa.PublicKey)

	return privateKey, publicKey
}

func SetKey() error {
	encPriv := "-----BEGIN PRIVATE KEY-----\nMHcCAQEEIKcYceu+9YTtZg4r6t4ZTVr4BRmvNC1u7Ob3gVKeHtg8oAoGCCqGSM49\nAwEHoUQDQgAEC+bxKe6s3B+c9GEltZC9Z/z3Eq0DuJKxeNMC1FtloYwc/UqFKgrJ\np/LUfqF5ozXq3x3llorrsl6xisopaSg44g==\n-----END PRIVATE KEY-----"
	encPub := "-----BEGIN PUBLIC KEY-----\nMFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAEC+bxKe6s3B+c9GEltZC9Z/z3Eq0D\nuJKxeNMC1FtloYwc/UqFKgrJp/LUfqF5ozXq3x3llorrsl6xisopaSg44g==\n-----END PUBLIC KEY-----"
	priv, pub := decode(encPriv, encPub)
	key = priv
	pubKey = pub
	return nil
}

func VerifyQuorumSignature(aggregatedSigs AggSig, blockID common.Hash, aggSigners []types.NodeID) (bool, error) {
	var sigIsCorrect bool
	var errAgg error
	for i, _ := range aggSigners {
		sigIsCorrect, errAgg = PubVerify(aggregatedSigs[i], IDToByte(blockID))
		if errAgg != nil {
			return false, errAgg
		}
		if !sigIsCorrect {
			return false, nil
		}
	}
	return true, nil
}
