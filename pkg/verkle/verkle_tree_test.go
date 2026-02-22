package verkle

import (
	"bytes"
	"sync"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

func TestVerkleTreeImpl_NewEmpty(t *testing.T) {
	vt := NewVerkleTreeImpl()
	if vt == nil {
		t.Fatal("NewVerkleTreeImpl returned nil")
	}

	root := vt.Root()
	if root.IsZero() {
		t.Error("root of empty tree should not be zero hash")
	}
}

func TestVerkleTreeImpl_PutAndGet(t *testing.T) {
	vt := NewVerkleTreeImpl()

	key := make([]byte, KeySize)
	key[0] = 0x01
	key[1] = 0x02
	value := make([]byte, ValueSize)
	value[0] = 0xAA
	value[1] = 0xBB

	if err := vt.Put(key, value); err != nil {
		t.Fatalf("Put: %v", err)
	}

	got, err := vt.Get(key)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got == nil {
		t.Fatal("Get returned nil")
	}
	if !bytes.Equal(got, value) {
		t.Errorf("Get value mismatch: got %x, want %x", got, value)
	}
}

func TestVerkleTreeImpl_GetNonExistent(t *testing.T) {
	vt := NewVerkleTreeImpl()

	key := make([]byte, KeySize)
	key[0] = 0xFF

	got, err := vt.Get(key)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil for non-existent key, got %x", got)
	}
}

func TestVerkleTreeImpl_Delete(t *testing.T) {
	vt := NewVerkleTreeImpl()

	key := make([]byte, KeySize)
	key[0] = 0x01
	value := make([]byte, ValueSize)
	value[0] = 0xAA

	vt.Put(key, value)

	if err := vt.Delete(key); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	got, err := vt.Get(key)
	if err != nil {
		t.Fatalf("Get after delete: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil after delete, got %x", got)
	}
}

func TestVerkleTreeImpl_MultiplePuts(t *testing.T) {
	vt := NewVerkleTreeImpl()

	// Insert 10 key-value pairs with different stems.
	for i := 0; i < 10; i++ {
		key := make([]byte, KeySize)
		key[0] = byte(i + 1)
		value := make([]byte, ValueSize)
		value[0] = byte(i + 100)

		if err := vt.Put(key, value); err != nil {
			t.Fatalf("Put %d: %v", i, err)
		}
	}

	// Verify all values.
	for i := 0; i < 10; i++ {
		key := make([]byte, KeySize)
		key[0] = byte(i + 1)

		got, err := vt.Get(key)
		if err != nil {
			t.Fatalf("Get %d: %v", i, err)
		}
		if got == nil {
			t.Fatalf("Get %d: got nil", i)
		}
		if got[0] != byte(i+100) {
			t.Errorf("Get %d: got %d, want %d", i, got[0], i+100)
		}
	}
}

func TestVerkleTreeImpl_SameStemDifferentSuffix(t *testing.T) {
	vt := NewVerkleTreeImpl()

	// Two keys sharing the same stem (first 31 bytes), different suffix.
	key1 := make([]byte, KeySize)
	key1[0] = 0x01
	key1[StemSize] = 0x00

	key2 := make([]byte, KeySize)
	key2[0] = 0x01
	key2[StemSize] = 0x01

	val1 := make([]byte, ValueSize)
	val1[0] = 0xAA
	val2 := make([]byte, ValueSize)
	val2[0] = 0xBB

	vt.Put(key1, val1)
	vt.Put(key2, val2)

	got1, _ := vt.Get(key1)
	got2, _ := vt.Get(key2)

	if got1 == nil || got1[0] != 0xAA {
		t.Errorf("key1: got %v, want 0xAA", got1)
	}
	if got2 == nil || got2[0] != 0xBB {
		t.Errorf("key2: got %v, want 0xBB", got2)
	}
}

func TestVerkleTreeImpl_Overwrite(t *testing.T) {
	vt := NewVerkleTreeImpl()

	key := make([]byte, KeySize)
	key[0] = 0x01

	val1 := make([]byte, ValueSize)
	val1[0] = 0xAA
	val2 := make([]byte, ValueSize)
	val2[0] = 0xBB

	vt.Put(key, val1)
	vt.Put(key, val2)

	got, _ := vt.Get(key)
	if got == nil || got[0] != 0xBB {
		t.Errorf("overwritten value: got %v, want 0xBB", got)
	}
}

func TestVerkleTreeImpl_Commit(t *testing.T) {
	vt := NewVerkleTreeImpl()

	// Empty tree commit.
	root1, err := vt.Commit()
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}

	// Add a value and recommit.
	key := make([]byte, KeySize)
	key[0] = 0x01
	value := make([]byte, ValueSize)
	value[0] = 0xAA
	vt.Put(key, value)

	root2, err := vt.Commit()
	if err != nil {
		t.Fatalf("Commit after put: %v", err)
	}

	// Roots should differ.
	if root1 == root2 {
		t.Error("root should change after inserting a value")
	}

	// Commit again without changes should be stable.
	root3, _ := vt.Commit()
	if root2 != root3 {
		t.Error("root should be stable without changes")
	}
}

func TestVerkleTreeImpl_Root(t *testing.T) {
	vt := NewVerkleTreeImpl()

	key := make([]byte, KeySize)
	key[0] = 0x01
	value := make([]byte, ValueSize)
	value[0] = 0xAA
	vt.Put(key, value)

	r1 := vt.Root()
	r2 := vt.Root()
	if r1 != r2 {
		t.Error("Root() should be stable")
	}

	// Root should match Commit.
	committed, _ := vt.Commit()
	if r1 != committed {
		t.Error("Root() should match Commit()")
	}
}

func TestVerkleTreeImpl_InvalidKeySize(t *testing.T) {
	vt := NewVerkleTreeImpl()

	// Too short.
	_, err := vt.Get([]byte{0x01})
	if err == nil {
		t.Error("Get with short key should fail")
	}

	err = vt.Put([]byte{0x01}, make([]byte, ValueSize))
	if err == nil {
		t.Error("Put with short key should fail")
	}

	err = vt.Delete([]byte{0x01})
	if err == nil {
		t.Error("Delete with short key should fail")
	}

	// Too long.
	_, err = vt.Get(make([]byte, KeySize+1))
	if err == nil {
		t.Error("Get with long key should fail")
	}
}

func TestVerkleTreeImpl_InvalidValueSize(t *testing.T) {
	vt := NewVerkleTreeImpl()

	key := make([]byte, KeySize)
	err := vt.Put(key, []byte{0x01})
	if err == nil {
		t.Error("Put with short value should fail")
	}

	err = vt.Put(key, make([]byte, ValueSize+1))
	if err == nil {
		t.Error("Put with long value should fail")
	}
}

func TestVerkleTreeImpl_ProveInclusion(t *testing.T) {
	vt := NewVerkleTreeImpl()

	key := make([]byte, KeySize)
	key[0] = 0x01
	value := make([]byte, ValueSize)
	value[0] = 0xAA

	vt.Put(key, value)

	proof, err := vt.Prove(key)
	if err != nil {
		t.Fatalf("Prove: %v", err)
	}

	if proof == nil {
		t.Fatal("proof is nil")
	}
	if !proof.IsSufficiencyProof() {
		t.Error("should be an inclusion proof")
	}
	if proof.IsAbsenceProof() {
		t.Error("should not be an absence proof")
	}
	if proof.Value == nil {
		t.Fatal("proof value is nil")
	}
	if proof.Value[0] != 0xAA {
		t.Errorf("proof value[0] = %d, want 0xAA", proof.Value[0])
	}
	if len(proof.CommitmentsByPath) == 0 {
		t.Error("CommitmentsByPath should not be empty")
	}
	if len(proof.IPAProof) == 0 {
		t.Error("IPAProof should not be empty")
	}
}

func TestVerkleTreeImpl_ProveAbsence(t *testing.T) {
	vt := NewVerkleTreeImpl()

	// Insert a value at one key.
	existingKey := make([]byte, KeySize)
	existingKey[0] = 0x01
	vt.Put(existingKey, make([]byte, ValueSize))

	// Prove absence of a different key.
	absentKey := make([]byte, KeySize)
	absentKey[0] = 0xFF

	proof, err := vt.Prove(absentKey)
	if err != nil {
		t.Fatalf("Prove: %v", err)
	}

	if proof.IsSufficiencyProof() {
		t.Error("should not be an inclusion proof for absent key")
	}
}

func TestVerkleTreeImpl_ProveInvalidKey(t *testing.T) {
	vt := NewVerkleTreeImpl()

	_, err := vt.Prove([]byte{0x01})
	if err == nil {
		t.Error("Prove with invalid key should fail")
	}
}

func TestVerkleTreeImpl_VerifyProof_Success(t *testing.T) {
	vt := NewVerkleTreeImpl()

	key := make([]byte, KeySize)
	key[0] = 0x01
	value := make([]byte, ValueSize)
	value[0] = 0xAA

	vt.Put(key, value)
	root := vt.Root()

	proof, err := vt.Prove(key)
	if err != nil {
		t.Fatalf("Prove: %v", err)
	}

	if !vt.VerifyProof(root, key, value, proof) {
		t.Error("VerifyProof should succeed for valid proof")
	}
}

func TestVerkleTreeImpl_VerifyProof_WrongRoot(t *testing.T) {
	vt := NewVerkleTreeImpl()

	key := make([]byte, KeySize)
	key[0] = 0x01
	value := make([]byte, ValueSize)
	value[0] = 0xAA

	vt.Put(key, value)

	proof, _ := vt.Prove(key)

	wrongRoot := types.BytesToHash([]byte{0xDE, 0xAD})
	if vt.VerifyProof(wrongRoot, key, value, proof) {
		t.Error("VerifyProof should fail with wrong root")
	}
}

func TestVerkleTreeImpl_VerifyProof_WrongValue(t *testing.T) {
	vt := NewVerkleTreeImpl()

	key := make([]byte, KeySize)
	key[0] = 0x01
	value := make([]byte, ValueSize)
	value[0] = 0xAA

	vt.Put(key, value)
	root := vt.Root()

	proof, _ := vt.Prove(key)

	wrongValue := make([]byte, ValueSize)
	wrongValue[0] = 0xFF
	if vt.VerifyProof(root, key, wrongValue, proof) {
		t.Error("VerifyProof should fail with wrong value")
	}
}

func TestVerkleTreeImpl_VerifyProof_WrongKey(t *testing.T) {
	vt := NewVerkleTreeImpl()

	key := make([]byte, KeySize)
	key[0] = 0x01
	value := make([]byte, ValueSize)
	value[0] = 0xAA

	vt.Put(key, value)
	root := vt.Root()

	proof, _ := vt.Prove(key)

	wrongKey := make([]byte, KeySize)
	wrongKey[0] = 0xFF
	if vt.VerifyProof(root, wrongKey, value, proof) {
		t.Error("VerifyProof should fail with wrong key")
	}
}

func TestVerkleTreeImpl_VerifyProof_NilProof(t *testing.T) {
	vt := NewVerkleTreeImpl()

	root := vt.Root()
	if vt.VerifyProof(root, make([]byte, KeySize), make([]byte, ValueSize), nil) {
		t.Error("VerifyProof should fail with nil proof")
	}
}

func TestVerkleTreeImpl_VerifyProof_TamperedIPA(t *testing.T) {
	vt := NewVerkleTreeImpl()

	key := make([]byte, KeySize)
	key[0] = 0x01
	value := make([]byte, ValueSize)
	value[0] = 0xAA

	vt.Put(key, value)
	root := vt.Root()

	proof, _ := vt.Prove(key)

	// Tamper with IPA proof.
	proof.IPAProof[0] ^= 0xFF
	if vt.VerifyProof(root, key, value, proof) {
		t.Error("VerifyProof should fail with tampered IPA")
	}
}

func TestVerkleTreeImpl_GenerateProof_Alias(t *testing.T) {
	vt := NewVerkleTreeImpl()

	key := make([]byte, KeySize)
	key[0] = 0x01
	value := make([]byte, ValueSize)
	value[0] = 0xAA
	vt.Put(key, value)

	proof, err := vt.GenerateProof(key)
	if err != nil {
		t.Fatalf("GenerateProof: %v", err)
	}
	if proof == nil {
		t.Fatal("GenerateProof returned nil")
	}
	if !proof.IsSufficiencyProof() {
		t.Error("should be an inclusion proof")
	}
}

func TestVerkleTreeImpl_NodeCount(t *testing.T) {
	vt := NewVerkleTreeImpl()

	// Empty tree: just the root internal node.
	if vt.NodeCount() != 1 {
		t.Errorf("empty tree NodeCount = %d, want 1", vt.NodeCount())
	}

	// One insert: root + leaf.
	key := make([]byte, KeySize)
	key[0] = 0x01
	vt.Put(key, make([]byte, ValueSize))

	count := vt.NodeCount()
	if count < 2 {
		t.Errorf("NodeCount after 1 insert = %d, want >= 2", count)
	}
}

func TestStemFromAddress(t *testing.T) {
	addr1 := types.BytesToAddress([]byte{0x01})
	addr2 := types.BytesToAddress([]byte{0x02})

	stem1 := StemFromAddress(addr1)
	stem2 := StemFromAddress(addr2)

	if len(stem1) != StemSize {
		t.Errorf("stem1 length = %d, want %d", len(stem1), StemSize)
	}
	if len(stem2) != StemSize {
		t.Errorf("stem2 length = %d, want %d", len(stem2), StemSize)
	}

	// Different addresses should produce different stems.
	if bytes.Equal(stem1, stem2) {
		t.Error("different addresses should produce different stems")
	}

	// Same address should produce same stem.
	stem1b := StemFromAddress(addr1)
	if !bytes.Equal(stem1, stem1b) {
		t.Error("same address should produce same stem")
	}
}

func TestVerkleTreeImpl_StemCollision(t *testing.T) {
	vt := NewVerkleTreeImpl()

	// Two keys sharing the first byte but diverging at byte 1.
	key1 := make([]byte, KeySize)
	key1[0] = 0x01
	key1[1] = 0x01

	key2 := make([]byte, KeySize)
	key2[0] = 0x01
	key2[1] = 0x02

	val1 := make([]byte, ValueSize)
	val1[0] = 0xAA
	val2 := make([]byte, ValueSize)
	val2[0] = 0xBB

	vt.Put(key1, val1)
	vt.Put(key2, val2)

	got1, _ := vt.Get(key1)
	got2, _ := vt.Get(key2)

	if got1 == nil || got1[0] != 0xAA {
		t.Errorf("key1: got %v, want 0xAA", got1)
	}
	if got2 == nil || got2[0] != 0xBB {
		t.Errorf("key2: got %v, want 0xBB", got2)
	}
}

func TestVerkleTreeImpl_ConcurrentReadWrite(t *testing.T) {
	vt := NewVerkleTreeImpl()

	var wg sync.WaitGroup
	n := 50

	// Concurrent writes.
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(idx int) {
			defer wg.Done()
			key := make([]byte, KeySize)
			key[0] = byte(idx)
			value := make([]byte, ValueSize)
			value[0] = byte(idx + 100)
			vt.Put(key, value)
		}(i)
	}
	wg.Wait()

	// Concurrent reads.
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(idx int) {
			defer wg.Done()
			key := make([]byte, KeySize)
			key[0] = byte(idx)
			got, err := vt.Get(key)
			if err != nil {
				t.Errorf("concurrent Get %d: %v", idx, err)
				return
			}
			if got == nil {
				t.Errorf("concurrent Get %d: nil", idx)
				return
			}
			if got[0] != byte(idx+100) {
				t.Errorf("concurrent Get %d: got %d, want %d", idx, got[0], idx+100)
			}
		}(i)
	}
	wg.Wait()
}

func TestVerkleTreeImpl_ConcurrentProve(t *testing.T) {
	vt := NewVerkleTreeImpl()

	// Insert some values.
	for i := 0; i < 20; i++ {
		key := make([]byte, KeySize)
		key[0] = byte(i)
		value := make([]byte, ValueSize)
		value[0] = byte(i)
		vt.Put(key, value)
	}

	var wg sync.WaitGroup
	wg.Add(20)
	for i := 0; i < 20; i++ {
		go func(idx int) {
			defer wg.Done()
			key := make([]byte, KeySize)
			key[0] = byte(idx)
			proof, err := vt.Prove(key)
			if err != nil {
				t.Errorf("Prove %d: %v", idx, err)
				return
			}
			if proof == nil {
				t.Errorf("Prove %d: nil", idx)
			}
		}(i)
	}
	wg.Wait()
}

func TestVerkleTreeImpl_ConcurrentCommit(t *testing.T) {
	vt := NewVerkleTreeImpl()

	key := make([]byte, KeySize)
	key[0] = 0x01
	value := make([]byte, ValueSize)
	value[0] = 0xAA
	vt.Put(key, value)

	var wg sync.WaitGroup
	roots := make([]types.Hash, 10)
	wg.Add(10)
	for i := 0; i < 10; i++ {
		go func(idx int) {
			defer wg.Done()
			root, err := vt.Commit()
			if err != nil {
				t.Errorf("Commit %d: %v", idx, err)
				return
			}
			roots[idx] = root
		}(i)
	}
	wg.Wait()

	// All commits should produce the same root.
	for i := 1; i < 10; i++ {
		if roots[i] != roots[0] {
			t.Errorf("Commit %d root differs from Commit 0", i)
		}
	}
}

func TestVerkleTreeImpl_DeleteNonExistent(t *testing.T) {
	vt := NewVerkleTreeImpl()

	key := make([]byte, KeySize)
	key[0] = 0xFF

	// Deleting non-existent key should not error.
	if err := vt.Delete(key); err != nil {
		t.Fatalf("Delete non-existent: %v", err)
	}
}

func TestVerkleTreeImpl_ProofVerifyRoundtrip(t *testing.T) {
	vt := NewVerkleTreeImpl()

	// Insert several values.
	for i := 0; i < 5; i++ {
		key := make([]byte, KeySize)
		key[0] = byte(i + 1)
		value := make([]byte, ValueSize)
		value[0] = byte(i + 10)
		vt.Put(key, value)
	}

	root := vt.Root()

	// Prove and verify each.
	for i := 0; i < 5; i++ {
		key := make([]byte, KeySize)
		key[0] = byte(i + 1)
		value := make([]byte, ValueSize)
		value[0] = byte(i + 10)

		proof, err := vt.Prove(key)
		if err != nil {
			t.Fatalf("Prove %d: %v", i, err)
		}

		if !vt.VerifyProof(root, key, value, proof) {
			t.Errorf("VerifyProof failed for key %d", i)
		}
	}
}

func TestVerkleTreeImpl_CommitChangesWithData(t *testing.T) {
	vt := NewVerkleTreeImpl()

	root1 := vt.Root()

	key := make([]byte, KeySize)
	key[0] = 0x01
	value := make([]byte, ValueSize)
	value[0] = 0xAA
	vt.Put(key, value)

	root2 := vt.Root()

	if root1 == root2 {
		t.Error("root should change after insert")
	}

	// Insert a second value with a different stem and verify root changes.
	key2 := make([]byte, KeySize)
	key2[0] = 0x02
	value2 := make([]byte, ValueSize)
	value2[0] = 0xBB
	vt.Put(key2, value2)

	root3 := vt.Root()
	if root2 == root3 {
		t.Error("root should change after second insert")
	}

	// Insert a third value and verify root changes again.
	key3 := make([]byte, KeySize)
	key3[0] = 0x03
	value3 := make([]byte, ValueSize)
	value3[0] = 0xCC
	vt.Put(key3, value3)

	root4 := vt.Root()
	if root3 == root4 {
		t.Error("root should change after third insert")
	}
}
