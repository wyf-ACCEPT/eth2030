package sync

import (
	"testing"

	"github.com/eth2028/eth2028/core/rawdb"
	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/crypto"
)

// makeNodeData creates deterministic node data and returns its Keccak256 hash.
func makeNodeData(seed byte) (types.Hash, []byte) {
	data := []byte{seed, seed + 1, seed + 2, seed + 3, 0xab, 0xcd, 0xef, 0x01,
		seed + 4, seed + 5, seed + 6, seed + 7, 0x12, 0x34, 0x56, 0x78,
		seed + 8, seed + 9, seed + 10, seed + 11, 0x9a, 0xbc, 0xde, 0xf0,
		seed + 12, seed + 13, seed + 14, seed + 15, 0x11, 0x22, 0x33, 0x44,
	}
	hash := types.BytesToHash(crypto.Keccak256(data))
	return hash, data
}

// --- Scheduling tests ---

func TestTrieSync_AddSubTrie(t *testing.T) {
	db := rawdb.NewMemoryDB()
	ts := NewTrieSync(db)

	hash1, _ := makeNodeData(0x01)
	hash2, _ := makeNodeData(0x02)

	ts.AddSubTrie(hash1, []byte{0x00}, nil)
	ts.AddSubTrie(hash2, []byte{0x01}, nil)

	if ts.Pending() != 2 {
		t.Fatalf("pending: want 2, got %d", ts.Pending())
	}
}

func TestTrieSync_AddSubTrie_Dedup(t *testing.T) {
	db := rawdb.NewMemoryDB()
	ts := NewTrieSync(db)

	hash, _ := makeNodeData(0x01)

	ts.AddSubTrie(hash, []byte{0x00}, nil)
	ts.AddSubTrie(hash, []byte{0x00}, nil) // duplicate

	if ts.Pending() != 1 {
		t.Fatalf("pending: want 1 (deduped), got %d", ts.Pending())
	}
}

func TestTrieSync_AddSubTrie_SkipsEmptyHash(t *testing.T) {
	db := rawdb.NewMemoryDB()
	ts := NewTrieSync(db)

	ts.AddSubTrie(types.Hash{}, []byte{0x00}, nil)
	ts.AddSubTrie(types.EmptyRootHash, []byte{0x01}, nil)

	if ts.Pending() != 0 {
		t.Fatalf("pending: want 0, got %d", ts.Pending())
	}
}

func TestTrieSync_AddSubTrie_SkipsExisting(t *testing.T) {
	db := rawdb.NewMemoryDB()
	hash, data := makeNodeData(0x01)

	// Pre-populate the database.
	key := trieNodeKey(hash)
	if err := db.Put(key, data); err != nil {
		t.Fatal(err)
	}

	ts := NewTrieSync(db)
	ts.AddSubTrie(hash, []byte{0x00}, nil)

	if ts.Pending() != 0 {
		t.Fatalf("pending: want 0 (already in db), got %d", ts.Pending())
	}
}

func TestTrieSync_AddCodeEntry(t *testing.T) {
	db := rawdb.NewMemoryDB()
	ts := NewTrieSync(db)

	code := []byte{0x60, 0x00, 0xfd}
	hash := types.BytesToHash(crypto.Keccak256(code))

	ts.AddCodeEntry(hash)

	if ts.PendingCodes() != 1 {
		t.Fatalf("pending codes: want 1, got %d", ts.PendingCodes())
	}
}

func TestTrieSync_AddCodeEntry_SkipsEmpty(t *testing.T) {
	db := rawdb.NewMemoryDB()
	ts := NewTrieSync(db)

	ts.AddCodeEntry(types.Hash{})
	ts.AddCodeEntry(types.EmptyCodeHash)

	if ts.PendingCodes() != 0 {
		t.Fatalf("pending codes: want 0, got %d", ts.PendingCodes())
	}
}

func TestTrieSync_AddCodeEntry_Dedup(t *testing.T) {
	db := rawdb.NewMemoryDB()
	ts := NewTrieSync(db)

	code := []byte{0x60, 0x00}
	hash := types.BytesToHash(crypto.Keccak256(code))

	ts.AddCodeEntry(hash)
	ts.AddCodeEntry(hash) // duplicate

	if ts.PendingCodes() != 1 {
		t.Fatalf("pending codes: want 1, got %d", ts.PendingCodes())
	}
}

// --- Processing tests ---

func TestTrieSync_ProcessNode(t *testing.T) {
	db := rawdb.NewMemoryDB()
	ts := NewTrieSync(db)

	hash, data := makeNodeData(0x01)
	ts.AddSubTrie(hash, []byte{0x00}, nil)

	if err := ts.ProcessNode(hash, data); err != nil {
		t.Fatalf("ProcessNode: %v", err)
	}

	// Should no longer be pending.
	if ts.Pending() != 0 {
		t.Fatalf("pending: want 0, got %d", ts.Pending())
	}
}

func TestTrieSync_ProcessNode_HashMismatch(t *testing.T) {
	db := rawdb.NewMemoryDB()
	ts := NewTrieSync(db)

	hash, _ := makeNodeData(0x01)
	ts.AddSubTrie(hash, []byte{0x00}, nil)

	// Process with wrong data.
	err := ts.ProcessNode(hash, []byte{0xff, 0xff})
	if err != ErrHashMismatch {
		t.Fatalf("expected ErrHashMismatch, got %v", err)
	}
}

func TestTrieSync_ProcessNode_NotRequested(t *testing.T) {
	db := rawdb.NewMemoryDB()
	ts := NewTrieSync(db)

	hash, data := makeNodeData(0x01)
	err := ts.ProcessNode(hash, data)
	if err != ErrNotRequested {
		t.Fatalf("expected ErrNotRequested, got %v", err)
	}
}

func TestTrieSync_ProcessNode_AlreadyProcessed(t *testing.T) {
	db := rawdb.NewMemoryDB()
	ts := NewTrieSync(db)

	hash, data := makeNodeData(0x01)
	ts.AddSubTrie(hash, []byte{0x00}, nil)

	if err := ts.ProcessNode(hash, data); err != nil {
		t.Fatal(err)
	}

	// Processing again should return ErrAlreadyProcessed.
	err := ts.ProcessNode(hash, data)
	if err != ErrAlreadyProcessed {
		t.Fatalf("expected ErrAlreadyProcessed, got %v", err)
	}
}

func TestTrieSync_ProcessCode(t *testing.T) {
	db := rawdb.NewMemoryDB()
	ts := NewTrieSync(db)

	code := []byte{0x60, 0x00, 0xfd}
	hash := types.BytesToHash(crypto.Keccak256(code))

	ts.AddCodeEntry(hash)

	if err := ts.ProcessCode(hash, code); err != nil {
		t.Fatalf("ProcessCode: %v", err)
	}

	if ts.PendingCodes() != 0 {
		t.Fatalf("pending codes: want 0, got %d", ts.PendingCodes())
	}
}

func TestTrieSync_ProcessCode_HashMismatch(t *testing.T) {
	db := rawdb.NewMemoryDB()
	ts := NewTrieSync(db)

	code := []byte{0x60, 0x00}
	hash := types.BytesToHash(crypto.Keccak256(code))
	ts.AddCodeEntry(hash)

	err := ts.ProcessCode(hash, []byte{0xff})
	if err != ErrHashMismatch {
		t.Fatalf("expected ErrHashMismatch, got %v", err)
	}
}

// --- Missing nodes tests ---

func TestTrieSync_Missing(t *testing.T) {
	db := rawdb.NewMemoryDB()
	ts := NewTrieSync(db)

	hash1, _ := makeNodeData(0x01)
	hash2, _ := makeNodeData(0x02)
	hash3, _ := makeNodeData(0x03)

	ts.AddSubTrie(hash1, []byte{0x00}, nil)
	ts.AddSubTrie(hash2, []byte{0x01}, nil)
	ts.AddSubTrie(hash3, []byte{0x02}, nil)

	missing := ts.Missing(0)
	if len(missing) != 3 {
		t.Fatalf("missing: want 3, got %d", len(missing))
	}

	// Test with max limit.
	missing = ts.Missing(2)
	if len(missing) != 2 {
		t.Fatalf("missing(max=2): want 2, got %d", len(missing))
	}
}

func TestTrieSync_MissingCodes(t *testing.T) {
	db := rawdb.NewMemoryDB()
	ts := NewTrieSync(db)

	code1 := []byte{0x01}
	code2 := []byte{0x02}
	hash1 := types.BytesToHash(crypto.Keccak256(code1))
	hash2 := types.BytesToHash(crypto.Keccak256(code2))

	ts.AddCodeEntry(hash1)
	ts.AddCodeEntry(hash2)

	missing := ts.MissingCodes(0)
	if len(missing) != 2 {
		t.Fatalf("missing codes: want 2, got %d", len(missing))
	}
}

func TestTrieSync_Missing_AfterProcess(t *testing.T) {
	db := rawdb.NewMemoryDB()
	ts := NewTrieSync(db)

	hash1, data1 := makeNodeData(0x01)
	hash2, _ := makeNodeData(0x02)

	ts.AddSubTrie(hash1, []byte{0x00}, nil)
	ts.AddSubTrie(hash2, []byte{0x01}, nil)

	// Process one node.
	if err := ts.ProcessNode(hash1, data1); err != nil {
		t.Fatal(err)
	}

	missing := ts.Missing(0)
	if len(missing) != 1 {
		t.Fatalf("missing: want 1, got %d", len(missing))
	}
	if missing[0] != hash2 {
		t.Fatalf("missing hash mismatch")
	}
}

// --- Commit tests ---

func TestTrieSync_Commit(t *testing.T) {
	db := rawdb.NewMemoryDB()
	ts := NewTrieSync(db)

	hash, data := makeNodeData(0x01)
	ts.AddSubTrie(hash, []byte{0x00}, nil)

	if err := ts.ProcessNode(hash, data); err != nil {
		t.Fatal(err)
	}

	committed, err := ts.Commit()
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if committed != 1 {
		t.Fatalf("committed: want 1, got %d", committed)
	}

	// Verify it is in the database.
	key := trieNodeKey(hash)
	stored, err := db.Get(key)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(stored) != len(data) {
		t.Fatal("stored data length mismatch")
	}
}

func TestTrieSync_CommitCodes(t *testing.T) {
	db := rawdb.NewMemoryDB()
	ts := NewTrieSync(db)

	code := []byte{0x60, 0x00, 0xfd}
	hash := types.BytesToHash(crypto.Keccak256(code))

	ts.AddCodeEntry(hash)
	if err := ts.ProcessCode(hash, code); err != nil {
		t.Fatal(err)
	}

	committed, err := ts.Commit()
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if committed != 1 {
		t.Fatalf("committed: want 1, got %d", committed)
	}

	// Verify in database.
	key := codeKey(hash)
	stored, err := db.Get(key)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(stored) != string(code) {
		t.Fatal("stored code mismatch")
	}
}

func TestTrieSync_CommitMultiple(t *testing.T) {
	db := rawdb.NewMemoryDB()
	ts := NewTrieSync(db)

	hash1, data1 := makeNodeData(0x01)
	hash2, data2 := makeNodeData(0x02)
	code := []byte{0x60, 0x01}
	codeHash := types.BytesToHash(crypto.Keccak256(code))

	ts.AddSubTrie(hash1, []byte{0x00}, nil)
	ts.AddSubTrie(hash2, []byte{0x01}, nil)
	ts.AddCodeEntry(codeHash)

	if err := ts.ProcessNode(hash1, data1); err != nil {
		t.Fatal(err)
	}
	if err := ts.ProcessNode(hash2, data2); err != nil {
		t.Fatal(err)
	}
	if err := ts.ProcessCode(codeHash, code); err != nil {
		t.Fatal(err)
	}

	committed, err := ts.Commit()
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if committed != 3 {
		t.Fatalf("committed: want 3, got %d", committed)
	}

	// Pending should be zero after commit.
	if ts.Pending() != 0 {
		t.Fatalf("pending: want 0, got %d", ts.Pending())
	}
	if ts.PendingCodes() != 0 {
		t.Fatalf("pending codes: want 0, got %d", ts.PendingCodes())
	}
}

func TestTrieSync_CommitEmpty(t *testing.T) {
	db := rawdb.NewMemoryDB()
	ts := NewTrieSync(db)

	committed, err := ts.Commit()
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if committed != 0 {
		t.Fatalf("committed: want 0, got %d", committed)
	}
}

// --- Healing mode tests ---

func TestTrieSync_HealingMode_SchedulesChildren(t *testing.T) {
	db := rawdb.NewMemoryDB()
	ts := NewTrieSync(db)
	ts.SetHealing(true)

	// Create a parent node that embeds a child hash reference.
	// RLP encoding of a 32-byte string starts with 0xa0.
	childData := []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
		0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10,
		0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18,
		0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f, 0x20,
	}
	childHash := types.BytesToHash(crypto.Keccak256(childData))

	// Build parent data that contains an 0xa0 prefix followed by the child hash.
	parentData := make([]byte, 0, 64)
	parentData = append(parentData, 0xab) // some prefix byte
	parentData = append(parentData, 0xa0) // RLP 32-byte string prefix
	parentData = append(parentData, childHash[:]...)

	parentHash := types.BytesToHash(crypto.Keccak256(parentData))

	ts.AddSubTrie(parentHash, []byte{0x00}, nil)

	// Process the parent node.
	if err := ts.ProcessNode(parentHash, parentData); err != nil {
		t.Fatalf("ProcessNode: %v", err)
	}

	// In healing mode, the child should now be scheduled.
	if ts.Pending() != 1 {
		t.Fatalf("pending: want 1 (child scheduled), got %d", ts.Pending())
	}

	missing := ts.Missing(0)
	if len(missing) != 1 || missing[0] != childHash {
		t.Fatal("child hash should be in the missing list")
	}
}

func TestTrieSync_HealingMode_Disabled(t *testing.T) {
	db := rawdb.NewMemoryDB()
	ts := NewTrieSync(db)
	// Healing is disabled by default.

	childHash := types.BytesToHash(crypto.Keccak256([]byte("child")))

	parentData := make([]byte, 0, 64)
	parentData = append(parentData, 0xab)
	parentData = append(parentData, 0xa0)
	parentData = append(parentData, childHash[:]...)

	parentHash := types.BytesToHash(crypto.Keccak256(parentData))
	ts.AddSubTrie(parentHash, []byte{0x00}, nil)

	if err := ts.ProcessNode(parentHash, parentData); err != nil {
		t.Fatalf("ProcessNode: %v", err)
	}

	// Without healing, no children should be scheduled.
	if ts.Pending() != 0 {
		t.Fatalf("pending: want 0 (healing disabled), got %d", ts.Pending())
	}
}

func TestTrieSync_HealingMode_SkipsKnownHashes(t *testing.T) {
	db := rawdb.NewMemoryDB()
	ts := NewTrieSync(db)
	ts.SetHealing(true)

	// Pre-populate the child in the database.
	childData := make([]byte, 32)
	for i := range childData {
		childData[i] = byte(i + 1)
	}
	childHash := types.BytesToHash(crypto.Keccak256(childData))
	key := trieNodeKey(childHash)
	if err := db.Put(key, childData); err != nil {
		t.Fatal(err)
	}

	// Build parent that references the child.
	parentData := make([]byte, 0, 64)
	parentData = append(parentData, 0xab)
	parentData = append(parentData, 0xa0)
	parentData = append(parentData, childHash[:]...)

	parentHash := types.BytesToHash(crypto.Keccak256(parentData))
	ts.AddSubTrie(parentHash, []byte{0x00}, nil)

	if err := ts.ProcessNode(parentHash, parentData); err != nil {
		t.Fatalf("ProcessNode: %v", err)
	}

	// Child is already in the db, so it should not be scheduled.
	if ts.Pending() != 0 {
		t.Fatalf("pending: want 0 (child already in db), got %d", ts.Pending())
	}
}

// --- Full process-commit cycle ---

func TestTrieSync_FullCycle(t *testing.T) {
	db := rawdb.NewMemoryDB()
	ts := NewTrieSync(db)

	// Schedule 5 nodes and 2 codes.
	type item struct {
		hash types.Hash
		data []byte
	}
	var nodes []item
	for i := byte(0); i < 5; i++ {
		h, d := makeNodeData(i + 10)
		nodes = append(nodes, item{h, d})
		ts.AddSubTrie(h, []byte{i}, nil)
	}

	code1 := []byte{0x60, 0x40, 0x52}
	code2 := []byte{0x60, 0x80, 0x60, 0x40, 0x52}
	codeHash1 := types.BytesToHash(crypto.Keccak256(code1))
	codeHash2 := types.BytesToHash(crypto.Keccak256(code2))
	ts.AddCodeEntry(codeHash1)
	ts.AddCodeEntry(codeHash2)

	if ts.Pending() != 5 {
		t.Fatalf("pending nodes: want 5, got %d", ts.Pending())
	}
	if ts.PendingCodes() != 2 {
		t.Fatalf("pending codes: want 2, got %d", ts.PendingCodes())
	}

	// Process all.
	for _, n := range nodes {
		if err := ts.ProcessNode(n.hash, n.data); err != nil {
			t.Fatalf("ProcessNode(%s): %v", n.hash.Hex(), err)
		}
	}
	if err := ts.ProcessCode(codeHash1, code1); err != nil {
		t.Fatal(err)
	}
	if err := ts.ProcessCode(codeHash2, code2); err != nil {
		t.Fatal(err)
	}

	if ts.Pending() != 0 {
		t.Fatalf("after process, pending: want 0, got %d", ts.Pending())
	}

	// Commit.
	committed, err := ts.Commit()
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if committed != 7 {
		t.Fatalf("committed: want 7, got %d", committed)
	}

	// Verify all items are in the database.
	for _, n := range nodes {
		key := trieNodeKey(n.hash)
		if _, err := db.Get(key); err != nil {
			t.Fatalf("node %s not in db: %v", n.hash.Hex(), err)
		}
	}
	for _, ch := range []types.Hash{codeHash1, codeHash2} {
		key := codeKey(ch)
		if _, err := db.Get(key); err != nil {
			t.Fatalf("code %s not in db: %v", ch.Hex(), err)
		}
	}
}

// --- Database key tests ---

func TestTrieNodeKey(t *testing.T) {
	hash := types.Hash{0x01, 0x02}
	key := trieNodeKey(hash)

	if key[0] != 't' {
		t.Fatalf("prefix: want 't', got %c", key[0])
	}
	if len(key) != 1+types.HashLength {
		t.Fatalf("key length: want %d, got %d", 1+types.HashLength, len(key))
	}
}

func TestCodeKey(t *testing.T) {
	hash := types.Hash{0xaa, 0xbb}
	key := codeKey(hash)

	if key[0] != 'C' {
		t.Fatalf("prefix: want 'C', got %c", key[0])
	}
	if len(key) != 1+types.HashLength {
		t.Fatalf("key length: want %d, got %d", 1+types.HashLength, len(key))
	}
}
