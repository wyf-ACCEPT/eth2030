package rollup

import (
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

func TestExecContextBeginEndCall(t *testing.T) {
	ec := NewExecutionContext(1, 1_000_000, DefaultExecutionContextConfig())

	caller := types.HexToAddress("0x01")
	input := []byte("calldata")

	err := ec.BeginCall(2, caller, input, 500_000)
	if err != nil {
		t.Fatalf("BeginCall: %v", err)
	}
	if ec.CurrentDepth() != 1 {
		t.Errorf("depth = %d, want 1", ec.CurrentDepth())
	}

	err = ec.EndCall(100_000, true, []byte("output"))
	if err != nil {
		t.Fatalf("EndCall: %v", err)
	}
	if ec.CurrentDepth() != 0 {
		t.Errorf("depth after EndCall = %d, want 0", ec.CurrentDepth())
	}
	if ec.GasUsed() != 100_000 {
		t.Errorf("gasUsed = %d, want 100000", ec.GasUsed())
	}
	// 1M - 500K reserved + 400K refunded = 900K
	if ec.GasRemaining() != 900_000 {
		t.Errorf("gasRemaining = %d, want 900000", ec.GasRemaining())
	}
}

func TestExecContextDepthLimit(t *testing.T) {
	config := ExecutionContextConfig{
		MaxCallDepth:  2,
		MaxGasPerExec: 10_000_000,
	}
	ec := NewExecutionContext(1, 10_000_000, config)

	caller := types.HexToAddress("0x01")
	input := []byte("data")

	// Depth 0 -> 1
	if err := ec.BeginCall(2, caller, input, 1000); err != nil {
		t.Fatalf("BeginCall depth 0: %v", err)
	}
	// Depth 1 -> 2
	if err := ec.BeginCall(3, caller, input, 1000); err != nil {
		t.Fatalf("BeginCall depth 1: %v", err)
	}
	// Depth 2 -> 3 should fail (max=2).
	if err := ec.BeginCall(4, caller, input, 1000); err == nil {
		t.Fatal("expected depth limit error, got nil")
	}
}

func TestExecContextGasExhaustion(t *testing.T) {
	ec := NewExecutionContext(1, 1000, DefaultExecutionContextConfig())

	caller := types.HexToAddress("0x01")
	input := []byte("data")

	// Request more gas than available.
	err := ec.BeginCall(2, caller, input, 2000)
	if err == nil {
		t.Fatal("expected gas exhaustion error, got nil")
	}
}

func TestExecContextNilInput(t *testing.T) {
	ec := NewExecutionContext(1, 1_000_000, DefaultExecutionContextConfig())
	caller := types.HexToAddress("0x01")

	err := ec.BeginCall(2, caller, nil, 1000)
	if err == nil {
		t.Fatal("expected nil input error, got nil")
	}
}

func TestExecContextZeroTarget(t *testing.T) {
	ec := NewExecutionContext(1, 1_000_000, DefaultExecutionContextConfig())
	caller := types.HexToAddress("0x01")

	err := ec.BeginCall(0, caller, []byte("data"), 1000)
	if err == nil {
		t.Fatal("expected zero target error, got nil")
	}
}

func TestExecContextFinishAndVerify(t *testing.T) {
	ec := NewExecutionContext(1, 1_000_000, DefaultExecutionContextConfig())

	caller := types.HexToAddress("0x01")
	_ = ec.BeginCall(2, caller, []byte("data"), 500_000)
	_ = ec.EndCall(100_000, true, []byte("result"))

	resultHash, err := ec.Finish()
	if err != nil {
		t.Fatalf("Finish: %v", err)
	}
	if resultHash.IsZero() {
		t.Error("result hash should not be zero")
	}

	// Verify with correct hash.
	valid, err := ec.VerifyResult(resultHash)
	if err != nil {
		t.Fatalf("VerifyResult: %v", err)
	}
	if !valid {
		t.Error("result should be valid")
	}

	// Verify with incorrect hash.
	_, err = ec.VerifyResult(types.HexToHash("0xff"))
	if err == nil {
		t.Error("expected mismatch error for wrong hash")
	}
}

func TestExecContextDoubleFinish(t *testing.T) {
	ec := NewExecutionContext(1, 1_000_000, DefaultExecutionContextConfig())

	_, err := ec.Finish()
	if err != nil {
		t.Fatalf("first Finish: %v", err)
	}

	_, err = ec.Finish()
	if err == nil {
		t.Fatal("expected error on second Finish, got nil")
	}
}

func TestExecContextCallsAfterFinish(t *testing.T) {
	ec := NewExecutionContext(1, 1_000_000, DefaultExecutionContextConfig())
	_, _ = ec.Finish()

	err := ec.BeginCall(2, types.HexToAddress("0x01"), []byte("data"), 1000)
	if err == nil {
		t.Fatal("expected error on BeginCall after Finish")
	}

	err = ec.EndCall(100, true, nil)
	if err == nil {
		t.Fatal("expected error on EndCall after Finish")
	}
}

func TestExecContextVerifyBeforeFinish(t *testing.T) {
	ec := NewExecutionContext(1, 1_000_000, DefaultExecutionContextConfig())

	_, err := ec.VerifyResult(types.HexToHash("0xaa"))
	if err == nil {
		t.Fatal("expected error verifying before Finish")
	}
}

func TestExecContextTrace(t *testing.T) {
	ec := NewExecutionContext(1, 1_000_000, DefaultExecutionContextConfig())

	caller := types.HexToAddress("0x01")
	_ = ec.BeginCall(2, caller, []byte("call1"), 100_000)
	_ = ec.EndCall(50_000, true, []byte("out1"))

	_ = ec.BeginCall(3, caller, []byte("call2"), 200_000)
	_ = ec.EndCall(75_000, false, nil)

	trace := ec.Trace()
	if len(trace) != 2 {
		t.Fatalf("trace length = %d, want 2", len(trace))
	}

	if trace[0].TargetRollupID != 2 {
		t.Errorf("trace[0].TargetRollupID = %d, want 2", trace[0].TargetRollupID)
	}
	if trace[0].GasUsed != 50_000 {
		t.Errorf("trace[0].GasUsed = %d, want 50000", trace[0].GasUsed)
	}
	if !trace[0].Success {
		t.Error("trace[0] should be successful")
	}

	if trace[1].TargetRollupID != 3 {
		t.Errorf("trace[1].TargetRollupID = %d, want 3", trace[1].TargetRollupID)
	}
	if trace[1].Success {
		t.Error("trace[1] should be failed")
	}
}

func TestExecContextNestedCalls(t *testing.T) {
	ec := NewExecutionContext(1, 1_000_000, DefaultExecutionContextConfig())

	caller := types.HexToAddress("0x01")

	// Depth 0 -> 1
	_ = ec.BeginCall(2, caller, []byte("outer"), 500_000)
	// Depth 1 -> 2
	_ = ec.BeginCall(3, caller, []byte("inner"), 100_000)

	if ec.CurrentDepth() != 2 {
		t.Errorf("depth = %d, want 2", ec.CurrentDepth())
	}

	// End inner call.
	_ = ec.EndCall(30_000, true, []byte("inner-out"))
	if ec.CurrentDepth() != 1 {
		t.Errorf("depth = %d, want 1", ec.CurrentDepth())
	}

	// End outer call.
	_ = ec.EndCall(200_000, true, []byte("outer-out"))
	if ec.CurrentDepth() != 0 {
		t.Errorf("depth = %d, want 0", ec.CurrentDepth())
	}

	if ec.TraceLength() != 2 {
		t.Errorf("trace length = %d, want 2", ec.TraceLength())
	}
}

func TestExecContextGasRefund(t *testing.T) {
	ec := NewExecutionContext(1, 1_000_000, DefaultExecutionContextConfig())

	caller := types.HexToAddress("0x01")
	_ = ec.BeginCall(2, caller, []byte("data"), 600_000)

	// After BeginCall: remaining = 1M - 600K = 400K.
	if ec.GasRemaining() != 400_000 {
		t.Errorf("gasRemaining after begin = %d, want 400000", ec.GasRemaining())
	}

	// Use only 200K of the 600K provisioned.
	_ = ec.EndCall(200_000, true, nil)

	// After EndCall: remaining = 400K + (600K - 200K) refund = 800K.
	if ec.GasRemaining() != 800_000 {
		t.Errorf("gasRemaining after end = %d, want 800000", ec.GasRemaining())
	}
	if ec.GasUsed() != 200_000 {
		t.Errorf("gasUsed = %d, want 200000", ec.GasUsed())
	}
}

func TestExecContextGasBudgetCap(t *testing.T) {
	config := ExecutionContextConfig{
		MaxCallDepth:  32,
		MaxGasPerExec: 5000,
	}
	// Request 10000 but MaxGasPerExec is 5000: should be capped.
	ec := NewExecutionContext(1, 10000, config)
	if ec.GasRemaining() != 5000 {
		t.Errorf("gasRemaining = %d, want 5000 (capped)", ec.GasRemaining())
	}
}

func TestExecContextRollupID(t *testing.T) {
	ec := NewExecutionContext(42, 1_000_000, DefaultExecutionContextConfig())
	if ec.RollupID() != 42 {
		t.Errorf("RollupID = %d, want 42", ec.RollupID())
	}
}

func TestExecContextIsFinished(t *testing.T) {
	ec := NewExecutionContext(1, 1_000_000, DefaultExecutionContextConfig())
	if ec.IsFinished() {
		t.Error("should not be finished initially")
	}
	_, _ = ec.Finish()
	if !ec.IsFinished() {
		t.Error("should be finished after Finish()")
	}
}

func TestExecContextEndCallCapsGasUsed(t *testing.T) {
	ec := NewExecutionContext(1, 1_000_000, DefaultExecutionContextConfig())
	caller := types.HexToAddress("0x01")

	_ = ec.BeginCall(2, caller, []byte("data"), 100_000)

	// Report more gas used than was provided; should be capped.
	_ = ec.EndCall(999_999, true, nil)

	if ec.GasUsed() != 100_000 {
		t.Errorf("gasUsed = %d, want 100000 (capped)", ec.GasUsed())
	}
}

func TestExecContextDeterministicResultHash(t *testing.T) {
	// Two identical execution contexts should produce the same result hash.
	makeCtx := func() *ExecutionContext {
		ec := NewExecutionContext(1, 1_000_000, DefaultExecutionContextConfig())
		caller := types.HexToAddress("0x01")
		_ = ec.BeginCall(2, caller, []byte("data"), 500_000)
		_ = ec.EndCall(100_000, true, []byte("output"))
		return ec
	}

	h1, _ := makeCtx().Finish()
	h2, _ := makeCtx().Finish()

	if h1 != h2 {
		t.Errorf("result hashes differ: %s vs %s", h1.Hex(), h2.Hex())
	}
}
