package epbs

import (
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

// --- helpers ---

func makeTestBidForScoring(value uint64) *BuilderBid {
	return &BuilderBid{
		ParentBlockHash: types.HexToHash("0xaa"),
		BlockHash:       types.HexToHash("0xbb"),
		Slot:            100,
		Value:           value,
		BuilderIndex:    1,
	}
}

// --- BidScoreCalculator tests ---

func TestBSCalculatorCreation(t *testing.T) {
	_, err := NewBidScoreCalculator(DefaultBidScoreConfig())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Zero MaxBidForNorm should fail.
	cfg := DefaultBidScoreConfig()
	cfg.MaxBidForNorm = 0
	_, err = NewBidScoreCalculator(cfg)
	if err == nil {
		t.Fatal("expected error for zero MaxBidForNorm")
	}
}

func TestBSCalculatorZeroLatencyMs(t *testing.T) {
	cfg := DefaultBidScoreConfig()
	cfg.MaxLatencyMs = 0
	_, err := NewBidScoreCalculator(cfg)
	if err == nil {
		t.Fatal("expected error for zero MaxLatencyMs")
	}
}

func TestBSCalculatorMaxScore(t *testing.T) {
	calc, _ := NewBidScoreCalculator(DefaultBidScoreConfig())
	components := ScoreComponents{
		BidAmount:        200_000_000, // above max, clamps to 1.0
		ReputationScore:  100,
		InclusionQuality: 1.0,
		LatencyMs:        0,
	}
	score := calc.ComputeScore(components)
	if score != 1.0 {
		t.Fatalf("expected score 1.0, got %f", score)
	}
}

func TestBSCalculatorZeroScore(t *testing.T) {
	calc, _ := NewBidScoreCalculator(DefaultBidScoreConfig())
	components := ScoreComponents{
		BidAmount:        0,
		ReputationScore:  0,
		InclusionQuality: 0,
		LatencyMs:        10000, // way above max
	}
	score := calc.ComputeScore(components)
	if score != 0.0 {
		t.Fatalf("expected score 0.0, got %f", score)
	}
}

func TestBSCalculatorPartialScore(t *testing.T) {
	calc, _ := NewBidScoreCalculator(DefaultBidScoreConfig())
	components := ScoreComponents{
		BidAmount:        50_000_000, // 50% of max
		ReputationScore:  50,
		InclusionQuality: 0.5,
		LatencyMs:        2500, // 50% of max
	}
	score := calc.ComputeScore(components)
	// All components are at 50%, so score should be ~0.5.
	if score < 0.45 || score > 0.55 {
		t.Fatalf("expected score ~0.5, got %f", score)
	}
}

func TestBSCalculatorHighLatencyPenalty(t *testing.T) {
	calc, _ := NewBidScoreCalculator(DefaultBidScoreConfig())
	fast := ScoreComponents{BidAmount: 50_000_000, ReputationScore: 80, InclusionQuality: 0.8, LatencyMs: 100}
	slow := ScoreComponents{BidAmount: 50_000_000, ReputationScore: 80, InclusionQuality: 0.8, LatencyMs: 4900}

	fastScore := calc.ComputeScore(fast)
	slowScore := calc.ComputeScore(slow)
	if fastScore <= slowScore {
		t.Fatalf("fast bid should score higher: fast=%f, slow=%f", fastScore, slowScore)
	}
}

// --- ReputationTracker tests ---

func TestBSReputationTrackerRegisterAndGet(t *testing.T) {
	rt := NewReputationTracker()
	addr := types.HexToAddress("0xaa")
	rt.Register(addr, 50.0)

	entry := rt.Get(addr)
	if entry == nil {
		t.Fatal("expected entry")
	}
	if entry.Score != 50.0 {
		t.Fatalf("expected score 50.0, got %f", entry.Score)
	}
	if rt.Count() != 1 {
		t.Fatalf("expected 1 builder, got %d", rt.Count())
	}
}

func TestBSReputationTrackerRecordBidAndReveal(t *testing.T) {
	rt := NewReputationTracker()
	addr := types.HexToAddress("0xaa")
	rt.Register(addr, 50.0)

	rt.RecordBid(addr)
	rt.RecordBid(addr)
	rt.RecordReveal(addr)

	entry := rt.Get(addr)
	if entry.TotalBids != 2 {
		t.Fatalf("expected 2 bids, got %d", entry.TotalBids)
	}
	if entry.SuccessfulReveals != 1 {
		t.Fatalf("expected 1 reveal, got %d", entry.SuccessfulReveals)
	}
	if entry.Reliability() != 0.5 {
		t.Fatalf("expected reliability 0.5, got %f", entry.Reliability())
	}
}

func TestBSReputationTrackerRecordFailure(t *testing.T) {
	rt := NewReputationTracker()
	addr := types.HexToAddress("0xaa")
	rt.Register(addr, 50.0)

	rt.RecordFailure(addr)
	entry := rt.Get(addr)
	if entry.Score != 45.0 {
		t.Fatalf("expected score 45.0, got %f", entry.Score)
	}

	// Score should not go below 0.
	for i := 0; i < 20; i++ {
		rt.RecordFailure(addr)
	}
	entry = rt.Get(addr)
	if entry.Score < 0 {
		t.Fatalf("score should not be negative: %f", entry.Score)
	}
}

func TestBSReputationTrackerNotFound(t *testing.T) {
	rt := NewReputationTracker()
	entry := rt.Get(types.HexToAddress("0xff"))
	if entry != nil {
		t.Fatal("expected nil for unknown builder")
	}
}

func TestBSReputationTrackerNewBuilderReliability(t *testing.T) {
	entry := &BuilderReputationEntry{}
	if entry.Reliability() != 1.0 {
		t.Fatalf("new builder should have 1.0 reliability, got %f", entry.Reliability())
	}
}

// --- BidRanker tests ---

func TestBSBidRankerRanking(t *testing.T) {
	calc, _ := NewBidScoreCalculator(DefaultBidScoreConfig())
	ranker := NewBidRanker(calc)

	bids := []RankedBid{
		{Bid: makeTestBidForScoring(1000), Score: 0.3, BidHash: types.HexToHash("0xcc")},
		{Bid: makeTestBidForScoring(5000), Score: 0.8, BidHash: types.HexToHash("0xaa")},
		{Bid: makeTestBidForScoring(3000), Score: 0.5, BidHash: types.HexToHash("0xbb")},
	}

	ranked := ranker.Rank(bids)
	if ranked[0].Score != 0.8 {
		t.Fatalf("expected highest score first, got %f", ranked[0].Score)
	}
	if ranked[2].Score != 0.3 {
		t.Fatalf("expected lowest score last, got %f", ranked[2].Score)
	}
}

func TestBSBidRankerTiebreak(t *testing.T) {
	calc, _ := NewBidScoreCalculator(DefaultBidScoreConfig())
	ranker := NewBidRanker(calc)

	bids := []RankedBid{
		{Bid: makeTestBidForScoring(1000), Score: 0.5, BidHash: types.HexToHash("0xcc")},
		{Bid: makeTestBidForScoring(1000), Score: 0.5, BidHash: types.HexToHash("0xaa")},
	}

	ranked := ranker.Rank(bids)
	// Smaller hash should be first.
	if ranked[0].BidHash != types.HexToHash("0xaa") {
		t.Fatal("expected smaller hash to win tiebreak")
	}
}

func TestBSBidRankerWinner(t *testing.T) {
	calc, _ := NewBidScoreCalculator(DefaultBidScoreConfig())
	ranker := NewBidRanker(calc)

	bids := []RankedBid{
		{Bid: makeTestBidForScoring(1000), Score: 0.3, BidHash: types.HexToHash("0xcc")},
		{Bid: makeTestBidForScoring(5000), Score: 0.9, BidHash: types.HexToHash("0xaa")},
	}

	winner, err := ranker.Winner(bids)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if winner.Score != 0.9 {
		t.Fatalf("expected winner score 0.9, got %f", winner.Score)
	}
}

func TestBSBidRankerEmptyBids(t *testing.T) {
	calc, _ := NewBidScoreCalculator(DefaultBidScoreConfig())
	ranker := NewBidRanker(calc)

	_, err := ranker.Winner(nil)
	if err == nil {
		t.Fatal("expected error for empty bids")
	}
}

// --- TiebreakerRule tests ---

func TestBSTiebreakerRule(t *testing.T) {
	tbr := NewTiebreakerRule()
	a := RankedBid{BidHash: types.HexToHash("0x01")}
	b := RankedBid{BidHash: types.HexToHash("0x02")}

	winner := tbr.Break(a, b)
	if winner.BidHash != a.BidHash {
		t.Fatal("expected smaller hash to win")
	}

	winner = tbr.Break(b, a)
	if winner.BidHash != a.BidHash {
		t.Fatal("expected smaller hash to win regardless of order")
	}
}

func TestBSTiebreakerRuleSameHash(t *testing.T) {
	tbr := NewTiebreakerRule()
	a := RankedBid{BidHash: types.HexToHash("0x01"), Score: 1.0}
	b := RankedBid{BidHash: types.HexToHash("0x01"), Score: 2.0}

	winner := tbr.Break(a, b)
	if winner.Score != 1.0 {
		t.Fatal("expected first arg to win on equal hash")
	}
}

// --- MinBidEnforcer tests ---

func TestBSMinBidEnforcerCheck(t *testing.T) {
	mbe := NewMinBidEnforcer(1000)

	bid := makeTestBidForScoring(500)
	err := mbe.Check(bid)
	if err == nil {
		t.Fatal("expected error for bid below minimum")
	}

	bid = makeTestBidForScoring(1000)
	err = mbe.Check(bid)
	if err != nil {
		t.Fatalf("unexpected error for bid at minimum: %v", err)
	}

	bid = makeTestBidForScoring(2000)
	err = mbe.Check(bid)
	if err != nil {
		t.Fatalf("unexpected error for bid above minimum: %v", err)
	}
}

func TestBSMinBidEnforcerNilBid(t *testing.T) {
	mbe := NewMinBidEnforcer(100)
	err := mbe.Check(nil)
	if err == nil {
		t.Fatal("expected error for nil bid")
	}
}

func TestBSMinBidEnforcerSetMinimum(t *testing.T) {
	mbe := NewMinBidEnforcer(100)
	if mbe.Minimum() != 100 {
		t.Fatalf("expected 100, got %d", mbe.Minimum())
	}

	mbe.SetMinimum(500)
	if mbe.Minimum() != 500 {
		t.Fatalf("expected 500, got %d", mbe.Minimum())
	}
}

func TestBSMinBidEnforcerFilterBids(t *testing.T) {
	mbe := NewMinBidEnforcer(1000)

	bids := []*BuilderBid{
		makeTestBidForScoring(500),
		makeTestBidForScoring(1500),
		makeTestBidForScoring(800),
		makeTestBidForScoring(2000),
		nil, // nil bid should be skipped
	}

	filtered := mbe.FilterBids(bids)
	if len(filtered) != 2 {
		t.Fatalf("expected 2 filtered bids, got %d", len(filtered))
	}
	if filtered[0].Value != 1500 {
		t.Fatalf("expected first filtered bid 1500, got %d", filtered[0].Value)
	}
	if filtered[1].Value != 2000 {
		t.Fatalf("expected second filtered bid 2000, got %d", filtered[1].Value)
	}
}

func TestBSMinBidEnforcerFilterEmpty(t *testing.T) {
	mbe := NewMinBidEnforcer(1000)
	filtered := mbe.FilterBids(nil)
	if len(filtered) != 0 {
		t.Fatalf("expected 0 filtered bids, got %d", len(filtered))
	}
}

// --- Integration test: full scoring and ranking pipeline ---

func TestBSFullScoringPipeline(t *testing.T) {
	// Create components.
	calc, _ := NewBidScoreCalculator(DefaultBidScoreConfig())
	ranker := NewBidRanker(calc)
	mbe := NewMinBidEnforcer(500)
	rt := NewReputationTracker()

	// Register builders.
	addr1 := types.HexToAddress("0x01")
	addr2 := types.HexToAddress("0x02")
	rt.Register(addr1, 80.0)
	rt.Register(addr2, 60.0)

	// Record some history.
	rt.RecordBid(addr1)
	rt.RecordBid(addr1)
	rt.RecordReveal(addr1)
	rt.RecordBid(addr2)
	rt.RecordReveal(addr2)

	// Create bids.
	bid1 := makeTestBidForScoring(50_000_000)
	bid2 := makeTestBidForScoring(40_000_000)

	// Filter by minimum.
	filtered := mbe.FilterBids([]*BuilderBid{bid1, bid2})
	if len(filtered) != 2 {
		t.Fatalf("expected 2 bids after filter, got %d", len(filtered))
	}

	// Score and rank.
	rep1 := rt.Get(addr1)
	rep2 := rt.Get(addr2)

	score1 := calc.ComputeScore(ScoreComponents{
		BidAmount:        bid1.Value,
		ReputationScore:  rep1.Score,
		InclusionQuality: 0.9,
		LatencyMs:        200,
	})
	score2 := calc.ComputeScore(ScoreComponents{
		BidAmount:        bid2.Value,
		ReputationScore:  rep2.Score,
		InclusionQuality: 0.7,
		LatencyMs:        1000,
	})

	rankedBids := []RankedBid{
		{Bid: bid1, BuilderAddr: addr1, Score: score1, BidHash: bid1.BidHash()},
		{Bid: bid2, BuilderAddr: addr2, Score: score2, BidHash: bid2.BidHash()},
	}

	winner, err := ranker.Winner(rankedBids)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Builder 1 has higher bid and reputation, should win.
	if winner.BuilderAddr != addr1 {
		t.Fatal("expected builder 1 to win")
	}
}
