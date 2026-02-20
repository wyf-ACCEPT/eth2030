package ssz

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestProgressiveEncoderAppendAndRoot(t *testing.T) {
	enc := NewProgressiveEncoder()
	if enc.Len() != 0 {
		t.Fatalf("expected empty encoder, got len %d", enc.Len())
	}

	// Append single elements.
	var chunks [][32]byte
	for i := 0; i < 10; i++ {
		var chunk [32]byte
		binary.LittleEndian.PutUint64(chunk[:8], uint64(i))
		chunks = append(chunks, chunk)
		if err := enc.Append(chunk); err != nil {
			t.Fatalf("Append(%d): %v", i, err)
		}
	}
	if enc.Len() != 10 {
		t.Fatalf("expected len 10, got %d", enc.Len())
	}

	// Compute root via the encoder.
	root, err := enc.Root()
	if err != nil {
		t.Fatalf("Root: %v", err)
	}

	// Compute expected root using the existing ProgressiveList.
	pl := NewProgressiveList(chunks)
	expected := pl.HashTreeRoot()
	if root != expected {
		t.Fatalf("root mismatch:\n  got  %x\n  want %x", root, expected)
	}
}

func TestProgressiveEncoderEmptyRoot(t *testing.T) {
	enc := NewProgressiveEncoder()
	root, err := enc.Root()
	if err != nil {
		t.Fatalf("Root on empty: %v", err)
	}
	// Empty progressive list root = MixInLength(zero, 0).
	expected := MixInLength(zeroHash(), 0)
	if root != expected {
		t.Fatalf("empty root mismatch:\n  got  %x\n  want %x", root, expected)
	}
}

func TestProgressiveEncoderFinalizedError(t *testing.T) {
	enc := NewProgressiveEncoder()
	enc.AppendUint64(42)
	_, err := enc.Root()
	if err != nil {
		t.Fatalf("Root: %v", err)
	}
	if !enc.IsFinalized() {
		t.Fatal("expected finalized after Root()")
	}
	// Append after finalize should fail.
	if err := enc.Append([32]byte{}); err != ErrEncoderFinalized {
		t.Fatalf("expected ErrEncoderFinalized, got %v", err)
	}
	// AppendBatch after finalize should fail.
	if err := enc.AppendBatch([][32]byte{{}}); err != ErrEncoderFinalized {
		t.Fatalf("expected ErrEncoderFinalized, got %v", err)
	}
}

func TestProgressiveEncoderAppendBatch(t *testing.T) {
	enc := NewProgressiveEncoder()
	var chunks [][32]byte
	for i := 0; i < 5; i++ {
		var c [32]byte
		c[0] = byte(i + 1)
		chunks = append(chunks, c)
	}
	if err := enc.AppendBatch(chunks); err != nil {
		t.Fatalf("AppendBatch: %v", err)
	}
	if enc.Len() != 5 {
		t.Fatalf("expected len 5, got %d", enc.Len())
	}
	root, err := enc.Root()
	if err != nil {
		t.Fatalf("Root: %v", err)
	}
	expected := HashTreeRootProgressiveList(chunks)
	if root != expected {
		t.Fatalf("batch root mismatch:\n  got  %x\n  want %x", root, expected)
	}
}

func TestProgressiveEncoderAppendBatchEmpty(t *testing.T) {
	enc := NewProgressiveEncoder()
	if err := enc.AppendBatch(nil); err != ErrEncoderBatchEmpty {
		t.Fatalf("expected ErrEncoderBatchEmpty, got %v", err)
	}
}

func TestProgressiveEncoderAppendUint64(t *testing.T) {
	enc := NewProgressiveEncoder()
	values := []uint64{100, 200, 300}
	for _, v := range values {
		if err := enc.AppendUint64(v); err != nil {
			t.Fatalf("AppendUint64(%d): %v", v, err)
		}
	}
	root, err := enc.Root()
	if err != nil {
		t.Fatalf("Root: %v", err)
	}

	// Build expected chunks manually.
	var chunks [][32]byte
	for _, v := range values {
		var c [32]byte
		binary.LittleEndian.PutUint64(c[:8], v)
		chunks = append(chunks, c)
	}
	expected := HashTreeRootProgressiveList(chunks)
	if root != expected {
		t.Fatalf("uint64 root mismatch:\n  got  %x\n  want %x", root, expected)
	}
}

func TestProgressiveEncoderSerialize(t *testing.T) {
	enc := NewProgressiveEncoder()
	var chunks [][32]byte
	for i := 0; i < 3; i++ {
		var c [32]byte
		c[0] = byte(i + 10)
		chunks = append(chunks, c)
		enc.Append(c)
	}

	data, err := enc.Serialize()
	if err != nil {
		t.Fatalf("Serialize: %v", err)
	}
	if len(data) != 3*BytesPerChunk {
		t.Fatalf("expected %d bytes, got %d", 3*BytesPerChunk, len(data))
	}

	// Verify round-trip.
	decoded, err := DeserializeProgressiveChunks(data)
	if err != nil {
		t.Fatalf("DeserializeProgressiveChunks: %v", err)
	}
	if len(decoded) != len(chunks) {
		t.Fatalf("expected %d chunks, got %d", len(chunks), len(decoded))
	}
	for i, c := range decoded {
		if c != chunks[i] {
			t.Fatalf("chunk %d mismatch", i)
		}
	}
}

func TestProgressiveEncoderSerializeFinalized(t *testing.T) {
	enc := NewProgressiveEncoder()
	enc.AppendUint64(1)
	enc.Serialize()
	_, err := enc.Serialize()
	if err != ErrEncoderFinalized {
		t.Fatalf("expected ErrEncoderFinalized, got %v", err)
	}
}

func TestProgressiveEncoderStreamTo(t *testing.T) {
	enc := NewProgressiveEncoder()
	for i := 0; i < 4; i++ {
		var c [32]byte
		c[31] = byte(i)
		enc.Append(c)
	}
	var buf bytes.Buffer
	n, err := enc.StreamTo(&buf)
	if err != nil {
		t.Fatalf("StreamTo: %v", err)
	}
	if n != int64(4*BytesPerChunk) {
		t.Fatalf("expected %d bytes written, got %d", 4*BytesPerChunk, n)
	}
	if buf.Len() != 4*BytesPerChunk {
		t.Fatalf("buffer length mismatch: %d", buf.Len())
	}
}

func TestProgressiveEncoderStreamToFinalized(t *testing.T) {
	enc := NewProgressiveEncoder()
	enc.AppendUint64(1)
	enc.StreamTo(&bytes.Buffer{})
	_, err := enc.StreamTo(&bytes.Buffer{})
	if err != ErrEncoderFinalized {
		t.Fatalf("expected ErrEncoderFinalized, got %v", err)
	}
}

func TestProgressiveEncoderReset(t *testing.T) {
	enc := NewProgressiveEncoder()
	enc.AppendUint64(42)
	enc.Root()
	if !enc.IsFinalized() {
		t.Fatal("expected finalized")
	}

	enc.Reset()
	if enc.IsFinalized() {
		t.Fatal("expected not finalized after Reset")
	}
	if enc.Len() != 0 {
		t.Fatalf("expected len 0 after Reset, got %d", enc.Len())
	}

	// Should be reusable.
	enc.AppendUint64(99)
	root, err := enc.Root()
	if err != nil {
		t.Fatalf("Root after Reset: %v", err)
	}
	var chunk [32]byte
	binary.LittleEndian.PutUint64(chunk[:8], 99)
	expected := HashTreeRootProgressiveList([][32]byte{chunk})
	if root != expected {
		t.Fatalf("root after reset mismatch")
	}
}

func TestProgressiveEncoderSubtreeRoots(t *testing.T) {
	enc := NewProgressiveEncoder()
	// Add 6 elements: subtree 0 gets 1, subtree 1 gets 4, subtree 2 gets 1.
	for i := 0; i < 6; i++ {
		var c [32]byte
		c[0] = byte(i + 1)
		enc.Append(c)
	}
	roots := enc.SubtreeRoots()
	// With 6 elements: subtree capacities are 1, 4, 16.
	// Split: [0:1], [1:5], [5:6]. So 3 subtree roots.
	if len(roots) != 3 {
		t.Fatalf("expected 3 subtree roots, got %d", len(roots))
	}
	// Verify incremental root matches full root.
	incRoot := IncrementalRoot(roots)
	fullRoot := merkleizeProgressive(enc.chunks, 1)
	if incRoot != fullRoot {
		t.Fatalf("incremental root mismatch:\n  got  %x\n  want %x", incRoot, fullRoot)
	}
}

func TestProgressiveEncoderSubtreeRootsEmpty(t *testing.T) {
	enc := NewProgressiveEncoder()
	roots := enc.SubtreeRoots()
	if roots != nil {
		t.Fatalf("expected nil subtree roots for empty encoder, got %d", len(roots))
	}
}

func TestProgressiveEncoderChunkedSerialize(t *testing.T) {
	enc := NewProgressiveEncoder()
	for i := 0; i < 7; i++ {
		var c [32]byte
		c[0] = byte(i)
		enc.Append(c)
	}
	segments, err := enc.ChunkedSerialize(3)
	if err != nil {
		t.Fatalf("ChunkedSerialize: %v", err)
	}
	// 7 elements with segment size 3: segments of 3, 3, 1.
	if len(segments) != 3 {
		t.Fatalf("expected 3 segments, got %d", len(segments))
	}
	if len(segments[0]) != 3*BytesPerChunk {
		t.Fatalf("segment 0 length: expected %d, got %d", 3*BytesPerChunk, len(segments[0]))
	}
	if len(segments[2]) != 1*BytesPerChunk {
		t.Fatalf("segment 2 length: expected %d, got %d", 1*BytesPerChunk, len(segments[2]))
	}
}

func TestProgressiveEncoderChunkedSerializeFinalized(t *testing.T) {
	enc := NewProgressiveEncoder()
	enc.AppendUint64(1)
	enc.ChunkedSerialize(1)
	_, err := enc.ChunkedSerialize(1)
	if err != ErrEncoderFinalized {
		t.Fatalf("expected ErrEncoderFinalized, got %v", err)
	}
}

func TestVerifyProgressiveRoot(t *testing.T) {
	var chunks [][32]byte
	for i := 0; i < 5; i++ {
		var c [32]byte
		c[0] = byte(i + 1)
		chunks = append(chunks, c)
	}
	pl := NewProgressiveList(chunks)
	root := pl.HashTreeRoot()

	if !VerifyProgressiveRoot(chunks, root) {
		t.Fatal("VerifyProgressiveRoot returned false for valid root")
	}
	// Tamper with root.
	bad := root
	bad[0] ^= 0xff
	if VerifyProgressiveRoot(chunks, bad) {
		t.Fatal("VerifyProgressiveRoot returned true for invalid root")
	}
}

func TestDeserializeProgressiveChunksInvalid(t *testing.T) {
	// Data not a multiple of 32.
	_, err := DeserializeProgressiveChunks(make([]byte, 33))
	if err == nil {
		t.Fatal("expected error for invalid data length")
	}
}

func TestDeserializeProgressiveChunksEmpty(t *testing.T) {
	chunks, err := DeserializeProgressiveChunks(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(chunks) != 0 {
		t.Fatalf("expected 0 chunks, got %d", len(chunks))
	}
}

func TestProgressiveEncoderLargeList(t *testing.T) {
	// Test with a larger number of elements to exercise multiple subtrees.
	enc := NewProgressiveEncoder()
	var chunks [][32]byte
	for i := 0; i < 100; i++ {
		var c [32]byte
		binary.LittleEndian.PutUint64(c[:8], uint64(i*7+3))
		chunks = append(chunks, c)
		enc.Append(c)
	}
	root, err := enc.Root()
	if err != nil {
		t.Fatalf("Root: %v", err)
	}
	expected := HashTreeRootProgressiveList(chunks)
	if root != expected {
		t.Fatalf("large list root mismatch:\n  got  %x\n  want %x", root, expected)
	}
}

func TestProgressiveEncoderAppendBytes32(t *testing.T) {
	enc := NewProgressiveEncoder()
	var data [32]byte
	data[15] = 0xab
	if err := enc.AppendBytes32(data); err != nil {
		t.Fatalf("AppendBytes32: %v", err)
	}
	if enc.Len() != 1 {
		t.Fatalf("expected len 1, got %d", enc.Len())
	}
	root, err := enc.Root()
	if err != nil {
		t.Fatalf("Root: %v", err)
	}
	expected := HashTreeRootProgressiveList([][32]byte{data})
	if root != expected {
		t.Fatalf("bytes32 root mismatch")
	}
}

func TestIncrementalRootEmpty(t *testing.T) {
	result := IncrementalRoot(nil)
	if result != zeroHash() {
		t.Fatalf("expected zero hash for empty incremental root")
	}
}

func TestIncrementalRootSingle(t *testing.T) {
	enc := NewProgressiveEncoder()
	var c [32]byte
	c[0] = 0x42
	enc.Append(c)
	roots := enc.SubtreeRoots()
	if len(roots) != 1 {
		t.Fatalf("expected 1 subtree root, got %d", len(roots))
	}
	incRoot := IncrementalRoot(roots)
	fullRoot := merkleizeProgressive(enc.chunks, 1)
	if incRoot != fullRoot {
		t.Fatalf("single element incremental root mismatch")
	}
}
