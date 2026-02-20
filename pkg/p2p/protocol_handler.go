// protocol_handler.go implements a versioned protocol message handling framework
// with per-code handler registration, multi-version support, and capability matching.
package p2p

import (
	"errors"
	"fmt"
	"sort"
	"sync"
)

// Errors returned by ProtoDispatcher operations.
var (
	ErrProtoDispatcherClosed     = errors.New("p2p: protocol dispatcher closed")
	ErrProtoHandlerExists        = errors.New("p2p: handler already registered for code+version")
	ErrProtoNoVersionHandler     = errors.New("p2p: no handler for message code at version")
	ErrProtoVersionNotRegistered = errors.New("p2p: protocol version not registered")
	ErrProtoPeerIncompatible     = errors.New("p2p: peer has no compatible protocol version")
)

// ProtoMsgHandler handles a single protocol message from a peer. The peerID
// identifies the sender, code is the protocol-relative message code, and
// payload contains the raw message data. Returning an error indicates the
// peer misbehaved and should be penalized.
type ProtoMsgHandler func(peerID string, code uint64, payload []byte) error

// ProtoVersionSpec describes a protocol version with its message codes.
type ProtoVersionSpec struct {
	Name       string // Protocol name (e.g., "eth").
	Version    uint   // Protocol version number.
	MaxMsgCode uint64 // Highest message code used by this version.
}

// handlerKey uniquely identifies a handler by protocol version + message code.
type handlerKey struct {
	version uint
	code    uint64
}

// ProtoDispatcher manages versioned protocol message handling. It supports
// registering handlers per (version, message-code) pair, routing messages
// to the correct handler, and matching capabilities between local and remote
// peers. All methods are safe for concurrent use.
type ProtoDispatcher struct {
	mu       sync.RWMutex
	name     string
	versions map[uint]*ProtoVersionSpec // Registered versions.
	handlers map[handlerKey]ProtoMsgHandler
	closed   bool
}

// NewProtoDispatcher creates a dispatcher for the named protocol (e.g., "eth").
func NewProtoDispatcher(name string) *ProtoDispatcher {
	return &ProtoDispatcher{
		name:     name,
		versions: make(map[uint]*ProtoVersionSpec),
		handlers: make(map[handlerKey]ProtoMsgHandler),
	}
}

// RegisterVersion registers a protocol version specification. This must be
// called before registering handlers for that version.
func (pd *ProtoDispatcher) RegisterVersion(spec ProtoVersionSpec) error {
	pd.mu.Lock()
	defer pd.mu.Unlock()

	if pd.closed {
		return ErrProtoDispatcherClosed
	}
	if _, exists := pd.versions[spec.Version]; exists {
		return fmt.Errorf("p2p: version %d already registered for %s", spec.Version, pd.name)
	}
	cp := spec
	pd.versions[spec.Version] = &cp
	return nil
}

// RegisterHandler registers a handler for a specific (version, code) pair.
// The version must have been registered via RegisterVersion first.
func (pd *ProtoDispatcher) RegisterHandler(version uint, code uint64, handler ProtoMsgHandler) error {
	pd.mu.Lock()
	defer pd.mu.Unlock()

	if pd.closed {
		return ErrProtoDispatcherClosed
	}
	spec, ok := pd.versions[version]
	if !ok {
		return ErrProtoVersionNotRegistered
	}
	if code > spec.MaxMsgCode {
		return fmt.Errorf("p2p: code %d exceeds max %d for %s/%d", code, spec.MaxMsgCode, pd.name, version)
	}
	key := handlerKey{version: version, code: code}
	if _, exists := pd.handlers[key]; exists {
		return ErrProtoHandlerExists
	}
	pd.handlers[key] = handler
	return nil
}

// SetHandler sets (or replaces) the handler for a (version, code) pair.
// Unlike RegisterHandler, it does not error on existing registrations.
func (pd *ProtoDispatcher) SetHandler(version uint, code uint64, handler ProtoMsgHandler) error {
	pd.mu.Lock()
	defer pd.mu.Unlock()

	if pd.closed {
		return ErrProtoDispatcherClosed
	}
	if _, ok := pd.versions[version]; !ok {
		return ErrProtoVersionNotRegistered
	}
	key := handlerKey{version: version, code: code}
	if handler == nil {
		delete(pd.handlers, key)
	} else {
		pd.handlers[key] = handler
	}
	return nil
}

// Route dispatches a message to the handler registered for the given version
// and code. Returns an error if no handler is found.
func (pd *ProtoDispatcher) Route(peerID string, version uint, code uint64, payload []byte) error {
	pd.mu.RLock()
	if pd.closed {
		pd.mu.RUnlock()
		return ErrProtoDispatcherClosed
	}

	key := handlerKey{version: version, code: code}
	handler, ok := pd.handlers[key]
	pd.mu.RUnlock()

	if !ok {
		return fmt.Errorf("%w: %s/%d code=0x%02x", ErrProtoNoVersionHandler, pd.name, version, code)
	}
	return handler(peerID, code, payload)
}

// RouteWithFallback tries to dispatch to the exact version handler, then falls
// back to the highest registered version that has a handler for the code.
func (pd *ProtoDispatcher) RouteWithFallback(peerID string, version uint, code uint64, payload []byte) error {
	pd.mu.RLock()
	if pd.closed {
		pd.mu.RUnlock()
		return ErrProtoDispatcherClosed
	}

	// Try exact match first.
	key := handlerKey{version: version, code: code}
	if handler, ok := pd.handlers[key]; ok {
		pd.mu.RUnlock()
		return handler(peerID, code, payload)
	}

	// Fallback: find highest version with a handler for this code.
	var bestHandler ProtoMsgHandler
	var bestVersion uint
	for k, h := range pd.handlers {
		if k.code == code && k.version > bestVersion {
			bestHandler = h
			bestVersion = k.version
		}
	}
	pd.mu.RUnlock()

	if bestHandler == nil {
		return fmt.Errorf("%w: %s code=0x%02x (no fallback)", ErrProtoNoVersionHandler, pd.name, code)
	}
	return bestHandler(peerID, code, payload)
}

// SupportedVersions returns all registered version numbers, sorted ascending.
func (pd *ProtoDispatcher) SupportedVersions() []uint {
	pd.mu.RLock()
	defer pd.mu.RUnlock()

	versions := make([]uint, 0, len(pd.versions))
	for v := range pd.versions {
		versions = append(versions, v)
	}
	sort.Slice(versions, func(i, j int) bool {
		return versions[i] < versions[j]
	})
	return versions
}

// HighestVersion returns the highest registered protocol version.
// Returns 0 if no versions are registered.
func (pd *ProtoDispatcher) HighestVersion() uint {
	pd.mu.RLock()
	defer pd.mu.RUnlock()

	var highest uint
	for v := range pd.versions {
		if v > highest {
			highest = v
		}
	}
	return highest
}

// Capabilities returns Capability entries for all registered versions.
func (pd *ProtoDispatcher) Capabilities() []Capability {
	pd.mu.RLock()
	defer pd.mu.RUnlock()

	caps := make([]Capability, 0, len(pd.versions))
	for v := range pd.versions {
		caps = append(caps, Capability{Name: pd.name, Version: v})
	}
	sort.Slice(caps, func(i, j int) bool {
		return caps[i].Version < caps[j].Version
	})
	return caps
}

// NegotiateVersion finds the best (highest) shared version between our
// registered versions and the remote peer's capabilities for our protocol.
// Returns the negotiated version or ErrProtoPeerIncompatible if no match.
func (pd *ProtoDispatcher) NegotiateVersion(remoteCaps []Capability) (uint, error) {
	pd.mu.RLock()
	defer pd.mu.RUnlock()

	var bestVersion uint
	found := false

	for _, cap := range remoteCaps {
		if cap.Name != pd.name {
			continue
		}
		if _, ok := pd.versions[cap.Version]; ok {
			if cap.Version > bestVersion {
				bestVersion = cap.Version
				found = true
			}
		}
	}

	if !found {
		return 0, ErrProtoPeerIncompatible
	}
	return bestVersion, nil
}

// HasHandler returns true if a handler is registered for the given version+code.
func (pd *ProtoDispatcher) HasHandler(version uint, code uint64) bool {
	pd.mu.RLock()
	defer pd.mu.RUnlock()

	key := handlerKey{version: version, code: code}
	_, ok := pd.handlers[key]
	return ok
}

// HandlerCount returns the total number of registered handlers across all versions.
func (pd *ProtoDispatcher) HandlerCount() int {
	pd.mu.RLock()
	defer pd.mu.RUnlock()
	return len(pd.handlers)
}

// Name returns the protocol name this dispatcher manages.
func (pd *ProtoDispatcher) Name() string {
	return pd.name
}

// Close marks the dispatcher as closed. Subsequent Route calls return errors.
func (pd *ProtoDispatcher) Close() {
	pd.mu.Lock()
	defer pd.mu.Unlock()
	pd.closed = true
}
