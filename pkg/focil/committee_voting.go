// committee_voting.go implements IL committee management extensions for
// voting, submission tracking, and deterministic committee computation.
//
// While committee_tracker.go provides the core committee selection and quorum
// logic, this file adds:
//   - Enhanced committee computation with epoch-aware seed derivation
//   - Per-slot submission tracking with IL hash recording
//   - Missing submitter detection for monitoring and slashing
//   - Committee quorum checking with configurable thresholds
//
// The design follows EIP-7805's committee model where IL_COMMITTEE_SIZE
// validators per slot are chosen pseudo-randomly, and at least 2/3 must
// submit their inclusion lists for a valid quorum.
package focil

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sort"
	"sync"

	"github.com/eth2028/eth2028/core/types"
	"golang.org/x/crypto/sha3"
)

// ValidatorIndex is a validator's position in the beacon state validator list.
type ValidatorIndex = uint64

// Voting tracker errors.
var (
	ErrVotingSlotZero        = errors.New("focil/voting: slot must be > 0")
	ErrVotingNoValidators    = errors.New("focil/voting: no active validators")
	ErrVotingNotMember       = errors.New("focil/voting: validator is not committee member")
	ErrVotingDuplicateSubmit = errors.New("focil/voting: duplicate submission")
	ErrVotingEpochZero       = errors.New("focil/voting: epoch must be > 0")
)

// SlotsPerEpoch is the number of slots in a beacon epoch (Ethereum mainnet).
const SlotsPerEpoch = 32

// SubmissionRecord tracks a single IL submission from a committee member.
type SubmissionRecord struct {
	// ValidatorIndex is the submitting validator's index.
	ValidatorIndex ValidatorIndex

	// Slot is the slot the submission targets.
	Slot uint64

	// ILHash is the keccak256 hash of the submitted inclusion list.
	ILHash types.Hash

	// Timestamp is the unix timestamp when the submission was received.
	Timestamp uint64
}

// CommitteeVotingConfig configures the CommitteeVoting tracker.
type CommitteeVotingConfig struct {
	// CommitteeSize is the number of validators per IL committee.
	CommitteeSize int

	// QuorumNumerator is the quorum fraction numerator (default 2).
	QuorumNumerator int

	// QuorumDenominator is the quorum fraction denominator (default 3).
	QuorumDenominator int

	// SlotsPerEpoch is the number of slots per epoch.
	SlotsPerEpoch uint64
}

// DefaultCommitteeVotingConfig returns sensible production defaults.
func DefaultCommitteeVotingConfig() CommitteeVotingConfig {
	return CommitteeVotingConfig{
		CommitteeSize:     IL_COMMITTEE_SIZE,
		QuorumNumerator:   QuorumNumerator,
		QuorumDenominator: QuorumDenominator,
		SlotsPerEpoch:     SlotsPerEpoch,
	}
}

// CommitteeVoting manages IL committee membership and submission tracking.
// It provides deterministic committee selection and voting/quorum logic.
// All public methods are safe for concurrent use.
type CommitteeVoting struct {
	mu     sync.RWMutex
	config CommitteeVotingConfig

	// validatorCount is the total number of active validators.
	validatorCount uint64

	// submissions maps slot -> validatorIndex -> SubmissionRecord.
	submissions map[uint64]map[ValidatorIndex]*SubmissionRecord

	// committeeCache maps slot -> sorted list of committee member indices.
	committeeCache map[uint64][]ValidatorIndex
}

// NewCommitteeVoting creates a new CommitteeVoting tracker.
func NewCommitteeVoting(config CommitteeVotingConfig, validatorCount uint64) *CommitteeVoting {
	if config.CommitteeSize <= 0 {
		config.CommitteeSize = IL_COMMITTEE_SIZE
	}
	if config.QuorumNumerator <= 0 {
		config.QuorumNumerator = QuorumNumerator
	}
	if config.QuorumDenominator <= 0 {
		config.QuorumDenominator = QuorumDenominator
	}
	if config.SlotsPerEpoch == 0 {
		config.SlotsPerEpoch = SlotsPerEpoch
	}
	return &CommitteeVoting{
		config:         config,
		validatorCount: validatorCount,
		submissions:    make(map[uint64]map[ValidatorIndex]*SubmissionRecord),
		committeeCache: make(map[uint64][]ValidatorIndex),
	}
}

// ComputeILCommittee deterministically selects the IL committee for a given
// slot using seed-based shuffling. The committee is selected by hashing
// (epoch, slot, seed, counter) to pick unique validator indices.
//
// Parameters:
//   - epoch: the beacon epoch
//   - slot: the specific slot within the epoch
//   - seed: a 32-byte random seed (typically the RANDAO reveal for the epoch)
//   - validatorCount: total active validators to select from
//
// Returns the sorted list of selected validator indices.
func (cv *CommitteeVoting) ComputeILCommittee(epoch uint64, slot uint64, seed [32]byte, validatorCount uint64) ([]ValidatorIndex, error) {
	if slot == 0 {
		return nil, ErrVotingSlotZero
	}
	if epoch == 0 {
		return nil, ErrVotingEpochZero
	}
	if validatorCount == 0 {
		return nil, ErrVotingNoValidators
	}

	cv.mu.RLock()
	if cached, ok := cv.committeeCache[slot]; ok {
		cv.mu.RUnlock()
		return cached, nil
	}
	cv.mu.RUnlock()

	size := cv.config.CommitteeSize
	if uint64(size) > validatorCount {
		size = int(validatorCount)
	}

	// Build a domain-separated seed: H(seed || epoch || slot).
	domainSeed := computeVotingSeed(seed, epoch, slot)

	// Select committee members using hash-chain shuffle.
	selected := make([]ValidatorIndex, 0, size)
	used := make(map[uint64]bool, size)

	for counter := uint64(0); len(selected) < size; counter++ {
		h := sha3.NewLegacyKeccak256()
		h.Write(domainSeed[:])
		var cBuf [8]byte
		binary.LittleEndian.PutUint64(cBuf[:], counter)
		h.Write(cBuf[:])
		digest := h.Sum(nil)

		idx := binary.LittleEndian.Uint64(digest[:8]) % validatorCount
		if !used[idx] {
			used[idx] = true
			selected = append(selected, idx)
		}
	}

	sort.Slice(selected, func(i, j int) bool { return selected[i] < selected[j] })

	// Cache the result.
	cv.mu.Lock()
	cv.committeeCache[slot] = selected
	cv.mu.Unlock()

	return selected, nil
}

// computeVotingSeed generates a domain-separated seed for committee selection.
// seed = keccak256(baseSeed || epoch || slot)
func computeVotingSeed(baseSeed [32]byte, epoch, slot uint64) [32]byte {
	h := sha3.NewLegacyKeccak256()
	h.Write(baseSeed[:])
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], epoch)
	h.Write(buf[:])
	binary.LittleEndian.PutUint64(buf[:], slot)
	h.Write(buf[:])
	var result [32]byte
	h.Sum(result[:0])
	return result
}

// IsILCommitteeMember checks whether a validator is in the IL committee for
// the given slot. The committee must have been previously computed via
// ComputeILCommittee, or this returns false.
func (cv *CommitteeVoting) IsILCommitteeMember(validatorIndex ValidatorIndex, slot uint64) bool {
	cv.mu.RLock()
	defer cv.mu.RUnlock()

	members, ok := cv.committeeCache[slot]
	if !ok {
		return false
	}
	// Binary search since members are sorted.
	i := sort.Search(len(members), func(i int) bool { return members[i] >= validatorIndex })
	return i < len(members) && members[i] == validatorIndex
}

// TrackSubmission records that a committee member submitted an inclusion list
// for a given slot. Returns an error if the validator is not a committee
// member or has already submitted.
func (cv *CommitteeVoting) TrackSubmission(validatorIndex ValidatorIndex, slot uint64, ilHash types.Hash) error {
	if slot == 0 {
		return ErrVotingSlotZero
	}

	cv.mu.RLock()
	members, hasCached := cv.committeeCache[slot]
	cv.mu.RUnlock()

	// Verify committee membership if we have the committee cached.
	if hasCached {
		isMember := false
		for _, m := range members {
			if m == validatorIndex {
				isMember = true
				break
			}
		}
		if !isMember {
			return fmt.Errorf("%w: validator %d at slot %d",
				ErrVotingNotMember, validatorIndex, slot)
		}
	}

	cv.mu.Lock()
	defer cv.mu.Unlock()

	if cv.submissions[slot] == nil {
		cv.submissions[slot] = make(map[ValidatorIndex]*SubmissionRecord)
	}
	if _, exists := cv.submissions[slot][validatorIndex]; exists {
		return fmt.Errorf("%w: validator %d at slot %d",
			ErrVotingDuplicateSubmit, validatorIndex, slot)
	}

	cv.submissions[slot][validatorIndex] = &SubmissionRecord{
		ValidatorIndex: validatorIndex,
		Slot:           slot,
		ILHash:         ilHash,
	}
	return nil
}

// GetMissingSubmitters returns the list of committee members who have not
// yet submitted their inclusion list for the given slot. The committee
// must have been previously computed.
func (cv *CommitteeVoting) GetMissingSubmitters(slot uint64) []ValidatorIndex {
	cv.mu.RLock()
	defer cv.mu.RUnlock()

	members, ok := cv.committeeCache[slot]
	if !ok {
		return nil
	}

	submitted := cv.submissions[slot]
	var missing []ValidatorIndex
	for _, m := range members {
		if submitted == nil || submitted[m] == nil {
			missing = append(missing, m)
		}
	}
	return missing
}

// GetSubmissions returns all submission records for a given slot.
func (cv *CommitteeVoting) GetSubmissions(slot uint64) []*SubmissionRecord {
	cv.mu.RLock()
	defer cv.mu.RUnlock()

	subs := cv.submissions[slot]
	if len(subs) == 0 {
		return nil
	}

	result := make([]*SubmissionRecord, 0, len(subs))
	for _, r := range subs {
		cp := *r
		result = append(result, &cp)
	}

	// Sort by validator index for determinism.
	sort.Slice(result, func(i, j int) bool {
		return result[i].ValidatorIndex < result[j].ValidatorIndex
	})
	return result
}

// CommitteeQuorum checks whether enough inclusion lists have been received
// for the given slot to satisfy the quorum threshold (default 2/3).
func (cv *CommitteeVoting) CommitteeQuorum(slot uint64) bool {
	cv.mu.RLock()
	defer cv.mu.RUnlock()

	members, ok := cv.committeeCache[slot]
	if !ok {
		return false
	}

	threshold := cv.computeQuorum(len(members))
	received := len(cv.submissions[slot])
	return received >= threshold
}

// QuorumDetail returns detailed quorum information for a slot.
func (cv *CommitteeVoting) QuorumDetail(slot uint64) (committeeSize, received, threshold int, reached bool) {
	cv.mu.RLock()
	defer cv.mu.RUnlock()

	members, ok := cv.committeeCache[slot]
	if !ok {
		return 0, 0, 0, false
	}

	committeeSize = len(members)
	threshold = cv.computeQuorum(committeeSize)
	received = len(cv.submissions[slot])
	reached = received >= threshold
	return
}

// computeQuorum calculates ceil(numerator/denominator * size).
func (cv *CommitteeVoting) computeQuorum(committeeSize int) int {
	num := cv.config.QuorumNumerator
	den := cv.config.QuorumDenominator
	return (committeeSize*num + den - 1) / den
}

// SlotToEpoch converts a slot number to its epoch.
func (cv *CommitteeVoting) SlotToEpoch(slot uint64) uint64 {
	if cv.config.SlotsPerEpoch == 0 {
		return 0
	}
	return slot / cv.config.SlotsPerEpoch
}

// PruneBefore removes cached data for slots older than the given slot.
// Returns the number of entries pruned.
func (cv *CommitteeVoting) PruneBefore(slot uint64) int {
	cv.mu.Lock()
	defer cv.mu.Unlock()

	pruned := 0
	for s := range cv.committeeCache {
		if s < slot {
			delete(cv.committeeCache, s)
			pruned++
		}
	}
	for s := range cv.submissions {
		if s < slot {
			delete(cv.submissions, s)
		}
	}
	return pruned
}
