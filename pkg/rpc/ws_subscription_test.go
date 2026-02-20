package rpc

import (
	"sync"
	"testing"
)

func TestWSSubscriptionManager_NewAndDefaults(t *testing.T) {
	cfg := DefaultSubscriptionConfig()
	if cfg.MaxSubscriptions != 256 {
		t.Fatalf("want MaxSubscriptions 256, got %d", cfg.MaxSubscriptions)
	}
	if cfg.BufferSize != 128 {
		t.Fatalf("want BufferSize 128, got %d", cfg.BufferSize)
	}
	if cfg.CleanupInterval != 300 {
		t.Fatalf("want CleanupInterval 300, got %d", cfg.CleanupInterval)
	}

	m := NewWSSubscriptionManager(cfg)
	if m == nil {
		t.Fatal("expected non-nil manager")
	}
}

func TestWSSubscriptionManager_Subscribe(t *testing.T) {
	m := NewWSSubscriptionManager(DefaultSubscriptionConfig())

	id, err := m.Subscribe("newHeads", nil)
	if err != nil {
		t.Fatalf("subscribe error: %v", err)
	}
	if id == "" {
		t.Fatal("expected non-empty subscription ID")
	}

	sub := m.GetSubscription(id)
	if sub == nil {
		t.Fatal("subscription not found")
	}
	if sub.Type != "newHeads" {
		t.Fatalf("want type newHeads, got %s", sub.Type)
	}
	if !sub.Active {
		t.Fatal("subscription should be active")
	}
	if sub.CreatedAt == 0 {
		t.Fatal("CreatedAt should be set")
	}
}

func TestWSSubscriptionManager_SubscribeAllTypes(t *testing.T) {
	m := NewWSSubscriptionManager(DefaultSubscriptionConfig())

	types := []string{"newHeads", "logs", "newPendingTransactions", "syncing"}
	for _, subType := range types {
		id, err := m.Subscribe(subType, nil)
		if err != nil {
			t.Fatalf("subscribe %q error: %v", subType, err)
		}
		sub := m.GetSubscription(id)
		if sub == nil {
			t.Fatalf("subscription %q not found", subType)
		}
		if sub.Type != subType {
			t.Fatalf("want type %s, got %s", subType, sub.Type)
		}
	}

	active := m.ActiveSubscriptions()
	if len(active) != 4 {
		t.Fatalf("want 4 active subscriptions, got %d", len(active))
	}
}

func TestWSSubscriptionManager_SubscribeUnsupportedType(t *testing.T) {
	m := NewWSSubscriptionManager(DefaultSubscriptionConfig())

	_, err := m.Subscribe("invalidType", nil)
	if err == nil {
		t.Fatal("expected error for unsupported type")
	}
}

func TestWSSubscriptionManager_SubscribeMaxReached(t *testing.T) {
	cfg := SubscriptionConfig{
		MaxSubscriptions: 2,
		BufferSize:       8,
		CleanupInterval:  300,
	}
	m := NewWSSubscriptionManager(cfg)

	_, err1 := m.Subscribe("newHeads", nil)
	if err1 != nil {
		t.Fatalf("subscribe 1: %v", err1)
	}
	_, err2 := m.Subscribe("logs", nil)
	if err2 != nil {
		t.Fatalf("subscribe 2: %v", err2)
	}

	_, err3 := m.Subscribe("syncing", nil)
	if err3 == nil {
		t.Fatal("expected error when max subscriptions reached")
	}
}

func TestWSSubscriptionManager_Unsubscribe(t *testing.T) {
	m := NewWSSubscriptionManager(DefaultSubscriptionConfig())

	id, _ := m.Subscribe("newHeads", nil)

	err := m.Unsubscribe(id)
	if err != nil {
		t.Fatalf("unsubscribe error: %v", err)
	}

	sub := m.GetSubscription(id)
	if sub != nil {
		t.Fatal("subscription should be removed after unsubscribe")
	}

	// Unsubscribing again should return error.
	err2 := m.Unsubscribe(id)
	if err2 == nil {
		t.Fatal("expected error for already-removed subscription")
	}
}

func TestWSSubscriptionManager_UnsubscribeClosesChannel(t *testing.T) {
	m := NewWSSubscriptionManager(DefaultSubscriptionConfig())

	id, _ := m.Subscribe("logs", nil)
	sub := m.GetSubscription(id)
	ch := sub.Channel()

	m.Unsubscribe(id)

	// Channel should be closed.
	_, open := <-ch
	if open {
		t.Fatal("channel should be closed after unsubscribe")
	}
}

func TestWSSubscriptionManager_GetSubscriptionNotFound(t *testing.T) {
	m := NewWSSubscriptionManager(DefaultSubscriptionConfig())

	sub := m.GetSubscription("0xnonexistent")
	if sub != nil {
		t.Fatal("expected nil for non-existent subscription")
	}
}

func TestWSSubscriptionManager_ActiveSubscriptions(t *testing.T) {
	m := NewWSSubscriptionManager(DefaultSubscriptionConfig())

	id1, _ := m.Subscribe("newHeads", nil)
	m.Subscribe("logs", map[string]interface{}{"address": "0xaaaa"})
	m.Subscribe("syncing", nil)

	active := m.ActiveSubscriptions()
	if len(active) != 3 {
		t.Fatalf("want 3 active, got %d", len(active))
	}

	// Unsubscribe one.
	m.Unsubscribe(id1)
	active = m.ActiveSubscriptions()
	if len(active) != 2 {
		t.Fatalf("want 2 active after unsubscribe, got %d", len(active))
	}
}

func TestWSSubscriptionManager_PublishEvent(t *testing.T) {
	m := NewWSSubscriptionManager(DefaultSubscriptionConfig())

	id1, _ := m.Subscribe("newHeads", nil)
	id2, _ := m.Subscribe("newHeads", nil)
	id3, _ := m.Subscribe("logs", nil) // should NOT receive

	m.PublishEvent("newHeads", "block-100")

	sub1 := m.GetSubscription(id1)
	sub2 := m.GetSubscription(id2)
	sub3 := m.GetSubscription(id3)

	// Both newHeads subs should get the event.
	for _, sub := range []*WSSubscription{sub1, sub2} {
		select {
		case msg := <-sub.Channel():
			if msg != "block-100" {
				t.Fatalf("want block-100, got %v", msg)
			}
		default:
			t.Fatal("expected event on newHeads channel")
		}
	}

	// Logs sub should not get the event.
	select {
	case <-sub3.Channel():
		t.Fatal("logs sub should not receive newHeads event")
	default:
		// Good.
	}
}

func TestWSSubscriptionManager_PublishEventNoSubscribers(t *testing.T) {
	m := NewWSSubscriptionManager(DefaultSubscriptionConfig())

	// Should not panic.
	m.PublishEvent("newHeads", "data")
}

func TestWSSubscriptionManager_PublishEventBufferFull(t *testing.T) {
	cfg := SubscriptionConfig{
		MaxSubscriptions: 10,
		BufferSize:       2,
		CleanupInterval:  300,
	}
	m := NewWSSubscriptionManager(cfg)

	id, _ := m.Subscribe("newHeads", nil)
	sub := m.GetSubscription(id)

	// Fill the buffer.
	m.PublishEvent("newHeads", "event1")
	m.PublishEvent("newHeads", "event2")

	// This should be dropped, not block.
	m.PublishEvent("newHeads", "event3")

	// Drain and count.
	count := 0
	for {
		select {
		case <-sub.Channel():
			count++
		default:
			goto done
		}
	}
done:
	if count != 2 {
		t.Fatalf("want 2 events (buffer size), got %d", count)
	}
}

func TestWSSubscriptionManager_SubscriberCount(t *testing.T) {
	m := NewWSSubscriptionManager(DefaultSubscriptionConfig())

	m.Subscribe("newHeads", nil)
	m.Subscribe("newHeads", nil)
	m.Subscribe("logs", nil)
	m.Subscribe("syncing", nil)

	if c := m.SubscriberCount("newHeads"); c != 2 {
		t.Fatalf("want 2 newHeads subscribers, got %d", c)
	}
	if c := m.SubscriberCount("logs"); c != 1 {
		t.Fatalf("want 1 logs subscriber, got %d", c)
	}
	if c := m.SubscriberCount("syncing"); c != 1 {
		t.Fatalf("want 1 syncing subscriber, got %d", c)
	}
	if c := m.SubscriberCount("newPendingTransactions"); c != 0 {
		t.Fatalf("want 0 pending tx subscribers, got %d", c)
	}
}

func TestWSSubscriptionManager_Cleanup(t *testing.T) {
	m := NewWSSubscriptionManager(DefaultSubscriptionConfig())

	id, _ := m.Subscribe("newHeads", nil)
	sub := m.GetSubscription(id)

	// Mark subscription as inactive manually.
	m.mu.Lock()
	sub.Active = false
	m.mu.Unlock()

	m.Cleanup()

	if got := m.GetSubscription(id); got != nil {
		t.Fatal("inactive subscription should be cleaned up")
	}
}

func TestWSSubscriptionManager_Close(t *testing.T) {
	m := NewWSSubscriptionManager(DefaultSubscriptionConfig())

	id1, _ := m.Subscribe("newHeads", nil)
	id2, _ := m.Subscribe("logs", nil)
	sub1 := m.GetSubscription(id1)
	sub2 := m.GetSubscription(id2)

	ch1 := sub1.Channel()
	ch2 := sub2.Channel()

	m.Close()

	// All channels should be closed.
	if _, open := <-ch1; open {
		t.Fatal("channel 1 should be closed after Close")
	}
	if _, open := <-ch2; open {
		t.Fatal("channel 2 should be closed after Close")
	}

	// No active subscriptions.
	active := m.ActiveSubscriptions()
	if len(active) != 0 {
		t.Fatalf("want 0 active after Close, got %d", len(active))
	}

	// New subscriptions should fail.
	_, err := m.Subscribe("newHeads", nil)
	if err == nil {
		t.Fatal("expected error after Close")
	}
}

func TestWSSubscriptionManager_FilterCriteria(t *testing.T) {
	m := NewWSSubscriptionManager(DefaultSubscriptionConfig())

	criteria := map[string]interface{}{
		"address": "0xaaaa",
		"topics":  []string{"0x1111"},
	}
	id, err := m.Subscribe("logs", criteria)
	if err != nil {
		t.Fatalf("subscribe error: %v", err)
	}

	sub := m.GetSubscription(id)
	if sub.FilterCriteria == nil {
		t.Fatal("filter criteria should not be nil")
	}
	if sub.FilterCriteria["address"] != "0xaaaa" {
		t.Fatalf("want address 0xaaaa, got %v", sub.FilterCriteria["address"])
	}
}

func TestWSSubscriptionManager_UniqueIDs(t *testing.T) {
	m := NewWSSubscriptionManager(DefaultSubscriptionConfig())

	ids := make(map[string]bool)
	for i := 0; i < 50; i++ {
		id, err := m.Subscribe("newHeads", nil)
		if err != nil {
			t.Fatalf("subscribe %d: %v", i, err)
		}
		if ids[id] {
			t.Fatalf("duplicate subscription ID: %s", id)
		}
		ids[id] = true
	}
}

func TestWSSubscriptionManager_ConcurrentOperations(t *testing.T) {
	m := NewWSSubscriptionManager(DefaultSubscriptionConfig())

	var wg sync.WaitGroup
	subIDs := make(chan string, 50)

	// Concurrently subscribe.
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			subType := "newHeads"
			if n%3 == 1 {
				subType = "logs"
			} else if n%3 == 2 {
				subType = "syncing"
			}
			id, err := m.Subscribe(subType, nil)
			if err != nil {
				t.Errorf("subscribe: %v", err)
				return
			}
			subIDs <- id
		}(i)
	}

	// Concurrently publish.
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m.PublishEvent("newHeads", "data")
			m.PublishEvent("logs", "logdata")
		}()
	}

	wg.Wait()
	close(subIDs)

	active := m.ActiveSubscriptions()
	if len(active) != 20 {
		t.Fatalf("want 20 active, got %d", len(active))
	}

	// Unsubscribe all concurrently.
	var wg2 sync.WaitGroup
	for id := range subIDs {
		wg2.Add(1)
		go func(sid string) {
			defer wg2.Done()
			m.Unsubscribe(sid)
		}(id)
	}
	wg2.Wait()

	active = m.ActiveSubscriptions()
	if len(active) != 0 {
		t.Fatalf("want 0 active after unsubscribe all, got %d", len(active))
	}
}

func TestWSSubscriptionManager_PublishMultipleEventTypes(t *testing.T) {
	m := NewWSSubscriptionManager(DefaultSubscriptionConfig())

	headID, _ := m.Subscribe("newHeads", nil)
	logID, _ := m.Subscribe("logs", nil)
	pendingID, _ := m.Subscribe("newPendingTransactions", nil)
	syncID, _ := m.Subscribe("syncing", nil)

	m.PublishEvent("newHeads", "head-data")
	m.PublishEvent("logs", "log-data")
	m.PublishEvent("newPendingTransactions", "tx-hash")
	m.PublishEvent("syncing", "sync-status")

	checks := map[string]string{
		headID:    "head-data",
		logID:     "log-data",
		pendingID: "tx-hash",
		syncID:    "sync-status",
	}

	for id, expected := range checks {
		sub := m.GetSubscription(id)
		select {
		case msg := <-sub.Channel():
			if msg != expected {
				t.Fatalf("sub %s: want %v, got %v", id, expected, msg)
			}
		default:
			t.Fatalf("sub %s: expected event", id)
		}
	}
}

func TestWSSubscriptionManager_ZeroConfig(t *testing.T) {
	// Zero-value config should get corrected to defaults.
	cfg := SubscriptionConfig{}
	m := NewWSSubscriptionManager(cfg)

	if m.config.MaxSubscriptions != 256 {
		t.Fatalf("want MaxSubscriptions 256, got %d", m.config.MaxSubscriptions)
	}
	if m.config.BufferSize != 128 {
		t.Fatalf("want BufferSize 128, got %d", m.config.BufferSize)
	}
	if m.config.CleanupInterval != 300 {
		t.Fatalf("want CleanupInterval 300, got %d", m.config.CleanupInterval)
	}
}
