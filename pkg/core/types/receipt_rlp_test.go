package types

import (
	"testing"
)

func TestReceiptRLPRoundTrip(t *testing.T) {
	r := &Receipt{
		Status:            ReceiptStatusSuccessful,
		CumulativeGasUsed: 21000,
		Bloom:             Bloom{},
		Logs: []*Log{
			{
				Address: HexToAddress("0xdead"),
				Topics: []Hash{
					HexToHash("0xaaaa"),
					HexToHash("0xbbbb"),
				},
				Data: []byte("hello"),
			},
			{
				Address: HexToAddress("0xbeef"),
				Topics:  []Hash{HexToHash("0xcccc")},
				Data:    []byte{},
			},
		},
	}

	enc, err := r.EncodeRLP()
	if err != nil {
		t.Fatalf("EncodeRLP failed: %v", err)
	}
	if len(enc) == 0 {
		t.Fatal("EncodeRLP returned empty bytes")
	}

	decoded, err := DecodeReceiptRLP(enc)
	if err != nil {
		t.Fatalf("DecodeReceiptRLP failed: %v", err)
	}

	if decoded.Status != r.Status {
		t.Fatalf("Status mismatch: got %d, want %d", decoded.Status, r.Status)
	}
	if decoded.CumulativeGasUsed != r.CumulativeGasUsed {
		t.Fatalf("CumulativeGasUsed mismatch: got %d, want %d", decoded.CumulativeGasUsed, r.CumulativeGasUsed)
	}
	if decoded.Bloom != r.Bloom {
		t.Fatal("Bloom mismatch")
	}
	if len(decoded.Logs) != len(r.Logs) {
		t.Fatalf("Logs count mismatch: got %d, want %d", len(decoded.Logs), len(r.Logs))
	}

	for i, log := range decoded.Logs {
		orig := r.Logs[i]
		if log.Address != orig.Address {
			t.Fatalf("Log[%d] Address mismatch", i)
		}
		if len(log.Topics) != len(orig.Topics) {
			t.Fatalf("Log[%d] Topics count mismatch: got %d, want %d", i, len(log.Topics), len(orig.Topics))
		}
		for j, topic := range log.Topics {
			if topic != orig.Topics[j] {
				t.Fatalf("Log[%d] Topic[%d] mismatch", i, j)
			}
		}
		if string(log.Data) != string(orig.Data) {
			t.Fatalf("Log[%d] Data mismatch", i)
		}
	}
}

func TestReceiptRLPEmptyLogs(t *testing.T) {
	r := &Receipt{
		Status:            ReceiptStatusFailed,
		CumulativeGasUsed: 42000,
	}

	enc, err := r.EncodeRLP()
	if err != nil {
		t.Fatalf("EncodeRLP failed: %v", err)
	}

	decoded, err := DecodeReceiptRLP(enc)
	if err != nil {
		t.Fatalf("DecodeReceiptRLP failed: %v", err)
	}

	if decoded.Status != r.Status {
		t.Fatalf("Status mismatch: got %d, want %d", decoded.Status, r.Status)
	}
	if decoded.CumulativeGasUsed != r.CumulativeGasUsed {
		t.Fatalf("CumulativeGasUsed mismatch: got %d, want %d", decoded.CumulativeGasUsed, r.CumulativeGasUsed)
	}
	if len(decoded.Logs) != 0 {
		t.Fatalf("Expected 0 logs, got %d", len(decoded.Logs))
	}
}

func TestReceiptTypedRoundTrip(t *testing.T) {
	r := &Receipt{
		Type:              DynamicFeeTxType,
		Status:            ReceiptStatusSuccessful,
		CumulativeGasUsed: 63000,
		Logs: []*Log{
			{
				Address: HexToAddress("0x1234"),
				Topics:  []Hash{HexToHash("0xeeee")},
				Data:    []byte("typed"),
			},
		},
	}

	enc, err := r.EncodeRLP()
	if err != nil {
		t.Fatalf("EncodeRLP failed: %v", err)
	}

	// Typed receipts should have the type byte prefix.
	if enc[0] != DynamicFeeTxType {
		t.Fatalf("Expected type prefix %d, got %d", DynamicFeeTxType, enc[0])
	}

	decoded, err := DecodeReceiptRLP(enc)
	if err != nil {
		t.Fatalf("DecodeReceiptRLP failed: %v", err)
	}

	if decoded.Type != r.Type {
		t.Fatalf("Type mismatch: got %d, want %d", decoded.Type, r.Type)
	}
	if decoded.Status != r.Status {
		t.Fatal("Status mismatch")
	}
	if decoded.CumulativeGasUsed != r.CumulativeGasUsed {
		t.Fatal("CumulativeGasUsed mismatch")
	}
}

func TestDeriveSha(t *testing.T) {
	r1 := &Receipt{
		Status:            ReceiptStatusSuccessful,
		CumulativeGasUsed: 21000,
	}
	r2 := &Receipt{
		Status:            ReceiptStatusSuccessful,
		CumulativeGasUsed: 42000,
	}

	root := DeriveSha([]*Receipt{r1, r2})
	if root.IsZero() {
		t.Fatal("DeriveSha should not return zero hash")
	}

	// Same receipts should produce the same root.
	root2 := DeriveSha([]*Receipt{r1, r2})
	if root != root2 {
		t.Fatal("DeriveSha should be deterministic")
	}

	// Different order should produce different root.
	root3 := DeriveSha([]*Receipt{r2, r1})
	if root == root3 {
		t.Fatal("DeriveSha should be order-dependent")
	}
}

func TestDeriveShaEmpty(t *testing.T) {
	root := DeriveSha([]*Receipt{})
	// Hash of empty data is the keccak of nothing.
	if root.IsZero() {
		t.Fatal("DeriveSha of empty list should return keccak of empty input")
	}
}
