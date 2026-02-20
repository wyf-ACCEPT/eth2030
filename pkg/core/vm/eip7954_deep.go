package vm

// eip7954_deep.go provides the full EIP-7954 implementation: contract code
// size limit enforcement with gas calculation, input validation helpers, and
// result formatting for CREATE/CREATE2 operations under Glamsterdam.

import (
	"errors"
	"fmt"
	"math"
)

// EIP-7954 validation errors.
var (
	ErrCodeSizeExceeded      = errors.New("eip7954: deployed code exceeds max code size")
	ErrInitCodeSizeExceeded  = errors.New("eip7954: init code exceeds max init code size")
	ErrCodeSizeZero          = errors.New("eip7954: deployed code is empty")
	ErrInitCodeEmpty         = errors.New("eip7954: init code is empty")
)

// CodeSizeLimits holds the code size limits for a specific fork.
type CodeSizeLimits struct {
	MaxCodeSize     int
	MaxInitCodeSize int
	ForkName        string
}

// CodeSizeLimitsForFork returns the code size limits for the given fork rules.
func CodeSizeLimitsForFork(rules ForkRules) CodeSizeLimits {
	if rules.IsEIP7954 {
		return CodeSizeLimits{
			MaxCodeSize:     MaxCodeSizeGlamsterdam,
			MaxInitCodeSize: MaxInitCodeSizeGlamsterdam,
			ForkName:        "Glamsterdam",
		}
	}
	return CodeSizeLimits{
		MaxCodeSize:     MaxCodeSize,
		MaxInitCodeSize: MaxInitCodeSize,
		ForkName:        "Pre-Glamsterdam",
	}
}

// ValidateInitCode checks that the init code does not exceed the maximum
// allowed size for the current fork rules.
func (l CodeSizeLimits) ValidateInitCode(code []byte) error {
	if len(code) == 0 {
		return ErrInitCodeEmpty
	}
	if len(code) > l.MaxInitCodeSize {
		return fmt.Errorf("%w: size %d exceeds limit %d (%s)",
			ErrInitCodeSizeExceeded, len(code), l.MaxInitCodeSize, l.ForkName)
	}
	return nil
}

// ValidateDeployedCode checks that the deployed code does not exceed the
// maximum allowed size for the current fork rules.
func (l CodeSizeLimits) ValidateDeployedCode(code []byte) error {
	if len(code) > l.MaxCodeSize {
		return fmt.Errorf("%w: size %d exceeds limit %d (%s)",
			ErrCodeSizeExceeded, len(code), l.MaxCodeSize, l.ForkName)
	}
	return nil
}

// InitCodeGas calculates the gas cost for init code per EIP-3860.
// Returns initCodeWordGas * ceil(len(code) / 32).
func InitCodeGas(code []byte) uint64 {
	if len(code) == 0 {
		return 0
	}
	words := (uint64(len(code)) + 31) / 32
	return InitCodeWordGas * words
}

// CodeDepositGas calculates the gas cost for deploying code.
// Pre-Glamsterdam: CreateDataGas (200) per byte.
// Post-Glamsterdam (EIP-8037): GasCodeDepositGlamsterdam (662) per byte.
func CodeDepositGas(codeLen int, rules ForkRules) uint64 {
	if codeLen <= 0 {
		return 0
	}
	perByte := uint64(CreateDataGas) // 200
	if rules.IsGlamsterdan {
		perByte = GasCodeDepositGlamsterdam // 662
	}
	// Safe multiply: guard against overflow.
	size := uint64(codeLen)
	if size > math.MaxUint64/perByte {
		return math.MaxUint64
	}
	return size * perByte
}

// CreateGasTotal computes the total gas cost for a CREATE/CREATE2 operation,
// including base creation gas, init code gas, and code deposit gas.
func CreateGasTotal(initCodeLen, deployedCodeLen int, rules ForkRules) uint64 {
	var total uint64

	// Base CREATE gas.
	if rules.IsGlamsterdan {
		total = GasCreateGlamsterdam // 83,144
	} else {
		total = GasCreate // 32,000
	}

	// Init code gas (EIP-3860).
	total = safeAdd(total, InitCodeGas(make([]byte, initCodeLen)))

	// Code deposit gas.
	total = safeAdd(total, CodeDepositGas(deployedCodeLen, rules))

	return total
}

// CodeSizeReport summarizes the code size validation result for a CREATE op.
type CodeSizeReport struct {
	InitCodeSize     int    `json:"initCodeSize"`
	DeployedCodeSize int    `json:"deployedCodeSize"`
	MaxCodeSize      int    `json:"maxCodeSize"`
	MaxInitCodeSize  int    `json:"maxInitCodeSize"`
	InitCodeGas      uint64 `json:"initCodeGas"`
	DepositGas       uint64 `json:"depositGas"`
	TotalCreateGas   uint64 `json:"totalCreateGas"`
	ForkName         string `json:"forkName"`
	Valid            bool   `json:"valid"`
	Error            string `json:"error,omitempty"`
}

// AnalyzeCodeSize produces a detailed report on code size compliance and
// gas costs for a CREATE operation.
func AnalyzeCodeSize(initCode, deployedCode []byte, rules ForkRules) *CodeSizeReport {
	limits := CodeSizeLimitsForFork(rules)
	report := &CodeSizeReport{
		InitCodeSize:     len(initCode),
		DeployedCodeSize: len(deployedCode),
		MaxCodeSize:      limits.MaxCodeSize,
		MaxInitCodeSize:  limits.MaxInitCodeSize,
		InitCodeGas:      InitCodeGas(initCode),
		DepositGas:       CodeDepositGas(len(deployedCode), rules),
		TotalCreateGas:   CreateGasTotal(len(initCode), len(deployedCode), rules),
		ForkName:         limits.ForkName,
		Valid:            true,
	}

	if err := limits.ValidateInitCode(initCode); err != nil {
		report.Valid = false
		report.Error = err.Error()
		return report
	}
	if err := limits.ValidateDeployedCode(deployedCode); err != nil {
		report.Valid = false
		report.Error = err.Error()
		return report
	}
	return report
}

// IsEIP7954Active returns true if EIP-7954 (increased code size limits)
// is active in the given fork rules.
func IsEIP7954Active(rules ForkRules) bool {
	return rules.IsEIP7954
}

// CodeSizeUtilization returns the ratio of code size to the maximum allowed
// code size (0.0 to 1.0+). Values > 1.0 indicate the code exceeds the limit.
func CodeSizeUtilization(codeLen int, rules ForkRules) float64 {
	limits := CodeSizeLimitsForFork(rules)
	if limits.MaxCodeSize == 0 {
		return 0
	}
	return float64(codeLen) / float64(limits.MaxCodeSize)
}

// InitCodeSizeUtilization returns the ratio of init code size to the maximum.
func InitCodeSizeUtilization(initCodeLen int, rules ForkRules) float64 {
	limits := CodeSizeLimitsForFork(rules)
	if limits.MaxInitCodeSize == 0 {
		return 0
	}
	return float64(initCodeLen) / float64(limits.MaxInitCodeSize)
}
