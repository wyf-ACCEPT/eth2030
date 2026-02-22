// Package consensus implements Ethereum consensus-layer primitives.
//
// prequorum.go implements the Secure Prequorum engine for pre-finality
// confirmations. Validators submit preconfirmations for transactions in
// upcoming slots. When enough unique validators have preconfirmed for a
// given slot, prequorum is reached, giving users high-confidence that
// their transactions will be included before full finality completes.
//
// This targets the CL Cryptography track: "secure prequorum" milestone.
package consensus

import (
	"encoding/binary"
	"errors"
	"sync"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

// Prequorum errors.
var (
	ErrNilPreconfirmation   = errors.New("prequorum: nil preconfirmation")
	ErrPrequorumInvalidSlot = errors.New("prequorum: invalid slot (zero)")
	ErrEmptySignature       = errors.New("prequorum: empty signature")
	ErrEmptyTxHash          = errors.New("prequorum: empty transaction hash")
	ErrEmptyCommitment      = errors.New("prequorum: empty commitment")
	ErrSlotFull             = errors.New("prequorum: max preconfirmations per slot reached")
	ErrDuplicatePreconf     = errors.New("prequorum: duplicate preconfirmation")
	ErrInvalidCommitment    = errors.New("prequorum: commitment does not match expected")
)

// DefaultQuorumThreshold is the fraction of unique validators required for quorum.
const DefaultQuorumThreshold = 0.67

// PrequorumConfig configures the prequorum engine.
type PrequorumConfig struct {
	// QuorumThreshold is the fraction (0,1] of unique validators needed
	// for quorum. Defaults to 0.67 (two-thirds).
	QuorumThreshold float64

	// MaxPreconfsPerSlot limits the number of preconfirmations stored per slot.
	MaxPreconfsPerSlot uint64

	// ValidatorSetSize is the total number of active validators.
	ValidatorSetSize uint64
}

// DefaultPrequorumConfig returns a sensible default prequorum config.
func DefaultPrequorumConfig() PrequorumConfig {
	return PrequorumConfig{
		QuorumThreshold:    DefaultQuorumThreshold,
		MaxPreconfsPerSlot: 10_000,
		ValidatorSetSize:   1_000,
	}
}

// Preconfirmation represents a validator's preconfirmation for a transaction
// in a given slot. Validators sign a commitment binding them to the inclusion
// of the referenced transaction.
type Preconfirmation struct {
	Slot           uint64
	ValidatorIndex uint64
	TxHash         types.Hash
	Commitment     types.Hash
	Signature      []byte
	Timestamp      uint64
}

// PrequorumStatus reports the prequorum state for a slot.
type PrequorumStatus struct {
	Slot             uint64
	TotalPreconfs    int
	UniqueValidators int
	QuorumReached    bool
	Confidence       float64
}

// slotData holds per-slot preconfirmation tracking data.
type slotData struct {
	preconfs   []*Preconfirmation
	validators map[uint64]bool          // set of unique validator indices
	txSet      map[types.Hash]bool      // set of confirmed tx hashes
	seen       map[preconfKey]bool      // dedup key (validator + txHash)
}

// preconfKey is a deduplication key for (validator, tx) pairs within a slot.
type preconfKey struct {
	validator uint64
	txHash    types.Hash
}

// PrequorumEngine manages pre-finality confirmations across slots.
// It is safe for concurrent use.
type PrequorumEngine struct {
	mu     sync.RWMutex
	config PrequorumConfig
	slots  map[uint64]*slotData
}

// NewPrequorumEngine creates a new prequorum engine with the given config.
func NewPrequorumEngine(config PrequorumConfig) *PrequorumEngine {
	if config.QuorumThreshold <= 0 || config.QuorumThreshold > 1 {
		config.QuorumThreshold = DefaultQuorumThreshold
	}
	if config.MaxPreconfsPerSlot == 0 {
		config.MaxPreconfsPerSlot = 10_000
	}
	if config.ValidatorSetSize == 0 {
		config.ValidatorSetSize = 1_000
	}
	return &PrequorumEngine{
		config: config,
		slots:  make(map[uint64]*slotData),
	}
}

// getOrCreateSlot returns the slot data, creating it if needed.
// Caller must hold the write lock.
func (pe *PrequorumEngine) getOrCreateSlot(slot uint64) *slotData {
	sd, ok := pe.slots[slot]
	if !ok {
		sd = &slotData{
			validators: make(map[uint64]bool),
			txSet:      make(map[types.Hash]bool),
			seen:       make(map[preconfKey]bool),
		}
		pe.slots[slot] = sd
	}
	return sd
}

// ValidatePreconfirmation validates a preconfirmation without storing it.
func (pe *PrequorumEngine) ValidatePreconfirmation(preconf *Preconfirmation) error {
	if preconf == nil {
		return ErrNilPreconfirmation
	}
	if preconf.Slot == 0 {
		return ErrPrequorumInvalidSlot
	}
	if preconf.TxHash.IsZero() {
		return ErrEmptyTxHash
	}
	if preconf.Commitment.IsZero() {
		return ErrEmptyCommitment
	}
	if len(preconf.Signature) == 0 {
		return ErrEmptySignature
	}
	// Verify commitment = H(slot || validatorIndex || txHash).
	expected := computeCommitment(preconf.Slot, preconf.ValidatorIndex, preconf.TxHash)
	if preconf.Commitment != expected {
		return ErrInvalidCommitment
	}
	return nil
}

// SubmitPreconfirmation validates and stores a preconfirmation.
func (pe *PrequorumEngine) SubmitPreconfirmation(preconf *Preconfirmation) error {
	if err := pe.ValidatePreconfirmation(preconf); err != nil {
		return err
	}

	pe.mu.Lock()
	defer pe.mu.Unlock()

	sd := pe.getOrCreateSlot(preconf.Slot)

	// Check slot capacity.
	if uint64(len(sd.preconfs)) >= pe.config.MaxPreconfsPerSlot {
		return ErrSlotFull
	}

	// Check for duplicates: same validator + same tx in same slot.
	key := preconfKey{validator: preconf.ValidatorIndex, txHash: preconf.TxHash}
	if sd.seen[key] {
		return ErrDuplicatePreconf
	}

	sd.preconfs = append(sd.preconfs, preconf)
	sd.validators[preconf.ValidatorIndex] = true
	sd.txSet[preconf.TxHash] = true
	sd.seen[key] = true
	return nil
}

// GetPreconfirmations returns all preconfirmations for the given slot.
func (pe *PrequorumEngine) GetPreconfirmations(slot uint64) []*Preconfirmation {
	pe.mu.RLock()
	defer pe.mu.RUnlock()

	sd, ok := pe.slots[slot]
	if !ok {
		return nil
	}
	// Return a copy to avoid data races on the slice.
	out := make([]*Preconfirmation, len(sd.preconfs))
	copy(out, sd.preconfs)
	return out
}

// CheckPrequorum checks whether the prequorum threshold has been reached
// for the given slot.
func (pe *PrequorumEngine) CheckPrequorum(slot uint64) *PrequorumStatus {
	pe.mu.RLock()
	defer pe.mu.RUnlock()

	status := &PrequorumStatus{Slot: slot}

	sd, ok := pe.slots[slot]
	if !ok {
		return status
	}

	status.TotalPreconfs = len(sd.preconfs)
	status.UniqueValidators = len(sd.validators)

	if pe.config.ValidatorSetSize > 0 {
		status.Confidence = float64(status.UniqueValidators) / float64(pe.config.ValidatorSetSize)
	}

	status.QuorumReached = status.Confidence >= pe.config.QuorumThreshold
	return status
}

// GetConfirmedTxs returns the set of transaction hashes that have been
// preconfirmed for the given slot.
func (pe *PrequorumEngine) GetConfirmedTxs(slot uint64) []types.Hash {
	pe.mu.RLock()
	defer pe.mu.RUnlock()

	sd, ok := pe.slots[slot]
	if !ok {
		return nil
	}

	txs := make([]types.Hash, 0, len(sd.txSet))
	for h := range sd.txSet {
		txs = append(txs, h)
	}
	return txs
}

// PurgeSlot removes all data for the given slot, freeing memory.
func (pe *PrequorumEngine) PurgeSlot(slot uint64) {
	pe.mu.Lock()
	defer pe.mu.Unlock()
	delete(pe.slots, slot)
}

// computeCommitment derives the expected commitment hash for a preconfirmation.
// commitment = Keccak256(slot || validatorIndex || txHash)
func computeCommitment(slot, validatorIndex uint64, txHash types.Hash) types.Hash {
	var buf [8 + 8 + types.HashLength]byte
	binary.BigEndian.PutUint64(buf[0:8], slot)
	binary.BigEndian.PutUint64(buf[8:16], validatorIndex)
	copy(buf[16:], txHash[:])
	return crypto.Keccak256Hash(buf[:])
}

// ComputeCommitment is an exported helper so tests and other packages can
// produce valid commitments for a given (slot, validator, txHash) triple.
func ComputeCommitment(slot, validatorIndex uint64, txHash types.Hash) types.Hash {
	return computeCommitment(slot, validatorIndex, txHash)
}
