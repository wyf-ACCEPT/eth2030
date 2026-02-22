// Package enr implements Ethereum Node Records as defined in EIP-778.
// A node record holds arbitrary key/value pairs describing a node on the
// peer-to-peer network, signed under the "v4" identity scheme (secp256k1-keccak).
package enr

import (
	"crypto/ecdsa"
	"errors"
	"sort"

	"github.com/eth2030/eth2030/crypto"
	"github.com/eth2030/eth2030/rlp"
)

// SizeLimit is the maximum encoded size of a node record (EIP-778).
const SizeLimit = 300

// Standard ENR key names.
const (
	KeyID        = "id"
	KeySecp256k1 = "secp256k1"
	KeyIP        = "ip"
	KeyTCP       = "tcp"
	KeyUDP       = "udp"
	KeyIP6       = "ip6"
	KeyTCP6      = "tcp6"
	KeyUDP6      = "udp6"
)

var (
	ErrInvalidSig  = errors.New("enr: invalid signature")
	ErrTooBig      = errors.New("enr: record exceeds size limit")
	ErrNotSigned   = errors.New("enr: record not signed")
	ErrNotSorted   = errors.New("enr: pairs not sorted by key")
	ErrDuplicateKey = errors.New("enr: duplicate key")
)

// Pair is a key/value entry in an ENR record.
type Pair struct {
	Key   string
	Value []byte
}

// Record is an Ethereum Node Record (EIP-778).
type Record struct {
	Seq       uint64
	Pairs     []Pair // sorted by key
	Signature []byte
}

// Set adds or updates a key/value pair, keeping Pairs sorted.
// Setting a value invalidates the signature.
func (r *Record) Set(key string, value []byte) {
	r.Signature = nil
	v := make([]byte, len(value))
	copy(v, value)

	i := sort.Search(len(r.Pairs), func(i int) bool {
		return r.Pairs[i].Key >= key
	})
	if i < len(r.Pairs) && r.Pairs[i].Key == key {
		r.Pairs[i].Value = v
		return
	}
	// Insert at position i.
	r.Pairs = append(r.Pairs, Pair{})
	copy(r.Pairs[i+1:], r.Pairs[i:])
	r.Pairs[i] = Pair{Key: key, Value: v}
}

// Get returns the value for key, or nil if not present.
func (r *Record) Get(key string) []byte {
	i := sort.Search(len(r.Pairs), func(i int) bool {
		return r.Pairs[i].Key >= key
	})
	if i < len(r.Pairs) && r.Pairs[i].Key == key {
		return r.Pairs[i].Value
	}
	return nil
}

// SetSeq sets the sequence number. Invalidates the signature.
func (r *Record) SetSeq(seq uint64) {
	r.Signature = nil
	r.Seq = seq
}

// NodeID returns the keccak256 hash of the compressed public key stored
// in the record, or a zero hash if no secp256k1 key is present.
func (r *Record) NodeID() [32]byte {
	pub := r.Get(KeySecp256k1)
	if len(pub) == 0 {
		return [32]byte{}
	}
	h := crypto.Keccak256(pub)
	var id [32]byte
	copy(id[:], h)
	return id
}

// contentForSigning builds the RLP list [seq, k1, v1, k2, v2, ...] used for signing.
func (r *Record) contentForSigning() ([]byte, error) {
	// Build list elements: seq, then alternating key, value.
	var items []interface{}
	items = append(items, r.Seq)
	for _, p := range r.Pairs {
		items = append(items, p.Key)
		items = append(items, p.Value)
	}
	return rlp.EncodeToBytes(items)
}

// EncodeENR produces the full RLP-encoded record: [sig, seq, k1, v1, ...].
func EncodeENR(r *Record) ([]byte, error) {
	if r.Signature == nil {
		return nil, ErrNotSigned
	}
	var items []interface{}
	items = append(items, r.Signature)
	items = append(items, r.Seq)
	for _, p := range r.Pairs {
		items = append(items, p.Key)
		items = append(items, p.Value)
	}
	data, err := rlp.EncodeToBytes(items)
	if err != nil {
		return nil, err
	}
	if len(data) > SizeLimit {
		return nil, ErrTooBig
	}
	return data, nil
}

// DecodeENR decodes an RLP-encoded ENR record.
// Format: RLP list [signature, seq, k1, v1, k2, v2, ...]
func DecodeENR(data []byte) (*Record, error) {
	if len(data) > SizeLimit {
		return nil, ErrTooBig
	}
	s := rlp.NewStreamFromBytes(data)
	_, err := s.List()
	if err != nil {
		return nil, err
	}

	// Read signature.
	sig, err := s.Bytes()
	if err != nil {
		return nil, err
	}

	// Read sequence number.
	seq, err := s.Uint64()
	if err != nil {
		return nil, err
	}

	// Read key/value pairs.
	var pairs []Pair
	var prevKey string
	for i := 0; ; i++ {
		keyBytes, err := s.Bytes()
		if err != nil {
			break // end of list
		}
		valBytes, err := s.Bytes()
		if err != nil {
			return nil, errors.New("enr: incomplete key/value pair")
		}
		key := string(keyBytes)
		if i > 0 {
			if key == prevKey {
				return nil, ErrDuplicateKey
			}
			if key < prevKey {
				return nil, ErrNotSorted
			}
		}
		pairs = append(pairs, Pair{Key: key, Value: valBytes})
		prevKey = key
	}

	return &Record{
		Seq:       seq,
		Pairs:     pairs,
		Signature: sig,
	}, nil
}

// SignENR signs the record with the given private key using the "v4" identity
// scheme (secp256k1-keccak). It sets the "id" and "secp256k1" entries, then
// computes the signature over [seq, k1, v1, k2, v2, ...].
func SignENR(r *Record, key *ecdsa.PrivateKey) error {
	// Set the identity scheme.
	r.Set(KeyID, []byte("v4"))

	// Set the compressed public key.
	compressed := crypto.CompressPubkey(&key.PublicKey)
	r.Set(KeySecp256k1, compressed)

	// Build content to sign.
	content, err := r.contentForSigning()
	if err != nil {
		return err
	}
	hash := crypto.Keccak256(content)

	sig, err := crypto.Sign(hash, key)
	if err != nil {
		return err
	}
	// ENR signature is the 64-byte compact form (no recovery ID).
	r.Signature = sig[:64]
	return nil
}

// VerifyENR verifies the signature on the record. The record must contain
// a "secp256k1" entry with the compressed public key.
func VerifyENR(r *Record) error {
	if r.Signature == nil || len(r.Signature) < 64 {
		return ErrInvalidSig
	}
	pub := r.Get(KeySecp256k1)
	if len(pub) == 0 {
		return errors.New("enr: missing secp256k1 key")
	}

	// Decompress the public key to get 65-byte uncompressed form.
	ecPub, err := crypto.DecompressPubkey(pub)
	if err != nil {
		return err
	}
	uncompressed := crypto.FromECDSAPub(ecPub)

	// Rebuild signing content and hash.
	content, err := r.contentForSigning()
	if err != nil {
		return err
	}
	hash := crypto.Keccak256(content)

	if !crypto.ValidateSignature(uncompressed, hash, r.Signature[:64]) {
		return ErrInvalidSig
	}
	return nil
}
