package core

import (
	"testing"

	"github.com/eth2030/eth2030/core/vm"
)

// TestEIP8038Constants verifies the EIP-8038 state access gas constants.
func TestEIP8038Constants(t *testing.T) {
	tests := []struct {
		name string
		got  uint64
		want uint64
	}{
		{"ColdAccountAccessGlamst", vm.ColdAccountAccessGlamst, 3500},
		{"ColdSloadGlamst", vm.ColdSloadGlamst, 2800},
		{"WarmStorageReadGlamst", vm.WarmStorageReadGlamst, 150},
		{"AccessListAddressGlamst", vm.AccessListAddressGlamst, 3200},
		{"AccessListStorageGlamst", vm.AccessListStorageGlamst, 2500},
		{"SstoreClearsRefundGlam", vm.SstoreClearsRefundGlam, 6400},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("%s = %d, want %d", tt.name, tt.got, tt.want)
			}
		})
	}
}

// TestEIP8038IncreasedAccessCosts verifies that all access costs have
// increased compared to the pre-Glamsterdam values.
func TestEIP8038IncreasedAccessCosts(t *testing.T) {
	checks := []struct {
		name    string
		glamst  uint64
		pre     uint64
	}{
		{"ColdAccountAccess", vm.ColdAccountAccessGlamst, vm.ColdAccountAccessCost},
		{"ColdSload", vm.ColdSloadGlamst, vm.ColdSloadCost},
		{"WarmStorageRead", vm.WarmStorageReadGlamst, vm.WarmStorageReadCost},
	}
	for _, c := range checks {
		t.Run(c.name, func(t *testing.T) {
			if c.glamst <= c.pre {
				t.Errorf("Glamsterdam %s (%d) should be > pre-Glamsterdam (%d)",
					c.name, c.glamst, c.pre)
			}
		})
	}
}

// TestEIP8038ExtCodeSizeExtraRead verifies that EXTCODESIZE under Glamsterdam
// charges an extra WarmStorageReadGlamst for the second DB read (code size).
func TestEIP8038ExtCodeSizeExtraRead(t *testing.T) {
	glamstTbl := vm.NewGlamsterdanJumpTable()
	pragueTbl := vm.NewPragueJumpTable()

	// Glamsterdam EXTCODESIZE should have higher constant gas than Prague.
	glamstGas := glamstTbl[vm.EXTCODESIZE].GetConstantGas()
	pragueGas := pragueTbl[vm.EXTCODESIZE].GetConstantGas()

	if glamstGas <= pragueGas {
		t.Errorf("Glamsterdam EXTCODESIZE constantGas (%d) should be > Prague (%d)",
			glamstGas, pragueGas)
	}
}

// TestEIP8038SloadCostIncrease verifies the SLOAD gas increase in the jump table.
func TestEIP8038SloadCostIncrease(t *testing.T) {
	glamstTbl := vm.NewGlamsterdanJumpTable()

	sloadGas := glamstTbl[vm.SLOAD].GetConstantGas()
	if sloadGas != vm.WarmStorageReadGlamst {
		t.Errorf("SLOAD constantGas = %d, want %d", sloadGas, vm.WarmStorageReadGlamst)
	}
}

// TestEIP8038BalanceCostIncrease verifies the BALANCE gas increase.
func TestEIP8038BalanceCostIncrease(t *testing.T) {
	glamstTbl := vm.NewGlamsterdanJumpTable()

	balGas := glamstTbl[vm.BALANCE].GetConstantGas()
	if balGas != vm.WarmStorageReadGlamst {
		t.Errorf("BALANCE constantGas = %d, want %d", balGas, vm.WarmStorageReadGlamst)
	}
}
