package vm

import "math/big"

// Memory implements a simple EVM memory model (byte-addressable, word-aligned expansion).
type Memory struct {
	store       []byte
	lastGasCost uint64
}

// NewMemory returns a new Memory instance.
func NewMemory() *Memory {
	return &Memory{}
}

// Set copies value into memory at the given offset.
func (m *Memory) Set(offset, size uint64, value []byte) {
	if size == 0 {
		return
	}
	if offset+size > uint64(len(m.store)) {
		panic("memory: out of bounds write")
	}
	copy(m.store[offset:offset+size], value)
}

// Set32 writes a 32-byte big.Int value at the given offset (big-endian, zero-padded).
func (m *Memory) Set32(offset uint64, val *big.Int) {
	if offset+32 > uint64(len(m.store)) {
		panic("memory: out of bounds write")
	}
	// Clear the 32-byte region first.
	copy(m.store[offset:offset+32], make([]byte, 32))
	b := val.Bytes()
	// Right-align (big-endian).
	copy(m.store[offset+32-uint64(len(b)):offset+32], b)
}

// Resize grows memory to the given size (in bytes), rounded up to 32-byte words.
func (m *Memory) Resize(size uint64) {
	if uint64(len(m.store)) < size {
		m.store = append(m.store, make([]byte, size-uint64(len(m.store)))...)
	}
}

// Get returns a copy of the memory contents at [offset, offset+size).
func (m *Memory) Get(offset, size int64) []byte {
	if size == 0 {
		return nil
	}
	out := make([]byte, size)
	copy(out, m.store[offset:offset+size])
	return out
}

// GetPtr returns a direct slice reference to memory at [offset, offset+size).
func (m *Memory) GetPtr(offset, size int64) []byte {
	if size == 0 {
		return nil
	}
	return m.store[offset : offset+size]
}

// Len returns the current length of the memory in bytes.
func (m *Memory) Len() int {
	return len(m.store)
}

// Data returns the full backing slice.
func (m *Memory) Data() []byte {
	return m.store
}

// MaxMemorySize is the maximum allowed memory size (32 MiB) to prevent DoS.
const MaxMemorySize = 32 * 1024 * 1024

// MemoryCost calculates the gas cost for expanding memory to newSize bytes.
// Returns 0 if no expansion is needed. Returns (cost, true) on success
// or (0, false) if the new size overflows or exceeds MaxMemorySize.
func MemoryCost(currentSize, newSize uint64) (uint64, bool) {
	if newSize <= currentSize {
		return 0, true
	}
	if newSize > MaxMemorySize {
		return 0, false
	}
	// Check for overflow in word calculation: (newSize + 31) could overflow.
	if newSize > (^uint64(0))-31 {
		return 0, false
	}
	newWords := (newSize + 31) / 32
	// Calculate cost for new size.
	newCost := newWords*3 + (newWords*newWords)/512
	if currentSize == 0 {
		return newCost, true
	}
	oldWords := (currentSize + 31) / 32
	oldCost := oldWords*3 + (oldWords*oldWords)/512
	return newCost - oldCost, true
}
