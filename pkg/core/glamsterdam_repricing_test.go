package core

import (
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

func TestDefaultGlamsterdamGasTable(t *testing.T) {
	table := DefaultGlamsterdamGasTable()

	checks := []struct {
		name string
		got  uint64
		want uint64
	}{
		{"SloadCold", table.SloadCold, 800},
		{"SloadWarm", table.SloadWarm, 100},
		{"SstoreSet", table.SstoreSet, 5000},
		{"SstoreReset", table.SstoreReset, 1500},
		{"CallCold", table.CallCold, 100},
		{"CallWarm", table.CallWarm, 100},
		{"BalanceCold", table.BalanceCold, 400},
		{"BalanceWarm", table.BalanceWarm, 100},
		{"Create", table.Create, 10000},
		{"ExtCodeSize", table.ExtCodeSize, 400},
		{"ExtCodeCopy", table.ExtCodeCopy, 400},
		{"ExtCodeHash", table.ExtCodeHash, 400},
		{"Selfdestruct", table.Selfdestruct, 5000},
		{"Log", table.Log, 375},
		{"Keccak256", table.Keccak256, 45},
	}

	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %d, want %d", c.name, c.got, c.want)
		}
	}
}

func TestApplyGlamsterdamRepricing(t *testing.T) {
	// Start with a table that has pre-Glamsterdam values.
	table := &GlamsterdamGasTable{
		SloadCold:   2100,
		SstoreSet:   20000,
		CallCold:    2600,
		BalanceCold: 2600,
		Create:      32000,
		ExtCodeSize: 2600,
		ExtCodeCopy: 2600,
		ExtCodeHash: 2600,
		Keccak256:   30,
	}

	result := ApplyGlamsterdamRepricing(table)

	// Should return the same pointer.
	if result != table {
		t.Fatal("ApplyGlamsterdamRepricing should return the same pointer")
	}

	// Verify all values were updated.
	if table.SloadCold != 800 {
		t.Errorf("SloadCold = %d, want 800", table.SloadCold)
	}
	if table.SstoreSet != 5000 {
		t.Errorf("SstoreSet = %d, want 5000", table.SstoreSet)
	}
	if table.CallCold != 100 {
		t.Errorf("CallCold = %d, want 100", table.CallCold)
	}
	if table.BalanceCold != 400 {
		t.Errorf("BalanceCold = %d, want 400", table.BalanceCold)
	}
	if table.Create != 10000 {
		t.Errorf("Create = %d, want 10000", table.Create)
	}
	if table.Keccak256 != 45 {
		t.Errorf("Keccak256 = %d, want 45", table.Keccak256)
	}
}

func TestGlamsterdamRepricingEntries(t *testing.T) {
	entries := GlamsterdamRepricingEntries()
	if len(entries) == 0 {
		t.Fatal("expected at least one repricing entry")
	}

	// Verify some known entries.
	found := make(map[byte]GasTableEntry)
	for _, e := range entries {
		found[e.Opcode] = e
	}

	// SLOAD: 0x54, 2100 -> 800
	if e, ok := found[0x54]; !ok {
		t.Error("missing SLOAD repricing entry")
	} else {
		if e.OldCost != 2100 || e.NewCost != 800 {
			t.Errorf("SLOAD: old=%d new=%d, want old=2100 new=800", e.OldCost, e.NewCost)
		}
	}

	// CALL: 0xF1, 2600 -> 100
	if e, ok := found[0xF1]; !ok {
		t.Error("missing CALL repricing entry")
	} else {
		if e.OldCost != 2600 || e.NewCost != 100 {
			t.Errorf("CALL: old=%d new=%d, want old=2600 new=100", e.OldCost, e.NewCost)
		}
	}

	// KECCAK256: 0x20, 30 -> 45 (increase, not decrease)
	if e, ok := found[0x20]; !ok {
		t.Error("missing KECCAK256 repricing entry")
	} else {
		if e.OldCost != 30 || e.NewCost != 45 {
			t.Errorf("KECCAK256: old=%d new=%d, want old=30 new=45", e.OldCost, e.NewCost)
		}
	}
}

func TestComputeCalldataGas(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want uint64
	}{
		{
			name: "empty data",
			data: nil,
			want: 0,
		},
		{
			name: "empty slice",
			data: []byte{},
			want: 0,
		},
		{
			name: "all zero bytes",
			data: make([]byte, 100),
			// standard: 100 * 4 = 400
			// floor: 4500 + 100*1 * 16 = 4500 + 1600 = 6100
			// max(400, 6100) = 6100
			want: 6100,
		},
		{
			name: "all nonzero bytes",
			data: []byte{0xFF, 0xFE, 0xFD, 0xFC},
			// standard: 4 * 16 = 64
			// tokens: 4 * 4 = 16, floor: 4500 + 16 * 16 = 4756
			// max(64, 4756) = 4756
			want: 4756,
		},
		{
			name: "mixed bytes small",
			data: []byte{0x00, 0xFF},
			// standard: 1*4 + 1*16 = 20
			// tokens: 1 + 1*4 = 5, floor: 4500 + 5*16 = 4580
			// max(20, 4580) = 4580
			want: 4580,
		},
		{
			name: "large calldata standard wins",
			// Need enough non-zero bytes so that standard > floor.
			// standard = nz * 16, floor = 4500 + nz*4*16 = 4500 + nz*64
			// standard > floor when nz*16 > 4500 + nz*64
			// This is never true since 16 < 64. So floor always wins for
			// pure non-zero data. Let's test large zero-heavy data instead.
			// standard = z*4, floor = 4500 + z*16
			// standard > floor when z*4 > 4500 + z*16 -> -12z > 4500 -> impossible.
			// The floor always wins with GlamsterdamTxBase = 4500.
			// So we test that the floor is correctly applied.
			data: make([]byte, 1000),
			// standard: 1000 * 4 = 4000
			// floor: 4500 + 1000*16 = 20500
			want: 20500,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ComputeCalldataGas(tt.data)
			if got != tt.want {
				t.Errorf("ComputeCalldataGas(%x) = %d, want %d", tt.data, got, tt.want)
			}
		})
	}
}

func TestComputeCalldataGasFloorAlwaysHigher(t *testing.T) {
	// With the Glamsterdam base of 4500 in the floor formula, the floor
	// should always exceed standard gas for any non-empty data.
	for nz := 0; nz < 100; nz++ {
		for z := 0; z < 100; z++ {
			data := make([]byte, z+nz)
			for i := z; i < z+nz; i++ {
				data[i] = 0xFF
			}
			gas := ComputeCalldataGas(data)
			if len(data) > 0 {
				standardGas := uint64(z)*4 + uint64(nz)*16
				floorTokens := uint64(z) + uint64(nz)*4
				floorGas := uint64(4500) + floorTokens*16
				expected := floorGas
				if standardGas > floorGas {
					expected = standardGas
				}
				if gas != expected {
					t.Errorf("data(z=%d,nz=%d): got %d, want %d", z, nz, gas, expected)
				}
			}
		}
	}
}

func TestIntrinsicGasGlamsterdam(t *testing.T) {
	tests := []struct {
		name       string
		data       []byte
		isCreate   bool
		accessList types.AccessList
		wantMin    uint64
	}{
		{
			name:     "empty transaction",
			data:     nil,
			isCreate: false,
			wantMin:  GlamsterdamTxBase,
		},
		{
			name:     "contract creation",
			data:     nil,
			isCreate: true,
			wantMin:  GlamsterdamTxBase + TxCreateGas,
		},
		{
			name:     "with calldata",
			data:     []byte{0xFF, 0x00},
			isCreate: false,
			// base: 4500, calldata floor will dominate
			wantMin: GlamsterdamTxBase,
		},
		{
			name:     "with access list",
			data:     nil,
			isCreate: false,
			accessList: types.AccessList{
				{Address: types.HexToAddress("0x1234"), StorageKeys: []types.Hash{{}, {}}},
			},
			// base + 1 address + 2 keys
			wantMin: GlamsterdamTxBase + TxAccessListAddressGas + 2*TxAccessListStorageKeyGas,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IntrinsicGasGlamsterdam(tt.data, tt.isCreate, tt.accessList)
			if got < tt.wantMin {
				t.Errorf("IntrinsicGasGlamsterdam() = %d, want >= %d", got, tt.wantMin)
			}
		})
	}
}

func TestIntrinsicGasGlamsterdamLowerThanLegacy(t *testing.T) {
	// The Glamsterdam base is 4500 vs legacy 21000.
	// For an empty transaction, Glamsterdam should be cheaper.
	glamst := IntrinsicGasGlamsterdam(nil, false, nil)
	legacy, _ := IntrinsicGas(nil, false, false)

	if glamst >= legacy {
		t.Errorf("Glamsterdam intrinsic (%d) should be < legacy (%d) for empty tx",
			glamst, legacy)
	}
}

func TestIsGlamsterdamActive(t *testing.T) {
	tests := []struct {
		name      string
		blockNum  *big.Int
		forkBlock *big.Int
		want      bool
	}{
		{
			name:      "nil block number",
			blockNum:  nil,
			forkBlock: big.NewInt(100),
			want:      false,
		},
		{
			name:      "nil fork block",
			blockNum:  big.NewInt(100),
			forkBlock: nil,
			want:      false,
		},
		{
			name:      "both nil",
			blockNum:  nil,
			forkBlock: nil,
			want:      false,
		},
		{
			name:      "before fork",
			blockNum:  big.NewInt(99),
			forkBlock: big.NewInt(100),
			want:      false,
		},
		{
			name:      "at fork block",
			blockNum:  big.NewInt(100),
			forkBlock: big.NewInt(100),
			want:      true,
		},
		{
			name:      "after fork block",
			blockNum:  big.NewInt(200),
			forkBlock: big.NewInt(100),
			want:      true,
		},
		{
			name:      "genesis fork",
			blockNum:  big.NewInt(0),
			forkBlock: big.NewInt(0),
			want:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsGlamsterdamActive(tt.blockNum, tt.forkBlock)
			if got != tt.want {
				t.Errorf("IsGlamsterdamActive(%v, %v) = %v, want %v",
					tt.blockNum, tt.forkBlock, got, tt.want)
			}
		})
	}
}

func TestGlamsterdamCalldataFloorDelta(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want uint64
	}{
		{
			name: "empty data",
			data: nil,
			want: 0,
		},
		{
			name: "small data",
			data: []byte{0xFF},
			// standard: 16, floor: 4500 + 4*16 = 4564, delta: 4548
			want: 4548,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CalldataFloorDelta(tt.data)
			if got != tt.want {
				t.Errorf("CalldataFloorDelta() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestGlamsterdamSavings(t *testing.T) {
	savings := GlamsterdamSavings()

	// SLOAD: 2100 - 800 = 1300
	if s, ok := savings[0x54]; !ok {
		t.Error("expected SLOAD savings")
	} else if s != 1300 {
		t.Errorf("SLOAD savings = %d, want 1300", s)
	}

	// CALL: 2600 - 100 = 2500
	if s, ok := savings[0xF1]; !ok {
		t.Error("expected CALL savings")
	} else if s != 2500 {
		t.Errorf("CALL savings = %d, want 2500", s)
	}

	// KECCAK256: 30 -> 45 is an increase, should NOT be in savings.
	if _, ok := savings[0x20]; ok {
		t.Error("KECCAK256 increased, should not be in savings map")
	}
}

func TestGlamsterdamConstants(t *testing.T) {
	if GlamsterdamFloorTokenCost != 16 {
		t.Errorf("GlamsterdamFloorTokenCost = %d, want 16", GlamsterdamFloorTokenCost)
	}
	if GlamsterdamTxBase != 4500 {
		t.Errorf("GlamsterdamTxBase = %d, want 4500", GlamsterdamTxBase)
	}
	if GlamsterdamCalldataZeroGas != 4 {
		t.Errorf("GlamsterdamCalldataZeroGas = %d, want 4", GlamsterdamCalldataZeroGas)
	}
	if GlamsterdamCalldataNonZeroGas != 16 {
		t.Errorf("GlamsterdamCalldataNonZeroGas = %d, want 16", GlamsterdamCalldataNonZeroGas)
	}
}

func TestGasTableEntryOpcodesUnique(t *testing.T) {
	entries := GlamsterdamRepricingEntries()
	seen := make(map[byte]bool)
	for _, e := range entries {
		if seen[e.Opcode] {
			t.Errorf("duplicate opcode 0x%02X in repricing entries", e.Opcode)
		}
		seen[e.Opcode] = true
	}
}

func TestGasTableAllReductions(t *testing.T) {
	// All repriced opcodes should have OldCost != NewCost.
	entries := GlamsterdamRepricingEntries()
	for _, e := range entries {
		if e.OldCost == e.NewCost {
			t.Errorf("opcode 0x%02X has no price change: %d -> %d",
				e.Opcode, e.OldCost, e.NewCost)
		}
	}
}
