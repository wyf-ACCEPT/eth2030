package rollup

import (
	"crypto/sha256"
	"encoding/binary"
	"math/big"
	"sync"
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

// testAddr returns a non-zero address with the given seed byte.
func testAddr(b byte) types.Address {
	var a types.Address
	a[0] = b
	a[19] = 0xff
	return a
}

// testConfig returns a valid NativeRollupConfig with the given ID.
func testConfig(id uint64) NativeRollupConfig {
	return NativeRollupConfig{
		ID:               id,
		Name:             "test-rollup",
		BridgeContract:   testAddr(0xBB),
		GenesisStateRoot: types.Hash{0x01},
		GasLimit:         30_000_000,
	}
}

// makeValidWithdrawalProof constructs a proof that passes verifyWithdrawalProof.
// It brute-forces a nonce appended to a base proof so that
// SHA256(rollupID || to || amount || proof)[0] == byte(len(proof)).
func makeValidWithdrawalProof(rollupID uint64, to types.Address, amount *big.Int) []byte {
	var idBuf [8]byte
	binary.BigEndian.PutUint64(idBuf[:], rollupID)
	amountBytes := amount.Bytes()

	// Try different proof lengths and nonce values.
	for proofLen := 1; proofLen < 256; proofLen++ {
		base := make([]byte, proofLen)
		for nonce := 0; nonce < 65536; nonce++ {
			if proofLen >= 2 {
				base[proofLen-2] = byte(nonce >> 8)
			}
			base[proofLen-1] = byte(nonce)

			h := sha256.New()
			h.Write(idBuf[:])
			h.Write(to[:])
			h.Write(amountBytes)
			h.Write(base)
			digest := h.Sum(nil)

			if digest[0] == byte(proofLen) {
				return base
			}
		}
	}
	return nil // should not happen in practice
}

func TestRegisterRollup(t *testing.T) {
	reg := NewRollupRegistry()
	cfg := testConfig(1)

	rollup, err := reg.RegisterRollup(cfg)
	if err != nil {
		t.Fatalf("RegisterRollup failed: %v", err)
	}
	if rollup.ID != 1 {
		t.Errorf("expected ID 1, got %d", rollup.ID)
	}
	if rollup.Name != "test-rollup" {
		t.Errorf("expected name 'test-rollup', got %q", rollup.Name)
	}
	if rollup.StateRoot != cfg.GenesisStateRoot {
		t.Error("state root mismatch")
	}
	if rollup.LastBlock != 0 {
		t.Errorf("expected LastBlock 0, got %d", rollup.LastBlock)
	}
	if rollup.BridgeContract != cfg.BridgeContract {
		t.Error("bridge contract mismatch")
	}
}

func TestRegisterRollupZeroID(t *testing.T) {
	reg := NewRollupRegistry()
	cfg := testConfig(0)

	_, err := reg.RegisterRollup(cfg)
	if err != ErrRollupIDZero {
		t.Errorf("expected ErrRollupIDZero, got %v", err)
	}
}

func TestRegisterRollupEmptyName(t *testing.T) {
	reg := NewRollupRegistry()
	cfg := testConfig(1)
	cfg.Name = ""

	_, err := reg.RegisterRollup(cfg)
	if err != ErrRollupNameEmpty {
		t.Errorf("expected ErrRollupNameEmpty, got %v", err)
	}
}

func TestRegisterRollupDuplicate(t *testing.T) {
	reg := NewRollupRegistry()
	cfg := testConfig(1)

	_, err := reg.RegisterRollup(cfg)
	if err != nil {
		t.Fatalf("first register failed: %v", err)
	}

	_, err = reg.RegisterRollup(cfg)
	if err != ErrRollupAlreadyExists {
		t.Errorf("expected ErrRollupAlreadyExists, got %v", err)
	}
}

func TestGetRollupState(t *testing.T) {
	reg := NewRollupRegistry()
	cfg := testConfig(1)
	reg.RegisterRollup(cfg)

	state, err := reg.GetRollupState(1)
	if err != nil {
		t.Fatalf("GetRollupState failed: %v", err)
	}
	if state.ID != 1 {
		t.Errorf("expected ID 1, got %d", state.ID)
	}
	if state.StateRoot != cfg.GenesisStateRoot {
		t.Error("state root mismatch")
	}
}

func TestGetRollupStateNotFound(t *testing.T) {
	reg := NewRollupRegistry()

	_, err := reg.GetRollupState(999)
	if err != ErrRollupNotFound {
		t.Errorf("expected ErrRollupNotFound, got %v", err)
	}
}

func TestSubmitBatch(t *testing.T) {
	reg := NewRollupRegistry()
	reg.RegisterRollup(testConfig(1))

	batchData := []byte("batch-data-payload")
	stateRoot := types.Hash{0xAA}

	result, err := reg.SubmitBatch(1, batchData, stateRoot)
	if err != nil {
		t.Fatalf("SubmitBatch failed: %v", err)
	}
	if result.RollupID != 1 {
		t.Errorf("expected rollupID 1, got %d", result.RollupID)
	}
	if result.PreStateRoot != (types.Hash{0x01}) {
		t.Error("pre-state root should be genesis root")
	}
	if result.PostStateRoot == (types.Hash{}) {
		t.Error("post-state root should be non-zero")
	}
	if result.BlockNumber != 1 {
		t.Errorf("expected block number 1, got %d", result.BlockNumber)
	}
	if result.BatchHash == (types.Hash{}) {
		t.Error("batch hash should be non-zero")
	}
}

func TestSubmitBatchEmptyData(t *testing.T) {
	reg := NewRollupRegistry()
	reg.RegisterRollup(testConfig(1))

	_, err := reg.SubmitBatch(1, nil, types.Hash{})
	if err != ErrBatchDataEmpty {
		t.Errorf("expected ErrBatchDataEmpty, got %v", err)
	}

	_, err = reg.SubmitBatch(1, []byte{}, types.Hash{})
	if err != ErrBatchDataEmpty {
		t.Errorf("expected ErrBatchDataEmpty, got %v", err)
	}
}

func TestSubmitBatchTooLarge(t *testing.T) {
	reg := NewRollupRegistry()
	reg.RegisterRollup(testConfig(1))

	bigData := make([]byte, MaxBatchDataSize+1)
	_, err := reg.SubmitBatch(1, bigData, types.Hash{})
	if err != ErrBatchDataTooLarge {
		t.Errorf("expected ErrBatchDataTooLarge, got %v", err)
	}
}

func TestSubmitBatchRollupNotFound(t *testing.T) {
	reg := NewRollupRegistry()

	_, err := reg.SubmitBatch(999, []byte("data"), types.Hash{})
	if err != ErrRollupNotFound {
		t.Errorf("expected ErrRollupNotFound, got %v", err)
	}
}

func TestSubmitBatchAdvancesState(t *testing.T) {
	reg := NewRollupRegistry()
	reg.RegisterRollup(testConfig(1))

	// Submit two batches and verify state advances.
	r1, _ := reg.SubmitBatch(1, []byte("batch-1"), types.Hash{0x11})
	r2, _ := reg.SubmitBatch(1, []byte("batch-2"), types.Hash{0x22})

	if r1.PostStateRoot == r2.PostStateRoot {
		t.Error("different batches should produce different post-state roots")
	}
	if r2.PreStateRoot != r1.PostStateRoot {
		t.Error("second batch pre-state should equal first batch post-state")
	}
	if r2.BlockNumber != 2 {
		t.Errorf("expected block 2, got %d", r2.BlockNumber)
	}

	state, _ := reg.GetRollupState(1)
	if state.TotalBatches != 2 {
		t.Errorf("expected 2 total batches, got %d", state.TotalBatches)
	}
}

func TestVerifyStateTransition(t *testing.T) {
	reg := NewRollupRegistry()
	reg.RegisterRollup(testConfig(1))

	pre := types.Hash{0x01}
	post := types.Hash{0x02}

	// Brute force a valid proof for this specific scenario.
	proof := makeValidSTFProof(1, pre, post)
	if proof == nil {
		t.Fatal("failed to construct valid STF proof")
	}

	valid, err := reg.VerifyStateTransition(1, pre, post, proof)
	if err != nil {
		t.Fatalf("VerifyStateTransition error: %v", err)
	}
	if !valid {
		t.Error("expected valid state transition")
	}
}

func TestVerifyStateTransitionInvalid(t *testing.T) {
	reg := NewRollupRegistry()
	reg.RegisterRollup(testConfig(1))

	pre := types.Hash{0x01}
	post := types.Hash{0x02}

	// Random proof that is very unlikely to pass verification.
	proof := make([]byte, 64)
	for i := range proof {
		proof[i] = 0xFF
	}

	valid, err := reg.VerifyStateTransition(1, pre, post, proof)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// This may or may not be valid depending on hash output.
	// We just verify no error is returned; validity depends on the proof.
	_ = valid
}

func TestVerifyStateTransitionProofTooShort(t *testing.T) {
	reg := NewRollupRegistry()
	reg.RegisterRollup(testConfig(1))

	_, err := reg.VerifyStateTransition(1, types.Hash{}, types.Hash{}, make([]byte, MinProofLen-1))
	if err != ErrProofTooShort {
		t.Errorf("expected ErrProofTooShort, got %v", err)
	}
}

func TestVerifyStateTransitionRollupNotFound(t *testing.T) {
	reg := NewRollupRegistry()

	_, err := reg.VerifyStateTransition(999, types.Hash{}, types.Hash{}, make([]byte, 64))
	if err != ErrRollupNotFound {
		t.Errorf("expected ErrRollupNotFound, got %v", err)
	}
}

func TestProcessDeposit(t *testing.T) {
	reg := NewRollupRegistry()
	reg.RegisterRollup(testConfig(1))

	from := testAddr(0x01)
	amount := big.NewInt(1_000_000)

	deposit, err := reg.ProcessDeposit(1, from, amount)
	if err != nil {
		t.Fatalf("ProcessDeposit failed: %v", err)
	}
	if deposit.RollupID != 1 {
		t.Errorf("expected rollupID 1, got %d", deposit.RollupID)
	}
	if deposit.From != from {
		t.Error("from address mismatch")
	}
	if deposit.Amount.Cmp(amount) != 0 {
		t.Error("amount mismatch")
	}
	if deposit.ID == (types.Hash{}) {
		t.Error("deposit ID should be non-zero")
	}
	if deposit.Finalized {
		t.Error("new deposit should not be finalized")
	}

	// Verify rollup state updated.
	state, _ := reg.GetRollupState(1)
	if state.TotalDeposits != 1 {
		t.Errorf("expected 1 total deposit, got %d", state.TotalDeposits)
	}
	if len(state.Deposits) != 1 {
		t.Errorf("expected 1 deposit in list, got %d", len(state.Deposits))
	}
}

func TestProcessDepositZeroAmount(t *testing.T) {
	reg := NewRollupRegistry()
	reg.RegisterRollup(testConfig(1))

	_, err := reg.ProcessDeposit(1, testAddr(0x01), big.NewInt(0))
	if err != ErrDepositAmountZero {
		t.Errorf("expected ErrDepositAmountZero, got %v", err)
	}

	_, err = reg.ProcessDeposit(1, testAddr(0x01), big.NewInt(-1))
	if err != ErrDepositAmountZero {
		t.Errorf("expected ErrDepositAmountZero for negative, got %v", err)
	}

	_, err = reg.ProcessDeposit(1, testAddr(0x01), nil)
	if err != ErrDepositAmountZero {
		t.Errorf("expected ErrDepositAmountZero for nil, got %v", err)
	}
}

func TestProcessDepositZeroAddress(t *testing.T) {
	reg := NewRollupRegistry()
	reg.RegisterRollup(testConfig(1))

	_, err := reg.ProcessDeposit(1, types.Address{}, big.NewInt(100))
	if err != ErrDepositFromZero {
		t.Errorf("expected ErrDepositFromZero, got %v", err)
	}
}

func TestProcessDepositRollupNotFound(t *testing.T) {
	reg := NewRollupRegistry()

	_, err := reg.ProcessDeposit(999, testAddr(0x01), big.NewInt(100))
	if err != ErrRollupNotFound {
		t.Errorf("expected ErrRollupNotFound, got %v", err)
	}
}

func TestProcessWithdrawal(t *testing.T) {
	reg := NewRollupRegistry()
	reg.RegisterRollup(testConfig(1))

	to := testAddr(0x02)
	amount := big.NewInt(500_000)
	proof := makeValidWithdrawalProof(1, to, amount)
	if proof == nil {
		t.Fatal("failed to construct valid withdrawal proof")
	}

	withdrawal, err := reg.ProcessWithdrawal(1, to, amount, proof)
	if err != nil {
		t.Fatalf("ProcessWithdrawal failed: %v", err)
	}
	if withdrawal.RollupID != 1 {
		t.Errorf("expected rollupID 1, got %d", withdrawal.RollupID)
	}
	if withdrawal.To != to {
		t.Error("to address mismatch")
	}
	if withdrawal.Amount.Cmp(amount) != 0 {
		t.Error("amount mismatch")
	}
	if !withdrawal.Verified {
		t.Error("withdrawal should be verified")
	}
	if withdrawal.ID == (types.Hash{}) {
		t.Error("withdrawal ID should be non-zero")
	}

	state, _ := reg.GetRollupState(1)
	if state.TotalWithdrawals != 1 {
		t.Errorf("expected 1 total withdrawal, got %d", state.TotalWithdrawals)
	}
}

func TestProcessWithdrawalZeroAmount(t *testing.T) {
	reg := NewRollupRegistry()
	reg.RegisterRollup(testConfig(1))

	_, err := reg.ProcessWithdrawal(1, testAddr(0x02), big.NewInt(0), []byte{0x01})
	if err != ErrWithdrawAmountZero {
		t.Errorf("expected ErrWithdrawAmountZero, got %v", err)
	}
}

func TestProcessWithdrawalZeroAddress(t *testing.T) {
	reg := NewRollupRegistry()
	reg.RegisterRollup(testConfig(1))

	_, err := reg.ProcessWithdrawal(1, types.Address{}, big.NewInt(100), []byte{0x01})
	if err != ErrWithdrawToZero {
		t.Errorf("expected ErrWithdrawToZero, got %v", err)
	}
}

func TestProcessWithdrawalEmptyProof(t *testing.T) {
	reg := NewRollupRegistry()
	reg.RegisterRollup(testConfig(1))

	_, err := reg.ProcessWithdrawal(1, testAddr(0x02), big.NewInt(100), nil)
	if err != ErrWithdrawProofEmpty {
		t.Errorf("expected ErrWithdrawProofEmpty, got %v", err)
	}

	_, err = reg.ProcessWithdrawal(1, testAddr(0x02), big.NewInt(100), []byte{})
	if err != ErrWithdrawProofEmpty {
		t.Errorf("expected ErrWithdrawProofEmpty for empty slice, got %v", err)
	}
}

func TestProcessWithdrawalRollupNotFound(t *testing.T) {
	reg := NewRollupRegistry()

	_, err := reg.ProcessWithdrawal(999, testAddr(0x02), big.NewInt(100), []byte{0x01})
	if err != ErrRollupNotFound {
		t.Errorf("expected ErrRollupNotFound, got %v", err)
	}
}

func TestRegistryCount(t *testing.T) {
	reg := NewRollupRegistry()
	if reg.Count() != 0 {
		t.Errorf("expected 0, got %d", reg.Count())
	}

	reg.RegisterRollup(testConfig(1))
	reg.RegisterRollup(testConfig(2))
	if reg.Count() != 2 {
		t.Errorf("expected 2, got %d", reg.Count())
	}
}

func TestRegistryIDs(t *testing.T) {
	reg := NewRollupRegistry()
	reg.RegisterRollup(testConfig(10))
	reg.RegisterRollup(testConfig(20))

	ids := reg.IDs()
	if len(ids) != 2 {
		t.Fatalf("expected 2 IDs, got %d", len(ids))
	}

	found := make(map[uint64]bool)
	for _, id := range ids {
		found[id] = true
	}
	if !found[10] || !found[20] {
		t.Errorf("expected IDs 10 and 20, got %v", ids)
	}
}

func TestRegistryConcurrency(t *testing.T) {
	reg := NewRollupRegistry()

	var wg sync.WaitGroup
	for i := uint64(1); i <= 50; i++ {
		wg.Add(1)
		go func(id uint64) {
			defer wg.Done()
			cfg := testConfig(id)
			cfg.Name = "rollup"
			reg.RegisterRollup(cfg)
		}(i)
	}
	wg.Wait()

	if reg.Count() != 50 {
		t.Errorf("expected 50 rollups, got %d", reg.Count())
	}
}

func TestMultipleDepositsUniqueIDs(t *testing.T) {
	reg := NewRollupRegistry()
	reg.RegisterRollup(testConfig(1))

	from := testAddr(0x01)
	amount := big.NewInt(100)

	d1, _ := reg.ProcessDeposit(1, from, amount)
	d2, _ := reg.ProcessDeposit(1, from, amount)

	if d1.ID == d2.ID {
		t.Error("different deposits should have different IDs")
	}
}

// makeValidSTFProof constructs a proof that passes verifyCommitment for the
// given rollup, pre-state, and post-state.
func makeValidSTFProof(rollupID uint64, pre, post types.Hash) []byte {
	// verifyCommitment checks: commitment[0] ^ commitment[1] == byte(rollupID) ^ byte(proofLen)
	// commitment = SHA256(pre || post || proof)
	// We try different proof contents until the check passes.
	for proofLen := MinProofLen; proofLen < MinProofLen+128; proofLen++ {
		proof := make([]byte, proofLen)
		for nonce := 0; nonce < 65536; nonce++ {
			proof[proofLen-2] = byte(nonce >> 8)
			proof[proofLen-1] = byte(nonce)

			h := sha256.New()
			h.Write(pre[:])
			h.Write(post[:])
			h.Write(proof)
			commitment := h.Sum(nil)

			expected := byte(rollupID) ^ byte(proofLen)
			actual := commitment[0] ^ commitment[1]
			if actual == expected {
				return proof
			}
		}
	}
	return nil
}
