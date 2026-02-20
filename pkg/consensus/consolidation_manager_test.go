package consensus

import (
	"testing"
)

// Helper to make a validator with compounding credentials for CM tests.
func cmMakeValidator(pubkey byte, balance uint64, active bool, slashed bool) *ValidatorBalance {
	creds := [32]byte{CompoundingWithdrawalPrefix, 0xAA}
	activation := Epoch(0)
	exit := FarFutureEpoch
	if !active {
		activation = Epoch(999)
	}
	return &ValidatorBalance{
		Pubkey:                [48]byte{pubkey},
		WithdrawalCredentials: creds,
		EffectiveBalance:      balance,
		Slashed:               slashed,
		ActivationEpoch:       activation,
		ExitEpoch:             exit,
	}
}

func cmMakeNonCompoundingValidator(pubkey byte, balance uint64) *ValidatorBalance {
	creds := [32]byte{0x01, 0xAA}
	return &ValidatorBalance{
		Pubkey:                [48]byte{pubkey},
		WithdrawalCredentials: creds,
		EffectiveBalance:      balance,
		ActivationEpoch:       0,
		ExitEpoch:             FarFutureEpoch,
	}
}

func TestConsolidationManager_EnqueueBasic(t *testing.T) {
	cm := NewConsolidationManager(DefaultConsolidationManagerConfig())
	source := cmMakeValidator(1, 32*GweiPerETH, true, false)
	target := cmMakeValidator(2, 32*GweiPerETH, true, false)

	req := &ConsolidationReq{SourceIndex: 0, TargetIndex: 1}
	err := cm.EnqueueRequest(req, source, target, 100)
	if err != nil {
		t.Fatalf("EnqueueRequest failed: %v", err)
	}
	if cm.QueueLen() != 1 {
		t.Errorf("QueueLen = %d, want 1", cm.QueueLen())
	}
}

func TestConsolidationManager_EnqueueSameValidator(t *testing.T) {
	cm := NewConsolidationManager(DefaultConsolidationManagerConfig())
	v := cmMakeValidator(1, 32*GweiPerETH, true, false)

	req := &ConsolidationReq{SourceIndex: 0, TargetIndex: 0}
	err := cm.EnqueueRequest(req, v, v, 100)
	if err != ErrCMSourceEqualsTarget {
		t.Errorf("got %v, want ErrCMSourceEqualsTarget", err)
	}
}

func TestConsolidationManager_EnqueueSourceInactive(t *testing.T) {
	cm := NewConsolidationManager(DefaultConsolidationManagerConfig())
	source := cmMakeValidator(1, 32*GweiPerETH, false, false) // inactive
	target := cmMakeValidator(2, 32*GweiPerETH, true, false)

	req := &ConsolidationReq{SourceIndex: 0, TargetIndex: 1}
	err := cm.EnqueueRequest(req, source, target, 100)
	if err != ErrCMSourceInactive {
		t.Errorf("got %v, want ErrCMSourceInactive", err)
	}
}

func TestConsolidationManager_EnqueueTargetInactive(t *testing.T) {
	cm := NewConsolidationManager(DefaultConsolidationManagerConfig())
	source := cmMakeValidator(1, 32*GweiPerETH, true, false)
	target := cmMakeValidator(2, 32*GweiPerETH, false, false) // inactive

	req := &ConsolidationReq{SourceIndex: 0, TargetIndex: 1}
	err := cm.EnqueueRequest(req, source, target, 100)
	if err != ErrCMTargetInactive {
		t.Errorf("got %v, want ErrCMTargetInactive", err)
	}
}

func TestConsolidationManager_EnqueueSourceSlashed(t *testing.T) {
	cm := NewConsolidationManager(DefaultConsolidationManagerConfig())
	source := cmMakeValidator(1, 32*GweiPerETH, true, true) // slashed
	target := cmMakeValidator(2, 32*GweiPerETH, true, false)

	req := &ConsolidationReq{SourceIndex: 0, TargetIndex: 1}
	err := cm.EnqueueRequest(req, source, target, 100)
	if err != ErrCMSourceSlashed {
		t.Errorf("got %v, want ErrCMSourceSlashed", err)
	}
}

func TestConsolidationManager_EnqueueTargetSlashed(t *testing.T) {
	cm := NewConsolidationManager(DefaultConsolidationManagerConfig())
	source := cmMakeValidator(1, 32*GweiPerETH, true, false)
	target := cmMakeValidator(2, 32*GweiPerETH, true, true) // slashed

	req := &ConsolidationReq{SourceIndex: 0, TargetIndex: 1}
	err := cm.EnqueueRequest(req, source, target, 100)
	if err != ErrCMTargetSlashed {
		t.Errorf("got %v, want ErrCMTargetSlashed", err)
	}
}

func TestConsolidationManager_EnqueueCredsMismatch(t *testing.T) {
	cm := NewConsolidationManager(DefaultConsolidationManagerConfig())
	source := cmMakeValidator(1, 32*GweiPerETH, true, false)
	target := cmMakeValidator(2, 32*GweiPerETH, true, false)
	target.WithdrawalCredentials[1] = 0xBB // different creds

	req := &ConsolidationReq{SourceIndex: 0, TargetIndex: 1}
	err := cm.EnqueueRequest(req, source, target, 100)
	if err != ErrCMCredsMismatch {
		t.Errorf("got %v, want ErrCMCredsMismatch", err)
	}
}

func TestConsolidationManager_EnqueueNotCompounding(t *testing.T) {
	cm := NewConsolidationManager(DefaultConsolidationManagerConfig())
	source := cmMakeNonCompoundingValidator(1, 32*GweiPerETH)
	target := cmMakeNonCompoundingValidator(2, 32*GweiPerETH)

	req := &ConsolidationReq{SourceIndex: 0, TargetIndex: 1}
	err := cm.EnqueueRequest(req, source, target, 100)
	if err != ErrCMNotCompounding {
		t.Errorf("got %v, want ErrCMNotCompounding", err)
	}
}

func TestConsolidationManager_EnqueueDuplicate(t *testing.T) {
	cm := NewConsolidationManager(DefaultConsolidationManagerConfig())
	source := cmMakeValidator(1, 32*GweiPerETH, true, false)
	target := cmMakeValidator(2, 32*GweiPerETH, true, false)

	req := &ConsolidationReq{SourceIndex: 0, TargetIndex: 1}
	cm.EnqueueRequest(req, source, target, 100)
	err := cm.EnqueueRequest(req, source, target, 100)
	if err != ErrCMRequestExists {
		t.Errorf("got %v, want ErrCMRequestExists", err)
	}
}

func TestConsolidationManager_EnqueueQueueFull(t *testing.T) {
	cfg := DefaultConsolidationManagerConfig()
	cfg.MaxQueueSize = 2
	cm := NewConsolidationManager(cfg)

	for i := 0; i < 2; i++ {
		src := cmMakeValidator(byte(i*2+1), 32*GweiPerETH, true, false)
		tgt := cmMakeValidator(byte(i*2+2), 32*GweiPerETH, true, false)
		req := &ConsolidationReq{SourceIndex: uint64(i * 2), TargetIndex: uint64(i*2 + 1)}
		if err := cm.EnqueueRequest(req, src, tgt, 100); err != nil {
			t.Fatalf("enqueue %d failed: %v", i, err)
		}
	}

	src := cmMakeValidator(5, 32*GweiPerETH, true, false)
	tgt := cmMakeValidator(6, 32*GweiPerETH, true, false)
	req := &ConsolidationReq{SourceIndex: 4, TargetIndex: 5}
	err := cm.EnqueueRequest(req, src, tgt, 100)
	if err != ErrCMQueueFull {
		t.Errorf("got %v, want ErrCMQueueFull", err)
	}
}

func TestConsolidationManager_ProcessBasic(t *testing.T) {
	cm := NewConsolidationManager(DefaultConsolidationManagerConfig())
	source := cmMakeValidator(1, 32*GweiPerETH, true, false)
	target := cmMakeValidator(2, 32*GweiPerETH, true, false)

	req := &ConsolidationReq{SourceIndex: 0, TargetIndex: 1}
	cm.EnqueueRequest(req, source, target, 100)

	validators := map[uint64]*ValidatorBalance{0: source, 1: target}
	balances := map[uint64]uint64{0: 32 * GweiPerETH, 1: 32 * GweiPerETH}

	result, err := cm.ProcessNextConsolidation(
		func(idx uint64) *ValidatorBalance { return validators[idx] },
		func(idx uint64) uint64 { return balances[idx] },
		100,
	)
	if err != nil {
		t.Fatalf("ProcessNextConsolidation failed: %v", err)
	}
	if result.AmountTransferred != 32*GweiPerETH {
		t.Errorf("AmountTransferred = %d, want %d", result.AmountTransferred, 32*GweiPerETH)
	}
	if result.NewTargetBalance != 64*GweiPerETH {
		t.Errorf("NewTargetBalance = %d, want %d", result.NewTargetBalance, 64*GweiPerETH)
	}
	if result.NewSourceBalance != 0 {
		t.Errorf("NewSourceBalance = %d, want 0", result.NewSourceBalance)
	}
	if source.ExitEpoch != 101 {
		t.Errorf("source ExitEpoch = %d, want 101", source.ExitEpoch)
	}
	if source.EffectiveBalance != 0 {
		t.Errorf("source EffectiveBalance = %d, want 0", source.EffectiveBalance)
	}
}

func TestConsolidationManager_ProcessCappedBalance(t *testing.T) {
	cm := NewConsolidationManager(DefaultConsolidationManagerConfig())
	source := cmMakeValidator(1, 1500*GweiPerETH, true, false)
	target := cmMakeValidator(2, 1500*GweiPerETH, true, false)

	req := &ConsolidationReq{SourceIndex: 0, TargetIndex: 1}
	cm.EnqueueRequest(req, source, target, 100)

	validators := map[uint64]*ValidatorBalance{0: source, 1: target}
	balances := map[uint64]uint64{0: 1500 * GweiPerETH, 1: 1500 * GweiPerETH}

	result, err := cm.ProcessNextConsolidation(
		func(idx uint64) *ValidatorBalance { return validators[idx] },
		func(idx uint64) uint64 { return balances[idx] },
		100,
	)
	if err != nil {
		t.Fatalf("ProcessNextConsolidation failed: %v", err)
	}
	if result.TargetEffBal != CMMaxEffectiveBalance {
		t.Errorf("TargetEffBal = %d, want %d (max)", result.TargetEffBal, CMMaxEffectiveBalance)
	}
}

func TestConsolidationManager_RateLimit(t *testing.T) {
	cfg := DefaultConsolidationManagerConfig()
	cfg.MaxPerEpoch = 1
	cm := NewConsolidationManager(cfg)

	// Enqueue two requests.
	for i := 0; i < 2; i++ {
		src := cmMakeValidator(byte(i*2+1), 32*GweiPerETH, true, false)
		tgt := cmMakeValidator(byte(i*2+2), 32*GweiPerETH, true, false)
		req := &ConsolidationReq{SourceIndex: uint64(i * 2), TargetIndex: uint64(i*2 + 1)}
		cm.EnqueueRequest(req, src, tgt, 100)
	}

	validators := map[uint64]*ValidatorBalance{
		0: cmMakeValidator(1, 32*GweiPerETH, true, false),
		1: cmMakeValidator(2, 32*GweiPerETH, true, false),
		2: cmMakeValidator(3, 32*GweiPerETH, true, false),
		3: cmMakeValidator(4, 32*GweiPerETH, true, false),
	}
	balances := map[uint64]uint64{0: 32 * GweiPerETH, 1: 32 * GweiPerETH, 2: 32 * GweiPerETH, 3: 32 * GweiPerETH}

	getVal := func(idx uint64) *ValidatorBalance { return validators[idx] }
	getBal := func(idx uint64) uint64 { return balances[idx] }

	// First should succeed.
	_, err := cm.ProcessNextConsolidation(getVal, getBal, 100)
	if err != nil {
		t.Fatalf("first process failed: %v", err)
	}

	// Second should be rate limited.
	_, err = cm.ProcessNextConsolidation(getVal, getBal, 100)
	if err != ErrCMRateLimited {
		t.Errorf("got %v, want ErrCMRateLimited", err)
	}

	// Processing in next epoch should work.
	_, err = cm.ProcessNextConsolidation(getVal, getBal, 101)
	if err != nil {
		t.Fatalf("next epoch process failed: %v", err)
	}
}

func TestConsolidationManager_EmptyQueue(t *testing.T) {
	cm := NewConsolidationManager(DefaultConsolidationManagerConfig())
	_, err := cm.ProcessNextConsolidation(
		func(uint64) *ValidatorBalance { return nil },
		func(uint64) uint64 { return 0 },
		100,
	)
	if err != ErrCMEmptyQueue {
		t.Errorf("got %v, want ErrCMEmptyQueue", err)
	}
}

func TestConsolidationManager_ProcessedInEpoch(t *testing.T) {
	cm := NewConsolidationManager(DefaultConsolidationManagerConfig())
	source := cmMakeValidator(1, 32*GweiPerETH, true, false)
	target := cmMakeValidator(2, 32*GweiPerETH, true, false)

	req := &ConsolidationReq{SourceIndex: 0, TargetIndex: 1}
	cm.EnqueueRequest(req, source, target, 100)

	validators := map[uint64]*ValidatorBalance{0: source, 1: target}
	balances := map[uint64]uint64{0: 32 * GweiPerETH, 1: 32 * GweiPerETH}

	cm.ProcessNextConsolidation(
		func(idx uint64) *ValidatorBalance { return validators[idx] },
		func(idx uint64) uint64 { return balances[idx] },
		100,
	)

	if cm.ProcessedInEpoch(100) != 1 {
		t.Errorf("ProcessedInEpoch = %d, want 1", cm.ProcessedInEpoch(100))
	}
	if cm.ProcessedInEpoch(101) != 0 {
		t.Errorf("ProcessedInEpoch(101) = %d, want 0", cm.ProcessedInEpoch(101))
	}
}

func TestConsolidationManager_Results(t *testing.T) {
	cm := NewConsolidationManager(DefaultConsolidationManagerConfig())
	source := cmMakeValidator(1, 32*GweiPerETH, true, false)
	target := cmMakeValidator(2, 32*GweiPerETH, true, false)

	req := &ConsolidationReq{SourceIndex: 0, TargetIndex: 1}
	cm.EnqueueRequest(req, source, target, 100)

	validators := map[uint64]*ValidatorBalance{0: source, 1: target}
	balances := map[uint64]uint64{0: 32 * GweiPerETH, 1: 32 * GweiPerETH}

	cm.ProcessNextConsolidation(
		func(idx uint64) *ValidatorBalance { return validators[idx] },
		func(idx uint64) uint64 { return balances[idx] },
		100,
	)

	results := cm.Results()
	if len(results) != 1 {
		t.Fatalf("Results len = %d, want 1", len(results))
	}
	if results[0].SourceIndex != 0 || results[0].TargetIndex != 1 {
		t.Error("result indices mismatch")
	}
}

func TestConsolidationManager_PruneProcessed(t *testing.T) {
	cm := NewConsolidationManager(DefaultConsolidationManagerConfig())
	source := cmMakeValidator(1, 32*GweiPerETH, true, false)
	target := cmMakeValidator(2, 32*GweiPerETH, true, false)

	req := &ConsolidationReq{SourceIndex: 0, TargetIndex: 1}
	cm.EnqueueRequest(req, source, target, 100)

	validators := map[uint64]*ValidatorBalance{0: source, 1: target}
	balances := map[uint64]uint64{0: 32 * GweiPerETH, 1: 32 * GweiPerETH}

	cm.ProcessNextConsolidation(
		func(idx uint64) *ValidatorBalance { return validators[idx] },
		func(idx uint64) uint64 { return balances[idx] },
		100,
	)

	pruned := cm.PruneProcessed()
	if pruned != 1 {
		t.Errorf("pruned = %d, want 1", pruned)
	}
	if cm.TotalQueued() != 0 {
		t.Errorf("TotalQueued = %d, want 0 after prune", cm.TotalQueued())
	}
}

func TestConsolidationManager_QueueOrdering(t *testing.T) {
	cm := NewConsolidationManager(DefaultConsolidationManagerConfig())

	// Enqueue 3 requests.
	for i := 0; i < 3; i++ {
		src := cmMakeValidator(byte(i*2+1), 32*GweiPerETH, true, false)
		tgt := cmMakeValidator(byte(i*2+2), 32*GweiPerETH, true, false)
		req := &ConsolidationReq{
			SourceIndex: uint64(i * 10),
			TargetIndex: uint64(i*10 + 1),
		}
		cm.EnqueueRequest(req, src, tgt, 100)
	}

	// Create validators for processing.
	validators := make(map[uint64]*ValidatorBalance)
	balances := make(map[uint64]uint64)
	for i := 0; i < 3; i++ {
		si := uint64(i * 10)
		ti := uint64(i*10 + 1)
		validators[si] = cmMakeValidator(byte(i*2+1), 32*GweiPerETH, true, false)
		validators[ti] = cmMakeValidator(byte(i*2+2), 32*GweiPerETH, true, false)
		balances[si] = 32 * GweiPerETH
		balances[ti] = 32 * GweiPerETH
	}

	getVal := func(idx uint64) *ValidatorBalance { return validators[idx] }
	getBal := func(idx uint64) uint64 { return balances[idx] }

	// Process in FIFO order.
	r1, _ := cm.ProcessNextConsolidation(getVal, getBal, 100)
	r2, _ := cm.ProcessNextConsolidation(getVal, getBal, 100)
	r3, _ := cm.ProcessNextConsolidation(getVal, getBal, 100)

	if r1.SourceIndex != 0 {
		t.Errorf("first processed SourceIndex = %d, want 0", r1.SourceIndex)
	}
	if r2.SourceIndex != 10 {
		t.Errorf("second processed SourceIndex = %d, want 10", r2.SourceIndex)
	}
	if r3.SourceIndex != 20 {
		t.Errorf("third processed SourceIndex = %d, want 20", r3.SourceIndex)
	}
}
