package core

import (
	"math/big"
	"testing"

	"github.com/eth2028/eth2028/core/state"
	"github.com/eth2028/eth2028/core/types"
)

// TestCumulativeGasUsed verifies that receipts correctly accumulate gas used
// across multiple transactions in a block.
func TestCumulativeGasUsed(t *testing.T) {
	statedb := state.NewMemoryStateDB()
	sender := types.HexToAddress("0xaaaa")
	receiver := types.HexToAddress("0xbbbb")

	// Fund sender with plenty of ETH.
	hundredETH := new(big.Int).Mul(big.NewInt(100), new(big.Int).SetUint64(1e18))
	statedb.AddBalance(sender, hundredETH)

	// Create 3 simple value transfer transactions.
	tx1 := types.NewTransaction(&types.LegacyTx{
		Nonce: 0, GasPrice: big.NewInt(1), Gas: 21000,
		To: &receiver, Value: big.NewInt(100),
	})
	tx1.SetSender(sender)

	tx2 := types.NewTransaction(&types.LegacyTx{
		Nonce: 1, GasPrice: big.NewInt(1), Gas: 21000,
		To: &receiver, Value: big.NewInt(200),
	})
	tx2.SetSender(sender)

	tx3 := types.NewTransaction(&types.LegacyTx{
		Nonce: 2, GasPrice: big.NewInt(1), Gas: 21000,
		To: &receiver, Value: big.NewInt(300),
	})
	tx3.SetSender(sender)

	header := &types.Header{
		Number:   big.NewInt(1),
		GasLimit: 30_000_000,
		Time:     1000,
		BaseFee:  big.NewInt(1),
		Coinbase: types.HexToAddress("0xfee"),
	}
	block := types.NewBlock(header, &types.Body{
		Transactions: []*types.Transaction{tx1, tx2, tx3},
	})

	proc := NewStateProcessor(TestConfig)
	receipts, err := proc.Process(block, statedb)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	if len(receipts) != 3 {
		t.Fatalf("expected 3 receipts, got %d", len(receipts))
	}

	// Each simple transfer uses exactly 21000 gas.
	// CumulativeGasUsed should be 21000, 42000, 63000.
	expectedCumGas := []uint64{21000, 42000, 63000}
	for i, r := range receipts {
		if r.CumulativeGasUsed != expectedCumGas[i] {
			t.Errorf("receipt[%d].CumulativeGasUsed = %d, want %d",
				i, r.CumulativeGasUsed, expectedCumGas[i])
		}
		if r.GasUsed != 21000 {
			t.Errorf("receipt[%d].GasUsed = %d, want 21000", i, r.GasUsed)
		}
	}
}

// TestReceiptStatusField verifies that post-Byzantium receipt status is
// correctly set to 1 (success) or 0 (failure).
func TestReceiptStatusField(t *testing.T) {
	statedb := state.NewMemoryStateDB()
	sender := types.HexToAddress("0xaaaa")
	contractAddr := types.HexToAddress("0xcccc")

	hundredETH := new(big.Int).Mul(big.NewInt(100), new(big.Int).SetUint64(1e18))
	statedb.AddBalance(sender, hundredETH)

	// Deploy a contract that REVERTs.
	revertCode := []byte{
		0x60, 0x00, // PUSH1 0x00
		0x60, 0x00, // PUSH1 0x00
		0xfd,       // REVERT
	}
	statedb.CreateAccount(contractAddr)
	statedb.SetCode(contractAddr, revertCode)

	// Deploy a contract that succeeds (STOP).
	okAddr := types.HexToAddress("0xdddd")
	statedb.CreateAccount(okAddr)
	statedb.SetCode(okAddr, []byte{0x00}) // STOP

	// tx1: call the reverting contract.
	tx1 := types.NewTransaction(&types.LegacyTx{
		Nonce: 0, GasPrice: big.NewInt(1), Gas: 100000,
		To: &contractAddr, Value: big.NewInt(0),
	})
	tx1.SetSender(sender)

	// tx2: call the successful contract.
	tx2 := types.NewTransaction(&types.LegacyTx{
		Nonce: 1, GasPrice: big.NewInt(1), Gas: 100000,
		To: &okAddr, Value: big.NewInt(0),
	})
	tx2.SetSender(sender)

	header := &types.Header{
		Number:   big.NewInt(1),
		GasLimit: 30_000_000,
		Time:     1000,
		BaseFee:  big.NewInt(1),
		Coinbase: types.HexToAddress("0xfee"),
	}
	block := types.NewBlock(header, &types.Body{
		Transactions: []*types.Transaction{tx1, tx2},
	})

	proc := NewStateProcessor(TestConfig)
	receipts, err := proc.Process(block, statedb)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	if len(receipts) != 2 {
		t.Fatalf("expected 2 receipts, got %d", len(receipts))
	}

	// tx1 should be failed (status 0).
	if receipts[0].Status != types.ReceiptStatusFailed {
		t.Errorf("receipt[0].Status = %d, want %d (failed)",
			receipts[0].Status, types.ReceiptStatusFailed)
	}

	// tx2 should be successful (status 1).
	if receipts[1].Status != types.ReceiptStatusSuccessful {
		t.Errorf("receipt[1].Status = %d, want %d (successful)",
			receipts[1].Status, types.ReceiptStatusSuccessful)
	}

	// Verify the Succeeded() helper method.
	if receipts[0].Succeeded() {
		t.Error("receipt[0].Succeeded() should be false for a reverting tx")
	}
	if !receipts[1].Succeeded() {
		t.Error("receipt[1].Succeeded() should be true for a successful tx")
	}
}

// TestReceiptLogsFromEVM verifies that logs emitted during EVM execution
// are properly captured in receipts with correct context fields.
func TestReceiptLogsFromEVM(t *testing.T) {
	statedb := state.NewMemoryStateDB()
	sender := types.HexToAddress("0xaaaa")

	hundredETH := new(big.Int).Mul(big.NewInt(100), new(big.Int).SetUint64(1e18))
	statedb.AddBalance(sender, hundredETH)

	// Contract that emits LOG0 with 32 bytes of memory data.
	// Bytecode: PUSH1 0x20, PUSH1 0x00, LOG0, STOP
	logContract := types.HexToAddress("0xcccc")
	logCode := []byte{
		0x60, 0x20, // PUSH1 0x20 (size = 32)
		0x60, 0x00, // PUSH1 0x00 (offset = 0)
		0xa0,       // LOG0
		0x00,       // STOP
	}
	statedb.CreateAccount(logContract)
	statedb.SetCode(logContract, logCode)

	// Contract that emits LOG2 with 2 topics.
	// Bytecode: PUSH32 topic2, PUSH32 topic1, PUSH1 0x20, PUSH1 0x00, LOG2, STOP
	logContract2 := types.HexToAddress("0xdddd")
	topic1 := types.HexToHash("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	topic2 := types.HexToHash("0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")

	var log2Code []byte
	// PUSH32 topic2
	log2Code = append(log2Code, 0x7f)
	log2Code = append(log2Code, topic2[:]...)
	// PUSH32 topic1
	log2Code = append(log2Code, 0x7f)
	log2Code = append(log2Code, topic1[:]...)
	// PUSH1 0x20 (size), PUSH1 0x00 (offset), LOG2, STOP
	log2Code = append(log2Code, 0x60, 0x20, 0x60, 0x00, 0xa2, 0x00)

	statedb.CreateAccount(logContract2)
	statedb.SetCode(logContract2, log2Code)

	// tx1: call the LOG0 contract.
	tx1 := types.NewTransaction(&types.LegacyTx{
		Nonce: 0, GasPrice: big.NewInt(1), Gas: 100000,
		To: &logContract, Value: big.NewInt(0),
	})
	tx1.SetSender(sender)

	// tx2: call the LOG2 contract.
	tx2 := types.NewTransaction(&types.LegacyTx{
		Nonce: 1, GasPrice: big.NewInt(1), Gas: 200000,
		To: &logContract2, Value: big.NewInt(0),
	})
	tx2.SetSender(sender)

	header := &types.Header{
		Number:   big.NewInt(5),
		GasLimit: 30_000_000,
		Time:     1000,
		BaseFee:  big.NewInt(1),
		Coinbase: types.HexToAddress("0xfee"),
	}
	block := types.NewBlock(header, &types.Body{
		Transactions: []*types.Transaction{tx1, tx2},
	})

	proc := NewStateProcessor(TestConfig)
	receipts, err := proc.Process(block, statedb)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	if len(receipts) != 2 {
		t.Fatalf("expected 2 receipts, got %d", len(receipts))
	}

	// Receipt 0: should have 1 log from LOG0.
	if len(receipts[0].Logs) != 1 {
		t.Fatalf("receipt[0] should have 1 log, got %d", len(receipts[0].Logs))
	}
	log0 := receipts[0].Logs[0]
	if log0.Address != logContract {
		t.Errorf("log0.Address = %v, want %v", log0.Address, logContract)
	}
	if len(log0.Topics) != 0 {
		t.Errorf("log0 should have 0 topics (LOG0), got %d", len(log0.Topics))
	}
	if log0.TxIndex != 0 {
		t.Errorf("log0.TxIndex = %d, want 0", log0.TxIndex)
	}
	if log0.Index != 0 {
		t.Errorf("log0.Index = %d, want 0 (first log in block)", log0.Index)
	}
	if log0.BlockNumber != 5 {
		t.Errorf("log0.BlockNumber = %d, want 5", log0.BlockNumber)
	}

	// Receipt 1: should have 1 log from LOG2.
	if len(receipts[1].Logs) != 1 {
		t.Fatalf("receipt[1] should have 1 log, got %d", len(receipts[1].Logs))
	}
	log1 := receipts[1].Logs[0]
	if log1.Address != logContract2 {
		t.Errorf("log1.Address = %v, want %v", log1.Address, logContract2)
	}
	if len(log1.Topics) != 2 {
		t.Fatalf("log1 should have 2 topics (LOG2), got %d", len(log1.Topics))
	}
	if log1.Topics[0] != topic1 {
		t.Errorf("log1.Topics[0] = %v, want %v", log1.Topics[0], topic1)
	}
	if log1.Topics[1] != topic2 {
		t.Errorf("log1.Topics[1] = %v, want %v", log1.Topics[1], topic2)
	}
	if log1.TxIndex != 1 {
		t.Errorf("log1.TxIndex = %d, want 1", log1.TxIndex)
	}
	if log1.Index != 1 {
		t.Errorf("log1.Index = %d, want 1 (second log in block)", log1.Index)
	}
	if log1.BlockNumber != 5 {
		t.Errorf("log1.BlockNumber = %d, want 5", log1.BlockNumber)
	}
}

// TestReceiptBloomFilter verifies that the bloom filter on each receipt
// contains the correct log address and topic entries.
func TestReceiptBloomFilter(t *testing.T) {
	statedb := state.NewMemoryStateDB()
	sender := types.HexToAddress("0xaaaa")

	hundredETH := new(big.Int).Mul(big.NewInt(100), new(big.Int).SetUint64(1e18))
	statedb.AddBalance(sender, hundredETH)

	// Contract that emits LOG1 with a known topic.
	contractAddr := types.HexToAddress("0xcccc")
	topic := types.HexToHash("0xdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef")

	var code []byte
	// PUSH32 topic
	code = append(code, 0x7f)
	code = append(code, topic[:]...)
	// PUSH1 0x20, PUSH1 0x00, LOG1, STOP
	code = append(code, 0x60, 0x20, 0x60, 0x00, 0xa1, 0x00)

	statedb.CreateAccount(contractAddr)
	statedb.SetCode(contractAddr, code)

	tx := types.NewTransaction(&types.LegacyTx{
		Nonce: 0, GasPrice: big.NewInt(1), Gas: 200000,
		To: &contractAddr, Value: big.NewInt(0),
	})
	tx.SetSender(sender)

	header := &types.Header{
		Number:   big.NewInt(1),
		GasLimit: 30_000_000,
		Time:     1000,
		BaseFee:  big.NewInt(1),
		Coinbase: types.HexToAddress("0xfee"),
	}
	block := types.NewBlock(header, &types.Body{
		Transactions: []*types.Transaction{tx},
	})

	proc := NewStateProcessor(TestConfig)
	receipts, err := proc.Process(block, statedb)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	if len(receipts) != 1 {
		t.Fatalf("expected 1 receipt, got %d", len(receipts))
	}

	receipt := receipts[0]

	// Bloom should contain the contract address.
	if !types.BloomContains(receipt.Bloom, contractAddr.Bytes()) {
		t.Error("receipt bloom should contain the contract address")
	}

	// Bloom should contain the topic.
	if !types.BloomContains(receipt.Bloom, topic.Bytes()) {
		t.Error("receipt bloom should contain the topic")
	}

	// Bloom should NOT be empty (the log was emitted).
	if receipt.Bloom == (types.Bloom{}) {
		t.Error("receipt bloom should not be zero for a tx that emits logs")
	}
}

// TestReceiptTransactionIndex verifies that each receipt has the correct
// TransactionIndex set during block processing.
func TestReceiptTransactionIndex(t *testing.T) {
	statedb := state.NewMemoryStateDB()
	sender := types.HexToAddress("0xaaaa")
	receiver := types.HexToAddress("0xbbbb")

	hundredETH := new(big.Int).Mul(big.NewInt(100), new(big.Int).SetUint64(1e18))
	statedb.AddBalance(sender, hundredETH)

	var txs []*types.Transaction
	for i := uint64(0); i < 5; i++ {
		tx := types.NewTransaction(&types.LegacyTx{
			Nonce: i, GasPrice: big.NewInt(1), Gas: 21000,
			To: &receiver, Value: big.NewInt(1),
		})
		tx.SetSender(sender)
		txs = append(txs, tx)
	}

	header := &types.Header{
		Number:   big.NewInt(1),
		GasLimit: 30_000_000,
		Time:     1000,
		BaseFee:  big.NewInt(1),
		Coinbase: types.HexToAddress("0xfee"),
	}
	block := types.NewBlock(header, &types.Body{Transactions: txs})

	proc := NewStateProcessor(TestConfig)
	receipts, err := proc.Process(block, statedb)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	for i, r := range receipts {
		if r.TransactionIndex != uint(i) {
			t.Errorf("receipt[%d].TransactionIndex = %d, want %d",
				i, r.TransactionIndex, i)
		}
		if r.TxHash != txs[i].Hash() {
			t.Errorf("receipt[%d].TxHash mismatch", i)
		}
	}
}

// TestReceiptBlockContextFields verifies that BlockHash and BlockNumber
// are set on receipts produced during block processing.
func TestReceiptBlockContextFields(t *testing.T) {
	statedb := state.NewMemoryStateDB()
	sender := types.HexToAddress("0xaaaa")
	receiver := types.HexToAddress("0xbbbb")

	hundredETH := new(big.Int).Mul(big.NewInt(100), new(big.Int).SetUint64(1e18))
	statedb.AddBalance(sender, hundredETH)

	tx := types.NewTransaction(&types.LegacyTx{
		Nonce: 0, GasPrice: big.NewInt(1), Gas: 21000,
		To: &receiver, Value: big.NewInt(1),
	})
	tx.SetSender(sender)

	header := &types.Header{
		Number:   big.NewInt(42),
		GasLimit: 30_000_000,
		Time:     1000,
		BaseFee:  big.NewInt(1),
		Coinbase: types.HexToAddress("0xfee"),
	}
	block := types.NewBlock(header, &types.Body{
		Transactions: []*types.Transaction{tx},
	})

	proc := NewStateProcessor(TestConfig)
	receipts, err := proc.Process(block, statedb)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	if len(receipts) != 1 {
		t.Fatalf("expected 1 receipt, got %d", len(receipts))
	}

	r := receipts[0]
	if r.BlockHash != block.Hash() {
		t.Errorf("receipt.BlockHash = %v, want %v", r.BlockHash, block.Hash())
	}
	if r.BlockNumber == nil || r.BlockNumber.Uint64() != 42 {
		t.Errorf("receipt.BlockNumber = %v, want 42", r.BlockNumber)
	}
}

// TestCumulativeGasWithMixedTxTypes verifies cumulative gas accumulation
// when the block has a mix of simple transfers and contract calls.
func TestCumulativeGasWithMixedTxTypes(t *testing.T) {
	statedb := state.NewMemoryStateDB()
	sender := types.HexToAddress("0xaaaa")
	receiver := types.HexToAddress("0xbbbb")
	contractAddr := types.HexToAddress("0xcccc")

	hundredETH := new(big.Int).Mul(big.NewInt(100), new(big.Int).SetUint64(1e18))
	statedb.AddBalance(sender, hundredETH)

	// Simple contract: PUSH1 0x42, PUSH1 0x00, MSTORE, PUSH1 0x20, PUSH1 0x00, RETURN
	contractCode := []byte{
		0x60, 0x42, 0x60, 0x00, 0x52,
		0x60, 0x20, 0x60, 0x00, 0xf3,
	}
	statedb.CreateAccount(contractAddr)
	statedb.SetCode(contractAddr, contractCode)

	// tx1: simple transfer (21000 gas).
	tx1 := types.NewTransaction(&types.LegacyTx{
		Nonce: 0, GasPrice: big.NewInt(1), Gas: 21000,
		To: &receiver, Value: big.NewInt(1),
	})
	tx1.SetSender(sender)

	// tx2: contract call (more than 21000 gas).
	tx2 := types.NewTransaction(&types.LegacyTx{
		Nonce: 1, GasPrice: big.NewInt(1), Gas: 100000,
		To: &contractAddr, Value: big.NewInt(0),
	})
	tx2.SetSender(sender)

	// tx3: another simple transfer.
	tx3 := types.NewTransaction(&types.LegacyTx{
		Nonce: 2, GasPrice: big.NewInt(1), Gas: 21000,
		To: &receiver, Value: big.NewInt(2),
	})
	tx3.SetSender(sender)

	header := &types.Header{
		Number:   big.NewInt(1),
		GasLimit: 30_000_000,
		Time:     1000,
		BaseFee:  big.NewInt(1),
		Coinbase: types.HexToAddress("0xfee"),
	}
	block := types.NewBlock(header, &types.Body{
		Transactions: []*types.Transaction{tx1, tx2, tx3},
	})

	proc := NewStateProcessor(TestConfig)
	receipts, err := proc.Process(block, statedb)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	if len(receipts) != 3 {
		t.Fatalf("expected 3 receipts, got %d", len(receipts))
	}

	// Verify cumulative gas is monotonically increasing.
	var prevCum uint64
	for i, r := range receipts {
		if r.CumulativeGasUsed <= prevCum && i > 0 {
			t.Errorf("receipt[%d].CumulativeGasUsed (%d) should be > receipt[%d] (%d)",
				i, r.CumulativeGasUsed, i-1, prevCum)
		}
		prevCum = r.CumulativeGasUsed
	}

	// Verify CumulativeGasUsed equals the running sum.
	var runningSum uint64
	for i, r := range receipts {
		runningSum += r.GasUsed
		if r.CumulativeGasUsed != runningSum {
			t.Errorf("receipt[%d].CumulativeGasUsed = %d, want running sum %d",
				i, r.CumulativeGasUsed, runningSum)
		}
	}

	// The contract call should use more gas than 21000.
	if receipts[1].GasUsed <= 21000 {
		t.Errorf("contract call receipt should use more than 21000 gas, used %d",
			receipts[1].GasUsed)
	}
}

// TestGlobalLogIndex verifies that log indices are globally sequential
// across multiple transactions in a block.
func TestGlobalLogIndex(t *testing.T) {
	statedb := state.NewMemoryStateDB()
	sender := types.HexToAddress("0xaaaa")

	hundredETH := new(big.Int).Mul(big.NewInt(100), new(big.Int).SetUint64(1e18))
	statedb.AddBalance(sender, hundredETH)

	// Contract 1: emits LOG0 (1 log).
	contract1 := types.HexToAddress("0xcc01")
	code1 := []byte{
		0x60, 0x20, // PUSH1 0x20
		0x60, 0x00, // PUSH1 0x00
		0xa0,       // LOG0
		0x00,       // STOP
	}
	statedb.CreateAccount(contract1)
	statedb.SetCode(contract1, code1)

	// Contract 2: emits 2 LOG0s.
	contract2 := types.HexToAddress("0xcc02")
	code2 := []byte{
		0x60, 0x20, // PUSH1 0x20
		0x60, 0x00, // PUSH1 0x00
		0xa0,       // LOG0 (1st log)
		0x60, 0x20, // PUSH1 0x20
		0x60, 0x00, // PUSH1 0x00
		0xa0,       // LOG0 (2nd log)
		0x00,       // STOP
	}
	statedb.CreateAccount(contract2)
	statedb.SetCode(contract2, code2)

	// tx1: call contract1 (1 log).
	tx1 := types.NewTransaction(&types.LegacyTx{
		Nonce: 0, GasPrice: big.NewInt(1), Gas: 100000,
		To: &contract1, Value: big.NewInt(0),
	})
	tx1.SetSender(sender)

	// tx2: call contract2 (2 logs).
	tx2 := types.NewTransaction(&types.LegacyTx{
		Nonce: 1, GasPrice: big.NewInt(1), Gas: 100000,
		To: &contract2, Value: big.NewInt(0),
	})
	tx2.SetSender(sender)

	header := &types.Header{
		Number:   big.NewInt(1),
		GasLimit: 30_000_000,
		Time:     1000,
		BaseFee:  big.NewInt(1),
		Coinbase: types.HexToAddress("0xfee"),
	}
	block := types.NewBlock(header, &types.Body{
		Transactions: []*types.Transaction{tx1, tx2},
	})

	proc := NewStateProcessor(TestConfig)
	receipts, err := proc.Process(block, statedb)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	if len(receipts) != 2 {
		t.Fatalf("expected 2 receipts, got %d", len(receipts))
	}

	// Receipt 0: 1 log, global index 0.
	if len(receipts[0].Logs) != 1 {
		t.Fatalf("receipt[0] should have 1 log, got %d", len(receipts[0].Logs))
	}
	if receipts[0].Logs[0].Index != 0 {
		t.Errorf("receipt[0].Logs[0].Index = %d, want 0", receipts[0].Logs[0].Index)
	}

	// Receipt 1: 2 logs, global indices 1 and 2.
	if len(receipts[1].Logs) != 2 {
		t.Fatalf("receipt[1] should have 2 logs, got %d", len(receipts[1].Logs))
	}
	if receipts[1].Logs[0].Index != 1 {
		t.Errorf("receipt[1].Logs[0].Index = %d, want 1", receipts[1].Logs[0].Index)
	}
	if receipts[1].Logs[1].Index != 2 {
		t.Errorf("receipt[1].Logs[1].Index = %d, want 2", receipts[1].Logs[1].Index)
	}
}

// TestReceiptRLPRoundTripWithLogs verifies that receipt RLP encoding/decoding
// preserves all consensus fields including logs and bloom.
func TestReceiptRLPRoundTripWithLogs(t *testing.T) {
	addr := types.HexToAddress("0xdeadbeef")
	topic := types.HexToHash("0xaabbccdd")

	logs := []*types.Log{
		{
			Address: addr,
			Topics:  []types.Hash{topic},
			Data:    []byte{0x01, 0x02, 0x03},
		},
	}

	receipt := &types.Receipt{
		Type:              types.DynamicFeeTxType,
		Status:            types.ReceiptStatusSuccessful,
		CumulativeGasUsed: 84000,
		Bloom:             types.LogsBloom(logs),
		Logs:              logs,
	}

	enc, err := receipt.EncodeRLP()
	if err != nil {
		t.Fatalf("EncodeRLP failed: %v", err)
	}

	decoded, err := types.DecodeReceiptRLP(enc)
	if err != nil {
		t.Fatalf("DecodeReceiptRLP failed: %v", err)
	}

	if decoded.Type != receipt.Type {
		t.Errorf("Type: got %d, want %d", decoded.Type, receipt.Type)
	}
	if decoded.Status != receipt.Status {
		t.Errorf("Status: got %d, want %d", decoded.Status, receipt.Status)
	}
	if decoded.CumulativeGasUsed != receipt.CumulativeGasUsed {
		t.Errorf("CumulativeGasUsed: got %d, want %d",
			decoded.CumulativeGasUsed, receipt.CumulativeGasUsed)
	}
	if decoded.Bloom != receipt.Bloom {
		t.Error("Bloom mismatch after RLP roundtrip")
	}
	if len(decoded.Logs) != 1 {
		t.Fatalf("expected 1 log, got %d", len(decoded.Logs))
	}
	if decoded.Logs[0].Address != addr {
		t.Error("log address mismatch after RLP roundtrip")
	}
	if len(decoded.Logs[0].Topics) != 1 || decoded.Logs[0].Topics[0] != topic {
		t.Error("log topics mismatch after RLP roundtrip")
	}
}

// TestDeriveReceiptFields verifies that DeriveReceiptFields correctly populates
// all derived fields across a list of receipts.
func TestDeriveReceiptFields(t *testing.T) {
	blockHash := types.HexToHash("0xblockhash")
	blockNumber := uint64(100)
	baseFee := big.NewInt(1_000_000_000)

	// Create receipts with logs.
	r1 := &types.Receipt{
		Status:            types.ReceiptStatusSuccessful,
		CumulativeGasUsed: 21000,
		Logs: []*types.Log{
			{Address: types.HexToAddress("0xc1")},
			{Address: types.HexToAddress("0xc1")},
		},
	}
	r2 := &types.Receipt{
		Status:            types.ReceiptStatusSuccessful,
		CumulativeGasUsed: 42000,
		Logs: []*types.Log{
			{Address: types.HexToAddress("0xc2")},
		},
	}
	r3 := &types.Receipt{
		Status:            types.ReceiptStatusFailed,
		CumulativeGasUsed: 63000,
	}

	to := types.HexToAddress("0xbbbb")
	txs := []*types.Transaction{
		types.NewTransaction(&types.LegacyTx{Nonce: 0, GasPrice: big.NewInt(1), Gas: 21000, To: &to}),
		types.NewTransaction(&types.LegacyTx{Nonce: 1, GasPrice: big.NewInt(1), Gas: 21000, To: &to}),
		types.NewTransaction(&types.LegacyTx{Nonce: 2, GasPrice: big.NewInt(1), Gas: 21000, To: &to}),
	}

	receipts := []*types.Receipt{r1, r2, r3}
	types.DeriveReceiptFields(receipts, blockHash, blockNumber, baseFee, txs)

	// Check receipt fields.
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

	// Check log fields: global indices should be 0, 1, 2 across all receipts.
	if r1.Logs[0].Index != 0 {
		t.Errorf("r1.Logs[0].Index = %d, want 0", r1.Logs[0].Index)
	}
	if r1.Logs[1].Index != 1 {
		t.Errorf("r1.Logs[1].Index = %d, want 1", r1.Logs[1].Index)
	}
	if r2.Logs[0].Index != 2 {
		t.Errorf("r2.Logs[0].Index = %d, want 2", r2.Logs[0].Index)
	}

	// Check log context.
	for _, log := range r1.Logs {
		if log.BlockHash != blockHash {
			t.Error("log.BlockHash mismatch")
		}
		if log.BlockNumber != blockNumber {
			t.Errorf("log.BlockNumber = %d, want %d", log.BlockNumber, blockNumber)
		}
		if log.TxIndex != 0 {
			t.Errorf("log.TxIndex = %d, want 0", log.TxIndex)
		}
	}
	if r2.Logs[0].TxIndex != 1 {
		t.Errorf("r2.Logs[0].TxIndex = %d, want 1", r2.Logs[0].TxIndex)
	}
}

// TestReceiptNoLogsSimpleTransfer verifies that a simple value transfer
// produces a receipt with no logs and a zero bloom filter.
func TestReceiptNoLogsSimpleTransfer(t *testing.T) {
	statedb := state.NewMemoryStateDB()
	sender := types.HexToAddress("0xaaaa")
	receiver := types.HexToAddress("0xbbbb")

	hundredETH := new(big.Int).Mul(big.NewInt(100), new(big.Int).SetUint64(1e18))
	statedb.AddBalance(sender, hundredETH)

	tx := types.NewTransaction(&types.LegacyTx{
		Nonce: 0, GasPrice: big.NewInt(1), Gas: 21000,
		To: &receiver, Value: big.NewInt(1),
	})
	tx.SetSender(sender)

	header := &types.Header{
		Number:   big.NewInt(1),
		GasLimit: 30_000_000,
		Time:     1000,
		BaseFee:  big.NewInt(1),
		Coinbase: types.HexToAddress("0xfee"),
	}
	block := types.NewBlock(header, &types.Body{
		Transactions: []*types.Transaction{tx},
	})

	proc := NewStateProcessor(TestConfig)
	receipts, err := proc.Process(block, statedb)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	if len(receipts) != 1 {
		t.Fatalf("expected 1 receipt, got %d", len(receipts))
	}

	r := receipts[0]
	if len(r.Logs) != 0 {
		t.Errorf("simple transfer should have 0 logs, got %d", len(r.Logs))
	}
	if r.Bloom != (types.Bloom{}) {
		t.Error("simple transfer should have zero bloom filter")
	}
	if r.Status != types.ReceiptStatusSuccessful {
		t.Errorf("simple transfer should succeed, got status %d", r.Status)
	}
	if r.GasUsed != 21000 {
		t.Errorf("simple transfer should use 21000 gas, got %d", r.GasUsed)
	}
	if r.CumulativeGasUsed != 21000 {
		t.Errorf("CumulativeGasUsed = %d, want 21000", r.CumulativeGasUsed)
	}
}
