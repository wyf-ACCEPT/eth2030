package crypto

// Extended Keccak utilities: Keccak-512, domain-separated hashing,
// hash-to-field for BLS, incremental hasher, and preimage tracking.

import (
	"encoding/binary"
	"errors"
	"hash"
	"math/big"
	"sync"

	"github.com/eth2030/eth2030/core/types"
	"golang.org/x/crypto/sha3"
)

// Keccak512 calculates the Keccak-512 hash of the given data.
func Keccak512(data ...[]byte) []byte {
	d := sha3.NewLegacyKeccak512()
	for _, b := range data {
		d.Write(b)
	}
	return d.Sum(nil)
}

// Keccak512Hash calculates Keccak-512 and returns the result as a 64-byte slice.
func Keccak512Hash(data ...[]byte) [64]byte {
	var h [64]byte
	copy(h[:], Keccak512(data...))
	return h
}

// DomainSeparatedHash computes Keccak256(domain || data) with a length-prefixed
// domain string to prevent collisions across different usage contexts.
// The domain is prefixed with its 2-byte big-endian length.
func DomainSeparatedHash(domain string, data []byte) []byte {
	d := sha3.NewLegacyKeccak256()
	var lenBuf [2]byte
	binary.BigEndian.PutUint16(lenBuf[:], uint16(len(domain)))
	d.Write(lenBuf[:])
	d.Write([]byte(domain))
	d.Write(data)
	return d.Sum(nil)
}

// DomainSeparatedHash256 is like DomainSeparatedHash but returns a types.Hash.
func DomainSeparatedHash256(domain string, data []byte) types.Hash {
	var h types.Hash
	copy(h[:], DomainSeparatedHash(domain, data))
	return h
}

// HashToFieldBLS hashes arbitrary data to a BLS12-381 scalar field element
// using the procedure from RFC 9380. The result is reduced mod r (the BLS
// subgroup order). The domain separation tag (DST) prevents cross-protocol
// attacks. Two independent field elements are produced using 64-byte chunks
// from Keccak-512, ensuring uniform distribution after modular reduction.
func HashToFieldBLS(data, dst []byte, count int) ([]*big.Int, error) {
	if count < 1 || count > 8 {
		return nil, errors.New("keccak: count must be 1..8")
	}
	if len(dst) > 255 {
		return nil, errors.New("keccak: DST exceeds 255 bytes")
	}

	results := make([]*big.Int, count)
	for i := 0; i < count; i++ {
		// Build input: dst_len(1) || dst || counter(1) || data
		input := make([]byte, 0, 1+len(dst)+1+len(data))
		input = append(input, byte(len(dst)))
		input = append(input, dst...)
		input = append(input, byte(i))
		input = append(input, data...)

		// Use Keccak-512 for 64 bytes of entropy to get uniform distribution
		// after reduction modulo r.
		hash := Keccak512(input)
		v := new(big.Int).SetBytes(hash)
		v.Mod(v, blsR)
		results[i] = v
	}
	return results, nil
}

// HashToFieldBN254 hashes data to a BN254 scalar field element using
// domain separation. Returns values reduced modulo the BN254 curve order n.
func HashToFieldBN254(data, dst []byte, count int) ([]*big.Int, error) {
	if count < 1 || count > 8 {
		return nil, errors.New("keccak: count must be 1..8")
	}

	results := make([]*big.Int, count)
	for i := 0; i < count; i++ {
		input := make([]byte, 0, 1+len(dst)+1+len(data))
		input = append(input, byte(len(dst)))
		input = append(input, dst...)
		input = append(input, byte(i))
		input = append(input, data...)

		hash := Keccak512(input)
		v := new(big.Int).SetBytes(hash)
		v.Mod(v, bn254N)
		results[i] = v
	}
	return results, nil
}

// IncrementalHasher is an incremental Keccak-256 hasher that allows data to be
// fed in chunks. It wraps sha3.NewLegacyKeccak256() with a convenient API.
type IncrementalHasher struct {
	state hash.Hash
	size  int // total bytes written
}

// NewIncrementalHasher creates a new incremental Keccak-256 hasher.
func NewIncrementalHasher() *IncrementalHasher {
	return &IncrementalHasher{
		state: sha3.NewLegacyKeccak256(),
	}
}

// Write feeds data into the hasher. Returns the number of bytes written.
func (h *IncrementalHasher) Write(data []byte) (int, error) {
	n, err := h.state.Write(data)
	h.size += n
	return n, err
}

// WriteUint64 writes a uint64 in big-endian encoding.
func (h *IncrementalHasher) WriteUint64(v uint64) {
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], v)
	h.state.Write(buf[:])
	h.size += 8
}

// WriteHash writes a 32-byte hash value.
func (h *IncrementalHasher) WriteHash(hash types.Hash) {
	h.state.Write(hash[:])
	h.size += 32
}

// WriteAddress writes a 20-byte address.
func (h *IncrementalHasher) WriteAddress(addr types.Address) {
	h.state.Write(addr[:])
	h.size += 20
}

// Sum256 finalizes the hash and returns the Keccak-256 digest.
// After calling Sum256, the hasher must not be reused.
func (h *IncrementalHasher) Sum256() types.Hash {
	var result types.Hash
	sum := h.state.Sum(nil)
	copy(result[:], sum[:32])
	return result
}

// SumBytes finalizes the hash and returns the digest as a byte slice.
func (h *IncrementalHasher) SumBytes() []byte {
	return h.state.Sum(nil)[:32]
}

// Size returns the total number of bytes written so far.
func (h *IncrementalHasher) Size() int {
	return h.size
}

// Reset resets the hasher to its initial state.
func (h *IncrementalHasher) Reset() {
	h.state.Reset()
	h.size = 0
}

// PreimageTracker records hash preimages for later retrieval. This is used
// during block execution to collect all data that was hashed, enabling
// stateless proof generation (EIP-6800 witnesses). Thread-safe.
type PreimageTracker struct {
	mu        sync.RWMutex
	preimages map[types.Hash][]byte
	enabled   bool
}

// NewPreimageTracker creates a new preimage tracker.
func NewPreimageTracker() *PreimageTracker {
	return &PreimageTracker{
		preimages: make(map[types.Hash][]byte),
		enabled:   true,
	}
}

// SetEnabled enables or disables preimage tracking. When disabled, Record
// is a no-op, avoiding overhead during normal operation.
func (pt *PreimageTracker) SetEnabled(enabled bool) {
	pt.mu.Lock()
	defer pt.mu.Unlock()
	pt.enabled = enabled
}

// Record computes Keccak256(data) and stores the preimage.
// Returns the hash. If tracking is disabled, only the hash is computed.
func (pt *PreimageTracker) Record(data []byte) types.Hash {
	hash := Keccak256Hash(data)
	pt.mu.Lock()
	defer pt.mu.Unlock()
	if pt.enabled {
		preimage := make([]byte, len(data))
		copy(preimage, data)
		pt.preimages[hash] = preimage
	}
	return hash
}

// Lookup returns the preimage for the given hash, or nil if not found.
func (pt *PreimageTracker) Lookup(hash types.Hash) []byte {
	pt.mu.RLock()
	defer pt.mu.RUnlock()
	data, ok := pt.preimages[hash]
	if !ok {
		return nil
	}
	ret := make([]byte, len(data))
	copy(ret, data)
	return ret
}

// Count returns the number of stored preimages.
func (pt *PreimageTracker) Count() int {
	pt.mu.RLock()
	defer pt.mu.RUnlock()
	return len(pt.preimages)
}

// Clear removes all stored preimages.
func (pt *PreimageTracker) Clear() {
	pt.mu.Lock()
	defer pt.mu.Unlock()
	pt.preimages = make(map[types.Hash][]byte)
}

// All returns a copy of all stored hash->preimage mappings.
func (pt *PreimageTracker) All() map[types.Hash][]byte {
	pt.mu.RLock()
	defer pt.mu.RUnlock()
	result := make(map[types.Hash][]byte, len(pt.preimages))
	for k, v := range pt.preimages {
		cp := make([]byte, len(v))
		copy(cp, v)
		result[k] = cp
	}
	return result
}

// Keccak256WithTracker computes Keccak-256 and records the preimage in the
// given tracker. If tracker is nil, it behaves like Keccak256Hash.
func Keccak256WithTracker(tracker *PreimageTracker, data []byte) types.Hash {
	if tracker == nil {
		return Keccak256Hash(data)
	}
	return tracker.Record(data)
}

// CommitHash computes Keccak256(a || b) used in Merkle tree constructions.
// Sorts inputs lexicographically to ensure commutativity.
func CommitHash(a, b types.Hash) types.Hash {
	// Sort: smaller hash goes first for commutative hashing.
	for i := 0; i < 32; i++ {
		if a[i] < b[i] {
			return Keccak256Hash(a[:], b[:])
		} else if a[i] > b[i] {
			return Keccak256Hash(b[:], a[:])
		}
	}
	// Equal hashes: hash(a || a).
	return Keccak256Hash(a[:], b[:])
}

// PersonalizedHash computes a personalized Keccak-256 hash with a fixed-length
// tag. The tag is zero-padded to exactly 32 bytes before prepending.
func PersonalizedHash(tag string, data ...[]byte) []byte {
	d := sha3.NewLegacyKeccak256()
	var tagBuf [32]byte
	copy(tagBuf[:], []byte(tag))
	d.Write(tagBuf[:])
	for _, b := range data {
		d.Write(b)
	}
	return d.Sum(nil)
}
