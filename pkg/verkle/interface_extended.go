// interface_extended.go extends the VerkleTree interface with multiproof
// support, stem-based access, batch operations, tree statistics,
// snapshotting, and state migration hooks.
package verkle

import (
	"errors"

	"github.com/eth2030/eth2030/core/types"
)

// Errors for extended operations.
var (
	ErrTreeEmpty       = errors.New("verkle: tree is empty")
	ErrBatchKeySize    = errors.New("verkle: batch key size mismatch")
	ErrBatchValueSize  = errors.New("verkle: batch value size mismatch")
	ErrSnapshotInvalid = errors.New("verkle: invalid snapshot")
	ErrStemSize        = errors.New("verkle: stem must be 31 bytes")
)

// BatchEntry represents a single key-value pair for batch operations.
type BatchEntry struct {
	Key   [KeySize]byte
	Value [ValueSize]byte
}

// MultiproofResult holds the results of a multiproof generation.
type MultiproofResult struct {
	// Keys that were proved.
	Keys [][KeySize]byte
	// Values at those keys (zero if absent).
	Values [][ValueSize]byte
	// Proofs for each key.
	Proofs []*VerkleProof
}

// TreeStats holds statistics about a Verkle tree.
type TreeStats struct {
	// InternalNodes is the count of internal branch nodes.
	InternalNodes int
	// LeafNodes is the count of leaf nodes with data.
	LeafNodes int
	// EmptyNodes is the count of empty slots.
	EmptyNodes int
	// TotalValues is the count of non-nil values across all leaves.
	TotalValues int
	// MaxDepth is the maximum depth of the tree.
	MaxDepth int
}

// TreeSnapshot captures a point-in-time copy of a VerkleTree for rollback.
type TreeSnapshot struct {
	tree VerkleTree
	root types.Hash
}

// ExtendedVerkleTree provides additional operations beyond the base
// VerkleTree interface: batch ops, stem access, multiproofs, stats, etc.
type ExtendedVerkleTree interface {
	VerkleTree

	// PutBatch inserts multiple key-value pairs atomically.
	PutBatch(entries []BatchEntry) error

	// DeleteBatch removes multiple keys atomically.
	DeleteBatch(keys [][KeySize]byte) error

	// GetStem retrieves all 256 values under the given 31-byte stem.
	// Returns a [256]*[32]byte array; nil entries mean absent values.
	GetStem(stem [StemSize]byte) ([NodeWidth]*[ValueSize]byte, error)

	// PutStem writes values to a stem. Only non-nil entries are written.
	PutStem(stem [StemSize]byte, values [NodeWidth]*[ValueSize]byte) error

	// GenerateMultiproof creates proofs for multiple keys at once.
	GenerateMultiproof(keys [][KeySize]byte) (*MultiproofResult, error)

	// Stats returns statistics about the tree structure.
	Stats() TreeStats
}

// InMemoryExtendedVerkleTree wraps InMemoryVerkleTree with extended ops.
type InMemoryExtendedVerkleTree struct {
	*InMemoryVerkleTree
}

// NewExtendedVerkleTree creates a new in-memory extended Verkle tree.
func NewExtendedVerkleTree() *InMemoryExtendedVerkleTree {
	return &InMemoryExtendedVerkleTree{
		InMemoryVerkleTree: NewInMemoryVerkleTree(),
	}
}

// PutBatch inserts multiple entries into the tree.
func (t *InMemoryExtendedVerkleTree) PutBatch(entries []BatchEntry) error {
	for _, e := range entries {
		if err := t.tree.Put(e.Key, e.Value); err != nil {
			return err
		}
	}
	return nil
}

// DeleteBatch removes multiple keys from the tree.
func (t *InMemoryExtendedVerkleTree) DeleteBatch(keys [][KeySize]byte) error {
	for _, k := range keys {
		if err := t.tree.Delete(k); err != nil {
			return err
		}
	}
	return nil
}

// GetStem retrieves all values under a 31-byte stem.
func (t *InMemoryExtendedVerkleTree) GetStem(stem [StemSize]byte) ([NodeWidth]*[ValueSize]byte, error) {
	var result [NodeWidth]*[ValueSize]byte
	leaf := t.tree.getLeaf(stem)
	if leaf == nil {
		return result, nil
	}
	for i := 0; i < NodeWidth; i++ {
		v := leaf.Get(byte(i))
		if v != nil {
			val := *v
			result[i] = &val
		}
	}
	return result, nil
}

// PutStem writes non-nil values to a stem.
func (t *InMemoryExtendedVerkleTree) PutStem(stem [StemSize]byte, values [NodeWidth]*[ValueSize]byte) error {
	for i := 0; i < NodeWidth; i++ {
		if values[i] != nil {
			var key [KeySize]byte
			copy(key[:StemSize], stem[:])
			key[StemSize] = byte(i)
			if err := t.tree.Put(key, *values[i]); err != nil {
				return err
			}
		}
	}
	return nil
}

// GenerateMultiproof creates proofs for multiple keys.
func (t *InMemoryExtendedVerkleTree) GenerateMultiproof(keys [][KeySize]byte) (*MultiproofResult, error) {
	if len(keys) == 0 {
		return nil, errors.New("verkle: no keys to prove")
	}

	result := &MultiproofResult{
		Keys:   keys,
		Values: make([][ValueSize]byte, len(keys)),
		Proofs: make([]*VerkleProof, len(keys)),
	}

	for i, key := range keys {
		val, _ := t.tree.Get(key)
		if val != nil {
			result.Values[i] = *val
		}

		proof, err := t.Prove(key[:])
		if err != nil {
			return nil, err
		}
		result.Proofs[i] = proof
	}

	return result, nil
}

// Stats returns statistics about the tree structure.
func (t *InMemoryExtendedVerkleTree) Stats() TreeStats {
	stats := TreeStats{}
	collectStats(t.tree.root, 0, &stats)
	return stats
}

func collectStats(n Node, depth int, stats *TreeStats) {
	if n == nil {
		stats.EmptyNodes++
		return
	}

	switch node := n.(type) {
	case *InternalNode:
		stats.InternalNodes++
		if depth > stats.MaxDepth {
			stats.MaxDepth = depth
		}
		for i := 0; i < NodeWidth; i++ {
			child := node.Child(byte(i))
			if child != nil {
				collectStats(child, depth+1, stats)
			}
		}

	case *LeafNode:
		stats.LeafNodes++
		if depth > stats.MaxDepth {
			stats.MaxDepth = depth
		}
		stats.TotalValues += node.ValueCount()

	case *EmptyNode:
		stats.EmptyNodes++
	}
}

// TakeSnapshot creates a copy of the tree for later rollback.
func TakeSnapshot(tree *InMemoryExtendedVerkleTree) (*TreeSnapshot, error) {
	root, err := tree.Commit()
	if err != nil {
		return nil, err
	}
	// Create a new tree and copy all data.
	cp := NewExtendedVerkleTree()
	copyTreeData(tree.tree, cp.tree)
	return &TreeSnapshot{
		tree: cp,
		root: root,
	}, nil
}

// RestoreSnapshot restores a tree from a snapshot.
func RestoreSnapshot(snap *TreeSnapshot) (*InMemoryExtendedVerkleTree, error) {
	if snap == nil {
		return nil, ErrSnapshotInvalid
	}
	ext, ok := snap.tree.(*InMemoryExtendedVerkleTree)
	if !ok {
		return nil, ErrSnapshotInvalid
	}
	// Create a fresh copy from the snapshot.
	cp := NewExtendedVerkleTree()
	copyTreeData(ext.tree, cp.tree)
	return cp, nil
}

// copyTreeData performs a shallow copy of tree data by re-walking the tree.
func copyTreeData(src, dst *Tree) {
	copyNode(src.root, dst.root, make([]byte, 0, StemSize))
}

func copyNode(src *InternalNode, dst *InternalNode, path []byte) {
	for i := 0; i < NodeWidth; i++ {
		child := src.Child(byte(i))
		if child == nil {
			continue
		}
		switch c := child.(type) {
		case *LeafNode:
			newLeaf := NewLeafNode(c.stem)
			for j := 0; j < NodeWidth; j++ {
				v := c.Get(byte(j))
				if v != nil {
					newLeaf.Set(byte(j), *v)
				}
			}
			dst.SetChild(byte(i), newLeaf)
		case *InternalNode:
			newInternal := NewInternalNode(c.depth)
			dst.SetChild(byte(i), newInternal)
			copyNode(c, newInternal, append(path, byte(i)))
		}
	}
}

// StateMigrationHook is called during state migration from MPT to Verkle.
// Implementations can perform custom logic during migration.
type StateMigrationHook interface {
	// OnAccountMigrated is called after an account is migrated.
	OnAccountMigrated(addr types.Address) error
	// OnStorageMigrated is called after a storage slot is migrated.
	OnStorageMigrated(addr types.Address, slot types.Hash) error
	// OnMigrationComplete is called when the full migration finishes.
	OnMigrationComplete(root types.Hash) error
}

// NoopMigrationHook is a no-op implementation of StateMigrationHook.
type NoopMigrationHook struct{}

func (NoopMigrationHook) OnAccountMigrated(_ types.Address) error       { return nil }
func (NoopMigrationHook) OnStorageMigrated(_ types.Address, _ types.Hash) error { return nil }
func (NoopMigrationHook) OnMigrationComplete(_ types.Hash) error        { return nil }

// CountingMigrationHook counts migration events.
type CountingMigrationHook struct {
	AccountCount  int
	StorageCount  int
	CompleteCount int
	FinalRoot     types.Hash
}

func (h *CountingMigrationHook) OnAccountMigrated(_ types.Address) error {
	h.AccountCount++
	return nil
}

func (h *CountingMigrationHook) OnStorageMigrated(_ types.Address, _ types.Hash) error {
	h.StorageCount++
	return nil
}

func (h *CountingMigrationHook) OnMigrationComplete(root types.Hash) error {
	h.CompleteCount++
	h.FinalRoot = root
	return nil
}
