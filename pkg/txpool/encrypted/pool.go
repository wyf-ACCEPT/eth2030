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
	commits map[types.Hash]*CommitEntry      // commitHash -> entry
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

// ComputeCommitHash computes keccak256(rlp(tx)) for use as a commit hash.
func ComputeCommitHash(tx *types.Transaction) types.Hash {
	encoded, err := tx.EncodeRLP()
	if err != nil {
		return types.Hash{}
	}
	return crypto.Keccak256Hash(encoded)
}
