package types

import (
	"sync"
	"testing"
)

func TestNewTxTypeRegistry(t *testing.T) {
	r := NewTxTypeRegistry()

	// Should have 5 pre-registered types.
	if got := r.Count(); got != 5 {
		t.Fatalf("Count() = %d, want 5", got)
	}

	// Verify each default type is present.
	expected := []struct {
		id   uint8
		name string
	}{
		{LegacyTxType, "Legacy"},
		{AccessListTxType, "AccessList"},
		{DynamicFeeTxType, "DynamicFee"},
		{BlobTxType, "Blob"},
		{SetCodeTxType, "SetCode"},
	}
	for _, e := range expected {
		info, err := r.Lookup(e.id)
		if err != nil {
			t.Fatalf("Lookup(0x%02x) error: %v", e.id, err)
		}
		if info.Name != e.name {
			t.Errorf("Lookup(0x%02x).Name = %q, want %q", e.id, info.Name, e.name)
		}
	}
}

func TestRegister(t *testing.T) {
	r := NewTxTypeRegistry()

	custom := TxTypeInfo{
		TypeID:             0x10,
		Name:               "CustomTx",
		SupportsAccessList: true,
		SupportsBlobs:      false,
		MaxPayloadSize:     65536,
		MinGas:             30000,
	}

	if err := r.Register(custom); err != nil {
		t.Fatalf("Register() error: %v", err)
	}
	if got := r.Count(); got != 6 {
		t.Fatalf("Count() = %d, want 6", got)
	}

	info, err := r.Lookup(0x10)
	if err != nil {
		t.Fatalf("Lookup(0x10) error: %v", err)
	}
	if info.Name != "CustomTx" {
		t.Errorf("Name = %q, want %q", info.Name, "CustomTx")
	}
	if info.MaxPayloadSize != 65536 {
		t.Errorf("MaxPayloadSize = %d, want 65536", info.MaxPayloadSize)
	}
	if info.MinGas != 30000 {
		t.Errorf("MinGas = %d, want 30000", info.MinGas)
	}
}

func TestRegisterDuplicate(t *testing.T) {
	r := NewTxTypeRegistry()

	dup := TxTypeInfo{
		TypeID: LegacyTxType,
		Name:   "LegacyDuplicate",
	}
	err := r.Register(dup)
	if err == nil {
		t.Fatal("Register() should return error for duplicate TypeID")
	}
}

func TestLookupUnknown(t *testing.T) {
	r := NewTxTypeRegistry()

	_, err := r.Lookup(0xFF)
	if err == nil {
		t.Fatal("Lookup(0xFF) should return error for unknown type")
	}
}

func TestIsSupported(t *testing.T) {
	r := NewTxTypeRegistry()

	if !r.IsSupported(LegacyTxType) {
		t.Error("IsSupported(Legacy) = false, want true")
	}
	if !r.IsSupported(BlobTxType) {
		t.Error("IsSupported(Blob) = false, want true")
	}
	if r.IsSupported(0xFF) {
		t.Error("IsSupported(0xFF) = true, want false")
	}
}

func TestValidateType(t *testing.T) {
	r := NewTxTypeRegistry()

	tests := []struct {
		name          string
		typeID        uint8
		hasAccessList bool
		hasBlobs      bool
		wantErr       bool
	}{
		{"legacy no extras", LegacyTxType, false, false, false},
		{"legacy with access list", LegacyTxType, true, false, true},
		{"legacy with blobs", LegacyTxType, false, true, true},
		{"access list with access list", AccessListTxType, true, false, false},
		{"access list with blobs", AccessListTxType, false, true, true},
		{"dynamic fee with access list", DynamicFeeTxType, true, false, false},
		{"blob with both", BlobTxType, true, true, false},
		{"blob without blobs", BlobTxType, false, false, false},
		{"setcode with access list", SetCodeTxType, true, false, false},
		{"setcode with blobs", SetCodeTxType, false, true, true},
		{"unknown type", 0xFF, false, false, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := r.ValidateType(tt.typeID, tt.hasAccessList, tt.hasBlobs)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateType() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestAllTypes(t *testing.T) {
	r := NewTxTypeRegistry()

	all := r.AllTypes()
	if len(all) != 5 {
		t.Fatalf("AllTypes() len = %d, want 5", len(all))
	}

	// Verify sorted order by TypeID.
	for i := 1; i < len(all); i++ {
		if all[i].TypeID <= all[i-1].TypeID {
			t.Errorf("AllTypes() not sorted: [%d].TypeID=%d <= [%d].TypeID=%d",
				i, all[i].TypeID, i-1, all[i-1].TypeID)
		}
	}

	// First should be Legacy (0x00).
	if all[0].TypeID != LegacyTxType {
		t.Errorf("AllTypes()[0].TypeID = 0x%02x, want 0x%02x", all[0].TypeID, LegacyTxType)
	}
}

func TestBlobTypes(t *testing.T) {
	r := NewTxTypeRegistry()

	blobTypes := r.BlobTypes()
	if len(blobTypes) != 1 {
		t.Fatalf("BlobTypes() len = %d, want 1", len(blobTypes))
	}
	if blobTypes[0].TypeID != BlobTxType {
		t.Errorf("BlobTypes()[0].TypeID = 0x%02x, want 0x%02x", blobTypes[0].TypeID, BlobTxType)
	}

	// Register a second blob-supporting type and verify it appears.
	err := r.Register(TxTypeInfo{
		TypeID:        0x20,
		Name:          "BlobV2",
		SupportsBlobs: true,
	})
	if err != nil {
		t.Fatalf("Register() error: %v", err)
	}
	blobTypes = r.BlobTypes()
	if len(blobTypes) != 2 {
		t.Fatalf("BlobTypes() len = %d, want 2", len(blobTypes))
	}
}

func TestRegistryFieldValues(t *testing.T) {
	r := NewTxTypeRegistry()

	// Legacy should not support access lists or blobs.
	info, _ := r.Lookup(LegacyTxType)
	if info.SupportsAccessList {
		t.Error("Legacy.SupportsAccessList = true, want false")
	}
	if info.SupportsBlobs {
		t.Error("Legacy.SupportsBlobs = true, want false")
	}
	if info.MinGas != 21000 {
		t.Errorf("Legacy.MinGas = %d, want 21000", info.MinGas)
	}

	// Blob should support both.
	info, _ = r.Lookup(BlobTxType)
	if !info.SupportsAccessList {
		t.Error("Blob.SupportsAccessList = false, want true")
	}
	if !info.SupportsBlobs {
		t.Error("Blob.SupportsBlobs = false, want true")
	}
}

func TestRegistryConcurrentAccess(t *testing.T) {
	r := NewTxTypeRegistry()

	var wg sync.WaitGroup
	// Concurrent reads.
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r.IsSupported(LegacyTxType)
			r.Lookup(BlobTxType)
			r.AllTypes()
			r.BlobTypes()
			r.Count()
		}()
	}
	// Concurrent writes.
	for i := 0; i < 50; i++ {
		wg.Add(1)
		id := uint8(0x80 + i) // unique IDs to avoid duplicate errors
		go func(typeID uint8) {
			defer wg.Done()
			r.Register(TxTypeInfo{
				TypeID: typeID,
				Name:   "Concurrent",
			})
		}(id)
	}
	wg.Wait()

	// All 50 concurrent registrations should succeed (unique IDs).
	if got := r.Count(); got != 55 { // 5 defaults + 50 new
		t.Errorf("Count() = %d, want 55", got)
	}
}

func TestValidateMultipleErrors(t *testing.T) {
	r := NewTxTypeRegistry()

	// Legacy with both access list and blobs should produce a combined error.
	err := r.ValidateType(LegacyTxType, true, true)
	if err == nil {
		t.Fatal("expected error for legacy with access list and blobs")
	}
}

func TestLookupReturnsCopy(t *testing.T) {
	r := NewTxTypeRegistry()

	info1, _ := r.Lookup(LegacyTxType)
	info1.Name = "Modified"

	// Original should remain unchanged.
	info2, _ := r.Lookup(LegacyTxType)
	if info2.Name != "Legacy" {
		t.Errorf("Lookup returned mutable reference: Name = %q, want %q", info2.Name, "Legacy")
	}
}
