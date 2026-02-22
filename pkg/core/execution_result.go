package core

import "github.com/eth2030/eth2030/core/types"

// ExecutionResult holds the outcome of a transaction execution.
type ExecutionResult struct {
	UsedGas         uint64
	BlockGasUsed    uint64 // EIP-7778: pre-refund gas used for block accounting
	Err             error
	ReturnData      []byte
	ContractAddress types.Address // set for contract creation
}

// Unwrap returns the execution error, if any.
func (r *ExecutionResult) Unwrap() error {
	return r.Err
}

// Failed returns whether the execution resulted in an error.
func (r *ExecutionResult) Failed() bool {
	return r.Err != nil
}

// Return returns the return data from a successful execution.
func (r *ExecutionResult) Return() []byte {
	if r.Failed() {
		return nil
	}
	return r.ReturnData
}

// Revert returns the return data from a reverted execution (revert reason).
func (r *ExecutionResult) Revert() []byte {
	if r.Failed() {
		return r.ReturnData
	}
	return nil
}
