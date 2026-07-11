// SPDX-License-Identifier: MPL-2.0

package metadatares

import "testing"

func TestParseImportUUIDs(t *testing.T) {
	ids, err := parseImportUUIDs("a2ff0efa-64ac-4499-b2a4-99b598ee1c9f", 1)
	if err != nil || len(ids) != 1 {
		t.Fatalf("single: %v %v", ids, err)
	}
	ids, err = parseImportUUIDs("a2ff0efa-64ac-4499-b2a4-99b598ee1c9f/7e57d004-2b97-44e7-8f00-63d2c6b0a50e", 2)
	if err != nil || len(ids) != 2 {
		t.Fatalf("double: %v %v", ids, err)
	}
	if _, err := parseImportUUIDs("only-one", 2); err == nil {
		t.Fatal("want error on wrong part count")
	}
	if _, err := parseImportUUIDs("not-a-uuid/also-not", 2); err == nil {
		t.Fatal("want error on non-uuid parts")
	}
}
