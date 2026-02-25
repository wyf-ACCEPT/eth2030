package verkle

import "testing"

func TestVerkleProofIsSufficiencyProof(t *testing.T) {
	val := [ValueSize]byte{0x01}
	proof := &VerkleProof{
		ExtensionPresent: true,
		Value:            &val,
	}
	if !proof.IsSufficiencyProof() {
		t.Error("expected IsSufficiencyProof=true when ExtensionPresent=true and Value!=nil")
	}
	if proof.IsAbsenceProof() {
		t.Error("expected IsAbsenceProof=false for sufficiency proof")
	}
}

func TestVerkleProofIsAbsenceProofNoExtension(t *testing.T) {
	proof := &VerkleProof{
		ExtensionPresent: false,
		Value:            nil,
	}
	if !proof.IsAbsenceProof() {
		t.Error("expected IsAbsenceProof=true when ExtensionPresent=false")
	}
	if proof.IsSufficiencyProof() {
		t.Error("expected IsSufficiencyProof=false for absence proof")
	}
}

func TestVerkleProofIsAbsenceProofNilValue(t *testing.T) {
	proof := &VerkleProof{
		ExtensionPresent: true,
		Value:            nil,
	}
	if !proof.IsAbsenceProof() {
		t.Error("expected IsAbsenceProof=true when Value=nil even with ExtensionPresent")
	}
	if proof.IsSufficiencyProof() {
		t.Error("expected IsSufficiencyProof=false when Value=nil")
	}
}

func TestVerkleProofVerifyValid(t *testing.T) {
	root := Commitment{0x01, 0x02}
	// Build a valid serialized IPA proof (absence proof with no value).
	absenceIPA := makeValidSerializedIPA()
	proof := &VerkleProof{
		CommitmentsByPath: []Commitment{{0xaa}},
		Depth:             3,
		IPAProof:          absenceIPA,
	}
	if !proof.Verify(root) {
		t.Error("expected Verify=true for structurally valid proof")
	}
}

// makeValidSerializedIPA creates a valid serialized IPAProofVerkle
// with 8 rounds (for NodeWidth=256) for use in tests.
func makeValidSerializedIPA() []byte {
	proof := &IPAProofVerkle{
		CL:          make([]Commitment, 8),
		CR:          make([]Commitment, 8),
		FinalScalar: One(),
	}
	for i := 0; i < 8; i++ {
		proof.CL[i] = Commitment{byte(i + 1)}
		proof.CR[i] = Commitment{byte(i + 0x10)}
	}
	data, _ := SerializeIPAProofVerkle(proof)
	return data
}

func TestVerkleProofVerifyEmptyCommitments(t *testing.T) {
	root := Commitment{0x01}
	proof := &VerkleProof{
		CommitmentsByPath: []Commitment{}, // empty
		Depth:             3,
		IPAProof:          []byte{0x01},
	}
	if proof.Verify(root) {
		t.Error("expected Verify=false when CommitmentsByPath is empty")
	}
}

func TestVerkleProofVerifyExceedsMaxDepth(t *testing.T) {
	root := Commitment{0x01}
	proof := &VerkleProof{
		CommitmentsByPath: []Commitment{{0xaa}},
		Depth:             uint8(MaxDepth + 1),
		IPAProof:          []byte{0x01},
	}
	if proof.Verify(root) {
		t.Error("expected Verify=false when Depth exceeds MaxDepth")
	}
}

func TestVerkleProofVerifyEmptyIPAProof(t *testing.T) {
	root := Commitment{0x01}
	proof := &VerkleProof{
		CommitmentsByPath: []Commitment{{0xaa}},
		Depth:             3,
		IPAProof:          []byte{}, // empty
	}
	if proof.Verify(root) {
		t.Error("expected Verify=false when IPAProof is empty")
	}
}

func TestVerkleProofVerifyAtMaxDepth(t *testing.T) {
	root := Commitment{0x01}
	absenceIPA := makeValidSerializedIPA()
	proof := &VerkleProof{
		CommitmentsByPath: []Commitment{{0xaa}},
		Depth:             uint8(MaxDepth), // exactly at max
		IPAProof:          absenceIPA,
	}
	if !proof.Verify(root) {
		t.Error("expected Verify=true at exactly MaxDepth")
	}
}

func TestVerkleProofKeyAndValue(t *testing.T) {
	var key [KeySize]byte
	for i := range key {
		key[i] = byte(i)
	}
	val := [ValueSize]byte{0xff}
	proof := &VerkleProof{
		Key:   key,
		Value: &val,
	}

	if proof.Key != key {
		t.Error("proof Key mismatch")
	}
	if *proof.Value != val {
		t.Error("proof Value mismatch")
	}
}

func TestProofElements(t *testing.T) {
	var stem [StemSize]byte
	stem[0] = 0xaa
	elem := &ProofElements{
		PathCommitments: []Commitment{{0x01}, {0x02}},
		Leaf:            NewLeafNode(stem),
		Depth:           5,
	}
	if len(elem.PathCommitments) != 2 {
		t.Errorf("PathCommitments count = %d, want 2", len(elem.PathCommitments))
	}
	if elem.Depth != 5 {
		t.Errorf("Depth = %d, want 5", elem.Depth)
	}
	if elem.Leaf == nil {
		t.Error("Leaf should not be nil")
	}
}
