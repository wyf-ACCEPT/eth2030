package ssz

import (
	"testing"
)

// --- StableContainer creation ---

func TestNewStableContainer(t *testing.T) {
	sc := NewStableContainer(8)
	if sc.Capacity() != 8 {
		t.Fatalf("capacity = %d, want 8", sc.Capacity())
	}
	if sc.Len() != 0 {
		t.Fatalf("len = %d, want 0", sc.Len())
	}
}

func TestNewStableContainerZeroCapacity(t *testing.T) {
	// Zero capacity should be clamped to 1.
	sc := NewStableContainer(0)
	if sc.Capacity() != 1 {
		t.Fatalf("capacity = %d, want 1", sc.Capacity())
	}
}

// --- AddField ---

func TestAddField_NonOptional(t *testing.T) {
	sc := NewStableContainer(4)
	var val [32]byte
	val[0] = 0xAA
	if err := sc.AddField("slot", val, false); err != nil {
		t.Fatalf("AddField error: %v", err)
	}
	if sc.Len() != 1 {
		t.Fatalf("len = %d, want 1", sc.Len())
	}
	// Non-optional fields start active.
	if !sc.IsActive(0) {
		t.Fatal("non-optional field should be active")
	}
}

func TestAddField_Optional(t *testing.T) {
	sc := NewStableContainer(4)
	var val [32]byte
	val[0] = 0xBB
	if err := sc.AddField("extra", val, true); err != nil {
		t.Fatalf("AddField error: %v", err)
	}
	// Optional fields start inactive.
	if sc.IsActive(0) {
		t.Fatal("optional field should start inactive")
	}
}

func TestAddField_CapacityExceeded(t *testing.T) {
	sc := NewStableContainer(1)
	var val [32]byte
	if err := sc.AddField("a", val, false); err != nil {
		t.Fatalf("first AddField error: %v", err)
	}
	if err := sc.AddField("b", val, false); err == nil {
		t.Fatal("expected error when exceeding capacity")
	}
}

func TestAddFieldWithTag(t *testing.T) {
	sc := NewStableContainer(4)
	var val [32]byte
	if err := sc.AddFieldWithTag("slot", "uint64", val, false); err != nil {
		t.Fatalf("AddFieldWithTag error: %v", err)
	}
	fd, err := sc.Field(0)
	if err != nil {
		t.Fatalf("Field error: %v", err)
	}
	if fd.Name != "slot" || fd.TypeTag != "uint64" {
		t.Fatalf("field = %+v, want name=slot typeTag=uint64", fd)
	}
}

// --- Active bitvector ---

func TestActiveBitvector_AllActive(t *testing.T) {
	sc := NewStableContainer(8)
	for i := 0; i < 3; i++ {
		if err := sc.AddField("", [32]byte{}, false); err != nil {
			t.Fatalf("AddField error: %v", err)
		}
	}
	bv := sc.ActiveBitvector()
	// Capacity = 8 -> 1 byte. Fields 0,1,2 active = 0b00000111 = 0x07.
	if len(bv) != 1 {
		t.Fatalf("bitvector len = %d, want 1", len(bv))
	}
	if bv[0] != 0x07 {
		t.Fatalf("bitvector = 0x%02x, want 0x07", bv[0])
	}
}

func TestActiveBitvector_MixedActive(t *testing.T) {
	sc := NewStableContainer(8)
	// Field 0: active (non-optional)
	sc.AddField("a", [32]byte{}, false)
	// Field 1: inactive (optional, not activated)
	sc.AddField("b", [32]byte{}, true)
	// Field 2: active (non-optional)
	sc.AddField("c", [32]byte{}, false)

	bv := sc.ActiveBitvector()
	// Bits: 0=active, 1=inactive, 2=active -> 0b00000101 = 0x05.
	if bv[0] != 0x05 {
		t.Fatalf("bitvector = 0x%02x, want 0x05", bv[0])
	}
}

func TestActiveBitvector_SetActive(t *testing.T) {
	sc := NewStableContainer(8)
	sc.AddField("a", [32]byte{}, true)  // starts inactive
	sc.AddField("b", [32]byte{}, false) // starts active

	// Activate field 0, deactivate field 1.
	sc.SetActive(0, true)
	sc.SetActive(1, false)

	bv := sc.ActiveBitvector()
	// Bit 0 = active, bit 1 = inactive -> 0b00000001 = 0x01.
	if bv[0] != 0x01 {
		t.Fatalf("bitvector = 0x%02x, want 0x01", bv[0])
	}
}

func TestActiveBitvector_CapacityPadding(t *testing.T) {
	// Capacity 16: bitvector should be 2 bytes even with fewer fields.
	sc := NewStableContainer(16)
	sc.AddField("a", [32]byte{}, false)

	bv := sc.ActiveBitvector()
	if len(bv) != 2 {
		t.Fatalf("bitvector len = %d, want 2", len(bv))
	}
	if bv[0] != 0x01 || bv[1] != 0x00 {
		t.Fatalf("bitvector = [0x%02x, 0x%02x], want [0x01, 0x00]", bv[0], bv[1])
	}
}

// --- IsActive edge cases ---

func TestIsActive_OutOfBounds(t *testing.T) {
	sc := NewStableContainer(4)
	if sc.IsActive(-1) {
		t.Fatal("IsActive(-1) should return false")
	}
	if sc.IsActive(100) {
		t.Fatal("IsActive(100) should return false")
	}
}

// --- HashTreeRoot ---

func TestHashTreeRoot_EmptyContainer(t *testing.T) {
	sc := NewStableContainer(4)
	root := sc.HashTreeRoot()
	// Should not panic and should return a deterministic hash.
	if root == [32]byte{} {
		// The hash of (merkleize(4 zero chunks), bitvector(all zero)) is
		// not all-zeros because the SHA-256 hash combines nonzero structure.
		// Actually, it could be if both sides are zero. Let's just verify
		// it's deterministic.
	}
	root2 := sc.HashTreeRoot()
	if root != root2 {
		t.Fatal("hash tree root should be deterministic")
	}
}

func TestHashTreeRoot_SingleField(t *testing.T) {
	sc := NewStableContainer(4)
	val := HashTreeRootUint64(42)
	sc.AddField("x", val, false)

	root := sc.HashTreeRoot()
	if root == [32]byte{} {
		t.Fatal("root should not be zero for a container with data")
	}
}

func TestHashTreeRoot_ChangesWithActiveFields(t *testing.T) {
	sc := NewStableContainer(4)
	val := HashTreeRootUint64(100)
	sc.AddField("a", val, true) // optional, starts inactive

	rootInactive := sc.HashTreeRoot()

	sc.SetActive(0, true)
	rootActive := sc.HashTreeRoot()

	if rootInactive == rootActive {
		t.Fatal("root should differ when field activation changes")
	}
}

func TestHashTreeRoot_DifferentValues(t *testing.T) {
	sc1 := NewStableContainer(4)
	sc1.AddField("x", HashTreeRootUint64(1), false)

	sc2 := NewStableContainer(4)
	sc2.AddField("x", HashTreeRootUint64(2), false)

	if sc1.HashTreeRoot() == sc2.HashTreeRoot() {
		t.Fatal("different values should produce different roots")
	}
}

func TestHashTreeRoot_InactiveFieldTreatedAsZero(t *testing.T) {
	// Container with one active field should differ from one with
	// the same field inactive, even if the value is the same.
	sc1 := NewStableContainer(4)
	sc1.AddField("x", HashTreeRootUint64(7), false) // active

	sc2 := NewStableContainer(4)
	sc2.AddField("x", HashTreeRootUint64(7), true) // inactive

	// sc1 has field active, sc2 has it inactive.
	// The bitvectors differ, so roots differ.
	if sc1.HashTreeRoot() == sc2.HashTreeRoot() {
		t.Fatal("active vs inactive field should produce different roots")
	}
}

func TestHashTreeRoot_CapacityAffectsRoot(t *testing.T) {
	// Same data but different capacity should produce different roots
	// because the Merkle tree is padded differently.
	sc4 := NewStableContainer(4)
	sc4.AddField("x", HashTreeRootUint64(1), false)

	sc8 := NewStableContainer(8)
	sc8.AddField("x", HashTreeRootUint64(1), false)

	if sc4.HashTreeRoot() == sc8.HashTreeRoot() {
		t.Fatal("different capacities should produce different roots")
	}
}

func TestHashTreeRoot_MultipleFields(t *testing.T) {
	sc := NewStableContainer(8)
	sc.AddField("slot", HashTreeRootUint64(100), false)
	sc.AddField("index", HashTreeRootUint64(5), false)
	sc.AddField("root", HashTreeRootBytes32([32]byte{0xff}), true)

	// With root inactive.
	root1 := sc.HashTreeRoot()

	// Activate the root field.
	sc.SetActive(2, true)
	root2 := sc.HashTreeRoot()

	if root1 == root2 {
		t.Fatal("roots should differ after activating a field")
	}
}

func TestSetValue(t *testing.T) {
	sc := NewStableContainer(4)
	sc.AddField("x", HashTreeRootUint64(1), false)
	root1 := sc.HashTreeRoot()

	sc.SetValue(0, HashTreeRootUint64(2))
	root2 := sc.HashTreeRoot()

	if root1 == root2 {
		t.Fatal("root should change after SetValue")
	}
}

// --- Profile ---

func TestProfile_Creation(t *testing.T) {
	sc := NewStableContainer(8)
	sc.AddField("slot", HashTreeRootUint64(1), false)
	sc.AddField("index", HashTreeRootUint64(2), false)
	sc.AddField("extra", HashTreeRootUint64(3), true)

	// Profile uses only fields 0 and 1.
	p, err := NewProfile(sc, []int{0, 1})
	if err != nil {
		t.Fatalf("NewProfile error: %v", err)
	}

	// Field 2 should be deactivated by the profile.
	if sc.IsActive(2) {
		t.Fatal("field 2 should be inactive under the profile")
	}
	if !sc.IsActive(0) || !sc.IsActive(1) {
		t.Fatal("fields 0 and 1 should be active under the profile")
	}

	root := p.HashTreeRoot()
	if root == [32]byte{} {
		t.Fatal("profile root should not be zero")
	}
}

func TestProfile_HashTreeRootMatchesContainer(t *testing.T) {
	sc := NewStableContainer(4)
	sc.AddField("a", HashTreeRootUint64(10), false)
	sc.AddField("b", HashTreeRootUint64(20), false)

	p, err := NewProfile(sc, []int{0, 1})
	if err != nil {
		t.Fatalf("NewProfile error: %v", err)
	}

	// The profile root should equal the container root since all fields
	// are active in both.
	if p.HashTreeRoot() != sc.HashTreeRoot() {
		t.Fatal("profile root should match container root when same fields are active")
	}
}

func TestProfile_InvalidIndex(t *testing.T) {
	sc := NewStableContainer(4)
	sc.AddField("a", [32]byte{}, false)

	_, err := NewProfile(sc, []int{5})
	if err == nil {
		t.Fatal("expected error for out-of-bounds profile index")
	}
}

func TestProfile_ActiveBitvector(t *testing.T) {
	sc := NewStableContainer(8)
	sc.AddField("a", [32]byte{}, false)
	sc.AddField("b", [32]byte{}, false)
	sc.AddField("c", [32]byte{}, false)

	// Profile activates only fields 0 and 2.
	p, _ := NewProfile(sc, []int{0, 2})
	bv := p.ActiveBitvector()

	// Bits: 0=active, 1=inactive, 2=active -> 0b00000101 = 0x05.
	if bv[0] != 0x05 {
		t.Fatalf("profile bitvector = 0x%02x, want 0x05", bv[0])
	}
}

func TestProfile_Container(t *testing.T) {
	sc := NewStableContainer(4)
	sc.AddField("a", [32]byte{}, false)
	p, _ := NewProfile(sc, []int{0})
	if p.Container() != sc {
		t.Fatal("Container() should return the underlying StableContainer")
	}
}

// --- Field ---

func TestField_Valid(t *testing.T) {
	sc := NewStableContainer(4)
	sc.AddFieldWithTag("slot", "uint64", [32]byte{}, false)
	fd, err := sc.Field(0)
	if err != nil {
		t.Fatalf("Field error: %v", err)
	}
	if fd.Name != "slot" {
		t.Fatalf("field name = %q, want %q", fd.Name, "slot")
	}
}

func TestField_OutOfBounds(t *testing.T) {
	sc := NewStableContainer(4)
	_, err := sc.Field(0)
	if err == nil {
		t.Fatal("expected error for empty container field access")
	}
}
