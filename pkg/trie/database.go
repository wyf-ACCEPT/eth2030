package trie

import (
	"errors"
	"sync"

	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/crypto"
)

var (
	ErrNodeNotFound = errors.New("trie: node not found in database")
)

// NodeReader retrieves trie nodes by hash.
type NodeReader interface {
	// Node retrieves the RLP-encoded trie node with the given hash.
	Node(hash types.Hash) ([]byte, error)
}

// NodeWriter stores trie nodes by hash.
type NodeWriter interface {
	// Put stores a trie node keyed by its hash.
	Put(hash types.Hash, data []byte) error
}

// NodeDatabase stores trie nodes in a two-layer cache: dirty nodes
// (pending commit) are kept in memory, with a disk-backed reader
// for committed nodes.
type NodeDatabase struct {
	mu    sync.RWMutex
	dirty map[types.Hash][]byte // uncommitted nodes
	disk  NodeReader            // backing store (nil for in-memory only)
	size  int                   // total size of dirty data in bytes
}

// NewNodeDatabase creates a trie node database backed by the given reader.
// If disk is nil, the database operates in memory only.
func NewNodeDatabase(disk NodeReader) *NodeDatabase {
	return &NodeDatabase{
		dirty: make(map[types.Hash][]byte),
		disk:  disk,
	}
}

// Node retrieves a trie node by hash. It checks the dirty cache first,
// then falls back to the disk reader.
func (db *NodeDatabase) Node(hash types.Hash) ([]byte, error) {
	if hash == (types.Hash{}) {
		return nil, ErrNodeNotFound
	}

	db.mu.RLock()
	if data, ok := db.dirty[hash]; ok {
		db.mu.RUnlock()
		return data, nil
	}
	db.mu.RUnlock()

	if db.disk != nil {
		return db.disk.Node(hash)
	}
	return nil, ErrNodeNotFound
}

// InsertNode stores a trie node in the dirty cache.
func (db *NodeDatabase) InsertNode(hash types.Hash, data []byte) {
	db.mu.Lock()
	defer db.mu.Unlock()
	if _, ok := db.dirty[hash]; !ok {
		db.size += len(data)
	}
	db.dirty[hash] = data
}

// DirtySize returns the total byte size of uncommitted nodes.
func (db *NodeDatabase) DirtySize() int {
	db.mu.RLock()
	defer db.mu.RUnlock()
	return db.size
}

// DirtyCount returns the number of uncommitted nodes.
func (db *NodeDatabase) DirtyCount() int {
	db.mu.RLock()
	defer db.mu.RUnlock()
	return len(db.dirty)
}

// Commit writes all dirty nodes to the given writer and clears the cache.
func (db *NodeDatabase) Commit(writer NodeWriter) error {
	db.mu.Lock()
	defer db.mu.Unlock()

	for hash, data := range db.dirty {
		if err := writer.Put(hash, data); err != nil {
			return err
		}
	}
	db.dirty = make(map[types.Hash][]byte)
	db.size = 0
	return nil
}

// rawdbNodeReader adapts a rawdb key-value reader to the NodeReader interface.
type rawdbNodeReader struct {
	get func(key []byte) ([]byte, error)
}

func (r *rawdbNodeReader) Node(hash types.Hash) ([]byte, error) {
	// Use the trie node prefix "t" + hash
	key := append([]byte("t"), hash[:]...)
	data, err := r.get(key)
	if err != nil {
		return nil, ErrNodeNotFound
	}
	return data, nil
}

// NewRawDBNodeReader creates a NodeReader from a function that reads by key.
func NewRawDBNodeReader(get func(key []byte) ([]byte, error)) NodeReader {
	return &rawdbNodeReader{get: get}
}

// rawdbNodeWriter adapts a rawdb key-value writer to the NodeWriter interface.
type rawdbNodeWriter struct {
	put func(key, value []byte) error
}

func (w *rawdbNodeWriter) Put(hash types.Hash, data []byte) error {
	key := append([]byte("t"), hash[:]...)
	return w.put(key, data)
}

// NewRawDBNodeWriter creates a NodeWriter from a function that writes by key.
func NewRawDBNodeWriter(put func(key, value []byte) error) NodeWriter {
	return &rawdbNodeWriter{put: put}
}

// CommitTrie collects all dirty nodes from the trie and stores them in
// the node database. Returns the root hash.
func CommitTrie(t *Trie, db *NodeDatabase) (types.Hash, error) {
	if t.root == nil {
		return emptyRoot, nil
	}

	h := newHasher()
	root, cached := commitNode(h, t.root, db)
	t.root = cached

	switch n := root.(type) {
	case hashNode:
		return types.BytesToHash(n), nil
	default:
		enc, err := encodeNode(root)
		if err != nil {
			return types.Hash{}, err
		}
		hash := crypto.Keccak256Hash(enc)
		db.InsertNode(hash, enc)
		return hash, nil
	}
}

// commitNode recursively hashes and stores all dirty nodes in the database.
func commitNode(h *hasher, n node, db *NodeDatabase) (node, node) {
	switch n := n.(type) {
	case nil:
		return nil, nil

	case valueNode:
		return n, n

	case hashNode:
		return n, n

	case *shortNode:
		// Commit child first.
		collapsed := n.copy()
		collapsed.Key = hexToCompact(n.Key)

		cached := n.copy()
		if _, ok := n.Val.(valueNode); !ok {
			childH, childC := commitNode(h, n.Val, db)
			collapsed.Val = childH
			cached.Val = childC
		}

		// Encode and store.
		enc, err := encodeNode(collapsed)
		if err != nil {
			return collapsed, cached
		}
		if len(enc) >= 32 {
			hash := crypto.Keccak256(enc)
			db.InsertNode(types.BytesToHash(hash), enc)
			hn := hashNode(hash)
			cached.flags.hash = hn
			cached.flags.dirty = false
			return hn, cached
		}
		return collapsed, cached

	case *fullNode:
		collapsed := n.copy()
		cached := n.copy()

		for i := 0; i < 16; i++ {
			if n.Children[i] != nil {
				childH, childC := commitNode(h, n.Children[i], db)
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
			db.InsertNode(types.BytesToHash(hash), enc)
			hn := hashNode(hash)
			cached.flags.hash = hn
			cached.flags.dirty = false
			return hn, cached
		}
		return collapsed, cached
	}

	return n, n
}

// ResolveTrie creates a trie that can resolve hashNode references from
// the node database. This enables loading tries from persistent storage.
type ResolvableTrie struct {
	Trie
	db *NodeDatabase
}

// NewResolvableTrie creates a trie backed by the given node database.
// If root is the empty root hash, returns an empty trie.
func NewResolvableTrie(root types.Hash, db *NodeDatabase) (*ResolvableTrie, error) {
	t := &ResolvableTrie{
		db: db,
	}
	if root == emptyRoot || root == (types.Hash{}) {
		return t, nil
	}

	// Load root node from database.
	rootNode, err := t.resolveHash(hashNode(root[:]))
	if err != nil {
		return nil, err
	}
	t.root = rootNode
	return t, nil
}

// Get retrieves a value from the trie, resolving hash nodes as needed.
func (t *ResolvableTrie) Get(key []byte) ([]byte, error) {
	value, found := t.resolveGet(t.root, keybytesToHex(key), 0)
	if !found {
		return nil, ErrNotFound
	}
	return value, nil
}

func (t *ResolvableTrie) resolveGet(n node, key []byte, pos int) ([]byte, bool) {
	switch n := n.(type) {
	case nil:
		return nil, false
	case valueNode:
		return []byte(n), true
	case *shortNode:
		if len(key)-pos < len(n.Key) || !keysEqual(n.Key, key[pos:pos+len(n.Key)]) {
			return nil, false
		}
		return t.resolveGet(n.Val, key, pos+len(n.Key))
	case *fullNode:
		if pos >= len(key) {
			return t.resolveGet(n.Children[16], key, pos)
		}
		return t.resolveGet(n.Children[key[pos]], key, pos+1)
	case hashNode:
		resolved, err := t.resolveHash(n)
		if err != nil {
			return nil, false
		}
		return t.resolveGet(resolved, key, pos)
	default:
		return nil, false
	}
}

// resolveHash loads a node from the database by its hash.
func (t *ResolvableTrie) resolveHash(hash hashNode) (node, error) {
	h := types.BytesToHash(hash)
	data, err := t.db.Node(h)
	if err != nil {
		return nil, err
	}
	return decodeNode(hash, data)
}

// Put inserts a key-value pair, resolving hash nodes as needed.
func (t *ResolvableTrie) Put(key, value []byte) error {
	if len(value) == 0 {
		return t.Trie.Delete(key)
	}
	k := keybytesToHex(key)
	n, err := t.resolveInsert(t.root, nil, k, valueNode(value))
	if err != nil {
		return err
	}
	t.root = n
	return nil
}

func (t *ResolvableTrie) resolveInsert(n node, prefix, key []byte, value node) (node, error) {
	if hn, ok := n.(hashNode); ok {
		resolved, err := t.resolveHash(hn)
		if err != nil {
			return nil, err
		}
		return t.resolveInsert(resolved, prefix, key, value)
	}
	return t.Trie.insert(n, prefix, key, value)
}

// Hash computes the root hash.
func (t *ResolvableTrie) Hash() types.Hash {
	return t.Trie.Hash()
}

// Commit stores all dirty nodes to the database and returns the root hash.
func (t *ResolvableTrie) Commit() (types.Hash, error) {
	return CommitTrie(&t.Trie, t.db)
}
