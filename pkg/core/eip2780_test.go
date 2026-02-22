package core

import (
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/state"
	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/core/vm"
)

// TestEIP2780Constants verifies the EIP-2780 gas constants.
func TestEIP2780Constants(t *testing.T) {
	tests := []struct {
		name string
		got  uint64
		want uint64
	}{
		{"TxBaseGlamsterdam", vm.TxBaseGlamsterdam, 4500},
		{"GasNewAccount", vm.GasNewAccount, 25000},
		{"StateUpdate", vm.StateUpdate, 1000},
		{"ColdAccountCostNoCode", vm.ColdAccountCostNoCode, 500},
		{"ColdAccountCostCode", vm.ColdAccountCostCode, 2600},
		{"CallValueTransferGlamst", vm.CallValueTransferGlamst, 2000},
		{"CallNewAccountGlamst", vm.CallNewAccountGlamst, 26000},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("%s = %d, want %d", tt.name, tt.got, tt.want)
			}
		})
	}
}

// TestEIP2780IntrinsicGas verifies the Glamsterdam intrinsic gas function.
func TestEIP2780IntrinsicGas(t *testing.T) {
	tests := []struct {
		name        string
		data        []byte
		isCreate    bool
		hasValue    bool
		toExists    bool
		authCount   uint64
		emptyAuth   uint64
		want        uint64
	}{
		{
			name: "minimal NOP tx",
			want: 4500, // TX_BASE_COST only
		},
		{
			name:     "ETH transfer to existing EOA",
			hasValue: true,
			toExists: true,
			want:     4500, // TX_BASE_COST, no surcharge (to exists)
		},
		{
			name:     "ETH transfer creating new account",
			hasValue: true,
			toExists: false,
			want:     4500 + 25000, // TX_BASE_COST + GAS_NEW_ACCOUNT = 29500
		},
		{
			name:     "value=0 to non-existent (no surcharge)",
			hasValue: false,
			toExists: false,
			want:     4500,
		},
		{
			name:     "create tx (unchanged by EIP-2780 for base)",
			isCreate: true,
			want:     4500 + 32000, // TX_BASE_COST + TxCreateGas
		},
		{
			name: "calldata pricing unchanged",
			data: []byte{0xff, 0x00, 0xaa, 0x00},
			// 2 non-zero * 16 = 32, 2 zero * 4 = 8 => 40
			want: 4500 + 40,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := intrinsicGasGlamst(tt.data, tt.isCreate, tt.hasValue, tt.toExists, tt.authCount, tt.emptyAuth)
			if got != tt.want {
				t.Errorf("intrinsicGasGlamst = %d, want %d", got, tt.want)
			}
		})
	}
}

// TestEIP2780ReducedIntrinsicGasViaApplyMessage verifies that Glamsterdam
// transactions use the reduced intrinsic gas from EIP-2780.
func TestEIP2780ReducedIntrinsicGasViaApplyMessage(t *testing.T) {
	statedb := state.NewMemoryStateDB()

	sender := types.HexToAddress("0x1111111111111111111111111111111111111111")
	statedb.CreateAccount(sender)
	statedb.AddBalance(sender, new(big.Int).Mul(big.NewInt(1e18), big.NewInt(100)))

	to := types.HexToAddress("0x2222222222222222222222222222222222222222")
	statedb.CreateAccount(to)

	msg := &Message{
		From:     sender,
		To:       &to,
		GasLimit: 100000,
		GasPrice: big.NewInt(1),
		Value:    big.NewInt(1),
		Nonce:    0,
	}

	header := &types.Header{
		Number:   big.NewInt(100),
		GasLimit: 60_000_000,
		BaseFee:  big.NewInt(1),
		Time:     100,
	}

	gp := new(GasPool).AddGas(header.GasLimit)
	result, err := applyMessage(TestConfigGlamsterdan, nil, statedb, header, msg, gp)
	if err != nil {
		t.Fatalf("applyMessage failed: %v", err)
	}

	// Under Glamsterdam (EIP-2780), a simple ETH transfer to existing EOA
	// should use ~6000 gas (4500 base + 500 cold_nocode + 1000 state_update),
	// but the calldata floor or other costs may dominate.
	// The key check is that it's less than the legacy 21000.
	if result.UsedGas >= 21000 {
		t.Errorf("UsedGas = %d, expected < 21000 under EIP-2780", result.UsedGas)
	}
}

// TestEIP2780NewAccountSurcharge verifies the GAS_NEW_ACCOUNT surcharge
// for value transfers to non-existent accounts.
func TestEIP2780NewAccountSurcharge(t *testing.T) {
	statedb := state.NewMemoryStateDB()

	sender := types.HexToAddress("0x1111111111111111111111111111111111111111")
	statedb.CreateAccount(sender)
	statedb.AddBalance(sender, new(big.Int).Mul(big.NewInt(1e18), big.NewInt(100)))

	// to does NOT exist
	to := types.HexToAddress("0x3333333333333333333333333333333333333333")

	msg := &Message{
		From:     sender,
		To:       &to,
		GasLimit: 200000,
		GasPrice: big.NewInt(1),
		Value:    big.NewInt(1),
		Nonce:    0,
	}

	header := &types.Header{
		Number:   big.NewInt(100),
		GasLimit: 60_000_000,
		BaseFee:  big.NewInt(1),
		Time:     100,
	}

	gp := new(GasPool).AddGas(header.GasLimit)
	result, err := applyMessage(TestConfigGlamsterdan, nil, statedb, header, msg, gp)
	if err != nil {
		t.Fatalf("applyMessage failed: %v", err)
	}

	// With surcharge: 4500 + 25000 = 29500 intrinsic gas minimum.
	if result.UsedGas < 29500 {
		t.Errorf("UsedGas = %d, expected >= 29500 with EIP-2780 new account surcharge", result.UsedGas)
	}
}
