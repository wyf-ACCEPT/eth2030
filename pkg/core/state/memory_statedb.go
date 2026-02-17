package state

import (
	"math/big"

	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/crypto"
)

// stateObject represents an Ethereum account with its associated state.
type stateObject struct {
	account          types.Account
	code             []byte
	dirtyStorage     map[types.Hash]types.Hash
	committedStorage map[types.Hash]types.Hash
	selfDestructed   bool
}

func newStateObject() *stateObject {
	return &stateObject{
		account:          types.NewAccount(),
		dirtyStorage:     make(map[types.Hash]types.Hash),
		committedStorage: make(map[types.Hash]types.Hash),
	}
}

// MemoryStateDB is an in-memory implementation of the StateDB interface.
type MemoryStateDB struct {
	stateObjects     map[types.Address]*stateObject
	journal          *journal
	logs             map[types.Hash][]*types.Log
	refund           uint64
	accessList       *accessList
	transientStorage map[types.Address]map[types.Hash]types.Hash
}

// NewMemoryStateDB creates a new in-memory state database.
func NewMemoryStateDB() *MemoryStateDB {
	return &MemoryStateDB{
		stateObjects:     make(map[types.Address]*stateObject),
		journal:          newJournal(),
		logs:             make(map[types.Hash][]*types.Log),
		accessList:       newAccessList(),
		transientStorage: make(map[types.Address]map[types.Hash]types.Hash),
	}
}

func (s *MemoryStateDB) getStateObject(addr types.Address) *stateObject {
	return s.stateObjects[addr]
}

func (s *MemoryStateDB) getOrNewStateObject(addr types.Address) *stateObject {
	if obj := s.stateObjects[addr]; obj != nil {
		return obj
	}
	obj := newStateObject()
	s.stateObjects[addr] = obj
	return obj
}

// --- Account operations ---

func (s *MemoryStateDB) CreateAccount(addr types.Address) {
	s.journal.append(createAccountChange{addr: addr})
	s.stateObjects[addr] = newStateObject()
}

func (s *MemoryStateDB) SubBalance(addr types.Address, amount *big.Int) {
	obj := s.getOrNewStateObject(addr)
	s.journal.append(balanceChange{addr: addr, prev: new(big.Int).Set(obj.account.Balance)})
	obj.account.Balance = new(big.Int).Sub(obj.account.Balance, amount)
}

func (s *MemoryStateDB) AddBalance(addr types.Address, amount *big.Int) {
	obj := s.getOrNewStateObject(addr)
	s.journal.append(balanceChange{addr: addr, prev: new(big.Int).Set(obj.account.Balance)})
	obj.account.Balance = new(big.Int).Add(obj.account.Balance, amount)
}

func (s *MemoryStateDB) GetBalance(addr types.Address) *big.Int {
	if obj := s.getStateObject(addr); obj != nil {
		return new(big.Int).Set(obj.account.Balance)
	}
	return new(big.Int)
}

func (s *MemoryStateDB) GetNonce(addr types.Address) uint64 {
	if obj := s.getStateObject(addr); obj != nil {
		return obj.account.Nonce
	}
	return 0
}

func (s *MemoryStateDB) SetNonce(addr types.Address, nonce uint64) {
	obj := s.getOrNewStateObject(addr)
	s.journal.append(nonceChange{addr: addr, prev: obj.account.Nonce})
	obj.account.Nonce = nonce
}

func (s *MemoryStateDB) GetCode(addr types.Address) []byte {
	if obj := s.getStateObject(addr); obj != nil {
		return obj.code
	}
	return nil
}

func (s *MemoryStateDB) SetCode(addr types.Address, code []byte) {
	obj := s.getOrNewStateObject(addr)
	prevCode := obj.code
	prevHash := make([]byte, len(obj.account.CodeHash))
	copy(prevHash, obj.account.CodeHash)
	s.journal.append(codeChange{addr: addr, prevCode: prevCode, prevHash: prevHash})
	obj.code = code
	obj.account.CodeHash = crypto.Keccak256(code)
}

func (s *MemoryStateDB) GetCodeHash(addr types.Address) types.Hash {
	if obj := s.getStateObject(addr); obj != nil {
		return types.BytesToHash(obj.account.CodeHash)
	}
	return types.Hash{}
}

func (s *MemoryStateDB) GetCodeSize(addr types.Address) int {
	if obj := s.getStateObject(addr); obj != nil {
		return len(obj.code)
	}
	return 0
}

// --- Self-destruct ---

func (s *MemoryStateDB) SelfDestruct(addr types.Address) {
	obj := s.getStateObject(addr)
	if obj == nil {
		return
	}
	s.journal.append(selfDestructChange{
		addr:           addr,
		prevDestructed: obj.selfDestructed,
		prevBalance:    new(big.Int).Set(obj.account.Balance),
	})
	obj.selfDestructed = true
	obj.account.Balance = new(big.Int)
}

func (s *MemoryStateDB) HasSelfDestructed(addr types.Address) bool {
	if obj := s.getStateObject(addr); obj != nil {
		return obj.selfDestructed
	}
	return false
}

// --- Storage operations ---

func (s *MemoryStateDB) GetState(addr types.Address, key types.Hash) types.Hash {
	if obj := s.getStateObject(addr); obj != nil {
		if val, ok := obj.dirtyStorage[key]; ok {
			return val
		}
		return obj.committedStorage[key]
	}
	return types.Hash{}
}

func (s *MemoryStateDB) SetState(addr types.Address, key types.Hash, value types.Hash) {
	obj := s.getOrNewStateObject(addr)
	prev := s.GetState(addr, key)
	s.journal.append(storageChange{addr: addr, key: key, prev: prev})
	obj.dirtyStorage[key] = value
}

func (s *MemoryStateDB) GetCommittedState(addr types.Address, key types.Hash) types.Hash {
	if obj := s.getStateObject(addr); obj != nil {
		return obj.committedStorage[key]
	}
	return types.Hash{}
}

// --- Account existence ---

func (s *MemoryStateDB) Exist(addr types.Address) bool {
	return s.stateObjects[addr] != nil
}

func (s *MemoryStateDB) Empty(addr types.Address) bool {
	obj := s.getStateObject(addr)
	if obj == nil {
		return true
	}
	return obj.account.Nonce == 0 &&
		obj.account.Balance.Sign() == 0 &&
		types.BytesToHash(obj.account.CodeHash) == types.EmptyCodeHash
}

// --- Snapshot and revert ---

func (s *MemoryStateDB) Snapshot() int {
	return s.journal.snapshot()
}

func (s *MemoryStateDB) RevertToSnapshot(id int) {
	s.journal.revertToSnapshot(id, s)
}

// --- Logs ---

func (s *MemoryStateDB) AddLog(log *types.Log) {
	txHash := log.TxHash
	s.journal.append(logChange{txHash: txHash, prevLen: len(s.logs[txHash])})
	s.logs[txHash] = append(s.logs[txHash], log)
}

func (s *MemoryStateDB) GetLogs(txHash types.Hash) []*types.Log {
	return s.logs[txHash]
}

// --- Refund counter ---

func (s *MemoryStateDB) AddRefund(gas uint64) {
	s.journal.append(refundChange{prev: s.refund})
	s.refund += gas
}

func (s *MemoryStateDB) SubRefund(gas uint64) {
	s.journal.append(refundChange{prev: s.refund})
	s.refund -= gas
}

func (s *MemoryStateDB) GetRefund() uint64 {
	return s.refund
}

// --- Access list (EIP-2929) ---

func (s *MemoryStateDB) AddAddressToAccessList(addr types.Address) {
	if !s.accessList.AddAddress(addr) {
		s.journal.append(accessListAddAccountChange{addr: addr})
	}
}

func (s *MemoryStateDB) AddSlotToAccessList(addr types.Address, slot types.Hash) {
	addrPresent, slotPresent := s.accessList.AddSlot(addr, slot)
	if !addrPresent {
		s.journal.append(accessListAddAccountChange{addr: addr})
	}
	if !slotPresent {
		s.journal.append(accessListAddSlotChange{addr: addr, slot: slot})
	}
}

func (s *MemoryStateDB) AddressInAccessList(addr types.Address) bool {
	return s.accessList.ContainsAddress(addr)
}

func (s *MemoryStateDB) SlotInAccessList(addr types.Address, slot types.Hash) (addressOk bool, slotOk bool) {
	return s.accessList.ContainsSlot(addr, slot)
}

// --- Transient storage (EIP-1153) ---

func (s *MemoryStateDB) GetTransientState(addr types.Address, key types.Hash) types.Hash {
	if slots, ok := s.transientStorage[addr]; ok {
		return slots[key]
	}
	return types.Hash{}
}

func (s *MemoryStateDB) SetTransientState(addr types.Address, key types.Hash, value types.Hash) {
	prev := s.GetTransientState(addr, key)
	s.journal.append(transientStorageChange{addr: addr, key: key, prev: prev})
	if _, ok := s.transientStorage[addr]; !ok {
		s.transientStorage[addr] = make(map[types.Hash]types.Hash)
	}
	s.transientStorage[addr][key] = value
}

// --- Commit ---

func (s *MemoryStateDB) Commit() (types.Hash, error) {
	// Flush dirty storage to committed storage and compute a state root.
	for _, obj := range s.stateObjects {
		for key, val := range obj.dirtyStorage {
			if val == (types.Hash{}) {
				delete(obj.committedStorage, key)
			} else {
				obj.committedStorage[key] = val
			}
		}
		obj.dirtyStorage = make(map[types.Hash]types.Hash)
	}

	// Compute a deterministic root from account data. For a full implementation
	// this would be a Merkle Patricia Trie root; here we hash all account state.
	hasher := make([]byte, 0, 256)
	for addr, obj := range s.stateObjects {
		hasher = append(hasher, addr[:]...)
		hasher = append(hasher, obj.account.CodeHash...)
		hasher = append(hasher, obj.account.Balance.Bytes()...)
	}
	if len(hasher) == 0 {
		return types.EmptyRootHash, nil
	}
	return crypto.Keccak256Hash(hasher), nil
}

// Verify interface compliance at compile time.
var _ StateDB = (*MemoryStateDB)(nil)
