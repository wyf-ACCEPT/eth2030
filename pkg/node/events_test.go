package node

import (
	"sync"
	"testing"
	"time"
)

func TestSubscribeAndPublish(t *testing.T) {
	bus := NewEventBus(10)
	defer bus.Close()

	sub := bus.Subscribe(EventNewBlock)

	bus.Publish(EventNewBlock, "block-1")

	select {
	case ev := <-sub.Chan():
		if ev.Type != EventNewBlock {
			t.Errorf("event type = %s, want %s", ev.Type, EventNewBlock)
		}
		if ev.Data != "block-1" {
			t.Errorf("event data = %v, want block-1", ev.Data)
		}
		if ev.Timestamp.IsZero() {
			t.Error("event timestamp should not be zero")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
	}
}

func TestUnsubscribe(t *testing.T) {
	bus := NewEventBus(10)
	defer bus.Close()

	sub := bus.Subscribe(EventNewTx)
	bus.Unsubscribe(sub)

	// Channel should be closed after unsubscribe.
	_, ok := <-sub.Chan()
	if ok {
		t.Error("expected channel to be closed after unsubscribe")
	}

	// Double unsubscribe should not panic.
	bus.Unsubscribe(sub)
	sub.Unsubscribe()
}

func TestMultipleSubscribers(t *testing.T) {
	bus := NewEventBus(10)
	defer bus.Close()

	sub1 := bus.Subscribe(EventNewBlock)
	sub2 := bus.Subscribe(EventNewBlock)

	bus.Publish(EventNewBlock, "block-2")

	for _, sub := range []*Subscription{sub1, sub2} {
		select {
		case ev := <-sub.Chan():
			if ev.Data != "block-2" {
				t.Errorf("event data = %v, want block-2", ev.Data)
			}
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for event")
		}
	}
}

func TestEventTypeFiltering(t *testing.T) {
	bus := NewEventBus(10)
	defer bus.Close()

	blockSub := bus.Subscribe(EventNewBlock)
	txSub := bus.Subscribe(EventNewTx)

	bus.Publish(EventNewBlock, "block-data")
	bus.Publish(EventNewTx, "tx-data")

	// Block sub should only get block events.
	select {
	case ev := <-blockSub.Chan():
		if ev.Type != EventNewBlock {
			t.Errorf("block sub got type %s", ev.Type)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for block event")
	}

	// Tx sub should only get tx events.
	select {
	case ev := <-txSub.Chan():
		if ev.Type != EventNewTx {
			t.Errorf("tx sub got type %s", ev.Type)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for tx event")
	}

	// Block sub should not receive tx events.
	select {
	case ev := <-blockSub.Chan():
		t.Errorf("block sub should not receive tx event, got %v", ev)
	case <-time.After(50 * time.Millisecond):
		// Expected: no event.
	}
}

func TestSubscribeMultiple(t *testing.T) {
	bus := NewEventBus(10)
	defer bus.Close()

	sub := bus.SubscribeMultiple(EventNewBlock, EventNewTx, EventChainHead)

	bus.Publish(EventNewBlock, "block")
	bus.Publish(EventNewTx, "tx")
	bus.Publish(EventChainHead, "head")
	bus.Publish(EventDropPeer, "peer") // should not be received

	received := make(map[EventType]bool)
	for i := 0; i < 3; i++ {
		select {
		case ev := <-sub.Chan():
			received[ev.Type] = true
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for event")
		}
	}

	for _, et := range []EventType{EventNewBlock, EventNewTx, EventChainHead} {
		if !received[et] {
			t.Errorf("did not receive event type %s", et)
		}
	}

	// Should not receive EventDropPeer.
	select {
	case ev := <-sub.Chan():
		t.Errorf("unexpected event: %v", ev)
	case <-time.After(50 * time.Millisecond):
		// Expected.
	}
}

func TestPublishAsync(t *testing.T) {
	// Use buffer size 1 to test non-blocking behavior.
	bus := NewEventBus(1)
	defer bus.Close()

	sub := bus.Subscribe(EventNewBlock)

	// Fill the buffer.
	bus.PublishAsync(EventNewBlock, "event-1")

	// This should not block even though the buffer is full.
	bus.PublishAsync(EventNewBlock, "event-2")

	// Should receive at least the first event.
	select {
	case ev := <-sub.Chan():
		if ev.Data != "event-1" {
			t.Errorf("first event data = %v, want event-1", ev.Data)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
	}
}

func TestSubscriberCount(t *testing.T) {
	bus := NewEventBus(10)
	defer bus.Close()

	if count := bus.SubscriberCount(EventNewBlock); count != 0 {
		t.Errorf("initial count = %d, want 0", count)
	}

	sub1 := bus.Subscribe(EventNewBlock)
	sub2 := bus.Subscribe(EventNewBlock)
	_ = bus.Subscribe(EventNewTx) // different type

	if count := bus.SubscriberCount(EventNewBlock); count != 2 {
		t.Errorf("count after 2 subs = %d, want 2", count)
	}
	if count := bus.SubscriberCount(EventNewTx); count != 1 {
		t.Errorf("tx count = %d, want 1", count)
	}

	bus.Unsubscribe(sub1)
	if count := bus.SubscriberCount(EventNewBlock); count != 1 {
		t.Errorf("count after unsub = %d, want 1", count)
	}

	bus.Unsubscribe(sub2)
	if count := bus.SubscriberCount(EventNewBlock); count != 0 {
		t.Errorf("count after both unsub = %d, want 0", count)
	}
}

func TestCloseBus(t *testing.T) {
	bus := NewEventBus(10)

	sub1 := bus.Subscribe(EventNewBlock)
	sub2 := bus.Subscribe(EventNewTx)

	bus.Close()

	// All channels should be closed.
	for _, sub := range []*Subscription{sub1, sub2} {
		_, ok := <-sub.Chan()
		if ok {
			t.Error("expected channel to be closed after bus.Close()")
		}
	}

	// Publish after close should not panic.
	bus.Publish(EventNewBlock, "late-event")
	bus.PublishAsync(EventNewBlock, "late-async")

	// Subscribe after close should return a closed subscription.
	lateSub := bus.Subscribe(EventNewBlock)
	_, ok := <-lateSub.Chan()
	if ok {
		t.Error("expected late subscription channel to be closed")
	}

	// Double close should not panic.
	bus.Close()
}

func TestConcurrentAccess(t *testing.T) {
	bus := NewEventBus(100)
	defer bus.Close()

	const (
		numPublishers  = 10
		numSubscribers = 10
		numEvents      = 50
	)

	var wg sync.WaitGroup

	// Start subscribers.
	subs := make([]*Subscription, numSubscribers)
	for i := 0; i < numSubscribers; i++ {
		subs[i] = bus.Subscribe(EventNewBlock)
	}

	// Readers: drain events.
	for i := 0; i < numSubscribers; i++ {
		wg.Add(1)
		go func(sub *Subscription) {
			defer wg.Done()
			count := 0
			for range sub.Chan() {
				count++
				if count >= numPublishers*numEvents {
					return
				}
			}
		}(subs[i])
	}

	// Publishers: send events concurrently.
	for i := 0; i < numPublishers; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numEvents; j++ {
				bus.Publish(EventNewBlock, id*1000+j)
			}
		}(i)
	}

	// Wait for publishers to finish, then close all subs so readers exit.
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Success.
	case <-time.After(10 * time.Second):
		// Clean up subscriptions so goroutines can exit.
		for _, sub := range subs {
			bus.Unsubscribe(sub)
		}
		t.Fatal("timed out waiting for concurrent operations")
	}
}

func TestConcurrentSubscribeUnsubscribe(t *testing.T) {
	bus := NewEventBus(10)
	defer bus.Close()

	var wg sync.WaitGroup
	const iterations = 100

	// Concurrent subscribe/unsubscribe.
	for i := 0; i < iterations; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sub := bus.Subscribe(EventNewBlock)
			bus.PublishAsync(EventNewBlock, "data")
			bus.Unsubscribe(sub)
		}()
	}

	wg.Wait()

	// All subscriptions should be cleaned up.
	if count := bus.SubscriberCount(EventNewBlock); count != 0 {
		t.Errorf("subscriber count after cleanup = %d, want 0", count)
	}
}

func TestUnsubscribeNil(t *testing.T) {
	bus := NewEventBus(10)
	defer bus.Close()

	// Should not panic.
	bus.Unsubscribe(nil)
}

func TestSubscriptionConvenienceUnsubscribe(t *testing.T) {
	bus := NewEventBus(10)
	defer bus.Close()

	sub := bus.Subscribe(EventChainHead)
	sub.Unsubscribe()

	_, ok := <-sub.Chan()
	if ok {
		t.Error("expected channel to be closed after Unsubscribe()")
	}

	if count := bus.SubscriberCount(EventChainHead); count != 0 {
		t.Errorf("count = %d, want 0", count)
	}
}

func TestEventConstants(t *testing.T) {
	// Verify all event type constants are distinct.
	allTypes := []EventType{
		EventNewBlock, EventNewTx, EventChainHead, EventChainSideHead,
		EventNewPeer, EventDropPeer,
		EventSyncStarted, EventSyncCompleted,
		EventTxPoolAdd, EventTxPoolDrop,
	}

	seen := make(map[EventType]bool)
	for _, et := range allTypes {
		if seen[et] {
			t.Errorf("duplicate event type: %s", et)
		}
		seen[et] = true

		if et == "" {
			t.Error("event type should not be empty")
		}
	}
}
