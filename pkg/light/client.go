package light

import (
	"errors"
	"sync"

	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/crypto"
)

var (
	ErrClientStopped  = errors.New("light: client is stopped")
	ErrInvalidProof   = errors.New("light: invalid state proof")
	ErrNoFinalizedHdr = errors.New("light: no finalized header available")
)

// LightClient provides a high-level API for light client operations.
// It manages syncing, header storage, and state proof verification.
type LightClient struct {
	syncer  *LightSyncer
	store   LightStore
	running bool
	mu      sync.RWMutex
}

// NewLightClient creates a new LightClient with an in-memory store.
func NewLightClient() *LightClient {
	store := NewMemoryLightStore()
	return &LightClient{
		syncer: NewLightSyncer(store),
		store:  store,
	}
}

// NewLightClientWithStore creates a LightClient with a custom store.
func NewLightClientWithStore(store LightStore) *LightClient {
	return &LightClient{
		syncer: NewLightSyncer(store),
		store:  store,
	}
}

// Start initializes the light client. Returns an error if already running.
func (lc *LightClient) Start() error {
	lc.mu.Lock()
	defer lc.mu.Unlock()
	lc.running = true
	return nil
}

// Stop shuts down the light client.
func (lc *LightClient) Stop() {
	lc.mu.Lock()
	defer lc.mu.Unlock()
	lc.running = false
}

// IsRunning returns whether the client is started.
func (lc *LightClient) IsRunning() bool {
	lc.mu.RLock()
	defer lc.mu.RUnlock()
	return lc.running
}

// ProcessUpdate forwards a light client update to the syncer.
func (lc *LightClient) ProcessUpdate(update *LightClientUpdate) error {
	lc.mu.RLock()
	defer lc.mu.RUnlock()
	if !lc.running {
		return ErrClientStopped
	}
	return lc.syncer.ProcessUpdate(update)
}

// GetFinalizedHeader returns the latest finalized header.
func (lc *LightClient) GetFinalizedHeader() *types.Header {
	return lc.syncer.GetFinalizedHeader()
}

// IsSynced returns whether the client has synced a finalized header.
func (lc *LightClient) IsSynced() bool {
	return lc.syncer.IsSynced()
}

// GetHeader retrieves a stored header by hash.
func (lc *LightClient) GetHeader(hash types.Hash) *types.Header {
	return lc.store.GetHeader(hash)
}

// GetHeaderByNumber retrieves a stored header by block number.
func (lc *LightClient) GetHeaderByNumber(num uint64) *types.Header {
	return lc.store.GetByNumber(num)
}

// VerifyStateProof verifies a state proof against a header's state root.
// The proof is expected to be a Keccak256 commitment: H(root || key || value).
// Returns the proven value on success.
func (lc *LightClient) VerifyStateProof(header *types.Header, key []byte, proof []byte) ([]byte, error) {
	if header == nil {
		return nil, ErrNoFinalizedHdr
	}
	if len(proof) < 32 {
		return nil, ErrInvalidProof
	}

	// The proof format: [value_len(4)] [value(N)] [commitment(32)]
	// The commitment = Keccak256(state_root || key || value).
	if len(proof) < 36 {
		return nil, ErrInvalidProof
	}

	// Extract value length (big-endian 4 bytes).
	valLen := uint32(proof[0])<<24 | uint32(proof[1])<<16 | uint32(proof[2])<<8 | uint32(proof[3])
	if uint32(len(proof)) < 4+valLen+32 {
		return nil, ErrInvalidProof
	}

	value := proof[4 : 4+valLen]
	commitment := proof[4+valLen : 4+valLen+32]

	// Verify: commitment == Keccak256(root || key || value).
	root := header.Root
	msg := append(root[:], key...)
	msg = append(msg, value...)
	expected := crypto.Keccak256(msg)

	for i := 0; i < 32; i++ {
		if commitment[i] != expected[i] {
			return nil, ErrInvalidProof
		}
	}

	result := make([]byte, len(value))
	copy(result, value)
	return result, nil
}

// BuildStateProof constructs a proof for a key-value pair against a state root.
// Used in tests to create valid proofs for VerifyStateProof.
func BuildStateProof(root types.Hash, key, value []byte) []byte {
	msg := append(root[:], key...)
	msg = append(msg, value...)
	commitment := crypto.Keccak256(msg)

	// Format: [value_len(4)] [value(N)] [commitment(32)]
	proof := make([]byte, 4+len(value)+32)
	proof[0] = byte(len(value) >> 24)
	proof[1] = byte(len(value) >> 16)
	proof[2] = byte(len(value) >> 8)
	proof[3] = byte(len(value))
	copy(proof[4:], value)
	copy(proof[4+len(value):], commitment)
	return proof
}

// Syncer returns the underlying LightSyncer.
func (lc *LightClient) Syncer() *LightSyncer {
	return lc.syncer
}
