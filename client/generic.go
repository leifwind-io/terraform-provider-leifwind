// SPDX-License-Identifier: MPL-2.0

package client

import (
	"context"
	"net/url"

	"github.com/google/uuid"
)

// GenericService covers the /generic data-plane read endpoints the
// provider needs (fragment schema names).
type GenericService struct {
	c *Client
}

// ListEntityFragments returns the fragment names of an entity (derived from
// its FRAGMENT-connection fields). entityName accepts a name or UUID string.
func (s *GenericService) ListEntityFragments(ctx context.Context, projectID uuid.UUID, entityName string) ([]string, error) {
	var out struct {
		Fragments []string `json:"fragments"`
	}
	// PathEscape: entity names are user input; convention for all name-typed
	// path segments.
	err := s.c.do(ctx, "GET",
		"/generic/projects/"+projectID.String()+"/schemas/entities/"+url.PathEscape(entityName)+"/fragments",
		nil, nil, &out)
	return out.Fragments, err
}
