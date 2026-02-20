// proto_negotiation.go implements sub-protocol negotiation including capability
// exchange, protocol matching, message ID offset computation for multiplexing,
// version compatibility checking, handshake timeout handling, and protocol-
// specific handshake delegation.
package p2p

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// Protocol negotiation errors.
var (
	ErrNegNoSharedProtocols = errors.New("p2p: no shared protocols found")
	ErrNegHandshakeTimeout  = errors.New("p2p: protocol negotiation timeout")
	ErrNegVersionMismatch   = errors.New("p2p: protocol version mismatch")
	ErrNegDuplicateProtocol = errors.New("p2p: duplicate protocol registration")
	ErrNegInvalidOffset     = errors.New("p2p: invalid message offset")
)

// ProtoCapability describes a sub-protocol capability with name, version,
// and its message code range for multiplexing.
type ProtoCapability struct {
	Name    string // Protocol name, e.g. "eth", "snap".
	Version uint   // Protocol version, e.g. 68, 1.
	Offset  uint64 // Base message code offset in the multiplexed stream.
	Length  uint64 // Number of message codes used by this protocol.
}

// String returns "name/version" representation.
func (pc ProtoCapability) String() string {
	return fmt.Sprintf("%s/%d", pc.Name, pc.Version)
}

// KnownProtocol stores a registered protocol and its message code length.
type KnownProtocol struct {
	Name    string
	Version uint
	Length  uint64 // Number of message codes this version uses.
}

// ProtoNegConfig configures the protocol negotiator.
type ProtoNegConfig struct {
	// HandshakeTimeout is the maximum time for protocol-specific handshakes.
	HandshakeTimeout time.Duration
	// MinETHVersion is the minimum acceptable ETH protocol version.
	MinETHVersion uint
	// MinSNAPVersion is the minimum acceptable SNAP protocol version.
	MinSNAPVersion uint
}

// DefaultProtoNegConfig returns production defaults.
func DefaultProtoNegConfig() ProtoNegConfig {
	return ProtoNegConfig{
		HandshakeTimeout: 10 * time.Second,
		MinETHVersion:    ETH68,
		MinSNAPVersion:   1,
	}
}

// ProtoHandshakeFunc performs a protocol-specific handshake on a transport.
// It receives the remote peer's ID and the negotiated version. Returns an
// error if the handshake fails.
type ProtoHandshakeFunc func(peerID string, version uint, tr Transport) error

// ProtoNeg handles sub-protocol capability exchange, matching, and message
// ID offset computation. It supports registering known protocols with their
// message code lengths and performing capability negotiation with remote peers.
// All methods are safe for concurrent use.
type ProtoNeg struct {
	mu       sync.RWMutex
	config   ProtoNegConfig
	known    []KnownProtocol
	handlers map[string]ProtoHandshakeFunc // "name/version" -> handshake func
}

// NewProtoNeg creates a new protocol negotiator with the given config.
func NewProtoNeg(cfg ProtoNegConfig) *ProtoNeg {
	if cfg.HandshakeTimeout <= 0 {
		cfg.HandshakeTimeout = 10 * time.Second
	}
	return &ProtoNeg{
		config:   cfg,
		known:    make([]KnownProtocol, 0, 8),
		handlers: make(map[string]ProtoHandshakeFunc),
	}
}

// RegisterProtocol adds a known protocol with its message code length.
// Returns an error if the exact name+version combination is already registered.
func (pn *ProtoNeg) RegisterProtocol(name string, version uint, length uint64) error {
	pn.mu.Lock()
	defer pn.mu.Unlock()

	for _, kp := range pn.known {
		if kp.Name == name && kp.Version == version {
			return ErrNegDuplicateProtocol
		}
	}
	pn.known = append(pn.known, KnownProtocol{
		Name:    name,
		Version: version,
		Length:  length,
	})
	return nil
}

// SetHandshakeFunc registers a protocol-specific handshake function for a
// given protocol name and version. This function will be called after
// capability matching to perform any protocol-level handshake.
func (pn *ProtoNeg) SetHandshakeFunc(name string, version uint, fn ProtoHandshakeFunc) {
	pn.mu.Lock()
	defer pn.mu.Unlock()
	key := fmt.Sprintf("%s/%d", name, version)
	pn.handlers[key] = fn
}

// LocalCapabilities returns the list of capabilities we advertise, sorted
// by name then version. These are suitable for inclusion in the hello packet.
func (pn *ProtoNeg) LocalCapabilities() []ProtoCapability {
	pn.mu.RLock()
	defer pn.mu.RUnlock()

	caps := make([]ProtoCapability, len(pn.known))
	for i, kp := range pn.known {
		caps[i] = ProtoCapability{
			Name:    kp.Name,
			Version: kp.Version,
			Length:  kp.Length,
		}
	}
	sortProtoCaps(caps)
	return caps
}

// NegotiateCapabilities performs capability matching between our known protocols
// and the remote peer's advertised capabilities. For each protocol name present
// in both lists, the highest mutually-supported version is selected. Returns
// the matched capabilities with computed message code offsets, or an error if
// no protocols match.
func (pn *ProtoNeg) NegotiateCapabilities(remoteCaps []ProtoCapability) ([]ProtoCapability, error) {
	pn.mu.RLock()
	defer pn.mu.RUnlock()

	// Build local map: name -> list of (version, length).
	type verLen struct {
		version uint
		length  uint64
	}
	localMap := make(map[string][]verLen)
	for _, kp := range pn.known {
		localMap[kp.Name] = append(localMap[kp.Name], verLen{kp.Version, kp.Length})
	}

	// Build remote map: name -> max version.
	remoteMap := make(map[string]uint)
	for _, rc := range remoteCaps {
		if v, ok := remoteMap[rc.Name]; !ok || rc.Version > v {
			remoteMap[rc.Name] = rc.Version
		}
	}

	// Find best shared version for each protocol.
	type matchResult struct {
		name    string
		version uint
		length  uint64
	}
	var matches []matchResult

	for name, versions := range localMap {
		remoteMax, ok := remoteMap[name]
		if !ok {
			continue
		}

		// Check minimum version requirements.
		minVer := pn.minVersionFor(name)

		// Find the highest local version that is <= remoteMax and >= minVer.
		var bestVer uint
		var bestLen uint64
		found := false
		for _, vl := range versions {
			if vl.version <= remoteMax && vl.version >= minVer {
				if !found || vl.version > bestVer {
					bestVer = vl.version
					bestLen = vl.length
					found = true
				}
			}
		}
		if found {
			matches = append(matches, matchResult{name, bestVer, bestLen})
		}
	}

	if len(matches) == 0 {
		return nil, ErrNegNoSharedProtocols
	}

	// Sort matches by name for deterministic offset assignment.
	sort.Slice(matches, func(i, j int) bool {
		return matches[i].name < matches[j].name
	})

	// Compute message code offsets. Each protocol gets a contiguous block of
	// message IDs starting after the base protocol messages (offset 0x10).
	const baseOffset uint64 = 0x10
	var offset = baseOffset
	result := make([]ProtoCapability, len(matches))
	for i, m := range matches {
		result[i] = ProtoCapability{
			Name:    m.name,
			Version: m.version,
			Offset:  offset,
			Length:  m.length,
		}
		offset += m.length
	}

	return result, nil
}

// PerformProtocolHandshakes runs protocol-specific handshakes for all matched
// capabilities, subject to the configured timeout. Returns an error if any
// handshake fails.
func (pn *ProtoNeg) PerformProtocolHandshakes(peerID string, matched []ProtoCapability, tr Transport) error {
	pn.mu.RLock()
	handlers := make(map[string]ProtoHandshakeFunc, len(pn.handlers))
	for k, v := range pn.handlers {
		handlers[k] = v
	}
	timeout := pn.config.HandshakeTimeout
	pn.mu.RUnlock()

	for _, cap := range matched {
		key := fmt.Sprintf("%s/%d", cap.Name, cap.Version)
		fn, ok := handlers[key]
		if !ok {
			continue // No protocol-specific handshake needed.
		}

		done := make(chan error, 1)
		go func() {
			done <- fn(peerID, cap.Version, tr)
		}()

		select {
		case err := <-done:
			if err != nil {
				return fmt.Errorf("p2p: %s handshake failed: %w", key, err)
			}
		case <-time.After(timeout):
			return fmt.Errorf("%w: %s handshake", ErrNegHandshakeTimeout, cap.Name)
		}
	}
	return nil
}

// IsVersionCompatible checks whether a remote protocol version is acceptable
// for the given protocol name, according to our minimum version constraints.
func (pn *ProtoNeg) IsVersionCompatible(name string, version uint) bool {
	pn.mu.RLock()
	defer pn.mu.RUnlock()

	minVer := pn.minVersionFor(name)
	return version >= minVer
}

// ComputeOffsets takes a list of matched capabilities and computes fresh
// message code offsets, returning the updated list. This is useful after
// renegotiation.
func (pn *ProtoNeg) ComputeOffsets(caps []ProtoCapability) []ProtoCapability {
	sorted := make([]ProtoCapability, len(caps))
	copy(sorted, caps)
	sortProtoCaps(sorted)

	const baseOffset uint64 = 0x10
	offset := baseOffset
	for i := range sorted {
		sorted[i].Offset = offset
		offset += sorted[i].Length
	}
	return sorted
}

// TotalMessageSpace returns the total message code space needed for the
// given set of matched capabilities.
func (pn *ProtoNeg) TotalMessageSpace(caps []ProtoCapability) uint64 {
	var total uint64
	for _, c := range caps {
		total += c.Length
	}
	return total
}

// FindProtocol returns the matched capability for a given protocol name,
// or nil if not found in the list.
func FindProtocol(caps []ProtoCapability, name string) *ProtoCapability {
	for i, c := range caps {
		if c.Name == name {
			return &caps[i]
		}
	}
	return nil
}

// MessageToProtocol finds which protocol owns a given wire message code
// and returns the protocol capability and the protocol-relative code.
func MessageToProtocol(caps []ProtoCapability, wireCode uint64) (*ProtoCapability, uint64, error) {
	for i, c := range caps {
		if wireCode >= c.Offset && wireCode < c.Offset+c.Length {
			return &caps[i], wireCode - c.Offset, nil
		}
	}
	return nil, 0, fmt.Errorf("%w: code 0x%02x", ErrNegInvalidOffset, wireCode)
}

// CapsToCaps converts ProtoCapability list to the handshake Cap format.
func CapsToCaps(caps []ProtoCapability) []Cap {
	result := make([]Cap, len(caps))
	for i, c := range caps {
		result[i] = Cap{Name: c.Name, Version: c.Version}
	}
	return result
}

// CapsFromCaps converts handshake Cap list to ProtoCapability (without offsets).
func CapsFromCaps(caps []Cap) []ProtoCapability {
	result := make([]ProtoCapability, len(caps))
	for i, c := range caps {
		result[i] = ProtoCapability{Name: c.Name, Version: c.Version}
	}
	return result
}

// --- internal helpers ---

// minVersionFor returns the minimum acceptable version for a protocol.
func (pn *ProtoNeg) minVersionFor(name string) uint {
	switch strings.ToLower(name) {
	case "eth":
		return pn.config.MinETHVersion
	case "snap":
		return pn.config.MinSNAPVersion
	default:
		return 1 // Accept version 1+ for unknown protocols.
	}
}

// sortProtoCaps sorts capabilities by name, then version ascending.
func sortProtoCaps(caps []ProtoCapability) {
	sort.Slice(caps, func(i, j int) bool {
		if caps[i].Name != caps[j].Name {
			return caps[i].Name < caps[j].Name
		}
		return caps[i].Version < caps[j].Version
	})
}
