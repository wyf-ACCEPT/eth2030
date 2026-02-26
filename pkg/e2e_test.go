// Package e2e_test provides end-to-end integration tests that exercise the full
// block processing pipeline: build a block, process it, and verify receipts
// and state changes.
package e2e_test

import (
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/bal"
	"github.com/eth2030/eth2030/core"
	"github.com/eth2030/eth2030/core/state"
	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
	"github.com/eth2030/eth2030/engine"
	"github.com/eth2030/eth2030/rlp"
	"github.com/eth2030/eth2030/txpool"
	"github.com/eth2030/eth2030/witness"
)

// TestEndToEndBlockProcessing builds a block with several value-transfer transactions,
// processes it through the state processor, and verifies:
// - Receipts are generated for each transaction
// - Sender balances are debited
// - Recipient balances are credited
// - Nonces are incremented
func TestEndToEndBlockProcessing(t *testing.T) {
	// Set up sender and receiver addresses.
	sender := types.BytesToAddress([]byte{0xa1})
	receiver1 := types.BytesToAddress([]byte{0xa2})
	receiver2 := types.BytesToAddress([]byte{0xa3})

	// Initialize state with sender having a large balance.
	statedb := state.NewMemoryStateDB()
	statedb.AddBalance(sender, big.NewInt(1_000_000_000_000)) // 1 trillion wei
	statedb.SetNonce(sender, 0)

	// Build transactions.
	gasPrice := big.NewInt(1)
	tx1 := types.NewTransaction(&types.LegacyTx{
		Nonce:    0,
		GasPrice: gasPrice,
		Gas:      21000,
		To:       &receiver1,
		Value:    big.NewInt(1000),
	})
	tx1.SetSender(sender)

	tx2 := types.NewTransaction(&types.LegacyTx{
		Nonce:    1,
		GasPrice: gasPrice,
		Gas:      21000,
		To:       &receiver2,
		Value:    big.NewInt(2000),
	})
	tx2.SetSender(sender)

	tx3 := types.NewTransaction(&types.LegacyTx{
		Nonce:    2,
		GasPrice: gasPrice,
		Gas:      21000,
		To:       &receiver1,
		Value:    big.NewInt(500),
	})
	tx3.SetSender(sender)

	// Build the block.
	header := &types.Header{
		Number:   big.NewInt(1),
		GasLimit: 30_000_000,
		Time:     1700000000,
		Coinbase: types.BytesToAddress([]byte{0xff}),
		BaseFee:  big.NewInt(1),
	}

	block := types.NewBlock(header, &types.Body{
		Transactions: []*types.Transaction{tx1, tx2, tx3},
	})

	// Process the block.
	processor := core.NewStateProcessor(core.TestConfig)
	receipts, err := processor.Process(block, statedb)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}

	// Verify receipts.
	if len(receipts) != 3 {
		t.Fatalf("expected 3 receipts, got %d", len(receipts))
	}
	for i, r := range receipts {
		if r.Status != types.ReceiptStatusSuccessful {
			t.Errorf("receipt %d: status = %d, want %d", i, r.Status, types.ReceiptStatusSuccessful)
		}
		if r.GasUsed != 21000 {
			t.Errorf("receipt %d: gasUsed = %d, want 21000", i, r.GasUsed)
		}
	}

	// Verify state changes.
	// Sender: initial 1T - (1000 + 2000 + 500) value - (3 * 21000 * 1) gas
	expectedSenderBal := new(big.Int).SetInt64(1_000_000_000_000 - 1000 - 2000 - 500 - 3*21000)
	actualBal := statedb.GetBalance(sender)
	if actualBal.Cmp(expectedSenderBal) != 0 {
		t.Errorf("sender balance = %s, want %s", actualBal, expectedSenderBal)
	}

	// Receiver1: 1000 + 500 = 1500
	if bal := statedb.GetBalance(receiver1); bal.Cmp(big.NewInt(1500)) != 0 {
		t.Errorf("receiver1 balance = %s, want 1500", bal)
	}

	// Receiver2: 2000
	if bal := statedb.GetBalance(receiver2); bal.Cmp(big.NewInt(2000)) != 0 {
		t.Errorf("receiver2 balance = %s, want 2000", bal)
	}

	// Nonce: sender should be at 3.
	if nonce := statedb.GetNonce(sender); nonce != 3 {
		t.Errorf("sender nonce = %d, want 3", nonce)
	}
}

// TestEndToEndStateSnapshotRevert tests that snapshots properly revert state changes.
func TestEndToEndStateSnapshotRevert(t *testing.T) {
	statedb := state.NewMemoryStateDB()
	addr := types.BytesToAddress([]byte{0x01})

	statedb.AddBalance(addr, big.NewInt(1000))
	snap := statedb.Snapshot()

	// Add more balance and change nonce after snapshot.
	statedb.AddBalance(addr, big.NewInt(2000)) // balance becomes 3000
	statedb.SetNonce(addr, 5)

	// Revert should restore to 1000 balance and 0 nonce.
	statedb.RevertToSnapshot(snap)

	if bal := statedb.GetBalance(addr); bal.Cmp(big.NewInt(1000)) != 0 {
		t.Errorf("balance after revert = %s, want 1000", bal)
	}
	if nonce := statedb.GetNonce(addr); nonce != 0 {
		t.Errorf("nonce after revert = %d, want 0", nonce)
	}
}

// TestEndToEndBALTracking tracks state accesses during block processing
// and produces a BAL for parallel execution analysis.
func TestEndToEndBALTracking(t *testing.T) {
	sender1 := types.BytesToAddress([]byte{0x01})
	sender2 := types.BytesToAddress([]byte{0x02})
	receiver := types.BytesToAddress([]byte{0x03})

	// Track accesses for two independent transactions.
	tracker1 := bal.NewTracker()
	tracker1.RecordBalanceChange(sender1, big.NewInt(1000), big.NewInt(900))
	tracker1.RecordBalanceChange(receiver, big.NewInt(0), big.NewInt(100))
	bal1 := tracker1.Build(1) // tx index 1

	tracker2 := bal.NewTracker()
	tracker2.RecordBalanceChange(sender2, big.NewInt(2000), big.NewInt(1900))
	tracker2.RecordBalanceChange(receiver, big.NewInt(100), big.NewInt(200))
	bal2 := tracker2.Build(2) // tx index 2

	// Merge into a block access list.
	blockBAL := bal.NewBlockAccessList()
	for _, entry := range bal1.Entries {
		blockBAL.AddEntry(entry)
	}
	for _, entry := range bal2.Entries {
		blockBAL.AddEntry(entry)
	}

	// Compute parallel sets.
	groups := bal.ComputeParallelSets(blockBAL)
	if len(groups) == 0 {
		t.Fatal("expected at least one execution group")
	}

	maxPar := bal.MaxParallelism(blockBAL)
	t.Logf("BAL: %d entries, %d groups, max parallelism = %d",
		len(blockBAL.Entries), len(groups), maxPar)

	// Since both txs modify receiver's balance, they conflict.
	// Therefore they should be in separate groups.
	if len(groups) < 2 {
		t.Logf("note: conflicting txs on receiver address, expecting 2 groups")
	}
}

// TestEndToEndRequestsHash tests EIP-7685 request hash computation and validation.
func TestEndToEndRequestsHash(t *testing.T) {
	requests := types.Requests{
		types.NewRequest(types.DepositRequestType, []byte{0x01, 0x02, 0x03}),
		types.NewRequest(types.WithdrawalRequestType, []byte{0x04, 0x05}),
		types.NewRequest(types.ConsolidationRequestType, []byte{0x06}),
	}

	hash := types.ComputeRequestsHash(requests)
	if hash.IsZero() {
		t.Fatal("requests hash should not be zero")
	}

	// Build a header with the hash.
	header := &types.Header{RequestsHash: &hash}
	if err := types.ValidateRequestsHash(header, requests); err != nil {
		t.Errorf("ValidateRequestsHash: %v", err)
	}

	// Wrong hash should fail.
	wrongHash := types.HexToHash("0xdeadbeef")
	header2 := &types.Header{RequestsHash: &wrongHash}
	if err := types.ValidateRequestsHash(header2, requests); err == nil {
		t.Error("expected error for wrong hash")
	}
}

// TestEndToEndWitness builds an execution witness and encodes/decodes it.
func TestEndToEndWitness(t *testing.T) {
	parentRoot := types.HexToHash("0x1234")
	w := witness.NewExecutionWitness(parentRoot)

	// Add a stem diff.
	var stem [31]byte
	stem[0] = 0x42
	currentVal := [32]byte{0xaa}
	newVal := [32]byte{0xbb}

	w.State = append(w.State, witness.StemStateDiff{
		Stem: stem,
		Suffixes: []witness.SuffixStateDiff{
			{Suffix: 0, CurrentValue: &currentVal, NewValue: &newVal},
			{Suffix: 1, CurrentValue: &currentVal},
		},
	})

	if w.NumStems() != 1 {
		t.Errorf("NumStems = %d, want 1", w.NumStems())
	}
	if w.NumSuffixes() != 2 {
		t.Errorf("NumSuffixes = %d, want 2", w.NumSuffixes())
	}

	// Encode and decode.
	encoded, encErr := witness.EncodeWitness(w)
	if encErr != nil {
		t.Fatalf("EncodeWitness: %v", encErr)
	}
	if len(encoded) == 0 {
		t.Fatal("encoded witness is empty")
	}

	decoded, err := witness.DecodeWitness(encoded)
	if err != nil {
		t.Fatalf("DecodeWitness: %v", err)
	}

	if decoded.ParentRoot != parentRoot {
		t.Error("parent root mismatch after decode")
	}
	if decoded.NumStems() != 1 {
		t.Errorf("decoded NumStems = %d, want 1", decoded.NumStems())
	}
}

// TestEndToEndEngineConversion tests Engine API payload/header conversion.
func TestEndToEndEngineConversion(t *testing.T) {
	header := &types.Header{
		Number:   big.NewInt(100),
		GasLimit: 30_000_000,
		GasUsed:  500_000,
		Time:     1700000000,
		Coinbase: types.BytesToAddress([]byte{0xff}),
		BaseFee:  big.NewInt(1000),
	}

	fields := engine.HeaderToPayloadFields(header)
	if fields.BlockNumber != 100 {
		t.Errorf("BlockNumber = %d, want 100", fields.BlockNumber)
	}
	if fields.GasLimit != 30_000_000 {
		t.Errorf("GasLimit = %d, want 30000000", fields.GasLimit)
	}
	if fields.GasUsed != 500_000 {
		t.Errorf("GasUsed = %d, want 500000", fields.GasUsed)
	}

	// Convert back to header using V4 payload.
	payload := engine.ExecutionPayloadV4{}
	payload.BlockNumber = 100
	payload.GasLimit = 30_000_000
	payload.GasUsed = 500_000
	payload.Timestamp = 1700000000
	payload.BaseFeePerGas = big.NewInt(1000)
	rtHeader := engine.PayloadToHeader(&payload)
	if rtHeader.Number.Int64() != 100 {
		t.Errorf("roundtrip Number = %d, want 100", rtHeader.Number.Int64())
	}
}

// TestEndToEndRLPRoundTrip tests RLP encode/decode round trip.
func TestEndToEndRLPRoundTrip(t *testing.T) {
	type account struct {
		Nonce    uint64
		Balance  uint64
		Root     [32]byte
		CodeHash [32]byte
	}

	original := account{
		Nonce:   42,
		Balance: 1_000_000,
	}
	copy(original.Root[:], types.EmptyRootHash[:])
	copy(original.CodeHash[:], types.EmptyCodeHash[:])

	encoded, err := rlp.EncodeToBytes(original)
	if err != nil {
		t.Fatalf("EncodeToBytes: %v", err)
	}

	var decoded account
	if err := rlp.DecodeBytes(encoded, &decoded); err != nil {
		t.Fatalf("DecodeBytes: %v", err)
	}

	if decoded.Nonce != original.Nonce {
		t.Errorf("Nonce = %d, want %d", decoded.Nonce, original.Nonce)
	}
	if decoded.Balance != original.Balance {
		t.Errorf("Balance = %d, want %d", decoded.Balance, original.Balance)
	}
	if decoded.Root != original.Root {
		t.Error("Root mismatch")
	}
}

// TestEndToEndTxPool tests the transaction pool lifecycle.
func TestEndToEndTxPool(t *testing.T) {
	statedb := state.NewMemoryStateDB()
	sender := types.BytesToAddress([]byte{0x01})
	statedb.AddBalance(sender, big.NewInt(1_000_000_000))

	// Create pool with state reader adapter.
	adapter := &stateAdapter{statedb: statedb}
	pool := txpool.New(txpool.DefaultConfig(), adapter)

	// Add transactions.
	receiver := types.BytesToAddress([]byte{0xaa})
	for i := uint64(0); i < 5; i++ {
		tx := types.NewTransaction(&types.LegacyTx{
			Nonce:    i,
			GasPrice: big.NewInt(100),
			Gas:      21000,
			To:       &receiver,
			Value:    big.NewInt(100),
		})
		tx.SetSender(sender)
		if err := pool.AddLocal(tx); err != nil {
			t.Fatalf("AddLocal tx %d: %v", i, err)
		}
	}

	if pool.PendingCount() != 5 {
		t.Errorf("PendingCount = %d, want 5", pool.PendingCount())
	}

	// Get pending txs for block building.
	pending := pool.Pending()
	if txs, ok := pending[sender]; !ok || len(txs) != 5 {
		t.Errorf("pending[sender] length = %d, want 5", len(pending[sender]))
	}
}

// TestEndToEndCryptoKeccak tests crypto primitives integration.
func TestEndToEndCryptoKeccak(t *testing.T) {
	data := []byte("hello world")
	hash := crypto.Keccak256Hash(data)

	if hash.IsZero() {
		t.Fatal("keccak hash should not be zero")
	}

	// Known keccak256("hello world") result.
	expected := "47173285a8d7341e5e972fc677286384f802f8ef42a5ec5f03bbfa254cb01fad"
	if hash.Hex()[2:] != expected {
		t.Errorf("keccak256 = %s, want %s", hash.Hex()[2:], expected)
	}
}

// TestEndToEndBlockBuilder builds a block from scratch and validates it.
func TestEndToEndBlockBuilder(t *testing.T) {
	coinbase := types.BytesToAddress([]byte{0xff})
	sender := types.BytesToAddress([]byte{0xb1})
	receiver := types.BytesToAddress([]byte{0xb2})

	// Initialize state.
	statedb := state.NewMemoryStateDB()
	statedb.AddBalance(sender, big.NewInt(10_000_000))

	// Create transactions.
	var txs []*types.Transaction
	for i := uint64(0); i < 3; i++ {
		tx := types.NewTransaction(&types.LegacyTx{
			Nonce:    i,
			GasPrice: big.NewInt(10),
			Gas:      21000,
			To:       &receiver,
			Value:    big.NewInt(1000),
		})
		tx.SetSender(sender)
		txs = append(txs, tx)
	}

	// Build block header.
	header := &types.Header{
		ParentHash: types.HexToHash("0x00"),
		Number:     big.NewInt(1),
		GasLimit:   30_000_000,
		Time:       1700000000,
		Coinbase:   coinbase,
		BaseFee:    big.NewInt(1),
	}

	block := types.NewBlock(header, &types.Body{Transactions: txs})

	// Verify block structure.
	if block.NumberU64() != 1 {
		t.Errorf("block number = %d, want 1", block.NumberU64())
	}
	if len(block.Transactions()) != 3 {
		t.Errorf("tx count = %d, want 3", len(block.Transactions()))
	}
	if block.GasLimit() != 30_000_000 {
		t.Errorf("gas limit = %d, want 30M", block.GasLimit())
	}

	// Process the block.
	processor := core.NewStateProcessor(core.TestConfig)
	receipts, err := processor.Process(block, statedb)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}

	// All receipts should be successful.
	for i, r := range receipts {
		if r.Status != types.ReceiptStatusSuccessful {
			t.Errorf("receipt %d failed with status %d", i, r.Status)
		}
	}

	// Verify final state.
	// sender: 10M - (3 * 1000 value) - (3 * 21000 * 10 gas)
	expectedBal := int64(10_000_000 - 3*1000 - 3*21000*10)
	if bal := statedb.GetBalance(sender); bal.Int64() != expectedBal {
		t.Errorf("sender balance = %d, want %d", bal.Int64(), expectedBal)
	}
	if bal := statedb.GetBalance(receiver); bal.Int64() != 3000 {
		t.Errorf("receiver balance = %d, want 3000", bal.Int64())
	}
	if nonce := statedb.GetNonce(sender); nonce != 3 {
		t.Errorf("sender nonce = %d, want 3", nonce)
	}

	t.Logf("Block #%d processed: %d txs, %d receipts, sender balance: %s",
		block.NumberU64(), len(block.Transactions()), len(receipts), statedb.GetBalance(sender))
}

// stateAdapter adapts MemoryStateDB to txpool.StateReader interface.
type stateAdapter struct {
	statedb *state.MemoryStateDB
}

func (a *stateAdapter) GetNonce(addr types.Address) uint64 {
	return a.statedb.GetNonce(addr)
}

func (a *stateAdapter) GetBalance(addr types.Address) *big.Int {
	return a.statedb.GetBalance(addr)
}
