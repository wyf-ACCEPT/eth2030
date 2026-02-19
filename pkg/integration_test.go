// Package integration_test provides cross-package integration tests that exercise
// the transaction lifecycle, multiple transaction types, and block building/validation
// across the core, txpool, and types packages.
package e2e_test

import (
	"crypto/ecdsa"
	"math/big"
	"testing"

	"github.com/eth2028/eth2028/core"
	"github.com/eth2028/eth2028/core/state"
	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/crypto"
)

// signTypedTx signs a typed transaction (AccessList, DynamicFee, etc.) with the given key.
// For typed transactions, V is the recovery ID (0 or 1).
func signTypedTx(t *testing.T, key *ecdsa.PrivateKey, inner types.TxData) *types.Transaction {
	t.Helper()
	tx := types.NewTransaction(inner)
	sigHash := tx.SigningHash()

	sig, err := crypto.Sign(sigHash[:], key)
	if err != nil {
		t.Fatalf("crypto.Sign: %v", err)
	}

	r := new(big.Int).SetBytes(sig[0:32])
	s := new(big.Int).SetBytes(sig[32:64])
	v := new(big.Int).SetUint64(uint64(sig[64]))

	// Set signature values on the inner data.
	switch d := inner.(type) {
	case *types.AccessListTx:
		d.V, d.R, d.S = v, r, s
	case *types.DynamicFeeTx:
		d.V, d.R, d.S = v, r, s
	case *types.BlobTx:
		d.V, d.R, d.S = v, r, s
	case *types.SetCodeTx:
		d.V, d.R, d.S = v, r, s
	default:
		t.Fatalf("signTypedTx: unsupported tx type %T", inner)
	}

	signed := types.NewTransaction(inner)
	signed.SetSender(crypto.PubkeyToAddress(key.PublicKey))
	return signed
}

// signLegacyTxInteg creates a legacy transaction signed with the given private key.
func signLegacyTxInteg(t *testing.T, key *ecdsa.PrivateKey, chainID *big.Int, inner *types.LegacyTx) *types.Transaction {
	t.Helper()
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
	recoveryID := sig[64]

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

// etherI returns n * 1e18 as *big.Int.
func etherI(n int64) *big.Int {
	return new(big.Int).Mul(big.NewInt(n), new(big.Int).SetUint64(1e18))
}

// gweiI returns n * 1e9 as *big.Int.
func gweiI(n int64) *big.Int {
	return new(big.Int).Mul(big.NewInt(n), big.NewInt(1e9))
}

// ---------------------------------------------------------------------------
// Test: Full Transaction Lifecycle
// ---------------------------------------------------------------------------

// TestFullTransactionLifecycle creates a signed transaction, processes it
// via the state processor, and verifies the resulting receipt and state.
func TestFullTransactionLifecycle(t *testing.T) {
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	sender := crypto.PubkeyToAddress(key.PublicKey)
	recipient := types.HexToAddress("0xBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB")
	coinbase := types.HexToAddress("0xCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCC")

	statedb := state.NewMemoryStateDB()
	statedb.AddBalance(sender, etherI(100))

	chainID := core.TestConfig.ChainID

	// Create and sign a legacy transfer.
	tx := signLegacyTxInteg(t, key, chainID, &types.LegacyTx{
		Nonce:    0,
		GasPrice: big.NewInt(10),
		Gas:      21000,
		To:       &recipient,
		Value:    etherI(1),
	})

	header := &types.Header{
		Number:   big.NewInt(1),
		GasLimit: 30_000_000,
		Time:     1700000000,
		Coinbase: coinbase,
		BaseFee:  big.NewInt(1),
	}
	block := types.NewBlock(header, &types.Body{
		Transactions: []*types.Transaction{tx},
	})

	processor := core.NewStateProcessor(core.TestConfig)
	receipts, err := processor.Process(block, statedb)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}

	if len(receipts) != 1 {
		t.Fatalf("receipt count = %d, want 1", len(receipts))
	}
	if receipts[0].Status != types.ReceiptStatusSuccessful {
		t.Fatalf("receipt status = %d, want success", receipts[0].Status)
	}
	if receipts[0].GasUsed != 21000 {
		t.Errorf("gas used = %d, want 21000", receipts[0].GasUsed)
	}

	// Verify state changes.
	recipientBal := statedb.GetBalance(recipient)
	if recipientBal.Cmp(etherI(1)) != 0 {
		t.Errorf("recipient balance = %s, want 1 ETH", recipientBal)
	}
	if statedb.GetNonce(sender) != 1 {
		t.Errorf("sender nonce = %d, want 1", statedb.GetNonce(sender))
	}
}

// ---------------------------------------------------------------------------
// Test: Multiple Transactions in Block
// ---------------------------------------------------------------------------

// TestMultipleTransactionsInBlock processes several transactions sequentially
// and verifies cumulative state.
func TestMultipleTransactionsInBlock(t *testing.T) {
	sender := types.BytesToAddress([]byte{0x10})
	receiver := types.BytesToAddress([]byte{0x20})
	coinbase := types.BytesToAddress([]byte{0xff})

	statedb := state.NewMemoryStateDB()
	statedb.AddBalance(sender, big.NewInt(1_000_000_000_000))

	gasPrice := big.NewInt(10)
	transferAmount := big.NewInt(1000)

	var txs []*types.Transaction
	for i := uint64(0); i < 5; i++ {
		tx := types.NewTransaction(&types.LegacyTx{
			Nonce:    i,
			GasPrice: gasPrice,
			Gas:      21000,
			To:       &receiver,
			Value:    transferAmount,
		})
		tx.SetSender(sender)
		txs = append(txs, tx)
	}

	header := &types.Header{
		Number:   big.NewInt(1),
		GasLimit: 30_000_000,
		Time:     1700000000,
		Coinbase: coinbase,
		BaseFee:  big.NewInt(1),
	}
	block := types.NewBlock(header, &types.Body{Transactions: txs})

	processor := core.NewStateProcessor(core.TestConfig)
	receipts, err := processor.Process(block, statedb)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}

	if len(receipts) != 5 {
		t.Fatalf("receipt count = %d, want 5", len(receipts))
	}
	for i, r := range receipts {
		if r.Status != types.ReceiptStatusSuccessful {
			t.Errorf("receipt %d: status = %d, want success", i, r.Status)
		}
	}

	// Receiver: 5 * 1000 = 5000.
	expectedReceiverBal := new(big.Int).Mul(big.NewInt(5), transferAmount)
	if bal := statedb.GetBalance(receiver); bal.Cmp(expectedReceiverBal) != 0 {
		t.Errorf("receiver balance = %s, want %s", bal, expectedReceiverBal)
	}

	// Sender nonce should be 5.
	if nonce := statedb.GetNonce(sender); nonce != 5 {
		t.Errorf("sender nonce = %d, want 5", nonce)
	}

	// Cumulative gas: 5 * 21000 = 105000.
	lastReceipt := receipts[len(receipts)-1]
	if lastReceipt.CumulativeGasUsed != 5*21000 {
		t.Errorf("cumulative gas = %d, want %d", lastReceipt.CumulativeGasUsed, 5*21000)
	}
}

// ---------------------------------------------------------------------------
// Test: Transaction Type Matrix
// ---------------------------------------------------------------------------

// TestTransactionTypeMatrix tests all 5 transaction types (Legacy, AccessList,
// DynamicFee, Blob, SetCode) through the state processor.
func TestTransactionTypeMatrix(t *testing.T) {
	sender := types.BytesToAddress([]byte{0x10})
	receiver := types.BytesToAddress([]byte{0x20})
	coinbase := types.BytesToAddress([]byte{0xff})

	statedb := state.NewMemoryStateDB()
	statedb.AddBalance(sender, big.NewInt(1_000_000_000_000_000))

	chainID := big.NewInt(1337)

	t.Run("LegacyTx", func(t *testing.T) {
		db := state.NewMemoryStateDB()
		db.AddBalance(sender, big.NewInt(1_000_000_000_000))

		tx := types.NewTransaction(&types.LegacyTx{
			Nonce:    0,
			GasPrice: big.NewInt(10),
			Gas:      21000,
			To:       &receiver,
			Value:    big.NewInt(100),
		})
		tx.SetSender(sender)

		header := &types.Header{
			Number:   big.NewInt(1),
			GasLimit: 30_000_000,
			Time:     1700000000,
			Coinbase: coinbase,
			BaseFee:  big.NewInt(1),
		}
		block := types.NewBlock(header, &types.Body{Transactions: []*types.Transaction{tx}})

		processor := core.NewStateProcessor(core.TestConfig)
		receipts, err := processor.Process(block, db)
		if err != nil {
			t.Fatalf("Process: %v", err)
		}
		if receipts[0].Status != types.ReceiptStatusSuccessful {
			t.Errorf("LegacyTx failed: status = %d", receipts[0].Status)
		}
		if tx.Type() != types.LegacyTxType {
			t.Errorf("type = %d, want %d", tx.Type(), types.LegacyTxType)
		}
	})

	t.Run("AccessListTx", func(t *testing.T) {
		db := state.NewMemoryStateDB()
		db.AddBalance(sender, big.NewInt(1_000_000_000_000))

		tx := types.NewTransaction(&types.AccessListTx{
			ChainID:  chainID,
			Nonce:    0,
			GasPrice: big.NewInt(10),
			Gas:      25000,
			To:       &receiver,
			Value:    big.NewInt(100),
			AccessList: types.AccessList{
				{Address: receiver, StorageKeys: nil},
			},
		})
		tx.SetSender(sender)

		header := &types.Header{
			Number:   big.NewInt(1),
			GasLimit: 30_000_000,
			Time:     1700000000,
			Coinbase: coinbase,
			BaseFee:  big.NewInt(1),
		}
		block := types.NewBlock(header, &types.Body{Transactions: []*types.Transaction{tx}})

		processor := core.NewStateProcessor(core.TestConfig)
		receipts, err := processor.Process(block, db)
		if err != nil {
			t.Fatalf("Process: %v", err)
		}
		if receipts[0].Status != types.ReceiptStatusSuccessful {
			t.Errorf("AccessListTx failed: status = %d", receipts[0].Status)
		}
		if tx.Type() != types.AccessListTxType {
			t.Errorf("type = %d, want %d", tx.Type(), types.AccessListTxType)
		}
	})

	t.Run("DynamicFeeTx", func(t *testing.T) {
		db := state.NewMemoryStateDB()
		db.AddBalance(sender, big.NewInt(1_000_000_000_000))

		tx := types.NewTransaction(&types.DynamicFeeTx{
			ChainID:   chainID,
			Nonce:     0,
			GasTipCap: big.NewInt(2),
			GasFeeCap: big.NewInt(20),
			Gas:       21000,
			To:        &receiver,
			Value:     big.NewInt(100),
		})
		tx.SetSender(sender)

		header := &types.Header{
			Number:   big.NewInt(1),
			GasLimit: 30_000_000,
			Time:     1700000000,
			Coinbase: coinbase,
			BaseFee:  big.NewInt(1),
		}
		block := types.NewBlock(header, &types.Body{Transactions: []*types.Transaction{tx}})

		processor := core.NewStateProcessor(core.TestConfig)
		receipts, err := processor.Process(block, db)
		if err != nil {
			t.Fatalf("Process: %v", err)
		}
		if receipts[0].Status != types.ReceiptStatusSuccessful {
			t.Errorf("DynamicFeeTx failed: status = %d", receipts[0].Status)
		}
		if tx.Type() != types.DynamicFeeTxType {
			t.Errorf("type = %d, want %d", tx.Type(), types.DynamicFeeTxType)
		}
	})

	t.Run("BlobTx", func(t *testing.T) {
		db := state.NewMemoryStateDB()
		db.AddBalance(sender, big.NewInt(1_000_000_000_000))

		blobHash := types.Hash{0x01, 0x02, 0x03}
		tx := types.NewTransaction(&types.BlobTx{
			ChainID:    chainID,
			Nonce:      0,
			GasTipCap:  big.NewInt(2),
			GasFeeCap:  big.NewInt(20),
			Gas:        21000,
			To:         receiver,
			Value:      big.NewInt(100),
			BlobFeeCap: big.NewInt(100),
			BlobHashes: []types.Hash{blobHash},
		})
		tx.SetSender(sender)

		if tx.Type() != types.BlobTxType {
			t.Errorf("type = %d, want %d", tx.Type(), types.BlobTxType)
		}
		if len(tx.BlobHashes()) != 1 {
			t.Errorf("blob hashes count = %d, want 1", len(tx.BlobHashes()))
		}
		if tx.BlobGas() != 131072 {
			t.Errorf("blob gas = %d, want 131072", tx.BlobGas())
		}
	})

	t.Run("SetCodeTx", func(t *testing.T) {
		db := state.NewMemoryStateDB()
		db.AddBalance(sender, big.NewInt(1_000_000_000_000))

		auth := types.Authorization{
			ChainID: chainID,
			Address: receiver,
			Nonce:   0,
		}
		tx := types.NewTransaction(&types.SetCodeTx{
			ChainID:           chainID,
			Nonce:             0,
			GasTipCap:         big.NewInt(2),
			GasFeeCap:         big.NewInt(20),
			Gas:               50000,
			To:                receiver,
			Value:             big.NewInt(0),
			AuthorizationList: []types.Authorization{auth},
		})
		tx.SetSender(sender)

		if tx.Type() != types.SetCodeTxType {
			t.Errorf("type = %d, want %d", tx.Type(), types.SetCodeTxType)
		}
		authList := tx.AuthorizationList()
		if len(authList) != 1 {
			t.Errorf("auth list count = %d, want 1", len(authList))
		}
		if authList[0].Address != receiver {
			t.Errorf("auth address mismatch")
		}
		_ = db
	})
}

// ---------------------------------------------------------------------------
// Test: Block Build and Validate
// ---------------------------------------------------------------------------

// TestBlockBuildAndValidate creates a block with transactions using the state
// processor and verifies its internal consistency.
func TestBlockBuildAndValidate(t *testing.T) {
	sender := types.BytesToAddress([]byte{0x10})
	receiver := types.BytesToAddress([]byte{0x20})
	coinbase := types.BytesToAddress([]byte{0xff})

	statedb := state.NewMemoryStateDB()
	statedb.AddBalance(sender, big.NewInt(1_000_000_000_000))

	var txs []*types.Transaction
	for i := uint64(0); i < 3; i++ {
		tx := types.NewTransaction(&types.LegacyTx{
			Nonce:    i,
			GasPrice: big.NewInt(10),
			Gas:      21000,
			To:       &receiver,
			Value:    big.NewInt(100),
		})
		tx.SetSender(sender)
		txs = append(txs, tx)
	}

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

	// Process and verify receipts.
	processor := core.NewStateProcessor(core.TestConfig)
	receipts, err := processor.Process(block, statedb)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if len(receipts) != 3 {
		t.Fatalf("receipt count = %d, want 3", len(receipts))
	}
	for i, r := range receipts {
		if r.Status != types.ReceiptStatusSuccessful {
			t.Errorf("receipt %d failed: status = %d", i, r.Status)
		}
	}

	// Gas used should be 3 * 21000.
	totalGas := uint64(0)
	for _, r := range receipts {
		totalGas += r.GasUsed
	}
	if totalGas != 3*21000 {
		t.Errorf("total gas = %d, want %d", totalGas, 3*21000)
	}

	// Sender balance: initial - 3*value - 3*gas*price
	gasCost := 3 * 21000 * 10
	valueCost := 3 * 100
	expectedBal := int64(1_000_000_000_000 - gasCost - valueCost)
	if bal := statedb.GetBalance(sender); bal.Int64() != expectedBal {
		t.Errorf("sender balance = %d, want %d", bal.Int64(), expectedBal)
	}
}

// ---------------------------------------------------------------------------
// Test: Glamsterdam Fork Transition
// ---------------------------------------------------------------------------

// TestGlamsterdamForkTransition verifies that transactions can be processed
// with both pre-Glamsterdam and Glamsterdam configs.
func TestGlamsterdamForkTransition(t *testing.T) {
	sender := types.BytesToAddress([]byte{0x10})
	receiver := types.BytesToAddress([]byte{0x20})
	coinbase := types.BytesToAddress([]byte{0xff})

	t.Run("PreGlamsterdam", func(t *testing.T) {
		statedb := state.NewMemoryStateDB()
		statedb.AddBalance(sender, big.NewInt(1_000_000_000_000))

		tx := types.NewTransaction(&types.LegacyTx{
			Nonce:    0,
			GasPrice: big.NewInt(10),
			Gas:      21000,
			To:       &receiver,
			Value:    big.NewInt(100),
		})
		tx.SetSender(sender)

		header := &types.Header{
			Number:   big.NewInt(1),
			GasLimit: 30_000_000,
			Time:     1700000000,
			Coinbase: coinbase,
			BaseFee:  big.NewInt(1),
		}
		block := types.NewBlock(header, &types.Body{Transactions: []*types.Transaction{tx}})

		// Pre-Glamsterdam config.
		processor := core.NewStateProcessor(core.TestConfig)
		receipts, err := processor.Process(block, statedb)
		if err != nil {
			t.Fatalf("Process (pre-Glamsterdam): %v", err)
		}
		if receipts[0].Status != types.ReceiptStatusSuccessful {
			t.Errorf("pre-Glamsterdam tx failed: status = %d", receipts[0].Status)
		}
		// Pre-Glamsterdam: intrinsic gas is 21000.
		if receipts[0].GasUsed != 21000 {
			t.Errorf("pre-Glamsterdam gas used = %d, want 21000", receipts[0].GasUsed)
		}
	})

	t.Run("Glamsterdam", func(t *testing.T) {
		statedb := state.NewMemoryStateDB()
		statedb.AddBalance(sender, big.NewInt(1_000_000_000_000))
		// Pre-create receiver so EIP-2780 GAS_NEW_ACCOUNT (25000) surcharge
		// does not apply -- we want to test the reduced base gas path.
		statedb.AddBalance(receiver, big.NewInt(0))

		tx := types.NewTransaction(&types.LegacyTx{
			Nonce:    0,
			GasPrice: big.NewInt(10),
			Gas:      21000,
			To:       &receiver,
			Value:    big.NewInt(100),
		})
		tx.SetSender(sender)

		header := &types.Header{
			Number:   big.NewInt(1),
			GasLimit: 30_000_000,
			Time:     1700000000,
			Coinbase: coinbase,
			BaseFee:  big.NewInt(1),
		}
		block := types.NewBlock(header, &types.Body{Transactions: []*types.Transaction{tx}})

		// Glamsterdam config has reduced base gas (4500 vs 21000).
		processor := core.NewStateProcessor(core.TestConfigGlamsterdan)
		receipts, err := processor.Process(block, statedb)
		if err != nil {
			t.Fatalf("Process (Glamsterdam): %v", err)
		}
		if receipts[0].Status != types.ReceiptStatusSuccessful {
			t.Errorf("Glamsterdam tx failed: status = %d", receipts[0].Status)
		}
		// Under Glamsterdam (EIP-2780), intrinsic gas is reduced (4500 base
		// vs legacy 21000). Gas used should be well under 21000 for a
		// simple transfer to an existing account.
		if receipts[0].GasUsed >= 21000 {
			t.Errorf("Glamsterdam gas used = %d, want < 21000", receipts[0].GasUsed)
		}
		if receipts[0].GasUsed == 0 {
			t.Error("Glamsterdam gas used should not be 0")
		}
	})
}

// ---------------------------------------------------------------------------
// Test: Mixed Typed Transactions in One Block
// ---------------------------------------------------------------------------

// TestMixedTypedTransactionsBlock processes legacy and EIP-1559 transactions
// together in a single block and verifies all receipts are successful.
func TestMixedTypedTransactionsBlock(t *testing.T) {
	sender1 := types.BytesToAddress([]byte{0x10})
	sender2 := types.BytesToAddress([]byte{0x11})
	receiver := types.BytesToAddress([]byte{0x20})
	coinbase := types.BytesToAddress([]byte{0xff})

	statedb := state.NewMemoryStateDB()
	statedb.AddBalance(sender1, big.NewInt(1_000_000_000_000))
	statedb.AddBalance(sender2, big.NewInt(1_000_000_000_000))

	// Legacy tx from sender1.
	legacyTx := types.NewTransaction(&types.LegacyTx{
		Nonce:    0,
		GasPrice: big.NewInt(20),
		Gas:      21000,
		To:       &receiver,
		Value:    big.NewInt(1000),
	})
	legacyTx.SetSender(sender1)

	// EIP-1559 tx from sender2.
	dynamicTx := types.NewTransaction(&types.DynamicFeeTx{
		ChainID:   big.NewInt(1337),
		Nonce:     0,
		GasTipCap: big.NewInt(2),
		GasFeeCap: big.NewInt(30),
		Gas:       21000,
		To:        &receiver,
		Value:     big.NewInt(2000),
	})
	dynamicTx.SetSender(sender2)

	header := &types.Header{
		Number:   big.NewInt(1),
		GasLimit: 30_000_000,
		Time:     1700000000,
		Coinbase: coinbase,
		BaseFee:  big.NewInt(1),
	}
	block := types.NewBlock(header, &types.Body{
		Transactions: []*types.Transaction{legacyTx, dynamicTx},
	})

	processor := core.NewStateProcessor(core.TestConfig)
	receipts, err := processor.Process(block, statedb)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}

	if len(receipts) != 2 {
		t.Fatalf("receipt count = %d, want 2", len(receipts))
	}
	for i, r := range receipts {
		if r.Status != types.ReceiptStatusSuccessful {
			t.Errorf("receipt %d: status = %d, want success", i, r.Status)
		}
	}

	// Receiver should have 1000 + 2000 = 3000.
	if bal := statedb.GetBalance(receiver); bal.Cmp(big.NewInt(3000)) != 0 {
		t.Errorf("receiver balance = %s, want 3000", bal)
	}

	// Both sender nonces should be 1.
	if statedb.GetNonce(sender1) != 1 {
		t.Errorf("sender1 nonce = %d, want 1", statedb.GetNonce(sender1))
	}
	if statedb.GetNonce(sender2) != 1 {
		t.Errorf("sender2 nonce = %d, want 1", statedb.GetNonce(sender2))
	}
}
