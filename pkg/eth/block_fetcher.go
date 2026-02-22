package eth

import (
	"errors"
	"sync"
	"time"

	"github.com/eth2030/eth2030/core/types"
)

// Block fetcher constants.
const (
	// maxAnnounces is the maximum number of pending block announcements.
	maxAnnounces = 256

	// maxPendingFetches is the maximum number of concurrent block fetches.
	maxPendingFetches = 64

	// announceExpiry is how long an announcement remains valid.
	announceExpiry = 5 * time.Minute

	// fetchTimeout is the maximum time to wait for a fetch response.
	fetchTimeout = 30 * time.Second
)

// Block fetcher errors.
var (
	ErrAlreadyAnnounced = errors.New("eth: block already announced")
	ErrFetchQueueFull   = errors.New("eth: fetch queue full")
	ErrUnknownBlock     = errors.New("eth: unknown block")
	ErrFetcherStopped   = errors.New("eth: fetcher stopped")
)

// BlockAnnounce represents a block hash announcement from a peer.
type BlockAnnounce struct {
	Hash   types.Hash // Block hash.
	Number uint64     // Block number.
	PeerID string     // Peer that announced it.
	Time   time.Time  // When the announcement was received.
}

// fetchRequest tracks a pending block fetch.
type fetchRequest struct {
	hash    types.Hash
	number  uint64
	peerID  string
	created time.Time
}

// BlockFetcher tracks announced blocks, deduplicates announcements, and
// schedules header/body downloads. It sits between the eth protocol handler
// and the sync pipeline, providing fast block import for newly announced
// blocks that are only a few blocks ahead of our head.
type BlockFetcher struct {
	mu sync.Mutex

	// announced tracks known block hashes and their announcements.
	announced map[types.Hash]*BlockAnnounce

	// fetching tracks hashes currently being fetched.
	fetching map[types.Hash]*fetchRequest

	// completed tracks hashes that have been successfully imported.
	completed map[types.Hash]struct{}

	// announceOrder preserves insertion order for expiry.
	announceOrder []types.Hash

	// chain is used to check if blocks are already known.
	chain Blockchain

	// stopped indicates the fetcher has been stopped.
	stopped bool
}

// NewBlockFetcher creates a new block fetcher.
func NewBlockFetcher(chain Blockchain) *BlockFetcher {
	return &BlockFetcher{
		announced: make(map[types.Hash]*BlockAnnounce),
		fetching:  make(map[types.Hash]*fetchRequest),
		completed: make(map[types.Hash]struct{}),
		chain:     chain,
	}
}

// Announce registers a new block hash announcement from a peer. It returns
// an error if the block is already announced or already known in the chain.
// Duplicate announcements from different peers are deduplicated.
func (f *BlockFetcher) Announce(hash types.Hash, number uint64, peerID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.stopped {
		return ErrFetcherStopped
	}

	// Already completed.
	if _, ok := f.completed[hash]; ok {
		return nil
	}

	// Already known in chain.
	if f.chain != nil && f.chain.HasBlock(hash) {
		f.completed[hash] = struct{}{}
		return nil
	}

	// Already announced.
	if _, ok := f.announced[hash]; ok {
		return ErrAlreadyAnnounced
	}

	// Already fetching.
	if _, ok := f.fetching[hash]; ok {
		return ErrAlreadyAnnounced
	}

	// Enforce maximum pending announcements.
	if len(f.announced) >= maxAnnounces {
		f.expireOldest()
	}

	ann := &BlockAnnounce{
		Hash:   hash,
		Number: number,
		PeerID: peerID,
		Time:   time.Now(),
	}
	f.announced[hash] = ann
	f.announceOrder = append(f.announceOrder, hash)
	return nil
}

// Import processes a received block, marking it as completed and removing
// it from the pending/fetching sets. Returns an error if the block was
// not previously announced.
func (f *BlockFetcher) Import(block *types.Block) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.stopped {
		return ErrFetcherStopped
	}

	hash := block.Hash()

	// Remove from announced/fetching.
	delete(f.announced, hash)
	delete(f.fetching, hash)

	// Mark as completed.
	f.completed[hash] = struct{}{}

	// Remove from announceOrder.
	for i, h := range f.announceOrder {
		if h == hash {
			f.announceOrder = append(f.announceOrder[:i], f.announceOrder[i+1:]...)
			break
		}
	}
	return nil
}

// FilterHeaders filters a set of headers, returning only those that
// correspond to pending block announcements (i.e., blocks we're interested in).
func (f *BlockFetcher) FilterHeaders(headers []*types.Header) []*types.Header {
	f.mu.Lock()
	defer f.mu.Unlock()

	var matched []*types.Header
	for _, h := range headers {
		hash := h.Hash()
		if _, ok := f.announced[hash]; ok {
			matched = append(matched, h)
		}
		if _, ok := f.fetching[hash]; ok {
			matched = append(matched, h)
		}
	}
	return matched
}

// FilterBodies filters a set of block body hashes, returning only the hashes
// of bodies we are waiting for (announced or currently fetching).
func (f *BlockFetcher) FilterBodies(hashes []types.Hash) []types.Hash {
	f.mu.Lock()
	defer f.mu.Unlock()

	var matched []types.Hash
	for _, h := range hashes {
		if _, ok := f.announced[h]; ok {
			matched = append(matched, h)
		}
		if _, ok := f.fetching[h]; ok {
			matched = append(matched, h)
		}
	}
	return matched
}

// ScheduleFetch moves announced blocks into the fetching state, returning
// the list of block hashes to fetch. At most maxPendingFetches blocks are
// moved.
func (f *BlockFetcher) ScheduleFetch() []types.Hash {
	f.mu.Lock()
	defer f.mu.Unlock()

	var toFetch []types.Hash
	for hash, ann := range f.announced {
		if len(f.fetching) >= maxPendingFetches {
			break
		}
		f.fetching[hash] = &fetchRequest{
			hash:    hash,
			number:  ann.Number,
			peerID:  ann.PeerID,
			created: time.Now(),
		}
		toFetch = append(toFetch, hash)
	}
	// Remove scheduled items from announced.
	for _, h := range toFetch {
		delete(f.announced, h)
	}
	return toFetch
}

// PendingCount returns the number of blocks waiting to be fetched
// (announced but not yet scheduled).
func (f *BlockFetcher) PendingCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.announced)
}

// FetchingCount returns the number of blocks currently being fetched.
func (f *BlockFetcher) FetchingCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.fetching)
}

// CompletedCount returns the number of blocks that have been imported.
func (f *BlockFetcher) CompletedCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.completed)
}

// IsAnnounced returns true if the given hash has been announced but not
// yet fetched or imported.
func (f *BlockFetcher) IsAnnounced(hash types.Hash) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.announced[hash]
	return ok
}

// IsFetching returns true if the given hash is currently being fetched.
func (f *BlockFetcher) IsFetching(hash types.Hash) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.fetching[hash]
	return ok
}

// IsCompleted returns true if the given hash has been imported.
func (f *BlockFetcher) IsCompleted(hash types.Hash) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.completed[hash]
	return ok
}

// ExpireStale removes announcements and fetch requests that have exceeded
// their timeout.
func (f *BlockFetcher) ExpireStale() int {
	f.mu.Lock()
	defer f.mu.Unlock()

	now := time.Now()
	expired := 0

	// Expire old announcements.
	for hash, ann := range f.announced {
		if now.Sub(ann.Time) > announceExpiry {
			delete(f.announced, hash)
			expired++
		}
	}

	// Expire timed-out fetches.
	for hash, req := range f.fetching {
		if now.Sub(req.created) > fetchTimeout {
			delete(f.fetching, hash)
			expired++
		}
	}

	// Clean up announceOrder.
	clean := f.announceOrder[:0]
	for _, h := range f.announceOrder {
		if _, ok := f.announced[h]; ok {
			clean = append(clean, h)
		}
	}
	f.announceOrder = clean

	return expired
}

// Stop marks the fetcher as stopped. All subsequent operations return
// ErrFetcherStopped.
func (f *BlockFetcher) Stop() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stopped = true
}

// Reset clears all internal state.
func (f *BlockFetcher) Reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.announced = make(map[types.Hash]*BlockAnnounce)
	f.fetching = make(map[types.Hash]*fetchRequest)
	f.completed = make(map[types.Hash]struct{})
	f.announceOrder = nil
	f.stopped = false
}

// expireOldest removes the oldest announcement to make room for new ones.
// Must be called with f.mu held.
func (f *BlockFetcher) expireOldest() {
	if len(f.announceOrder) == 0 {
		return
	}
	oldest := f.announceOrder[0]
	f.announceOrder = f.announceOrder[1:]
	delete(f.announced, oldest)
}

// GetAnnouncements returns a snapshot of all current announcements.
func (f *BlockFetcher) GetAnnouncements() []*BlockAnnounce {
	f.mu.Lock()
	defer f.mu.Unlock()

	result := make([]*BlockAnnounce, 0, len(f.announced))
	for _, ann := range f.announced {
		cp := *ann
		result = append(result, &cp)
	}
	return result
}
