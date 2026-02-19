package core

import (
	"math/big"
	"testing"
)

func TestBlobScheduleEntryParameters(t *testing.T) {
	// Verify Dencun schedule matches EIP-4844.
	if DencunBlobSchedule.Target != 3 {
		t.Errorf("Dencun target = %d, want 3", DencunBlobSchedule.Target)
	}
	if DencunBlobSchedule.Max != 6 {
		t.Errorf("Dencun max = %d, want 6", DencunBlobSchedule.Max)
	}
	if DencunBlobSchedule.BaseFeeUpdateFraction != 3338477 {
		t.Errorf("Dencun fraction = %d, want 3338477", DencunBlobSchedule.BaseFeeUpdateFraction)
	}

	// Verify Prague/Electra schedule matches EIP-7691.
	if PragueElectraBlobSchedule.Target != 6 {
		t.Errorf("Prague target = %d, want 6", PragueElectraBlobSchedule.Target)
	}
	if PragueElectraBlobSchedule.Max != 9 {
		t.Errorf("Prague max = %d, want 9", PragueElectraBlobSchedule.Max)
	}
	if PragueElectraBlobSchedule.BaseFeeUpdateFraction != 5007716 {
		t.Errorf("Prague fraction = %d, want 5007716", PragueElectraBlobSchedule.BaseFeeUpdateFraction)
	}
}

func TestGetBlobScheduleEntry(t *testing.T) {
	praguetime := uint64(1000)
	config := &ChainConfig{
		PragueTime: &praguetime,
	}

	// Before Prague: Dencun schedule.
	sched := GetBlobScheduleEntry(config, 999)
	if sched != DencunBlobSchedule {
		t.Errorf("before Prague: got %+v, want Dencun", sched)
	}

	// At Prague: Prague/Electra schedule.
	sched = GetBlobScheduleEntry(config, 1000)
	if sched != PragueElectraBlobSchedule {
		t.Errorf("at Prague: got %+v, want PragueElectra", sched)
	}

	// After Prague.
	sched = GetBlobScheduleEntry(config, 2000)
	if sched != PragueElectraBlobSchedule {
		t.Errorf("after Prague: got %+v, want PragueElectra", sched)
	}

	// No Prague configured: always Dencun.
	noPrague := &ChainConfig{}
	sched = GetBlobScheduleEntry(noPrague, 5000)
	if sched != DencunBlobSchedule {
		t.Errorf("no Prague: got %+v, want Dencun", sched)
	}
}

func TestCalcBlobBaseFeeWithSchedule(t *testing.T) {
	// Zero excess: base fee should be 1 (MIN_BASE_FEE).
	fee := CalcBlobBaseFeeWithSchedule(0, DencunBlobSchedule)
	if fee.Cmp(big.NewInt(1)) != 0 {
		t.Errorf("zero excess fee = %s, want 1", fee)
	}

	// Same excess, different fractions: smaller fraction = higher fee.
	// Need large enough excess for the exponential to exceed floor of 1.
	feeDencun := CalcBlobBaseFeeWithSchedule(10000000, DencunBlobSchedule)
	feePrague := CalcBlobBaseFeeWithSchedule(10000000, PragueElectraBlobSchedule)
	if feeDencun.Cmp(feePrague) <= 0 {
		t.Errorf("Dencun fee (%s) should be > Prague fee (%s) for same excess (smaller fraction)",
			feeDencun, feePrague)
	}

	// Fee should increase with excess.
	fee1 := CalcBlobBaseFeeWithSchedule(5000000, DencunBlobSchedule)
	fee2 := CalcBlobBaseFeeWithSchedule(10000000, DencunBlobSchedule)
	if fee2.Cmp(fee1) <= 0 {
		t.Errorf("fee should increase with excess: fee(5000000)=%s, fee(10000000)=%s",
			fee1, fee2)
	}
}

func TestCalcExcessBlobGasWithScheduleEntry(t *testing.T) {
	tests := []struct {
		name            string
		parentExcess    uint64
		parentBlobsUsed uint64
		schedule        BlobScheduleEntry
		want            uint64
	}{
		{
			name:            "Dencun: at target, no prior excess",
			parentExcess:    0,
			parentBlobsUsed: 3,
			schedule:        DencunBlobSchedule,
			want:            0,
		},
		{
			name:            "Dencun: above target",
			parentExcess:    0,
			parentBlobsUsed: 6, // max = 6 blobs = 786432 gas
			schedule:        DencunBlobSchedule,
			want:            3 * GasPerBlob, // (0 + 6*131072) - 3*131072 = 393216
		},
		{
			name:            "Dencun: below target, excess absorbed",
			parentExcess:    100000,
			parentBlobsUsed: 1, // 131072 gas
			schedule:        DencunBlobSchedule,
			// 100000 + 131072 < 393216, so excess = 0
			want: 0,
		},
		{
			name:            "Prague: at target",
			parentExcess:    0,
			parentBlobsUsed: 6, // target for Prague
			schedule:        PragueElectraBlobSchedule,
			want:            0,
		},
		{
			name:            "Prague: max blobs",
			parentExcess:    0,
			parentBlobsUsed: 9, // max for Prague
			schedule:        PragueElectraBlobSchedule,
			want:            3 * GasPerBlob, // (0 + 9*131072) - 6*131072 = 393216
		},
		{
			name:            "Prague: accumulating excess",
			parentExcess:    500000,
			parentBlobsUsed: 9,
			schedule:        PragueElectraBlobSchedule,
			want:            500000 + 9*GasPerBlob - 6*GasPerBlob,
		},
		{
			name:            "empty block, excess decays",
			parentExcess:    500000,
			parentBlobsUsed: 0,
			schedule:        PragueElectraBlobSchedule,
			// 500000 + 0 < 6*131072 = 786432, so 0
			want: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CalcExcessBlobGasWithSchedule(tt.parentExcess, tt.parentBlobsUsed, tt.schedule)
			if got != tt.want {
				t.Errorf("CalcExcessBlobGasWithSchedule(%d, %d, ...) = %d, want %d",
					tt.parentExcess, tt.parentBlobsUsed, got, tt.want)
			}
		})
	}
}

func TestCalcExcessBlobGasAccumulationWithSchedule(t *testing.T) {
	// Simulate a sequence of full blocks (max blobs) under Prague schedule.
	excess := uint64(0)
	for i := 0; i < 10; i++ {
		excess = CalcExcessBlobGasWithSchedule(excess, 9, PragueElectraBlobSchedule)
	}
	// After 10 full Prague blocks: each adds 3*GasPerBlob = 393216
	expected := uint64(10 * 3 * GasPerBlob)
	if excess != expected {
		t.Errorf("after 10 full blocks: excess = %d, want %d", excess, expected)
	}

	// Now empty blocks should decay it.
	for excess > 0 {
		excess = CalcExcessBlobGasWithSchedule(excess, 0, PragueElectraBlobSchedule)
	}
	if excess != 0 {
		t.Errorf("after decay: excess = %d, want 0", excess)
	}
}
