package eth

import (
	"errors"
	"sync"
	"testing"
)

func TestProtocolVersionString(t *testing.T) {
	tests := []struct {
		version ProtocolVersion
		want    string
	}{
		{ETH66Version, "ETH/66"},
		{ETH67Version, "ETH/67"},
		{ETH68Version, "ETH/68"},
		{ProtocolVersion{Major: 70, Name: "eth/70"}, "ETH/70"},
	}
	for _, tt := range tests {
		got := tt.version.String()
		if got != tt.want {
			t.Errorf("%s.String() = %q, want %q", tt.version.Name, got, tt.want)
		}
	}
}

func TestProtocolVersionEqual(t *testing.T) {
	a := ProtocolVersion{Major: 68, Minor: 0, Patch: 0}
	b := ProtocolVersion{Major: 68, Minor: 0, Patch: 0, Name: "different name"}
	c := ProtocolVersion{Major: 67, Minor: 0, Patch: 0}

	if !a.Equal(b) {
		t.Error("identical major/minor/patch should be equal")
	}
	if a.Equal(c) {
		t.Error("different major versions should not be equal")
	}
}

func TestProtocolVersionLess(t *testing.T) {
	tests := []struct {
		a, b ProtocolVersion
		want bool
	}{
		{ETH66Version, ETH67Version, true},
		{ETH68Version, ETH67Version, false},
		{ETH67Version, ETH67Version, false},
		{
			ProtocolVersion{Major: 68, Minor: 0, Patch: 0},
			ProtocolVersion{Major: 68, Minor: 1, Patch: 0},
			true,
		},
		{
			ProtocolVersion{Major: 68, Minor: 1, Patch: 0},
			ProtocolVersion{Major: 68, Minor: 1, Patch: 1},
			true,
		},
	}
	for _, tt := range tests {
		got := tt.a.Less(tt.b)
		if got != tt.want {
			t.Errorf("%v.Less(%v) = %v, want %v", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestNewVersionManager(t *testing.T) {
	vm := NewVersionManager([]ProtocolVersion{ETH66Version, ETH68Version, ETH67Version})
	versions := vm.SupportedVersions()

	if len(versions) != 3 {
		t.Fatalf("expected 3 supported versions, got %d", len(versions))
	}
	// Should be sorted descending: 68, 67, 66.
	if versions[0].Major != 68 {
		t.Errorf("first version should be 68, got %d", versions[0].Major)
	}
	if versions[1].Major != 67 {
		t.Errorf("second version should be 67, got %d", versions[1].Major)
	}
	if versions[2].Major != 66 {
		t.Errorf("third version should be 66, got %d", versions[2].Major)
	}
}

func TestNegotiateVersion_HighestCommon(t *testing.T) {
	vm := NewVersionManager([]ProtocolVersion{ETH66Version, ETH67Version, ETH68Version})

	// Peer supports 66 and 67, so highest common is 67.
	result, err := vm.NegotiateVersion([]ProtocolVersion{ETH66Version, ETH67Version})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Major != 67 {
		t.Errorf("negotiated version = %d, want 67", result.Major)
	}
}

func TestNegotiateVersion_ExactMatch(t *testing.T) {
	vm := NewVersionManager([]ProtocolVersion{ETH68Version})

	result, err := vm.NegotiateVersion([]ProtocolVersion{ETH68Version})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Equal(ETH68Version) {
		t.Errorf("expected ETH68, got %v", result)
	}
}

func TestNegotiateVersion_NoCommon(t *testing.T) {
	vm := NewVersionManager([]ProtocolVersion{ETH68Version})

	_, err := vm.NegotiateVersion([]ProtocolVersion{ETH66Version})
	if !errors.Is(err, ErrNoCommonVersion) {
		t.Errorf("expected ErrNoCommonVersion, got %v", err)
	}
}

func TestNegotiateVersion_EmptyPeerVersions(t *testing.T) {
	vm := NewVersionManager([]ProtocolVersion{ETH68Version})

	_, err := vm.NegotiateVersion(nil)
	if !errors.Is(err, ErrNoVersions) {
		t.Errorf("expected ErrNoVersions, got %v", err)
	}

	_, err = vm.NegotiateVersion([]ProtocolVersion{})
	if !errors.Is(err, ErrNoVersions) {
		t.Errorf("expected ErrNoVersions for empty slice, got %v", err)
	}
}

func TestIsSupported(t *testing.T) {
	vm := NewVersionManager([]ProtocolVersion{ETH67Version, ETH68Version})

	if !vm.IsSupported(ETH68Version) {
		t.Error("ETH68 should be supported")
	}
	if !vm.IsSupported(ETH67Version) {
		t.Error("ETH67 should be supported")
	}
	if vm.IsSupported(ETH66Version) {
		t.Error("ETH66 should not be supported")
	}
}

func TestRegisterAndGetPeerVersion(t *testing.T) {
	vm := NewVersionManager([]ProtocolVersion{ETH67Version, ETH68Version})

	// Peer not yet registered.
	if v := vm.GetPeerVersion("peer1"); v != nil {
		t.Errorf("expected nil for unregistered peer, got %v", v)
	}

	// Register a peer.
	vm.RegisterPeer("peer1", ETH68Version)
	v := vm.GetPeerVersion("peer1")
	if v == nil {
		t.Fatal("expected non-nil version for registered peer")
	}
	if !v.Equal(ETH68Version) {
		t.Errorf("peer version = %v, want ETH68", v)
	}
}

func TestRemovePeer(t *testing.T) {
	vm := NewVersionManager([]ProtocolVersion{ETH68Version})

	vm.RegisterPeer("peer1", ETH68Version)
	vm.RemovePeer("peer1")

	if v := vm.GetPeerVersion("peer1"); v != nil {
		t.Errorf("expected nil after removal, got %v", v)
	}
}

func TestRemovePeer_NonExistent(t *testing.T) {
	vm := NewVersionManager([]ProtocolVersion{ETH68Version})
	// Should not panic.
	vm.RemovePeer("nonexistent")
}

func TestPeerCount(t *testing.T) {
	vm := NewVersionManager([]ProtocolVersion{ETH68Version})

	if vm.PeerCount() != 0 {
		t.Errorf("expected 0 peers, got %d", vm.PeerCount())
	}

	vm.RegisterPeer("peer1", ETH68Version)
	vm.RegisterPeer("peer2", ETH67Version)

	if vm.PeerCount() != 2 {
		t.Errorf("expected 2 peers, got %d", vm.PeerCount())
	}

	vm.RemovePeer("peer1")
	if vm.PeerCount() != 1 {
		t.Errorf("expected 1 peer after removal, got %d", vm.PeerCount())
	}
}

func TestHighestSupported(t *testing.T) {
	vm := NewVersionManager([]ProtocolVersion{ETH66Version, ETH67Version, ETH68Version})

	v, err := vm.HighestSupported()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v.Major != 68 {
		t.Errorf("highest = %d, want 68", v.Major)
	}
}

func TestHighestSupported_Empty(t *testing.T) {
	vm := NewVersionManager(nil)

	_, err := vm.HighestSupported()
	if !errors.Is(err, ErrNoVersions) {
		t.Errorf("expected ErrNoVersions, got %v", err)
	}
}

func TestSupportedVersions_ReturnsCopy(t *testing.T) {
	vm := NewVersionManager([]ProtocolVersion{ETH66Version, ETH68Version})

	v1 := vm.SupportedVersions()
	v2 := vm.SupportedVersions()

	// Modifying the returned slice should not affect the internal state.
	v1[0] = ProtocolVersion{Major: 99}
	v2Again := vm.SupportedVersions()

	if v2Again[0].Major == 99 {
		t.Error("modifying returned slice should not affect internal state")
	}
	// The second call should still show 68 first.
	if v2[0].Major != 68 {
		t.Errorf("expected 68, got %d", v2[0].Major)
	}
}

func TestRegisterPeer_Overwrite(t *testing.T) {
	vm := NewVersionManager([]ProtocolVersion{ETH67Version, ETH68Version})

	vm.RegisterPeer("peer1", ETH67Version)
	vm.RegisterPeer("peer1", ETH68Version)

	v := vm.GetPeerVersion("peer1")
	if v == nil {
		t.Fatal("expected non-nil version")
	}
	if v.Major != 68 {
		t.Errorf("expected overwritten version to be 68, got %d", v.Major)
	}
}

func TestVersionManager_ConcurrentAccess(t *testing.T) {
	vm := NewVersionManager([]ProtocolVersion{ETH66Version, ETH67Version, ETH68Version})

	var wg sync.WaitGroup
	// Concurrent writers.
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			peerID := "peer" + string(rune('A'+id%26))
			vm.RegisterPeer(peerID, ETH68Version)
			vm.GetPeerVersion(peerID)
			vm.RemovePeer(peerID)
		}(i)
	}
	// Concurrent readers.
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			vm.SupportedVersions()
			vm.IsSupported(ETH68Version)
			vm.NegotiateVersion([]ProtocolVersion{ETH67Version, ETH68Version})
			vm.PeerCount()
		}()
	}
	wg.Wait()
}

func TestNegotiateVersion_MultiplePeerVersions(t *testing.T) {
	// Local supports only 67 and 68.
	vm := NewVersionManager([]ProtocolVersion{ETH67Version, ETH68Version})

	// Peer supports 66, 67, 68 - highest common should be 68.
	result, err := vm.NegotiateVersion([]ProtocolVersion{ETH66Version, ETH67Version, ETH68Version})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Major != 68 {
		t.Errorf("negotiated = %d, want 68", result.Major)
	}
}

func TestNegotiateVersion_PeerHigherThanLocal(t *testing.T) {
	// Local supports only 66 and 67.
	vm := NewVersionManager([]ProtocolVersion{ETH66Version, ETH67Version})

	// Peer supports 68 only - no common version.
	_, err := vm.NegotiateVersion([]ProtocolVersion{ETH68Version})
	if !errors.Is(err, ErrNoCommonVersion) {
		t.Errorf("expected ErrNoCommonVersion, got %v", err)
	}

	// Peer supports 67 and 68 - common is 67.
	result, err := vm.NegotiateVersion([]ProtocolVersion{ETH67Version, ETH68Version})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Major != 67 {
		t.Errorf("negotiated = %d, want 67", result.Major)
	}
}
