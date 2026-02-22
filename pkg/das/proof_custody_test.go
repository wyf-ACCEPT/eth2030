package das

import (
	"sync"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

func TestNewProofCustodyScheme(t *testing.T) {
	pcs := NewProofCustodyScheme(ProofCustodyConfig{})
	if pcs.config.MinStake != 32_000_000_000 || pcs.config.BondDuration != 256 {
		t.Fatalf("bad defaults: stake=%d dur=%d", pcs.config.MinStake, pcs.config.BondDuration)
	}
	if pcs.config.ChallengeWindow != 64 || pcs.config.SlashingPenalty != 1_000_000_000 {
		t.Fatalf("bad defaults: window=%d penalty=%d", pcs.config.ChallengeWindow, pcs.config.SlashingPenalty)
	}
	// Custom config.
	pcs2 := NewProofCustodyScheme(ProofCustodyConfig{MinStake: 1_000, BondDuration: 100})
	if pcs2.config.MinStake != 1_000 || pcs2.config.BondDuration != 100 {
		t.Fatalf("custom: stake=%d dur=%d", pcs2.config.MinStake, pcs2.config.BondDuration)
	}
}

func TestDefaultProofCustodyConfig(t *testing.T) {
	cfg := DefaultProofCustodyConfig()
	if cfg.MinStake != 32_000_000_000 || cfg.BondDuration == 0 || cfg.ChallengeWindow == 0 || cfg.SlashingPenalty == 0 {
		t.Fatalf("bad defaults: %+v", cfg)
	}
}

func TestGenerateCustodyBond(t *testing.T) {
	pcs := NewProofCustodyScheme(ProofCustodyConfig{MinStake: 1_000, BondDuration: 100})
	nodeID := types.HexToHash("0xabcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890")

	bond, err := pcs.GenerateCustodyBond(nodeID, 42)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if bond.NodeID != nodeID {
		t.Fatal("nodeID mismatch")
	}
	if bond.Epoch != 42 || bond.ExpiresAt != 142 {
		t.Fatalf("epoch=%d expires=%d", bond.Epoch, bond.ExpiresAt)
	}
	if bond.Commitment.IsZero() || bond.Stake != 1_000 {
		t.Fatalf("commitment zero or wrong stake: %d", bond.Stake)
	}
}

func TestGenerateCustodyBond_Deterministic(t *testing.T) {
	pcs := NewProofCustodyScheme(ProofCustodyConfig{MinStake: 1_000, BondDuration: 100})
	nodeID := types.HexToHash("0x1111111111111111111111111111111111111111111111111111111111111111")

	b1, _ := pcs.GenerateCustodyBond(nodeID, 10)
	b2, _ := pcs.GenerateCustodyBond(nodeID, 10)
	if b1.Commitment != b2.Commitment {
		t.Fatal("same inputs should produce the same commitment")
	}
	b3, _ := pcs.GenerateCustodyBond(nodeID, 11)
	if b1.Commitment == b3.Commitment {
		t.Fatal("different epochs should produce different commitments")
	}
}

func TestRegisterBond(t *testing.T) {
	pcs := NewProofCustodyScheme(ProofCustodyConfig{MinStake: 1_000, BondDuration: 100})
	bond, _ := pcs.GenerateCustodyBond(types.HexToHash("0xaaaa"), 10)

	if err := pcs.RegisterBond(bond); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pcs.BondCount() != 1 {
		t.Fatalf("expected 1 bond, got %d", pcs.BondCount())
	}
	// Nil bond.
	if err := pcs.RegisterBond(nil); err != ErrNilBond {
		t.Fatalf("expected ErrNilBond, got %v", err)
	}
	// Duplicate.
	if err := pcs.RegisterBond(bond); err != ErrBondAlreadyRegistered {
		t.Fatalf("expected ErrBondAlreadyRegistered, got %v", err)
	}
	// Stake too low.
	pcs2 := NewProofCustodyScheme(ProofCustodyConfig{MinStake: 10_000, BondDuration: 100})
	lowBond := &CustodyBond{Commitment: types.HexToHash("0xdd"), Stake: 5_000, ExpiresAt: 101}
	if err := pcs2.RegisterBond(lowBond); err == nil {
		t.Fatal("expected error for low stake")
	}
}

func TestGetActiveBonds(t *testing.T) {
	pcs := NewProofCustodyScheme(ProofCustodyConfig{MinStake: 1_000, BondDuration: 100})

	for i := uint64(0); i < 5; i++ {
		bond, _ := pcs.GenerateCustodyBond(types.BytesToHash([]byte{byte(i)}), i*50)
		_ = pcs.RegisterBond(bond)
	}
	// Epoch 75: bonds from epoch 0 (expires 100) and 50 (expires 150) active.
	if n := len(pcs.GetActiveBonds(75)); n != 2 {
		t.Fatalf("expected 2 active at epoch 75, got %d", n)
	}
	// Epoch 200: bonds from epoch 150 (expires 250) and 200 (expires 300).
	if n := len(pcs.GetActiveBonds(200)); n != 2 {
		t.Fatalf("expected 2 active at epoch 200, got %d", n)
	}
	// Epoch 500: nothing active.
	if n := len(pcs.GetActiveBonds(500)); n != 0 {
		t.Fatalf("expected 0 active at epoch 500, got %d", n)
	}
	// Empty scheme.
	pcs2 := NewProofCustodyScheme(ProofCustodyConfig{MinStake: 1_000})
	if n := len(pcs2.GetActiveBonds(0)); n != 0 {
		t.Fatalf("expected 0 active on empty, got %d", n)
	}
}

func TestProveDataHeld(t *testing.T) {
	pcs := NewProofCustodyScheme(ProofCustodyConfig{MinStake: 1_000, BondDuration: 100})
	bond, _ := pcs.GenerateCustodyBond(types.HexToHash("0xaaaa"), 10)

	proof, err := pcs.ProveDataHeld(bond, 42, []byte("hello, blob data"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if proof.BondID != bond.Commitment || proof.DataIndex != 42 {
		t.Fatalf("mismatch: bondID=%v idx=%d", proof.BondID, proof.DataIndex)
	}
	if proof.DataHash.IsZero() || len(proof.Proof) != 32 {
		t.Fatalf("bad proof: hash_zero=%v len=%d", proof.DataHash.IsZero(), len(proof.Proof))
	}
	if proof.Timestamp != bond.Epoch {
		t.Fatalf("expected timestamp=%d, got %d", bond.Epoch, proof.Timestamp)
	}
	// Nil bond.
	if _, err := pcs.ProveDataHeld(nil, 0, []byte("data")); err != ErrNilBond {
		t.Fatalf("expected ErrNilBond, got %v", err)
	}
	// Empty data.
	if _, err := pcs.ProveDataHeld(bond, 0, nil); err != ErrDataEmpty {
		t.Fatalf("expected ErrDataEmpty for nil, got %v", err)
	}
	if _, err := pcs.ProveDataHeld(bond, 0, []byte{}); err != ErrDataEmpty {
		t.Fatalf("expected ErrDataEmpty for empty, got %v", err)
	}
}

func TestVerifyDataHeld(t *testing.T) {
	pcs := NewProofCustodyScheme(ProofCustodyConfig{MinStake: 1_000, BondDuration: 100})
	bond, _ := pcs.GenerateCustodyBond(types.HexToHash("0xaaaa"), 10)
	proof, _ := pcs.ProveDataHeld(bond, 42, []byte("hello, blob data"))

	if !pcs.VerifyDataHeld(proof) {
		t.Fatal("valid proof should verify")
	}
	// Nil proof.
	if pcs.VerifyDataHeld(nil) {
		t.Fatal("nil proof should not verify")
	}
	// Tampered proof.
	tampered := *proof
	tampered.Proof = make([]byte, len(proof.Proof))
	copy(tampered.Proof, proof.Proof)
	tampered.Proof[0] ^= 0xff
	if pcs.VerifyDataHeld(&tampered) {
		t.Fatal("tampered proof should not verify")
	}
	// Wrong data hash.
	wrongHash := *proof
	wrongHash.DataHash = types.HexToHash("0xdead")
	if pcs.VerifyDataHeld(&wrongHash) {
		t.Fatal("wrong data hash should not verify")
	}
	// Zero data hash.
	if pcs.VerifyDataHeld(&DataHeldProof{
		BondID: types.HexToHash("0x01"), DataHash: types.Hash{}, Proof: make([]byte, 32),
	}) {
		t.Fatal("zero data hash should not verify")
	}
	// Short proof.
	if pcs.VerifyDataHeld(&DataHeldProof{
		BondID: types.HexToHash("0x01"), DataHash: types.HexToHash("0x02"), Proof: make([]byte, 16),
	}) {
		t.Fatal("short proof should not verify")
	}
}

func TestSlashNonCustodian(t *testing.T) {
	pcs := NewProofCustodyScheme(ProofCustodyConfig{
		MinStake: 1_000, BondDuration: 100, SlashingPenalty: 500,
	})
	bond, _ := pcs.GenerateCustodyBond(types.HexToHash("0xaaaa"), 10)
	_ = pcs.RegisterBond(bond)

	challenge := &CustodyBondChallenge{
		BondID: bond.Commitment, DataIndex: 5, Deadline: 200,
		ChallengerID: types.HexToHash("0xbbbb"),
	}
	result, err := pcs.SlashNonCustodian(bond, challenge)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Slashed || result.PenaltyAmount != 500 {
		t.Fatalf("slashed=%v penalty=%d", result.Slashed, result.PenaltyAmount)
	}
	if result.Reason != "failed custody challenge" {
		t.Fatalf("unexpected reason: %s", result.Reason)
	}
	if pcs.BondCount() != 0 {
		t.Fatalf("expected 0 bonds after slashing, got %d", pcs.BondCount())
	}
}

func TestSlashNonCustodian_ErrorCases(t *testing.T) {
	pcs := NewProofCustodyScheme(ProofCustodyConfig{MinStake: 1_000, BondDuration: 100})

	// Nil bond.
	if _, err := pcs.SlashNonCustodian(nil, &CustodyBondChallenge{}); err != ErrNilBond {
		t.Fatalf("expected ErrNilBond, got %v", err)
	}
	// Nil challenge.
	bond, _ := pcs.GenerateCustodyBond(types.HexToHash("0xaa"), 1)
	if _, err := pcs.SlashNonCustodian(bond, nil); err != ErrNilChallenge {
		t.Fatalf("expected ErrNilChallenge, got %v", err)
	}
	// Not registered.
	ch := &CustodyBondChallenge{BondID: bond.Commitment, ChallengerID: types.HexToHash("0xbb")}
	result, err := pcs.SlashNonCustodian(bond, ch)
	if err != ErrBondNotFound || result.Slashed {
		t.Fatalf("expected not found, got err=%v slashed=%v", err, result.Slashed)
	}
	// Bond ID mismatch.
	_ = pcs.RegisterBond(bond)
	mismatch := &CustodyBondChallenge{BondID: types.HexToHash("0xdifferent")}
	result, _ = pcs.SlashNonCustodian(bond, mismatch)
	if result.Slashed {
		t.Fatal("should not slash on bond ID mismatch")
	}
}

func TestSlashNonCustodian_PenaltyCappedAtStake(t *testing.T) {
	pcs := NewProofCustodyScheme(ProofCustodyConfig{
		MinStake: 500, BondDuration: 100, SlashingPenalty: 10_000,
	})
	bond, _ := pcs.GenerateCustodyBond(types.HexToHash("0xaaaa"), 10)
	_ = pcs.RegisterBond(bond)

	ch := &CustodyBondChallenge{BondID: bond.Commitment, ChallengerID: types.HexToHash("0xbb")}
	result, _ := pcs.SlashNonCustodian(bond, ch)
	if result.PenaltyAmount != 500 {
		t.Fatalf("penalty should be capped at stake 500, got %d", result.PenaltyAmount)
	}
}

func TestProveAndVerify_RoundTrip(t *testing.T) {
	pcs := NewProofCustodyScheme(ProofCustodyConfig{MinStake: 1_000, BondDuration: 100})
	bond, _ := pcs.GenerateCustodyBond(types.HexToHash("0xaaaa"), 10)

	for _, tc := range []struct {
		name string
		idx  uint64
		data []byte
	}{
		{"small", 0, []byte("hello")},
		{"1KiB", 1, make([]byte, 1024)},
		{"4KiB", 99, make([]byte, 4096)},
		{"1byte", 42, []byte{0xff}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, err := pcs.ProveDataHeld(bond, tc.idx, tc.data)
			if err != nil {
				t.Fatalf("prove: %v", err)
			}
			if !pcs.VerifyDataHeld(p) {
				t.Fatal("valid proof should verify")
			}
		})
	}
}

func TestProveDataHeld_DifferentDataDifferentProof(t *testing.T) {
	pcs := NewProofCustodyScheme(ProofCustodyConfig{MinStake: 1_000, BondDuration: 100})
	bond, _ := pcs.GenerateCustodyBond(types.HexToHash("0xaaaa"), 10)

	p1, _ := pcs.ProveDataHeld(bond, 0, []byte("data1"))
	p2, _ := pcs.ProveDataHeld(bond, 0, []byte("data2"))
	if p1.DataHash == p2.DataHash {
		t.Fatal("different data should produce different hashes")
	}
	if !pcs.VerifyDataHeld(p1) || !pcs.VerifyDataHeld(p2) {
		t.Fatal("both proofs should verify")
	}
}

func TestConcurrentBondRegistration(t *testing.T) {
	pcs := NewProofCustodyScheme(ProofCustodyConfig{MinStake: 1_000, BondDuration: 100})
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			bond, _ := pcs.GenerateCustodyBond(
				types.BytesToHash([]byte{byte(idx), byte(idx >> 8)}), uint64(idx))
			if err := pcs.RegisterBond(bond); err != nil {
				t.Errorf("goroutine %d: %v", idx, err)
			}
		}(i)
	}
	wg.Wait()
	if pcs.BondCount() != 50 {
		t.Fatalf("expected 50 bonds, got %d", pcs.BondCount())
	}
}

func TestConcurrentVerification(t *testing.T) {
	pcs := NewProofCustodyScheme(ProofCustodyConfig{MinStake: 1_000, BondDuration: 100})
	bond, _ := pcs.GenerateCustodyBond(types.HexToHash("0xaaaa"), 10)
	proof, _ := pcs.ProveDataHeld(bond, 0, []byte("test data"))

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if !pcs.VerifyDataHeld(proof) {
				t.Error("concurrent verification failed")
			}
		}()
	}
	wg.Wait()
}

func TestFullWorkflow(t *testing.T) {
	pcs := NewProofCustodyScheme(ProofCustodyConfig{
		MinStake: 1_000, BondDuration: 100, ChallengeWindow: 10, SlashingPenalty: 500,
	})

	// Generate, register, verify active.
	bond, _ := pcs.GenerateCustodyBond(types.HexToHash("0xnode1"), 10)
	if err := pcs.RegisterBond(bond); err != nil {
		t.Fatalf("register: %v", err)
	}
	if n := len(pcs.GetActiveBonds(50)); n != 1 {
		t.Fatalf("expected 1 active, got %d", n)
	}

	// Prove and verify data held.
	proof, _ := pcs.ProveDataHeld(bond, 7, []byte("blob data"))
	if !pcs.VerifyDataHeld(proof) {
		t.Fatal("proof should verify")
	}

	// Slash a second bond.
	bond2, _ := pcs.GenerateCustodyBond(types.HexToHash("0xnode2"), 20)
	_ = pcs.RegisterBond(bond2)
	ch := &CustodyBondChallenge{
		BondID: bond2.Commitment, DataIndex: 3, Deadline: 200,
		ChallengerID: types.HexToHash("0xchallenger"),
	}
	result, _ := pcs.SlashNonCustodian(bond2, ch)
	if !result.Slashed || result.PenaltyAmount != 500 {
		t.Fatalf("slash: slashed=%v penalty=%d", result.Slashed, result.PenaltyAmount)
	}
	if pcs.BondCount() != 1 {
		t.Fatalf("expected 1 bond remaining, got %d", pcs.BondCount())
	}
}
