// SPDX-License-Identifier: MPL-2.0

// Package lookup provides shared exact-name-resolution helpers for the
// metadata resources and data sources. The backend's list endpoints only
// support substring ("ILIKE") pattern matching, so exact-name lookups
// filter client-side.
package lookup

import (
	"context"

	"github.com/google/uuid"

	"gitlab.com/leifwind/stream/terraform-provider-leifwind/client"
)

// ProjectByName resolves a project by EXACT name (the server pattern
// is a substring match, so filter client-side). nil = not found.
func ProjectByName(ctx context.Context, c *client.Client, name string) (*client.MetadataProject, error) {
	for p, err := range c.Metadata.IterProjects(ctx, client.ListOpts{Pattern: name}) {
		if err != nil {
			return nil, err
		}
		if p.Name == name {
			return &p, nil
		}
	}
	return nil, nil
}

// EntityByName resolves an entity by EXACT name within a project.
func EntityByName(ctx context.Context, c *client.Client, projectID uuid.UUID, name string) (*client.MetadataEntity, error) {
	for e, err := range c.Metadata.IterEntities(ctx, projectID, client.ListOpts{Pattern: name}) {
		if err != nil {
			return nil, err
		}
		if e.Name == name {
			return &e, nil
		}
	}
	return nil, nil
}

// FieldByName resolves a field by EXACT name within an entity.
func FieldByName(ctx context.Context, c *client.Client, projectID, entityID uuid.UUID, name string) (*client.MetadataField, error) {
	for f, err := range c.Metadata.IterFields(ctx, projectID, entityID, client.ListOpts{Pattern: name}) {
		if err != nil {
			return nil, err
		}
		if f.Name == name {
			return &f, nil
		}
	}
	return nil, nil
}
