package ssz

import (
	"encoding/binary"
	"errors"
	"testing"
)

// --- test variant codecs for uint64 and []byte ---

func testUint64Codec() *UnionVariantCodec {
	return &UnionVariantCodec{
		Selector:  1,
		Name:      "uint64",
		FixedSize: 8,
		Encode: func(value interface{}) ([]byte, error) {
			v, ok := value.(uint64)
			if !ok {
				return nil, errors.New("expected uint64")
			}
			return MarshalUint64(v), nil
		},
		Decode: func(data []byte) (interface{}, error) {
			v, err := UnmarshalUint64(data)
			if err != nil {
				return nil, err
			}
			return v, nil
		},
		HashTreeRootFn: func(value interface{}) ([32]byte, error) {
			v, ok := value.(uint64)
			if !ok {
				return [32]byte{}, errors.New("expected uint64")
			}
			return HashTreeRootUint64(v), nil
		},
	}
}

func testBytesCodec() *UnionVariantCodec {
	return &UnionVariantCodec{
		Selector:  2,
		Name:      "bytes",
		FixedSize: 0, // variable-size
		Encode: func(value interface{}) ([]byte, error) {
			v, ok := value.([]byte)
			if !ok {
				return nil, errors.New("expected []byte")
			}
			return MarshalByteList(v), nil
		},
		Decode: func(data []byte) (interface{}, error) {
			cp := make([]byte, len(data))
			copy(cp, data)
			return cp, nil
		},
		HashTreeRootFn: func(value interface{}) ([32]byte, error) {
			v, ok := value.([]byte)
			if !ok {
				return [32]byte{}, errors.New("expected []byte")
			}
			return HashTreeRootByteList(v, 1024), nil
		},
	}
}

func testNoneCodec() *UnionVariantCodec {
	return &UnionVariantCodec{
		Selector:  NoneSelector,
		Name:      "none",
		FixedSize: 0,
		Encode: func(value interface{}) ([]byte, error) {
			return nil, nil
		},
		Decode: func(data []byte) (interface{}, error) {
			return nil, nil
		},
		HashTreeRootFn: func(value interface{}) ([32]byte, error) {
			return [32]byte{}, nil
		},
	}
}

func setupTestRegistry(t *testing.T) *UnionTypeRegistry {
	t.Helper()
	reg := NewUnionTypeRegistry()
	if err := reg.Register(testNoneCodec()); err != nil {
		t.Fatalf("Register none: %v", err)
	}
	if err := reg.Register(testUint64Codec()); err != nil {
		t.Fatalf("Register uint64: %v", err)
	}
	if err := reg.Register(testBytesCodec()); err != nil {
		t.Fatalf("Register bytes: %v", err)
	}
	return reg
}

func TestUnionCodecRegistryCount(t *testing.T) {
	reg := setupTestRegistry(t)
	if reg.Count() != 3 {
		t.Fatalf("expected 3 variants, got %d", reg.Count())
	}
}

func TestUnionCodecRegistrySelectors(t *testing.T) {
	reg := setupTestRegistry(t)
	sels := reg.Selectors()
	if len(sels) != 3 {
		t.Fatalf("expected 3 selectors, got %d", len(sels))
	}
	if sels[0] != 0 || sels[1] != 1 || sels[2] != 2 {
		t.Fatalf("unexpected selectors: %v", sels)
	}
}

func TestUnionCodecRegistryDuplicate(t *testing.T) {
	reg := NewUnionTypeRegistry()
	reg.Register(testUint64Codec())
	err := reg.Register(testUint64Codec())
	if err == nil || !errors.Is(err, ErrUnionSelectorDuplicate) {
		t.Fatalf("expected ErrUnionSelectorDuplicate, got %v", err)
	}
}

func TestUnionCodecRegistryNilCodec(t *testing.T) {
	reg := NewUnionTypeRegistry()
	err := reg.Register(nil)
	if err != ErrUnionNilCodec {
		t.Fatalf("expected ErrUnionNilCodec, got %v", err)
	}
}

func TestUnionCodecLookup(t *testing.T) {
	reg := setupTestRegistry(t)
	codec, err := reg.Lookup(1)
	if err != nil {
		t.Fatalf("Lookup(1): %v", err)
	}
	if codec.Name != "uint64" {
		t.Fatalf("expected 'uint64', got %q", codec.Name)
	}
}

func TestUnionCodecLookupByName(t *testing.T) {
	reg := setupTestRegistry(t)
	codec, err := reg.LookupByName("bytes")
	if err != nil {
		t.Fatalf("LookupByName: %v", err)
	}
	if codec.Selector != 2 {
		t.Fatalf("expected selector 2, got %d", codec.Selector)
	}
}

func TestUnionCodecLookupUnknown(t *testing.T) {
	reg := setupTestRegistry(t)
	_, err := reg.Lookup(99)
	if err == nil || !errors.Is(err, ErrUnionSelectorUnknown) {
		t.Fatalf("expected ErrUnionSelectorUnknown, got %v", err)
	}
}

func TestUnionCodecLookupByNameUnknown(t *testing.T) {
	reg := setupTestRegistry(t)
	_, err := reg.LookupByName("missing")
	if err == nil {
		t.Fatal("expected error for unknown name")
	}
}

func TestUnionCodecEncodeDecodeUint64(t *testing.T) {
	reg := setupTestRegistry(t)
	uc := NewUnionCodec(reg)

	uv := &UnionValue{Selector: 1, Value: uint64(12345)}
	encoded, err := uc.Encode(uv)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	// 1 byte selector + 8 bytes uint64.
	if len(encoded) != 9 {
		t.Fatalf("expected 9 bytes, got %d", len(encoded))
	}
	if encoded[0] != 1 {
		t.Fatalf("expected selector 1, got %d", encoded[0])
	}
	val := binary.LittleEndian.Uint64(encoded[1:])
	if val != 12345 {
		t.Fatalf("expected 12345, got %d", val)
	}

	decoded, err := uc.Decode(encoded)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if decoded.Selector != 1 {
		t.Fatalf("decoded selector: expected 1, got %d", decoded.Selector)
	}
	if decoded.Value.(uint64) != 12345 {
		t.Fatalf("decoded value: expected 12345, got %v", decoded.Value)
	}
}

func TestUnionCodecEncodeDecodeBytes(t *testing.T) {
	reg := setupTestRegistry(t)
	uc := NewUnionCodec(reg)

	payload := []byte("hello world")
	uv := &UnionValue{Selector: 2, Value: payload}
	encoded, err := uc.Encode(uv)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if len(encoded) != 1+len(payload) {
		t.Fatalf("expected %d bytes, got %d", 1+len(payload), len(encoded))
	}

	decoded, err := uc.Decode(encoded)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	decodedBytes := decoded.Value.([]byte)
	if string(decodedBytes) != "hello world" {
		t.Fatalf("decoded value mismatch: %q", decodedBytes)
	}
}

func TestUnionCodecEncodeNilValue(t *testing.T) {
	reg := setupTestRegistry(t)
	uc := NewUnionCodec(reg)

	_, err := uc.Encode(nil)
	if err != ErrUnionNilValue {
		t.Fatalf("expected ErrUnionNilValue, got %v", err)
	}
}

func TestUnionCodecDecodeEmpty(t *testing.T) {
	reg := setupTestRegistry(t)
	uc := NewUnionCodec(reg)

	_, err := uc.Decode(nil)
	if err != ErrUnionDataTooShort {
		t.Fatalf("expected ErrUnionDataTooShort, got %v", err)
	}
}

func TestUnionCodecDecodeUnknownSelector(t *testing.T) {
	reg := setupTestRegistry(t)
	uc := NewUnionCodec(reg)

	_, err := uc.Decode([]byte{99, 0, 0, 0, 0})
	if err == nil || !errors.Is(err, ErrUnionSelectorUnknown) {
		t.Fatalf("expected ErrUnionSelectorUnknown, got %v", err)
	}
}

func TestUnionCodecHashTreeRoot(t *testing.T) {
	reg := setupTestRegistry(t)
	uc := NewUnionCodec(reg)

	uv := &UnionValue{Selector: 1, Value: uint64(42)}
	root, err := uc.HashTreeRoot(uv)
	if err != nil {
		t.Fatalf("HashTreeRoot: %v", err)
	}

	// Verify manually: HashTreeRootUnion(uint64_root, 1).
	valRoot := HashTreeRootUint64(42)
	expected := HashTreeRootUnion(valRoot, 1)
	if root != expected {
		t.Fatalf("hash tree root mismatch:\n  got  %x\n  want %x", root, expected)
	}
}

func TestUnionCodecHashTreeRootNone(t *testing.T) {
	reg := setupTestRegistry(t)
	uc := NewUnionCodec(reg)

	uv := NewNoneValue()
	root, err := uc.HashTreeRoot(uv)
	if err != nil {
		t.Fatalf("HashTreeRoot(None): %v", err)
	}
	expected := HashTreeRootUnion([32]byte{}, NoneSelector)
	if root != expected {
		t.Fatalf("none root mismatch:\n  got  %x\n  want %x", root, expected)
	}
}

func TestUnionCodecHashTreeRootNilValue(t *testing.T) {
	reg := setupTestRegistry(t)
	uc := NewUnionCodec(reg)

	_, err := uc.HashTreeRoot(nil)
	if err != ErrUnionNilValue {
		t.Fatalf("expected ErrUnionNilValue, got %v", err)
	}
}

func TestUnionCodecSizeSSZ(t *testing.T) {
	reg := setupTestRegistry(t)
	uc := NewUnionCodec(reg)

	// Fixed-size uint64 variant.
	size, err := uc.SizeSSZ(&UnionValue{Selector: 1, Value: uint64(0)})
	if err != nil {
		t.Fatalf("SizeSSZ(uint64): %v", err)
	}
	if size != 9 { // 1 + 8
		t.Fatalf("expected size 9, got %d", size)
	}

	// Variable-size bytes variant.
	payload := []byte("test data")
	size, err = uc.SizeSSZ(&UnionValue{Selector: 2, Value: payload})
	if err != nil {
		t.Fatalf("SizeSSZ(bytes): %v", err)
	}
	if size != 1+len(payload) {
		t.Fatalf("expected size %d, got %d", 1+len(payload), size)
	}
}

func TestUnionCodecSizeSSZNilValue(t *testing.T) {
	reg := setupTestRegistry(t)
	uc := NewUnionCodec(reg)
	_, err := uc.SizeSSZ(nil)
	if err != ErrUnionNilValue {
		t.Fatalf("expected ErrUnionNilValue, got %v", err)
	}
}

func TestUnionCodecValidate(t *testing.T) {
	reg := setupTestRegistry(t)
	uc := NewUnionCodec(reg)

	// Valid.
	err := uc.Validate(&UnionValue{Selector: 1, Value: uint64(42)})
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}

	// Invalid selector.
	err = uc.Validate(&UnionValue{Selector: 99, Value: nil})
	if err == nil {
		t.Fatal("expected error for invalid selector")
	}

	// Nil value.
	err = uc.Validate(nil)
	if err != ErrUnionNilValue {
		t.Fatalf("expected ErrUnionNilValue, got %v", err)
	}
}

func TestUnionCodecRoundTrip(t *testing.T) {
	reg := setupTestRegistry(t)
	uc := NewUnionCodec(reg)

	// Round-trip uint64.
	uv := &UnionValue{Selector: 1, Value: uint64(99999)}
	decoded, err := uc.RoundTrip(uv)
	if err != nil {
		t.Fatalf("RoundTrip(uint64): %v", err)
	}
	if decoded.Selector != 1 {
		t.Fatalf("round-trip selector: expected 1, got %d", decoded.Selector)
	}
	if decoded.Value.(uint64) != 99999 {
		t.Fatalf("round-trip value: expected 99999, got %v", decoded.Value)
	}

	// Round-trip bytes.
	payload := []byte{0xde, 0xad, 0xbe, 0xef}
	uv = &UnionValue{Selector: 2, Value: payload}
	decoded, err = uc.RoundTrip(uv)
	if err != nil {
		t.Fatalf("RoundTrip(bytes): %v", err)
	}
	decodedBytes := decoded.Value.([]byte)
	if len(decodedBytes) != 4 {
		t.Fatalf("round-trip bytes length: expected 4, got %d", len(decodedBytes))
	}
	for i, b := range payload {
		if decodedBytes[i] != b {
			t.Fatalf("round-trip byte %d: expected %02x, got %02x", i, b, decodedBytes[i])
		}
	}
}

func TestUnionCodecNoneVariant(t *testing.T) {
	reg := setupTestRegistry(t)
	uc := NewUnionCodec(reg)

	none := NewNoneValue()
	if !IsNone(none) {
		t.Fatal("expected IsNone to return true for None value")
	}

	encoded, err := uc.Encode(none)
	if err != nil {
		t.Fatalf("Encode(None): %v", err)
	}
	// Selector 0 + no value bytes.
	if len(encoded) != 1 {
		t.Fatalf("expected 1 byte for None, got %d", len(encoded))
	}
	if encoded[0] != 0 {
		t.Fatalf("expected selector 0, got %d", encoded[0])
	}

	decoded, err := uc.Decode(encoded)
	if err != nil {
		t.Fatalf("Decode(None): %v", err)
	}
	if decoded.Selector != 0 {
		t.Fatalf("decoded selector: expected 0, got %d", decoded.Selector)
	}
}

func TestIsNoneEdgeCases(t *testing.T) {
	if IsNone(nil) {
		t.Fatal("IsNone(nil) should be false")
	}
	if IsNone(&UnionValue{Selector: 1, Value: nil}) {
		t.Fatal("IsNone with selector 1 should be false")
	}
	if IsNone(&UnionValue{Selector: 0, Value: "something"}) {
		t.Fatal("IsNone with non-nil value should be false")
	}
}

func TestUnionCodecEncodeUnknownSelector(t *testing.T) {
	reg := setupTestRegistry(t)
	uc := NewUnionCodec(reg)

	_, err := uc.Encode(&UnionValue{Selector: 77, Value: nil})
	if err == nil {
		t.Fatal("expected error for unknown selector")
	}
}

func TestUnionCodecHashTreeRootConsistency(t *testing.T) {
	// Verify that the codec's hash tree root matches direct computation.
	reg := setupTestRegistry(t)
	uc := NewUnionCodec(reg)

	for _, val := range []uint64{0, 1, 255, 65535, 1 << 40} {
		uv := &UnionValue{Selector: 1, Value: val}
		root, err := uc.HashTreeRoot(uv)
		if err != nil {
			t.Fatalf("HashTreeRoot(%d): %v", val, err)
		}
		expected := HashTreeRootUnion(HashTreeRootUint64(val), 1)
		if root != expected {
			t.Fatalf("consistency check failed for %d", val)
		}
	}
}

func TestUnionCodecMultipleEncodeDecode(t *testing.T) {
	reg := setupTestRegistry(t)
	uc := NewUnionCodec(reg)

	// Encode and decode multiple values in sequence.
	values := []struct {
		sel byte
		val interface{}
	}{
		{1, uint64(0)},
		{1, uint64(18446744073709551615)}, // max uint64
		{2, []byte("test")},
		{2, []byte{}},
		{0, nil},
	}

	for i, tc := range values {
		uv := &UnionValue{Selector: tc.sel, Value: tc.val}
		encoded, err := uc.Encode(uv)
		if err != nil {
			t.Fatalf("case %d: Encode: %v", i, err)
		}
		decoded, err := uc.Decode(encoded)
		if err != nil {
			t.Fatalf("case %d: Decode: %v", i, err)
		}
		if decoded.Selector != tc.sel {
			t.Fatalf("case %d: selector mismatch: %d vs %d", i, decoded.Selector, tc.sel)
		}
	}
}
