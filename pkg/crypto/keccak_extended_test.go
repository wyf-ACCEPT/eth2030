package crypto

import (
	"bytes"
	"encoding/hex"
	"math/big"
	"sync"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

func TestKeccak512EmptyString(t *testing.T) {
	hash := Keccak512([]byte{})
	if len(hash) != 64 {
		t.Fatalf("Keccak512 output length = %d, want 64", len(hash))
	}
	// Known Keccak-512 of empty string.
	want := "0eab42de4c3ceb9235fc91acffe746b29c29a8c366b7c60e4e67c466f36a4304" +
		"c00fa9caf9d87976ba469bcbe06713b435f091ef2769fb160cdab33d3670680e"
	got := hex.EncodeToString(hash)
	if got != want {
		t.Errorf("Keccak512(empty) = %s, want %s", got, want)
	}
}

func TestKeccak512NonEmpty(t *testing.T) {
	h1 := Keccak512([]byte("hello"))
	h2 := Keccak512([]byte("hello"))
	if !bytes.Equal(h1, h2) {
		t.Error("Keccak512 is not deterministic")
	}
	if len(h1) != 64 {
		t.Errorf("Keccak512 output length = %d, want 64", len(h1))
	}
}

func TestKeccak512MultipleInputs(t *testing.T) {
	combined := Keccak512([]byte("helloworld"))
	separate := Keccak512([]byte("hello"), []byte("world"))
	if !bytes.Equal(combined, separate) {
		t.Error("Keccak512 multi-input should equal concatenated input")
	}
}

func TestKeccak512HashReturnType(t *testing.T) {
	h := Keccak512Hash([]byte("test"))
	if len(h) != 64 {
		t.Errorf("[64]byte length = %d, want 64", len(h))
	}
	// Verify deterministic.
	h2 := Keccak512Hash([]byte("test"))
	if h != h2 {
		t.Error("Keccak512Hash is not deterministic")
	}
}

func TestDomainSeparatedHash(t *testing.T) {
	h1 := DomainSeparatedHash("domain-a", []byte("data"))
	h2 := DomainSeparatedHash("domain-b", []byte("data"))
	if bytes.Equal(h1, h2) {
		t.Error("different domains should produce different hashes")
	}

	h3 := DomainSeparatedHash("domain-a", []byte("data"))
	if !bytes.Equal(h1, h3) {
		t.Error("same domain+data should produce same hash")
	}
}

func TestDomainSeparatedHash256(t *testing.T) {
	h := DomainSeparatedHash256("test-domain", []byte("payload"))
	if h == (types.Hash{}) {
		t.Error("hash should not be zero")
	}
	h2 := DomainSeparatedHash256("test-domain", []byte("payload"))
	if h != h2 {
		t.Error("DomainSeparatedHash256 should be deterministic")
	}
}

func TestDomainSeparatedHashEmptyDomain(t *testing.T) {
	h := DomainSeparatedHash("", []byte("data"))
	if len(h) != 32 {
		t.Errorf("output length = %d, want 32", len(h))
	}
}

func TestHashToFieldBLS(t *testing.T) {
	data := []byte("test message")
	dst := []byte("BLS_TEST_DST")

	fields, err := HashToFieldBLS(data, dst, 2)
	if err != nil {
		t.Fatalf("HashToFieldBLS: %v", err)
	}
	if len(fields) != 2 {
		t.Fatalf("expected 2 field elements, got %d", len(fields))
	}

	for i, f := range fields {
		if f.Sign() < 0 {
			t.Errorf("field[%d] is negative", i)
		}
		if f.Cmp(blsR) >= 0 {
			t.Errorf("field[%d] >= blsR", i)
		}
	}

	// Different count should produce distinct first element only if count changes.
	fields1, _ := HashToFieldBLS(data, dst, 1)
	if fields1[0].Cmp(fields[0]) != 0 {
		t.Error("first element should match regardless of count")
	}
}

func TestHashToFieldBLSDeterministic(t *testing.T) {
	data := []byte("deterministic test")
	dst := []byte("DST")
	f1, _ := HashToFieldBLS(data, dst, 2)
	f2, _ := HashToFieldBLS(data, dst, 2)
	if f1[0].Cmp(f2[0]) != 0 || f1[1].Cmp(f2[1]) != 0 {
		t.Error("HashToFieldBLS should be deterministic")
	}
}

func TestHashToFieldBLSDistinct(t *testing.T) {
	dst := []byte("DST")
	f1, _ := HashToFieldBLS([]byte("msg1"), dst, 1)
	f2, _ := HashToFieldBLS([]byte("msg2"), dst, 1)
	if f1[0].Cmp(f2[0]) == 0 {
		t.Error("different messages should produce different field elements")
	}
}

func TestHashToFieldBLSErrors(t *testing.T) {
	_, err := HashToFieldBLS([]byte("data"), []byte("dst"), 0)
	if err == nil {
		t.Error("count=0 should error")
	}
	_, err = HashToFieldBLS([]byte("data"), []byte("dst"), 9)
	if err == nil {
		t.Error("count=9 should error")
	}
	longDST := make([]byte, 256)
	_, err = HashToFieldBLS([]byte("data"), longDST, 1)
	if err == nil {
		t.Error("DST > 255 should error")
	}
}

func TestHashToFieldBN254(t *testing.T) {
	data := []byte("bn254 test")
	dst := []byte("BN254_TEST")

	fields, err := HashToFieldBN254(data, dst, 2)
	if err != nil {
		t.Fatalf("HashToFieldBN254: %v", err)
	}
	for i, f := range fields {
		if f.Cmp(bn254N) >= 0 {
			t.Errorf("field[%d] >= bn254N", i)
		}
	}
}

func TestHashToFieldBN254Errors(t *testing.T) {
	_, err := HashToFieldBN254([]byte("data"), []byte("dst"), 0)
	if err == nil {
		t.Error("count=0 should error")
	}
	_, err = HashToFieldBN254([]byte("data"), []byte("dst"), 9)
	if err == nil {
		t.Error("count=9 should error")
	}
}

func TestIncrementalHasherBasic(t *testing.T) {
	h := NewIncrementalHasher()
	h.Write([]byte("hello"))
	h.Write([]byte("world"))
	got := h.Sum256()

	want := Keccak256Hash([]byte("helloworld"))
	if got != want {
		t.Errorf("incremental hash = %x, want %x", got, want)
	}
}

func TestIncrementalHasherWriteUint64(t *testing.T) {
	h := NewIncrementalHasher()
	h.WriteUint64(42)
	result := h.Sum256()
	if result == (types.Hash{}) {
		t.Error("hash should not be zero")
	}
	if h.Size() != 8 {
		t.Errorf("size = %d, want 8", h.Size())
	}
}

func TestIncrementalHasherWriteHash(t *testing.T) {
	h := NewIncrementalHasher()
	testHash := Keccak256Hash([]byte("test"))
	h.WriteHash(testHash)
	if h.Size() != 32 {
		t.Errorf("size = %d, want 32", h.Size())
	}
	result := h.Sum256()
	if result == (types.Hash{}) {
		t.Error("hash should not be zero")
	}
}

func TestIncrementalHasherWriteAddress(t *testing.T) {
	h := NewIncrementalHasher()
	addr := types.BytesToAddress([]byte{0x01, 0x02, 0x03})
	h.WriteAddress(addr)
	if h.Size() != 20 {
		t.Errorf("size = %d, want 20", h.Size())
	}
}

func TestIncrementalHasherReset(t *testing.T) {
	h := NewIncrementalHasher()
	h.Write([]byte("data"))
	h.Reset()
	if h.Size() != 0 {
		t.Errorf("size after reset = %d, want 0", h.Size())
	}
	// After reset, should produce hash of empty.
	result := h.SumBytes()
	empty := Keccak256([]byte{})
	if !bytes.Equal(result, empty) {
		t.Errorf("after reset, hash should equal empty hash")
	}
}

func TestIncrementalHasherSumBytes(t *testing.T) {
	h := NewIncrementalHasher()
	h.Write([]byte("test"))
	got := h.SumBytes()
	want := Keccak256([]byte("test"))
	if !bytes.Equal(got, want) {
		t.Errorf("SumBytes mismatch: %x != %x", got, want)
	}
}

func TestPreimageTrackerRecord(t *testing.T) {
	pt := NewPreimageTracker()
	data := []byte("preimage data")
	hash := pt.Record(data)

	want := Keccak256Hash(data)
	if hash != want {
		t.Errorf("Record hash = %x, want %x", hash, want)
	}

	preimage := pt.Lookup(hash)
	if !bytes.Equal(preimage, data) {
		t.Errorf("Lookup = %x, want %x", preimage, data)
	}
}

func TestPreimageTrackerLookupMiss(t *testing.T) {
	pt := NewPreimageTracker()
	var missing types.Hash
	missing[0] = 0xFF
	if pt.Lookup(missing) != nil {
		t.Error("missing hash should return nil")
	}
}

func TestPreimageTrackerDisabled(t *testing.T) {
	pt := NewPreimageTracker()
	pt.SetEnabled(false)
	data := []byte("should not be stored")
	hash := pt.Record(data)

	if pt.Lookup(hash) != nil {
		t.Error("disabled tracker should not store preimages")
	}
	if pt.Count() != 0 {
		t.Errorf("count = %d, want 0 when disabled", pt.Count())
	}
}

func TestPreimageTrackerClear(t *testing.T) {
	pt := NewPreimageTracker()
	pt.Record([]byte("one"))
	pt.Record([]byte("two"))
	if pt.Count() != 2 {
		t.Fatalf("count = %d, want 2", pt.Count())
	}
	pt.Clear()
	if pt.Count() != 0 {
		t.Errorf("count after clear = %d, want 0", pt.Count())
	}
}

func TestPreimageTrackerAll(t *testing.T) {
	pt := NewPreimageTracker()
	data1 := []byte("one")
	data2 := []byte("two")
	h1 := pt.Record(data1)
	h2 := pt.Record(data2)

	all := pt.All()
	if len(all) != 2 {
		t.Fatalf("All() returned %d entries, want 2", len(all))
	}
	if !bytes.Equal(all[h1], data1) {
		t.Error("preimage 1 mismatch")
	}
	if !bytes.Equal(all[h2], data2) {
		t.Error("preimage 2 mismatch")
	}
}

func TestPreimageTrackerConcurrent(t *testing.T) {
	pt := NewPreimageTracker()
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			data := []byte{byte(i), byte(i >> 8)}
			hash := pt.Record(data)
			_ = pt.Lookup(hash)
		}(i)
	}
	wg.Wait()
	if pt.Count() == 0 {
		t.Error("expected some preimages recorded")
	}
}

func TestPreimageTrackerReturnsCopy(t *testing.T) {
	pt := NewPreimageTracker()
	data := []byte("original")
	hash := pt.Record(data)

	looked := pt.Lookup(hash)
	looked[0] = 0xFF // Mutate the returned copy.

	// Original should be unchanged.
	original := pt.Lookup(hash)
	if original[0] == 0xFF {
		t.Error("Lookup should return a copy, not a reference")
	}
}

func TestKeccak256WithTrackerNilTracker(t *testing.T) {
	hash := Keccak256WithTracker(nil, []byte("data"))
	want := Keccak256Hash([]byte("data"))
	if hash != want {
		t.Errorf("nil tracker hash = %x, want %x", hash, want)
	}
}

func TestKeccak256WithTrackerRecords(t *testing.T) {
	pt := NewPreimageTracker()
	data := []byte("tracked")
	hash := Keccak256WithTracker(pt, data)
	if pt.Lookup(hash) == nil {
		t.Error("tracker should have recorded the preimage")
	}
}

func TestCommitHashDeterministic(t *testing.T) {
	a := Keccak256Hash([]byte("a"))
	b := Keccak256Hash([]byte("b"))

	h1 := CommitHash(a, b)
	h2 := CommitHash(a, b)
	if h1 != h2 {
		t.Error("CommitHash should be deterministic")
	}
}

func TestCommitHashCommutative(t *testing.T) {
	a := Keccak256Hash([]byte("first"))
	b := Keccak256Hash([]byte("second"))

	h1 := CommitHash(a, b)
	h2 := CommitHash(b, a)
	if h1 != h2 {
		t.Error("CommitHash should be commutative")
	}
}

func TestCommitHashSameInputs(t *testing.T) {
	a := Keccak256Hash([]byte("same"))
	h := CommitHash(a, a)
	if h == (types.Hash{}) {
		t.Error("CommitHash of equal inputs should not be zero")
	}
}

func TestPersonalizedHash(t *testing.T) {
	h1 := PersonalizedHash("tag-a", []byte("data"))
	h2 := PersonalizedHash("tag-b", []byte("data"))
	if bytes.Equal(h1, h2) {
		t.Error("different tags should produce different hashes")
	}

	if len(h1) != 32 {
		t.Errorf("output length = %d, want 32", len(h1))
	}
}

func TestPersonalizedHashDeterministic(t *testing.T) {
	h1 := PersonalizedHash("tag", []byte("data"))
	h2 := PersonalizedHash("tag", []byte("data"))
	if !bytes.Equal(h1, h2) {
		t.Error("PersonalizedHash should be deterministic")
	}
}

func TestHashToFieldBLSModReduction(t *testing.T) {
	// Test with known large inputs that would exceed blsR.
	data := bytes.Repeat([]byte{0xFF}, 100)
	dst := []byte("LARGE_INPUT_TEST")

	fields, err := HashToFieldBLS(data, dst, 1)
	if err != nil {
		t.Fatalf("HashToFieldBLS: %v", err)
	}
	if fields[0].Cmp(blsR) >= 0 {
		t.Error("result should be reduced mod blsR")
	}
	if fields[0].Sign() < 0 {
		t.Error("result should be non-negative")
	}
}

func TestHashToFieldBN254ModReduction(t *testing.T) {
	data := bytes.Repeat([]byte{0xFF}, 100)
	dst := []byte("LARGE_INPUT_TEST")

	fields, err := HashToFieldBN254(data, dst, 1)
	if err != nil {
		t.Fatalf("HashToFieldBN254: %v", err)
	}
	if fields[0].Cmp(bn254N) >= 0 {
		t.Error("result should be reduced mod bn254N")
	}

	// Verify it's nonzero (extremely unlikely to hash to zero).
	zero := new(big.Int)
	if fields[0].Cmp(zero) == 0 {
		t.Error("result should not be zero (extremely unlikely)")
	}
}
