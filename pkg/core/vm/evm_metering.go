// evm_metering.go implements advanced gas metering infrastructure for gigagas
// execution. It provides fine-grained tracking per opcode category, hotspot
// detection, and static gas estimation from bytecode analysis.
//
// Part of the EL roadmap: gigagas L1 (1 Ggas/sec) target (M+).
package vm

import (
	"sort"
	"sync"
	"time"
)

// OpCategory classifies opcodes into functional categories for gas profiling.
type OpCategory uint8

const (
	// CategoryCompute covers arithmetic, logic, and comparison opcodes.
	CategoryCompute OpCategory = iota

	// CategoryStorage covers SLOAD, SSTORE, TLOAD, TSTORE operations.
	CategoryStorage

	// CategoryCall covers CALL, DELEGATECALL, STATICCALL, CREATE, CREATE2.
	CategoryCall

	// CategoryMemory covers MLOAD, MSTORE, MSTORE8, MCOPY, MSIZE, memory expansion.
	CategoryMemory

	// CategorySystem covers block info, tx info, control flow, logging, etc.
	CategorySystem
)

// categoryNames maps categories to human-readable names.
var categoryNames = map[OpCategory]string{
	CategoryCompute: "compute",
	CategoryStorage: "storage",
	CategoryCall:    "call",
	CategoryMemory:  "memory",
	CategorySystem:  "system",
}

// String returns the human-readable name of the category.
func (c OpCategory) String() string {
	if name, ok := categoryNames[c]; ok {
		return name
	}
	return "unknown"
}

// ClassifyOpCode returns the functional category for an opcode.
func ClassifyOpCode(op OpCode) OpCategory {
	switch {
	// Arithmetic and logic
	case op >= ADD && op <= SIGNEXTEND:
		return CategoryCompute
	case op >= LT && op <= SAR:
		return CategoryCompute
	case op == CLZ:
		return CategoryCompute
	case op == KECCAK256:
		return CategoryCompute

	// Storage
	case op == SLOAD || op == SSTORE:
		return CategoryStorage
	case op == TLOAD || op == TSTORE:
		return CategoryStorage

	// Memory
	case op == MLOAD || op == MSTORE || op == MSTORE8:
		return CategoryMemory
	case op == MCOPY || op == MSIZE:
		return CategoryMemory
	case op == CALLDATACOPY || op == CODECOPY || op == EXTCODECOPY || op == RETURNDATACOPY:
		return CategoryMemory

	// Call
	case op == CALL || op == CALLCODE || op == DELEGATECALL || op == STATICCALL:
		return CategoryCall
	case op == CREATE || op == CREATE2:
		return CategoryCall

	// Everything else: block info, tx info, push/dup/swap, control flow, logs
	default:
		return CategorySystem
	}
}

// MeteringPolicy defines configurable gas metering parameters for execution
// profiling and gas limit enforcement.
type MeteringPolicy struct {
	// MaxGasPerOp is the maximum gas that any single opcode execution may consume.
	// Operations exceeding this are flagged in hotspot analysis. 0 means unlimited.
	MaxGasPerOp uint64

	// WarmStorageCost is the gas charged for warm storage reads (EIP-2929).
	WarmStorageCost uint64

	// ColdStorageCost is the gas charged for cold storage reads (EIP-2929).
	ColdStorageCost uint64
}

// DefaultMeteringPolicy returns a policy with standard post-Shanghai values.
func DefaultMeteringPolicy() MeteringPolicy {
	return MeteringPolicy{
		MaxGasPerOp:     0, // unlimited
		WarmStorageCost: WarmStorageReadCost,
		ColdStorageCost: ColdSloadCost,
	}
}

// opExecution records a single opcode execution for profiling.
type opExecution struct {
	Op       OpCode
	GasCost  uint64
	Duration time.Duration
}

// GasProfile provides a breakdown of gas consumption by category.
type GasProfile struct {
	TotalGas   uint64
	ComputeGas uint64
	StorageGas uint64
	CallGas    uint64
	MemoryGas  uint64
	SystemGas  uint64

	// TotalOps is the total number of opcode executions recorded.
	TotalOps uint64

	// TotalDuration is the aggregate wall-clock time for all recorded ops.
	TotalDuration time.Duration
}

// Hotspot identifies an opcode that consumes significant gas in an execution trace.
type Hotspot struct {
	Op       OpCode
	Count    uint64
	TotalGas uint64
	AvgGas   uint64

	// TotalDuration is the aggregate execution time for this opcode.
	TotalDuration time.Duration
}

// GasMeter provides fine-grained gas tracking per opcode category.
// It records individual opcode executions and supports post-hoc analysis.
// Thread-safe.
type GasMeter struct {
	mu     sync.Mutex
	policy MeteringPolicy

	// executions stores the raw execution trace.
	executions []opExecution

	// perOp aggregates gas and count per opcode.
	perOp map[OpCode]*opStats
}

// opStats tracks aggregate stats for a single opcode.
type opStats struct {
	count    uint64
	totalGas uint64
	totalDur time.Duration
}

// NewGasMeter creates a new GasMeter with the given metering policy.
func NewGasMeter(policy MeteringPolicy) *GasMeter {
	return &GasMeter{
		policy:     policy,
		executions: make([]opExecution, 0, 1024),
		perOp:      make(map[OpCode]*opStats),
	}
}

// RecordOpExecution records the execution of a single opcode with its gas
// cost and wall-clock duration. This is called by the EVM interpreter loop.
func (gm *GasMeter) RecordOpExecution(op OpCode, gasCost uint64, duration time.Duration) {
	gm.mu.Lock()
	defer gm.mu.Unlock()

	gm.executions = append(gm.executions, opExecution{
		Op:       op,
		GasCost:  gasCost,
		Duration: duration,
	})

	stats, ok := gm.perOp[op]
	if !ok {
		stats = &opStats{}
		gm.perOp[op] = stats
	}
	stats.count++
	stats.totalGas += gasCost
	stats.totalDur += duration
}

// AnalyzeGasUsage returns a GasProfile with gas broken down by category.
func (gm *GasMeter) AnalyzeGasUsage() GasProfile {
	gm.mu.Lock()
	defer gm.mu.Unlock()

	var profile GasProfile
	for _, exec := range gm.executions {
		profile.TotalGas += exec.GasCost
		profile.TotalOps++
		profile.TotalDuration += exec.Duration

		switch ClassifyOpCode(exec.Op) {
		case CategoryCompute:
			profile.ComputeGas += exec.GasCost
		case CategoryStorage:
			profile.StorageGas += exec.GasCost
		case CategoryCall:
			profile.CallGas += exec.GasCost
		case CategoryMemory:
			profile.MemoryGas += exec.GasCost
		case CategorySystem:
			profile.SystemGas += exec.GasCost
		}
	}
	return profile
}

// Policy returns the current metering policy.
func (gm *GasMeter) Policy() MeteringPolicy {
	return gm.policy
}

// Reset clears all recorded executions and per-op statistics.
func (gm *GasMeter) Reset() {
	gm.mu.Lock()
	defer gm.mu.Unlock()
	gm.executions = gm.executions[:0]
	gm.perOp = make(map[OpCode]*opStats)
}

// ExecutionCount returns the total number of recorded opcode executions.
func (gm *GasMeter) ExecutionCount() uint64 {
	gm.mu.Lock()
	defer gm.mu.Unlock()
	return uint64(len(gm.executions))
}

// HotspotDetector identifies the most expensive opcodes in an execution trace.
type HotspotDetector struct {
	// TopN is the number of top hotspots to report.
	TopN int
}

// NewHotspotDetector creates a detector that reports the top N hotspots.
func NewHotspotDetector(topN int) *HotspotDetector {
	if topN <= 0 {
		topN = 10
	}
	return &HotspotDetector{TopN: topN}
}

// Detect analyzes a GasMeter and returns the top N hotspots sorted by total gas.
func (hd *HotspotDetector) Detect(gm *GasMeter) []Hotspot {
	gm.mu.Lock()
	defer gm.mu.Unlock()

	hotspots := make([]Hotspot, 0, len(gm.perOp))
	for op, stats := range gm.perOp {
		avgGas := uint64(0)
		if stats.count > 0 {
			avgGas = stats.totalGas / stats.count
		}
		hotspots = append(hotspots, Hotspot{
			Op:            op,
			Count:         stats.count,
			TotalGas:      stats.totalGas,
			AvgGas:        avgGas,
			TotalDuration: stats.totalDur,
		})
	}

	// Sort by total gas descending.
	sort.Slice(hotspots, func(i, j int) bool {
		return hotspots[i].TotalGas > hotspots[j].TotalGas
	})

	if len(hotspots) > hd.TopN {
		hotspots = hotspots[:hd.TopN]
	}
	return hotspots
}

// ExceedsPolicy returns hotspots whose average gas per execution exceeds
// the metering policy's MaxGasPerOp. Returns nil if MaxGasPerOp is 0.
func (hd *HotspotDetector) ExceedsPolicy(gm *GasMeter) []Hotspot {
	if gm.policy.MaxGasPerOp == 0 {
		return nil
	}

	all := hd.Detect(gm)
	var flagged []Hotspot
	for _, h := range all {
		if h.AvgGas > gm.policy.MaxGasPerOp {
			flagged = append(flagged, h)
		}
	}
	return flagged
}

// staticGasCosts provides base gas costs for opcodes used in static analysis.
// Only non-dynamic costs are listed; dynamic opcodes (SSTORE, CALL, etc.)
// use their minimum base cost.
var staticGasCosts = map[OpCode]uint64{
	STOP: 0, ADD: 3, MUL: 5, SUB: 3, DIV: 5, SDIV: 5, MOD: 5, SMOD: 5,
	ADDMOD: 8, MULMOD: 8, EXP: 10, SIGNEXTEND: 5,
	LT: 3, GT: 3, SLT: 3, SGT: 3, EQ: 3, ISZERO: 3,
	AND: 3, OR: 3, XOR: 3, NOT: 3, BYTE: 3,
	SHL: 3, SHR: 3, SAR: 3, CLZ: 3,
	KECCAK256: 30,
	ADDRESS:   2, BALANCE: 2600, ORIGIN: 2, CALLER: 2, CALLVALUE: 2,
	CALLDATALOAD: 3, CALLDATASIZE: 2, CALLDATACOPY: 3,
	CODESIZE: 2, CODECOPY: 3, GASPRICE: 2,
	EXTCODESIZE: 2600, EXTCODECOPY: 2600, RETURNDATASIZE: 2, RETURNDATACOPY: 3,
	EXTCODEHASH: 2600,
	BLOCKHASH:   20, COINBASE: 2, TIMESTAMP: 2, NUMBER: 2, PREVRANDAO: 2,
	GASLIMIT: 2, CHAINID: 2, SELFBALANCE: 5, BASEFEE: 2,
	BLOBHASH: 3, BLOBBASEFEE: 2,
	POP: 2, MLOAD: 3, MSTORE: 3, MSTORE8: 3,
	SLOAD: 2100, SSTORE: 2900,
	JUMP: 8, JUMPI: 10, PC: 2, MSIZE: 2, GAS: 2, JUMPDEST: 1,
	TLOAD: 100, TSTORE: 100, MCOPY: 3,
	PUSH0: 2,
	LOG0:  375, LOG1: 750, LOG2: 1125, LOG3: 1500, LOG4: 1875,
	CREATE: 32000, CALL: 2600, CALLCODE: 2600,
	RETURN: 0, DELEGATECALL: 2600, CREATE2: 32000,
	STATICCALL: 2600, REVERT: 0, INVALID: 0, SELFDESTRUCT: 5000,
}

func init() {
	// PUSH1..PUSH32 all cost 3 gas.
	for op := PUSH1; op <= PUSH32; op++ {
		staticGasCosts[op] = 3
	}
	// DUP1..DUP16 all cost 3 gas.
	for op := DUP1; op <= DUP16; op++ {
		staticGasCosts[op] = 3
	}
	// SWAP1..SWAP16 all cost 3 gas.
	for op := SWAP1; op <= SWAP16; op++ {
		staticGasCosts[op] = 3
	}
}

// PredictGas performs a static gas estimation by scanning bytecode and
// summing the base gas cost of each opcode. This provides a lower bound
// since dynamic costs (memory expansion, storage access patterns) cannot
// be determined statically. Returns 0 for empty bytecode.
func PredictGas(bytecode []byte) uint64 {
	if len(bytecode) == 0 {
		return 0
	}

	var totalGas uint64
	for i := 0; i < len(bytecode); {
		op := OpCode(bytecode[i])

		// Add the static gas cost for this opcode.
		if cost, ok := staticGasCosts[op]; ok {
			totalGas += cost
		} else {
			// Unknown opcode, charge INVALID cost (0).
			totalGas += 0
		}

		// Skip over push data bytes.
		if op.IsPush() {
			pushSize := int(op-PUSH1) + 1
			i += 1 + pushSize
		} else {
			i++
		}
	}
	return totalGas
}

// CategoryBreakdown returns a map of gas consumption per OpCategory from
// a bytecode's static analysis. Useful for pre-execution profiling.
func CategoryBreakdown(bytecode []byte) map[OpCategory]uint64 {
	breakdown := make(map[OpCategory]uint64)
	if len(bytecode) == 0 {
		return breakdown
	}

	for i := 0; i < len(bytecode); {
		op := OpCode(bytecode[i])
		cost := uint64(0)
		if c, ok := staticGasCosts[op]; ok {
			cost = c
		}

		cat := ClassifyOpCode(op)
		breakdown[cat] += cost

		if op.IsPush() {
			pushSize := int(op-PUSH1) + 1
			i += 1 + pushSize
		} else {
			i++
		}
	}
	return breakdown
}
