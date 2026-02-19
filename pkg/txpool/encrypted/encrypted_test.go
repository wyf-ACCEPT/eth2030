package encrypted

import (
	"math/big"
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

// helper to create a simple test transaction.
func testTx(nonce uint64, value int64) *types.Transaction {
	to := types.HexToAddress("0x1234567890abcdef1234567890abcdef12345678")
	return types.NewTransaction(&types.LegacyTx{
		Nonce:    nonce,
		GasPrice: big.NewInt(1000000000),
		Gas:      21000,
		To:       &to,
		Value:    big.NewInt(value),
	})
}

// --- ComputeCommitHash ---

func TestComputeCommitHash(t *testing.T) {
	tx := testTx(0, 1000)
	hash := ComputeCommitHash(tx)
	if hash.IsZero() {
		t.Fatal("ComputeCommitHash returned zero hash")
	}

	// Same tx should produce the same hash.
	hash2 := ComputeCommitHash(tx)
	if hash != hash2 {
		t.Error("ComputeCommitHash is not deterministic")
	}

	// Different tx should produce a different hash.
	tx2 := testTx(1, 2000)
	hash3 := ComputeCommitHash(tx2)
	if hash == hash3 {
		t.Error("Different txs produced the same commit hash")
	}
}

// --- AddCommit ---

func TestAddCommit(t *testing.T) {
	pool := NewEncryptedPool()

	commit := &CommitTx{
		CommitHash: types.HexToHash("0xaabb"),
		Sender:     types.HexToAddress("0x1234"),
		GasLimit:   21000,
		MaxFee:     big.NewInt(1000000000),
		Timestamp:  100,
	}

	if err := pool.AddCommit(commit); err != nil {
		t.Fatalf("AddCommit: %v", err)
	}

	if pool.Pending() != 1 {
		t.Errorf("Pending() = %d, want 1", pool.Pending())
	}
	if pool.Committed() != 1 {
		t.Errorf("Committed() = %d, want 1", pool.Committed())
	}
}

func TestAddCommitDuplicate(t *testing.T) {
	pool := NewEncryptedPool()

	commit := &CommitTx{
		CommitHash: types.HexToHash("0xaabb"),
		Sender:     types.HexToAddress("0x1234"),
		Timestamp:  100,
	}
	pool.AddCommit(commit)

	err := pool.AddCommit(commit)
	if err != ErrDuplicateCommit {
		t.Errorf("AddCommit duplicate: got %v, want %v", err, ErrDuplicateCommit)
	}
}

func TestAddCommitNil(t *testing.T) {
	pool := NewEncryptedPool()
	if err := pool.AddCommit(nil); err == nil {
		t.Error("AddCommit(nil) should error")
	}
}

// --- AddReveal ---

func TestAddRevealRoundtrip(t *testing.T) {
	pool := NewEncryptedPool()
	tx := testTx(0, 1000)
	commitHash := ComputeCommitHash(tx)

	// Commit.
	commit := &CommitTx{
		CommitHash: commitHash,
		Sender:     types.HexToAddress("0x1234"),
		GasLimit:   21000,
		MaxFee:     big.NewInt(1000000000),
		Timestamp:  100,
	}
	if err := pool.AddCommit(commit); err != nil {
		t.Fatalf("AddCommit: %v", err)
	}

	// Reveal.
	reveal := &RevealTx{
		CommitHash:  commitHash,
		Transaction: tx,
	}
	if err := pool.AddReveal(reveal); err != nil {
		t.Fatalf("AddReveal: %v", err)
	}

	// Check revealed transactions.
	revealed := pool.GetRevealed()
	if len(revealed) != 1 {
		t.Fatalf("GetRevealed() len = %d, want 1", len(revealed))
	}

	// Pending commits should be 0 (revealed).
	if pool.Pending() != 0 {
		t.Errorf("Pending() = %d, want 0 after reveal", pool.Pending())
	}
}

func TestAddRevealHashMismatch(t *testing.T) {
	pool := NewEncryptedPool()
	tx := testTx(0, 1000)

	// Use a wrong commit hash.
	wrongHash := types.HexToHash("0xdead")
	commit := &CommitTx{
		CommitHash: wrongHash,
		Timestamp:  100,
	}
	pool.AddCommit(commit)

	reveal := &RevealTx{
		CommitHash:  wrongHash,
		Transaction: tx,
	}
	err := pool.AddReveal(reveal)
	if err != ErrHashMismatch {
		t.Errorf("AddReveal mismatch: got %v, want %v", err, ErrHashMismatch)
	}
}

func TestAddRevealNoCommit(t *testing.T) {
	pool := NewEncryptedPool()
	tx := testTx(0, 1000)
	commitHash := ComputeCommitHash(tx)

	reveal := &RevealTx{
		CommitHash:  commitHash,
		Transaction: tx,
	}
	err := pool.AddReveal(reveal)
	if err != ErrCommitNotFound {
		t.Errorf("AddReveal no commit: got %v, want %v", err, ErrCommitNotFound)
	}
}

func TestAddRevealAlreadyRevealed(t *testing.T) {
	pool := NewEncryptedPool()
	tx := testTx(0, 1000)
	commitHash := ComputeCommitHash(tx)

	pool.AddCommit(&CommitTx{CommitHash: commitHash, Timestamp: 100})
	pool.AddReveal(&RevealTx{CommitHash: commitHash, Transaction: tx})

	// Second reveal should fail.
	err := pool.AddReveal(&RevealTx{CommitHash: commitHash, Transaction: tx})
	if err != ErrAlreadyRevealed {
		t.Errorf("AddReveal duplicate: got %v, want %v", err, ErrAlreadyRevealed)
	}
}

func TestAddRevealNil(t *testing.T) {
	pool := NewEncryptedPool()
	if err := pool.AddReveal(nil); err != ErrNilTransaction {
		t.Errorf("AddReveal(nil): got %v, want %v", err, ErrNilTransaction)
	}
	if err := pool.AddReveal(&RevealTx{}); err != ErrNilTransaction {
		t.Errorf("AddReveal with nil tx: got %v, want %v", err, ErrNilTransaction)
	}
}

// --- ExpireCommits ---

func TestExpireCommits(t *testing.T) {
	pool := NewEncryptedPool()

	// Add two commits at different times.
	pool.AddCommit(&CommitTx{
		CommitHash: types.HexToHash("0x01"),
		Timestamp:  100,
	})
	pool.AddCommit(&CommitTx{
		CommitHash: types.HexToHash("0x02"),
		Timestamp:  200,
	})

	// Expire at time 113 (deadline = 100 + 12 = 112, so 113 > 112 -> expire first).
	expired := pool.ExpireCommits(113)
	if expired != 1 {
		t.Errorf("ExpireCommits(113) = %d, want 1", expired)
	}
	if pool.Pending() != 1 {
		t.Errorf("Pending() = %d, want 1", pool.Pending())
	}

	// Expire at time 213 (deadline = 200 + 12 = 212, so 213 > 212 -> expire second).
	expired = pool.ExpireCommits(213)
	if expired != 1 {
		t.Errorf("ExpireCommits(213) = %d, want 1", expired)
	}
	if pool.Pending() != 0 {
		t.Errorf("Pending() = %d, want 0", pool.Pending())
	}
}

func TestExpireCommitsNoEffect(t *testing.T) {
	pool := NewEncryptedPool()
	pool.AddCommit(&CommitTx{
		CommitHash: types.HexToHash("0x01"),
		Timestamp:  100,
	})

	// Time is before deadline (100 + 12 = 112), should not expire.
	expired := pool.ExpireCommits(110)
	if expired != 0 {
		t.Errorf("ExpireCommits(110) = %d, want 0", expired)
	}
}

func TestRevealExpiredCommit(t *testing.T) {
	pool := NewEncryptedPool()
	tx := testTx(0, 1000)
	commitHash := ComputeCommitHash(tx)

	pool.AddCommit(&CommitTx{CommitHash: commitHash, Timestamp: 100})
	pool.ExpireCommits(200) // Expire all

	// We need to re-add the commit since expired ones are removed.
	// Try to reveal with no commit should fail.
	err := pool.AddReveal(&RevealTx{CommitHash: commitHash, Transaction: tx})
	if err != ErrCommitNotFound {
		t.Errorf("Reveal after expire: got %v, want %v", err, ErrCommitNotFound)
	}
}

// --- Ordering ---

func TestOrderByCommitTime(t *testing.T) {
	entries := []*CommitEntry{
		{Commit: &CommitTx{Timestamp: 300}},
		{Commit: &CommitTx{Timestamp: 100}},
		{Commit: &CommitTx{Timestamp: 200}},
	}

	sorted := OrderByCommitTime(entries)
	if len(sorted) != 3 {
		t.Fatalf("sorted len = %d, want 3", len(sorted))
	}
	if sorted[0].Commit.Timestamp != 100 {
		t.Errorf("sorted[0].Timestamp = %d, want 100", sorted[0].Commit.Timestamp)
	}
	if sorted[1].Commit.Timestamp != 200 {
		t.Errorf("sorted[1].Timestamp = %d, want 200", sorted[1].Commit.Timestamp)
	}
	if sorted[2].Commit.Timestamp != 300 {
		t.Errorf("sorted[2].Timestamp = %d, want 300", sorted[2].Commit.Timestamp)
	}

	// Original should be unmodified.
	if entries[0].Commit.Timestamp != 300 {
		t.Error("OrderByCommitTime modified the original slice")
	}
}

func TestOrderByCommitTimeEmpty(t *testing.T) {
	sorted := OrderByCommitTime(nil)
	if len(sorted) != 0 {
		t.Errorf("sorted len = %d, want 0", len(sorted))
	}
}

// --- Multiple reveals ---

func TestMultipleReveals(t *testing.T) {
	pool := NewEncryptedPool()

	var txs []*types.Transaction
	for i := uint64(0); i < 5; i++ {
		tx := testTx(i, int64(i*1000))
		txs = append(txs, tx)
		commitHash := ComputeCommitHash(tx)
		pool.AddCommit(&CommitTx{CommitHash: commitHash, Timestamp: 100 + i})
		pool.AddReveal(&RevealTx{CommitHash: commitHash, Transaction: tx})
	}

	revealed := pool.GetRevealed()
	if len(revealed) != 5 {
		t.Fatalf("GetRevealed() len = %d, want 5", len(revealed))
	}
}

// --- CommitRevealWindow ---

func TestCommitRevealWindow(t *testing.T) {
	if CommitRevealWindow != 12 {
		t.Errorf("CommitRevealWindow = %d, want 12", CommitRevealWindow)
	}
}
