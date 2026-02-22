package vm

import (
	"bytes"
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

func TestComputeSelector(t *testing.T) {
	// keccak256("transfer(address,uint256)") = 0xa9059cbb...
	sel := ComputeSelector("transfer(address,uint256)")
	expected := [4]byte{0xa9, 0x05, 0x9c, 0xbb}
	if sel != expected {
		t.Fatalf("selector mismatch: got %x, want %x", sel, expected)
	}
}

func TestComputeSelectorBalanceOf(t *testing.T) {
	// keccak256("balanceOf(address)") = 0x70a08231...
	sel := ComputeSelector("balanceOf(address)")
	expected := [4]byte{0x70, 0xa0, 0x82, 0x31}
	if sel != expected {
		t.Fatalf("selector mismatch: got %x, want %x", sel, expected)
	}
}

func TestEncodeDecodeUint256(t *testing.T) {
	val := big.NewInt(42)
	abiType := ABIType{Kind: ABIUint256}
	v := ABIValue{Type: abiType, Uint256: val}

	sel := [4]byte{0x01, 0x02, 0x03, 0x04}
	encoded := EncodeFunctionCall(sel, []ABIValue{v})

	// Should be 4 + 32 = 36 bytes.
	if len(encoded) != 36 {
		t.Fatalf("encoded length: got %d, want 36", len(encoded))
	}
	if !bytes.Equal(encoded[:4], sel[:]) {
		t.Fatalf("selector mismatch in encoded data")
	}

	results, err := DecodeFunctionResult(encoded[4:], []ABIType{abiType})
	if err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("result count: got %d, want 1", len(results))
	}
	if results[0].Uint256.Cmp(val) != 0 {
		t.Fatalf("decoded value: got %s, want %s", results[0].Uint256, val)
	}
}

func TestEncodeDecodeAddress(t *testing.T) {
	addr := types.HexToAddress("0xdead000000000000000000000000000000000001")
	abiType := ABIType{Kind: ABIAddress}
	v := ABIValue{Type: abiType, Addr: addr}

	sel := [4]byte{}
	encoded := EncodeFunctionCall(sel, []ABIValue{v})

	results, err := DecodeFunctionResult(encoded[4:], []ABIType{abiType})
	if err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if results[0].Addr != addr {
		t.Fatalf("decoded address: got %s, want %s", results[0].Addr.Hex(), addr.Hex())
	}
}

func TestEncodeDecodeBool(t *testing.T) {
	abiType := ABIType{Kind: ABIBool}

	for _, boolVal := range []bool{true, false} {
		v := ABIValue{Type: abiType, Bool: boolVal}
		sel := [4]byte{}
		encoded := EncodeFunctionCall(sel, []ABIValue{v})

		results, err := DecodeFunctionResult(encoded[4:], []ABIType{abiType})
		if err != nil {
			t.Fatalf("decode error for %v: %v", boolVal, err)
		}
		if results[0].Bool != boolVal {
			t.Fatalf("decoded bool: got %v, want %v", results[0].Bool, boolVal)
		}
	}
}

func TestEncodeDecodeDynamicBytes(t *testing.T) {
	data := []byte("hello world")
	abiType := ABIType{Kind: ABIBytes}
	v := ABIValue{Type: abiType, BytesVal: data}

	sel := [4]byte{0xaa, 0xbb, 0xcc, 0xdd}
	encoded := EncodeFunctionCall(sel, []ABIValue{v})

	results, err := DecodeFunctionResult(encoded[4:], []ABIType{abiType})
	if err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if !bytes.Equal(results[0].BytesVal, data) {
		t.Fatalf("decoded bytes: got %x, want %x", results[0].BytesVal, data)
	}
}

func TestEncodeDecodeString(t *testing.T) {
	str := "Hello, Ethereum!"
	abiType := ABIType{Kind: ABIString}
	v := ABIValue{Type: abiType, StringVal: str}

	sel := [4]byte{}
	encoded := EncodeFunctionCall(sel, []ABIValue{v})

	results, err := DecodeFunctionResult(encoded[4:], []ABIType{abiType})
	if err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if results[0].StringVal != str {
		t.Fatalf("decoded string: got %q, want %q", results[0].StringVal, str)
	}
}

func TestEncodeDecodeMixedStaticDynamic(t *testing.T) {
	addrType := ABIType{Kind: ABIAddress}
	uint256Type := ABIType{Kind: ABIUint256}
	bytesType := ABIType{Kind: ABIBytes}

	addr := types.HexToAddress("0x1234567890abcdef1234567890abcdef12345678")
	amount := big.NewInt(1000000)
	payload := []byte{0xde, 0xad, 0xbe, 0xef}

	args := []ABIValue{
		{Type: addrType, Addr: addr},
		{Type: uint256Type, Uint256: amount},
		{Type: bytesType, BytesVal: payload},
	}

	sel := ComputeSelector("transfer(address,uint256,bytes)")
	encoded := EncodeFunctionCall(sel, args)

	results, err := DecodeFunctionResult(encoded[4:], []ABIType{addrType, uint256Type, bytesType})
	if err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("result count: got %d, want 3", len(results))
	}
	if results[0].Addr != addr {
		t.Fatalf("address mismatch")
	}
	if results[1].Uint256.Cmp(amount) != 0 {
		t.Fatalf("uint256 mismatch")
	}
	if !bytes.Equal(results[2].BytesVal, payload) {
		t.Fatalf("bytes mismatch")
	}
}

func TestEncodeDecodeFixedArray(t *testing.T) {
	elemType := ABIType{Kind: ABIUint256}
	arrType := ABIType{Kind: ABIFixedArray, Size: 3, Elem: &elemType}

	elems := []ABIValue{
		{Type: elemType, Uint256: big.NewInt(10)},
		{Type: elemType, Uint256: big.NewInt(20)},
		{Type: elemType, Uint256: big.NewInt(30)},
	}
	v := ABIValue{Type: arrType, ArrayElems: elems}

	sel := [4]byte{}
	encoded := EncodeFunctionCall(sel, []ABIValue{v})

	results, err := DecodeFunctionResult(encoded[4:], []ABIType{arrType})
	if err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if len(results[0].ArrayElems) != 3 {
		t.Fatalf("array length: got %d, want 3", len(results[0].ArrayElems))
	}
	for i, want := range []int64{10, 20, 30} {
		if results[0].ArrayElems[i].Uint256.Int64() != want {
			t.Fatalf("elem[%d]: got %d, want %d", i, results[0].ArrayElems[i].Uint256.Int64(), want)
		}
	}
}

func TestEncodeDecodeDynamicArray(t *testing.T) {
	elemType := ABIType{Kind: ABIUint256}
	arrType := ABIType{Kind: ABIDynamicArray, Elem: &elemType}

	elems := []ABIValue{
		{Type: elemType, Uint256: big.NewInt(100)},
		{Type: elemType, Uint256: big.NewInt(200)},
	}
	v := ABIValue{Type: arrType, ArrayElems: elems}

	sel := [4]byte{}
	encoded := EncodeFunctionCall(sel, []ABIValue{v})

	results, err := DecodeFunctionResult(encoded[4:], []ABIType{arrType})
	if err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if len(results[0].ArrayElems) != 2 {
		t.Fatalf("array length: got %d, want 2", len(results[0].ArrayElems))
	}
	if results[0].ArrayElems[0].Uint256.Int64() != 100 {
		t.Fatalf("elem[0]: got %d, want 100", results[0].ArrayElems[0].Uint256.Int64())
	}
	if results[0].ArrayElems[1].Uint256.Int64() != 200 {
		t.Fatalf("elem[1]: got %d, want 200", results[0].ArrayElems[1].Uint256.Int64())
	}
}

func TestEncodeDecodeTuple(t *testing.T) {
	addrType := ABIType{Kind: ABIAddress}
	uint256Type := ABIType{Kind: ABIUint256}
	tupleType := ABIType{
		Kind:   ABITuple,
		Fields: []ABIType{addrType, uint256Type},
	}

	addr := types.HexToAddress("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	amount := big.NewInt(999)

	v := ABIValue{
		Type: tupleType,
		TupleElems: []ABIValue{
			{Type: addrType, Addr: addr},
			{Type: uint256Type, Uint256: amount},
		},
	}

	sel := [4]byte{0x11, 0x22, 0x33, 0x44}
	encoded := EncodeFunctionCall(sel, []ABIValue{v})

	// A static tuple is encoded inline: 4 (sel) + 32 (address) + 32 (uint256) = 68.
	if len(encoded) != 68 {
		t.Fatalf("encoded length: got %d, want 68", len(encoded))
	}

	results, err := DecodeFunctionResult(encoded[4:], []ABIType{tupleType})
	if err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if results[0].TupleElems[0].Addr != addr {
		t.Fatalf("tuple address mismatch")
	}
	if results[0].TupleElems[1].Uint256.Cmp(amount) != 0 {
		t.Fatalf("tuple uint256 mismatch")
	}
}

func TestEncodeDecodeFixedBytes(t *testing.T) {
	abiType := ABIType{Kind: ABIFixedBytes, Size: 4}
	v := ABIValue{Type: abiType, BytesVal: []byte{0xde, 0xad, 0xbe, 0xef}}

	sel := [4]byte{}
	encoded := EncodeFunctionCall(sel, []ABIValue{v})

	results, err := DecodeFunctionResult(encoded[4:], []ABIType{abiType})
	if err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if !bytes.Equal(results[0].BytesVal, []byte{0xde, 0xad, 0xbe, 0xef}) {
		t.Fatalf("decoded fixed bytes: got %x, want deadbeef", results[0].BytesVal)
	}
}

func TestDecodeShortData(t *testing.T) {
	abiType := ABIType{Kind: ABIUint256}
	_, err := DecodeFunctionResult([]byte{0x01, 0x02}, []ABIType{abiType})
	if err == nil {
		t.Fatal("expected error for short data")
	}
}

func TestEncodeLargeUint256(t *testing.T) {
	maxUint256 := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
	abiType := ABIType{Kind: ABIUint256}
	v := ABIValue{Type: abiType, Uint256: maxUint256}

	sel := [4]byte{}
	encoded := EncodeFunctionCall(sel, []ABIValue{v})

	results, err := DecodeFunctionResult(encoded[4:], []ABIType{abiType})
	if err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if results[0].Uint256.Cmp(maxUint256) != 0 {
		t.Fatalf("max uint256 mismatch: got %s, want %s", results[0].Uint256, maxUint256)
	}
}

func TestEncodeEmptyDynamicArray(t *testing.T) {
	elemType := ABIType{Kind: ABIUint256}
	arrType := ABIType{Kind: ABIDynamicArray, Elem: &elemType}
	v := ABIValue{Type: arrType, ArrayElems: []ABIValue{}}

	sel := [4]byte{}
	encoded := EncodeFunctionCall(sel, []ABIValue{v})

	results, err := DecodeFunctionResult(encoded[4:], []ABIType{arrType})
	if err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if len(results[0].ArrayElems) != 0 {
		t.Fatalf("expected empty array, got %d elements", len(results[0].ArrayElems))
	}
}

func TestABIPad32(t *testing.T) {
	result := abiPad32([]byte{0x42})
	if len(result) != 32 {
		t.Fatalf("abiPad32 length: got %d, want 32", len(result))
	}
	if result[31] != 0x42 {
		t.Fatalf("abiPad32 last byte: got %x, want 42", result[31])
	}
	for i := 0; i < 31; i++ {
		if result[i] != 0 {
			t.Fatalf("abiPad32 byte %d: got %x, want 0", i, result[i])
		}
	}
}
