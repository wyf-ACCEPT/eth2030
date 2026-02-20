package das

import (
	"testing"

	"github.com/eth2028/eth2028/crypto"
)

func makeTestCell(blobIdx, cellIdx int, slot uint64) CellGossipMessage {
	data := make([]byte, 64) // Small valid cell data.
	data[0] = byte(blobIdx)
	data[1] = byte(cellIdx)
	return CellGossipMessage{
		BlobIndex: blobIdx,
		CellIndex: cellIdx,
		Data:      data,
		Slot:      slot,
	}
}

func TestNewCellGossipHandler(t *testing.T) {
	handler := NewCellGossipHandler(CellGossipHandlerConfig{})
	if handler == nil {
		t.Fatal("NewCellGossipHandler returned nil")
	}
	if handler.reconstructionThreshold != ReconstructionThreshold {
		t.Errorf("default threshold = %d, want %d",
			handler.reconstructionThreshold, ReconstructionThreshold)
	}
}

func TestCellGossipHandlerOnCellReceived(t *testing.T) {
	handler := NewCellGossipHandler(CellGossipHandlerConfig{
		ReconstructionThreshold: 3,
	})

	msg := makeTestCell(0, 0, 100)
	if err := handler.OnCellReceived(msg); err != nil {
		t.Fatalf("OnCellReceived: %v", err)
	}

	if handler.ReceivedCellCount(0) != 1 {
		t.Errorf("received count = %d, want 1", handler.ReceivedCellCount(0))
	}

	stats := handler.Stats()
	if stats.CellsReceived != 1 {
		t.Errorf("CellsReceived = %d, want 1", stats.CellsReceived)
	}
	if stats.CellsValidated != 1 {
		t.Errorf("CellsValidated = %d, want 1", stats.CellsValidated)
	}
}

func TestCellGossipHandlerDuplicate(t *testing.T) {
	handler := NewCellGossipHandler(CellGossipHandlerConfig{
		ReconstructionThreshold: 3,
	})

	msg := makeTestCell(0, 0, 100)
	handler.OnCellReceived(msg)
	handler.OnCellReceived(msg) // duplicate

	if handler.ReceivedCellCount(0) != 1 {
		t.Errorf("received count = %d, want 1 (duplicate should not increase)", handler.ReceivedCellCount(0))
	}

	stats := handler.Stats()
	if stats.CellsDuplicate != 1 {
		t.Errorf("CellsDuplicate = %d, want 1", stats.CellsDuplicate)
	}
}

func TestCellGossipHandlerReconstructionReady(t *testing.T) {
	threshold := 3
	handler := NewCellGossipHandler(CellGossipHandlerConfig{
		ReconstructionThreshold: threshold,
	})

	readyEvents := make([]int, 0)
	handler.RegisterCallback(func(blobIndex int, event string) {
		if event == "ready" {
			readyEvents = append(readyEvents, blobIndex)
		}
	})

	// Add cells one by one.
	for i := 0; i < threshold-1; i++ {
		handler.OnCellReceived(makeTestCell(0, i, 100))
	}
	if handler.CheckReconstructionReady(0) {
		t.Error("should not be ready with fewer than threshold cells")
	}
	if len(readyEvents) != 0 {
		t.Error("callback should not fire before threshold")
	}

	// Add the threshold cell.
	handler.OnCellReceived(makeTestCell(0, threshold-1, 100))
	if !handler.CheckReconstructionReady(0) {
		t.Error("should be ready at threshold")
	}
	if len(readyEvents) != 1 || readyEvents[0] != 0 {
		t.Errorf("expected ready callback for blob 0, got %v", readyEvents)
	}

	// Adding more cells should not trigger another ready event.
	handler.OnCellReceived(makeTestCell(0, threshold, 100))
	if len(readyEvents) != 1 {
		t.Errorf("ready event should fire only once, got %d", len(readyEvents))
	}
}

func TestCellGossipHandlerGetMissingCells(t *testing.T) {
	handler := NewCellGossipHandler(CellGossipHandlerConfig{
		MaxCellIndex: 5,
		ReconstructionThreshold: 3,
	})

	// Add cells 0, 2, 4.
	handler.OnCellReceived(makeTestCell(0, 0, 1))
	handler.OnCellReceived(makeTestCell(0, 2, 1))
	handler.OnCellReceived(makeTestCell(0, 4, 1))

	missing := handler.GetMissingCells(0)
	expected := []int{1, 3}
	if len(missing) != len(expected) {
		t.Fatalf("missing count = %d, want %d", len(missing), len(expected))
	}
	for i, v := range missing {
		if v != expected[i] {
			t.Errorf("missing[%d] = %d, want %d", i, v, expected[i])
		}
	}

	// Untracked blob returns nil.
	if handler.GetMissingCells(99) != nil {
		t.Error("untracked blob should return nil missing cells")
	}
}

func TestCellGossipHandlerGetReceivedCells(t *testing.T) {
	handler := NewCellGossipHandler(CellGossipHandlerConfig{
		ReconstructionThreshold: 5,
	})

	// Add cells in non-sequential order.
	handler.OnCellReceived(makeTestCell(0, 3, 1))
	handler.OnCellReceived(makeTestCell(0, 1, 1))
	handler.OnCellReceived(makeTestCell(0, 5, 1))

	cells := handler.GetReceivedCells(0)
	if len(cells) != 3 {
		t.Fatalf("received cells count = %d, want 3", len(cells))
	}

	// Cells should be sorted by cell index.
	if cells[0].CellIndex != 1 || cells[1].CellIndex != 3 || cells[2].CellIndex != 5 {
		t.Errorf("cells not sorted: [%d, %d, %d]",
			cells[0].CellIndex, cells[1].CellIndex, cells[2].CellIndex)
	}
}

func TestCellGossipHandlerValidationFailure(t *testing.T) {
	handler := NewCellGossipHandler(CellGossipHandlerConfig{
		ReconstructionThreshold: 3,
	})

	// Invalid: empty data.
	msg := CellGossipMessage{BlobIndex: 0, CellIndex: 0, Data: nil}
	err := handler.OnCellReceived(msg)
	if err == nil {
		t.Error("expected validation error for empty data")
	}

	stats := handler.Stats()
	if stats.CellsRejected != 1 {
		t.Errorf("CellsRejected = %d, want 1", stats.CellsRejected)
	}

	// Invalid: blob index out of range.
	msg = CellGossipMessage{BlobIndex: MaxBlobCommitmentsPerBlock, CellIndex: 0, Data: []byte{1}}
	err = handler.OnCellReceived(msg)
	if err == nil {
		t.Error("expected validation error for blob index out of range")
	}

	// Invalid: cell index out of range.
	msg = CellGossipMessage{BlobIndex: 0, CellIndex: CellsPerExtBlob, Data: []byte{1}}
	err = handler.OnCellReceived(msg)
	if err == nil {
		t.Error("expected validation error for cell index out of range")
	}
}

func TestCellGossipHandlerBroadcast(t *testing.T) {
	handler := NewCellGossipHandler(CellGossipHandlerConfig{
		MaxBroadcastQueue: 5,
	})

	msg := makeTestCell(0, 0, 100)
	if err := handler.BroadcastCell(msg); err != nil {
		t.Fatalf("BroadcastCell: %v", err)
	}

	stats := handler.Stats()
	if stats.CellsBroadcast != 1 {
		t.Errorf("CellsBroadcast = %d, want 1", stats.CellsBroadcast)
	}

	queue := handler.DrainBroadcastQueue()
	if len(queue) != 1 {
		t.Fatalf("broadcast queue length = %d, want 1", len(queue))
	}

	// After drain, queue should be empty.
	queue2 := handler.DrainBroadcastQueue()
	if len(queue2) != 0 {
		t.Errorf("broadcast queue should be empty after drain, got %d", len(queue2))
	}
}

func TestCellGossipHandlerBroadcastQueueOverflow(t *testing.T) {
	handler := NewCellGossipHandler(CellGossipHandlerConfig{
		MaxBroadcastQueue: 3,
	})

	// Add 5 cells to a queue of size 3.
	for i := 0; i < 5; i++ {
		handler.BroadcastCell(makeTestCell(0, i, 1))
	}

	queue := handler.DrainBroadcastQueue()
	if len(queue) != 3 {
		t.Errorf("broadcast queue length = %d, want 3 (oldest dropped)", len(queue))
	}

	// The queue should contain the last 3 cells (2, 3, 4).
	if queue[0].CellIndex != 2 || queue[1].CellIndex != 3 || queue[2].CellIndex != 4 {
		t.Errorf("queue content incorrect: [%d, %d, %d]",
			queue[0].CellIndex, queue[1].CellIndex, queue[2].CellIndex)
	}
}

func TestCellGossipHandlerMarkReconstructed(t *testing.T) {
	handler := NewCellGossipHandler(CellGossipHandlerConfig{
		ReconstructionThreshold: 2,
	})

	handler.OnCellReceived(makeTestCell(0, 0, 1))
	handler.OnCellReceived(makeTestCell(0, 1, 1))

	ready := handler.ReadyBlobs()
	if len(ready) != 1 || ready[0] != 0 {
		t.Errorf("ready blobs = %v, want [0]", ready)
	}

	handler.MarkReconstructed(0)

	ready = handler.ReadyBlobs()
	if len(ready) != 0 {
		t.Errorf("ready blobs after reconstruction = %v, want []", ready)
	}

	// New cells for reconstructed blob should be silently ignored.
	err := handler.OnCellReceived(makeTestCell(0, 2, 1))
	if err != nil {
		t.Errorf("unexpected error for cell after reconstruction: %v", err)
	}
	if handler.ReceivedCellCount(0) != 2 {
		t.Errorf("cell count should not increase after reconstruction, got %d",
			handler.ReceivedCellCount(0))
	}
}

func TestCellGossipHandlerTrackedBlobs(t *testing.T) {
	handler := NewCellGossipHandler(CellGossipHandlerConfig{})

	handler.OnCellReceived(makeTestCell(2, 0, 1))
	handler.OnCellReceived(makeTestCell(0, 0, 1))
	handler.OnCellReceived(makeTestCell(1, 0, 1))

	blobs := handler.TrackedBlobs()
	if len(blobs) != 3 {
		t.Fatalf("tracked blobs = %d, want 3", len(blobs))
	}
	if blobs[0] != 0 || blobs[1] != 1 || blobs[2] != 2 {
		t.Errorf("tracked blobs not sorted: %v", blobs)
	}
}

func TestCellGossipHandlerReset(t *testing.T) {
	handler := NewCellGossipHandler(CellGossipHandlerConfig{
		ReconstructionThreshold: 2,
	})

	handler.OnCellReceived(makeTestCell(0, 0, 1))
	handler.OnCellReceived(makeTestCell(0, 1, 1))
	handler.BroadcastCell(makeTestCell(0, 0, 1))

	handler.Reset()

	if handler.ReceivedCellCount(0) != 0 {
		t.Error("cell count should be 0 after reset")
	}
	if len(handler.TrackedBlobs()) != 0 {
		t.Error("no blobs should be tracked after reset")
	}
	if len(handler.DrainBroadcastQueue()) != 0 {
		t.Error("broadcast queue should be empty after reset")
	}
	stats := handler.Stats()
	if stats.CellsReceived != 0 {
		t.Error("stats should be reset")
	}
}

func TestCellGossipHandlerClosed(t *testing.T) {
	handler := NewCellGossipHandler(CellGossipHandlerConfig{})
	handler.Close()

	err := handler.OnCellReceived(makeTestCell(0, 0, 1))
	if err != ErrGossipHandlerClosed {
		t.Errorf("expected ErrGossipHandlerClosed, got %v", err)
	}

	err = handler.BroadcastCell(makeTestCell(0, 0, 1))
	if err != ErrGossipHandlerClosed {
		t.Errorf("expected ErrGossipHandlerClosed for broadcast, got %v", err)
	}
}

func TestSimpleCellValidator(t *testing.T) {
	v := NewSimpleCellValidator()

	// Valid cell.
	valid := CellGossipMessage{
		BlobIndex: 0,
		CellIndex: 0,
		Data:      make([]byte, 64),
	}
	if !v.ValidateCell(valid) {
		t.Error("valid cell rejected")
	}

	// Invalid: negative-like blob index (as int, check bounds).
	bad := CellGossipMessage{BlobIndex: -1, CellIndex: 0, Data: []byte{1}}
	if v.ValidateCell(bad) {
		t.Error("negative blob index should be rejected")
	}

	// Invalid: cell index too high.
	bad = CellGossipMessage{BlobIndex: 0, CellIndex: CellsPerExtBlob, Data: []byte{1}}
	if v.ValidateCell(bad) {
		t.Error("cell index >= MaxCellIndex should be rejected")
	}

	// Invalid: empty data.
	bad = CellGossipMessage{BlobIndex: 0, CellIndex: 0, Data: nil}
	if v.ValidateCell(bad) {
		t.Error("empty data should be rejected")
	}

	// Invalid: data too large.
	bad = CellGossipMessage{BlobIndex: 0, CellIndex: 0, Data: make([]byte, BytesPerCell+1)}
	if v.ValidateCell(bad) {
		t.Error("oversized data should be rejected")
	}
}

func TestSimpleCellValidatorHashCheck(t *testing.T) {
	v := NewSimpleCellValidator()

	data := []byte{1, 2, 3, 4, 5, 6, 7, 8}
	expectedHash := computeKeccak256(data)

	v.ExpectedHashes = map[cellKey][32]byte{
		{blob: 0, cell: 0}: expectedHash,
	}

	// Correct hash.
	msg := CellGossipMessage{BlobIndex: 0, CellIndex: 0, Data: data}
	if !v.ValidateCell(msg) {
		t.Error("valid hash check failed")
	}

	// Wrong data -> wrong hash.
	msg.Data = []byte{9, 9, 9, 9}
	if v.ValidateCell(msg) {
		t.Error("wrong hash should be rejected")
	}
}

// computeKeccak256 is a test helper that computes Keccak256 via the crypto package.
func computeKeccak256(data []byte) [32]byte {
	return [32]byte(crypto.Keccak256Hash(data))
}

func TestComputeCellHash(t *testing.T) {
	msg1 := makeTestCell(0, 0, 100)
	msg2 := makeTestCell(0, 1, 100)

	hash1 := ComputeCellHash(msg1)
	hash2 := ComputeCellHash(msg2)

	if hash1 == hash2 {
		t.Error("different cells should produce different hashes")
	}

	// Same message should produce the same hash.
	hash1b := ComputeCellHash(msg1)
	if hash1 != hash1b {
		t.Error("same cell should produce the same hash")
	}
}
