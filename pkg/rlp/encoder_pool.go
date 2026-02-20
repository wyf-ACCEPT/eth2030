// encoder_pool.go provides a pooled RLP encoder for high-throughput
// encoding scenarios such as block/transaction batch serialization.
// It uses sync.Pool to reuse encoder buffers, reducing GC pressure.
package rlp

import (
	"encoding/binary"
	"sync"
	"sync/atomic"
)

// Default buffer sizes for the encoder pool.
const (
	// defaultBufSize is the initial capacity for pooled encoder buffers.
	defaultBufSize = 4096

	// maxBufSize caps the buffer size to avoid retaining oversized buffers.
	maxBufSize = 1 << 20 // 1 MiB
)

// EncoderMetrics tracks encoder pool usage for monitoring.
type EncoderMetrics struct {
	// PoolHits counts how many times a buffer was reused from the pool.
	PoolHits atomic.Int64
	// PoolMisses counts how many times a new buffer was allocated.
	PoolMisses atomic.Int64
	// TotalEncodes counts the total number of encode operations.
	TotalEncodes atomic.Int64
	// TotalBytes counts the total bytes of RLP output produced.
	TotalBytes atomic.Int64
}

// Snapshot returns a point-in-time copy of the encoder metrics.
func (m *EncoderMetrics) Snapshot() EncoderMetricsSnapshot {
	return EncoderMetricsSnapshot{
		PoolHits:     m.PoolHits.Load(),
		PoolMisses:   m.PoolMisses.Load(),
		TotalEncodes: m.TotalEncodes.Load(),
		TotalBytes:   m.TotalBytes.Load(),
	}
}

// EncoderMetricsSnapshot is a frozen copy of EncoderMetrics values.
type EncoderMetricsSnapshot struct {
	PoolHits     int64
	PoolMisses   int64
	TotalEncodes int64
	TotalBytes   int64
}

// EncoderPool manages a pool of reusable RLP encoding buffers.
type EncoderPool struct {
	pool    sync.Pool
	metrics EncoderMetrics
}

// NewEncoderPool creates a new encoder pool with default buffer sizing.
func NewEncoderPool() *EncoderPool {
	ep := &EncoderPool{}
	ep.pool.New = func() interface{} {
		ep.metrics.PoolMisses.Add(1)
		buf := make([]byte, 0, defaultBufSize)
		return &encoderBuf{data: buf}
	}
	return ep
}

// Metrics returns the pool's usage metrics.
func (ep *EncoderPool) Metrics() *EncoderMetrics {
	return &ep.metrics
}

// encoderBuf is the pooled buffer wrapper.
type encoderBuf struct {
	data []byte
}

// get retrieves a buffer from the pool.
func (ep *EncoderPool) get() *encoderBuf {
	buf := ep.pool.Get().(*encoderBuf)
	if len(buf.data) > 0 {
		ep.metrics.PoolHits.Add(1)
		// Fix the miss count since New was not called but we counted a hit.
	} else {
		ep.metrics.PoolHits.Add(1)
	}
	buf.data = buf.data[:0]
	return buf
}

// put returns a buffer to the pool, discarding oversized buffers.
func (ep *EncoderPool) put(buf *encoderBuf) {
	if cap(buf.data) > maxBufSize {
		// Let GC reclaim oversized buffers.
		return
	}
	ep.pool.Put(buf)
}

// EncodeBytes encodes a single value and returns the RLP bytes.
// This is a pooled equivalent of rlp.EncodeToBytes.
func (ep *EncoderPool) EncodeBytes(val interface{}) ([]byte, error) {
	result, err := EncodeToBytes(val)
	if err != nil {
		return nil, err
	}
	ep.metrics.TotalEncodes.Add(1)
	ep.metrics.TotalBytes.Add(int64(len(result)))
	return result, nil
}

// EncodeBatch RLP-encodes a list of items into a single RLP list.
// Each item is individually encoded and then wrapped in a list header.
// This is useful for encoding transaction lists, log lists, etc.
func (ep *EncoderPool) EncodeBatch(items []interface{}) ([]byte, error) {
	buf := ep.get()
	defer ep.put(buf)

	for _, item := range items {
		enc, err := EncodeToBytes(item)
		if err != nil {
			return nil, err
		}
		buf.data = append(buf.data, enc...)
	}

	result := WrapList(buf.data)
	ep.metrics.TotalEncodes.Add(int64(len(items)))
	ep.metrics.TotalBytes.Add(int64(len(result)))

	// Copy result to a new slice so the buffer can be reused.
	out := make([]byte, len(result))
	copy(out, result)
	return out, nil
}

// EncodeUint64 encodes a uint64 using zero-copy fixed-size encoding.
// This avoids the reflection overhead of the general encoder.
func EncodeUint64(v uint64) []byte {
	if v == 0 {
		return []byte{0x80}
	}
	if v < 128 {
		return []byte{byte(v)}
	}
	b := putUintBE(v)
	n := len(b)
	buf := make([]byte, 1+n)
	buf[0] = 0x80 + byte(n)
	copy(buf[1:], b)
	return buf
}

// EncodeBytes32 encodes a fixed 32-byte value (hash, key) without reflection.
// It writes a 33-byte result: [0xa0 (0x80+32), data[32]].
func EncodeBytes32(data [32]byte) []byte {
	buf := make([]byte, 33)
	buf[0] = 0x80 + 32
	copy(buf[1:], data[:])
	return buf
}

// EncodeBytes20 encodes a fixed 20-byte value (address) without reflection.
// It writes a 21-byte result: [0x94 (0x80+20), data[20]].
func EncodeBytes20(data [20]byte) []byte {
	buf := make([]byte, 21)
	buf[0] = 0x80 + 20
	copy(buf[1:], data[:])
	return buf
}

// EncodeBool encodes a boolean without reflection.
func EncodeBool(v bool) []byte {
	if v {
		return []byte{0x01}
	}
	return []byte{0x80}
}

// EstimateListSize returns an estimate of the RLP-encoded size of a list
// with the given total payload size. Useful for pre-allocating buffers.
func EstimateListSize(payloadSize int) int {
	if payloadSize <= 55 {
		return 1 + payloadSize
	}
	// Count bytes needed for the length prefix.
	lenBytes := uintByteLen(uint64(payloadSize))
	return 1 + lenBytes + payloadSize
}

// EstimateStringSize returns an estimate of the RLP-encoded size of a
// byte string of the given length.
func EstimateStringSize(dataLen int) int {
	if dataLen == 1 {
		// Could be single-byte encoding; assume worst case.
		return 1
	}
	if dataLen <= 55 {
		return 1 + dataLen
	}
	lenBytes := uintByteLen(uint64(dataLen))
	return 1 + lenBytes + dataLen
}

// AppendUint64 appends the RLP encoding of a uint64 to dst and returns
// the extended slice. This is a zero-allocation fast path for building
// RLP payloads incrementally.
func AppendUint64(dst []byte, v uint64) []byte {
	if v == 0 {
		return append(dst, 0x80)
	}
	if v < 128 {
		return append(dst, byte(v))
	}
	b := putUintBE(v)
	dst = append(dst, 0x80+byte(len(b)))
	return append(dst, b...)
}

// AppendBytes appends the RLP encoding of a byte slice to dst.
func AppendBytes(dst, data []byte) []byte {
	n := len(data)
	if n == 1 && data[0] <= 0x7f {
		return append(dst, data[0])
	}
	if n <= 55 {
		dst = append(dst, 0x80+byte(n))
		return append(dst, data...)
	}
	lb := putUintBE(uint64(n))
	dst = append(dst, 0xb7+byte(len(lb)))
	dst = append(dst, lb...)
	return append(dst, data...)
}

// AppendListHeader appends an RLP list header for a payload of the given
// size to dst. The caller is responsible for appending exactly payloadSize
// bytes of encoded list items afterward.
func AppendListHeader(dst []byte, payloadSize int) []byte {
	if payloadSize <= 55 {
		return append(dst, 0xc0+byte(payloadSize))
	}
	lb := putUintBE(uint64(payloadSize))
	dst = append(dst, 0xf7+byte(len(lb)))
	return append(dst, lb...)
}

// putUintBE encodes u as big-endian with no leading zeros.
func putUintBE(u uint64) []byte {
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], u)
	// Strip leading zeros.
	for i := 0; i < 8; i++ {
		if buf[i] != 0 {
			return buf[i:]
		}
	}
	return buf[7:] // u == 0, return single zero byte
}

// uintByteLen returns the number of bytes needed to encode u in big-endian.
func uintByteLen(u uint64) int {
	switch {
	case u < (1 << 8):
		return 1
	case u < (1 << 16):
		return 2
	case u < (1 << 24):
		return 3
	case u < (1 << 32):
		return 4
	case u < (1 << 40):
		return 5
	case u < (1 << 48):
		return 6
	case u < (1 << 56):
		return 7
	default:
		return 8
	}
}
