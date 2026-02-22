package engine

import (
	"math/big"
	"sync"
	"testing"
	"time"

	"github.com/eth2030/eth2030/core/types"
)

func TestDefaultBuilderConfig(t *testing.T) {
	cfg := DefaultBuilderConfig()
	if cfg.MaxBuilders != 32 {
		t.Errorf("MaxBuilders = %d, want 32", cfg.MaxBuilders)
	}
	if cfg.BuilderTimeout != 2*time.Second {
		t.Errorf("BuilderTimeout = %v, want 2s", cfg.BuilderTimeout)
	}
	if cfg.MinBid.Sign() != 0 {
		t.Errorf("MinBid = %s, want 0", cfg.MinBid.String())
	}
	if cfg.SlotAuctionDuration != 4*time.Second {
		t.Errorf("SlotAuctionDuration = %v, want 4s", cfg.SlotAuctionDuration)
	}
}

func TestRegisterBuilder(t *testing.T) {
	bn := NewBuilderNetwork(DefaultBuilderConfig())

	id := types.HexToHash("0x01")
	addr := types.HexToAddress("0xaa")
	stake := big.NewInt(1000)

	err := bn.RegisterBuilder(id, addr, stake)
	if err != nil {
		t.Fatalf("RegisterBuilder: %v", err)
	}
	if bn.ActiveBuilders() != 1 {
		t.Errorf("ActiveBuilders = %d, want 1", bn.ActiveBuilders())
	}

	// Verify builder fields.
	bn.mu.RLock()
	b := bn.builders[id]
	bn.mu.RUnlock()

	if b.ID != id {
		t.Errorf("builder ID mismatch")
	}
	if b.Address != addr {
		t.Errorf("builder Address mismatch")
	}
	if b.Stake.Cmp(stake) != 0 {
		t.Errorf("builder Stake = %s, want %s", b.Stake, stake)
	}
	if !b.Active {
		t.Error("builder should be active")
	}
}

func TestRegisterBuilderDuplicate(t *testing.T) {
	bn := NewBuilderNetwork(DefaultBuilderConfig())
	id := types.HexToHash("0x01")
	bn.RegisterBuilder(id, types.Address{}, big.NewInt(100))

	err := bn.RegisterBuilder(id, types.Address{}, big.NewInt(200))
	if err != ErrDistBuilderExists {
		t.Errorf("duplicate register: got %v, want %v", err, ErrDistBuilderExists)
	}
}

func TestRegisterBuilderMaxReached(t *testing.T) {
	cfg := DefaultBuilderConfig()
	cfg.MaxBuilders = 2
	bn := NewBuilderNetwork(cfg)

	bn.RegisterBuilder(types.HexToHash("0x01"), types.Address{}, big.NewInt(100))
	bn.RegisterBuilder(types.HexToHash("0x02"), types.Address{}, big.NewInt(100))

	err := bn.RegisterBuilder(types.HexToHash("0x03"), types.Address{}, big.NewInt(100))
	if err != ErrDistBuilderMaxReached {
		t.Errorf("max reached: got %v, want %v", err, ErrDistBuilderMaxReached)
	}
}

func TestUnregisterBuilder(t *testing.T) {
	bn := NewBuilderNetwork(DefaultBuilderConfig())
	id := types.HexToHash("0x01")
	bn.RegisterBuilder(id, types.Address{}, big.NewInt(100))

	err := bn.UnregisterBuilder(id)
	if err != nil {
		t.Fatalf("UnregisterBuilder: %v", err)
	}
	if bn.ActiveBuilders() != 0 {
		t.Errorf("ActiveBuilders = %d, want 0", bn.ActiveBuilders())
	}
}

func TestUnregisterBuilderNotFound(t *testing.T) {
	bn := NewBuilderNetwork(DefaultBuilderConfig())
	err := bn.UnregisterBuilder(types.HexToHash("0x99"))
	if err != ErrDistBuilderNotFound {
		t.Errorf("unregister unknown: got %v, want %v", err, ErrDistBuilderNotFound)
	}
}

func TestDistSubmitBid(t *testing.T) {
	bn := NewBuilderNetwork(DefaultBuilderConfig())
	id := types.HexToHash("0x01")
	bn.RegisterBuilder(id, types.Address{}, big.NewInt(100))

	bid := &BuilderBid{
		BuilderID: id,
		Slot:      10,
		BlockHash: types.HexToHash("0xaa"),
		Value:     big.NewInt(500),
		Payload:   []byte("block data"),
		Timestamp: time.Now(),
	}

	err := bn.SubmitBid(bid)
	if err != nil {
		t.Fatalf("SubmitBid: %v", err)
	}

	// Check the bid was recorded.
	bn.mu.RLock()
	bids := bn.bids[10]
	bn.mu.RUnlock()
	if len(bids) != 1 {
		t.Fatalf("expected 1 bid for slot 10, got %d", len(bids))
	}
}

func TestDistSubmitBidUnregistered(t *testing.T) {
	bn := NewBuilderNetwork(DefaultBuilderConfig())

	bid := &BuilderBid{
		BuilderID: types.HexToHash("0x99"),
		Slot:      10,
		Value:     big.NewInt(500),
	}

	err := bn.SubmitBid(bid)
	if err != ErrDistBidBuilderNotFound {
		t.Errorf("bid from unregistered: got %v, want %v", err, ErrDistBidBuilderNotFound)
	}
}

func TestDistSubmitBidZeroValue(t *testing.T) {
	bn := NewBuilderNetwork(DefaultBuilderConfig())
	id := types.HexToHash("0x01")
	bn.RegisterBuilder(id, types.Address{}, big.NewInt(100))

	bid := &BuilderBid{
		BuilderID: id,
		Slot:      10,
		Value:     big.NewInt(0),
	}
	err := bn.SubmitBid(bid)
	if err != ErrDistBidZeroValue {
		t.Errorf("zero value bid: got %v, want %v", err, ErrDistBidZeroValue)
	}
}

func TestSubmitBidNilValue(t *testing.T) {
	bn := NewBuilderNetwork(DefaultBuilderConfig())
	id := types.HexToHash("0x01")
	bn.RegisterBuilder(id, types.Address{}, big.NewInt(100))

	bid := &BuilderBid{
		BuilderID: id,
		Slot:      10,
		Value:     nil,
	}
	err := bn.SubmitBid(bid)
	if err != ErrDistBidZeroValue {
		t.Errorf("nil value bid: got %v, want %v", err, ErrDistBidZeroValue)
	}
}

func TestSubmitBidNil(t *testing.T) {
	bn := NewBuilderNetwork(DefaultBuilderConfig())
	err := bn.SubmitBid(nil)
	if err != ErrDistBidInvalid {
		t.Errorf("nil bid: got %v, want %v", err, ErrDistBidInvalid)
	}
}

func TestDistSubmitBidInactiveBuilder(t *testing.T) {
	bn := NewBuilderNetwork(DefaultBuilderConfig())
	id := types.HexToHash("0x01")
	bn.RegisterBuilder(id, types.Address{}, big.NewInt(100))

	// Deactivate the builder directly.
	bn.mu.Lock()
	bn.builders[id].Active = false
	bn.mu.Unlock()

	bid := &BuilderBid{
		BuilderID: id,
		Slot:      10,
		Value:     big.NewInt(500),
	}
	err := bn.SubmitBid(bid)
	if err != ErrDistBuilderInactive {
		t.Errorf("inactive builder bid: got %v, want %v", err, ErrDistBuilderInactive)
	}
}

func TestGetWinningBid(t *testing.T) {
	bn := NewBuilderNetwork(DefaultBuilderConfig())

	id1 := types.HexToHash("0x01")
	id2 := types.HexToHash("0x02")
	id3 := types.HexToHash("0x03")
	bn.RegisterBuilder(id1, types.Address{}, big.NewInt(100))
	bn.RegisterBuilder(id2, types.Address{}, big.NewInt(100))
	bn.RegisterBuilder(id3, types.Address{}, big.NewInt(100))

	slot := uint64(42)

	bn.SubmitBid(&BuilderBid{BuilderID: id1, Slot: slot, Value: big.NewInt(100), Timestamp: time.Now()})
	bn.SubmitBid(&BuilderBid{BuilderID: id2, Slot: slot, Value: big.NewInt(300), Timestamp: time.Now()})
	bn.SubmitBid(&BuilderBid{BuilderID: id3, Slot: slot, Value: big.NewInt(200), Timestamp: time.Now()})

	winner := bn.GetWinningBid(slot)
	if winner == nil {
		t.Fatal("GetWinningBid returned nil")
	}
	if winner.Value.Cmp(big.NewInt(300)) != 0 {
		t.Errorf("winning bid value = %s, want 300", winner.Value)
	}
	if winner.BuilderID != id2 {
		t.Errorf("winning builder = %s, want %s", winner.BuilderID.Hex(), id2.Hex())
	}
}

func TestGetWinningBidEmpty(t *testing.T) {
	bn := NewBuilderNetwork(DefaultBuilderConfig())
	winner := bn.GetWinningBid(999)
	if winner != nil {
		t.Error("GetWinningBid should return nil for slot with no bids")
	}
}

func TestActiveBuilders(t *testing.T) {
	bn := NewBuilderNetwork(DefaultBuilderConfig())
	if bn.ActiveBuilders() != 0 {
		t.Errorf("initial ActiveBuilders = %d, want 0", bn.ActiveBuilders())
	}

	bn.RegisterBuilder(types.HexToHash("0x01"), types.Address{}, big.NewInt(100))
	bn.RegisterBuilder(types.HexToHash("0x02"), types.Address{}, big.NewInt(100))
	if bn.ActiveBuilders() != 2 {
		t.Errorf("ActiveBuilders = %d, want 2", bn.ActiveBuilders())
	}

	bn.UnregisterBuilder(types.HexToHash("0x01"))
	if bn.ActiveBuilders() != 1 {
		t.Errorf("ActiveBuilders after unregister = %d, want 1", bn.ActiveBuilders())
	}
}

func TestPruneStaleBids(t *testing.T) {
	bn := NewBuilderNetwork(DefaultBuilderConfig())
	id := types.HexToHash("0x01")
	bn.RegisterBuilder(id, types.Address{}, big.NewInt(100))

	// Add bids for slots 1, 5, 10, 15.
	for _, slot := range []uint64{1, 5, 10, 15} {
		bn.SubmitBid(&BuilderBid{
			BuilderID: id,
			Slot:      slot,
			Value:     big.NewInt(100),
			Timestamp: time.Now(),
		})
	}

	// Prune bids before slot 10.
	bn.PruneStaleBids(10)

	bn.mu.RLock()
	defer bn.mu.RUnlock()

	// Slots 1 and 5 should be pruned.
	if _, ok := bn.bids[1]; ok {
		t.Error("slot 1 should be pruned")
	}
	if _, ok := bn.bids[5]; ok {
		t.Error("slot 5 should be pruned")
	}
	// Slots 10 and 15 should remain.
	if _, ok := bn.bids[10]; !ok {
		t.Error("slot 10 should remain")
	}
	if _, ok := bn.bids[15]; !ok {
		t.Error("slot 15 should remain")
	}
}

func TestConcurrentBidSubmission(t *testing.T) {
	bn := NewBuilderNetwork(DefaultBuilderConfig())

	// Register several builders.
	numBuilders := 10
	ids := make([]types.Hash, numBuilders)
	for i := 0; i < numBuilders; i++ {
		ids[i] = types.BytesToHash([]byte{byte(i + 1)})
		bn.RegisterBuilder(ids[i], types.Address{}, big.NewInt(100))
	}

	slot := uint64(77)
	var wg sync.WaitGroup
	errCh := make(chan error, numBuilders*5)

	// Submit multiple bids concurrently.
	for i := 0; i < numBuilders; i++ {
		for j := 0; j < 5; j++ {
			wg.Add(1)
			go func(builderIdx, bidIdx int) {
				defer wg.Done()
				bid := &BuilderBid{
					BuilderID: ids[builderIdx],
					Slot:      slot,
					Value:     big.NewInt(int64(bidIdx*100 + builderIdx + 1)),
					Payload:   []byte("concurrent payload"),
					Timestamp: time.Now(),
				}
				if err := bn.SubmitBid(bid); err != nil {
					errCh <- err
				}
			}(i, j)
		}
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Errorf("concurrent bid error: %v", err)
	}

	// All bids should be recorded.
	bn.mu.RLock()
	bids := bn.bids[slot]
	bn.mu.RUnlock()

	expected := numBuilders * 5
	if len(bids) != expected {
		t.Errorf("total bids = %d, want %d", len(bids), expected)
	}

	// Winning bid should be the highest value.
	winner := bn.GetWinningBid(slot)
	if winner == nil {
		t.Fatal("GetWinningBid returned nil after concurrent submission")
	}
}

func TestNewBuilderNetworkNilConfig(t *testing.T) {
	bn := NewBuilderNetwork(nil)
	if bn.config.MaxBuilders != 32 {
		t.Errorf("nil config: MaxBuilders = %d, want 32", bn.config.MaxBuilders)
	}
}
