package vm

import (
	"encoding/binary"
	"testing"
)

func TestEOFDeepValidator_ValidMinimal(t *testing.T) {
	dv := NewEOFDeepValidator()
	bytecode := buildValidEOFCode(
		TypeSection{Inputs: 0, Outputs: 0x80, MaxStackHeight: 0},
		[]byte{byte(STOP)},
		nil,
	)
	container, stats, err := dv.ValidateDeep(bytecode)
	if err != nil {
		t.Fatalf("ValidateDeep failed: %v", err)
	}
	if container.Version != EOFVersion {
		t.Errorf("version = %d, want %d", container.Version, EOFVersion)
	}
	if stats.NumCodeSections != 1 {
		t.Errorf("NumCodeSections = %d, want 1", stats.NumCodeSections)
	}
	if stats.TotalCodeBytes != 1 {
		t.Errorf("TotalCodeBytes = %d, want 1", stats.TotalCodeBytes)
	}
}

func TestEOFDeepValidator_DataLoadNValid(t *testing.T) {
	dv := NewEOFDeepValidator()
	// DATALOADN offset=0 loads 32 bytes starting at 0. Data is 32 bytes.
	code := []byte{byte(DATALOADN), 0x00, 0x00, byte(POP), byte(STOP)}
	data := make([]byte, 32)
	bytecode := buildValidEOFCode(
		TypeSection{Inputs: 0, Outputs: 0x80, MaxStackHeight: 1},
		code,
		data,
	)
	_, _, err := dv.ValidateDeep(bytecode)
	if err != nil {
		t.Fatalf("ValidateDeep failed: %v", err)
	}
}

func TestEOFDeepValidator_DataLoadNOutOfBounds(t *testing.T) {
	dv := NewEOFDeepValidator()
	// DATALOADN offset=10, but data is only 8 bytes. 10+32 > 8.
	code := []byte{byte(DATALOADN), 0x00, 0x0A, byte(POP), byte(STOP)}
	data := make([]byte, 8)
	bytecode := buildValidEOFCode(
		TypeSection{Inputs: 0, Outputs: 0x80, MaxStackHeight: 1},
		code,
		data,
	)
	_, _, err := dv.ValidateDeep(bytecode)
	if err == nil {
		t.Fatal("expected ErrEOFDataLoadNOOB, got nil")
	}
}

func TestEOFDeepValidator_DataLoadNEdge(t *testing.T) {
	dv := NewEOFDeepValidator()
	// DATALOADN offset=1, data=33 bytes: 1+32=33 <= 33, valid.
	code := []byte{byte(DATALOADN), 0x00, 0x01, byte(POP), byte(STOP)}
	data := make([]byte, 33)
	bytecode := buildValidEOFCode(
		TypeSection{Inputs: 0, Outputs: 0x80, MaxStackHeight: 1},
		code,
		data,
	)
	_, _, err := dv.ValidateDeep(bytecode)
	if err != nil {
		t.Fatalf("ValidateDeep failed at edge: %v", err)
	}
}

func TestEOFDeepValidator_DupNValid(t *testing.T) {
	dv := NewEOFDeepValidator()
	// Push 2 items, DUPN 1 (duplicates item at depth 2, 0-indexed=1).
	// Stack: 0->1->2->3(dup)->2(pop)->1(pop)->0(pop)->stop
	code := []byte{
		byte(PUSH1), 0x01, byte(PUSH1), 0x02,
		byte(DUPN), 0x01, // dup item at index 1
		byte(POP), byte(POP), byte(POP),
		byte(STOP),
	}
	bytecode := buildValidEOFCode(
		TypeSection{Inputs: 0, Outputs: 0x80, MaxStackHeight: 3},
		code,
		nil,
	)
	_, _, err := dv.ValidateDeep(bytecode)
	if err != nil {
		t.Fatalf("ValidateDeep failed: %v", err)
	}
}

func TestEOFDeepValidator_SwapNValid(t *testing.T) {
	dv := NewEOFDeepValidator()
	// Push 3 items, SWAPN 0 (swaps top with item 2 positions below).
	code := []byte{
		byte(PUSH1), 0x01, byte(PUSH1), 0x02, byte(PUSH1), 0x03,
		byte(SWAPN), 0x00,
		byte(POP), byte(POP), byte(POP),
		byte(STOP),
	}
	bytecode := buildValidEOFCode(
		TypeSection{Inputs: 0, Outputs: 0x80, MaxStackHeight: 3},
		code,
		nil,
	)
	_, _, err := dv.ValidateDeep(bytecode)
	if err != nil {
		t.Fatalf("ValidateDeep failed: %v", err)
	}
}

func TestEOFDeepValidator_ExchangeValid(t *testing.T) {
	dv := NewEOFDeepValidator()
	// Push 4 items, EXCHANGE 0x00 (n=1, m=1: swap items at depth 1 and 2).
	code := []byte{
		byte(PUSH1), 0x01, byte(PUSH1), 0x02,
		byte(PUSH1), 0x03, byte(PUSH1), 0x04,
		byte(EXCHANGE), 0x00, // n=1, m=1
		byte(POP), byte(POP), byte(POP), byte(POP),
		byte(STOP),
	}
	bytecode := buildValidEOFCode(
		TypeSection{Inputs: 0, Outputs: 0x80, MaxStackHeight: 4},
		code,
		nil,
	)
	_, _, err := dv.ValidateDeep(bytecode)
	if err != nil {
		t.Fatalf("ValidateDeep failed: %v", err)
	}
}

func TestEOFDeepValidator_BannedOpcodePassthrough(t *testing.T) {
	dv := NewEOFDeepValidator()
	// JUMP is banned in EOF; deep validator should propagate this.
	code := []byte{byte(PUSH1), 0x00, byte(JUMP), byte(STOP)}
	bytecode := buildValidEOFCode(
		TypeSection{Inputs: 0, Outputs: 0x80, MaxStackHeight: 1},
		code,
		nil,
	)
	_, _, err := dv.ValidateDeep(bytecode)
	if err == nil {
		t.Fatal("expected error for banned JUMP, got nil")
	}
}

func TestEOFDeepValidator_ContainerStats_OpcodeFrequency(t *testing.T) {
	dv := NewEOFDeepValidator()
	// PUSH1 1; PUSH1 2; ADD; POP; STOP
	code := []byte{byte(PUSH1), 0x01, byte(PUSH1), 0x02, byte(ADD), byte(POP), byte(STOP)}
	bytecode := buildValidEOFCode(
		TypeSection{Inputs: 0, Outputs: 0x80, MaxStackHeight: 2},
		code,
		nil,
	)
	_, stats, err := dv.ValidateDeep(bytecode)
	if err != nil {
		t.Fatalf("ValidateDeep failed: %v", err)
	}
	if stats.OpcodeFrequency[PUSH1] != 2 {
		t.Errorf("PUSH1 frequency = %d, want 2", stats.OpcodeFrequency[PUSH1])
	}
	if stats.OpcodeFrequency[ADD] != 1 {
		t.Errorf("ADD frequency = %d, want 1", stats.OpcodeFrequency[ADD])
	}
	if stats.OpcodeFrequency[POP] != 1 {
		t.Errorf("POP frequency = %d, want 1", stats.OpcodeFrequency[POP])
	}
	if stats.OpcodeFrequency[STOP] != 1 {
		t.Errorf("STOP frequency = %d, want 1", stats.OpcodeFrequency[STOP])
	}
}

func TestEOFDeepValidator_CallGraph(t *testing.T) {
	dv := NewEOFDeepValidator()
	// Section 0: CALLF to section 1; POP; STOP
	sec0 := []byte{byte(CALLF), 0x00, 0x01, byte(POP), byte(STOP)}
	// Section 1: PUSH1 42; RETF
	sec1 := []byte{byte(PUSH1), 0x2A, byte(RETF)}

	types := []TypeSection{
		{Inputs: 0, Outputs: 0x80, MaxStackHeight: 1},
		{Inputs: 0, Outputs: 1, MaxStackHeight: 1},
	}
	bytecode := buildEOF(types, [][]byte{sec0, sec1}, nil, nil)
	_, stats, err := dv.ValidateDeep(bytecode)
	if err != nil {
		t.Fatalf("ValidateDeep multi-section failed: %v", err)
	}
	targets, ok := stats.CallGraph[0]
	if !ok || len(targets) != 1 || targets[0] != 1 {
		t.Errorf("CallGraph[0] = %v, want [1]", targets)
	}
	if stats.HasRecursion {
		t.Error("should not detect recursion in linear call chain")
	}
}

func TestEOFDeepValidator_RecursiveCallGraph(t *testing.T) {
	dv := NewEOFDeepValidator()
	// Section 0 (non-returning): CALLF section 1; POP; STOP
	// Section 1: CALLF section 0 -> recursion. But section 0 is non-returning,
	// so CALLF pops 0, pushes 0. Section 1 returns 1 output.
	// Actually section 0 is non-returning (0x80), CALLF to it is still valid
	// structurally for call graph detection. Let's make section 1 CALLF 0
	// and then RETF. Since section 0 is non-returning, CALLF 0 pushes 0 items.
	// Section 1 needs to return 1 output, so push something first.

	// Section 0: CALLF 1; POP; STOP
	sec0 := []byte{byte(CALLF), 0x00, 0x01, byte(POP), byte(STOP)}
	// Section 1: PUSH1 42; CALLF 0 (non-returning call); RETF
	// Actually non-returning CALLF is terminal... let's just check cycle detection.
	// Let's make mutual recursion between sections 1 and 2:
	// Section 1 -> CALLF 2, RETF
	// Section 2 -> CALLF 1, RETF
	sec1 := []byte{byte(PUSH1), 0x01, byte(CALLF), 0x00, 0x02, byte(POP), byte(RETF)}
	sec2 := []byte{byte(PUSH1), 0x01, byte(CALLF), 0x00, 0x01, byte(POP), byte(RETF)}

	types := []TypeSection{
		{Inputs: 0, Outputs: 0x80, MaxStackHeight: 1},
		{Inputs: 0, Outputs: 1, MaxStackHeight: 2},
		{Inputs: 0, Outputs: 1, MaxStackHeight: 2},
	}
	bytecode := buildEOF(types, [][]byte{sec0, sec1, sec2}, nil, nil)
	_, stats, err := dv.ValidateDeep(bytecode)
	if err != nil {
		t.Fatalf("ValidateDeep failed: %v", err)
	}
	if !stats.HasRecursion {
		t.Error("expected recursion detection for mutual CALLF cycle")
	}
}

func TestEOFDeepValidator_GasEstimate(t *testing.T) {
	dv := NewEOFDeepValidator()
	// SLOAD is expensive (100 gas), rest is cheap.
	code := []byte{byte(PUSH1), 0x00, byte(SLOAD), byte(POP), byte(STOP)}
	bytecode := buildValidEOFCode(
		TypeSection{Inputs: 0, Outputs: 0x80, MaxStackHeight: 1},
		code,
		nil,
	)
	_, stats, err := dv.ValidateDeep(bytecode)
	if err != nil {
		t.Fatalf("ValidateDeep failed: %v", err)
	}
	if stats.EstimatedGas < 100 {
		t.Errorf("EstimatedGas = %d, expected >= 100 for SLOAD", stats.EstimatedGas)
	}
}

func TestEOFDeepValidator_NestedContainerStats(t *testing.T) {
	dv := NewEOFDeepValidator()
	inner := buildValidEOFCode(
		TypeSection{Inputs: 0, Outputs: 0x80, MaxStackHeight: 0},
		[]byte{byte(STOP)},
		nil,
	)
	types := []TypeSection{{Inputs: 0, Outputs: 0x80, MaxStackHeight: 0}}
	codes := [][]byte{{byte(STOP)}}
	bytecode := buildEOF(types, codes, [][]byte{inner}, nil)

	_, stats, err := dv.ValidateDeep(bytecode)
	if err != nil {
		t.Fatalf("ValidateDeep with nested container failed: %v", err)
	}
	if stats.NumContainerSections != 1 {
		t.Errorf("NumContainerSections = %d, want 1", stats.NumContainerSections)
	}
}

func TestSectionGasEstimate(t *testing.T) {
	// Simple code: ADD + STOP
	code := []byte{byte(ADD), byte(STOP)}
	gas := SectionGasEstimate(code)
	if gas < 3 {
		t.Errorf("expected >= 3 gas for ADD, got %d", gas)
	}
}

func TestContainerSize(t *testing.T) {
	c := &EOFContainer{
		Version:      EOFVersion,
		TypeSections: []TypeSection{{Inputs: 0, Outputs: 0x80, MaxStackHeight: 0}},
		CodeSections: [][]byte{{byte(STOP)}},
	}
	size := ContainerSize(c)
	serialized := SerializeEOF(c)
	if size != len(serialized) {
		t.Errorf("ContainerSize = %d, actual serialized = %d", size, len(serialized))
	}
}

func TestContainerSize_Nil(t *testing.T) {
	if ContainerSize(nil) != 0 {
		t.Error("ContainerSize(nil) should be 0")
	}
}

func TestValidateStreaming_Chunked(t *testing.T) {
	dv := NewEOFDeepValidator()
	bytecode := buildValidEOFCode(
		TypeSection{Inputs: 0, Outputs: 0x80, MaxStackHeight: 0},
		[]byte{byte(STOP)},
		nil,
	)
	// Split into 2 chunks.
	mid := len(bytecode) / 2
	chunks := [][]byte{bytecode[:mid], bytecode[mid:]}
	container, err := dv.ValidateStreaming(chunks)
	if err != nil {
		t.Fatalf("ValidateStreaming failed: %v", err)
	}
	if container.Version != EOFVersion {
		t.Errorf("version = %d, want %d", container.Version, EOFVersion)
	}
}

func TestEOFDeepValidator_DataSection(t *testing.T) {
	dv := NewEOFDeepValidator()
	data := make([]byte, 64)
	for i := range data {
		data[i] = byte(i)
	}
	code := []byte{byte(DATASIZE), byte(POP), byte(STOP)}
	bytecode := buildValidEOFCode(
		TypeSection{Inputs: 0, Outputs: 0x80, MaxStackHeight: 1},
		code,
		data,
	)
	_, stats, err := dv.ValidateDeep(bytecode)
	if err != nil {
		t.Fatalf("ValidateDeep failed: %v", err)
	}
	if stats.TotalDataBytes != 64 {
		t.Errorf("TotalDataBytes = %d, want 64", stats.TotalDataBytes)
	}
}

func TestEOFDeepValidator_MaxStackDepth(t *testing.T) {
	dv := NewEOFDeepValidator()
	// Push 5 items, pop 5, stop: max stack height = 5.
	code := []byte{
		byte(PUSH1), 0x01, byte(PUSH1), 0x02, byte(PUSH1), 0x03,
		byte(PUSH1), 0x04, byte(PUSH1), 0x05,
		byte(POP), byte(POP), byte(POP), byte(POP), byte(POP),
		byte(STOP),
	}
	bytecode := buildValidEOFCode(
		TypeSection{Inputs: 0, Outputs: 0x80, MaxStackHeight: 5},
		code,
		nil,
	)
	_, stats, err := dv.ValidateDeep(bytecode)
	if err != nil {
		t.Fatalf("ValidateDeep failed: %v", err)
	}
	// MaxStackDepth = inputs(0) + max_stack_height(5) = 5
	if stats.MaxStackDepth != 5 {
		t.Errorf("MaxStackDepth = %d, want 5", stats.MaxStackDepth)
	}
}

func TestEOFDeepValidator_RJUMPWithDataLoadN(t *testing.T) {
	dv := NewEOFDeepValidator()
	// Combine RJUMP and DATALOADN in one section:
	// DATALOADN 0; POP; RJUMP +0; STOP
	data := make([]byte, 32)
	code := []byte{
		byte(DATALOADN), 0x00, 0x00,
		byte(POP),
		byte(RJUMP), 0x00, 0x00,
		byte(STOP),
	}
	bytecode := buildValidEOFCode(
		TypeSection{Inputs: 0, Outputs: 0x80, MaxStackHeight: 1},
		code,
		data,
	)
	_, _, err := dv.ValidateDeep(bytecode)
	if err != nil {
		t.Fatalf("ValidateDeep failed: %v", err)
	}
}

func TestEOFDeepValidator_MultipleDataLoadN(t *testing.T) {
	dv := NewEOFDeepValidator()
	// Two DATALOADNs: offset 0 and offset 32, data = 64 bytes.
	data := make([]byte, 64)
	off1 := make([]byte, 2)
	binary.BigEndian.PutUint16(off1, 0)
	off2 := make([]byte, 2)
	binary.BigEndian.PutUint16(off2, 32)
	code := []byte{
		byte(DATALOADN), off1[0], off1[1],
		byte(POP),
		byte(DATALOADN), off2[0], off2[1],
		byte(POP),
		byte(STOP),
	}
	bytecode := buildValidEOFCode(
		TypeSection{Inputs: 0, Outputs: 0x80, MaxStackHeight: 1},
		code,
		data,
	)
	_, _, err := dv.ValidateDeep(bytecode)
	if err != nil {
		t.Fatalf("ValidateDeep failed: %v", err)
	}
}
