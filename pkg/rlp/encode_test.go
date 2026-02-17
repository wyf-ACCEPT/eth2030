package rlp

import (
	"bytes"
	"math/big"
	"testing"
)

func TestEncodeEmptyString(t *testing.T) {
	got, err := EncodeToBytes("")
	if err != nil {
		t.Fatal(err)
	}
	want := []byte{0x80}
	if !bytes.Equal(got, want) {
		t.Fatalf("empty string: got %x, want %x", got, want)
	}
}

func TestEncodeDog(t *testing.T) {
	got, err := EncodeToBytes("dog")
	if err != nil {
		t.Fatal(err)
	}
	want := []byte{0x83, 0x64, 0x6f, 0x67}
	if !bytes.Equal(got, want) {
		t.Fatalf("\"dog\": got %x, want %x", got, want)
	}
}

func TestEncodeLongString(t *testing.T) {
	s := "Lorem ipsum dolor sit amet, consectetur adipisicing elit"
	got, err := EncodeToBytes(s)
	if err != nil {
		t.Fatal(err)
	}
	// len(s) = 56, which is >55, so: [0xb8, 0x38, ...data]
	if got[0] != 0xb8 {
		t.Fatalf("long string prefix: got %x, want 0xb8", got[0])
	}
	if got[1] != 0x38 {
		t.Fatalf("long string length: got %x, want 0x38", got[1])
	}
	if !bytes.Equal(got[2:], []byte(s)) {
		t.Fatal("long string data mismatch")
	}
}

func TestEncodeUint(t *testing.T) {
	tests := []struct {
		name string
		val  interface{}
		want []byte
	}{
		{"uint(0)", uint64(0), []byte{0x80}},
		{"uint(15)", uint64(15), []byte{0x0f}},
		{"uint(127)", uint64(127), []byte{0x7f}},
		{"uint(128)", uint64(128), []byte{0x81, 0x80}},
		{"uint(1024)", uint64(1024), []byte{0x82, 0x04, 0x00}},
		{"uint(256)", uint64(256), []byte{0x82, 0x01, 0x00}},
		{"uint(1)", uint64(1), []byte{0x01}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := EncodeToBytes(tt.val)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, tt.want) {
				t.Fatalf("%s: got %x, want %x", tt.name, got, tt.want)
			}
		})
	}
}

func TestEncodeBool(t *testing.T) {
	tests := []struct {
		name string
		val  bool
		want []byte
	}{
		{"false", false, []byte{0x80}},
		{"true", true, []byte{0x01}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := EncodeToBytes(tt.val)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, tt.want) {
				t.Fatalf("%s: got %x, want %x", tt.name, got, tt.want)
			}
		})
	}
}

func TestEncodeEmptyList(t *testing.T) {
	got, err := EncodeToBytes([]interface{}{})
	if err != nil {
		t.Fatal(err)
	}
	want := []byte{0xc0}
	if !bytes.Equal(got, want) {
		t.Fatalf("empty list: got %x, want %x", got, want)
	}
}

func TestEncodeCatDog(t *testing.T) {
	got, err := EncodeToBytes([]string{"cat", "dog"})
	if err != nil {
		t.Fatal(err)
	}
	want := []byte{0xc8, 0x83, 0x63, 0x61, 0x74, 0x83, 0x64, 0x6f, 0x67}
	if !bytes.Equal(got, want) {
		t.Fatalf("[\"cat\",\"dog\"]: got %x, want %x", got, want)
	}
}

func TestEncodeBytes(t *testing.T) {
	tests := []struct {
		name string
		val  []byte
		want []byte
	}{
		{"empty bytes", []byte{}, []byte{0x80}},
		{"single byte 0x00", []byte{0x00}, []byte{0x00}},
		{"single byte 0x7f", []byte{0x7f}, []byte{0x7f}},
		{"single byte 0x80", []byte{0x80}, []byte{0x81, 0x80}},
		{"three bytes", []byte{0x01, 0x02, 0x03}, []byte{0x83, 0x01, 0x02, 0x03}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := EncodeToBytes(tt.val)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, tt.want) {
				t.Fatalf("%s: got %x, want %x", tt.name, got, tt.want)
			}
		})
	}
}

func TestEncodeBigInt(t *testing.T) {
	tests := []struct {
		name string
		val  *big.Int
		want []byte
	}{
		{"big.Int(0)", big.NewInt(0), []byte{0x80}},
		{"big.Int(1)", big.NewInt(1), []byte{0x01}},
		{"big.Int(127)", big.NewInt(127), []byte{0x7f}},
		{"big.Int(128)", big.NewInt(128), []byte{0x81, 0x80}},
		{"big.Int(256)", big.NewInt(256), []byte{0x82, 0x01, 0x00}},
		{"big.Int(1024)", big.NewInt(1024), []byte{0x82, 0x04, 0x00}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := EncodeToBytes(tt.val)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, tt.want) {
				t.Fatalf("%s: got %x, want %x", tt.name, got, tt.want)
			}
		})
	}
}

func TestEncodeStruct(t *testing.T) {
	type TestStruct struct {
		Name string
		Age  uint64
	}
	s := TestStruct{Name: "cat", Age: 5}
	got, err := EncodeToBytes(s)
	if err != nil {
		t.Fatal(err)
	}
	// List: [string "cat" = 83 63 61 74, uint 5 = 05]
	// payload = 83 63 61 74 05 (5 bytes)
	// list prefix = c0 + 5 = c5
	want := []byte{0xc5, 0x83, 0x63, 0x61, 0x74, 0x05}
	if !bytes.Equal(got, want) {
		t.Fatalf("struct: got %x, want %x", got, want)
	}
}

func TestEncodeNestedList(t *testing.T) {
	// Encode a [][]string
	val := [][]string{{"cat"}, {"dog"}}
	got, err := EncodeToBytes(val)
	if err != nil {
		t.Fatal(err)
	}
	// inner1: [0xc4, 0x83, 0x63, 0x61, 0x74] (list of "cat")
	// inner2: [0xc4, 0x83, 0x64, 0x6f, 0x67] (list of "dog")
	// outer payload = 10 bytes
	// outer prefix = 0xc0 + 10 = 0xca
	want := []byte{0xca, 0xc4, 0x83, 0x63, 0x61, 0x74, 0xc4, 0x83, 0x64, 0x6f, 0x67}
	if !bytes.Equal(got, want) {
		t.Fatalf("nested list: got %x, want %x", got, want)
	}
}

func TestEncodeToWriter(t *testing.T) {
	var buf bytes.Buffer
	err := Encode(&buf, "dog")
	if err != nil {
		t.Fatal(err)
	}
	want := []byte{0x83, 0x64, 0x6f, 0x67}
	if !bytes.Equal(buf.Bytes(), want) {
		t.Fatalf("Encode to writer: got %x, want %x", buf.Bytes(), want)
	}
}

func TestEncodeSingleByte(t *testing.T) {
	// A single byte in [0x00, 0x7f] is its own RLP encoding.
	got, err := EncodeToBytes([]byte{0x42})
	if err != nil {
		t.Fatal(err)
	}
	want := []byte{0x42}
	if !bytes.Equal(got, want) {
		t.Fatalf("single byte: got %x, want %x", got, want)
	}
}
