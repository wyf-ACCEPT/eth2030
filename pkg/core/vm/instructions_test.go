package vm

import (
	"math/big"
	"testing"
)

func setupTest() (*EVM, *Contract, *Memory, *Stack) {
	evm := NewEVM(BlockContext{}, TxContext{}, Config{})
	contract := NewContract([20]byte{}, [20]byte{}, big.NewInt(0), 1000000)
	mem := NewMemory()
	st := NewStack()
	return evm, contract, mem, st
}

func TestOpAdd(t *testing.T) {
	evm, contract, mem, st := setupTest()
	pc := uint64(0)

	st.Push(big.NewInt(10))
	st.Push(big.NewInt(20))
	opAdd(&pc, evm, contract, mem, st)

	if st.Len() != 1 {
		t.Fatalf("stack len = %d, want 1", st.Len())
	}
	if st.Peek().Int64() != 30 {
		t.Errorf("10 + 20 = %d, want 30", st.Peek().Int64())
	}
}

func TestOpAddOverflow(t *testing.T) {
	evm, contract, mem, st := setupTest()
	pc := uint64(0)

	// max uint256 + 1 should wrap to 0
	max := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
	st.Push(big.NewInt(1))
	st.Push(max)
	opAdd(&pc, evm, contract, mem, st)

	if st.Peek().Sign() != 0 {
		t.Errorf("max + 1 = %s, want 0", st.Peek().String())
	}
}

func TestOpSub(t *testing.T) {
	evm, contract, mem, st := setupTest()
	pc := uint64(0)

	st.Push(big.NewInt(7))
	st.Push(big.NewInt(20))
	opSub(&pc, evm, contract, mem, st)

	if st.Peek().Int64() != 13 {
		t.Errorf("20 - 7 = %d, want 13", st.Peek().Int64())
	}
}

func TestOpMul(t *testing.T) {
	evm, contract, mem, st := setupTest()
	pc := uint64(0)

	st.Push(big.NewInt(6))
	st.Push(big.NewInt(7))
	opMul(&pc, evm, contract, mem, st)

	if st.Peek().Int64() != 42 {
		t.Errorf("7 * 6 = %d, want 42", st.Peek().Int64())
	}
}

func TestOpDiv(t *testing.T) {
	evm, contract, mem, st := setupTest()
	pc := uint64(0)

	st.Push(big.NewInt(3))
	st.Push(big.NewInt(10))
	opDiv(&pc, evm, contract, mem, st)

	if st.Peek().Int64() != 3 {
		t.Errorf("10 / 3 = %d, want 3", st.Peek().Int64())
	}
}

func TestOpDivByZero(t *testing.T) {
	evm, contract, mem, st := setupTest()
	pc := uint64(0)

	st.Push(big.NewInt(0))
	st.Push(big.NewInt(10))
	opDiv(&pc, evm, contract, mem, st)

	if st.Peek().Sign() != 0 {
		t.Errorf("10 / 0 = %d, want 0", st.Peek().Int64())
	}
}

func TestOpMod(t *testing.T) {
	evm, contract, mem, st := setupTest()
	pc := uint64(0)

	st.Push(big.NewInt(3))
	st.Push(big.NewInt(10))
	opMod(&pc, evm, contract, mem, st)

	if st.Peek().Int64() != 1 {
		t.Errorf("10 %% 3 = %d, want 1", st.Peek().Int64())
	}
}

func TestOpLt(t *testing.T) {
	evm, contract, mem, st := setupTest()
	pc := uint64(0)

	st.Push(big.NewInt(20))
	st.Push(big.NewInt(10))
	opLt(&pc, evm, contract, mem, st)

	if st.Peek().Int64() != 1 {
		t.Errorf("10 < 20 = %d, want 1", st.Peek().Int64())
	}
}

func TestOpGt(t *testing.T) {
	evm, contract, mem, st := setupTest()
	pc := uint64(0)

	st.Push(big.NewInt(5))
	st.Push(big.NewInt(10))
	opGt(&pc, evm, contract, mem, st)

	if st.Peek().Int64() != 1 {
		t.Errorf("10 > 5 = %d, want 1", st.Peek().Int64())
	}
}

func TestOpEq(t *testing.T) {
	evm, contract, mem, st := setupTest()
	pc := uint64(0)

	st.Push(big.NewInt(42))
	st.Push(big.NewInt(42))
	opEq(&pc, evm, contract, mem, st)

	if st.Peek().Int64() != 1 {
		t.Errorf("42 == 42 = %d, want 1", st.Peek().Int64())
	}
}

func TestOpIsZero(t *testing.T) {
	evm, contract, mem, st := setupTest()
	pc := uint64(0)

	st.Push(big.NewInt(0))
	opIsZero(&pc, evm, contract, mem, st)

	if st.Peek().Int64() != 1 {
		t.Errorf("ISZERO(0) = %d, want 1", st.Peek().Int64())
	}
}

func TestOpAnd(t *testing.T) {
	evm, contract, mem, st := setupTest()
	pc := uint64(0)

	st.Push(big.NewInt(0x0f))
	st.Push(big.NewInt(0xff))
	opAnd(&pc, evm, contract, mem, st)

	if st.Peek().Int64() != 0x0f {
		t.Errorf("0xff & 0x0f = 0x%x, want 0x0f", st.Peek().Int64())
	}
}

func TestOpOr(t *testing.T) {
	evm, contract, mem, st := setupTest()
	pc := uint64(0)

	st.Push(big.NewInt(0x0f))
	st.Push(big.NewInt(0xf0))
	opOr(&pc, evm, contract, mem, st)

	if st.Peek().Int64() != 0xff {
		t.Errorf("0xf0 | 0x0f = 0x%x, want 0xff", st.Peek().Int64())
	}
}

func TestOpXor(t *testing.T) {
	evm, contract, mem, st := setupTest()
	pc := uint64(0)

	st.Push(big.NewInt(0x0f))
	st.Push(big.NewInt(0xff))
	opXor(&pc, evm, contract, mem, st)

	if st.Peek().Int64() != 0xf0 {
		t.Errorf("0xff ^ 0x0f = 0x%x, want 0xf0", st.Peek().Int64())
	}
}

func TestOpNot(t *testing.T) {
	evm, contract, mem, st := setupTest()
	pc := uint64(0)

	st.Push(big.NewInt(0))
	opNot(&pc, evm, contract, mem, st)

	// NOT(0) should be max uint256
	max := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
	if st.Peek().Cmp(max) != 0 {
		t.Errorf("NOT(0) = %s, want max uint256", st.Peek().String())
	}
}

func TestOpPush1(t *testing.T) {
	evm, contract, mem, st := setupTest()
	contract.Code = []byte{byte(PUSH1), 0x42}
	pc := uint64(0)

	opPush1(&pc, evm, contract, mem, st)

	if st.Len() != 1 {
		t.Fatalf("stack len = %d, want 1", st.Len())
	}
	if st.Peek().Int64() != 0x42 {
		t.Errorf("PUSH1 0x42 = 0x%x, want 0x42", st.Peek().Int64())
	}
	if pc != 1 {
		t.Errorf("pc = %d, want 1", pc)
	}
}

func TestOpMstoreAndMload(t *testing.T) {
	evm, contract, mem, st := setupTest()
	mem.Resize(64)
	pc := uint64(0)

	// MSTORE: store 0xff at offset 0
	st.Push(big.NewInt(0xff))
	st.Push(big.NewInt(0))
	opMstore(&pc, evm, contract, mem, st)

	// MLOAD: load from offset 0
	st.Push(big.NewInt(0))
	opMload(&pc, evm, contract, mem, st)

	if st.Peek().Int64() != 0xff {
		t.Errorf("MLOAD after MSTORE = 0x%x, want 0xff", st.Peek().Int64())
	}
}

func TestOpMstore8(t *testing.T) {
	evm, contract, mem, st := setupTest()
	mem.Resize(32)
	pc := uint64(0)

	st.Push(big.NewInt(0xab))
	st.Push(big.NewInt(0))
	opMstore8(&pc, evm, contract, mem, st)

	if mem.Data()[0] != 0xab {
		t.Errorf("MSTORE8 byte = 0x%x, want 0xab", mem.Data()[0])
	}
}

func TestOpStop(t *testing.T) {
	evm, contract, mem, st := setupTest()
	pc := uint64(0)

	ret, err := opStop(&pc, evm, contract, mem, st)
	if err != nil {
		t.Errorf("opStop error: %v", err)
	}
	if ret != nil {
		t.Errorf("opStop return = %v, want nil", ret)
	}
}

func TestOpReturn(t *testing.T) {
	evm, contract, mem, st := setupTest()
	mem.Resize(64)
	pc := uint64(0)

	// Store data first
	mem.Set(0, 4, []byte{0xde, 0xad, 0xbe, 0xef})

	st.Push(big.NewInt(4)) // size
	st.Push(big.NewInt(0)) // offset
	ret, err := opReturn(&pc, evm, contract, mem, st)
	if err != nil {
		t.Errorf("opReturn error: %v", err)
	}
	if len(ret) != 4 || ret[0] != 0xde {
		t.Errorf("opReturn = %x, want deadbeef", ret)
	}
}

func TestOpRevert(t *testing.T) {
	evm, contract, mem, st := setupTest()
	mem.Resize(64)
	pc := uint64(0)

	mem.Set(0, 2, []byte{0xab, 0xcd})

	st.Push(big.NewInt(2))
	st.Push(big.NewInt(0))
	ret, err := opRevert(&pc, evm, contract, mem, st)
	if err != ErrExecutionReverted {
		t.Errorf("opRevert error = %v, want ErrExecutionReverted", err)
	}
	if len(ret) != 2 || ret[0] != 0xab {
		t.Errorf("opRevert = %x, want abcd", ret)
	}
}

func TestOpSdiv(t *testing.T) {
	evm, contract, mem, st := setupTest()
	pc := uint64(0)

	// 10 / 3 = 3 (positive case)
	st.Push(big.NewInt(3))
	st.Push(big.NewInt(10))
	opSdiv(&pc, evm, contract, mem, st)

	if st.Peek().Int64() != 3 {
		t.Errorf("SDIV(10, 3) = %d, want 3", st.Peek().Int64())
	}
}

func TestOpSmod(t *testing.T) {
	evm, contract, mem, st := setupTest()
	pc := uint64(0)

	st.Push(big.NewInt(3))
	st.Push(big.NewInt(10))
	opSmod(&pc, evm, contract, mem, st)

	if st.Peek().Int64() != 1 {
		t.Errorf("SMOD(10, 3) = %d, want 1", st.Peek().Int64())
	}
}
