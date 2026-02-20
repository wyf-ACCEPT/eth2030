package p2p

import (
	"crypto/sha256"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestGossipTopicString(t *testing.T) {
	tests := []struct {
		topic GossipTopic
		want  string
	}{
		{BeaconBlock, "beacon_block"},
		{BeaconAggregateAndProof, "beacon_aggregate_and_proof"},
		{VoluntaryExit, "voluntary_exit"},
		{ProposerSlashing, "proposer_slashing"},
		{AttesterSlashing, "attester_slashing"},
		{BlobSidecar, "blob_sidecar"},
		{SyncCommitteeContribution, "sync_committee_contribution"},
	}
	for _, tt := range tests {
		if got := tt.topic.String(); got != tt.want {
			t.Errorf("GossipTopic(%d).String() = %q, want %q", tt.topic, got, tt.want)
		}
	}
}

func TestGossipTopicStringUnknown(t *testing.T) {
	unknown := GossipTopic(999)
	got := unknown.String()
	if got == "" {
		t.Fatal("expected non-empty string for unknown topic")
	}
}

func TestTopicString(t *testing.T) {
	got := BeaconBlock.TopicString("abcd1234")
	want := "/eth2/abcd1234/beacon_block/ssz_snappy"
	if got != want {
		t.Errorf("TopicString = %q, want %q", got, want)
	}
}

func TestParseGossipTopic(t *testing.T) {
	topic, err := ParseGossipTopic("voluntary_exit")
	if err != nil {
		t.Fatalf("ParseGossipTopic: %v", err)
	}
	if topic != VoluntaryExit {
		t.Errorf("got %v, want VoluntaryExit", topic)
	}

	_, err = ParseGossipTopic("nonexistent_topic")
	if err == nil {
		t.Fatal("expected error for unknown topic name")
	}
}

func TestComputeGossipMessageID(t *testing.T) {
	data := []byte("test beacon block data")
	id := ComputeMessageID(data)

	// Verify against manual computation.
	h := sha256.New()
	h.Write(MessageDomainValidSnappy[:])
	h.Write(data)
	sum := h.Sum(nil)

	for i := 0; i < MessageIDSize; i++ {
		if id[i] != sum[i] {
			t.Fatalf("MessageID byte %d: got %02x, want %02x", i, id[i], sum[i])
		}
	}

	// Different data produces different ID.
	id2 := ComputeMessageID([]byte("different data"))
	if id == id2 {
		t.Fatal("different data should produce different message IDs")
	}
}

func TestComputeInvalidMessageID(t *testing.T) {
	data := []byte("invalid snappy data")
	id := ComputeInvalidMessageID(data)

	h := sha256.New()
	h.Write(MessageDomainInvalidSnappy[:])
	h.Write(data)
	sum := h.Sum(nil)

	for i := 0; i < MessageIDSize; i++ {
		if id[i] != sum[i] {
			t.Fatalf("InvalidMessageID byte %d: got %02x, want %02x", i, id[i], sum[i])
		}
	}

	// Valid and invalid domains produce different IDs for the same data.
	validID := ComputeMessageID(data)
	if id == validID {
		t.Fatal("valid and invalid domains should produce different IDs")
	}
}

func TestDefaultTopicParams(t *testing.T) {
	p := DefaultTopicParams()
	if p.MeshD != 8 {
		t.Errorf("MeshD = %d, want 8", p.MeshD)
	}
	if p.MeshDlo != 6 {
		t.Errorf("MeshDlo = %d, want 6", p.MeshDlo)
	}
	if p.MeshDhi != 12 {
		t.Errorf("MeshDhi = %d, want 12", p.MeshDhi)
	}
	if p.HeartbeatInterval != 700*time.Millisecond {
		t.Errorf("HeartbeatInterval = %v, want 700ms", p.HeartbeatInterval)
	}
	if p.HistoryLength != 6 {
		t.Errorf("HistoryLength = %d, want 6", p.HistoryLength)
	}
	if p.HistoryGossip != 3 {
		t.Errorf("HistoryGossip = %d, want 3", p.HistoryGossip)
	}
	if p.FanoutTTL != 60*time.Second {
		t.Errorf("FanoutTTL = %v, want 60s", p.FanoutTTL)
	}
}

func TestTopicManagerSubscribeUnsubscribe(t *testing.T) {
	tm := NewTopicManager(DefaultTopicParams())
	defer tm.Close()

	handler := func(topic GossipTopic, msgID MessageID, data []byte) {}

	// Subscribe to BeaconBlock.
	if err := tm.Subscribe(BeaconBlock, handler); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	if !tm.IsSubscribed(BeaconBlock) {
		t.Fatal("expected BeaconBlock to be subscribed")
	}

	// Double subscribe should fail.
	if err := tm.Subscribe(BeaconBlock, handler); err != ErrTopicAlreadySubscribed {
		t.Fatalf("expected ErrTopicAlreadySubscribed, got %v", err)
	}

	// Unsubscribe.
	if err := tm.Unsubscribe(BeaconBlock); err != nil {
		t.Fatalf("Unsubscribe: %v", err)
	}

	if tm.IsSubscribed(BeaconBlock) {
		t.Fatal("expected BeaconBlock to not be subscribed")
	}

	// Unsubscribe again should fail.
	if err := tm.Unsubscribe(BeaconBlock); err != ErrTopicNotSubscribed {
		t.Fatalf("expected ErrTopicNotSubscribed, got %v", err)
	}
}

func TestTopicManagerNilHandler(t *testing.T) {
	tm := NewTopicManager(DefaultTopicParams())
	defer tm.Close()

	err := tm.Subscribe(BeaconBlock, nil)
	if err != ErrTopicNilHandler {
		t.Fatalf("expected ErrTopicNilHandler, got %v", err)
	}
}

func TestTopicManagerPublish(t *testing.T) {
	tm := NewTopicManager(DefaultTopicParams())
	defer tm.Close()

	var received []byte
	var receivedTopic GossipTopic
	var receivedID MessageID

	handler := func(topic GossipTopic, msgID MessageID, data []byte) {
		receivedTopic = topic
		receivedID = msgID
		received = data
	}

	if err := tm.Subscribe(BeaconBlock, handler); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	data := []byte("block data payload")
	if err := tm.Publish(BeaconBlock, data); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	if receivedTopic != BeaconBlock {
		t.Errorf("received topic = %v, want BeaconBlock", receivedTopic)
	}
	if string(received) != string(data) {
		t.Errorf("received data = %q, want %q", received, data)
	}

	expectedID := ComputeMessageID(data)
	if receivedID != expectedID {
		t.Errorf("received ID mismatch")
	}
}

func TestTopicManagerPublishNotSubscribed(t *testing.T) {
	tm := NewTopicManager(DefaultTopicParams())
	defer tm.Close()

	err := tm.Publish(BeaconBlock, []byte("data"))
	if err != ErrTopicNotSubscribed {
		t.Fatalf("expected ErrTopicNotSubscribed, got %v", err)
	}
}

func TestTopicManagerPublishEmptyData(t *testing.T) {
	tm := NewTopicManager(DefaultTopicParams())
	defer tm.Close()

	err := tm.Publish(BeaconBlock, nil)
	if err != ErrTopicEmptyData {
		t.Fatalf("expected ErrTopicEmptyData, got %v", err)
	}

	err = tm.Publish(BeaconBlock, []byte{})
	if err != ErrTopicEmptyData {
		t.Fatalf("expected ErrTopicEmptyData, got %v", err)
	}
}

func TestTopicManagerPublishTooLarge(t *testing.T) {
	tm := NewTopicManager(DefaultTopicParams())
	defer tm.Close()

	handler := func(topic GossipTopic, msgID MessageID, data []byte) {}
	tm.Subscribe(BeaconBlock, handler)

	bigData := make([]byte, MaxPayloadSize+1)
	err := tm.Publish(BeaconBlock, bigData)
	if err != ErrTopicDataTooLarge {
		t.Fatalf("expected ErrTopicDataTooLarge, got %v", err)
	}
}

func TestTopicManagerDeduplication(t *testing.T) {
	tm := NewTopicManager(DefaultTopicParams())
	defer tm.Close()

	var count int
	handler := func(topic GossipTopic, msgID MessageID, data []byte) {
		count++
	}

	tm.Subscribe(BeaconBlock, handler)

	data := []byte("unique block data")
	if err := tm.Publish(BeaconBlock, data); err != nil {
		t.Fatalf("first publish: %v", err)
	}

	// Same data again should be a duplicate.
	err := tm.Publish(BeaconBlock, data)
	if err != ErrTopicDuplicateMessage {
		t.Fatalf("expected ErrTopicDuplicateMessage, got %v", err)
	}

	if count != 1 {
		t.Errorf("handler called %d times, want 1", count)
	}
}

func TestTopicManagerDeliver(t *testing.T) {
	tm := NewTopicManager(DefaultTopicParams())
	defer tm.Close()

	var received []byte
	handler := func(topic GossipTopic, msgID MessageID, data []byte) {
		received = data
	}

	tm.Subscribe(BeaconBlock, handler)

	data := []byte("delivered block data")
	if err := tm.Deliver(BeaconBlock, data, true); err != nil {
		t.Fatalf("Deliver: %v", err)
	}

	if string(received) != string(data) {
		t.Errorf("received = %q, want %q", received, data)
	}

	// Check scoring.
	score, ok := tm.TopicScore(BeaconBlock)
	if !ok {
		t.Fatal("expected topic score to exist")
	}
	if score.MessagesReceived != 1 {
		t.Errorf("MessagesReceived = %d, want 1", score.MessagesReceived)
	}
	if score.FirstDeliveries != 1 {
		t.Errorf("FirstDeliveries = %d, want 1", score.FirstDeliveries)
	}
}

func TestTopicManagerDeliverInvalid(t *testing.T) {
	tm := NewTopicManager(DefaultTopicParams())
	defer tm.Close()

	var handlerCalled bool
	handler := func(topic GossipTopic, msgID MessageID, data []byte) {
		handlerCalled = true
	}

	tm.Subscribe(BeaconBlock, handler)

	// Deliver with isValid=false should increment invalid counter and not call handler.
	if err := tm.Deliver(BeaconBlock, []byte("bad data"), false); err != nil {
		t.Fatalf("Deliver invalid: %v", err)
	}

	if handlerCalled {
		t.Fatal("handler should not be called for invalid messages")
	}

	score, _ := tm.TopicScore(BeaconBlock)
	if score.InvalidMessages != 1 {
		t.Errorf("InvalidMessages = %d, want 1", score.InvalidMessages)
	}
}

func TestTopicManagerTopicScore(t *testing.T) {
	tm := NewTopicManager(DefaultTopicParams())
	defer tm.Close()

	_, ok := tm.TopicScore(BeaconBlock)
	if ok {
		t.Fatal("expected false for unsubscribed topic")
	}

	handler := func(topic GossipTopic, msgID MessageID, data []byte) {}
	tm.Subscribe(BeaconBlock, handler)

	score, ok := tm.TopicScore(BeaconBlock)
	if !ok {
		t.Fatal("expected true for subscribed topic")
	}
	if score.MessagesReceived != 0 {
		t.Errorf("initial MessagesReceived = %d, want 0", score.MessagesReceived)
	}
}

func TestTopicManagerPeerScoring(t *testing.T) {
	tm := NewTopicManager(DefaultTopicParams())
	defer tm.Close()

	handler := func(topic GossipTopic, msgID MessageID, data []byte) {}
	tm.Subscribe(BeaconBlock, handler)

	// Initial score should be 0.
	if s := tm.PeerTopicScore(BeaconBlock, "peer1"); s != 0 {
		t.Errorf("initial peer score = %f, want 0", s)
	}

	tm.UpdatePeerTopicScore(BeaconBlock, "peer1", 5.0)
	if s := tm.PeerTopicScore(BeaconBlock, "peer1"); s != 5.0 {
		t.Errorf("peer score = %f, want 5.0", s)
	}

	tm.UpdatePeerTopicScore(BeaconBlock, "peer1", -2.0)
	if s := tm.PeerTopicScore(BeaconBlock, "peer1"); s != 3.0 {
		t.Errorf("peer score = %f, want 3.0", s)
	}

	// Score for unsubscribed topic returns 0.
	if s := tm.PeerTopicScore(VoluntaryExit, "peer1"); s != 0 {
		t.Errorf("unsubscribed topic score = %f, want 0", s)
	}
}

func TestTopicManagerSubscribedTopics(t *testing.T) {
	tm := NewTopicManager(DefaultTopicParams())
	defer tm.Close()

	handler := func(topic GossipTopic, msgID MessageID, data []byte) {}

	tm.Subscribe(BeaconBlock, handler)
	tm.Subscribe(VoluntaryExit, handler)
	tm.Subscribe(BlobSidecar, handler)

	topics := tm.SubscribedTopics()
	if len(topics) != 3 {
		t.Fatalf("subscribed topics = %d, want 3", len(topics))
	}

	// Check all expected topics are present.
	found := map[GossipTopic]bool{}
	for _, tp := range topics {
		found[tp] = true
	}
	for _, expected := range []GossipTopic{BeaconBlock, VoluntaryExit, BlobSidecar} {
		if !found[expected] {
			t.Errorf("missing topic %v", expected)
		}
	}
}

func TestTopicManagerPruneSeenMessages(t *testing.T) {
	params := DefaultTopicParams()
	params.SeenTTL = 50 * time.Millisecond
	tm := NewTopicManager(params)
	defer tm.Close()

	handler := func(topic GossipTopic, msgID MessageID, data []byte) {}
	tm.Subscribe(BeaconBlock, handler)

	// Publish some messages.
	for i := 0; i < 5; i++ {
		data := []byte{byte(i), byte(i + 1), byte(i + 2)}
		tm.Publish(BeaconBlock, data)
	}

	if tm.SeenCount() != 5 {
		t.Fatalf("SeenCount = %d, want 5", tm.SeenCount())
	}

	// Wait for SeenTTL to expire.
	time.Sleep(60 * time.Millisecond)

	pruned := tm.PruneSeenMessages()
	if pruned != 5 {
		t.Errorf("pruned = %d, want 5", pruned)
	}

	if tm.SeenCount() != 0 {
		t.Errorf("SeenCount after prune = %d, want 0", tm.SeenCount())
	}
}

func TestTopicManagerClose(t *testing.T) {
	tm := NewTopicManager(DefaultTopicParams())

	handler := func(topic GossipTopic, msgID MessageID, data []byte) {}
	tm.Subscribe(BeaconBlock, handler)

	tm.Close()

	// All operations should fail after close.
	if err := tm.Subscribe(VoluntaryExit, handler); err != ErrTopicManagerClosed {
		t.Errorf("Subscribe after close: got %v, want ErrTopicManagerClosed", err)
	}
	if err := tm.Unsubscribe(BeaconBlock); err != ErrTopicManagerClosed {
		t.Errorf("Unsubscribe after close: got %v, want ErrTopicManagerClosed", err)
	}
	if err := tm.Publish(BeaconBlock, []byte("data")); err != ErrTopicManagerClosed {
		t.Errorf("Publish after close: got %v, want ErrTopicManagerClosed", err)
	}
}

func TestTopicManagerConcurrency(t *testing.T) {
	tm := NewTopicManager(DefaultTopicParams())
	defer tm.Close()

	var count atomic.Int64
	handler := func(topic GossipTopic, msgID MessageID, data []byte) {
		count.Add(1)
	}

	tm.Subscribe(BeaconBlock, handler)
	tm.Subscribe(VoluntaryExit, handler)

	var wg sync.WaitGroup
	// Concurrent publishers.
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			data := []byte{byte(n), byte(n >> 8), byte(n >> 16)}
			// Alternate between topics.
			if n%2 == 0 {
				tm.Publish(BeaconBlock, data)
			} else {
				tm.Publish(VoluntaryExit, data)
			}
		}(i)
	}

	// Concurrent scoring.
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			tm.UpdatePeerTopicScore(BeaconBlock, "peer1", 1.0)
			tm.PeerTopicScore(BeaconBlock, "peer1")
		}(i)
	}

	// Concurrent reads.
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tm.IsSubscribed(BeaconBlock)
			tm.SubscribedTopics()
			tm.TopicScore(BeaconBlock)
			tm.SeenCount()
		}()
	}

	wg.Wait()

	if c := count.Load(); c == 0 {
		t.Fatal("expected at least some handler calls")
	}
}

func TestTopicManagerRecordInvalidMessage(t *testing.T) {
	tm := NewTopicManager(DefaultTopicParams())
	defer tm.Close()

	handler := func(topic GossipTopic, msgID MessageID, data []byte) {}
	tm.Subscribe(BeaconBlock, handler)

	tm.RecordInvalidMessage(BeaconBlock)
	tm.RecordInvalidMessage(BeaconBlock)

	score, ok := tm.TopicScore(BeaconBlock)
	if !ok {
		t.Fatal("expected topic score to exist")
	}
	if score.InvalidMessages != 2 {
		t.Errorf("InvalidMessages = %d, want 2", score.InvalidMessages)
	}

	// Recording on unsubscribed topic should not panic.
	tm.RecordInvalidMessage(VoluntaryExit)
}

func TestTopicManagerDeliverDeduplication(t *testing.T) {
	tm := NewTopicManager(DefaultTopicParams())
	defer tm.Close()

	var count int
	handler := func(topic GossipTopic, msgID MessageID, data []byte) {
		count++
	}

	tm.Subscribe(BeaconBlock, handler)

	data := []byte("unique deliver data")

	if err := tm.Deliver(BeaconBlock, data, true); err != nil {
		t.Fatalf("first deliver: %v", err)
	}

	err := tm.Deliver(BeaconBlock, data, true)
	if err != ErrTopicDuplicateMessage {
		t.Fatalf("expected ErrTopicDuplicateMessage, got %v", err)
	}

	if count != 1 {
		t.Errorf("handler called %d times, want 1", count)
	}
}

func TestTopicManagerParams(t *testing.T) {
	params := DefaultTopicParams()
	params.MeshD = 10
	tm := NewTopicManager(params)
	defer tm.Close()

	got := tm.Params()
	if got.MeshD != 10 {
		t.Errorf("Params().MeshD = %d, want 10", got.MeshD)
	}
}
