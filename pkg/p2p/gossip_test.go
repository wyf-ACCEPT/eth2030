package p2p

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

func gossipPeerID(b byte) types.Hash {
	var h types.Hash
	h[0] = b
	return h
}

func TestNewGossipManager(t *testing.T) {
	cfg := DefaultGossipConfig()
	gm := NewGossipManager(cfg)
	if gm == nil {
		t.Fatal("NewGossipManager returned nil")
	}
	if gm.config.MaxMessageSize != 1<<20 {
		t.Fatalf("MaxMessageSize got %d, want %d", gm.config.MaxMessageSize, uint64(1<<20))
	}
	if gm.config.FanoutSize != 6 {
		t.Fatalf("FanoutSize got %d, want 6", gm.config.FanoutSize)
	}
	if cfg.HeartbeatInterval != 1000 {
		t.Fatalf("HeartbeatInterval got %d, want 1000", cfg.HeartbeatInterval)
	}
	if cfg.PeerScoreThreshold != -50.0 {
		t.Fatalf("PeerScoreThreshold got %f, want -50.0", cfg.PeerScoreThreshold)
	}
}

func TestPublishAndSubscribe(t *testing.T) {
	gm := NewGossipManager(DefaultGossipConfig())
	defer gm.Close()

	sub := gm.Subscribe("blocks")
	if sub == nil || sub.Topic != "blocks" || !sub.IsActive() {
		t.Fatal("Subscribe returned invalid subscription")
	}

	if err := gm.PublishMessage("blocks", []byte("block-data")); err != nil {
		t.Fatalf("PublishMessage failed: %v", err)
	}

	select {
	case msg := <-sub.Messages:
		if msg.Topic != "blocks" || string(msg.Data) != "block-data" {
			t.Fatalf("unexpected message: topic=%s data=%s", msg.Topic, msg.Data)
		}
		if msg.MessageID.IsZero() || msg.Timestamp == 0 {
			t.Fatal("message ID or timestamp is zero")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for message")
	}
}

func TestPublishErrors(t *testing.T) {
	gm := NewGossipManager(GossipConfig{MaxMessageSize: 100})

	if err := gm.PublishMessage("", []byte("data")); !errors.Is(err, ErrGossipEmptyTopic) {
		t.Fatalf("expected ErrGossipEmptyTopic, got: %v", err)
	}
	if err := gm.PublishMessage("topic", nil); !errors.Is(err, ErrGossipEmptyData) {
		t.Fatalf("expected ErrGossipEmptyData, got: %v", err)
	}
	if err := gm.PublishMessage("topic", make([]byte, 200)); !errors.Is(err, ErrGossipMsgTooLarge) {
		t.Fatalf("expected ErrGossipMsgTooLarge, got: %v", err)
	}
	gm.Close()
	if err := gm.PublishMessage("topic", []byte("data")); !errors.Is(err, ErrGossipClosed) {
		t.Fatalf("expected ErrGossipClosed, got: %v", err)
	}
}

func TestMultipleSubscribers(t *testing.T) {
	gm := NewGossipManager(DefaultGossipConfig())
	defer gm.Close()

	sub1 := gm.Subscribe("tx")
	sub2 := gm.Subscribe("tx")
	sub3 := gm.Subscribe("blocks")

	gm.PublishMessage("tx", []byte("tx-data"))

	for _, sub := range []*GossipSubscription{sub1, sub2} {
		select {
		case msg := <-sub.Messages:
			if string(msg.Data) != "tx-data" {
				t.Fatalf("expected 'tx-data', got '%s'", string(msg.Data))
			}
		case <-time.After(time.Second):
			t.Fatal("timed out")
		}
	}

	select {
	case <-sub3.Messages:
		t.Fatal("blocks subscriber should not receive tx message")
	case <-time.After(50 * time.Millisecond):
	}
}

func TestUnsubscribe(t *testing.T) {
	gm := NewGossipManager(DefaultGossipConfig())
	defer gm.Close()

	sub := gm.Subscribe("blocks")
	if err := gm.Unsubscribe(sub); err != nil {
		t.Fatalf("Unsubscribe failed: %v", err)
	}
	if sub.IsActive() {
		t.Fatal("subscription should be inactive")
	}
	if err := gm.Unsubscribe(sub); !errors.Is(err, ErrGossipSubInactive) {
		t.Fatalf("expected ErrGossipSubInactive, got: %v", err)
	}
	if err := gm.Unsubscribe(nil); !errors.Is(err, ErrGossipSubNotFound) {
		t.Fatalf("expected ErrGossipSubNotFound, got: %v", err)
	}
	// Publish after unsubscribe should not panic.
	gm.PublishMessage("blocks", []byte("data"))
}

func TestValidateMessage(t *testing.T) {
	gm := NewGossipManager(GossipConfig{MaxMessageSize: 100})
	valid := &GossipMessage{
		Topic: "blocks", Data: []byte("data"), SenderID: gossipPeerID(1),
		Timestamp: uint64(time.Now().Unix()), MessageID: crypto.Keccak256Hash([]byte("msg1")),
	}

	if err := gm.ValidateMessage(valid); err != nil {
		t.Fatalf("expected valid, got: %v", err)
	}

	cases := []struct {
		name   string
		mutate func(*GossipMessage)
		want   error
	}{
		{"nil", nil, ErrGossipNilMsg},
		{"empty topic", func(m *GossipMessage) { m.Topic = "" }, ErrGossipEmptyTopic},
		{"empty data", func(m *GossipMessage) { m.Data = nil }, ErrGossipEmptyData},
		{"too large", func(m *GossipMessage) { m.Data = make([]byte, 200) }, ErrGossipMsgTooLarge},
		{"zero sender", func(m *GossipMessage) { m.SenderID = types.Hash{} }, ErrGossipZeroSender},
		{"zero timestamp", func(m *GossipMessage) { m.Timestamp = 0 }, ErrGossipZeroTimestamp},
		{"zero msg ID", func(m *GossipMessage) { m.MessageID = types.Hash{} }, ErrGossipZeroMessageID},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.mutate == nil {
				if err := gm.ValidateMessage(nil); !errors.Is(err, tc.want) {
					t.Fatalf("expected %v, got: %v", tc.want, err)
				}
				return
			}
			bad := *valid
			tc.mutate(&bad)
			if err := gm.ValidateMessage(&bad); !errors.Is(err, tc.want) {
				t.Fatalf("expected %v, got: %v", tc.want, err)
			}
		})
	}
}

func TestValidateMessageBannedSender(t *testing.T) {
	gm := NewGossipManager(DefaultGossipConfig())
	peerID := gossipPeerID(5)
	gm.BanPeer(peerID, "spamming", 3600)

	msg := &GossipMessage{
		Topic: "blocks", Data: []byte("data"), SenderID: peerID,
		Timestamp: uint64(time.Now().Unix()), MessageID: crypto.Keccak256Hash([]byte("msg")),
	}
	if err := gm.ValidateMessage(msg); !errors.Is(err, ErrGossipPeerBanned) {
		t.Fatalf("expected ErrGossipPeerBanned, got: %v", err)
	}
}

func TestPeerScoring(t *testing.T) {
	gm := NewGossipManager(DefaultGossipConfig())
	pid := gossipPeerID(1)

	if s := gm.PeerScore(pid); s != 0 {
		t.Fatalf("expected 0, got %f", s)
	}
	gm.UpdatePeerScore(pid, 10.0)
	if s := gm.PeerScore(pid); s != 10.0 {
		t.Fatalf("expected 10, got %f", s)
	}
	gm.UpdatePeerScore(pid, -15.0)
	if s := gm.PeerScore(pid); s != -5.0 {
		t.Fatalf("expected -5, got %f", s)
	}
	gm.UpdatePeerScore(pid, 200.0)
	if s := gm.PeerScore(pid); s != MaxScore {
		t.Fatalf("expected clamped to %f, got %f", MaxScore, s)
	}
	gm.UpdatePeerScore(pid, -300.0)
	if s := gm.PeerScore(pid); s != MinScore {
		t.Fatalf("expected clamped to %f, got %f", MinScore, s)
	}
}

func TestBanPeer(t *testing.T) {
	gm := NewGossipManager(DefaultGossipConfig())
	pid := gossipPeerID(7)
	gm.UpdatePeerScore(pid, 50.0)
	gm.BanPeer(pid, "protocol violation", 3600)
	if s := gm.PeerScore(pid); s != MinScore {
		t.Fatalf("expected %f after ban, got %f", MinScore, s)
	}
}

func TestTopicPeers(t *testing.T) {
	gm := NewGossipManager(DefaultGossipConfig())

	if len(gm.GetTopicPeers("blocks")) != 0 {
		t.Fatal("expected 0 peers initially")
	}

	gm.AddTopicPeer("blocks", gossipPeerID(1))
	gm.AddTopicPeer("blocks", gossipPeerID(2))
	gm.AddTopicPeer("blocks", gossipPeerID(3))
	gm.AddTopicPeer("tx", gossipPeerID(1))

	if n := len(gm.GetTopicPeers("blocks")); n != 3 {
		t.Fatalf("expected 3, got %d", n)
	}
	if n := len(gm.GetTopicPeers("tx")); n != 1 {
		t.Fatalf("expected 1, got %d", n)
	}

	gm.RemoveTopicPeer("blocks", gossipPeerID(2))
	if n := len(gm.GetTopicPeers("blocks")); n != 2 {
		t.Fatalf("expected 2 after remove, got %d", n)
	}

	gm.RemoveTopicPeer("tx", gossipPeerID(1))
	if n := len(gm.GetTopicPeers("tx")); n != 0 {
		t.Fatalf("expected 0 after removing last, got %d", n)
	}
}

func TestActiveTopics(t *testing.T) {
	gm := NewGossipManager(DefaultGossipConfig())
	defer gm.Close()

	if len(gm.ActiveTopics()) != 0 {
		t.Fatal("expected 0 topics initially")
	}

	gm.Subscribe("blocks")
	gm.Subscribe("tx")
	sub := gm.Subscribe("attestations")

	topics := gm.ActiveTopics()
	if len(topics) != 3 {
		t.Fatalf("expected 3 topics, got %d", len(topics))
	}
	if topics[0] != "attestations" || topics[1] != "blocks" || topics[2] != "tx" {
		t.Fatalf("topics not sorted: %v", topics)
	}

	// Unsubscribe removes the topic when no subs remain.
	gm.Unsubscribe(sub)
	if len(gm.ActiveTopics()) != 2 {
		t.Fatalf("expected 2 topics after unsubscribe")
	}
}

func TestMessageDeduplication(t *testing.T) {
	gm := NewGossipManager(DefaultGossipConfig())
	defer gm.Close()
	sub := gm.Subscribe("blocks")
	gm.PublishMessage("blocks", []byte("block-1"))

	msg := <-sub.Messages
	if !gm.IsSeen(msg.MessageID) {
		t.Fatal("published message should be seen")
	}
	if gm.IsSeen(crypto.Keccak256Hash([]byte("unknown"))) {
		t.Fatal("unknown message should not be seen")
	}
}

func TestCloseManager(t *testing.T) {
	gm := NewGossipManager(DefaultGossipConfig())
	sub := gm.Subscribe("blocks")
	gm.Close()

	if _, ok := <-sub.Messages; ok {
		t.Fatal("channel should be closed")
	}
	if sub.IsActive() {
		t.Fatal("subscription should be inactive")
	}
	if err := gm.PublishMessage("blocks", []byte("data")); !errors.Is(err, ErrGossipClosed) {
		t.Fatalf("expected ErrGossipClosed, got: %v", err)
	}
	gm.Close() // double close should not panic
}

func TestConcurrentPublishSubscribe(t *testing.T) {
	gm := NewGossipManager(DefaultGossipConfig())
	defer gm.Close()

	sub := gm.Subscribe("concurrent")
	received := make(chan *GossipMessage, 500)

	done := make(chan struct{})
	go func() {
		defer close(done)
		for msg := range sub.Messages {
			received <- msg
		}
	}()

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				gm.PublishMessage("concurrent", []byte{byte(id), byte(j)})
			}
		}(i)
	}
	wg.Wait()
	time.Sleep(50 * time.Millisecond)
	gm.Unsubscribe(sub)
	<-done
	close(received)

	count := 0
	for range received {
		count++
	}
	if count == 0 {
		t.Fatal("expected at least some messages")
	}
}

func TestConcurrentPeerScoring(t *testing.T) {
	gm := NewGossipManager(DefaultGossipConfig())
	pid := gossipPeerID(1)
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(2)
		go func() { defer wg.Done(); gm.UpdatePeerScore(pid, 1.0) }()
		go func() { defer wg.Done(); gm.PeerScore(pid) }()
	}
	wg.Wait()
	s := gm.PeerScore(pid)
	if s < MinScore || s > MaxScore {
		t.Fatalf("score %f out of bounds", s)
	}
}

func TestConcurrentTopicPeers(t *testing.T) {
	gm := NewGossipManager(DefaultGossipConfig())
	var wg sync.WaitGroup
	for i := byte(0); i < 50; i++ {
		wg.Add(2)
		go func(b byte) { defer wg.Done(); gm.AddTopicPeer("blocks", gossipPeerID(b)) }(i)
		go func(b byte) { defer wg.Done(); gm.GetTopicPeers("blocks") }(i)
	}
	wg.Wait()
}

func TestSubscriptionChannelBuffer(t *testing.T) {
	gm := NewGossipManager(DefaultGossipConfig())
	defer gm.Close()
	sub := gm.Subscribe("flood")

	for i := 0; i < 200; i++ {
		gm.PublishMessage("flood", []byte{byte(i)})
	}

	count := 0
	for {
		select {
		case <-sub.Messages:
			count++
		default:
			goto done
		}
	}
done:
	if count == 0 || count > 64 {
		t.Fatalf("unexpected message count: %d (want 1-64)", count)
	}
}

func TestComputeMessageID(t *testing.T) {
	id1 := computeMessageID("blocks", []byte("data"), 1000)
	id2 := computeMessageID("blocks", []byte("data"), 1000)
	id3 := computeMessageID("blocks", []byte("data"), 1001)
	id4 := computeMessageID("tx", []byte("data"), 1000)

	if id1 != id2 {
		t.Fatal("same inputs should produce same ID")
	}
	if id1 == id3 || id1 == id4 {
		t.Fatal("different inputs should produce different IDs")
	}
	if id1.IsZero() {
		t.Fatal("message ID should not be zero")
	}
}

func TestBanExpiry(t *testing.T) {
	gm := NewGossipManager(DefaultGossipConfig())
	pid := gossipPeerID(10)
	gm.BanPeer(pid, "test", 0) // ban for 0 seconds (already expired)

	msg := &GossipMessage{
		Topic: "blocks", Data: []byte("data"), SenderID: pid,
		Timestamp: uint64(time.Now().Unix()), MessageID: crypto.Keccak256Hash([]byte("msg")),
	}
	if err := gm.ValidateMessage(msg); err != nil {
		t.Fatalf("expected expired ban, got: %v", err)
	}
}
