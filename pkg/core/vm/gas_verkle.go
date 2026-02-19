package vm

import (
	"github.com/eth2028/eth2028/core/state"
	"github.com/eth2028/eth2028/core/types"
)

// AccessEventsGasCalculator provides EIP-4762 gas calculation functions
// that use the AccessEvents-based witness tracking from the state package.
// This is the binary trie equivalent of the WitnessGasTracker approach,
// designed for use with the full binary trie state structure.
type AccessEventsGasCalculator struct {
	Events *state.AccessEvents
}

// NewAccessEventsGasCalculator creates a calculator with fresh access events.
func NewAccessEventsGasCalculator() *AccessEventsGasCalculator {
	return &AccessEventsGasCalculator{
		Events: state.NewAccessEvents(),
	}
}

// SStoreGas returns the witness gas cost for an SSTORE operation.
func (c *AccessEventsGasCalculator) SStoreGas(addr types.Address, slot types.Hash, availableGas uint64) uint64 {
	return c.Events.SlotGas(addr, slot, true, availableGas, true)
}

// SLoadGas returns the witness gas cost for an SLOAD operation.
func (c *AccessEventsGasCalculator) SLoadGas(addr types.Address, slot types.Hash, availableGas uint64) uint64 {
	return c.Events.SlotGas(addr, slot, false, availableGas, true)
}

// BalanceGas returns the witness gas cost for a BALANCE operation.
func (c *AccessEventsGasCalculator) BalanceGas(addr types.Address, availableGas uint64) uint64 {
	return c.Events.BasicDataGas(addr, false, availableGas, true)
}

// ExtCodeSizeGas returns the witness gas cost for EXTCODESIZE.
func (c *AccessEventsGasCalculator) ExtCodeSizeGas(addr types.Address, availableGas uint64) uint64 {
	return c.Events.BasicDataGas(addr, false, availableGas, true)
}

// ExtCodeHashGas returns the witness gas cost for EXTCODEHASH.
func (c *AccessEventsGasCalculator) ExtCodeHashGas(addr types.Address, availableGas uint64) uint64 {
	return c.Events.CodeHashGas(addr, false, availableGas, true)
}

// CallGas returns the witness gas for a CALL including value transfer costs.
func (c *AccessEventsGasCalculator) CallGas(caller, target types.Address, transfersValue bool, availableGas uint64) uint64 {
	if transfersValue {
		return c.Events.ValueTransferGas(caller, target, availableGas)
	}
	return c.Events.MessageCallGas(target, availableGas)
}

// SelfDestructGas returns the witness gas for SELFDESTRUCT.
func (c *AccessEventsGasCalculator) SelfDestructGas(contractAddr, beneficiaryAddr types.Address, availableGas uint64) uint64 {
	return c.Events.BasicDataGas(contractAddr, false, availableGas, false)
}

// AddTxOrigin warms the sender's account fields.
func (c *AccessEventsGasCalculator) AddTxOrigin(addr types.Address) {
	c.Events.AddTxOrigin(addr)
}

// AddTxDestination warms the destination's account fields.
func (c *AccessEventsGasCalculator) AddTxDestination(addr types.Address, sendsValue, doesntExist bool) {
	c.Events.AddTxDestination(addr, sendsValue, doesntExist)
}

// Merge combines another calculator's events into this one.
func (c *AccessEventsGasCalculator) Merge(other *AccessEventsGasCalculator) {
	c.Events.Merge(other.Events)
}

// Copy returns a deep copy of this calculator.
func (c *AccessEventsGasCalculator) Copy() *AccessEventsGasCalculator {
	return &AccessEventsGasCalculator{
		Events: c.Events.Copy(),
	}
}
