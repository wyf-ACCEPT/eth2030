package epbs

import (
	"errors"
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

func slBid(slot, value uint64, builder BuilderIndex) *BuilderBid {
	return &BuilderBid{
		ParentBlockHash: types.HexToHash("0xaaaa000000000000000000000000000000000000000000000000000000000000"),
		BlockHash:       types.HexToHash("0xbbbb000000000000000000000000000000000000000000000000000000000000"),
		Slot:            slot,
		Value:           value,
		GasLimit:        30_000_000,
		BuilderIndex:    builder,
		FeeRecipient:    types.HexToAddress("0xdead"),
	}
}

func slAddr(b byte) types.Address {
	var a types.Address
	a[19] = b
	return a
}

func slPayload(bid *BuilderBid) *PayloadEnvelope {
	return &PayloadEnvelope{
		PayloadRoot: bid.BlockHash, BuilderIndex: bid.BuilderIndex,
		BeaconBlockRoot: types.HexToHash("0xbeef"),
		Slot: bid.Slot, StateRoot: types.HexToHash("0xcafe"),
	}
}

// --- NonDeliverySlashing ---

func TestNonDeliveryTriggeredWhenNoPayload(t *testing.T) {
	cond := &NonDeliverySlashing{DeadlineSlots: 4, CurrentSlot: 110}
	bid := slBid(100, 5000, 1)
	violated, reason := cond.Check(bid, nil)
	if !violated {
		t.Error("expected non-delivery violation")
	}
	if reason == "" {
		t.Error("reason should not be empty")
	}
}

func TestNonDeliveryNotTriggeredBeforeDeadline(t *testing.T) {
	cond := &NonDeliverySlashing{DeadlineSlots: 4, CurrentSlot: 103}
	violated, _ := cond.Check(slBid(100, 5000, 1), nil)
	if violated {
		t.Error("should not trigger before deadline")
	}
}

func TestNonDeliveryNotTriggeredWithPayload(t *testing.T) {
	cond := &NonDeliverySlashing{DeadlineSlots: 4, CurrentSlot: 200}
	bid := slBid(100, 5000, 1)
	violated, _ := cond.Check(bid, slPayload(bid))
	if violated {
		t.Error("should not trigger when payload is delivered")
	}
}

func TestNonDeliveryNilBid(t *testing.T) {
	cond := &NonDeliverySlashing{DeadlineSlots: 4, CurrentSlot: 200}
	violated, _ := cond.Check(nil, nil)
	if violated {
		t.Error("should not trigger for nil bid")
	}
}

func TestNonDeliveryType(t *testing.T) {
	if (&NonDeliverySlashing{}).Type() != SlashNonDelivery {
		t.Error("wrong type")
	}
}

// --- InvalidPayloadSlashing ---

func TestInvalidPayloadSlotMismatch(t *testing.T) {
	cond := &InvalidPayloadSlashing{}
	bid := slBid(100, 5000, 1)
	p := slPayload(bid)
	p.Slot = 200
	violated, reason := cond.Check(bid, p)
	if !violated {
		t.Error("expected slot mismatch violation")
	}
	if reason == "" {
		t.Error("reason should not be empty")
	}
}

func TestInvalidPayloadBuilderMismatch(t *testing.T) {
	bid := slBid(100, 5000, 1)
	p := slPayload(bid)
	p.BuilderIndex = 99
	violated, _ := (&InvalidPayloadSlashing{}).Check(bid, p)
	if !violated {
		t.Error("expected builder mismatch violation")
	}
}

func TestInvalidPayloadBlockHashMismatch(t *testing.T) {
	bid := slBid(100, 5000, 1)
	p := slPayload(bid)
	p.PayloadRoot = types.HexToHash("0xdead")
	violated, _ := (&InvalidPayloadSlashing{}).Check(bid, p)
	if !violated {
		t.Error("expected block hash mismatch violation")
	}
}

func TestInvalidPayloadValidPayload(t *testing.T) {
	bid := slBid(100, 5000, 1)
	violated, _ := (&InvalidPayloadSlashing{}).Check(bid, slPayload(bid))
	if violated {
		t.Error("matching payload should not trigger violation")
	}
}

func TestInvalidPayloadNilInputs(t *testing.T) {
	cond := &InvalidPayloadSlashing{}
	if v, _ := cond.Check(nil, nil); v {
		t.Error("nil inputs should not trigger")
	}
	if v, _ := cond.Check(slBid(100, 5000, 1), nil); v {
		t.Error("nil payload should not trigger")
	}
}

func TestInvalidPayloadType(t *testing.T) {
	if (&InvalidPayloadSlashing{}).Type() != SlashInvalidPayload {
		t.Error("wrong type")
	}
}

// --- EquivocationSlashing ---

func TestEquivocationDetected(t *testing.T) {
	bidA := slBid(100, 5000, 1)
	bidA.BlockHash = types.HexToHash("0xaaaa")
	bidB := slBid(100, 5000, 1)
	bidB.BlockHash = types.HexToHash("0xbbbb")
	cond := &EquivocationSlashing{Evidence: &EquivocationEvidence{BidA: bidA, BidB: bidB}}
	violated, reason := cond.Check(bidA, nil)
	if !violated {
		t.Error("expected equivocation violation")
	}
	if reason == "" {
		t.Error("reason should not be empty")
	}
}

func TestEquivocationSameHash(t *testing.T) {
	bidA := slBid(100, 5000, 1)
	bidB := slBid(100, 5000, 1) // same hash
	cond := &EquivocationSlashing{Evidence: &EquivocationEvidence{BidA: bidA, BidB: bidB}}
	if v, _ := cond.Check(bidA, nil); v {
		t.Error("same hash should not trigger equivocation")
	}
}

func TestEquivocationDifferentSlots(t *testing.T) {
	bidA := slBid(100, 5000, 1)
	bidA.BlockHash = types.HexToHash("0xaaaa")
	bidB := slBid(200, 5000, 1)
	bidB.BlockHash = types.HexToHash("0xbbbb")
	cond := &EquivocationSlashing{Evidence: &EquivocationEvidence{BidA: bidA, BidB: bidB}}
	if v, _ := cond.Check(bidA, nil); v {
		t.Error("different slots should not trigger equivocation")
	}
}

func TestEquivocationDifferentBuilders(t *testing.T) {
	bidA := slBid(100, 5000, 1)
	bidA.BlockHash = types.HexToHash("0xaaaa")
	bidB := slBid(100, 5000, 2)
	bidB.BlockHash = types.HexToHash("0xbbbb")
	cond := &EquivocationSlashing{Evidence: &EquivocationEvidence{BidA: bidA, BidB: bidB}}
	if v, _ := cond.Check(bidA, nil); v {
		t.Error("different builders should not trigger equivocation")
	}
}

func TestEquivocationNilEvidence(t *testing.T) {
	cond := &EquivocationSlashing{Evidence: nil}
	if v, _ := cond.Check(slBid(100, 5000, 1), nil); v {
		t.Error("nil evidence should not trigger")
	}
}

func TestEquivocationType(t *testing.T) {
	if (&EquivocationSlashing{}).Type() != SlashEquivocation {
		t.Error("wrong type")
	}
}

// --- ComputePenalty ---

func TestComputePenaltyNonDelivery(t *testing.T) {
	p, err := ComputePenalty(SlashNonDelivery, 10000, DefaultPenaltyMultipliers())
	if err != nil {
		t.Fatalf("ComputePenalty: %v", err)
	}
	if p != 20000 { // 2x bid
		t.Errorf("penalty = %d, want 20000", p)
	}
}

func TestComputePenaltyInvalidPayload(t *testing.T) {
	p, err := ComputePenalty(SlashInvalidPayload, 10000, DefaultPenaltyMultipliers())
	if err != nil {
		t.Fatalf("ComputePenalty: %v", err)
	}
	if p != 30000 { // 3x bid
		t.Errorf("penalty = %d, want 30000", p)
	}
}

func TestComputePenaltyEquivocation(t *testing.T) {
	p, err := ComputePenalty(SlashEquivocation, 10000, DefaultPenaltyMultipliers())
	if err != nil {
		t.Fatalf("ComputePenalty: %v", err)
	}
	if p != 50000 { // 5x bid
		t.Errorf("penalty = %d, want 50000", p)
	}
}

func TestComputePenaltyUnknownCondition(t *testing.T) {
	_, err := ComputePenalty("unknown_type", 10000, DefaultPenaltyMultipliers())
	if !errors.Is(err, ErrSlashingInvalidPenalty) {
		t.Errorf("expected ErrSlashingInvalidPenalty, got %v", err)
	}
}

func TestComputePenaltySmallValue(t *testing.T) {
	p, err := ComputePenalty(SlashNonDelivery, 500, DefaultPenaltyMultipliers())
	if err != nil {
		t.Fatalf("ComputePenalty: %v", err)
	}
	if p != 1000 { // 500 * 2
		t.Errorf("penalty = %d, want 1000", p)
	}
}

// --- ComputeEvidenceHash ---

func TestComputeEvidenceHashDeterministic(t *testing.T) {
	bid := slBid(100, 5000, 1)
	h1 := ComputeEvidenceHash(SlashNonDelivery, bid, slAddr(0x01))
	h2 := ComputeEvidenceHash(SlashNonDelivery, bid, slAddr(0x01))
	if h1 != h2 {
		t.Error("evidence hash should be deterministic")
	}
}

func TestComputeEvidenceHashDifferentConditions(t *testing.T) {
	bid := slBid(100, 5000, 1)
	addr := slAddr(0x01)
	h1 := ComputeEvidenceHash(SlashNonDelivery, bid, addr)
	h2 := ComputeEvidenceHash(SlashEquivocation, bid, addr)
	if h1 == h2 {
		t.Error("different conditions should produce different hashes")
	}
}

func TestComputeEvidenceHashNilBid(t *testing.T) {
	h := ComputeEvidenceHash(SlashNonDelivery, nil, slAddr(0x01))
	if h != (types.Hash{}) {
		t.Error("nil bid should produce zero hash")
	}
}

// --- SlashingEngine ---

func TestSlashingEngineNonDelivery(t *testing.T) {
	engine := NewSlashingEngine(DefaultPenaltyMultipliers(), 100)
	engine.RegisterCondition(&NonDeliverySlashing{DeadlineSlots: 4, CurrentSlot: 200})
	records, err := engine.EvaluateAll(slBid(100, 10000, 1), nil, slAddr(0x01))
	if err != nil {
		t.Fatalf("EvaluateAll: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].ConditionType != SlashNonDelivery {
		t.Errorf("condition = %s, want %s", records[0].ConditionType, SlashNonDelivery)
	}
	if records[0].PenaltyGwei != 20000 {
		t.Errorf("penalty = %d, want 20000", records[0].PenaltyGwei)
	}
}

func TestSlashingEngineMultipleConditions(t *testing.T) {
	engine := NewSlashingEngine(DefaultPenaltyMultipliers(), 100)
	engine.RegisterCondition(&NonDeliverySlashing{DeadlineSlots: 4, CurrentSlot: 200})
	engine.RegisterCondition(&InvalidPayloadSlashing{})
	// Non-delivery triggers; invalid payload does not (no payload given).
	records, err := engine.EvaluateAll(slBid(100, 10000, 1), nil, slAddr(0x01))
	if err != nil {
		t.Fatalf("EvaluateAll: %v", err)
	}
	if len(records) != 1 || records[0].ConditionType != SlashNonDelivery {
		t.Errorf("only non-delivery should trigger, got %d records", len(records))
	}
}

func TestSlashingEngineNoViolation(t *testing.T) {
	engine := NewSlashingEngine(DefaultPenaltyMultipliers(), 100)
	engine.RegisterCondition(&InvalidPayloadSlashing{})
	bid := slBid(100, 10000, 1)
	records, err := engine.EvaluateAll(bid, slPayload(bid), slAddr(0x01))
	if err != nil {
		t.Fatalf("EvaluateAll: %v", err)
	}
	if len(records) != 0 {
		t.Errorf("expected 0 records, got %d", len(records))
	}
}

func TestSlashingEngineNilBid(t *testing.T) {
	engine := NewSlashingEngine(DefaultPenaltyMultipliers(), 100)
	engine.RegisterCondition(&NonDeliverySlashing{DeadlineSlots: 4, CurrentSlot: 200})
	_, err := engine.EvaluateAll(nil, nil, slAddr(0x01))
	if !errors.Is(err, ErrSlashingNilBid) {
		t.Errorf("expected ErrSlashingNilBid, got %v", err)
	}
}

func TestSlashingEngineNoConditions(t *testing.T) {
	engine := NewSlashingEngine(DefaultPenaltyMultipliers(), 100)
	_, err := engine.EvaluateAll(slBid(100, 10000, 1), nil, slAddr(0x01))
	if !errors.Is(err, ErrSlashingNoConditions) {
		t.Errorf("expected ErrSlashingNoConditions, got %v", err)
	}
}

func TestSlashingEngineRecordCount(t *testing.T) {
	engine := NewSlashingEngine(DefaultPenaltyMultipliers(), 100)
	engine.RegisterCondition(&NonDeliverySlashing{DeadlineSlots: 4, CurrentSlot: 200})
	addr := slAddr(0x01)
	for i := uint64(1); i <= 5; i++ {
		engine.EvaluateAll(slBid(i, 1000*i, 1), nil, addr)
	}
	if engine.RecordCount() != 5 {
		t.Errorf("record count = %d, want 5", engine.RecordCount())
	}
}

func TestSlashingEngineTotalPenaltyForBuilder(t *testing.T) {
	engine := NewSlashingEngine(DefaultPenaltyMultipliers(), 100)
	engine.RegisterCondition(&NonDeliverySlashing{DeadlineSlots: 4, CurrentSlot: 200})
	addr1, addr2 := slAddr(0x01), slAddr(0x02)
	engine.EvaluateAll(slBid(100, 10000, 1), nil, addr1)
	engine.EvaluateAll(slBid(101, 5000, 1), nil, addr1)
	engine.EvaluateAll(slBid(102, 3000, 2), nil, addr2)
	if total := engine.TotalPenaltyForBuilder(addr1); total != 30000 {
		t.Errorf("builder 1 total = %d, want 30000", total)
	}
	if total := engine.TotalPenaltyForBuilder(addr2); total != 6000 {
		t.Errorf("builder 2 total = %d, want 6000", total)
	}
}

func TestSlashingEngineRecordsForBuilder(t *testing.T) {
	engine := NewSlashingEngine(DefaultPenaltyMultipliers(), 100)
	engine.RegisterCondition(&NonDeliverySlashing{DeadlineSlots: 4, CurrentSlot: 200})
	addr1, addr2 := slAddr(0x01), slAddr(0x02)
	engine.EvaluateAll(slBid(100, 10000, 1), nil, addr1)
	engine.EvaluateAll(slBid(101, 5000, 1), nil, addr1)
	engine.EvaluateAll(slBid(102, 3000, 2), nil, addr2)
	if n := len(engine.RecordsForBuilder(addr1)); n != 2 {
		t.Errorf("builder 1 records = %d, want 2", n)
	}
	if n := len(engine.RecordsForBuilder(addr2)); n != 1 {
		t.Errorf("builder 2 records = %d, want 1", n)
	}
}

func TestSlashingEngineRecordTrimming(t *testing.T) {
	engine := NewSlashingEngine(DefaultPenaltyMultipliers(), 3)
	engine.RegisterCondition(&NonDeliverySlashing{DeadlineSlots: 4, CurrentSlot: 200})
	addr := slAddr(0x01)
	for i := uint64(1); i <= 10; i++ {
		engine.EvaluateAll(slBid(i, 1000, 1), nil, addr)
	}
	if engine.RecordCount() != 3 {
		t.Errorf("record count = %d, want 3 (max)", engine.RecordCount())
	}
}

func TestSlashingEngineConditionCount(t *testing.T) {
	engine := NewSlashingEngine(DefaultPenaltyMultipliers(), 100)
	if engine.ConditionCount() != 0 {
		t.Error("initial condition count should be 0")
	}
	engine.RegisterCondition(&NonDeliverySlashing{})
	engine.RegisterCondition(&InvalidPayloadSlashing{})
	if engine.ConditionCount() != 2 {
		t.Errorf("condition count = %d, want 2", engine.ConditionCount())
	}
}
