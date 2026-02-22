package witness

import (
	"errors"
	"sync"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

func testAddress(b byte) types.Address {
	var addr types.Address
	addr[0] = b
	return addr
}

func testHash(b byte) types.Hash {
	var h types.Hash
	h[0] = b
	return h
}

// 1. NewWitnessProducer creates a valid producer with config.
func TestNewWitnessProducer(t *testing.T) {
	cfg := DefaultProducerConfig()
	wp := NewWitnessProducer(cfg)
	if wp == nil {
		t.Fatal("expected non-nil WitnessProducer")
	}
	if wp.config.MaxWitnessSize != DefaultMaxWitnessSize {
		t.Fatalf("expected max size %d, got %d", DefaultMaxWitnessSize, wp.config.MaxWitnessSize)
	}
	if !wp.config.IncludeStorageProofs {
		t.Fatal("expected IncludeStorageProofs to be true")
	}
	if !wp.config.IncludeCode {
		t.Fatal("expected IncludeCode to be true")
	}
}

// 2. ProduceWitness fails before BeginBlock.
func TestProduceWitness_NotStarted(t *testing.T) {
	wp := NewWitnessProducer(DefaultProducerConfig())
	_, err := wp.ProduceWitness()
	if !errors.Is(err, ErrWitnessNotStarted) {
		t.Fatalf("expected ErrWitnessNotStarted, got %v", err)
	}
}

// 3. ProduceWitness fails when no accesses recorded.
func TestProduceWitness_NoAccess(t *testing.T) {
	wp := NewWitnessProducer(DefaultProducerConfig())
	wp.BeginBlock(100, testHash(0xAA))
	_, err := wp.ProduceWitness()
	if !errors.Is(err, ErrWitnessNoAccess) {
		t.Fatalf("expected ErrWitnessNoAccess, got %v", err)
	}
}

// 4. BeginBlock sets block number and state root.
func TestBeginBlock(t *testing.T) {
	wp := NewWitnessProducer(DefaultProducerConfig())
	root := testHash(0xBB)
	wp.BeginBlock(42, root)

	if !wp.IsStarted() {
		t.Fatal("expected IsStarted true after BeginBlock")
	}
	if wp.BlockNumber() != 42 {
		t.Fatalf("expected block number 42, got %d", wp.BlockNumber())
	}
}

// 5. RecordAccountAccess records field accesses.
func TestRecordAccountAccess(t *testing.T) {
	wp := NewWitnessProducer(DefaultProducerConfig())
	wp.BeginBlock(1, testHash(0x01))

	addr := testAddress(0x01)
	wp.RecordAccountAccess(addr, []string{"nonce", "balance"})

	if !wp.HasAccountAccess(addr) {
		t.Fatal("expected account access to be recorded")
	}
	if wp.AccountAccessCount() != 1 {
		t.Fatalf("expected 1 account, got %d", wp.AccountAccessCount())
	}
}

// 6. RecordStorageAccess records storage key accesses.
func TestRecordStorageAccess(t *testing.T) {
	wp := NewWitnessProducer(DefaultProducerConfig())
	wp.BeginBlock(1, testHash(0x01))

	addr := testAddress(0x02)
	key := testHash(0xCC)
	wp.RecordStorageAccess(addr, key)

	if wp.StorageAccessCount() != 1 {
		t.Fatalf("expected 1 storage key, got %d", wp.StorageAccessCount())
	}
}

// 7. RecordCodeAccess marks code as accessed.
func TestRecordCodeAccess(t *testing.T) {
	wp := NewWitnessProducer(DefaultProducerConfig())
	wp.BeginBlock(1, testHash(0x01))

	addr := testAddress(0x03)
	wp.RecordCodeAccess(addr)

	pw, err := wp.ProduceWitness()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !pw.CodeChunks[addr] {
		t.Fatal("expected code access to be recorded")
	}
	rec := pw.AccessedAccounts[addr]
	if rec == nil || !rec.CodeAccessed {
		t.Fatal("expected CodeAccessed flag to be true")
	}
	if !rec.Fields["code"] {
		t.Fatal("expected 'code' field to be recorded")
	}
}

// 8. ProduceWitness returns correct witness with all fields.
func TestProduceWitness_Success(t *testing.T) {
	wp := NewWitnessProducer(DefaultProducerConfig())
	root := testHash(0xDD)
	wp.BeginBlock(100, root)

	addr1 := testAddress(0x10)
	addr2 := testAddress(0x20)
	key1 := testHash(0x01)
	key2 := testHash(0x02)

	wp.RecordAccountAccess(addr1, []string{"nonce", "balance"})
	wp.RecordStorageAccess(addr1, key1)
	wp.RecordStorageAccess(addr1, key2)
	wp.RecordAccountAccess(addr2, []string{"codehash"})
	wp.RecordCodeAccess(addr2)

	pw, err := wp.ProduceWitness()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pw.BlockNumber != 100 {
		t.Fatalf("expected block number 100, got %d", pw.BlockNumber)
	}
	if pw.StateRoot != root {
		t.Fatal("state root mismatch")
	}
	if pw.AccountCount != 2 {
		t.Fatalf("expected 2 accounts, got %d", pw.AccountCount)
	}
	if pw.StorageKeyCount != 2 {
		t.Fatalf("expected 2 storage keys, got %d", pw.StorageKeyCount)
	}
	if len(pw.StorageProofs[addr1]) != 2 {
		t.Fatalf("expected 2 storage proofs for addr1, got %d", len(pw.StorageProofs[addr1]))
	}
	if !pw.CodeChunks[addr2] {
		t.Fatal("expected code chunk for addr2")
	}
}

// 9. WitnessSize returns non-zero for a valid witness.
func TestWitnessSize_NonZero(t *testing.T) {
	wp := NewWitnessProducer(DefaultProducerConfig())
	wp.BeginBlock(1, testHash(0x01))
	wp.RecordAccountAccess(testAddress(0x01), []string{"balance"})
	wp.RecordStorageAccess(testAddress(0x01), testHash(0xAA))

	pw, err := wp.ProduceWitness()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	size := WitnessSize(pw)
	if size <= 0 {
		t.Fatalf("expected positive size, got %d", size)
	}
}

// 10. WitnessSize returns zero for nil witness.
func TestWitnessSize_Nil(t *testing.T) {
	if WitnessSize(nil) != 0 {
		t.Fatal("expected 0 for nil witness")
	}
}

// 11. Witness too large triggers error.
func TestProduceWitness_TooLarge(t *testing.T) {
	cfg := WitnessProducerConfig{
		MaxWitnessSize:       1, // impossibly small limit
		IncludeStorageProofs: true,
		IncludeCode:          true,
	}
	wp := NewWitnessProducer(cfg)
	wp.BeginBlock(1, testHash(0x01))
	wp.RecordAccountAccess(testAddress(0x01), []string{"balance", "nonce"})

	_, err := wp.ProduceWitness()
	if !errors.Is(err, ErrWitnessTooLarge) {
		t.Fatalf("expected ErrWitnessTooLarge, got %v", err)
	}
}

// 12. Reset clears all state.
func TestReset(t *testing.T) {
	wp := NewWitnessProducer(DefaultProducerConfig())
	wp.BeginBlock(1, testHash(0x01))
	wp.RecordAccountAccess(testAddress(0x01), []string{"nonce"})

	wp.Reset()

	if wp.IsStarted() {
		t.Fatal("expected IsStarted false after Reset")
	}
	if wp.AccountAccessCount() != 0 {
		t.Fatalf("expected 0 accounts after reset, got %d", wp.AccountAccessCount())
	}
	_, err := wp.ProduceWitness()
	if !errors.Is(err, ErrWitnessNotStarted) {
		t.Fatalf("expected ErrWitnessNotStarted after reset, got %v", err)
	}
}

// 13. BeginBlock resets previous block recordings.
func TestBeginBlock_ResetsState(t *testing.T) {
	wp := NewWitnessProducer(DefaultProducerConfig())
	wp.BeginBlock(1, testHash(0x01))
	wp.RecordAccountAccess(testAddress(0x01), []string{"nonce"})

	// Start a new block.
	wp.BeginBlock(2, testHash(0x02))

	if wp.AccountAccessCount() != 0 {
		t.Fatal("expected previous block accesses to be cleared")
	}
	if wp.BlockNumber() != 2 {
		t.Fatalf("expected block number 2, got %d", wp.BlockNumber())
	}
}

// 14. Duplicate field and storage key accesses are deduplicated.
func TestDeduplication(t *testing.T) {
	wp := NewWitnessProducer(DefaultProducerConfig())
	wp.BeginBlock(1, testHash(0x01))

	addr := testAddress(0x01)
	key := testHash(0xAA)

	wp.RecordAccountAccess(addr, []string{"nonce", "nonce", "balance"})
	wp.RecordStorageAccess(addr, key)
	wp.RecordStorageAccess(addr, key)

	pw, err := wp.ProduceWitness()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	rec := pw.AccessedAccounts[addr]
	if len(rec.Fields) != 2 {
		t.Fatalf("expected 2 unique fields, got %d", len(rec.Fields))
	}
	if len(rec.StorageKeys) != 1 {
		t.Fatalf("expected 1 unique storage key, got %d", len(rec.StorageKeys))
	}
}

// 15. Multiple accounts can be recorded independently.
func TestMultipleAccounts(t *testing.T) {
	wp := NewWitnessProducer(DefaultProducerConfig())
	wp.BeginBlock(1, testHash(0x01))

	for i := byte(0); i < 10; i++ {
		wp.RecordAccountAccess(testAddress(i), []string{"balance"})
		wp.RecordStorageAccess(testAddress(i), testHash(i))
	}

	if wp.AccountAccessCount() != 10 {
		t.Fatalf("expected 10 accounts, got %d", wp.AccountAccessCount())
	}
	if wp.StorageAccessCount() != 10 {
		t.Fatalf("expected 10 storage keys, got %d", wp.StorageAccessCount())
	}

	pw, err := wp.ProduceWitness()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pw.AccountCount != 10 {
		t.Fatalf("expected 10 in witness, got %d", pw.AccountCount)
	}
}

// 16. Thread safety: concurrent record calls do not race.
func TestConcurrency(t *testing.T) {
	wp := NewWitnessProducer(DefaultProducerConfig())
	wp.BeginBlock(1, testHash(0x01))

	var wg sync.WaitGroup
	for i := byte(0); i < 50; i++ {
		wg.Add(1)
		go func(b byte) {
			defer wg.Done()
			addr := testAddress(b)
			wp.RecordAccountAccess(addr, []string{"nonce", "balance"})
			wp.RecordStorageAccess(addr, testHash(b))
			wp.RecordCodeAccess(addr)
		}(i)
	}
	wg.Wait()

	pw, err := wp.ProduceWitness()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pw.AccountCount != 50 {
		t.Fatalf("expected 50 accounts, got %d", pw.AccountCount)
	}
}

// 17. Config with IncludeStorageProofs=false omits storage proofs.
func TestIncludeStorageProofs_False(t *testing.T) {
	cfg := WitnessProducerConfig{
		MaxWitnessSize:       0, // no limit
		IncludeStorageProofs: false,
		IncludeCode:          true,
	}
	wp := NewWitnessProducer(cfg)
	wp.BeginBlock(1, testHash(0x01))

	addr := testAddress(0x01)
	wp.RecordStorageAccess(addr, testHash(0xAA))
	wp.RecordStorageAccess(addr, testHash(0xBB))

	pw, err := wp.ProduceWitness()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pw.StorageProofs[addr]) != 0 {
		t.Fatalf("expected no storage proofs, got %d", len(pw.StorageProofs[addr]))
	}
	// But the access record should still have the keys.
	rec := pw.AccessedAccounts[addr]
	if len(rec.StorageKeys) != 2 {
		t.Fatalf("expected 2 storage keys in record, got %d", len(rec.StorageKeys))
	}
}

// 18. Config with IncludeCode=false omits code chunks.
func TestIncludeCode_False(t *testing.T) {
	cfg := WitnessProducerConfig{
		MaxWitnessSize:       0,
		IncludeStorageProofs: true,
		IncludeCode:          false,
	}
	wp := NewWitnessProducer(cfg)
	wp.BeginBlock(1, testHash(0x01))

	addr := testAddress(0x01)
	wp.RecordCodeAccess(addr)

	pw, err := wp.ProduceWitness()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pw.CodeChunks[addr] {
		t.Fatal("expected no code chunk when IncludeCode is false")
	}
	// But the access record should still reflect it.
	if !pw.AccessedAccounts[addr].CodeAccessed {
		t.Fatal("expected CodeAccessed to be true in the record")
	}
}

// 19. HasAccountAccess returns false for unknown address.
func TestHasAccountAccess_Unknown(t *testing.T) {
	wp := NewWitnessProducer(DefaultProducerConfig())
	wp.BeginBlock(1, testHash(0x01))

	if wp.HasAccountAccess(testAddress(0xFF)) {
		t.Fatal("expected false for unrecorded address")
	}
}

// 20. StorageProofs are sorted deterministically.
func TestStorageProofs_Sorted(t *testing.T) {
	wp := NewWitnessProducer(DefaultProducerConfig())
	wp.BeginBlock(1, testHash(0x01))

	addr := testAddress(0x01)
	// Insert keys in reverse order.
	wp.RecordStorageAccess(addr, testHash(0xFF))
	wp.RecordStorageAccess(addr, testHash(0x01))
	wp.RecordStorageAccess(addr, testHash(0x80))

	pw, err := wp.ProduceWitness()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	proofs := pw.StorageProofs[addr]
	if len(proofs) != 3 {
		t.Fatalf("expected 3 proofs, got %d", len(proofs))
	}
	// Verify sorted order: 0x01 < 0x80 < 0xFF
	if proofs[0][0] != 0x01 || proofs[1][0] != 0x80 || proofs[2][0] != 0xFF {
		t.Fatalf("proofs not sorted: %x, %x, %x", proofs[0][0], proofs[1][0], proofs[2][0])
	}
}

// 21. ProduceWitness returns deep copies (modifying output does not affect producer).
func TestProduceWitness_DeepCopy(t *testing.T) {
	wp := NewWitnessProducer(DefaultProducerConfig())
	wp.BeginBlock(1, testHash(0x01))

	addr := testAddress(0x01)
	wp.RecordAccountAccess(addr, []string{"nonce"})

	pw, err := wp.ProduceWitness()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Mutate the returned witness.
	pw.AccessedAccounts[addr].Fields["extra"] = true

	// Produce again and verify the original is unaffected.
	pw2, err := wp.ProduceWitness()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pw2.AccessedAccounts[addr].Fields["extra"] {
		t.Fatal("modifying witness output should not affect producer state")
	}
}

// 22. MaxWitnessSize of 0 means no limit.
func TestMaxWitnessSize_NoLimit(t *testing.T) {
	cfg := WitnessProducerConfig{
		MaxWitnessSize:       0,
		IncludeStorageProofs: true,
		IncludeCode:          true,
	}
	wp := NewWitnessProducer(cfg)
	wp.BeginBlock(1, testHash(0x01))

	// Record many accesses.
	for i := byte(0); i < 100; i++ {
		wp.RecordAccountAccess(testAddress(i), []string{"nonce", "balance", "codehash"})
		wp.RecordStorageAccess(testAddress(i), testHash(i))
	}

	pw, err := wp.ProduceWitness()
	if err != nil {
		t.Fatalf("expected no error with no size limit, got %v", err)
	}
	if pw.AccountCount != 100 {
		t.Fatalf("expected 100, got %d", pw.AccountCount)
	}
}
