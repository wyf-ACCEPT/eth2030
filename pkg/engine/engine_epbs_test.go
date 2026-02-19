package engine

import (
	"encoding/json"
	"math/big"
	"testing"
)

func setupRegistryWithBid(t *testing.T, slot uint64) (*EngineAPI, BuilderIndex) {
	t.Helper()
	api := NewEngineAPI(&mockBackend{})

	pk := newTestPubkey(0x01)
	reg := newTestRegistration(pk)
	b, err := api.builderRegistry.RegisterBuilder(reg, new(big.Int).Set(MinBuilderStake))
	if err != nil {
		t.Fatalf("RegisterBuilder: %v", err)
	}

	bid := newTestBid(b.Index, slot, 5000)
	if err := api.builderRegistry.SubmitBid(bid); err != nil {
		t.Fatalf("SubmitBid: %v", err)
	}

	return api, b.Index
}

func TestGetPayloadHeaderV1(t *testing.T) {
	api, _ := setupRegistryWithBid(t, 100)

	resp, err := api.GetPayloadHeaderV1(100)
	if err != nil {
		t.Fatalf("GetPayloadHeaderV1: %v", err)
	}
	if resp == nil || resp.Bid == nil {
		t.Fatal("expected non-nil response with bid")
	}
	if resp.Bid.Message.Slot != 100 {
		t.Errorf("bid slot = %d, want 100", resp.Bid.Message.Slot)
	}
	if resp.Bid.Message.Value != 5000 {
		t.Errorf("bid value = %d, want 5000", resp.Bid.Message.Value)
	}
}

func TestGetPayloadHeaderV1NoBuilderRegistry(t *testing.T) {
	api := &EngineAPI{}
	_, err := api.GetPayloadHeaderV1(100)
	if err != ErrUnknownPayload {
		t.Errorf("no registry: got %v, want ErrUnknownPayload", err)
	}
}

func TestGetPayloadHeaderV1NoBids(t *testing.T) {
	api := NewEngineAPI(&mockBackend{})
	_, err := api.GetPayloadHeaderV1(999)
	if err != ErrUnknownPayload {
		t.Errorf("no bids: got %v, want ErrUnknownPayload", err)
	}
}

func TestSubmitBlindedBlockV1Valid(t *testing.T) {
	api, builderIdx := setupRegistryWithBid(t, 100)

	req := SubmitBlindedBlockV1Request{
		Slot:         100,
		BuilderIndex: uint64(builderIdx),
	}

	resp, err := api.SubmitBlindedBlockV1(req)
	if err != nil {
		t.Fatalf("SubmitBlindedBlockV1: %v", err)
	}
	if resp.Status != StatusValid {
		t.Errorf("status = %s, want VALID", resp.Status)
	}
}

func TestSubmitBlindedBlockV1ZeroSlot(t *testing.T) {
	api := NewEngineAPI(&mockBackend{})

	req := SubmitBlindedBlockV1Request{Slot: 0, BuilderIndex: 1}
	_, err := api.SubmitBlindedBlockV1(req)
	if err != ErrInvalidParams {
		t.Errorf("zero slot: got %v, want ErrInvalidParams", err)
	}
}

func TestSubmitBlindedBlockV1UnknownBuilder(t *testing.T) {
	api, _ := setupRegistryWithBid(t, 100)

	req := SubmitBlindedBlockV1Request{
		Slot:         100,
		BuilderIndex: 999, // unknown
	}

	resp, err := api.SubmitBlindedBlockV1(req)
	if err != nil {
		t.Fatalf("SubmitBlindedBlockV1: %v", err)
	}
	if resp.Status != StatusInvalid {
		t.Errorf("status = %s, want INVALID", resp.Status)
	}
}

func TestGetPayloadHeaderV1JSONRPCHandler(t *testing.T) {
	api, _ := setupRegistryWithBid(t, 100)

	slotJSON, _ := json.Marshal(uint64(100))
	result, rpcErr := api.handleGetPayloadHeaderV1([]json.RawMessage{slotJSON})
	if rpcErr != nil {
		t.Fatalf("handleGetPayloadHeaderV1: code=%d msg=%s", rpcErr.Code, rpcErr.Message)
	}

	resp, ok := result.(*GetPayloadHeaderV1Response)
	if !ok {
		t.Fatal("result is not *GetPayloadHeaderV1Response")
	}
	if resp.Bid.Message.Value != 5000 {
		t.Errorf("value = %d, want 5000", resp.Bid.Message.Value)
	}
}

func TestSubmitBlindedBlockV1JSONRPCHandler(t *testing.T) {
	api, builderIdx := setupRegistryWithBid(t, 100)

	req := SubmitBlindedBlockV1Request{
		Slot:         100,
		BuilderIndex: uint64(builderIdx),
	}
	reqJSON, _ := json.Marshal(req)
	result, rpcErr := api.handleSubmitBlindedBlockV1([]json.RawMessage{reqJSON})
	if rpcErr != nil {
		t.Fatalf("handleSubmitBlindedBlockV1: code=%d msg=%s", rpcErr.Code, rpcErr.Message)
	}

	resp, ok := result.(*SubmitBlindedBlockV1Response)
	if !ok {
		t.Fatal("result is not *SubmitBlindedBlockV1Response")
	}
	if resp.Status != StatusValid {
		t.Errorf("status = %s, want VALID", resp.Status)
	}
}
