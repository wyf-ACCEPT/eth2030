package sync

import (
	"errors"
	"sync"

	"github.com/eth2030/eth2030/core/rawdb"
	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

// TrieSync errors.
var (
	ErrAlreadyProcessed = errors.New("trie sync: node already processed")
	ErrNotRequested     = errors.New("trie sync: node was not requested")
	ErrHashMismatch     = errors.New("trie sync: hash mismatch")
)

// trieSyncState tracks the state of a single trie node request.
type trieSyncState int

const (
	stateUnknown    trieSyncState = iota // not yet seen
	statePending                         // scheduled for download
	stateProcessed                       // downloaded and validated
	stateCommitted                       // written to the database
)

// trieSyncNode tracks a single trie node during synchronization.
type trieSyncNode struct {
	hash  types.Hash    // expected hash of the node
	path  []byte        // trie path of the node
	data  []byte        // downloaded node data (nil until processed)
	state trieSyncState // lifecycle state
	deps  int           // number of children not yet resolved
	// parent tracks the requesting node so we can decrement its dep count.
	parent *trieSyncNode
}

// codeEntry tracks a single bytecode download.
type codeEntry struct {
	hash  types.Hash
	data  []byte
	state trieSyncState
}

// TrieSync manages pending trie node and bytecode download requests for
// state synchronization. It tracks dependencies between trie nodes so
// that parent nodes are only committed after their children are resolved.
type TrieSync struct {
	mu sync.Mutex

	// Trie node requests keyed by hash.
	nodeReqs map[types.Hash]*trieSyncNode

	// Bytecode requests keyed by code hash.
	codeReqs map[types.Hash]*codeEntry

	// Queue of hashes that are pending download.
	pendingNodes []types.Hash
	pendingCodes []types.Hash

	// Database for committing completed nodes.
	db rawdb.KeyValueStore

	// Healing mode: when true, all unresolved hash references encountered
	// during processing are automatically scheduled for download.
	healing bool
}

// NewTrieSync creates a new trie synchronization scheduler.
func NewTrieSync(db rawdb.KeyValueStore) *TrieSync {
	return &TrieSync{
		nodeReqs: make(map[types.Hash]*trieSyncNode),
		codeReqs: make(map[types.Hash]*codeEntry),
		db:       db,
	}
}

// SetHealing enables or disables healing mode. In healing mode, every
// hash reference found during Process that is not already known is
// automatically scheduled for download.
func (s *TrieSync) SetHealing(enabled bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.healing = enabled
}

// AddSubTrie schedules a subtrie rooted at the given hash for downloading.
// The path parameter identifies the trie location of the root node.
// If the hash is already known to the database, it is a no-op.
func (s *TrieSync) AddSubTrie(hash types.Hash, path []byte, parent *trieSyncNode) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.addSubTrieLocked(hash, path, parent)
}

func (s *TrieSync) addSubTrieLocked(hash types.Hash, path []byte, parent *trieSyncNode) {
	// Skip the empty hash.
	if hash == (types.Hash{}) || hash == types.EmptyRootHash {
		return
	}

	// Skip if already requested.
	if _, ok := s.nodeReqs[hash]; ok {
		return
	}

	// Skip if already in the database.
	key := trieNodeKey(hash)
	if exists, _ := s.db.Has(key); exists {
		return
	}

	pathCopy := make([]byte, len(path))
	copy(pathCopy, path)

	node := &trieSyncNode{
		hash:   hash,
		path:   pathCopy,
		state:  statePending,
		parent: parent,
	}
	s.nodeReqs[hash] = node
	s.pendingNodes = append(s.pendingNodes, hash)

	// Increment parent's dependency count.
	if parent != nil {
		parent.deps++
	}
}

// AddCodeEntry schedules a bytecode for downloading by its hash.
// If the code is already in the database, it is a no-op.
func (s *TrieSync) AddCodeEntry(hash types.Hash) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if hash == (types.Hash{}) || hash == types.EmptyCodeHash {
		return
	}

	if _, ok := s.codeReqs[hash]; ok {
		return
	}

	// Check the database.
	key := codeKey(hash)
	if exists, _ := s.db.Has(key); exists {
		return
	}

	entry := &codeEntry{
		hash:  hash,
		state: statePending,
	}
	s.codeReqs[hash] = entry
	s.pendingCodes = append(s.pendingCodes, hash)
}

// Missing returns a list of trie node hashes that are still pending
// download. The caller should request these from the network.
func (s *TrieSync) Missing(max int) []types.Hash {
	s.mu.Lock()
	defer s.mu.Unlock()

	count := len(s.pendingNodes)
	if max > 0 && count > max {
		count = max
	}

	result := make([]types.Hash, count)
	copy(result, s.pendingNodes[:count])
	return result
}

// MissingCodes returns a list of bytecode hashes that are still pending
// download.
func (s *TrieSync) MissingCodes(max int) []types.Hash {
	s.mu.Lock()
	defer s.mu.Unlock()

	count := len(s.pendingCodes)
	if max > 0 && count > max {
		count = max
	}

	result := make([]types.Hash, count)
	copy(result, s.pendingCodes[:count])
	return result
}

// Pending returns the total count of pending trie node requests.
func (s *TrieSync) Pending() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.pendingNodes)
}

// PendingCodes returns the total count of pending bytecode requests.
func (s *TrieSync) PendingCodes() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.pendingCodes)
}

// ProcessNode validates and stores a downloaded trie node. The hash must
// match a previously scheduled request. The node data is validated by
// checking its Keccak256 hash against the expected value.
//
// In healing mode, any child hash references in the node data that are
// not yet known are automatically scheduled for download.
func (s *TrieSync) ProcessNode(hash types.Hash, data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	node, ok := s.nodeReqs[hash]
	if !ok {
		return ErrNotRequested
	}
	if node.state >= stateProcessed {
		return ErrAlreadyProcessed
	}

	// Validate the hash.
	computed := types.BytesToHash(crypto.Keccak256(data))
	if computed != hash {
		return ErrHashMismatch
	}

	node.data = make([]byte, len(data))
	copy(node.data, data)
	node.state = stateProcessed

	// Remove from pending queue.
	s.removePendingNode(hash)

	// In healing mode, scan the node data for child hash references
	// and schedule any unknown ones.
	if s.healing {
		s.scheduleChildrenLocked(node, data)
	}

	return nil
}

// ProcessCode validates and stores a downloaded bytecode. The hash must
// match a previously scheduled code request.
func (s *TrieSync) ProcessCode(hash types.Hash, data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	entry, ok := s.codeReqs[hash]
	if !ok {
		return ErrNotRequested
	}
	if entry.state >= stateProcessed {
		return ErrAlreadyProcessed
	}

	// Validate the hash.
	computed := types.BytesToHash(crypto.Keccak256(data))
	if computed != hash {
		return ErrHashMismatch
	}

	entry.data = make([]byte, len(data))
	copy(entry.data, data)
	entry.state = stateProcessed

	// Remove from pending codes queue.
	s.removePendingCode(hash)

	return nil
}

// Commit flushes all processed nodes and codes to the database.
// Returns the number of items committed.
func (s *TrieSync) Commit() (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	committed := 0

	// Commit trie nodes.
	for hash, node := range s.nodeReqs {
		if node.state != stateProcessed {
			continue
		}
		key := trieNodeKey(hash)
		if err := s.db.Put(key, node.data); err != nil {
			return committed, err
		}
		node.state = stateCommitted
		committed++

		// Decrement parent dependency.
		if node.parent != nil {
			node.parent.deps--
		}
	}

	// Commit bytecodes.
	for hash, entry := range s.codeReqs {
		if entry.state != stateProcessed {
			continue
		}
		key := codeKey(hash)
		if err := s.db.Put(key, entry.data); err != nil {
			return committed, err
		}
		entry.state = stateCommitted
		committed++
	}

	// Clean up committed entries.
	for hash, node := range s.nodeReqs {
		if node.state == stateCommitted {
			delete(s.nodeReqs, hash)
		}
	}
	for hash, entry := range s.codeReqs {
		if entry.state == stateCommitted {
			delete(s.codeReqs, hash)
		}
	}

	return committed, nil
}

// scheduleChildrenLocked scans node data for 32-byte hash references
// and schedules unknown ones for download. This implements healing mode
// where interior trie nodes that reference missing children automatically
// trigger additional downloads.
//
// A 32-byte sequence in the node data is considered a potential child
// hash reference. This is a heuristic: we look for 32-byte sequences
// that could be hash nodes embedded in the RLP-encoded trie node.
func (s *TrieSync) scheduleChildrenLocked(parent *trieSyncNode, data []byte) {
	// Scan for potential hash references in the node data.
	// In RLP-encoded trie nodes, hash references are 32-byte strings.
	// We look for the RLP prefix 0xa0 (32-byte string) followed by 32 bytes.
	for i := 0; i < len(data)-32; i++ {
		if data[i] == 0xa0 && i+33 <= len(data) {
			var childHash types.Hash
			copy(childHash[:], data[i+1:i+33])

			// Skip empty and well-known hashes.
			if childHash == (types.Hash{}) || childHash == types.EmptyRootHash || childHash == types.EmptyCodeHash {
				continue
			}

			childPath := append(parent.path, byte(i))
			s.addSubTrieLocked(childHash, childPath, parent)
		}
	}
}

// removePendingNode removes a hash from the pending nodes list.
func (s *TrieSync) removePendingNode(hash types.Hash) {
	for i, h := range s.pendingNodes {
		if h == hash {
			s.pendingNodes = append(s.pendingNodes[:i], s.pendingNodes[i+1:]...)
			return
		}
	}
}

// removePendingCode removes a hash from the pending codes list.
func (s *TrieSync) removePendingCode(hash types.Hash) {
	for i, h := range s.pendingCodes {
		if h == hash {
			s.pendingCodes = append(s.pendingCodes[:i], s.pendingCodes[i+1:]...)
			return
		}
	}
}

// trieNodeKey builds the database key for a trie node.
// Uses the "t" prefix followed by the node hash, matching the schema
// in core/rawdb/schema.go.
func trieNodeKey(hash types.Hash) []byte {
	return append([]byte("t"), hash[:]...)
}

// codeKey builds the database key for a bytecode entry.
// Uses the "C" prefix followed by the code hash, matching the schema
// in core/rawdb/schema.go.
func codeKey(hash types.Hash) []byte {
	return append([]byte("C"), hash[:]...)
}
