package encrypted

import (
	"fmt"
	"sync"
	"testing"

	"github.com/eth2028/eth2028/crypto"
)

func defaultProtocolConfig() EncryptedProtocolConfig {
	return EncryptedProtocolConfig{
		CommitWindowBlocks: 10,
		RevealWindowBlocks: 10,
		MaxPendingCommits:  100,
		MinRevealers:       1,
	}
}

// --- NewEncryptedMempoolProtocol ---

func TestNewEncryptedMempoolProtocol(t *testing.T) {
	cfg := defaultProtocolConfig()
	p := NewEncryptedMempoolProtocol(cfg)
	if p == nil {
		t.Fatal("NewEncryptedMempoolProtocol returned nil")
	}
	if p.CommitCount() != 0 {
		t.Errorf("CommitCount() = %d, want 0", p.CommitCount())
	}
	if p.RevealCount() != 0 {
		t.Errorf("RevealCount() = %d, want 0", p.RevealCount())
	}
	if p.PendingCommits() != 0 {
		t.Errorf("PendingCommits() = %d, want 0", p.PendingCommits())
	}
}

func TestProtocolConfig(t *testing.T) {
	cfg := EncryptedProtocolConfig{
		CommitWindowBlocks: 5,
		RevealWindowBlocks: 15,
		MaxPendingCommits:  50,
		MinRevealers:       3,
	}
	p := NewEncryptedMempoolProtocol(cfg)
	got := p.Config()
	if got.CommitWindowBlocks != 5 || got.RevealWindowBlocks != 15 {
		t.Errorf("Config mismatch: got %+v", got)
	}
	if got.MaxPendingCommits != 50 || got.MinRevealers != 3 {
		t.Errorf("Config mismatch: got %+v", got)
	}
}

// --- Commit ---

func TestCommitBasic(t *testing.T) {
	p := NewEncryptedMempoolProtocol(defaultProtocolConfig())
	hash, err := p.Commit("sender1", []byte("encrypted_tx_data"))
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if hash.IsZero() {
		t.Fatal("Commit returned zero hash")
	}
	if p.CommitCount() != 1 {
		t.Errorf("CommitCount() = %d, want 1", p.CommitCount())
	}
	if p.PendingCommits() != 1 {
		t.Errorf("PendingCommits() = %d, want 1", p.PendingCommits())
	}
}

func TestCommitDuplicate(t *testing.T) {
	p := NewEncryptedMempoolProtocol(defaultProtocolConfig())
	_, err := p.Commit("sender1", []byte("data1"))
	if err != nil {
		t.Fatalf("first Commit: %v", err)
	}
	_, err = p.Commit("sender1", []byte("data1"))
	if err != ErrEncProtocolCommitExists {
		t.Errorf("duplicate Commit: got %v, want %v", err, ErrEncProtocolCommitExists)
	}
}

func TestCommitEmptySender(t *testing.T) {
	p := NewEncryptedMempoolProtocol(defaultProtocolConfig())
	_, err := p.Commit("", []byte("data"))
	if err != ErrEncProtocolEmptySender {
		t.Errorf("empty sender: got %v, want %v", err, ErrEncProtocolEmptySender)
	}
}

func TestCommitEmptyData(t *testing.T) {
	p := NewEncryptedMempoolProtocol(defaultProtocolConfig())
	_, err := p.Commit("sender1", nil)
	if err != ErrEncProtocolEmptyData {
		t.Errorf("nil data: got %v, want %v", err, ErrEncProtocolEmptyData)
	}
	_, err = p.Commit("sender1", []byte{})
	if err != ErrEncProtocolEmptyData {
		t.Errorf("empty data: got %v, want %v", err, ErrEncProtocolEmptyData)
	}
}

func TestCommitTooManyPending(t *testing.T) {
	cfg := defaultProtocolConfig()
	cfg.MaxPendingCommits = 3
	p := NewEncryptedMempoolProtocol(cfg)

	for i := 0; i < 3; i++ {
		_, err := p.Commit(fmt.Sprintf("sender%d", i), []byte(fmt.Sprintf("data%d", i)))
		if err != nil {
			t.Fatalf("Commit %d: %v", i, err)
		}
	}

	_, err := p.Commit("sender_overflow", []byte("overflow_data"))
	if err != ErrEncProtocolTooManyCommits {
		t.Errorf("over limit: got %v, want %v", err, ErrEncProtocolTooManyCommits)
	}
}

func TestCommitDifferentDataDifferentHash(t *testing.T) {
	p := NewEncryptedMempoolProtocol(defaultProtocolConfig())
	h1, _ := p.Commit("sender1", []byte("data_a"))
	h2, _ := p.Commit("sender1", []byte("data_b"))
	if h1 == h2 {
		t.Error("different data should produce different hashes")
	}
}

func TestCommitDifferentSenderDifferentHash(t *testing.T) {
	p := NewEncryptedMempoolProtocol(defaultProtocolConfig())
	h1, _ := p.Commit("alice", []byte("same_data"))
	h2, _ := p.Commit("bob", []byte("same_data"))
	if h1 == h2 {
		t.Error("different senders should produce different hashes")
	}
}

// --- Reveal ---

func TestRevealBasic(t *testing.T) {
	p := NewEncryptedMempoolProtocol(defaultProtocolConfig())
	hash, _ := p.Commit("sender1", []byte("encrypted"))

	err := p.Reveal(hash, []byte("decrypted_tx_payload"), "revealer1")
	if err != nil {
		t.Fatalf("Reveal: %v", err)
	}

	if !p.IsRevealed(hash) {
		t.Error("IsRevealed should be true after reveal")
	}
	if p.PendingCommits() != 0 {
		t.Errorf("PendingCommits() = %d, want 0 after reveal", p.PendingCommits())
	}
	if p.RevealCount() != 1 {
		t.Errorf("RevealCount() = %d, want 1", p.RevealCount())
	}
}

func TestRevealNotCommitted(t *testing.T) {
	p := NewEncryptedMempoolProtocol(defaultProtocolConfig())
	fakeHash := crypto.Keccak256Hash([]byte("fake"))
	err := p.Reveal(fakeHash, []byte("data"), "revealer1")
	if err != ErrEncProtocolNotCommitted {
		t.Errorf("reveal without commit: got %v, want %v", err, ErrEncProtocolNotCommitted)
	}
}

func TestRevealAlreadyRevealed(t *testing.T) {
	p := NewEncryptedMempoolProtocol(defaultProtocolConfig())
	hash, _ := p.Commit("sender1", []byte("encrypted"))
	p.Reveal(hash, []byte("decrypted"), "revealer1")

	err := p.Reveal(hash, []byte("decrypted_again"), "revealer2")
	if err != ErrEncProtocolAlreadyRevealed {
		t.Errorf("double reveal: got %v, want %v", err, ErrEncProtocolAlreadyRevealed)
	}
}

func TestRevealEmptyData(t *testing.T) {
	p := NewEncryptedMempoolProtocol(defaultProtocolConfig())
	hash, _ := p.Commit("sender1", []byte("encrypted"))
	err := p.Reveal(hash, nil, "revealer1")
	if err != ErrEncProtocolEmptyDecrypted {
		t.Errorf("empty decrypted data: got %v, want %v", err, ErrEncProtocolEmptyDecrypted)
	}
}

func TestRevealEmptyRevealer(t *testing.T) {
	p := NewEncryptedMempoolProtocol(defaultProtocolConfig())
	hash, _ := p.Commit("sender1", []byte("encrypted"))
	err := p.Reveal(hash, []byte("decrypted"), "")
	if err != ErrEncProtocolEmptyRevealer {
		t.Errorf("empty revealer: got %v, want %v", err, ErrEncProtocolEmptyRevealer)
	}
}

// --- Multi-revealer ---

func TestMultiRevealerThreshold(t *testing.T) {
	cfg := defaultProtocolConfig()
	cfg.MinRevealers = 3
	p := NewEncryptedMempoolProtocol(cfg)

	hash, _ := p.Commit("sender1", []byte("encrypted"))

	// First two revealers: not yet fully revealed.
	p.Reveal(hash, []byte("decrypted"), "rev1")
	if p.IsRevealed(hash) {
		t.Error("should not be revealed with only 1 of 3 revealers")
	}

	p.Reveal(hash, []byte("decrypted"), "rev2")
	if p.IsRevealed(hash) {
		t.Error("should not be revealed with only 2 of 3 revealers")
	}

	// Third revealer: should finalize.
	p.Reveal(hash, []byte("decrypted"), "rev3")
	if !p.IsRevealed(hash) {
		t.Error("should be revealed with 3 of 3 revealers")
	}
}

func TestRevealerIdempotent(t *testing.T) {
	cfg := defaultProtocolConfig()
	cfg.MinRevealers = 2
	p := NewEncryptedMempoolProtocol(cfg)

	hash, _ := p.Commit("sender1", []byte("encrypted"))
	p.Reveal(hash, []byte("decrypted"), "rev1")
	// Same revealer again should be idempotent.
	p.Reveal(hash, []byte("decrypted"), "rev1")

	if p.IsRevealed(hash) {
		t.Error("duplicate revealer should not count toward threshold")
	}
}

// --- GetRevealed ---

func TestGetRevealed(t *testing.T) {
	p := NewEncryptedMempoolProtocol(defaultProtocolConfig())
	hash, _ := p.Commit("sender1", []byte("encrypted"))
	p.Reveal(hash, []byte("decrypted_payload"), "revealer1")

	rev, err := p.GetRevealed(hash)
	if err != nil {
		t.Fatalf("GetRevealed: %v", err)
	}
	if string(rev.DecryptedData) != "decrypted_payload" {
		t.Errorf("decrypted data = %q, want %q", rev.DecryptedData, "decrypted_payload")
	}
	if len(rev.Revealers) != 1 || rev.Revealers[0] != "revealer1" {
		t.Errorf("Revealers = %v, want [revealer1]", rev.Revealers)
	}
	if rev.OriginalTxHash.IsZero() {
		t.Error("OriginalTxHash should not be zero")
	}
}

func TestGetRevealedNotFound(t *testing.T) {
	p := NewEncryptedMempoolProtocol(defaultProtocolConfig())
	fakeHash := crypto.Keccak256Hash([]byte("nonexistent"))
	_, err := p.GetRevealed(fakeHash)
	if err != ErrEncProtocolNotCommitted {
		t.Errorf("GetRevealed not found: got %v, want %v", err, ErrEncProtocolNotCommitted)
	}
}

// --- IsCommitted / IsRevealed ---

func TestIsCommittedTrue(t *testing.T) {
	p := NewEncryptedMempoolProtocol(defaultProtocolConfig())
	hash, _ := p.Commit("sender1", []byte("encrypted"))
	if !p.IsCommitted(hash) {
		t.Error("IsCommitted should return true")
	}
}

func TestIsCommittedFalse(t *testing.T) {
	p := NewEncryptedMempoolProtocol(defaultProtocolConfig())
	fakeHash := crypto.Keccak256Hash([]byte("fake"))
	if p.IsCommitted(fakeHash) {
		t.Error("IsCommitted should return false for unknown hash")
	}
}

func TestIsRevealedFalseBeforeReveal(t *testing.T) {
	p := NewEncryptedMempoolProtocol(defaultProtocolConfig())
	hash, _ := p.Commit("sender1", []byte("encrypted"))
	if p.IsRevealed(hash) {
		t.Error("IsRevealed should return false before reveal")
	}
}

// --- ExpireOldCommits ---

func TestExpireOldCommits(t *testing.T) {
	cfg := defaultProtocolConfig()
	cfg.CommitWindowBlocks = 5
	cfg.RevealWindowBlocks = 5
	p := NewEncryptedMempoolProtocol(cfg)
	// deadline = 5 + 5 = 10 blocks after commit block

	p.SetEpoch(100)
	p.Commit("sender1", []byte("data1"))

	p.SetEpoch(105)
	p.Commit("sender2", []byte("data2"))

	// Block 111: 100 + 10 = 110, so 111 > 110, first commit expires.
	// But second commit at 105 + 10 = 115, 111 < 115 so it stays.
	expired := p.ExpireOldCommits(111)
	if expired != 1 {
		t.Errorf("ExpireOldCommits(111) = %d, want 1", expired)
	}
	if p.CommitCount() != 1 {
		t.Errorf("CommitCount() = %d, want 1", p.CommitCount())
	}

	// Block 116: second commit expires too.
	expired = p.ExpireOldCommits(116)
	if expired != 1 {
		t.Errorf("ExpireOldCommits(116) = %d, want 1", expired)
	}
	if p.CommitCount() != 0 {
		t.Errorf("CommitCount() = %d, want 0", p.CommitCount())
	}
}

func TestExpireOldCommitsNoExpiry(t *testing.T) {
	cfg := defaultProtocolConfig()
	cfg.CommitWindowBlocks = 10
	cfg.RevealWindowBlocks = 10
	p := NewEncryptedMempoolProtocol(cfg)

	p.SetEpoch(100)
	p.Commit("sender1", []byte("data1"))

	// Block 110: deadline = 100 + 20 = 120, 110 < 120 -> no expiry.
	expired := p.ExpireOldCommits(110)
	if expired != 0 {
		t.Errorf("ExpireOldCommits(110) = %d, want 0", expired)
	}
}

func TestExpireDoesNotAffectRevealed(t *testing.T) {
	cfg := defaultProtocolConfig()
	cfg.CommitWindowBlocks = 5
	cfg.RevealWindowBlocks = 5
	p := NewEncryptedMempoolProtocol(cfg)

	p.SetEpoch(100)
	hash, _ := p.Commit("sender1", []byte("data1"))
	p.Reveal(hash, []byte("decrypted"), "revealer1")

	// Even past the deadline, revealed commits should not be expired.
	expired := p.ExpireOldCommits(200)
	if expired != 0 {
		t.Errorf("ExpireOldCommits should not expire revealed commits, got %d", expired)
	}
}

// --- SetEpoch ---

func TestSetEpoch(t *testing.T) {
	p := NewEncryptedMempoolProtocol(defaultProtocolConfig())
	p.SetEpoch(42)
	hash, _ := p.Commit("sender1", []byte("data"))

	// The committed tx should have block 42.
	p.mu.RLock()
	committed := p.committed[hash]
	p.mu.RUnlock()
	if committed.CommitBlock != 42 {
		t.Errorf("CommitBlock = %d, want 42", committed.CommitBlock)
	}
}

// --- Concurrency ---

func TestConcurrentCommitsAndReveals(t *testing.T) {
	p := NewEncryptedMempoolProtocol(defaultProtocolConfig())

	var wg sync.WaitGroup
	errs := make(chan error, 200)

	// Concurrent commits.
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := p.Commit(
				fmt.Sprintf("sender_%d", i),
				[]byte(fmt.Sprintf("encrypted_%d", i)),
			)
			if err != nil {
				errs <- err
			}
		}(i)
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("concurrent commit error: %v", err)
	}

	if p.CommitCount() != 50 {
		t.Errorf("CommitCount() = %d, want 50", p.CommitCount())
	}
}

// --- CommitCount and RevealCount ---

func TestCommitAndRevealCounts(t *testing.T) {
	p := NewEncryptedMempoolProtocol(defaultProtocolConfig())

	h1, _ := p.Commit("s1", []byte("d1"))
	h2, _ := p.Commit("s2", []byte("d2"))
	p.Commit("s3", []byte("d3"))

	if p.CommitCount() != 3 {
		t.Errorf("CommitCount() = %d, want 3", p.CommitCount())
	}
	if p.RevealCount() != 0 {
		t.Errorf("RevealCount() = %d, want 0", p.RevealCount())
	}

	p.Reveal(h1, []byte("dec1"), "r1")
	p.Reveal(h2, []byte("dec2"), "r1")

	if p.RevealCount() != 2 {
		t.Errorf("RevealCount() = %d, want 2", p.RevealCount())
	}
	if p.PendingCommits() != 1 {
		t.Errorf("PendingCommits() = %d, want 1", p.PendingCommits())
	}
}

// --- MaxPendingCommits with zero (unlimited) ---

func TestMaxPendingCommitsZeroUnlimited(t *testing.T) {
	cfg := defaultProtocolConfig()
	cfg.MaxPendingCommits = 0 // unlimited
	p := NewEncryptedMempoolProtocol(cfg)

	for i := 0; i < 200; i++ {
		_, err := p.Commit(fmt.Sprintf("s%d", i), []byte(fmt.Sprintf("d%d", i)))
		if err != nil {
			t.Fatalf("Commit %d: %v", i, err)
		}
	}
	if p.CommitCount() != 200 {
		t.Errorf("CommitCount() = %d, want 200", p.CommitCount())
	}
}

// --- Reveal with epoch tracking ---

func TestRevealBlockTracking(t *testing.T) {
	p := NewEncryptedMempoolProtocol(defaultProtocolConfig())
	p.SetEpoch(10)
	hash, _ := p.Commit("sender1", []byte("encrypted"))

	p.SetEpoch(15)
	p.Reveal(hash, []byte("decrypted"), "revealer1")

	rev, _ := p.GetRevealed(hash)
	if rev.RevealBlock != 15 {
		t.Errorf("RevealBlock = %d, want 15", rev.RevealBlock)
	}
}

// --- Data isolation ---

func TestCommitDataIsolation(t *testing.T) {
	p := NewEncryptedMempoolProtocol(defaultProtocolConfig())
	data := []byte("original_data")
	hash, _ := p.Commit("sender1", data)

	// Mutate original after commit.
	data[0] = 'X'

	p.mu.RLock()
	stored := p.committed[hash]
	p.mu.RUnlock()
	if stored.EncryptedData[0] == 'X' {
		t.Error("commit should store a copy of data, not a reference")
	}
}
