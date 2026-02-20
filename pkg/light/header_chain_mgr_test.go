package light

import (
	"math/big"
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

func TestHeaderChainMgrStartStop(t *testing.T) {
	mgr := NewHeaderChainMgr(DefaultHeaderChainMgrConfig())

	if mgr.IsStarted() {
		t.Fatal("manager should not be started")
	}

	if err := mgr.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	if !mgr.IsStarted() {
		t.Fatal("manager should be started")
	}

	// Double start should error.
	if err := mgr.Start(); err != ErrMgrAlreadyStarted {
		t.Errorf("expected ErrMgrAlreadyStarted, got %v", err)
	}

	mgr.Stop()
	if mgr.IsStarted() {
		t.Fatal("manager should be stopped")
	}
}

func noSigCfg() HeaderChainMgrConfig {
	return HeaderChainMgrConfig{MaxHeaders: 1024, VerifySignatures: false, AllowReorgs: false}
}

func TestHeaderChainMgrInsertNoSig(t *testing.T) {
	mgr := NewHeaderChainMgr(noSigCfg())
	if err := mgr.Start(); err != nil {
		t.Fatal(err)
	}
	genesis := &types.Header{Number: big.NewInt(0), Difficulty: big.NewInt(1)}
	if err := mgr.InsertHeaderNoSig(genesis); err != nil {
		t.Fatalf("insert genesis failed: %v", err)
	}
	h1 := &types.Header{Number: big.NewInt(1), Difficulty: big.NewInt(1), ParentHash: genesis.Hash()}
	if err := mgr.InsertHeaderNoSig(h1); err != nil {
		t.Fatalf("insert h1 failed: %v", err)
	}
	if head := mgr.Head(); head == nil || head.Number.Uint64() != 1 {
		t.Errorf("head should be at number 1")
	}
}

func TestHeaderChainMgrInsertNotStarted(t *testing.T) {
	mgr := NewHeaderChainMgr(DefaultHeaderChainMgrConfig())
	h := &types.Header{Number: big.NewInt(0)}
	err := mgr.InsertHeaderNoSig(h)
	if err != ErrMgrNotStarted {
		t.Errorf("expected ErrMgrNotStarted, got %v", err)
	}
}

func TestHeaderChainMgrInsertNilHeader(t *testing.T) {
	mgr := NewHeaderChainMgr(DefaultHeaderChainMgrConfig())
	if err := mgr.Start(); err != nil {
		t.Fatal(err)
	}

	if err := mgr.InsertHeaderNoSig(nil); err != ErrMgrNilHeader {
		t.Errorf("expected ErrMgrNilHeader, got %v", err)
	}

	if err := mgr.InsertVerifiedHeader(nil, nil, nil); err != ErrMgrNilHeader {
		t.Errorf("expected ErrMgrNilHeader, got %v", err)
	}
}

func TestHeaderChainMgrInsertVerifiedHeader(t *testing.T) {
	cfg := HeaderChainMgrConfig{MaxHeaders: 1024, VerifySignatures: true, AllowReorgs: false}
	mgr := NewHeaderChainMgr(cfg)
	if err := mgr.Start(); err != nil {
		t.Fatal(err)
	}
	committee := MakeTestSyncCommittee(0)
	mgr.SetCommittee(committee)
	genesis := &types.Header{Number: big.NewInt(0), Difficulty: big.NewInt(1)}
	if err := mgr.InsertHeaderNoSig(genesis); err != nil {
		t.Fatal(err)
	}
	h1 := &types.Header{Number: big.NewInt(1), Difficulty: big.NewInt(1), ParentHash: genesis.Hash()}
	bits := MakeCommitteeBits(SyncCommitteeSize)
	sig := SignSyncCommittee(committee, h1.Hash(), bits)
	if err := mgr.InsertVerifiedHeader(h1, bits, sig); err != nil {
		t.Fatalf("insert with sig failed: %v", err)
	}
	if stats := mgr.Stats(); stats.SigVerified != 1 {
		t.Errorf("sig verified = %d, want 1", stats.SigVerified)
	}
}

func TestHeaderChainMgrInsertVerifiedBadSig(t *testing.T) {
	cfg := HeaderChainMgrConfig{MaxHeaders: 1024, VerifySignatures: true, AllowReorgs: false}
	mgr := NewHeaderChainMgr(cfg)
	if err := mgr.Start(); err != nil {
		t.Fatal(err)
	}
	mgr.SetCommittee(MakeTestSyncCommittee(0))
	genesis := &types.Header{Number: big.NewInt(0), Difficulty: big.NewInt(1)}
	if err := mgr.InsertHeaderNoSig(genesis); err != nil {
		t.Fatal(err)
	}
	h1 := &types.Header{Number: big.NewInt(1), Difficulty: big.NewInt(1), ParentHash: genesis.Hash()}
	err := mgr.InsertVerifiedHeader(h1, MakeCommitteeBits(SyncCommitteeSize), make([]byte, 32))
	if err != ErrMgrInvalidSig {
		t.Errorf("expected ErrMgrInvalidSig, got %v", err)
	}
	if stats := mgr.Stats(); stats.SigFailed != 1 {
		t.Errorf("sig failed = %d, want 1", stats.SigFailed)
	}
}

func TestHeaderChainMgrFinality(t *testing.T) {
	mgr := NewHeaderChainMgr(noSigCfg())
	if err := mgr.Start(); err != nil {
		t.Fatal(err)
	}
	genesis := &types.Header{Number: big.NewInt(0), Difficulty: big.NewInt(1)}
	h1 := &types.Header{Number: big.NewInt(1), Difficulty: big.NewInt(1), ParentHash: genesis.Hash()}
	h2 := &types.Header{Number: big.NewInt(2), Difficulty: big.NewInt(1), ParentHash: h1.Hash()}
	for _, h := range []*types.Header{genesis, h1, h2} {
		if err := mgr.InsertHeaderNoSig(h); err != nil {
			t.Fatal(err)
		}
	}
	if err := mgr.SetFinalized(h1.Hash(), 1); err != nil {
		t.Fatalf("SetFinalized failed: %v", err)
	}
	fh, fn := mgr.FinalizedHeader()
	if fh == nil || fn != 1 {
		t.Errorf("finalized = %v at %d, want non-nil at 1", fh, fn)
	}
	if err := mgr.SetFinalized(h2.Hash(), 2); err != nil {
		t.Fatal(err)
	}
	if err := mgr.SetFinalized(h1.Hash(), 0); err != ErrMgrFinalityRegress {
		t.Errorf("expected ErrMgrFinalityRegress, got %v", err)
	}
}

func TestHeaderChainMgrFinalityNotStarted(t *testing.T) {
	mgr := NewHeaderChainMgr(DefaultHeaderChainMgrConfig())
	err := mgr.SetFinalized(types.Hash{}, 0)
	if err != ErrMgrNotStarted {
		t.Errorf("expected ErrMgrNotStarted, got %v", err)
	}
}

func TestHeaderChainMgrHandleReorg(t *testing.T) {
	cfg := HeaderChainMgrConfig{MaxHeaders: 1024, VerifySignatures: false, AllowReorgs: true}
	mgr := NewHeaderChainMgr(cfg)
	if err := mgr.Start(); err != nil {
		t.Fatal(err)
	}
	genesis := &types.Header{Number: big.NewInt(0), Difficulty: big.NewInt(1)}
	h1 := &types.Header{Number: big.NewInt(1), Difficulty: big.NewInt(1), ParentHash: genesis.Hash()}
	h2 := &types.Header{Number: big.NewInt(2), Difficulty: big.NewInt(1), ParentHash: h1.Hash()}
	for _, h := range []*types.Header{genesis, h1, h2} {
		if err := mgr.InsertHeaderNoSig(h); err != nil {
			t.Fatal(err)
		}
	}
	h2f := &types.Header{Number: big.NewInt(2), Difficulty: big.NewInt(2), ParentHash: h1.Hash(), Extra: []byte("fork")}
	h3f := &types.Header{Number: big.NewInt(3), Difficulty: big.NewInt(2), ParentHash: h2f.Hash()}
	event, err := mgr.HandleReorg([]*types.Header{h2f, h3f})
	if err != nil {
		t.Fatalf("HandleReorg failed: %v", err)
	}
	if event == nil || event.NewHeight != 3 {
		t.Errorf("expected reorg to height 3")
	}
	if len(mgr.ReorgHistory()) != 1 {
		t.Errorf("expected 1 reorg event")
	}
}

func TestHeaderChainMgrHandleReorgBelowFinalized(t *testing.T) {
	cfg := HeaderChainMgrConfig{MaxHeaders: 1024, VerifySignatures: false, AllowReorgs: true}
	mgr := NewHeaderChainMgr(cfg)
	if err := mgr.Start(); err != nil {
		t.Fatal(err)
	}
	genesis := &types.Header{Number: big.NewInt(0), Difficulty: big.NewInt(1)}
	h1 := &types.Header{Number: big.NewInt(1), Difficulty: big.NewInt(1), ParentHash: genesis.Hash()}
	h2 := &types.Header{Number: big.NewInt(2), Difficulty: big.NewInt(1), ParentHash: h1.Hash()}
	for _, h := range []*types.Header{genesis, h1, h2} {
		if err := mgr.InsertHeaderNoSig(h); err != nil {
			t.Fatal(err)
		}
	}
	if err := mgr.SetFinalized(h2.Hash(), 2); err != nil {
		t.Fatal(err)
	}
	fork := &types.Header{Number: big.NewInt(1), Difficulty: big.NewInt(10), ParentHash: genesis.Hash(), Extra: []byte("fork")}
	if _, err := mgr.HandleReorg([]*types.Header{fork}); err != ErrMgrReorgBelowFinal {
		t.Errorf("expected ErrMgrReorgBelowFinal, got %v", err)
	}
}

func TestHeaderChainMgrCanonicalSnapshot(t *testing.T) {
	mgr := NewHeaderChainMgr(noSigCfg())
	if err := mgr.Start(); err != nil {
		t.Fatal(err)
	}
	genesis := &types.Header{Number: big.NewInt(0), Difficulty: big.NewInt(1)}
	h1 := &types.Header{Number: big.NewInt(1), Difficulty: big.NewInt(1), ParentHash: genesis.Hash()}
	h2 := &types.Header{Number: big.NewInt(2), Difficulty: big.NewInt(1), ParentHash: h1.Hash()}
	for _, h := range []*types.Header{genesis, h1, h2} {
		if err := mgr.InsertHeaderNoSig(h); err != nil {
			t.Fatal(err)
		}
	}
	snap, err := mgr.CanonicalSnapshot(0, 2)
	if err != nil {
		t.Fatalf("CanonicalSnapshot failed: %v", err)
	}
	if len(snap.Headers) != 3 || snap.StartBlock != 0 || snap.EndBlock != 2 {
		t.Errorf("snap = %d hdrs [%d,%d], want 3 [0,2]", len(snap.Headers), snap.StartBlock, snap.EndBlock)
	}
	if snap.ChainRoot.IsZero() {
		t.Error("chain root should not be zero")
	}
}

func TestHeaderChainMgrCanonicalSnapshotInvalidRange(t *testing.T) {
	mgr := NewHeaderChainMgr(DefaultHeaderChainMgrConfig())
	if err := mgr.Start(); err != nil {
		t.Fatal(err)
	}

	_, err := mgr.CanonicalSnapshot(10, 5)
	if err != ErrMgrRangeInvalid {
		t.Errorf("expected ErrMgrRangeInvalid, got %v", err)
	}
}

func TestHeaderChainMgrCanonicalSnapshotEmpty(t *testing.T) {
	mgr := NewHeaderChainMgr(DefaultHeaderChainMgrConfig())
	if err := mgr.Start(); err != nil {
		t.Fatal(err)
	}

	_, err := mgr.CanonicalSnapshot(100, 200)
	if err != ErrMgrSnapshotEmpty {
		t.Errorf("expected ErrMgrSnapshotEmpty, got %v", err)
	}
}

func TestHeaderChainMgrVerifyChainConsistency(t *testing.T) {
	mgr := NewHeaderChainMgr(noSigCfg())
	if err := mgr.Start(); err != nil {
		t.Fatal(err)
	}
	genesis := &types.Header{Number: big.NewInt(0), Difficulty: big.NewInt(1)}
	h1 := &types.Header{Number: big.NewInt(1), Difficulty: big.NewInt(1), ParentHash: genesis.Hash()}
	for _, h := range []*types.Header{genesis, h1} {
		if err := mgr.InsertHeaderNoSig(h); err != nil {
			t.Fatal(err)
		}
	}
	if err := mgr.VerifyChainConsistency(0, 1); err != nil {
		t.Errorf("chain should be consistent: %v", err)
	}
	if err := mgr.VerifyChainConsistency(10, 5); err != ErrMgrRangeInvalid {
		t.Errorf("expected ErrMgrRangeInvalid, got %v", err)
	}
}

func TestHeaderChainMgrStats(t *testing.T) {
	mgr := NewHeaderChainMgr(noSigCfg())
	if err := mgr.Start(); err != nil {
		t.Fatal(err)
	}
	if err := mgr.InsertHeaderNoSig(&types.Header{Number: big.NewInt(0), Difficulty: big.NewInt(1)}); err != nil {
		t.Fatal(err)
	}
	stats := mgr.Stats()
	if stats.HeadersInserted != 1 || stats.ChainLen != 1 {
		t.Errorf("stats = inserted:%d len:%d, want 1,1", stats.HeadersInserted, stats.ChainLen)
	}
}

func TestComputeSnapshotRoot(t *testing.T) {
	snap := &ChainSnapshot{
		Headers: []*types.Header{{Number: big.NewInt(0)}},
		ChainRoot: types.HexToHash("0xabcdef"),
	}
	root := ComputeSnapshotRoot(snap)
	if root.IsZero() {
		t.Error("should not be zero")
	}
	if root != ComputeSnapshotRoot(snap) {
		t.Error("should be deterministic")
	}
	if !ComputeSnapshotRoot(nil).IsZero() {
		t.Error("nil should produce zero")
	}
}

func TestHeaderChainMgrGetHeaderByNumber(t *testing.T) {
	mgr := NewHeaderChainMgr(noSigCfg())
	if err := mgr.Start(); err != nil {
		t.Fatal(err)
	}
	if err := mgr.InsertHeaderNoSig(&types.Header{Number: big.NewInt(0), Difficulty: big.NewInt(1)}); err != nil {
		t.Fatal(err)
	}
	if h := mgr.GetHeaderByNumber(0); h == nil || h.Number.Uint64() != 0 {
		t.Error("header at 0 should exist with number 0")
	}
	if mgr.GetHeaderByNumber(999) != nil {
		t.Error("header at 999 should be nil")
	}
}

func TestMakeTestChainHeaders(t *testing.T) {
	headers := makeTestChainHeaders(0, 5)
	if len(headers) != 5 {
		t.Fatalf("expected 5 headers, got %d", len(headers))
	}
	for i := 0; i < 5; i++ {
		if headers[i].Number.Uint64() != uint64(i) {
			t.Errorf("header[%d] number = %d, want %d", i, headers[i].Number.Uint64(), i)
		}
	}
	// Verify parent linkage.
	for i := 1; i < 5; i++ {
		if headers[i].ParentHash != headers[i-1].Hash() {
			t.Errorf("header[%d] parent hash mismatch", i)
		}
	}
}

func TestDefaultHeaderChainMgrConfig(t *testing.T) {
	cfg := DefaultHeaderChainMgrConfig()
	if cfg.MaxHeaders != 8192 {
		t.Errorf("MaxHeaders = %d, want 8192", cfg.MaxHeaders)
	}
	if !cfg.VerifySignatures {
		t.Error("VerifySignatures should default to true")
	}
	if !cfg.AllowReorgs {
		t.Error("AllowReorgs should default to true")
	}
}

func TestHeaderChainMgrHandleReorgEmpty(t *testing.T) {
	mgr := NewHeaderChainMgr(DefaultHeaderChainMgrConfig())
	if err := mgr.Start(); err != nil {
		t.Fatal(err)
	}

	_, err := mgr.HandleReorg(nil)
	if err != ErrMgrNilHeader {
		t.Errorf("expected ErrMgrNilHeader for empty reorg, got %v", err)
	}
}
