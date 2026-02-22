// constraint_compiler.go compiles arithmetic circuits into a flat wire-indexed
// constraint format suitable for proof system backends (Groth16, PLONK, STARK).
// It handles wire layout management, gate-level circuit representation,
// witness generation from zxVM execution traces, and constraint serialization
// into a portable binary format.
//
// Part of the K+/M+ roadmap for canonical zxVM and mandatory proofs.
package zkvm

import (
	"encoding/binary"
	"errors"
	"math/big"
)

// Constraint compiler errors.
var (
	ErrCompilerFinalized    = errors.New("compiler: circuit already finalized")
	ErrCompilerNotFinalized = errors.New("compiler: circuit not yet finalized")
	ErrCompilerWireOOB      = errors.New("compiler: wire index out of bounds")
	ErrCompilerNoGates      = errors.New("compiler: no gates in circuit")
	ErrCompilerWitnessSize  = errors.New("compiler: witness size mismatch")
	ErrCompilerSerialize    = errors.New("compiler: serialization error")
	ErrCompilerDeserialize  = errors.New("compiler: deserialization error")
	ErrCompilerEmptyTrace   = errors.New("compiler: empty execution trace")
)

// GateType identifies the type of arithmetic gate in the flat circuit.
type GateType uint8

const (
	GateAdd    GateType = 0x01 // output = left + right (mod p)
	GateMul    GateType = 0x02 // output = left * right (mod p)
	GateConst  GateType = 0x03 // output = constant value
	GateSub    GateType = 0x04 // output = left - right (mod p)
	GateAssert GateType = 0x05 // assert left == right (no output wire)
)

// String returns the gate mnemonic.
func (g GateType) String() string {
	switch g {
	case GateAdd:
		return "ADD"
	case GateMul:
		return "MUL"
	case GateConst:
		return "CONST"
	case GateSub:
		return "SUB"
	case GateAssert:
		return "ASSERT"
	default:
		return "UNKNOWN"
	}
}

// WireID is a unique identifier for a wire in the flat circuit layout.
type WireID uint32

const (
	// NullWire indicates no wire connection (used for constant gates).
	NullWire WireID = 0xFFFFFFFF
)

// FlatGate is a single gate in the compiled circuit. Each gate reads from
// up to two input wires and writes to one output wire.
type FlatGate struct {
	Type     GateType // Gate operation.
	Left     WireID   // Left input wire.
	Right    WireID   // Right input wire (NullWire for unary gates).
	Output   WireID   // Output wire (NullWire for assert gates).
	ConstVal *big.Int // Constant value (only for GateConst).
}

// ConstraintCompiler manages wire allocation and gate construction for a
// flat arithmetic circuit. After all gates are added, Finalize() locks
// the circuit and Serialize() exports it.
type ConstraintCompiler struct {
	gates     []FlatGate
	nextWire  WireID
	pubWires  map[WireID]bool
	finalized bool
	field     *big.Int
}

// NewConstraintCompiler creates a new compiler over the BN254 scalar field.
func NewConstraintCompiler() *ConstraintCompiler {
	return &ConstraintCompiler{
		gates:    make([]FlatGate, 0, 64),
		nextWire: 0,
		pubWires: make(map[WireID]bool),
		field:    new(big.Int).Set(bn254ScalarField),
	}
}

// NewConstraintCompilerWithField creates a compiler over a custom prime field.
func NewConstraintCompilerWithField(field *big.Int) *ConstraintCompiler {
	cc := NewConstraintCompiler()
	if field != nil && field.Sign() > 0 {
		cc.field = new(big.Int).Set(field)
	}
	return cc
}

// AllocWire allocates a new wire and returns its ID.
func (cc *ConstraintCompiler) AllocWire() (WireID, error) {
	if cc.finalized {
		return 0, ErrCompilerFinalized
	}
	w := cc.nextWire
	cc.nextWire++
	return w, nil
}

// AllocPublicWire allocates a new public input wire.
func (cc *ConstraintCompiler) AllocPublicWire() (WireID, error) {
	w, err := cc.AllocWire()
	if err != nil {
		return 0, err
	}
	cc.pubWires[w] = true
	return w, nil
}

// WireCount returns the total number of allocated wires.
func (cc *ConstraintCompiler) WireCount() int {
	return int(cc.nextWire)
}

// GateCount returns the total number of gates.
func (cc *ConstraintCompiler) GateCount() int {
	return len(cc.gates)
}

// IsPublicWire returns true if the wire is a public input.
func (cc *ConstraintCompiler) IsPublicWire(w WireID) bool {
	return cc.pubWires[w]
}

// AddGate appends an addition gate: output = left + right.
func (cc *ConstraintCompiler) AddGate(left, right, output WireID) error {
	if cc.finalized {
		return ErrCompilerFinalized
	}
	cc.gates = append(cc.gates, FlatGate{
		Type: GateAdd, Left: left, Right: right, Output: output,
	})
	return nil
}

// MulGate appends a multiplication gate: output = left * right.
func (cc *ConstraintCompiler) MulGate(left, right, output WireID) error {
	if cc.finalized {
		return ErrCompilerFinalized
	}
	cc.gates = append(cc.gates, FlatGate{
		Type: GateMul, Left: left, Right: right, Output: output,
	})
	return nil
}

// SubGate appends a subtraction gate: output = left - right.
func (cc *ConstraintCompiler) SubGate(left, right, output WireID) error {
	if cc.finalized {
		return ErrCompilerFinalized
	}
	cc.gates = append(cc.gates, FlatGate{
		Type: GateSub, Left: left, Right: right, Output: output,
	})
	return nil
}

// ConstGate appends a constant assignment gate: output = val.
func (cc *ConstraintCompiler) ConstGate(output WireID, val *big.Int) error {
	if cc.finalized {
		return ErrCompilerFinalized
	}
	v := new(big.Int).Set(val)
	v.Mod(v, cc.field)
	cc.gates = append(cc.gates, FlatGate{
		Type: GateConst, Left: NullWire, Right: NullWire,
		Output: output, ConstVal: v,
	})
	return nil
}

// AssertGate appends an equality assertion: left must equal right.
func (cc *ConstraintCompiler) AssertGate(left, right WireID) error {
	if cc.finalized {
		return ErrCompilerFinalized
	}
	cc.gates = append(cc.gates, FlatGate{
		Type: GateAssert, Left: left, Right: right, Output: NullWire,
	})
	return nil
}

// Finalize locks the circuit. No further gates or wires may be added.
func (cc *ConstraintCompiler) Finalize() error {
	if cc.finalized {
		return ErrCompilerFinalized
	}
	if len(cc.gates) == 0 {
		return ErrCompilerNoGates
	}
	cc.finalized = true
	return nil
}

// WitnessAssignment maps wire IDs to their field element values.
type WitnessAssignment map[WireID]*big.Int

// EvaluateWitness computes wire values by propagating through the gates
// in order, given initial assignments for input wires. Missing input values
// default to zero.
func (cc *ConstraintCompiler) EvaluateWitness(inputs WitnessAssignment) (WitnessAssignment, error) {
	if !cc.finalized {
		return nil, ErrCompilerNotFinalized
	}
	w := make(WitnessAssignment, int(cc.nextWire))
	// Copy inputs.
	for id, val := range inputs {
		w[id] = new(big.Int).Mod(new(big.Int).Set(val), cc.field)
	}
	// Fill zeros for unset wires.
	for i := WireID(0); i < cc.nextWire; i++ {
		if _, ok := w[i]; !ok {
			w[i] = new(big.Int)
		}
	}
	// Propagate.
	for _, g := range cc.gates {
		switch g.Type {
		case GateAdd:
			sum := new(big.Int).Add(w[g.Left], w[g.Right])
			sum.Mod(sum, cc.field)
			w[g.Output] = sum
		case GateMul:
			prod := new(big.Int).Mul(w[g.Left], w[g.Right])
			prod.Mod(prod, cc.field)
			w[g.Output] = prod
		case GateSub:
			diff := new(big.Int).Sub(w[g.Left], w[g.Right])
			diff.Mod(diff, cc.field)
			w[g.Output] = diff
		case GateConst:
			w[g.Output] = new(big.Int).Set(g.ConstVal)
		case GateAssert:
			// No output wire, just validate.
		}
	}
	return w, nil
}

// CheckWitness verifies that the witness satisfies all circuit constraints.
func (cc *ConstraintCompiler) CheckWitness(w WitnessAssignment) error {
	if !cc.finalized {
		return ErrCompilerNotFinalized
	}
	if len(w) < int(cc.nextWire) {
		return ErrCompilerWitnessSize
	}
	for i, g := range cc.gates {
		switch g.Type {
		case GateAdd:
			expected := new(big.Int).Add(w[g.Left], w[g.Right])
			expected.Mod(expected, cc.field)
			actual := new(big.Int).Mod(w[g.Output], cc.field)
			if expected.Cmp(actual) != 0 {
				return errors.New("compiler: ADD gate " + itoa(i) + " unsatisfied")
			}
		case GateMul:
			expected := new(big.Int).Mul(w[g.Left], w[g.Right])
			expected.Mod(expected, cc.field)
			actual := new(big.Int).Mod(w[g.Output], cc.field)
			if expected.Cmp(actual) != 0 {
				return errors.New("compiler: MUL gate " + itoa(i) + " unsatisfied")
			}
		case GateSub:
			expected := new(big.Int).Sub(w[g.Left], w[g.Right])
			expected.Mod(expected, cc.field)
			actual := new(big.Int).Mod(w[g.Output], cc.field)
			if expected.Cmp(actual) != 0 {
				return errors.New("compiler: SUB gate " + itoa(i) + " unsatisfied")
			}
		case GateConst:
			actual := new(big.Int).Mod(w[g.Output], cc.field)
			if g.ConstVal.Cmp(actual) != 0 {
				return errors.New("compiler: CONST gate " + itoa(i) + " unsatisfied")
			}
		case GateAssert:
			left := new(big.Int).Mod(w[g.Left], cc.field)
			right := new(big.Int).Mod(w[g.Right], cc.field)
			if left.Cmp(right) != 0 {
				return errors.New("compiler: ASSERT gate " + itoa(i) + " unsatisfied")
			}
		}
	}
	return nil
}

// Serialized circuit header (magic + version + counts).
const (
	circuitMagic   uint32 = 0x5A4B4349 // "ZKCI"
	circuitVersion uint16 = 1
	circuitHdrSize        = 4 + 2 + 4 + 4 + 4 // magic(4)+ver(2)+nWires(4)+nGates(4)+nPub(4)
)

// Serialize exports the finalized circuit to a portable binary format.
// Format per gate: type(1) + left(4) + right(4) + output(4) + constLen(2) + constBytes(var).
func (cc *ConstraintCompiler) Serialize() ([]byte, error) {
	if !cc.finalized {
		return nil, ErrCompilerNotFinalized
	}
	// Pre-compute size.
	size := circuitHdrSize
	for _, g := range cc.gates {
		size += 1 + 4 + 4 + 4 + 2
		if g.Type == GateConst && g.ConstVal != nil {
			size += len(g.ConstVal.Bytes())
		}
	}
	// Public wire list: 4 bytes each.
	size += len(cc.pubWires) * 4

	buf := make([]byte, size)
	off := 0
	binary.BigEndian.PutUint32(buf[off:], circuitMagic)
	off += 4
	binary.BigEndian.PutUint16(buf[off:], circuitVersion)
	off += 2
	binary.BigEndian.PutUint32(buf[off:], uint32(cc.nextWire))
	off += 4
	binary.BigEndian.PutUint32(buf[off:], uint32(len(cc.gates)))
	off += 4
	binary.BigEndian.PutUint32(buf[off:], uint32(len(cc.pubWires)))
	off += 4

	// Write gates.
	for _, g := range cc.gates {
		buf[off] = byte(g.Type)
		off++
		binary.BigEndian.PutUint32(buf[off:], uint32(g.Left))
		off += 4
		binary.BigEndian.PutUint32(buf[off:], uint32(g.Right))
		off += 4
		binary.BigEndian.PutUint32(buf[off:], uint32(g.Output))
		off += 4
		if g.Type == GateConst && g.ConstVal != nil {
			cb := g.ConstVal.Bytes()
			binary.BigEndian.PutUint16(buf[off:], uint16(len(cb)))
			off += 2
			copy(buf[off:], cb)
			off += len(cb)
		} else {
			binary.BigEndian.PutUint16(buf[off:], 0)
			off += 2
		}
	}
	// Write public wire IDs.
	for w := range cc.pubWires {
		binary.BigEndian.PutUint32(buf[off:], uint32(w))
		off += 4
	}
	return buf[:off], nil
}

// DeserializeCircuit reconstructs a ConstraintCompiler from serialized bytes.
func DeserializeCircuit(data []byte) (*ConstraintCompiler, error) {
	if len(data) < circuitHdrSize {
		return nil, ErrCompilerDeserialize
	}
	off := 0
	magic := binary.BigEndian.Uint32(data[off:])
	off += 4
	if magic != circuitMagic {
		return nil, ErrCompilerDeserialize
	}
	ver := binary.BigEndian.Uint16(data[off:])
	off += 2
	if ver != circuitVersion {
		return nil, ErrCompilerDeserialize
	}
	nWires := binary.BigEndian.Uint32(data[off:])
	off += 4
	nGates := binary.BigEndian.Uint32(data[off:])
	off += 4
	nPub := binary.BigEndian.Uint32(data[off:])
	off += 4

	cc := NewConstraintCompiler()
	cc.nextWire = WireID(nWires)
	cc.gates = make([]FlatGate, nGates)

	for i := uint32(0); i < nGates; i++ {
		if off+15 > len(data) {
			return nil, ErrCompilerDeserialize
		}
		g := FlatGate{
			Type:   GateType(data[off]),
			Left:   WireID(binary.BigEndian.Uint32(data[off+1:])),
			Right:  WireID(binary.BigEndian.Uint32(data[off+5:])),
			Output: WireID(binary.BigEndian.Uint32(data[off+9:])),
		}
		off += 13
		constLen := binary.BigEndian.Uint16(data[off:])
		off += 2
		if constLen > 0 {
			if off+int(constLen) > len(data) {
				return nil, ErrCompilerDeserialize
			}
			g.ConstVal = new(big.Int).SetBytes(data[off : off+int(constLen)])
			off += int(constLen)
		}
		cc.gates[i] = g
	}
	for i := uint32(0); i < nPub; i++ {
		if off+4 > len(data) {
			return nil, ErrCompilerDeserialize
		}
		w := WireID(binary.BigEndian.Uint32(data[off:]))
		off += 4
		cc.pubWires[w] = true
	}
	cc.finalized = true
	return cc, nil
}

// GenerateWitnessFromTrace builds a witness assignment from a zxVM execution
// trace. Each trace step's opcode and stack hash are mapped to circuit wires
// for verification within the proof system.
func GenerateWitnessFromTrace(trace *ZxTrace, numWires int) (WitnessAssignment, error) {
	if trace == nil || len(trace.Steps) == 0 {
		return nil, ErrCompilerEmptyTrace
	}
	w := make(WitnessAssignment, numWires)
	for i := 0; i < numWires; i++ {
		w[WireID(i)] = new(big.Int)
	}
	// Map trace steps to wires: for each step use PC and opcode as field values.
	for i, step := range trace.Steps {
		wireIdx := WireID(i % numWires)
		val := new(big.Int).SetUint64(step.PC)
		val.Add(val, new(big.Int).SetUint64(uint64(step.Opcode)<<32))
		if len(step.StackHash) >= 8 {
			sh := binary.BigEndian.Uint64(step.StackHash[:8])
			val.Add(val, new(big.Int).SetUint64(sh))
		}
		val.Mod(val, bn254ScalarField)
		w[wireIdx] = val
	}
	return w, nil
}

// itoa converts an int to a decimal string without importing strconv.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := false
	if n < 0 {
		neg = true
		n = -n
	}
	var digits [20]byte
	pos := len(digits)
	for n > 0 {
		pos--
		digits[pos] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		pos--
		digits[pos] = '-'
	}
	return string(digits[pos:])
}
