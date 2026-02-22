// trie_committer.go implements a trie commit and hashing pipeline with dirty
// node tracking, node reference counting for GC, batch database writes, and
// commit metrics. It provides a higher-level interface than the raw CommitTrie
// function in database.go.
package trie

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

// CommitMetrics tracks statistics about a trie commit operation.
type CommitMetrics struct {
	NodesWritten  int64
	BytesFlushed  int64
	DirtyBefore   int64
	DirtyAfter    int64
	CommitTimeNs  int64
	HashTimeNs    int64
}

// TrieCommitter manages the trie commit pipeline with dirty tracking,
// reference counting, and batch writes. All methods are safe for concurrent use.
type TrieCommitter struct {
	mu      sync.Mutex
	nodeDB  *NodeDatabase

	// Reference counting for GC: how many trie roots reference each node.
	refsMu sync.RWMutex
	refs   map[types.Hash]int32

	// Accumulated metrics across all commits.
	totalNodes   atomic.Int64
	totalBytes   atomic.Int64
	totalCommits atomic.Int64
}

// NewTrieCommitter creates a new committer backed by the given node database.
func NewTrieCommitter(db *NodeDatabase) *TrieCommitter {
	return &TrieCommitter{
		nodeDB: db,
		refs:   make(map[types.Hash]int32),
	}
}

// Commit hashes and stores all dirty nodes from the trie into the node
// database. Returns the root hash and commit metrics.
func (tc *TrieCommitter) Commit(t *Trie) (types.Hash, *CommitMetrics, error) {
	tc.mu.Lock()
	defer tc.mu.Unlock()

	metrics := &CommitMetrics{}
	dirtyBefore := tc.nodeDB.DirtyCount()
	metrics.DirtyBefore = int64(dirtyBefore)

	if t.root == nil {
		metrics.DirtyAfter = int64(tc.nodeDB.DirtyCount())
		return emptyRoot, metrics, nil
	}

	// Phase 1: Hash the trie to compute cached hashes (for timing metrics only).
	hashStart := time.Now()
	_ = t.Hash()
	metrics.HashTimeNs = time.Since(hashStart).Nanoseconds()

	// Phase 2: Recursively commit nodes. We use a fresh hasher to re-collapse
	// and encode nodes. The commitRecursive handles both clean and dirty nodes
	// because after Hash() nodes are marked clean but not yet stored in the DB.
	commitStart := time.Now()
	collector := &commitCollector{}
	hashed, cached := tc.commitRecursive(t.root, collector)
	t.root = cached

	// Phase 3: Write collected nodes to the database.
	for _, cn := range collector.nodes {
		tc.nodeDB.InsertNode(cn.hash, cn.data)
		tc.addRef(cn.hash)
	}

	metrics.CommitTimeNs = time.Since(commitStart).Nanoseconds()
	metrics.NodesWritten = int64(len(collector.nodes))
	for _, cn := range collector.nodes {
		metrics.BytesFlushed += int64(len(cn.data))
	}
	metrics.DirtyAfter = int64(tc.nodeDB.DirtyCount())

	// Extract root hash. Small root nodes (RLP < 32 bytes) are stored here.
	var rootHash types.Hash
	switch n := hashed.(type) {
	case hashNode:
		rootHash = types.BytesToHash(n)
	default:
		enc, err := encodeNode(hashed)
		if err != nil {
			return types.Hash{}, metrics, err
		}
		h := crypto.Keccak256Hash(enc)
		tc.nodeDB.InsertNode(h, enc)
		tc.addRef(h)
		metrics.NodesWritten++
		metrics.BytesFlushed += int64(len(enc))
		rootHash = h
	}

	// Update global counters after all nodes (including root) are accounted for.
	tc.totalNodes.Add(metrics.NodesWritten)
	tc.totalBytes.Add(metrics.BytesFlushed)
	tc.totalCommits.Add(1)

	return rootHash, metrics, nil
}

// CommitResolvable commits a resolvable (database-backed) trie.
func (tc *TrieCommitter) CommitResolvable(t *ResolvableTrie) (types.Hash, *CommitMetrics, error) {
	return tc.Commit(&t.Trie)
}

// Flush writes all dirty nodes from the node database to the given writer,
// clearing the dirty cache. Returns the number of nodes flushed.
func (tc *TrieCommitter) Flush(writer NodeWriter) (int, error) {
	tc.mu.Lock()
	defer tc.mu.Unlock()

	count := tc.nodeDB.DirtyCount()
	err := tc.nodeDB.Commit(writer)
	if err != nil {
		return 0, err
	}
	return count, nil
}

// Dereference decrements the reference count for nodes reachable from the
// given root. When a node's reference count drops to zero, it becomes
// eligible for garbage collection.
func (tc *TrieCommitter) Dereference(root types.Hash) []types.Hash {
	tc.refsMu.Lock()
	defer tc.refsMu.Unlock()

	var freed []types.Hash
	if root == emptyRoot || root == (types.Hash{}) {
		return freed
	}

	tc.refs[root]--
	if tc.refs[root] <= 0 {
		delete(tc.refs, root)
		freed = append(freed, root)
	}
	return freed
}

// RefCount returns the current reference count for a node hash.
func (tc *TrieCommitter) RefCount(hash types.Hash) int32 {
	tc.refsMu.RLock()
	defer tc.refsMu.RUnlock()
	return tc.refs[hash]
}

// TotalMetrics returns accumulated metrics across all commits.
func (tc *TrieCommitter) TotalMetrics() (nodes, bytesWritten, commits int64) {
	return tc.totalNodes.Load(), tc.totalBytes.Load(), tc.totalCommits.Load()
}

// DirtyCount returns the number of uncommitted nodes in the backing database.
func (tc *TrieCommitter) DirtyCount() int {
	return tc.nodeDB.DirtyCount()
}

// DirtySize returns the total byte size of uncommitted nodes.
func (tc *TrieCommitter) DirtySize() int {
	return tc.nodeDB.DirtySize()
}

// addRef increments the reference count for a node hash.
func (tc *TrieCommitter) addRef(hash types.Hash) {
	tc.refsMu.Lock()
	defer tc.refsMu.Unlock()
	tc.refs[hash]++
}

// commitCollector gathers nodes to write during a commit.
type commitCollector struct {
	nodes []collectedNode
}

type collectedNode struct {
	hash types.Hash
	data []byte
}

// commitRecursive recursively hashes and collects all storable nodes.
// Unlike a simple hash pass, this stores every node whose RLP encoding
// is >= 32 bytes into the collector for database persistence.
// Nodes that are already clean (not dirty) with a cached hash that exists
// in the nodeDB are skipped, avoiding redundant writes on re-commits.
func (tc *TrieCommitter) commitRecursive(n node, collector *commitCollector) (node, node) {
	switch n := n.(type) {
	case nil:
		return nil, nil
	case valueNode:
		return n, n
	case hashNode:
		// Already a hash reference (committed previously); skip.
		return n, n
	case *shortNode:
		// If this node was previously committed (clean with a cached hash
		// that the nodeDB already has), skip re-processing entirely.
		if hash, dirty := n.cache(); hash != nil && !dirty {
			h := types.BytesToHash(hash)
			if _, err := tc.nodeDB.Node(h); err == nil {
				return hashNode(hash), n
			}
		}

		collapsed := n.copy()
		collapsed.Key = hexToCompact(n.Key)
		cached := n.copy()

		if _, ok := n.Val.(valueNode); !ok {
			childH, childC := tc.commitRecursive(n.Val, collector)
			collapsed.Val = childH
			cached.Val = childC
		}

		enc, err := encodeNode(collapsed)
		if err != nil {
			return collapsed, cached
		}
		if len(enc) >= 32 {
			hash := crypto.Keccak256(enc)
			h := types.BytesToHash(hash)
			collector.nodes = append(collector.nodes, collectedNode{hash: h, data: enc})
			hn := hashNode(hash)
			cached.flags.hash = hn
			cached.flags.dirty = false
			return hn, cached
		}
		return collapsed, cached

	case *fullNode:
		// If this node was previously committed, skip re-processing.
		if hash, dirty := n.cache(); hash != nil && !dirty {
			h := types.BytesToHash(hash)
			if _, err := tc.nodeDB.Node(h); err == nil {
				return hashNode(hash), n
			}
		}

		collapsed := n.copy()
		cached := n.copy()

		for i := 0; i < 16; i++ {
			if n.Children[i] != nil {
				childH, childC := tc.commitRecursive(n.Children[i], collector)
				collapsed.Children[i] = childH
				cached.Children[i] = childC
			}
		}

		enc, err := encodeNode(collapsed)
		if err != nil {
			return collapsed, cached
		}
		if len(enc) >= 32 {
			hash := crypto.Keccak256(enc)
			h := types.BytesToHash(hash)
			collector.nodes = append(collector.nodes, collectedNode{hash: h, data: enc})
			hn := hashNode(hash)
			cached.flags.hash = hn
			cached.flags.dirty = false
			return hn, cached
		}
		return collapsed, cached
	}
	return n, n
}

// BatchWriter implements NodeWriter and buffers writes for batch flushing.
type BatchWriter struct {
	mu      sync.Mutex
	nodes   map[types.Hash][]byte
	maxSize int
	size    int
}

// NewBatchWriter creates a batch writer with the given maximum buffer size.
// When the buffer exceeds maxSize, a flush should be triggered.
func NewBatchWriter(maxSize int) *BatchWriter {
	if maxSize <= 0 {
		maxSize = 16 * 1024 * 1024 // 16 MiB default
	}
	return &BatchWriter{
		nodes:   make(map[types.Hash][]byte),
		maxSize: maxSize,
	}
}

// Put stores a node in the batch buffer.
func (bw *BatchWriter) Put(hash types.Hash, data []byte) error {
	bw.mu.Lock()
	defer bw.mu.Unlock()

	cp := make([]byte, len(data))
	copy(cp, data)
	if _, exists := bw.nodes[hash]; !exists {
		bw.size += len(data) + 32
	}
	bw.nodes[hash] = cp
	return nil
}

// FlushTo writes all buffered nodes to the target writer.
func (bw *BatchWriter) FlushTo(target NodeWriter) (int, error) {
	bw.mu.Lock()
	defer bw.mu.Unlock()

	count := 0
	for hash, data := range bw.nodes {
		if err := target.Put(hash, data); err != nil {
			return count, err
		}
		count++
	}
	bw.nodes = make(map[types.Hash][]byte)
	bw.size = 0
	return count, nil
}

// Size returns the current buffered data size in bytes.
func (bw *BatchWriter) Size() int {
	bw.mu.Lock()
	defer bw.mu.Unlock()
	return bw.size
}

// NeedFlush returns true if the buffer exceeds the configured maximum.
func (bw *BatchWriter) NeedFlush() bool {
	bw.mu.Lock()
	defer bw.mu.Unlock()
	return bw.size >= bw.maxSize
}

// Count returns the number of buffered nodes.
func (bw *BatchWriter) Count() int {
	bw.mu.Lock()
	defer bw.mu.Unlock()
	return len(bw.nodes)
}
