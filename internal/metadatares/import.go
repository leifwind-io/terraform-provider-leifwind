// SPDX-License-Identifier: MPL-2.0

package metadatares

import (
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// parseImportUUIDs parses "<uuid>[/<uuid>[/<uuid>]]" import IDs.
func parseImportUUIDs(raw string, parts int) ([]uuid.UUID, error) {
	segs := strings.Split(raw, "/")
	if len(segs) != parts {
		return nil, fmt.Errorf("import ID must have %d '/'-separated UUID segments, got %d", parts, len(segs))
	}
	out := make([]uuid.UUID, 0, parts)
	for _, s := range segs {
		id, err := uuid.Parse(s)
		if err != nil {
			return nil, fmt.Errorf("import ID segment %q is not a UUID: %w", s, err)
		}
		out = append(out, id)
	}
	return out, nil
}
