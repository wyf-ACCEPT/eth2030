// hash_extended.go adds comprehensive BAL hashing: access list merkle root
// computation, address/slot hashing, conflict detection hashing, and parallel
// hash computation for Block Access Lists (EIP-7928).
package bal

import (
	"encoding/binary"
	"sort"
	"sync"

	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/crypto"
)

// MerkleRoot computes a merkle root over all access entries in the BAL.
// Each leaf is the keccak256 hash of the RLP-encoded entry. The tree is
// built bottom-up by hashing pairs; an odd leaf is promoted unchanged.
func (bal *BlockAccessList) MerkleRoot() types.Hash {
	if bal == nil || len(bal.Entries) == 0 {
		return types.Hash{}
	}

	leaves := make([]types.Hash, len(bal.Entries))
	for i, entry := range bal.Entries {
		leaves[i] = HashAccessEntry(&entry)
	}
	return computeMerkleRoot(leaves)
}

// computeMerkleRoot builds a binary merkle tree from leaf hashes.
func computeMerkleRoot(leaves []types.Hash) types.Hash {
	if len(leaves) == 0 {
		return types.Hash{}
	}
	if len(leaves) == 1 {
		return leaves[0]
	}

	// Iterative bottom-up construction.
	layer := make([]types.Hash, len(leaves))
	copy(layer, leaves)

	for len(layer) > 1 {
		next := make([]types.Hash, 0, (len(layer)+1)/2)
		for i := 0; i < len(layer); i += 2 {
			if i+1 < len(layer) {
				next = append(next, hashPair(layer[i], layer[i+1]))
			} else {
				// Odd leaf is promoted.
				next = append(next, layer[i])
			}
		}
		layer = next
	}
	return layer[0]
}

// hashPair hashes two hashes together using keccak256(left || right).
func hashPair(left, right types.Hash) types.Hash {
	return crypto.Keccak256Hash(left[:], right[:])
}

// HashAccessEntry computes the keccak256 hash of a single access entry.
// The hash covers: address, access index, all storage reads, all storage
// changes, balance change, nonce change, and code change.
func HashAccessEntry(entry *AccessEntry) types.Hash {
	if entry == nil {
		return types.Hash{}
	}

	var buf []byte
	buf = append(buf, entry.Address[:]...)

	var idxBuf [8]byte
	binary.BigEndian.PutUint64(idxBuf[:], entry.AccessIndex)
	buf = append(buf, idxBuf[:]...)

	for _, sr := range entry.StorageReads {
		buf = append(buf, sr.Slot[:]...)
		buf = append(buf, sr.Value[:]...)
	}
	for _, sc := range entry.StorageChanges {
		buf = append(buf, sc.Slot[:]...)
		buf = append(buf, sc.OldValue[:]...)
		buf = append(buf, sc.NewValue[:]...)
	}

	if entry.BalanceChange != nil {
		if entry.BalanceChange.OldValue != nil {
			buf = append(buf, entry.BalanceChange.OldValue.Bytes()...)
		}
		buf = append(buf, 0xff) // separator
		if entry.BalanceChange.NewValue != nil {
			buf = append(buf, entry.BalanceChange.NewValue.Bytes()...)
		}
	}
	if entry.NonceChange != nil {
		var nb [16]byte
		binary.BigEndian.PutUint64(nb[:8], entry.NonceChange.OldValue)
		binary.BigEndian.PutUint64(nb[8:], entry.NonceChange.NewValue)
		buf = append(buf, nb[:]...)
	}
	if entry.CodeChange != nil {
		buf = append(buf, entry.CodeChange.OldCode...)
		buf = append(buf, 0xfe) // separator
		buf = append(buf, entry.CodeChange.NewCode...)
	}

	return crypto.Keccak256Hash(buf)
}

// HashAddressSlot computes a deterministic hash for an (address, slot) pair.
// Useful as a map key or conflict detection identifier.
func HashAddressSlot(addr types.Address, slot types.Hash) types.Hash {
	return crypto.Keccak256Hash(addr[:], slot[:])
}

// HashConflictPair creates a hash representing a conflict between two
// transaction indices on a specific address and slot.
func HashConflictPair(txA, txB int, addr types.Address, slot types.Hash) types.Hash {
	var buf [8]byte
	binary.BigEndian.PutUint32(buf[:4], uint32(txA))
	binary.BigEndian.PutUint32(buf[4:], uint32(txB))
	return crypto.Keccak256Hash(buf[:], addr[:], slot[:])
}

// ConflictSetHash computes an aggregate hash over all conflicts for a BAL.
// Conflicts are sorted before hashing for determinism.
func ConflictSetHash(conflicts []Conflict) types.Hash {
	if len(conflicts) == 0 {
		return types.Hash{}
	}

	sorted := make([]Conflict, len(conflicts))
	copy(sorted, conflicts)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].TxA != sorted[j].TxA {
			return sorted[i].TxA < sorted[j].TxA
		}
		if sorted[i].TxB != sorted[j].TxB {
			return sorted[i].TxB < sorted[j].TxB
		}
		return sorted[i].Type < sorted[j].Type
	})

	var buf []byte
	for _, c := range sorted {
		h := HashConflictPair(c.TxA, c.TxB, c.Address, c.Slot)
		buf = append(buf, h[:]...)
		buf = append(buf, byte(c.Type))
	}
	return crypto.Keccak256Hash(buf)
}

// ParallelMerkleRoot computes the BAL merkle root using parallel hashing.
// It divides the entry hashing across the given number of workers.
func ParallelMerkleRoot(bal *BlockAccessList, workers int) types.Hash {
	if bal == nil || len(bal.Entries) == 0 {
		return types.Hash{}
	}
	if workers < 1 {
		workers = 1
	}
	if workers > len(bal.Entries) {
		workers = len(bal.Entries)
	}

	leaves := make([]types.Hash, len(bal.Entries))

	var wg sync.WaitGroup
	chunkSize := (len(bal.Entries) + workers - 1) / workers

	for w := 0; w < workers; w++ {
		start := w * chunkSize
		end := start + chunkSize
		if end > len(bal.Entries) {
			end = len(bal.Entries)
		}
		if start >= end {
			break
		}

		wg.Add(1)
		go func(s, e int) {
			defer wg.Done()
			for i := s; i < e; i++ {
				leaves[i] = HashAccessEntry(&bal.Entries[i])
			}
		}(start, end)
	}
	wg.Wait()

	return computeMerkleRoot(leaves)
}

// EntryHashes returns the keccak256 hash of each access entry.
func EntryHashes(bal *BlockAccessList) []types.Hash {
	if bal == nil {
		return nil
	}
	hashes := make([]types.Hash, len(bal.Entries))
	for i, entry := range bal.Entries {
		hashes[i] = HashAccessEntry(&entry)
	}
	return hashes
}

// VerifyMerkleRoot checks if the given root matches the BAL's computed root.
func VerifyMerkleRoot(bal *BlockAccessList, expectedRoot types.Hash) bool {
	return bal.MerkleRoot() == expectedRoot
}
