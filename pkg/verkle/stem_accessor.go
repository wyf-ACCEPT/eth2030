// Verkle stem-based key access and code chunk encoding per EIP-6800.
//
// This file provides the stem accessor abstraction for reading and writing
// Ethereum account state through the Verkle tree's stem-based layout:
//
//   - Stem computation from addresses (31-byte prefix shared by all account fields)
//   - Suffix tree structure (256 slots per stem: header + code + storage)
//   - Code chunk encoding (31-byte chunks with leading push-data count)
//   - Account header layout (version, balance, nonce, code_hash, code_size)
//   - Stem-level proof aggregation for execution witnesses
//
// Each Ethereum address maps to a primary stem. The 256 suffix slots within
// that stem are partitioned as:
//
//   Suffix 0:     version
//   Suffix 1:     balance
//   Suffix 2:     nonce
//   Suffix 3:     code_hash
//   Suffix 4:     code_size
//   Suffix 5-63:  reserved
//   Suffix 64-127: small storage slots (indices 0-63)
//   Suffix 128-255: code chunks 0-127
//
// Large storage slots and code chunks beyond 127 use separate stems derived
// from the address and the slot/chunk offset.

package verkle

import (
	"encoding/binary"
	"errors"
	"math/big"

	"github.com/eth2028/eth2028/core/types"
)

// CodeChunkSize is the number of data bytes per Verkle code chunk.
// Each chunk is stored as a 32-byte value: [pushDataCount:1][code:31].
const CodeChunkSize = 31

// AccountHeaderFields is the number of defined header fields per EIP-6800.
const AccountHeaderFields = 5

// StemAccessor provides read/write access to a specific stem's 256 suffix
// slots in the Verkle tree. It caches values for batch operations and
// supports efficient proof aggregation at the stem level.
type StemAccessor struct {
	stem   [StemSize]byte
	tree   *Tree
	values [NodeWidth]*[ValueSize]byte
	loaded bool
}

// NewStemAccessor creates a stem accessor for the given address.
// It derives the stem and optionally pre-loads all values from the tree.
func NewStemAccessor(tree *Tree, addr types.Address) *StemAccessor {
	stem := getAccountStem(addr)
	sa := &StemAccessor{
		stem: stem,
		tree: tree,
	}
	return sa
}

// NewStemAccessorFromStem creates a stem accessor for an explicit stem.
func NewStemAccessorFromStem(tree *Tree, stem [StemSize]byte) *StemAccessor {
	return &StemAccessor{
		stem: stem,
		tree: tree,
	}
}

// Stem returns the 31-byte stem this accessor operates on.
func (sa *StemAccessor) Stem() [StemSize]byte {
	return sa.stem
}

// load fetches all values for this stem from the tree (lazy).
func (sa *StemAccessor) load() {
	if sa.loaded {
		return
	}
	for suffix := 0; suffix < NodeWidth; suffix++ {
		var key [KeySize]byte
		copy(key[:StemSize], sa.stem[:])
		key[StemSize] = byte(suffix)
		val, _ := sa.tree.Get(key)
		if val != nil {
			sa.values[suffix] = val
		}
	}
	sa.loaded = true
}

// Get returns the value at the given suffix, or nil if not set.
func (sa *StemAccessor) Get(suffix byte) *[ValueSize]byte {
	sa.load()
	return sa.values[suffix]
}

// Set writes a value at the given suffix.
func (sa *StemAccessor) Set(suffix byte, value [ValueSize]byte) error {
	var key [KeySize]byte
	copy(key[:StemSize], sa.stem[:])
	key[StemSize] = suffix
	return sa.tree.Put(key, value)
}

// --- Account header read/write ---

// AccountHeader holds the decoded account header fields from a Verkle stem.
type AccountHeader struct {
	Version  uint64
	Balance  *big.Int
	Nonce    uint64
	CodeHash [32]byte
	CodeSize uint64
}

// ReadAccountHeader reads the 5 account header fields from the stem.
func (sa *StemAccessor) ReadAccountHeader() *AccountHeader {
	header := &AccountHeader{
		Balance: new(big.Int),
	}

	if v := sa.Get(VersionLeafKey); v != nil {
		header.Version = binary.BigEndian.Uint64(v[24:32])
	}

	if v := sa.Get(BalanceLeafKey); v != nil {
		header.Balance.SetBytes(v[:])
	}

	if v := sa.Get(NonceLeafKey); v != nil {
		header.Nonce = binary.BigEndian.Uint64(v[24:32])
	}

	if v := sa.Get(CodeHashLeafKey); v != nil {
		copy(header.CodeHash[:], v[:])
	}

	if v := sa.Get(CodeSizeLeafKey); v != nil {
		header.CodeSize = binary.BigEndian.Uint64(v[24:32])
	}

	return header
}

// WriteAccountHeader writes the account header fields to the stem.
func (sa *StemAccessor) WriteAccountHeader(header *AccountHeader) error {
	// Version.
	var versionVal [ValueSize]byte
	binary.BigEndian.PutUint64(versionVal[24:], header.Version)
	if err := sa.Set(VersionLeafKey, versionVal); err != nil {
		return err
	}

	// Balance (32-byte big-endian, zero-padded).
	var balVal [ValueSize]byte
	if header.Balance != nil {
		b := header.Balance.Bytes()
		if len(b) > ValueSize {
			return errors.New("verkle/stem: balance too large")
		}
		copy(balVal[ValueSize-len(b):], b)
	}
	if err := sa.Set(BalanceLeafKey, balVal); err != nil {
		return err
	}

	// Nonce.
	var nonceVal [ValueSize]byte
	binary.BigEndian.PutUint64(nonceVal[24:], header.Nonce)
	if err := sa.Set(NonceLeafKey, nonceVal); err != nil {
		return err
	}

	// Code hash.
	var chVal [ValueSize]byte
	copy(chVal[:], header.CodeHash[:])
	if err := sa.Set(CodeHashLeafKey, chVal); err != nil {
		return err
	}

	// Code size.
	var csVal [ValueSize]byte
	binary.BigEndian.PutUint64(csVal[24:], header.CodeSize)
	return sa.Set(CodeSizeLeafKey, csVal)
}

// --- Code chunk encoding ---

// ChunkifyCode splits contract bytecode into Verkle code chunks per EIP-6800.
// Each chunk is 32 bytes: [pushDataCount:1][codeBytes:31].
//
// The pushDataCount byte indicates how many of the leading bytes in this
// chunk are continuation data from a PUSH instruction that started in a
// previous chunk. This allows the EVM to correctly identify where
// executable code begins within each chunk.
//
// PUSH1-PUSH32 opcodes (0x60-0x7F) consume 1-32 bytes of immediate data.
func ChunkifyCode(code []byte) [][ValueSize]byte {
	if len(code) == 0 {
		return nil
	}

	numChunks := (len(code) + CodeChunkSize - 1) / CodeChunkSize
	chunks := make([][ValueSize]byte, numChunks)

	// Track how many push-data bytes spill into the next chunk.
	pushDataRemaining := 0

	for chunkIdx := 0; chunkIdx < numChunks; chunkIdx++ {
		start := chunkIdx * CodeChunkSize
		end := start + CodeChunkSize
		if end > len(code) {
			end = len(code)
		}
		codeSlice := code[start:end]

		// pushDataCount: how many leading bytes are continuation data.
		pdc := pushDataRemaining
		if pdc > len(codeSlice) {
			pdc = len(codeSlice)
		}
		chunks[chunkIdx][0] = byte(pdc)

		// Copy the code bytes into positions 1..31.
		copy(chunks[chunkIdx][1:], codeSlice)

		// Scan this chunk's code to update pushDataRemaining for the next chunk.
		pushDataRemaining -= pdc
		if pushDataRemaining < 0 {
			pushDataRemaining = 0
		}

		for i := pdc; i < len(codeSlice); i++ {
			op := codeSlice[i]
			if op >= 0x60 && op <= 0x7F {
				// PUSH1..PUSH32: the opcode consumes (op - 0x5F) bytes of data.
				pushLen := int(op) - 0x5F
				// Data bytes within this chunk.
				dataInChunk := len(codeSlice) - i - 1
				if pushLen <= dataInChunk {
					i += pushLen // skip past the push data
				} else {
					// Push data spills into next chunk(s).
					pushDataRemaining = pushLen - dataInChunk
					break
				}
			}
		}
	}

	return chunks
}

// WriteCode writes contract bytecode to the Verkle tree under the given
// address stem. Code chunks 0-127 go to suffixes 128-255 of the primary
// stem. Chunks beyond 127 go to separate stems.
func (sa *StemAccessor) WriteCode(addr types.Address, code []byte) error {
	chunks := ChunkifyCode(code)
	for i, chunk := range chunks {
		if i < MaxCodeChunksPerStem {
			// In the primary stem at suffix CodeOffset+i.
			if err := sa.Set(CodeOffset+byte(i), chunk); err != nil {
				return err
			}
		} else {
			// Overflow to a separate stem.
			key := GetTreeKeyForCodeChunk(addr, uint64(i))
			if err := sa.tree.Put(key, chunk); err != nil {
				return err
			}
		}
	}
	return nil
}

// ReadCodeChunk reads a single code chunk from the stem.
func (sa *StemAccessor) ReadCodeChunk(chunkIndex int) *[ValueSize]byte {
	if chunkIndex < MaxCodeChunksPerStem {
		return sa.Get(CodeOffset + byte(chunkIndex))
	}
	return nil // caller must use the overflow stem
}

// --- Small storage slots ---

// ReadStorageSlot reads a small storage slot (index 0-63) from the header stem.
func (sa *StemAccessor) ReadStorageSlot(slotIndex byte) *[ValueSize]byte {
	if slotIndex >= 64 {
		return nil
	}
	return sa.Get(HeaderStorageOffset + slotIndex)
}

// WriteStorageSlot writes a small storage slot (index 0-63) to the header stem.
func (sa *StemAccessor) WriteStorageSlot(slotIndex byte, value [ValueSize]byte) error {
	if slotIndex >= 64 {
		return errors.New("verkle/stem: slot index >= 64 (use tree directly)")
	}
	return sa.Set(HeaderStorageOffset+slotIndex, value)
}

// --- Stem-level proof aggregation ---

// StemProofData collects the proof elements for a single stem.
// When building an execution witness, multiple keys under the same stem
// can share the same path commitments, reducing total proof size.
type StemProofData struct {
	// Stem is the 31-byte stem.
	Stem [StemSize]byte

	// SuffixStateDiffs contains the suffixes that were accessed and their
	// old/new values.
	SuffixStateDiffs []SuffixStateDiff

	// PathCommitments are the internal node commitments along the path
	// from root to this stem's leaf node.
	PathCommitments []Commitment

	// LeafCommitment is the leaf node commitment for this stem.
	LeafCommitment Commitment
}

// SuffixStateDiff records the state change for a single suffix within a stem.
type SuffixStateDiff struct {
	Suffix   byte
	OldValue *[ValueSize]byte
	NewValue *[ValueSize]byte
}

// AggregateStemProofs groups a set of keys by their stem and collects the
// proof data for each stem from the tree. This is the first step in building
// an EIP-6800 execution witness.
func AggregateStemProofs(tree *Tree, keys [][KeySize]byte) []StemProofData {
	// Group keys by stem.
	stemKeys := make(map[[StemSize]byte][]byte) // stem -> list of suffixes
	stemOrder := make([][StemSize]byte, 0)

	for _, key := range keys {
		stem := StemFromKey(key)
		suffix := SuffixFromKey(key)

		if _, exists := stemKeys[stem]; !exists {
			stemOrder = append(stemOrder, stem)
		}
		stemKeys[stem] = append(stemKeys[stem], suffix)
	}

	// Build proof data for each stem.
	proofs := make([]StemProofData, 0, len(stemOrder))
	for _, stem := range stemOrder {
		suffixes := stemKeys[stem]

		// Collect the state diffs for each suffix.
		diffs := make([]SuffixStateDiff, 0, len(suffixes))
		for _, suffix := range suffixes {
			var key [KeySize]byte
			copy(key[:StemSize], stem[:])
			key[StemSize] = suffix

			val, _ := tree.Get(key)
			diffs = append(diffs, SuffixStateDiff{
				Suffix:   suffix,
				OldValue: val,
			})
		}

		// Get path commitments by walking the tree.
		pathCommits := collectPathCommitments(tree, stem)

		// Get the leaf commitment.
		leaf := tree.getLeaf(stem)
		var leafCommit Commitment
		if leaf != nil {
			leafCommit = leaf.Commit()
		}

		proofs = append(proofs, StemProofData{
			Stem:             stem,
			SuffixStateDiffs: diffs,
			PathCommitments:  pathCommits,
			LeafCommitment:   leafCommit,
		})
	}
	return proofs
}

// collectPathCommitments walks the tree from root to the leaf at the given
// stem, collecting internal node commitments along the path.
func collectPathCommitments(tree *Tree, stem [StemSize]byte) []Commitment {
	var commits []Commitment
	node := tree.root
	commits = append(commits, node.Commit())

	for depth := 0; depth < StemSize; depth++ {
		child := node.Child(stem[depth])
		if child == nil {
			break
		}
		commits = append(commits, child.Commit())

		internal, ok := child.(*InternalNode)
		if !ok {
			break
		}
		node = internal
	}
	return commits
}

// StemProofSize returns the total number of suffix diffs across all stem proofs.
func StemProofSize(proofs []StemProofData) int {
	total := 0
	for _, p := range proofs {
		total += len(p.SuffixStateDiffs)
	}
	return total
}
