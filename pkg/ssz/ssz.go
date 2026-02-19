// Package ssz implements Simple Serialize (SSZ), the serialization format
// used by the Ethereum consensus layer. SSZ provides deterministic encoding,
// efficient Merkleization, and support for both fixed-size and variable-size
// types.
//
// Spec: https://github.com/ethereum/consensus-specs/blob/dev/ssz/simple-serialize.md
package ssz

import "errors"

// Common errors.
var (
	ErrSize          = errors.New("ssz: invalid size")
	ErrOffset        = errors.New("ssz: invalid offset")
	ErrListTooLong   = errors.New("ssz: list exceeds maximum length")
	ErrBufferTooSmall = errors.New("ssz: buffer too small")
	ErrInvalidBool   = errors.New("ssz: invalid boolean value")
)

// BytesPerLengthOffset is the number of bytes used for each offset in
// variable-length SSZ containers (4 bytes, little-endian uint32).
const BytesPerLengthOffset = 4

// Marshaler is implemented by types that can serialize themselves to SSZ.
type Marshaler interface {
	MarshalSSZ() ([]byte, error)
	SizeSSZ() int
}

// Unmarshaler is implemented by types that can deserialize themselves from SSZ.
type Unmarshaler interface {
	UnmarshalSSZ([]byte) error
}

// HashRoot is implemented by types that can compute their SSZ hash tree root.
type HashRoot interface {
	HashTreeRoot() ([32]byte, error)
}
