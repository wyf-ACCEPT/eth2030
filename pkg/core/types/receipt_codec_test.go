package types

import (
	"testing"
)

func makeTestReceipt(txType uint8, status uint64, gas uint64, logs []*Log) *Receipt {
	return &Receipt{
		Type:              txType,
		Status:            status,
		CumulativeGasUsed: gas,
		Bloom:             Bloom{},
		Logs:              logs,
	}
}

func TestReceiptCodecEncodeDecode_Legacy(t *testing.T) {
	codec := &ReceiptCodec{}

	logs := []*Log{
		{
			Address: HexToAddress("0xdead"),
			Topics:  []Hash{HexToHash("0xaaaa")},
			Data:    []byte("hello"),
		},
	}
	r := makeTestReceipt(LegacyTxType, ReceiptStatusSuccessful, 21000, logs)

	enc, err := codec.EncodeReceipt(r)
	if err != nil {
		t.Fatalf("EncodeReceipt: %v", err)
	}
	if len(enc) == 0 {
		t.Fatal("encoded receipt is empty")
	}

	// Legacy receipt should not have a type prefix byte < 0x80.
	if enc[0] < 0x80 {
		t.Fatalf("legacy receipt should not have type prefix, got 0x%02x", enc[0])
	}

	decoded, err := codec.DecodeReceipt(enc)
	if err != nil {
		t.Fatalf("DecodeReceipt: %v", err)
	}

	if !ReceiptEqual(r, decoded) {
		t.Error("decoded receipt does not equal original")
	}
}

func TestReceiptCodecEncodeDecode_Typed(t *testing.T) {
	codec := &ReceiptCodec{}

	types := []uint8{AccessListTxType, DynamicFeeTxType, BlobTxType}

	for _, txType := range types {
		logs := []*Log{
			{
				Address: HexToAddress("0x1234"),
				Topics:  []Hash{HexToHash("0xeeee"), HexToHash("0xffff")},
				Data:    []byte("typed"),
			},
		}
		r := makeTestReceipt(txType, ReceiptStatusSuccessful, 63000, logs)

		enc, err := codec.EncodeReceipt(r)
		if err != nil {
			t.Fatalf("EncodeReceipt type %d: %v", txType, err)
		}

		// Typed receipt should start with the type byte.
		if enc[0] != txType {
			t.Fatalf("expected type prefix %d, got %d", txType, enc[0])
		}

		decoded, err := codec.DecodeReceipt(enc)
		if err != nil {
			t.Fatalf("DecodeReceipt type %d: %v", txType, err)
		}

		if !ReceiptEqual(r, decoded) {
			t.Errorf("decoded receipt does not equal original for type %d", txType)
		}
	}
}

func TestReceiptCodecEncodeDecode_EmptyLogs(t *testing.T) {
	codec := &ReceiptCodec{}

	r := makeTestReceipt(LegacyTxType, ReceiptStatusFailed, 42000, nil)

	enc, err := codec.EncodeReceipt(r)
	if err != nil {
		t.Fatalf("EncodeReceipt: %v", err)
	}

	decoded, err := codec.DecodeReceipt(enc)
	if err != nil {
		t.Fatalf("DecodeReceipt: %v", err)
	}

	if decoded.Status != ReceiptStatusFailed {
		t.Errorf("Status = %d, want %d", decoded.Status, ReceiptStatusFailed)
	}
	if decoded.CumulativeGasUsed != 42000 {
		t.Errorf("CumulativeGasUsed = %d, want 42000", decoded.CumulativeGasUsed)
	}
	if len(decoded.Logs) != 0 {
		t.Errorf("expected 0 logs, got %d", len(decoded.Logs))
	}
}

func TestReceiptCodecBatchEncodeDecode(t *testing.T) {
	codec := &ReceiptCodec{}

	receipts := []*Receipt{
		makeTestReceipt(LegacyTxType, ReceiptStatusSuccessful, 21000, []*Log{
			{Address: HexToAddress("0xaaaa"), Topics: []Hash{HexToHash("0x01")}, Data: []byte("a")},
		}),
		makeTestReceipt(DynamicFeeTxType, ReceiptStatusSuccessful, 42000, []*Log{
			{Address: HexToAddress("0xbbbb"), Topics: nil, Data: []byte("b")},
		}),
		makeTestReceipt(BlobTxType, ReceiptStatusFailed, 63000, nil),
	}

	enc, err := codec.EncodeReceipts(receipts)
	if err != nil {
		t.Fatalf("EncodeReceipts: %v", err)
	}
	if len(enc) == 0 {
		t.Fatal("encoded receipts are empty")
	}

	decoded, err := codec.DecodeReceipts(enc)
	if err != nil {
		t.Fatalf("DecodeReceipts: %v", err)
	}

	if len(decoded) != len(receipts) {
		t.Fatalf("decoded %d receipts, want %d", len(decoded), len(receipts))
	}

	for i := range receipts {
		if !ReceiptEqual(receipts[i], decoded[i]) {
			t.Errorf("receipt[%d] mismatch after batch roundtrip", i)
		}
	}
}

func TestReceiptCodecBatchEncodeDecodeEmpty(t *testing.T) {
	codec := &ReceiptCodec{}

	enc, err := codec.EncodeReceipts([]*Receipt{})
	if err != nil {
		t.Fatalf("EncodeReceipts empty: %v", err)
	}

	decoded, err := codec.DecodeReceipts(enc)
	if err != nil {
		t.Fatalf("DecodeReceipts empty: %v", err)
	}

	if len(decoded) != 0 {
		t.Errorf("expected 0 decoded receipts, got %d", len(decoded))
	}
}

func TestReceiptCodecBatchEncodeDecodeNil(t *testing.T) {
	codec := &ReceiptCodec{}

	enc, err := codec.EncodeReceipts(nil)
	if err != nil {
		t.Fatalf("EncodeReceipts nil: %v", err)
	}

	decoded, err := codec.DecodeReceipts(enc)
	if err != nil {
		t.Fatalf("DecodeReceipts nil batch: %v", err)
	}

	if len(decoded) != 0 {
		t.Errorf("expected 0 decoded receipts, got %d", len(decoded))
	}
}

func TestDeriveReceiptCodecFields(t *testing.T) {
	blockHash := HexToHash("0xdeadbeef")
	blockNumber := uint64(100)

	receipts := []*Receipt{
		{
			Status:            ReceiptStatusSuccessful,
			CumulativeGasUsed: 21000,
			Logs: []*Log{
				{Address: HexToAddress("0xaa")},
			},
		},
		{
			Status:            ReceiptStatusSuccessful,
			CumulativeGasUsed: 42000,
			Logs: []*Log{
				{Address: HexToAddress("0xbb")},
				{Address: HexToAddress("0xcc")},
			},
		},
		{
			Status:            ReceiptStatusFailed,
			CumulativeGasUsed: 63000,
			Logs:              nil,
		},
	}

	DeriveReceiptCodecFields(receipts, blockHash, blockNumber)

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
	}

	// Check global log indices: 0, 1, 2.
	expectedIndices := []uint{0, 1, 2}
	idx := 0
	for i, r := range receipts {
		for j, log := range r.Logs {
			if log.Index != expectedIndices[idx] {
				t.Errorf("receipt[%d].Logs[%d].Index = %d, want %d", i, j, log.Index, expectedIndices[idx])
			}
			if log.BlockHash != blockHash {
				t.Errorf("receipt[%d].Logs[%d].BlockHash mismatch", i, j)
			}
			if log.BlockNumber != blockNumber {
				t.Errorf("receipt[%d].Logs[%d].BlockNumber mismatch", i, j)
			}
			if log.TxIndex != uint(i) {
				t.Errorf("receipt[%d].Logs[%d].TxIndex = %d, want %d", i, j, log.TxIndex, i)
			}
			idx++
		}
	}
}

func TestReceiptEqual(t *testing.T) {
	logs := []*Log{
		{
			Address: HexToAddress("0x1234"),
			Topics:  []Hash{HexToHash("0xaaaa")},
			Data:    []byte("test"),
		},
	}

	a := makeTestReceipt(DynamicFeeTxType, ReceiptStatusSuccessful, 21000, logs)
	b := makeTestReceipt(DynamicFeeTxType, ReceiptStatusSuccessful, 21000, []*Log{
		{
			Address: HexToAddress("0x1234"),
			Topics:  []Hash{HexToHash("0xaaaa")},
			Data:    []byte("test"),
		},
	})

	if !ReceiptEqual(a, b) {
		t.Error("identical receipts should be equal")
	}

	// Both nil.
	if !ReceiptEqual(nil, nil) {
		t.Error("nil receipts should be equal")
	}

	// One nil.
	if ReceiptEqual(a, nil) {
		t.Error("receipt vs nil should not be equal")
	}
	if ReceiptEqual(nil, b) {
		t.Error("nil vs receipt should not be equal")
	}
}

func TestReceiptEqualDifferences(t *testing.T) {
	base := func() *Receipt {
		return makeTestReceipt(DynamicFeeTxType, ReceiptStatusSuccessful, 21000, []*Log{
			{Address: HexToAddress("0x1234"), Topics: []Hash{HexToHash("0xaaaa")}, Data: []byte("x")},
		})
	}

	// Different type.
	a := base()
	b := base()
	b.Type = BlobTxType
	if ReceiptEqual(a, b) {
		t.Error("different types should not be equal")
	}

	// Different status.
	a = base()
	b = base()
	b.Status = ReceiptStatusFailed
	if ReceiptEqual(a, b) {
		t.Error("different status should not be equal")
	}

	// Different gas.
	a = base()
	b = base()
	b.CumulativeGasUsed = 99999
	if ReceiptEqual(a, b) {
		t.Error("different gas should not be equal")
	}

	// Different log count.
	a = base()
	b = base()
	b.Logs = nil
	if ReceiptEqual(a, b) {
		t.Error("different log count should not be equal")
	}

	// Different log data.
	a = base()
	b = base()
	b.Logs[0].Data = []byte("y")
	if ReceiptEqual(a, b) {
		t.Error("different log data should not be equal")
	}

	// Different log address.
	a = base()
	b = base()
	b.Logs[0].Address = HexToAddress("0x5678")
	if ReceiptEqual(a, b) {
		t.Error("different log address should not be equal")
	}

	// Different log topics.
	a = base()
	b = base()
	b.Logs[0].Topics = []Hash{HexToHash("0xbbbb")}
	if ReceiptEqual(a, b) {
		t.Error("different log topics should not be equal")
	}
}

func TestReceiptSize(t *testing.T) {
	r := makeTestReceipt(LegacyTxType, ReceiptStatusSuccessful, 21000, []*Log{
		{Address: HexToAddress("0xdead"), Topics: []Hash{HexToHash("0xaaaa")}, Data: []byte("data")},
	})

	size := ReceiptSize(r)
	if size == 0 {
		t.Error("receipt size should not be 0")
	}

	// A receipt with more logs should be larger.
	r2 := makeTestReceipt(LegacyTxType, ReceiptStatusSuccessful, 21000, []*Log{
		{Address: HexToAddress("0xdead"), Topics: []Hash{HexToHash("0xaaaa")}, Data: []byte("data")},
		{Address: HexToAddress("0xbeef"), Topics: []Hash{HexToHash("0xbbbb")}, Data: []byte("more data here")},
	})
	size2 := ReceiptSize(r2)
	if size2 <= size {
		t.Errorf("receipt with more logs should be larger: %d <= %d", size2, size)
	}

	// Nil receipt should return 0.
	if ReceiptSize(nil) != 0 {
		t.Error("nil receipt size should be 0")
	}
}

func TestReceiptCodecEncodeNil(t *testing.T) {
	codec := &ReceiptCodec{}

	_, err := codec.EncodeReceipt(nil)
	if err == nil {
		t.Error("expected error encoding nil receipt")
	}
}

func TestReceiptCodecDecodeEmpty(t *testing.T) {
	codec := &ReceiptCodec{}

	_, err := codec.DecodeReceipt(nil)
	if err == nil {
		t.Error("expected error decoding nil data")
	}

	_, err = codec.DecodeReceipt([]byte{})
	if err == nil {
		t.Error("expected error decoding empty data")
	}
}

func TestReceiptCodecSingleReceiptMultipleLogs(t *testing.T) {
	codec := &ReceiptCodec{}

	logs := []*Log{
		{Address: HexToAddress("0x01"), Topics: []Hash{HexToHash("0x10")}, Data: []byte{0x01}},
		{Address: HexToAddress("0x02"), Topics: []Hash{HexToHash("0x20"), HexToHash("0x21")}, Data: []byte{0x02, 0x03}},
		{Address: HexToAddress("0x03"), Topics: nil, Data: nil},
	}
	r := makeTestReceipt(AccessListTxType, ReceiptStatusSuccessful, 100000, logs)

	enc, err := codec.EncodeReceipt(r)
	if err != nil {
		t.Fatalf("EncodeReceipt: %v", err)
	}

	decoded, err := codec.DecodeReceipt(enc)
	if err != nil {
		t.Fatalf("DecodeReceipt: %v", err)
	}

	if !ReceiptEqual(r, decoded) {
		t.Error("receipt with multiple logs mismatch after roundtrip")
	}
}
