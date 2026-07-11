// SPDX-License-Identifier: MPL-2.0

package client_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"gitlab.com/leifwind/stream/terraform-provider-leifwind/client"
)

func TestDeleteProjectAndDryRun(t *testing.T) {
	t.Parallel()
	c, _ := newTestClient(t)
	ctx := context.Background()
	p, err := c.Metadata.UpsertProject(ctx, client.MetadataProject{Name: "del_me"})
	if err != nil {
		t.Fatal(err)
	}

	// dry run: validated but rolled back
	if err := c.Metadata.DeleteProject(ctx, *p.ObjectID, client.DryRun()); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Metadata.GetProject(ctx, *p.ObjectID); err != nil {
		t.Fatalf("dry-run delete must not delete: %v", err)
	}

	if err := c.Metadata.DeleteProject(ctx, *p.ObjectID); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Metadata.GetProject(ctx, *p.ObjectID); !errors.Is(err, client.ErrNotFound) {
		t.Fatalf("want ErrNotFound after delete, got %v", err)
	}
}

func TestListAndIterProjectsPaginate(t *testing.T) {
	t.Parallel()
	c, _ := newTestClient(t) // fresh org ⇒ only our projects
	ctx := context.Background()
	const total = 55 // > MAX_PAGE_SIZE to force >1 page even at limit 50
	for i := 0; i < total; i++ {
		if _, err := c.Metadata.UpsertProject(ctx,
			client.MetadataProject{Name: fmt.Sprintf("page_%02d", i)}); err != nil {
			t.Fatal(err)
		}
	}

	page, err := c.Metadata.ListProjects(ctx, client.ListOpts{Limit: 25})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Objects) != 25 || page.Cursor == nil {
		t.Fatalf("first page: %d objects, cursor=%v", len(page.Objects), page.Cursor)
	}

	// walk pages with cursor only (pattern/limit ride inside the cursor)
	seen := len(page.Objects)
	for page.Cursor != nil {
		page, err = c.Metadata.ListProjects(ctx, client.ListOpts{Cursor: *page.Cursor})
		if err != nil {
			t.Fatal(err)
		}
		seen += len(page.Objects)
	}
	if seen != total {
		t.Fatalf("page walk saw %d, want %d", seen, total)
	}

	count := 0
	for _, err := range c.Metadata.IterProjects(ctx, client.ListOpts{Limit: 20}) {
		if err != nil {
			t.Fatal(err)
		}
		count++
	}
	if count != total {
		t.Fatalf("iter saw %d, want %d", count, total)
	}
}

func TestListProjectsPattern(t *testing.T) {
	t.Parallel()
	c, _ := newTestClient(t)
	ctx := context.Background()
	for _, n := range []string{"alpha_one", "alpha_two", "beta_one"} {
		if _, err := c.Metadata.UpsertProject(ctx, client.MetadataProject{Name: n}); err != nil {
			t.Fatal(err)
		}
	}
	page, err := c.Metadata.ListProjects(ctx, client.ListOpts{Pattern: "alpha"})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Objects) != 2 {
		t.Fatalf("pattern alpha: got %d", len(page.Objects))
	}
}
