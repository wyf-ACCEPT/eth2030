package vm

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"testing"
)

// testVector matches the go-ethereum JSON test vector format.
type testVector struct {
	X        string `json:"X"`
	Y        string `json:"Y"`
	Expected string `json:"Expected"`
}

// loadTestVectors reads and unmarshals a JSON test vector file.
func loadTestVectors(t *testing.T, filename string) []testVector {
	t.Helper()
	data, err := os.ReadFile(filename)
	if err != nil {
		t.Fatalf("failed to read %s: %v", filename, err)
	}
	var vectors []testVector
	if err := json.Unmarshal(data, &vectors); err != nil {
		t.Fatalf("failed to unmarshal %s: %v", filename, err)
	}
	return vectors
}

// hexToBig parses a 64-char hex string into a big.Int.
func hexToBig(s string) *big.Int {
	b, _ := hex.DecodeString(s)
	return new(big.Int).SetBytes(b)
}

// bigToHex64 formats a big.Int as a zero-padded 64-char hex string.
func bigToHex64(v *big.Int) string {
	b := v.Bytes()
	if len(b) > 32 {
		b = b[len(b)-32:]
	}
	padded := make([]byte, 32)
	copy(padded[32-len(b):], b)
	return hex.EncodeToString(padded)
}

// runOpcodeTest executes an opcode function against all vectors from a JSON file.
// In go-ethereum's test convention, X is pushed first (bottom) and Y second (top).
// The opcode pops Y (top) first, then peeks X (second).
func runOpcodeTest(t *testing.T, file string, op executionFunc) {
	t.Helper()
	vectors := loadTestVectors(t, file)
	evm, contract, mem, _ := setupTest()
	pc := uint64(0)

	for i, v := range vectors {
		st := NewStack()
		st.Push(hexToBig(v.X))
		st.Push(hexToBig(v.Y))

		_, err := op(&pc, evm, contract, mem, st)
		if err != nil {
			t.Fatalf("vector %d: op error: %v", i, err)
		}
		if st.Len() != 1 {
			t.Fatalf("vector %d: stack len = %d, want 1", i, st.Len())
		}
		got := bigToHex64(st.Peek())
		if got != v.Expected {
			t.Errorf("vector %d: op(%s, %s) = %s, want %s", i, v.X, v.Y, got, v.Expected)
		}
	}
	t.Logf("passed %d vectors", len(vectors))
}

func TestJsonAdd(t *testing.T) {
	runOpcodeTest(t, "testdata/testcases_add.json", opAdd)
}

func TestJsonSub(t *testing.T) {
	runOpcodeTest(t, "testdata/testcases_sub.json", opSub)
}

func TestJsonMul(t *testing.T) {
	runOpcodeTest(t, "testdata/testcases_mul.json", opMul)
}

func TestJsonDiv(t *testing.T) {
	runOpcodeTest(t, "testdata/testcases_div.json", opDiv)
}

func TestJsonSdiv(t *testing.T) {
	runOpcodeTest(t, "testdata/testcases_sdiv.json", opSdiv)
}

func TestJsonMod(t *testing.T) {
	runOpcodeTest(t, "testdata/testcases_mod.json", opMod)
}

func TestJsonSmod(t *testing.T) {
	runOpcodeTest(t, "testdata/testcases_smod.json", opSmod)
}

func TestJsonExp(t *testing.T) {
	runOpcodeTest(t, "testdata/testcases_exp.json", opExp)
}

func TestJsonAnd(t *testing.T) {
	runOpcodeTest(t, "testdata/testcases_and.json", opAnd)
}

func TestJsonOr(t *testing.T) {
	runOpcodeTest(t, "testdata/testcases_or.json", opOr)
}

func TestJsonXor(t *testing.T) {
	runOpcodeTest(t, "testdata/testcases_xor.json", opXor)
}

func TestJsonByte(t *testing.T) {
	runOpcodeTest(t, "testdata/testcases_byte.json", opByte)
}

func TestJsonSHL(t *testing.T) {
	runOpcodeTest(t, "testdata/testcases_shl.json", opSHL)
}

func TestJsonSHR(t *testing.T) {
	runOpcodeTest(t, "testdata/testcases_shr.json", opSHR)
}

func TestJsonSAR(t *testing.T) {
	runOpcodeTest(t, "testdata/testcases_sar.json", opSAR)
}

func TestJsonLt(t *testing.T) {
	runOpcodeTest(t, "testdata/testcases_lt.json", opLt)
}

func TestJsonGt(t *testing.T) {
	runOpcodeTest(t, "testdata/testcases_gt.json", opGt)
}

func TestJsonSlt(t *testing.T) {
	runOpcodeTest(t, "testdata/testcases_slt.json", opSlt)
}

func TestJsonSgt(t *testing.T) {
	runOpcodeTest(t, "testdata/testcases_sgt.json", opSgt)
}

func TestJsonEq(t *testing.T) {
	runOpcodeTest(t, "testdata/testcases_eq.json", opEq)
}

func TestJsonSignExtend(t *testing.T) {
	runOpcodeTest(t, "testdata/testcases_signext.json", opSignExtend)
}

// TestJsonAllOpcodes runs all 21 opcode test vector files as subtests.
func TestJsonAllOpcodes(t *testing.T) {
	tests := []struct {
		name string
		file string
		op   executionFunc
	}{
		{"ADD", "testdata/testcases_add.json", opAdd},
		{"SUB", "testdata/testcases_sub.json", opSub},
		{"MUL", "testdata/testcases_mul.json", opMul},
		{"DIV", "testdata/testcases_div.json", opDiv},
		{"SDIV", "testdata/testcases_sdiv.json", opSdiv},
		{"MOD", "testdata/testcases_mod.json", opMod},
		{"SMOD", "testdata/testcases_smod.json", opSmod},
		{"EXP", "testdata/testcases_exp.json", opExp},
		{"AND", "testdata/testcases_and.json", opAnd},
		{"OR", "testdata/testcases_or.json", opOr},
		{"XOR", "testdata/testcases_xor.json", opXor},
		{"BYTE", "testdata/testcases_byte.json", opByte},
		{"SHL", "testdata/testcases_shl.json", opSHL},
		{"SHR", "testdata/testcases_shr.json", opSHR},
		{"SAR", "testdata/testcases_sar.json", opSAR},
		{"LT", "testdata/testcases_lt.json", opLt},
		{"GT", "testdata/testcases_gt.json", opGt},
		{"SLT", "testdata/testcases_slt.json", opSlt},
		{"SGT", "testdata/testcases_sgt.json", opSgt},
		{"EQ", "testdata/testcases_eq.json", opEq},
		{"SIGNEXTEND", "testdata/testcases_signext.json", opSignExtend},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			vectors := loadTestVectors(t, tc.file)
			evm, contract, mem, _ := setupTest()
			pc := uint64(0)

			for i, v := range vectors {
				st := NewStack()
				st.Push(hexToBig(v.X))
				st.Push(hexToBig(v.Y))

				_, err := tc.op(&pc, evm, contract, mem, st)
				if err != nil {
					t.Fatalf("vector %d: op error: %v", i, err)
				}
				if st.Len() != 1 {
					t.Fatalf("vector %d: stack len = %d, want 1", i, st.Len())
				}
				got := bigToHex64(st.Peek())
				if got != v.Expected {
					t.Errorf("vector %d: %s(%s, %s) = %s, want %s",
						i, tc.name, v.X, v.Y, got, v.Expected)
				}
			}
			t.Logf("%s: passed %d vectors", tc.name, len(vectors))
		})
	}
}

// TestJsonVectorCounts ensures all 21 test vector files exist and have vectors.
func TestJsonVectorCounts(t *testing.T) {
	files := []string{
		"testdata/testcases_add.json",
		"testdata/testcases_sub.json",
		"testdata/testcases_mul.json",
		"testdata/testcases_div.json",
		"testdata/testcases_sdiv.json",
		"testdata/testcases_mod.json",
		"testdata/testcases_smod.json",
		"testdata/testcases_exp.json",
		"testdata/testcases_and.json",
		"testdata/testcases_or.json",
		"testdata/testcases_xor.json",
		"testdata/testcases_byte.json",
		"testdata/testcases_shl.json",
		"testdata/testcases_shr.json",
		"testdata/testcases_sar.json",
		"testdata/testcases_lt.json",
		"testdata/testcases_gt.json",
		"testdata/testcases_slt.json",
		"testdata/testcases_sgt.json",
		"testdata/testcases_eq.json",
		"testdata/testcases_signext.json",
	}

	total := 0
	for _, f := range files {
		vectors := loadTestVectors(t, f)
		if len(vectors) == 0 {
			t.Errorf("%s has 0 vectors", f)
		}
		total += len(vectors)
		t.Logf("%s: %d vectors", f, len(vectors))
	}
	fmt.Printf("total test vectors across 21 files: %d\n", total)
}
