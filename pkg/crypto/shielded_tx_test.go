package crypto

import (
	"sync"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

func TestShieldedPedersenCommit(t *testing.T) {
	blinding := [32]byte{0x01, 0x02, 0x03}
	c1 := ShieldedPedersenCommit(1000, blinding)
	c2 := ShieldedPedersenCommit(1000, blinding)
	if c1 != c2 {
		t.Fatal("same inputs should produce same commitment")
	}
	// Different values produce different commitments.
	c3 := ShieldedPedersenCommit(200, blinding)
	if c1 == c3 {
		t.Fatal("different values should produce different commitments")
	}
	// Different blindings produce different commitments.
	c4 := ShieldedPedersenCommit(1000, [32]byte{0xFF})
	if c1 == c4 {
		t.Fatal("different blindings should produce different commitments")
	}
	// Zero value/blinding still produces non-zero commitment.
	c5 := ShieldedPedersenCommit(0, [32]byte{})
	if c5.IsZero() {
		t.Fatal("commitment should not be zero even for zero inputs")
	}
}

func TestVerifyCommitmentOpening(t *testing.T) {
	blinding := [32]byte{0xAA, 0xBB}
	value := uint64(42)
	commitment := ShieldedPedersenCommit(value, blinding)

	t.Run("valid", func(t *testing.T) {
		opening := &CommitmentOpening{Value: value, Blinding: blinding}
		if !VerifyCommitmentOpening(commitment, opening) {
			t.Fatal("valid commitment should verify")
		}
	})
	t.Run("wrong_value", func(t *testing.T) {
		if VerifyCommitmentOpening(commitment, &CommitmentOpening{Value: 999, Blinding: blinding}) {
			t.Fatal("wrong value should fail")
		}
	})
	t.Run("wrong_blinding", func(t *testing.T) {
		if VerifyCommitmentOpening(commitment, &CommitmentOpening{Value: value, Blinding: [32]byte{0xCC}}) {
			t.Fatal("wrong blinding should fail")
		}
	})
	t.Run("nil_opening", func(t *testing.T) {
		if VerifyCommitmentOpening(commitment, nil) {
			t.Fatal("nil opening should fail")
		}
	})
}

func TestCommitmentsHomomorphicAdd(t *testing.T) {
	o1 := &CommitmentOpening{Value: 100, Blinding: [32]byte{0x01}}
	o2 := &CommitmentOpening{Value: 200, Blinding: [32]byte{0x02}}
	c1 := ShieldedPedersenCommit(o1.Value, o1.Blinding)
	c2 := ShieldedPedersenCommit(o2.Value, o2.Blinding)

	combined, co := CommitmentsHomomorphicAdd(c1, c2, o1, o2)
	if co == nil {
		t.Fatal("combined opening should not be nil")
	}
	if co.Value != 300 {
		t.Errorf("combined value = %d, want 300", co.Value)
	}
	if !VerifyCommitmentOpening(combined, co) {
		t.Fatal("combined commitment should verify")
	}

	// Nil inputs return nil opening.
	_, nilCo := CommitmentsHomomorphicAdd(c1, c2, nil, nil)
	if nilCo != nil {
		t.Fatal("expected nil opening for nil inputs")
	}
}

func TestRangeProof(t *testing.T) {
	blinding := [32]byte{0x42}
	proof := GenerateRangeProof(1000, blinding)
	if proof == nil {
		t.Fatal("proof should not be nil")
	}
	if proof.BitLength != RangeProofBits {
		t.Errorf("BitLength = %d, want %d", proof.BitLength, RangeProofBits)
	}
	if !VerifyRangeProof(proof) {
		t.Fatal("valid range proof should verify")
	}

	t.Run("nil", func(t *testing.T) {
		if VerifyRangeProof(nil) {
			t.Fatal("nil proof should fail")
		}
	})
	t.Run("wrong_bits", func(t *testing.T) {
		p := GenerateRangeProof(100, [32]byte{0x01})
		p.BitLength = 32
		if VerifyRangeProof(p) {
			t.Fatal("wrong bit length should fail")
		}
	})
	t.Run("empty_data", func(t *testing.T) {
		p := &RangeProof{Commitment: types.Hash{0x01}, BitLength: RangeProofBits}
		if VerifyRangeProof(p) {
			t.Fatal("empty proof data should fail")
		}
	})
	t.Run("zero_commitment", func(t *testing.T) {
		p := &RangeProof{ProofData: []byte{0x01}, BitLength: RangeProofBits}
		if VerifyRangeProof(p) {
			t.Fatal("zero commitment should fail")
		}
	})
	t.Run("boundary_values", func(t *testing.T) {
		if !VerifyRangeProof(GenerateRangeProof(0, [32]byte{0xFF})) {
			t.Fatal("value=0 should verify")
		}
		if !VerifyRangeProof(GenerateRangeProof(^uint64(0), [32]byte{0x01})) {
			t.Fatal("max uint64 should verify")
		}
	})
}

func TestNullifierSet(t *testing.T) {
	ns := NewNullifierSet()
	n := types.Hash{0xAA}

	if ns.Has(n) {
		t.Fatal("should not have nullifier before adding")
	}
	if !ns.Add(n) {
		t.Fatal("first add should succeed")
	}
	if !ns.Has(n) {
		t.Fatal("should have nullifier after adding")
	}
	if ns.Add(n) {
		t.Fatal("second add should fail (double-spend)")
	}
	if ns.Size() != 1 {
		t.Errorf("size = %d, want 1", ns.Size())
	}
}

func TestNullifierSet_ConcurrentAccess(t *testing.T) {
	ns := NewNullifierSet()
	var wg sync.WaitGroup
	successes := make(chan bool, 50)
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			successes <- ns.Add(types.Hash{0x01})
		}()
	}
	wg.Wait()
	close(successes)
	count := 0
	for ok := range successes {
		if ok {
			count++
		}
	}
	if count != 1 {
		t.Errorf("exactly 1 add should succeed, got %d", count)
	}
}

func TestShieldedNotePool_CreateNote(t *testing.T) {
	pool := NewShieldedNotePool()
	note, opening, err := pool.CreateNote(1000, types.Address{0x42})
	if err != nil {
		t.Fatalf("CreateNote: %v", err)
	}
	if note == nil || opening == nil {
		t.Fatal("note and opening should not be nil")
	}
	if note.Commitment.IsZero() || note.NullifierHash.IsZero() {
		t.Fatal("commitment and nullifier should be non-zero")
	}
	if len(note.EncryptedValue) == 0 || note.RangeProof == nil {
		t.Fatal("encrypted value and range proof should be present")
	}
	if opening.Value != 1000 {
		t.Errorf("opening value = %d, want 1000", opening.Value)
	}
	if pool.NoteCommitmentCount() != 1 {
		t.Errorf("commitment count = %d, want 1", pool.NoteCommitmentCount())
	}
	// Commitment should verify with opening.
	if !VerifyCommitmentOpening(note.Commitment, opening) {
		t.Fatal("commitment should verify")
	}
	// Different values produce different commitments.
	note2, _, _ := pool.CreateNote(200, types.Address{0x01})
	if note.Commitment == note2.Commitment {
		t.Fatal("different values should produce different commitments")
	}
}

func TestShieldedNotePool_SpendNote(t *testing.T) {
	pool := NewShieldedNotePool()
	note, _, _ := pool.CreateNote(1000, types.Address{0x01})
	if err := pool.SpendNote(note.NullifierHash); err != nil {
		t.Fatalf("SpendNote: %v", err)
	}
	if err := pool.SpendNote(note.NullifierHash); err != ErrNullifierSpent {
		t.Errorf("expected ErrNullifierSpent, got %v", err)
	}
}

func TestShieldedNotePool_VerifyNote(t *testing.T) {
	pool := NewShieldedNotePool()
	note, _, _ := pool.CreateNote(1000, types.Address{0x01})
	if err := pool.VerifyNote(note); err != nil {
		t.Fatalf("VerifyNote: %v", err)
	}
	if err := pool.VerifyNote(nil); err != ErrNilNote {
		t.Errorf("expected ErrNilNote, got %v", err)
	}
	badNote := &ShieldNote{
		Commitment: types.Hash{},
		RangeProof: &RangeProof{Commitment: types.Hash{0x01}, ProofData: []byte{0x01}, BitLength: RangeProofBits},
	}
	if err := pool.VerifyNote(badNote); err != ErrInvalidProof {
		t.Errorf("expected ErrInvalidProof, got %v", err)
	}
}

func TestShieldedNotePool_HasGetIsSpent(t *testing.T) {
	pool := NewShieldedNotePool()
	note, _, _ := pool.CreateNote(1000, types.Address{0x01})

	if !pool.HasNoteCommitment(note.Commitment) {
		t.Fatal("should have commitment")
	}
	if pool.GetNote(note.Commitment) == nil {
		t.Fatal("GetNote should return note")
	}
	if pool.IsSpent(note.NullifierHash) {
		t.Fatal("should not be spent yet")
	}
	pool.SpendNote(note.NullifierHash)
	if !pool.IsSpent(note.NullifierHash) {
		t.Fatal("should be spent after SpendNote")
	}
}

func TestVerifyBalanceProof(t *testing.T) {
	balanced := VerifyBalanceProof(
		[]*CommitmentOpening{{Value: 500}, {Value: 300}},
		[]*CommitmentOpening{{Value: 800}},
	)
	if !balanced {
		t.Fatal("balanced should verify")
	}
	if VerifyBalanceProof([]*CommitmentOpening{{Value: 500}}, []*CommitmentOpening{{Value: 600}}) {
		t.Fatal("unbalanced should not verify")
	}
	if VerifyBalanceProof(nil, nil) {
		t.Fatal("empty should not verify")
	}
	if VerifyBalanceProof([]*CommitmentOpening{nil}, []*CommitmentOpening{{Value: 0}}) {
		t.Fatal("nil opening should not verify")
	}
}

func TestShieldedNotePool_VerifyTransfer(t *testing.T) {
	pool := NewShieldedNotePool()
	inputNote, inputOpening, _ := pool.CreateNote(1000, types.Address{0x01})
	outNote1, outOpening1, _ := pool.CreateNote(700, types.Address{0x02})
	outNote2, outOpening2, _ := pool.CreateNote(200, types.Address{0x03})

	transfer := &ShieldedTransfer{
		InputNotes:  []*ShieldNote{inputNote},
		OutputNotes: []*ShieldNote{outNote1, outNote2},
		Fee:         100,
	}
	err := pool.VerifyTransfer(transfer, []*CommitmentOpening{inputOpening}, []*CommitmentOpening{outOpening1, outOpening2})
	if err != nil {
		t.Fatalf("VerifyTransfer: %v", err)
	}

	t.Run("balance_mismatch", func(t *testing.T) {
		p := NewShieldedNotePool()
		in, inO, _ := p.CreateNote(1000, types.Address{0x01})
		out, outO, _ := p.CreateNote(500, types.Address{0x02})
		tr := &ShieldedTransfer{InputNotes: []*ShieldNote{in}, OutputNotes: []*ShieldNote{out}, Fee: 100}
		if err := p.VerifyTransfer(tr, []*CommitmentOpening{inO}, []*CommitmentOpening{outO}); err != ErrBalanceMismatch {
			t.Errorf("expected ErrBalanceMismatch, got %v", err)
		}
	})
	t.Run("spent_nullifier", func(t *testing.T) {
		p := NewShieldedNotePool()
		n, nO, _ := p.CreateNote(1000, types.Address{0x01})
		out, outO, _ := p.CreateNote(1000, types.Address{0x02})
		p.SpendNote(n.NullifierHash)
		tr := &ShieldedTransfer{InputNotes: []*ShieldNote{n}, OutputNotes: []*ShieldNote{out}, Fee: 0}
		if err := p.VerifyTransfer(tr, []*CommitmentOpening{nO}, []*CommitmentOpening{outO}); err != ErrNullifierSpent {
			t.Errorf("expected ErrNullifierSpent, got %v", err)
		}
	})
	t.Run("nil", func(t *testing.T) {
		if err := pool.VerifyTransfer(nil, nil, nil); err != ErrNilNote {
			t.Errorf("expected ErrNilNote, got %v", err)
		}
	})
}

func TestShieldedNotePool_ApplyTransfer(t *testing.T) {
	pool := NewShieldedNotePool()
	in, _, _ := pool.CreateNote(1000, types.Address{0x01})
	out, _, _ := pool.CreateNote(1000, types.Address{0x02})
	tr := &ShieldedTransfer{InputNotes: []*ShieldNote{in}, OutputNotes: []*ShieldNote{out}, Fee: 0}

	if err := pool.ApplyTransfer(tr); err != nil {
		t.Fatalf("ApplyTransfer: %v", err)
	}
	if !pool.IsSpent(in.NullifierHash) {
		t.Fatal("input should be spent")
	}
	if !pool.HasNoteCommitment(out.Commitment) {
		t.Fatal("output should exist")
	}
	// Double-spend should fail.
	if err := pool.ApplyTransfer(tr); err != ErrNullifierSpent {
		t.Errorf("expected ErrNullifierSpent, got %v", err)
	}
	// Nil transfer.
	if err := pool.ApplyTransfer(nil); err != ErrNilNote {
		t.Errorf("expected ErrNilNote, got %v", err)
	}
}

func TestShieldedNotePool_FullWorkflow(t *testing.T) {
	pool := NewShieldedNotePool()

	// Alice creates note with 1000.
	note, opening, _ := pool.CreateNote(1000, types.Address{0x01})
	if err := pool.VerifyNote(note); err != nil {
		t.Fatalf("VerifyNote: %v", err)
	}
	if !VerifyCommitmentOpening(note.Commitment, opening) {
		t.Fatal("commitment should verify")
	}

	// Alice sends 700 to Bob, 200 to Carol, 100 fee.
	bobNote, bobO, _ := pool.CreateNote(700, types.Address{0x02})
	carolNote, carolO, _ := pool.CreateNote(200, types.Address{0x03})
	tr := &ShieldedTransfer{
		InputNotes:  []*ShieldNote{note},
		OutputNotes: []*ShieldNote{bobNote, carolNote},
		Fee:         100,
	}
	if err := pool.VerifyTransfer(tr, []*CommitmentOpening{opening}, []*CommitmentOpening{bobO, carolO}); err != nil {
		t.Fatalf("VerifyTransfer: %v", err)
	}
	if err := pool.ApplyTransfer(tr); err != nil {
		t.Fatalf("ApplyTransfer: %v", err)
	}
	if !pool.IsSpent(note.NullifierHash) {
		t.Fatal("Alice's note should be spent")
	}
	if pool.NoteCommitmentCount() != 3 {
		t.Errorf("commitment count = %d, want 3", pool.NoteCommitmentCount())
	}
	if pool.SpentNullifierCount() != 1 {
		t.Errorf("nullifier count = %d, want 1", pool.SpentNullifierCount())
	}
	if err := pool.ApplyTransfer(tr); err != ErrNullifierSpent {
		t.Errorf("expected ErrNullifierSpent on double-spend, got %v", err)
	}
}

func TestShieldedNotePool_ConcurrentCreateAndSpend(t *testing.T) {
	pool := NewShieldedNotePool()
	var wg sync.WaitGroup
	notes := make([]*ShieldNote, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			n, _, _ := pool.CreateNote(uint64(idx*100), types.Address{byte(idx)})
			notes[idx] = n
		}(i)
	}
	wg.Wait()
	if pool.NoteCommitmentCount() != 20 {
		t.Errorf("commitment count = %d, want 20", pool.NoteCommitmentCount())
	}
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			pool.SpendNote(notes[idx].NullifierHash)
		}(i)
	}
	wg.Wait()
	if pool.SpentNullifierCount() != 10 {
		t.Errorf("nullifier count = %d, want 10", pool.SpentNullifierCount())
	}
}
