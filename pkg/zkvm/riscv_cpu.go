// riscv_cpu.go implements a RISC-V RV32IM CPU emulator for the canonical
// zkVM guest execution engine. Supports the full RV32I base integer ISA
// plus the M extension for integer multiplication and division.
//
// The emulator uses sparse page-based memory and collects execution traces
// for proof generation via the witness collector.
//
// Part of the K+ roadmap for canonical RISC-V guest execution.
package zkvm

import (
	"errors"
	"fmt"
)

// RISC-V CPU errors.
var (
	ErrRVInvalidInstruction = errors.New("riscv: invalid instruction")
	ErrRVGasExhausted       = errors.New("riscv: gas exhausted")
	ErrRVHalted             = errors.New("riscv: cpu halted")
	ErrRVMemoryFault        = errors.New("riscv: memory access fault")
	ErrRVEmptyProgram       = errors.New("riscv: empty program")
)

// ECALL function codes (placed in a7/x17 register).
const (
	RVEcallHalt   uint32 = 0 // Halt execution. Exit code in a0.
	RVEcallOutput uint32 = 1 // Output byte from a0.
	RVEcallInput  uint32 = 2 // Read input byte into a0.
)

// RVRegCount is the number of general-purpose registers.
const RVRegCount = 32

// RVCPU represents a RISC-V RV32IM processor.
type RVCPU struct {
	Regs    [RVRegCount]uint32
	PC      uint32
	Memory  *RVMemory
	Halted  bool
	ExitCode uint32

	// Gas metering: 1 gas per instruction.
	GasLimit uint64
	GasUsed  uint64

	// Step counter for cycle tracking.
	Steps uint64

	// I/O buffers for ECALL.
	InputBuf  []byte
	OutputBuf []byte
	inputPos  int

	// Witness collector (optional).
	Witness *RVWitnessCollector
}

// NewRVCPU creates a new RISC-V CPU with the given gas limit.
func NewRVCPU(gasLimit uint64) *RVCPU {
	return &RVCPU{
		Memory:   NewRVMemory(),
		GasLimit: gasLimit,
	}
}

// LoadProgram loads machine code into memory at the given base address
// and sets PC to entryPoint.
func (cpu *RVCPU) LoadProgram(code []byte, base, entryPoint uint32) error {
	if len(code) == 0 {
		return ErrRVEmptyProgram
	}
	if err := cpu.Memory.LoadSegment(base, code); err != nil {
		return err
	}
	cpu.PC = entryPoint
	return nil
}

// Run executes instructions until halt or gas exhaustion.
func (cpu *RVCPU) Run() error {
	for !cpu.Halted {
		if err := cpu.Step(); err != nil {
			return err
		}
	}
	return nil
}

// Step executes a single instruction.
func (cpu *RVCPU) Step() error {
	if cpu.Halted {
		return ErrRVHalted
	}
	if cpu.GasLimit > 0 && cpu.GasUsed >= cpu.GasLimit {
		return ErrRVGasExhausted
	}

	// Record pre-step state for witness.
	var regsBefore [RVRegCount]uint32
	if cpu.Witness != nil {
		copy(regsBefore[:], cpu.Regs[:])
	}
	pcBefore := cpu.PC

	// Fetch instruction.
	instr, err := cpu.Memory.ReadWord(cpu.PC)
	if err != nil {
		return fmt.Errorf("%w: fetch at 0x%08x: %v", ErrRVMemoryFault, cpu.PC, err)
	}

	// Decode and execute.
	memOps, err := cpu.execute(instr)
	if err != nil {
		return err
	}

	// x0 is always zero.
	cpu.Regs[0] = 0

	cpu.GasUsed++
	cpu.Steps++

	// Record witness step.
	if cpu.Witness != nil {
		cpu.Witness.RecordStep(pcBefore, instr, regsBefore, cpu.Regs, memOps)
	}

	return nil
}

// MemOp records a single memory read or write for witness collection.
type MemOp struct {
	Addr    uint32
	Value   uint32
	IsWrite bool
}

// execute decodes and executes a single 32-bit instruction.
func (cpu *RVCPU) execute(instr uint32) ([]MemOp, error) {
	opcode := instr & 0x7F
	var memOps []MemOp
	var err error

	switch opcode {
	case 0x37: // LUI
		rd, imm := decodeU(instr)
		cpu.Regs[rd] = imm
		cpu.PC += 4

	case 0x17: // AUIPC
		rd, imm := decodeU(instr)
		cpu.Regs[rd] = cpu.PC + imm
		cpu.PC += 4

	case 0x6F: // JAL
		rd, imm := decodeJ(instr)
		cpu.Regs[rd] = cpu.PC + 4
		cpu.PC = uint32(int32(cpu.PC) + imm)

	case 0x67: // JALR
		rd, rs1, imm := decodeI(instr)
		target := uint32(int32(cpu.Regs[rs1])+imm) & ^uint32(1)
		cpu.Regs[rd] = cpu.PC + 4
		cpu.PC = target

	case 0x63: // Branch
		rs1, rs2, imm := decodeB(instr)
		funct3 := (instr >> 12) & 0x7
		taken := false
		a, b := cpu.Regs[rs1], cpu.Regs[rs2]
		switch funct3 {
		case 0: // BEQ
			taken = a == b
		case 1: // BNE
			taken = a != b
		case 4: // BLT
			taken = int32(a) < int32(b)
		case 5: // BGE
			taken = int32(a) >= int32(b)
		case 6: // BLTU
			taken = a < b
		case 7: // BGEU
			taken = a >= b
		default:
			return nil, fmt.Errorf("%w: branch funct3=0x%x", ErrRVInvalidInstruction, funct3)
		}
		if taken {
			cpu.PC = uint32(int32(cpu.PC) + imm)
		} else {
			cpu.PC += 4
		}

	case 0x03: // Load
		rd, rs1, imm := decodeI(instr)
		funct3 := (instr >> 12) & 0x7
		addr := uint32(int32(cpu.Regs[rs1]) + imm)
		var val uint32
		switch funct3 {
		case 0: // LB
			b, err := cpu.Memory.ReadByte(addr)
			if err != nil {
				return nil, fmt.Errorf("%w: load byte at 0x%08x", ErrRVMemoryFault, addr)
			}
			val = uint32(int32(int8(b)))
		case 1: // LH
			h, err := cpu.Memory.ReadHalfword(addr)
			if err != nil {
				return nil, fmt.Errorf("%w: load halfword at 0x%08x", ErrRVMemoryFault, addr)
			}
			val = uint32(int32(int16(h)))
		case 2: // LW
			w, err := cpu.Memory.ReadWord(addr)
			if err != nil {
				return nil, fmt.Errorf("%w: load word at 0x%08x", ErrRVMemoryFault, addr)
			}
			val = w
		case 4: // LBU
			b, err := cpu.Memory.ReadByte(addr)
			if err != nil {
				return nil, fmt.Errorf("%w: load byte unsigned at 0x%08x", ErrRVMemoryFault, addr)
			}
			val = uint32(b)
		case 5: // LHU
			h, err := cpu.Memory.ReadHalfword(addr)
			if err != nil {
				return nil, fmt.Errorf("%w: load halfword unsigned at 0x%08x", ErrRVMemoryFault, addr)
			}
			val = uint32(h)
		default:
			return nil, fmt.Errorf("%w: load funct3=0x%x", ErrRVInvalidInstruction, funct3)
		}
		cpu.Regs[rd] = val
		memOps = append(memOps, MemOp{Addr: addr, Value: val, IsWrite: false})
		cpu.PC += 4

	case 0x23: // Store
		rs1, rs2, imm := decodeS(instr)
		funct3 := (instr >> 12) & 0x7
		addr := uint32(int32(cpu.Regs[rs1]) + imm)
		val := cpu.Regs[rs2]
		switch funct3 {
		case 0: // SB
			if err := cpu.Memory.WriteByte(addr, byte(val)); err != nil {
				return nil, fmt.Errorf("%w: store byte at 0x%08x", ErrRVMemoryFault, addr)
			}
		case 1: // SH
			if err := cpu.Memory.WriteHalfword(addr, uint16(val)); err != nil {
				return nil, fmt.Errorf("%w: store halfword at 0x%08x", ErrRVMemoryFault, addr)
			}
		case 2: // SW
			if err := cpu.Memory.WriteWord(addr, val); err != nil {
				return nil, fmt.Errorf("%w: store word at 0x%08x", ErrRVMemoryFault, addr)
			}
		default:
			return nil, fmt.Errorf("%w: store funct3=0x%x", ErrRVInvalidInstruction, funct3)
		}
		memOps = append(memOps, MemOp{Addr: addr, Value: val, IsWrite: true})
		cpu.PC += 4

	case 0x13: // Immediate arithmetic (ADDI, SLTI, etc.)
		memOps, err = cpu.executeImmediate(instr)
		if err != nil {
			return nil, err
		}

	case 0x33: // Register arithmetic (ADD, SUB, MUL, etc.)
		memOps, err = cpu.executeRegister(instr)
		if err != nil {
			return nil, err
		}

	case 0x73: // SYSTEM (ECALL/EBREAK)
		funct3 := (instr >> 12) & 0x7
		if funct3 != 0 {
			return nil, fmt.Errorf("%w: system funct3=0x%x", ErrRVInvalidInstruction, funct3)
		}
		immBits := instr >> 20
		if immBits == 0 { // ECALL
			cpu.handleEcall()
		}
		// EBREAK (immBits==1): treat as halt.
		if immBits == 1 {
			cpu.Halted = true
		}
		cpu.PC += 4

	default:
		return nil, fmt.Errorf("%w: opcode=0x%02x at PC=0x%08x", ErrRVInvalidInstruction, opcode, cpu.PC)
	}

	return memOps, nil
}

// executeImmediate handles I-type arithmetic instructions.
func (cpu *RVCPU) executeImmediate(instr uint32) ([]MemOp, error) {
	rd, rs1, imm := decodeI(instr)
	funct3 := (instr >> 12) & 0x7
	src := cpu.Regs[rs1]
	immU := uint32(imm)

	switch funct3 {
	case 0: // ADDI
		cpu.Regs[rd] = uint32(int32(src) + imm)
	case 2: // SLTI
		if int32(src) < imm {
			cpu.Regs[rd] = 1
		} else {
			cpu.Regs[rd] = 0
		}
	case 3: // SLTIU
		if src < immU {
			cpu.Regs[rd] = 1
		} else {
			cpu.Regs[rd] = 0
		}
	case 4: // XORI
		cpu.Regs[rd] = src ^ immU
	case 6: // ORI
		cpu.Regs[rd] = src | immU
	case 7: // ANDI
		cpu.Regs[rd] = src & immU
	case 1: // SLLI
		shamt := immU & 0x1F
		cpu.Regs[rd] = src << shamt
	case 5: // SRLI / SRAI
		shamt := immU & 0x1F
		if (instr>>30)&1 == 1 { // SRAI
			cpu.Regs[rd] = uint32(int32(src) >> shamt)
		} else { // SRLI
			cpu.Regs[rd] = src >> shamt
		}
	default:
		return nil, fmt.Errorf("%w: imm arith funct3=0x%x", ErrRVInvalidInstruction, funct3)
	}
	cpu.PC += 4
	return nil, nil
}

// executeRegister handles R-type instructions including M extension.
func (cpu *RVCPU) executeRegister(instr uint32) ([]MemOp, error) {
	rd := (instr >> 7) & 0x1F
	rs1 := (instr >> 15) & 0x1F
	rs2 := (instr >> 20) & 0x1F
	funct3 := (instr >> 12) & 0x7
	funct7 := (instr >> 25) & 0x7F
	a, b := cpu.Regs[rs1], cpu.Regs[rs2]

	if funct7 == 0x01 { // M extension
		return cpu.executeMExt(rd, a, b, funct3)
	}

	switch funct3 {
	case 0: // ADD / SUB
		if funct7 == 0x20 {
			cpu.Regs[rd] = uint32(int32(a) - int32(b))
		} else {
			cpu.Regs[rd] = a + b
		}
	case 1: // SLL
		cpu.Regs[rd] = a << (b & 0x1F)
	case 2: // SLT
		if int32(a) < int32(b) {
			cpu.Regs[rd] = 1
		} else {
			cpu.Regs[rd] = 0
		}
	case 3: // SLTU
		if a < b {
			cpu.Regs[rd] = 1
		} else {
			cpu.Regs[rd] = 0
		}
	case 4: // XOR
		cpu.Regs[rd] = a ^ b
	case 5: // SRL / SRA
		if funct7 == 0x20 {
			cpu.Regs[rd] = uint32(int32(a) >> (b & 0x1F))
		} else {
			cpu.Regs[rd] = a >> (b & 0x1F)
		}
	case 6: // OR
		cpu.Regs[rd] = a | b
	case 7: // AND
		cpu.Regs[rd] = a & b
	default:
		return nil, fmt.Errorf("%w: reg arith funct3=0x%x", ErrRVInvalidInstruction, funct3)
	}
	cpu.PC += 4
	return nil, nil
}

// executeMExt handles the M extension (MUL/DIV/REM).
func (cpu *RVCPU) executeMExt(rd, a, b uint32, funct3 uint32) ([]MemOp, error) {
	switch funct3 {
	case 0: // MUL
		cpu.Regs[rd] = uint32(int32(a) * int32(b))
	case 1: // MULH (signed x signed, high 32 bits)
		result := int64(int32(a)) * int64(int32(b))
		cpu.Regs[rd] = uint32(result >> 32)
	case 2: // MULHSU (signed x unsigned, high 32 bits)
		result := int64(int32(a)) * int64(b)
		cpu.Regs[rd] = uint32(result >> 32)
	case 3: // MULHU (unsigned x unsigned, high 32 bits)
		result := uint64(a) * uint64(b)
		cpu.Regs[rd] = uint32(result >> 32)
	case 4: // DIV (signed)
		if b == 0 {
			cpu.Regs[rd] = 0xFFFFFFFF // -1
		} else if int32(a) == -0x80000000 && int32(b) == -1 {
			cpu.Regs[rd] = a // overflow case
		} else {
			cpu.Regs[rd] = uint32(int32(a) / int32(b))
		}
	case 5: // DIVU (unsigned)
		if b == 0 {
			cpu.Regs[rd] = 0xFFFFFFFF
		} else {
			cpu.Regs[rd] = a / b
		}
	case 6: // REM (signed)
		if b == 0 {
			cpu.Regs[rd] = a
		} else if int32(a) == -0x80000000 && int32(b) == -1 {
			cpu.Regs[rd] = 0
		} else {
			cpu.Regs[rd] = uint32(int32(a) % int32(b))
		}
	case 7: // REMU (unsigned)
		if b == 0 {
			cpu.Regs[rd] = a
		} else {
			cpu.Regs[rd] = a % b
		}
	default:
		return nil, fmt.Errorf("%w: M-ext funct3=0x%x", ErrRVInvalidInstruction, funct3)
	}
	cpu.PC += 4
	return nil, nil
}

// handleEcall processes a system call based on a7 (x17).
func (cpu *RVCPU) handleEcall() {
	syscall := cpu.Regs[17] // a7
	switch syscall {
	case RVEcallHalt:
		cpu.ExitCode = cpu.Regs[10] // a0
		cpu.Halted = true
	case RVEcallOutput:
		cpu.OutputBuf = append(cpu.OutputBuf, byte(cpu.Regs[10]))
	case RVEcallInput:
		if cpu.inputPos < len(cpu.InputBuf) {
			cpu.Regs[10] = uint32(cpu.InputBuf[cpu.inputPos])
			cpu.inputPos++
		} else {
			cpu.Regs[10] = 0xFFFFFFFF // EOF
		}
	default:
		// Unknown syscall: halt with error code.
		cpu.ExitCode = 0xFF
		cpu.Halted = true
	}
}

// Decode and encode helpers are in riscv_encode.go.
