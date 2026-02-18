package types

import (
	"math/big"
	"testing"
)

func TestReceiptStatusConstants(t *testing.T) {
	if ReceiptStatusFailed != 0 {
		t.Errorf("ReceiptStatusFailed = %d, want 0", ReceiptStatusFailed)
	}
	if ReceiptStatusSuccessful != 1 {
		t.Errorf("ReceiptStatusSuccessful = %d, want 1", ReceiptStatusSuccessful)
	}
}

func TestNewReceipt(t *testing.T) {
	r := NewReceipt(ReceiptStatusSuccessful, 42000)
	if r.Status != ReceiptStatusSuccessful {
		t.Errorf("Status = %d, want %d", r.Status, ReceiptStatusSuccessful)
	}
	if r.CumulativeGasUsed != 42000 {
		t.Errorf("CumulativeGasUsed = %d, want 42000", r.CumulativeGasUsed)
	}
}

func TestReceiptSucceeded(t *testing.T) {
	r := NewReceipt(ReceiptStatusSuccessful, 21000)
	if !r.Succeeded() {
		t.Error("Succeeded() should return true for status 1")
	}

	r = NewReceipt(ReceiptStatusFailed, 21000)
	if r.Succeeded() {
		t.Error("Succeeded() should return false for status 0")
	}
}

func TestDeriveReceiptFieldsSetsBlockContext(t *testing.T) {
	blockHash := HexToHash("0xdeadbeef")
	blockNumber := uint64(99)
	baseFee := big.NewInt(1_000_000_000)

	to := HexToAddress("0xbbbb")
	txs := []*Transaction{
		NewTransaction(&LegacyTx{Nonce: 0, GasPrice: big.NewInt(1), Gas: 21000, To: &to}),
		NewTransaction(&LegacyTx{Nonce: 1, GasPrice: big.NewInt(1), Gas: 21000, To: &to}),
	}

	receipts := []*Receipt{
		{Status: ReceiptStatusSuccessful, CumulativeGasUsed: 21000},
		{Status: ReceiptStatusSuccessful, CumulativeGasUsed: 42000},
	}

	DeriveReceiptFields(receipts, blockHash, blockNumber, baseFee, txs)

	for i, r := range receipts {
		if r.BlockHash != blockHash {
			t.Errorf("receipt[%d].BlockHash mismatch", i)
		}
		if r.BlockNumber == nil || r.BlockNumber.Uint64() != blockNumber {
			t.Errorf("receipt[%d].BlockNumber = %v, want %d", i, r.BlockNumber, blockNumber)
		}
		if r.TransactionIndex != uint(i) {
			t.Errorf("receipt[%d].TransactionIndex = %d, want %d", i, r.TransactionIndex, i)
		}
		if r.TxHash != txs[i].Hash() {
			t.Errorf("receipt[%d].TxHash mismatch", i)
		}
	}
}

func TestDeriveReceiptFieldsSetsGlobalLogIndex(t *testing.T) {
	blockHash := HexToHash("0xdeadbeef")
	blockNumber := uint64(50)
	baseFee := big.NewInt(1_000_000_000)

	to := HexToAddress("0xbbbb")
	txs := []*Transaction{
		NewTransaction(&LegacyTx{Nonce: 0, GasPrice: big.NewInt(1), Gas: 21000, To: &to}),
		NewTransaction(&LegacyTx{Nonce: 1, GasPrice: big.NewInt(1), Gas: 21000, To: &to}),
		NewTransaction(&LegacyTx{Nonce: 2, GasPrice: big.NewInt(1), Gas: 21000, To: &to}),
	}

	receipts := []*Receipt{
		{
			Status:            ReceiptStatusSuccessful,
			CumulativeGasUsed: 21000,
			Logs: []*Log{
				{Address: HexToAddress("0xc1")},
			},
		},
		{
			Status:            ReceiptStatusSuccessful,
			CumulativeGasUsed: 42000,
			Logs: []*Log{
				{Address: HexToAddress("0xc2")},
				{Address: HexToAddress("0xc2")},
				{Address: HexToAddress("0xc2")},
			},
		},
		{
			Status:            ReceiptStatusSuccessful,
			CumulativeGasUsed: 63000,
			Logs: []*Log{
				{Address: HexToAddress("0xc3")},
			},
		},
	}

	DeriveReceiptFields(receipts, blockHash, blockNumber, baseFee, txs)

	// Global log indices should be 0, 1, 2, 3, 4.
	expectedIndex := []uint{0, 1, 2, 3, 4}
	idx := 0
	for i, r := range receipts {
		for j, log := range r.Logs {
			if log.Index != expectedIndex[idx] {
				t.Errorf("receipt[%d].Logs[%d].Index = %d, want %d",
					i, j, log.Index, expectedIndex[idx])
			}
			if log.BlockHash != blockHash {
				t.Errorf("receipt[%d].Logs[%d].BlockHash mismatch", i, j)
			}
			if log.BlockNumber != blockNumber {
				t.Errorf("receipt[%d].Logs[%d].BlockNumber = %d, want %d",
					i, j, log.BlockNumber, blockNumber)
			}
			if log.TxIndex != uint(i) {
				t.Errorf("receipt[%d].Logs[%d].TxIndex = %d, want %d",
					i, j, log.TxIndex, i)
			}
			if log.TxHash != txs[i].Hash() {
				t.Errorf("receipt[%d].Logs[%d].TxHash mismatch", i, j)
			}
			idx++
		}
	}
}

func TestDeriveReceiptFieldsEmpty(t *testing.T) {
	// Should not panic on empty inputs.
	DeriveReceiptFields(nil, Hash{}, 0, nil, nil)
	DeriveReceiptFields([]*Receipt{}, Hash{}, 0, nil, []*Transaction{})
}

func TestReceiptBloomComputedFromLogs(t *testing.T) {
	addr := HexToAddress("0x1234")
	topic := HexToHash("0xabcd")

	logs := []*Log{
		{
			Address: addr,
			Topics:  []Hash{topic},
			Data:    []byte{0xff},
		},
	}

	bloom := LogsBloom(logs)
	if !BloomContains(bloom, addr.Bytes()) {
		t.Error("bloom should contain the log address")
	}
	if !BloomContains(bloom, topic.Bytes()) {
		t.Error("bloom should contain the log topic")
	}

	// Verify receipt with bloom set from logs roundtrips correctly.
	receipt := &Receipt{
		Status:            ReceiptStatusSuccessful,
		CumulativeGasUsed: 21000,
		Bloom:             bloom,
		Logs:              logs,
	}

	enc, err := receipt.EncodeRLP()
	if err != nil {
		t.Fatalf("EncodeRLP: %v", err)
	}

	decoded, err := DecodeReceiptRLP(enc)
	if err != nil {
		t.Fatalf("DecodeReceiptRLP: %v", err)
	}

	if decoded.Bloom != bloom {
		t.Error("bloom mismatch after RLP roundtrip")
	}
	if !BloomContains(decoded.Bloom, addr.Bytes()) {
		t.Error("decoded bloom should contain the log address")
	}
}
