package core

import (
	"math/big"
	"sync"
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

// newExecTestHeader creates a test header with the given block number and gas limit.
func newExecTestHeader(num uint64, gasLimit uint64) *types.Header {
	return &types.Header{
		Number:   new(big.Int).SetUint64(num),
		GasLimit: gasLimit,
		GasUsed:  0,
		Time:     1000 + num,
		Root:     types.HexToHash("0x01"),
	}
}

// newExecTestTx creates a test legacy transaction with the given gas and data.
func newExecTestTx(gas uint64, data []byte) *types.Transaction {
	to := types.HexToAddress("0xdead")
	return types.NewTransaction(&types.LegacyTx{
		Nonce:    0,
		GasPrice: big.NewInt(1),
		Gas:      gas,
		To:       &to,
		Value:    big.NewInt(0),
		Data:     data,
	})
}

func TestNewBlockExecutor(t *testing.T) {
	config := DefaultExecutorConfig()
	be := NewBlockExecutor(config)
	if be == nil {
		t.Fatal("expected non-nil executor")
	}
	stats := be.ExecutionStats()
	if stats.BlocksExecuted != 0 || stats.TxsProcessed != 0 || stats.TotalGasUsed != 0 {
		t.Fatalf("expected zero stats, got %+v", stats)
	}
}

func TestExecuteEmptyBlock(t *testing.T) {
	be := NewBlockExecutor(DefaultExecutorConfig())
	header := newExecTestHeader(1, 30_000_000)

	result, err := be.Execute(header, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Fatal("expected success")
	}
	if result.GasUsed != 0 {
		t.Fatalf("expected 0 gas used, got %d", result.GasUsed)
	}
	if result.TxCount != 0 {
		t.Fatalf("expected 0 tx count, got %d", result.TxCount)
	}
	if result.StateRoot.IsZero() {
		t.Fatal("expected non-zero state root")
	}
	// Empty block should get EmptyRootHash for receipts.
	if result.ReceiptsRoot != types.EmptyRootHash {
		t.Fatalf("expected empty root hash for receipts, got %s", result.ReceiptsRoot.Hex())
	}
}

func TestExecuteWithTransactions(t *testing.T) {
	be := NewBlockExecutor(DefaultExecutorConfig())
	header := newExecTestHeader(1, 30_000_000)

	txs := []*types.Transaction{
		newExecTestTx(21000, nil),
		newExecTestTx(21000, nil),
		newExecTestTx(50000, []byte{0x01, 0x02}),
	}

	result, err := be.Execute(header, txs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Fatal("expected success")
	}
	expectedGas := uint64(21000 + 21000 + 50000)
	if result.GasUsed != expectedGas {
		t.Fatalf("expected gas %d, got %d", expectedGas, result.GasUsed)
	}
	if result.TxCount != 3 {
		t.Fatalf("expected 3 txs, got %d", result.TxCount)
	}
	if result.StateRoot.IsZero() {
		t.Fatal("expected non-zero state root")
	}
	if result.ReceiptsRoot.IsZero() {
		t.Fatal("expected non-zero receipts root")
	}
}

func TestExecuteNilHeader(t *testing.T) {
	be := NewBlockExecutor(DefaultExecutorConfig())
	_, err := be.Execute(nil, nil)
	if err != ErrNilHeader {
		t.Fatalf("expected ErrNilHeader, got %v", err)
	}
}

func TestExecuteNilTransaction(t *testing.T) {
	be := NewBlockExecutor(DefaultExecutorConfig())
	header := newExecTestHeader(1, 30_000_000)

	txs := []*types.Transaction{newExecTestTx(21000, nil), nil}
	_, err := be.Execute(header, txs)
	if err == nil {
		t.Fatal("expected error for nil transaction")
	}
}

func TestExecuteGasExceeded(t *testing.T) {
	be := NewBlockExecutor(DefaultExecutorConfig())
	header := newExecTestHeader(1, 50000) // low gas limit

	txs := []*types.Transaction{
		newExecTestTx(30000, nil),
		newExecTestTx(30000, nil), // exceeds limit
	}
	_, err := be.Execute(header, txs)
	if err == nil {
		t.Fatal("expected gas exceeded error")
	}
}

func TestExecuteMaxGasPerBlockConfig(t *testing.T) {
	config := ExecutorConfig{MaxGasPerBlock: 40000}
	be := NewBlockExecutor(config)
	header := newExecTestHeader(1, 30_000_000) // header has high limit

	txs := []*types.Transaction{
		newExecTestTx(25000, nil),
		newExecTestTx(25000, nil), // exceeds config max of 40000
	}
	_, err := be.Execute(header, txs)
	if err == nil {
		t.Fatal("expected gas exceeded error from config limit")
	}
}

func TestExecuteWithTracing(t *testing.T) {
	config := ExecutorConfig{TraceExecution: true, MaxGasPerBlock: 30_000_000}
	be := NewBlockExecutor(config)
	header := newExecTestHeader(1, 30_000_000)

	txs := []*types.Transaction{newExecTestTx(21000, []byte{0xab})}
	result, err := be.Execute(header, txs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Fatal("expected success")
	}
	// With tracing, logs bloom should be non-zero since logs are generated.
	allZero := true
	for _, b := range result.LogsBloom {
		if b != 0 {
			allZero = false
			break
		}
	}
	if allZero {
		t.Fatal("expected non-zero logs bloom with tracing enabled")
	}
}

func TestValidateExecution(t *testing.T) {
	be := NewBlockExecutor(DefaultExecutorConfig())
	header := newExecTestHeader(1, 30_000_000)

	txs := []*types.Transaction{newExecTestTx(21000, nil)}
	result, err := be.Execute(header, txs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Set header fields to match the result.
	header.GasUsed = result.GasUsed
	header.Root = result.StateRoot
	header.ReceiptHash = result.ReceiptsRoot

	err = be.ValidateExecution(result, header)
	if err != nil {
		t.Fatalf("unexpected validation error: %v", err)
	}
}

func TestValidateExecutionGasMismatch(t *testing.T) {
	be := NewBlockExecutor(DefaultExecutorConfig())
	header := newExecTestHeader(1, 30_000_000)

	txs := []*types.Transaction{newExecTestTx(21000, nil)}
	result, _ := be.Execute(header, txs)

	header.GasUsed = 999 // mismatch
	header.Root = result.StateRoot
	header.ReceiptHash = result.ReceiptsRoot

	err := be.ValidateExecution(result, header)
	if err == nil {
		t.Fatal("expected gas mismatch error")
	}
}

func TestValidateExecutionRootMismatch(t *testing.T) {
	be := NewBlockExecutor(DefaultExecutorConfig())
	header := newExecTestHeader(1, 30_000_000)

	txs := []*types.Transaction{newExecTestTx(21000, nil)}
	result, _ := be.Execute(header, txs)

	header.GasUsed = result.GasUsed
	header.Root = types.HexToHash("0xdeadbeef") // mismatch
	header.ReceiptHash = result.ReceiptsRoot

	err := be.ValidateExecution(result, header)
	if err == nil {
		t.Fatal("expected root mismatch error")
	}
}

func TestValidateExecutionNilInputs(t *testing.T) {
	be := NewBlockExecutor(DefaultExecutorConfig())

	err := be.ValidateExecution(nil, newExecTestHeader(1, 30_000_000))
	if err == nil {
		t.Fatal("expected error for nil result")
	}

	result := &BlockExecutionResult{Success: true}
	err = be.ValidateExecution(result, nil)
	if err != ErrNilHeader {
		t.Fatalf("expected ErrNilHeader, got %v", err)
	}
}

func TestBlockExecutorEstimateGasSimpleTransfer(t *testing.T) {
	be := NewBlockExecutor(DefaultExecutorConfig())
	tx := newExecTestTx(100000, nil)

	gas, err := be.EstimateGas(tx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gas != 21000 {
		t.Fatalf("expected 21000 gas for simple transfer, got %d", gas)
	}
}

func TestEstimateGasWithCalldata(t *testing.T) {
	be := NewBlockExecutor(DefaultExecutorConfig())
	data := []byte{0x00, 0x00, 0x01, 0x02} // 2 zero bytes (4 gas each) + 2 nonzero (16 each)
	tx := newExecTestTx(100000, data)

	gas, err := be.EstimateGas(tx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := uint64(21000 + 2*4 + 2*16) // 21000 + 8 + 32 = 21040
	if gas != expected {
		t.Fatalf("expected %d gas, got %d", expected, gas)
	}
}

func TestBlockExecutorEstimateGasContractCreation(t *testing.T) {
	be := NewBlockExecutor(DefaultExecutorConfig())
	// Contract creation: no To address.
	tx := types.NewTransaction(&types.LegacyTx{
		Nonce:    0,
		GasPrice: big.NewInt(1),
		Gas:      100000,
		To:       nil, // contract creation
		Value:    big.NewInt(0),
		Data:     []byte{0x60, 0x60}, // 2 nonzero bytes
	})

	gas, err := be.EstimateGas(tx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := uint64(53000 + 2*16) // 53032
	if gas != expected {
		t.Fatalf("expected %d gas, got %d", expected, gas)
	}
}

func TestEstimateGasNilTransaction(t *testing.T) {
	be := NewBlockExecutor(DefaultExecutorConfig())
	_, err := be.EstimateGas(nil)
	if err != ErrNilTransaction {
		t.Fatalf("expected ErrNilTransaction, got %v", err)
	}
}

func TestEstimateGasWithAccessList(t *testing.T) {
	be := NewBlockExecutor(DefaultExecutorConfig())
	to := types.HexToAddress("0xdead")
	tx := types.NewTransaction(&types.AccessListTx{
		ChainID:  big.NewInt(1),
		Nonce:    0,
		GasPrice: big.NewInt(1),
		Gas:      100000,
		To:       &to,
		Value:    big.NewInt(0),
		AccessList: types.AccessList{
			{Address: types.HexToAddress("0xbeef"), StorageKeys: []types.Hash{{}, {}}},
		},
	})

	gas, err := be.EstimateGas(tx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// 21000 base + 2400 for 1 address + 1900*2 for 2 keys = 27200
	expected := uint64(21000 + 2400 + 2*1900)
	if gas != expected {
		t.Fatalf("expected %d gas, got %d", expected, gas)
	}
}

func TestExecutionStats(t *testing.T) {
	be := NewBlockExecutor(DefaultExecutorConfig())
	header := newExecTestHeader(1, 30_000_000)

	txs := []*types.Transaction{
		newExecTestTx(21000, nil),
		newExecTestTx(50000, nil),
	}
	be.Execute(header, txs)

	stats := be.ExecutionStats()
	if stats.BlocksExecuted != 1 {
		t.Fatalf("expected 1 block executed, got %d", stats.BlocksExecuted)
	}
	if stats.TxsProcessed != 2 {
		t.Fatalf("expected 2 txs processed, got %d", stats.TxsProcessed)
	}
	if stats.TotalGasUsed != 71000 {
		t.Fatalf("expected 71000 total gas, got %d", stats.TotalGasUsed)
	}

	// Execute a second block.
	be.Execute(header, txs[:1])
	stats = be.ExecutionStats()
	if stats.BlocksExecuted != 2 {
		t.Fatalf("expected 2 blocks, got %d", stats.BlocksExecuted)
	}
	if stats.TxsProcessed != 3 {
		t.Fatalf("expected 3 txs, got %d", stats.TxsProcessed)
	}
}

func TestReset(t *testing.T) {
	be := NewBlockExecutor(DefaultExecutorConfig())
	header := newExecTestHeader(1, 30_000_000)

	be.Execute(header, []*types.Transaction{newExecTestTx(21000, nil)})
	be.Reset()

	stats := be.ExecutionStats()
	if stats.BlocksExecuted != 0 || stats.TxsProcessed != 0 || stats.TotalGasUsed != 0 {
		t.Fatalf("expected zero stats after reset, got %+v", stats)
	}
}

func TestExecuteDeterministic(t *testing.T) {
	be := NewBlockExecutor(DefaultExecutorConfig())
	header := newExecTestHeader(1, 30_000_000)
	txs := []*types.Transaction{newExecTestTx(21000, nil), newExecTestTx(30000, []byte{0x01})}

	r1, _ := be.Execute(header, txs)
	be.Reset()
	r2, _ := be.Execute(header, txs)

	if r1.StateRoot != r2.StateRoot {
		t.Fatal("state root not deterministic")
	}
	if r1.ReceiptsRoot != r2.ReceiptsRoot {
		t.Fatal("receipts root not deterministic")
	}
	if r1.GasUsed != r2.GasUsed {
		t.Fatal("gas used not deterministic")
	}
}

func TestExecuteConcurrent(t *testing.T) {
	be := NewBlockExecutor(DefaultExecutorConfig())
	header := newExecTestHeader(1, 30_000_000)
	txs := []*types.Transaction{newExecTestTx(21000, nil)}

	var wg sync.WaitGroup
	const goroutines = 10

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := be.Execute(header, txs)
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		}()
	}
	wg.Wait()

	stats := be.ExecutionStats()
	if stats.BlocksExecuted != goroutines {
		t.Fatalf("expected %d blocks, got %d", goroutines, stats.BlocksExecuted)
	}
	if stats.TxsProcessed != goroutines {
		t.Fatalf("expected %d txs, got %d", goroutines, stats.TxsProcessed)
	}
}
