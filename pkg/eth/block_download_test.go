package eth

import (
	"math/big"
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

// makeHeader creates a header with the given number and parent hash.
func makeHeader(num uint64, parentHash types.Hash) *types.Header {
	return &types.Header{
		Number:     new(big.Int).SetUint64(num),
		ParentHash: parentHash,
		Difficulty: big.NewInt(1),
		GasLimit:   8_000_000,
	}
}

// makeChainHeaders creates a sequence of headers forming a chain.
func makeChainHeaders(from, count uint64) []*types.Header {
	var headers []*types.Header
	parentHash := types.Hash{}
	for i := uint64(0); i < count; i++ {
		h := makeHeader(from+i, parentHash)
		parentHash = h.Hash()
		headers = append(headers, h)
	}
	return headers
}

func TestNewBlockDownloadManager(t *testing.T) {
	chain := newMockChain()
	dm := NewBlockDownloadManager(chain)
	if dm == nil {
		t.Fatal("NewBlockDownloadManager returned nil")
	}
	if dm.TaskCount() != 0 {
		t.Fatalf("expected 0 tasks, got %d", dm.TaskCount())
	}
	if dm.PendingCount() != 0 {
		t.Fatalf("expected 0 pending, got %d", dm.PendingCount())
	}
	if dm.ActiveCount() != 0 {
		t.Fatalf("expected 0 active, got %d", dm.ActiveCount())
	}
	if dm.ImportQueueLen() != 0 {
		t.Fatalf("expected 0 import queue, got %d", dm.ImportQueueLen())
	}
}

func TestNewBlockDownloadManager_NilChain(t *testing.T) {
	dm := NewBlockDownloadManager(nil)
	if dm == nil {
		t.Fatal("NewBlockDownloadManager with nil chain returned nil")
	}
	// Should work with nil chain.
	headers := makeChainHeaders(1, 3)
	added, err := dm.AddHeaders(headers, "peer1")
	if err != nil {
		t.Fatalf("AddHeaders with nil chain: %v", err)
	}
	if added != 3 {
		t.Fatalf("expected 3 added, got %d", added)
	}
}

func TestBlockDownloadManager_AddHeaders(t *testing.T) {
	chain := newMockChain()
	dm := NewBlockDownloadManager(chain)

	headers := makeChainHeaders(10, 5)
	added, err := dm.AddHeaders(headers, "peer1")
	if err != nil {
		t.Fatalf("AddHeaders: %v", err)
	}
	if added != 5 {
		t.Fatalf("expected 5 added, got %d", added)
	}
	if dm.TaskCount() != 5 {
		t.Fatalf("expected 5 tasks, got %d", dm.TaskCount())
	}
	if dm.PendingCount() != 5 {
		t.Fatalf("expected 5 pending, got %d", dm.PendingCount())
	}
}

func TestBlockDownloadManager_AddHeaders_Duplicate(t *testing.T) {
	chain := newMockChain()
	dm := NewBlockDownloadManager(chain)

	headers := makeChainHeaders(1, 3)
	dm.AddHeaders(headers, "peer1")

	// Adding same headers again should add 0.
	added, err := dm.AddHeaders(headers, "peer2")
	if err != nil {
		t.Fatalf("AddHeaders duplicate: %v", err)
	}
	if added != 0 {
		t.Fatalf("expected 0 added for duplicates, got %d", added)
	}
}

func TestBlockDownloadManager_AddHeaders_KnownInChain(t *testing.T) {
	chain := newMockChain()
	b := makeBlock(5)
	chain.addBlock(b)

	dm := NewBlockDownloadManager(chain)

	// Create a header with the same hash as the block in the chain.
	header := b.Header()
	added, err := dm.AddHeaders([]*types.Header{header}, "peer1")
	if err != nil {
		t.Fatalf("AddHeaders: %v", err)
	}
	if added != 0 {
		t.Fatalf("expected 0 added for chain-known block, got %d", added)
	}
}

func TestBlockDownloadManager_AddHeaders_Stopped(t *testing.T) {
	dm := NewBlockDownloadManager(nil)
	dm.Stop()

	headers := makeChainHeaders(1, 1)
	_, err := dm.AddHeaders(headers, "peer1")
	if err != ErrDownloadStopped {
		t.Fatalf("expected ErrDownloadStopped, got %v", err)
	}
}

func TestBlockDownloadManager_ScheduleBodies(t *testing.T) {
	dm := NewBlockDownloadManager(nil)

	headers := makeChainHeaders(1, 5)
	dm.AddHeaders(headers, "peer1")

	hashes := dm.ScheduleBodies()
	if len(hashes) != 5 {
		t.Fatalf("expected 5 scheduled, got %d", len(hashes))
	}
	if dm.PendingCount() != 0 {
		t.Fatalf("expected 0 pending, got %d", dm.PendingCount())
	}
	if dm.ActiveCount() != 5 {
		t.Fatalf("expected 5 active, got %d", dm.ActiveCount())
	}
}

func TestBlockDownloadManager_ScheduleBodies_Empty(t *testing.T) {
	dm := NewBlockDownloadManager(nil)
	hashes := dm.ScheduleBodies()
	if len(hashes) != 0 {
		t.Fatalf("expected 0 scheduled, got %d", len(hashes))
	}
}

func TestBlockDownloadManager_ScheduleBodies_Ordered(t *testing.T) {
	dm := NewBlockDownloadManager(nil)

	// Add headers in reverse order.
	h5 := makeHeader(5, types.Hash{})
	h3 := makeHeader(3, types.Hash{})
	h1 := makeHeader(1, types.Hash{})

	dm.AddHeaders([]*types.Header{h5, h3, h1}, "peer1")

	hashes := dm.ScheduleBodies()
	if len(hashes) != 3 {
		t.Fatalf("expected 3 scheduled, got %d", len(hashes))
	}

	// Verify they're scheduled in ascending block number order.
	task1 := dm.GetTask(hashes[0])
	task2 := dm.GetTask(hashes[1])
	task3 := dm.GetTask(hashes[2])

	if task1.Number >= task2.Number || task2.Number >= task3.Number {
		t.Fatalf("tasks not in order: %d, %d, %d", task1.Number, task2.Number, task3.Number)
	}
}

func TestBlockDownloadManager_DeliverBody(t *testing.T) {
	dm := NewBlockDownloadManager(nil)

	headers := makeChainHeaders(1, 2)
	dm.AddHeaders(headers, "peer1")
	hashes := dm.ScheduleBodies()

	body := &types.Body{}
	err := dm.DeliverBody(hashes[0], body)
	if err != nil {
		t.Fatalf("DeliverBody: %v", err)
	}

	if dm.ImportQueueLen() != 1 {
		t.Fatalf("expected 1 in import queue, got %d", dm.ImportQueueLen())
	}

	task := dm.GetTask(hashes[0])
	if task.State != DownloadComplete {
		t.Fatalf("expected DownloadComplete, got %d", task.State)
	}
}

func TestBlockDownloadManager_DeliverBody_UnknownHash(t *testing.T) {
	dm := NewBlockDownloadManager(nil)
	err := dm.DeliverBody(types.Hash{1, 2, 3}, &types.Body{})
	if err == nil {
		t.Fatal("expected error for unknown hash")
	}
}

func TestBlockDownloadManager_DeliverBody_NotActive(t *testing.T) {
	dm := NewBlockDownloadManager(nil)

	headers := makeChainHeaders(1, 1)
	dm.AddHeaders(headers, "peer1")
	hash := headers[0].Hash()

	// Task is Pending, not Active. Should fail.
	err := dm.DeliverBody(hash, &types.Body{})
	if err == nil {
		t.Fatal("expected error for non-active task")
	}
}

func TestBlockDownloadManager_DeliverBody_Stopped(t *testing.T) {
	dm := NewBlockDownloadManager(nil)
	dm.Stop()
	err := dm.DeliverBody(types.Hash{}, &types.Body{})
	if err != ErrDownloadStopped {
		t.Fatalf("expected ErrDownloadStopped, got %v", err)
	}
}

func TestBlockDownloadManager_DeliverBodies(t *testing.T) {
	dm := NewBlockDownloadManager(nil)

	headers := makeChainHeaders(1, 3)
	dm.AddHeaders(headers, "peer1")
	hashes := dm.ScheduleBodies()

	bodies := make([]*types.Body, len(hashes))
	for i := range bodies {
		bodies[i] = &types.Body{}
	}

	delivered, err := dm.DeliverBodies(hashes, bodies)
	if err != nil {
		t.Fatalf("DeliverBodies: %v", err)
	}
	if delivered != 3 {
		t.Fatalf("expected 3 delivered, got %d", delivered)
	}
	if dm.ImportQueueLen() != 3 {
		t.Fatalf("expected 3 in import queue, got %d", dm.ImportQueueLen())
	}
}

func TestBlockDownloadManager_DeliverBodies_Mismatch(t *testing.T) {
	dm := NewBlockDownloadManager(nil)
	_, err := dm.DeliverBodies(
		[]types.Hash{{1}, {2}},
		[]*types.Body{{}},
	)
	if err == nil {
		t.Fatal("expected error for hash/body count mismatch")
	}
}

func TestBlockDownloadManager_DrainImportQueue_NilChain(t *testing.T) {
	dm := NewBlockDownloadManager(nil)

	headers := makeChainHeaders(1, 3)
	dm.AddHeaders(headers, "peer1")
	hashes := dm.ScheduleBodies()

	for _, h := range hashes {
		dm.DeliverBody(h, &types.Body{})
	}

	// With nil chain, parent check is skipped (chain != nil is false).
	blocks := dm.DrainImportQueue()
	if len(blocks) != 3 {
		t.Fatalf("expected 3 blocks drained, got %d", len(blocks))
	}

	if dm.ImportQueueLen() != 0 {
		t.Fatalf("expected 0 in import queue after drain, got %d", dm.ImportQueueLen())
	}
}

func TestBlockDownloadManager_DrainImportQueue_WithChain(t *testing.T) {
	chain := newMockChain()

	// Create a chain of headers: block 10 -> 11 -> 12.
	h10 := makeHeader(10, types.Hash{})
	h11 := makeHeader(11, h10.Hash())
	h12 := makeHeader(12, h11.Hash())

	// Add block 10 to chain so it serves as the existing parent.
	b10 := types.NewBlock(h10, nil)
	chain.addBlock(b10)

	dm := NewBlockDownloadManager(chain)

	// Add headers 11 and 12.
	dm.AddHeaders([]*types.Header{h11, h12}, "peer1")
	hashes := dm.ScheduleBodies()

	for _, h := range hashes {
		dm.DeliverBody(h, &types.Body{})
	}

	// Block 11's parent (h10) exists in chain. Block 12's parent (h11)
	// will be in the "just imported" batch.
	blocks := dm.DrainImportQueue()
	if len(blocks) != 2 {
		t.Fatalf("expected 2 blocks drained, got %d", len(blocks))
	}
	if blocks[0].NumberU64() != 11 {
		t.Fatalf("expected block 11 first, got %d", blocks[0].NumberU64())
	}
	if blocks[1].NumberU64() != 12 {
		t.Fatalf("expected block 12 second, got %d", blocks[1].NumberU64())
	}
}

func TestBlockDownloadManager_DrainImportQueue_MissingParent(t *testing.T) {
	chain := newMockChain()
	dm := NewBlockDownloadManager(chain)

	// Block 100 has a parent hash that doesn't exist in chain.
	h := makeHeader(100, types.Hash{99, 99, 99})
	dm.AddHeaders([]*types.Header{h}, "peer1")
	hashes := dm.ScheduleBodies()
	dm.DeliverBody(hashes[0], &types.Body{})

	// Should not drain because parent is missing.
	blocks := dm.DrainImportQueue()
	if len(blocks) != 0 {
		t.Fatalf("expected 0 blocks (missing parent), got %d", len(blocks))
	}

	// Block should remain in import queue.
	if dm.ImportQueueLen() != 1 {
		t.Fatalf("expected 1 in import queue, got %d", dm.ImportQueueLen())
	}
}

func TestBlockDownloadManager_DrainImportQueue_Empty(t *testing.T) {
	dm := NewBlockDownloadManager(nil)
	blocks := dm.DrainImportQueue()
	if len(blocks) != 0 {
		t.Fatalf("expected 0 blocks from empty queue, got %d", len(blocks))
	}
}

func TestBlockDownloadManager_RequestHeaders(t *testing.T) {
	dm := NewBlockDownloadManager(nil)

	dm.RequestHeaders(100, 10)

	from, count := dm.NextHeaderBatch()
	if from != 100 || count != 10 {
		t.Fatalf("expected from=100, count=10, got from=%d, count=%d", from, count)
	}

	// Second call should return 0.
	from, count = dm.NextHeaderBatch()
	if from != 0 || count != 0 {
		t.Fatalf("expected empty batch, got from=%d, count=%d", from, count)
	}
}

func TestBlockDownloadManager_RequestHeaders_SkipExisting(t *testing.T) {
	dm := NewBlockDownloadManager(nil)

	// Add a header for block 5 first.
	h := makeHeader(5, types.Hash{})
	dm.AddHeaders([]*types.Header{h}, "peer1")

	// Request range 3-7: block 5 should be skipped.
	dm.RequestHeaders(3, 5)
	from, count := dm.NextHeaderBatch()
	if from != 3 {
		t.Fatalf("expected from=3, got %d", from)
	}
	// Blocks 3, 4 are contiguous from start.
	if count != 2 {
		t.Fatalf("expected count=2, got %d", count)
	}
}

func TestBlockDownloadManager_NextHeaderBatch_Empty(t *testing.T) {
	dm := NewBlockDownloadManager(nil)
	from, count := dm.NextHeaderBatch()
	if from != 0 || count != 0 {
		t.Fatalf("expected 0,0, got %d,%d", from, count)
	}
}

func TestBlockDownloadManager_NextHeaderBatch_NonContiguous(t *testing.T) {
	dm := NewBlockDownloadManager(nil)

	// Add non-contiguous block numbers.
	dm.RequestHeaders(10, 3) // 10, 11, 12
	dm.RequestHeaders(20, 2) // 20, 21

	// First batch should be 10-12 (contiguous).
	from, count := dm.NextHeaderBatch()
	if from != 10 || count != 3 {
		t.Fatalf("expected from=10, count=3, got from=%d, count=%d", from, count)
	}

	// Second batch should be 20-21.
	from, count = dm.NextHeaderBatch()
	if from != 20 || count != 2 {
		t.Fatalf("expected from=20, count=2, got from=%d, count=%d", from, count)
	}
}

func TestBlockDownloadManager_ExpireStale(t *testing.T) {
	dm := NewBlockDownloadManager(nil)

	// This test just verifies the method runs and returns 0 on fresh tasks.
	headers := makeChainHeaders(1, 3)
	dm.AddHeaders(headers, "peer1")

	expired := dm.ExpireStale()
	if expired != 0 {
		t.Fatalf("expected 0 expired for fresh tasks, got %d", expired)
	}
}

func TestBlockDownloadManager_CancelDownload(t *testing.T) {
	dm := NewBlockDownloadManager(nil)

	headers := makeChainHeaders(1, 3)
	dm.AddHeaders(headers, "peer1")

	hash := headers[1].Hash()
	dm.CancelDownload(hash)

	if dm.TaskCount() != 2 {
		t.Fatalf("expected 2 tasks after cancel, got %d", dm.TaskCount())
	}

	task := dm.GetTask(hash)
	if task != nil {
		t.Fatal("cancelled task should not be found")
	}
}

func TestBlockDownloadManager_CancelDownload_InImportQueue(t *testing.T) {
	dm := NewBlockDownloadManager(nil)

	headers := makeChainHeaders(1, 2)
	dm.AddHeaders(headers, "peer1")
	hashes := dm.ScheduleBodies()

	// Deliver body to put in import queue.
	dm.DeliverBody(hashes[0], &types.Body{})
	if dm.ImportQueueLen() != 1 {
		t.Fatalf("expected 1 in import queue, got %d", dm.ImportQueueLen())
	}

	// Cancel should remove from import queue too.
	dm.CancelDownload(hashes[0])
	if dm.ImportQueueLen() != 0 {
		t.Fatalf("expected 0 in import queue after cancel, got %d", dm.ImportQueueLen())
	}
}

func TestBlockDownloadManager_CancelDownload_NotFound(t *testing.T) {
	dm := NewBlockDownloadManager(nil)
	// Should not panic.
	dm.CancelDownload(types.Hash{0xff})
}

func TestBlockDownloadManager_IsDuplicate(t *testing.T) {
	dm := NewBlockDownloadManager(nil)

	headers := makeChainHeaders(1, 1)
	dm.AddHeaders(headers, "peer1")
	hash := headers[0].Hash()

	if !dm.IsDuplicate(hash) {
		t.Fatal("tracked hash should be duplicate")
	}

	unknown := types.Hash{0xaa, 0xbb}
	if dm.IsDuplicate(unknown) {
		t.Fatal("unknown hash should not be duplicate")
	}
}

func TestBlockDownloadManager_IsDuplicate_ChainKnown(t *testing.T) {
	chain := newMockChain()
	b := makeBlock(10)
	chain.addBlock(b)

	dm := NewBlockDownloadManager(chain)

	if !dm.IsDuplicate(b.Hash()) {
		t.Fatal("chain-known hash should be duplicate")
	}
}

func TestBlockDownloadManager_Stop(t *testing.T) {
	dm := NewBlockDownloadManager(nil)
	dm.Stop()

	headers := makeChainHeaders(1, 1)
	_, err := dm.AddHeaders(headers, "peer1")
	if err != ErrDownloadStopped {
		t.Fatalf("expected ErrDownloadStopped, got %v", err)
	}
}

func TestBlockDownloadManager_Reset(t *testing.T) {
	dm := NewBlockDownloadManager(nil)

	headers := makeChainHeaders(1, 5)
	dm.AddHeaders(headers, "peer1")
	dm.Stop()
	dm.Reset()

	if dm.TaskCount() != 0 {
		t.Fatalf("expected 0 tasks after reset, got %d", dm.TaskCount())
	}

	// Should work again after reset.
	added, err := dm.AddHeaders(headers, "peer1")
	if err != nil {
		t.Fatalf("AddHeaders after reset: %v", err)
	}
	if added != 5 {
		t.Fatalf("expected 5 added after reset, got %d", added)
	}
}

func TestBlockDownloadManager_GetTask(t *testing.T) {
	dm := NewBlockDownloadManager(nil)

	headers := makeChainHeaders(42, 1)
	dm.AddHeaders(headers, "peer1")
	hash := headers[0].Hash()

	task := dm.GetTask(hash)
	if task == nil {
		t.Fatal("GetTask returned nil")
	}
	if task.Number != 42 {
		t.Fatalf("expected number 42, got %d", task.Number)
	}
	if task.PeerID != "peer1" {
		t.Fatalf("expected peer1, got %s", task.PeerID)
	}
	if task.State != DownloadPending {
		t.Fatalf("expected DownloadPending, got %d", task.State)
	}
}

func TestBlockDownloadManager_GetTask_NotFound(t *testing.T) {
	dm := NewBlockDownloadManager(nil)
	task := dm.GetTask(types.Hash{0xff})
	if task != nil {
		t.Fatal("expected nil for missing task")
	}
}

func TestBlockDownloadManager_GetTask_ReturnsCopy(t *testing.T) {
	dm := NewBlockDownloadManager(nil)

	headers := makeChainHeaders(1, 1)
	dm.AddHeaders(headers, "peer1")
	hash := headers[0].Hash()

	task1 := dm.GetTask(hash)
	task2 := dm.GetTask(hash)

	// Modifying task1 should not affect task2.
	task1.PeerID = "modified"
	if task2.PeerID == "modified" {
		t.Fatal("GetTask should return copies")
	}
}

func TestBlockDownloadManager_FullPipeline(t *testing.T) {
	chain := newMockChain()

	// Set up a "genesis" block that is the parent for our chain.
	genesis := makeHeader(0, types.Hash{})
	genesisBlock := types.NewBlock(genesis, nil)
	chain.addBlock(genesisBlock)

	dm := NewBlockDownloadManager(chain)

	// Step 1: Request headers for blocks 1-5.
	dm.RequestHeaders(1, 5)
	from, count := dm.NextHeaderBatch()
	if from != 1 || count != 5 {
		t.Fatalf("expected from=1, count=5, got %d, %d", from, count)
	}

	// Step 2: Add headers (simulate receiving them).
	parentHash := genesisBlock.Hash()
	var headers []*types.Header
	for i := uint64(1); i <= 5; i++ {
		h := makeHeader(i, parentHash)
		headers = append(headers, h)
		parentHash = h.Hash()
	}
	added, err := dm.AddHeaders(headers, "peer1")
	if err != nil {
		t.Fatalf("AddHeaders: %v", err)
	}
	if added != 5 {
		t.Fatalf("expected 5 added, got %d", added)
	}

	// Step 3: Schedule body downloads.
	bodyHashes := dm.ScheduleBodies()
	if len(bodyHashes) != 5 {
		t.Fatalf("expected 5 body hashes, got %d", len(bodyHashes))
	}

	// Step 4: Deliver bodies.
	for _, h := range bodyHashes {
		if err := dm.DeliverBody(h, &types.Body{}); err != nil {
			t.Fatalf("DeliverBody: %v", err)
		}
	}

	// Step 5: Drain import queue.
	blocks := dm.DrainImportQueue()
	if len(blocks) != 5 {
		t.Fatalf("expected 5 blocks to import, got %d", len(blocks))
	}

	// Blocks should be in ascending order.
	for i, b := range blocks {
		expected := uint64(i + 1)
		if b.NumberU64() != expected {
			t.Fatalf("block %d: expected number %d, got %d", i, expected, b.NumberU64())
		}
	}
}

func TestBlockDownloadManager_Counters(t *testing.T) {
	dm := NewBlockDownloadManager(nil)

	headers := makeChainHeaders(1, 4)
	dm.AddHeaders(headers, "peer1")

	if dm.TaskCount() != 4 {
		t.Fatalf("expected 4 total tasks, got %d", dm.TaskCount())
	}
	if dm.PendingCount() != 4 {
		t.Fatalf("expected 4 pending, got %d", dm.PendingCount())
	}
	if dm.ActiveCount() != 0 {
		t.Fatalf("expected 0 active, got %d", dm.ActiveCount())
	}

	// Schedule 2 (all 4 will be scheduled since < maxConcurrentDownloads).
	dm.ScheduleBodies()

	if dm.PendingCount() != 0 {
		t.Fatalf("expected 0 pending after schedule, got %d", dm.PendingCount())
	}
	if dm.ActiveCount() != 4 {
		t.Fatalf("expected 4 active after schedule, got %d", dm.ActiveCount())
	}
}
