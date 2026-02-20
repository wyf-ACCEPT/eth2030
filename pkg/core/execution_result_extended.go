package core

import (
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"

	"github.com/eth2028/eth2028/core/types"
)

// Extended execution result with trace output, revert reason parsing,
// gas breakdown (execution/calldata/blob), and access list generation.

// Revert reason decoding: Solidity's revert strings use the Error(string)
// ABI selector 0x08c379a0, followed by ABI-encoded string.
var revertSelector = []byte{0x08, 0xc3, 0x79, 0xa0}

// Panic reason decoding: Solidity's Panic(uint256) ABI selector 0x4e487b71.
var panicSelector = []byte{0x4e, 0x48, 0x7b, 0x71}

// Known Solidity panic codes.
var panicReasons = map[uint64]string{
	0x00: "generic compiler panic",
	0x01: "assertion failure",
	0x11: "arithmetic overflow/underflow",
	0x12: "division by zero",
	0x21: "invalid enum conversion",
	0x22: "storage encoding error",
	0x31: "pop on empty array",
	0x32: "array out of bounds",
	0x41: "out of memory",
	0x51: "uninitialized function pointer",
}

// GasBreakdown provides detailed gas accounting across all dimensions.
type GasBreakdown struct {
	// Execution gas (EVM compute).
	ExecutionGas     uint64
	ExecutionRefund  uint64
	ExecutionEffective uint64 // after refund

	// Calldata gas (EIP-7706 separate dimension).
	CalldataGas uint64
	CalldataFee *big.Int // calldata_gas * calldata_base_fee

	// Blob gas (EIP-4844).
	BlobGas uint64
	BlobFee *big.Int // blob_gas * blob_base_fee

	// Intrinsic gas (base tx cost before EVM execution).
	IntrinsicGas uint64

	// Total cost in wei across all dimensions.
	TotalCost *big.Int
}

// TotalGas returns the sum of all gas dimensions.
func (b *GasBreakdown) TotalGas() uint64 {
	return b.ExecutionEffective + b.CalldataGas + b.BlobGas
}

// AccessListEntry represents a single address and its accessed storage slots.
type AccessListEntry struct {
	Address     types.Address
	StorageKeys []types.Hash
}

// TraceOutput contains structured trace information from execution.
type TraceOutput struct {
	// Gas used at each call depth.
	GasUsedByDepth []uint64
	// Addresses accessed during execution.
	AccessedAddresses []types.Address
	// Storage slots accessed during execution.
	AccessedSlots []AccessListEntry
	// Number of SSTORE operations.
	StorageWrites uint64
	// Number of LOG operations.
	LogCount uint64
	// Maximum call depth reached.
	MaxCallDepth uint64
	// Whether execution touched a CREATE/CREATE2.
	CreatedContracts []types.Address
}

// ExtendedExecutionResult extends ExecutionResult with trace output,
// gas breakdown, and access list generation.
type ExtendedExecutionResult struct {
	// Core result fields (mirrors ExecutionResult).
	UsedGas         uint64
	BlockGasUsed    uint64 // EIP-7778 pre-refund
	Err             error
	ReturnData      []byte
	ContractAddress types.Address

	// Extended fields.
	GasBreak  GasBreakdown
	Trace     *TraceOutput
	Logs      []*types.Log
	TxHash    types.Hash
}

// Failed returns whether the execution resulted in an error.
func (r *ExtendedExecutionResult) Failed() bool {
	return r.Err != nil
}

// Unwrap returns the execution error, if any.
func (r *ExtendedExecutionResult) Unwrap() error {
	return r.Err
}

// Return returns the return data from a successful execution.
func (r *ExtendedExecutionResult) Return() []byte {
	if r.Failed() {
		return nil
	}
	return r.ReturnData
}

// Revert returns the raw return data from a reverted execution.
func (r *ExtendedExecutionResult) Revert() []byte {
	if r.Failed() {
		return r.ReturnData
	}
	return nil
}

// RevertReason attempts to decode the revert reason from the return data.
// Supports Solidity's Error(string) and Panic(uint256) formats.
// Returns empty string if no recognizable revert reason is found.
func (r *ExtendedExecutionResult) RevertReason() string {
	if !r.Failed() || len(r.ReturnData) < 4 {
		return ""
	}
	return DecodeRevertReason(r.ReturnData)
}

// DecodeRevertReason decodes revert reason from raw return data.
// Supports Solidity's Error(string) selector 0x08c379a0 and
// Panic(uint256) selector 0x4e487b71.
func DecodeRevertReason(data []byte) string {
	if len(data) < 4 {
		return ""
	}

	// Check for Error(string) selector.
	if len(data) >= 68 && matchSelector(data[:4], revertSelector) {
		return decodeABIString(data[4:])
	}

	// Check for Panic(uint256) selector.
	if len(data) >= 36 && matchSelector(data[:4], panicSelector) {
		return decodePanicReason(data[4:36])
	}

	// Unknown format: return hex of the first 4 bytes.
	return fmt.Sprintf("unknown revert (selector: 0x%s)", hex.EncodeToString(data[:4]))
}

// matchSelector checks if two 4-byte selectors match.
func matchSelector(a, b []byte) bool {
	if len(a) < 4 || len(b) < 4 {
		return false
	}
	return a[0] == b[0] && a[1] == b[1] && a[2] == b[2] && a[3] == b[3]
}

// decodeABIString decodes an ABI-encoded string from the data following
// the selector. The layout is: offset (32 bytes) | length (32 bytes) | data.
func decodeABIString(data []byte) string {
	if len(data) < 64 {
		return ""
	}

	// Read the offset (first 32 bytes, big-endian).
	offset := new(big.Int).SetBytes(data[:32])
	if !offset.IsUint64() || offset.Uint64() > uint64(len(data)) {
		return ""
	}
	off := offset.Uint64()

	// Read the length (next 32 bytes after offset).
	if off+32 > uint64(len(data)) {
		return ""
	}
	length := new(big.Int).SetBytes(data[off : off+32])
	if !length.IsUint64() {
		return ""
	}
	strLen := length.Uint64()

	// Read the string data.
	strStart := off + 32
	if strStart+strLen > uint64(len(data)) {
		strLen = uint64(len(data)) - strStart
	}
	if strLen == 0 {
		return ""
	}
	return string(data[strStart : strStart+strLen])
}

// decodePanicReason decodes a Solidity Panic(uint256) code.
func decodePanicReason(data []byte) string {
	if len(data) < 32 {
		return "panic: unknown"
	}
	code := new(big.Int).SetBytes(data)
	if code.IsUint64() {
		if reason, ok := panicReasons[code.Uint64()]; ok {
			return fmt.Sprintf("panic: %s (0x%02x)", reason, code.Uint64())
		}
	}
	return fmt.Sprintf("panic: code 0x%x", code)
}

// GenerateAccessList generates an access list from the trace output,
// suitable for inclusion in EIP-2930/EIP-1559 transactions.
func (r *ExtendedExecutionResult) GenerateAccessList() types.AccessList {
	if r.Trace == nil || len(r.Trace.AccessedSlots) == 0 {
		return nil
	}

	var accessList types.AccessList
	for _, entry := range r.Trace.AccessedSlots {
		keys := make([]types.Hash, len(entry.StorageKeys))
		copy(keys, entry.StorageKeys)
		accessList = append(accessList, types.AccessTuple{
			Address:     entry.Address,
			StorageKeys: keys,
		})
	}
	return accessList
}

// EffectiveGasUsed returns the effective gas used after applying the refund.
func (r *ExtendedExecutionResult) EffectiveGasUsed() uint64 {
	return r.GasBreak.ExecutionEffective
}

// ToBasicResult converts an ExtendedExecutionResult to a basic ExecutionResult.
func (r *ExtendedExecutionResult) ToBasicResult() *ExecutionResult {
	return &ExecutionResult{
		UsedGas:         r.UsedGas,
		BlockGasUsed:    r.BlockGasUsed,
		Err:             r.Err,
		ReturnData:      r.ReturnData,
		ContractAddress: r.ContractAddress,
	}
}

// NewGasBreakdown creates a GasBreakdown from the individual gas measurements.
func NewGasBreakdown(
	executionGas, executionRefund, calldataGas, blobGas, intrinsicGas uint64,
	baseFee, calldataBaseFee, blobBaseFee *big.Int,
) *GasBreakdown {
	effective := executionGas
	if executionRefund > 0 && executionRefund < executionGas {
		effective = executionGas - executionRefund
	}

	b := &GasBreakdown{
		ExecutionGas:       executionGas,
		ExecutionRefund:    executionRefund,
		ExecutionEffective: effective,
		CalldataGas:        calldataGas,
		BlobGas:            blobGas,
		IntrinsicGas:       intrinsicGas,
	}

	// Compute fees.
	totalCost := new(big.Int)

	if baseFee != nil && baseFee.Sign() > 0 {
		execCost := new(big.Int).Mul(baseFee, new(big.Int).SetUint64(effective))
		totalCost.Add(totalCost, execCost)
	}

	if calldataBaseFee != nil && calldataBaseFee.Sign() > 0 && calldataGas > 0 {
		b.CalldataFee = new(big.Int).Mul(calldataBaseFee, new(big.Int).SetUint64(calldataGas))
		totalCost.Add(totalCost, b.CalldataFee)
	} else {
		b.CalldataFee = new(big.Int)
	}

	if blobBaseFee != nil && blobBaseFee.Sign() > 0 && blobGas > 0 {
		b.BlobFee = new(big.Int).Mul(blobBaseFee, new(big.Int).SetUint64(blobGas))
		totalCost.Add(totalCost, b.BlobFee)
	} else {
		b.BlobFee = new(big.Int)
	}

	b.TotalCost = totalCost
	return b
}

// IsRevert returns true if the error represents an EVM revert.
func IsRevert(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, ErrExecutionReverted) || err.Error() == "execution reverted"
}

// ErrExecutionReverted is returned when EVM execution encounters REVERT.
var ErrExecutionReverted = errors.New("execution reverted")

// ParseRevertData attempts to extract structured information from revert data.
// Returns the decoded reason and whether parsing was successful.
func ParseRevertData(data []byte) (reason string, ok bool) {
	if len(data) < 4 {
		return "", false
	}
	reason = DecodeRevertReason(data)
	if reason == "" {
		return "", false
	}
	return reason, true
}
