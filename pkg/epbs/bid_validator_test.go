package epbs

import (
	"errors"
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

// --- helpers (prefixed to avoid collision with auction_engine_test.go) ---

func bvBid(slot, value uint64) *BuilderBid {
	return &BuilderBid{
		ParentBlockHash: types.HexToHash("0xaaaa000000000000000000000000000000000000000000000000000000000000"),
		BlockHash:       types.HexToHash("0xbbbb000000000000000000000000000000000000000000000000000000000000"),
		Slot:            slot,
		Value:           value,
		GasLimit:        30_000_000,
		BuilderIndex:    1,
		FeeRecipient:    types.HexToAddress("0xdead"),
	}
}

func bvAddr(b byte) types.Address {
	var a types.Address
	a[19] = b
	return a
}

// --- ValidateBidSignature tests ---

func TestValidateBidSignatureValid(t *testing.T) {
	bid := bvBid(100, 5000)
	addr := bvAddr(0x01)
	commitment := ComputeBidCommitment(bid, addr)

	if err := ValidateBidSignature(bid, addr, commitment); err != nil {
		t.Errorf("valid signature: %v", err)
	}
}

func TestValidateBidSignatureMismatch(t *testing.T) {
	bid := bvBid(100, 5000)
	addr := bvAddr(0x01)

	// Use a wrong commitment.
	wrongCommitment := types.HexToHash("0xdeadbeef")
	err := ValidateBidSignature(bid, addr, wrongCommitment)
	if !errors.Is(err, ErrBidCommitmentMismatch) {
		t.Errorf("expected ErrBidCommitmentMismatch, got %v", err)
	}
}

func TestValidateBidSignatureNilBid(t *testing.T) {
	addr := bvAddr(0x01)
	err := ValidateBidSignature(nil, addr, types.Hash{})
	if !errors.Is(err, ErrBidValidatorNilBid) {
		t.Errorf("expected ErrBidValidatorNilBid, got %v", err)
	}
}

func TestValidateBidSignatureDifferentAddressProducesDifferentCommitment(t *testing.T) {
	bid := bvBid(100, 5000)
	addr1 := bvAddr(0x01)
	addr2 := bvAddr(0x02)

	c1 := ComputeBidCommitment(bid, addr1)
	c2 := ComputeBidCommitment(bid, addr2)

	if c1 == c2 {
		t.Error("different addresses should produce different commitments")
	}
}

func TestValidateBidSignatureDeterministic(t *testing.T) {
	bid := bvBid(100, 5000)
	addr := bvAddr(0x01)

	c1 := ComputeBidCommitment(bid, addr)
	c2 := ComputeBidCommitment(bid, addr)

	if c1 != c2 {
		t.Error("commitment should be deterministic")
	}
}

func TestValidateBidSignatureDifferentValues(t *testing.T) {
	bid1 := bvBid(100, 5000)
	bid2 := bvBid(100, 7000)
	addr := bvAddr(0x01)

	c1 := ComputeBidCommitment(bid1, addr)
	c2 := ComputeBidCommitment(bid2, addr)

	if c1 == c2 {
		t.Error("different bid values should produce different commitments")
	}
}

// --- ValidateBidCollateral tests ---

func TestValidateBidCollateralSufficient(t *testing.T) {
	config := DefaultBidValidatorConfig()
	// 32 ETH exactly meets the requirement.
	err := ValidateBidCollateral(32_000_000_000, config)
	if err != nil {
		t.Errorf("sufficient collateral: %v", err)
	}
}

func TestValidateBidCollateralAboveMin(t *testing.T) {
	config := DefaultBidValidatorConfig()
	err := ValidateBidCollateral(64_000_000_000, config)
	if err != nil {
		t.Errorf("above min collateral: %v", err)
	}
}

func TestValidateBidCollateralInsufficient(t *testing.T) {
	config := DefaultBidValidatorConfig()
	err := ValidateBidCollateral(1_000_000_000, config)
	if !errors.Is(err, ErrBidInsufficientCollateral) {
		t.Errorf("expected ErrBidInsufficientCollateral, got %v", err)
	}
}

func TestValidateBidCollateralZero(t *testing.T) {
	config := DefaultBidValidatorConfig()
	err := ValidateBidCollateral(0, config)
	if !errors.Is(err, ErrBidInsufficientCollateral) {
		t.Errorf("expected ErrBidInsufficientCollateral, got %v", err)
	}
}

// --- ValidateBidValue tests ---

func TestValidateBidValueInRange(t *testing.T) {
	config := DefaultBidValidatorConfig()
	err := ValidateBidValue(1000, config)
	if err != nil {
		t.Errorf("valid value: %v", err)
	}
}

func TestValidateBidValueAtMin(t *testing.T) {
	config := DefaultBidValidatorConfig()
	err := ValidateBidValue(config.MinBidValue, config)
	if err != nil {
		t.Errorf("min value: %v", err)
	}
}

func TestValidateBidValueBelowMin(t *testing.T) {
	config := BidValidatorConfig{MinBidValue: 100, MaxBidValue: 10000}
	err := ValidateBidValue(50, config)
	if !errors.Is(err, ErrBidValueTooLow) {
		t.Errorf("expected ErrBidValueTooLow, got %v", err)
	}
}

func TestValidateBidValueAboveMax(t *testing.T) {
	config := BidValidatorConfig{MinBidValue: 1, MaxBidValue: 10000}
	err := ValidateBidValue(20000, config)
	if !errors.Is(err, ErrBidValueTooHigh) {
		t.Errorf("expected ErrBidValueTooHigh, got %v", err)
	}
}

func TestValidateBidValueNoCeiling(t *testing.T) {
	config := BidValidatorConfig{MinBidValue: 1, MaxBidValue: 0}
	// No ceiling set, very large value should pass.
	err := ValidateBidValue(999_999_999_999_999, config)
	if err != nil {
		t.Errorf("no ceiling: %v", err)
	}
}

// --- BidScorer tests ---

func TestBidScorerCreation(t *testing.T) {
	scorer, err := NewBidScorer(DefaultScorerConfig())
	if err != nil {
		t.Fatalf("NewBidScorer: %v", err)
	}
	if scorer == nil {
		t.Fatal("scorer is nil")
	}
}

func TestBidScorerZeroMaxNorm(t *testing.T) {
	cfg := DefaultScorerConfig()
	cfg.MaxBidValueForNorm = 0
	_, err := NewBidScorer(cfg)
	if err == nil {
		t.Error("expected error for zero MaxBidValueForNorm")
	}
}

func TestBidScorerHighValueHighReputation(t *testing.T) {
	scorer, _ := NewBidScorer(DefaultScorerConfig())

	bid := bvBid(100, 100_000_000) // at max norm value
	rep := &BuilderReputation{Score: 100.0, TotalWins: 100, TotalDeliveries: 100}

	score := scorer.ScoreBid(bid, rep)
	// With all components at max, score should be very close to 1.0.
	if score < 0.95 {
		t.Errorf("max score = %f, want >= 0.95", score)
	}
}

func TestBidScorerLowValueLowReputation(t *testing.T) {
	scorer, _ := NewBidScorer(DefaultScorerConfig())

	bid := bvBid(100, 1) // very low value
	rep := &BuilderReputation{Score: 0.0, TotalWins: 100, TotalDeliveries: 10}

	score := scorer.ScoreBid(bid, rep)
	if score > 0.5 {
		t.Errorf("low score = %f, want <= 0.5", score)
	}
}

func TestBidScorerNilBid(t *testing.T) {
	scorer, _ := NewBidScorer(DefaultScorerConfig())
	rep := &BuilderReputation{Score: 50.0}
	score := scorer.ScoreBid(nil, rep)
	if score != 0.0 {
		t.Errorf("nil bid score = %f, want 0.0", score)
	}
}

func TestBidScorerNilReputation(t *testing.T) {
	scorer, _ := NewBidScorer(DefaultScorerConfig())
	bid := bvBid(100, 5000)
	score := scorer.ScoreBid(bid, nil)
	if score != 0.0 {
		t.Errorf("nil reputation score = %f, want 0.0", score)
	}
}

func TestBidScorerNewBuilderGetsBonusFromReliability(t *testing.T) {
	scorer, _ := NewBidScorer(DefaultScorerConfig())

	bid := bvBid(100, 50_000_000)
	// New builder with no wins: delivery rate defaults to 1.0.
	rep := &BuilderReputation{Score: 50.0, TotalWins: 0, TotalDeliveries: 0}

	score := scorer.ScoreBid(bid, rep)
	// Should include reliability bonus since delivery rate = 1.0.
	if score < 0.3 {
		t.Errorf("new builder score = %f, want >= 0.3", score)
	}
}

// --- SelectWinningBid tests ---

func TestSelectWinningBidHighestScore(t *testing.T) {
	bids := []ScoredBid{
		{Bid: bvBid(100, 3000), Score: 0.5, BidHash: types.HexToHash("0xaa")},
		{Bid: bvBid(100, 7000), Score: 0.9, BidHash: types.HexToHash("0xbb")},
		{Bid: bvBid(100, 5000), Score: 0.7, BidHash: types.HexToHash("0xcc")},
	}

	winner, idx, err := SelectWinningBid(bids)
	if err != nil {
		t.Fatalf("SelectWinningBid: %v", err)
	}
	if winner.Score != 0.9 {
		t.Errorf("winner score = %f, want 0.9", winner.Score)
	}
	if idx != 1 {
		t.Errorf("winner index = %d, want 1", idx)
	}
}

func TestSelectWinningBidTiebreakByHash(t *testing.T) {
	// Two bids with the same score: the one with the smaller hash wins.
	smallHash := types.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000001")
	largeHash := types.HexToHash("0xffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff")

	bids := []ScoredBid{
		{Bid: bvBid(100, 5000), Score: 0.8, BidHash: largeHash},
		{Bid: bvBid(100, 5000), Score: 0.8, BidHash: smallHash},
	}

	winner, idx, err := SelectWinningBid(bids)
	if err != nil {
		t.Fatalf("SelectWinningBid: %v", err)
	}
	if winner.BidHash != smallHash {
		t.Errorf("tiebreak: winner hash = %s, want smaller hash", winner.BidHash.Hex())
	}
	if idx != 1 {
		t.Errorf("tiebreak: winner index = %d, want 1", idx)
	}
}

func TestSelectWinningBidEmpty(t *testing.T) {
	_, _, err := SelectWinningBid(nil)
	if !errors.Is(err, ErrBidNoBids) {
		t.Errorf("expected ErrBidNoBids, got %v", err)
	}
}

func TestSelectWinningBidSingleBid(t *testing.T) {
	bids := []ScoredBid{
		{Bid: bvBid(100, 5000), Score: 0.7, BidHash: types.HexToHash("0xaa")},
	}

	winner, idx, err := SelectWinningBid(bids)
	if err != nil {
		t.Fatalf("SelectWinningBid: %v", err)
	}
	if idx != 0 {
		t.Errorf("single bid index = %d, want 0", idx)
	}
	if winner.Score != 0.7 {
		t.Errorf("single bid score = %f, want 0.7", winner.Score)
	}
}

// --- FullBidValidation tests ---

func TestFullBidValidationPass(t *testing.T) {
	bid := bvBid(100, 5000)
	addr := bvAddr(0x01)
	commitment := ComputeBidCommitment(bid, addr)
	config := DefaultBidValidatorConfig()

	err := FullBidValidation(bid, addr, commitment, 32_000_000_000, config)
	if err != nil {
		t.Errorf("full validation pass: %v", err)
	}
}

func TestFullBidValidationNilBid(t *testing.T) {
	config := DefaultBidValidatorConfig()
	err := FullBidValidation(nil, bvAddr(0x01), types.Hash{}, 32_000_000_000, config)
	if !errors.Is(err, ErrBidValidatorNilBid) {
		t.Errorf("expected ErrBidValidatorNilBid, got %v", err)
	}
}

func TestFullBidValidationBadCommitment(t *testing.T) {
	bid := bvBid(100, 5000)
	addr := bvAddr(0x01)
	badCommitment := types.HexToHash("0xdead")
	config := DefaultBidValidatorConfig()

	err := FullBidValidation(bid, addr, badCommitment, 32_000_000_000, config)
	if !errors.Is(err, ErrBidCommitmentMismatch) {
		t.Errorf("expected ErrBidCommitmentMismatch, got %v", err)
	}
}

func TestFullBidValidationLowCollateral(t *testing.T) {
	bid := bvBid(100, 5000)
	addr := bvAddr(0x01)
	commitment := ComputeBidCommitment(bid, addr)
	config := DefaultBidValidatorConfig()

	err := FullBidValidation(bid, addr, commitment, 1_000_000, config)
	if !errors.Is(err, ErrBidInsufficientCollateral) {
		t.Errorf("expected ErrBidInsufficientCollateral, got %v", err)
	}
}

func TestFullBidValidationValueTooHigh(t *testing.T) {
	bid := bvBid(100, 999_999_999_999_999_999) // extremely high
	addr := bvAddr(0x01)
	commitment := ComputeBidCommitment(bid, addr)
	config := DefaultBidValidatorConfig()

	err := FullBidValidation(bid, addr, commitment, 32_000_000_000, config)
	if !errors.Is(err, ErrBidValueTooHigh) {
		t.Errorf("expected ErrBidValueTooHigh, got %v", err)
	}
}

// --- DeliveryRate tests ---

func TestDeliveryRateNoWins(t *testing.T) {
	rep := &BuilderReputation{TotalWins: 0, TotalDeliveries: 0}
	if rate := rep.DeliveryRate(); rate != 1.0 {
		t.Errorf("no wins delivery rate = %f, want 1.0", rate)
	}
}

func TestDeliveryRatePerfect(t *testing.T) {
	rep := &BuilderReputation{TotalWins: 100, TotalDeliveries: 100}
	if rate := rep.DeliveryRate(); rate != 1.0 {
		t.Errorf("perfect delivery rate = %f, want 1.0", rate)
	}
}

func TestDeliveryRatePartial(t *testing.T) {
	rep := &BuilderReputation{TotalWins: 10, TotalDeliveries: 7}
	rate := rep.DeliveryRate()
	if rate < 0.69 || rate > 0.71 {
		t.Errorf("partial delivery rate = %f, want ~0.7", rate)
	}
}

// --- DefaultBidValidatorConfig test ---

func TestDefaultBidValidatorConfig(t *testing.T) {
	config := DefaultBidValidatorConfig()
	if config.MinCollateral != 32_000_000_000 {
		t.Errorf("MinCollateral = %d, want 32_000_000_000", config.MinCollateral)
	}
	if config.MinBidValue != 1 {
		t.Errorf("MinBidValue = %d, want 1", config.MinBidValue)
	}
	if config.MaxBidValue == 0 {
		t.Error("MaxBidValue should not be zero")
	}
}
