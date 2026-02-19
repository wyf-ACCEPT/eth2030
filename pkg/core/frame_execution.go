package core

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/eth2028/eth2028/core/types"
)

// Frame execution errors.
var (
	ErrFrameSenderNotApproved = errors.New("frame tx: SENDER mode requires sender approval")
	ErrFrameVerifyNoApprove   = errors.New("frame tx: VERIFY frame did not call APPROVE")
	ErrFramePayerNotApproved  = errors.New("frame tx: payer not approved after all frames")
	ErrFrameNonceMismatch     = errors.New("frame tx: nonce mismatch")
	ErrFrameInvalidMode       = errors.New("frame tx: invalid frame mode")
)

// FrameExecutionContext tracks transaction-scoped state for EIP-8141 frame execution.
type FrameExecutionContext struct {
	SenderApproved bool
	PayerApproved  bool
	Payer          types.Address
	CurrentFrame   int
	FrameResults   []types.FrameResult
}

// FrameCallFunc is a callback to execute a single frame call.
// Parameters: caller, target, gasLimit, data, mode, frameIndex.
// Returns: status (0=fail, 1=success), gasUsed, logs, whether APPROVE was called, approveScope, error.
type FrameCallFunc func(
	caller types.Address,
	target types.Address,
	gasLimit uint64,
	data []byte,
	mode uint8,
	frameIndex int,
) (status uint64, gasUsed uint64, logs []*types.Log, approved bool, approveScope uint8, err error)

// ExecuteFrameTx runs all frames in a FrameTx according to EIP-8141 semantics.
// It returns the execution context (with frame results and payer info) or an error
// if the transaction is invalid.
//
// The callFn callback is invoked for each frame to perform the actual EVM call.
// stateNonce is the current nonce of tx.Sender in state (for stateful validation).
func ExecuteFrameTx(tx *types.FrameTx, stateNonce uint64, callFn FrameCallFunc) (*FrameExecutionContext, error) {
	// Stateful validation: nonce check.
	if tx.Nonce != stateNonce {
		return nil, fmt.Errorf("%w: got %d, want %d", ErrFrameNonceMismatch, tx.Nonce, stateNonce)
	}

	ctx := &FrameExecutionContext{
		FrameResults: make([]types.FrameResult, len(tx.Frames)),
	}

	for i, frame := range tx.Frames {
		ctx.CurrentFrame = i

		// Determine caller and target per mode.
		var caller types.Address
		target := tx.Sender // default target is sender when frame.Target is nil
		if frame.Target != nil {
			target = *frame.Target
		}

		switch frame.Mode {
		case types.ModeDefault:
			caller = types.EntryPointAddress
		case types.ModeVerify:
			caller = types.EntryPointAddress
		case types.ModeSender:
			if !ctx.SenderApproved {
				return nil, ErrFrameSenderNotApproved
			}
			caller = tx.Sender
		default:
			return nil, fmt.Errorf("%w: %d", ErrFrameInvalidMode, frame.Mode)
		}

		// Execute the frame call.
		status, gasUsed, logs, approved, approveScope, err := callFn(
			caller, target, frame.GasLimit, frame.Data, frame.Mode, i,
		)
		if err != nil {
			// A call error is recorded as a failed frame, not a tx-level error.
			status = types.ReceiptStatusFailed
			gasUsed = frame.GasLimit
		}

		ctx.FrameResults[i] = types.FrameResult{
			Status:  status,
			GasUsed: gasUsed,
			Logs:    logs,
		}

		// Process APPROVE if it was called during this frame.
		if approved {
			if err := ctx.processApprove(approveScope, tx.Sender, target); err != nil {
				return nil, err
			}
		}

		// VERIFY frames must have successfully called APPROVE.
		if frame.Mode == types.ModeVerify {
			if !approved {
				return nil, ErrFrameVerifyNoApprove
			}
		}
	}

	// After all frames: payer must be approved.
	if !ctx.PayerApproved {
		return nil, ErrFramePayerNotApproved
	}

	return ctx, nil
}

// processApprove handles the APPROVE opcode's effect on transaction-scoped state.
func (ctx *FrameExecutionContext) processApprove(scope uint8, sender, target types.Address) error {
	switch scope {
	case 0: // Approval of execution only.
		if ctx.SenderApproved {
			return errors.New("frame tx: sender already approved")
		}
		if target != sender {
			return errors.New("frame tx: APPROVE(0) caller must be sender")
		}
		ctx.SenderApproved = true

	case 1: // Approval of payment only.
		if ctx.PayerApproved {
			return errors.New("frame tx: payer already approved")
		}
		if !ctx.SenderApproved {
			return errors.New("frame tx: APPROVE(1) requires sender approval first")
		}
		ctx.PayerApproved = true
		ctx.Payer = target

	case 2: // Approval of execution and payment.
		if ctx.SenderApproved {
			return errors.New("frame tx: sender already approved")
		}
		if ctx.PayerApproved {
			return errors.New("frame tx: payer already approved")
		}
		if target != sender {
			return errors.New("frame tx: APPROVE(2) caller must be sender")
		}
		ctx.SenderApproved = true
		ctx.PayerApproved = true
		ctx.Payer = target

	default:
		return fmt.Errorf("frame tx: invalid APPROVE scope %d", scope)
	}
	return nil
}

// BuildFrameReceipt creates a FrameTxReceipt from an execution context.
func BuildFrameReceipt(ctx *FrameExecutionContext, cumulativeGasUsed uint64) *types.FrameTxReceipt {
	return &types.FrameTxReceipt{
		CumulativeGasUsed: cumulativeGasUsed,
		Payer:             ctx.Payer,
		FrameResults:      ctx.FrameResults,
	}
}

// CalcFrameRefund calculates the gas refund for a frame transaction.
// refund = sum(frame.gas_limit) - total_gas_used
func CalcFrameRefund(tx *types.FrameTx, ctx *FrameExecutionContext) uint64 {
	var totalLimit, totalUsed uint64
	for i, frame := range tx.Frames {
		totalLimit += frame.GasLimit
		if i < len(ctx.FrameResults) {
			totalUsed += ctx.FrameResults[i].GasUsed
		}
	}
	if totalUsed >= totalLimit {
		return 0
	}
	return totalLimit - totalUsed
}

// MaxFrameTxCost returns the maximum ETH cost of a frame transaction given max fees.
// max_cost = tx_gas_limit * max_fee_per_gas + blob_count * GAS_PER_BLOB * max_fee_per_blob_gas
func MaxFrameTxCost(tx *types.FrameTx) *big.Int {
	gasLimit := types.CalcFrameTxGas(tx)
	cost := new(big.Int).Mul(new(big.Int).SetUint64(gasLimit), bigOrZeroLocal(tx.MaxFeePerGas))

	if len(tx.BlobVersionedHashes) > 0 && tx.MaxFeePerBlobGas != nil {
		blobGas := uint64(len(tx.BlobVersionedHashes)) * 131072 // GAS_PER_BLOB
		blobCost := new(big.Int).Mul(new(big.Int).SetUint64(blobGas), tx.MaxFeePerBlobGas)
		cost.Add(cost, blobCost)
	}
	return cost
}

func bigOrZeroLocal(i *big.Int) *big.Int {
	if i != nil {
		return i
	}
	return new(big.Int)
}
