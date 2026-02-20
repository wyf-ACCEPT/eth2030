package p2p

import (
	"errors"
	"testing"
)

func TestProtoDispatcher_RegisterVersion(t *testing.T) {
	pd := NewProtoDispatcher("eth")

	err := pd.RegisterVersion(ProtoVersionSpec{
		Name:       "eth",
		Version:    68,
		MaxMsgCode: 0x14,
	})
	if err != nil {
		t.Fatalf("RegisterVersion failed: %v", err)
	}

	// Duplicate version should error.
	err = pd.RegisterVersion(ProtoVersionSpec{
		Name:       "eth",
		Version:    68,
		MaxMsgCode: 0x14,
	})
	if err == nil {
		t.Fatal("expected error for duplicate version")
	}
}

func TestProtoDispatcher_RegisterHandler(t *testing.T) {
	pd := NewProtoDispatcher("eth")
	pd.RegisterVersion(ProtoVersionSpec{Name: "eth", Version: 68, MaxMsgCode: 0x14})

	handler := func(peerID string, code uint64, payload []byte) error {
		return nil
	}

	err := pd.RegisterHandler(68, 0x03, handler)
	if err != nil {
		t.Fatalf("RegisterHandler failed: %v", err)
	}

	// Duplicate registration should error.
	err = pd.RegisterHandler(68, 0x03, handler)
	if err != ErrProtoHandlerExists {
		t.Fatalf("expected ErrProtoHandlerExists, got %v", err)
	}
}

func TestProtoDispatcher_RegisterHandlerInvalidVersion(t *testing.T) {
	pd := NewProtoDispatcher("eth")

	handler := func(peerID string, code uint64, payload []byte) error {
		return nil
	}

	err := pd.RegisterHandler(99, 0x03, handler)
	if err != ErrProtoVersionNotRegistered {
		t.Fatalf("expected ErrProtoVersionNotRegistered, got %v", err)
	}
}

func TestProtoDispatcher_RegisterHandlerExceedsMaxCode(t *testing.T) {
	pd := NewProtoDispatcher("eth")
	pd.RegisterVersion(ProtoVersionSpec{Name: "eth", Version: 68, MaxMsgCode: 0x10})

	handler := func(peerID string, code uint64, payload []byte) error {
		return nil
	}

	err := pd.RegisterHandler(68, 0x20, handler)
	if err == nil {
		t.Fatal("expected error for code exceeding max")
	}
}

func TestProtoDispatcher_Route(t *testing.T) {
	pd := NewProtoDispatcher("eth")
	pd.RegisterVersion(ProtoVersionSpec{Name: "eth", Version: 68, MaxMsgCode: 0x14})

	var routedPeerID string
	var routedCode uint64
	var routedPayload []byte

	pd.RegisterHandler(68, 0x03, func(peerID string, code uint64, payload []byte) error {
		routedPeerID = peerID
		routedCode = code
		routedPayload = payload
		return nil
	})

	err := pd.Route("peer1", 68, 0x03, []byte("test"))
	if err != nil {
		t.Fatalf("Route failed: %v", err)
	}
	if routedPeerID != "peer1" {
		t.Fatalf("expected peer1, got %s", routedPeerID)
	}
	if routedCode != 0x03 {
		t.Fatalf("expected code 0x03, got 0x%x", routedCode)
	}
	if string(routedPayload) != "test" {
		t.Fatalf("expected 'test', got '%s'", string(routedPayload))
	}
}

func TestProtoDispatcher_RouteNoHandler(t *testing.T) {
	pd := NewProtoDispatcher("eth")
	pd.RegisterVersion(ProtoVersionSpec{Name: "eth", Version: 68, MaxMsgCode: 0x14})

	err := pd.Route("peer1", 68, 0x99, nil)
	if !errors.Is(err, ErrProtoNoVersionHandler) {
		t.Fatalf("expected ErrProtoNoVersionHandler, got %v", err)
	}
}

func TestProtoDispatcher_RouteHandlerError(t *testing.T) {
	pd := NewProtoDispatcher("eth")
	pd.RegisterVersion(ProtoVersionSpec{Name: "eth", Version: 68, MaxMsgCode: 0x14})

	handlerErr := errors.New("handler error")
	pd.RegisterHandler(68, 0x03, func(peerID string, code uint64, payload []byte) error {
		return handlerErr
	})

	err := pd.Route("peer1", 68, 0x03, nil)
	if err != handlerErr {
		t.Fatalf("expected handler error, got %v", err)
	}
}

func TestProtoDispatcher_RouteWithFallback(t *testing.T) {
	pd := NewProtoDispatcher("eth")
	pd.RegisterVersion(ProtoVersionSpec{Name: "eth", Version: 68, MaxMsgCode: 0x14})
	pd.RegisterVersion(ProtoVersionSpec{Name: "eth", Version: 70, MaxMsgCode: 0x14})

	var handlerVersion string

	pd.RegisterHandler(68, 0x03, func(peerID string, code uint64, payload []byte) error {
		handlerVersion = "v68"
		return nil
	})

	// Route with version 70 should fall back to v68 handler.
	err := pd.RouteWithFallback("peer1", 70, 0x03, nil)
	if err != nil {
		t.Fatalf("RouteWithFallback failed: %v", err)
	}
	if handlerVersion != "v68" {
		t.Fatalf("expected v68 handler, got %s", handlerVersion)
	}
}

func TestProtoDispatcher_RouteWithFallbackExactMatch(t *testing.T) {
	pd := NewProtoDispatcher("eth")
	pd.RegisterVersion(ProtoVersionSpec{Name: "eth", Version: 68, MaxMsgCode: 0x14})
	pd.RegisterVersion(ProtoVersionSpec{Name: "eth", Version: 70, MaxMsgCode: 0x14})

	var handlerVersion string

	pd.RegisterHandler(68, 0x03, func(peerID string, code uint64, payload []byte) error {
		handlerVersion = "v68"
		return nil
	})
	pd.RegisterHandler(70, 0x03, func(peerID string, code uint64, payload []byte) error {
		handlerVersion = "v70"
		return nil
	})

	// Route with version 70 should use the exact match.
	err := pd.RouteWithFallback("peer1", 70, 0x03, nil)
	if err != nil {
		t.Fatalf("RouteWithFallback failed: %v", err)
	}
	if handlerVersion != "v70" {
		t.Fatalf("expected v70 handler, got %s", handlerVersion)
	}
}

func TestProtoDispatcher_SupportedVersions(t *testing.T) {
	pd := NewProtoDispatcher("eth")
	pd.RegisterVersion(ProtoVersionSpec{Name: "eth", Version: 70, MaxMsgCode: 0x14})
	pd.RegisterVersion(ProtoVersionSpec{Name: "eth", Version: 68, MaxMsgCode: 0x14})
	pd.RegisterVersion(ProtoVersionSpec{Name: "eth", Version: 71, MaxMsgCode: 0x14})

	versions := pd.SupportedVersions()
	if len(versions) != 3 {
		t.Fatalf("expected 3 versions, got %d", len(versions))
	}
	// Should be sorted ascending.
	if versions[0] != 68 || versions[1] != 70 || versions[2] != 71 {
		t.Fatalf("expected [68, 70, 71], got %v", versions)
	}
}

func TestProtoDispatcher_HighestVersion(t *testing.T) {
	pd := NewProtoDispatcher("eth")

	// No versions registered.
	if pd.HighestVersion() != 0 {
		t.Fatalf("expected 0 for no versions, got %d", pd.HighestVersion())
	}

	pd.RegisterVersion(ProtoVersionSpec{Name: "eth", Version: 68, MaxMsgCode: 0x14})
	pd.RegisterVersion(ProtoVersionSpec{Name: "eth", Version: 71, MaxMsgCode: 0x14})

	if pd.HighestVersion() != 71 {
		t.Fatalf("expected 71, got %d", pd.HighestVersion())
	}
}

func TestProtoDispatcher_Capabilities(t *testing.T) {
	pd := NewProtoDispatcher("eth")
	pd.RegisterVersion(ProtoVersionSpec{Name: "eth", Version: 68, MaxMsgCode: 0x14})
	pd.RegisterVersion(ProtoVersionSpec{Name: "eth", Version: 70, MaxMsgCode: 0x14})

	caps := pd.Capabilities()
	if len(caps) != 2 {
		t.Fatalf("expected 2 capabilities, got %d", len(caps))
	}
	if caps[0].Name != "eth" || caps[0].Version != 68 {
		t.Fatalf("expected eth/68, got %s/%d", caps[0].Name, caps[0].Version)
	}
	if caps[1].Name != "eth" || caps[1].Version != 70 {
		t.Fatalf("expected eth/70, got %s/%d", caps[1].Name, caps[1].Version)
	}
}

func TestProtoDispatcher_NegotiateVersion(t *testing.T) {
	pd := NewProtoDispatcher("eth")
	pd.RegisterVersion(ProtoVersionSpec{Name: "eth", Version: 68, MaxMsgCode: 0x14})
	pd.RegisterVersion(ProtoVersionSpec{Name: "eth", Version: 70, MaxMsgCode: 0x14})

	// Remote supports eth/68 and eth/70.
	remoteCaps := []Capability{
		{Name: "eth", Version: 68},
		{Name: "eth", Version: 70},
		{Name: "snap", Version: 1},
	}

	version, err := pd.NegotiateVersion(remoteCaps)
	if err != nil {
		t.Fatalf("NegotiateVersion failed: %v", err)
	}
	if version != 70 {
		t.Fatalf("expected negotiated version 70, got %d", version)
	}
}

func TestProtoDispatcher_NegotiateVersionNoMatch(t *testing.T) {
	pd := NewProtoDispatcher("eth")
	pd.RegisterVersion(ProtoVersionSpec{Name: "eth", Version: 68, MaxMsgCode: 0x14})

	// Remote only supports snap protocol.
	remoteCaps := []Capability{
		{Name: "snap", Version: 1},
	}

	_, err := pd.NegotiateVersion(remoteCaps)
	if err != ErrProtoPeerIncompatible {
		t.Fatalf("expected ErrProtoPeerIncompatible, got %v", err)
	}
}

func TestProtoDispatcher_NegotiateVersionPartialMatch(t *testing.T) {
	pd := NewProtoDispatcher("eth")
	pd.RegisterVersion(ProtoVersionSpec{Name: "eth", Version: 68, MaxMsgCode: 0x14})
	pd.RegisterVersion(ProtoVersionSpec{Name: "eth", Version: 70, MaxMsgCode: 0x14})

	// Remote supports eth/71 (not our version) and eth/68.
	remoteCaps := []Capability{
		{Name: "eth", Version: 71},
		{Name: "eth", Version: 68},
	}

	version, err := pd.NegotiateVersion(remoteCaps)
	if err != nil {
		t.Fatalf("NegotiateVersion failed: %v", err)
	}
	if version != 68 {
		t.Fatalf("expected negotiated version 68, got %d", version)
	}
}

func TestProtoDispatcher_SetHandler(t *testing.T) {
	pd := NewProtoDispatcher("eth")
	pd.RegisterVersion(ProtoVersionSpec{Name: "eth", Version: 68, MaxMsgCode: 0x14})

	handler1 := func(peerID string, code uint64, payload []byte) error {
		return nil
	}
	handler2 := func(peerID string, code uint64, payload []byte) error {
		return errors.New("handler2")
	}

	pd.RegisterHandler(68, 0x03, handler1)

	// SetHandler should replace without error.
	err := pd.SetHandler(68, 0x03, handler2)
	if err != nil {
		t.Fatalf("SetHandler failed: %v", err)
	}

	// Routing should use the new handler.
	err = pd.Route("peer1", 68, 0x03, nil)
	if err == nil || err.Error() != "handler2" {
		t.Fatalf("expected handler2 error, got %v", err)
	}
}

func TestProtoDispatcher_SetHandlerNilRemoves(t *testing.T) {
	pd := NewProtoDispatcher("eth")
	pd.RegisterVersion(ProtoVersionSpec{Name: "eth", Version: 68, MaxMsgCode: 0x14})

	pd.RegisterHandler(68, 0x03, func(peerID string, code uint64, payload []byte) error {
		return nil
	})

	if !pd.HasHandler(68, 0x03) {
		t.Fatal("expected handler to exist")
	}

	pd.SetHandler(68, 0x03, nil)

	if pd.HasHandler(68, 0x03) {
		t.Fatal("expected handler to be removed")
	}
}

func TestProtoDispatcher_HandlerCount(t *testing.T) {
	pd := NewProtoDispatcher("eth")
	pd.RegisterVersion(ProtoVersionSpec{Name: "eth", Version: 68, MaxMsgCode: 0x14})

	handler := func(peerID string, code uint64, payload []byte) error { return nil }
	pd.RegisterHandler(68, 0x01, handler)
	pd.RegisterHandler(68, 0x02, handler)
	pd.RegisterHandler(68, 0x03, handler)

	if pd.HandlerCount() != 3 {
		t.Fatalf("expected 3 handlers, got %d", pd.HandlerCount())
	}
}

func TestProtoDispatcher_Name(t *testing.T) {
	pd := NewProtoDispatcher("snap")
	if pd.Name() != "snap" {
		t.Fatalf("expected 'snap', got '%s'", pd.Name())
	}
}

func TestProtoDispatcher_Close(t *testing.T) {
	pd := NewProtoDispatcher("eth")
	pd.RegisterVersion(ProtoVersionSpec{Name: "eth", Version: 68, MaxMsgCode: 0x14})
	pd.RegisterHandler(68, 0x03, func(peerID string, code uint64, payload []byte) error {
		return nil
	})

	pd.Close()

	err := pd.Route("peer1", 68, 0x03, nil)
	if err != ErrProtoDispatcherClosed {
		t.Fatalf("expected ErrProtoDispatcherClosed, got %v", err)
	}

	err = pd.RegisterVersion(ProtoVersionSpec{Name: "eth", Version: 71, MaxMsgCode: 0x14})
	if err != ErrProtoDispatcherClosed {
		t.Fatalf("expected ErrProtoDispatcherClosed for RegisterVersion, got %v", err)
	}
}
