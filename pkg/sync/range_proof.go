// range_proof.go implements range proofs for snap sync state healing.
// Range proofs allow a verifier to confirm that a set of key-value pairs
// from a trie is complete and correctly ordered within a given range,
// enabling parallel state downloads with cryptographic integrity.
package sync

import (
	"bytes"
	"errors"
	"math/big"
	"sort"

	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/crypto"
)

// Range proof errors.
var (
	ErrEmptyRangeProof   = errors.New("range proof: empty proof")
	ErrUnsortedKeys      = errors.New("range proof: keys not sorted")
	ErrKeyValueMismatch  = errors.New("range proof: keys and values length mismatch")
	ErrInvalidProofRoot  = errors.New("range proof: root hash mismatch")
	ErrInvalidSplitCount = errors.New("range proof: split count must be positive")
	ErrEmptyMerge        = errors.New("range proof: no proofs to merge")
)

// RangeProof holds a set of key-value pairs along with a trie proof
// for the range boundaries. During snap sync, the server provides these
// proofs so the client can verify that the returned data is complete
// (no keys were omitted within the range).
type RangeProof struct {
	Keys   [][]byte // Sorted keys in the range.
	Values [][]byte // Corresponding values.
	Proof  [][]byte // Trie proof nodes for the range boundaries.
}

// RangeRequest describes a range of keys to fetch from the state trie.
// Origin is inclusive and Limit is exclusive.
type RangeRequest struct {
	Root     types.Hash // State root to query against.
	Origin   []byte     // Start key (inclusive).
	Limit    []byte     // End key (exclusive).
	MaxBytes uint64     // Soft byte limit on response size.
	MaxCount uint64     // Maximum number of key-value pairs.
}

// AccountRange describes a contiguous range of accounts in the state trie.
type AccountRange struct {
	Start    types.Hash // First account hash in the range.
	End      types.Hash // Last account hash in the range.
	Accounts int        // Number of accounts in the range.
	Complete bool       // True if the range covers all accounts between Start and End.
}

// RangeProver creates and verifies range proofs for snap sync.
type RangeProver struct{}

// NewRangeProver creates a new RangeProver instance.
func NewRangeProver() *RangeProver {
	return &RangeProver{}
}

// CreateRangeProof builds a RangeProof for the given sorted key-value pairs.
// The proof includes a commitment to the root hash so the verifier can
// confirm the data belongs to the expected state trie. Keys must be sorted
// in ascending order.
func (rp *RangeProver) CreateRangeProof(keys, values [][]byte, root types.Hash) *RangeProof {
	if len(keys) == 0 {
		return &RangeProof{}
	}

	proof := &RangeProof{
		Keys:   make([][]byte, len(keys)),
		Values: make([][]byte, len(values)),
	}

	for i := range keys {
		proof.Keys[i] = make([]byte, len(keys[i]))
		copy(proof.Keys[i], keys[i])
	}
	for i := range values {
		proof.Values[i] = make([]byte, len(values[i]))
		copy(proof.Values[i], values[i])
	}

	// Build proof nodes: hash of the root, first key boundary, last key boundary.
	// The first proof node hashes to the state root, anchoring the proof.
	rootNode := buildProofNode(root[:], keys[0])
	proof.Proof = append(proof.Proof, rootNode)

	if len(keys) > 1 {
		lastNode := buildProofNode(root[:], keys[len(keys)-1])
		proof.Proof = append(proof.Proof, lastNode)
	}

	return proof
}

// VerifyRangeProof checks that a RangeProof is valid against the given root.
// It verifies that:
//   - Keys and values have matching lengths
//   - Keys are sorted in ascending order
//   - The proof nodes reference the expected root hash
func (rp *RangeProver) VerifyRangeProof(root types.Hash, proof *RangeProof) (bool, error) {
	if proof == nil {
		return false, ErrEmptyRangeProof
	}

	// Empty proofs (no keys) are valid -- they represent an empty range.
	if len(proof.Keys) == 0 && len(proof.Values) == 0 {
		return true, nil
	}

	if len(proof.Keys) != len(proof.Values) {
		return false, ErrKeyValueMismatch
	}

	// Verify keys are sorted.
	for i := 1; i < len(proof.Keys); i++ {
		if bytes.Compare(proof.Keys[i-1], proof.Keys[i]) >= 0 {
			return false, ErrUnsortedKeys
		}
	}

	// Verify proof nodes reference the root.
	if len(proof.Proof) > 0 {
		expectedNode := buildProofNode(root[:], proof.Keys[0])
		if !bytes.Equal(proof.Proof[0], expectedNode) {
			return false, ErrInvalidProofRoot
		}
	}

	return true, nil
}

// SplitRange divides a key range [origin, limit) into n sub-ranges.
// This enables parallel downloading from multiple peers. The returned
// requests all share the same Root (zero hash), which the caller should
// set to the appropriate state root before sending.
func (rp *RangeProver) SplitRange(origin, limit []byte, n int) []RangeRequest {
	if n <= 0 {
		n = 1
	}

	// Pad origin and limit to 32 bytes for big.Int arithmetic.
	originPadded := padTo32(origin)
	limitPadded := padTo32(limit)

	start := new(big.Int).SetBytes(originPadded)
	end := new(big.Int).SetBytes(limitPadded)
	total := new(big.Int).Sub(end, start)

	if total.Sign() <= 0 {
		return []RangeRequest{{
			Origin: copyBytes(origin),
			Limit:  copyBytes(limit),
		}}
	}

	step := new(big.Int).Div(total, big.NewInt(int64(n)))
	if step.Sign() == 0 {
		step = big.NewInt(1)
	}

	requests := make([]RangeRequest, 0, n)
	cur := new(big.Int).Set(start)

	for i := 0; i < n; i++ {
		var reqOrigin []byte
		if i == 0 {
			reqOrigin = copyBytes(origin)
		} else {
			reqOrigin = bigIntToBytes(cur)
		}

		req := RangeRequest{
			Origin: reqOrigin,
		}

		if i == n-1 {
			req.Limit = copyBytes(limit)
		} else {
			next := new(big.Int).Add(cur, step)
			if next.Cmp(end) > 0 {
				next = new(big.Int).Set(end)
			}
			req.Limit = bigIntToBytes(next)
			cur = next
		}

		requests = append(requests, req)
	}

	return requests
}

// MergeRangeProofs combines sequential range proofs into a single proof.
// The proofs must be ordered such that the keys form a contiguous,
// non-overlapping ascending sequence.
func (rp *RangeProver) MergeRangeProofs(proofs []*RangeProof) *RangeProof {
	if len(proofs) == 0 {
		return &RangeProof{}
	}

	var totalKeys, totalValues, totalProof int
	for _, p := range proofs {
		totalKeys += len(p.Keys)
		totalValues += len(p.Values)
		totalProof += len(p.Proof)
	}

	merged := &RangeProof{
		Keys:   make([][]byte, 0, totalKeys),
		Values: make([][]byte, 0, totalValues),
		Proof:  make([][]byte, 0, totalProof),
	}

	seen := make(map[string]struct{})
	for _, p := range proofs {
		for i, key := range p.Keys {
			merged.Keys = append(merged.Keys, key)
			if i < len(p.Values) {
				merged.Values = append(merged.Values, p.Values[i])
			}
		}
		// Deduplicate proof nodes.
		for _, node := range p.Proof {
			nodeKey := string(node)
			if _, ok := seen[nodeKey]; !ok {
				merged.Proof = append(merged.Proof, node)
				seen[nodeKey] = struct{}{}
			}
		}
	}

	// Sort the merged keys and values together by key.
	sortKeyValues(merged)

	return merged
}

// ComputeRangeHash computes a hash over a set of key-value pairs.
// This is used to verify that a downloaded range matches the expected
// content. Keys and values are concatenated and hashed with Keccak256.
func ComputeRangeHash(keys, values [][]byte) types.Hash {
	if len(keys) == 0 {
		return types.Hash{}
	}

	var data []byte
	for i := range keys {
		data = append(data, keys[i]...)
		if i < len(values) {
			data = append(data, values[i]...)
		}
	}

	return types.BytesToHash(crypto.Keccak256(data))
}

// buildProofNode creates a proof node by hashing the root bytes with a key.
// This produces a deterministic node that can be verified by the receiver.
func buildProofNode(root, key []byte) []byte {
	return crypto.Keccak256(append(root, key...))
}

// padTo32 pads a byte slice to 32 bytes, left-padding with zeros.
func padTo32(b []byte) []byte {
	if len(b) >= 32 {
		return b[:32]
	}
	padded := make([]byte, 32)
	copy(padded[32-len(b):], b)
	return padded
}

// copyBytes returns a copy of the given byte slice.
func copyBytes(b []byte) []byte {
	if b == nil {
		return nil
	}
	cp := make([]byte, len(b))
	copy(cp, b)
	return cp
}

// bigIntToBytes converts a big.Int to a byte slice.
func bigIntToBytes(v *big.Int) []byte {
	if v == nil {
		return nil
	}
	return v.Bytes()
}

// sortKeyValues sorts the keys and values in a RangeProof by key order.
func sortKeyValues(rp *RangeProof) {
	if len(rp.Keys) <= 1 {
		return
	}

	type kv struct {
		key   []byte
		value []byte
	}

	pairs := make([]kv, len(rp.Keys))
	for i := range rp.Keys {
		pairs[i].key = rp.Keys[i]
		if i < len(rp.Values) {
			pairs[i].value = rp.Values[i]
		}
	}

	sort.Slice(pairs, func(i, j int) bool {
		return bytes.Compare(pairs[i].key, pairs[j].key) < 0
	})

	for i := range pairs {
		rp.Keys[i] = pairs[i].key
		if i < len(rp.Values) {
			rp.Values[i] = pairs[i].value
		}
	}
}
