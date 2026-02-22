// proof_generator.go provides Merkle proof generation from MPT tries including
// inclusion proofs, exclusion proofs, multi-proofs, and proof serialization.
// It works with both in-memory tries and database-backed resolvable tries.
package trie

import (
	"bytes"
	"encoding/binary"
	"errors"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

// Proof generation errors.
var (
	ErrProofGenNilKey      = errors.New("proof_generator: nil key")
	ErrProofGenEmptyTrie   = errors.New("proof_generator: empty trie")
	ErrProofGenSerialize   = errors.New("proof_generator: serialization error")
	ErrProofGenDeserialize = errors.New("proof_generator: deserialization error")
)

// ProofGenerator generates Merkle proofs from a trie. It supports inclusion
// proofs, exclusion proofs, batch (multi) proofs, and serialization.
type ProofGenerator struct {
	trie    *Trie
	resTrie *ResolvableTrie
	nodeDB  *NodeDatabase
}

// NewProofGenerator creates a proof generator for an in-memory trie.
func NewProofGenerator(t *Trie) *ProofGenerator {
	return &ProofGenerator{trie: t}
}

// NewResolvableProofGenerator creates a proof generator for a database-backed trie.
func NewResolvableProofGenerator(t *ResolvableTrie) *ProofGenerator {
	return &ProofGenerator{resTrie: t, trie: &t.Trie, nodeDB: t.db}
}

// GenerateProof produces a Merkle inclusion proof for the given key. It returns
// the list of RLP-encoded trie nodes along the path from the root to the value.
func (pg *ProofGenerator) GenerateProof(key []byte) (*InclusionProof, error) {
	if key == nil {
		return nil, ErrProofGenNilKey
	}
	if pg.trie.root == nil {
		return nil, ErrProofGenEmptyTrie
	}

	// Hash the trie first to populate cached hashes.
	rootHash := pg.rootHash()

	var proofNodes [][]byte
	var err error
	if pg.resTrie != nil {
		proofNodes, err = pg.resTrie.Prove(key)
	} else {
		proofNodes, err = pg.trie.Prove(key)
	}
	if err != nil {
		return nil, err
	}

	// Retrieve the value.
	var value []byte
	if pg.resTrie != nil {
		value, _ = pg.resTrie.Get(key)
	} else {
		value, _ = pg.trie.Get(key)
	}

	return &InclusionProof{
		Key:        copySlice(key),
		Value:      value,
		ProofNodes: proofNodes,
		RootHash:   rootHash,
	}, nil
}

// GenerateExclusionProof produces a Merkle exclusion proof demonstrating that
// the given key does not exist in the trie. The proof contains the nodes along
// the path until the lookup diverges from the trie structure.
func (pg *ProofGenerator) GenerateExclusionProof(key []byte) (*ExclusionProof, error) {
	if key == nil {
		return nil, ErrProofGenNilKey
	}

	rootHash := pg.rootHash()

	// Empty trie: absence is trivially provable.
	if pg.trie.root == nil {
		return &ExclusionProof{
			Key:        copySlice(key),
			ProofNodes: nil,
			RootHash:   rootHash,
		}, nil
	}

	// Verify the key truly does not exist.
	var val []byte
	if pg.resTrie != nil {
		val, _ = pg.resTrie.Get(key)
	} else {
		val, _ = pg.trie.Get(key)
	}
	if val != nil {
		return nil, errors.New("proof_generator: key exists, cannot generate exclusion proof")
	}

	var proofNodes [][]byte
	var err error
	if pg.resTrie != nil {
		proofNodes, err = pg.resTrie.ProveAbsence(key)
	} else {
		proofNodes, err = pg.trie.ProveAbsence(key)
	}
	if err != nil {
		return nil, err
	}

	return &ExclusionProof{
		Key:        copySlice(key),
		ProofNodes: proofNodes,
		RootHash:   rootHash,
	}, nil
}

// GenerateMultiProof generates proofs for multiple keys in a single pass.
// Both existing and non-existing keys are handled: each result entry
// indicates whether the key was found (inclusion) or absent (exclusion).
func (pg *ProofGenerator) GenerateMultiProof(keys [][]byte) (*MultiProof, error) {
	if len(keys) == 0 {
		return nil, errors.New("proof_generator: empty key list")
	}

	rootHash := pg.rootHash()
	mp := &MultiProof{
		RootHash: rootHash,
		Items:    make([]MultiProofEntry, len(keys)),
	}

	for i, key := range keys {
		if key == nil {
			return nil, ErrProofGenNilKey
		}

		entry := MultiProofEntry{Key: copySlice(key)}

		// Check if key exists.
		var val []byte
		if pg.resTrie != nil {
			val, _ = pg.resTrie.Get(key)
		} else {
			val, _ = pg.trie.Get(key)
		}

		if val != nil {
			// Inclusion proof.
			entry.Exists = true
			entry.Value = val
			if pg.resTrie != nil {
				entry.ProofNodes, _ = pg.resTrie.Prove(key)
			} else {
				entry.ProofNodes, _ = pg.trie.Prove(key)
			}
		} else {
			// Exclusion proof.
			entry.Exists = false
			if pg.resTrie != nil {
				entry.ProofNodes, _ = pg.resTrie.ProveAbsence(key)
			} else {
				entry.ProofNodes, _ = pg.trie.ProveAbsence(key)
			}
		}
		mp.Items[i] = entry
	}
	return mp, nil
}

// VerifyInclusionProof verifies an inclusion proof against its root hash.
func VerifyInclusionProof(proof *InclusionProof) error {
	if proof == nil {
		return errors.New("proof_generator: nil proof")
	}
	val, err := VerifyProof(proof.RootHash, proof.Key, proof.ProofNodes)
	if err != nil {
		return err
	}
	if !bytes.Equal(val, proof.Value) {
		return errors.New("proof_generator: value mismatch")
	}
	return nil
}

// VerifyExclusionProof verifies an exclusion proof against its root hash.
func VerifyExclusionProof(proof *ExclusionProof) error {
	if proof == nil {
		return errors.New("proof_generator: nil proof")
	}
	val, err := VerifyProof(proof.RootHash, proof.Key, proof.ProofNodes)
	if err != nil {
		return err
	}
	if val != nil {
		return errors.New("proof_generator: expected absence but key exists")
	}
	return nil
}

// VerifyMultiProofResult verifies all entries in a multi-proof.
func VerifyMultiProofResult(mp *MultiProof) error {
	if mp == nil || len(mp.Items) == 0 {
		return errors.New("proof_generator: nil or empty multi-proof")
	}
	for i, entry := range mp.Items {
		val, err := VerifyProof(mp.RootHash, entry.Key, entry.ProofNodes)
		if err != nil {
			return err
		}
		if entry.Exists {
			if val == nil || !bytes.Equal(val, entry.Value) {
				return errors.New("proof_generator: inclusion proof value mismatch at index " + itoa(i))
			}
		} else {
			if val != nil {
				return errors.New("proof_generator: expected absence at index " + itoa(i))
			}
		}
	}
	return nil
}

// --- Proof types ---

// InclusionProof proves that a key-value pair exists in a trie.
type InclusionProof struct {
	Key        []byte
	Value      []byte
	ProofNodes [][]byte
	RootHash   types.Hash
}

// ExclusionProof proves that a key does not exist in a trie.
type ExclusionProof struct {
	Key        []byte
	Value      []byte // always nil for exclusion proofs (present for API consistency)
	ProofNodes [][]byte
	RootHash   types.Hash
}

// MultiProofEntry is one entry in a multi-proof.
type MultiProofEntry struct {
	Key        []byte
	Value      []byte
	Exists     bool
	ProofNodes [][]byte
}

// MultiProof contains proofs for multiple keys against the same root.
type MultiProof struct {
	RootHash types.Hash
	Items    []MultiProofEntry
}

// --- Serialization ---

// SerializedProof is the wire format for a Merkle proof. Format:
//
//	[32 bytes root hash]
//	[4 bytes key length]
//	[key bytes]
//	[4 bytes value length] (0 for exclusion proofs)
//	[value bytes]
//	[4 bytes node count]
//	For each node:
//	  [4 bytes node data length]
//	  [node data bytes]
type SerializedProof []byte

// SerializeInclusionProof serializes an inclusion proof for wire transmission.
func SerializeInclusionProof(proof *InclusionProof) (SerializedProof, error) {
	if proof == nil {
		return nil, ErrProofGenSerialize
	}
	return serializeProofData(proof.RootHash, proof.Key, proof.Value, proof.ProofNodes)
}

// SerializeExclusionProof serializes an exclusion proof for wire transmission.
func SerializeExclusionProof(proof *ExclusionProof) (SerializedProof, error) {
	if proof == nil {
		return nil, ErrProofGenSerialize
	}
	return serializeProofData(proof.RootHash, proof.Key, nil, proof.ProofNodes)
}

// DeserializeProof deserializes proof bytes into key, value, proof nodes, and root.
// If value is nil, the proof is an exclusion proof.
func DeserializeProof(data SerializedProof) (rootHash types.Hash, key, value []byte, proofNodes [][]byte, err error) {
	if len(data) < 32+4 {
		return types.Hash{}, nil, nil, nil, ErrProofGenDeserialize
	}
	pos := 0

	// Root hash (32 bytes).
	copy(rootHash[:], data[pos:pos+32])
	pos += 32

	// Key length + key.
	if pos+4 > len(data) {
		return types.Hash{}, nil, nil, nil, ErrProofGenDeserialize
	}
	keyLen := int(binary.BigEndian.Uint32(data[pos : pos+4]))
	pos += 4
	if pos+keyLen > len(data) {
		return types.Hash{}, nil, nil, nil, ErrProofGenDeserialize
	}
	key = make([]byte, keyLen)
	copy(key, data[pos:pos+keyLen])
	pos += keyLen

	// Value length + value.
	if pos+4 > len(data) {
		return types.Hash{}, nil, nil, nil, ErrProofGenDeserialize
	}
	valLen := int(binary.BigEndian.Uint32(data[pos : pos+4]))
	pos += 4
	if valLen > 0 {
		if pos+valLen > len(data) {
			return types.Hash{}, nil, nil, nil, ErrProofGenDeserialize
		}
		value = make([]byte, valLen)
		copy(value, data[pos:pos+valLen])
		pos += valLen
	}

	// Node count + nodes.
	if pos+4 > len(data) {
		return types.Hash{}, nil, nil, nil, ErrProofGenDeserialize
	}
	nodeCount := int(binary.BigEndian.Uint32(data[pos : pos+4]))
	pos += 4
	proofNodes = make([][]byte, nodeCount)
	for i := 0; i < nodeCount; i++ {
		if pos+4 > len(data) {
			return types.Hash{}, nil, nil, nil, ErrProofGenDeserialize
		}
		nLen := int(binary.BigEndian.Uint32(data[pos : pos+4]))
		pos += 4
		if pos+nLen > len(data) {
			return types.Hash{}, nil, nil, nil, ErrProofGenDeserialize
		}
		proofNodes[i] = make([]byte, nLen)
		copy(proofNodes[i], data[pos:pos+nLen])
		pos += nLen
	}
	return rootHash, key, value, proofNodes, nil
}

func serializeProofData(root types.Hash, key, value []byte, nodes [][]byte) (SerializedProof, error) {
	// Calculate total size.
	size := 32 + 4 + len(key) + 4 + len(value) + 4
	for _, n := range nodes {
		size += 4 + len(n)
	}

	buf := make([]byte, size)
	pos := 0

	// Root hash.
	copy(buf[pos:], root[:])
	pos += 32

	// Key.
	binary.BigEndian.PutUint32(buf[pos:], uint32(len(key)))
	pos += 4
	copy(buf[pos:], key)
	pos += len(key)

	// Value.
	binary.BigEndian.PutUint32(buf[pos:], uint32(len(value)))
	pos += 4
	copy(buf[pos:], value)
	pos += len(value)

	// Nodes.
	binary.BigEndian.PutUint32(buf[pos:], uint32(len(nodes)))
	pos += 4
	for _, n := range nodes {
		binary.BigEndian.PutUint32(buf[pos:], uint32(len(n)))
		pos += 4
		copy(buf[pos:], n)
		pos += len(n)
	}
	return buf, nil
}

// ProofSize returns the total byte size of proof nodes (for metrics).
func ProofSize(nodes [][]byte) int {
	total := 0
	for _, n := range nodes {
		total += len(n)
	}
	return total
}

// VerifyProofAgainstRoot is a convenience wrapper that verifies proof nodes
// against a root hash and returns the value at the key (nil for absence).
func VerifyProofAgainstRoot(root types.Hash, key []byte, proofNodes [][]byte) ([]byte, error) {
	return VerifyProof(root, key, proofNodes)
}

// rootHash returns the root hash, ensuring the trie is hashed first.
func (pg *ProofGenerator) rootHash() types.Hash {
	if pg.resTrie != nil {
		return pg.resTrie.Hash()
	}
	return pg.trie.Hash()
}

// HashProofNodes returns the keccak256 hash of concatenated proof node hashes.
// This can serve as a compact commitment to a proof.
func HashProofNodes(nodes [][]byte) types.Hash {
	var combined []byte
	for _, n := range nodes {
		h := crypto.Keccak256(n)
		combined = append(combined, h...)
	}
	if len(combined) == 0 {
		return types.Hash{}
	}
	return crypto.Keccak256Hash(combined)
}

func copySlice(b []byte) []byte {
	if b == nil {
		return nil
	}
	cp := make([]byte, len(b))
	copy(cp, b)
	return cp
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	buf := make([]byte, 0, 10)
	for n > 0 {
		buf = append(buf, byte('0'+n%10))
		n /= 10
	}
	// Reverse.
	for i, j := 0, len(buf)-1; i < j; i, j = i+1, j-1 {
		buf[i], buf[j] = buf[j], buf[i]
	}
	return string(buf)
}
