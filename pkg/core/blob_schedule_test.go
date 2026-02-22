package core

import (
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

func TestBlobScheduleConstants(t *testing.T) {
	// Verify pre-defined blob schedule values match reference.
	tests := []struct {
		name           string
		sched          BlobSchedule
		target, max    uint64
		updateFraction uint64
	}{
		{"Cancun", CancunBlobSchedule, 3, 6, 3338477},
		{"Prague", PragueBlobSchedule, 6, 9, 5376681},
		{"BPO1", BPO1BlobSchedule, 10, 15, 8346193},
		{"BPO2", BPO2BlobSchedule, 14, 21, 11684671},
	}
	for _, tt := range tests {
		if tt.sched.Target != tt.target {
			t.Errorf("%s: Target = %d, want %d", tt.name, tt.sched.Target, tt.target)
		}
		if tt.sched.Max != tt.max {
			t.Errorf("%s: Max = %d, want %d", tt.name, tt.sched.Max, tt.max)
		}
		if tt.sched.UpdateFraction != tt.updateFraction {
			t.Errorf("%s: UpdateFraction = %d, want %d", tt.name, tt.sched.UpdateFraction, tt.updateFraction)
		}
	}
}

func TestGetBlobSchedule_ForkTransitions(t *testing.T) {
	config := &ChainConfig{
		ChainID:                 big.NewInt(1337),
		HomesteadBlock:          big.NewInt(0),
		EIP150Block:             big.NewInt(0),
		EIP155Block:             big.NewInt(0),
		EIP158Block:             big.NewInt(0),
		ByzantiumBlock:          big.NewInt(0),
		ConstantinopleBlock:     big.NewInt(0),
		PetersburgBlock:         big.NewInt(0),
		IstanbulBlock:           big.NewInt(0),
		BerlinBlock:             big.NewInt(0),
		LondonBlock:             big.NewInt(0),
		TerminalTotalDifficulty: big.NewInt(0),
		ShanghaiTime:            newUint64(0),
		CancunTime:              newUint64(100),
		PragueTime:              newUint64(200),
		BPO1Time:                newUint64(300),
		BPO2Time:                newUint64(400),
	}

	tests := []struct {
		time   uint64
		target uint64
		max    uint64
	}{
		{50, 3, 6},    // pre-Prague, Cancun schedule
		{100, 3, 6},   // Cancun activated
		{199, 3, 6},   // still Cancun
		{200, 6, 9},   // Prague activated
		{299, 6, 9},   // still Prague
		{300, 10, 15}, // BPO1 activated
		{399, 10, 15}, // still BPO1
		{400, 14, 21}, // BPO2 activated
		{999, 14, 21}, // still BPO2
	}
	for _, tt := range tests {
		sched := GetBlobSchedule(config, tt.time)
		if sched.Target != tt.target {
			t.Errorf("time=%d: Target = %d, want %d", tt.time, sched.Target, tt.target)
		}
		if sched.Max != tt.max {
			t.Errorf("time=%d: Max = %d, want %d", tt.time, sched.Max, tt.max)
		}
	}
}

func TestMaxBlobsForBlock(t *testing.T) {
	config := &ChainConfig{
		ChainID:                 big.NewInt(1337),
		HomesteadBlock:          big.NewInt(0),
		EIP150Block:             big.NewInt(0),
		EIP155Block:             big.NewInt(0),
		EIP158Block:             big.NewInt(0),
		ByzantiumBlock:          big.NewInt(0),
		ConstantinopleBlock:     big.NewInt(0),
		PetersburgBlock:         big.NewInt(0),
		IstanbulBlock:           big.NewInt(0),
		BerlinBlock:             big.NewInt(0),
		LondonBlock:             big.NewInt(0),
		TerminalTotalDifficulty: big.NewInt(0),
		ShanghaiTime:            newUint64(0),
		CancunTime:              newUint64(0),
		PragueTime:              newUint64(100),
		BPO1Time:                newUint64(200),
		BPO2Time:                newUint64(300),
	}

	if got := MaxBlobsForBlock(config, 50); got != 6 {
		t.Errorf("Cancun MaxBlobs = %d, want 6", got)
	}
	if got := MaxBlobsForBlock(config, 100); got != 9 {
		t.Errorf("Prague MaxBlobs = %d, want 9", got)
	}
	if got := MaxBlobsForBlock(config, 200); got != 15 {
		t.Errorf("BPO1 MaxBlobs = %d, want 15", got)
	}
	if got := MaxBlobsForBlock(config, 300); got != 21 {
		t.Errorf("BPO2 MaxBlobs = %d, want 21", got)
	}
}

func TestTargetBlobsForBlock(t *testing.T) {
	config := &ChainConfig{
		ChainID:                 big.NewInt(1337),
		HomesteadBlock:          big.NewInt(0),
		EIP150Block:             big.NewInt(0),
		EIP155Block:             big.NewInt(0),
		EIP158Block:             big.NewInt(0),
		ByzantiumBlock:          big.NewInt(0),
		ConstantinopleBlock:     big.NewInt(0),
		PetersburgBlock:         big.NewInt(0),
		IstanbulBlock:           big.NewInt(0),
		BerlinBlock:             big.NewInt(0),
		LondonBlock:             big.NewInt(0),
		TerminalTotalDifficulty: big.NewInt(0),
		ShanghaiTime:            newUint64(0),
		CancunTime:              newUint64(0),
		PragueTime:              newUint64(100),
		BPO1Time:                newUint64(200),
		BPO2Time:                newUint64(300),
	}

	if got := TargetBlobsForBlock(config, 50); got != 3 {
		t.Errorf("Cancun TargetBlobs = %d, want 3", got)
	}
	if got := TargetBlobsForBlock(config, 100); got != 6 {
		t.Errorf("Prague TargetBlobs = %d, want 6", got)
	}
	if got := TargetBlobsForBlock(config, 200); got != 10 {
		t.Errorf("BPO1 TargetBlobs = %d, want 10", got)
	}
	if got := TargetBlobsForBlock(config, 300); got != 14 {
		t.Errorf("BPO2 TargetBlobs = %d, want 14", got)
	}
}

func TestCalcExcessBlobGasV2WithSchedule_BPO1(t *testing.T) {
	sched := BPO1BlobSchedule
	targetGas := sched.Target * GasPerBlob
	maxGas := sched.Max * GasPerBlob

	// Below target -> 0.
	if got := CalcExcessBlobGasV2WithSchedule(0, 0, big.NewInt(1), sched); got != 0 {
		t.Errorf("below target: got %d, want 0", got)
	}

	// Exactly at target -> 0.
	if got := CalcExcessBlobGasV2WithSchedule(0, targetGas, big.NewInt(1), sched); got != 0 {
		t.Errorf("at target: got %d, want 0", got)
	}

	// Full max blobs from zero excess (normal mode with low base fee).
	got := CalcExcessBlobGasV2WithSchedule(0, maxGas, big.NewInt(1), sched)
	expected := maxGas - targetGas
	if got != expected {
		t.Errorf("full max: got %d, want %d", got, expected)
	}
}

func TestCalcExcessBlobGasV2WithSchedule_BPO2(t *testing.T) {
	sched := BPO2BlobSchedule
	targetGas := sched.Target * GasPerBlob
	maxGas := sched.Max * GasPerBlob

	// Below target -> 0.
	if got := CalcExcessBlobGasV2WithSchedule(0, 0, big.NewInt(1), sched); got != 0 {
		t.Errorf("below target: got %d, want 0", got)
	}

	// Full max from zero excess.
	got := CalcExcessBlobGasV2WithSchedule(0, maxGas, big.NewInt(1), sched)
	expected := maxGas - targetGas
	if got != expected {
		t.Errorf("full max: got %d, want %d", got, expected)
	}
}

func TestCalcExcessBlobGasV2WithSchedule_ExecutionFeeLed(t *testing.T) {
	sched := BPO1BlobSchedule
	maxGas := sched.Max * GasPerBlob
	targetGas := sched.Target * GasPerBlob
	highBaseFee := big.NewInt(10_000_000_000)

	got := CalcExcessBlobGasV2WithSchedule(targetGas, maxGas, highBaseFee, sched)
	increase := maxGas * (sched.Max - sched.Target) / sched.Max
	expected := targetGas + increase

	if got != expected {
		t.Errorf("execution-fee-led BPO1: got %d, want %d", got, expected)
	}
}

func TestCalcBlobBaseFeeV2WithFraction(t *testing.T) {
	// With zero excess, fee should be MinBaseFeePerBlobGas regardless of fraction.
	for _, frac := range []uint64{3338477, 5376681, 8346193, 11684671} {
		fee := CalcBlobBaseFeeV2WithFraction(0, big.NewInt(0), frac)
		expected := big.NewInt(MinBaseFeePerBlobGas)
		if fee.Cmp(expected) != 0 {
			t.Errorf("fraction=%d: CalcBlobBaseFeeV2WithFraction(0, 0) = %s, want %s", frac, fee, expected)
		}
	}
}

// Test EIP-7742: header target override.
func TestCalcExcessBlobGasV2ForHeader_WithTargetOverride(t *testing.T) {
	// Set up a parent header with explicit TargetBlobsPerBlock = 4.
	target := uint64(4)
	excess := uint64(0)
	used := uint64(6 * GasPerBlob) // 6 blobs

	parent := &types.Header{
		BaseFee:             big.NewInt(1),
		ExcessBlobGas:       &excess,
		BlobGasUsed:         &used,
		TargetBlobsPerBlock: &target,
	}

	config := &ChainConfig{
		ChainID:                 big.NewInt(1337),
		HomesteadBlock:          big.NewInt(0),
		EIP150Block:             big.NewInt(0),
		EIP155Block:             big.NewInt(0),
		EIP158Block:             big.NewInt(0),
		ByzantiumBlock:          big.NewInt(0),
		ConstantinopleBlock:     big.NewInt(0),
		PetersburgBlock:         big.NewInt(0),
		IstanbulBlock:           big.NewInt(0),
		BerlinBlock:             big.NewInt(0),
		LondonBlock:             big.NewInt(0),
		TerminalTotalDifficulty: big.NewInt(0),
		ShanghaiTime:            newUint64(0),
		CancunTime:              newUint64(0),
		PragueTime:              newUint64(0),
	}

	result := CalcExcessBlobGasV2ForHeader(parent, config, 100)

	// With target=4, target gas = 4 * GasPerBlob = 524288.
	// excess + used = 0 + 786432 = 786432 > 524288
	// Normal mode (low base fee): 786432 - 524288 = 262144
	expectedTargetGas := target * GasPerBlob
	expectedExcess := used - expectedTargetGas
	if result != expectedExcess {
		t.Errorf("header target override: got %d, want %d", result, expectedExcess)
	}
}

func TestCalcExcessBlobGasV2ForHeader_WithoutTarget(t *testing.T) {
	// No target in header -> uses fork schedule.
	excess := uint64(0)
	used := uint64(9 * GasPerBlob) // max Prague blobs

	parent := &types.Header{
		BaseFee:       big.NewInt(1),
		ExcessBlobGas: &excess,
		BlobGasUsed:   &used,
	}

	config := &ChainConfig{
		ChainID:                 big.NewInt(1337),
		HomesteadBlock:          big.NewInt(0),
		EIP150Block:             big.NewInt(0),
		EIP155Block:             big.NewInt(0),
		EIP158Block:             big.NewInt(0),
		ByzantiumBlock:          big.NewInt(0),
		ConstantinopleBlock:     big.NewInt(0),
		PetersburgBlock:         big.NewInt(0),
		IstanbulBlock:           big.NewInt(0),
		BerlinBlock:             big.NewInt(0),
		LondonBlock:             big.NewInt(0),
		TerminalTotalDifficulty: big.NewInt(0),
		ShanghaiTime:            newUint64(0),
		CancunTime:              newUint64(0),
		PragueTime:              newUint64(0),
	}

	result := CalcExcessBlobGasV2ForHeader(parent, config, 100)

	// Prague schedule: target=6, so target gas = 786432.
	// excess + used = 0 + 1179648 = 1179648 > 786432
	// Normal: 1179648 - 786432 = 393216
	expected := used - PragueBlobSchedule.Target*GasPerBlob
	if result != expected {
		t.Errorf("no header target: got %d, want %d", result, expected)
	}
}

func TestCalcExcessBlobGasV2ForHeader_NilParentFields(t *testing.T) {
	parent := &types.Header{
		BaseFee: big.NewInt(1),
	}

	config := &ChainConfig{
		ChainID:                 big.NewInt(1337),
		HomesteadBlock:          big.NewInt(0),
		EIP150Block:             big.NewInt(0),
		EIP155Block:             big.NewInt(0),
		EIP158Block:             big.NewInt(0),
		ByzantiumBlock:          big.NewInt(0),
		ConstantinopleBlock:     big.NewInt(0),
		PetersburgBlock:         big.NewInt(0),
		IstanbulBlock:           big.NewInt(0),
		BerlinBlock:             big.NewInt(0),
		LondonBlock:             big.NewInt(0),
		TerminalTotalDifficulty: big.NewInt(0),
		ShanghaiTime:            newUint64(0),
		CancunTime:              newUint64(0),
		PragueTime:              newUint64(0),
	}

	result := CalcExcessBlobGasV2ForHeader(parent, config, 100)
	if result != 0 {
		t.Errorf("nil parent fields: got %d, want 0", result)
	}
}

func TestValidateBlockBlobGasWithConfig(t *testing.T) {
	config := &ChainConfig{
		ChainID:                 big.NewInt(1337),
		HomesteadBlock:          big.NewInt(0),
		EIP150Block:             big.NewInt(0),
		EIP155Block:             big.NewInt(0),
		EIP158Block:             big.NewInt(0),
		ByzantiumBlock:          big.NewInt(0),
		ConstantinopleBlock:     big.NewInt(0),
		PetersburgBlock:         big.NewInt(0),
		IstanbulBlock:           big.NewInt(0),
		BerlinBlock:             big.NewInt(0),
		LondonBlock:             big.NewInt(0),
		TerminalTotalDifficulty: big.NewInt(0),
		ShanghaiTime:            newUint64(0),
		CancunTime:              newUint64(0),
		PragueTime:              newUint64(0),
		BPO1Time:                newUint64(200),
	}

	// Parent at BPO1 time.
	parentExcess := uint64(0)
	parentUsed := uint64(10 * GasPerBlob) // 10 blobs (BPO1 target)
	parent := &types.Header{
		Time:          200,
		BaseFee:       big.NewInt(1),
		ExcessBlobGas: &parentExcess,
		BlobGasUsed:   &parentUsed,
	}

	expectedExcess := CalcExcessBlobGasV2ForHeader(parent, config, 201)
	childUsed := uint64(5 * GasPerBlob)
	child := &types.Header{
		Time:          201,
		BaseFee:       big.NewInt(1),
		ExcessBlobGas: &expectedExcess,
		BlobGasUsed:   &childUsed,
	}

	if err := ValidateBlockBlobGasWithConfig(config, child, parent); err != nil {
		t.Fatalf("valid block failed: %v", err)
	}

	// Test with excess that's too many blobs.
	tooMuch := uint64(16 * GasPerBlob) // > 15 max for BPO1
	badChild := &types.Header{
		Time:          201,
		BaseFee:       big.NewInt(1),
		ExcessBlobGas: &expectedExcess,
		BlobGasUsed:   &tooMuch,
	}
	if err := ValidateBlockBlobGasWithConfig(config, badChild, parent); err == nil {
		t.Fatal("expected error for exceeding BPO1 max blob gas")
	}
}

func TestGetBlobSchedule_NoBPOForks(t *testing.T) {
	config := &ChainConfig{
		ChainID:                 big.NewInt(1337),
		HomesteadBlock:          big.NewInt(0),
		EIP150Block:             big.NewInt(0),
		EIP155Block:             big.NewInt(0),
		EIP158Block:             big.NewInt(0),
		ByzantiumBlock:          big.NewInt(0),
		ConstantinopleBlock:     big.NewInt(0),
		PetersburgBlock:         big.NewInt(0),
		IstanbulBlock:           big.NewInt(0),
		BerlinBlock:             big.NewInt(0),
		LondonBlock:             big.NewInt(0),
		TerminalTotalDifficulty: big.NewInt(0),
		ShanghaiTime:            newUint64(0),
		CancunTime:              newUint64(0),
		PragueTime:              newUint64(0),
	}

	// Without BPO forks, should always return Prague schedule.
	sched := GetBlobSchedule(config, 999999)
	if sched.Target != 6 || sched.Max != 9 {
		t.Errorf("no BPO: Target=%d, Max=%d, want 6/9", sched.Target, sched.Max)
	}
}

func TestIsBPO1_IsBPO2(t *testing.T) {
	config := &ChainConfig{
		BPO1Time: newUint64(100),
		BPO2Time: newUint64(200),
	}

	if config.IsBPO1(99) {
		t.Error("IsBPO1(99) should be false")
	}
	if !config.IsBPO1(100) {
		t.Error("IsBPO1(100) should be true")
	}
	if !config.IsBPO1(200) {
		t.Error("IsBPO1(200) should be true")
	}

	if config.IsBPO2(199) {
		t.Error("IsBPO2(199) should be false")
	}
	if !config.IsBPO2(200) {
		t.Error("IsBPO2(200) should be true")
	}
}

func TestValidateBlobTxWithMax(t *testing.T) {
	makeHash := func(version byte) types.Hash {
		var h types.Hash
		h[0] = version
		return h
	}

	// 10 blobs with BPO1 max (15) should pass.
	hashes := make([]types.Hash, 10)
	for i := range hashes {
		hashes[i] = makeHash(BlobTxHashVersion)
	}
	tx := types.NewTransaction(&types.BlobTx{
		BlobHashes: hashes,
		BlobFeeCap: big.NewInt(1),
	})
	if err := ValidateBlobTxWithMax(tx, 0, 15); err != nil {
		t.Fatalf("10 blobs with max=15 should pass: %v", err)
	}

	// 16 blobs with BPO1 max (15) should fail.
	hashes = make([]types.Hash, 16)
	for i := range hashes {
		hashes[i] = makeHash(BlobTxHashVersion)
	}
	tx = types.NewTransaction(&types.BlobTx{
		BlobHashes: hashes,
		BlobFeeCap: big.NewInt(1),
	})
	if err := ValidateBlobTxWithMax(tx, 0, 15); err == nil {
		t.Fatal("16 blobs with max=15 should fail")
	}
}
