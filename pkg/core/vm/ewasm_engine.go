// Package vm implements the Ethereum Virtual Machine.
//
// ewasm_engine.go provides an eWASM interpreter for the EL roadmap:
// "more precompiles in eWASM", "STF in eRISC", "canonical guest".
// Thread-safe via per-execution isolation.
package vm

import (
	"encoding/binary"
	"errors"
	"sync"
)

// Engine WASM opcodes (engine* prefix avoids collisions).
const (
	engineOpUnreachable byte = 0x00
	engineOpNop         byte = 0x01
	engineOpBlock       byte = 0x02
	engineOpLoop        byte = 0x03
	engineOpEnd         byte = 0x0B
	engineOpBr          byte = 0x0C
	engineOpBrIf        byte = 0x0D
	engineOpReturn      byte = 0x0F
	engineOpCall        byte = 0x10
	engineOpDrop        byte = 0x1A
	engineOpSelect      byte = 0x1B
	engineOpLocalGet    byte = 0x20
	engineOpLocalSet    byte = 0x21
	engineOpI32Load     byte = 0x28
	engineOpI32Store    byte = 0x36
	engineOpI32Const    byte = 0x41
	engineOpI32Eqz      byte = 0x45
	engineOpI32Eq       byte = 0x46
	engineOpI32LtU      byte = 0x49
	engineOpI32GtU      byte = 0x4B
	engineOpI32LeU      byte = 0x4D
	engineOpI32GeU      byte = 0x4F
	engineOpI32Add      byte = 0x6A
	engineOpI32Sub      byte = 0x6B
	engineOpI32Mul      byte = 0x6C
	engineOpI32DivU     byte = 0x6D
	engineOpI32RemU     byte = 0x6F
	engineOpI32And      byte = 0x71
	engineOpI32Or       byte = 0x72
	engineOpI32Xor      byte = 0x73
	engineOpI32Shl      byte = 0x74
	engineOpI32ShrU     byte = 0x76
)

var (
	errEngineOutOfGas       = errors.New("ewasm-engine: out of gas")
	errEngineStackUnderflow = errors.New("ewasm-engine: stack underflow")
	errEngineInvalidOpcode  = errors.New("ewasm-engine: invalid opcode")
	errEngineDivisionByZero = errors.New("ewasm-engine: division by zero")
	errEngineMemoryOOB      = errors.New("ewasm-engine: memory access out of bounds")
	errEngineInvalidModule  = errors.New("ewasm-engine: invalid module")
	errEngineNoCodeSection  = errors.New("ewasm-engine: no code section")
	errEngineNoFunction     = errors.New("ewasm-engine: function not found")
	errEngineCallDepth      = errors.New("ewasm-engine: call depth exceeded")
	errEngineUnreachable    = errors.New("ewasm-engine: unreachable")
	errEngineInvalidLocal   = errors.New("ewasm-engine: invalid local index")
)

const (
	engineMaxCallDepth = 64
	enginePageSize     = 65536
	engineGasPerOp     = uint64(1)
	engineGasPerCall   = uint64(10)
)

type EWASMEngine struct{ mu sync.Mutex }

func NewEWASMEngine() *EWASMEngine { return &EWASMEngine{} }

type engineFrame struct {
	pc, blockDepth int
	body           []byte
	locals         []uint32
}

type engineState struct {
	stack      []uint32
	memory     []byte
	gas        uint64
	frames     []*engineFrame
	funcBodies [][]byte
	returned   bool
}

func (s *engineState) push(v uint32) { s.stack = append(s.stack, v) }
func (s *engineState) pop() (uint32, error) {
	if len(s.stack) == 0 {
		return 0, errEngineStackUnderflow
	}
	v := s.stack[len(s.stack)-1]
	s.stack = s.stack[:len(s.stack)-1]
	return v, nil
}

func (s *engineState) binop(fn func(a, b uint32) uint32) error {
	b, e := s.pop()
	if e != nil {
		return e
	}
	a, e := s.pop()
	if e != nil {
		return e
	}
	s.push(fn(a, b))
	return nil
}

func (e *EWASMEngine) Execute(bytecode, input []byte, gas uint64) ([]byte, uint64, error) {
	if err := ValidateWasmBytecode(bytecode); err != nil {
		return nil, gas, errEngineInvalidModule
	}
	secs, err := parseSections(bytecode[8:])
	if err != nil {
		return nil, gas, errEngineInvalidModule
	}
	st := &engineState{stack: make([]uint32, 0, 64), gas: gas, memory: make([]byte, enginePageSize)}
	if n := len(input); n > 0 {
		if n > enginePageSize {
			n = enginePageSize
		}
		copy(st.memory, input[:n])
	}
	if err := st.parseFuncs(secs); err != nil {
		return nil, gas, err
	}
	if len(st.funcBodies) == 0 {
		return nil, gas, errEngineNoCodeSection
	}
	entry := 0
	for _, s := range secs {
		if s.ID == WasmSectionExport {
			if idx := engineFindExportFunc(s.Data); idx >= 0 && idx < len(st.funcBodies) {
				entry = idx
			}
			break
		}
	}
	if err := st.callFunc(entry); err != nil {
		return nil, st.gas, err
	}
	if len(st.stack) > 0 {
		out := make([]byte, 4)
		binary.LittleEndian.PutUint32(out, st.stack[len(st.stack)-1])
		return out, st.gas, nil
	}
	out := make([]byte, 32)
	copy(out, st.memory[:32])
	return out, st.gas, nil
}

func (s *engineState) parseFuncs(secs []WasmSection) error {
	for _, sec := range secs {
		if sec.ID == WasmSectionCode {
			return s.parseCode(sec.Data)
		}
	}
	return errEngineNoCodeSection
}

func (s *engineState) parseCode(data []byte) error {
	if len(data) == 0 {
		return errEngineNoCodeSection
	}
	cnt, n, err := decodeLEB128(data)
	if err != nil {
		return errEngineInvalidModule
	}
	off := n
	for i := uint32(0); i < cnt; i++ {
		if off >= len(data) {
			return errEngineInvalidModule
		}
		sz, n2, e := decodeLEB128(data[off:])
		if e != nil {
			return errEngineInvalidModule
		}
		off += n2
		if off+int(sz) > len(data) {
			return errEngineInvalidModule
		}
		b := make([]byte, sz)
		copy(b, data[off:off+int(sz)])
		s.funcBodies = append(s.funcBodies, b)
		off += int(sz)
	}
	return nil
}

func (s *engineState) callFunc(idx int) error {
	if idx < 0 || idx >= len(s.funcBodies) {
		return errEngineNoFunction
	}
	if len(s.frames) >= engineMaxCallDepth {
		return errEngineCallDepth
	}
	body := s.funcBodies[idx]
	lc, pc := 0, 0
	if len(body) > 0 {
		nd, n, e := decodeLEB128(body)
		if e != nil {
			return errEngineInvalidModule
		}
		pc = n
		for i := uint32(0); i < nd && pc < len(body); i++ {
			c, n2, e2 := decodeLEB128(body[pc:])
			if e2 != nil {
				break
			}
			pc += n2
			if pc >= len(body) {
				break
			}
			pc++
			lc += int(c)
		}
	}
	s.frames = append(s.frames, &engineFrame{pc: pc, body: body, locals: make([]uint32, lc)})
	if s.gas < engineGasPerCall {
		return errEngineOutOfGas
	}
	s.gas -= engineGasPerCall
	err := s.exec()
	s.frames = s.frames[:len(s.frames)-1]
	return err
}

func (s *engineState) exec() error {
	f := s.frames[len(s.frames)-1]
	for f.pc < len(f.body) && !s.returned {
		if s.gas < engineGasPerOp {
			return errEngineOutOfGas
		}
		s.gas -= engineGasPerOp
		op := f.body[f.pc]
		f.pc++
		if err := s.dispatch(op, f); err != nil {
			return err
		}
	}
	return nil
}

func (s *engineState) dispatch(op byte, f *engineFrame) error {
	switch op {
	case engineOpUnreachable:
		return errEngineUnreachable
	case engineOpNop:
	case engineOpBlock, engineOpLoop:
		if f.pc < len(f.body) {
			f.pc++
		}
		f.blockDepth++
	case engineOpEnd:
		if f.blockDepth > 0 {
			f.blockDepth--
		} else {
			s.returned = true
		}
	case engineOpBr:
		d, n, e := decodeLEB128(f.body[f.pc:])
		if e != nil {
			return errEngineInvalidOpcode
		}
		f.pc += n
		s.branch(f, int(d))
	case engineOpBrIf:
		d, n, e := decodeLEB128(f.body[f.pc:])
		if e != nil {
			return errEngineInvalidOpcode
		}
		f.pc += n
		c, e2 := s.pop()
		if e2 != nil {
			return e2
		}
		if c != 0 {
			s.branch(f, int(d))
		}
	case engineOpReturn:
		s.returned = true
	case engineOpCall:
		idx, n, e := decodeLEB128(f.body[f.pc:])
		if e != nil {
			return errEngineInvalidOpcode
		}
		f.pc += n
		return s.callFunc(int(idx))
	case engineOpDrop:
		_, e := s.pop()
		return e
	case engineOpSelect:
		return s.doSelect()
	case engineOpLocalGet:
		return s.doLocalGet(f)
	case engineOpLocalSet:
		return s.doLocalSet(f)
	case engineOpI32Load:
		return s.doLoad(f)
	case engineOpI32Store:
		return s.doStore(f)
	case engineOpI32Const:
		v, n, e := engineDecodeSLEB128(f.body[f.pc:])
		if e != nil {
			return errEngineInvalidOpcode
		}
		f.pc += n
		s.push(uint32(v))
	case engineOpI32Eqz:
		v, e := s.pop()
		if e != nil {
			return e
		}
		s.push(b32(v == 0))
	case engineOpI32Eq:
		return s.binop(func(a, b uint32) uint32 { return b32(a == b) })
	case engineOpI32LtU:
		return s.binop(func(a, b uint32) uint32 { return b32(a < b) })
	case engineOpI32GtU:
		return s.binop(func(a, b uint32) uint32 { return b32(a > b) })
	case engineOpI32LeU:
		return s.binop(func(a, b uint32) uint32 { return b32(a <= b) })
	case engineOpI32GeU:
		return s.binop(func(a, b uint32) uint32 { return b32(a >= b) })
	case engineOpI32Add:
		return s.binop(func(a, b uint32) uint32 { return a + b })
	case engineOpI32Sub:
		return s.binop(func(a, b uint32) uint32 { return a - b })
	case engineOpI32Mul:
		return s.binop(func(a, b uint32) uint32 { return a * b })
	case engineOpI32DivU, engineOpI32RemU:
		return s.doDivRem(op == engineOpI32RemU)
	case engineOpI32And:
		return s.binop(func(a, b uint32) uint32 { return a & b })
	case engineOpI32Or:
		return s.binop(func(a, b uint32) uint32 { return a | b })
	case engineOpI32Xor:
		return s.binop(func(a, b uint32) uint32 { return a ^ b })
	case engineOpI32Shl:
		return s.binop(func(a, b uint32) uint32 { return a << (b & 31) })
	case engineOpI32ShrU:
		return s.binop(func(a, b uint32) uint32 { return a >> (b & 31) })
	default:
		return errEngineInvalidOpcode
	}
	return nil
}

func (s *engineState) doSelect() error {
	c, e := s.pop()
	if e != nil {
		return e
	}
	v2, e := s.pop()
	if e != nil {
		return e
	}
	v1, e := s.pop()
	if e != nil {
		return e
	}
	if c != 0 {
		s.push(v1)
	} else {
		s.push(v2)
	}
	return nil
}

func (s *engineState) doLocalGet(f *engineFrame) error {
	idx, n, e := decodeLEB128(f.body[f.pc:])
	if e != nil {
		return errEngineInvalidOpcode
	}
	f.pc += n
	if int(idx) >= len(f.locals) {
		return errEngineInvalidLocal
	}
	s.push(f.locals[idx])
	return nil
}

func (s *engineState) doLocalSet(f *engineFrame) error {
	idx, n, e := decodeLEB128(f.body[f.pc:])
	if e != nil {
		return errEngineInvalidOpcode
	}
	f.pc += n
	v, e := s.pop()
	if e != nil {
		return e
	}
	if int(idx) >= len(f.locals) {
		return errEngineInvalidLocal
	}
	f.locals[idx] = v
	return nil
}

func (s *engineState) readMemImm(f *engineFrame) (uint32, error) {
	_, n1, e := decodeLEB128(f.body[f.pc:])
	if e != nil {
		return 0, errEngineInvalidOpcode
	}
	f.pc += n1
	off, n2, e := decodeLEB128(f.body[f.pc:])
	if e != nil {
		return 0, errEngineInvalidOpcode
	}
	f.pc += n2
	return off, nil
}

func (s *engineState) doLoad(f *engineFrame) error {
	off, e := s.readMemImm(f)
	if e != nil {
		return e
	}
	addr, e := s.pop()
	if e != nil {
		return e
	}
	ea := int(addr) + int(off)
	if ea < 0 || ea+4 > len(s.memory) {
		return errEngineMemoryOOB
	}
	s.push(binary.LittleEndian.Uint32(s.memory[ea : ea+4]))
	return nil
}

func (s *engineState) doStore(f *engineFrame) error {
	off, e := s.readMemImm(f)
	if e != nil {
		return e
	}
	val, e := s.pop()
	if e != nil {
		return e
	}
	addr, e := s.pop()
	if e != nil {
		return e
	}
	ea := int(addr) + int(off)
	if ea < 0 || ea+4 > len(s.memory) {
		return errEngineMemoryOOB
	}
	binary.LittleEndian.PutUint32(s.memory[ea:ea+4], val)
	return nil
}

func (s *engineState) doDivRem(rem bool) error {
	b, e := s.pop()
	if e != nil {
		return e
	}
	a, e := s.pop()
	if e != nil {
		return e
	}
	if b == 0 {
		return errEngineDivisionByZero
	}
	if rem {
		s.push(a % b)
	} else {
		s.push(a / b)
	}
	return nil
}

func b32(v bool) uint32 {
	if v {
		return 1
	}
	return 0
}

func (s *engineState) branch(f *engineFrame, depth int) {
	nest := 0
	for f.pc < len(f.body) {
		op := f.body[f.pc]
		f.pc++
		switch op {
		case engineOpBlock, engineOpLoop:
			if f.pc < len(f.body) {
				f.pc++
			}
			nest++
		case engineOpEnd:
			if nest == depth {
				if f.blockDepth > 0 {
					f.blockDepth--
				}
				return
			}
			if nest > 0 {
				nest--
			}
		case engineOpI32Const:
			s.skipLEB(f)
		case engineOpLocalGet, engineOpLocalSet, engineOpBr, engineOpBrIf, engineOpCall:
			s.skipLEB(f)
		case engineOpI32Load, engineOpI32Store:
			s.skipLEB(f)
			s.skipLEB(f)
		}
	}
}

func (s *engineState) skipLEB(f *engineFrame) {
	for f.pc < len(f.body) {
		b := f.body[f.pc]
		f.pc++
		if b&0x80 == 0 {
			return
		}
	}
}

// engineDecodeSLEB128 decodes a signed LEB128 (i32) integer.
func engineDecodeSLEB128(data []byte) (int32, int, error) {
	var r int32
	var sh uint
	for i := 0; i < len(data) && i < 5; i++ {
		b := data[i]
		r |= int32(b&0x7F) << sh
		sh += 7
		if b&0x80 == 0 {
			if sh < 32 && b&0x40 != 0 {
				r |= -(1 << sh)
			}
			return r, i + 1, nil
		}
	}
	return 0, 0, errors.New("ewasm-engine: invalid signed LEB128")
}

func engineFindExportFunc(data []byte) int {
	if len(data) == 0 {
		return -1
	}
	cnt, n, e := decodeLEB128(data)
	if e != nil {
		return -1
	}
	off := n
	for i := uint32(0); i < cnt && off < len(data); i++ {
		nl, n2, e2 := decodeLEB128(data[off:])
		if e2 != nil {
			return -1
		}
		off += n2 + int(nl)
		if off >= len(data) {
			return -1
		}
		kind := data[off]
		off++
		idx, n3, e3 := decodeLEB128(data[off:])
		if e3 != nil {
			return -1
		}
		off += n3
		if kind == WasmExportFunc {
			return int(idx)
		}
	}
	return -1
}

// BuildEngineWasm builds a valid WASM with a single function for testing.
func BuildEngineWasm(code []byte, numLocals int, export string) []byte {
	buf := []byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00}
	buf = appendSection(buf, WasmSectionType, []byte{0x01, 0x60, 0x00, 0x01, 0x7F})
	buf = appendSection(buf, WasmSectionFunction, []byte{0x01, 0x00})
	if export != "" {
		ed := []byte{0x01, byte(len(export))}
		ed = append(ed, export...)
		ed = append(ed, WasmExportFunc, 0x00)
		buf = appendSection(buf, WasmSectionExport, ed)
	}
	var body []byte
	if numLocals > 0 {
		body = append(body, 0x01)
		body = appendLEB128(body, uint32(numLocals))
		body = append(body, 0x7F)
	} else {
		body = append(body, 0x00)
	}
	body = append(body, code...)
	body = append(body, engineOpEnd)
	cd := []byte{0x01}
	cd = appendLEB128(cd, uint32(len(body)))
	cd = append(cd, body...)
	buf = appendSection(buf, WasmSectionCode, cd)
	return buf
}
