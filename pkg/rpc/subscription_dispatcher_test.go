package rpc

import (
	"errors"
	"sync"
	"testing"
	"time"
)

func TestSubscriptionDispatcher_Subscribe(t *testing.T) {
	d := NewSubscriptionDispatcher(DefaultDispatcherConfig())

	sub, err := d.Subscribe("client-1", TopicNewHeads, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sub == nil {
		t.Fatal("expected non-nil subscription")
	}
	if sub.ID == "" {
		t.Fatal("expected non-empty ID")
	}
	if sub.ClientID != "client-1" {
		t.Fatalf("expected client-1, got %s", sub.ClientID)
	}
	if sub.Topic != TopicNewHeads {
		t.Fatalf("expected TopicNewHeads, got %s", sub.Topic)
	}
	if d.TotalSubscriptions() != 1 {
		t.Fatalf("expected 1 subscription, got %d", d.TotalSubscriptions())
	}
}

func TestSubscriptionDispatcher_SubscribeInvalidTopic(t *testing.T) {
	d := NewSubscriptionDispatcher(DefaultDispatcherConfig())

	_, err := d.Subscribe("client-1", "badTopic", nil)
	if !errors.Is(err, ErrDispatcherInvalidTopic) {
		t.Fatalf("expected ErrDispatcherInvalidTopic, got %v", err)
	}
}

func TestSubscriptionDispatcher_SubscribePerClientLimit(t *testing.T) {
	config := DefaultDispatcherConfig()
	config.MaxSubsPerClient = 2
	d := NewSubscriptionDispatcher(config)

	d.Subscribe("client-1", TopicNewHeads, nil)
	d.Subscribe("client-1", TopicLogs, nil)

	_, err := d.Subscribe("client-1", TopicPendingTxs, nil)
	if !errors.Is(err, ErrDispatcherClientLimit) {
		t.Fatalf("expected ErrDispatcherClientLimit, got %v", err)
	}

	// Different client should work.
	_, err = d.Subscribe("client-2", TopicNewHeads, nil)
	if err != nil {
		t.Fatalf("different client should succeed: %v", err)
	}
}

func TestSubscriptionDispatcher_SubscribeGlobalLimit(t *testing.T) {
	config := DefaultDispatcherConfig()
	config.MaxTotalSubs = 2
	config.MaxSubsPerClient = 10
	d := NewSubscriptionDispatcher(config)

	d.Subscribe("client-1", TopicNewHeads, nil)
	d.Subscribe("client-2", TopicLogs, nil)

	_, err := d.Subscribe("client-3", TopicPendingTxs, nil)
	if !errors.Is(err, ErrDispatcherClientLimit) {
		t.Fatalf("expected ErrDispatcherClientLimit, got %v", err)
	}
}

func TestSubscriptionDispatcher_Unsubscribe(t *testing.T) {
	d := NewSubscriptionDispatcher(DefaultDispatcherConfig())
	sub, _ := d.Subscribe("client-1", TopicNewHeads, nil)

	if err := d.Unsubscribe(sub.ID); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d.TotalSubscriptions() != 0 {
		t.Fatalf("expected 0 subscriptions, got %d", d.TotalSubscriptions())
	}
}

func TestSubscriptionDispatcher_UnsubscribeNotFound(t *testing.T) {
	d := NewSubscriptionDispatcher(DefaultDispatcherConfig())

	err := d.Unsubscribe("0xdeadbeef")
	if !errors.Is(err, ErrDispatcherSubNotFound) {
		t.Fatalf("expected ErrDispatcherSubNotFound, got %v", err)
	}
}

func TestSubscriptionDispatcher_UnsubscribeDecrementsClientCount(t *testing.T) {
	d := NewSubscriptionDispatcher(DefaultDispatcherConfig())
	sub1, _ := d.Subscribe("client-1", TopicNewHeads, nil)
	d.Subscribe("client-1", TopicLogs, nil)

	if d.ClientCount() != 1 {
		t.Fatalf("expected 1 client, got %d", d.ClientCount())
	}

	d.Unsubscribe(sub1.ID)
	if d.ClientCount() != 1 {
		t.Fatalf("still expect 1 client with remaining sub, got %d", d.ClientCount())
	}
}

func TestSubscriptionDispatcher_Broadcast(t *testing.T) {
	d := NewSubscriptionDispatcher(DefaultDispatcherConfig())
	sub, _ := d.Subscribe("client-1", TopicNewHeads, nil)

	d.Broadcast(TopicNewHeads, "block-100")

	select {
	case msg := <-sub.Channel():
		if msg != "block-100" {
			t.Fatalf("expected block-100, got %v", msg)
		}
	default:
		t.Fatal("expected notification on channel")
	}
}

func TestSubscriptionDispatcher_BroadcastTopicFiltering(t *testing.T) {
	d := NewSubscriptionDispatcher(DefaultDispatcherConfig())
	headsSub, _ := d.Subscribe("client-1", TopicNewHeads, nil)
	logsSub, _ := d.Subscribe("client-1", TopicLogs, nil)

	d.Broadcast(TopicNewHeads, "head-event")

	// Heads subscription should receive.
	select {
	case <-headsSub.Channel():
		// Good.
	default:
		t.Fatal("expected heads notification")
	}

	// Logs subscription should NOT receive.
	select {
	case <-logsSub.Channel():
		t.Fatal("logs sub should not receive heads event")
	default:
		// Good.
	}
}

func TestSubscriptionDispatcher_BroadcastRateLimit(t *testing.T) {
	config := DefaultDispatcherConfig()
	config.MaxEventsPerSec = 2
	config.RateWindow = time.Hour // Large window so it never resets.
	d := NewSubscriptionDispatcher(config)

	sub, _ := d.Subscribe("client-1", TopicNewHeads, nil)

	// First two should be delivered.
	d.Broadcast(TopicNewHeads, "event-1")
	d.Broadcast(TopicNewHeads, "event-2")
	// Third should be rate-limited.
	d.Broadcast(TopicNewHeads, "event-3")

	received := 0
	for i := 0; i < 3; i++ {
		select {
		case <-sub.Channel():
			received++
		default:
		}
	}

	if received != 2 {
		t.Fatalf("expected 2 events delivered (rate limited), got %d", received)
	}
}

func TestSubscriptionDispatcher_BroadcastBufferFull(t *testing.T) {
	config := DefaultDispatcherConfig()
	config.BufferSize = 1
	config.MaxEventsPerSec = 0 // Disable rate limiting.
	d := NewSubscriptionDispatcher(config)

	sub, _ := d.Subscribe("client-1", TopicNewHeads, nil)

	// Fill the buffer.
	d.Broadcast(TopicNewHeads, "event-1")
	// This should be dropped (buffer full), not block.
	d.Broadcast(TopicNewHeads, "event-2")

	select {
	case msg := <-sub.Channel():
		if msg != "event-1" {
			t.Fatalf("expected event-1, got %v", msg)
		}
	default:
		t.Fatal("expected at least one event")
	}
}

func TestSubscriptionDispatcher_GetSubscriptions(t *testing.T) {
	d := NewSubscriptionDispatcher(DefaultDispatcherConfig())
	d.Subscribe("client-1", TopicNewHeads, nil)
	d.Subscribe("client-1", TopicLogs, nil)
	d.Subscribe("client-2", TopicPendingTxs, nil)

	subs := d.GetSubscriptions("client-1")
	if len(subs) != 2 {
		t.Fatalf("expected 2 subscriptions for client-1, got %d", len(subs))
	}

	subs2 := d.GetSubscriptions("client-2")
	if len(subs2) != 1 {
		t.Fatalf("expected 1 subscription for client-2, got %d", len(subs2))
	}

	subs3 := d.GetSubscriptions("client-3")
	if len(subs3) != 0 {
		t.Fatalf("expected 0 subscriptions for client-3, got %d", len(subs3))
	}
}

func TestSubscriptionDispatcher_GetSubscription(t *testing.T) {
	d := NewSubscriptionDispatcher(DefaultDispatcherConfig())
	sub, _ := d.Subscribe("client-1", TopicNewHeads, nil)

	got := d.GetSubscription(sub.ID)
	if got == nil {
		t.Fatal("expected non-nil subscription")
	}
	if got.Topic != TopicNewHeads {
		t.Fatalf("expected TopicNewHeads, got %s", got.Topic)
	}

	if d.GetSubscription("nonexistent") != nil {
		t.Fatal("expected nil for nonexistent subscription")
	}
}

func TestSubscriptionDispatcher_CleanupStale(t *testing.T) {
	d := NewSubscriptionDispatcher(DefaultDispatcherConfig())

	// Create subscriptions with backdated creation times.
	sub1, _ := d.Subscribe("client-1", TopicNewHeads, nil)
	sub2, _ := d.Subscribe("client-2", TopicLogs, nil)
	sub3, _ := d.Subscribe("client-3", TopicPendingTxs, nil)

	// Backdate sub1 and sub2 to make them stale.
	d.mu.Lock()
	d.subs[sub1.ID].Created = time.Now().Add(-10 * time.Minute)
	d.subs[sub2.ID].Created = time.Now().Add(-10 * time.Minute)
	// sub3 stays fresh.
	d.mu.Unlock()

	removed := d.CleanupStale(5 * time.Minute)
	if removed != 2 {
		t.Fatalf("expected 2 removed, got %d", removed)
	}
	if d.TotalSubscriptions() != 1 {
		t.Fatalf("expected 1 remaining, got %d", d.TotalSubscriptions())
	}

	// Verify sub3 is still there.
	if d.GetSubscription(sub3.ID) == nil {
		t.Fatal("expected sub3 to still be active")
	}
}

func TestSubscriptionDispatcher_CleanupStaleWithRecentActivity(t *testing.T) {
	d := NewSubscriptionDispatcher(DefaultDispatcherConfig())

	sub, _ := d.Subscribe("client-1", TopicNewHeads, nil)

	// Backdate creation but set recent LastEvent.
	d.mu.Lock()
	d.subs[sub.ID].Created = time.Now().Add(-10 * time.Minute)
	d.subs[sub.ID].LastEvent = time.Now()
	d.mu.Unlock()

	removed := d.CleanupStale(5 * time.Minute)
	if removed != 0 {
		t.Fatalf("expected 0 removed (recent activity), got %d", removed)
	}
}

func TestSubscriptionDispatcher_DisconnectClient(t *testing.T) {
	d := NewSubscriptionDispatcher(DefaultDispatcherConfig())
	d.Subscribe("client-1", TopicNewHeads, nil)
	d.Subscribe("client-1", TopicLogs, nil)
	d.Subscribe("client-2", TopicPendingTxs, nil)

	removed := d.DisconnectClient("client-1")
	if removed != 2 {
		t.Fatalf("expected 2 removed, got %d", removed)
	}
	if d.TotalSubscriptions() != 1 {
		t.Fatalf("expected 1 remaining, got %d", d.TotalSubscriptions())
	}
	if d.ClientCount() != 1 {
		t.Fatalf("expected 1 client remaining, got %d", d.ClientCount())
	}
}

func TestSubscriptionDispatcher_SubscriptionStats(t *testing.T) {
	d := NewSubscriptionDispatcher(DefaultDispatcherConfig())
	d.Subscribe("client-1", TopicNewHeads, nil)
	d.Subscribe("client-2", TopicNewHeads, nil)
	d.Subscribe("client-1", TopicLogs, nil)
	d.Subscribe("client-3", TopicPendingTxs, nil)
	d.Subscribe("client-1", TopicSyncing, nil)

	stats := d.SubscriptionStats()
	if stats.Total != 5 {
		t.Fatalf("expected total=5, got %d", stats.Total)
	}
	if stats.NewHeads != 2 {
		t.Fatalf("expected newHeads=2, got %d", stats.NewHeads)
	}
	if stats.Logs != 1 {
		t.Fatalf("expected logs=1, got %d", stats.Logs)
	}
	if stats.PendingTxs != 1 {
		t.Fatalf("expected pendingTxs=1, got %d", stats.PendingTxs)
	}
	if stats.Syncing != 1 {
		t.Fatalf("expected syncing=1, got %d", stats.Syncing)
	}
	if stats.Clients != 3 {
		t.Fatalf("expected clients=3, got %d", stats.Clients)
	}
}

func TestSubscriptionDispatcher_Close(t *testing.T) {
	d := NewSubscriptionDispatcher(DefaultDispatcherConfig())
	d.Subscribe("client-1", TopicNewHeads, nil)
	d.Subscribe("client-2", TopicLogs, nil)

	d.Close()

	if !d.IsClosed() {
		t.Fatal("expected dispatcher to be closed")
	}
	if d.TotalSubscriptions() != 0 {
		t.Fatalf("expected 0 subscriptions after close, got %d", d.TotalSubscriptions())
	}

	// New subscriptions should fail.
	_, err := d.Subscribe("client-3", TopicNewHeads, nil)
	if !errors.Is(err, ErrDispatcherClosed) {
		t.Fatalf("expected ErrDispatcherClosed, got %v", err)
	}
}

func TestSubscriptionDispatcher_BroadcastAfterClose(t *testing.T) {
	d := NewSubscriptionDispatcher(DefaultDispatcherConfig())
	d.Close()

	// Should not panic.
	d.Broadcast(TopicNewHeads, "data")
}

func TestSubscriptionDispatcher_CheckClientRateLimit(t *testing.T) {
	config := DefaultDispatcherConfig()
	config.MaxEventsPerSec = 3
	config.RateWindow = time.Hour
	d := NewSubscriptionDispatcher(config)

	d.Subscribe("client-1", TopicNewHeads, nil)

	// Simulate events by broadcasting.
	d.Broadcast(TopicNewHeads, "e1")
	d.Broadcast(TopicNewHeads, "e2")
	d.Broadcast(TopicNewHeads, "e3")

	// Client should now be at the limit.
	if d.CheckClientRateLimit("client-1") {
		t.Fatal("expected client to be rate limited")
	}

	// Unknown client should pass.
	if !d.CheckClientRateLimit("unknown") {
		t.Fatal("unknown client should not be rate limited")
	}
}

func TestSubscriptionDispatcher_EventCounter(t *testing.T) {
	d := NewSubscriptionDispatcher(DefaultDispatcherConfig())
	sub, _ := d.Subscribe("client-1", TopicNewHeads, nil)

	d.Broadcast(TopicNewHeads, "e1")
	d.Broadcast(TopicNewHeads, "e2")
	d.Broadcast(TopicNewHeads, "e3")

	// Drain the channel.
	for i := 0; i < 3; i++ {
		<-sub.Channel()
	}

	got := d.GetSubscription(sub.ID)
	if got.Events != 3 {
		t.Fatalf("expected 3 events, got %d", got.Events)
	}
}

func TestSubscriptionDispatcher_IsValidTopic(t *testing.T) {
	if !IsValidTopic(TopicNewHeads) {
		t.Fatal("TopicNewHeads should be valid")
	}
	if !IsValidTopic(TopicLogs) {
		t.Fatal("TopicLogs should be valid")
	}
	if !IsValidTopic(TopicPendingTxs) {
		t.Fatal("TopicPendingTxs should be valid")
	}
	if !IsValidTopic(TopicSyncing) {
		t.Fatal("TopicSyncing should be valid")
	}
	if IsValidTopic("invalid") {
		t.Fatal("invalid topic should not be valid")
	}
}

func TestSubscriptionDispatcher_ConcurrentAccess(t *testing.T) {
	d := NewSubscriptionDispatcher(DefaultDispatcherConfig())

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(3)
		clientID := "client-" + string(rune('A'+i%5))
		go func(cid string) {
			defer wg.Done()
			d.Subscribe(cid, TopicNewHeads, nil)
		}(clientID)
		go func() {
			defer wg.Done()
			d.Broadcast(TopicNewHeads, "event")
		}()
		go func() {
			defer wg.Done()
			_ = d.SubscriptionStats()
		}()
	}
	wg.Wait()

	if d.TotalSubscriptions() == 0 {
		t.Fatal("expected some subscriptions")
	}
}
