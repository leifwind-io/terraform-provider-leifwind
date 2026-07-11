// SPDX-License-Identifier: MPL-2.0

package client_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"

	"github.com/google/uuid"
	"gitlab.com/leifwind/stream/terraform-provider-leifwind/client"
	"gitlab.com/leifwind/stream/terraform-provider-leifwind/client/leifwindtest"
)

// shared stack for ALL client blackbox tests (this file and later tasks')
var (
	sharedStack *leifwindtest.Stack
	stackErr    error
)

func TestMain(m *testing.M) {
	var cleanup func()
	sharedStack, cleanup, stackErr = leifwindtest.StartMain(leifwindtest.WithToxiproxy())
	code := m.Run()
	if cleanup != nil {
		cleanup()
	}
	os.Exit(code)
}

var orgMu sync.Mutex

// newTestClient returns a client bound to a FRESH org (tenant isolation).
func newTestClient(t *testing.T) (*client.Client, *leifwindtest.Org) {
	t.Helper()
	if stackErr != nil {
		t.Fatalf("stack: %v", stackErr)
	}
	orgMu.Lock()
	org := sharedStack.NewOrg(t)
	orgMu.Unlock()
	c, err := client.New(sharedStack.BackendURL,
		client.WithTokenSource(org.TokenSource(sharedStack)))
	if err != nil {
		t.Fatal(err)
	}
	return c, org
}

func TestNewRequiresEndpoint(t *testing.T) {
	if _, err := client.New(""); err == nil {
		t.Fatal("empty endpoint must error")
	}
}

func TestUpsertProjectCreatesAndAdopts(t *testing.T) {
	t.Parallel()
	c, _ := newTestClient(t)
	ctx := context.Background()

	created, err := c.Metadata.UpsertProject(ctx, client.MetadataProject{Name: "proj_a"})
	if err != nil {
		t.Fatal(err)
	}
	if created.ObjectID == nil || created.UniqueKey != "proj_a" {
		t.Fatalf("bad create result: %+v", created)
	}

	// same natural key, no object_id ⇒ adopt, same object_id back
	adopted, err := c.Metadata.UpsertProject(ctx, client.MetadataProject{Name: "proj_a"})
	if err != nil {
		t.Fatal(err)
	}
	if *adopted.ObjectID != *created.ObjectID {
		t.Fatalf("adopt returned different id: %s vs %s", adopted.ObjectID, created.ObjectID)
	}
}

func TestUpsertProjectImmutableRename(t *testing.T) {
	t.Parallel()
	c, _ := newTestClient(t)
	ctx := context.Background()
	p, err := c.Metadata.UpsertProject(ctx, client.MetadataProject{Name: "proj_b"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.Metadata.UpsertProject(ctx, client.MetadataProject{ObjectID: p.ObjectID, Name: "proj_b_renamed"})
	if !errors.Is(err, client.ErrValidation) {
		t.Fatalf("want ErrValidation, got %v", err)
	}
}

func TestGetProjectNotFound(t *testing.T) {
	t.Parallel()
	c, _ := newTestClient(t)
	_, err := c.Metadata.GetProject(context.Background(), uuid.New())
	if !errors.Is(err, client.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	var apiErr *client.APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != 404 {
		t.Fatalf("want APIError 404, got %v", err)
	}
}

var _ = fmt.Sprintf // keep fmt for later tasks extending this file
