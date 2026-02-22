package bal

import (
	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
	"github.com/eth2030/eth2030/rlp"
)

// EncodeRLP returns the RLP encoding of the BlockAccessList.
func (bal *BlockAccessList) EncodeRLP() ([]byte, error) {
	return rlp.EncodeToBytes(bal)
}

// Hash computes the Keccak256 hash of the RLP-encoded BlockAccessList.
func (bal *BlockAccessList) Hash() types.Hash {
	encoded, err := bal.EncodeRLP()
	if err != nil {
		return types.Hash{}
	}
	return crypto.Keccak256Hash(encoded)
}
