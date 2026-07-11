// SPDX-License-Identifier: MPL-2.0

// Package client is a standalone Go client for the leifwind metadata API.
// It mirrors the semantics of the backend's Python client: upsert-style
// POSTs resolved by object_id or natural unique_key, cursor pagination,
// and ZITADEL bearer-token authentication.
package client
