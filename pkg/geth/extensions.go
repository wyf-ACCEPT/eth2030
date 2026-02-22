// Package geth provides adapters between eth2030 and go-ethereum.
//
// extensions.go wires eth2030's custom precompiles into go-ethereum's EVM
// via the SetPrecompiles API. It also maps eth2030 fork rules (Glamsterdam,
// Hogota) to go-ethereum chain config parameters.
//
// Custom opcode injection is NOT possible from an external package because
// go-ethereum's `operation` struct and `JumpTable` are unexported. Custom
// opcodes (CLZ, DUPN/SWAPN/EXCHANGE, APPROVE, TXPARAM*, EOF, AA) remain
// available only through eth2030's native EVM interpreter.
package geth

import (
	gethcommon "github.com/ethereum/go-ethereum/common"
	gethvm "github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/params"

	"github.com/eth2030/eth2030/core"
	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/core/vm"
)

// PrecompileAdapter wraps an eth2030 PrecompiledContract to satisfy
// go-ethereum's PrecompiledContract interface (which adds Name()).
type PrecompileAdapter struct {
	inner vm.PrecompiledContract
	name  string
}

// RequiredGas delegates to the wrapped eth2030 precompile.
func (a *PrecompileAdapter) RequiredGas(input []byte) uint64 {
	return a.inner.RequiredGas(input)
}

// Run delegates to the wrapped eth2030 precompile.
func (a *PrecompileAdapter) Run(input []byte) ([]byte, error) {
	return a.inner.Run(input)
}

// Name returns the human-readable name for this precompile.
func (a *PrecompileAdapter) Name() string {
	return a.name
}

// NewPrecompileAdapter wraps an eth2030 precompile for use with go-ethereum.
func NewPrecompileAdapter(inner vm.PrecompiledContract, name string) gethvm.PrecompiledContract {
	return &PrecompileAdapter{inner: inner, name: name}
}

// Eth2028ForkLevel identifies which eth2030 fork is active.
type Eth2028ForkLevel int

const (
	ForkLevelPrague     Eth2028ForkLevel = 0
	ForkLevelGlamsterdam Eth2028ForkLevel = 1
	ForkLevelHogota     Eth2028ForkLevel = 2
	ForkLevelIPlus      Eth2028ForkLevel = 3
)

// customPrecompileEntry defines a custom precompile to inject.
type customPrecompileEntry struct {
	addr     gethcommon.Address
	name     string
	contract vm.PrecompiledContract
	minFork  Eth2028ForkLevel
}

// customPrecompiles returns the list of eth2030 custom precompiles with their
// activation forks. These are precompiles that go-ethereum does not include.
func customPrecompiles() []customPrecompileEntry {
	return []customPrecompileEntry{
		// Glamsterdam: repriced gas for existing precompiles.
		// These replace go-ethereum's default precompiles at the same addresses.
		{
			addr:     gethcommon.BytesToAddress([]byte{0x06}),
			name:     "ecAddGlamsterdam",
			contract: &vm.BN256AddGlamsterdanAdapter{},
			minFork:  ForkLevelGlamsterdam,
		},
		{
			addr:     gethcommon.BytesToAddress([]byte{0x08}),
			name:     "ecPairingGlamsterdam",
			contract: &vm.BN256PairingGlamsterdanAdapter{},
			minFork:  ForkLevelGlamsterdam,
		},
		{
			addr:     gethcommon.BytesToAddress([]byte{0x09}),
			name:     "blake2fGlamsterdam",
			contract: &vm.Blake2FGlamsterdanAdapter{},
			minFork:  ForkLevelGlamsterdam,
		},
		{
			addr:     gethcommon.BytesToAddress([]byte{0x0a}),
			name:     "pointEvalGlamsterdam",
			contract: &vm.KZGPointEvalGlamsterdanAdapter{},
			minFork:  ForkLevelGlamsterdam,
		},

		// I+ fork: NTT precompile.
		{
			addr:     gethcommon.BytesToAddress([]byte{0x15}),
			name:     "ntt",
			contract: &vm.NTTPrecompileAdapter{},
			minFork:  ForkLevelIPlus,
		},

		// J+ fork: NII (Number-Theoretic) precompiles.
		{
			addr:     gethcommon.BytesToAddress([]byte{0x02, 0x01}),
			name:     "niiModExp",
			contract: &vm.NiiModExpAdapter{},
			minFork:  ForkLevelIPlus,
		},
		{
			addr:     gethcommon.BytesToAddress([]byte{0x02, 0x02}),
			name:     "niiFieldMul",
			contract: &vm.NiiFieldMulAdapter{},
			minFork:  ForkLevelIPlus,
		},
		{
			addr:     gethcommon.BytesToAddress([]byte{0x02, 0x03}),
			name:     "niiFieldInv",
			contract: &vm.NiiFieldInvAdapter{},
			minFork:  ForkLevelIPlus,
		},
		{
			addr:     gethcommon.BytesToAddress([]byte{0x02, 0x04}),
			name:     "niiBatchVerify",
			contract: &vm.NiiBatchVerifyAdapter{},
			minFork:  ForkLevelIPlus,
		},

		// J+ fork: Extended field precompiles.
		{
			addr:     gethcommon.BytesToAddress([]byte{0x02, 0x05}),
			name:     "fieldMulExt",
			contract: &vm.FieldMulExtAdapter{},
			minFork:  ForkLevelIPlus,
		},
		{
			addr:     gethcommon.BytesToAddress([]byte{0x02, 0x06}),
			name:     "fieldInvExt",
			contract: &vm.FieldInvExtAdapter{},
			minFork:  ForkLevelIPlus,
		},
		{
			addr:     gethcommon.BytesToAddress([]byte{0x02, 0x07}),
			name:     "fieldExp",
			contract: &vm.FieldExpAdapter{},
			minFork:  ForkLevelIPlus,
		},
		{
			addr:     gethcommon.BytesToAddress([]byte{0x02, 0x08}),
			name:     "batchFieldVerify",
			contract: &vm.BatchFieldVerifyAdapter{},
			minFork:  ForkLevelIPlus,
		},
	}
}

// InjectCustomPrecompiles builds a go-ethereum precompile map that includes
// both go-ethereum's standard precompiles (for the given fork rules) and
// eth2030's custom precompiles (activated at the appropriate fork level).
func InjectCustomPrecompiles(rules params.Rules, forkLevel Eth2028ForkLevel) gethvm.PrecompiledContracts {
	// Start with go-ethereum's standard precompiles for this fork.
	precompiles := gethvm.ActivePrecompiledContracts(rules)

	// Add eth2030 custom precompiles that are active at this fork level.
	for _, entry := range customPrecompiles() {
		if forkLevel >= entry.minFork {
			precompiles[entry.addr] = NewPrecompileAdapter(entry.contract, entry.name)
		}
	}

	return precompiles
}

// Eth2028ForkLevelFromConfig determines the active eth2030 fork level based
// on the eth2030 chain config and block time.
func Eth2028ForkLevelFromConfig(config *core.ChainConfig, time uint64) Eth2028ForkLevel {
	if config == nil {
		return ForkLevelPrague
	}
	if config.IsHogota(time) {
		return ForkLevelHogota
	}
	if config.IsGlamsterdan(time) {
		return ForkLevelGlamsterdam
	}
	return ForkLevelPrague
}

// ToGethChainConfigWithEth2028Forks converts an eth2030 ChainConfig to a
// go-ethereum ChainConfig, mapping eth2030's custom fork timestamps.
// Glamsterdam and Hogota are mapped to Prague since they extend Prague.
func ToGethChainConfigWithEth2028Forks(c *core.ChainConfig) *params.ChainConfig {
	gc := ToGethChainConfig(c)
	if gc == nil {
		return nil
	}

	// Ensure Prague is enabled if Glamsterdam or Hogota are active,
	// since they are post-Prague forks.
	if gc.PragueTime == nil {
		if c.GlamsterdanTime != nil {
			gc.PragueTime = c.GlamsterdanTime
		}
		if c.HogotaTime != nil && gc.PragueTime == nil {
			gc.PragueTime = c.HogotaTime
		}
	}

	return gc
}

// CustomPrecompileAddresses returns the list of eth2030 custom precompile
// addresses active at the given fork level. This is used for EIP-2929 access
// list warming — all precompile addresses must be pre-warmed.
func CustomPrecompileAddresses(forkLevel Eth2028ForkLevel) []gethcommon.Address {
	var addrs []gethcommon.Address
	for _, entry := range customPrecompiles() {
		if forkLevel >= entry.minFork {
			addrs = append(addrs, entry.addr)
		}
	}
	return addrs
}

// CustomPrecompileCount returns the number of custom precompiles active at
// the given fork level.
func CustomPrecompileCount(forkLevel Eth2028ForkLevel) int {
	count := 0
	for _, entry := range customPrecompiles() {
		if forkLevel >= entry.minFork {
			count++
		}
	}
	return count
}

// OpcodeExtensionNote documents the opcode extensibility limitation.
//
// go-ethereum's JumpTable type and operation struct are unexported (lowercase),
// which means custom opcodes cannot be added from external packages. The
// following eth2030 custom opcodes are only available through eth2030's
// native EVM interpreter (pkg/core/vm/):
//
//   - CLZ (0x1e): Count leading zeros
//   - SLOTNUM (0x4b): Current slot number
//   - DUPN (0xe6), SWAPN (0xe7), EXCHANGE (0xe8): Stack manipulation
//   - APPROVE (0xaa): EIP-8141 frame transaction approval
//   - TXPARAMLOAD (0xb0), TXPARAMSIZE (0xb1), TXPARAMCOPY (0xb2): Frame params
//   - CURRENT_ROLE (0xab), ACCEPT_ROLE (0xac): EIP-7701 AA roles
//   - EOF opcodes: DATALOAD (0xd0), DATALOADN (0xd1), DATASIZE (0xd2),
//     DATACOPY (0xd3), RJUMP (0xe0), RJUMPI (0xe1), RJUMPV (0xe2),
//     CALLF (0xe3), RETF (0xe4), JUMPF (0xe5), EOFCREATE (0xec),
//     RETURNCONTRACT (0xee), EXTCALL (0xf7), EXTDELEGATECALL (0xf8),
//     EXTSTATICCALL (0xf9), RETURNDATALOAD (0xfb)
//
// When using the go-ethereum execution path, transactions that invoke these
// opcodes will be handled by go-ethereum's standard opcode set. For eth2030-
// specific opcodes not in go-ethereum, the eth2030 native EVM must be used.
const OpcodeExtensionNote = "go-ethereum operation struct is unexported; custom opcodes require eth2030 native EVM"

// PrecompileNames returns a mapping of precompile address to name for all
// custom precompiles active at the given fork level.
func PrecompileNames(forkLevel Eth2028ForkLevel) map[gethcommon.Address]string {
	names := make(map[gethcommon.Address]string)
	for _, entry := range customPrecompiles() {
		if forkLevel >= entry.minFork {
			names[entry.addr] = entry.name
		}
	}
	return names
}

// Eth2028PrecompileInfo holds metadata about an eth2030 custom precompile.
type Eth2028PrecompileInfo struct {
	Address  gethcommon.Address
	Name     string
	MinFork  Eth2028ForkLevel
	Category string // "repricing", "ntt", "nii", "field"
}

// ListCustomPrecompiles returns metadata about all eth2030 custom precompiles.
func ListCustomPrecompiles() []Eth2028PrecompileInfo {
	entries := customPrecompiles()
	result := make([]Eth2028PrecompileInfo, len(entries))
	for i, e := range entries {
		cat := "custom"
		niiBase := gethcommon.BytesToAddress([]byte{0x02, 0x01})
		switch {
		case e.addr == gethcommon.BytesToAddress([]byte{0x06}) ||
			e.addr == gethcommon.BytesToAddress([]byte{0x08}) ||
			e.addr == gethcommon.BytesToAddress([]byte{0x09}) ||
			e.addr == gethcommon.BytesToAddress([]byte{0x0a}):
			cat = "repricing"
		case e.addr == gethcommon.BytesToAddress([]byte{0x15}):
			cat = "ntt"
		case e.addr[18] == niiBase[18] && e.addr[19] >= 0x01 && e.addr[19] <= 0x04:
			cat = "nii"
		case e.addr[18] == niiBase[18] && e.addr[19] >= 0x05 && e.addr[19] <= 0x08:
			cat = "field"
		}
		result[i] = Eth2028PrecompileInfo{
			Address:  e.addr,
			Name:     e.name,
			MinFork:  e.minFork,
			Category: cat,
		}
	}
	return result
}

// addToAccessList adds the given addresses to the EIP-2929 access list.
// This should be called before transaction execution to warm custom precompile
// addresses (in addition to go-ethereum's standard precompile warming).
func addToAccessList(addresses []types.Address) []gethcommon.Address {
	result := make([]gethcommon.Address, len(addresses))
	for i, addr := range addresses {
		result[i] = ToGethAddress(addr)
	}
	return result
}
