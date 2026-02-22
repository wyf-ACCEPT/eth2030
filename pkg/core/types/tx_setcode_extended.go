package types

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/eth2030/eth2030/rlp"
	"golang.org/x/crypto/sha3"
)

// EIP-7702 extended constants.
const (
	// MaxAuthorizationListSize is the maximum number of authorization entries
	// allowed in a single SetCode transaction.
	MaxAuthorizationListSize = 256

	// DelegationCodeLength is the exact length of delegation designator code:
	// 3 bytes prefix (0xef0100) + 20 bytes address.
	DelegationCodeLength = 23

	// SetCodeTxIntrinsicGas is the base intrinsic gas cost for a SetCode tx.
	SetCodeTxIntrinsicGas uint64 = 21000
)

// SetCode transaction errors.
var (
	ErrSetCodeEmptyAuthList   = errors.New("setcode: authorization list is empty")
	ErrSetCodeTooManyAuths    = errors.New("setcode: too many authorization entries")
	ErrSetCodeNilChainID      = errors.New("setcode: nil chain ID in authorization")
	ErrSetCodeNegativeChainID = errors.New("setcode: negative chain ID in authorization")
	ErrSetCodeInvalidAuthSig  = errors.New("setcode: invalid authorization signature")
	ErrSetCodeZeroAddress     = errors.New("setcode: authorization targets zero address")
	ErrSetCodeDuplicateAuth   = errors.New("setcode: duplicate authorization for same address")
	ErrSetCodeSelfDelegation  = errors.New("setcode: cannot delegate to self")
)

// AuthorizationHash computes the EIP-7702 authorization signing hash:
// keccak256(0x05 || rlp([chain_id, address, nonce]))
func AuthorizationHash(auth *Authorization) Hash {
	if auth == nil {
		return Hash{}
	}
	chainID := bigOrZero(auth.ChainID)

	// RLP encode the authorization tuple: [chain_id, address, nonce]
	chainIDEnc, _ := rlp.EncodeToBytes(chainID)
	addrEnc, _ := rlp.EncodeToBytes(auth.Address)
	nonceEnc, _ := rlp.EncodeToBytes(auth.Nonce)

	var payload []byte
	payload = append(payload, chainIDEnc...)
	payload = append(payload, addrEnc...)
	payload = append(payload, nonceEnc...)

	d := sha3.NewLegacyKeccak256()
	d.Write([]byte{AuthMagic})
	d.Write(rlp.WrapList(payload))
	var h Hash
	copy(h[:], d.Sum(nil))
	return h
}

// RecoverAuthorizationSender recovers the signer address from an authorization
// entry using the V, R, S signature values and the computed authorization hash.
func RecoverAuthorizationSender(auth *Authorization) (Address, error) {
	if auth == nil {
		return Address{}, ErrSetCodeInvalidAuthSig
	}
	if auth.R == nil || auth.S == nil {
		return Address{}, ErrSetCodeInvalidAuthSig
	}

	authHash := AuthorizationHash(auth)

	var recovery byte
	if auth.V != nil {
		recovery = byte(auth.V.Uint64())
	}
	if recovery > 1 {
		return Address{}, ErrSetCodeInvalidAuthSig
	}

	return RecoverPlain(authHash, auth.R, auth.S, recovery)
}

// ValidateAuthorizationSignature checks that the authorization entry has
// valid signature components (non-nil, in range, and recoverable).
func ValidateAuthorizationSignature(auth *Authorization) error {
	if auth == nil {
		return ErrSetCodeInvalidAuthSig
	}
	if auth.R == nil || auth.S == nil {
		return ErrSetCodeInvalidAuthSig
	}
	if auth.R.Sign() <= 0 || auth.S.Sign() <= 0 {
		return ErrSetCodeInvalidAuthSig
	}
	if auth.R.Cmp(secp256k1NCopy) >= 0 || auth.S.Cmp(secp256k1NCopy) >= 0 {
		return ErrSetCodeInvalidAuthSig
	}
	// Check V is 0 or 1.
	if auth.V != nil && auth.V.Uint64() > 1 {
		return ErrSetCodeInvalidAuthSig
	}
	return nil
}

// ValidateAuthorizationList performs structural validation on the
// authorization list of a SetCode transaction.
func ValidateAuthorizationList(authList []Authorization, txChainID *big.Int) error {
	if len(authList) == 0 {
		return ErrSetCodeEmptyAuthList
	}
	if len(authList) > MaxAuthorizationListSize {
		return fmt.Errorf("%w: got %d, max %d",
			ErrSetCodeTooManyAuths, len(authList), MaxAuthorizationListSize)
	}

	seen := make(map[Address]bool)
	for i, auth := range authList {
		// Validate chain ID: must match tx chain ID or be 0 (wildcard).
		if auth.ChainID != nil {
			if auth.ChainID.Sign() < 0 {
				return fmt.Errorf("auth %d: %w", i, ErrSetCodeNegativeChainID)
			}
			// chain_id == 0 means "any chain" (wildcard)
			if auth.ChainID.Sign() > 0 && txChainID != nil && auth.ChainID.Cmp(txChainID) != 0 {
				return fmt.Errorf("auth %d: chain ID mismatch: auth=%s tx=%s",
					i, auth.ChainID, txChainID)
			}
		}

		// Validate target address is not zero.
		if auth.Address.IsZero() {
			return fmt.Errorf("auth %d: %w", i, ErrSetCodeZeroAddress)
		}

		// Validate signature components.
		if err := ValidateAuthorizationSignature(&authList[i]); err != nil {
			return fmt.Errorf("auth %d: %w", i, err)
		}

		// Check for duplicates: a signer should appear at most once.
		// We check by recovering the signer.
		signer, err := RecoverAuthorizationSender(&authList[i])
		if err != nil {
			return fmt.Errorf("auth %d: recovery failed: %w", i, err)
		}
		if seen[signer] {
			return fmt.Errorf("auth %d: %w (signer %s)", i, ErrSetCodeDuplicateAuth, signer.Hex())
		}
		seen[signer] = true
	}
	return nil
}

// ComputeSetCodeIntrinsicGas calculates the intrinsic gas for a SetCode tx.
// Gas = base_intrinsic + calldata_cost + per_auth_cost * len(auth_list) + empty_account_costs
// where empty_account_costs is PerEmptyAccountCost for each authorization
// that targets an account not yet existing (determined externally).
func ComputeSetCodeIntrinsicGas(data []byte, authCount int, emptyAccountCount int) uint64 {
	gas := SetCodeTxIntrinsicGas

	// Calldata cost (standard EIP-2028/EIP-7623 pricing).
	for _, b := range data {
		if b == 0 {
			gas += 4
		} else {
			gas += 16
		}
	}

	// Per-authorization base cost.
	gas += PerAuthBaseCost * uint64(authCount)

	// Per-empty-account cost.
	gas += PerEmptyAccountCost * uint64(emptyAccountCount)

	return gas
}

// BuildDelegationCode creates the delegation designator code for an address.
// This is equivalent to AddressToDelegation but provides a clearer name for
// the EIP-7702 use case.
func BuildDelegationCode(target Address) []byte {
	return AddressToDelegation(target)
}

// IsDelegated checks whether the given account code is a delegation designator.
// If so, it returns the delegated-to address. This combines HasDelegationPrefix
// and ParseDelegation into a single call.
func IsDelegated(code []byte) (Address, bool) {
	if len(code) != DelegationCodeLength {
		return Address{}, false
	}
	return ParseDelegation(code)
}

// ResolveDelegationChain resolves a chain of delegation designators up to
// a maximum depth. Returns the final target address and the chain length.
// If the chain is circular or exceeds maxDepth, returns an error.
func ResolveDelegationChain(startCode []byte, codeLookup func(Address) []byte, maxDepth int) (Address, int, error) {
	if maxDepth <= 0 {
		maxDepth = 10
	}

	current := startCode
	visited := make(map[Address]bool)

	for depth := 0; depth < maxDepth; depth++ {
		target, ok := ParseDelegation(current)
		if !ok {
			// Not a delegation; this is the final code
			if depth == 0 {
				return Address{}, 0, errors.New("setcode: not a delegation")
			}
			// The previous target was the final address
			return Address{}, depth, nil
		}

		if visited[target] {
			return Address{}, depth, errors.New("setcode: circular delegation chain")
		}
		visited[target] = true

		nextCode := codeLookup(target)
		if !HasDelegationPrefix(nextCode) {
			return target, depth + 1, nil
		}
		current = nextCode
	}
	return Address{}, maxDepth, fmt.Errorf("setcode: delegation chain exceeds max depth %d", maxDepth)
}

// NewAuthorization creates a new Authorization with the given parameters
// and zero signature values (to be signed later).
func NewAuthorization(chainID *big.Int, addr Address, nonce uint64) Authorization {
	auth := Authorization{
		Address: addr,
		Nonce:   nonce,
	}
	if chainID != nil {
		auth.ChainID = new(big.Int).Set(chainID)
	}
	return auth
}

// AuthorizationListGas returns the total gas cost for the authorization list.
func AuthorizationListGas(authCount int) uint64 {
	return PerAuthBaseCost * uint64(authCount)
}
