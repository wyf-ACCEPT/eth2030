package core

import (
	"testing"

	"github.com/eth2030/eth2030/core/vm"
)

// TestEIP8037Constants verifies the EIP-8037 state creation gas constants.
func TestEIP8037Constants(t *testing.T) {
	tests := []struct {
		name string
		got  uint64
		want uint64
	}{
		{"CostPerStateByte", vm.CostPerStateByte, 662},
		// GAS_CREATE = 112 * 662 + 9000 = 74144 + 9000 = 83144
		{"GasCreateGlamsterdam", vm.GasCreateGlamsterdam, 112*662 + 9000},
		// GAS_CODE_DEPOSIT = 662 per byte
		{"GasCodeDepositGlamsterdam", vm.GasCodeDepositGlamsterdam, 662},
		// GAS_STORAGE_SET = 32 * 662 + 2900 = 21184 + 2900 = 24084
		{"GasSstoreSetGlamsterdam", vm.GasSstoreSetGlamsterdam, 32*662 + 2900},
		// GAS_NEW_ACCOUNT state component = 112 * 662 = 74144
		{"GasNewAccountState", vm.GasNewAccountState, 112 * 662},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("%s = %d, want %d", tt.name, tt.got, tt.want)
			}
		})
	}
}

// TestEIP8037SstoreSetIncreased verifies that the SSTORE set cost is higher
// under Glamsterdam due to EIP-8037.
func TestEIP8037SstoreSetIncreased(t *testing.T) {
	if vm.GasSstoreSetGlamsterdam <= vm.GasSstoreSet {
		t.Errorf("GasSstoreSetGlamsterdam (%d) should be > GasSstoreSet (%d)",
			vm.GasSstoreSetGlamsterdam, vm.GasSstoreSet)
	}
}

// TestEIP8037CreateCostIncreased verifies that contract creation cost is
// higher under Glamsterdam due to EIP-8037.
func TestEIP8037CreateCostIncreased(t *testing.T) {
	if vm.GasCreateGlamsterdam <= vm.GasCreate {
		t.Errorf("GasCreateGlamsterdam (%d) should be > GasCreate (%d)",
			vm.GasCreateGlamsterdam, vm.GasCreate)
	}
}

// TestEIP8037CodeDepositIncreased verifies that code deposit cost per byte
// is higher under Glamsterdam due to EIP-8037.
func TestEIP8037CodeDepositIncreased(t *testing.T) {
	if vm.GasCodeDepositGlamsterdam <= vm.CreateDataGas {
		t.Errorf("GasCodeDepositGlamsterdam (%d) should be > CreateDataGas (%d)",
			vm.GasCodeDepositGlamsterdam, vm.CreateDataGas)
	}
}

// TestEIP8037CostPerStateByteDerivation verifies the cost_per_state_byte
// derivation for a 60M gas limit.
func TestEIP8037CostPerStateByteDerivation(t *testing.T) {
	// raw = ceil((60_000_000 * 2_628_000) / (2 * 107_374_182_400))
	// = ceil(157_680_000_000_000 / 214_748_364_800)
	// = ceil(734.12...) = 735
	// After quantization with 5 significant bits and offset 9578:
	// shifted = 735 + 9578 = 10313
	// bit_length(10313) = 14, shift = 14 - 5 = 9
	// quantized = ((10313 >> 9) << 9) - 9578 = (20 << 9) - 9578 = 10240 - 9578 = 662
	if vm.CostPerStateByte != 662 {
		t.Errorf("CostPerStateByte = %d, want 662 at 60M gas limit", vm.CostPerStateByte)
	}
}
