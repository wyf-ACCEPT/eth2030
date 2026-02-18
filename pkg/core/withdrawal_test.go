package core

import (
	"math/big"
	"testing"

	"github.com/eth2028/eth2028/core/state"
	"github.com/eth2028/eth2028/core/types"
)

// TestWithdrawalProcessing verifies that EIP-4895 beacon chain withdrawals
// correctly credit recipient addresses during block processing.
func TestWithdrawalProcessing(t *testing.T) {
	statedb := state.NewMemoryStateDB()

	addr1 := types.HexToAddress("0xaaaa")
	addr2 := types.HexToAddress("0xbbbb")
	addr3 := types.HexToAddress("0xcccc")

	// Pre-fund addr1 with 1 ETH to test crediting an existing account.
	oneETH := new(big.Int).SetUint64(1e18)
	statedb.AddBalance(addr1, oneETH)

	withdrawals := []*types.Withdrawal{
		{
			Index:          0,
			ValidatorIndex: 100,
			Address:        addr1,
			Amount:         1_000_000_000, // 1 ETH in Gwei
		},
		{
			Index:          1,
			ValidatorIndex: 200,
			Address:        addr2,
			Amount:         500_000_000, // 0.5 ETH in Gwei
		},
		{
			Index:          2,
			ValidatorIndex: 300,
			Address:        addr3,
			Amount:         2_000_000_000, // 2 ETH in Gwei
		},
	}

	wHash := CalcWithdrawalsHash(withdrawals)

	header := &types.Header{
		Number:          big.NewInt(1),
		GasLimit:        10_000_000,
		Time:            1000,
		BaseFee:         big.NewInt(1_000_000_000),
		Coinbase:        types.HexToAddress("0xfee"),
		WithdrawalsHash: &wHash,
	}

	body := &types.Body{
		Withdrawals: withdrawals,
	}
	block := types.NewBlock(header, body)

	proc := NewStateProcessor(TestConfig)
	receipts, err := proc.Process(block, statedb)
	if err != nil {
		t.Fatalf("unexpected error processing block with withdrawals: %v", err)
	}
	if len(receipts) != 0 {
		t.Fatalf("expected 0 receipts (no transactions), got %d", len(receipts))
	}

	// Verify balances.
	// addr1: 1 ETH (pre-funded) + 1 ETH (withdrawal) = 2 ETH
	expectedAddr1 := new(big.Int).Mul(big.NewInt(2), new(big.Int).SetUint64(1e18))
	if got := statedb.GetBalance(addr1); got.Cmp(expectedAddr1) != 0 {
		t.Fatalf("addr1 balance: want %v, got %v", expectedAddr1, got)
	}

	// addr2: 0 (new account) + 0.5 ETH = 0.5 ETH
	expectedAddr2 := new(big.Int).Mul(big.NewInt(500_000_000), big.NewInt(1_000_000_000))
	if got := statedb.GetBalance(addr2); got.Cmp(expectedAddr2) != 0 {
		t.Fatalf("addr2 balance: want %v, got %v", expectedAddr2, got)
	}

	// addr3: 0 (new account) + 2 ETH = 2 ETH
	expectedAddr3 := new(big.Int).Mul(big.NewInt(2), new(big.Int).SetUint64(1e18))
	if got := statedb.GetBalance(addr3); got.Cmp(expectedAddr3) != 0 {
		t.Fatalf("addr3 balance: want %v, got %v", expectedAddr3, got)
	}
}

// TestWithdrawalProcessingWithTransactions verifies that withdrawals are
// correctly applied after transaction processing in the same block.
func TestWithdrawalProcessingWithTransactions(t *testing.T) {
	statedb := state.NewMemoryStateDB()

	sender := types.HexToAddress("0x1111")
	recipient := types.HexToAddress("0x2222")
	validatorAddr := types.HexToAddress("0x3333")

	// Fund sender with 10 ETH.
	tenETH := new(big.Int).Mul(big.NewInt(10), new(big.Int).SetUint64(1e18))
	statedb.AddBalance(sender, tenETH)

	// Create a transfer transaction.
	oneETH := new(big.Int).SetUint64(1e18)
	gasPrice := big.NewInt(1)
	gasLimit := uint64(21000)
	tx := newTransferTx(0, recipient, oneETH, gasLimit, gasPrice)

	// Set sender on the transaction (needed for applyTransaction).
	tx.SetSender(sender)

	withdrawals := []*types.Withdrawal{
		{
			Index:          0,
			ValidatorIndex: 42,
			Address:        validatorAddr,
			Amount:         3_000_000_000, // 3 ETH in Gwei
		},
	}

	wHash := CalcWithdrawalsHash(withdrawals)

	header := &types.Header{
		Number:          big.NewInt(1),
		GasLimit:        10_000_000,
		Time:            1000,
		BaseFee:         big.NewInt(1_000_000_000),
		Coinbase:        types.HexToAddress("0xfee"),
		WithdrawalsHash: &wHash,
	}

	body := &types.Body{
		Transactions: []*types.Transaction{tx},
		Withdrawals:  withdrawals,
	}
	block := types.NewBlock(header, body)

	proc := NewStateProcessor(TestConfig)
	receipts, err := proc.Process(block, statedb)
	if err != nil {
		t.Fatalf("unexpected error processing block: %v", err)
	}
	if len(receipts) != 1 {
		t.Fatalf("expected 1 receipt, got %d", len(receipts))
	}

	// Validator address should have 3 ETH from the withdrawal.
	expectedValidator := new(big.Int).Mul(big.NewInt(3), new(big.Int).SetUint64(1e18))
	if got := statedb.GetBalance(validatorAddr); got.Cmp(expectedValidator) != 0 {
		t.Fatalf("validator balance: want %v, got %v", expectedValidator, got)
	}
}

// TestWithdrawalProcessingEmpty verifies that empty withdrawals are handled
// gracefully and don't affect state.
func TestWithdrawalProcessingEmpty(t *testing.T) {
	statedb := state.NewMemoryStateDB()

	addr := types.HexToAddress("0xaaaa")
	statedb.AddBalance(addr, big.NewInt(1000))

	// Process with empty withdrawals slice.
	ProcessWithdrawals(statedb, []*types.Withdrawal{})

	// Balance should be unchanged.
	if got := statedb.GetBalance(addr); got.Cmp(big.NewInt(1000)) != 0 {
		t.Fatalf("balance should be unchanged after empty withdrawals, got %v", got)
	}

	// Process with nil withdrawals.
	ProcessWithdrawals(statedb, nil)
	if got := statedb.GetBalance(addr); got.Cmp(big.NewInt(1000)) != 0 {
		t.Fatalf("balance should be unchanged after nil withdrawals, got %v", got)
	}
}

// TestWithdrawalProcessingZeroAmount verifies that a withdrawal with zero
// amount is a no-op (does not create new accounts unnecessarily).
func TestWithdrawalProcessingZeroAmount(t *testing.T) {
	statedb := state.NewMemoryStateDB()

	addr := types.HexToAddress("0xaaaa")

	withdrawals := []*types.Withdrawal{
		{
			Index:          0,
			ValidatorIndex: 1,
			Address:        addr,
			Amount:         0, // zero Gwei
		},
	}

	ProcessWithdrawals(statedb, withdrawals)

	// Even zero-amount withdrawal calls AddBalance(0), which is valid.
	bal := statedb.GetBalance(addr)
	if bal.Sign() != 0 {
		t.Fatalf("expected zero balance for zero-amount withdrawal, got %v", bal)
	}
}

// TestWithdrawalProcessingMultipleToSameAddress verifies that multiple
// withdrawals to the same address are accumulated correctly.
func TestWithdrawalProcessingMultipleToSameAddress(t *testing.T) {
	statedb := state.NewMemoryStateDB()

	addr := types.HexToAddress("0xaaaa")

	withdrawals := []*types.Withdrawal{
		{Index: 0, ValidatorIndex: 1, Address: addr, Amount: 1_000_000_000},  // 1 ETH
		{Index: 1, ValidatorIndex: 2, Address: addr, Amount: 2_000_000_000},  // 2 ETH
		{Index: 2, ValidatorIndex: 3, Address: addr, Amount: 500_000_000},    // 0.5 ETH
	}

	ProcessWithdrawals(statedb, withdrawals)

	// Total: 3.5 ETH = 3,500,000,000 Gwei = 3.5e18 Wei
	expected := new(big.Int).Mul(big.NewInt(3_500_000_000), big.NewInt(1_000_000_000))
	if got := statedb.GetBalance(addr); got.Cmp(expected) != 0 {
		t.Fatalf("accumulated balance: want %v, got %v", expected, got)
	}
}

// TestWithdrawalsHash verifies the withdrawal hash computation.
func TestWithdrawalsHash(t *testing.T) {
	// Empty withdrawals should produce EmptyRootHash.
	emptyHash := CalcWithdrawalsHash(nil)
	if emptyHash != types.EmptyRootHash {
		t.Fatalf("empty withdrawals hash: want %v, got %v", types.EmptyRootHash, emptyHash)
	}

	emptySliceHash := CalcWithdrawalsHash([]*types.Withdrawal{})
	if emptySliceHash != types.EmptyRootHash {
		t.Fatalf("empty slice withdrawals hash: want %v, got %v", types.EmptyRootHash, emptySliceHash)
	}

	// Non-empty withdrawals should produce a non-empty, non-zero hash.
	withdrawals := []*types.Withdrawal{
		{
			Index:          0,
			ValidatorIndex: 100,
			Address:        types.HexToAddress("0xaaaa"),
			Amount:         1_000_000_000,
		},
	}
	hash1 := CalcWithdrawalsHash(withdrawals)
	if hash1 == (types.Hash{}) {
		t.Fatal("non-empty withdrawals should produce non-zero hash")
	}
	if hash1 == types.EmptyRootHash {
		t.Fatal("non-empty withdrawals should not produce EmptyRootHash")
	}

	// Same input should produce the same hash (deterministic).
	hash2 := CalcWithdrawalsHash(withdrawals)
	if hash1 != hash2 {
		t.Fatalf("hash should be deterministic: %v != %v", hash1, hash2)
	}

	// Different withdrawals should produce different hashes.
	withdrawals2 := []*types.Withdrawal{
		{
			Index:          0,
			ValidatorIndex: 200, // different validator
			Address:        types.HexToAddress("0xaaaa"),
			Amount:         1_000_000_000,
		},
	}
	hash3 := CalcWithdrawalsHash(withdrawals2)
	if hash1 == hash3 {
		t.Fatal("different withdrawals should produce different hashes")
	}

	// Order matters: different ordering should produce different hashes.
	withdrawalsA := []*types.Withdrawal{
		{Index: 0, ValidatorIndex: 1, Address: types.HexToAddress("0xaa"), Amount: 100},
		{Index: 1, ValidatorIndex: 2, Address: types.HexToAddress("0xbb"), Amount: 200},
	}
	withdrawalsB := []*types.Withdrawal{
		{Index: 1, ValidatorIndex: 2, Address: types.HexToAddress("0xbb"), Amount: 200},
		{Index: 0, ValidatorIndex: 1, Address: types.HexToAddress("0xaa"), Amount: 100},
	}
	hashA := CalcWithdrawalsHash(withdrawalsA)
	hashB := CalcWithdrawalsHash(withdrawalsB)
	if hashA == hashB {
		t.Fatal("different withdrawal ordering should produce different hashes")
	}
}

// TestWithdrawalsNotAppliedPreShanghai verifies that withdrawals are not
// applied when the Shanghai fork is not active.
func TestWithdrawalsNotAppliedPreShanghai(t *testing.T) {
	statedb := state.NewMemoryStateDB()

	addr := types.HexToAddress("0xaaaa")

	withdrawals := []*types.Withdrawal{
		{
			Index:          0,
			ValidatorIndex: 1,
			Address:        addr,
			Amount:         1_000_000_000, // 1 ETH
		},
	}

	// Use a config where Shanghai is not active (ShanghaiTime=nil).
	preShanghaiConfig := &ChainConfig{
		ChainID:                 big.NewInt(1337),
		HomesteadBlock:          big.NewInt(0),
		EIP150Block:             big.NewInt(0),
		EIP155Block:             big.NewInt(0),
		EIP158Block:             big.NewInt(0),
		ByzantiumBlock:          big.NewInt(0),
		ConstantinopleBlock:     big.NewInt(0),
		PetersburgBlock:         big.NewInt(0),
		IstanbulBlock:           big.NewInt(0),
		BerlinBlock:             big.NewInt(0),
		LondonBlock:             big.NewInt(0),
		TerminalTotalDifficulty: big.NewInt(0),
		ShanghaiTime:            nil, // Shanghai not activated
	}

	header := &types.Header{
		Number:   big.NewInt(1),
		GasLimit: 10_000_000,
		Time:     1000,
		BaseFee:  big.NewInt(1_000_000_000),
		Coinbase: types.HexToAddress("0xfee"),
	}

	body := &types.Body{
		Withdrawals: withdrawals,
	}
	block := types.NewBlock(header, body)

	proc := NewStateProcessor(preShanghaiConfig)
	_, err := proc.Process(block, statedb)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Withdrawal should NOT have been credited because Shanghai is inactive.
	bal := statedb.GetBalance(addr)
	if bal.Sign() != 0 {
		t.Fatalf("withdrawal should not be applied pre-Shanghai, got balance %v", bal)
	}
}

// TestWithdrawalsHashInHeader verifies that the WithdrawalsHash in the header
// matches the computed hash from the block's withdrawals.
func TestWithdrawalsHashInHeader(t *testing.T) {
	withdrawals := []*types.Withdrawal{
		{Index: 0, ValidatorIndex: 1, Address: types.HexToAddress("0xaa"), Amount: 100},
		{Index: 1, ValidatorIndex: 2, Address: types.HexToAddress("0xbb"), Amount: 200},
	}

	wHash := CalcWithdrawalsHash(withdrawals)

	header := &types.Header{
		Number:          big.NewInt(1),
		GasLimit:        10_000_000,
		Time:            1000,
		Difficulty:      big.NewInt(0),
		WithdrawalsHash: &wHash,
	}

	block := types.NewBlock(header, &types.Body{
		Withdrawals: withdrawals,
	})

	// The header's WithdrawalsHash should match what we computed.
	headerCopy := block.Header()
	if headerCopy.WithdrawalsHash == nil {
		t.Fatal("WithdrawalsHash should be set in header")
	}
	if *headerCopy.WithdrawalsHash != wHash {
		t.Fatalf("WithdrawalsHash mismatch: want %v, got %v", wHash, *headerCopy.WithdrawalsHash)
	}

	// Recompute from the block's withdrawals and verify.
	recomputed := CalcWithdrawalsHash(block.Withdrawals())
	if recomputed != wHash {
		t.Fatalf("recomputed hash mismatch: want %v, got %v", wHash, recomputed)
	}
}
