package vm

import (
	"math/big"

	"github.com/eth2028/eth2028/crypto"
)

// executionFunc is the signature for opcode execution functions.
type executionFunc func(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error)

var (
	big0    = new(big.Int)
	tt256   = new(big.Int).Lsh(big.NewInt(1), 256)    // 2^256
	tt256m1 = new(big.Int).Sub(tt256, big.NewInt(1))   // 2^256 - 1
	tt255   = new(big.Int).Lsh(big.NewInt(1), 255)     // 2^255
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

func opAddmod(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	x, y, z := stack.Pop(), stack.Pop(), stack.Peek()
	if z.Sign() != 0 {
		add := new(big.Int).Add(x, y)
		z.Mod(add, z)
		toU256(z)
	} else {
		z.SetUint64(0)
	}
	return nil, nil
}

func opMulmod(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	x, y, z := stack.Pop(), stack.Pop(), stack.Peek()
	if z.Sign() != 0 {
		mul := new(big.Int).Mul(x, y)
		z.Mod(mul, z)
		toU256(z)
	} else {
		z.SetUint64(0)
	}
	return nil, nil
}

func opExp(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	base, exponent := stack.Pop(), stack.Peek()
	exponent.Exp(base, exponent, tt256)
	return nil, nil
}

func opSignExtend(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	back, num := stack.Pop(), stack.Peek()
	if back.Cmp(big.NewInt(31)) < 0 {
		bit := uint(back.Uint64()*8 + 7)
		mask := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), bit), big.NewInt(1))
		if num.Bit(int(bit)) > 0 {
			num.Or(num, new(big.Int).Not(mask))
		} else {
			num.And(num, mask)
		}
		toU256(num)
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

func opSlt(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	x, y := stack.Pop(), stack.Peek()
	sx := toS256(new(big.Int).Set(x))
	sy := toS256(new(big.Int).Set(y))
	if sx.Cmp(sy) < 0 {
		y.SetUint64(1)
	} else {
		y.SetUint64(0)
	}
	return nil, nil
}

func opSgt(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	x, y := stack.Pop(), stack.Peek()
	sx := toS256(new(big.Int).Set(x))
	sy := toS256(new(big.Int).Set(y))
	if sx.Cmp(sy) > 0 {
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

func opByte(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	th, val := stack.Pop(), stack.Peek()
	if th.Cmp(big.NewInt(32)) < 0 {
		b := val.Bytes()
		padded := make([]byte, 32)
		copy(padded[32-len(b):], b)
		val.SetUint64(uint64(padded[th.Uint64()]))
	} else {
		val.SetUint64(0)
	}
	return nil, nil
}

func opSHL(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	shift, value := stack.Pop(), stack.Peek()
	if shift.Cmp(big.NewInt(256)) >= 0 {
		value.SetUint64(0)
	} else {
		value.Lsh(value, uint(shift.Uint64()))
		toU256(value)
	}
	return nil, nil
}

func opSHR(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	shift, value := stack.Pop(), stack.Peek()
	if shift.Cmp(big.NewInt(256)) >= 0 {
		value.SetUint64(0)
	} else {
		value.Rsh(value, uint(shift.Uint64()))
	}
	return nil, nil
}

func opSAR(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	shift, value := stack.Pop(), stack.Peek()
	signed := toS256(new(big.Int).Set(value))
	if shift.Cmp(big.NewInt(256)) >= 0 {
		if signed.Sign() >= 0 {
			value.SetUint64(0)
		} else {
			value.Set(tt256m1) // all 1s
		}
	} else {
		signed.Rsh(signed, uint(shift.Uint64()))
		value.Set(fromS256(signed))
		toU256(value)
	}
	return nil, nil
}

func opCalldataLoad(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	x := stack.Peek()
	offset := x.Uint64()
	data := make([]byte, 32)
	if offset < uint64(len(contract.Input)) {
		copy(data, contract.Input[offset:])
	}
	x.SetBytes(data)
	return nil, nil
}

func opCalldataSize(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	stack.Push(new(big.Int).SetUint64(uint64(len(contract.Input))))
	return nil, nil
}

func opCalldataCopy(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	memOffset, dataOffset, length := stack.Pop(), stack.Pop(), stack.Pop()
	l := length.Uint64()
	if l == 0 {
		return nil, nil
	}
	dOff := dataOffset.Uint64()
	data := make([]byte, l)
	if dOff < uint64(len(contract.Input)) {
		copy(data, contract.Input[dOff:])
	}
	memory.Set(memOffset.Uint64(), l, data)
	return nil, nil
}

func opCodeSize(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	stack.Push(new(big.Int).SetUint64(uint64(len(contract.Code))))
	return nil, nil
}

func opCodeCopy(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	memOffset, codeOffset, length := stack.Pop(), stack.Pop(), stack.Pop()
	l := length.Uint64()
	if l == 0 {
		return nil, nil
	}
	cOff := codeOffset.Uint64()
	data := make([]byte, l)
	if cOff < uint64(len(contract.Code)) {
		copy(data, contract.Code[cOff:])
	}
	memory.Set(memOffset.Uint64(), l, data)
	return nil, nil
}

func opAddress(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	stack.Push(new(big.Int).SetBytes(contract.Address[:]))
	return nil, nil
}

func opOrigin(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	stack.Push(new(big.Int).SetBytes(evm.TxContext.Origin[:]))
	return nil, nil
}

func opCaller(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	stack.Push(new(big.Int).SetBytes(contract.CallerAddress[:]))
	return nil, nil
}

func opCallValue(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	v := new(big.Int)
	if contract.Value != nil {
		v.Set(contract.Value)
	}
	stack.Push(v)
	return nil, nil
}

func opGasPrice(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	v := new(big.Int)
	if evm.TxContext.GasPrice != nil {
		v.Set(evm.TxContext.GasPrice)
	}
	stack.Push(v)
	return nil, nil
}

func opCoinbase(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	stack.Push(new(big.Int).SetBytes(evm.Context.Coinbase[:]))
	return nil, nil
}

func opTimestamp(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	stack.Push(new(big.Int).SetUint64(evm.Context.Time))
	return nil, nil
}

func opNumber(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	v := new(big.Int)
	if evm.Context.BlockNumber != nil {
		v.Set(evm.Context.BlockNumber)
	}
	stack.Push(v)
	return nil, nil
}

func opPrevRandao(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	stack.Push(new(big.Int).SetBytes(evm.Context.PrevRandao[:]))
	return nil, nil
}

func opGasLimit(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	stack.Push(new(big.Int).SetUint64(evm.Context.GasLimit))
	return nil, nil
}

func opChainID(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	stack.Push(new(big.Int).SetUint64(evm.chainID))
	return nil, nil
}

func opBaseFee(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	v := new(big.Int)
	if evm.Context.BaseFee != nil {
		v.Set(evm.Context.BaseFee)
	}
	stack.Push(v)
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

func opJump(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	pos := stack.Pop()
	if !contract.validJumpdest(pos) {
		return nil, ErrInvalidJump
	}
	*pc = pos.Uint64()
	return nil, nil
}

func opJumpi(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	pos, cond := stack.Pop(), stack.Pop()
	if cond.Sign() != 0 {
		if !contract.validJumpdest(pos) {
			return nil, ErrInvalidJump
		}
		*pc = pos.Uint64()
	} else {
		*pc++
	}
	return nil, nil
}

func opJumpdest(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	return nil, nil
}

func opPc(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	stack.Push(new(big.Int).SetUint64(*pc))
	return nil, nil
}

func opMsize(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	stack.Push(new(big.Int).SetUint64(uint64(memory.Len())))
	return nil, nil
}

func opGas(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	stack.Push(new(big.Int).SetUint64(contract.Gas))
	return nil, nil
}

func opPush0(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	stack.Push(new(big.Int))
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

// makePush returns an executionFunc that pushes n bytes from code.
func makePush(size uint64) executionFunc {
	return func(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
		start := *pc + 1
		end := start + size
		codeLen := uint64(len(contract.Code))

		var data []byte
		if start >= codeLen {
			data = make([]byte, size)
		} else if end > codeLen {
			data = make([]byte, size)
			copy(data, contract.Code[start:codeLen])
		} else {
			data = contract.Code[start:end]
		}

		stack.Push(new(big.Int).SetBytes(data))
		*pc += size
		return nil, nil
	}
}

// makeDup returns an executionFunc that duplicates the nth stack item.
func makeDup(n int) executionFunc {
	return func(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
		stack.Dup(n)
		return nil, nil
	}
}

// makeSwap returns an executionFunc that swaps the top with the nth item.
func makeSwap(n int) executionFunc {
	return func(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
		stack.Swap(n)
		return nil, nil
	}
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

func opInvalid(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	return nil, ErrInvalidOpCode
}

func opKeccak256(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	offset, size := stack.Pop(), stack.Peek()
	data := memory.Get(int64(offset.Uint64()), int64(size.Uint64()))
	hash := crypto.Keccak256(data)
	size.SetBytes(hash)
	return nil, nil
}

func opSload(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	// TODO: implement state access
	loc := stack.Peek()
	_ = loc
	loc.SetUint64(0)
	return nil, nil
}

func opSstore(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	// TODO: implement state access
	_ = stack.Pop() // location
	_ = stack.Pop() // value
	return nil, nil
}

func opReturndataSize(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	// TODO: track return data from sub-calls
	stack.Push(new(big.Int))
	return nil, nil
}

func opReturndataCopy(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	// TODO: track return data from sub-calls
	_ = stack.Pop() // memOffset
	_ = stack.Pop() // dataOffset
	_ = stack.Pop() // length
	return nil, nil
}

func opSelfBalance(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	// TODO: implement state access for balance
	stack.Push(new(big.Int))
	return nil, nil
}

func opBalance(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	// TODO: implement state access for balance
	addr := stack.Peek()
	_ = addr
	addr.SetUint64(0)
	return nil, nil
}

// makeLog returns an executionFunc for LOG0..LOG4.
func makeLog(n int) executionFunc {
	return func(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
		// TODO: implement log emission
		_ = stack.Pop() // offset
		_ = stack.Pop() // size
		for i := 0; i < n; i++ {
			_ = stack.Pop() // topic
		}
		return nil, nil
	}
}
