// rich_data.go implements the Rich Data Smart Contracts system from the
// Ethereum 2028 roadmap (CL Accessibility track). It provides structured,
// schema-validated on-chain data storage with field-level indexing for
// efficient queries.
package core

import (
	"bytes"
	"errors"
	"sync"

	"github.com/eth2030/eth2030/core/types"
)

// DataType enumerates the field types supported by rich data schemas.
type DataType uint8

const (
	TypeUint256 DataType = iota
	TypeAddress
	TypeBytes32
	TypeString
	TypeBool
	TypeArray
)

// String returns a human-readable label for the DataType.
func (dt DataType) String() string {
	switch dt {
	case TypeUint256:
		return "uint256"
	case TypeAddress:
		return "address"
	case TypeBytes32:
		return "bytes32"
	case TypeString:
		return "string"
	case TypeBool:
		return "bool"
	case TypeArray:
		return "array"
	default:
		return "unknown"
	}
}

var (
	ErrSchemaExists       = errors.New("richdata: schema already registered")
	ErrSchemaNotFound     = errors.New("richdata: schema not found")
	ErrDataNotFound       = errors.New("richdata: data not found for key")
	ErrFieldNotInSchema   = errors.New("richdata: field not defined in schema")
	ErrMissingRequired    = errors.New("richdata: missing required field")
	ErrFieldTooLarge      = errors.New("richdata: field value exceeds max size")
	ErrEmptySchema        = errors.New("richdata: schema must have at least one field")
	ErrDuplicateFieldName = errors.New("richdata: duplicate field name in schema")
	ErrDataExists         = errors.New("richdata: data already exists for key")
)

// SchemaField describes a single field within a rich data schema.
type SchemaField struct {
	Name      string
	FieldType DataType
	Required  bool
	MaxSize   uint64
}

// RichDataIndex maintains per-field inverted indices for a single schema,
// mapping (fieldName, value) -> set of data keys.
type RichDataIndex struct {
	// fieldName -> value(hex) -> set of keys
	fields map[string]map[string]map[types.Hash]struct{}
}

func newRichDataIndex() *RichDataIndex {
	return &RichDataIndex{
		fields: make(map[string]map[string]map[types.Hash]struct{}),
	}
}

// add indexes a key under fieldName/value.
func (idx *RichDataIndex) add(fieldName string, value []byte, key types.Hash) {
	valMap, ok := idx.fields[fieldName]
	if !ok {
		valMap = make(map[string]map[types.Hash]struct{})
		idx.fields[fieldName] = valMap
	}
	valKey := string(value)
	if valMap[valKey] == nil {
		valMap[valKey] = make(map[types.Hash]struct{})
	}
	valMap[valKey][key] = struct{}{}
}

// remove removes a key from a fieldName/value index entry.
func (idx *RichDataIndex) remove(fieldName string, value []byte, key types.Hash) {
	valMap, ok := idx.fields[fieldName]
	if !ok {
		return
	}
	valKey := string(value)
	keys := valMap[valKey]
	if keys == nil {
		return
	}
	delete(keys, key)
	if len(keys) == 0 {
		delete(valMap, valKey)
	}
}

// query returns all keys where fieldName equals value.
func (idx *RichDataIndex) query(fieldName string, value []byte) []types.Hash {
	valMap, ok := idx.fields[fieldName]
	if !ok {
		return nil
	}
	keys := valMap[string(value)]
	if len(keys) == 0 {
		return nil
	}
	result := make([]types.Hash, 0, len(keys))
	for k := range keys {
		result = append(result, k)
	}
	return result
}

// RichDataStore manages schema-validated structured data with indexing.
// It is safe for concurrent use.
type RichDataStore struct {
	mu sync.RWMutex

	// schemaID -> ordered fields
	schemas map[types.Hash][]SchemaField
	// schemaID -> (key -> field values)
	data map[types.Hash]map[types.Hash]map[string][]byte
	// schemaID -> index
	indices map[types.Hash]*RichDataIndex
	// ordered list of schema IDs (for ListSchemas)
	schemaOrder []types.Hash
}

// NewRichDataStore creates an empty RichDataStore.
func NewRichDataStore() *RichDataStore {
	return &RichDataStore{
		schemas: make(map[types.Hash][]SchemaField),
		data:    make(map[types.Hash]map[types.Hash]map[string][]byte),
		indices: make(map[types.Hash]*RichDataIndex),
	}
}

// RegisterSchema registers a new data schema under schemaID.
// Fields must be non-empty and have unique names.
func (s *RichDataStore) RegisterSchema(schemaID types.Hash, fields []SchemaField) error {
	if len(fields) == 0 {
		return ErrEmptySchema
	}

	// Check for duplicate field names.
	seen := make(map[string]struct{}, len(fields))
	for _, f := range fields {
		if _, dup := seen[f.Name]; dup {
			return ErrDuplicateFieldName
		}
		seen[f.Name] = struct{}{}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.schemas[schemaID]; exists {
		return ErrSchemaExists
	}

	cp := make([]SchemaField, len(fields))
	copy(cp, fields)
	s.schemas[schemaID] = cp
	s.data[schemaID] = make(map[types.Hash]map[string][]byte)
	s.indices[schemaID] = newRichDataIndex()
	s.schemaOrder = append(s.schemaOrder, schemaID)
	return nil
}

// GetSchema returns the fields of a registered schema.
func (s *RichDataStore) GetSchema(schemaID types.Hash) ([]SchemaField, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	fields, ok := s.schemas[schemaID]
	if !ok {
		return nil, ErrSchemaNotFound
	}
	cp := make([]SchemaField, len(fields))
	copy(cp, fields)
	return cp, nil
}

// ListSchemas returns all registered schema IDs in registration order.
func (s *RichDataStore) ListSchemas() []types.Hash {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if len(s.schemaOrder) == 0 {
		return nil
	}
	out := make([]types.Hash, len(s.schemaOrder))
	copy(out, s.schemaOrder)
	return out
}

// ValidateData checks that data conforms to the schema: required fields are
// present, all fields belong to the schema, and size limits are respected.
func (s *RichDataStore) ValidateData(schemaID types.Hash, data map[string][]byte) error {
	s.mu.RLock()
	fields, ok := s.schemas[schemaID]
	s.mu.RUnlock()
	if !ok {
		return ErrSchemaNotFound
	}

	fieldMap := make(map[string]*SchemaField, len(fields))
	for i := range fields {
		fieldMap[fields[i].Name] = &fields[i]
	}

	// Ensure every key in data is a known field.
	for name := range data {
		if _, known := fieldMap[name]; !known {
			return ErrFieldNotInSchema
		}
	}

	// Check required fields and size limits.
	for _, f := range fields {
		val, present := data[f.Name]
		if f.Required && !present {
			return ErrMissingRequired
		}
		if present && f.MaxSize > 0 && uint64(len(val)) > f.MaxSize {
			return ErrFieldTooLarge
		}
	}

	return nil
}

// StoreData validates and stores structured data under (schemaID, key).
func (s *RichDataStore) StoreData(schemaID types.Hash, key types.Hash, data map[string][]byte) error {
	if err := s.ValidateData(schemaID, data); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	store := s.data[schemaID]
	if _, exists := store[key]; exists {
		return ErrDataExists
	}

	// Deep copy the data to prevent external mutation.
	entry := make(map[string][]byte, len(data))
	for k, v := range data {
		cp := make([]byte, len(v))
		copy(cp, v)
		entry[k] = cp
	}
	store[key] = entry

	// Update indices.
	idx := s.indices[schemaID]
	for fieldName, value := range entry {
		idx.add(fieldName, value, key)
	}

	return nil
}

// GetData retrieves structured data for (schemaID, key).
func (s *RichDataStore) GetData(schemaID types.Hash, key types.Hash) (map[string][]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	store, ok := s.data[schemaID]
	if !ok {
		return nil, ErrSchemaNotFound
	}
	entry, ok := store[key]
	if !ok {
		return nil, ErrDataNotFound
	}

	// Return a deep copy.
	out := make(map[string][]byte, len(entry))
	for k, v := range entry {
		cp := make([]byte, len(v))
		copy(cp, v)
		out[k] = cp
	}
	return out, nil
}

// QueryByField returns all data keys in the schema where fieldName equals value.
func (s *RichDataStore) QueryByField(schemaID types.Hash, fieldName string, value []byte) ([]types.Hash, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	fields, ok := s.schemas[schemaID]
	if !ok {
		return nil, ErrSchemaNotFound
	}

	// Ensure the field exists in the schema.
	found := false
	for _, f := range fields {
		if f.Name == fieldName {
			found = true
			break
		}
	}
	if !found {
		return nil, ErrFieldNotInSchema
	}

	idx := s.indices[schemaID]
	return idx.query(fieldName, value), nil
}

// DeleteData removes the entry at (schemaID, key) and cleans up indices.
func (s *RichDataStore) DeleteData(schemaID types.Hash, key types.Hash) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	store, ok := s.data[schemaID]
	if !ok {
		return ErrSchemaNotFound
	}
	entry, ok := store[key]
	if !ok {
		return ErrDataNotFound
	}

	// Remove from indices first.
	idx := s.indices[schemaID]
	for fieldName, value := range entry {
		idx.remove(fieldName, value, key)
	}

	delete(store, key)
	return nil
}

// fieldValueEqual is a helper for comparing byte slices.
func fieldValueEqual(a, b []byte) bool {
	return bytes.Equal(a, b)
}
