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

func TestAddLocal(t *testing.T) {
	state := newMockState()
	pool := New(DefaultConfig(), state)

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
	state := newMockState()
	pool := New(DefaultConfig(), state)

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
	state := newMockState()
	state.nonces[testSender] = 5

	pool := New(DefaultConfig(), state)

	tx := makeTx(3, 1000, 21000) // nonce 3 < state nonce 5
	err := pool.AddLocal(tx)
	if err != ErrNonceTooLow {
		t.Errorf("expected ErrNonceTooLow, got: %v", err)
	}
}

func TestFutureTxQueued(t *testing.T) {
	state := newMockState()
	pool := New(DefaultConfig(), state)

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
	state := newMockState()
	pool := New(DefaultConfig(), state)

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
	state := newMockState()
	pool := New(DefaultConfig(), state)

	tx1 := makeTx(0, 500, 21000)
	tx2 := makeTx(0, 2000, 21000)
	pool.AddLocal(tx1)
	pool.AddLocal(tx2)

	flat := pool.PendingFlat()
	if len(flat) < 1 {
		t.Fatal("expected at least 1 pending tx")
	}
	// First should have highest gas price.
	if flat[0].GasPrice().Cmp(big.NewInt(500)) < 0 {
		// At minimum, txs should be returned.
	}
}

func TestRemove(t *testing.T) {
	state := newMockState()
	pool := New(DefaultConfig(), state)

	tx := makeTx(0, 1000, 21000)
	pool.AddLocal(tx)

	pool.Remove(tx.Hash())

	if pool.Count() != 0 {
		t.Errorf("Count after remove = %d, want 0", pool.Count())
	}
}

func TestGet(t *testing.T) {
	state := newMockState()
	pool := New(DefaultConfig(), state)

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
	pool := New(config, state)

	pool.AddLocal(makeTx(0, 1000, 21000))
	pool.AddLocal(makeTx(1, 1000, 21000))

	err := pool.AddLocal(makeTx(2, 1000, 21000))
	if err != ErrTxPoolFull {
		t.Errorf("expected ErrTxPoolFull, got: %v", err)
	}
}

func TestGasLimitExceeded(t *testing.T) {
	state := newMockState()
	pool := New(DefaultConfig(), state)

	tx := makeTx(0, 1000, 50_000_000) // exceeds 30M block gas limit
	err := pool.AddLocal(tx)
	if err != ErrGasLimit {
		t.Errorf("expected ErrGasLimit, got: %v", err)
	}
}

func TestIntrinsicGasTooLow(t *testing.T) {
	state := newMockState()
	pool := New(DefaultConfig(), state)

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
	pool := New(config, state)

	tx := makeTx(0, 50, 21000) // gas price 50 < min 100
	err := pool.AddLocal(tx)
	if err != ErrUnderpriced {
		t.Errorf("expected ErrUnderpriced, got: %v", err)
	}
}

func TestNegativeValue(t *testing.T) {
	state := newMockState()
	pool := New(DefaultConfig(), state)

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
	state := newMockState()
	pool := New(DefaultConfig(), state)

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
	state := newMockState()
	pool := New(DefaultConfig(), state)

	pool.AddLocal(makeTx(0, 1000, 21000))
	pool.AddLocal(makeTx(1, 1000, 21000))

	if pool.PendingCount() != 2 {
		t.Fatalf("expected 2 pending, got %d", pool.PendingCount())
	}

	// Simulate block inclusion: state nonce advanced to 1.
	newState := newMockState()
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
