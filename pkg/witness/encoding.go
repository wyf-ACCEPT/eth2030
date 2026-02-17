package witness

import (
	"encoding/binary"
	"errors"
	"io"

	"github.com/eth2028/eth2028/core/types"
)

// Encoding errors.
var (
	ErrInvalidEncoding = errors.New("invalid witness encoding")
	ErrTruncatedData   = errors.New("truncated witness data")
)

// EncodeWitness serializes an ExecutionWitness to bytes.
// Format: [parentRoot(32)] [numStems(4)] [stem1] [stem2] ...
// Each stem: [stem(31)] [numSuffixes(2)] [suffix1] [suffix2] ...
// Each suffix: [index(1)] [hasOld(1)] [old(32)?] [hasNew(1)] [new(32)?]
func EncodeWitness(w *ExecutionWitness) ([]byte, error) {
	buf := make([]byte, 0, estimateSize(w))

	// Parent root
	buf = append(buf, w.ParentRoot[:]...)

	// Number of stems
	buf = binary.BigEndian.AppendUint32(buf, uint32(len(w.State)))

	for _, stem := range w.State {
		// Stem bytes (31)
		buf = append(buf, stem.Stem[:]...)

		// Number of suffixes
		buf = binary.BigEndian.AppendUint16(buf, uint16(len(stem.Suffixes)))

		for _, suffix := range stem.Suffixes {
			buf = append(buf, suffix.Suffix)

			if suffix.CurrentValue != nil {
				buf = append(buf, 1)
				buf = append(buf, suffix.CurrentValue[:]...)
			} else {
				buf = append(buf, 0)
			}

			if suffix.NewValue != nil {
				buf = append(buf, 1)
				buf = append(buf, suffix.NewValue[:]...)
			} else {
				buf = append(buf, 0)
			}
		}
	}
	return buf, nil
}

// DecodeWitness deserializes an ExecutionWitness from bytes.
func DecodeWitness(data []byte) (*ExecutionWitness, error) {
	if len(data) < 36 { // 32 (root) + 4 (numStems)
		return nil, ErrTruncatedData
	}

	w := &ExecutionWitness{}
	pos := 0

	// Parent root
	copy(w.ParentRoot[:], data[pos:pos+types.HashLength])
	pos += types.HashLength

	// Number of stems
	numStems := binary.BigEndian.Uint32(data[pos : pos+4])
	pos += 4

	w.State = make([]StemStateDiff, numStems)
	for i := uint32(0); i < numStems; i++ {
		if pos+31+2 > len(data) {
			return nil, ErrTruncatedData
		}

		// Stem
		copy(w.State[i].Stem[:], data[pos:pos+31])
		pos += 31

		// Number of suffixes
		numSuffixes := binary.BigEndian.Uint16(data[pos : pos+2])
		pos += 2

		w.State[i].Suffixes = make([]SuffixStateDiff, numSuffixes)
		for j := uint16(0); j < numSuffixes; j++ {
			if pos+1+1 > len(data) {
				return nil, ErrTruncatedData
			}

			w.State[i].Suffixes[j].Suffix = data[pos]
			pos++

			// Current value
			hasOld := data[pos]
			pos++
			if hasOld == 1 {
				if pos+32 > len(data) {
					return nil, ErrTruncatedData
				}
				val := new([32]byte)
				copy(val[:], data[pos:pos+32])
				w.State[i].Suffixes[j].CurrentValue = val
				pos += 32
			}

			// New value
			if pos+1 > len(data) {
				return nil, ErrTruncatedData
			}
			hasNew := data[pos]
			pos++
			if hasNew == 1 {
				if pos+32 > len(data) {
					return nil, ErrTruncatedData
				}
				val := new([32]byte)
				copy(val[:], data[pos:pos+32])
				w.State[i].Suffixes[j].NewValue = val
				pos += 32
			}
		}
	}
	return w, nil
}

// estimateSize estimates the encoded size of a witness.
func estimateSize(w *ExecutionWitness) int {
	size := 32 + 4 // parent root + num stems
	for _, stem := range w.State {
		size += 31 + 2 // stem + num suffixes
		for _, suffix := range stem.Suffixes {
			size += 1 + 1 + 1 // suffix byte + hasOld + hasNew
			if suffix.CurrentValue != nil {
				size += 32
			}
			if suffix.NewValue != nil {
				size += 32
			}
		}
	}
	return size
}

// WriteTo writes the encoded witness to a writer.
func WriteTo(w *ExecutionWitness, writer io.Writer) error {
	data, err := EncodeWitness(w)
	if err != nil {
		return err
	}
	_, err = writer.Write(data)
	return err
}
