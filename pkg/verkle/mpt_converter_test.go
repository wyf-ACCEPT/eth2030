package verkle

import (
	"encoding/binary"
	"math/big"
	"testing"
)

// --- ConverterConfig tests ---

func TestDefaultConverterConfig(t *testing.T) {
	cfg := DefaultConverterConfig()
	if cfg.BatchSize != 1000 {
		t.Errorf("BatchSize = %d, want 1000", cfg.BatchSize)
	}
	if cfg.MaxPendingKeys != 10000 {
		t.Errorf("MaxPendingKeys = %d, want 10000", cfg.MaxPendingKeys)
	}
	if cfg.ProgressCallback != nil {
		t.Error("ProgressCallback should be nil by default")
	}
}

// --- MPTConverter creation tests ---

func TestNewMPTConverter(t *testing.T) {
	cfg := DefaultConverterConfig()
	c := NewMPTConverter(cfg, nil)

	if c.GetVerkleRoot() == nil {
		t.Fatal("GetVerkleRoot() returned nil")
	}

	converted, remaining := c.ComputeProgress()
	if converted != 0 || remaining != 0 {
		t.Errorf("initial progress: converted=%d, remaining=%d", converted, remaining)
	}
}

func TestNewMPTConverterWithConfig(t *testing.T) {
	cfg := DefaultConverterConfig()
	pedConfig := NewPedersenConfig(16)
	c := NewMPTConverterWithConfig(cfg, nil, pedConfig)
	if c.GetVerkleRoot() == nil {
		t.Fatal("GetVerkleRoot() returned nil")
	}
}

func TestNewMPTConverterWithNilPedersenConfig(t *testing.T) {
	cfg := DefaultConverterConfig()
	c := NewMPTConverterWithConfig(cfg, nil, nil)
	if c.GetVerkleRoot() == nil {
		t.Fatal("GetVerkleRoot() returned nil with nil PedersenConfig")
	}
}

// --- ConvertAccount tests ---

func TestConvertAccount_Basic(t *testing.T) {
	cfg := DefaultConverterConfig()
	c := NewMPTConverter(cfg, nil)

	var addr [20]byte
	addr[19] = 0x01
	balance := big.NewInt(1_000_000).Bytes()
	var codeHash [32]byte
	codeHash[0] = 0xAB
	var storageRoot [32]byte

	err := c.ConvertAccount(addr, 42, balance, codeHash, storageRoot)
	if err != nil {
		t.Fatalf("ConvertAccount: %v", err)
	}

	converted, _ := c.ComputeProgress()
	if converted != 4 {
		t.Errorf("converted = %d, want 4 (version+balance+nonce+codeHash)", converted)
	}
}

func TestConvertAccount_ZeroBalance(t *testing.T) {
	cfg := DefaultConverterConfig()
	c := NewMPTConverter(cfg, nil)

	var addr [20]byte
	addr[0] = 0xFF
	var codeHash, storageRoot [32]byte

	err := c.ConvertAccount(addr, 0, nil, codeHash, storageRoot)
	if err != nil {
		t.Fatalf("ConvertAccount: %v", err)
	}

	converted, _ := c.ComputeProgress()
	if converted != 4 {
		t.Errorf("converted = %d, want 4", converted)
	}
}

func TestConvertAccount_LargeBalance(t *testing.T) {
	cfg := DefaultConverterConfig()
	c := NewMPTConverter(cfg, nil)

	var addr [20]byte
	addr[0] = 0x01
	// 10 ether in wei = 10^19
	balance := new(big.Int).Mul(big.NewInt(10), new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil))
	var codeHash, storageRoot [32]byte

	err := c.ConvertAccount(addr, 100, balance.Bytes(), codeHash, storageRoot)
	if err != nil {
		t.Fatalf("ConvertAccount: %v", err)
	}
}

// --- ConvertStorageSlot tests ---

func TestConvertStorageSlot_Basic(t *testing.T) {
	cfg := DefaultConverterConfig()
	c := NewMPTConverter(cfg, nil)

	var addr [20]byte
	addr[0] = 0x01

	var slot, value [32]byte
	slot[31] = 0x05
	value[31] = 0xAA

	err := c.ConvertStorageSlot(addr, slot, value)
	if err != nil {
		t.Fatalf("ConvertStorageSlot: %v", err)
	}

	converted, _ := c.ComputeProgress()
	if converted != 1 {
		t.Errorf("converted = %d, want 1", converted)
	}
}

func TestConvertStorageSlot_Multiple(t *testing.T) {
	cfg := DefaultConverterConfig()
	c := NewMPTConverter(cfg, nil)

	var addr [20]byte
	addr[0] = 0x01

	for i := 0; i < 10; i++ {
		var slot, value [32]byte
		binary.BigEndian.PutUint64(slot[24:], uint64(i))
		value[31] = byte(i + 1)

		err := c.ConvertStorageSlot(addr, slot, value)
		if err != nil {
			t.Fatalf("ConvertStorageSlot(%d): %v", i, err)
		}
	}

	converted, _ := c.ComputeProgress()
	if converted != 10 {
		t.Errorf("converted = %d, want 10", converted)
	}
}

// --- BatchConvert tests ---

func TestBatchConvert_Basic(t *testing.T) {
	cfg := DefaultConverterConfig()
	c := NewMPTConverter(cfg, nil)

	keys := make([][]byte, 5)
	values := make([][]byte, 5)
	for i := 0; i < 5; i++ {
		keys[i] = make([]byte, KeySize)
		keys[i][0] = byte(i + 1)
		values[i] = make([]byte, ValueSize)
		values[i][31] = byte(i + 10)
	}

	n, err := c.BatchConvert(keys, values)
	if err != nil {
		t.Fatalf("BatchConvert: %v", err)
	}
	if n != 5 {
		t.Errorf("BatchConvert returned %d, want 5", n)
	}

	converted, _ := c.ComputeProgress()
	if converted != 5 {
		t.Errorf("converted = %d, want 5", converted)
	}
}

func TestBatchConvert_LengthMismatch(t *testing.T) {
	cfg := DefaultConverterConfig()
	c := NewMPTConverter(cfg, nil)

	keys := make([][]byte, 3)
	values := make([][]byte, 5)
	_, err := c.BatchConvert(keys, values)
	if err == nil {
		t.Error("BatchConvert should fail with mismatched lengths")
	}
}

func TestBatchConvert_InvalidKeySize(t *testing.T) {
	cfg := DefaultConverterConfig()
	c := NewMPTConverter(cfg, nil)

	keys := [][]byte{
		make([]byte, KeySize),
		{1, 2, 3}, // invalid
		make([]byte, KeySize),
	}
	values := [][]byte{
		make([]byte, ValueSize),
		make([]byte, ValueSize),
		make([]byte, ValueSize),
	}

	n, err := c.BatchConvert(keys, values)
	if err != nil {
		t.Fatalf("BatchConvert: %v", err)
	}
	// 2 valid + 1 invalid = 2 converted.
	if n != 2 {
		t.Errorf("BatchConvert returned %d, want 2 (one invalid skipped)", n)
	}
	if c.ErrorCount() != 1 {
		t.Errorf("ErrorCount = %d, want 1", c.ErrorCount())
	}
}

func TestBatchConvert_WithProgressCallback(t *testing.T) {
	callbackCalled := false
	var lastProcessed, lastTotal int

	cfg := ConverterConfig{
		BatchSize:      100,
		MaxPendingKeys: 100,
		ProgressCallback: func(processed, total int) {
			callbackCalled = true
			lastProcessed = processed
			lastTotal = total
		},
	}
	c := NewMPTConverter(cfg, nil)
	c.SetRemaining(10)

	keys := make([][]byte, 3)
	values := make([][]byte, 3)
	for i := 0; i < 3; i++ {
		keys[i] = make([]byte, KeySize)
		keys[i][0] = byte(i + 1)
		values[i] = make([]byte, ValueSize)
	}

	c.BatchConvert(keys, values)

	if !callbackCalled {
		t.Error("progress callback was not called")
	}
	if lastProcessed != 3 {
		t.Errorf("callback processed = %d, want 3", lastProcessed)
	}
	if lastTotal != 13 {
		t.Errorf("callback total = %d, want 13 (3+10)", lastTotal)
	}
}

func TestBatchConvert_BatchSizeLimit(t *testing.T) {
	cfg := ConverterConfig{
		BatchSize:      3,
		MaxPendingKeys: 100,
	}
	c := NewMPTConverter(cfg, nil)

	keys := make([][]byte, 10)
	values := make([][]byte, 10)
	for i := 0; i < 10; i++ {
		keys[i] = make([]byte, KeySize)
		keys[i][0] = byte(i + 1)
		values[i] = make([]byte, ValueSize)
	}

	n, err := c.BatchConvert(keys, values)
	if err != nil {
		t.Fatalf("BatchConvert: %v", err)
	}
	if n != 3 {
		t.Errorf("BatchConvert returned %d, want 3 (batch size limit)", n)
	}
}

// --- Finalize tests ---

func TestFinalize_EmptyTree(t *testing.T) {
	cfg := DefaultConverterConfig()
	c := NewMPTConverter(cfg, nil)

	root, err := c.Finalize()
	if err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	// Empty tree commitment should be zero (identity point).
	var zero [32]byte
	if root != zero {
		t.Error("Finalize of empty tree should return zero commitment")
	}
}

func TestFinalize_AfterInserts(t *testing.T) {
	cfg := DefaultConverterConfig()
	c := NewMPTConverter(cfg, nil)

	var addr [20]byte
	addr[0] = 0x01
	c.ConvertAccount(addr, 1, big.NewInt(100).Bytes(), [32]byte{}, [32]byte{})

	root, err := c.Finalize()
	if err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	var zero [32]byte
	if root == zero {
		t.Error("Finalize after inserts should produce non-zero commitment")
	}
}

func TestFinalize_Deterministic(t *testing.T) {
	cfg := DefaultConverterConfig()

	c1 := NewMPTConverter(cfg, nil)
	c2 := NewMPTConverter(cfg, nil)

	var addr [20]byte
	addr[0] = 0x42
	balance := big.NewInt(999).Bytes()

	c1.ConvertAccount(addr, 5, balance, [32]byte{}, [32]byte{})
	c2.ConvertAccount(addr, 5, balance, [32]byte{}, [32]byte{})

	root1, _ := c1.Finalize()
	root2, _ := c2.Finalize()

	if root1 != root2 {
		t.Error("same inputs should produce same root commitment")
	}
}

// --- Pending buffer tests ---

func TestAddPending_AndFlush(t *testing.T) {
	cfg := ConverterConfig{
		BatchSize:      100,
		MaxPendingKeys: 5,
	}
	c := NewMPTConverter(cfg, nil)

	for i := 0; i < 3; i++ {
		key := make([]byte, KeySize)
		key[0] = byte(i + 1)
		val := make([]byte, ValueSize)
		val[31] = byte(i)
		c.AddPending(key, val)
	}

	// Not yet flushed (under MaxPendingKeys).
	converted, _ := c.ComputeProgress()
	if converted != 0 {
		t.Errorf("converted before flush = %d, want 0", converted)
	}

	c.FlushPending()

	converted, _ = c.ComputeProgress()
	if converted != 3 {
		t.Errorf("converted after flush = %d, want 3", converted)
	}
}

func TestAddPending_AutoFlush(t *testing.T) {
	cfg := ConverterConfig{
		BatchSize:      100,
		MaxPendingKeys: 3,
	}
	c := NewMPTConverter(cfg, nil)

	for i := 0; i < 3; i++ {
		key := make([]byte, KeySize)
		key[0] = byte(i + 1)
		val := make([]byte, ValueSize)
		c.AddPending(key, val)
	}

	// Should have auto-flushed at MaxPendingKeys.
	converted, _ := c.ComputeProgress()
	if converted != 3 {
		t.Errorf("converted after auto-flush = %d, want 3", converted)
	}
}

// --- Stats tests ---

func TestStats(t *testing.T) {
	cfg := DefaultConverterConfig()
	c := NewMPTConverter(cfg, nil)
	c.SetRemaining(100)

	var addr [20]byte
	addr[0] = 0x01
	c.ConvertAccount(addr, 0, nil, [32]byte{}, [32]byte{})

	stats := c.Stats()
	if stats.Converted != 4 {
		t.Errorf("Converted = %d, want 4", stats.Converted)
	}
	if stats.Remaining != 100 {
		t.Errorf("Remaining = %d, want 100", stats.Remaining)
	}
	if stats.Errors != 0 {
		t.Errorf("Errors = %d, want 0", stats.Errors)
	}
}

// --- ConvertAccountFromSource tests ---

func TestConvertAccountFromSource_NoReader(t *testing.T) {
	cfg := DefaultConverterConfig()
	c := NewMPTConverter(cfg, nil)

	var addr [20]byte
	err := c.ConvertAccountFromSource(addr)
	if err == nil {
		t.Error("ConvertAccountFromSource without reader should fail")
	}
}

func TestConvertAccountFromSource_WithReader(t *testing.T) {
	// Create a mock source reader that returns predetermined values.
	reader := func(key []byte) ([]byte, error) {
		val := make([]byte, ValueSize)
		val[31] = 0x42
		return val, nil
	}

	cfg := DefaultConverterConfig()
	c := NewMPTConverter(cfg, reader)

	var addr [20]byte
	addr[0] = 0x01
	err := c.ConvertAccountFromSource(addr)
	if err != nil {
		t.Fatalf("ConvertAccountFromSource: %v", err)
	}

	// Should have converted the account fields.
	converted, _ := c.ComputeProgress()
	if converted != 4 {
		t.Errorf("converted = %d, want 4", converted)
	}
}

// --- Integration: account + storage -> finalize ---

func TestIntegration_AccountAndStorage(t *testing.T) {
	cfg := DefaultConverterConfig()
	c := NewMPTConverter(cfg, nil)

	var addr [20]byte
	addr[0] = 0x01
	balance := big.NewInt(1_000_000).Bytes()
	c.ConvertAccount(addr, 10, balance, [32]byte{0xAB}, [32]byte{})

	// Add some storage slots.
	for i := 0; i < 5; i++ {
		var slot, value [32]byte
		binary.BigEndian.PutUint64(slot[24:], uint64(i))
		value[31] = byte(i + 1)
		c.ConvertStorageSlot(addr, slot, value)
	}

	root, err := c.Finalize()
	if err != nil {
		t.Fatalf("Finalize: %v", err)
	}

	var zero [32]byte
	if root == zero {
		t.Error("finalized tree should have non-zero root")
	}

	stats := c.Stats()
	if stats.Converted != 9 { // 4 account fields + 5 storage slots
		t.Errorf("total converted = %d, want 9", stats.Converted)
	}
}
