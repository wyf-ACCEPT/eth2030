package das

import (
	"testing"
	"time"

	"golang.org/x/crypto/sha3"
)

func makeTestBlob(size int) []byte {
	blob := make([]byte, size)
	for i := range blob {
		// Ensure high byte of each 32-byte element is < 0x73.
		if i%BytesPerFieldElement == 0 {
			blob[i] = 0x10
		} else {
			blob[i] = byte(i % 256)
		}
	}
	return blob
}

func commitmentOf(data []byte) [32]byte {
	h := sha3.NewLegacyKeccak256()
	h.Write(data)
	var out [32]byte
	h.Sum(out[:0])
	return out
}

func TestSizeRuleValidate(t *testing.T) {
	rule := &SizeRule{MinSize: 10, MaxSize: 1000}

	// Valid size.
	blob := makeTestBlob(100)
	if err := rule.Validate(blob, BlobMeta{}); err != nil {
		t.Fatalf("expected valid, got %v", err)
	}

	// Too small.
	small := makeTestBlob(5)
	if err := rule.Validate(small, BlobMeta{}); err == nil {
		t.Fatal("expected error for small blob")
	}

	// Too large.
	large := makeTestBlob(2000)
	if err := rule.Validate(large, BlobMeta{}); err == nil {
		t.Fatal("expected error for large blob")
	}

	// No limits.
	noLimits := &SizeRule{}
	if err := noLimits.Validate(makeTestBlob(1), BlobMeta{}); err != nil {
		t.Fatalf("expected valid with no limits, got %v", err)
	}
}

func TestFormatRuleValidate(t *testing.T) {
	rule := &FormatRule{
		FieldElementSize: BytesPerFieldElement,
		StrictAlignment:  true,
	}

	// Valid aligned blob.
	blob := makeTestBlob(BytesPerFieldElement * 4) // 128 bytes
	if err := rule.Validate(blob, BlobMeta{}); err != nil {
		t.Fatalf("expected valid, got %v", err)
	}

	// Unaligned blob with strict alignment.
	unaligned := makeTestBlob(BytesPerFieldElement*4 + 3)
	if err := rule.Validate(unaligned, BlobMeta{}); err == nil {
		t.Fatal("expected error for unaligned blob")
	}

	// High byte violation.
	bad := makeTestBlob(BytesPerFieldElement)
	bad[0] = 0x80 // >= 0x73
	if err := rule.Validate(bad, BlobMeta{}); err == nil {
		t.Fatal("expected error for high-byte violation")
	}

	// Non-strict alignment.
	relaxed := &FormatRule{
		FieldElementSize: BytesPerFieldElement,
		StrictAlignment:  false,
	}
	blob2 := makeTestBlob(BytesPerFieldElement*2 + 5)
	if err := relaxed.Validate(blob2, BlobMeta{}); err != nil {
		t.Fatalf("expected valid with relaxed alignment, got %v", err)
	}
}

func TestCommitmentRuleValidate(t *testing.T) {
	rule := &CommitmentRule{}

	blob := makeTestBlob(64)
	commitment := commitmentOf(blob)

	// Correct commitment.
	meta := BlobMeta{Commitment: commitment}
	if err := rule.Validate(blob, meta); err != nil {
		t.Fatalf("expected valid commitment, got %v", err)
	}

	// Wrong commitment.
	badCommitment := [32]byte{0xDE, 0xAD}
	meta2 := BlobMeta{Commitment: badCommitment}
	if err := rule.Validate(blob, meta2); err == nil {
		t.Fatal("expected error for wrong commitment")
	}

	// Zero commitment (skip check).
	meta3 := BlobMeta{}
	if err := rule.Validate(blob, meta3); err != nil {
		t.Fatalf("expected skip for zero commitment, got %v", err)
	}
}

func TestExpiryRuleValidate(t *testing.T) {
	rule := &ExpiryRule{}

	// Not expired.
	meta := BlobMeta{Expiry: 100, CurrentSlot: 50}
	if err := rule.Validate(nil, meta); err != nil {
		t.Fatalf("expected valid, got %v", err)
	}

	// Expired.
	meta2 := BlobMeta{Expiry: 50, CurrentSlot: 100}
	if err := rule.Validate(nil, meta2); err == nil {
		t.Fatal("expected error for expired blob")
	}

	// No expiry set.
	meta3 := BlobMeta{CurrentSlot: 100}
	if err := rule.Validate(nil, meta3); err != nil {
		t.Fatalf("expected skip for zero expiry, got %v", err)
	}

	// At exact boundary (not expired).
	meta4 := BlobMeta{Expiry: 100, CurrentSlot: 100}
	if err := rule.Validate(nil, meta4); err != nil {
		t.Fatalf("expected valid at boundary, got %v", err)
	}
}

func TestBlobValidatorValidateBlob(t *testing.T) {
	bv := DefaultBlobValidator()

	blob := makeTestBlob(BytesPerFieldElement * 4)
	commitment := commitmentOf(blob)
	meta := BlobMeta{
		Size:        int64(len(blob)),
		Commitment:  commitment,
		CurrentSlot: 10,
		Expiry:      100,
	}

	result := bv.ValidateBlob(blob, meta)
	if !result.Valid {
		t.Fatalf("expected valid result, got errors: %v", result.Errors)
	}
	if result.Duration <= 0 {
		t.Fatal("expected positive duration")
	}
}

func TestBlobValidatorNilBlob(t *testing.T) {
	bv := DefaultBlobValidator()
	result := bv.ValidateBlob(nil, BlobMeta{})
	if result.Valid {
		t.Fatal("expected invalid for nil blob")
	}
	if len(result.Errors) == 0 {
		t.Fatal("expected at least one error")
	}
}

func TestBlobValidatorEmptyBlob(t *testing.T) {
	bv := DefaultBlobValidator()
	result := bv.ValidateBlob([]byte{}, BlobMeta{})
	if result.Valid {
		t.Fatal("expected invalid for empty blob")
	}
}

func TestBlobValidatorCaching(t *testing.T) {
	bv := DefaultBlobValidator()

	blob := makeTestBlob(BytesPerFieldElement * 2)
	meta := BlobMeta{}

	// First validation.
	r1 := bv.ValidateBlob(blob, meta)
	if !r1.Valid {
		t.Fatalf("expected valid, got %v", r1.Errors)
	}

	// Second validation should come from cache.
	r2 := bv.ValidateBlob(blob, meta)
	if !r2.Valid {
		t.Fatalf("expected valid from cache, got %v", r2.Errors)
	}

	hits, misses := bv.cache.Stats()
	if hits != 1 {
		t.Fatalf("expected 1 cache hit, got %d", hits)
	}
	if misses != 1 {
		t.Fatalf("expected 1 cache miss, got %d", misses)
	}
}

func TestBlobValidationCacheLRUEviction(t *testing.T) {
	cache := NewBlobValidationCache(3, 1*time.Minute)

	// Fill cache with 3 entries.
	for i := 0; i < 3; i++ {
		h := [32]byte{byte(i)}
		cache.Put(h, BlobValidationResult{Valid: true})
	}
	if cache.Size() != 3 {
		t.Fatalf("expected size 3, got %d", cache.Size())
	}

	// Add a 4th; should evict the LRU (index 0).
	h4 := [32]byte{0x10}
	cache.Put(h4, BlobValidationResult{Valid: true})
	if cache.Size() != 3 {
		t.Fatalf("expected size 3 after eviction, got %d", cache.Size())
	}

	// Index 0 should be evicted.
	_, ok := cache.Get([32]byte{0})
	if ok {
		t.Fatal("expected entry 0 to be evicted")
	}

	// Index 1 should still be present.
	_, ok = cache.Get([32]byte{1})
	if !ok {
		t.Fatal("expected entry 1 to still be present")
	}
}

func TestBlobValidationCacheExpiry(t *testing.T) {
	cache := NewBlobValidationCache(10, 1*time.Millisecond)

	h := [32]byte{0xAA}
	cache.Put(h, BlobValidationResult{Valid: true})

	// Wait for expiry.
	time.Sleep(5 * time.Millisecond)

	_, ok := cache.Get(h)
	if ok {
		t.Fatal("expected cache entry to be expired")
	}
}

func TestBlobValidatorRuleCount(t *testing.T) {
	bv := DefaultBlobValidator()
	if bv.RuleCount() != 4 {
		t.Fatalf("expected 4 rules, got %d", bv.RuleCount())
	}
}

func TestBlobValidatorAddRule(t *testing.T) {
	bv := NewBlobValidator(nil, nil)
	bv.AddRule(&SizeRule{MaxSize: 100})
	if bv.RuleCount() != 1 {
		t.Fatalf("expected 1 rule, got %d", bv.RuleCount())
	}
}
