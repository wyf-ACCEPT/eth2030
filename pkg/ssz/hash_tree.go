// hash_tree.go implements SSZ hash tree root computation helpers including
// a precomputed zero hash cache, chunk count calculations for basic and
// composite types, and higher-level Merkleization routines for common
// Ethereum consensus layer patterns.
//
// This file complements merkle.go by providing:
//   - A cached zero-hash table (avoids recomputation on every call)
//   - ChunkCount helpers that follow the SSZ spec for basic/composite types
//   - Union hash tree root support (per SSZ spec)
//   - Helper to compute hash tree root from raw SSZ-encoded containers
//   - Multiproof generation for containers and vectors
package ssz

import (
	"crypto/sha256"
	"encoding/binary"
	"sync"
)

// maxCachedZeroHashDepth is the maximum depth of precomputed zero hashes.
// 64 levels supports trees of up to 2^64 leaves.
const maxCachedZeroHashDepth = 64

// cachedZeroHashes stores precomputed zero hashes at each tree depth.
// cachedZeroHashes[0] = Bytes32() (all zeros)
// cachedZeroHashes[i] = sha256(cachedZeroHashes[i-1] || cachedZeroHashes[i-1])
var (
	cachedZeroHashesOnce sync.Once
	cachedZeroHashTable  [maxCachedZeroHashDepth + 1][32]byte
)

// initZeroHashCache computes the zero hash table once.
func initZeroHashCache() {
	cachedZeroHashesOnce.Do(func() {
		// Level 0 is the zero chunk (already zeroed by Go).
		for i := 1; i <= maxCachedZeroHashDepth; i++ {
			cachedZeroHashTable[i] = hash(cachedZeroHashTable[i-1], cachedZeroHashTable[i-1])
		}
	})
}

// ZeroHash returns the cached zero hash at the given tree depth.
// Depth 0 is a 32-byte zero chunk; depth d is the root of a tree
// of height d containing only zero leaves.
func ZeroHash(depth int) [32]byte {
	initZeroHashCache()
	if depth < 0 || depth > maxCachedZeroHashDepth {
		// Fall back to on-the-fly computation for out-of-range depths.
		h := [32]byte{}
		for i := 0; i < depth; i++ {
			h = hash(h, h)
		}
		return h
	}
	return cachedZeroHashTable[depth]
}

// ConcatHash computes SHA-256(a || b) for two 32-byte inputs.
// Exported so callers can build custom Merkle proofs.
func ConcatHash(a, b [32]byte) [32]byte {
	return hash(a, b)
}

// SHA256 computes SHA-256 over an arbitrary byte slice, returning a [32]byte.
func SHA256(data []byte) [32]byte {
	return sha256.Sum256(data)
}

// --- Chunk count calculation ---

// ChunkCountBasic returns the number of 32-byte chunks needed to pack
// n values of the given elemByteSize. Per the SSZ spec, basic types
// are packed into 32-byte chunks.
func ChunkCountBasic(n, elemByteSize int) int {
	totalBytes := n * elemByteSize
	return (totalBytes + BytesPerChunk - 1) / BytesPerChunk
}

// ChunkCountBitvector returns the number of chunks for a Bitvector[N].
// Each chunk holds 256 bits.
func ChunkCountBitvector(n int) int {
	return (n + 255) / 256
}

// ChunkCountBitlist returns the chunk limit for a Bitlist[N].
// The limit is the number of chunks needed for the max capacity.
func ChunkCountBitlist(maxLen int) int {
	return (maxLen + 255) / 256
}

// ChunkCountByteVector returns the chunks for a ByteVector[N].
func ChunkCountByteVector(n int) int {
	return (n + BytesPerChunk - 1) / BytesPerChunk
}

// ChunkCountByteList returns the chunk limit for a ByteList[N].
func ChunkCountByteList(maxLen int) int {
	return (maxLen + BytesPerChunk - 1) / BytesPerChunk
}

// --- Optimized Merkleization with cached zero hashes ---

// MerkleizeCached computes the Merkle root of chunks using the precomputed
// zero hash cache, avoiding repeated allocation of zero hash arrays.
// If limit is 0, the limit is the next power of two of len(chunks).
func MerkleizeCached(chunks [][32]byte, limit int) [32]byte {
	initZeroHashCache()

	count := len(chunks)
	if limit == 0 {
		limit = nextPowerOfTwo(count)
	}
	if limit < count {
		limit = nextPowerOfTwo(count)
	}
	limit = nextPowerOfTwo(limit)

	if count == 0 {
		// Tree is entirely zero hashes. Return the zero hash at the
		// appropriate depth.
		depth := treeDepth(limit)
		return ZeroHash(depth)
	}

	depth := treeDepth(limit)

	// Build the bottom layer padded to limit.
	layer := make([][32]byte, limit)
	copy(layer, chunks)
	for i := count; i < limit; i++ {
		layer[i] = cachedZeroHashTable[0]
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

// treeDepth returns the depth (number of levels) for a tree with the
// given number of leaves (must be a power of two or 0).
func treeDepth(n int) int {
	if n <= 1 {
		return 0
	}
	d := 0
	for (1 << uint(d)) < n {
		d++
	}
	return d
}

// --- Union hash tree root ---

// HashTreeRootUnion computes the hash tree root of an SSZ union.
// A union is a type with a 1-byte selector and then one of several
// concrete types. Per the SSZ spec:
//
//	hash_tree_root(union) = hash(hash_tree_root(value), selector_chunk)
//
// where selector_chunk is a 32-byte chunk with the selector byte in
// position 0. If selectorByte is 0 and the union is the "None" variant,
// the value root should be the zero hash.
func HashTreeRootUnion(valueRoot [32]byte, selectorByte byte) [32]byte {
	var selectorChunk [32]byte
	selectorChunk[0] = selectorByte
	return hash(valueRoot, selectorChunk)
}

// --- Container field root helpers ---

// HashTreeRootAddress computes the hash tree root of a 20-byte address.
// The address is left-aligned in a 32-byte chunk (zero-padded on the right).
func HashTreeRootAddress(addr [20]byte) [32]byte {
	var chunk [32]byte
	copy(chunk[:20], addr[:])
	return chunk
}

// HashTreeRootBytes48 computes the hash tree root of a 48-byte fixed vector
// (e.g., a BLS public key). Per SSZ, this is Merkleize(pack(value)).
func HashTreeRootBytes48(b [48]byte) [32]byte {
	chunks := Pack(b[:])
	return Merkleize(chunks, 0)
}

// HashTreeRootBytes96 computes the hash tree root of a 96-byte fixed vector
// (e.g., a BLS signature). Per SSZ, this is Merkleize(pack(value)).
func HashTreeRootBytes96(b [96]byte) [32]byte {
	chunks := Pack(b[:])
	return Merkleize(chunks, 0)
}

// --- Multiproof support ---

// GeneralizedIndex returns the generalized index for a given depth and
// position within a binary Merkle tree. The root has generalized index 1.
// At depth d, the leftmost leaf has index 2^d and the rightmost 2^(d+1)-1.
func GeneralizedIndex(depth, pos int) uint64 {
	return (1 << uint(depth)) + uint64(pos)
}

// GenerateMultiproof generates a Merkle multiproof for the specified leaf
// indices within a set of chunks Merkleized to the given limit.
// Returns the auxiliary (sibling) hashes needed to reconstruct the root
// and the helper indices indicating which branches to include.
func GenerateMultiproof(chunks [][32]byte, limit int, indices []int) ([][32]byte, []uint64) {
	initZeroHashCache()

	if limit == 0 {
		limit = nextPowerOfTwo(len(chunks))
	}
	limit = nextPowerOfTwo(limit)
	depth := treeDepth(limit)

	// Build the full tree.
	padded := make([][32]byte, limit)
	copy(padded, chunks)

	layers := make([][][32]byte, depth+1)
	layers[0] = padded
	for d := 0; d < depth; d++ {
		sz := len(layers[d]) / 2
		layers[d+1] = make([][32]byte, sz)
		for i := 0; i < sz; i++ {
			layers[d+1][i] = hash(layers[d][2*i], layers[d][2*i+1])
		}
	}

	// Determine which nodes are needed. Walk from each target leaf
	// up to the root, marking siblings as needed.
	needed := make(map[uint64]bool) // generalized indices of needed proof nodes
	provided := make(map[uint64]bool)
	for _, idx := range indices {
		gidx := GeneralizedIndex(depth, idx)
		provided[gidx] = true
	}

	for _, idx := range indices {
		gidx := GeneralizedIndex(depth, idx)
		for gidx > 1 {
			sibling := gidx ^ 1
			if !provided[sibling] {
				needed[sibling] = true
			}
			gidx /= 2
			provided[gidx] = true
		}
	}

	// Collect proof hashes and helper indices.
	var proofHashes [][32]byte
	var helperIndices []uint64
	for gidx := range needed {
		d := 0
		gi := gidx
		for gi > 1 {
			gi /= 2
			d++
		}
		layerDepth := depth - d
		pos := int(gidx) - (1 << uint(d))
		if layerDepth >= 0 && layerDepth <= depth && pos >= 0 && pos < len(layers[layerDepth]) {
			proofHashes = append(proofHashes, layers[layerDepth][pos])
			helperIndices = append(helperIndices, gidx)
		}
	}

	return proofHashes, helperIndices
}

// --- Subtree root helpers ---

// SubtreeRoot computes the Merkle root of a contiguous range of chunks
// within a larger tree. This is useful for computing partial roots when
// only a subset of the tree is available.
func SubtreeRoot(chunks [][32]byte) [32]byte {
	n := len(chunks)
	if n == 0 {
		return ZeroHash(0)
	}
	limit := nextPowerOfTwo(n)
	return MerkleizeCached(chunks, limit)
}

// MixInSelector mixes a root with a type selector, used for SSZ unions.
// This is functionally identical to HashTreeRootUnion but named to match
// the spec terminology.
func MixInSelector(root [32]byte, selector uint64) [32]byte {
	var selectorChunk [32]byte
	binary.LittleEndian.PutUint64(selectorChunk[:8], selector)
	return hash(root, selectorChunk)
}
