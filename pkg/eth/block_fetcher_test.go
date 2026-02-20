package eth

import (
	"math/big"
	"testing"
	"time"

	"github.com/eth2028/eth2028/core/types"
)

// mockChain implements the Blockchain interface for testing.
type mockChain struct {
	blocks map[types.Hash]*types.Block
	byNum  map[uint64]*types.Block
}

func newMockChain() *mockChain {
	return &mockChain{
		blocks: make(map[types.Hash]*types.Block),
		byNum:  make(map[uint64]*types.Block),
	}
}

func (m *mockChain) CurrentBlock() *types.Block {
	return makeBlock(0)
}

func (m *mockChain) GetBlock(hash types.Hash) *types.Block {
	return m.blocks[hash]
}

func (m *mockChain) GetBlockByNumber(number uint64) *types.Block {
	return m.byNum[number]
}

func (m *mockChain) HasBlock(hash types.Hash) bool {
	_, ok := m.blocks[hash]
	return ok
}

func (m *mockChain) InsertBlock(block *types.Block) error {
	m.blocks[block.Hash()] = block
	m.byNum[block.NumberU64()] = block
	return nil
}

func (m *mockChain) Genesis() *types.Block {
	return makeBlock(0)
}

func (m *mockChain) addBlock(b *types.Block) {
	m.blocks[b.Hash()] = b
	m.byNum[b.NumberU64()] = b
}

func makeBlock(num uint64) *types.Block {
	header := &types.Header{
		Number:     new(big.Int).SetUint64(num),
		Difficulty: big.NewInt(1),
		GasLimit:   8_000_000,
	}
	return types.NewBlock(header, nil)
}

func makeBlockHash(num uint64) types.Hash {
	return makeBlock(num).Hash()
}

func TestNewBlockFetcher(t *testing.T) {
	chain := newMockChain()
	f := NewBlockFetcher(chain)
	if f == nil {
		t.Fatal("NewBlockFetcher returned nil")
	}
	if f.PendingCount() != 0 {
		t.Fatalf("expected 0 pending, got %d", f.PendingCount())
	}
}

func TestBlockFetcher_Announce(t *testing.T) {
	chain := newMockChain()
	f := NewBlockFetcher(chain)

	hash := types.HexToHash("0xabc1")
	if err := f.Announce(hash, 1, "peer1"); err != nil {
		t.Fatalf("Announce: %v", err)
	}
	if !f.IsAnnounced(hash) {
		t.Fatal("block should be announced")
	}
	if f.PendingCount() != 1 {
		t.Fatalf("expected 1 pending, got %d", f.PendingCount())
	}
}

func TestBlockFetcher_Announce_Duplicate(t *testing.T) {
	chain := newMockChain()
	f := NewBlockFetcher(chain)

	hash := types.HexToHash("0xdef2")
	f.Announce(hash, 2, "peer1")

	err := f.Announce(hash, 2, "peer2")
	if err != ErrAlreadyAnnounced {
		t.Fatalf("expected ErrAlreadyAnnounced, got %v", err)
	}
}

func TestBlockFetcher_Announce_KnownBlock(t *testing.T) {
	chain := newMockChain()
	b := makeBlock(5)
	chain.addBlock(b)

	f := NewBlockFetcher(chain)
	err := f.Announce(b.Hash(), 5, "peer1")
	if err != nil {
		t.Fatalf("Announce known block should not error, got %v", err)
	}
	// Should be marked completed, not pending.
	if f.IsAnnounced(b.Hash()) {
		t.Fatal("known block should not be in announced set")
	}
	if !f.IsCompleted(b.Hash()) {
		t.Fatal("known block should be completed")
	}
}

func TestBlockFetcher_Import(t *testing.T) {
	chain := newMockChain()
	f := NewBlockFetcher(chain)

	b := makeBlock(10)
	hash := b.Hash()
	f.Announce(hash, 10, "peer1")

	if err := f.Import(b); err != nil {
		t.Fatalf("Import: %v", err)
	}
	if f.IsAnnounced(hash) {
		t.Fatal("should not be announced after import")
	}
	if !f.IsCompleted(hash) {
		t.Fatal("should be completed after import")
	}
}

func TestBlockFetcher_FilterHeaders(t *testing.T) {
	chain := newMockChain()
	f := NewBlockFetcher(chain)

	b1 := makeBlock(1)
	b2 := makeBlock(2)
	b3 := makeBlock(3)

	f.Announce(b1.Hash(), 1, "peer1")
	f.Announce(b2.Hash(), 2, "peer1")

	// b3 is not announced.
	headers := []*types.Header{b1.Header(), b2.Header(), b3.Header()}
	matched := f.FilterHeaders(headers)
	if len(matched) != 2 {
		t.Fatalf("expected 2 matched headers, got %d", len(matched))
	}
}

func TestBlockFetcher_FilterBodies(t *testing.T) {
	chain := newMockChain()
	f := NewBlockFetcher(chain)

	h1 := types.HexToHash("0x1111")
	h2 := types.HexToHash("0x2222")
	h3 := types.HexToHash("0x3333")

	f.Announce(h1, 1, "peer1")
	f.Announce(h2, 2, "peer1")

	hashes := []types.Hash{h1, h2, h3}
	matched := f.FilterBodies(hashes)
	if len(matched) != 2 {
		t.Fatalf("expected 2 matched, got %d", len(matched))
	}
}

func TestBlockFetcher_ScheduleFetch(t *testing.T) {
	chain := newMockChain()
	f := NewBlockFetcher(chain)

	h1 := types.HexToHash("0xaa")
	h2 := types.HexToHash("0xbb")
	f.Announce(h1, 1, "peer1")
	f.Announce(h2, 2, "peer2")

	scheduled := f.ScheduleFetch()
	if len(scheduled) != 2 {
		t.Fatalf("expected 2 scheduled, got %d", len(scheduled))
	}

	// Pending should be 0, fetching should be 2.
	if f.PendingCount() != 0 {
		t.Fatalf("expected 0 pending, got %d", f.PendingCount())
	}
	if f.FetchingCount() != 2 {
		t.Fatalf("expected 2 fetching, got %d", f.FetchingCount())
	}
}

func TestBlockFetcher_ScheduleFetch_DedupAnnounced(t *testing.T) {
	chain := newMockChain()
	f := NewBlockFetcher(chain)

	h := types.HexToHash("0xcc")
	f.Announce(h, 1, "peer1")
	f.ScheduleFetch()

	// Trying to announce again while fetching should fail.
	err := f.Announce(h, 1, "peer2")
	if err != ErrAlreadyAnnounced {
		t.Fatalf("expected ErrAlreadyAnnounced, got %v", err)
	}
}

func TestBlockFetcher_FilterBodies_Fetching(t *testing.T) {
	chain := newMockChain()
	f := NewBlockFetcher(chain)

	h := types.HexToHash("0xdd")
	f.Announce(h, 1, "peer1")
	f.ScheduleFetch()

	// Should match fetching hashes too.
	matched := f.FilterBodies([]types.Hash{h})
	if len(matched) != 1 {
		t.Fatalf("expected 1 matched fetching body, got %d", len(matched))
	}
}

func TestBlockFetcher_CompletedCount(t *testing.T) {
	chain := newMockChain()
	f := NewBlockFetcher(chain)

	b := makeBlock(7)
	f.Announce(b.Hash(), 7, "peer1")
	f.Import(b)

	if f.CompletedCount() != 1 {
		t.Fatalf("expected 1 completed, got %d", f.CompletedCount())
	}
}

func TestBlockFetcher_Stop(t *testing.T) {
	chain := newMockChain()
	f := NewBlockFetcher(chain)
	f.Stop()

	err := f.Announce(types.HexToHash("0xee"), 1, "peer1")
	if err != ErrFetcherStopped {
		t.Fatalf("expected ErrFetcherStopped, got %v", err)
	}
}

func TestBlockFetcher_Stop_Import(t *testing.T) {
	chain := newMockChain()
	f := NewBlockFetcher(chain)
	f.Stop()

	err := f.Import(makeBlock(1))
	if err != ErrFetcherStopped {
		t.Fatalf("expected ErrFetcherStopped, got %v", err)
	}
}

func TestBlockFetcher_Reset(t *testing.T) {
	chain := newMockChain()
	f := NewBlockFetcher(chain)

	f.Announce(types.HexToHash("0xff"), 1, "peer1")
	f.Stop()
	f.Reset()

	if f.PendingCount() != 0 {
		t.Fatalf("expected 0 pending after reset, got %d", f.PendingCount())
	}
	// Should work after reset.
	if err := f.Announce(types.HexToHash("0xff"), 1, "peer1"); err != nil {
		t.Fatalf("Announce after reset: %v", err)
	}
}

func TestBlockFetcher_MaxAnnounces(t *testing.T) {
	chain := newMockChain()
	f := NewBlockFetcher(chain)

	// Fill up to maxAnnounces.
	for i := 0; i < maxAnnounces; i++ {
		h := types.BytesToHash(big.NewInt(int64(i + 1)).Bytes())
		if err := f.Announce(h, uint64(i+1), "peer1"); err != nil {
			t.Fatalf("Announce %d: %v", i, err)
		}
	}
	if f.PendingCount() != maxAnnounces {
		t.Fatalf("expected %d pending, got %d", maxAnnounces, f.PendingCount())
	}

	// One more should succeed (oldest evicted).
	extra := types.HexToHash("0xffffff")
	if err := f.Announce(extra, 999, "peer1"); err != nil {
		t.Fatalf("Announce overflow: %v", err)
	}
	if f.PendingCount() != maxAnnounces {
		t.Fatalf("expected %d pending after overflow, got %d", maxAnnounces, f.PendingCount())
	}
}

func TestBlockFetcher_GetAnnouncements(t *testing.T) {
	chain := newMockChain()
	f := NewBlockFetcher(chain)

	h1 := types.HexToHash("0xa1")
	h2 := types.HexToHash("0xa2")
	f.Announce(h1, 1, "peer1")
	f.Announce(h2, 2, "peer2")

	anns := f.GetAnnouncements()
	if len(anns) != 2 {
		t.Fatalf("expected 2 announcements, got %d", len(anns))
	}
}

func TestBlockFetcher_NilChain(t *testing.T) {
	// Should work with nil chain (no HasBlock check).
	f := NewBlockFetcher(nil)
	h := types.HexToHash("0xb1")
	if err := f.Announce(h, 1, "peer1"); err != nil {
		t.Fatalf("Announce with nil chain: %v", err)
	}
}

func TestBlockFetcher_ImportRemovesFromFetching(t *testing.T) {
	chain := newMockChain()
	f := NewBlockFetcher(chain)

	b := makeBlock(20)
	f.Announce(b.Hash(), 20, "peer1")
	f.ScheduleFetch()

	if !f.IsFetching(b.Hash()) {
		t.Fatal("should be fetching")
	}

	f.Import(b)
	if f.IsFetching(b.Hash()) {
		t.Fatal("should not be fetching after import")
	}
	if !f.IsCompleted(b.Hash()) {
		t.Fatal("should be completed after import")
	}
}

func TestBlockFetcher_ExpireStale(t *testing.T) {
	chain := newMockChain()
	f := NewBlockFetcher(chain)

	h := types.HexToHash("0xc1")
	f.Announce(h, 1, "peer1")

	// Manually backdate the announcement.
	f.mu.Lock()
	f.announced[h].Time = f.announced[h].Time.Add(-(announceExpiry + time.Second))
	f.mu.Unlock()

	expired := f.ExpireStale()
	if expired != 1 {
		t.Fatalf("expected 1 expired, got %d", expired)
	}
	if f.IsAnnounced(h) {
		t.Fatal("expired announcement should be removed")
	}
}

func TestBlockFetcher_FilterHeaders_Empty(t *testing.T) {
	chain := newMockChain()
	f := NewBlockFetcher(chain)
	matched := f.FilterHeaders(nil)
	if len(matched) != 0 {
		t.Fatalf("expected 0 matched, got %d", len(matched))
	}
}

func TestBlockFetcher_FilterBodies_Empty(t *testing.T) {
	chain := newMockChain()
	f := NewBlockFetcher(chain)
	matched := f.FilterBodies(nil)
	if len(matched) != 0 {
		t.Fatalf("expected 0 matched, got %d", len(matched))
	}
}

func TestBlockFetcher_ScheduleFetch_Empty(t *testing.T) {
	chain := newMockChain()
	f := NewBlockFetcher(chain)
	scheduled := f.ScheduleFetch()
	if len(scheduled) != 0 {
		t.Fatalf("expected 0 scheduled, got %d", len(scheduled))
	}
}

func TestBlockFetcher_Import_Unanounced(t *testing.T) {
	chain := newMockChain()
	f := NewBlockFetcher(chain)

	// Importing a block that was never announced should still work
	// (marks it as completed).
	b := makeBlock(99)
	if err := f.Import(b); err != nil {
		t.Fatalf("Import unannounced: %v", err)
	}
	if !f.IsCompleted(b.Hash()) {
		t.Fatal("should be completed")
	}
}
