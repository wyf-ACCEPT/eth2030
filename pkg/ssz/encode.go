package ssz

import "encoding/binary"

// --- Basic type encoding ---

// MarshalBool encodes a boolean as a single byte: 0x01 for true, 0x00 for false.
func MarshalBool(v bool) []byte {
	if v {
		return []byte{1}
	}
	return []byte{0}
}

// MarshalUint8 encodes a uint8 as a single byte.
func MarshalUint8(v uint8) []byte {
	return []byte{v}
}

// MarshalUint16 encodes a uint16 as 2 bytes little-endian.
func MarshalUint16(v uint16) []byte {
	b := make([]byte, 2)
	binary.LittleEndian.PutUint16(b, v)
	return b
}

// MarshalUint32 encodes a uint32 as 4 bytes little-endian.
func MarshalUint32(v uint32) []byte {
	b := make([]byte, 4)
	binary.LittleEndian.PutUint32(b, v)
	return b
}

// MarshalUint64 encodes a uint64 as 8 bytes little-endian.
func MarshalUint64(v uint64) []byte {
	b := make([]byte, 8)
	binary.LittleEndian.PutUint64(b, v)
	return b
}

// MarshalUint128 encodes a 128-bit unsigned integer (as [2]uint64, little-endian
// limbs: lo, hi) into 16 bytes little-endian.
func MarshalUint128(lo, hi uint64) []byte {
	b := make([]byte, 16)
	binary.LittleEndian.PutUint64(b[0:8], lo)
	binary.LittleEndian.PutUint64(b[8:16], hi)
	return b
}

// MarshalUint256 encodes a 256-bit unsigned integer (as [4]uint64, little-endian
// limbs) into 32 bytes little-endian.
func MarshalUint256(limbs [4]uint64) []byte {
	b := make([]byte, 32)
	binary.LittleEndian.PutUint64(b[0:8], limbs[0])
	binary.LittleEndian.PutUint64(b[8:16], limbs[1])
	binary.LittleEndian.PutUint64(b[16:24], limbs[2])
	binary.LittleEndian.PutUint64(b[24:32], limbs[3])
	return b
}

// --- Composite type encoding ---

// MarshalVector encodes a fixed-length vector of fixed-size elements by
// concatenating each element's SSZ encoding.
func MarshalVector(elements [][]byte) []byte {
	var out []byte
	for _, e := range elements {
		out = append(out, e...)
	}
	return out
}

// MarshalFixedContainer encodes a container where all fields are fixed-size
// by concatenating each field's SSZ encoding.
func MarshalFixedContainer(fields [][]byte) []byte {
	return MarshalVector(fields)
}

// MarshalList encodes a variable-length list of fixed-size elements.
// This is the same as MarshalVector but semantically different (lists have a
// max length and mix_in_length during Merkleization).
func MarshalList(elements [][]byte) []byte {
	return MarshalVector(elements)
}

// MarshalVariableContainer encodes a container that has variable-length fields.
// fixedParts: the encoded fixed-size fields (nil for variable-size field slots).
// variableParts: the encoded variable-size fields, in order.
// variableIndices: the indices within fixedParts that are variable-size.
func MarshalVariableContainer(fixedParts [][]byte, variableParts [][]byte, variableIndices []int) []byte {
	// Calculate the fixed portion size. Each variable field contributes
	// a 4-byte offset in the fixed section.
	fixedSize := 0
	for i, fp := range fixedParts {
		if isVariableIndex(i, variableIndices) {
			fixedSize += BytesPerLengthOffset
		} else {
			fixedSize += len(fp)
		}
	}

	// Calculate offsets for variable parts.
	offsets := make([]uint32, len(variableParts))
	currentOffset := uint32(fixedSize)
	for i, vp := range variableParts {
		offsets[i] = currentOffset
		currentOffset += uint32(len(vp))
	}

	// Build the output.
	out := make([]byte, 0, int(currentOffset))
	varIdx := 0
	for i, fp := range fixedParts {
		if isVariableIndex(i, variableIndices) {
			// Write offset.
			var ob [4]byte
			binary.LittleEndian.PutUint32(ob[:], offsets[varIdx])
			out = append(out, ob[:]...)
			varIdx++
		} else {
			out = append(out, fp...)
		}
	}
	// Append variable parts.
	for _, vp := range variableParts {
		out = append(out, vp...)
	}
	return out
}

func isVariableIndex(idx int, variableIndices []int) bool {
	for _, vi := range variableIndices {
		if vi == idx {
			return true
		}
	}
	return false
}

// --- Bitfield encoding ---

// MarshalBitvector encodes a bitvector of exactly n bits. The bits are packed
// into bytes with the least significant bit first. The length of bits must
// equal n.
func MarshalBitvector(bits []bool) []byte {
	numBytes := (len(bits) + 7) / 8
	out := make([]byte, numBytes)
	for i, b := range bits {
		if b {
			out[i/8] |= 1 << (uint(i) % 8)
		}
	}
	return out
}

// MarshalBitlist encodes a bitlist of at most maxLen bits. The encoding
// includes a sentinel bit to mark the length boundary.
func MarshalBitlist(bits []bool) []byte {
	// Append a sentinel 1-bit after the last data bit.
	withSentinel := make([]bool, len(bits)+1)
	copy(withSentinel, bits)
	withSentinel[len(bits)] = true
	numBytes := (len(withSentinel) + 7) / 8
	out := make([]byte, numBytes)
	for i, b := range withSentinel {
		if b {
			out[i/8] |= 1 << (uint(i) % 8)
		}
	}
	return out
}

// MarshalByteVector encodes a fixed-length byte vector (ByteVector[N]).
// The input must be exactly n bytes.
func MarshalByteVector(data []byte) []byte {
	out := make([]byte, len(data))
	copy(out, data)
	return out
}

// MarshalByteList encodes a variable-length byte list (ByteList[N]).
// Just returns a copy of the data (the list length is implicit in SSZ
// container offsets, and is mixed in during Merkleization).
func MarshalByteList(data []byte) []byte {
	out := make([]byte, len(data))
	copy(out, data)
	return out
}
