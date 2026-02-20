package trie

import (
	"bytes"
	"sync"
	"testing"
)

func TestDiffEntry_Types(t *testing.T) {
	insert := &DiffEntry{Key: []byte("a"), NewValue: []byte("v")}
	if !insert.IsInsert() {
		t.Fatal("should be insert")
	}
	if insert.IsDelete() || insert.IsUpdate() {
		t.Fatal("insert should not be delete or update")
	}

	del := &DiffEntry{Key: []byte("b"), OldValue: []byte("v")}
	if !del.IsDelete() {
		t.Fatal("should be delete")
	}
	if del.IsInsert() || del.IsUpdate() {
		t.Fatal("delete should not be insert or update")
	}

	upd := &DiffEntry{Key: []byte("c"), OldValue: []byte("old"), NewValue: []byte("new")}
	if !upd.IsUpdate() {
		t.Fatal("should be update")
	}
	if upd.IsInsert() || upd.IsDelete() {
		t.Fatal("update should not be insert or delete")
	}
}

func TestDiffTracker_RecordInsert(t *testing.T) {
	dt := NewDiffTracker()
	dt.RecordInsert([]byte("key"), []byte("val"))

	if dt.Len() != 1 {
		t.Fatalf("Len() = %d, want 1", dt.Len())
	}

	inserts := dt.Inserts()
	if len(inserts) != 1 {
		t.Fatalf("Inserts() = %d, want 1", len(inserts))
	}
	if string(inserts[0].Key) != "key" || string(inserts[0].NewValue) != "val" {
		t.Fatal("insert data mismatch")
	}
}

func TestDiffTracker_RecordDelete(t *testing.T) {
	dt := NewDiffTracker()
	dt.RecordDelete([]byte("key"), []byte("oldval"))

	deletes := dt.Deletes()
	if len(deletes) != 1 {
		t.Fatalf("Deletes() = %d, want 1", len(deletes))
	}
	if string(deletes[0].OldValue) != "oldval" {
		t.Fatal("delete old value mismatch")
	}
}

func TestDiffTracker_RecordUpdate(t *testing.T) {
	dt := NewDiffTracker()
	dt.RecordUpdate([]byte("key"), []byte("old"), []byte("new"))

	updates := dt.Updates()
	if len(updates) != 1 {
		t.Fatalf("Updates() = %d, want 1", len(updates))
	}
	if string(updates[0].OldValue) != "old" || string(updates[0].NewValue) != "new" {
		t.Fatal("update values mismatch")
	}
}

func TestDiffTracker_InsertThenDelete_CancelsOut(t *testing.T) {
	dt := NewDiffTracker()
	dt.RecordInsert([]byte("key"), []byte("val"))
	dt.RecordDelete([]byte("key"), nil)

	if dt.Len() != 0 {
		t.Fatalf("insert+delete should cancel out, Len() = %d", dt.Len())
	}
}

func TestDiffTracker_DeleteThenInsert_BecomesUpdate(t *testing.T) {
	dt := NewDiffTracker()
	dt.RecordDelete([]byte("key"), []byte("old"))
	dt.RecordInsert([]byte("key"), []byte("new"))

	if dt.Len() != 1 {
		t.Fatalf("Len() = %d, want 1", dt.Len())
	}

	entry := dt.Get([]byte("key"))
	if entry == nil {
		t.Fatal("expected entry for key")
	}
	if string(entry.OldValue) != "old" || string(entry.NewValue) != "new" {
		t.Fatalf("expected update: old=%q new=%q", entry.OldValue, entry.NewValue)
	}
}

func TestDiffTracker_Entries_SortedByKey(t *testing.T) {
	dt := NewDiffTracker()
	dt.RecordInsert([]byte("zebra"), []byte("z"))
	dt.RecordInsert([]byte("alpha"), []byte("a"))
	dt.RecordInsert([]byte("mango"), []byte("m"))

	entries := dt.Entries()
	if len(entries) != 3 {
		t.Fatalf("Entries() = %d, want 3", len(entries))
	}
	for i := 1; i < len(entries); i++ {
		if bytes.Compare(entries[i-1].Key, entries[i].Key) >= 0 {
			t.Fatalf("not sorted at index %d: %q >= %q", i, entries[i-1].Key, entries[i].Key)
		}
	}
}

func TestDiffTracker_Has(t *testing.T) {
	dt := NewDiffTracker()
	dt.RecordInsert([]byte("exists"), []byte("v"))

	if !dt.Has([]byte("exists")) {
		t.Fatal("Has should return true for tracked key")
	}
	if dt.Has([]byte("missing")) {
		t.Fatal("Has should return false for untracked key")
	}
}

func TestDiffTracker_Get(t *testing.T) {
	dt := NewDiffTracker()
	dt.RecordInsert([]byte("a"), []byte("1"))

	entry := dt.Get([]byte("a"))
	if entry == nil {
		t.Fatal("Get returned nil")
	}
	if string(entry.NewValue) != "1" {
		t.Fatalf("Get value = %q, want 1", entry.NewValue)
	}

	if dt.Get([]byte("missing")) != nil {
		t.Fatal("Get should return nil for missing key")
	}
}

func TestDiffTracker_Reset(t *testing.T) {
	dt := NewDiffTracker()
	dt.RecordInsert([]byte("a"), []byte("1"))
	dt.RecordInsert([]byte("b"), []byte("2"))

	dt.Reset()
	if dt.Len() != 0 {
		t.Fatalf("Len after reset = %d, want 0", dt.Len())
	}
}

func TestDiffTracker_Summary(t *testing.T) {
	dt := NewDiffTracker()
	dt.RecordInsert([]byte("new1"), []byte("val1"))
	dt.RecordInsert([]byte("new2"), []byte("val22"))
	dt.RecordDelete([]byte("old1"), []byte("x"))
	dt.RecordUpdate([]byte("chg1"), []byte("a"), []byte("bbb"))

	s := dt.Summary()
	if s.Inserts != 2 {
		t.Fatalf("Inserts = %d, want 2", s.Inserts)
	}
	if s.Deletes != 1 {
		t.Fatalf("Deletes = %d, want 1", s.Deletes)
	}
	if s.Updates != 1 {
		t.Fatalf("Updates = %d, want 1", s.Updates)
	}
	// TotalBytes = len("val1") + len("val22") + len("bbb") = 4 + 5 + 3 = 12
	if s.TotalBytes != 12 {
		t.Fatalf("TotalBytes = %d, want 12", s.TotalBytes)
	}
}

func TestComputeTrieDiff_Empty(t *testing.T) {
	a := New()
	b := New()

	dt := ComputeTrieDiff(a, b)
	if dt.Len() != 0 {
		t.Fatalf("diff of two empty tries should be empty, got %d", dt.Len())
	}
}

func TestComputeTrieDiff_Inserts(t *testing.T) {
	a := New()
	b := New()
	b.Put([]byte("new1"), []byte("val1"))
	b.Put([]byte("new2"), []byte("val2"))

	dt := ComputeTrieDiff(a, b)
	if dt.Len() != 2 {
		t.Fatalf("Len() = %d, want 2", dt.Len())
	}

	inserts := dt.Inserts()
	if len(inserts) != 2 {
		t.Fatalf("Inserts = %d, want 2", len(inserts))
	}
}

func TestComputeTrieDiff_Deletes(t *testing.T) {
	a := New()
	a.Put([]byte("old1"), []byte("val1"))
	a.Put([]byte("old2"), []byte("val2"))
	b := New()

	dt := ComputeTrieDiff(a, b)
	if dt.Len() != 2 {
		t.Fatalf("Len() = %d, want 2", dt.Len())
	}

	deletes := dt.Deletes()
	if len(deletes) != 2 {
		t.Fatalf("Deletes = %d, want 2", len(deletes))
	}
}

func TestComputeTrieDiff_Updates(t *testing.T) {
	a := New()
	a.Put([]byte("key"), []byte("old"))
	b := New()
	b.Put([]byte("key"), []byte("new"))

	dt := ComputeTrieDiff(a, b)
	if dt.Len() != 1 {
		t.Fatalf("Len() = %d, want 1", dt.Len())
	}

	updates := dt.Updates()
	if len(updates) != 1 {
		t.Fatalf("Updates = %d, want 1", len(updates))
	}
	if string(updates[0].OldValue) != "old" || string(updates[0].NewValue) != "new" {
		t.Fatal("update values mismatch")
	}
}

func TestComputeTrieDiff_Mixed(t *testing.T) {
	a := New()
	a.Put([]byte("keep"), []byte("same"))
	a.Put([]byte("change"), []byte("old"))
	a.Put([]byte("remove"), []byte("gone"))

	b := New()
	b.Put([]byte("keep"), []byte("same"))
	b.Put([]byte("change"), []byte("new"))
	b.Put([]byte("add"), []byte("fresh"))

	dt := ComputeTrieDiff(a, b)

	if dt.Len() != 3 {
		t.Fatalf("Len() = %d, want 3", dt.Len())
	}

	s := dt.Summary()
	if s.Inserts != 1 {
		t.Fatalf("Inserts = %d, want 1", s.Inserts)
	}
	if s.Deletes != 1 {
		t.Fatalf("Deletes = %d, want 1", s.Deletes)
	}
	if s.Updates != 1 {
		t.Fatalf("Updates = %d, want 1", s.Updates)
	}
}

func TestComputeTrieDiff_Identical(t *testing.T) {
	a := New()
	a.Put([]byte("x"), []byte("1"))
	a.Put([]byte("y"), []byte("2"))

	b := New()
	b.Put([]byte("x"), []byte("1"))
	b.Put([]byte("y"), []byte("2"))

	dt := ComputeTrieDiff(a, b)
	if dt.Len() != 0 {
		t.Fatalf("identical tries should have no diff, got %d", dt.Len())
	}
}

func TestComputeTrieDiffFromDB(t *testing.T) {
	// Build two tries, commit them to a shared DB, then diff via root hashes.
	trA := New()
	trA.Put([]byte("alpha"), []byte("1"))
	trA.Put([]byte("bravo"), []byte("2"))

	trB := New()
	trB.Put([]byte("alpha"), []byte("1"))
	trB.Put([]byte("bravo"), []byte("changed"))
	trB.Put([]byte("charlie"), []byte("3"))

	db := NewNodeDatabase(nil)
	rootA, err := CommitTrie(trA, db)
	if err != nil {
		t.Fatal(err)
	}
	rootB, err := CommitTrie(trB, db)
	if err != nil {
		t.Fatal(err)
	}

	dt, err := ComputeTrieDiffFromDB(rootA, rootB, db)
	if err != nil {
		t.Fatalf("ComputeTrieDiffFromDB error: %v", err)
	}

	s := dt.Summary()
	if s.Inserts != 1 {
		t.Fatalf("Inserts = %d, want 1 (charlie)", s.Inserts)
	}
	if s.Updates != 1 {
		t.Fatalf("Updates = %d, want 1 (bravo)", s.Updates)
	}
	if s.Deletes != 0 {
		t.Fatalf("Deletes = %d, want 0", s.Deletes)
	}
}

func TestDiffTracker_Concurrent(t *testing.T) {
	dt := NewDiffTracker()

	var wg sync.WaitGroup
	for g := 0; g < 4; g++ {
		wg.Add(1)
		go func(offset int) {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				key := []byte{byte(offset*50 + i)}
				dt.RecordInsert(key, []byte("v"))
			}
		}(g)
	}
	for g := 0; g < 4; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				dt.Len()
				dt.Summary()
				dt.Entries()
			}
		}()
	}
	wg.Wait()

	if dt.Len() != 200 {
		t.Fatalf("Len() = %d, want 200", dt.Len())
	}
}

func TestDiffTracker_UpdateOverwrite(t *testing.T) {
	dt := NewDiffTracker()
	dt.RecordUpdate([]byte("k"), []byte("v1"), []byte("v2"))
	dt.RecordUpdate([]byte("k"), []byte("v2"), []byte("v3"))

	entry := dt.Get([]byte("k"))
	if string(entry.OldValue) != "v1" {
		t.Fatalf("OldValue = %q, want v1", entry.OldValue)
	}
	if string(entry.NewValue) != "v3" {
		t.Fatalf("NewValue = %q, want v3", entry.NewValue)
	}
}
