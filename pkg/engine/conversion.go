package engine

import (
	"math/big"

	"github.com/eth2028/eth2028/core/types"
)

// PayloadToHeader converts an ExecutionPayloadV4 to a block Header.
func PayloadToHeader(payload *ExecutionPayloadV4) *types.Header {
	header := &types.Header{
		ParentHash:    payload.ParentHash,
		Coinbase:      payload.FeeRecipient,
		Root:          payload.StateRoot,
		ReceiptHash:   payload.ReceiptsRoot,
		Bloom:         payload.LogsBloom,
		MixDigest:     payload.PrevRandao,
		Number:        new(big.Int).SetUint64(payload.BlockNumber),
		GasLimit:      payload.GasLimit,
		GasUsed:       payload.GasUsed,
		Time:          payload.Timestamp,
		Extra:         payload.ExtraData,
		BaseFee:       payload.BaseFeePerGas,
		BlobGasUsed:   &payload.BlobGasUsed,
		ExcessBlobGas: &payload.ExcessBlobGas,
	}
	// Post-merge: difficulty is always 0, uncle hash is empty.
	header.Difficulty = new(big.Int)
	header.UncleHash = types.EmptyUncleHash
	return header
}

// HeaderToPayloadFields extracts common payload fields from a Header.
func HeaderToPayloadFields(header *types.Header) ExecutionPayloadV1 {
	return ExecutionPayloadV1{
		ParentHash:    header.ParentHash,
		FeeRecipient:  header.Coinbase,
		StateRoot:     header.Root,
		ReceiptsRoot:  header.ReceiptHash,
		LogsBloom:     header.Bloom,
		PrevRandao:    header.MixDigest,
		BlockNumber:   header.Number.Uint64(),
		GasLimit:      header.GasLimit,
		GasUsed:       header.GasUsed,
		Timestamp:     header.Time,
		ExtraData:     header.Extra,
		BaseFeePerGas: header.BaseFee,
	}
}

// WithdrawalsToEngine converts core Withdrawal types to engine Withdrawal types.
func WithdrawalsToEngine(ws []*types.Withdrawal) []*Withdrawal {
	result := make([]*Withdrawal, len(ws))
	for i, w := range ws {
		result[i] = &Withdrawal{
			Index:          w.Index,
			ValidatorIndex: w.ValidatorIndex,
			Address:        w.Address,
			Amount:         w.Amount,
		}
	}
	return result
}

// WithdrawalsToCore converts engine Withdrawal types to core Withdrawal types.
func WithdrawalsToCore(ws []*Withdrawal) []*types.Withdrawal {
	result := make([]*types.Withdrawal, len(ws))
	for i, w := range ws {
		result[i] = &types.Withdrawal{
			Index:          w.Index,
			ValidatorIndex: w.ValidatorIndex,
			Address:        w.Address,
			Amount:         w.Amount,
		}
	}
	return result
}
