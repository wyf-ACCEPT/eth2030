// bitfield.go implements SSZ bitfield types: Bitlist and Bitvector.
//
// A Bitlist is a variable-length sequence of bits with a trailing length bit
// (sentinel) in the serialized form. It is used in the consensus layer for
// aggregation bitfields in attestations (e.g., which validators participated).
//
// A Bitvector is a fixed-length sequence of bits. It is used for fixed-size
// bitfields like sync committee participation.
//
// Both types support standard bitwise operations, population counting, and
// SSZ Merkleization (hash tree root computation).
//
// Spec: https://github.com/ethereum/consensus-specs/blob/dev/ssz/simple-serialize.md
package ssz

import (
	"errors"
)

// Bitfield errors.
var (
	ErrBitlistZeroLength    = errors.New("bitfield: bitlist length must be positive")
	ErrBitlistIndexOOB      = errors.New("bitfield: bit index out of bounds")
	ErrBitlistLengthMismatch = errors.New("bitfield: bitlist length mismatch for OR")
	ErrBitvectorZeroLength  = errors.New("bitfield: bitvector length must be positive")
	ErrBitvectorIndexOOB    = errors.New("bitfield: bitvector index out of bounds")
	ErrBitvectorLengthMismatch = errors.New("bitfield: bitvector length mismatch")
)

// Bitlist is a variable-length bit array. The underlying byte slice includes
// a trailing sentinel bit to encode the length. The usable bit capacity is
// determined by the position of the highest set bit in the serialized form.
type Bitlist struct {
	data   []byte
	length int // number of usable bits (excludes sentinel)
}

// NewBitlist creates a new Bitlist with the given number of usable bits.
// All bits are initially unset. The serialized form includes a sentinel bit.
func NewBitlist(length int) (Bitlist, error) {
	if length <= 0 {
		return Bitlist{}, ErrBitlistZeroLength
	}
	// Serialized size: (length+1) bits = ceil((length+1)/8) bytes.
	totalBits := length + 1
	numBytes := (totalBits + 7) / 8
	data := make([]byte, numBytes)
	// Set the sentinel bit at position 'length'.
	data[length/8] |= 1 << (uint(length) % 8)
	return Bitlist{data: data, length: length}, nil
}

// BitlistFromBytes creates a Bitlist from raw serialized bytes (with sentinel).
// Returns error if no sentinel bit is found.
func BitlistFromBytes(data []byte) (Bitlist, error) {
	if len(data) == 0 {
		return Bitlist{}, ErrBitlistZeroLength
	}
	// Find the sentinel bit: highest set bit in the last byte.
	lastByte := data[len(data)-1]
	if lastByte == 0 {
		return Bitlist{}, errors.New("bitfield: no sentinel bit found")
	}
	// Find the position of the highest set bit in the last byte.
	sentinelBitInByte := 0
	for b := lastByte; b > 1; b >>= 1 {
		sentinelBitInByte++
	}
	sentinelPos := (len(data)-1)*8 + sentinelBitInByte
	length := sentinelPos

	cp := make([]byte, len(data))
	copy(cp, data)
	return Bitlist{data: cp, length: length}, nil
}

// Set sets the bit at the given index. Panics if out of bounds.
func (b Bitlist) Set(index int) {
	if index < 0 || index >= b.length {
		return // silently ignore out-of-bounds
	}
	b.data[index/8] |= 1 << (uint(index) % 8)
}

// Clear unsets the bit at the given index.
func (b Bitlist) Clear(index int) {
	if index < 0 || index >= b.length {
		return
	}
	b.data[index/8] &^= 1 << (uint(index) % 8)
}

// Get returns true if the bit at the given index is set.
func (b Bitlist) Get(index int) bool {
	if index < 0 || index >= b.length {
		return false
	}
	return b.data[index/8]&(1<<(uint(index)%8)) != 0
}

// Len returns the number of usable bits (excludes sentinel).
func (b Bitlist) Len() int {
	return b.length
}

// Count returns the number of set bits (population count), excluding sentinel.
func (b Bitlist) Count() int {
	count := 0
	for i := 0; i < b.length; i++ {
		if b.Get(i) {
			count++
		}
	}
	return count
}

// Bytes returns a copy of the underlying serialized bytes (with sentinel).
func (b Bitlist) Bytes() []byte {
	cp := make([]byte, len(b.data))
	copy(cp, b.data)
	return cp
}

// OR performs bitwise OR of two bitlists. Both must have the same length.
func (b Bitlist) OR(other Bitlist) (Bitlist, error) {
	if b.length != other.length {
		return Bitlist{}, ErrBitlistLengthMismatch
	}
	result, _ := NewBitlist(b.length)
	for i := 0; i < len(b.data); i++ {
		result.data[i] = b.data[i] | other.data[i]
	}
	// Ensure sentinel is set.
	result.data[b.length/8] |= 1 << (uint(b.length) % 8)
	return result, nil
}

// AND performs bitwise AND of two bitlists. Both must have the same length.
func (b Bitlist) AND(other Bitlist) (Bitlist, error) {
	if b.length != other.length {
		return Bitlist{}, ErrBitlistLengthMismatch
	}
	result, _ := NewBitlist(b.length)
	for i := 0; i < len(b.data); i++ {
		result.data[i] = b.data[i] & other.data[i]
	}
	// Re-set sentinel (AND might have cleared it).
	result.data[b.length/8] |= 1 << (uint(b.length) % 8)
	return result, nil
}

// Overlaps returns true if any bit is set in both bitlists.
func (b Bitlist) Overlaps(other Bitlist) bool {
	if b.length != other.length {
		return false
	}
	for i := 0; i < b.length; i++ {
		if b.Get(i) && other.Get(i) {
			return true
		}
	}
	return false
}

// IsZero returns true if no bits are set (excluding sentinel).
func (b Bitlist) IsZero() bool {
	return b.Count() == 0
}

// BitlistHashTreeRoot computes the SSZ hash tree root of a bitlist.
// The bitfield is packed (without sentinel) into chunks, Merkleized with
// a limit derived from maxLength, and mixed in with the actual bit count.
func BitlistHashTreeRoot(b Bitlist, maxLength int) [32]byte {
	// Pack the bits (without sentinel) into bytes.
	packed := packBitsWithoutSentinel(b)
	chunks := Pack(packed)
	maxChunks := ChunkCount(maxLength)
	root := Merkleize(chunks, nextPowerOfTwo(maxChunks))
	return MixInLength(root, uint64(b.length))
}

// packBitsWithoutSentinel extracts the data bits (excluding sentinel) as bytes.
func packBitsWithoutSentinel(b Bitlist) []byte {
	numBytes := (b.length + 7) / 8
	if numBytes == 0 {
		return nil
	}
	result := make([]byte, numBytes)
	for i := 0; i < b.length; i++ {
		if b.Get(i) {
			result[i/8] |= 1 << (uint(i) % 8)
		}
	}
	return result
}

// --- Bitvector ---

// Bitvector is a fixed-length bit array. Unlike Bitlist, it has no sentinel
// bit. The length is always known at compile/construction time.
type Bitvector struct {
	data   []byte
	length int
}

// NewBitvector creates a new Bitvector with the given length. All bits start unset.
func NewBitvector(length int) (Bitvector, error) {
	if length <= 0 {
		return Bitvector{}, ErrBitvectorZeroLength
	}
	numBytes := (length + 7) / 8
	return Bitvector{
		data:   make([]byte, numBytes),
		length: length,
	}, nil
}

// BitvectorFromBytes creates a Bitvector from raw bytes with the given bit length.
func BitvectorFromBytes(data []byte, length int) (Bitvector, error) {
	if length <= 0 {
		return Bitvector{}, ErrBitvectorZeroLength
	}
	expectedBytes := (length + 7) / 8
	if len(data) < expectedBytes {
		return Bitvector{}, ErrBitvectorLengthMismatch
	}
	cp := make([]byte, expectedBytes)
	copy(cp, data[:expectedBytes])
	return Bitvector{data: cp, length: length}, nil
}

// Set sets the bit at the given index.
func (bv Bitvector) Set(index int) {
	if index < 0 || index >= bv.length {
		return
	}
	bv.data[index/8] |= 1 << (uint(index) % 8)
}

// Clear unsets the bit at the given index.
func (bv Bitvector) Clear(index int) {
	if index < 0 || index >= bv.length {
		return
	}
	bv.data[index/8] &^= 1 << (uint(index) % 8)
}

// Get returns true if the bit at the given index is set.
func (bv Bitvector) Get(index int) bool {
	if index < 0 || index >= bv.length {
		return false
	}
	return bv.data[index/8]&(1<<(uint(index)%8)) != 0
}

// Len returns the fixed bit length of the bitvector.
func (bv Bitvector) Len() int {
	return bv.length
}

// Count returns the number of set bits (population count).
func (bv Bitvector) Count() int {
	count := 0
	for i := 0; i < bv.length; i++ {
		if bv.Get(i) {
			count++
		}
	}
	return count
}

// Bytes returns a copy of the underlying byte data.
func (bv Bitvector) Bytes() []byte {
	cp := make([]byte, len(bv.data))
	copy(cp, bv.data)
	return cp
}

// OR performs bitwise OR of two bitvectors. Both must have the same length.
func (bv Bitvector) OR(other Bitvector) (Bitvector, error) {
	if bv.length != other.length {
		return Bitvector{}, ErrBitvectorLengthMismatch
	}
	result, _ := NewBitvector(bv.length)
	for i := 0; i < len(bv.data); i++ {
		result.data[i] = bv.data[i] | other.data[i]
	}
	return result, nil
}

// AND performs bitwise AND of two bitvectors.
func (bv Bitvector) AND(other Bitvector) (Bitvector, error) {
	if bv.length != other.length {
		return Bitvector{}, ErrBitvectorLengthMismatch
	}
	result, _ := NewBitvector(bv.length)
	for i := 0; i < len(bv.data); i++ {
		result.data[i] = bv.data[i] & other.data[i]
	}
	return result, nil
}

// Overlaps returns true if any bit is set in both bitvectors.
func (bv Bitvector) Overlaps(other Bitvector) bool {
	if bv.length != other.length {
		return false
	}
	for i := 0; i < bv.length; i++ {
		if bv.Get(i) && other.Get(i) {
			return true
		}
	}
	return false
}

// IsZero returns true if no bits are set.
func (bv Bitvector) IsZero() bool {
	return bv.Count() == 0
}

// BitvectorHashTreeRoot computes the SSZ hash tree root of a bitvector.
// The bits are packed into bytes, then packed into 32-byte chunks and Merkleized.
func BitvectorHashTreeRoot(bv Bitvector) [32]byte {
	chunks := Pack(bv.data)
	return Merkleize(chunks, 0)
}

// ChunkCount returns the number of 32-byte chunks needed for a bitfield
// of the given bit length. Each chunk holds 256 bits.
func ChunkCount(bitLength int) int {
	if bitLength <= 0 {
		return 1
	}
	return (bitLength + 255) / 256
}

// --- Bitlist/Bitvector equality ---

// BitlistEqual returns true if two bitlists have the same length and bits.
func BitlistEqual(a, b Bitlist) bool {
	if a.length != b.length {
		return false
	}
	for i := 0; i < a.length; i++ {
		if a.Get(i) != b.Get(i) {
			return false
		}
	}
	return true
}

// BitvectorEqual returns true if two bitvectors have the same length and bits.
func BitvectorEqual(a, b Bitvector) bool {
	if a.length != b.length {
		return false
	}
	for i := 0; i < a.length; i++ {
		if a.Get(i) != b.Get(i) {
			return false
		}
	}
	return true
}

// --- Bitlist serialization helpers ---

// BitlistMarshalSSZ serializes a bitlist with its sentinel bit.
// This is equivalent to the existing MarshalBitlist but operates on the
// Bitlist type directly.
func BitlistMarshalSSZ(b Bitlist) []byte {
	return b.Bytes()
}

// BitlistUnmarshalSSZ deserializes a bitlist from SSZ bytes.
func BitlistUnmarshalSSZ(data []byte) (Bitlist, error) {
	return BitlistFromBytes(data)
}

// BitvectorMarshalSSZ serializes a bitvector as packed bytes.
func BitvectorMarshalSSZ(bv Bitvector) []byte {
	return bv.Bytes()
}

// BitvectorUnmarshalSSZ deserializes a bitvector from SSZ bytes.
func BitvectorUnmarshalSSZ(data []byte, length int) (Bitvector, error) {
	return BitvectorFromBytes(data, length)
}
