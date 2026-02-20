package vm

import (
	"testing"
	"time"
)

func TestGasMeterRecordOpExecution(t *testing.T) {
	gm := NewGasMeter(DefaultMeteringPolicy())

	gm.RecordOpExecution(ADD, 3, 100*time.Nanosecond)
	gm.RecordOpExecution(ADD, 3, 120*time.Nanosecond)
	gm.RecordOpExecution(SLOAD, 2100, 500*time.Nanosecond)

	if got := gm.ExecutionCount(); got != 3 {
		t.Fatalf("expected 3 executions, got %d", got)
	}
}

func TestGasMeterProfile(t *testing.T) {
	gm := NewGasMeter(DefaultMeteringPolicy())

	// Record compute ops.
	gm.RecordOpExecution(ADD, 3, 10*time.Nanosecond)
	gm.RecordOpExecution(MUL, 5, 15*time.Nanosecond)

	// Record storage ops.
	gm.RecordOpExecution(SLOAD, 2100, 500*time.Nanosecond)
	gm.RecordOpExecution(SSTORE, 5000, 800*time.Nanosecond)

	// Record call ops.
	gm.RecordOpExecution(CALL, 2600, 1000*time.Nanosecond)

	// Record memory ops.
	gm.RecordOpExecution(MLOAD, 3, 5*time.Nanosecond)
	gm.RecordOpExecution(MSTORE, 3, 5*time.Nanosecond)

	// Record system ops.
	gm.RecordOpExecution(JUMPDEST, 1, 2*time.Nanosecond)
	gm.RecordOpExecution(COINBASE, 2, 3*time.Nanosecond)

	profile := gm.AnalyzeGasUsage()

	if profile.ComputeGas != 8 {
		t.Errorf("ComputeGas: expected 8, got %d", profile.ComputeGas)
	}
	if profile.StorageGas != 7100 {
		t.Errorf("StorageGas: expected 7100, got %d", profile.StorageGas)
	}
	if profile.CallGas != 2600 {
		t.Errorf("CallGas: expected 2600, got %d", profile.CallGas)
	}
	if profile.MemoryGas != 6 {
		t.Errorf("MemoryGas: expected 6, got %d", profile.MemoryGas)
	}
	if profile.SystemGas != 3 {
		t.Errorf("SystemGas: expected 3, got %d", profile.SystemGas)
	}
	expectedTotal := uint64(8 + 7100 + 2600 + 6 + 3)
	if profile.TotalGas != expectedTotal {
		t.Errorf("TotalGas: expected %d, got %d", expectedTotal, profile.TotalGas)
	}
	if profile.TotalOps != 9 {
		t.Errorf("TotalOps: expected 9, got %d", profile.TotalOps)
	}
	if profile.TotalDuration == 0 {
		t.Error("TotalDuration should be non-zero")
	}
}

func TestGasMeterReset(t *testing.T) {
	gm := NewGasMeter(DefaultMeteringPolicy())
	gm.RecordOpExecution(ADD, 3, time.Nanosecond)
	gm.RecordOpExecution(MUL, 5, time.Nanosecond)

	gm.Reset()

	if got := gm.ExecutionCount(); got != 0 {
		t.Fatalf("after Reset, expected 0 executions, got %d", got)
	}

	profile := gm.AnalyzeGasUsage()
	if profile.TotalGas != 0 {
		t.Errorf("after Reset, TotalGas should be 0, got %d", profile.TotalGas)
	}
}

func TestHotspotDetector(t *testing.T) {
	gm := NewGasMeter(DefaultMeteringPolicy())

	// SLOAD is the most expensive.
	for i := 0; i < 10; i++ {
		gm.RecordOpExecution(SLOAD, 2100, 500*time.Nanosecond)
	}
	// ADD is frequent but cheap.
	for i := 0; i < 100; i++ {
		gm.RecordOpExecution(ADD, 3, 10*time.Nanosecond)
	}
	// Single expensive CALL.
	gm.RecordOpExecution(CALL, 2600, 1000*time.Nanosecond)

	detector := NewHotspotDetector(3)
	hotspots := detector.Detect(gm)

	if len(hotspots) != 3 {
		t.Fatalf("expected 3 hotspots, got %d", len(hotspots))
	}

	// Sorted by total gas descending.
	// SLOAD: 10 * 2100 = 21000
	// CALL:  1 * 2600  = 2600
	// ADD:   100 * 3   = 300
	if hotspots[0].Op != SLOAD {
		t.Errorf("hotspot[0]: expected SLOAD, got %v", hotspots[0].Op)
	}
	if hotspots[0].TotalGas != 21000 {
		t.Errorf("hotspot[0] TotalGas: expected 21000, got %d", hotspots[0].TotalGas)
	}
	if hotspots[0].Count != 10 {
		t.Errorf("hotspot[0] Count: expected 10, got %d", hotspots[0].Count)
	}
	if hotspots[0].AvgGas != 2100 {
		t.Errorf("hotspot[0] AvgGas: expected 2100, got %d", hotspots[0].AvgGas)
	}

	if hotspots[1].Op != CALL {
		t.Errorf("hotspot[1]: expected CALL, got %v", hotspots[1].Op)
	}
	if hotspots[2].Op != ADD {
		t.Errorf("hotspot[2]: expected ADD, got %v", hotspots[2].Op)
	}
}

func TestHotspotDetectorExceedsPolicy(t *testing.T) {
	policy := MeteringPolicy{
		MaxGasPerOp:     1000,
		WarmStorageCost: WarmStorageReadCost,
		ColdStorageCost: ColdSloadCost,
	}
	gm := NewGasMeter(policy)

	// SLOAD avg = 2100, exceeds 1000.
	gm.RecordOpExecution(SLOAD, 2100, 500*time.Nanosecond)
	// ADD avg = 3, does not exceed 1000.
	gm.RecordOpExecution(ADD, 3, 10*time.Nanosecond)

	detector := NewHotspotDetector(10)
	flagged := detector.ExceedsPolicy(gm)

	if len(flagged) != 1 {
		t.Fatalf("expected 1 flagged hotspot, got %d", len(flagged))
	}
	if flagged[0].Op != SLOAD {
		t.Errorf("expected SLOAD flagged, got %v", flagged[0].Op)
	}
}

func TestHotspotDetectorUnlimitedPolicy(t *testing.T) {
	gm := NewGasMeter(DefaultMeteringPolicy()) // MaxGasPerOp = 0
	gm.RecordOpExecution(SLOAD, 2100, time.Nanosecond)

	detector := NewHotspotDetector(10)
	flagged := detector.ExceedsPolicy(gm)

	if flagged != nil {
		t.Errorf("expected nil flagged with unlimited policy, got %d", len(flagged))
	}
}

func TestGasPredictionEmpty(t *testing.T) {
	gas := PredictGas(nil)
	if gas != 0 {
		t.Errorf("expected 0 for empty bytecode, got %d", gas)
	}
}

func TestGasPredictionSimple(t *testing.T) {
	// bytecode: PUSH1 0x01 PUSH1 0x02 ADD STOP
	bytecode := []byte{
		byte(PUSH1), 0x01,
		byte(PUSH1), 0x02,
		byte(ADD),
		byte(STOP),
	}

	gas := PredictGas(bytecode)
	// PUSH1 = 3, PUSH1 = 3, ADD = 3, STOP = 0
	expected := uint64(9)
	if gas != expected {
		t.Errorf("expected %d, got %d", expected, gas)
	}
}

func TestGasPredictionWithStorage(t *testing.T) {
	// PUSH1 0x00 SLOAD PUSH1 0x01 SSTORE
	bytecode := []byte{
		byte(PUSH1), 0x00,
		byte(SLOAD),
		byte(PUSH1), 0x01,
		byte(SSTORE),
	}

	gas := PredictGas(bytecode)
	// PUSH1(3) + SLOAD(2100) + PUSH1(3) + SSTORE(2900)
	expected := uint64(5006)
	if gas != expected {
		t.Errorf("expected %d, got %d", expected, gas)
	}
}

func TestOpCodeCategories(t *testing.T) {
	tests := []struct {
		op       OpCode
		expected OpCategory
	}{
		{ADD, CategoryCompute},
		{MUL, CategoryCompute},
		{EXP, CategoryCompute},
		{KECCAK256, CategoryCompute},
		{CLZ, CategoryCompute},
		{SHL, CategoryCompute},
		{SLOAD, CategoryStorage},
		{SSTORE, CategoryStorage},
		{TLOAD, CategoryStorage},
		{TSTORE, CategoryStorage},
		{CALL, CategoryCall},
		{DELEGATECALL, CategoryCall},
		{STATICCALL, CategoryCall},
		{CREATE, CategoryCall},
		{CREATE2, CategoryCall},
		{MLOAD, CategoryMemory},
		{MSTORE, CategoryMemory},
		{MSTORE8, CategoryMemory},
		{MCOPY, CategoryMemory},
		{CALLDATACOPY, CategoryMemory},
		{COINBASE, CategorySystem},
		{JUMPDEST, CategorySystem},
		{PUSH1, CategorySystem},
		{DUP1, CategorySystem},
		{SWAP1, CategorySystem},
		{LOG0, CategorySystem},
		{RETURN, CategorySystem},
	}

	for _, tt := range tests {
		got := ClassifyOpCode(tt.op)
		if got != tt.expected {
			t.Errorf("ClassifyOpCode(%v): expected %v, got %v", tt.op, tt.expected, got)
		}
	}
}

func TestMeteringPolicyDefaults(t *testing.T) {
	policy := DefaultMeteringPolicy()
	if policy.MaxGasPerOp != 0 {
		t.Errorf("default MaxGasPerOp should be 0, got %d", policy.MaxGasPerOp)
	}
	if policy.WarmStorageCost != WarmStorageReadCost {
		t.Errorf("default WarmStorageCost: expected %d, got %d", WarmStorageReadCost, policy.WarmStorageCost)
	}
	if policy.ColdStorageCost != ColdSloadCost {
		t.Errorf("default ColdStorageCost: expected %d, got %d", ColdSloadCost, policy.ColdStorageCost)
	}
}

func TestCategoryBreakdown(t *testing.T) {
	// PUSH1 0x00 SLOAD ADD MSTORE STOP
	bytecode := []byte{
		byte(PUSH1), 0x00,
		byte(SLOAD),
		byte(ADD),
		byte(MSTORE),
		byte(STOP),
	}

	bd := CategoryBreakdown(bytecode)

	if bd[CategorySystem] != 3 { // PUSH1=3, STOP=0
		t.Errorf("system gas: expected 3, got %d", bd[CategorySystem])
	}
	if bd[CategoryStorage] != 2100 { // SLOAD=2100
		t.Errorf("storage gas: expected 2100, got %d", bd[CategoryStorage])
	}
	if bd[CategoryCompute] != 3 { // ADD=3
		t.Errorf("compute gas: expected 3, got %d", bd[CategoryCompute])
	}
	if bd[CategoryMemory] != 3 { // MSTORE=3
		t.Errorf("memory gas: expected 3, got %d", bd[CategoryMemory])
	}
}

func TestOpCategoryString(t *testing.T) {
	if s := CategoryCompute.String(); s != "compute" {
		t.Errorf("expected 'compute', got %q", s)
	}
	if s := OpCategory(99).String(); s != "unknown" {
		t.Errorf("expected 'unknown', got %q", s)
	}
}
