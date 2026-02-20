// req_resp.go implements a high-level request-response messaging layer for
// Ethereum P2P communication. It builds on the existing ReqRespProtocol and
// provides typed request builders for beacon blocks by range/root, blob
// sidecars, data columns (PeerDAS), chunked response streaming with SSZ
// encoding markers, and configurable timeout/retry with exponential backoff.
package p2p

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"sync"
	"time"
)

// Req/Resp method identifiers for additional protocol methods.
const (
	// DataColumnsByRangeV1 requests data columns for PeerDAS sampling.
	DataColumnsByRangeV1 MethodID = iota + 100
	// DataColumnsByRootV1 requests specific data columns by root.
	DataColumnsByRootV1
)

// BeaconBlocksByRangeRequest encodes a request for blocks in a slot range.
type BeaconBlocksByRangeRequest struct {
	StartSlot uint64
	Count     uint64
	Step      uint64 // Deprecated in later specs; usually 1.
}

// Encode serializes the request to wire format (24 bytes: 3 x uint64).
func (r *BeaconBlocksByRangeRequest) Encode() []byte {
	buf := make([]byte, 24)
	binary.BigEndian.PutUint64(buf[0:8], r.StartSlot)
	binary.BigEndian.PutUint64(buf[8:16], r.Count)
	binary.BigEndian.PutUint64(buf[16:24], r.Step)
	return buf
}

// DecodeBeaconBlocksByRange decodes a BeaconBlocksByRangeRequest from wire bytes.
func DecodeBeaconBlocksByRange(data []byte) (*BeaconBlocksByRangeRequest, error) {
	if len(data) < 24 {
		return nil, fmt.Errorf("blocks_by_range: need 24 bytes, got %d", len(data))
	}
	return &BeaconBlocksByRangeRequest{
		StartSlot: binary.BigEndian.Uint64(data[0:8]),
		Count:     binary.BigEndian.Uint64(data[8:16]),
		Step:      binary.BigEndian.Uint64(data[16:24]),
	}, nil
}

// BeaconBlocksByRootRequest encodes a request for specific blocks by root hash.
type BeaconBlocksByRootRequest struct {
	Roots [][32]byte
}

// Encode serializes the request to wire format.
func (r *BeaconBlocksByRootRequest) Encode() []byte {
	buf := make([]byte, 4+32*len(r.Roots))
	binary.BigEndian.PutUint32(buf[0:4], uint32(len(r.Roots)))
	for i, root := range r.Roots {
		copy(buf[4+i*32:4+(i+1)*32], root[:])
	}
	return buf
}

// DecodeBeaconBlocksByRoot decodes a BeaconBlocksByRootRequest from wire bytes.
func DecodeBeaconBlocksByRoot(data []byte) (*BeaconBlocksByRootRequest, error) {
	if len(data) < 4 {
		return nil, fmt.Errorf("blocks_by_root: need at least 4 bytes, got %d", len(data))
	}
	count := binary.BigEndian.Uint32(data[0:4])
	if len(data) < int(4+count*32) {
		return nil, fmt.Errorf("blocks_by_root: need %d bytes for %d roots, got %d", 4+count*32, count, len(data))
	}
	roots := make([][32]byte, count)
	for i := uint32(0); i < count; i++ {
		copy(roots[i][:], data[4+i*32:4+(i+1)*32])
	}
	return &BeaconBlocksByRootRequest{Roots: roots}, nil
}

// BlobSidecarsByRangeRequest requests blob sidecars in a slot range (EIP-7594).
type BlobSidecarsByRangeRequest struct {
	StartSlot uint64
	Count     uint64
}

// Encode serializes the request to wire format (16 bytes).
func (r *BlobSidecarsByRangeRequest) Encode() []byte {
	buf := make([]byte, 16)
	binary.BigEndian.PutUint64(buf[0:8], r.StartSlot)
	binary.BigEndian.PutUint64(buf[8:16], r.Count)
	return buf
}

// DecodeBlobSidecarsByRange decodes a BlobSidecarsByRangeRequest.
func DecodeBlobSidecarsByRange(data []byte) (*BlobSidecarsByRangeRequest, error) {
	if len(data) < 16 {
		return nil, fmt.Errorf("blob_sidecars_by_range: need 16 bytes, got %d", len(data))
	}
	return &BlobSidecarsByRangeRequest{
		StartSlot: binary.BigEndian.Uint64(data[0:8]),
		Count:     binary.BigEndian.Uint64(data[8:16]),
	}, nil
}

// DataColumnsByRangeRequest requests data columns for PeerDAS sampling.
type DataColumnsByRangeRequest struct {
	StartSlot   uint64
	Count       uint64
	ColumnIndex uint64 // Which column index to fetch.
}

// Encode serializes the request to wire format (24 bytes).
func (r *DataColumnsByRangeRequest) Encode() []byte {
	buf := make([]byte, 24)
	binary.BigEndian.PutUint64(buf[0:8], r.StartSlot)
	binary.BigEndian.PutUint64(buf[8:16], r.Count)
	binary.BigEndian.PutUint64(buf[16:24], r.ColumnIndex)
	return buf
}

// DecodeDataColumnsByRange decodes a DataColumnsByRangeRequest.
func DecodeDataColumnsByRange(data []byte) (*DataColumnsByRangeRequest, error) {
	if len(data) < 24 {
		return nil, fmt.Errorf("data_columns_by_range: need 24 bytes, got %d", len(data))
	}
	return &DataColumnsByRangeRequest{
		StartSlot:   binary.BigEndian.Uint64(data[0:8]),
		Count:       binary.BigEndian.Uint64(data[8:16]),
		ColumnIndex: binary.BigEndian.Uint64(data[16:24]),
	}, nil
}

// SSZChunk represents a single chunk in a streamed SSZ-encoded response.
type SSZChunk struct {
	Code    ResponseCode
	Length  uint32 // SSZ payload length.
	Payload []byte // SSZ-encoded payload.
	Error   string // Non-empty on error responses.
}

// EncodeSSZChunk encodes a single SSZ response chunk to wire format.
// Format: code[1] || length[4] || payload[length] || err_len[2] || err[err_len]
func EncodeSSZChunk(chunk SSZChunk) []byte {
	errBytes := []byte(chunk.Error)
	size := 1 + 4 + len(chunk.Payload) + 2 + len(errBytes)
	buf := make([]byte, size)
	buf[0] = byte(chunk.Code)
	binary.BigEndian.PutUint32(buf[1:5], uint32(len(chunk.Payload)))
	copy(buf[5:], chunk.Payload)
	off := 5 + len(chunk.Payload)
	binary.BigEndian.PutUint16(buf[off:], uint16(len(errBytes)))
	copy(buf[off+2:], errBytes)
	return buf
}

// DecodeSSZChunk decodes a single SSZ response chunk from wire bytes.
func DecodeSSZChunk(data []byte) (*SSZChunk, int, error) {
	if len(data) < 7 { // 1 + 4 + 2 minimum
		return nil, 0, errors.New("ssz_chunk: data too short")
	}
	code := ResponseCode(data[0])
	payloadLen := binary.BigEndian.Uint32(data[1:5])

	off := 5
	if uint32(len(data)-off) < payloadLen {
		return nil, 0, errors.New("ssz_chunk: truncated payload")
	}
	payload := make([]byte, payloadLen)
	copy(payload, data[off:off+int(payloadLen)])
	off += int(payloadLen)

	if len(data)-off < 2 {
		return nil, 0, errors.New("ssz_chunk: truncated error length")
	}
	errLen := binary.BigEndian.Uint16(data[off:])
	off += 2
	if len(data)-off < int(errLen) {
		return nil, 0, errors.New("ssz_chunk: truncated error")
	}
	errMsg := string(data[off : off+int(errLen)])
	off += int(errLen)

	return &SSZChunk{
		Code:    code,
		Length:  payloadLen,
		Payload: payload,
		Error:   errMsg,
	}, off, nil
}

// DecodeSSZStream decodes all chunks from a streamed SSZ response.
func DecodeSSZStream(data []byte) ([]SSZChunk, error) {
	var chunks []SSZChunk
	offset := 0
	for offset < len(data) {
		chunk, consumed, err := DecodeSSZChunk(data[offset:])
		if err != nil {
			return chunks, fmt.Errorf("ssz_stream: chunk %d: %w", len(chunks), err)
		}
		chunks = append(chunks, *chunk)
		offset += consumed
	}
	return chunks, nil
}

// RetryConfig configures timeout and exponential backoff retry behavior.
type RetryConfig struct {
	// MaxRetries is the maximum number of retry attempts.
	MaxRetries int
	// InitialBackoff is the delay before the first retry.
	InitialBackoff time.Duration
	// MaxBackoff caps the exponential backoff growth.
	MaxBackoff time.Duration
	// BackoffMultiplier scales the backoff between retries.
	BackoffMultiplier float64
	// RequestTimeout is the per-attempt timeout.
	RequestTimeout time.Duration
}

// DefaultRetryConfig returns sensible retry defaults.
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxRetries:        3,
		InitialBackoff:    100 * time.Millisecond,
		MaxBackoff:        5 * time.Second,
		BackoffMultiplier: 2.0,
		RequestTimeout:    10 * time.Second,
	}
}

// backoffDuration computes the backoff duration for the given attempt.
func (rc *RetryConfig) backoffDuration(attempt int) time.Duration {
	if attempt <= 0 {
		return rc.InitialBackoff
	}
	backoff := float64(rc.InitialBackoff) * math.Pow(rc.BackoffMultiplier, float64(attempt))
	if backoff > float64(rc.MaxBackoff) {
		backoff = float64(rc.MaxBackoff)
	}
	return time.Duration(backoff)
}

// ReqRespErrors for the request-response layer.
var (
	ErrReqRespMaxRetries = errors.New("req_resp: max retries exceeded")
	ErrReqRespTimeout    = errors.New("req_resp: request timed out")
	ErrReqRespClosed     = errors.New("req_resp: manager closed")
)

// ReqRespManager wraps a ReqRespProtocol with retry logic and typed helpers
// for beacon block, blob sidecar, and data column requests. Thread-safe.
type ReqRespManager struct {
	mu       sync.RWMutex
	protocol *ReqRespProtocol
	retry    RetryConfig
	closed   bool
}

// NewReqRespManager creates a manager with the given protocol and retry config.
func NewReqRespManager(protocol *ReqRespProtocol, retry RetryConfig) *ReqRespManager {
	return &ReqRespManager{
		protocol: protocol,
		retry:    retry,
	}
}

// RequestBlocksByRange sends a BeaconBlocksByRange request with retries.
func (rm *ReqRespManager) RequestBlocksByRange(peer string, req *BeaconBlocksByRangeRequest) (*StreamedResponse, error) {
	return rm.sendStreamingWithRetry(peer, BeaconBlocksByRangeV2, req.Encode())
}

// RequestBlocksByRoot sends a BeaconBlocksByRoot request with retries.
func (rm *ReqRespManager) RequestBlocksByRoot(peer string, req *BeaconBlocksByRootRequest) (*StreamedResponse, error) {
	return rm.sendStreamingWithRetry(peer, BeaconBlocksByRootV2, req.Encode())
}

// RequestBlobSidecarsByRange sends a BlobSidecarsByRange request with retries.
func (rm *ReqRespManager) RequestBlobSidecarsByRange(peer string, req *BlobSidecarsByRangeRequest) (*StreamedResponse, error) {
	return rm.sendStreamingWithRetry(peer, BlobSidecarsByRangeV1, req.Encode())
}

// RequestDataColumnsByRange sends a DataColumnsByRange request with retries.
func (rm *ReqRespManager) RequestDataColumnsByRange(peer string, req *DataColumnsByRangeRequest) (*StreamedResponse, error) {
	return rm.sendStreamingWithRetry(peer, DataColumnsByRangeV1, req.Encode())
}

// sendStreamingWithRetry executes a streaming request with exponential backoff.
func (rm *ReqRespManager) sendStreamingWithRetry(peer string, method MethodID, payload []byte) (*StreamedResponse, error) {
	rm.mu.RLock()
	if rm.closed {
		rm.mu.RUnlock()
		return nil, ErrReqRespClosed
	}
	rm.mu.RUnlock()

	var lastErr error
	for attempt := 0; attempt <= rm.retry.MaxRetries; attempt++ {
		if attempt > 0 {
			backoff := rm.retry.backoffDuration(attempt - 1)
			time.Sleep(backoff)
		}

		resp, err := rm.protocol.SendStreamingRequest(peer, method, payload)
		if err == nil {
			return resp, nil
		}
		lastErr = err

		// Do not retry on permanent errors.
		if errors.Is(err, ErrProtocolClosed) || errors.Is(err, ErrProtocolRateLimited) {
			return nil, err
		}
	}
	return nil, fmt.Errorf("%w: %v", ErrReqRespMaxRetries, lastErr)
}

// Close shuts down the manager.
func (rm *ReqRespManager) Close() {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	rm.closed = true
}

// RetryConfigForManager returns the current retry config.
func (rm *ReqRespManager) RetryConfigForManager() RetryConfig {
	return rm.retry
}
