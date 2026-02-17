// Package types defines core Ethereum data structures.
package types

import (
	"encoding/hex"
	"fmt"
	"math/big"
)

const (
	HashLength    = 32
	AddressLength = 20
	BloomLength   = 256
	NonceLength   = 8
)

// Hash represents the 32-byte Keccak256 hash of data.
type Hash [HashLength]byte

// Address represents the 20-byte address of an Ethereum account.
type Address [AddressLength]byte

// Bloom represents a 2048-bit bloom filter.
type Bloom [BloomLength]byte

// BlockNonce is the 8-byte block nonce (legacy PoW, always zero post-merge).
type BlockNonce [NonceLength]byte

// BytesToHash converts bytes to Hash, left-padding if shorter than 32 bytes.
func BytesToHash(b []byte) Hash {
	var h Hash
	h.SetBytes(b)
	return h
}

// HexToHash converts a hex string to Hash.
func HexToHash(s string) Hash {
	return BytesToHash(fromHex(s))
}

// Bytes returns the byte representation of the hash.
func (h Hash) Bytes() []byte { return h[:] }

// Hex returns the hex string representation of the hash.
func (h Hash) Hex() string { return fmt.Sprintf("0x%x", h[:]) }

// SetBytes sets the hash from a byte slice, left-padding if necessary.
func (h *Hash) SetBytes(b []byte) {
	if len(b) > HashLength {
		b = b[len(b)-HashLength:]
	}
	copy(h[HashLength-len(b):], b)
}

// IsZero returns whether the hash is all zeros.
func (h Hash) IsZero() bool {
	return h == Hash{}
}

// String implements fmt.Stringer.
func (h Hash) String() string { return h.Hex() }

// BytesToAddress converts bytes to Address, left-padding if shorter than 20 bytes.
func BytesToAddress(b []byte) Address {
	var a Address
	a.SetBytes(b)
	return a
}

// HexToAddress converts a hex string to Address.
func HexToAddress(s string) Address {
	return BytesToAddress(fromHex(s))
}

// Bytes returns the byte representation of the address.
func (a Address) Bytes() []byte { return a[:] }

// Hex returns the hex string representation of the address.
func (a Address) Hex() string { return fmt.Sprintf("0x%x", a[:]) }

// SetBytes sets the address from a byte slice.
func (a *Address) SetBytes(b []byte) {
	if len(b) > AddressLength {
		b = b[len(b)-AddressLength:]
	}
	copy(a[AddressLength-len(b):], b)
}

// IsZero returns whether the address is all zeros.
func (a Address) IsZero() bool {
	return a == Address{}
}

// String implements fmt.Stringer.
func (a Address) String() string { return a.Hex() }

// Account represents an Ethereum account in the state trie.
type Account struct {
	Nonce    uint64
	Balance  *big.Int
	Root     Hash   // storage trie root (emptyRoot for no storage)
	CodeHash []byte // keccak256 of code (emptyCodeHash for EOAs)
}

// NewAccount creates a new account with zero balance and empty storage.
func NewAccount() Account {
	return Account{
		Balance:  new(big.Int),
		CodeHash: EmptyCodeHash.Bytes(),
		Root:     EmptyRootHash,
	}
}

// Log represents a contract log event.
type Log struct {
	Address     Address
	Topics      []Hash
	Data        []byte
	BlockNumber uint64
	TxHash      Hash
	TxIndex     uint
	BlockHash   Hash
	Index       uint
	Removed     bool
}

var (
	// EmptyRootHash is the hash of an empty state trie.
	EmptyRootHash = HexToHash("56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421")

	// EmptyCodeHash is the hash of empty EVM bytecode (keccak256 of empty string).
	EmptyCodeHash = HexToHash("c5d2460186f7233c927e7db2dcc703c0e500b653ca82273b7bfad8045d85a470")

	// EmptyUncleHash is the hash of an empty uncle list (keccak256 of RLP of empty list).
	EmptyUncleHash = HexToHash("1dcc4de8dec75d7aab85b567b6ccd41ad312451b948a7413f0a142fd40d49347")
)

// fromHex decodes a hex string, stripping optional "0x" prefix.
func fromHex(s string) []byte {
	if has0xPrefix(s) {
		s = s[2:]
	}
	if len(s)%2 == 1 {
		s = "0" + s
	}
	b, _ := hex.DecodeString(s)
	return b
}

func has0xPrefix(s string) bool {
	return len(s) >= 2 && s[0] == '0' && (s[1] == 'x' || s[1] == 'X')
}
