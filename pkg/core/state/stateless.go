package state

import (
	"errors"
	"math/big"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
	"github.com/eth2030/eth2030/rlp"
	"github.com/eth2030/eth2030/trie"
)

// Errors returned by stateless state operations.
var (
	ErrAccountNotInWitness = errors.New("account not present in witness")
	ErrStorageNotInWitness = errors.New("storage slot not present in witness")
	ErrCodeNotInWitness    = errors.New("code not present in witness")
	ErrWitnessRootMismatch = errors.New("witness root does not match expected state root")
)

// StatelessWitness contains the pre-state data needed to execute a block
// without holding the full state trie. It carries account proofs, storage
// proofs, and code preimages that the stateless executor can read from.
type StatelessWitness struct {
	// Accounts maps addresses to their pre-execution account state.
	Accounts map[types.Address]*WitnessAccount

	// Codes maps code hashes to bytecode for all contracts read during
	// execution. The key is the Keccak-256 hash of the bytecode.
	Codes map[types.Hash][]byte

	// StateRoot is the expected state root that the witness was generated
	// against. Used for verification.
	StateRoot types.Hash
}

// WitnessAccount holds the pre-state for a single account as captured in
// the witness.
type WitnessAccount struct {
	Nonce    uint64
	Balance  *big.Int
	CodeHash types.Hash
	Storage  map[types.Hash]types.Hash
	Exists   bool
}

// NewStatelessWitness creates an empty stateless witness.
func NewStatelessWitness(stateRoot types.Hash) *StatelessWitness {
	return &StatelessWitness{
		Accounts:  make(map[types.Address]*WitnessAccount),
		Codes:     make(map[types.Hash][]byte),
		StateRoot: stateRoot,
	}
}

// AddAccount adds an account entry to the witness.
func (w *StatelessWitness) AddAccount(addr types.Address, acct *WitnessAccount) {
	w.Accounts[addr] = acct
}

// AddCode stores bytecode in the witness keyed by its hash.
func (w *StatelessWitness) AddCode(codeHash types.Hash, code []byte) {
	cp := make([]byte, len(code))
	copy(cp, code)
	w.Codes[codeHash] = cp
}

// Verify validates that the witness data is consistent with the expected
// state root. It rebuilds a Merkle Patricia Trie from the witness accounts
// and compares the resulting root to StateRoot.
func (w *StatelessWitness) Verify(expectedRoot types.Hash) error {
	if len(w.Accounts) == 0 {
		// An empty witness matches only the empty root.
		if expectedRoot != types.EmptyRootHash {
			return ErrWitnessRootMismatch
		}
		return nil
	}

	stateTrie := trie.New()
	for addr, acct := range w.Accounts {
		if !acct.Exists {
			continue
		}

		// Compute the storage root for this account.
		storageRoot := computeWitnessStorageRoot(acct)

		codeHash := acct.CodeHash
		if codeHash == (types.Hash{}) {
			codeHash = types.EmptyCodeHash
		}

		acc := rlpAccount{
			Nonce:    acct.Nonce,
			Balance:  acct.Balance,
			Root:     storageRoot[:],
			CodeHash: codeHash.Bytes(),
		}
		encoded, err := rlp.EncodeToBytes(acc)
		if err != nil {
			continue
		}
		hashedAddr := crypto.Keccak256(addr[:])
		stateTrie.Put(hashedAddr, encoded)
	}

	computedRoot := stateTrie.Hash()
	if computedRoot != expectedRoot {
		return ErrWitnessRootMismatch
	}
	return nil
}

// computeWitnessStorageRoot builds a storage trie from the witness account's
// storage slots.
func computeWitnessStorageRoot(acct *WitnessAccount) types.Hash {
	if len(acct.Storage) == 0 {
		return types.EmptyRootHash
	}
	storageTrie := trie.New()
	for slot, val := range acct.Storage {
		if val == (types.Hash{}) {
			continue
		}
		hashedSlot := crypto.Keccak256(slot[:])
		trimmed := trimLeadingZeros(val[:])
		encoded, err := rlp.EncodeToBytes(trimmed)
		if err != nil {
			continue
		}
		storageTrie.Put(hashedSlot, encoded)
	}
	return storageTrie.Hash()
}

// StatelessStateDB is a StateDB backed entirely by witness data. It reads
// account state, storage, and code from a StatelessWitness and accumulates
// mutations in an internal journal, enabling stateless block execution.
type StatelessStateDB struct {
	witness *StatelessWitness

	// Overlay state: dirty writes on top of witness data.
	accounts         map[types.Address]*stateObject
	logs             map[types.Hash][]*types.Log
	refund           uint64
	accessList       *accessList
	transientStorage map[types.Address]map[types.Hash]types.Hash

	// Current transaction context.
	txHash  types.Hash
	txIndex int

	// Simple journal for snapshot/revert. Each snapshot records the
	// full set of dirty accounts so we can restore on revert.
	snapshots  map[int]*statelessSnapshot
	nextSnapID int
}

// statelessSnapshot captures a point-in-time copy of the overlay state.
type statelessSnapshot struct {
	accounts map[types.Address]*stateObject
	refund   uint64
	logs     map[types.Hash][]*types.Log
}

// NewStatelessStateDB creates a new stateless state database backed by witness
// data. All reads are served from the witness; writes accumulate in an overlay.
func NewStatelessStateDB(witness *StatelessWitness) *StatelessStateDB {
	return &StatelessStateDB{
		witness:          witness,
		accounts:         make(map[types.Address]*stateObject),
		logs:             make(map[types.Hash][]*types.Log),
		accessList:       newAccessList(),
		transientStorage: make(map[types.Address]map[types.Hash]types.Hash),
		snapshots:        make(map[int]*statelessSnapshot),
	}
}

// getOrLoadAccount returns the overlay stateObject for addr, creating one
// from the witness if it does not yet exist in the overlay.
func (s *StatelessStateDB) getOrLoadAccount(addr types.Address) *stateObject {
	if obj, ok := s.accounts[addr]; ok {
		return obj
	}
	obj := newStateObject()
	if wAcct, ok := s.witness.Accounts[addr]; ok && wAcct.Exists {
		obj.account.Nonce = wAcct.Nonce
		if wAcct.Balance != nil {
			obj.account.Balance = new(big.Int).Set(wAcct.Balance)
		}
		if wAcct.CodeHash != (types.Hash{}) {
			obj.account.CodeHash = wAcct.CodeHash.Bytes()
		}
		// Load code from witness if available.
		codeHash := types.BytesToHash(obj.account.CodeHash)
		if code, ok := s.witness.Codes[codeHash]; ok {
			obj.code = make([]byte, len(code))
			copy(obj.code, code)
		}
		// Load storage from witness.
		for slot, val := range wAcct.Storage {
			obj.committedStorage[slot] = val
		}
	}
	s.accounts[addr] = obj
	return obj
}

// existsInWitness checks whether the address exists in the witness.
func (s *StatelessStateDB) existsInWitness(addr types.Address) bool {
	if wAcct, ok := s.witness.Accounts[addr]; ok {
		return wAcct.Exists
	}
	return false
}

// --- Account operations ---

func (s *StatelessStateDB) CreateAccount(addr types.Address) {
	s.accounts[addr] = newStateObject()
}

func (s *StatelessStateDB) SubBalance(addr types.Address, amount *big.Int) {
	obj := s.getOrLoadAccount(addr)
	obj.account.Balance = new(big.Int).Sub(obj.account.Balance, amount)
}

func (s *StatelessStateDB) AddBalance(addr types.Address, amount *big.Int) {
	obj := s.getOrLoadAccount(addr)
	obj.account.Balance = new(big.Int).Add(obj.account.Balance, amount)
}

func (s *StatelessStateDB) GetBalance(addr types.Address) *big.Int {
	obj := s.getOrLoadAccount(addr)
	return new(big.Int).Set(obj.account.Balance)
}

func (s *StatelessStateDB) GetNonce(addr types.Address) uint64 {
	obj := s.getOrLoadAccount(addr)
	return obj.account.Nonce
}

func (s *StatelessStateDB) SetNonce(addr types.Address, nonce uint64) {
	obj := s.getOrLoadAccount(addr)
	obj.account.Nonce = nonce
}

func (s *StatelessStateDB) GetCode(addr types.Address) []byte {
	obj := s.getOrLoadAccount(addr)
	return obj.code
}

func (s *StatelessStateDB) SetCode(addr types.Address, code []byte) {
	obj := s.getOrLoadAccount(addr)
	obj.code = code
	obj.account.CodeHash = crypto.Keccak256(code)
}

func (s *StatelessStateDB) GetCodeHash(addr types.Address) types.Hash {
	obj := s.getOrLoadAccount(addr)
	return types.BytesToHash(obj.account.CodeHash)
}

func (s *StatelessStateDB) GetCodeSize(addr types.Address) int {
	obj := s.getOrLoadAccount(addr)
	return len(obj.code)
}

// --- Self-destruct ---

func (s *StatelessStateDB) SelfDestruct(addr types.Address) {
	if _, ok := s.accounts[addr]; !ok {
		if !s.existsInWitness(addr) {
			return
		}
	}
	obj := s.getOrLoadAccount(addr)
	obj.selfDestructed = true
	obj.account.Balance = new(big.Int)
}

func (s *StatelessStateDB) HasSelfDestructed(addr types.Address) bool {
	if obj, ok := s.accounts[addr]; ok {
		return obj.selfDestructed
	}
	return false
}

// --- Storage operations ---

func (s *StatelessStateDB) GetState(addr types.Address, key types.Hash) types.Hash {
	obj := s.getOrLoadAccount(addr)
	if val, ok := obj.dirtyStorage[key]; ok {
		return val
	}
	return obj.committedStorage[key]
}

func (s *StatelessStateDB) SetState(addr types.Address, key types.Hash, value types.Hash) {
	obj := s.getOrLoadAccount(addr)
	obj.dirtyStorage[key] = value
}

func (s *StatelessStateDB) GetCommittedState(addr types.Address, key types.Hash) types.Hash {
	obj := s.getOrLoadAccount(addr)
	return obj.committedStorage[key]
}

// --- Account existence ---

func (s *StatelessStateDB) Exist(addr types.Address) bool {
	if obj, ok := s.accounts[addr]; ok {
		return !obj.selfDestructed
	}
	return s.existsInWitness(addr)
}

func (s *StatelessStateDB) Empty(addr types.Address) bool {
	if _, ok := s.accounts[addr]; !ok {
		if !s.existsInWitness(addr) {
			return true
		}
	}
	obj := s.getOrLoadAccount(addr)
	return obj.account.Nonce == 0 &&
		obj.account.Balance.Sign() == 0 &&
		types.BytesToHash(obj.account.CodeHash) == types.EmptyCodeHash
}

// --- Snapshot and revert ---

func (s *StatelessStateDB) Snapshot() int {
	id := s.nextSnapID
	s.nextSnapID++

	// Deep-copy the overlay accounts.
	acctsCopy := make(map[types.Address]*stateObject, len(s.accounts))
	for addr, obj := range s.accounts {
		cp := &stateObject{
			account: types.Account{
				Nonce:    obj.account.Nonce,
				Balance:  new(big.Int).Set(obj.account.Balance),
				Root:     obj.account.Root,
				CodeHash: make([]byte, len(obj.account.CodeHash)),
			},
			code:             make([]byte, len(obj.code)),
			dirtyStorage:     make(map[types.Hash]types.Hash, len(obj.dirtyStorage)),
			committedStorage: make(map[types.Hash]types.Hash, len(obj.committedStorage)),
			selfDestructed:   obj.selfDestructed,
		}
		copy(cp.account.CodeHash, obj.account.CodeHash)
		copy(cp.code, obj.code)
		for k, v := range obj.dirtyStorage {
			cp.dirtyStorage[k] = v
		}
		for k, v := range obj.committedStorage {
			cp.committedStorage[k] = v
		}
		acctsCopy[addr] = cp
	}

	logsCopy := make(map[types.Hash][]*types.Log, len(s.logs))
	for h, ls := range s.logs {
		lc := make([]*types.Log, len(ls))
		for i, l := range ls {
			lcp := *l
			lc[i] = &lcp
		}
		logsCopy[h] = lc
	}

	s.snapshots[id] = &statelessSnapshot{
		accounts: acctsCopy,
		refund:   s.refund,
		logs:     logsCopy,
	}
	return id
}

func (s *StatelessStateDB) RevertToSnapshot(id int) {
	snap, ok := s.snapshots[id]
	if !ok {
		return
	}
	s.accounts = snap.accounts
	s.refund = snap.refund
	s.logs = snap.logs

	// Remove this and all later snapshots.
	for sid := range s.snapshots {
		if sid >= id {
			delete(s.snapshots, sid)
		}
	}
}

// --- Logs ---

func (s *StatelessStateDB) AddLog(log *types.Log) {
	txHash := s.txHash
	log.TxHash = txHash
	log.TxIndex = uint(s.txIndex)
	s.logs[txHash] = append(s.logs[txHash], log)
}

func (s *StatelessStateDB) GetLogs(txHash types.Hash) []*types.Log {
	return s.logs[txHash]
}

func (s *StatelessStateDB) SetTxContext(txHash types.Hash, txIndex int) {
	s.txHash = txHash
	s.txIndex = txIndex
}

// --- Refund counter ---

func (s *StatelessStateDB) AddRefund(gas uint64) {
	s.refund += gas
}

func (s *StatelessStateDB) SubRefund(gas uint64) {
	s.refund -= gas
}

func (s *StatelessStateDB) GetRefund() uint64 {
	return s.refund
}

// --- Access list (EIP-2929) ---

func (s *StatelessStateDB) AddAddressToAccessList(addr types.Address) {
	s.accessList.AddAddress(addr)
}

func (s *StatelessStateDB) AddSlotToAccessList(addr types.Address, slot types.Hash) {
	s.accessList.AddSlot(addr, slot)
}

func (s *StatelessStateDB) AddressInAccessList(addr types.Address) bool {
	return s.accessList.ContainsAddress(addr)
}

func (s *StatelessStateDB) SlotInAccessList(addr types.Address, slot types.Hash) (bool, bool) {
	return s.accessList.ContainsSlot(addr, slot)
}

// --- Transient storage (EIP-1153) ---

func (s *StatelessStateDB) GetTransientState(addr types.Address, key types.Hash) types.Hash {
	if slots, ok := s.transientStorage[addr]; ok {
		return slots[key]
	}
	return types.Hash{}
}

func (s *StatelessStateDB) SetTransientState(addr types.Address, key types.Hash, value types.Hash) {
	if _, ok := s.transientStorage[addr]; !ok {
		s.transientStorage[addr] = make(map[types.Hash]types.Hash)
	}
	s.transientStorage[addr][key] = value
}

func (s *StatelessStateDB) ClearTransientStorage() {
	s.transientStorage = make(map[types.Address]map[types.Hash]types.Hash)
}

// --- Root computation ---

// GetRoot computes the state root from the overlay state merged with the
// witness data.
func (s *StatelessStateDB) GetRoot() types.Hash {
	// Ensure all witness accounts are loaded into the overlay.
	for addr := range s.witness.Accounts {
		s.getOrLoadAccount(addr)
	}

	if len(s.accounts) == 0 {
		return types.EmptyRootHash
	}

	stateTrie := trie.New()
	for addr, obj := range s.accounts {
		if obj.selfDestructed {
			continue
		}
		storageRoot := computeStorageRoot(obj)
		codeHash := obj.account.CodeHash
		if len(codeHash) == 0 {
			codeHash = types.EmptyCodeHash.Bytes()
		}
		acc := rlpAccount{
			Nonce:    obj.account.Nonce,
			Balance:  obj.account.Balance,
			Root:     storageRoot[:],
			CodeHash: codeHash,
		}
		encoded, err := rlp.EncodeToBytes(acc)
		if err != nil {
			continue
		}
		hashedAddr := crypto.Keccak256(addr[:])
		stateTrie.Put(hashedAddr, encoded)
	}
	return stateTrie.Hash()
}

// StorageRoot computes the storage root for the given address.
func (s *StatelessStateDB) StorageRoot(addr types.Address) types.Hash {
	obj := s.getOrLoadAccount(addr)
	return computeStorageRoot(obj)
}

// Commit flushes dirty storage into committed storage and returns the state root.
func (s *StatelessStateDB) Commit() (types.Hash, error) {
	// Ensure all witness accounts are in the overlay.
	for addr := range s.witness.Accounts {
		s.getOrLoadAccount(addr)
	}

	for _, obj := range s.accounts {
		for key, val := range obj.dirtyStorage {
			if val == (types.Hash{}) {
				delete(obj.committedStorage, key)
			} else {
				obj.committedStorage[key] = val
			}
		}
		obj.dirtyStorage = make(map[types.Hash]types.Hash)
	}
	return s.GetRoot(), nil
}

// DirtyAccounts returns the set of addresses that have been modified in
// the overlay. Useful for building post-execution witness diffs.
func (s *StatelessStateDB) DirtyAccounts() []types.Address {
	addrs := make([]types.Address, 0, len(s.accounts))
	for addr := range s.accounts {
		addrs = append(addrs, addr)
	}
	return addrs
}

// Verify validates the underlying witness against the expected state root.
func (s *StatelessStateDB) Verify(expectedRoot types.Hash) error {
	return s.witness.Verify(expectedRoot)
}

// Verify interface compliance at compile time.
var _ StateDB = (*StatelessStateDB)(nil)
