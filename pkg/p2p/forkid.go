package p2p

import (
	"encoding/binary"
	"hash/crc32"
	"sort"

	"github.com/eth2028/eth2028/core/types"
)

// CalcForkID computes the EIP-2124 fork identifier from a genesis hash, the
// current head block number, and the set of known fork block numbers.
//
// The algorithm:
//  1. Start with CRC32(genesisHash).
//  2. For each fork block that has already been passed (fork <= head),
//     update CRC32 with the fork block number encoded as big-endian uint64.
//  3. The first fork block not yet passed becomes ForkID.Next.
//  4. If all forks have passed, Next is 0.
func CalcForkID(genesisHash types.Hash, head uint64, forkBlocks []uint64) ForkID {
	// Start with CRC32 of the genesis hash.
	hash := crc32.ChecksumIEEE(genesisHash[:])

	// Deduplicate and sort fork blocks, removing 0 (genesis).
	forks := cleanForks(forkBlocks)

	for _, fork := range forks {
		if fork <= head {
			// Fork already passed, fold into the checksum.
			hash = checksumUpdate(hash, fork)
			continue
		}
		// Found the next upcoming fork.
		return ForkID{
			Hash: checksumToBytes(hash),
			Next: fork,
		}
	}
	// All known forks passed.
	return ForkID{
		Hash: checksumToBytes(hash),
		Next: 0,
	}
}

// checksumUpdate folds a fork block number into the running CRC32 checksum.
func checksumUpdate(hash uint32, fork uint64) uint32 {
	var blob [8]byte
	binary.BigEndian.PutUint64(blob[:], fork)
	return crc32.Update(hash, crc32.IEEETable, blob[:])
}

// checksumToBytes converts a CRC32 checksum to [4]byte big-endian.
func checksumToBytes(hash uint32) [4]byte {
	var b [4]byte
	binary.BigEndian.PutUint32(b[:], hash)
	return b
}

// cleanForks deduplicates and sorts fork block numbers, removing zero values.
func cleanForks(forks []uint64) []uint64 {
	if len(forks) == 0 {
		return nil
	}
	// Copy to avoid mutating the caller's slice.
	cp := make([]uint64, len(forks))
	copy(cp, forks)
	sort.Slice(cp, func(i, j int) bool { return cp[i] < cp[j] })

	// Deduplicate and skip zero.
	result := make([]uint64, 0, len(cp))
	for i, f := range cp {
		if f == 0 {
			continue
		}
		if i > 0 && f == cp[i-1] {
			continue
		}
		result = append(result, f)
	}
	return result
}
