package types

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/eth2030/eth2030/rlp"
)

// Frame receipt status codes.
const (
	FrameStatusSuccess uint64 = 1
	FrameStatusRevert  uint64 = 0
	FrameStatusHalt    uint64 = 2 // out-of-gas or exceptional halt
)

// Frame receipt gas breakdown fields per EIP-8141.
const (
	// MaxSubFrames is the maximum sub-frame nesting depth.
	MaxSubFrames = 16
)

// Frame receipt errors.
var (
	ErrFrameReceiptNil         = errors.New("frame receipt: nil receipt")
	ErrFrameResultsEmpty       = errors.New("frame receipt: empty frame results")
	ErrTooManySubFrames        = errors.New("frame receipt: too many sub-frames")
	ErrFrameGasExceedsCumul    = errors.New("frame receipt: total gas exceeds cumulative")
	ErrFrameResultInvalidIndex = errors.New("frame result: index out of range")
)

// FrameGasBreakdown holds a detailed gas cost breakdown for a single frame.
type FrameGasBreakdown struct {
	IntrinsicGas uint64 // base cost of the frame call
	ExecutionGas uint64 // gas consumed by EVM execution
	CalldataGas  uint64 // gas attributed to calldata (EIP-7706)
	RefundGas    uint64 // gas refunded (capped at 1/5 of total consumed)
}

// TotalConsumed returns IntrinsicGas + ExecutionGas + CalldataGas - RefundGas.
func (g *FrameGasBreakdown) TotalConsumed() uint64 {
	total := g.IntrinsicGas + g.ExecutionGas + g.CalldataGas
	if g.RefundGas > total {
		return 0
	}
	return total - g.RefundGas
}

// SubFrameResult represents the execution outcome of a nested sub-frame call.
type SubFrameResult struct {
	Target  Address
	Status  uint64
	GasUsed uint64
	ReturnData []byte
}

// RollupFrameFields holds rollup-specific metadata attached to a frame receipt.
// These fields track L1 cost attribution for rollup sequencer billing.
type RollupFrameFields struct {
	L1GasUsed     uint64   // L1 gas attributed to this frame's data
	L1GasPrice    *big.Int // L1 gas price at the time of inclusion
	L1Fee         *big.Int // total L1 fee charged (l1GasUsed * l1GasPrice * scalar)
	FeeScalar     *big.Int // L1 fee scalar (fixed-point, 6 decimal places)
	SequencerAddr Address  // address of the sequencer that included this frame
}

// ExtendedFrameResult holds the full execution result of a single frame
// within a Frame transaction, including gas breakdown and sub-frame tracking.
type ExtendedFrameResult struct {
	FrameIndex   uint64
	Status       uint64
	GasUsed      uint64
	GasBreakdown FrameGasBreakdown
	Logs         []*Log
	ReturnData   []byte
	SubFrames    []SubFrameResult
}

// Succeeded returns true if the frame executed successfully.
func (r *ExtendedFrameResult) Succeeded() bool {
	return r.Status == FrameStatusSuccess
}

// Reverted returns true if the frame was explicitly reverted.
func (r *ExtendedFrameResult) Reverted() bool {
	return r.Status == FrameStatusRevert
}

// SubFrameGasUsed returns the total gas consumed by all sub-frames.
func (r *ExtendedFrameResult) SubFrameGasUsed() uint64 {
	var total uint64
	for _, sf := range r.SubFrames {
		total += sf.GasUsed
	}
	return total
}

// ExtendedFrameTxReceipt is the full receipt for a Frame transaction (EIP-8141).
type ExtendedFrameTxReceipt struct {
	CumulativeGasUsed uint64
	Payer             Address
	FrameResults      []ExtendedFrameResult
	RollupFields      *RollupFrameFields // nil if not a rollup frame

	// EffectiveGasPrice is the effective gas price after EIP-1559 base fee.
	EffectiveGasPrice *big.Int
	// BlobGasUsed tracks blob gas if the frame tx carried blobs.
	BlobGasUsed uint64
	BlobGasPrice *big.Int
}

// TotalGasUsed returns the sum of gas used across all frame results.
func (r *ExtendedFrameTxReceipt) TotalGasUsed() uint64 {
	var total uint64
	for _, fr := range r.FrameResults {
		total += fr.GasUsed
	}
	return total
}

// AllLogs returns all logs from all frame results in order.
func (r *ExtendedFrameTxReceipt) AllLogs() []*Log {
	var logs []*Log
	for _, fr := range r.FrameResults {
		logs = append(logs, fr.Logs...)
	}
	return logs
}

// SuccessCount returns the number of frames that succeeded.
func (r *ExtendedFrameTxReceipt) SuccessCount() int {
	count := 0
	for _, fr := range r.FrameResults {
		if fr.Succeeded() {
			count++
		}
	}
	return count
}

// FailureCount returns the number of frames that reverted or halted.
func (r *ExtendedFrameTxReceipt) FailureCount() int {
	return len(r.FrameResults) - r.SuccessCount()
}

// FrameAt returns the frame result at the given index or an error if out of range.
func (r *ExtendedFrameTxReceipt) FrameAt(index int) (*ExtendedFrameResult, error) {
	if index < 0 || index >= len(r.FrameResults) {
		return nil, ErrFrameResultInvalidIndex
	}
	return &r.FrameResults[index], nil
}

// TotalCalldataGas sums the calldata gas across all frames.
func (r *ExtendedFrameTxReceipt) TotalCalldataGas() uint64 {
	var total uint64
	for _, fr := range r.FrameResults {
		total += fr.GasBreakdown.CalldataGas
	}
	return total
}

// TotalRefund sums the gas refunds across all frames.
func (r *ExtendedFrameTxReceipt) TotalRefund() uint64 {
	var total uint64
	for _, fr := range r.FrameResults {
		total += fr.GasBreakdown.RefundGas
	}
	return total
}

// ComputeBloom computes the combined bloom filter for all logs in the receipt.
func (r *ExtendedFrameTxReceipt) ComputeBloom() Bloom {
	return LogsBloom(r.AllLogs())
}

// HasRollupFields returns true if rollup-specific fields are present.
func (r *ExtendedFrameTxReceipt) HasRollupFields() bool {
	return r.RollupFields != nil
}

// L1Cost returns the total L1 cost from rollup fields, or zero if not a rollup.
func (r *ExtendedFrameTxReceipt) L1Cost() *big.Int {
	if r.RollupFields == nil || r.RollupFields.L1Fee == nil {
		return new(big.Int)
	}
	return new(big.Int).Set(r.RollupFields.L1Fee)
}

// ValidateFrameReceipt performs structural validation on a frame receipt.
func ValidateFrameReceipt(r *ExtendedFrameTxReceipt) error {
	if r == nil {
		return ErrFrameReceiptNil
	}
	if len(r.FrameResults) == 0 {
		return ErrFrameResultsEmpty
	}
	totalGas := r.TotalGasUsed()
	if totalGas > r.CumulativeGasUsed {
		return ErrFrameGasExceedsCumul
	}
	for _, fr := range r.FrameResults {
		if len(fr.SubFrames) > MaxSubFrames {
			return ErrTooManySubFrames
		}
	}
	return nil
}

// --- RLP encoding of frame receipt ---

// frameGasBreakdownRLP is the RLP layout for FrameGasBreakdown.
type frameGasBreakdownRLP struct {
	IntrinsicGas uint64
	ExecutionGas uint64
	CalldataGas  uint64
	RefundGas    uint64
}

// subFrameResultRLP is the RLP layout for SubFrameResult.
type subFrameResultRLP struct {
	Target     Address
	Status     uint64
	GasUsed    uint64
	ReturnData []byte
}

// extendedFrameResultRLP is the RLP layout for ExtendedFrameResult.
type extendedFrameResultRLP struct {
	FrameIndex   uint64
	Status       uint64
	GasUsed      uint64
	GasBreakdown frameGasBreakdownRLP
	ReturnData   []byte
	SubFrames    []subFrameResultRLP
}

// EncodeExtendedFrameReceipt RLP-encodes the frame results (without logs)
// as [frameIndex, status, gasUsed, [intrinsic, execution, calldata, refund], returnData, [[target, status, gasUsed, returnData], ...]].
func EncodeExtendedFrameReceipt(r *ExtendedFrameTxReceipt) ([]byte, error) {
	if r == nil {
		return nil, ErrFrameReceiptNil
	}
	results := make([]extendedFrameResultRLP, len(r.FrameResults))
	for i, fr := range r.FrameResults {
		subs := make([]subFrameResultRLP, len(fr.SubFrames))
		for j, sf := range fr.SubFrames {
			subs[j] = subFrameResultRLP{
				Target:     sf.Target,
				Status:     sf.Status,
				GasUsed:    sf.GasUsed,
				ReturnData: sf.ReturnData,
			}
		}
		results[i] = extendedFrameResultRLP{
			FrameIndex: fr.FrameIndex,
			Status:     fr.Status,
			GasUsed:    fr.GasUsed,
			GasBreakdown: frameGasBreakdownRLP{
				IntrinsicGas: fr.GasBreakdown.IntrinsicGas,
				ExecutionGas: fr.GasBreakdown.ExecutionGas,
				CalldataGas:  fr.GasBreakdown.CalldataGas,
				RefundGas:    fr.GasBreakdown.RefundGas,
			},
			ReturnData: fr.ReturnData,
			SubFrames:  subs,
		}
	}
	return rlp.EncodeToBytes(results)
}

// DecodeExtendedFrameResults decodes RLP-encoded frame results back into
// a slice of ExtendedFrameResult. Logs must be populated separately.
func DecodeExtendedFrameResults(data []byte) ([]ExtendedFrameResult, error) {
	var raw []extendedFrameResultRLP
	if err := rlp.DecodeBytes(data, &raw); err != nil {
		return nil, fmt.Errorf("decode frame results: %w", err)
	}
	results := make([]ExtendedFrameResult, len(raw))
	for i, r := range raw {
		subs := make([]SubFrameResult, len(r.SubFrames))
		for j, sf := range r.SubFrames {
			subs[j] = SubFrameResult{
				Target:     sf.Target,
				Status:     sf.Status,
				GasUsed:    sf.GasUsed,
				ReturnData: sf.ReturnData,
			}
		}
		results[i] = ExtendedFrameResult{
			FrameIndex: r.FrameIndex,
			Status:     r.Status,
			GasUsed:    r.GasUsed,
			GasBreakdown: FrameGasBreakdown{
				IntrinsicGas: r.GasBreakdown.IntrinsicGas,
				ExecutionGas: r.GasBreakdown.ExecutionGas,
				CalldataGas:  r.GasBreakdown.CalldataGas,
				RefundGas:    r.GasBreakdown.RefundGas,
			},
			ReturnData: r.ReturnData,
			SubFrames:  subs,
		}
	}
	return results, nil
}

// ComputeL1Fee computes the L1 fee for a rollup frame given the parameters.
// L1Fee = l1GasUsed * l1GasPrice * feeScalar / 1e6
func ComputeL1Fee(l1GasUsed uint64, l1GasPrice, feeScalar *big.Int) *big.Int {
	if l1GasPrice == nil || feeScalar == nil {
		return new(big.Int)
	}
	fee := new(big.Int).SetUint64(l1GasUsed)
	fee.Mul(fee, l1GasPrice)
	fee.Mul(fee, feeScalar)
	fee.Div(fee, big.NewInt(1_000_000)) // 6 decimal places
	return fee
}
