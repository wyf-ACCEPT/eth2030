package state

import (
	"math/big"

	"github.com/eth2028/eth2028/core/types"
)

// StateDB is an interface for managing Ethereum world state.
type StateDB interface {
	// Account operations
	CreateAccount(addr types.Address)
	SubBalance(addr types.Address, amount *big.Int)
	AddBalance(addr types.Address, amount *big.Int)
	GetBalance(addr types.Address) *big.Int
	GetNonce(addr types.Address) uint64
	SetNonce(addr types.Address, nonce uint64)
	GetCode(addr types.Address) []byte
	SetCode(addr types.Address, code []byte)
	GetCodeHash(addr types.Address) types.Hash
	GetCodeSize(addr types.Address) int

	// Self-destruct
	SelfDestruct(addr types.Address)
	HasSelfDestructed(addr types.Address) bool

	// Storage operations
	GetState(addr types.Address, key types.Hash) types.Hash
	SetState(addr types.Address, key types.Hash, value types.Hash)
	GetCommittedState(addr types.Address, key types.Hash) types.Hash

	// Account existence
	Exist(addr types.Address) bool
	Empty(addr types.Address) bool

	// Snapshot and revert for tx-level atomicity
	Snapshot() int
	RevertToSnapshot(id int)

	// Logs
	AddLog(log *types.Log)
	GetLogs(txHash types.Hash) []*types.Log

	// Refund counter
	AddRefund(gas uint64)
	SubRefund(gas uint64)
	GetRefund() uint64

	// Access list (EIP-2929 warm/cold tracking)
	AddAddressToAccessList(addr types.Address)
	AddSlotToAccessList(addr types.Address, slot types.Hash)
	AddressInAccessList(addr types.Address) bool
	SlotInAccessList(addr types.Address, slot types.Hash) (addressOk bool, slotOk bool)

	// Transient storage (EIP-1153)
	GetTransientState(addr types.Address, key types.Hash) types.Hash
	SetTransientState(addr types.Address, key types.Hash, value types.Hash)

	// Commit
	Commit() (types.Hash, error)
}
