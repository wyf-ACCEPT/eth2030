package consensus

import (
	"math/big"
	"sync"
	"testing"
	"time"
)

// eth32 returns 32 ETH in wei for test builder registrations.
func eth32() *big.Int {
	return new(big.Int).Mul(big.NewInt(32), new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil))
}

func coordPubkey(b byte) [48]byte {
	var pk [48]byte
	pk[0] = b
	pk[1] = 0xFF
	return pk
}

func coordReg(id string, b byte) *BuilderRegistration {
	return &BuilderRegistration{
		ID:           id,
		Pubkey:       coordPubkey(b),
		Stake:        eth32(),
		Capabilities: CapTransactions,
		MaxFragments: 4,
	}
}

func TestDefaultCoordinatorConfig(t *testing.T) {
	cfg := DefaultCoordinatorConfig()
	if cfg.MaxBuilders != 32 {
		t.Errorf("MaxBuilders = %d, want 32", cfg.MaxBuilders)
	}
	if cfg.GasLimit != 30_000_000 {
		t.Errorf("GasLimit = %d, want 30000000", cfg.GasLimit)
	}
	if cfg.DefaultReputation != 0.5 {
		t.Errorf("DefaultReputation = %f, want 0.5", cfg.DefaultReputation)
	}
}

func TestNewDistCoordinator_NilConfig(t *testing.T) {
	dc := NewDistCoordinator(nil)
	if dc.config.MaxBuilders != 32 {
		t.Error("nil config should use defaults")
	}
}

func TestRegisterBuilder_Basic(t *testing.T) {
	dc := NewDistCoordinator(nil)
	reg := coordReg("alice", 1)

	if err := dc.RegisterBuilder(reg); err != nil {
		t.Fatalf("RegisterBuilder: %v", err)
	}
	if dc.BuilderCount() != 1 {
		t.Errorf("BuilderCount = %d, want 1", dc.BuilderCount())
	}

	got := dc.GetBuilder("alice")
	if got == nil {
		t.Fatal("GetBuilder returned nil")
	}
	if got.ID != "alice" {
		t.Errorf("ID = %s, want alice", got.ID)
	}
	if got.Reputation != 0.5 {
		t.Errorf("Reputation = %f, want 0.5", got.Reputation)
	}
}

func TestRegisterBuilder_Duplicate(t *testing.T) {
	dc := NewDistCoordinator(nil)
	dc.RegisterBuilder(coordReg("alice", 1))

	err := dc.RegisterBuilder(coordReg("alice", 2))
	if err != ErrCoordBuilderExists {
		t.Errorf("duplicate: got %v, want ErrCoordBuilderExists", err)
	}
}

func TestRegisterBuilder_InvalidPubkey(t *testing.T) {
	dc := NewDistCoordinator(nil)
	reg := coordReg("bob", 0)
	reg.Pubkey = [48]byte{} // all zeros
	if err := dc.RegisterBuilder(reg); err != ErrCoordInvalidPubkey {
		t.Errorf("zero pubkey: got %v, want ErrCoordInvalidPubkey", err)
	}
}

func TestRegisterBuilder_NilReg(t *testing.T) {
	dc := NewDistCoordinator(nil)
	if err := dc.RegisterBuilder(nil); err != ErrCoordInvalidPubkey {
		t.Errorf("nil reg: got %v, want ErrCoordInvalidPubkey", err)
	}
}

func TestRegisterBuilder_EmptyID(t *testing.T) {
	dc := NewDistCoordinator(nil)
	reg := coordReg("", 1)
	reg.ID = ""
	if err := dc.RegisterBuilder(reg); err != ErrCoordInvalidPubkey {
		t.Errorf("empty ID: got %v, want ErrCoordInvalidPubkey", err)
	}
}

func TestRegisterBuilder_InsufficientStake(t *testing.T) {
	dc := NewDistCoordinator(nil)
	reg := coordReg("underfunded", 1)
	reg.Stake = big.NewInt(1) // 1 wei, way below 32 ETH
	if err := dc.RegisterBuilder(reg); err != ErrCoordInsufficientStake {
		t.Errorf("low stake: got %v, want ErrCoordInsufficientStake", err)
	}
}

func TestRegisterBuilder_MaxBuilders(t *testing.T) {
	cfg := DefaultCoordinatorConfig()
	cfg.MaxBuilders = 2
	dc := NewDistCoordinator(cfg)

	dc.RegisterBuilder(coordReg("a", 1))
	dc.RegisterBuilder(coordReg("b", 2))

	err := dc.RegisterBuilder(coordReg("c", 3))
	if err != ErrCoordMaxBuilders {
		t.Errorf("max builders: got %v, want ErrCoordMaxBuilders", err)
	}
}

func TestUnregisterBuilder(t *testing.T) {
	dc := NewDistCoordinator(nil)
	dc.RegisterBuilder(coordReg("alice", 1))

	if err := dc.UnregisterBuilder("alice"); err != nil {
		t.Fatalf("UnregisterBuilder: %v", err)
	}
	if dc.BuilderCount() != 0 {
		t.Error("builder should be removed")
	}
}

func TestUnregisterBuilder_NotFound(t *testing.T) {
	dc := NewDistCoordinator(nil)
	if err := dc.UnregisterBuilder("ghost"); err != ErrCoordBuilderNotFound {
		t.Errorf("not found: got %v, want ErrCoordBuilderNotFound", err)
	}
}

func TestGetBuilder_NotFound(t *testing.T) {
	dc := NewDistCoordinator(nil)
	if dc.GetBuilder("ghost") != nil {
		t.Error("expected nil for unknown builder")
	}
}

func TestStartRound_Basic(t *testing.T) {
	dc := NewDistCoordinator(nil)
	if err := dc.StartRound(Slot(100)); err != nil {
		t.Fatalf("StartRound: %v", err)
	}
	if !dc.HasActiveRound() {
		t.Error("expected active round")
	}
	if dc.CurrentSlot() != 100 {
		t.Errorf("CurrentSlot = %d, want 100", dc.CurrentSlot())
	}
}

func TestStartRound_InvalidSlot(t *testing.T) {
	dc := NewDistCoordinator(nil)
	if err := dc.StartRound(0); err != ErrCoordInvalidSlot {
		t.Errorf("slot 0: got %v, want ErrCoordInvalidSlot", err)
	}
}

func TestStartRound_AlreadyActive(t *testing.T) {
	dc := NewDistCoordinator(nil)
	dc.StartRound(1)
	if err := dc.StartRound(2); err != ErrCoordRoundActive {
		t.Errorf("double start: got %v, want ErrCoordRoundActive", err)
	}
}

func TestCoordSubmitFragment_Basic(t *testing.T) {
	cfg := DefaultCoordinatorConfig()
	cfg.RoundTimeout = 10 * time.Second
	dc := NewDistCoordinator(cfg)
	dc.RegisterBuilder(coordReg("alice", 1))
	dc.StartRound(1)

	frag := makeFrag("alice", 3, 21000, 5)
	if err := dc.SubmitFragment("alice", frag); err != nil {
		t.Fatalf("SubmitFragment: %v", err)
	}
	if dc.FragmentCount() != 1 {
		t.Errorf("FragmentCount = %d, want 1", dc.FragmentCount())
	}
}

func TestSubmitFragment_NilFragment(t *testing.T) {
	dc := NewDistCoordinator(nil)
	dc.RegisterBuilder(coordReg("alice", 1))
	dc.StartRound(1)

	if err := dc.SubmitFragment("alice", nil); err != ErrCoordNilFragment {
		t.Errorf("nil frag: got %v, want ErrCoordNilFragment", err)
	}
}

func TestSubmitFragment_EmptyTxList(t *testing.T) {
	dc := NewDistCoordinator(nil)
	dc.RegisterBuilder(coordReg("alice", 1))
	dc.StartRound(1)

	frag := &BlockFragment{BuilderID: "alice", TxList: [][]byte{}, GasUsed: 21000}
	if err := dc.SubmitFragment("alice", frag); err != ErrCoordEmptyFragment {
		t.Errorf("empty frag: got %v, want ErrCoordEmptyFragment", err)
	}
}

func TestSubmitFragment_NoActiveRound(t *testing.T) {
	dc := NewDistCoordinator(nil)
	dc.RegisterBuilder(coordReg("alice", 1))

	frag := makeFrag("alice", 1, 21000, 1)
	if err := dc.SubmitFragment("alice", frag); err != ErrCoordNoActiveRound {
		t.Errorf("no round: got %v, want ErrCoordNoActiveRound", err)
	}
}

func TestSubmitFragment_UnregisteredBuilder(t *testing.T) {
	dc := NewDistCoordinator(nil)
	dc.StartRound(1)

	frag := makeFrag("ghost", 1, 21000, 1)
	if err := dc.SubmitFragment("ghost", frag); err != ErrCoordBuilderNotFound {
		t.Errorf("unregistered: got %v, want ErrCoordBuilderNotFound", err)
	}
}

func TestSubmitFragment_BuilderFragmentLimit(t *testing.T) {
	cfg := DefaultCoordinatorConfig()
	cfg.RoundTimeout = 10 * time.Second
	dc := NewDistCoordinator(cfg)

	reg := coordReg("alice", 1)
	reg.MaxFragments = 2
	dc.RegisterBuilder(reg)
	dc.StartRound(1)

	dc.SubmitFragment("alice", makeFrag("alice", 1, 21000, 1))
	dc.SubmitFragment("alice", makeFrag("alice", 1, 21000, 2))

	err := dc.SubmitFragment("alice", makeFrag("alice", 1, 21000, 3))
	if err != ErrCoordFragmentLimit {
		t.Errorf("frag limit: got %v, want ErrCoordFragmentLimit", err)
	}
}

func TestSubmitFragment_GasConflict(t *testing.T) {
	cfg := DefaultCoordinatorConfig()
	cfg.GasLimit = 50_000
	cfg.RoundTimeout = 10 * time.Second
	dc := NewDistCoordinator(cfg)
	dc.RegisterBuilder(coordReg("alice", 1))
	dc.StartRound(1)

	dc.SubmitFragment("alice", makeFrag("alice", 1, 30_000, 1))

	// This would push total to 60k > 50k limit.
	err := dc.SubmitFragment("alice", makeFrag("alice", 1, 30_000, 2))
	if err != ErrCoordGasConflict {
		t.Errorf("gas conflict: got %v, want ErrCoordGasConflict", err)
	}
}

func TestSubmitFragment_DeadlinePassed(t *testing.T) {
	cfg := DefaultCoordinatorConfig()
	cfg.RoundTimeout = 1 * time.Millisecond
	dc := NewDistCoordinator(cfg)
	dc.RegisterBuilder(coordReg("alice", 1))
	dc.StartRound(1)

	time.Sleep(5 * time.Millisecond)

	err := dc.SubmitFragment("alice", makeFrag("alice", 1, 21000, 1))
	if err != ErrCoordDeadlinePassed {
		t.Errorf("deadline: got %v, want ErrCoordDeadlinePassed", err)
	}
}

func TestAssembleBlock_Basic(t *testing.T) {
	cfg := DefaultCoordinatorConfig()
	cfg.GasLimit = 200_000
	cfg.RoundTimeout = 10 * time.Second
	dc := NewDistCoordinator(cfg)
	dc.RegisterBuilder(coordReg("alice", 1))
	dc.RegisterBuilder(coordReg("bob", 2))
	dc.StartRound(Slot(42))

	dc.SubmitFragment("alice", makeFrag("alice", 2, 30_000, 10))
	dc.SubmitFragment("bob", makeFrag("bob", 3, 40_000, 5))

	assembled, err := dc.AssembleBlock()
	if err != nil {
		t.Fatalf("AssembleBlock: %v", err)
	}
	if assembled.Slot != 42 {
		t.Errorf("Slot = %d, want 42", assembled.Slot)
	}
	if assembled.TotalGas != 70_000 {
		t.Errorf("TotalGas = %d, want 70000", assembled.TotalGas)
	}
	if assembled.TotalTxs != 5 {
		t.Errorf("TotalTxs = %d, want 5", assembled.TotalTxs)
	}
	if len(assembled.BuilderIDs) != 2 {
		t.Errorf("BuilderIDs = %d, want 2", len(assembled.BuilderIDs))
	}
	// Higher-priority fragment (alice, prio 10) should come first by score.
	if assembled.Fragments[0].BuilderID != "alice" {
		t.Errorf("first fragment builder = %s, want alice", assembled.Fragments[0].BuilderID)
	}
}

func TestAssembleBlock_NoActiveRound(t *testing.T) {
	dc := NewDistCoordinator(nil)
	_, err := dc.AssembleBlock()
	if err != ErrCoordNoActiveRound {
		t.Errorf("no round: got %v, want ErrCoordNoActiveRound", err)
	}
}

func TestAssembleBlock_NoFragments(t *testing.T) {
	dc := NewDistCoordinator(nil)
	dc.StartRound(1)
	_, err := dc.AssembleBlock()
	if err != ErrCoordNoFragments {
		t.Errorf("no frags: got %v, want ErrCoordNoFragments", err)
	}
}

func TestAssembleBlock_GasLimitPacking(t *testing.T) {
	cfg := DefaultCoordinatorConfig()
	cfg.GasLimit = 50_000
	cfg.RoundTimeout = 10 * time.Second
	dc := NewDistCoordinator(cfg)
	dc.RegisterBuilder(coordReg("alice", 1))
	dc.StartRound(1)

	// Submit two fragments totaling 42k (within 50k limit).
	dc.SubmitFragment("alice", makeFrag("alice", 1, 21_000, 10))
	dc.SubmitFragment("alice", makeFrag("alice", 1, 21_000, 5))

	assembled, err := dc.AssembleBlock()
	if err != nil {
		t.Fatalf("AssembleBlock: %v", err)
	}
	if assembled.TotalGas != 42_000 {
		t.Errorf("TotalGas = %d, want 42000", assembled.TotalGas)
	}
}

func TestFinalizeRound_Basic(t *testing.T) {
	cfg := DefaultCoordinatorConfig()
	cfg.RoundTimeout = 10 * time.Second
	dc := NewDistCoordinator(cfg)
	dc.RegisterBuilder(coordReg("alice", 1))
	dc.StartRound(1)
	dc.SubmitFragment("alice", makeFrag("alice", 2, 21_000, 5))

	result, err := dc.FinalizeRound()
	if err != nil {
		t.Fatalf("FinalizeRound: %v", err)
	}
	if result == nil {
		t.Fatal("expected assembled block")
	}
	if dc.HasActiveRound() {
		t.Error("round should be finalized (not active)")
	}
}

func TestFinalizeRound_AlreadyFinalized(t *testing.T) {
	cfg := DefaultCoordinatorConfig()
	cfg.RoundTimeout = 10 * time.Second
	dc := NewDistCoordinator(cfg)
	dc.RegisterBuilder(coordReg("alice", 1))
	dc.StartRound(1)
	dc.SubmitFragment("alice", makeFrag("alice", 1, 21_000, 1))
	dc.FinalizeRound()

	_, err := dc.FinalizeRound()
	if err != ErrCoordRoundFinalized {
		t.Errorf("double finalize: got %v, want ErrCoordRoundFinalized", err)
	}
}

func TestFinalizeRound_NoRound(t *testing.T) {
	dc := NewDistCoordinator(nil)
	_, err := dc.FinalizeRound()
	if err != ErrCoordNoActiveRound {
		t.Errorf("no round: got %v, want ErrCoordNoActiveRound", err)
	}
}

func TestFinalizeRound_NoFragments(t *testing.T) {
	dc := NewDistCoordinator(nil)
	dc.StartRound(1)
	_, err := dc.FinalizeRound()
	if err != ErrCoordNoFragments {
		t.Errorf("no frags: got %v, want ErrCoordNoFragments", err)
	}
}

func TestFinalizeRound_UpdatesReputation(t *testing.T) {
	cfg := DefaultCoordinatorConfig()
	cfg.RoundTimeout = 10 * time.Second
	dc := NewDistCoordinator(cfg)
	dc.RegisterBuilder(coordReg("alice", 1))
	dc.StartRound(1)
	dc.SubmitFragment("alice", makeFrag("alice", 1, 21_000, 5))

	repBefore := dc.GetBuilder("alice").Reputation
	dc.FinalizeRound()
	repAfter := dc.GetBuilder("alice").Reputation

	if repAfter <= repBefore {
		t.Errorf("reputation should increase: before=%f, after=%f", repBefore, repAfter)
	}
}

func TestFinalizeRound_WithPriorAssembly(t *testing.T) {
	cfg := DefaultCoordinatorConfig()
	cfg.RoundTimeout = 10 * time.Second
	dc := NewDistCoordinator(cfg)
	dc.RegisterBuilder(coordReg("alice", 1))
	dc.StartRound(1)
	dc.SubmitFragment("alice", makeFrag("alice", 1, 21_000, 5))

	// Assemble first, then finalize.
	assembled, _ := dc.AssembleBlock()
	result, err := dc.FinalizeRound()
	if err != nil {
		t.Fatalf("FinalizeRound after AssembleBlock: %v", err)
	}
	if result.BlockHash != assembled.BlockHash {
		t.Error("finalized result should match assembled block")
	}
}

func TestHistory(t *testing.T) {
	cfg := DefaultCoordinatorConfig()
	cfg.RoundTimeout = 10 * time.Second
	dc := NewDistCoordinator(cfg)
	dc.RegisterBuilder(coordReg("alice", 1))

	for i := Slot(1); i <= 3; i++ {
		dc.StartRound(i)
		dc.SubmitFragment("alice", makeFrag("alice", 1, 21_000, int(i)))
		dc.FinalizeRound()
	}

	hist := dc.History(2)
	if len(hist) != 2 {
		t.Fatalf("History(2) = %d, want 2", len(hist))
	}
	if hist[0].Slot != 2 {
		t.Errorf("hist[0].Slot = %d, want 2", hist[0].Slot)
	}
	if hist[1].Slot != 3 {
		t.Errorf("hist[1].Slot = %d, want 3", hist[1].Slot)
	}
}

func TestHistory_Empty(t *testing.T) {
	dc := NewDistCoordinator(nil)
	if dc.History(5) != nil {
		t.Error("expected nil for empty history")
	}
}

func TestHistory_RequestMoreThanAvailable(t *testing.T) {
	cfg := DefaultCoordinatorConfig()
	cfg.RoundTimeout = 10 * time.Second
	dc := NewDistCoordinator(cfg)
	dc.RegisterBuilder(coordReg("alice", 1))
	dc.StartRound(1)
	dc.SubmitFragment("alice", makeFrag("alice", 1, 21_000, 1))
	dc.FinalizeRound()

	hist := dc.History(100)
	if len(hist) != 1 {
		t.Errorf("History(100) = %d, want 1", len(hist))
	}
}

func TestCurrentSlot_NoRound(t *testing.T) {
	dc := NewDistCoordinator(nil)
	if dc.CurrentSlot() != 0 {
		t.Errorf("CurrentSlot = %d, want 0", dc.CurrentSlot())
	}
}

func TestFragmentCount_NoRound(t *testing.T) {
	dc := NewDistCoordinator(nil)
	if dc.FragmentCount() != 0 {
		t.Errorf("FragmentCount = %d, want 0", dc.FragmentCount())
	}
}

func TestHasActiveRound_Finalized(t *testing.T) {
	cfg := DefaultCoordinatorConfig()
	cfg.RoundTimeout = 10 * time.Second
	dc := NewDistCoordinator(cfg)
	dc.RegisterBuilder(coordReg("a", 1))
	dc.StartRound(1)
	dc.SubmitFragment("a", makeFrag("a", 1, 21_000, 1))
	dc.FinalizeRound()

	if dc.HasActiveRound() {
		t.Error("finalized round should not be active")
	}
}

func TestStartRound_AfterFinalize(t *testing.T) {
	cfg := DefaultCoordinatorConfig()
	cfg.RoundTimeout = 10 * time.Second
	dc := NewDistCoordinator(cfg)
	dc.RegisterBuilder(coordReg("a", 1))
	dc.StartRound(1)
	dc.SubmitFragment("a", makeFrag("a", 1, 21_000, 1))
	dc.FinalizeRound()

	// Should be able to start a new round after finalization.
	if err := dc.StartRound(2); err != nil {
		t.Fatalf("StartRound after finalize: %v", err)
	}
	if dc.CurrentSlot() != 2 {
		t.Errorf("CurrentSlot = %d, want 2", dc.CurrentSlot())
	}
}

func TestIsDeadlinePassed(t *testing.T) {
	cfg := DefaultCoordinatorConfig()
	cfg.RoundTimeout = 1 * time.Millisecond
	dc := NewDistCoordinator(cfg)
	dc.StartRound(1)

	time.Sleep(5 * time.Millisecond)
	if !dc.IsDeadlinePassed() {
		t.Error("deadline should have passed")
	}
}

func TestIsDeadlinePassed_NoRound(t *testing.T) {
	dc := NewDistCoordinator(nil)
	if dc.IsDeadlinePassed() {
		t.Error("no round should not report deadline passed")
	}
}

func TestAssembleBlock_Finalized(t *testing.T) {
	cfg := DefaultCoordinatorConfig()
	cfg.RoundTimeout = 10 * time.Second
	dc := NewDistCoordinator(cfg)
	dc.RegisterBuilder(coordReg("a", 1))
	dc.StartRound(1)
	dc.SubmitFragment("a", makeFrag("a", 1, 21_000, 1))
	dc.FinalizeRound()

	_, err := dc.AssembleBlock()
	if err != ErrCoordRoundFinalized {
		t.Errorf("after finalize: got %v, want ErrCoordRoundFinalized", err)
	}
}

func TestScoring_HigherPriorityWins(t *testing.T) {
	cfg := DefaultCoordinatorConfig()
	cfg.GasLimit = 200_000
	cfg.RoundTimeout = 10 * time.Second
	dc := NewDistCoordinator(cfg)
	dc.RegisterBuilder(coordReg("low", 1))
	dc.RegisterBuilder(coordReg("high", 2))
	dc.StartRound(1)

	dc.SubmitFragment("low", makeFrag("low", 1, 30_000, 1))
	dc.SubmitFragment("high", makeFrag("high", 1, 30_000, 10))

	assembled, err := dc.AssembleBlock()
	if err != nil {
		t.Fatalf("AssembleBlock: %v", err)
	}
	// Both fit within gas limit. Higher-priority/score fragment should be first.
	if len(assembled.Fragments) != 2 {
		t.Fatalf("fragments = %d, want 2", len(assembled.Fragments))
	}
	if assembled.Fragments[0].BuilderID != "high" {
		t.Errorf("first = %s, want high (higher priority)", assembled.Fragments[0].BuilderID)
	}
	if assembled.Fragments[1].BuilderID != "low" {
		t.Errorf("second = %s, want low", assembled.Fragments[1].BuilderID)
	}
}

func TestMultipleBuilders_FragmentCollection(t *testing.T) {
	cfg := DefaultCoordinatorConfig()
	cfg.GasLimit = 1_000_000
	cfg.RoundTimeout = 10 * time.Second
	dc := NewDistCoordinator(cfg)

	for i := byte(1); i <= 5; i++ {
		id := string(rune('A'+i-1)) + "Builder"
		dc.RegisterBuilder(coordReg(id, i))
	}

	dc.StartRound(100)
	for i := byte(1); i <= 5; i++ {
		id := string(rune('A'+i-1)) + "Builder"
		dc.SubmitFragment(id, makeFrag(id, 2, 21_000, int(i)))
	}

	if dc.FragmentCount() != 5 {
		t.Errorf("FragmentCount = %d, want 5", dc.FragmentCount())
	}

	assembled, err := dc.AssembleBlock()
	if err != nil {
		t.Fatalf("AssembleBlock: %v", err)
	}
	if len(assembled.BuilderIDs) != 5 {
		t.Errorf("BuilderIDs = %d, want 5", len(assembled.BuilderIDs))
	}
	if assembled.TotalGas != 105_000 {
		t.Errorf("TotalGas = %d, want 105000", assembled.TotalGas)
	}
}

func TestConcurrentFragmentSubmission(t *testing.T) {
	cfg := DefaultCoordinatorConfig()
	cfg.GasLimit = 10_000_000
	cfg.MaxFragments = 200
	cfg.RoundTimeout = 10 * time.Second
	dc := NewDistCoordinator(cfg)

	for i := byte(1); i <= 10; i++ {
		reg := coordReg("b"+string(rune('0'+i)), i)
		reg.MaxFragments = 20
		dc.RegisterBuilder(reg)
	}
	dc.StartRound(1)

	var wg sync.WaitGroup
	for i := byte(1); i <= 10; i++ {
		wg.Add(1)
		go func(idx byte) {
			defer wg.Done()
			id := "b" + string(rune('0'+idx))
			for j := 0; j < 5; j++ {
				dc.SubmitFragment(id, makeFrag(id, 1, 1000, int(idx)))
			}
		}(i)
	}
	wg.Wait()

	if dc.FragmentCount() != 50 {
		t.Errorf("FragmentCount = %d, want 50", dc.FragmentCount())
	}
}

func TestEstimateRevenue(t *testing.T) {
	frag := &BlockFragment{GasUsed: 21000, Priority: 10}
	rev := estimateRevenue(frag)
	expected := big.NewInt(21000 * 10)
	if rev.Cmp(expected) != 0 {
		t.Errorf("revenue = %s, want %s", rev, expected)
	}
}

func TestEstimateRevenue_ZeroPriority(t *testing.T) {
	frag := &BlockFragment{GasUsed: 21000, Priority: 0}
	rev := estimateRevenue(frag)
	expected := big.NewInt(21000) // falls back to priority=1
	if rev.Cmp(expected) != 0 {
		t.Errorf("revenue = %s, want %s", rev, expected)
	}
}

func TestComputeScore(t *testing.T) {
	rev := big.NewInt(1_000_000_000) // 1 Gwei
	score := computeScore(rev, 1.0)
	// 1.0 * 0.7 + 1.0 * 0.3 = 1.0
	if score < 0.99 || score > 1.01 {
		t.Errorf("score = %f, want ~1.0", score)
	}
}

func TestAssemblyHash_Deterministic(t *testing.T) {
	ab := &AssembledBlock{
		Slot:     42,
		TotalGas: 21000,
		TotalTxs: 3,
	}
	h1 := computeAssemblyHash(ab)
	h2 := computeAssemblyHash(ab)
	if h1 != h2 {
		t.Error("assembly hash should be deterministic")
	}
}

func TestAssemblyHash_DifferentSlots(t *testing.T) {
	ab1 := &AssembledBlock{Slot: 1, TotalGas: 21000, TotalTxs: 1}
	ab2 := &AssembledBlock{Slot: 2, TotalGas: 21000, TotalTxs: 1}
	if computeAssemblyHash(ab1) == computeAssemblyHash(ab2) {
		t.Error("different slots should produce different hashes")
	}
}

func TestCapabilities(t *testing.T) {
	if CapTransactions&CapBlobs != 0 {
		t.Error("capabilities should not overlap")
	}
	combined := CapTransactions | CapBlobs | CapBundles
	if combined&CapTransactions == 0 || combined&CapBlobs == 0 || combined&CapBundles == 0 {
		t.Error("combined capabilities should contain all bits")
	}
}
