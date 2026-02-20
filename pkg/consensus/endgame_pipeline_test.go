package consensus

import (
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

// mockExecutionEngine implements ExecutionEngine for testing.
type mockExecutionEngine struct {
	returnRoot types.Hash
	returnErr  error
}

func (m *mockExecutionEngine) ExecuteBlock(block *SignedBeaconBlock, parentStateRoot types.Hash) (types.Hash, error) {
	if m.returnErr != nil {
		return types.Hash{}, m.returnErr
	}
	return m.returnRoot, nil
}

// mockBLSVerifier implements BLSVerifier for testing.
type mockBLSVerifier struct {
	returnValid bool
}

func (m *mockBLSVerifier) VerifyAggregate(pubkeys [][48]byte, message []byte, aggSig [96]byte) bool {
	return m.returnValid
}

func makePipelineTestBlock(slot Slot) *SignedBeaconBlock {
	return &SignedBeaconBlock{
		Block: &ExtBeaconBlock{
			Slot:       slot,
			ParentRoot: types.Hash{0x01},
			StateRoot:  types.Hash{0x02},
			Body: &ExtBeaconBlockBody{
				Graffiti: [32]byte{0xAA},
			},
		},
	}
}

func makePipelineTestVotes(slot uint64, root types.Hash, count int, stakeEach uint64) []SSFRoundVote {
	votes := make([]SSFRoundVote, count)
	for i := 0; i < count; i++ {
		var pkHash types.Hash
		pkHash[0] = byte(i)
		pkHash[1] = byte(i >> 8)
		votes[i] = SSFRoundVote{
			ValidatorPubkeyHash: pkHash,
			Slot:                slot,
			BlockRoot:           root,
			Stake:               stakeEach,
		}
	}
	return votes
}

func TestEndgamePipelineCreate(t *testing.T) {
	// Default config should work.
	p := NewEndgamePipeline(nil, nil, nil)
	if p == nil {
		t.Fatal("expected non-nil pipeline with default config")
	}

	// Explicit config.
	cfg := DefaultPipelineConfig()
	exec := &mockExecutionEngine{returnRoot: types.Hash{0x99}}
	verifier := &mockBLSVerifier{returnValid: true}
	p2 := NewEndgamePipeline(cfg, exec, verifier)
	if p2 == nil {
		t.Fatal("expected non-nil pipeline with explicit config")
	}

	// Invalid config: zero stake.
	badCfg := &PipelineConfig{
		FinalityThresholdNum: 2,
		FinalityThresholdDen: 3,
		TotalStake:           0,
	}
	p3 := NewEndgamePipeline(badCfg, nil, nil)
	if p3 != nil {
		t.Fatal("expected nil pipeline with zero stake config")
	}
}

func TestEndgamePipelineProcessBlock(t *testing.T) {
	root := types.Hash{0x42}
	exec := &mockExecutionEngine{returnRoot: root}
	cfg := DefaultPipelineConfig()
	cfg.TotalStake = 300 // small for testing
	p := NewEndgamePipeline(cfg, exec, nil)
	if p == nil {
		t.Fatal("expected non-nil pipeline")
	}

	block := makePipelineTestBlock(Slot(10))
	votes := makePipelineTestVotes(10, root, 10, 30) // 10*30 = 300 >= 2/3*300

	result, err := p.ProcessBlock(block, types.Hash{0x01}, votes)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Finalized {
		t.Error("expected block to be finalized")
	}
	if result.ParticipantCount != 10 {
		t.Errorf("expected 10 participants, got %d", result.ParticipantCount)
	}
	if result.LatencyMs < 0 {
		t.Error("expected non-negative latency")
	}
}

func TestEndgamePipelineProcessBlockNilInputs(t *testing.T) {
	cfg := DefaultPipelineConfig()
	cfg.TotalStake = 100
	p := NewEndgamePipeline(cfg, nil, nil)
	if p == nil {
		t.Fatal("expected non-nil pipeline")
	}

	// Nil block.
	_, err := p.ProcessBlock(nil, types.Hash{}, nil)
	if err != ErrPipelineNilBlock {
		t.Errorf("expected ErrPipelineNilBlock, got %v", err)
	}

	// Block with nil inner.
	_, err = p.ProcessBlock(&SignedBeaconBlock{}, types.Hash{}, nil)
	if err != ErrPipelineNilBlock {
		t.Errorf("expected ErrPipelineNilBlock for nil inner block, got %v", err)
	}
}

func TestEndgamePipelineAttemptFastFinality(t *testing.T) {
	cfg := DefaultPipelineConfig()
	cfg.TotalStake = 900
	p := NewEndgamePipeline(cfg, nil, nil)
	if p == nil {
		t.Fatal("expected non-nil pipeline")
	}

	root := types.Hash{0x77}

	// Not enough stake: 500/900 < 2/3.
	votes := makePipelineTestVotes(5, root, 5, 100) // 500
	ok, err := p.AttemptFastFinality(5, votes)
	if ok {
		t.Error("expected finality to fail with insufficient stake")
	}
	if err != ErrPipelineQuorumNotMet {
		t.Errorf("expected ErrPipelineQuorumNotMet, got %v", err)
	}

	// Enough stake: 700/900 >= 2/3.
	votes2 := makePipelineTestVotes(6, root, 7, 100) // 700
	ok, err = p.AttemptFastFinality(6, votes2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Error("expected finality to succeed with sufficient stake")
	}

	// Already finalized.
	_, err = p.AttemptFastFinality(6, votes2)
	if err != ErrPipelineAlreadyFinal {
		t.Errorf("expected ErrPipelineAlreadyFinal, got %v", err)
	}
}

func TestEndgamePipelineAttemptFastFinalityNoAttestations(t *testing.T) {
	cfg := DefaultPipelineConfig()
	cfg.TotalStake = 100
	p := NewEndgamePipeline(cfg, nil, nil)

	ok, err := p.AttemptFastFinality(1, nil)
	if ok {
		t.Error("expected false with no attestations")
	}
	if err != ErrPipelineNoAttestations {
		t.Errorf("expected ErrPipelineNoAttestations, got %v", err)
	}
}

func TestEndgamePipelineValidateQuorum(t *testing.T) {
	cfg := DefaultPipelineConfig()
	cfg.TotalStake = 300
	p := NewEndgamePipeline(cfg, nil, nil)

	root := types.Hash{0x01}

	// Exactly 2/3: 200*3 >= 300*2 -> 600 >= 600, true.
	votes := makePipelineTestVotes(1, root, 2, 100) // stake = 200
	if !p.ValidateQuorum(votes, 300) {
		t.Error("expected quorum met with 200/300")
	}

	// Below 2/3: 100*3 < 300*2 -> 300 < 600, false.
	votes2 := makePipelineTestVotes(1, root, 1, 100) // stake = 100
	if p.ValidateQuorum(votes2, 300) {
		t.Error("expected quorum not met with 100/300")
	}

	// Zero total stake.
	if p.ValidateQuorum(votes, 0) {
		t.Error("expected false with zero total stake")
	}

	// Empty votes.
	if p.ValidateQuorum(nil, 300) {
		t.Error("expected false with nil votes")
	}
}

func TestEndgamePipelineTimeout(t *testing.T) {
	cfg := DefaultPipelineConfig()
	cfg.TotalStake = 100
	cfg.TargetFinalityMs = 500 // 500ms - generous for test
	p := NewEndgamePipeline(cfg, nil, nil)

	root := types.Hash{0xBB}
	// Provide enough stake to pass quorum. The timeout is unlikely to
	// trigger in a fast test, but we verify the pipeline does not error.
	votes := makePipelineTestVotes(1, root, 5, 30) // 150 >= 2/3*100
	ok, err := p.AttemptFastFinality(1, votes)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Error("expected finality within timeout")
	}
}

func TestEndgamePipelineNilInputs(t *testing.T) {
	// Nil config uses defaults.
	p := NewEndgamePipeline(nil, nil, nil)
	if p == nil {
		t.Fatal("expected non-nil with nil config")
	}

	// Invalid threshold config.
	bad := &PipelineConfig{
		FinalityThresholdNum: 0,
		FinalityThresholdDen: 3,
		TotalStake:           100,
	}
	if NewEndgamePipeline(bad, nil, nil) != nil {
		t.Error("expected nil with zero threshold numerator")
	}
}

func TestEndgamePipelineIsFinalized(t *testing.T) {
	cfg := DefaultPipelineConfig()
	cfg.TotalStake = 100
	p := NewEndgamePipeline(cfg, nil, nil)

	if p.IsFinalized(42) {
		t.Error("slot 42 should not be finalized initially")
	}

	root := types.Hash{0xCC}
	votes := makePipelineTestVotes(42, root, 5, 30)
	_, _ = p.AttemptFastFinality(42, votes)

	if !p.IsFinalized(42) {
		t.Error("slot 42 should be finalized after successful attempt")
	}

	got := p.FinalizedRoot(42)
	if got != root {
		t.Errorf("expected finalized root %x, got %x", root, got)
	}
}

func TestEndgamePipelineStats(t *testing.T) {
	cfg := DefaultPipelineConfig()
	cfg.TotalStake = 100
	p := NewEndgamePipeline(cfg, nil, nil)

	root := types.Hash{0xDD}
	votes := makePipelineTestVotes(1, root, 5, 30)
	_, _ = p.AttemptFastFinality(1, votes)

	stats := p.Stats()
	if stats.TotalRuns != 1 {
		t.Errorf("expected 1 run, got %d", stats.TotalRuns)
	}
	if stats.TotalFinalized != 1 {
		t.Errorf("expected 1 finalized, got %d", stats.TotalFinalized)
	}
}
