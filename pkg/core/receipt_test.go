package core

import (
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/rawdb"
	"github.com/eth2030/eth2030/core/state"
	"github.com/eth2030/eth2030/core/types"
)

// testChainWithFunds creates a blockchain with a funded sender account.
func testChainWithFunds(t *testing.T) (*Blockchain, types.Address, types.Address) {
	t.Helper()
	statedb := state.NewMemoryStateDB()
	sender := types.HexToAddress("0xaaaa")
	receiver := types.HexToAddress("0xbbbb")

	// Fund sender with 100 ETH.
	hundredETH := new(big.Int).Mul(big.NewInt(100), new(big.Int).SetUint64(1e18))
	statedb.AddBalance(sender, hundredETH)

	genesis := makeGenesis(30_000_000, big.NewInt(1))
	db := rawdb.NewMemoryDB()
	bc, err := NewBlockchain(TestConfig, genesis, statedb, db)
	if err != nil {
		t.Fatalf("NewBlockchain: %v", err)
	}
	return bc, sender, receiver
}

// makeTx creates a simple value transfer transaction.
func makeTx(nonce uint64, sender, receiver types.Address, value int64) *types.Transaction {
	to := receiver
	tx := types.NewTransaction(&types.LegacyTx{
		Nonce:    nonce,
		GasPrice: big.NewInt(1_000_000_000), // Must be >= BaseFee (EIP-1559)
		Gas:      21000,
		To:       &to,
		Value:    big.NewInt(value),
	})
	tx.SetSender(sender)
	return tx
}

func TestReceiptPersistence(t *testing.T) {
	bc, sender, receiver := testChainWithFunds(t)

	// Create two transactions.
	tx1 := makeTx(0, sender, receiver, 100)
	tx2 := makeTx(1, sender, receiver, 200)

	block := makeBlockWithState(bc.Genesis(), []*types.Transaction{tx1, tx2}, bc.State())
	if err := bc.InsertBlock(block); err != nil {
		t.Fatalf("InsertBlock: %v", err)
	}

	// Verify receipts are stored and retrievable by block hash.
	blockHash := block.Hash()
	receipts := bc.GetReceipts(blockHash)
	if len(receipts) != 2 {
		t.Fatalf("expected 2 receipts, got %d", len(receipts))
	}

	// Check receipt fields are populated.
	for i, r := range receipts {
		if r.BlockHash != blockHash {
			t.Errorf("receipt[%d].BlockHash = %v, want %v", i, r.BlockHash, blockHash)
		}
		if r.BlockNumber == nil || r.BlockNumber.Uint64() != 1 {
			t.Errorf("receipt[%d].BlockNumber = %v, want 1", i, r.BlockNumber)
		}
		if r.TransactionIndex != uint(i) {
			t.Errorf("receipt[%d].TransactionIndex = %d, want %d", i, r.TransactionIndex, i)
		}
		if r.Status != types.ReceiptStatusSuccessful {
			t.Errorf("receipt[%d].Status = %d, want %d", i, r.Status, types.ReceiptStatusSuccessful)
		}
		if r.GasUsed == 0 {
			t.Errorf("receipt[%d].GasUsed = 0, expected non-zero", i)
		}
	}

	// Verify GetBlockReceipts by number also works.
	receipts2 := bc.GetBlockReceipts(1)
	if len(receipts2) != 2 {
		t.Fatalf("GetBlockReceipts(1) = %d receipts, want 2", len(receipts2))
	}

	// Non-existent block returns nil.
	if r := bc.GetReceipts(types.Hash{0xff}); r != nil {
		t.Errorf("expected nil receipts for unknown hash, got %d", len(r))
	}
	if r := bc.GetBlockReceipts(999); r != nil {
		t.Errorf("expected nil receipts for unknown number, got %d", len(r))
	}
}

func TestTransactionLookup(t *testing.T) {
	bc, sender, receiver := testChainWithFunds(t)

	tx1 := makeTx(0, sender, receiver, 100)
	tx2 := makeTx(1, sender, receiver, 200)

	block := makeBlockWithState(bc.Genesis(), []*types.Transaction{tx1, tx2}, bc.State())
	if err := bc.InsertBlock(block); err != nil {
		t.Fatalf("InsertBlock: %v", err)
	}

	blockHash := block.Hash()

	// Look up first transaction.
	bh, bn, idx, found := bc.GetTransactionLookup(tx1.Hash())
	if !found {
		t.Fatal("tx1 not found in lookup")
	}
	if bh != blockHash {
		t.Errorf("tx1 blockHash = %v, want %v", bh, blockHash)
	}
	if bn != 1 {
		t.Errorf("tx1 blockNumber = %d, want 1", bn)
	}
	if idx != 0 {
		t.Errorf("tx1 txIndex = %d, want 0", idx)
	}

	// Look up second transaction.
	bh, bn, idx, found = bc.GetTransactionLookup(tx2.Hash())
	if !found {
		t.Fatal("tx2 not found in lookup")
	}
	if bh != blockHash {
		t.Errorf("tx2 blockHash = %v, want %v", bh, blockHash)
	}
	if bn != 1 {
		t.Errorf("tx2 blockNumber = %d, want 1", bn)
	}
	if idx != 1 {
		t.Errorf("tx2 txIndex = %d, want 1", idx)
	}

	// Unknown tx hash should not be found.
	_, _, _, found = bc.GetTransactionLookup(types.Hash{0xde, 0xad})
	if found {
		t.Error("expected unknown tx to not be found")
	}
}

func TestGetLogs(t *testing.T) {
	bc, sender, receiver := testChainWithFunds(t)

	// Insert two blocks with transactions.
	tx1 := makeTx(0, sender, receiver, 100)
	block1 := makeBlockWithState(bc.Genesis(), []*types.Transaction{tx1}, bc.State())
	if err := bc.InsertBlock(block1); err != nil {
		t.Fatalf("InsertBlock(1): %v", err)
	}

	tx2 := makeTx(1, sender, receiver, 200)
	block2 := makeBlockWithState(block1, []*types.Transaction{tx2}, bc.State())
	if err := bc.InsertBlock(block2); err != nil {
		t.Fatalf("InsertBlock(2): %v", err)
	}

	// For simple value transfers, logs should be empty.
	logs1 := bc.GetLogs(block1.Hash())
	if len(logs1) != 0 {
		t.Errorf("expected 0 logs for simple transfer, got %d", len(logs1))
	}

	logs2 := bc.GetLogs(block2.Hash())
	if len(logs2) != 0 {
		t.Errorf("expected 0 logs for simple transfer, got %d", len(logs2))
	}

	// Non-existent block returns nil.
	if l := bc.GetLogs(types.Hash{0xff}); l != nil {
		t.Errorf("expected nil logs for unknown hash, got %d", len(l))
	}
}

func TestGetLogsWithContract(t *testing.T) {
	statedb := state.NewMemoryStateDB()
	sender := types.HexToAddress("0xaaaa")
	contractAddr := types.HexToAddress("0xcccc")

	// Fund sender.
	hundredETH := new(big.Int).Mul(big.NewInt(100), new(big.Int).SetUint64(1e18))
	statedb.AddBalance(sender, hundredETH)

	// Deploy a contract that emits a LOG0.
	// Bytecode: PUSH1 0x20, PUSH1 0x00, LOG0, STOP
	// This logs 32 bytes from memory[0:32].
	logCode := []byte{
		0x60, 0x20, // PUSH1 0x20 (size = 32)
		0x60, 0x00, // PUSH1 0x00 (offset = 0)
		0xa0, // LOG0
		0x00, // STOP
	}
	statedb.CreateAccount(contractAddr)
	statedb.SetCode(contractAddr, logCode)

	genesis := makeGenesis(30_000_000, big.NewInt(1))
	db := rawdb.NewMemoryDB()
	bc, err := NewBlockchain(TestConfig, genesis, statedb, db)
	if err != nil {
		t.Fatalf("NewBlockchain: %v", err)
	}

	// Call the contract. Gas price must be >= base fee (7 wei minimum).
	tx := types.NewTransaction(&types.LegacyTx{
		Nonce:    0,
		GasPrice: big.NewInt(100),
		Gas:      100000,
		To:       &contractAddr,
		Value:    big.NewInt(0),
	})
	tx.SetSender(sender)

	// Use BlockBuilder to construct the block so the bloom filter is computed
	// correctly from the receipts' logs.
	pool := &mockTxPool{txs: []*types.Transaction{tx}}
	builder := NewBlockBuilder(TestConfig, bc, pool)
	attrs := &BuildBlockAttributes{
		Timestamp:    genesis.Time() + 12,
		FeeRecipient: types.HexToAddress("0xfee"),
		GasLimit:     30_000_000,
	}
	block, _, err := builder.BuildBlock(genesis.Header(), attrs)
	if err != nil {
		t.Fatalf("BuildBlock: %v", err)
	}

	if err := bc.InsertBlock(block); err != nil {
		t.Fatalf("InsertBlock: %v", err)
	}

	// Verify logs were collected.
	logs := bc.GetLogs(block.Hash())
	if len(logs) != 1 {
		t.Fatalf("expected 1 log, got %d", len(logs))
	}

	log := logs[0]
	if log.Address != contractAddr {
		t.Errorf("log address = %v, want %v", log.Address, contractAddr)
	}
	if log.BlockHash != block.Hash() {
		t.Errorf("log BlockHash = %v, want %v", log.BlockHash, block.Hash())
	}
	if log.BlockNumber != 1 {
		t.Errorf("log BlockNumber = %d, want 1", log.BlockNumber)
	}
	if log.TxIndex != 0 {
		t.Errorf("log TxIndex = %d, want 0", log.TxIndex)
	}
	if log.Index != 0 {
		t.Errorf("log Index = %d, want 0", log.Index)
	}

	// Receipts should have the log too.
	receipts := bc.GetReceipts(block.Hash())
	if len(receipts) != 1 {
		t.Fatalf("expected 1 receipt, got %d", len(receipts))
	}
	if len(receipts[0].Logs) != 1 {
		t.Fatalf("expected 1 log in receipt, got %d", len(receipts[0].Logs))
	}

	// Verify the bloom filter in the block header contains the contract address.
	blockBloom := block.Bloom()
	if !types.BloomContains(blockBloom, contractAddr.Bytes()) {
		t.Error("block bloom should contain the contract address from LOG0")
	}
}

func TestBloomFilter(t *testing.T) {
	addr1 := types.HexToAddress("0x1111111111111111111111111111111111111111")
	addr2 := types.HexToAddress("0x2222222222222222222222222222222222222222")
	topic1 := types.HexToHash("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	topic2 := types.HexToHash("0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	topic3 := types.HexToHash("0xcccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc")

	// Create a bloom filter from a log.
	log := &types.Log{
		Address: addr1,
		Topics:  []types.Hash{topic1, topic2},
	}
	bloom := types.LogsBloom([]*types.Log{log})

	// Test address matching.
	if !BloomMatchesAddresses(bloom, []types.Address{addr1}) {
		t.Error("bloom should match addr1")
	}
	if BloomMatchesAddresses(bloom, []types.Address{addr2}) {
		t.Error("bloom should not match addr2")
	}
	// Empty address filter matches everything.
	if !BloomMatchesAddresses(bloom, nil) {
		t.Error("empty address filter should match")
	}

	// Test topic matching.
	if !BloomMatchesTopics(bloom, [][]types.Hash{{topic1}}) {
		t.Error("bloom should match topic1")
	}
	if !BloomMatchesTopics(bloom, [][]types.Hash{{topic1}, {topic2}}) {
		t.Error("bloom should match topic1 AND topic2")
	}
	if BloomMatchesTopics(bloom, [][]types.Hash{{topic3}}) {
		t.Error("bloom should not match topic3")
	}
	// Wildcard position.
	if !BloomMatchesTopics(bloom, [][]types.Hash{{}, {topic2}}) {
		t.Error("bloom should match wildcard + topic2")
	}
	// Empty topics matches everything.
	if !BloomMatchesTopics(bloom, nil) {
		t.Error("empty topics filter should match")
	}

	// Test combined filter.
	if !BloomMatchesFilter(bloom, []types.Address{addr1}, [][]types.Hash{{topic1}}) {
		t.Error("combined filter should match")
	}
	if BloomMatchesFilter(bloom, []types.Address{addr2}, [][]types.Hash{{topic1}}) {
		t.Error("combined filter should not match wrong address")
	}
	if BloomMatchesFilter(bloom, []types.Address{addr1}, [][]types.Hash{{topic3}}) {
		t.Error("combined filter should not match wrong topic")
	}

	// Test OR within a topic position.
	if !BloomMatchesTopics(bloom, [][]types.Hash{{topic1, topic3}}) {
		t.Error("bloom should match topic1 OR topic3 at position 0")
	}
	if BloomMatchesTopics(bloom, [][]types.Hash{{topic3}, {topic2}}) {
		t.Error("bloom should not match: topic3 at position 0 fails")
	}
}

func TestReceiptMultipleBlocks(t *testing.T) {
	bc, sender, receiver := testChainWithFunds(t)

	// Insert block 1 with one tx.
	tx1 := makeTx(0, sender, receiver, 100)
	block1 := makeBlockWithState(bc.Genesis(), []*types.Transaction{tx1}, bc.State())
	if err := bc.InsertBlock(block1); err != nil {
		t.Fatalf("InsertBlock(1): %v", err)
	}

	// Insert block 2 with two txs.
	tx2 := makeTx(1, sender, receiver, 200)
	tx3 := makeTx(2, sender, receiver, 300)
	block2 := makeBlockWithState(block1, []*types.Transaction{tx2, tx3}, bc.State())
	if err := bc.InsertBlock(block2); err != nil {
		t.Fatalf("InsertBlock(2): %v", err)
	}

	// Block 1 should have 1 receipt.
	r1 := bc.GetReceipts(block1.Hash())
	if len(r1) != 1 {
		t.Fatalf("block1: expected 1 receipt, got %d", len(r1))
	}
	if r1[0].TxHash != tx1.Hash() {
		t.Errorf("block1 receipt TxHash mismatch")
	}

	// Block 2 should have 2 receipts.
	r2 := bc.GetReceipts(block2.Hash())
	if len(r2) != 2 {
		t.Fatalf("block2: expected 2 receipts, got %d", len(r2))
	}
	if r2[0].TxHash != tx2.Hash() {
		t.Errorf("block2 receipt[0] TxHash mismatch")
	}
	if r2[1].TxHash != tx3.Hash() {
		t.Errorf("block2 receipt[1] TxHash mismatch")
	}

	// All three txs should be findable via lookup.
	for i, tx := range []*types.Transaction{tx1, tx2, tx3} {
		_, _, _, found := bc.GetTransactionLookup(tx.Hash())
		if !found {
			t.Errorf("tx[%d] not found in lookup", i)
		}
	}
}
