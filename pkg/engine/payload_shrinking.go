// payload_shrinking.go implements payload size reduction for the eth2028 client.
// Part of the EL Sustainability track: reduce block payload size through
// compression and pruning of redundant data (e.g., trailing zeros, duplicate
// byte sequences).
package engine

import (
	"bytes"
	"compress/flate"
	"errors"
	"fmt"
	"io"
)

// Shrinking strategy identifiers.
const (
	StrategyCompress   = "compress"
	StrategyPruneZeros = "prune_zeros"
	StrategyDedup      = "dedup"
	StrategyCombined   = "combined"
)

// Payload shrinking errors.
var (
	ErrShrinkEmptyPayload   = errors.New("payload_shrink: empty payload")
	ErrShrinkMaxSizeExceeded = errors.New("payload_shrink: payload exceeds max size")
	ErrShrinkUnknownStrategy = errors.New("payload_shrink: unknown strategy")
	ErrShrinkCompressFailed  = errors.New("payload_shrink: compression failed")
)

// ShrinkConfig configures the PayloadShrinker behavior.
type ShrinkConfig struct {
	// MaxPayloadSize is the maximum allowed payload size in bytes.
	// Payloads larger than this are rejected. 0 means no limit.
	MaxPayloadSize int
	// CompressionLevel is the flate compression level (1-9). Default 6.
	CompressionLevel int
	// PrunableFields lists field names eligible for pruning (informational).
	PrunableFields []string
}

// DefaultShrinkConfig returns a ShrinkConfig with sensible defaults.
func DefaultShrinkConfig() ShrinkConfig {
	return ShrinkConfig{
		MaxPayloadSize:   10 * 1024 * 1024, // 10 MiB
		CompressionLevel: 6,
		PrunableFields:   []string{"logsBloom", "extraData", "transactions"},
	}
}

// ShrinkStats holds statistics about a shrink operation.
type ShrinkStats struct {
	// OriginalSize is the size of the input payload in bytes.
	OriginalSize int
	// ShrunkSize is the size of the output payload in bytes.
	ShrunkSize int
	// CompressionRatio is ShrunkSize / OriginalSize (0.0 to 1.0, lower is better).
	CompressionRatio float64
	// FieldsPruned is the number of fields or regions that were pruned.
	FieldsPruned int
	// Strategy is the strategy that was applied.
	Strategy string
}

// ShrinkEstimate provides an estimate of shrinkage without modifying the payload.
type ShrinkEstimate struct {
	// EstimatedRatio is the estimated compression ratio (0.0 to 1.0).
	EstimatedRatio float64
	// PrunableBytes is the estimated number of bytes that can be pruned.
	PrunableBytes int
}

// PayloadShrinker reduces payload size through compression and pruning.
type PayloadShrinker struct {
	config ShrinkConfig
}

// NewPayloadShrinker creates a new PayloadShrinker with the given config.
func NewPayloadShrinker(config ShrinkConfig) *PayloadShrinker {
	if config.CompressionLevel < 1 || config.CompressionLevel > 9 {
		config.CompressionLevel = 6
	}
	return &PayloadShrinker{config: config}
}

// ShrinkPayload applies compression and pruning to reduce payload size.
// Uses the combined strategy by default.
func (s *PayloadShrinker) ShrinkPayload(payload []byte) ([]byte, ShrinkStats, error) {
	stats := ShrinkStats{Strategy: StrategyCombined}

	if len(payload) == 0 {
		return nil, stats, ErrShrinkEmptyPayload
	}
	if s.config.MaxPayloadSize > 0 && len(payload) > s.config.MaxPayloadSize {
		return nil, stats, fmt.Errorf("%w: size %d exceeds max %d",
			ErrShrinkMaxSizeExceeded, len(payload), s.config.MaxPayloadSize)
	}

	stats.OriginalSize = len(payload)

	// Apply combined strategy: prune zeros, then dedup, then compress.
	result, fieldsPruned := pruneTrailingZeros(payload)
	result, dedupPruned := dedupBytes(result)
	fieldsPruned += dedupPruned

	compressed, err := compressFlate(result, s.config.CompressionLevel)
	if err != nil {
		return nil, stats, fmt.Errorf("%w: %v", ErrShrinkCompressFailed, err)
	}

	// Use compressed only if it is actually smaller.
	if len(compressed) < len(result) {
		result = compressed
	}

	stats.ShrunkSize = len(result)
	stats.FieldsPruned = fieldsPruned
	if stats.OriginalSize > 0 {
		stats.CompressionRatio = float64(stats.ShrunkSize) / float64(stats.OriginalSize)
	}

	return result, stats, nil
}

// EstimateShrinkage estimates the potential shrinkage without modifying the payload.
func (s *PayloadShrinker) EstimateShrinkage(payload []byte) ShrinkEstimate {
	if len(payload) == 0 {
		return ShrinkEstimate{EstimatedRatio: 1.0, PrunableBytes: 0}
	}

	// Count trailing zeros as prunable.
	trailingZeros := countTrailingZeros(payload)

	// Count duplicate 8-byte chunks as prunable.
	dedupSavings := estimateDedupSavings(payload)

	prunableBytes := trailingZeros + dedupSavings

	// Estimate compression ratio: heuristic based on byte entropy.
	uniqueBytes := countUniqueBytes(payload)
	entropyFactor := float64(uniqueBytes) / 256.0
	// Higher entropy = less compressible. Low entropy -> ratio ~0.3, high -> ~0.9.
	estimatedRatio := 0.3 + 0.6*entropyFactor
	if prunableBytes > 0 {
		pruneFactor := 1.0 - float64(prunableBytes)/float64(len(payload))
		estimatedRatio *= pruneFactor
	}
	if estimatedRatio > 1.0 {
		estimatedRatio = 1.0
	}
	if estimatedRatio < 0.01 {
		estimatedRatio = 0.01
	}

	return ShrinkEstimate{
		EstimatedRatio: estimatedRatio,
		PrunableBytes:  prunableBytes,
	}
}

// ApplyStrategy applies a specific shrinking strategy to the payload.
func (s *PayloadShrinker) ApplyStrategy(payload []byte, strategy string) ([]byte, error) {
	if len(payload) == 0 {
		return nil, ErrShrinkEmptyPayload
	}
	if s.config.MaxPayloadSize > 0 && len(payload) > s.config.MaxPayloadSize {
		return nil, fmt.Errorf("%w: size %d exceeds max %d",
			ErrShrinkMaxSizeExceeded, len(payload), s.config.MaxPayloadSize)
	}

	switch strategy {
	case StrategyCompress:
		result, err := compressFlate(payload, s.config.CompressionLevel)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrShrinkCompressFailed, err)
		}
		return result, nil

	case StrategyPruneZeros:
		result, _ := pruneTrailingZeros(payload)
		return result, nil

	case StrategyDedup:
		result, _ := dedupBytes(payload)
		return result, nil

	case StrategyCombined:
		result, _ := pruneTrailingZeros(payload)
		result, _ = dedupBytes(result)
		compressed, err := compressFlate(result, s.config.CompressionLevel)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrShrinkCompressFailed, err)
		}
		if len(compressed) < len(result) {
			return compressed, nil
		}
		return result, nil

	default:
		return nil, fmt.Errorf("%w: %s", ErrShrinkUnknownStrategy, strategy)
	}
}

// --- Internal helpers ---

// compressFlate compresses data using the DEFLATE algorithm.
func compressFlate(data []byte, level int) ([]byte, error) {
	var buf bytes.Buffer
	w, err := flate.NewWriter(&buf, level)
	if err != nil {
		return nil, err
	}
	if _, err := w.Write(data); err != nil {
		w.Close()
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// decompressFlate decompresses DEFLATE-compressed data.
func decompressFlate(data []byte) ([]byte, error) {
	r := flate.NewReader(bytes.NewReader(data))
	defer r.Close()
	return io.ReadAll(r)
}

// pruneTrailingZeros removes trailing zero bytes from the payload.
// Returns the pruned payload and the count of pruned regions (0 or 1).
func pruneTrailingZeros(data []byte) ([]byte, int) {
	end := len(data)
	for end > 0 && data[end-1] == 0 {
		end--
	}
	if end == len(data) {
		// No trailing zeros to prune.
		result := make([]byte, len(data))
		copy(result, data)
		return result, 0
	}
	if end == 0 {
		// All zeros: preserve at least one byte.
		return []byte{0}, 1
	}
	result := make([]byte, end)
	copy(result, data[:end])
	return result, 1
}

// dedupBytes replaces consecutive duplicate 8-byte chunks with a single copy.
// Returns the deduped data and the number of chunks removed.
func dedupBytes(data []byte) ([]byte, int) {
	const chunkSize = 8
	if len(data) < chunkSize*2 {
		result := make([]byte, len(data))
		copy(result, data)
		return result, 0
	}

	var buf bytes.Buffer
	removed := 0
	i := 0

	for i < len(data) {
		if i+chunkSize*2 <= len(data) {
			chunk := data[i : i+chunkSize]
			nextChunk := data[i+chunkSize : i+chunkSize*2]
			if bytes.Equal(chunk, nextChunk) {
				// Write the chunk once, skip duplicates.
				buf.Write(chunk)
				i += chunkSize
				for i+chunkSize <= len(data) && bytes.Equal(data[i:i+chunkSize], chunk) {
					removed++
					i += chunkSize
				}
				continue
			}
		}
		buf.WriteByte(data[i])
		i++
	}

	return buf.Bytes(), removed
}

// countTrailingZeros counts the number of trailing zero bytes.
func countTrailingZeros(data []byte) int {
	count := 0
	for i := len(data) - 1; i >= 0; i-- {
		if data[i] == 0 {
			count++
		} else {
			break
		}
	}
	return count
}

// estimateDedupSavings estimates bytes saved by deduplication.
func estimateDedupSavings(data []byte) int {
	const chunkSize = 8
	if len(data) < chunkSize*2 {
		return 0
	}
	savings := 0
	i := 0
	for i+chunkSize*2 <= len(data) {
		if bytes.Equal(data[i:i+chunkSize], data[i+chunkSize:i+chunkSize*2]) {
			savings += chunkSize
			i += chunkSize * 2
			for i+chunkSize <= len(data) && bytes.Equal(data[i:i+chunkSize], data[i-chunkSize:i]) {
				savings += chunkSize
				i += chunkSize
			}
		} else {
			i++
		}
	}
	return savings
}

// countUniqueBytes returns the number of distinct byte values in the data.
func countUniqueBytes(data []byte) int {
	var seen [256]bool
	for _, b := range data {
		seen[b] = true
	}
	count := 0
	for _, s := range seen {
		if s {
			count++
		}
	}
	return count
}
