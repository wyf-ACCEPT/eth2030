package witness

import (
	"errors"
	"math/big"

	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/core/vm"
)

var (
	ErrAccountNotInWitness = errors.New("account not in witness")
	ErrSlotNotInWitness    = errors.New("storage slot not in witness")
	ErrCodeNotInWitness    = errors.New("code not in witness")
)

// WitnessStateDB implements vm.StateDB backed entirely by witness data. It
// allows re-executing a block without access to the full state trie. Writes
// are stored in an in-memory overlay.
type WitnessStateDB struct {
	witness *BlockWitness

	// overlay tracks all writes made during execution.
	accounts         map[types.Address]*witnessAccount
	logs             []*types.Log
	refund           uint64
	accessList       *witnessAccessList
	transientStorage map[types.Address]map[types.Hash]types.Hash
	snapshots        map[int]*witnessSnapshot
	nextSnapshotID   int
}

// witnessAccount tracks both the witness pre-state and any writes.
type witnessAccount struct {
	balance        *big.Int
	nonce          uint64
	codeHash       types.Hash
	code           []byte
	storage        map[types.Hash]types.Hash // current (dirty) state
	exists         bool
	selfDestructed bool
	created        bool // true if created during execution
}

type witnessSnapshot struct {
	accounts         map[types.Address]*witnessAccount
	refund           uint64
	logsLen          int
	accessList       *witnessAccessList
	transientStorage map[types.Address]map[types.Hash]types.Hash
}

// NewWitnessStateDB creates a state database backed by the given witness.
func NewWitnessStateDB(w *BlockWitness) *WitnessStateDB {
	sdb := &WitnessStateDB{
		witness:          w,
		accounts:         make(map[types.Address]*witnessAccount),
		accessList:       newWitnessAccessList(),
		transientStorage: make(map[types.Address]map[types.Hash]types.Hash),
		snapshots:        make(map[int]*witnessSnapshot),
	}
	// Initialize overlay from witness pre-state.
	for addr, aw := range w.State {
		wa := &witnessAccount{
			balance:  new(big.Int).Set(aw.Balance),
			nonce:    aw.Nonce,
			codeHash: aw.CodeHash,
			exists:   aw.Exists,
			storage:  make(map[types.Hash]types.Hash),
		}
		// Copy witness storage as the initial state.
		for k, v := range aw.Storage {
			wa.storage[k] = v
		}
		// Resolve code from witness codes map.
		if code, ok := w.Codes[aw.CodeHash]; ok {
			wa.code = code
		}
		sdb.accounts[addr] = wa
	}
	return sdb
}

func (s *WitnessStateDB) getAccount(addr types.Address) *witnessAccount {
	return s.accounts[addr]
}

func (s *WitnessStateDB) getOrCreateAccount(addr types.Address) *witnessAccount {
	if wa := s.accounts[addr]; wa != nil {
		return wa
	}
	wa := &witnessAccount{
		balance: new(big.Int),
		storage: make(map[types.Hash]types.Hash),
	}
	s.accounts[addr] = wa
	return wa
}

// --- Account operations ---

func (s *WitnessStateDB) CreateAccount(addr types.Address) {
	wa := s.getOrCreateAccount(addr)
	wa.exists = true
	wa.created = true
	wa.balance = new(big.Int)
	wa.nonce = 0
	wa.codeHash = types.EmptyCodeHash
	wa.code = nil
	wa.storage = make(map[types.Hash]types.Hash)
	wa.selfDestructed = false
}

func (s *WitnessStateDB) GetBalance(addr types.Address) *big.Int {
	if wa := s.getAccount(addr); wa != nil && wa.exists {
		return new(big.Int).Set(wa.balance)
	}
	return new(big.Int)
}

func (s *WitnessStateDB) AddBalance(addr types.Address, amount *big.Int) {
	wa := s.getOrCreateAccount(addr)
	if !wa.exists {
		wa.exists = true
		wa.codeHash = types.EmptyCodeHash
	}
	wa.balance = new(big.Int).Add(wa.balance, amount)
}

func (s *WitnessStateDB) SubBalance(addr types.Address, amount *big.Int) {
	wa := s.getOrCreateAccount(addr)
	wa.balance = new(big.Int).Sub(wa.balance, amount)
}

func (s *WitnessStateDB) GetNonce(addr types.Address) uint64 {
	if wa := s.getAccount(addr); wa != nil && wa.exists {
		return wa.nonce
	}
	return 0
}

func (s *WitnessStateDB) SetNonce(addr types.Address, nonce uint64) {
	wa := s.getOrCreateAccount(addr)
	wa.nonce = nonce
}

func (s *WitnessStateDB) GetCode(addr types.Address) []byte {
	if wa := s.getAccount(addr); wa != nil {
		return wa.code
	}
	return nil
}

func (s *WitnessStateDB) SetCode(addr types.Address, code []byte) {
	wa := s.getOrCreateAccount(addr)
	cp := make([]byte, len(code))
	copy(cp, code)
	wa.code = cp
	// We don't compute keccak here to avoid importing crypto in the verifier.
	// The code hash will be set by the witness data or remain as-is.
}

func (s *WitnessStateDB) GetCodeHash(addr types.Address) types.Hash {
	if wa := s.getAccount(addr); wa != nil && wa.exists {
		return wa.codeHash
	}
	return types.Hash{}
}

func (s *WitnessStateDB) GetCodeSize(addr types.Address) int {
	if wa := s.getAccount(addr); wa != nil {
		return len(wa.code)
	}
	return 0
}

// --- Storage ---

func (s *WitnessStateDB) GetState(addr types.Address, key types.Hash) types.Hash {
	if wa := s.getAccount(addr); wa != nil {
		if val, ok := wa.storage[key]; ok {
			return val
		}
	}
	return types.Hash{}
}

func (s *WitnessStateDB) SetState(addr types.Address, key types.Hash, value types.Hash) {
	wa := s.getOrCreateAccount(addr)
	wa.storage[key] = value
}

func (s *WitnessStateDB) GetCommittedState(addr types.Address, key types.Hash) types.Hash {
	// In witness mode, committed state is the witness pre-state value.
	if aw, ok := s.witness.State[addr]; ok {
		if val, ok := aw.Storage[key]; ok {
			return val
		}
	}
	return types.Hash{}
}

// --- Transient storage (EIP-1153) ---

func (s *WitnessStateDB) GetTransientState(addr types.Address, key types.Hash) types.Hash {
	if slots, ok := s.transientStorage[addr]; ok {
		return slots[key]
	}
	return types.Hash{}
}

func (s *WitnessStateDB) SetTransientState(addr types.Address, key types.Hash, value types.Hash) {
	if _, ok := s.transientStorage[addr]; !ok {
		s.transientStorage[addr] = make(map[types.Hash]types.Hash)
	}
	s.transientStorage[addr][key] = value
}

func (s *WitnessStateDB) ClearTransientStorage() {
	s.transientStorage = make(map[types.Address]map[types.Hash]types.Hash)
}

// --- Self-destruct ---

func (s *WitnessStateDB) SelfDestruct(addr types.Address) {
	if wa := s.getAccount(addr); wa != nil {
		wa.selfDestructed = true
		wa.balance = new(big.Int)
	}
}

func (s *WitnessStateDB) HasSelfDestructed(addr types.Address) bool {
	if wa := s.getAccount(addr); wa != nil {
		return wa.selfDestructed
	}
	return false
}

// --- Account existence ---

func (s *WitnessStateDB) Exist(addr types.Address) bool {
	if wa := s.getAccount(addr); wa != nil {
		return wa.exists
	}
	return false
}

func (s *WitnessStateDB) Empty(addr types.Address) bool {
	wa := s.getAccount(addr)
	if wa == nil || !wa.exists {
		return true
	}
	return wa.nonce == 0 &&
		wa.balance.Sign() == 0 &&
		wa.codeHash == types.EmptyCodeHash
}

// --- Snapshot / Revert ---

func (s *WitnessStateDB) Snapshot() int {
	id := s.nextSnapshotID
	s.nextSnapshotID++

	snap := &witnessSnapshot{
		accounts:         s.copyAccounts(),
		refund:           s.refund,
		logsLen:          len(s.logs),
		accessList:       s.accessList.copy(),
		transientStorage: s.copyTransientStorage(),
	}
	s.snapshots[id] = snap
	return id
}

func (s *WitnessStateDB) RevertToSnapshot(id int) {
	snap, ok := s.snapshots[id]
	if !ok {
		return
	}
	s.accounts = snap.accounts
	s.refund = snap.refund
	s.logs = s.logs[:snap.logsLen]
	s.accessList = snap.accessList
	s.transientStorage = snap.transientStorage

	// Invalidate newer snapshots.
	for sid := range s.snapshots {
		if sid >= id {
			delete(s.snapshots, sid)
		}
	}
}

// --- Logs ---

func (s *WitnessStateDB) AddLog(log *types.Log) {
	s.logs = append(s.logs, log)
}

// --- Refund counter ---

func (s *WitnessStateDB) AddRefund(gas uint64) {
	s.refund += gas
}

func (s *WitnessStateDB) SubRefund(gas uint64) {
	s.refund -= gas
}

func (s *WitnessStateDB) GetRefund() uint64 {
	return s.refund
}

// --- Access list (EIP-2929) ---

func (s *WitnessStateDB) AddAddressToAccessList(addr types.Address) {
	s.accessList.addAddress(addr)
}

func (s *WitnessStateDB) AddSlotToAccessList(addr types.Address, slot types.Hash) {
	s.accessList.addSlot(addr, slot)
}

func (s *WitnessStateDB) AddressInAccessList(addr types.Address) bool {
	return s.accessList.containsAddress(addr)
}

func (s *WitnessStateDB) SlotInAccessList(addr types.Address, slot types.Hash) (addressOk bool, slotOk bool) {
	return s.accessList.containsSlot(addr, slot)
}

// --- Internal helpers ---

func (s *WitnessStateDB) copyAccounts() map[types.Address]*witnessAccount {
	cp := make(map[types.Address]*witnessAccount, len(s.accounts))
	for addr, wa := range s.accounts {
		nwa := &witnessAccount{
			balance:        new(big.Int).Set(wa.balance),
			nonce:          wa.nonce,
			codeHash:       wa.codeHash,
			code:           wa.code, // code is read-only, safe to share
			exists:         wa.exists,
			selfDestructed: wa.selfDestructed,
			created:        wa.created,
			storage:        make(map[types.Hash]types.Hash, len(wa.storage)),
		}
		for k, v := range wa.storage {
			nwa.storage[k] = v
		}
		cp[addr] = nwa
	}
	return cp
}

func (s *WitnessStateDB) copyTransientStorage() map[types.Address]map[types.Hash]types.Hash {
	cp := make(map[types.Address]map[types.Hash]types.Hash, len(s.transientStorage))
	for addr, slots := range s.transientStorage {
		cpSlots := make(map[types.Hash]types.Hash, len(slots))
		for k, v := range slots {
			cpSlots[k] = v
		}
		cp[addr] = cpSlots
	}
	return cp
}

// --- Minimal access list implementation for witness verification ---

type witnessAccessList struct {
	addresses map[types.Address]int
	slots     []map[types.Hash]struct{}
}

func newWitnessAccessList() *witnessAccessList {
	return &witnessAccessList{
		addresses: make(map[types.Address]int),
	}
}

func (al *witnessAccessList) addAddress(addr types.Address) {
	if _, ok := al.addresses[addr]; !ok {
		al.addresses[addr] = -1
	}
}

func (al *witnessAccessList) addSlot(addr types.Address, slot types.Hash) {
	idx, addrPresent := al.addresses[addr]
	if !addrPresent {
		al.addresses[addr] = len(al.slots)
		al.slots = append(al.slots, map[types.Hash]struct{}{slot: {}})
		return
	}
	if idx == -1 {
		al.addresses[addr] = len(al.slots)
		al.slots = append(al.slots, map[types.Hash]struct{}{slot: {}})
		return
	}
	al.slots[idx][slot] = struct{}{}
}

func (al *witnessAccessList) containsAddress(addr types.Address) bool {
	_, ok := al.addresses[addr]
	return ok
}

func (al *witnessAccessList) containsSlot(addr types.Address, slot types.Hash) (bool, bool) {
	idx, ok := al.addresses[addr]
	if !ok {
		return false, false
	}
	if idx == -1 {
		return true, false
	}
	_, slotOk := al.slots[idx][slot]
	return true, slotOk
}

func (al *witnessAccessList) copy() *witnessAccessList {
	cp := &witnessAccessList{
		addresses: make(map[types.Address]int, len(al.addresses)),
		slots:     make([]map[types.Hash]struct{}, len(al.slots)),
	}
	for k, v := range al.addresses {
		cp.addresses[k] = v
	}
	for i, m := range al.slots {
		cp.slots[i] = make(map[types.Hash]struct{}, len(m))
		for k := range m {
			cp.slots[i][k] = struct{}{}
		}
	}
	return cp
}

// Compile-time assertion that WitnessStateDB implements vm.StateDB.
var _ vm.StateDB = (*WitnessStateDB)(nil)
