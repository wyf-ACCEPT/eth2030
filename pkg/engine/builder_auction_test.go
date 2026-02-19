package engine

import (
	"errors"
	"sync"
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

// helper to create a builder ID from a byte.
func auctionBuilderID(b byte) types.Hash {
	var h types.Hash
	h[0] = b
	return h
}

// helper to create a valid AuctionBid for testing.
func validAuctionBid(builderByte byte, slot, value, gasLimit uint64) *AuctionBid {
	return &AuctionBid{
		BuilderID: auctionBuilderID(builderByte),
		Slot:      slot,
		Value:     value,
		GasLimit:  gasLimit,
		Payload:   []byte("test-payload"),
		Signature: []byte("test-sig"),
	}
}

func TestAuctionNew(t *testing.T) {
	cfg := DefaultAuctionConfig()
	ba := NewBuilderAuction(cfg)
	if ba == nil {
		t.Fatal("NewBuilderAuction returned nil")
	}
	if ba.config.MinBid != cfg.MinBid {
		t.Fatalf("config.MinBid mismatch: got %d, want %d", ba.config.MinBid, cfg.MinBid)
	}
}

func TestAuctionRegisterBuilder(t *testing.T) {
	ba := NewBuilderAuction(DefaultAuctionConfig())
	id := auctionBuilderID(1)

	if err := ba.RegisterBuilder(id, 200); err != nil {
		t.Fatalf("RegisterBuilder failed: %v", err)
	}

	// Duplicate registration.
	err := ba.RegisterBuilder(id, 200)
	if !errors.Is(err, ErrAuctionBuilderExists) {
		t.Fatalf("expected ErrAuctionBuilderExists, got: %v", err)
	}

	// Insufficient stake.
	id2 := auctionBuilderID(2)
	err = ba.RegisterBuilder(id2, 10) // MinStake is 100
	if !errors.Is(err, ErrAuctionInsufficientStk) {
		t.Fatalf("expected ErrAuctionInsufficientStk, got: %v", err)
	}

	// Zero builder ID.
	err = ba.RegisterBuilder(types.Hash{}, 200)
	if !errors.Is(err, ErrAuctionBidEmptyBuilder) {
		t.Fatalf("expected ErrAuctionBidEmptyBuilder, got: %v", err)
	}
}

func TestAuctionSlashBuilder(t *testing.T) {
	ba := NewBuilderAuction(DefaultAuctionConfig())
	id := auctionBuilderID(1)

	// Slash unregistered builder.
	err := ba.SlashBuilder(id, "test")
	if !errors.Is(err, ErrAuctionBuilderNotReg) {
		t.Fatalf("expected ErrAuctionBuilderNotReg, got: %v", err)
	}

	// Register, then slash.
	if err := ba.RegisterBuilder(id, 200); err != nil {
		t.Fatal(err)
	}
	if err := ba.SlashBuilder(id, "equivocation"); err != nil {
		t.Fatalf("SlashBuilder failed: %v", err)
	}

	// Slashed builder cannot submit bids.
	bid := validAuctionBid(1, 10, 50, 30_000_000)
	err = ba.SubmitBid(bid)
	if !errors.Is(err, ErrAuctionBuilderSlashed) {
		t.Fatalf("expected ErrAuctionBuilderSlashed, got: %v", err)
	}

	// Slash zero ID.
	err = ba.SlashBuilder(types.Hash{}, "zero")
	if !errors.Is(err, ErrAuctionSlashZeroID) {
		t.Fatalf("expected ErrAuctionSlashZeroID, got: %v", err)
	}
}

func TestAuctionValidateBid(t *testing.T) {
	ba := NewBuilderAuction(AuctionConfig{
		MinBid:   10,
		MaxBid:   1000,
		MinStake: 100,
	})

	tests := []struct {
		name string
		bid  *AuctionBid
		want error
	}{
		{
			name: "nil bid",
			bid:  nil,
			want: ErrAuctionNilBid,
		},
		{
			name: "zero builder ID",
			bid: &AuctionBid{
				Slot: 1, Value: 50, GasLimit: 30_000_000,
				Payload: []byte("p"), Signature: []byte("s"),
			},
			want: ErrAuctionBidEmptyBuilder,
		},
		{
			name: "zero slot",
			bid: &AuctionBid{
				BuilderID: auctionBuilderID(1), Value: 50, GasLimit: 30_000_000,
				Payload: []byte("p"), Signature: []byte("s"),
			},
			want: ErrAuctionBidZeroSlot,
		},
		{
			name: "bid below minimum",
			bid: &AuctionBid{
				BuilderID: auctionBuilderID(1), Slot: 1, Value: 5, GasLimit: 30_000_000,
				Payload: []byte("p"), Signature: []byte("s"),
			},
			want: ErrAuctionBidTooLow,
		},
		{
			name: "bid above maximum",
			bid: &AuctionBid{
				BuilderID: auctionBuilderID(1), Slot: 1, Value: 2000, GasLimit: 30_000_000,
				Payload: []byte("p"), Signature: []byte("s"),
			},
			want: ErrAuctionBidTooHigh,
		},
		{
			name: "zero gas limit",
			bid: &AuctionBid{
				BuilderID: auctionBuilderID(1), Slot: 1, Value: 50,
				Payload: []byte("p"), Signature: []byte("s"),
			},
			want: ErrAuctionBidZeroGas,
		},
		{
			name: "empty payload",
			bid: &AuctionBid{
				BuilderID: auctionBuilderID(1), Slot: 1, Value: 50, GasLimit: 30_000_000,
				Signature: []byte("s"),
			},
			want: ErrAuctionBidNoPayload,
		},
		{
			name: "empty signature",
			bid: &AuctionBid{
				BuilderID: auctionBuilderID(1), Slot: 1, Value: 50, GasLimit: 30_000_000,
				Payload: []byte("p"),
			},
			want: ErrAuctionBidNoSignature,
		},
		{
			name: "valid bid",
			bid: &AuctionBid{
				BuilderID: auctionBuilderID(1), Slot: 1, Value: 50, GasLimit: 30_000_000,
				Payload: []byte("p"), Signature: []byte("s"),
			},
			want: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ba.ValidateBid(tc.bid)
			if tc.want == nil {
				if err != nil {
					t.Fatalf("expected nil error, got: %v", err)
				}
				return
			}
			if !errors.Is(err, tc.want) {
				t.Fatalf("expected %v, got: %v", tc.want, err)
			}
		})
	}
}

func TestAuctionSubmitBid(t *testing.T) {
	ba := NewBuilderAuction(DefaultAuctionConfig())

	// Register builder.
	id := auctionBuilderID(1)
	if err := ba.RegisterBuilder(id, 200); err != nil {
		t.Fatal(err)
	}

	// Submit valid bid.
	bid := validAuctionBid(1, 10, 50, 30_000_000)
	if err := ba.SubmitBid(bid); err != nil {
		t.Fatalf("SubmitBid failed: %v", err)
	}

	// Unregistered builder.
	bid2 := validAuctionBid(99, 10, 50, 30_000_000)
	err := ba.SubmitBid(bid2)
	if !errors.Is(err, ErrAuctionBuilderNotReg) {
		t.Fatalf("expected ErrAuctionBuilderNotReg, got: %v", err)
	}
}

func TestAuctionGetWinningBid(t *testing.T) {
	ba := NewBuilderAuction(DefaultAuctionConfig())

	// No bids.
	_, err := ba.GetWinningBid(10)
	if !errors.Is(err, ErrAuctionNoBids) {
		t.Fatalf("expected ErrAuctionNoBids, got: %v", err)
	}

	// Register two builders, submit bids.
	ba.RegisterBuilder(auctionBuilderID(1), 200)
	ba.RegisterBuilder(auctionBuilderID(2), 200)

	ba.SubmitBid(validAuctionBid(1, 10, 50, 30_000_000))
	ba.SubmitBid(validAuctionBid(2, 10, 80, 30_000_000))
	ba.SubmitBid(validAuctionBid(1, 10, 60, 30_000_000))

	winner, err := ba.GetWinningBid(10)
	if err != nil {
		t.Fatalf("GetWinningBid failed: %v", err)
	}
	if winner.Value != 80 {
		t.Fatalf("expected winning value 80, got %d", winner.Value)
	}
	if winner.BuilderID != auctionBuilderID(2) {
		t.Fatalf("expected builder 2 to win")
	}
}

func TestAuctionRunAuction(t *testing.T) {
	ba := NewBuilderAuction(DefaultAuctionConfig())

	// No bids.
	_, err := ba.RunAuction(5)
	if !errors.Is(err, ErrAuctionNoBids) {
		t.Fatalf("expected ErrAuctionNoBids, got: %v", err)
	}

	// Register three builders.
	ba.RegisterBuilder(auctionBuilderID(1), 200)
	ba.RegisterBuilder(auctionBuilderID(2), 200)
	ba.RegisterBuilder(auctionBuilderID(3), 200)

	// Submit bids: 100, 200, 150.
	ba.SubmitBid(validAuctionBid(1, 5, 100, 30_000_000))
	ba.SubmitBid(validAuctionBid(2, 5, 200, 30_000_000))
	ba.SubmitBid(validAuctionBid(3, 5, 150, 30_000_000))

	result, err := ba.RunAuction(5)
	if err != nil {
		t.Fatalf("RunAuction failed: %v", err)
	}
	if result.Slot != 5 {
		t.Fatalf("expected slot 5, got %d", result.Slot)
	}
	if result.WinnerID != auctionBuilderID(2) {
		t.Fatal("expected builder 2 to win")
	}
	if result.WinningValue != 200 {
		t.Fatalf("expected winning value 200, got %d", result.WinningValue)
	}
	if result.SecondPrice != 150 {
		t.Fatalf("expected second price 150, got %d", result.SecondPrice)
	}
	if result.TotalBids != 3 {
		t.Fatalf("expected 3 total bids, got %d", result.TotalBids)
	}
}

func TestAuctionRunSingleBid(t *testing.T) {
	ba := NewBuilderAuction(DefaultAuctionConfig())
	ba.RegisterBuilder(auctionBuilderID(1), 200)
	ba.SubmitBid(validAuctionBid(1, 7, 100, 30_000_000))

	result, err := ba.RunAuction(7)
	if err != nil {
		t.Fatalf("RunAuction failed: %v", err)
	}
	// With a single bid, second price equals the winning bid.
	if result.SecondPrice != 100 {
		t.Fatalf("expected second price 100 for single bid, got %d", result.SecondPrice)
	}
	if result.TotalBids != 1 {
		t.Fatalf("expected 1 total bid, got %d", result.TotalBids)
	}
}

func TestAuctionGetBidHistory(t *testing.T) {
	ba := NewBuilderAuction(DefaultAuctionConfig())
	ba.RegisterBuilder(auctionBuilderID(1), 200)
	ba.RegisterBuilder(auctionBuilderID(2), 200)

	ba.SubmitBid(validAuctionBid(1, 3, 40, 30_000_000))
	ba.SubmitBid(validAuctionBid(2, 3, 60, 30_000_000))

	history := ba.GetBidHistory(3)
	if len(history) != 2 {
		t.Fatalf("expected 2 bids in history, got %d", len(history))
	}

	// Empty slot.
	empty := ba.GetBidHistory(999)
	if len(empty) != 0 {
		t.Fatalf("expected 0 bids for empty slot, got %d", len(empty))
	}
}

func TestAuctionBidHash(t *testing.T) {
	bid1 := validAuctionBid(1, 10, 50, 30_000_000)
	bid2 := validAuctionBid(1, 10, 50, 30_000_000)
	bid3 := validAuctionBid(2, 10, 50, 30_000_000)

	h1 := bid1.Hash()
	h2 := bid2.Hash()
	h3 := bid3.Hash()

	if h1 != h2 {
		t.Fatal("identical bids should produce the same hash")
	}
	if h1 == h3 {
		t.Fatal("different builder IDs should produce different hashes")
	}
	if h1.IsZero() {
		t.Fatal("bid hash should not be zero")
	}
}

func TestAuctionMaxBidUnlimited(t *testing.T) {
	// MaxBid=0 means unlimited.
	ba := NewBuilderAuction(AuctionConfig{
		MinBid:   1,
		MaxBid:   0,
		MinStake: 100,
	})
	ba.RegisterBuilder(auctionBuilderID(1), 200)

	bid := validAuctionBid(1, 1, 999_999_999, 30_000_000)
	if err := ba.SubmitBid(bid); err != nil {
		t.Fatalf("unlimited MaxBid should accept any value: %v", err)
	}
}

func TestAuctionConcurrentBidSubmission(t *testing.T) {
	ba := NewBuilderAuction(DefaultAuctionConfig())

	// Register 10 builders.
	for i := byte(1); i <= 10; i++ {
		if err := ba.RegisterBuilder(auctionBuilderID(i), 200); err != nil {
			t.Fatal(err)
		}
	}

	var wg sync.WaitGroup
	errCh := make(chan error, 100)

	// Each builder submits 10 bids concurrently.
	for i := byte(1); i <= 10; i++ {
		for j := uint64(1); j <= 10; j++ {
			wg.Add(1)
			go func(builder byte, value uint64) {
				defer wg.Done()
				bid := validAuctionBid(builder, 20, value, 30_000_000)
				if err := ba.SubmitBid(bid); err != nil {
					errCh <- err
				}
			}(i, j*10)
		}
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Fatalf("concurrent SubmitBid failed: %v", err)
	}

	history := ba.GetBidHistory(20)
	if len(history) != 100 {
		t.Fatalf("expected 100 bids, got %d", len(history))
	}
}

func TestAuctionConcurrentRegisterAndBid(t *testing.T) {
	ba := NewBuilderAuction(DefaultAuctionConfig())

	var wg sync.WaitGroup

	// Concurrently register builders.
	for i := byte(1); i <= 20; i++ {
		wg.Add(1)
		go func(b byte) {
			defer wg.Done()
			ba.RegisterBuilder(auctionBuilderID(b), 200)
		}(i)
	}
	wg.Wait()

	// Concurrently submit bids and read results.
	for i := byte(1); i <= 20; i++ {
		wg.Add(2)
		go func(b byte) {
			defer wg.Done()
			ba.SubmitBid(validAuctionBid(b, 30, uint64(b)*10, 30_000_000))
		}(i)
		go func(b byte) {
			defer wg.Done()
			ba.GetWinningBid(30)
		}(i)
	}
	wg.Wait()

	// Run auction concurrently.
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ba.RunAuction(30)
		}()
	}
	wg.Wait()
}

func TestAuctionDefaultConfig(t *testing.T) {
	cfg := DefaultAuctionConfig()
	if cfg.MinBid != 1 {
		t.Fatalf("expected MinBid 1, got %d", cfg.MinBid)
	}
	if cfg.MaxBid != 0 {
		t.Fatalf("expected MaxBid 0 (unlimited), got %d", cfg.MaxBid)
	}
	if cfg.MinStake != 100 {
		t.Fatalf("expected MinStake 100, got %d", cfg.MinStake)
	}
}

func TestAuctionMultipleSlots(t *testing.T) {
	ba := NewBuilderAuction(DefaultAuctionConfig())
	ba.RegisterBuilder(auctionBuilderID(1), 200)
	ba.RegisterBuilder(auctionBuilderID(2), 200)

	// Bids for different slots.
	ba.SubmitBid(validAuctionBid(1, 10, 100, 30_000_000))
	ba.SubmitBid(validAuctionBid(2, 10, 200, 30_000_000))
	ba.SubmitBid(validAuctionBid(1, 20, 300, 30_000_000))
	ba.SubmitBid(validAuctionBid(2, 20, 150, 30_000_000))

	// Slot 10: builder 2 wins.
	r1, err := ba.RunAuction(10)
	if err != nil {
		t.Fatal(err)
	}
	if r1.WinnerID != auctionBuilderID(2) {
		t.Fatal("expected builder 2 to win slot 10")
	}
	if r1.SecondPrice != 100 {
		t.Fatalf("expected second price 100, got %d", r1.SecondPrice)
	}

	// Slot 20: builder 1 wins.
	r2, err := ba.RunAuction(20)
	if err != nil {
		t.Fatal(err)
	}
	if r2.WinnerID != auctionBuilderID(1) {
		t.Fatal("expected builder 1 to win slot 20")
	}
	if r2.SecondPrice != 150 {
		t.Fatalf("expected second price 150, got %d", r2.SecondPrice)
	}
}
