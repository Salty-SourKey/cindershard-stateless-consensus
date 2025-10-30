package crypto

import (
	"crypto/ecdsa"
	"crypto/rand"
	"math/big"
)

// ecdsa.Sign returns two *big.Int variables. In order to save it in the Signature type,
// I first turn them into strings, and then I turn the strings to byte arrays.
// I have implemented a Signature to ecdsa signature parser (toECDSA in signature.go) in oder to
// cast the byte array Signature into the original signature of the ECDSA signing method.
func PrivSign(msg []byte, hasher Hasher) (Signature, error) {
	var r, s *big.Int
	var err error
	if hasher != nil {
		r, s, err = ecdsa.Sign(rand.Reader, key, hasher.ComputeHash(msg))
		if err != nil {
			return nil, err
		}
	} else {
		r, s, err = ecdsa.Sign(rand.Reader, key, msg)
		if err != nil {
			return nil, err
		}
	}
	sig := make([][]byte, 2)
	sig[0] = []byte(r.String())
	sig[1] = []byte(s.String())
	return sig, err
}

func PubVerify(sig Signature, hash Hash) (bool, error) {
	ecdsaSig := sig.ToECDSA()
	isVerified := ecdsa.Verify(pubKey, hash, ecdsaSig.r, ecdsaSig.s)
	return isVerified, nil
}
