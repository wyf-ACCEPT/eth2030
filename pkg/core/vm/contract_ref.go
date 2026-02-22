package vm

// contract_ref.go implements contract reference and caller tracking during
// EVM execution. It provides:
//   - AccountRef: wraps an external account address used as a call origin
//   - ContractRef: tracks the self address, caller, and delegated caller
//     for DELEGATECALL contexts, enabling correct CALLER/ADDRESS resolution
//   - CallerChain: maintains the full chain of callers through nested calls

import (
	"github.com/eth2030/eth2030/core/types"
)

// CallerReference is the interface for entities that can be a caller in the
// EVM. Both external accounts and contracts implement this.
type CallerReference interface {
	// RefAddress returns the address associated with this reference.
	RefAddress() types.Address
}

// AccountRef represents an external (non-contract) account address in the
// context of EVM execution. It is used for the transaction origin and for
// externally-owned accounts that initiate calls.
type AccountRef types.Address

// RefAddress returns the underlying address.
func (ar AccountRef) RefAddress() types.Address {
	return types.Address(ar)
}

// ContractRefInfo tracks the address relationships for a contract during
// execution. It resolves the correct addresses for CALLER, ADDRESS, and
// DELEGATECALL scenarios.
//
// For a normal CALL:
//   - SelfAddr is the contract being executed
//   - CallerAddr is the address that called this contract
//   - DelegatedCaller is nil
//
// For a DELEGATECALL:
//   - SelfAddr is the contract whose code is being executed (the delegate)
//   - CallerAddr is the original msg.sender from the parent context
//   - DelegatedCaller points to the parent contract reference
type ContractRefInfo struct {
	SelfAddr        types.Address // the address of this contract (ADDRESS opcode)
	CallerAddr      types.Address // the caller of this contract (CALLER opcode)
	DelegatedCaller *ContractRefInfo // non-nil for DELEGATECALL chains
	CodeAddr        types.Address // the address from which code was loaded
	IsDelegate      bool          // true if this is a DELEGATECALL context
}

// NewAccountContractRef creates a ContractRefInfo for an external account
// calling a contract. The caller is the account itself.
func NewAccountContractRef(account types.Address, target types.Address) *ContractRefInfo {
	return &ContractRefInfo{
		SelfAddr:   target,
		CallerAddr: account,
		CodeAddr:   target,
	}
}

// NewDelegateContractRef creates a ContractRefInfo for a DELEGATECALL
// context. In DELEGATECALL, the code runs at codeAddr but:
//   - ADDRESS returns the parent's self address (not the delegate's)
//   - CALLER returns the parent's caller (preserved from parent)
//   - Storage operations use the parent's storage
func NewDelegateContractRef(parent *ContractRefInfo, codeAddr types.Address) *ContractRefInfo {
	return &ContractRefInfo{
		SelfAddr:        parent.SelfAddr,   // preserve parent's self address
		CallerAddr:      parent.CallerAddr, // preserve parent's caller
		DelegatedCaller: parent,
		CodeAddr:        codeAddr,
		IsDelegate:      true,
	}
}

// NewCallCodeContractRef creates a ContractRefInfo for a CALLCODE context.
// In CALLCODE, the callee's code runs but:
//   - ADDRESS returns the caller's address (runs in caller's context)
//   - CALLER is the address that initiated the CALLCODE
//   - Storage operations use the caller's storage
func NewCallCodeContractRef(callerAddr types.Address, codeAddr types.Address) *ContractRefInfo {
	return &ContractRefInfo{
		SelfAddr:   callerAddr, // runs in caller's context
		CallerAddr: callerAddr,
		CodeAddr:   codeAddr,
	}
}

// RefAddress returns the address that should be used for the ADDRESS opcode.
func (cr *ContractRefInfo) RefAddress() types.Address {
	return cr.SelfAddr
}

// Caller returns the address that should be used for the CALLER opcode.
func (cr *ContractRefInfo) Caller() types.Address {
	return cr.CallerAddr
}

// CodeAddress returns the address from which the executing code was loaded.
// This differs from SelfAddr in DELEGATECALL and CALLCODE scenarios.
func (cr *ContractRefInfo) CodeAddress() types.Address {
	return cr.CodeAddr
}

// StorageAddress returns the address whose storage should be used for
// SLOAD/SSTORE operations. For DELEGATECALL and CALLCODE, this is the
// caller's address; for normal CALL, this is the contract's own address.
func (cr *ContractRefInfo) StorageAddress() types.Address {
	return cr.SelfAddr
}

// OriginCaller walks up the delegation chain to find the original external
// account that initiated the call chain. Returns the top-level caller.
func (cr *ContractRefInfo) OriginCaller() types.Address {
	current := cr
	for current.DelegatedCaller != nil {
		current = current.DelegatedCaller
	}
	return current.CallerAddr
}

// DelegationDepth returns how many levels of DELEGATECALL exist in the
// chain from this reference back to the original caller.
func (cr *ContractRefInfo) DelegationDepth() int {
	depth := 0
	current := cr
	for current.DelegatedCaller != nil {
		depth++
		current = current.DelegatedCaller
	}
	return depth
}

// CallerChain maintains an ordered list of caller references through
// nested CALL/DELEGATECALL/CALLCODE operations. It enables walking the
// full call chain for debugging, tracing, and access control checks.
type CallerChain struct {
	entries []*ContractRefInfo
}

// NewCallerChain creates an empty caller chain.
func NewCallerChain() *CallerChain {
	return &CallerChain{
		entries: make([]*ContractRefInfo, 0, 8),
	}
}

// Push adds a new contract reference to the chain (when entering a call).
func (cc *CallerChain) Push(ref *ContractRefInfo) {
	cc.entries = append(cc.entries, ref)
}

// Pop removes and returns the most recent reference (when returning from
// a call). Returns nil if the chain is empty.
func (cc *CallerChain) Pop() *ContractRefInfo {
	n := len(cc.entries)
	if n == 0 {
		return nil
	}
	ref := cc.entries[n-1]
	cc.entries = cc.entries[:n-1]
	return ref
}

// Current returns the current (most recent) contract reference without
// removing it. Returns nil if the chain is empty.
func (cc *CallerChain) Current() *ContractRefInfo {
	n := len(cc.entries)
	if n == 0 {
		return nil
	}
	return cc.entries[n-1]
}

// Depth returns the number of entries in the caller chain.
func (cc *CallerChain) Depth() int {
	return len(cc.entries)
}

// AtDepth returns the contract reference at the specified depth (0-indexed
// from the bottom of the chain). Returns nil if out of bounds.
func (cc *CallerChain) AtDepth(depth int) *ContractRefInfo {
	if depth < 0 || depth >= len(cc.entries) {
		return nil
	}
	return cc.entries[depth]
}

// Callers returns a slice of all caller addresses in the chain, from
// oldest to newest.
func (cc *CallerChain) Callers() []types.Address {
	addrs := make([]types.Address, len(cc.entries))
	for i, e := range cc.entries {
		addrs[i] = e.CallerAddr
	}
	return addrs
}

// HasDelegation returns true if any entry in the chain is a DELEGATECALL.
func (cc *CallerChain) HasDelegation() bool {
	for _, e := range cc.entries {
		if e.IsDelegate {
			return true
		}
	}
	return false
}
