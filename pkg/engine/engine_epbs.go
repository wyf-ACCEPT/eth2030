package engine

import (
	"encoding/json"
	"fmt"

	"github.com/eth2030/eth2030/epbs"
)

// GetPayloadHeaderV1Response is the response for engine_getPayloadHeaderV1.
// Returns the winning builder bid for the requested slot, providing the
// proposer with the execution payload commitment without the full payload.
type GetPayloadHeaderV1Response struct {
	Bid *epbs.SignedBuilderBid `json:"bid"`
}

// GetPayloadHeaderV1 returns the winning builder bid for the given slot.
// The proposer calls this to get the execution payload commitment to include
// in the beacon block body per EIP-7732.
func (api *EngineAPI) GetPayloadHeaderV1(slot uint64) (*GetPayloadHeaderV1Response, error) {
	if api.builderRegistry == nil {
		return nil, ErrUnknownPayload
	}

	bestBid, err := api.builderRegistry.GetBestBid(slot)
	if err != nil {
		return nil, ErrUnknownPayload
	}

	// Convert engine builder bid to epbs type.
	epbsBid := &epbs.SignedBuilderBid{
		Message: epbs.BuilderBid{
			ParentBlockHash:    bestBid.Message.ParentBlockHash,
			ParentBlockRoot:    bestBid.Message.ParentBlockRoot,
			BlockHash:          bestBid.Message.BlockHash,
			PrevRandao:         bestBid.Message.PrevRandao,
			FeeRecipient:       bestBid.Message.FeeRecipient,
			GasLimit:           bestBid.Message.GasLimit,
			BuilderIndex:       epbs.BuilderIndex(bestBid.Message.BuilderIndex),
			Slot:               bestBid.Message.Slot,
			Value:              bestBid.Message.Value,
			ExecutionPayment:   bestBid.Message.ExecutionPayment,
			BlobKZGCommitments: bestBid.Message.BlobKZGCommitments,
		},
	}

	return &GetPayloadHeaderV1Response{Bid: epbsBid}, nil
}

// SubmitBlindedBlockV1Request contains a blinded block submission from the proposer.
// The proposer has committed to a builder's bid and submits it here so the
// EL can track the commitment and later verify the builder's reveal.
type SubmitBlindedBlockV1Request struct {
	Slot            uint64     `json:"slot"`
	BuilderIndex    uint64     `json:"builderIndex"`
	BidHash         [32]byte   `json:"bidHash"`
	BeaconBlockRoot [32]byte   `json:"beaconBlockRoot"`
}

// SubmitBlindedBlockV1Response is the response to engine_submitBlindedBlockV1.
type SubmitBlindedBlockV1Response struct {
	Status string `json:"status"`
}

// SubmitBlindedBlockV1 processes a blinded block submission from the CL proposer.
// The proposer has selected a builder's bid and committed to it in the beacon block.
// This informs the EL so it can track the commitment.
func (api *EngineAPI) SubmitBlindedBlockV1(req SubmitBlindedBlockV1Request) (*SubmitBlindedBlockV1Response, error) {
	if api.builderRegistry == nil {
		return nil, ErrUnknownPayload
	}

	if req.Slot == 0 {
		return nil, ErrInvalidParams
	}

	// Verify there is a bid from the specified builder for this slot.
	bids := api.builderRegistry.GetBidsForSlot(req.Slot)
	found := false
	for _, bid := range bids {
		if uint64(bid.Message.BuilderIndex) == req.BuilderIndex {
			found = true
			break
		}
	}

	if !found {
		return &SubmitBlindedBlockV1Response{Status: StatusInvalid}, nil
	}

	return &SubmitBlindedBlockV1Response{Status: StatusValid}, nil
}

// handleGetPayloadHeaderV1 is the JSON-RPC handler for engine_getPayloadHeaderV1.
func (api *EngineAPI) handleGetPayloadHeaderV1(params []json.RawMessage) (any, *jsonrpcError) {
	if len(params) != 1 {
		return nil, &jsonrpcError{
			Code:    InvalidParamsCode,
			Message: fmt.Sprintf("expected 1 param, got %d", len(params)),
		}
	}

	var slot uint64
	if err := json.Unmarshal(params[0], &slot); err != nil {
		return nil, &jsonrpcError{
			Code:    InvalidParamsCode,
			Message: fmt.Sprintf("invalid slot: %v", err),
		}
	}

	result, err := api.GetPayloadHeaderV1(slot)
	if err != nil {
		return nil, engineErrorToRPC(err)
	}
	return result, nil
}

// handleSubmitBlindedBlockV1 is the JSON-RPC handler for engine_submitBlindedBlockV1.
func (api *EngineAPI) handleSubmitBlindedBlockV1(params []json.RawMessage) (any, *jsonrpcError) {
	if len(params) != 1 {
		return nil, &jsonrpcError{
			Code:    InvalidParamsCode,
			Message: fmt.Sprintf("expected 1 param, got %d", len(params)),
		}
	}

	var req SubmitBlindedBlockV1Request
	if err := json.Unmarshal(params[0], &req); err != nil {
		return nil, &jsonrpcError{
			Code:    InvalidParamsCode,
			Message: fmt.Sprintf("invalid request: %v", err),
		}
	}

	result, err := api.SubmitBlindedBlockV1(req)
	if err != nil {
		return nil, engineErrorToRPC(err)
	}
	return result, nil
}
