// Package vm implements the Ethereum Virtual Machine.
//
// precompile_registry.go provides a dynamic registry for EVM precompiled
// contracts with fork-aware activation tracking and thread-safe operations.
package vm

import (
	"errors"
	"sort"
	"sync"

	"github.com/eth2030/eth2030/core/types"
)

// PrecompileInfo describes a precompiled contract's metadata and gas model.
type PrecompileInfo struct {
	Address        types.Address
	Name           string
	GasCost        func(input []byte) uint64
	MinInput       int
	MaxInput       int
	ActivationFork string
}

// PrecompileRegistry is a thread-safe registry of precompiled contracts with
// fork-based activation tracking.
type PrecompileRegistry struct {
	mu          sync.RWMutex
	precompiles map[types.Address]*PrecompileInfo
}

// NewPrecompileRegistry creates a new registry pre-populated with the
// standard Ethereum precompiles (0x01..0x0a) active since the Cancun fork.
func NewPrecompileRegistry() *PrecompileRegistry {
	r := &PrecompileRegistry{
		precompiles: make(map[types.Address]*PrecompileInfo),
	}
	r.registerDefaults()
	return r
}

// Register adds a precompile to the registry. Returns an error if the address
// is already occupied by another precompile.
func (r *PrecompileRegistry) Register(info PrecompileInfo) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.precompiles[info.Address]; exists {
		return errors.New("precompile registry: address already registered")
	}
	// Store a copy so callers cannot mutate internal state.
	stored := info
	r.precompiles[info.Address] = &stored
	return nil
}

// Lookup returns the PrecompileInfo for a given address, or false if not found.
func (r *PrecompileRegistry) Lookup(addr types.Address) (*PrecompileInfo, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	info, ok := r.precompiles[addr]
	if !ok {
		return nil, false
	}
	cp := *info
	return &cp, true
}

// IsPrecompile returns true if the address has a registered precompile.
func (r *PrecompileRegistry) IsPrecompile(addr types.Address) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.precompiles[addr]
	return ok
}

// ActivePrecompiles returns all precompiles whose ActivationFork matches
// the given fork name, sorted by address in ascending byte order.
func (r *PrecompileRegistry) ActivePrecompiles(fork string) []PrecompileInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var result []PrecompileInfo
	for _, info := range r.precompiles {
		if info.ActivationFork == fork {
			result = append(result, *info)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return addressLess(result[i].Address, result[j].Address)
	})
	return result
}

// GasCost computes the gas cost for invoking the precompile at addr with the
// given input. Returns an error if the address is not a registered precompile.
func (r *PrecompileRegistry) GasCost(addr types.Address, input []byte) (uint64, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	info, ok := r.precompiles[addr]
	if !ok {
		return 0, errors.New("precompile registry: address not found")
	}
	if info.GasCost == nil {
		return 0, nil
	}
	return info.GasCost(input), nil
}

// AllPrecompiles returns every registered precompile sorted by address in
// ascending byte order.
func (r *PrecompileRegistry) AllPrecompiles() []PrecompileInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]PrecompileInfo, 0, len(r.precompiles))
	for _, info := range r.precompiles {
		result = append(result, *info)
	}
	sort.Slice(result, func(i, j int) bool {
		return addressLess(result[i].Address, result[j].Address)
	})
	return result
}

// ForkPrecompiles groups all registered precompile addresses by their
// activation fork.
func (r *PrecompileRegistry) ForkPrecompiles() map[string][]types.Address {
	r.mu.RLock()
	defer r.mu.RUnlock()

	forks := make(map[string][]types.Address)
	for _, info := range r.precompiles {
		forks[info.ActivationFork] = append(forks[info.ActivationFork], info.Address)
	}
	// Sort addresses within each fork for deterministic output.
	for fork := range forks {
		addrs := forks[fork]
		sort.Slice(addrs, func(i, j int) bool {
			return addressLess(addrs[i], addrs[j])
		})
	}
	return forks
}

// Count returns the total number of registered precompiles.
func (r *PrecompileRegistry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.precompiles)
}

// addressLess returns true if a < b in byte-lexicographic order.
func addressLess(a, b types.Address) bool {
	for i := range a {
		if a[i] < b[i] {
			return true
		}
		if a[i] > b[i] {
			return false
		}
	}
	return false
}

// registerDefaults populates the registry with the 10 standard precompiles
// (0x01 through 0x0a) that are active from the Cancun fork.
func (r *PrecompileRegistry) registerDefaults() {
	defaults := []PrecompileInfo{
		{
			Address:        types.BytesToAddress([]byte{0x01}),
			Name:           "ecRecover",
			GasCost:        func([]byte) uint64 { return 3000 },
			MinInput:       0,
			MaxInput:       128,
			ActivationFork: "Homestead",
		},
		{
			Address: types.BytesToAddress([]byte{0x02}),
			Name:    "sha256",
			GasCost: func(input []byte) uint64 {
				return 60 + 12*wordCount(len(input))
			},
			MinInput:       0,
			MaxInput:       0, // no max
			ActivationFork: "Homestead",
		},
		{
			Address: types.BytesToAddress([]byte{0x03}),
			Name:    "ripemd160",
			GasCost: func(input []byte) uint64 {
				return 600 + 120*wordCount(len(input))
			},
			MinInput:       0,
			MaxInput:       0,
			ActivationFork: "Homestead",
		},
		{
			Address: types.BytesToAddress([]byte{0x04}),
			Name:    "identity",
			GasCost: func(input []byte) uint64 {
				return 15 + 3*wordCount(len(input))
			},
			MinInput:       0,
			MaxInput:       0,
			ActivationFork: "Homestead",
		},
		{
			Address: types.BytesToAddress([]byte{0x05}),
			Name:    "modexp",
			GasCost: func(input []byte) uint64 {
				c := &bigModExp{}
				return c.RequiredGas(input)
			},
			MinInput:       0,
			MaxInput:       0,
			ActivationFork: "Byzantium",
		},
		{
			Address:        types.BytesToAddress([]byte{0x06}),
			Name:           "ecAdd",
			GasCost:        func([]byte) uint64 { return 150 },
			MinInput:       0,
			MaxInput:       128,
			ActivationFork: "Byzantium",
		},
		{
			Address:        types.BytesToAddress([]byte{0x07}),
			Name:           "ecMul",
			GasCost:        func([]byte) uint64 { return 6000 },
			MinInput:       0,
			MaxInput:       96,
			ActivationFork: "Byzantium",
		},
		{
			Address: types.BytesToAddress([]byte{0x08}),
			Name:    "ecPairing",
			GasCost: func(input []byte) uint64 {
				k := uint64(len(input)) / 192
				return 45000 + 34000*k
			},
			MinInput:       0,
			MaxInput:       0,
			ActivationFork: "Byzantium",
		},
		{
			Address: types.BytesToAddress([]byte{0x09}),
			Name:    "blake2f",
			GasCost: func(input []byte) uint64 {
				c := &blake2F{}
				return c.RequiredGas(input)
			},
			MinInput:       213,
			MaxInput:       213,
			ActivationFork: "Istanbul",
		},
		{
			Address:        types.BytesToAddress([]byte{0x0a}),
			Name:           "pointEval",
			GasCost:        func([]byte) uint64 { return 50000 },
			MinInput:       192,
			MaxInput:       192,
			ActivationFork: "Cancun",
		},
	}
	for _, info := range defaults {
		stored := info
		r.precompiles[stored.Address] = &stored
	}
}
