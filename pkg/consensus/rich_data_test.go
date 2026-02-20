package consensus

import (
	"errors"
	"sync"
	"testing"
	"time"
)

// testSchema returns a simple schema for testing.
func testSchema() RichDataSchema {
	return RichDataSchema{
		Name:    "attestation_metadata",
		Version: 1,
		Fields: []FieldDefinition{
			{Name: "label", Type: FieldString, Required: true},
			{Name: "priority", Type: FieldInt, Required: true},
			{Name: "verified", Type: FieldBool, Required: false},
		},
		MaxSize: 1024,
	}
}

// testEntry returns a valid entry matching testSchema.
func testEntry(slot uint64) RichDataEntry {
	return RichDataEntry{
		SchemaName:  "attestation_metadata",
		ValidatorID: 42,
		Slot:        slot,
		Data: map[string]interface{}{
			"label":    "high-confidence",
			"priority": int64(1),
			"verified": true,
		},
		Timestamp: time.Now(),
	}
}

func TestNewRichDataRegistry(t *testing.T) {
	reg := NewRichDataRegistry()
	if reg == nil {
		t.Fatal("NewRichDataRegistry returned nil")
	}
	if reg.SchemaCount() != 0 {
		t.Errorf("initial schema count: want 0, got %d", reg.SchemaCount())
	}
	if reg.EntryCount() != 0 {
		t.Errorf("initial entry count: want 0, got %d", reg.EntryCount())
	}
}

func TestRegisterSchema(t *testing.T) {
	reg := NewRichDataRegistry()
	err := reg.RegisterSchema(testSchema())
	if err != nil {
		t.Fatalf("RegisterSchema: %v", err)
	}
	if reg.SchemaCount() != 1 {
		t.Errorf("schema count: want 1, got %d", reg.SchemaCount())
	}
}

func TestRegisterSchemaDuplicate(t *testing.T) {
	reg := NewRichDataRegistry()
	reg.RegisterSchema(testSchema())

	err := reg.RegisterSchema(testSchema())
	if err != ErrRichDataSchemaExists {
		t.Errorf("expected ErrRichDataSchemaExists, got %v", err)
	}
}

func TestGetSchema(t *testing.T) {
	reg := NewRichDataRegistry()
	reg.RegisterSchema(testSchema())

	s := reg.GetSchema("attestation_metadata")
	if s == nil {
		t.Fatal("GetSchema returned nil")
	}
	if s.Name != "attestation_metadata" {
		t.Errorf("schema name: want attestation_metadata, got %s", s.Name)
	}
	if s.Version != 1 {
		t.Errorf("schema version: want 1, got %d", s.Version)
	}
}

func TestGetSchemaNotFound(t *testing.T) {
	reg := NewRichDataRegistry()
	if reg.GetSchema("nonexistent") != nil {
		t.Error("expected nil for unknown schema")
	}
}

func TestSubmitEntry(t *testing.T) {
	reg := NewRichDataRegistry()
	reg.RegisterSchema(testSchema())

	err := reg.SubmitEntry(testEntry(100))
	if err != nil {
		t.Fatalf("SubmitEntry: %v", err)
	}
	if reg.EntryCount() != 1 {
		t.Errorf("entry count: want 1, got %d", reg.EntryCount())
	}
}

func TestSubmitEntrySchemaNotFound(t *testing.T) {
	reg := NewRichDataRegistry()

	entry := testEntry(100)
	entry.SchemaName = "nonexistent"
	err := reg.SubmitEntry(entry)
	if err != ErrRichDataSchemaNotFound {
		t.Errorf("expected ErrRichDataSchemaNotFound, got %v", err)
	}
}

func TestSubmitEntryMissingRequiredField(t *testing.T) {
	reg := NewRichDataRegistry()
	reg.RegisterSchema(testSchema())

	entry := testEntry(100)
	delete(entry.Data, "label") // required field

	err := reg.SubmitEntry(entry)
	if !errors.Is(err, ErrRichDataEntryInvalid) {
		t.Errorf("expected ErrRichDataEntryInvalid, got %v", err)
	}
}

func TestSubmitEntryWrongFieldType(t *testing.T) {
	reg := NewRichDataRegistry()
	reg.RegisterSchema(testSchema())

	entry := testEntry(100)
	entry.Data["priority"] = "not-an-int" // should be int

	err := reg.SubmitEntry(entry)
	if !errors.Is(err, ErrRichDataEntryInvalid) {
		t.Errorf("expected ErrRichDataEntryInvalid, got %v", err)
	}
}

func TestSubmitEntryTooLarge(t *testing.T) {
	schema := RichDataSchema{
		Name:    "tiny",
		Version: 1,
		Fields: []FieldDefinition{
			{Name: "payload", Type: FieldString, Required: true},
		},
		MaxSize: 10,
	}
	reg := NewRichDataRegistry()
	reg.RegisterSchema(schema)

	entry := RichDataEntry{
		SchemaName: "tiny",
		Slot:       1,
		Data: map[string]interface{}{
			"payload": "this string is way too long for the tiny schema",
		},
		Timestamp: time.Now(),
	}

	err := reg.SubmitEntry(entry)
	if err != ErrRichDataTooLarge {
		t.Errorf("expected ErrRichDataTooLarge, got %v", err)
	}
}

func TestGetEntries(t *testing.T) {
	reg := NewRichDataRegistry()
	reg.RegisterSchema(testSchema())

	reg.SubmitEntry(testEntry(100))
	reg.SubmitEntry(testEntry(100))
	reg.SubmitEntry(testEntry(200))

	entries := reg.GetEntries("attestation_metadata", 100)
	if len(entries) != 2 {
		t.Errorf("entries at slot 100: want 2, got %d", len(entries))
	}

	entries = reg.GetEntries("attestation_metadata", 200)
	if len(entries) != 1 {
		t.Errorf("entries at slot 200: want 1, got %d", len(entries))
	}
}

func TestGetEntriesEmpty(t *testing.T) {
	reg := NewRichDataRegistry()
	entries := reg.GetEntries("attestation_metadata", 999)
	if entries != nil {
		t.Errorf("expected nil for no entries, got %d", len(entries))
	}
}

func TestValidateEntry(t *testing.T) {
	reg := NewRichDataRegistry()
	reg.RegisterSchema(testSchema())

	err := reg.ValidateEntry(testEntry(100))
	if err != nil {
		t.Fatalf("ValidateEntry: %v", err)
	}
}

func TestValidateEntryOptionalFieldMissing(t *testing.T) {
	reg := NewRichDataRegistry()
	reg.RegisterSchema(testSchema())

	entry := testEntry(100)
	delete(entry.Data, "verified") // optional field

	err := reg.ValidateEntry(entry)
	if err != nil {
		t.Fatalf("optional field missing should not error: %v", err)
	}
}

func TestSchemaCount(t *testing.T) {
	reg := NewRichDataRegistry()
	reg.RegisterSchema(RichDataSchema{Name: "s1", Version: 1, MaxSize: 100})
	reg.RegisterSchema(RichDataSchema{Name: "s2", Version: 1, MaxSize: 100})

	if reg.SchemaCount() != 2 {
		t.Errorf("schema count: want 2, got %d", reg.SchemaCount())
	}
}

func TestEntryCount(t *testing.T) {
	reg := NewRichDataRegistry()
	schema := RichDataSchema{
		Name:    "simple",
		Version: 1,
		Fields:  nil,
		MaxSize: 0, // no size limit
	}
	reg.RegisterSchema(schema)

	for i := 0; i < 5; i++ {
		reg.SubmitEntry(RichDataEntry{
			SchemaName: "simple",
			Slot:       uint64(i),
			Data:       map[string]interface{}{},
			Timestamp:  time.Now(),
		})
	}

	if reg.EntryCount() != 5 {
		t.Errorf("entry count: want 5, got %d", reg.EntryCount())
	}
}

func TestPruneOldEntries(t *testing.T) {
	reg := NewRichDataRegistry()
	reg.RegisterSchema(testSchema())

	reg.SubmitEntry(testEntry(10))
	reg.SubmitEntry(testEntry(20))
	reg.SubmitEntry(testEntry(30))
	reg.SubmitEntry(testEntry(40))

	pruned := reg.PruneOldEntries(25)
	if pruned != 2 {
		t.Errorf("pruned: want 2, got %d", pruned)
	}
	if reg.EntryCount() != 2 {
		t.Errorf("remaining entries: want 2, got %d", reg.EntryCount())
	}

	// Entries at slot 30 and 40 should remain.
	if entries := reg.GetEntries("attestation_metadata", 30); len(entries) != 1 {
		t.Errorf("slot 30 entries: want 1, got %d", len(entries))
	}
	if entries := reg.GetEntries("attestation_metadata", 40); len(entries) != 1 {
		t.Errorf("slot 40 entries: want 1, got %d", len(entries))
	}
}

func TestPruneOldEntriesNone(t *testing.T) {
	reg := NewRichDataRegistry()
	reg.RegisterSchema(testSchema())

	reg.SubmitEntry(testEntry(100))

	pruned := reg.PruneOldEntries(50) // all entries are newer
	if pruned != 0 {
		t.Errorf("pruned: want 0, got %d", pruned)
	}
}

func TestFieldTypeString(t *testing.T) {
	tests := []struct {
		ft   FieldType
		want string
	}{
		{FieldString, "string"},
		{FieldInt, "int"},
		{FieldBool, "bool"},
		{FieldBytes, "bytes"},
		{FieldType(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.ft.String(); got != tt.want {
			t.Errorf("FieldType(%d).String(): want %q, got %q", tt.ft, tt.want, got)
		}
	}
}

func TestValidateEntryBytesField(t *testing.T) {
	schema := RichDataSchema{
		Name:    "binary_schema",
		Version: 1,
		Fields: []FieldDefinition{
			{Name: "payload", Type: FieldBytes, Required: true},
		},
		MaxSize: 1024,
	}
	reg := NewRichDataRegistry()
	reg.RegisterSchema(schema)

	// Valid bytes entry.
	entry := RichDataEntry{
		SchemaName: "binary_schema",
		Slot:       1,
		Data:       map[string]interface{}{"payload": []byte{0x01, 0x02}},
		Timestamp:  time.Now(),
	}
	if err := reg.ValidateEntry(entry); err != nil {
		t.Fatalf("valid bytes entry should pass: %v", err)
	}

	// Wrong type for bytes field.
	entry.Data["payload"] = "not-bytes"
	err := reg.ValidateEntry(entry)
	if !errors.Is(err, ErrRichDataEntryInvalid) {
		t.Errorf("expected ErrRichDataEntryInvalid for wrong bytes type, got %v", err)
	}
}

func TestConcurrentSubmitAndRead(t *testing.T) {
	reg := NewRichDataRegistry()
	reg.RegisterSchema(testSchema())

	var wg sync.WaitGroup

	// Concurrent writes.
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			reg.SubmitEntry(testEntry(uint64(i % 10)))
		}(i)
	}

	// Concurrent reads.
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			reg.GetEntries("attestation_metadata", uint64(i%10))
		}(i)
	}

	wg.Wait()

	if reg.EntryCount() != 50 {
		t.Errorf("entry count after concurrent writes: want 50, got %d", reg.EntryCount())
	}
}

func TestConcurrentRegisterAndValidate(t *testing.T) {
	reg := NewRichDataRegistry()
	reg.RegisterSchema(testSchema())

	var wg sync.WaitGroup

	// Concurrent schema registrations (some will fail with duplicate).
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			reg.RegisterSchema(RichDataSchema{
				Name:    "attestation_metadata", // duplicate on purpose
				Version: uint64(i),
				MaxSize: 100,
			})
		}(i)
	}

	// Concurrent validations.
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			reg.ValidateEntry(testEntry(1))
		}()
	}

	wg.Wait()

	// Schema count should still be 1 (all duplicates rejected).
	if reg.SchemaCount() != 1 {
		t.Errorf("schema count: want 1, got %d", reg.SchemaCount())
	}
}

func TestMultipleSchemas(t *testing.T) {
	reg := NewRichDataRegistry()

	s1 := RichDataSchema{
		Name:    "schema_a",
		Version: 1,
		Fields:  []FieldDefinition{{Name: "x", Type: FieldString, Required: true}},
		MaxSize: 256,
	}
	s2 := RichDataSchema{
		Name:    "schema_b",
		Version: 1,
		Fields:  []FieldDefinition{{Name: "y", Type: FieldInt, Required: true}},
		MaxSize: 256,
	}

	reg.RegisterSchema(s1)
	reg.RegisterSchema(s2)

	reg.SubmitEntry(RichDataEntry{
		SchemaName: "schema_a",
		Slot:       1,
		Data:       map[string]interface{}{"x": "hello"},
		Timestamp:  time.Now(),
	})
	reg.SubmitEntry(RichDataEntry{
		SchemaName: "schema_b",
		Slot:       1,
		Data:       map[string]interface{}{"y": int64(42)},
		Timestamp:  time.Now(),
	})

	if reg.SchemaCount() != 2 {
		t.Errorf("schema count: want 2, got %d", reg.SchemaCount())
	}
	if reg.EntryCount() != 2 {
		t.Errorf("entry count: want 2, got %d", reg.EntryCount())
	}

	aEntries := reg.GetEntries("schema_a", 1)
	if len(aEntries) != 1 {
		t.Errorf("schema_a entries at slot 1: want 1, got %d", len(aEntries))
	}
	bEntries := reg.GetEntries("schema_b", 1)
	if len(bEntries) != 1 {
		t.Errorf("schema_b entries at slot 1: want 1, got %d", len(bEntries))
	}
}
