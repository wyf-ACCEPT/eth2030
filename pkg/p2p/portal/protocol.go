// Package portal implements the Portal Network wire protocol for lightweight
// access to historical Ethereum data. It defines content key types, message
// types, content ID computation (SHA-256), and the XOR distance metric used
// for content-addressed routing in the Portal DHT overlay.
//
// Reference: https://github.com/ethereum/portal-network-specs
package portal

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"math/big"

	"github.com/eth2028/eth2028/core/types"
)

// Protocol identifiers for Portal sub-protocols.
const (
	HistoryNetworkID = uint16(0x500B) // History network
	StateNetworkID   = uint16(0x500A) // State network
	BeaconNetworkID  = uint16(0x500C) // Beacon chain network
)

// Content key type selectors.
const (
	ContentKeyBlockHeader      byte = 0x00
	ContentKeyBlockBody        byte = 0x01
	ContentKeyReceipt          byte = 0x02
	ContentKeyEpochAccumulator byte = 0x03
)

// Portal wire protocol message type codes.
const (
	MsgPing        byte = 0x01
	MsgPong        byte = 0x02
	MsgFindNodes   byte = 0x03
	MsgNodes       byte = 0x04
	MsgFindContent byte = 0x05
	MsgContent     byte = 0x06
	MsgOffer       byte = 0x07
	MsgAccept      byte = 0x08
)

// Content response type selectors (Content message sub-types).
const (
	ContentConnectionID byte = 0x00 // uTP connection ID for large content
	ContentRawPayload   byte = 0x01 // inline content payload
	ContentENRs         byte = 0x02 // ENR list of closer nodes
)

// Errors returned by protocol operations.
var (
	ErrInvalidContentKey = errors.New("portal: invalid content key")
	ErrUnknownKeyType    = errors.New("portal: unknown content key type")
	ErrContentNotFound   = errors.New("portal: content not found")
	ErrEmptyPayload      = errors.New("portal: empty payload")
)

// ContentID is a 32-byte identifier derived from a content key via SHA-256.
// It determines where content lives in the DHT key space.
type ContentID [32]byte

// Bytes returns the content ID as a byte slice.
func (c ContentID) Bytes() []byte { return c[:] }

// IsZero reports whether the content ID is all zeros.
func (c ContentID) IsZero() bool { return c == ContentID{} }

// BlockHeaderKey identifies a block header by its block hash.
type BlockHeaderKey struct {
	BlockHash types.Hash
}

// Encode serializes the content key with its type selector prefix.
func (k BlockHeaderKey) Encode() []byte {
	buf := make([]byte, 1+types.HashLength)
	buf[0] = ContentKeyBlockHeader
	copy(buf[1:], k.BlockHash[:])
	return buf
}

// BlockBodyKey identifies a block body by its block hash.
type BlockBodyKey struct {
	BlockHash types.Hash
}

// Encode serializes the content key with its type selector prefix.
func (k BlockBodyKey) Encode() []byte {
	buf := make([]byte, 1+types.HashLength)
	buf[0] = ContentKeyBlockBody
	copy(buf[1:], k.BlockHash[:])
	return buf
}

// ReceiptKey identifies block receipts by block hash.
type ReceiptKey struct {
	BlockHash types.Hash
}

// Encode serializes the content key with its type selector prefix.
func (k ReceiptKey) Encode() []byte {
	buf := make([]byte, 1+types.HashLength)
	buf[0] = ContentKeyReceipt
	copy(buf[1:], k.BlockHash[:])
	return buf
}

// EpochAccumulatorKey identifies an epoch accumulator by epoch index.
type EpochAccumulatorKey struct {
	EpochHash types.Hash
}

// Encode serializes the content key with its type selector prefix.
func (k EpochAccumulatorKey) Encode() []byte {
	buf := make([]byte, 1+types.HashLength)
	buf[0] = ContentKeyEpochAccumulator
	copy(buf[1:], k.EpochHash[:])
	return buf
}

// ContentKeyEncoder is implemented by all content key types.
type ContentKeyEncoder interface {
	Encode() []byte
}

// DecodeContentKey parses raw bytes into a typed content key.
// The first byte is the type selector; the remaining 32 bytes are the hash.
func DecodeContentKey(data []byte) (ContentKeyEncoder, error) {
	if len(data) < 1+types.HashLength {
		return nil, ErrInvalidContentKey
	}
	var h types.Hash
	copy(h[:], data[1:1+types.HashLength])

	switch data[0] {
	case ContentKeyBlockHeader:
		return BlockHeaderKey{BlockHash: h}, nil
	case ContentKeyBlockBody:
		return BlockBodyKey{BlockHash: h}, nil
	case ContentKeyReceipt:
		return ReceiptKey{BlockHash: h}, nil
	case ContentKeyEpochAccumulator:
		return EpochAccumulatorKey{EpochHash: h}, nil
	default:
		return nil, ErrUnknownKeyType
	}
}

// ComputeContentID derives the content ID from an encoded content key.
// Content ID = SHA-256(content_key).
func ComputeContentID(contentKey []byte) ContentID {
	return sha256.Sum256(contentKey)
}

// Distance computes the XOR distance between two 32-byte identifiers.
// This is the same metric used in Discovery V5 / Kademlia.
func Distance(a, b [32]byte) *big.Int {
	result := new(big.Int)
	for i := 0; i < 32; i++ {
		result.SetBit(result, (31-i)*8+7, uint(((a[i]^b[i])>>7)&1))
		result.SetBit(result, (31-i)*8+6, uint(((a[i]^b[i])>>6)&1))
		result.SetBit(result, (31-i)*8+5, uint(((a[i]^b[i])>>5)&1))
		result.SetBit(result, (31-i)*8+4, uint(((a[i]^b[i])>>4)&1))
		result.SetBit(result, (31-i)*8+3, uint(((a[i]^b[i])>>3)&1))
		result.SetBit(result, (31-i)*8+2, uint(((a[i]^b[i])>>2)&1))
		result.SetBit(result, (31-i)*8+1, uint(((a[i]^b[i])>>1)&1))
		result.SetBit(result, (31-i)*8+0, uint((a[i]^b[i])&1))
	}
	return result
}

// XORBytes computes the byte-wise XOR of two 32-byte arrays.
func XORBytes(a, b [32]byte) [32]byte {
	var out [32]byte
	for i := 0; i < 32; i++ {
		out[i] = a[i] ^ b[i]
	}
	return out
}

// LogDistance returns log2(XOR(a, b)), the number of bits in the XOR distance.
// Returns 0 if a == b.
func LogDistance(a, b [32]byte) int {
	d := Distance(a, b)
	if d.Sign() == 0 {
		return 0
	}
	return d.BitLen()
}

// NodeRadius represents the advertised content radius for a portal peer.
// A peer stores content whose distance from its node ID is less than or
// equal to its radius. MaxRadius means the node stores everything.
type NodeRadius struct {
	// Raw is the 256-bit radius value. A node accepts content with
	// distance(nodeID, contentID) <= Raw.
	Raw *big.Int
}

// MaxRadius returns a NodeRadius that covers the entire key space.
func MaxRadius() NodeRadius {
	max := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
	return NodeRadius{Raw: max}
}

// ZeroRadius returns a NodeRadius that covers nothing.
func ZeroRadius() NodeRadius {
	return NodeRadius{Raw: new(big.Int)}
}

// Contains reports whether contentID falls within this radius relative to nodeID.
func (r NodeRadius) Contains(nodeID, contentID [32]byte) bool {
	dist := Distance(nodeID, contentID)
	return dist.Cmp(r.Raw) <= 0
}

// --- Portal wire protocol message types ---

// Ping is sent to check liveness and exchange radius information.
type Ping struct {
	RequestID    uint64
	ENRSeq       uint64 // local ENR sequence number
	CustomPayload []byte // SSZ-encoded radius
}

// Pong is the response to Ping.
type Pong struct {
	RequestID    uint64
	ENRSeq       uint64
	CustomPayload []byte // SSZ-encoded radius
}

// FindNodes requests nodes at specified log distances.
type FindNodes struct {
	RequestID uint64
	Distances []uint16
}

// Nodes is the response to FindNodes.
type Nodes struct {
	RequestID uint64
	Total     uint8
	ENRs      [][]byte
}

// FindContent requests content by its content key.
type FindContent struct {
	RequestID  uint64
	ContentKey []byte
}

// Content is the response to FindContent.
type Content struct {
	RequestID uint64
	// Type indicates the response variant:
	//   ContentConnectionID - payload is a uTP connection ID
	//   ContentRawPayload   - payload is the raw content
	//   ContentENRs         - payload is a list of closer node ENRs
	Type    byte
	Payload []byte
}

// Offer proactively pushes content keys to a peer.
type Offer struct {
	RequestID   uint64
	ContentKeys [][]byte
}

// Accept is the response to Offer, indicating which content keys are wanted.
type Accept struct {
	RequestID    uint64
	ConnectionID uint16
	AcceptBits   []byte // bitfield: bit i = 1 means content key i is accepted
}

// EncodeRadius encodes a NodeRadius as a 32-byte big-endian value
// suitable for Ping/Pong custom payload.
func EncodeRadius(r NodeRadius) []byte {
	b := r.Raw.Bytes()
	if len(b) > 32 {
		b = b[len(b)-32:]
	}
	buf := make([]byte, 32)
	copy(buf[32-len(b):], b)
	return buf
}

// DecodeRadius decodes a 32-byte big-endian radius from Ping/Pong payload.
func DecodeRadius(data []byte) NodeRadius {
	if len(data) == 0 {
		return ZeroRadius()
	}
	return NodeRadius{Raw: new(big.Int).SetBytes(data)}
}

// EncodeUint16 encodes a uint16 in little-endian for connection IDs.
func EncodeUint16(v uint16) []byte {
	buf := make([]byte, 2)
	binary.LittleEndian.PutUint16(buf, v)
	return buf
}

// DecodeUint16 decodes a little-endian uint16.
func DecodeUint16(data []byte) uint16 {
	if len(data) < 2 {
		return 0
	}
	return binary.LittleEndian.Uint16(data)
}
