package lethe

import (
	"bytes"
	"fmt"
	"sync"
	"testing"
)

func TestNewEnhancedLETHE(t *testing.T) {
	el := NewEnhancedLETHE(DefaultEnhancedLETHEConfig())
	if el == nil {
		t.Fatal("EnhancedLETHE should not be nil")
	}
	if el.ActiveSessions() != 0 {
		t.Errorf("expected 0 sessions, got %d", el.ActiveSessions())
	}
}

func TestCreateSession_Basic(t *testing.T) {
	el := NewEnhancedLETHE(DefaultEnhancedLETHEConfig())

	session, err := el.CreateSession([]string{"validator-A", "validator-B"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if session == nil {
		t.Fatal("session should not be nil")
	}
	if session.ID == "" {
		t.Error("session ID should not be empty")
	}
	if len(session.Participants) != 2 {
		t.Errorf("expected 2 participants, got %d", len(session.Participants))
	}
	if session.Status != SessionActive {
		t.Errorf("expected SessionActive, got %d", session.Status)
	}
	if el.ActiveSessions() != 1 {
		t.Errorf("expected 1 active session, got %d", el.ActiveSessions())
	}
}

func TestCreateSession_NoParticipants(t *testing.T) {
	el := NewEnhancedLETHE(DefaultEnhancedLETHEConfig())

	_, err := el.CreateSession(nil)
	if err != ErrLETHENoParticipants {
		t.Errorf("expected ErrLETHENoParticipants, got %v", err)
	}

	_, err = el.CreateSession([]string{})
	if err != ErrLETHENoParticipants {
		t.Errorf("expected ErrLETHENoParticipants for empty, got %v", err)
	}
}

func TestCreateSession_MaxSessions(t *testing.T) {
	cfg := DefaultEnhancedLETHEConfig()
	cfg.MaxSessions = 2
	el := NewEnhancedLETHE(cfg)

	_, err := el.CreateSession([]string{"a"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = el.CreateSession([]string{"b"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = el.CreateSession([]string{"c"})
	if err != ErrLETHEMaxSessions {
		t.Errorf("expected ErrLETHEMaxSessions, got %v", err)
	}
}

func TestSubmitToMix_Basic(t *testing.T) {
	el := NewEnhancedLETHE(DefaultEnhancedLETHEConfig())
	session, _ := el.CreateSession([]string{"v1", "v2"})

	if err := el.SubmitToMix(session.ID, []byte("data-1")); err != nil {
		t.Fatalf("SubmitToMix: %v", err)
	}
	if err := el.SubmitToMix(session.ID, []byte("data-2")); err != nil {
		t.Fatalf("SubmitToMix: %v", err)
	}

	if el.MixBufferSize(session.ID) != 2 {
		t.Errorf("expected mix buffer size 2, got %d", el.MixBufferSize(session.ID))
	}
}

func TestSubmitToMix_SessionNotFound(t *testing.T) {
	el := NewEnhancedLETHE(DefaultEnhancedLETHEConfig())

	err := el.SubmitToMix("nonexistent", []byte("data"))
	if err != ErrLETHESessionNotFound {
		t.Errorf("expected ErrLETHESessionNotFound, got %v", err)
	}
}

func TestSubmitToMix_SessionClosed(t *testing.T) {
	el := NewEnhancedLETHE(DefaultEnhancedLETHEConfig())
	session, _ := el.CreateSession([]string{"v1"})

	// Close the session internally by setting status.
	el.mu.Lock()
	el.sessions[session.ID].Status = SessionClosed
	el.mu.Unlock()

	err := el.SubmitToMix(session.ID, []byte("data"))
	if err != ErrLETHESessionClosed {
		t.Errorf("expected ErrLETHESessionClosed, got %v", err)
	}
}

func TestSubmitToMix_BufferFull(t *testing.T) {
	cfg := DefaultEnhancedLETHEConfig()
	cfg.MaxMixBuffer = 2
	el := NewEnhancedLETHE(cfg)

	session, _ := el.CreateSession([]string{"v1"})
	el.SubmitToMix(session.ID, []byte("a"))
	el.SubmitToMix(session.ID, []byte("b"))

	err := el.SubmitToMix(session.ID, []byte("c"))
	if err != ErrLETHESessionFull {
		t.Errorf("expected ErrLETHESessionFull, got %v", err)
	}
}

func TestFlushMix_Basic(t *testing.T) {
	cfg := DefaultEnhancedLETHEConfig()
	cfg.MinMixSize = 2
	el := NewEnhancedLETHE(cfg)

	session, _ := el.CreateSession([]string{"v1", "v2"})
	el.SubmitToMix(session.ID, []byte("item-A"))
	el.SubmitToMix(session.ID, []byte("item-B"))
	el.SubmitToMix(session.ID, []byte("item-C"))

	result, err := el.FlushMix(session.ID)
	if err != nil {
		t.Fatalf("FlushMix: %v", err)
	}
	if len(result) != 3 {
		t.Fatalf("expected 3 items, got %d", len(result))
	}

	// All original items should be present (order may differ).
	found := map[string]bool{"item-A": false, "item-B": false, "item-C": false}
	for _, item := range result {
		found[string(item)] = true
	}
	for k, v := range found {
		if !v {
			t.Errorf("missing item: %s", k)
		}
	}

	// Buffer should be cleared after flush.
	if el.MixBufferSize(session.ID) != 0 {
		t.Errorf("expected empty buffer after flush, got %d", el.MixBufferSize(session.ID))
	}
}

func TestFlushMix_InsufficientItems(t *testing.T) {
	cfg := DefaultEnhancedLETHEConfig()
	cfg.MinMixSize = 3
	el := NewEnhancedLETHE(cfg)

	session, _ := el.CreateSession([]string{"v1"})
	el.SubmitToMix(session.ID, []byte("only-one"))
	el.SubmitToMix(session.ID, []byte("only-two"))

	_, err := el.FlushMix(session.ID)
	if err != ErrLETHEInsufficientMix {
		t.Errorf("expected ErrLETHEInsufficientMix, got %v", err)
	}
}

func TestFlushMix_SessionNotFound(t *testing.T) {
	el := NewEnhancedLETHE(DefaultEnhancedLETHEConfig())
	_, err := el.FlushMix("nonexistent")
	if err != ErrLETHESessionNotFound {
		t.Errorf("expected ErrLETHESessionNotFound, got %v", err)
	}
}

func TestFlushMix_SessionClosed(t *testing.T) {
	el := NewEnhancedLETHE(DefaultEnhancedLETHEConfig())
	session, _ := el.CreateSession([]string{"v1"})

	el.mu.Lock()
	el.sessions[session.ID].Status = SessionClosed
	el.mu.Unlock()

	_, err := el.FlushMix(session.ID)
	if err != ErrLETHESessionClosed {
		t.Errorf("expected ErrLETHESessionClosed, got %v", err)
	}
}

func TestAddNoise_StripNoise_RoundTrip(t *testing.T) {
	el := NewEnhancedLETHE(DefaultEnhancedLETHEConfig())
	original := []byte("validator attestation data")

	noisy := el.AddNoise(original)
	if len(noisy) <= len(original) {
		t.Error("noisy data should be longer than original")
	}

	stripped, err := el.StripNoise(noisy)
	if err != nil {
		t.Fatalf("StripNoise: %v", err)
	}
	if !bytes.Equal(stripped, original) {
		t.Errorf("round trip failed: got %q, want %q", stripped, original)
	}
}

func TestAddNoise_EmptyData(t *testing.T) {
	el := NewEnhancedLETHE(DefaultEnhancedLETHEConfig())
	noisy := el.AddNoise([]byte{})

	stripped, err := el.StripNoise(noisy)
	if err != nil {
		t.Fatalf("StripNoise empty: %v", err)
	}
	if len(stripped) != 0 {
		t.Errorf("expected empty result, got %d bytes", len(stripped))
	}
}

func TestAddNoise_ZeroNoiseLevel(t *testing.T) {
	cfg := DefaultEnhancedLETHEConfig()
	cfg.NoiseLevel = 0
	el := NewEnhancedLETHE(cfg)

	original := []byte("no noise")
	noisy := el.AddNoise(original)

	// Should still have length prefix.
	if len(noisy) != 4+len(original) {
		t.Errorf("expected %d bytes with zero noise, got %d", 4+len(original), len(noisy))
	}

	stripped, err := el.StripNoise(noisy)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(stripped, original) {
		t.Error("round trip failed with zero noise")
	}
}

func TestStripNoise_TooShort(t *testing.T) {
	el := NewEnhancedLETHE(DefaultEnhancedLETHEConfig())

	_, err := el.StripNoise([]byte{0x01, 0x02})
	if err != ErrLETHEInvalidNoiseData {
		t.Errorf("expected ErrLETHEInvalidNoiseData, got %v", err)
	}
}

func TestStripNoise_InvalidLength(t *testing.T) {
	el := NewEnhancedLETHE(DefaultEnhancedLETHEConfig())

	// Length prefix says 100 bytes but payload is only 4 bytes total.
	bad := []byte{0x00, 0x00, 0x00, 0x64} // length = 100
	_, err := el.StripNoise(bad)
	if err != ErrLETHEInvalidNoiseData {
		t.Errorf("expected ErrLETHEInvalidNoiseData, got %v", err)
	}
}

func TestCloseSession(t *testing.T) {
	el := NewEnhancedLETHE(DefaultEnhancedLETHEConfig())
	session, _ := el.CreateSession([]string{"v1"})

	if err := el.CloseSession(session.ID); err != nil {
		t.Fatalf("CloseSession: %v", err)
	}

	if el.ActiveSessions() != 0 {
		t.Errorf("expected 0 active sessions, got %d", el.ActiveSessions())
	}

	// Double close should fail.
	if err := el.CloseSession(session.ID); err != ErrLETHESessionNotFound {
		t.Errorf("expected ErrLETHESessionNotFound on double close, got %v", err)
	}
}

func TestCloseSession_NotFound(t *testing.T) {
	el := NewEnhancedLETHE(DefaultEnhancedLETHEConfig())
	err := el.CloseSession("nonexistent")
	if err != ErrLETHESessionNotFound {
		t.Errorf("expected ErrLETHESessionNotFound, got %v", err)
	}
}

func TestGetSession(t *testing.T) {
	el := NewEnhancedLETHE(DefaultEnhancedLETHEConfig())
	session, _ := el.CreateSession([]string{"v1", "v2"})

	got, err := el.GetSession(session.ID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if got.ID != session.ID {
		t.Error("session ID mismatch")
	}
	if len(got.Participants) != 2 {
		t.Errorf("expected 2 participants, got %d", len(got.Participants))
	}
}

func TestGetSession_NotFound(t *testing.T) {
	el := NewEnhancedLETHE(DefaultEnhancedLETHEConfig())
	_, err := el.GetSession("nonexistent")
	if err != ErrLETHESessionNotFound {
		t.Errorf("expected ErrLETHESessionNotFound, got %v", err)
	}
}

func TestMixBufferSize_NotFound(t *testing.T) {
	el := NewEnhancedLETHE(DefaultEnhancedLETHEConfig())
	if el.MixBufferSize("nonexistent") != -1 {
		t.Error("expected -1 for nonexistent session")
	}
}

func TestEnhancedLETHE_ConcurrentSessions(t *testing.T) {
	el := NewEnhancedLETHE(DefaultEnhancedLETHEConfig())
	var wg sync.WaitGroup
	errs := make(chan error, 100)

	// Concurrent session creation.
	sessionIDs := make(chan string, 30)
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			s, err := el.CreateSession([]string{fmt.Sprintf("v-%d", n)})
			if err != nil {
				errs <- err
				return
			}
			sessionIDs <- s.ID
		}(i)
	}

	wg.Wait()
	close(sessionIDs)
	close(errs)

	for err := range errs {
		t.Errorf("concurrent error: %v", err)
	}

	if el.ActiveSessions() != 30 {
		t.Errorf("expected 30 active sessions, got %d", el.ActiveSessions())
	}
}

func TestEnhancedLETHE_ConcurrentSubmitAndFlush(t *testing.T) {
	cfg := DefaultEnhancedLETHEConfig()
	cfg.MinMixSize = 1 // low threshold for testing
	el := NewEnhancedLETHE(cfg)

	session, _ := el.CreateSession([]string{"v1"})
	var wg sync.WaitGroup
	errs := make(chan error, 100)

	// Concurrent submits.
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			data := []byte(fmt.Sprintf("data-%d", n))
			if err := el.SubmitToMix(session.ID, data); err != nil {
				// Session may have been flushed (status changed), that's ok.
				if err != ErrLETHESessionClosed {
					errs <- err
				}
			}
		}(i)
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("concurrent submit error: %v", err)
	}
}

func TestFlushMix_DataIntegrity(t *testing.T) {
	cfg := DefaultEnhancedLETHEConfig()
	cfg.MinMixSize = 1
	el := NewEnhancedLETHE(cfg)

	session, _ := el.CreateSession([]string{"v1"})

	// Submit unique items.
	items := []string{"alpha", "bravo", "charlie", "delta", "echo"}
	for _, item := range items {
		el.SubmitToMix(session.ID, []byte(item))
	}

	result, err := el.FlushMix(session.ID)
	if err != nil {
		t.Fatal(err)
	}

	if len(result) != len(items) {
		t.Fatalf("expected %d items, got %d", len(items), len(result))
	}

	// Verify all items present regardless of order.
	found := make(map[string]bool)
	for _, r := range result {
		found[string(r)] = true
	}
	for _, item := range items {
		if !found[item] {
			t.Errorf("missing item after flush: %s", item)
		}
	}
}

func TestNoiseProducesDifferentOutput(t *testing.T) {
	el := NewEnhancedLETHE(DefaultEnhancedLETHEConfig())
	data := []byte("same data")

	noisy1 := el.AddNoise(data)
	noisy2 := el.AddNoise(data)

	// Due to random noise, the two outputs should differ.
	if bytes.Equal(noisy1, noisy2) {
		t.Error("two noise wrappings of same data should differ (random noise)")
	}

	// But both should strip to the same original.
	s1, _ := el.StripNoise(noisy1)
	s2, _ := el.StripNoise(noisy2)
	if !bytes.Equal(s1, s2) {
		t.Error("stripped data should be identical")
	}
}

func TestDefaultEnhancedLETHEConfig(t *testing.T) {
	cfg := DefaultEnhancedLETHEConfig()
	if cfg.NoiseLevel != 32 {
		t.Errorf("expected NoiseLevel 32, got %d", cfg.NoiseLevel)
	}
	if cfg.MaxSessions != 256 {
		t.Errorf("expected MaxSessions 256, got %d", cfg.MaxSessions)
	}
	if cfg.MinMixSize != 3 {
		t.Errorf("expected MinMixSize 3, got %d", cfg.MinMixSize)
	}
	if cfg.MaxMixBuffer != 1024 {
		t.Errorf("expected MaxMixBuffer 1024, got %d", cfg.MaxMixBuffer)
	}
}
