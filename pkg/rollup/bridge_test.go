package rollup

import (
	"math/big"
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

func TestDeposit(t *testing.T) {
	bridge := NewBridge(DefaultBridgeConfig())

	from := types.HexToAddress("0x1111111111111111111111111111111111111111")
	to := types.HexToAddress("0x2222222222222222222222222222222222222222")
	amount := big.NewInt(1_000_000_000)

	dep, err := bridge.Deposit(from, to, amount, 100)
	if err != nil {
		t.Fatalf("Deposit error: %v", err)
	}
	if dep == nil {
		t.Fatal("deposit is nil")
	}
	if dep.ID.IsZero() {
		t.Error("expected non-zero deposit ID")
	}
	if dep.From != from {
		t.Error("from address mismatch")
	}
	if dep.To != to {
		t.Error("to address mismatch")
	}
	if dep.Amount.Cmp(amount) != 0 {
		t.Error("amount mismatch")
	}
	if dep.L1Block != 100 {
		t.Errorf("L1Block: got %d, want 100", dep.L1Block)
	}
	if dep.Status != StatusPending {
		t.Errorf("Status: got %d, want %d", dep.Status, StatusPending)
	}
}

func TestDepositZeroAmount(t *testing.T) {
	bridge := NewBridge(DefaultBridgeConfig())
	from := types.HexToAddress("0x1111111111111111111111111111111111111111")
	to := types.HexToAddress("0x2222222222222222222222222222222222222222")

	_, err := bridge.Deposit(from, to, big.NewInt(0), 100)
	if err != ErrDepositZeroAmount {
		t.Errorf("expected ErrDepositZeroAmount, got %v", err)
	}

	_, err = bridge.Deposit(from, to, nil, 100)
	if err != ErrDepositZeroAmount {
		t.Errorf("expected ErrDepositZeroAmount for nil amount, got %v", err)
	}
}

func TestConfirmDeposits(t *testing.T) {
	cfg := DefaultBridgeConfig()
	cfg.ConfirmationBlocks = 10
	bridge := NewBridge(cfg)

	from := types.HexToAddress("0x1111111111111111111111111111111111111111")
	to := types.HexToAddress("0x2222222222222222222222222222222222222222")

	// Deposit at L1 block 100.
	bridge.Deposit(from, to, big.NewInt(1000), 100)

	// Confirm at block 110 (exactly enough).
	confirmed := bridge.ConfirmDeposits(110)
	if confirmed != 1 {
		t.Errorf("expected 1 confirmed, got %d", confirmed)
	}

	// No more pending to confirm.
	confirmed = bridge.ConfirmDeposits(200)
	if confirmed != 0 {
		t.Errorf("expected 0 confirmed on second call, got %d", confirmed)
	}
}

func TestConfirmDepositsNotReady(t *testing.T) {
	cfg := DefaultBridgeConfig()
	cfg.ConfirmationBlocks = 10
	bridge := NewBridge(cfg)

	from := types.HexToAddress("0x1111111111111111111111111111111111111111")
	to := types.HexToAddress("0x2222222222222222222222222222222222222222")

	// Deposit at L1 block 100.
	bridge.Deposit(from, to, big.NewInt(1000), 100)

	// Try to confirm at block 109 (not enough).
	confirmed := bridge.ConfirmDeposits(109)
	if confirmed != 0 {
		t.Errorf("expected 0 confirmed, got %d", confirmed)
	}

	// Pending should still have 1.
	pending := bridge.PendingDeposits()
	if len(pending) != 1 {
		t.Errorf("expected 1 pending, got %d", len(pending))
	}
}

func TestInitiateWithdrawal(t *testing.T) {
	bridge := NewBridge(DefaultBridgeConfig())

	from := types.HexToAddress("0x3333333333333333333333333333333333333333")
	to := types.HexToAddress("0x4444444444444444444444444444444444444444")
	amount := big.NewInt(500_000_000)

	w, err := bridge.InitiateWithdrawal(from, to, amount)
	if err != nil {
		t.Fatalf("InitiateWithdrawal error: %v", err)
	}
	if w == nil {
		t.Fatal("withdrawal is nil")
	}
	if w.ID.IsZero() {
		t.Error("expected non-zero withdrawal ID")
	}
	if w.Status != StatusPending {
		t.Errorf("Status: got %d, want %d", w.Status, StatusPending)
	}
}

func TestInitiateWithdrawalZeroAmount(t *testing.T) {
	bridge := NewBridge(DefaultBridgeConfig())
	from := types.HexToAddress("0x3333333333333333333333333333333333333333")
	to := types.HexToAddress("0x4444444444444444444444444444444444444444")

	_, err := bridge.InitiateWithdrawal(from, to, big.NewInt(0))
	if err != ErrWithdrawalZeroAmount {
		t.Errorf("expected ErrWithdrawalZeroAmount, got %v", err)
	}
}

func TestProveWithdrawal(t *testing.T) {
	bridge := NewBridge(DefaultBridgeConfig())

	from := types.HexToAddress("0x3333333333333333333333333333333333333333")
	to := types.HexToAddress("0x4444444444444444444444444444444444444444")

	w, _ := bridge.InitiateWithdrawal(from, to, big.NewInt(1000))

	proof := []byte{0xaa, 0xbb, 0xcc}
	err := bridge.ProveWithdrawal(w.ID, proof)
	if err != nil {
		t.Fatalf("ProveWithdrawal error: %v", err)
	}

	// After proving, withdrawal should no longer be in pending list.
	pending := bridge.PendingWithdrawals()
	if len(pending) != 0 {
		t.Errorf("expected 0 pending withdrawals after proof, got %d", len(pending))
	}
}

func TestProveWithdrawalNotFound(t *testing.T) {
	bridge := NewBridge(DefaultBridgeConfig())

	err := bridge.ProveWithdrawal(types.Hash{0x01}, []byte{0x01})
	if err != ErrWithdrawalNotFound {
		t.Errorf("expected ErrWithdrawalNotFound, got %v", err)
	}
}

func TestProveWithdrawalEmptyProof(t *testing.T) {
	bridge := NewBridge(DefaultBridgeConfig())

	from := types.HexToAddress("0x3333333333333333333333333333333333333333")
	to := types.HexToAddress("0x4444444444444444444444444444444444444444")
	w, _ := bridge.InitiateWithdrawal(from, to, big.NewInt(1000))

	err := bridge.ProveWithdrawal(w.ID, nil)
	if err != ErrProofEmpty {
		t.Errorf("expected ErrProofEmpty, got %v", err)
	}
}

func TestFinalizeWithdrawal(t *testing.T) {
	bridge := NewBridge(DefaultBridgeConfig())

	from := types.HexToAddress("0x3333333333333333333333333333333333333333")
	to := types.HexToAddress("0x4444444444444444444444444444444444444444")

	w, _ := bridge.InitiateWithdrawal(from, to, big.NewInt(1000))
	bridge.ProveWithdrawal(w.ID, []byte{0x01, 0x02})

	err := bridge.FinalizeWithdrawal(w.ID)
	if err != nil {
		t.Fatalf("FinalizeWithdrawal error: %v", err)
	}
}

func TestFinalizeWithdrawalNotProven(t *testing.T) {
	bridge := NewBridge(DefaultBridgeConfig())

	from := types.HexToAddress("0x3333333333333333333333333333333333333333")
	to := types.HexToAddress("0x4444444444444444444444444444444444444444")

	w, _ := bridge.InitiateWithdrawal(from, to, big.NewInt(1000))

	err := bridge.FinalizeWithdrawal(w.ID)
	if err != ErrWithdrawalNotProven {
		t.Errorf("expected ErrWithdrawalNotProven, got %v", err)
	}
}

func TestFinalizeWithdrawalNotFound(t *testing.T) {
	bridge := NewBridge(DefaultBridgeConfig())

	err := bridge.FinalizeWithdrawal(types.Hash{0xff})
	if err != ErrWithdrawalNotFound {
		t.Errorf("expected ErrWithdrawalNotFound, got %v", err)
	}
}

func TestPendingDeposits(t *testing.T) {
	bridge := NewBridge(DefaultBridgeConfig())

	from := types.HexToAddress("0x1111111111111111111111111111111111111111")
	to := types.HexToAddress("0x2222222222222222222222222222222222222222")

	bridge.Deposit(from, to, big.NewInt(100), 10)
	bridge.Deposit(from, to, big.NewInt(200), 20)

	pending := bridge.PendingDeposits()
	if len(pending) != 2 {
		t.Errorf("expected 2 pending deposits, got %d", len(pending))
	}
}

func TestMaxPendingDeposits(t *testing.T) {
	cfg := DefaultBridgeConfig()
	cfg.MaxPendingDeposits = 2
	bridge := NewBridge(cfg)

	from := types.HexToAddress("0x1111111111111111111111111111111111111111")
	to := types.HexToAddress("0x2222222222222222222222222222222222222222")

	_, err := bridge.Deposit(from, to, big.NewInt(100), 10)
	if err != nil {
		t.Fatalf("first deposit error: %v", err)
	}

	_, err = bridge.Deposit(from, to, big.NewInt(200), 20)
	if err != nil {
		t.Fatalf("second deposit error: %v", err)
	}

	// Third deposit should fail.
	_, err = bridge.Deposit(from, to, big.NewInt(300), 30)
	if err != ErrMaxPendingDeposits {
		t.Errorf("expected ErrMaxPendingDeposits, got %v", err)
	}
}

func TestPendingWithdrawals(t *testing.T) {
	bridge := NewBridge(DefaultBridgeConfig())

	from := types.HexToAddress("0x3333333333333333333333333333333333333333")
	to := types.HexToAddress("0x4444444444444444444444444444444444444444")

	bridge.InitiateWithdrawal(from, to, big.NewInt(100))
	bridge.InitiateWithdrawal(from, to, big.NewInt(200))

	pending := bridge.PendingWithdrawals()
	if len(pending) != 2 {
		t.Errorf("expected 2 pending withdrawals, got %d", len(pending))
	}
}

func TestDepositUniqueIDs(t *testing.T) {
	bridge := NewBridge(DefaultBridgeConfig())

	from := types.HexToAddress("0x1111111111111111111111111111111111111111")
	to := types.HexToAddress("0x2222222222222222222222222222222222222222")

	d1, _ := bridge.Deposit(from, to, big.NewInt(100), 10)
	d2, _ := bridge.Deposit(from, to, big.NewInt(100), 10)

	if d1.ID == d2.ID {
		t.Error("expected unique deposit IDs for sequential deposits")
	}
}
