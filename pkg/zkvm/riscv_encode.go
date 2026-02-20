// riscv_encode.go contains RISC-V instruction encoding and decoding helpers
// for RV32IM. The encoding functions construct 32-bit instruction words from
// their fields, and the decoding functions extract fields from instruction
// words. Used by the CPU emulator and test suites.
//
// Part of the K+ roadmap for canonical RISC-V guest execution.
package zkvm

// --- Instruction decoding helpers ---

// decodeU extracts U-type fields: rd, imm[31:12].
func decodeU(instr uint32) (rd uint32, imm uint32) {
	rd = (instr >> 7) & 0x1F
	imm = instr & 0xFFFFF000
	return
}

// decodeJ extracts J-type fields: rd, imm (sign-extended 21-bit offset).
func decodeJ(instr uint32) (rd uint32, imm int32) {
	rd = (instr >> 7) & 0x1F
	// imm[20|10:1|11|19:12]
	rawImm := ((instr >> 31) << 20) | // bit 20
		(((instr >> 12) & 0xFF) << 12) | // bits 19:12
		(((instr >> 20) & 0x1) << 11) | // bit 11
		(((instr >> 21) & 0x3FF) << 1) // bits 10:1
	// Sign-extend from bit 20.
	if rawImm&(1<<20) != 0 {
		rawImm |= 0xFFF00000
	}
	imm = int32(rawImm)
	return
}

// decodeI extracts I-type fields: rd, rs1, imm (sign-extended 12-bit).
func decodeI(instr uint32) (rd uint32, rs1 uint32, imm int32) {
	rd = (instr >> 7) & 0x1F
	rs1 = (instr >> 15) & 0x1F
	rawImm := instr >> 20
	// Sign-extend from bit 11.
	if rawImm&(1<<11) != 0 {
		rawImm |= 0xFFFFF000
	}
	imm = int32(rawImm)
	return
}

// decodeS extracts S-type fields: rs1, rs2, imm (sign-extended 12-bit).
func decodeS(instr uint32) (rs1, rs2 uint32, imm int32) {
	rs1 = (instr >> 15) & 0x1F
	rs2 = (instr >> 20) & 0x1F
	rawImm := ((instr >> 7) & 0x1F) | (((instr >> 25) & 0x7F) << 5)
	if rawImm&(1<<11) != 0 {
		rawImm |= 0xFFFFF000
	}
	imm = int32(rawImm)
	return
}

// decodeB extracts B-type fields: rs1, rs2, imm (sign-extended 13-bit offset).
func decodeB(instr uint32) (rs1, rs2 uint32, imm int32) {
	rs1 = (instr >> 15) & 0x1F
	rs2 = (instr >> 20) & 0x1F
	// imm[12|10:5|4:1|11]
	rawImm := (((instr >> 31) & 0x1) << 12) | // bit 12
		(((instr >> 7) & 0x1) << 11) | // bit 11
		(((instr >> 25) & 0x3F) << 5) | // bits 10:5
		(((instr >> 8) & 0xF) << 1) // bits 4:1
	if rawImm&(1<<12) != 0 {
		rawImm |= 0xFFFFE000
	}
	imm = int32(rawImm)
	return
}

// --- Instruction encoding helpers (used for tests/program construction) ---

// EncodeRType encodes an R-type instruction.
func EncodeRType(opcode, rd, funct3, rs1, rs2, funct7 uint32) uint32 {
	return (funct7 << 25) | (rs2 << 20) | (rs1 << 15) | (funct3 << 12) | (rd << 7) | opcode
}

// EncodeIType encodes an I-type instruction.
func EncodeIType(opcode, rd, funct3, rs1 uint32, imm int32) uint32 {
	return (uint32(imm&0xFFF) << 20) | (rs1 << 15) | (funct3 << 12) | (rd << 7) | opcode
}

// EncodeSType encodes an S-type instruction.
func EncodeSType(opcode, funct3, rs1, rs2 uint32, imm int32) uint32 {
	immU := uint32(imm & 0xFFF)
	return ((immU >> 5) << 25) | (rs2 << 20) | (rs1 << 15) | (funct3 << 12) |
		((immU & 0x1F) << 7) | opcode
}

// EncodeBType encodes a B-type instruction.
func EncodeBType(opcode, funct3, rs1, rs2 uint32, imm int32) uint32 {
	immU := uint32(imm)
	return (((immU >> 12) & 0x1) << 31) | (((immU >> 5) & 0x3F) << 25) |
		(rs2 << 20) | (rs1 << 15) | (funct3 << 12) |
		(((immU >> 1) & 0xF) << 8) | (((immU >> 11) & 0x1) << 7) | opcode
}

// EncodeUType encodes a U-type instruction.
func EncodeUType(opcode, rd uint32, imm uint32) uint32 {
	return (imm & 0xFFFFF000) | (rd << 7) | opcode
}

// EncodeJType encodes a J-type instruction.
func EncodeJType(opcode, rd uint32, imm int32) uint32 {
	immU := uint32(imm)
	return (((immU >> 20) & 0x1) << 31) | (((immU >> 1) & 0x3FF) << 21) |
		(((immU >> 11) & 0x1) << 20) | (((immU >> 12) & 0xFF) << 12) |
		(rd << 7) | opcode
}
