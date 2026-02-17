package crypto

import (
	"github.com/eth2028/eth2028/core/types"
	"golang.org/x/crypto/sha3"
)

// Keccak256 calculates the Keccak-256 hash of the given data.
func Keccak256(data ...[]byte) []byte {
	d := sha3.NewLegacyKeccak256()
	for _, b := range data {
		d.Write(b)
	}
	return d.Sum(nil)
}

// Keccak256Hash calculates Keccak-256 and returns it as a types.Hash.
func Keccak256Hash(data ...[]byte) types.Hash {
	return types.BytesToHash(Keccak256(data...))
}
