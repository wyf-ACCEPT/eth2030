// Package vm implements the Ethereum Virtual Machine.
// This file adds an eWASM JIT compilation cache (EL roadmap: "more precompiles
// in eWASM", "canonical guest"). Provides WASM bytecode validation, module
// parsing, thread-safe LRU cache, simulated execution, and gas calculation.
package vm

import (
	"encoding/binary"
	"errors"
	"sync"
	"time"

	"github.com/eth2030/eth2030/core/types"
	"golang.org/x/crypto/sha3"
)

// WASM binary format constants.
const (
	WasmMagic   uint32 = 0x6D736100 // \0asm in little-endian
	WasmMinSize        = 8          // magic (4) + version (4)
	WasmMaxSize        = 512 * 1024 // 512 KiB max module
)

// WASM section IDs per the specification.
const (
	WasmSectionCustom   byte = 0
	WasmSectionType     byte = 1
	WasmSectionImport   byte = 2
	WasmSectionFunction byte = 3
	WasmSectionTable    byte = 4
	WasmSectionMemory   byte = 5
	WasmSectionGlobal   byte = 6
	WasmSectionExport   byte = 7
	WasmSectionStart    byte = 8
	WasmSectionElement  byte = 9
	WasmSectionCode     byte = 10
	WasmSectionData     byte = 11
)

// Export kind constants.
const (
	WasmExportFunc   byte = 0
	WasmExportTable  byte = 1
	WasmExportMemory byte = 2
	WasmExportGlobal byte = 3
)

// WASM validation and execution errors.
var (
	ErrWasmTooShort       = errors.New("ewasm: bytecode too short for WASM header")
	ErrWasmInvalidMagic   = errors.New("ewasm: invalid WASM magic bytes")
	ErrWasmInvalidVersion = errors.New("ewasm: unsupported WASM version")
	ErrWasmTooLarge       = errors.New("ewasm: module exceeds maximum size")
	ErrWasmBadSection     = errors.New("ewasm: invalid section header")
	ErrWasmSectionTooLong = errors.New("ewasm: section extends beyond bytecode")
	ErrWasmDupSection     = errors.New("ewasm: duplicate non-custom section")
	ErrWasmExportNotFound = errors.New("ewasm: exported function not found")
	ErrWasmExecFailed     = errors.New("ewasm: execution failed")
)

// WasmSection represents a parsed WASM binary section.
type WasmSection struct {
	ID   byte
	Size uint32
	Data []byte
}

// WasmModule represents a parsed and (simulated) compiled WASM module.
type WasmModule struct {
	Code              []byte
	Hash              types.Hash
	CompileTime       time.Duration
	Sections          []WasmSection
	ExportedFunctions []string
	CodeSectionSize   uint32
}

// ValidateWasmBytecode checks magic bytes (0x00 0x61 0x73 0x6d), version,
// size limits, and section header integrity.
func ValidateWasmBytecode(code []byte) error {
	if len(code) < WasmMinSize {
		return ErrWasmTooShort
	}
	if len(code) > WasmMaxSize {
		return ErrWasmTooLarge
	}
	if binary.LittleEndian.Uint32(code[0:4]) != WasmMagic {
		return ErrWasmInvalidMagic
	}
	if binary.LittleEndian.Uint32(code[4:8]) != 1 {
		return ErrWasmInvalidVersion
	}
	// Walk section headers to validate structure.
	offset := 8
	seen := make(map[byte]bool)
	for offset < len(code) {
		sectionID := code[offset]
		offset++
		size, n, err := decodeLEB128(code[offset:])
		if err != nil {
			return ErrWasmBadSection
		}
		offset += n
		if offset+int(size) > len(code) {
			return ErrWasmSectionTooLong
		}
		if sectionID != WasmSectionCustom {
			if seen[sectionID] {
				return ErrWasmDupSection
			}
			seen[sectionID] = true
		}
		offset += int(size)
	}
	return nil
}

// CompileModule validates WASM bytecode, parses sections, extracts exports,
// and returns a WasmModule.
func CompileModule(code []byte) (*WasmModule, error) {
	start := time.Now()
	if err := ValidateWasmBytecode(code); err != nil {
		return nil, err
	}
	sections, err := parseSections(code[8:])
	if err != nil {
		return nil, err
	}
	var exports []string
	var codeSectionSize uint32
	for _, s := range sections {
		if s.ID == WasmSectionExport {
			exports = parseExportNames(s.Data)
		}
		if s.ID == WasmSectionCode {
			codeSectionSize = s.Size
		}
	}
	return &WasmModule{
		Code: append([]byte(nil), code...), Hash: wasmHash(code),
		CompileTime: time.Since(start), Sections: sections,
		ExportedFunctions: exports, CodeSectionSize: codeSectionSize,
	}, nil
}

// ExecuteExport simulates execution of an exported WASM function. Returns a
// deterministic keccak256 result based on function name, args, and module hash.
func ExecuteExport(module *WasmModule, funcName string, args []byte) ([]byte, error) {
	if module == nil {
		return nil, ErrWasmExecFailed
	}
	found := false
	for _, name := range module.ExportedFunctions {
		if name == funcName {
			found = true
			break
		}
	}
	if !found {
		return nil, ErrWasmExportNotFound
	}
	h := sha3.NewLegacyKeccak256()
	h.Write([]byte(funcName))
	h.Write(args)
	h.Write(module.Hash[:])
	return h.Sum(nil), nil
}

// JITCache is a thread-safe LRU cache for compiled WASM modules.
type JITCache struct {
	mu       sync.Mutex
	capacity int
	items    map[types.Hash]*WasmModule
	order    []types.Hash // Front = most recently used.
}

// NewJITCache creates a new LRU cache with the given capacity.
func NewJITCache(capacity int) *JITCache {
	if capacity <= 0 {
		capacity = 64
	}
	return &JITCache{
		capacity: capacity,
		items:    make(map[types.Hash]*WasmModule, capacity),
		order:    make([]types.Hash, 0, capacity),
	}
}

// CacheModule stores a compiled module. Evicts LRU entry if at capacity.
func (c *JITCache) CacheModule(hash types.Hash, module *WasmModule) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.items[hash]; ok {
		c.moveToFront(hash)
		c.items[hash] = module
		return
	}
	if len(c.items) >= c.capacity {
		c.evictLRU()
	}
	c.items[hash] = module
	c.order = append([]types.Hash{hash}, c.order...)
}

// GetCachedModule retrieves a module, moving it to front of LRU on access.
func (c *JITCache) GetCachedModule(hash types.Hash) (*WasmModule, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	mod, ok := c.items[hash]
	if !ok {
		return nil, false
	}
	c.moveToFront(hash)
	return mod, true
}

// Size returns the number of cached modules.
func (c *JITCache) Size() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.items)
}

// Clear removes all entries from the cache.
func (c *JITCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items = make(map[types.Hash]*WasmModule, c.capacity)
	c.order = c.order[:0]
}

func (c *JITCache) evictLRU() {
	if len(c.order) == 0 {
		return
	}
	lru := c.order[len(c.order)-1]
	c.order = c.order[:len(c.order)-1]
	delete(c.items, lru)
}

func (c *JITCache) moveToFront(hash types.Hash) {
	for i, h := range c.order {
		if h == hash {
			c.order = append(c.order[:i], c.order[i+1:]...)
			break
		}
	}
	c.order = append([]types.Hash{hash}, c.order...)
}

// WasmGasCalculator computes gas costs for WASM operations.
type WasmGasCalculator struct {
	BaseCompileGas    uint64
	PerByteCompileGas uint64
	BaseExecGas       uint64
	PerByteInputGas   uint64
	PerCodeByteGas    uint64
}

// DefaultWasmGasCalculator returns a gas calculator with default parameters.
func DefaultWasmGasCalculator() *WasmGasCalculator {
	return &WasmGasCalculator{
		BaseCompileGas: 200, PerByteCompileGas: 3,
		BaseExecGas: 100, PerByteInputGas: 2, PerCodeByteGas: 1,
	}
}

// CompileGas returns gas for compiling a module of the given byte size.
func (g *WasmGasCalculator) CompileGas(codeSize uint64) uint64 {
	return g.BaseCompileGas + g.PerByteCompileGas*codeSize
}

// ExecuteGas returns gas for executing with given input and code section sizes.
func (g *WasmGasCalculator) ExecuteGas(inputSize, codeSectionSize uint64) uint64 {
	return g.BaseExecGas + g.PerByteInputGas*inputSize + g.PerCodeByteGas*codeSectionSize
}

// TotalGas returns combined compilation + execution gas.
func (g *WasmGasCalculator) TotalGas(codeSize, inputSize, codeSectionSize uint64) uint64 {
	return g.CompileGas(codeSize) + g.ExecuteGas(inputSize, codeSectionSize)
}

// --- Internal helpers ---

func wasmHash(code []byte) types.Hash {
	h := sha3.NewLegacyKeccak256()
	h.Write(code)
	var result types.Hash
	h.Sum(result[:0])
	return result
}

func parseSections(data []byte) ([]WasmSection, error) {
	var sections []WasmSection
	offset := 0
	for offset < len(data) {
		id := data[offset]
		offset++
		size, n, err := decodeLEB128(data[offset:])
		if err != nil {
			return nil, ErrWasmBadSection
		}
		offset += n
		if offset+int(size) > len(data) {
			return nil, ErrWasmSectionTooLong
		}
		sd := make([]byte, size)
		copy(sd, data[offset:offset+int(size)])
		sections = append(sections, WasmSection{ID: id, Size: size, Data: sd})
		offset += int(size)
	}
	return sections, nil
}

// parseExportNames extracts function export names from an export section.
// Format: count (LEB128), then per entry: name_len, name, kind, index.
func parseExportNames(data []byte) []string {
	if len(data) == 0 {
		return nil
	}
	count, n, err := decodeLEB128(data)
	if err != nil {
		return nil
	}
	offset := n
	var names []string
	for i := uint32(0); i < count && offset < len(data); i++ {
		nameLen, n2, err2 := decodeLEB128(data[offset:])
		if err2 != nil {
			break
		}
		offset += n2
		if offset+int(nameLen) > len(data) {
			break
		}
		name := string(data[offset : offset+int(nameLen)])
		offset += int(nameLen)
		if offset >= len(data) {
			break
		}
		kind := data[offset]
		offset++
		_, n3, err3 := decodeLEB128(data[offset:])
		if err3 != nil {
			break
		}
		offset += n3
		if kind == WasmExportFunc {
			names = append(names, name)
		}
	}
	return names
}

// decodeLEB128 decodes an unsigned LEB128 integer.
func decodeLEB128(data []byte) (uint32, int, error) {
	var result uint32
	var shift uint
	for i := 0; i < len(data) && i < 5; i++ {
		b := data[i]
		result |= uint32(b&0x7F) << shift
		if b&0x80 == 0 {
			return result, i + 1, nil
		}
		shift += 7
	}
	return 0, 0, errors.New("ewasm: invalid LEB128 encoding")
}

// BuildMinimalWasm builds a minimal valid WASM binary with optional function
// exports. Useful for testing.
func BuildMinimalWasm(exportNames ...string) []byte {
	buf := []byte{
		0x00, 0x61, 0x73, 0x6d, // magic
		0x01, 0x00, 0x00, 0x00, // version 1
	}
	// Type section: one func type () -> ().
	buf = appendSection(buf, WasmSectionType, []byte{0x01, 0x60, 0x00, 0x00})
	// Function section referencing type 0.
	funcCount := len(exportNames)
	if funcCount == 0 {
		funcCount = 1
	}
	funcSection := []byte{byte(funcCount)}
	for i := 0; i < funcCount; i++ {
		funcSection = append(funcSection, 0x00)
	}
	buf = appendSection(buf, WasmSectionFunction, funcSection)
	// Export section.
	if len(exportNames) > 0 {
		var ed []byte
		ed = append(ed, byte(len(exportNames)))
		for i, name := range exportNames {
			ed = append(ed, byte(len(name)))
			ed = append(ed, []byte(name)...)
			ed = append(ed, WasmExportFunc, byte(i))
		}
		buf = appendSection(buf, WasmSectionExport, ed)
	}
	// Code section with empty function bodies.
	var cd []byte
	cd = append(cd, byte(funcCount))
	for i := 0; i < funcCount; i++ {
		cd = append(cd, 0x02, 0x00, 0x0B) // size=2, locals=0, end
	}
	buf = appendSection(buf, WasmSectionCode, cd)
	return buf
}

func appendSection(buf []byte, id byte, data []byte) []byte {
	buf = append(buf, id)
	buf = appendLEB128(buf, uint32(len(data)))
	return append(buf, data...)
}

func appendLEB128(buf []byte, v uint32) []byte {
	for {
		b := byte(v & 0x7F)
		v >>= 7
		if v != 0 {
			b |= 0x80
		}
		buf = append(buf, b)
		if v == 0 {
			return buf
		}
	}
}
