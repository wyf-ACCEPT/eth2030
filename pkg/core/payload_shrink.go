// Package core provides payload shrinking for reducing execution payload size
// on the wire. Part of the EL sustainability track (Hogota+ upgrades).
package core

import (
	"bytes"
	"encoding/binary"
	"errors"
	"sync"

	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/crypto"
)

// CompressionMethod identifies the compression strategy applied to a payload.
type CompressionMethod uint8

const (
	// MethodNone means no compression was applied.
	MethodNone CompressionMethod = iota
	// MethodTxDedup removes duplicate transactions.
	MethodTxDedup
	// MethodReceiptPrune strips unnecessary receipt fields.
	MethodReceiptPrune
	// MethodStateDelta encodes state as deltas instead of full values.
	MethodStateDelta
)

// ExecutionPayload represents an execution layer payload for compression.
type ExecutionPayload struct {
	Transactions [][]byte
	Receipts     [][]byte
	StateRoot    types.Hash
	BlockHash    types.Hash
	GasUsed      uint64
	BaseFee      uint64
}

// CompressedPayload holds the compressed form of an ExecutionPayload.
type CompressedPayload struct {
	Data           []byte
	OriginalSize   uint64
	CompressedSize uint64
	Method         CompressionMethod
}

// CompressorConfig controls which compression strategies are enabled.
type CompressorConfig struct {
	EnableDedup        bool
	EnableReceiptPrune bool
	MaxPayloadSize     uint64
}

// PayloadCompressor compresses and decompresses execution payloads.
// It is safe for concurrent use.
type PayloadCompressor struct {
	mu     sync.RWMutex
	config CompressorConfig
}

// NewPayloadCompressor creates a new PayloadCompressor with the given config.
func NewPayloadCompressor(config CompressorConfig) *PayloadCompressor {
	return &PayloadCompressor{config: config}
}

// payloadSize returns the total byte size of a payload's variable-length fields.
func payloadSize(p *ExecutionPayload) uint64 {
	var size uint64
	for _, tx := range p.Transactions {
		size += uint64(len(tx))
	}
	for _, r := range p.Receipts {
		size += uint64(len(r))
	}
	// StateRoot (32) + BlockHash (32) + GasUsed (8) + BaseFee (8) = 80
	size += 80
	return size
}

// CompressPayload compresses an execution payload according to the
// compressor's configuration. Thread-safe.
func (pc *PayloadCompressor) CompressPayload(payload *ExecutionPayload) (*CompressedPayload, error) {
	if payload == nil {
		return nil, errors.New("payload_shrink: nil payload")
	}
	pc.mu.RLock()
	cfg := pc.config
	pc.mu.RUnlock()

	origSize := payloadSize(payload)

	txs := payload.Transactions
	receipts := payload.Receipts
	method := MethodNone

	// Apply dedup if enabled.
	var dedupIndices []int
	if cfg.EnableDedup && len(txs) > 0 {
		txs, dedupIndices = DeduplicateTransactions(txs)
		if len(dedupIndices) > 0 {
			method = MethodTxDedup
		}
	}

	// Apply receipt pruning if enabled.
	if cfg.EnableReceiptPrune && len(receipts) > 0 {
		receipts = PruneReceipts(receipts)
		if method == MethodNone {
			method = MethodReceiptPrune
		}
	}

	// Encode the compressed payload into a byte stream.
	data := encodeCompressedPayload(txs, dedupIndices, receipts, payload)

	if cfg.MaxPayloadSize > 0 && uint64(len(data)) > cfg.MaxPayloadSize {
		return nil, errors.New("payload_shrink: compressed payload exceeds max size")
	}

	return &CompressedPayload{
		Data:           data,
		OriginalSize:   origSize,
		CompressedSize: uint64(len(data)),
		Method:         method,
	}, nil
}

// DecompressPayload reconstructs an ExecutionPayload from its compressed form.
// Thread-safe.
func (pc *PayloadCompressor) DecompressPayload(compressed *CompressedPayload) (*ExecutionPayload, error) {
	if compressed == nil {
		return nil, errors.New("payload_shrink: nil compressed payload")
	}
	return decodeCompressedPayload(compressed.Data)
}

// EstimateCompression returns the estimated compression ratio (0.0-1.0) for a
// payload. A ratio of 0.5 means the compressed size is roughly half the
// original. Returns 1.0 if no compression benefit is expected.
func (pc *PayloadCompressor) EstimateCompression(payload *ExecutionPayload) float64 {
	if payload == nil {
		return 1.0
	}
	pc.mu.RLock()
	cfg := pc.config
	pc.mu.RUnlock()

	origSize := payloadSize(payload)
	if origSize == 0 {
		return 1.0
	}

	var saved uint64

	// Estimate dedup savings.
	if cfg.EnableDedup {
		_, indices := DeduplicateTransactions(payload.Transactions)
		for _, idx := range indices {
			if idx < len(payload.Transactions) {
				saved += uint64(len(payload.Transactions[idx]))
			}
		}
	}

	// Estimate receipt pruning savings (roughly 30% of receipt data).
	if cfg.EnableReceiptPrune {
		for _, r := range payload.Receipts {
			saved += uint64(len(r)) * 30 / 100
		}
	}

	if saved >= origSize {
		return 0.01 // near-total compression
	}
	return float64(origSize-saved) / float64(origSize)
}

// DeduplicateTransactions removes duplicate transactions from the slice.
// It returns the deduplicated slice and the indices of duplicates that were
// removed (indices refer to positions in the original slice).
func DeduplicateTransactions(txs [][]byte) ([][]byte, []int) {
	if len(txs) == 0 {
		return txs, nil
	}

	seen := make(map[types.Hash]struct{}, len(txs))
	unique := make([][]byte, 0, len(txs))
	var dupIndices []int

	for i, tx := range txs {
		h := crypto.Keccak256Hash(tx)
		if _, exists := seen[h]; exists {
			dupIndices = append(dupIndices, i)
			continue
		}
		seen[h] = struct{}{}
		unique = append(unique, tx)
	}
	return unique, dupIndices
}

// PruneReceipts removes unnecessary trailing zeros and metadata from receipt
// byte slices. Receipts shorter than 4 bytes are passed through unchanged
// since they likely contain only essential data.
func PruneReceipts(receipts [][]byte) [][]byte {
	pruned := make([][]byte, len(receipts))
	for i, r := range receipts {
		pruned[i] = pruneReceipt(r)
	}
	return pruned
}

// pruneReceipt strips trailing zero bytes from a single receipt.
func pruneReceipt(receipt []byte) []byte {
	if len(receipt) <= 4 {
		return receipt
	}
	// Trim trailing zero bytes (padding, zeroed bloom bits, etc.).
	end := len(receipt)
	for end > 0 && receipt[end-1] == 0 {
		end--
	}
	if end == 0 {
		// All zeros -- keep at least one byte.
		return receipt[:1]
	}
	result := make([]byte, end)
	copy(result, receipt[:end])
	return result
}

// --- Wire encoding for compressed payloads ---
//
// Format:
//   [4] magic "SHRK"
//   [2] tx count
//   for each tx: [4] len, [len] data
//   [2] dedup index count
//   for each dedup: [4] index
//   [2] receipt count
//   for each receipt: [4] len, [len] data
//   [32] state root
//   [32] block hash
//   [8]  gas used
//   [8]  base fee

var shrinkMagic = [4]byte{'S', 'H', 'R', 'K'}

func encodeCompressedPayload(txs [][]byte, dedupIndices []int, receipts [][]byte, p *ExecutionPayload) []byte {
	var buf bytes.Buffer

	// Magic header.
	buf.Write(shrinkMagic[:])

	// Transactions.
	writU16(&buf, uint16(len(txs)))
	for _, tx := range txs {
		writU32(&buf, uint32(len(tx)))
		buf.Write(tx)
	}

	// Dedup indices.
	writU16(&buf, uint16(len(dedupIndices)))
	for _, idx := range dedupIndices {
		writU32(&buf, uint32(idx))
	}

	// Receipts.
	writU16(&buf, uint16(len(receipts)))
	for _, r := range receipts {
		writU32(&buf, uint32(len(r)))
		buf.Write(r)
	}

	// Fixed fields.
	buf.Write(p.StateRoot[:])
	buf.Write(p.BlockHash[:])

	var tmp [8]byte
	binary.BigEndian.PutUint64(tmp[:], p.GasUsed)
	buf.Write(tmp[:])
	binary.BigEndian.PutUint64(tmp[:], p.BaseFee)
	buf.Write(tmp[:])

	return buf.Bytes()
}

func decodeCompressedPayload(data []byte) (*ExecutionPayload, error) {
	if len(data) < 4 {
		return nil, errors.New("payload_shrink: data too short")
	}
	if !bytes.Equal(data[:4], shrinkMagic[:]) {
		return nil, errors.New("payload_shrink: invalid magic header")
	}

	r := bytes.NewReader(data[4:])
	p := &ExecutionPayload{}

	// Transactions.
	txCount, err := readU16(r)
	if err != nil {
		return nil, err
	}
	p.Transactions = make([][]byte, txCount)
	for i := range txCount {
		l, err := readU32(r)
		if err != nil {
			return nil, err
		}
		buf := make([]byte, l)
		if _, err := r.Read(buf); err != nil {
			return nil, err
		}
		p.Transactions[i] = buf
	}

	// Dedup indices (informational; we don't re-expand duplicates here).
	dedupCount, err := readU16(r)
	if err != nil {
		return nil, err
	}
	for range dedupCount {
		if _, err := readU32(r); err != nil {
			return nil, err
		}
	}

	// Receipts.
	rcCount, err := readU16(r)
	if err != nil {
		return nil, err
	}
	p.Receipts = make([][]byte, rcCount)
	for i := range rcCount {
		l, err := readU32(r)
		if err != nil {
			return nil, err
		}
		buf := make([]byte, l)
		if _, err := r.Read(buf); err != nil {
			return nil, err
		}
		p.Receipts[i] = buf
	}

	// Fixed fields.
	if _, err := r.Read(p.StateRoot[:]); err != nil {
		return nil, err
	}
	if _, err := r.Read(p.BlockHash[:]); err != nil {
		return nil, err
	}

	var tmp [8]byte
	if _, err := r.Read(tmp[:]); err != nil {
		return nil, err
	}
	p.GasUsed = binary.BigEndian.Uint64(tmp[:])

	if _, err := r.Read(tmp[:]); err != nil {
		return nil, err
	}
	p.BaseFee = binary.BigEndian.Uint64(tmp[:])

	return p, nil
}

func writU16(buf *bytes.Buffer, v uint16) {
	var b [2]byte
	binary.BigEndian.PutUint16(b[:], v)
	buf.Write(b[:])
}

func writU32(buf *bytes.Buffer, v uint32) {
	var b [4]byte
	binary.BigEndian.PutUint32(b[:], v)
	buf.Write(b[:])
}

func readU16(r *bytes.Reader) (uint16, error) {
	var b [2]byte
	if _, err := r.Read(b[:]); err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint16(b[:]), nil
}

func readU32(r *bytes.Reader) (uint32, error) {
	var b [4]byte
	if _, err := r.Read(b[:]); err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint32(b[:]), nil
}
