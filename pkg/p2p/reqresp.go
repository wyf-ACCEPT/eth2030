package p2p

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// ReqRespConfig configures the request-response protocol.
type ReqRespConfig struct {
	// MaxRequestSize is the maximum allowed request payload size in bytes.
	MaxRequestSize int
	// ResponseTimeout is the maximum time to wait for a response.
	ResponseTimeout time.Duration
	// MaxConcurrent is the maximum number of concurrent pending requests.
	MaxConcurrent int
}

// DefaultReqRespConfig returns the default request-response configuration.
func DefaultReqRespConfig() ReqRespConfig {
	return ReqRespConfig{
		MaxRequestSize:  1 << 20, // 1 MiB
		ResponseTimeout: 10 * time.Second,
		MaxConcurrent:   64,
	}
}

// RequestID is a unique identifier for a request.
type RequestID = uint64

// Request represents an outgoing or incoming request.
type Request struct {
	ID        RequestID
	Method    string
	Payload   []byte
	Timestamp time.Time
}

// Response represents a response to a request.
type Response struct {
	ID        RequestID
	Method    string
	Payload   []byte
	Error     string
	Timestamp time.Time
}

var (
	// ErrRequestTooLarge is returned when a request exceeds the configured max size.
	ErrRequestTooLarge = errors.New("reqresp: request too large")

	// ErrInvalidEncoding is returned when a message cannot be decoded.
	ErrInvalidEncoding = errors.New("reqresp: invalid encoding")

	// ErrMethodTooLong is returned when the method string exceeds 65535 bytes.
	ErrMethodTooLong = errors.New("reqresp: method name too long")
)

// Wire format:
//   method_len[2] || method[method_len] || id[8] || payload_len[4] || payload[payload_len]
// For responses, an additional error field is appended:
//   method_len[2] || method[method_len] || id[8] || payload_len[4] || payload[payload_len] || err_len[2] || err[err_len]

// ReqRespCodec manages encoding, decoding, and tracking of request-response pairs.
type ReqRespCodec struct {
	config  ReqRespConfig
	nextID  atomic.Uint64
	mu      sync.Mutex
	pending map[RequestID]time.Time
}

// NewReqRespCodec creates a new codec with the given configuration.
func NewReqRespCodec(config ReqRespConfig) *ReqRespCodec {
	return &ReqRespCodec{
		config:  config,
		pending: make(map[RequestID]time.Time),
	}
}

// EncodeRequest encodes a request with the given method and payload.
// Returns the request struct and its wire-format encoding.
func (c *ReqRespCodec) EncodeRequest(method string, payload []byte) (*Request, []byte, error) {
	if len(method) > 0xFFFF {
		return nil, nil, ErrMethodTooLong
	}
	if len(payload) > c.config.MaxRequestSize {
		return nil, nil, ErrRequestTooLarge
	}

	id := c.nextID.Add(1)
	now := time.Now()

	req := &Request{
		ID:        id,
		Method:    method,
		Payload:   payload,
		Timestamp: now,
	}

	data := encodeRequestWire(id, method, payload)

	c.mu.Lock()
	c.pending[id] = now
	c.mu.Unlock()

	return req, data, nil
}

// DecodeRequest decodes a wire-format request.
func (c *ReqRespCodec) DecodeRequest(data []byte) (*Request, error) {
	if len(data) < 2 {
		return nil, fmt.Errorf("%w: too short for method length", ErrInvalidEncoding)
	}

	methodLen := int(binary.BigEndian.Uint16(data[0:2]))
	offset := 2

	if offset+methodLen > len(data) {
		return nil, fmt.Errorf("%w: truncated method", ErrInvalidEncoding)
	}
	method := string(data[offset : offset+methodLen])
	offset += methodLen

	// ID: 8 bytes
	if offset+8 > len(data) {
		return nil, fmt.Errorf("%w: truncated request ID", ErrInvalidEncoding)
	}
	id := binary.BigEndian.Uint64(data[offset : offset+8])
	offset += 8

	// Payload length: 4 bytes
	if offset+4 > len(data) {
		return nil, fmt.Errorf("%w: truncated payload length", ErrInvalidEncoding)
	}
	payloadLen := int(binary.BigEndian.Uint32(data[offset : offset+4]))
	offset += 4

	if offset+payloadLen > len(data) {
		return nil, fmt.Errorf("%w: truncated payload", ErrInvalidEncoding)
	}
	payload := make([]byte, payloadLen)
	copy(payload, data[offset:offset+payloadLen])

	return &Request{
		ID:        id,
		Method:    method,
		Payload:   payload,
		Timestamp: time.Now(),
	}, nil
}

// EncodeResponse encodes a response for the given request ID.
func (c *ReqRespCodec) EncodeResponse(reqID RequestID, method string, payload []byte, errMsg string) ([]byte, error) {
	if len(method) > 0xFFFF {
		return nil, ErrMethodTooLong
	}

	// Remove from pending.
	c.mu.Lock()
	delete(c.pending, reqID)
	c.mu.Unlock()

	data := encodeResponseWire(reqID, method, payload, errMsg)
	return data, nil
}

// DecodeResponse decodes a wire-format response.
func (c *ReqRespCodec) DecodeResponse(data []byte) (*Response, error) {
	if len(data) < 2 {
		return nil, fmt.Errorf("%w: too short for method length", ErrInvalidEncoding)
	}

	methodLen := int(binary.BigEndian.Uint16(data[0:2]))
	offset := 2

	if offset+methodLen > len(data) {
		return nil, fmt.Errorf("%w: truncated method", ErrInvalidEncoding)
	}
	method := string(data[offset : offset+methodLen])
	offset += methodLen

	// ID: 8 bytes
	if offset+8 > len(data) {
		return nil, fmt.Errorf("%w: truncated response ID", ErrInvalidEncoding)
	}
	id := binary.BigEndian.Uint64(data[offset : offset+8])
	offset += 8

	// Payload length: 4 bytes
	if offset+4 > len(data) {
		return nil, fmt.Errorf("%w: truncated payload length", ErrInvalidEncoding)
	}
	payloadLen := int(binary.BigEndian.Uint32(data[offset : offset+4]))
	offset += 4

	if offset+payloadLen > len(data) {
		return nil, fmt.Errorf("%w: truncated payload", ErrInvalidEncoding)
	}
	payload := make([]byte, payloadLen)
	copy(payload, data[offset:offset+payloadLen])
	offset += payloadLen

	// Error length: 2 bytes
	if offset+2 > len(data) {
		return nil, fmt.Errorf("%w: truncated error length", ErrInvalidEncoding)
	}
	errLen := int(binary.BigEndian.Uint16(data[offset : offset+2]))
	offset += 2

	if offset+errLen > len(data) {
		return nil, fmt.Errorf("%w: truncated error message", ErrInvalidEncoding)
	}
	errMsg := string(data[offset : offset+errLen])

	// Remove from pending.
	c.mu.Lock()
	delete(c.pending, id)
	c.mu.Unlock()

	return &Response{
		ID:        id,
		Method:    method,
		Payload:   payload,
		Error:     errMsg,
		Timestamp: time.Now(),
	}, nil
}

// PendingRequests returns the number of currently pending requests.
func (c *ReqRespCodec) PendingRequests() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.pending)
}

// encodeRequestWire builds the wire format for a request.
func encodeRequestWire(id uint64, method string, payload []byte) []byte {
	methodBytes := []byte(method)
	// 2 (method_len) + method + 8 (id) + 4 (payload_len) + payload
	size := 2 + len(methodBytes) + 8 + 4 + len(payload)
	buf := make([]byte, size)
	offset := 0

	binary.BigEndian.PutUint16(buf[offset:], uint16(len(methodBytes)))
	offset += 2

	copy(buf[offset:], methodBytes)
	offset += len(methodBytes)

	binary.BigEndian.PutUint64(buf[offset:], id)
	offset += 8

	binary.BigEndian.PutUint32(buf[offset:], uint32(len(payload)))
	offset += 4

	copy(buf[offset:], payload)
	return buf
}

// encodeResponseWire builds the wire format for a response.
func encodeResponseWire(id uint64, method string, payload []byte, errMsg string) []byte {
	methodBytes := []byte(method)
	errBytes := []byte(errMsg)
	// 2 (method_len) + method + 8 (id) + 4 (payload_len) + payload + 2 (err_len) + err
	size := 2 + len(methodBytes) + 8 + 4 + len(payload) + 2 + len(errBytes)
	buf := make([]byte, size)
	offset := 0

	binary.BigEndian.PutUint16(buf[offset:], uint16(len(methodBytes)))
	offset += 2

	copy(buf[offset:], methodBytes)
	offset += len(methodBytes)

	binary.BigEndian.PutUint64(buf[offset:], id)
	offset += 8

	binary.BigEndian.PutUint32(buf[offset:], uint32(len(payload)))
	offset += 4

	copy(buf[offset:], payload)
	offset += len(payload)

	binary.BigEndian.PutUint16(buf[offset:], uint16(len(errBytes)))
	offset += 2

	copy(buf[offset:], errBytes)
	return buf
}
