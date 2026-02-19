package consensus

import (
	"sync"
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

// makePreconf builds a valid Preconfirmation with a correct commitment.
func makePreconf(slot, validator uint64, txHash types.Hash) *Preconfirmation {
	commitment := ComputeCommitment(slot, validator, txHash)
	return &Preconfirmation{
		Slot:           slot,
		ValidatorIndex: validator,
		TxHash:         txHash,
		Commitment:     commitment,
		Signature:      []byte{0x01, 0x02, 0x03},
		Timestamp:      1000,
	}
}

func txHash(b byte) types.Hash {
	var h types.Hash
	h[0] = b
	return h
}

func TestNewPrequorumEngine_Defaults(t *testing.T) {
	pe := NewPrequorumEngine(PrequorumConfig{})
	if pe.config.QuorumThreshold != DefaultQuorumThreshold {
		t.Errorf("threshold = %f, want %f", pe.config.QuorumThreshold, DefaultQuorumThreshold)
	}
	if pe.config.MaxPreconfsPerSlot != 10_000 {
		t.Errorf("max preconfs = %d, want 10000", pe.config.MaxPreconfsPerSlot)
	}
	if pe.config.ValidatorSetSize != 1_000 {
		t.Errorf("validator set = %d, want 1000", pe.config.ValidatorSetSize)
	}
}

func TestNewPrequorumEngine_Custom(t *testing.T) {
	pe := NewPrequorumEngine(PrequorumConfig{
		QuorumThreshold:    0.5,
		MaxPreconfsPerSlot: 100,
		ValidatorSetSize:   200,
	})
	if pe.config.QuorumThreshold != 0.5 {
		t.Errorf("threshold = %f, want 0.5", pe.config.QuorumThreshold)
	}
	if pe.config.MaxPreconfsPerSlot != 100 {
		t.Errorf("max preconfs = %d, want 100", pe.config.MaxPreconfsPerSlot)
	}
}

func TestNewPrequorumEngine_InvalidThreshold(t *testing.T) {
	// Negative threshold should be corrected to default.
	pe := NewPrequorumEngine(PrequorumConfig{QuorumThreshold: -0.5})
	if pe.config.QuorumThreshold != DefaultQuorumThreshold {
		t.Errorf("negative threshold not corrected: %f", pe.config.QuorumThreshold)
	}
	// Zero threshold should be corrected.
	pe2 := NewPrequorumEngine(PrequorumConfig{QuorumThreshold: 0})
	if pe2.config.QuorumThreshold != DefaultQuorumThreshold {
		t.Errorf("zero threshold not corrected: %f", pe2.config.QuorumThreshold)
	}
}

func TestDefaultPrequorumConfig(t *testing.T) {
	cfg := DefaultPrequorumConfig()
	if cfg.QuorumThreshold != DefaultQuorumThreshold {
		t.Errorf("threshold = %f, want %f", cfg.QuorumThreshold, DefaultQuorumThreshold)
	}
	if cfg.ValidatorSetSize == 0 {
		t.Error("validator set size should not be zero")
	}
}

func TestValidatePreconfirmation_Nil(t *testing.T) {
	pe := NewPrequorumEngine(DefaultPrequorumConfig())
	if err := pe.ValidatePreconfirmation(nil); err != ErrNilPreconfirmation {
		t.Errorf("expected ErrNilPreconfirmation, got %v", err)
	}
}

func TestValidatePreconfirmation_InvalidSlot(t *testing.T) {
	pe := NewPrequorumEngine(DefaultPrequorumConfig())
	pc := makePreconf(0, 1, txHash(0x01))
	pc.Slot = 0 // force zero slot
	err := pe.ValidatePreconfirmation(pc)
	if err != ErrPrequorumInvalidSlot {
		t.Errorf("expected ErrPrequorumInvalidSlot, got %v", err)
	}
}

func TestValidatePreconfirmation_EmptyTxHash(t *testing.T) {
	pe := NewPrequorumEngine(DefaultPrequorumConfig())
	pc := makePreconf(1, 1, types.Hash{})
	// TxHash is zero, so validation should fail before commitment check.
	err := pe.ValidatePreconfirmation(pc)
	if err != ErrEmptyTxHash {
		t.Errorf("expected ErrEmptyTxHash, got %v", err)
	}
}

func TestValidatePreconfirmation_EmptyCommitment(t *testing.T) {
	pe := NewPrequorumEngine(DefaultPrequorumConfig())
	pc := &Preconfirmation{
		Slot:       1,
		TxHash:     txHash(0x01),
		Commitment: types.Hash{}, // zero commitment
		Signature:  []byte{0x01},
	}
	err := pe.ValidatePreconfirmation(pc)
	if err != ErrEmptyCommitment {
		t.Errorf("expected ErrEmptyCommitment, got %v", err)
	}
}

func TestValidatePreconfirmation_EmptySignature(t *testing.T) {
	pe := NewPrequorumEngine(DefaultPrequorumConfig())
	commitment := ComputeCommitment(1, 0, txHash(0x01))
	pc := &Preconfirmation{
		Slot:       1,
		TxHash:     txHash(0x01),
		Commitment: commitment,
		Signature:  nil,
	}
	err := pe.ValidatePreconfirmation(pc)
	if err != ErrEmptySignature {
		t.Errorf("expected ErrEmptySignature, got %v", err)
	}
}

func TestValidatePreconfirmation_InvalidCommitment(t *testing.T) {
	pe := NewPrequorumEngine(DefaultPrequorumConfig())
	pc := &Preconfirmation{
		Slot:       1,
		TxHash:     txHash(0x01),
		Commitment: txHash(0xFF), // wrong commitment
		Signature:  []byte{0x01},
	}
	err := pe.ValidatePreconfirmation(pc)
	if err != ErrInvalidCommitment {
		t.Errorf("expected ErrInvalidCommitment, got %v", err)
	}
}

func TestValidatePreconfirmation_Valid(t *testing.T) {
	pe := NewPrequorumEngine(DefaultPrequorumConfig())
	pc := makePreconf(1, 5, txHash(0xAB))
	if err := pe.ValidatePreconfirmation(pc); err != nil {
		t.Errorf("valid preconfirmation rejected: %v", err)
	}
}

func TestSubmitPreconfirmation_Basic(t *testing.T) {
	pe := NewPrequorumEngine(DefaultPrequorumConfig())
	pc := makePreconf(1, 0, txHash(0x01))

	if err := pe.SubmitPreconfirmation(pc); err != nil {
		t.Fatalf("submit failed: %v", err)
	}

	pcs := pe.GetPreconfirmations(1)
	if len(pcs) != 1 {
		t.Fatalf("expected 1 preconf, got %d", len(pcs))
	}
	if pcs[0].ValidatorIndex != 0 {
		t.Errorf("validator index = %d, want 0", pcs[0].ValidatorIndex)
	}
}

func TestSubmitPreconfirmation_Duplicate(t *testing.T) {
	pe := NewPrequorumEngine(DefaultPrequorumConfig())
	pc := makePreconf(1, 0, txHash(0x01))

	if err := pe.SubmitPreconfirmation(pc); err != nil {
		t.Fatalf("first submit failed: %v", err)
	}
	if err := pe.SubmitPreconfirmation(pc); err != ErrDuplicatePreconf {
		t.Errorf("expected ErrDuplicatePreconf, got %v", err)
	}
}

func TestSubmitPreconfirmation_SameValidatorDiffTx(t *testing.T) {
	pe := NewPrequorumEngine(DefaultPrequorumConfig())
	pc1 := makePreconf(1, 0, txHash(0x01))
	pc2 := makePreconf(1, 0, txHash(0x02))

	if err := pe.SubmitPreconfirmation(pc1); err != nil {
		t.Fatalf("submit pc1: %v", err)
	}
	if err := pe.SubmitPreconfirmation(pc2); err != nil {
		t.Fatalf("submit pc2: %v", err)
	}

	pcs := pe.GetPreconfirmations(1)
	if len(pcs) != 2 {
		t.Fatalf("expected 2 preconfs, got %d", len(pcs))
	}
}

func TestSubmitPreconfirmation_SlotFull(t *testing.T) {
	pe := NewPrequorumEngine(PrequorumConfig{
		QuorumThreshold:    0.67,
		MaxPreconfsPerSlot: 2,
		ValidatorSetSize:   100,
	})

	for i := uint64(0); i < 2; i++ {
		pc := makePreconf(1, i, txHash(byte(i+1)))
		if err := pe.SubmitPreconfirmation(pc); err != nil {
			t.Fatalf("submit %d: %v", i, err)
		}
	}

	pc := makePreconf(1, 99, txHash(0x99))
	if err := pe.SubmitPreconfirmation(pc); err != ErrSlotFull {
		t.Errorf("expected ErrSlotFull, got %v", err)
	}
}

func TestGetPreconfirmations_EmptySlot(t *testing.T) {
	pe := NewPrequorumEngine(DefaultPrequorumConfig())
	pcs := pe.GetPreconfirmations(999)
	if pcs != nil {
		t.Errorf("expected nil for empty slot, got %v", pcs)
	}
}

func TestCheckPrequorum_Empty(t *testing.T) {
	pe := NewPrequorumEngine(DefaultPrequorumConfig())
	status := pe.CheckPrequorum(1)
	if status.Slot != 1 {
		t.Errorf("slot = %d, want 1", status.Slot)
	}
	if status.TotalPreconfs != 0 || status.UniqueValidators != 0 {
		t.Error("expected zero preconfs and validators for empty slot")
	}
	if status.QuorumReached {
		t.Error("quorum should not be reached for empty slot")
	}
	if status.Confidence != 0 {
		t.Errorf("confidence = %f, want 0", status.Confidence)
	}
}

func TestCheckPrequorum_BelowThreshold(t *testing.T) {
	pe := NewPrequorumEngine(PrequorumConfig{
		QuorumThreshold:    0.67,
		MaxPreconfsPerSlot: 10_000,
		ValidatorSetSize:   10,
	})

	// Submit preconfs from 6 out of 10 validators (60% < 67%).
	for i := uint64(0); i < 6; i++ {
		pc := makePreconf(1, i, txHash(byte(i+1)))
		if err := pe.SubmitPreconfirmation(pc); err != nil {
			t.Fatalf("submit %d: %v", i, err)
		}
	}

	status := pe.CheckPrequorum(1)
	if status.QuorumReached {
		t.Error("quorum should not be reached at 60%")
	}
	if status.UniqueValidators != 6 {
		t.Errorf("unique validators = %d, want 6", status.UniqueValidators)
	}
	if status.Confidence != 0.6 {
		t.Errorf("confidence = %f, want 0.6", status.Confidence)
	}
}

func TestCheckPrequorum_ReachesThreshold(t *testing.T) {
	pe := NewPrequorumEngine(PrequorumConfig{
		QuorumThreshold:    0.67,
		MaxPreconfsPerSlot: 10_000,
		ValidatorSetSize:   10,
	})

	// Submit from 7 out of 10 validators (70% >= 67%).
	for i := uint64(0); i < 7; i++ {
		pc := makePreconf(1, i, txHash(byte(i+1)))
		if err := pe.SubmitPreconfirmation(pc); err != nil {
			t.Fatalf("submit %d: %v", i, err)
		}
	}

	status := pe.CheckPrequorum(1)
	if !status.QuorumReached {
		t.Error("quorum should be reached at 70%")
	}
	if status.TotalPreconfs != 7 {
		t.Errorf("total preconfs = %d, want 7", status.TotalPreconfs)
	}
	if status.Confidence != 0.7 {
		t.Errorf("confidence = %f, want 0.7", status.Confidence)
	}
}

func TestCheckPrequorum_MultiplePreconfsFromSameValidator(t *testing.T) {
	pe := NewPrequorumEngine(PrequorumConfig{
		QuorumThreshold:    0.5,
		MaxPreconfsPerSlot: 10_000,
		ValidatorSetSize:   10,
	})

	// One validator submits preconfs for 5 different txs.
	for i := byte(1); i <= 5; i++ {
		pc := makePreconf(1, 0, txHash(i))
		if err := pe.SubmitPreconfirmation(pc); err != nil {
			t.Fatalf("submit: %v", err)
		}
	}

	status := pe.CheckPrequorum(1)
	if status.TotalPreconfs != 5 {
		t.Errorf("total preconfs = %d, want 5", status.TotalPreconfs)
	}
	if status.UniqueValidators != 1 {
		t.Errorf("unique validators = %d, want 1", status.UniqueValidators)
	}
	if status.QuorumReached {
		t.Error("quorum should not be reached with 1/10 validators")
	}
}

func TestGetConfirmedTxs(t *testing.T) {
	pe := NewPrequorumEngine(DefaultPrequorumConfig())

	tx1 := txHash(0x01)
	tx2 := txHash(0x02)

	if err := pe.SubmitPreconfirmation(makePreconf(1, 0, tx1)); err != nil {
		t.Fatal(err)
	}
	if err := pe.SubmitPreconfirmation(makePreconf(1, 1, tx2)); err != nil {
		t.Fatal(err)
	}
	// Same tx from another validator.
	if err := pe.SubmitPreconfirmation(makePreconf(1, 2, tx1)); err != nil {
		t.Fatal(err)
	}

	txs := pe.GetConfirmedTxs(1)
	if len(txs) != 2 {
		t.Fatalf("expected 2 confirmed txs, got %d", len(txs))
	}

	found := make(map[types.Hash]bool)
	for _, h := range txs {
		found[h] = true
	}
	if !found[tx1] || !found[tx2] {
		t.Error("missing expected tx hashes")
	}
}

func TestGetConfirmedTxs_EmptySlot(t *testing.T) {
	pe := NewPrequorumEngine(DefaultPrequorumConfig())
	txs := pe.GetConfirmedTxs(999)
	if txs != nil {
		t.Errorf("expected nil for empty slot, got %v", txs)
	}
}

func TestPurgeSlot(t *testing.T) {
	pe := NewPrequorumEngine(DefaultPrequorumConfig())

	if err := pe.SubmitPreconfirmation(makePreconf(1, 0, txHash(0x01))); err != nil {
		t.Fatal(err)
	}
	if err := pe.SubmitPreconfirmation(makePreconf(2, 0, txHash(0x02))); err != nil {
		t.Fatal(err)
	}

	pe.PurgeSlot(1)

	// Slot 1 should be empty now.
	if pcs := pe.GetPreconfirmations(1); pcs != nil {
		t.Errorf("slot 1 should be purged, got %d preconfs", len(pcs))
	}
	// Slot 2 should still exist.
	if pcs := pe.GetPreconfirmations(2); len(pcs) != 1 {
		t.Errorf("slot 2 should have 1 preconf, got %d", len(pcs))
	}
}

func TestPurgeSlot_NonExistent(t *testing.T) {
	pe := NewPrequorumEngine(DefaultPrequorumConfig())
	// Should not panic.
	pe.PurgeSlot(999)
}

func TestComputeCommitment_Deterministic(t *testing.T) {
	h1 := ComputeCommitment(1, 2, txHash(0xAB))
	h2 := ComputeCommitment(1, 2, txHash(0xAB))
	if h1 != h2 {
		t.Error("commitment should be deterministic")
	}

	h3 := ComputeCommitment(1, 3, txHash(0xAB))
	if h1 == h3 {
		t.Error("different validator should produce different commitment")
	}

	h4 := ComputeCommitment(2, 2, txHash(0xAB))
	if h1 == h4 {
		t.Error("different slot should produce different commitment")
	}
}

func TestPrequorumEngine_MultipleSlots(t *testing.T) {
	pe := NewPrequorumEngine(PrequorumConfig{
		QuorumThreshold:    0.5,
		MaxPreconfsPerSlot: 10_000,
		ValidatorSetSize:   4,
	})

	// Slot 10: 3 validators.
	for i := uint64(0); i < 3; i++ {
		pc := makePreconf(10, i, txHash(byte(i+1)))
		if err := pe.SubmitPreconfirmation(pc); err != nil {
			t.Fatalf("slot 10 submit: %v", err)
		}
	}

	// Slot 20: 1 validator.
	if err := pe.SubmitPreconfirmation(makePreconf(20, 0, txHash(0x01))); err != nil {
		t.Fatalf("slot 20 submit: %v", err)
	}

	s10 := pe.CheckPrequorum(10)
	if !s10.QuorumReached {
		t.Error("slot 10: quorum should be reached (3/4 = 75% >= 50%)")
	}

	s20 := pe.CheckPrequorum(20)
	if s20.QuorumReached {
		t.Error("slot 20: quorum should not be reached (1/4 = 25% < 50%)")
	}
}

func TestPrequorumEngine_Concurrent(t *testing.T) {
	pe := NewPrequorumEngine(PrequorumConfig{
		QuorumThreshold:    0.5,
		MaxPreconfsPerSlot: 10_000,
		ValidatorSetSize:   100,
	})

	var wg sync.WaitGroup
	for i := uint64(0); i < 50; i++ {
		wg.Add(1)
		go func(idx uint64) {
			defer wg.Done()
			pc := makePreconf(1, idx, txHash(byte(idx+1)))
			// Ignore errors; we just want no data races.
			_ = pe.SubmitPreconfirmation(pc)
		}(i)
	}
	wg.Wait()

	status := pe.CheckPrequorum(1)
	if status.UniqueValidators != 50 {
		t.Errorf("unique validators = %d, want 50", status.UniqueValidators)
	}
	if !status.QuorumReached {
		t.Error("quorum should be reached (50/100 = 50%)")
	}
}

func TestPrequorumEngine_ConcurrentReadWrite(t *testing.T) {
	pe := NewPrequorumEngine(PrequorumConfig{
		QuorumThreshold:    0.5,
		MaxPreconfsPerSlot: 10_000,
		ValidatorSetSize:   100,
	})

	var wg sync.WaitGroup

	// Writers.
	for i := uint64(0); i < 20; i++ {
		wg.Add(1)
		go func(idx uint64) {
			defer wg.Done()
			pc := makePreconf(5, idx, txHash(byte(idx+1)))
			_ = pe.SubmitPreconfirmation(pc)
		}(i)
	}

	// Readers.
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = pe.CheckPrequorum(5)
			_ = pe.GetPreconfirmations(5)
			_ = pe.GetConfirmedTxs(5)
		}()
	}

	wg.Wait()
}

func TestSubmitPreconfirmation_ValidationErrors(t *testing.T) {
	pe := NewPrequorumEngine(DefaultPrequorumConfig())

	// Nil.
	if err := pe.SubmitPreconfirmation(nil); err != ErrNilPreconfirmation {
		t.Errorf("nil: expected ErrNilPreconfirmation, got %v", err)
	}

	// Bad slot.
	pc := makePreconf(0, 0, txHash(0x01))
	pc.Slot = 0
	if err := pe.SubmitPreconfirmation(pc); err != ErrPrequorumInvalidSlot {
		t.Errorf("bad slot: expected ErrPrequorumInvalidSlot, got %v", err)
	}
}
