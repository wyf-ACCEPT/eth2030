package types

import (
	"math/big"
	"testing"
)

func TestLegacyTxCreation(t *testing.T) {
	to := HexToAddress("0xdead")
	inner := &LegacyTx{
		Nonce:    1,
		GasPrice: big.NewInt(20_000_000_000),
		Gas:      21000,
		To:       &to,
		Value:    big.NewInt(1_000_000_000_000_000_000),
		Data:     nil,
	}
	tx := NewTransaction(inner)
	if tx.Type() != LegacyTxType {
		t.Fatalf("expected type %d, got %d", LegacyTxType, tx.Type())
	}
	if tx.Nonce() != 1 {
		t.Fatalf("expected nonce 1, got %d", tx.Nonce())
	}
	if tx.Gas() != 21000 {
		t.Fatalf("expected gas 21000, got %d", tx.Gas())
	}
	if tx.GasPrice().Cmp(big.NewInt(20_000_000_000)) != 0 {
		t.Fatal("GasPrice mismatch")
	}
	if tx.Value().Cmp(big.NewInt(1_000_000_000_000_000_000)) != 0 {
		t.Fatal("Value mismatch")
	}
	if *tx.To() != to {
		t.Fatal("To mismatch")
	}
}

func TestLegacyTxContractCreation(t *testing.T) {
	inner := &LegacyTx{
		Nonce:    0,
		GasPrice: big.NewInt(1),
		Gas:      100000,
		To:       nil,
		Value:    big.NewInt(0),
		Data:     []byte{0x60, 0x80},
	}
	tx := NewTransaction(inner)
	if tx.To() != nil {
		t.Fatal("contract creation should have nil To")
	}
	if len(tx.Data()) != 2 {
		t.Fatal("Data mismatch")
	}
}

func TestAccessListTxCreation(t *testing.T) {
	to := HexToAddress("0xbeef")
	inner := &AccessListTx{
		ChainID:  big.NewInt(1),
		Nonce:    5,
		GasPrice: big.NewInt(10_000_000_000),
		Gas:      50000,
		To:       &to,
		Value:    big.NewInt(0),
		AccessList: AccessList{
			{
				Address:     HexToAddress("0xaaaa"),
				StorageKeys: []Hash{HexToHash("0x01")},
			},
		},
	}
	tx := NewTransaction(inner)
	if tx.Type() != AccessListTxType {
		t.Fatalf("expected type %d, got %d", AccessListTxType, tx.Type())
	}
	if tx.ChainId().Int64() != 1 {
		t.Fatal("ChainId mismatch")
	}
	if len(tx.AccessList()) != 1 {
		t.Fatal("AccessList length mismatch")
	}
	if tx.AccessList()[0].Address != HexToAddress("0xaaaa") {
		t.Fatal("AccessList address mismatch")
	}
}

func TestDynamicFeeTxCreation(t *testing.T) {
	to := HexToAddress("0xcafe")
	inner := &DynamicFeeTx{
		ChainID:   big.NewInt(1),
		Nonce:     10,
		GasTipCap: big.NewInt(2_000_000_000),
		GasFeeCap: big.NewInt(100_000_000_000),
		Gas:       21000,
		To:        &to,
		Value:     big.NewInt(0),
	}
	tx := NewTransaction(inner)
	if tx.Type() != DynamicFeeTxType {
		t.Fatalf("expected type %d, got %d", DynamicFeeTxType, tx.Type())
	}
	if tx.GasTipCap().Cmp(big.NewInt(2_000_000_000)) != 0 {
		t.Fatal("GasTipCap mismatch")
	}
	if tx.GasFeeCap().Cmp(big.NewInt(100_000_000_000)) != 0 {
		t.Fatal("GasFeeCap mismatch")
	}
}

func TestBlobTxCreation(t *testing.T) {
	inner := &BlobTx{
		ChainID:    big.NewInt(1),
		Nonce:      0,
		GasTipCap:  big.NewInt(1_000_000_000),
		GasFeeCap:  big.NewInt(50_000_000_000),
		Gas:        21000,
		To:         HexToAddress("0xblob"),
		Value:      big.NewInt(0),
		BlobFeeCap: big.NewInt(1_000_000),
		BlobHashes: []Hash{HexToHash("0x01"), HexToHash("0x02")},
	}
	tx := NewTransaction(inner)
	if tx.Type() != BlobTxType {
		t.Fatalf("expected type %d, got %d", BlobTxType, tx.Type())
	}
	// BlobTx.To is never nil.
	if tx.To() == nil {
		t.Fatal("BlobTx To should not be nil")
	}
}

func TestSetCodeTxCreation(t *testing.T) {
	inner := &SetCodeTx{
		ChainID:   big.NewInt(1),
		Nonce:     0,
		GasTipCap: big.NewInt(1_000_000_000),
		GasFeeCap: big.NewInt(50_000_000_000),
		Gas:       100000,
		To:        HexToAddress("0x7702"),
		Value:     big.NewInt(0),
		AuthorizationList: []Authorization{
			{
				ChainID: big.NewInt(1),
				Address: HexToAddress("0xdelegated"),
				Nonce:   0,
				V:       big.NewInt(0),
				R:       big.NewInt(0),
				S:       big.NewInt(0),
			},
		},
	}
	tx := NewTransaction(inner)
	if tx.Type() != SetCodeTxType {
		t.Fatalf("expected type %d, got %d", SetCodeTxType, tx.Type())
	}
	if tx.To() == nil {
		t.Fatal("SetCodeTx To should not be nil")
	}
}

func TestTxTypeConstants(t *testing.T) {
	if LegacyTxType != 0x00 {
		t.Fatal("LegacyTxType should be 0x00")
	}
	if AccessListTxType != 0x01 {
		t.Fatal("AccessListTxType should be 0x01")
	}
	if DynamicFeeTxType != 0x02 {
		t.Fatal("DynamicFeeTxType should be 0x02")
	}
	if BlobTxType != 0x03 {
		t.Fatal("BlobTxType should be 0x03")
	}
	if SetCodeTxType != 0x04 {
		t.Fatal("SetCodeTxType should be 0x04")
	}
}

func TestTransactionCopyIndependence(t *testing.T) {
	to := HexToAddress("0xdead")
	inner := &LegacyTx{
		Nonce:    1,
		GasPrice: big.NewInt(100),
		Gas:      21000,
		To:       &to,
		Value:    big.NewInt(500),
	}
	tx := NewTransaction(inner)

	// Mutate original inner data; tx should be unaffected.
	inner.Nonce = 99
	inner.GasPrice.SetInt64(999)
	inner.Value.SetInt64(999)

	if tx.Nonce() != 1 {
		t.Fatal("Transaction nonce should be independent of original")
	}
	if tx.GasPrice().Int64() != 100 {
		t.Fatal("Transaction GasPrice should be independent of original")
	}
	if tx.Value().Int64() != 500 {
		t.Fatal("Transaction Value should be independent of original")
	}
}

func TestDeriveChainID(t *testing.T) {
	tests := []struct {
		v    *big.Int
		want int64
	}{
		{big.NewInt(27), 0},
		{big.NewInt(28), 0},
		{big.NewInt(37), 1},  // chainID=1 => v = 1*2+35 = 37
		{big.NewInt(38), 1},  // chainID=1 => v = 1*2+36 = 38
		{nil, 0},
	}
	for _, tt := range tests {
		got := deriveChainID(tt.v)
		if got.Int64() != tt.want {
			t.Errorf("deriveChainID(%v) = %d, want %d", tt.v, got.Int64(), tt.want)
		}
	}
}
