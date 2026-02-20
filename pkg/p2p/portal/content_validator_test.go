package portal

import (
	"testing"

	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/crypto"
)

// --- HeaderValidator ---

func TestHeaderValidator_Valid(t *testing.T) {
	v := &HeaderValidator{}
	headerData := []byte("test header rlp data for validator")
	hash := crypto.Keccak256Hash(headerData)
	contentKey := BlockHeaderKey{BlockHash: hash}.Encode()

	if err := v.Validate(contentKey, headerData); err != nil {
		t.Fatalf("Validate valid header: %v", err)
	}
}

func TestHeaderValidator_Mismatch(t *testing.T) {
	v := &HeaderValidator{}
	headerData := []byte("correct header")
	hash := crypto.Keccak256Hash(headerData)
	contentKey := BlockHeaderKey{BlockHash: hash}.Encode()

	wrongData := []byte("wrong header data")
	if err := v.Validate(contentKey, wrongData); err != ErrValidationFailed {
		t.Fatalf("want ErrValidationFailed, got %v", err)
	}
}

func TestHeaderValidator_EmptyContent(t *testing.T) {
	v := &HeaderValidator{}
	hash := types.HexToHash("0x1234")
	contentKey := BlockHeaderKey{BlockHash: hash}.Encode()

	if err := v.Validate(contentKey, nil); err != ErrEmptyContent {
		t.Fatalf("want ErrEmptyContent, got %v", err)
	}
	if err := v.Validate(contentKey, []byte{}); err != ErrEmptyContent {
		t.Fatalf("want ErrEmptyContent for empty slice, got %v", err)
	}
}

func TestHeaderValidator_MalformedKey(t *testing.T) {
	v := &HeaderValidator{}
	if err := v.Validate([]byte{0x00}, []byte("data")); err != ErrMalformedContentKey {
		t.Fatalf("want ErrMalformedContentKey, got %v", err)
	}
}

func TestHeaderValidator_WrongKeyType(t *testing.T) {
	v := &HeaderValidator{}
	key := make([]byte, 33)
	key[0] = ContentKeyBlockBody // wrong type for HeaderValidator
	if err := v.Validate(key, []byte("data")); err != ErrValidationFailed {
		t.Fatalf("want ErrValidationFailed, got %v", err)
	}
}

// --- BodyValidator ---

func TestBodyValidator_NoHeaderLookup(t *testing.T) {
	v := &BodyValidator{}
	hash := types.HexToHash("0xbody")
	contentKey := BlockBodyKey{BlockHash: hash}.Encode()

	// Without header lookup, non-empty body is accepted.
	if err := v.Validate(contentKey, []byte("body data")); err != nil {
		t.Fatalf("Validate without header lookup: %v", err)
	}
}

func TestBodyValidator_WithHeaderLookup_NoHeader(t *testing.T) {
	v := &BodyValidator{
		HeaderLookup: func(blockHash types.Hash) []byte {
			return nil // header not available
		},
	}
	hash := types.HexToHash("0xbody2")
	contentKey := BlockBodyKey{BlockHash: hash}.Encode()

	if err := v.Validate(contentKey, []byte("body data")); err != nil {
		t.Fatalf("Validate with missing header: %v", err)
	}
}

func TestBodyValidator_EmptyContent(t *testing.T) {
	v := &BodyValidator{}
	hash := types.HexToHash("0xbody3")
	contentKey := BlockBodyKey{BlockHash: hash}.Encode()

	if err := v.Validate(contentKey, nil); err != ErrEmptyContent {
		t.Fatalf("want ErrEmptyContent, got %v", err)
	}
}

func TestBodyValidator_WrongKeyType(t *testing.T) {
	v := &BodyValidator{}
	key := make([]byte, 33)
	key[0] = ContentKeyBlockHeader // wrong type
	if err := v.Validate(key, []byte("data")); err != ErrValidationFailed {
		t.Fatalf("want ErrValidationFailed, got %v", err)
	}
}

// --- ReceiptValidator ---

func TestReceiptValidator_NoHeaderLookup(t *testing.T) {
	v := &ReceiptValidator{}
	hash := types.HexToHash("0xreceipt")
	contentKey := ReceiptKey{BlockHash: hash}.Encode()

	if err := v.Validate(contentKey, []byte("receipt data")); err != nil {
		t.Fatalf("Validate without header lookup: %v", err)
	}
}

func TestReceiptValidator_EmptyContent(t *testing.T) {
	v := &ReceiptValidator{}
	hash := types.HexToHash("0xreceipt2")
	contentKey := ReceiptKey{BlockHash: hash}.Encode()

	if err := v.Validate(contentKey, nil); err != ErrEmptyContent {
		t.Fatalf("want ErrEmptyContent, got %v", err)
	}
}

func TestReceiptValidator_WrongKeyType(t *testing.T) {
	v := &ReceiptValidator{}
	key := make([]byte, 33)
	key[0] = ContentKeyBlockHeader // wrong
	if err := v.Validate(key, []byte("data")); err != ErrValidationFailed {
		t.Fatalf("want ErrValidationFailed, got %v", err)
	}
}

// --- StateValidator ---

func TestStateValidator_TrieNode(t *testing.T) {
	v := &StateValidator{}
	// Build a state content key: type(1) || address_hash(32) || path
	key := make([]byte, 1+types.HashLength+4)
	key[0] = StateKeyAccountTrieNode

	if err := v.Validate(key, []byte("trie node data")); err != nil {
		t.Fatalf("Validate trie node: %v", err)
	}
}

func TestStateValidator_ContractBytecode_Valid(t *testing.T) {
	v := &StateValidator{}
	bytecode := []byte("contract bytecode data for test")
	codeHash := crypto.Keccak256(bytecode)

	// Build content key: type(1) || address_hash(32) || code_hash(32)
	key := make([]byte, 1+types.HashLength+types.HashLength)
	key[0] = StateKeyContractBytecode
	copy(key[1+types.HashLength:], codeHash)

	if err := v.Validate(key, bytecode); err != nil {
		t.Fatalf("Validate bytecode: %v", err)
	}
}

func TestStateValidator_ContractBytecode_Mismatch(t *testing.T) {
	v := &StateValidator{}
	bytecode := []byte("correct code")
	wrongCode := []byte("wrong code")
	codeHash := crypto.Keccak256(bytecode)

	key := make([]byte, 1+types.HashLength+types.HashLength)
	key[0] = StateKeyContractBytecode
	copy(key[1+types.HashLength:], codeHash)

	if err := v.Validate(key, wrongCode); err != ErrStateProofInvalid {
		t.Fatalf("want ErrStateProofInvalid, got %v", err)
	}
}

func TestStateValidator_EmptyContent(t *testing.T) {
	v := &StateValidator{}
	key := make([]byte, 1+types.HashLength)
	key[0] = StateKeyAccountTrieNode

	if err := v.Validate(key, nil); err != ErrEmptyContent {
		t.Fatalf("want ErrEmptyContent, got %v", err)
	}
}

func TestStateValidator_UnknownType(t *testing.T) {
	v := &StateValidator{}
	key := make([]byte, 1+types.HashLength)
	key[0] = 0xFF // unknown state type

	if err := v.Validate(key, []byte("data")); err != ErrMalformedContentKey {
		t.Fatalf("want ErrMalformedContentKey, got %v", err)
	}
}

// --- AccumulatorProof ---

func TestVerifyAccumulatorProof_Valid(t *testing.T) {
	// Build a small Merkle tree: 2 leaves.
	leaf0 := [32]byte{0x01}
	leaf1 := [32]byte{0x02}
	root := hashPair(leaf0, leaf1)

	// Proof for leaf0: sibling is leaf1.
	proof := AccumulatorProof{
		Proof:     [][32]byte{leaf1},
		LeafIndex: 0,
	}
	if !VerifyAccumulatorProof(leaf0, proof, root) {
		t.Fatal("valid proof should verify")
	}

	// Proof for leaf1: sibling is leaf0.
	proof1 := AccumulatorProof{
		Proof:     [][32]byte{leaf0},
		LeafIndex: 1,
	}
	if !VerifyAccumulatorProof(leaf1, proof1, root) {
		t.Fatal("valid proof for leaf1 should verify")
	}
}

func TestVerifyAccumulatorProof_Invalid(t *testing.T) {
	leaf0 := [32]byte{0x01}
	leaf1 := [32]byte{0x02}
	root := hashPair(leaf0, leaf1)

	wrongLeaf := [32]byte{0xFF}
	proof := AccumulatorProof{
		Proof:     [][32]byte{leaf1},
		LeafIndex: 0,
	}
	if VerifyAccumulatorProof(wrongLeaf, proof, root) {
		t.Fatal("invalid proof should not verify")
	}
}

func TestVerifyAccumulatorProof_EmptyProof(t *testing.T) {
	proof := AccumulatorProof{
		Proof:     nil,
		LeafIndex: 0,
	}
	if VerifyAccumulatorProof([32]byte{}, proof, [32]byte{}) {
		t.Fatal("empty proof should not verify")
	}
}

func TestVerifyAccumulatorProof_DeepTree(t *testing.T) {
	// Build a 4-leaf tree.
	l0 := [32]byte{0x10}
	l1 := [32]byte{0x11}
	l2 := [32]byte{0x12}
	l3 := [32]byte{0x13}

	h01 := hashPair(l0, l1)
	h23 := hashPair(l2, l3)
	root := hashPair(h01, h23)

	// Proof for l2 (index=2): siblings are l3 (level 0) and h01 (level 1).
	proof := AccumulatorProof{
		Proof:     [][32]byte{l3, h01},
		LeafIndex: 2,
	}
	if !VerifyAccumulatorProof(l2, proof, root) {
		t.Fatal("deep tree proof should verify")
	}
}

// --- ComputeTrieRoot ---

func TestComputeTrieRoot_Empty(t *testing.T) {
	root := ComputeTrieRoot(nil)
	if root != ([32]byte{}) {
		t.Fatal("empty items should produce zero root")
	}
}

func TestComputeTrieRoot_SingleItem(t *testing.T) {
	item := []byte("single item")
	root := ComputeTrieRoot([][]byte{item})

	h := crypto.Keccak256(item)
	var expected [32]byte
	copy(expected[:], h)

	// With a single leaf, nextPowerOf2(1)==1, so the root is the leaf itself.
	if root != expected {
		t.Fatal("single item root mismatch")
	}
}

func TestComputeTrieRoot_TwoItems(t *testing.T) {
	a := []byte("item a")
	b := []byte("item b")
	root := ComputeTrieRoot([][]byte{a, b})

	ha := crypto.Keccak256(a)
	hb := crypto.Keccak256(b)
	var la, lb [32]byte
	copy(la[:], ha)
	copy(lb[:], hb)
	expected := hashPair(la, lb)

	if root != expected {
		t.Fatal("two item root mismatch")
	}
}

func TestComputeTrieRoot_Deterministic(t *testing.T) {
	items := [][]byte{[]byte("x"), []byte("y"), []byte("z")}
	r1 := ComputeTrieRoot(items)
	r2 := ComputeTrieRoot(items)
	if r1 != r2 {
		t.Fatal("ComputeTrieRoot should be deterministic")
	}
}

// --- ValidatorRegistry ---

func TestValidatorRegistry_Register(t *testing.T) {
	reg := NewValidatorRegistry()
	reg.Register(ContentKeyBlockHeader, &HeaderValidator{})

	if !reg.HasValidator(ContentKeyBlockHeader) {
		t.Fatal("should have header validator")
	}
	if reg.HasValidator(ContentKeyBlockBody) {
		t.Fatal("should not have body validator")
	}
}

func TestValidatorRegistry_Validate_HeaderOK(t *testing.T) {
	reg := NewValidatorRegistry()
	reg.Register(ContentKeyBlockHeader, &HeaderValidator{})

	headerData := []byte("registry test header")
	hash := crypto.Keccak256Hash(headerData)
	contentKey := BlockHeaderKey{BlockHash: hash}.Encode()

	if err := reg.Validate(contentKey, headerData); err != nil {
		t.Fatalf("registry Validate: %v", err)
	}
}

func TestValidatorRegistry_Validate_NotRegistered(t *testing.T) {
	reg := NewValidatorRegistry()
	contentKey := ReceiptKey{BlockHash: types.HexToHash("0xabc")}.Encode()

	if err := reg.Validate(contentKey, []byte("data")); err != ErrValidatorNotRegistered {
		t.Fatalf("want ErrValidatorNotRegistered, got %v", err)
	}
}

func TestValidatorRegistry_Validate_EmptyKey(t *testing.T) {
	reg := NewValidatorRegistry()
	if err := reg.Validate(nil, []byte("data")); err != ErrMalformedContentKey {
		t.Fatalf("want ErrMalformedContentKey, got %v", err)
	}
}

func TestNewDefaultRegistry(t *testing.T) {
	reg := NewDefaultRegistry()
	if !reg.HasValidator(ContentKeyBlockHeader) {
		t.Fatal("default registry should have header validator")
	}
	if !reg.HasValidator(ContentKeyBlockBody) {
		t.Fatal("default registry should have body validator")
	}
	if !reg.HasValidator(ContentKeyReceipt) {
		t.Fatal("default registry should have receipt validator")
	}
	if !reg.HasValidator(StateKeyAccountTrieNode) {
		t.Fatal("default registry should have account trie node validator")
	}
	if !reg.HasValidator(StateKeyContractStorageTrieNode) {
		t.Fatal("default registry should have storage trie node validator")
	}
	if !reg.HasValidator(StateKeyContractBytecode) {
		t.Fatal("default registry should have bytecode validator")
	}
}

// --- nextPowerOf2 ---

func TestNextPowerOf2(t *testing.T) {
	tests := []struct {
		n, want int
	}{
		{0, 1},
		{1, 1},
		{2, 2},
		{3, 4},
		{4, 4},
		{5, 8},
		{7, 8},
		{8, 8},
		{9, 16},
	}
	for _, tt := range tests {
		got := nextPowerOf2(tt.n)
		if got != tt.want {
			t.Errorf("nextPowerOf2(%d): want %d, got %d", tt.n, tt.want, got)
		}
	}
}
