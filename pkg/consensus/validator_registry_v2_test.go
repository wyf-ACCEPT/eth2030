package consensus

import (
	"testing"
)

func vrTestPubkey(id byte) [48]byte {
	var pk [48]byte
	pk[0] = id
	return pk
}

func vrTestCreds(prefix byte) [32]byte {
	var c [32]byte
	c[0] = prefix
	return c
}

func makeRegistryV2(n int) *ValidatorRegistryV2 {
	cfg := DefaultValidatorRegistryV2Config()
	r := NewValidatorRegistryV2(cfg)
	for i := 0; i < n; i++ {
		pk := vrTestPubkey(byte(i + 1))
		creds := vrTestCreds(VRWithdrawalCredentialETH1)
		r.RegisterValidator(pk, creds, 32*GweiPerETH, 32*GweiPerETH)
		// Manually activate.
		r.validators[i].ActivationEligibility = 0
		r.validators[i].ActivationEpoch = 0
	}
	return r
}

func TestValidatorRegistryV2Register(t *testing.T) {
	r := NewValidatorRegistryV2(DefaultValidatorRegistryV2Config())

	pk := vrTestPubkey(1)
	creds := vrTestCreds(VRWithdrawalCredentialETH1)
	idx, err := r.RegisterValidator(pk, creds, 32*GweiPerETH, 32*GweiPerETH)
	if err != nil {
		t.Fatalf("RegisterValidator: %v", err)
	}
	if idx != 0 {
		t.Errorf("idx=%d, want 0", idx)
	}
	if r.Size() != 1 {
		t.Errorf("Size()=%d, want 1", r.Size())
	}

	// Duplicate pubkey should fail.
	_, err = r.RegisterValidator(pk, creds, 32*GweiPerETH, 32*GweiPerETH)
	if err != ErrVRDuplicatePubkey {
		t.Fatalf("expected ErrVRDuplicatePubkey, got %v", err)
	}
}

func TestValidatorRegistryV2GetValidator(t *testing.T) {
	r := makeRegistryV2(4)
	v, err := r.GetValidator(2)
	if err != nil {
		t.Fatalf("GetValidator: %v", err)
	}
	if v.Index != 2 {
		t.Errorf("Index=%d, want 2", v.Index)
	}

	_, err = r.GetValidator(100)
	if err != ErrVRIndexOutOfRange {
		t.Fatalf("expected ErrVRIndexOutOfRange, got %v", err)
	}
}

func TestValidatorRegistryV2GetByPubkey(t *testing.T) {
	r := makeRegistryV2(4)
	pk := vrTestPubkey(3)
	v, err := r.GetByPubkey(pk)
	if err != nil {
		t.Fatalf("GetByPubkey: %v", err)
	}
	if v.Index != 2 { // pk(3) is the third one added (index 2)
		t.Errorf("Index=%d, want 2", v.Index)
	}

	_, err = r.GetByPubkey(vrTestPubkey(99))
	if err != ErrVRNotFound {
		t.Fatalf("expected ErrVRNotFound, got %v", err)
	}
}

func TestValidatorRegistryV2ActiveIndices(t *testing.T) {
	r := makeRegistryV2(4)
	indices := r.ActiveIndices(5)
	if len(indices) != 4 {
		t.Fatalf("ActiveIndices len=%d, want 4", len(indices))
	}

	// Verify sorted order.
	for i := 1; i < len(indices); i++ {
		if indices[i] <= indices[i-1] {
			t.Fatal("ActiveIndices not sorted")
		}
	}
}

func TestValidatorRegistryV2TotalEffectiveBalance(t *testing.T) {
	r := makeRegistryV2(4)
	tab := r.TotalEffectiveBalance(5)
	expected := uint64(4 * 32 * GweiPerETH)
	if tab != expected {
		t.Errorf("TotalEffectiveBalance=%d, want %d", tab, expected)
	}
}

func TestValidatorRegistryV2ComputeChurn(t *testing.T) {
	r := makeRegistryV2(4)
	churn := r.ComputeChurn(5)
	// 4 validators / 65536 = 0, so min = 4.
	if churn != MinPerEpochChurnLimit {
		t.Errorf("ComputeChurn=%d, want %d", churn, MinPerEpochChurnLimit)
	}
}

func TestValidatorRegistryV2ActivationQueue(t *testing.T) {
	cfg := DefaultValidatorRegistryV2Config()
	r := NewValidatorRegistryV2(cfg)

	// Create 4 active validators.
	for i := 0; i < 4; i++ {
		pk := vrTestPubkey(byte(i + 1))
		r.RegisterValidator(pk, vrTestCreds(0x01), 32*GweiPerETH, 32*GweiPerETH)
		r.validators[i].ActivationEligibility = 0
		r.validators[i].ActivationEpoch = 0
	}

	// Add 2 pending validators that are eligible for activation.
	for i := 4; i < 6; i++ {
		pk := vrTestPubkey(byte(i + 1))
		r.RegisterValidator(pk, vrTestCreds(0x01), 32*GweiPerETH, 32*GweiPerETH)
		r.validators[i].ActivationEligibility = 1 // eligible at epoch 1
	}

	activated := r.ProcessActivationQueueV2(5, 3) // current=5, finalized=3
	if len(activated) != 2 {
		t.Fatalf("activated %d validators, want 2", len(activated))
	}

	// Verify activation epoch was set.
	for _, idx := range activated {
		v := r.validators[idx]
		if v.ActivationEpoch == FarFutureEpoch {
			t.Errorf("validator %d still has FarFutureEpoch", idx)
		}
	}
}

func TestValidatorRegistryV2VoluntaryExit(t *testing.T) {
	r := makeRegistryV2(4)

	// Try to exit validator 2 at epoch 300 (past shard committee period).
	err := r.InitiateVoluntaryExit(2, 300)
	if err != nil {
		t.Fatalf("InitiateVoluntaryExit: %v", err)
	}

	v := r.validators[2]
	if v.ExitEpoch == FarFutureEpoch {
		t.Fatal("exit epoch should be set")
	}
	if v.WithdrawableEpoch == FarFutureEpoch {
		t.Fatal("withdrawable epoch should be set")
	}
}

func TestValidatorRegistryV2VoluntaryExitTooEarly(t *testing.T) {
	r := makeRegistryV2(4)

	// Exit at epoch 100 < activation_epoch(0) + shard_committee_period(256).
	err := r.InitiateVoluntaryExit(2, 100)
	if err != ErrVRTooEarlyExit {
		t.Fatalf("expected ErrVRTooEarlyExit, got %v", err)
	}
}

func TestValidatorRegistryV2VoluntaryExitAlreadyExiting(t *testing.T) {
	r := makeRegistryV2(4)
	r.validators[1].ExitEpoch = 500 // In future but != FarFutureEpoch.

	err := r.InitiateVoluntaryExit(1, 300)
	if err != ErrVRAlreadyExiting {
		t.Fatalf("expected ErrVRAlreadyExiting, got %v", err)
	}
}

func TestValidatorRegistryV2VoluntaryExitNotActive(t *testing.T) {
	r := makeRegistryV2(4)
	r.validators[1].ActivationEpoch = 500

	err := r.InitiateVoluntaryExit(1, 300)
	if err != ErrVRNotActive {
		t.Fatalf("expected ErrVRNotActive, got %v", err)
	}
}

func TestValidatorRegistryV2Slashing(t *testing.T) {
	r := makeRegistryV2(4)
	penalty, err := r.ProcessSlashingV2(1, 5)
	if err != nil {
		t.Fatalf("ProcessSlashingV2: %v", err)
	}

	if penalty == 0 {
		t.Fatal("expected non-zero penalty")
	}

	v := r.validators[1]
	if !v.Slashed {
		t.Fatal("validator should be slashed")
	}
	if v.ExitEpoch == FarFutureEpoch {
		t.Fatal("slashed validator should have exit epoch set")
	}

	// Double slashing should fail.
	_, err = r.ProcessSlashingV2(1, 5)
	if err != ErrVRAlreadySlashed {
		t.Fatalf("expected ErrVRAlreadySlashed, got %v", err)
	}
}

func TestValidatorRegistryV2EffectiveBalanceUpdate(t *testing.T) {
	r := makeRegistryV2(4)

	// Increase actual balance significantly.
	r.validators[0].Balance = 40 * GweiPerETH
	r.validators[0].EffectiveBalance = 32 * GweiPerETH

	r.UpdateEffectiveBalancesV2()

	if r.validators[0].EffectiveBalance <= 32*GweiPerETH {
		t.Errorf("effective balance not increased: %d", r.validators[0].EffectiveBalance)
	}
}

func TestValidatorRegistryV2UpdateBalance(t *testing.T) {
	r := makeRegistryV2(4)

	// Increase balance.
	newBal, err := r.UpdateBalance(0, int64(5*GweiPerETH))
	if err != nil {
		t.Fatalf("UpdateBalance: %v", err)
	}
	if newBal != 37*GweiPerETH {
		t.Errorf("balance=%d, want %d", newBal, 37*GweiPerETH)
	}

	// Decrease balance.
	newBal, err = r.UpdateBalance(0, -int64(2*GweiPerETH))
	if err != nil {
		t.Fatalf("UpdateBalance: %v", err)
	}
	if newBal != 35*GweiPerETH {
		t.Errorf("balance=%d, want %d", newBal, 35*GweiPerETH)
	}

	// Decrease below zero should floor at 0.
	newBal, err = r.UpdateBalance(0, -int64(100*GweiPerETH))
	if err != nil {
		t.Fatalf("UpdateBalance: %v", err)
	}
	if newBal != 0 {
		t.Errorf("balance=%d, want 0", newBal)
	}
}

func TestValidatorRegistryV2WithdrawalCredentials(t *testing.T) {
	r := makeRegistryV2(4)

	v, _ := r.GetValidator(0)
	if !v.HasETH1Credentials() {
		t.Fatal("expected ETH1 credentials")
	}

	// Update to compounding credentials.
	newCreds := vrTestCreds(VRWithdrawalCredentialCompounding)
	err := r.UpdateWithdrawalCredentials(0, newCreds)
	if err != nil {
		t.Fatalf("UpdateWithdrawalCredentials: %v", err)
	}

	v, _ = r.GetValidator(0)
	if !v.HasCompoundingCreds() {
		t.Fatal("expected compounding credentials")
	}
}

func TestValidatorRegistryV2Ejection(t *testing.T) {
	r := makeRegistryV2(4)

	// Set validator 3 to ejection balance.
	r.validators[3].EffectiveBalance = EjectionBalance

	ejected := r.ProcessEjections(5)
	if len(ejected) != 1 || ejected[0] != 3 {
		t.Fatalf("expected validator 3 ejected, got %v", ejected)
	}

	if r.validators[3].ExitEpoch == FarFutureEpoch {
		t.Fatal("ejected validator should have exit epoch set")
	}
}

func TestValidatorRegistryV2Stats(t *testing.T) {
	r := makeRegistryV2(4)

	// Set validator 3 as exited.
	r.validators[3].ExitEpoch = 3
	r.validators[3].WithdrawableEpoch = Epoch(3 + MinValidatorWithdrawDelay)

	stats := r.Stats(5)
	if stats.Total != 4 {
		t.Errorf("Total=%d, want 4", stats.Total)
	}
	if stats.Active != 3 {
		t.Errorf("Active=%d, want 3", stats.Active)
	}
	if stats.Exited != 1 {
		t.Errorf("Exited=%d, want 1", stats.Exited)
	}
}

func TestValidatorRecordV2LifecycleStates(t *testing.T) {
	v := &ValidatorRecordV2{
		ActivationEligibility: FarFutureEpoch,
		ActivationEpoch:       FarFutureEpoch,
		ExitEpoch:             FarFutureEpoch,
		WithdrawableEpoch:     FarFutureEpoch,
	}

	if !v.IsPending() {
		t.Fatal("should be pending")
	}

	v.ActivationEpoch = 5
	if v.IsPending() {
		t.Fatal("should not be pending after activation")
	}
	if !v.IsActive(10) {
		t.Fatal("should be active at epoch 10")
	}
	if !v.IsSlashable(10) {
		t.Fatal("should be slashable at epoch 10")
	}

	v.ExitEpoch = 20
	v.WithdrawableEpoch = 100
	if !v.IsActive(15) {
		t.Fatal("should be active before exit epoch")
	}
	if v.IsActive(20) {
		t.Fatal("should not be active at exit epoch")
	}
	if !v.IsExited(25) {
		t.Fatal("should be exited at epoch 25")
	}
	if v.IsWithdrawable(50) {
		t.Fatal("should not be withdrawable before withdrawable epoch")
	}
	if !v.IsWithdrawable(100) {
		t.Fatal("should be withdrawable at epoch 100")
	}
}

func TestValidatorRegistryV2CredentialTypes(t *testing.T) {
	v := &ValidatorRecordV2{}
	if v.CredentialType() != CredentialBLS {
		t.Error("zero credentials should be BLS type")
	}

	v.WithdrawalCredentials[0] = 0x01
	if v.CredentialType() != CredentialETH1 {
		t.Error("0x01 prefix should be ETH1")
	}
	if !v.HasETH1Credentials() {
		t.Error("should have ETH1 credentials")
	}

	v.WithdrawalCredentials[0] = 0x02
	if v.CredentialType() != CredentialCompounding {
		t.Error("0x02 prefix should be compounding")
	}
	if !v.HasCompoundingCreds() {
		t.Error("should have compounding credentials")
	}
}

func TestValidatorRegistryV2Full(t *testing.T) {
	cfg := DefaultValidatorRegistryV2Config()
	cfg.MaxValidators = 2
	r := NewValidatorRegistryV2(cfg)

	r.RegisterValidator(vrTestPubkey(1), vrTestCreds(0x01), 32*GweiPerETH, 32*GweiPerETH)
	r.RegisterValidator(vrTestPubkey(2), vrTestCreds(0x01), 32*GweiPerETH, 32*GweiPerETH)

	_, err := r.RegisterValidator(vrTestPubkey(3), vrTestCreds(0x01), 32*GweiPerETH, 32*GweiPerETH)
	if err != ErrVRRegistryFull {
		t.Fatalf("expected ErrVRRegistryFull, got %v", err)
	}
}

func TestValidatorRegistryV2MarkEligible(t *testing.T) {
	cfg := DefaultValidatorRegistryV2Config()
	r := NewValidatorRegistryV2(cfg)

	pk := vrTestPubkey(1)
	r.RegisterValidator(pk, vrTestCreds(0x01), 32*GweiPerETH, 32*GweiPerETH)

	err := r.MarkEligibleForActivation(0, 5)
	if err != nil {
		t.Fatalf("MarkEligibleForActivation: %v", err)
	}
	if r.validators[0].ActivationEligibility != 6 {
		t.Errorf("eligibility epoch=%d, want 6", r.validators[0].ActivationEligibility)
	}

	// Insufficient balance.
	r.RegisterValidator(vrTestPubkey(2), vrTestCreds(0x01), 1*GweiPerETH, 1*GweiPerETH)
	err = r.MarkEligibleForActivation(1, 5)
	if err != ErrVRInsufficientBal {
		t.Fatalf("expected ErrVRInsufficientBal, got %v", err)
	}
}
