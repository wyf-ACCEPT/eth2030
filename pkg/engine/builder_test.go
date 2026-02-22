package engine

import (
	"encoding/json"
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

func TestBuilderTypes(t *testing.T) {
	t.Run("BLSPubkey size", func(t *testing.T) {
		var pk BLSPubkey
		if len(pk) != 48 {
			t.Errorf("BLSPubkey size = %d, want 48", len(pk))
		}
	})

	t.Run("BLSSignature size", func(t *testing.T) {
		var sig BLSSignature
		if len(sig) != 96 {
			t.Errorf("BLSSignature size = %d, want 96", len(sig))
		}
	})

	t.Run("BuilderStatus constants", func(t *testing.T) {
		if BuilderStatusActive != 0 {
			t.Errorf("BuilderStatusActive = %d, want 0", BuilderStatusActive)
		}
		if BuilderStatusExiting != 1 {
			t.Errorf("BuilderStatusExiting = %d, want 1", BuilderStatusExiting)
		}
		if BuilderStatusWithdrawn != 2 {
			t.Errorf("BuilderStatusWithdrawn = %d, want 2", BuilderStatusWithdrawn)
		}
	})
}

func TestBuilderJSON(t *testing.T) {
	builder := Builder{
		Index:            42,
		FeeRecipient:     types.HexToAddress("0xdead"),
		GasLimit:         30_000_000,
		Balance:          big.NewInt(1e18),
		Status:           BuilderStatusActive,
		RegistrationTime: 1700000000,
	}

	data, err := json.Marshal(builder)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var decoded Builder
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if decoded.Index != 42 {
		t.Errorf("Index = %d, want 42", decoded.Index)
	}
	if decoded.GasLimit != 30_000_000 {
		t.Errorf("GasLimit = %d, want 30000000", decoded.GasLimit)
	}
	if decoded.Status != BuilderStatusActive {
		t.Errorf("Status = %d, want Active", decoded.Status)
	}
}

func TestBidHash(t *testing.T) {
	bid := ExecutionPayloadBid{
		ParentBlockHash: types.HexToHash("0xaabb000000000000000000000000000000000000000000000000000000000000"),
		BlockHash:       types.HexToHash("0xccdd000000000000000000000000000000000000000000000000000000000000"),
		Slot:            100,
		Value:           1000,
		GasLimit:        30_000_000,
		BuilderIndex:    1,
	}

	hash1 := bid.BidHash()
	hash2 := bid.BidHash()

	// Same bid should produce same hash.
	if hash1 != hash2 {
		t.Error("BidHash is not deterministic")
	}

	// Different bid should produce different hash.
	bid2 := bid
	bid2.Value = 2000
	hash3 := bid2.BidHash()
	if hash1 == hash3 {
		t.Error("different bids should produce different hashes")
	}
}

func TestSignedBidJSON(t *testing.T) {
	signed := SignedExecutionPayloadBid{
		Message: ExecutionPayloadBid{
			ParentBlockHash: types.HexToHash("0x1111"),
			BlockHash:       types.HexToHash("0x2222"),
			Slot:            100,
			Value:           5000,
			GasLimit:        30_000_000,
			BuilderIndex:    3,
			FeeRecipient:    types.HexToAddress("0xdead"),
		},
	}

	data, err := json.Marshal(signed)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var decoded SignedExecutionPayloadBid
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

func TestExecutionPayloadEnvelopeJSON(t *testing.T) {
	env := ExecutionPayloadEnvelope{
		BuilderIndex:    7,
		BeaconBlockRoot: types.HexToHash("0xbeef"),
		Slot:            200,
		StateRoot:       types.HexToHash("0xcafe"),
	}

	data, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var decoded ExecutionPayloadEnvelope
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if decoded.BuilderIndex != 7 {
		t.Errorf("BuilderIndex = %d, want 7", decoded.BuilderIndex)
	}
	if decoded.Slot != 200 {
		t.Errorf("Slot = %d, want 200", decoded.Slot)
	}
}

// --- Builder Registry Tests ---

func newTestPubkey(b byte) BLSPubkey {
	var pk BLSPubkey
	pk[0] = b
	return pk
}

func newTestRegistration(pubkey BLSPubkey) *BuilderRegistrationV1 {
	return &BuilderRegistrationV1{
		FeeRecipient: types.HexToAddress("0xfee"),
		GasLimit:     30_000_000,
		Timestamp:    1700000000,
		Pubkey:       pubkey,
	}
}

func TestBuilderRegistryRegister(t *testing.T) {
	reg := NewBuilderRegistry()

	pk := newTestPubkey(0x01)
	registration := newTestRegistration(pk)
	stake := new(big.Int).Set(MinBuilderStake)

	builder, err := reg.RegisterBuilder(registration, stake)
	if err != nil {
		t.Fatalf("RegisterBuilder: %v", err)
	}

	if builder.Index != 0 {
		t.Errorf("first builder index = %d, want 0", builder.Index)
	}
	if builder.Status != BuilderStatusActive {
		t.Errorf("status = %d, want Active", builder.Status)
	}
	if builder.GasLimit != 30_000_000 {
		t.Errorf("gas limit = %d, want 30000000", builder.GasLimit)
	}
	if builder.Balance.Cmp(stake) != 0 {
		t.Errorf("balance = %s, want %s", builder.Balance, stake)
	}
}

func TestBuilderRegistryDuplicate(t *testing.T) {
	reg := NewBuilderRegistry()

	pk := newTestPubkey(0x01)
	registration := newTestRegistration(pk)
	stake := new(big.Int).Set(MinBuilderStake)

	_, err := reg.RegisterBuilder(registration, stake)
	if err != nil {
		t.Fatalf("first RegisterBuilder: %v", err)
	}

	// Duplicate should fail.
	_, err = reg.RegisterBuilder(registration, stake)
	if err != ErrBuilderAlreadyExists {
		t.Errorf("duplicate register err = %v, want ErrBuilderAlreadyExists", err)
	}
}

func TestBuilderRegistryInsufficientStake(t *testing.T) {
	reg := NewBuilderRegistry()

	pk := newTestPubkey(0x01)
	registration := newTestRegistration(pk)

	// Stake below minimum.
	tooLow := big.NewInt(1000)
	_, err := reg.RegisterBuilder(registration, tooLow)
	if err == nil {
		t.Error("expected error for insufficient stake")
	}
}

func TestBuilderRegistryUnregister(t *testing.T) {
	reg := NewBuilderRegistry()

	pk := newTestPubkey(0x01)
	registration := newTestRegistration(pk)
	stake := new(big.Int).Set(MinBuilderStake)

	_, err := reg.RegisterBuilder(registration, stake)
	if err != nil {
		t.Fatalf("RegisterBuilder: %v", err)
	}

	// Unregister should succeed.
	if err := reg.UnregisterBuilder(pk); err != nil {
		t.Fatalf("UnregisterBuilder: %v", err)
	}

	// Check status is Exiting.
	builder, _ := reg.GetBuilder(pk)
	if builder.Status != BuilderStatusExiting {
		t.Errorf("status = %d, want Exiting", builder.Status)
	}

	// Unregister again should fail (not active).
	if err := reg.UnregisterBuilder(pk); err != ErrBuilderNotActive {
		t.Errorf("double unregister err = %v, want ErrBuilderNotActive", err)
	}
}

func TestBuilderRegistryNotFound(t *testing.T) {
	reg := NewBuilderRegistry()

	pk := newTestPubkey(0xff)
	_, err := reg.GetBuilder(pk)
	if err != ErrBuilderNotFound {
		t.Errorf("GetBuilder err = %v, want ErrBuilderNotFound", err)
	}

	err = reg.UnregisterBuilder(pk)
	if err != ErrBuilderNotFound {
		t.Errorf("UnregisterBuilder err = %v, want ErrBuilderNotFound", err)
	}
}

func TestBuilderRegistryGetByIndex(t *testing.T) {
	reg := NewBuilderRegistry()
	stake := new(big.Int).Set(MinBuilderStake)

	// Register two builders.
	pk1 := newTestPubkey(0x01)
	b1, _ := reg.RegisterBuilder(newTestRegistration(pk1), stake)

	pk2 := newTestPubkey(0x02)
	b2, _ := reg.RegisterBuilder(newTestRegistration(pk2), stake)

	// Look up by index.
	got1, err := reg.GetBuilderByIndex(b1.Index)
	if err != nil {
		t.Fatalf("GetBuilderByIndex(%d): %v", b1.Index, err)
	}
	if got1.Pubkey != pk1 {
		t.Error("builder 1 pubkey mismatch")
	}

	got2, err := reg.GetBuilderByIndex(b2.Index)
	if err != nil {
		t.Fatalf("GetBuilderByIndex(%d): %v", b2.Index, err)
	}
	if got2.Pubkey != pk2 {
		t.Error("builder 2 pubkey mismatch")
	}

	// Unknown index.
	_, err = reg.GetBuilderByIndex(999)
	if err != ErrBuilderNotFound {
		t.Errorf("unknown index err = %v, want ErrBuilderNotFound", err)
	}
}

func TestBuilderRegistryGetRegistered(t *testing.T) {
	reg := NewBuilderRegistry()
	stake := new(big.Int).Set(MinBuilderStake)

	// Register three builders, unregister one.
	pk1 := newTestPubkey(0x01)
	reg.RegisterBuilder(newTestRegistration(pk1), stake)

	pk2 := newTestPubkey(0x02)
	reg.RegisterBuilder(newTestRegistration(pk2), stake)

	pk3 := newTestPubkey(0x03)
	reg.RegisterBuilder(newTestRegistration(pk3), stake)

	reg.UnregisterBuilder(pk2)

	active := reg.GetRegisteredBuilders()
	if len(active) != 2 {
		t.Errorf("active builders = %d, want 2", len(active))
	}
}

func TestBuilderRegistryBuilderCount(t *testing.T) {
	reg := NewBuilderRegistry()
	stake := new(big.Int).Set(MinBuilderStake)

	if reg.BuilderCount() != 0 {
		t.Errorf("initial count = %d, want 0", reg.BuilderCount())
	}

	reg.RegisterBuilder(newTestRegistration(newTestPubkey(0x01)), stake)
	reg.RegisterBuilder(newTestRegistration(newTestPubkey(0x02)), stake)

	if reg.BuilderCount() != 2 {
		t.Errorf("count = %d, want 2", reg.BuilderCount())
	}
}

// --- Bid Management Tests ---

func registerTestBuilder(t *testing.T, reg *BuilderRegistry, idx byte) BuilderIndex {
	t.Helper()
	pk := newTestPubkey(idx)
	b, err := reg.RegisterBuilder(newTestRegistration(pk), new(big.Int).Set(MinBuilderStake))
	if err != nil {
		t.Fatalf("RegisterBuilder: %v", err)
	}
	return b.Index
}

func newTestBid(builderIdx BuilderIndex, slot uint64, value uint64) *SignedExecutionPayloadBid {
	return &SignedExecutionPayloadBid{
		Message: ExecutionPayloadBid{
			ParentBlockHash: types.HexToHash("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
			BlockHash:       types.HexToHash("0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"),
			Slot:            slot,
			Value:           value,
			GasLimit:        30_000_000,
			BuilderIndex:    builderIdx,
			FeeRecipient:    types.HexToAddress("0xfeefeefeefeefeefeefe"),
		},
	}
}

func TestSubmitBid(t *testing.T) {
	reg := NewBuilderRegistry()
	idx := registerTestBuilder(t, reg, 0x01)

	bid := newTestBid(idx, 100, 5000)
	if err := reg.SubmitBid(bid); err != nil {
		t.Fatalf("SubmitBid: %v", err)
	}

	bids := reg.GetBidsForSlot(100)
	if len(bids) != 1 {
		t.Fatalf("bids for slot 100 = %d, want 1", len(bids))
	}
	if bids[0].Message.Value != 5000 {
		t.Errorf("bid value = %d, want 5000", bids[0].Message.Value)
	}
}

func TestSubmitBidUnknownBuilder(t *testing.T) {
	reg := NewBuilderRegistry()

	bid := newTestBid(999, 100, 5000)
	if err := reg.SubmitBid(bid); err == nil {
		t.Error("expected error for unknown builder")
	}
}

func TestSubmitBidInactiveBuilder(t *testing.T) {
	reg := NewBuilderRegistry()
	pk := newTestPubkey(0x01)
	b, _ := reg.RegisterBuilder(newTestRegistration(pk), new(big.Int).Set(MinBuilderStake))

	reg.UnregisterBuilder(pk)

	bid := newTestBid(b.Index, 100, 5000)
	if err := reg.SubmitBid(bid); err != ErrBuilderNotActive {
		t.Errorf("inactive builder bid err = %v, want ErrBuilderNotActive", err)
	}
}

func TestSubmitBidZeroValue(t *testing.T) {
	reg := NewBuilderRegistry()
	idx := registerTestBuilder(t, reg, 0x01)

	bid := newTestBid(idx, 100, 0)
	if err := reg.SubmitBid(bid); err == nil {
		t.Error("expected error for zero value bid")
	}
}

func TestSubmitBidEmptyBlockHash(t *testing.T) {
	reg := NewBuilderRegistry()
	idx := registerTestBuilder(t, reg, 0x01)

	bid := newTestBid(idx, 100, 5000)
	bid.Message.BlockHash = types.Hash{}
	if err := reg.SubmitBid(bid); err == nil {
		t.Error("expected error for empty block hash")
	}
}

func TestSubmitBidEmptyParentHash(t *testing.T) {
	reg := NewBuilderRegistry()
	idx := registerTestBuilder(t, reg, 0x01)

	bid := newTestBid(idx, 100, 5000)
	bid.Message.ParentBlockHash = types.Hash{}
	if err := reg.SubmitBid(bid); err == nil {
		t.Error("expected error for empty parent hash")
	}
}

func TestGetBestBid(t *testing.T) {
	reg := NewBuilderRegistry()
	idx1 := registerTestBuilder(t, reg, 0x01)
	idx2 := registerTestBuilder(t, reg, 0x02)

	slot := uint64(100)

	// Submit multiple bids with different values.
	reg.SubmitBid(newTestBid(idx1, slot, 3000))
	reg.SubmitBid(newTestBid(idx2, slot, 7000))
	reg.SubmitBid(newTestBid(idx1, slot, 5000))

	best, err := reg.GetBestBid(slot)
	if err != nil {
		t.Fatalf("GetBestBid: %v", err)
	}
	if best.Message.Value != 7000 {
		t.Errorf("best bid value = %d, want 7000", best.Message.Value)
	}
}

func TestGetBestBidNoSlot(t *testing.T) {
	reg := NewBuilderRegistry()

	_, err := reg.GetBestBid(999)
	if err != ErrNoBidsAvailable {
		t.Errorf("no bids err = %v, want ErrNoBidsAvailable", err)
	}
}

func TestGetBidsForSlotSorted(t *testing.T) {
	reg := NewBuilderRegistry()
	idx := registerTestBuilder(t, reg, 0x01)

	slot := uint64(100)
	reg.SubmitBid(newTestBid(idx, slot, 1000))
	reg.SubmitBid(newTestBid(idx, slot, 5000))
	reg.SubmitBid(newTestBid(idx, slot, 3000))

	bids := reg.GetBidsForSlot(slot)
	if len(bids) != 3 {
		t.Fatalf("bids count = %d, want 3", len(bids))
	}

	// Should be sorted by value descending.
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

func TestGetBidsForSlotEmpty(t *testing.T) {
	reg := NewBuilderRegistry()

	bids := reg.GetBidsForSlot(999)
	if len(bids) != 0 {
		t.Errorf("empty slot bids = %d, want 0", len(bids))
	}
}

func TestPruneSlot(t *testing.T) {
	reg := NewBuilderRegistry()
	idx := registerTestBuilder(t, reg, 0x01)

	slot := uint64(100)
	reg.SubmitBid(newTestBid(idx, slot, 5000))

	if len(reg.GetBidsForSlot(slot)) != 1 {
		t.Fatal("expected 1 bid before prune")
	}

	reg.PruneSlot(slot)

	if len(reg.GetBidsForSlot(slot)) != 0 {
		t.Error("expected 0 bids after prune")
	}
}

// --- Bid Payload Validation Tests ---

func TestValidateBidPayloadMatch(t *testing.T) {
	reg := NewBuilderRegistry()

	parentHash := types.HexToHash("0x1111111111111111111111111111111111111111111111111111111111111111")
	blockHash := types.HexToHash("0x2222222222222222222222222222222222222222222222222222222222222222")
	feeAddr := types.HexToAddress("0xfeefeefeefeefeefeefe")

	bid := &ExecutionPayloadBid{
		ParentBlockHash: parentHash,
		BlockHash:       blockHash,
		GasLimit:        30_000_000,
		FeeRecipient:    feeAddr,
	}

	payload := &ExecutionPayloadV4{
		ExecutionPayloadV3: ExecutionPayloadV3{
			ExecutionPayloadV2: ExecutionPayloadV2{
				ExecutionPayloadV1: ExecutionPayloadV1{
					ParentHash:   parentHash,
					BlockHash:    blockHash,
					GasLimit:     30_000_000,
					FeeRecipient: feeAddr,
				},
			},
		},
	}

	if err := reg.ValidateBidPayload(bid, payload); err != nil {
		t.Errorf("valid payload: %v", err)
	}
}

func TestValidateBidPayloadBlockHashMismatch(t *testing.T) {
	reg := NewBuilderRegistry()

	parentHash := types.HexToHash("0x1111111111111111111111111111111111111111111111111111111111111111")
	blockHash := types.HexToHash("0x2222222222222222222222222222222222222222222222222222222222222222")
	wrongHash := types.HexToHash("0x3333333333333333333333333333333333333333333333333333333333333333")
	feeAddr := types.HexToAddress("0xfeefeefeefeefeefeefe")

	bid := &ExecutionPayloadBid{
		ParentBlockHash: parentHash,
		BlockHash:       blockHash,
		GasLimit:        30_000_000,
		FeeRecipient:    feeAddr,
	}

	payload := &ExecutionPayloadV4{
		ExecutionPayloadV3: ExecutionPayloadV3{
			ExecutionPayloadV2: ExecutionPayloadV2{
				ExecutionPayloadV1: ExecutionPayloadV1{
					ParentHash:   parentHash,
					BlockHash:    wrongHash,
					GasLimit:     30_000_000,
					FeeRecipient: feeAddr,
				},
			},
		},
	}

	err := reg.ValidateBidPayload(bid, payload)
	if err == nil {
		t.Error("expected error for block hash mismatch")
	}
}

func TestValidateBidPayloadParentHashMismatch(t *testing.T) {
	reg := NewBuilderRegistry()

	parentHash := types.HexToHash("0x1111111111111111111111111111111111111111111111111111111111111111")
	blockHash := types.HexToHash("0x2222222222222222222222222222222222222222222222222222222222222222")
	wrongHash := types.HexToHash("0x3333333333333333333333333333333333333333333333333333333333333333")
	feeAddr := types.HexToAddress("0xfeefeefeefeefeefeefe")

	bid := &ExecutionPayloadBid{
		ParentBlockHash: parentHash,
		BlockHash:       blockHash,
		GasLimit:        30_000_000,
		FeeRecipient:    feeAddr,
	}

	payload := &ExecutionPayloadV4{
		ExecutionPayloadV3: ExecutionPayloadV3{
			ExecutionPayloadV2: ExecutionPayloadV2{
				ExecutionPayloadV1: ExecutionPayloadV1{
					ParentHash:   wrongHash,
					BlockHash:    blockHash,
					GasLimit:     30_000_000,
					FeeRecipient: feeAddr,
				},
			},
		},
	}

	err := reg.ValidateBidPayload(bid, payload)
	if err == nil {
		t.Error("expected error for parent hash mismatch")
	}
}

func TestValidateBidPayloadGasLimitMismatch(t *testing.T) {
	reg := NewBuilderRegistry()

	parentHash := types.HexToHash("0x1111111111111111111111111111111111111111111111111111111111111111")
	blockHash := types.HexToHash("0x2222222222222222222222222222222222222222222222222222222222222222")
	feeAddr := types.HexToAddress("0xfeefeefeefeefeefeefe")

	bid := &ExecutionPayloadBid{
		ParentBlockHash: parentHash,
		BlockHash:       blockHash,
		GasLimit:        30_000_000,
		FeeRecipient:    feeAddr,
	}

	payload := &ExecutionPayloadV4{
		ExecutionPayloadV3: ExecutionPayloadV3{
			ExecutionPayloadV2: ExecutionPayloadV2{
				ExecutionPayloadV1: ExecutionPayloadV1{
					ParentHash:   parentHash,
					BlockHash:    blockHash,
					GasLimit:     60_000_000,
					FeeRecipient: feeAddr,
				},
			},
		},
	}

	err := reg.ValidateBidPayload(bid, payload)
	if err == nil {
		t.Error("expected error for gas limit mismatch")
	}
}

func TestValidateBidPayloadFeeRecipientMismatch(t *testing.T) {
	reg := NewBuilderRegistry()

	parentHash := types.HexToHash("0x1111111111111111111111111111111111111111111111111111111111111111")
	blockHash := types.HexToHash("0x2222222222222222222222222222222222222222222222222222222222222222")
	feeAddr := types.HexToAddress("0xfeefeefeefeefeefeefe")
	wrongAddr := types.HexToAddress("0xdeaddeaddeaddeaddead")

	bid := &ExecutionPayloadBid{
		ParentBlockHash: parentHash,
		BlockHash:       blockHash,
		GasLimit:        30_000_000,
		FeeRecipient:    feeAddr,
	}

	payload := &ExecutionPayloadV4{
		ExecutionPayloadV3: ExecutionPayloadV3{
			ExecutionPayloadV2: ExecutionPayloadV2{
				ExecutionPayloadV1: ExecutionPayloadV1{
					ParentHash:   parentHash,
					BlockHash:    blockHash,
					GasLimit:     30_000_000,
					FeeRecipient: wrongAddr,
				},
			},
		},
	}

	err := reg.ValidateBidPayload(bid, payload)
	if err == nil {
		t.Error("expected error for fee recipient mismatch")
	}
}

func TestMultipleSlotsBids(t *testing.T) {
	reg := NewBuilderRegistry()
	idx := registerTestBuilder(t, reg, 0x01)

	// Bids on different slots.
	reg.SubmitBid(newTestBid(idx, 100, 5000))
	reg.SubmitBid(newTestBid(idx, 101, 6000))
	reg.SubmitBid(newTestBid(idx, 102, 7000))

	// Each slot should have exactly 1 bid.
	for slot := uint64(100); slot <= 102; slot++ {
		bids := reg.GetBidsForSlot(slot)
		if len(bids) != 1 {
			t.Errorf("slot %d: bids = %d, want 1", slot, len(bids))
		}
	}

	// Prune slot 100, others should remain.
	reg.PruneSlot(100)
	if len(reg.GetBidsForSlot(100)) != 0 {
		t.Error("slot 100 should have 0 bids after prune")
	}
	if len(reg.GetBidsForSlot(101)) != 1 {
		t.Error("slot 101 should still have 1 bid")
	}
}

func TestBuilderRegistrationJSON(t *testing.T) {
	reg := BuilderRegistrationV1{
		FeeRecipient: types.HexToAddress("0xdead"),
		GasLimit:     30_000_000,
		Timestamp:    1700000000,
	}

	data, err := json.Marshal(reg)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var decoded BuilderRegistrationV1
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if decoded.GasLimit != 30_000_000 {
		t.Errorf("GasLimit = %d, want 30000000", decoded.GasLimit)
	}
	if decoded.Timestamp != 1700000000 {
		t.Errorf("Timestamp = %d, want 1700000000", decoded.Timestamp)
	}
}

func TestMinBuilderStake(t *testing.T) {
	oneETH := new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil)
	if MinBuilderStake.Cmp(oneETH) != 0 {
		t.Errorf("MinBuilderStake = %s, want 1e18", MinBuilderStake)
	}
}
