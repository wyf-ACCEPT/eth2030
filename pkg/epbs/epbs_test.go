package epbs

import (
	"encoding/json"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

// --- Type Tests ---

func TestPayloadStatusConstants(t *testing.T) {
	if PayloadAbsent != 0 {
		t.Errorf("PayloadAbsent = %d, want 0", PayloadAbsent)
	}
	if PayloadPresent != 1 {
		t.Errorf("PayloadPresent = %d, want 1", PayloadPresent)
	}
	if PayloadWithheld != 2 {
		t.Errorf("PayloadWithheld = %d, want 2", PayloadWithheld)
	}
}

func TestPTCSize(t *testing.T) {
	if PTC_SIZE != 512 {
		t.Errorf("PTC_SIZE = %d, want 512", PTC_SIZE)
	}
}

func TestMaxPayloadAttestations(t *testing.T) {
	if MAX_PAYLOAD_ATTESTATIONS != 4 {
		t.Errorf("MAX_PAYLOAD_ATTESTATIONS = %d, want 4", MAX_PAYLOAD_ATTESTATIONS)
	}
}

func TestIsPayloadStatusValid(t *testing.T) {
	tests := []struct {
		status uint8
		valid  bool
	}{
		{PayloadAbsent, true},
		{PayloadPresent, true},
		{PayloadWithheld, true},
		{3, false},
		{255, false},
	}
	for _, tt := range tests {
		if got := IsPayloadStatusValid(tt.status); got != tt.valid {
			t.Errorf("IsPayloadStatusValid(%d) = %v, want %v", tt.status, got, tt.valid)
		}
	}
}

func TestBidHash(t *testing.T) {
	bid := BuilderBid{
		ParentBlockHash: types.HexToHash("0xaabb000000000000000000000000000000000000000000000000000000000000"),
		BlockHash:       types.HexToHash("0xccdd000000000000000000000000000000000000000000000000000000000000"),
		Slot:            100,
		Value:           1000,
		GasLimit:        30_000_000,
		BuilderIndex:    1,
	}

	hash1 := bid.BidHash()
	hash2 := bid.BidHash()

	if hash1 != hash2 {
		t.Error("BidHash is not deterministic")
	}

	// Different value -> different hash.
	bid2 := bid
	bid2.Value = 2000
	hash3 := bid2.BidHash()
	if hash1 == hash3 {
		t.Error("different bids should produce different hashes")
	}

	// Different slot -> different hash.
	bid3 := bid
	bid3.Slot = 200
	hash4 := bid3.BidHash()
	if hash1 == hash4 {
		t.Error("different slot should produce different hash")
	}
}

func TestBuilderBidJSON(t *testing.T) {
	bid := SignedBuilderBid{
		Message: BuilderBid{
			ParentBlockHash: types.HexToHash("0x1111"),
			BlockHash:       types.HexToHash("0x2222"),
			Slot:            100,
			Value:           5000,
			GasLimit:        30_000_000,
			BuilderIndex:    3,
			FeeRecipient:    types.HexToAddress("0xdead"),
		},
	}

	data, err := json.Marshal(bid)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var decoded SignedBuilderBid
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if decoded.Message.Slot != 100 {
		t.Errorf("Slot = %d, want 100", decoded.Message.Slot)
	}
	if decoded.Message.Value != 5000 {
		t.Errorf("Value = %d, want 5000", decoded.Message.Value)
	}
	if decoded.Message.BuilderIndex != 3 {
		t.Errorf("BuilderIndex = %d, want 3", decoded.Message.BuilderIndex)
	}
}

func TestPayloadEnvelopeJSON(t *testing.T) {
	env := PayloadEnvelope{
		PayloadRoot:     types.HexToHash("0xabcd"),
		BuilderIndex:    7,
		BeaconBlockRoot: types.HexToHash("0xbeef"),
		Slot:            200,
		StateRoot:       types.HexToHash("0xcafe"),
	}

	data, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var decoded PayloadEnvelope
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if decoded.BuilderIndex != 7 {
		t.Errorf("BuilderIndex = %d, want 7", decoded.BuilderIndex)
	}
	if decoded.Slot != 200 {
		t.Errorf("Slot = %d, want 200", decoded.Slot)
	}
	if decoded.PayloadRoot != env.PayloadRoot {
		t.Error("PayloadRoot mismatch")
	}
}

func TestPayloadAttestationDataJSON(t *testing.T) {
	data := PayloadAttestationData{
		BeaconBlockRoot: types.HexToHash("0x1234"),
		Slot:            42,
		PayloadStatus:   PayloadPresent,
	}

	encoded, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var decoded PayloadAttestationData
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if decoded.Slot != 42 {
		t.Errorf("Slot = %d, want 42", decoded.Slot)
	}
	if decoded.PayloadStatus != PayloadPresent {
		t.Errorf("PayloadStatus = %d, want PRESENT(%d)", decoded.PayloadStatus, PayloadPresent)
	}
}

// --- Validation Tests ---

func newValidSignedBid() *SignedBuilderBid {
	return &SignedBuilderBid{
		Message: BuilderBid{
			ParentBlockHash: types.HexToHash("0x1111111111111111111111111111111111111111111111111111111111111111"),
			BlockHash:       types.HexToHash("0x2222222222222222222222222222222222222222222222222222222222222222"),
			Slot:            100,
			Value:           5000,
			GasLimit:        30_000_000,
			BuilderIndex:    1,
			FeeRecipient:    types.HexToAddress("0xdead"),
		},
	}
}

func TestValidateBuilderBidValid(t *testing.T) {
	if err := ValidateBuilderBid(newValidSignedBid()); err != nil {
		t.Errorf("valid bid: %v", err)
	}
}

func TestValidateBuilderBidEmptyBlockHash(t *testing.T) {
	bid := newValidSignedBid()
	bid.Message.BlockHash = types.Hash{}
	if err := ValidateBuilderBid(bid); err != ErrEmptyBlockHash {
		t.Errorf("empty block hash: got %v, want ErrEmptyBlockHash", err)
	}
}

func TestValidateBuilderBidEmptyParentHash(t *testing.T) {
	bid := newValidSignedBid()
	bid.Message.ParentBlockHash = types.Hash{}
	if err := ValidateBuilderBid(bid); err != ErrEmptyParentBlockHash {
		t.Errorf("empty parent hash: got %v, want ErrEmptyParentBlockHash", err)
	}
}

func TestValidateBuilderBidZeroValue(t *testing.T) {
	bid := newValidSignedBid()
	bid.Message.Value = 0
	if err := ValidateBuilderBid(bid); err != ErrZeroBidValue {
		t.Errorf("zero value: got %v, want ErrZeroBidValue", err)
	}
}

func TestValidateBuilderBidZeroSlot(t *testing.T) {
	bid := newValidSignedBid()
	bid.Message.Slot = 0
	if err := ValidateBuilderBid(bid); err != ErrZeroSlot {
		t.Errorf("zero slot: got %v, want ErrZeroSlot", err)
	}
}

func TestValidatePayloadEnvelopeValid(t *testing.T) {
	env := &PayloadEnvelope{
		PayloadRoot:     types.HexToHash("0xabcd"),
		BeaconBlockRoot: types.HexToHash("0xbeef"),
		StateRoot:       types.HexToHash("0xcafe"),
		Slot:            100,
		BuilderIndex:    1,
	}
	if err := ValidatePayloadEnvelope(env); err != nil {
		t.Errorf("valid envelope: %v", err)
	}
}

func TestValidatePayloadEnvelopeEmptyPayloadRoot(t *testing.T) {
	env := &PayloadEnvelope{
		BeaconBlockRoot: types.HexToHash("0xbeef"),
		StateRoot:       types.HexToHash("0xcafe"),
		Slot:            100,
	}
	if err := ValidatePayloadEnvelope(env); err != ErrEmptyPayloadRoot {
		t.Errorf("empty payload root: got %v, want ErrEmptyPayloadRoot", err)
	}
}

func TestValidatePayloadEnvelopeEmptyBeaconRoot(t *testing.T) {
	env := &PayloadEnvelope{
		PayloadRoot: types.HexToHash("0xabcd"),
		StateRoot:   types.HexToHash("0xcafe"),
		Slot:        100,
	}
	if err := ValidatePayloadEnvelope(env); err != ErrEmptyBeaconRoot {
		t.Errorf("empty beacon root: got %v, want ErrEmptyBeaconRoot", err)
	}
}

func TestValidatePayloadEnvelopeEmptyStateRoot(t *testing.T) {
	env := &PayloadEnvelope{
		PayloadRoot:     types.HexToHash("0xabcd"),
		BeaconBlockRoot: types.HexToHash("0xbeef"),
		Slot:            100,
	}
	if err := ValidatePayloadEnvelope(env); err != ErrEmptyStateRoot {
		t.Errorf("empty state root: got %v, want ErrEmptyStateRoot", err)
	}
}

func TestValidatePayloadAttestationData(t *testing.T) {
	valid := &PayloadAttestationData{
		BeaconBlockRoot: types.HexToHash("0x1234"),
		Slot:            42,
		PayloadStatus:   PayloadPresent,
	}
	if err := ValidatePayloadAttestationData(valid); err != nil {
		t.Errorf("valid attestation: %v", err)
	}

	// Invalid status.
	invalid := &PayloadAttestationData{
		BeaconBlockRoot: types.HexToHash("0x1234"),
		Slot:            42,
		PayloadStatus:   5,
	}
	if err := ValidatePayloadAttestationData(invalid); err == nil {
		t.Error("expected error for invalid payload status")
	}
}

func TestValidateBidEnvelopeConsistency(t *testing.T) {
	bid := &BuilderBid{Slot: 100, BuilderIndex: 5}
	env := &PayloadEnvelope{Slot: 100, BuilderIndex: 5}

	if err := ValidateBidEnvelopeConsistency(bid, env); err != nil {
		t.Errorf("consistent bid/envelope: %v", err)
	}

	// Slot mismatch.
	envBad := &PayloadEnvelope{Slot: 200, BuilderIndex: 5}
	if err := ValidateBidEnvelopeConsistency(bid, envBad); err == nil {
		t.Error("expected error for slot mismatch")
	}

	// Builder mismatch.
	envBad2 := &PayloadEnvelope{Slot: 100, BuilderIndex: 9}
	if err := ValidateBidEnvelopeConsistency(bid, envBad2); err == nil {
		t.Error("expected error for builder index mismatch")
	}
}

// --- Auction Tests ---

func TestAuctionSubmitAndGetWinning(t *testing.T) {
	auction := NewPayloadAuction()

	bid1 := newValidSignedBid()
	bid1.Message.Value = 3000

	bid2 := newValidSignedBid()
	bid2.Message.Value = 7000
	bid2.Message.BuilderIndex = 2

	bid3 := newValidSignedBid()
	bid3.Message.Value = 5000
	bid3.Message.BuilderIndex = 3

	for _, bid := range []*SignedBuilderBid{bid1, bid2, bid3} {
		if err := auction.SubmitBid(bid); err != nil {
			t.Fatalf("SubmitBid: %v", err)
		}
	}

	winner, err := auction.GetWinningBid(100)
	if err != nil {
		t.Fatalf("GetWinningBid: %v", err)
	}
	if winner.Message.Value != 7000 {
		t.Errorf("winning value = %d, want 7000", winner.Message.Value)
	}
}

func TestAuctionGetWinningBidNoSlot(t *testing.T) {
	auction := NewPayloadAuction()
	_, err := auction.GetWinningBid(999)
	if err != ErrNoBidsForSlot {
		t.Errorf("no bids: got %v, want ErrNoBidsForSlot", err)
	}
}

func TestAuctionGetBidsForSlotSorted(t *testing.T) {
	auction := NewPayloadAuction()

	values := []uint64{1000, 5000, 3000}
	for i, v := range values {
		bid := newValidSignedBid()
		bid.Message.Value = v
		bid.Message.BuilderIndex = BuilderIndex(i + 1)
		if err := auction.SubmitBid(bid); err != nil {
			t.Fatalf("SubmitBid: %v", err)
		}
	}

	bids := auction.GetBidsForSlot(100)
	if len(bids) != 3 {
		t.Fatalf("bid count = %d, want 3", len(bids))
	}
	if bids[0].Message.Value != 5000 {
		t.Errorf("bids[0].Value = %d, want 5000", bids[0].Message.Value)
	}
	if bids[1].Message.Value != 3000 {
		t.Errorf("bids[1].Value = %d, want 3000", bids[1].Message.Value)
	}
	if bids[2].Message.Value != 1000 {
		t.Errorf("bids[2].Value = %d, want 1000", bids[2].Message.Value)
	}
}

func TestAuctionBidCount(t *testing.T) {
	auction := NewPayloadAuction()

	if auction.BidCount(100) != 0 {
		t.Errorf("initial count = %d, want 0", auction.BidCount(100))
	}

	for i := 0; i < 3; i++ {
		bid := newValidSignedBid()
		bid.Message.Value = uint64(1000 + i*1000)
		bid.Message.BuilderIndex = BuilderIndex(i + 1)
		auction.SubmitBid(bid)
	}

	if auction.BidCount(100) != 3 {
		t.Errorf("count = %d, want 3", auction.BidCount(100))
	}
}

func TestAuctionPruneSlot(t *testing.T) {
	auction := NewPayloadAuction()

	bid := newValidSignedBid()
	auction.SubmitBid(bid)

	if auction.BidCount(100) != 1 {
		t.Fatal("expected 1 bid before prune")
	}

	auction.PruneSlot(100)

	if auction.BidCount(100) != 0 {
		t.Error("expected 0 bids after prune")
	}
}

func TestAuctionPruneBefore(t *testing.T) {
	auction := NewPayloadAuction()

	for slot := uint64(98); slot <= 102; slot++ {
		bid := newValidSignedBid()
		bid.Message.Slot = slot
		bid.Message.Value = 1000
		auction.SubmitBid(bid)
	}

	auction.PruneBefore(100)

	// Slots 98, 99 should be pruned.
	if auction.BidCount(98) != 0 {
		t.Error("slot 98 should be pruned")
	}
	if auction.BidCount(99) != 0 {
		t.Error("slot 99 should be pruned")
	}
	// Slots 100, 101, 102 should remain.
	for slot := uint64(100); slot <= 102; slot++ {
		if auction.BidCount(slot) != 1 {
			t.Errorf("slot %d should have 1 bid", slot)
		}
	}
}

func TestAuctionRejectsInvalidBid(t *testing.T) {
	auction := NewPayloadAuction()

	bid := &SignedBuilderBid{
		Message: BuilderBid{
			// Missing required fields.
			Value: 0,
		},
	}

	if err := auction.SubmitBid(bid); err == nil {
		t.Error("expected error for invalid bid")
	}
}

func TestAuctionMultipleSlots(t *testing.T) {
	auction := NewPayloadAuction()

	for slot := uint64(100); slot <= 102; slot++ {
		bid := newValidSignedBid()
		bid.Message.Slot = slot
		bid.Message.Value = slot * 100
		if err := auction.SubmitBid(bid); err != nil {
			t.Fatalf("SubmitBid slot %d: %v", slot, err)
		}
	}

	for slot := uint64(100); slot <= 102; slot++ {
		bids := auction.GetBidsForSlot(slot)
		if len(bids) != 1 {
			t.Errorf("slot %d: bid count = %d, want 1", slot, len(bids))
		}
		if bids[0].Message.Value != slot*100 {
			t.Errorf("slot %d: value = %d, want %d", slot, bids[0].Message.Value, slot*100)
		}
	}
}

func TestAuctionEmptySlotBids(t *testing.T) {
	auction := NewPayloadAuction()
	bids := auction.GetBidsForSlot(999)
	if len(bids) != 0 {
		t.Errorf("empty slot bids = %d, want 0", len(bids))
	}
}
