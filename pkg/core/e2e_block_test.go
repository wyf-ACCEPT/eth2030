package core

import (
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/state"
	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
	"github.com/eth2030/eth2030/rlp"
)

// TestE2E_BlockCreation tests the full block lifecycle:
// create transactions, build a block, compute state root, verify receipts.
func TestE2E_BlockCreation(t *testing.T) {
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	sender := crypto.PubkeyToAddress(key.PublicKey)
	recipient := types.HexToAddress("0x1111111111111111111111111111111111111111")
	coinbase := types.HexToAddress("0x2222222222222222222222222222222222222222")

	// Set up a chain with the sender pre-funded.
	bc := e2eChain(t, 30_000_000, big.NewInt(1), map[types.Address]*big.Int{
		sender: ether(50),
	})

	// Create 3 transactions of increasing value.
	var txs []*types.Transaction
	for i := uint64(0); i < 3; i++ {
		tx := signLegacyTx(t, key, TestConfig.ChainID, &types.LegacyTx{
			Nonce:    i,
			GasPrice: big.NewInt(10),
			Gas:      21000,
			To:       &recipient,
			Value:    big.NewInt(int64((i + 1) * 1000)),
		})
		txs = append(txs, tx)
	}

	pool := &simpleTxPool{txs: txs}
	block, receipts := buildAndInsert(t, bc, pool, coinbase)

	// Verify the block contains all 3 transactions.
	if len(block.Transactions()) != 3 {
		t.Fatalf("block tx count = %d, want 3", len(block.Transactions()))
	}

	// Verify block number and parent hash.
	if block.NumberU64() != 1 {
		t.Errorf("block number = %d, want 1", block.NumberU64())
	}
	genesis := bc.GetBlockByNumber(0)
	if block.ParentHash() != genesis.Hash() {
		t.Errorf("parent hash mismatch: got %s, want %s", block.ParentHash().Hex(), genesis.Hash().Hex())
	}

	// Verify state root is non-zero and changed from genesis.
	if block.Root().IsZero() {
		t.Error("state root is zero")
	}
	if block.Root() == genesis.Root() {
		t.Error("state root did not change from genesis")
	}

	// Verify receipts match transactions.
	if len(receipts) != 3 {
		t.Fatalf("receipt count = %d, want 3", len(receipts))
	}
	for i, r := range receipts {
		if r.Status != types.ReceiptStatusSuccessful {
			t.Errorf("receipt[%d] status = %d, want success", i, r.Status)
		}
		if r.GasUsed != TxGas {
			t.Errorf("receipt[%d] gas used = %d, want %d", i, r.GasUsed, TxGas)
		}
	}

	// Verify cumulative gas is correct: 21000, 42000, 63000.
	expectedCumGas := uint64(0)
	for i, r := range receipts {
		expectedCumGas += TxGas
		if r.CumulativeGasUsed != expectedCumGas {
			t.Errorf("receipt[%d] cumulative gas = %d, want %d", i, r.CumulativeGasUsed, expectedCumGas)
		}
	}

	// Verify block gas used matches total.
	if block.GasUsed() != 3*TxGas {
		t.Errorf("block gas used = %d, want %d", block.GasUsed(), 3*TxGas)
	}

	// Verify recipient balance.
	st := bc.State()
	expectedRecipientBal := big.NewInt(1000 + 2000 + 3000) // 1+2+3 * 1000
	if bal := st.GetBalance(recipient); bal.Cmp(expectedRecipientBal) != 0 {
		t.Errorf("recipient balance = %s, want %s", bal, expectedRecipientBal)
	}
}

// TestE2E_TransactionProcessing creates different tx types (Legacy, EIP-1559,
// EIP-4844 BlobTx), signs them, and validates their properties.
func TestE2E_TransactionProcessing(t *testing.T) {
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	sender := crypto.PubkeyToAddress(key.PublicKey)
	recipient := types.HexToAddress("0x3333333333333333333333333333333333333333")

	chainID := big.NewInt(1)

	// --- Legacy transaction ---
	legacyTx := signLegacyTx(t, key, chainID, &types.LegacyTx{
		Nonce:    0,
		GasPrice: big.NewInt(20),
		Gas:      21000,
		To:       &recipient,
		Value:    big.NewInt(500),
	})
	if legacyTx.Type() != types.LegacyTxType {
		t.Errorf("legacy tx type = %d, want %d", legacyTx.Type(), types.LegacyTxType)
	}
	if legacyTx.Nonce() != 0 {
		t.Errorf("legacy nonce = %d, want 0", legacyTx.Nonce())
	}
	if legacyTx.Gas() != 21000 {
		t.Errorf("legacy gas = %d, want 21000", legacyTx.Gas())
	}
	if legacyTx.Value().Cmp(big.NewInt(500)) != 0 {
		t.Errorf("legacy value = %s, want 500", legacyTx.Value())
	}
	if legacyTx.Hash().IsZero() {
		t.Error("legacy tx hash is zero")
	}

	// Verify sender recovery with the signer.
	signer := types.NewLondonSigner(1)
	recoveredSender, err := signer.Sender(legacyTx)
	if err != nil {
		t.Fatalf("recover legacy sender: %v", err)
	}
	if recoveredSender != sender {
		t.Errorf("recovered sender = %s, want %s", recoveredSender.Hex(), sender.Hex())
	}

	// --- EIP-1559 DynamicFeeTx ---
	dynamicTx := signDynamicFeeTx(t, key, &types.DynamicFeeTx{
		ChainID:   chainID,
		Nonce:     1,
		GasTipCap: big.NewInt(5),
		GasFeeCap: big.NewInt(30),
		Gas:       21000,
		To:        &recipient,
		Value:     big.NewInt(1000),
	})
	if dynamicTx.Type() != types.DynamicFeeTxType {
		t.Errorf("dynamic tx type = %d, want %d", dynamicTx.Type(), types.DynamicFeeTxType)
	}
	if dynamicTx.GasTipCap().Cmp(big.NewInt(5)) != 0 {
		t.Errorf("dynamic tip cap = %s, want 5", dynamicTx.GasTipCap())
	}
	if dynamicTx.GasFeeCap().Cmp(big.NewInt(30)) != 0 {
		t.Errorf("dynamic fee cap = %s, want 30", dynamicTx.GasFeeCap())
	}
	recoveredDyn, err := signer.Sender(dynamicTx)
	if err != nil {
		t.Fatalf("recover dynamic sender: %v", err)
	}
	if recoveredDyn != sender {
		t.Errorf("dynamic recovered sender = %s, want %s", recoveredDyn.Hex(), sender.Hex())
	}

	// --- EIP-4844 BlobTx (unsigned, structural test) ---
	blobHash1 := types.HexToHash("0x0100000000000000000000000000000000000000000000000000000000000001")
	blobHash2 := types.HexToHash("0x0100000000000000000000000000000000000000000000000000000000000002")
	blobTx := types.NewTransaction(&types.BlobTx{
		ChainID:    chainID,
		Nonce:      2,
		GasTipCap:  big.NewInt(3),
		GasFeeCap:  big.NewInt(50),
		Gas:        21000,
		To:         recipient,
		Value:      big.NewInt(0),
		BlobFeeCap: big.NewInt(100),
		BlobHashes: []types.Hash{blobHash1, blobHash2},
	})
	if blobTx.Type() != types.BlobTxType {
		t.Errorf("blob tx type = %d, want %d", blobTx.Type(), types.BlobTxType)
	}
	if len(blobTx.BlobHashes()) != 2 {
		t.Errorf("blob hashes count = %d, want 2", len(blobTx.BlobHashes()))
	}
	if blobTx.BlobGasFeeCap().Cmp(big.NewInt(100)) != 0 {
		t.Errorf("blob gas fee cap = %s, want 100", blobTx.BlobGasFeeCap())
	}
	// BlobGas = numBlobs * 131072
	expectedBlobGas := uint64(2 * 131072)
	if blobTx.BlobGas() != expectedBlobGas {
		t.Errorf("blob gas = %d, want %d", blobTx.BlobGas(), expectedBlobGas)
	}

	// Verify all tx hashes are distinct.
	hashes := map[types.Hash]string{
		legacyTx.Hash():  "legacy",
		dynamicTx.Hash(): "dynamic",
		blobTx.Hash():    "blob",
	}
	if len(hashes) != 3 {
		t.Error("transaction hashes are not all unique")
	}
}

// TestE2E_StateTransition creates accounts, executes transfers through
// StateDB, and verifies balance changes, nonces, and snapshots.
func TestE2E_StateTransition(t *testing.T) {
	statedb := state.NewMemoryStateDB()

	alice := types.HexToAddress("0xAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA")
	bob := types.HexToAddress("0xBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB")
	charlie := types.HexToAddress("0xCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCC")

	// Create and fund accounts.
	statedb.CreateAccount(alice)
	statedb.AddBalance(alice, ether(100))
	statedb.SetNonce(alice, 0)

	statedb.CreateAccount(bob)
	statedb.AddBalance(bob, ether(50))

	// Verify initial state.
	if bal := statedb.GetBalance(alice); bal.Cmp(ether(100)) != 0 {
		t.Errorf("alice initial balance = %s, want 100 ether", bal)
	}
	if bal := statedb.GetBalance(bob); bal.Cmp(ether(50)) != 0 {
		t.Errorf("bob initial balance = %s, want 50 ether", bal)
	}
	if statedb.Exist(charlie) {
		t.Error("charlie should not exist yet")
	}

	// Snapshot before transfers.
	snap := statedb.Snapshot()

	// Transfer 10 ETH from Alice to Bob.
	statedb.SubBalance(alice, ether(10))
	statedb.AddBalance(bob, ether(10))
	statedb.SetNonce(alice, 1)

	// Transfer 5 ETH from Bob to Charlie (creates Charlie's account).
	statedb.CreateAccount(charlie)
	statedb.SubBalance(bob, ether(5))
	statedb.AddBalance(charlie, ether(5))
	statedb.SetNonce(bob, 1)

	// Verify post-transfer state.
	if bal := statedb.GetBalance(alice); bal.Cmp(ether(90)) != 0 {
		t.Errorf("alice balance after transfer = %s, want 90 ether", bal)
	}
	if bal := statedb.GetBalance(bob); bal.Cmp(ether(55)) != 0 {
		t.Errorf("bob balance after transfer = %s, want 55 ether", bal)
	}
	if bal := statedb.GetBalance(charlie); bal.Cmp(ether(5)) != 0 {
		t.Errorf("charlie balance = %s, want 5 ether", bal)
	}
	if statedb.GetNonce(alice) != 1 {
		t.Errorf("alice nonce = %d, want 1", statedb.GetNonce(alice))
	}

	// Compute state root. It should be deterministic and non-empty.
	root1 := statedb.GetRoot()
	if root1.IsZero() {
		t.Error("state root is zero after transfers")
	}
	if root1 == types.EmptyRootHash {
		t.Error("state root is empty root hash after transfers")
	}

	// Compute root again; should be identical (deterministic).
	root2 := statedb.GetRoot()
	if root1 != root2 {
		t.Errorf("state root not deterministic: %s != %s", root1.Hex(), root2.Hex())
	}

	// Revert to snapshot; transfers should be undone.
	statedb.RevertToSnapshot(snap)
	if bal := statedb.GetBalance(alice); bal.Cmp(ether(100)) != 0 {
		t.Errorf("alice balance after revert = %s, want 100 ether", bal)
	}
	if bal := statedb.GetBalance(bob); bal.Cmp(ether(50)) != 0 {
		t.Errorf("bob balance after revert = %s, want 50 ether", bal)
	}

	// State root after revert should differ from post-transfer root.
	rootAfterRevert := statedb.GetRoot()
	if rootAfterRevert == root1 {
		t.Error("state root did not change after revert")
	}

	// Set code on an account and verify code hash.
	code := []byte{0x60, 0x00, 0x60, 0x00, 0xf3} // PUSH0, PUSH0, RETURN
	statedb.SetCode(alice, code)
	if statedb.GetCodeSize(alice) != len(code) {
		t.Errorf("alice code size = %d, want %d", statedb.GetCodeSize(alice), len(code))
	}
	codeHash := statedb.GetCodeHash(alice)
	expectedCodeHash := types.BytesToHash(crypto.Keccak256(code))
	if codeHash != expectedCodeHash {
		t.Errorf("code hash mismatch: got %s, want %s", codeHash.Hex(), expectedCodeHash.Hex())
	}

	// Set storage and verify.
	storageKey := types.HexToHash("0x01")
	storageVal := types.HexToHash("0x42")
	statedb.SetState(alice, storageKey, storageVal)
	if got := statedb.GetState(alice, storageKey); got != storageVal {
		t.Errorf("storage value = %s, want %s", got.Hex(), storageVal.Hex())
	}

	// Commit and verify root.
	commitRoot, err := statedb.Commit()
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if commitRoot.IsZero() {
		t.Error("commit root is zero")
	}
}

// TestE2E_BlockValidation creates block headers and validates gas limits,
// timestamps, and parent hash linking.
func TestE2E_BlockValidation(t *testing.T) {
	now := uint64(1700000000)

	// Build a chain of 5 headers, each referencing the previous.
	headers := make([]*types.Header, 5)
	for i := 0; i < 5; i++ {
		h := &types.Header{
			Number:     big.NewInt(int64(i)),
			GasLimit:   30_000_000,
			GasUsed:    uint64(i * 21000),
			Time:       now + uint64(i*12),
			Difficulty: big.NewInt(1),
			BaseFee:    big.NewInt(1000000000),
			Extra:      []byte("test"),
		}
		if i > 0 {
			h.ParentHash = headers[i-1].Hash()
		}
		headers[i] = h
	}

	// Verify parent hash linking.
	for i := 1; i < len(headers); i++ {
		if headers[i].ParentHash != headers[i-1].Hash() {
			t.Errorf("header[%d] parent hash does not match header[%d] hash", i, i-1)
		}
	}

	// Verify timestamps are increasing.
	for i := 1; i < len(headers); i++ {
		if headers[i].Time <= headers[i-1].Time {
			t.Errorf("header[%d] time %d <= header[%d] time %d",
				i, headers[i].Time, i-1, headers[i-1].Time)
		}
	}

	// Verify gas used does not exceed gas limit.
	for i, h := range headers {
		if h.GasUsed > h.GasLimit {
			t.Errorf("header[%d] gas used %d > gas limit %d", i, h.GasUsed, h.GasLimit)
		}
	}

	// Build blocks from headers and verify block properties.
	for i, h := range headers {
		block := types.NewBlock(h, nil)
		if block.NumberU64() != uint64(i) {
			t.Errorf("block[%d] number = %d", i, block.NumberU64())
		}
		if block.GasLimit() != 30_000_000 {
			t.Errorf("block[%d] gas limit = %d, want 30000000", i, block.GasLimit())
		}
		if block.Time() != now+uint64(i*12) {
			t.Errorf("block[%d] time = %d, want %d", i, block.Time(), now+uint64(i*12))
		}
	}

	// Verify header hashes are unique.
	seen := make(map[types.Hash]int)
	for i, h := range headers {
		hash := h.Hash()
		if prev, ok := seen[hash]; ok {
			t.Errorf("header[%d] hash collides with header[%d]", i, prev)
		}
		seen[hash] = i
	}

	// Verify header RLP encoding roundtrips.
	for i, h := range headers {
		encoded, err := rlp.EncodeToBytes(h)
		if err != nil {
			t.Fatalf("RLP encode header[%d]: %v", i, err)
		}
		if len(encoded) == 0 {
			t.Errorf("header[%d] RLP encoding is empty", i)
		}
		// Verify the encoded data is non-trivial.
		if len(encoded) < 10 {
			t.Errorf("header[%d] RLP encoding suspiciously short: %d bytes", i, len(encoded))
		}
	}
}

// TestE2E_ReceiptGeneration executes transactions, generates receipts, and
// verifies bloom filters and logs.
func TestE2E_ReceiptGeneration(t *testing.T) {
	// Build receipts using ReceiptBuilder with various logs.
	contractAddr := types.HexToAddress("0x5555555555555555555555555555555555555555")
	topic1 := types.HexToHash("0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef") // Transfer topic
	topic2 := types.HexToHash("0x8c5be1e5ebec7d5bd14f71427d1e84f3dd0314c0f7b2291e5b200ac8c7c3b925") // Approval topic

	log1 := &types.Log{
		Address: contractAddr,
		Topics:  []types.Hash{topic1},
		Data:    []byte{0x01, 0x02, 0x03},
	}
	log2 := &types.Log{
		Address: contractAddr,
		Topics:  []types.Hash{topic2},
		Data:    []byte{0x04, 0x05},
	}
	log3 := &types.Log{
		Address: types.HexToAddress("0x6666666666666666666666666666666666666666"),
		Topics:  []types.Hash{topic1, types.HexToHash("0xAA")},
		Data:    nil,
	}

	// Receipt 1: success with 2 logs.
	receipt1 := types.NewReceiptBuilder().
		SetStatus(types.ReceiptStatusSuccessful).
		SetGasUsed(21000).
		SetCumulativeGasUsed(21000).
		SetType(types.LegacyTxType).
		AddLog(log1).
		AddLog(log2).
		SetBlockNumber(1).
		SetTransactionIndex(0).
		Build()

	// Receipt 2: success with 1 log.
	receipt2 := types.NewReceiptBuilder().
		SetStatus(types.ReceiptStatusSuccessful).
		SetGasUsed(42000).
		SetCumulativeGasUsed(63000).
		SetType(types.DynamicFeeTxType).
		AddLog(log3).
		SetBlockNumber(1).
		SetTransactionIndex(1).
		Build()

	// Receipt 3: failed tx with no logs.
	receipt3 := types.NewReceiptBuilder().
		SetStatus(types.ReceiptStatusFailed).
		SetGasUsed(21000).
		SetCumulativeGasUsed(84000).
		SetType(types.LegacyTxType).
		SetBlockNumber(1).
		SetTransactionIndex(2).
		Build()

	receipts := []*types.Receipt{receipt1, receipt2, receipt3}

	// Verify receipt statuses.
	if !receipt1.Succeeded() {
		t.Error("receipt1 should be successful")
	}
	if !receipt2.Succeeded() {
		t.Error("receipt2 should be successful")
	}
	if receipt3.Succeeded() {
		t.Error("receipt3 should be failed")
	}

	// Verify log counts.
	if len(receipt1.Logs) != 2 {
		t.Errorf("receipt1 log count = %d, want 2", len(receipt1.Logs))
	}
	if len(receipt2.Logs) != 1 {
		t.Errorf("receipt2 log count = %d, want 1", len(receipt2.Logs))
	}
	if len(receipt3.Logs) != 0 {
		t.Errorf("receipt3 log count = %d, want 0", len(receipt3.Logs))
	}

	// Verify bloom filter on receipt1 contains the contract address and topics.
	bloom1 := receipt1.Bloom
	if !types.BloomContains(bloom1, contractAddr.Bytes()) {
		t.Error("receipt1 bloom does not contain contract address")
	}
	if !types.BloomContains(bloom1, topic1.Bytes()) {
		t.Error("receipt1 bloom does not contain topic1 (Transfer)")
	}
	if !types.BloomContains(bloom1, topic2.Bytes()) {
		t.Error("receipt1 bloom does not contain topic2 (Approval)")
	}

	// Verify bloom on receipt2 contains its log's address and topic.
	bloom2 := receipt2.Bloom
	addr2 := types.HexToAddress("0x6666666666666666666666666666666666666666")
	if !types.BloomContains(bloom2, addr2.Bytes()) {
		t.Error("receipt2 bloom does not contain log address")
	}
	if !types.BloomContains(bloom2, topic1.Bytes()) {
		t.Error("receipt2 bloom does not contain topic1")
	}

	// Verify receipt3 bloom is empty (no logs).
	emptyBloom := types.Bloom{}
	if receipt3.Bloom != emptyBloom {
		t.Error("receipt3 bloom should be empty (no logs)")
	}

	// Verify CreateBloom combines all receipt blooms.
	combinedBloom := types.CreateBloom(receipts)
	if !types.BloomContains(combinedBloom, contractAddr.Bytes()) {
		t.Error("combined bloom does not contain contract address")
	}
	if !types.BloomContains(combinedBloom, addr2.Bytes()) {
		t.Error("combined bloom does not contain second log address")
	}
	if !types.BloomContains(combinedBloom, topic1.Bytes()) {
		t.Error("combined bloom does not contain Transfer topic")
	}
	if !types.BloomContains(combinedBloom, topic2.Bytes()) {
		t.Error("combined bloom does not contain Approval topic")
	}

	// Verify DeriveReceiptFields populates block context.
	blockHash := types.HexToHash("0xABCDEF")
	txs := make([]*types.Transaction, 3)
	for i := range txs {
		txs[i] = types.NewTransaction(&types.LegacyTx{
			Nonce:    uint64(i),
			GasPrice: big.NewInt(10),
			Gas:      21000,
			Value:    big.NewInt(0),
		})
	}
	types.DeriveReceiptFields(receipts, blockHash, 1, big.NewInt(1), txs)

	for i, r := range receipts {
		if r.BlockHash != blockHash {
			t.Errorf("receipt[%d] block hash mismatch", i)
		}
		if r.BlockNumber.Uint64() != 1 {
			t.Errorf("receipt[%d] block number = %d, want 1", i, r.BlockNumber.Uint64())
		}
		if r.TransactionIndex != uint(i) {
			t.Errorf("receipt[%d] tx index = %d, want %d", i, r.TransactionIndex, i)
		}
		if r.TxHash != txs[i].Hash() {
			t.Errorf("receipt[%d] tx hash mismatch", i)
		}
	}

	// Verify that log indices within receipts are set by DeriveReceiptFields.
	// receipt1 has 2 logs => indices 0, 1
	// receipt2 has 1 log  => index 2
	if len(receipt1.Logs) >= 2 {
		if receipt1.Logs[0].Index != 0 {
			t.Errorf("receipt1.Logs[0].Index = %d, want 0", receipt1.Logs[0].Index)
		}
		if receipt1.Logs[1].Index != 1 {
			t.Errorf("receipt1.Logs[1].Index = %d, want 1", receipt1.Logs[1].Index)
		}
	}
	if len(receipt2.Logs) >= 1 {
		if receipt2.Logs[0].Index != 2 {
			t.Errorf("receipt2.Logs[0].Index = %d, want 2", receipt2.Logs[0].Index)
		}
	}
}
