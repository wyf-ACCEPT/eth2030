package types

import (
	"crypto/sha256"
	"errors"
	"fmt"
)

// Request types defined by EIP-7685.
// Each request type is identified by a single byte prefix.
const (
	DepositRequestType    byte = 0x00 // EIP-6110: supply validator deposits
	WithdrawalRequestType byte = 0x01 // EIP-7002: trigger validator withdrawals
	ConsolidationRequestType byte = 0x02 // EIP-7251: validator consolidations
)

// Request represents a typed execution layer request per EIP-7685.
// Format: request_type || request_data
type Request struct {
	Type byte
	Data []byte
}

// NewRequest creates a new request with the given type and data.
func NewRequest(reqType byte, data []byte) *Request {
	return &Request{Type: reqType, Data: data}
}

// Encode serializes the request to its wire format: type || data.
func (r *Request) Encode() []byte {
	out := make([]byte, 1+len(r.Data))
	out[0] = r.Type
	copy(out[1:], r.Data)
	return out
}

// DecodeRequest deserializes a request from its wire format.
func DecodeRequest(data []byte) (*Request, error) {
	if len(data) < 1 {
		return nil, errors.New("request too short")
	}
	return &Request{
		Type: data[0],
		Data: data[1:],
	}, nil
}

// Requests is a list of EL requests.
type Requests []*Request

// FilterByType returns all requests of the given type.
func (rs Requests) FilterByType(reqType byte) Requests {
	var result Requests
	for _, r := range rs {
		if r.Type == reqType {
			result = append(result, r)
		}
	}
	return result
}

// Encode serializes all requests to their wire formats.
func (rs Requests) Encode() [][]byte {
	result := make([][]byte, len(rs))
	for i, r := range rs {
		result[i] = r.Encode()
	}
	return result
}

// ComputeRequestsHash computes the SHA-256 commitment over all requests.
// Per EIP-7685: sha256(sha256(requests_0) ++ sha256(requests_1) ++ ...)
// where each requests_i is the concatenation of all request data of type i.
func ComputeRequestsHash(requests Requests) Hash {
	// Group requests by type, maintaining order.
	byType := make(map[byte][][]byte)
	for _, r := range requests {
		byType[r.Type] = append(byType[r.Type], r.Data)
	}

	// Compute the flat hash: sha256 of all type-prefixed request hashes.
	h := sha256.New()
	for i := byte(0); i <= 2; i++ {
		datas, ok := byType[i]
		if !ok {
			continue
		}
		// Concatenate all request data of this type.
		typeH := sha256.New()
		typeH.Write([]byte{i})
		for _, d := range datas {
			typeH.Write(d)
		}
		h.Write(typeH.Sum(nil))
	}

	var result Hash
	copy(result[:], h.Sum(nil))
	return result
}

// ValidateRequestsHash verifies the requests_hash field in a block header.
func ValidateRequestsHash(header *Header, requests Requests) error {
	if header.RequestsHash == nil {
		if len(requests) == 0 {
			return nil
		}
		return fmt.Errorf("header has no requests_hash but block has %d requests", len(requests))
	}
	computed := ComputeRequestsHash(requests)
	if *header.RequestsHash != computed {
		return fmt.Errorf("requests hash mismatch: header=%s computed=%s", header.RequestsHash.Hex(), computed.Hex())
	}
	return nil
}
