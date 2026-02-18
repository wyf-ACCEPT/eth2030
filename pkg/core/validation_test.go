package core

import (
	"math/big"
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

// --- Header Validation Tests ---

func TestValidateHeader_ParentHashMismatch(t *testing.T) {
	v := NewBlockValidator(TestConfig)
	parent := makeValidParent()
	child := makeValidChild(parent)
	child.ParentHash = types.Hash{0xde, 0xad} // wrong parent hash

	err := v.ValidateHeader(child, parent)
	if err == nil {
		t.Fatal("expected error for parent hash mismatch")
	}
}

func TestValidateHeader_GasLimitBoundary(t *testing.T) {
	v := NewBlockValidator(TestConfig)

	tests := []struct {
		name      string
		parentGL  uint64
		childGL   uint64
		expectErr bool
	}{
		{
			name:      "same gas limit",
			parentGL:  30_000_000,
			childGL:   30_000_000,
			expectErr: false,
		},
		{
			name:      "increase by exactly limit-1",
			parentGL:  30_000_000,
			childGL:   30_000_000 + 30_000_000/1024 - 1,
			expectErr: false,
		},
		{
			name:      "increase by exactly limit (should fail)",
			parentGL:  30_000_000,
			childGL:   30_000_000 + 30_000_000/1024,
			expectErr: true,
		},
		{
			name:      "decrease by exactly limit-1",
			parentGL:  30_000_000,
			childGL:   30_000_000 - 30_000_000/1024 + 1,
			expectErr: false,
		},
		{
			name:      "decrease by exactly limit (should fail)",
			parentGL:  30_000_000,
			childGL:   30_000_000 - 30_000_000/1024,
			expectErr: true,
		},
		{
			name:      "below minimum gas limit",
			parentGL:  MinGasLimit + 10,
			childGL:   MinGasLimit - 1,
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parent := makeValidParent()
			parent.GasLimit = tt.parentGL
			child := makeValidChild(parent)
			child.GasLimit = tt.childGL

			err := v.ValidateHeader(child, parent)
			if tt.expectErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.expectErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestValidateHeader_ExtraDataBoundary(t *testing.T) {
	v := NewBlockValidator(TestConfig)
	parent := makeValidParent()

	// Exactly 32 bytes: should pass.
	child := makeValidChild(parent)
	child.Extra = make([]byte, MaxExtraDataSize)
	if err := v.ValidateHeader(child, parent); err != nil {
		t.Errorf("32-byte extra data should be valid: %v", err)
	}

	// 33 bytes: should fail.
	child2 := makeValidChild(parent)
	child2.Extra = make([]byte, MaxExtraDataSize+1)
	if err := v.ValidateHeader(child2, parent); err == nil {
		t.Error("33-byte extra data should be rejected")
	}

	// Empty extra data: should pass.
	child3 := makeValidChild(parent)
	child3.Extra = nil
	if err := v.ValidateHeader(child3, parent); err != nil {
		t.Errorf("empty extra data should be valid: %v", err)
	}
}

func TestValidateHeader_PostMergeFields(t *testing.T) {
	v := NewBlockValidator(TestConfig)
	parent := makeValidParent()

	// Difficulty must be 0 post-merge.
	child := makeValidChild(parent)
	child.Difficulty = big.NewInt(1)
	if err := v.ValidateHeader(child, parent); err == nil {
		t.Error("expected error for non-zero difficulty post-merge")
	}

	// Nil difficulty is treated as 0 (should pass).
	child2 := makeValidChild(parent)
	child2.Difficulty = nil
	if err := v.ValidateHeader(child2, parent); err != nil {
		t.Errorf("nil difficulty should be valid post-merge: %v", err)
	}

	// Nonce must be 0.
	child3 := makeValidChild(parent)
	child3.Nonce = types.BlockNonce{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01}
	if err := v.ValidateHeader(child3, parent); err == nil {
		t.Error("expected error for non-zero nonce post-merge")
	}
}

func TestValidateHeader_GasUsedExactlyAtLimit(t *testing.T) {
	v := NewBlockValidator(TestConfig)
	parent := makeValidParent()

	// Gas used == gas limit: should pass.
	child := makeValidChild(parent)
	child.GasUsed = child.GasLimit
	if err := v.ValidateHeader(child, parent); err != nil {
		t.Errorf("gas used == gas limit should be valid: %v", err)
	}
}

// --- Base Fee Calculation Tests ---

func TestCalcBaseFee_MinimumFloorAt7Wei(t *testing.T) {
	// With a very low base fee and empty block, the decrease should be
	// clamped at 7 wei minimum (EIP-4844 era).
	parent := &types.Header{
		GasLimit: 30_000_000,
		GasUsed:  0,
		BaseFee:  big.NewInt(8), // just above minimum
	}
	got := CalcBaseFee(parent)
	if got.Cmp(big.NewInt(MinBaseFee)) < 0 {
		t.Errorf("base fee %s below minimum %d wei", got, MinBaseFee)
	}
}

func TestCalcBaseFee_MinimumFloorEnforced(t *testing.T) {
	// Parent base fee at exactly minimum: empty block should not decrease further.
	parent := &types.Header{
		GasLimit: 30_000_000,
		GasUsed:  0,
		BaseFee:  big.NewInt(MinBaseFee),
	}
	got := CalcBaseFee(parent)
	if got.Cmp(big.NewInt(MinBaseFee)) < 0 {
		t.Errorf("base fee %s below minimum %d wei", got, MinBaseFee)
	}
}

func TestCalcBaseFee_FullBlock(t *testing.T) {
	// Full block: maximum increase (12.5%).
	parent := &types.Header{
		GasLimit: 30_000_000,
		GasUsed:  30_000_000,
		BaseFee:  big.NewInt(1_000_000_000),
	}
	got := CalcBaseFee(parent)
	// Expected increase: baseFee * (gasUsed - target) / target / 8
	// = 1e9 * 15000000 / 15000000 / 8 = 1e9 / 8 = 125000000
	// New fee = 1e9 + 125000000 = 1125000000
	expected := big.NewInt(1_125_000_000)
	if got.Cmp(expected) != 0 {
		t.Errorf("full block: want %v, got %v", expected, got)
	}
}

func TestCalcBaseFee_EmptyBlock(t *testing.T) {
	// Empty block: maximum decrease (12.5%).
	parent := &types.Header{
		GasLimit: 30_000_000,
		GasUsed:  0,
		BaseFee:  big.NewInt(1_000_000_000),
	}
	got := CalcBaseFee(parent)
	// Expected decrease: baseFee * target / target / 8 = 1e9 / 8 = 125000000
	// New fee = 1e9 - 125000000 = 875000000
	expected := big.NewInt(875_000_000)
	if got.Cmp(expected) != 0 {
		t.Errorf("empty block: want %v, got %v", expected, got)
	}
}

func TestCalcBaseFee_MultiBlockSequence(t *testing.T) {
	// Simulate a sequence of blocks and verify base fee adjusts correctly.
	baseFee := big.NewInt(1_000_000_000) // 1 Gwei

	// Block 1: full -> increase
	header1 := &types.Header{GasLimit: 30_000_000, GasUsed: 30_000_000, BaseFee: baseFee}
	baseFee2 := CalcBaseFee(header1)
	if baseFee2.Cmp(baseFee) <= 0 {
		t.Fatalf("full block should increase base fee: %v -> %v", baseFee, baseFee2)
	}

	// Block 2: still full -> increase more
	header2 := &types.Header{GasLimit: 30_000_000, GasUsed: 30_000_000, BaseFee: baseFee2}
	baseFee3 := CalcBaseFee(header2)
	if baseFee3.Cmp(baseFee2) <= 0 {
		t.Fatalf("consecutive full blocks should keep increasing: %v -> %v", baseFee2, baseFee3)
	}

	// Block 3: empty -> decrease
	header3 := &types.Header{GasLimit: 30_000_000, GasUsed: 0, BaseFee: baseFee3}
	baseFee4 := CalcBaseFee(header3)
	if baseFee4.Cmp(baseFee3) >= 0 {
		t.Fatalf("empty block should decrease base fee: %v -> %v", baseFee3, baseFee4)
	}

	// Block 4: at target -> unchanged
	header4 := &types.Header{GasLimit: 30_000_000, GasUsed: 15_000_000, BaseFee: baseFee4}
	baseFee5 := CalcBaseFee(header4)
	if baseFee5.Cmp(baseFee4) != 0 {
		t.Fatalf("at-target block should keep base fee unchanged: %v -> %v", baseFee4, baseFee5)
	}
}

func TestCalcBaseFee_VeryHighBaseFee(t *testing.T) {
	// Verify behavior with very high base fee (no overflow).
	parent := &types.Header{
		GasLimit: 30_000_000,
		GasUsed:  30_000_000,
		BaseFee:  new(big.Int).SetUint64(1e18), // 1 ETH
	}
	got := CalcBaseFee(parent)
	if got.Cmp(parent.BaseFee) <= 0 {
		t.Errorf("full block with high base fee should still increase: %v", got)
	}
}

// --- Bloom Validation in InsertBlock ---

func TestInsertBlock_BloomMismatchRejected(t *testing.T) {
	bc, _ := testChain(t)

	// Build a valid block first, then corrupt the bloom.
	parent := bc.Genesis()
	validBlock := makeBlock(parent, nil)
	// Create a copy of the block with a corrupted bloom.
	h := *validBlock.Header()
	h.Bloom = types.Bloom{0xff} // intentionally wrong bloom
	block := types.NewBlock(&h, validBlock.Body())

	err := bc.InsertBlock(block)
	if err == nil {
		t.Fatal("expected error for bloom mismatch, got nil")
	}
}

func TestInsertBlock_CorrectBloomAccepted(t *testing.T) {
	bc, _ := testChain(t)

	// Build a valid empty block using makeBlock which computes all
	// consensus-critical fields (state root, tx root, receipt root, bloom).
	parent := bc.Genesis()
	block := makeBlock(parent, nil)

	if err := bc.InsertBlock(block); err != nil {
		t.Fatalf("valid block rejected: %v", err)
	}
}
