package types

import (
	"math/big"
	"testing"
)

func TestReceiptBuilderNewEmpty(t *testing.T) {
	rb := NewReceiptBuilder()
	r := rb.Build()

	if r.Status != 0 {
		t.Errorf("empty builder Status = %d, want 0", r.Status)
	}
	if r.GasUsed != 0 {
		t.Errorf("empty builder GasUsed = %d, want 0", r.GasUsed)
	}
	if r.CumulativeGasUsed != 0 {
		t.Errorf("empty builder CumulativeGasUsed = %d, want 0", r.CumulativeGasUsed)
	}
	if len(r.Logs) != 0 {
		t.Errorf("empty builder Logs len = %d, want 0", len(r.Logs))
	}
	if r.BlockNumber != nil {
		t.Errorf("empty builder BlockNumber = %v, want nil", r.BlockNumber)
	}
	if r.Bloom != (Bloom{}) {
		t.Error("empty builder should have zero bloom")
	}
}

func TestReceiptBuilderSetStatus(t *testing.T) {
	r := NewReceiptBuilder().SetStatus(ReceiptStatusSuccessful).Build()
	if r.Status != ReceiptStatusSuccessful {
		t.Errorf("Status = %d, want %d", r.Status, ReceiptStatusSuccessful)
	}

	r = NewReceiptBuilder().SetStatus(ReceiptStatusFailed).Build()
	if r.Status != ReceiptStatusFailed {
		t.Errorf("Status = %d, want %d", r.Status, ReceiptStatusFailed)
	}
}

func TestReceiptBuilderSetGasUsed(t *testing.T) {
	r := NewReceiptBuilder().SetGasUsed(21000).Build()
	if r.GasUsed != 21000 {
		t.Errorf("GasUsed = %d, want 21000", r.GasUsed)
	}
}

func TestReceiptBuilderSetCumulativeGas(t *testing.T) {
	r := NewReceiptBuilder().SetCumulativeGasUsed(63000).Build()
	if r.CumulativeGasUsed != 63000 {
		t.Errorf("CumulativeGasUsed = %d, want 63000", r.CumulativeGasUsed)
	}
}

func TestReceiptBuilderAddLog(t *testing.T) {
	log1 := &Log{
		Address: HexToAddress("0xaabb"),
		Topics:  []Hash{HexToHash("0x1111")},
		Data:    []byte{0x01},
	}
	log2 := &Log{
		Address: HexToAddress("0xccdd"),
		Topics:  []Hash{HexToHash("0x2222")},
		Data:    []byte{0x02},
	}

	r := NewReceiptBuilder().AddLog(log1).AddLog(log2).Build()

	if len(r.Logs) != 2 {
		t.Fatalf("Logs len = %d, want 2", len(r.Logs))
	}
	if r.Logs[0].Address != log1.Address {
		t.Errorf("Logs[0].Address mismatch")
	}
	if r.Logs[1].Address != log2.Address {
		t.Errorf("Logs[1].Address mismatch")
	}
}

func TestReceiptBuilderAddNilLog(t *testing.T) {
	r := NewReceiptBuilder().AddLog(nil).Build()
	if len(r.Logs) != 0 {
		t.Errorf("adding nil log should be ignored, got %d logs", len(r.Logs))
	}
}

func TestReceiptBuilderSetContractAddress(t *testing.T) {
	addr := HexToAddress("0xdeadbeefdeadbeef")
	r := NewReceiptBuilder().SetContractAddress(addr).Build()
	if r.ContractAddress != addr {
		t.Errorf("ContractAddress = %v, want %v", r.ContractAddress, addr)
	}
}

func TestReceiptBuilderSetTxHash(t *testing.T) {
	h := HexToHash("0xabcdef1234567890")
	r := NewReceiptBuilder().SetTxHash(h).Build()
	if r.TxHash != h {
		t.Errorf("TxHash = %v, want %v", r.TxHash, h)
	}
}

func TestReceiptBuilderSetBlockHash(t *testing.T) {
	h := HexToHash("0xblockhash999")
	r := NewReceiptBuilder().SetBlockHash(h).Build()
	if r.BlockHash != h {
		t.Errorf("BlockHash = %v, want %v", r.BlockHash, h)
	}
}

func TestReceiptBuilderSetBlockNumber(t *testing.T) {
	r := NewReceiptBuilder().SetBlockNumber(12345).Build()
	if r.BlockNumber == nil || r.BlockNumber.Uint64() != 12345 {
		t.Errorf("BlockNumber = %v, want 12345", r.BlockNumber)
	}
}

func TestReceiptBuilderSetTransactionIndex(t *testing.T) {
	r := NewReceiptBuilder().SetTransactionIndex(7).Build()
	if r.TransactionIndex != 7 {
		t.Errorf("TransactionIndex = %d, want 7", r.TransactionIndex)
	}
}

func TestReceiptBuilderBloomComputed(t *testing.T) {
	addr := HexToAddress("0xc0ffee")
	topic := HexToHash("0xdeadbeef")

	log := &Log{
		Address: addr,
		Topics:  []Hash{topic},
		Data:    []byte{0xff},
	}

	r := NewReceiptBuilder().
		SetStatus(ReceiptStatusSuccessful).
		AddLog(log).
		Build()

	// Bloom should contain the address and topic.
	if !BloomContains(r.Bloom, addr.Bytes()) {
		t.Error("bloom should contain the log address")
	}
	if !BloomContains(r.Bloom, topic.Bytes()) {
		t.Error("bloom should contain the log topic")
	}
}

func TestReceiptBuilderBloomEmpty(t *testing.T) {
	r := NewReceiptBuilder().SetStatus(ReceiptStatusSuccessful).Build()
	if r.Bloom != (Bloom{}) {
		t.Error("bloom should be zero when there are no logs")
	}
}

func TestReceiptBuilderBloomMultipleLogs(t *testing.T) {
	addr1 := HexToAddress("0xaaaa")
	addr2 := HexToAddress("0xbbbb")
	topic1 := HexToHash("0x1111")
	topic2 := HexToHash("0x2222")

	r := NewReceiptBuilder().
		AddLog(&Log{Address: addr1, Topics: []Hash{topic1}}).
		AddLog(&Log{Address: addr2, Topics: []Hash{topic2}}).
		Build()

	if !BloomContains(r.Bloom, addr1.Bytes()) {
		t.Error("bloom should contain addr1")
	}
	if !BloomContains(r.Bloom, addr2.Bytes()) {
		t.Error("bloom should contain addr2")
	}
	if !BloomContains(r.Bloom, topic1.Bytes()) {
		t.Error("bloom should contain topic1")
	}
	if !BloomContains(r.Bloom, topic2.Bytes()) {
		t.Error("bloom should contain topic2")
	}
}

func TestReceiptBuilderFullChain(t *testing.T) {
	// Build a full receipt with all fields set, simulating post-execution.
	txHash := HexToHash("0xaaa")
	blockHash := HexToHash("0xbbb")
	contractAddr := HexToAddress("0xccc")
	logAddr := HexToAddress("0xddd")
	topic := HexToHash("0xeee")

	r := NewReceiptBuilder().
		SetStatus(ReceiptStatusSuccessful).
		SetGasUsed(50000).
		SetCumulativeGasUsed(150000).
		SetTxHash(txHash).
		SetBlockHash(blockHash).
		SetBlockNumber(100).
		SetTransactionIndex(3).
		SetContractAddress(contractAddr).
		SetType(2).
		SetEffectiveGasPrice(big.NewInt(1_000_000_000)).
		AddLog(&Log{Address: logAddr, Topics: []Hash{topic}}).
		Build()

	if r.Status != ReceiptStatusSuccessful {
		t.Errorf("Status = %d", r.Status)
	}
	if r.GasUsed != 50000 {
		t.Errorf("GasUsed = %d", r.GasUsed)
	}
	if r.CumulativeGasUsed != 150000 {
		t.Errorf("CumulativeGasUsed = %d", r.CumulativeGasUsed)
	}
	if r.TxHash != txHash {
		t.Errorf("TxHash mismatch")
	}
	if r.BlockHash != blockHash {
		t.Errorf("BlockHash mismatch")
	}
	if r.BlockNumber == nil || r.BlockNumber.Uint64() != 100 {
		t.Errorf("BlockNumber = %v", r.BlockNumber)
	}
	if r.TransactionIndex != 3 {
		t.Errorf("TransactionIndex = %d", r.TransactionIndex)
	}
	if r.ContractAddress != contractAddr {
		t.Errorf("ContractAddress mismatch")
	}
	if r.Type != 2 {
		t.Errorf("Type = %d", r.Type)
	}
	if r.EffectiveGasPrice == nil || r.EffectiveGasPrice.Int64() != 1_000_000_000 {
		t.Errorf("EffectiveGasPrice = %v", r.EffectiveGasPrice)
	}
	if !BloomContains(r.Bloom, logAddr.Bytes()) {
		t.Error("bloom should contain log address")
	}
	if !BloomContains(r.Bloom, topic.Bytes()) {
		t.Error("bloom should contain log topic")
	}
	if !r.Succeeded() {
		t.Error("receipt should indicate success")
	}
}

func TestReceiptBuilderBlobFields(t *testing.T) {
	r := NewReceiptBuilder().
		SetBlobGasUsed(131072).
		SetBlobGasPrice(big.NewInt(42)).
		Build()

	if r.BlobGasUsed != 131072 {
		t.Errorf("BlobGasUsed = %d, want 131072", r.BlobGasUsed)
	}
	if r.BlobGasPrice == nil || r.BlobGasPrice.Int64() != 42 {
		t.Errorf("BlobGasPrice = %v, want 42", r.BlobGasPrice)
	}
}

func TestReceiptBuilderChaining(t *testing.T) {
	// Verify fluent chaining returns the same builder instance.
	rb := NewReceiptBuilder()
	rb2 := rb.SetStatus(1).SetGasUsed(21000).SetBlockNumber(5)

	if rb != rb2 {
		t.Error("chained calls should return the same builder pointer")
	}
}

func TestComputeReceiptBloomMatchesLogsBloom(t *testing.T) {
	addr := HexToAddress("0x1234567890abcdef")
	topic := HexToHash("0xfedcba0987654321")
	logs := []*Log{
		{Address: addr, Topics: []Hash{topic}, Data: []byte{0xaa}},
	}

	bloom1 := ComputeReceiptBloom(logs)
	bloom2 := LogsBloom(logs)

	if bloom1 != bloom2 {
		t.Error("ComputeReceiptBloom should match LogsBloom")
	}
}

func TestComputeReceiptBloomEmptyLogs(t *testing.T) {
	bloom := ComputeReceiptBloom(nil)
	if bloom != (Bloom{}) {
		t.Error("bloom of nil logs should be zero")
	}

	bloom = ComputeReceiptBloom([]*Log{})
	if bloom != (Bloom{}) {
		t.Error("bloom of empty logs should be zero")
	}
}

func TestReceiptBuilderSetType(t *testing.T) {
	r := NewReceiptBuilder().SetType(3).Build()
	if r.Type != 3 {
		t.Errorf("Type = %d, want 3", r.Type)
	}
}
