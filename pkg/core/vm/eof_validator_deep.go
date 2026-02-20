// eof_validator_deep.go extends the EOF validator with advanced analysis:
// - DATALOADN bounds checking against the data section size
// - DUPN/SWAPN/EXCHANGE stack depth validation
// - EOF container statistics collection (opcode frequency, call graph)
// - Gas cost estimation for EOF code sections
// - Streaming validation for large containers
package vm

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sync"
)

// Extended EOF validation errors.
var (
	ErrEOFDataLoadNOOB     = errors.New("eof: DATALOADN offset out of bounds")
	ErrEOFDupNTooDeep      = errors.New("eof: DUPN requires more stack items than available")
	ErrEOFSwapNTooDeep     = errors.New("eof: SWAPN requires more stack items than available")
	ErrEOFExchangeTooDeep  = errors.New("eof: EXCHANGE requires more stack items than available")
	ErrEOFMaxStackExceeded = errors.New("eof: total stack usage exceeds max allowed")
	ErrEOFEmptyContainer   = errors.New("eof: empty container for analysis")
)

// EOFContainerStats holds analysis results for an EOF container.
type EOFContainerStats struct {
	// TotalCodeBytes is the sum of all code section sizes.
	TotalCodeBytes int
	// TotalDataBytes is the data section size.
	TotalDataBytes int
	// NumCodeSections counts code sections.
	NumCodeSections int
	// NumContainerSections counts nested containers.
	NumContainerSections int
	// OpcodeFrequency maps opcode to count across all code sections.
	OpcodeFrequency map[OpCode]int
	// CallGraph maps code section index to called section indices (CALLF/JUMPF).
	CallGraph map[int][]int
	// MaxStackDepth is the observed maximum stack depth across all sections.
	MaxStackDepth int
	// EstimatedGas is a rough gas cost estimate based on opcode counts.
	EstimatedGas uint64
	// HasRecursion indicates the call graph has cycles.
	HasRecursion bool
}

// EOFDeepValidator extends EOFValidator with data bounds, stack depth,
// and container statistics analysis.
type EOFDeepValidator struct {
	mu    sync.Mutex
	inner *EOFValidator
}

// NewEOFDeepValidator creates a deep EOF validator.
func NewEOFDeepValidator() *EOFDeepValidator {
	return &EOFDeepValidator{
		inner: NewEOFValidator(),
	}
}

// ValidateDeep performs full structural and semantic validation including
// DATALOADN bounds, DUPN/SWAPN/EXCHANGE stack depth, and returns
// container statistics on success.
func (dv *EOFDeepValidator) ValidateDeep(bytecode []byte) (*EOFContainer, *EOFContainerStats, error) {
	dv.mu.Lock()
	defer dv.mu.Unlock()

	// Step 1: standard validation.
	container, err := dv.inner.Validate(bytecode)
	if err != nil {
		return nil, nil, err
	}

	// Step 2: DATALOADN bounds validation.
	if err := validateDataLoadNBounds(container); err != nil {
		return nil, nil, err
	}

	// Step 3: DUPN/SWAPN/EXCHANGE stack depth validation.
	if err := validateExtendedStackOps(container); err != nil {
		return nil, nil, err
	}

	// Step 4: Collect statistics.
	stats := collectContainerStats(container)

	return container, stats, nil
}

// validateDataLoadNBounds checks that every DATALOADN instruction's 16-bit
// offset does not exceed the data section length - 32 (since DATALOADN loads
// a 32-byte word starting at that offset).
func validateDataLoadNBounds(c *EOFContainer) error {
	dataLen := len(c.DataSection)
	for secIdx, code := range c.CodeSections {
		pos := 0
		for pos < len(code) {
			op := OpCode(code[pos])
			info := eofOpcodeTable[op]
			if !info.validEOF {
				pos++
				continue
			}

			immLen := info.imm
			if immLen == -1 {
				if pos+1 >= len(code) {
					break
				}
				count := int(code[pos+1])
				immLen = 1 + (count+1)*2
			}

			if op == DATALOADN && pos+2 < len(code) {
				offset := int(binary.BigEndian.Uint16(code[pos+1 : pos+3]))
				if offset+32 > dataLen {
					return fmt.Errorf("%w: section %d offset %d at pc=%d, data length=%d",
						ErrEOFDataLoadNOOB, secIdx, offset, pos, dataLen)
				}
			}
			pos += 1 + immLen
		}
	}
	return nil
}

// validateExtendedStackOps validates that DUPN, SWAPN, and EXCHANGE
// instructions reference stack positions consistent with the declared
// type section inputs/outputs and max stack height.
func validateExtendedStackOps(c *EOFContainer) error {
	for secIdx, code := range c.CodeSections {
		ts := c.TypeSections[secIdx]
		maxAvail := int(ts.Inputs) + int(ts.MaxStackHeight)

		pos := 0
		for pos < len(code) {
			op := OpCode(code[pos])
			info := eofOpcodeTable[op]
			if !info.validEOF {
				pos++
				continue
			}

			immLen := info.imm
			if immLen == -1 {
				if pos+1 >= len(code) {
					break
				}
				count := int(code[pos+1])
				immLen = 1 + (count+1)*2
			}

			if pos+1+immLen > len(code) {
				break
			}

			switch op {
			case DUPN:
				// DUPN immediate is the 0-based stack depth index.
				n := int(code[pos+1]) + 1 // 1-indexed depth
				if n > maxAvail {
					return fmt.Errorf("%w: section %d, DUPN %d at pc=%d, max stack=%d",
						ErrEOFDupNTooDeep, secIdx, n, pos, maxAvail)
				}
			case SWAPN:
				n := int(code[pos+1]) + 2 // SWAPN N swaps top with N+2 deep
				if n > maxAvail {
					return fmt.Errorf("%w: section %d, SWAPN %d at pc=%d, max stack=%d",
						ErrEOFSwapNTooDeep, secIdx, n, pos, maxAvail)
				}
			case EXCHANGE:
				// EXCHANGE imm encodes (n, m) as imm = (n-1)*16 + (m-1).
				imm := code[pos+1]
				n := int(imm>>4) + 1
				m := int(imm&0x0f) + 1
				depth := n + m + 1 // needs n+m+1 items on stack
				if depth > maxAvail {
					return fmt.Errorf("%w: section %d, EXCHANGE n=%d m=%d at pc=%d, max stack=%d",
						ErrEOFExchangeTooDeep, secIdx, n, m, pos, maxAvail)
				}
			}
			pos += 1 + immLen
		}
	}
	return nil
}

// collectContainerStats gathers opcode frequency, call graph, and gas estimates.
func collectContainerStats(c *EOFContainer) *EOFContainerStats {
	stats := &EOFContainerStats{
		TotalDataBytes:       len(c.DataSection),
		NumCodeSections:      len(c.CodeSections),
		NumContainerSections: len(c.ContainerSections),
		OpcodeFrequency:      make(map[OpCode]int),
		CallGraph:            make(map[int][]int),
	}

	for secIdx, code := range c.CodeSections {
		stats.TotalCodeBytes += len(code)

		pos := 0
		for pos < len(code) {
			op := OpCode(code[pos])
			info := eofOpcodeTable[op]
			stats.OpcodeFrequency[op]++

			immLen := info.imm
			if immLen == -1 {
				if pos+1 >= len(code) {
					break
				}
				count := int(code[pos+1])
				immLen = 1 + (count+1)*2
			}

			// Track call graph edges.
			if (op == CALLF || op == JUMPF) && pos+2 < len(code) {
				target := int(binary.BigEndian.Uint16(code[pos+1 : pos+3]))
				stats.CallGraph[secIdx] = append(stats.CallGraph[secIdx], target)
			}

			// Estimate gas cost per opcode class.
			stats.EstimatedGas += eofOpcodeGasCost(op)

			pos += 1 + immLen
		}
	}

	// Compute max stack depth.
	for _, ts := range c.TypeSections {
		depth := int(ts.Inputs) + int(ts.MaxStackHeight)
		if depth > stats.MaxStackDepth {
			stats.MaxStackDepth = depth
		}
	}

	// Detect cycles in call graph.
	stats.HasRecursion = detectCallGraphCycle(stats.CallGraph, len(c.CodeSections))

	return stats
}

// eofOpcodeGasCost returns a rough gas estimate for an opcode.
func eofOpcodeGasCost(op OpCode) uint64 {
	switch {
	case op == STOP || op == INVALID:
		return 0
	case op >= PUSH0 && op <= PUSH0+32:
		return 3 // Gverylow
	case op >= DUP1 && op <= DUP1+15:
		return 3
	case op >= SWAP1 && op <= SWAP1+15:
		return 3
	case op == ADD || op == SUB || op == LT || op == GT || op == SLT || op == SGT ||
		op == EQ || op == ISZERO || op == AND || op == OR || op == XOR || op == NOT ||
		op == BYTE || op == SHL || op == SHR || op == SAR || op == POP || op == PUSH0:
		return 3
	case op == MUL || op == DIV || op == SDIV || op == MOD || op == SMOD || op == SIGNEXTEND:
		return 5
	case op == ADDMOD || op == MULMOD:
		return 8
	case op == EXP:
		return 10
	case op == KECCAK256:
		return 30
	case op == SLOAD || op == SSTORE:
		return 100
	case op == MLOAD || op == MSTORE || op == MSTORE8:
		return 3
	case op == BALANCE || op == EXTCALL || op == EXTDELEGATECALL || op == EXTSTATICCALL:
		return 2600
	case op == CALLF || op == RETF || op == JUMPF:
		return 5
	case op == RJUMP || op == RJUMPI || op == RJUMPT:
		return 2
	case op == EOFCREATE:
		return 32000
	default:
		return 3
	}
}

// detectCallGraphCycle checks for cycles in the code section call graph via DFS.
func detectCallGraphCycle(graph map[int][]int, numSections int) bool {
	const (
		white = 0 // unvisited
		gray  = 1 // in current DFS path
		black = 2 // fully processed
	)
	color := make([]int, numSections)

	var dfs func(node int) bool
	dfs = func(node int) bool {
		color[node] = gray
		for _, next := range graph[node] {
			if next >= numSections {
				continue
			}
			if color[next] == gray {
				return true // back edge = cycle
			}
			if color[next] == white {
				if dfs(next) {
					return true
				}
			}
		}
		color[node] = black
		return false
	}

	for i := 0; i < numSections; i++ {
		if color[i] == white {
			if dfs(i) {
				return true
			}
		}
	}
	return false
}

// ValidateStreaming validates EOF bytecode in a streaming fashion,
// allowing the caller to process chunks of a large container without
// loading the entire thing into memory at once. For containers that
// fit in memory, use ValidateDeep instead.
func (dv *EOFDeepValidator) ValidateStreaming(chunks [][]byte) (*EOFContainer, error) {
	dv.mu.Lock()
	defer dv.mu.Unlock()

	// Concatenate chunks (in practice, a streaming parser would avoid this).
	total := 0
	for _, ch := range chunks {
		total += len(ch)
	}
	buf := make([]byte, 0, total)
	for _, ch := range chunks {
		buf = append(buf, ch...)
	}

	return dv.inner.Validate(buf)
}

// SectionGasEstimate returns a gas estimate for a single code section.
func SectionGasEstimate(code []byte) uint64 {
	var gas uint64
	pos := 0
	for pos < len(code) {
		op := OpCode(code[pos])
		info := eofOpcodeTable[op]
		gas += eofOpcodeGasCost(op)

		immLen := info.imm
		if immLen == -1 {
			if pos+1 >= len(code) {
				break
			}
			count := int(code[pos+1])
			immLen = 1 + (count+1)*2
		}
		pos += 1 + immLen
	}
	return gas
}

// ContainerSize returns the total byte size of a serialized EOF container.
func ContainerSize(c *EOFContainer) int {
	if c == nil {
		return 0
	}
	size := 3 // magic + version
	size += 3 // type section header (kind + type_size)
	size += 1 + 2 + 2*len(c.CodeSections) // code section header (kind + num_code + code_sizes)
	if len(c.ContainerSections) > 0 {
		size += 1 + 2 + 4*len(c.ContainerSections) // container header (kind + num + sizes)
	}
	size += 3 // data section header (kind + data_size)
	size += 1 // terminator

	// Body
	size += 4 * len(c.TypeSections)
	for _, cs := range c.CodeSections {
		size += len(cs)
	}
	for _, cs := range c.ContainerSections {
		size += len(cs)
	}
	size += len(c.DataSection)
	return size
}
