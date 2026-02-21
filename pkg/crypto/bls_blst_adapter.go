//go:build blst

// Real BLS12-381 adapter using the supranational/blst library.
//
// This file provides a production-grade BLS backend for the Ethereum consensus
// layer using the blst C library via CGO. It implements the BLSBackend interface
// with the "MinPk" scheme used by Ethereum:
//   - Public keys in G1 (48-byte compressed P1Affine)
//   - Signatures in G2 (96-byte compressed P2Affine)
//   - DST: BLS_SIG_BLS12381G2_XMD:SHA-256_SSWU_RO_POP_
//
// Build with: go build -tags blst
// Test with:  go test -tags blst ./crypto/ -run Blst
package crypto

import (
	"errors"
	"fmt"

	blst "github.com/supranational/blst/bindings/go"
)

// blstDST is the domain separation tag for Ethereum BLS signatures.
// This matches BLSSignatureDST from bls_integration.go.
var blstDST = []byte("BLS_SIG_BLS12381G2_XMD:SHA-256_SSWU_RO_POP_")

// Key and signature sizes for the MinPk scheme.
const (
	blstPubkeySize = 48 // compressed G1
	blstSigSize    = 96 // compressed G2
	blstSecretSize = 32 // scalar field element
)

// Errors returned by blst adapter helpers.
var (
	ErrBlstInvalidIKM       = errors.New("blst: IKM must be at least 32 bytes")
	ErrBlstKeyGenFailed     = errors.New("blst: key generation failed")
	ErrBlstInvalidSecretKey = errors.New("blst: invalid secret key bytes")
	ErrBlstSignFailed       = errors.New("blst: signing failed")
	ErrBlstNoSignatures     = errors.New("blst: no signatures to aggregate")
	ErrBlstInvalidSignature = errors.New("blst: invalid signature bytes")
	ErrBlstAggregateFailed  = errors.New("blst: signature aggregation failed")
)

// BlstRealBackend implements BLSBackend using the supranational/blst library
// with the MinPk scheme (PK in G1, Sig in G2) as used by Ethereum.
type BlstRealBackend struct{}

// Name returns the backend identifier.
func (b *BlstRealBackend) Name() string {
	return "blst-real"
}

// Verify checks a single BLS signature using blst.
// pubkey must be 48-byte compressed G1, sig must be 96-byte compressed G2.
func (b *BlstRealBackend) Verify(pubkey, msg, sig []byte) bool {
	if len(pubkey) == 0 || len(sig) == 0 {
		return false
	}

	pk := new(blst.P1Affine).Uncompress(pubkey)
	if pk == nil {
		return false
	}

	s := new(blst.P2Affine).Uncompress(sig)
	if s == nil {
		return false
	}

	return s.Verify(true, pk, true, msg, blstDST)
}

// AggregateVerify checks an aggregate signature where each signer signed a
// different message. pubkeys[i] signed msgs[i], and sig is the aggregate
// signature over all of them.
func (b *BlstRealBackend) AggregateVerify(pubkeys, msgs [][]byte, sig []byte) bool {
	n := len(pubkeys)
	if n == 0 || n != len(msgs) || len(sig) == 0 {
		return false
	}

	s := new(blst.P2Affine).Uncompress(sig)
	if s == nil {
		return false
	}

	pks := make([]*blst.P1Affine, n)
	for i, pkBytes := range pubkeys {
		pks[i] = new(blst.P1Affine).Uncompress(pkBytes)
		if pks[i] == nil {
			return false
		}
	}

	// blst.Message is just []byte, so msgs ([][]byte) maps directly.
	blstMsgs := make([]blst.Message, n)
	for i, m := range msgs {
		blstMsgs[i] = m
	}

	return s.AggregateVerify(true, pks, true, blstMsgs, blstDST)
}

// FastAggregateVerify checks an aggregate signature where all signers signed
// the same message. This is the common case for Ethereum attestations.
func (b *BlstRealBackend) FastAggregateVerify(pubkeys [][]byte, msg, sig []byte) bool {
	n := len(pubkeys)
	if n == 0 || len(sig) == 0 {
		return false
	}

	s := new(blst.P2Affine).Uncompress(sig)
	if s == nil {
		return false
	}

	pks := make([]*blst.P1Affine, n)
	for i, pkBytes := range pubkeys {
		pks[i] = new(blst.P1Affine).Uncompress(pkBytes)
		if pks[i] == nil {
			return false
		}
	}

	return s.FastAggregateVerify(true, pks, msg, blstDST)
}

// --- Helper functions ---

// BlstKeyGen generates a BLS key pair from input key material (IKM).
// IKM must be at least 32 bytes. Returns compressed public key (48 bytes)
// and serialized secret key (32 bytes).
func BlstKeyGen(ikm []byte) (pubkey, secretKey []byte, err error) {
	if len(ikm) < 32 {
		return nil, nil, ErrBlstInvalidIKM
	}

	sk := blst.KeyGen(ikm)
	if sk == nil {
		return nil, nil, ErrBlstKeyGenFailed
	}

	pk := new(blst.P1Affine).From(sk)
	pubkey = pk.Compress()
	secretKey = sk.Serialize()
	return pubkey, secretKey, nil
}

// BlstSign signs a message using the given secret key bytes (32 bytes).
// Returns the compressed signature (96 bytes).
func BlstSign(secretKey, msg []byte) ([]byte, error) {
	if len(secretKey) != blstSecretSize {
		return nil, ErrBlstInvalidSecretKey
	}

	sk := new(blst.SecretKey).Deserialize(secretKey)
	if sk == nil {
		return nil, ErrBlstInvalidSecretKey
	}

	sig := new(blst.P2Affine).Sign(sk, msg, blstDST)
	if sig == nil {
		return nil, ErrBlstSignFailed
	}

	return sig.Compress(), nil
}

// BlstAggregateSigs aggregates multiple compressed signatures (each 96 bytes)
// into a single compressed aggregate signature.
func BlstAggregateSigs(sigs [][]byte) ([]byte, error) {
	if len(sigs) == 0 {
		return nil, ErrBlstNoSignatures
	}

	agg := new(blst.P2Aggregate)
	if !agg.AggregateCompressed(sigs, true) {
		return nil, ErrBlstAggregateFailed
	}

	return agg.ToAffine().Compress(), nil
}

// blstGenKeyPair is a convenience for tests: generates a key pair from IKM,
// panicking on failure. Not exported to avoid misuse in production code.
func blstGenKeyPair(ikm []byte) (pk, sk []byte) {
	pubkey, secretKey, err := BlstKeyGen(ikm)
	if err != nil {
		panic(fmt.Sprintf("blstGenKeyPair: %v", err))
	}
	return pubkey, secretKey
}
