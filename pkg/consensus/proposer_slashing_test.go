package consensus

import (
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

func makeSlashingHeader(slot Slot, parentRoot, stateRoot, bodyRoot types.Hash) SignedBeaconBlockHeader {
	return SignedBeaconBlockHeader{
		Slot:       slot,
		ParentRoot: parentRoot,
		StateRoot:  stateRoot,
		BodyRoot:   bodyRoot,
	}
}

func makeTestValidators(count int) []*ValidatorV2 {
	vals := make([]*ValidatorV2, count)
	for i := range vals {
		vals[i] = &ValidatorV2{
			EffectiveBalance:           32_000_000_000, // 32 ETH in Gwei
			Slashed:                    false,
			ActivationEligibilityEpoch: 0,
			ActivationEpoch:            0,
			ExitEpoch:                  ^Epoch(0), // far future
			WithdrawableEpoch:          ^Epoch(0),
		}
	}
	return vals
}

func TestHeaderRoot_DifferentHeaders(t *testing.T) {
	h1 := makeSlashingHeader(10, types.HexToHash("0xaa"), types.HexToHash("0xbb"), types.HexToHash("0xcc"))
	h2 := makeSlashingHeader(10, types.HexToHash("0xdd"), types.HexToHash("0xee"), types.HexToHash("0xff"))

	r1 := HeaderRoot(&h1)
	r2 := HeaderRoot(&h2)

	if r1 == r2 {
		t.Error("different headers should have different roots")
	}
}

func TestHeaderRoot_SameHeader(t *testing.T) {
	h1 := makeSlashingHeader(10, types.HexToHash("0xaa"), types.HexToHash("0xbb"), types.HexToHash("0xcc"))
	h2 := makeSlashingHeader(10, types.HexToHash("0xaa"), types.HexToHash("0xbb"), types.HexToHash("0xcc"))

	if HeaderRoot(&h1) != HeaderRoot(&h2) {
		t.Error("identical headers should have the same root")
	}
}

func TestHeaderRoot_Deterministic(t *testing.T) {
	h := makeSlashingHeader(42, types.HexToHash("0x01"), types.HexToHash("0x02"), types.HexToHash("0x03"))
	r1 := HeaderRoot(&h)
	r2 := HeaderRoot(&h)
	if r1 != r2 {
		t.Error("HeaderRoot should be deterministic")
	}
}

func TestComputeProposerSlashingPenalty(t *testing.T) {
	tests := []struct {
		name             string
		effectiveBalance uint64
		wantPenalty      uint64
		wantWhistle      uint64
		wantProposer     uint64
	}{
		{
			name:             "32 ETH",
			effectiveBalance: 32_000_000_000,
			wantPenalty:      32_000_000_000 / 32,
			wantWhistle:      32_000_000_000 / 512,
			wantProposer:     32_000_000_000 / 512 / 8,
		},
		{
			name:             "16 ETH",
			effectiveBalance: 16_000_000_000,
			wantPenalty:      16_000_000_000 / 32,
			wantWhistle:      16_000_000_000 / 512,
			wantProposer:     16_000_000_000 / 512 / 8,
		},
		{
			name:             "zero balance",
			effectiveBalance: 0,
			wantPenalty:      0,
			wantWhistle:      0,
			wantProposer:     0,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := ComputeProposerSlashingPenalty(tc.effectiveBalance, ValidatorIndex(0))
			if p.Penalty != tc.wantPenalty {
				t.Errorf("penalty = %d, want %d", p.Penalty, tc.wantPenalty)
			}
			if p.WhistleblowerRwd != tc.wantWhistle {
				t.Errorf("whistleblower reward = %d, want %d", p.WhistleblowerRwd, tc.wantWhistle)
			}
			if p.ProposerRwd != tc.wantProposer {
				t.Errorf("proposer reward = %d, want %d", p.ProposerRwd, tc.wantProposer)
			}
			if p.EffectiveBalance != tc.effectiveBalance {
				t.Errorf("effective balance = %d, want %d", p.EffectiveBalance, tc.effectiveBalance)
			}
		})
	}
}

func TestValidateProposerSlashing_Valid(t *testing.T) {
	validators := makeTestValidators(10)
	record := &ProposerSlashingRecord{
		ProposerIndex: 5,
		Slot:          10,
		Header1:       makeSlashingHeader(10, types.HexToHash("0xaa"), types.HexToHash("0xbb"), types.HexToHash("0xcc")),
		Header2:       makeSlashingHeader(10, types.HexToHash("0xdd"), types.HexToHash("0xee"), types.HexToHash("0xff")),
	}

	err := ValidateProposerSlashing(record, validators, Epoch(1))
	if err != nil {
		t.Fatalf("expected valid slashing, got error: %v", err)
	}
}

func TestValidateProposerSlashing_NilRecord(t *testing.T) {
	err := ValidateProposerSlashing(nil, nil, 0)
	if err != ErrPSNilEvidence {
		t.Errorf("expected ErrPSNilEvidence, got %v", err)
	}
}

func TestValidateProposerSlashing_DifferentSlots(t *testing.T) {
	validators := makeTestValidators(10)
	record := &ProposerSlashingRecord{
		ProposerIndex: 5,
		Slot:          10,
		Header1:       makeSlashingHeader(10, types.HexToHash("0xaa"), types.HexToHash("0xbb"), types.HexToHash("0xcc")),
		Header2:       makeSlashingHeader(11, types.HexToHash("0xdd"), types.HexToHash("0xee"), types.HexToHash("0xff")),
	}

	err := ValidateProposerSlashing(record, validators, Epoch(1))
	if err != ErrPSDifferentSlot {
		t.Errorf("expected ErrPSDifferentSlot, got %v", err)
	}
}

func TestValidateProposerSlashing_SameRoot(t *testing.T) {
	validators := makeTestValidators(10)
	h := makeSlashingHeader(10, types.HexToHash("0xaa"), types.HexToHash("0xbb"), types.HexToHash("0xcc"))
	record := &ProposerSlashingRecord{
		ProposerIndex: 5,
		Slot:          10,
		Header1:       h,
		Header2:       h,
	}

	err := ValidateProposerSlashing(record, validators, Epoch(1))
	if err != ErrPSSameRoot {
		t.Errorf("expected ErrPSSameRoot, got %v", err)
	}
}

func TestValidateProposerSlashing_ProposerOutOfBounds(t *testing.T) {
	validators := makeTestValidators(5)
	record := &ProposerSlashingRecord{
		ProposerIndex: 10, // out of bounds
		Slot:          10,
		Header1:       makeSlashingHeader(10, types.HexToHash("0xaa"), types.HexToHash("0xbb"), types.HexToHash("0xcc")),
		Header2:       makeSlashingHeader(10, types.HexToHash("0xdd"), types.HexToHash("0xee"), types.HexToHash("0xff")),
	}

	err := ValidateProposerSlashing(record, validators, Epoch(1))
	if err != ErrPSNotActive {
		t.Errorf("expected ErrPSNotActive, got %v", err)
	}
}

func TestValidateProposerSlashing_AlreadySlashed(t *testing.T) {
	validators := makeTestValidators(10)
	validators[5].Slashed = true

	record := &ProposerSlashingRecord{
		ProposerIndex: 5,
		Slot:          10,
		Header1:       makeSlashingHeader(10, types.HexToHash("0xaa"), types.HexToHash("0xbb"), types.HexToHash("0xcc")),
		Header2:       makeSlashingHeader(10, types.HexToHash("0xdd"), types.HexToHash("0xee"), types.HexToHash("0xff")),
	}

	err := ValidateProposerSlashing(record, validators, Epoch(1))
	if err != ErrPSAlreadySlashed {
		t.Errorf("expected ErrPSAlreadySlashed, got %v", err)
	}
}

func TestValidateProposerSlashing_NotSlashable(t *testing.T) {
	validators := makeTestValidators(10)
	// Make validator 5 not yet activated.
	validators[5].ActivationEpoch = 100

	record := &ProposerSlashingRecord{
		ProposerIndex: 5,
		Slot:          10,
		Header1:       makeSlashingHeader(10, types.HexToHash("0xaa"), types.HexToHash("0xbb"), types.HexToHash("0xcc")),
		Header2:       makeSlashingHeader(10, types.HexToHash("0xdd"), types.HexToHash("0xee"), types.HexToHash("0xff")),
	}

	err := ValidateProposerSlashing(record, validators, Epoch(1))
	if err != ErrPSNotSlashable {
		t.Errorf("expected ErrPSNotSlashable, got %v", err)
	}
}

func TestProposerSlashingPool_AddEvidence(t *testing.T) {
	pool := NewProposerSlashingPool()

	record := &ProposerSlashingRecord{
		ProposerIndex: 5,
		Slot:          10,
		Header1:       makeSlashingHeader(10, types.HexToHash("0xaa"), types.HexToHash("0xbb"), types.HexToHash("0xcc")),
		Header2:       makeSlashingHeader(10, types.HexToHash("0xdd"), types.HexToHash("0xee"), types.HexToHash("0xff")),
	}

	err := pool.AddEvidence(record)
	if err != nil {
		t.Fatalf("AddEvidence failed: %v", err)
	}
	if pool.PendingCount() != 1 {
		t.Errorf("pending count = %d, want 1", pool.PendingCount())
	}
	if !pool.HasEvidence(5) {
		t.Error("should have evidence for proposer 5")
	}
}

func TestProposerSlashingPool_AddEvidenceNil(t *testing.T) {
	pool := NewProposerSlashingPool()
	err := pool.AddEvidence(nil)
	if err != ErrPSNilEvidence {
		t.Errorf("expected ErrPSNilEvidence, got %v", err)
	}
}

func TestProposerSlashingPool_DuplicateEvidence(t *testing.T) {
	pool := NewProposerSlashingPool()

	record := &ProposerSlashingRecord{
		ProposerIndex: 5,
		Slot:          10,
		Header1:       makeSlashingHeader(10, types.HexToHash("0xaa"), types.HexToHash("0xbb"), types.HexToHash("0xcc")),
		Header2:       makeSlashingHeader(10, types.HexToHash("0xdd"), types.HexToHash("0xee"), types.HexToHash("0xff")),
	}

	pool.AddEvidence(record)
	err := pool.AddEvidence(record)
	if err != ErrPSDuplicateEvidence {
		t.Errorf("expected ErrPSDuplicateEvidence, got %v", err)
	}
}

func TestProposerSlashingPool_GetPending(t *testing.T) {
	pool := NewProposerSlashingPool()

	for i := 0; i < 5; i++ {
		pool.AddEvidence(&ProposerSlashingRecord{
			ProposerIndex: ValidatorIndex(i),
			Slot:          Slot(i),
			Header1:       makeSlashingHeader(Slot(i), types.HexToHash("0xaa"), types.HexToHash("0xbb"), types.HexToHash("0xcc")),
			Header2:       makeSlashingHeader(Slot(i), types.HexToHash("0xdd"), types.HexToHash("0xee"), types.HexToHash("0xff")),
		})
	}

	// Get with limit.
	result := pool.GetPending(3)
	if len(result) != 3 {
		t.Errorf("GetPending(3) returned %d items, want 3", len(result))
	}

	// Get with zero should use MaxProposerSlashings.
	result = pool.GetPending(0)
	if len(result) != 5 {
		t.Errorf("GetPending(0) returned %d items, want 5", len(result))
	}
}

func TestProposerSlashingPool_MarkIncluded(t *testing.T) {
	pool := NewProposerSlashingPool()

	pool.AddEvidence(&ProposerSlashingRecord{
		ProposerIndex: 5,
		Slot:          10,
	})
	pool.AddEvidence(&ProposerSlashingRecord{
		ProposerIndex: 7,
		Slot:          11,
	})

	pool.MarkIncluded(5)
	if pool.PendingCount() != 1 {
		t.Errorf("pending count = %d, want 1", pool.PendingCount())
	}
	if pool.HasEvidence(5) {
		t.Error("should not have evidence for proposer 5 after inclusion")
	}
	if !pool.HasEvidence(7) {
		t.Error("should still have evidence for proposer 7")
	}
}

func TestProposerSlashingPool_ProposerIndices(t *testing.T) {
	pool := NewProposerSlashingPool()

	pool.AddEvidence(&ProposerSlashingRecord{ProposerIndex: 10, Slot: 1})
	pool.AddEvidence(&ProposerSlashingRecord{ProposerIndex: 3, Slot: 2})
	pool.AddEvidence(&ProposerSlashingRecord{ProposerIndex: 7, Slot: 3})

	indices := pool.ProposerIndices()
	if len(indices) != 3 {
		t.Fatalf("expected 3 indices, got %d", len(indices))
	}
	// Should be sorted.
	for i := 1; i < len(indices); i++ {
		if indices[i-1] >= indices[i] {
			t.Errorf("indices not sorted: %v", indices)
			break
		}
	}
}

func TestProposerSlashingPool_RegisterBlockHeader_NoConflict(t *testing.T) {
	pool := NewProposerSlashingPool()
	h := makeSlashingHeader(10, types.HexToHash("0xaa"), types.HexToHash("0xbb"), types.HexToHash("0xcc"))

	record := pool.RegisterBlockHeader(5, h, 10)
	if record != nil {
		t.Error("first registration should not produce a slashing record")
	}
	if pool.RegistrySize() != 1 {
		t.Errorf("registry size = %d, want 1", pool.RegistrySize())
	}
}

func TestProposerSlashingPool_RegisterBlockHeader_SameHeader(t *testing.T) {
	pool := NewProposerSlashingPool()
	h := makeSlashingHeader(10, types.HexToHash("0xaa"), types.HexToHash("0xbb"), types.HexToHash("0xcc"))

	pool.RegisterBlockHeader(5, h, 10)
	record := pool.RegisterBlockHeader(5, h, 10)
	if record != nil {
		t.Error("registering the same header twice should not produce a slashing")
	}
}

func TestProposerSlashingPool_RegisterBlockHeader_DoubleProposal(t *testing.T) {
	pool := NewProposerSlashingPool()
	h1 := makeSlashingHeader(10, types.HexToHash("0xaa"), types.HexToHash("0xbb"), types.HexToHash("0xcc"))
	h2 := makeSlashingHeader(10, types.HexToHash("0xdd"), types.HexToHash("0xee"), types.HexToHash("0xff"))

	pool.RegisterBlockHeader(5, h1, 10)
	record := pool.RegisterBlockHeader(5, h2, 10)
	if record == nil {
		t.Fatal("double proposal should produce a slashing record")
	}
	if record.ProposerIndex != 5 {
		t.Errorf("proposer index = %d, want 5", record.ProposerIndex)
	}
	if record.Slot != 10 {
		t.Errorf("slot = %d, want 10", record.Slot)
	}
	if pool.PendingCount() != 1 {
		t.Errorf("pending count = %d, want 1", pool.PendingCount())
	}
}

func TestProposerSlashingPool_Prune(t *testing.T) {
	pool := NewProposerSlashingPool()
	h1 := makeSlashingHeader(5, types.HexToHash("0xaa"), types.HexToHash("0xbb"), types.HexToHash("0xcc"))
	h2 := makeSlashingHeader(15, types.HexToHash("0xdd"), types.HexToHash("0xee"), types.HexToHash("0xff"))

	pool.RegisterBlockHeader(1, h1, 5)
	pool.RegisterBlockHeader(2, h2, 15)

	if pool.RegistrySize() != 2 {
		t.Fatalf("registry size = %d, want 2", pool.RegistrySize())
	}

	// Prune entries before slot 10; only h1 (slot 5) should be removed.
	pool.Prune(10)
	if pool.RegistrySize() != 1 {
		t.Errorf("registry size after prune = %d, want 1", pool.RegistrySize())
	}
}

func TestProposerSlashingPool_Eviction(t *testing.T) {
	pool := NewProposerSlashingPool()

	// Fill the pool to capacity.
	for i := 0; i < MaxProposerSlashingsPerPool; i++ {
		pool.AddEvidence(&ProposerSlashingRecord{
			ProposerIndex: ValidatorIndex(i),
			Slot:          Slot(i),
		})
	}
	if pool.PendingCount() != MaxProposerSlashingsPerPool {
		t.Fatalf("pending = %d, want %d", pool.PendingCount(), MaxProposerSlashingsPerPool)
	}

	// Adding one more should evict the oldest.
	err := pool.AddEvidence(&ProposerSlashingRecord{
		ProposerIndex: ValidatorIndex(9999),
		Slot:          999,
	})
	if err != nil {
		t.Fatalf("AddEvidence with eviction failed: %v", err)
	}
	if pool.PendingCount() != MaxProposerSlashingsPerPool {
		t.Errorf("pending after eviction = %d, want %d", pool.PendingCount(), MaxProposerSlashingsPerPool)
	}
	// Oldest (proposer 0) should have been evicted.
	if pool.HasEvidence(0) {
		t.Error("proposer 0 should have been evicted")
	}
	if !pool.HasEvidence(9999) {
		t.Error("proposer 9999 should be present")
	}
}

func TestComputeProposerSlashingPenalty_RewardRelationships(t *testing.T) {
	// The spec requires: penalty > whistleblower_reward > proposer_reward.
	balance := uint64(32_000_000_000)
	p := ComputeProposerSlashingPenalty(balance, ValidatorIndex(0))

	if p.Penalty <= p.WhistleblowerRwd {
		t.Error("penalty should be greater than whistleblower reward")
	}
	if p.WhistleblowerRwd <= p.ProposerRwd {
		t.Error("whistleblower reward should be greater than proposer reward")
	}
	// Proposer reward is whistleblower_reward / 8.
	if p.ProposerRwd != p.WhistleblowerRwd/8 {
		t.Errorf("proposer reward = %d, want %d", p.ProposerRwd, p.WhistleblowerRwd/8)
	}
}
