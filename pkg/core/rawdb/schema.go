package rawdb

import "encoding/binary"

// Key prefixes for the database schema.
// Following go-ethereum's prefix-based approach to avoid key collisions.
var (
	// Header data
	headerPrefix       = []byte("h") // h + num (8 bytes BE) + hash -> header RLP
	headerNumberPrefix = []byte("H") // H + hash -> num (8 bytes BE)

	// Block body data
	bodyPrefix = []byte("b") // b + num (8 bytes BE) + hash -> body RLP

	// Receipt data
	receiptPrefix = []byte("r") // r + num (8 bytes BE) + hash -> receipts RLP

	// Transaction lookup
	txLookupPrefix = []byte("l") // l + tx hash -> block num (8 bytes BE)

	// Canonical chain
	canonicalPrefix = []byte("c")  // c + num (8 bytes BE) -> canonical hash
	headHeaderKey   = []byte("hh") // -> hash of the current head header
	headBlockKey    = []byte("hb") // -> hash of the current head block

	// Contract code
	codePrefix = []byte("C") // C + code hash -> contract bytecode

	// State trie nodes (optional for future use)
	trieNodePrefix = []byte("t") // t + node hash -> trie node data
)

// encodeBlockNumber encodes a block number as an 8-byte big-endian value.
func encodeBlockNumber(number uint64) []byte {
	enc := make([]byte, 8)
	binary.BigEndian.PutUint64(enc, number)
	return enc
}

// headerKey = headerPrefix + num + hash
func headerKey(number uint64, hash [32]byte) []byte {
	return append(append(headerPrefix, encodeBlockNumber(number)...), hash[:]...)
}

// headerNumberKey = headerNumberPrefix + hash
func headerNumberKey(hash [32]byte) []byte {
	return append(headerNumberPrefix, hash[:]...)
}

// bodyKey = bodyPrefix + num + hash
func bodyKey(number uint64, hash [32]byte) []byte {
	return append(append(bodyPrefix, encodeBlockNumber(number)...), hash[:]...)
}

// receiptKey = receiptPrefix + num + hash
func receiptKey(number uint64, hash [32]byte) []byte {
	return append(append(receiptPrefix, encodeBlockNumber(number)...), hash[:]...)
}

// txLookupKey = txLookupPrefix + txHash
func txLookupKey(txHash [32]byte) []byte {
	return append(txLookupPrefix, txHash[:]...)
}

// canonicalKey = canonicalPrefix + num
func canonicalKey(number uint64) []byte {
	return append(canonicalPrefix, encodeBlockNumber(number)...)
}

// codeKey = codePrefix + codeHash
func codeKey(codeHash [32]byte) []byte {
	return append(codePrefix, codeHash[:]...)
}
