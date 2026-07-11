// SPDX-License-Identifier: MPL-2.0

package metadatares

import "testing"

func TestValidateFieldCombination(t *testing.T) {
	if msg := validateFieldCombination("FRAGMENT", "content", true); msg != "" {
		t.Fatalf("valid fragment rejected: %s", msg)
	}
	if msg := validateFieldCombination("KEY", "", false); msg != "" {
		t.Fatalf("valid key rejected: %s", msg)
	}
	if msg := validateFieldCombination("FRAGMENT", "", false); msg == "" {
		t.Fatal("FRAGMENT without fragment_name must be invalid")
	}
	if msg := validateFieldCombination("KEY", "content", true); msg == "" {
		t.Fatal("KEY with fragment_name must be invalid")
	}
}

func TestValidateKeyFieldIDsCombination(t *testing.T) {
	// FRAGMENT: key_field_ids required and non-empty
	if msg := validateKeyFieldIDsCombination("FRAGMENT", true, false); msg != "" {
		t.Fatalf("valid FRAGMENT rejected: %s", msg)
	}
	if msg := validateKeyFieldIDsCombination("FRAGMENT", false, false); msg == "" {
		t.Fatal("FRAGMENT without key_field_ids must be invalid")
	}
	if msg := validateKeyFieldIDsCombination("FRAGMENT", true, true); msg == "" {
		t.Fatal("FRAGMENT with empty key_field_ids must be invalid")
	}
	// KEY: key_field_ids forbidden
	if msg := validateKeyFieldIDsCombination("KEY", false, false); msg != "" {
		t.Fatalf("valid KEY rejected: %s", msg)
	}
	if msg := validateKeyFieldIDsCombination("KEY", true, false); msg == "" {
		t.Fatal("KEY with key_field_ids must be invalid")
	}
	if msg := validateKeyFieldIDsCombination("KEY", true, true); msg != "" {
		t.Fatalf("KEY with empty (non-null) key_field_ids should be tolerated: %s", msg)
	}
}

func TestMissingKeyFieldIDs(t *testing.T) {
	keys := map[string]struct{}{"a": {}, "b": {}}
	if got := missingKeyFieldIDs([]string{"a", "b"}, keys); got != nil {
		t.Fatalf("all present should be nil, got %v", got)
	}
	if got := missingKeyFieldIDs([]string{"a", "c"}, keys); len(got) != 1 || got[0] != "c" {
		t.Fatalf("want [c], got %v", got)
	}
	if got := missingKeyFieldIDs(nil, keys); got != nil {
		t.Fatalf("empty supplied should be nil, got %v", got)
	}
}
