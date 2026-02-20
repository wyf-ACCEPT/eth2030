package consensus

import (
	"testing"
)

func TestTechDebtTracker_RegisterAndCount(t *testing.T) {
	tracker := NewTechDebtTracker(nil)

	if tracker.FieldCount() != 0 {
		t.Fatalf("expected 0 fields, got %d", tracker.FieldCount())
	}

	err := tracker.RegisterDeprecation(&DeprecatedField{
		FieldName:           "old_balance_field",
		DeprecatedSinceEpoch: 100,
		ReplacedBy:          []string{"new_balance_field"},
		RemovalEpoch:        200,
	})
	if err != nil {
		t.Fatalf("RegisterDeprecation: %v", err)
	}
	if tracker.FieldCount() != 1 {
		t.Fatalf("expected 1 field, got %d", tracker.FieldCount())
	}

	err = tracker.RegisterDeprecation(&DeprecatedField{
		FieldName:           "old_nonce_field",
		DeprecatedSinceEpoch: 150,
		RemovalEpoch:        300,
	})
	if err != nil {
		t.Fatalf("RegisterDeprecation: %v", err)
	}
	if tracker.FieldCount() != 2 {
		t.Fatalf("expected 2 fields, got %d", tracker.FieldCount())
	}
}

func TestTechDebtTracker_RegisterErrors(t *testing.T) {
	tracker := NewTechDebtTracker(nil)

	// nil field
	if err := tracker.RegisterDeprecation(nil); err != ErrTechDebtNilField {
		t.Errorf("nil field: got %v, want %v", err, ErrTechDebtNilField)
	}

	// empty name
	if err := tracker.RegisterDeprecation(&DeprecatedField{}); err != ErrTechDebtEmptyName {
		t.Errorf("empty name: got %v, want %v", err, ErrTechDebtEmptyName)
	}

	// invalid epochs (removal before deprecation)
	if err := tracker.RegisterDeprecation(&DeprecatedField{
		FieldName:           "bad_epochs",
		DeprecatedSinceEpoch: 200,
		RemovalEpoch:        100,
	}); err != ErrTechDebtInvalidEpochs {
		t.Errorf("invalid epochs: got %v, want %v", err, ErrTechDebtInvalidEpochs)
	}

	// duplicate
	tracker.RegisterDeprecation(&DeprecatedField{
		FieldName:           "dup_field",
		DeprecatedSinceEpoch: 10,
	})
	if err := tracker.RegisterDeprecation(&DeprecatedField{
		FieldName:           "dup_field",
		DeprecatedSinceEpoch: 20,
	}); err != ErrTechDebtDuplicate {
		t.Errorf("duplicate: got %v, want %v", err, ErrTechDebtDuplicate)
	}
}

func TestTechDebtTracker_IsDeprecated(t *testing.T) {
	tracker := NewTechDebtTracker(nil)

	tracker.RegisterDeprecation(&DeprecatedField{
		FieldName:           "old_field",
		DeprecatedSinceEpoch: 100,
		RemovalEpoch:        200,
	})

	// Before deprecation epoch.
	if tracker.IsDeprecated("old_field", 50) {
		t.Error("should not be deprecated at epoch 50")
	}

	// At deprecation epoch.
	if !tracker.IsDeprecated("old_field", 100) {
		t.Error("should be deprecated at epoch 100")
	}

	// After deprecation, before removal.
	if !tracker.IsDeprecated("old_field", 150) {
		t.Error("should be deprecated at epoch 150")
	}

	// After removal epoch -- still "deprecated" (the function only checks
	// if the field was deprecated, not if it was removed).
	if !tracker.IsDeprecated("old_field", 250) {
		t.Error("should still report as deprecated at epoch 250")
	}

	// Unknown field.
	if tracker.IsDeprecated("nonexistent", 100) {
		t.Error("unknown field should not be deprecated")
	}
}

func TestTechDebtTracker_GetReplacements(t *testing.T) {
	tracker := NewTechDebtTracker(nil)

	tracker.RegisterDeprecation(&DeprecatedField{
		FieldName:           "old_a",
		DeprecatedSinceEpoch: 10,
		ReplacedBy:          []string{"new_a1", "new_a2"},
	})

	tracker.RegisterDeprecation(&DeprecatedField{
		FieldName:           "old_b",
		DeprecatedSinceEpoch: 20,
		// no replacements
	})

	repls := tracker.GetReplacements("old_a")
	if len(repls) != 2 {
		t.Fatalf("expected 2 replacements, got %d", len(repls))
	}
	if repls[0] != "new_a1" || repls[1] != "new_a2" {
		t.Errorf("replacements = %v, want [new_a1, new_a2]", repls)
	}

	// Ensure returned slice is a copy.
	repls[0] = "mutated"
	repls2 := tracker.GetReplacements("old_a")
	if repls2[0] == "mutated" {
		t.Error("GetReplacements should return a copy")
	}

	// No replacements.
	if r := tracker.GetReplacements("old_b"); r != nil {
		t.Errorf("expected nil replacements for old_b, got %v", r)
	}

	// Unknown field.
	if r := tracker.GetReplacements("unknown"); r != nil {
		t.Errorf("expected nil for unknown field, got %v", r)
	}
}

func TestTechDebtTracker_MigrateState(t *testing.T) {
	tracker := NewTechDebtTracker(&TechDebtConfig{AutoMigrate: true})

	tracker.RegisterDeprecation(&DeprecatedField{
		FieldName:           "old_balance",
		DeprecatedSinceEpoch: 100,
		ReplacedBy:          []string{"new_balance"},
		RemovalEpoch:        200,
	})

	tracker.RegisterDeprecation(&DeprecatedField{
		FieldName:           "old_nonce",
		DeprecatedSinceEpoch: 100,
		ReplacedBy:          []string{"new_nonce"},
		RemovalEpoch:        300,
	})

	state := map[string]interface{}{
		"old_balance": uint64(1000),
		"old_nonce":   uint64(42),
		"other":       "keep",
	}

	// Migrate at epoch 150 (deprecated but not removed).
	result, err := tracker.MigrateState(state, 150)
	if err != nil {
		t.Fatalf("MigrateState: %v", err)
	}

	// Replacements should be populated.
	if result["new_balance"] != uint64(1000) {
		t.Errorf("new_balance = %v, want 1000", result["new_balance"])
	}
	if result["new_nonce"] != uint64(42) {
		t.Errorf("new_nonce = %v, want 42", result["new_nonce"])
	}
	// Old fields should still exist (not past removal epoch).
	if result["old_balance"] != uint64(1000) {
		t.Errorf("old_balance should still exist at epoch 150")
	}
	if result["other"] != "keep" {
		t.Error("non-deprecated field should be preserved")
	}

	// Migrate at epoch 250 (old_balance past removal, old_nonce not).
	result2, err := tracker.MigrateState(state, 250)
	if err != nil {
		t.Fatalf("MigrateState at 250: %v", err)
	}
	if _, exists := result2["old_balance"]; exists {
		t.Error("old_balance should be removed at epoch 250")
	}
	if _, exists := result2["old_nonce"]; !exists {
		t.Error("old_nonce should still exist at epoch 250")
	}

	// Original state should not be mutated.
	if _, exists := state["old_balance"]; !exists {
		t.Error("original state should not be mutated")
	}

	// nil state should error.
	if _, err := tracker.MigrateState(nil, 100); err == nil {
		t.Error("expected error for nil state")
	}
}

func TestTechDebtTracker_MigrateNoOverwrite(t *testing.T) {
	tracker := NewTechDebtTracker(&TechDebtConfig{AutoMigrate: false})

	tracker.RegisterDeprecation(&DeprecatedField{
		FieldName:           "old_field",
		DeprecatedSinceEpoch: 10,
		ReplacedBy:          []string{"new_field"},
		RemovalEpoch:        100,
	})

	state := map[string]interface{}{
		"old_field": "old_value",
		"new_field": "existing_value", // already exists
	}

	result, err := tracker.MigrateState(state, 50)
	if err != nil {
		t.Fatalf("MigrateState: %v", err)
	}

	// Existing replacement should not be overwritten.
	if result["new_field"] != "existing_value" {
		t.Errorf("new_field = %v, want existing_value", result["new_field"])
	}
}

func TestTechDebtTracker_DeprecationReport(t *testing.T) {
	tracker := NewTechDebtTracker(nil)

	tracker.RegisterDeprecation(&DeprecatedField{
		FieldName:           "field_c",
		DeprecatedSinceEpoch: 300,
		RemovalEpoch:        500,
	})
	tracker.RegisterDeprecation(&DeprecatedField{
		FieldName:           "field_a",
		DeprecatedSinceEpoch: 100,
		RemovalEpoch:        200,
	})
	tracker.RegisterDeprecation(&DeprecatedField{
		FieldName:           "field_b",
		DeprecatedSinceEpoch: 200,
		RemovalEpoch:        400,
	})

	// At epoch 150: only field_a is deprecated and not removed.
	report := tracker.DeprecationReport(150)
	if len(report) != 1 {
		t.Fatalf("epoch 150: expected 1, got %d", len(report))
	}
	if report[0].FieldName != "field_a" {
		t.Errorf("expected field_a, got %s", report[0].FieldName)
	}

	// At epoch 350: field_a removed, field_b and field_c active.
	report = tracker.DeprecationReport(350)
	if len(report) != 2 {
		t.Fatalf("epoch 350: expected 2, got %d", len(report))
	}
	if report[0].FieldName != "field_b" {
		t.Errorf("first should be field_b, got %s", report[0].FieldName)
	}
	if report[1].FieldName != "field_c" {
		t.Errorf("second should be field_c, got %s", report[1].FieldName)
	}

	// At epoch 600: all removed, empty report.
	report = tracker.DeprecationReport(600)
	if len(report) != 0 {
		t.Errorf("epoch 600: expected 0, got %d", len(report))
	}

	// At epoch 50: none deprecated yet.
	report = tracker.DeprecationReport(50)
	if len(report) != 0 {
		t.Errorf("epoch 50: expected 0, got %d", len(report))
	}
}

func TestTechDebtTracker_CleanupRemovedFields(t *testing.T) {
	tracker := NewTechDebtTracker(nil)

	tracker.RegisterDeprecation(&DeprecatedField{
		FieldName:           "remove_me",
		DeprecatedSinceEpoch: 100,
		RemovalEpoch:        200,
	})
	tracker.RegisterDeprecation(&DeprecatedField{
		FieldName:           "keep_me",
		DeprecatedSinceEpoch: 100,
		RemovalEpoch:        500,
	})
	tracker.RegisterDeprecation(&DeprecatedField{
		FieldName:           "no_removal",
		DeprecatedSinceEpoch: 100,
		RemovalEpoch:        0, // never removed
	})

	state := map[string]interface{}{
		"remove_me":  "gone",
		"keep_me":    "stay",
		"no_removal": "forever",
		"other":      "unrelated",
	}

	// At epoch 300: remove_me is past removal, keep_me and no_removal are not.
	removed := tracker.CleanupRemovedFields(state, 300)
	if removed != 1 {
		t.Fatalf("expected 1 removed, got %d", removed)
	}
	if _, exists := state["remove_me"]; exists {
		t.Error("remove_me should be cleaned up")
	}
	if _, exists := state["keep_me"]; !exists {
		t.Error("keep_me should still exist")
	}
	if _, exists := state["no_removal"]; !exists {
		t.Error("no_removal should still exist")
	}
	if _, exists := state["other"]; !exists {
		t.Error("unrelated field should still exist")
	}

	// nil state returns 0.
	if n := tracker.CleanupRemovedFields(nil, 300); n != 0 {
		t.Errorf("nil state: expected 0, got %d", n)
	}
}

func TestTechDebtTracker_IsRemoved(t *testing.T) {
	tracker := NewTechDebtTracker(nil)

	tracker.RegisterDeprecation(&DeprecatedField{
		FieldName:           "removable",
		DeprecatedSinceEpoch: 100,
		RemovalEpoch:        200,
	})
	tracker.RegisterDeprecation(&DeprecatedField{
		FieldName:           "permanent",
		DeprecatedSinceEpoch: 100,
		RemovalEpoch:        0,
	})

	if tracker.IsRemoved("removable", 150) {
		t.Error("should not be removed at epoch 150")
	}
	if !tracker.IsRemoved("removable", 200) {
		t.Error("should be removed at epoch 200")
	}
	if !tracker.IsRemoved("removable", 300) {
		t.Error("should be removed at epoch 300")
	}
	if tracker.IsRemoved("permanent", 1000) {
		t.Error("permanent field should never be removed")
	}
	if tracker.IsRemoved("nonexistent", 100) {
		t.Error("unknown field should not be removed")
	}
}

func TestTechDebtTracker_KnownDeprecations(t *testing.T) {
	config := &TechDebtConfig{
		AutoMigrate: true,
		KnownDeprecations: []*DeprecatedField{
			{
				FieldName:           "legacy_slot_count",
				DeprecatedSinceEpoch: 50,
				ReplacedBy:          []string{"modern_slot_count"},
				RemovalEpoch:        100,
			},
			{
				FieldName:           "legacy_epoch_length",
				DeprecatedSinceEpoch: 50,
				ReplacedBy:          []string{"epoch_slot_count"},
				RemovalEpoch:        100,
			},
		},
	}

	tracker := NewTechDebtTracker(config)

	if tracker.FieldCount() != 2 {
		t.Fatalf("expected 2 pre-loaded fields, got %d", tracker.FieldCount())
	}

	if !tracker.IsDeprecated("legacy_slot_count", 75) {
		t.Error("legacy_slot_count should be deprecated at epoch 75")
	}

	repls := tracker.GetReplacements("legacy_epoch_length")
	if len(repls) != 1 || repls[0] != "epoch_slot_count" {
		t.Errorf("replacements = %v, want [epoch_slot_count]", repls)
	}
}
