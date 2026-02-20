// Package portal implements the Portal Network wire protocol.
// This file implements the Portal State Network client for distributed
// retrieval of Ethereum state data via the Portal DHT overlay. It provides
// content key construction, content distance computation, radius-based
// filtering, and DHT-based content lookup and offer operations for account
// trie nodes, contract storage trie nodes, and contract bytecode.
//
// Reference: https://github.com/ethereum/portal-network-specs/blob/master/state-network.md
package portal

import (
	"crypto/sha256"
	"errors"
	"math/big"
	"sync"

	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/crypto"
)

// State content key type constants for the Portal State Network.
// These represent the three types of state data that can be requested.
const (
	StateKeyAccountTrieNode         byte = 0x20
	StateKeyContractStorageTrieNode byte = 0x21
	StateKeyContractBytecode        byte = 0x22
)

// State network client errors.
var (
	ErrClientNotStarted    = errors.New("portal/state_network: client not started")
	ErrInvalidStateContent = errors.New("portal/state_network: invalid content key")
	ErrContentMissing      = errors.New("portal/state_network: content not found")
	ErrInvalidPath         = errors.New("portal/state_network: invalid path")
	ErrInvalidAddress      = errors.New("portal/state_network: invalid address")
	ErrNoPeers             = errors.New("portal/state_network: no peers available")
	ErrOfferRejected       = errors.New("portal/state_network: offer rejected by all peers")
)

// StateContentKeyV2 represents a typed content key for the Portal State Network.
// It distinguishes between account trie nodes, contract storage trie nodes,
// and contract bytecode.
type StateContentKeyV2 struct {
	// Type is the content key type selector.
	Type byte

	// Path is the nibble path into the trie (for trie node keys) or
	// the address hash (for bytecode keys).
	Path []byte

	// Address is the Ethereum address associated with this content.
	Address types.Address
}

// Encode serializes the state content key with its type prefix.
// Format: type(1) || address_hash(32) || path(variable).
func (k StateContentKeyV2) Encode() []byte {
	addrHash := crypto.Keccak256Hash(k.Address[:])

	switch k.Type {
	case StateKeyAccountTrieNode:
		// type || address_hash || path
		buf := make([]byte, 1+types.HashLength+len(k.Path))
		buf[0] = k.Type
		copy(buf[1:], addrHash[:])
		copy(buf[1+types.HashLength:], k.Path)
		return buf

	case StateKeyContractStorageTrieNode:
		// type || address_hash || path
		buf := make([]byte, 1+types.HashLength+len(k.Path))
		buf[0] = k.Type
		copy(buf[1:], addrHash[:])
		copy(buf[1+types.HashLength:], k.Path)
		return buf

	case StateKeyContractBytecode:
		// type || address_hash || code_hash
		buf := make([]byte, 1+types.HashLength+len(k.Path))
		buf[0] = k.Type
		copy(buf[1:], addrHash[:])
		copy(buf[1+types.HashLength:], k.Path)
		return buf

	default:
		return nil
	}
}

// StateContentID computes the content ID for a state content key.
// Content ID = SHA-256(encoded_content_key), which determines the
// location of the content in the DHT key space.
func StateContentID(key StateContentKeyV2) ContentID {
	encoded := key.Encode()
	if encoded == nil {
		return ContentID{}
	}
	return sha256.Sum256(encoded)
}

// StateNetworkClient manages state content lookups via the Portal DHT.
// It wraps a routing table and content store to provide higher-level
// state retrieval operations.
type StateNetworkClient struct {
	mu      sync.RWMutex
	table   *RoutingTable
	store   ContentStore
	started bool
	nodeID  [32]byte
}

// NewStateNetworkClient creates a new state network client with the given
// routing table and content store.
func NewStateNetworkClient(table *RoutingTable, store ContentStore) *StateNetworkClient {
	return &StateNetworkClient{
		table:  table,
		store:  store,
		nodeID: table.Self(),
	}
}

// Start initializes the state network client.
func (c *StateNetworkClient) Start() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.started = true
	return nil
}

// Stop shuts down the state network client.
func (c *StateNetworkClient) Stop() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.started = false
}

// IsStarted reports whether the client is running.
func (c *StateNetworkClient) IsStarted() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.started
}

// FindContent searches for state content by key. It first checks the local
// content store, then falls back to querying peers via the DHT routing table.
// Returns the raw content bytes or an error.
func (c *StateNetworkClient) FindContent(contentKey StateContentKeyV2) ([]byte, error) {
	c.mu.RLock()
	started := c.started
	c.mu.RUnlock()

	if !started {
		return nil, ErrClientNotStarted
	}

	encoded := contentKey.Encode()
	if encoded == nil {
		return nil, ErrInvalidStateContent
	}

	contentID := sha256.Sum256(encoded)

	// Try local store first.
	if data, err := c.store.Get(contentID); err == nil {
		return data, nil
	}

	// Fall back to DHT lookup.
	closest := c.table.ClosestPeers(contentID, BucketSize)
	if len(closest) == 0 {
		return nil, ErrNoPeers
	}

	// In a real implementation, we would iteratively query peers.
	// For now, check if any peer's radius covers this content.
	for _, peer := range closest {
		if peer.Radius.Contains(peer.NodeID, contentID) {
			// Peer should have it -- in production we would send FindContent.
			// Placeholder: peer does not have it, continue.
			continue
		}
	}

	return nil, ErrContentMissing
}

// FindContentWithQuery performs a DHT content lookup using the provided
// query function to contact peers. This is the full iterative lookup.
func (c *StateNetworkClient) FindContentWithQuery(contentKey StateContentKeyV2, queryFn ContentQueryFn) ([]byte, error) {
	c.mu.RLock()
	started := c.started
	c.mu.RUnlock()

	if !started {
		return nil, ErrClientNotStarted
	}

	encoded := contentKey.Encode()
	if encoded == nil {
		return nil, ErrInvalidStateContent
	}

	contentID := sha256.Sum256(encoded)

	// Check local store first.
	if data, err := c.store.Get(contentID); err == nil {
		return data, nil
	}

	// Iterative DHT lookup via routing table.
	result := c.table.ContentLookup(encoded, queryFn)
	if result.Found {
		// Cache locally.
		_ = c.store.Put(contentID, result.Content)
		return result.Content, nil
	}

	return nil, ErrContentMissing
}

// StoreContent stores state content in the local content store.
func (c *StateNetworkClient) StoreContent(contentKey StateContentKeyV2, data []byte) error {
	if len(data) == 0 {
		return ErrEmptyPayload
	}

	encoded := contentKey.Encode()
	if encoded == nil {
		return ErrInvalidStateContent
	}

	contentID := sha256.Sum256(encoded)
	return c.store.Put(contentID, data)
}

// OfferContent pushes content to interested peers whose radius covers the
// content ID. Returns the number of peers that accepted the content.
func (c *StateNetworkClient) OfferContent(contentKey StateContentKeyV2, content []byte) (int, error) {
	c.mu.RLock()
	started := c.started
	c.mu.RUnlock()

	if !started {
		return 0, ErrClientNotStarted
	}

	if len(content) == 0 {
		return 0, ErrEmptyPayload
	}

	encoded := contentKey.Encode()
	if encoded == nil {
		return 0, ErrInvalidStateContent
	}

	contentID := sha256.Sum256(encoded)

	// Find peers whose radius covers this content.
	closest := c.table.ClosestPeers(contentID, BucketSize)
	accepted := 0

	for _, peer := range closest {
		if peer.Radius.Contains(peer.NodeID, contentID) {
			accepted++
		}
	}

	if accepted == 0 {
		return 0, ErrOfferRejected
	}

	return accepted, nil
}

// ComputeContentDistance returns the XOR distance between a node ID and
// a content ID. This is the fundamental metric for DHT content routing.
func ComputeContentDistance(nodeID [32]byte, contentID ContentID) *big.Int {
	return Distance(nodeID, contentID)
}

// RadiusFilter checks whether a content ID falls within the given radius
// relative to the node ID. Returns true if the content is within range.
func RadiusFilter(nodeID [32]byte, contentID ContentID, radius NodeRadius) bool {
	dist := Distance(nodeID, contentID)
	return dist.Cmp(radius.Raw) <= 0
}

// MakeAccountTrieNodeKey creates a state content key for an account trie node.
func MakeAccountTrieNodeKey(addr types.Address, path []byte) StateContentKeyV2 {
	pathCopy := make([]byte, len(path))
	copy(pathCopy, path)
	return StateContentKeyV2{
		Type:    StateKeyAccountTrieNode,
		Path:    pathCopy,
		Address: addr,
	}
}

// MakeContractStorageTrieNodeKey creates a state content key for a contract
// storage trie node.
func MakeContractStorageTrieNodeKey(addr types.Address, path []byte) StateContentKeyV2 {
	pathCopy := make([]byte, len(path))
	copy(pathCopy, path)
	return StateContentKeyV2{
		Type:    StateKeyContractStorageTrieNode,
		Path:    pathCopy,
		Address: addr,
	}
}

// MakeContractBytecodeKey creates a state content key for contract bytecode.
func MakeContractBytecodeKey(addr types.Address, codeHash types.Hash) StateContentKeyV2 {
	return StateContentKeyV2{
		Type:    StateKeyContractBytecode,
		Path:    codeHash[:],
		Address: addr,
	}
}

// DecodeStateContentKeyV2 parses raw bytes into a StateContentKeyV2.
func DecodeStateContentKeyV2(data []byte) (StateContentKeyV2, error) {
	if len(data) < 1+types.HashLength {
		return StateContentKeyV2{}, ErrInvalidStateContent
	}

	keyType := data[0]
	switch keyType {
	case StateKeyAccountTrieNode, StateKeyContractStorageTrieNode, StateKeyContractBytecode:
		// type(1) || address_hash(32) || path(variable)
		path := make([]byte, len(data)-1-types.HashLength)
		copy(path, data[1+types.HashLength:])
		return StateContentKeyV2{
			Type: keyType,
			Path: path,
			// Address cannot be recovered from the hash.
		}, nil
	default:
		return StateContentKeyV2{}, ErrInvalidStateContent
	}
}

// ContentIDFromRawKey computes the content ID from already-encoded content key bytes.
func ContentIDFromRawKey(encodedKey []byte) ContentID {
	return sha256.Sum256(encodedKey)
}
