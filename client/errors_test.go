// SPDX-License-Identifier: MPL-2.0

package client

import (
	"errors"
	"testing"
)

func TestStatusToSentinel(t *testing.T) {
	cases := []struct {
		status int
		want   error
	}{
		{404, ErrNotFound},
		{409, ErrConflict},
		{422, ErrValidation},
		{400, ErrValidation},
		{401, ErrUnauthenticated},
	}
	for _, c := range cases {
		err := newAPIError("GET", "/metadata/projects", c.status, []byte(`{"detail":"boom"}`))
		if !errors.Is(err, c.want) {
			t.Errorf("status %d: not errors.Is(%v)", c.status, c.want)
		}
	}
	// unmapped status carries no sentinel
	if errors.Is(newAPIError("GET", "/x", 500, nil), ErrValidation) {
		t.Error("500 must not map to a sentinel")
	}
}

func TestDetailParsing(t *testing.T) {
	// FastAPI handler-raised: detail is a string
	e := newAPIError("GET", "/x", 404, []byte(`{"detail":"couldn't find a project with id: abc"}`))
	if e.Detail != "couldn't find a project with id: abc" {
		t.Fatalf("string detail: %q", e.Detail)
	}
	// pydantic validation: detail is an array — keep raw JSON
	e = newAPIError("POST", "/x", 422, []byte(`{"detail":[{"loc":["body","name"],"msg":"bad"}]}`))
	if e.Detail == "" || e.Detail[0] != '[' {
		t.Fatalf("array detail should keep raw JSON: %q", e.Detail)
	}
	// non-JSON body — keep as-is
	e = newAPIError("GET", "/x", 502, []byte("bad gateway"))
	if e.Detail != "bad gateway" {
		t.Fatalf("raw detail: %q", e.Detail)
	}
}

func TestAPIErrorMessage(t *testing.T) {
	e := newAPIError("DELETE", "/metadata/projects/abc", 404, []byte(`{"detail":"nope"}`))
	want := "DELETE /metadata/projects/abc: 404 nope"
	if e.Error() != want {
		t.Fatalf("got %q want %q", e.Error(), want)
	}
}
