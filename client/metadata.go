// SPDX-License-Identifier: MPL-2.0

package client

import (
	"context"

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
