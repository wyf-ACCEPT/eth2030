package proofs

import (
	"sort"
	"testing"
)

func TestNewProverRegistry(t *testing.T) {
	reg := NewProverRegistry()
	if reg == nil {
		t.Fatal("NewProverRegistry returned nil")
	}
	names := reg.Names()
	if len(names) != 0 {
		t.Errorf("new registry has %d names, want 0", len(names))
	}
}

func TestProverRegistryRegister(t *testing.T) {
	reg := NewProverRegistry()
	agg := NewSimpleAggregator()

	err := reg.Register("simple", agg)
	if err != nil {
		t.Fatalf("Register error: %v", err)
	}
}

func TestProverRegistryRegisterDuplicate(t *testing.T) {
	reg := NewProverRegistry()
	agg := NewSimpleAggregator()

	if err := reg.Register("simple", agg); err != nil {
		t.Fatalf("first Register error: %v", err)
	}

	err := reg.Register("simple", agg)
	if err != ErrAggregatorExists {
		t.Errorf("duplicate Register error = %v, want ErrAggregatorExists", err)
	}
}

func TestProverRegistryGet(t *testing.T) {
	reg := NewProverRegistry()
	agg := NewSimpleAggregator()
	reg.Register("simple", agg)

	got, err := reg.Get("simple")
	if err != nil {
		t.Fatalf("Get error: %v", err)
	}
	if got == nil {
		t.Fatal("Get returned nil")
	}
}

func TestProverRegistryGetNotFound(t *testing.T) {
	reg := NewProverRegistry()

	_, err := reg.Get("nonexistent")
	if err != ErrAggregatorNotFound {
		t.Errorf("Get(nonexistent) error = %v, want ErrAggregatorNotFound", err)
	}
}

func TestProverRegistryNamesAll(t *testing.T) {
	reg := NewProverRegistry()
	reg.Register("alpha", NewSimpleAggregator())
	reg.Register("beta", NewSimpleAggregator())
	reg.Register("gamma", NewSimpleAggregator())

	names := reg.Names()
	if len(names) != 3 {
		t.Fatalf("Names count = %d, want 3", len(names))
	}

	sort.Strings(names)
	expected := []string{"alpha", "beta", "gamma"}
	for i, name := range names {
		if name != expected[i] {
			t.Errorf("Names[%d] = %q, want %q", i, name, expected[i])
		}
	}
}

func TestProverRegistryMultipleAggregators(t *testing.T) {
	reg := NewProverRegistry()
	agg1 := NewSimpleAggregator()
	agg2 := NewSimpleAggregator()

	if err := reg.Register("agg1", agg1); err != nil {
		t.Fatalf("Register agg1 error: %v", err)
	}
	if err := reg.Register("agg2", agg2); err != nil {
		t.Fatalf("Register agg2 error: %v", err)
	}

	// Both should be retrievable by their respective names.
	got1, err := reg.Get("agg1")
	if err != nil {
		t.Fatalf("Get agg1 error: %v", err)
	}
	got2, err := reg.Get("agg2")
	if err != nil {
		t.Fatalf("Get agg2 error: %v", err)
	}
	if got1 == nil || got2 == nil {
		t.Error("registered aggregators should not return nil")
	}

	// Names should contain both.
	names := reg.Names()
	if len(names) != 2 {
		t.Errorf("Names count = %d, want 2", len(names))
	}
}

func TestProverRegistryErrorMessages(t *testing.T) {
	if ErrAggregatorExists.Error() == "" {
		t.Error("ErrAggregatorExists should have non-empty message")
	}
	if ErrAggregatorNotFound.Error() == "" {
		t.Error("ErrAggregatorNotFound should have non-empty message")
	}
}
