package core

import (
	"encoding/binary"
	"errors"
	"fmt"
)

const (
	// FieldElementSize is the size of a BLS field element in bytes.
	FieldElementSize = 32
	// UsableBytesPerElement is the number of usable bytes per field element.
	// The high byte must be 0 for BLS field modulus compliance.
	UsableBytesPerElement = 31
	// ElementsPerBlob is the number of field elements in a single blob.
	ElementsPerBlob = 4096
	// UsableBytesPerBlob is the usable payload capacity of a single blob.
	UsableBytesPerBlob = UsableBytesPerElement * ElementsPerBlob // 126,976
	// BlobSize is the total size of a blob in bytes (4096 * 32).
	BlobSize = ElementsPerBlob * FieldElementSize // 131,072
	// BlockLengthPrefixSize is the byte length of the block RLP length prefix.
	BlockLengthPrefixSize = 4
)

// BlobEncoder handles encoding/decoding of block RLP data into blob format.
type BlobEncoder struct{}

// EncodeBlockToBlobs encodes RLP-encoded block data into one or more blobs.
// Each blob is 131,072 bytes (4096 field elements of 32 bytes each).
// Data is packed into the low 31 bytes of each field element with the high byte set to 0.
func (e *BlobEncoder) EncodeBlockToBlobs(blockRLP []byte) ([][]byte, error) {
	if len(blockRLP) > 0xFFFFFFFF {
		return nil, errors.New("block RLP too large")
	}

	// Prepend 4-byte big-endian length prefix.
	prefixed := make([]byte, BlockLengthPrefixSize+len(blockRLP))
	binary.BigEndian.PutUint32(prefixed[:BlockLengthPrefixSize], uint32(len(blockRLP)))
	copy(prefixed[BlockLengthPrefixSize:], blockRLP)

	numBlobs := CalcBlobsRequired(len(blockRLP))
	blobs := make([][]byte, numBlobs)

	srcOff := 0
	for bi := range numBlobs {
		blob := make([]byte, BlobSize)
		for ei := range ElementsPerBlob {
			// High byte (blob[ei*FieldElementSize]) stays 0.
			elemStart := ei*FieldElementSize + 1 // skip high byte
			remaining := len(prefixed) - srcOff
			toCopy := UsableBytesPerElement
			if remaining < toCopy {
				toCopy = remaining
			}
			if toCopy > 0 {
				copy(blob[elemStart:elemStart+toCopy], prefixed[srcOff:srcOff+toCopy])
				srcOff += toCopy
			}
		}
		blobs[bi] = blob
	}

	return blobs, nil
}

// DecodeBlobsToBlock decodes one or more blobs back into block RLP data.
func (e *BlobEncoder) DecodeBlobsToBlock(blobs [][]byte) ([]byte, error) {
	if len(blobs) == 0 {
		return nil, errors.New("no blobs provided")
	}

	for i, blob := range blobs {
		if len(blob) != BlobSize {
			return nil, fmt.Errorf("blob %d: expected %d bytes, got %d", i, BlobSize, len(blob))
		}
	}

	// Extract all usable bytes from field elements across all blobs.
	totalUsable := len(blobs) * UsableBytesPerBlob
	raw := make([]byte, 0, totalUsable)

	for _, blob := range blobs {
		for ei := range ElementsPerBlob {
			elemStart := ei*FieldElementSize + 1 // skip high byte
			raw = append(raw, blob[elemStart:elemStart+UsableBytesPerElement]...)
		}
	}

	if len(raw) < BlockLengthPrefixSize {
		return nil, errors.New("decoded data too short for length prefix")
	}

	// Read length prefix and extract block RLP.
	blockLen := binary.BigEndian.Uint32(raw[:BlockLengthPrefixSize])
	dataStart := BlockLengthPrefixSize
	dataEnd := dataStart + int(blockLen)

	if dataEnd > len(raw) {
		return nil, fmt.Errorf("block length %d exceeds available data %d", blockLen, len(raw)-dataStart)
	}

	result := make([]byte, blockLen)
	copy(result, raw[dataStart:dataEnd])
	return result, nil
}

// ValidateBlockInBlobs checks that a set of blobs intended to encode a block are well-formed:
//   - There must be at least one blob
//   - Each blob must be exactly BlobSize bytes
//   - The high byte of each field element must be zero (BLS field compliance)
func ValidateBlockInBlobs(blobs [][]byte) error {
	if len(blobs) == 0 {
		return errors.New("block_in_blobs: no blobs provided")
	}
	for i, blob := range blobs {
		if len(blob) != BlobSize {
			return fmt.Errorf("block_in_blobs: blob %d has size %d, want %d", i, len(blob), BlobSize)
		}
		for ei := 0; ei < ElementsPerBlob; ei++ {
			if blob[ei*FieldElementSize] != 0 {
				return fmt.Errorf("block_in_blobs: blob %d element %d high byte is non-zero", i, ei)
			}
		}
	}
	return nil
}

// CalcBlobsRequired returns the number of blobs needed to store a block of the given size.
func CalcBlobsRequired(blockSize int) int {
	// Account for 4-byte length prefix.
	totalBytes := blockSize + BlockLengthPrefixSize
	n := (totalBytes + UsableBytesPerBlob - 1) / UsableBytesPerBlob
	if n == 0 {
		n = 1
	}
	return n
}
