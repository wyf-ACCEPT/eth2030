package sync

import (
	"errors"
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

// --- mock body source for body downloader tests ---

type bdlMockBodySource struct {
	bodies map[types.Hash]*types.Body
	failOn map[types.Hash]bool
}

func newBDLMockBodySource() *bdlMockBodySource {
	return &bdlMockBodySource{
		bodies: make(map[types.Hash]*types.Body),
		failOn: make(map[types.Hash]bool),
	}
}

func (s *bdlMockBodySource) FetchBodies(hashes []types.Hash) ([]*types.Body, error) {
	result := make([]*types.Body, len(hashes))
	for i, h := range hashes {
		if s.failOn[h] {
			return nil, errors.New("mock body fetch error")
		}
		body, ok := s.bodies[h]
		if !ok {
			body = &types.Body{} // empty body
		}
		result[i] = body
	}
	return result, nil
}

// bdlTestHeader creates a header for body downloader tests.
func bdlTestHeader(num uint64, parentHash types.Hash) *types.Header {
	return &types.Header{
		Number:     new(big.Int).SetUint64(num),
		ParentHash: parentHash,
		Difficulty: new(big.Int),
		GasLimit:   30_000_000,
		Time:       1000 + num*12,
		TxHash:     types.EmptyRootHash,
	}
}

// --- tests ---

func TestBDLNewBodyDownloader(t *testing.T) {
	config := DefaultBDLConfig()
	source := newBDLMockBodySource()
	bd := NewBodyDownloader(config, source)

	if bd == nil {
		t.Fatal("NewBodyDownloader returned nil")
	}
	if bd.PendingCount() != 0 {
		t.Errorf("PendingCount = %d, want 0", bd.PendingCount())
	}
	if bd.DownloadedCount() != 0 {
		t.Errorf("DownloadedCount = %d, want 0", bd.DownloadedCount())
	}
}

func TestBDLAddRemovePeer(t *testing.T) {
	bd := NewBodyDownloader(DefaultBDLConfig(), nil)
	source1 := newBDLMockBodySource()
	source2 := newBDLMockBodySource()

	bd.AddPeer("peer1", source1)
	bd.AddPeer("peer2", source2)

	bd.mu.Lock()
	if len(bd.peers) != 2 {
		t.Errorf("peer count = %d, want 2", len(bd.peers))
	}
	bd.mu.Unlock()

	bd.RemovePeer("peer1")
	bd.mu.Lock()
	if len(bd.peers) != 1 {
		t.Errorf("peer count = %d, want 1", len(bd.peers))
	}
	bd.mu.Unlock()
}

func TestBDLScheduleHeaders(t *testing.T) {
	source := newBDLMockBodySource()
	bd := NewBodyDownloader(DefaultBDLConfig(), source)

	h1 := bdlTestHeader(1, types.Hash{})
	h2 := bdlTestHeader(2, h1.Hash())

	bd.ScheduleHeaders([]*types.Header{h1, h2})

	if bd.PendingCount() != 2 {
		t.Errorf("PendingCount = %d, want 2", bd.PendingCount())
	}

	// Scheduling same headers again should not increase count.
	bd.ScheduleHeaders([]*types.Header{h1, h2})
	if bd.PendingCount() != 2 {
		t.Errorf("PendingCount = %d, want 2 (no duplicates)", bd.PendingCount())
	}
}

func TestBDLDownloadBodiesEmptyTx(t *testing.T) {
	source := newBDLMockBodySource()
	bd := NewBodyDownloader(DefaultBDLConfig(), source)

	h1 := bdlTestHeader(1, types.Hash{})
	h2 := bdlTestHeader(2, h1.Hash())

	// Register bodies in the mock source.
	source.bodies[h1.Hash()] = &types.Body{}
	source.bodies[h2.Hash()] = &types.Body{}

	bd.ScheduleHeaders([]*types.Header{h1, h2})

	err := bd.DownloadBodies()
	if err != nil {
		t.Fatalf("DownloadBodies failed: %v", err)
	}

	if bd.DownloadedCount() != 2 {
		t.Errorf("DownloadedCount = %d, want 2", bd.DownloadedCount())
	}
	if bd.PendingCount() != 0 {
		t.Errorf("PendingCount = %d, want 0", bd.PendingCount())
	}
}

func TestBDLDownloadBodiesClosed(t *testing.T) {
	source := newBDLMockBodySource()
	bd := NewBodyDownloader(DefaultBDLConfig(), source)

	h := bdlTestHeader(1, types.Hash{})
	bd.ScheduleHeaders([]*types.Header{h})
	bd.Close()

	err := bd.DownloadBodies()
	if !errors.Is(err, ErrBDLClosed) {
		t.Errorf("expected ErrBDLClosed, got %v", err)
	}
}

func TestBDLDownloadBodiesNoPeers(t *testing.T) {
	bd := NewBodyDownloader(DefaultBDLConfig(), nil)

	h := bdlTestHeader(1, types.Hash{})
	bd.ScheduleHeaders([]*types.Header{h})

	err := bd.DownloadBodies()
	if !errors.Is(err, ErrBDLNoPeers) {
		t.Errorf("expected ErrBDLNoPeers, got %v", err)
	}
}

func TestBDLValidateBodyTxRoot(t *testing.T) {
	bd := NewBodyDownloader(DefaultBDLConfig(), nil)

	h := &types.Header{
		Number:     big.NewInt(1),
		Difficulty: new(big.Int),
		TxHash:     types.Hash{0xab}, // Wrong tx root.
	}
	body := &types.Body{} // empty tx list -> EmptyRootHash

	err := bd.validateBody(h, body)
	if !errors.Is(err, ErrBDLTxRootMismatch) {
		t.Errorf("expected ErrBDLTxRootMismatch, got %v", err)
	}
}

func TestBDLValidateBodyWithdrawalsRoot(t *testing.T) {
	bd := NewBodyDownloader(DefaultBDLConfig(), nil)

	badRoot := types.Hash{0xbb}
	h := &types.Header{
		Number:          big.NewInt(1),
		Difficulty:      new(big.Int),
		TxHash:          types.EmptyRootHash,
		WithdrawalsHash: &badRoot, // Wrong withdrawals root.
	}
	body := &types.Body{
		Withdrawals: []*types.Withdrawal{},
	}

	err := bd.validateBody(h, body)
	if !errors.Is(err, ErrBDLWdRootMismatch) {
		t.Errorf("expected ErrBDLWdRootMismatch, got %v", err)
	}
}

func TestBDLValidateBodyCorrectWithdrawals(t *testing.T) {
	bd := NewBodyDownloader(DefaultBDLConfig(), nil)

	withdrawals := []*types.Withdrawal{
		{Index: 0, ValidatorIndex: 1, Address: types.Address{1}, Amount: 1000},
	}
	wdRoot := types.WithdrawalsRoot(withdrawals)

	h := &types.Header{
		Number:          big.NewInt(1),
		Difficulty:      new(big.Int),
		TxHash:          types.EmptyRootHash,
		WithdrawalsHash: &wdRoot,
	}
	body := &types.Body{
		Withdrawals: withdrawals,
	}

	err := bd.validateBody(h, body)
	if err != nil {
		t.Errorf("validateBody with correct withdrawals: %v", err)
	}
}

func TestBDLPriorityOrdering(t *testing.T) {
	source := newBDLMockBodySource()
	bd := NewBodyDownloader(DefaultBDLConfig(), source)

	// Schedule headers in reverse order.
	headers := make([]*types.Header, 5)
	for i := 4; i >= 0; i-- {
		headers[4-i] = bdlTestHeader(uint64(i+1), types.Hash{byte(i)})
		source.bodies[headers[4-i].Hash()] = &types.Body{}
	}
	bd.ScheduleHeaders(headers)

	// Get the next batch -- should be sorted by block number.
	batch := bd.nextBatch()
	if len(batch) == 0 {
		t.Fatal("nextBatch returned empty")
	}

	for i := 1; i < len(batch); i++ {
		if batch[i].Priority < batch[i-1].Priority {
			t.Errorf("batch[%d].Priority=%d < batch[%d].Priority=%d (not sorted)",
				i, batch[i].Priority, i-1, batch[i-1].Priority)
		}
	}
}

func TestBDLGetBody(t *testing.T) {
	source := newBDLMockBodySource()
	bd := NewBodyDownloader(DefaultBDLConfig(), source)

	h := bdlTestHeader(1, types.Hash{})
	source.bodies[h.Hash()] = &types.Body{}

	bd.ScheduleHeaders([]*types.Header{h})
	_ = bd.DownloadBodies()

	body := bd.GetBody(h.Hash())
	if body == nil {
		t.Error("GetBody returned nil for downloaded body")
	}

	// Unknown hash.
	body2 := bd.GetBody(types.Hash{0xff})
	if body2 != nil {
		t.Error("GetBody should return nil for unknown hash")
	}
}

func TestBDLProgress(t *testing.T) {
	source := newBDLMockBodySource()
	bd := NewBodyDownloader(DefaultBDLConfig(), source)

	h1 := bdlTestHeader(1, types.Hash{})
	source.bodies[h1.Hash()] = &types.Body{}

	bd.ScheduleHeaders([]*types.Header{h1})
	_ = bd.DownloadBodies()

	prog := bd.Progress()
	if prog.Downloaded == 0 {
		t.Error("Downloaded = 0, want > 0")
	}
	if prog.Validated == 0 {
		t.Error("Validated = 0, want > 0")
	}
}

func TestBDLReset(t *testing.T) {
	source := newBDLMockBodySource()
	bd := NewBodyDownloader(DefaultBDLConfig(), source)

	h := bdlTestHeader(1, types.Hash{})
	source.bodies[h.Hash()] = &types.Body{}

	bd.ScheduleHeaders([]*types.Header{h})
	_ = bd.DownloadBodies()

	bd.Reset()
	if bd.PendingCount() != 0 {
		t.Errorf("PendingCount after reset = %d, want 0", bd.PendingCount())
	}
	if bd.DownloadedCount() != 0 {
		t.Errorf("DownloadedCount after reset = %d, want 0", bd.DownloadedCount())
	}
}

func TestBDLSelectPeer(t *testing.T) {
	bd := NewBodyDownloader(DefaultBDLConfig(), nil)

	// No peers.
	bd.mu.Lock()
	p := bd.selectPeerLocked()
	bd.mu.Unlock()
	if p != nil {
		t.Error("selectPeerLocked should return nil when no peers")
	}

	// Add peers.
	s1 := newBDLMockBodySource()
	s2 := newBDLMockBodySource()
	bd.AddPeer("p1", s1)
	bd.AddPeer("p2", s2)

	bd.mu.Lock()
	bd.peers["p1"].ActiveJobs = 3
	bd.peers["p2"].ActiveJobs = 1
	p = bd.selectPeerLocked()
	bd.mu.Unlock()

	if p == nil {
		t.Fatal("selectPeerLocked returned nil")
	}
	if p.ID != "p2" {
		t.Errorf("selected peer = %s, want p2 (fewer active jobs)", p.ID)
	}
}
