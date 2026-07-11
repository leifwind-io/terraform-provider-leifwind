// SPDX-License-Identifier: MPL-2.0

package client

import (
	"context"
	"iter"

	"github.com/google/uuid"
)

// MetadataService covers the /metadata control-plane endpoints.
type MetadataService struct {
	c *Client
}

// UpsertProject creates or adopts a project. Omit ObjectID to create-or-adopt
// by name; the response carries the canonical ObjectID and UniqueKey.
// Changing Name of an existing ObjectID returns ErrValidation (immutable).
func (s *MetadataService) UpsertProject(ctx context.Context, p MetadataProject, opts ...WriteOption) (MetadataProject, error) {
	var out MetadataProject
	err := s.c.do(ctx, "POST", "/metadata/projects", writeValues(opts), p, &out)
	return out, err
}

// GetProject fetches one project; ErrNotFound covers missing AND cross-tenant.
func (s *MetadataService) GetProject(ctx context.Context, projectID uuid.UUID) (MetadataProject, error) {
	var out MetadataProject
	err := s.c.do(ctx, "GET", "/metadata/projects/"+projectID.String(), nil, nil, &out)
	return out, err
}

// DeleteProject deletes a project; entities/fields cascade server-side and
// the per-project schema is dropped.
func (s *MetadataService) DeleteProject(ctx context.Context, projectID uuid.UUID, opts ...WriteOption) error {
	var out struct {
		Detail string `json:"detail"`
	}
	return s.c.do(ctx, "DELETE", "/metadata/projects/"+projectID.String(), writeValues(opts), nil, &out)
}

// ListProjects returns one page.
func (s *MetadataService) ListProjects(ctx context.Context, opts ListOpts) (Page[MetadataProject], error) {
	return listPage[MetadataProject](ctx, s.c, "/metadata/projects", opts)
}

// IterProjects auto-pages through all projects.
func (s *MetadataService) IterProjects(ctx context.Context, opts ListOpts) iter.Seq2[MetadataProject, error] {
	return iterPages(ctx, opts, func(ctx context.Context, o ListOpts) (Page[MetadataProject], error) {
		return s.ListProjects(ctx, o)
	})
}

func listPage[T any](ctx context.Context, c *Client, path string, opts ListOpts) (Page[T], error) {
	var out Page[T]
	err := c.do(ctx, "GET", path, opts.values(), nil, &out)
	return out, err
}

// iterPages mirrors the Python iter_* helpers: after the first page only
// the cursor is forwarded (pattern+limit are baked into it server-side).
func iterPages[T any](ctx context.Context, opts ListOpts, list func(context.Context, ListOpts) (Page[T], error)) iter.Seq2[T, error] {
	return func(yield func(T, error) bool) {
		for {
			page, err := list(ctx, opts)
			if err != nil {
				var zero T
				yield(zero, err)
				return
			}
			for _, obj := range page.Objects {
				if !yield(obj, nil) {
					return
				}
			}
			if page.Cursor == nil {
				return
			}
			opts = ListOpts{Cursor: *page.Cursor}
		}
	}
}
