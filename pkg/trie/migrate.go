package trie

import (
	"github.com/eth2030/eth2030/crypto"
)

// MigrateFromMPT converts an MPT trie to a binary Merkle trie. Each key-value
// pair from the MPT is re-inserted into the binary trie with the key hashed
// via keccak256 (matching the binary trie's key derivation).
func MigrateFromMPT(mpt *Trie) *BinaryTrie {
	bt := NewBinaryTrie()
	it := NewIterator(mpt)
	for it.Next() {
		hk := crypto.Keccak256Hash(it.Key)
		bt.PutHashed(hk, it.Value)
	}
	return bt
}
