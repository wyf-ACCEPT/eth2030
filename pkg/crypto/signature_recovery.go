// ECDSA signature recovery utilities for Ethereum transaction signing.
//
// Provides compact signature representation (65 bytes: R || S || V),
// public key recovery from signatures, Ethereum address derivation,
// EcRecover precompile implementation, EIP-155 chain-aware recovery,
// and batch verification for transaction pools.
//
// V value encoding:
//   - 0 or 1: raw recovery ID
//   - 27 or 28: Ethereum legacy (pre-EIP-155)
//   - 35 + 2*chainID or 36 + 2*chainID: EIP-155 replay-protected
//
// Signature malleability: s is always normalized to the lower half of
// the curve order per EIP-2 (Homestead), preventing transaction hash
// malleability attacks.
package crypto

import (
	"crypto/ecdsa"
	"errors"
	"math/big"
	"sync"

	"github.com/eth2030/eth2030/core/types"
)

// SigRecover provides ECDSA signature recovery operations.
// It encapsulates signature parsing, validation, public key recovery,
// and Ethereum address derivation. Stateless; all methods are safe
// for concurrent use.
type SigRecover struct{}

// NewSigRecover creates a new SigRecover instance.
func NewSigRecover() *SigRecover {
	return &SigRecover{}
}

// CompactSignature is a 65-byte ECDSA signature: R (32) || S (32) || V (1).
// R and S are the signature components; V is the recovery ID that allows
// the signer's public key to be recovered from the signature alone.
type CompactSignature struct {
	R [32]byte
	S [32]byte
	V byte
}

// Errors for signature recovery operations.
var (
	ErrSigRecoverInvalidLength = errors.New("sig_recover: signature must be 65 bytes")
	ErrSigRecoverInvalidV      = errors.New("sig_recover: invalid V value")
	ErrSigRecoverInvalidR      = errors.New("sig_recover: R must be in [1, n-1]")
	ErrSigRecoverInvalidS      = errors.New("sig_recover: S must be in [1, n-1]")
	ErrSigRecoverMalleable     = errors.New("sig_recover: S is in upper half (malleable)")
	ErrSigRecoverHashLength    = errors.New("sig_recover: message hash must be 32 bytes")
	ErrSigRecoverFailed        = errors.New("sig_recover: public key recovery failed")
	ErrSigRecoverBatchEmpty    = errors.New("sig_recover: empty batch")
	ErrSigRecoverBatchMismatch = errors.New("sig_recover: batch lengths do not match")
)

// ParseCompactSignature parses a 65-byte signature into a CompactSignature.
// Does not validate the signature components; use Validate for that.
func ParseCompactSignature(sig []byte) (*CompactSignature, error) {
	if len(sig) != 65 {
		return nil, ErrSigRecoverInvalidLength
	}
	cs := &CompactSignature{V: sig[64]}
	copy(cs.R[:], sig[:32])
	copy(cs.S[:], sig[32:64])
	return cs, nil
}

// Bytes encodes the compact signature as 65 bytes: R || S || V.
func (cs *CompactSignature) Bytes() []byte {
	buf := make([]byte, 65)
	copy(buf[:32], cs.R[:])
	copy(buf[32:64], cs.S[:])
	buf[64] = cs.V
	return buf
}

// RBigInt returns R as a big.Int.
func (cs *CompactSignature) RBigInt() *big.Int {
	return new(big.Int).SetBytes(cs.R[:])
}

// SBigInt returns S as a big.Int.
func (cs *CompactSignature) SBigInt() *big.Int {
	return new(big.Int).SetBytes(cs.S[:])
}

// NormalizeV converts V from any Ethereum encoding to raw 0/1.
// Handles:
//   - 0, 1: already raw
//   - 27, 28: legacy Ethereum (subtract 27)
//   - 35 + 2*chainID, 36 + 2*chainID: EIP-155 (extract recovery bit)
//
// Returns the raw V (0 or 1) and the chain ID (0 for non-EIP-155).
func NormalizeV(v *big.Int) (byte, *big.Int) {
	vUint := v.Uint64()

	// Raw recovery ID.
	if v.IsInt64() && (vUint == 0 || vUint == 1) {
		return byte(vUint), new(big.Int)
	}

	// Legacy Ethereum encoding.
	if v.IsInt64() && (vUint == 27 || vUint == 28) {
		return byte(vUint - 27), new(big.Int)
	}

	// EIP-155: v = 35 + 2*chainID + recoveryBit
	// recoveryBit = (v - 35) % 2
	// chainID = (v - 35) / 2
	if v.Cmp(big.NewInt(35)) >= 0 {
		diff := new(big.Int).Sub(v, big.NewInt(35))
		recoveryBit := byte(new(big.Int).Mod(diff, big.NewInt(2)).Uint64())
		chainID := new(big.Int).Div(diff, big.NewInt(2))
		return recoveryBit, chainID
	}

	// Unrecognized V: treat as raw if low enough.
	if v.IsInt64() && vUint < 4 {
		return byte(vUint & 1), new(big.Int)
	}
	return 0, new(big.Int)
}

// EncodeVLegacy encodes a raw V (0 or 1) as legacy Ethereum V (27 or 28).
func EncodeVLegacy(rawV byte) byte {
	return rawV + 27
}

// EncodeVEIP155 encodes a raw V (0 or 1) as EIP-155 V for the given chain ID.
// v = 35 + 2*chainID + rawV
func EncodeVEIP155(rawV byte, chainID *big.Int) *big.Int {
	v := new(big.Int).Mul(chainID, big.NewInt(2))
	v.Add(v, big.NewInt(35))
	v.Add(v, new(big.Int).SetUint64(uint64(rawV)))
	return v
}

// Validate checks that the signature components are valid:
//   - R in [1, n-1]
//   - S in [1, n-1]
//   - S in lower half of curve order (non-malleable)
//   - V is 0 or 1
func (cs *CompactSignature) Validate() error {
	r := cs.RBigInt()
	s := cs.SBigInt()
	return validateSigComponents(r, s, cs.V)
}

// validateSigComponents checks r, s, v for correctness.
func validateSigComponents(r, s *big.Int, v byte) error {
	if v > 1 {
		return ErrSigRecoverInvalidV
	}
	// R must be in [1, n-1].
	if r.Sign() <= 0 || r.Cmp(secp256k1N) >= 0 {
		return ErrSigRecoverInvalidR
	}
	// S must be in [1, n-1].
	if s.Sign() <= 0 || s.Cmp(secp256k1N) >= 0 {
		return ErrSigRecoverInvalidS
	}
	// Malleability check: S must be in lower half.
	if s.Cmp(secp256k1halfN) > 0 {
		return ErrSigRecoverMalleable
	}
	return nil
}

// NormalizeS ensures S is in the lower half of the curve order.
// If S > n/2, it is replaced by n - S and V is flipped.
// This is required by EIP-2 to prevent transaction malleability.
func (cs *CompactSignature) NormalizeS() {
	s := cs.SBigInt()
	if s.Cmp(secp256k1halfN) > 0 {
		s.Sub(secp256k1N, s)
		sBytes := s.Bytes()
		cs.S = [32]byte{}
		copy(cs.S[32-len(sBytes):], sBytes)
		cs.V ^= 1 // flip recovery bit
	}
}

// RecoverPublicKey recovers the uncompressed public key (65 bytes) from
// a 32-byte message hash and 65-byte compact signature.
// Returns [0x04 || X (32) || Y (32)].
func (sr *SigRecover) RecoverPublicKey(hash []byte, sig *CompactSignature) ([]byte, error) {
	if len(hash) != 32 {
		return nil, ErrSigRecoverHashLength
	}
	if err := sig.Validate(); err != nil {
		return nil, err
	}
	pub, err := SigToPub(hash, sig.Bytes())
	if err != nil {
		return nil, ErrSigRecoverFailed
	}
	return FromECDSAPub(pub), nil
}

// RecoverPublicKeyFromBytes recovers the public key from raw byte inputs.
// sig must be exactly 65 bytes.
func (sr *SigRecover) RecoverPublicKeyFromBytes(hash, sig []byte) ([]byte, error) {
	cs, err := ParseCompactSignature(sig)
	if err != nil {
		return nil, err
	}
	return sr.RecoverPublicKey(hash, cs)
}

// SignatureToAddress recovers the Ethereum address from a message hash
// and compact signature. This is the common operation for transaction
// sender recovery: address = Keccak256(pubkey[1:])[12:].
func (sr *SigRecover) SignatureToAddress(hash []byte, sig *CompactSignature) (types.Address, error) {
	if len(hash) != 32 {
		return types.Address{}, ErrSigRecoverHashLength
	}
	if err := sig.Validate(); err != nil {
		return types.Address{}, err
	}
	pub, err := SigToPub(hash, sig.Bytes())
	if err != nil {
		return types.Address{}, ErrSigRecoverFailed
	}
	return PubkeyToAddress(*pub), nil
}

// SignatureToAddressBytes is a convenience wrapper that accepts raw bytes.
func (sr *SigRecover) SignatureToAddressBytes(hash, sig []byte) (types.Address, error) {
	cs, err := ParseCompactSignature(sig)
	if err != nil {
		return types.Address{}, err
	}
	return sr.SignatureToAddress(hash, cs)
}

// EcRecoverPrecompile implements the ecRecover precompile (address 0x01).
// Input: hash (32) || v (32) || r (32) || s (32) = 128 bytes.
// V is the legacy Ethereum value (27 or 28).
// Output: left-padded 32-byte address, or nil on failure.
func (sr *SigRecover) EcRecoverPrecompile(input []byte) []byte {
	if len(input) < 128 {
		padded := make([]byte, 128)
		copy(padded, input)
		input = padded
	}

	hash := input[:32]

	// V is a 32-byte big-endian integer; must be 27 or 28.
	vBI := new(big.Int).SetBytes(input[32:64])
	if !vBI.IsInt64() {
		return nil
	}
	vVal := vBI.Int64()
	if vVal != 27 && vVal != 28 {
		return nil
	}
	rawV := byte(vVal - 27)

	r := new(big.Int).SetBytes(input[64:96])
	s := new(big.Int).SetBytes(input[96:128])

	// Validate components.
	if err := validateSigComponents(r, s, rawV); err != nil {
		return nil
	}

	// Build the 65-byte signature.
	sig := make([]byte, 65)
	rBytes := r.Bytes()
	sBytes := s.Bytes()
	copy(sig[32-len(rBytes):32], rBytes)
	copy(sig[64-len(sBytes):64], sBytes)
	sig[64] = rawV

	pub, err := Ecrecover(hash, sig)
	if err != nil || pub == nil {
		return nil
	}

	// Derive address: Keccak256(pubkey[1:])[12:].
	addr := Keccak256(pub[1:])
	// Return as left-padded 32 bytes.
	result := make([]byte, 32)
	copy(result[12:], addr[12:])
	return result
}

// BatchRecoveryResult holds the result of a single recovery in a batch.
type BatchRecoveryResult struct {
	Address types.Address
	PubKey  *ecdsa.PublicKey
	Err     error
}

// BatchSignatureVerification verifies multiple signatures concurrently,
// recovering the signer address for each. Useful for transaction pool
// validation where many signatures need verification at once.
//
// hashes[i] and sigs[i] correspond to the i-th signature to verify.
// Results are returned in the same order.
func (sr *SigRecover) BatchSignatureVerification(
	hashes [][]byte,
	sigs []*CompactSignature,
) ([]BatchRecoveryResult, error) {
	n := len(hashes)
	if n == 0 {
		return nil, ErrSigRecoverBatchEmpty
	}
	if n != len(sigs) {
		return nil, ErrSigRecoverBatchMismatch
	}

	results := make([]BatchRecoveryResult, n)

	// Use a worker pool for parallelism when the batch is large enough.
	if n <= 4 {
		// Small batch: sequential recovery.
		for i := 0; i < n; i++ {
			results[i] = sr.recoverOne(hashes[i], sigs[i])
		}
		return results, nil
	}

	// Large batch: parallel recovery with bounded concurrency.
	var wg sync.WaitGroup
	workers := 8
	if n < workers {
		workers = n
	}
	work := make(chan int, n)

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range work {
				results[i] = sr.recoverOne(hashes[i], sigs[i])
			}
		}()
	}

	for i := 0; i < n; i++ {
		work <- i
	}
	close(work)
	wg.Wait()

	return results, nil
}

// recoverOne performs a single signature recovery, returning the result.
func (sr *SigRecover) recoverOne(hash []byte, sig *CompactSignature) BatchRecoveryResult {
	if len(hash) != 32 {
		return BatchRecoveryResult{Err: ErrSigRecoverHashLength}
	}
	if sig == nil {
		return BatchRecoveryResult{Err: ErrSigRecoverInvalidLength}
	}
	if err := sig.Validate(); err != nil {
		return BatchRecoveryResult{Err: err}
	}
	pub, err := SigToPub(hash, sig.Bytes())
	if err != nil {
		return BatchRecoveryResult{Err: ErrSigRecoverFailed}
	}
	addr := PubkeyToAddress(*pub)
	return BatchRecoveryResult{
		Address: addr,
		PubKey:  pub,
	}
}

// RecoverEIP155Sender recovers the sender address from an EIP-155
// transaction signature. The signature hash must have been computed
// with the chain ID included per EIP-155.
//
// v is the full EIP-155 V value (>= 35).
// r, s are the signature components.
// chainID is the expected chain ID for validation.
func (sr *SigRecover) RecoverEIP155Sender(
	hash []byte,
	v *big.Int,
	r, s *big.Int,
	chainID *big.Int,
) (types.Address, error) {
	if len(hash) != 32 {
		return types.Address{}, ErrSigRecoverHashLength
	}

	rawV, extractedChainID := NormalizeV(v)
	if chainID.Sign() > 0 && extractedChainID.Cmp(chainID) != 0 {
		return types.Address{}, ErrSigRecoverInvalidV
	}

	if err := validateSigComponents(r, s, rawV); err != nil {
		return types.Address{}, err
	}

	sig := make([]byte, 65)
	rBytes := r.Bytes()
	sBytes := s.Bytes()
	copy(sig[32-len(rBytes):32], rBytes)
	copy(sig[64-len(sBytes):64], sBytes)
	sig[64] = rawV

	pub, err := SigToPub(hash, sig)
	if err != nil {
		return types.Address{}, ErrSigRecoverFailed
	}
	return PubkeyToAddress(*pub), nil
}

// IsValidSignature performs a quick check on whether a 65-byte signature
// has valid R, S, and V components without performing recovery.
func IsValidSignature(sig []byte) bool {
	if len(sig) != 65 {
		return false
	}
	cs, err := ParseCompactSignature(sig)
	if err != nil {
		return false
	}
	return cs.Validate() == nil
}

// RecoverCompressed recovers a 33-byte compressed public key from
// a message hash and compact signature.
func (sr *SigRecover) RecoverCompressed(hash []byte, sig *CompactSignature) ([]byte, error) {
	if len(hash) != 32 {
		return nil, ErrSigRecoverHashLength
	}
	if err := sig.Validate(); err != nil {
		return nil, err
	}
	pub, err := SigToPub(hash, sig.Bytes())
	if err != nil {
		return nil, ErrSigRecoverFailed
	}
	return CompressPubkey(pub), nil
}
