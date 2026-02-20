// Package das - reed_solomon_encode.go implements Reed-Solomon erasure coding
// over GF(2^16) for data availability in PeerDAS. The encoder treats data shards
// as polynomial coefficients and evaluates the polynomial at totalShards distinct
// points in GF(2^16) to produce an extended codeword. Recovery uses Lagrange
// interpolation from any dataShards evaluations to reconstruct the polynomial.
//
// This evaluation-based approach means each shard i is the polynomial evaluated
// at the i-th evaluation point (a^i where a is the primitive element of GF(2^16)).
// The first dataShards evaluations correspond to the data polynomial, and the
// remaining parityShards are parity evaluations.
//
// Reference: Reed & Solomon (1960), "Polynomial Codes over Certain Finite Fields"
package das

import (
	"errors"
	"fmt"
)

// Reed-Solomon encoder errors.
var (
	ErrRSInvalidConfig     = errors.New("das/rs: invalid shard configuration")
	ErrRSDataTooLarge      = errors.New("das/rs: data exceeds maximum encodable size")
	ErrRSShardSizeMismatch = errors.New("das/rs: input shard sizes are not uniform")
	ErrRSEmptyInput        = errors.New("das/rs: empty input data")
	ErrRSShardCount        = errors.New("das/rs: shard count mismatch")
	ErrRSTooFewShards      = errors.New("das/rs: insufficient shards for reconstruction")
)

// MaxGF16Shards is the maximum number of total shards for GF(2^16).
// Limited to the field order since each shard needs a unique evaluation point.
const MaxGF16Shards = gfOrder // 65535

// RSEncoder implements Reed-Solomon encoding over GF(2^16) using polynomial
// evaluation. Data symbols form the coefficients of a polynomial, and each
// shard is an evaluation of that polynomial at a distinct field element.
type RSEncoder struct {
	// dataShards is the number of original data shards (k).
	dataShards int

	// parityShards is the number of parity shards (n - k).
	parityShards int

	// totalShards is dataShards + parityShards.
	totalShards int

	// evalPoints are the evaluation points: a^0, a^1, ..., a^{totalShards-1}.
	evalPoints []GF2_16

	// generatorPoly is the generator polynomial g(x) of degree parityShards.
	// Used for VerifyParity: g(x) = prod(x - evalPoint[i]) for i in [k, n).
	generatorPoly []GF2_16
}

// NewRSEncoder creates a new Reed-Solomon encoder for the given shard counts.
// dataShards (k) is the number of data shards; parityShards (m) is the number
// of parity shards. The total k + m must not exceed MaxGF16Shards.
func NewRSEncoder(dataShards, parityShards int) (*RSEncoder, error) {
	if dataShards <= 0 || parityShards <= 0 {
		return nil, fmt.Errorf("%w: dataShards=%d, parityShards=%d",
			ErrRSInvalidConfig, dataShards, parityShards)
	}
	total := dataShards + parityShards
	if total > MaxGF16Shards {
		return nil, fmt.Errorf("%w: total shards %d exceeds max %d",
			ErrRSInvalidConfig, total, MaxGF16Shards)
	}

	// Ensure GF tables are initialized.
	initGFTables()

	// Precompute evaluation points: a^0, a^1, ..., a^{total-1}.
	evalPoints := make([]GF2_16, total)
	for i := 0; i < total; i++ {
		evalPoints[i] = GFExp(i)
	}

	gen := computeGeneratorPoly(parityShards)

	return &RSEncoder{
		dataShards:    dataShards,
		parityShards:  parityShards,
		totalShards:   total,
		evalPoints:    evalPoints,
		generatorPoly: gen,
	}, nil
}

// computeGeneratorPoly computes the generator polynomial for n parity shards:
//
//	g(x) = (x - a^0)(x - a^1)...(x - a^{n-1})
//
// where a = GFExp(1) is the primitive element.
// The result has degree n with leading coefficient 1.
func computeGeneratorPoly(n int) []GF2_16 {
	initGFTables()
	// Start with g(x) = 1.
	gen := []GF2_16{1}
	for i := 0; i < n; i++ {
		// Multiply by (x - a^i). In GF(2^k), (x - a^i) = (x + a^i).
		root := GFExp(i)
		factor := []GF2_16{root, 1}
		gen = GFPolyMul(gen, factor)
	}
	return gen
}

// Encode takes dataShards slices of equal length and produces totalShards
// slices. For each symbol column, the data symbols become polynomial
// coefficients, and each output shard is the polynomial evaluated at the
// corresponding evaluation point (a^i).
//
// For 16-bit symbols, each pair of consecutive bytes in a shard forms one
// GF(2^16) symbol. Shard sizes must be even (zero-padded if needed).
func (enc *RSEncoder) Encode(data [][]byte) ([][]byte, error) {
	if len(data) != enc.dataShards {
		return nil, fmt.Errorf("%w: got %d data shards, want %d",
			ErrRSShardCount, len(data), enc.dataShards)
	}

	// Verify uniform shard sizes.
	if len(data) == 0 {
		return nil, ErrRSEmptyInput
	}
	shardSize := len(data[0])
	if shardSize == 0 {
		return nil, ErrRSEmptyInput
	}
	for i, d := range data {
		if len(d) != shardSize {
			return nil, fmt.Errorf("%w: shard %d has size %d, shard 0 has size %d",
				ErrRSShardSizeMismatch, i, len(d), shardSize)
		}
	}

	// Ensure shard size is even for 16-bit symbol alignment.
	symbolSize := shardSize
	if symbolSize%2 != 0 {
		symbolSize++
	}
	numSymbols := symbolSize / 2

	// Allocate output shards.
	output := make([][]byte, enc.totalShards)
	for i := 0; i < enc.totalShards; i++ {
		output[i] = make([]byte, symbolSize)
	}

	// Encode each symbol column independently.
	for col := 0; col < numSymbols; col++ {
		byteOff := col * 2

		// Extract the message polynomial coefficients from data shards.
		coeffs := make([]GF2_16, enc.dataShards)
		for i := 0; i < enc.dataShards; i++ {
			hi := uint16(0)
			lo := uint16(0)
			if byteOff < len(data[i]) {
				hi = uint16(data[i][byteOff])
			}
			if byteOff+1 < len(data[i]) {
				lo = uint16(data[i][byteOff+1])
			}
			coeffs[i] = GF2_16(hi<<8 | lo)
		}

		// Evaluate the polynomial at each evaluation point.
		for si := 0; si < enc.totalShards; si++ {
			val := GFPolyEval(coeffs, enc.evalPoints[si])
			output[si][byteOff] = byte(val >> 8)
			if byteOff+1 < symbolSize {
				output[si][byteOff+1] = byte(val & 0xFF)
			}
		}
	}

	return output, nil
}

// EncodeBlob splits a blob into dataShards pieces, encodes with Reed-Solomon,
// and returns totalShards pieces (data + parity evaluations).
// numDataShards specifies how many data shards to split into.
func (enc *RSEncoder) EncodeBlob(blob []byte, numDataShards int) ([][]byte, error) {
	if len(blob) == 0 {
		return nil, ErrRSEmptyInput
	}
	if numDataShards <= 0 {
		return nil, fmt.Errorf("%w: numDataShards must be positive", ErrRSInvalidConfig)
	}

	// Compute shard size (ceiling division, ensure even).
	shardSize := (len(blob) + numDataShards - 1) / numDataShards
	if shardSize%2 != 0 {
		shardSize++
	}

	// Split blob into shards, zero-padding the last.
	shards := make([][]byte, numDataShards)
	for i := 0; i < numDataShards; i++ {
		shards[i] = make([]byte, shardSize)
		start := i * shardSize
		end := start + shardSize
		if start < len(blob) {
			if end > len(blob) {
				end = len(blob)
			}
			copy(shards[i], blob[start:end])
		}
	}

	return enc.Encode(shards)
}

// DataShards returns the number of data shards this encoder is configured for.
func (enc *RSEncoder) DataShards() int {
	return enc.dataShards
}

// ParityShards returns the number of parity shards.
func (enc *RSEncoder) ParityShards() int {
	return enc.parityShards
}

// TotalShards returns the total number of shards (data + parity).
func (enc *RSEncoder) TotalShards() int {
	return enc.totalShards
}

// GeneratorDegree returns the degree of the generator polynomial.
func (enc *RSEncoder) GeneratorDegree() int {
	return enc.parityShards
}

// VerifyParity verifies that all shards are consistent by re-encoding the
// data polynomial from any dataShards evaluations and comparing all shards.
func (enc *RSEncoder) VerifyParity(shards [][]byte) (bool, error) {
	if len(shards) != enc.totalShards {
		return false, fmt.Errorf("%w: got %d shards, want %d",
			ErrRSShardCount, len(shards), enc.totalShards)
	}

	// Determine shard/symbol size.
	shardSize := len(shards[0])
	if shardSize == 0 {
		return false, ErrRSEmptyInput
	}
	symbolSize := shardSize
	if symbolSize%2 != 0 {
		symbolSize++
	}
	numSymbols := symbolSize / 2

	// For each symbol column, interpolate from the first dataShards shards,
	// then verify the remaining evaluations match.
	for col := 0; col < numSymbols; col++ {
		byteOff := col * 2

		xs := make([]GF2_16, enc.dataShards)
		ys := make([]GF2_16, enc.dataShards)
		for i := 0; i < enc.dataShards; i++ {
			xs[i] = enc.evalPoints[i]
			hi := uint16(0)
			lo := uint16(0)
			if byteOff < len(shards[i]) {
				hi = uint16(shards[i][byteOff])
			}
			if byteOff+1 < len(shards[i]) {
				lo = uint16(shards[i][byteOff+1])
			}
			ys[i] = GF2_16(hi<<8 | lo)
		}

		poly := GFInterpolate(xs, ys)

		// Verify parity shards match evaluation.
		for si := enc.dataShards; si < enc.totalShards; si++ {
			expected := GFPolyEval(poly, enc.evalPoints[si])
			hi := uint16(0)
			lo := uint16(0)
			if byteOff < len(shards[si]) {
				hi = uint16(shards[si][byteOff])
			}
			if byteOff+1 < len(shards[si]) {
				lo = uint16(shards[si][byteOff+1])
			}
			actual := GF2_16(hi<<8 | lo)
			if actual != expected {
				return false, nil
			}
		}
	}
	return true, nil
}

// RSRecoverData recovers all shards from a partial set using Lagrange
// interpolation over GF(2^16). shardData contains the available shards,
// shardIndices contains their positions in [0, totalShards). At least
// dataShards shards are needed.
func RSRecoverData(
	shardData [][]byte,
	shardIndices []int,
	dataShards, parityShards int,
) ([][]byte, error) {
	if len(shardData) != len(shardIndices) {
		return nil, fmt.Errorf("%w: data/indices length mismatch", ErrRSShardCount)
	}
	if len(shardData) < dataShards {
		return nil, fmt.Errorf("%w: have %d shards, need %d",
			ErrRSTooFewShards, len(shardData), dataShards)
	}

	initGFTables()

	// Determine shard size from available data.
	shardSize := 0
	for _, d := range shardData {
		if len(d) > 0 {
			shardSize = len(d)
			break
		}
	}
	if shardSize == 0 {
		return nil, ErrRSEmptyInput
	}

	// Ensure even for 16-bit symbols.
	symbolSize := shardSize
	if symbolSize%2 != 0 {
		symbolSize++
	}
	numSymbols := symbolSize / 2
	totalShards := dataShards + parityShards

	// Use exactly dataShards available shards for interpolation.
	n := dataShards
	if n > len(shardData) {
		n = len(shardData)
	}

	// Evaluation points: shard i was evaluated at a^i.
	xs := make([]GF2_16, n)
	for i := 0; i < n; i++ {
		xs[i] = GFExp(shardIndices[i])
	}

	// Allocate result.
	result := make([][]byte, totalShards)
	for i := 0; i < totalShards; i++ {
		result[i] = make([]byte, symbolSize)
	}

	for col := 0; col < numSymbols; col++ {
		byteOff := col * 2

		// Extract y-values for this column from available shards.
		ys := make([]GF2_16, n)
		for i := 0; i < n; i++ {
			hi := uint16(0)
			lo := uint16(0)
			if byteOff < len(shardData[i]) {
				hi = uint16(shardData[i][byteOff])
			}
			if byteOff+1 < len(shardData[i]) {
				lo = uint16(shardData[i][byteOff+1])
			}
			ys[i] = GF2_16(hi<<8 | lo)
		}

		// Interpolate to recover the polynomial of degree < dataShards.
		poly := GFInterpolate(xs, ys)

		// Re-evaluate at all shard positions to recover all shards.
		for si := 0; si < totalShards; si++ {
			evalPt := GFExp(si)
			val := GFPolyEval(poly, evalPt)
			result[si][byteOff] = byte(val >> 8)
			if byteOff+1 < symbolSize {
				result[si][byteOff+1] = byte(val & 0xFF)
			}
		}
	}

	return result, nil
}
