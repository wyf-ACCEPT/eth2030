package crypto

import (
	"math/big"
	"testing"
)

func TestNewBLSBatchAggregator(t *testing.T) {
	ba := NewBLSBatchAggregator()
	if ba.Pending() != 0 {
		t.Errorf("Pending() = %d, want 0", ba.Pending())
	}
	if ba.IsClosed() {
		t.Error("new aggregator should not be closed")
	}
}

func TestBatchAggregatorSubmit(t *testing.T) {
	ba := NewBLSBatchAggregator()

	secret := big.NewInt(42)
	pk := BLSPubkeyFromSecret(secret)
	msg := []byte("test")
	sig := BLSSign(secret, msg)

	err := ba.Submit(BatchVerifyEntry{
		PubKey:    pk,
		Message:   msg,
		Signature: sig,
		Tag:       "entry1",
	})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if ba.Pending() != 1 {
		t.Errorf("Pending() = %d, want 1", ba.Pending())
	}
}

func TestBatchAggregatorSubmitDuplicate(t *testing.T) {
	ba := NewBLSBatchAggregator()

	entry := BatchVerifyEntry{Tag: "dup"}
	ba.Submit(entry)
	err := ba.Submit(entry)
	if err != ErrBatchAlreadyAdded {
		t.Errorf("expected ErrBatchAlreadyAdded, got %v", err)
	}
}

func TestBatchAggregatorSubmitNoTag(t *testing.T) {
	ba := NewBLSBatchAggregator()

	// Entries without tags should always be accepted.
	for i := 0; i < 5; i++ {
		err := ba.Submit(BatchVerifyEntry{Tag: ""})
		if err != nil {
			t.Fatalf("Submit without tag: %v", err)
		}
	}
	if ba.Pending() != 5 {
		t.Errorf("Pending() = %d, want 5", ba.Pending())
	}
}

func TestBatchAggregatorClose(t *testing.T) {
	ba := NewBLSBatchAggregator()
	ba.Close()

	if !ba.IsClosed() {
		t.Error("should be closed after Close()")
	}

	err := ba.Submit(BatchVerifyEntry{Tag: "late"})
	if err != ErrBatchClosed {
		t.Errorf("expected ErrBatchClosed, got %v", err)
	}
}

func TestBatchAggregatorVerifyEmpty(t *testing.T) {
	ba := NewBLSBatchAggregator()
	_, err := ba.VerifyBatch()
	if err != ErrBatchEmpty {
		t.Errorf("expected ErrBatchEmpty, got %v", err)
	}
}

func TestBatchAggregatorVerifyInvalid(t *testing.T) {
	ba := NewBLSBatchAggregator()

	// Submit entry with zero pubkey (invalid).
	var zeroPK [BLSPubkeySize]byte
	var zeroSig [BLSSignatureSize]byte
	ba.Submit(BatchVerifyEntry{
		PubKey:    zeroPK,
		Message:   []byte("bad"),
		Signature: zeroSig,
	})

	ok, err := ba.VerifyBatch()
	if err != nil {
		t.Fatalf("VerifyBatch error: %v", err)
	}
	if ok {
		t.Fatal("batch with invalid entries should not verify")
	}

	// Queue should be drained after verify.
	if ba.Pending() != 0 {
		t.Errorf("Pending() = %d after verify, want 0", ba.Pending())
	}
}

func TestBatchAggregatorVerifyDrainsQueue(t *testing.T) {
	ba := NewBLSBatchAggregator()

	secret := big.NewInt(99)
	pk := BLSPubkeyFromSecret(secret)
	sig := BLSSign(secret, []byte("msg"))
	ba.Submit(BatchVerifyEntry{PubKey: pk, Message: []byte("msg"), Signature: sig})

	ba.VerifyBatch()
	if ba.Pending() != 0 {
		t.Errorf("Pending() = %d after VerifyBatch, want 0", ba.Pending())
	}
}

// --- Weighted Aggregation Tests ---

func TestAggregateWeightedPubkeysEmpty(t *testing.T) {
	_, err := AggregateWeightedPubkeys(nil)
	if err != ErrBatchEmpty {
		t.Errorf("expected ErrBatchEmpty, got %v", err)
	}
}

func TestAggregateWeightedPubkeysZeroWeight(t *testing.T) {
	pk := BLSPubkeyFromSecret(big.NewInt(10))
	_, err := AggregateWeightedPubkeys([]WeightedPubkey{
		{PubKey: pk, Weight: 0},
	})
	if err != ErrBatchWeightZero {
		t.Errorf("expected ErrBatchWeightZero, got %v", err)
	}
}

func TestAggregateWeightedPubkeysInvalid(t *testing.T) {
	var bad [BLSPubkeySize]byte
	_, err := AggregateWeightedPubkeys([]WeightedPubkey{
		{PubKey: bad, Weight: 1},
	})
	if err != ErrBatchInvalidPubkey {
		t.Errorf("expected ErrBatchInvalidPubkey, got %v", err)
	}
}

func TestAggregateWeightedPubkeysValid(t *testing.T) {
	pk1 := BLSPubkeyFromSecret(big.NewInt(10))
	pk2 := BLSPubkeyFromSecret(big.NewInt(20))

	agg, err := AggregateWeightedPubkeys([]WeightedPubkey{
		{PubKey: pk1, Weight: 1},
		{PubKey: pk2, Weight: 2},
	})
	if err != nil {
		t.Fatalf("AggregateWeightedPubkeys: %v", err)
	}

	// Result should be non-zero.
	allZero := true
	for _, b := range agg {
		if b != 0 {
			allZero = false
			break
		}
	}
	if allZero {
		t.Fatal("weighted aggregate should not be zero")
	}
}

func TestAggregateWeightedPubkeysUnitWeight(t *testing.T) {
	// Weight=1 for all should match standard aggregation.
	pk1 := BLSPubkeyFromSecret(big.NewInt(10))
	pk2 := BLSPubkeyFromSecret(big.NewInt(20))

	weighted, err := AggregateWeightedPubkeys([]WeightedPubkey{
		{PubKey: pk1, Weight: 1},
		{PubKey: pk2, Weight: 1},
	})
	if err != nil {
		t.Fatalf("weighted: %v", err)
	}

	standard := AggregatePublicKeys([][BLSPubkeySize]byte{pk1, pk2})
	if weighted != standard {
		t.Fatal("unit-weight aggregation should match standard aggregation")
	}
}

// --- Incremental Aggregator Tests ---

func TestNewIncrementalAggregator(t *testing.T) {
	ia := NewIncrementalAggregator()
	if ia.Count() != 0 {
		t.Errorf("Count() = %d, want 0", ia.Count())
	}
}

func TestIncrementalAggregatorAddValid(t *testing.T) {
	ia := NewIncrementalAggregator()

	secret := big.NewInt(42)
	pk := BLSPubkeyFromSecret(secret)
	sig := BLSSign(secret, []byte("msg"))

	err := ia.Add(pk, sig)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if ia.Count() != 1 {
		t.Errorf("Count() = %d, want 1", ia.Count())
	}
}

func TestIncrementalAggregatorAddDuplicate(t *testing.T) {
	ia := NewIncrementalAggregator()

	secret := big.NewInt(42)
	pk := BLSPubkeyFromSecret(secret)
	sig := BLSSign(secret, []byte("msg"))

	ia.Add(pk, sig)
	err := ia.Add(pk, sig)
	if err != ErrBatchAlreadyAdded {
		t.Errorf("expected ErrBatchAlreadyAdded, got %v", err)
	}
}

func TestIncrementalAggregatorInvalidPubkey(t *testing.T) {
	ia := NewIncrementalAggregator()
	var zeroPK [BLSPubkeySize]byte
	var sig [BLSSignatureSize]byte
	err := ia.Add(zeroPK, sig)
	if err != ErrBatchInvalidPubkey {
		t.Errorf("expected ErrBatchInvalidPubkey, got %v", err)
	}
}

func TestIncrementalAggregatorInvalidSig(t *testing.T) {
	ia := NewIncrementalAggregator()
	pk := BLSPubkeyFromSecret(big.NewInt(42))
	var zeroSig [BLSSignatureSize]byte
	err := ia.Add(pk, zeroSig)
	if err != ErrBatchInvalidSig {
		t.Errorf("expected ErrBatchInvalidSig, got %v", err)
	}
}

func TestIncrementalAggregatorMultiple(t *testing.T) {
	ia := NewIncrementalAggregator()

	secrets := []*big.Int{big.NewInt(10), big.NewInt(20), big.NewInt(30)}
	msg := []byte("same message")

	for _, s := range secrets {
		pk := BLSPubkeyFromSecret(s)
		sig := BLSSign(s, msg)
		err := ia.Add(pk, sig)
		if err != nil {
			t.Fatalf("Add: %v", err)
		}
	}

	if ia.Count() != 3 {
		t.Errorf("Count() = %d, want 3", ia.Count())
	}

	// Aggregate signature should be non-trivial.
	aggSig := ia.AggregateSignature()
	allZero := true
	for _, b := range aggSig {
		if b != 0 {
			allZero = false
			break
		}
	}
	if allZero {
		t.Fatal("aggregate signature should not be zero")
	}

	// Aggregate pubkey should be non-trivial.
	aggPK := ia.AggregatePubkey()
	allZero = true
	for _, b := range aggPK {
		if b != 0 {
			allZero = false
			break
		}
	}
	if allZero {
		t.Fatal("aggregate pubkey should not be zero")
	}
}

func TestIncrementalAggregatorMatchesStandard(t *testing.T) {
	ia := NewIncrementalAggregator()
	msg := []byte("shared msg")

	s1, s2 := big.NewInt(10), big.NewInt(20)
	pk1, pk2 := BLSPubkeyFromSecret(s1), BLSPubkeyFromSecret(s2)
	sig1, sig2 := BLSSign(s1, msg), BLSSign(s2, msg)

	ia.Add(pk1, sig1)
	ia.Add(pk2, sig2)

	// Compare with standard aggregation.
	stdAggSig := AggregateSignatures([][BLSSignatureSize]byte{sig1, sig2})
	stdAggPK := AggregatePublicKeys([][BLSPubkeySize]byte{pk1, pk2})

	if ia.AggregateSignature() != stdAggSig {
		t.Fatal("incremental sig aggregation should match standard")
	}
	if ia.AggregatePubkey() != stdAggPK {
		t.Fatal("incremental pk aggregation should match standard")
	}
}

// --- Threshold Assembler Tests ---

func TestNewThresholdAssembler(t *testing.T) {
	ta := NewThresholdAssembler(3)
	if ta.PartialCount() != 0 {
		t.Errorf("PartialCount() = %d, want 0", ta.PartialCount())
	}
	if ta.IsComplete() {
		t.Error("should not be complete with 0 partials and threshold 3")
	}
}

func TestThresholdAssemblerAddPartial(t *testing.T) {
	ta := NewThresholdAssembler(2)

	sig := BLSSign(big.NewInt(42), []byte("msg"))
	err := ta.AddPartial(0, sig)
	if err != nil {
		t.Fatalf("AddPartial: %v", err)
	}
	if ta.PartialCount() != 1 {
		t.Errorf("PartialCount() = %d, want 1", ta.PartialCount())
	}
}

func TestThresholdAssemblerDuplicate(t *testing.T) {
	ta := NewThresholdAssembler(2)

	sig := BLSSign(big.NewInt(42), []byte("msg"))
	ta.AddPartial(0, sig)
	err := ta.AddPartial(0, sig)
	if err != ErrBatchAlreadyAdded {
		t.Errorf("expected ErrBatchAlreadyAdded, got %v", err)
	}
}

func TestThresholdAssemblerNotMet(t *testing.T) {
	ta := NewThresholdAssembler(3)

	sig := BLSSign(big.NewInt(42), []byte("msg"))
	ta.AddPartial(0, sig)

	_, err := ta.Assemble()
	if err != ErrBatchThresholdNotMet {
		t.Errorf("expected ErrBatchThresholdNotMet, got %v", err)
	}
}

func TestThresholdAssemblerComplete(t *testing.T) {
	ta := NewThresholdAssembler(2)

	secrets := []*big.Int{big.NewInt(10), big.NewInt(20)}
	msg := []byte("threshold msg")

	for i, s := range secrets {
		sig := BLSSign(s, msg)
		ta.AddPartial(i, sig)
	}

	if !ta.IsComplete() {
		t.Fatal("should be complete with 2 partials and threshold 2")
	}

	agg, err := ta.Assemble()
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}

	// Should match standard aggregation.
	sig1 := BLSSign(secrets[0], msg)
	sig2 := BLSSign(secrets[1], msg)
	expected := AggregateSignatures([][BLSSignatureSize]byte{sig1, sig2})
	if agg != expected {
		t.Fatal("assembled signature should match standard aggregation")
	}
}

func TestThresholdAssemblerOverThreshold(t *testing.T) {
	ta := NewThresholdAssembler(2)

	msg := []byte("msg")
	for i := 0; i < 5; i++ {
		sig := BLSSign(big.NewInt(int64(i+1)*10), msg)
		ta.AddPartial(i, sig)
	}

	if ta.PartialCount() != 5 {
		t.Errorf("PartialCount() = %d, want 5", ta.PartialCount())
	}

	// Should still assemble successfully with more than threshold.
	_, err := ta.Assemble()
	if err != nil {
		t.Fatalf("Assemble with surplus: %v", err)
	}
}
