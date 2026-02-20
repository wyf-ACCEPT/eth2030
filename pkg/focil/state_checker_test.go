package focil

import (
	"errors"
	"math/big"
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

// --- Test helpers ---

// mockStateReader is a simple in-memory StateReader for tests.
type mockStateReader struct {
	nonces   map[types.Address]uint64
	balances map[types.Address]*big.Int
}

func newMockStateReader() *mockStateReader {
	return &mockStateReader{
		nonces:   make(map[types.Address]uint64),
		balances: make(map[types.Address]*big.Int),
	}
}

func (m *mockStateReader) GetNonce(addr types.Address) uint64 {
	return m.nonces[addr]
}

func (m *mockStateReader) GetBalance(addr types.Address) *big.Int {
	if bal, ok := m.balances[addr]; ok {
		return new(big.Int).Set(bal)
	}
	return new(big.Int)
}

func (m *mockStateReader) SetNonce(addr types.Address, nonce uint64) {
	m.nonces[addr] = nonce
}

func (m *mockStateReader) SetBalance(addr types.Address, bal *big.Int) {
	m.balances[addr] = bal
}

// makeTxWithSender creates a legacy tx and caches a sender address on it.
func makeTxWithSender(nonce uint64, gas uint64, gasPrice int64, value int64, sender types.Address) *types.Transaction {
	to := types.HexToAddress("0xdeaddeaddeaddeaddeaddeaddeaddeaddeaddead")
	tx := types.NewTransaction(&types.LegacyTx{
		Nonce:    nonce,
		GasPrice: big.NewInt(gasPrice),
		Gas:      gas,
		To:       &to,
		Value:    big.NewInt(value),
		V:        big.NewInt(27),
		R:        big.NewInt(1),
		S:        big.NewInt(1),
	})
	tx.SetSender(sender)
	return tx
}

// --- StateChecker creation ---

func TestNewStateChecker(t *testing.T) {
	sc := NewStateChecker(DefaultStateCheckerConfig())
	if sc == nil {
		t.Fatal("NewStateChecker returned nil")
	}
}

func TestNewStateReaderFromFuncs(t *testing.T) {
	addr := types.HexToAddress("0xaaaa")
	reader := NewStateReaderFromFuncs(
		func(a types.Address) uint64 { return 5 },
		func(a types.Address) *big.Int { return big.NewInt(1000) },
	)
	if reader.GetNonce(addr) != 5 {
		t.Errorf("GetNonce = %d, want 5", reader.GetNonce(addr))
	}
	if reader.GetBalance(addr).Int64() != 1000 {
		t.Errorf("GetBalance = %d, want 1000", reader.GetBalance(addr).Int64())
	}
}

func TestNewStateReaderFromFuncsNil(t *testing.T) {
	reader := NewStateReaderFromFuncs(nil, nil)
	addr := types.HexToAddress("0xaaaa")
	if reader.GetNonce(addr) != 0 {
		t.Errorf("nil NonceFunc: GetNonce = %d, want 0", reader.GetNonce(addr))
	}
	if reader.GetBalance(addr).Sign() != 0 {
		t.Errorf("nil BalanceFunc: GetBalance = %s, want 0", reader.GetBalance(addr))
	}
}

// --- CheckTransactionValidity ---

func TestCheckTransactionValidityCorrectNonce(t *testing.T) {
	sc := NewStateChecker(DefaultStateCheckerConfig())
	sender := types.HexToAddress("0x1111111111111111111111111111111111111111")
	state := newMockStateReader()
	state.SetNonce(sender, 5)

	tx := makeTxWithSender(5, 21000, 1, 0, sender)
	if err := sc.CheckTransactionValidity(tx, state); err != nil {
		t.Errorf("correct nonce should pass: %v", err)
	}
}

func TestCheckTransactionValidityWrongNonce(t *testing.T) {
	sc := NewStateChecker(DefaultStateCheckerConfig())
	sender := types.HexToAddress("0x1111111111111111111111111111111111111111")
	state := newMockStateReader()
	state.SetNonce(sender, 5)

	tx := makeTxWithSender(3, 21000, 1, 0, sender)
	err := sc.CheckTransactionValidity(tx, state)
	if !errors.Is(err, ErrStateNonceMismatch) {
		t.Errorf("wrong nonce: got %v, want ErrStateNonceMismatch", err)
	}
}

func TestCheckTransactionValidityFutureNonceAllowed(t *testing.T) {
	cfg := DefaultStateCheckerConfig()
	cfg.AllowFutureNonce = true
	sc := NewStateChecker(cfg)
	sender := types.HexToAddress("0x1111111111111111111111111111111111111111")
	state := newMockStateReader()
	state.SetNonce(sender, 5)

	// Future nonce (10 > 5) should be allowed.
	tx := makeTxWithSender(10, 21000, 1, 0, sender)
	if err := sc.CheckTransactionValidity(tx, state); err != nil {
		t.Errorf("future nonce should pass with AllowFutureNonce: %v", err)
	}

	// Past nonce (3 < 5) should still fail.
	txPast := makeTxWithSender(3, 21000, 1, 0, sender)
	err := sc.CheckTransactionValidity(txPast, state)
	if !errors.Is(err, ErrStateNonceMismatch) {
		t.Errorf("past nonce should fail: got %v", err)
	}
}

func TestCheckTransactionValiditySkipNonce(t *testing.T) {
	cfg := DefaultStateCheckerConfig()
	cfg.SkipNonceCheck = true
	sc := NewStateChecker(cfg)
	sender := types.HexToAddress("0x1111111111111111111111111111111111111111")
	state := newMockStateReader()
	state.SetNonce(sender, 5)

	// Wrong nonce but check is skipped.
	tx := makeTxWithSender(999, 21000, 1, 0, sender)
	if err := sc.CheckTransactionValidity(tx, state); err != nil {
		t.Errorf("nonce check should be skipped: %v", err)
	}
}

func TestCheckTransactionValidityNilTx(t *testing.T) {
	sc := NewStateChecker(DefaultStateCheckerConfig())
	state := newMockStateReader()
	if err := sc.CheckTransactionValidity(nil, state); !errors.Is(err, ErrStateNilTx) {
		t.Errorf("nil tx: got %v, want ErrStateNilTx", err)
	}
}

func TestCheckTransactionValidityNilReader(t *testing.T) {
	sc := NewStateChecker(DefaultStateCheckerConfig())
	sender := types.HexToAddress("0x1111111111111111111111111111111111111111")
	tx := makeTxWithSender(0, 21000, 1, 0, sender)
	if err := sc.CheckTransactionValidity(tx, nil); !errors.Is(err, ErrStateNilReader) {
		t.Errorf("nil reader: got %v, want ErrStateNilReader", err)
	}
}

func TestCheckTransactionValidityNoSender(t *testing.T) {
	sc := NewStateChecker(DefaultStateCheckerConfig())
	state := newMockStateReader()
	// Transaction without a cached sender.
	to := types.HexToAddress("0xdead")
	tx := types.NewTransaction(&types.LegacyTx{
		Nonce: 0, GasPrice: big.NewInt(1), Gas: 21000,
		To: &to, Value: big.NewInt(0),
		V: big.NewInt(27), R: big.NewInt(1), S: big.NewInt(1),
	})
	err := sc.CheckTransactionValidity(tx, state)
	if !errors.Is(err, ErrStateSenderUnknown) {
		t.Errorf("no sender: got %v, want ErrStateSenderUnknown", err)
	}
}

// --- CheckSenderBalance ---

func TestCheckSenderBalanceSufficient(t *testing.T) {
	sc := NewStateChecker(DefaultStateCheckerConfig())
	sender := types.HexToAddress("0x2222222222222222222222222222222222222222")
	state := newMockStateReader()
	// gas=21000, gasPrice=10, value=100 => cost = 210000 + 100 = 210100
	state.SetBalance(sender, big.NewInt(1_000_000))

	tx := makeTxWithSender(0, 21000, 10, 100, sender)
	if err := sc.CheckSenderBalance(tx, state); err != nil {
		t.Errorf("sufficient balance should pass: %v", err)
	}
}

func TestCheckSenderBalanceInsufficient(t *testing.T) {
	sc := NewStateChecker(DefaultStateCheckerConfig())
	sender := types.HexToAddress("0x2222222222222222222222222222222222222222")
	state := newMockStateReader()
	state.SetBalance(sender, big.NewInt(100)) // way too low

	tx := makeTxWithSender(0, 21000, 10, 0, sender)
	err := sc.CheckSenderBalance(tx, state)
	if !errors.Is(err, ErrStateInsufficientBal) {
		t.Errorf("insufficient balance: got %v, want ErrStateInsufficientBal", err)
	}
}

func TestCheckSenderBalanceSkip(t *testing.T) {
	cfg := DefaultStateCheckerConfig()
	cfg.SkipBalanceCheck = true
	sc := NewStateChecker(cfg)
	sender := types.HexToAddress("0x2222222222222222222222222222222222222222")
	state := newMockStateReader()
	state.SetBalance(sender, big.NewInt(0))

	tx := makeTxWithSender(0, 21000, 1_000_000, 0, sender)
	if err := sc.CheckSenderBalance(tx, state); err != nil {
		t.Errorf("balance check should be skipped: %v", err)
	}
}

func TestCheckSenderBalanceExact(t *testing.T) {
	sc := NewStateChecker(DefaultStateCheckerConfig())
	sender := types.HexToAddress("0x3333333333333333333333333333333333333333")
	state := newMockStateReader()
	// Exact balance: 21000 * 1 + 0 = 21000
	state.SetBalance(sender, big.NewInt(21000))

	tx := makeTxWithSender(0, 21000, 1, 0, sender)
	if err := sc.CheckSenderBalance(tx, state); err != nil {
		t.Errorf("exact balance should pass: %v", err)
	}
}

// --- CheckGasAvailability ---

func TestCheckGasAvailabilitySufficient(t *testing.T) {
	sc := NewStateChecker(DefaultStateCheckerConfig())
	sender := types.HexToAddress("0x1111")
	tx := makeTxWithSender(0, 21000, 1, 0, sender)

	if err := sc.CheckGasAvailability(tx, 30_000_000, 0); err != nil {
		t.Errorf("sufficient gas should pass: %v", err)
	}
}

func TestCheckGasAvailabilityExhausted(t *testing.T) {
	sc := NewStateChecker(DefaultStateCheckerConfig())
	sender := types.HexToAddress("0x1111")
	tx := makeTxWithSender(0, 21000, 1, 0, sender)

	err := sc.CheckGasAvailability(tx, 30_000_000, 29_990_000)
	if !errors.Is(err, ErrStateGasExhausted) {
		t.Errorf("gas exhausted: got %v, want ErrStateGasExhausted", err)
	}
}

func TestCheckGasAvailabilityNilTx(t *testing.T) {
	sc := NewStateChecker(DefaultStateCheckerConfig())
	if err := sc.CheckGasAvailability(nil, 30_000_000, 0); !errors.Is(err, ErrStateNilTx) {
		t.Errorf("nil tx: got %v, want ErrStateNilTx", err)
	}
}

func TestCheckGasAvailabilityExact(t *testing.T) {
	sc := NewStateChecker(DefaultStateCheckerConfig())
	sender := types.HexToAddress("0x1111")
	tx := makeTxWithSender(0, 21000, 1, 0, sender)

	// Exactly enough gas remaining.
	if err := sc.CheckGasAvailability(tx, 30_000_000, 30_000_000-21000); err != nil {
		t.Errorf("exact gas should pass: %v", err)
	}
}

// --- BatchValidate ---

func TestBatchValidateAllValid(t *testing.T) {
	sc := NewStateChecker(DefaultStateCheckerConfig())
	sender := types.HexToAddress("0x4444444444444444444444444444444444444444")
	state := newMockStateReader()
	state.SetNonce(sender, 0)
	state.SetBalance(sender, big.NewInt(1_000_000_000))

	tx1 := makeTxWithSender(0, 21000, 1, 0, sender)
	tx1Bytes, _ := tx1.EncodeRLP()

	il := &InclusionList{
		Slot: 100,
		Entries: []InclusionListEntry{
			{Transaction: tx1Bytes, Index: 0},
		},
	}

	result := sc.BatchValidate(il, state)
	if result.ValidCount != 1 {
		t.Errorf("ValidCount = %d, want 1", result.ValidCount)
	}
	if result.InvalidCount != 0 {
		t.Errorf("InvalidCount = %d, want 0", result.InvalidCount)
	}
	if result.TotalChecked != 1 {
		t.Errorf("TotalChecked = %d, want 1", result.TotalChecked)
	}
}

func TestBatchValidateMixedResults(t *testing.T) {
	sc := NewStateChecker(DefaultStateCheckerConfig())
	state := newMockStateReader()

	// Valid tx (decodable).
	tx1 := mkTx(0, 21000, 1)
	tx1Bytes := encTx(t, tx1)

	// Invalid tx (bad RLP encoding).
	badBytes := []byte{0xff, 0xfe, 0xfd}

	il := &InclusionList{
		Slot: 100,
		Entries: []InclusionListEntry{
			{Transaction: tx1Bytes, Index: 0},
			{Transaction: badBytes, Index: 1},
		},
	}

	result := sc.BatchValidate(il, state)
	// tx1 is valid (decoded, sender unknown => treated as valid).
	// badBytes fails decode.
	if result.ValidCount != 1 {
		t.Errorf("ValidCount = %d, want 1", result.ValidCount)
	}
	if result.InvalidCount != 1 {
		t.Errorf("InvalidCount = %d, want 1", result.InvalidCount)
	}
	if result.TotalChecked != 2 {
		t.Errorf("TotalChecked = %d, want 2", result.TotalChecked)
	}
	if result.Invalid[0].Status != TxDecodeFailed {
		t.Errorf("invalid status = %s, want decode_failed", result.Invalid[0].Status)
	}
}

func TestBatchValidateWithSenderNonceCheck(t *testing.T) {
	// Test nonce checking when sender is known (via direct method calls).
	sc := NewStateChecker(DefaultStateCheckerConfig())
	sender := types.HexToAddress("0x5555555555555555555555555555555555555555")
	state := newMockStateReader()
	state.SetNonce(sender, 0)
	state.SetBalance(sender, big.NewInt(1_000_000))

	// Wrong nonce: tx has nonce=5 but state has nonce=0.
	tx := makeTxWithSender(5, 21000, 1, 0, sender)
	err := sc.CheckTransactionValidity(tx, state)
	if err == nil {
		t.Error("wrong nonce should fail")
	}

	// Correct nonce.
	txOk := makeTxWithSender(0, 21000, 1, 0, sender)
	err = sc.CheckTransactionValidity(txOk, state)
	if err != nil {
		t.Errorf("correct nonce should pass: %v", err)
	}
}

func TestBatchValidateNilIL(t *testing.T) {
	sc := NewStateChecker(DefaultStateCheckerConfig())
	state := newMockStateReader()
	result := sc.BatchValidate(nil, state)
	if result.TotalChecked != 0 {
		t.Errorf("nil IL: TotalChecked = %d, want 0", result.TotalChecked)
	}
}

func TestBatchValidateNilReader(t *testing.T) {
	sc := NewStateChecker(DefaultStateCheckerConfig())
	tx := makeTxWithSender(0, 21000, 1, 0, types.HexToAddress("0xaaaa"))
	txBytes, _ := tx.EncodeRLP()
	il := &InclusionList{
		Slot:    100,
		Entries: []InclusionListEntry{{Transaction: txBytes}},
	}
	result := sc.BatchValidate(il, nil)
	if result.InvalidCount != 1 {
		t.Errorf("nil reader: InvalidCount = %d, want 1", result.InvalidCount)
	}
}

func TestBatchValidateInvalidEncoding(t *testing.T) {
	sc := NewStateChecker(DefaultStateCheckerConfig())
	state := newMockStateReader()
	il := &InclusionList{
		Slot: 100,
		Entries: []InclusionListEntry{
			{Transaction: []byte{0xff, 0xfe}, Index: 0},
		},
	}
	result := sc.BatchValidate(il, state)
	if result.InvalidCount != 1 {
		t.Errorf("InvalidCount = %d, want 1", result.InvalidCount)
	}
	if result.Invalid[0].Status != TxDecodeFailed {
		t.Errorf("status = %s, want decode_failed", result.Invalid[0].Status)
	}
}

// --- DeduplicateIL ---

func TestDeduplicateILNoDuplicates(t *testing.T) {
	tx1 := mkTx(0, 21000, 1)
	tx2 := mkTx(1, 21000, 2)
	tx1Bytes := encTx(t, tx1)
	tx2Bytes := encTx(t, tx2)

	il := &InclusionList{
		Slot: 100,
		Entries: []InclusionListEntry{
			{Transaction: tx1Bytes, Index: 0},
			{Transaction: tx2Bytes, Index: 1},
		},
	}

	deduped := DeduplicateIL(il)
	if len(deduped.Entries) != 2 {
		t.Errorf("entries = %d, want 2", len(deduped.Entries))
	}
}

func TestDeduplicateILWithDuplicates(t *testing.T) {
	tx := mkTx(0, 21000, 1)
	txBytes := encTx(t, tx)

	il := &InclusionList{
		Slot: 100,
		Entries: []InclusionListEntry{
			{Transaction: txBytes, Index: 0},
			{Transaction: txBytes, Index: 1}, // duplicate
			{Transaction: txBytes, Index: 2}, // duplicate
		},
	}

	deduped := DeduplicateIL(il)
	if len(deduped.Entries) != 1 {
		t.Errorf("entries = %d, want 1", len(deduped.Entries))
	}
	if deduped.Slot != 100 {
		t.Errorf("slot = %d, want 100", deduped.Slot)
	}
}

func TestDeduplicateILNil(t *testing.T) {
	if result := DeduplicateIL(nil); result != nil {
		t.Error("nil IL should return nil")
	}
}

func TestDeduplicateILReindexes(t *testing.T) {
	tx1 := mkTx(0, 21000, 1)
	tx2 := mkTx(1, 21000, 2)
	tx1Bytes := encTx(t, tx1)
	tx2Bytes := encTx(t, tx2)

	il := &InclusionList{
		Slot: 50,
		Entries: []InclusionListEntry{
			{Transaction: tx1Bytes, Index: 0},
			{Transaction: tx1Bytes, Index: 1}, // dup
			{Transaction: tx2Bytes, Index: 2},
		},
	}

	deduped := DeduplicateIL(il)
	if len(deduped.Entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(deduped.Entries))
	}
	// Indices should be resequenced.
	if deduped.Entries[0].Index != 0 {
		t.Errorf("entry 0 index = %d, want 0", deduped.Entries[0].Index)
	}
	if deduped.Entries[1].Index != 1 {
		t.Errorf("entry 1 index = %d, want 1", deduped.Entries[1].Index)
	}
}

// --- DeduplicateHashes ---

func TestDeduplicateHashes(t *testing.T) {
	h1 := types.HexToHash("0x01")
	h2 := types.HexToHash("0x02")
	h3 := types.HexToHash("0x03")

	result := DeduplicateHashes([]types.Hash{h1, h2, h1, h3, h2})
	if len(result) != 3 {
		t.Errorf("len = %d, want 3", len(result))
	}
	if result[0] != h1 || result[1] != h2 || result[2] != h3 {
		t.Error("order not preserved")
	}
}

func TestDeduplicateHashesEmpty(t *testing.T) {
	result := DeduplicateHashes(nil)
	if result != nil {
		t.Error("nil input should return nil")
	}
}

// --- TxValidity String ---

func TestTxValidityString(t *testing.T) {
	tests := []struct {
		v    TxValidity
		want string
	}{
		{TxValid, "valid"},
		{TxInvalidNonce, "invalid_nonce"},
		{TxInsufficientBalance, "insufficient_balance"},
		{TxGasExhausted, "gas_exhausted"},
		{TxDecodeFailed, "decode_failed"},
		{TxNoSender, "no_sender"},
		{TxValidity(255), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.v.String(); got != tt.want {
			t.Errorf("TxValidity(%d).String() = %q, want %q", tt.v, got, tt.want)
		}
	}
}
