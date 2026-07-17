// SPDX-License-Identifier: MPL-2.0

package client_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"gitlab.com/leifwind/stream/terraform-provider-leifwind/client"
)

func TestEmptyBody2xxTolerated(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusOK) // 200 with empty body
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)
	c, err := client.New(srv.URL, client.WithRetry(client.RetryConfig{MaxAttempts: 1}))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	// 204 with a non-nil out: no error, out stays zero-valued.
	page, err := c.Metadata.ListProjects(ctx, client.ListOpts{})
	if err != nil {
		t.Fatalf("204 with non-nil out must not error: %v", err)
	}
	if len(page.Objects) != 0 || page.Cursor != nil {
		t.Fatalf("out must stay zero-valued, got %+v", page)
	}

	// Delete* against an empty-body 200 succeeds.
	if err := c.Metadata.DeleteProject(ctx, uuid.New()); err != nil {
		t.Fatalf("DeleteProject: %v", err)
	}
	if err := c.Metadata.DeleteEntity(ctx, uuid.New(), uuid.New()); err != nil {
		t.Fatalf("DeleteEntity: %v", err)
	}
	if err := c.Metadata.DeleteField(ctx, uuid.New(), uuid.New(), uuid.New()); err != nil {
		t.Fatalf("DeleteField: %v", err)
	}
}
