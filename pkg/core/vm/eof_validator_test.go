package vm

import (
	"encoding/binary"
	"errors"
	"sync"
	"testing"
)

// buildValidEOFCode builds a minimal valid EOF container with the given code,
// type section, and optional data.
func buildValidEOFCode(ts TypeSection, code []byte, data []byte) []byte {
	return buildEOF([]TypeSection{ts}, [][]byte{code}, nil, data)
}

func TestEOFValidator_MinimalValid(t *testing.T) {
	v := NewEOFValidator()
	// STOP is a valid terminal in EOF.
	bytecode := buildValidEOFCode(
		TypeSection{Inputs: 0, Outputs: 0x80, MaxStackHeight: 0},
		[]byte{byte(STOP)},
		nil,
	)
	container, err := v.Validate(bytecode)
	if err != nil {
		t.Fatalf("Validate failed: %v", err)
	}
	if container.Version != EOFVersion {
		t.Errorf("version = %d, want %d", container.Version, EOFVersion)
	}
}

func TestEOFValidator_PushAndAdd(t *testing.T) {
	v := NewEOFValidator()
	// PUSH1 1; PUSH1 2; ADD; POP; STOP
	// Stack: 0 -> 1 -> 2 -> 1 -> 0 -> 0
	// Max increase above inputs(0) = 2
	code := []byte{byte(PUSH1), 0x01, byte(PUSH1), 0x02, byte(ADD), byte(POP), byte(STOP)}
	bytecode := buildValidEOFCode(
		TypeSection{Inputs: 0, Outputs: 0x80, MaxStackHeight: 2},
		code,
		nil,
	)
	_, err := v.Validate(bytecode)
	if err != nil {
		t.Fatalf("Validate failed: %v", err)
	}
}

func TestEOFValidator_WrongMaxStackHeight(t *testing.T) {
	v := NewEOFValidator()
	// PUSH1 1; PUSH1 2; ADD; POP; STOP -> max increase = 2
	code := []byte{byte(PUSH1), 0x01, byte(PUSH1), 0x02, byte(ADD), byte(POP), byte(STOP)}
	// Declare wrong max_stack_height = 1.
	bytecode := buildValidEOFCode(
		TypeSection{Inputs: 0, Outputs: 0x80, MaxStackHeight: 1},
		code,
		nil,
	)
	_, err := v.Validate(bytecode)
	if err == nil {
		t.Fatal("expected error for wrong max_stack_height, got nil")
	}
	if !errors.Is(err, ErrEOFInvalidStackHeight) {
		t.Errorf("expected ErrEOFInvalidStackHeight, got: %v", err)
	}
}

func TestEOFValidator_BannedOpcodes(t *testing.T) {
	v := NewEOFValidator()
	banned := []struct {
		name string
		op   OpCode
	}{
		{"JUMP", JUMP},
		{"JUMPI", JUMPI},
		{"JUMPDEST", JUMPDEST},
		{"PC", PC},
		{"GAS", GAS},
		{"SELFDESTRUCT", SELFDESTRUCT},
		{"CREATE", CREATE},
		{"CREATE2", CREATE2},
		{"CALL", CALL},
		{"CALLCODE", CALLCODE},
		{"DELEGATECALL", DELEGATECALL},
		{"STATICCALL", STATICCALL},
		{"CODESIZE", CODESIZE},
		{"CODECOPY", CODECOPY},
		{"EXTCODESIZE", EXTCODESIZE},
		{"EXTCODECOPY", EXTCODECOPY},
		{"EXTCODEHASH", EXTCODEHASH},
	}
	for _, tt := range banned {
		t.Run(tt.name, func(t *testing.T) {
			// Some banned opcodes need stack items. Provide enough stack
			// to avoid stack underflow masking the invalid opcode error.
			var code []byte
			// Push enough items (7 covers the worst case: CALL needs 7).
			for i := 0; i < 7; i++ {
				code = append(code, byte(PUSH1), 0x00)
			}
			code = append(code, byte(tt.op))
			code = append(code, byte(STOP))

			bytecode := buildValidEOFCode(
				TypeSection{Inputs: 0, Outputs: 0x80, MaxStackHeight: 7},
				code,
				nil,
			)
			_, err := v.Validate(bytecode)
			if err == nil {
				t.Fatalf("expected error for banned opcode %s, got nil", tt.name)
			}
			if !errors.Is(err, ErrEOFInvalidOpcode) {
				t.Errorf("expected ErrEOFInvalidOpcode for %s, got: %v", tt.name, err)
			}
		})
	}
}

func TestEOFValidator_TruncatedImmediate(t *testing.T) {
	v := NewEOFValidator()
	// PUSH2 with only 1 byte of data.
	code := []byte{byte(PUSH2), 0x01}
	bytecode := buildValidEOFCode(
		TypeSection{Inputs: 0, Outputs: 0x80, MaxStackHeight: 0},
		code,
		nil,
	)
	_, err := v.Validate(bytecode)
	if err == nil {
		t.Fatal("expected error for truncated immediate, got nil")
	}
	if !errors.Is(err, ErrEOFTruncatedImmediate) {
		t.Errorf("expected ErrEOFTruncatedImmediate, got: %v", err)
	}
}

func TestEOFValidator_RJUMP_Valid(t *testing.T) {
	v := NewEOFValidator()
	// RJUMP +0 (jump to next instruction = STOP)
	// Offset 0: RJUMP [00 00] -> target = 0 + 3 + 0 = 3
	// Offset 3: STOP
	code := []byte{byte(RJUMP), 0x00, 0x00, byte(STOP)}
	bytecode := buildValidEOFCode(
		TypeSection{Inputs: 0, Outputs: 0x80, MaxStackHeight: 0},
		code,
		nil,
	)
	_, err := v.Validate(bytecode)
	if err != nil {
		t.Fatalf("Validate failed: %v", err)
	}
}

func TestEOFValidator_RJUMP_Backward(t *testing.T) {
	v := NewEOFValidator()
	// Offset 0: STOP (but code section 0's entry, non-returning)
	// We need RJUMP to jump backward. Let's do:
	// Offset 0: RJUMP +3 -> offset 6 (RJUMP -6 -> offset 0)
	// Actually, a backward jump creating a loop needs a STOP somewhere.
	// Let's create: PUSH1 0; RJUMP -4 (back to PUSH1)
	// But that would loop infinitely. Validator should accept it though
	// since stack is consistent (it's a valid loop).
	// Offset 0: PUSH1 0x00  (2 bytes)
	// Offset 2: POP          (1 byte)
	// Offset 3: RJUMP -3    -> target = 3 + 3 + (-3) = 3 (self-loop, but that's to itself, before the RJUMP)
	// Actually let's do target = 0:
	// Offset 3: RJUMP with offset = 0 - (3+3) = -6 => 0xFFFA
	rel := int16(-6)
	relBytes := make([]byte, 2)
	binary.BigEndian.PutUint16(relBytes, uint16(rel))
	code := []byte{byte(PUSH1), 0x00, byte(POP), byte(RJUMP), relBytes[0], relBytes[1]}
	bytecode := buildValidEOFCode(
		TypeSection{Inputs: 0, Outputs: 0x80, MaxStackHeight: 1},
		code,
		nil,
	)
	_, err := v.Validate(bytecode)
	if err != nil {
		t.Fatalf("Validate backward RJUMP failed: %v", err)
	}
}

func TestEOFValidator_RJUMP_InvalidTarget(t *testing.T) {
	v := NewEOFValidator()
	// RJUMP target into the middle of a PUSH2 immediate.
	// Offset 0: PUSH2 0x0000  (3 bytes)
	// Offset 3: POP           (1 byte)
	// Offset 4: RJUMP to offset 1 (middle of PUSH2 data)
	// rel = 1 - (4 + 3) = -6
	rel := int16(-6)
	relBytes := make([]byte, 2)
	binary.BigEndian.PutUint16(relBytes, uint16(rel))
	code := []byte{byte(PUSH2), 0x00, 0x00, byte(POP), byte(RJUMP), relBytes[0], relBytes[1]}
	bytecode := buildValidEOFCode(
		TypeSection{Inputs: 0, Outputs: 0x80, MaxStackHeight: 1},
		code,
		nil,
	)
	_, err := v.Validate(bytecode)
	if err == nil {
		t.Fatal("expected error for RJUMP into PUSH data, got nil")
	}
	if !errors.Is(err, ErrEOFInvalidRJUMPTarget) {
		t.Errorf("expected ErrEOFInvalidRJUMPTarget, got: %v", err)
	}
}

func TestEOFValidator_RJUMPI_Valid(t *testing.T) {
	v := NewEOFValidator()
	// PUSH1 1; RJUMPI +0; STOP
	// Stack: 0 -> 1 -> 0 (RJUMPI pops condition)
	// RJUMPI at offset 2, target = 2 + 3 + 0 = 5 = STOP
	code := []byte{byte(PUSH1), 0x01, byte(RJUMPI), 0x00, 0x00, byte(STOP)}
	bytecode := buildValidEOFCode(
		TypeSection{Inputs: 0, Outputs: 0x80, MaxStackHeight: 1},
		code,
		nil,
	)
	_, err := v.Validate(bytecode)
	if err != nil {
		t.Fatalf("Validate failed: %v", err)
	}
}

func TestEOFValidator_StackUnderflow(t *testing.T) {
	v := NewEOFValidator()
	// ADD with no items on stack.
	code := []byte{byte(ADD), byte(STOP)}
	bytecode := buildValidEOFCode(
		TypeSection{Inputs: 0, Outputs: 0x80, MaxStackHeight: 0},
		code,
		nil,
	)
	_, err := v.Validate(bytecode)
	if err == nil {
		t.Fatal("expected stack underflow error, got nil")
	}
	if !errors.Is(err, ErrEOFStackUnderflow) {
		t.Errorf("expected ErrEOFStackUnderflow, got: %v", err)
	}
}

func TestEOFValidator_FallsOffEnd(t *testing.T) {
	v := NewEOFValidator()
	// PUSH1 0x00 -- no terminal.
	code := []byte{byte(PUSH1), 0x00}
	bytecode := buildValidEOFCode(
		TypeSection{Inputs: 0, Outputs: 0x80, MaxStackHeight: 1},
		code,
		nil,
	)
	_, err := v.Validate(bytecode)
	if err == nil {
		t.Fatal("expected error for code falling off the end, got nil")
	}
}

func TestEOFValidator_MultipleSections(t *testing.T) {
	v := NewEOFValidator()
	// Section 0: non-returning, calls section 1.
	// CALLF 0x0001; STOP
	sec0 := []byte{byte(CALLF), 0x00, 0x01, byte(POP), byte(STOP)}
	// Section 1: takes 0 inputs, returns 1 output.
	// PUSH1 42; RETF
	sec1 := []byte{byte(PUSH1), 0x2A, byte(RETF)}

	types := []TypeSection{
		{Inputs: 0, Outputs: 0x80, MaxStackHeight: 1},
		{Inputs: 0, Outputs: 1, MaxStackHeight: 1},
	}
	bytecode := buildEOF(types, [][]byte{sec0, sec1}, nil, nil)
	_, err := v.Validate(bytecode)
	if err != nil {
		t.Fatalf("Validate multi-section failed: %v", err)
	}
}

func TestEOFValidator_InvalidCALLFTarget(t *testing.T) {
	v := NewEOFValidator()
	// CALLF to section 5 which doesn't exist.
	code := []byte{byte(CALLF), 0x00, 0x05, byte(STOP)}
	bytecode := buildValidEOFCode(
		TypeSection{Inputs: 0, Outputs: 0x80, MaxStackHeight: 0},
		code,
		nil,
	)
	_, err := v.Validate(bytecode)
	if err == nil {
		t.Fatal("expected error for invalid CALLF target, got nil")
	}
	if !errors.Is(err, ErrEOFInvalidCALLFTarget) {
		t.Errorf("expected ErrEOFInvalidCALLFTarget, got: %v", err)
	}
}

func TestEOFValidator_WithDataSection(t *testing.T) {
	v := NewEOFValidator()
	// DATASIZE; POP; STOP
	code := []byte{byte(DATASIZE), byte(POP), byte(STOP)}
	data := []byte{0xDE, 0xAD, 0xBE, 0xEF}
	bytecode := buildValidEOFCode(
		TypeSection{Inputs: 0, Outputs: 0x80, MaxStackHeight: 1},
		code,
		data,
	)
	container, err := v.Validate(bytecode)
	if err != nil {
		t.Fatalf("Validate failed: %v", err)
	}
	if len(container.DataSection) != 4 {
		t.Errorf("data section len = %d, want 4", len(container.DataSection))
	}
}

func TestEOFValidator_NestedContainers(t *testing.T) {
	v := NewEOFValidator()

	inner := buildValidEOFCode(
		TypeSection{Inputs: 0, Outputs: 0x80, MaxStackHeight: 0},
		[]byte{byte(STOP)},
		nil,
	)
	types := []TypeSection{{Inputs: 0, Outputs: 0x80, MaxStackHeight: 0}}
	codes := [][]byte{{byte(STOP)}}
	bytecode := buildEOF(types, codes, [][]byte{inner}, nil)

	container, err := v.Validate(bytecode)
	if err != nil {
		t.Fatalf("Validate with nested container failed: %v", err)
	}
	if len(container.ContainerSections) != 1 {
		t.Errorf("container sections = %d, want 1", len(container.ContainerSections))
	}
}

func TestEOFValidator_InvalidNestedContainer(t *testing.T) {
	v := NewEOFValidator()

	// Invalid inner: wrong magic.
	invalidInner := []byte{0xEF, 0x01, 0x01, 0x00}
	types := []TypeSection{{Inputs: 0, Outputs: 0x80, MaxStackHeight: 0}}
	codes := [][]byte{{byte(STOP)}}
	bytecode := buildEOF(types, codes, [][]byte{invalidInner}, nil)

	_, err := v.Validate(bytecode)
	if err == nil {
		t.Fatal("expected error for invalid nested container, got nil")
	}
}

func TestEOFValidator_ThreadSafety(t *testing.T) {
	v := NewEOFValidator()
	bytecode := buildValidEOFCode(
		TypeSection{Inputs: 0, Outputs: 0x80, MaxStackHeight: 0},
		[]byte{byte(STOP)},
		nil,
	)

	var wg sync.WaitGroup
	errs := make(chan error, 50)
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := v.Validate(bytecode)
			if err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("concurrent Validate failed: %v", err)
	}
}

func TestEOFValidator_RETURN_Terminal(t *testing.T) {
	v := NewEOFValidator()
	// PUSH1 0; PUSH1 0; RETURN
	code := []byte{byte(PUSH1), 0x00, byte(PUSH1), 0x00, byte(RETURN)}
	bytecode := buildValidEOFCode(
		TypeSection{Inputs: 0, Outputs: 0x80, MaxStackHeight: 2},
		code,
		nil,
	)
	_, err := v.Validate(bytecode)
	if err != nil {
		t.Fatalf("Validate failed: %v", err)
	}
}

func TestEOFValidator_REVERT_Terminal(t *testing.T) {
	v := NewEOFValidator()
	// PUSH1 0; PUSH1 0; REVERT
	code := []byte{byte(PUSH1), 0x00, byte(PUSH1), 0x00, byte(REVERT)}
	bytecode := buildValidEOFCode(
		TypeSection{Inputs: 0, Outputs: 0x80, MaxStackHeight: 2},
		code,
		nil,
	)
	_, err := v.Validate(bytecode)
	if err != nil {
		t.Fatalf("Validate failed: %v", err)
	}
}

func TestEOFValidator_InvalidMagic(t *testing.T) {
	v := NewEOFValidator()
	_, err := v.Validate([]byte{0xFE, 0x00, 0x01})
	if err == nil {
		t.Fatal("expected error for invalid magic, got nil")
	}
}

func TestEOFValidator_IsEOF(t *testing.T) {
	// Quick check that IsEOF works for EOF prefix.
	if !IsEOF([]byte{0xEF, 0x00, 0x01}) {
		t.Error("IsEOF should return true for EOF prefix")
	}
	if IsEOF([]byte{0x60, 0x00}) {
		t.Error("IsEOF should return false for legacy code")
	}
}

func TestEOFValidator_DUP_SWAP_StackEffects(t *testing.T) {
	v := NewEOFValidator()
	// PUSH1 1; PUSH1 2; DUP2; POP; POP; POP; STOP
	// Stack: 0->1->2->3(dup2)->2(pop)->1(pop)->0(pop)
	// Max increase = 3
	code := []byte{
		byte(PUSH1), 0x01,
		byte(PUSH1), 0x02,
		byte(DUP2),
		byte(POP), byte(POP), byte(POP),
		byte(STOP),
	}
	bytecode := buildValidEOFCode(
		TypeSection{Inputs: 0, Outputs: 0x80, MaxStackHeight: 3},
		code,
		nil,
	)
	_, err := v.Validate(bytecode)
	if err != nil {
		t.Fatalf("Validate failed: %v", err)
	}
}

func TestEOFValidator_RJUMPI_BothPaths(t *testing.T) {
	v := NewEOFValidator()
	// Test RJUMPI where both paths (taken / not-taken) lead to valid code.
	// PUSH1 0; RJUMPI +2; PUSH1 1; POP; STOP
	// Offset 0: PUSH1 0x00      (stack 0->1)
	// Offset 2: RJUMPI +2       (stack 1->0, branch to offset 7)
	// Offset 5: PUSH1 0x01      (stack 0->1)
	// Offset 7: POP             (stack 1->0)
	// Offset 8: STOP
	// But if RJUMPI taken, we jump to offset 7 (POP) with stack 0, underflow.
	// Instead, let's make both paths have same stack height:
	// PUSH1 cond; RJUMPI +3; PUSH1 val; RJUMP +0; POP; STOP
	// Offset 0: PUSH1 cond     (0->1)
	// Offset 2: RJUMPI +3      (1->0, target = 2+3+3 = 8)
	// Offset 5: PUSH1 val      (0->1)
	// Offset 7: POP            (1->0)
	// Offset 8: STOP           (0)
	// On RJUMPI taken: stack 0, target=8=STOP, ok.
	// On RJUMPI not-taken: stack 0, fall to 5=PUSH1, stack 1, POP, 0, STOP, ok.
	code := []byte{
		byte(PUSH1), 0x01,
		byte(RJUMPI), 0x00, 0x03,
		byte(PUSH1), 0x42,
		byte(POP),
		byte(STOP),
	}
	bytecode := buildValidEOFCode(
		TypeSection{Inputs: 0, Outputs: 0x80, MaxStackHeight: 1},
		code,
		nil,
	)
	_, err := v.Validate(bytecode)
	if err != nil {
		t.Fatalf("Validate RJUMPI both paths failed: %v", err)
	}
}

func TestEOFValidator_EOFCreateOpcode(t *testing.T) {
	v := NewEOFValidator()
	// Test that EOFCREATE (1-byte immediate) is accepted.
	// PUSH1 x4; EOFCREATE 0; POP; STOP
	// Stack: 0->1->2->3->4 (4 pushes) -> EOFCREATE(pops 4, pushes 1) -> 1 -> POP -> 0 -> STOP
	code := []byte{
		byte(PUSH1), 0x00, byte(PUSH1), 0x00,
		byte(PUSH1), 0x00, byte(PUSH1), 0x00,
		byte(EOFCREATE), 0x00,
		byte(POP),
		byte(STOP),
	}
	bytecode := buildValidEOFCode(
		TypeSection{Inputs: 0, Outputs: 0x80, MaxStackHeight: 4},
		code,
		nil,
	)
	_, err := v.Validate(bytecode)
	if err != nil {
		t.Fatalf("Validate EOFCREATE failed: %v", err)
	}
}

func TestEOFValidator_ExtcallOpcodes(t *testing.T) {
	v := NewEOFValidator()
	// EXTCALL: pops 4, pushes 1
	code := []byte{
		byte(PUSH1), 0x00, byte(PUSH1), 0x00,
		byte(PUSH1), 0x00, byte(PUSH1), 0x00,
		byte(EXTCALL),
		byte(POP),
		byte(STOP),
	}
	bytecode := buildValidEOFCode(
		TypeSection{Inputs: 0, Outputs: 0x80, MaxStackHeight: 4},
		code,
		nil,
	)
	_, err := v.Validate(bytecode)
	if err != nil {
		t.Fatalf("Validate EXTCALL failed: %v", err)
	}
}
