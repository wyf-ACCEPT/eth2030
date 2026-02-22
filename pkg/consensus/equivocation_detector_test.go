package consensus

import (
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

func TestEquivocationTypeString(t *testing.T) {
	tests := []struct {
		et   EquivocationType
		want string
	}{
		{DoubleProposal, "double_proposal"},
		{DoubleVote7, "double_vote"},
		{SurroundVote7, "surround_vote"},
		{EquivocationType(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.et.String(); got != tt.want {
			t.Errorf("EquivocationType(%d).String() = %q, want %q", tt.et, got, tt.want)
		}
	}
}

func TestCheckProposalNoDuplicate(t *testing.T) {
	d := NewEquivocationDetector(nil)

	hash1 := [32]byte{0x01}
	ev := d.CheckProposal(10, ValidatorIndex(5), hash1)
	if ev != nil {
		t.Error("first proposal should not generate evidence")
	}
}

func TestCheckProposalSameBlock(t *testing.T) {
	d := NewEquivocationDetector(nil)

	hash1 := [32]byte{0x01}
	d.CheckProposal(10, ValidatorIndex(5), hash1)
	ev := d.CheckProposal(10, ValidatorIndex(5), hash1)
	if ev != nil {
		t.Error("same block hash should not generate evidence")
	}
}

func TestCheckProposalDoubleProposal(t *testing.T) {
	d := NewEquivocationDetector(nil)

	hash1 := [32]byte{0x01}
	hash2 := [32]byte{0x02}

	d.CheckProposal(10, ValidatorIndex(5), hash1)
	ev := d.CheckProposal(10, ValidatorIndex(5), hash2)
	if ev == nil {
		t.Fatal("expected double-proposal evidence")
	}
	if ev.Type != DoubleProposal {
		t.Errorf("expected DoubleProposal, got %s", ev.Type.String())
	}
	if ev.ValidatorIndex != ValidatorIndex(5) {
		t.Errorf("expected validator 5, got %d", ev.ValidatorIndex)
	}
	if ev.Evidence1Hash != hash1 {
		t.Error("evidence1 hash mismatch")
	}
	if ev.Evidence2Hash != hash2 {
		t.Error("evidence2 hash mismatch")
	}
}

func TestCheckProposalDifferentSlots(t *testing.T) {
	d := NewEquivocationDetector(nil)

	hash1 := [32]byte{0x01}
	hash2 := [32]byte{0x02}

	d.CheckProposal(10, ValidatorIndex(5), hash1)
	ev := d.CheckProposal(11, ValidatorIndex(5), hash2)
	if ev != nil {
		t.Error("different slots should not generate evidence")
	}
}

func TestCheckProposalDifferentValidators(t *testing.T) {
	d := NewEquivocationDetector(nil)

	hash1 := [32]byte{0x01}
	hash2 := [32]byte{0x02}

	d.CheckProposal(10, ValidatorIndex(5), hash1)
	ev := d.CheckProposal(10, ValidatorIndex(6), hash2)
	if ev != nil {
		t.Error("different validators should not generate evidence")
	}
}

func TestCheckAttestationNoViolation(t *testing.T) {
	d := NewEquivocationDetector(nil)

	att := &AttestationData{
		Slot:            10,
		BeaconBlockRoot: types.Hash{0x01},
		Source:          Checkpoint{Epoch: 0, Root: types.Hash{0xAA}},
		Target:          Checkpoint{Epoch: 1, Root: types.Hash{0xBB}},
	}

	ev := d.CheckAttestation(att, ValidatorIndex(5))
	if ev != nil {
		t.Error("first attestation should not generate evidence")
	}
}

func TestCheckAttestationDoubleVote(t *testing.T) {
	d := NewEquivocationDetector(nil)

	att1 := &AttestationData{
		Slot:            10,
		BeaconBlockRoot: types.Hash{0x01},
		Source:          Checkpoint{Epoch: 0, Root: types.Hash{0xAA}},
		Target:          Checkpoint{Epoch: 1, Root: types.Hash{0xBB}},
	}
	att2 := &AttestationData{
		Slot:            10,
		BeaconBlockRoot: types.Hash{0x02}, // different block root
		Source:          Checkpoint{Epoch: 0, Root: types.Hash{0xAA}},
		Target:          Checkpoint{Epoch: 1, Root: types.Hash{0xCC}}, // same target epoch, different root
	}

	d.CheckAttestation(att1, ValidatorIndex(5))
	ev := d.CheckAttestation(att2, ValidatorIndex(5))
	if ev == nil {
		t.Fatal("expected double-vote evidence")
	}
	if ev.Type != DoubleVote7 {
		t.Errorf("expected DoubleVote7, got %s", ev.Type.String())
	}
	if ev.ValidatorIndex != ValidatorIndex(5) {
		t.Errorf("expected validator 5, got %d", ev.ValidatorIndex)
	}
}

func TestCheckAttestationSurroundVote(t *testing.T) {
	d := NewEquivocationDetector(nil)

	// First attestation: source=2, target=5.
	att1 := &AttestationData{
		Slot:            10,
		BeaconBlockRoot: types.Hash{0x01},
		Source:          Checkpoint{Epoch: 2, Root: types.Hash{0xAA}},
		Target:          Checkpoint{Epoch: 5, Root: types.Hash{0xBB}},
	}
	// Second attestation: source=1, target=6 (surrounds att1).
	att2 := &AttestationData{
		Slot:            20,
		BeaconBlockRoot: types.Hash{0x02},
		Source:          Checkpoint{Epoch: 1, Root: types.Hash{0xCC}},
		Target:          Checkpoint{Epoch: 6, Root: types.Hash{0xDD}},
	}

	d.CheckAttestation(att1, ValidatorIndex(5))
	ev := d.CheckAttestation(att2, ValidatorIndex(5))
	if ev == nil {
		t.Fatal("expected surround-vote evidence")
	}
	if ev.Type != SurroundVote7 {
		t.Errorf("expected SurroundVote7, got %s", ev.Type.String())
	}
	if ev.Source1 != 2 || ev.Target1 != 5 {
		t.Errorf("expected source1=2, target1=5, got source1=%d, target1=%d", ev.Source1, ev.Target1)
	}
	if ev.Source2 != 1 || ev.Target2 != 6 {
		t.Errorf("expected source2=1, target2=6, got source2=%d, target2=%d", ev.Source2, ev.Target2)
	}
}

func TestCheckAttestationSurroundedVote(t *testing.T) {
	d := NewEquivocationDetector(nil)

	// First attestation: source=1, target=6 (wide range).
	att1 := &AttestationData{
		Slot:            10,
		BeaconBlockRoot: types.Hash{0x01},
		Source:          Checkpoint{Epoch: 1, Root: types.Hash{0xAA}},
		Target:          Checkpoint{Epoch: 6, Root: types.Hash{0xBB}},
	}
	// Second attestation: source=2, target=5 (surrounded by att1).
	att2 := &AttestationData{
		Slot:            20,
		BeaconBlockRoot: types.Hash{0x02},
		Source:          Checkpoint{Epoch: 2, Root: types.Hash{0xCC}},
		Target:          Checkpoint{Epoch: 5, Root: types.Hash{0xDD}},
	}

	d.CheckAttestation(att1, ValidatorIndex(5))
	ev := d.CheckAttestation(att2, ValidatorIndex(5))
	if ev == nil {
		t.Fatal("expected surround-vote evidence (surrounded case)")
	}
	if ev.Type != SurroundVote7 {
		t.Errorf("expected SurroundVote7, got %s", ev.Type.String())
	}
}

func TestCheckAttestationNil(t *testing.T) {
	d := NewEquivocationDetector(nil)
	ev := d.CheckAttestation(nil, ValidatorIndex(0))
	if ev != nil {
		t.Error("nil attestation should return nil evidence")
	}
}

func TestIsSurroundVoteCheck(t *testing.T) {
	tests := []struct {
		name    string
		s1, t1  Epoch
		s2, t2  Epoch
		want    bool
	}{
		{"no_surround", 1, 2, 3, 4, false},
		{"equal", 1, 4, 1, 4, false},
		{"first_surrounds", 1, 6, 2, 5, true},
		{"second_surrounds", 2, 5, 1, 6, true},
		{"adjacent", 1, 3, 2, 4, false},
		{"same_source", 1, 5, 1, 6, false},
		{"same_target", 1, 5, 2, 5, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsSurroundVoteCheck(tt.s1, tt.t1, tt.s2, tt.t2)
			if got != tt.want {
				t.Errorf("IsSurroundVoteCheck(%d,%d,%d,%d) = %v, want %v",
					tt.s1, tt.t1, tt.s2, tt.t2, got, tt.want)
			}
		})
	}
}

func TestGetPendingSlashings(t *testing.T) {
	d := NewEquivocationDetector(nil)

	// Generate some evidence.
	d.CheckProposal(10, ValidatorIndex(1), [32]byte{0x01})
	d.CheckProposal(10, ValidatorIndex(1), [32]byte{0x02})
	d.CheckProposal(20, ValidatorIndex(2), [32]byte{0x03})
	d.CheckProposal(20, ValidatorIndex(2), [32]byte{0x04})

	pending := d.GetPendingSlashings()
	if len(pending) != 2 {
		t.Fatalf("expected 2 pending, got %d", len(pending))
	}

	// After consuming, pending should be empty.
	pending2 := d.GetPendingSlashings()
	if len(pending2) != 0 {
		t.Errorf("expected 0 pending after consume, got %d", len(pending2))
	}
}

func TestPeekPendingSlashings(t *testing.T) {
	d := NewEquivocationDetector(nil)

	d.CheckProposal(10, ValidatorIndex(1), [32]byte{0x01})
	d.CheckProposal(10, ValidatorIndex(1), [32]byte{0x02})

	peek := d.PeekPendingSlashings()
	if len(peek) != 1 {
		t.Fatalf("expected 1 pending, got %d", len(peek))
	}

	// Peek should not consume.
	peek2 := d.PeekPendingSlashings()
	if len(peek2) != 1 {
		t.Errorf("expected 1 pending after peek, got %d", len(peek2))
	}
}

func TestProcessedCount(t *testing.T) {
	d := NewEquivocationDetector(nil)

	d.CheckProposal(10, ValidatorIndex(1), [32]byte{0x01})
	d.CheckProposal(10, ValidatorIndex(1), [32]byte{0x02})

	if d.ProcessedCount() != 1 {
		t.Errorf("expected 1 processed, got %d", d.ProcessedCount())
	}

	// Consuming does not reset processed count.
	d.GetPendingSlashings()
	if d.ProcessedCount() != 1 {
		t.Errorf("expected 1 processed after consume, got %d", d.ProcessedCount())
	}
}

func TestProcessSlashableEvidence(t *testing.T) {
	d := NewEquivocationDetector(nil)

	ev := &EquivocationEvidence{
		Type:           DoubleProposal,
		ValidatorIndex: ValidatorIndex(99),
		Slot:           100,
		Evidence1Hash:  [32]byte{0x01},
		Evidence2Hash:  [32]byte{0x02},
	}
	d.ProcessSlashableEvidence(ev)

	pending := d.PeekPendingSlashings()
	if len(pending) != 1 {
		t.Fatalf("expected 1 pending, got %d", len(pending))
	}
	if pending[0].ValidatorIndex != ValidatorIndex(99) {
		t.Errorf("expected validator 99, got %d", pending[0].ValidatorIndex)
	}
}

func TestProcessSlashableEvidenceNil(t *testing.T) {
	d := NewEquivocationDetector(nil)
	d.ProcessSlashableEvidence(nil) // should not panic
	if d.PendingCount() != 0 {
		t.Error("nil evidence should not be added")
	}
}

func TestPruneOld(t *testing.T) {
	d := NewEquivocationDetector(&EquivocationDetectorConfig{
		AttestationWindow:      10,
		MaxPendingSlashings:    100,
		ProposalRetentionSlots: 100,
	})

	// Add proposals at slots 10 and 500.
	d.CheckProposal(10, ValidatorIndex(1), [32]byte{0x01})
	d.CheckProposal(500, ValidatorIndex(2), [32]byte{0x02})

	// Add attestations.
	att1 := &AttestationData{
		Slot:   10,
		Source: Checkpoint{Epoch: 0},
		Target: Checkpoint{Epoch: 1},
	}
	d.CheckAttestation(att1, ValidatorIndex(3))

	att2 := &AttestationData{
		Slot:   500,
		Source: Checkpoint{Epoch: 14},
		Target: Checkpoint{Epoch: 15},
	}
	d.CheckAttestation(att2, ValidatorIndex(4))

	// Prune at slot 600 with window 100 for proposals.
	d.PruneOld(600)

	if d.TrackedProposalCount() != 1 {
		t.Errorf("expected 1 proposal after prune, got %d", d.TrackedProposalCount())
	}

	// Attestation history: epoch 600/32 = 18, window=10, cutoff=8.
	// Epoch 1 should be pruned, epoch 15 should remain.
	if d.TrackedValidatorCount() != 1 {
		t.Errorf("expected 1 tracked validator after prune, got %d", d.TrackedValidatorCount())
	}
}

func TestMaxPendingSlashings(t *testing.T) {
	cfg := &EquivocationDetectorConfig{
		AttestationWindow:      256,
		MaxPendingSlashings:    3,
		ProposalRetentionSlots: 4096,
	}
	d := NewEquivocationDetector(cfg)

	// Generate 5 double proposals.
	for i := 0; i < 5; i++ {
		d.CheckProposal(Slot(i), ValidatorIndex(i), [32]byte{byte(i)})
		d.CheckProposal(Slot(i), ValidatorIndex(i), [32]byte{byte(i + 100)})
	}

	if d.PendingCount() != 3 {
		t.Errorf("expected 3 pending (capped), got %d", d.PendingCount())
	}
	if d.ProcessedCount() != 5 {
		t.Errorf("expected 5 total processed, got %d", d.ProcessedCount())
	}
}

func TestGetSlashingsByType(t *testing.T) {
	d := NewEquivocationDetector(nil)

	// Double proposal.
	d.CheckProposal(10, ValidatorIndex(1), [32]byte{0x01})
	d.CheckProposal(10, ValidatorIndex(1), [32]byte{0x02})

	// Double vote.
	att1 := &AttestationData{
		Slot:            10,
		BeaconBlockRoot: types.Hash{0x01},
		Source:          Checkpoint{Epoch: 0, Root: types.Hash{0xAA}},
		Target:          Checkpoint{Epoch: 1, Root: types.Hash{0xBB}},
	}
	att2 := &AttestationData{
		Slot:            10,
		BeaconBlockRoot: types.Hash{0x02},
		Source:          Checkpoint{Epoch: 0, Root: types.Hash{0xAA}},
		Target:          Checkpoint{Epoch: 1, Root: types.Hash{0xCC}},
	}
	d.CheckAttestation(att1, ValidatorIndex(2))
	d.CheckAttestation(att2, ValidatorIndex(2))

	proposals := d.GetSlashingsByType(DoubleProposal)
	if len(proposals) != 1 {
		t.Errorf("expected 1 double proposal, got %d", len(proposals))
	}

	doubleVotes := d.GetSlashingsByType(DoubleVote7)
	if len(doubleVotes) != 1 {
		t.Errorf("expected 1 double vote, got %d", len(doubleVotes))
	}
}

func TestGetSlashedValidators(t *testing.T) {
	d := NewEquivocationDetector(nil)

	d.CheckProposal(10, ValidatorIndex(5), [32]byte{0x01})
	d.CheckProposal(10, ValidatorIndex(5), [32]byte{0x02})
	d.CheckProposal(20, ValidatorIndex(3), [32]byte{0x03})
	d.CheckProposal(20, ValidatorIndex(3), [32]byte{0x04})

	validators := d.GetSlashedValidators()
	if len(validators) != 2 {
		t.Fatalf("expected 2 validators, got %d", len(validators))
	}
	// Should be sorted.
	if validators[0] != ValidatorIndex(3) || validators[1] != ValidatorIndex(5) {
		t.Errorf("expected [3, 5], got %v", validators)
	}
}

func TestDifferentValidatorsNoConflict(t *testing.T) {
	d := NewEquivocationDetector(nil)

	att1 := &AttestationData{
		Slot:            10,
		BeaconBlockRoot: types.Hash{0x01},
		Source:          Checkpoint{Epoch: 0, Root: types.Hash{0xAA}},
		Target:          Checkpoint{Epoch: 1, Root: types.Hash{0xBB}},
	}
	att2 := &AttestationData{
		Slot:            10,
		BeaconBlockRoot: types.Hash{0x02},
		Source:          Checkpoint{Epoch: 0, Root: types.Hash{0xAA}},
		Target:          Checkpoint{Epoch: 1, Root: types.Hash{0xCC}},
	}

	d.CheckAttestation(att1, ValidatorIndex(1))
	ev := d.CheckAttestation(att2, ValidatorIndex(2)) // different validator
	if ev != nil {
		t.Error("different validators should not trigger equivocation")
	}
}
