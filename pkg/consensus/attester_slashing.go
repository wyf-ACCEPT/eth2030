// Package consensus - attester slashing detection, validation, and penalty
// processing per the Ethereum beacon chain spec (phase0).
package consensus

import (
	"errors"
	"sort"
	"sync"

	"github.com/eth2030/eth2030/core/types"
)

// Attester slashing constants.
const (
	// MaxAttesterSlashingsPerPool is the maximum pending attester
	// slashings the pool retains.
	MaxAttesterSlashingsPerPool = 512

	// AttesterSlashingPenaltyQuotient is the initial penalty divisor:
	// effective_balance / 32, same as proposer slashing.
	AttesterSlashingPenaltyQuotient uint64 = 32

	// AttesterWhistleblowerQuotient is the whistleblower reward divisor
	// for attester slashings: effective_balance / 512.
	AttesterWhistleblowerQuotient uint64 = 512
)

// Attester slashing errors.
var (
	ErrASNilEvidence    = errors.New("attester_slashing: nil evidence")
	ErrASEmptyIndices   = errors.New("attester_slashing: empty attesting indices")
	ErrASNoIntersection = errors.New("attester_slashing: no intersecting validator indices")
	ErrASNotSlashable   = errors.New("attester_slashing: attestations are not slashable")
	ErrASAlreadySlashed = errors.New("attester_slashing: validator already slashed")
	ErrASDuplicate      = errors.New("attester_slashing: duplicate slashing evidence")
	ErrASInvalidIndices = errors.New("attester_slashing: attesting indices not sorted unique")
)

// SlashableVoteType categorizes the slashable attestation violation.
type SlashableVoteType uint8

const (
	// DoubleVote: same target epoch, different target root.
	DoubleVote SlashableVoteType = iota
	// SurroundVote: one attestation's source/target range encloses the other's.
	SurroundVote
)

// String returns a human-readable name for the slashable vote type.
func (t SlashableVoteType) String() string {
	switch t {
	case DoubleVote:
		return "double_vote"
	case SurroundVote:
		return "surround_vote"
	default:
		return "unknown"
	}
}

// SlashableAttestationData holds the data from an indexed attestation
// relevant for slashing analysis: source and target epochs/roots.
type SlashableAttestationData struct {
	SourceEpoch Epoch
	SourceRoot  types.Hash
	TargetEpoch Epoch
	TargetRoot  types.Hash
}

// AttesterSlashingRecord contains a complete attester slashing: two
// conflicting indexed attestations and the set of validators that are
// slashable as a result.
type AttesterSlashingRecord struct {
	Attestation1     BlockIndexedAttestation
	Attestation2     BlockIndexedAttestation
	VoteType         SlashableVoteType
	SlashableIndices []ValidatorIndex // intersection of attesting indices
	DetectedAt       Slot
}

// IsDoubleVote checks if two attestation data entries constitute a double
// vote: same target epoch but different data.
func IsDoubleVote(a1, a2 *SlashableAttestationData) bool {
	if a1.TargetEpoch != a2.TargetEpoch {
		return false
	}
	// Same target epoch, different root = double vote.
	return a1.TargetRoot != a2.TargetRoot ||
		a1.SourceEpoch != a2.SourceEpoch ||
		a1.SourceRoot != a2.SourceRoot
}

// IsSurroundVote checks if a1 surrounds a2 or vice versa.
func IsSurroundVote(a1, a2 *SlashableAttestationData) bool {
	// a1 surrounds a2
	if a1.SourceEpoch < a2.SourceEpoch && a2.TargetEpoch < a1.TargetEpoch {
		return true
	}
	// a2 surrounds a1
	if a2.SourceEpoch < a1.SourceEpoch && a1.TargetEpoch < a2.TargetEpoch {
		return true
	}
	return false
}

// IsSlashableAttestationPair checks if two attestation data entries form
// any slashable condition and returns the violation type.
func IsSlashableAttestationPair(a1, a2 *SlashableAttestationData) (SlashableVoteType, bool) {
	if IsDoubleVote(a1, a2) {
		return DoubleVote, true
	}
	if IsSurroundVote(a1, a2) {
		return SurroundVote, true
	}
	return 0, false
}

// IntersectValidatorIndices computes the intersection of two sorted unique
// slices of validator indices.
func IntersectValidatorIndices(a, b []ValidatorIndex) []ValidatorIndex {
	var result []ValidatorIndex
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		if a[i] == b[j] {
			result = append(result, a[i])
			i++
			j++
		} else if a[i] < b[j] {
			i++
		} else {
			j++
		}
	}
	return result
}

// IsSortedUnique checks that indices are sorted ascending with no duplicates.
func IsSortedUnique(indices []ValidatorIndex) bool {
	for i := 1; i < len(indices); i++ {
		if indices[i] <= indices[i-1] {
			return false
		}
	}
	return true
}

// ValidateAttesterSlashing checks validity: non-empty sorted indices,
// slashable pair, non-empty intersection, and at least one slashable validator.
func ValidateAttesterSlashing(
	record *AttesterSlashingRecord,
	validators []*ValidatorV2,
	currentEpoch Epoch,
) error {
	if record == nil {
		return ErrASNilEvidence
	}

	att1 := &record.Attestation1
	att2 := &record.Attestation2

	// Both must have non-empty attesting indices.
	if len(att1.AttestingIndices) == 0 || len(att2.AttestingIndices) == 0 {
		return ErrASEmptyIndices
	}

	// Indices must be sorted and unique.
	if !IsSortedUnique(att1.AttestingIndices) || !IsSortedUnique(att2.AttestingIndices) {
		return ErrASInvalidIndices
	}

	// Check that the attestations form a slashable pair.
	data1 := &SlashableAttestationData{
		SourceEpoch: att1.Data.Source.Epoch,
		SourceRoot:  att1.Data.Source.Root,
		TargetEpoch: att1.Data.Target.Epoch,
		TargetRoot:  att1.Data.Target.Root,
	}
	data2 := &SlashableAttestationData{
		SourceEpoch: att2.Data.Source.Epoch,
		SourceRoot:  att2.Data.Source.Root,
		TargetEpoch: att2.Data.Target.Epoch,
		TargetRoot:  att2.Data.Target.Root,
	}

	_, slashable := IsSlashableAttestationPair(data1, data2)
	if !slashable {
		return ErrASNotSlashable
	}

	// Compute intersection of attesting indices.
	intersection := IntersectValidatorIndices(att1.AttestingIndices, att2.AttestingIndices)
	if len(intersection) == 0 {
		return ErrASNoIntersection
	}

	// At least one validator in the intersection must be slashable.
	hasSlashable := false
	for _, idx := range intersection {
		vi := uint64(idx)
		if vi < uint64(len(validators)) && validators[vi].IsSlashableV2(currentEpoch) {
			hasSlashable = true
			break
		}
	}
	if !hasSlashable {
		return ErrASAlreadySlashed
	}

	return nil
}

// AttesterSlashingPenalty holds the penalty breakdown for a single slashing.
type AttesterSlashingPenalty struct {
	ValidatorIndex   ValidatorIndex
	EffectiveBalance uint64
	Penalty          uint64
	WhistleblowerRwd uint64
	ProposerRwd      uint64
}

// ComputeAttesterSlashingPenalties computes penalties for each slashable
// validator in an attester slashing.
func ComputeAttesterSlashingPenalties(
	slashableIndices []ValidatorIndex,
	validators []*ValidatorV2,
) []*AttesterSlashingPenalty {
	var penalties []*AttesterSlashingPenalty
	for _, idx := range slashableIndices {
		vi := uint64(idx)
		if vi >= uint64(len(validators)) {
			continue
		}
		v := validators[vi]
		if v.Slashed {
			continue // already slashed, skip
		}
		penalty := v.EffectiveBalance / AttesterSlashingPenaltyQuotient
		whistleblowerRwd := v.EffectiveBalance / AttesterWhistleblowerQuotient
		proposerRwd := whistleblowerRwd / 8

		penalties = append(penalties, &AttesterSlashingPenalty{
			ValidatorIndex:   idx,
			EffectiveBalance: v.EffectiveBalance,
			Penalty:          penalty,
			WhistleblowerRwd: whistleblowerRwd,
			ProposerRwd:      proposerRwd,
		})
	}
	return penalties
}

// attesterSlashingKey uniquely identifies a slashing for deduplication.
type attesterSlashingKey struct {
	voteType   SlashableVoteType
	att1Source Epoch
	att1Target Epoch
	att2Source Epoch
	att2Target Epoch
}

// AttesterSlashingPool manages pending attester slashings with surround
// vote and double vote detection via attestation history. Thread-safe.
type AttesterSlashingPool struct {
	mu sync.RWMutex

	pending       []*AttesterSlashingRecord
	slashedSet    map[ValidatorIndex]bool
	history       map[ValidatorIndex][]SlashableAttestationData
	seen          map[attesterSlashingKey]bool
	historyWindow uint64
}

// NewAttesterSlashingPool creates a new attester slashing pool.
func NewAttesterSlashingPool(historyWindow uint64) *AttesterSlashingPool {
	if historyWindow == 0 {
		historyWindow = DefaultAttestationWindow
	}
	return &AttesterSlashingPool{
		pending:       make([]*AttesterSlashingRecord, 0),
		slashedSet:    make(map[ValidatorIndex]bool),
		history:       make(map[ValidatorIndex][]SlashableAttestationData),
		seen:          make(map[attesterSlashingKey]bool),
		historyWindow: historyWindow,
	}
}

// RecordAttestation records an attestation and checks for slashable
// conditions against history. Returns newly detected slashing records.
func (p *AttesterSlashingPool) RecordAttestation(
	attestingIndices []ValidatorIndex,
	data SlashableAttestationData,
	currentSlot Slot,
) []*AttesterSlashingRecord {
	p.mu.Lock()
	defer p.mu.Unlock()

	var detected []*AttesterSlashingRecord

	for _, idx := range attestingIndices {
		if p.slashedSet[idx] {
			continue
		}

		history := p.history[idx]
		for _, prev := range history {
			voteType, isSlashable := IsSlashableAttestationPair(&prev, &data)
			if !isSlashable {
				continue
			}

			key := attesterSlashingKey{
				voteType:   voteType,
				att1Source: prev.SourceEpoch,
				att1Target: prev.TargetEpoch,
				att2Source: data.SourceEpoch,
				att2Target: data.TargetEpoch,
			}
			if p.seen[key] {
				continue
			}

			record := &AttesterSlashingRecord{
				Attestation1: BlockIndexedAttestation{
					AttestingIndices: []ValidatorIndex{idx},
					Data: AttestationData{
						Source: Checkpoint{Epoch: prev.SourceEpoch, Root: prev.SourceRoot},
						Target: Checkpoint{Epoch: prev.TargetEpoch, Root: prev.TargetRoot},
					},
				},
				Attestation2: BlockIndexedAttestation{
					AttestingIndices: []ValidatorIndex{idx},
					Data: AttestationData{
						Source: Checkpoint{Epoch: data.SourceEpoch, Root: data.SourceRoot},
						Target: Checkpoint{Epoch: data.TargetEpoch, Root: data.TargetRoot},
					},
				},
				VoteType:         voteType,
				SlashableIndices: []ValidatorIndex{idx},
				DetectedAt:       currentSlot,
			}

			if len(p.pending) < MaxAttesterSlashingsPerPool {
				p.pending = append(p.pending, record)
				p.seen[key] = true
				detected = append(detected, record)
			}
		}

		// Add to history.
		p.history[idx] = append(p.history[idx], data)

		// Prune old history entries for this validator.
		p.pruneHistoryLocked(idx, data.TargetEpoch)
	}

	return detected
}

// AddEvidence manually adds a pre-constructed attester slashing record.
func (p *AttesterSlashingPool) AddEvidence(record *AttesterSlashingRecord) error {
	if record == nil {
		return ErrASNilEvidence
	}
	if len(record.SlashableIndices) == 0 {
		return ErrASEmptyIndices
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	key := p.makeKey(record)
	if p.seen[key] {
		return ErrASDuplicate
	}
	if len(p.pending) >= MaxAttesterSlashingsPerPool {
		delete(p.seen, p.makeKey(p.pending[0]))
		p.pending = p.pending[1:]
	}
	p.pending = append(p.pending, record)
	p.seen[key] = true
	return nil
}

func (p *AttesterSlashingPool) makeKey(r *AttesterSlashingRecord) attesterSlashingKey {
	return attesterSlashingKey{
		voteType:   r.VoteType,
		att1Source: r.Attestation1.Data.Source.Epoch,
		att1Target: r.Attestation1.Data.Target.Epoch,
		att2Source: r.Attestation2.Data.Source.Epoch,
		att2Target: r.Attestation2.Data.Target.Epoch,
	}
}

// GetPending returns up to maxCount pending attester slashing records.
func (p *AttesterSlashingPool) GetPending(maxCount int) []*AttesterSlashingRecord {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if maxCount <= 0 || maxCount > MaxAttesterSlashings {
		maxCount = MaxAttesterSlashings
	}
	count := len(p.pending)
	if count > maxCount {
		count = maxCount
	}
	result := make([]*AttesterSlashingRecord, count)
	copy(result, p.pending[:count])
	return result
}

// MarkSlashed records that a validator has been slashed.
func (p *AttesterSlashingPool) MarkSlashed(idx ValidatorIndex) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.slashedSet[idx] = true
}

// MarkIncluded removes a slashing from the pool by marking validators.
func (p *AttesterSlashingPool) MarkIncluded(indices []ValidatorIndex) {
	p.mu.Lock()
	defer p.mu.Unlock()
	removeSet := make(map[ValidatorIndex]bool, len(indices))
	for _, idx := range indices {
		removeSet[idx] = true
		p.slashedSet[idx] = true
	}
	n := 0
	for _, rec := range p.pending {
		allDone := true
		for _, idx := range rec.SlashableIndices {
			if !removeSet[idx] {
				allDone = false
				break
			}
		}
		if !allDone {
			p.pending[n] = rec
			n++
		}
	}
	p.pending = p.pending[:n]
}

// PendingCount returns the number of pending attester slashings.
func (p *AttesterSlashingPool) PendingCount() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.pending)
}

// SlashedCount returns the number of validators marked as slashed.
func (p *AttesterSlashingPool) SlashedCount() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.slashedSet)
}

// TrackedValidators returns a sorted list of validator indices with history.
func (p *AttesterSlashingPool) TrackedValidators() []ValidatorIndex {
	p.mu.RLock()
	defer p.mu.RUnlock()
	indices := make([]ValidatorIndex, 0, len(p.history))
	for idx := range p.history {
		indices = append(indices, idx)
	}
	sort.Slice(indices, func(i, j int) bool { return indices[i] < indices[j] })
	return indices
}

// HistorySize returns total attestation records across all validators.
func (p *AttesterSlashingPool) HistorySize() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	total := 0
	for _, h := range p.history {
		total += len(h)
	}
	return total
}

// PruneHistory removes history older than the given cutoff epoch.
func (p *AttesterSlashingPool) PruneHistory(cutoffEpoch Epoch) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for idx := range p.history {
		p.pruneHistoryForEpochLocked(idx, cutoffEpoch)
	}
}

// pruneHistoryLocked prunes old history for a single validator.
func (p *AttesterSlashingPool) pruneHistoryLocked(idx ValidatorIndex, currentTarget Epoch) {
	if p.historyWindow == 0 {
		return
	}
	var cutoff Epoch
	if uint64(currentTarget) > p.historyWindow {
		cutoff = Epoch(uint64(currentTarget) - p.historyWindow)
	}
	p.pruneHistoryForEpochLocked(idx, cutoff)
}

func (p *AttesterSlashingPool) pruneHistoryForEpochLocked(idx ValidatorIndex, cutoff Epoch) {
	history := p.history[idx]
	n := 0
	for _, h := range history {
		if h.TargetEpoch >= cutoff {
			history[n] = h
			n++
		}
	}
	if n == 0 {
		delete(p.history, idx)
	} else {
		p.history[idx] = history[:n]
	}
}
