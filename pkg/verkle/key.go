package verkle

import (
	"encoding/binary"
	"math/big"

	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/crypto"
)

// EIP-6800 tree key derivation constants.
// Each account's state is stored under a common stem derived from the address.
// Different suffixes within the stem correspond to different fields.
const (
	// Header fields (suffixes 0-63).
	VersionLeafKey    byte = 0
	BalanceLeafKey    byte = 1
	NonceLeafKey      byte = 2
	CodeHashLeafKey   byte = 3
	CodeSizeLeafKey   byte = 4

	// Code chunks start at suffix 128.
	CodeOffset byte = 128

	// Storage slots are derived from the slot key using a separate tree key.
	MainStorageOffset uint64 = 256

	// Header storage offset for small storage slots (0-63).
	HeaderStorageOffset byte = 64

	// Max code chunks per stem (suffixes 128-255).
	MaxCodeChunksPerStem = 128
)

// GetTreeKeyForVersion returns the tree key for the account version field.
func GetTreeKeyForVersion(addr types.Address) [KeySize]byte {
	return getTreeKey(addr, VersionLeafKey)
}

// GetTreeKeyForBalance returns the tree key for the account balance.
func GetTreeKeyForBalance(addr types.Address) [KeySize]byte {
	return getTreeKey(addr, BalanceLeafKey)
}

// GetTreeKeyForNonce returns the tree key for the account nonce.
func GetTreeKeyForNonce(addr types.Address) [KeySize]byte {
	return getTreeKey(addr, NonceLeafKey)
}

// GetTreeKeyForCodeHash returns the tree key for the account code hash.
func GetTreeKeyForCodeHash(addr types.Address) [KeySize]byte {
	return getTreeKey(addr, CodeHashLeafKey)
}

// GetTreeKeyForCodeSize returns the tree key for the account code size.
func GetTreeKeyForCodeSize(addr types.Address) [KeySize]byte {
	return getTreeKey(addr, CodeSizeLeafKey)
}

// GetTreeKeyForCodeChunk returns the tree key for the Nth code chunk.
// Each chunk is 31 bytes. Chunks 0-127 fit in the account header stem.
func GetTreeKeyForCodeChunk(addr types.Address, chunkID uint64) [KeySize]byte {
	if chunkID < MaxCodeChunksPerStem {
		return getTreeKey(addr, CodeOffset+byte(chunkID))
	}
	// Chunks beyond 127 go to separate stems via the storage tree key mechanism.
	return GetTreeKeyForStorageSlot(addr, uint64(CodeOffset)+chunkID)
}

// GetTreeKeyForStorageSlot returns the tree key for a storage slot.
// Small slots (< 64) are stored in the account header stem.
// Larger slots are stored in a separate stem derived from the slot number.
func GetTreeKeyForStorageSlot(addr types.Address, storageSlot uint64) [KeySize]byte {
	if storageSlot < 64 {
		return getTreeKey(addr, HeaderStorageOffset+byte(storageSlot))
	}

	// For larger slots, derive a separate stem.
	stem := getStorageStem(addr, storageSlot)
	suffix := byte(storageSlot % 256)

	var key [KeySize]byte
	copy(key[:StemSize], stem[:])
	key[StemSize] = suffix
	return key
}

// getTreeKey derives the tree key for an address and suffix in the account header stem.
func getTreeKey(addr types.Address, suffix byte) [KeySize]byte {
	stem := getAccountStem(addr)
	var key [KeySize]byte
	copy(key[:StemSize], stem[:])
	key[StemSize] = suffix
	return key
}

// getAccountStem derives the 31-byte Verkle tree stem for an Ethereum address.
// Per EIP-6800: stem = PedersenHash(addr)[0:31], where PedersenHash maps
// the address bytes through the Pedersen commitment and then to the field.
func getAccountStem(addr types.Address) [StemSize]byte {
	// Encode the address as scalars for the Pedersen commitment.
	// Split 20-byte address into two 128-bit values for the commitment vector.
	values := make([]*big.Int, 4)
	values[0] = big.NewInt(1) // domain separator for account stems
	values[1] = new(big.Int).SetBytes(addr[:])
	values[2] = new(big.Int)
	values[3] = new(big.Int)

	h := crypto.PedersenCommitBytes(values)
	var stem [StemSize]byte
	copy(stem[:], h[:StemSize])
	return stem
}

// getStorageStem derives the stem for a storage slot beyond the header range.
// Per EIP-6800: storage keys use a different tree path derived from address + slot.
func getStorageStem(addr types.Address, slot uint64) [StemSize]byte {
	values := make([]*big.Int, 4)
	values[0] = big.NewInt(2) // domain separator for storage stems
	values[1] = new(big.Int).SetBytes(addr[:])
	var slotBytes [8]byte
	binary.BigEndian.PutUint64(slotBytes[:], slot/256)
	values[2] = new(big.Int).SetBytes(slotBytes[:])
	values[3] = new(big.Int)

	h := crypto.PedersenCommitBytes(values)
	var stem [StemSize]byte
	copy(stem[:], h[:StemSize])
	return stem
}

// VerkleKeyFromAddress computes the 32-byte tree key for an address
// with a given suffix, per EIP-6800 stem derivation.
// The first 31 bytes are the stem (derived from the address via the
// Pedersen hash placeholder) and the last byte is the suffix that
// selects the field within the leaf node.
func VerkleKeyFromAddress(addr types.Address, suffix byte) []byte {
	stem := getAccountStem(addr)
	key := make([]byte, KeySize)
	copy(key[:StemSize], stem[:])
	key[StemSize] = suffix
	return key
}

// AccountHeaderKeys returns the five standard EIP-6800 account header
// tree keys for an address: version, balance, nonce, code_hash, and
// code_size. These all share the same 31-byte stem.
func AccountHeaderKeys(addr types.Address) [5][KeySize]byte {
	return [5][KeySize]byte{
		GetTreeKeyForVersion(addr),
		GetTreeKeyForBalance(addr),
		GetTreeKeyForNonce(addr),
		GetTreeKeyForCodeHash(addr),
		GetTreeKeyForCodeSize(addr),
	}
}

// StemFromKey extracts the 31-byte stem from a 32-byte tree key.
func StemFromKey(key [KeySize]byte) [StemSize]byte {
	var stem [StemSize]byte
	copy(stem[:], key[:StemSize])
	return stem
}

// SuffixFromKey extracts the suffix byte from a 32-byte tree key.
func SuffixFromKey(key [KeySize]byte) byte {
	return key[StemSize]
}
