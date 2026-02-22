// Package consensus implements Ethereum consensus-layer primitives.
// This file implements an EIP-6110 deposit processing queue that manages
// deposits from the execution layer for validator activation on the beacon chain.

package consensus

import (
	"encoding/binary"
	"errors"
	"sync"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

const (
	// DepositPubkeyLen is the expected BLS public key length for deposits.
	DepositPubkeyLen = 48

	// DepositSigLen is the expected BLS signature length for deposits.
	DepositSigLen = 96

	// DepositWithdrawalCredsLen is the expected withdrawal credentials length.
	DepositWithdrawalCredsLen = 32
)

var (
	ErrDepositQueueBelowMinimum      = errors.New("deposit queue: amount below minimum")
	ErrDepositQueueAboveMax          = errors.New("deposit queue: amount above max effective balance")
	ErrDepositQueueInvalidPubkey     = errors.New("deposit queue: invalid pubkey length")
	ErrDepositQueueEmptyPubkey       = errors.New("deposit queue: pubkey is empty")
	ErrDepositQueueInvalidSig        = errors.New("deposit queue: invalid signature length")
	ErrDepositQueueInvalidCreds      = errors.New("deposit queue: invalid withdrawal credentials length")
	ErrDepositQueueDuplicateIndex    = errors.New("deposit queue: duplicate deposit index")
	ErrDepositQueueZeroAmount        = errors.New("deposit queue: amount must be > 0")
)

// DepositQueueConfig holds configuration for the deposit processing queue.
type DepositQueueConfig struct {
	// MaxDepositsPerBlock is the maximum number of deposits processed per block.
	MaxDepositsPerBlock int

	// MinDepositAmount is the minimum deposit amount in Gwei.
	MinDepositAmount uint64

	// MaxEffectiveBalance is the maximum effective balance in Gwei.
	MaxEffectiveBalance uint64

	// DepositContractAddress is the execution-layer deposit contract address.
	DepositContractAddress types.Address
}

// DefaultDepositQueueConfig returns sensible default values.
func DefaultDepositQueueConfig() DepositQueueConfig {
	return DepositQueueConfig{
		MaxDepositsPerBlock: 16,
		MinDepositAmount:    32_000_000_000, // 32 ETH
		MaxEffectiveBalance: 2048_000_000_000, // 2048 ETH (EIP-7251)
		DepositContractAddress: types.HexToAddress(
			"0x00000000219ab540356cBB839Cbe05303d7705Fa",
		),
	}
}

// DepositEntry represents a single deposit from the execution layer.
type DepositEntry struct {
	// Index is the monotonically increasing deposit index.
	Index uint64

	// Pubkey is the BLS12-381 public key (48 bytes).
	Pubkey []byte

	// WithdrawalCredentials are the 32-byte withdrawal credentials.
	WithdrawalCredentials []byte

	// Amount is the deposit amount in Gwei.
	Amount uint64

	// Signature is the BLS12-381 signature (96 bytes).
	Signature []byte

	// BlockNumber is the execution-layer block containing this deposit.
	BlockNumber uint64
}

// DepositQueue processes EIP-6110 deposits from the execution layer.
// It validates, queues, and tracks deposits for beacon chain processing.
// All methods are thread-safe.
type DepositQueue struct {
	mu         sync.Mutex
	config     DepositQueueConfig
	pending    []*DepositEntry
	processed  []*DepositEntry
	indexSet   map[uint64]bool // tracks all known deposit indices
	validators map[string]uint64 // pubkey (hex) -> accumulated amount
	totalCount uint64
}

// NewDepositQueue creates a new deposit queue with the given config.
func NewDepositQueue(config DepositQueueConfig) *DepositQueue {
	return &DepositQueue{
		config:     config,
		pending:    make([]*DepositEntry, 0),
		processed:  make([]*DepositEntry, 0),
		indexSet:   make(map[uint64]bool),
		validators: make(map[string]uint64),
	}
}

// ValidateDeposit checks a deposit entry for validity without adding it
// to the queue. Returns an error describing any validation failure.
func (dq *DepositQueue) ValidateDeposit(entry DepositEntry) error {
	if len(entry.Pubkey) == 0 {
		return ErrDepositQueueEmptyPubkey
	}
	if len(entry.Pubkey) != DepositPubkeyLen {
		return ErrDepositQueueInvalidPubkey
	}
	if entry.Amount == 0 {
		return ErrDepositQueueZeroAmount
	}
	if entry.Amount < dq.config.MinDepositAmount {
		return ErrDepositQueueBelowMinimum
	}
	if entry.Amount > dq.config.MaxEffectiveBalance {
		return ErrDepositQueueAboveMax
	}
	if len(entry.Signature) != 0 && len(entry.Signature) != DepositSigLen {
		return ErrDepositQueueInvalidSig
	}
	if len(entry.WithdrawalCredentials) != 0 && len(entry.WithdrawalCredentials) != DepositWithdrawalCredsLen {
		return ErrDepositQueueInvalidCreds
	}
	return nil
}

// AddDeposit validates and adds a deposit entry to the pending queue.
func (dq *DepositQueue) AddDeposit(entry DepositEntry) error {
	if err := dq.ValidateDeposit(entry); err != nil {
		return err
	}

	dq.mu.Lock()
	defer dq.mu.Unlock()

	if dq.indexSet[entry.Index] {
		return ErrDepositQueueDuplicateIndex
	}

	// Store a copy.
	e := entry
	e.Pubkey = copyBytes(entry.Pubkey)
	e.WithdrawalCredentials = copyBytes(entry.WithdrawalCredentials)
	e.Signature = copyBytes(entry.Signature)

	dq.pending = append(dq.pending, &e)
	dq.indexSet[entry.Index] = true
	dq.totalCount++

	// Track accumulated amount per validator pubkey.
	key := string(entry.Pubkey)
	dq.validators[key] += entry.Amount

	return nil
}

// ProcessDeposits removes and returns up to maxCount deposits from the
// front of the pending queue, moving them to the processed list.
// If maxCount exceeds MaxDepositsPerBlock, it is capped.
func (dq *DepositQueue) ProcessDeposits(maxCount int) []DepositEntry {
	dq.mu.Lock()
	defer dq.mu.Unlock()

	if maxCount > dq.config.MaxDepositsPerBlock {
		maxCount = dq.config.MaxDepositsPerBlock
	}
	if maxCount > len(dq.pending) {
		maxCount = len(dq.pending)
	}
	if maxCount <= 0 {
		return nil
	}

	result := make([]DepositEntry, maxCount)
	for i := 0; i < maxCount; i++ {
		result[i] = *dq.pending[i]
		dq.processed = append(dq.processed, dq.pending[i])
	}
	dq.pending = dq.pending[maxCount:]

	return result
}

// GetDepositRoot computes a Merkle root of all pending deposits using
// Keccak-256 hashing. Returns the zero hash if there are no pending deposits.
func (dq *DepositQueue) GetDepositRoot() types.Hash {
	dq.mu.Lock()
	defer dq.mu.Unlock()

	if len(dq.pending) == 0 {
		return types.Hash{}
	}

	leaves := make([][]byte, len(dq.pending))
	for i, d := range dq.pending {
		leaves[i] = hashDepositEntry(d)
	}

	return merkleRoot(leaves)
}

// GetDepositCount returns the total number of deposits ever added
// (both pending and processed).
func (dq *DepositQueue) GetDepositCount() uint64 {
	dq.mu.Lock()
	defer dq.mu.Unlock()
	return dq.totalCount
}

// PendingDeposits returns the number of currently pending deposits.
func (dq *DepositQueue) PendingDeposits() int {
	dq.mu.Lock()
	defer dq.mu.Unlock()
	return len(dq.pending)
}

// hashDepositEntry produces a Keccak-256 hash of a deposit entry's fields.
func hashDepositEntry(d *DepositEntry) []byte {
	// Encode: index (8 LE) + pubkey + creds + amount (8 LE) + block (8 LE)
	buf := make([]byte, 0, 8+len(d.Pubkey)+len(d.WithdrawalCredentials)+8+8)

	var tmp [8]byte
	binary.LittleEndian.PutUint64(tmp[:], d.Index)
	buf = append(buf, tmp[:]...)
	buf = append(buf, d.Pubkey...)
	buf = append(buf, d.WithdrawalCredentials...)
	binary.LittleEndian.PutUint64(tmp[:], d.Amount)
	buf = append(buf, tmp[:]...)
	binary.LittleEndian.PutUint64(tmp[:], d.BlockNumber)
	buf = append(buf, tmp[:]...)

	return crypto.Keccak256(buf)
}

// merkleRoot builds a binary Merkle tree from leaf hashes and returns
// the root as a types.Hash. Odd levels duplicate the last leaf.
func merkleRoot(leaves [][]byte) types.Hash {
	if len(leaves) == 0 {
		return types.Hash{}
	}
	if len(leaves) == 1 {
		return types.BytesToHash(leaves[0])
	}

	current := make([][]byte, len(leaves))
	copy(current, leaves)

	for len(current) > 1 {
		if len(current)%2 != 0 {
			current = append(current, current[len(current)-1])
		}
		next := make([][]byte, len(current)/2)
		for i := 0; i < len(current); i += 2 {
			combined := make([]byte, 0, len(current[i])+len(current[i+1]))
			combined = append(combined, current[i]...)
			combined = append(combined, current[i+1]...)
			next[i/2] = crypto.Keccak256(combined)
		}
		current = next
	}

	return types.BytesToHash(current[0])
}

// copyBytes returns a copy of the byte slice. Returns nil for nil input.
func copyBytes(b []byte) []byte {
	if b == nil {
		return nil
	}
	out := make([]byte, len(b))
	copy(out, b)
	return out
}
