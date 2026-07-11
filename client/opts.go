// SPDX-License-Identifier: MPL-2.0

package client

import (
	"net/url"
	"strconv"
)

// ListOpts controls list endpoints. Zero values are omitted from the query
// string (the backend rejects empty-string values on typed params). Limit
// must be ≤ 50 (backend MAX_PAGE_SIZE); the server default is 50.
type ListOpts struct {
	Limit   int
	Pattern string
	Cursor  string
}

func (o ListOpts) values() url.Values {
	v := url.Values{}
	if o.Limit > 0 {
		v.Set("limit", strconv.Itoa(o.Limit))
	}
	if o.Pattern != "" {
		v.Set("pattern", o.Pattern)
	}
	if o.Cursor != "" {
		v.Set("cursor", o.Cursor)
	}
	return v
}

// Page is one page of a cursor-paginated listing. Cursor == nil means the
// last page. The cursor is opaque and embeds pattern+limit — pass ONLY the
// cursor on follow-up calls.
type Page[T any] struct {
	Objects []T     `json:"objects"`
	Cursor  *string `json:"cursor"`
}

type writeSettings struct {
	dryRun bool
}

// WriteOption modifies write requests (upserts and deletes).
type WriteOption func(*writeSettings)

// DryRun makes the backend validate and then roll back the transaction.
func DryRun() WriteOption {
	return func(w *writeSettings) { w.dryRun = true }
}

func writeValues(opts []WriteOption) url.Values {
	var s writeSettings
	for _, o := range opts {
		o(&s)
	}
	v := url.Values{}
	if s.dryRun {
		v.Set("dry_run", "true")
	}
	return v
}
