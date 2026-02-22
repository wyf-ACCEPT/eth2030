package consensus

import (
	"sync"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

// makeTestBeaconState creates a FullBeaconState with n active validators.
func makeTestBeaconState(n int) *FullBeaconState {
	state := NewFullBeaconState(DefaultConfig())
	for i := 0; i < n; i++ {
		var pubkey [48]byte
		pubkey[0] = byte(i)
		pubkey[1] = byte(i >> 8)
		pubkey[2] = byte(i >> 16)
		v := &ValidatorBalance{
			Pubkey:           pubkey,
			EffectiveBalance: 32 * GweiPerETH,
			ActivationEpoch:  0,
			ExitEpoch:        FarFutureEpoch,
		}
		state.AddValidator(v, 32*GweiPerETH)
	}
	return state
}

func TestComputeSyncCommittee(t *testing.T) {
	state := makeTestBeaconState(1000)
	mgr := NewSyncCommitteeManager(nil)

	committee, indices, err := mgr.ComputeSyncCommittee(state, 0)
	if err != nil {
		t.Fatalf("ComputeSyncCommittee failed: %v", err)
	}

	// Committee should have exactly SyncCommitteeSize members.
	for i := 0; i < SyncCommitteeSize; i++ {
		if committee.Pubkeys[i].IsZero() {
			t.Errorf("pubkey at position %d is zero", i)
		}
	}

	// All indices should be valid validator indices.
	for i, idx := range indices {
		if int(idx) >= state.ValidatorCount() {
			t.Errorf("index %d: validator index %d out of range", i, idx)
		}
	}

	// Aggregate pubkey should not be zero.
	if committee.AggregatePubkey.IsZero() {
		t.Error("aggregate pubkey should not be zero")
	}
}

func TestComputeSyncCommittee_DeterministicForSameEpoch(t *testing.T) {
	state := makeTestBeaconState(600)
	mgr := NewSyncCommitteeManager(nil)

	c1, idx1, err := mgr.ComputeSyncCommittee(state, 5)
	if err != nil {
		t.Fatalf("first compute failed: %v", err)
	}
	c2, idx2, err := mgr.ComputeSyncCommittee(state, 5)
	if err != nil {
		t.Fatalf("second compute failed: %v", err)
	}

	if c1.AggregatePubkey != c2.AggregatePubkey {
		t.Error("same epoch should produce same aggregate pubkey")
	}
	for i := 0; i < SyncCommitteeSize; i++ {
		if idx1[i] != idx2[i] {
			t.Errorf("position %d: index mismatch %d vs %d", i, idx1[i], idx2[i])
		}
	}
}

func TestComputeSyncCommittee_DifferentEpochs(t *testing.T) {
	state := makeTestBeaconState(800)
	mgr := NewSyncCommitteeManager(nil)

	c1, _, err := mgr.ComputeSyncCommittee(state, 0)
	if err != nil {
		t.Fatal(err)
	}
	c2, _, err := mgr.ComputeSyncCommittee(state, 256)
	if err != nil {
		t.Fatal(err)
	}

	// Different epochs should produce different committees (with high probability).
	if c1.AggregatePubkey == c2.AggregatePubkey {
		t.Error("different epochs should (very likely) produce different committees")
	}
}

func TestComputeSyncCommittee_NilState(t *testing.T) {
	mgr := NewSyncCommitteeManager(nil)
	_, _, err := mgr.ComputeSyncCommittee(nil, 0)
	if err != ErrSyncNilState {
		t.Errorf("expected ErrSyncNilState, got %v", err)
	}
}

func TestComputeSyncCommittee_NoValidators(t *testing.T) {
	state := makeTestBeaconState(0)
	mgr := NewSyncCommitteeManager(nil)
	_, _, err := mgr.ComputeSyncCommittee(state, 0)
	if err != ErrSyncNoValidators {
		t.Errorf("expected ErrSyncNoValidators, got %v", err)
	}
}

func TestInitializeCommittees(t *testing.T) {
	state := makeTestBeaconState(1000)
	mgr := NewSyncCommitteeManager(nil)

	err := mgr.InitializeCommittees(state, 0)
	if err != nil {
		t.Fatalf("InitializeCommittees failed: %v", err)
	}

	if mgr.CurrentCommittee() == nil {
		t.Error("current committee should not be nil after init")
	}
	if mgr.NextCommittee() == nil {
		t.Error("next committee should not be nil after init")
	}

	// Current and next committees should differ.
	curr := mgr.CurrentCommittee()
	next := mgr.NextCommittee()
	if curr.AggregatePubkey == next.AggregatePubkey {
		t.Error("current and next committees should (very likely) differ")
	}
}

func TestProcessSyncAggregate(t *testing.T) {
	state := makeTestBeaconState(1000)
	mgr := NewSyncCommitteeManager(nil)

	if err := mgr.InitializeCommittees(state, 0); err != nil {
		t.Fatal(err)
	}

	// Create a sync aggregate where all members participated.
	var bits [SyncAggregateBitfieldSize]byte
	for i := range bits {
		bits[i] = 0xFF
	}
	sig := types.HexToHash("0xdeadbeef")

	err := mgr.ProcessSyncAggregate(bits, sig)
	if err != nil {
		t.Fatalf("ProcessSyncAggregate failed: %v", err)
	}

	// Participation rate should be 1.0 (all bits set).
	rate := mgr.ParticipationRate()
	if rate != 1.0 {
		t.Errorf("expected participation rate 1.0, got %f", rate)
	}
}

func TestProcessSyncAggregate_PartialParticipation(t *testing.T) {
	state := makeTestBeaconState(1000)
	mgr := NewSyncCommitteeManager(nil)

	if err := mgr.InitializeCommittees(state, 0); err != nil {
		t.Fatal(err)
	}

	// Set only the first 256 bits (half).
	var bits [SyncAggregateBitfieldSize]byte
	for i := 0; i < SyncAggregateBitfieldSize/2; i++ {
		bits[i] = 0xFF
	}

	err := mgr.ProcessSyncAggregate(bits, types.Hash{})
	if err != nil {
		t.Fatalf("ProcessSyncAggregate failed: %v", err)
	}

	rate := mgr.ParticipationRate()
	if rate < 0.49 || rate > 0.51 {
		t.Errorf("expected participation rate ~0.5, got %f", rate)
	}
}

func TestProcessSyncAggregate_NotInitialized(t *testing.T) {
	mgr := NewSyncCommitteeManager(nil)
	var bits [SyncAggregateBitfieldSize]byte
	err := mgr.ProcessSyncAggregate(bits, types.Hash{})
	if err != ErrSyncCommitteeNotReady {
		t.Errorf("expected ErrSyncCommitteeNotReady, got %v", err)
	}
}

func TestRewardTracking(t *testing.T) {
	state := makeTestBeaconState(1000)
	cfg := DefaultSyncCommitteeConfig()
	cfg.BaseRewardPerInc = 100
	mgr := NewSyncCommitteeManager(cfg)

	if err := mgr.InitializeCommittees(state, 0); err != nil {
		t.Fatal(err)
	}

	// Set bit 0 only.
	var bits [SyncAggregateBitfieldSize]byte
	bits[0] = 0x01

	if err := mgr.ProcessSyncAggregate(bits, types.Hash{}); err != nil {
		t.Fatal(err)
	}

	// Validator at position 0 should have received a reward.
	mgr.mu.RLock()
	idx0 := mgr.currentIndices[0]
	mgr.mu.RUnlock()

	reward := mgr.GetReward(idx0)
	if reward != 100 {
		t.Errorf("expected reward 100, got %d", reward)
	}

	// Process another aggregate with same bit set.
	if err := mgr.ProcessSyncAggregate(bits, types.Hash{}); err != nil {
		t.Fatal(err)
	}
	reward = mgr.GetReward(idx0)
	if reward != 200 {
		t.Errorf("expected accumulated reward 200, got %d", reward)
	}
}

func TestRotateCommittees(t *testing.T) {
	state := makeTestBeaconState(1000)
	mgr := NewSyncCommitteeManager(nil)

	if err := mgr.InitializeCommittees(state, 0); err != nil {
		t.Fatal(err)
	}

	nextBefore := mgr.NextCommittee()

	// Rotate at the next period boundary.
	nextPeriodEpoch := Epoch(EpochsPerSyncCommitteePeriod)
	err := mgr.RotateCommittees(state, nextPeriodEpoch)
	if err != nil {
		t.Fatalf("RotateCommittees failed: %v", err)
	}

	// Current should now equal the old next.
	currAfter := mgr.CurrentCommittee()
	if currAfter.AggregatePubkey != nextBefore.AggregatePubkey {
		t.Error("after rotation, current should equal the previous next committee")
	}

	// Next should be a new committee.
	newNext := mgr.NextCommittee()
	if newNext == nil {
		t.Error("next committee should not be nil after rotation")
	}

	// Participation should be reset.
	if mgr.ParticipationRate() != 0 {
		t.Error("participation rate should be reset after rotation")
	}

	// Period should advance.
	if mgr.CurrentPeriod() != 1 {
		t.Errorf("expected period 1, got %d", mgr.CurrentPeriod())
	}
}

func TestShouldRotate(t *testing.T) {
	mgr := NewSyncCommitteeManager(nil)

	if !mgr.ShouldRotate(0) {
		t.Error("epoch 0 should be a rotation boundary")
	}
	if !mgr.ShouldRotate(256) {
		t.Error("epoch 256 should be a rotation boundary")
	}
	if mgr.ShouldRotate(1) {
		t.Error("epoch 1 should not be a rotation boundary")
	}
	if mgr.ShouldRotate(255) {
		t.Error("epoch 255 should not be a rotation boundary")
	}
}

func TestSyncCommitteeManager_ThreadSafety(t *testing.T) {
	state := makeTestBeaconState(1000)
	mgr := NewSyncCommitteeManager(nil)

	if err := mgr.InitializeCommittees(state, 0); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	errs := make(chan error, 100)

	// Concurrent reads.
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = mgr.CurrentCommittee()
			_ = mgr.NextCommittee()
			_ = mgr.ParticipationRate()
			_ = mgr.CurrentPeriod()
			_ = mgr.GetReward(ValidatorIndex(0))
		}()
	}

	// Concurrent writes.
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var bits [SyncAggregateBitfieldSize]byte
			bits[0] = 0x01
			if err := mgr.ProcessSyncAggregate(bits, types.Hash{}); err != nil {
				errs <- err
			}
		}()
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("concurrent error: %v", err)
	}
}

func TestComputeSyncCommittee_SmallValidatorSet(t *testing.T) {
	// With fewer validators than committee size, validators can repeat
	// (allowed by spec: "with possible duplicates").
	state := makeTestBeaconState(10)
	mgr := NewSyncCommitteeManager(nil)

	committee, _, err := mgr.ComputeSyncCommittee(state, 0)
	if err != nil {
		t.Fatalf("ComputeSyncCommittee failed with small set: %v", err)
	}

	// Should still fill all 512 positions.
	nonZero := 0
	for _, pk := range committee.Pubkeys {
		if !pk.IsZero() {
			nonZero++
		}
	}
	if nonZero != SyncCommitteeSize {
		t.Errorf("expected %d non-zero pubkeys, got %d", SyncCommitteeSize, nonZero)
	}
}

func TestDefaultSyncCommitteeConfig(t *testing.T) {
	cfg := DefaultSyncCommitteeConfig()
	if cfg.SlotsPerEpoch != 32 {
		t.Errorf("expected SlotsPerEpoch 32, got %d", cfg.SlotsPerEpoch)
	}
	if cfg.CommitteeSize != SyncCommitteeSize {
		t.Errorf("expected CommitteeSize %d, got %d", SyncCommitteeSize, cfg.CommitteeSize)
	}
	if cfg.PeriodLength != EpochsPerSyncCommitteePeriod {
		t.Errorf("expected PeriodLength %d, got %d", EpochsPerSyncCommitteePeriod, cfg.PeriodLength)
	}
}
