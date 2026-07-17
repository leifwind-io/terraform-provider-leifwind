// SPDX-License-Identifier: MPL-2.0

package client_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"gitlab.com/leifwind/stream/terraform-provider-leifwind/client"
)

// noObjectIDClient answers every request 200 with a body whose object_id is
// absent — a contract-violating backend.
func noObjectIDClient(t *testing.T) *client.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		last := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
		if _, err := uuid.Parse(last); err == nil || r.Method == http.MethodPost {
			// single-object response (Get*/Upsert*) without object_id
			_, _ = w.Write([]byte(`{"metadata_type":"metadata_project","name":"p"}`))
			return
		}
		// list response whose objects lack object_id
		_, _ = w.Write([]byte(`{"objects":[{"metadata_type":"metadata_project","name":"p"}],"cursor":null}`))
	}))
	t.Cleanup(srv.Close)
	c, err := client.New(srv.URL, client.WithRetry(client.RetryConfig{MaxAttempts: 1}))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestMissingObjectIDReturnsErrorNotPanic(t *testing.T) {
	t.Parallel()
	c := noObjectIDClient(t)
	ctx := context.Background()
	id := uuid.New()

	calls := map[string]func() error{
		"UpsertProject": func() error {
			_, err := c.Metadata.UpsertProject(ctx, client.MetadataProject{Name: "p"})
			return err
		},
		"GetProject": func() error { _, err := c.Metadata.GetProject(ctx, id); return err },
		"ListProjects": func() error {
			_, err := c.Metadata.ListProjects(ctx, client.ListOpts{})
			return err
		},
		"UpsertEntity": func() error {
			_, err := c.Metadata.UpsertEntity(ctx, client.MetadataEntity{ProjectID: id, Name: "e"})
			return err
		},
		"GetEntity": func() error { _, err := c.Metadata.GetEntity(ctx, id, id); return err },
		"ListEntities": func() error {
			_, err := c.Metadata.ListEntities(ctx, id, client.ListOpts{})
			return err
		},
		"UpsertField": func() error {
			_, err := c.Metadata.UpsertField(ctx, client.MetadataField{
				ProjectID: id, EntityID: id, Name: "f",
				Config:     client.FieldConfig{DataType: client.DataTypeText},
				Connection: client.Connection{Type: client.ConnectionKey},
			})
			return err
		},
		"GetField": func() error { _, err := c.Metadata.GetField(ctx, id, id, id); return err },
		"ListFields": func() error {
			_, err := c.Metadata.ListFields(ctx, id, id, client.ListOpts{})
			return err
		},
	}
	for name, call := range calls {
		if err := call(); err == nil || !strings.Contains(err.Error(), "object without object_id") {
			t.Errorf("%s: want object_id contract error, got %v", name, err)
		}
	}
}
