package types

import (
	"math/big"
	"sync/atomic"
	"unsafe"
)

// Withdrawal represents a validator withdrawal from the beacon chain.
type Withdrawal struct {
	Index          uint64
	ValidatorIndex uint64
	Address        Address
	Amount         uint64 // in Gwei
}

// Body contains the transactions and auxiliary data of a block.
type Body struct {
	Transactions []*Transaction
	Uncles       []*Header
	Withdrawals  []*Withdrawal
}

// Block represents an Ethereum block.
type Block struct {
	header *Header
	body   Body

	hash atomic.Pointer[Hash]
	size atomic.Uint64
}

// NewBlock creates a new block with the given header and body.
// A nil body is treated as an empty body.
func NewBlock(header *Header, body *Body) *Block {
	b := &Block{header: copyHeader(header)}
	if body != nil {
		b.body.Transactions = make([]*Transaction, len(body.Transactions))
		copy(b.body.Transactions, body.Transactions)

		b.body.Uncles = make([]*Header, len(body.Uncles))
		for i, uncle := range body.Uncles {
			b.body.Uncles[i] = copyHeader(uncle)
		}

		if body.Withdrawals != nil {
			b.body.Withdrawals = make([]*Withdrawal, len(body.Withdrawals))
			for i, w := range body.Withdrawals {
				wCopy := *w
				b.body.Withdrawals[i] = &wCopy
			}
		}
	}
	return b
}

// Header returns the block header (copy).
func (b *Block) Header() *Header { return copyHeader(b.header) }

// Body returns the block body.
func (b *Block) Body() *Body {
	return &Body{
		Transactions: b.body.Transactions,
		Uncles:       b.body.Uncles,
		Withdrawals:  b.body.Withdrawals,
	}
}

// Transactions returns the block's transactions.
func (b *Block) Transactions() []*Transaction { return b.body.Transactions }

// Uncles returns the block's uncle headers.
func (b *Block) Uncles() []*Header { return b.body.Uncles }

// Withdrawals returns the block's withdrawals (nil if pre-Shanghai).
func (b *Block) Withdrawals() []*Withdrawal { return b.body.Withdrawals }

// Number returns the block number.
func (b *Block) Number() *big.Int {
	if b.header.Number == nil {
		return new(big.Int)
	}
	return new(big.Int).Set(b.header.Number)
}

// NumberU64 returns the block number as uint64.
func (b *Block) NumberU64() uint64 {
	if b.header.Number == nil {
		return 0
	}
	return b.header.Number.Uint64()
}

// GasLimit returns the gas limit of the block.
func (b *Block) GasLimit() uint64 { return b.header.GasLimit }

// GasUsed returns the gas used in the block.
func (b *Block) GasUsed() uint64 { return b.header.GasUsed }

// Time returns the block timestamp.
func (b *Block) Time() uint64 { return b.header.Time }

// Difficulty returns the block difficulty.
func (b *Block) Difficulty() *big.Int {
	if b.header.Difficulty == nil {
		return new(big.Int)
	}
	return new(big.Int).Set(b.header.Difficulty)
}

// BaseFee returns the base fee of the block (nil if pre-EIP-1559).
func (b *Block) BaseFee() *big.Int {
	if b.header.BaseFee == nil {
		return nil
	}
	return new(big.Int).Set(b.header.BaseFee)
}

// ParentHash returns the parent block hash.
func (b *Block) ParentHash() Hash { return b.header.ParentHash }

// TxHash returns the transactions root hash.
func (b *Block) TxHash() Hash { return b.header.TxHash }

// ReceiptHash returns the receipts root hash.
func (b *Block) ReceiptHash() Hash { return b.header.ReceiptHash }

// UncleHash returns the uncle hash.
func (b *Block) UncleHash() Hash { return b.header.UncleHash }

// Root returns the state root hash.
func (b *Block) Root() Hash { return b.header.Root }

// Coinbase returns the block coinbase/miner address.
func (b *Block) Coinbase() Address { return b.header.Coinbase }

// Bloom returns the block bloom filter.
func (b *Block) Bloom() Bloom { return b.header.Bloom }

// MixDigest returns the block mix digest.
func (b *Block) MixDigest() Hash { return b.header.MixDigest }

// Nonce returns the block nonce.
func (b *Block) Nonce() BlockNonce { return b.header.Nonce }

// Extra returns the block extra data.
func (b *Block) Extra() []byte { return b.header.Extra }

// Hash returns the keccak256 hash of the block header.
func (b *Block) Hash() Hash {
	if cached := b.hash.Load(); cached != nil {
		return *cached
	}
	h := b.header.Hash()
	b.hash.Store(&h)
	return h
}

// Size returns the approximate memory footprint of the block.
func (b *Block) Size() uint64 {
	if cached := b.size.Load(); cached != 0 {
		return cached
	}
	s := unsafe.Sizeof(*b)
	s += unsafe.Sizeof(*b.header)
	for _, tx := range b.body.Transactions {
		s += uintptr(tx.Size())
	}
	for _, uncle := range b.body.Uncles {
		s += uintptr(uncle.Size())
	}
	size := uint64(s)
	b.size.Store(size)
	return size
}

// copyHeader creates a deep copy of a header.
func copyHeader(h *Header) *Header {
	// Copy field-by-field to avoid copying atomic fields (hash, size).
	cpy := Header{
		ParentHash:  h.ParentHash,
		UncleHash:   h.UncleHash,
		Coinbase:    h.Coinbase,
		Root:        h.Root,
		TxHash:      h.TxHash,
		ReceiptHash: h.ReceiptHash,
		Bloom:       h.Bloom,
		GasLimit:    h.GasLimit,
		GasUsed:     h.GasUsed,
		Time:        h.Time,
		MixDigest: h.MixDigest,
		Nonce:     h.Nonce,
	}

	if h.Difficulty != nil {
		cpy.Difficulty = new(big.Int).Set(h.Difficulty)
	}
	if h.Number != nil {
		cpy.Number = new(big.Int).Set(h.Number)
	}
	if h.BaseFee != nil {
		cpy.BaseFee = new(big.Int).Set(h.BaseFee)
	}
	if len(h.Extra) > 0 {
		cpy.Extra = make([]byte, len(h.Extra))
		copy(cpy.Extra, h.Extra)
	}
	if h.WithdrawalsHash != nil {
		wh := *h.WithdrawalsHash
		cpy.WithdrawalsHash = &wh
	}
	if h.BlobGasUsed != nil {
		bgu := *h.BlobGasUsed
		cpy.BlobGasUsed = &bgu
	}
	if h.ExcessBlobGas != nil {
		ebg := *h.ExcessBlobGas
		cpy.ExcessBlobGas = &ebg
	}
	if h.ParentBeaconRoot != nil {
		pbr := *h.ParentBeaconRoot
		cpy.ParentBeaconRoot = &pbr
	}
	if h.RequestsHash != nil {
		rh := *h.RequestsHash
		cpy.RequestsHash = &rh
	}
	if h.BlockAccessListHash != nil {
		bah := *h.BlockAccessListHash
		cpy.BlockAccessListHash = &bah
	}
	return &cpy
}
