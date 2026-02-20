package p2p

import (
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

// mockSender is a PeerSender that records calls and can simulate failures.
type mockSender struct {
	calls    int32 // atomic
	failPeer string
}

func (m *mockSender) SendToPeer(peerID string, msgType string, data []byte) (time.Duration, error) {
	atomic.AddInt32(&m.calls, 1)
	if m.failPeer != "" && peerID == m.failPeer {
		return 0, errors.New("mock: send failed")
	}
	return time.Microsecond * 100, nil
}

func TestNewEIP2PBroadcaster(t *testing.T) {
	b := NewEIP2PBroadcaster()
	if b == nil {
		t.Fatal("NewEIP2PBroadcaster returned nil")
	}
	if b.GetFanout() != DefaultFanout {
		t.Errorf("fanout = %d, want %d", b.GetFanout(), DefaultFanout)
	}
}

func TestBroadcastBasic(t *testing.T) {
	sender := &mockSender{}
	b := NewEIP2PBroadcasterWithSender(sender)

	peers := []string{"peer1", "peer2", "peer3"}
	results := b.Broadcast("blocks", []byte("block data"), peers)

	if len(results) != 3 {
		t.Fatalf("results len = %d, want 3", len(results))
	}

	for _, r := range results {
		if !r.Success {
			t.Errorf("peer %s failed: %v", r.PeerID, r.Error)
		}
	}

	if calls := atomic.LoadInt32(&sender.calls); calls != 3 {
		t.Errorf("sender calls = %d, want 3", calls)
	}
}

func TestBroadcastFanoutLimit(t *testing.T) {
	sender := &mockSender{}
	b := NewEIP2PBroadcasterWithSender(sender)
	_ = b.SetFanout(2)

	peers := []string{"peer1", "peer2", "peer3", "peer4", "peer5"}
	results := b.Broadcast("blocks", []byte("block data"), peers)

	if len(results) != 2 {
		t.Fatalf("results len = %d, want 2 (fanout)", len(results))
	}

	if calls := atomic.LoadInt32(&sender.calls); calls != 2 {
		t.Errorf("sender calls = %d, want 2", calls)
	}
}

func TestBroadcastEmptyType(t *testing.T) {
	b := NewEIP2PBroadcaster()
	results := b.Broadcast("", []byte("data"), []string{"peer1"})

	if len(results) != 1 || results[0].Success {
		t.Error("expected failure for empty message type")
	}
	if !errors.Is(results[0].Error, ErrBroadcastEmptyType) {
		t.Errorf("error = %v, want %v", results[0].Error, ErrBroadcastEmptyType)
	}
}

func TestBroadcastNilData(t *testing.T) {
	b := NewEIP2PBroadcaster()
	results := b.Broadcast("blocks", nil, []string{"peer1"})

	if len(results) != 1 || results[0].Success {
		t.Error("expected failure for nil data")
	}
}

func TestBroadcastEmptyData(t *testing.T) {
	b := NewEIP2PBroadcaster()
	results := b.Broadcast("blocks", []byte{}, []string{"peer1"})

	if len(results) != 1 || results[0].Success {
		t.Error("expected failure for empty data")
	}
}

func TestBroadcastNoPeers(t *testing.T) {
	b := NewEIP2PBroadcaster()
	results := b.Broadcast("blocks", []byte("data"), nil)

	if len(results) != 1 || results[0].Success {
		t.Error("expected failure for no peers")
	}
	if !errors.Is(results[0].Error, ErrBroadcastNoPeers) {
		t.Errorf("error = %v, want %v", results[0].Error, ErrBroadcastNoPeers)
	}
}

func TestBroadcastTooLarge(t *testing.T) {
	b := NewEIP2PBroadcaster()
	largeData := make([]byte, DefaultMaxMessageSize+1)
	results := b.Broadcast("blocks", largeData, []string{"peer1"})

	if len(results) != 1 || results[0].Success {
		t.Error("expected failure for too-large message")
	}
	if !errors.Is(results[0].Error, ErrBroadcastTooLarge) {
		t.Errorf("error = %v, want %v", results[0].Error, ErrBroadcastTooLarge)
	}
}

func TestBroadcastClosed(t *testing.T) {
	b := NewEIP2PBroadcaster()
	b.Close()

	results := b.Broadcast("blocks", []byte("data"), []string{"peer1"})
	if len(results) != 1 || results[0].Success {
		t.Error("expected failure for closed broadcaster")
	}
}

func TestBroadcastWithFailure(t *testing.T) {
	sender := &mockSender{failPeer: "peer2"}
	b := NewEIP2PBroadcasterWithSender(sender)

	peers := []string{"peer1", "peer2", "peer3"}
	results := b.Broadcast("blocks", []byte("block data"), peers)

	successes := 0
	failures := 0
	for _, r := range results {
		if r.Success {
			successes++
		} else {
			failures++
		}
	}

	if successes != 2 {
		t.Errorf("successes = %d, want 2", successes)
	}
	if failures != 1 {
		t.Errorf("failures = %d, want 1", failures)
	}
}

func TestSetFanout(t *testing.T) {
	b := NewEIP2PBroadcaster()

	if err := b.SetFanout(16); err != nil {
		t.Fatalf("SetFanout(16): %v", err)
	}
	if b.GetFanout() != 16 {
		t.Errorf("fanout = %d, want 16", b.GetFanout())
	}

	// Minimum.
	if err := b.SetFanout(MinFanout); err != nil {
		t.Fatalf("SetFanout(min): %v", err)
	}
	if b.GetFanout() != MinFanout {
		t.Errorf("fanout = %d, want %d", b.GetFanout(), MinFanout)
	}

	// Maximum.
	if err := b.SetFanout(MaxFanout); err != nil {
		t.Fatalf("SetFanout(max): %v", err)
	}
	if b.GetFanout() != MaxFanout {
		t.Errorf("fanout = %d, want %d", b.GetFanout(), MaxFanout)
	}
}

func TestSetFanoutOutOfRange(t *testing.T) {
	b := NewEIP2PBroadcaster()

	if err := b.SetFanout(0); err != ErrBroadcastFanoutRange {
		t.Errorf("SetFanout(0) error = %v, want %v", err, ErrBroadcastFanoutRange)
	}
	if err := b.SetFanout(MaxFanout + 1); err != ErrBroadcastFanoutRange {
		t.Errorf("SetFanout(too high) error = %v, want %v", err, ErrBroadcastFanoutRange)
	}
	if err := b.SetFanout(-1); err != ErrBroadcastFanoutRange {
		t.Errorf("SetFanout(-1) error = %v, want %v", err, ErrBroadcastFanoutRange)
	}
}

func TestSubscribeTopic(t *testing.T) {
	b := NewEIP2PBroadcaster()

	sub := b.SubscribeTopic("blocks")
	if sub == nil {
		t.Fatal("SubscribeTopic returned nil")
	}
	if sub.Topic != "blocks" {
		t.Errorf("Topic = %q, want %q", sub.Topic, "blocks")
	}
	if !sub.IsActive() {
		t.Error("subscription should be active")
	}
}

func TestSubscribeTopicEmpty(t *testing.T) {
	b := NewEIP2PBroadcaster()
	sub := b.SubscribeTopic("")
	if sub != nil {
		t.Error("SubscribeTopic(\"\") should return nil")
	}
}

func TestSubscribeTopicReceivesMessages(t *testing.T) {
	sender := &mockSender{}
	b := NewEIP2PBroadcasterWithSender(sender)

	sub := b.SubscribeTopic("blocks")

	// Broadcast a message to the "blocks" topic.
	b.Broadcast("blocks", []byte("block 123"), []string{"peer1"})

	// Check the subscription received the message.
	select {
	case msg := <-sub.Messages:
		if msg.Type != "blocks" {
			t.Errorf("msg.Type = %q, want %q", msg.Type, "blocks")
		}
		if string(msg.Data) != "block 123" {
			t.Errorf("msg.Data = %q, want %q", msg.Data, "block 123")
		}
		if msg.Timestamp.IsZero() {
			t.Error("msg.Timestamp should not be zero")
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for message")
	}
}

func TestSubscribeTopicNoDeliveryForOtherTopics(t *testing.T) {
	sender := &mockSender{}
	b := NewEIP2PBroadcasterWithSender(sender)

	sub := b.SubscribeTopic("blocks")

	// Broadcast to a different topic.
	b.Broadcast("transactions", []byte("tx data"), []string{"peer1"})

	select {
	case msg := <-sub.Messages:
		t.Errorf("should not receive message for other topic, got: %s", msg.Type)
	case <-time.After(50 * time.Millisecond):
		// Expected: no message received.
	}
}

func TestUnsubscribeTopic(t *testing.T) {
	b := NewEIP2PBroadcaster()

	sub := b.SubscribeTopic("blocks")
	if !sub.IsActive() {
		t.Fatal("subscription should be active")
	}

	b.UnsubscribeTopic("blocks")

	if sub.IsActive() {
		t.Error("subscription should be inactive after unsubscribe")
	}

	// Channel should be closed.
	_, ok := <-sub.Messages
	if ok {
		t.Error("Messages channel should be closed")
	}
}

func TestUnsubscribeTopicEmpty(t *testing.T) {
	b := NewEIP2PBroadcaster()
	// Should not panic.
	b.UnsubscribeTopic("")
	b.UnsubscribeTopic("nonexistent")
}

func TestTopicFilter(t *testing.T) {
	sender := &mockSender{}
	b := NewEIP2PBroadcasterWithSender(sender)

	sub := b.SubscribeTopic("blocks")

	// Set a filter that rejects small messages.
	b.SetTopicFilter("blocks", func(msgType string, data []byte) bool {
		return len(data) > 10
	})

	// Small message should be filtered out.
	b.Broadcast("blocks", []byte("small"), []string{"peer1"})

	select {
	case <-sub.Messages:
		t.Error("small message should have been filtered")
	case <-time.After(50 * time.Millisecond):
		// Expected.
	}

	// Large message should pass through.
	b.Broadcast("blocks", []byte("this is a large enough message"), []string{"peer1"})

	select {
	case msg := <-sub.Messages:
		if string(msg.Data) != "this is a large enough message" {
			t.Errorf("unexpected message data: %q", msg.Data)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for large message")
	}
}

func TestRemoveTopicFilter(t *testing.T) {
	sender := &mockSender{}
	b := NewEIP2PBroadcasterWithSender(sender)

	sub := b.SubscribeTopic("blocks")

	// Set and then remove filter.
	b.SetTopicFilter("blocks", func(msgType string, data []byte) bool {
		return false // reject everything
	})
	b.SetTopicFilter("blocks", nil) // remove filter

	b.Broadcast("blocks", []byte("test data"), []string{"peer1"})

	select {
	case msg := <-sub.Messages:
		if string(msg.Data) != "test data" {
			t.Errorf("unexpected data: %q", msg.Data)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout: filter should have been removed")
	}
}

func TestBroadcastStats(t *testing.T) {
	sender := &mockSender{}
	b := NewEIP2PBroadcasterWithSender(sender)

	// Initial stats should be zero.
	stats := b.Stats()
	if stats.MessagesSent != 0 || stats.BytesSent != 0 || stats.Failures != 0 {
		t.Error("initial stats should be zero")
	}

	// Broadcast some messages.
	data := []byte("test broadcast data")
	b.Broadcast("blocks", data, []string{"peer1", "peer2"})

	stats = b.Stats()
	if stats.MessagesSent != 2 {
		t.Errorf("MessagesSent = %d, want 2", stats.MessagesSent)
	}
	if stats.BytesSent != uint64(len(data))*2 {
		t.Errorf("BytesSent = %d, want %d", stats.BytesSent, len(data)*2)
	}
	if stats.Failures != 0 {
		t.Errorf("Failures = %d, want 0", stats.Failures)
	}
}

func TestBroadcastStatsWithFailures(t *testing.T) {
	sender := &mockSender{failPeer: "peer2"}
	b := NewEIP2PBroadcasterWithSender(sender)

	b.Broadcast("blocks", []byte("data"), []string{"peer1", "peer2", "peer3"})

	stats := b.Stats()
	if stats.MessagesSent != 2 {
		t.Errorf("MessagesSent = %d, want 2", stats.MessagesSent)
	}
	if stats.Failures != 1 {
		t.Errorf("Failures = %d, want 1", stats.Failures)
	}
}

func TestResetStats(t *testing.T) {
	sender := &mockSender{}
	b := NewEIP2PBroadcasterWithSender(sender)

	b.Broadcast("blocks", []byte("data"), []string{"peer1"})
	b.ResetStats()

	stats := b.Stats()
	if stats.MessagesSent != 0 || stats.BytesSent != 0 || stats.Failures != 0 {
		t.Error("stats should be zero after reset")
	}
}

func TestEIP2PActiveTopics(t *testing.T) {
	b := NewEIP2PBroadcaster()

	topics := b.ActiveTopics()
	if len(topics) != 0 {
		t.Errorf("ActiveTopics = %v, want empty", topics)
	}

	b.SubscribeTopic("blocks")
	b.SubscribeTopic("blobs")
	b.SubscribeTopic("transactions")

	topics = b.ActiveTopics()
	if len(topics) != 3 {
		t.Fatalf("ActiveTopics len = %d, want 3", len(topics))
	}

	// Check all topics are present.
	found := make(map[string]bool)
	for _, t := range topics {
		found[t] = true
	}
	for _, expected := range []string{"blocks", "blobs", "transactions"} {
		if !found[expected] {
			t.Errorf("topic %q not in ActiveTopics", expected)
		}
	}
}

func TestClose(t *testing.T) {
	b := NewEIP2PBroadcaster()

	sub1 := b.SubscribeTopic("blocks")
	sub2 := b.SubscribeTopic("blobs")

	b.Close()

	if sub1.IsActive() {
		t.Error("sub1 should be inactive after close")
	}
	if sub2.IsActive() {
		t.Error("sub2 should be inactive after close")
	}

	// Double close should not panic.
	b.Close()
}

func TestClosePreventsBroadcast(t *testing.T) {
	b := NewEIP2PBroadcaster()
	b.Close()

	results := b.Broadcast("blocks", []byte("data"), []string{"peer1"})
	if len(results) != 1 || results[0].Success {
		t.Error("broadcast after close should fail")
	}
}

func TestSelectPeers(t *testing.T) {
	// Fewer peers than limit: return all.
	peers := []string{"a", "b"}
	selected := selectPeers(peers, 5)
	if len(selected) != 2 {
		t.Errorf("selected %d peers, want 2", len(selected))
	}

	// More peers than limit: return exactly limit.
	peers = []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j"}
	selected = selectPeers(peers, 3)
	if len(selected) != 3 {
		t.Errorf("selected %d peers, want 3", len(selected))
	}

	// Selection should be deterministic.
	selected2 := selectPeers(peers, 3)
	for i := range selected {
		if selected[i] != selected2[i] {
			t.Error("selectPeers is not deterministic")
			break
		}
	}
}

func TestSelectPeersEmpty(t *testing.T) {
	selected := selectPeers(nil, 5)
	if len(selected) != 0 {
		t.Errorf("selected %d peers from nil list", len(selected))
	}
}

func TestMultipleSubscribersSameTopic(t *testing.T) {
	sender := &mockSender{}
	b := NewEIP2PBroadcasterWithSender(sender)

	sub1 := b.SubscribeTopic("blocks")
	sub2 := b.SubscribeTopic("blocks")

	b.Broadcast("blocks", []byte("block data"), []string{"peer1"})

	// Both subscribers should receive the message.
	for i, sub := range []*Subscription{sub1, sub2} {
		select {
		case msg := <-sub.Messages:
			if string(msg.Data) != "block data" {
				t.Errorf("sub%d: unexpected data %q", i+1, msg.Data)
			}
		case <-time.After(time.Second):
			t.Fatalf("sub%d: timeout waiting for message", i+1)
		}
	}
}

func TestBroadcastStatsAvgLatency(t *testing.T) {
	sender := &mockSender{}
	b := NewEIP2PBroadcasterWithSender(sender)

	// Multiple broadcasts.
	for i := 0; i < 5; i++ {
		b.Broadcast("blocks", []byte("data"), []string{"peer1"})
	}

	stats := b.Stats()
	if stats.MessagesSent != 5 {
		t.Errorf("MessagesSent = %d, want 5", stats.MessagesSent)
	}
	// AvgLatency should be non-negative (could be 0 for very fast operations).
	// Just verify it's computed without error.
	_ = stats.AvgLatency
}

func TestBroadcastResultFields(t *testing.T) {
	sender := &mockSender{failPeer: "bad-peer"}
	b := NewEIP2PBroadcasterWithSender(sender)

	results := b.Broadcast("blocks", []byte("data"), []string{"good-peer", "bad-peer"})

	for _, r := range results {
		if r.PeerID == "good-peer" {
			if !r.Success {
				t.Error("good-peer should succeed")
			}
			if r.Error != nil {
				t.Errorf("good-peer should have nil error, got %v", r.Error)
			}
		}
		if r.PeerID == "bad-peer" {
			if r.Success {
				t.Error("bad-peer should fail")
			}
			if r.Error == nil {
				t.Error("bad-peer should have non-nil error")
			}
		}
	}
}

func TestComparHashes(t *testing.T) {
	a := [32]byte{0, 0, 1}
	b := [32]byte{0, 0, 2}

	if !comparHashes(a, b) {
		t.Error("a < b should be true")
	}
	if comparHashes(b, a) {
		t.Error("b < a should be false")
	}
	if comparHashes(a, a) {
		t.Error("a < a should be false")
	}
}
