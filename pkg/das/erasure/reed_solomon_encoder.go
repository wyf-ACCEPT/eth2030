// reed_solomon_encoder.go implements a Reed-Solomon encoder/decoder over
// GF(2^8) using the Galois field primitives from galois_field.go. The encoder
// uses an evaluation-based scheme: data bytes form polynomial coefficients,
// and each shard (data and parity alike) is an evaluation of that polynomial
// at a distinct non-zero GF(2^8) element.
//
// Key properties:
//   - Any k-of-n shards suffice to reconstruct the original data (MDS code)
//   - Reconstruction uses Lagrange interpolation over GF(2^8)
//   - Shard integrity is validated via polynomial consistency checks
//
// Reference: Reed & Solomon (1960), "Polynomial Codes over Certain Finite Fields"
package erasure

import (
	"errors"
	"fmt"
)

// Reed-Solomon encoder errors.
var (
	ErrRSEncInvalidConfig   = errors.New("erasure/rs-enc: invalid shard configuration")
	ErrRSEncDataTooLarge    = errors.New("erasure/rs-enc: data exceeds maximum encodable size")
	ErrRSEncEmptyInput      = errors.New("erasure/rs-enc: empty input data")
	ErrRSEncShardCount      = errors.New("erasure/rs-enc: shard count mismatch")
	ErrRSEncShardSize       = errors.New("erasure/rs-enc: shard sizes not uniform")
	ErrRSEncTooFewShards    = errors.New("erasure/rs-enc: insufficient shards for reconstruction")
	ErrRSEncCorruptedShard  = errors.New("erasure/rs-enc: shard data is corrupted")
	ErrRSEncMaxShardsExceed = errors.New("erasure/rs-enc: total shards exceed GF(2^8) field order")
)

// MaxGF256Shards is the maximum number of total shards for GF(2^8).
// Each shard needs a unique evaluation point, and GF(2^8) has 255 non-zero
// elements, so we cap at 255.
const MaxGF256Shards = 255

// RSEncoderGF256 implements Reed-Solomon encoding over GF(2^8). It uses
// polynomial evaluation for encoding and Lagrange interpolation for
// reconstruction. Every shard (data and parity) is a polynomial evaluation,
// ensuring any k-of-n shards can reconstruct the original data.
type RSEncoderGF256 struct {
	// dataShards is the number of original data shards (k).
	dataShards int

	// parityShards is the number of parity shards (n - k).
	parityShards int

	// totalShards is dataShards + parityShards.
	totalShards int

	// evalPoints are the evaluation points: g^0, g^1, ..., g^{totalShards-1}
	// where g is the primitive element of GF(2^8).
	evalPoints []GF256
}

// NewRSEncoderGF256 creates a new Reed-Solomon encoder over GF(2^8).
// dataShards (k) is the number of data shards; parityShards (m) is the
// number of parity shards. The total k + m must not exceed MaxGF256Shards.
func NewRSEncoderGF256(dataShards, parityShards int) (*RSEncoderGF256, error) {
	if dataShards <= 0 || parityShards <= 0 {
		return nil, fmt.Errorf("%w: dataShards=%d, parityShards=%d",
			ErrRSEncInvalidConfig, dataShards, parityShards)
	}
	total := dataShards + parityShards
	if total > MaxGF256Shards {
		return nil, fmt.Errorf("%w: %d > %d",
			ErrRSEncMaxShardsExceed, total, MaxGF256Shards)
	}

	// Ensure GF(2^8) tables are initialized.
	initGF256Tables()

	// Precompute evaluation points: g^0, g^1, ..., g^{total-1}.
	evalPoints := make([]GF256, total)
	for i := 0; i < total; i++ {
		evalPoints[i] = GF256Exp(i)
	}

	return &RSEncoderGF256{
		dataShards:   dataShards,
		parityShards: parityShards,
		totalShards:  total,
		evalPoints:   evalPoints,
	}, nil
}

// DataShards returns the number of data shards.
func (enc *RSEncoderGF256) DataShards() int { return enc.dataShards }

// ParityShards returns the number of parity shards.
func (enc *RSEncoderGF256) ParityShards() int { return enc.parityShards }

// TotalShards returns the total number of shards (data + parity).
func (enc *RSEncoderGF256) TotalShards() int { return enc.totalShards }

// Encode takes raw data, splits it into dataShards coefficient groups, and
// produces totalShards output slices. For each byte position, the data
// bytes form polynomial coefficients of degree < dataShards, and each
// output shard i is the polynomial evaluated at evalPoints[i].
//
// All shards (including the first dataShards) are polynomial evaluations,
// enabling reconstruction from any k-of-n shards via Lagrange interpolation.
func (enc *RSEncoderGF256) Encode(data []byte) ([][]byte, error) {
	if len(data) == 0 {
		return nil, ErrRSEncEmptyInput
	}

	// Compute shard size, padding if needed.
	shardSize := (len(data) + enc.dataShards - 1) / enc.dataShards
	padded := make([]byte, shardSize*enc.dataShards)
	copy(padded, data)

	// Split into coefficient groups.
	coeffGroups := make([][]byte, enc.dataShards)
	for i := 0; i < enc.dataShards; i++ {
		coeffGroups[i] = padded[i*shardSize : (i+1)*shardSize]
	}

	return enc.EncodeShards(coeffGroups)
}

// EncodeShards takes pre-split data coefficient groups and produces
// totalShards output slices. Each output shard is a polynomial evaluation.
// All input groups must have identical length.
func (enc *RSEncoderGF256) EncodeShards(coeffGroups [][]byte) ([][]byte, error) {
	if len(coeffGroups) != enc.dataShards {
		return nil, fmt.Errorf("%w: got %d data shards, want %d",
			ErrRSEncShardCount, len(coeffGroups), enc.dataShards)
	}
	if len(coeffGroups) == 0 {
		return nil, ErrRSEncEmptyInput
	}

	shardSize := len(coeffGroups[0])
	if shardSize == 0 {
		return nil, ErrRSEncEmptyInput
	}
	for i, s := range coeffGroups {
		if len(s) != shardSize {
			return nil, fmt.Errorf("%w: shard %d has %d bytes, shard 0 has %d",
				ErrRSEncShardSize, i, len(s), shardSize)
		}
	}

	// Allocate output.
	output := make([][]byte, enc.totalShards)
	for i := 0; i < enc.totalShards; i++ {
		output[i] = make([]byte, shardSize)
	}

	// For each byte position, form a polynomial from the coefficient group
	// bytes and evaluate at ALL evaluation points (data + parity).
	for byteIdx := 0; byteIdx < shardSize; byteIdx++ {
		coeffs := make([]GF256, enc.dataShards)
		for i := 0; i < enc.dataShards; i++ {
			coeffs[i] = GF256(coeffGroups[i][byteIdx])
		}

		// Evaluate at every shard position.
		for si := 0; si < enc.totalShards; si++ {
			output[si][byteIdx] = byte(GF256PolyEval(coeffs, enc.evalPoints[si]))
		}
	}

	return output, nil
}

// Reconstruct recovers all shards from a partial set using Lagrange
// interpolation. The shards slice must have length totalShards; missing
// shards should be nil. At least dataShards non-nil shards are required.
// Returns a new slice with all shards reconstructed.
func (enc *RSEncoderGF256) Reconstruct(shards [][]byte) ([][]byte, error) {
	if len(shards) != enc.totalShards {
		return nil, fmt.Errorf("%w: got %d shards, want %d",
			ErrRSEncShardCount, len(shards), enc.totalShards)
	}

	// Identify available shards and verify uniform sizes.
	shardSize := 0
	var availableIndices []int
	var availableData [][]byte
	for i, s := range shards {
		if s != nil {
			if shardSize == 0 {
				shardSize = len(s)
			} else if len(s) != shardSize {
				return nil, ErrRSEncShardSize
			}
			availableIndices = append(availableIndices, i)
			availableData = append(availableData, s)
		}
	}

	if len(availableData) < enc.dataShards {
		return nil, fmt.Errorf("%w: have %d, need %d",
			ErrRSEncTooFewShards, len(availableData), enc.dataShards)
	}
	if shardSize == 0 {
		return nil, ErrRSEncEmptyInput
	}

	// Use exactly dataShards available shards for interpolation.
	n := enc.dataShards
	xs := make([]GF256, n)
	for i := 0; i < n; i++ {
		xs[i] = enc.evalPoints[availableIndices[i]]
	}

	// Allocate result.
	result := make([][]byte, enc.totalShards)
	for i := 0; i < enc.totalShards; i++ {
		result[i] = make([]byte, shardSize)
	}

	// For each byte position, interpolate the polynomial and re-evaluate.
	for byteIdx := 0; byteIdx < shardSize; byteIdx++ {
		ys := make([]GF256, n)
		for i := 0; i < n; i++ {
			ys[i] = GF256(availableData[i][byteIdx])
		}

		// Lagrange interpolation to recover the polynomial.
		poly := GF256Interpolate(xs, ys)

		// Evaluate at all shard positions.
		for si := 0; si < enc.totalShards; si++ {
			result[si][byteIdx] = byte(GF256PolyEval(poly, enc.evalPoints[si]))
		}
	}

	return result, nil
}

// ReconstructData recovers the original data by reconstructing all shards
// and then extracting the polynomial coefficients. The original data is
// the concatenation of the polynomial coefficients (one per data shard).
func (enc *RSEncoderGF256) ReconstructData(shards [][]byte) ([]byte, error) {
	if len(shards) != enc.totalShards {
		return nil, fmt.Errorf("%w: got %d shards, want %d",
			ErrRSEncShardCount, len(shards), enc.totalShards)
	}

	// Identify available shards.
	shardSize := 0
	var availableIndices []int
	var availableData [][]byte
	for i, s := range shards {
		if s != nil {
			if shardSize == 0 {
				shardSize = len(s)
			} else if len(s) != shardSize {
				return nil, ErrRSEncShardSize
			}
			availableIndices = append(availableIndices, i)
			availableData = append(availableData, s)
		}
	}

	if len(availableData) < enc.dataShards {
		return nil, fmt.Errorf("%w: have %d, need %d",
			ErrRSEncTooFewShards, len(availableData), enc.dataShards)
	}
	if shardSize == 0 {
		return nil, ErrRSEncEmptyInput
	}

	n := enc.dataShards
	xs := make([]GF256, n)
	for i := 0; i < n; i++ {
		xs[i] = enc.evalPoints[availableIndices[i]]
	}

	// Recover polynomial coefficients for each byte position.
	result := make([]byte, shardSize*enc.dataShards)
	for byteIdx := 0; byteIdx < shardSize; byteIdx++ {
		ys := make([]GF256, n)
		for i := 0; i < n; i++ {
			ys[i] = GF256(availableData[i][byteIdx])
		}

		// Interpolate to get polynomial coefficients.
		poly := GF256Interpolate(xs, ys)

		// The coefficients are the original data bytes.
		for i := 0; i < enc.dataShards && i < len(poly); i++ {
			result[i*shardSize+byteIdx] = byte(poly[i])
		}
	}

	return result, nil
}

// VerifyIntegrity checks that all shards are consistent by interpolating
// the polynomial from the first dataShards shards and verifying the
// remaining shards match the polynomial evaluations. Returns true if
// all shards are consistent.
func (enc *RSEncoderGF256) VerifyIntegrity(shards [][]byte) (bool, error) {
	if len(shards) != enc.totalShards {
		return false, fmt.Errorf("%w: got %d, want %d",
			ErrRSEncShardCount, len(shards), enc.totalShards)
	}

	shardSize := len(shards[0])
	if shardSize == 0 {
		return false, ErrRSEncEmptyInput
	}
	for i, s := range shards {
		if len(s) != shardSize {
			return false, fmt.Errorf("%w: shard %d has %d bytes, shard 0 has %d",
				ErrRSEncShardSize, i, len(s), shardSize)
		}
	}

	// For each byte position, interpolate from the first dataShards shards
	// and verify the remaining evaluations match.
	for byteIdx := 0; byteIdx < shardSize; byteIdx++ {
		xs := make([]GF256, enc.dataShards)
		ys := make([]GF256, enc.dataShards)
		for i := 0; i < enc.dataShards; i++ {
			xs[i] = enc.evalPoints[i]
			ys[i] = GF256(shards[i][byteIdx])
		}

		poly := GF256Interpolate(xs, ys)

		// Verify parity shards.
		for si := enc.dataShards; si < enc.totalShards; si++ {
			expected := GF256PolyEval(poly, enc.evalPoints[si])
			if GF256(shards[si][byteIdx]) != expected {
				return false, nil
			}
		}
	}

	return true, nil
}

// DetectCorruption identifies which shards are corrupted by checking each
// parity shard against the polynomial defined by the data shards. Returns
// the indices of corrupted shards. Only checks parity shards (the first
// dataShards evaluations define the polynomial).
func (enc *RSEncoderGF256) DetectCorruption(shards [][]byte) ([]int, error) {
	if len(shards) != enc.totalShards {
		return nil, fmt.Errorf("%w: got %d, want %d",
			ErrRSEncShardCount, len(shards), enc.totalShards)
	}

	shardSize := len(shards[0])
	if shardSize == 0 {
		return nil, ErrRSEncEmptyInput
	}

	corrupted := make(map[int]bool)

	for byteIdx := 0; byteIdx < shardSize; byteIdx++ {
		xs := make([]GF256, enc.dataShards)
		ys := make([]GF256, enc.dataShards)
		for i := 0; i < enc.dataShards; i++ {
			xs[i] = enc.evalPoints[i]
			ys[i] = GF256(shards[i][byteIdx])
		}

		poly := GF256Interpolate(xs, ys)

		for si := enc.dataShards; si < enc.totalShards; si++ {
			expected := GF256PolyEval(poly, enc.evalPoints[si])
			if GF256(shards[si][byteIdx]) != expected {
				corrupted[si] = true
			}
		}
	}

	var result []int
	for idx := range corrupted {
		result = append(result, idx)
	}
	sortInts(result)
	return result, nil
}

// sortInts sorts a slice of ints in ascending order (simple insertion sort).
func sortInts(a []int) {
	for i := 1; i < len(a); i++ {
		for j := i; j > 0 && a[j] < a[j-1]; j-- {
			a[j], a[j-1] = a[j-1], a[j]
		}
	}
}
