# Full-repository review — terraform-provider-leifwind

- **Date:** 2026-07-14
- **Revision reviewed:** `ae04371` (main). The working tree's uncommitted
  changes at review time were not part of this review.
- **Method:** dynamic multi-agent workflow — one independent Opus reviewer per
  review unit (98 units covering all 107 tracked files), one Fable
  meta-reviewer per area (9 areas) adversarially verifying, deduplicating, and
  recalibrating every finding against the actual code, final synthesis by the
  session (Fable). 107 agents, 0 errors. 21 per-file claims were refuted by
  the meta-reviewers and are excluded below.
- **Companion artifacts:** `ledger.md` (per-unit progress and verdicts),
  Linear backlog items (LW-…, listed at the end), Notion review page under
  Refinements.

## Assessment

**Ready to release: with fixes — none of them correctness bugs in shipped
provider logic.**

Zero critical findings across the entire repository, and not a single
confirmed correctness bug in the provider's resource/data-source logic, the
client's request path, or the release pipeline. The 6 important findings are
all *coverage and gating* gaps: tests that don't exist (strict-create/drift
for entity+field, by-id data-source branches, the entire root module in
CI/make), release gating that doesn't wait for govulncheck, missing
docs-drift enforcement, and one public-API behavioral gap in the test fixture
(`UserToken` not idempotent per Org). The ~50 confirmed minors are
pre-first-release hardening and polish, most valuable now because `client/`
and `client/leifwindtest` are public modules whose API-shape decisions become
breaking changes after the first tag.

## Strengths (verified, not assumed)

- **Consistent resource architecture:** all three resources share the same
  strict-create, drift-removal (`ErrNotFound` → `RemoveResource`),
  idempotent-delete, and RequiresReplace patterns; load-bearing subtleties
  (plan-time vs apply-time `key_field_ids` emptiness, CLI word-wrap vs
  acceptance-test regexes, server UUID lowercasing) are commented at the
  point of use.
- **Docs generation pipeline works exactly as designed:** every generated
  registry page was verified byte-consistent with its schema Descriptions and
  examples; no hand-edits to generated files anywhere; all import syntaxes
  match the actual ImportState parsers.
- **Client module is release-shape:** clean functional-options API, accurate
  doc comments encoding real backend contract nuances, sentinel-error surface
  bridged via Unwrap, golden wire-format fixtures, toxiproxy fault-injection
  tests, per-test fresh-org tenant isolation.
- **Build/CI craftsmanship:** non-obvious choices (unanchored tag regex, Ryuk
  disabling, cache-key prefixing, `interruptible: false` on release, GOWORK
  toggling) are annotated inline; tool versions are (mostly) pinned;
  goreleaser output matches the registry's required artifact layout exactly.
- **leifwindtest fixture:** container lifecycle and LIFO teardown carefully
  reasoned; ZITADEL version-specific quirks (token exchange, projection lag,
  PAT readback under dind) documented with unusual precision; the
  expired-token acceptance test is empirically grounded with its own
  dedicated stack.
- **Config/auth consistency:** attribute names, `LEIFWIND_*` env vars, and
  Sensitive flags are exactly consistent across `config.go`, the provider
  schema, and `docs/index.md`; Configure correctly rejects unknown values
  before they can masquerade as empty strings.

## Findings

### Critical

None.

### Important (6)

1. **Strict-create and drift-recreation untested for entity and field**
   (`internal/acctest/`, `internal/metadatares/entity.go`, `field.go`).
   All three resources implement the same bespoke strict-create branch and
   Read-time drift removal, but only project has acceptance tests for them.
   Comments in `entity.go:119` and `field.go:339` claim the wrap-safe
   `already exists.*terraform import` wording is "used by acceptance tests" —
   false for entity and field, so the exact regressions that wording guards
   against would go undetected for two of three resources.
   *Fix:* mirror `TestAccProjectStrictCreate` / `TestAccProjectDriftRecreates`
   for entity and field (field drift cleanup must respect LW-70 KEY/FRAGMENT
   ordering).

2. **Root-module unit tests are executed nowhere — not in CI, not by `make
   test`** (`Makefile`, `.gitlab-ci.yml`). Real ungated tests exist
   (`internal/provider/config_test.go`, `internal/metadatares/field_test.go`,
   `import_test.go`) but `make test` only runs the client module and CI only
   runs `test:client` + `test:acceptance` (acctest package only). Provider
   auth-config resolution and field validation logic can regress silently.
   *Fix:* add root `go test ./...` to both the Makefile target and a cheap CI
   job.

3. **Tagged release does not wait for govulncheck** (`.gitlab-ci.yml:147`).
   `release.needs: [lint, test:client, test:acceptance]` decouples the
   release job from stage ordering under GitLab DAG semantics — it publishes
   to the public registries in parallel with (and regardless of) govulncheck.
   Every other deliberate exclusion in the file is annotated; this one is not.
   *Fix:* add govulncheck to `release.needs`, or annotate the exclusion as
   deliberate (see also the "gate on a moving vuln DB?" decision below).

4. **No CI check that generated registry docs are current**
   (`.gitlab-ci.yml`, `Makefile`). Schema changes that skip a manual
   `make docs` ship stale public registry documentation; the working tree at
   review time showed exactly this drift pattern in practice.
   *Fix:* MR+main CI job: run tfplugindocs v0.25.0 and `git diff --exit-code
   docs/`.

5. **By-id lookup branch of `leifwind_entity`/`leifwind_field` data sources
   completely untested** (`internal/metadatads/entity.go:95-106`,
   `field.go:109-120`). Every acceptance config uses the by-name path; the
   by-id branches — advertised in the published examples — are executed by no
   test. Only `leifwind_project` has by-id coverage. Related assertion gaps:
   `leifwind_fields` never asserts `connection_type`/`fragment_name`
   projection; `unique_key` is never asserted on any singular data source.
   *Fix:* extend the existing acceptance configs — near-zero runtime cost.

6. **`leifwindtest.UserToken` is not idempotent per Org — second call
   hard-fails** (`client/leifwindtest/usertoken.go:111-115`). The
   impersonator-role member-add runs on every call outside the `sync.Once`;
   ZITADEL returns AlreadyExists (409) on repeat, which `t.Fatalf`s. Two
   human tokens in one tenant is a reasonable use of this *public* helper.
   *Fix:* tolerate AlreadyExists (or per-Org once-guard the grant); add a
   double-call test.

### Minor — grouped into work items (backlog-worthy)

- **Client hardening before `client/v0.1.0`** (`client/`): retry loop panics
  on negative `MaxBackoff` and retries deterministic errors (marshal/decode)
  with full backoff; token endpoint accepts empty `access_token` (caches ""
  → opaque 401s) and drops the body-read error; empty 2xx bodies would fail
  decode (three Delete methods make it reachable); `Version` const hardcoded
  `0.1.0-dev` stamped into every User-Agent; no nil-guard anywhere on
  server-returned `ObjectID` (a contract-violating response panics the
  provider through the framework).
- **Client test hardening** (`client/*_test.go`): retry integration tests
  have under-provisioned wall-clock margins and lifecycle-unbound goroutines
  mutating the shared proxy; ~7 public-contract branches untested (auth
  non-200 path, `expires_in==0` fallback, FRAGMENT-without-name marshal
  error, LW-70 422→`ErrValidation` contract, UUID-input fragments branch,
  retry 5xx-then-success, `newAPIError` nil body).
- **leifwindtest polish before next client tag** (`client/leifwindtest/`):
  doc-hygiene sweep removing internal task/spec/LW-nn references from
  exported godoc and error strings (renders on pkg.go.dev);
  `SetAccessTokenLifetime(string)` → `time.Duration` (now-or-never breaking
  change); one shared `http.Client` with timeout for all fixture HTTP (a hung
  connection currently defeats every deadline loop).
- **Provider-core minors** (`main.go`, `go.mod`, `internal/provider/`,
  `internal/lookup/`): goreleaser injects `-X main.commit` but no `commit`
  var exists (silently discarded); root `go 1.25.8` vs client `go 1.25.0`
  directive mismatch; `audience` silently ignored on the static-token path;
  config_test gaps (M2M env fallbacks, resolved-field values, full-trio
  mutual exclusion); `internal/lookup` exact-match filtering has no unit
  tests.
- **Data-source/docs polish** (`internal/metadatads/`, generated docs): by-id
  Get* errors uniformly mislabeled "not found" for network/auth/5xx failures
  (client exposes `ErrNotFound` — one-line fix per site);
  `ExactlyOneOf(id, name)` invisible in all three singular data-source docs
  (tfplugindocs can't render ConfigValidators — mirror into Descriptions);
  computed/nested output attributes lack Descriptions across all list data
  sources; `leifwind_entity_fragments` missing Description + `HasError` check
  + nil→[] normalization; field resource description omits `key_field_ids`
  from "only fragment_name is updatable in place".
- **Acceptance-test minors** (`internal/acctest/`): `TestAccMissingCredentials`
  not hermetic against ambient M2M env vars; project data-source assertions
  lack discriminating power (filter no-op passes, `unique_key` unasserted,
  both-set validation untested).
- **Spec sync** (`docs/superpowers/`): LW-70/86 spec prescribes a
  ValidateConfig guard that would *degrade* the shipped (better) code if
  followed literally; Update re-validation is change-gated post-e25e2c6 but
  the spec says per-update; both spec status headers still read
  "awaiting approval/plan" though the features are merged.

### Minor — small/opportunistic (no dedicated backlog item)

- [acctest] Stale `\s+` regex rationale comment in `field_acc_test.go:228`; dead `_ = fmt.Sprintf` in `project_acc_test.go:108`; `orgMu` mutex lacks a rationale comment.
- [build-ci] `default_install_hook_types` missing so commit-msg linting is silently inactive after plain `pre-commit install`; goimports `local-prefixes` unset and default issue caps on a gating lint job; govulncheck `@latest` breaks pinning discipline; `release-dry-run` hardcodes `"main"` vs `$CI_DEFAULT_BRANCH`; dead `GO_MODULES` Makefile var + timeout drift vs CI; `.gitignore` misses the built provider binary and test artifacts; pre-commit tidy check diffs go.mod but not go.sum.
- [client] Unkeyed composite literals in wire-struct marshalers; inconsistent nil-guards in tests; dead import-retention scaffolding; request-construction errors lack method/path context.
- [datasources] Unguarded `*uuid.UUID` deref shared across all six sources — accept the contract explicitly in one comment or add one shared helper; don't fix piecemeal.
- [docs-examples] import.sh placeholder style inconsistent; ZITADEL named in registry docs (see decision); provider example has no version constraint (defer to LW-68).
- [leifwindtest] `exchangeSetup` sync.Once poisoned by `t.Fatalf` (cascading failures); `Toxiproxy()` third-party coupling undocumented; delegated-token test can't positively assert human `sub`; `tok[:20]` diagnostic slice can panic.
- [planning] Executed LW-86 plan quotes drifted code — one-line header note at most.
- [provider-core] `pick` treats explicitly-empty HCL attr as unset, undocumented.
- [resources] Awkward single-segment import error message.

## Cross-cutting themes (visible only at whole-repo level)

1. **Negative-path tests were written once (for project) and never
   propagated** — the same asymmetry shows up independently in acctest
   (strict-create/drift) and datasources (by-id branch), and file-level
   comments even claim coverage that doesn't exist. Anything bespoke on
   project deserves a checklist item "propagate to entity + field."
2. **The root module falls through every test net** — each file (Makefile,
   CI, lint config) looks reasonable alone; only the area view shows
   `internal/provider` and `internal/metadatares` unit tests run nowhere.
3. **Public-module discipline gaps concentrated in leifwindtest** — internal
   task vocabulary in godoc, protobuf-duration string parameter, `:edge`
   image pin, third-party type in the API. All cheap now, breaking changes
   after the first client tag.
4. **Single sources of truth are missing in two places:** lint/test/tidy
   commands restated across Makefile/CI/pre-commit (drift already exists),
   and ILIKE pattern escaping addressed nowhere (client, lookup, spec) — a
   backslash in an object name could make exact-name lookup silently fail
   (needs a backend answer first; see decisions).
5. **LW-71 global project-name uniqueness is a tribal invariant in the test
   suite** — ~19 hard-coded project names must be suite-unique despite
   per-test org isolation; one helper (`uniqueName(t, prefix)`) would make it
   structural.
6. **Positive pattern worth codifying:** auth attribute names/env
   vars/Sensitive flags are perfectly consistent across code, schema, and
   docs — but the Configure unknown-value guard enumerates all six manually
   and would silently miss a seventh.

## Decisions needed (recorded in Notion; not code fixes)

1. **Backend JWT expiry leeway (~30–55s past `exp`)** — intended or a bug?
   (Found empirically by `TestAccExpiredToken`; relates to LW-76.)
2. **Module split for `leifwindtest`** — its testcontainers/moby/toxiproxy
   closure rides in the public client module's dependency graph (and the
   provider's). Split vs accept, before first client tag.
3. **Strict-create TOCTOU race** — lookup-then-upsert cannot be closed
   provider-side; should the backend offer fail-on-exists (conditional POST)?
4. **govulncheck release gating** — gate tags on a moving vuln DB (newly
   published CVE can block an urgent release) or annotate the exclusion.
5. **`leifwind_entity_fragments` keys on `entity_name`** while every sibling
   uses `entity_id` — add `entity_id` + ExactlyOneOf now, or bless the
   asymmetry permanently.
6. **ILIKE metacharacters in object names** — does the backend escape
   patterns? Determines client-side escaping vs server-side restriction vs
   documentation.
7. **`audience` with static token** — hard error, warning, or silently
   ignored (current)?
8. **ObjectID trust posture** — document "server always returns object_id"
   once, or harden to diagnostics instead of panics.
9. **Ops verification (not checkable from the repo):** confirm `v*` is a
   protected tag pattern and GPG/mirror tokens are protected+masked — anyone
   who can push a v-tag can currently trigger a signed public release.
10. **Smaller:** M2M refresh acceptance test worth a dedicated stack boot?;
    32-bit (386) builds dropped deliberately?; CI↔Makefile single source of
    truth; go 1.25 floor for the public client; ZITADEL naming in registry
    docs; `FieldConfig` forward-compat contract (data_type-only vs raw-JSON
    passthrough); executed-plan vs living-spec lifecycle convention;
    `UserToken` returning the human user ID; `Version` const vs
    `debug.ReadBuildInfo`.

## Statistics

| | |
|---|---|
| Files reviewed | 107 (98 review units, 9 areas) |
| Agents | 107 (98 Opus reviewers, 9 Fable meta-reviewers), 0 errors |
| Confirmed findings | 56 — 0 critical, 6 important, 50 minor |
| Refuted file-review claims | 21 (excluded above) |
| Clean units | 63 of 98 |
| Wall clock | ~15 min; ~3.46M subagent tokens |

## Backlog mapping

Linear issues created from this review are listed in the ledger and linked
from the Notion page (parent: LW-41; related: LW-43, LW-68).
