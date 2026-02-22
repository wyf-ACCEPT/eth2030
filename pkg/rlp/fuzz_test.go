package rlp

import (
	"testing"
)

func FuzzDecode(f *testing.F) {
	// Seed with valid RLP encodings.
	f.Add([]byte{0x80})                                                 // empty string
	f.Add([]byte{0x83, 0x64, 0x6f, 0x67})                               // "dog"
	f.Add([]byte{0x01})                                                 // uint(1)
	f.Add([]byte{0x7f})                                                 // uint(127)
	f.Add([]byte{0x82, 0x04, 0x00})                                     // uint(1024)
	f.Add([]byte{0xc0})                                                 // empty list
	f.Add([]byte{0xc8, 0x83, 0x63, 0x61, 0x74, 0x83, 0x64, 0x6f, 0x67}) // ["cat","dog"]
	f.Add([]byte{0xc5, 0x83, 0x63, 0x61, 0x74, 0x05})                   // struct{Name:"cat", Age:5}

	f.Fuzz(func(t *testing.T, data []byte) {
		// Decode as string: should not panic.
		var s string
		_ = DecodeBytes(data, &s)

		// Decode as uint64: should not panic.
		var u uint64
		_ = DecodeBytes(data, &u)

		// Decode as []byte: should not panic.
		var b []byte
		_ = DecodeBytes(data, &b)

		// Decode as []string: should not panic.
		var ss []string
		_ = DecodeBytes(data, &ss)
	})
}
