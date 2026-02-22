package encrypted

import (
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

// poolTestTx creates a test transaction, distinct from testTx in encrypted_test.go.
func poolTestTx(nonce uint64, gasPrice int64) *types.Transaction {
	to := types.HexToAddress("0xdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef")
	return types.NewTransaction(&types.LegacyTx{
		Nonce:    nonce,
		GasPrice: big.NewInt(gasPrice),
		Gas:      21000,
		To:       &to,
		Value:    big.NewInt(1),
	})
}

// --- Pool creation ---

func TestPool_NewEncryptedPool(t *testing.T) {
	pool := NewEncryptedPool()
	if pool == nil {
		t.Fatal("NewEncryptedPool returned nil")
	}
	if pool.Pending() != 0 {
		t.Fatalf("new pool pending: want 0, got %d", pool.Pending())
	}
	if pool.Committed() != 0 {
		t.Fatalf("new pool committed: want 0, got %d", pool.Committed())
	}
}

// --- Commit lifecycle ---

func TestPool_CommitAndPending(t *testing.T) {
	pool := NewEncryptedPool()

	for i := 0; i < 5; i++ {
		commit := &CommitTx{
			CommitHash: types.HexToHash("0x" + string(rune('a'+i))),
			Sender:     types.HexToAddress("0x1111"),
			GasLimit:   21000,
			MaxFee:     big.NewInt(1000),
			Timestamp:  uint64(100 + i),
		}
		if err := pool.AddCommit(commit); err != nil {
			t.Fatalf("AddCommit(%d): %v", i, err)
		}
	}

	if pool.Pending() != 5 {
		t.Fatalf("pending: want 5, got %d", pool.Pending())
	}
	if pool.Committed() != 5 {
		t.Fatalf("committed: want 5, got %d", pool.Committed())
	}
}

// --- Reveal lifecycle ---

func TestPool_CommitRevealLifecycle(t *testing.T) {
	pool := NewEncryptedPool()
	tx := poolTestTx(0, 2000)
	commitHash := ComputeCommitHash(tx)

	commit := &CommitTx{
		CommitHash: commitHash,
		Sender:     types.HexToAddress("0x2222"),
		GasLimit:   21000,
		MaxFee:     big.NewInt(2000),
		Timestamp:  150,
	}
	if err := pool.AddCommit(commit); err != nil {
		t.Fatalf("AddCommit: %v", err)
	}

	reveal := &RevealTx{
		CommitHash:  commitHash,
		Transaction: tx,
	}
	if err := pool.AddReveal(reveal); err != nil {
		t.Fatalf("AddReveal: %v", err)
	}

	revealed := pool.GetRevealed()
	if len(revealed) != 1 {
		t.Fatalf("revealed: want 1, got %d", len(revealed))
	}
	if pool.Pending() != 0 {
		t.Fatalf("pending after reveal: want 0, got %d", pool.Pending())
	}
}

// --- Nil commit ---

func TestPool_AddCommitNilSafe(t *testing.T) {
	pool := NewEncryptedPool()
	if err := pool.AddCommit(nil); err == nil {
		t.Fatal("AddCommit(nil) should return error")
	}
}

// --- Duplicate commit ---

func TestPool_AddCommitDuplicateCheck(t *testing.T) {
	pool := NewEncryptedPool()
	commit := &CommitTx{
		CommitHash: types.HexToHash("0xbeef"),
		Timestamp:  100,
	}
	pool.AddCommit(commit)
	if err := pool.AddCommit(commit); err != ErrDuplicateCommit {
		t.Fatalf("duplicate: want ErrDuplicateCommit, got %v", err)
	}
}

// --- Reveal hash mismatch ---

func TestPool_RevealHashMismatch(t *testing.T) {
	pool := NewEncryptedPool()
	tx := poolTestTx(1, 3000)

	// Commit with a different hash than what tx produces.
	fakeHash := types.HexToHash("0xfacade")
	pool.AddCommit(&CommitTx{CommitHash: fakeHash, Timestamp: 100})

	reveal := &RevealTx{
		CommitHash:  fakeHash,
		Transaction: tx,
	}
	if err := pool.AddReveal(reveal); err != ErrHashMismatch {
		t.Fatalf("mismatch: want ErrHashMismatch, got %v", err)
	}
}

// --- Nil reveal ---

func TestPool_AddRevealNilCheck(t *testing.T) {
	pool := NewEncryptedPool()
	if err := pool.AddReveal(nil); err != ErrNilTransaction {
		t.Fatalf("nil reveal: want ErrNilTransaction, got %v", err)
	}
	if err := pool.AddReveal(&RevealTx{}); err != ErrNilTransaction {
		t.Fatalf("empty reveal: want ErrNilTransaction, got %v", err)
	}
}

// --- Expire commits ---

func TestPool_ExpireMultipleCommits(t *testing.T) {
	pool := NewEncryptedPool()

	// Timestamps: 100, 200, 300
	for i := 0; i < 3; i++ {
		pool.AddCommit(&CommitTx{
			CommitHash: types.HexToHash("0x" + string(rune('a'+i))),
			Timestamp:  uint64(100 + i*100),
		})
	}

	// CommitRevealWindow = 12, so deadlines are 112, 212, 312.
	// At time 215: first two are expired.
	expired := pool.ExpireCommits(215)
	if expired != 2 {
		t.Fatalf("expired: want 2, got %d", expired)
	}
	if pool.Pending() != 1 {
		t.Fatalf("pending: want 1, got %d", pool.Pending())
	}
}

// --- Multiple reveals ---

func TestPool_MultipleRevealsOrdered(t *testing.T) {
	pool := NewEncryptedPool()

	var commitHashes []types.Hash
	for i := uint64(0); i < 3; i++ {
		tx := poolTestTx(i, int64(1000+i*500))
		ch := ComputeCommitHash(tx)
		commitHashes = append(commitHashes, ch)
		pool.AddCommit(&CommitTx{CommitHash: ch, Timestamp: 100 + i*10})
		pool.AddReveal(&RevealTx{CommitHash: ch, Transaction: tx})
	}

	revealed := pool.GetRevealed()
	if len(revealed) != 3 {
		t.Fatalf("revealed: want 3, got %d", len(revealed))
	}
}

// --- Already revealed ---

func TestPool_AlreadyRevealedError(t *testing.T) {
	pool := NewEncryptedPool()
	tx := poolTestTx(0, 1000)
	ch := ComputeCommitHash(tx)

	pool.AddCommit(&CommitTx{CommitHash: ch, Timestamp: 100})
	pool.AddReveal(&RevealTx{CommitHash: ch, Transaction: tx})

	err := pool.AddReveal(&RevealTx{CommitHash: ch, Transaction: tx})
	if err != ErrAlreadyRevealed {
		t.Fatalf("double reveal: want ErrAlreadyRevealed, got %v", err)
	}
}

// --- Reveal with no commit ---

func TestPool_RevealWithoutCommit(t *testing.T) {
	pool := NewEncryptedPool()
	tx := poolTestTx(0, 1000)
	ch := ComputeCommitHash(tx)

	err := pool.AddReveal(&RevealTx{CommitHash: ch, Transaction: tx})
	if err != ErrCommitNotFound {
		t.Fatalf("no commit: want ErrCommitNotFound, got %v", err)
	}
}

// --- CommitRevealWindow constant ---

func TestPool_CommitRevealWindowValue(t *testing.T) {
	if CommitRevealWindow != 12 {
		t.Fatalf("CommitRevealWindow: want 12, got %d", CommitRevealWindow)
	}
}

// --- Status constants ---

func TestPool_StatusConstants(t *testing.T) {
	if COMMITTED != 0 {
		t.Fatalf("COMMITTED: want 0, got %d", COMMITTED)
	}
	if REVEALED != 1 {
		t.Fatalf("REVEALED: want 1, got %d", REVEALED)
	}
	if EXPIRED != 2 {
		t.Fatalf("EXPIRED: want 2, got %d", EXPIRED)
	}
}

// --- ComputeCommitHash deterministic ---

func TestPool_CommitHashDeterministic(t *testing.T) {
	tx := poolTestTx(42, 5000)
	h1 := ComputeCommitHash(tx)
	h2 := ComputeCommitHash(tx)
	if h1 != h2 {
		t.Fatal("ComputeCommitHash should be deterministic")
	}
	if h1.IsZero() {
		t.Fatal("ComputeCommitHash should not return zero hash")
	}
}

// --- ComputeCommitHash different txs produce different hashes ---

func TestPool_CommitHashDiffers(t *testing.T) {
	tx1 := poolTestTx(0, 1000)
	tx2 := poolTestTx(1, 2000)
	if ComputeCommitHash(tx1) == ComputeCommitHash(tx2) {
		t.Fatal("different txs should produce different commit hashes")
	}
}
