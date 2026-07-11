// SPDX-License-Identifier: MPL-2.0

package client_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"gitlab.com/leifwind/stream/terraform-provider-leifwind/client"
)

func TestEntityLifecycle(t *testing.T) {
	t.Parallel()
	c, _ := newTestClient(t)
	ctx := context.Background()
	p, err := c.Metadata.UpsertProject(ctx, client.MetadataProject{Name: "ent_proj"})
	if err != nil {
		t.Fatal(err)
	}

	e, err := c.Metadata.UpsertEntity(ctx, client.MetadataEntity{ProjectID: *p.ObjectID, Name: "book"})
	if err != nil {
		t.Fatal(err)
	}
	if e.ObjectID == nil {
		t.Fatal("no object_id")
	}

	got, err := c.Metadata.GetEntity(ctx, *p.ObjectID, *e.ObjectID)
	if err != nil || got.Name != "book" {
		t.Fatalf("get: %+v, %v", got, err)
	}

	page, err := c.Metadata.ListEntities(ctx, *p.ObjectID, client.ListOpts{Pattern: "boo"})
	if err != nil || len(page.Objects) != 1 {
		t.Fatalf("list: %d, %v", len(page.Objects), err)
	}

	n := 0
	for _, err := range c.Metadata.IterEntities(ctx, *p.ObjectID, client.ListOpts{}) {
		if err != nil {
			t.Fatal(err)
		}
		n++
	}
	if n != 1 {
		t.Fatalf("iter: %d", n)
	}

	if err := c.Metadata.DeleteEntity(ctx, *p.ObjectID, *e.ObjectID); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Metadata.GetEntity(ctx, *p.ObjectID, *e.ObjectID); !errors.Is(err, client.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestUpsertEntityUnknownProject(t *testing.T) {
	t.Parallel()
	c, _ := newTestClient(t)
	_, err := c.Metadata.UpsertEntity(context.Background(),
		client.MetadataEntity{ProjectID: uuid.New(), Name: "orphan"})
	if !errors.Is(err, client.ErrNotFound) {
		t.Fatalf("want ErrNotFound (unknown/foreign project), got %v", err)
	}
}
