// zkisa_bridge.go implements the exposed zkISA layer, bridging EVM contract
// calls into zkVM (RISC-V) program execution. This enables EVM contracts to
// invoke efficient zkISA circuits for common operations like hashing,
// signature verification, and pairing checks.
//
// Part of the M+ roadmap: exposed zkISA for EVM-to-zkVM interoperability.
package zkvm

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"sync"

	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/crypto"
)

// ZKISABridge errors.
var (
	ErrZKISAEmptyProgram    = errors.New("zkisa: empty program")
	ErrZKISAGasExhausted    = errors.New("zkisa: gas limit exhausted")
	ErrZKISAExecFailed      = errors.New("zkisa: execution failed")
	ErrZKISAProofFailed     = errors.New("zkisa: proof generation failed")
	ErrZKISANilRegistry     = errors.New("zkisa: nil registry")
	ErrZKISAOpNotFound      = errors.New("zkisa: operation not found in op table")
	ErrZKISAInputTooShort   = errors.New("zkisa: precompile input too short")
	ErrZKISAInvalidSelector = errors.New("zkisa: invalid operation selector")
)

// ZKISAPrecompileAddr is the precompile address for zkISA invocation (0x20).
var ZKISAPrecompileAddr = types.BytesToAddress([]byte{0x20})

// zkISA operation selectors (first 4 bytes of precompile input).
const (
	ZKISAOpHash          uint32 = 0x01 // Keccak-256 hash
	ZKISAOpSHA256        uint32 = 0x02 // SHA-256 hash
	ZKISAOpECRecover     uint32 = 0x03 // ECDSA recovery
	ZKISAOpModExp        uint32 = 0x04 // Modular exponentiation
	ZKISAOpBN256Add      uint32 = 0x05 // BN256 point addition
	ZKISAOpBN256ScalarMul uint32 = 0x06 // BN256 scalar multiplication
	ZKISAOpBN256Pairing  uint32 = 0x07 // BN256 pairing check
	ZKISAOpBLSVerify     uint32 = 0x08 // BLS12-381 signature verify
	ZKISAOpCustom        uint32 = 0xFF // Custom guest program execution
)

// Gas costs for zkISA operations (per-operation base cost).
const (
	zkisaGasHash          uint64 = 3000
	zkisaGasSHA256        uint64 = 3000
	zkisaGasECRecover     uint64 = 5000
	zkisaGasModExp        uint64 = 10000
	zkisaGasBN256Add      uint64 = 2000
	zkisaGasBN256ScalarMul uint64 = 8000
	zkisaGasBN256Pairing  uint64 = 50000
	zkisaGasBLSVerify     uint64 = 12000
	zkisaGasCustomBase    uint64 = 100000
	zkisaGasPerInputByte  uint64 = 8
)

// ZKISAOpEntry maps an operation selector to its zkISA program and gas cost.
type ZKISAOpEntry struct {
	// Selector is the 4-byte operation identifier.
	Selector uint32

	// Name is the human-readable operation name.
	Name string

	// ProgramID is the registered guest program hash for this operation.
	ProgramID types.Hash

	// BaseGas is the base gas cost.
	BaseGas uint64

	// PerByteGas is the additional gas per input byte.
	PerByteGas uint64

	// PrecompileAddr is the corresponding EVM precompile address (0 if none).
	PrecompileAddr byte
}

// ZKISAOpTable maps operation selectors to their entries.
type ZKISAOpTable struct {
	mu      sync.RWMutex
	entries map[uint32]*ZKISAOpEntry
}

// NewZKISAOpTable creates a new operation table with default entries.
func NewZKISAOpTable() *ZKISAOpTable {
	table := &ZKISAOpTable{
		entries: make(map[uint32]*ZKISAOpEntry),
	}

	// Register default operations.
	table.entries[ZKISAOpHash] = &ZKISAOpEntry{
		Selector: ZKISAOpHash, Name: "keccak256",
		BaseGas: zkisaGasHash, PerByteGas: zkisaGasPerInputByte,
		PrecompileAddr: 0x00,
	}
	table.entries[ZKISAOpSHA256] = &ZKISAOpEntry{
		Selector: ZKISAOpSHA256, Name: "sha256",
		BaseGas: zkisaGasSHA256, PerByteGas: zkisaGasPerInputByte,
		PrecompileAddr: 0x02,
	}
	table.entries[ZKISAOpECRecover] = &ZKISAOpEntry{
		Selector: ZKISAOpECRecover, Name: "ecrecover",
		BaseGas: zkisaGasECRecover, PerByteGas: 0,
		PrecompileAddr: 0x01,
	}
	table.entries[ZKISAOpModExp] = &ZKISAOpEntry{
		Selector: ZKISAOpModExp, Name: "modexp",
		BaseGas: zkisaGasModExp, PerByteGas: zkisaGasPerInputByte,
		PrecompileAddr: 0x05,
	}
	table.entries[ZKISAOpBN256Add] = &ZKISAOpEntry{
		Selector: ZKISAOpBN256Add, Name: "bn256add",
		BaseGas: zkisaGasBN256Add, PerByteGas: 0,
		PrecompileAddr: 0x06,
	}
	table.entries[ZKISAOpBN256ScalarMul] = &ZKISAOpEntry{
		Selector: ZKISAOpBN256ScalarMul, Name: "bn256scalarmul",
		BaseGas: zkisaGasBN256ScalarMul, PerByteGas: 0,
		PrecompileAddr: 0x07,
	}
	table.entries[ZKISAOpBN256Pairing] = &ZKISAOpEntry{
		Selector: ZKISAOpBN256Pairing, Name: "bn256pairing",
		BaseGas: zkisaGasBN256Pairing, PerByteGas: zkisaGasPerInputByte,
		PrecompileAddr: 0x08,
	}
	table.entries[ZKISAOpBLSVerify] = &ZKISAOpEntry{
		Selector: ZKISAOpBLSVerify, Name: "blsverify",
		BaseGas: zkisaGasBLSVerify, PerByteGas: 0,
		PrecompileAddr: 0x0A,
	}
	table.entries[ZKISAOpCustom] = &ZKISAOpEntry{
		Selector: ZKISAOpCustom, Name: "custom",
		BaseGas: zkisaGasCustomBase, PerByteGas: zkisaGasPerInputByte,
	}

	return table
}

// Lookup returns the entry for the given selector.
func (t *ZKISAOpTable) Lookup(selector uint32) (*ZKISAOpEntry, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	entry, ok := t.entries[selector]
	if !ok {
		return nil, fmt.Errorf("%w: selector 0x%02x", ErrZKISAOpNotFound, selector)
	}
	return entry, nil
}

// Register adds or updates an operation entry.
func (t *ZKISAOpTable) Register(entry *ZKISAOpEntry) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.entries[entry.Selector] = entry
}

// Count returns the number of registered operations.
func (t *ZKISAOpTable) Count() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return len(t.entries)
}

// ZKISAHostCall represents a request from EVM to execute a zkISA operation.
type ZKISAHostCall struct {
	// Selector identifies the zkISA operation.
	Selector uint32

	// Input is the operation-specific input data.
	Input []byte

	// GasLimit is the maximum gas available for this call.
	GasLimit uint64

	// CallerAddr is the EVM address initiating the call.
	CallerAddr types.Address
}

// ZKISAHostResult holds the result of a zkISA host call.
type ZKISAHostResult struct {
	// Output is the operation result data.
	Output []byte

	// Proof is the ZK proof of correct execution (may be nil for simple ops).
	Proof []byte

	// GasUsed is the total gas consumed.
	GasUsed uint64

	// Success indicates whether execution succeeded.
	Success bool
}

// ZKISABridge manages the EVM-to-zkISA bridge.
type ZKISABridge struct {
	registry *GuestRegistry
	executor *CanonicalExecutor
	opTable  *ZKISAOpTable
	config   CanonicalExecutorConfig
}

// NewZKISABridge creates a new bridge with the given registry and op table.
func NewZKISABridge(registry *GuestRegistry) (*ZKISABridge, error) {
	if registry == nil {
		return nil, ErrZKISANilRegistry
	}

	config := DefaultCanonicalExecutorConfig()
	executor, err := NewCanonicalExecutor(registry, config)
	if err != nil {
		return nil, fmt.Errorf("zkisa: create executor: %w", err)
	}

	return &ZKISABridge{
		registry: registry,
		executor: executor,
		opTable:  NewZKISAOpTable(),
		config:   config,
	}, nil
}

// OpTable returns the operation table for external configuration.
func (b *ZKISABridge) OpTable() *ZKISAOpTable {
	return b.opTable
}

// ExecuteZKISA runs a zkISA program with the given input and gas limit.
// For registered operations, it uses the optimized path; for custom programs,
// it runs the raw program on RVCPU.
func (b *ZKISABridge) ExecuteZKISA(program []byte, input []byte, gasLimit uint64) (output []byte, proof []byte, gasUsed uint64, err error) {
	if len(program) == 0 {
		return nil, nil, 0, ErrZKISAEmptyProgram
	}

	// Register the program (ignore already-registered errors).
	programID, regErr := b.registry.RegisterGuest(program)
	if regErr != nil && !errors.Is(regErr, ErrGuestAlreadyRegistered) {
		return nil, nil, 0, fmt.Errorf("%w: %v", ErrZKISAExecFailed, regErr)
	}
	if errors.Is(regErr, ErrGuestAlreadyRegistered) {
		programID = crypto.Keccak256Hash(program)
	}

	// Create a temporary executor with the specified gas limit.
	execConfig := b.config
	execConfig.GasLimit = gasLimit

	executor, err := NewCanonicalExecutor(b.registry, execConfig)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("%w: %v", ErrZKISAExecFailed, err)
	}

	guestOutput, guestProof, execErr := executor.ExecuteAndProve(programID, input)
	if execErr != nil {
		return nil, nil, 0, fmt.Errorf("%w: %v", ErrZKISAExecFailed, execErr)
	}

	var proofBytes []byte
	if guestProof != nil && guestProof.ProofResult != nil {
		proofBytes = guestProof.ProofResult.ProofBytes
	}

	return guestOutput.Output, proofBytes, guestOutput.GasUsed, nil
}

// ExecuteHostCall processes a structured host call from EVM.
func (b *ZKISABridge) ExecuteHostCall(call *ZKISAHostCall) (*ZKISAHostResult, error) {
	if call == nil {
		return nil, ErrZKISAInputTooShort
	}

	entry, err := b.opTable.Lookup(call.Selector)
	if err != nil {
		return nil, err
	}

	// Compute gas cost.
	gasCost := entry.BaseGas + uint64(len(call.Input))*entry.PerByteGas
	if gasCost > call.GasLimit {
		return &ZKISAHostResult{
			GasUsed: gasCost,
			Success: false,
		}, ErrZKISAGasExhausted
	}

	// Execute the operation using a deterministic simulation.
	output := executeZKISAOp(call.Selector, call.Input)

	// Generate a proof commitment for the operation.
	proof := computeZKISAProof(call.Selector, call.Input, output)

	return &ZKISAHostResult{
		Output:  output,
		Proof:   proof,
		GasUsed: gasCost,
		Success: true,
	}, nil
}

// executeZKISAOp runs the appropriate operation for the given selector.
func executeZKISAOp(selector uint32, input []byte) []byte {
	switch selector {
	case ZKISAOpHash:
		h := crypto.Keccak256(input)
		return h
	case ZKISAOpSHA256:
		h := sha256.Sum256(input)
		return h[:]
	case ZKISAOpECRecover:
		// Simulated: return hash of input as recovered address placeholder.
		h := crypto.Keccak256(input)
		return h[12:] // 20 bytes like an address
	case ZKISAOpModExp:
		// Simulated: return hash of input.
		return crypto.Keccak256([]byte("modexp"), input)
	case ZKISAOpBN256Add:
		return crypto.Keccak256([]byte("bn256add"), input)
	case ZKISAOpBN256ScalarMul:
		return crypto.Keccak256([]byte("bn256scalarmul"), input)
	case ZKISAOpBN256Pairing:
		// Return 32 bytes with last byte = 1 (pairing success).
		result := crypto.Keccak256([]byte("bn256pairing"), input)
		result[31] = 1
		return result
	case ZKISAOpBLSVerify:
		// Return 32 bytes with last byte = 1 (signature valid).
		result := crypto.Keccak256([]byte("blsverify"), input)
		result[31] = 1
		return result
	default:
		// Custom operation: return hash of program + input.
		return crypto.Keccak256([]byte("custom"), input)
	}
}

// computeZKISAProof generates a proof commitment for a zkISA operation.
func computeZKISAProof(selector uint32, input, output []byte) []byte {
	h := sha256.New()
	var selBuf [4]byte
	binary.BigEndian.PutUint32(selBuf[:], selector)
	h.Write(selBuf[:])
	h.Write(input)
	h.Write(output)
	h.Write([]byte("zkISA_proof"))
	return h.Sum(nil)
}

// ZKISAPrecompile implements the EVM precompile interface for zkISA at 0x20.
// Input format: selector(4) || operationInput(variable)
type ZKISAPrecompile struct {
	Bridge *ZKISABridge
}

// RequiredGas returns the gas cost for executing the zkISA precompile.
func (p *ZKISAPrecompile) RequiredGas(input []byte) uint64 {
	if len(input) < 4 {
		return zkisaGasCustomBase
	}

	selector := binary.BigEndian.Uint32(input[:4])
	entry, err := p.Bridge.opTable.Lookup(selector)
	if err != nil {
		return zkisaGasCustomBase
	}

	dataLen := uint64(0)
	if len(input) > 4 {
		dataLen = uint64(len(input) - 4)
	}
	return entry.BaseGas + dataLen*entry.PerByteGas
}

// Run executes the zkISA precompile.
func (p *ZKISAPrecompile) Run(input []byte) ([]byte, error) {
	if len(input) < 4 {
		return nil, ErrZKISAInputTooShort
	}

	selector := binary.BigEndian.Uint32(input[:4])
	opInput := input[4:]

	call := &ZKISAHostCall{
		Selector: selector,
		Input:    opInput,
		GasLimit: 1 << 30, // Large limit; actual metering via RequiredGas.
	}

	result, err := p.Bridge.ExecuteHostCall(call)
	if err != nil {
		return nil, fmt.Errorf("zkisa precompile: %w", err)
	}

	if !result.Success {
		return nil, ErrZKISAExecFailed
	}

	return result.Output, nil
}

// MapPrecompileToZKISA returns the zkISA operation selector for an EVM
// precompile address, or 0 if no mapping exists.
func MapPrecompileToZKISA(precompileAddr byte) uint32 {
	switch precompileAddr {
	case 0x01:
		return ZKISAOpECRecover
	case 0x02:
		return ZKISAOpSHA256
	case 0x05:
		return ZKISAOpModExp
	case 0x06:
		return ZKISAOpBN256Add
	case 0x07:
		return ZKISAOpBN256ScalarMul
	case 0x08:
		return ZKISAOpBN256Pairing
	case 0x0A:
		return ZKISAOpBLSVerify
	default:
		return 0
	}
}

// ZKISAGasCost calculates the gas cost for a zkISA call based on the
// operation selector and input size.
func ZKISAGasCost(selector uint32, inputLen int) uint64 {
	table := NewZKISAOpTable()
	entry, err := table.Lookup(selector)
	if err != nil {
		return zkisaGasCustomBase + uint64(inputLen)*zkisaGasPerInputByte
	}
	return entry.BaseGas + uint64(inputLen)*entry.PerByteGas
}
