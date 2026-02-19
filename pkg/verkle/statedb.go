package verkle

import (
	"encoding/binary"
	"math/big"

	"github.com/eth2028/eth2028/core/types"
)

// VerkleStateDB provides account and storage access backed by a Verkle tree.
// It uses EIP-6800 key derivation to map account fields and storage slots
// to Verkle tree keys.
type VerkleStateDB struct {
	tree VerkleTree
}

// NewVerkleStateDB wraps a VerkleTree with account/storage accessors.
func NewVerkleStateDB(tree VerkleTree) *VerkleStateDB {
	return &VerkleStateDB{tree: tree}
}

// GetAccount reads account fields from the Verkle tree and returns
// a reconstructed AccountState. Returns nil if the account does not exist.
func (s *VerkleStateDB) GetAccount(addr types.Address) *AccountState {
	// Read balance.
	balKey := GetTreeKeyForBalance(addr)
	balBytes, err := s.tree.Get(balKey[:])
	if err != nil || balBytes == nil {
		return nil
	}

	// Read nonce.
	nonceKey := GetTreeKeyForNonce(addr)
	nonceBytes, _ := s.tree.Get(nonceKey[:])

	// Read code hash.
	chKey := GetTreeKeyForCodeHash(addr)
	chBytes, _ := s.tree.Get(chKey[:])

	acct := &AccountState{
		Balance: new(big.Int).SetBytes(balBytes),
	}

	if nonceBytes != nil {
		acct.Nonce = binary.LittleEndian.Uint64(pad8(nonceBytes))
	}
	if chBytes != nil {
		copy(acct.CodeHash[:], chBytes)
	}

	return acct
}

// SetAccount writes account fields into the Verkle tree.
func (s *VerkleStateDB) SetAccount(addr types.Address, acct *AccountState) {
	if acct == nil {
		return
	}

	// Write version = 0.
	vKey := GetTreeKeyForVersion(addr)
	var versionVal [ValueSize]byte
	s.tree.Put(vKey[:], versionVal[:])

	// Write balance.
	balKey := GetTreeKeyForBalance(addr)
	var balVal [ValueSize]byte
	if acct.Balance != nil {
		b := acct.Balance.Bytes()
		// Store in big-endian, left-padded.
		copy(balVal[ValueSize-len(b):], b)
	}
	s.tree.Put(balKey[:], balVal[:])

	// Write nonce.
	nonceKey := GetTreeKeyForNonce(addr)
	var nonceVal [ValueSize]byte
	binary.LittleEndian.PutUint64(nonceVal[:8], acct.Nonce)
	s.tree.Put(nonceKey[:], nonceVal[:])

	// Write code hash.
	chKey := GetTreeKeyForCodeHash(addr)
	var chVal [ValueSize]byte
	copy(chVal[:], acct.CodeHash[:])
	s.tree.Put(chKey[:], chVal[:])
}

// GetStorage reads a storage slot from the Verkle tree.
func (s *VerkleStateDB) GetStorage(addr types.Address, key types.Hash) types.Hash {
	// Convert the hash key to a storage slot number (first 8 bytes).
	slot := hashToSlot(key)
	treeKey := GetTreeKeyForStorageSlot(addr, slot)
	val, err := s.tree.Get(treeKey[:])
	if err != nil || val == nil {
		return types.Hash{}
	}
	return types.BytesToHash(val)
}

// SetStorage writes a storage slot into the Verkle tree.
func (s *VerkleStateDB) SetStorage(addr types.Address, key, value types.Hash) {
	slot := hashToSlot(key)
	treeKey := GetTreeKeyForStorageSlot(addr, slot)
	var val [ValueSize]byte
	copy(val[:], value[:])
	s.tree.Put(treeKey[:], val[:])
}

// Commit computes and returns the Verkle tree root hash.
func (s *VerkleStateDB) Commit() (types.Hash, error) {
	return s.tree.Commit()
}

// Tree returns the underlying VerkleTree.
func (s *VerkleStateDB) Tree() VerkleTree {
	return s.tree
}

// AccountState represents the state of an account in the Verkle state DB.
type AccountState struct {
	Nonce    uint64
	Balance  *big.Int
	CodeHash types.Hash
}

// hashToSlot converts a storage key hash to a slot number by reading
// the last 8 bytes as a uint64.
func hashToSlot(h types.Hash) uint64 {
	return binary.BigEndian.Uint64(h[24:])
}

// pad8 returns a byte slice padded or truncated to at least 8 bytes.
func pad8(b []byte) []byte {
	if len(b) >= 8 {
		return b[:8]
	}
	padded := make([]byte, 8)
	copy(padded, b)
	return padded
}
