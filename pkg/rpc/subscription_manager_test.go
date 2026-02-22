package rpc

import (
	"errors"
	"math/big"
	"testing"
	"time"

	"github.com/eth2030/eth2030/core/types"
)

// TestSubRegistry_NewHeadsSub tests creating a newHeads subscription.
func TestSubRegistry_NewHeadsSub(t *testing.T) {
	r := NewSubRegistry(DefaultSubRateLimitConfig(), 128)
	id, err := r.NewHeadsSub("conn-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id == "" {
		t.Fatal("expected non-empty subscription ID")
	}
	if r.Count() != 1 {
		t.Fatalf("want 1 sub, got %d", r.Count())
	}
}

// TestSubRegistry_LogsSub tests creating a logs subscription with filter.
func TestSubRegistry_LogsSub(t *testing.T) {
	r := NewSubRegistry(DefaultSubRateLimitConfig(), 128)
	query := FilterQuery{
		Addresses: []types.Address{types.HexToAddress("0xaaaa")},
	}
	id, err := r.LogsSub("conn-1", query)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id == "" {
		t.Fatal("expected non-empty subscription ID")
	}
	entry := r.GetSub(id)
	if entry == nil {
		t.Fatal("expected non-nil entry")
	}
	if entry.Kind != SubKindLogs {
		t.Fatalf("want SubKindLogs, got %d", entry.Kind)
	}
	if len(entry.Query.Addresses) != 1 {
		t.Fatalf("want 1 address, got %d", len(entry.Query.Addresses))
	}
}

// TestSubRegistry_PendingTxSub tests creating a pending tx subscription.
func TestSubRegistry_PendingTxSub(t *testing.T) {
	r := NewSubRegistry(DefaultSubRateLimitConfig(), 128)
	id, err := r.PendingTxSub("conn-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r.CountByKind(SubKindPendingTx) != 1 {
		t.Fatalf("want 1 pending tx sub, got %d", r.CountByKind(SubKindPendingTx))
	}
	_ = id
}

// TestSubRegistry_SyncStatusSub tests creating a sync status subscription.
func TestSubRegistry_SyncStatusSub(t *testing.T) {
	r := NewSubRegistry(DefaultSubRateLimitConfig(), 128)
	id, err := r.SyncStatusSub("conn-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r.CountByKind(SubKindSyncStatus) != 1 {
		t.Fatalf("want 1 sync sub, got %d", r.CountByKind(SubKindSyncStatus))
	}
	_ = id
}

// TestSubRegistry_Unsubscribe tests removing a subscription.
func TestSubRegistry_Unsubscribe(t *testing.T) {
	r := NewSubRegistry(DefaultSubRateLimitConfig(), 128)
	id, _ := r.NewHeadsSub("conn-1")

	err := r.Unsubscribe(id)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r.Count() != 0 {
		t.Fatalf("want 0 subs after unsubscribe, got %d", r.Count())
	}
}

// TestSubRegistry_Unsubscribe_NotFound tests removing a non-existent sub.
func TestSubRegistry_Unsubscribe_NotFound(t *testing.T) {
	r := NewSubRegistry(DefaultSubRateLimitConfig(), 128)
	err := r.Unsubscribe("0xdeadbeef")
	if !errors.Is(err, ErrSubManagerNotFound) {
		t.Fatalf("want ErrSubManagerNotFound, got %v", err)
	}
}

// TestSubRegistry_DisconnectConn tests cleanup on connection close.
func TestSubRegistry_DisconnectConn(t *testing.T) {
	r := NewSubRegistry(DefaultSubRateLimitConfig(), 128)
	r.NewHeadsSub("conn-1")
	r.LogsSub("conn-1", FilterQuery{})
	r.NewHeadsSub("conn-2")

	removed := r.DisconnectConn("conn-1")
	if removed != 2 {
		t.Fatalf("want 2 removed, got %d", removed)
	}
	if r.Count() != 1 {
		t.Fatalf("want 1 sub remaining, got %d", r.Count())
	}
	if r.ConnSubCount("conn-1") != 0 {
		t.Fatalf("want 0 subs for conn-1, got %d", r.ConnSubCount("conn-1"))
	}
}

// TestSubRegistry_RateLimit tests per-connection subscription rate limiting.
func TestSubRegistry_RateLimit(t *testing.T) {
	config := SubRateLimitConfig{
		MaxSubsPerConn:  2,
		WindowDuration:  1,
		MaxEventsPerSec: 100,
	}
	r := NewSubRegistry(config, 128)

	// First two should succeed.
	_, err := r.NewHeadsSub("conn-1")
	if err != nil {
		t.Fatalf("first sub should succeed: %v", err)
	}
	_, err = r.LogsSub("conn-1", FilterQuery{})
	if err != nil {
		t.Fatalf("second sub should succeed: %v", err)
	}

	// Third should be rate limited.
	_, err = r.PendingTxSub("conn-1")
	if !errors.Is(err, ErrSubManagerRateLimit) {
		t.Fatalf("want ErrSubManagerRateLimit, got %v", err)
	}

	// Different connection should still work.
	_, err = r.NewHeadsSub("conn-2")
	if err != nil {
		t.Fatalf("different conn should succeed: %v", err)
	}
}

// TestSubRegistry_NotifyNewHead tests head notifications reach subscribers.
func TestSubRegistry_NotifyNewHead(t *testing.T) {
	r := NewSubRegistry(DefaultSubRateLimitConfig(), 128)
	id, _ := r.NewHeadsSub("conn-1")

	header := &types.Header{
		Number:   big.NewInt(100),
		GasLimit: 30_000_000,
		Time:     1700000000,
	}
	r.NotifyNewHead(header)

	entry := r.GetSub(id)
	select {
	case msg := <-entry.Channel():
		block, ok := msg.(*RPCBlock)
		if !ok {
			t.Fatalf("expected *RPCBlock, got %T", msg)
		}
		if block.Number != "0x64" {
			t.Fatalf("want number 0x64, got %s", block.Number)
		}
	default:
		t.Fatal("expected notification on channel")
	}
}

// TestSubRegistry_NotifyLogEvents tests log notifications with filtering.
func TestSubRegistry_NotifyLogEvents(t *testing.T) {
	r := NewSubRegistry(DefaultSubRateLimitConfig(), 128)
	contractAddr := types.HexToAddress("0xcccc")
	query := FilterQuery{
		Addresses: []types.Address{contractAddr},
	}
	id, _ := r.LogsSub("conn-1", query)

	logs := []*types.Log{
		{Address: contractAddr, Topics: []types.Hash{}, BlockNumber: 42},
		{Address: types.HexToAddress("0xdddd"), Topics: []types.Hash{}, BlockNumber: 42},
	}
	r.NotifyLogEvents(logs)

	entry := r.GetSub(id)
	// Should receive exactly one log (matching address).
	select {
	case <-entry.Channel():
		// Good.
	default:
		t.Fatal("expected log notification")
	}

	// Should not have a second notification.
	select {
	case <-entry.Channel():
		t.Fatal("should not receive non-matching log")
	default:
		// Good.
	}
}

// TestSubRegistry_NotifyPendingTxHash tests pending tx notifications.
func TestSubRegistry_NotifyPendingTxHash(t *testing.T) {
	r := NewSubRegistry(DefaultSubRateLimitConfig(), 128)
	id, _ := r.PendingTxSub("conn-1")

	txHash := types.HexToHash("0xabcdef")
	r.NotifyPendingTxHash(txHash)

	entry := r.GetSub(id)
	select {
	case msg := <-entry.Channel():
		hashStr, ok := msg.(string)
		if !ok {
			t.Fatalf("expected string, got %T", msg)
		}
		if hashStr == "" {
			t.Fatal("expected non-empty hash string")
		}
	default:
		t.Fatal("expected pending tx notification")
	}
}

// TestSubRegistry_NotifySyncStatus tests sync notifications.
func TestSubRegistry_NotifySyncStatus(t *testing.T) {
	r := NewSubRegistry(DefaultSubRateLimitConfig(), 128)
	id, _ := r.SyncStatusSub("conn-1")

	r.NotifySyncStatus(true, 100, 200)
	entry := r.GetSub(id)
	select {
	case msg := <-entry.Channel():
		m, ok := msg.(map[string]string)
		if !ok {
			t.Fatalf("expected map, got %T", msg)
		}
		if m["currentBlock"] != "0x64" {
			t.Fatalf("want currentBlock 0x64, got %s", m["currentBlock"])
		}
	default:
		t.Fatal("expected sync notification")
	}

	// Test synced (false) notification.
	r.NotifySyncStatus(false, 200, 200)
	select {
	case msg := <-entry.Channel():
		if msg != false {
			t.Fatalf("expected false, got %v", msg)
		}
	default:
		t.Fatal("expected sync complete notification")
	}
}

// TestSubRegistry_Close tests closing the registry.
func TestSubRegistry_Close(t *testing.T) {
	r := NewSubRegistry(DefaultSubRateLimitConfig(), 128)
	r.NewHeadsSub("conn-1")
	r.LogsSub("conn-2", FilterQuery{})

	r.Close()

	if !r.IsClosed() {
		t.Fatal("expected registry to be closed")
	}
	if r.Count() != 0 {
		t.Fatalf("want 0 subs after close, got %d", r.Count())
	}

	// New subscriptions should fail.
	_, err := r.NewHeadsSub("conn-3")
	if !errors.Is(err, ErrSubManagerClosed) {
		t.Fatalf("want ErrSubManagerClosed, got %v", err)
	}
}

// TestSubRegistry_ConnSubCount tests per-connection counting.
func TestSubRegistry_ConnSubCount(t *testing.T) {
	r := NewSubRegistry(DefaultSubRateLimitConfig(), 128)
	r.NewHeadsSub("conn-1")
	r.LogsSub("conn-1", FilterQuery{})
	r.NewHeadsSub("conn-2")

	if r.ConnSubCount("conn-1") != 2 {
		t.Fatalf("want 2 subs for conn-1, got %d", r.ConnSubCount("conn-1"))
	}
	if r.ConnSubCount("conn-2") != 1 {
		t.Fatalf("want 1 sub for conn-2, got %d", r.ConnSubCount("conn-2"))
	}
	if r.ConnSubCount("conn-3") != 0 {
		t.Fatalf("want 0 subs for conn-3, got %d", r.ConnSubCount("conn-3"))
	}
}

// TestSubRegistry_ParseSubKind tests subscription type parsing.
func TestSubRegistry_ParseSubKind(t *testing.T) {
	tests := []struct {
		name    string
		want    SubKind
		wantErr bool
	}{
		{"newHeads", SubKindNewHeads, false},
		{"logs", SubKindLogs, false},
		{"newPendingTransactions", SubKindPendingTx, false},
		{"syncing", SubKindSyncStatus, false},
		{"badType", 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kind, err := ParseSubKind(tt.name)
			if tt.wantErr {
				if !errors.Is(err, ErrSubManagerInvalidTyp) {
					t.Fatalf("want ErrSubManagerInvalidTyp, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if kind != tt.want {
				t.Fatalf("want kind %d, got %d", tt.want, kind)
			}
		})
	}
}

// TestSubRegistry_CheckRateLimit tests event rate limiting.
func TestSubRegistry_CheckRateLimit(t *testing.T) {
	config := SubRateLimitConfig{
		MaxSubsPerConn:  10,
		WindowDuration:  time.Hour, // large window so it never resets
		MaxEventsPerSec: 2,
	}
	r := NewSubRegistry(config, 128)
	r.NewHeadsSub("conn-1")

	// First two events should pass.
	if !r.CheckRateLimit("conn-1") {
		t.Fatal("first event should pass")
	}
	if !r.CheckRateLimit("conn-1") {
		t.Fatal("second event should pass")
	}
	// Third should be rate limited (within same window).
	if r.CheckRateLimit("conn-1") {
		t.Fatal("third event should be rate limited")
	}
}

// TestSubRegistry_CountByKind tests counting by subscription kind.
func TestSubRegistry_CountByKind(t *testing.T) {
	r := NewSubRegistry(DefaultSubRateLimitConfig(), 128)
	r.NewHeadsSub("conn-1")
	r.NewHeadsSub("conn-2")
	r.LogsSub("conn-1", FilterQuery{})
	r.PendingTxSub("conn-3")

	if r.CountByKind(SubKindNewHeads) != 2 {
		t.Fatalf("want 2 newHeads, got %d", r.CountByKind(SubKindNewHeads))
	}
	if r.CountByKind(SubKindLogs) != 1 {
		t.Fatalf("want 1 logs, got %d", r.CountByKind(SubKindLogs))
	}
	if r.CountByKind(SubKindPendingTx) != 1 {
		t.Fatalf("want 1 pending tx, got %d", r.CountByKind(SubKindPendingTx))
	}
	if r.CountByKind(SubKindSyncStatus) != 0 {
		t.Fatalf("want 0 sync, got %d", r.CountByKind(SubKindSyncStatus))
	}
}
