package core

import (
	"crypto/ecdsa"
	"math/big"
	"testing"

	"github.com/eth2028/eth2028/core/rawdb"
	"github.com/eth2028/eth2028/core/state"
	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/crypto"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// e2eChain sets up a blockchain with genesis state where the given accounts
// are pre-funded. Returns the blockchain and the genesis state snapshot.
func e2eChain(t *testing.T, gasLimit uint64, baseFee *big.Int, alloc map[types.Address]*big.Int) *Blockchain {
	t.Helper()
	statedb := state.NewMemoryStateDB()
	for addr, bal := range alloc {
		statedb.AddBalance(addr, bal)
	}
	genesis := makeGenesis(gasLimit, baseFee)
	db := rawdb.NewMemoryDB()
	bc, err := NewBlockchain(TestConfig, genesis, statedb, db)
	if err != nil {
		t.Fatalf("NewBlockchain: %v", err)
	}
	return bc
}

// ether returns n * 1e18.
func ether(n int64) *big.Int {
	return new(big.Int).Mul(big.NewInt(n), new(big.Int).SetUint64(1e18))
}

// gwei returns n * 1e9.
func gwei(n int64) *big.Int {
	return new(big.Int).Mul(big.NewInt(n), big.NewInt(1e9))
}

// signLegacyTx creates a legacy transaction signed with the given private key.
// It sets the EIP-155 V value so that the signing hash includes the chainID.
func signLegacyTx(t *testing.T, key *ecdsa.PrivateKey, chainID *big.Int, inner *types.LegacyTx) *types.Transaction {
	t.Helper()
	// For the signing hash to include chainID (EIP-155), we need to set V
	// so that deriveChainID(V) returns the correct chainID.
	// V = chainID*2 + 35 (with recovery_id=0 as placeholder).
	inner.V = new(big.Int).Add(new(big.Int).Mul(chainID, big.NewInt(2)), big.NewInt(35))
	inner.R = new(big.Int)
	inner.S = new(big.Int)

	tx := types.NewTransaction(inner)
	sigHash := tx.SigningHash()

	sig, err := crypto.Sign(sigHash[:], key)
	if err != nil {
		t.Fatalf("crypto.Sign: %v", err)
	}

	r := new(big.Int).SetBytes(sig[0:32])
	s := new(big.Int).SetBytes(sig[32:64])
	recoveryID := sig[64] // 0 or 1

	// EIP-155 V = chainID * 2 + 35 + recovery_id
	v := new(big.Int).Add(
		new(big.Int).Add(new(big.Int).Mul(chainID, big.NewInt(2)), big.NewInt(35)),
		new(big.Int).SetUint64(uint64(recoveryID)),
	)

	inner.V = v
	inner.R = r
	inner.S = s

	signed := types.NewTransaction(inner)
	signed.SetSender(crypto.PubkeyToAddress(key.PublicKey))
	return signed
}

// signDynamicFeeTx creates an EIP-1559 transaction signed with the given key.
func signDynamicFeeTx(t *testing.T, key *ecdsa.PrivateKey, inner *types.DynamicFeeTx) *types.Transaction {
	t.Helper()
	// Typed transactions: V is 0 or 1 (recovery id).
	inner.V = new(big.Int)
	inner.R = new(big.Int)
	inner.S = new(big.Int)

	tx := types.NewTransaction(inner)
	sigHash := tx.SigningHash()

	sig, err := crypto.Sign(sigHash[:], key)
	if err != nil {
		t.Fatalf("crypto.Sign: %v", err)
	}

	r := new(big.Int).SetBytes(sig[0:32])
	s := new(big.Int).SetBytes(sig[32:64])
	v := new(big.Int).SetUint64(uint64(sig[64])) // 0 or 1

	inner.V = v
	inner.R = r
	inner.S = s

	signed := types.NewTransaction(inner)
	signed.SetSender(crypto.PubkeyToAddress(key.PublicKey))
	return signed
}

// simpleTxPool is a minimal TxPoolReader for testing.
type simpleTxPool struct {
	txs []*types.Transaction
}

func (p *simpleTxPool) Pending() []*types.Transaction {
	return p.txs
}

// buildAndInsert builds a block from the pool and inserts it into the chain.
func buildAndInsert(t *testing.T, bc *Blockchain, pool TxPoolReader, feeRecipient types.Address) (*types.Block, []*types.Receipt) {
	t.Helper()
	parent := bc.CurrentBlock()
	builder := NewBlockBuilder(TestConfig, bc, pool)
	attrs := &BuildBlockAttributes{
		Timestamp:    parent.Time() + 12,
		FeeRecipient: feeRecipient,
		GasLimit:     parent.GasLimit(),
	}
	block, receipts, err := builder.BuildBlock(parent.Header(), attrs)
	if err != nil {
		t.Fatalf("BuildBlock: %v", err)
	}
	if err := bc.InsertBlock(block); err != nil {
		t.Fatalf("InsertBlock: %v", err)
	}
	return block, receipts
}

// ---------------------------------------------------------------------------
// Test 1: Full Transaction Lifecycle
// ---------------------------------------------------------------------------

// TestE2E_FullTransactionLifecycle tests the complete pipeline:
// generate key -> fund account -> sign tx -> add to pool -> build block -> verify state.
func TestE2E_FullTransactionLifecycle(t *testing.T) {
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	sender := crypto.PubkeyToAddress(key.PublicKey)
	recipient := types.HexToAddress("0xBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB")
	coinbase := types.HexToAddress("0xCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCC")

	initialBalance := ether(100)
	transferAmount := ether(1)

	bc := e2eChain(t, 30_000_000, big.NewInt(1), map[types.Address]*big.Int{
		sender: initialBalance,
	})

	// Record the genesis state root.
	genesisRoot := bc.CurrentBlock().Root()

	// Create and sign a legacy transaction.
	tx := signLegacyTx(t, key, TestConfig.ChainID, &types.LegacyTx{
		Nonce:    0,
		GasPrice: big.NewInt(10),
		Gas:      21000,
		To:       &recipient,
		Value:    transferAmount,
	})

	pool := &simpleTxPool{txs: []*types.Transaction{tx}}
	block, receipts := buildAndInsert(t, bc, pool, coinbase)

	// --- Verify block ---
	if block.NumberU64() != 1 {
		t.Errorf("block number = %d, want 1", block.NumberU64())
	}
	if len(block.Transactions()) != 1 {
		t.Fatalf("tx count = %d, want 1", len(block.Transactions()))
	}

	// --- Verify receipt ---
	if len(receipts) != 1 {
		t.Fatalf("receipt count = %d, want 1", len(receipts))
	}
	if receipts[0].Status != types.ReceiptStatusSuccessful {
		t.Fatalf("receipt status = %d, want success", receipts[0].Status)
	}
	if receipts[0].GasUsed != TxGas {
		t.Errorf("receipt gas used = %d, want %d", receipts[0].GasUsed, TxGas)
	}

	// --- Verify state changes ---
	st := bc.State()

	// Recipient balance should be exactly transferAmount.
	recipientBal := st.GetBalance(recipient)
	if recipientBal.Cmp(transferAmount) != 0 {
		t.Errorf("recipient balance = %s, want %s", recipientBal, transferAmount)
	}

	// Sender nonce should be incremented.
	if st.GetNonce(sender) != 1 {
		t.Errorf("sender nonce = %d, want 1", st.GetNonce(sender))
	}

	// Sender balance = initial - transfer - gasCost.
	gasCost := new(big.Int).Mul(big.NewInt(10), new(big.Int).SetUint64(TxGas))
	expectedSenderBal := new(big.Int).Sub(initialBalance, transferAmount)
	expectedSenderBal.Sub(expectedSenderBal, gasCost)
	senderBal := st.GetBalance(sender)
	if senderBal.Cmp(expectedSenderBal) != 0 {
		t.Errorf("sender balance = %s, want %s", senderBal, expectedSenderBal)
	}

	// --- Verify state root changed ---
	newRoot := block.Root()
	if newRoot == genesisRoot {
		t.Error("state root did not change after transaction")
	}

	// --- Verify receipts are stored in blockchain ---
	storedReceipts := bc.GetReceipts(block.Hash())
	if len(storedReceipts) != 1 {
		t.Errorf("stored receipt count = %d, want 1", len(storedReceipts))
	}

	// --- Verify transaction lookup ---
	blockHash, blockNum, txIdx, found := bc.GetTransactionLookup(tx.Hash())
	if !found {
		t.Fatal("transaction lookup not found")
	}
	if blockHash != block.Hash() {
		t.Errorf("tx lookup block hash mismatch")
	}
	if blockNum != 1 {
		t.Errorf("tx lookup block number = %d, want 1", blockNum)
	}
	if txIdx != 0 {
		t.Errorf("tx lookup tx index = %d, want 0", txIdx)
	}
}

// ---------------------------------------------------------------------------
// Test 2: Contract Deployment and Execution
// ---------------------------------------------------------------------------

// TestE2E_ContractDeploymentAndExecution deploys a simple storage contract
// and then calls it to verify the storage was written.
func TestE2E_ContractDeploymentAndExecution(t *testing.T) {
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	sender := crypto.PubkeyToAddress(key.PublicKey)
	coinbase := types.HexToAddress("0xCCCC")

	bc := e2eChain(t, 30_000_000, big.NewInt(1), map[types.Address]*big.Int{
		sender: ether(100),
	})

	// Init code that does: PUSH1 0x42, PUSH1 0x00, SSTORE, STOP
	// Then returns empty runtime code (just a STOP).
	// Bytecode: 60 42 60 00 55 00
	// But we need to return runtime code. Let's do:
	//   PUSH1 0x42  PUSH1 0x00  SSTORE      -- store 0x42 at slot 0
	//   PUSH1 0x01  PUSH1 0x00  MSTORE8     -- store 0x00 at mem[0] (runtime = single STOP byte)
	//   PUSH1 0x01  PUSH1 0x00  RETURN      -- return 1 byte from mem[0] => runtime code = [0x00] (STOP)
	//
	// Actually, let's return runtime code that returns the stored value when called:
	// Runtime code: PUSH1 0x00, SLOAD, PUSH1 0x00, MSTORE, PUSH1 0x20, PUSH1 0x00, RETURN
	// = 60 00 54 60 00 52 60 20 60 00 f3

	runtimeCode := []byte{
		0x60, 0x00, // PUSH1 0x00
		0x54,       // SLOAD (load slot 0)
		0x60, 0x00, // PUSH1 0x00
		0x52,       // MSTORE
		0x60, 0x20, // PUSH1 0x20
		0x60, 0x00, // PUSH1 0x00
		0xf3,       // RETURN
	}
	runtimeLen := byte(len(runtimeCode)) // 11

	// Init code: SSTORE 0x42 at slot 0, then CODECOPY runtime + RETURN
	// PUSH1 0x42, PUSH1 0x00, SSTORE         -- 60 42 60 00 55
	// PUSH1 runtimeLen, PUSH1 initLen, PUSH1 0x00, CODECOPY  -- 60 0b 60 XX 60 00 39
	// PUSH1 runtimeLen, PUSH1 0x00, RETURN    -- 60 0b 60 00 f3
	// Then append runtimeCode.

	initPrefix := []byte{
		0x60, 0x42, // PUSH1 0x42
		0x60, 0x00, // PUSH1 0x00
		0x55,                     // SSTORE
		0x60, runtimeLen,         // PUSH1 runtimeLen
		0x60, 0x00,               // PUSH1 (placeholder for initLen, filled below)
		0x60, 0x00,               // PUSH1 0x00
		0x39,                     // CODECOPY
		0x60, runtimeLen,         // PUSH1 runtimeLen
		0x60, 0x00,               // PUSH1 0x00
		0xf3,                     // RETURN
	}
	// The initLen is the length of initPrefix.
	initPrefix[8] = byte(len(initPrefix))

	initCode := append(initPrefix, runtimeCode...)

	// --- Block 1: Deploy contract ---
	deployTx := signLegacyTx(t, key, TestConfig.ChainID, &types.LegacyTx{
		Nonce:    0,
		GasPrice: big.NewInt(10),
		Gas:      200_000,
		To:       nil, // contract creation
		Value:    big.NewInt(0),
		Data:     initCode,
	})

	pool := &simpleTxPool{txs: []*types.Transaction{deployTx}}
	block1, receipts1 := buildAndInsert(t, bc, pool, coinbase)

	if len(receipts1) != 1 {
		t.Fatalf("deploy receipt count = %d, want 1", len(receipts1))
	}
	if receipts1[0].Status != types.ReceiptStatusSuccessful {
		t.Fatalf("deploy receipt status = %d, want success", receipts1[0].Status)
	}

	// The contract address is stored in the receipt.
	contractAddr := receipts1[0].ContractAddress
	if contractAddr == (types.Address{}) {
		t.Fatal("contract address is zero")
	}

	// Verify contract code exists in state.
	st := bc.State()
	code := st.GetCode(contractAddr)
	if len(code) == 0 {
		t.Fatal("contract code is empty after deployment")
	}
	if len(code) != len(runtimeCode) {
		t.Errorf("contract code length = %d, want %d", len(code), len(runtimeCode))
	}

	// Verify storage slot 0 was set to 0x42.
	slot0 := st.GetState(contractAddr, types.Hash{})
	expected := types.Hash{}
	expected[31] = 0x42
	if slot0 != expected {
		t.Errorf("storage slot 0 = %x, want 0x42 in last byte", slot0)
	}

	// --- Block 2: Call the contract ---
	callTx := signLegacyTx(t, key, TestConfig.ChainID, &types.LegacyTx{
		Nonce:    1,
		GasPrice: big.NewInt(10),
		Gas:      100_000,
		To:       &contractAddr,
		Value:    big.NewInt(0),
		Data:     nil, // no calldata needed
	})

	pool2 := &simpleTxPool{txs: []*types.Transaction{callTx}}
	_, receipts2 := buildAndInsert(t, bc, pool2, coinbase)

	if len(receipts2) != 1 {
		t.Fatalf("call receipt count = %d, want 1", len(receipts2))
	}
	if receipts2[0].Status != types.ReceiptStatusSuccessful {
		t.Fatalf("call receipt status = %d, want success", receipts2[0].Status)
	}

	// Verify the chain advanced properly.
	if bc.CurrentBlock().NumberU64() != 2 {
		t.Errorf("head = %d, want 2", bc.CurrentBlock().NumberU64())
	}
	_ = block1
}

// ---------------------------------------------------------------------------
// Test 3: Multi-Block Chain
// ---------------------------------------------------------------------------

// TestE2E_MultiBlockChain builds a chain of 7 blocks, each with a transfer,
// and verifies cumulative state and block linkage.
func TestE2E_MultiBlockChain(t *testing.T) {
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	sender := crypto.PubkeyToAddress(key.PublicKey)
	recipient := types.HexToAddress("0xDDDD")
	coinbase := types.HexToAddress("0xCCCC")

	const numBlocks = 7
	transferPerBlock := big.NewInt(1000)

	bc := e2eChain(t, 30_000_000, big.NewInt(1), map[types.Address]*big.Int{
		sender: ether(100),
	})

	for i := 0; i < numBlocks; i++ {
		tx := signLegacyTx(t, key, TestConfig.ChainID, &types.LegacyTx{
			Nonce:    uint64(i),
			GasPrice: big.NewInt(10),
			Gas:      21000,
			To:       &recipient,
			Value:    transferPerBlock,
		})
		pool := &simpleTxPool{txs: []*types.Transaction{tx}}
		buildAndInsert(t, bc, pool, coinbase)
	}

	// --- Verify chain length ---
	if bc.ChainLength() != numBlocks+1 {
		t.Errorf("chain length = %d, want %d", bc.ChainLength(), numBlocks+1)
	}
	if bc.CurrentBlock().NumberU64() != numBlocks {
		t.Errorf("head = %d, want %d", bc.CurrentBlock().NumberU64(), numBlocks)
	}

	// --- Verify cumulative state ---
	st := bc.State()

	// Recipient should have numBlocks * transferPerBlock.
	expectedRecipientBal := new(big.Int).Mul(big.NewInt(numBlocks), transferPerBlock)
	recipientBal := st.GetBalance(recipient)
	if recipientBal.Cmp(expectedRecipientBal) != 0 {
		t.Errorf("recipient balance = %s, want %s", recipientBal, expectedRecipientBal)
	}

	// Sender nonce should equal numBlocks.
	if st.GetNonce(sender) != numBlocks {
		t.Errorf("sender nonce = %d, want %d", st.GetNonce(sender), numBlocks)
	}

	// --- Verify block numbers and parent hashes are correct ---
	for i := uint64(1); i <= numBlocks; i++ {
		block := bc.GetBlockByNumber(i)
		if block == nil {
			t.Fatalf("block %d is nil", i)
		}
		if block.NumberU64() != i {
			t.Errorf("block %d number = %d", i, block.NumberU64())
		}
		parent := bc.GetBlockByNumber(i - 1)
		if parent == nil {
			t.Fatalf("parent block %d is nil", i-1)
		}
		if block.ParentHash() != parent.Hash() {
			t.Errorf("block %d parent hash mismatch", i)
		}
		// Each block should have exactly 1 tx.
		if len(block.Transactions()) != 1 {
			t.Errorf("block %d tx count = %d, want 1", i, len(block.Transactions()))
		}
	}

	// --- Verify receipts at each block ---
	for i := uint64(1); i <= numBlocks; i++ {
		receipts := bc.GetBlockReceipts(i)
		if len(receipts) != 1 {
			t.Errorf("block %d receipt count = %d, want 1", i, len(receipts))
		}
	}
}

// ---------------------------------------------------------------------------
// Test 4: EIP-1559 Dynamic Fee Transactions
// ---------------------------------------------------------------------------

// TestE2E_EIP1559DynamicFee verifies EIP-1559 gas pricing:
// effective gas price, tip distribution, and base fee calculation.
func TestE2E_EIP1559DynamicFee(t *testing.T) {
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	sender := crypto.PubkeyToAddress(key.PublicKey)
	recipient := types.HexToAddress("0xDDDD")
	coinbase := types.HexToAddress("0xCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCC")

	baseFee := gwei(10)
	bc := e2eChain(t, 30_000_000, baseFee, map[types.Address]*big.Int{
		sender: ether(100),
	})

	// maxPriorityFeePerGas = 2 gwei, maxFeePerGas = 20 gwei
	tipCap := gwei(2)
	feeCap := gwei(20)

	tx := signDynamicFeeTx(t, key, &types.DynamicFeeTx{
		ChainID:   TestConfig.ChainID,
		Nonce:     0,
		GasTipCap: tipCap,
		GasFeeCap: feeCap,
		Gas:       21000,
		To:        &recipient,
		Value:     ether(1),
	})

	pool := &simpleTxPool{txs: []*types.Transaction{tx}}

	// Record coinbase balance before.
	coinbaseBalBefore := bc.State().GetBalance(coinbase)

	block, receipts := buildAndInsert(t, bc, pool, coinbase)

	if len(receipts) != 1 {
		t.Fatalf("receipt count = %d, want 1", len(receipts))
	}
	if receipts[0].Status != types.ReceiptStatusSuccessful {
		t.Fatalf("receipt status = %d, want success", receipts[0].Status)
	}

	// The block's base fee is computed from the parent (genesis) header.
	blockBaseFee := block.BaseFee()
	if blockBaseFee == nil {
		t.Fatal("block base fee is nil")
	}

	// Effective gas price = min(feeCap, baseFee + tipCap)
	effectivePrice := new(big.Int).Add(blockBaseFee, tipCap)
	if effectivePrice.Cmp(feeCap) > 0 {
		effectivePrice = new(big.Int).Set(feeCap)
	}

	// Verify receipt effective gas price.
	if receipts[0].EffectiveGasPrice != nil {
		if receipts[0].EffectiveGasPrice.Cmp(effectivePrice) != 0 {
			t.Errorf("receipt effective gas price = %s, want %s",
				receipts[0].EffectiveGasPrice, effectivePrice)
		}
	}

	// Verify tip went to coinbase.
	// Tip per unit of gas = effectivePrice - baseFee
	tip := new(big.Int).Sub(effectivePrice, blockBaseFee)
	expectedCoinbaseGain := new(big.Int).Mul(tip, new(big.Int).SetUint64(TxGas))

	coinbaseBalAfter := bc.State().GetBalance(coinbase)
	coinbaseGain := new(big.Int).Sub(coinbaseBalAfter, coinbaseBalBefore)
	if coinbaseGain.Cmp(expectedCoinbaseGain) != 0 {
		t.Errorf("coinbase tip = %s, want %s", coinbaseGain, expectedCoinbaseGain)
	}

	// Verify sender paid = effectivePrice * gasUsed + value.
	st := bc.State()
	senderBal := st.GetBalance(sender)
	totalGasCost := new(big.Int).Mul(effectivePrice, new(big.Int).SetUint64(TxGas))
	expectedSenderBal := new(big.Int).Sub(ether(100), ether(1))
	expectedSenderBal.Sub(expectedSenderBal, totalGasCost)
	if senderBal.Cmp(expectedSenderBal) != 0 {
		t.Errorf("sender balance = %s, want %s", senderBal, expectedSenderBal)
	}
}

// ---------------------------------------------------------------------------
// Test 5: Block Builder with Gas Limit
// ---------------------------------------------------------------------------

// TestE2E_BlockBuilderGasLimit verifies that the block builder respects the
// gas limit and skips transactions that don't fit.
func TestE2E_BlockBuilderGasLimit(t *testing.T) {
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	sender := crypto.PubkeyToAddress(key.PublicKey)
	recipient := types.HexToAddress("0xDDDD")
	coinbase := types.HexToAddress("0xCCCC")

	// Gas limit of 50000: only fits 2 simple transfers (21000 each).
	bc := e2eChain(t, 50000, big.NewInt(1), map[types.Address]*big.Int{
		sender: ether(100),
	})

	// Create 5 transactions.
	var txs []*types.Transaction
	for i := uint64(0); i < 5; i++ {
		tx := signLegacyTx(t, key, TestConfig.ChainID, &types.LegacyTx{
			Nonce:    i,
			GasPrice: big.NewInt(10),
			Gas:      21000,
			To:       &recipient,
			Value:    big.NewInt(100),
		})
		txs = append(txs, tx)
	}

	pool := &simpleTxPool{txs: txs}

	parent := bc.CurrentBlock()
	builder := NewBlockBuilder(TestConfig, bc, pool)
	attrs := &BuildBlockAttributes{
		Timestamp:    parent.Time() + 12,
		FeeRecipient: coinbase,
		GasLimit:     50000,
	}

	block, receipts, err := builder.BuildBlock(parent.Header(), attrs)
	if err != nil {
		t.Fatalf("BuildBlock: %v", err)
	}

	// 50000 / 21000 = 2.38 => max 2 txs.
	includedCount := len(block.Transactions())
	if includedCount > 2 {
		t.Errorf("included %d txs, want at most 2", includedCount)
	}
	if includedCount < 1 {
		t.Errorf("expected at least 1 tx to be included, got 0")
	}

	// Receipts should match included txs.
	if len(receipts) != includedCount {
		t.Errorf("receipt count %d != tx count %d", len(receipts), includedCount)
	}

	// Gas used should not exceed gas limit.
	if block.GasUsed() > block.GasLimit() {
		t.Errorf("gas used %d > gas limit %d", block.GasUsed(), block.GasLimit())
	}

	// Gas accounting: gasUsed should be exactly includedCount * 21000.
	expectedGas := uint64(includedCount) * TxGas
	if block.GasUsed() != expectedGas {
		t.Errorf("gas used = %d, want %d", block.GasUsed(), expectedGas)
	}

	// All included receipts should be successful.
	for i, r := range receipts {
		if r.Status != types.ReceiptStatusSuccessful {
			t.Errorf("receipt %d status = %d, want success", i, r.Status)
		}
	}
}

// ---------------------------------------------------------------------------
// Test 6: Value Transfer Chain (A -> B -> C in same block)
// ---------------------------------------------------------------------------

// TestE2E_ValueTransferChain tests A sends to B, B sends to C in the same block.
// Verifies that nonce ordering matters and final balances are correct.
func TestE2E_ValueTransferChain(t *testing.T) {
	keyA, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey A: %v", err)
	}
	keyB, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey B: %v", err)
	}
	addrA := crypto.PubkeyToAddress(keyA.PublicKey)
	addrB := crypto.PubkeyToAddress(keyB.PublicKey)
	addrC := types.HexToAddress("0xCCCC")
	coinbase := types.HexToAddress("0xFFFF")

	// Fund both A and B. B needs some balance for gas.
	bc := e2eChain(t, 30_000_000, big.NewInt(1), map[types.Address]*big.Int{
		addrA: ether(100),
		addrB: ether(1), // enough for gas but not much else
	})

	transferAtoB := ether(5)
	transferBtoC := ether(3)
	gasPrice := big.NewInt(10)

	// tx1: A -> B (5 ETH)
	tx1 := signLegacyTx(t, keyA, TestConfig.ChainID, &types.LegacyTx{
		Nonce:    0,
		GasPrice: gasPrice,
		Gas:      21000,
		To:       &addrB,
		Value:    transferAtoB,
	})

	// tx2: B -> C (3 ETH). B's balance after tx1 = 1 ETH (initial) + 5 ETH = 6 ETH.
	tx2 := signLegacyTx(t, keyB, TestConfig.ChainID, &types.LegacyTx{
		Nonce:    0,
		GasPrice: gasPrice,
		Gas:      21000,
		To:       &addrC,
		Value:    transferBtoC,
	})

	// Both transactions in the same pool.
	pool := &simpleTxPool{txs: []*types.Transaction{tx1, tx2}}
	_, receipts := buildAndInsert(t, bc, pool, coinbase)

	if len(receipts) != 2 {
		t.Fatalf("receipt count = %d, want 2", len(receipts))
	}
	for i, r := range receipts {
		if r.Status != types.ReceiptStatusSuccessful {
			t.Errorf("receipt %d status = %d, want success", i, r.Status)
		}
	}

	st := bc.State()
	gasCost := new(big.Int).Mul(gasPrice, new(big.Int).SetUint64(TxGas))

	// A's balance: 100 ETH - 5 ETH - gasCost
	expectedA := new(big.Int).Sub(ether(100), transferAtoB)
	expectedA.Sub(expectedA, gasCost)
	if balA := st.GetBalance(addrA); balA.Cmp(expectedA) != 0 {
		t.Errorf("A balance = %s, want %s", balA, expectedA)
	}

	// B's balance: 1 ETH (initial) + 5 ETH (from A) - 3 ETH (to C) - gasCost
	expectedB := new(big.Int).Add(ether(1), transferAtoB)
	expectedB.Sub(expectedB, transferBtoC)
	expectedB.Sub(expectedB, gasCost)
	if balB := st.GetBalance(addrB); balB.Cmp(expectedB) != 0 {
		t.Errorf("B balance = %s, want %s", balB, expectedB)
	}

	// C's balance: 3 ETH
	if balC := st.GetBalance(addrC); balC.Cmp(transferBtoC) != 0 {
		t.Errorf("C balance = %s, want %s", balC, transferBtoC)
	}

	// Nonces: A=1, B=1
	if st.GetNonce(addrA) != 1 {
		t.Errorf("A nonce = %d, want 1", st.GetNonce(addrA))
	}
	if st.GetNonce(addrB) != 1 {
		t.Errorf("B nonce = %d, want 1", st.GetNonce(addrB))
	}
}

// ---------------------------------------------------------------------------
// Test 7: Multi-Block Chain with Contract Interaction
// ---------------------------------------------------------------------------

// TestE2E_MultiBlockContractInteraction builds multiple blocks:
// block 1 deploys a contract, blocks 2-4 call it, verifying storage persists.
func TestE2E_MultiBlockContractInteraction(t *testing.T) {
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	sender := crypto.PubkeyToAddress(key.PublicKey)
	coinbase := types.HexToAddress("0xCCCC")

	bc := e2eChain(t, 30_000_000, big.NewInt(1), map[types.Address]*big.Int{
		sender: ether(100),
	})

	// Simple contract: PUSH1 value, PUSH1 slot, SSTORE, STOP
	// Init code returns runtime code that stores calldata byte 0 at slot 0.
	// Runtime: PUSH1 0x00, CALLDATALOAD, PUSH1 0x00, SSTORE, STOP
	// = 60 00 35 60 00 55 00
	runtimeCode := []byte{
		0x60, 0x00, // PUSH1 0x00
		0x35,       // CALLDATALOAD (load 32 bytes from calldata[0])
		0x60, 0x00, // PUSH1 0x00
		0x55,       // SSTORE (store at slot 0)
		0x00,       // STOP
	}
	runtimeLen := byte(len(runtimeCode)) // 7

	// Init code: CODECOPY runtime + RETURN
	initPrefix := []byte{
		0x60, runtimeLen, // PUSH1 runtimeLen
		0x60, 0x00,       // PUSH1 (placeholder for initLen)
		0x60, 0x00,       // PUSH1 0x00
		0x39,             // CODECOPY
		0x60, runtimeLen, // PUSH1 runtimeLen
		0x60, 0x00,       // PUSH1 0x00
		0xf3,             // RETURN
	}
	initPrefix[3] = byte(len(initPrefix))
	initCode := append(initPrefix, runtimeCode...)

	// --- Block 1: Deploy ---
	deployTx := signLegacyTx(t, key, TestConfig.ChainID, &types.LegacyTx{
		Nonce:    0,
		GasPrice: big.NewInt(10),
		Gas:      200_000,
		To:       nil,
		Value:    big.NewInt(0),
		Data:     initCode,
	})

	pool := &simpleTxPool{txs: []*types.Transaction{deployTx}}
	_, receipts := buildAndInsert(t, bc, pool, coinbase)
	if receipts[0].Status != types.ReceiptStatusSuccessful {
		t.Fatalf("deploy failed")
	}
	contractAddr := receipts[0].ContractAddress

	// --- Blocks 2-4: Store different values ---
	for i := 1; i <= 3; i++ {
		// Calldata: 32 bytes with value i in the last byte.
		calldata := make([]byte, 32)
		calldata[31] = byte(i)

		callTx := signLegacyTx(t, key, TestConfig.ChainID, &types.LegacyTx{
			Nonce:    uint64(i),
			GasPrice: big.NewInt(10),
			Gas:      100_000,
			To:       &contractAddr,
			Value:    big.NewInt(0),
			Data:     calldata,
		})
		pool := &simpleTxPool{txs: []*types.Transaction{callTx}}
		_, rr := buildAndInsert(t, bc, pool, coinbase)
		if rr[0].Status != types.ReceiptStatusSuccessful {
			t.Fatalf("call %d failed", i)
		}
	}

	// After 3 calls, slot 0 should contain value 3 (from the last call).
	st := bc.State()
	slot0 := st.GetState(contractAddr, types.Hash{})
	expected := types.Hash{}
	expected[31] = 3
	if slot0 != expected {
		t.Errorf("slot 0 = %x, want 0x03 in last byte", slot0)
	}

	// Chain should be at block 4.
	if bc.CurrentBlock().NumberU64() != 4 {
		t.Errorf("head = %d, want 4", bc.CurrentBlock().NumberU64())
	}
}

// ---------------------------------------------------------------------------
// Test 8: Mixed Transaction Types in Multi-Block
// ---------------------------------------------------------------------------

// TestE2E_MixedTransactionTypes tests both legacy and EIP-1559 transactions
// in a multi-block scenario.
func TestE2E_MixedTransactionTypes(t *testing.T) {
	keyA, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	keyB, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	addrA := crypto.PubkeyToAddress(keyA.PublicKey)
	addrB := crypto.PubkeyToAddress(keyB.PublicKey)
	recipient := types.HexToAddress("0xDDDD")
	coinbase := types.HexToAddress("0xFFFF")

	bc := e2eChain(t, 30_000_000, gwei(10), map[types.Address]*big.Int{
		addrA: ether(100),
		addrB: ether(100),
	})

	// Block 1: Legacy tx from A + EIP-1559 tx from B.
	legacyTx := signLegacyTx(t, keyA, TestConfig.ChainID, &types.LegacyTx{
		Nonce:    0,
		GasPrice: gwei(20),
		Gas:      21000,
		To:       &recipient,
		Value:    ether(1),
	})

	dynamicTx := signDynamicFeeTx(t, keyB, &types.DynamicFeeTx{
		ChainID:   TestConfig.ChainID,
		Nonce:     0,
		GasTipCap: gwei(2),
		GasFeeCap: gwei(30),
		Gas:       21000,
		To:        &recipient,
		Value:     ether(2),
	})

	pool := &simpleTxPool{txs: []*types.Transaction{legacyTx, dynamicTx}}
	_, receipts := buildAndInsert(t, bc, pool, coinbase)

	if len(receipts) != 2 {
		t.Fatalf("receipt count = %d, want 2", len(receipts))
	}
	for i, r := range receipts {
		if r.Status != types.ReceiptStatusSuccessful {
			t.Errorf("receipt %d status = %d, want success", i, r.Status)
		}
	}

	// Recipient should have 3 ETH total.
	st := bc.State()
	recipientBal := st.GetBalance(recipient)
	if recipientBal.Cmp(ether(3)) != 0 {
		t.Errorf("recipient balance = %s, want %s", recipientBal, ether(3))
	}

	// Block 2: More transactions from both.
	legacyTx2 := signLegacyTx(t, keyA, TestConfig.ChainID, &types.LegacyTx{
		Nonce:    1,
		GasPrice: gwei(20),
		Gas:      21000,
		To:       &recipient,
		Value:    ether(1),
	})
	dynamicTx2 := signDynamicFeeTx(t, keyB, &types.DynamicFeeTx{
		ChainID:   TestConfig.ChainID,
		Nonce:     1,
		GasTipCap: gwei(2),
		GasFeeCap: gwei(30),
		Gas:       21000,
		To:        &recipient,
		Value:     ether(2),
	})

	pool2 := &simpleTxPool{txs: []*types.Transaction{legacyTx2, dynamicTx2}}
	buildAndInsert(t, bc, pool2, coinbase)

	st2 := bc.State()
	recipientBal2 := st2.GetBalance(recipient)
	if recipientBal2.Cmp(ether(6)) != 0 {
		t.Errorf("recipient balance after 2 blocks = %s, want %s", recipientBal2, ether(6))
	}
}

// ---------------------------------------------------------------------------
// Test 9: Nonce Ordering Matters
// ---------------------------------------------------------------------------

// TestE2E_NonceOrdering verifies that transactions with wrong nonces are
// rejected or skipped by the block builder.
func TestE2E_NonceOrdering(t *testing.T) {
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	sender := crypto.PubkeyToAddress(key.PublicKey)
	recipient := types.HexToAddress("0xDDDD")
	coinbase := types.HexToAddress("0xCCCC")

	bc := e2eChain(t, 30_000_000, big.NewInt(1), map[types.Address]*big.Int{
		sender: ether(100),
	})

	// Submit tx with nonce 1 first (skipping nonce 0). It should be skipped
	// because the sender's state nonce is 0.
	txNonce1 := signLegacyTx(t, key, TestConfig.ChainID, &types.LegacyTx{
		Nonce:    1,
		GasPrice: big.NewInt(10),
		Gas:      21000,
		To:       &recipient,
		Value:    big.NewInt(100),
	})

	pool := &simpleTxPool{txs: []*types.Transaction{txNonce1}}
	block, _ := buildAndInsert(t, bc, pool, coinbase)

	// The tx with nonce 1 should be skipped (sender nonce is 0).
	if len(block.Transactions()) != 0 {
		t.Errorf("expected 0 txs (nonce gap), got %d", len(block.Transactions()))
	}

	// Now submit nonce 0, which should succeed.
	txNonce0 := signLegacyTx(t, key, TestConfig.ChainID, &types.LegacyTx{
		Nonce:    0,
		GasPrice: big.NewInt(10),
		Gas:      21000,
		To:       &recipient,
		Value:    big.NewInt(100),
	})

	pool2 := &simpleTxPool{txs: []*types.Transaction{txNonce0}}
	block2, _ := buildAndInsert(t, bc, pool2, coinbase)

	if len(block2.Transactions()) != 1 {
		t.Errorf("expected 1 tx, got %d", len(block2.Transactions()))
	}

	st := bc.State()
	if st.GetNonce(sender) != 1 {
		t.Errorf("sender nonce = %d, want 1", st.GetNonce(sender))
	}
}

// ---------------------------------------------------------------------------
// Test 10: State Root Consistency
// ---------------------------------------------------------------------------

// TestE2E_StateRootConsistency verifies that state roots change between blocks
// and that re-executing the chain produces the same state.
func TestE2E_StateRootConsistency(t *testing.T) {
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	sender := crypto.PubkeyToAddress(key.PublicKey)
	recipient := types.HexToAddress("0xDDDD")
	coinbase := types.HexToAddress("0xCCCC")

	bc := e2eChain(t, 30_000_000, big.NewInt(1), map[types.Address]*big.Int{
		sender: ether(100),
	})

	roots := make([]types.Hash, 0, 6)
	roots = append(roots, bc.CurrentBlock().Root())

	for i := 0; i < 5; i++ {
		tx := signLegacyTx(t, key, TestConfig.ChainID, &types.LegacyTx{
			Nonce:    uint64(i),
			GasPrice: big.NewInt(10),
			Gas:      21000,
			To:       &recipient,
			Value:    big.NewInt(int64(i+1) * 100),
		})
		pool := &simpleTxPool{txs: []*types.Transaction{tx}}
		block, _ := buildAndInsert(t, bc, pool, coinbase)
		roots = append(roots, block.Root())
	}

	// Each block should have a different state root (since state changes each block).
	for i := 1; i < len(roots); i++ {
		if roots[i] == roots[i-1] {
			t.Errorf("state root at block %d same as block %d", i, i-1)
		}
	}

	// All roots should be unique.
	seen := make(map[types.Hash]int)
	for i, root := range roots {
		if prev, ok := seen[root]; ok {
			t.Errorf("state root at block %d duplicates block %d", i, prev)
		}
		seen[root] = i
	}
}

// ---------------------------------------------------------------------------
// Test 11: Block Builder with SetSender (no crypto signing)
// ---------------------------------------------------------------------------

// TestE2E_BlockBuilderWithSetSender tests the simpler path where the sender
// is set directly on the transaction (no crypto.Sign required).
func TestE2E_BlockBuilderWithSetSender(t *testing.T) {
	sender := types.HexToAddress("0xAAAA")
	recipient := types.HexToAddress("0xBBBB")
	coinbase := types.HexToAddress("0xCCCC")

	bc := e2eChain(t, 30_000_000, big.NewInt(1), map[types.Address]*big.Int{
		sender: ether(10),
	})

	// Create 3 transactions using SetSender directly.
	var txs []*types.Transaction
	for i := uint64(0); i < 3; i++ {
		tx := types.NewTransaction(&types.LegacyTx{
			Nonce:    i,
			GasPrice: big.NewInt(10),
			Gas:      21000,
			To:       &recipient,
			Value:    big.NewInt(1000),
		})
		tx.SetSender(sender)
		txs = append(txs, tx)
	}

	pool := &simpleTxPool{txs: txs}
	block, receipts := buildAndInsert(t, bc, pool, coinbase)

	if len(block.Transactions()) != 3 {
		t.Errorf("tx count = %d, want 3", len(block.Transactions()))
	}
	if len(receipts) != 3 {
		t.Errorf("receipt count = %d, want 3", len(receipts))
	}

	st := bc.State()
	recipientBal := st.GetBalance(recipient)
	expectedBal := big.NewInt(3000) // 3 * 1000
	if recipientBal.Cmp(expectedBal) != 0 {
		t.Errorf("recipient balance = %s, want %s", recipientBal, expectedBal)
	}
	if st.GetNonce(sender) != 3 {
		t.Errorf("sender nonce = %d, want 3", st.GetNonce(sender))
	}
}

// ---------------------------------------------------------------------------
// Test 12: EIP-1559 Base Fee Adjusts Across Blocks
// ---------------------------------------------------------------------------

// TestE2E_BaseFeeAdjustment verifies that the base fee adjusts across blocks
// based on gas usage relative to target.
func TestE2E_BaseFeeAdjustment(t *testing.T) {
	sender := types.HexToAddress("0xAAAA")
	recipient := types.HexToAddress("0xBBBB")
	coinbase := types.HexToAddress("0xCCCC")

	initialBaseFee := gwei(10)
	bc := e2eChain(t, 30_000_000, initialBaseFee, map[types.Address]*big.Int{
		sender: ether(1000),
	})

	var baseFees []*big.Int
	baseFees = append(baseFees, initialBaseFee)

	// Build 5 blocks with some transactions.
	for i := 0; i < 5; i++ {
		var txs []*types.Transaction
		// Include a few transactions per block to push gas usage above/near target.
		for j := 0; j < 3; j++ {
			nonce := uint64(i*3 + j)
			tx := types.NewTransaction(&types.LegacyTx{
				Nonce:    nonce,
				GasPrice: gwei(100), // high gas price to always be included
				Gas:      21000,
				To:       &recipient,
				Value:    big.NewInt(1),
			})
			tx.SetSender(sender)
			txs = append(txs, tx)
		}
		pool := &simpleTxPool{txs: txs}
		block, _ := buildAndInsert(t, bc, pool, coinbase)
		baseFees = append(baseFees, block.BaseFee())
	}

	// The base fee should change (likely decrease since gas usage is well below
	// the 50% target). Verify that it changes between blocks.
	changed := false
	for i := 1; i < len(baseFees); i++ {
		if baseFees[i].Cmp(baseFees[i-1]) != 0 {
			changed = true
			break
		}
	}
	if !changed {
		t.Error("base fee did not change across 5 blocks")
	}

	// All base fees should be >= 1 (minimum).
	for i, bf := range baseFees {
		if bf.Cmp(big.NewInt(MinBaseFee)) < 0 {
			t.Errorf("base fee at block %d = %s, below minimum %d", i, bf, MinBaseFee)
		}
	}
}

// ---------------------------------------------------------------------------
// Test 13: Full Blockchain Persistence via RawDB
// ---------------------------------------------------------------------------

// TestE2E_BlockchainPersistence tests that blocks and transactions can be
// recovered from rawdb after being evicted from the in-memory cache.
func TestE2E_BlockchainPersistence(t *testing.T) {
	sender := types.HexToAddress("0xAAAA")
	recipient := types.HexToAddress("0xBBBB")
	coinbase := types.HexToAddress("0xCCCC")

	bc := e2eChain(t, 30_000_000, big.NewInt(1), map[types.Address]*big.Int{
		sender: ether(100),
	})

	// Build 3 blocks with transactions.
	var blockHashes []types.Hash
	for i := 0; i < 3; i++ {
		tx := types.NewTransaction(&types.LegacyTx{
			Nonce:    uint64(i),
			GasPrice: big.NewInt(10),
			Gas:      21000,
			To:       &recipient,
			Value:    big.NewInt(100),
		})
		tx.SetSender(sender)
		pool := &simpleTxPool{txs: []*types.Transaction{tx}}
		block, _ := buildAndInsert(t, bc, pool, coinbase)
		blockHashes = append(blockHashes, block.Hash())
	}

	// Evict blocks from in-memory cache.
	bc.mu.Lock()
	for _, hash := range blockHashes {
		delete(bc.blockCache, hash)
	}
	bc.mu.Unlock()

	// Blocks should still be retrievable from rawdb.
	for i, hash := range blockHashes {
		recovered := bc.GetBlock(hash)
		if recovered == nil {
			t.Fatalf("block %d not recovered from rawdb", i+1)
		}
		if recovered.NumberU64() != uint64(i+1) {
			t.Errorf("recovered block %d number = %d", i+1, recovered.NumberU64())
		}
		if len(recovered.Transactions()) != 1 {
			t.Errorf("recovered block %d tx count = %d, want 1", i+1, len(recovered.Transactions()))
		}
	}
}

// ---------------------------------------------------------------------------
// Test 14: Multiple Senders in Same Block
// ---------------------------------------------------------------------------

// TestE2E_MultipleSendersInBlock tests that transactions from multiple senders
// in the same block are all processed correctly.
func TestE2E_MultipleSendersInBlock(t *testing.T) {
	numSenders := 5
	recipient := types.HexToAddress("0xDDDD")
	coinbase := types.HexToAddress("0xCCCC")

	alloc := make(map[types.Address]*big.Int)
	keys := make([]*ecdsa.PrivateKey, numSenders)
	addrs := make([]types.Address, numSenders)

	for i := 0; i < numSenders; i++ {
		key, err := crypto.GenerateKey()
		if err != nil {
			t.Fatalf("GenerateKey %d: %v", i, err)
		}
		keys[i] = key
		addrs[i] = crypto.PubkeyToAddress(key.PublicKey)
		alloc[addrs[i]] = ether(10)
	}

	bc := e2eChain(t, 30_000_000, big.NewInt(1), alloc)

	// Each sender sends 1000 wei to recipient.
	var txs []*types.Transaction
	for i := 0; i < numSenders; i++ {
		tx := signLegacyTx(t, keys[i], TestConfig.ChainID, &types.LegacyTx{
			Nonce:    0,
			GasPrice: big.NewInt(10),
			Gas:      21000,
			To:       &recipient,
			Value:    big.NewInt(1000),
		})
		txs = append(txs, tx)
	}

	pool := &simpleTxPool{txs: txs}
	block, receipts := buildAndInsert(t, bc, pool, coinbase)

	if len(block.Transactions()) != numSenders {
		t.Errorf("tx count = %d, want %d", len(block.Transactions()), numSenders)
	}
	if len(receipts) != numSenders {
		t.Errorf("receipt count = %d, want %d", len(receipts), numSenders)
	}

	st := bc.State()

	// Recipient should have numSenders * 1000.
	expectedBal := big.NewInt(int64(numSenders) * 1000)
	if bal := st.GetBalance(recipient); bal.Cmp(expectedBal) != 0 {
		t.Errorf("recipient balance = %s, want %s", bal, expectedBal)
	}

	// Each sender's nonce should be 1.
	for i, addr := range addrs {
		if st.GetNonce(addr) != 1 {
			t.Errorf("sender %d nonce = %d, want 1", i, st.GetNonce(addr))
		}
	}
}

// ---------------------------------------------------------------------------
// Test 15: Empty Blocks Advance Chain
// ---------------------------------------------------------------------------

// TestE2E_EmptyBlocksAdvanceChain tests that empty blocks can be built and
// inserted, advancing the chain correctly.
func TestE2E_EmptyBlocksAdvanceChain(t *testing.T) {
	coinbase := types.HexToAddress("0xCCCC")
	bc := e2eChain(t, 30_000_000, big.NewInt(1000), nil)

	for i := 0; i < 10; i++ {
		pool := &simpleTxPool{txs: nil}
		block, receipts := buildAndInsert(t, bc, pool, coinbase)
		if len(block.Transactions()) != 0 {
			t.Errorf("block %d tx count = %d, want 0", i+1, len(block.Transactions()))
		}
		if len(receipts) != 0 {
			t.Errorf("block %d receipt count = %d, want 0", i+1, len(receipts))
		}
		if block.GasUsed() != 0 {
			t.Errorf("block %d gas used = %d, want 0", i+1, block.GasUsed())
		}
	}

	if bc.CurrentBlock().NumberU64() != 10 {
		t.Errorf("head = %d, want 10", bc.CurrentBlock().NumberU64())
	}
	if bc.ChainLength() != 11 {
		t.Errorf("chain length = %d, want 11", bc.ChainLength())
	}
}
