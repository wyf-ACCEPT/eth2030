package trie

import (
	"testing"

	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/crypto"
)

// --- BatchConverter tests ---

func TestBatchConverter_InvalidSize(t *testing.T) {
	_, err := NewBatchConverter(0)
	if err != ErrMigrationInvalidBatch {
		t.Fatalf("expected ErrMigrationInvalidBatch, got %v", err)
	}
}

func TestBatchConverter_ConvertAll(t *testing.T) {
	bc, err := NewBatchConverter(10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	pairs := []migrationPair{
		{key: crypto.Keccak256Hash([]byte("a")), value: []byte("v1")},
		{key: crypto.Keccak256Hash([]byte("b")), value: []byte("v2")},
		{key: crypto.Keccak256Hash([]byte("c")), value: []byte("v3")},
	}

	dest := NewBinaryTrie()
	count, done := bc.ConvertBatch(pairs, dest)
	if count != 3 || !done {
		t.Fatalf("expected 3/true, got %d/%v", count, done)
	}
	if bc.Converted() != 3 {
		t.Errorf("Converted = %d, want 3", bc.Converted())
	}

	val, err := dest.GetHashed(pairs[0].key)
	if err != nil || string(val) != "v1" {
		t.Errorf("expected v1, got %s (err=%v)", val, err)
	}
}

func TestBatchConverter_PartialBatch(t *testing.T) {
	bc, err := NewBatchConverter(2)
	if err != nil {
		t.Fatal(err)
	}

	pairs := []migrationPair{
		{key: crypto.Keccak256Hash([]byte("a")), value: []byte("v1")},
		{key: crypto.Keccak256Hash([]byte("b")), value: []byte("v2")},
		{key: crypto.Keccak256Hash([]byte("c")), value: []byte("v3")},
	}

	dest := NewBinaryTrie()
	count, done := bc.ConvertBatch(pairs, dest)
	if count != 2 || done {
		t.Fatalf("expected 2/false, got %d/%v", count, done)
	}
}

func TestBatchConverter_EmptyInput(t *testing.T) {
	bc, _ := NewBatchConverter(10)
	dest := NewBinaryTrie()
	count, done := bc.ConvertBatch(nil, dest)
	if count != 0 || !done {
		t.Fatalf("expected 0/true, got %d/%v", count, done)
	}
}

// --- AddressSpaceSplitter tests ---

func TestAddressSpaceSplitter_InvalidCount(t *testing.T) {
	_, err := NewAddressSpaceSplitter(0)
	if err != ErrMigrationInvalidSplit {
		t.Fatalf("expected ErrMigrationInvalidSplit, got %v", err)
	}
}

func TestAddressSpaceSplitter_SingleRange(t *testing.T) {
	s, err := NewAddressSpaceSplitter(1)
	if err != nil {
		t.Fatal(err)
	}
	if s.NumRanges() != 1 {
		t.Fatalf("expected 1 range, got %d", s.NumRanges())
	}
	r := s.Ranges()[0]
	// Should cover entire address space.
	if r.Start != (types.Hash{}) {
		t.Error("start should be zero")
	}
	for i := 0; i < 32; i++ {
		if r.End[i] != 0xFF {
			t.Error("end should be all 0xFF")
			break
		}
	}
}

func TestAddressSpaceSplitter_TwoRanges(t *testing.T) {
	s, err := NewAddressSpaceSplitter(2)
	if err != nil {
		t.Fatal(err)
	}
	if s.NumRanges() != 2 {
		t.Fatalf("expected 2 ranges, got %d", s.NumRanges())
	}
	// First range starts at 0x00, second at 0x80.
	if s.Ranges()[0].Start[0] != 0x00 {
		t.Error("first range should start at 0x00")
	}
	if s.Ranges()[1].Start[0] != 0x80 {
		t.Errorf("second range should start at 0x80, got 0x%02x", s.Ranges()[1].Start[0])
	}
}

func TestInRange(t *testing.T) {
	r := AddressRange{
		Start: types.HexToHash("0x8000000000000000000000000000000000000000000000000000000000000000"),
		End:   types.HexToHash("0xFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFF"),
	}
	// Key starting with 0x90 should be in range.
	key := types.HexToHash("0x9000000000000000000000000000000000000000000000000000000000000001")
	if !InRange(key, r) {
		t.Error("expected key to be in range")
	}
	// Key starting with 0x01 should not be in range.
	lowKey := types.HexToHash("0x0100000000000000000000000000000000000000000000000000000000000000")
	if InRange(lowKey, r) {
		t.Error("expected low key to be out of range")
	}
}

// --- StateProofGenerator tests ---

func TestStateProofGenerator_Generate(t *testing.T) {
	tr := New()
	tr.Put([]byte("alpha"), []byte("one"))
	tr.Put([]byte("bravo"), []byte("two"))

	gen := NewStateProofGenerator(tr)
	proof, err := gen.GenerateProof([]byte("alpha"))
	if err != nil {
		t.Fatalf("GenerateProof failed: %v", err)
	}
	if len(proof) == 0 {
		t.Error("expected non-empty proof")
	}
	if gen.ProofCount() != 1 {
		t.Errorf("ProofCount = %d, want 1", gen.ProofCount())
	}

	// Second call should return cached proof.
	proof2, err := gen.GenerateProof([]byte("alpha"))
	if err != nil {
		t.Fatalf("cached GenerateProof failed: %v", err)
	}
	if len(proof2) != len(proof) {
		t.Error("cached proof should match")
	}
	if gen.ProofCount() != 1 {
		t.Error("cache should not grow on re-request")
	}
}

// --- MigrationCheckpointer tests ---

func TestMigrationCheckpointer_SaveAndRestore(t *testing.T) {
	mc := NewMigrationCheckpointer()
	_, err := mc.Latest()
	if err != ErrMigrationNoCheckpoint {
		t.Fatalf("expected ErrMigrationNoCheckpoint, got %v", err)
	}

	mc.Save(MigrationCheckpoint{KeysMigrated: 100, BatchNumber: 1})
	mc.Save(MigrationCheckpoint{KeysMigrated: 200, BatchNumber: 2})

	cp, err := mc.Latest()
	if err != nil {
		t.Fatal(err)
	}
	if cp.KeysMigrated != 200 || cp.BatchNumber != 2 {
		t.Errorf("unexpected checkpoint: %+v", cp)
	}
	if mc.Count() != 2 {
		t.Errorf("Count = %d, want 2", mc.Count())
	}
}

// --- GasAccountant tests ---

func TestGasAccountant_Basic(t *testing.T) {
	ga := NewGasAccountant(10000, 100, 500, 300)
	if err := ga.ChargeRead(); err != nil {
		t.Fatal(err)
	}
	if ga.TotalGas() != 100 {
		t.Errorf("TotalGas = %d, want 100", ga.TotalGas())
	}
	if ga.Remaining() != 9900 {
		t.Errorf("Remaining = %d, want 9900", ga.Remaining())
	}
}

func TestGasAccountant_ExceedsBudget(t *testing.T) {
	ga := NewGasAccountant(500, 100, 500, 300)
	if err := ga.ChargeRead(); err != nil {
		t.Fatal(err)
	}
	if err := ga.ChargeWrite(); err == nil {
		t.Fatal("expected ErrMigrationGasExceeded")
	}
}

func TestGasAccountant_NoBudget(t *testing.T) {
	ga := NewGasAccountant(0, 100, 500, 300)
	// No budget means unlimited.
	for i := 0; i < 100; i++ {
		if err := ga.ChargeRead(); err != nil {
			t.Fatalf("unexpected error with no budget: %v", err)
		}
	}
	if ga.Remaining() != 0 {
		t.Error("Remaining should be 0 with no budget")
	}
}

func TestGasAccountant_ChargeProof(t *testing.T) {
	ga := NewGasAccountant(10000, 100, 500, 3000)
	if err := ga.ChargeProof(); err != nil {
		t.Fatal(err)
	}
	if ga.TotalGas() != 3000 {
		t.Errorf("TotalGas = %d, want 3000", ga.TotalGas())
	}
}

// --- MPTToVerkleTrieMigrator tests ---

func TestMPTToVerkleTrieMigrator_NilSource(t *testing.T) {
	_, err := NewMPTToVerkleTrieMigrator(nil, 10, 0)
	if err != ErrMigrationNilSource {
		t.Fatalf("expected ErrMigrationNilSource, got %v", err)
	}
}

func TestMPTToVerkleTrieMigrator_FullMigration(t *testing.T) {
	tr := New()
	tr.Put([]byte("alpha"), []byte("one"))
	tr.Put([]byte("bravo"), []byte("two"))
	tr.Put([]byte("charlie"), []byte("three"))

	m, err := NewMPTToVerkleTrieMigrator(tr, 2, 0)
	if err != nil {
		t.Fatal(err)
	}

	// First batch: 2 keys.
	count, done, err := m.MigrateBatch()
	if err != nil {
		t.Fatalf("batch 1 error: %v", err)
	}
	if count != 2 || done {
		t.Fatalf("batch 1: expected 2/false, got %d/%v", count, done)
	}

	// Second batch: 1 key remaining.
	count, done, err = m.MigrateBatch()
	if err != nil {
		t.Fatalf("batch 2 error: %v", err)
	}
	if count != 1 || !done {
		t.Fatalf("batch 2: expected 1/true, got %d/%v", count, done)
	}

	if m.KeysMigrated() != 3 {
		t.Errorf("KeysMigrated = %d, want 3", m.KeysMigrated())
	}

	// Verify checkpoints were saved.
	if m.Checkpointer().Count() != 2 {
		t.Errorf("expected 2 checkpoints, got %d", m.Checkpointer().Count())
	}
}

func TestMPTToVerkleTrieMigrator_AlreadyDone(t *testing.T) {
	tr := New()
	tr.Put([]byte("x"), []byte("y"))

	m, _ := NewMPTToVerkleTrieMigrator(tr, 100, 0)
	m.MigrateBatch()
	_, _, err := m.MigrateBatch()
	if err != ErrMigrationAlreadyDone {
		t.Fatalf("expected ErrMigrationAlreadyDone, got %v", err)
	}
}

func TestMPTToVerkleTrieMigrator_EmptySource(t *testing.T) {
	tr := New()
	m, _ := NewMPTToVerkleTrieMigrator(tr, 10, 0)
	count, done, err := m.MigrateBatch()
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 || !done {
		t.Fatalf("expected 0/true, got %d/%v", count, done)
	}
}

func TestMPTToVerkleTrieMigrator_GasExhaustion(t *testing.T) {
	tr := New()
	for i := 0; i < 10; i++ {
		tr.Put([]byte{byte(i)}, []byte{byte(i + 1)})
	}

	// Very small gas budget: 200 read + 5000 write = 5200 per key.
	// Budget of 10000 allows ~1 key.
	m, err := NewMPTToVerkleTrieMigrator(tr, 5, 10000)
	if err != nil {
		t.Fatal(err)
	}

	_, _, err = m.MigrateBatch()
	if err == nil {
		t.Fatal("expected gas exceeded error")
	}
}
