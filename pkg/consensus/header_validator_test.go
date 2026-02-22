package consensus

import (
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

// makeTestHeader creates a header with the given parameters for testing.
func makeTestHeader(parentHash types.Hash, number uint64, time uint64, gasLimit uint64, gasUsed uint64, extra []byte) *types.Header {
	return &types.Header{
		ParentHash: parentHash,
		Number:     new(big.Int).SetUint64(number),
		Time:       time,
		GasLimit:   gasLimit,
		GasUsed:    gasUsed,
		Extra:      extra,
		Difficulty: new(big.Int),
	}
}

func TestValidateHeader_Valid(t *testing.T) {
	parent := makeTestHeader(types.Hash{}, 100, 1000, 30_000_000, 15_000_000, nil)
	parentHash := parent.Hash()
	child := makeTestHeader(parentHash, 101, 1012, 30_000_000, 20_000_000, nil)

	hv := NewHeaderValidator()
	if err := hv.ValidateHeader(child, parent); err != nil {
		t.Fatalf("expected valid header, got error: %v", err)
	}
}

func TestValidateHeader_NilHeader(t *testing.T) {
	parent := makeTestHeader(types.Hash{}, 100, 1000, 30_000_000, 0, nil)
	hv := NewHeaderValidator()

	if err := hv.ValidateHeader(nil, parent); err != ErrNilHeader {
		t.Fatalf("expected ErrNilHeader, got %v", err)
	}
}

func TestValidateHeader_NilParent(t *testing.T) {
	child := makeTestHeader(types.Hash{}, 101, 1012, 30_000_000, 0, nil)
	hv := NewHeaderValidator()

	if err := hv.ValidateHeader(child, nil); err != ErrNilParent {
		t.Fatalf("expected ErrNilParent, got %v", err)
	}
}

func TestValidateHeader_ParentHashMismatch(t *testing.T) {
	parent := makeTestHeader(types.Hash{}, 100, 1000, 30_000_000, 0, nil)
	wrongHash := types.Hash{0x01}
	child := makeTestHeader(wrongHash, 101, 1012, 30_000_000, 0, nil)

	hv := NewHeaderValidator()
	if err := hv.ValidateHeader(child, parent); err != ErrInvalidParentHash {
		t.Fatalf("expected ErrInvalidParentHash, got %v", err)
	}
}

func TestValidateHeader_InvalidNumber(t *testing.T) {
	parent := makeTestHeader(types.Hash{}, 100, 1000, 30_000_000, 0, nil)
	parentHash := parent.Hash()
	child := makeTestHeader(parentHash, 103, 1012, 30_000_000, 0, nil) // should be 101

	hv := NewHeaderValidator()
	if err := hv.ValidateHeader(child, parent); err != ErrInvalidNumber {
		t.Fatalf("expected ErrInvalidNumber, got %v", err)
	}
}

func TestValidateHeader_InvalidTimestamp(t *testing.T) {
	parent := makeTestHeader(types.Hash{}, 100, 1000, 30_000_000, 0, nil)
	parentHash := parent.Hash()
	child := makeTestHeader(parentHash, 101, 999, 30_000_000, 0, nil) // timestamp before parent

	hv := NewHeaderValidator()
	if err := hv.ValidateHeader(child, parent); err != ErrInvalidTimestamp {
		t.Fatalf("expected ErrInvalidTimestamp, got %v", err)
	}
}

func TestValidateHeader_TimestampEqual(t *testing.T) {
	parent := makeTestHeader(types.Hash{}, 100, 1000, 30_000_000, 0, nil)
	parentHash := parent.Hash()
	child := makeTestHeader(parentHash, 101, 1000, 30_000_000, 0, nil) // equal timestamp

	hv := NewHeaderValidator()
	if err := hv.ValidateHeader(child, parent); err != ErrInvalidTimestamp {
		t.Fatalf("expected ErrInvalidTimestamp for equal timestamps, got %v", err)
	}
}

func TestValidateHeader_GasLimitTooHigh(t *testing.T) {
	parentGas := uint64(30_000_000)
	parent := makeTestHeader(types.Hash{}, 100, 1000, parentGas, 0, nil)
	parentHash := parent.Hash()
	// Increase gas limit by more than parentGas/1024
	childGas := parentGas + parentGas/GasLimitBoundDivisor + 1
	child := makeTestHeader(parentHash, 101, 1012, childGas, 0, nil)

	hv := NewHeaderValidator()
	if err := hv.ValidateHeader(child, parent); err != ErrInvalidGasLimit {
		t.Fatalf("expected ErrInvalidGasLimit, got %v", err)
	}
}

func TestValidateHeader_GasLimitTooLow(t *testing.T) {
	parentGas := uint64(30_000_000)
	parent := makeTestHeader(types.Hash{}, 100, 1000, parentGas, 0, nil)
	parentHash := parent.Hash()
	// Decrease gas limit by more than parentGas/1024
	childGas := parentGas - parentGas/GasLimitBoundDivisor - 1
	child := makeTestHeader(parentHash, 101, 1012, childGas, 0, nil)

	hv := NewHeaderValidator()
	if err := hv.ValidateHeader(child, parent); err != ErrInvalidGasLimit {
		t.Fatalf("expected ErrInvalidGasLimit, got %v", err)
	}
}

func TestValidateHeader_GasUsedExceedsLimit(t *testing.T) {
	parent := makeTestHeader(types.Hash{}, 100, 1000, 30_000_000, 0, nil)
	parentHash := parent.Hash()
	child := makeTestHeader(parentHash, 101, 1012, 30_000_000, 30_000_001, nil)

	hv := NewHeaderValidator()
	if err := hv.ValidateHeader(child, parent); err != ErrGasUsedExceedsLimit {
		t.Fatalf("expected ErrGasUsedExceedsLimit, got %v", err)
	}
}

func TestValidateHeader_ExtraDataTooLong(t *testing.T) {
	parent := makeTestHeader(types.Hash{}, 100, 1000, 30_000_000, 0, nil)
	parentHash := parent.Hash()
	longExtra := make([]byte, MaxExtraDataBytes+1)
	child := makeTestHeader(parentHash, 101, 1012, 30_000_000, 0, longExtra)

	hv := NewHeaderValidator()
	if err := hv.ValidateHeader(child, parent); err != ErrExtraDataTooLong {
		t.Fatalf("expected ErrExtraDataTooLong, got %v", err)
	}
}

func TestValidateHeader_ExtraDataExact32(t *testing.T) {
	parent := makeTestHeader(types.Hash{}, 100, 1000, 30_000_000, 0, nil)
	parentHash := parent.Hash()
	exact32 := make([]byte, MaxExtraDataBytes)
	child := makeTestHeader(parentHash, 101, 1012, 30_000_000, 0, exact32)

	hv := NewHeaderValidator()
	if err := hv.ValidateHeader(child, parent); err != nil {
		t.Fatalf("expected valid header with 32 bytes extra data, got error: %v", err)
	}
}

func TestValidateGasLimit(t *testing.T) {
	tests := []struct {
		name     string
		parent   uint64
		child    uint64
		expected bool
	}{
		{"same limit", 30_000_000, 30_000_000, true},
		{"small increase", 30_000_000, 30_000_010, true},
		{"small decrease", 30_000_000, 29_999_990, true},
		{"max valid increase", 30_000_000, 30_000_000 + 30_000_000/1024 - 1, true},
		{"max valid decrease", 30_000_000, 30_000_000 - 30_000_000/1024 + 1, true},
		{"exact bound increase", 30_000_000, 30_000_000 + 30_000_000/1024, false},
		{"exact bound decrease", 30_000_000, 30_000_000 - 30_000_000/1024, false},
		{"zero child", 30_000_000, 0, false},
		{"small parent", 1024, 1024, true},
		{"small parent increase by 1", 1024, 1025, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := ValidateGasLimit(tc.parent, tc.child)
			if result != tc.expected {
				t.Errorf("ValidateGasLimit(%d, %d) = %v, want %v", tc.parent, tc.child, result, tc.expected)
			}
		})
	}
}

func TestValidateTimestamp(t *testing.T) {
	tests := []struct {
		name     string
		parent   uint64
		child    uint64
		expected bool
	}{
		{"child after parent", 1000, 1001, true},
		{"child equal parent", 1000, 1000, false},
		{"child before parent", 1000, 999, false},
		{"zero parent", 0, 1, true},
		{"both zero", 0, 0, false},
		{"large gap", 1000, 2000, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := ValidateTimestamp(tc.parent, tc.child)
			if result != tc.expected {
				t.Errorf("ValidateTimestamp(%d, %d) = %v, want %v", tc.parent, tc.child, result, tc.expected)
			}
		})
	}
}

func TestCalcDifficulty(t *testing.T) {
	// Post-merge difficulty is always 0 regardless of inputs.
	tests := []struct {
		name            string
		parentDiff      *big.Int
		parentTimestamp uint64
		currentTime     uint64
	}{
		{"zero parent difficulty", big.NewInt(0), 1000, 1012},
		{"large parent difficulty", big.NewInt(1_000_000), 1000, 1012},
		{"nil parent difficulty", nil, 1000, 1012},
		{"same timestamp", big.NewInt(500), 1000, 1000},
		{"large time gap", big.NewInt(500), 1000, 2000},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := CalcDifficulty(tc.parentDiff, tc.parentTimestamp, tc.currentTime)
			if result == nil {
				t.Fatal("CalcDifficulty returned nil")
			}
			if result.Sign() != 0 {
				t.Errorf("expected difficulty 0, got %s", result.String())
			}
		})
	}
}

func TestValidateHeader_NilBlockNumber(t *testing.T) {
	parent := makeTestHeader(types.Hash{}, 100, 1000, 30_000_000, 0, nil)
	parentHash := parent.Hash()

	child := &types.Header{
		ParentHash: parentHash,
		Number:     nil,
		Time:       1012,
		GasLimit:   30_000_000,
		Difficulty: new(big.Int),
	}

	hv := NewHeaderValidator()
	if err := hv.ValidateHeader(child, parent); err != ErrInvalidNumber {
		t.Fatalf("expected ErrInvalidNumber for nil block number, got %v", err)
	}
}

func TestValidateGasLimit_VerySmallParent(t *testing.T) {
	// When parent gas limit is very small (< 1024), the bound should be at least 1.
	if !ValidateGasLimit(10, 10) {
		t.Error("expected same gas limit to be valid even with small parent")
	}
	// With parent=10, bound = max(10/1024, 1) = 1, so diff < 1 means diff must be 0.
	if ValidateGasLimit(10, 11) {
		t.Error("expected diff of 1 to be invalid when parent is 10 (bound=1)")
	}
}
