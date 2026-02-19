package types

import (
	"bytes"
)

// EIP-7702 SetCode constants.
const (
	// AuthMagic is the signing magic byte for EIP-7702 authorization hashes.
	// The authorization hash is: keccak256(0x05 || rlp([chain_id, address, nonce]))
	AuthMagic byte = 0x05

	// PerAuthBaseCost is the gas charged per authorization entry (EIP-7702).
	PerAuthBaseCost uint64 = 12500

	// PerEmptyAccountCost is the additional gas charged per authorization
	// entry that targets an empty (non-existent) account.
	PerEmptyAccountCost uint64 = 25000
)

// DelegationPrefix is the EIP-7702 delegation designator prefix.
// Code starting with this prefix indicates account code delegation.
var DelegationPrefix = []byte{0xef, 0x01, 0x00}

// ParseDelegation extracts the target address from delegation code.
// Returns the delegated address and true if b is exactly 23 bytes
// with the 0xef0100 prefix. Returns zero address and false otherwise.
func ParseDelegation(b []byte) (Address, bool) {
	if len(b) != len(DelegationPrefix)+AddressLength {
		return Address{}, false
	}
	if !bytes.HasPrefix(b, DelegationPrefix) {
		return Address{}, false
	}
	return BytesToAddress(b[len(DelegationPrefix):]), true
}

// AddressToDelegation creates delegation designator code: 0xef0100 || address.
func AddressToDelegation(addr Address) []byte {
	code := make([]byte, len(DelegationPrefix)+AddressLength)
	copy(code, DelegationPrefix)
	copy(code[len(DelegationPrefix):], addr[:])
	return code
}

// HasDelegationPrefix returns whether the code starts with the delegation prefix.
func HasDelegationPrefix(code []byte) bool {
	return bytes.HasPrefix(code, DelegationPrefix)
}
