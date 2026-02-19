package vm

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math/big"

	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/crypto"
)

// ABITypeKind identifies the category of each ABI type.
type ABITypeKind uint8

const (
	ABIUint256      ABITypeKind = iota // uint256
	ABIAddress                         // address (20 bytes, left-padded to 32)
	ABIBool                            // bool
	ABIBytes                           // bytes (dynamic)
	ABIString                          // string (dynamic)
	ABIFixedArray                      // T[N] fixed-size array
	ABIDynamicArray                    // T[] dynamic array
	ABITuple                           // tuple (struct)
	ABIFixedBytes                      // bytesN (1..32, static)
)

// ABIType describes a Solidity ABI type for encoding/decoding.
type ABIType struct {
	Kind   ABITypeKind
	Size   int       // fixed array length or bytesN size (1..32)
	Elem   *ABIType  // element type for arrays
	Fields []ABIType // field types for tuples
}

// isDynamic returns true if this type uses head/tail (indirect) encoding.
func (t ABIType) isDynamic() bool {
	switch t.Kind {
	case ABIBytes, ABIString, ABIDynamicArray:
		return true
	case ABIFixedArray:
		return t.Elem != nil && t.Elem.isDynamic()
	case ABITuple:
		for _, f := range t.Fields {
			if f.isDynamic() {
				return true
			}
		}
		return false
	default:
		return false
	}
}

// ABIValue holds a decoded ABI value alongside its type.
type ABIValue struct {
	Type       ABIType
	Uint256    *big.Int      // for ABIUint256
	Addr       types.Address // for ABIAddress
	Bool       bool          // for ABIBool
	BytesVal   []byte        // for ABIBytes, ABIString, ABIFixedBytes
	StringVal  string        // for ABIString (convenience)
	ArrayElems []ABIValue    // for ABIFixedArray, ABIDynamicArray
	TupleElems []ABIValue    // for ABITuple
}

// Common ABI errors.
var (
	ErrABIShortData      = errors.New("abi: data too short")
	ErrABIInvalidBool    = errors.New("abi: invalid bool value")
	ErrABIInvalidType    = errors.New("abi: unsupported type kind")
	ErrABIOffsetOverflow = errors.New("abi: offset exceeds data length")
)

// ComputeSelector computes the 4-byte function selector from a canonical
// function signature string like "transfer(address,uint256)".
func ComputeSelector(signature string) [4]byte {
	hash := crypto.Keccak256([]byte(signature))
	var sel [4]byte
	copy(sel[:], hash[:4])
	return sel
}

// EncodeFunctionCall encodes a Solidity function call: 4-byte selector
// followed by ABI-encoded arguments using head/tail encoding.
func EncodeFunctionCall(selector [4]byte, args []ABIValue) []byte {
	encoded := abiEncodeVals(args)
	result := make([]byte, 4+len(encoded))
	copy(result[:4], selector[:])
	copy(result[4:], encoded)
	return result
}

// DecodeFunctionResult decodes ABI-encoded return data given the expected
// types. The data should not include a function selector.
func DecodeFunctionResult(data []byte, abiTypes []ABIType) ([]ABIValue, error) {
	return abiDecodeVals(data, abiTypes, 0)
}

// abiEncodeVals ABI-encodes a list of values using head/tail encoding.
func abiEncodeVals(vals []ABIValue) []byte {
	headSize := len(vals) * 32
	var heads, tails []byte

	for _, v := range vals {
		if v.Type.isDynamic() {
			offset := headSize + len(tails)
			heads = append(heads, abiPad32(big.NewInt(int64(offset)).Bytes())...)
			tails = append(tails, abiEncodeOne(v)...)
		} else {
			heads = append(heads, abiEncodeOne(v)...)
		}
	}
	return append(heads, tails...)
}

// abiEncodeOne encodes a single ABI value.
func abiEncodeOne(v ABIValue) []byte {
	switch v.Type.Kind {
	case ABIUint256:
		val := v.Uint256
		if val == nil {
			val = new(big.Int)
		}
		return abiPad32(val.Bytes())

	case ABIAddress:
		return abiPad32(v.Addr[:])

	case ABIBool:
		if v.Bool {
			return abiPad32([]byte{1})
		}
		return make([]byte, 32)

	case ABIFixedBytes:
		out := make([]byte, 32)
		copy(out, v.BytesVal)
		return out

	case ABIBytes:
		return abiEncodeDynB(v.BytesVal)

	case ABIString:
		data := []byte(v.StringVal)
		if len(data) == 0 {
			data = v.BytesVal
		}
		return abiEncodeDynB(data)

	case ABIFixedArray:
		return abiEncodeArr(v.ArrayElems, v.Type.Elem)

	case ABIDynamicArray:
		lenBytes := abiPad32(big.NewInt(int64(len(v.ArrayElems))).Bytes())
		encoded := abiEncodeArr(v.ArrayElems, v.Type.Elem)
		return append(lenBytes, encoded...)

	case ABITuple:
		return abiEncodeVals(v.TupleElems)

	default:
		return make([]byte, 32)
	}
}

// abiEncodeArr encodes array elements using head/tail encoding.
func abiEncodeArr(elems []ABIValue, elemType *ABIType) []byte {
	if elemType == nil || !elemType.isDynamic() {
		var out []byte
		for _, e := range elems {
			out = append(out, abiEncodeOne(e)...)
		}
		return out
	}
	headSize := len(elems) * 32
	var heads, tails []byte
	for _, e := range elems {
		offset := headSize + len(tails)
		heads = append(heads, abiPad32(big.NewInt(int64(offset)).Bytes())...)
		tails = append(tails, abiEncodeOne(e)...)
	}
	return append(heads, tails...)
}

// abiEncodeDynB encodes a dynamic bytes/string value:
// [length (32 bytes)][data padded to 32-byte boundary].
func abiEncodeDynB(data []byte) []byte {
	lenBytes := abiPad32(big.NewInt(int64(len(data))).Bytes())
	padded := make([]byte, ((len(data)+31)/32)*32)
	copy(padded, data)
	return append(lenBytes, padded...)
}

// abiDecodeVals decodes multiple ABI values from data at the given base offset.
func abiDecodeVals(data []byte, abiTypes []ABIType, baseOffset int) ([]ABIValue, error) {
	if len(abiTypes) == 0 {
		return nil, nil
	}
	headPos := baseOffset
	results := make([]ABIValue, len(abiTypes))

	for i, t := range abiTypes {
		if headPos+32 > len(data) {
			return nil, fmt.Errorf("%w: need 32 bytes at offset %d, have %d",
				ErrABIShortData, headPos, len(data))
		}
		if t.isDynamic() {
			off := new(big.Int).SetBytes(data[headPos : headPos+32])
			absOff := baseOffset + int(off.Int64())
			if absOff >= len(data) {
				return nil, fmt.Errorf("%w: offset %d, data length %d",
					ErrABIOffsetOverflow, absOff, len(data))
			}
			val, err := abiDecodeOne(data, t, absOff)
			if err != nil {
				return nil, err
			}
			results[i] = val
		} else {
			val, err := abiDecodeOne(data, t, headPos)
			if err != nil {
				return nil, err
			}
			results[i] = val
		}
		headPos += 32
	}
	return results, nil
}

// abiDecodeOne decodes a single ABI value from data at the given offset.
func abiDecodeOne(data []byte, t ABIType, offset int) (ABIValue, error) {
	switch t.Kind {
	case ABIUint256:
		if offset+32 > len(data) {
			return ABIValue{}, fmt.Errorf("%w: uint256 at offset %d", ErrABIShortData, offset)
		}
		val := new(big.Int).SetBytes(data[offset : offset+32])
		return ABIValue{Type: t, Uint256: val}, nil

	case ABIAddress:
		if offset+32 > len(data) {
			return ABIValue{}, fmt.Errorf("%w: address at offset %d", ErrABIShortData, offset)
		}
		var addr types.Address
		copy(addr[:], data[offset+12:offset+32])
		return ABIValue{Type: t, Addr: addr}, nil

	case ABIBool:
		if offset+32 > len(data) {
			return ABIValue{}, fmt.Errorf("%w: bool at offset %d", ErrABIShortData, offset)
		}
		val := new(big.Int).SetBytes(data[offset : offset+32])
		if val.Uint64() == 0 {
			return ABIValue{Type: t, Bool: false}, nil
		} else if val.Uint64() == 1 {
			return ABIValue{Type: t, Bool: true}, nil
		}
		return ABIValue{}, ErrABIInvalidBool

	case ABIFixedBytes:
		if offset+32 > len(data) {
			return ABIValue{}, fmt.Errorf("%w: bytes%d at offset %d",
				ErrABIShortData, t.Size, offset)
		}
		val := make([]byte, t.Size)
		copy(val, data[offset:offset+t.Size])
		return ABIValue{Type: t, BytesVal: val}, nil

	case ABIBytes:
		return abiDecodeDynB(data, t, offset)

	case ABIString:
		v, err := abiDecodeDynB(data, t, offset)
		if err != nil {
			return ABIValue{}, err
		}
		v.StringVal = string(v.BytesVal)
		return v, nil

	case ABIFixedArray:
		return abiDecodeFixArr(data, t, offset)

	case ABIDynamicArray:
		return abiDecodeDynArr(data, t, offset)

	case ABITuple:
		return abiDecodeTup(data, t, offset)

	default:
		return ABIValue{}, ErrABIInvalidType
	}
}

// abiDecodeDynB decodes a dynamic bytes or string value.
func abiDecodeDynB(data []byte, t ABIType, offset int) (ABIValue, error) {
	if offset+32 > len(data) {
		return ABIValue{}, fmt.Errorf("%w: dynamic bytes length at offset %d",
			ErrABIShortData, offset)
	}
	length := new(big.Int).SetBytes(data[offset : offset+32]).Int64()
	start := offset + 32
	if start+int(length) > len(data) {
		return ABIValue{}, fmt.Errorf("%w: dynamic bytes data at offset %d, length %d",
			ErrABIShortData, start, length)
	}
	val := make([]byte, length)
	copy(val, data[start:start+int(length)])
	return ABIValue{Type: t, BytesVal: val}, nil
}

// abiDecodeFixArr decodes a fixed-size array.
func abiDecodeFixArr(data []byte, t ABIType, offset int) (ABIValue, error) {
	if t.Elem == nil {
		return ABIValue{}, ErrABIInvalidType
	}
	elems := make([]ABIValue, t.Size)
	if t.Elem.isDynamic() {
		for i := 0; i < t.Size; i++ {
			hOff := offset + i*32
			if hOff+32 > len(data) {
				return ABIValue{}, fmt.Errorf("%w: fixed array offset at %d",
					ErrABIShortData, hOff)
			}
			eOff := int(new(big.Int).SetBytes(data[hOff : hOff+32]).Int64())
			val, err := abiDecodeOne(data, *t.Elem, offset+eOff)
			if err != nil {
				return ABIValue{}, err
			}
			elems[i] = val
		}
	} else {
		pos := offset
		for i := 0; i < t.Size; i++ {
			val, err := abiDecodeOne(data, *t.Elem, pos)
			if err != nil {
				return ABIValue{}, err
			}
			elems[i] = val
			pos += 32
		}
	}
	return ABIValue{Type: t, ArrayElems: elems}, nil
}

// abiDecodeDynArr decodes a dynamic-length array.
func abiDecodeDynArr(data []byte, t ABIType, offset int) (ABIValue, error) {
	if t.Elem == nil {
		return ABIValue{}, ErrABIInvalidType
	}
	if offset+32 > len(data) {
		return ABIValue{}, fmt.Errorf("%w: dynamic array length at offset %d",
			ErrABIShortData, offset)
	}
	length := int(new(big.Int).SetBytes(data[offset : offset+32]).Int64())
	start := offset + 32

	elems := make([]ABIValue, length)
	if t.Elem.isDynamic() {
		for i := 0; i < length; i++ {
			hOff := start + i*32
			if hOff+32 > len(data) {
				return ABIValue{}, fmt.Errorf("%w: dynamic array elem offset at %d",
					ErrABIShortData, hOff)
			}
			eOff := int(new(big.Int).SetBytes(data[hOff : hOff+32]).Int64())
			val, err := abiDecodeOne(data, *t.Elem, start+eOff)
			if err != nil {
				return ABIValue{}, err
			}
			elems[i] = val
		}
	} else {
		pos := start
		for i := 0; i < length; i++ {
			val, err := abiDecodeOne(data, *t.Elem, pos)
			if err != nil {
				return ABIValue{}, err
			}
			elems[i] = val
			pos += 32
		}
	}
	return ABIValue{Type: t, ArrayElems: elems}, nil
}

// abiDecodeTup decodes a tuple (struct) value.
func abiDecodeTup(data []byte, t ABIType, offset int) (ABIValue, error) {
	vals, err := abiDecodeVals(data, t.Fields, offset)
	if err != nil {
		return ABIValue{}, err
	}
	return ABIValue{Type: t, TupleElems: vals}, nil
}

// abiPad32 left-pads a byte slice to 32 bytes with zero bytes.
func abiPad32(b []byte) []byte {
	if len(b) >= 32 {
		return b[len(b)-32:]
	}
	out := make([]byte, 32)
	copy(out[32-len(b):], b)
	return out
}

// Uint256ToBytes converts a uint64 to a big-endian 32-byte representation.
func Uint256ToBytes(v uint64) []byte {
	out := make([]byte, 32)
	binary.BigEndian.PutUint64(out[24:], v)
	return out
}
