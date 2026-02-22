package crypto

import (
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

func TestCreateShieldedTx(t *testing.T) {
	sender := types.Address{0x01, 0x02, 0x03}
	recipient := types.Address{0x04, 0x05, 0x06}
	amount := uint64(1_000_000)
	blinding := [32]byte{0xAA, 0xBB, 0xCC}

	tx := CreateShieldedTx(sender, recipient, amount, blinding)
	if tx == nil {
		t.Fatal("expected non-nil shielded tx")
	}

	// Commitment and nullifier should be non-zero.
	if tx.Commitment == (types.Hash{}) {
		t.Fatal("expected non-zero commitment")
	}
	if tx.NullifierHash == (types.Hash{}) {
		t.Fatal("expected non-zero nullifier")
	}
	// They should differ.
	if tx.Commitment == tx.NullifierHash {
		t.Fatal("commitment and nullifier should differ")
	}
	// EncryptedData should not be empty.
	if len(tx.EncryptedData) == 0 {
		t.Fatal("expected non-empty encrypted data")
	}
}

func TestCreateShieldedTx_Deterministic(t *testing.T) {
	sender := types.Address{0x01}
	recipient := types.Address{0x02}
	amount := uint64(500)
	blinding := [32]byte{0x42}

	tx1 := CreateShieldedTx(sender, recipient, amount, blinding)
	tx2 := CreateShieldedTx(sender, recipient, amount, blinding)

	if tx1.Commitment != tx2.Commitment {
		t.Fatal("same inputs should produce same commitment")
	}
	if tx1.NullifierHash != tx2.NullifierHash {
		t.Fatal("same inputs should produce same nullifier")
	}
}

func TestCreateShieldedTx_DifferentBlinding(t *testing.T) {
	sender := types.Address{0x01}
	recipient := types.Address{0x02}
	amount := uint64(500)

	tx1 := CreateShieldedTx(sender, recipient, amount, [32]byte{0x01})
	tx2 := CreateShieldedTx(sender, recipient, amount, [32]byte{0x02})

	if tx1.Commitment == tx2.Commitment {
		t.Fatal("different blinding factors should produce different commitments")
	}
}

func TestVerifyShieldedTx(t *testing.T) {
	sender := types.Address{0x01}
	recipient := types.Address{0x02}

	tx := CreateShieldedTx(sender, recipient, 1000, [32]byte{})
	if !VerifyShieldedTx(tx) {
		t.Fatal("stub verification should return true")
	}

	// Nil tx should fail.
	if VerifyShieldedTx(nil) {
		t.Fatal("nil tx should fail verification")
	}
}

func TestShieldedPool_Commitments(t *testing.T) {
	pool := NewShieldedPool()

	c1 := types.Hash{0x01}
	c2 := types.Hash{0x02}

	pool.AddCommitment(c1)
	pool.AddCommitment(c2)

	if !pool.HasCommitment(c1) {
		t.Fatal("expected commitment c1 to exist")
	}
	if !pool.HasCommitment(c2) {
		t.Fatal("expected commitment c2 to exist")
	}
	if pool.HasCommitment(types.Hash{0x03}) {
		t.Fatal("unexpected commitment found")
	}
	if pool.CommitmentCount() != 2 {
		t.Fatalf("expected 2 commitments, got %d", pool.CommitmentCount())
	}
}

func TestShieldedPool_Nullifiers(t *testing.T) {
	pool := NewShieldedPool()

	n1 := types.Hash{0xAA}

	// Not yet revealed.
	if pool.CheckNullifier(n1) {
		t.Fatal("nullifier should not be revealed yet")
	}

	// Reveal it.
	if !pool.RevealNullifier(n1) {
		t.Fatal("first reveal should succeed")
	}

	// Now it should be detected.
	if !pool.CheckNullifier(n1) {
		t.Fatal("nullifier should be detected after reveal")
	}

	// Double-spend: reveal again should fail.
	if pool.RevealNullifier(n1) {
		t.Fatal("double reveal should fail")
	}

	if pool.NullifierCount() != 1 {
		t.Fatalf("expected 1 nullifier, got %d", pool.NullifierCount())
	}
}

func TestShieldedPool_NullifierRoot(t *testing.T) {
	pool := NewShieldedPool()

	// Empty pool -> zero hash.
	root := pool.NullifierRoot()
	if root != (types.Hash{}) {
		t.Fatal("expected zero hash for empty nullifier set")
	}

	// Add a nullifier and check root is non-zero.
	pool.RevealNullifier(types.Hash{0x01})
	root = pool.NullifierRoot()
	if root == (types.Hash{}) {
		t.Fatal("expected non-zero root with nullifiers")
	}

	// Root should be deterministic.
	root2 := pool.NullifierRoot()
	if root != root2 {
		t.Fatal("nullifier root should be deterministic")
	}
}

func TestShieldedPool_FullWorkflow(t *testing.T) {
	pool := NewShieldedPool()

	sender := types.Address{0x01}
	recipient := types.Address{0x02}

	// Create a shielded tx.
	stx := CreateShieldedTx(sender, recipient, 1_000_000, [32]byte{0x42})

	// Verify it (stub).
	if !VerifyShieldedTx(stx) {
		t.Fatal("verification failed")
	}

	// Add commitment.
	pool.AddCommitment(stx.Commitment)
	if !pool.HasCommitment(stx.Commitment) {
		t.Fatal("commitment not found after adding")
	}

	// Check nullifier is not yet spent.
	if pool.CheckNullifier(stx.NullifierHash) {
		t.Fatal("nullifier should not be spent yet")
	}

	// Spend it.
	if !pool.RevealNullifier(stx.NullifierHash) {
		t.Fatal("reveal should succeed")
	}

	// Double-spend attempt.
	if pool.RevealNullifier(stx.NullifierHash) {
		t.Fatal("double-spend should be rejected")
	}

	// Check final state.
	if pool.CommitmentCount() != 1 {
		t.Fatalf("expected 1 commitment, got %d", pool.CommitmentCount())
	}
	if pool.NullifierCount() != 1 {
		t.Fatalf("expected 1 nullifier, got %d", pool.NullifierCount())
	}
}
