package crypto

import (
	"sync"

	"github.com/eth2030/eth2030/core/types"
)

// ShieldedTx represents a private shielded transfer on L1.
// This is a stub for future zk-SNARK integration. The proof field will
// eventually contain a zero-knowledge proof of valid state transition.
type ShieldedTx struct {
	NullifierHash types.Hash // unique identifier to prevent double-spend
	Commitment    types.Hash // Pedersen commitment to the transfer
	EncryptedData []byte     // encrypted transfer details (recipient, amount)
	Proof         []byte     // zk-SNARK proof (stub: not validated yet)
}

// ShieldedPool tracks commitments and nullifiers for private transfers.
// Commitments represent unspent notes; nullifiers mark notes as spent.
type ShieldedPool struct {
	mu          sync.RWMutex
	commitments map[types.Hash]bool // set of valid commitments
	nullifiers  map[types.Hash]bool // set of revealed nullifiers (spent notes)
}

// NewShieldedPool creates a new shielded transfer pool.
func NewShieldedPool() *ShieldedPool {
	return &ShieldedPool{
		commitments: make(map[types.Hash]bool),
		nullifiers:  make(map[types.Hash]bool),
	}
}

// CreateShieldedTx creates a new shielded transaction stub.
// In a real implementation, this would generate a zk-SNARK proof from the
// sender, recipient, amount, and blinding factor. For now, it creates a
// deterministic commitment using Keccak256.
func CreateShieldedTx(sender, recipient types.Address, amount uint64, blinding [32]byte) *ShieldedTx {
	// Generate commitment: H(sender || recipient || amount || blinding).
	var amountBytes [8]byte
	amountBytes[0] = byte(amount >> 56)
	amountBytes[1] = byte(amount >> 48)
	amountBytes[2] = byte(amount >> 40)
	amountBytes[3] = byte(amount >> 32)
	amountBytes[4] = byte(amount >> 24)
	amountBytes[5] = byte(amount >> 16)
	amountBytes[6] = byte(amount >> 8)
	amountBytes[7] = byte(amount)

	commitment := Keccak256Hash(sender[:], recipient[:], amountBytes[:], blinding[:])

	// Generate nullifier: H(commitment || sender).
	nullifier := Keccak256Hash(commitment[:], sender[:])

	// Encrypted data: just a placeholder (concatenate recipient + amount bytes).
	encrypted := make([]byte, 0, types.AddressLength+8)
	encrypted = append(encrypted, recipient[:]...)
	encrypted = append(encrypted, amountBytes[:]...)

	return &ShieldedTx{
		NullifierHash: nullifier,
		Commitment:    commitment,
		EncryptedData: encrypted,
		Proof:         nil, // stub: no proof generated yet
	}
}

// VerifyShieldedTx verifies a shielded transaction.
// Stub implementation: always returns true. Future versions will verify
// the zk-SNARK proof against the commitment and nullifier.
func VerifyShieldedTx(tx *ShieldedTx) bool {
	if tx == nil {
		return false
	}
	// Stub: always valid. Real implementation would verify:
	// 1. The zk-SNARK proof is valid
	// 2. The commitment is well-formed
	// 3. The nullifier hasn't been seen before
	return true
}

// AddCommitment adds a commitment to the shielded pool.
func (sp *ShieldedPool) AddCommitment(commitment types.Hash) {
	sp.mu.Lock()
	defer sp.mu.Unlock()
	sp.commitments[commitment] = true
}

// HasCommitment returns true if the commitment exists in the pool.
func (sp *ShieldedPool) HasCommitment(commitment types.Hash) bool {
	sp.mu.RLock()
	defer sp.mu.RUnlock()
	return sp.commitments[commitment]
}

// CheckNullifier returns true if the nullifier has already been revealed
// (i.e., the note has been spent). Used to prevent double-spend.
func (sp *ShieldedPool) CheckNullifier(hash types.Hash) bool {
	sp.mu.RLock()
	defer sp.mu.RUnlock()
	return sp.nullifiers[hash]
}

// RevealNullifier marks a nullifier as spent. Returns false if the nullifier
// was already revealed (double-spend attempt).
func (sp *ShieldedPool) RevealNullifier(hash types.Hash) bool {
	sp.mu.Lock()
	defer sp.mu.Unlock()
	if sp.nullifiers[hash] {
		return false // already spent
	}
	sp.nullifiers[hash] = true
	return true
}

// NullifierRoot computes a Merkle root of the nullifier set.
// This is a simplified implementation that hashes all nullifiers together.
// A real implementation would use a sparse Merkle tree.
func (sp *ShieldedPool) NullifierRoot() types.Hash {
	sp.mu.RLock()
	defer sp.mu.RUnlock()

	if len(sp.nullifiers) == 0 {
		return types.Hash{}
	}

	// Collect all nullifiers and hash them together.
	// Order doesn't matter for this stub since we're just XOR-folding
	// then hashing. A real implementation would use a Merkle tree.
	var combined []byte
	for hash := range sp.nullifiers {
		combined = append(combined, hash[:]...)
	}
	return Keccak256Hash(combined)
}

// CommitmentCount returns the number of commitments in the pool.
func (sp *ShieldedPool) CommitmentCount() int {
	sp.mu.RLock()
	defer sp.mu.RUnlock()
	return len(sp.commitments)
}

// NullifierCount returns the number of revealed nullifiers.
func (sp *ShieldedPool) NullifierCount() int {
	sp.mu.RLock()
	defer sp.mu.RUnlock()
	return len(sp.nullifiers)
}
