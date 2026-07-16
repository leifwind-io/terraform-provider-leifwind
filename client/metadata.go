// SPDX-License-Identifier: MPL-2.0

package client

import (
	"context"
	"fmt"
	"iter"

	"github.com/google/uuid"
)

// objectIDCarrier is implemented by the metadata models; the backend
// contract guarantees object_id on every returned object.
type objectIDCarrier interface{ hasObjectID() bool }

// requireObjectID rejects a decoded response object whose object_id is
// absent — a contract-violating backend would otherwise panic the provider
// at the first dereference.
func requireObjectID(method, path string, v any) error {
	if c, ok := v.(objectIDCarrier); ok && !c.hasObjectID() {
		return fmt.Errorf("%s %s: server returned object without object_id", method, path)
	}
	return nil
}

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
	if err == nil {
		err = requireObjectID("POST", "/metadata/projects", out)
	}
	return out, err
}

// GetProject fetches one project; ErrNotFound covers missing AND cross-tenant.
func (s *MetadataService) GetProject(ctx context.Context, projectID uuid.UUID) (MetadataProject, error) {
	path := "/metadata/projects/" + projectID.String()
	var out MetadataProject
	err := s.c.do(ctx, "GET", path, nil, nil, &out)
	if err == nil {
		err = requireObjectID("GET", path, out)
	}
	return out, err
}

// DeleteProject deletes a project; entities/fields cascade server-side and
// the per-project schema is dropped.
func (s *MetadataService) DeleteProject(ctx context.Context, projectID uuid.UUID, opts ...WriteOption) error {
	return s.c.do(ctx, "DELETE", "/metadata/projects/"+projectID.String(), writeValues(opts), nil, nil)
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

// UpsertEntity creates or adopts an entity in e.ProjectID.
func (s *MetadataService) UpsertEntity(ctx context.Context, e MetadataEntity, opts ...WriteOption) (MetadataEntity, error) {
	path := "/metadata/projects/" + e.ProjectID.String() + "/entities"
	var out MetadataEntity
	err := s.c.do(ctx, "POST", path, writeValues(opts), e, &out)
	if err == nil {
		err = requireObjectID("POST", path, out)
	}
	return out, err
}

// GetEntity fetches one entity.
func (s *MetadataService) GetEntity(ctx context.Context, projectID, entityID uuid.UUID) (MetadataEntity, error) {
	path := "/metadata/projects/" + projectID.String() + "/entities/" + entityID.String()
	var out MetadataEntity
	err := s.c.do(ctx, "GET", path, nil, nil, &out)
	if err == nil {
		err = requireObjectID("GET", path, out)
	}
	return out, err
}

// DeleteEntity deletes an entity; its fields cascade server-side.
func (s *MetadataService) DeleteEntity(ctx context.Context, projectID, entityID uuid.UUID, opts ...WriteOption) error {
	return s.c.do(ctx, "DELETE",
		"/metadata/projects/"+projectID.String()+"/entities/"+entityID.String(),
		writeValues(opts), nil, nil)
}

// ListEntities returns one page of a project's entities.
func (s *MetadataService) ListEntities(ctx context.Context, projectID uuid.UUID, opts ListOpts) (Page[MetadataEntity], error) {
	return listPage[MetadataEntity](ctx, s.c, "/metadata/projects/"+projectID.String()+"/entities", opts)
}

// IterEntities auto-pages through a project's entities.
func (s *MetadataService) IterEntities(ctx context.Context, projectID uuid.UUID, opts ListOpts) iter.Seq2[MetadataEntity, error] {
	return iterPages(ctx, opts, func(ctx context.Context, o ListOpts) (Page[MetadataEntity], error) {
		return s.ListEntities(ctx, projectID, o)
	})
}

// UpsertField creates or adopts a field. Only Connection.FragmentName is
// mutable on an existing field; data_type/connection_type changes → ErrValidation.
func (s *MetadataService) UpsertField(ctx context.Context, f MetadataField, opts ...WriteOption) (MetadataField, error) {
	path := "/metadata/projects/" + f.ProjectID.String() + "/entities/" + f.EntityID.String() + "/fields"
	var out MetadataField
	err := s.c.do(ctx, "POST", path, writeValues(opts), f, &out)
	if err == nil {
		err = requireObjectID("POST", path, out)
	}
	return out, err
}

// GetField fetches one field.
func (s *MetadataService) GetField(ctx context.Context, projectID, entityID, fieldID uuid.UUID) (MetadataField, error) {
	path := "/metadata/projects/" + projectID.String() + "/entities/" + entityID.String() + "/fields/" + fieldID.String()
	var out MetadataField
	err := s.c.do(ctx, "GET", path, nil, nil, &out)
	if err == nil {
		err = requireObjectID("GET", path, out)
	}
	return out, err
}

// DeleteField deletes a field (drops the backing column server-side).
func (s *MetadataService) DeleteField(ctx context.Context, projectID, entityID, fieldID uuid.UUID, opts ...WriteOption) error {
	return s.c.do(ctx, "DELETE",
		"/metadata/projects/"+projectID.String()+"/entities/"+entityID.String()+"/fields/"+fieldID.String(),
		writeValues(opts), nil, nil)
}

// ListFields returns one page of an entity's fields (404 for bogus entities).
func (s *MetadataService) ListFields(ctx context.Context, projectID, entityID uuid.UUID, opts ListOpts) (Page[MetadataField], error) {
	return listPage[MetadataField](ctx, s.c,
		"/metadata/projects/"+projectID.String()+"/entities/"+entityID.String()+"/fields", opts)
}

// IterFields auto-pages through an entity's fields.
func (s *MetadataService) IterFields(ctx context.Context, projectID, entityID uuid.UUID, opts ListOpts) iter.Seq2[MetadataField, error] {
	return iterPages(ctx, opts, func(ctx context.Context, o ListOpts) (Page[MetadataField], error) {
		return s.ListFields(ctx, projectID, entityID, o)
	})
}

func listPage[T any](ctx context.Context, c *Client, path string, opts ListOpts) (Page[T], error) {
	var out Page[T]
	if err := c.do(ctx, "GET", path, opts.values(), nil, &out); err != nil {
		return out, err
	}
	for _, obj := range out.Objects {
		if err := requireObjectID("GET", path, obj); err != nil {
			return out, err
		}
	}
	return out, nil
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
