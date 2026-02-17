package rlp

import (
	"io"
	"math/big"
	"reflect"
)

// Encode writes the RLP encoding of val to w.
// val must be a supported type: bool, uint8/16/32/64, *big.Int,
// []byte, string, slice/array, or struct (exported fields only).
func Encode(w io.Writer, val interface{}) error {
	b, err := EncodeToBytes(val)
	if err != nil {
		return err
	}
	_, err = w.Write(b)
	return err
}

// EncodeToBytes returns the RLP encoding of val.
func EncodeToBytes(val interface{}) ([]byte, error) {
	return encodeValue(reflect.ValueOf(val))
}

func encodeValue(v reflect.Value) ([]byte, error) {
	// Handle interface values by unwrapping.
	for v.Kind() == reflect.Interface || v.Kind() == reflect.Ptr {
		if v.IsNil() {
			// nil pointer/interface encodes as empty string.
			return []byte{0x80}, nil
		}
		v = v.Elem()
	}

	// Check for *big.Int by seeing if the original (or unwrapped) value is a big.Int.
	if v.Type() == reflect.TypeOf(big.Int{}) {
		bi := v.Addr().Interface().(*big.Int)
		return encodeBigInt(bi), nil
	}

	switch v.Kind() {
	case reflect.Bool:
		if v.Bool() {
			return []byte{0x01}, nil
		}
		return []byte{0x80}, nil

	case reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uint:
		return encodeUint(v.Uint()), nil

	case reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Int:
		u := uint64(v.Int())
		return encodeUint(u), nil

	case reflect.String:
		return encodeString([]byte(v.String())), nil

	case reflect.Slice:
		if v.Type().Elem().Kind() == reflect.Uint8 {
			// []byte is encoded as an RLP string.
			return encodeString(v.Bytes()), nil
		}
		return encodeList(v)

	case reflect.Array:
		if v.Type().Elem().Kind() == reflect.Uint8 {
			// [N]byte is encoded as an RLP string.
			b := make([]byte, v.Len())
			for i := 0; i < v.Len(); i++ {
				b[i] = byte(v.Index(i).Uint())
			}
			return encodeString(b), nil
		}
		return encodeList(v)

	case reflect.Struct:
		return encodeStruct(v)

	case reflect.Invalid:
		return []byte{0x80}, nil

	default:
		return nil, ErrValueTooLarge
	}
}

func encodeUint(u uint64) []byte {
	if u == 0 {
		return []byte{0x80}
	}
	if u < 128 {
		return []byte{byte(u)}
	}
	b := putUintBigEndian(u)
	return encodeString(b)
}

func encodeBigInt(i *big.Int) []byte {
	if i.Sign() == 0 {
		return []byte{0x80}
	}
	b := i.Bytes() // big-endian, no leading zeros
	return encodeString(b)
}

func encodeString(data []byte) []byte {
	n := len(data)
	if n == 1 && data[0] <= 0x7f {
		return data
	}
	if n <= 55 {
		buf := make([]byte, 1+n)
		buf[0] = 0x80 + byte(n)
		copy(buf[1:], data)
		return buf
	}
	lenBytes := putUintBigEndian(uint64(n))
	buf := make([]byte, 1+len(lenBytes)+n)
	buf[0] = 0xb7 + byte(len(lenBytes))
	copy(buf[1:], lenBytes)
	copy(buf[1+len(lenBytes):], data)
	return buf
}

func encodeList(v reflect.Value) ([]byte, error) {
	var payload []byte
	for i := 0; i < v.Len(); i++ {
		enc, err := encodeValue(v.Index(i))
		if err != nil {
			return nil, err
		}
		payload = append(payload, enc...)
	}
	return wrapList(payload), nil
}

func encodeStruct(v reflect.Value) ([]byte, error) {
	var payload []byte
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		enc, err := encodeValue(v.Field(i))
		if err != nil {
			return nil, err
		}
		payload = append(payload, enc...)
	}
	return wrapList(payload), nil
}

// WrapList wraps an already-encoded RLP payload in a list header.
func WrapList(payload []byte) []byte {
	return wrapList(payload)
}

func wrapList(payload []byte) []byte {
	n := len(payload)
	if n <= 55 {
		buf := make([]byte, 1+n)
		buf[0] = 0xc0 + byte(n)
		copy(buf[1:], payload)
		return buf
	}
	lenBytes := putUintBigEndian(uint64(n))
	buf := make([]byte, 1+len(lenBytes)+n)
	buf[0] = 0xf7 + byte(len(lenBytes))
	copy(buf[1:], lenBytes)
	copy(buf[1+len(lenBytes):], payload)
	return buf
}

// putUintBigEndian encodes u as big-endian with no leading zeros.
func putUintBigEndian(u uint64) []byte {
	switch {
	case u < (1 << 8):
		return []byte{byte(u)}
	case u < (1 << 16):
		return []byte{byte(u >> 8), byte(u)}
	case u < (1 << 24):
		return []byte{byte(u >> 16), byte(u >> 8), byte(u)}
	case u < (1 << 32):
		return []byte{byte(u >> 24), byte(u >> 16), byte(u >> 8), byte(u)}
	case u < (1 << 40):
		return []byte{byte(u >> 32), byte(u >> 24), byte(u >> 16), byte(u >> 8), byte(u)}
	case u < (1 << 48):
		return []byte{byte(u >> 40), byte(u >> 32), byte(u >> 24), byte(u >> 16), byte(u >> 8), byte(u)}
	case u < (1 << 56):
		return []byte{byte(u >> 48), byte(u >> 40), byte(u >> 32), byte(u >> 24), byte(u >> 16), byte(u >> 8), byte(u)}
	default:
		return []byte{byte(u >> 56), byte(u >> 48), byte(u >> 40), byte(u >> 32), byte(u >> 24), byte(u >> 16), byte(u >> 8), byte(u)}
	}
}
