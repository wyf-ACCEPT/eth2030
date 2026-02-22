package vm

import (
	"sync"
)

// ForkLevel represents a network upgrade level for gas repricing.
type ForkLevel int

const (
	// ForkGlamsterdam is the Glamsterdam upgrade (2026).
	ForkGlamsterdam ForkLevel = iota
	// ForkHogota is the Hogota upgrade (2026-2027).
	ForkHogota
	// ForkI is the I+ upgrade (2027).
	ForkI
	// ForkJ is the J+ upgrade (2027-2028).
	ForkJ
	// ForkK is the K+ upgrade (2028).
	ForkK
)

// forkLevelNames maps fork levels to human-readable names.
var forkLevelNames = map[ForkLevel]string{
	ForkGlamsterdam: "Glamsterdam",
	ForkHogota:      "Hogota",
	ForkI:           "I+",
	ForkJ:           "J+",
	ForkK:           "K+",
}

// String returns the name of the fork level.
func (f ForkLevel) String() string {
	if name, ok := forkLevelNames[f]; ok {
		return name
	}
	return "Unknown"
}

// RepricingRule describes a gas repricing change for a specific opcode at a fork.
type RepricingRule struct {
	Opcode byte
	OldGas uint64
	NewGas uint64
	Fork   ForkLevel
	Reason string
}

// RepricingImpact estimates the gas cost impact of a fork's repricing on tx data.
type RepricingImpact struct {
	OldGas        uint64
	NewGas        uint64
	Savings       int64
	PercentChange float64
}

// RepricingEngine manages gas repricing across network upgrades.
// It is safe for concurrent use.
type RepricingEngine struct {
	mu       sync.RWMutex
	fork     ForkLevel
	gasCosts [256]uint64 // per-opcode gas cost at the current fork
	rules    map[ForkLevel][]*RepricingRule
}

// baseGasCosts returns the pre-Glamsterdam (Berlin/London/Cancun) baseline
// gas costs for opcodes that participate in repricing.
func baseGasCosts() [256]uint64 {
	var costs [256]uint64

	// State access opcodes (EIP-2929 Berlin costs).
	costs[byte(SLOAD)] = ColdSloadCost               // 2100
	costs[byte(BALANCE)] = ColdAccountAccessCost     // 2600
	costs[byte(EXTCODESIZE)] = ColdAccountAccessCost // 2600
	costs[byte(EXTCODECOPY)] = ColdAccountAccessCost // 2600
	costs[byte(EXTCODEHASH)] = ColdAccountAccessCost // 2600

	// Storage write.
	costs[byte(SSTORE)] = GasSstoreSet // 20000 (set cost; reset is separate)

	// Contract creation.
	costs[byte(CREATE)] = GasCreate  // 32000
	costs[byte(CREATE2)] = GasCreate // 32000

	// Call family (cold access cost).
	costs[byte(CALL)] = GasCallCold         // 2600
	costs[byte(CALLCODE)] = GasCallCold     // 2600
	costs[byte(DELEGATECALL)] = GasCallCold // 2600
	costs[byte(STATICCALL)] = GasCallCold   // 2600

	// Self-destruct.
	costs[byte(SELFDESTRUCT)] = GasSelfdestruct // 5000

	// Compute opcodes.
	costs[byte(KECCAK256)] = GasKeccak256 // 30
	costs[byte(EXP)] = GasSlowStep        // 10

	// Log opcodes.
	costs[byte(LOG0)] = GasLog // 375
	costs[byte(LOG1)] = GasLog
	costs[byte(LOG2)] = GasLog
	costs[byte(LOG3)] = GasLog
	costs[byte(LOG4)] = GasLog

	return costs
}

// allRepricingRules returns all repricing rules indexed by fork.
func allRepricingRules() map[ForkLevel][]*RepricingRule {
	rules := make(map[ForkLevel][]*RepricingRule)

	// Glamsterdam (2026): increased state access costs per EIP-8038.
	rules[ForkGlamsterdam] = []*RepricingRule{
		{
			Opcode: byte(SLOAD), OldGas: ColdSloadCost, NewGas: ColdSloadGlamst,
			Fork: ForkGlamsterdam, Reason: "EIP-8038: state access gas increase",
		},
		{
			Opcode: byte(BALANCE), OldGas: ColdAccountAccessCost, NewGas: ColdAccountAccessGlamst,
			Fork: ForkGlamsterdam, Reason: "EIP-8038: state access gas increase",
		},
		{
			Opcode: byte(EXTCODESIZE), OldGas: ColdAccountAccessCost, NewGas: ColdAccountAccessGlamst,
			Fork: ForkGlamsterdam, Reason: "EIP-8038: state access gas increase",
		},
		{
			Opcode: byte(EXTCODECOPY), OldGas: ColdAccountAccessCost, NewGas: ColdAccountAccessGlamst,
			Fork: ForkGlamsterdam, Reason: "EIP-8038: state access gas increase",
		},
		{
			Opcode: byte(EXTCODEHASH), OldGas: ColdAccountAccessCost, NewGas: ColdAccountAccessGlamst,
			Fork: ForkGlamsterdam, Reason: "EIP-8038: state access gas increase",
		},
		{
			Opcode: byte(KECCAK256), OldGas: GasKeccak256, NewGas: GasKeccak256Glamsterdan,
			Fork: ForkGlamsterdam, Reason: "EIP-7904: compute gas cost increase",
		},
		{
			Opcode: byte(CREATE), OldGas: GasCreate, NewGas: GasCreateGlamsterdam,
			Fork: ForkGlamsterdam, Reason: "EIP-8037: state creation gas increase",
		},
	}

	// Hogota (2026-2027): conversion repricing -- reduce costs that were
	// over-priced relative to actual resource consumption.
	rules[ForkHogota] = []*RepricingRule{
		{
			Opcode: byte(SLOAD), OldGas: ColdSloadGlamst, NewGas: 1800,
			Fork: ForkHogota, Reason: "Hogota conversion repricing: SLOAD reduction",
		},
		{
			Opcode: byte(BALANCE), OldGas: ColdAccountAccessGlamst, NewGas: 400,
			Fork: ForkHogota, Reason: "Hogota conversion repricing: BALANCE reduction",
		},
	}

	// I+ (2027): further repricing for EXT* family.
	rules[ForkI] = []*RepricingRule{
		{
			Opcode: byte(EXTCODESIZE), OldGas: ColdAccountAccessGlamst, NewGas: 100,
			Fork: ForkI, Reason: "I+ repricing: EXTCODESIZE reduction with verkle proofs",
		},
		{
			Opcode: byte(EXTCODEHASH), OldGas: ColdAccountAccessGlamst, NewGas: 200,
			Fork: ForkI, Reason: "I+ repricing: EXTCODEHASH reduction with verkle proofs",
		},
	}

	// J+ (2027-2028): CREATE cost reduction with improved state management.
	rules[ForkJ] = []*RepricingRule{
		{
			Opcode: byte(CREATE), OldGas: GasCreateGlamsterdam, NewGas: 20000,
			Fork: ForkJ, Reason: "J+ repricing: CREATE reduction with advanced state mgmt",
		},
		{
			Opcode: byte(CREATE2), OldGas: GasCreate, NewGas: 20000,
			Fork: ForkJ, Reason: "J+ repricing: CREATE2 reduction with advanced state mgmt",
		},
	}

	// K+ (2028): mandatory proof-based repricing.
	rules[ForkK] = []*RepricingRule{
		{
			Opcode: byte(SLOAD), OldGas: 1800, NewGas: 800,
			Fork: ForkK, Reason: "K+ repricing: SLOAD with mandatory proofs",
		},
		{
			Opcode: byte(CALL), OldGas: GasCallCold, NewGas: 1000,
			Fork: ForkK, Reason: "K+ repricing: CALL cold access with mandatory proofs",
		},
		{
			Opcode: byte(STATICCALL), OldGas: GasCallCold, NewGas: 1000,
			Fork: ForkK, Reason: "K+ repricing: STATICCALL cold access with mandatory proofs",
		},
	}

	return rules
}

// NewRepricingEngine creates a RepricingEngine initialized to the given fork.
// All repricing rules up to and including the specified fork are applied.
func NewRepricingEngine(fork ForkLevel) *RepricingEngine {
	e := &RepricingEngine{
		fork:     fork,
		gasCosts: baseGasCosts(),
		rules:    allRepricingRules(),
	}
	// Apply all repricings up to and including the target fork.
	for f := ForkGlamsterdam; f <= fork; f++ {
		e.applyRulesForFork(f)
	}
	return e
}

// applyRulesForFork applies the repricing rules for a single fork (no lock).
func (e *RepricingEngine) applyRulesForFork(fork ForkLevel) {
	for _, rule := range e.rules[fork] {
		e.gasCosts[rule.Opcode] = rule.NewGas
	}
}

// GetOpGasCost returns the gas cost for an opcode at the given fork level.
// It computes the cost by starting from base and applying all rules up to fork.
func (e *RepricingEngine) GetOpGasCost(opcode byte, fork ForkLevel) uint64 {
	e.mu.RLock()
	defer e.mu.RUnlock()

	// Start from base costs and apply rules up to the requested fork.
	costs := baseGasCosts()
	for f := ForkGlamsterdam; f <= fork; f++ {
		for _, rule := range e.rules[f] {
			costs[rule.Opcode] = rule.NewGas
		}
	}
	return costs[opcode]
}

// GetRepricingRules returns all repricing rules for a specific fork level.
func (e *RepricingEngine) GetRepricingRules(fork ForkLevel) []*RepricingRule {
	e.mu.RLock()
	defer e.mu.RUnlock()

	rules := e.rules[fork]
	// Return a copy to prevent external mutation.
	out := make([]*RepricingRule, len(rules))
	for i, r := range rules {
		cp := *r
		out[i] = &cp
	}
	return out
}

// ApplyRepricing applies all repricing rules up to and including the given fork.
// This updates the engine's current fork level and internal gas cost table.
func (e *RepricingEngine) ApplyRepricing(fork ForkLevel) {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Reset to base and re-apply up to the new fork.
	e.gasCosts = baseGasCosts()
	for f := ForkGlamsterdam; f <= fork; f++ {
		e.applyRulesForFork(f)
	}
	e.fork = fork
}

// CurrentGasCost returns the gas cost for an opcode at the engine's current fork.
func (e *RepricingEngine) CurrentGasCost(opcode byte) uint64 {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.gasCosts[opcode]
}

// CurrentFork returns the engine's current fork level.
func (e *RepricingEngine) CurrentFork() ForkLevel {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.fork
}

// EstimateImpact estimates the gas cost impact of repricing on transaction data.
// It scans txData as raw EVM bytecode (a simplistic heuristic) and sums up gas
// costs for each byte interpreted as an opcode, comparing the engine's current
// fork costs against the costs at the specified target fork.
func (e *RepricingEngine) EstimateImpact(txData []byte, fork ForkLevel) *RepricingImpact {
	e.mu.RLock()
	currentFork := e.fork
	e.mu.RUnlock()

	// Build cost tables for both the current fork and the target fork.
	oldCosts := baseGasCosts()
	for f := ForkGlamsterdam; f <= currentFork; f++ {
		for _, rule := range e.rules[f] {
			oldCosts[rule.Opcode] = rule.NewGas
		}
	}
	newCosts := baseGasCosts()
	for f := ForkGlamsterdam; f <= fork; f++ {
		for _, rule := range e.rules[f] {
			newCosts[rule.Opcode] = rule.NewGas
		}
	}

	var oldTotal, newTotal uint64
	for _, b := range txData {
		oldTotal += oldCosts[b]
		newTotal += newCosts[b]
	}

	impact := &RepricingImpact{
		OldGas:  oldTotal,
		NewGas:  newTotal,
		Savings: int64(oldTotal) - int64(newTotal),
	}
	if oldTotal > 0 {
		impact.PercentChange = (float64(newTotal) - float64(oldTotal)) / float64(oldTotal) * 100.0
	}
	return impact
}
