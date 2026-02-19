package ssz

import (
	"crypto/sha256"
	"encoding/binary"
)

// BytesPerChunk is the number of bytes in each leaf chunk for Merkleization.
const BytesPerChunk = 32

// hash combines two 32-byte inputs using SHA-256.
func hash(a, b [32]byte) [32]byte {
	var combined [64]byte
	copy(combined[:32], a[:])
	copy(combined[32:], b[:])
	return sha256.Sum256(combined[:])
}

// zeroHash returns a zero-filled 32-byte array.
func zeroHash() [32]byte {
	return [32]byte{}
}

// zeroHashes returns a cache of zero hashes for each level of a Merkle tree.
// zeroHashes[0] = zero chunk, zeroHashes[i] = hash(zeroHashes[i-1], zeroHashes[i-1]).
func zeroHashes(depth int) [][32]byte {
	hashes := make([][32]byte, depth+1)
	for i := 1; i <= depth; i++ {
		hashes[i] = hash(hashes[i-1], hashes[i-1])
	}
	return hashes
}

// nextPowerOfTwo returns the smallest power of 2 >= n.
func nextPowerOfTwo(n int) int {
	if n <= 1 {
		return 1
	}
	p := 1
	for p < n {
		p <<= 1
	}
	return p
}

// Pack packs a sequence of SSZ serialized values into 32-byte chunks,
// right-padding the last chunk with zeros if needed.
func Pack(serialized []byte) [][32]byte {
	if len(serialized) == 0 {
		return [][32]byte{zeroHash()}
	}
	numChunks := (len(serialized) + BytesPerChunk - 1) / BytesPerChunk
	chunks := make([][32]byte, numChunks)
	for i := 0; i < numChunks; i++ {
		start := i * BytesPerChunk
		end := start + BytesPerChunk
		if end > len(serialized) {
			end = len(serialized)
		}
		copy(chunks[i][:], serialized[start:end])
	}
	return chunks
}

// Merkleize computes the Merkle root of a list of chunks padded to the given
// limit. If limit is 0, it uses the next power of two of the chunk count.
func Merkleize(chunks [][32]byte, limit int) [32]byte {
	count := len(chunks)
	if limit == 0 {
		limit = nextPowerOfTwo(count)
	}
	if limit < count {
		limit = nextPowerOfTwo(count)
	}

	// Ensure limit is a power of two.
	limit = nextPowerOfTwo(limit)

	if count == 0 {
		chunks = [][32]byte{zeroHash()}
		count = 1
	}

	// Compute the tree depth.
	depth := 0
	for (1 << uint(depth)) < limit {
		depth++
	}

	zeros := zeroHashes(depth)

	// Build the tree layer by layer.
	layer := make([][32]byte, limit)
	copy(layer, chunks)
	for i := count; i < limit; i++ {
		layer[i] = zeros[0]
	}

	for d := 0; d < depth; d++ {
		newSize := len(layer) / 2
		newLayer := make([][32]byte, newSize)
		for i := 0; i < newSize; i++ {
			newLayer[i] = hash(layer[2*i], layer[2*i+1])
		}
		layer = newLayer
	}

	return layer[0]
}

// MixInLength mixes a Merkle root with a length value, used for
// variable-size types (lists, bitlists, byte lists).
func MixInLength(root [32]byte, length uint64) [32]byte {
	var lengthChunk [32]byte
	binary.LittleEndian.PutUint64(lengthChunk[:8], length)
	return hash(root, lengthChunk)
}

// --- Hash tree root functions for basic types ---

// HashTreeRootBool computes the hash tree root of a boolean.
func HashTreeRootBool(v bool) [32]byte {
	var chunk [32]byte
	if v {
		chunk[0] = 1
	}
	return chunk
}

// HashTreeRootUint8 computes the hash tree root of a uint8.
func HashTreeRootUint8(v uint8) [32]byte {
	var chunk [32]byte
	chunk[0] = v
	return chunk
}

// HashTreeRootUint16 computes the hash tree root of a uint16.
func HashTreeRootUint16(v uint16) [32]byte {
	var chunk [32]byte
	binary.LittleEndian.PutUint16(chunk[:2], v)
	return chunk
}

// HashTreeRootUint32 computes the hash tree root of a uint32.
func HashTreeRootUint32(v uint32) [32]byte {
	var chunk [32]byte
	binary.LittleEndian.PutUint32(chunk[:4], v)
	return chunk
}

// HashTreeRootUint64 computes the hash tree root of a uint64.
func HashTreeRootUint64(v uint64) [32]byte {
	var chunk [32]byte
	binary.LittleEndian.PutUint64(chunk[:8], v)
	return chunk
}

// HashTreeRootBytes32 computes the hash tree root of a 32-byte fixed vector.
// Since it already fits in one chunk, it is its own root.
func HashTreeRootBytes32(b [32]byte) [32]byte {
	return b
}

// --- Hash tree root functions for composite types ---

// HashTreeRootVector computes the hash tree root of a vector of elements.
// Each element is provided as its 32-byte hash tree root.
func HashTreeRootVector(elementRoots [][32]byte) [32]byte {
	return Merkleize(elementRoots, 0)
}

// HashTreeRootList computes the hash tree root of a list with the given
// max length. Each element is provided as its 32-byte hash tree root.
func HashTreeRootList(elementRoots [][32]byte, maxLen int) [32]byte {
	root := Merkleize(elementRoots, nextPowerOfTwo(maxLen))
	return MixInLength(root, uint64(len(elementRoots)))
}

// HashTreeRootContainer computes the hash tree root of a container.
// Each field is provided as its 32-byte hash tree root.
func HashTreeRootContainer(fieldRoots [][32]byte) [32]byte {
	return Merkleize(fieldRoots, 0)
}

// HashTreeRootByteList computes the hash tree root of a ByteList[N].
func HashTreeRootByteList(data []byte, maxLen int) [32]byte {
	chunks := Pack(data)
	maxChunks := (maxLen + BytesPerChunk - 1) / BytesPerChunk
	root := Merkleize(chunks, nextPowerOfTwo(maxChunks))
	return MixInLength(root, uint64(len(data)))
}

// HashTreeRootBitvector computes the hash tree root of a Bitvector[N].
func HashTreeRootBitvector(bits []bool) [32]byte {
	packed := MarshalBitvector(bits)
	chunks := Pack(packed)
	return Merkleize(chunks, 0)
}

// HashTreeRootBitlist computes the hash tree root of a Bitlist[N].
func HashTreeRootBitlist(bits []bool, maxLen int) [32]byte {
	packed := MarshalBitvector(bits) // pack without sentinel for hashing
	chunks := Pack(packed)
	maxChunks := (maxLen + 255) / 256 // each chunk holds 256 bits
	root := Merkleize(chunks, nextPowerOfTwo(maxChunks))
	return MixInLength(root, uint64(len(bits)))
}

// HashTreeRootBasicVector computes the hash tree root of a vector of basic
// type values. The serialized data is packed into chunks and Merkleized.
func HashTreeRootBasicVector(serialized []byte) [32]byte {
	chunks := Pack(serialized)
	return Merkleize(chunks, 0)
}

// HashTreeRootBasicList computes the hash tree root of a list of basic type
// values. The serialized data is packed into chunks, Merkleized with the
// limit, and mixed in with the length.
func HashTreeRootBasicList(serialized []byte, count int, elemSize int, maxLen int) [32]byte {
	chunks := Pack(serialized)
	maxChunks := (maxLen*elemSize + BytesPerChunk - 1) / BytesPerChunk
	root := Merkleize(chunks, nextPowerOfTwo(maxChunks))
	return MixInLength(root, uint64(count))
}
