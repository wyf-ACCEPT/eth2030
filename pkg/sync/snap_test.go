package sync

import (
	"bytes"
	"errors"
	"math/big"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

// --- Mock SnapPeer ---

// mockSnapPeer implements SnapPeer for testing.
type mockSnapPeer struct {
	mu       sync.Mutex
	id       string
	accounts []AccountData
	storage  map[types.Hash][]StorageData // keyed by account hash
	codes    map[types.Hash][]byte        // keyed by code hash
	healData map[string][]byte            // keyed by hex path

	// Error injection.
	accountErr  error
	storageErr  error
	bytecodeErr error
	healErr     error

	// Call counters.
	accountCalls  int
	storageCalls  int
	bytecodeCalls int
	healCalls     int
}

func newMockSnapPeer(id string) *mockSnapPeer {
	return &mockSnapPeer{
		id:       id,
		storage:  make(map[types.Hash][]StorageData),
		codes:    make(map[types.Hash][]byte),
		healData: make(map[string][]byte),
	}
}

func (m *mockSnapPeer) ID() string { return m.id }

func (m *mockSnapPeer) RequestAccountRange(req AccountRangeRequest) (*AccountRangeResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.accountCalls++
	if m.accountErr != nil {
		return nil, m.accountErr
	}

	var result []AccountData
	for _, acct := range m.accounts {
		if bytes.Compare(acct.Hash[:], req.Origin[:]) >= 0 &&
			bytes.Compare(acct.Hash[:], req.Limit[:]) <= 0 {
			result = append(result, acct)
		}
		if len(result) >= MaxAccountRange {
			break
		}
	}

	more := false
	if len(result) > 0 {
		last := result[len(result)-1].Hash
		for _, acct := range m.accounts {
			if bytes.Compare(acct.Hash[:], last[:]) > 0 &&
				bytes.Compare(acct.Hash[:], req.Limit[:]) <= 0 {
				more = true
				break
			}
		}
	}

	return &AccountRangeResponse{
		ID:       req.ID,
		Accounts: result,
		More:     more,
	}, nil
}

func (m *mockSnapPeer) RequestStorageRange(req StorageRangeRequest) (*StorageRangeResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.storageCalls++
	if m.storageErr != nil {
		return nil, m.storageErr
	}

	var result []StorageData
	for _, acctHash := range req.Accounts {
		slots, ok := m.storage[acctHash]
		if !ok {
			continue
		}
		for _, slot := range slots {
			if bytes.Compare(slot.SlotHash[:], req.Origin[:]) >= 0 &&
				bytes.Compare(slot.SlotHash[:], req.Limit[:]) <= 0 {
				result = append(result, slot)
			}
		}
	}

	return &StorageRangeResponse{
		ID:    req.ID,
		Slots: result,
		More:  false,
	}, nil
}

func (m *mockSnapPeer) RequestBytecodes(req BytecodeRequest) (*BytecodeResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.bytecodeCalls++
	if m.bytecodeErr != nil {
		return nil, m.bytecodeErr
	}

	var result []BytecodeData
	for _, hash := range req.Hashes {
		if code, ok := m.codes[hash]; ok {
			result = append(result, BytecodeData{Hash: hash, Code: code})
		}
	}

	return &BytecodeResponse{
		ID:    req.ID,
		Codes: result,
	}, nil
}

func (m *mockSnapPeer) RequestTrieNodes(root types.Hash, paths [][]byte) ([][]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.healCalls++
	if m.healErr != nil {
		return nil, m.healErr
	}

	result := make([][]byte, len(paths))
	for i, path := range paths {
		if data, ok := m.healData[string(path)]; ok {
			result[i] = data
		}
	}
	return result, nil
}

// --- Mock StateWriter ---

type mockStateWriter struct {
	mu       sync.Mutex
	accounts map[types.Hash]AccountData
	storage  map[string][]byte // key: accountHash + slotHash
	codes    map[types.Hash][]byte
	nodes    map[string][]byte

	// Configurable missing nodes for healing tests.
	missingPaths [][]byte
	healRounds   int // How many rounds of healing to require.
	healCalls    int
}

func newMockStateWriter() *mockStateWriter {
	return &mockStateWriter{
		accounts: make(map[types.Hash]AccountData),
		storage:  make(map[string][]byte),
		codes:    make(map[types.Hash][]byte),
		nodes:    make(map[string][]byte),
	}
}

func (w *mockStateWriter) WriteAccount(hash types.Hash, data AccountData) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.accounts[hash] = data
	return nil
}

func (w *mockStateWriter) WriteStorage(accountHash, slotHash types.Hash, value []byte) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	key := string(accountHash[:]) + string(slotHash[:])
	w.storage[key] = value
	return nil
}

func (w *mockStateWriter) WriteBytecode(hash types.Hash, code []byte) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.codes[hash] = code
	return nil
}

func (w *mockStateWriter) WriteTrieNode(path []byte, data []byte) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.nodes[string(path)] = data
	return nil
}

func (w *mockStateWriter) HasBytecode(hash types.Hash) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	_, ok := w.codes[hash]
	return ok
}

func (w *mockStateWriter) HasTrieNode(path []byte) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	_, ok := w.nodes[string(path)]
	return ok
}

func (w *mockStateWriter) MissingTrieNodes(root types.Hash, limit int) [][]byte {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.healCalls++

	// Simulate a finite number of heal rounds.
	if w.healCalls > w.healRounds {
		return nil
	}

	if len(w.missingPaths) == 0 {
		return nil
	}

	count := limit
	if count > len(w.missingPaths) {
		count = len(w.missingPaths)
	}
	return w.missingPaths[:count]
}

// --- Helper: make test accounts ---

func makeTestAccounts(n int) []AccountData {
	accounts := make([]AccountData, n)
	for i := 0; i < n; i++ {
		// Create a deterministic hash for each account.
		hashInput := []byte{byte(i >> 24), byte(i >> 16), byte(i >> 8), byte(i)}
		hash := types.BytesToHash(crypto.Keccak256(hashInput))

		accounts[i] = AccountData{
			Hash:     hash,
			Nonce:    uint64(i),
			Balance:  big.NewInt(int64(1000 * (i + 1))),
			Root:     types.EmptyRootHash,
			CodeHash: types.EmptyCodeHash,
		}
	}

	// Sort by hash to simulate trie ordering.
	sort.Slice(accounts, func(i, j int) bool {
		return bytes.Compare(accounts[i].Hash[:], accounts[j].Hash[:]) < 0
	})
	return accounts
}

// makeTestPivotHeader creates a header suitable for use as a snap sync pivot.
func makeTestPivotHeader(number uint64, root types.Hash) *types.Header {
	return &types.Header{
		Number:     new(big.Int).SetUint64(number),
		Root:       root,
		ParentHash: types.Hash{0xaa},
		Difficulty: big.NewInt(1),
		GasLimit:   30_000_000,
		Time:       1000 + number*12,
	}
}

// --- Tests ---

func TestSplitAccountRange_SingleRange(t *testing.T) {
	origin := types.Hash{}
	var limit types.Hash
	for i := range limit {
		limit[i] = 0xff
	}

	ranges := SplitAccountRange(origin, limit, 1)
	if len(ranges) != 1 {
		t.Fatalf("expected 1 range, got %d", len(ranges))
	}
	if ranges[0].Origin != origin {
		t.Fatalf("range[0].Origin: want %s, got %s", origin.Hex(), ranges[0].Origin.Hex())
	}
	if ranges[0].Limit != limit {
		t.Fatalf("range[0].Limit: want %s, got %s", limit.Hex(), ranges[0].Limit.Hex())
	}
}

func TestSplitAccountRange_MultipleRanges(t *testing.T) {
	origin := types.Hash{}
	var limit types.Hash
	for i := range limit {
		limit[i] = 0xff
	}

	ranges := SplitAccountRange(origin, limit, 4)
	if len(ranges) != 4 {
		t.Fatalf("expected 4 ranges, got %d", len(ranges))
	}

	// Verify non-overlapping: each range's origin should be past the previous limit.
	for i := 1; i < len(ranges); i++ {
		if bytes.Compare(ranges[i].Origin[:], ranges[i-1].Limit[:]) <= 0 {
			t.Errorf("range[%d].Origin %s <= range[%d].Limit %s (overlap detected)",
				i, ranges[i].Origin.Hex(), i-1, ranges[i-1].Limit.Hex())
		}
	}

	// First range starts at origin.
	if ranges[0].Origin != origin {
		t.Fatalf("first range should start at origin")
	}
	// Last range ends at limit.
	if ranges[len(ranges)-1].Limit != limit {
		t.Fatalf("last range should end at limit")
	}
}

func TestSplitAccountRange_ZeroCount(t *testing.T) {
	ranges := SplitAccountRange(types.Hash{}, types.Hash{0xff}, 0)
	if len(ranges) != 1 {
		t.Fatalf("expected 1 range for count=0, got %d", len(ranges))
	}
}

func TestMergeAccountRanges_Basic(t *testing.T) {
	a := makeTestAccounts(3)
	b := makeTestAccounts(3)

	merged := MergeAccountRanges(a, b)
	// Since a and b have the same hashes, result should deduplicate.
	if len(merged) != 3 {
		t.Fatalf("expected 3 merged accounts, got %d", len(merged))
	}

	// Verify sorted order.
	for i := 1; i < len(merged); i++ {
		if bytes.Compare(merged[i-1].Hash[:], merged[i].Hash[:]) >= 0 {
			t.Fatal("merged accounts not sorted")
		}
	}
}

func TestMergeAccountRanges_Disjoint(t *testing.T) {
	a := []AccountData{
		{Hash: types.Hash{0x01}, Balance: big.NewInt(100)},
		{Hash: types.Hash{0x02}, Balance: big.NewInt(200)},
	}
	b := []AccountData{
		{Hash: types.Hash{0x03}, Balance: big.NewInt(300)},
		{Hash: types.Hash{0x04}, Balance: big.NewInt(400)},
	}

	merged := MergeAccountRanges(a, b)
	if len(merged) != 4 {
		t.Fatalf("expected 4 merged accounts, got %d", len(merged))
	}
}

func TestMergeAccountRanges_BOverridesA(t *testing.T) {
	hash := types.Hash{0xab}
	a := []AccountData{
		{Hash: hash, Nonce: 1, Balance: big.NewInt(100)},
	}
	b := []AccountData{
		{Hash: hash, Nonce: 5, Balance: big.NewInt(999)},
	}

	merged := MergeAccountRanges(a, b)
	if len(merged) != 1 {
		t.Fatalf("expected 1 account, got %d", len(merged))
	}
	if merged[0].Nonce != 5 {
		t.Fatalf("expected nonce=5 (b overrides a), got %d", merged[0].Nonce)
	}
}

func TestMergeAccountRanges_Empty(t *testing.T) {
	merged := MergeAccountRanges(nil, nil)
	if len(merged) != 0 {
		t.Fatalf("expected 0 merged, got %d", len(merged))
	}
}

// --- Progress tracking tests ---

func TestSnapProgress_ETA(t *testing.T) {
	p := &SnapProgress{
		StartTime:     time.Now().Add(-10 * time.Second),
		AccountsTotal: 100,
		AccountsDone:  50,
	}

	eta := p.ETA()
	// Should be approximately 10 seconds (50% done in 10s => 10s remaining).
	if eta < 8*time.Second || eta > 12*time.Second {
		t.Fatalf("ETA: expected ~10s, got %v", eta)
	}
}

func TestSnapProgress_ETA_ZeroProgress(t *testing.T) {
	p := &SnapProgress{
		StartTime:     time.Now().Add(-5 * time.Second),
		AccountsTotal: 100,
		AccountsDone:  0,
	}

	eta := p.ETA()
	if eta != 0 {
		t.Fatalf("ETA with zero progress: expected 0, got %v", eta)
	}
}

func TestSnapProgress_ETA_Complete(t *testing.T) {
	p := &SnapProgress{
		StartTime:     time.Now().Add(-5 * time.Second),
		AccountsTotal: 100,
		AccountsDone:  100,
	}

	eta := p.ETA()
	if eta != 0 {
		t.Fatalf("ETA when complete: expected 0, got %v", eta)
	}
}

func TestSnapProgress_BytesTotal(t *testing.T) {
	p := &SnapProgress{
		AccountBytes: 100,
		StorageBytes: 200,
		BytecodeBytes: 300,
		HealBytes:    400,
	}

	if p.BytesTotal() != 1000 {
		t.Fatalf("BytesTotal: want 1000, got %d", p.BytesTotal())
	}
}

func TestSnapProgress_Elapsed(t *testing.T) {
	p := &SnapProgress{StartTime: time.Now().Add(-2 * time.Second)}
	elapsed := p.Elapsed()
	if elapsed < 1*time.Second || elapsed > 3*time.Second {
		t.Fatalf("Elapsed: expected ~2s, got %v", elapsed)
	}
}

func TestSnapProgress_Elapsed_NotStarted(t *testing.T) {
	p := &SnapProgress{}
	if p.Elapsed() != 0 {
		t.Fatalf("Elapsed without start: expected 0, got %v", p.Elapsed())
	}
}

func TestSnapProgress_PhaseDone(t *testing.T) {
	p := &SnapProgress{Phase: PhaseAccounts}
	if p.PhaseDone() {
		t.Fatal("should not be done in accounts phase")
	}

	p.Phase = PhaseComplete
	if !p.PhaseDone() {
		t.Fatal("should be done in complete phase")
	}
}

// --- Pivot point tests ---

func TestSelectPivot_Normal(t *testing.T) {
	pivot, err := SelectPivot(1000)
	if err != nil {
		t.Fatalf("SelectPivot(1000): %v", err)
	}
	expected := uint64(1000 - PivotOffset)
	if pivot != expected {
		t.Fatalf("pivot: want %d, got %d", expected, pivot)
	}
}

func TestSelectPivot_TooShort(t *testing.T) {
	_, err := SelectPivot(50)
	if !errors.Is(err, ErrNoPivotBlock) {
		t.Fatalf("expected ErrNoPivotBlock, got %v", err)
	}
}

func TestSelectPivot_ExactMinimum(t *testing.T) {
	pivot, err := SelectPivot(MinPivotBlock)
	if err != nil {
		t.Fatalf("SelectPivot(%d): %v", MinPivotBlock, err)
	}
	expected := uint64(MinPivotBlock - PivotOffset)
	if pivot != expected {
		t.Fatalf("pivot: want %d, got %d", expected, pivot)
	}
}

func TestSelectPivot_LargeChain(t *testing.T) {
	pivot, err := SelectPivot(20_000_000)
	if err != nil {
		t.Fatalf("SelectPivot: %v", err)
	}
	if pivot != 20_000_000-PivotOffset {
		t.Fatalf("pivot: want %d, got %d", 20_000_000-PivotOffset, pivot)
	}
}

// --- State healing detection tests ---

func TestDetectHealingNeeded_NoMissing(t *testing.T) {
	w := newMockStateWriter()
	needed := DetectHealingNeeded(w, types.Hash{0x01})
	if needed {
		t.Fatal("should not need healing when no missing nodes")
	}
}

func TestDetectHealingNeeded_HasMissing(t *testing.T) {
	w := newMockStateWriter()
	w.missingPaths = [][]byte{{0x01, 0x02}}
	w.healRounds = 10 // Allow detection.

	needed := DetectHealingNeeded(w, types.Hash{0x01})
	if !needed {
		t.Fatal("should need healing when nodes are missing")
	}
}

// --- Phase name tests ---

func TestPhaseName(t *testing.T) {
	tests := []struct {
		phase uint32
		want  string
	}{
		{PhaseIdle, "idle"},
		{PhaseAccounts, "accounts"},
		{PhaseStorage, "storage"},
		{PhaseBytecode, "bytecode"},
		{PhaseHealing, "healing"},
		{PhaseComplete, "complete"},
		{99, "unknown(99)"},
	}

	for _, tt := range tests {
		got := PhaseName(tt.phase)
		if got != tt.want {
			t.Errorf("PhaseName(%d): want %q, got %q", tt.phase, tt.want, got)
		}
	}
}

// --- SnapSyncer lifecycle tests ---

func TestSnapSyncer_NewDefault(t *testing.T) {
	w := newMockStateWriter()
	ss := NewSnapSyncer(w)
	if ss.IsRunning() {
		t.Fatal("should not be running initially")
	}
	if ss.Phase() != PhaseIdle {
		t.Fatalf("phase: want idle, got %d", ss.Phase())
	}
}

func TestSnapSyncer_SetPivot(t *testing.T) {
	w := newMockStateWriter()
	ss := NewSnapSyncer(w)

	root := types.Hash{0xde, 0xad}
	header := makeTestPivotHeader(1000, root)
	ss.SetPivot(header)

	prog := ss.Progress()
	if prog.PivotBlock != 1000 {
		t.Fatalf("pivot block: want 1000, got %d", prog.PivotBlock)
	}
	if prog.PivotRoot != root {
		t.Fatalf("pivot root: want %s, got %s", root.Hex(), prog.PivotRoot.Hex())
	}
}

func TestSnapSyncer_Cancel(t *testing.T) {
	w := newMockStateWriter()
	ss := NewSnapSyncer(w)

	// Cancel should not panic even when not started.
	ss.Cancel()
}

// --- Full download simulation tests ---

func TestSnapSyncer_DownloadAccounts(t *testing.T) {
	accounts := makeTestAccounts(10)
	peer := newMockSnapPeer("peer1")
	peer.accounts = accounts

	w := newMockStateWriter()
	ss := NewSnapSyncer(w)

	root := types.Hash{0x01}
	header := makeTestPivotHeader(200, root)
	ss.SetPivot(header)

	err := ss.Start(peer)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	// All accounts should be written.
	if len(w.accounts) != 10 {
		t.Fatalf("written accounts: want 10, got %d", len(w.accounts))
	}

	prog := ss.Progress()
	if prog.AccountsDone != 10 {
		t.Fatalf("accounts done: want 10, got %d", prog.AccountsDone)
	}
	if prog.Phase != PhaseComplete {
		t.Fatalf("phase: want complete, got %s", PhaseName(prog.Phase))
	}
}

func TestSnapSyncer_DownloadWithStorage(t *testing.T) {
	storageRoot := types.Hash{0xbb, 0xcc}
	accounts := []AccountData{
		{
			Hash:     types.Hash{0x10},
			Nonce:    1,
			Balance:  big.NewInt(100),
			Root:     storageRoot, // Non-empty storage.
			CodeHash: types.EmptyCodeHash,
		},
	}

	peer := newMockSnapPeer("peer1")
	peer.accounts = accounts
	peer.storage[types.Hash{0x10}] = []StorageData{
		{AccountHash: types.Hash{0x10}, SlotHash: types.Hash{0x01}, Value: []byte{0xaa}},
		{AccountHash: types.Hash{0x10}, SlotHash: types.Hash{0x02}, Value: []byte{0xbb}},
	}

	w := newMockStateWriter()
	ss := NewSnapSyncer(w)
	ss.SetPivot(makeTestPivotHeader(200, types.Hash{0x01}))

	err := ss.Start(peer)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Check storage was written.
	if len(w.storage) != 2 {
		t.Fatalf("written storage slots: want 2, got %d", len(w.storage))
	}

	prog := ss.Progress()
	if prog.StorageTotal != 2 {
		t.Fatalf("storage total: want 2, got %d", prog.StorageTotal)
	}
}

func TestSnapSyncer_DownloadWithBytecodes(t *testing.T) {
	code := []byte{0x60, 0x00, 0x60, 0x00, 0xfd} // PUSH1 0 PUSH1 0 REVERT
	codeHash := types.BytesToHash(crypto.Keccak256(code))

	accounts := []AccountData{
		{
			Hash:     types.Hash{0x20},
			Nonce:    1,
			Balance:  big.NewInt(100),
			Root:     types.EmptyRootHash,
			CodeHash: codeHash,
		},
	}

	peer := newMockSnapPeer("peer1")
	peer.accounts = accounts
	peer.codes[codeHash] = code

	w := newMockStateWriter()
	ss := NewSnapSyncer(w)
	ss.SetPivot(makeTestPivotHeader(200, types.Hash{0x01}))

	err := ss.Start(peer)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Check bytecode was written.
	storedCode, ok := w.codes[codeHash]
	if !ok {
		t.Fatal("bytecode not written")
	}
	if !bytes.Equal(storedCode, code) {
		t.Fatal("stored bytecode does not match")
	}

	prog := ss.Progress()
	if prog.BytecodesTotal != 1 {
		t.Fatalf("bytecodes total: want 1, got %d", prog.BytecodesTotal)
	}
}

func TestSnapSyncer_BadBytecodeHash(t *testing.T) {
	code := []byte{0x60, 0x00}
	codeHash := types.BytesToHash(crypto.Keccak256(code))

	accounts := []AccountData{
		{
			Hash:     types.Hash{0x30},
			Nonce:    1,
			Balance:  big.NewInt(100),
			Root:     types.EmptyRootHash,
			CodeHash: codeHash,
		},
	}

	peer := newMockSnapPeer("peer1")
	peer.accounts = accounts
	// Store the wrong code for this hash.
	peer.codes[codeHash] = []byte{0xff, 0xff, 0xff}

	w := newMockStateWriter()
	ss := NewSnapSyncer(w)
	ss.SetPivot(makeTestPivotHeader(200, types.Hash{0x01}))

	err := ss.Start(peer)
	if !errors.Is(err, ErrBadBytecode) {
		t.Fatalf("expected ErrBadBytecode, got %v", err)
	}
}

func TestSnapSyncer_HealingPhase(t *testing.T) {
	peer := newMockSnapPeer("peer1")
	// No accounts, but healing is needed.
	peer.healData[string([]byte{0x01})] = []byte{0xde, 0xad}
	peer.healData[string([]byte{0x02})] = []byte{0xbe, 0xef}

	w := newMockStateWriter()
	w.missingPaths = [][]byte{{0x01}, {0x02}}
	w.healRounds = 1 // Only one round of healing needed.

	ss := NewSnapSyncer(w)
	ss.SetPivot(makeTestPivotHeader(200, types.Hash{0x01}))

	err := ss.Start(peer)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Check trie nodes were written.
	if len(w.nodes) != 2 {
		t.Fatalf("healed nodes: want 2, got %d", len(w.nodes))
	}

	prog := ss.Progress()
	if prog.HealTrieNodes != 2 {
		t.Fatalf("heal trie nodes: want 2, got %d", prog.HealTrieNodes)
	}
	if prog.Phase != PhaseComplete {
		t.Fatalf("phase: want complete, got %s", PhaseName(prog.Phase))
	}
}

func TestSnapSyncer_DoubleStart(t *testing.T) {
	w := newMockStateWriter()
	ss := NewSnapSyncer(w)
	ss.SetPivot(makeTestPivotHeader(200, types.Hash{0x01}))

	peer := newMockSnapPeer("peer1")

	// Start in a goroutine so it blocks.
	// Use a slow peer to keep it running.
	slowPeer := &blockingSnapPeer{
		inner:   peer,
		blockCh: make(chan struct{}),
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- ss.Start(slowPeer)
	}()

	// Wait briefly for the goroutine to acquire the running flag.
	time.Sleep(20 * time.Millisecond)

	// Second start should fail.
	err := ss.Start(peer)
	if !errors.Is(err, ErrSnapAlreadyRunning) {
		t.Fatalf("expected ErrSnapAlreadyRunning, got %v", err)
	}

	// Unblock the first start.
	close(slowPeer.blockCh)
	<-errCh
}

// blockingSnapPeer blocks on account requests until unblocked.
type blockingSnapPeer struct {
	inner   *mockSnapPeer
	blockCh chan struct{}
}

func (p *blockingSnapPeer) ID() string { return p.inner.ID() }

func (p *blockingSnapPeer) RequestAccountRange(req AccountRangeRequest) (*AccountRangeResponse, error) {
	<-p.blockCh
	return p.inner.RequestAccountRange(req)
}

func (p *blockingSnapPeer) RequestStorageRange(req StorageRangeRequest) (*StorageRangeResponse, error) {
	return p.inner.RequestStorageRange(req)
}

func (p *blockingSnapPeer) RequestBytecodes(req BytecodeRequest) (*BytecodeResponse, error) {
	return p.inner.RequestBytecodes(req)
}

func (p *blockingSnapPeer) RequestTrieNodes(root types.Hash, paths [][]byte) ([][]byte, error) {
	return p.inner.RequestTrieNodes(root, paths)
}

// --- Error injection tests ---

func TestSnapSyncer_AccountRequestError(t *testing.T) {
	peer := newMockSnapPeer("peer1")
	peer.accountErr = errors.New("network error")

	w := newMockStateWriter()
	ss := NewSnapSyncer(w)
	ss.SetPivot(makeTestPivotHeader(200, types.Hash{0x01}))

	err := ss.Start(peer)
	if err == nil {
		t.Fatal("expected error from account request failure")
	}
}

func TestSnapSyncer_StorageRequestError(t *testing.T) {
	accounts := []AccountData{
		{
			Hash:     types.Hash{0x10},
			Nonce:    1,
			Balance:  big.NewInt(100),
			Root:     types.Hash{0xbb}, // Non-empty storage triggers storage download.
			CodeHash: types.EmptyCodeHash,
		},
	}

	peer := newMockSnapPeer("peer1")
	peer.accounts = accounts
	peer.storageErr = errors.New("storage fetch failed")

	w := newMockStateWriter()
	ss := NewSnapSyncer(w)
	ss.SetPivot(makeTestPivotHeader(200, types.Hash{0x01}))

	err := ss.Start(peer)
	if err == nil {
		t.Fatal("expected error from storage request failure")
	}
}

func TestSnapSyncer_HealRequestError(t *testing.T) {
	peer := newMockSnapPeer("peer1")
	peer.healErr = errors.New("heal failed")

	w := newMockStateWriter()
	w.missingPaths = [][]byte{{0x01}}
	w.healRounds = 5

	ss := NewSnapSyncer(w)
	ss.SetPivot(makeTestPivotHeader(200, types.Hash{0x01}))

	err := ss.Start(peer)
	if err == nil {
		t.Fatal("expected error from heal request failure")
	}
}

// --- incrementHash tests ---

func TestIncrementHash_Basic(t *testing.T) {
	h := types.Hash{0x00, 0x00}
	h[31] = 0x05
	next := incrementHash(h)
	if next[31] != 0x06 {
		t.Fatalf("increment: want 0x06, got 0x%02x", next[31])
	}
}

func TestIncrementHash_Carry(t *testing.T) {
	h := types.Hash{}
	h[31] = 0xff
	next := incrementHash(h)
	if next[31] != 0x00 || next[30] != 0x01 {
		t.Fatalf("carry: want [30]=0x01, [31]=0x00, got [30]=0x%02x, [31]=0x%02x", next[30], next[31])
	}
}

func TestIncrementHash_MaxWraps(t *testing.T) {
	var h types.Hash
	for i := range h {
		h[i] = 0xff
	}
	next := incrementHash(h)
	// Should wrap to all zeros.
	if next != (types.Hash{}) {
		t.Fatalf("max hash should wrap to zero, got %s", next.Hex())
	}
}

// --- Account range splitting internals ---

func TestInitAccountTasks(t *testing.T) {
	w := newMockStateWriter()
	ss := NewSnapSyncer(w)
	ss.initAccountTasks(4)

	if len(ss.accountTasks) != 4 {
		t.Fatalf("tasks: want 4, got %d", len(ss.accountTasks))
	}

	// Verify non-overlapping ranges.
	for i := 1; i < len(ss.accountTasks); i++ {
		prev := ss.accountTasks[i-1]
		curr := ss.accountTasks[i]
		if bytes.Compare(curr.origin[:], prev.limit[:]) <= 0 {
			t.Errorf("task[%d].origin %s <= task[%d].limit %s",
				i, curr.origin.Hex(), i-1, prev.limit.Hex())
		}
	}

	// First task starts at zero.
	if ss.accountTasks[0].origin != (types.Hash{}) {
		t.Fatalf("first task origin should be zero hash")
	}

	// Last task ends at max.
	var maxHash types.Hash
	for i := range maxHash {
		maxHash[i] = 0xff
	}
	if ss.accountTasks[len(ss.accountTasks)-1].limit != maxHash {
		t.Fatalf("last task limit should be max hash")
	}
}

func TestInitAccountTasks_Single(t *testing.T) {
	w := newMockStateWriter()
	ss := NewSnapSyncer(w)
	ss.initAccountTasks(1)

	if len(ss.accountTasks) != 1 {
		t.Fatalf("tasks: want 1, got %d", len(ss.accountTasks))
	}

	// Should cover full range.
	if ss.accountTasks[0].origin != (types.Hash{}) {
		t.Fatal("single task should start at zero")
	}
	var maxHash types.Hash
	for i := range maxHash {
		maxHash[i] = 0xff
	}
	if ss.accountTasks[0].limit != maxHash {
		t.Fatal("single task should end at max")
	}
}

func TestInitAccountTasks_Large(t *testing.T) {
	w := newMockStateWriter()
	ss := NewSnapSyncer(w)
	ss.initAccountTasks(16)

	if len(ss.accountTasks) != 16 {
		t.Fatalf("tasks: want 16, got %d", len(ss.accountTasks))
	}

	// All tasks should be non-overlapping.
	for i := 1; i < len(ss.accountTasks); i++ {
		if bytes.Compare(ss.accountTasks[i].origin[:], ss.accountTasks[i-1].limit[:]) <= 0 {
			t.Errorf("overlap at task %d", i)
		}
	}
}

// --- Verify account range tests ---

func TestVerifyAccountRange_SortedAccounts(t *testing.T) {
	accounts := makeTestAccounts(5)
	err := VerifyAccountRange(types.Hash{0x01}, accounts, nil)
	if err != nil {
		t.Fatalf("VerifyAccountRange: %v", err)
	}
}

func TestVerifyAccountRange_UnsortedAccounts(t *testing.T) {
	accounts := makeTestAccounts(3)
	// Reverse the order.
	accounts[0], accounts[2] = accounts[2], accounts[0]
	err := VerifyAccountRange(types.Hash{0x01}, accounts, nil)
	if !errors.Is(err, ErrBadAccountProof) {
		t.Fatalf("expected ErrBadAccountProof for unsorted, got %v", err)
	}
}

func TestVerifyAccountRange_Empty(t *testing.T) {
	err := VerifyAccountRange(types.Hash{0x01}, nil, nil)
	if err != nil {
		t.Fatalf("empty accounts should be valid: %v", err)
	}
}

// --- Constants and config tests ---

func TestSnapSyncConstants(t *testing.T) {
	if MaxAccountRange <= 0 {
		t.Fatal("MaxAccountRange must be positive")
	}
	if MaxStorageRange <= 0 {
		t.Fatal("MaxStorageRange must be positive")
	}
	if MaxBytecodeItems <= 0 {
		t.Fatal("MaxBytecodeItems must be positive")
	}
	if PivotOffset == 0 {
		t.Fatal("PivotOffset must be non-zero")
	}
	if MinPivotBlock <= PivotOffset {
		t.Fatal("MinPivotBlock must be greater than PivotOffset")
	}
}

// --- Full integration: accounts + storage + bytecode + healing ---

func TestSnapSyncer_FullPipeline(t *testing.T) {
	code := []byte{0x60, 0x40, 0x60, 0x00, 0x52}
	codeHash := types.BytesToHash(crypto.Keccak256(code))
	storageRoot := types.Hash{0xdd, 0xee}

	accounts := []AccountData{
		{
			Hash:     types.Hash{0x10},
			Nonce:    1,
			Balance:  big.NewInt(1000),
			Root:     types.EmptyRootHash,
			CodeHash: types.EmptyCodeHash,
		},
		{
			Hash:     types.Hash{0x20},
			Nonce:    5,
			Balance:  big.NewInt(5000),
			Root:     storageRoot,
			CodeHash: codeHash,
		},
	}

	peer := newMockSnapPeer("peer1")
	peer.accounts = accounts
	peer.codes[codeHash] = code
	peer.storage[types.Hash{0x20}] = []StorageData{
		{AccountHash: types.Hash{0x20}, SlotHash: types.Hash{0x01}, Value: []byte{0x42}},
	}
	peer.healData[string([]byte{0xab})] = []byte{0x01, 0x02, 0x03}

	w := newMockStateWriter()
	w.missingPaths = [][]byte{{0xab}}
	w.healRounds = 1

	ss := NewSnapSyncer(w)
	ss.SetPivot(makeTestPivotHeader(200, types.Hash{0x01}))

	err := ss.Start(peer)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	if ss.Phase() != PhaseComplete {
		t.Fatalf("phase: want complete, got %s", PhaseName(ss.Phase()))
	}

	prog := ss.Progress()
	if prog.AccountsDone != 2 {
		t.Fatalf("accounts: want 2, got %d", prog.AccountsDone)
	}
	if prog.StorageTotal != 1 {
		t.Fatalf("storage: want 1, got %d", prog.StorageTotal)
	}
	if prog.BytecodesTotal != 1 {
		t.Fatalf("bytecodes: want 1, got %d", prog.BytecodesTotal)
	}
	if prog.HealTrieNodes != 1 {
		t.Fatalf("heal nodes: want 1, got %d", prog.HealTrieNodes)
	}
	if prog.BytesTotal() == 0 {
		t.Fatal("total bytes should be > 0")
	}
}
