package engine

import (
	"math/big"
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

func validPayload() *ExecutionPayloadV3 {
	return &ExecutionPayloadV3{
		ExecutionPayloadV2: ExecutionPayloadV2{
			ExecutionPayloadV1: ExecutionPayloadV1{
				GasLimit:      30_000_000,
				GasUsed:       15_000_000,
				Timestamp:     1000,
				BaseFeePerGas: big.NewInt(1_000_000_000),
				ExtraData:     []byte("test"),
			},
		},
	}
}

func TestNewPayloadProcessor(t *testing.T) {
	pp := NewPayloadProcessor()
	if pp == nil {
		t.Fatal("expected non-nil processor")
	}
	if pp.minGasLimit != MinGasLimit {
		t.Errorf("expected min gas limit %d, got %d", MinGasLimit, pp.minGasLimit)
	}
}

func TestValidatePayloadValid(t *testing.T) {
	pp := NewPayloadProcessor()
	if err := pp.ValidatePayload(validPayload()); err != nil {
		t.Errorf("expected valid payload, got error: %v", err)
	}
}

func TestValidatePayloadNil(t *testing.T) {
	pp := NewPayloadProcessor()
	if err := pp.ValidatePayload(nil); err != ErrPPNilPayload {
		t.Errorf("expected ErrPPNilPayload, got %v", err)
	}
}

func TestValidatePayloadGasExceedsLimit(t *testing.T) {
	pp := NewPayloadProcessor()
	p := validPayload()
	p.GasUsed = p.GasLimit + 1
	if err := pp.ValidatePayload(p); err == nil {
		t.Error("expected error for gas exceeding limit")
	}
}

func TestValidatePayloadZeroGasLimit(t *testing.T) {
	pp := NewPayloadProcessor()
	p := validPayload()
	p.GasLimit = 0
	p.GasUsed = 0
	if err := pp.ValidatePayload(p); err != ErrPPZeroGasLimit {
		t.Errorf("expected ErrPPZeroGasLimit, got %v", err)
	}
}

func TestValidatePayloadExtraDataTooLong(t *testing.T) {
	pp := NewPayloadProcessor()
	p := validPayload()
	p.ExtraData = make([]byte, MaxExtraDataSize+1)
	if err := pp.ValidatePayload(p); err == nil {
		t.Error("expected error for extra data too long")
	}
}

func TestValidatePayloadNilBaseFee(t *testing.T) {
	pp := NewPayloadProcessor()
	p := validPayload()
	p.BaseFeePerGas = nil
	if err := pp.ValidatePayload(p); err != ErrPPNilBaseFee {
		t.Errorf("expected ErrPPNilBaseFee, got %v", err)
	}
}

func TestValidatePayloadNegativeBaseFee(t *testing.T) {
	pp := NewPayloadProcessor()
	p := validPayload()
	p.BaseFeePerGas = big.NewInt(-1)
	if err := pp.ValidatePayload(p); err == nil {
		t.Error("expected error for negative base fee")
	}
}

func TestValidateBlockHashNilPayload(t *testing.T) {
	pp := NewPayloadProcessor()
	if err := pp.ValidateBlockHash(nil); err != ErrPPNilPayload {
		t.Errorf("expected ErrPPNilPayload, got %v", err)
	}
}

func TestValidateBlockHashZeroHash(t *testing.T) {
	pp := NewPayloadProcessor()
	p := validPayload()
	// Zero block hash means "don't check", should pass.
	p.BlockHash = types.Hash{}
	if err := pp.ValidateBlockHash(p); err != nil {
		t.Errorf("expected nil error for zero block hash, got %v", err)
	}
}

func TestValidateBlockHashMismatch(t *testing.T) {
	pp := NewPayloadProcessor()
	p := validPayload()
	p.BaseFeePerGas = big.NewInt(7)
	// Set an arbitrary hash that won't match the computed one.
	p.BlockHash = types.HexToHash("0x1111111111111111111111111111111111111111111111111111111111111111")
	err := pp.ValidateBlockHash(p)
	if err == nil {
		t.Error("expected block hash mismatch error")
	}
}

func TestValidateBlockHashCorrect(t *testing.T) {
	pp := NewPayloadProcessor()
	p := validPayload()
	p.BaseFeePerGas = big.NewInt(7)
	// Compute the correct hash and set it.
	v4 := &ExecutionPayloadV4{ExecutionPayloadV3: *p}
	header := PayloadToHeader(v4)
	p.BlockHash = header.Hash()
	if err := pp.ValidateBlockHash(p); err != nil {
		t.Errorf("expected valid block hash, got error: %v", err)
	}
}

func TestValidateGasLimitsNilPayload(t *testing.T) {
	pp := NewPayloadProcessor()
	if err := pp.ValidateGasLimits(nil, 30_000_000); err != ErrPPNilPayload {
		t.Errorf("expected ErrPPNilPayload, got %v", err)
	}
}

func TestValidateGasLimitsBelowMinimum(t *testing.T) {
	pp := NewPayloadProcessor()
	p := validPayload()
	p.GasLimit = MinGasLimit - 1
	if err := pp.ValidateGasLimits(p, 30_000_000); err == nil {
		t.Error("expected error for gas limit below minimum")
	}
}

func TestValidateGasLimitsValidIncrease(t *testing.T) {
	pp := NewPayloadProcessor()
	p := validPayload()
	parentGasLimit := uint64(30_000_000)
	// Increase by less than parent/1024.
	p.GasLimit = parentGasLimit + parentGasLimit/GasLimitBoundDivisor - 1
	if err := pp.ValidateGasLimits(p, parentGasLimit); err != nil {
		t.Errorf("expected valid gas limit increase, got error: %v", err)
	}
}

func TestValidateGasLimitsExcessiveIncrease(t *testing.T) {
	pp := NewPayloadProcessor()
	p := validPayload()
	parentGasLimit := uint64(30_000_000)
	// Increase by exactly parent/1024 (should fail: diff >= maxDelta).
	p.GasLimit = parentGasLimit + parentGasLimit/GasLimitBoundDivisor
	if err := pp.ValidateGasLimits(p, parentGasLimit); err == nil {
		t.Error("expected error for excessive gas limit increase")
	}
}

func TestValidateGasLimitsValidDecrease(t *testing.T) {
	pp := NewPayloadProcessor()
	p := validPayload()
	parentGasLimit := uint64(30_000_000)
	// Decrease by less than parent/1024.
	p.GasLimit = parentGasLimit - parentGasLimit/GasLimitBoundDivisor + 1
	if err := pp.ValidateGasLimits(p, parentGasLimit); err != nil {
		t.Errorf("expected valid gas limit decrease, got error: %v", err)
	}
}

func TestValidateGasLimitsNoChange(t *testing.T) {
	pp := NewPayloadProcessor()
	p := validPayload()
	if err := pp.ValidateGasLimits(p, p.GasLimit); err != nil {
		t.Errorf("expected valid for no change, got error: %v", err)
	}
}

func TestValidateTimestampNilPayload(t *testing.T) {
	pp := NewPayloadProcessor()
	if err := pp.ValidateTimestamp(nil, 100); err != ErrPPNilPayload {
		t.Errorf("expected ErrPPNilPayload, got %v", err)
	}
}

func TestValidateTimestampValid(t *testing.T) {
	pp := NewPayloadProcessor()
	p := validPayload()
	p.Timestamp = 200
	if err := pp.ValidateTimestamp(p, 100); err != nil {
		t.Errorf("expected valid timestamp, got error: %v", err)
	}
}

func TestValidateTimestampEqual(t *testing.T) {
	pp := NewPayloadProcessor()
	p := validPayload()
	p.Timestamp = 100
	if err := pp.ValidateTimestamp(p, 100); err == nil {
		t.Error("expected error for equal timestamp")
	}
}

func TestValidateTimestampBefore(t *testing.T) {
	pp := NewPayloadProcessor()
	p := validPayload()
	p.Timestamp = 50
	if err := pp.ValidateTimestamp(p, 100); err == nil {
		t.Error("expected error for timestamp before parent")
	}
}

func TestValidateBaseFeeNilPayload(t *testing.T) {
	pp := NewPayloadProcessor()
	if err := pp.ValidateBaseFee(nil, 100, 50, 50); err != ErrPPNilPayload {
		t.Errorf("expected ErrPPNilPayload, got %v", err)
	}
}

func TestValidateBaseFeeCorrect(t *testing.T) {
	pp := NewPayloadProcessor()
	parentBaseFee := uint64(1_000_000_000) // 1 Gwei
	parentGasTarget := uint64(15_000_000)
	parentGasUsed := parentGasTarget // At target, no change.
	expectedBaseFee := CalcBaseFee(parentBaseFee, parentGasUsed, parentGasTarget)

	p := validPayload()
	p.BaseFeePerGas = new(big.Int).SetUint64(expectedBaseFee)
	if err := pp.ValidateBaseFee(p, parentBaseFee, parentGasUsed, parentGasTarget); err != nil {
		t.Errorf("expected valid base fee, got error: %v", err)
	}
}

func TestValidateBaseFeeIncorrect(t *testing.T) {
	pp := NewPayloadProcessor()
	parentBaseFee := uint64(1_000_000_000)
	parentGasTarget := uint64(15_000_000)
	parentGasUsed := uint64(20_000_000) // Above target -> fee increases.

	p := validPayload()
	p.BaseFeePerGas = big.NewInt(1) // Wrong.
	if err := pp.ValidateBaseFee(p, parentBaseFee, parentGasUsed, parentGasTarget); err == nil {
		t.Error("expected error for incorrect base fee")
	}
}

func TestValidateBaseFeeNilInPayload(t *testing.T) {
	pp := NewPayloadProcessor()
	p := validPayload()
	p.BaseFeePerGas = nil
	if err := pp.ValidateBaseFee(p, 100, 50, 50); err != ErrPPNilBaseFee {
		t.Errorf("expected ErrPPNilBaseFee, got %v", err)
	}
}

func TestCalcBaseFeeAtTarget(t *testing.T) {
	// When gas used equals gas target, base fee is unchanged.
	result := CalcBaseFee(1_000_000_000, 15_000_000, 15_000_000)
	if result != 1_000_000_000 {
		t.Errorf("expected 1_000_000_000 at target, got %d", result)
	}
}

func TestCalcBaseFeeAboveTarget(t *testing.T) {
	// When gas used > target, base fee should increase.
	parentBaseFee := uint64(1_000_000_000)
	gasTarget := uint64(15_000_000)
	gasUsed := uint64(30_000_000) // 2x target -> max increase.

	result := CalcBaseFee(parentBaseFee, gasUsed, gasTarget)
	if result <= parentBaseFee {
		t.Errorf("expected base fee to increase above %d, got %d", parentBaseFee, result)
	}

	// delta = 1_000_000_000 * 15_000_000 / 15_000_000 / 8 = 125_000_000.
	expected := parentBaseFee + 125_000_000
	if result != expected {
		t.Errorf("expected %d, got %d", expected, result)
	}
}

func TestCalcBaseFeeBelowTarget(t *testing.T) {
	// When gas used < target, base fee should decrease.
	parentBaseFee := uint64(1_000_000_000)
	gasTarget := uint64(15_000_000)
	gasUsed := uint64(0) // Empty block.

	result := CalcBaseFee(parentBaseFee, gasUsed, gasTarget)
	if result >= parentBaseFee {
		t.Errorf("expected base fee to decrease below %d, got %d", parentBaseFee, result)
	}

	// delta = 1_000_000_000 * 15_000_000 / 15_000_000 / 8 = 125_000_000.
	expected := parentBaseFee - 125_000_000
	if result != expected {
		t.Errorf("expected %d, got %d", expected, result)
	}
}

func TestCalcBaseFeeZeroTarget(t *testing.T) {
	// Zero target should return parent base fee unchanged.
	result := CalcBaseFee(100, 50, 0)
	if result != 100 {
		t.Errorf("expected 100 for zero target, got %d", result)
	}
}

func TestCalcBaseFeeMinimumIncrease(t *testing.T) {
	// Very small base fee should still increase by at least 1.
	result := CalcBaseFee(1, 20_000_000, 15_000_000)
	if result < 2 {
		t.Errorf("expected minimum increase to at least 2, got %d", result)
	}
}

func TestCalcBaseFeeFloorAtZero(t *testing.T) {
	// Very low base fee with empty block: delta = 1*15M/15M/8 = 0 (integer).
	// So base fee remains 1 - 0 = 1 (delta rounds to 0, no decrease).
	result := CalcBaseFee(1, 0, 15_000_000)
	if result > 1 {
		t.Errorf("expected at most 1 for very low base fee with empty block, got %d", result)
	}

	// Test actual floor: base fee 8, empty block -> delta = 8*15M/15M/8 = 1.
	// Result = 8 - 1 = 7.
	result2 := CalcBaseFee(8, 0, 15_000_000)
	if result2 != 7 {
		t.Errorf("expected 7, got %d", result2)
	}
}

func TestProcessPayloadNil(t *testing.T) {
	pp := NewPayloadProcessor()
	_, err := pp.ProcessPayload(nil)
	if err != ErrPPNilPayload {
		t.Errorf("expected ErrPPNilPayload, got %v", err)
	}
}

func TestProcessPayloadValid(t *testing.T) {
	pp := NewPayloadProcessor()
	p := validPayload()
	p.StateRoot = types.HexToHash("0xabcd")
	p.ReceiptsRoot = types.HexToHash("0x1234")

	result, err := pp.ProcessPayload(p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.StateRoot != p.StateRoot {
		t.Errorf("state root mismatch")
	}
	if result.ReceiptsRoot != p.ReceiptsRoot {
		t.Errorf("receipts root mismatch")
	}
	if result.GasUsed != p.GasUsed {
		t.Errorf("gas used mismatch: expected %d, got %d", p.GasUsed, result.GasUsed)
	}
}

func TestProcessPayloadInvalid(t *testing.T) {
	pp := NewPayloadProcessor()
	p := validPayload()
	p.GasUsed = p.GasLimit + 1 // Invalid.
	_, err := pp.ProcessPayload(p)
	if err == nil {
		t.Error("expected error for invalid payload")
	}
}

func TestCalcBaseFeeRealisticSequence(t *testing.T) {
	// Simulate a sequence of blocks to check base fee convergence.
	baseFee := uint64(1_000_000_000) // Start at 1 Gwei.
	target := uint64(15_000_000)

	// Full blocks should drive fee up.
	for i := 0; i < 10; i++ {
		baseFee = CalcBaseFee(baseFee, 30_000_000, target)
	}
	if baseFee <= 1_000_000_000 {
		t.Error("base fee should have increased after 10 full blocks")
	}

	// Empty blocks should drive fee down.
	highFee := baseFee
	for i := 0; i < 10; i++ {
		baseFee = CalcBaseFee(baseFee, 0, target)
	}
	if baseFee >= highFee {
		t.Error("base fee should have decreased after 10 empty blocks")
	}
}

func TestProcessResultFields(t *testing.T) {
	r := &ProcessResult{
		StateRoot:    types.HexToHash("0x01"),
		ReceiptsRoot: types.HexToHash("0x02"),
		GasUsed:      21000,
	}
	if r.StateRoot.IsZero() {
		t.Error("expected non-zero state root")
	}
	if r.GasUsed != 21000 {
		t.Errorf("expected gas used 21000, got %d", r.GasUsed)
	}
}
