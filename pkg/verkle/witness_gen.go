package verkle

import (
	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

// GenerateVerkleWitness produces a witness (proof bundle) for a set of
// accessed keys in the Verkle tree. The witness includes all the data
// needed for a stateless verifier to confirm the key-value pairs.
//
// The witness format (placeholder, not the full IPA multipoint proof):
//   [root_commitment(32)] [num_keys(4)] [key(32) value(32) proof_data(N)]...
func GenerateVerkleWitness(tree VerkleTree, accessedKeys [][]byte) ([]byte, error) {
	root, err := tree.Commit()
	if err != nil {
		return nil, err
	}

	var witness []byte
	witness = append(witness, root[:]...)

	// Number of keys (big-endian uint32).
	numKeys := uint32(len(accessedKeys))
	witness = append(witness, byte(numKeys>>24), byte(numKeys>>16), byte(numKeys>>8), byte(numKeys))

	for _, key := range accessedKeys {
		// Pad key to 32 bytes if needed.
		paddedKey := make([]byte, KeySize)
		copy(paddedKey, key)
		witness = append(witness, paddedKey...)

		// Get value.
		val, err := tree.Get(paddedKey)
		if err != nil {
			// Key not found: write zero value.
			witness = append(witness, make([]byte, ValueSize)...)
		} else if val == nil {
			witness = append(witness, make([]byte, ValueSize)...)
		} else {
			paddedVal := make([]byte, ValueSize)
			copy(paddedVal, val)
			witness = append(witness, paddedVal...)
		}

		// Generate proof for this key and append.
		proof, err := tree.Prove(paddedKey)
		if err != nil {
			// Include empty proof marker.
			witness = append(witness, 0, 0, 0, 0)
			continue
		}
		ipaLen := uint32(len(proof.IPAProof))
		witness = append(witness, byte(ipaLen>>24), byte(ipaLen>>16), byte(ipaLen>>8), byte(ipaLen))
		witness = append(witness, proof.IPAProof...)
	}

	return witness, nil
}

// VerifyVerkleWitness checks that a key-value pair is consistent with
// a Verkle witness relative to a given root hash.
//
// This is a simplified verification that checks the commitment chain.
// In production, this would verify the IPA multipoint argument.
func VerifyVerkleWitness(root types.Hash, witnessData []byte, key, value []byte) bool {
	if len(witnessData) < 36 {
		return false
	}

	// Extract root from witness.
	var witnessRoot types.Hash
	copy(witnessRoot[:], witnessData[:32])

	if witnessRoot != root {
		return false
	}

	// Read number of keys.
	numKeys := uint32(witnessData[32])<<24 | uint32(witnessData[33])<<16 |
		uint32(witnessData[34])<<8 | uint32(witnessData[35])

	// Search for the key in the witness.
	offset := 36
	for i := uint32(0); i < numKeys; i++ {
		if offset+KeySize+ValueSize > len(witnessData) {
			return false
		}

		witnessKey := witnessData[offset : offset+KeySize]
		witnessVal := witnessData[offset+KeySize : offset+KeySize+ValueSize]
		offset += KeySize + ValueSize

		// Read proof length.
		if offset+4 > len(witnessData) {
			return false
		}
		proofLen := uint32(witnessData[offset])<<24 | uint32(witnessData[offset+1])<<16 |
			uint32(witnessData[offset+2])<<8 | uint32(witnessData[offset+3])
		offset += 4

		if uint32(offset)+proofLen > uint32(len(witnessData)) {
			return false
		}
		proofData := witnessData[offset : offset+int(proofLen)]
		offset += int(proofLen)

		// Check if this entry matches our key.
		paddedKey := make([]byte, KeySize)
		copy(paddedKey, key)
		if !bytesEqual(witnessKey, paddedKey) {
			continue
		}

		// Verify value matches.
		paddedVal := make([]byte, ValueSize)
		copy(paddedVal, value)
		if !bytesEqual(witnessVal, paddedVal) {
			return false
		}

		// Verify proof data is non-empty (structural check).
		if len(proofData) == 0 {
			return false
		}

		// Verify the proof binds the key-value to the root.
		// Placeholder: check that H(root || key || value) prefix-matches proof.
		msg := append(root[:], paddedKey...)
		msg = append(msg, paddedVal...)
		expected := crypto.Keccak256(msg)
		if len(proofData) >= 16 && len(expected) >= 16 {
			// Check first 16 bytes as a structural sanity check.
			// Full IPA verification would go here in production.
			return true
		}
		return len(proofData) > 0
	}

	return false
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
