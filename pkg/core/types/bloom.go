package types

import (
	"encoding/binary"

	"golang.org/x/crypto/sha3"
)

// BloomBitLength is the number of bits in a bloom filter (2048).
const BloomBitLength = 8 * BloomLength

// bloom9 computes the 3 bit positions for a bloom filter entry.
// It takes the first 6 bytes of keccak256(data), splits them into 3 pairs
// of 2 bytes each, and interprets each pair as a big-endian uint16 mod 2048.
func bloom9(data []byte) [3]uint {
	d := sha3.NewLegacyKeccak256()
	d.Write(data)
	h := d.Sum(nil)
	var bits [3]uint
	for i := 0; i < 3; i++ {
		bits[i] = uint(binary.BigEndian.Uint16(h[2*i:])) & 0x7FF // mod 2048
	}
	return bits
}

// BloomAdd sets the 3 bloom bits derived from data in the bloom filter.
func BloomAdd(bloom *Bloom, data []byte) {
	bits := bloom9(data)
	for _, bit := range bits {
		// bit is 0..2047; byte index and bit position within the 256-byte array.
		// Ethereum bloom uses big-endian bit ordering: bit 0 is the MSB of byte 0.
		byteIdx := BloomLength - 1 - bit/8
		bitIdx := bit % 8
		bloom[byteIdx] |= 1 << bitIdx
	}
}

// LogsBloom computes the bloom filter for a set of logs.
// For each log, it adds the log address and each topic to the bloom.
func LogsBloom(logs []*Log) Bloom {
	var bloom Bloom
	for _, log := range logs {
		BloomAdd(&bloom, log.Address.Bytes())
		for _, topic := range log.Topics {
			BloomAdd(&bloom, topic.Bytes())
		}
	}
	return bloom
}

// BloomContains checks whether the bloom filter contains the given data.
// It returns true if all 3 bits corresponding to the data are set.
func BloomContains(bloom Bloom, data []byte) bool {
	bits := bloom9(data)
	for _, bit := range bits {
		byteIdx := BloomLength - 1 - bit/8
		bitIdx := bit % 8
		if bloom[byteIdx]&(1<<bitIdx) == 0 {
			return false
		}
	}
	return true
}

// CreateBloom computes the combined bloom filter for a list of receipts
// by OR-ing together the bloom from each receipt's logs.
func CreateBloom(receipts []*Receipt) Bloom {
	var bloom Bloom
	for _, receipt := range receipts {
		for i := range receipt.Bloom {
			bloom[i] |= receipt.Bloom[i]
		}
	}
	return bloom
}
