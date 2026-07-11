// SPDX-License-Identifier: MPL-2.0

package metadatares

import (
	"context"

	"github.com/google/uuid"

	"gitlab.com/leifwind/stream/terraform-provider-leifwind/client"
)

// findProjectByName resolves a project by EXACT name (the server pattern
// is a substring match, so filter client-side). nil = not found.
func findProjectByName(ctx context.Context, c *client.Client, name string) (*client.MetadataProject, error) {
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

// findEntityByName resolves an entity by EXACT name within a project.
func findEntityByName(ctx context.Context, c *client.Client, projectID uuid.UUID, name string) (*client.MetadataEntity, error) {
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
