package vops

import (
	"math/big"
	"testing"

	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/crypto"
)

func testProofState() (*PartialState, types.Hash) {
	ps := NewPartialState()
	addr := types.BytesToAddress([]byte{0x10})
	ps.SetAccount(addr, &AccountState{
		Nonce:       1,
		Balance:     big.NewInt(1000),
		CodeHash:    types.EmptyCodeHash,
		StorageRoot: types.EmptyRootHash,
	})
	ps.SetStorage(addr, types.HexToHash("0x01"), types.HexToHash("0xaa"))
	stateRoot := types.HexToHash("0xbeef")
	return ps, stateRoot
}

func TestNewProofChecker(t *testing.T) {
	root := types.HexToHash("0x01")
	pc := NewProofChecker(root)
	if pc == nil {
		t.Fatal("NewProofChecker returned nil")
	}
	if pc.StateRoot() != root {
		t.Errorf("StateRoot = %v, want %v", pc.StateRoot(), root)
	}
}

func TestProofCheckerVerifyAccountProof(t *testing.T) {
	ps, stateRoot := testProofState()
	proofSet := BuildProofSet(stateRoot, ps)
	pc := NewProofChecker(stateRoot)

	// Verify the first account proof.
	if len(proofSet.AccountProofs) == 0 {
		t.Fatal("no account proofs")
	}
	err := pc.VerifyAccountProof(&proofSet.AccountProofs[0])
	if err != nil {
		t.Errorf("VerifyAccountProof: %v", err)
	}
}

func TestProofCheckerVerifyAccountProofNil(t *testing.T) {
	pc := NewProofChecker(types.HexToHash("0x01"))
	err := pc.VerifyAccountProof(nil)
	if err != ErrProofNil {
		t.Errorf("expected ErrProofNil, got %v", err)
	}
}

func TestProofCheckerVerifyAccountProofEmpty(t *testing.T) {
	pc := NewProofChecker(types.HexToHash("0x01"))
	err := pc.VerifyAccountProof(&AccountProof{
		Address:    types.BytesToAddress([]byte{0x10}),
		ProofNodes: nil,
	})
	if err != ErrProofEmpty {
		t.Errorf("expected ErrProofEmpty, got %v", err)
	}
}

func TestProofCheckerVerifyAccountProofWrongRoot(t *testing.T) {
	ps, stateRoot := testProofState()
	proofSet := BuildProofSet(stateRoot, ps)

	// Use a different state root for the checker.
	wrongRoot := types.HexToHash("0xdead")
	pc := NewProofChecker(wrongRoot)

	err := pc.VerifyAccountProof(&proofSet.AccountProofs[0])
	if err != ErrStateRootMismatch {
		t.Errorf("expected ErrStateRootMismatch, got %v", err)
	}
}

func TestProofCheckerVerifyAccountProofInvalidNode(t *testing.T) {
	stateRoot := types.HexToHash("0x01")
	pc := NewProofChecker(stateRoot)

	proof := &AccountProof{
		Address:     types.BytesToAddress([]byte{0x10}),
		Nonce:       1,
		Balance:     big.NewInt(100),
		CodeHash:    types.EmptyCodeHash,
		StorageRoot: types.EmptyRootHash,
		ProofNodes:  [][]byte{{}}, // empty node
	}
	err := pc.VerifyAccountProof(proof)
	if err != ErrInvalidProofNode {
		t.Errorf("expected ErrInvalidProofNode, got %v", err)
	}
}

func TestProofCheckerVerifyStorageProof(t *testing.T) {
	ps, stateRoot := testProofState()
	proofSet := BuildProofSet(stateRoot, ps)
	pc := NewProofChecker(stateRoot)

	if len(proofSet.StorageProofs) == 0 {
		t.Fatal("no storage proofs")
	}
	if len(proofSet.AccountProofs) == 0 {
		t.Fatal("no account proofs")
	}
	accountStorageRoot := proofSet.AccountProofs[0].StorageRoot
	err := pc.VerifyStorageProof(accountStorageRoot, &proofSet.StorageProofs[0])
	if err != nil {
		t.Errorf("VerifyStorageProof: %v", err)
	}
}

func TestProofCheckerVerifyStorageProofNil(t *testing.T) {
	pc := NewProofChecker(types.HexToHash("0x01"))
	err := pc.VerifyStorageProof(types.EmptyRootHash, nil)
	if err != ErrProofNil {
		t.Errorf("expected ErrProofNil, got %v", err)
	}
}

func TestProofCheckerVerifyStorageProofEmpty(t *testing.T) {
	pc := NewProofChecker(types.HexToHash("0x01"))
	err := pc.VerifyStorageProof(types.EmptyRootHash, &StorageProof{
		ProofNodes: nil,
	})
	if err != ErrProofEmpty {
		t.Errorf("expected ErrProofEmpty, got %v", err)
	}
}

func TestProofCheckerVerifyStorageProofWrongRoot(t *testing.T) {
	ps, stateRoot := testProofState()
	proofSet := BuildProofSet(stateRoot, ps)
	pc := NewProofChecker(stateRoot)

	if len(proofSet.StorageProofs) == 0 {
		t.Fatal("no storage proofs")
	}
	wrongRoot := types.HexToHash("0xdead")
	err := pc.VerifyStorageProof(wrongRoot, &proofSet.StorageProofs[0])
	if err != ErrStateRootMismatch {
		t.Errorf("expected ErrStateRootMismatch, got %v", err)
	}
}

func TestProofCheckerVerifyCodeProof(t *testing.T) {
	code := []byte{0x60, 0x00, 0x60, 0x00, 0xf3}
	codeHash := crypto.Keccak256Hash(code)
	pc := NewProofChecker(types.HexToHash("0x01"))

	proof := &CodeProof{
		Address:  types.BytesToAddress([]byte{0x10}),
		Code:     code,
		CodeHash: codeHash,
	}

	err := pc.VerifyCodeProof(proof, codeHash)
	if err != nil {
		t.Errorf("VerifyCodeProof: %v", err)
	}
}

func TestProofCheckerVerifyCodeProofNil(t *testing.T) {
	pc := NewProofChecker(types.HexToHash("0x01"))
	err := pc.VerifyCodeProof(nil, types.EmptyCodeHash)
	if err != ErrProofNil {
		t.Errorf("expected ErrProofNil, got %v", err)
	}
}

func TestProofCheckerVerifyCodeProofMismatch(t *testing.T) {
	code := []byte{0x60, 0x00}
	wrongCodeHash := types.HexToHash("0xdead")
	pc := NewProofChecker(types.HexToHash("0x01"))

	proof := &CodeProof{
		Address:  types.BytesToAddress([]byte{0x10}),
		Code:     code,
		CodeHash: crypto.Keccak256Hash(code),
	}

	err := pc.VerifyCodeProof(proof, wrongCodeHash)
	if err != ErrCodeHashMismatch {
		t.Errorf("expected ErrCodeHashMismatch, got %v", err)
	}
}

func TestProofCheckerVerifyCodeProofEmptyCode(t *testing.T) {
	pc := NewProofChecker(types.HexToHash("0x01"))
	// Empty code with non-empty code hash should fail.
	proof := &CodeProof{
		Address: types.BytesToAddress([]byte{0x10}),
		Code:    nil,
	}
	err := pc.VerifyCodeProof(proof, types.HexToHash("0xdead"))
	if err != ErrCodeNotFound {
		t.Errorf("expected ErrCodeNotFound, got %v", err)
	}
}

func TestProofCheckerVerifyCodeProofEmptyCodeEmptyHash(t *testing.T) {
	pc := NewProofChecker(types.HexToHash("0x01"))
	// Empty code with EmptyCodeHash should pass (code hash matches).
	proof := &CodeProof{
		Address: types.BytesToAddress([]byte{0x10}),
		Code:    nil,
	}
	err := pc.VerifyCodeProof(proof, types.EmptyCodeHash)
	if err != nil {
		t.Errorf("empty code with EmptyCodeHash should pass, got: %v", err)
	}
}

func TestProofCheckerVerifyProofSet(t *testing.T) {
	ps, stateRoot := testProofState()
	// Add code to the state.
	addr := types.BytesToAddress([]byte{0x10})
	code := []byte{0x60, 0x00}
	ps.Code[addr] = code
	ps.Accounts[addr].CodeHash = crypto.Keccak256Hash(code)

	proofSet := BuildProofSet(stateRoot, ps)
	pc := NewProofChecker(stateRoot)

	result := pc.VerifyProofSet(proofSet)
	if !result.Valid {
		t.Errorf("expected valid result, got errors: %v", result.Errors)
	}
	if result.AccountsChecked != 1 {
		t.Errorf("AccountsChecked = %d, want 1", result.AccountsChecked)
	}
	if result.StorageChecked != 1 {
		t.Errorf("StorageChecked = %d, want 1", result.StorageChecked)
	}
	if result.CodeChecked != 1 {
		t.Errorf("CodeChecked = %d, want 1", result.CodeChecked)
	}
}

func TestProofCheckerVerifyProofSetNil(t *testing.T) {
	pc := NewProofChecker(types.HexToHash("0x01"))
	result := pc.VerifyProofSet(nil)
	if result.Valid {
		t.Error("nil proof set should be invalid")
	}
	if len(result.Errors) == 0 {
		t.Error("expected errors for nil proof set")
	}
}

func TestProofCheckerVerifyProofSetWrongStateRoot(t *testing.T) {
	ps, stateRoot := testProofState()
	proofSet := BuildProofSet(stateRoot, ps)

	wrongRoot := types.HexToHash("0xdead")
	pc := NewProofChecker(wrongRoot)

	result := pc.VerifyProofSet(proofSet)
	if result.Valid {
		t.Error("wrong state root should be invalid")
	}
}

func TestProofCheckerVerifyProofSetMissingAccount(t *testing.T) {
	stateRoot := types.HexToHash("0xbeef")
	pc := NewProofChecker(stateRoot)

	// Proof set with storage proof but no matching account proof.
	proofSet := &ProofSet{
		StateRoot: stateRoot,
		StorageProofs: []StorageProof{
			{
				Address:    types.BytesToAddress([]byte{0x99}),
				Slot:       types.HexToHash("0x01"),
				Value:      types.HexToHash("0xaa"),
				ProofNodes: [][]byte{{0x01}},
			},
		},
	}

	result := pc.VerifyProofSet(proofSet)
	if result.Valid {
		t.Error("storage proof without account proof should be invalid")
	}
	if result.StorageChecked != 1 {
		t.Errorf("StorageChecked = %d, want 1", result.StorageChecked)
	}
}

func TestBuildProofSet(t *testing.T) {
	ps, stateRoot := testProofState()
	proofSet := BuildProofSet(stateRoot, ps)

	if proofSet.StateRoot != stateRoot {
		t.Errorf("StateRoot = %v, want %v", proofSet.StateRoot, stateRoot)
	}
	if len(proofSet.AccountProofs) != 1 {
		t.Fatalf("AccountProofs = %d, want 1", len(proofSet.AccountProofs))
	}
	if len(proofSet.StorageProofs) != 1 {
		t.Fatalf("StorageProofs = %d, want 1", len(proofSet.StorageProofs))
	}
	// No code in this test state.
	if len(proofSet.CodeProofs) != 0 {
		t.Errorf("CodeProofs = %d, want 0", len(proofSet.CodeProofs))
	}
}

func TestBuildProofSetNilState(t *testing.T) {
	stateRoot := types.HexToHash("0x01")
	proofSet := BuildProofSet(stateRoot, nil)
	if proofSet.StateRoot != stateRoot {
		t.Errorf("StateRoot = %v, want %v", proofSet.StateRoot, stateRoot)
	}
	if len(proofSet.AccountProofs) != 0 {
		t.Error("nil state should produce empty account proofs")
	}
}

func TestBuildProofSetMultipleAccounts(t *testing.T) {
	ps := NewPartialState()
	for i := byte(1); i <= 5; i++ {
		addr := types.BytesToAddress([]byte{i})
		ps.SetAccount(addr, &AccountState{
			Nonce:       uint64(i),
			Balance:     big.NewInt(int64(i) * 100),
			CodeHash:    types.EmptyCodeHash,
			StorageRoot: types.EmptyRootHash,
		})
	}

	stateRoot := types.HexToHash("0xbeef")
	proofSet := BuildProofSet(stateRoot, ps)
	if len(proofSet.AccountProofs) != 5 {
		t.Fatalf("AccountProofs = %d, want 5", len(proofSet.AccountProofs))
	}

	// Verify deterministic ordering.
	for i := 1; i < len(proofSet.AccountProofs); i++ {
		if !addressLess(proofSet.AccountProofs[i-1].Address, proofSet.AccountProofs[i].Address) {
			t.Error("account proofs not sorted by address")
		}
	}
}

func TestBuildProofSetWithCode(t *testing.T) {
	ps := NewPartialState()
	addr := types.BytesToAddress([]byte{0x10})
	code := []byte{0x60, 0x00, 0x60, 0x00, 0xf3}
	codeHash := crypto.Keccak256Hash(code)

	ps.SetAccount(addr, &AccountState{
		Nonce:       0,
		Balance:     big.NewInt(0),
		CodeHash:    codeHash,
		StorageRoot: types.EmptyRootHash,
	})
	ps.Code[addr] = code

	stateRoot := types.HexToHash("0xbeef")
	proofSet := BuildProofSet(stateRoot, ps)

	if len(proofSet.CodeProofs) != 1 {
		t.Fatalf("CodeProofs = %d, want 1", len(proofSet.CodeProofs))
	}
	if proofSet.CodeProofs[0].CodeHash != codeHash {
		t.Error("code hash mismatch in proof set")
	}
}

func TestProofCheckResultErrorCollection(t *testing.T) {
	result := &ProofCheckResult{
		Valid: false,
		Errors: []ProofError{
			{Address: types.BytesToAddress([]byte{0x01}), Message: "first error"},
			{Address: types.BytesToAddress([]byte{0x02}), Message: "second error"},
		},
	}
	if len(result.Errors) != 2 {
		t.Fatalf("Errors = %d, want 2", len(result.Errors))
	}
	if result.Errors[0].Message != "first error" {
		t.Error("first error message mismatch")
	}
}

func TestProofSetRoundTrip(t *testing.T) {
	// Build a state, create proofs, verify them.
	ps := NewPartialState()
	addr1 := types.BytesToAddress([]byte{0x10})
	addr2 := types.BytesToAddress([]byte{0x20})
	code := []byte{0x60, 0x80, 0x60, 0x40}
	codeHash := crypto.Keccak256Hash(code)

	ps.SetAccount(addr1, &AccountState{
		Nonce:       5,
		Balance:     big.NewInt(5000),
		CodeHash:    codeHash,
		StorageRoot: types.EmptyRootHash,
	})
	ps.Code[addr1] = code
	ps.SetAccount(addr2, &AccountState{
		Nonce:       0,
		Balance:     big.NewInt(0),
		CodeHash:    types.EmptyCodeHash,
		StorageRoot: types.EmptyRootHash,
	})
	ps.SetStorage(addr1, types.HexToHash("0x01"), types.HexToHash("0xaa"))
	ps.SetStorage(addr1, types.HexToHash("0x02"), types.HexToHash("0xbb"))

	stateRoot := types.HexToHash("0xbeef")
	proofSet := BuildProofSet(stateRoot, ps)
	pc := NewProofChecker(stateRoot)
	result := pc.VerifyProofSet(proofSet)

	if !result.Valid {
		for _, e := range result.Errors {
			t.Errorf("proof error: %s (addr=%v, slot=%v)", e.Message, e.Address, e.Slot)
		}
		t.Fatal("round-trip proof verification failed")
	}
	if result.AccountsChecked != 2 {
		t.Errorf("AccountsChecked = %d, want 2", result.AccountsChecked)
	}
	if result.StorageChecked != 2 {
		t.Errorf("StorageChecked = %d, want 2", result.StorageChecked)
	}
	if result.CodeChecked != 1 {
		t.Errorf("CodeChecked = %d, want 1", result.CodeChecked)
	}
}
