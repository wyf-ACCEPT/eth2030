package types

import (
	"errors"
	"fmt"
	"sort"
	"sync"
)

// TxTypeInfo describes metadata about a transaction type.
type TxTypeInfo struct {
	TypeID             uint8
	Name               string
	SupportsAccessList bool
	SupportsBlobs      bool
	MaxPayloadSize     uint64
	MinGas             uint64
}

// TxTypeRegistry is a thread-safe registry of supported transaction types.
type TxTypeRegistry struct {
	mu    sync.RWMutex
	types map[uint8]TxTypeInfo
}

// NewTxTypeRegistry creates a registry pre-populated with the standard
// Ethereum transaction types: Legacy(0), AccessList(1), DynamicFee(2),
// Blob(3), and SetCode(4).
func NewTxTypeRegistry() *TxTypeRegistry {
	r := &TxTypeRegistry{
		types: make(map[uint8]TxTypeInfo),
	}
	// Pre-register the five standard types.
	defaults := []TxTypeInfo{
		{
			TypeID:             LegacyTxType,
			Name:               "Legacy",
			SupportsAccessList: false,
			SupportsBlobs:      false,
			MaxPayloadSize:     131072, // 128 KiB
			MinGas:             21000,
		},
		{
			TypeID:             AccessListTxType,
			Name:               "AccessList",
			SupportsAccessList: true,
			SupportsBlobs:      false,
			MaxPayloadSize:     131072,
			MinGas:             21000,
		},
		{
			TypeID:             DynamicFeeTxType,
			Name:               "DynamicFee",
			SupportsAccessList: true,
			SupportsBlobs:      false,
			MaxPayloadSize:     131072,
			MinGas:             21000,
		},
		{
			TypeID:             BlobTxType,
			Name:               "Blob",
			SupportsAccessList: true,
			SupportsBlobs:      true,
			MaxPayloadSize:     786432, // 768 KiB (accounts for blob sidecar refs)
			MinGas:             21000,
		},
		{
			TypeID:             SetCodeTxType,
			Name:               "SetCode",
			SupportsAccessList: true,
			SupportsBlobs:      false,
			MaxPayloadSize:     131072,
			MinGas:             21000,
		},
	}
	for _, info := range defaults {
		r.types[info.TypeID] = info
	}
	return r
}

// Register adds a new transaction type to the registry. Returns an error if
// the TypeID is already registered.
func (r *TxTypeRegistry) Register(info TxTypeInfo) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if existing, ok := r.types[info.TypeID]; ok {
		return fmt.Errorf("tx type 0x%02x already registered as %q", existing.TypeID, existing.Name)
	}
	r.types[info.TypeID] = info
	return nil
}

// Lookup returns the TxTypeInfo for the given type ID, or an error if not found.
func (r *TxTypeRegistry) Lookup(typeID uint8) (*TxTypeInfo, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	info, ok := r.types[typeID]
	if !ok {
		return nil, fmt.Errorf("unknown tx type: 0x%02x", typeID)
	}
	return &info, nil
}

// IsSupported returns true if the given type ID is registered.
func (r *TxTypeRegistry) IsSupported(typeID uint8) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.types[typeID]
	return ok
}

// ValidateType checks that a transaction's characteristics match the
// requirements of its declared type. Returns an error on mismatch.
func (r *TxTypeRegistry) ValidateType(typeID uint8, hasAccessList bool, hasBlobs bool) error {
	r.mu.RLock()
	defer r.mu.RUnlock()

	info, ok := r.types[typeID]
	if !ok {
		return fmt.Errorf("unknown tx type: 0x%02x", typeID)
	}

	var errs []error

	if hasAccessList && !info.SupportsAccessList {
		errs = append(errs, fmt.Errorf("tx type %q (0x%02x) does not support access lists", info.Name, typeID))
	}
	if hasBlobs && !info.SupportsBlobs {
		errs = append(errs, fmt.Errorf("tx type %q (0x%02x) does not support blobs", info.Name, typeID))
	}

	return errors.Join(errs...)
}

// AllTypes returns all registered types sorted by TypeID.
func (r *TxTypeRegistry) AllTypes() []TxTypeInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]TxTypeInfo, 0, len(r.types))
	for _, info := range r.types {
		result = append(result, info)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].TypeID < result[j].TypeID
	})
	return result
}

// BlobTypes returns all registered types that support blobs, sorted by TypeID.
func (r *TxTypeRegistry) BlobTypes() []TxTypeInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var result []TxTypeInfo
	for _, info := range r.types {
		if info.SupportsBlobs {
			result = append(result, info)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].TypeID < result[j].TypeID
	})
	return result
}

// Count returns the number of registered transaction types.
func (r *TxTypeRegistry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.types)
}
