package consensus

import (
	"testing"
)

func TestNewCasperFinalityTracker(t *testing.T) {
	ft := NewCasperFinalityTracker(32)
	if ft == nil {
		t.Fatal("expected non-nil tracker")
	}
	// Genesis should be finalized at epoch 0.
	cp := ft.GetFinalizedCheckpoint()
	if cp.Epoch != 0 {
		t.Errorf("expected finalized epoch 0, got %d", cp.Epoch)
	}
	jcp := ft.GetJustifiedCheckpoint()
	if jcp.Epoch != 0 {
		t.Errorf("expected justified epoch 0, got %d", jcp.Epoch)
	}
}

func TestNewCasperFinalityTrackerDefaultSlotsPerEpoch(t *testing.T) {
	ft := NewCasperFinalityTracker(0)
	if ft.slotsPerEpoch != 32 {
		t.Errorf("expected default slots per epoch 32, got %d", ft.slotsPerEpoch)
	}
}

func TestIsSuperMajority(t *testing.T) {
	tests := []struct {
		name        string
		vote, total uint64
		want        bool
	}{
		{"zero total", 0, 0, false},
		{"exactly 2/3", 200, 300, true},
		{"above 2/3", 300, 400, true},
		{"below 2/3", 100, 300, false},
		{"unanimous", 100, 100, true},
		{"barely below", 199, 300, false},
		{"one above", 201, 300, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isSuperMajority(tt.vote, tt.total)
			if got != tt.want {
				t.Errorf("isSuperMajority(%d, %d) = %v, want %v", tt.vote, tt.total, got, tt.want)
			}
		})
	}
}

func TestCasperCheckpointIsZero(t *testing.T) {
	zero := CasperCheckpoint{}
	if !zero.IsZero() {
		t.Error("expected zero checkpoint to be zero")
	}
	nonZero := CasperCheckpoint{Epoch: 1}
	if nonZero.IsZero() {
		t.Error("expected non-zero checkpoint to not be zero")
	}
}

func TestCasperCheckpointEquals(t *testing.T) {
	a := CasperCheckpoint{Epoch: 5, Root: [32]byte{1}}
	b := CasperCheckpoint{Epoch: 5, Root: [32]byte{1}}
	c := CasperCheckpoint{Epoch: 6, Root: [32]byte{1}}
	if !a.Equals(b) {
		t.Error("expected equal checkpoints")
	}
	if a.Equals(c) {
		t.Error("expected different checkpoints")
	}
}

func TestProcessJustificationNilState(t *testing.T) {
	ft := NewCasperFinalityTracker(32)
	if err := ft.ProcessJustification(5, nil); err != ErrFRNilState {
		t.Errorf("expected ErrFRNilState, got %v", err)
	}
}

func TestProcessJustificationGenesisEpoch(t *testing.T) {
	ft := NewCasperFinalityTracker(32)
	state := NewBeaconStateV2(32)
	if err := ft.ProcessJustification(0, state); err != ErrFRGenesisEpoch {
		t.Errorf("expected ErrFRGenesisEpoch for epoch 0, got %v", err)
	}
	if err := ft.ProcessJustification(1, state); err != ErrFRGenesisEpoch {
		t.Errorf("expected ErrFRGenesisEpoch for epoch 1, got %v", err)
	}
}

func TestProcessJustificationNoValidators(t *testing.T) {
	ft := NewCasperFinalityTracker(32)
	state := NewBeaconStateV2(32)
	err := ft.ProcessJustification(5, state)
	if err != ErrFRNoValidators {
		t.Errorf("expected ErrFRNoValidators, got %v", err)
	}
}

// makeStateWithValidators creates a BeaconStateV2 with n active validators
// each having the given effective balance.
func makeStateWithValidators(n int, balance uint64, spe uint64) *BeaconStateV2 {
	state := NewBeaconStateV2(spe)
	for i := 0; i < n; i++ {
		v := &ValidatorV2{
			EffectiveBalance:           balance,
			ActivationEpoch:            0,
			ExitEpoch:                  FarFutureEpoch,
			ActivationEligibilityEpoch: 0,
			WithdrawableEpoch:          FarFutureEpoch,
		}
		v.Pubkey[0] = byte(i)
		state.AddValidatorV2(v, balance)
	}
	return state
}

func TestProcessJustificationWithWeights(t *testing.T) {
	ft := NewCasperFinalityTracker(32)
	state := makeStateWithValidators(100, 32*GweiPerETH, 32)
	state.Slot = 5 * 32

	totalWeight := uint64(100 * 32 * GweiPerETH)
	// Supermajority: needs voteWeight*3 >= totalWeight*2.
	// Using 2/3 + 1 to ensure integer math passes threshold.
	prevWeight := totalWeight*2/3 + 1
	currWeight := totalWeight*2/3 + 1

	err := ft.ProcessJustificationWithWeights(5, state, prevWeight, currWeight, totalWeight)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Both epochs should be justified; current epoch justified -> epoch 5.
	jcp := ft.GetJustifiedCheckpoint()
	if jcp.Epoch != 5 {
		t.Errorf("expected justified epoch 5, got %d", jcp.Epoch)
	}

	bits := ft.GetJustificationBits()
	if !bits[0] {
		t.Error("expected bit 0 (current) set")
	}
	if !bits[1] {
		t.Error("expected bit 1 (previous) set")
	}
}

func TestProcessJustificationWithWeightsInvalid(t *testing.T) {
	ft := NewCasperFinalityTracker(32)
	state := makeStateWithValidators(10, 32*GweiPerETH, 32)

	// Vote weight > total weight should error.
	err := ft.ProcessJustificationWithWeights(5, state, 200, 100, 100)
	if err != ErrFRInvalidWeight {
		t.Errorf("expected ErrFRInvalidWeight, got %v", err)
	}
}

func TestProcessFinalizationNilState(t *testing.T) {
	ft := NewCasperFinalityTracker(32)
	if err := ft.ProcessFinalization(5, nil); err != ErrFRNilState {
		t.Errorf("expected ErrFRNilState, got %v", err)
	}
}

func TestProcessFinalizationGenesisEpoch(t *testing.T) {
	ft := NewCasperFinalityTracker(32)
	state := NewBeaconStateV2(32)
	if err := ft.ProcessFinalization(0, state); err != ErrFRGenesisEpoch {
		t.Errorf("expected ErrFRGenesisEpoch, got %v", err)
	}
}

// TestFinalizationCondition1 tests: bits[0] && bits[1], justified.Epoch+1 == current.
func TestFinalizationCondition1(t *testing.T) {
	ft := NewCasperFinalityTracker(32)
	state := makeStateWithValidators(10, 32*GweiPerETH, 32)

	// Set up: justified at epoch 4, bits[0] and bits[1] set, current = 5.
	cp := CasperCheckpoint{Epoch: 4, Root: [32]byte{4}}
	ft.SetJustified(cp)
	ft.SetPreviousJustified(CasperCheckpoint{Epoch: 3, Root: [32]byte{3}})
	ft.SetJustificationBits([4]bool{true, true, false, false})

	err := ft.ProcessFinalization(5, state)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	fin := ft.GetFinalizedCheckpoint()
	if fin.Epoch != 4 {
		t.Errorf("condition 1: expected finalized epoch 4, got %d", fin.Epoch)
	}
}

// TestFinalizationCondition2 tests: bits[1] && bits[2], previousJustified.Epoch+2 == current.
func TestFinalizationCondition2(t *testing.T) {
	ft := NewCasperFinalityTracker(32)
	state := makeStateWithValidators(10, 32*GweiPerETH, 32)

	pj := CasperCheckpoint{Epoch: 3, Root: [32]byte{3}}
	ft.SetPreviousJustified(pj)
	ft.SetJustified(CasperCheckpoint{Epoch: 4, Root: [32]byte{4}})
	ft.SetJustificationBits([4]bool{false, true, true, false})

	err := ft.ProcessFinalization(5, state)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	fin := ft.GetFinalizedCheckpoint()
	if fin.Epoch != 3 {
		t.Errorf("condition 2: expected finalized epoch 3, got %d", fin.Epoch)
	}
}

// TestFinalizationCondition3 tests: bits[0] && bits[1] && bits[2], justified.Epoch+2 == current.
func TestFinalizationCondition3(t *testing.T) {
	ft := NewCasperFinalityTracker(32)
	state := makeStateWithValidators(10, 32*GweiPerETH, 32)

	cj := CasperCheckpoint{Epoch: 4, Root: [32]byte{4}}
	ft.SetJustified(cj)
	ft.SetPreviousJustified(CasperCheckpoint{Epoch: 3, Root: [32]byte{3}})
	ft.SetJustificationBits([4]bool{true, true, true, false})

	err := ft.ProcessFinalization(6, state)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	fin := ft.GetFinalizedCheckpoint()
	if fin.Epoch != 4 {
		t.Errorf("condition 3: expected finalized epoch 4, got %d", fin.Epoch)
	}
}

// TestFinalizationCondition4 tests: bits[1] && bits[2] && bits[3], previousJustified.Epoch+3 == current.
func TestFinalizationCondition4(t *testing.T) {
	ft := NewCasperFinalityTracker(32)
	state := makeStateWithValidators(10, 32*GweiPerETH, 32)

	pj := CasperCheckpoint{Epoch: 3, Root: [32]byte{3}}
	ft.SetPreviousJustified(pj)
	ft.SetJustified(CasperCheckpoint{Epoch: 5, Root: [32]byte{5}})
	ft.SetJustificationBits([4]bool{false, true, true, true})

	err := ft.ProcessFinalization(6, state)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	fin := ft.GetFinalizedCheckpoint()
	if fin.Epoch != 3 {
		t.Errorf("condition 4: expected finalized epoch 3, got %d", fin.Epoch)
	}
}

func TestNoFinalizationWhenBitsNotSet(t *testing.T) {
	ft := NewCasperFinalityTracker(32)
	state := makeStateWithValidators(10, 32*GweiPerETH, 32)

	ft.SetJustified(CasperCheckpoint{Epoch: 4, Root: [32]byte{4}})
	ft.SetPreviousJustified(CasperCheckpoint{Epoch: 3, Root: [32]byte{3}})
	// No bits set -- should not finalize.
	ft.SetJustificationBits([4]bool{false, false, false, false})

	err := ft.ProcessFinalization(5, state)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	fin := ft.GetFinalizedCheckpoint()
	// Finalized should remain at genesis (epoch 0).
	if fin.Epoch != 0 {
		t.Errorf("expected finalized epoch 0 (no finalization), got %d", fin.Epoch)
	}
}

func TestIsFinalized(t *testing.T) {
	ft := NewCasperFinalityTracker(32)
	ft.SetFinalized(CasperCheckpoint{Epoch: 5, Root: [32]byte{5}})

	if !ft.IsFinalized(CasperCheckpoint{Epoch: 3}) {
		t.Error("epoch 3 should be finalized (before epoch 5)")
	}
	if !ft.IsFinalized(CasperCheckpoint{Epoch: 5}) {
		t.Error("epoch 5 should be finalized (equal to finalized)")
	}
	if ft.IsFinalized(CasperCheckpoint{Epoch: 6}) {
		t.Error("epoch 6 should not be finalized")
	}
}

func TestFinalityDelay(t *testing.T) {
	ft := NewCasperFinalityTracker(32)
	ft.SetFinalized(CasperCheckpoint{Epoch: 3})

	if d := ft.FinalityDelay(10); d != 7 {
		t.Errorf("expected finality delay 7, got %d", d)
	}
	if d := ft.FinalityDelay(3); d != 0 {
		t.Errorf("expected finality delay 0 when current == finalized, got %d", d)
	}
	if d := ft.FinalityDelay(1); d != 0 {
		t.Errorf("expected finality delay 0 when current < finalized, got %d", d)
	}
}

// TestFullJustificationAndFinalization runs a multi-epoch scenario through
// justification and finalization.
func TestFullJustificationAndFinalization(t *testing.T) {
	ft := NewCasperFinalityTracker(32)
	state := makeStateWithValidators(100, 32*GweiPerETH, 32)

	totalWeight := uint64(100 * 32 * GweiPerETH)
	// Needs voteWeight*3 >= totalWeight*2 -- use slightly above 2/3.
	supermajority := totalWeight*2/3 + 1

	// Process epochs 2-5 with supermajority attestation using the combined
	// method that correctly captures old checkpoint values before rotation.
	for epoch := Epoch(2); epoch <= 5; epoch++ {
		state.Slot = uint64(epoch) * 32
		err := ft.ProcessJustificationAndFinalization(epoch, state, supermajority, supermajority, totalWeight)
		if err != nil {
			t.Fatalf("epoch %d justification+finalization: %v", epoch, err)
		}
	}

	// After consecutive justification, finalization should have progressed.
	fin := ft.GetFinalizedCheckpoint()
	if fin.Epoch == 0 {
		t.Error("expected finalization to progress beyond genesis after 4 justified epochs")
	}
}

func TestJustificationBitsShiftOnProcessing(t *testing.T) {
	ft := NewCasperFinalityTracker(32)
	state := makeStateWithValidators(10, 32*GweiPerETH, 32)
	state.Slot = 3 * 32

	totalWeight := uint64(10 * 32 * GweiPerETH)
	// Below supermajority so nothing gets justified.
	belowMajority := totalWeight / 3

	ft.SetJustificationBits([4]bool{true, false, true, false})

	err := ft.ProcessJustificationWithWeights(3, state, belowMajority, belowMajority, totalWeight)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	bits := ft.GetJustificationBits()
	// After shift: old[0]=true -> bits[1], old[2]=true -> bits[3], bits[0]=false.
	if bits[0] {
		t.Error("expected bit 0 to be false after shift")
	}
	if !bits[1] {
		t.Error("expected bit 1 to be true (shifted from bit 0)")
	}
	if bits[2] {
		t.Error("expected bit 2 to be false (shifted from bit 1)")
	}
	if !bits[3] {
		t.Error("expected bit 3 to be true (shifted from bit 2)")
	}
}

func TestConcurrentAccessFinalityRules(t *testing.T) {
	ft := NewCasperFinalityTracker(32)
	done := make(chan struct{})

	go func() {
		for i := 0; i < 100; i++ {
			ft.GetFinalizedCheckpoint()
			ft.GetJustifiedCheckpoint()
			ft.FinalityDelay(10)
			ft.IsFinalized(CasperCheckpoint{Epoch: 5})
		}
		close(done)
	}()

	for i := 0; i < 100; i++ {
		ft.SetJustified(CasperCheckpoint{Epoch: Epoch(i)})
		ft.SetFinalized(CasperCheckpoint{Epoch: Epoch(i)})
	}

	<-done
}
