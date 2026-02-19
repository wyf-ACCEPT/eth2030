package core

import (
	"math/big"
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

func TestIntrinsicGasSimpleTransfer(t *testing.T) {
	gas, err := IntrinsicGas(nil, false, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gas != TxGas {
		t.Errorf("simple transfer gas = %d, want %d", gas, TxGas)
	}
}

func TestIntrinsicGasContractCreation(t *testing.T) {
	gas, err := IntrinsicGas(nil, true, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gas != TxGasContractCreation {
		t.Errorf("contract creation gas = %d, want %d", gas, TxGasContractCreation)
	}
}

func TestIntrinsicGasWithData(t *testing.T) {
	tests := []struct {
		name     string
		data     []byte
		creation bool
		want     uint64
	}{
		{
			name:     "all zero bytes",
			data:     make([]byte, 10),
			creation: false,
			want:     TxGas + 10*TxDataZeroGas,
		},
		{
			name:     "all non-zero bytes",
			data:     []byte{1, 2, 3, 4, 5},
			creation: false,
			want:     TxGas + 5*TxDataNonZeroGas,
		},
		{
			name:     "mixed bytes",
			data:     []byte{0, 1, 0, 2, 0},
			creation: false,
			want:     TxGas + 3*TxDataZeroGas + 2*TxDataNonZeroGas,
		},
		{
			name:     "data with contract creation",
			data:     []byte{0xFF, 0x00},
			creation: true,
			want:     TxGasContractCreation + 1*TxDataZeroGas + 1*TxDataNonZeroGas,
		},
		{
			name:     "empty data transfer",
			data:     []byte{},
			creation: false,
			want:     TxGas,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gas, err := IntrinsicGas(tt.data, tt.creation, false)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if gas != tt.want {
				t.Errorf("gas = %d, want %d", gas, tt.want)
			}
		})
	}
}

func TestIntrinsicGasAccessList(t *testing.T) {
	// Access list tx without data.
	gas, err := IntrinsicGas(nil, false, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := TxGas + AccessListTxGas
	if gas != want {
		t.Errorf("access list gas = %d, want %d", gas, want)
	}
}

func TestIntrinsicGasWithAccessListEntries(t *testing.T) {
	al := types.AccessList{
		{
			Address:     types.Address{1},
			StorageKeys: []types.Hash{{1}, {2}},
		},
		{
			Address:     types.Address{2},
			StorageKeys: []types.Hash{{3}},
		},
	}

	gas, err := IntrinsicGasWithAccessList(nil, false, al)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Base + access list surcharge + 2 addresses * addressGas + 3 keys * keyGas
	want := TxGas + AccessListTxGas + 2*TxAccessListAddressGas + 3*TxAccessListStorageKeyGas
	if gas != want {
		t.Errorf("gas = %d, want %d", gas, want)
	}
}

func TestEstimateGasSimpleTransfer(t *testing.T) {
	ge := NewGasEstimator(DefaultGasEstimatorConfig())

	// Executor that succeeds when gas >= 21000.
	ge.SetExecutor(func(msg CallMsg, gas uint64) (bool, uint64, error) {
		if gas >= TxGas {
			return true, TxGas, nil
		}
		return false, gas, nil
	})

	to := types.Address{1}
	msg := CallMsg{
		From:  types.Address{0xAA},
		To:    &to,
		Value: big.NewInt(1),
	}

	estimated, err := ge.EstimateGas(msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Binary search converges close to 21000 and multiplier is applied.
	// The estimate must be >= TxGas (the minimum) and reasonable.
	if estimated < TxGas {
		t.Errorf("estimated %d is below intrinsic gas %d", estimated, TxGas)
	}
	// With 1.2 multiplier, should not exceed 21000 * 1.2 + small delta.
	maxExpected := uint64(float64(TxGas)*1.2) + 100
	if estimated > maxExpected {
		t.Errorf("estimated %d is unreasonably high (max expected ~%d)", estimated, maxExpected)
	}
}

func TestEstimateGasWithData(t *testing.T) {
	ge := NewGasEstimator(DefaultGasEstimatorConfig())

	data := []byte{0xFF, 0x00, 0xFF} // 2 non-zero + 1 zero = 36 gas for data
	dataGas := 2*TxDataNonZeroGas + 1*TxDataZeroGas
	requiredGas := TxGas + dataGas + 5000 // extra 5000 for simulated execution

	ge.SetExecutor(func(msg CallMsg, gas uint64) (bool, uint64, error) {
		if gas >= requiredGas {
			return true, requiredGas, nil
		}
		return false, gas, nil
	})

	to := types.Address{2}
	msg := CallMsg{
		From: types.Address{1},
		To:   &to,
		Data: data,
	}

	estimated, err := ge.EstimateGas(msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The estimate should be close to requiredGas * 1.2.
	minExpected := uint64(float64(requiredGas) * 1.2)
	if estimated < requiredGas {
		t.Errorf("estimated %d is below required gas %d", estimated, requiredGas)
	}
	// Allow small delta from binary search non-exact convergence.
	if estimated > minExpected+100 {
		t.Errorf("estimated %d is unreasonably high (expected ~%d)", estimated, minExpected)
	}
}

func TestEstimateGasContractCreation(t *testing.T) {
	ge := NewGasEstimator(DefaultGasEstimatorConfig())

	requiredGas := TxGasContractCreation + 10000 // 53000 + execution cost

	ge.SetExecutor(func(msg CallMsg, gas uint64) (bool, uint64, error) {
		if gas >= requiredGas {
			return true, requiredGas, nil
		}
		return false, gas, nil
	})

	msg := CallMsg{
		From: types.Address{1},
		To:   nil, // contract creation
		Data: []byte{0x60, 0x00, 0x60, 0x00, 0x52},
	}

	estimated, err := ge.EstimateGas(msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The estimate should be close to requiredGas * 1.2.
	if estimated < requiredGas {
		t.Errorf("estimated %d is below required gas %d", estimated, requiredGas)
	}
	maxExpected := uint64(float64(requiredGas)*1.2) + 100
	if estimated > maxExpected {
		t.Errorf("estimated %d is unreasonably high (expected ~%d)", estimated, uint64(float64(requiredGas)*1.2))
	}
}

func TestEstimateGasBinarySearchConvergence(t *testing.T) {
	ge := NewGasEstimator(GasEstimatorConfig{
		DefaultGasLimit:  1_000_000,
		MaxIterations:    30,
		GasCapMultiplier: 1.0, // no multiplier to test exact convergence
	})

	targetGas := uint64(123456)

	ge.SetExecutor(func(msg CallMsg, gas uint64) (bool, uint64, error) {
		if gas >= targetGas {
			return true, targetGas, nil
		}
		return false, gas, nil
	})

	to := types.Address{1}
	msg := CallMsg{
		From: types.Address{0xAA},
		To:   &to,
	}

	estimated, err := ge.EstimateGas(msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// With 30 iterations, binary search over [21000, 1000000] should converge exactly.
	if estimated != targetGas {
		t.Errorf("estimated = %d, want %d", estimated, targetGas)
	}
}

func TestEstimateGasAlwaysFails(t *testing.T) {
	ge := NewGasEstimator(DefaultGasEstimatorConfig())

	// Executor that always fails.
	ge.SetExecutor(func(msg CallMsg, gas uint64) (bool, uint64, error) {
		return false, 0, nil
	})

	to := types.Address{1}
	msg := CallMsg{
		From: types.Address{1},
		To:   &to,
	}

	_, err := ge.EstimateGas(msg)
	if err != ErrGasEstimationFailed {
		t.Errorf("expected ErrGasEstimationFailed, got %v", err)
	}
}

func TestEstimateGasIntrinsicTooHigh(t *testing.T) {
	ge := NewGasEstimator(GasEstimatorConfig{
		DefaultGasLimit:  100, // very low limit
		MaxIterations:    20,
		GasCapMultiplier: 1.2,
	})

	to := types.Address{1}
	msg := CallMsg{
		From: types.Address{1},
		To:   &to,
	}

	_, err := ge.EstimateGas(msg)
	if err != ErrIntrinsicGasTooHigh {
		t.Errorf("expected ErrIntrinsicGasTooHigh, got %v", err)
	}
}

func TestEstimateAccessListGas(t *testing.T) {
	ge := NewGasEstimator(DefaultGasEstimatorConfig())

	to := types.Address{1}
	msg := CallMsg{
		From: types.Address{0xAA},
		To:   &to,
	}

	addrs := []types.Address{{1}, {2}, {3}}
	gas := ge.EstimateAccessListGas(msg, addrs)

	// 21000 base + 2600 access list surcharge + 3 * 2400 per address
	want := TxGas + AccessListTxGas + 3*TxAccessListAddressGas
	if gas != want {
		t.Errorf("access list gas = %d, want %d", gas, want)
	}
}

func TestEstimateAccessListGasContractCreation(t *testing.T) {
	ge := NewGasEstimator(DefaultGasEstimatorConfig())

	msg := CallMsg{
		From: types.Address{0xAA},
		To:   nil, // contract creation
		Data: []byte{0x60, 0x00},
	}

	addrs := []types.Address{{1}}
	gas := ge.EstimateAccessListGas(msg, addrs)

	dataGas := uint64(1)*TxDataNonZeroGas + uint64(1)*TxDataZeroGas
	want := TxGasContractCreation + AccessListTxGas + dataGas + 1*TxAccessListAddressGas
	if gas != want {
		t.Errorf("access list gas = %d, want %d", gas, want)
	}
}

func TestEstimateGasWithUserGasCap(t *testing.T) {
	ge := NewGasEstimator(GasEstimatorConfig{
		DefaultGasLimit:  30_000_000,
		MaxIterations:    30,
		GasCapMultiplier: 1.0,
	})

	targetGas := uint64(50000)

	ge.SetExecutor(func(msg CallMsg, gas uint64) (bool, uint64, error) {
		if gas >= targetGas {
			return true, targetGas, nil
		}
		return false, gas, nil
	})

	to := types.Address{1}
	msg := CallMsg{
		From: types.Address{1},
		To:   &to,
		Gas:  100000, // user-specified cap
	}

	estimated, err := ge.EstimateGas(msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// With multiplier 1.0 and 30 iterations over [21000, 100000], should converge.
	if estimated != targetGas {
		t.Errorf("estimated = %d, want %d", estimated, targetGas)
	}
}

func TestNewGasEstimatorDefaults(t *testing.T) {
	ge := NewGasEstimator(GasEstimatorConfig{})

	if ge.config.DefaultGasLimit != 30_000_000 {
		t.Errorf("DefaultGasLimit = %d, want 30000000", ge.config.DefaultGasLimit)
	}
	if ge.config.MaxIterations != 20 {
		t.Errorf("MaxIterations = %d, want 20", ge.config.MaxIterations)
	}
	if ge.config.GasCapMultiplier != 1.2 {
		t.Errorf("GasCapMultiplier = %f, want 1.2", ge.config.GasCapMultiplier)
	}
}

func TestEstimateGasCapAtBlockLimit(t *testing.T) {
	ge := NewGasEstimator(GasEstimatorConfig{
		DefaultGasLimit:  50000,
		MaxIterations:    30,
		GasCapMultiplier: 2.0, // large multiplier to test capping
	})

	ge.SetExecutor(func(msg CallMsg, gas uint64) (bool, uint64, error) {
		if gas >= 30000 {
			return true, 30000, nil
		}
		return false, gas, nil
	})

	to := types.Address{1}
	msg := CallMsg{
		From: types.Address{1},
		To:   &to,
	}

	estimated, err := ge.EstimateGas(msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 30000 * 2.0 = 60000 but capped at 50000
	if estimated != 50000 {
		t.Errorf("estimated = %d, want 50000 (capped at block limit)", estimated)
	}
}
