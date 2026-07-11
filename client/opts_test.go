// SPDX-License-Identifier: MPL-2.0

package client

import "testing"

func TestListOptsOmitsZeroValues(t *testing.T) {
	if got := (ListOpts{}).values().Encode(); got != "" {
		t.Fatalf("zero opts must encode empty, got %q", got)
	}
	got := (ListOpts{Limit: 25, Pattern: "alpha", Cursor: "c1"}).values().Encode()
	want := "cursor=c1&limit=25&pattern=alpha"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestDryRunOption(t *testing.T) {
	if got := writeValues(nil).Encode(); got != "" {
		t.Fatalf("no opts must encode empty, got %q", got)
	}
	if got := writeValues([]WriteOption{DryRun()}).Encode(); got != "dry_run=true" {
		t.Fatalf("got %q", got)
	}
}
