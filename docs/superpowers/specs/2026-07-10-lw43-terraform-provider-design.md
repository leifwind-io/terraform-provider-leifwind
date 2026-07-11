# LW-43 Design: terraform-provider-leifwind — public provider + standalone Go client

**Status:** Refined — approved by owner 2026-07-10, awaiting implementation plan.
**Date:** 2026-07-10
**Ticket:** [LW-43](https://linear.app/leifwind/issue/LW-43) (parent [LW-41](https://linear.app/leifwind/issue/LW-41) sub-project 2; blocked LW-42 is **Done**, backend `main` @ `a828641`)
**Related:** LW-29 (auth, merged), LW-44 (primary consumer), LW-68 (first-release checklist, split out of this ticket), [LW-42 refinement](https://app.notion.com/p/39870227d4dd817897ece7e9a8319190), DR-005 (ZITADEL), DR-009 (OpenTofu).

---

## Context

Public Terraform/OpenTofu provider for the leifwind metadata API (control plane only), plus a standalone, importable Go client. Primary execution context is LW-44's delegated apply-runner: the frontend logs a user in via ZITADEL and the runner executes `tofu plan/apply` on behalf of that user — the token the provider carries must represent the delegating user (`sub` = user, `org_id` claim scopes tenancy). Interactive/local operator use is secondary but supported.

Backend contract (verified against `main` @ `a828641`, all LW-42 endpoints merged):

- 12 `/metadata` endpoints: upsert (POST, create-or-adopt by `object_id` **or** server-computed natural `unique_key`), GET single, cursor-paginated lists (`limit` ≤ 50, opaque signed cursor embedding pattern+limit, `cursor: null` = last page), DELETE with server-side cascade (FK `ON DELETE CASCADE` + `DROP SCHEMA`). Immutable attributes (project/entity `name`, field `project_id`/`entity_id`/`name`/`data_type`/`connection_type`) → 422 on change. Every write takes a transactional `dry_run` query param. Cross-tenant access → 404 (no existence oracle).
- Fragments are field-level: `connection_type = "FRAGMENT"` + `fragment_name`; read-only fragment names via `GET /generic/projects/{id}/schemas/entities/{name}/fragments` → `{"fragments": [...]}`.
- Auth (LW-29): JWKS-validated JWTs only (RS256 + EdDSA), `iss`/`aud` checked (`aud` = ZITADEL API-project ID), `exp`/`iat`/`sub` required, `org_id` from `urn:zitadel:iam:user:resourceowner:id`. Opaque PATs rejected. The ticket's "token accepted but unused" wording is stale — the token is functional from day one.

## Fixed decisions (recorded, not relitigated)

| Decision | Rationale |
|---|---|
| GitLab primary (`gitlab.com/leifwind/stream/terraform-provider-leifwind`), GitHub push mirror for distribution | Both registries resolve providers via public GitHub repos + GitHub Releases; development, review and CI stay with the backend's GitLab group. Mirror is already configured in the GitLab UI. |
| License MPL-2.0 (whole repo) — **owner revision 2026-07-10**, superseding the earlier AGPL-3.0-only decision | File-level copyleft suffices for a locally-run plugin binary (AGPL's §13 network clause has no practical bite for a provider); keeps HashiCorp Partner-tier eligibility open (AGPL/GPL are excluded from its allowed-license list, MPL-2.0 is on it); matches provider-ecosystem convention and lets the hcloudgroup template's MPL header tooling be reused as-is. Community-tier publishing has no license restriction either way (verified 2026-07-10). Decision is cheapest now, while the sole copyright holder. |
| Monorepo, two Go modules: `/` (provider, terraform-plugin-framework only) + `/client` (nested module, tagged `client/vX.Y.Z`, zero `terraform-plugin-*` deps) | Client is a first-class public deliverable (LW-44 runner imports it standalone). |
| Handwritten client mirroring `client.py` semantics; no OpenAPI codegen | Upsert + cursor semantics are subtle; the Python client is the reference implementation. |
| Provider talks to the backend **exclusively** through `/client` (no `net/http` in provider code; depguard-enforced) | Dogfooding keeps the client honest and tested. |
| Structural template: `terraform-provider-hcloudgroup` | goreleaser + GPG signing + `terraform-registry-manifest.json` (protocol 6.0) + tfplugindocs recipe, framework v1.19 patterns. |

## Resolved OPEN decisions

### 1. Module paths & namespace
- Provider module: `gitlab.com/leifwind/stream/terraform-provider-leifwind`; client module: `.../terraform-provider-leifwind/client`. Import paths match real hosting; no vanity-URL infra.
- GitHub mirror: `github.com/leifwind-io/terraform-provider-leifwind` → registry source address **`leifwind-io/leifwind`**.

### 2. Client API shape
- `context.Context`-first on every method; default `User-Agent: terraform-provider-leifwind-client/<version>` (provider appends its own product token).
- **Errors:** `*APIError{StatusCode, Detail, Method, Path}` unwrapping to sentinels `ErrNotFound` (404), `ErrConflict` (409), `ErrValidation` (422), `ErrUnauthenticated` (401); checked via `errors.Is`.
- **Retries:** default 3 attempts, exponential backoff + jitter, context-aware, on transport errors and 5xx for **all** verbs (upserts/deletes are idempotent by natural-key design). 4xx never retried. A retried DELETE treats 404 as success only when a prior attempt failed mid-flight. Configurable via `WithRetry`.
- **Pagination:** `List*(ctx, …, ListOpts{Limit, Pattern, Cursor}) (Page[T], error)` + `Iter*` returning `iter.Seq2[T, error]` (auto-paging; forwards only the cursor on follow-up pages, matching server semantics). Unset params omitted from the query string (FastAPI rejects empty-string ints).

### 3. Versioning & release
- Independent version streams: `vX.Y.Z` (provider) and `client/vX.Y.Z` (client bumps only on client changes). Provider `go.mod` always pins a **released** client tag; committed `go.work` makes local dev + CI tests use the workspace copy; release/dry-run jobs run `GOWORK=off` to prove the pin builds standalone. Bootstrap order: `client/v0.1.0` before `v0.1.0` (tracked in LW-68).
- Conventional commits (commitlint-enforced) + goreleaser-generated grouped changelog (`use: git`); no hand-maintained CHANGELOG.md in v0.

### 4. CI split & infrastructure access
- Everything on GitLab CI; the GitHub mirror runs no CI. Push mirror (already configured) syncs code; the release job additionally pushes the tag to GitHub explicitly before goreleaser (mirror sync lag must not race registry ingestion).
- Backend image for tests: pulled from the private GitLab registry via `CI_JOB_TOKEN`; requires an allowlist entry **on the backend project** naming this project, in default "user membership and role" mode (GitLab fine-grained job-token permissions do not cover container-registry pulls). ⚠️ An inverse entry (backend listed on the provider's allowlist) was added 2026-07-10 and should be removed (tracked in LW-68).
- Image pin: `edge` temporarily (backend has no release tag yet); owner cuts a backend release in parallel; switch to the semver tag before first provider release (LW-68).

### 5. Toolchain
- Go 1.25 (terraform-plugin-framework v1.19 floor). golangci-lint v2, strict set from day one: errcheck, govet, staticcheck, unused, gosec, bodyclose, contextcheck, revive, exhaustive, errorlint, nilerr + gofmt/goimports, over both modules. goheader enforces `// SPDX-License-Identifier: MPL-2.0` on every Go file. depguard forbids `net/http` imports in provider packages (dogfooding rule).
- pre-commit: gofmt/goimports, golangci-lint, conventional-commit message hook, `go mod tidy` check.

### 6. Auth for delegated execution
- **(a) Acquisition (LW-44 runner):** forward the user's access token (frontend requests the API-project audience scope at login). No standing impersonation privilege; works on pinned ZITADEL v4.15.3. **Upgrade path documented, not implemented:** RFC 8693 token exchange (GA in ZITADEL ≥ Feb 2026 releases; pre-GA behind `oidc_token_exchange` flag on v4.15.3) once the backend learns to read/audit the `act` claim. Security rationale: exchange's audit/scope-down wins require backend `act` support to pay off, while its cost — an org-wide impersonator role on the runner SA — is a larger standing attack surface than replaying session-delegated tokens.
- **(b) Delivery:** provider v0 ships static `token` (sensitive, `LEIFWIND_TOKEN`). The client's public API takes a `TokenSource` interface (`Token(ctx) (string, error)`) from day one with `StaticToken` and `ClientCredentials` implementations — `token_file`/re-reading sources can land later with zero breaking changes. Token fetched from the source per request, so refresh Just Works mid-apply.
- **(c) Acceptance-test tokens:** real ZITADEL v4.15.3 testcontainer (port of the backend's `testing.py` fixture). Delegated user tokens minted via token exchange inside the fixture (see Test strategy). No fake-issuer harness — owner decision: fidelity over speed.
- **(d) M2M in v0 (deviation from ticket, owner decision, commented on LW-43):** provider schema also ships `issuer`/`client_id`/`client_secret`/`audience` with auto-refreshing client_credentials (mirrors `ClientCredentialsTokenProvider`: `POST {issuer}/oauth/v2/token`, basic auth, scopes `openid` + `urn:zitadel:iam:user:resourceowner` + `urn:zitadel:iam:org:project:id:{aud}:aud`, refresh 60 s before expiry). Env names reuse the backend CLI's set: `LEIFWIND_OIDC_ISSUER`, `LEIFWIND_CLIENT_ID`, `LEIFWIND_CLIENT_SECRET`, `LEIFWIND_OIDC_AUDIENCE`. Endpoint stays `LEIFWIND_ENDPOINT` per ticket (CLI uses `LEIFWIND_BASE_URL`; discrepancy noted, no dual-name support).

## Repo layout

```
terraform-provider-leifwind/          module gitlab.com/leifwind/stream/terraform-provider-leifwind
├── go.work                           use (., ./client) — committed
├── main.go                           providerserver.Serve, registry.terraform.io/leifwind-io/leifwind
├── internal/
│   ├── provider/                     provider schema + Configure
│   ├── metadatares/                  leifwind_project/_entity/_field resources
│   ├── metadatads/                   data sources incl. leifwind_entity_fragments
│   └── acctest/                      TF_ACC harness (imports client/leifwindtest)
├── client/                           module .../client — zero terraform-plugin-* deps
│   └── leifwindtest/                 exported containerized-stack fixture (public deliverable)
├── docs/  examples/                  tfplugindocs output + runnable examples
├── .goreleaser.yml                   from hcloudgroup: zips, SHA256SUMS + GPG sig, manifest copy
├── terraform-registry-manifest.json  protocol_versions ["6.0"]
├── .gitlab-ci.yml  .golangci.yml  .pre-commit-config.yaml  LICENSE  README.md
```

Build matrix: standard scaffold set including windows/amd64 (pure-HTTP provider; hcloudgroup's trimmed matrix existed only for its smoke-coverage policy).

## Client API design (`/client`, package `client`)

```go
c, err := client.New(endpoint,
    client.WithTokenSource(client.StaticToken(tok)),
    // or: client.WithTokenSource(client.ClientCredentials(issuer, id, secret, client.WithAudience(projectID)))
    client.WithUserAgent("terraform-provider-leifwind/0.1.0"),
    client.WithRetry(client.RetryConfig{MaxAttempts: 3}),
    client.WithHTTPClient(hc))
```

Services mirror the Python namespaces:

- `c.Metadata`: `UpsertProject/GetProject/DeleteProject/ListProjects/IterProjects` + same triplets for entities and fields (`Get/List/Iter/Upsert/Delete`). Write methods accept `client.DryRun()` option (server-side transactional dry run).
- `c.Generic.ListEntityFragments(ctx, projectID, entityName) ([]string, error)`.

Models mirror pydantic byte-for-byte on the wire: `MetadataProject{ObjectID *uuid.UUID, Name, UniqueKey /*read-only*/}`, `MetadataEntity{+ProjectID}`, `MetadataField{+EntityID, Config FieldConfig, Connection Connection}`. `FieldConfig.DataType` enum: `TEXT INTEGER DECIMAL BOOLEAN DATE TIME TIMESTAMP UUID`. `Connection{Type: KEY|FRAGMENT, FragmentName}` — `fragment_name` marshalled only when `FRAGMENT`. Upsert contract documented on methods: omit `object_id` to create-or-adopt; server 422s on immutable-field changes; response always carries canonical `object_id` + `unique_key`.

Testability seam: `WithClock` option (injected clock) so token-refresh timing is testable against real ZITADEL.

## Provider design

Config (all optional in schema; validated in `Configure` — `token` XOR `issuer/client_id/client_secret` trio, one must resolve via config or env; diagnostics name missing env vars):

```hcl
provider "leifwind" {
  endpoint      = "..."   # LEIFWIND_ENDPOINT (required via config or env)
  token         = "..."   # LEIFWIND_TOKEN, sensitive — delegated/runner path
  issuer        = "..."   # LEIFWIND_OIDC_ISSUER      ┐
  client_id     = "..."   # LEIFWIND_CLIENT_ID        │ M2M path
  client_secret = "..."   # LEIFWIND_CLIENT_SECRET, sensitive
  audience      = "..."   # LEIFWIND_OIDC_AUDIENCE    ┘
}
```

Resources — server immutability maps to plan modifiers:

| Resource | Attributes | RequiresReplace | Updatable | Import ID |
|---|---|---|---|---|
| `leifwind_project` | `id` (computed), `name` | `name` | — | `<project_id>` |
| `leifwind_entity` | `id`, `project_id`, `name` | `project_id`, `name` | — | `<project_id>/<entity_id>` |
| `leifwind_field` | `id`, `project_id`, `entity_id`, `name`, `data_type`, `connection_type`, `fragment_name`, `key_field_ids` | all except `fragment_name`, `key_field_ids` | `fragment_name`, `key_field_ids` (config-only ordering hint, LW-86) | `<project_id>/<entity_id>/<field_id>` |

- `data_type`/`connection_type` are flat string attributes with enum validators; client translates to/from the nested discriminated unions.
- `ValidateConfig` on field: `fragment_name` required iff `connection_type == "FRAGMENT"`, forbidden otherwise (fail at plan time instead of the server silently nulling it). `key_field_ids` follows the same shape rule (required-for-FRAGMENT / forbidden-for-KEY, config-only) — the reference to the entity's KEY field id(s) supplies the Terraform create/destroy ordering edge (backend enforces KEY-before-FRAGMENT); membership is validated at apply and the value is seeded from the server on import (LW-86).
- **Strict Create** (deliberate deviation from raw upsert semantics): `Create` pre-checks existence by exact name (list + client-side match); if found → error "already exists — use terraform import". Rationale: Terraform's contract; silent adoption would let a later `destroy` delete infrastructure the config never created. Check-then-POST race accepted and documented. LW-44's adopt-then-diff uses explicit `import` blocks and is unaffected.
- `Read`: GET by `object_id`; `errors.Is(err, ErrNotFound)` → `RemoveResource` (drift + cross-org 404 handling). `Delete`: relies on server-side cascade. Project/entity `Update` unreachable (all attrs RequiresReplace); field `Update` flows only `fragment_name`.
- No `timeouts` blocks in v0.

Data sources: `leifwind_project`/`_entity`/`_field` (by `id` XOR `name`; name lookup lists with `pattern` then exact-matches client-side — server pattern is ILIKE-substring); `leifwind_projects`/`_entities`/`_fields` (optional `pattern`, iterate all pages); `leifwind_entity_fragments(project_id, entity_name) → fragments []string`.

## Test strategy — blackbox through the full stack

**Owner decision (deviation from ticket's "httptest unit tests"):** no mocked-backend suite. Every wire-touching test runs `provider → client → backend container → PostgreSQL` with real ZITADEL-issued tokens, TDD (tests first). The API contract is verified against the real backend so tests cannot drift from reality.

**Shared fixture — exported `client/leifwindtest`:** ZITADEL v4.15.3 + `postgres:18-alpine` (ZITADEL DB) + PostgreSQL 18 (backend DB) + backend image on one docker network, porting `testing.py`: `start-from-init --masterkeyFromEnv --tlsMode disabled`, first-instance PAT via docker archive API (distroless), `leifwind-api` project → `OIDC_AUDIENCE`, backend env `OIDC_ISSUER`/`OIDC_INTERNAL_BASE_URL`. Per-test fresh org + machine user (JWT access-token type) + project grant → tenant isolation, `t.Parallel()`. Public deliverable: client consumers (LW-44 runner) get "spin up a real leifwind stack" for their own tests. Backend image: `registry.gitlab.com/leifwind/stream/backend:edge` (temporary; semver pin via LW-68). Accepted cost: ~1–2 min fixture boot per package.

**Token paths, both exercised in TF_ACC:**
1. **M2M**: provider configured with the client_credentials block against fixture ZITADEL.
2. **Delegated user token**: fixture enables `oidc_token_exchange` feature flag + impersonation, creates a human user, grants the fixture machine user an impersonator role, exchanges (RFC 8693, `user_id` subject type) for a genuine user-scoped token (`sub` = human, `email` present) fed via static `token`. Satisfies the "delegated-style token" requirement authentically and rehearses the LW-44 upgrade path in CI.

**Layers:**
1. Client blackbox (`/client` against the stack): all 16 public methods (Upsert/Get/List/Iter/Delete × project/entity/field + `ListEntityFragments`), create-vs-adopt, immutable-field 422s, pagination across real pages (51+ objects), sentinel mapping from real 404/409/422/401, `dry_run` rollback, `ClientCredentials` refresh via injected clock (fresh token with new `iat`).
2. Fault injection — toxiproxy container between client and backend: connection resets/timeouts → retry-with-backoff, DELETE-retry 404-tolerance, context cancellation mid-backoff. (HTTP-5xx retry shares the transport-error code path; no 500-injection rig.)
3. Provider acceptance (`TF_ACC=1`, real `tofu`): per resource create → read → update (where legal) → import (`ImportStateVerify`) → destroy; drift (out-of-band delete → plan recreates); strict-create conflict; all data sources incl. by-name and fragments; negative matrix: missing token, garbage token, **forged token** (signed by a key not in ZITADEL's JWKS → 401), **cross-org 404** (two real orgs), nonexistent import ID. **Expired token**: plan A — minimal access-token lifetime via ZITADEL admin OIDC-settings API in a dedicated org, test waits out expiry (feasibility verified during implementation); fallback — documented as backend-tested (pyjwt `exp`), provider-side 401 handling covered by the forged-token path.
4. Pure in-process logic (no I/O, nothing mocked): import-ID parsing, config mutual-exclusion diagnostics, fragment/connection validation.

## CI/CD

`.gitlab-ci.yml` stages (dind jobs use `docker:27-dind` + `TESTCONTAINERS_HOST_OVERRIDE=docker`, matching the backend's CI):

1. **lint** — golangci-lint (both modules), commitlint (MR).
2. **test** — `test:client` and `test:acceptance` (dind; `docker login registry.gitlab.com` via `CI_JOB_TOKEN`; acceptance installs `tofu`, sets `TF_ACC=1`, `TF_ACC_PROVIDER_HOST=registry.opentofu.org`).
3. **release-dry-run** — MRs + main: `GOWORK=off goreleaser release --snapshot --skip=sign,publish` + `goreleaser check`.
4. **release** — protected `v*` tags, after tests: import GPG key → `git push github "$CI_COMMIT_TAG"` → `GOWORK=off goreleaser release --clean` (`GITHUB_TOKEN=$GITHUB_MIRROR_TOKEN`). `client/v*` tags: lint + test only (module tags need no artifacts; mirror syncs them).

CI variables (masked, protected): `GPG_PRIVATE_KEY`, `GPG_PASSPHRASE`, `GITHUB_MIRROR_TOKEN`. Setup steps + registry onboarding: **LW-68**.

**Local test prerequisites:** docker daemon; one-time `docker login registry.gitlab.com` (PAT with `read_registry`); Go ≥ 1.25; `tofu` binary. Everything else is public images; the fixture self-bootstraps.

## Docs

tfplugindocs-generated `docs/` (no custom templates dir) fed by runnable `examples/` per resource/data source + provider block; client README with standalone non-Terraform example (client_credentials → list projects); root README: install from both registries, both auth paths, local dev/test guide, release process.

## Non-goals

- Data-plane CRUD (entity rows / fragment **content**) — reads stay direct API calls (LW-41).
- `leifwind_fragment` resource — fragments remain field-level attributes + read-only data source.
- `token_file` provider attribute — future, non-breaking via the TokenSource seam.
- RFC 8693 in the production runner story — documented upgrade path only (CI exercises exchange for test-token minting).
- SDKv2 / muxing; PAT/introspection support (backend rejects opaque tokens); rate-limit/429 handling (backend has none); Partner-tier registry listing (program application/review overhead — now license-eligible with MPL-2.0 should it ever be wanted).
- First-release/registry onboarding execution — split to **LW-68**.

## Definition of done (LW-43)

- GitLab CI green: lint + client blackbox + TF_ACC acceptance (all container-based).
- goreleaser dry-run passes (`GOWORK=off`, snapshot, no sign/publish).
- docs/ generated, examples runnable; client README standalone example.
- Refinement (this doc) on Notion under Refinements, linked on LW-43; implementation plan committed; deviation comments on LW-43 (env naming — posted 2026-07-10; final summary at completion).
- Branch `feature/lw-43-terraform-provider-leifwind-public-provider-for-the-metadata`, small atomic conventional commits, strict TDD.

## Risks / open items

- **Backend semver tag** pending (owner, parallel) — tests pin `edge` until then; switch tracked in LW-68. `edge` breakage on backend merges is accepted short-term.
- **Expired-token acceptance test** feasibility (min token lifetime configurable via ZITADEL admin API?) — resolved during implementation; documented fallback exists.
- **Token-exchange fixture** relies on pre-GA flag semantics on v4.15.3 — validated by the fixture itself in CI; if the flag misbehaves, fallback is a machine-user token labeled as delegated-equivalent (backend treats both identically; `is_machine` heuristic only).
- **Job-token allowlist** direction must be fixed on the backend project before provider CI can pull the image (LW-68; currently reversed).
