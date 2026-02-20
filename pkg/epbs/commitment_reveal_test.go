package epbs

import (
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

// --- helpers ---

func makeTestCommitment(slot uint64, builder BuilderIndex) *BuilderCommitment {
	return &BuilderCommitment{
		Slot:         slot,
		BuilderIndex: builder,
		BuilderAddr:  types.HexToAddress("0xaa"),
		BidAmount:    1000,
		BlockRoot:    types.HexToHash("0xbb"),
	}
}

func makeTestPayloadEnvelope(slot uint64, builder BuilderIndex, root types.Hash) *PayloadEnvelope {
	return &PayloadEnvelope{
		PayloadRoot:     root,
		BuilderIndex:    builder,
		BeaconBlockRoot: types.HexToHash("0xcc"),
		Slot:            slot,
		StateRoot:       types.HexToHash("0xdd"),
	}
}

// --- RevealWindow tests ---

func TestCRRevealWindowDefault(t *testing.T) {
	w := DefaultRevealWindow()
	if w.DeadlineSlots != 1 {
		t.Fatalf("expected 1, got %d", w.DeadlineSlots)
	}
}

func TestCRRevealWindowExpired(t *testing.T) {
	w := RevealWindow{DeadlineSlots: 2}
	if w.IsExpired(10, 12) {
		t.Fatal("should not be expired at exact deadline")
	}
	if !w.IsExpired(10, 13) {
		t.Fatal("should be expired past deadline")
	}
}

func TestCRRevealWindowWithin(t *testing.T) {
	w := RevealWindow{DeadlineSlots: 2}
	if !w.IsWithinWindow(10, 10) {
		t.Fatal("should be within window at commit slot")
	}
	if !w.IsWithinWindow(10, 12) {
		t.Fatal("should be within window at deadline")
	}
	if w.IsWithinWindow(10, 13) {
		t.Fatal("should not be within window past deadline")
	}
}

func TestCRRevealWindowDeadline(t *testing.T) {
	w := RevealWindow{DeadlineSlots: 3}
	if w.Deadline(10) != 13 {
		t.Fatalf("expected 13, got %d", w.Deadline(10))
	}
}

// --- CommitmentChain tests ---

func TestCRCommitmentChainAppendAndQuery(t *testing.T) {
	cc := NewCommitmentChain()
	c1 := makeTestCommitment(10, 1)
	c2 := makeTestCommitment(10, 2)
	c3 := makeTestCommitment(11, 1)

	cc.Append(c1)
	cc.Append(c2)
	cc.Append(c3)

	slot10 := cc.ForSlot(10)
	if len(slot10) != 2 {
		t.Fatalf("expected 2 commitments for slot 10, got %d", len(slot10))
	}

	slot11 := cc.ForSlot(11)
	if len(slot11) != 1 {
		t.Fatalf("expected 1 commitment for slot 11, got %d", len(slot11))
	}

	if cc.Len(10) != 2 {
		t.Fatalf("expected Len(10)=2, got %d", cc.Len(10))
	}
}

func TestCRCommitmentChainPrune(t *testing.T) {
	cc := NewCommitmentChain()
	cc.Append(makeTestCommitment(10, 1))
	cc.PruneSlot(10)
	if cc.Len(10) != 0 {
		t.Fatal("expected 0 after prune")
	}
}

func TestCRCommitmentChainNilAppend(t *testing.T) {
	cc := NewCommitmentChain()
	cc.Append(nil) // should not panic
	if cc.Len(0) != 0 {
		t.Fatal("expected 0 for nil append")
	}
}

// --- RevealVerifier tests ---

func TestCRRevealVerifierSuccess(t *testing.T) {
	rv := NewRevealVerifier()
	c := makeTestCommitment(10, 1)
	p := makeTestPayloadEnvelope(10, 1, c.BlockRoot)

	if err := rv.Verify(c, p); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCRRevealVerifierSlotMismatch(t *testing.T) {
	rv := NewRevealVerifier()
	c := makeTestCommitment(10, 1)
	p := makeTestPayloadEnvelope(11, 1, c.BlockRoot) // wrong slot

	if err := rv.Verify(c, p); err == nil {
		t.Fatal("expected error for slot mismatch")
	}
}

func TestCRRevealVerifierBuilderMismatch(t *testing.T) {
	rv := NewRevealVerifier()
	c := makeTestCommitment(10, 1)
	p := makeTestPayloadEnvelope(10, 2, c.BlockRoot) // wrong builder

	if err := rv.Verify(c, p); err == nil {
		t.Fatal("expected error for builder mismatch")
	}
}

func TestCRRevealVerifierRootMismatch(t *testing.T) {
	rv := NewRevealVerifier()
	c := makeTestCommitment(10, 1)
	p := makeTestPayloadEnvelope(10, 1, types.HexToHash("0xff")) // wrong root

	if err := rv.Verify(c, p); err == nil {
		t.Fatal("expected error for root mismatch")
	}
}

func TestCRRevealVerifierNilInputs(t *testing.T) {
	rv := NewRevealVerifier()
	if err := rv.Verify(nil, &PayloadEnvelope{}); err == nil {
		t.Fatal("expected error for nil commitment")
	}
	if err := rv.Verify(makeTestCommitment(10, 1), nil); err == nil {
		t.Fatal("expected error for nil payload")
	}
}

// --- CRPenaltyEngine tests ---

func TestCRPenaltyEngineNonReveal(t *testing.T) {
	pe := NewCRPenaltyEngine(DefaultCRPenaltyConfig())
	c := makeTestCommitment(10, 1)
	c.BidAmount = 10000

	record, err := pe.PenalizeNonReveal(c)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// 10000 * 20000 / 10000 = 20000 Gwei
	if record.PenaltyGwei != 20000 {
		t.Fatalf("expected penalty 20000, got %d", record.PenaltyGwei)
	}
	if record.Reason == "" {
		t.Fatal("expected non-empty reason")
	}
}

func TestCRPenaltyEngineMismatch(t *testing.T) {
	pe := NewCRPenaltyEngine(DefaultCRPenaltyConfig())
	c := makeTestCommitment(10, 1)
	c.BidAmount = 10000

	record, err := pe.PenalizeMismatch(c, "root mismatch")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// 10000 * 30000 / 10000 = 30000 Gwei
	if record.PenaltyGwei != 30000 {
		t.Fatalf("expected penalty 30000, got %d", record.PenaltyGwei)
	}
}

func TestCRPenaltyEngineTotalForBuilder(t *testing.T) {
	pe := NewCRPenaltyEngine(DefaultCRPenaltyConfig())
	c1 := makeTestCommitment(10, 1)
	c1.BidAmount = 10000
	c2 := makeTestCommitment(11, 1)
	c2.BidAmount = 5000

	pe.PenalizeNonReveal(c1)
	pe.PenalizeNonReveal(c2)

	total := pe.TotalPenaltyForBuilder(types.HexToAddress("0xaa"))
	// 10000*2 + 5000*2 = 30000
	if total != 30000 {
		t.Fatalf("expected total 30000, got %d", total)
	}
}

func TestCRPenaltyEngineNilCommitment(t *testing.T) {
	pe := NewCRPenaltyEngine(DefaultCRPenaltyConfig())
	_, err := pe.PenalizeNonReveal(nil)
	if err == nil {
		t.Fatal("expected error for nil commitment")
	}
}

// --- CommitmentManager tests ---

func TestCRManagerCommitAndReveal(t *testing.T) {
	cm := NewCommitmentManager(RevealWindow{DeadlineSlots: 2}, DefaultCRPenaltyConfig())

	c := makeTestCommitment(10, 1)
	if err := cm.Commit(c); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cm.CommitmentCount() != 1 {
		t.Fatalf("expected 1 commitment, got %d", cm.CommitmentCount())
	}

	p := makeTestPayloadEnvelope(10, 1, c.BlockRoot)
	if err := cm.Reveal(p, 10); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify commitment is marked as revealed.
	stored, ok := cm.GetCommitment(10, 1)
	if !ok {
		t.Fatal("commitment not found")
	}
	if !stored.Revealed {
		t.Fatal("expected revealed=true")
	}
}

func TestCRManagerDuplicateCommit(t *testing.T) {
	cm := NewCommitmentManager(DefaultRevealWindow(), DefaultCRPenaltyConfig())
	c := makeTestCommitment(10, 1)
	cm.Commit(c)
	err := cm.Commit(c)
	if err == nil {
		t.Fatal("expected error for duplicate commitment")
	}
}

func TestCRManagerRevealAfterDeadline(t *testing.T) {
	cm := NewCommitmentManager(RevealWindow{DeadlineSlots: 1}, DefaultCRPenaltyConfig())
	c := makeTestCommitment(10, 1)
	cm.Commit(c)

	p := makeTestPayloadEnvelope(10, 1, c.BlockRoot)
	err := cm.Reveal(p, 12) // deadline is slot 11, current is 12
	if err == nil {
		t.Fatal("expected error for reveal after deadline")
	}
}

func TestCRManagerRevealMismatch(t *testing.T) {
	cm := NewCommitmentManager(RevealWindow{DeadlineSlots: 2}, DefaultCRPenaltyConfig())
	c := makeTestCommitment(10, 1)
	cm.Commit(c)

	p := makeTestPayloadEnvelope(10, 1, types.HexToHash("0xff")) // wrong root
	err := cm.Reveal(p, 10)
	if err == nil {
		t.Fatal("expected error for reveal mismatch")
	}

	// Penalty should have been recorded.
	records := cm.PenaltyRecords()
	if len(records) == 0 {
		t.Fatal("expected penalty record for mismatch")
	}
}

func TestCRManagerCheckDeadlines(t *testing.T) {
	cm := NewCommitmentManager(RevealWindow{DeadlineSlots: 1}, DefaultCRPenaltyConfig())
	cm.Commit(makeTestCommitment(10, 1))
	cm.Commit(makeTestCommitment(10, 2))

	// At slot 12, both should be penalized.
	penalties := cm.CheckDeadlines(12)
	if len(penalties) != 2 {
		t.Fatalf("expected 2 penalties, got %d", len(penalties))
	}
}

func TestCRManagerRevealNoCommitment(t *testing.T) {
	cm := NewCommitmentManager(DefaultRevealWindow(), DefaultCRPenaltyConfig())
	p := makeTestPayloadEnvelope(99, 99, types.HexToHash("0xff"))
	err := cm.Reveal(p, 99)
	if err == nil {
		t.Fatal("expected error for no commitment")
	}
}

func TestCRManagerDoubleReveal(t *testing.T) {
	cm := NewCommitmentManager(RevealWindow{DeadlineSlots: 5}, DefaultCRPenaltyConfig())
	c := makeTestCommitment(10, 1)
	cm.Commit(c)

	p := makeTestPayloadEnvelope(10, 1, c.BlockRoot)
	cm.Reveal(p, 10)

	err := cm.Reveal(p, 11)
	if err == nil {
		t.Fatal("expected error for double reveal")
	}
}
