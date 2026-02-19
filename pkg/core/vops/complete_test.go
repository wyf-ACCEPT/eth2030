package vops

import (
	"testing"

	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/crypto"
)

func TestNewVOPSValidator(t *testing.T) {
	v := NewVOPSValidator()
	if v == nil {
		t.Fatal("NewVOPSValidator returned nil")
	}
	if v.witnesses == nil {
		t.Error("witnesses map should be initialized")
	}
	if v.accessList == nil {
		t.Error("accessList map should be initialized")
	}
	if v.storageProofs == nil {
		t.Error("storageProofs map should be initialized")
	}
}

func TestAddWitness(t *testing.T) {
	v := NewVOPSValidator()
	root := types.HexToHash("0x01")
	witness := []byte{0xaa, 0xbb, 0xcc}

	if err := v.AddWitness(root, witness); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify stored.
	got, ok := v.witnesses[root]
	if !ok {
		t.Fatal("witness not found after AddWitness")
	}
	if len(got) != len(witness) {
		t.Errorf("witness length = %d, want %d", len(got), len(witness))
	}
	for i := range witness {
		if got[i] != witness[i] {
			t.Errorf("witness[%d] = %x, want %x", i, got[i], witness[i])
		}
	}

	// Verify it's a copy, not the original slice.
	witness[0] = 0xff
	if v.witnesses[root][0] == 0xff {
		t.Error("witness should be a defensive copy")
	}
}

func TestAddWitnessEmpty(t *testing.T) {
	v := NewVOPSValidator()
	root := types.HexToHash("0x01")

	err := v.AddWitness(root, nil)
	if err != ErrEmptyWitness {
		t.Errorf("expected ErrEmptyWitness, got %v", err)
	}

	err = v.AddWitness(root, []byte{})
	if err != ErrEmptyWitness {
		t.Errorf("expected ErrEmptyWitness for empty slice, got %v", err)
	}
}

func TestAddAccessListEntry(t *testing.T) {
	v := NewVOPSValidator()
	addr1 := types.BytesToAddress([]byte{0x01})
	addr2 := types.BytesToAddress([]byte{0x02})

	v.AddAccessListEntry(addr1)
	v.AddAccessListEntry(addr2)

	if !v.accessList[addr1] {
		t.Error("addr1 should be in access list")
	}
	if !v.accessList[addr2] {
		t.Error("addr2 should be in access list")
	}

	// Adding the same address again is idempotent.
	v.AddAccessListEntry(addr1)
	if len(v.accessList) != 2 {
		t.Errorf("access list size = %d, want 2 (duplicates should be ignored)", len(v.accessList))
	}
}

func TestAddStorageProof(t *testing.T) {
	v := NewVOPSValidator()
	slot := types.HexToHash("0x10")
	proof := [][]byte{{0x01, 0x02}, {0x03, 0x04}}

	v.AddStorageProof(slot, proof)

	got, ok := v.storageProofs[slot]
	if !ok {
		t.Fatal("storage proof not found")
	}
	if len(got) != 2 {
		t.Fatalf("proof nodes = %d, want 2", len(got))
	}
	if got[0][0] != 0x01 || got[0][1] != 0x02 {
		t.Error("first proof node mismatch")
	}

	// Verify deep copy.
	proof[0][0] = 0xff
	if v.storageProofs[slot][0][0] == 0xff {
		t.Error("storage proof should be a defensive copy")
	}
}

func TestValidateTransitionComplete(t *testing.T) {
	v := NewVOPSValidator()
	preRoot := types.HexToHash("0x01")
	witnessData := []byte{0xaa, 0xbb}
	blockData := []byte{0xcc, 0xdd}

	_ = v.AddWitness(preRoot, witnessData)

	// Compute the expected post root the same way the validator does.
	expectedPost := computePostRoot(preRoot, witnessData, blockData)

	ok, err := v.ValidateTransition(preRoot, expectedPost, blockData)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Error("valid transition should succeed")
	}
}

func TestValidateTransitionMissingWitness(t *testing.T) {
	v := NewVOPSValidator()
	preRoot := types.HexToHash("0x01")
	postRoot := types.HexToHash("0x02")

	ok, err := v.ValidateTransition(preRoot, postRoot, []byte{0x01})
	if err != ErrWitnessNotFound {
		t.Errorf("expected ErrWitnessNotFound, got %v", err)
	}
	if ok {
		t.Error("should not validate with missing witness")
	}
}

func TestValidateTransitionEmptyBlock(t *testing.T) {
	v := NewVOPSValidator()
	preRoot := types.HexToHash("0x01")
	postRoot := types.HexToHash("0x02")

	_ = v.AddWitness(preRoot, []byte{0xaa})

	ok, err := v.ValidateTransition(preRoot, postRoot, nil)
	if err != ErrEmptyBlock {
		t.Errorf("expected ErrEmptyBlock, got %v", err)
	}
	if ok {
		t.Error("should not validate with empty block")
	}
}

func TestValidateTransitionMismatch(t *testing.T) {
	v := NewVOPSValidator()
	preRoot := types.HexToHash("0x01")
	witnessData := []byte{0xaa, 0xbb}
	blockData := []byte{0xcc, 0xdd}

	_ = v.AddWitness(preRoot, witnessData)

	// Provide a wrong post root.
	wrongPost := types.HexToHash("0xdeadbeef")
	ok, err := v.ValidateTransition(preRoot, wrongPost, blockData)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Error("mismatched post root should fail validation")
	}
}

func TestValidateReceipt(t *testing.T) {
	txHash := types.HexToHash("0xaabb")
	receipt := []byte{0x01, 0x02, 0x03}

	// Compute the expected receipt root.
	receiptRoot := crypto.Keccak256Hash(txHash[:], receipt)

	v := NewVOPSValidator()
	if !v.ValidateReceipt(txHash, receipt, receiptRoot) {
		t.Error("valid receipt should verify")
	}
}

func TestValidateReceiptInvalid(t *testing.T) {
	v := NewVOPSValidator()
	txHash := types.HexToHash("0xaabb")
	receipt := []byte{0x01, 0x02, 0x03}
	wrongRoot := types.HexToHash("0xdead")

	if v.ValidateReceipt(txHash, receipt, wrongRoot) {
		t.Error("wrong root should fail receipt validation")
	}
}

func TestValidateReceiptEmpty(t *testing.T) {
	v := NewVOPSValidator()
	txHash := types.HexToHash("0xaabb")
	root := types.HexToHash("0x01")

	if v.ValidateReceipt(txHash, nil, root) {
		t.Error("nil receipt should fail validation")
	}
	if v.ValidateReceipt(txHash, []byte{}, root) {
		t.Error("empty receipt should fail validation")
	}
}

func TestWitnessSize(t *testing.T) {
	v := NewVOPSValidator()

	if v.WitnessSize() != 0 {
		t.Errorf("empty validator witness size = %d, want 0", v.WitnessSize())
	}

	root1 := types.HexToHash("0x01")
	root2 := types.HexToHash("0x02")
	_ = v.AddWitness(root1, []byte{0x01, 0x02, 0x03})       // 3 bytes
	_ = v.AddWitness(root2, []byte{0x04, 0x05, 0x06, 0x07}) // 4 bytes

	if v.WitnessSize() != 7 {
		t.Errorf("witness size = %d, want 7", v.WitnessSize())
	}
}

func TestAccessedAddresses(t *testing.T) {
	v := NewVOPSValidator()

	addrs := v.AccessedAddresses()
	if len(addrs) != 0 {
		t.Errorf("empty validator should have 0 accessed addresses, got %d", len(addrs))
	}

	addr1 := types.BytesToAddress([]byte{0x01})
	addr2 := types.BytesToAddress([]byte{0x02})
	addr3 := types.BytesToAddress([]byte{0x03})

	v.AddAccessListEntry(addr1)
	v.AddAccessListEntry(addr2)
	v.AddAccessListEntry(addr3)

	addrs = v.AccessedAddresses()
	if len(addrs) != 3 {
		t.Errorf("accessed addresses = %d, want 3", len(addrs))
	}

	// Verify all addresses are present.
	found := make(map[types.Address]bool)
	for _, a := range addrs {
		found[a] = true
	}
	for _, want := range []types.Address{addr1, addr2, addr3} {
		if !found[want] {
			t.Errorf("address %v not found in accessed addresses", want)
		}
	}
}

func TestReset(t *testing.T) {
	v := NewVOPSValidator()

	// Populate state.
	root := types.HexToHash("0x01")
	_ = v.AddWitness(root, []byte{0xaa})
	v.AddAccessListEntry(types.BytesToAddress([]byte{0x01}))
	v.AddStorageProof(types.HexToHash("0x10"), [][]byte{{0x01}})

	// Verify non-empty.
	if v.WitnessSize() == 0 {
		t.Fatal("should have witnesses before reset")
	}
	if len(v.AccessedAddresses()) == 0 {
		t.Fatal("should have accessed addresses before reset")
	}
	if len(v.storageProofs) == 0 {
		t.Fatal("should have storage proofs before reset")
	}

	// Reset and verify empty.
	v.Reset()

	if v.WitnessSize() != 0 {
		t.Errorf("witness size after reset = %d, want 0", v.WitnessSize())
	}
	if len(v.AccessedAddresses()) != 0 {
		t.Errorf("accessed addresses after reset = %d, want 0", len(v.AccessedAddresses()))
	}
	if len(v.storageProofs) != 0 {
		t.Errorf("storage proofs after reset = %d, want 0", len(v.storageProofs))
	}
}
