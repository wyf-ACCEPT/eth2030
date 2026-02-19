package light

import (
	"sync"

	"github.com/eth2028/eth2028/core/types"
)

// LightStore provides persistent storage for light client headers.
type LightStore interface {
	StoreHeader(header *types.Header) error
	GetHeader(hash types.Hash) *types.Header
	GetLatest() *types.Header
	GetByNumber(num uint64) *types.Header
}

// MemoryLightStore is an in-memory implementation of LightStore.
type MemoryLightStore struct {
	mu        sync.RWMutex
	byHash    map[types.Hash]*types.Header
	byNumber  map[uint64]*types.Header
	latest    *types.Header
	latestNum uint64
}

// NewMemoryLightStore creates a new in-memory light store.
func NewMemoryLightStore() *MemoryLightStore {
	return &MemoryLightStore{
		byHash:   make(map[types.Hash]*types.Header),
		byNumber: make(map[uint64]*types.Header),
	}
}

// StoreHeader stores a header, updating the latest if it has a higher number.
func (s *MemoryLightStore) StoreHeader(header *types.Header) error {
	if header == nil || header.Number == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	hash := header.Hash()
	s.byHash[hash] = header
	num := header.Number.Uint64()
	s.byNumber[num] = header

	if num >= s.latestNum {
		s.latest = header
		s.latestNum = num
	}
	return nil
}

// GetHeader retrieves a header by its hash.
func (s *MemoryLightStore) GetHeader(hash types.Hash) *types.Header {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.byHash[hash]
}

// GetLatest returns the header with the highest block number.
func (s *MemoryLightStore) GetLatest() *types.Header {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.latest
}

// GetByNumber retrieves a header by block number.
func (s *MemoryLightStore) GetByNumber(num uint64) *types.Header {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.byNumber[num]
}

// Count returns the number of stored headers.
func (s *MemoryLightStore) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.byHash)
}
