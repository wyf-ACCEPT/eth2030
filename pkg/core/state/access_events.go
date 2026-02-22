package state

import (
	"maps"
	"math"
	"math/big"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/trie/bintrie"
)

// mode specifies how a tree location has been accessed.
// bit 0: read, bit 1: write
type mode byte

const (
	AccessWitnessReadFlag  = mode(1)
	AccessWitnessWriteFlag = mode(2)
)

// Witness gas costs per EIP-4762.
const (
	WitnessBranchReadCost  uint64 = 1900
	WitnessChunkReadCost   uint64 = 200
	WitnessBranchWriteCost uint64 = 3000
	WitnessChunkWriteCost  uint64 = 500
	WitnessChunkFillCost   uint64 = 6200
	WarmStorageReadCost    uint64 = 100
)

var zeroTreeIndex [32]byte

// bigToTreeIndex converts a big.Int tree index to a fixed-size 32-byte array
// for use as a map key.
func bigToTreeIndex(bi *big.Int) [32]byte {
	var idx [32]byte
	if bi != nil && bi.Sign() != 0 {
		b := bi.Bytes()
		copy(idx[32-len(b):], b)
	}
	return idx
}

// AccessEvents tracks which tree locations have been accessed during
// block production, enabling witness-based gas accounting.
type AccessEvents struct {
	branches map[branchAccessKey]mode
	chunks   map[chunkAccessKey]mode
}

// NewAccessEvents creates a new empty access event tracker.
func NewAccessEvents() *AccessEvents {
	return &AccessEvents{
		branches: make(map[branchAccessKey]mode),
		chunks:   make(map[chunkAccessKey]mode),
	}
}

// Merge combines access events from another tracker (e.g., after a tx).
func (ae *AccessEvents) Merge(other *AccessEvents) {
	for k := range other.branches {
		ae.branches[k] |= other.branches[k]
	}
	for k, chunk := range other.chunks {
		ae.chunks[k] |= chunk
	}
}

// Keys returns the list of tree keys that were touched.
func (ae *AccessEvents) Keys() [][]byte {
	keys := make([][]byte, 0, len(ae.chunks))
	for chunk := range ae.chunks {
		var offset [32]byte
		copy(offset[:31], chunk.treeIndex[1:])
		offset[31] = chunk.leafKey
		key := bintrie.GetBinaryTreeKey(chunk.addr, offset[:])
		keys = append(keys, key)
	}
	return keys
}

// Copy returns a deep copy of the access events.
func (ae *AccessEvents) Copy() *AccessEvents {
	return &AccessEvents{
		branches: maps.Clone(ae.branches),
		chunks:   maps.Clone(ae.chunks),
	}
}

// AddAccount returns the gas to be charged for accessing an account's
// basic data and code hash fields.
func (ae *AccessEvents) AddAccount(addr types.Address, isWrite bool, availableGas uint64) uint64 {
	var gas uint64
	consumed, expected := ae.touchAddressAndChargeGas(addr, zeroTreeIndex, bintrie.BasicDataLeafKey, isWrite, availableGas)
	if consumed < expected {
		return expected
	}
	gas += consumed
	consumed, expected = ae.touchAddressAndChargeGas(addr, zeroTreeIndex, bintrie.CodeHashLeafKey, isWrite, availableGas-consumed)
	if consumed < expected {
		return expected + gas
	}
	gas += expected
	return gas
}

// MessageCallGas returns the gas for a cold message call destination.
func (ae *AccessEvents) MessageCallGas(destination types.Address, availableGas uint64) uint64 {
	_, expected := ae.touchAddressAndChargeGas(destination, zeroTreeIndex, bintrie.BasicDataLeafKey, false, availableGas)
	if expected == 0 {
		expected = WarmStorageReadCost
	}
	return expected
}

// ValueTransferGas returns the gas for a value transfer (both caller and callee).
func (ae *AccessEvents) ValueTransferGas(callerAddr, targetAddr types.Address, availableGas uint64) uint64 {
	_, expected1 := ae.touchAddressAndChargeGas(callerAddr, zeroTreeIndex, bintrie.BasicDataLeafKey, true, availableGas)
	if expected1 > availableGas {
		return expected1
	}
	_, expected2 := ae.touchAddressAndChargeGas(targetAddr, zeroTreeIndex, bintrie.BasicDataLeafKey, true, availableGas-expected1)
	if expected1+expected2 == 0 {
		return WarmStorageReadCost
	}
	return expected1 + expected2
}

// ContractCreatePreCheckGas charges read costs before contract creation.
func (ae *AccessEvents) ContractCreatePreCheckGas(addr types.Address, availableGas uint64) uint64 {
	consumed, expected1 := ae.touchAddressAndChargeGas(addr, zeroTreeIndex, bintrie.BasicDataLeafKey, false, availableGas)
	_, expected2 := ae.touchAddressAndChargeGas(addr, zeroTreeIndex, bintrie.CodeHashLeafKey, false, availableGas-consumed)
	return expected1 + expected2
}

// ContractCreateInitGas returns the gas costs for contract creation initialization.
func (ae *AccessEvents) ContractCreateInitGas(addr types.Address, availableGas uint64) (uint64, uint64) {
	var gas uint64
	consumed, expected1 := ae.touchAddressAndChargeGas(addr, zeroTreeIndex, bintrie.BasicDataLeafKey, true, availableGas)
	gas += consumed
	consumed, expected2 := ae.touchAddressAndChargeGas(addr, zeroTreeIndex, bintrie.CodeHashLeafKey, true, availableGas-consumed)
	gas += consumed
	return gas, expected1 + expected2
}

// AddTxOrigin warms the sender account fields (covered by the 21000 intrinsic gas).
func (ae *AccessEvents) AddTxOrigin(originAddr types.Address) {
	ae.touchAddressAndChargeGas(originAddr, zeroTreeIndex, bintrie.BasicDataLeafKey, true, math.MaxUint64)
	ae.touchAddressAndChargeGas(originAddr, zeroTreeIndex, bintrie.CodeHashLeafKey, false, math.MaxUint64)
}

// AddTxDestination warms the destination account fields.
func (ae *AccessEvents) AddTxDestination(addr types.Address, sendsValue, doesntExist bool) {
	ae.touchAddressAndChargeGas(addr, zeroTreeIndex, bintrie.BasicDataLeafKey, sendsValue, math.MaxUint64)
	ae.touchAddressAndChargeGas(addr, zeroTreeIndex, bintrie.CodeHashLeafKey, doesntExist, math.MaxUint64)
}

// SlotGas returns the gas cost for a cold storage slot access.
func (ae *AccessEvents) SlotGas(addr types.Address, slot types.Hash, isWrite bool, availableGas uint64, chargeWarmCosts bool) uint64 {
	treeIndex, subIndex := bintrie.StorageIndex(slot[:])
	ti := bigToTreeIndex(treeIndex)
	_, expected := ae.touchAddressAndChargeGas(addr, ti, subIndex, isWrite, availableGas)
	if expected == 0 && chargeWarmCosts {
		expected = WarmStorageReadCost
	}
	return expected
}

// BasicDataGas adds the account's basic data to the accessed data.
func (ae *AccessEvents) BasicDataGas(addr types.Address, isWrite bool, availableGas uint64, chargeWarmCosts bool) uint64 {
	_, expected := ae.touchAddressAndChargeGas(addr, zeroTreeIndex, bintrie.BasicDataLeafKey, isWrite, availableGas)
	if expected == 0 && chargeWarmCosts {
		if availableGas < WarmStorageReadCost {
			return availableGas
		}
		expected = WarmStorageReadCost
	}
	return expected
}

// CodeHashGas adds the account's code hash to the accessed data.
func (ae *AccessEvents) CodeHashGas(addr types.Address, isWrite bool, availableGas uint64, chargeWarmCosts bool) uint64 {
	_, expected := ae.touchAddressAndChargeGas(addr, zeroTreeIndex, bintrie.CodeHashLeafKey, isWrite, availableGas)
	if expected == 0 && chargeWarmCosts {
		if availableGas < WarmStorageReadCost {
			return availableGas
		}
		expected = WarmStorageReadCost
	}
	return expected
}

// CodeChunksRangeGas charges witness gas for accessing a range of code chunks.
func (ae *AccessEvents) CodeChunksRangeGas(contractAddr types.Address, startPC, size uint64, codeLen uint64, isWrite bool, availableGas uint64) (uint64, uint64) {
	if (codeLen == 0 && size == 0) || startPC > codeLen {
		return 0, 0
	}

	endPC := startPC + size
	if endPC > codeLen {
		endPC = codeLen
	}
	if endPC > 0 {
		endPC--
	}

	var statelessGasCharged uint64
	for chunkNumber := startPC / 31; chunkNumber <= endPC/31; chunkNumber++ {
		tiBig := new(big.Int).SetUint64((chunkNumber + 128) / 256)
		ti := bigToTreeIndex(tiBig)
		subIndex := byte((chunkNumber + 128) % 256)
		consumed, expected := ae.touchAddressAndChargeGas(contractAddr, ti, subIndex, isWrite, availableGas)
		if expected > consumed {
			return statelessGasCharged + consumed, statelessGasCharged + expected
		}
		statelessGasCharged += consumed
		availableGas -= consumed
	}
	return statelessGasCharged, statelessGasCharged
}

// touchAddressAndChargeGas adds missing access events and returns consumed/expected gas.
func (ae *AccessEvents) touchAddressAndChargeGas(addr types.Address, treeIndex [32]byte, subIndex byte, isWrite bool, availableGas uint64) (uint64, uint64) {
	branchKey := branchAccessKey{addr: addr, treeIndex: treeIndex}
	chunkKey := chunkAccessKey{branchAccessKey: branchKey, leafKey: subIndex}

	var branchRead, chunkRead bool
	if _, hasStem := ae.branches[branchKey]; !hasStem {
		branchRead = true
	}
	if _, hasSelector := ae.chunks[chunkKey]; !hasSelector {
		chunkRead = true
	}

	var branchWrite, chunkWrite bool
	if isWrite {
		if (ae.branches[branchKey] & AccessWitnessWriteFlag) == 0 {
			branchWrite = true
		}
		chunkValue := ae.chunks[chunkKey]
		if (chunkValue & AccessWitnessWriteFlag) == 0 {
			chunkWrite = true
		}
	}

	var gas uint64
	if branchRead {
		gas += WitnessBranchReadCost
	}
	if chunkRead {
		gas += WitnessChunkReadCost
	}
	if branchWrite {
		gas += WitnessBranchWriteCost
	}
	if chunkWrite {
		gas += WitnessChunkWriteCost
	}

	if availableGas < gas {
		return availableGas, gas
	}

	if branchRead {
		ae.branches[branchKey] = AccessWitnessReadFlag
	}
	if branchWrite {
		ae.branches[branchKey] |= AccessWitnessWriteFlag
	}
	if chunkRead {
		ae.chunks[chunkKey] = AccessWitnessReadFlag
	}
	if chunkWrite {
		ae.chunks[chunkKey] |= AccessWitnessWriteFlag
	}

	return gas, gas
}

// branchAccessKey identifies a branch (subtree) in the binary trie.
// Uses [32]byte instead of big.Int so it can be used as a map key.
type branchAccessKey struct {
	addr      types.Address
	treeIndex [32]byte
}

// chunkAccessKey identifies a specific leaf chunk in the binary trie.
type chunkAccessKey struct {
	branchAccessKey
	leafKey byte
}
