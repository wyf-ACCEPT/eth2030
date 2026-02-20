package vm

import (
	"testing"
)

// --- WasmOp helpers ---

func TestNewI32Const(t *testing.T) {
	op := NewI32Const(42)
	if op.Opcode != WasmI32Const {
		t.Fatalf("expected WasmI32Const opcode, got 0x%02x", op.Opcode)
	}
	if op.I32Value() != 42 {
		t.Fatalf("expected 42, got %d", op.I32Value())
	}
}

func TestNewI64Const(t *testing.T) {
	op := NewI64Const(123456789)
	if op.Opcode != WasmI64Const {
		t.Fatalf("expected WasmI64Const opcode, got 0x%02x", op.Opcode)
	}
	if op.I64Value() != 123456789 {
		t.Fatalf("expected 123456789, got %d", op.I64Value())
	}
}

func TestI32ValueWrongOpcode(t *testing.T) {
	op := NewI64Const(10)
	if op.I32Value() != 0 {
		t.Fatal("expected 0 for wrong opcode type")
	}
}

func TestI64ValueWrongOpcode(t *testing.T) {
	op := NewI32Const(10)
	if op.I64Value() != 0 {
		t.Fatal("expected 0 for wrong opcode type")
	}
}

func TestLocalIndex(t *testing.T) {
	op := NewLocalGet(7)
	if op.LocalIndex() != 7 {
		t.Fatalf("expected local index 7, got %d", op.LocalIndex())
	}
}

func TestLocalIndexShortImm(t *testing.T) {
	op := WasmOp{Opcode: WasmLocalGet, Immediates: []byte{1}}
	if op.LocalIndex() != 0 {
		t.Fatal("expected 0 for short immediates")
	}
}

// --- ConstantFolding ---

func TestConstantFoldingI32Add(t *testing.T) {
	ops := []WasmOp{
		NewI32Const(10),
		NewI32Const(20),
		{Opcode: WasmI32Add},
	}
	pass := &ConstantFolding{}
	result := pass.Apply(ops)
	if len(result) != 1 {
		t.Fatalf("expected 1 op after folding, got %d", len(result))
	}
	if result[0].I32Value() != 30 {
		t.Fatalf("expected 30, got %d", result[0].I32Value())
	}
}

func TestConstantFoldingI32Sub(t *testing.T) {
	ops := []WasmOp{
		NewI32Const(50),
		NewI32Const(15),
		{Opcode: WasmI32Sub},
	}
	pass := &ConstantFolding{}
	result := pass.Apply(ops)
	if len(result) != 1 || result[0].I32Value() != 35 {
		t.Fatalf("expected 35 after sub folding, got %d ops, val=%d", len(result), result[0].I32Value())
	}
}

func TestConstantFoldingI32Mul(t *testing.T) {
	ops := []WasmOp{
		NewI32Const(6),
		NewI32Const(7),
		{Opcode: WasmI32Mul},
	}
	pass := &ConstantFolding{}
	result := pass.Apply(ops)
	if len(result) != 1 || result[0].I32Value() != 42 {
		t.Fatal("expected 42 after mul folding")
	}
}

func TestConstantFoldingI32And(t *testing.T) {
	ops := []WasmOp{
		NewI32Const(0xFF),
		NewI32Const(0x0F),
		{Opcode: WasmI32And},
	}
	pass := &ConstantFolding{}
	result := pass.Apply(ops)
	if len(result) != 1 || result[0].I32Value() != 0x0F {
		t.Fatal("expected 0x0F after AND folding")
	}
}

func TestConstantFoldingI32Or(t *testing.T) {
	ops := []WasmOp{
		NewI32Const(0xF0),
		NewI32Const(0x0F),
		{Opcode: WasmI32Or},
	}
	pass := &ConstantFolding{}
	result := pass.Apply(ops)
	if len(result) != 1 || result[0].I32Value() != 0xFF {
		t.Fatal("expected 0xFF after OR folding")
	}
}

func TestConstantFoldingI32Xor(t *testing.T) {
	ops := []WasmOp{
		NewI32Const(0xFF),
		NewI32Const(0xAA),
		{Opcode: WasmI32Xor},
	}
	pass := &ConstantFolding{}
	result := pass.Apply(ops)
	if len(result) != 1 || result[0].I32Value() != 0x55 {
		t.Fatal("expected 0x55 after XOR folding")
	}
}

func TestConstantFoldingI32Shl(t *testing.T) {
	ops := []WasmOp{
		NewI32Const(1),
		NewI32Const(4),
		{Opcode: WasmI32Shl},
	}
	pass := &ConstantFolding{}
	result := pass.Apply(ops)
	if len(result) != 1 || result[0].I32Value() != 16 {
		t.Fatal("expected 16 after SHL folding")
	}
}

func TestConstantFoldingI32ShrU(t *testing.T) {
	ops := []WasmOp{
		NewI32Const(256),
		NewI32Const(4),
		{Opcode: WasmI32ShrU},
	}
	pass := &ConstantFolding{}
	result := pass.Apply(ops)
	if len(result) != 1 || result[0].I32Value() != 16 {
		t.Fatal("expected 16 after SHR_U folding")
	}
}

func TestConstantFoldingI64Add(t *testing.T) {
	ops := []WasmOp{
		NewI64Const(100),
		NewI64Const(200),
		{Opcode: WasmI64Add},
	}
	pass := &ConstantFolding{}
	result := pass.Apply(ops)
	if len(result) != 1 || result[0].I64Value() != 300 {
		t.Fatal("expected 300 after i64 add folding")
	}
}

func TestConstantFoldingI64Sub(t *testing.T) {
	ops := []WasmOp{
		NewI64Const(500),
		NewI64Const(123),
		{Opcode: WasmI64Sub},
	}
	pass := &ConstantFolding{}
	result := pass.Apply(ops)
	if len(result) != 1 || result[0].I64Value() != 377 {
		t.Fatal("expected 377 after i64 sub folding")
	}
}

func TestConstantFoldingI64Mul(t *testing.T) {
	ops := []WasmOp{
		NewI64Const(11),
		NewI64Const(13),
		{Opcode: WasmI64Mul},
	}
	pass := &ConstantFolding{}
	result := pass.Apply(ops)
	if len(result) != 1 || result[0].I64Value() != 143 {
		t.Fatal("expected 143 after i64 mul folding")
	}
}

func TestConstantFoldingI64Bitwise(t *testing.T) {
	tests := []struct {
		name   string
		a, b   uint64
		opcode WasmOpcode
		expect uint64
	}{
		{"and", 0xFF, 0x0F, WasmI64And, 0x0F},
		{"or", 0xF0, 0x0F, WasmI64Or, 0xFF},
		{"xor", 0xFF, 0xAA, WasmI64Xor, 0x55},
		{"shl", 1, 8, WasmI64Shl, 256},
		{"shr", 256, 4, WasmI64ShrU, 16},
	}
	pass := &ConstantFolding{}
	for _, tt := range tests {
		ops := []WasmOp{NewI64Const(tt.a), NewI64Const(tt.b), {Opcode: tt.opcode}}
		result := pass.Apply(ops)
		if len(result) != 1 || result[0].I64Value() != tt.expect {
			t.Errorf("%s: expected %d, got %d (len=%d)", tt.name, tt.expect, result[0].I64Value(), len(result))
		}
	}
}

func TestConstantFoldingNoFold(t *testing.T) {
	// Non-constant + constant should not fold.
	ops := []WasmOp{
		NewLocalGet(0),
		NewI32Const(5),
		{Opcode: WasmI32Add},
	}
	pass := &ConstantFolding{}
	result := pass.Apply(ops)
	if len(result) != 3 {
		t.Fatalf("expected no folding (3 ops), got %d", len(result))
	}
}

func TestConstantFoldingMixedTypes(t *testing.T) {
	// i32 const + i64 const should not fold.
	ops := []WasmOp{
		NewI32Const(1),
		NewI64Const(2),
		{Opcode: WasmI32Add},
	}
	pass := &ConstantFolding{}
	result := pass.Apply(ops)
	if len(result) != 3 {
		t.Fatal("should not fold mixed i32/i64 types")
	}
}

func TestConstantFoldingUnfoldableOp(t *testing.T) {
	// Const pair followed by a non-arithmetic op.
	ops := []WasmOp{
		NewI32Const(1),
		NewI32Const(2),
		{Opcode: WasmReturn},
	}
	pass := &ConstantFolding{}
	result := pass.Apply(ops)
	if len(result) != 3 {
		t.Fatal("should not fold when op is not arithmetic")
	}
}

func TestConstantFoldingShortInput(t *testing.T) {
	pass := &ConstantFolding{}
	result := pass.Apply([]WasmOp{NewI32Const(1)})
	if len(result) != 1 {
		t.Fatal("single op should pass through")
	}
	result = pass.Apply(nil)
	if len(result) != 0 {
		t.Fatal("nil should return nil/empty")
	}
}

func TestConstantFoldingName(t *testing.T) {
	pass := &ConstantFolding{}
	if pass.Name() != "constant-folding" {
		t.Fatalf("unexpected name: %s", pass.Name())
	}
}

// --- DeadCodeElimination ---

func TestDeadCodeEliminationAfterReturn(t *testing.T) {
	ops := []WasmOp{
		NewI32Const(1),
		{Opcode: WasmReturn},
		NewI32Const(2), // dead
		NewI32Const(3), // dead
	}
	pass := &DeadCodeElimination{}
	result := pass.Apply(ops)
	if len(result) != 2 {
		t.Fatalf("expected 2 ops after DCE, got %d", len(result))
	}
	if result[0].Opcode != WasmI32Const || result[1].Opcode != WasmReturn {
		t.Fatal("unexpected ops after DCE")
	}
}

func TestDeadCodeEliminationAfterUnreachable(t *testing.T) {
	ops := []WasmOp{
		{Opcode: WasmUnreachable},
		NewI32Const(99), // dead
	}
	pass := &DeadCodeElimination{}
	result := pass.Apply(ops)
	if len(result) != 1 {
		t.Fatalf("expected 1 op after DCE, got %d", len(result))
	}
}

func TestDeadCodeEliminationNestedBlock(t *testing.T) {
	// Return inside block: dead code until matching End.
	ops := []WasmOp{
		{Opcode: WasmBlock},
		NewI32Const(1),
		{Opcode: WasmReturn},
		NewI32Const(2), // dead
		{Opcode: WasmEnd},
		NewI32Const(3), // alive again
	}
	pass := &DeadCodeElimination{}
	result := pass.Apply(ops)
	// Block + const(1) + return + end + const(3) = 5.
	if len(result) != 5 {
		t.Fatalf("expected 5 ops, got %d", len(result))
	}
}

func TestDeadCodeEliminationNoDeadCode(t *testing.T) {
	ops := []WasmOp{
		NewI32Const(1),
		NewI32Const(2),
		{Opcode: WasmI32Add},
	}
	pass := &DeadCodeElimination{}
	result := pass.Apply(ops)
	if len(result) != 3 {
		t.Fatalf("expected no elimination, got %d ops", len(result))
	}
}

func TestDeadCodeEliminationName(t *testing.T) {
	pass := &DeadCodeElimination{}
	if pass.Name() != "dead-code-elimination" {
		t.Fatalf("unexpected name: %s", pass.Name())
	}
}

// --- StackScheduling ---

func TestStackSchedulingConstDrop(t *testing.T) {
	ops := []WasmOp{
		NewI32Const(42),
		{Opcode: WasmDrop},
		NewI32Const(1),
	}
	pass := &StackScheduling{}
	result := pass.Apply(ops)
	if len(result) != 1 {
		t.Fatalf("expected 1 op after removing const+drop, got %d", len(result))
	}
	if result[0].I32Value() != 1 {
		t.Fatal("expected remaining const to be 1")
	}
}

func TestStackSchedulingI64ConstDrop(t *testing.T) {
	ops := []WasmOp{
		NewI64Const(99),
		{Opcode: WasmDrop},
	}
	pass := &StackScheduling{}
	result := pass.Apply(ops)
	if len(result) != 0 {
		t.Fatalf("expected 0 ops after removing i64.const+drop, got %d", len(result))
	}
}

func TestStackSchedulingLocalGetDrop(t *testing.T) {
	ops := []WasmOp{
		NewLocalGet(3),
		{Opcode: WasmDrop},
	}
	pass := &StackScheduling{}
	result := pass.Apply(ops)
	if len(result) != 0 {
		t.Fatalf("expected 0 ops after removing local.get+drop, got %d", len(result))
	}
}

func TestStackSchedulingNopRemoval(t *testing.T) {
	ops := []WasmOp{
		{Opcode: WasmNop},
		NewI32Const(1),
		{Opcode: WasmNop},
		{Opcode: WasmNop},
	}
	pass := &StackScheduling{}
	result := pass.Apply(ops)
	if len(result) != 1 {
		t.Fatalf("expected 1 op after nop removal, got %d", len(result))
	}
}

func TestStackSchedulingTeePattern(t *testing.T) {
	// local.set X; local.get X -> keep both (tee pattern).
	ops := []WasmOp{
		NewLocalSet(5),
		NewLocalGet(5),
	}
	pass := &StackScheduling{}
	result := pass.Apply(ops)
	if len(result) != 2 {
		t.Fatalf("expected 2 ops for tee pattern, got %d", len(result))
	}
}

func TestStackSchedulingSetGetDifferentLocal(t *testing.T) {
	// local.set X; local.get Y (X != Y) -> keep both, no tee.
	ops := []WasmOp{
		NewLocalSet(1),
		NewLocalGet(2),
	}
	pass := &StackScheduling{}
	result := pass.Apply(ops)
	if len(result) != 2 {
		t.Fatal("expected no optimization for different local indices")
	}
}

func TestStackSchedulingName(t *testing.T) {
	pass := &StackScheduling{}
	if pass.Name() != "stack-scheduling" {
		t.Fatalf("unexpected name: %s", pass.Name())
	}
}

func TestStackSchedulingShortInput(t *testing.T) {
	pass := &StackScheduling{}
	result := pass.Apply([]WasmOp{{Opcode: WasmReturn}})
	if len(result) != 1 {
		t.Fatal("single op should pass through")
	}
}

// --- InliningPass ---

func TestInliningSmallFunction(t *testing.T) {
	bodies := map[uint32][]WasmOp{
		1: {NewI32Const(42), {Opcode: WasmReturn}},
	}
	ops := []WasmOp{
		NewCallOp(1),
		{Opcode: WasmI32Add},
	}
	pass := &InliningPass{FunctionBodies: bodies, MaxInlineSize: 8}
	result := pass.Apply(ops)
	// Call should be replaced by const(42) (Return stripped).
	if len(result) != 2 {
		t.Fatalf("expected 2 ops after inlining, got %d", len(result))
	}
	if result[0].Opcode != WasmI32Const || result[0].I32Value() != 42 {
		t.Fatal("expected inlined const 42")
	}
}

func TestInliningLargeFunction(t *testing.T) {
	// Function body too large to inline.
	body := make([]WasmOp, 20)
	for i := range body {
		body[i] = NewI32Const(uint32(i))
	}
	bodies := map[uint32][]WasmOp{1: body}
	ops := []WasmOp{NewCallOp(1)}
	pass := &InliningPass{FunctionBodies: bodies, MaxInlineSize: 8}
	result := pass.Apply(ops)
	if len(result) != 1 || result[0].Opcode != WasmCall {
		t.Fatal("large function should not be inlined")
	}
}

func TestInliningUnknownFunction(t *testing.T) {
	bodies := map[uint32][]WasmOp{
		1: {NewI32Const(1), {Opcode: WasmReturn}},
	}
	ops := []WasmOp{NewCallOp(99)} // func 99 not in map
	pass := &InliningPass{FunctionBodies: bodies}
	result := pass.Apply(ops)
	if len(result) != 1 || result[0].Opcode != WasmCall {
		t.Fatal("unknown function call should be preserved")
	}
}

func TestInliningNilBodies(t *testing.T) {
	ops := []WasmOp{NewCallOp(1)}
	pass := &InliningPass{}
	result := pass.Apply(ops)
	if len(result) != 1 {
		t.Fatal("nil function bodies should leave bytecode unchanged")
	}
}

func TestInliningName(t *testing.T) {
	pass := &InliningPass{}
	if pass.Name() != "inlining" {
		t.Fatalf("unexpected name: %s", pass.Name())
	}
}

func TestInliningStripEnd(t *testing.T) {
	bodies := map[uint32][]WasmOp{
		0: {NewI32Const(7), {Opcode: WasmEnd}},
	}
	ops := []WasmOp{NewCallOp(0)}
	pass := &InliningPass{FunctionBodies: bodies, MaxInlineSize: 8}
	result := pass.Apply(ops)
	if len(result) != 1 || result[0].I32Value() != 7 {
		t.Fatal("End should be stripped during inlining")
	}
}

// --- LoopUnrolling ---

func TestLoopUnrollingBasic(t *testing.T) {
	// i32.const 3; loop; i32.const 1; i32.add; end
	ops := []WasmOp{
		NewI32Const(3),
		{Opcode: WasmLoop},
		NewI32Const(1),
		{Opcode: WasmI32Add},
		{Opcode: WasmEnd},
	}
	pass := &LoopUnrolling{MaxUnrollCount: 8, MaxBodySize: 16}
	result := pass.Apply(ops)
	// 3 iterations * 2 ops = 6 ops.
	if len(result) != 6 {
		t.Fatalf("expected 6 ops after unrolling, got %d", len(result))
	}
	for i := 0; i < 6; i += 2 {
		if result[i].Opcode != WasmI32Const || result[i].I32Value() != 1 {
			t.Fatalf("expected i32.const 1 at index %d", i)
		}
		if result[i+1].Opcode != WasmI32Add {
			t.Fatalf("expected i32.add at index %d", i+1)
		}
	}
}

func TestLoopUnrollingTooManyIterations(t *testing.T) {
	ops := []WasmOp{
		NewI32Const(100), // too many
		{Opcode: WasmLoop},
		NewI32Const(1),
		{Opcode: WasmEnd},
	}
	pass := &LoopUnrolling{MaxUnrollCount: 8}
	result := pass.Apply(ops)
	// Should not unroll.
	if len(result) != 4 {
		t.Fatalf("expected no unrolling (4 ops), got %d", len(result))
	}
}

func TestLoopUnrollingBodyTooLarge(t *testing.T) {
	body := make([]WasmOp, 0)
	body = append(body, NewI32Const(2), WasmOp{Opcode: WasmLoop})
	for i := 0; i < 20; i++ {
		body = append(body, NewI32Const(uint32(i)))
	}
	body = append(body, WasmOp{Opcode: WasmEnd})
	pass := &LoopUnrolling{MaxUnrollCount: 8, MaxBodySize: 4}
	result := pass.Apply(body)
	if len(result) != len(body) {
		t.Fatal("body too large, should not unroll")
	}
}

func TestLoopUnrollingZeroIterations(t *testing.T) {
	ops := []WasmOp{
		NewI32Const(0),
		{Opcode: WasmLoop},
		NewI32Const(1),
		{Opcode: WasmEnd},
	}
	pass := &LoopUnrolling{}
	result := pass.Apply(ops)
	// Zero iterations: should not unroll (n > 0 check fails).
	if len(result) != 4 {
		t.Fatalf("expected no unrolling for 0 iterations, got %d ops", len(result))
	}
}

func TestLoopUnrollingName(t *testing.T) {
	pass := &LoopUnrolling{}
	if pass.Name() != "loop-unrolling" {
		t.Fatalf("unexpected name: %s", pass.Name())
	}
}

func TestLoopUnrollingNestedBlocks(t *testing.T) {
	// Nested block inside loop body.
	ops := []WasmOp{
		NewI32Const(2),
		{Opcode: WasmLoop},
		{Opcode: WasmBlock},
		NewI32Const(1),
		{Opcode: WasmEnd}, // end block
		{Opcode: WasmEnd}, // end loop
	}
	pass := &LoopUnrolling{MaxUnrollCount: 8, MaxBodySize: 16}
	result := pass.Apply(ops)
	// Body is: block, const(1), end -> 3 ops, unrolled 2x = 6.
	if len(result) != 6 {
		t.Fatalf("expected 6 ops after nested unrolling, got %d", len(result))
	}
}

// --- OptimizationPipeline ---

func TestPipelineBasic(t *testing.T) {
	ops := []WasmOp{
		NewI32Const(10),
		NewI32Const(20),
		{Opcode: WasmI32Add},
		{Opcode: WasmReturn},
		NewI32Const(99), // dead
	}
	pipeline := DefaultOptimizationPipeline()
	result := pipeline.Apply(ops)
	// Constant folding: 3 -> 1, then DCE removes dead code after return.
	// Expected: i32.const(30), return.
	if len(result) != 2 {
		t.Fatalf("expected 2 ops after pipeline, got %d", len(result))
	}
	if result[0].I32Value() != 30 {
		t.Fatalf("expected 30 after folding, got %d", result[0].I32Value())
	}
}

func TestPipelineMetrics(t *testing.T) {
	ops := []WasmOp{
		NewI32Const(1),
		NewI32Const(2),
		{Opcode: WasmI32Add},
	}
	pipeline := DefaultOptimizationPipeline()
	pipeline.Apply(ops)

	metrics := pipeline.Metrics()
	if len(metrics) != 4 {
		t.Fatalf("expected 4 pass metrics, got %d", len(metrics))
	}
	if metrics[0].PassName != "constant-folding" {
		t.Fatalf("expected first pass to be constant-folding, got %s", metrics[0].PassName)
	}
	if metrics[0].OpsBefore != 3 {
		t.Fatalf("expected 3 ops before folding, got %d", metrics[0].OpsBefore)
	}
	if metrics[0].OpsAfter != 1 {
		t.Fatalf("expected 1 op after folding, got %d", metrics[0].OpsAfter)
	}
}

func TestPipelineTotalReduction(t *testing.T) {
	ops := []WasmOp{
		NewI32Const(1),
		NewI32Const(2),
		{Opcode: WasmI32Add},
		{Opcode: WasmReturn},
		NewI32Const(99),
	}
	pipeline := DefaultOptimizationPipeline()
	pipeline.Apply(ops)
	reduction := pipeline.TotalReduction()
	// 5 -> 2 = reduction of 3.
	if reduction != 3 {
		t.Fatalf("expected reduction of 3, got %d", reduction)
	}
}

func TestPipelineTotalDuration(t *testing.T) {
	ops := []WasmOp{NewI32Const(1)}
	pipeline := DefaultOptimizationPipeline()
	pipeline.Apply(ops)
	// Duration should be non-negative.
	if pipeline.TotalDuration() < 0 {
		t.Fatal("expected non-negative total duration")
	}
}

func TestPipelineEmpty(t *testing.T) {
	pipeline := NewOptimizationPipeline()
	result := pipeline.Apply([]WasmOp{NewI32Const(1)})
	if len(result) != 1 {
		t.Fatal("empty pipeline should return input unchanged")
	}
	if pipeline.TotalReduction() != 0 {
		t.Fatal("expected 0 reduction for empty pipeline")
	}
}

func TestPipelineCustomPasses(t *testing.T) {
	pipeline := NewOptimizationPipeline(
		&ConstantFolding{},
		&StackScheduling{},
	)
	ops := []WasmOp{
		NewI32Const(5),
		NewI32Const(3),
		{Opcode: WasmI32Mul},
		{Opcode: WasmNop},
	}
	result := pipeline.Apply(ops)
	// Fold: 5*3=15 (3->1), then nop removed (2->1).
	if len(result) != 1 {
		t.Fatalf("expected 1 op, got %d", len(result))
	}
	if result[0].I32Value() != 15 {
		t.Fatalf("expected 15, got %d", result[0].I32Value())
	}
}

// --- findMatchingEnd ---

func TestFindMatchingEndBasic(t *testing.T) {
	ops := []WasmOp{
		NewI32Const(1),
		{Opcode: WasmEnd},
	}
	idx := findMatchingEnd(ops, 0)
	if idx != 1 {
		t.Fatalf("expected end at index 1, got %d", idx)
	}
}

func TestFindMatchingEndNested(t *testing.T) {
	ops := []WasmOp{
		{Opcode: WasmBlock},
		NewI32Const(1),
		{Opcode: WasmEnd},
		{Opcode: WasmEnd},
	}
	idx := findMatchingEnd(ops, 0)
	if idx != 3 {
		t.Fatalf("expected outer end at index 3, got %d", idx)
	}
}

func TestFindMatchingEndNotFound(t *testing.T) {
	ops := []WasmOp{
		NewI32Const(1),
		NewI32Const(2),
	}
	idx := findMatchingEnd(ops, 0)
	if idx != -1 {
		t.Fatalf("expected -1 for no matching end, got %d", idx)
	}
}

// --- Benchmark ---

func BenchmarkConstantFolding(b *testing.B) {
	ops := make([]WasmOp, 0, 300)
	for i := 0; i < 100; i++ {
		ops = append(ops,
			NewI32Const(uint32(i)),
			NewI32Const(uint32(i+1)),
			WasmOp{Opcode: WasmI32Add},
		)
	}
	pass := &ConstantFolding{}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pass.Apply(ops)
	}
}

func BenchmarkDeadCodeElimination(b *testing.B) {
	ops := []WasmOp{
		NewI32Const(1),
		{Opcode: WasmReturn},
	}
	for i := 0; i < 100; i++ {
		ops = append(ops, NewI32Const(uint32(i)))
	}
	pass := &DeadCodeElimination{}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pass.Apply(ops)
	}
}

func BenchmarkFullPipeline(b *testing.B) {
	ops := make([]WasmOp, 0, 400)
	for i := 0; i < 50; i++ {
		ops = append(ops,
			NewI32Const(uint32(i)),
			NewI32Const(uint32(i+1)),
			WasmOp{Opcode: WasmI32Add},
		)
	}
	ops = append(ops, WasmOp{Opcode: WasmReturn})
	for i := 0; i < 50; i++ {
		ops = append(ops, NewI32Const(uint32(i)))
	}
	pipeline := DefaultOptimizationPipeline()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pipeline.Apply(ops)
	}
}
