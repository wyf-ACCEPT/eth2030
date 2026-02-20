// riscv_witness.go implements the execution witness collector for the RISC-V
// CPU emulator. It records every instruction's inputs and outputs to build
// a complete execution trace suitable for ZK proof generation.
//
// The trace captures: PC, instruction word, register state before/after,
// and memory operations at each step. This trace is then serialized for
// the proof backend.
//
// Part of the K+ roadmap for canonical RISC-V guest execution.
package zkvm

import (
	"crypto/sha256"
	"encoding/binary"
)

// RVWitnessStep records a single CPU step in the execution trace.
type RVWitnessStep struct {
	PC          uint32
	Instruction uint32
	RegsBefore  [RVRegCount]uint32
	RegsAfter   [RVRegCount]uint32
	MemoryOps   []MemOp
}

// RVWitnessCollector accumulates execution trace steps.
type RVWitnessCollector struct {
	Steps []RVWitnessStep
}

// NewRVWitnessCollector creates a new empty witness collector.
func NewRVWitnessCollector() *RVWitnessCollector {
	return &RVWitnessCollector{
		Steps: make([]RVWitnessStep, 0, 256),
	}
}

// RecordStep adds a step to the trace.
func (w *RVWitnessCollector) RecordStep(
	pc uint32,
	instr uint32,
	regsBefore [RVRegCount]uint32,
	regsAfter [RVRegCount]uint32,
	memOps []MemOp,
) {
	step := RVWitnessStep{
		PC:          pc,
		Instruction: instr,
		RegsBefore:  regsBefore,
		RegsAfter:   regsAfter,
	}
	if len(memOps) > 0 {
		step.MemoryOps = make([]MemOp, len(memOps))
		copy(step.MemoryOps, memOps)
	}
	w.Steps = append(w.Steps, step)
}

// StepCount returns the number of recorded steps.
func (w *RVWitnessCollector) StepCount() int {
	return len(w.Steps)
}

// Reset clears all recorded steps.
func (w *RVWitnessCollector) Reset() {
	w.Steps = w.Steps[:0]
}

// Serialize encodes the execution trace into a byte slice suitable for
// proof generation. Format per step:
//
//	PC(4) + instruction(4) + regsBefore(128) + regsAfter(128) +
//	numMemOps(2) + [addr(4)+value(4)+isWrite(1)] per memOp
func (w *RVWitnessCollector) Serialize() []byte {
	// Header: step count (4 bytes).
	headerSize := 4
	stepBaseSize := 4 + 4 + (RVRegCount * 4) + (RVRegCount * 4) + 2
	memOpSize := 4 + 4 + 1

	totalSize := headerSize
	for _, step := range w.Steps {
		totalSize += stepBaseSize + len(step.MemoryOps)*memOpSize
	}

	buf := make([]byte, totalSize)
	off := 0

	// Write step count.
	binary.LittleEndian.PutUint32(buf[off:], uint32(len(w.Steps)))
	off += 4

	for _, step := range w.Steps {
		// PC.
		binary.LittleEndian.PutUint32(buf[off:], step.PC)
		off += 4

		// Instruction.
		binary.LittleEndian.PutUint32(buf[off:], step.Instruction)
		off += 4

		// RegsBefore.
		for i := 0; i < RVRegCount; i++ {
			binary.LittleEndian.PutUint32(buf[off:], step.RegsBefore[i])
			off += 4
		}

		// RegsAfter.
		for i := 0; i < RVRegCount; i++ {
			binary.LittleEndian.PutUint32(buf[off:], step.RegsAfter[i])
			off += 4
		}

		// Memory operations count.
		binary.LittleEndian.PutUint16(buf[off:], uint16(len(step.MemoryOps)))
		off += 2

		// Memory operations.
		for _, mop := range step.MemoryOps {
			binary.LittleEndian.PutUint32(buf[off:], mop.Addr)
			off += 4
			binary.LittleEndian.PutUint32(buf[off:], mop.Value)
			off += 4
			if mop.IsWrite {
				buf[off] = 1
			}
			off++
		}
	}

	return buf[:off]
}

// Deserialize reconstructs a witness trace from serialized bytes.
func DeserializeWitness(data []byte) (*RVWitnessCollector, error) {
	if len(data) < 4 {
		return nil, ErrRVEmptyProgram
	}

	off := 0
	stepCount := binary.LittleEndian.Uint32(data[off:])
	off += 4

	w := &RVWitnessCollector{
		Steps: make([]RVWitnessStep, 0, stepCount),
	}

	for i := uint32(0); i < stepCount; i++ {
		if off+8+(RVRegCount*4*2)+2 > len(data) {
			break
		}

		step := RVWitnessStep{}
		step.PC = binary.LittleEndian.Uint32(data[off:])
		off += 4
		step.Instruction = binary.LittleEndian.Uint32(data[off:])
		off += 4

		for j := 0; j < RVRegCount; j++ {
			step.RegsBefore[j] = binary.LittleEndian.Uint32(data[off:])
			off += 4
		}
		for j := 0; j < RVRegCount; j++ {
			step.RegsAfter[j] = binary.LittleEndian.Uint32(data[off:])
			off += 4
		}

		numOps := binary.LittleEndian.Uint16(data[off:])
		off += 2

		step.MemoryOps = make([]MemOp, numOps)
		for j := uint16(0); j < numOps; j++ {
			if off+9 > len(data) {
				break
			}
			step.MemoryOps[j].Addr = binary.LittleEndian.Uint32(data[off:])
			off += 4
			step.MemoryOps[j].Value = binary.LittleEndian.Uint32(data[off:])
			off += 4
			step.MemoryOps[j].IsWrite = data[off] != 0
			off++
		}

		w.Steps = append(w.Steps, step)
	}

	return w, nil
}

// ComputeTraceCommitment computes a SHA-256 Merkle root over the witness
// steps. Each leaf is SHA-256(step_data). The tree is built bottom-up.
func (w *RVWitnessCollector) ComputeTraceCommitment() [32]byte {
	if len(w.Steps) == 0 {
		return sha256.Sum256(nil)
	}

	// Compute leaf hashes.
	leaves := make([][32]byte, len(w.Steps))
	for i, step := range w.Steps {
		leaves[i] = hashWitnessStep(step)
	}

	return merkleRoot(leaves)
}

// hashWitnessStep computes SHA-256 of a single witness step.
func hashWitnessStep(step RVWitnessStep) [32]byte {
	h := sha256.New()

	var buf [4]byte
	binary.LittleEndian.PutUint32(buf[:], step.PC)
	h.Write(buf[:])

	binary.LittleEndian.PutUint32(buf[:], step.Instruction)
	h.Write(buf[:])

	for i := 0; i < RVRegCount; i++ {
		binary.LittleEndian.PutUint32(buf[:], step.RegsBefore[i])
		h.Write(buf[:])
	}
	for i := 0; i < RVRegCount; i++ {
		binary.LittleEndian.PutUint32(buf[:], step.RegsAfter[i])
		h.Write(buf[:])
	}

	for _, mop := range step.MemoryOps {
		binary.LittleEndian.PutUint32(buf[:], mop.Addr)
		h.Write(buf[:])
		binary.LittleEndian.PutUint32(buf[:], mop.Value)
		h.Write(buf[:])
		if mop.IsWrite {
			h.Write([]byte{1})
		} else {
			h.Write([]byte{0})
		}
	}

	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result
}

// merkleRoot computes a binary Merkle tree root from leaves using SHA-256.
// Pads with duplicate of the last leaf if the count is odd.
func merkleRoot(leaves [][32]byte) [32]byte {
	if len(leaves) == 0 {
		return sha256.Sum256(nil)
	}
	if len(leaves) == 1 {
		return leaves[0]
	}

	// Pad to even.
	current := make([][32]byte, len(leaves))
	copy(current, leaves)
	if len(current)%2 != 0 {
		current = append(current, current[len(current)-1])
	}

	for len(current) > 1 {
		next := make([][32]byte, len(current)/2)
		for i := 0; i < len(current); i += 2 {
			h := sha256.New()
			h.Write(current[i][:])
			h.Write(current[i+1][:])
			copy(next[i/2][:], h.Sum(nil))
		}
		current = next
		if len(current) > 1 && len(current)%2 != 0 {
			current = append(current, current[len(current)-1])
		}
	}

	return current[0]
}
