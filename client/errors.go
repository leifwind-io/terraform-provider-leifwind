// SPDX-License-Identifier: MPL-2.0

package client

import (
	"encoding/json"
	"errors"
	"fmt"
)

var (
	// ErrNotFound wraps HTTP 404 (also returned for cross-tenant access).
	ErrNotFound = errors.New("not found")
	// ErrConflict wraps HTTP 409 (e.g. project name already in use).
	ErrConflict = errors.New("conflict")
	// ErrValidation wraps HTTP 400/422 (immutable-field changes, bad cursors).
	ErrValidation = errors.New("validation failed")
	// ErrUnauthenticated wraps HTTP 401.
	ErrUnauthenticated = errors.New("unauthenticated")
)

// APIError is the error returned for every non-2xx backend response.
type APIError struct {
	StatusCode int
	Detail     string
	Method     string
	Path       string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("%s %s: %d %s", e.Method, e.Path, e.StatusCode, e.Detail)
}

// Unwrap maps the status code onto the package sentinels for errors.Is.
func (e *APIError) Unwrap() error {
	switch e.StatusCode {
	case 404:
		return ErrNotFound
	case 409:
		return ErrConflict
	case 400, 422:
		return ErrValidation
	case 401:
		return ErrUnauthenticated
	}
	return nil
}

func newAPIError(method, path string, status int, body []byte) *APIError {
	detail := string(body)
	var probe struct {
		Detail json.RawMessage `json:"detail"`
	}
	if err := json.Unmarshal(body, &probe); err == nil && len(probe.Detail) > 0 {
		var s string
		if err := json.Unmarshal(probe.Detail, &s); err == nil {
			detail = s
		} else {
			detail = string(probe.Detail)
		}
	}
	return &APIError{StatusCode: status, Detail: detail, Method: method, Path: path}
}
