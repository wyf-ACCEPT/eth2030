package light

import (
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

func TestSyncCommitteePeriod(t *testing.T) {
	tests := []struct {
		slot   uint64
		period uint64
	}{
		{0, 0},
		{1, 0},
		{8191, 0},
		{8192, 1},
		{8193, 1},
		{16384, 2},
		{24576, 3},
	}

	for _, tt := range tests {
		got := SyncCommitteePeriod(tt.slot)
		if got != tt.period {
			t.Errorf("SyncCommitteePeriod(%d) = %d, want %d", tt.slot, got, tt.period)
		}
	}
}

func TestSyncCommitteePeriodStartSlot(t *testing.T) {
	tests := []struct {
		period uint64
		slot   uint64
	}{
		{0, 0},
		{1, 8192},
		{2, 16384},
		{10, 81920},
	}

	for _, tt := range tests {
		got := SyncCommitteePeriodStartSlot(tt.period)
		if got != tt.slot {
			t.Errorf("SyncCommitteePeriodStartSlot(%d) = %d, want %d", tt.period, got, tt.slot)
		}
	}
}

func TestComputeCommitteeRoot(t *testing.T) {
	pubkeys := [][]byte{
		{0x01, 0x02},
		{0x03, 0x04},
	}

	root1 := ComputeCommitteeRoot(pubkeys)
	root2 := ComputeCommitteeRoot(pubkeys)

	if root1 != root2 {
		t.Error("committee root should be deterministic")
	}

	if root1.IsZero() {
		t.Error("committee root should not be zero")
	}

	// Different pubkeys should produce different root.
	pubkeys2 := [][]byte{
		{0x05, 0x06},
		{0x07, 0x08},
	}
	root3 := ComputeCommitteeRoot(pubkeys2)
	if root1 == root3 {
		t.Error("different pubkeys should produce different root")
	}
}

func TestVerifySyncCommitteeSignature(t *testing.T) {
	committee := MakeTestSyncCommittee(0)
	signingRoot := types.HexToHash("0xabcdef")

	// Create supermajority bits (all 512 validators signing).
	bits := MakeCommitteeBits(SyncCommitteeSize)

	// Create valid signature.
	sig := SignSyncCommittee(committee, signingRoot, bits)

	// Verify should pass.
	if err := VerifySyncCommitteeSignature(committee, signingRoot, bits, sig); err != nil {
		t.Fatalf("valid signature should verify: %v", err)
	}
}

func TestVerifySyncCommitteeSignature_InvalidSignature(t *testing.T) {
	committee := MakeTestSyncCommittee(0)
	signingRoot := types.HexToHash("0xabcdef")
	bits := MakeCommitteeBits(SyncCommitteeSize)

	badSig := make([]byte, 32)
	err := VerifySyncCommitteeSignature(committee, signingRoot, bits, badSig)
	if err != ErrInvalidSignature {
		t.Errorf("expected ErrInvalidSignature, got %v", err)
	}
}

func TestVerifySyncCommitteeSignature_InsufficientParticipation(t *testing.T) {
	committee := MakeTestSyncCommittee(0)
	signingRoot := types.HexToHash("0xabcdef")

	// Only 100 out of 512 signers -- not supermajority.
	bits := MakeCommitteeBits(100)
	sig := SignSyncCommittee(committee, signingRoot, bits)

	err := VerifySyncCommitteeSignature(committee, signingRoot, bits, sig)
	if err != ErrInsufficientParticipation {
		t.Errorf("expected ErrInsufficientParticipation, got %v", err)
	}
}

func TestVerifySyncCommitteeSignature_NilCommittee(t *testing.T) {
	err := VerifySyncCommitteeSignature(nil, types.Hash{}, nil, nil)
	if err != ErrNilCommittee {
		t.Errorf("expected ErrNilCommittee, got %v", err)
	}
}

func TestVerifySyncCommitteeSignature_WrongSize(t *testing.T) {
	committee := &SyncCommittee{
		Pubkeys: make([][]byte, 10), // wrong size
		Period:  0,
	}
	err := VerifySyncCommitteeSignature(committee, types.Hash{}, nil, nil)
	if err != ErrCommitteeWrongSize {
		t.Errorf("expected ErrCommitteeWrongSize, got %v", err)
	}
}

func TestNextSyncCommittee(t *testing.T) {
	current := MakeTestSyncCommittee(0)

	next, err := NextSyncCommittee(current)
	if err != nil {
		t.Fatalf("NextSyncCommittee failed: %v", err)
	}

	if next.Period != 1 {
		t.Errorf("next period = %d, want 1", next.Period)
	}
	if len(next.Pubkeys) != SyncCommitteeSize {
		t.Errorf("next pubkeys count = %d, want %d", len(next.Pubkeys), SyncCommitteeSize)
	}

	// Pubkeys should differ from current.
	samePK := 0
	for i := 0; i < SyncCommitteeSize; i++ {
		if len(current.Pubkeys[i]) == len(next.Pubkeys[i]) {
			same := true
			for j := range current.Pubkeys[i] {
				if current.Pubkeys[i][j] != next.Pubkeys[i][j] {
					same = false
					break
				}
			}
			if same {
				samePK++
			}
		}
	}
	if samePK > 0 {
		t.Errorf("%d pubkeys are identical after rotation, expected all different", samePK)
	}

	// Should be deterministic.
	next2, _ := NextSyncCommittee(current)
	for i := 0; i < SyncCommitteeSize; i++ {
		for j := range next.Pubkeys[i] {
			if next.Pubkeys[i][j] != next2.Pubkeys[i][j] {
				t.Fatal("NextSyncCommittee should be deterministic")
			}
		}
	}
}

func TestNextSyncCommittee_NilInput(t *testing.T) {
	_, err := NextSyncCommittee(nil)
	if err != ErrNilCommittee {
		t.Errorf("expected ErrNilCommittee, got %v", err)
	}
}

func TestSyncCommitteeUpdate(t *testing.T) {
	current := MakeTestSyncCommittee(5)
	next, err := NextSyncCommittee(current)
	if err != nil {
		t.Fatal(err)
	}

	rotated, err := SyncCommitteeUpdate(current, next)
	if err != nil {
		t.Fatalf("SyncCommitteeUpdate failed: %v", err)
	}
	if rotated.Period != 6 {
		t.Errorf("rotated period = %d, want 6", rotated.Period)
	}
}

func TestSyncCommitteeUpdate_BadPeriod(t *testing.T) {
	current := MakeTestSyncCommittee(5)
	next := MakeTestSyncCommittee(10) // not current+1

	_, err := SyncCommitteeUpdate(current, next)
	if err == nil {
		t.Error("expected error for non-sequential period")
	}
}

func TestSyncCommitteeUpdate_NilInputs(t *testing.T) {
	committee := MakeTestSyncCommittee(0)
	if _, err := SyncCommitteeUpdate(nil, committee); err != ErrNilCommittee {
		t.Errorf("expected ErrNilCommittee, got %v", err)
	}
	if _, err := SyncCommitteeUpdate(committee, nil); err != ErrNilCommittee {
		t.Errorf("expected ErrNilCommittee, got %v", err)
	}
}

func TestProcessBootstrap(t *testing.T) {
	committee := MakeTestSyncCommittee(0)
	committeeRoot := ComputeCommitteeRoot(committee.Pubkeys)

	header := &types.Header{
		Number: new(big.Int).SetUint64(100),
		Root:   types.HexToHash("0xdeadbeef"),
	}

	bootstrap := &LightClientBootstrap{
		Header:           header,
		CurrentCommittee: committee,
		CommitteeRoot:    committeeRoot,
	}

	// With matching trusted root.
	state, err := ProcessBootstrap(bootstrap, header.Root)
	if err != nil {
		t.Fatalf("ProcessBootstrap failed: %v", err)
	}
	if state.CurrentSlot != 100 {
		t.Errorf("slot = %d, want 100", state.CurrentSlot)
	}
	if state.FinalizedHeader != header {
		t.Error("finalized header mismatch")
	}
	if state.CurrentCommittee != committee {
		t.Error("committee mismatch")
	}

	// With zero trusted root (skip root check).
	state2, err := ProcessBootstrap(bootstrap, types.Hash{})
	if err != nil {
		t.Fatalf("ProcessBootstrap with zero root failed: %v", err)
	}
	if state2.CurrentSlot != 100 {
		t.Errorf("slot = %d, want 100", state2.CurrentSlot)
	}
}

func TestProcessBootstrap_NilInput(t *testing.T) {
	if _, err := ProcessBootstrap(nil, types.Hash{}); err != ErrNilBootstrap {
		t.Errorf("expected ErrNilBootstrap, got %v", err)
	}
}

func TestProcessBootstrap_NilHeader(t *testing.T) {
	bootstrap := &LightClientBootstrap{
		CurrentCommittee: MakeTestSyncCommittee(0),
	}
	if _, err := ProcessBootstrap(bootstrap, types.Hash{}); err != ErrNoFinalizedHdr {
		t.Errorf("expected ErrNoFinalizedHdr, got %v", err)
	}
}

func TestProcessBootstrap_RootMismatch(t *testing.T) {
	committee := MakeTestSyncCommittee(0)
	committeeRoot := ComputeCommitteeRoot(committee.Pubkeys)

	header := &types.Header{
		Number: new(big.Int).SetUint64(1),
		Root:   types.HexToHash("0xaaa"),
	}

	bootstrap := &LightClientBootstrap{
		Header:           header,
		CurrentCommittee: committee,
		CommitteeRoot:    committeeRoot,
	}

	_, err := ProcessBootstrap(bootstrap, types.HexToHash("0xbbb"))
	if err != ErrBootstrapMismatch {
		t.Errorf("expected ErrBootstrapMismatch, got %v", err)
	}
}

func TestProcessBootstrap_CommitteeRootMismatch(t *testing.T) {
	committee := MakeTestSyncCommittee(0)

	header := &types.Header{
		Number: new(big.Int).SetUint64(1),
		Root:   types.HexToHash("0xaaa"),
	}

	bootstrap := &LightClientBootstrap{
		Header:           header,
		CurrentCommittee: committee,
		CommitteeRoot:    types.HexToHash("0xbadroot"),
	}

	_, err := ProcessBootstrap(bootstrap, types.Hash{})
	if err != ErrBootstrapMismatch {
		t.Errorf("expected ErrBootstrapMismatch for bad committee root, got %v", err)
	}
}

func TestProcessIncrementalUpdate(t *testing.T) {
	committee := MakeTestSyncCommittee(0)

	attestedHeader := &types.Header{
		Number: new(big.Int).SetUint64(200),
	}
	finalizedHeader := &types.Header{
		Number: new(big.Int).SetUint64(190),
	}

	bits := MakeCommitteeBits(SyncCommitteeSize)
	signingRoot := attestedHeader.Hash()
	sig := SignSyncCommittee(committee, signingRoot, bits)

	state := &LightClientState{
		CurrentSlot:      100,
		FinalizedHeader:  &types.Header{Number: new(big.Int).SetUint64(100)},
		CurrentCommittee: committee,
	}

	update := &LightClientIncrementalUpdate{
		AttestedHeader:    attestedHeader,
		FinalizedHeader:   finalizedHeader,
		SyncCommitteeBits: bits,
		Signature:         sig,
	}

	if err := ProcessIncrementalUpdate(state, update); err != nil {
		t.Fatalf("ProcessIncrementalUpdate failed: %v", err)
	}

	if state.FinalizedHeader != finalizedHeader {
		t.Error("finalized header not updated")
	}
	if state.CurrentSlot != 200 {
		t.Errorf("slot = %d, want 200", state.CurrentSlot)
	}
}

func TestProcessIncrementalUpdate_WithCommitteeRotation(t *testing.T) {
	committee := MakeTestSyncCommittee(0)
	nextCommittee, err := NextSyncCommittee(committee)
	if err != nil {
		t.Fatal(err)
	}

	attestedHeader := &types.Header{
		Number: new(big.Int).SetUint64(300),
	}
	finalizedHeader := &types.Header{
		Number: new(big.Int).SetUint64(290),
	}

	bits := MakeCommitteeBits(SyncCommitteeSize)
	signingRoot := attestedHeader.Hash()
	sig := SignSyncCommittee(committee, signingRoot, bits)

	state := &LightClientState{
		CurrentSlot:      100,
		FinalizedHeader:  &types.Header{Number: new(big.Int).SetUint64(100)},
		CurrentCommittee: committee,
	}

	update := &LightClientIncrementalUpdate{
		AttestedHeader:    attestedHeader,
		FinalizedHeader:   finalizedHeader,
		SyncCommitteeBits: bits,
		Signature:         sig,
		NextSyncCommittee: nextCommittee,
	}

	if err := ProcessIncrementalUpdate(state, update); err != nil {
		t.Fatalf("ProcessIncrementalUpdate with rotation failed: %v", err)
	}

	if state.CurrentCommittee.Period != 1 {
		t.Errorf("committee period = %d, want 1", state.CurrentCommittee.Period)
	}
}

func TestProcessIncrementalUpdate_RegressingFinality(t *testing.T) {
	committee := MakeTestSyncCommittee(0)

	attestedHeader := &types.Header{
		Number: new(big.Int).SetUint64(50),
	}
	finalizedHeader := &types.Header{
		Number: new(big.Int).SetUint64(40),
	}

	bits := MakeCommitteeBits(SyncCommitteeSize)
	sig := SignSyncCommittee(committee, attestedHeader.Hash(), bits)

	state := &LightClientState{
		CurrentSlot:      100,
		FinalizedHeader:  &types.Header{Number: new(big.Int).SetUint64(100)},
		CurrentCommittee: committee,
	}

	update := &LightClientIncrementalUpdate{
		AttestedHeader:    attestedHeader,
		FinalizedHeader:   finalizedHeader,
		SyncCommitteeBits: bits,
		Signature:         sig,
	}

	err := ProcessIncrementalUpdate(state, update)
	if err != ErrUpdateNotNewer {
		t.Errorf("expected ErrUpdateNotNewer, got %v", err)
	}
}

func TestProcessIncrementalUpdate_NilUpdate(t *testing.T) {
	state := &LightClientState{}
	if err := ProcessIncrementalUpdate(state, nil); err != ErrNilUpdate {
		t.Errorf("expected ErrNilUpdate, got %v", err)
	}
}

func TestProcessIncrementalUpdate_BadSignature(t *testing.T) {
	committee := MakeTestSyncCommittee(0)

	attestedHeader := &types.Header{
		Number: new(big.Int).SetUint64(200),
	}
	finalizedHeader := &types.Header{
		Number: new(big.Int).SetUint64(190),
	}

	bits := MakeCommitteeBits(SyncCommitteeSize)

	state := &LightClientState{
		CurrentSlot:      100,
		FinalizedHeader:  &types.Header{Number: new(big.Int).SetUint64(100)},
		CurrentCommittee: committee,
	}

	update := &LightClientIncrementalUpdate{
		AttestedHeader:    attestedHeader,
		FinalizedHeader:   finalizedHeader,
		SyncCommitteeBits: bits,
		Signature:         []byte{0x00, 0x01, 0x02}, // bad signature
	}

	err := ProcessIncrementalUpdate(state, update)
	if err != ErrInvalidSignature {
		t.Errorf("expected ErrInvalidSignature, got %v", err)
	}
}

func TestMakeTestSyncCommittee(t *testing.T) {
	c0 := MakeTestSyncCommittee(0)
	if len(c0.Pubkeys) != SyncCommitteeSize {
		t.Fatalf("pubkeys count = %d, want %d", len(c0.Pubkeys), SyncCommitteeSize)
	}
	if c0.Period != 0 {
		t.Errorf("period = %d, want 0", c0.Period)
	}
	if len(c0.AggregatePubkey) == 0 {
		t.Error("aggregate pubkey should not be empty")
	}

	// Different periods should produce different committees.
	c1 := MakeTestSyncCommittee(1)
	if c0.AggregatePubkey[0] == c1.AggregatePubkey[0] &&
		c0.AggregatePubkey[1] == c1.AggregatePubkey[1] {
		// Very unlikely to be the same, but check the first pubkey too.
		same := true
		for i := range c0.Pubkeys[0] {
			if c0.Pubkeys[0][i] != c1.Pubkeys[0][i] {
				same = false
				break
			}
		}
		if same {
			t.Error("different periods should produce different pubkeys")
		}
	}
}

func TestCountBits(t *testing.T) {
	tests := []struct {
		data  []byte
		count int
	}{
		{nil, 0},
		{[]byte{0x00}, 0},
		{[]byte{0x01}, 1},
		{[]byte{0xff}, 8},
		{[]byte{0xff, 0xff}, 16},
		{[]byte{0xaa}, 4}, // 10101010
	}

	for _, tt := range tests {
		if got := countBits(tt.data); got != tt.count {
			t.Errorf("countBits(%x) = %d, want %d", tt.data, got, tt.count)
		}
	}
}
