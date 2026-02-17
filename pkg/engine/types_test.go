package engine

import (
	"encoding/json"
	"math/big"
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

func TestPayloadStatusConstants(t *testing.T) {
	if StatusValid != "VALID" {
		t.Error("StatusValid should be VALID")
	}
	if StatusInvalid != "INVALID" {
		t.Error("StatusInvalid should be INVALID")
	}
	if StatusSyncing != "SYNCING" {
		t.Error("StatusSyncing should be SYNCING")
	}
	if StatusAccepted != "ACCEPTED" {
		t.Error("StatusAccepted should be ACCEPTED")
	}
}

func TestPayloadIDString(t *testing.T) {
	id := PayloadID{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08}
	s := id.String()
	if s != "0x0102030405060708" {
		t.Errorf("PayloadID.String() = %s, want 0x0102030405060708", s)
	}
}

func TestPayloadStatusJSON(t *testing.T) {
	hash := types.HexToHash("0xdeadbeef")
	errMsg := "invalid block"
	status := PayloadStatusV1{
		Status:          StatusInvalid,
		LatestValidHash: &hash,
		ValidationError: &errMsg,
	}

	data, err := json.Marshal(status)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var decoded PayloadStatusV1
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if decoded.Status != StatusInvalid {
		t.Errorf("status = %s, want INVALID", decoded.Status)
	}
	if decoded.LatestValidHash == nil {
		t.Error("latestValidHash should not be nil")
	}
	if decoded.ValidationError == nil || *decoded.ValidationError != "invalid block" {
		t.Error("validationError mismatch")
	}
}

func TestForkchoiceStateJSON(t *testing.T) {
	fc := ForkchoiceStateV1{
		HeadBlockHash:      types.HexToHash("0x1111"),
		SafeBlockHash:      types.HexToHash("0x2222"),
		FinalizedBlockHash: types.HexToHash("0x3333"),
	}

	data, err := json.Marshal(fc)
	if err != nil {
		t.Fatal(err)
	}

	var decoded ForkchoiceStateV1
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}

	if decoded.HeadBlockHash != fc.HeadBlockHash {
		t.Error("head hash mismatch")
	}
	if decoded.SafeBlockHash != fc.SafeBlockHash {
		t.Error("safe hash mismatch")
	}
	if decoded.FinalizedBlockHash != fc.FinalizedBlockHash {
		t.Error("finalized hash mismatch")
	}
}

func TestExecutionPayloadV4(t *testing.T) {
	payload := ExecutionPayloadV4{
		ExecutionPayloadV3: ExecutionPayloadV3{
			ExecutionPayloadV2: ExecutionPayloadV2{
				ExecutionPayloadV1: ExecutionPayloadV1{
					ParentHash:    types.HexToHash("0xparent"),
					BlockNumber:   100,
					GasLimit:      30_000_000,
					GasUsed:       15_000_000,
					Timestamp:     1700000000,
					BaseFeePerGas: big.NewInt(1000000000),
					BlockHash:     types.HexToHash("0xblock"),
				},
				Withdrawals: []*Withdrawal{
					{Index: 1, ValidatorIndex: 100, Amount: 32000000000},
				},
			},
			BlobGasUsed:   131072,
			ExcessBlobGas: 0,
		},
		ExecutionRequests: [][]byte{{0x00, 0x01}, {0x01, 0x02}},
	}

	if payload.BlockNumber != 100 {
		t.Errorf("BlockNumber = %d, want 100", payload.BlockNumber)
	}
	if payload.GasLimit != 30_000_000 {
		t.Errorf("GasLimit = %d, want 30000000", payload.GasLimit)
	}
	if len(payload.Withdrawals) != 1 {
		t.Errorf("Withdrawals len = %d, want 1", len(payload.Withdrawals))
	}
	if payload.BlobGasUsed != 131072 {
		t.Errorf("BlobGasUsed = %d, want 131072", payload.BlobGasUsed)
	}
	if len(payload.ExecutionRequests) != 2 {
		t.Errorf("ExecutionRequests len = %d, want 2", len(payload.ExecutionRequests))
	}
}

func TestWithdrawal(t *testing.T) {
	w := Withdrawal{
		Index:          42,
		ValidatorIndex: 1000,
		Address:        types.HexToAddress("0xdead"),
		Amount:         32000000000, // 32 ETH in Gwei
	}

	if w.Index != 42 {
		t.Errorf("Index = %d, want 42", w.Index)
	}
	if w.Amount != 32000000000 {
		t.Errorf("Amount = %d, want 32000000000", w.Amount)
	}
}

func TestErrorCodes(t *testing.T) {
	if UnknownPayloadCode != -38001 {
		t.Errorf("UnknownPayloadCode = %d, want -38001", UnknownPayloadCode)
	}
	if InvalidForkchoiceStateCode != -38002 {
		t.Errorf("InvalidForkchoiceStateCode = %d, want -38002", InvalidForkchoiceStateCode)
	}
}
