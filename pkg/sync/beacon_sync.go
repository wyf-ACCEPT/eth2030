// beacon_sync.go implements Beacon & Blob Sync Revamp for the eth2028 client.
// It provides a BeaconSyncer for syncing beacon chain data (blocks and blob
// sidecars) and a BlobRecovery mechanism for recovering missing blobs from
// partial availability using data availability sampling concepts.
package sync

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/eth2028/eth2028/crypto"
)

// Beacon sync errors.
var (
	ErrBeaconAlreadySyncing   = errors.New("beacon sync: already syncing")
	ErrBeaconInvalidSlotRange = errors.New("beacon sync: invalid slot range")
	ErrBeaconSlotTimeout      = errors.New("beacon sync: slot request timed out")
	ErrBeaconBlockNil         = errors.New("beacon sync: block is nil")
	ErrBeaconBlockInvalid     = errors.New("beacon sync: block validation failed")
	ErrBeaconSidecarNil       = errors.New("beacon sync: sidecar is nil")
	ErrBeaconSidecarInvalid   = errors.New("beacon sync: sidecar validation failed")
	ErrBeaconBlobIndexInvalid = errors.New("beacon sync: blob index out of range")
	ErrBeaconMaxRetries       = errors.New("beacon sync: max retries exceeded")
	ErrBlobRecoveryFailed     = errors.New("blob recovery: insufficient data for recovery")
)

// MaxBlobsPerBlock is the maximum number of blob sidecars per beacon block.
const MaxBlobsPerBlock = 6

// BeaconBlock represents a beacon chain block for sync purposes.
type BeaconBlock struct {
	Slot          uint64
	ProposerIndex uint64
	ParentRoot    [32]byte
	StateRoot     [32]byte
	Body          []byte // SSZ-encoded block body
}

// Hash computes a deterministic hash of the beacon block for identification.
func (b *BeaconBlock) Hash() [32]byte {
	data := make([]byte, 0, 8+8+32+32+len(b.Body))
	data = appendUint64(data, b.Slot)
	data = appendUint64(data, b.ProposerIndex)
	data = append(data, b.ParentRoot[:]...)
	data = append(data, b.StateRoot[:]...)
	data = append(data, b.Body...)
	return crypto.Keccak256Hash(data)
}

// BlobSidecar represents a blob sidecar attached to a beacon block.
type BlobSidecar struct {
	Index             uint64
	Blob              [131072]byte // 128 KiB blob
	KZGCommitment     [48]byte
	KZGProof          [48]byte
	SignedBlockHeader [32]byte // block header root this sidecar references
}

// BeaconSyncConfig holds configuration for the beacon syncer.
type BeaconSyncConfig struct {
	MaxConcurrentRequests int
	SlotTimeout           time.Duration
	BlobVerification      bool
	MaxRetries            int
}

// DefaultBeaconSyncConfig returns sensible defaults for beacon sync.
func DefaultBeaconSyncConfig() BeaconSyncConfig {
	return BeaconSyncConfig{
		MaxConcurrentRequests: 16,
		SlotTimeout:           10 * time.Second,
		BlobVerification:      true,
		MaxRetries:            3,
	}
}

// SyncStatus reports the current state of beacon sync.
type SyncStatus struct {
	CurrentSlot    uint64
	TargetSlot     uint64
	BlobsDownloaded uint64
	IsComplete     bool
}

// BeaconBlockFetcher is the interface for fetching beacon blocks from the network.
type BeaconBlockFetcher interface {
	FetchBeaconBlock(slot uint64) (*BeaconBlock, error)
	FetchBlobSidecars(slot uint64) ([]*BlobSidecar, error)
}

// BeaconSyncer manages syncing of beacon chain blocks and blob sidecars.
type BeaconSyncer struct {
	config  BeaconSyncConfig
	fetcher BeaconBlockFetcher

	mu     sync.RWMutex
	status SyncStatus

	// Processed blocks indexed by slot.
	blocks   map[uint64]*BeaconBlock
	blobs    map[uint64][]*BlobSidecar
	syncing  atomic.Bool
	cancel   chan struct{}
	recovery *BlobRecovery
}

// NewBeaconSyncer creates a new beacon syncer with the given configuration.
func NewBeaconSyncer(config BeaconSyncConfig) *BeaconSyncer {
	if config.MaxConcurrentRequests <= 0 {
		config.MaxConcurrentRequests = 16
	}
	if config.SlotTimeout <= 0 {
		config.SlotTimeout = 10 * time.Second
	}
	if config.MaxRetries <= 0 {
		config.MaxRetries = 3
	}
	return &BeaconSyncer{
		config:   config,
		blocks:   make(map[uint64]*BeaconBlock),
		blobs:    make(map[uint64][]*BlobSidecar),
		cancel:   make(chan struct{}),
		recovery: NewBlobRecovery(MaxBlobsPerBlock),
	}
}

// SetFetcher sets the block/blob fetcher used for network requests.
func (bs *BeaconSyncer) SetFetcher(f BeaconBlockFetcher) {
	bs.mu.Lock()
	defer bs.mu.Unlock()
	bs.fetcher = f
}

// GetSyncStatus returns a snapshot of the current sync status.
func (bs *BeaconSyncer) GetSyncStatus() *SyncStatus {
	bs.mu.RLock()
	defer bs.mu.RUnlock()
	s := bs.status
	return &s
}

// SyncSlotRange syncs beacon blocks and blob sidecars for a range of slots.
// The range is inclusive: [fromSlot, toSlot].
func (bs *BeaconSyncer) SyncSlotRange(fromSlot, toSlot uint64) error {
	if fromSlot > toSlot {
		return ErrBeaconInvalidSlotRange
	}
	if !bs.syncing.CompareAndSwap(false, true) {
		return ErrBeaconAlreadySyncing
	}
	defer bs.syncing.Store(false)

	bs.mu.Lock()
	bs.cancel = make(chan struct{})
	bs.status = SyncStatus{
		CurrentSlot: fromSlot,
		TargetSlot:  toSlot,
	}
	bs.mu.Unlock()

	// Semaphore for concurrent requests.
	sem := make(chan struct{}, bs.config.MaxConcurrentRequests)

	var wg sync.WaitGroup
	errCh := make(chan error, 1) // first error wins

	for slot := fromSlot; slot <= toSlot; slot++ {
		select {
		case <-bs.cancel:
			return ErrCancelled
		default:
		}

		sem <- struct{}{} // acquire
		wg.Add(1)

		go func(s uint64) {
			defer wg.Done()
			defer func() { <-sem }() // release

			if err := bs.syncSlot(s); err != nil {
				select {
				case errCh <- fmt.Errorf("slot %d: %w", s, err):
				default:
				}
			}
		}(slot)
	}

	wg.Wait()

	select {
	case err := <-errCh:
		return err
	default:
	}

	bs.mu.Lock()
	bs.status.CurrentSlot = toSlot
	bs.status.IsComplete = true
	bs.mu.Unlock()

	return nil
}

// syncSlot downloads and processes a single slot.
func (bs *BeaconSyncer) syncSlot(slot uint64) error {
	block, err := bs.RequestBlock(slot)
	if err != nil {
		return err
	}

	if err := bs.ProcessBlock(block); err != nil {
		return err
	}

	sidecars, err := bs.RequestBlobSidecars(slot)
	if err != nil {
		return err
	}

	for _, sc := range sidecars {
		if err := bs.ProcessBlobSidecar(sc); err != nil {
			return err
		}
	}

	bs.mu.Lock()
	bs.status.BlobsDownloaded += uint64(len(sidecars))
	if slot > bs.status.CurrentSlot {
		bs.status.CurrentSlot = slot
	}
	bs.mu.Unlock()

	return nil
}

// RequestBlock requests a beacon block for the given slot with retries.
func (bs *BeaconSyncer) RequestBlock(slot uint64) (*BeaconBlock, error) {
	bs.mu.RLock()
	fetcher := bs.fetcher
	bs.mu.RUnlock()

	if fetcher == nil {
		return nil, ErrBeaconBlockNil
	}

	var lastErr error
	for attempt := 0; attempt < bs.config.MaxRetries; attempt++ {
		block, err := fetcher.FetchBeaconBlock(slot)
		if err == nil {
			return block, nil
		}
		lastErr = err
	}
	return nil, fmt.Errorf("%w: %v", ErrBeaconMaxRetries, lastErr)
}

// RequestBlobSidecars requests blob sidecars for the given slot with retries.
func (bs *BeaconSyncer) RequestBlobSidecars(slot uint64) ([]*BlobSidecar, error) {
	bs.mu.RLock()
	fetcher := bs.fetcher
	bs.mu.RUnlock()

	if fetcher == nil {
		return nil, ErrBeaconSidecarNil
	}

	var lastErr error
	for attempt := 0; attempt < bs.config.MaxRetries; attempt++ {
		sidecars, err := fetcher.FetchBlobSidecars(slot)
		if err == nil {
			return sidecars, nil
		}
		lastErr = err
	}
	return nil, fmt.Errorf("%w: %v", ErrBeaconMaxRetries, lastErr)
}

// ProcessBlock validates and stores a beacon block.
func (bs *BeaconSyncer) ProcessBlock(block *BeaconBlock) error {
	if block == nil {
		return ErrBeaconBlockNil
	}
	// Validate that the block has a body.
	if len(block.Body) == 0 {
		return fmt.Errorf("%w: empty body at slot %d", ErrBeaconBlockInvalid, block.Slot)
	}
	// Validate the state root is non-zero.
	if block.StateRoot == [32]byte{} {
		return fmt.Errorf("%w: zero state root at slot %d", ErrBeaconBlockInvalid, block.Slot)
	}

	bs.mu.Lock()
	bs.blocks[block.Slot] = block
	bs.mu.Unlock()
	return nil
}

// ProcessBlobSidecar validates and stores a blob sidecar.
func (bs *BeaconSyncer) ProcessBlobSidecar(sidecar *BlobSidecar) error {
	if sidecar == nil {
		return ErrBeaconSidecarNil
	}
	if sidecar.Index >= MaxBlobsPerBlock {
		return fmt.Errorf("%w: index %d >= %d",
			ErrBeaconBlobIndexInvalid, sidecar.Index, MaxBlobsPerBlock)
	}

	// Verify the KZG commitment is non-zero if blob verification is enabled.
	if bs.config.BlobVerification {
		if sidecar.KZGCommitment == [48]byte{} {
			return fmt.Errorf("%w: zero KZG commitment at index %d",
				ErrBeaconSidecarInvalid, sidecar.Index)
		}
	}

	bs.mu.Lock()
	// Use the signed block header as a slot key proxy.
	// Find which slot this sidecar belongs to by checking stored blocks.
	found := false
	for slot, block := range bs.blocks {
		h := block.Hash()
		if h == sidecar.SignedBlockHeader {
			bs.blobs[slot] = append(bs.blobs[slot], sidecar)
			found = true
			break
		}
	}
	if !found {
		// Store under a synthetic slot key based on the header hash.
		// The sidecar can be matched later when the block arrives.
		key := uint64(sidecar.SignedBlockHeader[0])<<56 |
			uint64(sidecar.SignedBlockHeader[1])<<48
		bs.blobs[key] = append(bs.blobs[key], sidecar)
	}
	bs.mu.Unlock()
	return nil
}

// Cancel stops any in-progress sync.
func (bs *BeaconSyncer) Cancel() {
	bs.mu.Lock()
	ch := bs.cancel
	bs.mu.Unlock()
	select {
	case <-ch:
	default:
		close(ch)
	}
}

// GetBlock returns a previously synced beacon block for the given slot.
func (bs *BeaconSyncer) GetBlock(slot uint64) *BeaconBlock {
	bs.mu.RLock()
	defer bs.mu.RUnlock()
	return bs.blocks[slot]
}

// GetBlobs returns previously synced blob sidecars for the given slot.
func (bs *BeaconSyncer) GetBlobs(slot uint64) []*BlobSidecar {
	bs.mu.RLock()
	defer bs.mu.RUnlock()
	return bs.blobs[slot]
}

// BlobRecovery implements blob recovery from partial availability.
// When some blob sidecars for a slot are missing, recovery attempts to
// reconstruct them from the available ones using erasure coding concepts.
type BlobRecovery struct {
	mu      sync.Mutex
	custody int // number of blob columns this node is responsible for
}

// NewBlobRecovery creates a new BlobRecovery with the given custody count.
// Custody is the number of blob columns the node is responsible for storing.
func NewBlobRecovery(custody int) *BlobRecovery {
	if custody <= 0 {
		custody = MaxBlobsPerBlock
	}
	return &BlobRecovery{
		custody: custody,
	}
}

// AttemptRecovery attempts to recover missing blobs from the available ones.
// It returns the complete set if at least half of the expected blobs are
// available (simulating erasure code recovery with 50% threshold).
// The slot parameter identifies which slot's blobs are being recovered.
func (br *BlobRecovery) AttemptRecovery(slot uint64, available []*BlobSidecar) ([]*BlobSidecar, error) {
	br.mu.Lock()
	defer br.mu.Unlock()

	if len(available) == 0 {
		return nil, ErrBlobRecoveryFailed
	}

	// Build index of available blobs.
	present := make(map[uint64]*BlobSidecar)
	for _, sc := range available {
		present[sc.Index] = sc
	}

	// Recovery threshold: need at least half of custody columns.
	threshold := (br.custody + 1) / 2
	if len(present) < threshold {
		return nil, fmt.Errorf("%w: have %d/%d (need %d)",
			ErrBlobRecoveryFailed, len(present), br.custody, threshold)
	}

	// Reconstruct missing blobs by deriving them from available data.
	// This simulates erasure coding recovery: missing blob data is
	// generated deterministically from the available blob data.
	result := make([]*BlobSidecar, 0, br.custody)
	for i := 0; i < br.custody; i++ {
		idx := uint64(i)
		if sc, ok := present[idx]; ok {
			result = append(result, sc)
			continue
		}
		// Recover a missing blob from the first available sidecar.
		// In production this would use Reed-Solomon erasure coding.
		src := available[0]
		recovered := &BlobSidecar{
			Index:             idx,
			KZGCommitment:     src.KZGCommitment,
			KZGProof:          src.KZGProof,
			SignedBlockHeader: src.SignedBlockHeader,
		}
		// Fill recovered blob data: XOR source blob with index-based pattern.
		for j := range recovered.Blob {
			recovered.Blob[j] = src.Blob[j%len(src.Blob)] ^ byte(idx)
		}
		result = append(result, recovered)
	}

	return result, nil
}

// Custody returns the custody column count.
func (br *BlobRecovery) Custody() int {
	br.mu.Lock()
	defer br.mu.Unlock()
	return br.custody
}

// appendUint64 appends a uint64 as 8 big-endian bytes.
func appendUint64(buf []byte, v uint64) []byte {
	return append(buf, byte(v>>56), byte(v>>48), byte(v>>40), byte(v>>32),
		byte(v>>24), byte(v>>16), byte(v>>8), byte(v))
}
