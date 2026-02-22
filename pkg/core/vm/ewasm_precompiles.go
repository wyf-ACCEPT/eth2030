// Package vm implements the Ethereum Virtual Machine.
//
// ewasm_precompiles.go provides a registry for migrating standard EVM
// precompiles (0x01-0x0a) to run in eWASM for extensibility. Part of the
// J+ roadmap: "more precompiles in eWASM".
package vm

import (
	"errors"
	"sort"
	"sync"

	"golang.org/x/crypto/sha3"
)

// Errors for eWASM precompile operations.
var (
	ErrEWASMPrecompileNotFound = errors.New("ewasm-precompile: address not registered")
	ErrEWASMExecutionFailed    = errors.New("ewasm-precompile: execution failed")
	ErrEWASMOutOfGas           = errors.New("ewasm-precompile: out of gas")
	ErrEWASMAlreadyRegistered  = errors.New("ewasm-precompile: address already registered")
	errEWASMInvalidAddress     = errors.New("ewasm-precompile: address must be 0x01-0x0a")
	errEWASMNilWasmCode        = errors.New("ewasm-precompile: nil wasm bytecode")
	errEWASMEmptyWasmCode      = errors.New("ewasm-precompile: empty wasm bytecode")
)

// EWASMPrecompile describes a precompile that has been migrated to run in
// eWASM rather than as native Go code. The WasmCode field holds the compiled
// WASM module bytecode, and GasCostFn computes the gas cost for a given input.
type EWASMPrecompile struct {
	Address   byte                      // precompile address (0x01-0x0a)
	Name      string                    // human-readable name (e.g., "ecRecover")
	WasmCode  []byte                    // WASM module bytecode
	GasCostFn func(input []byte) uint64 // gas cost calculator
}

// EWASMPrecompileRegistry manages eWASM-backed precompiles. It supports
// registration, execution via the JIT cache, gas calculation, and migration
// tracking. All methods are safe for concurrent use.
type EWASMPrecompileRegistry struct {
	mu          sync.RWMutex
	precompiles map[byte]*EWASMPrecompile
	migrated    map[byte]bool // tracks which native precompiles have been migrated
	cache       *JITCache     // JIT compilation cache for WASM modules
}

// NewEWASMPrecompileRegistry creates a new empty registry with a default
// JIT cache capacity of 32 modules.
func NewEWASMPrecompileRegistry() *EWASMPrecompileRegistry {
	return &EWASMPrecompileRegistry{
		precompiles: make(map[byte]*EWASMPrecompile),
		migrated:    make(map[byte]bool),
		cache:       NewJITCache(32),
	}
}

// isValidPrecompileAddr returns true if addr is in the standard range 0x01-0x0a.
func isValidPrecompileAddr(addr byte) bool {
	return addr >= 0x01 && addr <= 0x0a
}

// Register adds an eWASM precompile to the registry. Returns an error if the
// address is already registered or is outside the valid range.
func (r *EWASMPrecompileRegistry) Register(addr byte, precompile EWASMPrecompile) error {
	if !isValidPrecompileAddr(addr) {
		return errEWASMInvalidAddress
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.precompiles[addr]; exists {
		return ErrEWASMAlreadyRegistered
	}

	// Store a copy so callers cannot mutate internal state.
	stored := precompile
	stored.Address = addr
	stored.WasmCode = make([]byte, len(precompile.WasmCode))
	copy(stored.WasmCode, precompile.WasmCode)
	r.precompiles[addr] = &stored
	return nil
}

// Execute runs the eWASM precompile at the given address with the provided
// input and gas limit. Returns the output bytes, remaining gas, and any error.
func (r *EWASMPrecompileRegistry) Execute(addr byte, input []byte, gas uint64) ([]byte, uint64, error) {
	r.mu.RLock()
	p, exists := r.precompiles[addr]
	if !exists {
		r.mu.RUnlock()
		return nil, gas, ErrEWASMPrecompileNotFound
	}
	// Copy fields under lock to avoid races.
	wasmCode := make([]byte, len(p.WasmCode))
	copy(wasmCode, p.WasmCode)
	gasCostFn := p.GasCostFn
	r.mu.RUnlock()

	// Calculate gas cost.
	var gasCost uint64
	if gasCostFn != nil {
		gasCost = gasCostFn(input)
	}
	if gas < gasCost {
		return nil, 0, ErrEWASMOutOfGas
	}

	// Compile or retrieve cached WASM module.
	hash := wasmHash(wasmCode)
	module, ok := r.cache.GetCachedModule(hash)
	if !ok {
		var err error
		module, err = CompileModule(wasmCode)
		if err != nil {
			return nil, gas - gasCost, ErrEWASMExecutionFailed
		}
		r.cache.CacheModule(hash, module)
	}

	// Execute the "run" export with the input.
	result, err := ExecuteExport(module, "run", input)
	if err != nil {
		return nil, gas - gasCost, ErrEWASMExecutionFailed
	}

	return result, gas - gasCost, nil
}

// IsRegistered returns true if an eWASM precompile exists at the given address.
func (r *EWASMPrecompileRegistry) IsRegistered(addr byte) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, exists := r.precompiles[addr]
	return exists
}

// GasCost computes the gas cost for the eWASM precompile at addr with input.
// Returns 0 if the precompile is not registered or has no gas cost function.
func (r *EWASMPrecompileRegistry) GasCost(addr byte, input []byte) uint64 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, exists := r.precompiles[addr]
	if !exists || p.GasCostFn == nil {
		return 0
	}
	return p.GasCostFn(input)
}

// MigrateFromNative marks a native precompile address as eWASM-backed by
// registering it with the provided WASM bytecode. The WASM module must contain
// a "run" export for execution.
func (r *EWASMPrecompileRegistry) MigrateFromNative(addr byte, name string, wasmCode []byte) error {
	if !isValidPrecompileAddr(addr) {
		return errEWASMInvalidAddress
	}
	if wasmCode == nil {
		return errEWASMNilWasmCode
	}
	if len(wasmCode) == 0 {
		return errEWASMEmptyWasmCode
	}

	// Validate the WASM module.
	if err := ValidateWasmBytecode(wasmCode); err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.precompiles[addr]; exists {
		return ErrEWASMAlreadyRegistered
	}

	// Use a default gas cost based on input length, similar to native precompiles.
	gasFn := func(input []byte) uint64 {
		return 100 + 3*uint64(len(input))
	}

	p := &EWASMPrecompile{
		Address:   addr,
		Name:      name,
		WasmCode:  make([]byte, len(wasmCode)),
		GasCostFn: gasFn,
	}
	copy(p.WasmCode, wasmCode)
	r.precompiles[addr] = p
	r.migrated[addr] = true
	return nil
}

// ListMigrated returns a sorted list of precompile addresses that have been
// migrated from native Go to eWASM via MigrateFromNative.
func (r *EWASMPrecompileRegistry) ListMigrated() []byte {
	r.mu.RLock()
	defer r.mu.RUnlock()

	addrs := make([]byte, 0, len(r.migrated))
	for addr := range r.migrated {
		addrs = append(addrs, addr)
	}
	sort.Slice(addrs, func(i, j int) bool {
		return addrs[i] < addrs[j]
	})
	return addrs
}

// Count returns the total number of registered eWASM precompiles.
func (r *EWASMPrecompileRegistry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.precompiles)
}

// Get returns a copy of the eWASM precompile at addr, or nil if not registered.
func (r *EWASMPrecompileRegistry) Get(addr byte) *EWASMPrecompile {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, exists := r.precompiles[addr]
	if !exists {
		return nil
	}
	cp := *p
	cp.WasmCode = make([]byte, len(p.WasmCode))
	copy(cp.WasmCode, p.WasmCode)
	return &cp
}

// Unregister removes the eWASM precompile at addr. Returns an error if no
// precompile is registered at that address.
func (r *EWASMPrecompileRegistry) Unregister(addr byte) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.precompiles[addr]; !exists {
		return ErrEWASMPrecompileNotFound
	}
	delete(r.precompiles, addr)
	delete(r.migrated, addr)
	return nil
}

// IsMigrated returns true if the precompile at addr was migrated from native.
func (r *EWASMPrecompileRegistry) IsMigrated(addr byte) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.migrated[addr]
}

// ewasmPrecompileHash produces a deterministic hash for an eWASM precompile
// execution, combining the address, input, and WASM code hash.
func ewasmPrecompileHash(addr byte, input, wasmCodeHash []byte) []byte {
	h := sha3.NewLegacyKeccak256()
	h.Write([]byte{addr})
	h.Write(input)
	h.Write(wasmCodeHash)
	return h.Sum(nil)
}
