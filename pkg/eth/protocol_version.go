package eth

import (
	"errors"
	"fmt"
	"sort"
	"sync"
)

// ProtocolVersion represents a version of the ETH wire protocol.
type ProtocolVersion struct {
	Major uint8
	Minor uint8
	Patch uint8
	Name  string
}

// Pre-defined ETH protocol versions.
var (
	ETH66Version = ProtocolVersion{Major: 66, Minor: 0, Patch: 0, Name: "eth/66"}
	ETH67Version = ProtocolVersion{Major: 67, Minor: 0, Patch: 0, Name: "eth/67"}
	ETH68Version = ProtocolVersion{Major: 68, Minor: 0, Patch: 0, Name: "eth/68"}
)

var (
	ErrNoCommonVersion = errors.New("no common protocol version found")
	ErrPeerNotFound    = errors.New("peer not found")
	ErrNoVersions      = errors.New("no versions provided")
)

// String returns the protocol version in "ETH/68" format.
func (v ProtocolVersion) String() string {
	return fmt.Sprintf("ETH/%d", v.Major)
}

// Equal returns true if two protocol versions have the same major, minor,
// and patch numbers.
func (v ProtocolVersion) Equal(other ProtocolVersion) bool {
	return v.Major == other.Major && v.Minor == other.Minor && v.Patch == other.Patch
}

// Less returns true if v is lower than other, comparing major then minor
// then patch.
func (v ProtocolVersion) Less(other ProtocolVersion) bool {
	if v.Major != other.Major {
		return v.Major < other.Major
	}
	if v.Minor != other.Minor {
		return v.Minor < other.Minor
	}
	return v.Patch < other.Patch
}

// VersionManager tracks supported protocol versions and negotiated versions
// per peer. Thread-safe via sync.RWMutex.
type VersionManager struct {
	mu        sync.RWMutex
	supported []ProtocolVersion
	peers     map[string]ProtocolVersion
}

// NewVersionManager creates a new VersionManager with the given supported
// versions. The versions are sorted in descending order (highest first).
func NewVersionManager(supported []ProtocolVersion) *VersionManager {
	// Make a copy so the caller's slice is not modified.
	sorted := make([]ProtocolVersion, len(supported))
	copy(sorted, supported)
	// Sort descending by version number.
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[j].Less(sorted[i])
	})
	return &VersionManager{
		supported: sorted,
		peers:     make(map[string]ProtocolVersion),
	}
}

// NegotiateVersion finds the highest protocol version common to both the
// local supported set and the provided peer versions. Returns an error if
// no common version exists or if the peer provides an empty list.
func (vm *VersionManager) NegotiateVersion(peerVersions []ProtocolVersion) (*ProtocolVersion, error) {
	if len(peerVersions) == 0 {
		return nil, ErrNoVersions
	}

	vm.mu.RLock()
	defer vm.mu.RUnlock()

	// Build a set of peer versions for O(1) lookup.
	peerSet := make(map[[3]uint8]ProtocolVersion, len(peerVersions))
	for _, pv := range peerVersions {
		key := [3]uint8{pv.Major, pv.Minor, pv.Patch}
		peerSet[key] = pv
	}

	// Walk our supported versions from highest to lowest and find the first
	// version the peer also supports.
	for _, sv := range vm.supported {
		key := [3]uint8{sv.Major, sv.Minor, sv.Patch}
		if _, ok := peerSet[key]; ok {
			result := sv
			return &result, nil
		}
	}

	return nil, ErrNoCommonVersion
}

// IsSupported returns true if the given version is in the supported set.
func (vm *VersionManager) IsSupported(version ProtocolVersion) bool {
	vm.mu.RLock()
	defer vm.mu.RUnlock()

	for _, sv := range vm.supported {
		if sv.Equal(version) {
			return true
		}
	}
	return false
}

// RegisterPeer records the negotiated protocol version for a peer.
func (vm *VersionManager) RegisterPeer(peerID string, version ProtocolVersion) {
	vm.mu.Lock()
	defer vm.mu.Unlock()
	vm.peers[peerID] = version
}

// GetPeerVersion returns the negotiated version for the given peer, or nil
// if the peer is not registered.
func (vm *VersionManager) GetPeerVersion(peerID string) *ProtocolVersion {
	vm.mu.RLock()
	defer vm.mu.RUnlock()

	v, ok := vm.peers[peerID]
	if !ok {
		return nil
	}
	return &v
}

// RemovePeer removes a peer from version tracking.
func (vm *VersionManager) RemovePeer(peerID string) {
	vm.mu.Lock()
	defer vm.mu.Unlock()
	delete(vm.peers, peerID)
}

// SupportedVersions returns a copy of the supported versions list, ordered
// from highest to lowest.
func (vm *VersionManager) SupportedVersions() []ProtocolVersion {
	vm.mu.RLock()
	defer vm.mu.RUnlock()

	result := make([]ProtocolVersion, len(vm.supported))
	copy(result, vm.supported)
	return result
}

// HighestSupported returns the highest supported protocol version, or an
// error if no versions are configured.
func (vm *VersionManager) HighestSupported() (*ProtocolVersion, error) {
	vm.mu.RLock()
	defer vm.mu.RUnlock()

	if len(vm.supported) == 0 {
		return nil, ErrNoVersions
	}
	v := vm.supported[0]
	return &v, nil
}

// PeerCount returns the number of registered peers.
func (vm *VersionManager) PeerCount() int {
	vm.mu.RLock()
	defer vm.mu.RUnlock()
	return len(vm.peers)
}
