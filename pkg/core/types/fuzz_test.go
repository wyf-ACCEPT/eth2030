package types

import (
	"bytes"
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/rlp"
)

// FuzzTransactionRLPRoundtrip creates transactions with fuzz-derived fields,
// RLP-encodes them, decodes back, and verifies the roundtrip.
func FuzzTransactionRLPRoundtrip(f *testing.F) {
	// Seed: valid legacy tx encoded bytes.
	legacyTx := buildLegacyTx(1, 20_000_000_000, 21000, 1_000_000, 0xca, 37, 123456, 654321)
	if enc, err := legacyTx.EncodeRLP(); err == nil {
		f.Add(enc)
	}

	// Seed: valid EIP-1559 tx encoded bytes.
	dynTx := buildDynamicFeeTx(1, 5, 1000, 2000, 50000, 100, 0xfe, 1, 111, 222)
	if enc, err := dynTx.EncodeRLP(); err == nil {
		f.Add(enc)
	}

	// Seed: valid access list tx encoded bytes.
	alTx := buildAccessListTx(1, 3, 10_000, 30000, 500, 0xab, 0, 333, 444)
	if enc, err := alTx.EncodeRLP(); err == nil {
		f.Add(enc)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) < 16 {
			return
		}

		// Use the fuzz data to construct a legacy transaction with deterministic fields.
		nonce := uint64(data[0])<<8 | uint64(data[1])
		gasPrice := new(big.Int).SetBytes(data[2:6])
		gas := uint64(data[6])<<8 | uint64(data[7])
		if gas == 0 {
			gas = 21000
		}
		value := new(big.Int).SetBytes(data[8:12])
		txData := data[12:]
		if len(txData) > 256 {
			txData = txData[:256]
		}

		addrEnd := 20 % len(data)
		if addrEnd == 0 {
			addrEnd = 1
		}
		to := BytesToAddress(data[:addrEnd])
		rEnd := 8 % len(data)
		if rEnd == 0 {
			rEnd = 1
		}
		sEnd := 4 % len(data)
		if sEnd == 0 {
			sEnd = 1
		}
		inner := &LegacyTx{
			Nonce:    nonce,
			GasPrice: gasPrice,
			Gas:      gas,
			To:       &to,
			Value:    value,
			Data:     txData,
			V:        big.NewInt(37), // chain ID 1
			R:        new(big.Int).SetBytes(data[:rEnd]),
			S:        new(big.Int).SetBytes(data[:sEnd]),
		}
		tx := NewTransaction(inner)

		enc, err := tx.EncodeRLP()
		if err != nil {
			// Encoding failure is acceptable for edge-case field values.
			return
		}

		decoded, err := DecodeTxRLP(enc)
		if err != nil {
			t.Fatalf("DecodeTxRLP failed on valid encoding: %v", err)
		}

		// Verify core fields.
		if decoded.Nonce() != tx.Nonce() {
			t.Fatalf("Nonce mismatch: got %d, want %d", decoded.Nonce(), tx.Nonce())
		}
		if decoded.Gas() != tx.Gas() {
			t.Fatalf("Gas mismatch: got %d, want %d", decoded.Gas(), tx.Gas())
		}
		if decoded.Type() != tx.Type() {
			t.Fatalf("Type mismatch: got %d, want %d", decoded.Type(), tx.Type())
		}
		if decoded.GasPrice().Cmp(tx.GasPrice()) != 0 {
			t.Fatalf("GasPrice mismatch: got %s, want %s", decoded.GasPrice(), tx.GasPrice())
		}
		if decoded.Value().Cmp(tx.Value()) != 0 {
			t.Fatalf("Value mismatch: got %s, want %s", decoded.Value(), tx.Value())
		}
		if !bytes.Equal(decoded.Data(), tx.Data()) {
			t.Fatalf("Data mismatch")
		}
	})
}

// FuzzReceiptRLPRoundtrip creates receipts with fuzz-derived fields,
// RLP-encodes them, decodes back, and verifies the roundtrip.
func FuzzReceiptRLPRoundtrip(f *testing.F) {
	// Seed: valid receipt encoding.
	r := &Receipt{
		Status:            ReceiptStatusSuccessful,
		CumulativeGasUsed: 21000,
		Bloom:             Bloom{},
		Logs: []*Log{
			{
				Address: HexToAddress("0xdead"),
				Topics:  []Hash{HexToHash("0xaaaa")},
				Data:    []byte("test"),
			},
		},
	}
	if enc, err := r.EncodeRLP(); err == nil {
		f.Add(enc)
	}

	// Seed: empty receipt.
	r2 := &Receipt{
		Status:            ReceiptStatusFailed,
		CumulativeGasUsed: 0,
		Bloom:             Bloom{},
		Logs:              []*Log{},
	}
	if enc, err := r2.EncodeRLP(); err == nil {
		f.Add(enc)
	}

	// Seed: typed receipt (EIP-1559).
	r3 := &Receipt{
		Type:              DynamicFeeTxType,
		Status:            ReceiptStatusSuccessful,
		CumulativeGasUsed: 50000,
		Bloom:             Bloom{},
		Logs:              []*Log{},
	}
	if enc, err := r3.EncodeRLP(); err == nil {
		f.Add(enc)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) < 8 {
			return
		}

		// Build a receipt from fuzz data.
		status := uint64(data[0]) % 2
		cumGas := uint64(data[1])<<16 | uint64(data[2])<<8 | uint64(data[3])
		txType := data[4] % 5 // 0..4

		var logs []*Log
		numLogs := int(data[5]) % 4
		offset := 6
		for i := 0; i < numLogs && offset+20 < len(data); i++ {
			addr := BytesToAddress(data[offset : offset+20])
			offset += 20
			logData := []byte{}
			if offset+4 <= len(data) {
				logData = data[offset : offset+4]
				offset += 4
			}
			logs = append(logs, &Log{
				Address: addr,
				Topics:  []Hash{},
				Data:    logData,
			})
		}

		receipt := &Receipt{
			Type:              txType,
			Status:            status,
			CumulativeGasUsed: cumGas,
			Bloom:             Bloom{},
			Logs:              logs,
		}

		enc, err := receipt.EncodeRLP()
		if err != nil {
			return
		}

		decoded, err := DecodeReceiptRLP(enc)
		if err != nil {
			t.Fatalf("DecodeReceiptRLP failed on valid encoding: %v", err)
		}

		if decoded.Status != receipt.Status {
			t.Fatalf("Status mismatch: got %d, want %d", decoded.Status, receipt.Status)
		}
		if decoded.CumulativeGasUsed != receipt.CumulativeGasUsed {
			t.Fatalf("CumulativeGasUsed mismatch: got %d, want %d", decoded.CumulativeGasUsed, receipt.CumulativeGasUsed)
		}
		if decoded.Type != receipt.Type {
			t.Fatalf("Type mismatch: got %d, want %d", decoded.Type, receipt.Type)
		}
		if len(decoded.Logs) != len(receipt.Logs) {
			t.Fatalf("Log count mismatch: got %d, want %d", len(decoded.Logs), len(receipt.Logs))
		}
		for i, log := range decoded.Logs {
			if log.Address != receipt.Logs[i].Address {
				t.Fatalf("Log %d address mismatch", i)
			}
			if !bytes.Equal(log.Data, receipt.Logs[i].Data) {
				t.Fatalf("Log %d data mismatch", i)
			}
		}
	})
}

// FuzzLogRLPRoundtrip encodes and decodes individual log entries.
func FuzzLogRLPRoundtrip(f *testing.F) {
	// Seed: minimal log.
	logEnc, _ := encodeLog(&Log{
		Address: HexToAddress("0xdead"),
		Topics:  []Hash{},
		Data:    []byte{},
	})
	if logEnc != nil {
		f.Add(logEnc)
	}

	// Seed: log with topics and data.
	logEnc2, _ := encodeLog(&Log{
		Address: HexToAddress("0xbeef"),
		Topics:  []Hash{HexToHash("0xaaaa"), HexToHash("0xbbbb")},
		Data:    []byte("event data"),
	})
	if logEnc2 != nil {
		f.Add(logEnc2)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) < 20 {
			return
		}

		// Build a log from fuzz data.
		addr := BytesToAddress(data[:20])
		numTopics := int(data[0]) % 5
		var topics []Hash
		offset := 20
		for i := 0; i < numTopics && offset+32 <= len(data); i++ {
			topics = append(topics, BytesToHash(data[offset:offset+32]))
			offset += 32
		}
		logData := data[offset:]
		if len(logData) > 256 {
			logData = logData[:256]
		}

		log := &Log{
			Address: addr,
			Topics:  topics,
			Data:    logData,
		}

		enc, err := encodeLog(log)
		if err != nil {
			return
		}

		// Decode via the receipt RLP stream mechanism by wrapping in a receipt.
		receipt := &Receipt{
			Status:            ReceiptStatusSuccessful,
			CumulativeGasUsed: 100,
			Bloom:             Bloom{},
			Logs:              []*Log{log},
		}
		rEnc, err := receipt.EncodeRLP()
		if err != nil {
			return
		}
		decoded, err := DecodeReceiptRLP(rEnc)
		if err != nil {
			t.Fatalf("DecodeReceiptRLP failed: %v", err)
		}
		if len(decoded.Logs) != 1 {
			t.Fatalf("Expected 1 log, got %d", len(decoded.Logs))
		}
		dLog := decoded.Logs[0]
		if dLog.Address != log.Address {
			t.Fatalf("Address mismatch")
		}
		if len(dLog.Topics) != len(log.Topics) {
			t.Fatalf("Topic count mismatch: got %d, want %d", len(dLog.Topics), len(log.Topics))
		}
		for i, topic := range dLog.Topics {
			if topic != log.Topics[i] {
				t.Fatalf("Topic %d mismatch", i)
			}
		}
		if !bytes.Equal(dLog.Data, log.Data) {
			t.Fatalf("Data mismatch")
		}

		// Also verify the raw encodeLog output is non-empty.
		if len(enc) == 0 {
			t.Fatal("encodeLog returned empty bytes")
		}
	})
}

// FuzzTransactionRLPDecode feeds random bytes to transaction RLP decoding.
// It must never panic on arbitrary input.
func FuzzTransactionRLPDecode(f *testing.F) {
	// Seed: valid legacy tx.
	legacyTx := buildLegacyTx(0, 1, 21000, 0, 0, 27, 1, 1)
	if enc, err := legacyTx.EncodeRLP(); err == nil {
		f.Add(enc)
	}

	// Seed: valid EIP-1559 tx.
	dynTx := buildDynamicFeeTx(1, 0, 100, 200, 21000, 0, 0, 0, 1, 1)
	if enc, err := dynTx.EncodeRLP(); err == nil {
		f.Add(enc)
	}

	// Seed: typed prefix byte + garbage.
	f.Add([]byte{0x01, 0xc0})
	f.Add([]byte{0x02, 0xc0})
	f.Add([]byte{0x03, 0xc0})
	f.Add([]byte{0x04, 0xc0})

	// Seed: RLP list prefix.
	f.Add([]byte{0xc0})
	f.Add([]byte{0xc1, 0x80})

	// Seed: empty.
	f.Add([]byte{})

	// Seed: random-ish.
	f.Add([]byte{0xff, 0xfe, 0xfd, 0xfc})

	f.Fuzz(func(t *testing.T, data []byte) {
		// Must not panic on any input.
		_, _ = DecodeTxRLP(data)
	})
}

// FuzzHeaderRLPRoundtrip creates headers with fuzz-derived fields,
// RLP-encodes them, decodes back, and verifies the roundtrip.
func FuzzHeaderRLPRoundtrip(f *testing.F) {
	// Seed: valid header encoding.
	h := &Header{
		ParentHash:  HexToHash("0x1111"),
		UncleHash:   EmptyUncleHash,
		Coinbase:    HexToAddress("0xaabbcc"),
		Root:        EmptyRootHash,
		TxHash:      EmptyRootHash,
		ReceiptHash: EmptyRootHash,
		Difficulty:  big.NewInt(0),
		Number:      big.NewInt(100),
		GasLimit:    30_000_000,
		GasUsed:     21_000,
		Time:        1700000000,
		Extra:       []byte("ETH2030"),
		BaseFee:     big.NewInt(1_000_000_000),
	}
	if enc, err := h.EncodeRLP(); err == nil {
		f.Add(enc)
	}

	// Seed: header with all optional fields.
	wh := HexToHash("0xaaaa")
	beaconRoot := HexToHash("0xbeac")
	reqHash := HexToHash("0x7685")
	bgu := uint64(131072)
	ebg := uint64(0)
	cgu := uint64(5000)
	ceg := uint64(1000)
	h2 := &Header{
		ParentHash:        HexToHash("0x2222"),
		UncleHash:         EmptyUncleHash,
		Coinbase:          HexToAddress("0xddee"),
		Root:              HexToHash("0x3333"),
		TxHash:            HexToHash("0x4444"),
		ReceiptHash:       HexToHash("0x5555"),
		Difficulty:        big.NewInt(0),
		Number:            big.NewInt(200),
		GasLimit:          60_000_000,
		GasUsed:           42_000,
		Time:              1700000001,
		Extra:             []byte("fuzz"),
		BaseFee:           big.NewInt(2_000_000_000),
		WithdrawalsHash:   &wh,
		BlobGasUsed:       &bgu,
		ExcessBlobGas:     &ebg,
		ParentBeaconRoot:  &beaconRoot,
		RequestsHash:      &reqHash,
		CalldataGasUsed:   &cgu,
		CalldataExcessGas: &ceg,
	}
	if enc, err := h2.EncodeRLP(); err == nil {
		f.Add(enc)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) < 32 {
			return
		}

		// Build a header from fuzz data.
		header := &Header{
			ParentHash:  BytesToHash(data[:32]),
			UncleHash:   EmptyUncleHash,
			Root:        EmptyRootHash,
			TxHash:      EmptyRootHash,
			ReceiptHash: EmptyRootHash,
			Difficulty:  big.NewInt(0),
			Number:      new(big.Int).SetUint64(uint64(data[0])<<8 | uint64(data[1])),
			GasLimit:    uint64(data[2])<<16 | uint64(data[3])<<8 | uint64(data[4]%128),
			GasUsed:     uint64(data[5])<<8 | uint64(data[6]),
			Time:        uint64(data[7])<<24 | uint64(data[8])<<16 | uint64(data[9])<<8 | uint64(data[10]),
			Extra:       data[11:min(len(data), 43)],
			BaseFee:     new(big.Int).SetUint64(uint64(data[11])<<8 | uint64(data[12])),
		}

		enc, err := header.EncodeRLP()
		if err != nil {
			return
		}

		decoded, err := DecodeHeaderRLP(enc)
		if err != nil {
			t.Fatalf("DecodeHeaderRLP failed: %v", err)
		}

		if decoded.ParentHash != header.ParentHash {
			t.Fatalf("ParentHash mismatch")
		}
		if decoded.Number.Cmp(header.Number) != 0 {
			t.Fatalf("Number mismatch: got %s, want %s", decoded.Number, header.Number)
		}
		if decoded.GasLimit != header.GasLimit {
			t.Fatalf("GasLimit mismatch: got %d, want %d", decoded.GasLimit, header.GasLimit)
		}
		if decoded.GasUsed != header.GasUsed {
			t.Fatalf("GasUsed mismatch: got %d, want %d", decoded.GasUsed, header.GasUsed)
		}
		if decoded.Time != header.Time {
			t.Fatalf("Time mismatch: got %d, want %d", decoded.Time, header.Time)
		}
		if decoded.BaseFee.Cmp(header.BaseFee) != 0 {
			t.Fatalf("BaseFee mismatch: got %s, want %s", decoded.BaseFee, header.BaseFee)
		}
	})
}

// --- Helper functions to build valid seed transactions ---

func buildLegacyTx(nonce, gasPrice, gas, value uint64, dataByte byte, v, r, s int64) *Transaction {
	to := HexToAddress("0xdead")
	inner := &LegacyTx{
		Nonce:    nonce,
		GasPrice: big.NewInt(int64(gasPrice)),
		Gas:      gas,
		To:       &to,
		Value:    big.NewInt(int64(value)),
		Data:     []byte{dataByte},
		V:        big.NewInt(v),
		R:        big.NewInt(r),
		S:        big.NewInt(s),
	}
	return NewTransaction(inner)
}

func buildDynamicFeeTx(chainID, nonce uint64, tipCap, feeCap, gas, value uint64, dataByte byte, v, r, s int64) *Transaction {
	to := HexToAddress("0xbeef")
	inner := &DynamicFeeTx{
		ChainID:   big.NewInt(int64(chainID)),
		Nonce:     nonce,
		GasTipCap: big.NewInt(int64(tipCap)),
		GasFeeCap: big.NewInt(int64(feeCap)),
		Gas:       gas,
		To:        &to,
		Value:     big.NewInt(int64(value)),
		Data:      []byte{dataByte},
		V:         big.NewInt(v),
		R:         big.NewInt(r),
		S:         big.NewInt(s),
	}
	return NewTransaction(inner)
}

func buildAccessListTx(chainID, nonce, gasPrice, gas, value uint64, dataByte byte, v, r, s int64) *Transaction {
	to := HexToAddress("0xcafe")
	inner := &AccessListTx{
		ChainID:  big.NewInt(int64(chainID)),
		Nonce:    nonce,
		GasPrice: big.NewInt(int64(gasPrice)),
		Gas:      gas,
		To:       &to,
		Value:    big.NewInt(int64(value)),
		Data:     []byte{dataByte},
		AccessList: AccessList{
			{
				Address:     HexToAddress("0xaaaa"),
				StorageKeys: []Hash{HexToHash("0x01")},
			},
		},
		V: big.NewInt(v),
		R: big.NewInt(r),
		S: big.NewInt(s),
	}
	return NewTransaction(inner)
}

// Ensure rlp package is used (the import is needed for seed encoding).
var _ = rlp.EncodeToBytes
