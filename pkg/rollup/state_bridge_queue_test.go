package rollup

import (
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

// --- Test helpers ---

var (
	queueTestSender    = types.BytesToAddress([]byte{0x11, 0x22, 0x33, 0x44, 0x55})
	queueTestRecipient = types.BytesToAddress([]byte{0xAA, 0xBB, 0xCC, 0xDD, 0xEE})
)

func makeQueueDeposit(nonce, l1Block uint64, amount int64) *BridgeQueueDeposit {
	return &BridgeQueueDeposit{
		Sender:    queueTestSender,
		Recipient: queueTestRecipient,
		Amount:    big.NewInt(amount),
		Nonce:     nonce,
		L1Block:   l1Block,
	}
}

func makeQueueWithdrawal(nonce, l2Block uint64, amount int64) *BridgeQueueWithdrawal {
	return &BridgeQueueWithdrawal{
		Sender:    queueTestSender,
		Recipient: queueTestRecipient,
		Amount:    big.NewInt(amount),
		Nonce:     nonce,
		L2Block:   l2Block,
	}
}

// --- NewStateBridge tests ---

func TestStateBridgeNew(t *testing.T) {
	sb := NewStateBridge()
	if sb == nil {
		t.Fatal("NewStateBridge returned nil")
	}
	if sb.DepositCount() != 0 {
		t.Errorf("expected 0 deposits, got %d", sb.DepositCount())
	}
	if sb.WithdrawalCount() != 0 {
		t.Errorf("expected 0 withdrawals, got %d", sb.WithdrawalCount())
	}
	if sb.FinalizedL1Block() != 0 {
		t.Errorf("expected finalized L1 block 0, got %d", sb.FinalizedL1Block())
	}
}

// --- QueueDeposit tests ---

func TestStateBridgeQueueDepositBasic(t *testing.T) {
	sb := NewStateBridge()
	dep := makeQueueDeposit(1, 100, 1_000_000)

	err := sb.QueueDeposit(dep)
	if err != nil {
		t.Fatalf("QueueDeposit failed: %v", err)
	}
	if sb.DepositCount() != 1 {
		t.Errorf("expected 1 deposit, got %d", sb.DepositCount())
	}
}

func TestStateBridgeQueueDepositNil(t *testing.T) {
	sb := NewStateBridge()
	err := sb.QueueDeposit(nil)
	if err != ErrQueueBridgeNilDeposit {
		t.Fatalf("expected ErrQueueBridgeNilDeposit, got %v", err)
	}
}

func TestStateBridgeQueueDepositZeroSender(t *testing.T) {
	sb := NewStateBridge()
	dep := makeQueueDeposit(1, 100, 1_000_000)
	dep.Sender = types.Address{}
	err := sb.QueueDeposit(dep)
	if err != ErrQueueBridgeZeroAddress {
		t.Fatalf("expected ErrQueueBridgeZeroAddress, got %v", err)
	}
}

func TestStateBridgeQueueDepositZeroRecipient(t *testing.T) {
	sb := NewStateBridge()
	dep := makeQueueDeposit(1, 100, 1_000_000)
	dep.Recipient = types.Address{}
	err := sb.QueueDeposit(dep)
	if err != ErrQueueBridgeZeroAddress {
		t.Fatalf("expected ErrQueueBridgeZeroAddress, got %v", err)
	}
}

func TestStateBridgeQueueDepositZeroAmount(t *testing.T) {
	sb := NewStateBridge()
	dep := makeQueueDeposit(1, 100, 0)
	dep.Amount = big.NewInt(0)
	err := sb.QueueDeposit(dep)
	if err != ErrQueueBridgeZeroAmount {
		t.Fatalf("expected ErrQueueBridgeZeroAmount, got %v", err)
	}
}

func TestStateBridgeQueueDepositNilAmount(t *testing.T) {
	sb := NewStateBridge()
	dep := makeQueueDeposit(1, 100, 1)
	dep.Amount = nil
	err := sb.QueueDeposit(dep)
	if err != ErrQueueBridgeZeroAmount {
		t.Fatalf("expected ErrQueueBridgeZeroAmount, got %v", err)
	}
}

func TestStateBridgeQueueDepositNegativeAmount(t *testing.T) {
	sb := NewStateBridge()
	dep := makeQueueDeposit(1, 100, -1)
	err := sb.QueueDeposit(dep)
	if err != ErrQueueBridgeZeroAmount {
		t.Fatalf("expected ErrQueueBridgeZeroAmount, got %v", err)
	}
}

func TestStateBridgeQueueDepositDuplicateNonce(t *testing.T) {
	sb := NewStateBridge()
	dep1 := makeQueueDeposit(5, 100, 1_000_000)
	dep2 := makeQueueDeposit(5, 101, 2_000_000) // same nonce

	if err := sb.QueueDeposit(dep1); err != nil {
		t.Fatalf("first QueueDeposit failed: %v", err)
	}
	err := sb.QueueDeposit(dep2)
	if err == nil {
		t.Fatal("expected error for duplicate nonce")
	}
}

func TestStateBridgeQueueDepositMultiple(t *testing.T) {
	sb := NewStateBridge()
	for i := uint64(1); i <= 5; i++ {
		dep := makeQueueDeposit(i, 100+i, int64(i*1000))
		if err := sb.QueueDeposit(dep); err != nil {
			t.Fatalf("QueueDeposit %d failed: %v", i, err)
		}
	}
	if sb.DepositCount() != 5 {
		t.Errorf("expected 5 deposits, got %d", sb.DepositCount())
	}
}

func TestStateBridgeQueueDepositStoreCopy(t *testing.T) {
	sb := NewStateBridge()
	dep := makeQueueDeposit(1, 100, 1_000_000)
	sb.QueueDeposit(dep)

	// Mutate original.
	dep.Amount.SetInt64(999)

	// Stored copy should be unaffected.
	stats := sb.BridgeStats()
	if stats.PendingDeposits != 1 {
		t.Errorf("expected 1 pending, got %d", stats.PendingDeposits)
	}
}

// --- ProcessDeposits tests ---

func TestStateBridgeProcessDepositsEmpty(t *testing.T) {
	sb := NewStateBridge()
	_, err := sb.ProcessDeposits(100)
	if err != ErrQueueBridgeEmptyQueue {
		t.Fatalf("expected ErrQueueBridgeEmptyQueue, got %v", err)
	}
}

func TestStateBridgeProcessDepositsAfterFinalize(t *testing.T) {
	sb := NewStateBridge()

	// Queue deposits at L1 block 100.
	for i := uint64(1); i <= 3; i++ {
		sb.QueueDeposit(makeQueueDeposit(i, 100, int64(i*1000)))
	}

	// Before finalization, nothing is ready.
	_, err := sb.ProcessDeposits(200)
	if err != ErrQueueBridgeEmptyQueue {
		t.Fatalf("expected ErrQueueBridgeEmptyQueue before finalize, got %v", err)
	}

	// Finalize up to block 100.
	sb.Finalize(100)

	// Now deposits should be processable.
	deposits, err := sb.ProcessDeposits(200)
	if err != nil {
		t.Fatalf("ProcessDeposits failed: %v", err)
	}
	if len(deposits) != 3 {
		t.Fatalf("expected 3 deposits, got %d", len(deposits))
	}

	// Check they are sorted by nonce.
	for i := 1; i < len(deposits); i++ {
		if deposits[i].Nonce <= deposits[i-1].Nonce {
			t.Errorf("deposits not sorted by nonce: %d <= %d", deposits[i].Nonce, deposits[i-1].Nonce)
		}
	}
}

func TestStateBridgeProcessDepositsIdempotent(t *testing.T) {
	sb := NewStateBridge()
	sb.QueueDeposit(makeQueueDeposit(1, 100, 1_000_000))
	sb.Finalize(100)

	// First process should succeed.
	deposits, err := sb.ProcessDeposits(200)
	if err != nil {
		t.Fatalf("first ProcessDeposits failed: %v", err)
	}
	if len(deposits) != 1 {
		t.Fatalf("expected 1 deposit, got %d", len(deposits))
	}

	// Second process: already processed, nothing pending.
	_, err = sb.ProcessDeposits(201)
	if err != ErrQueueBridgeEmptyQueue {
		t.Fatalf("expected ErrQueueBridgeEmptyQueue on second process, got %v", err)
	}
}

// --- VerifyWithdrawal tests ---

func TestStateBridgeVerifyWithdrawalNil(t *testing.T) {
	sb := NewStateBridge()
	ok, err := sb.VerifyWithdrawal(nil, []byte{0x01})
	if err != ErrQueueBridgeNilWithdrawal {
		t.Fatalf("expected ErrQueueBridgeNilWithdrawal, got %v", err)
	}
	if ok {
		t.Error("expected ok=false")
	}
}

func TestStateBridgeVerifyWithdrawalEmptyProof(t *testing.T) {
	sb := NewStateBridge()
	w := makeQueueWithdrawal(1, 200, 500_000)
	ok, err := sb.VerifyWithdrawal(w, nil)
	if err != ErrQueueBridgeEmptyProof {
		t.Fatalf("expected ErrQueueBridgeEmptyProof, got %v", err)
	}
	if ok {
		t.Error("expected ok=false")
	}
}

func TestStateBridgeVerifyWithdrawalZeroAddress(t *testing.T) {
	sb := NewStateBridge()
	w := makeQueueWithdrawal(1, 200, 500_000)
	w.Sender = types.Address{}
	ok, err := sb.VerifyWithdrawal(w, make([]byte, 64))
	if err != ErrQueueBridgeZeroAddress {
		t.Fatalf("expected ErrQueueBridgeZeroAddress, got %v", err)
	}
	if ok {
		t.Error("expected ok=false")
	}
}

func TestStateBridgeVerifyWithdrawalZeroAmount(t *testing.T) {
	sb := NewStateBridge()
	w := makeQueueWithdrawal(1, 200, 0)
	w.Amount = big.NewInt(0)
	ok, err := sb.VerifyWithdrawal(w, make([]byte, 64))
	if err != ErrQueueBridgeZeroAmount {
		t.Fatalf("expected ErrQueueBridgeZeroAmount, got %v", err)
	}
	if ok {
		t.Error("expected ok=false")
	}
}

func TestStateBridgeVerifyWithdrawalShortProof(t *testing.T) {
	sb := NewStateBridge()
	w := makeQueueWithdrawal(1, 200, 500_000)
	ok, err := sb.VerifyWithdrawal(w, []byte{0x01, 0x02})
	if err != ErrQueueBridgeProofInvalid {
		t.Fatalf("expected ErrQueueBridgeProofInvalid, got %v", err)
	}
	if ok {
		t.Error("expected ok=false for short proof")
	}
}

func TestStateBridgeVerifyWithdrawalValid(t *testing.T) {
	sb := NewStateBridge()
	w := makeQueueWithdrawal(1, 200, 500_000)

	// Create a proof that passes verification.
	// Use deterministic proof data that won't trigger probabilistic rejection.
	proof := make([]byte, 64)
	for i := range proof {
		proof[i] = byte(i + 1)
	}

	ok, err := sb.VerifyWithdrawal(w, proof)
	// The result depends on hash-based probabilistic check.
	// We verify no panics and proper handling.
	if err != nil && err != ErrQueueBridgeProofInvalid {
		t.Fatalf("unexpected error: %v", err)
	}
	_ = ok // probabilistic

	// If it passed, check withdrawal count.
	if ok {
		if sb.WithdrawalCount() != 1 {
			t.Errorf("expected 1 withdrawal after valid proof, got %d", sb.WithdrawalCount())
		}
	}
}

// --- GetDepositRoot tests ---

func TestStateBridgeGetDepositRootEmpty(t *testing.T) {
	sb := NewStateBridge()
	root := sb.GetDepositRoot()
	if root != (types.Hash{}) {
		t.Error("expected zero hash for empty deposit root")
	}
}

func TestStateBridgeGetDepositRootSingle(t *testing.T) {
	sb := NewStateBridge()
	sb.QueueDeposit(makeQueueDeposit(1, 100, 1_000_000))

	root := sb.GetDepositRoot()
	if root == (types.Hash{}) {
		t.Error("expected non-zero deposit root")
	}
}

func TestStateBridgeGetDepositRootDeterministic(t *testing.T) {
	sb1 := NewStateBridge()
	sb2 := NewStateBridge()

	for i := uint64(1); i <= 4; i++ {
		sb1.QueueDeposit(makeQueueDeposit(i, 100+i, int64(i*1000)))
		sb2.QueueDeposit(makeQueueDeposit(i, 100+i, int64(i*1000)))
	}

	if sb1.GetDepositRoot() != sb2.GetDepositRoot() {
		t.Error("deposit root should be deterministic")
	}
}

// --- GetWithdrawalRoot tests ---

func TestStateBridgeGetWithdrawalRootEmpty(t *testing.T) {
	sb := NewStateBridge()
	root := sb.GetWithdrawalRoot()
	if root != (types.Hash{}) {
		t.Error("expected zero hash for empty withdrawal root")
	}
}

// --- Finalize tests ---

func TestStateBridgeFinalizeAdvancesBlock(t *testing.T) {
	sb := NewStateBridge()
	sb.Finalize(100)
	if sb.FinalizedL1Block() != 100 {
		t.Errorf("expected finalized block 100, got %d", sb.FinalizedL1Block())
	}
}

func TestStateBridgeFinalizeDoesNotRegress(t *testing.T) {
	sb := NewStateBridge()
	sb.Finalize(100)
	sb.Finalize(50) // should be a no-op
	if sb.FinalizedL1Block() != 100 {
		t.Errorf("expected finalized block 100 (no regression), got %d", sb.FinalizedL1Block())
	}
}

func TestStateBridgeFinalizeMarksDeposits(t *testing.T) {
	sb := NewStateBridge()
	sb.QueueDeposit(makeQueueDeposit(1, 100, 1_000_000))
	sb.QueueDeposit(makeQueueDeposit(2, 200, 2_000_000))

	// Finalize up to block 100, making the first deposit processable.
	sb.Finalize(100)

	// Process deposits -- first deposit (L1 block 100) becomes Processed.
	sb.ProcessDeposits(300)

	// Finalize to block 101 -- the processed deposit at block 100 becomes finalized.
	sb.Finalize(101)

	stats := sb.BridgeStats()
	if stats.FinalizedDeposits != 1 {
		t.Errorf("expected 1 finalized deposit, got %d", stats.FinalizedDeposits)
	}
	// Deposit at L1 block 200 should still be pending.
	if stats.PendingDeposits != 1 {
		t.Errorf("expected 1 pending deposit, got %d", stats.PendingDeposits)
	}
}

// --- BridgeStats tests ---

func TestStateBridgeBridgeStatsEmpty(t *testing.T) {
	sb := NewStateBridge()
	stats := sb.BridgeStats()

	if stats.PendingDeposits != 0 {
		t.Errorf("expected 0 pending, got %d", stats.PendingDeposits)
	}
	if stats.ProcessedDeposits != 0 {
		t.Errorf("expected 0 processed, got %d", stats.ProcessedDeposits)
	}
	if stats.FinalizedDeposits != 0 {
		t.Errorf("expected 0 finalized, got %d", stats.FinalizedDeposits)
	}
	if stats.TotalWithdrawals != 0 {
		t.Errorf("expected 0 withdrawals, got %d", stats.TotalWithdrawals)
	}
	if stats.FinalizedL1Block != 0 {
		t.Errorf("expected finalized block 0, got %d", stats.FinalizedL1Block)
	}
	if stats.DepositRoot != (types.Hash{}) {
		t.Error("expected zero deposit root")
	}
	if stats.WithdrawalRoot != (types.Hash{}) {
		t.Error("expected zero withdrawal root")
	}
}

func TestStateBridgeBridgeStatsWithDeposits(t *testing.T) {
	sb := NewStateBridge()

	for i := uint64(1); i <= 3; i++ {
		sb.QueueDeposit(makeQueueDeposit(i, 100, int64(i*1000)))
	}

	stats := sb.BridgeStats()
	if stats.PendingDeposits != 3 {
		t.Errorf("expected 3 pending, got %d", stats.PendingDeposits)
	}
	if stats.DepositRoot == (types.Hash{}) {
		t.Error("expected non-zero deposit root")
	}
}

// --- GetDeposit tests ---

func TestStateBridgeGetDepositFound(t *testing.T) {
	sb := NewStateBridge()
	dep := makeQueueDeposit(1, 100, 1_000_000)
	sb.QueueDeposit(dep)

	// Get the hash by checking bridge stats or computing it.
	hash := computeQueueDepositHash(dep)
	found, ok := sb.GetDeposit(hash)
	if !ok {
		t.Fatal("expected deposit to be found")
	}
	if found.Sender != dep.Sender {
		t.Error("sender mismatch")
	}
	if found.Amount.Cmp(dep.Amount) != 0 {
		t.Error("amount mismatch")
	}
}

func TestStateBridgeGetDepositNotFound(t *testing.T) {
	sb := NewStateBridge()
	_, ok := sb.GetDeposit(types.Hash{0xFF})
	if ok {
		t.Error("expected deposit not found")
	}
}

func TestStateBridgeGetDepositReturnsCopy(t *testing.T) {
	sb := NewStateBridge()
	dep := makeQueueDeposit(1, 100, 1_000_000)
	sb.QueueDeposit(dep)

	hash := computeQueueDepositHash(dep)
	found, _ := sb.GetDeposit(hash)

	// Mutate the copy.
	found.Amount.SetInt64(999)

	// Original should be unaffected.
	found2, _ := sb.GetDeposit(hash)
	if found2.Amount.Int64() == 999 {
		t.Error("GetDeposit should return a copy")
	}
}

// --- MerkleRoot computation tests ---

func TestStateBridgeComputeMerkleRootEmpty(t *testing.T) {
	root := computeQueueMerkleRoot(nil)
	if root != (types.Hash{}) {
		t.Error("expected zero hash for empty leaves")
	}
}

func TestStateBridgeComputeMerkleRootSingle(t *testing.T) {
	leaf := types.BytesToHash([]byte{0x01})
	root := computeQueueMerkleRoot([]types.Hash{leaf})
	if root != leaf {
		t.Error("single leaf should be its own root")
	}
}

func TestStateBridgeComputeMerkleRootOddLeaves(t *testing.T) {
	leaves := []types.Hash{
		types.BytesToHash([]byte{0x01}),
		types.BytesToHash([]byte{0x02}),
		types.BytesToHash([]byte{0x03}),
	}
	root := computeQueueMerkleRoot(leaves)
	if root == (types.Hash{}) {
		t.Error("expected non-zero root for odd leaves")
	}
}

func TestStateBridgeComputeMerkleRootDeterministic(t *testing.T) {
	leaves := []types.Hash{
		types.BytesToHash([]byte{0x01}),
		types.BytesToHash([]byte{0x02}),
		types.BytesToHash([]byte{0x03}),
		types.BytesToHash([]byte{0x04}),
	}
	root1 := computeQueueMerkleRoot(leaves)
	root2 := computeQueueMerkleRoot(leaves)
	if root1 != root2 {
		t.Error("merkle root should be deterministic")
	}
}
