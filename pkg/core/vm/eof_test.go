package vm

import (
	"bytes"
	"testing"
)

// buildEOF is a test helper that builds a minimal valid EOF container.
func buildEOF(types []TypeSection, codes [][]byte, containers [][]byte, data []byte) []byte {
	c := &EOFContainer{
		Version:           EOFVersion,
		TypeSections:      types,
		CodeSections:      codes,
		ContainerSections: containers,
		DataSection:       data,
	}
	return SerializeEOF(c)
}

// minimalEOF returns a minimal valid EOF container with a single code section.
func minimalEOF() []byte {
	return buildEOF(
		[]TypeSection{{Inputs: 0, Outputs: 0x80, MaxStackHeight: 0}},
		[][]byte{{byte(STOP)}},
		nil,
		nil,
	)
}

func TestIsEOF(t *testing.T) {
	tests := []struct {
		name string
		code []byte
		want bool
	}{
		{"valid magic", []byte{0xEF, 0x00, 0x01}, true},
		{"too short", []byte{0xEF}, false},
		{"empty", nil, false},
		{"wrong magic0", []byte{0xFE, 0x00, 0x01}, false},
		{"wrong magic1", []byte{0xEF, 0x01, 0x01}, false},
		{"just magic", []byte{0xEF, 0x00}, true},
		{"legacy code", []byte{0x60, 0x00, 0x60, 0x00}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsEOF(tt.code); got != tt.want {
				t.Errorf("IsEOF() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseEOF_MinimalValid(t *testing.T) {
	code := minimalEOF()
	container, err := ParseEOF(code)
	if err != nil {
		t.Fatalf("ParseEOF failed: %v", err)
	}
	if container.Version != EOFVersion {
		t.Errorf("version = %d, want %d", container.Version, EOFVersion)
	}
	if len(container.TypeSections) != 1 {
		t.Fatalf("type sections = %d, want 1", len(container.TypeSections))
	}
	if container.TypeSections[0].Inputs != 0 {
		t.Errorf("inputs = %d, want 0", container.TypeSections[0].Inputs)
	}
	if container.TypeSections[0].Outputs != 0x80 {
		t.Errorf("outputs = 0x%02x, want 0x80", container.TypeSections[0].Outputs)
	}
	if len(container.CodeSections) != 1 {
		t.Fatalf("code sections = %d, want 1", len(container.CodeSections))
	}
	if len(container.CodeSections[0]) != 1 || container.CodeSections[0][0] != byte(STOP) {
		t.Errorf("code section 0 = %x, want [00]", container.CodeSections[0])
	}
	if len(container.ContainerSections) != 0 {
		t.Errorf("container sections = %d, want 0", len(container.ContainerSections))
	}
	if len(container.DataSection) != 0 {
		t.Errorf("data section len = %d, want 0", len(container.DataSection))
	}
}

func TestParseEOF_WithData(t *testing.T) {
	data := []byte{0xDE, 0xAD, 0xBE, 0xEF}
	code := buildEOF(
		[]TypeSection{{Inputs: 0, Outputs: 0x80, MaxStackHeight: 2}},
		[][]byte{{byte(PUSH1), 0x01, byte(PUSH1), 0x02, byte(ADD), byte(STOP)}},
		nil,
		data,
	)
	container, err := ParseEOF(code)
	if err != nil {
		t.Fatalf("ParseEOF failed: %v", err)
	}
	if !bytes.Equal(container.DataSection, data) {
		t.Errorf("data = %x, want %x", container.DataSection, data)
	}
}

func TestParseEOF_MultipleCodeSections(t *testing.T) {
	types := []TypeSection{
		{Inputs: 0, Outputs: 0x80, MaxStackHeight: 0},
		{Inputs: 2, Outputs: 1, MaxStackHeight: 3},
		{Inputs: 1, Outputs: 0, MaxStackHeight: 1},
	}
	codes := [][]byte{
		{byte(STOP)},
		{byte(ADD), byte(STOP)},
		{byte(POP), byte(STOP)},
	}
	raw := buildEOF(types, codes, nil, nil)
	container, err := ParseEOF(raw)
	if err != nil {
		t.Fatalf("ParseEOF failed: %v", err)
	}
	if len(container.TypeSections) != 3 {
		t.Fatalf("type sections = %d, want 3", len(container.TypeSections))
	}
	if len(container.CodeSections) != 3 {
		t.Fatalf("code sections = %d, want 3", len(container.CodeSections))
	}
	// Verify second type section.
	ts := container.TypeSections[1]
	if ts.Inputs != 2 || ts.Outputs != 1 || ts.MaxStackHeight != 3 {
		t.Errorf("type[1] = {%d,%d,%d}, want {2,1,3}", ts.Inputs, ts.Outputs, ts.MaxStackHeight)
	}
}

func TestParseEOF_WithContainerSections(t *testing.T) {
	innerContainer := minimalEOF()
	types := []TypeSection{{Inputs: 0, Outputs: 0x80, MaxStackHeight: 0}}
	codes := [][]byte{{byte(STOP)}}
	containers := [][]byte{innerContainer}
	raw := buildEOF(types, codes, containers, []byte{0x42})

	container, err := ParseEOF(raw)
	if err != nil {
		t.Fatalf("ParseEOF failed: %v", err)
	}
	if len(container.ContainerSections) != 1 {
		t.Fatalf("container sections = %d, want 1", len(container.ContainerSections))
	}
	if !bytes.Equal(container.ContainerSections[0], innerContainer) {
		t.Error("nested container mismatch")
	}
}

func TestSerializeEOF_Roundtrip(t *testing.T) {
	types := []TypeSection{
		{Inputs: 0, Outputs: 0x80, MaxStackHeight: 5},
		{Inputs: 3, Outputs: 2, MaxStackHeight: 10},
	}
	codes := [][]byte{
		{byte(PUSH1), 0x01, byte(STOP)},
		{byte(ADD), byte(MUL), byte(STOP)},
	}
	data := []byte{0x01, 0x02, 0x03}
	original := buildEOF(types, codes, nil, data)

	container, err := ParseEOF(original)
	if err != nil {
		t.Fatalf("ParseEOF failed: %v", err)
	}
	reserialized := SerializeEOF(container)
	if !bytes.Equal(original, reserialized) {
		t.Errorf("roundtrip mismatch:\n  original:     %x\n  reserialized: %x", original, reserialized)
	}
}

func TestSerializeEOF_RoundtripWithContainers(t *testing.T) {
	inner := minimalEOF()
	types := []TypeSection{{Inputs: 0, Outputs: 0x80, MaxStackHeight: 0}}
	codes := [][]byte{{byte(STOP)}}
	containers := [][]byte{inner, inner}
	data := []byte{0xFF}
	original := buildEOF(types, codes, containers, data)

	container, err := ParseEOF(original)
	if err != nil {
		t.Fatalf("ParseEOF failed: %v", err)
	}
	reserialized := SerializeEOF(container)
	if !bytes.Equal(original, reserialized) {
		t.Errorf("roundtrip mismatch:\n  original:     %x\n  reserialized: %x", original, reserialized)
	}
}

func TestParseEOF_InvalidMagic(t *testing.T) {
	code := []byte{0xEF, 0x01, 0x01}
	_, err := ParseEOF(code)
	if err != ErrEOFInvalidMagic {
		t.Errorf("expected ErrEOFInvalidMagic, got %v", err)
	}
}

func TestParseEOF_InvalidVersion(t *testing.T) {
	code := []byte{0xEF, 0x00, 0x00}
	_, err := ParseEOF(code)
	if err != ErrEOFInvalidVersion {
		t.Errorf("expected ErrEOFInvalidVersion, got %v", err)
	}

	// Version 2 (not supported).
	code2 := []byte{0xEF, 0x00, 0x02}
	_, err = ParseEOF(code2)
	if err != ErrEOFInvalidVersion {
		t.Errorf("expected ErrEOFInvalidVersion for version 2, got %v", err)
	}
}

func TestParseEOF_MissingSections(t *testing.T) {
	// Only magic+version + immediate terminator: no type section.
	code := []byte{0xEF, 0x00, 0x01, 0x00}
	_, err := ParseEOF(code)
	if err != ErrEOFMissingTypeSection {
		t.Errorf("expected ErrEOFMissingTypeSection, got %v", err)
	}
}

func TestParseEOF_MissingCodeSection(t *testing.T) {
	// Type section present but no code section, then terminator.
	code := []byte{
		0xEF, 0x00, 0x01, // magic + version
		0x01, 0x00, 0x04, // type section: kind=1, size=4
		0xFF, 0x00, 0x00, // data section: kind=0xFF, size=0
		0x00, // terminator
	}
	_, err := ParseEOF(code)
	if err != ErrEOFMissingCodeSection {
		t.Errorf("expected ErrEOFMissingCodeSection, got %v", err)
	}
}

func TestParseEOF_TypeSizeMismatch(t *testing.T) {
	// type_size=8 (2 entries) but only 1 code section.
	var buf []byte
	buf = append(buf, 0xEF, 0x00, 0x01)         // magic + version
	buf = append(buf, 0x01, 0x00, 0x08)          // type: kind=1, size=8 (2 entries)
	buf = append(buf, 0x02, 0x00, 0x01, 0x00, 0x01) // code: kind=2, num=1, size=1
	buf = append(buf, 0xFF, 0x00, 0x00)          // data: kind=0xFF, size=0
	buf = append(buf, 0x00)                       // terminator
	// Body: 2 type entries + 1 code byte.
	buf = append(buf, 0x00, 0x80, 0x00, 0x00)    // type entry 0
	buf = append(buf, 0x01, 0x01, 0x00, 0x02)    // type entry 1
	buf = append(buf, byte(STOP))                  // code section 0

	_, err := ParseEOF(buf)
	if err != ErrEOFTypeSizeMismatch {
		t.Errorf("expected ErrEOFTypeSizeMismatch, got %v", err)
	}
}

func TestParseEOF_TrailingBytes(t *testing.T) {
	valid := minimalEOF()
	withTrail := append(valid, 0xFF)
	_, err := ParseEOF(withTrail)
	if err != ErrEOFTrailingBytes {
		t.Errorf("expected ErrEOFTrailingBytes, got %v", err)
	}
}

func TestParseEOF_TooShort(t *testing.T) {
	_, err := ParseEOF([]byte{0xEF, 0x00})
	if err != ErrEOFTooShort {
		t.Errorf("expected ErrEOFTooShort, got %v", err)
	}

	_, err = ParseEOF(nil)
	if err != ErrEOFTooShort {
		t.Errorf("expected ErrEOFTooShort for nil, got %v", err)
	}
}

func TestParseEOF_ZeroCodeSize(t *testing.T) {
	// Build header manually with a code section of size 0.
	var buf []byte
	buf = append(buf, 0xEF, 0x00, 0x01)
	buf = append(buf, 0x01, 0x00, 0x04)             // type: size=4
	buf = append(buf, 0x02, 0x00, 0x01, 0x00, 0x00) // code: num=1, size=0
	buf = append(buf, 0xFF, 0x00, 0x00)              // data: size=0
	buf = append(buf, 0x00)                           // terminator
	buf = append(buf, 0x00, 0x80, 0x00, 0x00)        // type entry

	_, err := ParseEOF(buf)
	if err != ErrEOFZeroCodeSize {
		t.Errorf("expected ErrEOFZeroCodeSize, got %v", err)
	}
}

func TestParseEOF_DuplicateTypeSection(t *testing.T) {
	var buf []byte
	buf = append(buf, 0xEF, 0x00, 0x01)
	buf = append(buf, 0x01, 0x00, 0x04) // type section 1
	buf = append(buf, 0x01, 0x00, 0x04) // type section 2 (duplicate)
	buf = append(buf, 0x00)

	_, err := ParseEOF(buf)
	if err != ErrEOFDuplicateSection {
		t.Errorf("expected ErrEOFDuplicateSection, got %v", err)
	}
}

func TestParseEOF_InvalidSectionOrder_DataBeforeCode(t *testing.T) {
	var buf []byte
	buf = append(buf, 0xEF, 0x00, 0x01)
	buf = append(buf, 0x01, 0x00, 0x04) // type
	buf = append(buf, 0xFF, 0x00, 0x00) // data before code
	buf = append(buf, 0x00)

	_, err := ParseEOF(buf)
	if err != ErrEOFMissingCodeSection {
		t.Errorf("expected ErrEOFMissingCodeSection (data before code), got %v", err)
	}
}

func TestValidateEOF_InvalidFirstCodeSection(t *testing.T) {
	container := &EOFContainer{
		Version: EOFVersion,
		TypeSections: []TypeSection{
			{Inputs: 1, Outputs: 0, MaxStackHeight: 0}, // wrong: first must be 0 inputs, 0x80 outputs
		},
		CodeSections: [][]byte{{byte(STOP)}},
	}
	err := ValidateEOF(container)
	if err != ErrEOFInvalidFirstCode {
		t.Errorf("expected ErrEOFInvalidFirstCode, got %v", err)
	}
}

func TestValidateEOF_TypeCountMismatch(t *testing.T) {
	container := &EOFContainer{
		Version: EOFVersion,
		TypeSections: []TypeSection{
			{Inputs: 0, Outputs: 0x80, MaxStackHeight: 0},
		},
		CodeSections: [][]byte{{byte(STOP)}, {byte(STOP)}},
	}
	err := ValidateEOF(container)
	if err != ErrEOFTypeSizeMismatch {
		t.Errorf("expected ErrEOFTypeSizeMismatch, got %v", err)
	}
}

func TestValidateEOF_MaxStackHeightOverflow(t *testing.T) {
	container := &EOFContainer{
		Version: EOFVersion,
		TypeSections: []TypeSection{
			{Inputs: 0, Outputs: 0x80, MaxStackHeight: 0x0400}, // exceeds 0x03FF
		},
		CodeSections: [][]byte{{byte(STOP)}},
	}
	err := ValidateEOF(container)
	if err == nil {
		t.Error("expected error for max_stack_height > 0x03FF, got nil")
	}
}

func TestValidateEOF_EmptyCodeSection(t *testing.T) {
	container := &EOFContainer{
		Version: EOFVersion,
		TypeSections: []TypeSection{
			{Inputs: 0, Outputs: 0x80, MaxStackHeight: 0},
		},
		CodeSections: [][]byte{{}},
	}
	err := ValidateEOF(container)
	if err == nil {
		t.Error("expected error for empty code section, got nil")
	}
}

func TestValidateEOF_Valid(t *testing.T) {
	container := &EOFContainer{
		Version: EOFVersion,
		TypeSections: []TypeSection{
			{Inputs: 0, Outputs: 0x80, MaxStackHeight: 0},
			{Inputs: 2, Outputs: 1, MaxStackHeight: 3},
		},
		CodeSections: [][]byte{
			{byte(STOP)},
			{byte(ADD), byte(STOP)},
		},
		DataSection: []byte{0xAA, 0xBB},
	}
	if err := ValidateEOF(container); err != nil {
		t.Errorf("ValidateEOF failed: %v", err)
	}
}

func TestParseEOF_EmptyData(t *testing.T) {
	code := buildEOF(
		[]TypeSection{{Inputs: 0, Outputs: 0x80, MaxStackHeight: 0}},
		[][]byte{{byte(STOP)}},
		nil,
		[]byte{},
	)
	container, err := ParseEOF(code)
	if err != nil {
		t.Fatalf("ParseEOF failed: %v", err)
	}
	if len(container.DataSection) != 0 {
		t.Errorf("data section len = %d, want 0", len(container.DataSection))
	}
}

func TestParseEOF_MaxSections(t *testing.T) {
	// Build a container with 1024 code sections (max allowed).
	numSections := 1024
	types := make([]TypeSection, numSections)
	codes := make([][]byte, numSections)
	types[0] = TypeSection{Inputs: 0, Outputs: 0x80, MaxStackHeight: 0}
	codes[0] = []byte{byte(STOP)}
	for i := 1; i < numSections; i++ {
		types[i] = TypeSection{Inputs: 0, Outputs: 0, MaxStackHeight: 0}
		codes[i] = []byte{byte(STOP)}
	}

	raw := buildEOF(types, codes, nil, nil)
	container, err := ParseEOF(raw)
	if err != nil {
		t.Fatalf("ParseEOF with %d sections failed: %v", numSections, err)
	}
	if len(container.CodeSections) != numSections {
		t.Errorf("code sections = %d, want %d", len(container.CodeSections), numSections)
	}
}

func TestParseEOF_BodyTruncated(t *testing.T) {
	// Build a valid container then chop the last byte of the body.
	data := []byte{0x01, 0x02, 0x03, 0x04}
	full := buildEOF(
		[]TypeSection{{Inputs: 0, Outputs: 0x80, MaxStackHeight: 0}},
		[][]byte{{byte(STOP)}},
		nil,
		data,
	)
	truncated := full[:len(full)-1]
	_, err := ParseEOF(truncated)
	if err != ErrEOFBodyTruncated {
		t.Errorf("expected ErrEOFBodyTruncated, got %v", err)
	}
}

func TestParseEOF_TypeSizeNotDivisibleBy4(t *testing.T) {
	// Manually build a header with type_size=5 (not divisible by 4).
	var buf []byte
	buf = append(buf, 0xEF, 0x00, 0x01)
	buf = append(buf, 0x01, 0x00, 0x05)             // type: size=5 (invalid)
	buf = append(buf, 0x02, 0x00, 0x01, 0x00, 0x01) // code: num=1, size=1
	buf = append(buf, 0xFF, 0x00, 0x00)              // data: size=0
	buf = append(buf, 0x00)                           // terminator
	// Body (won't get parsed because of header validation).
	buf = append(buf, 0x00, 0x80, 0x00, 0x00, 0x00) // 5 bytes type section
	buf = append(buf, byte(STOP))

	_, err := ParseEOF(buf)
	if err != ErrEOFTypeSizeNotDivisible {
		t.Errorf("expected ErrEOFTypeSizeNotDivisible, got %v", err)
	}
}

func TestEOFConstants(t *testing.T) {
	if EOFMagic0 != 0xEF {
		t.Errorf("EOFMagic0 = 0x%02x, want 0xEF", EOFMagic0)
	}
	if EOFMagic1 != 0x00 {
		t.Errorf("EOFMagic1 = 0x%02x, want 0x00", EOFMagic1)
	}
	if EOFVersion != 0x01 {
		t.Errorf("EOFVersion = 0x%02x, want 0x01", EOFVersion)
	}
	if EOFSectionType != 0x01 {
		t.Errorf("EOFSectionType = 0x%02x, want 0x01", EOFSectionType)
	}
	if EOFSectionCode != 0x02 {
		t.Errorf("EOFSectionCode = 0x%02x, want 0x02", EOFSectionCode)
	}
	if EOFSectionContainer != 0x03 {
		t.Errorf("EOFSectionContainer = 0x%02x, want 0x03", EOFSectionContainer)
	}
	if EOFSectionData != 0xFF {
		t.Errorf("EOFSectionData = 0x%02x, want 0xFF", EOFSectionData)
	}
}

func TestSerializeEOF_ManualVerification(t *testing.T) {
	// Build and verify byte-by-byte.
	container := &EOFContainer{
		Version: EOFVersion,
		TypeSections: []TypeSection{
			{Inputs: 0, Outputs: 0x80, MaxStackHeight: 0},
		},
		CodeSections: [][]byte{{byte(STOP)}},
	}
	raw := SerializeEOF(container)

	// Verify header bytes manually.
	expected := []byte{
		0xEF, 0x00, 0x01,       // magic + version
		0x01, 0x00, 0x04,       // type section: kind=1, size=4
		0x02, 0x00, 0x01,       // code section: kind=2, num=1
		0x00, 0x01,             // code section 0 size=1
		0xFF, 0x00, 0x00,       // data section: kind=0xFF, size=0
		0x00,                    // terminator
		0x00, 0x80, 0x00, 0x00, // type[0]: inputs=0, outputs=0x80, max_stack=0
		0x00,                    // code[0]: STOP
	}
	if !bytes.Equal(raw, expected) {
		t.Errorf("SerializeEOF:\n  got:  %x\n  want: %x", raw, expected)
	}
}

func TestParseEOF_MissingTerminator(t *testing.T) {
	// Build a buffer that has type and code headers but no terminator.
	var buf []byte
	buf = append(buf, 0xEF, 0x00, 0x01)
	buf = append(buf, 0x01, 0x00, 0x04)             // type
	buf = append(buf, 0x02, 0x00, 0x01, 0x00, 0x01) // code
	buf = append(buf, 0xFF, 0x00, 0x00)              // data
	// No terminator -- just end the buffer.
	_, err := ParseEOF(buf)
	if err != ErrEOFMissingTerminator {
		t.Errorf("expected ErrEOFMissingTerminator, got %v", err)
	}
}

func TestParseEOF_ZeroTypeSize(t *testing.T) {
	var buf []byte
	buf = append(buf, 0xEF, 0x00, 0x01)
	buf = append(buf, 0x01, 0x00, 0x00) // type: size=0 (invalid)
	buf = append(buf, 0x00)

	_, err := ParseEOF(buf)
	if err != ErrEOFZeroTypeSize {
		t.Errorf("expected ErrEOFZeroTypeSize, got %v", err)
	}
}

// Test that binary.BigEndian is used correctly by verifying endianness of a
// multi-byte field through a parse/serialize roundtrip.
func TestSerializeEOF_Endianness(t *testing.T) {
	container := &EOFContainer{
		Version: EOFVersion,
		TypeSections: []TypeSection{
			{Inputs: 0, Outputs: 0x80, MaxStackHeight: 0x0102},
		},
		CodeSections: [][]byte{{byte(STOP)}},
	}
	raw := SerializeEOF(container)

	// Parse back and verify the MaxStackHeight was preserved through roundtrip.
	parsed, err := ParseEOF(raw)
	if err != nil {
		t.Fatalf("ParseEOF failed: %v", err)
	}
	msh := parsed.TypeSections[0].MaxStackHeight
	if msh != 0x0102 {
		t.Errorf("max_stack_height = 0x%04x, want 0x0102", msh)
	}

	// Also verify raw bytes directly. The header for a single-code-section
	// minimal container is exactly:
	//   EF 00 01                  (3 bytes: magic + version)
	//   01 00 04                  (3 bytes: type kind + size=4)
	//   02 00 01 00 01            (5 bytes: code kind + num=1 + size=1)
	//   FF 00 00                  (3 bytes: data kind + size=0)
	//   00                        (1 byte: terminator)
	// Total header = 15 bytes. Body starts at offset 15.
	// Type entry at body[2:4] = max_stack_height.
	if len(raw) < 19 {
		t.Fatalf("raw too short: %d", len(raw))
	}
	hi := raw[17]
	lo := raw[18]
	if hi != 0x01 || lo != 0x02 {
		t.Errorf("raw max_stack_height bytes = [0x%02x, 0x%02x], want [0x01, 0x02]", hi, lo)
	}
}
