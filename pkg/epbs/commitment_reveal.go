// commitment_reveal.go implements the builder commitment-reveal protocol for
// ePBS (EIP-7732). Builders commit to execution payloads by submitting bids
// with a block root commitment hash. After winning the auction, the builder
// must reveal the actual payload within a configurable deadline. Builders who
// fail to reveal or reveal invalid payloads are penalized.
//
// Components:
//   - CommitmentManager: tracks commitments and reveal deadlines per slot
//   - BuilderCommitment: individual commitment with slot, builder, bid, hash
//   - RevealWindow: configurable reveal deadline
//   - PenaltyEngine: penalizes builders for failed reveals
//   - CommitmentChain: linked list of commitments per slot for audit
//   - RevealVerifier: verifies revealed payload matches commitment
package epbs

import (
	"errors"
	"fmt"
	"sync"

	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/crypto"
)

// Commitment-reveal errors.
var (
	ErrCRNilCommitment     = errors.New("commitment_reveal: nil commitment")
	ErrCRDuplicateCommit   = errors.New("commitment_reveal: duplicate commitment for slot/builder")
	ErrCRDeadlinePassed    = errors.New("commitment_reveal: reveal deadline has passed")
	ErrCRDeadlineNotPassed = errors.New("commitment_reveal: reveal deadline has not passed yet")
	ErrCRRevealMismatch    = errors.New("commitment_reveal: revealed payload does not match commitment")
	ErrCRNoCommitment      = errors.New("commitment_reveal: no commitment found")
	ErrCRAlreadyRevealed   = errors.New("commitment_reveal: already revealed")
	ErrCRNilPayload        = errors.New("commitment_reveal: nil payload envelope")
	ErrCRSlotMismatch      = errors.New("commitment_reveal: slot mismatch between commitment and payload")
	ErrCRBuilderMismatch   = errors.New("commitment_reveal: builder mismatch between commitment and payload")
)

// BuilderCommitment represents a builder's commitment to produce a payload.
type BuilderCommitment struct {
	Slot           uint64
	BuilderIndex   BuilderIndex
	BuilderAddr    types.Address
	BidAmount      uint64        // in Gwei
	CommitmentHash types.Hash    // keccak256(bid fields)
	BlockRoot      types.Hash    // committed block root
	Revealed       bool
	RevealedAt     uint64        // slot at which payload was revealed
}

// RevealWindow defines the time window for revealing payloads.
type RevealWindow struct {
	// DeadlineSlots is the number of slots after the commitment slot
	// within which the payload must be revealed.
	DeadlineSlots uint64
}

// DefaultRevealWindow returns a standard reveal window of 1 slot.
func DefaultRevealWindow() RevealWindow {
	return RevealWindow{DeadlineSlots: 1}
}

// IsExpired returns true if the reveal deadline has passed.
func (rw RevealWindow) IsExpired(commitSlot, currentSlot uint64) bool {
	return currentSlot > commitSlot+rw.DeadlineSlots
}

// IsWithinWindow returns true if the current slot is within the reveal window.
func (rw RevealWindow) IsWithinWindow(commitSlot, currentSlot uint64) bool {
	return currentSlot >= commitSlot && currentSlot <= commitSlot+rw.DeadlineSlots
}

// Deadline returns the absolute deadline slot for a given commitment slot.
func (rw RevealWindow) Deadline(commitSlot uint64) uint64 {
	return commitSlot + rw.DeadlineSlots
}

// CommitmentNode is a node in the per-slot commitment linked list.
type CommitmentNode struct {
	Commitment *BuilderCommitment
	Next       *CommitmentNode
}

// CommitmentChain is a linked list of commitments per slot for audit trail.
type CommitmentChain struct {
	mu    sync.RWMutex
	heads map[uint64]*CommitmentNode // slot -> head of linked list
}

// NewCommitmentChain creates a new commitment chain.
func NewCommitmentChain() *CommitmentChain {
	return &CommitmentChain{
		heads: make(map[uint64]*CommitmentNode),
	}
}

// Append adds a commitment to the chain for its slot.
func (cc *CommitmentChain) Append(c *BuilderCommitment) {
	if c == nil {
		return
	}
	cc.mu.Lock()
	defer cc.mu.Unlock()

	node := &CommitmentNode{Commitment: c}
	if head, ok := cc.heads[c.Slot]; ok {
		// Walk to the end.
		current := head
		for current.Next != nil {
			current = current.Next
		}
		current.Next = node
	} else {
		cc.heads[c.Slot] = node
	}
}

// ForSlot returns all commitments for a slot in order.
func (cc *CommitmentChain) ForSlot(slot uint64) []*BuilderCommitment {
	cc.mu.RLock()
	defer cc.mu.RUnlock()

	var result []*BuilderCommitment
	node := cc.heads[slot]
	for node != nil {
		result = append(result, node.Commitment)
		node = node.Next
	}
	return result
}

// Len returns the number of commitments for a slot.
func (cc *CommitmentChain) Len(slot uint64) int {
	cc.mu.RLock()
	defer cc.mu.RUnlock()

	count := 0
	node := cc.heads[slot]
	for node != nil {
		count++
		node = node.Next
	}
	return count
}

// PruneSlot removes all commitments for a slot.
func (cc *CommitmentChain) PruneSlot(slot uint64) {
	cc.mu.Lock()
	defer cc.mu.Unlock()
	delete(cc.heads, slot)
}

// RevealVerifier verifies that a revealed payload matches a builder commitment.
type RevealVerifier struct{}

// NewRevealVerifier creates a new verifier.
func NewRevealVerifier() *RevealVerifier {
	return &RevealVerifier{}
}

// Verify checks that the payload envelope matches the commitment.
// It validates slot, builder index, and payload root against the committed hash.
func (rv *RevealVerifier) Verify(commitment *BuilderCommitment, payload *PayloadEnvelope) error {
	if commitment == nil {
		return ErrCRNilCommitment
	}
	if payload == nil {
		return ErrCRNilPayload
	}

	if commitment.Slot != payload.Slot {
		return fmt.Errorf("%w: commitment slot %d, payload slot %d",
			ErrCRSlotMismatch, commitment.Slot, payload.Slot)
	}

	if commitment.BuilderIndex != payload.BuilderIndex {
		return fmt.Errorf("%w: commitment builder %d, payload builder %d",
			ErrCRBuilderMismatch, commitment.BuilderIndex, payload.BuilderIndex)
	}

	// The payload root must match the committed block root.
	if commitment.BlockRoot != payload.PayloadRoot {
		return fmt.Errorf("%w: committed %s, revealed %s",
			ErrCRRevealMismatch, commitment.BlockRoot.Hex(), payload.PayloadRoot.Hex())
	}

	return nil
}

// CRPenaltyConfig configures penalties for the commitment-reveal protocol.
type CRPenaltyConfig struct {
	// NonRevealBasisPoints is the penalty for failing to reveal (basis points of bid).
	NonRevealBasisPoints uint64

	// MismatchBasisPoints is the penalty for revealing a mismatched payload.
	MismatchBasisPoints uint64
}

// DefaultCRPenaltyConfig returns default penalty settings.
func DefaultCRPenaltyConfig() CRPenaltyConfig {
	return CRPenaltyConfig{
		NonRevealBasisPoints: 20000, // 2x bid value
		MismatchBasisPoints:  30000, // 3x bid value
	}
}

// CRPenaltyRecord records a penalty applied through the commitment-reveal protocol.
type CRPenaltyRecord struct {
	Slot         uint64
	BuilderIndex BuilderIndex
	BuilderAddr  types.Address
	BidAmount    uint64
	PenaltyGwei  uint64
	Reason       string
}

// CRPenaltyEngine penalizes builders who violate the commitment-reveal protocol.
type CRPenaltyEngine struct {
	mu      sync.RWMutex
	config  CRPenaltyConfig
	records []*CRPenaltyRecord
}

// NewCRPenaltyEngine creates a penalty engine with the given config.
func NewCRPenaltyEngine(config CRPenaltyConfig) *CRPenaltyEngine {
	return &CRPenaltyEngine{
		config: config,
	}
}

// PenalizeNonReveal records a penalty for failing to reveal a payload.
func (pe *CRPenaltyEngine) PenalizeNonReveal(commitment *BuilderCommitment) (*CRPenaltyRecord, error) {
	if commitment == nil {
		return nil, ErrCRNilCommitment
	}

	penalty := computeBasisPointsPenalty(commitment.BidAmount, pe.config.NonRevealBasisPoints)
	record := &CRPenaltyRecord{
		Slot:         commitment.Slot,
		BuilderIndex: commitment.BuilderIndex,
		BuilderAddr:  commitment.BuilderAddr,
		BidAmount:    commitment.BidAmount,
		PenaltyGwei:  penalty,
		Reason:       "non-reveal: deadline passed without payload delivery",
	}

	pe.mu.Lock()
	pe.records = append(pe.records, record)
	pe.mu.Unlock()

	return record, nil
}

// PenalizeMismatch records a penalty for revealing a mismatched payload.
func (pe *CRPenaltyEngine) PenalizeMismatch(commitment *BuilderCommitment, reason string) (*CRPenaltyRecord, error) {
	if commitment == nil {
		return nil, ErrCRNilCommitment
	}

	penalty := computeBasisPointsPenalty(commitment.BidAmount, pe.config.MismatchBasisPoints)
	record := &CRPenaltyRecord{
		Slot:         commitment.Slot,
		BuilderIndex: commitment.BuilderIndex,
		BuilderAddr:  commitment.BuilderAddr,
		BidAmount:    commitment.BidAmount,
		PenaltyGwei:  penalty,
		Reason:       fmt.Sprintf("mismatch: %s", reason),
	}

	pe.mu.Lock()
	pe.records = append(pe.records, record)
	pe.mu.Unlock()

	return record, nil
}

// Records returns a copy of all penalty records.
func (pe *CRPenaltyEngine) Records() []*CRPenaltyRecord {
	pe.mu.RLock()
	defer pe.mu.RUnlock()
	result := make([]*CRPenaltyRecord, len(pe.records))
	copy(result, pe.records)
	return result
}

// TotalPenaltyForBuilder returns the cumulative penalty for a builder.
func (pe *CRPenaltyEngine) TotalPenaltyForBuilder(addr types.Address) uint64 {
	pe.mu.RLock()
	defer pe.mu.RUnlock()
	var total uint64
	for _, r := range pe.records {
		if r.BuilderAddr == addr {
			total += r.PenaltyGwei
		}
	}
	return total
}

// computeBasisPointsPenalty calculates penalty = (amount * basisPoints) / 10000.
func computeBasisPointsPenalty(amount, basisPoints uint64) uint64 {
	return (amount/10000)*basisPoints + (amount%10000)*basisPoints/10000
}

// CommitmentManager orchestrates the full commitment-reveal flow.
// It tracks commitments, handles reveals, enforces deadlines, and applies penalties.
type CommitmentManager struct {
	mu          sync.RWMutex
	commitments map[commitmentKey]*BuilderCommitment
	window      RevealWindow
	chain       *CommitmentChain
	verifier    *RevealVerifier
	penalties   *CRPenaltyEngine
}

// commitmentKey uniquely identifies a commitment by slot and builder.
type commitmentKey struct {
	Slot         uint64
	BuilderIndex BuilderIndex
}

// NewCommitmentManager creates a new commitment manager.
func NewCommitmentManager(window RevealWindow, penaltyConfig CRPenaltyConfig) *CommitmentManager {
	return &CommitmentManager{
		commitments: make(map[commitmentKey]*BuilderCommitment),
		window:      window,
		chain:       NewCommitmentChain(),
		verifier:    NewRevealVerifier(),
		penalties:   NewCRPenaltyEngine(penaltyConfig),
	}
}

// Commit registers a builder commitment for a slot.
func (cm *CommitmentManager) Commit(c *BuilderCommitment) error {
	if c == nil {
		return ErrCRNilCommitment
	}

	cm.mu.Lock()
	defer cm.mu.Unlock()

	key := commitmentKey{Slot: c.Slot, BuilderIndex: c.BuilderIndex}
	if _, exists := cm.commitments[key]; exists {
		return ErrCRDuplicateCommit
	}

	// Compute commitment hash.
	c.CommitmentHash = computeCommitmentHash(c)
	cm.commitments[key] = c
	cm.chain.Append(c)

	return nil
}

// Reveal processes a payload reveal against a stored commitment.
func (cm *CommitmentManager) Reveal(payload *PayloadEnvelope, currentSlot uint64) error {
	if payload == nil {
		return ErrCRNilPayload
	}

	cm.mu.Lock()
	defer cm.mu.Unlock()

	key := commitmentKey{Slot: payload.Slot, BuilderIndex: payload.BuilderIndex}
	commitment, ok := cm.commitments[key]
	if !ok {
		return ErrCRNoCommitment
	}

	if commitment.Revealed {
		return ErrCRAlreadyRevealed
	}

	// Check deadline.
	if cm.window.IsExpired(commitment.Slot, currentSlot) {
		return ErrCRDeadlinePassed
	}

	// Verify payload matches commitment.
	if err := cm.verifier.Verify(commitment, payload); err != nil {
		cm.penalties.PenalizeMismatch(commitment, err.Error())
		return err
	}

	commitment.Revealed = true
	commitment.RevealedAt = currentSlot

	return nil
}

// CheckDeadlines checks all unrevealed commitments and penalizes those past deadline.
func (cm *CommitmentManager) CheckDeadlines(currentSlot uint64) []*CRPenaltyRecord {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	var penalties []*CRPenaltyRecord
	for _, c := range cm.commitments {
		if !c.Revealed && cm.window.IsExpired(c.Slot, currentSlot) {
			record, err := cm.penalties.PenalizeNonReveal(c)
			if err == nil {
				penalties = append(penalties, record)
			}
			// Mark as revealed to avoid double-penalizing.
			c.Revealed = true
		}
	}
	return penalties
}

// GetCommitment retrieves a commitment by slot and builder.
func (cm *CommitmentManager) GetCommitment(slot uint64, builder BuilderIndex) (*BuilderCommitment, bool) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	key := commitmentKey{Slot: slot, BuilderIndex: builder}
	c, ok := cm.commitments[key]
	return c, ok
}

// CommitmentCount returns the total number of tracked commitments.
func (cm *CommitmentManager) CommitmentCount() int {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return len(cm.commitments)
}

// PenaltyRecords returns all penalty records.
func (cm *CommitmentManager) PenaltyRecords() []*CRPenaltyRecord {
	return cm.penalties.Records()
}

// computeCommitmentHash computes a hash for the commitment.
func computeCommitmentHash(c *BuilderCommitment) types.Hash {
	return crypto.Keccak256Hash(
		c.BlockRoot[:],
		c.BuilderAddr[:],
		[]byte(fmt.Sprintf("%d:%d:%d", c.Slot, c.BuilderIndex, c.BidAmount)),
	)
}
