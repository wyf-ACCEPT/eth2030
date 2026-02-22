package vm

import (
	"sync"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

func TestNewGigagasExecutor(t *testing.T) {
	ge := NewGigagasExecutor(GigagasConfig{})
	if ge.config.MaxBatchSize != 10_000 || ge.config.MaxWorkers != 8 {
		t.Fatalf("defaults: batch=%d workers=%d", ge.config.MaxBatchSize, ge.config.MaxWorkers)
	}
	if ge.config.TargetGasPerSecond != DefaultTargetGasPerSecond {
		t.Fatalf("expected target=%d, got %d", DefaultTargetGasPerSecond, ge.config.TargetGasPerSecond)
	}
	ge2 := NewGigagasExecutor(GigagasConfig{MaxBatchSize: 500, MaxWorkers: 4})
	if ge2.config.MaxBatchSize != 500 || ge2.config.MaxWorkers != 4 {
		t.Fatalf("custom: batch=%d workers=%d", ge2.config.MaxBatchSize, ge2.config.MaxWorkers)
	}
}

func TestDefaultGigagasConfig(t *testing.T) {
	cfg := DefaultGigagasConfig()
	if cfg.TargetGasPerSecond != 1_000_000_000 || cfg.MaxBatchSize == 0 || cfg.MaxWorkers == 0 {
		t.Fatalf("bad defaults: target=%d batch=%d workers=%d",
			cfg.TargetGasPerSecond, cfg.MaxBatchSize, cfg.MaxWorkers)
	}
}

func TestPrevalidateBatch(t *testing.T) {
	ge := NewGigagasExecutor(DefaultGigagasConfig())

	txs := []*GigagasTx{
		{From: types.HexToAddress("0x01"), To: types.HexToAddress("0x02"), GasLimit: 21000},
		nil,
		{From: types.HexToAddress("0x01"), To: types.HexToAddress("0x02"), GasLimit: 0},
		{From: types.HexToAddress("0x03"), To: types.HexToAddress("0x04"), GasLimit: 50000, Data: []byte{0x01}},
	}

	errs := ge.PrevalidateBatch(txs)
	if len(errs) != 4 {
		t.Fatalf("expected 4 errors, got %d", len(errs))
	}
	if errs[0] != nil {
		t.Fatalf("tx[0] should be valid, got %v", errs[0])
	}
	if errs[1] != ErrNilTransaction {
		t.Fatalf("tx[1] should be ErrNilTransaction, got %v", errs[1])
	}
	if errs[2] != ErrGasLimitZero {
		t.Fatalf("tx[2] should be ErrGasLimitZero, got %v", errs[2])
	}
	if errs[3] != nil {
		t.Fatalf("tx[3] should be valid, got %v", errs[3])
	}
}

func TestExecuteBatch_Empty(t *testing.T) {
	ge := NewGigagasExecutor(DefaultGigagasConfig())
	_, err := ge.ExecuteBatch(nil)
	if err != ErrBatchEmpty {
		t.Fatalf("expected ErrBatchEmpty, got %v", err)
	}
	_, err = ge.ExecuteBatch([]*GigagasTx{})
	if err != ErrBatchEmpty {
		t.Fatalf("expected ErrBatchEmpty, got %v", err)
	}
}

func TestExecuteBatch_TooLarge(t *testing.T) {
	ge := NewGigagasExecutor(GigagasConfig{MaxBatchSize: 2})
	txs := make([]*GigagasTx, 3)
	for i := range txs {
		txs[i] = &GigagasTx{GasLimit: 21000}
	}
	_, err := ge.ExecuteBatch(txs)
	if err == nil {
		t.Fatal("expected error for oversized batch")
	}
}

func TestExecuteBatch_SimpleTransfer(t *testing.T) {
	ge := NewGigagasExecutor(DefaultGigagasConfig())

	from := types.HexToAddress("0xaaaa")
	to := types.HexToAddress("0xbbbb")
	txs := []*GigagasTx{
		{From: from, To: to, GasLimit: 21000, Value: 100, Nonce: 0},
	}

	results, err := ge.ExecuteBatch(txs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	r := results[0]
	if !r.Success {
		t.Fatal("expected success for simple transfer")
	}
	if r.GasUsed != 21000 {
		t.Fatalf("expected 21000 gas, got %d", r.GasUsed)
	}
	if r.TxHash.IsZero() {
		t.Fatal("tx hash should not be zero")
	}
	if len(r.Logs) != 0 {
		t.Fatal("simple transfer should produce no logs")
	}
}

func TestExecuteBatch_WithCalldata(t *testing.T) {
	ge := NewGigagasExecutor(DefaultGigagasConfig())

	// 4 bytes selector + 32 bytes arg = 36 bytes, all non-zero.
	data := make([]byte, 36)
	for i := range data {
		data[i] = byte(i + 1)
	}

	txs := []*GigagasTx{
		{
			From:     types.HexToAddress("0x01"),
			To:       types.HexToAddress("0x02"),
			GasLimit: 100_000,
			Data:     data,
		},
	}

	results, err := ge.ExecuteBatch(txs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	r := results[0]
	if !r.Success {
		t.Fatal("expected success")
	}
	// Intrinsic: 21000 + 36*16 = 21576. Exec: (100000-21576)/2 = 39212. Total: 60788.
	expectedIntrinsic := uint64(21000 + 36*16)
	expectedExec := (uint64(100_000) - expectedIntrinsic) / 2
	expectedGas := expectedIntrinsic + expectedExec
	if r.GasUsed != expectedGas {
		t.Fatalf("expected gas %d, got %d", expectedGas, r.GasUsed)
	}
	// Should have a log from the 4-byte selector.
	if len(r.Logs) != 1 {
		t.Fatalf("expected 1 log, got %d", len(r.Logs))
	}
	if r.Logs[0].Address != types.HexToAddress("0x02") {
		t.Fatal("log address should match To")
	}
	if len(r.Logs[0].Topics) != 1 {
		t.Fatal("log should have 1 topic")
	}
}

func TestExecuteBatch_InsufficientGas(t *testing.T) {
	ge := NewGigagasExecutor(DefaultGigagasConfig())

	// Non-zero data means intrinsic > 21000.
	data := []byte{0xff, 0xff}
	txs := []*GigagasTx{
		{
			From:     types.HexToAddress("0x01"),
			To:       types.HexToAddress("0x02"),
			GasLimit: 21000, // too low for calldata
			Data:     data,
		},
	}

	results, err := ge.ExecuteBatch(txs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if results[0].Success {
		t.Fatal("expected failure due to insufficient gas for calldata")
	}
	if results[0].GasUsed != 21000 {
		t.Fatalf("on failure, should consume full gas limit, got %d", results[0].GasUsed)
	}
}

func TestExecuteBatch_NilTx(t *testing.T) {
	ge := NewGigagasExecutor(DefaultGigagasConfig())
	txs := []*GigagasTx{nil}
	results, err := ge.ExecuteBatch(txs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if results[0].Success {
		t.Fatal("nil tx should not succeed")
	}
}

func TestExecuteBatch_ZeroByteCalldata(t *testing.T) {
	ge := NewGigagasExecutor(DefaultGigagasConfig())

	// 4 zero bytes + 4 non-zero bytes.
	data := []byte{0, 0, 0, 0, 1, 2, 3, 4}
	txs := []*GigagasTx{
		{From: types.HexToAddress("0x01"), To: types.HexToAddress("0x02"), GasLimit: 100_000, Data: data},
	}

	results, err := ge.ExecuteBatch(txs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Intrinsic: 21000 + 4*4 + 4*16 = 21000 + 16 + 64 = 21080.
	expectedIntrinsic := uint64(21000 + 4*4 + 4*16)
	expectedExec := (uint64(100_000) - expectedIntrinsic) / 2
	expectedGas := expectedIntrinsic + expectedExec
	if results[0].GasUsed != expectedGas {
		t.Fatalf("expected %d gas, got %d", expectedGas, results[0].GasUsed)
	}
}

func TestExecuteBatch_MultipleTxs(t *testing.T) {
	ge := NewGigagasExecutor(DefaultGigagasConfig())

	txs := make([]*GigagasTx, 100)
	for i := range txs {
		txs[i] = &GigagasTx{
			From:     types.HexToAddress("0x01"),
			To:       types.HexToAddress("0x02"),
			GasLimit: 21000,
			Nonce:    uint64(i),
		}
	}

	results, err := ge.ExecuteBatch(txs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 100 {
		t.Fatalf("expected 100 results, got %d", len(results))
	}
	for i, r := range results {
		if !r.Success {
			t.Fatalf("tx %d should succeed", i)
		}
	}

	// Each tx should have a unique hash.
	hashes := make(map[types.Hash]bool)
	for _, r := range results {
		if hashes[r.TxHash] {
			t.Fatal("duplicate tx hash detected")
		}
		hashes[r.TxHash] = true
	}
}

func TestParallelExecute_Basic(t *testing.T) {
	ge := NewGigagasExecutor(DefaultGigagasConfig())

	txs := make([]*GigagasTx, 50)
	for i := range txs {
		txs[i] = &GigagasTx{
			From:     types.BytesToAddress([]byte{byte(i % 5)}),
			To:       types.HexToAddress("0x02"),
			GasLimit: 21000,
			Nonce:    uint64(i / 5),
		}
	}

	results, err := ge.ParallelExecute(txs, 4)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 50 {
		t.Fatalf("expected 50 results, got %d", len(results))
	}
	for i, r := range results {
		if !r.Success {
			t.Fatalf("tx %d should succeed", i)
		}
		if r.GasUsed != 21000 {
			t.Fatalf("tx %d: expected 21000 gas, got %d", i, r.GasUsed)
		}
	}
}

func TestParallelExecute_InvalidWorkers(t *testing.T) {
	ge := NewGigagasExecutor(DefaultGigagasConfig())
	txs := []*GigagasTx{{GasLimit: 21000}}
	_, err := ge.ParallelExecute(txs, 0)
	if err != ErrInvalidWorkerCount {
		t.Fatalf("expected ErrInvalidWorkerCount, got %v", err)
	}
	_, err = ge.ParallelExecute(txs, -1)
	if err != ErrInvalidWorkerCount {
		t.Fatalf("expected ErrInvalidWorkerCount, got %v", err)
	}
}

func TestParallelExecute_Empty(t *testing.T) {
	ge := NewGigagasExecutor(DefaultGigagasConfig())
	_, err := ge.ParallelExecute(nil, 4)
	if err != ErrBatchEmpty {
		t.Fatalf("expected ErrBatchEmpty, got %v", err)
	}
}

func TestParallelExecute_TooLarge(t *testing.T) {
	ge := NewGigagasExecutor(GigagasConfig{MaxBatchSize: 5})
	txs := make([]*GigagasTx, 6)
	for i := range txs {
		txs[i] = &GigagasTx{GasLimit: 21000}
	}
	_, err := ge.ParallelExecute(txs, 2)
	if err == nil {
		t.Fatal("expected error for oversized batch")
	}
}

func TestParallelExecute_CapsWorkers(t *testing.T) {
	ge := NewGigagasExecutor(GigagasConfig{MaxWorkers: 2})
	txs := make([]*GigagasTx, 10)
	for i := range txs {
		txs[i] = &GigagasTx{
			From:     types.BytesToAddress([]byte{byte(i)}),
			GasLimit: 21000,
		}
	}
	// Request more workers than MaxWorkers; should not fail.
	results, err := ge.ParallelExecute(txs, 100)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 10 {
		t.Fatalf("expected 10 results, got %d", len(results))
	}
}

func TestParallelExecute_WithNilTx(t *testing.T) {
	ge := NewGigagasExecutor(DefaultGigagasConfig())
	txs := []*GigagasTx{
		{From: types.HexToAddress("0x01"), GasLimit: 21000},
		nil,
		{From: types.HexToAddress("0x02"), GasLimit: 21000},
	}
	results, err := ge.ParallelExecute(txs, 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if results[0].Success != true {
		t.Fatal("tx[0] should succeed")
	}
	if results[1].Success != false {
		t.Fatal("tx[1] (nil) should fail")
	}
	if results[2].Success != true {
		t.Fatal("tx[2] should succeed")
	}
}

func TestMeasureThroughput(t *testing.T) {
	ge := NewGigagasExecutor(DefaultGigagasConfig())

	txs := make([]*GigagasTx, 100)
	for i := range txs {
		txs[i] = &GigagasTx{
			From:     types.HexToAddress("0x01"),
			To:       types.HexToAddress("0x02"),
			GasLimit: 21000,
			Nonce:    uint64(i),
		}
	}

	metrics := ge.MeasureThroughput(txs)
	if metrics.TxCount != 100 {
		t.Fatalf("expected TxCount=100, got %d", metrics.TxCount)
	}
	if metrics.TotalGas != 100*21000 {
		t.Fatalf("expected TotalGas=%d, got %d", 100*21000, metrics.TotalGas)
	}
	if metrics.Duration == 0 {
		t.Fatal("Duration should not be zero")
	}
	if metrics.GasPerSecond == 0 {
		t.Fatal("GasPerSecond should not be zero")
	}
	if metrics.AvgGasPerTx != 21000 {
		t.Fatalf("expected AvgGasPerTx=21000, got %d", metrics.AvgGasPerTx)
	}
}

func TestMeasureThroughput_Empty(t *testing.T) {
	ge := NewGigagasExecutor(DefaultGigagasConfig())
	metrics := ge.MeasureThroughput(nil)
	if metrics.TxCount != 0 {
		t.Fatalf("expected TxCount=0, got %d", metrics.TxCount)
	}
}

func TestExecutorCounters(t *testing.T) {
	ge := NewGigagasExecutor(DefaultGigagasConfig())

	txs := []*GigagasTx{
		{From: types.HexToAddress("0x01"), GasLimit: 21000},
		{From: types.HexToAddress("0x02"), GasLimit: 21000},
	}

	_, err := ge.ExecuteBatch(txs)
	if err != nil {
		t.Fatal(err)
	}
	if ge.TotalExecuted() != 2 {
		t.Fatalf("expected 2 executed, got %d", ge.TotalExecuted())
	}
	if ge.TotalGasUsed() != 42000 {
		t.Fatalf("expected 42000 gas, got %d", ge.TotalGasUsed())
	}
}

func TestMemoryLimitExceeded(t *testing.T) {
	ge := NewGigagasExecutor(GigagasConfig{MemoryLimit: 100})

	// Large calldata should exceed memory limit.
	data := make([]byte, 200)
	txs := []*GigagasTx{
		{From: types.HexToAddress("0x01"), GasLimit: 100_000, Data: data},
	}
	_, err := ge.ExecuteBatch(txs)
	if err != ErrMemoryLimitExceeded {
		t.Fatalf("expected ErrMemoryLimitExceeded, got %v", err)
	}
}

func TestConcurrentAccess(t *testing.T) {
	ge := NewGigagasExecutor(DefaultGigagasConfig())

	var wg sync.WaitGroup
	for g := 0; g < 10; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			txs := make([]*GigagasTx, 20)
			for i := range txs {
				txs[i] = &GigagasTx{
					From:     types.BytesToAddress([]byte{byte(id)}),
					To:       types.HexToAddress("0x02"),
					GasLimit: 21000,
					Nonce:    uint64(i),
				}
			}
			results, err := ge.ExecuteBatch(txs)
			if err != nil {
				t.Errorf("goroutine %d: %v", id, err)
				return
			}
			for j, r := range results {
				if !r.Success {
					t.Errorf("goroutine %d, tx %d: expected success", id, j)
				}
			}
		}(g)
	}
	wg.Wait()

	if ge.TotalExecuted() != 200 {
		t.Fatalf("expected 200 executed, got %d", ge.TotalExecuted())
	}
}

func TestDeterministicTxHash(t *testing.T) {
	ge := NewGigagasExecutor(DefaultGigagasConfig())

	tx := &GigagasTx{
		From:     types.HexToAddress("0xaaaa"),
		To:       types.HexToAddress("0xbbbb"),
		GasLimit: 21000,
		Nonce:    42,
	}

	r1, _ := ge.ExecuteBatch([]*GigagasTx{tx})
	r2, _ := ge.ExecuteBatch([]*GigagasTx{tx})

	if r1[0].TxHash != r2[0].TxHash {
		t.Fatal("same tx should produce the same hash")
	}
}
