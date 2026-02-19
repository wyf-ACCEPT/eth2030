// Package erasure provides Reed-Solomon erasure coding for PeerDAS blob
// data recovery. This is a simplified implementation using XOR-based parity
// for demonstration; a production implementation would use proper Galois
// field arithmetic over GF(2^8) or the BLS12-381 scalar field.
package erasure

import (
	"errors"
	"fmt"
)

var (
	ErrInvalidShardConfig = errors.New("erasure: invalid shard configuration")
	ErrTooFewShards       = errors.New("erasure: insufficient shards for reconstruction")
	ErrShardSizeMismatch  = errors.New("erasure: shard sizes are not uniform")
	ErrInvalidShardCount  = errors.New("erasure: shard count mismatch")
)

// Encode splits data into dataShards data pieces and produces parityShards
// parity pieces using a simple XOR-based scheme.
//
// Each shard will be len(data)/dataShards bytes (data is zero-padded if needed).
// The total number of shards returned is dataShards + parityShards.
func Encode(data []byte, dataShards, parityShards int) ([][]byte, error) {
	if dataShards <= 0 || parityShards <= 0 {
		return nil, ErrInvalidShardConfig
	}

	// Compute shard size, padding data if necessary.
	shardSize := (len(data) + dataShards - 1) / dataShards
	padded := make([]byte, shardSize*dataShards)
	copy(padded, data)

	totalShards := dataShards + parityShards
	shards := make([][]byte, totalShards)

	// Create data shards.
	for i := 0; i < dataShards; i++ {
		shards[i] = make([]byte, shardSize)
		copy(shards[i], padded[i*shardSize:(i+1)*shardSize])
	}

	// Create parity shards using XOR of rotated data shard combinations.
	for p := 0; p < parityShards; p++ {
		shards[dataShards+p] = make([]byte, shardSize)
		for d := 0; d < dataShards; d++ {
			// Each parity shard XORs data shards with a rotation offset.
			srcIdx := (d + p) % dataShards
			for b := 0; b < shardSize; b++ {
				shards[dataShards+p][b] ^= shards[srcIdx][b]
			}
		}
	}

	return shards, nil
}

// Decode reconstructs the original data from a set of shards. Missing shards
// should be set to nil. At least dataShards non-nil shards are required.
//
// The shards slice must have length dataShards + parityShards.
func Decode(shards [][]byte, dataShards, parityShards int) ([]byte, error) {
	if dataShards <= 0 || parityShards <= 0 {
		return nil, ErrInvalidShardConfig
	}
	totalShards := dataShards + parityShards
	if len(shards) != totalShards {
		return nil, fmt.Errorf("%w: got %d, want %d",
			ErrInvalidShardCount, len(shards), totalShards)
	}

	// Determine shard size from any non-nil shard.
	shardSize := 0
	available := 0
	for _, s := range shards {
		if s != nil {
			if shardSize == 0 {
				shardSize = len(s)
			} else if len(s) != shardSize {
				return nil, ErrShardSizeMismatch
			}
			available++
		}
	}
	if available < dataShards {
		return nil, fmt.Errorf("%w: have %d, need %d",
			ErrTooFewShards, available, dataShards)
	}
	if shardSize == 0 {
		return nil, ErrTooFewShards
	}

	// If all data shards are present, just concatenate them.
	allDataPresent := true
	for i := 0; i < dataShards; i++ {
		if shards[i] == nil {
			allDataPresent = false
			break
		}
	}

	if allDataPresent {
		result := make([]byte, 0, shardSize*dataShards)
		for i := 0; i < dataShards; i++ {
			result = append(result, shards[i]...)
		}
		return result, nil
	}

	// Simple recovery: for each missing data shard, try to recover from
	// parity shards. This works for the simple XOR scheme when only a
	// small number of shards are missing.
	recovered := make([][]byte, totalShards)
	for i, s := range shards {
		if s != nil {
			recovered[i] = make([]byte, shardSize)
			copy(recovered[i], s)
		}
	}

	// Try to recover missing data shards from parity.
	for p := 0; p < parityShards; p++ {
		parityIdx := dataShards + p
		if recovered[parityIdx] == nil {
			continue
		}

		// Find which data shard is missing that this parity can help recover.
		missingIdx := -1
		missingCount := 0
		for d := 0; d < dataShards; d++ {
			srcIdx := (d + p) % dataShards
			if recovered[srcIdx] == nil {
				missingIdx = srcIdx
				missingCount++
			}
		}

		if missingCount != 1 || missingIdx < 0 {
			continue // Can only recover exactly one missing shard per parity.
		}

		// Recover: missing = parity XOR all_other_data_shards
		rec := make([]byte, shardSize)
		copy(rec, recovered[parityIdx])
		for d := 0; d < dataShards; d++ {
			srcIdx := (d + p) % dataShards
			if srcIdx == missingIdx {
				continue
			}
			for b := 0; b < shardSize; b++ {
				rec[b] ^= recovered[srcIdx][b]
			}
		}
		recovered[missingIdx] = rec
	}

	// Verify we have all data shards now.
	for i := 0; i < dataShards; i++ {
		if recovered[i] == nil {
			return nil, fmt.Errorf("%w: could not recover shard %d",
				ErrTooFewShards, i)
		}
	}

	result := make([]byte, 0, shardSize*dataShards)
	for i := 0; i < dataShards; i++ {
		result = append(result, recovered[i]...)
	}
	return result, nil
}
