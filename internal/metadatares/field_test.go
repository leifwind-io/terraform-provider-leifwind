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
