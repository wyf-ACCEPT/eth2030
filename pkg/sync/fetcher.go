package sync

import (
	"errors"
	"sync"
)

// PeerID is a unique identifier for a peer.
type PeerID string

// FetchRequest represents a request for headers or bodies.
type FetchRequest struct {
	PeerID PeerID
	From   uint64 // starting block number
	Count  int    // number of items requested
}

// FetchResponse represents a response to a fetch request.
type FetchResponse struct {
	PeerID  PeerID
	Headers []HeaderData
	Bodies  []BlockData
	Err     error
}

// HeaderFetcher manages header download requests across peers.
type HeaderFetcher struct {
	mu       sync.Mutex
	pending  map[PeerID]*FetchRequest // active requests
	results  chan FetchResponse       // completed results
	maxBatch int
}

// NewHeaderFetcher creates a new header fetcher.
func NewHeaderFetcher(batchSize int) *HeaderFetcher {
	return &HeaderFetcher{
		pending:  make(map[PeerID]*FetchRequest),
		results:  make(chan FetchResponse, 64),
		maxBatch: batchSize,
	}
}

// Request creates a new fetch request for a peer.
func (f *HeaderFetcher) Request(peer PeerID, from uint64, count int) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if _, exists := f.pending[peer]; exists {
		return errors.New("peer already has pending request")
	}

	if count > f.maxBatch {
		count = f.maxBatch
	}

	f.pending[peer] = &FetchRequest{
		PeerID: peer,
		From:   from,
		Count:  count,
	}
	return nil
}

// Deliver handles a response from a peer.
func (f *HeaderFetcher) Deliver(peer PeerID, headers []HeaderData) error {
	f.mu.Lock()
	_, exists := f.pending[peer]
	if exists {
		delete(f.pending, peer)
	}
	f.mu.Unlock()

	if !exists {
		return errors.New("no pending request for peer")
	}

	f.results <- FetchResponse{
		PeerID:  peer,
		Headers: headers,
	}
	return nil
}

// DeliverError reports a failed fetch.
func (f *HeaderFetcher) DeliverError(peer PeerID, err error) {
	f.mu.Lock()
	delete(f.pending, peer)
	f.mu.Unlock()

	f.results <- FetchResponse{
		PeerID: peer,
		Err:    err,
	}
}

// Results returns the channel for completed fetch responses.
func (f *HeaderFetcher) Results() <-chan FetchResponse {
	return f.results
}

// PendingCount returns the number of active requests.
func (f *HeaderFetcher) PendingCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.pending)
}

// HasPending checks if a peer has a pending request.
func (f *HeaderFetcher) HasPending(peer PeerID) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, exists := f.pending[peer]
	return exists
}

// BodyFetcher manages block body download requests.
type BodyFetcher struct {
	mu       sync.Mutex
	pending  map[PeerID]*FetchRequest
	results  chan FetchResponse
	maxBatch int
}

// NewBodyFetcher creates a new body fetcher.
func NewBodyFetcher(batchSize int) *BodyFetcher {
	return &BodyFetcher{
		pending:  make(map[PeerID]*FetchRequest),
		results:  make(chan FetchResponse, 64),
		maxBatch: batchSize,
	}
}

// Request creates a body fetch request for a peer.
func (f *BodyFetcher) Request(peer PeerID, from uint64, count int) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if _, exists := f.pending[peer]; exists {
		return errors.New("peer already has pending request")
	}

	if count > f.maxBatch {
		count = f.maxBatch
	}

	f.pending[peer] = &FetchRequest{
		PeerID: peer,
		From:   from,
		Count:  count,
	}
	return nil
}

// Deliver handles a body response from a peer.
func (f *BodyFetcher) Deliver(peer PeerID, bodies []BlockData) error {
	f.mu.Lock()
	_, exists := f.pending[peer]
	if exists {
		delete(f.pending, peer)
	}
	f.mu.Unlock()

	if !exists {
		return errors.New("no pending request for peer")
	}

	f.results <- FetchResponse{
		PeerID: peer,
		Bodies: bodies,
	}
	return nil
}

// Results returns the channel for completed body responses.
func (f *BodyFetcher) Results() <-chan FetchResponse {
	return f.results
}

// PendingCount returns the number of active body requests.
func (f *BodyFetcher) PendingCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.pending)
}
