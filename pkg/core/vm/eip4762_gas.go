package vm

import (
	"github.com/eth2028/eth2028/core/types"
)

// WitnessGasTracker maintains the four sets specified by EIP-4762 for tracking
// which Verkle tree subtrees and leaves have been accessed/edited during a
// transaction. It charges gas for the first access or edit of each unique
// (address, subKey) subtree and (address, subKey, leafKey) leaf.
type WitnessGasTracker struct {
	accessedSubtrees map[witnessSubtreeKey]bool
	accessedLeaves   map[witnessLeafKey]bool
	editedSubtrees   map[witnessSubtreeKey]bool
	editedLeaves     map[witnessLeafKey]bool
}

type witnessSubtreeKey struct {
	addr   types.Address
	subKey uint64
}

type witnessLeafKey struct {
	addr    types.Address
	subKey  uint64
	leafKey uint8
}

// NewWitnessGasTracker creates a new tracker with empty sets.
func NewWitnessGasTracker() *WitnessGasTracker {
	return &WitnessGasTracker{
		accessedSubtrees: make(map[witnessSubtreeKey]bool),
		accessedLeaves:   make(map[witnessLeafKey]bool),
		editedSubtrees:   make(map[witnessSubtreeKey]bool),
		editedLeaves:     make(map[witnessLeafKey]bool),
	}
}

// TouchAccessEvent computes the witness gas for an access event at
// (address, subKey, leafKey). It returns the gas to charge. If the subtree
// or leaf has already been accessed, no gas is charged for that component.
func (t *WitnessGasTracker) TouchAccessEvent(addr types.Address, subKey uint64, leafKey uint8) uint64 {
	var gas uint64

	sk := witnessSubtreeKey{addr: addr, subKey: subKey}
	if !t.accessedSubtrees[sk] {
		t.accessedSubtrees[sk] = true
		gas = safeAdd(gas, WitnessBranchCost)
	}

	lk := witnessLeafKey{addr: addr, subKey: subKey, leafKey: leafKey}
	if !t.accessedLeaves[lk] {
		t.accessedLeaves[lk] = true
		gas = safeAdd(gas, WitnessChunkCost)
	}

	return gas
}

// TouchWriteEvent computes the witness gas for a write event at
// (address, subKey, leafKey). This is charged in addition to any access
// event gas. The fill parameter indicates whether the slot was previously
// empty (None), triggering the extra CHUNK_FILL_COST.
func (t *WitnessGasTracker) TouchWriteEvent(addr types.Address, subKey uint64, leafKey uint8, fill bool) uint64 {
	var gas uint64

	sk := witnessSubtreeKey{addr: addr, subKey: subKey}
	if !t.editedSubtrees[sk] {
		t.editedSubtrees[sk] = true
		gas = safeAdd(gas, SubtreeEditCost)
	}

	lk := witnessLeafKey{addr: addr, subKey: subKey, leafKey: leafKey}
	if !t.editedLeaves[lk] {
		t.editedLeaves[lk] = true
		gas = safeAdd(gas, ChunkEditCost)
		if fill {
			gas = safeAdd(gas, ChunkFillCost)
		}
	}

	return gas
}

// Verkle tree layout constants from EIP-4762.
const (
	// BasicDataLeafKey is leaf key 0 for the account header (balance, nonce, code size, code hash prefix).
	BasicDataLeafKey uint8 = 0
	// CodeHashLeafKey is leaf key 1 for the full code hash.
	CodeHashLeafKey uint8 = 1

	// HeaderStorageOffset is the Verkle tree offset for "header" storage slots (0..63).
	HeaderStorageOffset uint64 = 64
	// CodeOffset is the Verkle tree offset where code chunks begin.
	CodeOffset uint64 = 128
	// MainStorageOffset is the Verkle tree offset for main storage.
	MainStorageOffset uint64 = 256 * 64 // 16384
	// VerkleNodeWidth is the number of leaves per subtree node.
	VerkleNodeWidth uint64 = 256
)

// GetStorageSlotTreeKeys computes the (treeKey, subKey) for a given storage
// slot index per EIP-4762.
func GetStorageSlotTreeKeys(storageKey uint64) (uint64, uint8) {
	var pos uint64
	if storageKey < (CodeOffset - HeaderStorageOffset) {
		pos = HeaderStorageOffset + storageKey
	} else {
		pos = MainStorageOffset + storageKey
	}
	return pos / VerkleNodeWidth, uint8(pos % VerkleNodeWidth)
}

// GetCodeChunkTreeKeys computes the (treeKey, subKey) for a given code chunk
// index per EIP-4762.
func GetCodeChunkTreeKeys(chunkID uint64) (uint64, uint8) {
	pos := CodeOffset + chunkID
	return pos / VerkleNodeWidth, uint8(pos % VerkleNodeWidth)
}

// --- EIP-4762 dynamic gas functions ---

// gasAccountAccessEIP4762 charges witness gas for accessing an account's
// basic data (balance, nonce, etc). Used by BALANCE, EXTCODESIZE, etc.
func gasAccountAccessEIP4762(evm *EVM, addr types.Address) uint64 {
	if evm.witnessGas == nil {
		return 0
	}
	return evm.witnessGas.TouchAccessEvent(addr, 0, BasicDataLeafKey)
}

// gasAccountWriteEIP4762 charges witness gas for writing an account's basic
// data. Used for value-bearing CALLs.
func gasAccountWriteEIP4762(evm *EVM, addr types.Address, fill bool) uint64 {
	if evm.witnessGas == nil {
		return 0
	}
	gas := evm.witnessGas.TouchAccessEvent(addr, 0, BasicDataLeafKey)
	gas = safeAdd(gas, evm.witnessGas.TouchWriteEvent(addr, 0, BasicDataLeafKey, fill))
	return gas
}

// gasCodeHashAccessEIP4762 charges witness gas for accessing an account's
// code hash. Used by EXTCODEHASH.
func gasCodeHashAccessEIP4762(evm *EVM, addr types.Address) uint64 {
	if evm.witnessGas == nil {
		return 0
	}
	return evm.witnessGas.TouchAccessEvent(addr, 0, CodeHashLeafKey)
}

// gasSloadEIP4762 calculates witness-based gas for SLOAD under EIP-4762.
func gasSloadEIP4762(evm *EVM, contract *Contract, stack *Stack, mem *Memory, memorySize uint64) (uint64, error) {
	if evm.witnessGas == nil {
		return 0, nil
	}
	loc := stack.Back(0)
	storageKey := loc.Uint64()
	treeKey, subKey := GetStorageSlotTreeKeys(storageKey)
	return evm.witnessGas.TouchAccessEvent(contract.Address, treeKey, subKey), nil
}

// gasSstoreEIP4762 calculates witness-based gas for SSTORE under EIP-4762.
// Per the EIP, we remove the EIP-2200 SSTORE gas schedule and charge only
// SLOAD_GAS (WarmStorageReadCost) plus witness access + write costs.
func gasSstoreEIP4762(evm *EVM, contract *Contract, stack *Stack, mem *Memory, memorySize uint64) (uint64, error) {
	gas := WarmStorageReadCost // base SLOAD_GAS

	if evm.witnessGas == nil {
		return gas, nil
	}

	loc := stack.Back(0)
	storageKey := loc.Uint64()
	treeKey, subKey := GetStorageSlotTreeKeys(storageKey)

	// Access event.
	gas = safeAdd(gas, evm.witnessGas.TouchAccessEvent(contract.Address, treeKey, subKey))

	// Determine if this is a fill (writing to a previously-empty slot).
	key := bigToHash(loc)
	fill := false
	if evm.StateDB != nil {
		committed := evm.StateDB.GetCommittedState(contract.Address, key)
		fill = isZeroHash(committed)
	}

	// Write event.
	gas = safeAdd(gas, evm.witnessGas.TouchWriteEvent(contract.Address, treeKey, subKey, fill))
	return gas, nil
}

// gasBalanceEIP4762 charges witness gas for BALANCE under EIP-4762.
func gasBalanceEIP4762(evm *EVM, contract *Contract, stack *Stack, mem *Memory, memorySize uint64) (uint64, error) {
	addr := types.BytesToAddress(stack.Back(0).Bytes())
	return gasAccountAccessEIP4762(evm, addr), nil
}

// gasExtCodeSizeEIP4762 charges witness gas for EXTCODESIZE under EIP-4762.
func gasExtCodeSizeEIP4762(evm *EVM, contract *Contract, stack *Stack, mem *Memory, memorySize uint64) (uint64, error) {
	addr := types.BytesToAddress(stack.Back(0).Bytes())
	return gasAccountAccessEIP4762(evm, addr), nil
}

// gasExtCodeHashEIP4762 charges witness gas for EXTCODEHASH under EIP-4762.
func gasExtCodeHashEIP4762(evm *EVM, contract *Contract, stack *Stack, mem *Memory, memorySize uint64) (uint64, error) {
	addr := types.BytesToAddress(stack.Back(0).Bytes())
	return gasCodeHashAccessEIP4762(evm, addr), nil
}

// gasExtCodeCopyEIP4762 charges witness gas for EXTCODECOPY under EIP-4762.
// Charges per code chunk accessed plus copy + memory expansion.
func gasExtCodeCopyEIP4762(evm *EVM, contract *Contract, stack *Stack, mem *Memory, memorySize uint64) (uint64, error) {
	addr := types.BytesToAddress(stack.Back(0).Bytes())

	// Account header access.
	gas := gasAccountAccessEIP4762(evm, addr)

	// Copy gas: 3 per word. Size is at stack position 3.
	size := stack.Back(3).Uint64()
	gas = safeAdd(gas, safeMul(GasCopy, toWordSize(size)))
	memGas, err := gasMemExpansion(evm, contract, stack, mem, memorySize)
	if err != nil {
		return 0, err
	}
	gas = safeAdd(gas, memGas)

	// Charge per code chunk accessed.
	if evm.witnessGas != nil && size > 0 {
		codeOffset := stack.Back(2).Uint64()
		var codeSize uint64
		if evm.StateDB != nil {
			codeSize = uint64(evm.StateDB.GetCodeSize(addr))
		}
		if codeSize > 0 {
			gas = safeAdd(gas, gasCodeChunksAccess(evm, addr, codeOffset, size, codeSize))
		}
	}

	return gas, nil
}

// gasCallEIP4762 charges witness gas for CALL under EIP-4762.
// Stack: gas, addr, value, argsOffset, argsLength, retOffset, retLength
func gasCallEIP4762(evm *EVM, contract *Contract, stack *Stack, mem *Memory, memorySize uint64) (uint64, error) {
	addr := types.BytesToAddress(stack.Back(1).Bytes())
	gas := gasAccountAccessEIP4762(evm, addr)

	transfersValue := stack.Back(2).Sign() != 0
	if transfersValue {
		// Under EIP-4762, the increased CALL gas for value transfer is removed.
		// Instead, charge witness write costs for both caller and callee basic data.
		callerFill := false
		calleeFill := false
		if evm.StateDB != nil {
			calleeFill = !evm.StateDB.Exist(addr)
		}
		gas = safeAdd(gas, gasAccountWriteEIP4762(evm, contract.CallerAddress, callerFill))
		gas = safeAdd(gas, gasAccountWriteEIP4762(evm, addr, calleeFill))
		if calleeFill {
			// Write to codehash leaf for new account.
			if evm.witnessGas != nil {
				gas = safeAdd(gas, evm.witnessGas.TouchAccessEvent(addr, 0, CodeHashLeafKey))
				gas = safeAdd(gas, evm.witnessGas.TouchWriteEvent(addr, 0, CodeHashLeafKey, true))
			}
		}
	}

	memGas, err := gasMemExpansion(evm, contract, stack, mem, memorySize)
	if err != nil {
		return 0, err
	}
	gas = safeAdd(gas, memGas)
	// Compute call gas via 63/64 rule and store in evm.callGasTemp.
	evm.callGasTemp = callGasEIP150(contract.Gas, gas, stack.Back(0).Uint64())
	gas = safeAdd(gas, evm.callGasTemp)
	return gas, nil
}

// gasCallCodeEIP4762 charges witness gas for CALLCODE under EIP-4762.
func gasCallCodeEIP4762(evm *EVM, contract *Contract, stack *Stack, mem *Memory, memorySize uint64) (uint64, error) {
	addr := types.BytesToAddress(stack.Back(1).Bytes())
	gas := gasAccountAccessEIP4762(evm, addr)

	// Under EIP-4762, CALLCODE value-sending costs are removed.
	// Witness access is still charged.
	memGas, err := gasMemExpansion(evm, contract, stack, mem, memorySize)
	if err != nil {
		return 0, err
	}
	gas = safeAdd(gas, memGas)
	// Compute call gas via 63/64 rule and store in evm.callGasTemp.
	evm.callGasTemp = callGasEIP150(contract.Gas, gas, stack.Back(0).Uint64())
	gas = safeAdd(gas, evm.callGasTemp)
	return gas, nil
}

// gasDelegateCallEIP4762 charges witness gas for DELEGATECALL under EIP-4762.
func gasDelegateCallEIP4762(evm *EVM, contract *Contract, stack *Stack, mem *Memory, memorySize uint64) (uint64, error) {
	addr := types.BytesToAddress(stack.Back(1).Bytes())
	gas := gasAccountAccessEIP4762(evm, addr)
	memGas, err := gasMemExpansion(evm, contract, stack, mem, memorySize)
	if err != nil {
		return 0, err
	}
	gas = safeAdd(gas, memGas)
	// Compute call gas via 63/64 rule and store in evm.callGasTemp.
	evm.callGasTemp = callGasEIP150(contract.Gas, gas, stack.Back(0).Uint64())
	gas = safeAdd(gas, evm.callGasTemp)
	return gas, nil
}

// gasStaticCallEIP4762 charges witness gas for STATICCALL under EIP-4762.
func gasStaticCallEIP4762(evm *EVM, contract *Contract, stack *Stack, mem *Memory, memorySize uint64) (uint64, error) {
	addr := types.BytesToAddress(stack.Back(1).Bytes())
	gas := gasAccountAccessEIP4762(evm, addr)
	memGas, err := gasMemExpansion(evm, contract, stack, mem, memorySize)
	if err != nil {
		return 0, err
	}
	gas = safeAdd(gas, memGas)
	// Compute call gas via 63/64 rule and store in evm.callGasTemp.
	evm.callGasTemp = callGasEIP150(contract.Gas, gas, stack.Back(0).Uint64())
	gas = safeAdd(gas, evm.callGasTemp)
	return gas, nil
}

// gasSelfdestructEIP4762 charges witness gas for SELFDESTRUCT under EIP-4762.
func gasSelfdestructEIP4762(evm *EVM, contract *Contract, stack *Stack, mem *Memory, memorySize uint64) (uint64, error) {
	addr := types.BytesToAddress(stack.Back(0).Bytes())
	gas := gasAccountAccessEIP4762(evm, addr)

	if evm.StateDB != nil && evm.StateDB.GetBalance(contract.Address).Sign() != 0 {
		// Value-bearing selfdestruct: write events for both sender and beneficiary.
		calleeFill := false
		if evm.StateDB != nil {
			calleeFill = !evm.StateDB.Exist(addr)
		}
		gas = safeAdd(gas, gasAccountWriteEIP4762(evm, contract.Address, false))
		gas = safeAdd(gas, gasAccountWriteEIP4762(evm, addr, calleeFill))
	}

	return gas, nil
}

// gasCodeChunksAccess charges witness gas for accessing code chunks in range
// [codeOffset, codeOffset+readSize) clamped to codeSize.
func gasCodeChunksAccess(evm *EVM, addr types.Address, codeOffset, readSize, codeSize uint64) uint64 {
	if evm.witnessGas == nil || readSize == 0 || codeSize == 0 {
		return 0
	}

	// Clamp the range to codeSize.
	endByte := codeOffset + readSize
	if endByte > codeSize {
		endByte = codeSize
	}
	if codeOffset >= codeSize {
		return 0
	}

	firstChunk := codeOffset / CodeChunkSize
	lastChunk := (endByte - 1) / CodeChunkSize

	var gas uint64
	for chunk := firstChunk; chunk <= lastChunk; chunk++ {
		treeKey, subKey := GetCodeChunkTreeKeys(chunk)
		gas = safeAdd(gas, evm.witnessGas.TouchAccessEvent(addr, treeKey, subKey))
	}
	return gas
}

// isZeroHash returns true if all bytes in h are zero.
func isZeroHash(h types.Hash) bool {
	for _, b := range h {
		if b != 0 {
			return false
		}
	}
	return true
}
