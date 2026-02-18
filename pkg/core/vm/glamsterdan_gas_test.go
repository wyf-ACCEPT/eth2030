package vm

import (
	"encoding/binary"
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

// TestGlamsterdanOpcodeGasCosts verifies that the Glamsterdan jump table
// applies EIP-7904 gas repricing to the correct opcodes.
func TestGlamsterdanOpcodeGasCosts(t *testing.T) {
	glamsterdan := NewGlamsterdanJumpTable()
	prague := NewPragueJumpTable()

	tests := []struct {
		name      string
		op        OpCode
		preGas    uint64 // pre-Glamsterdan (Prague) gas cost
		postGas   uint64 // post-Glamsterdan gas cost
		unchanged bool   // true if gas should NOT change
	}{
		{"DIV", DIV, GasFastStep, GasDivGlamsterdan, false},
		{"SDIV", SDIV, GasFastStep, GasSdivGlamsterdan, false},
		{"MOD", MOD, GasFastStep, GasModGlamsterdan, false},
		{"MULMOD", MULMOD, GasMidStep, GasMulmodGlamsterdan, false},
		{"KECCAK256", KECCAK256, GasKeccak256, GasKeccak256Glamsterdan, false},
		// ADDMOD stays at 8 (no change per EIP-7904).
		{"ADDMOD", ADDMOD, GasMidStep, GasMidStep, true},
		// SMOD stays at 5 (no change per EIP-7904).
		{"SMOD", SMOD, GasFastStep, GasFastStep, true},
	}

	for _, tt := range tests {
		t.Run(tt.name+"_pre", func(t *testing.T) {
			op := prague[tt.op]
			if op == nil {
				t.Fatalf("opcode %s not found in Prague table", tt.name)
			}
			if op.constantGas != tt.preGas {
				t.Errorf("Prague %s constantGas = %d, want %d", tt.name, op.constantGas, tt.preGas)
			}
		})

		t.Run(tt.name+"_post", func(t *testing.T) {
			op := glamsterdan[tt.op]
			if op == nil {
				t.Fatalf("opcode %s not found in Glamsterdan table", tt.name)
			}
			if op.constantGas != tt.postGas {
				t.Errorf("Glamsterdan %s constantGas = %d, want %d", tt.name, op.constantGas, tt.postGas)
			}
		})

		if tt.unchanged {
			t.Run(tt.name+"_unchanged", func(t *testing.T) {
				preCost := prague[tt.op].constantGas
				postCost := glamsterdan[tt.op].constantGas
				if preCost != postCost {
					t.Errorf("%s gas changed from %d to %d, expected no change", tt.name, preCost, postCost)
				}
			})
		}
	}
}

// TestGlamsterdanSpecificGasValues verifies the exact gas values from EIP-7904.
func TestGlamsterdanSpecificGasValues(t *testing.T) {
	if GasDivGlamsterdan != 15 {
		t.Errorf("GasDivGlamsterdan = %d, want 15", GasDivGlamsterdan)
	}
	if GasSdivGlamsterdan != 20 {
		t.Errorf("GasSdivGlamsterdan = %d, want 20", GasSdivGlamsterdan)
	}
	if GasModGlamsterdan != 12 {
		t.Errorf("GasModGlamsterdan = %d, want 12", GasModGlamsterdan)
	}
	if GasMulmodGlamsterdan != 11 {
		t.Errorf("GasMulmodGlamsterdan = %d, want 11", GasMulmodGlamsterdan)
	}
	if GasKeccak256Glamsterdan != 45 {
		t.Errorf("GasKeccak256Glamsterdan = %d, want 45", GasKeccak256Glamsterdan)
	}
}

// TestGlamsterdanPrecompileGas verifies EIP-7904 precompile gas changes.
func TestGlamsterdanPrecompileGas(t *testing.T) {
	tests := []struct {
		name    string
		addr    types.Address
		input   []byte
		preGas  uint64 // Cancun gas cost
		postGas uint64 // Glamsterdan gas cost
	}{
		{
			name:    "ECADD",
			addr:    types.BytesToAddress([]byte{6}),
			input:   nil,
			preGas:  150,
			postGas: GasECADDGlamsterdan, // 314
		},
		{
			name:    "POINT_EVALUATION",
			addr:    types.BytesToAddress([]byte{0x0a}),
			input:   nil,
			preGas:  50000,
			postGas: GasPointEvalGlamsterdan, // 89363
		},
	}

	for _, tt := range tests {
		t.Run(tt.name+"_pre", func(t *testing.T) {
			p, ok := PrecompiledContractsCancun[tt.addr]
			if !ok {
				t.Fatalf("precompile %s not found in Cancun set", tt.name)
			}
			gas := p.RequiredGas(tt.input)
			if gas != tt.preGas {
				t.Errorf("Cancun %s gas = %d, want %d", tt.name, gas, tt.preGas)
			}
		})

		t.Run(tt.name+"_post", func(t *testing.T) {
			p, ok := PrecompiledContractsGlamsterdan[tt.addr]
			if !ok {
				t.Fatalf("precompile %s not found in Glamsterdan set", tt.name)
			}
			gas := p.RequiredGas(tt.input)
			if gas != tt.postGas {
				t.Errorf("Glamsterdan %s gas = %d, want %d", tt.name, gas, tt.postGas)
			}
		})
	}
}

// TestGlamsterdanBlake2FGas verifies the BLAKE2F gas with the new constant + per-round cost.
func TestGlamsterdanBlake2FGas(t *testing.T) {
	blake2fAddr := types.BytesToAddress([]byte{9})

	tests := []struct {
		name    string
		rounds  uint32
		preGas  uint64
		postGas uint64
	}{
		{
			name:    "0_rounds",
			rounds:  0,
			preGas:  0,
			postGas: GasBlake2fConstGlamsterdan, // 170 + 0*2 = 170
		},
		{
			name:    "1_round",
			rounds:  1,
			preGas:  1,
			postGas: GasBlake2fConstGlamsterdan + 1*GasBlake2fPerRoundGlamsterdan, // 170 + 2 = 172
		},
		{
			name:    "12_rounds",
			rounds:  12,
			preGas:  12,
			postGas: GasBlake2fConstGlamsterdan + 12*GasBlake2fPerRoundGlamsterdan, // 170 + 24 = 194
		},
		{
			name:    "100_rounds",
			rounds:  100,
			preGas:  100,
			postGas: GasBlake2fConstGlamsterdan + 100*GasBlake2fPerRoundGlamsterdan, // 170 + 200 = 370
		},
	}

	for _, tt := range tests {
		input := make([]byte, 4)
		binary.BigEndian.PutUint32(input, tt.rounds)

		t.Run(tt.name+"_pre", func(t *testing.T) {
			p := PrecompiledContractsCancun[blake2fAddr]
			gas := p.RequiredGas(input)
			if gas != tt.preGas {
				t.Errorf("Cancun BLAKE2F(%d rounds) = %d, want %d", tt.rounds, gas, tt.preGas)
			}
		})

		t.Run(tt.name+"_post", func(t *testing.T) {
			p := PrecompiledContractsGlamsterdan[blake2fAddr]
			gas := p.RequiredGas(input)
			if gas != tt.postGas {
				t.Errorf("Glamsterdan BLAKE2F(%d rounds) = %d, want %d", tt.rounds, gas, tt.postGas)
			}
		})
	}
}

// TestGlamsterdanECPairingGas verifies BN256 pairing gas with the updated per-pair cost.
func TestGlamsterdanECPairingGas(t *testing.T) {
	pairingAddr := types.BytesToAddress([]byte{8})

	tests := []struct {
		name    string
		pairs   int
		preGas  uint64
		postGas uint64
	}{
		{
			name:    "0_pairs",
			pairs:   0,
			preGas:  45000,
			postGas: GasECPairingConstGlamsterdan,
		},
		{
			name:    "1_pair",
			pairs:   1,
			preGas:  45000 + 34000,
			postGas: GasECPairingConstGlamsterdan + GasECPairingPerPairGlamsterdan,
		},
		{
			name:    "3_pairs",
			pairs:   3,
			preGas:  45000 + 3*34000,
			postGas: GasECPairingConstGlamsterdan + 3*GasECPairingPerPairGlamsterdan,
		},
	}

	for _, tt := range tests {
		input := make([]byte, tt.pairs*192)

		t.Run(tt.name+"_pre", func(t *testing.T) {
			p := PrecompiledContractsCancun[pairingAddr]
			gas := p.RequiredGas(input)
			if gas != tt.preGas {
				t.Errorf("Cancun pairing(%d pairs) = %d, want %d", tt.pairs, gas, tt.preGas)
			}
		})

		t.Run(tt.name+"_post", func(t *testing.T) {
			p := PrecompiledContractsGlamsterdan[pairingAddr]
			gas := p.RequiredGas(input)
			if gas != tt.postGas {
				t.Errorf("Glamsterdan pairing(%d pairs) = %d, want %d", tt.pairs, gas, tt.postGas)
			}
		})
	}
}

// TestGlamsterdanUnchangedPrecompiles verifies precompiles that should NOT
// change gas cost between Cancun and Glamsterdan.
func TestGlamsterdanUnchangedPrecompiles(t *testing.T) {
	tests := []struct {
		name  string
		addr  types.Address
		input []byte
	}{
		{"ECRECOVER", types.BytesToAddress([]byte{1}), nil},
		{"SHA256", types.BytesToAddress([]byte{2}), nil},
		{"RIPEMD160", types.BytesToAddress([]byte{3}), nil},
		{"IDENTITY", types.BytesToAddress([]byte{4}), nil},
		{"MODEXP", types.BytesToAddress([]byte{5}), nil},
		{"BN256SCALARMUL", types.BytesToAddress([]byte{7}), nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			preCost := PrecompiledContractsCancun[tt.addr].RequiredGas(tt.input)
			postCost := PrecompiledContractsGlamsterdan[tt.addr].RequiredGas(tt.input)
			if preCost != postCost {
				t.Errorf("%s gas changed from %d to %d, expected no change", tt.name, preCost, postCost)
			}
		})
	}
}

// TestGlamsterdanJumpTableInheritance verifies that the Glamsterdan table
// inherits all Prague opcodes and only modifies the EIP-7904 opcodes.
func TestGlamsterdanJumpTableInheritance(t *testing.T) {
	glamsterdan := NewGlamsterdanJumpTable()
	prague := NewPragueJumpTable()

	// EIP-7904 modified opcodes.
	modified := map[OpCode]bool{
		DIV: true, SDIV: true, MOD: true, MULMOD: true, KECCAK256: true,
	}

	// Check that non-modified opcodes have identical gas costs.
	for i := 0; i < 256; i++ {
		op := OpCode(i)
		if modified[op] {
			continue
		}
		pOp := prague[op]
		gOp := glamsterdan[op]
		if pOp == nil && gOp == nil {
			continue
		}
		if pOp == nil || gOp == nil {
			t.Errorf("opcode 0x%02x: Prague nil=%v Glamsterdan nil=%v",
				i, pOp == nil, gOp == nil)
			continue
		}
		if pOp.constantGas != gOp.constantGas {
			t.Errorf("opcode 0x%02x (%s): constantGas changed from %d to %d",
				i, op.String(), pOp.constantGas, gOp.constantGas)
		}
	}
}

// TestSelectJumpTableGlamsterdan verifies that SelectJumpTable picks the
// Glamsterdan table when the fork flag is set.
func TestSelectJumpTableGlamsterdan(t *testing.T) {
	rules := ForkRules{IsGlamsterdan: true}
	jt := SelectJumpTable(rules)

	// The Glamsterdan table should have DIV gas = 15.
	if jt[DIV].constantGas != GasDivGlamsterdan {
		t.Errorf("SelectJumpTable(Glamsterdan) DIV gas = %d, want %d",
			jt[DIV].constantGas, GasDivGlamsterdan)
	}

	// The Prague table should have DIV gas = 5.
	rules = ForkRules{IsPrague: true}
	jt = SelectJumpTable(rules)
	if jt[DIV].constantGas != GasFastStep {
		t.Errorf("SelectJumpTable(Prague) DIV gas = %d, want %d",
			jt[DIV].constantGas, GasFastStep)
	}
}

// TestSelectPrecompilesGlamsterdan verifies that SelectPrecompiles picks the
// correct precompile set.
func TestSelectPrecompilesGlamsterdan(t *testing.T) {
	// Glamsterdan rules should yield Glamsterdan precompiles.
	rules := ForkRules{IsGlamsterdan: true}
	precompiles := SelectPrecompiles(rules)
	ecaddAddr := types.BytesToAddress([]byte{6})
	p := precompiles[ecaddAddr]
	if p.RequiredGas(nil) != GasECADDGlamsterdan {
		t.Errorf("Glamsterdan ECADD gas = %d, want %d", p.RequiredGas(nil), GasECADDGlamsterdan)
	}

	// Prague rules should yield Cancun precompiles.
	rules = ForkRules{IsPrague: true}
	precompiles = SelectPrecompiles(rules)
	p = precompiles[ecaddAddr]
	if p.RequiredGas(nil) != 150 {
		t.Errorf("Prague ECADD gas = %d, want 150", p.RequiredGas(nil))
	}
}

// TestGlamsterdanPrecompileCount verifies both precompile maps have the same
// number of entries.
func TestGlamsterdanPrecompileCount(t *testing.T) {
	if len(PrecompiledContractsCancun) != len(PrecompiledContractsGlamsterdan) {
		t.Errorf("precompile count: Cancun=%d Glamsterdan=%d",
			len(PrecompiledContractsCancun), len(PrecompiledContractsGlamsterdan))
	}
}

// TestGlamsterdanEVMPrecompileIntegration verifies that the EVM correctly
// dispatches to the right precompile map when it is set.
func TestGlamsterdanEVMPrecompileIntegration(t *testing.T) {
	evm := NewEVM(BlockContext{}, TxContext{}, Config{})

	// Default: Cancun precompiles.
	ecaddAddr := types.BytesToAddress([]byte{6})
	p, ok := evm.precompile(ecaddAddr)
	if !ok {
		t.Fatal("expected ECADD precompile in default EVM")
	}
	if p.RequiredGas(nil) != 150 {
		t.Errorf("default EVM ECADD gas = %d, want 150", p.RequiredGas(nil))
	}

	// Set Glamsterdan precompiles.
	evm.SetPrecompiles(PrecompiledContractsGlamsterdan)
	p, ok = evm.precompile(ecaddAddr)
	if !ok {
		t.Fatal("expected ECADD precompile after SetPrecompiles")
	}
	if p.RequiredGas(nil) != GasECADDGlamsterdan {
		t.Errorf("Glamsterdan EVM ECADD gas = %d, want %d", p.RequiredGas(nil), GasECADDGlamsterdan)
	}
}

// TestGlamsterdanBlake2FShortInput verifies that BLAKE2F with short input
// returns 0 gas in both pre and post Glamsterdan.
func TestGlamsterdanBlake2FShortInput(t *testing.T) {
	blake2fAddr := types.BytesToAddress([]byte{9})
	shortInput := []byte{0x01, 0x02} // less than 4 bytes

	preGas := PrecompiledContractsCancun[blake2fAddr].RequiredGas(shortInput)
	if preGas != 0 {
		t.Errorf("Cancun BLAKE2F short input gas = %d, want 0", preGas)
	}

	postGas := PrecompiledContractsGlamsterdan[blake2fAddr].RequiredGas(shortInput)
	if postGas != 0 {
		t.Errorf("Glamsterdan BLAKE2F short input gas = %d, want 0", postGas)
	}
}
