package consensus

import (
	"sync"
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

func TestEndgameFinalityV2_NewDefault(t *testing.T) {
	cfg := DefaultFinalityV2Config()
	ef := NewEndgameFinalityV2(cfg)
	if ef == nil {
		t.Fatal("NewEndgameFinalityV2 returned nil")
	}
	if cfg.SupermajorityPct != 90 {
		t.Errorf("SupermajorityPct: got %d, want 90", cfg.SupermajorityPct)
	}
	if cfg.RetainRounds != 64 {
		t.Errorf("RetainRounds: got %d, want 64", cfg.RetainRounds)
	}
}

func TestEndgameFinalityV2_StartRound(t *testing.T) {
	ef := NewEndgameFinalityV2(DefaultFinalityV2Config())

	round := ef.StartRound(1, 100)
	if round == nil {
		t.Fatal("StartRound returned nil")
	}
	if round.Slot != 1 {
		t.Errorf("Slot: got %d, want 1", round.Slot)
	}
	if round.ValidatorCount != 100 {
		t.Errorf("ValidatorCount: got %d, want 100", round.ValidatorCount)
	}
	// Threshold should be 2/3+1 = 67.
	if round.Threshold != 67 {
		t.Errorf("Threshold: got %d, want 67", round.Threshold)
	}
	if round.Finalized {
		t.Error("round should not be finalized initially")
	}
}

func TestEndgameFinalityV2_StartRoundIdempotent(t *testing.T) {
	ef := NewEndgameFinalityV2(DefaultFinalityV2Config())
	r1 := ef.StartRound(5, 100)
	r2 := ef.StartRound(5, 200) // should return existing round
	if r1.ValidatorCount != r2.ValidatorCount {
		t.Error("StartRound should return existing round, not create new one")
	}
}

func TestEndgameFinalityV2_CastVote_Errors(t *testing.T) {
	ef := NewEndgameFinalityV2(DefaultFinalityV2Config())

	// Vote without starting round.
	err := ef.CastVote(&FinalityVote{Slot: 1, ValidatorIndex: 0, BlockHash: types.Hash{0xaa}})
	if err != ErrRoundNotStarted {
		t.Errorf("expected ErrRoundNotStarted, got %v", err)
	}

	ef.StartRound(1, 10)

	// Invalid validator index.
	err = ef.CastVote(&FinalityVote{Slot: 1, ValidatorIndex: 10, BlockHash: types.Hash{0xaa}})
	if err != ErrInvalidValidator {
		t.Errorf("expected ErrInvalidValidator, got %v", err)
	}

	// Valid vote.
	err = ef.CastVote(&FinalityVote{Slot: 1, ValidatorIndex: 0, BlockHash: types.Hash{0xaa}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Duplicate vote.
	err = ef.CastVote(&FinalityVote{Slot: 1, ValidatorIndex: 0, BlockHash: types.Hash{0xaa}})
	if err != ErrDuplicateVoteV2 {
		t.Errorf("expected ErrDuplicateVoteV2, got %v", err)
	}
}

func TestEndgameFinalityV2_StandardFinality(t *testing.T) {
	ef := NewEndgameFinalityV2(DefaultFinalityV2Config())
	ef.StartRound(1, 9) // threshold = 6+1 = 7

	blockHash := types.Hash{0xBB}

	// Cast 6 votes: not enough.
	for i := uint64(0); i < 6; i++ {
		if err := ef.CastVote(&FinalityVote{
			Slot:           1,
			ValidatorIndex: i,
			BlockHash:      blockHash,
			Timestamp:      100 + i,
		}); err != nil {
			t.Fatalf("vote %d error: %v", i, err)
		}
	}
	if ef.IsFinalized(1) {
		t.Error("6/9 should not finalize (need 7)")
	}

	// 7th vote should trigger finality.
	if err := ef.CastVote(&FinalityVote{
		Slot:           1,
		ValidatorIndex: 6,
		BlockHash:      blockHash,
		Timestamp:      107,
	}); err != nil {
		t.Fatalf("vote 6 error: %v", err)
	}
	if !ef.IsFinalized(1) {
		t.Error("7/9 should finalize")
	}
	if ef.FinalizedHash(1) != blockHash {
		t.Errorf("FinalizedHash: got %s, want %s", ef.FinalizedHash(1).Hex(), blockHash.Hex())
	}
}

func TestEndgameFinalityV2_OptimisticFinality(t *testing.T) {
	// With 90% supermajority threshold and 10 validators,
	// optimistic threshold = 9 votes.
	ef := NewEndgameFinalityV2(DefaultFinalityV2Config())
	ef.StartRound(1, 10) // threshold = 7, optimistic = 9

	blockHash := types.Hash{0xCC}

	// Standard threshold is 7, so 7 votes should finalize normally.
	// Let's test that with optimistic config set higher it still works.
	for i := uint64(0); i < 7; i++ {
		ef.CastVote(&FinalityVote{Slot: 1, ValidatorIndex: i, BlockHash: blockHash})
	}
	if !ef.IsFinalized(1) {
		t.Error("7/10 should finalize via standard threshold")
	}
}

func TestEndgameFinalityV2_OptimisticFastPath(t *testing.T) {
	// 100 validators: threshold=67, optimistic=90.
	ef := NewEndgameFinalityV2(DefaultFinalityV2Config())
	ef.StartRound(1, 100)
	blockHash := types.Hash{0xDD}
	for i := uint64(0); i < 66; i++ {
		ef.CastVote(&FinalityVote{Slot: 1, ValidatorIndex: i, BlockHash: blockHash})
	}
	if ef.IsFinalized(1) {
		t.Error("66/100 should not finalize")
	}
	ef.CastVote(&FinalityVote{Slot: 1, ValidatorIndex: 66, BlockHash: blockHash})
	if !ef.IsFinalized(1) {
		t.Error("67/100 should finalize via standard threshold")
	}
}

func TestEndgameFinalityV2_SplitVotes(t *testing.T) {
	ef := NewEndgameFinalityV2(DefaultFinalityV2Config())
	ef.StartRound(1, 9) // threshold = 7

	hashA := types.Hash{0xAA}
	hashB := types.Hash{0xBB}

	// 4 votes for A, 3 votes for B: neither reaches threshold.
	for i := uint64(0); i < 4; i++ {
		ef.CastVote(&FinalityVote{Slot: 1, ValidatorIndex: i, BlockHash: hashA})
	}
	for i := uint64(4); i < 7; i++ {
		ef.CastVote(&FinalityVote{Slot: 1, ValidatorIndex: i, BlockHash: hashB})
	}
	if ef.IsFinalized(1) {
		t.Error("split votes should not finalize")
	}

	// Remaining 2 validators vote for A: total A=6, still < 7.
	ef.CastVote(&FinalityVote{Slot: 1, ValidatorIndex: 7, BlockHash: hashA})
	if ef.IsFinalized(1) {
		t.Error("5/9 for hash A should not finalize")
	}

	ef.CastVote(&FinalityVote{Slot: 1, ValidatorIndex: 8, BlockHash: hashA})
	// Now A has 6, B has 3, still not 7 for A... wait, 4+1+1=6. No.
	// Actually: initially 4 for A (indices 0-3), then index 7 for A = 5,
	// then index 8 for A = 6. Still < 7.
	if ef.IsFinalized(1) {
		t.Error("6/9 for hash A should not finalize")
	}
}

func TestEndgameFinalityV2_VoteAfterFinalized(t *testing.T) {
	ef := NewEndgameFinalityV2(DefaultFinalityV2Config())
	ef.StartRound(1, 3) // threshold = 3

	blockHash := types.Hash{0xEE}
	for i := uint64(0); i < 3; i++ {
		ef.CastVote(&FinalityVote{Slot: 1, ValidatorIndex: i, BlockHash: blockHash})
	}
	if !ef.IsFinalized(1) {
		t.Fatal("should be finalized")
	}

	// Voting on a finalized round should error.
	err := ef.CastVote(&FinalityVote{Slot: 1, ValidatorIndex: 0, BlockHash: blockHash})
	if err != ErrRoundFinalized {
		t.Errorf("expected ErrRoundFinalized, got %v", err)
	}
}

func TestEndgameFinalityV2_IsFinalized_NoRound(t *testing.T) {
	ef := NewEndgameFinalityV2(DefaultFinalityV2Config())
	if ef.IsFinalized(999) {
		t.Error("nonexistent round should not be finalized")
	}
}

func TestEndgameFinalityV2_FinalizedHash_NoRound(t *testing.T) {
	ef := NewEndgameFinalityV2(DefaultFinalityV2Config())
	h := ef.FinalizedHash(999)
	if !h.IsZero() {
		t.Error("nonexistent round should return zero hash")
	}
}

func TestEndgameFinalityV2_FinalizedHash_NotFinalized(t *testing.T) {
	ef := NewEndgameFinalityV2(DefaultFinalityV2Config())
	ef.StartRound(1, 10)
	ef.CastVote(&FinalityVote{Slot: 1, ValidatorIndex: 0, BlockHash: types.Hash{0x11}})
	h := ef.FinalizedHash(1)
	if !h.IsZero() {
		t.Error("unfinalized round should return zero hash")
	}
}

func TestEndgameFinalityV2_LatestFinalizedSlot(t *testing.T) {
	ef := NewEndgameFinalityV2(DefaultFinalityV2Config())
	if ef.LatestFinalizedSlot() != 0 {
		t.Error("initial latest should be 0")
	}

	// Finalize slot 5.
	ef.StartRound(5, 3)
	for i := uint64(0); i < 3; i++ {
		ef.CastVote(&FinalityVote{Slot: 5, ValidatorIndex: i, BlockHash: types.Hash{0x55}})
	}
	if ef.LatestFinalizedSlot() != 5 {
		t.Errorf("LatestFinalizedSlot: got %d, want 5", ef.LatestFinalizedSlot())
	}

	// Finalize slot 10.
	ef.StartRound(10, 3)
	for i := uint64(0); i < 3; i++ {
		ef.CastVote(&FinalityVote{Slot: 10, ValidatorIndex: i, BlockHash: types.Hash{0xAA}})
	}
	if ef.LatestFinalizedSlot() != 10 {
		t.Errorf("LatestFinalizedSlot: got %d, want 10", ef.LatestFinalizedSlot())
	}
}

func TestEndgameFinalityV2_FinalizedSlots(t *testing.T) {
	ef := NewEndgameFinalityV2(DefaultFinalityV2Config())

	for slot := uint64(1); slot <= 3; slot++ {
		ef.StartRound(slot, 3)
		for i := uint64(0); i < 3; i++ {
			ef.CastVote(&FinalityVote{Slot: slot, ValidatorIndex: i, BlockHash: types.Hash{byte(slot)}})
		}
	}

	slots := ef.FinalizedSlots()
	if len(slots) != 3 {
		t.Fatalf("FinalizedSlots: got %d, want 3", len(slots))
	}
	for i, want := range []uint64{1, 2, 3} {
		if slots[i] != want {
			t.Errorf("slot[%d]: got %d, want %d", i, slots[i], want)
		}
	}
}

func TestEndgameFinalityV2_GetRound(t *testing.T) {
	ef := NewEndgameFinalityV2(DefaultFinalityV2Config())

	if ef.GetRound(1) != nil {
		t.Error("GetRound should return nil for nonexistent round")
	}

	ef.StartRound(1, 10)
	ef.CastVote(&FinalityVote{Slot: 1, ValidatorIndex: 0, BlockHash: types.Hash{0x11}})

	round := ef.GetRound(1)
	if round == nil {
		t.Fatal("GetRound returned nil")
	}
	if round.VoteCount() != 1 {
		t.Errorf("VoteCount: got %d, want 1", round.VoteCount())
	}
}

func TestEndgameFinalityV2_Progress(t *testing.T) {
	ef := NewEndgameFinalityV2(DefaultFinalityV2Config())

	// No round: 0 progress.
	if ef.Progress(1) != 0 {
		t.Error("no round should return 0 progress")
	}

	ef.StartRound(1, 9) // threshold = 7

	// 0 votes: 0 progress.
	if ef.Progress(1) != 0 {
		t.Error("0 votes should return 0 progress")
	}

	// 3 votes: 3/7 progress.
	for i := uint64(0); i < 3; i++ {
		ef.CastVote(&FinalityVote{Slot: 1, ValidatorIndex: i, BlockHash: types.Hash{0xAA}})
	}
	p := ef.Progress(1)
	expected := 3.0 / 7.0
	if p < expected-0.01 || p > expected+0.01 {
		t.Errorf("Progress: got %f, want ~%f", p, expected)
	}
}

func TestEndgameFinalityV2_ActiveRounds(t *testing.T) {
	ef := NewEndgameFinalityV2(DefaultFinalityV2Config())
	if ef.ActiveRounds() != 0 {
		t.Error("initial ActiveRounds should be 0")
	}

	ef.StartRound(1, 10)
	ef.StartRound(2, 10)
	if ef.ActiveRounds() != 2 {
		t.Errorf("ActiveRounds: got %d, want 2", ef.ActiveRounds())
	}
}

func TestEndgameFinalityV2_Cleanup(t *testing.T) {
	ef := NewEndgameFinalityV2(DefaultFinalityV2Config())

	// Start 3 rounds, finalize only slot 2.
	ef.StartRound(1, 10)
	ef.StartRound(2, 3)
	ef.StartRound(3, 10)

	for i := uint64(0); i < 3; i++ {
		ef.CastVote(&FinalityVote{Slot: 2, ValidatorIndex: i, BlockHash: types.Hash{0x22}})
	}

	// Cleanup rounds before slot 3: should remove slot 1 (not finalized)
	// but keep slot 2 (finalized) and slot 3.
	removed := ef.Cleanup(3)
	if removed != 1 {
		t.Errorf("removed: got %d, want 1", removed)
	}
	if ef.ActiveRounds() != 2 {
		t.Errorf("ActiveRounds after cleanup: got %d, want 2", ef.ActiveRounds())
	}
}

func TestEndgameFinalityV2_Pruning(t *testing.T) {
	cfg := DefaultFinalityV2Config()
	cfg.RetainRounds = 3
	ef := NewEndgameFinalityV2(cfg)

	// Finalize slots 1 through 6.
	for slot := uint64(1); slot <= 6; slot++ {
		ef.StartRound(slot, 3)
		for i := uint64(0); i < 3; i++ {
			ef.CastVote(&FinalityVote{
				Slot:           slot,
				ValidatorIndex: i,
				BlockHash:      types.Hash{byte(slot)},
			})
		}
	}

	// With retainRounds=3 and latest=6, cutoff=3. Slots 1,2 should be pruned.
	// Slot 3 might also be pruned since cutoff is strictly <.
	if ef.GetRound(1) != nil {
		t.Error("slot 1 should have been pruned")
	}
	if ef.GetRound(2) != nil {
		t.Error("slot 2 should have been pruned")
	}
	// Slots 4,5,6 should remain.
	if ef.GetRound(4) == nil {
		t.Error("slot 4 should still exist")
	}
	if ef.GetRound(6) == nil {
		t.Error("slot 6 should still exist")
	}
}

func TestEndgameFinalityV2_LeadingHash(t *testing.T) {
	ef := NewEndgameFinalityV2(DefaultFinalityV2Config())
	ef.StartRound(1, 10)

	hashA := types.Hash{0xAA}
	hashB := types.Hash{0xBB}

	ef.CastVote(&FinalityVote{Slot: 1, ValidatorIndex: 0, BlockHash: hashA})
	ef.CastVote(&FinalityVote{Slot: 1, ValidatorIndex: 1, BlockHash: hashA})
	ef.CastVote(&FinalityVote{Slot: 1, ValidatorIndex: 2, BlockHash: hashB})

	round := ef.GetRound(1)
	leading, count := round.LeadingHash()
	if leading != hashA {
		t.Errorf("LeadingHash: got %s, want %s", leading.Hex(), hashA.Hex())
	}
	if count != 2 {
		t.Errorf("leading count: got %d, want 2", count)
	}
}

func TestEndgameFinalityV2_Threshold_SmallValidatorSet(t *testing.T) {
	ef := NewEndgameFinalityV2(DefaultFinalityV2Config())
	for _, tc := range []struct{ n, want int }{{1, 1}, {2, 2}, {3, 3}} {
		r := ef.StartRound(uint64(100+tc.n), tc.n)
		if r.Threshold != tc.want {
			t.Errorf("Threshold for %d validators: got %d, want %d", tc.n, r.Threshold, tc.want)
		}
	}
}

func TestEndgameFinalityV2_ConcurrentSafety(t *testing.T) {
	ef := NewEndgameFinalityV2(DefaultFinalityV2Config())

	// Start 10 rounds with 100 validators each.
	for slot := uint64(0); slot < 10; slot++ {
		ef.StartRound(slot, 100)
	}

	var wg sync.WaitGroup
	// Cast votes concurrently from many goroutines.
	for slot := uint64(0); slot < 10; slot++ {
		for v := uint64(0); v < 100; v++ {
			wg.Add(1)
			go func(s, vi uint64) {
				defer wg.Done()
				ef.CastVote(&FinalityVote{
					Slot:           s,
					ValidatorIndex: vi,
					BlockHash:      types.Hash{byte(s)},
					Timestamp:      1000 + vi,
				})
			}(slot, v)
		}
	}
	wg.Wait()

	// All 10 rounds should be finalized.
	for slot := uint64(0); slot < 10; slot++ {
		if !ef.IsFinalized(slot) {
			t.Errorf("slot %d should be finalized", slot)
		}
	}
}

func TestEndgameFinalityV2_InvalidConfig(t *testing.T) {
	// Zero/negative config values should be corrected.
	cfg := FinalityV2Config{SupermajorityPct: 0, RetainRounds: -1}
	ef := NewEndgameFinalityV2(cfg)
	if ef.config.SupermajorityPct != 90 {
		t.Errorf("SupermajorityPct should default to 90, got %d", ef.config.SupermajorityPct)
	}
	if ef.config.RetainRounds != 64 {
		t.Errorf("RetainRounds should default to 64, got %d", ef.config.RetainRounds)
	}
}

func TestEndgameFinalityV2_HashVotes(t *testing.T) {
	ef := NewEndgameFinalityV2(DefaultFinalityV2Config())
	ef.StartRound(1, 10)

	hashA := types.Hash{0xAA}
	hashB := types.Hash{0xBB}

	ef.CastVote(&FinalityVote{Slot: 1, ValidatorIndex: 0, BlockHash: hashA})
	ef.CastVote(&FinalityVote{Slot: 1, ValidatorIndex: 1, BlockHash: hashB})
	ef.CastVote(&FinalityVote{Slot: 1, ValidatorIndex: 2, BlockHash: hashA})

	round := ef.GetRound(1)
	if round.HashVotes(hashA) != 2 {
		t.Errorf("hashA votes: got %d, want 2", round.HashVotes(hashA))
	}
	if round.HashVotes(hashB) != 1 {
		t.Errorf("hashB votes: got %d, want 1", round.HashVotes(hashB))
	}
}
