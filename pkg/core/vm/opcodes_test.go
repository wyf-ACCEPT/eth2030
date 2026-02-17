package vm

import "testing"

func TestOpcodeConstants(t *testing.T) {
	tests := []struct {
		op   OpCode
		val  byte
		name string
	}{
		{STOP, 0x00, "STOP"},
		{ADD, 0x01, "ADD"},
		{MUL, 0x02, "MUL"},
		{SUB, 0x03, "SUB"},
		{DIV, 0x04, "DIV"},
		{KECCAK256, 0x20, "KECCAK256"},
		{ADDRESS, 0x30, "ADDRESS"},
		{CALLER, 0x33, "CALLER"},
		{PUSH0, 0x5f, "PUSH0"},
		{PUSH1, 0x60, "PUSH1"},
		{PUSH32, 0x7f, "PUSH32"},
		{DUP1, 0x80, "DUP1"},
		{DUP16, 0x8f, "DUP16"},
		{SWAP1, 0x90, "SWAP1"},
		{SWAP16, 0x9f, "SWAP16"},
		{LOG0, 0xa0, "LOG0"},
		{LOG4, 0xa4, "LOG4"},
		{CREATE, 0xf0, "CREATE"},
		{CALL, 0xf1, "CALL"},
		{RETURN, 0xf3, "RETURN"},
		{REVERT, 0xfd, "REVERT"},
		{INVALID, 0xfe, "INVALID"},
		{SELFDESTRUCT, 0xff, "SELFDESTRUCT"},
	}
	for _, tt := range tests {
		if byte(tt.op) != tt.val {
			t.Errorf("%s: got 0x%02x, want 0x%02x", tt.name, byte(tt.op), tt.val)
		}
	}
}

func TestOpcodeString(t *testing.T) {
	if s := ADD.String(); s != "ADD" {
		t.Errorf("ADD.String() = %q, want %q", s, "ADD")
	}
	if s := PUSH1.String(); s != "PUSH1" {
		t.Errorf("PUSH1.String() = %q, want %q", s, "PUSH1")
	}
	if s := SELFDESTRUCT.String(); s != "SELFDESTRUCT" {
		t.Errorf("SELFDESTRUCT.String() = %q, want %q", s, "SELFDESTRUCT")
	}
	// Unknown opcode
	unknown := OpCode(0xef)
	if s := unknown.String(); s != "opcode 0xef" {
		t.Errorf("unknown.String() = %q, want %q", s, "opcode 0xef")
	}
}

func TestOpcodeIsPush(t *testing.T) {
	if PUSH0.IsPush() {
		t.Error("PUSH0.IsPush() should be false")
	}
	if !PUSH1.IsPush() {
		t.Error("PUSH1.IsPush() should be true")
	}
	if !PUSH32.IsPush() {
		t.Error("PUSH32.IsPush() should be true")
	}
	if STOP.IsPush() {
		t.Error("STOP.IsPush() should be false")
	}
}

func TestPushRange(t *testing.T) {
	// Verify PUSH1..PUSH32 are contiguous from 0x60 to 0x7f
	for i := 0; i < 32; i++ {
		op := OpCode(0x60 + byte(i))
		if !op.IsPush() {
			t.Errorf("OpCode(0x%02x) should be a push", byte(op))
		}
	}
}

func TestDupSwapRange(t *testing.T) {
	// DUP1..DUP16: 0x80..0x8f
	if byte(DUP1) != 0x80 {
		t.Errorf("DUP1 = 0x%02x, want 0x80", byte(DUP1))
	}
	if byte(DUP16) != 0x8f {
		t.Errorf("DUP16 = 0x%02x, want 0x8f", byte(DUP16))
	}
	// SWAP1..SWAP16: 0x90..0x9f
	if byte(SWAP1) != 0x90 {
		t.Errorf("SWAP1 = 0x%02x, want 0x90", byte(SWAP1))
	}
	if byte(SWAP16) != 0x9f {
		t.Errorf("SWAP16 = 0x%02x, want 0x9f", byte(SWAP16))
	}
}
