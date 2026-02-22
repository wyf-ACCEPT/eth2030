package txpool

import (
	"container/heap"
	"encoding/json"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"github.com/eth2030/eth2030/core/types"
)

// Blob pool constants.
const (
	// DefaultMaxBlobs is the default maximum number of blob transactions in the pool.
	DefaultMaxBlobs = 256

	// DefaultMaxBlobsPerAccount is the maximum blob transactions per account.
	DefaultMaxBlobsPerAccount = 16

	// DefaultMaxBlobSize is the maximum allowed blob sidecar size (128KB per blob).
	DefaultMaxBlobSize = 128 * 1024

	// BlobGasPerBlob is the gas consumed by each blob (2^17 = 131072).
	BlobGasPerBlob = 131072

	// DefaultDatacap is the default soft-cap on total blob data storage (2.5 GB).
	DefaultDatacap = 2560 * 1024 * 1024

	// DefaultBlobPriceBump is the minimum price bump percentage for replacement.
	DefaultBlobPriceBump = 100

	// DefaultEvictionTipThreshold is the minimum effective tip (in wei) below
	// which transactions become eviction candidates.
	DefaultEvictionTipThreshold = 1_000_000_000 // 1 gwei

	// CellsPerBlob is the number of cells in one extended blob (EIP-7594 PeerDAS).
	CellsPerBlob = 128

	// DefaultCustodyColumns is the number of columns a full node custodies.
	DefaultCustodyColumns = 4

	// DefaultCellSampleCount is the number of columns sampled per slot.
	DefaultCellSampleCount = 8
)

// Blob pool error codes.
var (
	ErrBlobPoolFull         = errors.New("blob pool is full")
	ErrNotBlobTx            = errors.New("not a blob transaction")
	ErrBlobAccountLimit     = errors.New("blob per-account limit exceeded")
	ErrBlobAlreadyKnown     = errors.New("blob transaction already known")
	ErrBlobNonceTooLow      = errors.New("blob tx nonce too low")
	ErrBlobMissingHashes    = errors.New("blob transaction missing versioned hashes")
	ErrBlobFeeCapTooLow     = errors.New("blob fee cap below blob base fee")
	ErrBlobReplaceTooLow    = errors.New("blob replacement gas price too low")
	ErrBlobNotCustodied     = errors.New("blob not in custody column set")
	ErrBlobDatacapExceeded  = errors.New("blob pool datacap exceeded")
	ErrBlobSidecarNotFound  = errors.New("blob sidecar not found")
	ErrBlobOversized        = errors.New("blob sidecar exceeds maximum size")
)

// BlobMetadata tracks blob-related metadata without holding full blob data in memory.
type BlobMetadata struct {
	TxHash         types.Hash
	BlobHashes     []types.Hash // versioned hashes of the blobs
	BlobCount      int
	BlobGas        uint64
	BlobFeeCap     *big.Int
	GasFeeCap      *big.Int
	GasTipCap      *big.Int
	From           types.Address
	Nonce          uint64
	DataSize       uint64 // total sidecar data size in bytes
}

// BlobSidecar represents a blob sidecar for persistent storage.
type BlobSidecar struct {
	TxHash      types.Hash   `json:"tx_hash"`
	BlobHashes  []types.Hash `json:"blob_hashes"`
	BlobData    [][]byte     `json:"blob_data"`    // raw blob payloads
	Commitments [][]byte     `json:"commitments"`  // KZG commitments
	Proofs      [][]byte     `json:"proofs"`       // KZG proofs
	CellIndices []uint64     `json:"cell_indices"` // custody cell indices stored
}

// CustodyConfig defines the node's custody parameters for PeerDAS.
type CustodyConfig struct {
	// CustodyColumns are the column indices this node is responsible for.
	CustodyColumns []uint64

	// CellSampleCount is the number of random columns sampled per slot.
	CellSampleCount int
}

// DefaultCustodyConfig returns a default custody configuration with
// 4 sequential columns starting at 0.
func DefaultCustodyConfig() CustodyConfig {
	cols := make([]uint64, DefaultCustodyColumns)
	for i := range cols {
		cols[i] = uint64(i)
	}
	return CustodyConfig{
		CustodyColumns:  cols,
		CellSampleCount: DefaultCellSampleCount,
	}
}

// IsCustodyColumn returns true if the given column index is custodied.
func (cc *CustodyConfig) IsCustodyColumn(col uint64) bool {
	for _, c := range cc.CustodyColumns {
		if c == col {
			return true
		}
	}
	return false
}

// CustodyFilter returns the subset of cell indices that belong to this
// node's custody set. Each blob has CellsPerBlob cells, and cells map
// to columns as: column = cellIndex % CellsPerBlob.
func (cc *CustodyConfig) CustodyFilter(cellIndices []uint64) []uint64 {
	var kept []uint64
	for _, idx := range cellIndices {
		col := idx % CellsPerBlob
		if cc.IsCustodyColumn(col) {
			kept = append(kept, idx)
		}
	}
	return kept
}

// BlobPoolConfig holds configuration for the BlobPool.
type BlobPoolConfig struct {
	MaxBlobs           int      // Maximum blob transactions in pool
	MaxBlobsPerAccount int      // Maximum blob transactions per account
	MaxBlobSize        int      // Maximum allowed blob sidecar size
	MinBlobGasPrice    *big.Int // Minimum blob gas price to accept

	// EIP-8070 sparse blobpool additions.
	Datacap              uint64 // Soft-cap on total sidecar storage bytes
	PriceBump            uint64 // Minimum price bump % for replacement (100 = 2x)
	EvictionTipThreshold uint64 // Effective tip below which txs are eviction-eligible (wei)

	// Persistence.
	Datadir string // Directory for blob sidecar journal

	// Custody.
	Custody CustodyConfig
}

// DefaultBlobPoolConfig returns sensible defaults.
func DefaultBlobPoolConfig() BlobPoolConfig {
	return BlobPoolConfig{
		MaxBlobs:             DefaultMaxBlobs,
		MaxBlobsPerAccount:   DefaultMaxBlobsPerAccount,
		MaxBlobSize:          DefaultMaxBlobSize,
		MinBlobGasPrice:      big.NewInt(1),
		Datacap:              DefaultDatacap,
		PriceBump:            DefaultBlobPriceBump,
		EvictionTipThreshold: DefaultEvictionTipThreshold,
		Custody:              DefaultCustodyConfig(),
	}
}

// journalEntry is the on-disk format for WAL journal entries.
type journalEntry struct {
	Op       string        `json:"op"`       // "add" or "remove"
	TxHash   types.Hash    `json:"tx_hash"`
	Sidecar  *BlobSidecar  `json:"sidecar,omitempty"`
	Metadata *BlobMetadata `json:"metadata,omitempty"`
}

// BlobPool implements a memory-efficient blob transaction pool (EIP-8070).
// Full blob sidecar data is not kept in memory; only metadata (versioned hashes,
// sizes, fees) is tracked. Sidecars are persisted to disk with WAL journaling.
// The pool orders transactions by effective tip for eviction and uses
// custody-aligned cell tracking for PeerDAS.
type BlobPool struct {
	config      BlobPoolConfig
	state       StateReader
	blobBaseFee *big.Int // current blob base fee

	mu        sync.RWMutex
	pending   map[types.Address]*blobTxList // blob txs by sender, sorted by nonce
	lookup    map[types.Hash]*types.Transaction
	metadata  map[types.Hash]*BlobMetadata
	sidecars  map[types.Hash]*BlobSidecar // in-memory sidecar cache
	evictHeap *blobEvictHeap              // priority heap for eviction
	dataUsed  uint64                      // total sidecar bytes stored

	// Persistence.
	journalPath string
	journalFile *os.File
}

// NewBlobPool creates a new blob transaction pool.
func NewBlobPool(config BlobPoolConfig, state StateReader) *BlobPool {
	bp := &BlobPool{
		config:   config,
		state:    state,
		pending:  make(map[types.Address]*blobTxList),
		lookup:   make(map[types.Hash]*types.Transaction),
		metadata: make(map[types.Hash]*BlobMetadata),
		sidecars: make(map[types.Hash]*BlobSidecar),
	}
	bp.evictHeap = newBlobEvictHeap()

	if config.Datadir != "" {
		bp.journalPath = filepath.Join(config.Datadir, "blobpool_journal.jsonl")
		bp.recoverJournal()
		bp.openJournal()
	}
	return bp
}

// blobTxList maintains blob transactions sorted by nonce for a single account.
type blobTxList struct {
	items []*types.Transaction
}

func (l *blobTxList) Len() int { return len(l.items) }

func (l *blobTxList) Add(tx *types.Transaction) (replaced *types.Transaction) {
	idx := sort.Search(len(l.items), func(i int) bool {
		return l.items[i].Nonce() >= tx.Nonce()
	})
	if idx < len(l.items) && l.items[idx].Nonce() == tx.Nonce() {
		old := l.items[idx]
		l.items[idx] = tx
		return old
	}
	l.items = append(l.items, nil)
	copy(l.items[idx+1:], l.items[idx:])
	l.items[idx] = tx
	return nil
}

func (l *blobTxList) Remove(nonce uint64) *types.Transaction {
	for i, tx := range l.items {
		if tx.Nonce() == nonce {
			l.items = append(l.items[:i], l.items[i+1:]...)
			return tx
		}
	}
	return nil
}

func (l *blobTxList) Get(nonce uint64) *types.Transaction {
	idx := sort.Search(len(l.items), func(i int) bool {
		return l.items[i].Nonce() >= nonce
	})
	if idx < len(l.items) && l.items[idx].Nonce() == nonce {
		return l.items[idx]
	}
	return nil
}

// blobEvictHeap is a min-heap ordered by effective tip for eviction.
type blobEvictHeap struct {
	items []blobEvictItem
	index map[types.Hash]int
}

type blobEvictItem struct {
	hash         types.Hash
	effectiveTip *big.Int
}

func newBlobEvictHeap() *blobEvictHeap {
	return &blobEvictHeap{
		index: make(map[types.Hash]int),
	}
}

func (h *blobEvictHeap) Len() int { return len(h.items) }

func (h *blobEvictHeap) Less(i, j int) bool {
	return h.items[i].effectiveTip.Cmp(h.items[j].effectiveTip) < 0
}

func (h *blobEvictHeap) Swap(i, j int) {
	h.items[i], h.items[j] = h.items[j], h.items[i]
	h.index[h.items[i].hash] = i
	h.index[h.items[j].hash] = j
}

func (h *blobEvictHeap) Push(x interface{}) {
	item := x.(blobEvictItem)
	h.index[item.hash] = len(h.items)
	h.items = append(h.items, item)
}

func (h *blobEvictHeap) Pop() interface{} {
	n := len(h.items)
	item := h.items[n-1]
	h.items = h.items[:n-1]
	delete(h.index, item.hash)
	return item
}

func (h *blobEvictHeap) push(hash types.Hash, tip *big.Int) {
	heap.Push(h, blobEvictItem{hash: hash, effectiveTip: tip})
}

func (h *blobEvictHeap) remove(hash types.Hash) {
	if idx, ok := h.index[hash]; ok {
		heap.Remove(h, idx)
	}
}

func (h *blobEvictHeap) peek() *blobEvictItem {
	if len(h.items) == 0 {
		return nil
	}
	return &h.items[0]
}

// Add adds a blob transaction to the pool. Only type-3 (BlobTx) transactions
// are accepted. Returns an error if validation fails.
func (bp *BlobPool) Add(tx *types.Transaction) error {
	bp.mu.Lock()
	defer bp.mu.Unlock()

	if tx.Type() != types.BlobTxType {
		return ErrNotBlobTx
	}

	hash := tx.Hash()
	if _, ok := bp.lookup[hash]; ok {
		return ErrBlobAlreadyKnown
	}

	blobHashes := tx.BlobHashes()
	if len(blobHashes) == 0 {
		return ErrBlobMissingHashes
	}

	// Validate blob fee cap against base fee.
	if bp.blobBaseFee != nil {
		blobFeeCap := tx.BlobGasFeeCap()
		if blobFeeCap == nil || blobFeeCap.Cmp(bp.blobBaseFee) < 0 {
			return ErrBlobFeeCapTooLow
		}
	}

	from := bp.senderOf(tx)

	// Nonce validation.
	if bp.state != nil {
		stateNonce := bp.state.GetNonce(from)
		if tx.Nonce() < stateNonce {
			return ErrBlobNonceTooLow
		}
	}

	// Check for replacement.
	list := bp.pending[from]
	replaced := false
	if list != nil {
		if old := list.Get(tx.Nonce()); old != nil {
			// Replacement: require PriceBump % higher gas price.
			if !bp.hasSufficientBump(old, tx) {
				return ErrBlobReplaceTooLow
			}
			replaced = true
		}
	}

	// Per-account limit check (only for new additions, not replacements).
	if !replaced {
		if list != nil && list.Len() >= bp.config.MaxBlobsPerAccount {
			return ErrBlobAccountLimit
		}
	}

	// Pool size limit with eviction.
	if !replaced && len(bp.lookup) >= bp.config.MaxBlobs {
		if !bp.evictCheapest(tx) {
			return ErrBlobPoolFull
		}
	}

	// Insert into the pool.
	if list == nil {
		list = &blobTxList{}
		bp.pending[from] = list
	}
	oldTx := list.Add(tx)
	if oldTx != nil {
		oldHash := oldTx.Hash()
		delete(bp.lookup, oldHash)
		if oldMeta, ok := bp.metadata[oldHash]; ok {
			bp.dataUsed -= oldMeta.DataSize
		}
		delete(bp.metadata, oldHash)
		delete(bp.sidecars, oldHash)
		bp.evictHeap.remove(oldHash)
	}

	meta := &BlobMetadata{
		TxHash:     hash,
		BlobHashes: blobHashes,
		BlobCount:  len(blobHashes),
		BlobGas:    tx.BlobGas(),
		BlobFeeCap: tx.BlobGasFeeCap(),
		GasFeeCap:  tx.GasFeeCap(),
		GasTipCap:  tx.GasTipCap(),
		From:       from,
		Nonce:      tx.Nonce(),
	}

	bp.lookup[hash] = tx
	bp.metadata[hash] = meta

	// Add to eviction heap using effective tip.
	tip := blobEffectiveTip(tx, bp.blobBaseFee)
	bp.evictHeap.push(hash, tip)

	return nil
}

// AddBlobTx adds a blob transaction along with its sidecar data.
// This is the full EIP-8070 entry point that handles custody filtering
// and persistent sidecar storage.
func (bp *BlobPool) AddBlobTx(tx *types.Transaction, sidecar *BlobSidecar) error {
	if sidecar != nil {
		// Validate sidecar size.
		dataSize := sidecarSize(sidecar)
		if bp.config.MaxBlobSize > 0 && dataSize > uint64(bp.config.MaxBlobSize)*uint64(len(sidecar.BlobData)) {
			return ErrBlobOversized
		}
	}

	// Add the transaction to the pool first.
	if err := bp.Add(tx); err != nil {
		return err
	}

	if sidecar == nil {
		return nil
	}

	bp.mu.Lock()
	defer bp.mu.Unlock()

	hash := tx.Hash()
	dataSize := sidecarSize(sidecar)

	// Apply custody filter: only store cells in our custody set.
	filtered := bp.applyCustodyFilter(sidecar)

	bp.sidecars[hash] = filtered
	if meta, ok := bp.metadata[hash]; ok {
		meta.DataSize = dataSize
	}
	bp.dataUsed += dataSize

	// Journal the addition.
	bp.journalWrite(journalEntry{
		Op:       "add",
		TxHash:   hash,
		Sidecar:  filtered,
		Metadata: bp.metadata[hash],
	})

	return nil
}

// RemoveBlobTx removes a blob transaction and its sidecar from the pool.
func (bp *BlobPool) RemoveBlobTx(hash types.Hash) {
	bp.mu.Lock()
	defer bp.mu.Unlock()

	meta, ok := bp.metadata[hash]
	if !ok {
		return
	}

	if list, ok := bp.pending[meta.From]; ok {
		list.Remove(meta.Nonce)
		if list.Len() == 0 {
			delete(bp.pending, meta.From)
		}
	}

	bp.dataUsed -= meta.DataSize
	delete(bp.lookup, hash)
	delete(bp.metadata, hash)
	delete(bp.sidecars, hash)
	bp.evictHeap.remove(hash)

	bp.journalWrite(journalEntry{Op: "remove", TxHash: hash})
}

// GetBlobSidecar returns the blob sidecar for a transaction hash.
func (bp *BlobPool) GetBlobSidecar(hash types.Hash) (*BlobSidecar, error) {
	bp.mu.RLock()
	defer bp.mu.RUnlock()

	sc, ok := bp.sidecars[hash]
	if !ok {
		return nil, ErrBlobSidecarNotFound
	}
	return sc, nil
}

// PruneSidecars removes sidecars for transactions no longer in the pool
// and evicts low-priority sidecars if datacap is exceeded.
func (bp *BlobPool) PruneSidecars() int {
	bp.mu.Lock()
	defer bp.mu.Unlock()

	pruned := 0

	// Remove orphan sidecars (no matching transaction).
	for hash := range bp.sidecars {
		if _, ok := bp.metadata[hash]; !ok {
			sz := sidecarSize(bp.sidecars[hash])
			bp.dataUsed -= sz
			delete(bp.sidecars, hash)
			pruned++
		}
	}

	// Evict by lowest tip until under datacap.
	for bp.config.Datacap > 0 && bp.dataUsed > bp.config.Datacap {
		item := bp.evictHeap.peek()
		if item == nil {
			break
		}
		hash := item.hash
		if sc, ok := bp.sidecars[hash]; ok {
			bp.dataUsed -= sidecarSize(sc)
			delete(bp.sidecars, hash)
			pruned++
		}
		// Also remove the transaction itself.
		bp.removeLocked(hash)
	}

	return pruned
}

// Remove removes a blob transaction from the pool by hash.
func (bp *BlobPool) Remove(hash types.Hash) {
	bp.mu.Lock()
	defer bp.mu.Unlock()

	meta, ok := bp.metadata[hash]
	if !ok {
		return
	}

	if list, ok := bp.pending[meta.From]; ok {
		list.Remove(meta.Nonce)
		if list.Len() == 0 {
			delete(bp.pending, meta.From)
		}
	}

	bp.dataUsed -= meta.DataSize
	delete(bp.lookup, hash)
	delete(bp.metadata, hash)
	delete(bp.sidecars, hash)
	bp.evictHeap.remove(hash)
}

// Get retrieves a blob transaction by hash.
func (bp *BlobPool) Get(hash types.Hash) *types.Transaction {
	bp.mu.RLock()
	defer bp.mu.RUnlock()
	return bp.lookup[hash]
}

// Has returns true if the pool contains the blob transaction.
func (bp *BlobPool) Has(hash types.Hash) bool {
	bp.mu.RLock()
	defer bp.mu.RUnlock()
	_, ok := bp.lookup[hash]
	return ok
}

// GetMetadata returns blob metadata for a transaction hash.
func (bp *BlobPool) GetMetadata(hash types.Hash) *BlobMetadata {
	bp.mu.RLock()
	defer bp.mu.RUnlock()
	return bp.metadata[hash]
}

// Count returns the total number of blob transactions in the pool.
func (bp *BlobPool) Count() int {
	bp.mu.RLock()
	defer bp.mu.RUnlock()
	return len(bp.lookup)
}

// DataUsed returns the total sidecar data bytes currently stored.
func (bp *BlobPool) DataUsed() uint64 {
	bp.mu.RLock()
	defer bp.mu.RUnlock()
	return bp.dataUsed
}

// Pending returns all blob transactions grouped by sender.
func (bp *BlobPool) Pending() map[types.Address][]*types.Transaction {
	bp.mu.RLock()
	defer bp.mu.RUnlock()

	result := make(map[types.Address][]*types.Transaction)
	for addr, list := range bp.pending {
		txs := make([]*types.Transaction, len(list.items))
		copy(txs, list.items)
		result[addr] = txs
	}
	return result
}

// PendingSorted returns all blob transactions sorted by effective blob gas price (descending).
func (bp *BlobPool) PendingSorted() []*types.Transaction {
	bp.mu.RLock()
	defer bp.mu.RUnlock()

	var all []*types.Transaction
	for _, list := range bp.pending {
		all = append(all, list.items...)
	}

	sort.Slice(all, func(i, j int) bool {
		pi := blobEffectivePrice(all[i])
		pj := blobEffectivePrice(all[j])
		return pi.Cmp(pj) > 0
	})
	return all
}

// SetBlobBaseFee updates the blob base fee and evicts transactions below it.
func (bp *BlobPool) SetBlobBaseFee(baseFee *big.Int) {
	bp.mu.Lock()
	defer bp.mu.Unlock()

	bp.blobBaseFee = new(big.Int).Set(baseFee)

	// Evict transactions whose blob fee cap is below the new base fee.
	var toRemove []types.Hash
	for hash, meta := range bp.metadata {
		if meta.BlobFeeCap != nil && meta.BlobFeeCap.Cmp(baseFee) < 0 {
			toRemove = append(toRemove, hash)
		}
	}
	for _, hash := range toRemove {
		bp.removeLocked(hash)
	}
}

// SetStateReader updates the state reader used for nonce validation.
func (bp *BlobPool) SetStateReader(state StateReader) {
	bp.mu.Lock()
	defer bp.mu.Unlock()
	bp.state = state
}

// Close flushes and closes the journal file.
func (bp *BlobPool) Close() error {
	bp.mu.Lock()
	defer bp.mu.Unlock()
	if bp.journalFile != nil {
		err := bp.journalFile.Close()
		bp.journalFile = nil
		return err
	}
	return nil
}

// removeLocked removes a blob transaction. Caller must hold bp.mu.
func (bp *BlobPool) removeLocked(hash types.Hash) {
	meta, ok := bp.metadata[hash]
	if !ok {
		return
	}
	if list, ok := bp.pending[meta.From]; ok {
		list.Remove(meta.Nonce)
		if list.Len() == 0 {
			delete(bp.pending, meta.From)
		}
	}
	bp.dataUsed -= meta.DataSize
	delete(bp.lookup, hash)
	delete(bp.metadata, hash)
	delete(bp.sidecars, hash)
	bp.evictHeap.remove(hash)
}

// evictCheapest removes the blob transaction with the lowest effective price
// to make room for newTx. Returns true if an eviction occurred.
func (bp *BlobPool) evictCheapest(newTx *types.Transaction) bool {
	newPrice := blobEffectivePrice(newTx)

	var cheapestHash types.Hash
	var cheapestPrice *big.Int

	for hash := range bp.metadata {
		tx := bp.lookup[hash]
		if tx == nil {
			continue
		}
		price := blobEffectivePrice(tx)
		if cheapestPrice == nil || price.Cmp(cheapestPrice) < 0 {
			cheapestPrice = price
			cheapestHash = hash
		}
	}

	if cheapestPrice == nil {
		return false
	}

	// Only evict if the new tx has a higher price than the cheapest.
	if newPrice.Cmp(cheapestPrice) <= 0 {
		return false
	}

	bp.removeLocked(cheapestHash)
	return true
}

// hasSufficientBump checks if newTx has PriceBump% higher blob gas price than oldTx.
func (bp *BlobPool) hasSufficientBump(oldTx, newTx *types.Transaction) bool {
	oldPrice := blobEffectivePrice(oldTx)
	newPrice := blobEffectivePrice(newTx)

	threshold := new(big.Int).Mul(oldPrice, big.NewInt(100+PriceBump))
	threshold.Div(threshold, big.NewInt(100))
	return newPrice.Cmp(threshold) >= 0
}

// blobEffectivePrice returns the effective price for ordering blob transactions.
// It uses the blob fee cap as the primary ordering key, with gas tip cap as tiebreaker.
func blobEffectivePrice(tx *types.Transaction) *big.Int {
	blobFeeCap := tx.BlobGasFeeCap()
	if blobFeeCap != nil {
		return new(big.Int).Set(blobFeeCap)
	}
	return new(big.Int)
}

// blobEffectiveTip returns the effective tip: min(tipCap, feeCap - baseFee).
// Used for eviction ordering.
func blobEffectiveTip(tx *types.Transaction, blobBaseFee *big.Int) *big.Int {
	tipCap := tx.GasTipCap()
	if tipCap == nil {
		tipCap = new(big.Int)
	}
	feeCap := tx.BlobGasFeeCap()
	if feeCap == nil || blobBaseFee == nil {
		return new(big.Int).Set(tipCap)
	}
	excess := new(big.Int).Sub(feeCap, blobBaseFee)
	if excess.Cmp(tipCap) < 0 {
		return excess
	}
	return new(big.Int).Set(tipCap)
}

// senderOf extracts the sender address from a transaction.
func (bp *BlobPool) senderOf(tx *types.Transaction) types.Address {
	if from := tx.Sender(); from != nil {
		return *from
	}
	return types.Address{}
}

// applyCustodyFilter filters a sidecar to only retain cells in the
// node's custody column set. Returns a new sidecar with filtered data.
func (bp *BlobPool) applyCustodyFilter(sc *BlobSidecar) *BlobSidecar {
	if len(bp.config.Custody.CustodyColumns) == 0 {
		return sc
	}
	filtered := &BlobSidecar{
		TxHash:     sc.TxHash,
		BlobHashes: sc.BlobHashes,
	}
	custodied := bp.config.Custody.CustodyFilter(sc.CellIndices)
	custodySet := make(map[uint64]bool, len(custodied))
	for _, idx := range custodied {
		custodySet[idx] = true
	}

	for i, idx := range sc.CellIndices {
		if custodySet[idx] {
			filtered.CellIndices = append(filtered.CellIndices, idx)
			if i < len(sc.BlobData) {
				filtered.BlobData = append(filtered.BlobData, sc.BlobData[i])
			}
			if i < len(sc.Commitments) {
				filtered.Commitments = append(filtered.Commitments, sc.Commitments[i])
			}
			if i < len(sc.Proofs) {
				filtered.Proofs = append(filtered.Proofs, sc.Proofs[i])
			}
		}
	}
	return filtered
}

// sidecarSize returns the total byte size of a sidecar's data.
func sidecarSize(sc *BlobSidecar) uint64 {
	if sc == nil {
		return 0
	}
	var total uint64
	for _, b := range sc.BlobData {
		total += uint64(len(b))
	}
	for _, c := range sc.Commitments {
		total += uint64(len(c))
	}
	for _, p := range sc.Proofs {
		total += uint64(len(p))
	}
	return total
}

// Journal persistence.

func (bp *BlobPool) openJournal() {
	if bp.journalPath == "" {
		return
	}
	dir := filepath.Dir(bp.journalPath)
	os.MkdirAll(dir, 0755)
	f, err := os.OpenFile(bp.journalPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	bp.journalFile = f
}

func (bp *BlobPool) journalWrite(entry journalEntry) {
	if bp.journalFile == nil {
		return
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return
	}
	data = append(data, '\n')
	bp.journalFile.Write(data)
}

// recoverJournal replays the journal file to restore pool state.
func (bp *BlobPool) recoverJournal() {
	if bp.journalPath == "" {
		return
	}
	data, err := os.ReadFile(bp.journalPath)
	if err != nil {
		return
	}

	// Parse line-delimited JSON.
	var entries []journalEntry
	start := 0
	for i := 0; i < len(data); i++ {
		if data[i] == '\n' {
			line := data[start:i]
			start = i + 1
			if len(line) == 0 {
				continue
			}
			var entry journalEntry
			if err := json.Unmarshal(line, &entry); err != nil {
				continue
			}
			entries = append(entries, entry)
		}
	}
	// Handle last line without newline.
	if start < len(data) {
		var entry journalEntry
		if err := json.Unmarshal(data[start:], &entry); err == nil {
			entries = append(entries, entry)
		}
	}

	// Replay entries.
	for _, e := range entries {
		switch e.Op {
		case "add":
			if e.Metadata != nil {
				bp.metadata[e.TxHash] = e.Metadata
				if e.Metadata.DataSize > 0 {
					bp.dataUsed += e.Metadata.DataSize
				}
			}
			if e.Sidecar != nil {
				bp.sidecars[e.TxHash] = e.Sidecar
			}
		case "remove":
			if meta, ok := bp.metadata[e.TxHash]; ok {
				bp.dataUsed -= meta.DataSize
			}
			delete(bp.metadata, e.TxHash)
			delete(bp.sidecars, e.TxHash)
		}
	}

	// Truncate journal after successful recovery (compact).
	os.Remove(bp.journalPath)
}
