package consensus

import (
	"bytes"
	"sync"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

func makePubkey(b byte) []byte {
	pk := make([]byte, DepositPubkeyLen)
	pk[0] = b
	return pk
}

func makeSig(b byte) []byte {
	sig := make([]byte, DepositSigLen)
	sig[0] = b
	return sig
}

func makeCreds(b byte) []byte {
	creds := make([]byte, DepositWithdrawalCredsLen)
	creds[0] = b
	return creds
}

func makeValidDeposit(index uint64) DepositEntry {
	return DepositEntry{
		Index:                 index,
		Pubkey:                makePubkey(byte(index + 1)),
		WithdrawalCredentials: makeCreds(byte(index + 1)),
		Amount:                32_000_000_000,
		Signature:             makeSig(byte(index + 1)),
		BlockNumber:           100 + index,
	}
}

func TestNewDepositQueue(t *testing.T) {
	cfg := DefaultDepositQueueConfig()
	dq := NewDepositQueue(cfg)
	if dq == nil {
		t.Fatal("NewDepositQueue returned nil")
	}
	if dq.PendingDeposits() != 0 {
		t.Fatalf("expected 0 pending, got %d", dq.PendingDeposits())
	}
	if dq.GetDepositCount() != 0 {
		t.Fatalf("expected 0 total, got %d", dq.GetDepositCount())
	}
}

func TestAddDepositBasic(t *testing.T) {
	cfg := DefaultDepositQueueConfig()
	dq := NewDepositQueue(cfg)

	entry := makeValidDeposit(0)
	if err := dq.AddDeposit(entry); err != nil {
		t.Fatalf("AddDeposit failed: %v", err)
	}
	if dq.PendingDeposits() != 1 {
		t.Fatalf("expected 1 pending, got %d", dq.PendingDeposits())
	}
	if dq.GetDepositCount() != 1 {
		t.Fatalf("expected count 1, got %d", dq.GetDepositCount())
	}
}

func TestAddDepositValidation(t *testing.T) {
	cfg := DefaultDepositQueueConfig()
	dq := NewDepositQueue(cfg)

	tests := []struct {
		name    string
		entry   DepositEntry
		wantErr error
	}{
		{
			name: "empty pubkey",
			entry: DepositEntry{
				Index:  0,
				Pubkey: nil,
				Amount: 32_000_000_000,
			},
			wantErr: ErrDepositQueueEmptyPubkey,
		},
		{
			name: "short pubkey",
			entry: DepositEntry{
				Index:  0,
				Pubkey: []byte{0x01, 0x02},
				Amount: 32_000_000_000,
			},
			wantErr: ErrDepositQueueInvalidPubkey,
		},
		{
			name: "zero amount",
			entry: DepositEntry{
				Index:  0,
				Pubkey: makePubkey(1),
				Amount: 0,
			},
			wantErr: ErrDepositQueueZeroAmount,
		},
		{
			name: "below minimum",
			entry: DepositEntry{
				Index:  0,
				Pubkey: makePubkey(1),
				Amount: 1_000_000_000, // 1 ETH, below 32 ETH
			},
			wantErr: ErrDepositQueueBelowMinimum,
		},
		{
			name: "above max effective",
			entry: DepositEntry{
				Index:  0,
				Pubkey: makePubkey(1),
				Amount: 3000_000_000_000, // above 2048 ETH
			},
			wantErr: ErrDepositQueueAboveMax,
		},
		{
			name: "invalid sig length",
			entry: DepositEntry{
				Index:     0,
				Pubkey:    makePubkey(1),
				Amount:    32_000_000_000,
				Signature: []byte{0x01, 0x02},
			},
			wantErr: ErrDepositQueueInvalidSig,
		},
		{
			name: "invalid creds length",
			entry: DepositEntry{
				Index:                 0,
				Pubkey:                makePubkey(1),
				Amount:                32_000_000_000,
				WithdrawalCredentials: []byte{0x01},
			},
			wantErr: ErrDepositQueueInvalidCreds,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := dq.AddDeposit(tt.entry)
			if err != tt.wantErr {
				t.Fatalf("expected %v, got %v", tt.wantErr, err)
			}
		})
	}
}

func TestAddDepositDuplicate(t *testing.T) {
	cfg := DefaultDepositQueueConfig()
	dq := NewDepositQueue(cfg)

	entry := makeValidDeposit(0)
	if err := dq.AddDeposit(entry); err != nil {
		t.Fatalf("first add failed: %v", err)
	}

	err := dq.AddDeposit(entry)
	if err != ErrDepositQueueDuplicateIndex {
		t.Fatalf("expected ErrDepositQueueDuplicateIndex, got %v", err)
	}
}

func TestValidateDepositAcceptsEmptySigAndCreds(t *testing.T) {
	cfg := DefaultDepositQueueConfig()
	dq := NewDepositQueue(cfg)

	entry := DepositEntry{
		Index:  0,
		Pubkey: makePubkey(1),
		Amount: 32_000_000_000,
		// No signature or creds -- should be valid.
	}
	if err := dq.ValidateDeposit(entry); err != nil {
		t.Fatalf("ValidateDeposit should accept empty sig/creds, got %v", err)
	}
}

func TestProcessDepositsBasic(t *testing.T) {
	cfg := DefaultDepositQueueConfig()
	dq := NewDepositQueue(cfg)

	for i := uint64(0); i < 5; i++ {
		dq.AddDeposit(makeValidDeposit(i))
	}

	// Process 3.
	result := dq.ProcessDeposits(3)
	if len(result) != 3 {
		t.Fatalf("expected 3 processed, got %d", len(result))
	}
	if dq.PendingDeposits() != 2 {
		t.Fatalf("expected 2 pending, got %d", dq.PendingDeposits())
	}
	if dq.GetDepositCount() != 5 {
		t.Fatalf("total count should remain 5, got %d", dq.GetDepositCount())
	}

	// Verify FIFO order.
	for i, d := range result {
		if d.Index != uint64(i) {
			t.Fatalf("expected index %d at position %d, got %d", i, i, d.Index)
		}
	}
}

func TestProcessDepositsCappedByConfig(t *testing.T) {
	cfg := DepositQueueConfig{
		MaxDepositsPerBlock: 2,
		MinDepositAmount:    32_000_000_000,
		MaxEffectiveBalance: 2048_000_000_000,
	}
	dq := NewDepositQueue(cfg)

	for i := uint64(0); i < 5; i++ {
		dq.AddDeposit(makeValidDeposit(i))
	}

	// Request 10 but config caps at 2.
	result := dq.ProcessDeposits(10)
	if len(result) != 2 {
		t.Fatalf("expected 2 (capped), got %d", len(result))
	}
	if dq.PendingDeposits() != 3 {
		t.Fatalf("expected 3 pending, got %d", dq.PendingDeposits())
	}
}

func TestProcessDepositsEmptyQueue(t *testing.T) {
	cfg := DefaultDepositQueueConfig()
	dq := NewDepositQueue(cfg)

	result := dq.ProcessDeposits(5)
	if result != nil {
		t.Fatalf("expected nil from empty queue, got %v", result)
	}
}

func TestProcessDepositsZeroCount(t *testing.T) {
	cfg := DefaultDepositQueueConfig()
	dq := NewDepositQueue(cfg)
	dq.AddDeposit(makeValidDeposit(0))

	result := dq.ProcessDeposits(0)
	if result != nil {
		t.Fatalf("expected nil for zero maxCount, got %v", result)
	}
	if dq.PendingDeposits() != 1 {
		t.Fatalf("expected 1 still pending, got %d", dq.PendingDeposits())
	}
}

func TestGetDepositRoot(t *testing.T) {
	cfg := DefaultDepositQueueConfig()
	dq := NewDepositQueue(cfg)

	// Empty root should be zero hash.
	root := dq.GetDepositRoot()
	if !root.IsZero() {
		t.Fatalf("expected zero hash for empty queue, got %s", root.Hex())
	}

	// Add one deposit.
	dq.AddDeposit(makeValidDeposit(0))
	root1 := dq.GetDepositRoot()
	if root1.IsZero() {
		t.Fatal("expected non-zero hash for non-empty queue")
	}

	// Add another deposit -- root should change.
	dq.AddDeposit(makeValidDeposit(1))
	root2 := dq.GetDepositRoot()
	if root2.IsZero() {
		t.Fatal("expected non-zero hash")
	}
	if root1 == root2 {
		t.Fatal("root should change when deposits are added")
	}
}

func TestGetDepositRootDeterministic(t *testing.T) {
	cfg := DefaultDepositQueueConfig()

	// Build two queues with the same deposits.
	dq1 := NewDepositQueue(cfg)
	dq2 := NewDepositQueue(cfg)

	for i := uint64(0); i < 4; i++ {
		dq1.AddDeposit(makeValidDeposit(i))
		dq2.AddDeposit(makeValidDeposit(i))
	}

	r1 := dq1.GetDepositRoot()
	r2 := dq2.GetDepositRoot()
	if r1 != r2 {
		t.Fatalf("deterministic roots should match: %s vs %s", r1.Hex(), r2.Hex())
	}
}

func TestGetDepositRootAfterProcessing(t *testing.T) {
	cfg := DefaultDepositQueueConfig()
	dq := NewDepositQueue(cfg)

	for i := uint64(0); i < 4; i++ {
		dq.AddDeposit(makeValidDeposit(i))
	}

	rootBefore := dq.GetDepositRoot()

	// Process 2 deposits.
	dq.ProcessDeposits(2)

	rootAfter := dq.GetDepositRoot()
	if rootBefore == rootAfter {
		t.Fatal("root should change after processing deposits")
	}
}

func TestDefaultDepositQueueConfig(t *testing.T) {
	cfg := DefaultDepositQueueConfig()
	if cfg.MaxDepositsPerBlock != 16 {
		t.Fatalf("unexpected MaxDepositsPerBlock: %d", cfg.MaxDepositsPerBlock)
	}
	if cfg.MinDepositAmount != 32_000_000_000 {
		t.Fatalf("unexpected MinDepositAmount: %d", cfg.MinDepositAmount)
	}
	if cfg.MaxEffectiveBalance != 2048_000_000_000 {
		t.Fatalf("unexpected MaxEffectiveBalance: %d", cfg.MaxEffectiveBalance)
	}
	expectedAddr := types.HexToAddress("0x00000000219ab540356cBB839Cbe05303d7705Fa")
	if cfg.DepositContractAddress != expectedAddr {
		t.Fatalf("unexpected DepositContractAddress: %s", cfg.DepositContractAddress.Hex())
	}
}

func TestAddDepositCopiesInput(t *testing.T) {
	cfg := DefaultDepositQueueConfig()
	dq := NewDepositQueue(cfg)

	pk := makePubkey(0xAA)
	entry := DepositEntry{
		Index:  0,
		Pubkey: pk,
		Amount: 32_000_000_000,
	}
	if err := dq.AddDeposit(entry); err != nil {
		t.Fatalf("AddDeposit failed: %v", err)
	}

	// Mutate the original pubkey.
	pk[0] = 0xFF

	// Process and verify the stored copy is unmodified.
	result := dq.ProcessDeposits(1)
	if result[0].Pubkey[0] != 0xAA {
		t.Fatal("stored pubkey was mutated -- copy semantics broken")
	}
}

func TestGetDepositCount(t *testing.T) {
	cfg := DefaultDepositQueueConfig()
	dq := NewDepositQueue(cfg)

	for i := uint64(0); i < 3; i++ {
		dq.AddDeposit(makeValidDeposit(i))
	}
	if dq.GetDepositCount() != 3 {
		t.Fatalf("expected 3, got %d", dq.GetDepositCount())
	}

	dq.ProcessDeposits(2)
	// Count should still be 3 (total ever added).
	if dq.GetDepositCount() != 3 {
		t.Fatalf("expected 3 (total), got %d", dq.GetDepositCount())
	}
}

func TestConcurrentDepositOperations(t *testing.T) {
	cfg := DepositQueueConfig{
		MaxDepositsPerBlock: 100,
		MinDepositAmount:    32_000_000_000,
		MaxEffectiveBalance: 2048_000_000_000,
	}
	dq := NewDepositQueue(cfg)

	var wg sync.WaitGroup

	// Add deposits from multiple goroutines.
	for g := 0; g < 10; g++ {
		wg.Add(1)
		go func(base int) {
			defer wg.Done()
			for i := 0; i < 20; i++ {
				idx := uint64(base*20 + i)
				entry := DepositEntry{
					Index:  idx,
					Pubkey: makePubkey(byte(idx%255 + 1)),
					Amount: 32_000_000_000,
				}
				dq.AddDeposit(entry)
			}
		}(g)
	}

	// Process concurrently.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 5; i++ {
			dq.ProcessDeposits(10)
		}
	}()

	wg.Wait()

	total := dq.GetDepositCount()
	pending := dq.PendingDeposits()
	if total != 200 {
		t.Fatalf("expected 200 total deposits, got %d", total)
	}
	if pending < 0 || pending > 200 {
		t.Fatalf("unexpected pending count: %d", pending)
	}
}

func TestProcessMultipleBatches(t *testing.T) {
	cfg := DepositQueueConfig{
		MaxDepositsPerBlock: 3,
		MinDepositAmount:    32_000_000_000,
		MaxEffectiveBalance: 2048_000_000_000,
	}
	dq := NewDepositQueue(cfg)

	for i := uint64(0); i < 10; i++ {
		dq.AddDeposit(makeValidDeposit(i))
	}

	var allProcessed []DepositEntry
	for dq.PendingDeposits() > 0 {
		batch := dq.ProcessDeposits(100) // capped to 3
		if len(batch) > 3 {
			t.Fatalf("batch exceeded max: %d", len(batch))
		}
		allProcessed = append(allProcessed, batch...)
	}

	if len(allProcessed) != 10 {
		t.Fatalf("expected 10 total processed, got %d", len(allProcessed))
	}
	// Verify order.
	for i, d := range allProcessed {
		if d.Index != uint64(i) {
			t.Fatalf("position %d: expected index %d, got %d", i, i, d.Index)
		}
	}
}

func TestMerkleRootSingleDeposit(t *testing.T) {
	cfg := DefaultDepositQueueConfig()
	dq := NewDepositQueue(cfg)

	dq.AddDeposit(makeValidDeposit(0))
	root := dq.GetDepositRoot()

	// For a single leaf, the root should be the hash of that deposit entry.
	if root.IsZero() {
		t.Fatal("single deposit root should not be zero")
	}

	// Verify it matches a manually computed hash.
	entry := makeValidDeposit(0)
	e := &DepositEntry{
		Index:                 entry.Index,
		Pubkey:                entry.Pubkey,
		WithdrawalCredentials: entry.WithdrawalCredentials,
		Amount:                entry.Amount,
		Signature:             entry.Signature,
		BlockNumber:           entry.BlockNumber,
	}
	expected := types.BytesToHash(hashDepositEntry(e))
	if !bytes.Equal(root[:], expected[:]) {
		t.Fatalf("single leaf root mismatch: %s vs %s", root.Hex(), expected.Hex())
	}
}
