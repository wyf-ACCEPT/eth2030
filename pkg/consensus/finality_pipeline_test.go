package consensus

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

// --- Test helpers ---

// fpMockExecutor implements FPBlockExecutor for testing.
type fpMockExecutor struct {
	mu         sync.Mutex
	execCount  int
	shouldFail bool
	stateRoot  types.Hash
}

func (m *fpMockExecutor) ExecuteBlock(slot uint64, blockRoot types.Hash) (types.Hash, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.shouldFail {
		return types.Hash{}, errors.New("execution failed")
	}
	m.execCount++
	return m.stateRoot, nil
}

// fpMockProver implements FPProofValidator for testing.
type fpMockProver struct {
	valid bool
}

func (m *fpMockProver) ValidateProof(blockRoot, stateRoot types.Hash, proofData []byte) bool {
	return m.valid
}

// fpTestBLSBackend is a mock BLS backend that accepts all signatures where
// SigningData is non-empty. This allows tests to focus on the pipeline logic
// without requiring real BLS pairing math.
type fpTestBLSBackend struct{}

func (b *fpTestBLSBackend) Name() string { return "test-mock" }

func (b *fpTestBLSBackend) Verify(pubkey, msg, sig []byte) bool {
	return len(pubkey) == crypto.BLSPubkeySize && len(sig) == crypto.BLSSignatureSize && len(msg) > 0
}

func (b *fpTestBLSBackend) AggregateVerify(pubkeys, msgs [][]byte, sig []byte) bool {
	return len(pubkeys) > 0 && len(sig) == crypto.BLSSignatureSize
}

func (b *fpTestBLSBackend) FastAggregateVerify(pubkeys [][]byte, msg, sig []byte) bool {
	return len(pubkeys) > 0 && len(sig) == crypto.BLSSignatureSize
}

// fpSetupPipeline creates a FinalityPipeline with the given config.
func fpSetupPipeline(t *testing.T, numValidators int, threshold float64) *FinalityPipeline {
	t.Helper()

	config := DefaultFPConfig()
	config.TargetFinalityMs = 500

	engineConfig := DefaultEndgameEngineConfig()
	engineConfig.FinalityThreshold = threshold
	engine := NewEndgameEngine(engineConfig)

	weights := make(map[uint64]uint64, numValidators)
	for i := 0; i < numValidators; i++ {
		weights[uint64(i)] = 100
	}
	engine.SetValidatorSet(weights)

	executor := &fpMockExecutor{
		stateRoot: types.BytesToHash([]byte{0xAA, 0xBB}),
	}

	prover := &fpMockProver{valid: true}
	backend := &fpTestBLSBackend{}

	pipeline, err := NewFinalityPipeline(config, engine, backend, executor, prover)
	if err != nil {
		t.Fatalf("NewFinalityPipeline: %v", err)
	}

	return pipeline
}

// fpMakeVote creates an FPVote with a deterministic mock signature.
func fpMakeVote(slot uint64, validatorIdx int, blockHash types.Hash) *FPVote {
	var pk [48]byte
	pk[0] = 0x80 // compression flag
	pk[1] = byte(validatorIdx)

	var sig [96]byte
	sig[0] = 0x80 // compression flag
	sig[1] = byte(validatorIdx)
	sig[2] = byte(slot)

	return &FPVote{
		Slot:           slot,
		ValidatorIndex: uint64(validatorIdx),
		BlockHash:      blockHash,
		Weight:         100,
		Pubkey:         pk,
		Signature:      sig,
		SigningData:    []byte("finality-vote-digest"),
	}
}

// --- Tests ---

func TestFPCreation(t *testing.T) {
	backend := &fpTestBLSBackend{}
	engine := NewEndgameEngine(DefaultEndgameEngineConfig())
	executor := &fpMockExecutor{}

	p, err := NewFinalityPipeline(DefaultFPConfig(), engine, backend, executor, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p == nil {
		t.Fatal("expected non-nil pipeline")
	}

	_, err = NewFinalityPipeline(nil, engine, backend, executor, nil)
	if !errors.Is(err, ErrFPNilConfig) {
		t.Fatalf("expected ErrFPNilConfig, got %v", err)
	}

	_, err = NewFinalityPipeline(DefaultFPConfig(), nil, backend, executor, nil)
	if !errors.Is(err, ErrFPNilEngine) {
		t.Fatalf("expected ErrFPNilEngine, got %v", err)
	}

	_, err = NewFinalityPipeline(DefaultFPConfig(), engine, nil, executor, nil)
	if !errors.Is(err, ErrFPNilBLS) {
		t.Fatalf("expected ErrFPNilBLS, got %v", err)
	}

	_, err = NewFinalityPipeline(DefaultFPConfig(), engine, backend, nil, nil)
	if !errors.Is(err, ErrFPNilExecutor) {
		t.Fatalf("expected ErrFPNilExecutor, got %v", err)
	}
}

func TestFPConfigDefaults(t *testing.T) {
	cfg := DefaultFPConfig()
	if cfg.TargetFinalityMs != 500 {
		t.Errorf("expected TargetFinalityMs=500, got %d", cfg.TargetFinalityMs)
	}
	if !cfg.RequireProofOnSlowPath {
		t.Error("expected RequireProofOnSlowPath=true")
	}
	if cfg.SkipExecution {
		t.Error("expected SkipExecution=false")
	}
	if cfg.MaxConcurrentSlots != 64 {
		t.Errorf("expected MaxConcurrentSlots=64, got %d", cfg.MaxConcurrentSlots)
	}
}

func TestFPVoteRecording(t *testing.T) {
	pipeline := fpSetupPipeline(t, 10, 0.90)
	blockHash := types.BytesToHash([]byte{0x01, 0x02})

	vote := fpMakeVote(1, 0, blockHash)
	_, err := pipeline.SubmitVote(vote)
	if err != nil {
		t.Fatalf("SubmitVote: %v", err)
	}

	if pipeline.GetFPSlotVoteCount(1) != 1 {
		t.Errorf("expected 1 vote, got %d", pipeline.GetFPSlotVoteCount(1))
	}
}

func TestFPThresholdDetection(t *testing.T) {
	// Use 10 validators, threshold 0.8 => needs 800/1000 = 8 votes.
	pipeline := fpSetupPipeline(t, 10, 0.80)
	blockHash := types.BytesToHash([]byte{0x01, 0x02})

	// Submit 7 votes (7/10 = 70%, below 80% threshold).
	for i := 0; i < 7; i++ {
		vote := fpMakeVote(1, i, blockHash)
		_, err := pipeline.SubmitVote(vote)
		if err != nil {
			t.Fatalf("vote %d: %v", i, err)
		}
	}

	if pipeline.IsFPSlotFinalized(1) {
		t.Error("slot should not be finalized with 7/10 votes at 80% threshold")
	}

	// 8th vote crosses threshold (8/10 = 80%).
	vote := fpMakeVote(1, 7, blockHash)
	result, err := pipeline.SubmitVote(vote)
	if err != nil {
		t.Fatalf("vote 8: %v", err)
	}
	if result == nil {
		t.Fatal("expected finality result after 8th vote")
	}
	if !result.ExecutionValid {
		t.Error("execution should be valid")
	}
	if result.BlockRoot != blockHash {
		t.Errorf("expected block root %v, got %v", blockHash, result.BlockRoot)
	}
}

func TestFPBLSVerificationAccept(t *testing.T) {
	pipeline := fpSetupPipeline(t, 3, 0.667)
	blockHash := types.BytesToHash([]byte{0x03})

	vote := fpMakeVote(1, 0, blockHash)
	_, err := pipeline.SubmitVote(vote)
	if err != nil {
		t.Fatalf("valid vote failed: %v", err)
	}
}

func TestFPBLSVerificationReject(t *testing.T) {
	pipeline := fpSetupPipeline(t, 3, 0.667)
	blockHash := types.BytesToHash([]byte{0x03})

	// Empty signing data should cause BLS rejection.
	badVote := &FPVote{
		Slot:           1,
		ValidatorIndex: 0,
		BlockHash:      blockHash,
		Weight:         100,
		SigningData:    nil,
	}
	_, err := pipeline.SubmitVote(badVote)
	if !errors.Is(err, ErrFPBLSFailed) {
		t.Fatalf("expected ErrFPBLSFailed, got %v", err)
	}
}

func TestFPBlockExecution(t *testing.T) {
	// 3 validators, threshold 0.667 => threshold=200, so 2 votes finalize.
	pipeline := fpSetupPipeline(t, 3, 0.667)
	blockHash := types.BytesToHash([]byte{0x04})

	// First vote: no finality.
	vote0 := fpMakeVote(1, 0, blockHash)
	result0, err := pipeline.SubmitVote(vote0)
	if err != nil {
		t.Fatalf("vote 0: %v", err)
	}
	if result0 != nil {
		t.Fatal("should not finalize after 1 vote")
	}

	// Second vote: finality reached.
	vote1 := fpMakeVote(1, 1, blockHash)
	result1, err := pipeline.SubmitVote(vote1)
	if err != nil {
		t.Fatalf("vote 1: %v", err)
	}
	if result1 == nil {
		t.Fatal("expected result after 2nd vote")
	}
	if !result1.ExecutionValid {
		t.Error("execution should be valid")
	}

	if pipeline.FPFinalizedCount() != 1 {
		t.Errorf("expected 1 finalized, got %d", pipeline.FPFinalizedCount())
	}
}

func TestFPExecutionFailure(t *testing.T) {
	config := DefaultFPConfig()
	engineConfig := DefaultEndgameEngineConfig()
	engine := NewEndgameEngine(engineConfig)
	weights := map[uint64]uint64{0: 100, 1: 100, 2: 100}
	engine.SetValidatorSet(weights)

	executor := &fpMockExecutor{shouldFail: true}
	backend := &fpTestBLSBackend{}

	pipeline, _ := NewFinalityPipeline(config, engine, backend, executor, nil)

	blockHash := types.BytesToHash([]byte{0x05})

	// First vote ok, second triggers finality -> exec failure.
	vote0 := fpMakeVote(1, 0, blockHash)
	_, err := pipeline.SubmitVote(vote0)
	if err != nil {
		t.Fatalf("vote 0: %v", err)
	}

	vote1 := fpMakeVote(1, 1, blockHash)
	_, err = pipeline.SubmitVote(vote1)
	if !errors.Is(err, ErrFPExecFailed) {
		t.Fatalf("expected ErrFPExecFailed, got %v", err)
	}
}

func TestFPFastPathVsSlowPath(t *testing.T) {
	pipeline := fpSetupPipeline(t, 3, 0.667)
	blockHash := types.BytesToHash([]byte{0x06})

	// Fast path: votes submitted quickly (< 500ms).
	var finalResult *FPFinalityResult
	for i := 0; i < 3; i++ {
		vote := fpMakeVote(1, i, blockHash)
		result, err := pipeline.SubmitVote(vote)
		if err != nil && !errors.Is(err, ErrFPSlotFinalized) {
			t.Fatalf("vote %d: %v", i, err)
		}
		if result != nil && finalResult == nil {
			finalResult = result
		}
	}
	if finalResult == nil {
		t.Fatal("expected finality result")
	}
	if !finalResult.FastPath {
		t.Error("expected fast path for quick votes")
	}
}

func TestFPTimingMetrics(t *testing.T) {
	pipeline := fpSetupPipeline(t, 3, 0.667)
	blockHash := types.BytesToHash([]byte{0x07})

	for i := 0; i < 3; i++ {
		vote := fpMakeVote(1, i, blockHash)
		pipeline.SubmitVote(vote)
	}

	metrics := pipeline.GetFPMetrics(1)
	if metrics == nil {
		t.Fatal("expected non-nil metrics for finalized slot")
	}
	if metrics.TotalLatencyMs < 0 {
		t.Errorf("total latency should be >= 0, got %d", metrics.TotalLatencyMs)
	}
}

func TestFPConcurrentVoteSubmission(t *testing.T) {
	pipeline := fpSetupPipeline(t, 12, 0.667)
	blockHash := types.BytesToHash([]byte{0x08})

	var wg sync.WaitGroup
	var finalizedCount atomic.Int32

	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			vote := fpMakeVote(1, idx, blockHash)
			result, err := pipeline.SubmitVote(vote)
			if err != nil && !errors.Is(err, ErrDuplicateVote) && !errors.Is(err, ErrFPSlotFinalized) {
				return
			}
			if result != nil {
				finalizedCount.Add(1)
			}
		}(i)
	}
	wg.Wait()

	if !pipeline.IsFPSlotFinalized(1) {
		t.Error("slot 1 should be finalized after concurrent votes")
	}
}

func TestFPInvalidVoteRejection(t *testing.T) {
	pipeline := fpSetupPipeline(t, 3, 0.667)

	// Nil vote.
	_, err := pipeline.SubmitVote(nil)
	if !errors.Is(err, ErrFPNilVote) {
		t.Fatalf("expected ErrFPNilVote, got %v", err)
	}

	// Zero block root.
	_, err = pipeline.SubmitVote(&FPVote{Slot: 1, Weight: 100})
	if !errors.Is(err, ErrFPNoBlockRoot) {
		t.Fatalf("expected ErrFPNoBlockRoot, got %v", err)
	}

	// Zero weight.
	_, err = pipeline.SubmitVote(&FPVote{
		Slot:      1,
		Weight:    0,
		BlockHash: types.BytesToHash([]byte{0x01}),
	})
	if !errors.Is(err, ErrFPInvalidVote) {
		t.Fatalf("expected ErrFPInvalidVote, got %v", err)
	}

	// Empty signing data => BLS fails.
	_, err = pipeline.SubmitVote(&FPVote{
		Slot:      1,
		Weight:    100,
		BlockHash: types.BytesToHash([]byte{0x01}),
	})
	if !errors.Is(err, ErrFPBLSFailed) {
		t.Fatalf("expected ErrFPBLSFailed, got %v", err)
	}
}

func TestFPMultipleSlotFinality(t *testing.T) {
	// 3 validators, 0.667 threshold = 200, so 2 votes finalize.
	pipeline := fpSetupPipeline(t, 3, 0.667)

	for slot := uint64(1); slot <= 5; slot++ {
		blockHash := types.BytesToHash([]byte{byte(slot)})
		for i := 0; i < 3; i++ {
			vote := fpMakeVote(slot, i, blockHash)
			pipeline.SubmitVote(vote)
		}
	}

	if pipeline.FPFinalizedCount() != 5 {
		t.Errorf("expected 5 finalized slots, got %d", pipeline.FPFinalizedCount())
	}

	for slot := uint64(1); slot <= 5; slot++ {
		if !pipeline.IsFPSlotFinalized(slot) {
			t.Errorf("slot %d should be finalized", slot)
		}
		result := pipeline.GetFPResult(slot)
		if result == nil {
			t.Errorf("slot %d: nil result", slot)
			continue
		}
		expected := types.BytesToHash([]byte{byte(slot)})
		if result.BlockRoot != expected {
			t.Errorf("slot %d: wrong block root", slot)
		}
	}
}

func TestFPEIP8025ProofValidation(t *testing.T) {
	config := DefaultFPConfig()
	config.RequireProofOnSlowPath = true
	config.TargetFinalityMs = 0 // force slow path

	engineConfig := DefaultEndgameEngineConfig()
	engine := NewEndgameEngine(engineConfig)
	weights := map[uint64]uint64{0: 100, 1: 100, 2: 100}
	engine.SetValidatorSet(weights)

	executor := &fpMockExecutor{stateRoot: types.BytesToHash([]byte{0xCC})}
	prover := &fpMockProver{valid: true}
	backend := &fpTestBLSBackend{}

	pipeline, _ := NewFinalityPipeline(config, engine, backend, executor, prover)

	blockHash := types.BytesToHash([]byte{0x09})
	// With 3 validators and 0.667 threshold, 2 votes finalize.
	vote0 := fpMakeVote(1, 0, blockHash)
	pipeline.SubmitVote(vote0)

	vote1 := fpMakeVote(1, 1, blockHash)
	result, err := pipeline.SubmitVote(vote1)
	if err != nil {
		t.Fatalf("finalizing vote error: %v", err)
	}
	if result == nil {
		t.Fatal("expected result")
	}
	if !result.ProofValid {
		t.Error("proof should be valid")
	}
}

func TestFPEIP8025ProofFailure(t *testing.T) {
	config := DefaultFPConfig()
	config.RequireProofOnSlowPath = true
	config.TargetFinalityMs = 0 // force slow path

	engineConfig := DefaultEndgameEngineConfig()
	engine := NewEndgameEngine(engineConfig)
	weights := map[uint64]uint64{0: 100, 1: 100, 2: 100}
	engine.SetValidatorSet(weights)

	executor := &fpMockExecutor{stateRoot: types.BytesToHash([]byte{0xDD})}
	prover := &fpMockProver{valid: false}
	backend := &fpTestBLSBackend{}

	pipeline, _ := NewFinalityPipeline(config, engine, backend, executor, prover)

	blockHash := types.BytesToHash([]byte{0x0A})
	vote0 := fpMakeVote(1, 0, blockHash)
	pipeline.SubmitVote(vote0)

	vote1 := fpMakeVote(1, 1, blockHash)
	_, err := pipeline.SubmitVote(vote1)
	if !errors.Is(err, ErrFPProofFailed) {
		t.Fatalf("expected ErrFPProofFailed, got %v", err)
	}
}

func TestFPDuplicateVote(t *testing.T) {
	pipeline := fpSetupPipeline(t, 10, 0.90)
	blockHash := types.BytesToHash([]byte{0x0B})

	vote := fpMakeVote(1, 0, blockHash)
	_, err := pipeline.SubmitVote(vote)
	if err != nil {
		t.Fatalf("first vote: %v", err)
	}

	_, err = pipeline.SubmitVote(vote)
	if !errors.Is(err, ErrDuplicateVote) {
		t.Fatalf("expected ErrDuplicateVote, got %v", err)
	}
}

func TestFPStopPreventsVotes(t *testing.T) {
	pipeline := fpSetupPipeline(t, 3, 0.667)
	pipeline.FPStop()

	vote := fpMakeVote(1, 0, types.BytesToHash([]byte{0x0C}))
	_, err := pipeline.SubmitVote(vote)
	if !errors.Is(err, ErrFPStopped) {
		t.Fatalf("expected ErrFPStopped, got %v", err)
	}
}

func TestFPOnFinalityCallback(t *testing.T) {
	pipeline := fpSetupPipeline(t, 3, 0.667)
	blockHash := types.BytesToHash([]byte{0x0D})

	var callbackResult *FPFinalityResult
	pipeline.SetOnFinality(func(r *FPFinalityResult) {
		callbackResult = r
	})

	for i := 0; i < 3; i++ {
		vote := fpMakeVote(1, i, blockHash)
		pipeline.SubmitVote(vote)
	}

	if callbackResult == nil {
		t.Fatal("callback was not invoked")
	}
	if callbackResult.Slot != 1 {
		t.Errorf("expected slot 1, got %d", callbackResult.Slot)
	}
}

func TestFPBLSBackendName(t *testing.T) {
	pipeline := fpSetupPipeline(t, 1, 0.667)
	name := pipeline.FPBLSBackendName()
	if name != "test-mock" {
		t.Errorf("expected 'test-mock', got %q", name)
	}
}

func TestFPPruneOldSlots(t *testing.T) {
	pipeline := fpSetupPipeline(t, 3, 0.667)

	for slot := uint64(1); slot <= 10; slot++ {
		blockHash := types.BytesToHash([]byte{byte(slot)})
		for i := 0; i < 3; i++ {
			vote := fpMakeVote(slot, i, blockHash)
			pipeline.SubmitVote(vote)
		}
	}

	pruned := pipeline.FPPruneOldSlots(6)
	if pruned != 5 {
		t.Errorf("expected 5 pruned, got %d", pruned)
	}

	for slot := uint64(1); slot <= 5; slot++ {
		if pipeline.GetFPResult(slot) != nil {
			t.Errorf("slot %d should be pruned", slot)
		}
	}
	for slot := uint64(6); slot <= 10; slot++ {
		if pipeline.GetFPResult(slot) == nil {
			t.Errorf("slot %d should still exist", slot)
		}
	}
}

func TestFPSkipExecution(t *testing.T) {
	config := DefaultFPConfig()
	config.SkipExecution = true

	engineConfig := DefaultEndgameEngineConfig()
	engine := NewEndgameEngine(engineConfig)
	weights := map[uint64]uint64{0: 100, 1: 100, 2: 100}
	engine.SetValidatorSet(weights)

	executor := &fpMockExecutor{shouldFail: true}
	backend := &fpTestBLSBackend{}

	pipeline, _ := NewFinalityPipeline(config, engine, backend, executor, nil)

	blockHash := types.BytesToHash([]byte{0x0E})
	// 2 votes finalize with default 0.667 threshold.
	vote0 := fpMakeVote(1, 0, blockHash)
	_, err := pipeline.SubmitVote(vote0)
	if err != nil {
		t.Fatalf("vote 0: %v", err)
	}

	vote1 := fpMakeVote(1, 1, blockHash)
	result, err := pipeline.SubmitVote(vote1)
	if err != nil {
		t.Fatalf("vote 1: %v", err)
	}
	if result == nil {
		t.Fatal("expected result")
	}
	if !result.ExecutionValid {
		t.Error("execution should be marked valid when skipped")
	}
}

func TestFPVoteBatch(t *testing.T) {
	pipeline := fpSetupPipeline(t, 3, 0.667)
	blockHash := types.BytesToHash([]byte{0x0F})

	votes := make([]*FPVote, 3)
	for i := 0; i < 3; i++ {
		votes[i] = fpMakeVote(1, i, blockHash)
	}

	result, err := pipeline.SubmitFPVoteBatch(votes)
	if err != nil && !errors.Is(err, ErrFPSlotFinalized) {
		t.Fatalf("batch: %v", err)
	}
	if result == nil {
		t.Fatal("expected finality result from batch")
	}
	if result.Slot != 1 {
		t.Errorf("expected slot 1, got %d", result.Slot)
	}
}

func TestFPResultFields(t *testing.T) {
	// Use 3 validators, 0.667 threshold => finalize on vote 2.
	pipeline := fpSetupPipeline(t, 3, 0.667)
	blockHash := types.BytesToHash([]byte{0x10})

	for i := 0; i < 2; i++ {
		vote := fpMakeVote(1, i, blockHash)
		pipeline.SubmitVote(vote)
	}

	result := pipeline.GetFPResult(1)
	if result == nil {
		t.Fatal("expected result")
	}
	if result.Slot != 1 {
		t.Errorf("slot: expected 1, got %d", result.Slot)
	}
	if result.BlockRoot != blockHash {
		t.Errorf("block root mismatch")
	}
	if result.FinalizedAt.IsZero() {
		t.Error("FinalizedAt should be set")
	}
	if result.VoteCount != 2 {
		t.Errorf("expected 2 votes, got %d", result.VoteCount)
	}
	if result.TotalWeight != 200 {
		t.Errorf("expected total weight 200, got %d", result.TotalWeight)
	}
}

func TestFPSlotAlreadyFinalized(t *testing.T) {
	// 4 validators, 0.667 => threshold=268, 3 votes (300) finalize.
	pipeline := fpSetupPipeline(t, 4, 0.667)
	blockHash := types.BytesToHash([]byte{0x11})

	// Submit 3 votes to finalize.
	for i := 0; i < 3; i++ {
		vote := fpMakeVote(1, i, blockHash)
		pipeline.SubmitVote(vote)
	}

	// Submit 4th vote after finality.
	vote := fpMakeVote(1, 3, blockHash)
	result, err := pipeline.SubmitVote(vote)
	if !errors.Is(err, ErrFPSlotFinalized) {
		t.Fatalf("expected ErrFPSlotFinalized, got %v", err)
	}
	if result == nil {
		t.Fatal("should return existing result even with error")
	}
}
