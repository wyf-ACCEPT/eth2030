package types

import (
	"math/big"
	"sync/atomic"
	"unsafe"
)

// Header represents an Ethereum block header.
type Header struct {
	ParentHash  Hash
	UncleHash   Hash
	Coinbase    Address
	Root        Hash
	TxHash      Hash
	ReceiptHash Hash
	Bloom       Bloom
	Difficulty  *big.Int
	Number      *big.Int
	GasLimit    uint64
	GasUsed     uint64
	Time        uint64
	Extra       []byte
	MixDigest   Hash
	Nonce       BlockNonce

	// EIP-1559
	BaseFee *big.Int

	// EIP-4895: Beacon chain push withdrawals
	WithdrawalsHash *Hash

	// EIP-4844: Shard blob transactions
	BlobGasUsed   *uint64
	ExcessBlobGas *uint64

	// EIP-4788: Beacon block root in the EVM
	ParentBeaconRoot *Hash

	// EIP-7685: General purpose execution layer requests
	RequestsHash *Hash

	// EIP-7928: Block-level access list
	BlockAccessListHash *Hash

	// EIP-7706: Separate gas type for calldata
	CalldataGasUsed   *uint64
	CalldataExcessGas *uint64

	// Cache fields (not serialized).
	hash atomic.Pointer[Hash]
	size atomic.Uint64
}

// Hash returns the keccak256 hash of the RLP-encoded header.
func (h *Header) Hash() Hash {
	if cached := h.hash.Load(); cached != nil {
		return *cached
	}
	hash := computeHeaderHash(h)
	h.hash.Store(&hash)
	return hash
}

// Size returns the approximate memory footprint of the header in bytes.
func (h *Header) Size() uint64 {
	if cached := h.size.Load(); cached != 0 {
		return cached
	}
	s := unsafe.Sizeof(*h)
	if h.Difficulty != nil {
		s += unsafe.Sizeof(*h.Difficulty)
	}
	if h.Number != nil {
		s += unsafe.Sizeof(*h.Number)
	}
	if h.BaseFee != nil {
		s += unsafe.Sizeof(*h.BaseFee)
	}
	s += uintptr(len(h.Extra))
	size := uint64(s)
	h.size.Store(size)
	return size
}
