package core

import (
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/state"
	"github.com/eth2030/eth2030/core/types"
)

// Helper to create a simple legacy transaction for IL testing.
func makeILTx(nonce uint64, gasLimit uint64, gasPrice *big.Int, to types.Address, value *big.Int) *types.Transaction {
	return types.NewTransaction(&types.LegacyTx{
		Nonce:    nonce,
		GasPrice: gasPrice,
		Gas:      gasLimit,
		To:       &to,
		Value:    value,
		V:        big.NewInt(27),
		R:        big.NewInt(1),
		S:        big.NewInt(1),
	})
}

// Helper to create a dynamic fee transaction for IL testing.
func makeILDynTx(nonce uint64, gasLimit uint64, gasTipCap, gasFeeCap *big.Int, to types.Address) *types.Transaction {
	return types.NewTransaction(&types.DynamicFeeTx{
		ChainID:   big.NewInt(1),
		Nonce:     nonce,
		GasTipCap: gasTipCap,
		GasFeeCap: gasFeeCap,
		Gas:       gasLimit,
		To:        &to,
		Value:     big.NewInt(0),
		V:         big.NewInt(0),
		R:         big.NewInt(1),
		S:         big.NewInt(1),
	})
}

// encodeTx encodes a transaction to RLP bytes.
func encodeTx(t *testing.T, tx *types.Transaction) []byte {
	t.Helper()
	encoded, err := tx.EncodeRLP()
	if err != nil {
		t.Fatalf("failed to encode tx: %v", err)
	}
	return encoded
}

func TestValidateInclusionListBasic(t *testing.T) {
	addr := types.HexToAddress("0x1234567890abcdef1234567890abcdef12345678")

	t.Run("empty transactions", func(t *testing.T) {
		il := &types.InclusionList{Slot: 1, Transactions: nil}
		err := ValidateInclusionListBasic(il)
		if err != ErrILEmptyTransactions {
			t.Errorf("expected ErrILEmptyTransactions, got %v", err)
		}
	})

	t.Run("too many transactions", func(t *testing.T) {
		il := &types.InclusionList{Slot: 1}
		for i := 0; i <= types.MaxTransactionsPerInclusionList; i++ {
			tx := makeILTx(uint64(i), 21000, big.NewInt(1e9), addr, big.NewInt(0))
			il.Transactions = append(il.Transactions, encodeTx(t, tx))
		}
		err := ValidateInclusionListBasic(il)
		if err == nil {
			t.Error("expected error for too many transactions")
		}
	})

	t.Run("valid inclusion list", func(t *testing.T) {
		il := &types.InclusionList{Slot: 1}
		tx := makeILTx(0, 21000, big.NewInt(1e9), addr, big.NewInt(0))
		il.Transactions = append(il.Transactions, encodeTx(t, tx))
		err := ValidateInclusionListBasic(il)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
	})

	t.Run("invalid transaction bytes", func(t *testing.T) {
		il := &types.InclusionList{
			Slot:         1,
			Transactions: [][]byte{{0xff, 0xfe, 0xfd}},
		}
		err := ValidateInclusionListBasic(il)
		if err == nil {
			t.Error("expected error for invalid transaction bytes")
		}
	})

	t.Run("gas limit exceeded", func(t *testing.T) {
		il := &types.InclusionList{Slot: 1}
		// Create a tx with very high gas that exceeds MaxGasPerInclusionList.
		tx := makeILTx(0, types.MaxGasPerInclusionList+1, big.NewInt(1e9), addr, big.NewInt(0))
		il.Transactions = append(il.Transactions, encodeTx(t, tx))
		err := ValidateInclusionListBasic(il)
		if err == nil {
			t.Error("expected error for gas limit exceeded")
		}
	})

	t.Run("multiple valid transactions", func(t *testing.T) {
		il := &types.InclusionList{Slot: 1}
		for i := 0; i < 5; i++ {
			tx := makeILTx(uint64(i), 21000, big.NewInt(1e9), addr, big.NewInt(0))
			il.Transactions = append(il.Transactions, encodeTx(t, tx))
		}
		err := ValidateInclusionListBasic(il)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
	})
}

func TestValidateInclusionListState(t *testing.T) {
	addr := types.HexToAddress("0xaabbccdd11223344aabbccdd11223344aabbccdd")
	baseFee := big.NewInt(1e9) // 1 gwei

	t.Run("executable transaction", func(t *testing.T) {
		statedb := state.NewMemoryStateDB()
		statedb.CreateAccount(addr)
		statedb.AddBalance(addr, big.NewInt(1e18)) // 1 ETH
		statedb.SetNonce(addr, 0)

		// Gas fee cap of 2 gwei is > 112.5% of 1 gwei base fee (1.125 gwei).
		tx := makeILDynTx(0, 21000, big.NewInt(1e8), big.NewInt(2e9), addr)

		il := &types.InclusionList{
			Slot:         1,
			Transactions: [][]byte{encodeTx(t, tx)},
			Summary:      []types.InclusionListEntry{{Address: addr, GasLimit: 21000}},
		}
		err := ValidateInclusionListState(il, statedb, baseFee)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
	})

	t.Run("insufficient gas fee cap", func(t *testing.T) {
		statedb := state.NewMemoryStateDB()
		statedb.CreateAccount(addr)
		statedb.AddBalance(addr, big.NewInt(1e18))
		statedb.SetNonce(addr, 0)

		// Gas fee cap of 1 gwei is not > 112.5% of 1 gwei base fee.
		tx := makeILDynTx(0, 21000, big.NewInt(1e8), big.NewInt(1e9), addr)

		il := &types.InclusionList{
			Slot:         1,
			Transactions: [][]byte{encodeTx(t, tx)},
			Summary:      []types.InclusionListEntry{{Address: addr, GasLimit: 21000}},
		}
		err := ValidateInclusionListState(il, statedb, baseFee)
		if err == nil {
			t.Error("expected error for insufficient gas fee cap")
		}
	})

	t.Run("stale nonce", func(t *testing.T) {
		statedb := state.NewMemoryStateDB()
		statedb.CreateAccount(addr)
		statedb.AddBalance(addr, big.NewInt(1e18))
		statedb.SetNonce(addr, 5) // Account at nonce 5

		// Tx with nonce 3 is stale.
		tx := makeILDynTx(3, 21000, big.NewInt(1e8), big.NewInt(2e9), addr)

		il := &types.InclusionList{
			Slot:         1,
			Transactions: [][]byte{encodeTx(t, tx)},
			Summary:      []types.InclusionListEntry{{Address: addr, GasLimit: 21000}},
		}
		err := ValidateInclusionListState(il, statedb, baseFee)
		if err == nil {
			t.Error("expected error for stale nonce")
		}
	})

	t.Run("insufficient balance", func(t *testing.T) {
		statedb := state.NewMemoryStateDB()
		statedb.CreateAccount(addr)
		statedb.AddBalance(addr, big.NewInt(100)) // Very low balance
		statedb.SetNonce(addr, 0)

		// Tx transferring 1 ETH with gas costs -- more than 100 wei balance.
		tx := makeILDynTx(0, 21000, big.NewInt(1e8), big.NewInt(2e9), addr)

		il := &types.InclusionList{
			Slot:         1,
			Transactions: [][]byte{encodeTx(t, tx)},
			Summary:      []types.InclusionListEntry{{Address: addr, GasLimit: 21000}},
		}
		err := ValidateInclusionListState(il, statedb, baseFee)
		if err == nil {
			t.Error("expected error for insufficient balance")
		}
	})
}

func TestCheckBlockSatisfiesInclusionList(t *testing.T) {
	addr := types.HexToAddress("0xaabbccdd11223344aabbccdd11223344aabbccdd")

	t.Run("empty inclusion list", func(t *testing.T) {
		block := types.NewBlock(&types.Header{
			Number: big.NewInt(1),
		}, &types.Body{})

		result := CheckBlockSatisfiesInclusionList(block, nil, nil)
		if !result.Satisfied {
			t.Error("expected satisfied for empty inclusion list")
		}
	})

	t.Run("block includes all IL transactions", func(t *testing.T) {
		tx := makeILTx(0, 21000, big.NewInt(1e9), addr, big.NewInt(0))
		block := types.NewBlock(&types.Header{
			Number: big.NewInt(1),
		}, &types.Body{Transactions: []*types.Transaction{tx}})

		il := &types.InclusionList{
			Slot:         1,
			Transactions: [][]byte{encodeTx(t, tx)},
		}
		result := CheckBlockSatisfiesInclusionList(block, il, nil)
		if !result.Satisfied {
			t.Error("expected satisfied when block includes IL transaction")
		}
	})

	t.Run("block missing IL transaction", func(t *testing.T) {
		ilTx := makeILTx(0, 21000, big.NewInt(1e9), addr, big.NewInt(0))
		otherTx := makeILTx(1, 21000, big.NewInt(1e9), addr, big.NewInt(0))

		block := types.NewBlock(&types.Header{
			Number: big.NewInt(1),
		}, &types.Body{Transactions: []*types.Transaction{otherTx}})

		il := &types.InclusionList{
			Slot:         1,
			Transactions: [][]byte{encodeTx(t, ilTx)},
		}
		result := CheckBlockSatisfiesInclusionList(block, il, nil)
		if result.Satisfied {
			t.Error("expected unsatisfied when block is missing IL transaction")
		}
		if len(result.MissingTxHashes) != 1 {
			t.Errorf("expected 1 missing tx hash, got %d", len(result.MissingTxHashes))
		}
	})

	t.Run("IL tx becomes invalid (nonce consumed)", func(t *testing.T) {
		ilTx := makeILTx(0, 21000, big.NewInt(1e9), addr, big.NewInt(0))

		block := types.NewBlock(&types.Header{
			Number: big.NewInt(1),
		}, &types.Body{})

		statedb := state.NewMemoryStateDB()
		statedb.CreateAccount(addr)
		statedb.SetNonce(addr, 1) // Nonce already past IL tx's nonce 0

		il := &types.InclusionList{
			Slot:         1,
			Transactions: [][]byte{encodeTx(t, ilTx)},
			Summary:      []types.InclusionListEntry{{Address: addr, GasLimit: 21000}},
		}
		result := CheckBlockSatisfiesInclusionList(block, il, statedb)
		if !result.Satisfied {
			t.Error("expected satisfied when IL tx nonce is consumed")
		}
	})

	t.Run("IL tx becomes invalid (insufficient balance)", func(t *testing.T) {
		ilTx := makeILDynTx(0, 21000, big.NewInt(1e8), big.NewInt(2e9), addr)

		block := types.NewBlock(&types.Header{
			Number: big.NewInt(1),
		}, &types.Body{})

		statedb := state.NewMemoryStateDB()
		statedb.CreateAccount(addr)
		statedb.SetNonce(addr, 0)
		statedb.AddBalance(addr, big.NewInt(100)) // Too little

		il := &types.InclusionList{
			Slot:         1,
			Transactions: [][]byte{encodeTx(t, ilTx)},
			Summary:      []types.InclusionListEntry{{Address: addr, GasLimit: 21000}},
		}
		result := CheckBlockSatisfiesInclusionList(block, il, statedb)
		if !result.Satisfied {
			t.Error("expected satisfied when IL tx sender has insufficient balance")
		}
	})

	t.Run("multiple IL txs: some included some invalid", func(t *testing.T) {
		addr2 := types.HexToAddress("0xdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef")

		// tx1: included in block
		tx1 := makeILTx(0, 21000, big.NewInt(1e9), addr, big.NewInt(0))
		// tx2: not included, but nonce is consumed
		tx2 := makeILTx(0, 21000, big.NewInt(1e9), addr2, big.NewInt(0))

		block := types.NewBlock(&types.Header{
			Number: big.NewInt(1),
		}, &types.Body{Transactions: []*types.Transaction{tx1}})

		statedb := state.NewMemoryStateDB()
		statedb.CreateAccount(addr2)
		statedb.SetNonce(addr2, 1)

		il := &types.InclusionList{
			Slot:         1,
			Transactions: [][]byte{encodeTx(t, tx1), encodeTx(t, tx2)},
			Summary: []types.InclusionListEntry{
				{Address: addr, GasLimit: 21000},
				{Address: addr2, GasLimit: 21000},
			},
		}
		result := CheckBlockSatisfiesInclusionList(block, il, statedb)
		if !result.Satisfied {
			t.Error("expected satisfied: tx1 included, tx2 nonce consumed")
		}
	})
}

func TestInclusionListStore(t *testing.T) {
	addr := types.HexToAddress("0xaabbccdd11223344aabbccdd11223344aabbccdd")

	t.Run("store and retrieve", func(t *testing.T) {
		store := NewInclusionListStore()

		tx := makeILTx(0, 21000, big.NewInt(1e9), addr, big.NewInt(0))
		il := &types.InclusionList{
			Slot:           1,
			ValidatorIndex: 42,
			CommitteeRoot:  types.HexToHash("0x1111"),
			Transactions:   [][]byte{encodeTx(t, tx)},
		}

		err := store.ProcessInclusionList(il, true)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		txs := store.GetInclusionListTransactions(1, types.HexToHash("0x1111"))
		if len(txs) != 1 {
			t.Errorf("expected 1 transaction, got %d", len(txs))
		}
	})

	t.Run("equivocation detection", func(t *testing.T) {
		store := NewInclusionListStore()
		root := types.HexToHash("0x2222")

		tx1 := makeILTx(0, 21000, big.NewInt(1e9), addr, big.NewInt(0))
		il1 := &types.InclusionList{
			Slot:           1,
			ValidatorIndex: 42,
			CommitteeRoot:  root,
			Transactions:   [][]byte{encodeTx(t, tx1)},
		}

		tx2 := makeILTx(1, 21000, big.NewInt(1e9), addr, big.NewInt(0))
		il2 := &types.InclusionList{
			Slot:           1,
			ValidatorIndex: 42, // same validator
			CommitteeRoot:  root,
			Transactions:   [][]byte{encodeTx(t, tx2)}, // different tx = equivocation
		}

		err := store.ProcessInclusionList(il1, true)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		err = store.ProcessInclusionList(il2, true)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// After equivocation, the validator's IL should be removed.
		txs := store.GetInclusionListTransactions(1, root)
		if len(txs) != 0 {
			t.Errorf("expected 0 transactions after equivocation, got %d", len(txs))
		}
	})

	t.Run("duplicate submission is no-op", func(t *testing.T) {
		store := NewInclusionListStore()
		root := types.HexToHash("0x3333")

		tx := makeILTx(0, 21000, big.NewInt(1e9), addr, big.NewInt(0))
		il := &types.InclusionList{
			Slot:           1,
			ValidatorIndex: 42,
			CommitteeRoot:  root,
			Transactions:   [][]byte{encodeTx(t, tx)},
		}

		err := store.ProcessInclusionList(il, true)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		err = store.ProcessInclusionList(il, true) // duplicate
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		txs := store.GetInclusionListTransactions(1, root)
		if len(txs) != 1 {
			t.Errorf("expected 1 transaction (no duplicates), got %d", len(txs))
		}
	})

	t.Run("reject after deadline", func(t *testing.T) {
		store := NewInclusionListStore()
		root := types.HexToHash("0x4444")

		tx := makeILTx(0, 21000, big.NewInt(1e9), addr, big.NewInt(0))
		il := &types.InclusionList{
			Slot:           1,
			ValidatorIndex: 42,
			CommitteeRoot:  root,
			Transactions:   [][]byte{encodeTx(t, tx)},
		}

		err := store.ProcessInclusionList(il, false) // after deadline
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		txs := store.GetInclusionListTransactions(1, root)
		if len(txs) != 0 {
			t.Errorf("expected 0 transactions after deadline, got %d", len(txs))
		}
	})

	t.Run("multiple validators same slot", func(t *testing.T) {
		store := NewInclusionListStore()
		root := types.HexToHash("0x5555")
		addr2 := types.HexToAddress("0xdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef")

		tx1 := makeILTx(0, 21000, big.NewInt(1e9), addr, big.NewInt(0))
		il1 := &types.InclusionList{
			Slot:           1,
			ValidatorIndex: 42,
			CommitteeRoot:  root,
			Transactions:   [][]byte{encodeTx(t, tx1)},
		}

		tx2 := makeILTx(0, 21000, big.NewInt(1e9), addr2, big.NewInt(0))
		il2 := &types.InclusionList{
			Slot:           1,
			ValidatorIndex: 99, // different validator
			CommitteeRoot:  root,
			Transactions:   [][]byte{encodeTx(t, tx2)},
		}

		_ = store.ProcessInclusionList(il1, true)
		_ = store.ProcessInclusionList(il2, true)

		txs := store.GetInclusionListTransactions(1, root)
		if len(txs) != 2 {
			t.Errorf("expected 2 transactions from 2 validators, got %d", len(txs))
		}
	})

	t.Run("prune old slots", func(t *testing.T) {
		store := NewInclusionListStore()
		root := types.HexToHash("0x6666")

		tx := makeILTx(0, 21000, big.NewInt(1e9), addr, big.NewInt(0))
		il := &types.InclusionList{
			Slot:           5,
			ValidatorIndex: 42,
			CommitteeRoot:  root,
			Transactions:   [][]byte{encodeTx(t, tx)},
		}
		_ = store.ProcessInclusionList(il, true)

		store.PruneSlot(10) // prune everything before slot 10

		txs := store.GetInclusionListTransactions(5, root)
		if len(txs) != 0 {
			t.Errorf("expected 0 transactions after pruning, got %d", len(txs))
		}
	})

	t.Run("transaction deduplication across validators", func(t *testing.T) {
		store := NewInclusionListStore()
		root := types.HexToHash("0x7777")

		// Same transaction submitted by two different validators.
		tx := makeILTx(0, 21000, big.NewInt(1e9), addr, big.NewInt(0))
		txBytes := encodeTx(t, tx)

		il1 := &types.InclusionList{
			Slot:           1,
			ValidatorIndex: 42,
			CommitteeRoot:  root,
			Transactions:   [][]byte{txBytes},
		}
		il2 := &types.InclusionList{
			Slot:           1,
			ValidatorIndex: 99,
			CommitteeRoot:  root,
			Transactions:   [][]byte{txBytes},
		}

		_ = store.ProcessInclusionList(il1, true)
		_ = store.ProcessInclusionList(il2, true)

		txs := store.GetInclusionListTransactions(1, root)
		if len(txs) != 1 {
			t.Errorf("expected 1 deduplicated transaction, got %d", len(txs))
		}
	})
}

func TestGetInclusionListFromPool(t *testing.T) {
	addr := types.HexToAddress("0xaabbccdd11223344aabbccdd11223344aabbccdd")
	baseFee := big.NewInt(1e9)

	t.Run("nil pool", func(t *testing.T) {
		il := GetInclusionListFromPool(nil, nil, baseFee)
		if len(il.Transactions) != 0 {
			t.Errorf("expected 0 transactions, got %d", len(il.Transactions))
		}
	})

	t.Run("pool with valid transactions", func(t *testing.T) {
		statedb := state.NewMemoryStateDB()
		statedb.CreateAccount(addr)
		statedb.AddBalance(addr, big.NewInt(1e18))
		statedb.SetNonce(addr, 0)

		tx := makeILDynTx(0, 21000, big.NewInt(1e8), big.NewInt(2e9), addr)
		tx.SetSender(addr)

		pool := &mockTxPool{txs: []*types.Transaction{tx}}

		il := GetInclusionListFromPool(pool, statedb, baseFee)
		if len(il.Transactions) != 1 {
			t.Errorf("expected 1 transaction, got %d", len(il.Transactions))
		}
		if len(il.Summary) != 1 {
			t.Errorf("expected 1 summary entry, got %d", len(il.Summary))
		}
	})

	t.Run("pool filters low gas fee cap", func(t *testing.T) {
		statedb := state.NewMemoryStateDB()
		statedb.CreateAccount(addr)
		statedb.AddBalance(addr, big.NewInt(1e18))

		// Gas fee cap of 1 gwei <= 112.5% of 1 gwei base fee.
		tx := makeILDynTx(0, 21000, big.NewInt(1e8), big.NewInt(1e9), addr)
		tx.SetSender(addr)

		pool := &mockTxPool{txs: []*types.Transaction{tx}}

		il := GetInclusionListFromPool(pool, statedb, baseFee)
		if len(il.Transactions) != 0 {
			t.Errorf("expected 0 transactions (filtered by fee cap), got %d", len(il.Transactions))
		}
	})

	t.Run("respects max transactions limit", func(t *testing.T) {
		statedb := state.NewMemoryStateDB()
		var txs []*types.Transaction
		for i := 0; i < types.MaxTransactionsPerInclusionList+5; i++ {
			a := types.BytesToAddress(big.NewInt(int64(i + 1)).Bytes())
			statedb.CreateAccount(a)
			statedb.AddBalance(a, big.NewInt(1e18))
			tx := makeILDynTx(0, 21000, big.NewInt(1e8), big.NewInt(2e9), a)
			tx.SetSender(a)
			txs = append(txs, tx)
		}

		pool := &mockTxPool{txs: txs}

		il := GetInclusionListFromPool(pool, statedb, baseFee)
		if len(il.Transactions) != types.MaxTransactionsPerInclusionList {
			t.Errorf("expected %d transactions (max), got %d",
				types.MaxTransactionsPerInclusionList, len(il.Transactions))
		}
	})
}

func TestInclusionListsEqual(t *testing.T) {
	addr := types.HexToAddress("0xaabbccdd11223344aabbccdd11223344aabbccdd")
	tx := makeILTx(0, 21000, big.NewInt(1e9), addr, big.NewInt(0))
	txBytes := []byte{0x01, 0x02, 0x03} // dummy bytes for comparison

	t.Run("equal lists", func(t *testing.T) {
		a := &types.InclusionList{Slot: 1, ValidatorIndex: 42, Transactions: [][]byte{txBytes}}
		b := &types.InclusionList{Slot: 1, ValidatorIndex: 42, Transactions: [][]byte{txBytes}}
		if !inclusionListsEqual(a, b) {
			t.Error("expected lists to be equal")
		}
	})

	t.Run("different slot", func(t *testing.T) {
		a := &types.InclusionList{Slot: 1, ValidatorIndex: 42, Transactions: [][]byte{txBytes}}
		b := &types.InclusionList{Slot: 2, ValidatorIndex: 42, Transactions: [][]byte{txBytes}}
		if inclusionListsEqual(a, b) {
			t.Error("expected lists to be unequal (different slot)")
		}
	})

	t.Run("different transactions", func(t *testing.T) {
		txBytes2, _ := tx.EncodeRLP()
		a := &types.InclusionList{Slot: 1, ValidatorIndex: 42, Transactions: [][]byte{txBytes}}
		b := &types.InclusionList{Slot: 1, ValidatorIndex: 42, Transactions: [][]byte{txBytes2}}
		if inclusionListsEqual(a, b) {
			t.Error("expected lists to be unequal (different transactions)")
		}
	})
}

// mockTxPool is defined in block_builder_test.go (same package).
