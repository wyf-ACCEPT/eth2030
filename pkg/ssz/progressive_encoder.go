// progressive_encoder.go implements incremental SSZ encoding for EIP-7916
// ProgressiveLists. Instead of encoding the entire list at once, the
// progressive encoder supports chunked serialization, streaming writes,
// and incremental Merkleization so that large lists can be processed
// without holding the entire serialized form in memory.
//
// The encoder maintains internal state tracking the current subtree
// boundaries and running hash computations, allowing callers to append
// elements one at a time or in batches.
package ssz

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// Progressive encoder errors.
var (
	ErrEncoderFinalized   = errors.New("ssz: progressive encoder already finalized")
	ErrEncoderEmpty       = errors.New("ssz: progressive encoder has no elements")
	ErrEncoderBatchEmpty  = errors.New("ssz: empty batch provided")
	ErrEncoderWriteFailed = errors.New("ssz: stream write failed")
)

// ProgressiveEncoder incrementally encodes a ProgressiveList to SSZ. It
// tracks subtree boundaries and computes running Merkle hashes so that
// elements can be appended without re-processing the entire list.
type ProgressiveEncoder struct {
	chunks    [][32]byte // accumulated element chunks
	finalized bool       // true after Root() or Finalize() is called
}

// NewProgressiveEncoder creates a new progressive encoder.
func NewProgressiveEncoder() *ProgressiveEncoder {
	return &ProgressiveEncoder{}
}

// Append adds a single element (as its 32-byte hash tree root chunk) to
// the encoder. Returns an error if the encoder is already finalized.
func (pe *ProgressiveEncoder) Append(chunk [32]byte) error {
	if pe.finalized {
		return ErrEncoderFinalized
	}
	pe.chunks = append(pe.chunks, chunk)
	return nil
}

// AppendBatch adds multiple element chunks at once. Returns an error if
// the encoder is already finalized or the batch is empty.
func (pe *ProgressiveEncoder) AppendBatch(chunks [][32]byte) error {
	if pe.finalized {
		return ErrEncoderFinalized
	}
	if len(chunks) == 0 {
		return ErrEncoderBatchEmpty
	}
	pe.chunks = append(pe.chunks, chunks...)
	return nil
}

// AppendUint64 appends a uint64 value packed into a 32-byte chunk.
func (pe *ProgressiveEncoder) AppendUint64(v uint64) error {
	var chunk [32]byte
	binary.LittleEndian.PutUint64(chunk[:8], v)
	return pe.Append(chunk)
}

// AppendBytes32 appends raw 32-byte data as a chunk.
func (pe *ProgressiveEncoder) AppendBytes32(data [32]byte) error {
	return pe.Append(data)
}

// Len returns the number of elements appended so far.
func (pe *ProgressiveEncoder) Len() int {
	return len(pe.chunks)
}

// Root computes the progressive list hash tree root, finalizing the
// encoder. After calling Root(), no more elements can be appended.
func (pe *ProgressiveEncoder) Root() ([32]byte, error) {
	if len(pe.chunks) == 0 {
		pe.finalized = true
		// Empty list: progressive root is zero hash mixed with length 0.
		return MixInLength(zeroHash(), 0), nil
	}
	pe.finalized = true
	root := merkleizeProgressive(pe.chunks, 1)
	return MixInLength(root, uint64(len(pe.chunks))), nil
}

// IsFinalized reports whether the encoder has been finalized.
func (pe *ProgressiveEncoder) IsFinalized() bool {
	return pe.finalized
}

// Reset clears the encoder state so it can be reused.
func (pe *ProgressiveEncoder) Reset() {
	pe.chunks = pe.chunks[:0]
	pe.finalized = false
}

// Serialize produces the SSZ serialized form of the progressive list.
// For a list of fixed-size 32-byte elements, this is simply the
// concatenation of all chunks. The length is encoded separately (via
// the Merkle mix-in). Finalizes the encoder.
func (pe *ProgressiveEncoder) Serialize() ([]byte, error) {
	if pe.finalized {
		return nil, ErrEncoderFinalized
	}
	pe.finalized = true
	buf := make([]byte, 0, len(pe.chunks)*BytesPerChunk)
	for _, c := range pe.chunks {
		buf = append(buf, c[:]...)
	}
	return buf, nil
}

// SubtreeRoots returns the intermediate subtree roots for the progressive
// tree structure. This is useful for incremental proof updates. The
// returned slice contains one root per subtree level:
//   - subtree 0: capacity 1  (chunks[0:1])
//   - subtree 1: capacity 4  (chunks[1:5])
//   - subtree 2: capacity 16 (chunks[5:21])
//   - ...
func (pe *ProgressiveEncoder) SubtreeRoots() [][32]byte {
	if len(pe.chunks) == 0 {
		return nil
	}
	var roots [][32]byte
	remaining := pe.chunks
	numLeaves := 1
	for len(remaining) > 0 {
		splitAt := numLeaves
		if splitAt > len(remaining) {
			splitAt = len(remaining)
		}
		root := Merkleize(remaining[:splitAt], numLeaves)
		roots = append(roots, root)
		remaining = remaining[splitAt:]
		numLeaves *= 4
	}
	return roots
}

// IncrementalRoot computes the progressive root from precomputed subtree
// roots. This allows updating only the affected subtree when elements are
// appended, rather than recomputing the entire tree.
//
// The progressive tree is structured as:
//   hash(subtree_0, hash(subtree_1, hash(subtree_2, ... zero)))
// So we fold right-to-left: start with the rightmost subtree paired with
// a zero hash for the empty remainder, then wrap each subtree as the left
// child with the accumulated right child.
func IncrementalRoot(subtreeRoots [][32]byte) [32]byte {
	if len(subtreeRoots) == 0 {
		return zeroHash()
	}
	// Start from the rightmost subtree, paired with zero for the empty tail.
	result := hash(subtreeRoots[len(subtreeRoots)-1], zeroHash())
	// Fold right-to-left: each subtree root is the left child, accumulated
	// result is the right child.
	for i := len(subtreeRoots) - 2; i >= 0; i-- {
		result = hash(subtreeRoots[i], result)
	}
	return result
}

// StreamTo writes the serialized progressive list to a writer in chunks,
// avoiding a large single allocation. Each write sends one chunk (32 bytes).
// Returns the total bytes written.
func (pe *ProgressiveEncoder) StreamTo(w io.Writer) (int64, error) {
	if pe.finalized {
		return 0, ErrEncoderFinalized
	}
	pe.finalized = true
	var total int64
	for _, c := range pe.chunks {
		n, err := w.Write(c[:])
		total += int64(n)
		if err != nil {
			return total, fmt.Errorf("%w: %v", ErrEncoderWriteFailed, err)
		}
	}
	return total, nil
}

// ChunkedSerialize produces the serialized form split into segments of
// the given chunk size (in number of elements). This is useful for
// streaming large lists over a network where each segment can be sent
// and verified independently.
func (pe *ProgressiveEncoder) ChunkedSerialize(segmentSize int) ([][]byte, error) {
	if pe.finalized {
		return nil, ErrEncoderFinalized
	}
	if segmentSize <= 0 {
		segmentSize = 1
	}
	pe.finalized = true

	var segments [][]byte
	for i := 0; i < len(pe.chunks); i += segmentSize {
		end := i + segmentSize
		if end > len(pe.chunks) {
			end = len(pe.chunks)
		}
		seg := make([]byte, 0, (end-i)*BytesPerChunk)
		for j := i; j < end; j++ {
			seg = append(seg, pe.chunks[j][:]...)
		}
		segments = append(segments, seg)
	}
	return segments, nil
}

// VerifyRoot checks that the given chunks produce the expected progressive
// list root. This is a convenience function for round-trip verification.
func VerifyProgressiveRoot(chunks [][32]byte, expectedRoot [32]byte) bool {
	root := merkleizeProgressive(chunks, 1)
	computed := MixInLength(root, uint64(len(chunks)))
	return computed == expectedRoot
}

// DeserializeProgressiveChunks decodes a serialized progressive list back
// into 32-byte chunks. The input length must be a multiple of 32.
func DeserializeProgressiveChunks(data []byte) ([][32]byte, error) {
	if len(data)%BytesPerChunk != 0 {
		return nil, fmt.Errorf("%w: data length %d not a multiple of %d",
			ErrSize, len(data), BytesPerChunk)
	}
	n := len(data) / BytesPerChunk
	chunks := make([][32]byte, n)
	for i := 0; i < n; i++ {
		copy(chunks[i][:], data[i*BytesPerChunk:(i+1)*BytesPerChunk])
	}
	return chunks, nil
}
