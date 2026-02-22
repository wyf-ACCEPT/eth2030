package consensus

import (
	"sync"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

// testPubkey returns a 48-byte pubkey with the given seed byte.
func testPubkey(seed byte) []byte {
	pk := make([]byte, BLSPubkeyLength)
	pk[0] = seed
	return pk
}

// testSignature returns a 96-byte signature with the given seed byte.
func testSignature(seed byte) []byte {
	sig := make([]byte, BLSSignatureLength)
	sig[0] = seed
	return sig
}

func TestNewDepositProcessor(t *testing.T) {
	cfg := DefaultDepositConfig()
	dp := NewDepositProcessor(cfg)
	if dp == nil {
		t.Fatal("NewDepositProcessor returned nil")
	}
	if dp.config.MinDepositAmount != MinDepositAmountGwei {
		t.Errorf("expected MinDepositAmount=%d, got %d", MinDepositAmountGwei, dp.config.MinDepositAmount)
	}
	if dp.config.MaxDepositsPerBlock != DefaultMaxDepositsPerBlock {
		t.Errorf("expected MaxDepositsPerBlock=%d, got %d", DefaultMaxDepositsPerBlock, dp.config.MaxDepositsPerBlock)
	}
	if dp.config.ActivationDelay != DefaultActivationDelay {
		t.Errorf("expected ActivationDelay=%d, got %d", DefaultActivationDelay, dp.config.ActivationDelay)
	}
}

func TestDefaultDepositConfig(t *testing.T) {
	cfg := DefaultDepositConfig()
	if cfg.MinDepositAmount != 32_000_000_000 {
		t.Errorf("expected 32 ETH in Gwei, got %d", cfg.MinDepositAmount)
	}
}

func TestDepositProcessor_ValidateDeposit_Valid(t *testing.T) {
	dp := NewDepositProcessor(DefaultDepositConfig())

	d := &ValidatorDeposit{
		Pubkey:                testPubkey(1),
		WithdrawalCredentials: types.Hash{0x01},
		Amount:                MinDepositAmountGwei,
		Signature:             testSignature(1),
		Index:                 0,
	}
	if err := dp.ValidateDeposit(d); err != nil {
		t.Errorf("valid deposit should not error: %v", err)
	}
}

func TestDepositProcessor_ValidateDeposit_EmptyPubkey(t *testing.T) {
	dp := NewDepositProcessor(DefaultDepositConfig())

	d := &ValidatorDeposit{
		Pubkey: nil,
		Amount: MinDepositAmountGwei,
	}
	if err := dp.ValidateDeposit(d); err != ErrEmptyPubkey {
		t.Errorf("expected ErrEmptyPubkey, got %v", err)
	}
}

func TestDepositProcessor_ValidateDeposit_InvalidPubkeyLength(t *testing.T) {
	dp := NewDepositProcessor(DefaultDepositConfig())

	d := &ValidatorDeposit{
		Pubkey: []byte{1, 2, 3}, // too short
		Amount: MinDepositAmountGwei,
	}
	if err := dp.ValidateDeposit(d); err != ErrInvalidPubkeyLength {
		t.Errorf("expected ErrInvalidPubkeyLength, got %v", err)
	}
}

func TestDepositProcessor_ValidateDeposit_InvalidSigLength(t *testing.T) {
	dp := NewDepositProcessor(DefaultDepositConfig())

	d := &ValidatorDeposit{
		Pubkey:    testPubkey(1),
		Amount:    MinDepositAmountGwei,
		Signature: []byte{1, 2, 3}, // invalid length (not 0 and not 96)
	}
	if err := dp.ValidateDeposit(d); err != ErrInvalidSigLength {
		t.Errorf("expected ErrInvalidSigLength, got %v", err)
	}
}

func TestDepositProcessor_ValidateDeposit_EmptySignature(t *testing.T) {
	dp := NewDepositProcessor(DefaultDepositConfig())

	// Empty signature is allowed (not all deposits have signatures in EIP-6110).
	d := &ValidatorDeposit{
		Pubkey:    testPubkey(1),
		Amount:    MinDepositAmountGwei,
		Signature: nil,
	}
	if err := dp.ValidateDeposit(d); err != nil {
		t.Errorf("empty signature should be valid: %v", err)
	}
}

func TestDepositProcessor_ValidateDeposit_ZeroAmount(t *testing.T) {
	dp := NewDepositProcessor(DefaultDepositConfig())

	d := &ValidatorDeposit{
		Pubkey: testPubkey(1),
		Amount: 0,
	}
	if err := dp.ValidateDeposit(d); err != ErrDepositZeroAmount {
		t.Errorf("expected ErrDepositZeroAmount, got %v", err)
	}
}

func TestDepositProcessor_ProcessDeposit(t *testing.T) {
	dp := NewDepositProcessor(DefaultDepositConfig())

	d := &ValidatorDeposit{
		Pubkey:                testPubkey(1),
		WithdrawalCredentials: types.Hash{0x01},
		Amount:                MinDepositAmountGwei,
		Signature:             testSignature(1),
		Index:                 0,
	}

	if err := dp.ProcessDeposit(d); err != nil {
		t.Fatalf("ProcessDeposit failed: %v", err)
	}

	if dp.GetDepositCount() != 1 {
		t.Errorf("expected deposit count=1, got %d", dp.GetDepositCount())
	}
}

func TestDepositProcessor_ProcessDeposit_DuplicateIndex(t *testing.T) {
	dp := NewDepositProcessor(DefaultDepositConfig())

	d1 := &ValidatorDeposit{
		Pubkey: testPubkey(1),
		Amount: MinDepositAmountGwei,
		Index:  0,
	}
	d2 := &ValidatorDeposit{
		Pubkey: testPubkey(2),
		Amount: MinDepositAmountGwei,
		Index:  0, // same index
	}

	dp.ProcessDeposit(d1)
	if err := dp.ProcessDeposit(d2); err != ErrDepositAlreadyExists {
		t.Errorf("expected ErrDepositAlreadyExists, got %v", err)
	}
}

func TestDepositProcessor_ProcessDeposit_InvalidDeposit(t *testing.T) {
	dp := NewDepositProcessor(DefaultDepositConfig())

	d := &ValidatorDeposit{
		Pubkey: nil,
		Amount: MinDepositAmountGwei,
		Index:  0,
	}
	if err := dp.ProcessDeposit(d); err != ErrEmptyPubkey {
		t.Errorf("expected ErrEmptyPubkey, got %v", err)
	}
	if dp.GetDepositCount() != 0 {
		t.Error("invalid deposit should not increment count")
	}
}

func TestDepositProcessor_GetPendingDeposits(t *testing.T) {
	dp := NewDepositProcessor(DefaultDepositConfig())

	for i := uint64(0); i < 3; i++ {
		dp.ProcessDeposit(&ValidatorDeposit{
			Pubkey: testPubkey(byte(i + 1)),
			Amount: MinDepositAmountGwei,
			Index:  i,
		})
	}

	pending := dp.GetPendingDeposits()
	if len(pending) != 3 {
		t.Errorf("expected 3 pending deposits, got %d", len(pending))
	}
}

func TestDepositProcessor_GetPendingDeposits_IsCopy(t *testing.T) {
	dp := NewDepositProcessor(DefaultDepositConfig())

	dp.ProcessDeposit(&ValidatorDeposit{
		Pubkey: testPubkey(1),
		Amount: MinDepositAmountGwei,
		Index:  0,
	})

	pending := dp.GetPendingDeposits()
	pending[0] = nil // mutate the copy

	original := dp.GetPendingDeposits()
	if original[0] == nil {
		t.Error("GetPendingDeposits should return a copy")
	}
}

func TestDepositProcessor_GetDepositCount(t *testing.T) {
	dp := NewDepositProcessor(DefaultDepositConfig())

	if dp.GetDepositCount() != 0 {
		t.Error("initial count should be 0")
	}

	for i := uint64(0); i < 5; i++ {
		dp.ProcessDeposit(&ValidatorDeposit{
			Pubkey: testPubkey(byte(i + 1)),
			Amount: MinDepositAmountGwei,
			Index:  i,
		})
	}

	if dp.GetDepositCount() != 5 {
		t.Errorf("expected count=5, got %d", dp.GetDepositCount())
	}
}

func TestDepositProcessor_GetDepositRoot_Empty(t *testing.T) {
	dp := NewDepositProcessor(DefaultDepositConfig())
	root := dp.GetDepositRoot()
	if root != (types.Hash{}) {
		t.Error("empty deposit list should have zero root")
	}
}

func TestDepositProcessor_GetDepositRoot_SingleDeposit(t *testing.T) {
	dp := NewDepositProcessor(DefaultDepositConfig())

	dp.ProcessDeposit(&ValidatorDeposit{
		Pubkey:                testPubkey(1),
		WithdrawalCredentials: types.Hash{0x01},
		Amount:                MinDepositAmountGwei,
		Index:                 0,
	})

	root := dp.GetDepositRoot()
	if root == (types.Hash{}) {
		t.Error("single deposit should have non-zero root")
	}
}

func TestDepositProcessor_GetDepositRoot_Deterministic(t *testing.T) {
	makeDP := func() *DepositProcessor {
		dp := NewDepositProcessor(DefaultDepositConfig())
		for i := uint64(0); i < 4; i++ {
			dp.ProcessDeposit(&ValidatorDeposit{
				Pubkey:                testPubkey(byte(i + 1)),
				WithdrawalCredentials: types.Hash{byte(i)},
				Amount:                MinDepositAmountGwei,
				Index:                 i,
			})
		}
		return dp
	}

	root1 := makeDP().GetDepositRoot()
	root2 := makeDP().GetDepositRoot()

	if root1 != root2 {
		t.Errorf("deposit root should be deterministic: %v != %v", root1, root2)
	}
}

func TestDepositProcessor_GetDepositRoot_DifferentDepositsGiveDifferentRoots(t *testing.T) {
	dp1 := NewDepositProcessor(DefaultDepositConfig())
	dp1.ProcessDeposit(&ValidatorDeposit{
		Pubkey: testPubkey(1),
		Amount: MinDepositAmountGwei,
		Index:  0,
	})

	dp2 := NewDepositProcessor(DefaultDepositConfig())
	dp2.ProcessDeposit(&ValidatorDeposit{
		Pubkey: testPubkey(2),
		Amount: MinDepositAmountGwei,
		Index:  0,
	})

	if dp1.GetDepositRoot() == dp2.GetDepositRoot() {
		t.Error("different deposits should produce different roots")
	}
}

func TestDepositProcessor_GetDepositRoot_OddNumberOfDeposits(t *testing.T) {
	dp := NewDepositProcessor(DefaultDepositConfig())
	for i := uint64(0); i < 3; i++ {
		dp.ProcessDeposit(&ValidatorDeposit{
			Pubkey: testPubkey(byte(i + 1)),
			Amount: MinDepositAmountGwei,
			Index:  i,
		})
	}

	root := dp.GetDepositRoot()
	if root == (types.Hash{}) {
		t.Error("3 deposits should have non-zero root")
	}
}

func TestDepositProcessor_ActivateValidators_Basic(t *testing.T) {
	dp := NewDepositProcessor(DefaultDepositConfig())

	dp.ProcessDeposit(&ValidatorDeposit{
		Pubkey: testPubkey(1),
		Amount: MinDepositAmountGwei,
		Index:  0,
	})
	dp.ProcessDeposit(&ValidatorDeposit{
		Pubkey: testPubkey(2),
		Amount: MinDepositAmountGwei,
		Index:  1,
	})

	activated := dp.ActivateValidators(10)
	if len(activated) != 2 {
		t.Fatalf("expected 2 activated validators, got %d", len(activated))
	}

	for _, a := range activated {
		if a.ActivationEpoch != 10+DefaultActivationDelay {
			t.Errorf("expected activation epoch %d, got %d", 10+DefaultActivationDelay, a.ActivationEpoch)
		}
		if a.EffectiveBalance != MinDepositAmountGwei {
			t.Errorf("expected effective balance %d, got %d", MinDepositAmountGwei, a.EffectiveBalance)
		}
	}
}

func TestDepositProcessor_ActivateValidators_InsufficientDeposit(t *testing.T) {
	cfg := DefaultDepositConfig()
	cfg.MinDepositAmount = 100 // lower for testing
	dp := NewDepositProcessor(cfg)

	dp.ProcessDeposit(&ValidatorDeposit{
		Pubkey: testPubkey(1),
		Amount: 50, // below threshold
		Index:  0,
	})

	activated := dp.ActivateValidators(1)
	if len(activated) != 0 {
		t.Error("validator with insufficient deposit should not be activated")
	}

	// Deposit more to reach threshold.
	dp.ProcessDeposit(&ValidatorDeposit{
		Pubkey: testPubkey(1),
		Amount: 50, // now total = 100
		Index:  1,
	})

	activated = dp.ActivateValidators(2)
	if len(activated) != 1 {
		t.Fatalf("expected 1 activated validator, got %d", len(activated))
	}
	if activated[0].EffectiveBalance != 100 {
		t.Errorf("expected effective balance 100, got %d", activated[0].EffectiveBalance)
	}
}

func TestDepositProcessor_ActivateValidators_AlreadyActivated(t *testing.T) {
	dp := NewDepositProcessor(DefaultDepositConfig())

	dp.ProcessDeposit(&ValidatorDeposit{
		Pubkey: testPubkey(1),
		Amount: MinDepositAmountGwei,
		Index:  0,
	})

	activated1 := dp.ActivateValidators(5)
	if len(activated1) != 1 {
		t.Fatal("first activation should succeed")
	}

	// Second activation should return nothing.
	activated2 := dp.ActivateValidators(10)
	if len(activated2) != 0 {
		t.Error("already activated validator should not be activated again")
	}
}

func TestDepositProcessor_ActivateValidators_ClearsPending(t *testing.T) {
	dp := NewDepositProcessor(DefaultDepositConfig())

	dp.ProcessDeposit(&ValidatorDeposit{
		Pubkey: testPubkey(1),
		Amount: MinDepositAmountGwei,
		Index:  0,
	})

	if len(dp.GetPendingDeposits()) != 1 {
		t.Error("should have 1 pending deposit")
	}

	dp.ActivateValidators(1)

	if len(dp.GetPendingDeposits()) != 0 {
		t.Error("pending should be empty after activation")
	}
}

func TestDepositProcessor_ActivateValidators_PartialActivation(t *testing.T) {
	cfg := DefaultDepositConfig()
	cfg.MinDepositAmount = 100
	dp := NewDepositProcessor(cfg)

	// Validator A has enough.
	dp.ProcessDeposit(&ValidatorDeposit{
		Pubkey: testPubkey(1),
		Amount: 100,
		Index:  0,
	})
	// Validator B does not have enough.
	dp.ProcessDeposit(&ValidatorDeposit{
		Pubkey: testPubkey(2),
		Amount: 50,
		Index:  1,
	})

	activated := dp.ActivateValidators(1)
	if len(activated) != 1 {
		t.Errorf("expected 1 activated, got %d", len(activated))
	}
	if !pubkeysEqual(activated[0].Pubkey, testPubkey(1)) {
		t.Error("validator A should be the one activated")
	}

	// Validator B should still be pending.
	pending := dp.GetPendingDeposits()
	if len(pending) != 1 {
		t.Errorf("expected 1 pending, got %d", len(pending))
	}
}

func TestDepositProcessor_GetValidatorByPubkey_Found(t *testing.T) {
	dp := NewDepositProcessor(DefaultDepositConfig())

	pk := testPubkey(42)
	dp.ProcessDeposit(&ValidatorDeposit{
		Pubkey:                pk,
		WithdrawalCredentials: types.Hash{0xab},
		Amount:                MinDepositAmountGwei,
		Index:                 0,
	})

	found, ok := dp.GetValidatorByPubkey(pk)
	if !ok {
		t.Fatal("expected to find validator")
	}
	if found.Index != 0 {
		t.Errorf("expected index=0, got %d", found.Index)
	}
	if found.WithdrawalCredentials != (types.Hash{0xab}) {
		t.Error("withdrawal credentials mismatch")
	}
}

func TestDepositProcessor_GetValidatorByPubkey_NotFound(t *testing.T) {
	dp := NewDepositProcessor(DefaultDepositConfig())

	_, ok := dp.GetValidatorByPubkey(testPubkey(99))
	if ok {
		t.Error("should not find non-existent validator")
	}
}

func TestDepositProcessor_GetValidatorByPubkey_MultipleDeposits(t *testing.T) {
	cfg := DefaultDepositConfig()
	cfg.MinDepositAmount = 50
	dp := NewDepositProcessor(cfg)

	pk := testPubkey(1)

	dp.ProcessDeposit(&ValidatorDeposit{
		Pubkey: pk,
		Amount: 50,
		Index:  0,
	})
	dp.ProcessDeposit(&ValidatorDeposit{
		Pubkey: pk,
		Amount: 50,
		Index:  1,
	})

	found, ok := dp.GetValidatorByPubkey(pk)
	if !ok {
		t.Fatal("expected to find validator")
	}
	// Should return the latest deposit.
	if found.Index != 1 {
		t.Errorf("expected latest deposit index=1, got %d", found.Index)
	}
}

func TestDepositProcessor_GetValidatorBalance(t *testing.T) {
	cfg := DefaultDepositConfig()
	cfg.MinDepositAmount = 50
	dp := NewDepositProcessor(cfg)

	pk := testPubkey(1)

	dp.ProcessDeposit(&ValidatorDeposit{Pubkey: pk, Amount: 60, Index: 0})
	dp.ProcessDeposit(&ValidatorDeposit{Pubkey: pk, Amount: 40, Index: 1})

	bal, ok := dp.GetValidatorBalance(pk)
	if !ok {
		t.Fatal("expected to find balance")
	}
	if bal != 100 {
		t.Errorf("expected balance=100, got %d", bal)
	}
}

func TestDepositProcessor_GetValidatorBalance_NotFound(t *testing.T) {
	dp := NewDepositProcessor(DefaultDepositConfig())
	_, ok := dp.GetValidatorBalance(testPubkey(99))
	if ok {
		t.Error("should not find non-existent validator balance")
	}
}

func TestDepositProcessor_IsActivated(t *testing.T) {
	dp := NewDepositProcessor(DefaultDepositConfig())

	pk := testPubkey(1)
	dp.ProcessDeposit(&ValidatorDeposit{Pubkey: pk, Amount: MinDepositAmountGwei, Index: 0})

	if dp.IsActivated(pk) {
		t.Error("should not be activated before ActivateValidators")
	}

	dp.ActivateValidators(1)

	if !dp.IsActivated(pk) {
		t.Error("should be activated after ActivateValidators")
	}
}

func TestDepositProcessor_IsActivated_NotFound(t *testing.T) {
	dp := NewDepositProcessor(DefaultDepositConfig())
	if dp.IsActivated(testPubkey(99)) {
		t.Error("unknown pubkey should not be activated")
	}
}

func TestDepositProcessor_ThreadSafety(t *testing.T) {
	cfg := DefaultDepositConfig()
	cfg.MinDepositAmount = 10
	dp := NewDepositProcessor(cfg)

	var wg sync.WaitGroup

	// Concurrent deposits.
	for i := uint64(0); i < 50; i++ {
		wg.Add(1)
		go func(idx uint64) {
			defer wg.Done()
			dp.ProcessDeposit(&ValidatorDeposit{
				Pubkey: testPubkey(byte(idx + 1)),
				Amount: 10,
				Index:  idx,
			})
		}(i)
	}
	wg.Wait()

	if dp.GetDepositCount() != 50 {
		t.Errorf("expected 50 deposits, got %d", dp.GetDepositCount())
	}

	// Concurrent reads.
	for i := 0; i < 20; i++ {
		wg.Add(4)
		go func() {
			defer wg.Done()
			dp.GetPendingDeposits()
		}()
		go func() {
			defer wg.Done()
			dp.GetDepositCount()
		}()
		go func() {
			defer wg.Done()
			dp.GetDepositRoot()
		}()
		go func() {
			defer wg.Done()
			dp.GetValidatorByPubkey(testPubkey(1))
		}()
	}
	wg.Wait()
}

func TestDepositProcessor_ThreadSafety_ActivateWhileDepositing(t *testing.T) {
	cfg := DefaultDepositConfig()
	cfg.MinDepositAmount = 10
	dp := NewDepositProcessor(cfg)

	var wg sync.WaitGroup

	// Deposit concurrently.
	for i := uint64(0); i < 30; i++ {
		wg.Add(1)
		go func(idx uint64) {
			defer wg.Done()
			dp.ProcessDeposit(&ValidatorDeposit{
				Pubkey: testPubkey(byte(idx + 1)),
				Amount: 10,
				Index:  idx,
			})
		}(i)
	}

	// Activate concurrently.
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(epoch uint64) {
			defer wg.Done()
			dp.ActivateValidators(epoch)
		}(uint64(i))
	}

	wg.Wait()
}

func TestDepositProcessor_MultipleDepositsAccumulate(t *testing.T) {
	cfg := DefaultDepositConfig()
	cfg.MinDepositAmount = 100
	dp := NewDepositProcessor(cfg)

	pk := testPubkey(1)

	// Three deposits of 40 each = 120 total.
	dp.ProcessDeposit(&ValidatorDeposit{Pubkey: pk, Amount: 40, Index: 0})
	dp.ProcessDeposit(&ValidatorDeposit{Pubkey: pk, Amount: 40, Index: 1})
	dp.ProcessDeposit(&ValidatorDeposit{Pubkey: pk, Amount: 40, Index: 2})

	activated := dp.ActivateValidators(1)
	if len(activated) != 1 {
		t.Fatalf("expected 1 activated, got %d", len(activated))
	}
	if activated[0].EffectiveBalance != 120 {
		t.Errorf("expected effective balance 120, got %d", activated[0].EffectiveBalance)
	}
}

func TestDepositProcessor_ActivationDelay(t *testing.T) {
	cfg := DefaultDepositConfig()
	cfg.ActivationDelay = 10
	dp := NewDepositProcessor(cfg)

	dp.ProcessDeposit(&ValidatorDeposit{
		Pubkey: testPubkey(1),
		Amount: MinDepositAmountGwei,
		Index:  0,
	})

	activated := dp.ActivateValidators(5)
	if len(activated) != 1 {
		t.Fatal("expected 1 activated")
	}
	if activated[0].ActivationEpoch != 15 {
		t.Errorf("expected activation epoch 15 (5+10), got %d", activated[0].ActivationEpoch)
	}
}

func TestComputeMerkleRoot_PowerOfTwo(t *testing.T) {
	leaves := []types.Hash{
		{0x01},
		{0x02},
		{0x03},
		{0x04},
	}
	root := computeMerkleRoot(leaves)
	if root == (types.Hash{}) {
		t.Error("root should not be zero")
	}
}

func TestComputeMerkleRoot_Empty(t *testing.T) {
	root := computeMerkleRoot(nil)
	if root != (types.Hash{}) {
		t.Error("empty leaves should return zero root")
	}
}

func TestComputeMerkleRoot_Single(t *testing.T) {
	leaf := types.Hash{0xab}
	root := computeMerkleRoot([]types.Hash{leaf})
	if root != leaf {
		t.Error("single leaf should be its own root")
	}
}

func TestComputeMerkleRoot_DifferentOrderDifferentRoot(t *testing.T) {
	a := types.Hash{0x01}
	b := types.Hash{0x02}

	root1 := computeMerkleRoot([]types.Hash{a, b})
	root2 := computeMerkleRoot([]types.Hash{b, a})

	if root1 == root2 {
		t.Error("different leaf order should produce different roots")
	}
}
