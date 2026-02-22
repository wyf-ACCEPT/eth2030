package txpool

import (
	"math/big"
	"testing"
	"time"

	"github.com/eth2030/eth2030/core/types"
)

// vpMakeTx creates a test transaction for validation pipeline tests.
func vpMakeTx(nonce uint64, gasPrice int64, gas uint64) *types.Transaction {
	return types.NewTransaction(&types.LegacyTx{
		Nonce:    nonce,
		GasPrice: big.NewInt(gasPrice),
		Gas:      gas,
		Value:    big.NewInt(0),
		V:        big.NewInt(27),
		R:        big.NewInt(1),
		S:        big.NewInt(1),
	})
}

// vpMakeDynTx creates a test EIP-1559 tx for validation pipeline tests.
func vpMakeDynTx(nonce uint64, tipCap, feeCap int64, gas uint64) *types.Transaction {
	return types.NewTransaction(&types.DynamicFeeTx{
		Nonce:     nonce,
		GasTipCap: big.NewInt(tipCap),
		GasFeeCap: big.NewInt(feeCap),
		Gas:       gas,
		Value:     big.NewInt(0),
		ChainID:   big.NewInt(1),
		V:         big.NewInt(0),
		R:         big.NewInt(1),
		S:         big.NewInt(1),
	})
}

// mockVPState implements StateProvider for testing.
type mockVPState struct {
	nonces   map[types.Address]uint64
	balances map[types.Address]*big.Int
}

func newMockVPState() *mockVPState {
	return &mockVPState{
		nonces:   make(map[types.Address]uint64),
		balances: make(map[types.Address]*big.Int),
	}
}

func (m *mockVPState) GetNonce(addr types.Address) uint64 {
	return m.nonces[addr]
}

func (m *mockVPState) GetBalance(addr types.Address) *big.Int {
	if b, ok := m.balances[addr]; ok {
		return b
	}
	return new(big.Int)
}

// -- SyntaxCheck tests --

func TestVPSyntaxCheckNilTx(t *testing.T) {
	sc := NewSyntaxCheck(30_000_000, 128*1024)
	err := sc.Check(nil)
	if err != ErrVPNilTx {
		t.Errorf("expected ErrVPNilTx, got %v", err)
	}
}

func TestVPSyntaxCheckZeroGas(t *testing.T) {
	sc := NewSyntaxCheck(30_000_000, 128*1024)
	tx := vpMakeTx(0, 100, 0) // gas=0
	err := sc.Check(tx)
	if err != ErrVPGasZero {
		t.Errorf("expected ErrVPGasZero, got %v", err)
	}
}

func TestVPSyntaxCheckGasExceedsMax(t *testing.T) {
	sc := NewSyntaxCheck(30_000_000, 128*1024)
	tx := vpMakeTx(0, 100, 50_000_000) // exceeds 30M
	err := sc.Check(tx)
	if err != ErrVPGasExceedsMax {
		t.Errorf("expected ErrVPGasExceedsMax, got %v", err)
	}
}

func TestVPSyntaxCheckNegativeValue(t *testing.T) {
	sc := NewSyntaxCheck(30_000_000, 128*1024)
	tx := types.NewTransaction(&types.LegacyTx{
		Nonce:    0,
		GasPrice: big.NewInt(100),
		Gas:      21000,
		Value:    big.NewInt(-1),
		V:        big.NewInt(27),
		R:        big.NewInt(1),
		S:        big.NewInt(1),
	})
	err := sc.Check(tx)
	if err != ErrVPNegativeValue {
		t.Errorf("expected ErrVPNegativeValue, got %v", err)
	}
}

func TestVPSyntaxCheckFeeBelowTip(t *testing.T) {
	sc := NewSyntaxCheck(30_000_000, 128*1024)
	// feeCap=50 < tipCap=100
	tx := vpMakeDynTx(0, 100, 50, 21000)
	err := sc.Check(tx)
	if err != ErrVPFeeBelowTip {
		t.Errorf("expected ErrVPFeeBelowTip, got %v", err)
	}
}

func TestVPSyntaxCheckDataTooLarge(t *testing.T) {
	sc := NewSyntaxCheck(30_000_000, 100) // small max data
	tx := types.NewTransaction(&types.LegacyTx{
		Nonce:    0,
		GasPrice: big.NewInt(100),
		Gas:      1_000_000,
		Value:    big.NewInt(0),
		Data:     make([]byte, 200), // exceeds 100
		V:        big.NewInt(27),
		R:        big.NewInt(1),
		S:        big.NewInt(1),
	})
	err := sc.Check(tx)
	if err != ErrVPDataTooLarge {
		t.Errorf("expected ErrVPDataTooLarge, got %v", err)
	}
}

func TestVPSyntaxCheckValid(t *testing.T) {
	sc := NewSyntaxCheck(30_000_000, 128*1024)
	tx := vpMakeTx(0, 100, 21000)
	if err := sc.Check(tx); err != nil {
		t.Errorf("expected nil error for valid tx, got %v", err)
	}
}

// -- SignatureVerify tests --

func TestVPSignatureVerifyValid(t *testing.T) {
	sv := NewSignatureVerify()
	tx := vpMakeTx(0, 100, 21000)
	if err := sv.Verify(tx); err != nil {
		t.Errorf("expected nil for valid signature, got %v", err)
	}
}

func TestVPSignatureVerifyMissing(t *testing.T) {
	sv := NewSignatureVerify()
	tx := types.NewTransaction(&types.LegacyTx{
		Nonce:    0,
		GasPrice: big.NewInt(100),
		Gas:      21000,
		Value:    big.NewInt(0),
	})
	err := sv.Verify(tx)
	if err != ErrVPNoSignature {
		t.Errorf("expected ErrVPNoSignature, got %v", err)
	}
}

func TestVPSignatureVerifyNilTx(t *testing.T) {
	sv := NewSignatureVerify()
	err := sv.Verify(nil)
	if err != ErrVPNilTx {
		t.Errorf("expected ErrVPNilTx, got %v", err)
	}
}

// -- StateCheck tests --

func TestVPStateCheckNonceTooLow(t *testing.T) {
	state := newMockVPState()
	sender := types.Address{0x01}
	state.nonces[sender] = 10
	state.balances[sender] = big.NewInt(1_000_000_000)

	sc := NewStateCheck(state, 64)
	tx := vpMakeTx(5, 1, 21000) // nonce 5 < state nonce 10
	err := sc.Check(tx, sender)
	if err != ErrVPNonceTooLow {
		t.Errorf("expected ErrVPNonceTooLow, got %v", err)
	}
}

func TestVPStateCheckNonceTooHigh(t *testing.T) {
	state := newMockVPState()
	sender := types.Address{0x01}
	state.nonces[sender] = 0
	state.balances[sender] = big.NewInt(1_000_000_000_000)

	sc := NewStateCheck(state, 10)
	tx := vpMakeTx(100, 1, 21000) // nonce gap 100 > max 10
	err := sc.Check(tx, sender)
	if err != ErrVPNonceTooHigh {
		t.Errorf("expected ErrVPNonceTooHigh, got %v", err)
	}
}

func TestVPStateCheckInsufficientBalance(t *testing.T) {
	state := newMockVPState()
	sender := types.Address{0x01}
	state.nonces[sender] = 0
	state.balances[sender] = big.NewInt(1) // only 1 wei

	sc := NewStateCheck(state, 64)
	tx := vpMakeTx(0, 1_000_000, 21000) // cost = 21000 * 1M = 21B
	err := sc.Check(tx, sender)
	if err != ErrVPInsufficientBal {
		t.Errorf("expected ErrVPInsufficientBal, got %v", err)
	}
}

func TestVPStateCheckValid(t *testing.T) {
	state := newMockVPState()
	sender := types.Address{0x01}
	state.nonces[sender] = 5
	state.balances[sender] = new(big.Int).Mul(big.NewInt(1_000_000), big.NewInt(1_000_000))

	sc := NewStateCheck(state, 64)
	tx := vpMakeTx(5, 100, 21000)
	if err := sc.Check(tx, sender); err != nil {
		t.Errorf("expected nil for valid state check, got %v", err)
	}
}

// -- BlobCheck tests --

func TestVPBlobCheckNonBlobTxSkipped(t *testing.T) {
	bc := NewBlobCheck(big.NewInt(100))
	tx := vpMakeTx(0, 100, 21000) // legacy tx, not blob
	if err := bc.Check(tx); err != nil {
		t.Errorf("expected nil for non-blob tx, got %v", err)
	}
}

// -- RateLimiter tests --

func TestVPRateLimiterAllow(t *testing.T) {
	rl := NewRateLimiter(3, time.Minute)

	// First 3 should be allowed.
	for i := 0; i < 3; i++ {
		if err := rl.Allow("peer1"); err != nil {
			t.Errorf("request %d: expected allow, got %v", i, err)
		}
	}

	// 4th should be rate limited.
	if err := rl.Allow("peer1"); err != ErrVPRateLimited {
		t.Errorf("expected ErrVPRateLimited, got %v", err)
	}
}

func TestVPRateLimiterDifferentPeers(t *testing.T) {
	rl := NewRateLimiter(2, time.Minute)

	rl.Allow("peer1")
	rl.Allow("peer1")
	rl.Allow("peer2") // different peer, should be allowed

	if err := rl.Allow("peer1"); err != ErrVPRateLimited {
		t.Errorf("expected peer1 rate limited, got %v", err)
	}
	if err := rl.Allow("peer2"); err != nil {
		t.Errorf("expected peer2 allowed, got %v", err)
	}
}

func TestVPRateLimiterResetPeer(t *testing.T) {
	rl := NewRateLimiter(1, time.Minute)

	rl.Allow("peer1")
	if err := rl.Allow("peer1"); err != ErrVPRateLimited {
		t.Errorf("expected rate limited, got %v", err)
	}

	rl.ResetPeer("peer1")
	if err := rl.Allow("peer1"); err != nil {
		t.Errorf("expected allow after reset, got %v", err)
	}
}

func TestVPRateLimiterPeerCount(t *testing.T) {
	rl := NewRateLimiter(10, time.Minute)

	rl.Allow("peer1")
	rl.Allow("peer2")
	rl.Allow("peer3")

	if rl.PeerCount() != 3 {
		t.Errorf("expected 3 peers, got %d", rl.PeerCount())
	}
}

// -- Full Pipeline tests --

func TestVPFullPipelineValid(t *testing.T) {
	state := newMockVPState()
	sender := types.Address{0x01}
	state.nonces[sender] = 0
	state.balances[sender] = new(big.Int).Mul(big.NewInt(1_000_000), big.NewInt(1_000_000_000))

	config := DefaultValidationPipelineConfig()
	vp := NewValidationPipeline(config, state)

	tx := vpMakeTx(0, 1000, 21000)
	result := vp.Validate(tx, sender, "peer1")

	if !result.Valid {
		t.Fatalf("expected valid, got error: %v", result.Error)
	}
	// All 5 stages should have passed.
	if len(result.Stages) != 5 {
		t.Errorf("expected 5 stages, got %d: %v", len(result.Stages), result.Stages)
	}
}

func TestVPFullPipelineSyntaxFailure(t *testing.T) {
	state := newMockVPState()
	config := DefaultValidationPipelineConfig()
	vp := NewValidationPipeline(config, state)

	tx := vpMakeTx(0, 100, 0) // gas=0
	result := vp.Validate(tx, types.Address{0x01}, "peer1")

	if result.Valid {
		t.Error("expected invalid for gas=0")
	}
	if result.ErrorCode != ValidationSyntaxErr {
		t.Errorf("expected syntax error code, got %d", result.ErrorCode)
	}
}

func TestVPFullPipelineSignatureFailure(t *testing.T) {
	state := newMockVPState()
	config := DefaultValidationPipelineConfig()
	vp := NewValidationPipeline(config, state)

	tx := types.NewTransaction(&types.LegacyTx{
		Nonce:    0,
		GasPrice: big.NewInt(100),
		Gas:      21000,
		Value:    big.NewInt(0),
		// No V, R, S
	})
	result := vp.Validate(tx, types.Address{0x01}, "peer1")

	if result.Valid {
		t.Error("expected invalid for missing signature")
	}
	if result.ErrorCode != ValidationSignatureErr {
		t.Errorf("expected signature error code, got %d", result.ErrorCode)
	}
}

func TestVPFullPipelineStateFailure(t *testing.T) {
	state := newMockVPState()
	sender := types.Address{0x01}
	state.nonces[sender] = 10 // state nonce is 10
	state.balances[sender] = big.NewInt(1_000_000_000)

	config := DefaultValidationPipelineConfig()
	vp := NewValidationPipeline(config, state)

	tx := vpMakeTx(5, 100, 21000) // nonce 5 < state 10
	result := vp.Validate(tx, sender, "peer1")

	if result.Valid {
		t.Error("expected invalid for nonce too low")
	}
	if result.ErrorCode != ValidationStateErr {
		t.Errorf("expected state error code, got %d", result.ErrorCode)
	}
}

func TestVPFullPipelineRateLimitFailure(t *testing.T) {
	state := newMockVPState()
	sender := types.Address{0x01}
	state.balances[sender] = big.NewInt(1_000_000_000_000)

	config := DefaultValidationPipelineConfig()
	config.MaxPerPeerRate = 1 // 1 tx per window
	vp := NewValidationPipeline(config, state)

	tx1 := vpMakeTx(0, 100, 21000)
	result1 := vp.Validate(tx1, sender, "peer1")
	if !result1.Valid {
		t.Fatalf("first tx should be valid, got %v", result1.Error)
	}

	tx2 := vpMakeTx(1, 100, 21000)
	result2 := vp.Validate(tx2, sender, "peer1")
	if result2.Valid {
		t.Error("expected rate limited")
	}
	if result2.ErrorCode != ValidationRateLimitErr {
		t.Errorf("expected rate limit error code, got %d", result2.ErrorCode)
	}
}

func TestVPFullPipelineNoPeerIDSkipsRateLimit(t *testing.T) {
	state := newMockVPState()
	sender := types.Address{0x01}
	state.balances[sender] = big.NewInt(1_000_000_000_000)

	config := DefaultValidationPipelineConfig()
	config.MaxPerPeerRate = 1
	vp := NewValidationPipeline(config, state)

	// Empty peer ID should skip rate limiting.
	tx := vpMakeTx(0, 100, 21000)
	result := vp.Validate(tx, sender, "")
	if !result.Valid {
		t.Fatalf("expected valid with empty peer ID, got %v", result.Error)
	}
}

func TestVPValidateBatch(t *testing.T) {
	state := newMockVPState()
	sender := types.Address{0x01}
	state.balances[sender] = big.NewInt(1_000_000_000_000)

	config := DefaultValidationPipelineConfig()
	vp := NewValidationPipeline(config, state)

	txs := []*types.Transaction{
		vpMakeTx(0, 100, 21000),
		vpMakeTx(1, 100, 0), // invalid: gas=0
		vpMakeTx(2, 100, 21000),
	}
	senders := []types.Address{sender, sender, sender}

	results := vp.ValidateBatch(txs, senders, "")
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
	if !results[0].Valid {
		t.Errorf("tx 0 should be valid")
	}
	if results[1].Valid {
		t.Errorf("tx 1 should be invalid (gas=0)")
	}
	if !results[2].Valid {
		t.Errorf("tx 2 should be valid")
	}
}
