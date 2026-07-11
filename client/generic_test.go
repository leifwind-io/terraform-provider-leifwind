// SPDX-License-Identifier: MPL-2.0

package client_test

import (
	"context"
	"sort"
	"testing"

	"github.com/google/uuid"
	"gitlab.com/leifwind/stream/terraform-provider-leifwind/client"
)

func TestListEntityFragments(t *testing.T) {
	t.Parallel()
	c, _ := newTestClient(t)
	ctx := context.Background()
	// Project names are globally unique across tenants (LW-71); use a
	// per-test-unique name to avoid cross-test 409s.
	p, err := c.Metadata.UpsertProject(ctx, client.MetadataProject{Name: "frag_proj_" + uuid.NewString()[:8]})
	if err != nil {
		t.Fatal(err)
	}
	e, err := c.Metadata.UpsertEntity(ctx, client.MetadataEntity{ProjectID: *p.ObjectID, Name: "doc"})
	if err != nil {
		t.Fatal(err)
	}
	// A FRAGMENT column syncs against the entity's KEY column(s) server-side;
	// adding a FRAGMENT field before any KEY field exists 500s on
	// backend:edge (same sync_entity_schema class of bug as LW-70), so
	// create a KEY field first.
	if _, err := c.Metadata.UpsertField(ctx, client.MetadataField{
		ProjectID: *p.ObjectID, EntityID: *e.ObjectID, Name: "id",
		Config:     client.FieldConfig{DataType: client.DataTypeText},
		Connection: client.Connection{Type: client.ConnectionKey},
	}); err != nil {
		t.Fatal(err)
	}
	for _, f := range []struct{ field, fragment string }{
		{"body", "content"}, {"meta", "annotations"},
	} {
		if _, err := c.Metadata.UpsertField(ctx, client.MetadataField{
			ProjectID: *p.ObjectID, EntityID: *e.ObjectID, Name: f.field,
			Config:     client.FieldConfig{DataType: client.DataTypeText},
			Connection: client.Connection{Type: client.ConnectionFragment, FragmentName: f.fragment},
		}); err != nil {
			t.Fatalf("field %q: %v", f.field, err)
		}
	}

	frags, err := c.Generic.ListEntityFragments(ctx, *p.ObjectID, "doc")
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(frags)
	if len(frags) != 2 || frags[0] != "annotations" || frags[1] != "content" {
		t.Fatalf("got %v", frags)
	}
}
