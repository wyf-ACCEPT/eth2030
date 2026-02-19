package das

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sync"

	"github.com/eth2028/eth2028/core/types"
	"golang.org/x/crypto/sha3"
)

// DAS network errors.
var (
	ErrDASNotStarted       = errors.New("das/network: not started")
	ErrInvalidBlobIdx      = errors.New("das/network: blob index out of range")
	ErrInvalidCellIdx      = errors.New("das/network: cell index out of range")
	ErrInvalidSampleData   = errors.New("das/network: invalid sample data")
	ErrSampleNotAvailable  = errors.New("das/network: sample not available")
	ErrVerificationFailed  = errors.New("das/network: sample verification failed")
	ErrReconstructNotReady = errors.New("das/network: not enough fragments for reconstruction")
	ErrReconstructDone     = errors.New("das/network: reconstruction already complete")
	ErrDuplicateFragment   = errors.New("das/network: duplicate fragment index")
	ErrFragmentOutOfRange  = errors.New("das/network: fragment index exceeds column size")
)

// DASNetworkConfig configures the DAS network layer.
type DASNetworkConfig struct {
	// NumSubnets is the total number of gossip subnets.
	NumSubnets uint64

	// SamplesPerSlot is how many samples a node requests per slot.
	SamplesPerSlot uint64

	// MinCustodySubnets is the minimum number of subnets a node must custody.
	MinCustodySubnets uint64

	// ColumnSize is the size of a full column in bytes (cells per blob * bytes per cell).
	ColumnSize uint64
}

// DefaultDASNetworkConfig returns sensible defaults based on PeerDAS parameters.
func DefaultDASNetworkConfig() DASNetworkConfig {
	return DASNetworkConfig{
		NumSubnets:        DataColumnSidecarSubnetCount, // 64
		SamplesPerSlot:    SamplesPerSlot,               // 8
		MinCustodySubnets: CustodyRequirement,           // 4
		ColumnSize:        BytesPerCell,                  // 2048
	}
}

// SampleResponse holds data returned from a sample request.
type SampleResponse struct {
	// BlobIndex identifies which blob in the block.
	BlobIndex uint64

	// CellIndex identifies the column position of the cell.
	CellIndex uint64

	// Data is the raw cell data.
	Data []byte

	// Proof is the KZG proof for this cell.
	Proof []byte
}

// SampleStore is the interface for local sample storage.
type SampleStore interface {
	GetSample(blobIndex, cellIndex uint64) (*SampleResponse, bool)
	PutSample(sample *SampleResponse)
}

// DASNetwork connects data availability sampling to the P2P layer. It handles
// requesting samples from peers, serving locally stored samples, verifying
// samples against commitments, and managing custody subnet assignments.
type DASNetwork struct {
	mu      sync.RWMutex
	config  DASNetworkConfig
	store   SampleStore
	started bool
}

// NewDASNetwork creates a new DAS network handler.
func NewDASNetwork(config DASNetworkConfig) *DASNetwork {
	return &DASNetwork{
		config: config,
		store:  newMemorySampleStore(),
	}
}

// NewDASNetworkWithStore creates a DAS network with a custom sample store.
func NewDASNetworkWithStore(config DASNetworkConfig, store SampleStore) *DASNetwork {
	return &DASNetwork{
		config: config,
		store:  store,
	}
}

// Start initializes the DAS network.
func (dn *DASNetwork) Start() {
	dn.mu.Lock()
	defer dn.mu.Unlock()
	dn.started = true
}

// Stop shuts down the DAS network.
func (dn *DASNetwork) Stop() {
	dn.mu.Lock()
	defer dn.mu.Unlock()
	dn.started = false
}

// isStarted returns the current started state.
func (dn *DASNetwork) isStarted() bool {
	dn.mu.RLock()
	defer dn.mu.RUnlock()
	return dn.started
}

// Config returns the current DAS network configuration.
func (dn *DASNetwork) Config() DASNetworkConfig {
	dn.mu.RLock()
	defer dn.mu.RUnlock()
	return dn.config
}

// RequestSamples requests samples for a given blob at the specified cell indices.
// In a full implementation this queries peers on the appropriate subnets;
// here it looks up the local store.
func (dn *DASNetwork) RequestSamples(blobIndex uint64, indices []uint64) ([]*SampleResponse, error) {
	if !dn.isStarted() {
		return nil, ErrDASNotStarted
	}
	if blobIndex >= MaxBlobCommitmentsPerBlock {
		return nil, fmt.Errorf("%w: %d >= %d", ErrInvalidBlobIdx, blobIndex, MaxBlobCommitmentsPerBlock)
	}

	results := make([]*SampleResponse, 0, len(indices))
	for _, idx := range indices {
		if idx >= NumberOfColumns {
			return nil, fmt.Errorf("%w: %d >= %d", ErrInvalidCellIdx, idx, NumberOfColumns)
		}
		sample, ok := dn.store.GetSample(blobIndex, idx)
		if ok {
			results = append(results, sample)
		}
	}
	return results, nil
}

// ServeSample retrieves a locally stored sample to serve to a requesting peer.
func (dn *DASNetwork) ServeSample(blobIndex uint64, cellIndex uint64) (*SampleResponse, error) {
	if !dn.isStarted() {
		return nil, ErrDASNotStarted
	}
	if blobIndex >= MaxBlobCommitmentsPerBlock {
		return nil, fmt.Errorf("%w: %d >= %d", ErrInvalidBlobIdx, blobIndex, MaxBlobCommitmentsPerBlock)
	}
	if cellIndex >= NumberOfColumns {
		return nil, fmt.Errorf("%w: %d >= %d", ErrInvalidCellIdx, cellIndex, NumberOfColumns)
	}

	sample, ok := dn.store.GetSample(blobIndex, cellIndex)
	if !ok {
		return nil, ErrSampleNotAvailable
	}
	return sample, nil
}

// StoreSample stores a sample in the local store for serving to peers.
func (dn *DASNetwork) StoreSample(sample *SampleResponse) error {
	if sample == nil || len(sample.Data) == 0 {
		return ErrInvalidSampleData
	}
	if sample.BlobIndex >= MaxBlobCommitmentsPerBlock {
		return fmt.Errorf("%w: blob %d", ErrInvalidBlobIdx, sample.BlobIndex)
	}
	if sample.CellIndex >= NumberOfColumns {
		return fmt.Errorf("%w: cell %d", ErrInvalidCellIdx, sample.CellIndex)
	}
	dn.store.PutSample(sample)
	return nil
}

// VerifySample verifies a sample's data against a commitment using a hash-based
// scheme. In production this would use KZG proof verification; here we use
// keccak256(commitment || blobIndex || cellIndex || data) == proof.
func VerifySample(sample *SampleResponse, commitment []byte) bool {
	if sample == nil || len(sample.Data) == 0 || len(commitment) == 0 {
		return false
	}
	if len(sample.Proof) == 0 {
		return false
	}
	expected := computeSampleProof(commitment, sample.BlobIndex, sample.CellIndex, sample.Data)
	if len(expected) != len(sample.Proof) {
		return false
	}
	for i := range expected {
		if expected[i] != sample.Proof[i] {
			return false
		}
	}
	return true
}

// ComputeSampleProof computes the expected proof for a sample. Exported for
// test helpers to build valid samples.
func ComputeSampleProof(commitment []byte, blobIndex, cellIndex uint64, data []byte) []byte {
	return computeSampleProof(commitment, blobIndex, cellIndex, data)
}

// computeSampleProof computes keccak256(commitment || blobIndex || cellIndex || data).
func computeSampleProof(commitment []byte, blobIndex, cellIndex uint64, data []byte) []byte {
	h := sha3.NewLegacyKeccak256()
	h.Write(commitment)
	var buf [16]byte
	binary.LittleEndian.PutUint64(buf[:8], blobIndex)
	binary.LittleEndian.PutUint64(buf[8:], cellIndex)
	h.Write(buf[:])
	h.Write(data)
	return h.Sum(nil)
}

// --- Custody subnet management ---

// CustodySubnet defines which subnets a node is responsible for custodying.
type CustodySubnet struct {
	// NodeID identifies the node.
	NodeID types.Hash

	// SubnetIDs lists the subnet IDs this node custodies.
	SubnetIDs []uint64

	// NumSubnets is the total number of subnets in the network.
	NumSubnets uint64
}

// Contains reports whether the custody assignment includes the given subnet.
func (cs *CustodySubnet) Contains(subnetID uint64) bool {
	for _, s := range cs.SubnetIDs {
		if s == subnetID {
			return true
		}
	}
	return false
}

// AssignCustody deterministically assigns custody subnets to a node based on
// its node ID. The assignment is stable: the same nodeID always gets the same
// subnets. This uses a hash-chain approach similar to GetCustodyGroups.
func AssignCustody(nodeID types.Hash, numSubnets uint64) *CustodySubnet {
	minSubnets := uint64(CustodyRequirement)
	if numSubnets < minSubnets {
		numSubnets = minSubnets
	}
	if numSubnets > DataColumnSidecarSubnetCount {
		numSubnets = DataColumnSidecarSubnetCount
	}

	subnets := make([]uint64, 0, numSubnets)
	seen := make(map[uint64]bool)

	currentHash := nodeID
	for uint64(len(subnets)) < numSubnets {
		h := sha3.NewLegacyKeccak256()
		h.Write(currentHash[:])
		digest := h.Sum(nil)

		val := binary.LittleEndian.Uint64(digest[:8])
		subnet := val % DataColumnSidecarSubnetCount

		if !seen[subnet] {
			seen[subnet] = true
			subnets = append(subnets, subnet)
		}

		copy(currentHash[:], digest[:32])
	}

	return &CustodySubnet{
		NodeID:     nodeID,
		SubnetIDs:  subnets,
		NumSubnets: DataColumnSidecarSubnetCount,
	}
}

// IsCustodian returns true if the given node is a custodian for the specified
// subnet. It recomputes the assignment deterministically.
func IsCustodian(nodeID types.Hash, subnetID uint64) bool {
	custody := AssignCustody(nodeID, CustodyRequirement)
	return custody.Contains(subnetID)
}

// --- Column reconstruction from partial data ---

// ColumnReconstructor accumulates fragments of a column and reconstructs the
// full column data once enough fragments have been received.
type ColumnReconstructor struct {
	mu        sync.Mutex
	threshold int
	columnSize int
	fragments map[uint64][]byte
	complete  bool
}

// NewColumnReconstructor creates a new column reconstructor.
// threshold is the minimum number of fragments needed for reconstruction.
func NewColumnReconstructor(threshold int) *ColumnReconstructor {
	return &ColumnReconstructor{
		threshold:  threshold,
		columnSize: BytesPerCell,
		fragments:  make(map[uint64][]byte),
	}
}

// AddFragment adds a data fragment at the given index. Returns true if
// the threshold has been reached and reconstruction is possible.
func (cr *ColumnReconstructor) AddFragment(index uint64, data []byte) bool {
	cr.mu.Lock()
	defer cr.mu.Unlock()

	if cr.complete {
		return true
	}

	// Store a copy to avoid aliasing.
	frag := make([]byte, len(data))
	copy(frag, data)
	cr.fragments[index] = frag

	return len(cr.fragments) >= cr.threshold
}

// FragmentCount returns the number of fragments received so far.
func (cr *ColumnReconstructor) FragmentCount() int {
	cr.mu.Lock()
	defer cr.mu.Unlock()
	return len(cr.fragments)
}

// CanReconstruct reports whether enough fragments have been received.
func (cr *ColumnReconstructor) CanReconstruct() bool {
	cr.mu.Lock()
	defer cr.mu.Unlock()
	return len(cr.fragments) >= cr.threshold
}

// Reconstruct combines all received fragments into the full column data.
// It XORs overlapping fragments to simulate simple erasure recovery. In a
// full implementation this would use Reed-Solomon decoding.
//
// The simplest model: each fragment covers the same column (one cell per blob),
// and we need threshold-many unique fragments to consider the column available.
// Here we concatenate all fragment data sorted by index.
func (cr *ColumnReconstructor) Reconstruct() ([]byte, error) {
	cr.mu.Lock()
	defer cr.mu.Unlock()

	if cr.complete {
		return nil, ErrReconstructDone
	}
	if len(cr.fragments) < cr.threshold {
		return nil, fmt.Errorf("%w: have %d, need %d",
			ErrReconstructNotReady, len(cr.fragments), cr.threshold)
	}

	// Find the maximum index to determine output size.
	maxIdx := uint64(0)
	for idx := range cr.fragments {
		if idx > maxIdx {
			maxIdx = idx
		}
	}

	// Build output: place each fragment at its index position.
	fragSize := 0
	for _, data := range cr.fragments {
		if len(data) > fragSize {
			fragSize = len(data)
		}
	}
	if fragSize == 0 {
		fragSize = cr.columnSize
	}

	outputSize := (int(maxIdx) + 1) * fragSize
	result := make([]byte, outputSize)

	for idx, data := range cr.fragments {
		offset := int(idx) * fragSize
		copy(result[offset:], data)
	}

	cr.complete = true
	return result, nil
}

// Reset clears all fragments and allows reuse of the reconstructor.
func (cr *ColumnReconstructor) Reset() {
	cr.mu.Lock()
	defer cr.mu.Unlock()
	cr.fragments = make(map[uint64][]byte)
	cr.complete = false
}

// --- In-memory sample store ---

type sampleKey struct {
	blobIndex uint64
	cellIndex uint64
}

type memorySampleStore struct {
	mu      sync.RWMutex
	samples map[sampleKey]*SampleResponse
}

func newMemorySampleStore() *memorySampleStore {
	return &memorySampleStore{
		samples: make(map[sampleKey]*SampleResponse),
	}
}

func (ms *memorySampleStore) GetSample(blobIndex, cellIndex uint64) (*SampleResponse, bool) {
	ms.mu.RLock()
	defer ms.mu.RUnlock()
	s, ok := ms.samples[sampleKey{blobIndex, cellIndex}]
	return s, ok
}

func (ms *memorySampleStore) PutSample(sample *SampleResponse) {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	ms.samples[sampleKey{sample.BlobIndex, sample.CellIndex}] = sample
}
