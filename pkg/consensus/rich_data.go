package consensus

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

// Errors for the rich data registry.
var (
	ErrRichDataSchemaExists   = errors.New("rich data: schema already registered")
	ErrRichDataSchemaNotFound = errors.New("rich data: schema not found")
	ErrRichDataEntryInvalid   = errors.New("rich data: entry validation failed")
	ErrRichDataTooLarge       = errors.New("rich data: entry exceeds schema max size")
)

// FieldType enumerates the allowed types for schema fields.
type FieldType int

const (
	FieldString  FieldType = iota // string value
	FieldInt                      // integer value
	FieldBool                     // boolean value
	FieldBytes                    // raw byte data
)

// String returns a human-readable field type name.
func (ft FieldType) String() string {
	switch ft {
	case FieldString:
		return "string"
	case FieldInt:
		return "int"
	case FieldBool:
		return "bool"
	case FieldBytes:
		return "bytes"
	default:
		return "unknown"
	}
}

// FieldDefinition describes a single field in a RichDataSchema.
type FieldDefinition struct {
	Name     string    // field name
	Type     FieldType // expected type
	Required bool      // whether the field must be present
}

// RichDataSchema defines the structure of rich data entries that validators
// can attach to attestations and blocks.
type RichDataSchema struct {
	Name    string            // unique schema name
	Version uint64            // schema version
	Fields  []FieldDefinition // field definitions
	MaxSize int               // maximum total data size in bytes
}

// RichDataEntry is a single rich data submission from a validator.
type RichDataEntry struct {
	SchemaName  string                 // which schema this entry conforms to
	ValidatorID uint64                 // submitting validator index
	Slot        uint64                 // slot this entry is associated with
	Data        map[string]interface{} // field name -> value
	Timestamp   time.Time              // when the entry was created
}

// entryKey is used to index entries by schema name and slot.
type entryKey struct {
	schema string
	slot   uint64
}

// RichDataRegistry manages schema registration and entry storage for
// validators attaching structured metadata to consensus objects.
type RichDataRegistry struct {
	mu      sync.RWMutex
	schemas map[string]*RichDataSchema       // name -> schema
	entries map[entryKey][]*RichDataEntry     // (schema, slot) -> entries
}

// NewRichDataRegistry creates a new empty registry.
func NewRichDataRegistry() *RichDataRegistry {
	return &RichDataRegistry{
		schemas: make(map[string]*RichDataSchema),
		entries: make(map[entryKey][]*RichDataEntry),
	}
}

// RegisterSchema registers a new schema. Returns an error if a schema
// with the same name already exists.
func (r *RichDataRegistry) RegisterSchema(schema RichDataSchema) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.schemas[schema.Name]; exists {
		return ErrRichDataSchemaExists
	}
	s := schema // copy to avoid external mutation
	r.schemas[schema.Name] = &s
	return nil
}

// GetSchema returns the schema with the given name, or nil if not found.
func (r *RichDataRegistry) GetSchema(name string) *RichDataSchema {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.schemas[name]
}

// SubmitEntry validates an entry against its schema and stores it.
func (r *RichDataRegistry) SubmitEntry(entry RichDataEntry) error {
	if err := r.ValidateEntry(entry); err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	key := entryKey{schema: entry.SchemaName, slot: entry.Slot}
	e := entry // copy
	r.entries[key] = append(r.entries[key], &e)
	return nil
}

// GetEntries returns all entries for the given schema name and slot.
func (r *RichDataRegistry) GetEntries(schemaName string, slot uint64) []RichDataEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()

	key := entryKey{schema: schemaName, slot: slot}
	stored := r.entries[key]
	if len(stored) == 0 {
		return nil
	}

	result := make([]RichDataEntry, len(stored))
	for i, e := range stored {
		result[i] = *e
	}
	return result
}

// ValidateEntry checks whether an entry conforms to its declared schema.
func (r *RichDataRegistry) ValidateEntry(entry RichDataEntry) error {
	r.mu.RLock()
	defer r.mu.RUnlock()

	schema, ok := r.schemas[entry.SchemaName]
	if !ok {
		return ErrRichDataSchemaNotFound
	}

	// Check total data size.
	if schema.MaxSize > 0 {
		size := estimateDataSize(entry.Data)
		if size > schema.MaxSize {
			return ErrRichDataTooLarge
		}
	}

	// Validate required fields and types.
	for _, field := range schema.Fields {
		val, present := entry.Data[field.Name]
		if !present {
			if field.Required {
				return fmt.Errorf("%w: missing required field %q", ErrRichDataEntryInvalid, field.Name)
			}
			continue
		}
		if err := validateFieldType(field, val); err != nil {
			return err
		}
	}

	return nil
}

// SchemaCount returns the number of registered schemas.
func (r *RichDataRegistry) SchemaCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.schemas)
}

// EntryCount returns the total number of stored entries across all schemas.
func (r *RichDataRegistry) EntryCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()

	count := 0
	for _, entries := range r.entries {
		count += len(entries)
	}
	return count
}

// PruneOldEntries removes all entries with slot strictly less than beforeSlot.
// Returns the number of entries removed.
func (r *RichDataRegistry) PruneOldEntries(beforeSlot uint64) int {
	r.mu.Lock()
	defer r.mu.Unlock()

	pruned := 0
	for key, entries := range r.entries {
		if key.slot < beforeSlot {
			pruned += len(entries)
			delete(r.entries, key)
		}
	}
	return pruned
}

// estimateDataSize estimates the byte size of a data map for size validation.
func estimateDataSize(data map[string]interface{}) int {
	size := 0
	for k, v := range data {
		size += len(k)
		switch val := v.(type) {
		case string:
			size += len(val)
		case []byte:
			size += len(val)
		case int, int64, uint64:
			size += 8
		case bool:
			size += 1
		default:
			size += 32 // conservative estimate for unknown types
		}
	}
	return size
}

// validateFieldType checks that a value matches the expected field type.
func validateFieldType(field FieldDefinition, val interface{}) error {
	switch field.Type {
	case FieldString:
		if _, ok := val.(string); !ok {
			return fmt.Errorf("%w: field %q expects string, got %T",
				ErrRichDataEntryInvalid, field.Name, val)
		}
	case FieldInt:
		switch val.(type) {
		case int, int64, uint64:
			// ok
		default:
			return fmt.Errorf("%w: field %q expects int, got %T",
				ErrRichDataEntryInvalid, field.Name, val)
		}
	case FieldBool:
		if _, ok := val.(bool); !ok {
			return fmt.Errorf("%w: field %q expects bool, got %T",
				ErrRichDataEntryInvalid, field.Name, val)
		}
	case FieldBytes:
		if _, ok := val.([]byte); !ok {
			return fmt.Errorf("%w: field %q expects bytes, got %T",
				ErrRichDataEntryInvalid, field.Name, val)
		}
	}
	return nil
}
