package core

import (
	"math/big"
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

func TestHogotaForkConfig(t *testing.T) {
	// TestConfigHogota should have Hogota active at genesis.
	if !TestConfigHogota.IsHogota(0) {
		t.Fatal("TestConfigHogota.IsHogota(0) should be true")
	}
	if !TestConfigHogota.IsHogota(100) {
		t.Fatal("TestConfigHogota.IsHogota(100) should be true")
	}

	// Hogota implies Glamsterdan is also active.
	if !TestConfigHogota.IsGlamsterdan(0) {
		t.Fatal("TestConfigHogota should also have Glamsterdan active")
	}

	// EIP-7999 should be active when Hogota is active.
	if !TestConfigHogota.IsEIP7999(0) {
		t.Fatal("EIP-7999 should be active with Hogota")
	}

	// TestConfig (pre-Glamsterdan) should NOT have Hogota.
	if TestConfig.IsHogota(0) {
		t.Fatal("TestConfig should not have Hogota active")
	}

	// TestConfigGlamsterdan should NOT have Hogota.
	if TestConfigGlamsterdan.IsHogota(0) {
		t.Fatal("TestConfigGlamsterdan should not have Hogota active")
	}

	// MainnetConfig should NOT have Hogota (nil).
	if MainnetConfig.IsHogota(0) {
		t.Fatal("MainnetConfig should not have Hogota active")
	}

	// Config with a future HogotaTime should not be active before it.
	futureConfig := &ChainConfig{
		ChainID:    big.NewInt(1),
		HogotaTime: newUint64(1000),
	}
	if futureConfig.IsHogota(999) {
		t.Fatal("IsHogota should be false before activation time")
	}
	if !futureConfig.IsHogota(1000) {
		t.Fatal("IsHogota should be true at activation time")
	}
	if !futureConfig.IsHogota(2000) {
		t.Fatal("IsHogota should be true after activation time")
	}

	// Rules should include IsHogota and IsEIP7999.
	rules := TestConfigHogota.Rules(big.NewInt(1), true, 0)
	if !rules.IsHogota {
		t.Fatal("Rules.IsHogota should be true for TestConfigHogota")
	}
	if !rules.IsEIP7999 {
		t.Fatal("Rules.IsEIP7999 should be true for TestConfigHogota")
	}

	// Rules for pre-Hogota config should have false.
	rules2 := TestConfigGlamsterdan.Rules(big.NewInt(1), true, 0)
	if rules2.IsHogota {
		t.Fatal("Rules.IsHogota should be false for TestConfigGlamsterdan")
	}
}

func TestCalcCalldataBaseFeeMultidim(t *testing.T) {
	// Zero excess should yield the minimum base fee.
	excess := uint64(0)
	used := uint64(0)
	parent := &types.Header{
		GasLimit:          30_000_000,
		CalldataGasUsed:   &used,
		CalldataExcessGas: &excess,
	}
	fee := CalcCalldataBaseFeeFromHeader(parent)
	if fee.Cmp(big.NewInt(MinCalldataBaseFee)) != 0 {
		t.Fatalf("expected min fee %d, got %s", MinCalldataBaseFee, fee)
	}

	// Large excess should produce a higher fee.
	bigExcess := uint64(50_000_000)
	parent2 := &types.Header{
		GasLimit:          30_000_000,
		CalldataGasUsed:   &used,
		CalldataExcessGas: &bigExcess,
	}
	fee2 := CalcCalldataBaseFeeFromHeader(parent2)
	if fee2.Cmp(fee) <= 0 {
		t.Fatalf("larger excess should yield higher fee: got %s vs %s", fee2, fee)
	}

	// Even larger excess should yield even higher fee (monotonicity).
	hugeExcess := uint64(200_000_000)
	parent3 := &types.Header{
		GasLimit:          30_000_000,
		CalldataGasUsed:   &used,
		CalldataExcessGas: &hugeExcess,
	}
	fee3 := CalcCalldataBaseFeeFromHeader(parent3)
	if fee3.Cmp(fee2) <= 0 {
		t.Fatalf("even larger excess should yield even higher fee: got %s vs %s", fee3, fee2)
	}
}

func TestCalcCalldataGasUsedMultidim(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want uint64
	}{
		{"nil data", nil, 0},
		{"empty data", []byte{}, 0},
		{"single zero byte", []byte{0x00}, 4},          // 1 token * 4 gas
		{"single nonzero byte", []byte{0xff}, 16},       // 4 tokens * 4 gas
		{"mixed bytes", []byte{0x00, 0xff, 0x00, 0xab}, 40}, // (2*1 + 2*4) * 4 = 40
		{
			"all zeros 100 bytes",
			make([]byte, 100),
			400, // 100 * 1 * 4
		},
		{
			"all nonzero 10 bytes",
			[]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10},
			160, // 10 * 4 * 4
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CalcCalldataGasUsed(tt.data)
			if got != tt.want {
				t.Errorf("CalcCalldataGasUsed(%x) = %d, want %d", tt.data, got, tt.want)
			}
		})
	}
}

func TestCalldataGasValidation(t *testing.T) {
	gasLimit := uint64(30_000_000)
	zeroExcess := uint64(0)
	zeroUsed := uint64(0)

	parent := &types.Header{
		GasLimit:          gasLimit,
		CalldataGasUsed:   &zeroUsed,
		CalldataExcessGas: &zeroExcess,
	}

	// Valid child header.
	childExcess := CalcCalldataExcessGas(0, 0, gasLimit)
	childUsed := uint64(500)
	validChild := &types.Header{
		GasLimit:          gasLimit,
		CalldataGasUsed:   &childUsed,
		CalldataExcessGas: &childExcess,
	}
	if err := ValidateCalldataGasFields(validChild, parent); err != nil {
		t.Fatalf("valid child should pass: %v", err)
	}

	// Missing CalldataGasUsed should fail.
	if err := ValidateCalldataGasFields(&types.Header{
		GasLimit:          gasLimit,
		CalldataExcessGas: &childExcess,
	}, parent); err == nil {
		t.Fatal("should fail with missing CalldataGasUsed")
	}

	// Missing CalldataExcessGas should fail.
	if err := ValidateCalldataGasFields(&types.Header{
		GasLimit:        gasLimit,
		CalldataGasUsed: &childUsed,
	}, parent); err == nil {
		t.Fatal("should fail with missing CalldataExcessGas")
	}

	// CalldataGasUsed exceeding limit should fail.
	overLimit := CalcCalldataGasLimit(gasLimit) + 1
	if err := ValidateCalldataGasFields(&types.Header{
		GasLimit:          gasLimit,
		CalldataGasUsed:   &overLimit,
		CalldataExcessGas: &childExcess,
	}, parent); err == nil {
		t.Fatal("should fail with calldata gas over limit")
	}

	// Wrong excess should fail.
	wrongExcess := uint64(12345)
	if err := ValidateCalldataGasFields(&types.Header{
		GasLimit:          gasLimit,
		CalldataGasUsed:   &childUsed,
		CalldataExcessGas: &wrongExcess,
	}, parent); err == nil {
		t.Fatal("should fail with wrong excess gas")
	}

	// Excess accumulation scenario: parent used above target.
	parentUsedHigh := uint64(3_000_000) // above target of 1,875,000
	parentWithUsage := &types.Header{
		GasLimit:          gasLimit,
		CalldataGasUsed:   &parentUsedHigh,
		CalldataExcessGas: &zeroExcess,
	}
	expectedExcess := CalcCalldataExcessGas(0, parentUsedHigh, gasLimit)
	if expectedExcess == 0 {
		t.Fatal("expected nonzero excess from high parent usage")
	}
	childValid := &types.Header{
		GasLimit:          gasLimit,
		CalldataGasUsed:   &childUsed,
		CalldataExcessGas: &expectedExcess,
	}
	if err := ValidateCalldataGasFields(childValid, parentWithUsage); err != nil {
		t.Fatalf("valid child with accumulated excess should pass: %v", err)
	}
}

func TestGasDimensionsTracking(t *testing.T) {
	// Test basic GasDimensions arithmetic.
	a := GasDimensions{Compute: 100, Calldata: 50, Blob: 25}
	b := GasDimensions{Compute: 30, Calldata: 20, Blob: 10}

	// Add.
	sum := a.Add(b)
	if sum.Compute != 130 || sum.Calldata != 70 || sum.Blob != 35 {
		t.Fatalf("Add: got %+v, want {130 70 35}", sum)
	}

	// Sub.
	diff := a.Sub(b)
	if diff.Compute != 70 || diff.Calldata != 30 || diff.Blob != 15 {
		t.Fatalf("Sub: got %+v, want {70 30 15}", diff)
	}

	// Sub clamped at zero.
	diff2 := b.Sub(a)
	if diff2.Compute != 0 || diff2.Calldata != 0 || diff2.Blob != 0 {
		t.Fatalf("Sub clamp: got %+v, want {0 0 0}", diff2)
	}

	// FitsWithin.
	limits := GasDimensions{Compute: 200, Calldata: 100, Blob: 50}
	if !a.FitsWithin(limits) {
		t.Fatal("a should fit within limits")
	}
	oversize := GasDimensions{Compute: 201, Calldata: 50, Blob: 25}
	if oversize.FitsWithin(limits) {
		t.Fatal("big should not fit within limits (compute exceeds)")
	}

	// IsZero.
	zero := GasDimensions{}
	if !zero.IsZero() {
		t.Fatal("zero dimensions should be zero")
	}
	if a.IsZero() {
		t.Fatal("non-zero dimensions should not be zero")
	}

	// TotalCost.
	dims := GasDimensions{Compute: 100, Calldata: 50, Blob: 10}
	cost := dims.TotalCost(big.NewInt(10), big.NewInt(5), big.NewInt(2))
	// 100*10 + 50*5 + 10*2 = 1000 + 250 + 20 = 1270
	if cost.Int64() != 1270 {
		t.Fatalf("TotalCost: got %s, want 1270", cost)
	}

	// TotalCost with nil prices.
	cost2 := dims.TotalCost(big.NewInt(10), nil, nil)
	if cost2.Int64() != 1000 {
		t.Fatalf("TotalCost with nil: got %s, want 1000", cost2)
	}
}

func TestCalldataGasPool(t *testing.T) {
	pool := NewCalldataGasPool(1000)
	if pool.Gas() != 1000 {
		t.Fatalf("initial gas: got %d, want 1000", pool.Gas())
	}

	// Subtract some gas.
	if err := pool.SubGas(300); err != nil {
		t.Fatalf("SubGas(300): %v", err)
	}
	if pool.Gas() != 700 {
		t.Fatalf("after sub 300: got %d, want 700", pool.Gas())
	}

	// Add gas back.
	pool.AddGas(100)
	if pool.Gas() != 800 {
		t.Fatalf("after add 100: got %d, want 800", pool.Gas())
	}

	// Subtract more than available should fail.
	if err := pool.SubGas(900); err == nil {
		t.Fatal("SubGas(900) should fail with only 800 available")
	}
	// Gas should remain unchanged after failed subtraction.
	if pool.Gas() != 800 {
		t.Fatalf("after failed sub: got %d, want 800", pool.Gas())
	}

	// Exact depletion should work.
	if err := pool.SubGas(800); err != nil {
		t.Fatalf("SubGas(800): %v", err)
	}
	if pool.Gas() != 0 {
		t.Fatalf("after exact depletion: got %d, want 0", pool.Gas())
	}

	// Subtract from zero should fail.
	if err := pool.SubGas(1); err == nil {
		t.Fatal("SubGas from empty pool should fail")
	}
}

func TestMultidimGasPool(t *testing.T) {
	limits := GasDimensions{Compute: 30_000_000, Calldata: 7_500_000, Blob: 786432}
	pool := NewMultidimGasPool(limits)

	if pool.Compute.Gas() != 30_000_000 {
		t.Fatalf("compute gas: got %d, want 30M", pool.Compute.Gas())
	}
	if pool.Calldata.Gas() != 7_500_000 {
		t.Fatalf("calldata gas: got %d, want 7.5M", pool.Calldata.Gas())
	}

	// Subtract gas across dimensions.
	usage := GasDimensions{Compute: 21000, Calldata: 64}
	if err := pool.SubGas(usage); err != nil {
		t.Fatalf("SubGas: %v", err)
	}

	remaining := pool.Remaining()
	if remaining.Compute != 30_000_000-21000 {
		t.Fatalf("compute remaining: got %d", remaining.Compute)
	}
	if remaining.Calldata != 7_500_000-64 {
		t.Fatalf("calldata remaining: got %d", remaining.Calldata)
	}

	// Exhaust calldata gas should fail and roll back compute.
	tooMuch := GasDimensions{Compute: 100, Calldata: remaining.Calldata + 1}
	if err := pool.SubGas(tooMuch); err == nil {
		t.Fatal("should fail when calldata exceeds pool")
	}
	// Compute should be rolled back.
	if pool.Compute.Gas() != remaining.Compute {
		t.Fatalf("compute should be rolled back: got %d, want %d", pool.Compute.Gas(), remaining.Compute)
	}
}

func TestBlockGasLimits(t *testing.T) {
	header := &types.Header{GasLimit: 30_000_000}
	limits := BlockGasLimits(header)

	if limits.Compute != 30_000_000 {
		t.Fatalf("compute limit: got %d, want 30M", limits.Compute)
	}
	if limits.Calldata != 7_500_000 {
		t.Fatalf("calldata limit: got %d, want 7.5M", limits.Calldata)
	}
	if limits.Blob != types.MaxBlobGasPerBlock {
		t.Fatalf("blob limit: got %d, want %d", limits.Blob, types.MaxBlobGasPerBlock)
	}
}

func TestBlockGasTargets(t *testing.T) {
	header := &types.Header{GasLimit: 30_000_000}
	targets := BlockGasTargets(header)

	if targets.Compute != 15_000_000 {
		t.Fatalf("compute target: got %d, want 15M", targets.Compute)
	}
	if targets.Calldata != 1_875_000 {
		t.Fatalf("calldata target: got %d, want 1.875M", targets.Calldata)
	}
	if targets.Blob != types.TargetBlobGasPerBlock {
		t.Fatalf("blob target: got %d, want %d", targets.Blob, types.TargetBlobGasPerBlock)
	}
}

func TestTxGasDimensions(t *testing.T) {
	to := types.HexToAddress("0xdead")

	// Simple transfer with calldata.
	tx := types.NewTransaction(&types.DynamicFeeTx{
		ChainID:   big.NewInt(1),
		GasTipCap: big.NewInt(1),
		GasFeeCap: big.NewInt(100),
		Gas:       21000,
		To:        &to,
		Value:     big.NewInt(0),
		Data:      []byte{0xff, 0x00, 0xab, 0x00}, // 2 nonzero + 2 zero
	})

	dims := TxGasDimensions(tx)
	if dims.Compute != 21000 {
		t.Fatalf("compute: got %d, want 21000", dims.Compute)
	}
	// tokens = 2*1 + 2*4 = 10, gas = 10*4 = 40
	if dims.Calldata != 40 {
		t.Fatalf("calldata: got %d, want 40", dims.Calldata)
	}
	if dims.Blob != 0 {
		t.Fatalf("blob: got %d, want 0", dims.Blob)
	}

	// Blob transaction.
	blobTx := types.NewTransaction(&types.BlobTx{
		ChainID:    big.NewInt(1),
		GasTipCap:  big.NewInt(1),
		GasFeeCap:  big.NewInt(100),
		Gas:        50000,
		To:         to,
		Value:      big.NewInt(0),
		BlobFeeCap: big.NewInt(10),
		BlobHashes: []types.Hash{{1}, {2}},
		Data:       []byte{0x01},
	})

	blobDims := TxGasDimensions(blobTx)
	if blobDims.Compute != 50000 {
		t.Fatalf("blob tx compute: got %d, want 50000", blobDims.Compute)
	}
	if blobDims.Blob != 2*131072 {
		t.Fatalf("blob tx blob gas: got %d, want %d", blobDims.Blob, 2*131072)
	}
	// 1 nonzero byte: tokens=4, gas=16
	if blobDims.Calldata != 16 {
		t.Fatalf("blob tx calldata: got %d, want 16", blobDims.Calldata)
	}
}

func TestBlockBaseFees(t *testing.T) {
	excess := uint64(0)
	used := uint64(0)
	blobGasUsed := uint64(0)
	excessBlobGas := uint64(0)

	parent := &types.Header{
		GasLimit:          30_000_000,
		GasUsed:           15_000_000, // exactly at target
		BaseFee:           big.NewInt(1_000_000_000),
		CalldataGasUsed:   &used,
		CalldataExcessGas: &excess,
		BlobGasUsed:       &blobGasUsed,
		ExcessBlobGas:     &excessBlobGas,
	}

	compute, calldata, blob := BlockBaseFees(parent)

	// Compute base fee should be unchanged (at target).
	if compute.Cmp(big.NewInt(1_000_000_000)) != 0 {
		t.Fatalf("compute base fee: got %s, want 1000000000", compute)
	}

	// Calldata base fee with zero excess should be minimum.
	if calldata.Cmp(big.NewInt(MinCalldataBaseFee)) != 0 {
		t.Fatalf("calldata base fee: got %s, want %d", calldata, MinCalldataBaseFee)
	}

	// Blob base fee with zero excess should be minimum.
	if blob.Cmp(big.NewInt(1)) < 0 {
		t.Fatalf("blob base fee: got %s, want >= 1", blob)
	}
}

func TestCalcExcessGasDimensions(t *testing.T) {
	gasLimit := uint64(30_000_000)
	calldataUsed := uint64(2_000_000) // above target of 1,875,000
	calldataExcess := uint64(100_000)
	blobUsed := uint64(393216 + 50000) // above blob target
	blobExcess := uint64(0)

	parent := &types.Header{
		GasLimit:          gasLimit,
		GasUsed:           20_000_000,
		CalldataGasUsed:   &calldataUsed,
		CalldataExcessGas: &calldataExcess,
		BlobGasUsed:       &blobUsed,
		ExcessBlobGas:     &blobExcess,
	}

	excess := CalcExcessGasDimensions(parent)

	// Calldata: excess=100K + used=2M - target=1.875M = 225K
	expectedCalldataExcess := CalcCalldataExcessGas(calldataExcess, calldataUsed, gasLimit)
	if excess.Calldata != expectedCalldataExcess {
		t.Fatalf("calldata excess: got %d, want %d", excess.Calldata, expectedCalldataExcess)
	}

	// Blob: excess=0 + used=443216 - target=393216 = 50000
	expectedBlobExcess := types.CalcExcessBlobGas(blobExcess, blobUsed)
	if excess.Blob != expectedBlobExcess {
		t.Fatalf("blob excess: got %d, want %d", excess.Blob, expectedBlobExcess)
	}
}

func TestGasDimensionsConstants(t *testing.T) {
	if CalldataGasPerTokenMultidim != 4 {
		t.Errorf("CalldataGasPerTokenMultidim = %d, want 4", CalldataGasPerTokenMultidim)
	}
	if TokensPerNonZeroByteMultidim != 4 {
		t.Errorf("TokensPerNonZeroByteMultidim = %d, want 4", TokensPerNonZeroByteMultidim)
	}
	if CalldataGasLimitRatioMultidim != 4 {
		t.Errorf("CalldataGasLimitRatioMultidim = %d, want 4", CalldataGasLimitRatioMultidim)
	}
	if LimitTargetRatioCompute != 2 {
		t.Errorf("LimitTargetRatioCompute = %d, want 2", LimitTargetRatioCompute)
	}
	if LimitTargetRatioBlob != 2 {
		t.Errorf("LimitTargetRatioBlob = %d, want 2", LimitTargetRatioBlob)
	}
	if LimitTargetRatioCalldata != 4 {
		t.Errorf("LimitTargetRatioCalldata = %d, want 4", LimitTargetRatioCalldata)
	}
}
