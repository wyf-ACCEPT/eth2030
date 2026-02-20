// Package das - blob_validator.go implements a multi-rule blob validation
// pipeline for cell-level messages per the PeerDAS spec. It validates blob
// data against configurable rules (size, format, commitment, expiry) and
// caches validation results using an LRU cache to avoid redundant checks.
package das

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"golang.org/x/crypto/sha3"
)

// Blob validation errors.
var (
	ErrBlobValidateNil         = errors.New("das/blobval: blob data is nil")
	ErrBlobValidateEmpty       = errors.New("das/blobval: blob data is empty")
	ErrBlobValidateSizeMax     = errors.New("das/blobval: blob exceeds maximum size")
	ErrBlobValidateSizeMin     = errors.New("das/blobval: blob below minimum size")
	ErrBlobValidateFormat      = errors.New("das/blobval: invalid blob format")
	ErrBlobValidateCommitment  = errors.New("das/blobval: commitment mismatch")
	ErrBlobValidateExpiry      = errors.New("das/blobval: blob has expired")
	ErrBlobValidateNoRules     = errors.New("das/blobval: no validation rules configured")
)

// BlobMeta contains metadata associated with a blob for validation.
type BlobMeta struct {
	// Size is the expected size of the blob in bytes.
	Size int64

	// Commitment is the expected hash commitment over the blob data.
	Commitment [32]byte

	// Expiry is the slot number after which the blob is considered expired.
	Expiry uint64

	// CellIndex is the cell index within the extended blob.
	CellIndex uint64

	// ColumnIndex identifies the column in the data matrix.
	ColumnIndex uint64

	// CurrentSlot is the current slot for expiry comparison.
	CurrentSlot uint64
}

// BlobValidationError contains details about a single validation failure.
type BlobValidationError struct {
	Rule    string
	Message string
	Err     error
}

// Error implements the error interface.
func (ve *BlobValidationError) Error() string {
	return fmt.Sprintf("rule %s: %s", ve.Rule, ve.Message)
}

// Unwrap returns the underlying error.
func (ve *BlobValidationError) Unwrap() error {
	return ve.Err
}

// BlobValidationResult holds the outcome of validating a blob.
type BlobValidationResult struct {
	Valid    bool
	Errors   []BlobValidationError
	Duration time.Duration
}

// BlobValidationRule is the interface that each blob validation rule implements.
type BlobValidationRule interface {
	// Name returns the rule's human-readable name.
	Name() string

	// Validate checks the blob data against this rule.
	// Returns nil if the blob passes, or an error describing the violation.
	Validate(blob []byte, meta BlobMeta) error
}

// SizeRule validates that blob data is within acceptable size bounds.
type SizeRule struct {
	MinSize int64 // minimum blob size in bytes (0 = no minimum)
	MaxSize int64 // maximum blob size in bytes (0 = no maximum)
}

// Name returns the rule name.
func (r *SizeRule) Name() string { return "size" }

// Validate checks blob size constraints.
func (r *SizeRule) Validate(blob []byte, meta BlobMeta) error {
	size := int64(len(blob))
	if r.MinSize > 0 && size < r.MinSize {
		return fmt.Errorf("%w: %d < min %d", ErrBlobValidateSizeMin, size, r.MinSize)
	}
	if r.MaxSize > 0 && size > r.MaxSize {
		return fmt.Errorf("%w: %d > max %d", ErrBlobValidateSizeMax, size, r.MaxSize)
	}
	return nil
}

// FormatRule validates that blob data adheres to expected formatting. It
// checks that the blob contains valid field elements (each 32-byte chunk
// has its high bit clear, consistent with BLS12-381 scalar field).
type FormatRule struct {
	// FieldElementSize is the size of each field element (default 32).
	FieldElementSize int

	// StrictAlignment requires blob size to be a multiple of FieldElementSize.
	StrictAlignment bool
}

// Name returns the rule name.
func (r *FormatRule) Name() string { return "format" }

// Validate checks blob format constraints.
func (r *FormatRule) Validate(blob []byte, meta BlobMeta) error {
	feSize := r.FieldElementSize
	if feSize <= 0 {
		feSize = BytesPerFieldElement // 32
	}

	if r.StrictAlignment && len(blob)%feSize != 0 {
		return fmt.Errorf("%w: size %d not aligned to %d bytes",
			ErrBlobValidateFormat, len(blob), feSize)
	}

	// Check that each field element's high byte is less than the BLS modulus
	// high byte (0x73). We use a simplified check: the first byte of each
	// 32-byte element must be < 0x73.
	for offset := 0; offset+feSize <= len(blob); offset += feSize {
		if blob[offset] >= 0x73 {
			return fmt.Errorf("%w: field element at offset %d has high byte 0x%02x >= 0x73",
				ErrBlobValidateFormat, offset, blob[offset])
		}
	}
	return nil
}

// CommitmentRule validates that the blob data matches its stated commitment.
// The commitment is computed as keccak256(blob).
type CommitmentRule struct{}

// Name returns the rule name.
func (r *CommitmentRule) Name() string { return "commitment" }

// Validate checks that keccak256(blob) matches meta.Commitment.
func (r *CommitmentRule) Validate(blob []byte, meta BlobMeta) error {
	// A zero commitment means the caller did not provide one; skip check.
	zero := [32]byte{}
	if meta.Commitment == zero {
		return nil
	}

	h := sha3.NewLegacyKeccak256()
	h.Write(blob)
	var computed [32]byte
	h.Sum(computed[:0])

	if computed != meta.Commitment {
		return fmt.Errorf("%w: computed %x, expected %x",
			ErrBlobValidateCommitment, computed[:8], meta.Commitment[:8])
	}
	return nil
}

// ExpiryRule validates that a blob has not expired based on its slot metadata.
type ExpiryRule struct{}

// Name returns the rule name.
func (r *ExpiryRule) Name() string { return "expiry" }

// Validate checks if the blob's expiry slot has passed.
func (r *ExpiryRule) Validate(_ []byte, meta BlobMeta) error {
	// An expiry of 0 means no expiration set.
	if meta.Expiry == 0 {
		return nil
	}
	if meta.CurrentSlot > meta.Expiry {
		return fmt.Errorf("%w: current slot %d > expiry %d",
			ErrBlobValidateExpiry, meta.CurrentSlot, meta.Expiry)
	}
	return nil
}

// blobValCacheEntry stores a cached validation result.
type blobValCacheEntry struct {
	result BlobValidationResult
	expiry time.Time
}

// BlobValidationCache caches blob validation results to avoid redundant
// validation of the same blob data. It uses LRU eviction.
type BlobValidationCache struct {
	mu       sync.Mutex
	entries  map[[32]byte]*blobValCacheEntry
	order    [][32]byte // LRU order: most recently used at end
	maxSize  int
	cacheTTL time.Duration
	hits     int64
	misses   int64
}

// NewBlobValidationCache creates a new validation cache with the given capacity.
func NewBlobValidationCache(maxSize int, ttl time.Duration) *BlobValidationCache {
	if maxSize <= 0 {
		maxSize = 4096
	}
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	return &BlobValidationCache{
		entries:  make(map[[32]byte]*blobValCacheEntry, maxSize),
		order:    make([][32]byte, 0, maxSize),
		maxSize:  maxSize,
		cacheTTL: ttl,
	}
}

// Get retrieves a cached result. Returns the result and true if found
// and not expired, or a zero result and false otherwise.
func (vc *BlobValidationCache) Get(blobHash [32]byte) (BlobValidationResult, bool) {
	vc.mu.Lock()
	defer vc.mu.Unlock()

	entry, ok := vc.entries[blobHash]
	if !ok {
		vc.misses++
		return BlobValidationResult{}, false
	}
	if time.Now().After(entry.expiry) {
		// Expired; remove it.
		delete(vc.entries, blobHash)
		vc.removeFromOrder(blobHash)
		vc.misses++
		return BlobValidationResult{}, false
	}

	vc.hits++
	// Move to end (most recent).
	vc.removeFromOrder(blobHash)
	vc.order = append(vc.order, blobHash)
	return entry.result, true
}

// Put stores a validation result in the cache, evicting the LRU entry
// if the cache is full.
func (vc *BlobValidationCache) Put(blobHash [32]byte, result BlobValidationResult) {
	vc.mu.Lock()
	defer vc.mu.Unlock()

	// If already present, update.
	if _, ok := vc.entries[blobHash]; ok {
		vc.entries[blobHash] = &blobValCacheEntry{
			result: result,
			expiry: time.Now().Add(vc.cacheTTL),
		}
		vc.removeFromOrder(blobHash)
		vc.order = append(vc.order, blobHash)
		return
	}

	// Evict LRU if at capacity.
	if len(vc.entries) >= vc.maxSize && len(vc.order) > 0 {
		evict := vc.order[0]
		vc.order = vc.order[1:]
		delete(vc.entries, evict)
	}

	vc.entries[blobHash] = &blobValCacheEntry{
		result: result,
		expiry: time.Now().Add(vc.cacheTTL),
	}
	vc.order = append(vc.order, blobHash)
}

// Size returns the number of cached entries.
func (vc *BlobValidationCache) Size() int {
	vc.mu.Lock()
	defer vc.mu.Unlock()
	return len(vc.entries)
}

// Stats returns cache hit and miss counts.
func (vc *BlobValidationCache) Stats() (hits, misses int64) {
	vc.mu.Lock()
	defer vc.mu.Unlock()
	return vc.hits, vc.misses
}

// removeFromOrder removes a hash from the LRU order. Caller must hold lock.
func (vc *BlobValidationCache) removeFromOrder(h [32]byte) {
	for i, entry := range vc.order {
		if entry == h {
			vc.order = append(vc.order[:i], vc.order[i+1:]...)
			return
		}
	}
}

// BlobValidator validates blob data against a set of configurable rules.
// It supports caching validation results for repeated blobs.
type BlobValidator struct {
	rules []BlobValidationRule
	cache *BlobValidationCache
}

// NewBlobValidator creates a new blob validator with the given rules.
// If cache is nil, no caching is performed.
func NewBlobValidator(rules []BlobValidationRule, cache *BlobValidationCache) *BlobValidator {
	return &BlobValidator{
		rules: rules,
		cache: cache,
	}
}

// DefaultBlobValidator creates a validator with standard rules for PeerDAS
// blob validation: size, format, commitment, and expiry checks.
func DefaultBlobValidator() *BlobValidator {
	rules := []BlobValidationRule{
		&SizeRule{
			MinSize: 1,
			MaxSize: int64(FieldElementsPerBlob * BytesPerFieldElement), // 128 KiB
		},
		&FormatRule{
			FieldElementSize: BytesPerFieldElement,
			StrictAlignment:  false,
		},
		&CommitmentRule{},
		&ExpiryRule{},
	}
	cache := NewBlobValidationCache(4096, 5*time.Minute)
	return NewBlobValidator(rules, cache)
}

// ValidateBlob runs all configured rules against the blob data and returns
// a combined result. If caching is enabled, repeated validations of the
// same blob data are served from cache.
func (bv *BlobValidator) ValidateBlob(data []byte, meta BlobMeta) BlobValidationResult {
	if data == nil {
		return BlobValidationResult{
			Valid: false,
			Errors: []BlobValidationError{{
				Rule:    "precondition",
				Message: "blob data is nil",
				Err:     ErrBlobValidateNil,
			}},
		}
	}
	if len(data) == 0 {
		return BlobValidationResult{
			Valid: false,
			Errors: []BlobValidationError{{
				Rule:    "precondition",
				Message: "blob data is empty",
				Err:     ErrBlobValidateEmpty,
			}},
		}
	}

	// Check cache.
	if bv.cache != nil {
		blobHash := computeBlobHash(data)
		if cached, ok := bv.cache.Get(blobHash); ok {
			return cached
		}
	}

	start := time.Now()
	var validationErrors []BlobValidationError

	for _, rule := range bv.rules {
		if err := rule.Validate(data, meta); err != nil {
			validationErrors = append(validationErrors, BlobValidationError{
				Rule:    rule.Name(),
				Message: err.Error(),
				Err:     err,
			})
		}
	}

	result := BlobValidationResult{
		Valid:    len(validationErrors) == 0,
		Errors:   validationErrors,
		Duration: time.Since(start),
	}

	// Store in cache.
	if bv.cache != nil {
		blobHash := computeBlobHash(data)
		bv.cache.Put(blobHash, result)
	}

	return result
}

// RuleCount returns the number of configured validation rules.
func (bv *BlobValidator) RuleCount() int {
	return len(bv.rules)
}

// AddRule appends a validation rule.
func (bv *BlobValidator) AddRule(rule BlobValidationRule) {
	bv.rules = append(bv.rules, rule)
}

// computeBlobHash returns keccak256(blob) as a cache key.
func computeBlobHash(data []byte) [32]byte {
	h := sha3.NewLegacyKeccak256()
	h.Write(data)
	var result [32]byte
	h.Sum(result[:0])
	return result
}
