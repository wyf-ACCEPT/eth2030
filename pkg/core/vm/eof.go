package vm

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// EOF magic bytes and version per EIP-3540.
const (
	EOFMagic0  byte = 0xEF
	EOFMagic1  byte = 0x00
	EOFVersion byte = 0x01
)

// EOF section kind markers per EIP-3540.
const (
	EOFSectionType      byte = 0x01
	EOFSectionCode      byte = 0x02
	EOFSectionContainer byte = 0x03
	EOFSectionData      byte = 0xFF
	EOFHeaderTerminator byte = 0x00
)

// Limits per the EIP-3540 spec.
const (
	eofMaxCodeSections      = 0x0400 // 1024
	eofMaxContainerSections = 0x0100 // 256
	eofTypeSectionEntrySize = 4      // 4 bytes per type entry (inputs, outputs, max_stack_height)

	// Non-returning function sentinel value.
	eofNonReturning byte = 0x80
)

var (
	ErrEOFTooShort             = errors.New("eof: container too short")
	ErrEOFInvalidMagic         = errors.New("eof: invalid magic bytes")
	ErrEOFInvalidVersion       = errors.New("eof: invalid version")
	ErrEOFMissingTypeSection   = errors.New("eof: missing type section")
	ErrEOFMissingCodeSection   = errors.New("eof: missing code section")
	ErrEOFMissingTerminator    = errors.New("eof: missing header terminator")
	ErrEOFTypeSizeMismatch     = errors.New("eof: type section size does not match code section count")
	ErrEOFZeroTypeSize         = errors.New("eof: type section size is zero")
	ErrEOFZeroCodeSize         = errors.New("eof: code section size is zero")
	ErrEOFInvalidSectionOrder  = errors.New("eof: invalid section order")
	ErrEOFDuplicateSection     = errors.New("eof: duplicate section")
	ErrEOFTrailingBytes        = errors.New("eof: trailing bytes after declared sections")
	ErrEOFInvalidFirstCode     = errors.New("eof: first code section must have 0 inputs and 0x80 outputs")
	ErrEOFZeroCodeSections     = errors.New("eof: zero code sections")
	ErrEOFContainerTooLarge    = errors.New("eof: container exceeds max initcode size")
	ErrEOFBodyTruncated        = errors.New("eof: body truncated")
	ErrEOFTypeSizeNotDivisible = errors.New("eof: type_size not divisible by 4")
)

// TypeSection holds the metadata for a single code section.
type TypeSection struct {
	Inputs         uint8
	Outputs        uint8
	MaxStackHeight uint16
}

// EOFContainer is a parsed EIP-3540 EOF v1 container.
type EOFContainer struct {
	Version           byte
	TypeSections      []TypeSection
	CodeSections      [][]byte
	ContainerSections [][]byte
	DataSection       []byte
}

// IsEOF returns true if code starts with the EOF magic bytes 0xEF00.
func IsEOF(code []byte) bool {
	return len(code) >= 2 && code[0] == EOFMagic0 && code[1] == EOFMagic1
}

// ParseEOF parses an EOF v1 container from raw bytes.
func ParseEOF(code []byte) (*EOFContainer, error) {
	if len(code) < 3 {
		return nil, ErrEOFTooShort
	}
	if code[0] != EOFMagic0 || code[1] != EOFMagic1 {
		return nil, ErrEOFInvalidMagic
	}
	if code[2] != EOFVersion {
		return nil, ErrEOFInvalidVersion
	}

	pos := 3

	// Parse header sections. Expected order: type, code, [container], data, terminator.
	var (
		typeSize       uint16
		codeSizes      []uint16
		containerSizes []uint32
		dataSize       uint16
		hasType        bool
		hasCode        bool
		hasContainer   bool
		hasData        bool
	)

	for {
		if pos >= len(code) {
			return nil, ErrEOFMissingTerminator
		}
		kind := code[pos]
		pos++

		if kind == EOFHeaderTerminator {
			break
		}

		switch kind {
		case EOFSectionType:
			if hasType {
				return nil, ErrEOFDuplicateSection
			}
			if hasCode || hasContainer || hasData {
				return nil, ErrEOFInvalidSectionOrder
			}
			if pos+2 > len(code) {
				return nil, ErrEOFTooShort
			}
			typeSize = binary.BigEndian.Uint16(code[pos : pos+2])
			pos += 2
			if typeSize == 0 {
				return nil, ErrEOFZeroTypeSize
			}
			hasType = true

		case EOFSectionCode:
			if hasCode {
				return nil, ErrEOFDuplicateSection
			}
			if !hasType {
				return nil, ErrEOFMissingTypeSection
			}
			if hasContainer || hasData {
				return nil, ErrEOFInvalidSectionOrder
			}
			if pos+2 > len(code) {
				return nil, ErrEOFTooShort
			}
			numCode := binary.BigEndian.Uint16(code[pos : pos+2])
			pos += 2
			if numCode == 0 {
				return nil, ErrEOFZeroCodeSections
			}
			codeSizes = make([]uint16, numCode)
			for i := uint16(0); i < numCode; i++ {
				if pos+2 > len(code) {
					return nil, ErrEOFTooShort
				}
				codeSizes[i] = binary.BigEndian.Uint16(code[pos : pos+2])
				pos += 2
				if codeSizes[i] == 0 {
					return nil, ErrEOFZeroCodeSize
				}
			}
			hasCode = true

		case EOFSectionContainer:
			if hasContainer {
				return nil, ErrEOFDuplicateSection
			}
			if !hasCode {
				return nil, ErrEOFMissingCodeSection
			}
			if hasData {
				return nil, ErrEOFInvalidSectionOrder
			}
			if pos+2 > len(code) {
				return nil, ErrEOFTooShort
			}
			numContainer := binary.BigEndian.Uint16(code[pos : pos+2])
			pos += 2
			containerSizes = make([]uint32, numContainer)
			for i := uint16(0); i < numContainer; i++ {
				if pos+4 > len(code) {
					return nil, ErrEOFTooShort
				}
				containerSizes[i] = binary.BigEndian.Uint32(code[pos : pos+4])
				pos += 4
			}
			hasContainer = true

		case EOFSectionData:
			if hasData {
				return nil, ErrEOFDuplicateSection
			}
			if !hasCode {
				return nil, ErrEOFMissingCodeSection
			}
			if pos+2 > len(code) {
				return nil, ErrEOFTooShort
			}
			dataSize = binary.BigEndian.Uint16(code[pos : pos+2])
			pos += 2
			hasData = true

		default:
			return nil, fmt.Errorf("eof: unknown section kind 0x%02x", kind)
		}
	}

	if !hasType {
		return nil, ErrEOFMissingTypeSection
	}
	if !hasCode {
		return nil, ErrEOFMissingCodeSection
	}

	// Validate type_size is divisible by 4 and matches code section count.
	if typeSize%eofTypeSectionEntrySize != 0 {
		return nil, ErrEOFTypeSizeNotDivisible
	}
	numTypes := int(typeSize / eofTypeSectionEntrySize)
	if numTypes != len(codeSizes) {
		return nil, ErrEOFTypeSizeMismatch
	}

	// Parse body: type entries
	container := &EOFContainer{Version: EOFVersion}

	container.TypeSections = make([]TypeSection, numTypes)
	for i := 0; i < numTypes; i++ {
		if pos+4 > len(code) {
			return nil, ErrEOFBodyTruncated
		}
		container.TypeSections[i] = TypeSection{
			Inputs:         code[pos],
			Outputs:        code[pos+1],
			MaxStackHeight: binary.BigEndian.Uint16(code[pos+2 : pos+4]),
		}
		pos += 4
	}

	// Parse body: code sections
	container.CodeSections = make([][]byte, len(codeSizes))
	for i, size := range codeSizes {
		end := pos + int(size)
		if end > len(code) {
			return nil, ErrEOFBodyTruncated
		}
		container.CodeSections[i] = make([]byte, size)
		copy(container.CodeSections[i], code[pos:end])
		pos = end
	}

	// Parse body: container sections
	if hasContainer {
		container.ContainerSections = make([][]byte, len(containerSizes))
		for i, size := range containerSizes {
			end := pos + int(size)
			if end > len(code) {
				return nil, ErrEOFBodyTruncated
			}
			container.ContainerSections[i] = make([]byte, size)
			copy(container.ContainerSections[i], code[pos:end])
			pos = end
		}
	}

	// Parse body: data section
	if hasData {
		end := pos + int(dataSize)
		if end > len(code) {
			// Per spec: data body length may be shorter for not-yet-deployed containers.
			// For a deployed (fully validated) container, treat truncation as error.
			return nil, ErrEOFBodyTruncated
		}
		container.DataSection = make([]byte, dataSize)
		copy(container.DataSection, code[pos:end])
		pos = end
	}

	// No trailing bytes allowed.
	if pos != len(code) {
		return nil, ErrEOFTrailingBytes
	}

	return container, nil
}

// ValidateEOF performs structural validation on a parsed EOF container.
func ValidateEOF(container *EOFContainer) error {
	if container.Version != EOFVersion {
		return ErrEOFInvalidVersion
	}
	if len(container.TypeSections) == 0 {
		return ErrEOFMissingTypeSection
	}
	if len(container.CodeSections) == 0 {
		return ErrEOFMissingCodeSection
	}
	if len(container.TypeSections) != len(container.CodeSections) {
		return ErrEOFTypeSizeMismatch
	}

	// First code section must have 0 inputs and 0x80 (non-returning) outputs.
	first := container.TypeSections[0]
	if first.Inputs != 0 || first.Outputs != eofNonReturning {
		return ErrEOFInvalidFirstCode
	}

	// Validate individual code sections are non-empty.
	for i, cs := range container.CodeSections {
		if len(cs) == 0 {
			return fmt.Errorf("eof: code section %d is empty", i)
		}
	}

	// Validate max stack height fits in 10 bits (0x0000-0x03FF).
	for i, ts := range container.TypeSections {
		if ts.MaxStackHeight > 0x03FF {
			return fmt.Errorf("eof: type section %d max_stack_height %d exceeds 0x03FF", i, ts.MaxStackHeight)
		}
	}

	return nil
}

// SerializeEOF serializes an EOFContainer back to its binary representation.
func SerializeEOF(container *EOFContainer) []byte {
	numCode := len(container.CodeSections)
	numContainer := len(container.ContainerSections)

	// Estimate size: header + body.
	// Header: magic(2) + version(1) + type_kind(1) + type_size(2)
	//       + code_kind(1) + num_code(2) + code_sizes(2*numCode)
	//       + [container_kind(1) + num_container(2) + container_sizes(4*numContainer)]
	//       + data_kind(1) + data_size(2) + terminator(1)
	headerSize := 2 + 1 + 1 + 2 + 1 + 2 + 2*numCode + 1 + 2 + 1
	if numContainer > 0 {
		headerSize += 1 + 2 + 4*numContainer
	}

	// Body: types(4*numCode) + code_bodies + container_bodies + data
	bodySize := 4 * numCode
	for _, cs := range container.CodeSections {
		bodySize += len(cs)
	}
	for _, cs := range container.ContainerSections {
		bodySize += len(cs)
	}
	bodySize += len(container.DataSection)

	buf := make([]byte, 0, headerSize+bodySize)

	// Magic + version
	buf = append(buf, EOFMagic0, EOFMagic1, container.Version)

	// Type section header
	typeSize := uint16(numCode * eofTypeSectionEntrySize)
	buf = append(buf, EOFSectionType)
	buf = binary.BigEndian.AppendUint16(buf, typeSize)

	// Code section header
	buf = append(buf, EOFSectionCode)
	buf = binary.BigEndian.AppendUint16(buf, uint16(numCode))
	for _, cs := range container.CodeSections {
		buf = binary.BigEndian.AppendUint16(buf, uint16(len(cs)))
	}

	// Container section header (optional)
	if numContainer > 0 {
		buf = append(buf, EOFSectionContainer)
		buf = binary.BigEndian.AppendUint16(buf, uint16(numContainer))
		for _, cs := range container.ContainerSections {
			buf = binary.BigEndian.AppendUint32(buf, uint32(len(cs)))
		}
	}

	// Data section header
	buf = append(buf, EOFSectionData)
	buf = binary.BigEndian.AppendUint16(buf, uint16(len(container.DataSection)))

	// Header terminator
	buf = append(buf, EOFHeaderTerminator)

	// Body: type entries
	for _, ts := range container.TypeSections {
		buf = append(buf, ts.Inputs, ts.Outputs)
		buf = binary.BigEndian.AppendUint16(buf, ts.MaxStackHeight)
	}

	// Body: code sections
	for _, cs := range container.CodeSections {
		buf = append(buf, cs...)
	}

	// Body: container sections
	for _, cs := range container.ContainerSections {
		buf = append(buf, cs...)
	}

	// Body: data section
	buf = append(buf, container.DataSection...)

	return buf
}
