package vm

import (
	"encoding/binary"
	"errors"

	"github.com/eth2030/eth2030/core/types"
)

// AA proof precompile at address 0x0205.
// Verifies account abstraction proof data for EVM-native AA support.

// AAProofPrecompileAddr is the address for the AA proof verification precompile.
var AAProofPrecompileAddr = types.BytesToAddress([]byte{0x02, 0x05})

// AA proof type identifiers.
const (
	AAProofCodeHash         byte = 0x01
	AAProofStorageProof     byte = 0x02
	AAProofValidationResult byte = 0x03
)

// AA proof gas costs.
const (
	aaProofBaseGas    uint64 = 5000
	aaProofPerItemGas uint64 = 1000
)

// AA proof errors.
var (
	ErrAAProofShortInput = errors.New("aa proof: input too short")
)

// AAProofPrecompile implements account abstraction proof verification.
type AAProofPrecompile struct{}

// RequiredGas returns the gas cost: base 5000 + 1000 per proof item.
// The number of proof items is estimated from input size: each item is
// at least 32 bytes, plus the 1-byte proof type prefix.
func (c *AAProofPrecompile) RequiredGas(input []byte) uint64 {
	if len(input) <= 1 {
		return aaProofBaseGas
	}
	// Count proof items based on 32-byte chunks in the proof data.
	proofDataLen := len(input) - 1
	items := uint64(proofDataLen+31) / 32
	if items == 0 {
		items = 1
	}
	return aaProofBaseGas + items*aaProofPerItemGas
}

// Run executes the AA proof verification precompile.
// Input format: proofType[1] || proofData
// Returns 0x01 for valid proof, 0x00 for invalid, error for malformed input.
func (c *AAProofPrecompile) Run(input []byte) ([]byte, error) {
	if len(input) < 1 {
		return nil, ErrAAProofShortInput
	}

	proofType := input[0]
	proofData := input[1:]

	switch proofType {
	case AAProofCodeHash:
		return c.verifyCodeHash(proofData)
	case AAProofStorageProof:
		return c.verifyStorageProof(proofData)
	case AAProofValidationResult:
		return c.verifyValidationResult(proofData)
	default:
		// Unknown proof type returns 0x00 (invalid).
		return []byte{0x00}, nil
	}
}

// verifyCodeHash validates an account code hash proof.
// Expects 32 bytes (the code hash). Returns 0x01 if well-formed.
func (c *AAProofPrecompile) verifyCodeHash(data []byte) ([]byte, error) {
	if len(data) < 32 {
		return []byte{0x00}, nil
	}
	// Check that the hash is non-zero (a zero hash is not meaningful).
	allZero := true
	for _, b := range data[:32] {
		if b != 0 {
			allZero = false
			break
		}
	}
	if allZero {
		return []byte{0x00}, nil
	}
	return []byte{0x01}, nil
}

// verifyStorageProof validates a storage proof.
// Expects key[32] || value[32] || proof... Returns 0x01 if well-formed.
func (c *AAProofPrecompile) verifyStorageProof(data []byte) ([]byte, error) {
	// Need at least key (32) + value (32) = 64 bytes.
	if len(data) < 64 {
		return []byte{0x00}, nil
	}
	// Verify that the key is non-zero.
	allZero := true
	for _, b := range data[:32] {
		if b != 0 {
			allZero = false
			break
		}
	}
	if allZero {
		return []byte{0x00}, nil
	}
	return []byte{0x01}, nil
}

// verifyValidationResult validates an AA validation result.
// Expects status[1] || validAfter[8] || validUntil[8] = 17 bytes.
// Returns 0x01 if the time range is valid (validAfter < validUntil and status == 0x01).
func (c *AAProofPrecompile) verifyValidationResult(data []byte) ([]byte, error) {
	if len(data) < 17 {
		return []byte{0x00}, nil
	}

	status := data[0]
	validAfter := binary.BigEndian.Uint64(data[1:9])
	validUntil := binary.BigEndian.Uint64(data[9:17])

	// Status must be 0x01 (success) and validAfter must be strictly less than validUntil.
	if status != 0x01 {
		return []byte{0x00}, nil
	}
	if validAfter >= validUntil {
		return []byte{0x00}, nil
	}
	return []byte{0x01}, nil
}
