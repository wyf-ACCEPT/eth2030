package vops

import (
	"testing"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/witness"
)

func TestValidateTransition(t *testing.T) {
	preRoot := types.HexToHash("0x01")
	postRoot := types.HexToHash("0x02")
	keys := [][]byte{{0x01}, {0x02}}

	proof := BuildValidityProof(preRoot, postRoot, keys)

	if !ValidateTransition(preRoot, postRoot, proof) {
		t.Error("valid proof should pass validation")
	}
}

func TestValidateTransitionNilProof(t *testing.T) {
	preRoot := types.HexToHash("0x01")
	postRoot := types.HexToHash("0x02")

	if ValidateTransition(preRoot, postRoot, nil) {
		t.Error("nil proof should fail validation")
	}
}

func TestValidateTransitionWrongPreRoot(t *testing.T) {
	preRoot := types.HexToHash("0x01")
	postRoot := types.HexToHash("0x02")
	keys := [][]byte{{0x01}}

	proof := BuildValidityProof(preRoot, postRoot, keys)

	wrongPre := types.HexToHash("0xff")
	if ValidateTransition(wrongPre, postRoot, proof) {
		t.Error("wrong pre-root should fail validation")
	}
}

func TestValidateTransitionWrongPostRoot(t *testing.T) {
	preRoot := types.HexToHash("0x01")
	postRoot := types.HexToHash("0x02")
	keys := [][]byte{{0x01}}

	proof := BuildValidityProof(preRoot, postRoot, keys)

	wrongPost := types.HexToHash("0xff")
	if ValidateTransition(preRoot, wrongPost, proof) {
		t.Error("wrong post-root should fail validation")
	}
}

func TestValidateTransitionEmptyKeys(t *testing.T) {
	preRoot := types.HexToHash("0x01")
	postRoot := types.HexToHash("0x02")

	proof := &ValidityProof{
		PreStateRoot:  preRoot,
		PostStateRoot: postRoot,
		AccessedKeys:  nil,
		ProofData:     []byte{0x01},
	}

	if ValidateTransition(preRoot, postRoot, proof) {
		t.Error("empty accessed keys should fail validation")
	}
}

func TestValidateTransitionEmptyProofData(t *testing.T) {
	preRoot := types.HexToHash("0x01")
	postRoot := types.HexToHash("0x02")

	proof := &ValidityProof{
		PreStateRoot:  preRoot,
		PostStateRoot: postRoot,
		AccessedKeys:  [][]byte{{0x01}},
		ProofData:     nil,
	}

	if ValidateTransition(preRoot, postRoot, proof) {
		t.Error("empty proof data should fail validation")
	}
}

func TestBuildValidityProof(t *testing.T) {
	preRoot := types.HexToHash("0x01")
	postRoot := types.HexToHash("0x02")
	keys := [][]byte{{0xaa}, {0xbb}}

	proof := BuildValidityProof(preRoot, postRoot, keys)

	if proof.PreStateRoot != preRoot {
		t.Error("pre root mismatch")
	}
	if proof.PostStateRoot != postRoot {
		t.Error("post root mismatch")
	}
	if len(proof.AccessedKeys) != 2 {
		t.Errorf("accessed keys count = %d, want 2", len(proof.AccessedKeys))
	}
	if len(proof.ProofData) == 0 {
		t.Error("proof data should not be empty")
	}
}

func TestBuildPartialStateFromWitness(t *testing.T) {
	// Create a witness with account data.
	var stem [31]byte
	stem[0] = 0x01

	w := &witness.ExecutionWitness{
		ParentRoot: types.HexToHash("0x01"),
		State: []witness.StemStateDiff{
			{
				Stem: stem,
				Suffixes: []witness.SuffixStateDiff{
					{Suffix: 1, CurrentValue: makeValue(100)}, // balance
					{Suffix: 2, CurrentValue: makeValue(5)},   // nonce
				},
			},
		},
	}

	ps := BuildPartialStateFromWitness(w)
	if ps == nil {
		t.Fatal("partial state is nil")
	}
	if len(ps.Accounts) == 0 {
		t.Error("expected at least one account")
	}
}

func TestBuildPartialStateFromNilWitness(t *testing.T) {
	ps := BuildPartialStateFromWitness(nil)
	if ps == nil {
		t.Fatal("should return empty partial state for nil witness")
	}
	if len(ps.Accounts) != 0 {
		t.Error("expected empty accounts")
	}
}

func makeValue(val byte) *[32]byte {
	var v [32]byte
	v[0] = val
	return &v
}
