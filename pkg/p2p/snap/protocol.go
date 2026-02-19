// Package snap implements the snap/1 wire protocol for Ethereum state
// synchronization. It defines message types for requesting and serving
// account ranges, storage ranges, bytecodes, and trie nodes.
package snap

import (
	"github.com/eth2028/eth2028/core/types"
)

// Protocol version and name.
const (
	ProtocolName    = "snap"
	ProtocolVersion = 1
)

// Message codes for the snap/1 protocol.
const (
	GetAccountRangeMsg  uint64 = 0x00
	AccountRangeMsg     uint64 = 0x01
	GetStorageRangesMsg uint64 = 0x02
	StorageRangesMsg    uint64 = 0x03
	GetByteCodesMsg     uint64 = 0x04
	ByteCodesMsg        uint64 = 0x05
	GetTrieNodesMsg     uint64 = 0x06
	TrieNodesMsg        uint64 = 0x07
)

// Response size limits.
const (
	// SoftResponseLimit is the target maximum size of responses (500 KB).
	SoftResponseLimit = 500 * 1024

	// HardResponseLimit is the absolute maximum size of responses (2 MB).
	HardResponseLimit = 2 * 1024 * 1024

	// MaxAccountRangeCount is the maximum number of accounts per range response.
	MaxAccountRangeCount = 256

	// MaxStorageRangeCount is the maximum number of storage slots per response.
	MaxStorageRangeCount = 512

	// MaxByteCodeCount is the maximum number of bytecodes per response.
	MaxByteCodeCount = 64

	// MaxTrieNodeCount is the maximum number of trie nodes per response.
	MaxTrieNodeCount = 512
)

// GetAccountRangePacket requests a range of accounts from the state trie.
type GetAccountRangePacket struct {
	ID     uint64     // Request identifier.
	Root   types.Hash // State trie root to query against.
	Origin types.Hash // Account hash range start (inclusive).
	Limit  types.Hash // Account hash range end (inclusive).
	Bytes  uint64     // Soft limit on response size in bytes.
}

// AccountData is a single account in a range response, with its hash and
// RLP-encoded slim account body.
type AccountData struct {
	Hash types.Hash // Keccak256 of the account address.
	Body []byte     // RLP-encoded account (nonce, balance, root, codehash).
}

// AccountRangePacket is the response to GetAccountRangePacket.
type AccountRangePacket struct {
	ID       uint64        // Echoed request identifier.
	Accounts []AccountData // Accounts in the requested range.
	Proof    [][]byte      // Merkle proof for range boundaries.
}

// GetStorageRangesPacket requests storage slot ranges for a set of accounts.
type GetStorageRangesPacket struct {
	ID       uint64       // Request identifier.
	Root     types.Hash   // State trie root.
	Accounts []types.Hash // Account hashes to query.
	Origin   []byte       // Storage hash range start (may be empty for full range).
	Limit    []byte       // Storage hash range end (may be empty for full range).
	Bytes    uint64       // Soft limit on response size in bytes.
}

// StorageData is a single storage slot in a range response.
type StorageData struct {
	Hash types.Hash // Keccak256 of the storage key.
	Body []byte     // RLP-encoded storage value.
}

// StorageRangesPacket is the response to GetStorageRangesPacket.
type StorageRangesPacket struct {
	ID    uint64          // Echoed request identifier.
	Slots [][]StorageData // Slots per requested account (parallel array).
	Proof [][]byte        // Merkle proof for the last account's range boundary.
}

// GetByteCodesPacket requests contract bytecodes by their code hashes.
type GetByteCodesPacket struct {
	ID     uint64       // Request identifier.
	Hashes []types.Hash // Code hashes to retrieve.
	Bytes  uint64       // Soft limit on response size in bytes.
}

// ByteCodesPacket is the response to GetByteCodesPacket.
type ByteCodesPacket struct {
	ID    uint64   // Echoed request identifier.
	Codes [][]byte // Retrieved bytecodes (parallel to requested hashes).
}

// TrieNodePathSet is a set of trie node paths to request. The first element
// is the account hash (or empty for the account trie), and subsequent
// elements are the path components within the storage trie.
type TrieNodePathSet [][]byte

// GetTrieNodesPacket requests trie nodes by path.
type GetTrieNodesPacket struct {
	ID    uint64             // Request identifier.
	Root  types.Hash         // State trie root.
	Paths []TrieNodePathSet  // Sets of trie node paths to retrieve.
	Bytes uint64             // Soft limit on response size in bytes.
}

// TrieNodesPacket is the response to GetTrieNodesPacket.
type TrieNodesPacket struct {
	ID    uint64   // Echoed request identifier.
	Nodes [][]byte // Retrieved trie node blobs.
}

// Handler defines the interface for processing incoming snap protocol messages.
type Handler interface {
	// HandleGetAccountRange processes a request for an account range.
	HandleGetAccountRange(req *GetAccountRangePacket) (*AccountRangePacket, error)

	// HandleGetStorageRanges processes a request for storage ranges.
	HandleGetStorageRanges(req *GetStorageRangesPacket) (*StorageRangesPacket, error)

	// HandleGetByteCodes processes a request for contract bytecodes.
	HandleGetByteCodes(req *GetByteCodesPacket) (*ByteCodesPacket, error)

	// HandleGetTrieNodes processes a request for trie nodes.
	HandleGetTrieNodes(req *GetTrieNodesPacket) (*TrieNodesPacket, error)
}
