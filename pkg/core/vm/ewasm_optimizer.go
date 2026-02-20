// Package vm implements the Ethereum Virtual Machine.
//
// ewasm_optimizer.go provides JIT optimization passes for eWASM bytecode.
// Part of the J+ era roadmap: "more precompiles in eWASM", "STF in eRISC".
// These passes transform WASM instruction sequences to reduce gas costs
// and improve execution throughput for provable computation.
package vm

import (
	"encoding/binary"
	"time"
)

// WasmOpcode represents a WebAssembly instruction opcode.
type WasmOpcode byte

// WASM instruction opcodes used by the optimizer.
const (
	WasmI32Const WasmOpcode = 0x41
	WasmI64Const WasmOpcode = 0x42
	WasmI32Add   WasmOpcode = 0x6A
	WasmI64Add   WasmOpcode = 0x7C
	WasmI32Sub   WasmOpcode = 0x6B
	WasmI64Sub   WasmOpcode = 0x7D
	WasmI32Mul   WasmOpcode = 0x6C
	WasmI64Mul   WasmOpcode = 0x7E
	WasmI32And   WasmOpcode = 0x71
	WasmI64And   WasmOpcode = 0x83
	WasmI32Or    WasmOpcode = 0x72
	WasmI64Or    WasmOpcode = 0x84
	WasmI32Xor   WasmOpcode = 0x73
	WasmI64Xor   WasmOpcode = 0x85
	WasmI32Shl   WasmOpcode = 0x74
	WasmI64Shl   WasmOpcode = 0x86
	WasmI32ShrU  WasmOpcode = 0x76
	WasmI64ShrU  WasmOpcode = 0x88

	WasmLocalGet WasmOpcode = 0x20
	WasmLocalSet WasmOpcode = 0x21
	WasmDrop     WasmOpcode = 0x1A
	WasmSelect   WasmOpcode = 0x1B
	WasmCall     WasmOpcode = 0x10
	WasmReturn   WasmOpcode = 0x0F
	WasmBlock    WasmOpcode = 0x02
	WasmLoop     WasmOpcode = 0x03
	WasmBrIf     WasmOpcode = 0x0D
	WasmBr       WasmOpcode = 0x0C
	WasmEnd      WasmOpcode = 0x0B
	WasmNop      WasmOpcode = 0x01

	WasmUnreachable WasmOpcode = 0x00
)

// WasmOp represents a single WebAssembly instruction with opcode and operands.
type WasmOp struct {
	// Opcode is the WASM instruction opcode.
	Opcode WasmOpcode

	// Immediates holds immediate operands (constants, indices).
	// For I32Const: 4 bytes LE. For I64Const: 8 bytes LE.
	// For LocalGet/Set: 4 bytes LE local index.
	// For Call: 4 bytes LE function index.
	Immediates []byte
}

// I32Value extracts the i32 immediate value from a WasmOp.
// Returns 0 if the op is not an i32 const or immediates are malformed.
func (op WasmOp) I32Value() uint32 {
	if op.Opcode != WasmI32Const || len(op.Immediates) < 4 {
		return 0
	}
	return binary.LittleEndian.Uint32(op.Immediates[:4])
}

// I64Value extracts the i64 immediate value from a WasmOp.
func (op WasmOp) I64Value() uint64 {
	if op.Opcode != WasmI64Const || len(op.Immediates) < 8 {
		return 0
	}
	return binary.LittleEndian.Uint64(op.Immediates[:8])
}

// LocalIndex extracts the local variable index from a LocalGet/LocalSet op.
func (op WasmOp) LocalIndex() uint32 {
	if len(op.Immediates) < 4 {
		return 0
	}
	return binary.LittleEndian.Uint32(op.Immediates[:4])
}

// NewI32Const creates an i32.const instruction.
func NewI32Const(val uint32) WasmOp {
	imm := make([]byte, 4)
	binary.LittleEndian.PutUint32(imm, val)
	return WasmOp{Opcode: WasmI32Const, Immediates: imm}
}

// NewI64Const creates an i64.const instruction.
func NewI64Const(val uint64) WasmOp {
	imm := make([]byte, 8)
	binary.LittleEndian.PutUint64(imm, val)
	return WasmOp{Opcode: WasmI64Const, Immediates: imm}
}

// NewLocalGet creates a local.get instruction.
func NewLocalGet(idx uint32) WasmOp {
	imm := make([]byte, 4)
	binary.LittleEndian.PutUint32(imm, idx)
	return WasmOp{Opcode: WasmLocalGet, Immediates: imm}
}

// NewLocalSet creates a local.set instruction.
func NewLocalSet(idx uint32) WasmOp {
	imm := make([]byte, 4)
	binary.LittleEndian.PutUint32(imm, idx)
	return WasmOp{Opcode: WasmLocalSet, Immediates: imm}
}

// NewCallOp creates a call instruction.
func NewCallOp(funcIdx uint32) WasmOp {
	imm := make([]byte, 4)
	binary.LittleEndian.PutUint32(imm, funcIdx)
	return WasmOp{Opcode: WasmCall, Immediates: imm}
}

// OptimizationPass defines an optimization transformation on WASM bytecode.
type OptimizationPass interface {
	// Name returns the pass name for metrics/logging.
	Name() string

	// Apply transforms a sequence of WASM instructions, returning the
	// optimized sequence.
	Apply(bytecode []WasmOp) []WasmOp
}

// OptimizationMetrics records statistics from an optimization run.
type OptimizationMetrics struct {
	PassName  string
	OpsBefore int
	OpsAfter  int
	Duration  time.Duration
}

// --- ConstantFolding ---

// ConstantFolding evaluates constant expressions at compile time.
// When two consecutive const instructions are followed by an arithmetic op,
// the three instructions are replaced by a single const with the result.
type ConstantFolding struct{}

func (p *ConstantFolding) Name() string { return "constant-folding" }

func (p *ConstantFolding) Apply(bytecode []WasmOp) []WasmOp {
	if len(bytecode) < 3 {
		return bytecode
	}
	out := make([]WasmOp, 0, len(bytecode))
	i := 0
	for i < len(bytecode) {
		// Try to fold i32 const pairs.
		if i+2 < len(bytecode) &&
			bytecode[i].Opcode == WasmI32Const &&
			bytecode[i+1].Opcode == WasmI32Const {
			a := bytecode[i].I32Value()
			b := bytecode[i+1].I32Value()
			if result, ok := foldI32(a, b, bytecode[i+2].Opcode); ok {
				out = append(out, NewI32Const(result))
				i += 3
				continue
			}
		}
		// Try to fold i64 const pairs.
		if i+2 < len(bytecode) &&
			bytecode[i].Opcode == WasmI64Const &&
			bytecode[i+1].Opcode == WasmI64Const {
			a := bytecode[i].I64Value()
			b := bytecode[i+1].I64Value()
			if result, ok := foldI64(a, b, bytecode[i+2].Opcode); ok {
				out = append(out, NewI64Const(result))
				i += 3
				continue
			}
		}
		out = append(out, bytecode[i])
		i++
	}
	return out
}

// foldI32 evaluates an i32 binary operation on constants.
func foldI32(a, b uint32, op WasmOpcode) (uint32, bool) {
	switch op {
	case WasmI32Add:
		return a + b, true
	case WasmI32Sub:
		return a - b, true
	case WasmI32Mul:
		return a * b, true
	case WasmI32And:
		return a & b, true
	case WasmI32Or:
		return a | b, true
	case WasmI32Xor:
		return a ^ b, true
	case WasmI32Shl:
		return a << (b & 31), true
	case WasmI32ShrU:
		return a >> (b & 31), true
	default:
		return 0, false
	}
}

// foldI64 evaluates an i64 binary operation on constants.
func foldI64(a, b uint64, op WasmOpcode) (uint64, bool) {
	switch op {
	case WasmI64Add:
		return a + b, true
	case WasmI64Sub:
		return a - b, true
	case WasmI64Mul:
		return a * b, true
	case WasmI64And:
		return a & b, true
	case WasmI64Or:
		return a | b, true
	case WasmI64Xor:
		return a ^ b, true
	case WasmI64Shl:
		return a << (b & 63), true
	case WasmI64ShrU:
		return a >> (b & 63), true
	default:
		return 0, false
	}
}

// --- DeadCodeElimination ---

// DeadCodeElimination removes unreachable instructions. Instructions after
// an unconditional Return or Unreachable (within the same block depth) are
// removed until the next Block, Loop, or End marker.
type DeadCodeElimination struct{}

func (p *DeadCodeElimination) Name() string { return "dead-code-elimination" }

func (p *DeadCodeElimination) Apply(bytecode []WasmOp) []WasmOp {
	out := make([]WasmOp, 0, len(bytecode))
	dead := false
	deadDepth := 0 // nested blocks opened WITHIN dead code

	for _, op := range bytecode {
		// Track block nesting.
		switch op.Opcode {
		case WasmBlock, WasmLoop:
			if dead {
				deadDepth++
				continue
			}
			out = append(out, op)
			continue
		case WasmEnd:
			if dead {
				if deadDepth > 0 {
					deadDepth--
					continue
				}
				// Matching End for the block that went dead. Emit and resume.
				dead = false
			}
			out = append(out, op)
			continue
		}

		if dead {
			continue
		}

		out = append(out, op)

		// After Return or Unreachable, mark subsequent code as dead.
		if op.Opcode == WasmReturn || op.Opcode == WasmUnreachable {
			dead = true
		}
	}
	return out
}

// --- StackScheduling ---

// StackScheduling optimizes stack operations by eliminating redundant
// push/pop pairs. Patterns like (local.set X; local.get X) that operate
// on the same local are replaced by a single tee (local.set + dup).
// Also eliminates push-then-drop sequences.
type StackScheduling struct{}

func (p *StackScheduling) Name() string { return "stack-scheduling" }

func (p *StackScheduling) Apply(bytecode []WasmOp) []WasmOp {
	if len(bytecode) < 2 {
		return bytecode
	}
	out := make([]WasmOp, 0, len(bytecode))
	i := 0
	for i < len(bytecode) {
		// Pattern: const X; drop -> eliminate both.
		if i+1 < len(bytecode) &&
			isConstOp(bytecode[i].Opcode) &&
			bytecode[i+1].Opcode == WasmDrop {
			i += 2
			continue
		}
		// Pattern: local.get X; drop -> eliminate both.
		if i+1 < len(bytecode) &&
			bytecode[i].Opcode == WasmLocalGet &&
			bytecode[i+1].Opcode == WasmDrop {
			i += 2
			continue
		}
		// Pattern: local.set X; local.get X (same index) -> keep set, re-push.
		// This is the "tee" pattern: we keep the local.set and add a local.get.
		// (Effectively a no-op transformation, but if the get is the only use
		// it signals to later passes that the value is still on the stack.)
		if i+1 < len(bytecode) &&
			bytecode[i].Opcode == WasmLocalSet &&
			bytecode[i+1].Opcode == WasmLocalGet &&
			bytecode[i].LocalIndex() == bytecode[i+1].LocalIndex() {
			// Keep both; this is already minimal for tee semantics.
			out = append(out, bytecode[i], bytecode[i+1])
			i += 2
			continue
		}
		// Pattern: nop -> remove.
		if bytecode[i].Opcode == WasmNop {
			i++
			continue
		}
		out = append(out, bytecode[i])
		i++
	}
	return out
}

func isConstOp(op WasmOpcode) bool {
	return op == WasmI32Const || op == WasmI64Const
}

// --- InliningPass ---

// InliningPass inlines small function calls. When a Call targets a function
// whose body (provided via the FunctionBodies map) is below the size
// threshold, the Call is replaced by the function body instructions.
type InliningPass struct {
	// FunctionBodies maps function index to its instruction body.
	// Only functions with len <= MaxInlineSize are inlined.
	FunctionBodies map[uint32][]WasmOp

	// MaxInlineSize is the maximum number of ops in a function body
	// that qualifies for inlining. Default: 8.
	MaxInlineSize int
}

func (p *InliningPass) Name() string { return "inlining" }

func (p *InliningPass) Apply(bytecode []WasmOp) []WasmOp {
	maxSize := p.MaxInlineSize
	if maxSize <= 0 {
		maxSize = 8
	}
	if p.FunctionBodies == nil {
		return bytecode
	}
	out := make([]WasmOp, 0, len(bytecode))
	for _, op := range bytecode {
		if op.Opcode == WasmCall && len(op.Immediates) >= 4 {
			idx := binary.LittleEndian.Uint32(op.Immediates[:4])
			if body, ok := p.FunctionBodies[idx]; ok && len(body) <= maxSize {
				// Inline: append body without trailing Return/End.
				for _, bop := range body {
					if bop.Opcode == WasmReturn || bop.Opcode == WasmEnd {
						continue
					}
					out = append(out, bop)
				}
				continue
			}
		}
		out = append(out, op)
	}
	return out
}

// --- LoopUnrolling ---

// LoopUnrolling unrolls small fixed-iteration loops. It recognizes patterns
// where a loop body is small and preceded by a known iteration count constant.
// The pattern is: i32.const N; loop_marker; <body>; end_marker.
// Replaced by N copies of <body>.
type LoopUnrolling struct {
	// MaxUnrollCount limits how many iterations to unroll. Default: 8.
	MaxUnrollCount int

	// MaxBodySize limits the loop body size for unrolling. Default: 16.
	MaxBodySize int
}

func (p *LoopUnrolling) Name() string { return "loop-unrolling" }

func (p *LoopUnrolling) Apply(bytecode []WasmOp) []WasmOp {
	maxUnroll := p.MaxUnrollCount
	if maxUnroll <= 0 {
		maxUnroll = 8
	}
	maxBody := p.MaxBodySize
	if maxBody <= 0 {
		maxBody = 16
	}

	out := make([]WasmOp, 0, len(bytecode))
	i := 0
	for i < len(bytecode) {
		// Pattern: i32.const N; loop; <body...>; end
		if i+2 < len(bytecode) &&
			bytecode[i].Opcode == WasmI32Const &&
			bytecode[i+1].Opcode == WasmLoop {
			n := int(bytecode[i].I32Value())
			if n > 0 && n <= maxUnroll {
				// Find matching end.
				bodyStart := i + 2
				bodyEnd := findMatchingEnd(bytecode, bodyStart)
				if bodyEnd > bodyStart {
					bodyLen := bodyEnd - bodyStart
					if bodyLen <= maxBody {
						body := bytecode[bodyStart:bodyEnd]
						for j := 0; j < n; j++ {
							out = append(out, body...)
						}
						i = bodyEnd + 1 // skip past end
						continue
					}
				}
			}
		}
		out = append(out, bytecode[i])
		i++
	}
	return out
}

// findMatchingEnd finds the index of the End op that closes a block
// starting at startIdx. Returns -1 if not found.
func findMatchingEnd(bytecode []WasmOp, startIdx int) int {
	depth := 1
	for i := startIdx; i < len(bytecode); i++ {
		switch bytecode[i].Opcode {
		case WasmBlock, WasmLoop:
			depth++
		case WasmEnd:
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// --- OptimizationPipeline ---

// OptimizationPipeline chains multiple optimization passes together.
// Passes are applied in order, and metrics are recorded for each.
type OptimizationPipeline struct {
	passes  []OptimizationPass
	metrics []OptimizationMetrics
}

// NewOptimizationPipeline creates a pipeline with the given passes.
func NewOptimizationPipeline(passes ...OptimizationPass) *OptimizationPipeline {
	return &OptimizationPipeline{
		passes: passes,
	}
}

// DefaultOptimizationPipeline returns a pipeline with all standard passes.
func DefaultOptimizationPipeline() *OptimizationPipeline {
	return NewOptimizationPipeline(
		&ConstantFolding{},
		&DeadCodeElimination{},
		&StackScheduling{},
		&LoopUnrolling{},
	)
}

// Apply runs all passes sequentially on the bytecode.
func (p *OptimizationPipeline) Apply(bytecode []WasmOp) []WasmOp {
	p.metrics = make([]OptimizationMetrics, 0, len(p.passes))
	current := bytecode

	for _, pass := range p.passes {
		before := len(current)
		start := time.Now()
		current = pass.Apply(current)
		dur := time.Since(start)
		p.metrics = append(p.metrics, OptimizationMetrics{
			PassName:  pass.Name(),
			OpsBefore: before,
			OpsAfter:  len(current),
			Duration:  dur,
		})
	}
	return current
}

// Metrics returns optimization metrics from the most recent Apply call.
func (p *OptimizationPipeline) Metrics() []OptimizationMetrics {
	return p.metrics
}

// TotalReduction returns the total number of ops eliminated across all passes.
func (p *OptimizationPipeline) TotalReduction() int {
	if len(p.metrics) == 0 {
		return 0
	}
	first := p.metrics[0].OpsBefore
	last := p.metrics[len(p.metrics)-1].OpsAfter
	if first > last {
		return first - last
	}
	return 0
}

// TotalDuration returns the total time spent across all passes.
func (p *OptimizationPipeline) TotalDuration() time.Duration {
	var total time.Duration
	for _, m := range p.metrics {
		total += m.Duration
	}
	return total
}
