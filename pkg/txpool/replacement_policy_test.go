package txpool

import (
	"math/big"
	"sync"
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

// makeRBFLegacyTx creates a legacy tx for RBF policy tests.
func makeRBFLegacyTx(nonce uint64, gasPrice int64) *types.Transaction {
	return types.NewTransaction(&types.LegacyTx{
		Nonce:    nonce,
		GasPrice: big.NewInt(gasPrice),
		Gas:      21000,
		Value:    big.NewInt(0),
	})
}

// makeRBFDynTx creates a dynamic fee tx for RBF policy tests.
func makeRBFDynTx(nonce uint64, tipCap, feeCap int64) *types.Transaction {
	return types.NewTransaction(&types.DynamicFeeTx{
		ChainID:   big.NewInt(1),
		Nonce:     nonce,
		GasTipCap: big.NewInt(tipCap),
		GasFeeCap: big.NewInt(feeCap),
		Gas:       21000,
		Value:     big.NewInt(0),
	})
}

// makeRBFBlobTx creates a blob tx for RBF policy tests.
func makeRBFBlobTx(nonce uint64, tipCap, feeCap, blobFeeCap int64) *types.Transaction {
	return types.NewTransaction(&types.BlobTx{
		ChainID:    big.NewInt(1),
		Nonce:      nonce,
		GasTipCap:  big.NewInt(tipCap),
		GasFeeCap:  big.NewInt(feeCap),
		Gas:        21000,
		Value:      big.NewInt(0),
		BlobFeeCap: big.NewInt(blobFeeCap),
		BlobHashes: []types.Hash{{0x01}},
	})
}

var rbfSender = types.Address{0xaa, 0xbb}

func newTestRBFEngine() *RBFPolicyEngine {
	return NewRBFPolicyEngine(DefaultRBFPolicyConfig())
}

func TestRBFPolicyConfig_Defaults(t *testing.T) {
	cfg := DefaultRBFPolicyConfig()
	if cfg.MinFeeBump != RBFMinFeeBump {
		t.Errorf("MinFeeBump = %d, want %d", cfg.MinFeeBump, RBFMinFeeBump)
	}
	if cfg.MinTipBump != RBFMinTipBump {
		t.Errorf("MinTipBump = %d, want %d", cfg.MinTipBump, RBFMinTipBump)
	}
	if cfg.BlobFeeBump != RBFBlobFeeBump {
		t.Errorf("BlobFeeBump = %d, want %d", cfg.BlobFeeBump, RBFBlobFeeBump)
	}
	if cfg.MaxReplacements != RBFMaxReplacements {
		t.Errorf("MaxReplacements = %d, want %d", cfg.MaxReplacements, RBFMaxReplacements)
	}
	if cfg.MaxChainDepth != RBFMaxChainDepth {
		t.Errorf("MaxChainDepth = %d, want %d", cfg.MaxChainDepth, RBFMaxChainDepth)
	}
}

func TestRBFPolicyEngine_ConfigDefaults(t *testing.T) {
	// Zero config should fallback to defaults.
	e := NewRBFPolicyEngine(RBFPolicyConfig{})
	if e.config.MinFeeBump != RBFMinFeeBump {
		t.Errorf("fallback MinFeeBump = %d, want %d", e.config.MinFeeBump, RBFMinFeeBump)
	}
	if e.config.MaxReplacements != RBFMaxReplacements {
		t.Errorf("fallback MaxReplacements = %d, want %d", e.config.MaxReplacements, RBFMaxReplacements)
	}
}

func TestRBFPolicyEngine_NilTx(t *testing.T) {
	e := newTestRBFEngine()
	err := e.ValidateReplacement(rbfSender, nil, makeRBFLegacyTx(0, 100))
	if err != ErrRBFNilTx {
		t.Errorf("nil existing: got %v, want ErrRBFNilTx", err)
	}
	err = e.ValidateReplacement(rbfSender, makeRBFLegacyTx(0, 100), nil)
	if err != ErrRBFNilTx {
		t.Errorf("nil new: got %v, want ErrRBFNilTx", err)
	}
}

func TestRBFPolicyEngine_NonceMismatch(t *testing.T) {
	e := newTestRBFEngine()
	err := e.ValidateReplacement(rbfSender, makeRBFLegacyTx(0, 100), makeRBFLegacyTx(1, 200))
	if err != ErrRBFNonceMismatch {
		t.Errorf("nonce mismatch: got %v, want ErrRBFNonceMismatch", err)
	}
}

func TestRBFPolicyEngine_Legacy10PctBump(t *testing.T) {
	e := newTestRBFEngine()

	// Exactly 10% bump: 100 -> 110, should pass.
	err := e.ValidateReplacement(rbfSender,
		makeRBFLegacyTx(5, 100),
		makeRBFLegacyTx(5, 110),
	)
	if err != nil {
		t.Errorf("10%% bump: %v", err)
	}

	// 20% bump should also pass.
	sender2 := types.Address{0xcc}
	err = e.ValidateReplacement(sender2,
		makeRBFLegacyTx(5, 100),
		makeRBFLegacyTx(5, 120),
	)
	if err != nil {
		t.Errorf("20%% bump: %v", err)
	}
}

func TestRBFPolicyEngine_LegacyInsufficientBump(t *testing.T) {
	e := newTestRBFEngine()

	// 9% bump: 100 -> 109, should fail.
	err := e.ValidateReplacement(rbfSender,
		makeRBFLegacyTx(5, 100),
		makeRBFLegacyTx(5, 109),
	)
	if err != ErrRBFInsufficientFeeBump {
		t.Errorf("9%% bump: got %v, want ErrRBFInsufficientFeeBump", err)
	}

	// Same price should fail.
	err = e.ValidateReplacement(rbfSender,
		makeRBFLegacyTx(5, 100),
		makeRBFLegacyTx(5, 100),
	)
	if err != ErrRBFInsufficientFeeBump {
		t.Errorf("0%% bump: got %v, want ErrRBFInsufficientFeeBump", err)
	}

	// Lower price should fail.
	err = e.ValidateReplacement(rbfSender,
		makeRBFLegacyTx(5, 100),
		makeRBFLegacyTx(5, 90),
	)
	if err != ErrRBFInsufficientFeeBump {
		t.Errorf("negative bump: got %v, want ErrRBFInsufficientFeeBump", err)
	}
}

func TestRBFPolicyEngine_DynFeeBothBumps(t *testing.T) {
	e := newTestRBFEngine()

	// Both fee cap and tip cap have >= 10% bump.
	err := e.ValidateReplacement(rbfSender,
		makeRBFDynTx(3, 50, 100),
		makeRBFDynTx(3, 55, 110),
	)
	if err != nil {
		t.Errorf("dynamic 10%% bump both: %v", err)
	}
}

func TestRBFPolicyEngine_DynFeeInsufficientTip(t *testing.T) {
	e := newTestRBFEngine()

	// Fee cap has 10% bump but tip cap does not (8% bump).
	err := e.ValidateReplacement(rbfSender,
		makeRBFDynTx(3, 50, 100),
		makeRBFDynTx(3, 54, 110),
	)
	if err != ErrRBFInsufficientTipBump {
		t.Errorf("insufficient tip bump: got %v, want ErrRBFInsufficientTipBump", err)
	}
}

func TestRBFPolicyEngine_DynFeeInsufficientFeeCap(t *testing.T) {
	e := newTestRBFEngine()

	// Tip has 10% bump but fee cap does not (9% bump).
	err := e.ValidateReplacement(rbfSender,
		makeRBFDynTx(3, 50, 100),
		makeRBFDynTx(3, 55, 109),
	)
	if err != ErrRBFInsufficientFeeBump {
		t.Errorf("insufficient fee bump: got %v, want ErrRBFInsufficientFeeBump", err)
	}
}

func TestRBFPolicyEngine_BlobReplacement(t *testing.T) {
	e := newTestRBFEngine()

	// Blob tx: needs 10% gas fee bump AND 100% blob fee bump.
	// 100 -> 110 gas fee (10%), 50 -> 100 blob fee (100%).
	err := e.ValidateReplacement(rbfSender,
		makeRBFBlobTx(1, 50, 100, 50),
		makeRBFBlobTx(1, 55, 110, 100),
	)
	if err != nil {
		t.Errorf("valid blob replacement: %v", err)
	}
}

func TestRBFPolicyEngine_BlobInsufficientBlobFee(t *testing.T) {
	e := newTestRBFEngine()

	// Gas fees OK (10% bump) but blob fee only 50% bump (need 100%).
	err := e.ValidateReplacement(rbfSender,
		makeRBFBlobTx(1, 50, 100, 100),
		makeRBFBlobTx(1, 55, 110, 150), // 50% blob bump, need 100%.
	)
	if err != ErrRBFInsufficientBlobBump {
		t.Errorf("insufficient blob fee: got %v, want ErrRBFInsufficientBlobBump", err)
	}
}

func TestRBFPolicyEngine_BlobToNonBlobRejected(t *testing.T) {
	e := newTestRBFEngine()

	// Cannot replace a blob tx with a non-blob tx.
	err := e.ValidateReplacement(rbfSender,
		makeRBFBlobTx(1, 50, 100, 50),
		makeRBFDynTx(1, 55, 200),
	)
	if err != ErrRBFTypeMismatch {
		t.Errorf("blob->non-blob: got %v, want ErrRBFTypeMismatch", err)
	}
}

func TestRBFPolicyEngine_NonBlobToBlobAllowed(t *testing.T) {
	e := newTestRBFEngine()

	// Replacing a non-blob with a blob (with sufficient bumps) should work.
	err := e.ValidateReplacement(rbfSender,
		makeRBFDynTx(1, 50, 100),
		makeRBFBlobTx(1, 55, 110, 100),
	)
	if err != nil {
		t.Errorf("non-blob->blob: %v", err)
	}
}

func TestRBFPolicyEngine_ReplacementChainTracking(t *testing.T) {
	e := newTestRBFEngine()

	// Do 3 replacements at nonce 5.
	prices := []int64{110, 121, 134}
	existing := makeRBFLegacyTx(5, 100)
	for _, price := range prices {
		newTx := makeRBFLegacyTx(5, price)
		err := e.ValidateReplacement(rbfSender, existing, newTx)
		if err != nil {
			t.Fatalf("replacement at price %d: %v", price, err)
		}
		existing = newTx
	}

	count := e.ReplacementCount(rbfSender, 5)
	if count != 3 {
		t.Errorf("ReplacementCount = %d, want 3", count)
	}

	depth := e.AccountReplacementDepth(rbfSender)
	if depth != 3 {
		t.Errorf("AccountReplacementDepth = %d, want 3", depth)
	}

	chain := e.ReplacementChain(rbfSender, 5)
	if len(chain) != 3 {
		t.Fatalf("ReplacementChain len = %d, want 3", len(chain))
	}
	for i, entry := range chain {
		if entry.Nonce != 5 {
			t.Errorf("chain[%d].Nonce = %d, want 5", i, entry.Nonce)
		}
		if entry.FeeBump < 10 {
			t.Errorf("chain[%d].FeeBump = %d, want >= 10", i, entry.FeeBump)
		}
	}
}

func TestRBFPolicyEngine_MaxReplacements(t *testing.T) {
	cfg := DefaultRBFPolicyConfig()
	cfg.MaxReplacements = 3
	cfg.MaxChainDepth = 100
	e := NewRBFPolicyEngine(cfg)

	// Do 3 replacements, then the 4th should be rejected.
	existing := makeRBFLegacyTx(0, 100)
	for i := 0; i < 3; i++ {
		price := int64(110 + i*20) // Increasing prices with > 10% bump.
		newTx := makeRBFLegacyTx(0, price)
		err := e.ValidateReplacement(rbfSender, existing, newTx)
		if err != nil {
			t.Fatalf("replacement %d: %v", i, err)
		}
		existing = newTx
	}

	// 4th replacement should be rejected.
	err := e.ValidateReplacement(rbfSender, existing, makeRBFLegacyTx(0, 500))
	if err != ErrRBFMaxReplacements {
		t.Errorf("4th replacement: got %v, want ErrRBFMaxReplacements", err)
	}
}

func TestRBFPolicyEngine_MaxChainDepth(t *testing.T) {
	cfg := DefaultRBFPolicyConfig()
	cfg.MaxReplacements = 100
	cfg.MaxChainDepth = 5
	e := NewRBFPolicyEngine(cfg)

	// Replace 3 at nonce 0 and 2 at nonce 1 (total 5).
	existing0 := makeRBFLegacyTx(0, 100)
	for i := 0; i < 3; i++ {
		price := int64(120 + i*30)
		newTx := makeRBFLegacyTx(0, price)
		err := e.ValidateReplacement(rbfSender, existing0, newTx)
		if err != nil {
			t.Fatalf("nonce0 replacement %d: %v", i, err)
		}
		existing0 = newTx
	}

	existing1 := makeRBFLegacyTx(1, 100)
	for i := 0; i < 2; i++ {
		price := int64(120 + i*30)
		newTx := makeRBFLegacyTx(1, price)
		err := e.ValidateReplacement(rbfSender, existing1, newTx)
		if err != nil {
			t.Fatalf("nonce1 replacement %d: %v", i, err)
		}
		existing1 = newTx
	}

	// 6th total replacement should be rejected.
	err := e.ValidateReplacement(rbfSender, existing1, makeRBFLegacyTx(1, 500))
	if err != ErrRBFMaxChainDepth {
		t.Errorf("6th total: got %v, want ErrRBFMaxChainDepth", err)
	}
}

func TestRBFPolicyEngine_ClearNonce(t *testing.T) {
	e := newTestRBFEngine()

	_ = e.ValidateReplacement(rbfSender,
		makeRBFLegacyTx(5, 100),
		makeRBFLegacyTx(5, 110),
	)
	if count := e.ReplacementCount(rbfSender, 5); count != 1 {
		t.Fatalf("before clear: count = %d, want 1", count)
	}

	e.ClearNonce(rbfSender, 5)

	if count := e.ReplacementCount(rbfSender, 5); count != 0 {
		t.Errorf("after clear: count = %d, want 0", count)
	}
	if depth := e.AccountReplacementDepth(rbfSender); depth != 0 {
		t.Errorf("after clear: depth = %d, want 0", depth)
	}
}

func TestRBFPolicyEngine_ClearAccount(t *testing.T) {
	e := newTestRBFEngine()

	_ = e.ValidateReplacement(rbfSender,
		makeRBFLegacyTx(0, 100), makeRBFLegacyTx(0, 110))
	_ = e.ValidateReplacement(rbfSender,
		makeRBFLegacyTx(1, 100), makeRBFLegacyTx(1, 110))

	if e.TrackedAccounts() != 1 {
		t.Fatalf("TrackedAccounts = %d, want 1", e.TrackedAccounts())
	}

	e.ClearAccount(rbfSender)

	if e.TrackedAccounts() != 0 {
		t.Errorf("after ClearAccount: TrackedAccounts = %d, want 0", e.TrackedAccounts())
	}
	if e.ReplacementCount(rbfSender, 0) != 0 {
		t.Errorf("after ClearAccount: count = %d, want 0", e.ReplacementCount(rbfSender, 0))
	}
}

func TestRBFPolicyEngine_Stats(t *testing.T) {
	e := newTestRBFEngine()

	// Successful replacement.
	_ = e.ValidateReplacement(rbfSender,
		makeRBFLegacyTx(0, 100), makeRBFLegacyTx(0, 110))

	// Failed replacement (insufficient bump).
	_ = e.ValidateReplacement(rbfSender,
		makeRBFLegacyTx(1, 100), makeRBFLegacyTx(1, 105))

	stats := e.Stats()
	if stats.TotalAttempts != 2 {
		t.Errorf("TotalAttempts = %d, want 2", stats.TotalAttempts)
	}
	if stats.TotalAccepted != 1 {
		t.Errorf("TotalAccepted = %d, want 1", stats.TotalAccepted)
	}
	if stats.TotalRejected != 1 {
		t.Errorf("TotalRejected = %d, want 1", stats.TotalRejected)
	}
	if stats.FeeRejects != 1 {
		t.Errorf("FeeRejects = %d, want 1", stats.FeeRejects)
	}
}

func TestRBFPolicyEngine_MinFeeBumpRequired(t *testing.T) {
	e := newTestRBFEngine()

	// Legacy tx with gas price 100: required = 100 * 110 / 100 = 110.
	required := e.MinFeeBumpRequired(makeRBFLegacyTx(0, 100))
	if required.Int64() != 110 {
		t.Errorf("MinFeeBumpRequired(100) = %d, want 110", required.Int64())
	}

	// Dynamic tx with fee cap 200: required = 200 * 110 / 100 = 220.
	required = e.MinFeeBumpRequired(makeRBFDynTx(0, 50, 200))
	if required.Int64() != 220 {
		t.Errorf("MinFeeBumpRequired(200 feeCap) = %d, want 220", required.Int64())
	}

	// Nil tx returns 0.
	required = e.MinFeeBumpRequired(nil)
	if required.Sign() != 0 {
		t.Errorf("MinFeeBumpRequired(nil) = %d, want 0", required.Int64())
	}
}

func TestRBFPolicyEngine_MinBlobFeeBumpRequired(t *testing.T) {
	e := newTestRBFEngine()

	// Blob tx with blob fee cap 50: required = 50 * 200 / 100 = 100.
	required := e.MinBlobFeeBumpRequired(makeRBFBlobTx(0, 10, 100, 50))
	if required.Int64() != 100 {
		t.Errorf("MinBlobFeeBumpRequired(50) = %d, want 100", required.Int64())
	}

	// Non-blob tx has nil blob fee cap.
	required = e.MinBlobFeeBumpRequired(makeRBFLegacyTx(0, 100))
	if required.Sign() != 0 {
		t.Errorf("MinBlobFeeBumpRequired(legacy) = %d, want 0", required.Int64())
	}
}

func TestRBFPolicyEngine_ConcurrentAccess(t *testing.T) {
	e := newTestRBFEngine()
	var wg sync.WaitGroup

	// Concurrent validations from multiple goroutines with different senders.
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			sender := types.Address{byte(idx)}
			existing := makeRBFLegacyTx(0, 100)
			for j := 0; j < 5; j++ {
				price := int64(110 + j*20)
				newTx := makeRBFLegacyTx(0, price)
				_ = e.ValidateReplacement(sender, existing, newTx)
				existing = newTx
			}
		}(i)
	}

	// Concurrent reads.
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = e.Stats()
			_ = e.TrackedAccounts()
			_ = e.ReplacementCount(rbfSender, 0)
		}()
	}

	wg.Wait()

	stats := e.Stats()
	if stats.TotalAttempts != 50 {
		t.Errorf("TotalAttempts = %d, want 50", stats.TotalAttempts)
	}
}

func TestRBFPolicyEngine_EmptyStateAndEdgeCases(t *testing.T) {
	e := newTestRBFEngine()

	// Empty state queries should return zero values.
	if chain := e.ReplacementChain(rbfSender, 0); chain != nil {
		t.Errorf("empty chain = %v, want nil", chain)
	}
	if count := e.ReplacementCount(rbfSender, 0); count != 0 {
		t.Errorf("empty count = %d, want 0", count)
	}
	if depth := e.AccountReplacementDepth(rbfSender); depth != 0 {
		t.Errorf("empty depth = %d, want 0", depth)
	}

	// Clearing nonexistent data should not panic.
	e.ClearNonce(rbfSender, 99)
	e.ClearAccount(rbfSender)
}
