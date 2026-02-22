// chaindb.go provides a high-level chain database wrapping the low-level
// rawdb accessors with LRU caches for blocks, headers, receipts, and total
// difficulty. It is thread-safe and intended as the primary interface for
// reading and writing blockchain data.
package rawdb

import (
	"encoding/binary"
	"errors"
	"math/big"
	"sync"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/rlp"
)

// Cache sizes.
const (
	blockCacheSize   = 256
	headerCacheSize  = 1024
	receiptCacheSize = 256
	tdCacheSize      = 1024
)

// Schema extension for total difficulty.
var tdPrefix = []byte("d") // d + num (8 bytes BE) + hash -> total difficulty RLP

// tdKey = tdPrefix + num + hash
func tdKey(number uint64, hash types.Hash) []byte {
	key := make([]byte, 0, len(tdPrefix)+8+32)
	key = append(key, tdPrefix...)
	key = append(key, encodeBlockNumber(number)...)
	key = append(key, hash[:]...)
	return key
}

// lruCache is a simple fixed-size LRU cache using a doubly-linked list and map.
type lruCache[K comparable, V any] struct {
	mu       sync.Mutex
	capacity int
	items    map[K]*lruNode[K, V]
	head     *lruNode[K, V] // most recent
	tail     *lruNode[K, V] // least recent
}

type lruNode[K comparable, V any] struct {
	key        K
	value      V
	prev, next *lruNode[K, V]
}

func newLRU[K comparable, V any](capacity int) *lruCache[K, V] {
	return &lruCache[K, V]{
		capacity: capacity,
		items:    make(map[K]*lruNode[K, V], capacity),
	}
}

func (c *lruCache[K, V]) get(key K) (V, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	node, ok := c.items[key]
	if !ok {
		var zero V
		return zero, false
	}
	c.moveToFront(node)
	return node.value, true
}

func (c *lruCache[K, V]) put(key K, value V) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if node, ok := c.items[key]; ok {
		node.value = value
		c.moveToFront(node)
		return
	}
	if len(c.items) >= c.capacity {
		c.evict()
	}
	node := &lruNode[K, V]{key: key, value: value}
	c.items[key] = node
	c.pushFront(node)
}

func (c *lruCache[K, V]) remove(key K) {
	c.mu.Lock()
	defer c.mu.Unlock()
	node, ok := c.items[key]
	if !ok {
		return
	}
	c.removeNode(node)
	delete(c.items, key)
}

func (c *lruCache[K, V]) pushFront(node *lruNode[K, V]) {
	node.prev = nil
	node.next = c.head
	if c.head != nil {
		c.head.prev = node
	}
	c.head = node
	if c.tail == nil {
		c.tail = node
	}
}

func (c *lruCache[K, V]) removeNode(node *lruNode[K, V]) {
	if node.prev != nil {
		node.prev.next = node.next
	} else {
		c.head = node.next
	}
	if node.next != nil {
		node.next.prev = node.prev
	} else {
		c.tail = node.prev
	}
	node.prev = nil
	node.next = nil
}

func (c *lruCache[K, V]) moveToFront(node *lruNode[K, V]) {
	if c.head == node {
		return
	}
	c.removeNode(node)
	c.pushFront(node)
}

func (c *lruCache[K, V]) evict() {
	if c.tail == nil {
		return
	}
	victim := c.tail
	c.removeNode(victim)
	delete(c.items, victim.key)
}

// ChainDB is a high-level chain database wrapping a low-level Database with
// LRU caches for frequently accessed data. It is safe for concurrent use.
type ChainDB struct {
	db Database

	blockCache   *lruCache[types.Hash, *types.Block]
	headerCache  *lruCache[types.Hash, *types.Header]
	receiptCache *lruCache[types.Hash, []*types.Receipt]
	tdCache      *lruCache[types.Hash, *big.Int]

	mu sync.RWMutex // protects head pointers and canonical lookups
}

// NewChainDB creates a new ChainDB wrapping the given low-level database.
func NewChainDB(db Database) *ChainDB {
	return &ChainDB{
		db:           db,
		blockCache:   newLRU[types.Hash, *types.Block](blockCacheSize),
		headerCache:  newLRU[types.Hash, *types.Header](headerCacheSize),
		receiptCache: newLRU[types.Hash, []*types.Receipt](receiptCacheSize),
		tdCache:      newLRU[types.Hash, *big.Int](tdCacheSize),
	}
}

// DB returns the underlying low-level database.
func (cdb *ChainDB) DB() Database { return cdb.db }

// --- Block operations ---

// ReadBlock retrieves a full block by hash, using the cache when possible.
// Returns nil if the block is not found.
func (cdb *ChainDB) ReadBlock(hash types.Hash) *types.Block {
	if block, ok := cdb.blockCache.get(hash); ok {
		return block
	}
	// Look up block number from hash.
	num, err := ReadHeaderNumber(cdb.db, hash)
	if err != nil {
		return nil
	}
	block := cdb.readBlockFromDB(num, hash)
	if block != nil {
		cdb.blockCache.put(hash, block)
	}
	return block
}

// ReadBlockByNumber retrieves a block by its canonical block number.
// Returns nil if no canonical block exists at this number.
func (cdb *ChainDB) ReadBlockByNumber(number uint64) *types.Block {
	hash, err := cdb.ReadCanonicalHash(number)
	if err != nil {
		return nil
	}
	return cdb.ReadBlock(hash)
}

// WriteBlock stores a complete block (header + body) and associated tx lookups.
func (cdb *ChainDB) WriteBlock(block *types.Block) error {
	hash := block.Hash()
	num := block.NumberU64()

	// Write header.
	if err := cdb.WriteHeader(block.Header()); err != nil {
		return err
	}

	// Encode and write body.
	bodyData, err := encodeBlockBody(block)
	if err != nil {
		return err
	}
	if err := WriteBody(cdb.db, num, hash, bodyData); err != nil {
		return err
	}

	// Write tx lookup entries.
	for _, tx := range block.Transactions() {
		txHash := tx.Hash()
		if err := WriteTxLookup(cdb.db, txHash, num); err != nil {
			return err
		}
	}

	cdb.blockCache.put(hash, block)
	return nil
}

// HasBlock checks whether a block with the given hash exists.
func (cdb *ChainDB) HasBlock(hash types.Hash) bool {
	if _, ok := cdb.blockCache.get(hash); ok {
		return true
	}
	num, err := ReadHeaderNumber(cdb.db, hash)
	if err != nil {
		return false
	}
	return HasHeader(cdb.db, num, hash)
}

// --- Header operations ---

// ReadHeader retrieves a header by hash, using the cache when possible.
// Returns nil if the header is not found.
func (cdb *ChainDB) ReadHeader(hash types.Hash) *types.Header {
	if header, ok := cdb.headerCache.get(hash); ok {
		return header
	}
	num, err := ReadHeaderNumber(cdb.db, hash)
	if err != nil {
		return nil
	}
	header := cdb.readHeaderFromDB(num, hash)
	if header != nil {
		cdb.headerCache.put(hash, header)
	}
	return header
}

// WriteHeader stores a header and its hash-to-number mapping.
func (cdb *ChainDB) WriteHeader(header *types.Header) error {
	hash := header.Hash()
	num := header.Number.Uint64()
	data, err := header.EncodeRLP()
	if err != nil {
		return err
	}
	if err := WriteHeader(cdb.db, num, hash, data); err != nil {
		return err
	}
	cdb.headerCache.put(hash, header)
	return nil
}

// --- Receipt operations ---

// ReadReceipts retrieves the receipts for a block by hash.
// Returns nil if no receipts are found.
func (cdb *ChainDB) ReadReceipts(blockHash types.Hash) []*types.Receipt {
	if receipts, ok := cdb.receiptCache.get(blockHash); ok {
		return receipts
	}
	num, err := ReadHeaderNumber(cdb.db, blockHash)
	if err != nil {
		return nil
	}
	data, err := ReadReceipts(cdb.db, num, blockHash)
	if err != nil || len(data) == 0 {
		return nil
	}
	receipts, err := decodeReceiptList(data)
	if err != nil {
		return nil
	}
	cdb.receiptCache.put(blockHash, receipts)
	return receipts
}

// WriteReceipts stores receipts for a block.
func (cdb *ChainDB) WriteReceipts(blockHash types.Hash, number uint64, receipts []*types.Receipt) error {
	data, err := encodeReceiptList(receipts)
	if err != nil {
		return err
	}
	if err := WriteReceipts(cdb.db, number, blockHash, data); err != nil {
		return err
	}
	cdb.receiptCache.put(blockHash, receipts)
	return nil
}

// --- Transaction lookup ---

// ReadTransaction retrieves a transaction by hash, returning the transaction,
// block hash, and block number. Returns nil, zero hash, 0 if not found.
func (cdb *ChainDB) ReadTransaction(txHash types.Hash) (*types.Transaction, types.Hash, uint64) {
	blockNum, err := ReadTxLookup(cdb.db, txHash)
	if err != nil {
		return nil, types.Hash{}, 0
	}
	// Get the canonical block at this number.
	blockHash, err := cdb.ReadCanonicalHash(blockNum)
	if err != nil {
		return nil, types.Hash{}, 0
	}
	block := cdb.ReadBlock(blockHash)
	if block == nil {
		return nil, types.Hash{}, 0
	}
	for _, tx := range block.Transactions() {
		if tx.Hash() == txHash {
			return tx, blockHash, blockNum
		}
	}
	return nil, types.Hash{}, 0
}

// --- Total difficulty ---

// ReadTd retrieves the total difficulty for a block hash. Returns nil if not found.
func (cdb *ChainDB) ReadTd(hash types.Hash) *big.Int {
	if td, ok := cdb.tdCache.get(hash); ok {
		return new(big.Int).Set(td)
	}
	num, err := ReadHeaderNumber(cdb.db, hash)
	if err != nil {
		return nil
	}
	data, err := cdb.db.Get(tdKey(num, hash))
	if err != nil {
		return nil
	}
	td := new(big.Int)
	if err := rlp.DecodeBytes(data, td); err != nil {
		return nil
	}
	cdb.tdCache.put(hash, td)
	return new(big.Int).Set(td)
}

// WriteTd stores the total difficulty for a block.
func (cdb *ChainDB) WriteTd(hash types.Hash, td *big.Int) error {
	num, err := ReadHeaderNumber(cdb.db, hash)
	if err != nil {
		return err
	}
	data, err := rlp.EncodeToBytes(td)
	if err != nil {
		return err
	}
	if err := cdb.db.Put(tdKey(num, hash), data); err != nil {
		return err
	}
	cdb.tdCache.put(hash, new(big.Int).Set(td))
	return nil
}

// --- Canonical chain ---

// ReadCanonicalHash retrieves the canonical block hash for a number.
func (cdb *ChainDB) ReadCanonicalHash(number uint64) (types.Hash, error) {
	cdb.mu.RLock()
	defer cdb.mu.RUnlock()
	return ReadCanonicalHash(cdb.db, number)
}

// WriteCanonicalHash stores the canonical block hash for a number.
func (cdb *ChainDB) WriteCanonicalHash(number uint64, hash types.Hash) error {
	cdb.mu.Lock()
	defer cdb.mu.Unlock()
	return WriteCanonicalHash(cdb.db, number, hash)
}

// --- Head tracking ---

// ReadHeadBlockHash retrieves the hash of the current head block.
func (cdb *ChainDB) ReadHeadBlockHash() (types.Hash, error) {
	cdb.mu.RLock()
	defer cdb.mu.RUnlock()
	return ReadHeadBlockHash(cdb.db)
}

// WriteHeadBlockHash stores the hash of the current head block.
func (cdb *ChainDB) WriteHeadBlockHash(hash types.Hash) error {
	cdb.mu.Lock()
	defer cdb.mu.Unlock()
	return WriteHeadBlockHash(cdb.db, hash)
}

// --- Internal helpers ---

// readBlockFromDB reads and decodes a block from the raw database.
func (cdb *ChainDB) readBlockFromDB(number uint64, hash types.Hash) *types.Block {
	header := cdb.readHeaderFromDB(number, hash)
	if header == nil {
		return nil
	}
	bodyData, err := ReadBody(cdb.db, number, hash)
	if err != nil {
		// Block with header but no body: return header-only block.
		return types.NewBlock(header, nil)
	}
	body, err := decodeBlockBody(bodyData)
	if err != nil {
		return types.NewBlock(header, nil)
	}
	return types.NewBlock(header, body)
}

// readHeaderFromDB reads and decodes a header from the raw database.
func (cdb *ChainDB) readHeaderFromDB(number uint64, hash types.Hash) *types.Header {
	data, err := ReadHeader(cdb.db, number, hash)
	if err != nil {
		return nil
	}
	header, err := types.DecodeHeaderRLP(data)
	if err != nil {
		return nil
	}
	return header
}

// encodeBlockBody encodes the body portion of a block (transactions + uncles).
func encodeBlockBody(block *types.Block) ([]byte, error) {
	// Encode transactions list.
	var txsPayload []byte
	for _, tx := range block.Transactions() {
		txEnc, err := tx.EncodeRLP()
		if err != nil {
			return nil, err
		}
		wrapped, err := rlp.EncodeToBytes(txEnc)
		if err != nil {
			return nil, err
		}
		txsPayload = append(txsPayload, wrapped...)
	}

	// Encode uncles list.
	var unclesPayload []byte
	for _, uncle := range block.Uncles() {
		uncleEnc, err := uncle.EncodeRLP()
		if err != nil {
			return nil, err
		}
		unclesPayload = append(unclesPayload, uncleEnc...)
	}

	var payload []byte
	payload = append(payload, rlp.WrapList(txsPayload)...)
	payload = append(payload, rlp.WrapList(unclesPayload)...)
	return rlp.WrapList(payload), nil
}

// decodeBlockBody decodes a body from RLP.
func decodeBlockBody(data []byte) (*types.Body, error) {
	s := rlp.NewStreamFromBytes(data)
	_, err := s.List()
	if err != nil {
		return nil, err
	}

	// Decode transactions.
	_, err = s.List()
	if err != nil {
		return nil, err
	}
	var txs []*types.Transaction
	for !s.AtListEnd() {
		txBytes, err := s.Bytes()
		if err != nil {
			return nil, err
		}
		tx, err := types.DecodeTxRLP(txBytes)
		if err != nil {
			return nil, err
		}
		txs = append(txs, tx)
	}
	if err := s.ListEnd(); err != nil {
		return nil, err
	}

	// Decode uncles.
	_, err = s.List()
	if err != nil {
		return nil, err
	}
	var uncles []*types.Header
	for !s.AtListEnd() {
		uncleBytes, err := s.RawItem()
		if err != nil {
			return nil, err
		}
		uncle, err := types.DecodeHeaderRLP(uncleBytes)
		if err != nil {
			return nil, err
		}
		uncles = append(uncles, uncle)
	}
	if err := s.ListEnd(); err != nil {
		return nil, err
	}
	if err := s.ListEnd(); err != nil {
		return nil, err
	}

	return &types.Body{
		Transactions: txs,
		Uncles:       uncles,
	}, nil
}

// encodeReceiptList RLP-encodes a list of receipts as a single blob.
func encodeReceiptList(receipts []*types.Receipt) ([]byte, error) {
	var payload []byte
	for _, r := range receipts {
		enc, err := r.EncodeRLP()
		if err != nil {
			return nil, err
		}
		wrapped, err := rlp.EncodeToBytes(enc)
		if err != nil {
			return nil, err
		}
		payload = append(payload, wrapped...)
	}
	return rlp.WrapList(payload), nil
}

// decodeReceiptList decodes an RLP-encoded receipt list.
func decodeReceiptList(data []byte) ([]*types.Receipt, error) {
	s := rlp.NewStreamFromBytes(data)
	_, err := s.List()
	if err != nil {
		return nil, err
	}
	var receipts []*types.Receipt
	for !s.AtListEnd() {
		raw, err := s.Bytes()
		if err != nil {
			return nil, err
		}
		r, err := types.DecodeReceiptRLP(raw)
		if err != nil {
			return nil, err
		}
		receipts = append(receipts, r)
	}
	if err := s.ListEnd(); err != nil {
		return nil, err
	}
	return receipts, nil
}

// Close closes the underlying database.
func (cdb *ChainDB) Close() error {
	return cdb.db.Close()
}

// Compile-time check: ensure KeyValueReader has Get returning ([]byte, error).
var _ = func() {
	var _ KeyValueReader = (*MemoryDB)(nil)
	_ = errors.New("")
	_ = binary.BigEndian
}
