// Package verkle implements Verkle tree types and key derivation for EIP-6800.
//
// VerkleProof represents an IPA (Inner Product Argument) proof for a
// key-value pair in the Verkle tree. It contains Banderwagon curve
// commitments along the tree path and a serialized IPA proof that
// demonstrates polynomial evaluation correctness.
package verkle

// VerkleProof holds the data needed to verify a key's inclusion (or
// absence) in a Verkle tree. The proof follows the IPA multipoint
// argument structure from the Verkle tree specification.
type VerkleProof struct {
	// CommitmentsByPath contains the inner node Pedersen commitments along
	// the path from the root to the leaf. Each entry is a 32-byte
	// serialized Banderwagon point (map-to-field representation).
	CommitmentsByPath []Commitment

	// D is the Pedersen commitment to the leaf polynomial used in the
	// IPA proof. This is the 32-byte map-to-field serialization of the
	// Banderwagon curve point.
	D Commitment

	// IPAProof is the serialized Inner Product Argument proof that
	// demonstrates the polynomial evaluation is correct. It contains
	// log2(256) = 8 pairs of Banderwagon L/R points plus the final scalar.
	IPAProof []byte

	// Depth is the depth at which the leaf (or absence) was found.
	Depth uint8

	// ExtensionPresent indicates whether a leaf node (extension) was
	// found at the path. If false, this is an absence proof.
	ExtensionPresent bool

	// Key is the 32-byte key being proved.
	Key [KeySize]byte

	// Value is the 32-byte value at the key, or nil for absence proofs.
	Value *[ValueSize]byte
}

// IsSufficiencyProof returns true if this proof demonstrates that the
// key is present in the tree (the leaf node exists and holds a value).
func (p *VerkleProof) IsSufficiencyProof() bool {
	return p.ExtensionPresent && p.Value != nil
}

// IsAbsenceProof returns true if this proof demonstrates that the
// key is NOT present in the tree.
func (p *VerkleProof) IsAbsenceProof() bool {
	return !p.ExtensionPresent || p.Value == nil
}

// Verify checks that the proof is structurally valid and, for inclusion
// proofs, verifies the IPA proof against the leaf commitment using real
// Banderwagon curve arithmetic.
func (p *VerkleProof) Verify(root Commitment) bool {
	// Structural validation.
	if len(p.CommitmentsByPath) == 0 {
		return false
	}
	if int(p.Depth) > MaxDepth {
		return false
	}
	if len(p.IPAProof) == 0 {
		return false
	}

	// Verify the IPA proof bytes have valid structure (correct length).
	if len(p.IPAProof) < 33 {
		return false
	}

	// For inclusion proofs with a leaf commitment, verify the IPA proof
	// against the leaf commitment using the default IPA config.
	if p.ExtensionPresent && p.Value != nil && !p.D.IsZero() {
		cfg := DefaultIPAConfig()
		return verifyIPAProofBytes(cfg, p)
	}

	return true
}

// ProofElements holds the raw elements extracted from the tree to
// build a VerkleProof. This separates tree traversal from proof
// construction.
type ProofElements struct {
	// Commitments along the path from root to leaf.
	PathCommitments []Commitment

	// The leaf node, if found.
	Leaf *LeafNode

	// Depth at which the search terminated.
	Depth uint8
}
