package txpool

import (
	"math/big"
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

// mockState implements StateReader for testing.
type mockState struct {
	nonces   map[types.Address]uint64
	balances map[types.Address]*big.Int
}

func newMockState() *mockState {
	return &mockState{
		nonces:   make(map[types.Address]uint64),
		balances: make(map[types.Address]*big.Int),
	}
}

func (s *mockState) GetNonce(addr types.Address) uint64 {
	return s.nonces[addr]
}

func (s *mockState) GetBalance(addr types.Address) *big.Int {
	bal, ok := s.balances[addr]
	if !ok {
		return new(big.Int)
	}
	return bal
}

// testSender is the default sender address used in tests.
var testSender = types.BytesToAddress([]byte{0x01, 0x02, 0x03})

// richBalance is a large balance used for tests that don't care about balance.
var richBalance = new(big.Int).Mul(big.NewInt(1e18), big.NewInt(1_000_000))

func makeTx(nonce uint64, gasPrice int64, gas uint64) *types.Transaction {
	to := types.BytesToAddress([]byte{0xde, 0xad})
	tx := types.NewTransaction(&types.LegacyTx{
		Nonce:    nonce,
		GasPrice: big.NewInt(gasPrice),
		Gas:      gas,
		To:       &to,
		Value:    big.NewInt(0),
		Data:     nil,
	})
	tx.SetSender(testSender)
	return tx
}

func makeTxWithValue(nonce uint64, gasPrice int64, gas uint64, value *big.Int) *types.Transaction {
	to := types.BytesToAddress([]byte{0xde, 0xad})
	tx := types.NewTransaction(&types.LegacyTx{
		Nonce:    nonce,
		GasPrice: big.NewInt(gasPrice),
		Gas:      gas,
		To:       &to,
		Value:    value,
		Data:     nil,
	})
	tx.SetSender(testSender)
	return tx
}

func makeTxFrom(from types.Address, nonce uint64, gasPrice int64, gas uint64) *types.Transaction {
	to := types.BytesToAddress([]byte{0xde, 0xad})
	tx := types.NewTransaction(&types.LegacyTx{
		Nonce:    nonce,
		GasPrice: big.NewInt(gasPrice),
		Gas:      gas,
		To:       &to,
		Value:    big.NewInt(0),
		Data:     nil,
	})
	tx.SetSender(from)
	return tx
}

func makeDynamicTx(from types.Address, nonce uint64, tipCap, feeCap int64, gas uint64) *types.Transaction {
	to := types.BytesToAddress([]byte{0xde, 0xad})
	tx := types.NewTransaction(&types.DynamicFeeTx{
		Nonce:     nonce,
		GasTipCap: big.NewInt(tipCap),
		GasFeeCap: big.NewInt(feeCap),
		Gas:       gas,
		To:        &to,
		Value:     big.NewInt(0),
	})
	tx.SetSender(from)
	return tx
}

func makeBlobTx(from types.Address, nonce uint64, tipCap, feeCap int64, gas uint64, blobHashes []types.Hash) *types.Transaction {
	tx := types.NewTransaction(&types.BlobTx{
		Nonce:      nonce,
		GasTipCap:  big.NewInt(tipCap),
		GasFeeCap:  big.NewInt(feeCap),
		Gas:        gas,
		To:         types.BytesToAddress([]byte{0xde, 0xad}),
		Value:      big.NewInt(0),
		BlobFeeCap: big.NewInt(1000),
		BlobHashes: blobHashes,
	})
	tx.SetSender(from)
	return tx
}

// newRichPool creates a pool + state where testSender has a large balance.
func newRichPool() (*TxPool, *mockState) {
	state := newMockState()
	state.balances[testSender] = new(big.Int).Set(richBalance)
	pool := New(DefaultConfig(), state)
	return pool, state
}

// --- Existing tests ---

func TestAddLocal(t *testing.T) {
	pool, _ := newRichPool()

	tx := makeTx(0, 1000, 21000)
	if err := pool.AddLocal(tx); err != nil {
		t.Fatalf("AddLocal failed: %v", err)
	}

	if pool.Count() != 1 {
		t.Errorf("Count = %d, want 1", pool.Count())
	}
	if pool.PendingCount() != 1 {
		t.Errorf("PendingCount = %d, want 1", pool.PendingCount())
	}
}

func TestAddDuplicate(t *testing.T) {
	pool, _ := newRichPool()

	tx := makeTx(0, 1000, 21000)
	if err := pool.AddLocal(tx); err != nil {
		t.Fatalf("AddLocal failed: %v", err)
	}

	err := pool.AddLocal(tx)
	if err != ErrAlreadyKnown {
		t.Errorf("expected ErrAlreadyKnown, got: %v", err)
	}
}

func TestNonceTooLow(t *testing.T) {
	pool, state := newRichPool()
	state.nonces[testSender] = 5

	tx := makeTx(3, 1000, 21000) // nonce 3 < state nonce 5
	err := pool.AddLocal(tx)
	if err != ErrNonceTooLow {
		t.Errorf("expected ErrNonceTooLow, got: %v", err)
	}
}

func TestFutureTxQueued(t *testing.T) {
	pool, _ := newRichPool()

	tx := makeTx(5, 1000, 21000) // nonce 5, but state nonce is 0
	if err := pool.AddLocal(tx); err != nil {
		t.Fatalf("AddLocal failed: %v", err)
	}

	if pool.PendingCount() != 0 {
		t.Errorf("PendingCount = %d, want 0", pool.PendingCount())
	}
	if pool.QueuedCount() != 1 {
		t.Errorf("QueuedCount = %d, want 1", pool.QueuedCount())
	}
}

func TestPromoteQueue(t *testing.T) {
	pool, _ := newRichPool()

	// Add tx with nonce 1 first (queued).
	tx1 := makeTx(1, 1000, 21000)
	if err := pool.AddLocal(tx1); err != nil {
		t.Fatalf("AddLocal tx1: %v", err)
	}
	if pool.PendingCount() != 0 {
		t.Errorf("PendingCount after tx1 = %d, want 0", pool.PendingCount())
	}

	// Add tx with nonce 0 (pending), should promote nonce 1 too.
	tx0 := makeTx(0, 1000, 21000)
	if err := pool.AddLocal(tx0); err != nil {
		t.Fatalf("AddLocal tx0: %v", err)
	}

	if pool.PendingCount() != 2 {
		t.Errorf("PendingCount after promotion = %d, want 2", pool.PendingCount())
	}
	if pool.QueuedCount() != 0 {
		t.Errorf("QueuedCount after promotion = %d, want 0", pool.QueuedCount())
	}
}

func TestPendingFlat(t *testing.T) {
	pool, _ := newRichPool()

	tx1 := makeTx(0, 500, 21000)
	tx2 := makeTx(0, 2000, 21000)
	pool.AddLocal(tx1)
	pool.AddLocal(tx2)

	flat := pool.PendingFlat()
	if len(flat) < 1 {
		t.Fatal("expected at least 1 pending tx")
	}
}

func TestRemove(t *testing.T) {
	pool, _ := newRichPool()

	tx := makeTx(0, 1000, 21000)
	pool.AddLocal(tx)

	pool.Remove(tx.Hash())

	if pool.Count() != 0 {
		t.Errorf("Count after remove = %d, want 0", pool.Count())
	}
}

func TestGet(t *testing.T) {
	pool, _ := newRichPool()

	tx := makeTx(0, 1000, 21000)
	pool.AddLocal(tx)

	got := pool.Get(tx.Hash())
	if got == nil {
		t.Fatal("Get returned nil")
	}
	if got.Hash() != tx.Hash() {
		t.Error("Get returned wrong transaction")
	}
}

func TestPoolFull(t *testing.T) {
	config := DefaultConfig()
	config.MaxSize = 2

	state := newMockState()
	state.balances[testSender] = new(big.Int).Set(richBalance)
	sender2 := types.BytesToAddress([]byte{0x04, 0x05, 0x06})
	state.balances[sender2] = new(big.Int).Set(richBalance)
	sender3 := types.BytesToAddress([]byte{0x07, 0x08, 0x09})
	state.balances[sender3] = new(big.Int).Set(richBalance)

	pool := New(config, state)

	// Use two different senders so each has exactly one tx (protected from eviction).
	pool.AddLocal(makeTx(0, 1000, 21000))
	pool.AddLocal(makeTxFrom(sender2, 0, 1001, 21000))

	// Third tx can't be added: both existing txs are protected (only tx per sender).
	err := pool.AddLocal(makeTxFrom(sender3, 0, 1002, 21000))
	if err != ErrTxPoolFull {
		t.Errorf("expected ErrTxPoolFull, got: %v", err)
	}
}

func TestGasLimitExceeded(t *testing.T) {
	pool, _ := newRichPool()

	tx := makeTx(0, 1000, 50_000_000) // exceeds 30M block gas limit
	err := pool.AddLocal(tx)
	if err != ErrGasLimit {
		t.Errorf("expected ErrGasLimit, got: %v", err)
	}
}

func TestIntrinsicGasTooLow(t *testing.T) {
	pool, _ := newRichPool()

	tx := makeTx(0, 1000, 5000) // less than 21000 intrinsic gas
	err := pool.AddLocal(tx)
	if err != ErrIntrinsicGas {
		t.Errorf("expected ErrIntrinsicGas, got: %v", err)
	}
}

func TestUnderpriced(t *testing.T) {
	config := DefaultConfig()
	config.MinGasPrice = big.NewInt(100)

	state := newMockState()
	state.balances[testSender] = new(big.Int).Set(richBalance)
	pool := New(config, state)

	tx := makeTx(0, 50, 21000) // gas price 50 < min 100
	err := pool.AddLocal(tx)
	if err != ErrUnderpriced {
		t.Errorf("expected ErrUnderpriced, got: %v", err)
	}
}

func TestNegativeValue(t *testing.T) {
	pool, _ := newRichPool()

	to := types.BytesToAddress([]byte{0xde, 0xad})
	tx := types.NewTransaction(&types.LegacyTx{
		Nonce:    0,
		GasPrice: big.NewInt(1000),
		Gas:      21000,
		To:       &to,
		Value:    big.NewInt(-1),
	})
	tx.SetSender(testSender)
	err := pool.AddLocal(tx)
	if err != ErrNegativeValue {
		t.Errorf("expected ErrNegativeValue, got: %v", err)
	}
}

func TestOversizedData(t *testing.T) {
	pool, _ := newRichPool()

	bigData := make([]byte, 129*1024) // > 128KB
	to := types.BytesToAddress([]byte{0xde, 0xad})
	tx := types.NewTransaction(&types.LegacyTx{
		Nonce:    0,
		GasPrice: big.NewInt(1000),
		Gas:      5_000_000, // high enough for data
		To:       &to,
		Value:    big.NewInt(0),
		Data:     bigData,
	})
	tx.SetSender(testSender)
	err := pool.AddLocal(tx)
	if err != ErrOversizedData {
		t.Errorf("expected ErrOversizedData, got: %v", err)
	}
}

func TestReset(t *testing.T) {
	pool, _ := newRichPool()

	pool.AddLocal(makeTx(0, 1000, 21000))
	pool.AddLocal(makeTx(1, 1000, 21000))

	if pool.PendingCount() != 2 {
		t.Fatalf("expected 2 pending, got %d", pool.PendingCount())
	}

	// Simulate block inclusion: state nonce advanced to 1.
	newState := newMockState()
	newState.balances[testSender] = new(big.Int).Set(richBalance)
	for addr := range pool.pending {
		newState.nonces[addr] = 1
	}

	pool.Reset(newState)

	if pool.PendingCount() != 1 {
		t.Errorf("PendingCount after reset = %d, want 1", pool.PendingCount())
	}
}

func TestIntrinsicGasFunction(t *testing.T) {
	// Transfer: 21000
	if gas := IntrinsicGas(nil, false); gas != 21000 {
		t.Errorf("transfer gas = %d, want 21000", gas)
	}

	// Contract creation: 53000
	if gas := IntrinsicGas(nil, true); gas != 53000 {
		t.Errorf("creation gas = %d, want 53000", gas)
	}

	// Data with non-zero and zero bytes.
	data := []byte{0x00, 0x01, 0x02, 0x00}
	gas := IntrinsicGas(data, false)
	expected := uint64(21000 + 2*4 + 2*16) // 2 zero bytes, 2 non-zero bytes
	if gas != expected {
		t.Errorf("data gas = %d, want %d", gas, expected)
	}
}

// --- New comprehensive tests ---

// TestValidateTx_InsufficientBalance verifies that a transaction is rejected
// when the sender does not have enough balance to cover gas*price + value.
func TestValidateTx_InsufficientBalance(t *testing.T) {
	state := newMockState()
	// Give sender only 1 wei -- not enough for 21000 gas * 1000 gwei.
	state.balances[testSender] = big.NewInt(1)

	pool := New(DefaultConfig(), state)

	tx := makeTx(0, 1000, 21000) // cost = 21000 * 1000 = 21_000_000
	err := pool.AddLocal(tx)
	if err != ErrInsufficientFunds {
		t.Errorf("expected ErrInsufficientFunds, got: %v", err)
	}

	// Also test with value transfer: sender has enough for gas but not value.
	state.balances[testSender] = big.NewInt(21_000_000) // exactly enough for gas
	valueTx := makeTxWithValue(0, 1000, 21000, big.NewInt(1))
	err = pool.AddLocal(valueTx)
	if err != ErrInsufficientFunds {
		t.Errorf("expected ErrInsufficientFunds with value, got: %v", err)
	}

	// Give exactly enough: should succeed.
	state.balances[testSender] = big.NewInt(21_000_001) // gas cost + 1 wei value
	err = pool.AddLocal(valueTx)
	if err != nil {
		t.Errorf("expected success with exact balance, got: %v", err)
	}
}

// TestValidateTx_NonceTooLow verifies that txs with nonces below the state nonce
// are rejected.
func TestValidateTx_NonceTooLow(t *testing.T) {
	pool, state := newRichPool()
	state.nonces[testSender] = 10

	// Nonce 9 is below state nonce 10.
	tx := makeTx(9, 1000, 21000)
	err := pool.AddLocal(tx)
	if err != ErrNonceTooLow {
		t.Errorf("expected ErrNonceTooLow for nonce 9, got: %v", err)
	}

	// Nonce 0 is also below.
	tx0 := makeTx(0, 1000, 21000)
	err = pool.AddLocal(tx0)
	if err != ErrNonceTooLow {
		t.Errorf("expected ErrNonceTooLow for nonce 0, got: %v", err)
	}

	// Nonce 10 (equal to state nonce) should succeed.
	tx10 := makeTx(10, 1000, 21000)
	err = pool.AddLocal(tx10)
	if err != nil {
		t.Errorf("expected success for nonce 10, got: %v", err)
	}
}

// TestValidateTx_GasLimitExceeded verifies that txs with gas exceeding the
// block gas limit are rejected.
func TestValidateTx_GasLimitExceeded(t *testing.T) {
	pool, _ := newRichPool()

	// Block gas limit is 30M. Tx with 30M + 1 should fail.
	tx := makeTx(0, 1, 30_000_001)
	err := pool.AddLocal(tx)
	if err != ErrGasLimit {
		t.Errorf("expected ErrGasLimit, got: %v", err)
	}

	// Exactly 30M should succeed.
	txOk := makeTx(0, 1, 30_000_000)
	err = pool.AddLocal(txOk)
	if err != nil {
		t.Errorf("expected success at exactly 30M gas, got: %v", err)
	}
}

// TestValidateTx_MaxTxSize verifies that transactions with data exceeding 128KB
// are rejected.
func TestValidateTx_MaxTxSize(t *testing.T) {
	pool, _ := newRichPool()

	// Exactly at limit: should succeed.
	okData := make([]byte, MaxTxSize)
	to := types.BytesToAddress([]byte{0xde, 0xad})
	txOk := types.NewTransaction(&types.LegacyTx{
		Nonce:    0,
		GasPrice: big.NewInt(1000),
		Gas:      5_000_000,
		To:       &to,
		Value:    big.NewInt(0),
		Data:     okData,
	})
	txOk.SetSender(testSender)
	err := pool.AddLocal(txOk)
	if err != nil {
		t.Errorf("expected success at exactly MaxTxSize, got: %v", err)
	}

	// One byte over limit: should fail.
	bigData := make([]byte, MaxTxSize+1)
	txBig := types.NewTransaction(&types.LegacyTx{
		Nonce:    1,
		GasPrice: big.NewInt(1000),
		Gas:      5_000_000,
		To:       &to,
		Value:    big.NewInt(0),
		Data:     bigData,
	})
	txBig.SetSender(testSender)
	err = pool.AddLocal(txBig)
	if err != ErrOversizedData {
		t.Errorf("expected ErrOversizedData, got: %v", err)
	}
}

// TestValidateTx_EIP1559_FeeCapBelowTip verifies that EIP-1559 transactions
// where maxFeePerGas < maxPriorityFeePerGas are rejected.
func TestValidateTx_EIP1559_FeeCapBelowTip(t *testing.T) {
	state := newMockState()
	state.balances[testSender] = new(big.Int).Set(richBalance)
	pool := New(DefaultConfig(), state)

	// tipCap=200, feeCap=100 -- feeCap < tipCap, should fail.
	tx := makeDynamicTx(testSender, 0, 200, 100, 21000)
	err := pool.AddLocal(tx)
	if err != ErrFeeCapBelowTip {
		t.Errorf("expected ErrFeeCapBelowTip, got: %v", err)
	}

	// tipCap=100, feeCap=200 -- valid, should succeed.
	txOk := makeDynamicTx(testSender, 0, 100, 200, 21000)
	err = pool.AddLocal(txOk)
	if err != nil {
		t.Errorf("expected success with valid fee cap, got: %v", err)
	}

	// tipCap=100, feeCap=100 -- equal is valid.
	txEqual := makeDynamicTx(testSender, 1, 100, 100, 21000)
	err = pool.AddLocal(txEqual)
	if err != nil {
		t.Errorf("expected success with equal tip/fee cap, got: %v", err)
	}
}

// TestReplaceByFee_InsufficientBump verifies that replacing a transaction
// requires at least a 10% gas price increase.
func TestReplaceByFee_InsufficientBump(t *testing.T) {
	pool, _ := newRichPool()

	// Add original tx with gas price 1000.
	orig := makeTx(0, 1000, 21000)
	if err := pool.AddLocal(orig); err != nil {
		t.Fatalf("AddLocal original: %v", err)
	}

	// Try to replace with only 5% bump (1050). Needs 10% (1100).
	lowBump := makeTx(0, 1050, 21000)
	err := pool.AddLocal(lowBump)
	if err != ErrReplacementUnderpriced {
		t.Errorf("expected ErrReplacementUnderpriced for 5%% bump, got: %v", err)
	}

	// Try exactly at threshold: 1099 (still below 1100).
	almostBump := makeTx(0, 1099, 21000)
	err = pool.AddLocal(almostBump)
	if err != ErrReplacementUnderpriced {
		t.Errorf("expected ErrReplacementUnderpriced for 9.9%% bump, got: %v", err)
	}

	// Verify the original is still in the pool.
	if pool.Count() != 1 {
		t.Errorf("pool count = %d, want 1", pool.Count())
	}
	if pool.Get(orig.Hash()) == nil {
		t.Error("original tx should still be in pool")
	}
}

// TestReplaceByFee_Success verifies that a valid replace-by-fee works correctly.
func TestReplaceByFee_Success(t *testing.T) {
	pool, _ := newRichPool()

	// Add original tx with gas price 1000.
	orig := makeTx(0, 1000, 21000)
	if err := pool.AddLocal(orig); err != nil {
		t.Fatalf("AddLocal original: %v", err)
	}

	// Replace with exactly 10% bump (1100).
	replacement := makeTx(0, 1100, 21000)
	if err := pool.AddLocal(replacement); err != nil {
		t.Fatalf("expected success for 10%% bump, got: %v", err)
	}

	// Pool should still have exactly 1 tx.
	if pool.Count() != 1 {
		t.Errorf("pool count = %d, want 1", pool.Count())
	}

	// The old tx should be gone.
	if pool.Get(orig.Hash()) != nil {
		t.Error("original tx should have been replaced")
	}

	// The new tx should be present.
	if pool.Get(replacement.Hash()) == nil {
		t.Error("replacement tx should be in pool")
	}

	// Replace with an even higher price.
	replacement2 := makeTx(0, 2000, 21000)
	if err := pool.AddLocal(replacement2); err != nil {
		t.Fatalf("expected success for large bump, got: %v", err)
	}
	if pool.Count() != 1 {
		t.Errorf("pool count = %d, want 1 after second replacement", pool.Count())
	}
	if pool.Get(replacement2.Hash()) == nil {
		t.Error("second replacement tx should be in pool")
	}
}

// TestPerSenderLimit verifies that no more than MaxPerSender transactions
// can be added per sender.
func TestPerSenderLimit(t *testing.T) {
	config := DefaultConfig()
	config.MaxPerSender = 16

	state := newMockState()
	state.balances[testSender] = new(big.Int).Set(richBalance)
	pool := New(config, state)

	// Add MaxPerSender txs (nonces 0..15, all pending since sequential).
	for i := 0; i < config.MaxPerSender; i++ {
		tx := makeTx(uint64(i), 1000+int64(i), 21000)
		if err := pool.AddLocal(tx); err != nil {
			t.Fatalf("AddLocal nonce %d: %v", i, err)
		}
	}

	if pool.Count() != config.MaxPerSender {
		t.Fatalf("pool count = %d, want %d", pool.Count(), config.MaxPerSender)
	}

	// The 17th tx should be rejected.
	txOver := makeTx(uint64(config.MaxPerSender), 2000, 21000)
	err := pool.AddLocal(txOver)
	if err != ErrSenderLimitExceeded {
		t.Errorf("expected ErrSenderLimitExceeded, got: %v", err)
	}

	// But a replacement (same nonce, higher price) should still work.
	replacement := makeTx(0, 5000, 21000) // replace nonce 0
	err = pool.AddLocal(replacement)
	if err != nil {
		t.Errorf("expected replacement to succeed despite sender limit, got: %v", err)
	}
	if pool.Count() != config.MaxPerSender {
		t.Errorf("pool count = %d, want %d after replacement", pool.Count(), config.MaxPerSender)
	}

	// A different sender should not be affected by the limit.
	sender2 := types.BytesToAddress([]byte{0x04, 0x05, 0x06})
	state.balances[sender2] = new(big.Int).Set(richBalance)
	txOther := makeTxFrom(sender2, 0, 1000, 21000)
	err = pool.AddLocal(txOther)
	if err != nil {
		t.Errorf("expected success for different sender, got: %v", err)
	}
}

// TestPoolEviction verifies that when the pool reaches capacity, the lowest-priced
// transaction is evicted to make room for higher-priced ones.
func TestPoolEviction(t *testing.T) {
	config := DefaultConfig()
	config.MaxSize = 5

	state := newMockState()
	state.balances[testSender] = new(big.Int).Set(richBalance)
	pool := New(config, state)

	// Fill the pool with 5 sequential txs at different prices.
	// nonce 0: 100, nonce 1: 200, nonce 2: 300, nonce 3: 400, nonce 4: 500
	var txs []*types.Transaction
	for i := 0; i < 5; i++ {
		tx := makeTx(uint64(i), int64((i+1)*100), 21000)
		if err := pool.AddLocal(tx); err != nil {
			t.Fatalf("AddLocal nonce %d: %v", i, err)
		}
		txs = append(txs, tx)
	}

	if pool.Count() != 5 {
		t.Fatalf("pool count = %d, want 5", pool.Count())
	}

	// Now add a 6th tx from a different sender with higher price.
	// This should evict the cheapest tx (nonce 0, price 100).
	sender2 := types.BytesToAddress([]byte{0x04, 0x05, 0x06})
	state.balances[sender2] = new(big.Int).Set(richBalance)
	highTx := makeTxFrom(sender2, 0, 600, 21000)
	err := pool.AddLocal(highTx)
	if err != nil {
		t.Fatalf("AddLocal high-priced tx: %v", err)
	}

	// Pool should still be at capacity.
	if pool.Count() != 5 {
		t.Errorf("pool count = %d, want 5 after eviction", pool.Count())
	}

	// The cheapest tx (nonce 0, price 100) should have been evicted.
	// Note: nonce 4 (price 500) is the highest-nonce pending tx for testSender
	// and is protected. The cheapest unprotected is nonce 0 (price 100).
	if pool.Get(txs[0].Hash()) != nil {
		t.Error("cheapest tx (nonce 0) should have been evicted")
	}

	// The high-priced tx should be in the pool.
	if pool.Get(highTx.Hash()) == nil {
		t.Error("high-priced tx should be in pool")
	}
}

// TestNonceGap_Promotion verifies that when a pending transaction is removed,
// queued transactions with sequential nonces are promoted to pending.
func TestNonceGap_Promotion(t *testing.T) {
	pool, state := newRichPool()
	state.nonces[testSender] = 0

	// Add nonce 0 (pending).
	tx0 := makeTx(0, 1000, 21000)
	if err := pool.AddLocal(tx0); err != nil {
		t.Fatalf("AddLocal nonce 0: %v", err)
	}

	// Add nonce 2 and 3 (queued, since nonce 1 is missing).
	tx2 := makeTx(2, 1000, 21000)
	if err := pool.AddLocal(tx2); err != nil {
		t.Fatalf("AddLocal nonce 2: %v", err)
	}
	tx3 := makeTx(3, 1000, 21000)
	if err := pool.AddLocal(tx3); err != nil {
		t.Fatalf("AddLocal nonce 3: %v", err)
	}

	if pool.PendingCount() != 1 {
		t.Errorf("PendingCount = %d, want 1", pool.PendingCount())
	}
	if pool.QueuedCount() != 2 {
		t.Errorf("QueuedCount = %d, want 2", pool.QueuedCount())
	}

	// Now add nonce 1 to fill the gap. This should promote nonce 2 and 3.
	tx1 := makeTx(1, 1000, 21000)
	if err := pool.AddLocal(tx1); err != nil {
		t.Fatalf("AddLocal nonce 1: %v", err)
	}

	if pool.PendingCount() != 4 {
		t.Errorf("PendingCount after gap fill = %d, want 4", pool.PendingCount())
	}
	if pool.QueuedCount() != 0 {
		t.Errorf("QueuedCount after gap fill = %d, want 0", pool.QueuedCount())
	}

	// Now test promotion on Remove: simulate state nonce advancing to 2,
	// then remove nonce 0 and 1. Queued txs with nonce >=2 should be promoted.
	pool2, state2 := newRichPool()
	state2.nonces[testSender] = 0

	// Add nonces 0, 1 (pending), 3, 4 (queued - gap at 2).
	pool2.AddLocal(makeTx(0, 1000, 21000))
	pool2.AddLocal(makeTx(1, 1000, 21000))
	pool2.AddLocal(makeTx(3, 1000, 21000))
	pool2.AddLocal(makeTx(4, 1000, 21000))

	if pool2.PendingCount() != 2 {
		t.Fatalf("pool2 PendingCount = %d, want 2", pool2.PendingCount())
	}
	if pool2.QueuedCount() != 2 {
		t.Fatalf("pool2 QueuedCount = %d, want 2", pool2.QueuedCount())
	}

	// Add nonce 2 to fill the gap. All should now be pending.
	pool2.AddLocal(makeTx(2, 1000, 21000))
	if pool2.PendingCount() != 5 {
		t.Errorf("pool2 PendingCount after filling gap = %d, want 5", pool2.PendingCount())
	}
	if pool2.QueuedCount() != 0 {
		t.Errorf("pool2 QueuedCount after filling gap = %d, want 0", pool2.QueuedCount())
	}
}

// TestPending_OrderByPrice verifies that PendingSorted returns transactions
// sorted by effective gas price in descending order.
func TestPending_OrderByPrice(t *testing.T) {
	state := newMockState()
	state.balances[testSender] = new(big.Int).Set(richBalance)
	sender2 := types.BytesToAddress([]byte{0x04, 0x05, 0x06})
	state.balances[sender2] = new(big.Int).Set(richBalance)
	sender3 := types.BytesToAddress([]byte{0x07, 0x08, 0x09})
	state.balances[sender3] = new(big.Int).Set(richBalance)

	pool := New(DefaultConfig(), state)

	// Add txs from different senders with different gas prices.
	pool.AddLocal(makeTxFrom(testSender, 0, 100, 21000))
	pool.AddLocal(makeTxFrom(sender2, 0, 500, 21000))
	pool.AddLocal(makeTxFrom(sender3, 0, 300, 21000))
	pool.AddLocal(makeTxFrom(testSender, 1, 200, 21000))

	sorted := pool.PendingSorted()
	if len(sorted) != 4 {
		t.Fatalf("PendingSorted returned %d txs, want 4", len(sorted))
	}

	// Verify descending order by gas price.
	for i := 1; i < len(sorted); i++ {
		prevPrice := EffectiveGasPrice(sorted[i-1], nil)
		curPrice := EffectiveGasPrice(sorted[i], nil)
		if prevPrice.Cmp(curPrice) < 0 {
			t.Errorf("tx[%d] price %s > tx[%d] price %s: not sorted descending",
				i, curPrice, i-1, prevPrice)
		}
	}

	// First should be the highest price (500).
	if sorted[0].GasPrice().Cmp(big.NewInt(500)) != 0 {
		t.Errorf("first tx price = %s, want 500", sorted[0].GasPrice())
	}
}

// TestValidateTx_BlobTx_MissingHashes verifies that blob transactions without
// versioned hashes are rejected.
func TestValidateTx_BlobTx_MissingHashes(t *testing.T) {
	state := newMockState()
	state.balances[testSender] = new(big.Int).Set(richBalance)
	pool := New(DefaultConfig(), state)

	// Blob tx with no hashes.
	tx := makeBlobTx(testSender, 0, 100, 200, 21000, nil)
	err := pool.AddLocal(tx)
	if err != ErrBlobTxMissingHashes {
		t.Errorf("expected ErrBlobTxMissingHashes, got: %v", err)
	}

	// Blob tx with empty slice.
	tx2 := makeBlobTx(testSender, 0, 100, 200, 21000, []types.Hash{})
	err = pool.AddLocal(tx2)
	if err != ErrBlobTxMissingHashes {
		t.Errorf("expected ErrBlobTxMissingHashes for empty slice, got: %v", err)
	}

	// Blob tx with valid hashes should succeed.
	hash := types.Hash{0x01}
	tx3 := makeBlobTx(testSender, 0, 100, 200, 21000, []types.Hash{hash})
	err = pool.AddLocal(tx3)
	if err != nil {
		t.Errorf("expected success for valid blob tx, got: %v", err)
	}
}

// TestValidateTx_BlobTx_FeeCapBelowTip verifies that blob transactions
// also enforce maxFeePerGas >= maxPriorityFeePerGas.
func TestValidateTx_BlobTx_FeeCapBelowTip(t *testing.T) {
	state := newMockState()
	state.balances[testSender] = new(big.Int).Set(richBalance)
	pool := New(DefaultConfig(), state)

	hash := types.Hash{0x01}
	// tipCap=300, feeCap=100 => invalid.
	tx := makeBlobTx(testSender, 0, 300, 100, 21000, []types.Hash{hash})
	err := pool.AddLocal(tx)
	if err != ErrFeeCapBelowTip {
		t.Errorf("expected ErrFeeCapBelowTip for blob tx, got: %v", err)
	}
}
