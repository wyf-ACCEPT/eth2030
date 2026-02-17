package core

import (
	"bytes"
	"errors"
	"fmt"
	"math/big"

	"github.com/eth2028/eth2028/core/state"
	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/crypto"
)

const (
	// delegationPrefix is the EIP-7702 delegation designator prefix (0xef0100).
	// Code starting with this prefix indicates that the account has delegated
	// its code execution to the address encoded in the remaining 20 bytes.
	delegationPrefixLen = 3

	// delegationCodeLen is the total length of a delegation designator:
	// 3 bytes prefix (0xef0100) + 20 bytes address = 23 bytes.
	delegationCodeLen = delegationPrefixLen + types.AddressLength

	// EIP-7702 authorization signing magic byte.
	// The authorization hash is: keccak256(0x05 || rlp([chain_id, address, nonce]))
	authMagic = 0x05
)

// delegationPrefix bytes for matching and construction.
var delegationPrefixBytes = []byte{0xef, 0x01, 0x00}

var (
	ErrAuthChainID    = errors.New("authorization chain ID mismatch")
	ErrAuthNonce      = errors.New("authorization nonce mismatch")
	ErrAuthSignature  = errors.New("authorization signature recovery failed")
	ErrAuthInvalidSig = errors.New("authorization signature values invalid")
)

// ProcessAuthorizations processes EIP-7702 authorization entries for a SetCode
// transaction. For each authorization, it verifies the chain ID, nonce, and
// signature, then sets the signer's code to a delegation designator pointing
// to the authorized address.
//
// Per EIP-7702, invalid authorizations are silently skipped (they do not cause
// the transaction to fail). The function only returns an error for truly
// unrecoverable situations.
func ProcessAuthorizations(statedb state.StateDB, authorizations []types.Authorization, chainID *big.Int) error {
	for i := range authorizations {
		if err := processOneAuthorization(statedb, &authorizations[i], chainID); err != nil {
			// Per EIP-7702: invalid authorizations are skipped, not fatal.
			// In a production implementation, these would be logged.
			continue
		}
	}
	return nil
}

// processOneAuthorization processes a single authorization entry.
func processOneAuthorization(statedb state.StateDB, auth *types.Authorization, chainID *big.Int) error {
	// 1. Verify chain ID: must match the current chain, or be 0 (any-chain wildcard).
	if auth.ChainID != nil && auth.ChainID.Sign() != 0 {
		if chainID == nil || auth.ChainID.Cmp(chainID) != 0 {
			return ErrAuthChainID
		}
	}

	// 2. Validate signature values (r, s must be valid, v must be 0 or 1).
	v := byte(0)
	if auth.V != nil {
		if !auth.V.IsUint64() || auth.V.Uint64() > 1 {
			return ErrAuthInvalidSig
		}
		v = byte(auth.V.Uint64())
	}
	if !crypto.ValidateSignatureValues(v, auth.R, auth.S, true) {
		return ErrAuthInvalidSig
	}

	// 3. Compute the authorization hash and recover the signer.
	authHash := computeAuthorizationHash(auth)

	// Build the 65-byte signature: R (32 bytes) || S (32 bytes) || V (1 byte)
	sig := make([]byte, 65)
	if auth.R != nil {
		rBytes := auth.R.Bytes()
		copy(sig[32-len(rBytes):32], rBytes)
	}
	if auth.S != nil {
		sBytes := auth.S.Bytes()
		copy(sig[64-len(sBytes):64], sBytes)
	}
	sig[64] = v

	pubBytes, err := crypto.Ecrecover(authHash, sig)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrAuthSignature, err)
	}

	// Derive the signer address from the recovered public key.
	signerAddr := types.BytesToAddress(crypto.Keccak256(pubBytes[1:])[12:])

	// 4. Verify the nonce matches the signer's current nonce.
	currentNonce := statedb.GetNonce(signerAddr)
	if auth.Nonce != currentNonce {
		return ErrAuthNonce
	}

	// 5. Set the signer's code to the delegation designator: 0xef0100 || address.
	delegationCode := makeDelegationCode(auth.Address)
	statedb.SetCode(signerAddr, delegationCode)

	// 6. Increment the signer's nonce.
	statedb.SetNonce(signerAddr, currentNonce+1)

	return nil
}

// computeAuthorizationHash computes the EIP-7702 authorization signing hash.
// The hash is: keccak256(0x05 || rlp([chain_id, address, nonce]))
//
// We use a simplified RLP encoding here since the structure is fixed.
func computeAuthorizationHash(auth *types.Authorization) []byte {
	// Encode the RLP list: [chain_id, address, nonce]
	chainIDBytes := encodeBigIntRLP(auth.ChainID)
	addressBytes := encodeFixedBytesRLP(auth.Address[:])
	nonceBytes := encodeUint64RLP(auth.Nonce)

	// List payload
	payload := make([]byte, 0, len(chainIDBytes)+len(addressBytes)+len(nonceBytes))
	payload = append(payload, chainIDBytes...)
	payload = append(payload, addressBytes...)
	payload = append(payload, nonceBytes...)

	// RLP list header
	listData := encodeListHeaderRLP(payload)

	// Prepend the magic byte
	msg := make([]byte, 0, 1+len(listData))
	msg = append(msg, authMagic)
	msg = append(msg, listData...)

	return crypto.Keccak256(msg)
}

// makeDelegationCode creates the delegation designator code: 0xef0100 || address.
func makeDelegationCode(addr types.Address) []byte {
	code := make([]byte, delegationCodeLen)
	copy(code, delegationPrefixBytes)
	copy(code[delegationPrefixLen:], addr[:])
	return code
}

// IsDelegated checks if the given code starts with the EIP-7702 delegation
// designator prefix (0xef0100). Delegated code indicates the account has
// delegated its execution to another address.
func IsDelegated(code []byte) bool {
	if len(code) < delegationPrefixLen {
		return false
	}
	return bytes.HasPrefix(code, delegationPrefixBytes)
}

// ResolveDelegation extracts the target address from EIP-7702 delegation code.
// Returns the delegation target address and true if the code is a valid
// delegation designator (exactly 23 bytes: 0xef0100 || 20-byte address).
// Returns zero address and false otherwise.
func ResolveDelegation(code []byte) (types.Address, bool) {
	if len(code) != delegationCodeLen {
		return types.Address{}, false
	}
	if !IsDelegated(code) {
		return types.Address{}, false
	}
	var addr types.Address
	copy(addr[:], code[delegationPrefixLen:])
	return addr, true
}

// --- Simplified RLP encoding helpers ---
// These implement just enough RLP to encode the EIP-7702 authorization struct.

// encodeBigIntRLP encodes a big.Int as RLP.
func encodeBigIntRLP(i *big.Int) []byte {
	if i == nil || i.Sign() == 0 {
		// Zero is encoded as empty byte string (0x80)
		return []byte{0x80}
	}
	b := i.Bytes()
	return encodeBytesRLP(b)
}

// encodeUint64RLP encodes a uint64 as RLP.
func encodeUint64RLP(n uint64) []byte {
	if n == 0 {
		return []byte{0x80}
	}
	if n < 128 {
		return []byte{byte(n)}
	}
	// Encode as big-endian bytes with no leading zeros
	b := make([]byte, 8)
	b[0] = byte(n >> 56)
	b[1] = byte(n >> 48)
	b[2] = byte(n >> 40)
	b[3] = byte(n >> 32)
	b[4] = byte(n >> 24)
	b[5] = byte(n >> 16)
	b[6] = byte(n >> 8)
	b[7] = byte(n)
	// Trim leading zeros
	for len(b) > 1 && b[0] == 0 {
		b = b[1:]
	}
	return encodeBytesRLP(b)
}

// encodeBytesRLP encodes a byte slice as RLP.
func encodeBytesRLP(b []byte) []byte {
	if len(b) == 1 && b[0] < 128 {
		return b
	}
	if len(b) <= 55 {
		return append([]byte{0x80 + byte(len(b))}, b...)
	}
	lenBytes := encodeLength(uint64(len(b)))
	header := append([]byte{0xb7 + byte(len(lenBytes))}, lenBytes...)
	return append(header, b...)
}

// encodeFixedBytesRLP encodes a fixed-length byte slice as RLP string.
func encodeFixedBytesRLP(b []byte) []byte {
	return encodeBytesRLP(b)
}

// encodeListHeaderRLP wraps payload bytes in an RLP list header.
func encodeListHeaderRLP(payload []byte) []byte {
	if len(payload) <= 55 {
		return append([]byte{0xc0 + byte(len(payload))}, payload...)
	}
	lenBytes := encodeLength(uint64(len(payload)))
	header := append([]byte{0xf7 + byte(len(lenBytes))}, lenBytes...)
	return append(header, payload...)
}

// encodeLength encodes a length as big-endian bytes with no leading zeros.
func encodeLength(n uint64) []byte {
	if n < 256 {
		return []byte{byte(n)}
	}
	b := make([]byte, 8)
	b[0] = byte(n >> 56)
	b[1] = byte(n >> 48)
	b[2] = byte(n >> 40)
	b[3] = byte(n >> 32)
	b[4] = byte(n >> 24)
	b[5] = byte(n >> 16)
	b[6] = byte(n >> 8)
	b[7] = byte(n)
	for len(b) > 1 && b[0] == 0 {
		b = b[1:]
	}
	return b
}
