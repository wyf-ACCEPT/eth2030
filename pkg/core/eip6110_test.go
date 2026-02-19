package core

import (
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

func TestDepositEventSignature(t *testing.T) {
	// The signature should be non-zero.
	if DepositEventSignature.IsZero() {
		t.Fatal("deposit event signature should not be zero")
	}
}

func TestValidateDepositRequest(t *testing.T) {
	validDep := types.DepositRequest{
		Amount: 32 * 1_000_000_000, // 32 ETH
		Index:  0,
	}
	// Set non-zero pubkey.
	validDep.Pubkey[0] = 0xAA
	validDep.Pubkey[1] = 0xBB

	if err := ValidateDepositRequest(&validDep); err != nil {
		t.Fatalf("valid deposit rejected: %v", err)
	}
}

func TestValidateDepositRequest_EmptyPubkey(t *testing.T) {
	dep := types.DepositRequest{
		Amount: 32 * 1_000_000_000,
	}
	// Pubkey is all zeros.
	if err := ValidateDepositRequest(&dep); err != ErrDepositEmptyPubkey {
		t.Fatalf("expected ErrDepositEmptyPubkey, got %v", err)
	}
}

func TestValidateDepositRequest_ZeroAmount(t *testing.T) {
	dep := types.DepositRequest{
		Amount: 0,
	}
	dep.Pubkey[0] = 0x01
	if err := ValidateDepositRequest(&dep); err != ErrDepositZeroAmount {
		t.Fatalf("expected ErrDepositZeroAmount, got %v", err)
	}
}

func TestValidateDepositRequest_BelowMinimum(t *testing.T) {
	dep := types.DepositRequest{
		Amount: 100, // way below 1 ETH
	}
	dep.Pubkey[0] = 0x01
	if err := ValidateDepositRequest(&dep); err != ErrDepositBelowMinimum {
		t.Fatalf("expected ErrDepositBelowMinimum, got %v", err)
	}
}

func TestValidateBlockDeposits_TooMany(t *testing.T) {
	deposits := make([]types.DepositRequest, MaxDepositsPerBlock+1)
	for i := range deposits {
		deposits[i].Pubkey[0] = byte(i % 256)
		deposits[i].Pubkey[1] = byte(i / 256)
		deposits[i].Amount = 32 * 1_000_000_000
	}
	if err := ValidateBlockDeposits(deposits); err != ErrTooManyDeposits {
		t.Fatalf("expected ErrTooManyDeposits, got %v", err)
	}
}

func TestValidateBlockDeposits_Valid(t *testing.T) {
	deposits := make([]types.DepositRequest, 3)
	for i := range deposits {
		deposits[i].Pubkey[0] = byte(i + 1)
		deposits[i].Amount = 32 * 1_000_000_000
	}
	if err := ValidateBlockDeposits(deposits); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParseDepositLogs(t *testing.T) {
	// Build a deposit request.
	dep := &types.DepositRequest{
		Amount: 32 * 1_000_000_000,
		Index:  42,
	}
	dep.Pubkey[0] = 0xAA
	dep.Pubkey[47] = 0xBB
	dep.WithdrawalCredentials[0] = 0x01
	dep.Signature[0] = 0xCC

	// Build ABI-encoded log data.
	logData := BuildDepositLogData(dep)

	receipt := &types.Receipt{
		Status: types.ReceiptStatusSuccessful,
		Logs: []*types.Log{
			{
				Address: DepositContractAddr,
				Topics:  []types.Hash{DepositEventSignature},
				Data:    logData,
			},
		},
	}

	deposits := ParseDepositLogs([]*types.Receipt{receipt})
	if len(deposits) != 1 {
		t.Fatalf("expected 1 deposit, got %d", len(deposits))
	}

	got := deposits[0]
	if got.Pubkey != dep.Pubkey {
		t.Error("pubkey mismatch")
	}
	if got.WithdrawalCredentials != dep.WithdrawalCredentials {
		t.Error("withdrawal credentials mismatch")
	}
	if got.Amount != dep.Amount {
		t.Errorf("amount mismatch: got %d, want %d", got.Amount, dep.Amount)
	}
	if got.Signature != dep.Signature {
		t.Error("signature mismatch")
	}
	if got.Index != dep.Index {
		t.Errorf("index mismatch: got %d, want %d", got.Index, dep.Index)
	}
}

func TestParseDepositLogs_SkipsFailedReceipt(t *testing.T) {
	dep := &types.DepositRequest{Amount: 32 * 1_000_000_000}
	dep.Pubkey[0] = 0x01
	logData := BuildDepositLogData(dep)

	receipt := &types.Receipt{
		Status: types.ReceiptStatusFailed,
		Logs: []*types.Log{
			{
				Address: DepositContractAddr,
				Topics:  []types.Hash{DepositEventSignature},
				Data:    logData,
			},
		},
	}

	deposits := ParseDepositLogs([]*types.Receipt{receipt})
	if len(deposits) != 0 {
		t.Fatalf("expected 0 deposits from failed receipt, got %d", len(deposits))
	}
}

func TestParseDepositLogs_SkipsWrongAddress(t *testing.T) {
	dep := &types.DepositRequest{Amount: 32 * 1_000_000_000}
	dep.Pubkey[0] = 0x01
	logData := BuildDepositLogData(dep)

	wrongAddr := types.HexToAddress("0x1111111111111111111111111111111111111111")
	receipt := &types.Receipt{
		Status: types.ReceiptStatusSuccessful,
		Logs: []*types.Log{
			{
				Address: wrongAddr,
				Topics:  []types.Hash{DepositEventSignature},
				Data:    logData,
			},
		},
	}

	deposits := ParseDepositLogs([]*types.Receipt{receipt})
	if len(deposits) != 0 {
		t.Fatalf("expected 0 deposits from wrong address, got %d", len(deposits))
	}
}

func TestProcessDeposits(t *testing.T) {
	vs := NewDepositValidatorSet()

	deps := []types.DepositRequest{
		{Amount: 32 * 1_000_000_000},
		{Amount: 64 * 1_000_000_000},
	}
	deps[0].Pubkey[0] = 0x01
	deps[1].Pubkey[0] = 0x02

	if err := ProcessDeposits(deps, vs); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if vs.Count() != 2 {
		t.Fatalf("expected 2 validators, got %d", vs.Count())
	}

	bal, ok := vs.GetBalance(deps[0].Pubkey)
	if !ok {
		t.Fatal("validator 0 not found")
	}
	if bal != 32*1_000_000_000 {
		t.Errorf("wrong balance: got %d", bal)
	}
}

func TestProcessDeposits_TopUp(t *testing.T) {
	vs := NewDepositValidatorSet()

	var pubkey [48]byte
	pubkey[0] = 0x01

	deps := []types.DepositRequest{
		{Amount: 32 * 1_000_000_000, Pubkey: pubkey},
		{Amount: 16 * 1_000_000_000, Pubkey: pubkey},
	}

	if err := ProcessDeposits(deps, vs); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should be 1 validator with combined balance.
	if vs.Count() != 1 {
		t.Fatalf("expected 1 validator, got %d", vs.Count())
	}

	bal, ok := vs.GetBalance(pubkey)
	if !ok {
		t.Fatal("validator not found")
	}
	expected := uint64(48 * 1_000_000_000)
	if bal != expected {
		t.Errorf("wrong balance: got %d, want %d", bal, expected)
	}
}

func TestBuildDepositLogData_Roundtrip(t *testing.T) {
	dep := &types.DepositRequest{
		Amount: 100 * 1_000_000_000,
		Index:  7,
	}
	for i := range dep.Pubkey {
		dep.Pubkey[i] = byte(i)
	}
	for i := range dep.WithdrawalCredentials {
		dep.WithdrawalCredentials[i] = byte(i + 48)
	}
	for i := range dep.Signature {
		dep.Signature[i] = byte(i + 80)
	}

	data := BuildDepositLogData(dep)
	got, err := parseDepositLogData(data)
	if err != nil {
		t.Fatalf("roundtrip parse failed: %v", err)
	}

	if got.Pubkey != dep.Pubkey {
		t.Error("pubkey roundtrip failed")
	}
	if got.WithdrawalCredentials != dep.WithdrawalCredentials {
		t.Error("withdrawal credentials roundtrip failed")
	}
	if got.Amount != dep.Amount {
		t.Errorf("amount roundtrip failed: %d vs %d", got.Amount, dep.Amount)
	}
	if got.Signature != dep.Signature {
		t.Error("signature roundtrip failed")
	}
	if got.Index != dep.Index {
		t.Errorf("index roundtrip failed: %d vs %d", got.Index, dep.Index)
	}
}
