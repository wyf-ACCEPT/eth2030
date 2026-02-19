package vm

import (
	"bytes"
	"encoding/binary"
	"math/big"
	"testing"
)

// ---------- EVM Stack/Memory Fuzzing ----------

// FuzzStackPushPop pushes random uint256 values, pops them back, and verifies
// LIFO order. Must not panic on any input sequence.
func FuzzStackPushPop(f *testing.F) {
	// Seed: small values, edge cases.
	f.Add([]byte{0x00})
	f.Add([]byte{0xff, 0xff, 0xff, 0xff})
	f.Add(bytes.Repeat([]byte{0xff}, 32))

	f.Fuzz(func(t *testing.T, data []byte) {
		st := NewStack()
		// Each 32-byte chunk is one value to push.
		var pushed []*big.Int
		for len(data) >= 32 {
			val := new(big.Int).SetBytes(data[:32])
			data = data[32:]
			if err := st.Push(new(big.Int).Set(val)); err != nil {
				// Stack overflow (1024 limit) is expected, stop pushing.
				break
			}
			pushed = append(pushed, val)
		}
		// Also handle a partial trailing chunk as a smaller value.
		if len(data) > 0 && len(pushed) < stackLimit {
			val := new(big.Int).SetBytes(data)
			if err := st.Push(new(big.Int).Set(val)); err == nil {
				pushed = append(pushed, val)
			}
		}

		// Pop and verify LIFO.
		for i := len(pushed) - 1; i >= 0; i-- {
			if st.Len() == 0 {
				t.Fatalf("stack empty but expected %d more items", i+1)
			}
			got := st.Pop()
			if got.Cmp(pushed[i]) != 0 {
				t.Fatalf("LIFO mismatch at depth %d: got %s, want %s", len(pushed)-1-i, got, pushed[i])
			}
		}
		if st.Len() != 0 {
			t.Fatalf("stack should be empty, has %d items", st.Len())
		}
	})
}

// FuzzMemorySetGet sets random offsets/sizes in Memory, reads back, and verifies
// correctness. Must not panic.
func FuzzMemorySetGet(f *testing.F) {
	f.Add(uint16(0), uint16(0), []byte{})
	f.Add(uint16(0), uint16(1), []byte{0x42})
	f.Add(uint16(32), uint16(32), bytes.Repeat([]byte{0xab}, 32))

	f.Fuzz(func(t *testing.T, offsetRaw uint16, sizeRaw uint16, value []byte) {
		offset := uint64(offsetRaw)
		size := uint64(sizeRaw)

		// Cap size to prevent excessive memory allocation.
		if size > 4096 {
			size = 4096
		}
		if size == 0 {
			return
		}

		mem := NewMemory()
		// Expand memory to fit offset+size, rounded up to 32-byte word.
		needed := offset + size
		if needed < offset {
			return // overflow
		}
		words := (needed + 31) / 32
		mem.Resize(words * 32)

		// Prepare value to write (truncate or pad).
		writeVal := make([]byte, size)
		copy(writeVal, value)

		mem.Set(offset, size, writeVal)

		// Read back.
		got := mem.Get(int64(offset), int64(size))
		if !bytes.Equal(got, writeVal) {
			t.Fatalf("memory mismatch at offset=%d size=%d", offset, size)
		}
	})
}

// ---------- Opcode Fuzzing ----------

// makeTestScope creates a minimal EVM, Contract, Memory, and Stack for opcode testing.
func makeTestScope() (*EVM, *Contract, *Memory, *Stack) {
	evm := NewEVM(BlockContext{
		BlockNumber: big.NewInt(1),
		BaseFee:     big.NewInt(1),
	}, TxContext{
		GasPrice: big.NewInt(1),
	}, Config{})
	contract := NewContract(
		[20]byte{},
		[20]byte{},
		big.NewInt(0),
		1_000_000,
	)
	mem := NewMemory()
	stack := NewStack()
	return evm, contract, mem, stack
}

// bigFrom32 reads up to 32 bytes from data and returns a non-negative big.Int.
func bigFrom32(data []byte) *big.Int {
	if len(data) > 32 {
		data = data[:32]
	}
	return new(big.Int).SetBytes(data)
}

// FuzzArithmeticOps exercises all arithmetic opcodes with random 256-bit inputs.
func FuzzArithmeticOps(f *testing.F) {
	f.Add([]byte{0x00, 0x00}, []byte{0x00, 0x00}, []byte{0x01})
	f.Add(bytes.Repeat([]byte{0xff}, 32), bytes.Repeat([]byte{0xff}, 32), bytes.Repeat([]byte{0xff}, 32))
	f.Add([]byte{0x01}, []byte{0x02}, []byte{0x03})

	type arithOp struct {
		name string
		fn   executionFunc
		args int // 2 or 3
	}
	ops := []arithOp{
		{"ADD", opAdd, 2},
		{"SUB", opSub, 2},
		{"MUL", opMul, 2},
		{"DIV", opDiv, 2},
		{"SDIV", opSdiv, 2},
		{"MOD", opMod, 2},
		{"SMOD", opSmod, 2},
		{"EXP", opExp, 2},
		{"ADDMOD", opAddmod, 3},
		{"MULMOD", opMulmod, 3},
	}

	f.Fuzz(func(t *testing.T, a, b, c []byte) {
		for _, op := range ops {
			evm, contract, mem, stack := makeTestScope()
			var pc uint64

			if op.args == 3 {
				_ = stack.Push(bigFrom32(c))
			}
			_ = stack.Push(bigFrom32(b))
			_ = stack.Push(bigFrom32(a))

			_, err := op.fn(&pc, evm, contract, mem, stack)
			if err != nil {
				// Opcodes should not return errors for valid stack setups.
				t.Fatalf("%s returned unexpected error: %v", op.name, err)
			}
			// Verify result is within uint256 range.
			result := stack.Peek()
			if result.Sign() < 0 || result.BitLen() > 256 {
				t.Fatalf("%s produced out-of-range result: %s", op.name, result)
			}
		}
	})
}

// FuzzBitwiseOps exercises bitwise opcodes with random inputs.
func FuzzBitwiseOps(f *testing.F) {
	f.Add([]byte{0x00}, []byte{0x00})
	f.Add(bytes.Repeat([]byte{0xff}, 32), bytes.Repeat([]byte{0xff}, 32))
	f.Add([]byte{0x01, 0x02, 0x03}, []byte{0x10})

	type bitwiseOp struct {
		name string
		fn   executionFunc
		args int // 1 or 2
	}
	ops := []bitwiseOp{
		{"AND", opAnd, 2},
		{"OR", opOr, 2},
		{"XOR", opXor, 2},
		{"NOT", opNot, 1},
		{"BYTE", opByte, 2},
		{"SHL", opSHL, 2},
		{"SHR", opSHR, 2},
		{"SAR", opSAR, 2},
	}

	f.Fuzz(func(t *testing.T, a, b []byte) {
		for _, op := range ops {
			evm, contract, mem, stack := makeTestScope()
			var pc uint64

			if op.args == 2 {
				_ = stack.Push(bigFrom32(b))
			}
			_ = stack.Push(bigFrom32(a))

			_, err := op.fn(&pc, evm, contract, mem, stack)
			if err != nil {
				t.Fatalf("%s returned unexpected error: %v", op.name, err)
			}
			result := stack.Peek()
			if result.Sign() < 0 || result.BitLen() > 256 {
				t.Fatalf("%s produced out-of-range result: %s", op.name, result)
			}
		}
	})
}

// FuzzComparisonOps exercises comparison opcodes with random inputs.
func FuzzComparisonOps(f *testing.F) {
	f.Add([]byte{0x00}, []byte{0x00})
	f.Add([]byte{0x01}, []byte{0x02})
	f.Add(bytes.Repeat([]byte{0xff}, 32), bytes.Repeat([]byte{0x00}, 32))

	type cmpOp struct {
		name string
		fn   executionFunc
		args int // 1 or 2
	}
	ops := []cmpOp{
		{"LT", opLt, 2},
		{"GT", opGt, 2},
		{"SLT", opSlt, 2},
		{"SGT", opSgt, 2},
		{"EQ", opEq, 2},
		{"ISZERO", opIsZero, 1},
	}

	f.Fuzz(func(t *testing.T, a, b []byte) {
		for _, op := range ops {
			evm, contract, mem, stack := makeTestScope()
			var pc uint64

			if op.args == 2 {
				_ = stack.Push(bigFrom32(b))
			}
			_ = stack.Push(bigFrom32(a))

			_, err := op.fn(&pc, evm, contract, mem, stack)
			if err != nil {
				t.Fatalf("%s returned unexpected error: %v", op.name, err)
			}
			result := stack.Peek()
			// Comparison results must be 0 or 1.
			if result.Cmp(big.NewInt(0)) != 0 && result.Cmp(big.NewInt(1)) != 0 {
				t.Fatalf("%s produced non-boolean result: %s", op.name, result)
			}
		}
	})
}

// ---------- Precompile Fuzzing ----------

// FuzzPrecompileEcRecover feeds random bytes to the ecRecover precompile.
// Must not panic; output is either 32 bytes or nil.
func FuzzPrecompileEcRecover(f *testing.F) {
	// Valid ecrecover input: hash(32) + v(32) + r(32) + s(32) = 128 bytes.
	f.Add(make([]byte, 128))
	f.Add([]byte{})
	// A crafted input with v=27 but invalid r,s.
	crafted := make([]byte, 128)
	crafted[63] = 27
	f.Add(crafted)

	p := &ecrecover{}
	f.Fuzz(func(t *testing.T, data []byte) {
		out, err := p.Run(data)
		// ecRecover returns nil output for invalid inputs (not an error).
		if err != nil {
			t.Fatalf("ecrecover.Run returned error: %v", err)
		}
		if out != nil && len(out) != 32 {
			t.Fatalf("ecrecover.Run returned %d bytes, expected 0 or 32", len(out))
		}
	})
}

// FuzzPrecompileBN254Add feeds random bytes to the bn256Add precompile.
func FuzzPrecompileBN254Add(f *testing.F) {
	f.Add(make([]byte, 128))
	f.Add([]byte{})
	f.Add(bytes.Repeat([]byte{0xff}, 128))

	p := &bn256Add{}
	f.Fuzz(func(t *testing.T, data []byte) {
		// Must not panic. Error is acceptable for invalid curve points.
		_, _ = p.Run(data)
	})
}

// FuzzPrecompileBN254Mul feeds random bytes to the bn256ScalarMul precompile.
func FuzzPrecompileBN254Mul(f *testing.F) {
	f.Add(make([]byte, 96))
	f.Add([]byte{})
	f.Add(bytes.Repeat([]byte{0xff}, 96))

	p := &bn256ScalarMul{}
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = p.Run(data)
	})
}

// FuzzPrecompileBN254Pairing feeds random bytes to the bn256Pairing precompile.
func FuzzPrecompileBN254Pairing(f *testing.F) {
	f.Add(make([]byte, 192))
	f.Add([]byte{})
	f.Add(bytes.Repeat([]byte{0xff}, 192))

	p := &bn256Pairing{}
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = p.Run(data)
	})
}

// FuzzPrecompileModExp feeds random bytes to the modexp precompile.
// This is important because modexp has complex length parsing that must be robust.
func FuzzPrecompileModExp(f *testing.F) {
	// Minimal valid: baseLen=1, expLen=1, modLen=1, base=2, exp=3, mod=5.
	validInput := make([]byte, 96+3)
	validInput[31] = 1 // baseLen = 1
	validInput[63] = 1 // expLen = 1
	validInput[95] = 1 // modLen = 1
	validInput[96] = 2 // base = 2
	validInput[97] = 3 // exp = 3
	validInput[98] = 5 // mod = 5
	f.Add(validInput)
	f.Add([]byte{})
	f.Add(make([]byte, 96))
	// Large length fields (but actual data is short).
	largeLens := make([]byte, 96)
	largeLens[31] = 32
	largeLens[63] = 32
	largeLens[95] = 32
	f.Add(largeLens)

	p := &bigModExp{}
	f.Fuzz(func(t *testing.T, data []byte) {
		// Must not panic. Error is acceptable for overflow or zero mod.
		_, _ = p.Run(data)
		// Also exercise RequiredGas with the same data.
		_ = p.RequiredGas(data)
	})
}

// FuzzPrecompileBlake2F feeds random bytes to the blake2F precompile.
func FuzzPrecompileBlake2F(f *testing.F) {
	// Valid: 213 bytes with 1 round and final byte = 1.
	valid := make([]byte, 213)
	binary.BigEndian.PutUint32(valid[:4], 1) // 1 round
	valid[212] = 1                            // final = true
	f.Add(valid)
	f.Add([]byte{})
	f.Add(make([]byte, 213))

	p := &blake2F{}
	f.Fuzz(func(t *testing.T, data []byte) {
		// Must not panic. Error is expected for len != 213 or invalid final byte.
		_, _ = p.Run(data)
	})
}

// FuzzPrecompileP256Verify feeds random bytes to the p256Verify precompile.
func FuzzPrecompileP256Verify(f *testing.F) {
	f.Add(make([]byte, 160))
	f.Add([]byte{})
	f.Add(bytes.Repeat([]byte{0xff}, 160))

	p := &p256Verify{}
	f.Fuzz(func(t *testing.T, data []byte) {
		// Must not panic. Returns nil for invalid inputs (no error).
		out, err := p.Run(data)
		if err != nil {
			t.Fatalf("p256Verify.Run returned error: %v", err)
		}
		// Output is either nil (invalid/failed) or 32 bytes with value 1.
		if out != nil && len(out) != 32 {
			t.Fatalf("p256Verify.Run returned %d bytes, expected 0 or 32", len(out))
		}
	})
}

// FuzzPrecompileBLS12G1Add feeds random bytes to the bls12G1Add precompile.
func FuzzPrecompileBLS12G1Add(f *testing.F) {
	// Valid input size: 2 * 128 = 256 bytes (two G1 points).
	f.Add(make([]byte, 256))
	f.Add([]byte{})
	f.Add(bytes.Repeat([]byte{0xff}, 256))

	p := &bls12G1Add{}
	f.Fuzz(func(t *testing.T, data []byte) {
		// Must not panic. Error is acceptable for invalid points or wrong length.
		_, _ = p.Run(data)
	})
}
