package epbs

import (
	"fmt"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

// makeBidForEscrow creates a BuilderBid suitable for escrow testing.
func makeBidForEscrow(slot uint64, builderIdx BuilderIndex, value uint64) *BuilderBid {
	return &BuilderBid{
		Slot:            slot,
		BuilderIndex:    builderIdx,
		Value:           value,
		BlockHash:       types.HexToHash("0xaa"),
		ParentBlockHash: types.HexToHash("0xbb"),
		GasLimit:        30_000_000,
	}
}

// makePayloadForEscrow creates a PayloadEnvelope that matches a bid.
func makePayloadForEscrow(bid *BuilderBid) *PayloadEnvelope {
	return &PayloadEnvelope{
		Slot:            bid.Slot,
		BuilderIndex:    bid.BuilderIndex,
		PayloadRoot:     bid.BlockHash, // must match committed block hash
		BeaconBlockRoot: types.HexToHash("0xcc"),
		StateRoot:       types.HexToHash("0xdd"),
	}
}

func TestBidEscrowDeposit(t *testing.T) {
	escrow := NewBidEscrow(100)

	err := escrow.Deposit("builder1", 1000)
	if err != nil {
		t.Fatalf("Deposit: %v", err)
	}

	if got := escrow.GetBalance("builder1"); got != 1000 {
		t.Errorf("GetBalance = %d, want 1000", got)
	}
	if got := escrow.GetLockedBalance("builder1"); got != 0 {
		t.Errorf("GetLockedBalance = %d, want 0", got)
	}
}

func TestBidEscrowDepositZero(t *testing.T) {
	escrow := NewBidEscrow(100)

	err := escrow.Deposit("builder1", 0)
	if err == nil {
		t.Fatal("expected error for zero deposit")
	}
}

func TestBidEscrowDepositMultiple(t *testing.T) {
	escrow := NewBidEscrow(100)

	_ = escrow.Deposit("builder1", 500)
	_ = escrow.Deposit("builder1", 300)

	if got := escrow.GetBalance("builder1"); got != 800 {
		t.Errorf("GetBalance = %d, want 800", got)
	}
}

func TestBidEscrowPlaceBid(t *testing.T) {
	escrow := NewBidEscrow(100)
	_ = escrow.Deposit("1", 5000)

	bid := makeBidForEscrow(100, 1, 3000)
	err := escrow.PlaceBid(bid)
	if err != nil {
		t.Fatalf("PlaceBid: %v", err)
	}

	if got := escrow.GetBalance("1"); got != 2000 {
		t.Errorf("available = %d, want 2000", got)
	}
	if got := escrow.GetLockedBalance("1"); got != 3000 {
		t.Errorf("locked = %d, want 3000", got)
	}
}

func TestBidEscrowPlaceBidNil(t *testing.T) {
	escrow := NewBidEscrow(100)

	err := escrow.PlaceBid(nil)
	if err == nil {
		t.Fatal("expected error for nil bid")
	}
}

func TestBidEscrowPlaceBidInsufficientFunds(t *testing.T) {
	escrow := NewBidEscrow(100)
	_ = escrow.Deposit("1", 100)

	bid := makeBidForEscrow(100, 1, 500)
	err := escrow.PlaceBid(bid)
	if err == nil {
		t.Fatal("expected error for insufficient funds")
	}
}

func TestBidEscrowPlaceBidDuplicate(t *testing.T) {
	escrow := NewBidEscrow(100)
	_ = escrow.Deposit("1", 5000)

	bid1 := makeBidForEscrow(100, 1, 1000)
	_ = escrow.PlaceBid(bid1)

	bid2 := makeBidForEscrow(100, 1, 500)
	err := escrow.PlaceBid(bid2)
	if err == nil {
		t.Fatal("expected error for duplicate bid on same slot")
	}
}

func TestBidEscrowRevealPayload(t *testing.T) {
	escrow := NewBidEscrow(100)
	_ = escrow.Deposit("1", 5000)

	bid := makeBidForEscrow(100, 1, 3000)
	_ = escrow.PlaceBid(bid)

	payload := makePayloadForEscrow(bid)
	err := escrow.RevealPayload(100, "1", payload)
	if err != nil {
		t.Fatalf("RevealPayload: %v", err)
	}

	state, found := escrow.GetBidState(100)
	if !found {
		t.Fatal("bid not found after reveal")
	}
	if state != EscrowBidRevealed {
		t.Errorf("state = %s, want revealed", state)
	}
}

func TestBidEscrowRevealPayloadNil(t *testing.T) {
	escrow := NewBidEscrow(100)

	err := escrow.RevealPayload(100, "1", nil)
	if err == nil {
		t.Fatal("expected error for nil payload")
	}
}

func TestBidEscrowRevealPayloadNoBid(t *testing.T) {
	escrow := NewBidEscrow(100)

	payload := &PayloadEnvelope{
		Slot:            100,
		PayloadRoot:     types.HexToHash("0xaa"),
		BeaconBlockRoot: types.HexToHash("0xcc"),
		StateRoot:       types.HexToHash("0xdd"),
	}
	err := escrow.RevealPayload(100, "1", payload)
	if err == nil {
		t.Fatal("expected error for no bid")
	}
}

func TestBidEscrowRevealPayloadMismatch(t *testing.T) {
	escrow := NewBidEscrow(100)
	_ = escrow.Deposit("1", 5000)

	bid := makeBidForEscrow(100, 1, 3000)
	_ = escrow.PlaceBid(bid)

	// Payload with wrong root.
	payload := &PayloadEnvelope{
		Slot:            100,
		BuilderIndex:    1,
		PayloadRoot:     types.HexToHash("0xff"), // does not match bid.BlockHash
		BeaconBlockRoot: types.HexToHash("0xcc"),
		StateRoot:       types.HexToHash("0xdd"),
	}
	err := escrow.RevealPayload(100, "1", payload)
	if err == nil {
		t.Fatal("expected error for payload root mismatch")
	}
}

func TestBidEscrowRevealPayloadWrongBuilder(t *testing.T) {
	escrow := NewBidEscrow(100)
	_ = escrow.Deposit("1", 5000)

	bid := makeBidForEscrow(100, 1, 3000)
	_ = escrow.PlaceBid(bid)

	payload := makePayloadForEscrow(bid)
	err := escrow.RevealPayload(100, "2", payload) // wrong builder
	if err == nil {
		t.Fatal("expected error for wrong builder")
	}
}

func TestBidEscrowSettleBidSuccess(t *testing.T) {
	escrow := NewBidEscrow(100)
	_ = escrow.Deposit("1", 5000)

	bid := makeBidForEscrow(100, 1, 3000)
	_ = escrow.PlaceBid(bid)

	payload := makePayloadForEscrow(bid)
	_ = escrow.RevealPayload(100, "1", payload)

	result, err := escrow.SettleBid(100)
	if err != nil {
		t.Fatalf("SettleBid: %v", err)
	}

	if !result.Success {
		t.Error("expected settlement success")
	}
	if result.AmountReleased != 3000 {
		t.Errorf("AmountReleased = %d, want 3000", result.AmountReleased)
	}
	if result.AmountSlashed != 0 {
		t.Errorf("AmountSlashed = %d, want 0", result.AmountSlashed)
	}

	// Balance should be fully available again.
	if got := escrow.GetBalance("1"); got != 5000 {
		t.Errorf("available after settle = %d, want 5000", got)
	}
	if got := escrow.GetLockedBalance("1"); got != 0 {
		t.Errorf("locked after settle = %d, want 0", got)
	}
}

func TestBidEscrowSettleBidSlash(t *testing.T) {
	escrow := NewBidEscrow(100)
	_ = escrow.Deposit("1", 5000)

	bid := makeBidForEscrow(200, 1, 2000)
	_ = escrow.PlaceBid(bid)

	// Do NOT reveal payload -> settle should slash.
	result, err := escrow.SettleBid(200)
	if err != nil {
		t.Fatalf("SettleBid: %v", err)
	}

	if result.Success {
		t.Error("expected settlement failure (slash)")
	}
	if result.AmountSlashed != 2000 {
		t.Errorf("AmountSlashed = %d, want 2000", result.AmountSlashed)
	}
	if result.AmountReleased != 0 {
		t.Errorf("AmountReleased = %d, want 0", result.AmountReleased)
	}

	// Locked should be zero, available should be original minus slashed.
	if got := escrow.GetBalance("1"); got != 3000 {
		t.Errorf("available after slash = %d, want 3000", got)
	}
	if got := escrow.GetLockedBalance("1"); got != 0 {
		t.Errorf("locked after slash = %d, want 0", got)
	}
}

func TestBidEscrowSettleBidNoBid(t *testing.T) {
	escrow := NewBidEscrow(100)

	_, err := escrow.SettleBid(999)
	if err == nil {
		t.Fatal("expected error for settling nonexistent bid")
	}
}

func TestBidEscrowSettleBidAlreadySettled(t *testing.T) {
	escrow := NewBidEscrow(100)
	_ = escrow.Deposit("1", 5000)

	bid := makeBidForEscrow(100, 1, 1000)
	_ = escrow.PlaceBid(bid)
	_, _ = escrow.SettleBid(100)

	_, err := escrow.SettleBid(100)
	if err == nil {
		t.Fatal("expected error for already settled bid")
	}
}

func TestBidEscrowSlashBuilder(t *testing.T) {
	escrow := NewBidEscrow(100)
	_ = escrow.Deposit("builder1", 5000)

	err := escrow.SlashBuilder("builder1", 2000, "equivocation")
	if err != nil {
		t.Fatalf("SlashBuilder: %v", err)
	}

	if got := escrow.GetBalance("builder1"); got != 3000 {
		t.Errorf("available after slash = %d, want 3000", got)
	}
}

func TestBidEscrowSlashBuilderFromLocked(t *testing.T) {
	escrow := NewBidEscrow(100)
	_ = escrow.Deposit("1", 3000)

	// Place a bid to lock some balance.
	bid := makeBidForEscrow(100, 1, 2000)
	_ = escrow.PlaceBid(bid)

	// Available is 1000, locked is 2000. Slash 2500 -> takes from available + locked.
	err := escrow.SlashBuilder("1", 2500, "severe violation")
	if err != nil {
		t.Fatalf("SlashBuilder: %v", err)
	}

	if got := escrow.GetBalance("1"); got != 0 {
		t.Errorf("available = %d, want 0", got)
	}
	if got := escrow.GetLockedBalance("1"); got != 500 {
		t.Errorf("locked = %d, want 500", got)
	}
}

func TestBidEscrowSlashBuilderUnknown(t *testing.T) {
	escrow := NewBidEscrow(100)

	err := escrow.SlashBuilder("unknown", 100, "test")
	if err == nil {
		t.Fatal("expected error for unknown builder")
	}
}

func TestBidEscrowSlashBuilderZero(t *testing.T) {
	escrow := NewBidEscrow(100)

	err := escrow.SlashBuilder("builder1", 0, "test")
	if err == nil {
		t.Fatal("expected error for zero slash amount")
	}
}

func TestBidEscrowWithdrawBalance(t *testing.T) {
	escrow := NewBidEscrow(100)
	_ = escrow.Deposit("builder1", 5000)

	err := escrow.WithdrawBalance("builder1", 3000)
	if err != nil {
		t.Fatalf("WithdrawBalance: %v", err)
	}

	if got := escrow.GetBalance("builder1"); got != 2000 {
		t.Errorf("available after withdraw = %d, want 2000", got)
	}
}

func TestBidEscrowWithdrawBalanceInsufficient(t *testing.T) {
	escrow := NewBidEscrow(100)
	_ = escrow.Deposit("builder1", 1000)

	err := escrow.WithdrawBalance("builder1", 2000)
	if err == nil {
		t.Fatal("expected error for insufficient withdraw")
	}
}

func TestBidEscrowWithdrawBalanceZero(t *testing.T) {
	escrow := NewBidEscrow(100)

	err := escrow.WithdrawBalance("builder1", 0)
	if err == nil {
		t.Fatal("expected error for zero withdraw")
	}
}

func TestBidEscrowWithdrawBalanceUnknown(t *testing.T) {
	escrow := NewBidEscrow(100)

	err := escrow.WithdrawBalance("unknown", 100)
	if err == nil {
		t.Fatal("expected error for unknown builder")
	}
}

func TestBidEscrowGetBidState(t *testing.T) {
	escrow := NewBidEscrow(100)
	_ = escrow.Deposit("1", 5000)

	// No bid -> not found.
	_, found := escrow.GetBidState(100)
	if found {
		t.Error("expected not found for no bid")
	}

	bid := makeBidForEscrow(100, 1, 1000)
	_ = escrow.PlaceBid(bid)

	state, found := escrow.GetBidState(100)
	if !found {
		t.Fatal("expected bid to be found")
	}
	if state != EscrowBidPending {
		t.Errorf("state = %s, want pending", state)
	}
}

func TestBidEscrowGetBid(t *testing.T) {
	escrow := NewBidEscrow(100)
	_ = escrow.Deposit("1", 5000)

	// No bid -> nil.
	if got := escrow.GetBid(100); got != nil {
		t.Error("expected nil for no bid")
	}

	bid := makeBidForEscrow(100, 1, 2000)
	_ = escrow.PlaceBid(bid)

	got := escrow.GetBid(100)
	if got == nil {
		t.Fatal("expected bid to be returned")
	}
	if got.Value != 2000 {
		t.Errorf("bid value = %d, want 2000", got.Value)
	}

	// Ensure returned value is a copy.
	got.Value = 9999
	original := escrow.GetBid(100)
	if original.Value != 2000 {
		t.Error("GetBid should return a copy, original was mutated")
	}
}

func TestBidEscrowActiveBidCount(t *testing.T) {
	escrow := NewBidEscrow(100)
	_ = escrow.Deposit("1", 50000)

	if escrow.ActiveBidCount() != 0 {
		t.Errorf("ActiveBidCount = %d, want 0", escrow.ActiveBidCount())
	}

	// Place 3 bids.
	for i := uint64(1); i <= 3; i++ {
		bid := makeBidForEscrow(i, 1, 1000)
		_ = escrow.PlaceBid(bid)
	}

	if escrow.ActiveBidCount() != 3 {
		t.Errorf("ActiveBidCount = %d, want 3", escrow.ActiveBidCount())
	}

	// Settle one (slash).
	_, _ = escrow.SettleBid(1)

	if escrow.ActiveBidCount() != 2 {
		t.Errorf("ActiveBidCount after settle = %d, want 2", escrow.ActiveBidCount())
	}
}

func TestBidEscrowSettlementHistory(t *testing.T) {
	escrow := NewBidEscrow(100)
	_ = escrow.Deposit("1", 50000)

	// Place and settle 5 bids.
	for i := uint64(1); i <= 5; i++ {
		bid := makeBidForEscrow(i, 1, 1000)
		_ = escrow.PlaceBid(bid)
		_, _ = escrow.SettleBid(i)
	}

	history := escrow.SettlementHistory(3)
	if len(history) != 3 {
		t.Fatalf("SettlementHistory len = %d, want 3", len(history))
	}

	// Should be the last 3 results.
	if history[0].Slot != 3 {
		t.Errorf("history[0].Slot = %d, want 3", history[0].Slot)
	}
	if history[2].Slot != 5 {
		t.Errorf("history[2].Slot = %d, want 5", history[2].Slot)
	}

	// Request more than available.
	all := escrow.SettlementHistory(100)
	if len(all) != 5 {
		t.Errorf("SettlementHistory(100) len = %d, want 5", len(all))
	}

	// Zero or negative.
	if got := escrow.SettlementHistory(0); got != nil {
		t.Errorf("SettlementHistory(0) = %v, want nil", got)
	}
}

func TestBidEscrowPruneBefore(t *testing.T) {
	escrow := NewBidEscrow(100)
	_ = escrow.Deposit("1", 50000)

	// Place and settle bids at slots 10, 20, 30.
	for _, slot := range []uint64{10, 20, 30} {
		bid := makeBidForEscrow(slot, 1, 1000)
		_ = escrow.PlaceBid(bid)
		_, _ = escrow.SettleBid(slot)
	}

	// Place an unsettled bid at slot 5.
	bid := makeBidForEscrow(5, 1, 500)
	_ = escrow.PlaceBid(bid)

	// Prune before slot 25: should remove slots 10, 20 (settled), but not 5 (active).
	pruned := escrow.PruneBefore(25)
	if pruned != 2 {
		t.Errorf("PruneBefore = %d, want 2", pruned)
	}

	// Slot 5 should still exist (active).
	if _, found := escrow.GetBidState(5); !found {
		t.Error("active bid at slot 5 should not be pruned")
	}

	// Slot 30 should still exist.
	if _, found := escrow.GetBidState(30); !found {
		t.Error("bid at slot 30 should not be pruned")
	}
}

func TestBidEscrowMultipleDepositors(t *testing.T) {
	escrow := NewBidEscrow(100)
	_ = escrow.Deposit("alice", 1000)
	_ = escrow.Deposit("bob", 2000)

	if got := escrow.GetBalance("alice"); got != 1000 {
		t.Errorf("alice balance = %d, want 1000", got)
	}
	if got := escrow.GetBalance("bob"); got != 2000 {
		t.Errorf("bob balance = %d, want 2000", got)
	}
}

func TestBidEscrowFullLifecycle(t *testing.T) {
	escrow := NewBidEscrow(100)

	// Builder deposits.
	_ = escrow.Deposit("1", 10000)

	// Place bid.
	bid := makeBidForEscrow(500, 1, 5000)
	err := escrow.PlaceBid(bid)
	if err != nil {
		t.Fatalf("PlaceBid: %v", err)
	}

	// Verify balances.
	if got := escrow.GetBalance("1"); got != 5000 {
		t.Errorf("available after bid = %d, want 5000", got)
	}
	if got := escrow.GetLockedBalance("1"); got != 5000 {
		t.Errorf("locked after bid = %d, want 5000", got)
	}

	// Reveal payload.
	payload := makePayloadForEscrow(bid)
	err = escrow.RevealPayload(500, "1", payload)
	if err != nil {
		t.Fatalf("RevealPayload: %v", err)
	}

	// Settle bid.
	result, err := escrow.SettleBid(500)
	if err != nil {
		t.Fatalf("SettleBid: %v", err)
	}

	if !result.Success {
		t.Error("expected successful settlement")
	}

	// All collateral should be available again.
	if got := escrow.GetBalance("1"); got != 10000 {
		t.Errorf("available after settlement = %d, want 10000", got)
	}
	if got := escrow.GetLockedBalance("1"); got != 0 {
		t.Errorf("locked after settlement = %d, want 0", got)
	}
}

func TestBidEscrowRevealAlreadyRevealed(t *testing.T) {
	escrow := NewBidEscrow(100)
	_ = escrow.Deposit("1", 5000)

	bid := makeBidForEscrow(100, 1, 2000)
	_ = escrow.PlaceBid(bid)

	payload := makePayloadForEscrow(bid)
	_ = escrow.RevealPayload(100, "1", payload)

	// Second reveal should fail.
	err := escrow.RevealPayload(100, "1", payload)
	if err == nil {
		t.Fatal("expected error for already revealed payload")
	}
}

func TestBidEscrowMultipleBuilders(t *testing.T) {
	escrow := NewBidEscrow(100)
	_ = escrow.Deposit("1", 10000)
	_ = escrow.Deposit("2", 8000)

	// Builder 1 bids on slot 100.
	bid1 := makeBidForEscrow(100, 1, 4000)
	_ = escrow.PlaceBid(bid1)

	// Builder 2 bids on slot 200.
	bid2 := makeBidForEscrow(200, 2, 3000)
	_ = escrow.PlaceBid(bid2)

	// Check balances are independent.
	if got := escrow.GetBalance("1"); got != 6000 {
		t.Errorf("builder1 available = %d, want 6000", got)
	}
	if got := escrow.GetBalance("2"); got != 5000 {
		t.Errorf("builder2 available = %d, want 5000", got)
	}

	if escrow.ActiveBidCount() != 2 {
		t.Errorf("ActiveBidCount = %d, want 2", escrow.ActiveBidCount())
	}
}

func TestBidEscrowGetBalanceUnknownBuilder(t *testing.T) {
	escrow := NewBidEscrow(100)

	if got := escrow.GetBalance("unknown"); got != 0 {
		t.Errorf("GetBalance(unknown) = %d, want 0", got)
	}
	if got := escrow.GetLockedBalance("unknown"); got != 0 {
		t.Errorf("GetLockedBalance(unknown) = %d, want 0", got)
	}
}

func TestEscrowBidStateString(t *testing.T) {
	cases := []struct {
		state EscrowBidState
		want  string
	}{
		{EscrowBidPending, "pending"},
		{EscrowBidRevealed, "revealed"},
		{EscrowBidSettledSuccess, "settled_success"},
		{EscrowBidSettledSlashed, "settled_slashed"},
		{EscrowBidState(99), "unknown"},
	}
	for _, c := range cases {
		if got := c.state.String(); got != c.want {
			t.Errorf("%d.String() = %q, want %q", c.state, got, c.want)
		}
	}
}

func TestBidEscrowResultTrimming(t *testing.T) {
	escrow := NewBidEscrow(3) // max 3 results
	_ = escrow.Deposit("1", 100000)

	// Place and settle 5 bids.
	for i := uint64(1); i <= 5; i++ {
		bid := makeBidForEscrow(i, 1, 100)
		_ = escrow.PlaceBid(bid)
		_, _ = escrow.SettleBid(i)
	}

	history := escrow.SettlementHistory(10)
	if len(history) != 3 {
		t.Errorf("history len = %d, want 3 (capped at maxResults)", len(history))
	}

	// Should contain the last 3 settlements.
	if history[0].Slot != 3 {
		t.Errorf("oldest result slot = %d, want 3", history[0].Slot)
	}
}

func TestBidEscrowRevealSlotMismatch(t *testing.T) {
	escrow := NewBidEscrow(100)
	_ = escrow.Deposit("1", 5000)

	bid := makeBidForEscrow(100, 1, 2000)
	_ = escrow.PlaceBid(bid)

	// Payload with wrong slot.
	payload := &PayloadEnvelope{
		Slot:            999, // wrong slot
		BuilderIndex:    1,
		PayloadRoot:     bid.BlockHash,
		BeaconBlockRoot: types.HexToHash("0xcc"),
		StateRoot:       types.HexToHash("0xdd"),
	}
	err := escrow.RevealPayload(100, "1", payload)
	if err == nil {
		t.Fatal("expected error for slot mismatch")
	}
}

func TestBidEscrowNewBidEscrowDefaults(t *testing.T) {
	escrow := NewBidEscrow(-1)
	if escrow.maxResults != 1024 {
		t.Errorf("default maxResults = %d, want 1024", escrow.maxResults)
	}

	escrow2 := NewBidEscrow(0)
	if escrow2.maxResults != 1024 {
		t.Errorf("default maxResults (0) = %d, want 1024", escrow2.maxResults)
	}
}

func TestBidEscrowSlashBuilderZeroBalance(t *testing.T) {
	escrow := NewBidEscrow(100)
	_ = escrow.Deposit("builder1", 0)

	// Builder has zero balance.
	err := escrow.SlashBuilder("builder1", 100, "test")
	if err == nil {
		t.Fatal("expected error for slashing builder with zero balance")
	}
}

func TestBidEscrowConcurrentDeposits(t *testing.T) {
	escrow := NewBidEscrow(100)
	done := make(chan struct{})

	// Concurrent deposits.
	for i := 0; i < 10; i++ {
		go func(id int) {
			_ = escrow.Deposit(fmt.Sprintf("builder%d", id), 1000)
			done <- struct{}{}
		}(i)
	}
	for i := 0; i < 10; i++ {
		<-done
	}

	// Verify all deposits succeeded by checking balances.
	for i := 0; i < 10; i++ {
		if got := escrow.GetBalance(fmt.Sprintf("builder%d", i)); got != 1000 {
			t.Errorf("builder%d balance = %d, want 1000", i, got)
		}
	}
}
