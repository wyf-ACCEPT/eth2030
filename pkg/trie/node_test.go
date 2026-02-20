package trie

import (
	"bytes"
	"testing"
)

// --- node interface: fullNode ---

func TestFullNode_Cache_NewNode(t *testing.T) {
	fn := &fullNode{}
	hash, dirty := fn.cache()
	if hash != nil {
		t.Fatal("new fullNode should have nil cached hash")
	}
	if dirty {
		t.Fatal("new fullNode should not be dirty by default")
	}
}

func TestFullNode_Cache_DirtyNode(t *testing.T) {
	fn := &fullNode{flags: nodeFlag{dirty: true}}
	_, dirty := fn.cache()
	if !dirty {
		t.Fatal("dirty fullNode should report dirty=true")
	}
}

func TestFullNode_Cache_HashedNode(t *testing.T) {
	h := hashNode(bytes.Repeat([]byte{0x11}, 32))
	fn := &fullNode{flags: nodeFlag{hash: h, dirty: false}}
	hash, dirty := fn.cache()
	if !bytes.Equal(hash, h) {
		t.Fatal("cached hash should match")
	}
	if dirty {
		t.Fatal("should not be dirty")
	}
}

func TestFullNode_Copy(t *testing.T) {
	fn := &fullNode{flags: nodeFlag{dirty: true}}
	fn.Children[0] = valueNode([]byte("zero"))
	fn.Children[15] = valueNode([]byte("fifteen"))

	cp := fn.copy()
	if cp == fn {
		t.Fatal("copy should return a different pointer")
	}
	if cp.Children[0] == nil {
		t.Fatal("copy should preserve children[0]")
	}
	if cp.Children[15] == nil {
		t.Fatal("copy should preserve children[15]")
	}
	if !cp.flags.dirty {
		t.Fatal("copy should preserve flags")
	}
}

func TestFullNode_Copy_Independence(t *testing.T) {
	fn := &fullNode{flags: nodeFlag{dirty: true}}
	fn.Children[0] = valueNode([]byte("original"))

	cp := fn.copy()
	// Modify the copy's Children array (shallow copy means the Children array is copied).
	cp.Children[0] = valueNode([]byte("modified"))

	// The original node's child reference should still be "original" since
	// copy() does a shallow struct copy (the array is copied by value in Go).
	origVal := fn.Children[0].(valueNode)
	if string(origVal) != "original" {
		t.Fatalf("original child = %q, want %q", origVal, "original")
	}
}

// --- node interface: shortNode ---

func TestShortNode_Cache_NewNode(t *testing.T) {
	sn := &shortNode{Key: []byte{0x01}, Val: valueNode([]byte("v"))}
	hash, dirty := sn.cache()
	if hash != nil {
		t.Fatal("new shortNode should have nil cached hash")
	}
	if dirty {
		t.Fatal("new shortNode should not be dirty by default")
	}
}

func TestShortNode_Cache_Dirty(t *testing.T) {
	sn := &shortNode{
		Key:   []byte{0x01},
		Val:   valueNode([]byte("v")),
		flags: nodeFlag{dirty: true},
	}
	_, dirty := sn.cache()
	if !dirty {
		t.Fatal("dirty shortNode should report dirty=true")
	}
}

func TestShortNode_Cache_Hashed(t *testing.T) {
	h := hashNode(bytes.Repeat([]byte{0x22}, 32))
	sn := &shortNode{
		Key:   []byte{0x01},
		Val:   valueNode([]byte("v")),
		flags: nodeFlag{hash: h, dirty: false},
	}
	hash, dirty := sn.cache()
	if !bytes.Equal(hash, h) {
		t.Fatal("cached hash mismatch")
	}
	if dirty {
		t.Fatal("should not be dirty")
	}
}

func TestShortNode_Copy(t *testing.T) {
	sn := &shortNode{
		Key:   []byte{0x01, 0x02},
		Val:   valueNode([]byte("val")),
		flags: nodeFlag{dirty: true},
	}
	cp := sn.copy()
	if cp == sn {
		t.Fatal("copy should return a different pointer")
	}
	// The Key slice is shared (shallow copy).
	if !bytes.Equal(cp.Key, sn.Key) {
		t.Fatal("copy should have same key")
	}
	if !cp.flags.dirty {
		t.Fatal("copy should preserve dirty flag")
	}
}

// --- node interface: hashNode ---

func TestHashNode_Cache(t *testing.T) {
	hn := hashNode(bytes.Repeat([]byte{0x33}, 32))
	hash, dirty := hn.cache()
	if hash != nil {
		t.Fatal("hashNode.cache() should return nil hash")
	}
	if !dirty {
		t.Fatal("hashNode.cache() should return dirty=true")
	}
}

// --- node interface: valueNode ---

func TestValueNode_Cache(t *testing.T) {
	vn := valueNode([]byte("data"))
	hash, dirty := vn.cache()
	if hash != nil {
		t.Fatal("valueNode.cache() should return nil hash")
	}
	if !dirty {
		t.Fatal("valueNode.cache() should return dirty=true")
	}
}

// --- nodeFlag ---

func TestNodeFlag_Zero(t *testing.T) {
	var nf nodeFlag
	if nf.hash != nil {
		t.Fatal("zero nodeFlag should have nil hash")
	}
	if nf.dirty {
		t.Fatal("zero nodeFlag should not be dirty")
	}
}

func TestNodeFlag_WithHash(t *testing.T) {
	h := hashNode(bytes.Repeat([]byte{0x44}, 32))
	nf := nodeFlag{hash: h, dirty: false}
	if nf.hash == nil {
		t.Fatal("hash should be set")
	}
	if nf.dirty {
		t.Fatal("should not be dirty")
	}
}

// --- decodeNode ---

func TestDecodeNode_EmptyData(t *testing.T) {
	_, err := decodeNode(nil, []byte{})
	if err == nil {
		t.Fatal("decoding empty data should return error")
	}
}

func TestDecodeNode_ShortNode_Leaf(t *testing.T) {
	// Build a leaf shortNode, encode it, then decode.
	leaf := &shortNode{
		Key:   hexToCompact([]byte{0x01, 0x02, terminatorByte}),
		Val:   valueNode([]byte("leaf-value")),
		flags: nodeFlag{dirty: true},
	}
	enc, err := encodeShortNode(leaf)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	hash := hashNode(bytes.Repeat([]byte{0xab}, 32))
	decoded, err := decodeNode(hash, enc)
	if err != nil {
		t.Fatalf("decodeNode: %v", err)
	}
	sn, ok := decoded.(*shortNode)
	if !ok {
		t.Fatalf("expected *shortNode, got %T", decoded)
	}
	// Key should be hex-decoded (compactToHex), with terminator.
	if !hasTerm(sn.Key) {
		t.Fatal("decoded leaf key should have terminator")
	}
	// Value should match.
	v, ok := sn.Val.(valueNode)
	if !ok {
		t.Fatalf("expected valueNode, got %T", sn.Val)
	}
	if string(v) != "leaf-value" {
		t.Fatalf("value = %q, want %q", v, "leaf-value")
	}
	// Hash should be cached.
	if !bytes.Equal(sn.flags.hash, hash) {
		t.Fatal("decoded node should have cached hash")
	}
	if sn.flags.dirty {
		t.Fatal("decoded node should not be dirty")
	}
}

func TestDecodeNode_ShortNode_Extension(t *testing.T) {
	// Extension: key without terminator, child is a hash ref.
	childHash := hashNode(bytes.Repeat([]byte{0xcc}, 32))
	ext := &shortNode{
		Key: hexToCompact([]byte{0x01, 0x02}),
		Val: childHash,
	}
	enc, err := encodeShortNode(ext)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	hash := hashNode(bytes.Repeat([]byte{0xdd}, 32))
	decoded, err := decodeNode(hash, enc)
	if err != nil {
		t.Fatalf("decodeNode: %v", err)
	}
	sn, ok := decoded.(*shortNode)
	if !ok {
		t.Fatalf("expected *shortNode, got %T", decoded)
	}
	if hasTerm(sn.Key) {
		t.Fatal("decoded extension key should not have terminator")
	}
	// Child should be a hashNode (32-byte reference).
	ch, ok := sn.Val.(hashNode)
	if !ok {
		t.Fatalf("child should be hashNode, got %T", sn.Val)
	}
	if !bytes.Equal(ch, childHash) {
		t.Fatal("child hash mismatch")
	}
}

func TestDecodeNode_FullNode(t *testing.T) {
	fn := &fullNode{}
	fn.Children[0] = hashNode(bytes.Repeat([]byte{0x01}, 32))
	fn.Children[5] = hashNode(bytes.Repeat([]byte{0x05}, 32))
	fn.Children[16] = valueNode([]byte("branch-value"))

	enc, err := encodeFullNode(fn)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	hash := hashNode(bytes.Repeat([]byte{0xee}, 32))
	decoded, err := decodeNode(hash, enc)
	if err != nil {
		t.Fatalf("decodeNode: %v", err)
	}
	decodedFN, ok := decoded.(*fullNode)
	if !ok {
		t.Fatalf("expected *fullNode, got %T", decoded)
	}

	// Children 0 and 5 should be hash refs.
	if decodedFN.Children[0] == nil {
		t.Fatal("child 0 should not be nil")
	}
	if decodedFN.Children[5] == nil {
		t.Fatal("child 5 should not be nil")
	}
	// Child 16 should be valueNode.
	if decodedFN.Children[16] == nil {
		t.Fatal("child 16 (value) should not be nil")
	}
	// Verify hash is cached.
	if !bytes.Equal(decodedFN.flags.hash, hash) {
		t.Fatal("decoded fullNode should have cached hash")
	}
}

// --- decodeRef ---

func TestDecodeRef_Empty(t *testing.T) {
	n, err := decodeRef(nil)
	if err != nil {
		t.Fatalf("decodeRef(nil): %v", err)
	}
	if n != nil {
		t.Fatal("decodeRef(nil) should return nil node")
	}
}

func TestDecodeRef_32Bytes(t *testing.T) {
	data := bytes.Repeat([]byte{0xab}, 32)
	n, err := decodeRef(data)
	if err != nil {
		t.Fatalf("decodeRef(32 bytes): %v", err)
	}
	hn, ok := n.(hashNode)
	if !ok {
		t.Fatalf("expected hashNode, got %T", n)
	}
	if !bytes.Equal(hn, data) {
		t.Fatal("hash mismatch")
	}
}

// --- decodeRLPList ---

func TestDecodeRLPList_NonListPrefix(t *testing.T) {
	// 0x80 is a string prefix, not a list.
	_, err := decodeRLPList([]byte{0x80})
	if err == nil {
		t.Fatal("expected error for non-list prefix")
	}
}

func TestDecodeRLPList_Empty(t *testing.T) {
	_, err := decodeRLPList([]byte{})
	if err == nil {
		t.Fatal("expected error for empty data")
	}
}

func TestDecodeRLPList_Truncated(t *testing.T) {
	// Short list header claiming 10 bytes but only 2 available.
	_, err := decodeRLPList([]byte{0xca, 0x01})
	if err == nil {
		t.Fatal("expected error for truncated list")
	}
}

// --- decodeOneElement ---

func TestDecodeOneElement_SingleByte(t *testing.T) {
	data := []byte{0x42, 0x43}
	content, rest, err := decodeOneElement(data)
	if err != nil {
		t.Fatalf("decodeOneElement: %v", err)
	}
	if !bytes.Equal(content, []byte{0x42}) {
		t.Fatalf("content = %x, want [42]", content)
	}
	if !bytes.Equal(rest, []byte{0x43}) {
		t.Fatalf("rest = %x, want [43]", rest)
	}
}

func TestDecodeOneElement_EmptyString(t *testing.T) {
	data := []byte{0x80, 0x01}
	content, rest, err := decodeOneElement(data)
	if err != nil {
		t.Fatalf("decodeOneElement: %v", err)
	}
	if content != nil {
		t.Fatalf("content = %x, want nil", content)
	}
	if !bytes.Equal(rest, []byte{0x01}) {
		t.Fatalf("rest = %x, want [01]", rest)
	}
}

func TestDecodeOneElement_ShortString(t *testing.T) {
	// 0x83 followed by 3 bytes.
	data := []byte{0x83, 0x61, 0x62, 0x63, 0xff}
	content, rest, err := decodeOneElement(data)
	if err != nil {
		t.Fatalf("decodeOneElement: %v", err)
	}
	if string(content) != "abc" {
		t.Fatalf("content = %q, want %q", content, "abc")
	}
	if !bytes.Equal(rest, []byte{0xff}) {
		t.Fatalf("rest = %x, want [ff]", rest)
	}
}

func TestDecodeOneElement_Empty(t *testing.T) {
	_, _, err := decodeOneElement(nil)
	if err == nil {
		t.Fatal("expected error for empty data")
	}
}

// --- Roundtrip encode/decode ---

func TestEncodeDecodeRoundtrip_ShortNode(t *testing.T) {
	original := &shortNode{
		Key: hexToCompact([]byte{0x0a, 0x0b, terminatorByte}),
		Val: valueNode([]byte("roundtrip-value")),
	}
	enc, err := encodeShortNode(original)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	decoded, err := decodeNode(nil, enc)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	sn, ok := decoded.(*shortNode)
	if !ok {
		t.Fatalf("expected *shortNode, got %T", decoded)
	}
	if !hasTerm(sn.Key) {
		t.Fatal("decoded key should have terminator")
	}
	v := sn.Val.(valueNode)
	if string(v) != "roundtrip-value" {
		t.Fatalf("value = %q, want %q", v, "roundtrip-value")
	}
}

func TestEncodeDecodeRoundtrip_FullNode(t *testing.T) {
	original := &fullNode{}
	original.Children[0] = hashNode(bytes.Repeat([]byte{0x01}, 32))
	original.Children[16] = valueNode([]byte("branch-val"))

	enc, err := encodeFullNode(original)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	decoded, err := decodeNode(nil, enc)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	fn, ok := decoded.(*fullNode)
	if !ok {
		t.Fatalf("expected *fullNode, got %T", decoded)
	}
	if fn.Children[0] == nil {
		t.Fatal("child 0 should be present")
	}
	if fn.Children[16] == nil {
		t.Fatal("child 16 (value) should be present")
	}
}

// --- Invalid RLP element count ---

func TestDecodeNode_InvalidElementCount(t *testing.T) {
	// Build a 3-element RLP list (neither 2 nor 17).
	e1, _ := encodeNodeValue(valueNode([]byte("a")))
	e2, _ := encodeNodeValue(valueNode([]byte("b")))
	e3, _ := encodeNodeValue(valueNode([]byte("c")))
	payload := append(append(e1, e2...), e3...)
	data := wrapListPayload(payload)

	_, err := decodeNode(nil, data)
	if err == nil {
		t.Fatal("expected error for 3-element list")
	}
}
