package consensus

import (
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

func TestPQChainSecurityBlockHash(t *testing.T) {
	header := &types.Header{
		ParentHash: types.HexToHash("0x01"),
		Number:     big.NewInt(100),
		GasLimit:   30000000,
		GasUsed:    21000,
		Time:       1700000000,
		Extra:      []byte("test"),
	}

	hash, err := PQBlockHash(header)
	if err != nil {
		t.Fatalf("PQBlockHash error: %v", err)
	}
	if hash.IsZero() {
		t.Error("PQ block hash should not be zero")
	}

	// Same header should produce same hash.
	hash2, err := PQBlockHash(header)
	if err != nil {
		t.Fatalf("PQBlockHash 2 error: %v", err)
	}
	if hash != hash2 {
		t.Error("PQ block hash should be deterministic")
	}
}

func TestPQChainSecurityBlockHashNilHeader(t *testing.T) {
	_, err := PQBlockHash(nil)
	if err != ErrPQChainNilHeader {
		t.Errorf("PQBlockHash(nil) error = %v, want ErrPQChainNilHeader", err)
	}
}

func TestPQChainSecurityBlockHashDiffers(t *testing.T) {
	h1 := &types.Header{
		Number: big.NewInt(1),
		Time:   100,
	}
	h2 := &types.Header{
		Number: big.NewInt(2),
		Time:   200,
	}

	hash1, _ := PQBlockHash(h1)
	hash2, _ := PQBlockHash(h2)
	if hash1 == hash2 {
		t.Error("different headers should produce different PQ hashes")
	}
}

func TestPQChainSecurityValidatorCreation(t *testing.T) {
	v := NewPQChainValidator(nil)
	if v == nil {
		t.Fatal("validator should not be nil")
	}
	if v.config.SecurityLevel != PQSecurityPreferred {
		t.Errorf("default security level = %d, want PQSecurityPreferred", v.config.SecurityLevel)
	}
}

func TestPQChainSecurityCustomConfig(t *testing.T) {
	config := &PQChainConfig{
		SecurityLevel:      PQSecurityRequired,
		PQThresholdPercent: 80,
		TransitionEpoch:    100,
		SlotsPerEpoch:      32,
	}
	v := NewPQChainValidator(config)
	if v.config.SecurityLevel != PQSecurityRequired {
		t.Errorf("security level = %d, want PQSecurityRequired", v.config.SecurityLevel)
	}
}

func TestPQChainSecurityPQEnforcement(t *testing.T) {
	config := &PQChainConfig{
		SecurityLevel:      PQSecurityPreferred,
		PQThresholdPercent: 67,
		TransitionEpoch:    10,
		SlotsPerEpoch:      32,
	}
	v := NewPQChainValidator(config)

	// Before transition epoch.
	if v.IsPQEnforced(5) {
		t.Error("PQ should not be enforced before transition epoch")
	}

	// After transition, no validators registered.
	if v.IsPQEnforced(20) {
		t.Error("PQ should not be enforced without registered validators")
	}

	// Register validators above threshold.
	v.RegisterEpochValidators(20, 70, 100)
	if !v.IsPQEnforced(20) {
		t.Error("PQ should be enforced when threshold is met")
	}

	// Register validators below threshold.
	v.RegisterEpochValidators(21, 50, 100)
	if v.IsPQEnforced(21) {
		t.Error("PQ should not be enforced below threshold")
	}
}

func TestPQChainSecurityPQEnforcementRequired(t *testing.T) {
	config := &PQChainConfig{
		SecurityLevel:      PQSecurityRequired,
		PQThresholdPercent: 67,
		TransitionEpoch:    10,
		SlotsPerEpoch:      32,
	}
	v := NewPQChainValidator(config)

	// Required mode: always enforced after transition.
	if v.IsPQEnforced(5) {
		t.Error("PQ should not be enforced before transition")
	}
	if !v.IsPQEnforced(10) {
		t.Error("PQ should be enforced in required mode after transition")
	}
}

func TestPQChainSecurityPQEnforcementOptional(t *testing.T) {
	config := &PQChainConfig{
		SecurityLevel: PQSecurityOptional,
		SlotsPerEpoch: 32,
	}
	v := NewPQChainValidator(config)

	// Optional mode: never enforced.
	v.RegisterEpochValidators(100, 100, 100)
	if v.IsPQEnforced(100) {
		t.Error("PQ should not be enforced in optional mode")
	}
}

func TestPQChainSecurityValidateChainPQSecurity(t *testing.T) {
	v := NewPQChainValidator(nil)

	headers := []*types.Header{
		{Number: big.NewInt(1), Time: 100, Extra: []byte("block1")},
		{Number: big.NewInt(2), Time: 200, Extra: []byte("block2")},
		{Number: big.NewInt(3), Time: 300, Extra: []byte("block3")},
	}

	result, err := v.ValidateChainPQSecurity(headers)
	if err != nil {
		t.Fatalf("ValidateChainPQSecurity error: %v", err)
	}
	if result.TotalBlocks != 3 {
		t.Errorf("total blocks = %d, want 3", result.TotalBlocks)
	}
	if result.PQCompliant != 3 {
		t.Errorf("PQ compliant = %d, want 3", result.PQCompliant)
	}
	if result.ComplianceScore != 1.0 {
		t.Errorf("compliance score = %f, want 1.0", result.ComplianceScore)
	}
}

func TestPQChainSecurityValidateEmptyChain(t *testing.T) {
	v := NewPQChainValidator(nil)
	_, err := v.ValidateChainPQSecurity(nil)
	if err != ErrPQChainEmptyChain {
		t.Errorf("empty chain error = %v, want ErrPQChainEmptyChain", err)
	}
}

func TestPQChainSecurityValidateNilHeaders(t *testing.T) {
	v := NewPQChainValidator(nil)
	headers := []*types.Header{nil, {Number: big.NewInt(1)}}
	result, err := v.ValidateChainPQSecurity(headers)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.NonCompliant != 1 {
		t.Errorf("non-compliant = %d, want 1", result.NonCompliant)
	}
}

func TestPQChainSecurityForkChoiceAddBlock(t *testing.T) {
	v := NewPQChainValidator(DefaultPQChainConfig())
	fc := NewPQForkChoice(v)

	header := &types.Header{
		Number: big.NewInt(1),
		Time:   100,
		Extra:  []byte("block1"),
	}

	err := fc.AddBlock(header)
	if err != nil {
		t.Fatalf("AddBlock error: %v", err)
	}
	if fc.BlockCount() != 1 {
		t.Errorf("block count = %d, want 1", fc.BlockCount())
	}
}

func TestPQChainSecurityForkChoiceAddBlockNil(t *testing.T) {
	v := NewPQChainValidator(DefaultPQChainConfig())
	fc := NewPQForkChoice(v)

	err := fc.AddBlock(nil)
	if err != ErrPQChainNilHeader {
		t.Errorf("AddBlock(nil) error = %v, want ErrPQChainNilHeader", err)
	}
}

func TestPQChainSecurityForkChoiceAttestation(t *testing.T) {
	config := &PQChainConfig{
		SecurityLevel:      PQSecurityOptional,
		PQThresholdPercent: 67,
		TransitionEpoch:    0,
		SlotsPerEpoch:      32,
	}
	v := NewPQChainValidator(config)
	fc := NewPQForkChoice(v)

	root := types.HexToHash("0xaa")
	att := &PQAttestationRecord{
		BlockRoot:      root,
		ValidatorIndex: 1,
		HasPQSig:       true,
		Slot:           100,
		Weight:         1000,
	}

	err := fc.AddAttestation(att)
	if err != nil {
		t.Fatalf("AddAttestation error: %v", err)
	}

	weight := fc.GetWeight(root)
	// PQ-signed attestations get 10% bonus: 1000 + 100 = 1100.
	if weight != 1100 {
		t.Errorf("weight = %d, want 1100", weight)
	}
}

func TestPQChainSecurityForkChoiceAttestationNonPQ(t *testing.T) {
	config := &PQChainConfig{
		SecurityLevel: PQSecurityOptional,
		SlotsPerEpoch: 32,
	}
	v := NewPQChainValidator(config)
	fc := NewPQForkChoice(v)

	root := types.HexToHash("0xbb")
	att := &PQAttestationRecord{
		BlockRoot:      root,
		ValidatorIndex: 2,
		HasPQSig:       false,
		Slot:           100,
		Weight:         1000,
	}

	err := fc.AddAttestation(att)
	if err != nil {
		t.Fatalf("AddAttestation error: %v", err)
	}

	weight := fc.GetWeight(root)
	if weight != 1000 {
		t.Errorf("weight = %d, want 1000", weight)
	}
}

func TestPQChainSecurityChainCommitment(t *testing.T) {
	hashes := []types.Hash{
		types.HexToHash("0x01"),
		types.HexToHash("0x02"),
		types.HexToHash("0x03"),
	}

	commitment := NewPQChainCommitment(10, hashes)
	if commitment.Epoch != 10 {
		t.Errorf("epoch = %d, want 10", commitment.Epoch)
	}
	if commitment.CommitmentRoot.IsZero() {
		t.Error("commitment root should not be zero")
	}
	if !commitment.Verify() {
		t.Error("commitment should verify")
	}
}

func TestPQChainSecurityHistoryAccumulator(t *testing.T) {
	acc := NewPQHistoryAccumulator()
	if acc.Size() != 0 {
		t.Errorf("initial size = %d, want 0", acc.Size())
	}

	h1 := types.HexToHash("0x01")
	h2 := types.HexToHash("0x02")

	acc.Append(h1)
	acc.Append(h2)

	if acc.Size() != 2 {
		t.Errorf("size = %d, want 2", acc.Size())
	}

	root := acc.Root()
	if root.IsZero() {
		t.Error("root should not be zero")
	}

	if !acc.Verify(h1) {
		t.Error("h1 should be in accumulator")
	}
	if !acc.Verify(h2) {
		t.Error("h2 should be in accumulator")
	}
	if acc.Verify(types.HexToHash("0x03")) {
		t.Error("h3 should not be in accumulator")
	}
}

func TestPQChainSecurityHistoryAccumulatorDeterministic(t *testing.T) {
	acc1 := NewPQHistoryAccumulator()
	acc2 := NewPQHistoryAccumulator()

	for i := 0; i < 10; i++ {
		h := types.HexToHash("0x" + string(rune('a'+i)))
		acc1.Append(h)
		acc2.Append(h)
	}

	if acc1.Root() != acc2.Root() {
		t.Error("same input should produce same root")
	}
}

func TestPQChainSecurityStats(t *testing.T) {
	v := NewPQChainValidator(nil)
	headers := []*types.Header{
		{Number: big.NewInt(1)},
	}
	v.ValidateChainPQSecurity(headers)

	bv, bf, av, af := v.Stats()
	if bv != 1 {
		t.Errorf("blocks validated = %d, want 1", bv)
	}
	if bf != 0 {
		t.Errorf("blocks failed = %d, want 0", bf)
	}
	if av != 0 {
		t.Errorf("attestations valid = %d, want 0", av)
	}
	if af != 0 {
		t.Errorf("attestations failed = %d, want 0", af)
	}
}

func TestPQChainSecurityPQMerkleRoot(t *testing.T) {
	// Empty.
	root := pqMerkleRoot(nil)
	if !root.IsZero() {
		t.Error("empty merkle root should be zero")
	}

	// Single.
	single := pqMerkleRoot([]types.Hash{types.HexToHash("0x01")})
	if single.IsZero() {
		t.Error("single-leaf merkle root should not be zero")
	}

	// Two leaves.
	two := pqMerkleRoot([]types.Hash{
		types.HexToHash("0x01"),
		types.HexToHash("0x02"),
	})
	if two.IsZero() {
		t.Error("two-leaf merkle root should not be zero")
	}
	if two == single {
		t.Error("different input should produce different root")
	}
}

func TestValidatePQTransition(t *testing.T) {
	cfg := DefaultPQChainConfig()

	// Valid transition.
	if err := ValidatePQTransition(PQSecurityOptional, PQSecurityPreferred, cfg.TransitionEpoch, cfg); err != nil {
		t.Errorf("valid transition: %v", err)
	}

	// Before transition epoch.
	if err := ValidatePQTransition(PQSecurityOptional, PQSecurityPreferred, 0, cfg); err == nil {
		t.Error("expected error for before transition epoch")
	}

	// Level decrease.
	if err := ValidatePQTransition(PQSecurityRequired, PQSecurityOptional, cfg.TransitionEpoch, cfg); err == nil {
		t.Error("expected error for level decrease")
	}
}

func TestValidatePQChainConfig(t *testing.T) {
	cfg := DefaultPQChainConfig()
	if err := ValidatePQChainConfig(cfg); err != nil {
		t.Errorf("valid config: %v", err)
	}

	if err := ValidatePQChainConfig(nil); err == nil {
		t.Error("expected error for nil config")
	}

	bad := *cfg
	bad.PQThresholdPercent = 200
	if err := ValidatePQChainConfig(&bad); err == nil {
		t.Error("expected error for threshold > 100")
	}
}

func TestIntegratePQForkChoice(t *testing.T) {
	t.Run("applies PQ weights to main fork choice", func(t *testing.T) {
		config := &PQChainConfig{
			SecurityLevel: PQSecurityOptional,
			SlotsPerEpoch: 32,
		}
		v := NewPQChainValidator(config)
		pqFC := NewPQForkChoice(v)

		// Create a standard fork choice and add a block.
		blockHash := types.HexToHash("0xaa")
		parentHash := types.HexToHash("0x00")
		fc := NewForkChoiceStore(ForkChoiceConfig{})
		fc.AddBlock(blockHash, parentHash, 1)

		// Add PQ attestation to the PQ fork choice.
		pqFC.AddAttestation(&PQAttestationRecord{
			BlockRoot: blockHash,
			HasPQSig:  true,
			Weight:    100,
			Slot:      1,
		})

		// Integrate PQ weights into the main fork choice.
		count := IntegratePQForkChoice(fc, pqFC)
		if count != 1 {
			t.Errorf("applied = %d, want 1", count)
		}
	})

	t.Run("skips blocks not in main fork choice", func(t *testing.T) {
		v := NewPQChainValidator(nil)
		pqFC := NewPQForkChoice(v)

		unknown := types.HexToHash("0xcc")
		pqFC.AddAttestation(&PQAttestationRecord{
			BlockRoot: unknown,
			HasPQSig:  true,
			Weight:    50,
			Slot:      1,
		})

		fc := NewForkChoiceStore(ForkChoiceConfig{})
		count := IntegratePQForkChoice(fc, pqFC)
		if count != 0 {
			t.Errorf("applied = %d, want 0 (block not in FC)", count)
		}
	})

	t.Run("nil parameters", func(t *testing.T) {
		count := IntegratePQForkChoice(nil, nil)
		if count != 0 {
			t.Errorf("nil inputs should return 0, got %d", count)
		}
	})
}
