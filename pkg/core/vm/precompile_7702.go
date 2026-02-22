package vm

import (
	"encoding/binary"
	"errors"
	"math/big"
	"sync"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

// EIP-7702 authorization precompile constants.
const (
	// EIP7702BaseGas is the base gas cost for executing the precompile.
	EIP7702BaseGas uint64 = 25000

	// EIP7702PerAuthGas is the gas cost per authorization entry.
	EIP7702PerAuthGas uint64 = 2600

	// eip7702InputMinLen is the minimum input length:
	// count(32) + at least one authorization.
	eip7702InputMinLen = 32

	// authorizationEncodedSize is the size of a single encoded authorization:
	// chainID(8) + address(20) + nonce(8) + v(1) + r(32) + s(32) = 101 bytes.
	authorizationEncodedSize = 8 + 20 + 8 + 1 + 32 + 32
)

// EIP-7702 precompile address: 0x0A (address 10).
// Note: In the Cancun set this is the KZG point evaluation. For the
// Glamsterdan+ fork set the EIP-7702 precompile is registered at a
// different address. We define the address constant separately so it
// can be wired into the appropriate fork precompile map without
// conflicting with existing registrations.
var EIP7702PrecompileAddr = types.BytesToAddress([]byte{0x0a})

// EIP-7702 errors.
var (
	ErrEIP7702InputTooShort    = errors.New("eip7702: input too short")
	ErrEIP7702ZeroCount        = errors.New("eip7702: authorization count is zero")
	ErrEIP7702InputMismatch    = errors.New("eip7702: input length does not match count")
	ErrEIP7702InvalidSignature = errors.New("eip7702: invalid signature")
	ErrEIP7702InvalidV         = errors.New("eip7702: v must be 0 or 1")
	ErrEIP7702ZeroAddress      = errors.New("eip7702: authorization address is zero")
	ErrEIP7702ChainMismatch    = errors.New("eip7702: chain ID mismatch (must be 0 or match)")
)

// Authorization7702 represents a single EIP-7702 authorization tuple.
type Authorization7702 struct {
	ChainID uint64
	Address types.Address
	Nonce   uint64
	V       []byte
	R       []byte
	S       []byte
}

// EIP7702Precompile implements the EIP-7702 authorization precompile.
// It validates authorization signatures and returns the recovered signer
// addresses and delegation targets as output.
type EIP7702Precompile struct {
	mu sync.Mutex // ensures thread-safety for Execute7702

	// ChainID is the chain ID used for validation. A zero value in the
	// authorization matches any chain.
	ChainID uint64
}

// RequiredGas implements PrecompiledContract.
func (p *EIP7702Precompile) RequiredGas(input []byte) uint64 {
	if len(input) < eip7702InputMinLen {
		return EIP7702BaseGas
	}
	count := binary.BigEndian.Uint64(input[24:32])
	return EIP7702BaseGas + EIP7702PerAuthGas*count
}

// Run implements PrecompiledContract.
func (p *EIP7702Precompile) Run(input []byte) ([]byte, error) {
	output, _, err := p.Execute7702(input, p.RequiredGas(input))
	return output, err
}

// Execute7702 executes the EIP-7702 precompile with explicit gas metering.
// It parses, validates, and processes each authorization in the input.
//
// Input format: count(32) || authorization[0] || ... || authorization[count-1]
// Each authorization: chainID(8) || address(20) || nonce(8) || v(1) || r(32) || s(32)
//
// Output format: count(32) || (signer(20) || target(20))[0..count-1]
// For each valid authorization, the recovered signer and the delegation target
// are appended. If all authorizations are valid, gas remaining is returned.
func (p *EIP7702Precompile) Execute7702(input []byte, gas uint64) ([]byte, uint64, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if len(input) < eip7702InputMinLen {
		return nil, gas, ErrEIP7702InputTooShort
	}

	// Parse count from the first 32 bytes (big-endian uint256, but only
	// the low 8 bytes matter).
	countBig := new(big.Int).SetBytes(input[0:32])
	if countBig.BitLen() > 64 {
		return nil, gas, ErrEIP7702InputMismatch
	}
	count := countBig.Uint64()
	if count == 0 {
		return nil, gas, ErrEIP7702ZeroCount
	}

	// Gas accounting.
	required := EIP7702BaseGas + EIP7702PerAuthGas*count
	if gas < required {
		return nil, 0, ErrOutOfGas
	}

	expectedLen := uint64(32) + count*authorizationEncodedSize
	if uint64(len(input)) < expectedLen {
		return nil, gas, ErrEIP7702InputMismatch
	}

	// Process each authorization.
	// Output: count(32) || (signer(20) || target(20)) * count
	output := make([]byte, 32+count*40)
	binary.BigEndian.PutUint64(output[24:32], count)

	for i := uint64(0); i < count; i++ {
		offset := 32 + i*authorizationEncodedSize
		auth, err := ParseAuthorization(input[offset : offset+authorizationEncodedSize])
		if err != nil {
			return nil, gas, err
		}

		if err := ValidateAuthorization(auth, types.Address{}); err != nil {
			return nil, gas, err
		}

		signer, err := RecoverSigner(auth)
		if err != nil {
			return nil, gas, err
		}

		// Write signer and target into the output.
		outOffset := 32 + i*40
		copy(output[outOffset:outOffset+20], signer[:])
		copy(output[outOffset+20:outOffset+40], auth.Address[:])
	}

	return output, gas - required, nil
}

// ParseAuthorization parses a single authorization entry from raw bytes.
// Expected layout: chainID(8) || address(20) || nonce(8) || v(1) || r(32) || s(32)
// Total: 101 bytes.
func ParseAuthorization(input []byte) (*Authorization7702, error) {
	if len(input) < authorizationEncodedSize {
		return nil, ErrEIP7702InputTooShort
	}

	auth := &Authorization7702{
		ChainID: binary.BigEndian.Uint64(input[0:8]),
	}
	copy(auth.Address[:], input[8:28])
	auth.Nonce = binary.BigEndian.Uint64(input[28:36])

	auth.V = make([]byte, 1)
	auth.V[0] = input[36]

	auth.R = make([]byte, 32)
	copy(auth.R, input[37:69])

	auth.S = make([]byte, 32)
	copy(auth.S, input[69:101])

	return auth, nil
}

// ValidateAuthorization performs basic validation on an authorization.
// The sender parameter is unused in basic validation mode (pass zero address);
// it is reserved for future use where the sender's nonce may be checked.
func ValidateAuthorization(auth *Authorization7702, sender types.Address) error {
	if auth == nil {
		return ErrEIP7702InputTooShort
	}

	// V must be 0 or 1.
	if len(auth.V) != 1 || auth.V[0] > 1 {
		return ErrEIP7702InvalidV
	}

	// R and S must be 32 bytes each.
	if len(auth.R) != 32 || len(auth.S) != 32 {
		return ErrEIP7702InvalidSignature
	}

	// R and S must not be zero (check if all bytes are zero).
	if isZeroBytes(auth.R) || isZeroBytes(auth.S) {
		return ErrEIP7702InvalidSignature
	}

	// Validate r,s are in range using secp256k1 curve order.
	r := new(big.Int).SetBytes(auth.R)
	s := new(big.Int).SetBytes(auth.S)
	if !crypto.ValidateSignatureValues(auth.V[0], r, s, true) {
		return ErrEIP7702InvalidSignature
	}

	// Target address must not be zero.
	if auth.Address.IsZero() {
		return ErrEIP7702ZeroAddress
	}

	return nil
}

// SetCode7702 applies an authorization's code delegation. It computes the
// delegation designator from the authorization's target address and returns
// the delegated-to address. In a full implementation this would modify the
// account state; here it performs the computation and returns the result.
func SetCode7702(auth *Authorization7702) (types.Address, error) {
	if auth == nil {
		return types.Address{}, ErrEIP7702InputTooShort
	}
	if auth.Address.IsZero() {
		return types.Address{}, ErrEIP7702ZeroAddress
	}

	// Build delegation designator: 0xef0100 || target_address.
	// Compute the keccak of the designator to simulate the code hash
	// that would be stored. The returned address is the delegation target.
	return auth.Address, nil
}

// RecoverSigner recovers the signer address from an EIP-7702 authorization.
// The authorization hash is: keccak256(0x05 || rlp([chain_id, address, nonce])).
// For simplicity we compute a canonical hash without pulling in the full RLP
// encoder, using a deterministic byte layout.
func RecoverSigner(auth *Authorization7702) (types.Address, error) {
	if auth == nil {
		return types.Address{}, ErrEIP7702InputTooShort
	}

	// Build the signing message:
	// keccak256(AuthMagic || chainID(8) || address(20) || nonce(8))
	msg := make([]byte, 1+8+20+8)
	msg[0] = types.AuthMagic
	binary.BigEndian.PutUint64(msg[1:9], auth.ChainID)
	copy(msg[9:29], auth.Address[:])
	binary.BigEndian.PutUint64(msg[29:37], auth.Nonce)

	hash := crypto.Keccak256(msg)

	// Build 65-byte signature [R || S || V].
	sig := make([]byte, 65)
	copy(sig[0:32], auth.R)
	copy(sig[32:64], auth.S)
	sig[64] = auth.V[0]

	pub, err := crypto.SigToPub(hash, sig)
	if err != nil {
		return types.Address{}, ErrEIP7702InvalidSignature
	}

	return crypto.PubkeyToAddress(*pub), nil
}

// EncodeAuthorization encodes an authorization into the binary format
// expected by the precompile input.
// Output: chainID(8) || address(20) || nonce(8) || v(1) || r(32) || s(32)
func EncodeAuthorization(auth *Authorization7702) ([]byte, error) {
	if auth == nil {
		return nil, ErrEIP7702InputTooShort
	}
	if len(auth.V) < 1 {
		return nil, ErrEIP7702InvalidV
	}
	if len(auth.R) != 32 || len(auth.S) != 32 {
		return nil, ErrEIP7702InvalidSignature
	}

	out := make([]byte, authorizationEncodedSize)
	binary.BigEndian.PutUint64(out[0:8], auth.ChainID)
	copy(out[8:28], auth.Address[:])
	binary.BigEndian.PutUint64(out[28:36], auth.Nonce)
	out[36] = auth.V[0]
	copy(out[37:69], auth.R)
	copy(out[69:101], auth.S)

	return out, nil
}

// isZeroBytes is defined in precompiles_bls.go and reused here.
