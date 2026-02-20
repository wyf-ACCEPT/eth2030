// state_access_gas.go implements EIP-4762 gas cost calculations for Verkle
// witness-based state access. It provides a StateAccessGasCalculator that
// computes gas costs for individual Verkle tree leaf accesses, code chunk
// accesses, and witness-size-based gas charges.
package vm

import (
	"github.com/eth2028/eth2028/core/types"
)

// Leaf access gas cost constants per EIP-4762.
const (
	// LeafReadGas is the gas cost for reading a single Verkle tree leaf.
	LeafReadGas uint64 = 200

	// LeafWriteGas is the gas cost for writing a single Verkle tree leaf.
	LeafWriteGas uint64 = 500

	// BranchReadGas is the gas cost for accessing a new branch/subtree.
	BranchReadGas uint64 = 1900

	// BranchWriteGas is the gas cost for writing to a new branch/subtree.
	BranchWriteGas uint64 = 3000

	// LeafFillGas is the additional gas for writing to a previously empty leaf.
	LeafFillGas uint64 = 6200

	// WitnessGasPerByte is the gas cost per byte of witness data.
	WitnessGasPerByte uint64 = 12

	// ChunkGasSize is the size of a code chunk in bytes (EIP-4762).
	ChunkGasSize uint64 = 31

	// MaxWitnessGasCharge is the maximum gas that can be charged for witness data.
	MaxWitnessGasCharge uint64 = 30_000_000
)

// StateAccessGasCalculator computes EIP-4762 gas costs for Verkle witness
// state accesses. It tracks which leaves and branches have been accessed to
// determine cold vs warm gas costs.
type StateAccessGasCalculator struct {
	accessedBranches map[accessBranchKey]bool
	accessedLeaves   map[accessLeafKey]bool
	writtenBranches  map[accessBranchKey]bool
	writtenLeaves    map[accessLeafKey]bool
}

// accessBranchKey identifies a branch in the Verkle tree for gas purposes.
type accessBranchKey struct {
	addr      types.Address
	treeIndex uint64
}

// accessLeafKey identifies a single leaf in the Verkle tree for gas purposes.
type accessLeafKey struct {
	addr      types.Address
	treeIndex uint64
	leafIndex uint8
}

// NewStateAccessGasCalculator creates a new calculator with empty state.
func NewStateAccessGasCalculator() *StateAccessGasCalculator {
	return &StateAccessGasCalculator{
		accessedBranches: make(map[accessBranchKey]bool),
		accessedLeaves:   make(map[accessLeafKey]bool),
		writtenBranches:  make(map[accessBranchKey]bool),
		writtenLeaves:    make(map[accessLeafKey]bool),
	}
}

// LeafAccessGas computes the gas cost for accessing a single Verkle tree
// leaf. Returns 0 for already-warm accesses.
func (c *StateAccessGasCalculator) LeafAccessGas(addr types.Address, treeIndex uint64, leafIndex uint8) uint64 {
	var gas uint64

	bk := accessBranchKey{addr: addr, treeIndex: treeIndex}
	if !c.accessedBranches[bk] {
		gas += BranchReadGas
		c.accessedBranches[bk] = true
	}

	lk := accessLeafKey{addr: addr, treeIndex: treeIndex, leafIndex: leafIndex}
	if !c.accessedLeaves[lk] {
		gas += LeafReadGas
		c.accessedLeaves[lk] = true
	}

	return gas
}

// LeafWriteGasCharge computes the gas cost for writing a single Verkle tree
// leaf. Includes fill cost if the leaf was previously empty.
func (c *StateAccessGasCalculator) LeafWriteGasCharge(addr types.Address, treeIndex uint64, leafIndex uint8, isFill bool) uint64 {
	var gas uint64

	bk := accessBranchKey{addr: addr, treeIndex: treeIndex}
	if !c.writtenBranches[bk] {
		gas += BranchWriteGas
		c.writtenBranches[bk] = true
	}

	lk := accessLeafKey{addr: addr, treeIndex: treeIndex, leafIndex: leafIndex}
	if !c.writtenLeaves[lk] {
		gas += LeafWriteGas
		c.writtenLeaves[lk] = true
		if isFill {
			gas += LeafFillGas
		}
	}

	return gas
}

// ChunkAccessGas computes the gas cost for accessing code chunks in the
// range [startPC, startPC+size). Each 31-byte chunk costs leaf access gas.
func (c *StateAccessGasCalculator) ChunkAccessGas(addr types.Address, startPC, size, codeLen uint64) uint64 {
	if size == 0 || codeLen == 0 || startPC >= codeLen {
		return 0
	}

	endPC := startPC + size
	if endPC > codeLen {
		endPC = codeLen
	}
	if endPC == 0 {
		return 0
	}

	firstChunk := startPC / ChunkGasSize
	lastChunk := (endPC - 1) / ChunkGasSize

	var gas uint64
	for chunk := firstChunk; chunk <= lastChunk; chunk++ {
		treeIdx, leafIdx := chunkToTreeKeys(chunk)
		gas += c.LeafAccessGas(addr, treeIdx, leafIdx)
	}
	return gas
}

// chunkToTreeKeys converts a code chunk number to Verkle tree keys.
// Code chunks start at offset 128 in the account stem.
func chunkToTreeKeys(chunkNum uint64) (uint64, uint8) {
	pos := CodeOffset + chunkNum
	return pos / VerkleNodeWidth, uint8(pos % VerkleNodeWidth)
}

// WitnessGasCharger computes gas costs based on witness data size.
type WitnessGasCharger struct {
	totalBytes   uint64
	totalGas     uint64
	chargeCount  uint64
}

// NewWitnessGasCharger creates a new witness gas charger.
func NewWitnessGasCharger() *WitnessGasCharger {
	return &WitnessGasCharger{}
}

// ChargeWitnessGas computes and returns the gas cost for a witness of the
// given size in bytes. The cost is WitnessGasPerByte * bytes, capped at
// MaxWitnessGasCharge.
func (w *WitnessGasCharger) ChargeWitnessGas(witnessBytes uint64) uint64 {
	gas := witnessBytes * WitnessGasPerByte
	if gas > MaxWitnessGasCharge {
		gas = MaxWitnessGasCharge
	}

	w.totalBytes += witnessBytes
	w.totalGas += gas
	w.chargeCount++
	return gas
}

// TotalBytes returns the total witness bytes charged.
func (w *WitnessGasCharger) TotalBytes() uint64 {
	return w.totalBytes
}

// TotalGas returns the total gas charged for witness data.
func (w *WitnessGasCharger) TotalGas() uint64 {
	return w.totalGas
}

// ChargeCount returns the number of witness charges.
func (w *WitnessGasCharger) ChargeCount() uint64 {
	return w.chargeCount
}

// SloadAccessGas computes the gas for an SLOAD at the given storage slot.
// It maps the slot to a Verkle tree leaf and charges access gas.
func (c *StateAccessGasCalculator) SloadAccessGas(addr types.Address, storageKey uint64) uint64 {
	treeKey, subKey := GetStorageSlotTreeKeys(storageKey)
	return c.LeafAccessGas(addr, treeKey, subKey)
}

// SstoreAccessGas computes the gas for an SSTORE at the given storage slot.
// It charges both access and write gas.
func (c *StateAccessGasCalculator) SstoreAccessGas(addr types.Address, storageKey uint64, isFill bool) uint64 {
	treeKey, subKey := GetStorageSlotTreeKeys(storageKey)
	gas := c.LeafAccessGas(addr, treeKey, subKey)
	gas += c.LeafWriteGasCharge(addr, treeKey, subKey, isFill)
	return gas
}

// BalanceAccessGas computes the gas for a BALANCE opcode access.
func (c *StateAccessGasCalculator) BalanceAccessGas(addr types.Address) uint64 {
	return c.LeafAccessGas(addr, 0, BasicDataLeafKey)
}

// CodeHashAccessGas computes the gas for an EXTCODEHASH opcode access.
func (c *StateAccessGasCalculator) CodeHashAccessGas(addr types.Address) uint64 {
	return c.LeafAccessGas(addr, 0, CodeHashLeafKey)
}

// CallAccessGas computes the gas for a CALL-family opcode targeting an address.
func (c *StateAccessGasCalculator) CallAccessGas(target types.Address) uint64 {
	return c.LeafAccessGas(target, 0, BasicDataLeafKey)
}

// IsLeafWarm reports whether a specific leaf has already been accessed.
func (c *StateAccessGasCalculator) IsLeafWarm(addr types.Address, treeIndex uint64, leafIndex uint8) bool {
	lk := accessLeafKey{addr: addr, treeIndex: treeIndex, leafIndex: leafIndex}
	return c.accessedLeaves[lk]
}

// IsBranchWarm reports whether a specific branch has already been accessed.
func (c *StateAccessGasCalculator) IsBranchWarm(addr types.Address, treeIndex uint64) bool {
	bk := accessBranchKey{addr: addr, treeIndex: treeIndex}
	return c.accessedBranches[bk]
}

// AccessedLeafCount returns the number of unique leaves accessed.
func (c *StateAccessGasCalculator) AccessedLeafCount() int {
	return len(c.accessedLeaves)
}

// AccessedBranchCount returns the number of unique branches accessed.
func (c *StateAccessGasCalculator) AccessedBranchCount() int {
	return len(c.accessedBranches)
}

// WrittenLeafCount returns the number of unique leaves written.
func (c *StateAccessGasCalculator) WrittenLeafCount() int {
	return len(c.writtenLeaves)
}
