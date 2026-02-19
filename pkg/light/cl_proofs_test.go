package light

import (
	"sync"
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

func TestNewCLProver(t *testing.T) {
	p := NewCLProver(DefaultCLProverConfig())
	if p == nil {
		t.Fatal("NewCLProver returned nil")
	}
	if p.config.ProofType != ProofTypeMerkle {
		t.Errorf("default ProofType = %d, want %d", p.config.ProofType, ProofTypeMerkle)
	}
	if p.config.MaxProofDepth != 32 {
		t.Errorf("default MaxProofDepth = %d, want 32", p.config.MaxProofDepth)
	}
}

func TestNewCLProverDefaults(t *testing.T) {
	// Zero config should get sensible defaults.
	p := NewCLProver(CLProverConfig{})
	if p.config.MaxProofDepth != 32 {
		t.Errorf("expected default MaxProofDepth=32, got %d", p.config.MaxProofDepth)
	}
	if p.config.CacheSize != 512 {
		t.Errorf("expected default CacheSize=512, got %d", p.config.CacheSize)
	}
}

func TestGenerateStateProof(t *testing.T) {
	p := NewCLProver(DefaultCLProverConfig())
	root := types.HexToHash("0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef")

	proof, err := p.GenerateStateProof(100, root)
	if err != nil {
		t.Fatalf("GenerateStateProof: %v", err)
	}
	if proof.Slot != 100 {
		t.Errorf("Slot = %d, want 100", proof.Slot)
	}
	if proof.StateRoot != root {
		t.Errorf("StateRoot mismatch")
	}
	if proof.BeaconRoot.IsZero() {
		t.Error("BeaconRoot should not be zero")
	}
	if len(proof.Proof) != 32 {
		t.Errorf("Proof length = %d, want 32", len(proof.Proof))
	}
	if proof.ProofType != ProofTypeMerkle {
		t.Errorf("ProofType = %d, want %d", proof.ProofType, ProofTypeMerkle)
	}
	if proof.Timestamp.IsZero() {
		t.Error("Timestamp should be set")
	}
}

func TestGenerateStateProofZeroRoot(t *testing.T) {
	p := NewCLProver(DefaultCLProverConfig())
	_, err := p.GenerateStateProof(0, types.Hash{})
	if err != ErrProverZeroStateRoot {
		t.Errorf("expected ErrProverZeroStateRoot, got %v", err)
	}
}

func TestVerifyStateProof(t *testing.T) {
	p := NewCLProver(DefaultCLProverConfig())
	root := types.HexToHash("0xabcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789")

	proof, err := p.GenerateStateProof(42, root)
	if err != nil {
		t.Fatalf("GenerateStateProof: %v", err)
	}
	if !p.VerifyStateProof(proof) {
		t.Error("VerifyStateProof returned false for valid proof")
	}
}

func TestVerifyStateProofTampered(t *testing.T) {
	p := NewCLProver(DefaultCLProverConfig())
	root := types.HexToHash("0xabcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789")

	proof, err := p.GenerateStateProof(42, root)
	if err != nil {
		t.Fatalf("GenerateStateProof: %v", err)
	}

	// Tamper with the beacon root.
	proof.BeaconRoot[0] ^= 0xff
	if p.VerifyStateProof(proof) {
		t.Error("VerifyStateProof should reject tampered proof")
	}
}

func TestVerifyStateProofNil(t *testing.T) {
	p := NewCLProver(DefaultCLProverConfig())
	if p.VerifyStateProof(nil) {
		t.Error("VerifyStateProof(nil) should return false")
	}
}

func TestVerifyStateProofEmptyBranch(t *testing.T) {
	p := NewCLProver(DefaultCLProverConfig())
	proof := &CLStateProof{
		Slot:      1,
		StateRoot: types.HexToHash("0x01"),
	}
	if p.VerifyStateProof(proof) {
		t.Error("VerifyStateProof should reject proof with empty branch")
	}
}

func TestVerifyStateProofZeroStateRoot(t *testing.T) {
	p := NewCLProver(DefaultCLProverConfig())
	proof := &CLStateProof{
		Slot:  1,
		Proof: []types.Hash{{1}},
	}
	if p.VerifyStateProof(proof) {
		t.Error("VerifyStateProof should reject proof with zero state root")
	}
}

func TestCLProverGenerateValidatorProof(t *testing.T) {
	p := NewCLProver(DefaultCLProverConfig())

	vp, err := p.GenerateValidatorProof(0)
	if err != nil {
		t.Fatalf("GenerateValidatorProof: %v", err)
	}
	if vp.Index != 0 {
		t.Errorf("Index = %d, want 0", vp.Index)
	}
	if vp.Balance != deriveBalance(0) {
		t.Errorf("Balance = %d, want %d", vp.Balance, deriveBalance(0))
	}
	if vp.Status != "active" {
		t.Errorf("Status = %q, want %q", vp.Status, "active")
	}
	if vp.StateRoot.IsZero() {
		t.Error("StateRoot should not be zero")
	}
	if len(vp.Proof) != 32 {
		t.Errorf("Proof length = %d, want 32", len(vp.Proof))
	}
}

func TestValidatorProofStatuses(t *testing.T) {
	p := NewCLProver(DefaultCLProverConfig())

	expected := map[uint64]string{
		0: "active",
		1: "pending",
		2: "exited",
		3: "slashed",
		4: "active",
	}
	for idx, want := range expected {
		vp, err := p.GenerateValidatorProof(idx)
		if err != nil {
			t.Fatalf("GenerateValidatorProof(%d): %v", idx, err)
		}
		if vp.Status != want {
			t.Errorf("validator %d status = %q, want %q", idx, vp.Status, want)
		}
	}
}

func TestVerifyValidatorProof(t *testing.T) {
	p := NewCLProver(DefaultCLProverConfig())

	vp, err := p.GenerateValidatorProof(7)
	if err != nil {
		t.Fatalf("GenerateValidatorProof: %v", err)
	}
	if !p.VerifyValidatorProof(vp) {
		t.Error("VerifyValidatorProof returned false for valid proof")
	}
}

func TestVerifyValidatorProofTampered(t *testing.T) {
	p := NewCLProver(DefaultCLProverConfig())

	vp, err := p.GenerateValidatorProof(7)
	if err != nil {
		t.Fatalf("GenerateValidatorProof: %v", err)
	}

	// Tamper with balance.
	vp.Balance += 1
	if p.VerifyValidatorProof(vp) {
		t.Error("VerifyValidatorProof should reject tampered balance")
	}
}

func TestVerifyValidatorProofNil(t *testing.T) {
	p := NewCLProver(DefaultCLProverConfig())
	if p.VerifyValidatorProof(nil) {
		t.Error("VerifyValidatorProof(nil) should return false")
	}
}

func TestGenerateAttestationProof(t *testing.T) {
	p := NewCLProver(DefaultCLProverConfig())

	ap, err := p.GenerateAttestationProof(100, 3)
	if err != nil {
		t.Fatalf("GenerateAttestationProof: %v", err)
	}
	if ap.Slot != 100 {
		t.Errorf("Slot = %d, want 100", ap.Slot)
	}
	if ap.CommitteeIndex != 3 {
		t.Errorf("CommitteeIndex = %d, want 3", ap.CommitteeIndex)
	}
	if len(ap.AggregationBits) != 8 {
		t.Errorf("AggregationBits len = %d, want 8", len(ap.AggregationBits))
	}
	if len(ap.Proof) != 32 {
		t.Errorf("Proof length = %d, want 32", len(ap.Proof))
	}
}

func TestAttestationProofDeterministic(t *testing.T) {
	p := NewCLProver(DefaultCLProverConfig())

	ap1, _ := p.GenerateAttestationProof(50, 2)
	// Create a fresh prover to avoid cache hit returning same pointer.
	p2 := NewCLProver(DefaultCLProverConfig())
	ap2, _ := p2.GenerateAttestationProof(50, 2)

	if ap1.Slot != ap2.Slot || ap1.CommitteeIndex != ap2.CommitteeIndex {
		t.Error("attestation proof should be deterministic")
	}
	for i := range ap1.AggregationBits {
		if ap1.AggregationBits[i] != ap2.AggregationBits[i] {
			t.Error("aggregation bits should be deterministic")
			break
		}
	}
	for i := range ap1.Proof {
		if ap1.Proof[i] != ap2.Proof[i] {
			t.Error("proof branch should be deterministic")
			break
		}
	}
}

func TestBatchVerify(t *testing.T) {
	p := NewCLProver(DefaultCLProverConfig())

	var proofs []*CLStateProof
	for i := uint64(1); i <= 5; i++ {
		root := types.BytesToHash([]byte{byte(i), 0x01, 0x02, 0x03})
		sp, err := p.GenerateStateProof(i, root)
		if err != nil {
			t.Fatalf("GenerateStateProof(%d): %v", i, err)
		}
		proofs = append(proofs, sp)
	}

	if !p.BatchVerify(proofs) {
		t.Error("BatchVerify should return true for valid proofs")
	}
}

func TestBatchVerifyEmpty(t *testing.T) {
	p := NewCLProver(DefaultCLProverConfig())
	if p.BatchVerify(nil) {
		t.Error("BatchVerify(nil) should return false")
	}
	if p.BatchVerify([]*CLStateProof{}) {
		t.Error("BatchVerify([]) should return false")
	}
}

func TestBatchVerifyOneBad(t *testing.T) {
	p := NewCLProver(DefaultCLProverConfig())

	good, _ := p.GenerateStateProof(1, types.HexToHash("0x01"))
	bad, _ := p.GenerateStateProof(2, types.HexToHash("0x02"))
	bad.BeaconRoot[0] ^= 0xff // tamper

	if p.BatchVerify([]*CLStateProof{good, bad}) {
		t.Error("BatchVerify should reject batch with one bad proof")
	}
}

func TestProofTypeSTARK(t *testing.T) {
	config := CLProverConfig{
		ProofType:     ProofTypeSTARK,
		CacheSize:     64,
		MaxProofDepth: 16,
	}
	p := NewCLProver(config)
	root := types.HexToHash("0xdeadbeef")

	proof, err := p.GenerateStateProof(10, root)
	if err != nil {
		t.Fatalf("GenerateStateProof (STARK): %v", err)
	}
	if proof.ProofType != ProofTypeSTARK {
		t.Errorf("ProofType = %d, want %d", proof.ProofType, ProofTypeSTARK)
	}
	if len(proof.Proof) != 16 {
		t.Errorf("Proof length = %d, want 16", len(proof.Proof))
	}
	if !p.VerifyStateProof(proof) {
		t.Error("STARK proof should verify")
	}
}

func TestProofTypeSNARK(t *testing.T) {
	config := CLProverConfig{
		ProofType:     ProofTypeSNARK,
		CacheSize:     64,
		MaxProofDepth: 20,
	}
	p := NewCLProver(config)
	root := types.HexToHash("0xcafebabe")

	proof, err := p.GenerateStateProof(99, root)
	if err != nil {
		t.Fatalf("GenerateStateProof (SNARK): %v", err)
	}
	if proof.ProofType != ProofTypeSNARK {
		t.Errorf("ProofType = %d, want %d", proof.ProofType, ProofTypeSNARK)
	}
	if !p.VerifyStateProof(proof) {
		t.Error("SNARK proof should verify")
	}
}

func TestProofTypesProduceDifferentBranches(t *testing.T) {
	root := types.HexToHash("0xaaaa")
	slot := uint64(5)

	configs := []CLProverConfig{
		{ProofType: ProofTypeMerkle, CacheSize: 64, MaxProofDepth: 8},
		{ProofType: ProofTypeSTARK, CacheSize: 64, MaxProofDepth: 8},
		{ProofType: ProofTypeSNARK, CacheSize: 64, MaxProofDepth: 8},
	}
	var proofs []*CLStateProof
	for _, c := range configs {
		p := NewCLProver(c)
		sp, _ := p.GenerateStateProof(slot, root)
		proofs = append(proofs, sp)
	}

	// All three should have different branch hashes due to domain tags.
	if proofs[0].Proof[0] == proofs[1].Proof[0] {
		t.Error("Merkle and STARK branches should differ")
	}
	if proofs[1].Proof[0] == proofs[2].Proof[0] {
		t.Error("STARK and SNARK branches should differ")
	}
	if proofs[0].Proof[0] == proofs[2].Proof[0] {
		t.Error("Merkle and SNARK branches should differ")
	}
}

func TestCacheHit(t *testing.T) {
	p := NewCLProver(DefaultCLProverConfig())
	root := types.HexToHash("0xfeedface")

	sp1, _ := p.GenerateStateProof(10, root)
	sp2, _ := p.GenerateStateProof(10, root)

	// Cached: should return the same pointer.
	if sp1 != sp2 {
		t.Error("expected cache hit to return same proof pointer")
	}
}

func TestCacheEviction(t *testing.T) {
	config := CLProverConfig{
		ProofType:     ProofTypeMerkle,
		CacheSize:     2,
		MaxProofDepth: 8,
	}
	p := NewCLProver(config)

	// Fill cache beyond capacity.
	for i := uint64(1); i <= 5; i++ {
		root := types.BytesToHash([]byte{byte(i)})
		_, err := p.GenerateStateProof(i, root)
		if err != nil {
			t.Fatalf("GenerateStateProof(%d): %v", i, err)
		}
	}

	p.mu.RLock()
	cacheLen := len(p.cache)
	p.mu.RUnlock()

	if cacheLen > config.CacheSize {
		t.Errorf("cache size = %d, should be <= %d", cacheLen, config.CacheSize)
	}
}

func TestConcurrentProofGeneration(t *testing.T) {
	p := NewCLProver(DefaultCLProverConfig())

	var wg sync.WaitGroup
	errs := make(chan error, 100)

	for i := 1; i <= 100; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			root := types.BytesToHash([]byte{byte(idx%256 + 1), byte(idx/256 + 1)})
			sp, err := p.GenerateStateProof(uint64(idx), root)
			if err != nil {
				errs <- err
				return
			}
			if !p.VerifyStateProof(sp) {
				errs <- ErrProverInvalidProof
			}
		}(i)
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("concurrent error: %v", err)
	}
}

func TestConcurrentValidatorProofs(t *testing.T) {
	p := NewCLProver(DefaultCLProverConfig())

	var wg sync.WaitGroup
	errs := make(chan error, 50)

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			vp, err := p.GenerateValidatorProof(uint64(idx))
			if err != nil {
				errs <- err
				return
			}
			if !p.VerifyValidatorProof(vp) {
				errs <- ErrProverInvalidProof
			}
		}(i)
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("concurrent validator error: %v", err)
	}
}

func TestDeriveBalance(t *testing.T) {
	b0 := deriveBalance(0)
	if b0 != 32_000_000_000 {
		t.Errorf("balance(0) = %d, want 32000000000", b0)
	}
	b1 := deriveBalance(1)
	if b1 != 32_000_000_001 {
		t.Errorf("balance(1) = %d, want 32000000001", b1)
	}
}

func TestDeriveStatus(t *testing.T) {
	tests := []struct {
		index  uint64
		status string
	}{
		{0, "active"},
		{1, "pending"},
		{2, "exited"},
		{3, "slashed"},
		{4, "active"},
		{100, "active"},
		{101, "pending"},
	}
	for _, tt := range tests {
		got := deriveStatus(tt.index)
		if got != tt.status {
			t.Errorf("deriveStatus(%d) = %q, want %q", tt.index, got, tt.status)
		}
	}
}

func TestDeriveAggBits(t *testing.T) {
	bits := deriveAggBits(10, 3)
	if len(bits) != 8 {
		t.Fatalf("aggBits len = %d, want 8", len(bits))
	}

	// Deterministic: same inputs yield same output.
	bits2 := deriveAggBits(10, 3)
	for i := range bits {
		if bits[i] != bits2[i] {
			t.Fatal("deriveAggBits should be deterministic")
		}
	}

	// Different inputs yield different output (with high probability).
	bits3 := deriveAggBits(10, 4)
	same := true
	for i := range bits {
		if bits[i] != bits3[i] {
			same = false
			break
		}
	}
	if same {
		t.Error("different committee indices should produce different bits")
	}
}

func TestStateProofDifferentSlots(t *testing.T) {
	p := NewCLProver(DefaultCLProverConfig())
	root := types.HexToHash("0xaaaa")

	sp1, _ := p.GenerateStateProof(1, root)
	sp2, _ := p.GenerateStateProof(2, root)

	if sp1.BeaconRoot == sp2.BeaconRoot {
		t.Error("different slots should produce different beacon roots")
	}
}

func TestStateProofDifferentRoots(t *testing.T) {
	p := NewCLProver(DefaultCLProverConfig())

	sp1, _ := p.GenerateStateProof(1, types.HexToHash("0xaa"))
	sp2, _ := p.GenerateStateProof(1, types.HexToHash("0xbb"))

	if sp1.BeaconRoot == sp2.BeaconRoot {
		t.Error("different state roots should produce different beacon roots")
	}
}
