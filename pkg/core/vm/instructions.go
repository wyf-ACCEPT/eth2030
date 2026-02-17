package vm

import (
	"math/big"
)

// executionFunc is the signature for opcode execution functions.
type executionFunc func(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error)

var (
	big0 = new(big.Int)
	tt256 = new(big.Int).Lsh(big.NewInt(1), 256)    // 2^256
	tt256m1 = new(big.Int).Sub(tt256, big.NewInt(1)) // 2^256 - 1
	tt255 = new(big.Int).Lsh(big.NewInt(1), 255)     // 2^255
)

// toU256 masks val to 256 bits (unsigned).
func toU256(val *big.Int) *big.Int {
	return val.And(val, tt256m1)
}

// toS256 interprets a 256-bit unsigned integer as a signed integer.
func toS256(val *big.Int) *big.Int {
	if val.Cmp(tt255) < 0 {
		return val
	}
	return new(big.Int).Sub(val, tt256)
}

// fromS256 converts a signed 256-bit integer to unsigned representation.
func fromS256(val *big.Int) *big.Int {
	if val.Sign() >= 0 {
		return val
	}
	return new(big.Int).Add(val, tt256)
}

func opAdd(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	x, y := stack.Pop(), stack.Peek()
	y.Add(x, y)
	toU256(y)
	return nil, nil
}

func opSub(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	x, y := stack.Pop(), stack.Peek()
	y.Sub(x, y)
	toU256(y)
	return nil, nil
}

func opMul(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	x, y := stack.Pop(), stack.Peek()
	y.Mul(x, y)
	toU256(y)
	return nil, nil
}

func opDiv(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	x, y := stack.Pop(), stack.Peek()
	if y.Sign() != 0 {
		y.Div(x, y)
	} else {
		y.SetUint64(0)
	}
	return nil, nil
}

func opSdiv(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	x, y := stack.Pop(), stack.Peek()
	sx := toS256(new(big.Int).Set(x))
	sy := toS256(new(big.Int).Set(y))
	if sy.Sign() == 0 {
		y.SetUint64(0)
	} else {
		result := new(big.Int).Div(new(big.Int).Abs(sx), new(big.Int).Abs(sy))
		if sx.Sign() != sy.Sign() {
			result.Neg(result)
		}
		y.Set(fromS256(result))
		toU256(y)
	}
	return nil, nil
}

func opMod(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	x, y := stack.Pop(), stack.Peek()
	if y.Sign() != 0 {
		y.Mod(x, y)
	} else {
		y.SetUint64(0)
	}
	return nil, nil
}

func opSmod(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	x, y := stack.Pop(), stack.Peek()
	sx := toS256(new(big.Int).Set(x))
	sy := toS256(new(big.Int).Set(y))
	if sy.Sign() == 0 {
		y.SetUint64(0)
	} else {
		result := new(big.Int).Mod(new(big.Int).Abs(sx), new(big.Int).Abs(sy))
		if sx.Sign() < 0 {
			result.Neg(result)
		}
		y.Set(fromS256(result))
		toU256(y)
	}
	return nil, nil
}

func opLt(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	x, y := stack.Pop(), stack.Peek()
	if x.Cmp(y) < 0 {
		y.SetUint64(1)
	} else {
		y.SetUint64(0)
	}
	return nil, nil
}

func opGt(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	x, y := stack.Pop(), stack.Peek()
	if x.Cmp(y) > 0 {
		y.SetUint64(1)
	} else {
		y.SetUint64(0)
	}
	return nil, nil
}

func opEq(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	x, y := stack.Pop(), stack.Peek()
	if x.Cmp(y) == 0 {
		y.SetUint64(1)
	} else {
		y.SetUint64(0)
	}
	return nil, nil
}

func opIsZero(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	x := stack.Peek()
	if x.Sign() == 0 {
		x.SetUint64(1)
	} else {
		x.SetUint64(0)
	}
	return nil, nil
}

func opAnd(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	x, y := stack.Pop(), stack.Peek()
	y.And(x, y)
	return nil, nil
}

func opOr(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	x, y := stack.Pop(), stack.Peek()
	y.Or(x, y)
	return nil, nil
}

func opXor(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	x, y := stack.Pop(), stack.Peek()
	y.Xor(x, y)
	return nil, nil
}

func opNot(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	x := stack.Peek()
	x.Not(x)
	toU256(x)
	return nil, nil
}

func opPush1(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	var b uint64
	if *pc+1 < uint64(len(contract.Code)) {
		b = uint64(contract.Code[*pc+1])
	}
	stack.Push(new(big.Int).SetUint64(b))
	*pc += 1
	return nil, nil
}

func opPop(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	stack.Pop()
	return nil, nil
}

func opMload(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	offset := stack.Peek()
	off := offset.Uint64()
	data := memory.Get(int64(off), 32)
	offset.SetBytes(data)
	return nil, nil
}

func opMstore(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	offset, val := stack.Pop(), stack.Pop()
	memory.Set32(offset.Uint64(), val)
	return nil, nil
}

func opMstore8(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	offset, val := stack.Pop(), stack.Pop()
	memory.Set(offset.Uint64(), 1, []byte{byte(val.Uint64())})
	return nil, nil
}

func opStop(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	return nil, nil
}

func opReturn(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	offset, size := stack.Pop(), stack.Pop()
	ret := memory.Get(int64(offset.Uint64()), int64(size.Uint64()))
	return ret, nil
}

func opRevert(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	offset, size := stack.Pop(), stack.Pop()
	ret := memory.Get(int64(offset.Uint64()), int64(size.Uint64()))
	return ret, ErrExecutionReverted
}
