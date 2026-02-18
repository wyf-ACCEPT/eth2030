package witness

import (
	"math/big"

	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/core/vm"
)

// WitnessCollector wraps a vm.StateDB and records all state reads into a
// BlockWitness. Write operations are delegated to the inner StateDB without
// being recorded in the witness (the witness captures pre-state only).
type WitnessCollector struct {
	inner    vm.StateDB
	witness  *BlockWitness
	recorded map[types.Address]bool // tracks which accounts have been snapshotted
}

// NewWitnessCollector creates a WitnessCollector that delegates to inner and
// accumulates read data in the provided witness.
func NewWitnessCollector(inner vm.StateDB, witness *BlockWitness) *WitnessCollector {
	return &WitnessCollector{
		inner:    inner,
		witness:  witness,
		recorded: make(map[types.Address]bool),
	}
}

// Witness returns the accumulated block witness.
func (w *WitnessCollector) Witness() *BlockWitness {
	return w.witness
}

// --- Account read operations (recorded in witness) ---

func (w *WitnessCollector) GetBalance(addr types.Address) *big.Int {
	bal := w.inner.GetBalance(addr)
	w.recordAccount(addr)
	return bal
}

func (w *WitnessCollector) GetNonce(addr types.Address) uint64 {
	nonce := w.inner.GetNonce(addr)
	w.recordAccount(addr)
	return nonce
}

func (w *WitnessCollector) GetCode(addr types.Address) []byte {
	code := w.inner.GetCode(addr)
	w.recordAccount(addr)
	if len(code) > 0 {
		codeHash := w.inner.GetCodeHash(addr)
		w.witness.AddCode(codeHash, code)
	}
	return code
}

func (w *WitnessCollector) GetCodeHash(addr types.Address) types.Hash {
	h := w.inner.GetCodeHash(addr)
	w.recordAccount(addr)
	return h
}

func (w *WitnessCollector) GetCodeSize(addr types.Address) int {
	size := w.inner.GetCodeSize(addr)
	w.recordAccount(addr)
	return size
}

func (w *WitnessCollector) GetState(addr types.Address, key types.Hash) types.Hash {
	val := w.inner.GetState(addr, key)
	w.recordAccount(addr)
	aw := w.witness.TouchAccount(addr)
	// Record the slot value only on first access.
	if _, seen := aw.Storage[key]; !seen {
		aw.Storage[key] = val
	}
	return val
}

func (w *WitnessCollector) GetCommittedState(addr types.Address, key types.Hash) types.Hash {
	val := w.inner.GetCommittedState(addr, key)
	w.recordAccount(addr)
	aw := w.witness.TouchAccount(addr)
	if _, seen := aw.Storage[key]; !seen {
		aw.Storage[key] = val
	}
	return val
}

func (w *WitnessCollector) Exist(addr types.Address) bool {
	exists := w.inner.Exist(addr)
	w.recordAccount(addr)
	return exists
}

func (w *WitnessCollector) Empty(addr types.Address) bool {
	empty := w.inner.Empty(addr)
	w.recordAccount(addr)
	return empty
}

func (w *WitnessCollector) HasSelfDestructed(addr types.Address) bool {
	return w.inner.HasSelfDestructed(addr)
}

// --- Account write operations (delegated, not recorded) ---

func (w *WitnessCollector) CreateAccount(addr types.Address) {
	// Record the pre-state before the account is created.
	w.recordAccount(addr)
	w.inner.CreateAccount(addr)
}

func (w *WitnessCollector) AddBalance(addr types.Address, amount *big.Int) {
	w.recordAccount(addr)
	w.inner.AddBalance(addr, amount)
}

func (w *WitnessCollector) SubBalance(addr types.Address, amount *big.Int) {
	w.recordAccount(addr)
	w.inner.SubBalance(addr, amount)
}

func (w *WitnessCollector) SetNonce(addr types.Address, nonce uint64) {
	w.recordAccount(addr)
	w.inner.SetNonce(addr, nonce)
}

func (w *WitnessCollector) SetCode(addr types.Address, code []byte) {
	w.recordAccount(addr)
	w.inner.SetCode(addr, code)
}

func (w *WitnessCollector) SetState(addr types.Address, key types.Hash, value types.Hash) {
	// Record the pre-state value for the slot before the write.
	w.recordAccount(addr)
	aw := w.witness.TouchAccount(addr)
	if _, seen := aw.Storage[key]; !seen {
		aw.Storage[key] = w.inner.GetState(addr, key)
	}
	w.inner.SetState(addr, key, value)
}

func (w *WitnessCollector) SelfDestruct(addr types.Address) {
	w.recordAccount(addr)
	w.inner.SelfDestruct(addr)
}

// --- Transient storage (not persisted, no witness needed) ---

func (w *WitnessCollector) GetTransientState(addr types.Address, key types.Hash) types.Hash {
	return w.inner.GetTransientState(addr, key)
}

func (w *WitnessCollector) SetTransientState(addr types.Address, key types.Hash, value types.Hash) {
	w.inner.SetTransientState(addr, key, value)
}

// --- Snapshot / Revert ---

func (w *WitnessCollector) Snapshot() int {
	return w.inner.Snapshot()
}

func (w *WitnessCollector) RevertToSnapshot(id int) {
	// Witness data is not reverted -- we always keep the pre-state values
	// even if the transaction that triggered the read is later reverted.
	// The verifier needs the pre-state regardless.
	w.inner.RevertToSnapshot(id)
}

// --- Logs ---

func (w *WitnessCollector) AddLog(log *types.Log) {
	w.inner.AddLog(log)
}

// --- Refund counter ---

func (w *WitnessCollector) AddRefund(gas uint64) {
	w.inner.AddRefund(gas)
}

func (w *WitnessCollector) SubRefund(gas uint64) {
	w.inner.SubRefund(gas)
}

func (w *WitnessCollector) GetRefund() uint64 {
	return w.inner.GetRefund()
}

// --- Access list (EIP-2929) ---

func (w *WitnessCollector) AddAddressToAccessList(addr types.Address) {
	w.inner.AddAddressToAccessList(addr)
}

func (w *WitnessCollector) AddSlotToAccessList(addr types.Address, slot types.Hash) {
	w.inner.AddSlotToAccessList(addr, slot)
}

func (w *WitnessCollector) AddressInAccessList(addr types.Address) bool {
	return w.inner.AddressInAccessList(addr)
}

func (w *WitnessCollector) SlotInAccessList(addr types.Address, slot types.Hash) (addressOk bool, slotOk bool) {
	return w.inner.SlotInAccessList(addr, slot)
}

// recordAccount snapshots the account's pre-state into the witness on first
// access. Subsequent calls for the same address are no-ops. The recorded map
// tracks addresses independently of the Exists flag, so that accounts that
// don't exist in the pre-state are not re-queried after creation.
func (w *WitnessCollector) recordAccount(addr types.Address) {
	if w.recorded[addr] {
		return
	}
	w.recorded[addr] = true
	aw := w.witness.TouchAccount(addr)
	exists := w.inner.Exist(addr)
	aw.Exists = exists
	if exists {
		aw.Balance = new(big.Int).Set(w.inner.GetBalance(addr))
		aw.Nonce = w.inner.GetNonce(addr)
		aw.CodeHash = w.inner.GetCodeHash(addr)
	}
}

// Compile-time assertion that WitnessCollector implements vm.StateDB.
var _ vm.StateDB = (*WitnessCollector)(nil)
