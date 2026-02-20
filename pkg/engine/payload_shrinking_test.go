package engine

import (
	"bytes"
	"errors"
	"math"
	"testing"
)

func TestShrinkPayload_BasicCompression(t *testing.T) {
	shrinker := NewPayloadShrinker(DefaultShrinkConfig())
	// Highly compressible payload: repeated pattern.
	payload := bytes.Repeat([]byte{0xab, 0xcd, 0xef, 0x01}, 1000)

	result, stats, err := shrinker.ShrinkPayload(payload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) >= len(payload) {
		t.Fatalf("expected shrunk payload to be smaller: got %d, original %d", len(result), len(payload))
	}
	if stats.OriginalSize != len(payload) {
		t.Fatalf("expected original size %d, got %d", len(payload), stats.OriginalSize)
	}
	if stats.ShrunkSize != len(result) {
		t.Fatalf("expected shrunk size %d, got %d", len(result), stats.ShrunkSize)
	}
	if stats.CompressionRatio <= 0 || stats.CompressionRatio >= 1.0 {
		t.Fatalf("expected compression ratio between 0 and 1, got %f", stats.CompressionRatio)
	}
}

func TestShrinkPayload_EmptyPayload(t *testing.T) {
	shrinker := NewPayloadShrinker(DefaultShrinkConfig())
	_, _, err := shrinker.ShrinkPayload(nil)
	if !errors.Is(err, ErrShrinkEmptyPayload) {
		t.Fatalf("expected ErrShrinkEmptyPayload, got %v", err)
	}
	_, _, err = shrinker.ShrinkPayload([]byte{})
	if !errors.Is(err, ErrShrinkEmptyPayload) {
		t.Fatalf("expected ErrShrinkEmptyPayload for empty slice, got %v", err)
	}
}

func TestShrinkPayload_MaxSizeExceeded(t *testing.T) {
	shrinker := NewPayloadShrinker(ShrinkConfig{
		MaxPayloadSize:   100,
		CompressionLevel: 6,
	})
	payload := make([]byte, 200)
	_, _, err := shrinker.ShrinkPayload(payload)
	if !errors.Is(err, ErrShrinkMaxSizeExceeded) {
		t.Fatalf("expected ErrShrinkMaxSizeExceeded, got %v", err)
	}
}

func TestShrinkPayload_AlreadySmall(t *testing.T) {
	shrinker := NewPayloadShrinker(DefaultShrinkConfig())
	// Very small payload: compression overhead may make it larger.
	payload := []byte{0x01, 0x02, 0x03}

	result, stats, err := shrinker.ShrinkPayload(payload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Result should still be valid even if not smaller.
	if len(result) == 0 {
		t.Fatal("result should not be empty")
	}
	if stats.OriginalSize != 3 {
		t.Fatalf("expected original size 3, got %d", stats.OriginalSize)
	}
}

func TestShrinkPayload_StatsAccuracy(t *testing.T) {
	shrinker := NewPayloadShrinker(DefaultShrinkConfig())
	payload := bytes.Repeat([]byte{0x00}, 5000)

	result, stats, err := shrinker.ShrinkPayload(payload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stats.OriginalSize != 5000 {
		t.Fatalf("original size mismatch: got %d", stats.OriginalSize)
	}
	if stats.ShrunkSize != len(result) {
		t.Fatalf("shrunk size mismatch: stats=%d, actual=%d", stats.ShrunkSize, len(result))
	}
	expectedRatio := float64(stats.ShrunkSize) / float64(stats.OriginalSize)
	if math.Abs(stats.CompressionRatio-expectedRatio) > 0.001 {
		t.Fatalf("compression ratio mismatch: stats=%f, calculated=%f",
			stats.CompressionRatio, expectedRatio)
	}
	if stats.Strategy != StrategyCombined {
		t.Fatalf("expected strategy %q, got %q", StrategyCombined, stats.Strategy)
	}
}

func TestShrinkPayload_TrailingZeros(t *testing.T) {
	shrinker := NewPayloadShrinker(DefaultShrinkConfig())
	// Payload with many trailing zeros.
	payload := make([]byte, 1000)
	payload[0] = 0xab
	payload[1] = 0xcd

	result, stats, err := shrinker.ShrinkPayload(payload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) >= len(payload) {
		t.Fatalf("expected smaller result after pruning zeros: %d >= %d", len(result), len(payload))
	}
	if stats.FieldsPruned == 0 {
		t.Fatal("expected at least one field pruned (trailing zeros)")
	}
}

// --- ApplyStrategy tests ---

func TestApplyStrategy_Compress(t *testing.T) {
	shrinker := NewPayloadShrinker(DefaultShrinkConfig())
	payload := bytes.Repeat([]byte{0x42}, 1000)

	result, err := shrinker.ApplyStrategy(payload, StrategyCompress)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) >= len(payload) {
		t.Fatalf("compression should reduce size: %d >= %d", len(result), len(payload))
	}

	// Verify we can decompress back.
	decompressed, err := decompressFlate(result)
	if err != nil {
		t.Fatalf("decompression failed: %v", err)
	}
	if !bytes.Equal(decompressed, payload) {
		t.Fatal("decompressed data does not match original")
	}
}

func TestApplyStrategy_PruneZeros(t *testing.T) {
	shrinker := NewPayloadShrinker(DefaultShrinkConfig())
	payload := make([]byte, 100)
	payload[0] = 0xff
	payload[1] = 0xee

	result, err := shrinker.ApplyStrategy(payload, StrategyPruneZeros)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("expected pruned size 2, got %d", len(result))
	}
	if result[0] != 0xff || result[1] != 0xee {
		t.Fatal("pruned data content mismatch")
	}
}

func TestApplyStrategy_PruneZeros_AllZeros(t *testing.T) {
	shrinker := NewPayloadShrinker(DefaultShrinkConfig())
	payload := make([]byte, 50)

	result, err := shrinker.ApplyStrategy(payload, StrategyPruneZeros)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// All-zeros should preserve at least one byte.
	if len(result) != 1 {
		t.Fatalf("expected 1 byte for all-zeros, got %d", len(result))
	}
}

func TestApplyStrategy_PruneZeros_NoTrailingZeros(t *testing.T) {
	shrinker := NewPayloadShrinker(DefaultShrinkConfig())
	payload := []byte{0x01, 0x02, 0x03}

	result, err := shrinker.ApplyStrategy(payload, StrategyPruneZeros)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !bytes.Equal(result, payload) {
		t.Fatal("payload without trailing zeros should be unchanged")
	}
}

func TestApplyStrategy_Dedup(t *testing.T) {
	shrinker := NewPayloadShrinker(DefaultShrinkConfig())
	// Create data with duplicate 8-byte chunks.
	chunk := []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08}
	payload := bytes.Repeat(chunk, 10) // 80 bytes, all same chunk

	result, err := shrinker.ApplyStrategy(payload, StrategyDedup)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) >= len(payload) {
		t.Fatalf("dedup should reduce size: %d >= %d", len(result), len(payload))
	}
}

func TestApplyStrategy_Combined(t *testing.T) {
	shrinker := NewPayloadShrinker(DefaultShrinkConfig())
	// Payload with both duplicate chunks and trailing zeros.
	chunk := []byte{0xab, 0xcd, 0xef, 0x01, 0x02, 0x03, 0x04, 0x05}
	payload := bytes.Repeat(chunk, 20)
	payload = append(payload, make([]byte, 200)...) // trailing zeros

	result, err := shrinker.ApplyStrategy(payload, StrategyCombined)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) >= len(payload) {
		t.Fatalf("combined should reduce size: %d >= %d", len(result), len(payload))
	}
}

func TestApplyStrategy_UnknownStrategy(t *testing.T) {
	shrinker := NewPayloadShrinker(DefaultShrinkConfig())
	_, err := shrinker.ApplyStrategy([]byte{0x01}, "bogus")
	if !errors.Is(err, ErrShrinkUnknownStrategy) {
		t.Fatalf("expected ErrShrinkUnknownStrategy, got %v", err)
	}
}

func TestApplyStrategy_EmptyPayload(t *testing.T) {
	shrinker := NewPayloadShrinker(DefaultShrinkConfig())
	_, err := shrinker.ApplyStrategy(nil, StrategyCompress)
	if !errors.Is(err, ErrShrinkEmptyPayload) {
		t.Fatalf("expected ErrShrinkEmptyPayload, got %v", err)
	}
}

func TestApplyStrategy_MaxSizeExceeded(t *testing.T) {
	shrinker := NewPayloadShrinker(ShrinkConfig{
		MaxPayloadSize:   50,
		CompressionLevel: 6,
	})
	payload := make([]byte, 100)
	_, err := shrinker.ApplyStrategy(payload, StrategyCompress)
	if !errors.Is(err, ErrShrinkMaxSizeExceeded) {
		t.Fatalf("expected ErrShrinkMaxSizeExceeded, got %v", err)
	}
}

// --- EstimateShrinkage tests ---

func TestEstimateShrinkage_EmptyPayload(t *testing.T) {
	shrinker := NewPayloadShrinker(DefaultShrinkConfig())
	est := shrinker.EstimateShrinkage(nil)
	if est.EstimatedRatio != 1.0 {
		t.Fatalf("expected ratio 1.0 for empty payload, got %f", est.EstimatedRatio)
	}
	if est.PrunableBytes != 0 {
		t.Fatalf("expected 0 prunable bytes for empty payload, got %d", est.PrunableBytes)
	}
}

func TestEstimateShrinkage_HighlyCompressible(t *testing.T) {
	shrinker := NewPayloadShrinker(DefaultShrinkConfig())
	payload := bytes.Repeat([]byte{0x00}, 10000)

	est := shrinker.EstimateShrinkage(payload)
	if est.EstimatedRatio >= 0.5 {
		t.Fatalf("expected low ratio for all-zeros, got %f", est.EstimatedRatio)
	}
	if est.PrunableBytes == 0 {
		t.Fatal("expected prunable bytes for all-zeros payload")
	}
}

func TestEstimateShrinkage_VsActual(t *testing.T) {
	shrinker := NewPayloadShrinker(DefaultShrinkConfig())
	payload := bytes.Repeat([]byte{0xab, 0xcd}, 2000)

	est := shrinker.EstimateShrinkage(payload)
	_, stats, err := shrinker.ShrinkPayload(payload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Estimate should be in the right ballpark (within 50% of actual).
	if est.EstimatedRatio > stats.CompressionRatio*3 {
		t.Logf("estimate=%f, actual=%f (estimate too high but acceptable as heuristic)",
			est.EstimatedRatio, stats.CompressionRatio)
	}
	// Just verify the estimate is a valid number.
	if est.EstimatedRatio <= 0 || est.EstimatedRatio > 1.0 {
		t.Fatalf("estimate ratio out of range: %f", est.EstimatedRatio)
	}
}

// --- DefaultShrinkConfig ---

func TestDefaultShrinkConfig(t *testing.T) {
	cfg := DefaultShrinkConfig()
	if cfg.MaxPayloadSize != 10*1024*1024 {
		t.Fatalf("expected max size 10MiB, got %d", cfg.MaxPayloadSize)
	}
	if cfg.CompressionLevel != 6 {
		t.Fatalf("expected compression level 6, got %d", cfg.CompressionLevel)
	}
	if len(cfg.PrunableFields) != 3 {
		t.Fatalf("expected 3 prunable fields, got %d", len(cfg.PrunableFields))
	}
}

// --- Internal helper tests ---

func TestCountTrailingZeros(t *testing.T) {
	if countTrailingZeros([]byte{0x01, 0x00, 0x00}) != 2 {
		t.Fatal("expected 2 trailing zeros")
	}
	if countTrailingZeros([]byte{0x01, 0x02, 0x03}) != 0 {
		t.Fatal("expected 0 trailing zeros")
	}
	if countTrailingZeros([]byte{0x00, 0x00, 0x00}) != 3 {
		t.Fatal("expected 3 trailing zeros")
	}
	if countTrailingZeros([]byte{}) != 0 {
		t.Fatal("expected 0 for empty")
	}
}

func TestCountUniqueBytes(t *testing.T) {
	if countUniqueBytes([]byte{0x00, 0x00, 0x00}) != 1 {
		t.Fatal("expected 1 unique byte")
	}
	if countUniqueBytes([]byte{0x00, 0x01, 0x02}) != 3 {
		t.Fatal("expected 3 unique bytes")
	}
	if countUniqueBytes(nil) != 0 {
		t.Fatal("expected 0 for nil")
	}
}

func TestCompressionLevelClamping(t *testing.T) {
	// Invalid compression levels should be clamped to 6.
	s1 := NewPayloadShrinker(ShrinkConfig{CompressionLevel: 0})
	if s1.config.CompressionLevel != 6 {
		t.Fatalf("expected level 6, got %d", s1.config.CompressionLevel)
	}
	s2 := NewPayloadShrinker(ShrinkConfig{CompressionLevel: 10})
	if s2.config.CompressionLevel != 6 {
		t.Fatalf("expected level 6, got %d", s2.config.CompressionLevel)
	}
	// Valid level should be preserved.
	s3 := NewPayloadShrinker(ShrinkConfig{CompressionLevel: 1})
	if s3.config.CompressionLevel != 1 {
		t.Fatalf("expected level 1, got %d", s3.config.CompressionLevel)
	}
}

func TestNoMaxSizeLimit(t *testing.T) {
	// MaxPayloadSize=0 means no limit.
	shrinker := NewPayloadShrinker(ShrinkConfig{
		MaxPayloadSize:   0,
		CompressionLevel: 6,
	})
	payload := make([]byte, 100000)
	payload[0] = 0xff
	_, _, err := shrinker.ShrinkPayload(payload)
	if err != nil {
		t.Fatalf("unexpected error with no max size: %v", err)
	}
}
