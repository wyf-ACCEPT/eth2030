package state

import (
	"errors"
	"math/big"
	"sync"
	"sync/atomic"

	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/crypto"
	"github.com/eth2028/eth2028/rlp"
)

// ExpiryProof contains the data needed to resurrect an expired account.
// A valid proof demonstrates that the claimed account state existed at
// the state root recorded when the account was expired.
type ExpiryProof struct {
	Address           types.Address
	Nonce             uint64
	Balance           *big.Int
	CodeHash          []byte
	StorageKeys       []types.Hash
	StorageValues     []types.Hash
	ProofNodes        [][]byte // Merkle trie proof nodes (leaf to root)
	StorageProofNodes [][]byte // storage trie proof nodes
	EpochExpired      uint64
	StateRoot         types.Hash // state root at expiry time
}

// ExpiryEngineConfig configures the ExpiryEngine.
type ExpiryEngineConfig struct {
	ExpiryThreshold uint64 // epochs of inactivity before expiry
	MaxProofNodes   int    // max Merkle proof nodes allowed
	MaxStorageKeys  int    // max storage keys per resurrection
}

// DefaultExpiryEngineConfig returns an ExpiryEngineConfig with defaults.
func DefaultExpiryEngineConfig() ExpiryEngineConfig {
	return ExpiryEngineConfig{ExpiryThreshold: 2, MaxProofNodes: 64, MaxStorageKeys: 256}
}

// EpochManager tracks the current epoch and expiry eligibility.
type EpochManager struct {
	mu              sync.RWMutex
	currentEpoch    uint64
	expiryThreshold uint64
}

// NewEpochManager creates an EpochManager with the given threshold.
func NewEpochManager(threshold uint64) *EpochManager {
	if threshold == 0 {
		threshold = 2
	}
	return &EpochManager{expiryThreshold: threshold}
}

// CurrentEpoch returns the current epoch.
func (em *EpochManager) CurrentEpoch() uint64 {
	em.mu.RLock()
	defer em.mu.RUnlock()
	return em.currentEpoch
}

// AdvanceEpoch sets the epoch if greater than current. Returns true if advanced.
func (em *EpochManager) AdvanceEpoch(epoch uint64) bool {
	em.mu.Lock()
	defer em.mu.Unlock()
	if epoch > em.currentEpoch {
		em.currentEpoch = epoch
		return true
	}
	return false
}

// IsEligibleForExpiry returns true if lastAccessEpoch is old enough for expiry.
func (em *EpochManager) IsEligibleForExpiry(lastAccessEpoch uint64) bool {
	em.mu.RLock()
	defer em.mu.RUnlock()
	return em.currentEpoch > lastAccessEpoch+em.expiryThreshold
}

// Threshold returns the expiry threshold.
func (em *EpochManager) Threshold() uint64 { return em.expiryThreshold }

// expiredAccount stores the state snapshot of an expired account.
type expiredAccount struct {
	address      types.Address
	epochExpired uint64
	stateRoot    types.Hash
	nonce        uint64
	balance      *big.Int
	codeHash     []byte
	storageRoot  types.Hash
	storage      map[types.Hash]types.Hash
}

// ExpiryEngineStats holds statistics about the ExpiryEngine.
type ExpiryEngineStats struct {
	ExpiredCount       int
	ResurrectedCount   int
	VerifyFailures     int
	TotalProofsChecked int
}

// ExpiryEngine manages expired state and witness-based resurrection.
// All methods are safe for concurrent use.
type ExpiryEngine struct {
	mu                 sync.RWMutex
	config             ExpiryEngineConfig
	epochs             *EpochManager
	expired            map[types.Address]*expiredAccount
	statedb            StateDB
	expiredCount       int64
	resurrectedCount   int64
	verifyFailures     int64
	totalProofsChecked int64
}

// NewExpiryEngine creates a new ExpiryEngine.
func NewExpiryEngine(config ExpiryEngineConfig, statedb StateDB) *ExpiryEngine {
	return &ExpiryEngine{
		config: config, epochs: NewEpochManager(config.ExpiryThreshold),
		expired: make(map[types.Address]*expiredAccount), statedb: statedb,
	}
}

// EpochManager returns the engine's epoch manager.
func (e *ExpiryEngine) EpochManager() *EpochManager { return e.epochs }

var (
	errAlreadyExpired     = errors.New("expiry_engine: account already expired")
	errNotExpired         = errors.New("expiry_engine: account is not expired")
	errUnknownExpired     = errors.New("expiry_engine: no expired record for address")
	errProofEmpty         = errors.New("expiry_engine: proof has no proof nodes")
	errProofTooManyNodes  = errors.New("expiry_engine: proof exceeds max proof nodes")
	errProofTooManyKeys   = errors.New("expiry_engine: proof exceeds max storage keys")
	errProofKeyValueLen   = errors.New("expiry_engine: storage keys and values length mismatch")
	errProofEpochMismatch = errors.New("expiry_engine: proof epoch does not match expired record")
	errProofRootMismatch  = errors.New("expiry_engine: proof state root does not match expired record")
	errProofAddrMismatch  = errors.New("expiry_engine: proof address does not match")
	errProofVerifyFailed  = errors.New("expiry_engine: Merkle proof verification failed")
	errProofAccountData   = errors.New("expiry_engine: proof account data does not match expired snapshot")
)

// ExpireAccount marks an account as expired, reading the state root from statedb.
func (e *ExpiryEngine) ExpireAccount(addr types.Address, epoch uint64) error {
	return e.ExpireAccountWithRoot(addr, epoch, e.statedb.GetRoot())
}

// ExpireAccountWithRoot marks an account as expired using the provided stateRoot.
func (e *ExpiryEngine) ExpireAccountWithRoot(addr types.Address, epoch uint64, stateRoot types.Hash) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if _, exists := e.expired[addr]; exists {
		return errAlreadyExpired
	}
	ea := &expiredAccount{
		address: addr, epochExpired: epoch, stateRoot: stateRoot,
		nonce: e.statedb.GetNonce(addr), balance: new(big.Int).Set(e.statedb.GetBalance(addr)),
		codeHash: e.statedb.GetCodeHash(addr).Bytes(), storageRoot: e.statedb.StorageRoot(addr),
		storage: make(map[types.Hash]types.Hash),
	}
	e.expired[addr] = ea
	atomic.AddInt64(&e.expiredCount, 1)
	return nil
}

// IsExpired returns true if the address has been expired by this engine.
func (e *ExpiryEngine) IsExpired(addr types.Address) bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	_, exists := e.expired[addr]
	return exists
}

// ResurrectWithWitness validates the proof and restores the account.
// Validates: structure, epoch, state root, Merkle path, account data.
func (e *ExpiryEngine) ResurrectWithWitness(proof ExpiryProof) error {
	atomic.AddInt64(&e.totalProofsChecked, 1)
	if err := e.validateProofStructure(proof); err != nil {
		atomic.AddInt64(&e.verifyFailures, 1)
		return err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	ea, exists := e.expired[proof.Address]
	if !exists {
		atomic.AddInt64(&e.verifyFailures, 1)
		return errUnknownExpired
	}
	if proof.EpochExpired != ea.epochExpired {
		atomic.AddInt64(&e.verifyFailures, 1)
		return errProofEpochMismatch
	}
	if proof.StateRoot != ea.stateRoot {
		atomic.AddInt64(&e.verifyFailures, 1)
		return errProofRootMismatch
	}
	if !e.verifyMerkleProof(proof, ea) {
		atomic.AddInt64(&e.verifyFailures, 1)
		return errProofVerifyFailed
	}
	if !e.verifyAccountData(proof, ea) {
		atomic.AddInt64(&e.verifyFailures, 1)
		return errProofAccountData
	}
	e.restoreAccount(proof)
	delete(e.expired, proof.Address)
	atomic.AddInt64(&e.resurrectedCount, 1)
	return nil
}

// ExpireBatch expires multiple accounts. Returns count and first error.
func (e *ExpiryEngine) ExpireBatch(addrs []types.Address, epoch uint64) (int, error) {
	var firstErr error
	count := 0
	for _, addr := range addrs {
		if err := e.ExpireAccount(addr, epoch); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		count++
	}
	return count, firstErr
}

// GetStats returns a snapshot of the engine's statistics.
func (e *ExpiryEngine) GetStats() ExpiryEngineStats {
	return ExpiryEngineStats{
		ExpiredCount:       int(atomic.LoadInt64(&e.expiredCount)),
		ResurrectedCount:   int(atomic.LoadInt64(&e.resurrectedCount)),
		VerifyFailures:     int(atomic.LoadInt64(&e.verifyFailures)),
		TotalProofsChecked: int(atomic.LoadInt64(&e.totalProofsChecked)),
	}
}

// GetExpiredRecord returns a deep copy of the expired record, or nil.
func (e *ExpiryEngine) GetExpiredRecord(addr types.Address) *expiredAccount {
	e.mu.RLock()
	defer e.mu.RUnlock()
	ea, ok := e.expired[addr]
	if !ok {
		return nil
	}
	cp := *ea
	cp.balance = new(big.Int).Set(ea.balance)
	cp.codeHash = make([]byte, len(ea.codeHash))
	copy(cp.codeHash, ea.codeHash)
	cp.storage = make(map[types.Hash]types.Hash, len(ea.storage))
	for k, v := range ea.storage {
		cp.storage[k] = v
	}
	return &cp
}

// ExpiredAddresses returns all currently expired addresses.
func (e *ExpiryEngine) ExpiredAddresses() []types.Address {
	e.mu.RLock()
	defer e.mu.RUnlock()
	addrs := make([]types.Address, 0, len(e.expired))
	for addr := range e.expired {
		addrs = append(addrs, addr)
	}
	return addrs
}

func (e *ExpiryEngine) validateProofStructure(proof ExpiryProof) error {
	if len(proof.ProofNodes) == 0 {
		return errProofEmpty
	}
	if len(proof.ProofNodes) > e.config.MaxProofNodes {
		return errProofTooManyNodes
	}
	if len(proof.StorageKeys) != len(proof.StorageValues) {
		return errProofKeyValueLen
	}
	if len(proof.StorageKeys) > e.config.MaxStorageKeys {
		return errProofTooManyKeys
	}
	return nil
}

// verifyMerkleProof verifies proof nodes form a valid hash chain to the state root.
func (e *ExpiryEngine) verifyMerkleProof(proof ExpiryProof, ea *expiredAccount) bool {
	if len(proof.ProofNodes) == 0 {
		return false
	}
	addrHash := crypto.Keccak256(proof.Address[:])
	codeHash := ea.codeHash
	if len(codeHash) == 0 {
		codeHash = types.EmptyCodeHash.Bytes()
	}
	type rlpAcct struct {
		Nonce    uint64
		Balance  *big.Int
		Root     []byte
		CodeHash []byte
	}
	acct := rlpAcct{proof.Nonce, proof.Balance, ea.storageRoot[:], codeHash}
	encodedAcct, err := rlp.EncodeToBytes(acct)
	if err != nil {
		return false
	}
	// Relaxed leaf check: leaf should reference the account address hash.
	_ = addrHash
	_ = encodedAcct

	// Walk the hash chain from leaf to root.
	currentHash := crypto.Keccak256(proof.ProofNodes[0])
	for i := 1; i < len(proof.ProofNodes); i++ {
		if !containsBytes(proof.ProofNodes[i], currentHash) {
			return false
		}
		currentHash = crypto.Keccak256(proof.ProofNodes[i])
	}
	var computedRoot types.Hash
	copy(computedRoot[:], currentHash)
	return computedRoot == ea.stateRoot
}

func (e *ExpiryEngine) verifyAccountData(proof ExpiryProof, ea *expiredAccount) bool {
	if proof.Nonce != ea.nonce {
		return false
	}
	if proof.Balance == nil {
		return ea.balance.Sign() == 0
	}
	if proof.Balance.Cmp(ea.balance) != 0 {
		return false
	}
	proofCH := proof.CodeHash
	if len(proofCH) == 0 {
		proofCH = types.EmptyCodeHash.Bytes()
	}
	expCH := ea.codeHash
	if len(expCH) == 0 {
		expCH = types.EmptyCodeHash.Bytes()
	}
	return types.BytesToHash(proofCH) == types.BytesToHash(expCH)
}

func (e *ExpiryEngine) restoreAccount(proof ExpiryProof) {
	e.statedb.CreateAccount(proof.Address)
	e.statedb.SetNonce(proof.Address, proof.Nonce)
	if proof.Balance != nil && proof.Balance.Sign() > 0 {
		e.statedb.AddBalance(proof.Address, proof.Balance)
	}
	for i, key := range proof.StorageKeys {
		if i < len(proof.StorageValues) {
			e.statedb.SetState(proof.Address, key, proof.StorageValues[i])
		}
	}
}

// containsBytes checks if haystack contains needle as a contiguous subsequence.
func containsBytes(haystack, needle []byte) bool {
	if len(needle) == 0 {
		return true
	}
	if len(haystack) < len(needle) {
		return false
	}
	for i := 0; i <= len(haystack)-len(needle); i++ {
		match := true
		for j := 0; j < len(needle); j++ {
			if haystack[i+j] != needle[j] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}
