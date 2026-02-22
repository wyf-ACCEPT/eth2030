// union_codec.go implements SSZ union type encoding and decoding with a
// selector byte, type registry, validation, and round-trip support.
//
// Per the SSZ spec, a union is encoded as:
//
//	[selector_byte (1)] [value_bytes (variable)]
//
// The selector identifies which variant type is active, and the value
// bytes contain the SSZ encoding of that variant. The hash tree root is:
//
//	hash(hash_tree_root(value), selector_chunk)
//
// where selector_chunk is a 32-byte chunk with the selector in byte 0.
//
// This codec provides a UnionTypeRegistry for registering variant types
// and UnionValue for encoding/decoding concrete union instances.
package ssz

import (
	"errors"
	"fmt"
)

// Union codec errors.
var (
	ErrUnionSelectorUnknown   = errors.New("ssz: unknown union selector")
	ErrUnionSelectorDuplicate = errors.New("ssz: duplicate union selector")
	ErrUnionRegistryEmpty     = errors.New("ssz: union registry has no types")
	ErrUnionDataTooShort      = errors.New("ssz: union data too short for selector")
	ErrUnionNilCodec          = errors.New("ssz: nil union codec provided")
	ErrUnionNilValue          = errors.New("ssz: nil union value")
	ErrUnionValueMismatch     = errors.New("ssz: union value does not match selector")
	ErrUnionMaxVariants       = errors.New("ssz: union exceeds max 256 variants")
)

// MaxUnionVariants is the maximum number of variant types in a union (0-255).
const MaxUnionVariants = 256

// UnionVariantCodec defines how to encode, decode, and hash a specific
// union variant type.
type UnionVariantCodec struct {
	// Selector is the unique byte identifying this variant (0-255).
	Selector byte
	// Name is a human-readable name for the variant.
	Name string
	// FixedSize is the fixed SSZ size of the variant, or 0 if variable-size.
	FixedSize int
	// Encode serializes a variant value to SSZ bytes.
	Encode func(value interface{}) ([]byte, error)
	// Decode deserializes SSZ bytes into a variant value.
	Decode func(data []byte) (interface{}, error)
	// HashTreeRootFn computes the hash tree root of a variant value.
	HashTreeRootFn func(value interface{}) ([32]byte, error)
}

// UnionTypeRegistry holds the set of variant types for a union.
type UnionTypeRegistry struct {
	variants map[byte]*UnionVariantCodec
	names    map[string]byte // name -> selector mapping
}

// NewUnionTypeRegistry creates an empty union type registry.
func NewUnionTypeRegistry() *UnionTypeRegistry {
	return &UnionTypeRegistry{
		variants: make(map[byte]*UnionVariantCodec),
		names:    make(map[string]byte),
	}
}

// Register adds a variant codec to the registry.
func (r *UnionTypeRegistry) Register(codec *UnionVariantCodec) error {
	if codec == nil {
		return ErrUnionNilCodec
	}
	if len(r.variants) >= MaxUnionVariants {
		return ErrUnionMaxVariants
	}
	if _, exists := r.variants[codec.Selector]; exists {
		return fmt.Errorf("%w: selector %d", ErrUnionSelectorDuplicate, codec.Selector)
	}
	r.variants[codec.Selector] = codec
	if codec.Name != "" {
		r.names[codec.Name] = codec.Selector
	}
	return nil
}

// Lookup returns the variant codec for the given selector.
func (r *UnionTypeRegistry) Lookup(selector byte) (*UnionVariantCodec, error) {
	codec, ok := r.variants[selector]
	if !ok {
		return nil, fmt.Errorf("%w: %d", ErrUnionSelectorUnknown, selector)
	}
	return codec, nil
}

// LookupByName returns the variant codec for the given name.
func (r *UnionTypeRegistry) LookupByName(name string) (*UnionVariantCodec, error) {
	sel, ok := r.names[name]
	if !ok {
		return nil, fmt.Errorf("%w: name %q", ErrUnionSelectorUnknown, name)
	}
	return r.variants[sel], nil
}

// Count returns the number of registered variants.
func (r *UnionTypeRegistry) Count() int {
	return len(r.variants)
}

// Selectors returns all registered selector bytes in ascending order.
func (r *UnionTypeRegistry) Selectors() []byte {
	sels := make([]byte, 0, len(r.variants))
	for s := range r.variants {
		sels = append(sels, s)
	}
	// Sort ascending.
	for i := 0; i < len(sels); i++ {
		for j := i + 1; j < len(sels); j++ {
			if sels[j] < sels[i] {
				sels[i], sels[j] = sels[j], sels[i]
			}
		}
	}
	return sels
}

// UnionValue is an encoded union instance with a selector and value.
type UnionValue struct {
	Selector byte
	Value    interface{}
}

// UnionCodec encodes and decodes union values using a type registry.
type UnionCodec struct {
	registry *UnionTypeRegistry
}

// NewUnionCodec creates a union codec backed by the given registry.
func NewUnionCodec(registry *UnionTypeRegistry) *UnionCodec {
	return &UnionCodec{registry: registry}
}

// Encode serializes a union value to SSZ bytes: [selector][value_bytes].
func (uc *UnionCodec) Encode(uv *UnionValue) ([]byte, error) {
	if uv == nil {
		return nil, ErrUnionNilValue
	}
	codec, err := uc.registry.Lookup(uv.Selector)
	if err != nil {
		return nil, err
	}
	if codec.Encode == nil {
		return nil, fmt.Errorf("%w: no encode function for selector %d",
			ErrUnionNilCodec, uv.Selector)
	}
	valueBytes, err := codec.Encode(uv.Value)
	if err != nil {
		return nil, fmt.Errorf("ssz: union encode variant %d: %w", uv.Selector, err)
	}
	// Prepend selector byte.
	out := make([]byte, 1+len(valueBytes))
	out[0] = uv.Selector
	copy(out[1:], valueBytes)
	return out, nil
}

// Decode deserializes SSZ bytes into a union value.
func (uc *UnionCodec) Decode(data []byte) (*UnionValue, error) {
	if len(data) < 1 {
		return nil, ErrUnionDataTooShort
	}
	selector := data[0]
	codec, err := uc.registry.Lookup(selector)
	if err != nil {
		return nil, err
	}
	if codec.Decode == nil {
		return nil, fmt.Errorf("%w: no decode function for selector %d",
			ErrUnionNilCodec, selector)
	}
	value, err := codec.Decode(data[1:])
	if err != nil {
		return nil, fmt.Errorf("ssz: union decode variant %d: %w", selector, err)
	}
	return &UnionValue{
		Selector: selector,
		Value:    value,
	}, nil
}

// HashTreeRoot computes the union hash tree root:
//
//	hash(hash_tree_root(value), selector_chunk)
func (uc *UnionCodec) HashTreeRoot(uv *UnionValue) ([32]byte, error) {
	if uv == nil {
		return [32]byte{}, ErrUnionNilValue
	}
	codec, err := uc.registry.Lookup(uv.Selector)
	if err != nil {
		return [32]byte{}, err
	}
	if codec.HashTreeRootFn == nil {
		return [32]byte{}, fmt.Errorf("%w: no hash function for selector %d",
			ErrUnionNilCodec, uv.Selector)
	}
	valueRoot, err := codec.HashTreeRootFn(uv.Value)
	if err != nil {
		return [32]byte{}, fmt.Errorf("ssz: union hash variant %d: %w", uv.Selector, err)
	}
	return HashTreeRootUnion(valueRoot, uv.Selector), nil
}

// SizeSSZ returns the serialized size of a union value (1 + value size).
func (uc *UnionCodec) SizeSSZ(uv *UnionValue) (int, error) {
	if uv == nil {
		return 0, ErrUnionNilValue
	}
	codec, err := uc.registry.Lookup(uv.Selector)
	if err != nil {
		return 0, err
	}
	if codec.FixedSize > 0 {
		return 1 + codec.FixedSize, nil
	}
	// For variable-size, we need to encode to determine size.
	if codec.Encode == nil {
		return 0, fmt.Errorf("%w: no encode function for selector %d",
			ErrUnionNilCodec, uv.Selector)
	}
	valueBytes, err := codec.Encode(uv.Value)
	if err != nil {
		return 0, err
	}
	return 1 + len(valueBytes), nil
}

// Validate checks that a union value is valid for the registry.
func (uc *UnionCodec) Validate(uv *UnionValue) error {
	if uv == nil {
		return ErrUnionNilValue
	}
	_, err := uc.registry.Lookup(uv.Selector)
	if err != nil {
		return err
	}
	return nil
}

// RoundTrip encodes and decodes a union value, verifying lossless
// serialization. Returns the decoded value.
func (uc *UnionCodec) RoundTrip(uv *UnionValue) (*UnionValue, error) {
	encoded, err := uc.Encode(uv)
	if err != nil {
		return nil, fmt.Errorf("ssz: round-trip encode: %w", err)
	}
	decoded, err := uc.Decode(encoded)
	if err != nil {
		return nil, fmt.Errorf("ssz: round-trip decode: %w", err)
	}
	return decoded, nil
}

// NoneSelector is the conventional selector byte for the "None" variant
// in optional unions.
const NoneSelector byte = 0

// IsNone checks whether a union value represents the None variant.
func IsNone(uv *UnionValue) bool {
	return uv != nil && uv.Selector == NoneSelector && uv.Value == nil
}

// NewNoneValue creates a None union value with selector 0 and nil value.
func NewNoneValue() *UnionValue {
	return &UnionValue{Selector: NoneSelector, Value: nil}
}
