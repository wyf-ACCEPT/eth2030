package core

import (
	"math/big"
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

func makeValidParent() *types.Header {
	return &types.Header{
		Number:     big.NewInt(100),
		GasLimit:   30000000,
		GasUsed:    15000000,
		Time:       1000,
		Difficulty: new(big.Int),
		BaseFee:    big.NewInt(1000000000), // 1 Gwei
	}
}

func makeValidChild(parent *types.Header) *types.Header {
	return &types.Header{
		ParentHash: parent.Hash(),
		Number:     new(big.Int).Add(parent.Number, big.NewInt(1)),
		GasLimit:   parent.GasLimit, // same gas limit (within bounds)
		GasUsed:    10000000,
		Time:       parent.Time + 12,
		Difficulty: new(big.Int),
		BaseFee:    CalcBaseFee(parent),
	}
}

func TestValidateHeader_Valid(t *testing.T) {
	v := NewBlockValidator(TestConfig)
	parent := makeValidParent()
	child := makeValidChild(parent)

	if err := v.ValidateHeader(child, parent); err != nil {
		t.Fatalf("valid header rejected: %v", err)
	}
}

func TestValidateHeader_InvalidNumber(t *testing.T) {
	v := NewBlockValidator(TestConfig)
	parent := makeValidParent()
	child := makeValidChild(parent)
	child.Number = big.NewInt(999) // wrong number

	err := v.ValidateHeader(child, parent)
	if err == nil {
		t.Fatal("expected error for invalid number")
	}
}

func TestValidateHeader_TimestampNotIncreasing(t *testing.T) {
	v := NewBlockValidator(TestConfig)
	parent := makeValidParent()
	child := makeValidChild(parent)
	child.Time = parent.Time // same timestamp

	err := v.ValidateHeader(child, parent)
	if err == nil {
		t.Fatal("expected error for non-increasing timestamp")
	}
}

func TestValidateHeader_TimestampBefore(t *testing.T) {
	v := NewBlockValidator(TestConfig)
	parent := makeValidParent()
	child := makeValidChild(parent)
	child.Time = parent.Time - 1 // before parent

	err := v.ValidateHeader(child, parent)
	if err == nil {
		t.Fatal("expected error for timestamp before parent")
	}
}

func TestValidateHeader_GasUsedExceedsLimit(t *testing.T) {
	v := NewBlockValidator(TestConfig)
	parent := makeValidParent()
	child := makeValidChild(parent)
	child.GasUsed = child.GasLimit + 1

	err := v.ValidateHeader(child, parent)
	if err == nil {
		t.Fatal("expected error for gas used > gas limit")
	}
}

func TestValidateHeader_ExtraDataTooLong(t *testing.T) {
	v := NewBlockValidator(TestConfig)
	parent := makeValidParent()
	child := makeValidChild(parent)
	child.Extra = make([]byte, MaxExtraDataSize+1)

	err := v.ValidateHeader(child, parent)
	if err == nil {
		t.Fatal("expected error for extra data too long")
	}
}

func TestValidateHeader_GasLimitTooMuchChange(t *testing.T) {
	v := NewBlockValidator(TestConfig)
	parent := makeValidParent()
	child := makeValidChild(parent)
	child.GasLimit = parent.GasLimit * 2 // way too much change

	err := v.ValidateHeader(child, parent)
	if err == nil {
		t.Fatal("expected error for gas limit change too large")
	}
}

func TestValidateHeader_InvalidDifficulty(t *testing.T) {
	v := NewBlockValidator(TestConfig)
	parent := makeValidParent()
	child := makeValidChild(parent)
	child.Difficulty = big.NewInt(1) // must be 0 post-merge

	err := v.ValidateHeader(child, parent)
	if err == nil {
		t.Fatal("expected error for non-zero difficulty post-merge")
	}
}

func TestValidateHeader_InvalidNonce(t *testing.T) {
	v := NewBlockValidator(TestConfig)
	parent := makeValidParent()
	child := makeValidChild(parent)
	child.Nonce = types.BlockNonce{0x01} // must be zero post-merge

	err := v.ValidateHeader(child, parent)
	if err == nil {
		t.Fatal("expected error for non-zero nonce post-merge")
	}
}

func TestValidateHeader_InvalidBaseFee(t *testing.T) {
	v := NewBlockValidator(TestConfig)
	parent := makeValidParent()
	child := makeValidChild(parent)
	child.BaseFee = big.NewInt(1) // wrong base fee

	err := v.ValidateHeader(child, parent)
	if err == nil {
		t.Fatal("expected error for invalid base fee")
	}
}

func TestCalcBaseFee_ExactTarget(t *testing.T) {
	parent := &types.Header{
		GasLimit: 30000000,
		GasUsed:  15000000, // exactly at target
		BaseFee:  big.NewInt(1000000000),
	}
	got := CalcBaseFee(parent)
	if got.Cmp(parent.BaseFee) != 0 {
		t.Fatalf("at target: want %v, got %v", parent.BaseFee, got)
	}
}

func TestCalcBaseFee_AboveTarget(t *testing.T) {
	parent := &types.Header{
		GasLimit: 30000000,
		GasUsed:  25000000, // above target
		BaseFee:  big.NewInt(1000000000),
	}
	got := CalcBaseFee(parent)
	if got.Cmp(parent.BaseFee) <= 0 {
		t.Fatalf("above target: base fee should increase, got %v", got)
	}
}

func TestCalcBaseFee_BelowTarget(t *testing.T) {
	parent := &types.Header{
		GasLimit: 30000000,
		GasUsed:  5000000, // below target
		BaseFee:  big.NewInt(1000000000),
	}
	got := CalcBaseFee(parent)
	if got.Cmp(parent.BaseFee) >= 0 {
		t.Fatalf("below target: base fee should decrease, got %v", got)
	}
}

func TestCalcBaseFee_NilParent(t *testing.T) {
	parent := &types.Header{
		GasLimit: 30000000,
		GasUsed:  0,
		BaseFee:  nil,
	}
	got := CalcBaseFee(parent)
	if got.Cmp(big.NewInt(1000000000)) != 0 {
		t.Fatalf("nil parent: want 1 Gwei default, got %v", got)
	}
}

func TestValidateBody_NoUncles(t *testing.T) {
	v := NewBlockValidator(TestConfig)
	header := makeValidParent()
	block := types.NewBlock(header, nil)

	if err := v.ValidateBody(block); err != nil {
		t.Fatalf("empty body should be valid: %v", err)
	}
}
