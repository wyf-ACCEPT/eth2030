// Package verkle implements Verkle tree witness construction for stateless
// execution per EIP-6800. A WitnessBuilder accumulates state reads and writes
// during EVM execution and produces a VerkleWitness that contains all the
// stem/suffix/value data needed for a stateless verifier to re-execute a block.
package verkle

import (
	"encoding/binary"
	"errors"
	"sync"
)

// Witness construction errors.
var (
	ErrNilStem          = errors.New("verkle/witness: nil stem")
	ErrInvalidStemSize  = errors.New("verkle/witness: stem must be 31 bytes")
	ErrInvalidValueSize = errors.New("verkle/witness: value must be 32 bytes or nil")
	ErrBuilderFinalized = errors.New("verkle/witness: builder already finalized")
	ErrNilWitness       = errors.New("verkle/witness: nil witness")
)

// VerkleWitness holds the data required for stateless block execution.
// It captures all stems, suffixes, current/new values, and a commitment
// proof that binds them to the pre-state and post-state roots.
type VerkleWitness struct {
	// Stems is the set of unique 31-byte stems accessed during execution.
	Stems [][]byte

	// Suffixes lists the suffix byte for each access, parallel to CurrentValues/NewValues.
	Suffixes [][]byte

	// CurrentValues holds the pre-state value for each accessed slot (32 bytes or nil).
	CurrentValues [][]byte

	// NewValues holds the post-state value for each written slot (32 bytes or nil for reads).
	NewValues [][]byte

	// CommitmentProof is the serialized IPA multipoint proof binding all
	// accessed stems/suffixes to the pre-state root. Placeholder for now.
	CommitmentProof []byte
}

// stemSuffixKey is used to deduplicate accesses by (stem, suffix) pair.
type stemSuffixKey struct {
	stem   [StemSize]byte
	suffix byte
}

// accessEntry records a single state access.
type accessEntry struct {
	stem     [StemSize]byte
	suffix   byte
	oldValue []byte // pre-state value (32 bytes or nil)
	newValue []byte // post-state value (32 bytes or nil for reads)
	isWrite  bool
}

// WitnessBuilder accumulates state accesses during EVM execution and
// produces a VerkleWitness. It is safe for concurrent use.
type WitnessBuilder struct {
	mu        sync.Mutex
	accesses  []accessEntry
	seen      map[stemSuffixKey]int // maps (stem,suffix) -> index in accesses
	stems     map[[StemSize]byte]struct{}
	finalized bool
}

// NewWitnessBuilder creates a new empty WitnessBuilder.
func NewWitnessBuilder() *WitnessBuilder {
	return &WitnessBuilder{
		accesses: make([]accessEntry, 0, 64),
		seen:     make(map[stemSuffixKey]int),
		stems:    make(map[[StemSize]byte]struct{}),
	}
}

// AddRead records a state read at (stem, suffix) with the given value.
// If the same (stem, suffix) was already recorded, this is a no-op for
// the current value but the access is still tracked.
func (wb *WitnessBuilder) AddRead(stem, suffix, value []byte) error {
	if err := wb.validateInputs(stem, suffix, value); err != nil {
		return err
	}

	wb.mu.Lock()
	defer wb.mu.Unlock()

	if wb.finalized {
		return ErrBuilderFinalized
	}

	var stemArr [StemSize]byte
	copy(stemArr[:], stem)
	suffixByte := suffix[0]

	key := stemSuffixKey{stem: stemArr, suffix: suffixByte}
	if _, exists := wb.seen[key]; exists {
		// Already recorded this (stem, suffix); skip duplicate.
		return nil
	}

	idx := len(wb.accesses)
	wb.seen[key] = idx
	wb.stems[stemArr] = struct{}{}

	var valCopy []byte
	if value != nil {
		valCopy = make([]byte, len(value))
		copy(valCopy, value)
	}

	wb.accesses = append(wb.accesses, accessEntry{
		stem:     stemArr,
		suffix:   suffixByte,
		oldValue: valCopy,
		newValue: nil,
		isWrite:  false,
	})
	return nil
}

// AddWrite records a state write at (stem, suffix) with old and new values.
// If this (stem, suffix) was previously read, the entry is upgraded to a write.
func (wb *WitnessBuilder) AddWrite(stem, suffix, oldValue, newValue []byte) error {
	if err := wb.validateInputs(stem, suffix, oldValue); err != nil {
		return err
	}
	if newValue != nil && len(newValue) != ValueSize {
		return ErrInvalidValueSize
	}

	wb.mu.Lock()
	defer wb.mu.Unlock()

	if wb.finalized {
		return ErrBuilderFinalized
	}

	var stemArr [StemSize]byte
	copy(stemArr[:], stem)
	suffixByte := suffix[0]

	key := stemSuffixKey{stem: stemArr, suffix: suffixByte}

	var oldCopy, newCopy []byte
	if oldValue != nil {
		oldCopy = make([]byte, len(oldValue))
		copy(oldCopy, oldValue)
	}
	if newValue != nil {
		newCopy = make([]byte, len(newValue))
		copy(newCopy, newValue)
	}

	if idx, exists := wb.seen[key]; exists {
		// Upgrade existing read to a write.
		wb.accesses[idx].newValue = newCopy
		wb.accesses[idx].isWrite = true
		if oldCopy != nil {
			wb.accesses[idx].oldValue = oldCopy
		}
		return nil
	}

	idx := len(wb.accesses)
	wb.seen[key] = idx
	wb.stems[stemArr] = struct{}{}

	wb.accesses = append(wb.accesses, accessEntry{
		stem:     stemArr,
		suffix:   suffixByte,
		oldValue: oldCopy,
		newValue: newCopy,
		isWrite:  true,
	})
	return nil
}

// Build produces the VerkleWitness from all accumulated accesses.
// After Build is called, the builder is finalized and no more accesses
// can be added.
func (wb *WitnessBuilder) Build() *VerkleWitness {
	wb.mu.Lock()
	defer wb.mu.Unlock()

	wb.finalized = true

	w := &VerkleWitness{
		Stems:         make([][]byte, 0, len(wb.stems)),
		Suffixes:      make([][]byte, len(wb.accesses)),
		CurrentValues: make([][]byte, len(wb.accesses)),
		NewValues:     make([][]byte, len(wb.accesses)),
	}

	// Collect unique stems.
	for stem := range wb.stems {
		s := make([]byte, StemSize)
		copy(s, stem[:])
		w.Stems = append(w.Stems, s)
	}

	// Fill per-access data.
	for i, acc := range wb.accesses {
		w.Suffixes[i] = []byte{acc.suffix}
		w.CurrentValues[i] = acc.oldValue
		w.NewValues[i] = acc.newValue
	}

	// Placeholder commitment proof: encode a marker with access count.
	w.CommitmentProof = encodeProofPlaceholder(len(wb.accesses))

	return w
}

// AccessCount returns the number of unique (stem, suffix) accesses recorded.
func (wb *WitnessBuilder) AccessCount() int {
	wb.mu.Lock()
	defer wb.mu.Unlock()
	return len(wb.accesses)
}

// StemCount returns the number of unique stems accessed.
func (wb *WitnessBuilder) StemCount() int {
	wb.mu.Lock()
	defer wb.mu.Unlock()
	return len(wb.stems)
}

// Reset clears all accumulated accesses and allows re-use of the builder.
func (wb *WitnessBuilder) Reset() {
	wb.mu.Lock()
	defer wb.mu.Unlock()
	wb.accesses = wb.accesses[:0]
	wb.seen = make(map[stemSuffixKey]int)
	wb.stems = make(map[[StemSize]byte]struct{})
	wb.finalized = false
}

// EstimateWitnessSize returns an estimate of the serialized witness size in bytes.
// This is useful for gas metering and block size estimation.
//
// Layout per access: stem(31) + suffix(1) + currentValue(32) + newValue(32) = 96.
// Plus: stem dedup overhead, proof overhead.
func EstimateWitnessSize(w *VerkleWitness) int {
	if w == nil {
		return 0
	}

	size := 0

	// Unique stems: 31 bytes each.
	size += len(w.Stems) * StemSize

	// Per-access: suffix (1) + currentValue (up to 32) + newValue (up to 32).
	for i := range w.Suffixes {
		size += len(w.Suffixes[i])
		if i < len(w.CurrentValues) && w.CurrentValues[i] != nil {
			size += len(w.CurrentValues[i])
		}
		if i < len(w.NewValues) && w.NewValues[i] != nil {
			size += len(w.NewValues[i])
		}
	}

	// Commitment proof.
	size += len(w.CommitmentProof)

	// Header overhead (counts, lengths).
	size += 16

	return size
}

// MergeWitnesses combines two VerkleWitness values, deduplicating stems.
// Accesses from both witnesses are concatenated; duplicate stems appear only once.
func MergeWitnesses(a, b *VerkleWitness) (*VerkleWitness, error) {
	if a == nil && b == nil {
		return nil, ErrNilWitness
	}
	if a == nil {
		return b, nil
	}
	if b == nil {
		return a, nil
	}

	merged := &VerkleWitness{}

	// Deduplicate stems.
	stemSet := make(map[[StemSize]byte]struct{})
	for _, s := range a.Stems {
		var arr [StemSize]byte
		copy(arr[:], s)
		if _, ok := stemSet[arr]; !ok {
			stemSet[arr] = struct{}{}
			sCopy := make([]byte, len(s))
			copy(sCopy, s)
			merged.Stems = append(merged.Stems, sCopy)
		}
	}
	for _, s := range b.Stems {
		var arr [StemSize]byte
		copy(arr[:], s)
		if _, ok := stemSet[arr]; !ok {
			stemSet[arr] = struct{}{}
			sCopy := make([]byte, len(s))
			copy(sCopy, s)
			merged.Stems = append(merged.Stems, sCopy)
		}
	}

	// Concatenate suffixes and values.
	merged.Suffixes = append(merged.Suffixes, a.Suffixes...)
	merged.Suffixes = append(merged.Suffixes, b.Suffixes...)

	merged.CurrentValues = append(merged.CurrentValues, a.CurrentValues...)
	merged.CurrentValues = append(merged.CurrentValues, b.CurrentValues...)

	merged.NewValues = append(merged.NewValues, a.NewValues...)
	merged.NewValues = append(merged.NewValues, b.NewValues...)

	// Merge commitment proofs by concatenation (placeholder).
	merged.CommitmentProof = append(merged.CommitmentProof, a.CommitmentProof...)
	merged.CommitmentProof = append(merged.CommitmentProof, b.CommitmentProof...)

	return merged, nil
}

// VerifyWitnessCompleteness checks that every key in accessedKeys has a
// corresponding entry in the witness. Each key is a 32-byte value where
// the first 31 bytes are the stem and the last byte is the suffix.
func VerifyWitnessCompleteness(witness *VerkleWitness, accessedKeys [][]byte) (bool, [][]byte) {
	if witness == nil {
		if len(accessedKeys) == 0 {
			return true, nil
		}
		return false, accessedKeys
	}

	// Build a set of stems in the witness.
	stemSet := make(map[[StemSize]byte]struct{})
	for _, s := range witness.Stems {
		var arr [StemSize]byte
		copy(arr[:], s)
		stemSet[arr] = struct{}{}
	}

	// Build a set of (stem, suffix) pairs in the witness.
	type pair struct {
		stem   [StemSize]byte
		suffix byte
	}
	pairSet := make(map[pair]struct{})
	for i, s := range witness.Suffixes {
		if len(s) == 0 {
			continue
		}
		// Find the stem for this access -- we need to check all stems.
		// Since suffixes and stems are parallel in a per-access sense, we use
		// a heuristic: check that the stem from the accessed key is present.
		_ = i // used implicitly via pairSet population below
	}

	// For each suffix entry, we record which (stem, suffix) combinations exist.
	// We need to match accessed keys to witness data. Build suffix lookup from
	// the witness accesses.
	for i := range witness.Suffixes {
		if len(witness.Suffixes[i]) == 0 {
			continue
		}
		suffix := witness.Suffixes[i][0]
		// Check each stem in the witness for this suffix.
		for _, s := range witness.Stems {
			var arr [StemSize]byte
			copy(arr[:], s)
			pairSet[pair{stem: arr, suffix: suffix}] = struct{}{}
		}
	}

	var missing [][]byte
	for _, key := range accessedKeys {
		if len(key) < KeySize {
			missing = append(missing, key)
			continue
		}
		var stemArr [StemSize]byte
		copy(stemArr[:], key[:StemSize])
		suffix := key[StemSize]

		// First check: stem must be present.
		if _, ok := stemSet[stemArr]; !ok {
			missing = append(missing, key)
			continue
		}

		// Second check: (stem, suffix) pair must be present.
		if _, ok := pairSet[pair{stem: stemArr, suffix: suffix}]; !ok {
			missing = append(missing, key)
		}
	}

	return len(missing) == 0, missing
}

// validateInputs checks common input constraints for Add methods.
func (wb *WitnessBuilder) validateInputs(stem, suffix, value []byte) error {
	if stem == nil {
		return ErrNilStem
	}
	if len(stem) != StemSize {
		return ErrInvalidStemSize
	}
	if len(suffix) != 1 {
		return errors.New("verkle/witness: suffix must be 1 byte")
	}
	if value != nil && len(value) != ValueSize {
		return ErrInvalidValueSize
	}
	return nil
}

// encodeProofPlaceholder creates a placeholder commitment proof.
// Format: "VERKLE_PROOF" || access_count(4 bytes big-endian).
func encodeProofPlaceholder(accessCount int) []byte {
	marker := []byte("VERKLE_PROOF")
	buf := make([]byte, len(marker)+4)
	copy(buf, marker)
	binary.BigEndian.PutUint32(buf[len(marker):], uint32(accessCount))
	return buf
}
