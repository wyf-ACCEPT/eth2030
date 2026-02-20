package witness

import (
	"math/big"
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

// buildTestStateWitness creates a StateWitness for testing.
func buildTestStateWitness(t *testing.T) *StateWitness {
	t.Helper()
	b := NewStateWitnessBuilder(100, types.HexToHash("0xaabb"))
	addr1 := types.HexToAddress("0x01")
	addr2 := types.HexToAddress("0x02")

	b.RecordAccount(addr1, true, 5, big.NewInt(1000), types.HexToHash("0xcc"))
	b.RecordStorage(addr1, types.HexToHash("0x10"), types.HexToHash("0x20"))
	b.RecordStorage(addr1, types.HexToHash("0x30"), types.HexToHash("0x40"))

	b.RecordAccount(addr2, true, 10, big.NewInt(2000), types.HexToHash("0xdd"))
	b.RecordStorage(addr2, types.HexToHash("0x50"), types.HexToHash("0x60"))

	sw, err := b.Finalize()
	if err != nil {
		t.Fatalf("buildTestStateWitness: %v", err)
	}
	return sw
}

func TestWitnessProofGeneratorAccountProof(t *testing.T) {
	sw := buildTestStateWitness(t)
	gen := NewWitnessProofGenerator(ProofTreeDepth, MaxProofBundleSize)

	addr := types.HexToAddress("0x01")
	proof, err := gen.GenerateAccountProof(sw, addr)
	if err != nil {
		t.Fatalf("GenerateAccountProof: %v", err)
	}

	if proof.StateRoot != sw.StateRoot {
		t.Fatalf("state root mismatch")
	}
	if proof.Address != addr {
		t.Fatalf("address mismatch")
	}
	if proof.AddressKey.IsZero() {
		t.Fatal("address key should not be zero")
	}
	if !proof.Exists {
		t.Fatal("expected account to exist")
	}
	if len(proof.Nodes) != ProofTreeDepth {
		t.Fatalf("expected %d nodes, got %d", ProofTreeDepth, len(proof.Nodes))
	}
}

func TestWitnessProofGeneratorAccountProofNilWitness(t *testing.T) {
	gen := NewWitnessProofGenerator(0, 0)
	_, err := gen.GenerateAccountProof(nil, types.Address{})
	if err != ErrProofGenNilWitness {
		t.Fatalf("expected ErrProofGenNilWitness, got %v", err)
	}
}

func TestWitnessProofGeneratorAccountProofZeroRoot(t *testing.T) {
	sw := &StateWitness{StateRoot: types.Hash{}}
	gen := NewWitnessProofGenerator(8, 0)
	_, err := gen.GenerateAccountProof(sw, types.HexToAddress("0x01"))
	if err != ErrProofGenNilRoot {
		t.Fatalf("expected ErrProofGenNilRoot, got %v", err)
	}
}

func TestWitnessProofGeneratorAccountProofNotFound(t *testing.T) {
	sw := buildTestStateWitness(t)
	gen := NewWitnessProofGenerator(8, 0)
	_, err := gen.GenerateAccountProof(sw, types.HexToAddress("0xff"))
	if err == nil {
		t.Fatal("expected error for missing address")
	}
}

func TestWitnessProofGeneratorStorageProof(t *testing.T) {
	sw := buildTestStateWitness(t)
	gen := NewWitnessProofGenerator(ProofTreeDepth, MaxProofBundleSize)

	addr := types.HexToAddress("0x01")
	slotKey := types.HexToHash("0x10")
	proof, err := gen.GenerateStorageProof(sw, addr, slotKey)
	if err != nil {
		t.Fatalf("GenerateStorageProof: %v", err)
	}

	if proof.Address != addr {
		t.Fatalf("address mismatch")
	}
	if proof.SlotKey != slotKey {
		t.Fatalf("slot key mismatch")
	}
	if proof.Value != types.HexToHash("0x20") {
		t.Fatalf("value mismatch")
	}
	if proof.StorageRoot.IsZero() {
		t.Fatal("storage root should not be zero")
	}
	if proof.SlotHash.IsZero() {
		t.Fatal("slot hash should not be zero")
	}
	if len(proof.Nodes) != ProofTreeDepth {
		t.Fatalf("expected %d nodes, got %d", ProofTreeDepth, len(proof.Nodes))
	}
}

func TestWitnessProofGeneratorStorageProofNotFound(t *testing.T) {
	sw := buildTestStateWitness(t)
	gen := NewWitnessProofGenerator(8, 0)

	// Address exists but slot does not.
	_, err := gen.GenerateStorageProof(sw, types.HexToAddress("0x01"), types.HexToHash("0xff"))
	if err == nil {
		t.Fatal("expected error for missing slot")
	}

	// Address does not exist.
	_, err = gen.GenerateStorageProof(sw, types.HexToAddress("0xff"), types.HexToHash("0x10"))
	if err == nil {
		t.Fatal("expected error for missing address")
	}
}

func TestWitnessProofGeneratorProofBundle(t *testing.T) {
	sw := buildTestStateWitness(t)
	gen := NewWitnessProofGenerator(ProofTreeDepth, MaxProofBundleSize)

	bundle, err := gen.GenerateProofBundle(sw)
	if err != nil {
		t.Fatalf("GenerateProofBundle: %v", err)
	}

	if bundle.StateRoot != sw.StateRoot {
		t.Fatalf("state root mismatch")
	}
	if len(bundle.AccountProofs) != 2 { // addr1 and addr2
		t.Fatalf("expected 2 account proofs, got %d", len(bundle.AccountProofs))
	}
	if len(bundle.StorageProofs) != 3 { // addr1: 2 slots, addr2: 1 slot
		t.Fatalf("expected 3 storage proofs, got %d", len(bundle.StorageProofs))
	}
	if len(bundle.SharedNodes) == 0 {
		t.Fatal("expected shared nodes to be populated")
	}
	if bundle.TotalSize <= 0 {
		t.Fatal("expected positive total size")
	}
}

func TestWitnessProofGeneratorProofBundleNilWitness(t *testing.T) {
	gen := NewWitnessProofGenerator(8, 0)
	_, err := gen.GenerateProofBundle(nil)
	if err != ErrProofGenNilWitness {
		t.Fatalf("expected ErrProofGenNilWitness, got %v", err)
	}
}

func TestWitnessProofGeneratorProofBundleZeroRoot(t *testing.T) {
	sw := &StateWitness{StateRoot: types.Hash{}}
	gen := NewWitnessProofGenerator(8, 0)
	_, err := gen.GenerateProofBundle(sw)
	if err != ErrProofGenNilRoot {
		t.Fatalf("expected ErrProofGenNilRoot, got %v", err)
	}
}

func TestWitnessProofGeneratorProofBundleNoAccounts(t *testing.T) {
	sw := &StateWitness{
		StateRoot: types.HexToHash("0xaa"),
		Accounts:  make(map[types.Address]*StateWitnessAccount),
	}
	gen := NewWitnessProofGenerator(8, 0)
	_, err := gen.GenerateProofBundle(sw)
	if err != ErrProofGenNoAccounts {
		t.Fatalf("expected ErrProofGenNoAccounts, got %v", err)
	}
}

func TestWitnessProofGeneratorProofBundleTooLarge(t *testing.T) {
	sw := buildTestStateWitness(t)
	// Set max size very small to trigger the error.
	gen := NewWitnessProofGenerator(ProofTreeDepth, 1)
	_, err := gen.GenerateProofBundle(sw)
	if err == nil {
		t.Fatal("expected ErrProofBundleTooLarge")
	}
}

func TestVerifyAccountInclusionProof(t *testing.T) {
	sw := buildTestStateWitness(t)
	gen := NewWitnessProofGenerator(ProofTreeDepth, MaxProofBundleSize)

	addr := types.HexToAddress("0x01")
	proof, _ := gen.GenerateAccountProof(sw, addr)

	if !VerifyAccountInclusionProof(proof) {
		t.Fatal("expected valid account proof")
	}

	// Nil proof.
	if VerifyAccountInclusionProof(nil) {
		t.Fatal("expected false for nil proof")
	}

	// Empty nodes.
	bad := *proof
	bad.Nodes = nil
	if VerifyAccountInclusionProof(&bad) {
		t.Fatal("expected false for empty nodes")
	}

	// Zero state root.
	bad2 := *proof
	bad2.StateRoot = types.Hash{}
	if VerifyAccountInclusionProof(&bad2) {
		t.Fatal("expected false for zero state root")
	}

	// Tampered node data.
	bad3 := *proof
	bad3.Nodes = make([]WitnessProofNode, len(proof.Nodes))
	copy(bad3.Nodes, proof.Nodes)
	bad3.Nodes[0].Data = []byte{0xff}
	if VerifyAccountInclusionProof(&bad3) {
		t.Fatal("expected false for tampered node")
	}
}

func TestVerifyStorageInclusionProof(t *testing.T) {
	sw := buildTestStateWitness(t)
	gen := NewWitnessProofGenerator(ProofTreeDepth, MaxProofBundleSize)

	proof, _ := gen.GenerateStorageProof(sw, types.HexToAddress("0x01"), types.HexToHash("0x10"))

	if !VerifyStorageInclusionProof(proof) {
		t.Fatal("expected valid storage proof")
	}

	// Nil proof.
	if VerifyStorageInclusionProof(nil) {
		t.Fatal("expected false for nil proof")
	}

	// Zero storage root.
	bad := *proof
	bad.StorageRoot = types.Hash{}
	if VerifyStorageInclusionProof(&bad) {
		t.Fatal("expected false for zero storage root")
	}
}

func TestVerifyProofBundle(t *testing.T) {
	sw := buildTestStateWitness(t)
	gen := NewWitnessProofGenerator(ProofTreeDepth, MaxProofBundleSize)

	bundle, _ := gen.GenerateProofBundle(sw)
	if !VerifyProofBundle(bundle) {
		t.Fatal("expected valid proof bundle")
	}

	if VerifyProofBundle(nil) {
		t.Fatal("expected false for nil bundle")
	}
}

func TestComputeProofBundleStats(t *testing.T) {
	sw := buildTestStateWitness(t)
	gen := NewWitnessProofGenerator(ProofTreeDepth, MaxProofBundleSize)

	bundle, _ := gen.GenerateProofBundle(sw)
	stats := ComputeProofBundleStats(bundle)

	if stats.AccountProofCount != 2 {
		t.Fatalf("expected 2 account proofs, got %d", stats.AccountProofCount)
	}
	if stats.StorageProofCount != 3 {
		t.Fatalf("expected 3 storage proofs, got %d", stats.StorageProofCount)
	}
	if stats.UniqueNodeCount == 0 {
		t.Fatal("expected non-zero unique node count")
	}
	if stats.TotalSize <= 0 {
		t.Fatal("expected positive total size")
	}
}

func TestComputeProofBundleStatsNil(t *testing.T) {
	stats := ComputeProofBundleStats(nil)
	if stats.AccountProofCount != 0 || stats.StorageProofCount != 0 {
		t.Fatal("expected zero stats for nil bundle")
	}
}

func TestWitnessProofGeneratorGeneratedCount(t *testing.T) {
	sw := buildTestStateWitness(t)
	gen := NewWitnessProofGenerator(ProofTreeDepth, MaxProofBundleSize)

	if gen.GeneratedCount() != 0 {
		t.Fatalf("expected 0 generated initially, got %d", gen.GeneratedCount())
	}

	gen.GenerateAccountProof(sw, types.HexToAddress("0x01"))
	gen.GenerateStorageProof(sw, types.HexToAddress("0x01"), types.HexToHash("0x10"))

	if gen.GeneratedCount() != 2 {
		t.Fatalf("expected 2 generated, got %d", gen.GeneratedCount())
	}
}

func TestWitnessProofGeneratorDefaultDepth(t *testing.T) {
	// depth 0 should default to ProofTreeDepth.
	gen := NewWitnessProofGenerator(0, 0)
	sw := buildTestStateWitness(t)
	proof, err := gen.GenerateAccountProof(sw, types.HexToAddress("0x01"))
	if err != nil {
		t.Fatalf("GenerateAccountProof: %v", err)
	}
	if len(proof.Nodes) != ProofTreeDepth {
		t.Fatalf("expected %d nodes with default depth, got %d",
			ProofTreeDepth, len(proof.Nodes))
	}
}

func TestWitnessProofGeneratorCustomDepth(t *testing.T) {
	gen := NewWitnessProofGenerator(4, MaxProofBundleSize)
	sw := buildTestStateWitness(t)
	proof, err := gen.GenerateAccountProof(sw, types.HexToAddress("0x01"))
	if err != nil {
		t.Fatalf("GenerateAccountProof: %v", err)
	}
	if len(proof.Nodes) != 4 {
		t.Fatalf("expected 4 nodes with depth 4, got %d", len(proof.Nodes))
	}
}

func TestWitnessProofBundleDeterministic(t *testing.T) {
	// Two identical witnesses should produce identical bundles.
	buildSW := func() *StateWitness {
		b := NewStateWitnessBuilder(100, types.HexToHash("0xaabb"))
		b.RecordAccount(types.HexToAddress("0x01"), true, 5, big.NewInt(1000), types.HexToHash("0xcc"))
		b.RecordStorage(types.HexToAddress("0x01"), types.HexToHash("0x10"), types.HexToHash("0x20"))
		b.RecordAccount(types.HexToAddress("0x02"), true, 10, big.NewInt(2000), types.HexToHash("0xdd"))
		sw, _ := b.Finalize()
		return sw
	}

	gen := NewWitnessProofGenerator(ProofTreeDepth, MaxProofBundleSize)

	sw1 := buildSW()
	sw2 := buildSW()

	bundle1, _ := gen.GenerateProofBundle(sw1)
	bundle2, _ := gen.GenerateProofBundle(sw2)

	if len(bundle1.AccountProofs) != len(bundle2.AccountProofs) {
		t.Fatal("account proof count mismatch")
	}
	if len(bundle1.StorageProofs) != len(bundle2.StorageProofs) {
		t.Fatal("storage proof count mismatch")
	}
	if len(bundle1.SharedNodes) != len(bundle2.SharedNodes) {
		t.Fatal("shared node count mismatch")
	}

	// Verify node hashes match.
	for h := range bundle1.SharedNodes {
		if _, ok := bundle2.SharedNodes[h]; !ok {
			t.Fatalf("shared node %s in bundle1 not found in bundle2", h.Hex())
		}
	}
}

func TestVerifyProofBundleTampered(t *testing.T) {
	sw := buildTestStateWitness(t)
	gen := NewWitnessProofGenerator(ProofTreeDepth, MaxProofBundleSize)

	bundle, _ := gen.GenerateProofBundle(sw)

	// Tamper with an account proof node.
	if len(bundle.AccountProofs) > 0 && len(bundle.AccountProofs[0].Nodes) > 0 {
		bundle.AccountProofs[0].Nodes[0].Data = []byte{0xba, 0xad}
		if VerifyProofBundle(bundle) {
			t.Fatal("expected false for tampered bundle")
		}
	}
}
