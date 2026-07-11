// SPDX-License-Identifier: MPL-2.0

package client_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"gitlab.com/leifwind/stream/terraform-provider-leifwind/client"
)

func fieldFixture(t *testing.T) (*client.Client, uuid.UUID, uuid.UUID) {
	t.Helper()
	c, _ := newTestClient(t)
	ctx := context.Background()
	// Project names are globally unique across tenants (schema-per-project ⇒
	// DB-global Postgres schema names; by design, LW-71), so use a per-test
	// unique name to avoid cross-test 409s.
	p, err := c.Metadata.UpsertProject(ctx, client.MetadataProject{Name: "fld_proj_" + uuid.NewString()[:8]})
	if err != nil {
		t.Fatal(err)
	}
	e, err := c.Metadata.UpsertEntity(ctx, client.MetadataEntity{ProjectID: *p.ObjectID, Name: "book"})
	if err != nil {
		t.Fatal(err)
	}
	return c, *p.ObjectID, *e.ObjectID
}

func TestFieldLifecycleKeyAndFragment(t *testing.T) {
	t.Parallel()
	c, pid, eid := fieldFixture(t)
	ctx := context.Background()

	key, err := c.Metadata.UpsertField(ctx, client.MetadataField{
		ProjectID: pid, EntityID: eid, Name: "title",
		Config:     client.FieldConfig{DataType: client.DataTypeText},
		Connection: client.Connection{Type: client.ConnectionKey},
	})
	if err != nil {
		t.Fatal(err)
	}

	frag, err := c.Metadata.UpsertField(ctx, client.MetadataField{
		ProjectID: pid, EntityID: eid, Name: "body",
		Config:     client.FieldConfig{DataType: client.DataTypeText},
		Connection: client.Connection{Type: client.ConnectionFragment, FragmentName: "content"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if frag.Connection.FragmentName != "content" {
		t.Fatalf("fragment_name lost: %+v", frag.Connection)
	}

	// fragment_name is the ONLY mutable field attribute
	frag.Connection.FragmentName = "content_v2"
	updated, err := c.Metadata.UpsertField(ctx, frag)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Connection.FragmentName != "content_v2" {
		t.Fatalf("fragment_name not updated: %+v", updated.Connection)
	}

	// data_type is immutable
	bad := key
	bad.Config.DataType = client.DataTypeInteger
	if _, err := c.Metadata.UpsertField(ctx, bad); !errors.Is(err, client.ErrValidation) {
		t.Fatalf("want ErrValidation on data_type change, got %v", err)
	}

	got, err := c.Metadata.GetField(ctx, pid, eid, *key.ObjectID)
	if err != nil || got.Name != "title" {
		t.Fatalf("get: %+v, %v", got, err)
	}

	page, err := c.Metadata.ListFields(ctx, pid, eid, client.ListOpts{})
	if err != nil || len(page.Objects) != 2 {
		t.Fatalf("list: %d, %v", len(page.Objects), err)
	}

	// LW-70 also 500s when a delete would leave the entity with zero fields
	// (sync_entity_schema: "no fields provided to get_table_structure"), so
	// keep one KEY field alive while both delete paths are exercised below.
	if _, err := c.Metadata.UpsertField(ctx, client.MetadataField{
		ProjectID: pid, EntityID: eid, Name: "author",
		Config:     client.FieldConfig{DataType: client.DataTypeText},
		Connection: client.Connection{Type: client.ConnectionKey},
	}); err != nil {
		t.Fatal(err)
	}

	// Delete FRAGMENT before KEY: deleting a KEY field while a FRAGMENT
	// sibling exists 500s on backend:edge (LW-70).
	if err := c.Metadata.DeleteField(ctx, pid, eid, *frag.ObjectID); err != nil {
		t.Fatal(err)
	}
	if err := c.Metadata.DeleteField(ctx, pid, eid, *key.ObjectID); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Metadata.GetField(ctx, pid, eid, *key.ObjectID); !errors.Is(err, client.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestListFieldsBogusEntity404(t *testing.T) {
	t.Parallel()
	c, pid, _ := fieldFixture(t)
	_, err := c.Metadata.ListFields(context.Background(), pid, uuid.New(), client.ListOpts{})
	if !errors.Is(err, client.ErrNotFound) {
		t.Fatalf("want ErrNotFound for bogus entity (not empty list), got %v", err)
	}
}
