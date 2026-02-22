// Package portal implements the Portal Network wire protocol.
// This file implements the Portal State Network sub-protocol for distributed
// access to Ethereum state data (account proofs, storage proofs, contract code).
//
// Reference: https://github.com/ethereum/portal-network-specs/blob/master/state-network.md
package portal

import (
	"encoding/binary"
	"errors"
	"math/big"
	"sync"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

// State content key type selectors (separate namespace from history keys).
const (
	ContentTypeAccountTrieNode         byte = 0x20
	ContentTypeContractStorageTrieNode byte = 0x21
	ContentTypeContractBytecode        byte = 0x22
	ContentTypeAccountProof            byte = 0x23
)

// State network errors.
var (
	ErrStateNotStarted      = errors.New("portal/state: network not started")
	ErrStateContentTooLarge = errors.New("portal/state: content exceeds max size")
	ErrStateContentNotFound = errors.New("portal/state: content not found")
	ErrInvalidStateKey      = errors.New("portal/state: invalid content key")
	ErrEmptyStatePayload    = errors.New("portal/state: empty payload")
)

// EvictionPolicy controls how content is evicted when storage is full.
type EvictionPolicy int

const (
	// EvictLRU evicts least recently used content first.
	EvictLRU EvictionPolicy = iota
	// EvictFarthest evicts content farthest from the local node ID.
	EvictFarthest
)

// StateNetworkConfig configures the state network.
type StateNetworkConfig struct {
	// MaxContentSize is the maximum size of a single content item in bytes.
	MaxContentSize uint64

	// ContentRadius is the initial content radius for this node.
	ContentRadius NodeRadius

	// EvictionPolicy controls how content is evicted when storage is full.
	EvictionPolicy EvictionPolicy
}

// DefaultStateNetworkConfig returns a default state network configuration.
func DefaultStateNetworkConfig() StateNetworkConfig {
	return StateNetworkConfig{
		MaxContentSize: 1 << 20, // 1 MiB
		ContentRadius:  MaxRadius(),
		EvictionPolicy: EvictFarthest,
	}
}

// StateContentKey encodes a state content key with its type selector and payload.
type StateContentKey struct {
	// ContentType is the type selector byte.
	ContentType byte

	// AddressHash is the keccak256(address) for address-scoped content.
	AddressHash types.Hash

	// Slot is used only for storage proofs (keccak256(slot)).
	Slot types.Hash

	// CodeHash is used only for contract bytecode lookups.
	CodeHash types.Hash
}

// Encode serializes the state content key with its type prefix.
func (k StateContentKey) Encode() []byte {
	switch k.ContentType {
	case ContentTypeAccountTrieNode, ContentTypeAccountProof:
		// type || addressHash
		buf := make([]byte, 1+types.HashLength)
		buf[0] = k.ContentType
		copy(buf[1:], k.AddressHash[:])
		return buf

	case ContentTypeContractStorageTrieNode:
		// type || addressHash || slot
		buf := make([]byte, 1+2*types.HashLength)
		buf[0] = k.ContentType
		copy(buf[1:], k.AddressHash[:])
		copy(buf[1+types.HashLength:], k.Slot[:])
		return buf

	case ContentTypeContractBytecode:
		// type || addressHash || codeHash
		buf := make([]byte, 1+2*types.HashLength)
		buf[0] = k.ContentType
		copy(buf[1:], k.AddressHash[:])
		copy(buf[1+types.HashLength:], k.CodeHash[:])
		return buf

	default:
		return nil
	}
}

// DecodeStateContentKey parses raw bytes into a StateContentKey.
func DecodeStateContentKey(data []byte) (StateContentKey, error) {
	if len(data) < 1 {
		return StateContentKey{}, ErrInvalidStateKey
	}

	keyType := data[0]
	switch keyType {
	case ContentTypeAccountTrieNode, ContentTypeAccountProof:
		if len(data) < 1+types.HashLength {
			return StateContentKey{}, ErrInvalidStateKey
		}
		var k StateContentKey
		k.ContentType = keyType
		copy(k.AddressHash[:], data[1:1+types.HashLength])
		return k, nil

	case ContentTypeContractStorageTrieNode:
		if len(data) < 1+2*types.HashLength {
			return StateContentKey{}, ErrInvalidStateKey
		}
		var k StateContentKey
		k.ContentType = keyType
		copy(k.AddressHash[:], data[1:1+types.HashLength])
		copy(k.Slot[:], data[1+types.HashLength:1+2*types.HashLength])
		return k, nil

	case ContentTypeContractBytecode:
		if len(data) < 1+2*types.HashLength {
			return StateContentKey{}, ErrInvalidStateKey
		}
		var k StateContentKey
		k.ContentType = keyType
		copy(k.AddressHash[:], data[1:1+types.HashLength])
		copy(k.CodeHash[:], data[1+types.HashLength:1+2*types.HashLength])
		return k, nil

	default:
		return StateContentKey{}, ErrInvalidStateKey
	}
}

// AccountProofResult holds a Merkle proof for an account in the state trie.
type AccountProofResult struct {
	// Address is the account address.
	Address types.Address

	// Balance is the account balance.
	Balance *big.Int

	// Nonce is the account nonce.
	Nonce uint64

	// CodeHash is the hash of the account's code.
	CodeHash types.Hash

	// StorageHash is the root of the account's storage trie.
	StorageHash types.Hash

	// Proof contains the Merkle-Patricia trie proof nodes (RLP-encoded).
	Proof [][]byte
}

// StorageProofResult holds a Merkle proof for a storage slot.
type StorageProofResult struct {
	// Key is the storage slot key.
	Key types.Hash

	// Value is the storage slot value.
	Value types.Hash

	// Proof contains the Merkle-Patricia trie proof nodes (RLP-encoded).
	Proof [][]byte
}

// StateNetwork manages state data distribution via the portal state
// sub-protocol. It provides content-addressed storage and retrieval for
// account proofs, storage proofs, and contract bytecode.
type StateNetwork struct {
	mu      sync.RWMutex
	config  StateNetworkConfig
	table   *RoutingTable
	store   ContentStore
	started bool
}

// NewStateNetwork creates a new state network handler.
func NewStateNetwork(config StateNetworkConfig, table *RoutingTable, store ContentStore) *StateNetwork {
	return &StateNetwork{
		config: config,
		table:  table,
		store:  store,
	}
}

// Start initializes the state network.
func (sn *StateNetwork) Start() error {
	sn.mu.Lock()
	defer sn.mu.Unlock()
	sn.started = true
	return nil
}

// Stop shuts down the state network.
func (sn *StateNetwork) Stop() {
	sn.mu.Lock()
	defer sn.mu.Unlock()
	sn.started = false
}

// isStarted returns the current started state (caller must NOT hold mu).
func (sn *StateNetwork) isStarted() bool {
	sn.mu.RLock()
	defer sn.mu.RUnlock()
	return sn.started
}

// MakeAccountProofKey builds a state content key for an account proof.
func MakeAccountProofKey(addr types.Address) StateContentKey {
	addrHash := crypto.Keccak256Hash(addr[:])
	return StateContentKey{
		ContentType: ContentTypeAccountProof,
		AddressHash: addrHash,
	}
}

// MakeStorageProofKey builds a state content key for a storage proof.
func MakeStorageProofKey(addr types.Address, slot types.Hash) StateContentKey {
	addrHash := crypto.Keccak256Hash(addr[:])
	slotHash := crypto.Keccak256Hash(slot[:])
	return StateContentKey{
		ContentType: ContentTypeContractStorageTrieNode,
		AddressHash: addrHash,
		Slot:        slotHash,
	}
}

// MakeContractCodeKey builds a state content key for contract bytecode.
func MakeContractCodeKey(addr types.Address, codeHash types.Hash) StateContentKey {
	addrHash := crypto.Keccak256Hash(addr[:])
	return StateContentKey{
		ContentType: ContentTypeContractBytecode,
		AddressHash: addrHash,
		CodeHash:    codeHash,
	}
}

// GetAccountProof retrieves the account proof for the given address from
// the local store or DHT.
func (sn *StateNetwork) GetAccountProof(addr types.Address) (*AccountProofResult, error) {
	if !sn.isStarted() {
		return nil, ErrStateNotStarted
	}

	key := MakeAccountProofKey(addr)
	data, err := sn.FindContent(key)
	if err != nil {
		return nil, err
	}

	return decodeAccountProof(addr, data)
}

// GetStorageProof retrieves a storage proof for the given address and slot.
func (sn *StateNetwork) GetStorageProof(addr types.Address, slot types.Hash) (*StorageProofResult, error) {
	if !sn.isStarted() {
		return nil, ErrStateNotStarted
	}

	key := MakeStorageProofKey(addr, slot)
	data, err := sn.FindContent(key)
	if err != nil {
		return nil, err
	}

	return decodeStorageProof(slot, data)
}

// GetContractCode retrieves contract bytecode by code hash.
func (sn *StateNetwork) GetContractCode(codeHash types.Hash) ([]byte, error) {
	if !sn.isStarted() {
		return nil, ErrStateNotStarted
	}

	// For code lookup we need an address; use a zero-address key with the
	// code hash. In practice the caller knows the address.
	key := StateContentKey{
		ContentType: ContentTypeContractBytecode,
		AddressHash: types.Hash{}, // wildcard
		CodeHash:    codeHash,
	}
	return sn.FindContent(key)
}

// StoreContent stores state content keyed by its content key.
func (sn *StateNetwork) StoreContent(key StateContentKey, data []byte) error {
	if len(data) == 0 {
		return ErrEmptyStatePayload
	}

	sn.mu.RLock()
	maxSize := sn.config.MaxContentSize
	sn.mu.RUnlock()

	if uint64(len(data)) > maxSize {
		return ErrStateContentTooLarge
	}

	encoded := key.Encode()
	if encoded == nil {
		return ErrInvalidStateKey
	}

	contentID := ComputeContentID(encoded)
	return sn.store.Put(contentID, data)
}

// FindContent retrieves content from the local store by state content key.
func (sn *StateNetwork) FindContent(key StateContentKey) ([]byte, error) {
	encoded := key.Encode()
	if encoded == nil {
		return nil, ErrInvalidStateKey
	}

	contentID := ComputeContentID(encoded)
	data, err := sn.store.Get(contentID)
	if err != nil {
		return nil, ErrStateContentNotFound
	}
	return data, nil
}

// decodeAccountProof decodes serialized account proof data.
// Format: nonce[8] || balance[32] || storageHash[32] || codeHash[32] ||
// proofCount[4] || (proofLen[4] || proofNode)...
func decodeAccountProof(addr types.Address, data []byte) (*AccountProofResult, error) {
	if len(data) < 8+32+32+32+4 {
		return nil, ErrInvalidStateKey
	}

	offset := 0
	nonce := binary.BigEndian.Uint64(data[offset : offset+8])
	offset += 8

	balance := new(big.Int).SetBytes(data[offset : offset+32])
	offset += 32

	var storageHash types.Hash
	copy(storageHash[:], data[offset:offset+32])
	offset += 32

	var codeHash types.Hash
	copy(codeHash[:], data[offset:offset+32])
	offset += 32

	if offset+4 > len(data) {
		return nil, ErrInvalidStateKey
	}
	proofCount := binary.BigEndian.Uint32(data[offset : offset+4])
	offset += 4

	proofNodes := make([][]byte, 0, proofCount)
	for i := uint32(0); i < proofCount; i++ {
		if offset+4 > len(data) {
			return nil, ErrInvalidStateKey
		}
		nodeLen := binary.BigEndian.Uint32(data[offset : offset+4])
		offset += 4
		if offset+int(nodeLen) > len(data) {
			return nil, ErrInvalidStateKey
		}
		node := make([]byte, nodeLen)
		copy(node, data[offset:offset+int(nodeLen)])
		offset += int(nodeLen)
		proofNodes = append(proofNodes, node)
	}

	return &AccountProofResult{
		Address:     addr,
		Balance:     balance,
		Nonce:       nonce,
		CodeHash:    codeHash,
		StorageHash: storageHash,
		Proof:       proofNodes,
	}, nil
}

// EncodeAccountProof serializes an AccountProofResult for storage.
func EncodeAccountProof(result *AccountProofResult) []byte {
	size := 8 + 32 + 32 + 32 + 4
	for _, node := range result.Proof {
		size += 4 + len(node)
	}

	buf := make([]byte, size)
	offset := 0

	binary.BigEndian.PutUint64(buf[offset:], result.Nonce)
	offset += 8

	balBytes := result.Balance.Bytes()
	copy(buf[offset+32-len(balBytes):offset+32], balBytes)
	offset += 32

	copy(buf[offset:], result.StorageHash[:])
	offset += 32

	copy(buf[offset:], result.CodeHash[:])
	offset += 32

	binary.BigEndian.PutUint32(buf[offset:], uint32(len(result.Proof)))
	offset += 4

	for _, node := range result.Proof {
		binary.BigEndian.PutUint32(buf[offset:], uint32(len(node)))
		offset += 4
		copy(buf[offset:], node)
		offset += len(node)
	}

	return buf
}

// decodeStorageProof decodes serialized storage proof data.
// Format: value[32] || proofCount[4] || (proofLen[4] || proofNode)...
func decodeStorageProof(slot types.Hash, data []byte) (*StorageProofResult, error) {
	if len(data) < 32+4 {
		return nil, ErrInvalidStateKey
	}

	offset := 0
	var value types.Hash
	copy(value[:], data[offset:offset+32])
	offset += 32

	proofCount := binary.BigEndian.Uint32(data[offset : offset+4])
	offset += 4

	proofNodes := make([][]byte, 0, proofCount)
	for i := uint32(0); i < proofCount; i++ {
		if offset+4 > len(data) {
			return nil, ErrInvalidStateKey
		}
		nodeLen := binary.BigEndian.Uint32(data[offset : offset+4])
		offset += 4
		if offset+int(nodeLen) > len(data) {
			return nil, ErrInvalidStateKey
		}
		node := make([]byte, nodeLen)
		copy(node, data[offset:offset+int(nodeLen)])
		offset += int(nodeLen)
		proofNodes = append(proofNodes, node)
	}

	return &StorageProofResult{
		Key:   slot,
		Value: value,
		Proof: proofNodes,
	}, nil
}

// EncodeStorageProof serializes a StorageProofResult for storage.
func EncodeStorageProof(result *StorageProofResult) []byte {
	size := 32 + 4
	for _, node := range result.Proof {
		size += 4 + len(node)
	}

	buf := make([]byte, size)
	offset := 0

	copy(buf[offset:], result.Value[:])
	offset += 32

	binary.BigEndian.PutUint32(buf[offset:], uint32(len(result.Proof)))
	offset += 4

	for _, node := range result.Proof {
		binary.BigEndian.PutUint32(buf[offset:], uint32(len(node)))
		offset += 4
		copy(buf[offset:], node)
		offset += len(node)
	}

	return buf
}
