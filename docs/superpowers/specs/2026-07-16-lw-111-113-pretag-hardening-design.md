# Pre-tag hardening: `client` (LW-111) + `leifwindtest` (LW-113)

**Date:** 2026-07-16
**Source:** 2026-07-14 full-repo multi-agent review (`docs/superpowers/reviews/2026-07-14-full-repo-review/report.md`, commit `4203738`); Linear [LW-111](https://linear.app/leifwind/issue/LW-111) and [LW-113](https://linear.app/leifwind/issue/LW-113), parent LW-41.
**Gate:** everything here must land **before the first `client/vX.Y.Z` tag** — both packages are public modules whose API shape freezes at that tag. The `SetAccessTokenLifetime` signature change is the one hard now-or-never breaking change.

## Owner decisions (adjudicated 2026-07-16)

Three items were left open by the review ("open decisions" on the Notion review page). Decided with the owner:

1. **Version reporting** → resolve at runtime via `debug.ReadBuildInfo`, not a release-checklist const bump.
2. **ObjectID trust posture** → harden in the client (post-decode guard returning an error), not document-only.
3. **`UserToken` exposing the human user ID** → deferred, out of scope. Adding it later is additive (new method), so it is not gated on the tag.

## Scope

Two independent workstreams; they may land as one MR or two. No provider behavior changes except that a contract-violating backend response now surfaces as a Terraform diagnostic instead of a provider panic.

**Non-goals:** splitting `leifwindtest` into its own module (review open decision #2); pinning `BackendImage` off `:edge` (LW-68); `UserToken` per-org idempotency (LW-110); client *test* hardening (LW-112); exposing the human user ID from `UserToken` (deferred, above).

---

## Workstream A — `client` package (LW-111)

### A1. Retry hardening (`retry.go`, `client.go`)

**Problems.** (a) `RetryConfig` is public API with zero validation: a negative `MaxBackoff` flows through `sleepBackoff` into `rand.Int64N` with a non-positive argument → panic. (b) `doRetry` retries every non-`APIError` failure up to `MaxAttempts` with full backoff — including deterministic ones. Worst case: a 2xx response whose body fails to decode **re-executes the request**.

**Design.**

- `New` normalizes the retry config once, after options are applied:
  - `MaxAttempts < 1` → `1`
  - `MinBackoff < 0` → `0`
  - `MaxBackoff < MinBackoff` → `MinBackoff`

  `sleepBackoff` then never sees a negative bound (its existing `backoff <= 0 → MaxBackoff` clamp keeps handling the shift-overflow case). The `maxAttempts < 1` re-check in `doRetry` becomes redundant but stays as a cheap invariant.
- Error classification: an unexported marker wrapper `permanentError` (implements `Unwrap`). `doOnce` wraps the deterministic failure stages: request-body encode, `http.NewRequestWithContext` failure, and 2xx decode. `doRetry` retries only errors that are (i) `APIError` with status ≥ 500, or (ii) neither an `APIError` nor marked permanent (transport errors, including body-read failures, which can be mid-stream connection drops). Marked errors return immediately on attempt 1. `errors.Is`/`errors.As` through the wrapper keeps working for callers.
- Token-source failures are deliberately **not** marked permanent: deterministic credential failures surface as 4xx `APIError`s (already non-retried), while transient token-endpoint transport failures remain retryable.

### A2. Token-endpoint response handling (`auth.go`)

**Problems.** `body, _ := io.ReadAll(resp.Body)` discards the read error (the main request path wraps it — drift). A 200 response with a missing or empty `access_token` caches `""` and returns nil, producing `Authorization: Bearer ` (opaque backend 401) and a token re-fetch on every subsequent request.

**Design.** In `ccTokenSource.Token`: wrap the read error (`token endpoint: read body: %w`); after decoding, if `out.AccessToken == ""`, return an error (`token endpoint: 200 response without access_token`) and cache nothing. The existing `ExpiresIn == 0 → 3600` default stays.

### A3. Empty 2xx body tolerance (`client.go`, `metadata.go`)

**Problems.** `doOnce` unconditionally unmarshals when `out != nil`; a future backend 204 (or empty 200) breaks every such call. All three `Delete*` methods pass a non-nil `out` struct (`Detail`) that is never read, so today even deletes would break.

**Design.** Both halves:

- `doOnce`: skip the unmarshal when the response body is empty (`len(rb) == 0`); `out` stays zero-valued.
- `DeleteProject`/`DeleteEntity`/`DeleteField`: pass `nil` out — the `Detail` struct is dead code.

### A4. Version reporting (`client.go`)

**Problem.** `const Version = "0.1.0-dev"` is stamped into every User-Agent with no release hook — it will still say `0.1.0-dev` in the tagged `v0.1.0` module.

**Design.** Replace the const with a cached function:

- `func Version() string`, computed once (`sync.OnceValue`): from `debug.ReadBuildInfo`, find the dep whose `Path` is this module's path (`gitlab.com/leifwind/stream/terraform-provider-leifwind/client`); fall back to `bi.Main.Version` when the module *is* the main module; fall back to `"dev"` when build info is unavailable or the version is empty/`(devel)`.
- User-Agent becomes `terraform-provider-leifwind-client/` + `Version()`.

External consumers of the tagged module get the real version automatically (module versions are stamped into build info by the toolchain). In-repo `go.work` builds resolve to `"dev"` — acceptable: the provider prepends its own product token via `WithUserAgent`.

Removing the exported `Version` const in favor of a function is a pre-tag API change, allowed under the gate.

### A5. ObjectID nil-guard (`models.go`, `metadata.go`)

**Problem.** `ObjectID` is `*uuid.UUID` on all three models and no code path guards it. A contract-violating 200 (missing `object_id`) panics the provider through the framework at ~10 call-sites — and not only via `Upsert*/Get*`: the plural data sources dereference `ObjectID` from **List** results (`internal/metadatads/projects.go:89`, `entities.go:97`, `fields.go:111`).

**Design.** One central post-decode check covering upserts, gets, and lists (Iter* is built on List, so it is covered for free):

- Unexported interface, implemented by `MetadataProject`, `MetadataEntity`, `MetadataField`:

  ```go
  type objectIDCarrier interface{ hasObjectID() bool }
  ```
- Shared helper `requireObjectID(method, path string, v any) error`: if `v` implements `objectIDCarrier` and reports false, return `fmt.Errorf("%s %s: server returned object without object_id", method, path)`.
- Call it after successful decode in the six `Upsert*/Get*` methods and, in `listPage`, over `out.Objects`.

No new exported sentinel error (YAGNI — callers have no branch to take on it; it indicates a broken backend).

### A6. Opportunistic (small, same files)

- Keyed composite literals for the three wire-struct marshalers (`projectWire`, `entityWire`, `fieldWire` at `models.go:103,134,171`) — positional literals silently misassign on field insertion.
- Wrap the `http.NewRequestWithContext` error in `doOnce` with method/path context, matching every other error in that path.

### A. Tests (httptest-based, following `retry_test.go` / `client_test.go` patterns)

1. `WithRetry(RetryConfig{MaxAttempts: 3, MaxBackoff: -1})` + failing server: completes without panic.
2. 2xx with malformed JSON body: exactly one request issued (server-side counter), error mentions `decode`.
3. Unmarshalable request body: exactly zero requests issued, no retries.
4. Token endpoint 200 with `{}` and with `{"access_token": ""}`: `Token` returns an error; a subsequent call re-fetches (nothing cached).
5. 204/empty-body 2xx with non-nil `out`: no error, `out` zero-valued; `Delete*` against empty-body 200 succeeds.
6. Upsert/Get/List responses with `object_id` absent: wrapped error, no panic; provider-side behavior needs no test (error surfaces as a normal diagnostic).
7. `Version()`: returns non-empty; UA string matches `^terraform-provider-leifwind-client/`; fallback path returns `dev` (unit-testable by asserting the parse helper on a synthetic `debug.BuildInfo` if the lookup is factored to take one).

---

## Workstream B — `leifwindtest` package (LW-113)

### B1. Doc-hygiene sweep (godoc renders on pkg.go.dev)

Remove internal task/spec/issue vocabulary; keep the genuinely valuable content (ZITADEL v4.15.3 deviations, feasibility facts) rewritten self-contained:

| Site | Today | Change |
|---|---|---|
| `stack.go:41` | "fully wired in the retry task" | Describe behavior: routes `ProxiedBackendURL` through a toxiproxy container for fault injection. |
| `stack.go:50-51` | "set by startBackend (Task 11)" / "set by WithToxiproxy (Task 17)" | Drop the task references; keep the setter attribution. |
| `usertoken.go:13-18` | "LW-44's runner", "see the task-10 report for the full trace" | Describe the token shape in product terms (delegated user token forwarded by the plan/apply runner); keep the three-deviation summary as the self-contained record. |
| `oidc_settings.go:28` | "(throwaway spike, see task-27 report)" | Keep the feasibility fact (PUT `accessTokenLifetime=10s` accepted and effective immediately), drop the report pointer. |
| `usertoken.go:142` | runtime error `… — see spec 'Risks': pre-GA flag on v4.15.3; investigate before falling back` | Self-contained actionable message, e.g. `token exchange failed (status=%d): %v (oidcTokenExchange is pre-GA in ZITADEL v4.15.3 — investigate before changing the flow)`. |

Untouched: `BackendImage`'s `TODO(LW-68)` (tracked by LW-68 itself) and the `zitadel#5219` upstream reference (public issue, correct to cite).

### B2. `SetAccessTokenLifetime` takes `time.Duration` (breaking — now-or-never)

**Problem.** `SetAccessTokenLifetime(t testing.TB, lifetime string)` requires ZITADEL's protobuf-duration format (`"5s"`); `"5"`, `"5000ms"`, `"1m5s"` produce an opaque PUT rejection.

**Design.** Signature becomes `SetAccessTokenLifetime(t testing.TB, lifetime time.Duration)`. Formats internally as whole seconds (`fmt.Sprintf("%ds", int64(lifetime/time.Second))`). Guard: non-positive or sub-second-remainder input → `t.Fatalf` (seconds are ZITADEL's evidenced granularity; silently rounding would change test semantics). Misuse becomes unrepresentable at the type level.

**Caller migration.** Exactly one caller: `internal/acctest/auth_negative_acc_test.go:167` — `"5s"` → `5*time.Second`.

### B3. Shared `http.Client` with timeout

**Problem.** `waitZitadelReady` (`zitadel.go:184`, `http.Get`), `mgmtDo` (`zitadel.go:244`, `http.DefaultClient`), and `fetchToken` (`org.go:136`, `http.DefaultClient`) all use clients with **no timeout**. Every deadline loop checks the clock only between requests, so one hung connection blocks until the CI job timeout.

**Design.** One package-level client used by all three sites:

```go
// httpClient bounds every fixture HTTP call; the per-site deadline loops
// only check between requests, so a hung connection must fail on its own.
var httpClient = &http.Client{Timeout: 15 * time.Second}
```

15s is comfortably above observed ZITADEL admin-API latency under CPU-starved CI runners and well under the 30–120s loop deadlines, so each loop gets multiple attempts.

### B4. Opportunistic

- `Toxiproxy()` godoc: document that the return type couples the public API to `github.com/Shopify/toxiproxy/v2/client` (consumers import that module to use the handle).
- `startToxiproxy`: wrap the `Host()` / `MappedPort()` errors with context (`toxiproxy host: %w` etc.) — today they return bare.
- `org_test.go:15`: `tok[:20]` panics on short garbage; guard with `tok[:min(len(tok), 20)]`.

### B. Verification

- `go build ./...` + `go vet ./...` in the client module; existing `leifwindtest` unit tests (`stack_test.go`, `org_test.go`, `usertoken_test.go`) pass.
- Acceptance suite unaffected except the one migrated caller; `TestAccExpiredToken` (the `SetAccessTokenLifetime` consumer) is the behavioral check for B2 and runs in the normal acceptance lane, not per-MR.
- `pkg.go.dev` rendering of B1 is reviewed via `go doc` output for the touched symbols.

---

## Sequencing

A and B are independent. Within A: A1–A3 and A6 are one natural commit series (request path), A4 and A5 are standalone. Within B: B1 is docs-only; B2 touches one test caller; B3/B4 are mechanical. Suggested order: A5 first (provider-facing safety), then A1–A3, A4, A6; B in any order.
