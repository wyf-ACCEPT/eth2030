package vm

import (
	"errors"
	"fmt"
	"math/big"
)

// EVMStack errors.
var (
	ErrEVMStackOverflow  = errors.New("evm: stack overflow (max 1024)")
	ErrEVMStackUnderflow = errors.New("evm: stack underflow")
	ErrEVMSwapOutOfRange = errors.New("evm: swap position out of range")
	ErrEVMDupOutOfRange  = errors.New("evm: dup position out of range")
)

// evmStackLimit is the maximum depth of the EVM stack (1024 elements).
const evmStackLimit = 1024

// maxSwap is the maximum SWAP operand (SWAP1 through SWAP16).
const maxSwap = 16

// maxDup is the maximum DUP operand (DUP1 through DUP16).
const maxDup = 16

// EVMStack implements a 1024-element uint256 stack for EVM execution.
// It provides error-returning variants of all stack operations, making
// it suitable for use in contexts that need explicit overflow/underflow
// checking (e.g., validation, debugging, standalone execution).
type EVMStack struct {
	data [evmStackLimit]*big.Int
	top  int // number of elements on the stack
}

// NewEVMStack returns a new empty EVMStack.
func NewEVMStack() *EVMStack {
	return &EVMStack{}
}

// Push pushes a value onto the stack. Returns ErrEVMStackOverflow if
// the stack is full (1024 elements).
func (s *EVMStack) Push(val *big.Int) error {
	if s.top >= evmStackLimit {
		return ErrEVMStackOverflow
	}
	// Store a copy to avoid aliasing with the caller's value.
	s.data[s.top] = new(big.Int).Set(val)
	s.top++
	return nil
}

// Pop removes and returns the top element. Returns ErrEVMStackUnderflow
// if the stack is empty.
func (s *EVMStack) Pop() (*big.Int, error) {
	if s.top == 0 {
		return nil, ErrEVMStackUnderflow
	}
	s.top--
	val := s.data[s.top]
	s.data[s.top] = nil // clear reference for GC
	return val, nil
}

// Peek returns the top element without removing it. Returns
// ErrEVMStackUnderflow if the stack is empty.
func (s *EVMStack) Peek() (*big.Int, error) {
	if s.top == 0 {
		return nil, ErrEVMStackUnderflow
	}
	return s.data[s.top-1], nil
}

// Swap swaps the top element with the nth element from the top.
// n must be in [1, 16] (corresponding to SWAP1 through SWAP16).
// The stack must have at least n+1 elements.
func (s *EVMStack) Swap(n int) error {
	if n < 1 || n > maxSwap {
		return fmt.Errorf("%w: SWAP%d", ErrEVMSwapOutOfRange, n)
	}
	if s.top < n+1 {
		return fmt.Errorf("%w: need %d elements for SWAP%d, have %d",
			ErrEVMStackUnderflow, n+1, n, s.top)
	}
	topIdx := s.top - 1
	nthIdx := s.top - 1 - n
	s.data[topIdx], s.data[nthIdx] = s.data[nthIdx], s.data[topIdx]
	return nil
}

// Dup duplicates the nth element from the top and pushes the copy.
// n must be in [1, 16] (corresponding to DUP1 through DUP16).
// The stack must have at least n elements and not be full.
func (s *EVMStack) Dup(n int) error {
	if n < 1 || n > maxDup {
		return fmt.Errorf("%w: DUP%d", ErrEVMDupOutOfRange, n)
	}
	if s.top < n {
		return fmt.Errorf("%w: need %d elements for DUP%d, have %d",
			ErrEVMStackUnderflow, n, n, s.top)
	}
	if s.top >= evmStackLimit {
		return ErrEVMStackOverflow
	}
	// Copy the nth element from the top (1-indexed: 1 = top).
	val := s.data[s.top-n]
	s.data[s.top] = new(big.Int).Set(val)
	s.top++
	return nil
}

// Len returns the current number of elements on the stack.
func (s *EVMStack) Len() int {
	return s.top
}

// Reset clears all elements from the stack.
func (s *EVMStack) Reset() {
	for i := 0; i < s.top; i++ {
		s.data[i] = nil
	}
	s.top = 0
}
