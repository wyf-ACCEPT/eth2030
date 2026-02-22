// depproc_deposit_processing.go implements deposit handling, voluntary exits,
// attester slashing processing, and proposer slashing processing per the
// Ethereum beacon chain spec.
//
// Provides deposit Merkle proof validation, applying deposits to the validator
// registry (new or top-up), processing voluntary exits with validation, and
// processing attester/proposer slashings with double vote and surround vote
// detection, all operating on BsnBeaconState.
package consensus

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
)

// Deposit processing constants.
const (
	// DepProcDepositContractTreeDepth is the depth of the deposit Merkle tree.
	DepProcDepositContractTreeDepth = 32

	// DepProcMaxDepositsPerBlock is the maximum deposits per block.
	DepProcMaxDepositsPerBlock = 16

	// DepProcMinDepositAmount is the minimum deposit to create a new validator (1 ETH in Gwei).
	DepProcMinDepositAmount uint64 = 1_000_000_000

	// DepProcMaxEffectiveBalance is the max effective balance for deposit processing.
	DepProcMaxEffectiveBalance uint64 = 32_000_000_000

	// DepProcShardCommitteePeriod is the minimum epochs a validator must be active
	// before a voluntary exit is allowed.
	DepProcShardCommitteePeriod uint64 = 256

	// DepProcMinSlashingPenaltyQuotient is the divisor for initial slashing penalty.
	DepProcMinSlashingPenaltyQuotient uint64 = 128

	// DepProcWhistleblowerRewardQuotient is the divisor for whistleblower rewards.
	DepProcWhistleblowerRewardQuotient uint64 = 512

	// DepProcProposerRewardQuotient is the divisor applied to the whistleblower reward
	// to obtain the proposer reward.
	DepProcProposerRewardQuotient uint64 = 8
)

// Deposit processing errors.
var (
	ErrDepProcNilState         = errors.New("depproc: nil beacon state")
	ErrDepProcNilDeposit       = errors.New("depproc: nil deposit")
	ErrDepProcInvalidProof     = errors.New("depproc: invalid Merkle proof")
	ErrDepProcProofLength      = errors.New("depproc: proof length mismatch")
	ErrDepProcDepositOverflow  = errors.New("depproc: deposit index overflow")
	ErrDepProcZeroAmount       = errors.New("depproc: zero deposit amount")
	ErrDepProcVolExitNil       = errors.New("depproc: nil voluntary exit")
	ErrDepProcVolExitInactive  = errors.New("depproc: validator not active for exit")
	ErrDepProcVolExitAlready   = errors.New("depproc: validator already exiting")
	ErrDepProcVolExitTooEarly  = errors.New("depproc: validator has not been active long enough")
	ErrDepProcVolExitFuture    = errors.New("depproc: exit epoch is in the future")
	ErrDepProcVolExitBadIdx    = errors.New("depproc: validator index out of range")
	ErrDepProcASNilRecord      = errors.New("depproc: nil attester slashing record")
	ErrDepProcASNotSlashable   = errors.New("depproc: attestations are not slashable")
	ErrDepProcASNoIntersection = errors.New("depproc: no intersecting slashable indices")
	ErrDepProcPSNilRecord      = errors.New("depproc: nil proposer slashing record")
	ErrDepProcPSSameHeader     = errors.New("depproc: proposer slashing headers are identical")
	ErrDepProcPSDiffSlot       = errors.New("depproc: proposer slashing headers are for different slots")
	ErrDepProcPSNotSlashable   = errors.New("depproc: proposer is not slashable")
	ErrDepProcPSBadIdx         = errors.New("depproc: proposer index out of range")
)

// DepProcDeposit represents a deposit from the execution layer.
type DepProcDeposit struct {
	Proof                 [][32]byte // Merkle proof, length = DepProcDepositContractTreeDepth + 1
	Pubkey                [48]byte
	WithdrawalCredentials [32]byte
	Amount                uint64
	Signature             [96]byte
}

// DepProcVoluntaryExit represents a signed voluntary exit request.
type DepProcVoluntaryExit struct {
	Epoch          Epoch
	ValidatorIndex uint64
	Signature      [96]byte
}

// DepProcAttesterSlashing contains two conflicting indexed attestations.
type DepProcAttesterSlashing struct {
	Attestation1 DepProcIndexedAttestation
	Attestation2 DepProcIndexedAttestation
}

// DepProcIndexedAttestation is an indexed attestation for slashing processing.
type DepProcIndexedAttestation struct {
	AttestingIndices []uint64
	SourceEpoch      Epoch
	TargetEpoch      Epoch
	SourceRoot       [32]byte
	TargetRoot       [32]byte
	Signature        [96]byte
}

// DepProcProposerSlashing contains two conflicting signed block headers.
type DepProcProposerSlashing struct {
	ProposerIndex uint64
	Header1       DepProcSignedHeader
	Header2       DepProcSignedHeader
}

// DepProcSignedHeader is a signed beacon block header for slashing.
type DepProcSignedHeader struct {
	Slot       uint64
	ParentRoot [32]byte
	StateRoot  [32]byte
	BodyRoot   [32]byte
	Signature  [96]byte
}

// DepProcValidateMerkleProof validates a deposit Merkle proof.
// The proof must be DepProcDepositContractTreeDepth + 1 elements long
// and must verify the leaf against the deposit root in the state.
func DepProcValidateMerkleProof(
	leaf [32]byte,
	proof [][32]byte,
	index uint64,
	root [32]byte,
) error {
	expectedLen := DepProcDepositContractTreeDepth + 1
	if len(proof) != expectedLen {
		return fmt.Errorf("%w: got %d, want %d", ErrDepProcProofLength, len(proof), expectedLen)
	}

	value := leaf
	for depth := 0; depth < DepProcDepositContractTreeDepth; depth++ {
		if (index>>depth)&1 == 1 {
			value = depProcSHA256Pair(proof[depth], value)
		} else {
			value = depProcSHA256Pair(value, proof[depth])
		}
	}

	// The last proof element is mixed-in with the deposit count.
	value = depProcSHA256Pair(value, proof[DepProcDepositContractTreeDepth])

	if value != root {
		return ErrDepProcInvalidProof
	}
	return nil
}

// depProcSHA256Pair computes SHA-256 of the concatenation of two 32-byte values.
func depProcSHA256Pair(a, b [32]byte) [32]byte {
	var buf [64]byte
	copy(buf[:32], a[:])
	copy(buf[32:], b[:])
	return sha256.Sum256(buf[:])
}

// depProcDepositLeaf computes the leaf hash for a deposit.
func depProcDepositLeaf(d *DepProcDeposit) [32]byte {
	// Hash the pubkey (48 bytes, padded to 64).
	var pkBuf [64]byte
	copy(pkBuf[:48], d.Pubkey[:])
	pkHash := sha256.Sum256(pkBuf[:])

	// Hash the withdrawal credentials (already 32 bytes, pad to 64).
	var wcBuf [64]byte
	copy(wcBuf[:32], d.WithdrawalCredentials[:])
	wcHash := sha256.Sum256(wcBuf[:])

	// Hash amount (8 bytes LE, padded to 32, then to 64).
	var amtBuf [64]byte
	binary.LittleEndian.PutUint64(amtBuf[:8], d.Amount)
	amtHash := sha256.Sum256(amtBuf[:])

	// Hash signature (96 bytes, split into 3 chunks of 32).
	var sigBuf [128]byte
	copy(sigBuf[:96], d.Signature[:])
	sigHash := sha256.Sum256(sigBuf[:])

	// Combine: hash(hash(pkHash || wcHash) || hash(amtHash || sigHash)).
	left := depProcSHA256Pair(pkHash, wcHash)
	right := depProcSHA256Pair(amtHash, sigHash)
	return depProcSHA256Pair(left, right)
}

// DepProcProcessDeposit processes a single deposit against the beacon state.
// If the pubkey already exists in the validator registry, the deposit is a
// top-up. Otherwise, a new validator entry is created.
func DepProcProcessDeposit(
	state *BsnBeaconState,
	deposit *DepProcDeposit,
) error {
	if state == nil {
		return ErrDepProcNilState
	}
	if deposit == nil {
		return ErrDepProcNilDeposit
	}
	if deposit.Amount == 0 {
		return ErrDepProcZeroAmount
	}

	// Validate Merkle proof against eth1 deposit root.
	if len(deposit.Proof) > 0 {
		leaf := depProcDepositLeaf(deposit)
		if err := DepProcValidateMerkleProof(
			leaf, deposit.Proof, state.Eth1DepositIndex, state.Eth1Data.DepositRoot,
		); err != nil {
			return err
		}
	}

	// Increment deposit index.
	state.Eth1DepositIndex++

	state.mu.Lock()
	defer state.mu.Unlock()

	// Check if validator already exists (by pubkey scan).
	existingIdx := int(-1)
	for i, v := range state.Validators {
		if v.Pubkey == deposit.Pubkey {
			existingIdx = i
			break
		}
	}

	if existingIdx >= 0 {
		// Top-up existing validator.
		state.Balances[existingIdx] += deposit.Amount
	} else {
		// New validator entry.
		effBalance := deposit.Amount - (deposit.Amount % EffectiveBalanceIncrement)
		if effBalance > DepProcMaxEffectiveBalance {
			effBalance = DepProcMaxEffectiveBalance
		}
		v := &ValidatorV2{
			Pubkey:                     deposit.Pubkey,
			WithdrawalCredentials:      deposit.WithdrawalCredentials,
			EffectiveBalance:           effBalance,
			Slashed:                    false,
			ActivationEligibilityEpoch: FarFutureEpoch,
			ActivationEpoch:            FarFutureEpoch,
			ExitEpoch:                  FarFutureEpoch,
			WithdrawableEpoch:          FarFutureEpoch,
		}
		state.Validators = append(state.Validators, v)
		state.Balances = append(state.Balances, deposit.Amount)
		state.PreviousEpochParticipation = append(state.PreviousEpochParticipation, 0)
		state.CurrentEpochParticipation = append(state.CurrentEpochParticipation, 0)
		state.InactivityScores = append(state.InactivityScores, 0)
	}

	return nil
}

// DepProcProcessVoluntaryExit processes a voluntary exit request.
// Validates that the validator is active, not already exiting, and has
// been active for the shard committee period.
func DepProcProcessVoluntaryExit(
	state *BsnBeaconState,
	exit *DepProcVoluntaryExit,
) error {
	if state == nil {
		return ErrDepProcNilState
	}
	if exit == nil {
		return ErrDepProcVolExitNil
	}

	state.mu.Lock()
	defer state.mu.Unlock()

	idx := exit.ValidatorIndex
	if idx >= uint64(len(state.Validators)) {
		return ErrDepProcVolExitBadIdx
	}

	v := state.Validators[idx]
	currentEpoch := state.BsnGetCurrentEpoch()

	// The exit epoch must not be in the future relative to current epoch.
	if exit.Epoch > currentEpoch {
		return ErrDepProcVolExitFuture
	}

	// Validator must be active.
	if !v.IsActiveV2(currentEpoch) {
		return ErrDepProcVolExitInactive
	}

	// Validator must not already be exiting.
	if v.ExitEpoch != FarFutureEpoch {
		return ErrDepProcVolExitAlready
	}

	// Validator must have been active for at least SHARD_COMMITTEE_PERIOD.
	if currentEpoch < v.ActivationEpoch+Epoch(DepProcShardCommitteePeriod) {
		return ErrDepProcVolExitTooEarly
	}

	// Signature verification placeholder: in production, verify BLS signature
	// against the validator's pubkey.

	// Initiate exit.
	depProcInitiateExit(state, idx, currentEpoch)

	return nil
}

// depProcInitiateExit computes the exit queue epoch and assigns exit/withdrawable epochs.
// Must be called with state.mu held.
func depProcInitiateExit(state *BsnBeaconState, validatorIdx uint64, currentEpoch Epoch) {
	exitQueueEpoch := Epoch(uint64(currentEpoch) + 1 + MaxSeedLookahead)
	exitQueueChurn := uint64(0)

	for _, v := range state.Validators {
		if v.ExitEpoch != FarFutureEpoch {
			if v.ExitEpoch > exitQueueEpoch {
				exitQueueEpoch = v.ExitEpoch
				exitQueueChurn = 1
			} else if v.ExitEpoch == exitQueueEpoch {
				exitQueueChurn++
			}
		}
	}

	// Compute churn limit.
	var activeCount uint64
	for _, v := range state.Validators {
		if v.IsActiveV2(currentEpoch) {
			activeCount++
		}
	}
	churn := activeCount / ChurnLimitQuotient
	if churn < MinPerEpochChurnLimit {
		churn = MinPerEpochChurnLimit
	}
	if exitQueueChurn >= churn {
		exitQueueEpoch++
	}

	state.Validators[validatorIdx].ExitEpoch = exitQueueEpoch
	state.Validators[validatorIdx].WithdrawableEpoch = Epoch(
		uint64(exitQueueEpoch) + MinValidatorWithdrawDelay,
	)
}

// DepProcProcessAttesterSlashing processes an attester slashing by validating
// the double vote or surround vote condition and slashing applicable validators.
// Returns the list of slashed validator indices and total penalties.
func DepProcProcessAttesterSlashing(
	state *BsnBeaconState,
	slashing *DepProcAttesterSlashing,
	proposerIndex uint64,
) ([]uint64, uint64, error) {
	if state == nil {
		return nil, 0, ErrDepProcNilState
	}
	if slashing == nil {
		return nil, 0, ErrDepProcASNilRecord
	}

	att1 := &slashing.Attestation1
	att2 := &slashing.Attestation2

	// Check slashable conditions.
	isDouble := att1.TargetEpoch == att2.TargetEpoch &&
		(att1.TargetRoot != att2.TargetRoot ||
			att1.SourceEpoch != att2.SourceEpoch ||
			att1.SourceRoot != att2.SourceRoot)

	isSurround := (att1.SourceEpoch < att2.SourceEpoch && att2.TargetEpoch < att1.TargetEpoch) ||
		(att2.SourceEpoch < att1.SourceEpoch && att1.TargetEpoch < att2.TargetEpoch)

	if !isDouble && !isSurround {
		return nil, 0, ErrDepProcASNotSlashable
	}

	// Find intersection of attesting indices.
	intersection := depProcIntersect(att1.AttestingIndices, att2.AttestingIndices)
	if len(intersection) == 0 {
		return nil, 0, ErrDepProcASNoIntersection
	}

	state.mu.Lock()
	defer state.mu.Unlock()

	currentEpoch := state.BsnGetCurrentEpoch()
	var slashedIndices []uint64
	var totalPenalty uint64

	for _, idx := range intersection {
		if idx >= uint64(len(state.Validators)) {
			continue
		}
		v := state.Validators[idx]
		if !v.IsSlashableV2(currentEpoch) {
			continue
		}

		// Slash the validator.
		v.Slashed = true

		// Set exit epoch if not already exiting.
		if v.ExitEpoch == FarFutureEpoch {
			exitEpoch := Epoch(uint64(currentEpoch) + 1 + MaxSeedLookahead)
			v.ExitEpoch = exitEpoch
			v.WithdrawableEpoch = Epoch(uint64(exitEpoch) + MinValidatorWithdrawDelay)
		}

		// Extend withdrawable epoch for slashing.
		slashWithdrawable := Epoch(uint64(currentEpoch) + BsnEpochsPerSlashingsVector)
		if slashWithdrawable > v.WithdrawableEpoch {
			v.WithdrawableEpoch = slashWithdrawable
		}

		// Record in slashings accumulator.
		slashingsIdx := uint64(currentEpoch) % BsnEpochsPerSlashingsVector
		state.Slashings[slashingsIdx] += v.EffectiveBalance

		// Apply initial penalty.
		penalty := v.EffectiveBalance / DepProcMinSlashingPenaltyQuotient
		if idx < uint64(len(state.Balances)) {
			if penalty > state.Balances[idx] {
				state.Balances[idx] = 0
			} else {
				state.Balances[idx] -= penalty
			}
		}
		totalPenalty += penalty

		// Whistleblower reward.
		whistleblowerReward := v.EffectiveBalance / DepProcWhistleblowerRewardQuotient
		proposerReward := whistleblowerReward / DepProcProposerRewardQuotient
		if proposerIndex < uint64(len(state.Balances)) {
			state.Balances[proposerIndex] += proposerReward
		}

		slashedIndices = append(slashedIndices, idx)
	}

	if len(slashedIndices) == 0 {
		return nil, 0, ErrDepProcASNoIntersection
	}

	return slashedIndices, totalPenalty, nil
}

// depProcIntersect computes the intersection of two sorted uint64 slices.
func depProcIntersect(a, b []uint64) []uint64 {
	// Sort both slices first (they should already be sorted per spec,
	// but we handle unsorted gracefully).
	sa := depProcSortUnique(a)
	sb := depProcSortUnique(b)

	var result []uint64
	i, j := 0, 0
	for i < len(sa) && j < len(sb) {
		if sa[i] == sb[j] {
			result = append(result, sa[i])
			i++
			j++
		} else if sa[i] < sb[j] {
			i++
		} else {
			j++
		}
	}
	return result
}

// depProcSortUnique returns a sorted, deduplicated copy of the input.
func depProcSortUnique(in []uint64) []uint64 {
	if len(in) == 0 {
		return nil
	}
	out := make([]uint64, len(in))
	copy(out, in)

	// Simple insertion sort (slices are typically small).
	for i := 1; i < len(out); i++ {
		key := out[i]
		j := i - 1
		for j >= 0 && out[j] > key {
			out[j+1] = out[j]
			j--
		}
		out[j+1] = key
	}

	// Deduplicate.
	n := 0
	for i := 0; i < len(out); i++ {
		if i == 0 || out[i] != out[i-1] {
			out[n] = out[i]
			n++
		}
	}
	return out[:n]
}

// DepProcProcessProposerSlashing processes a proposer slashing where a
// validator signed two distinct headers for the same slot.
func DepProcProcessProposerSlashing(
	state *BsnBeaconState,
	slashing *DepProcProposerSlashing,
	whistleblowerIndex uint64,
) (uint64, error) {
	if state == nil {
		return 0, ErrDepProcNilState
	}
	if slashing == nil {
		return 0, ErrDepProcPSNilRecord
	}

	h1 := &slashing.Header1
	h2 := &slashing.Header2

	// Headers must be for the same slot.
	if h1.Slot != h2.Slot {
		return 0, ErrDepProcPSDiffSlot
	}

	// Headers must differ.
	r1 := depProcHeaderRoot(h1)
	r2 := depProcHeaderRoot(h2)
	if r1 == r2 {
		return 0, ErrDepProcPSSameHeader
	}

	state.mu.Lock()
	defer state.mu.Unlock()

	idx := slashing.ProposerIndex
	if idx >= uint64(len(state.Validators)) {
		return 0, ErrDepProcPSBadIdx
	}

	v := state.Validators[idx]
	currentEpoch := state.BsnGetCurrentEpoch()

	if !v.IsSlashableV2(currentEpoch) {
		return 0, ErrDepProcPSNotSlashable
	}

	// Slash the proposer.
	v.Slashed = true

	if v.ExitEpoch == FarFutureEpoch {
		exitEpoch := Epoch(uint64(currentEpoch) + 1 + MaxSeedLookahead)
		v.ExitEpoch = exitEpoch
		v.WithdrawableEpoch = Epoch(uint64(exitEpoch) + MinValidatorWithdrawDelay)
	}

	slashWithdrawable := Epoch(uint64(currentEpoch) + BsnEpochsPerSlashingsVector)
	if slashWithdrawable > v.WithdrawableEpoch {
		v.WithdrawableEpoch = slashWithdrawable
	}

	// Record slashing.
	slashingsIdx := uint64(currentEpoch) % BsnEpochsPerSlashingsVector
	state.Slashings[slashingsIdx] += v.EffectiveBalance

	// Apply penalty.
	penalty := v.EffectiveBalance / DepProcMinSlashingPenaltyQuotient
	if idx < uint64(len(state.Balances)) {
		if penalty > state.Balances[idx] {
			state.Balances[idx] = 0
		} else {
			state.Balances[idx] -= penalty
		}
	}

	// Whistleblower reward.
	whistleblowerReward := v.EffectiveBalance / DepProcWhistleblowerRewardQuotient
	proposerReward := whistleblowerReward / DepProcProposerRewardQuotient
	if whistleblowerIndex < uint64(len(state.Balances)) {
		state.Balances[whistleblowerIndex] += proposerReward
	}

	return penalty, nil
}

// depProcHeaderRoot computes the signing root for a DepProcSignedHeader.
func depProcHeaderRoot(h *DepProcSignedHeader) [32]byte {
	var buf [8 + 32*3]byte
	binary.LittleEndian.PutUint64(buf[:8], h.Slot)
	copy(buf[8:40], h.ParentRoot[:])
	copy(buf[40:72], h.StateRoot[:])
	copy(buf[72:104], h.BodyRoot[:])
	return sha256.Sum256(buf[:])
}
