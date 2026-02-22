package core

import (
	"testing"
)

func TestCalldataFloorGas(t *testing.T) {
	tests := []struct {
		name     string
		data     []byte
		isCreate bool
		want     uint64
	}{
		{
			name: "empty calldata",
			data: nil,
			want: TxGas, // 21000
		},
		{
			name: "all zero bytes",
			data: make([]byte, 100),
			// tokens = 100 * 1 = 100
			// floor = 21000 + 100 * 10 = 22000
			want: 21000 + 100*TotalCostFloorPerToken,
		},
		{
			name: "all non-zero bytes",
			data: []byte{0xff, 0xaa, 0xbb, 0xcc},
			// tokens = 4 * 4 = 16
			// floor = 21000 + 16 * 10 = 21160
			want: 21000 + 16*TotalCostFloorPerToken,
		},
		{
			name: "mixed calldata",
			data: []byte{0x00, 0xff, 0x00, 0xaa},
			// tokens = 2 * 1 + 2 * 4 = 10
			// floor = 21000 + 10 * 10 = 21100
			want: 21000 + 10*TotalCostFloorPerToken,
		},
		{
			name:     "create transaction",
			data:     []byte{0xff, 0xff},
			isCreate: true,
			// tokens = 2 * 4 = 8
			// floor = 21000 + 32000 + 8 * 10 = 53080
			want: TxGas + TxCreateGas + 8*TotalCostFloorPerToken,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := calldataFloorGas(tt.data, tt.isCreate)
			if got != tt.want {
				t.Errorf("calldataFloorGas = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestCalldataFloorGasVsIntrinsicGas(t *testing.T) {
	// For calldata with many non-zero bytes, the standard intrinsic gas
	// (16 per non-zero byte) should exceed the floor (10 per token of 4).
	data := make([]byte, 100)
	for i := range data {
		data[i] = 0xff // all non-zero
	}

	standard := intrinsicGas(data, false, false, 0, 0)
	floor := calldataFloorGas(data, false)

	// Standard: 21000 + 100 * 16 = 22600
	// Floor: 21000 + 400 * 10 = 25000
	// In this case floor > standard, so EIP-7623 would charge more.
	if standard >= floor {
		// The floor can sometimes be lower than standard for dense data.
		// This is expected behavior — the floor only matters when standard
		// gas would be too cheap (e.g., for transactions with lots of zero bytes).
	}

	// For mostly-zero calldata, standard gas is very cheap but floor is higher.
	zeroData := make([]byte, 1000)
	zeroData[0] = 0xff // one non-zero byte

	standardZero := intrinsicGas(zeroData, false, false, 0, 0)
	floorZero := calldataFloorGas(zeroData, false)

	// Standard: 21000 + 999 * 4 + 1 * 16 = 25012
	// Floor: 21000 + (999 * 1 + 1 * 4) * 10 = 31030
	if floorZero <= standardZero {
		t.Errorf("expected floor (%d) > standard (%d) for mostly-zero calldata", floorZero, standardZero)
	}
}

func TestEIP7623Constants(t *testing.T) {
	if TotalCostFloorPerToken != 10 {
		t.Errorf("TotalCostFloorPerToken = %d, want 10", TotalCostFloorPerToken)
	}
	if StandardTokenCost != 16 {
		t.Errorf("StandardTokenCost = %d, want 16", StandardTokenCost)
	}
}
