package zkvm

import (
	"errors"
	"fmt"
	"sync"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

// Canonical zkVM Guest Execution (K+ roadmap)
//
// Implements the canonical guest framework for zkVM execution. This enables
// mandatory proof-carrying blocks where the state transition is verified via
// a RISC-V zkVM guest program. The guest program executes the Ethereum STF
// inside a zkVM and produces a proof of correct execution.

// CanonicalGuestPrecompileAddr is the precompile address for guest execution.
// Assigned at 0x0200 in the K+ precompile range.
var CanonicalGuestPrecompileAddr = types.BytesToAddress([]byte{0x02, 0x00})

// Guest execution errors.
var (
	ErrGuestEmptyProgram     = errors.New("zkvm: empty guest program")
	ErrGuestCycleLimit       = errors.New("zkvm: cycle limit exceeded")
	ErrGuestMemoryLimit      = errors.New("zkvm: memory limit exceeded")
	ErrGuestNotRegistered    = errors.New("zkvm: guest program not registered")
	ErrGuestAlreadyRegistered = errors.New("zkvm: guest program already registered")
	ErrGuestInvalidProof     = errors.New("zkvm: guest proof verification failed")
	ErrGuestInputTooShort    = errors.New("zkvm: precompile input too short")
)

// Default configuration values.
const (
	DefaultMaxCycles   = 1 << 24 // ~16M cycles
	DefaultMemoryLimit = 1 << 28 // 256 MiB
	DefaultProofSystem = "stark"

	// Gas costs for the canonical guest precompile.
	GuestPrecompileBaseGas   = 100_000
	GuestPrecompilePerCycleGas = 1 // 1 gas per cycle
)

// CanonicalGuestConfig holds configuration for guest execution.
type CanonicalGuestConfig struct {
	MaxCycles   uint64
	MemoryLimit uint64
	ProofSystem string
}

// DefaultGuestConfig returns the default configuration.
func DefaultGuestConfig() CanonicalGuestConfig {
	return CanonicalGuestConfig{
		MaxCycles:   DefaultMaxCycles,
		MemoryLimit: DefaultMemoryLimit,
		ProofSystem: DefaultProofSystem,
	}
}

// RiscVGuest represents a RISC-V guest program ready for execution.
type RiscVGuest struct {
	program []byte
	input   []byte
	config  CanonicalGuestConfig
}

// GuestExecution holds the result of a guest program execution.
type GuestExecution struct {
	Output    []byte
	Cycles    uint64
	ProofData []byte
	Success   bool
}

// NewRiscVGuest creates a new RISC-V guest with the given program, input, and config.
func NewRiscVGuest(program, input []byte, config CanonicalGuestConfig) *RiscVGuest {
	return &RiscVGuest{
		program: program,
		input:   input,
		config:  config,
	}
}

// Execute runs the guest program and returns the execution result.
// This is a stub that simulates RISC-V execution by computing a
// deterministic output from the program and input.
func (g *RiscVGuest) Execute() (*GuestExecution, error) {
	if len(g.program) == 0 {
		return nil, ErrGuestEmptyProgram
	}

	// Simulate cycle count based on program + input size.
	cycles := uint64(len(g.program)) + uint64(len(g.input))
	if cycles > g.config.MaxCycles {
		return &GuestExecution{
			Cycles:  cycles,
			Success: false,
		}, ErrGuestCycleLimit
	}

	// Simulate memory usage.
	memUsed := uint64(len(g.program)) * 4
	if memUsed > g.config.MemoryLimit {
		return &GuestExecution{
			Cycles:  cycles,
			Success: false,
		}, ErrGuestMemoryLimit
	}

	// Compute deterministic output: H(program || input).
	output := crypto.Keccak256(g.program, g.input)

	// Compute stub proof data: H("proof" || program || input).
	proofData := crypto.Keccak256([]byte("proof"), g.program, g.input)

	return &GuestExecution{
		Output:    output,
		Cycles:    cycles,
		ProofData: proofData,
		Success:   true,
	}, nil
}

// VerifyExecution verifies a guest execution result.
// In production, this would verify the STARK/SNARK proof. The stub
// verifies that the output matches the expected deterministic hash.
func VerifyExecution(execution *GuestExecution, program, input []byte) bool {
	if execution == nil || !execution.Success {
		return false
	}
	if len(execution.ProofData) == 0 {
		return false
	}

	// Recompute expected output.
	expectedOutput := crypto.Keccak256(program, input)
	if len(execution.Output) != len(expectedOutput) {
		return false
	}
	for i := range expectedOutput {
		if execution.Output[i] != expectedOutput[i] {
			return false
		}
	}

	// Recompute expected proof.
	expectedProof := crypto.Keccak256([]byte("proof"), program, input)
	if len(execution.ProofData) != len(expectedProof) {
		return false
	}
	for i := range expectedProof {
		if execution.ProofData[i] != expectedProof[i] {
			return false
		}
	}

	return true
}

// GuestRegistry manages registered guest programs by their program hash.
type GuestRegistry struct {
	mu       sync.RWMutex
	programs map[types.Hash][]byte // programHash -> program bytes
}

// NewGuestRegistry creates a new empty guest registry.
func NewGuestRegistry() *GuestRegistry {
	return &GuestRegistry{
		programs: make(map[types.Hash][]byte),
	}
}

// RegisterGuest registers a guest program by its hash.
func (r *GuestRegistry) RegisterGuest(program []byte) (types.Hash, error) {
	if len(program) == 0 {
		return types.Hash{}, ErrGuestEmptyProgram
	}

	h := crypto.Keccak256Hash(program)

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.programs[h]; exists {
		return h, ErrGuestAlreadyRegistered
	}

	// Store a copy.
	stored := make([]byte, len(program))
	copy(stored, program)
	r.programs[h] = stored

	return h, nil
}

// GetGuest retrieves a registered guest program by hash.
func (r *GuestRegistry) GetGuest(programHash types.Hash) ([]byte, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	program, exists := r.programs[programHash]
	if !exists {
		return nil, ErrGuestNotRegistered
	}

	// Return a copy.
	result := make([]byte, len(program))
	copy(result, program)
	return result, nil
}

// Count returns the number of registered guest programs.
func (r *GuestRegistry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.programs)
}

// CanonicalGuestPrecompile implements the precompile interface for canonical
// guest invocation at address 0x0200.
//
// Input format: programHash(32) || input(variable)
// Output: execution output bytes
type CanonicalGuestPrecompile struct {
	Registry *GuestRegistry
	Config   CanonicalGuestConfig
}

// RequiredGas returns the gas cost for running the canonical guest precompile.
func (p *CanonicalGuestPrecompile) RequiredGas(input []byte) uint64 {
	if len(input) < 32 {
		return GuestPrecompileBaseGas
	}
	// Base gas + per-byte cost for input.
	inputLen := uint64(len(input) - 32)
	return GuestPrecompileBaseGas + inputLen*GuestPrecompilePerCycleGas
}

// Run executes the canonical guest precompile.
func (p *CanonicalGuestPrecompile) Run(input []byte) ([]byte, error) {
	if len(input) < 32 {
		return nil, ErrGuestInputTooShort
	}

	// Parse program hash from first 32 bytes.
	var programHash types.Hash
	copy(programHash[:], input[:32])

	// Look up registered program.
	program, err := p.Registry.GetGuest(programHash)
	if err != nil {
		return nil, fmt.Errorf("canonical guest: %w", err)
	}

	// Execute the guest.
	guestInput := input[32:]
	guest := NewRiscVGuest(program, guestInput, p.Config)
	execution, err := guest.Execute()
	if err != nil {
		return nil, fmt.Errorf("canonical guest execution: %w", err)
	}

	if !execution.Success {
		return nil, errors.New("canonical guest: execution failed")
	}

	return execution.Output, nil
}
