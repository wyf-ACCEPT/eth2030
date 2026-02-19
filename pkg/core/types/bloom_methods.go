package types

import (
	"encoding/binary"

	"golang.org/x/crypto/sha3"
)

// BloomByteLength is the number of bytes in a bloom filter (256).
const BloomByteLength = BloomLength

// bloomBitLength is the number of bits in a bloom filter (2048).
const bloomBitLength = BloomByteLength * 8

// BytesToBloom converts a byte slice to a Bloom, left-truncating or
// right-padding as necessary to fill exactly 256 bytes.
func BytesToBloom(b []byte) Bloom {
	var bloom Bloom
	bloom.SetBytes(b)
	return bloom
}

// Bytes returns a copy of the bloom filter as a byte slice.
func (b Bloom) Bytes() []byte {
	out := make([]byte, BloomByteLength)
	copy(out, b[:])
	return out
}

// SetBytes sets the bloom filter from a byte slice, left-padding if shorter
// than 256 bytes or truncating from the left if longer.
func (b *Bloom) SetBytes(data []byte) {
	*b = Bloom{}
	if len(data) > BloomByteLength {
		data = data[len(data)-BloomByteLength:]
	}
	copy(b[BloomByteLength-len(data):], data)
}

// Add inserts data into the bloom filter by setting 3 bit positions
// derived from Keccak256(data).
func (b *Bloom) Add(data []byte) {
	BloomAdd(b, data)
}

// Test checks whether data might be present in the bloom filter.
// Returns true if all 3 bits for the data are set (may be a false positive).
func (b Bloom) Test(data []byte) bool {
	return BloomContains(b, data)
}

// Or performs a bitwise OR of the receiver with another bloom filter,
// storing the result in the receiver.
func (b *Bloom) Or(other Bloom) {
	for i := range b {
		b[i] |= other[i]
	}
}

// bloomBits computes 3 (byteIndex, bitMask) pairs from the first 6 bytes
// of Keccak256(data). Each pair indicates which bit to set/test in the
// 256-byte bloom array. The returned [3][2]uint contains, for each of the
// 3 positions: [0] = the byte index in the 256-byte array, [1] = the bit
// position within that byte (0..7).
func bloomBits(data []byte) [3][2]uint {
	d := sha3.NewLegacyKeccak256()
	d.Write(data)
	h := d.Sum(nil)

	var result [3][2]uint
	for i := 0; i < 3; i++ {
		bit := uint(binary.BigEndian.Uint16(h[2*i:])) & 0x7FF // mod 2048
		result[i][0] = BloomByteLength - 1 - bit/8             // byte index
		result[i][1] = bit % 8                                  // bit position
	}
	return result
}
