// slashing.go implements builder slashing conditions for ePBS.
//
// In enshrined PBS, builders who commit bids but fail to fulfill their
// obligations are subject to slashing penalties. Three primary slashing
// conditions are defined:
//
//   - NonDelivery: builder won the auction but did not reveal a payload
//     within the allowed time window.
//   - InvalidPayload: builder revealed a payload that does not match
//     the committed bid (e.g., block hash mismatch).
//   - Equivocation: builder submitted multiple conflicting bids for
//     the same slot (attempting to manipulate the auction).
//
// The SlashingEngine evaluates all registered conditions against a
// bid-payload pair and produces SlashingRecords with computed penalties.
package epbs

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/crypto"
)

// Slashing errors.
var (
	ErrSlashingNilBid        = errors.New("slashing: nil bid")
	ErrSlashingNilEvidence   = errors.New("slashing: nil evidence")
	ErrSlashingNoConditions  = errors.New("slashing: no conditions registered")
	ErrSlashingNilPayload    = errors.New("slashing: nil payload")
	ErrSlashingInvalidPenalty = errors.New("slashing: invalid penalty multiplier")
)

// SlashingConditionType identifies the type of slashing condition.
type SlashingConditionType string

const (
	SlashNonDelivery    SlashingConditionType = "non_delivery"
	SlashInvalidPayload SlashingConditionType = "invalid_payload"
	SlashEquivocation   SlashingConditionType = "equivocation"
)

// SlashingCondition is the interface for all slashing conditions.
// Implementations check whether a particular violation occurred and
// return a human-readable reason if so.
type SlashingCondition interface {
	// Type returns the condition type identifier.
	Type() SlashingConditionType

	// Check evaluates whether the slashing condition is triggered.
	// It returns (true, reason) if the condition is violated,
	// or (false, "") if no violation occurred.
	Check(bid *BuilderBid, payload *PayloadEnvelope) (violated bool, reason string)
}

// NonDeliverySlashing detects when a builder committed a bid but failed
// to deliver the payload within the specified time window.
type NonDeliverySlashing struct {
	// DeadlineSlots is the number of slots after the bid slot within
	// which the payload must be delivered.
	DeadlineSlots uint64

	// CurrentSlot is the current chain head slot, used to determine
	// whether the deadline has passed.
	CurrentSlot uint64
}

// Type returns SlashNonDelivery.
func (n *NonDeliverySlashing) Type() SlashingConditionType {
	return SlashNonDelivery
}

// Check evaluates the non-delivery condition. If the payload is nil and
// the deadline has passed, the condition is triggered.
func (n *NonDeliverySlashing) Check(bid *BuilderBid, payload *PayloadEnvelope) (bool, string) {
	if bid == nil {
		return false, ""
	}

	// If a payload was delivered, non-delivery does not apply.
	if payload != nil {
		return false, ""
	}

	deadline := bid.Slot + n.DeadlineSlots
	if n.CurrentSlot > deadline {
		return true, fmt.Sprintf("payload not delivered by slot %d (deadline %d, current %d)",
			bid.Slot, deadline, n.CurrentSlot)
	}
	return false, ""
}

// InvalidPayloadSlashing detects when a builder delivered a payload that
// does not match the committed bid. It checks that the payload's builder
// index and slot match the bid.
type InvalidPayloadSlashing struct{}

// Type returns SlashInvalidPayload.
func (ip *InvalidPayloadSlashing) Type() SlashingConditionType {
	return SlashInvalidPayload
}

// Check evaluates the invalid payload condition by verifying consistency
// between the bid and the delivered payload envelope.
func (ip *InvalidPayloadSlashing) Check(bid *BuilderBid, payload *PayloadEnvelope) (bool, string) {
	if bid == nil || payload == nil {
		return false, ""
	}

	// Check slot mismatch.
	if bid.Slot != payload.Slot {
		return true, fmt.Sprintf("slot mismatch: bid=%d, payload=%d",
			bid.Slot, payload.Slot)
	}

	// Check builder index mismatch.
	if bid.BuilderIndex != payload.BuilderIndex {
		return true, fmt.Sprintf("builder mismatch: bid=%d, payload=%d",
			bid.BuilderIndex, payload.BuilderIndex)
	}

	// Check block hash: the bid's block hash should match the payload root
	// (the payload root is the commitment to the execution payload content).
	if bid.BlockHash != payload.PayloadRoot {
		return true, fmt.Sprintf("block hash mismatch: bid=%s, payload=%s",
			bid.BlockHash.Hex(), payload.PayloadRoot.Hex())
	}

	return false, ""
}

// EquivocationEvidence holds the two conflicting bids that form the
// equivocation proof.
type EquivocationEvidence struct {
	BidA *BuilderBid
	BidB *BuilderBid
}

// EquivocationSlashing detects when a builder submitted multiple
// conflicting bids for the same slot. Two bids conflict if they are
// for the same slot from the same builder but have different block hashes.
type EquivocationSlashing struct {
	// Evidence holds the conflicting bid pair. Must be set before Check.
	Evidence *EquivocationEvidence
}

// Type returns SlashEquivocation.
func (e *EquivocationSlashing) Type() SlashingConditionType {
	return SlashEquivocation
}

// Check evaluates the equivocation condition. The bid parameter is the
// primary bid; the evidence contains the conflicting bid. Both must be
// for the same slot and builder but with different block hashes.
func (e *EquivocationSlashing) Check(bid *BuilderBid, _ *PayloadEnvelope) (bool, string) {
	if bid == nil || e.Evidence == nil {
		return false, ""
	}
	if e.Evidence.BidA == nil || e.Evidence.BidB == nil {
		return false, ""
	}

	a, b := e.Evidence.BidA, e.Evidence.BidB

	// Must be the same slot.
	if a.Slot != b.Slot {
		return false, ""
	}

	// Must be the same builder.
	if a.BuilderIndex != b.BuilderIndex {
		return false, ""
	}

	// Must have different block hashes (conflicting).
	if a.BlockHash == b.BlockHash {
		return false, ""
	}

	return true, fmt.Sprintf("equivocation at slot %d by builder %d: hash_a=%s, hash_b=%s",
		a.Slot, a.BuilderIndex, a.BlockHash.Hex(), b.BlockHash.Hex())
}

// PenaltyMultipliers defines the penalty multiplier for each condition type.
// The penalty is: bidValue * multiplier (in basis points / 10000).
type PenaltyMultipliers struct {
	// NonDelivery penalty as basis points of bid value (e.g., 20000 = 2x).
	NonDelivery uint64

	// InvalidPayload penalty as basis points of bid value.
	InvalidPayload uint64

	// Equivocation penalty as basis points of bid value.
	Equivocation uint64
}

// DefaultPenaltyMultipliers returns default penalty multipliers.
func DefaultPenaltyMultipliers() PenaltyMultipliers {
	return PenaltyMultipliers{
		NonDelivery:    20000, // 2x bid value
		InvalidPayload: 30000, // 3x bid value
		Equivocation:   50000, // 5x bid value
	}
}

// ComputePenalty calculates the penalty amount in Gwei for a given
// slashing condition and bid value. The penalty is:
//
//	penalty = (bidValue * multiplier) / 10000
//
// where multiplier is in basis points.
func ComputePenalty(condType SlashingConditionType, bidValue uint64, multipliers PenaltyMultipliers) (uint64, error) {
	var mult uint64
	switch condType {
	case SlashNonDelivery:
		mult = multipliers.NonDelivery
	case SlashInvalidPayload:
		mult = multipliers.InvalidPayload
	case SlashEquivocation:
		mult = multipliers.Equivocation
	default:
		return 0, fmt.Errorf("%w: unknown condition %s", ErrSlashingInvalidPenalty, condType)
	}

	// penalty = bidValue * mult / 10000 (integer arithmetic).
	penalty := (bidValue / 10000) * mult
	// Handle remainder to avoid truncation for small values.
	remainder := (bidValue % 10000) * mult / 10000
	return penalty + remainder, nil
}

// SlashingRecord tracks a single slashing event with full evidence.
type SlashingRecord struct {
	BuilderIndex  BuilderIndex          `json:"builderIndex"`
	BuilderAddr   types.Address         `json:"builderAddr"`
	Slot          uint64                `json:"slot"`
	ConditionType SlashingConditionType `json:"conditionType"`
	Reason        string                `json:"reason"`
	BidValue      uint64                `json:"bidValue"`
	PenaltyGwei   uint64                `json:"penaltyGwei"`
	EvidenceHash  types.Hash            `json:"evidenceHash"`
	Timestamp     time.Time             `json:"timestamp"`
}

// ComputeEvidenceHash computes a deterministic hash of the slashing
// evidence for auditing. It hashes the condition type, bid hash, and
// builder address together.
func ComputeEvidenceHash(condType SlashingConditionType, bid *BuilderBid, builderAddr types.Address) types.Hash {
	if bid == nil {
		return types.Hash{}
	}
	bidHash := bid.BidHash()
	return crypto.Keccak256Hash(
		[]byte(condType),
		bidHash[:],
		builderAddr[:],
	)
}

// SlashingEngine evaluates all registered slashing conditions against
// bid-payload pairs and produces SlashingRecords. Thread-safe.
type SlashingEngine struct {
	mu          sync.RWMutex
	conditions  []SlashingCondition
	multipliers PenaltyMultipliers
	records     []*SlashingRecord
	maxRecords  int
}

// NewSlashingEngine creates a new slashing engine with default penalty
// multipliers and the given maximum record retention.
func NewSlashingEngine(multipliers PenaltyMultipliers, maxRecords int) *SlashingEngine {
	if maxRecords <= 0 {
		maxRecords = 1024
	}
	return &SlashingEngine{
		conditions:  make([]SlashingCondition, 0),
		multipliers: multipliers,
		maxRecords:  maxRecords,
	}
}

// RegisterCondition adds a slashing condition to the engine.
func (se *SlashingEngine) RegisterCondition(cond SlashingCondition) {
	se.mu.Lock()
	defer se.mu.Unlock()
	se.conditions = append(se.conditions, cond)
}

// ConditionCount returns the number of registered conditions.
func (se *SlashingEngine) ConditionCount() int {
	se.mu.RLock()
	defer se.mu.RUnlock()
	return len(se.conditions)
}

// EvaluateAll checks all registered conditions against the given bid
// and payload. It returns all triggered SlashingRecords.
func (se *SlashingEngine) EvaluateAll(
	bid *BuilderBid,
	payload *PayloadEnvelope,
	builderAddr types.Address,
) ([]*SlashingRecord, error) {
	if bid == nil {
		return nil, ErrSlashingNilBid
	}

	se.mu.Lock()
	defer se.mu.Unlock()

	if len(se.conditions) == 0 {
		return nil, ErrSlashingNoConditions
	}

	var triggered []*SlashingRecord
	for _, cond := range se.conditions {
		violated, reason := cond.Check(bid, payload)
		if !violated {
			continue
		}

		penalty, err := ComputePenalty(cond.Type(), bid.Value, se.multipliers)
		if err != nil {
			continue
		}

		record := &SlashingRecord{
			BuilderIndex:  bid.BuilderIndex,
			BuilderAddr:   builderAddr,
			Slot:          bid.Slot,
			ConditionType: cond.Type(),
			Reason:        reason,
			BidValue:      bid.Value,
			PenaltyGwei:   penalty,
			EvidenceHash:  ComputeEvidenceHash(cond.Type(), bid, builderAddr),
			Timestamp:     time.Now(),
		}
		triggered = append(triggered, record)
		se.records = append(se.records, record)
	}

	// Trim records if needed.
	if len(se.records) > se.maxRecords {
		se.records = se.records[len(se.records)-se.maxRecords:]
	}

	return triggered, nil
}

// Records returns a copy of all slashing records.
func (se *SlashingEngine) Records() []*SlashingRecord {
	se.mu.RLock()
	defer se.mu.RUnlock()
	result := make([]*SlashingRecord, len(se.records))
	copy(result, se.records)
	return result
}

// RecordCount returns the number of slashing records.
func (se *SlashingEngine) RecordCount() int {
	se.mu.RLock()
	defer se.mu.RUnlock()
	return len(se.records)
}

// RecordsForBuilder returns all slashing records for a specific builder.
func (se *SlashingEngine) RecordsForBuilder(builderAddr types.Address) []*SlashingRecord {
	se.mu.RLock()
	defer se.mu.RUnlock()

	var result []*SlashingRecord
	for _, r := range se.records {
		if r.BuilderAddr == builderAddr {
			result = append(result, r)
		}
	}
	return result
}

// TotalPenaltyForBuilder returns the cumulative penalty (in Gwei)
// across all slashing records for a given builder.
func (se *SlashingEngine) TotalPenaltyForBuilder(builderAddr types.Address) uint64 {
	se.mu.RLock()
	defer se.mu.RUnlock()

	var total uint64
	for _, r := range se.records {
		if r.BuilderAddr == builderAddr {
			total += r.PenaltyGwei
		}
	}
	return total
}
