# Pre-tag Hardening (LW-111 client + LW-113 leifwindtest) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Land every API-shape-freezing fix in the public `client` and `leifwindtest` packages before the first `client/vX.Y.Z` tag: retry/panic hardening, token-endpoint validation, empty-body tolerance, runtime version reporting, an ObjectID contract guard, `SetAccessTokenLifetime(time.Duration)`, doc hygiene, and HTTP timeouts.

**Architecture:** Two independent workstreams inside the `client` Go module (`client/` at repo root, its own go.mod, wired into the root module via `go.work` + replace). Workstream A hardens the HTTP client request path (`client.go`, `retry.go`, `auth.go`, `metadata.go`, `models.go`); Workstream B cleans the public test fixture (`client/leifwindtest/`). No provider behavior changes except: a contract-violating backend response now surfaces as a Terraform diagnostic instead of a provider panic.

**Tech Stack:** Go 1.25, `net/http/httptest` for new unit tests, `runtime/debug.ReadBuildInfo` for version stamping, testcontainers (existing, untouched) for the container-based suites.

**Source spec:** `docs/superpowers/specs/2026-07-16-lw-111-113-pretag-hardening-design.md` (commit `feaf070`).

## Global Constraints

- Everything lands **before the first `client/vX.Y.Z` tag** — breaking API changes (removing `const Version`, changing `SetAccessTokenLifetime`'s signature) are explicitly allowed under this gate.
- Client module path (used verbatim in Task 5): `gitlab.com/leifwind/stream/terraform-provider-leifwind/client`.
- Every new `.go` file starts with `// SPDX-License-Identifier: MPL-2.0` followed by a blank line.
- Conventional commit messages, scope in parens (`fix(client): …`, `docs(leifwindtest): …`), trailer `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>`.
- **Out of scope (do NOT do):** splitting `leifwindtest` into its own module; pinning `BackendImage` off `:edge` (LW-68); `UserToken` per-org idempotency (LW-110); client *test* infra hardening (LW-112); exposing the human user ID from `UserToken`; touching `BackendImage`'s `TODO(LW-68)` comment or the `zitadel#5219` reference.
- New exported sentinel errors: none (YAGNI — the ObjectID guard returns a plain `fmt.Errorf`).

## Test-running reality (read before Task 1)

Both test binaries boot Docker containers in `TestMain` **eagerly** (`client/client_test.go` and `client/leifwindtest/main_test.go` each call `leifwindtest.StartMain` before any test runs, ~60–90s). The new httptest-based tests never touch that stack, but they live in the same binaries, so:

- Every `go test` invocation below pays the stack boot if Docker is available. **Batch your red/green checks where possible** (write test + run once; implement + run once).
- Without Docker the boot fails fast, `stackErr`/`mainStackErr` is set, stack-using tests fail individually — the new httptest tests still pass. That is acceptable for red/green cycles; the full-suite gate (Task 9) needs Docker.
- Full suite: `make test` from repo root (`cd client && go test ./... -v -timeout 20m -p 1`). Lint: `make lint` (golangci-lint over both modules).

All commands below run from the repo root unless stated otherwise.

---

## Workstream A — `client` package (LW-111)

### Task 1: ObjectID post-decode guard (spec A5) + keyed wire literals (spec A6)

A contract-violating 200 (missing `object_id`) currently flows a nil `*uuid.UUID` into ~10 provider call-sites that dereference it (including the plural data sources, off List results). Add one central post-decode check; while in `models.go`, convert the three positional wire-struct literals to keyed form.

**Files:**
- Modify: `client/models.go` (add three `hasObjectID` methods; keyed literals at lines 103, 134, 171)
- Modify: `client/metadata.go` (add `objectIDCarrier` + `requireObjectID`; call in the six `Upsert*/Get*` methods and `listPage`)
- Create: `client/objectid_test.go`

**Interfaces:**
- Consumes: existing `MetadataService` methods, `Page[T]`, `ListOpts` (all pre-existing).
- Produces: unexported `requireObjectID(method, path string, v any) error` and interface `objectIDCarrier interface{ hasObjectID() bool }` in package `client` (`metadata.go`); `hasObjectID()` value-receiver methods on `MetadataProject`, `MetadataEntity`, `MetadataField`. No exported API change. Error text (used by tests): `"<METHOD> <path>: server returned object without object_id"`.

- [ ] **Step 1: Write the failing test**

Create `client/objectid_test.go`:

```go
// SPDX-License-Identifier: MPL-2.0

package client_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"gitlab.com/leifwind/stream/terraform-provider-leifwind/client"
)

// noObjectIDClient answers every request 200 with a body whose object_id is
// absent — a contract-violating backend.
func noObjectIDClient(t *testing.T) *client.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		last := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
		if _, err := uuid.Parse(last); err == nil || r.Method == http.MethodPost {
			// single-object response (Get*/Upsert*) without object_id
			_, _ = w.Write([]byte(`{"metadata_type":"metadata_project","name":"p"}`))
			return
		}
		// list response whose objects lack object_id
		_, _ = w.Write([]byte(`{"objects":[{"metadata_type":"metadata_project","name":"p"}],"cursor":null}`))
	}))
	t.Cleanup(srv.Close)
	c, err := client.New(srv.URL, client.WithRetry(client.RetryConfig{MaxAttempts: 1}))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestMissingObjectIDReturnsErrorNotPanic(t *testing.T) {
	t.Parallel()
	c := noObjectIDClient(t)
	ctx := context.Background()
	id := uuid.New()

	calls := map[string]func() error{
		"UpsertProject": func() error {
			_, err := c.Metadata.UpsertProject(ctx, client.MetadataProject{Name: "p"})
			return err
		},
		"GetProject": func() error { _, err := c.Metadata.GetProject(ctx, id); return err },
		"ListProjects": func() error {
			_, err := c.Metadata.ListProjects(ctx, client.ListOpts{})
			return err
		},
		"UpsertEntity": func() error {
			_, err := c.Metadata.UpsertEntity(ctx, client.MetadataEntity{ProjectID: id, Name: "e"})
			return err
		},
		"GetEntity": func() error { _, err := c.Metadata.GetEntity(ctx, id, id); return err },
		"ListEntities": func() error {
			_, err := c.Metadata.ListEntities(ctx, id, client.ListOpts{})
			return err
		},
		"UpsertField": func() error {
			_, err := c.Metadata.UpsertField(ctx, client.MetadataField{
				ProjectID: id, EntityID: id, Name: "f",
				Config:     client.FieldConfig{DataType: client.DataTypeText},
				Connection: client.Connection{Type: client.ConnectionKey},
			})
			return err
		},
		"GetField": func() error { _, err := c.Metadata.GetField(ctx, id, id, id); return err },
		"ListFields": func() error {
			_, err := c.Metadata.ListFields(ctx, id, id, client.ListOpts{})
			return err
		},
	}
	for name, call := range calls {
		if err := call(); err == nil || !strings.Contains(err.Error(), "object without object_id") {
			t.Errorf("%s: want object_id contract error, got %v", name, err)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd client && go test ./ -run TestMissingObjectIDReturnsErrorNotPanic -count=1`
Expected: FAIL — every subtest reports `want object_id contract error, got <nil>` (today the nil ObjectID is silently accepted). The TestMain stack boot (or its fast failure without Docker) precedes the run.

- [ ] **Step 3: Implement the guard**

In `client/models.go`, add directly after the `MetadataField` `UnmarshalJSON` method (end of file):

```go
func (p MetadataProject) hasObjectID() bool { return p.ObjectID != nil }
func (e MetadataEntity) hasObjectID() bool  { return e.ObjectID != nil }
func (f MetadataField) hasObjectID() bool   { return f.ObjectID != nil }
```

In `client/metadata.go`, add `"fmt"` to the imports, then add above `type MetadataService`:

```go
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
```

Rewrite the six `Upsert*/Get*` methods to check after a successful `do`, capturing composed paths in a variable. Exact new bodies:

```go
func (s *MetadataService) UpsertProject(ctx context.Context, p MetadataProject, opts ...WriteOption) (MetadataProject, error) {
	var out MetadataProject
	err := s.c.do(ctx, "POST", "/metadata/projects", writeValues(opts), p, &out)
	if err == nil {
		err = requireObjectID("POST", "/metadata/projects", out)
	}
	return out, err
}

func (s *MetadataService) GetProject(ctx context.Context, projectID uuid.UUID) (MetadataProject, error) {
	path := "/metadata/projects/" + projectID.String()
	var out MetadataProject
	err := s.c.do(ctx, "GET", path, nil, nil, &out)
	if err == nil {
		err = requireObjectID("GET", path, out)
	}
	return out, err
}

func (s *MetadataService) UpsertEntity(ctx context.Context, e MetadataEntity, opts ...WriteOption) (MetadataEntity, error) {
	path := "/metadata/projects/" + e.ProjectID.String() + "/entities"
	var out MetadataEntity
	err := s.c.do(ctx, "POST", path, writeValues(opts), e, &out)
	if err == nil {
		err = requireObjectID("POST", path, out)
	}
	return out, err
}

func (s *MetadataService) GetEntity(ctx context.Context, projectID, entityID uuid.UUID) (MetadataEntity, error) {
	path := "/metadata/projects/" + projectID.String() + "/entities/" + entityID.String()
	var out MetadataEntity
	err := s.c.do(ctx, "GET", path, nil, nil, &out)
	if err == nil {
		err = requireObjectID("GET", path, out)
	}
	return out, err
}

func (s *MetadataService) UpsertField(ctx context.Context, f MetadataField, opts ...WriteOption) (MetadataField, error) {
	path := "/metadata/projects/" + f.ProjectID.String() + "/entities/" + f.EntityID.String() + "/fields"
	var out MetadataField
	err := s.c.do(ctx, "POST", path, writeValues(opts), f, &out)
	if err == nil {
		err = requireObjectID("POST", path, out)
	}
	return out, err
}

func (s *MetadataService) GetField(ctx context.Context, projectID, entityID, fieldID uuid.UUID) (MetadataField, error) {
	path := "/metadata/projects/" + projectID.String() + "/entities/" + entityID.String() + "/fields/" + fieldID.String()
	var out MetadataField
	err := s.c.do(ctx, "GET", path, nil, nil, &out)
	if err == nil {
		err = requireObjectID("GET", path, out)
	}
	return out, err
}
```

Keep each method's existing doc comment unchanged. Rewrite `listPage` (covers `List*` and, via them, `Iter*`):

```go
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
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd client && go test ./ -run TestMissingObjectIDReturnsErrorNotPanic -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add client/models.go client/metadata.go client/objectid_test.go
git commit -m "fix(client): reject contract-violating responses missing object_id (LW-111)"
```

- [ ] **Step 6: Keyed wire-struct literals (A6)**

In `client/models.go`, replace the three positional composite literals:

`MetadataProject.MarshalJSON` (line ~103):

```go
// MarshalJSON marshals MetadataProject to JSON.
func (p MetadataProject) MarshalJSON() ([]byte, error) {
	return json.Marshal(projectWire{
		MetadataType: "metadata_project",
		ObjectID:     p.ObjectID,
		Name:         p.Name,
		UniqueKey:    p.UniqueKey,
	})
}
```

`MetadataEntity.MarshalJSON` (line ~134):

```go
// MarshalJSON marshals MetadataEntity to JSON.
func (e MetadataEntity) MarshalJSON() ([]byte, error) {
	return json.Marshal(entityWire{
		MetadataType: "metadata_entity",
		ObjectID:     e.ObjectID,
		ProjectID:    e.ProjectID,
		Name:         e.Name,
		UniqueKey:    e.UniqueKey,
	})
}
```

`MetadataField.MarshalJSON` (line ~171):

```go
// MarshalJSON marshals MetadataField to JSON.
func (f MetadataField) MarshalJSON() ([]byte, error) {
	return json.Marshal(fieldWire{
		MetadataType: "metadata_field",
		ObjectID:     f.ObjectID,
		ProjectID:    f.ProjectID,
		EntityID:     f.EntityID,
		Name:         f.Name,
		Config:       f.Config,
		Connection:   f.Connection,
		UniqueKey:    f.UniqueKey,
	})
}
```

- [ ] **Step 7: Verify marshaling is unchanged, then commit**

Run: `cd client && go test ./ -run 'TestProject|TestEntity|TestField|TestConnection' -count=1 && go vet ./...`
Expected: PASS (existing `models_test.go` covers the wire shapes)

```bash
git add client/models.go
git commit -m "refactor(client): keyed composite literals for wire structs (LW-111)"
```

---

### Task 2: Retry hardening — config normalization + permanent-error classification (spec A1 + A6)

Two defects: (a) a negative `MaxBackoff` reaches `rand.Int64N` with a non-positive argument → panic; (b) `doRetry` retries deterministic failures — worst case, a 2xx whose body fails to decode **re-executes the request** up to `MaxAttempts` times.

**Files:**
- Modify: `client/client.go` (normalize retry config in `New`; wrap deterministic failures in `doOnce`)
- Modify: `client/retry.go` (add `permanentError`; classification in `doRetry`)
- Create: `client/retry_unit_test.go`

**Interfaces:**
- Consumes: `RetryConfig`, `doOnce`, `doRetry`, `sleepBackoff`, `APIError` (all pre-existing).
- Produces: unexported `type permanentError struct{ err error }` with `Error()`/`Unwrap()` and constructor `func permanent(err error) error` in `retry.go`. **Task 4 reuses `permanent(...)`.** `errors.Is`/`errors.As` still see through the wrapper. Token-source failures are deliberately NOT marked permanent (deterministic credential failures already surface as non-retried 4xx `APIError`s from `ccTokenSource`; transient token-endpoint transport failures must stay retryable).

- [ ] **Step 1: Write the failing tests**

Create `client/retry_unit_test.go`:

```go
// SPDX-License-Identifier: MPL-2.0

package client_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"gitlab.com/leifwind/stream/terraform-provider-leifwind/client"
)

// countingServer returns a server running handler and a counter of requests
// that actually reached the wire.
func countingServer(t *testing.T, handler func(w http.ResponseWriter, r *http.Request)) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		handler(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv, &calls
}

func TestNegativeMaxBackoffDoesNotPanic(t *testing.T) {
	t.Parallel()
	srv, calls := countingServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	c, err := client.New(srv.URL, client.WithRetry(client.RetryConfig{MaxAttempts: 3, MaxBackoff: -1}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Metadata.ListProjects(context.Background(), client.ListOpts{}); err == nil {
		t.Fatal("expected 500 error")
	}
	if got := calls.Load(); got != 3 {
		t.Fatalf("want 3 attempts against a 5xx server, got %d", got)
	}
}

func TestMalformedResponseBodyIsNotRetried(t *testing.T) {
	t.Parallel()
	srv, calls := countingServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{not json`))
	})
	c, err := client.New(srv.URL, client.WithRetry(client.RetryConfig{
		MaxAttempts: 5, MinBackoff: time.Millisecond, MaxBackoff: time.Millisecond}))
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.Metadata.ListProjects(context.Background(), client.ListOpts{})
	if err == nil || !strings.Contains(err.Error(), "decode") {
		t.Fatalf("want decode error, got %v", err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("a 2xx decode failure must not re-execute the request: %d attempts", got)
	}
}

func TestUnmarshalableRequestBodyIsNotRetried(t *testing.T) {
	// Regression guard for spec test 3: MetadataField.MarshalJSON fails for
	// FRAGMENT without FragmentName — a deterministic encode error that must
	// never reach the wire. (The retry classification's red/green cycle is
	// driven by TestMalformedResponseBodyIsNotRetried; this test also passes
	// pre-change because the encode error fires before any request.)
	t.Parallel()
	srv, calls := countingServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	})
	c, err := client.New(srv.URL, client.WithRetry(client.RetryConfig{
		MaxAttempts: 5, MinBackoff: time.Millisecond, MaxBackoff: time.Millisecond}))
	if err != nil {
		t.Fatal(err)
	}
	id := uuid.New()
	_, err = c.Metadata.UpsertField(context.Background(), client.MetadataField{
		ProjectID: id, EntityID: id, Name: "f",
		Config:     client.FieldConfig{DataType: client.DataTypeText},
		Connection: client.Connection{Type: client.ConnectionFragment}, // no FragmentName → marshal error
	})
	if err == nil || !strings.Contains(err.Error(), "encode") {
		t.Fatalf("want encode error, got %v", err)
	}
	if got := calls.Load(); got != 0 {
		t.Fatalf("an encode failure must issue zero requests, got %d", got)
	}
}
```

- [ ] **Step 2: Run tests to verify the two real failures**

Run: `cd client && go test ./ -run 'TestNegativeMaxBackoff|TestMalformedResponseBody|TestUnmarshalableRequestBody' -count=1`
Expected: `TestNegativeMaxBackoffDoesNotPanic` FAILS with a panic (`rand.Int64N` invalid argument); `TestMalformedResponseBodyIsNotRetried` FAILS with 5 attempts; `TestUnmarshalableRequestBodyIsNotRetried` passes (documented above).

- [ ] **Step 3: Implement**

In `client/client.go`, `New`, insert after the options loop (`for _, o := range opts { o(&s) }`) and before `ua := ...`:

```go
	// Normalize the retry config once: sleepBackoff must never see a
	// negative bound (rand.Int64N panics on non-positive arguments).
	if s.retry.MaxAttempts < 1 {
		s.retry.MaxAttempts = 1
	}
	if s.retry.MinBackoff < 0 {
		s.retry.MinBackoff = 0
	}
	if s.retry.MaxBackoff < s.retry.MinBackoff {
		s.retry.MaxBackoff = s.retry.MinBackoff
	}
```

(The `maxAttempts < 1` re-check in `doRetry` becomes redundant but stays as a cheap invariant — do not remove it.)

In `client/client.go`, `doOnce`, wrap the three deterministic failure stages:

```go
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return permanent(fmt.Errorf("%s %s: encode: %w", method, path, err))
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rdr)
	if err != nil {
		return permanent(fmt.Errorf("%s %s: build request: %w", method, path, err))
	}
```

and the decode branch at the bottom:

```go
	if out != nil {
		if err := json.Unmarshal(rb, out); err != nil {
			return permanent(fmt.Errorf("%s %s: decode: %w", method, path, err))
		}
	}
```

(The `read body` error stays unmarked — a body-read failure can be a mid-stream connection drop, which is transient. Token-source errors also stay unmarked, see Interfaces.)

In `client/retry.go`, add above `doRetry`:

```go
// permanentError marks a failure as deterministic: retrying cannot change
// the outcome (request-body encode, request construction, 2xx decode).
type permanentError struct{ err error }

func (p *permanentError) Error() string { return p.err.Error() }
func (p *permanentError) Unwrap() error { return p.err }

func permanent(err error) error { return &permanentError{err: err} }
```

In `doRetry`, extend the classification after the existing `APIError` block so the loop body reads:

```go
		err := c.doOnce(ctx, method, path, query, body, out)
		if err == nil {
			return nil
		}
		var apiErr *APIError
		if errors.As(err, &apiErr) {
			if method == http.MethodDelete && attempt > 1 && apiErr.StatusCode == 404 {
				return nil
			}
			if apiErr.StatusCode < 500 {
				return err
			}
		} else {
			var perm *permanentError
			if errors.As(err, &perm) {
				return err
			}
		}
```

(Everything else in `doRetry` — `lastErr`, the attempt bound, `sleepBackoff` — is unchanged. Net policy: retry only (i) `APIError` ≥ 500, (ii) unmarked non-`APIError` errors, i.e. transport failures.)

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd client && go test ./ -run 'TestNegativeMaxBackoff|TestMalformedResponseBody|TestUnmarshalableRequestBody' -count=1 && go vet ./...`
Expected: all three PASS.

- [ ] **Step 5: Commit**

```bash
git add client/client.go client/retry.go client/retry_unit_test.go
git commit -m "fix(client): normalize retry config, stop retrying deterministic failures (LW-111)"
```

---

### Task 3: Token-endpoint response handling (spec A2)

`ccTokenSource.Token` discards the body-read error and happily caches an empty `access_token` from a 200, producing `Authorization: Bearer ` and an opaque backend 401.

**Files:**
- Modify: `client/auth.go` (`ccTokenSource.Token`, lines ~110–127)
- Create: `client/auth_unit_test.go`

**Interfaces:**
- Consumes: `ClientCredentials` constructor (pre-existing; `issuer` pointed at an httptest server in tests).
- Produces: no API change. New error texts: `"token endpoint: read body: <cause>"` and `"token endpoint: 200 response without access_token"`.

- [ ] **Step 1: Write the failing tests**

Create `client/auth_unit_test.go`:

```go
// SPDX-License-Identifier: MPL-2.0

package client_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"gitlab.com/leifwind/stream/terraform-provider-leifwind/client"
)

func TestTokenEndpointEmptyAccessTokenIsErrorAndNotCached(t *testing.T) {
	t.Parallel()
	for _, body := range []string{`{}`, `{"access_token": ""}`} {
		var calls atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(body))
		}))
		ts := client.ClientCredentials(srv.URL, "id", "secret")
		if _, err := ts.Token(context.Background()); err == nil || !strings.Contains(err.Error(), "access_token") {
			t.Errorf("body %s: want access_token error, got %v", body, err)
		}
		// nothing cached: the next call must hit the endpoint again
		_, _ = ts.Token(context.Background())
		if got := calls.Load(); got != 2 {
			t.Errorf("body %s: want a re-fetch after the failure, got %d calls", body, got)
		}
		srv.Close()
	}
}

func TestTokenEndpointBodyReadErrorIsWrapped(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "1000") // promise more than we send
		_, _ = w.Write([]byte(`{"access_`))
	}))
	t.Cleanup(srv.Close)
	ts := client.ClientCredentials(srv.URL, "id", "secret")
	if _, err := ts.Token(context.Background()); err == nil || !strings.Contains(err.Error(), "read body") {
		t.Fatalf("want wrapped read error, got %v", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd client && go test ./ -run TestTokenEndpoint -count=1`
Expected: FAIL — first test gets `nil` error (empty token cached, second call served from cache → 1 call); second test's error is a bare JSON syntax error without `read body`.

- [ ] **Step 3: Implement**

In `client/auth.go`, `Token`, replace `body, _ := io.ReadAll(resp.Body)` with:

```go
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("token endpoint: read body: %w", err)
	}
```

and after the `if out.ExpiresIn == 0 { out.ExpiresIn = 3600 }` default, insert before the cache assignment:

```go
	if out.AccessToken == "" {
		return "", fmt.Errorf("token endpoint: 200 response without access_token")
	}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd client && go test ./ -run TestTokenEndpoint -count=1 && go vet ./...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add client/auth.go client/auth_unit_test.go
git commit -m "fix(client): reject token-endpoint 200s without access_token, wrap read errors (LW-111)"
```

---

### Task 4: Empty 2xx body tolerance + nil-out deletes (spec A3)

`doOnce` unconditionally unmarshals when `out != nil`, so a future backend 204 (or empty 200) breaks every such call — including all three `Delete*` methods, which pass a never-read `Detail` struct.

**Files:**
- Modify: `client/client.go` (`doOnce` decode branch — the one Task 2 wrapped)
- Modify: `client/metadata.go` (`DeleteProject`/`DeleteEntity`/`DeleteField` pass `nil` out)
- Create: `client/emptybody_test.go`

**Interfaces:**
- Consumes: `permanent(...)` from Task 2.
- Produces: no API change. Semantics: empty 2xx body + non-nil `out` → no error, `out` stays zero-valued.

- [ ] **Step 1: Write the failing test**

Create `client/emptybody_test.go`:

```go
// SPDX-License-Identifier: MPL-2.0

package client_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"gitlab.com/leifwind/stream/terraform-provider-leifwind/client"
)

func TestEmptyBody2xxTolerated(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusOK) // 200 with empty body
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)
	c, err := client.New(srv.URL, client.WithRetry(client.RetryConfig{MaxAttempts: 1}))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	// 204 with a non-nil out: no error, out stays zero-valued.
	page, err := c.Metadata.ListProjects(ctx, client.ListOpts{})
	if err != nil {
		t.Fatalf("204 with non-nil out must not error: %v", err)
	}
	if len(page.Objects) != 0 || page.Cursor != nil {
		t.Fatalf("out must stay zero-valued, got %+v", page)
	}

	// Delete* against an empty-body 200 succeeds.
	if err := c.Metadata.DeleteProject(ctx, uuid.New()); err != nil {
		t.Fatalf("DeleteProject: %v", err)
	}
	if err := c.Metadata.DeleteEntity(ctx, uuid.New(), uuid.New()); err != nil {
		t.Fatalf("DeleteEntity: %v", err)
	}
	if err := c.Metadata.DeleteField(ctx, uuid.New(), uuid.New(), uuid.New()); err != nil {
		t.Fatalf("DeleteField: %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd client && go test ./ -run TestEmptyBody2xxTolerated -count=1`
Expected: FAIL with `204 with non-nil out must not error: GET /metadata/projects: decode: unexpected end of JSON input`.

- [ ] **Step 3: Implement**

In `client/client.go`, `doOnce`, change the decode branch (post-Task-2 state shown) to skip empty bodies:

```go
	if out != nil && len(rb) > 0 {
		if err := json.Unmarshal(rb, out); err != nil {
			return permanent(fmt.Errorf("%s %s: decode: %w", method, path, err))
		}
	}
```

In `client/metadata.go`, replace the three delete bodies (the `Detail` struct is dead code — keep each doc comment):

```go
// DeleteProject deletes a project; entities/fields cascade server-side and
// the per-project schema is dropped.
func (s *MetadataService) DeleteProject(ctx context.Context, projectID uuid.UUID, opts ...WriteOption) error {
	return s.c.do(ctx, "DELETE", "/metadata/projects/"+projectID.String(), writeValues(opts), nil, nil)
}
```

```go
// DeleteEntity deletes an entity; its fields cascade server-side.
func (s *MetadataService) DeleteEntity(ctx context.Context, projectID, entityID uuid.UUID, opts ...WriteOption) error {
	return s.c.do(ctx, "DELETE",
		"/metadata/projects/"+projectID.String()+"/entities/"+entityID.String(),
		writeValues(opts), nil, nil)
}
```

```go
// DeleteField deletes a field (drops the backing column server-side).
func (s *MetadataService) DeleteField(ctx context.Context, projectID, entityID, fieldID uuid.UUID, opts ...WriteOption) error {
	return s.c.do(ctx, "DELETE",
		"/metadata/projects/"+projectID.String()+"/entities/"+entityID.String()+"/fields/"+fieldID.String(),
		writeValues(opts), nil, nil)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd client && go test ./ -run TestEmptyBody2xxTolerated -count=1 && go vet ./...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add client/client.go client/metadata.go client/emptybody_test.go
git commit -m "fix(client): tolerate empty 2xx bodies, drop dead Detail out-structs from deletes (LW-111)"
```

---

### Task 5: Runtime version reporting (spec A4)

`const Version = "0.1.0-dev"` has no release hook — the tagged `v0.1.0` module would still report `0.1.0-dev`. Replace it with a cached function over `debug.ReadBuildInfo`. Removing the exported const is a pre-tag API change, allowed under the gate (nothing else in the repo references `client.Version` — verified by grep).

**Files:**
- Create: `client/version.go`
- Modify: `client/client.go` (delete `const Version` + its comment, lines 17–18; use `Version()` at line ~73)
- Create: `client/version_test.go` (internal — `package client` — to reach `versionFromBuildInfo`; existing internal test files like `models_test.go` set the precedent)

**Interfaces:**
- Consumes: nothing from other tasks.
- Produces: exported `func Version() string` (replaces `const Version`); unexported `func versionFromBuildInfo(bi *debug.BuildInfo) string` and `const modulePath = "gitlab.com/leifwind/stream/terraform-provider-leifwind/client"`.

- [ ] **Step 1: Write the failing test**

Create `client/version_test.go`:

```go
// SPDX-License-Identifier: MPL-2.0

package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"regexp"
	"runtime/debug"
	"testing"
)

func TestVersionFromBuildInfo(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		bi   *debug.BuildInfo
		want string
	}{
		{"tagged dependency",
			&debug.BuildInfo{Deps: []*debug.Module{{Path: modulePath, Version: "v0.1.0"}}},
			"v0.1.0"},
		{"main module (module's own tests)",
			&debug.BuildInfo{Main: debug.Module{Path: modulePath, Version: "(devel)"}},
			"dev"},
		{"replaced dep (in-repo go.work build)",
			&debug.BuildInfo{Deps: []*debug.Module{{
				Path: modulePath, Version: "v0.0.0-00010101000000-000000000000",
				Replace: &debug.Module{Path: modulePath, Version: "(devel)"},
			}}},
			"dev"},
		{"module absent from build info", &debug.BuildInfo{}, "dev"},
	}
	for _, c := range cases {
		if got := versionFromBuildInfo(c.bi); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestVersionStampsUserAgent(t *testing.T) {
	t.Parallel()
	if Version() == "" {
		t.Fatal("Version() must be non-empty")
	}
	var ua string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ua = r.Header.Get("User-Agent")
		_, _ = w.Write([]byte(`{"objects":[],"cursor":null}`))
	}))
	t.Cleanup(srv.Close)
	c, err := New(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Metadata.ListProjects(context.Background(), ListOpts{}); err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`^terraform-provider-leifwind-client/\S+$`).MatchString(ua) {
		t.Fatalf("User-Agent = %q, want ^terraform-provider-leifwind-client/<version>", ua)
	}
}
```

- [ ] **Step 2: Verify it fails to compile**

Run: `cd client && go test ./ -run TestVersion -count=1`
Expected: compile error — `undefined: modulePath`, `undefined: versionFromBuildInfo`, and `Version` (a const) is not callable.

- [ ] **Step 3: Implement**

Create `client/version.go`:

```go
// SPDX-License-Identifier: MPL-2.0

package client

import (
	"runtime/debug"
	"sync"
)

// modulePath must match this module's declared path — it is how Version
// finds our own entry in the consumer's build info.
const modulePath = "gitlab.com/leifwind/stream/terraform-provider-leifwind/client"

var cachedVersion = sync.OnceValue(func() string {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return "dev"
	}
	return versionFromBuildInfo(bi)
})

// Version reports this module's version as recorded in the running binary's
// build info: the module tag (e.g. "v0.1.0") when consumed as a dependency,
// "dev" for in-repo go.work builds and other unstamped binaries.
func Version() string { return cachedVersion() }

func versionFromBuildInfo(bi *debug.BuildInfo) string {
	v := ""
	if bi.Main.Path == modulePath {
		v = bi.Main.Version
	}
	for _, dep := range bi.Deps {
		if dep.Path != modulePath {
			continue
		}
		v = dep.Version
		if dep.Replace != nil {
			v = dep.Replace.Version // local replace / go.work: "(devel)" or ""
		}
	}
	if v == "" || v == "(devel)" {
		return "dev"
	}
	return v
}
```

In `client/client.go`, delete lines 17–18:

```go
// Version is stamped into the default User-Agent.
const Version = "0.1.0-dev"
```

and change the `New` line `ua := "terraform-provider-leifwind-client/" + Version` to:

```go
	ua := "terraform-provider-leifwind-client/" + Version()
```

- [ ] **Step 4: Run tests, build both modules**

Run: `cd client && go test ./ -run TestVersion -count=1 && go vet ./... && cd .. && go build ./...`
Expected: PASS; both modules build (the provider consumes the client via go.work and would fail here if anything still referenced the const).

- [ ] **Step 5: Commit**

```bash
git add client/version.go client/client.go client/version_test.go
git commit -m "feat(client)!: resolve Version() from build info instead of a hardcoded const (LW-111)"
```

---

## Workstream B — `leifwindtest` package (LW-113)

### Task 6: Doc-hygiene sweep (spec B1)

Godoc for this public package renders on pkg.go.dev; remove internal task/spec vocabulary while keeping the genuinely valuable content (ZITADEL v4.15.3 deviations, feasibility facts) self-contained. Docs/message-text only — no behavior change.

**Files:**
- Modify: `client/leifwindtest/stack.go` (lines ~41, 50–51)
- Modify: `client/leifwindtest/usertoken.go` (doc header lines ~13–18; error message line ~142)
- Modify: `client/leifwindtest/oidc_settings.go` (feasibility note, lines ~28–31)

**Interfaces:** none — comments and one `t.Fatalf` string. Leave untouched: `BackendImage`'s `TODO(LW-68)` and the `zitadel/zitadel#5219` reference (public issue, correct to cite).

- [ ] **Step 1: Apply the five edits**

`stack.go` — `WithToxiproxy` doc:

```go
// WithToxiproxy routes ProxiedBackendURL through a toxiproxy container
// for fault injection.
func WithToxiproxy() StackOption {
```

`stack.go` — `Stack` fields:

```go
	BackendURL        string // set by startBackend
	ProxiedBackendURL string // set by WithToxiproxy
```

`usertoken.go` — replace the first two doc paragraphs (everything above `//  1. The v4.15.3 image ships…`) with:

```go
// UserToken mints a genuine delegated user token via RFC 8693 token
// exchange (user_id subject type): sub = a human user, email claim present.
// This is the token shape the plan/apply runner forwards on behalf of a
// human user.
//
// The exchange flow deviates from ZITADEL's documented behavior in three
// concrete, investigated ways on v4.15.3:
```

(The numbered deviations 1–3 stay verbatim — they are the self-contained record.)

`usertoken.go` — the exchange-failure fatal (line ~142):

```go
	if err != nil || status != 200 {
		t.Fatalf("token exchange failed (status=%d): %v (oidcTokenExchange is pre-GA in ZITADEL v4.15.3 — investigate before changing the flow)", status, err)
	}
```

`oidc_settings.go` — the feasibility paragraph of `SetAccessTokenLifetime`'s doc:

```go
// Feasibility (verified on v4.15.3): PUT accessTokenLifetime=10s against a
// fresh instance is accepted (200) and takes effect immediately — the next
// machine-user token minted afterward had exp-iat == 10s exactly.
```

- [ ] **Step 2: Verify rendering and that nothing internal remains**

Run: `cd client && go build ./... && go doc ./leifwindtest WithToxiproxy && go doc ./leifwindtest Stack.UserToken && go doc ./leifwindtest Stack.SetAccessTokenLifetime`
Expected: builds; rendered docs read self-contained.

Run: `rg -n 'task-|Task 1|the brief|LW-44|spec .Risks' client/leifwindtest/*.go`
Expected: no matches in non-test files (the `TODO(LW-68)` in `stack.go` is allowed and doesn't match these patterns).

- [ ] **Step 3: Commit**

```bash
git add client/leifwindtest/stack.go client/leifwindtest/usertoken.go client/leifwindtest/oidc_settings.go
git commit -m "docs(leifwindtest): self-contained godoc, drop internal task/spec vocabulary (LW-113)"
```

---

### Task 7: `SetAccessTokenLifetime` takes `time.Duration` (spec B2 — breaking, now-or-never)

The `string` parameter requires ZITADEL's protobuf-duration format (`"5s"`); `"5"`, `"5000ms"`, `"1m5s"` produce an opaque PUT rejection. Make misuse unrepresentable at the type level. Exactly one caller migrates: `internal/acctest/auth_negative_acc_test.go:167`.

**Files:**
- Modify: `client/leifwindtest/oidc_settings.go`
- Create: `client/leifwindtest/oidc_settings_test.go`
- Modify: `internal/acctest/auth_negative_acc_test.go` (line ~167; `time` is already imported)

**Interfaces:**
- Produces: `func (s *Stack) SetAccessTokenLifetime(t testing.TB, lifetime time.Duration)` — breaking signature change. Guard: non-positive or sub-second-remainder lifetime → `t.Fatalf` containing `"whole number of seconds"`. Formats internally as `fmt.Sprintf("%ds", int64(lifetime/time.Second))`.

- [ ] **Step 1: Write the failing test**

Create `client/leifwindtest/oidc_settings_test.go`. The guard fires before any HTTP call, so a zero `&Stack{}` works; `fatalRecorder` captures `Fatalf` and panics to emulate its no-return contract:

```go
// SPDX-License-Identifier: MPL-2.0

package leifwindtest

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// fatalRecorder captures Fatalf and panics to stop execution the way a real
// t.Fatalf would (the guard under test must not fall through to HTTP).
type fatalRecorder struct {
	testing.TB
	msg string
}

func (f *fatalRecorder) Fatalf(format string, args ...any) {
	f.msg = fmt.Sprintf(format, args...)
	panic("fatalRecorder")
}
func (f *fatalRecorder) Helper() {}

func TestSetAccessTokenLifetimeRejectsNonWholeSeconds(t *testing.T) {
	t.Parallel()
	for _, d := range []time.Duration{0, -time.Second, 1500 * time.Millisecond} {
		rec := &fatalRecorder{TB: t}
		func() {
			defer func() { _ = recover() }()
			(&Stack{}).SetAccessTokenLifetime(rec, d)
		}()
		if !strings.Contains(rec.msg, "whole number of seconds") {
			t.Errorf("lifetime %v: guard did not fire (msg=%q)", d, rec.msg)
		}
	}
}
```

- [ ] **Step 2: Verify it fails to compile**

Run: `cd client && go vet ./leifwindtest`
Expected: compile error — `cannot use d (variable of type time.Duration) as string value` (the signature is still `string`). Using `go vet` avoids booting the package's container stack just to see the compile failure.

- [ ] **Step 3: Implement**

In `client/leifwindtest/oidc_settings.go`, add `"fmt"` and `"time"` to the imports, and replace `SetAccessTokenLifetime` (doc comment: keep the instance-wide warning paragraph as edited in Task 6, replace the old "protobuf-duration string" framing with the granularity note):

```go
// SetAccessTokenLifetime overrides this Stack's INSTANCE-WIDE OIDC
// access-token lifetime via ZITADEL's admin API. There is no org- or
// app-level granularity in v4.15.3 (zitadel/zitadel#5219 is still open as
// of writing): every org and every app on the instance is affected for the
// rest of the Stack's lifetime. Callers MUST use a Stack dedicated to the
// test that needs a short lifetime (e.g. via Start(t), never a
// package-shared stack), since every other acceptance test sharing an
// instance would otherwise start minting tokens with the same short
// lifetime.
//
// lifetime must be a positive whole number of seconds — seconds are
// ZITADEL's evidenced granularity, and silently rounding would change test
// semantics, so anything finer fails the test.
//
// Feasibility (verified on v4.15.3): PUT accessTokenLifetime=10s against a
// fresh instance is accepted (200) and takes effect immediately — the next
// machine-user token minted afterward had exp-iat == 10s exactly.
func (s *Stack) SetAccessTokenLifetime(t testing.TB, lifetime time.Duration) {
	t.Helper()
	if lifetime <= 0 || lifetime%time.Second != 0 {
		t.Fatalf("SetAccessTokenLifetime: lifetime must be a positive whole number of seconds, got %v", lifetime)
	}
	var current struct {
		Settings oidcSettings `json:"settings"`
	}
	if err := s.mgmtDo("GET", "/admin/v1/settings/oidc", "", nil, &current); err != nil {
		t.Fatalf("get oidc settings: %v", err)
	}
	current.Settings.AccessTokenLifetime = fmt.Sprintf("%ds", int64(lifetime/time.Second))
	if err := s.mgmtDo("PUT", "/admin/v1/settings/oidc", "", current.Settings, nil); err != nil {
		t.Fatalf("put oidc settings: %v", err)
	}
}
```

Migrate the single caller — in `internal/acctest/auth_negative_acc_test.go` (line ~167), change:

```go
	s.SetAccessTokenLifetime(t, "5s")
```

to:

```go
	s.SetAccessTokenLifetime(t, 5*time.Second)
```

- [ ] **Step 4: Run the guard test and compile the caller**

Run: `cd client && go test ./leifwindtest -run TestSetAccessTokenLifetimeRejectsNonWholeSeconds -count=1` (boots the package stack first — expected; without Docker the boot error is tolerated and this test still runs)
Expected: PASS

Run: `go vet ./internal/acctest/`
Expected: clean (caller compiles against the new signature). The behavioral check is `TestAccExpiredToken` in the normal acceptance lane (`make testacc`), not per-MR.

- [ ] **Step 5: Commit**

```bash
git add client/leifwindtest/oidc_settings.go client/leifwindtest/oidc_settings_test.go internal/acctest/auth_negative_acc_test.go
git commit -m "feat(leifwindtest)!: SetAccessTokenLifetime takes time.Duration (LW-113)"
```

---

### Task 8: Fixture HTTP timeouts + opportunistic robustness (spec B3 + B4)

Three fixture call-sites use HTTP clients with no timeout (`waitZitadelReady`, `mgmtDo`, `fetchToken`); their deadline loops only check the clock between requests, so one hung connection blocks until the CI job timeout. Plus three small robustness items.

**Files:**
- Modify: `client/leifwindtest/zitadel.go` (add `httpClient`; use at lines ~184 and ~244)
- Modify: `client/leifwindtest/org.go` (`fetchToken`, line ~136)
- Modify: `client/leifwindtest/toxiproxy.go` (godoc + error wrapping)
- Modify: `client/leifwindtest/org_test.go` (line ~15)

**Interfaces:**
- Produces: package-level `var httpClient = &http.Client{Timeout: 15 * time.Second}` in `zitadel.go`, shared by all three sites. 15s is comfortably above observed ZITADEL admin-API latency under CPU-starved CI runners and well under the 30–120s loop deadlines, so each loop gets multiple attempts.

- [ ] **Step 1: Shared client with timeout**

In `client/leifwindtest/zitadel.go`, add below the import block:

```go
// httpClient bounds every fixture HTTP call; the per-site deadline loops
// only check between requests, so a hung connection must fail on its own.
var httpClient = &http.Client{Timeout: 15 * time.Second}
```

Then replace the three call-sites:

`zitadel.go`, `waitZitadelReady` (line ~184): `resp, err := http.Get(s.Issuer + "/.well-known/openid-configuration")` →

```go
		resp, err := httpClient.Get(s.Issuer + "/.well-known/openid-configuration")
```

`zitadel.go`, `mgmtDo` (line ~244): `resp, err := http.DefaultClient.Do(req)` →

```go
		resp, err := httpClient.Do(req)
```

`org.go`, `fetchToken` (line ~136): `resp, err := http.DefaultClient.Do(req)` →

```go
	resp, err := httpClient.Do(req)
```

- [ ] **Step 2: Opportunistic items (B4)**

`toxiproxy.go` — `Toxiproxy` godoc:

```go
// Toxiproxy returns the control handle for the backend proxy. The return
// type couples this API to github.com/Shopify/toxiproxy/v2/client:
// consumers import that module to drive the handle.
// Panics unless the stack was started WithToxiproxy().
func (s *Stack) Toxiproxy() *toxiproxy.Proxy {
```

`toxiproxy.go` — `startToxiproxy` error wrapping (the three bare returns):

```go
	host, err := tp.Host(ctx)
	if err != nil {
		return fmt.Errorf("toxiproxy host: %w", err)
	}
	adminPort, err := tp.MappedPort(ctx, "8474/tcp")
	if err != nil {
		return fmt.Errorf("toxiproxy admin port: %w", err)
	}
	dataPort, err := tp.MappedPort(ctx, "8666/tcp")
	if err != nil {
		return fmt.Errorf("toxiproxy data port: %w", err)
	}
```

`org_test.go` (line ~15) — `tok[:20]` panics on short garbage:

```go
		t.Fatalf("expected a JWT (3 segments), got %q…", tok[:min(len(tok), 20)])
```

- [ ] **Step 3: Build, vet, commit**

Run: `cd client && go build ./... && go vet ./...`
Expected: clean. (Behavioral verification is the existing container suite in Task 9 — these changes are mechanical.)

```bash
git add client/leifwindtest/zitadel.go client/leifwindtest/org.go client/leifwindtest/toxiproxy.go client/leifwindtest/org_test.go
git commit -m "fix(leifwindtest): bound fixture HTTP calls with a 15s timeout; small robustness fixes (LW-113)"
```

---

### Task 9: Full verification + merge request

**Files:** none new — gates only.

- [ ] **Step 1: Full gates (Docker required)**

```bash
go build ./... && go vet ./...
(cd client && go build ./... && go vet ./...)
make lint
make test
```

Expected: all green. `make test` runs the whole client module including both container-based suites (`-p 1`, up to 20m). The acceptance suite (`make testacc`) is NOT required per-MR — `TestAccExpiredToken` covers Task 7 in the normal acceptance lane.

- [ ] **Step 2: Push and open the MR**

Work happens on this branch (`worktree-lw-111-113-spec`, already tracking origin). Push, then open the MR with glab (auth: `set -a; source .env; set +a; command glab …` — never print the token):

```bash
git push
set -a; source .env; set +a
command glab mr create --draft --title "Pre-tag hardening: client (LW-111) + leifwindtest (LW-113)" \
  --description "Implements docs/superpowers/specs/2026-07-16-lw-111-113-pretag-hardening-design.md. Closes LW-111, LW-113. Breaking (pre-tag, allowed): client.Version const → Version() func; SetAccessTokenLifetime(string) → (time.Duration)." \
  --source-branch worktree-lw-111-113-spec --target-branch main
```

If glab auth is unavailable, push and hand the MR link creation to the user.
