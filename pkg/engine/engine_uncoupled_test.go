package engine

import (
	"testing"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

// --- Inclusion Proof Tests ---

func TestComputeMerkleRoot_SimpleTree(t *testing.T) {
	// Build a simple 4-leaf tree and verify proof.
	leaves := []types.Hash{
		crypto.Keccak256Hash([]byte("leaf0")),
		crypto.Keccak256Hash([]byte("leaf1")),
		crypto.Keccak256Hash([]byte("leaf2")),
		crypto.Keccak256Hash([]byte("leaf3")),
	}

	// Compute expected root manually.
	h01 := hashPair(leaves[0], leaves[1])
	h23 := hashPair(leaves[2], leaves[3])
	expectedRoot := hashPair(h01, h23)

	// Build proof for leaf at index 0: branch = [leaf1, h23].
	proof := &InclusionProof{
		Leaf:   leaves[0],
		Branch: []types.Hash{leaves[1], h23},
		Index:  0,
	}

	computed := computeMerkleRoot(proof.Leaf, proof.Branch, proof.Index)
	if computed != expectedRoot {
		t.Fatalf("root mismatch: got %s, want %s", computed.Hex(), expectedRoot.Hex())
	}
}

func TestComputeMerkleRoot_RightChild(t *testing.T) {
	leaves := []types.Hash{
		crypto.Keccak256Hash([]byte("a")),
		crypto.Keccak256Hash([]byte("b")),
		crypto.Keccak256Hash([]byte("c")),
		crypto.Keccak256Hash([]byte("d")),
	}

	h01 := hashPair(leaves[0], leaves[1])
	h23 := hashPair(leaves[2], leaves[3])
	expectedRoot := hashPair(h01, h23)

	// Proof for leaf at index 1: branch = [leaf0, h23].
	proof := &InclusionProof{
		Leaf:   leaves[1],
		Branch: []types.Hash{leaves[0], h23},
		Index:  1,
	}

	computed := computeMerkleRoot(proof.Leaf, proof.Branch, proof.Index)
	if computed != expectedRoot {
		t.Fatalf("root mismatch: got %s, want %s", computed.Hex(), expectedRoot.Hex())
	}
}

func TestComputeMerkleRoot_Index3(t *testing.T) {
	leaves := []types.Hash{
		crypto.Keccak256Hash([]byte("a")),
		crypto.Keccak256Hash([]byte("b")),
		crypto.Keccak256Hash([]byte("c")),
		crypto.Keccak256Hash([]byte("d")),
	}

	h01 := hashPair(leaves[0], leaves[1])
	h23 := hashPair(leaves[2], leaves[3])
	expectedRoot := hashPair(h01, h23)

	// Proof for leaf at index 3: branch = [leaf2, h01].
	proof := &InclusionProof{
		Leaf:   leaves[3],
		Branch: []types.Hash{leaves[2], h01},
		Index:  3,
	}

	computed := computeMerkleRoot(proof.Leaf, proof.Branch, proof.Index)
	if computed != expectedRoot {
		t.Fatalf("root mismatch: got %s, want %s", computed.Hex(), expectedRoot.Hex())
	}
}

func TestValidateInclusionProof_Valid(t *testing.T) {
	leaves := []types.Hash{
		crypto.Keccak256Hash([]byte("field0")),
		crypto.Keccak256Hash([]byte("field1")),
		crypto.Keccak256Hash([]byte("payload")),
		crypto.Keccak256Hash([]byte("field3")),
	}

	h01 := hashPair(leaves[0], leaves[1])
	h23 := hashPair(leaves[2], leaves[3])
	root := hashPair(h01, h23)

	proof := &InclusionProof{
		Leaf:   leaves[2],
		Branch: []types.Hash{leaves[3], h01},
		Index:  2,
	}

	if err := ValidateInclusionProof(proof, root); err != nil {
		t.Fatalf("valid proof rejected: %v", err)
	}
}

func TestValidateInclusionProof_Invalid(t *testing.T) {
	proof := &InclusionProof{
		Leaf:   crypto.Keccak256Hash([]byte("wrong")),
		Branch: []types.Hash{{0x01}, {0x02}},
		Index:  0,
	}
	wrongRoot := types.Hash{0xff}

	err := ValidateInclusionProof(proof, wrongRoot)
	if err == nil {
		t.Fatal("expected error for invalid proof")
	}
}

func TestValidateInclusionProof_NilProof(t *testing.T) {
	if err := ValidateInclusionProof(nil, types.Hash{}); err != ErrMissingInclusionProof {
		t.Fatalf("expected ErrMissingInclusionProof, got %v", err)
	}
}

func TestValidateInclusionProof_EmptyLeaf(t *testing.T) {
	proof := &InclusionProof{
		Leaf:   types.Hash{}, // zero
		Branch: []types.Hash{{1}},
		Index:  0,
	}
	if err := ValidateInclusionProof(proof, types.Hash{}); err != ErrInvalidInclusionProof {
		t.Fatalf("expected ErrInvalidInclusionProof, got %v", err)
	}
}

func TestValidateInclusionProof_EmptyBranch(t *testing.T) {
	proof := &InclusionProof{
		Leaf:   types.Hash{1},
		Branch: []types.Hash{},
		Index:  0,
	}
	if err := ValidateInclusionProof(proof, types.Hash{}); err != ErrInvalidInclusionProof {
		t.Fatalf("expected ErrInvalidInclusionProof, got %v", err)
	}
}

// --- BuildInclusionProof Tests ---

func TestBuildInclusionProof(t *testing.T) {
	// Simulate 4 beacon block body fields.
	fields := []types.Hash{
		crypto.Keccak256Hash([]byte("randao")),
		crypto.Keccak256Hash([]byte("eth1data")),
		crypto.Keccak256Hash([]byte("payload")),
		crypto.Keccak256Hash([]byte("attestations")),
	}

	payloadIndex := 2
	proof, err := BuildInclusionProof(fields[payloadIndex], fields, payloadIndex)
	if err != nil {
		t.Fatalf("build proof failed: %v", err)
	}

	// Compute the expected root.
	h01 := hashPair(fields[0], fields[1])
	h23 := hashPair(fields[2], fields[3])
	expectedRoot := hashPair(h01, h23)

	// Verify the proof against the computed root.
	if err := ValidateInclusionProof(proof, expectedRoot); err != nil {
		t.Fatalf("proof validation failed: %v", err)
	}
}

func TestBuildInclusionProof_AllIndices(t *testing.T) {
	fields := []types.Hash{
		crypto.Keccak256Hash([]byte("f0")),
		crypto.Keccak256Hash([]byte("f1")),
		crypto.Keccak256Hash([]byte("f2")),
		crypto.Keccak256Hash([]byte("f3")),
	}

	// Compute root.
	h01 := hashPair(fields[0], fields[1])
	h23 := hashPair(fields[2], fields[3])
	root := hashPair(h01, h23)

	for i := 0; i < 4; i++ {
		proof, err := BuildInclusionProof(fields[i], fields, i)
		if err != nil {
			t.Fatalf("index %d: build failed: %v", i, err)
		}
		if err := ValidateInclusionProof(proof, root); err != nil {
			t.Fatalf("index %d: validation failed: %v", i, err)
		}
	}
}

func TestBuildInclusionProof_NonPowerOfTwo(t *testing.T) {
	// 3 fields should be padded to 4.
	fields := []types.Hash{
		crypto.Keccak256Hash([]byte("f0")),
		crypto.Keccak256Hash([]byte("f1")),
		crypto.Keccak256Hash([]byte("f2")),
	}

	// Padded: fields[3] = Hash{}
	padded := padToPowerOfTwo(fields)
	h01 := hashPair(padded[0], padded[1])
	h23 := hashPair(padded[2], padded[3])
	root := hashPair(h01, h23)

	proof, err := BuildInclusionProof(fields[2], fields, 2)
	if err != nil {
		t.Fatalf("build failed: %v", err)
	}
	if err := ValidateInclusionProof(proof, root); err != nil {
		t.Fatalf("validation failed: %v", err)
	}
}

func TestBuildInclusionProof_InvalidIndex(t *testing.T) {
	fields := []types.Hash{{1}}
	_, err := BuildInclusionProof(fields[0], fields, 5)
	if err == nil {
		t.Fatal("expected error for out-of-range index")
	}
}

func TestBuildInclusionProof_EmptyFields(t *testing.T) {
	_, err := BuildInclusionProof(types.Hash{}, nil, 0)
	if err == nil {
		t.Fatal("expected error for empty fields")
	}
}

// --- UncoupledPayloadEnvelope Tests ---

func TestUncoupledPayloadEnvelope_Validate(t *testing.T) {
	validPayload := &ExecutionPayloadV5{
		ExecutionPayloadV4: ExecutionPayloadV4{
			ExecutionPayloadV3: ExecutionPayloadV3{
				ExecutionPayloadV2: ExecutionPayloadV2{
					ExecutionPayloadV1: ExecutionPayloadV1{
						BlockHash: types.Hash{0x01},
					},
				},
			},
		},
	}

	proof := &InclusionProof{
		Leaf:   types.Hash{0x01},
		Branch: []types.Hash{{0x02}},
		Index:  0,
	}

	tests := []struct {
		name    string
		env     *UncoupledPayloadEnvelope
		wantErr error
	}{
		{
			name: "valid",
			env: &UncoupledPayloadEnvelope{
				BeaconBlockRoot: types.Hash{0xaa},
				Slot:            100,
				Payload:         validPayload,
				Proof:           proof,
			},
			wantErr: nil,
		},
		{
			name: "missing beacon root",
			env: &UncoupledPayloadEnvelope{
				Slot:    100,
				Payload: validPayload,
				Proof:   proof,
			},
			wantErr: ErrMissingBeaconRoot,
		},
		{
			name: "zero slot",
			env: &UncoupledPayloadEnvelope{
				BeaconBlockRoot: types.Hash{0xaa},
				Slot:            0,
				Payload:         validPayload,
				Proof:           proof,
			},
			wantErr: ErrInvalidParams,
		},
		{
			name: "nil payload",
			env: &UncoupledPayloadEnvelope{
				BeaconBlockRoot: types.Hash{0xaa},
				Slot:            100,
				Proof:           proof,
			},
			wantErr: ErrInvalidPayloadAttributes,
		},
		{
			name: "nil proof",
			env: &UncoupledPayloadEnvelope{
				BeaconBlockRoot: types.Hash{0xaa},
				Slot:            100,
				Payload:         validPayload,
			},
			wantErr: ErrMissingInclusionProof,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.env.Validate()
			if err != tt.wantErr {
				t.Errorf("got error %v, want %v", err, tt.wantErr)
			}
		})
	}
}

// --- UncoupledPayloadHandler Tests ---

func TestUncoupledPayloadHandler_SubmitAndGet(t *testing.T) {
	handler := NewUncoupledPayloadHandler(nil)

	// Build a valid envelope with a real inclusion proof.
	fields := []types.Hash{
		crypto.Keccak256Hash([]byte("f0")),
		crypto.Keccak256Hash([]byte("f1")),
		crypto.Keccak256Hash([]byte("payload")),
		crypto.Keccak256Hash([]byte("f3")),
	}
	h01 := hashPair(fields[0], fields[1])
	h23 := hashPair(fields[2], fields[3])
	beaconRoot := hashPair(h01, h23)

	proof, err := BuildInclusionProof(fields[2], fields, 2)
	if err != nil {
		t.Fatalf("build proof: %v", err)
	}

	envelope := &UncoupledPayloadEnvelope{
		BeaconBlockRoot: beaconRoot,
		Slot:            42,
		Payload: &ExecutionPayloadV5{
			ExecutionPayloadV4: ExecutionPayloadV4{
				ExecutionPayloadV3: ExecutionPayloadV3{
					ExecutionPayloadV2: ExecutionPayloadV2{
						ExecutionPayloadV1: ExecutionPayloadV1{
							BlockHash: types.Hash{0x01},
						},
					},
				},
			},
		},
		Proof: proof,
	}

	status, err := handler.SubmitUncoupledPayload(envelope)
	if err != nil {
		t.Fatalf("submit failed: %v", err)
	}
	if status != UncoupledStatusVerified {
		t.Fatalf("expected VERIFIED, got %s", status)
	}

	// Retrieve it.
	got := handler.GetPendingPayload(beaconRoot)
	if got == nil {
		t.Fatal("expected pending payload, got nil")
	}
	if got.Slot != 42 {
		t.Fatalf("slot mismatch: got %d, want 42", got.Slot)
	}

	// Check status.
	if handler.GetPayloadStatus(beaconRoot) != UncoupledStatusVerified {
		t.Fatal("expected verified status")
	}
	if handler.PendingCount() != 1 {
		t.Fatalf("expected 1 pending, got %d", handler.PendingCount())
	}
}

func TestUncoupledPayloadHandler_DuplicateSubmit(t *testing.T) {
	handler := NewUncoupledPayloadHandler(nil)

	fields := []types.Hash{
		crypto.Keccak256Hash([]byte("f0")),
		crypto.Keccak256Hash([]byte("payload")),
	}
	padded := padToPowerOfTwo(fields)
	root := hashPair(padded[0], padded[1])

	proof, _ := BuildInclusionProof(fields[1], fields, 1)

	envelope := &UncoupledPayloadEnvelope{
		BeaconBlockRoot: root,
		Slot:            10,
		Payload: &ExecutionPayloadV5{
			ExecutionPayloadV4: ExecutionPayloadV4{
				ExecutionPayloadV3: ExecutionPayloadV3{
					ExecutionPayloadV2: ExecutionPayloadV2{
						ExecutionPayloadV1: ExecutionPayloadV1{
							BlockHash: types.Hash{0x01},
						},
					},
				},
			},
		},
		Proof: proof,
	}

	// First submit.
	handler.SubmitUncoupledPayload(envelope)

	// Duplicate submit should also succeed.
	status, err := handler.SubmitUncoupledPayload(envelope)
	if err != nil {
		t.Fatalf("duplicate submit failed: %v", err)
	}
	if status != UncoupledStatusVerified {
		t.Fatalf("expected VERIFIED for duplicate, got %s", status)
	}

	// Should still only have 1 entry.
	if handler.PendingCount() != 1 {
		t.Fatalf("expected 1 pending after duplicate, got %d", handler.PendingCount())
	}
}

func TestUncoupledPayloadHandler_InvalidProof(t *testing.T) {
	handler := NewUncoupledPayloadHandler(nil)

	envelope := &UncoupledPayloadEnvelope{
		BeaconBlockRoot: types.Hash{0xaa},
		Slot:            10,
		Payload: &ExecutionPayloadV5{
			ExecutionPayloadV4: ExecutionPayloadV4{
				ExecutionPayloadV3: ExecutionPayloadV3{
					ExecutionPayloadV2: ExecutionPayloadV2{
						ExecutionPayloadV1: ExecutionPayloadV1{
							BlockHash: types.Hash{0x01},
						},
					},
				},
			},
		},
		Proof: &InclusionProof{
			Leaf:   types.Hash{0xff},
			Branch: []types.Hash{{0x01}},
			Index:  0,
		},
	}

	status, err := handler.SubmitUncoupledPayload(envelope)
	if err == nil {
		t.Fatal("expected error for invalid proof")
	}
	if status != UncoupledStatusInvalid {
		t.Fatalf("expected INVALID, got %s", status)
	}
}

func TestUncoupledPayloadHandler_GetPending_NotFound(t *testing.T) {
	handler := NewUncoupledPayloadHandler(nil)
	if handler.GetPendingPayload(types.Hash{0xff}) != nil {
		t.Fatal("expected nil for unknown root")
	}
	if handler.GetPayloadStatus(types.Hash{0xff}) != UncoupledStatusPending {
		t.Fatal("expected PENDING for unknown root")
	}
}

func TestUncoupledPayloadHandler_RemovePending(t *testing.T) {
	handler := NewUncoupledPayloadHandler(nil)

	fields := []types.Hash{
		crypto.Keccak256Hash([]byte("f0")),
		crypto.Keccak256Hash([]byte("payload")),
	}
	padded := padToPowerOfTwo(fields)
	root := hashPair(padded[0], padded[1])

	proof, _ := BuildInclusionProof(fields[1], fields, 1)

	envelope := &UncoupledPayloadEnvelope{
		BeaconBlockRoot: root,
		Slot:            10,
		Payload: &ExecutionPayloadV5{
			ExecutionPayloadV4: ExecutionPayloadV4{
				ExecutionPayloadV3: ExecutionPayloadV3{
					ExecutionPayloadV2: ExecutionPayloadV2{
						ExecutionPayloadV1: ExecutionPayloadV1{
							BlockHash: types.Hash{0x01},
						},
					},
				},
			},
		},
		Proof: proof,
	}

	handler.SubmitUncoupledPayload(envelope)
	handler.RemovePending(root)

	if handler.PendingCount() != 0 {
		t.Fatalf("expected 0 pending after remove, got %d", handler.PendingCount())
	}
	if handler.GetPendingPayload(root) != nil {
		t.Fatal("expected nil after remove")
	}
}

func TestUncoupledPayloadHandler_VerifyInclusion(t *testing.T) {
	handler := NewUncoupledPayloadHandler(nil)

	fields := []types.Hash{
		crypto.Keccak256Hash([]byte("f0")),
		crypto.Keccak256Hash([]byte("f1")),
		crypto.Keccak256Hash([]byte("payload")),
		crypto.Keccak256Hash([]byte("f3")),
	}
	h01 := hashPair(fields[0], fields[1])
	h23 := hashPair(fields[2], fields[3])
	root := hashPair(h01, h23)

	proof, _ := BuildInclusionProof(fields[2], fields, 2)

	envelope := &UncoupledPayloadEnvelope{
		BeaconBlockRoot: root,
		Slot:            5,
		Payload: &ExecutionPayloadV5{
			ExecutionPayloadV4: ExecutionPayloadV4{
				ExecutionPayloadV3: ExecutionPayloadV3{
					ExecutionPayloadV2: ExecutionPayloadV2{
						ExecutionPayloadV1: ExecutionPayloadV1{
							BlockHash: types.Hash{0x01},
						},
					},
				},
			},
		},
		Proof: proof,
	}

	if err := handler.VerifyInclusion(envelope); err != nil {
		t.Fatalf("verify inclusion failed: %v", err)
	}
}

func TestUncoupledPayloadHandler_VerifyInclusion_Invalid(t *testing.T) {
	handler := NewUncoupledPayloadHandler(nil)

	envelope := &UncoupledPayloadEnvelope{
		BeaconBlockRoot: types.Hash{0xaa},
		Slot:            5,
		Payload: &ExecutionPayloadV5{
			ExecutionPayloadV4: ExecutionPayloadV4{
				ExecutionPayloadV3: ExecutionPayloadV3{
					ExecutionPayloadV2: ExecutionPayloadV2{
						ExecutionPayloadV1: ExecutionPayloadV1{
							BlockHash: types.Hash{0x01},
						},
					},
				},
			},
		},
		Proof: &InclusionProof{
			Leaf:   types.Hash{0xff},
			Branch: []types.Hash{{0x01}},
			Index:  0,
		},
	}

	if err := handler.VerifyInclusion(envelope); err == nil {
		t.Fatal("expected error for invalid proof")
	}
}

func TestPadToPowerOfTwo(t *testing.T) {
	tests := []struct {
		input int
		want  int
	}{
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
		hashes := make([]types.Hash, tt.input)
		padded := padToPowerOfTwo(hashes)
		if len(padded) != tt.want {
			t.Errorf("padToPowerOfTwo(%d) = %d, want %d", tt.input, len(padded), tt.want)
		}
	}
}

func TestHashPair_Deterministic(t *testing.T) {
	a := types.Hash{0x01}
	b := types.Hash{0x02}

	h1 := hashPair(a, b)
	h2 := hashPair(a, b)
	if h1 != h2 {
		t.Fatal("hashPair should be deterministic")
	}

	// Order matters.
	h3 := hashPair(b, a)
	if h1 == h3 {
		t.Fatal("hashPair(a,b) should differ from hashPair(b,a)")
	}
}

// Test 8-leaf tree for deeper proof.
func TestBuildInclusionProof_EightLeaves(t *testing.T) {
	leaves := make([]types.Hash, 8)
	for i := range leaves {
		leaves[i] = crypto.Keccak256Hash([]byte{byte(i)})
	}

	// Compute root layer by layer.
	layer := make([]types.Hash, 8)
	copy(layer, leaves)
	for len(layer) > 1 {
		next := make([]types.Hash, len(layer)/2)
		for i := 0; i < len(layer); i += 2 {
			next[i/2] = hashPair(layer[i], layer[i+1])
		}
		layer = next
	}
	root := layer[0]

	// Verify proof for every index.
	for i := 0; i < 8; i++ {
		proof, err := BuildInclusionProof(leaves[i], leaves, i)
		if err != nil {
			t.Fatalf("index %d: build failed: %v", i, err)
		}
		if err := ValidateInclusionProof(proof, root); err != nil {
			t.Fatalf("index %d: validation failed: %v", i, err)
		}
	}
}
