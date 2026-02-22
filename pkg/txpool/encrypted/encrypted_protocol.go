// Package encrypted implements the encrypted mempool protocol with commit-reveal
// scheme for MEV protection. Part of the EL Cryptography roadmap track.
//
// The EncryptedMempoolProtocol manages the lifecycle of encrypted transactions:
// 1. Commit: sender submits encrypted tx data, receives a commitment hash.
// 2. Reveal: after commit window, revealers submit decrypted data.
// 3. Expiry: uncommitted txs past the reveal window are garbage collected.
package encrypted

import (
	"errors"
	"sync"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

// Errors for the encrypted mempool protocol.
var (
	ErrEncProtocolCommitExists    = errors.New("enc_protocol: commitment already exists")
	ErrEncProtocolNotCommitted    = errors.New("enc_protocol: transaction not committed")
	ErrEncProtocolAlreadyRevealed = errors.New("enc_protocol: transaction already revealed")
	ErrEncProtocolTooManyCommits  = errors.New("enc_protocol: too many pending commits")
	ErrEncProtocolEmptyData       = errors.New("enc_protocol: encrypted data is empty")
	ErrEncProtocolEmptySender     = errors.New("enc_protocol: sender is empty")
	ErrEncProtocolEmptyRevealer   = errors.New("enc_protocol: revealer is empty")
	ErrEncProtocolEmptyDecrypted  = errors.New("enc_protocol: decrypted data is empty")
)

// EncryptedProtocolConfig configures the encrypted mempool protocol.
type EncryptedProtocolConfig struct {
	// CommitWindowBlocks is the number of blocks a commit is valid before
	// the reveal window begins.
	CommitWindowBlocks uint64
	// RevealWindowBlocks is how many blocks after the commit window the
	// reveal must happen before the commit expires.
	RevealWindowBlocks uint64
	// MaxPendingCommits limits the number of outstanding commits.
	MaxPendingCommits int
	// MinRevealers is the minimum number of revealers required for a valid reveal.
	MinRevealers int
}

// CommittedTx represents an encrypted transaction that has been committed
// but not yet revealed.
type CommittedTx struct {
	CommitmentHash types.Hash
	Sender         string
	CommitBlock    uint64
	EncryptedData  []byte
	Revealed       bool
}

// RevealedTx represents a transaction that has been revealed after commitment.
type RevealedTx struct {
	OriginalTxHash types.Hash
	DecryptedData  []byte
	RevealBlock    uint64
	Revealers      []string
}

// EncryptedMempoolProtocol manages the commit-reveal lifecycle for encrypted
// transactions. It is thread-safe.
type EncryptedMempoolProtocol struct {
	mu        sync.RWMutex
	config    EncryptedProtocolConfig
	committed map[types.Hash]*CommittedTx
	revealed  map[types.Hash]*RevealedTx
	epoch     uint64
}

// NewEncryptedMempoolProtocol creates a new protocol instance.
func NewEncryptedMempoolProtocol(config EncryptedProtocolConfig) *EncryptedMempoolProtocol {
	return &EncryptedMempoolProtocol{
		config:    config,
		committed: make(map[types.Hash]*CommittedTx),
		revealed:  make(map[types.Hash]*RevealedTx),
	}
}

// Commit registers an encrypted transaction commitment. It hashes the sender
// and encrypted data to produce a unique commitment hash, then stores the
// commitment. Returns the commitment hash or an error.
func (p *EncryptedMempoolProtocol) Commit(sender string, encryptedData []byte) (types.Hash, error) {
	if sender == "" {
		return types.Hash{}, ErrEncProtocolEmptySender
	}
	if len(encryptedData) == 0 {
		return types.Hash{}, ErrEncProtocolEmptyData
	}

	// Compute commitment hash from sender + encrypted data.
	hashInput := append([]byte(sender), encryptedData...)
	commitHash := crypto.Keccak256Hash(hashInput)

	p.mu.Lock()
	defer p.mu.Unlock()

	if _, exists := p.committed[commitHash]; exists {
		return types.Hash{}, ErrEncProtocolCommitExists
	}

	// Check capacity limit.
	pendingCount := 0
	for _, c := range p.committed {
		if !c.Revealed {
			pendingCount++
		}
	}
	if p.config.MaxPendingCommits > 0 && pendingCount >= p.config.MaxPendingCommits {
		return types.Hash{}, ErrEncProtocolTooManyCommits
	}

	p.committed[commitHash] = &CommittedTx{
		CommitmentHash: commitHash,
		Sender:         sender,
		CommitBlock:    p.epoch,
		EncryptedData:  make([]byte, len(encryptedData)),
		Revealed:       false,
	}
	copy(p.committed[commitHash].EncryptedData, encryptedData)

	return commitHash, nil
}

// Reveal submits decrypted data for a previously committed transaction.
// The revealer string identifies who revealed, and multiple revealers
// are accumulated until MinRevealers is met.
func (p *EncryptedMempoolProtocol) Reveal(commitHash types.Hash, decryptedData []byte, revealer string) error {
	if len(decryptedData) == 0 {
		return ErrEncProtocolEmptyDecrypted
	}
	if revealer == "" {
		return ErrEncProtocolEmptyRevealer
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	committed, exists := p.committed[commitHash]
	if !exists {
		return ErrEncProtocolNotCommitted
	}
	if committed.Revealed {
		return ErrEncProtocolAlreadyRevealed
	}

	// Check if we already have a partial reveal for this commitment.
	rev, revExists := p.revealed[commitHash]
	if !revExists {
		// First reveal for this commitment.
		txHash := crypto.Keccak256Hash(decryptedData)
		rev = &RevealedTx{
			OriginalTxHash: txHash,
			DecryptedData:  make([]byte, len(decryptedData)),
			RevealBlock:    p.epoch,
			Revealers:      []string{revealer},
		}
		copy(rev.DecryptedData, decryptedData)
		p.revealed[commitHash] = rev
	} else {
		// Additional revealer for existing partial reveal.
		// Check if this revealer already contributed.
		for _, r := range rev.Revealers {
			if r == revealer {
				return nil // already counted, idempotent
			}
		}
		rev.Revealers = append(rev.Revealers, revealer)
	}

	// Mark as fully revealed if minimum revealers threshold is met.
	if len(rev.Revealers) >= p.config.MinRevealers || p.config.MinRevealers <= 1 {
		committed.Revealed = true
	}

	return nil
}

// GetRevealed returns the revealed transaction data for a given commitment hash.
func (p *EncryptedMempoolProtocol) GetRevealed(commitHash types.Hash) (*RevealedTx, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	rev, exists := p.revealed[commitHash]
	if !exists {
		return nil, ErrEncProtocolNotCommitted
	}
	return rev, nil
}

// IsCommitted returns true if a commitment hash exists (revealed or not).
func (p *EncryptedMempoolProtocol) IsCommitted(commitHash types.Hash) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	_, exists := p.committed[commitHash]
	return exists
}

// IsRevealed returns true if the commitment has been fully revealed.
func (p *EncryptedMempoolProtocol) IsRevealed(commitHash types.Hash) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	committed, exists := p.committed[commitHash]
	if !exists {
		return false
	}
	return committed.Revealed
}

// PendingCommits returns the number of commits that have not yet been revealed.
func (p *EncryptedMempoolProtocol) PendingCommits() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	count := 0
	for _, c := range p.committed {
		if !c.Revealed {
			count++
		}
	}
	return count
}

// ExpireOldCommits removes commits whose reveal window has passed based on
// block number. A commit expires when currentBlock exceeds commitBlock +
// CommitWindowBlocks + RevealWindowBlocks. Returns the number of expired entries.
func (p *EncryptedMempoolProtocol) ExpireOldCommits(currentBlock uint64) int {
	p.mu.Lock()
	defer p.mu.Unlock()

	deadline := p.config.CommitWindowBlocks + p.config.RevealWindowBlocks
	expired := 0

	for hash, c := range p.committed {
		if c.Revealed {
			continue
		}
		if currentBlock > c.CommitBlock+deadline {
			delete(p.committed, hash)
			delete(p.revealed, hash) // also clean up partial reveals
			expired++
		}
	}
	return expired
}

// CommitCount returns the total number of committed transactions (all states).
func (p *EncryptedMempoolProtocol) CommitCount() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.committed)
}

// RevealCount returns the number of fully revealed transactions.
func (p *EncryptedMempoolProtocol) RevealCount() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	count := 0
	for _, c := range p.committed {
		if c.Revealed {
			count++
		}
	}
	return count
}

// SetEpoch updates the current block number (epoch) used for commit timestamps.
func (p *EncryptedMempoolProtocol) SetEpoch(block uint64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.epoch = block
}

// Config returns the protocol configuration.
func (p *EncryptedMempoolProtocol) Config() EncryptedProtocolConfig {
	return p.config
}
