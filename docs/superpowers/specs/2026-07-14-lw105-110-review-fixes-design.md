# Design: review important-findings fixes (LW-105…LW-110)

**Status:** Approved by owner 2026-07-14; implementation plan next.
**Tickets:** [LW-105](https://linear.app/leifwind/issue/LW-105) [LW-106](https://linear.app/leifwind/issue/LW-106) [LW-107](https://linear.app/leifwind/issue/LW-107) [LW-108](https://linear.app/leifwind/issue/LW-108) [LW-109](https://linear.app/leifwind/issue/LW-109) [LW-110](https://linear.app/leifwind/issue/LW-110) — the six important findings of the 2026-07-14 full-repo review (`docs/superpowers/reviews/2026-07-14-full-repo-review/report.md`).
**Baseline:** main @ `c5ccb64` (code identical to reviewed `ae04371`; all line references below are against that state).

## Owner decisions (2026-07-14, recorded here, not relitigated)

1. **govulncheck gates the tagged release.** A public provider shipping a
   known CVE is worse than a delayed tag. Remedy for a blocked urgent
   release: fix or triage, then re-tag. No override variable.
2. **Delivery: one spec, one plan, three MRs by theme** — (A) CI/build
   gating, (B) acceptance coverage, (C) leifwindtest fix. Each MR closes its
   tickets independently; an acceptance flake never blocks the CI fixes.

## MR-A: CI/build gating — LW-106, LW-107, LW-108

### LW-106 — root-module unit tests

Today no entry point runs the root module's tests (`internal/provider`,
`internal/metadatares`, `internal/lookup`): Makefile `test` runs only the
client module; CI runs only `test:client` and `test:acceptance`.

- **Makefile:** `test` gains a root leg `go test ./... -timeout 5m` *before*
  the client leg (fail fast on the cheap one). The root module's acceptance
  package is TF_ACC-gated, so this stays hermetic.
- **CI:** new `test:unit` job in the existing test stage — plain `golang`
  image, **no dind, no services** — running the root-module tests. Rules:
  MRs + main, same as lint. Deliberately not folded into `test:acceptance`,
  so an acceptance flake never masks a unit regression.

### LW-107 — govulncheck gates the release

- Add `govulncheck` to `release.needs` (`.gitlab-ci.yml:147`).
- Pin govulncheck to a version tag (currently `@latest`, the file's one
  unpinned tool); bumps are deliberate.
- Inline comment stating the policy: a newly published CVE blocks the tag;
  remedy is fix-or-triage then re-tag.
- **Manual owner step (not automatable from the repo):** verify in GitLab
  project settings that `v*` is a protected tag pattern and that the three
  release CI variables (`GPG_PRIVATE_KEY`, `GPG_PASSPHRASE`,
  `GITHUB_MIRROR_TOKEN`) are protected + masked. Until confirmed, anyone who
  can push a v-tag can trigger a signed public release.

### LW-108 — generated-docs drift check

New `docs:drift` CI job (MRs + main, no dind):

```
go run github.com/hashicorp/terraform-plugin-docs/cmd/tfplugindocs@v0.25.0 generate --provider-name leifwind
git diff --exit-code docs/
```

The version pin must equal the Makefile `docs` target's pin; keeping them
equal is a review-time check (a shared variable across the two files is not
worth the indirection).

## MR-B: acceptance coverage — LW-105, LW-109

### LW-105 — strict-create + drift tests for entity and field

Mirror the existing project tests' shape exactly
(`project_acc_test.go:45-109`):

- `TestAccEntityStrictCreate`, `TestAccFieldStrictCreate`: pre-create the
  object out-of-band via `client.UpsertEntity`/`UpsertField`, then apply a
  config for the same name and assert
  `ExpectError: already exists.*terraform import` (the wrap-safe regex the
  resource comments at `entity.go:119` / `field.go:339` claim is exercised —
  after this MR the claim is true).
- `TestAccEntityDriftRecreates`, `TestAccFieldDriftRecreates`: out-of-band
  delete in `PreConfig`, re-apply, assert the object is recreated (new id).
  Field cleanup respects LW-70 ordering: delete FRAGMENT fields before KEY.

Bundled ticket items in the same MR:

- `TestAccMissingCredentials` hermeticity: `t.Setenv` clears for
  `LEIFWIND_OIDC_ISSUER`, `LEIFWIND_CLIENT_ID`, `LEIFWIND_CLIENT_SECRET`
  next to the existing `LEIFWIND_TOKEN` clear
  (`auth_negative_acc_test.go:24`), making the code match its comment.
- Project data-source assertion strength (`project_ds_acc_test.go`): a
  non-matching-pattern `leifwind_projects` block asserting `projects.# == 0`;
  a `unique_key` assertion on the singular source; a both-id-and-name config
  step expecting the same ExactlyOneOf error as the zero-attribute case.

### LW-109 — by-id data-source coverage + assertion gaps

Extend the existing tests in place (`entity_field_ds_acc_test.go`,
`project_ds_acc_test.go`) — no new test functions, no extra stack boots:

- by-id `data.leifwind_entity` / `data.leifwind_field` blocks, asserted via
  `TestCheckResourceAttrPair` against the by-name blocks (covers
  `entity.go:95-106`, `field.go:109-120` including keep-config-id logic).
- Pattern-filtered `leifwind_entities` block asserting `entities.0.name`.
- `fields.0.connection_type` and `fields.0.fragment_name` assertions (the
  FRAGMENT projection at `fields.go:116-120`).
- One `unique_key` assertion per singular data source.

Any new project names must be fresh, never-used-in-suite names (LW-71:
project names are globally unique across tenants; suite convention).

## MR-C: leifwindtest UserToken idempotency — LW-110

Public-module behavioral fix (`client/leifwindtest/usertoken.go`):

- **Fix at the grant site, not mgmtDo.** On the member-add POST
  (`usertoken.go:111-115`) failure, detect ZITADEL's AlreadyExists response
  (HTTP 409 / error body match) and continue; any other error still fails.
  `mgmtDo`'s strict ≥400 semantics stay untouched — other callers rely on
  them.
- **Un-poison `exchangeSetup`:** after the `sync.Once.Do`, fail fast with a
  message pointing at the earlier setup failure if `exchangeAppClientID` is
  empty (today a `t.Fatalf` inside the Once silently degrades the shared
  stack for all later tests in the binary).
- **New test:** call `UserToken` twice on the same Org; both returned tokens
  must be valid (exercises the AlreadyExists path).

Conventional-commit scope `fix(leifwindtest)`. No `client/v*` tag in this
MR — tagging remains LW-68's job.

## Verification / definition of done

| MR | Check |
|----|-------|
| A | `make test` runs root+client legs locally; pipeline shows `test:unit` and `docs:drift` green on the MR; `release.needs` includes govulncheck (verified in pipeline graph); govulncheck pinned. Owner confirms the protected-tag/variables settings check. |
| B | `TF_ACC=1` targeted runs of the four new tests + both extended DS tests pass against the container stack; the `already exists.*terraform import` comments in entity.go/field.go are now true. |
| C | New double-UserToken test passes; full leifwindtest package green; no mgmtDo behavior change (existing tests unaffected). |

All three MRs: lint green over both modules, conventional commits, tickets
LW-105…LW-110 closed on merge.

## Out of scope

- LW-111…LW-116 (grouped minors from the same review).
- M2M token-refresh acceptance test (open decision — dedicated stack cost).
- Backend-side strict-create / TOCTOU race (needs backend support; open
  decision).
- Any `client/v*` release tagging (LW-68).
