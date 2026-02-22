package sync

import (
	"bytes"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

func TestRangeProver_CreateAndVerify(t *testing.T) {
	rp := NewRangeProver()
	root := types.Hash{0x01, 0x02, 0x03}

	keys := [][]byte{
		{0x01, 0x00},
		{0x02, 0x00},
		{0x03, 0x00},
	}
	values := [][]byte{
		{0xaa},
		{0xbb},
		{0xcc},
	}

	proof := rp.CreateRangeProof(keys, values, root)
	if proof == nil {
		t.Fatal("CreateRangeProof returned nil")
	}
	if len(proof.Keys) != 3 {
		t.Fatalf("expected 3 keys, got %d", len(proof.Keys))
	}
	if len(proof.Values) != 3 {
		t.Fatalf("expected 3 values, got %d", len(proof.Values))
	}
	if len(proof.Proof) == 0 {
		t.Fatal("expected non-empty proof")
	}

	ok, err := rp.VerifyRangeProof(root, proof)
	if err != nil {
		t.Fatalf("VerifyRangeProof: %v", err)
	}
	if !ok {
		t.Fatal("proof should be valid")
	}
}

func TestRangeProver_VerifyBadRoot(t *testing.T) {
	rp := NewRangeProver()
	root := types.Hash{0x01, 0x02, 0x03}
	wrongRoot := types.Hash{0xff, 0xfe, 0xfd}

	keys := [][]byte{{0x01}, {0x02}}
	values := [][]byte{{0xaa}, {0xbb}}

	proof := rp.CreateRangeProof(keys, values, root)
	ok, err := rp.VerifyRangeProof(wrongRoot, proof)
	if err == nil {
		t.Fatal("expected error for wrong root")
	}
	if ok {
		t.Fatal("proof should be invalid for wrong root")
	}
}

func TestRangeProver_VerifyUnsortedKeys(t *testing.T) {
	rp := NewRangeProver()
	root := types.Hash{0x01}

	// Manually build a proof with unsorted keys.
	proof := &RangeProof{
		Keys:   [][]byte{{0x03}, {0x01}},
		Values: [][]byte{{0xaa}, {0xbb}},
	}

	ok, err := rp.VerifyRangeProof(root, proof)
	if err == nil {
		t.Fatal("expected error for unsorted keys")
	}
	if ok {
		t.Fatal("proof should be invalid for unsorted keys")
	}
}

func TestRangeProver_VerifyKeyValueMismatch(t *testing.T) {
	rp := NewRangeProver()
	root := types.Hash{0x01}

	proof := &RangeProof{
		Keys:   [][]byte{{0x01}, {0x02}},
		Values: [][]byte{{0xaa}}, // Only one value for two keys.
	}

	ok, err := rp.VerifyRangeProof(root, proof)
	if err == nil {
		t.Fatal("expected error for key/value mismatch")
	}
	if ok {
		t.Fatal("proof should be invalid for key/value mismatch")
	}
}

func TestRangeProver_VerifyNilProof(t *testing.T) {
	rp := NewRangeProver()
	ok, err := rp.VerifyRangeProof(types.Hash{0x01}, nil)
	if err == nil {
		t.Fatal("expected error for nil proof")
	}
	if ok {
		t.Fatal("nil proof should be invalid")
	}
}

func TestRangeProver_VerifyEmptyProof(t *testing.T) {
	rp := NewRangeProver()
	proof := &RangeProof{}

	ok, err := rp.VerifyRangeProof(types.Hash{0x01}, proof)
	if err != nil {
		t.Fatalf("empty proof should be valid: %v", err)
	}
	if !ok {
		t.Fatal("empty proof should be valid")
	}
}

func TestRangeProver_SplitRange(t *testing.T) {
	rp := NewRangeProver()
	origin := []byte{0x00}
	limit := []byte{0xff}

	requests := rp.SplitRange(origin, limit, 4)
	if len(requests) != 4 {
		t.Fatalf("expected 4 ranges, got %d", len(requests))
	}

	// First range starts at origin.
	if !bytes.Equal(requests[0].Origin, origin) {
		t.Fatalf("first range origin: want %x, got %x", origin, requests[0].Origin)
	}

	// Last range ends at limit.
	if !bytes.Equal(requests[len(requests)-1].Limit, limit) {
		t.Fatalf("last range limit: want %x, got %x", limit, requests[len(requests)-1].Limit)
	}

	// Ranges should be non-overlapping and contiguous.
	for i := 1; i < len(requests); i++ {
		prevLimit := padTo32(requests[i-1].Limit)
		currOrigin := padTo32(requests[i].Origin)
		if bytes.Compare(currOrigin, prevLimit) < 0 {
			t.Errorf("range[%d].Origin %x < range[%d].Limit %x (overlap)",
				i, requests[i].Origin, i-1, requests[i-1].Limit)
		}
	}
}

func TestRangeProver_SplitRangeSingle(t *testing.T) {
	rp := NewRangeProver()
	origin := []byte{0x10}
	limit := []byte{0x20}

	requests := rp.SplitRange(origin, limit, 1)
	if len(requests) != 1 {
		t.Fatalf("expected 1 range, got %d", len(requests))
	}
	if !bytes.Equal(requests[0].Origin, origin) {
		t.Fatalf("origin mismatch: want %x, got %x", origin, requests[0].Origin)
	}
	if !bytes.Equal(requests[0].Limit, limit) {
		t.Fatalf("limit mismatch: want %x, got %x", limit, requests[0].Limit)
	}
}

func TestRangeProver_SplitRangeZero(t *testing.T) {
	rp := NewRangeProver()
	// n=0 should be treated as n=1.
	requests := rp.SplitRange([]byte{0x00}, []byte{0xff}, 0)
	if len(requests) != 1 {
		t.Fatalf("expected 1 range for n=0, got %d", len(requests))
	}
}

func TestRangeProver_SplitRangeInvertedBounds(t *testing.T) {
	rp := NewRangeProver()
	// origin > limit: should return a single range.
	requests := rp.SplitRange([]byte{0xff}, []byte{0x01}, 4)
	if len(requests) != 1 {
		t.Fatalf("expected 1 range for inverted bounds, got %d", len(requests))
	}
}

func TestRangeProver_MergeProofs(t *testing.T) {
	rp := NewRangeProver()
	root := types.Hash{0x01}

	proof1 := rp.CreateRangeProof(
		[][]byte{{0x01}, {0x02}},
		[][]byte{{0xaa}, {0xbb}},
		root,
	)
	proof2 := rp.CreateRangeProof(
		[][]byte{{0x03}, {0x04}},
		[][]byte{{0xcc}, {0xdd}},
		root,
	)

	merged := rp.MergeRangeProofs([]*RangeProof{proof1, proof2})
	if len(merged.Keys) != 4 {
		t.Fatalf("expected 4 merged keys, got %d", len(merged.Keys))
	}
	if len(merged.Values) != 4 {
		t.Fatalf("expected 4 merged values, got %d", len(merged.Values))
	}

	// Verify sorted order.
	for i := 1; i < len(merged.Keys); i++ {
		if bytes.Compare(merged.Keys[i-1], merged.Keys[i]) >= 0 {
			t.Fatal("merged keys not sorted")
		}
	}

	// Proof nodes should be deduplicated.
	if len(merged.Proof) == 0 {
		t.Fatal("merged proof should have proof nodes")
	}
}

func TestRangeProver_MergeEmptyProofs(t *testing.T) {
	rp := NewRangeProver()
	merged := rp.MergeRangeProofs(nil)
	if len(merged.Keys) != 0 {
		t.Fatalf("expected 0 keys for nil merge, got %d", len(merged.Keys))
	}
}

func TestRangeProver_MergeSingleProof(t *testing.T) {
	rp := NewRangeProver()
	root := types.Hash{0xab}

	proof := rp.CreateRangeProof(
		[][]byte{{0x01}, {0x02}},
		[][]byte{{0xaa}, {0xbb}},
		root,
	)

	merged := rp.MergeRangeProofs([]*RangeProof{proof})
	if len(merged.Keys) != 2 {
		t.Fatalf("expected 2 keys, got %d", len(merged.Keys))
	}
}

func TestComputeRangeHash(t *testing.T) {
	keys := [][]byte{{0x01}, {0x02}}
	values := [][]byte{{0xaa}, {0xbb}}

	hash := ComputeRangeHash(keys, values)
	if hash.IsZero() {
		t.Fatal("range hash should not be zero")
	}

	// Same input should produce the same hash.
	hash2 := ComputeRangeHash(keys, values)
	if hash != hash2 {
		t.Fatal("same input should produce same hash")
	}

	// Different input should produce different hash.
	hash3 := ComputeRangeHash([][]byte{{0x03}}, [][]byte{{0xcc}})
	if hash == hash3 {
		t.Fatal("different input should produce different hash")
	}
}

func TestComputeRangeHash_Empty(t *testing.T) {
	hash := ComputeRangeHash(nil, nil)
	if !hash.IsZero() {
		t.Fatal("empty range hash should be zero")
	}
}

func TestAccountRange_Struct(t *testing.T) {
	ar := AccountRange{
		Start:    types.Hash{0x01},
		End:      types.Hash{0xff},
		Accounts: 100,
		Complete: true,
	}

	if ar.Start.IsZero() {
		t.Fatal("start should not be zero")
	}
	if ar.Accounts != 100 {
		t.Fatalf("expected 100 accounts, got %d", ar.Accounts)
	}
	if !ar.Complete {
		t.Fatal("should be complete")
	}
}

func TestRangeRequest_Struct(t *testing.T) {
	req := RangeRequest{
		Root:     types.Hash{0xab},
		Origin:   []byte{0x00},
		Limit:    []byte{0xff},
		MaxBytes: 1024 * 1024,
		MaxCount: 1000,
	}

	if req.Root.IsZero() {
		t.Fatal("root should not be zero")
	}
	if req.MaxBytes != 1024*1024 {
		t.Fatalf("max bytes: want %d, got %d", 1024*1024, req.MaxBytes)
	}
	if req.MaxCount != 1000 {
		t.Fatalf("max count: want 1000, got %d", req.MaxCount)
	}
}

func TestRangeProver_CreateEmptyRange(t *testing.T) {
	rp := NewRangeProver()
	root := types.Hash{0x01}

	proof := rp.CreateRangeProof(nil, nil, root)
	if len(proof.Keys) != 0 {
		t.Fatalf("expected 0 keys, got %d", len(proof.Keys))
	}
	if len(proof.Proof) != 0 {
		t.Fatalf("expected 0 proof nodes, got %d", len(proof.Proof))
	}

	ok, err := rp.VerifyRangeProof(root, proof)
	if err != nil {
		t.Fatalf("verify empty proof: %v", err)
	}
	if !ok {
		t.Fatal("empty proof should verify")
	}
}

func TestRangeProver_CreateSingleKeyProof(t *testing.T) {
	rp := NewRangeProver()
	root := types.Hash{0xde, 0xad}

	keys := [][]byte{{0x42}}
	values := [][]byte{{0xff}}

	proof := rp.CreateRangeProof(keys, values, root)
	if len(proof.Keys) != 1 {
		t.Fatalf("expected 1 key, got %d", len(proof.Keys))
	}
	// Single key proof should have exactly 1 proof node (no last boundary needed).
	if len(proof.Proof) != 1 {
		t.Fatalf("expected 1 proof node for single key, got %d", len(proof.Proof))
	}

	ok, err := rp.VerifyRangeProof(root, proof)
	if err != nil {
		t.Fatalf("verify single key proof: %v", err)
	}
	if !ok {
		t.Fatal("single key proof should verify")
	}
}

func TestRangeProof_DeepCopy(t *testing.T) {
	rp := NewRangeProver()
	root := types.Hash{0x01}

	keys := [][]byte{{0x01}, {0x02}}
	values := [][]byte{{0xaa}, {0xbb}}

	proof := rp.CreateRangeProof(keys, values, root)

	// Mutate original keys -- proof should be unaffected.
	keys[0][0] = 0xff
	values[0][0] = 0xff

	if proof.Keys[0][0] == 0xff {
		t.Fatal("proof keys should be independent of original")
	}
	if proof.Values[0][0] == 0xff {
		t.Fatal("proof values should be independent of original")
	}
}

func TestRangeProver_SplitLargeRange(t *testing.T) {
	rp := NewRangeProver()

	// 32-byte origin and limit, simulating full hash space.
	origin := make([]byte, 32)
	limit := make([]byte, 32)
	for i := range limit {
		limit[i] = 0xff
	}

	requests := rp.SplitRange(origin, limit, 16)
	if len(requests) != 16 {
		t.Fatalf("expected 16 ranges, got %d", len(requests))
	}

	// All ranges should be non-overlapping.
	for i := 1; i < len(requests); i++ {
		prev := padTo32(requests[i-1].Limit)
		curr := padTo32(requests[i].Origin)
		if bytes.Compare(curr, prev) < 0 {
			t.Errorf("range[%d] overlaps with range[%d]", i, i-1)
		}
	}
}

func TestComputeRangeHash_OrderMatters(t *testing.T) {
	keys1 := [][]byte{{0x01}, {0x02}}
	vals1 := [][]byte{{0xaa}, {0xbb}}

	keys2 := [][]byte{{0x02}, {0x01}}
	vals2 := [][]byte{{0xbb}, {0xaa}}

	hash1 := ComputeRangeHash(keys1, vals1)
	hash2 := ComputeRangeHash(keys2, vals2)

	// Different order should produce different hashes.
	if hash1 == hash2 {
		t.Fatal("different key order should produce different hashes")
	}
}
