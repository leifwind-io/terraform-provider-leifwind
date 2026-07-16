# Dual-PSR Release Automation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Every release-worthy merge to `main` automatically mints the right tags — `client/vX.Y.Z` when `client/**` changed, `vX.Y.Z` when anything buildable changed — and the existing tag-triggered goreleaser job publishes the provider to GitHub for Terraform/OpenTofu registry ingestion.

**Architecture:** Two python-semantic-release (PSR ≥ 10.4 `conventional-monorepo` parser) configurations, each path-scoped, both compute-only. A `release:tag` job on branch pipelines runs a ported copy of the backend's `ci/release_tag.sh` twice (client first, then provider) and mints tags server-side via the GitLab Releases API. Tag pipelines stay as they are: `v*` runs goreleaser → GitHub; `client/v*` spawns **no pipeline** (a Go module tag is consumed straight from git by the module proxy — nothing to build). The `replace ./client` directive in `go.mod` is **kept**, so the provider watches `client/**` too: a client fix correctly bumps both tag families; an `internal/` feat bumps only the provider.

**Tech Stack:** python-semantic-release 10.6.1 (pinned — same as backend), commitizen, uv (hash-locked throwaway venv, digest-pinned image), goreleaser v2.15.4, GitLab CI, GitLab Releases API.

**Reference material (read before starting):**
- Backend implementation this plan ports: `../backend` at `origin/main` — `.gitlab-ci.yml` (`.release-tools`, `release:tag`), `ci/release_tag.sh`, `ci/release-tools.in`/`.txt`, `pyproject.toml` `[tool.semantic_release]`, spec `docs/superpowers/specs/2026-07-15-versioning-automation-design.md`. Run `git -C ../backend fetch origin` first; the local checkout may be behind.
- PSR monorepo docs: https://python-semantic-release.readthedocs.io/en/latest/configuration/configuration-guides/monorepos.html
- Linear: LW-68 (first-release plumbing checklist), LW-107 (govulncheck gate), LW-79/LW-81 (backend precedent).

## Global Constraints

- **Merging this MR performs the first release.** The repo has zero tags; the entire history is bump-worthy, so the first `main` pipeline with `release:tag` mints `client/v0.1.0` + `v0.1.0` and the `v0.1.0` tag pipeline publishes to GitHub. That is the intended bootstrap — but the manual prerequisites below MUST exist before merge, or the tag pipeline fails on the real release.
- **Manual prerequisites (owner, GitLab UI — not code, verified in Task 7):**
  - CI/CD variables (masked + protected): `GPG_PRIVATE_KEY`, `GPG_PASSPHRASE`, `GPG_FINGERPRINT`, `GITHUB_MIRROR_TOKEN`. GPG key must be **RSA** (registry requirement). `GPG_FINGERPRINT` is read by `.goreleaser.yml` and is missing from the LW-68 checklist — do not skip it.
  - Protected tag patterns `v*` **and** `client/v*`, "allowed to create: Maintainers". Without protection, masked+protected variables are not injected into tag pipelines.
  - GitHub fine-grained PAT (`contents: read/write` on `leifwind-io/terraform-provider-leifwind` only) as `GITHUB_MIRROR_TOKEN`.
- **PSR pinned to exactly `10.6.1`** — the backend verified its bump/no-bump behavior empirically at this version; the monorepo parser needs ≥ 10.4.0.
- **All commits in this MR use non-bumping conventional types** (`ci:`, `docs:`, `chore:`) — commit subjects are release inputs from now on.
- **Provider tags are plain `vX.Y.Z`** (both registries require it; goreleaser assumes it). Client tags are `client/vX.Y.Z` (Go submodule rule). Never invent other prefixes.
- **Never read or print `.env` in cleartext** (project rule). For authenticated `glab` calls use: `set -a; source .env; set +a; command glab …`.
- Shell scripts are POSIX `sh` (CI images have no bash guarantee); YAML must pass `command glab ci lint`.

## File Structure

```
ci/release-tools.in        # NEW  — direct deps for release tooling (PSR, commitizen)
ci/release-tools.txt       # NEW  — uv pip compile --generate-hashes output
ci/psr-client.toml         # NEW  — PSR config: tag_format client/v{version}, path_filters client/**
ci/psr-provider.toml       # NEW  — PSR config: tag_format v{version}, path_filters = everything buildable
ci/release_tag.sh          # NEW  — compute-only tag minting (port of backend script, componentized)
.gitlab-ci.yml             # MOD  — workflow rules, .release-tools base, release:tag job,
                           #        commitlint squash-title check, goreleaser checksum pin,
                           #        govulncheck release gate, anchored tag regexes
.goreleaser.yml            # MOD  — GPG passphrase via loopback/stdin (CI has no pinentry)
README.md                  # MOD  — version-logic + commit-contract documentation
```

---

### Task 1: Hash-locked release tooling

**Files:**
- Create: `ci/release-tools.in`
- Create: `ci/release-tools.txt` (generated)

**Interfaces:**
- Produces: `ci/release-tools.txt` consumed by `.release-tools` CI base (Task 4) via `uv pip install --require-hashes`; local venvs in Tasks 2–3 install from it. Binaries used later: `.release-tools-venv/bin/semantic-release`, `.release-tools-venv/bin/cz`.

- [ ] **Step 1: Write `ci/release-tools.in`**

```
# Release tooling for CI only — never part of the Go toolchain or any
# project dependency set. Compiled to release-tools.txt with hashes:
#   uv pip compile --generate-hashes --python-version 3.14 ci/release-tools.in -o ci/release-tools.txt
# PSR is pinned exactly: its bump behavior was verified empirically at
# 10.6.1 by the backend (spec key decision 7); the conventional-monorepo
# parser this repo depends on needs >= 10.4.0. commitizen floats here and
# is locked by the compile output (used only for warn-only commit-parse
# checks in ci/release_tag.sh).
commitizen
python-semantic-release==10.6.1
```

- [ ] **Step 2: Compile the lockfile**

Run:
```bash
uv pip compile --generate-hashes --python-version 3.14 ci/release-tools.in -o ci/release-tools.txt
```
Expected: `ci/release-tools.txt` created, every requirement carries `--hash=sha256:` lines, header names the exact command.

- [ ] **Step 3: Verify a clean hash-checked install works**

Run:
```bash
uv venv /tmp/rt-venv --python 3.14
uv pip install --python /tmp/rt-venv/bin/python --require-hashes -r ci/release-tools.txt
/tmp/rt-venv/bin/semantic-release --version
/tmp/rt-venv/bin/cz version
```
Expected: install succeeds with no hash errors; `semantic-release --version` prints `semantic-release, version 10.6.1`; `cz version` prints a version.

- [ ] **Step 4: Commit**

```bash
git add ci/release-tools.in ci/release-tools.txt
git commit -m "ci: add hash-locked release tooling lockfile (PSR 10.6.1 + commitizen)"
```

---

### Task 2: Path-scoped PSR configurations (+ empirical attribution verification)

**Files:**
- Create: `ci/psr-client.toml`
- Create: `ci/psr-provider.toml`

**Interfaces:**
- Consumes: `/tmp/rt-venv` from Task 1 Step 3 (rebuild it if gone).
- Produces: configs invoked as `semantic-release -c ci/psr-client.toml version --print-tag` (and `…provider…`) by `ci/release_tag.sh` (Task 3). Tag formats produced: `client/v{version}` and `v{version}`.

**Background for the implementer:** PSR's `conventional-monorepo` parser attributes a commit to a package when the commit's **changed file paths** match `path_filters` OR its **conventional scope** starts with `scope_prefix`. `tag_format` tells PSR which existing tags define the package's current version. Per the PSR docs, relative `path_filters` entries resolve **relative to the config file's directory** (docs example: per-package config with `path_filters = ["."]` and `"../../../docs/…"` entries) — hence the `../` prefixes below, since these configs live in `ci/`. Step 3 verifies this empirically and Step 4 is the fallback if the docs' semantics don't hold at 10.6.1.

- [ ] **Step 1: Write `ci/psr-client.toml`**

```toml
# PSR config for the client Go module (compute-only; ci/release_tag.sh mints
# the tag server-side). Scope: commits touching client/** — or scoped
# "(client…)". Tag family client/v{version} is dictated by Go's submodule
# tagging rule, not by choice.
[semantic_release]
tag_format = "client/v{version}"
commit_parser = "conventional-monorepo"
major_on_zero = false
# PSR 10 defaults allow_zero_version to false, which would mint v1.0.0 on
# the first run (backend verified empirically at 10.6.1).
allow_zero_version = true

[semantic_release.remote]
# Compute-only never calls the API, but the origin-URL parser runs
# regardless — keep it truthful.
type = "gitlab"

[semantic_release.commit_parser_options]
# Relative to THIS file's directory (ci/), per PSR monorepo docs.
path_filters = ["../client/**"]
scope_prefix = "client"

[semantic_release.branches.main]
match = "main"
prerelease = false

[semantic_release.branches.prerelease]
match = "^(hotfix|rc)/"
prerelease = true
prerelease_token = "rc"
```

- [ ] **Step 2: Write `ci/psr-provider.toml`**

```toml
# PSR config for the provider binary (compute-only; ci/release_tag.sh mints
# the tag server-side). Scope: everything that changes the shipped binary.
# client/** is DELIBERATELY included: go.mod has `replace ./client`, so the
# provider always builds from the workspace client — a client-only fix
# changes the provider binary and must bump v* too. Tag family v{version}
# is required by both registries (a prefixed provider tag is never
# ingested); do not "align" it with the client's prefixed format.
[semantic_release]
tag_format = "v{version}"
commit_parser = "conventional-monorepo"
major_on_zero = false
allow_zero_version = true

[semantic_release.remote]
type = "gitlab"

[semantic_release.commit_parser_options]
# Relative to THIS file's directory (ci/), per PSR monorepo docs.
path_filters = [
  "../internal/**",
  "../client/**",
  "../main.go",
  "../go.mod",
  "../go.sum",
  "../.goreleaser.yml",
  "../terraform-registry-manifest.json",
]
scope_prefix = "provider"

[semantic_release.branches.main]
match = "main"
prerelease = false

[semantic_release.branches.prerelease]
match = "^(hotfix|rc)/"
prerelease = true
prerelease_token = "rc"
```

- [ ] **Step 3: Verify attribution empirically in a scratch clone (this is the task's test — run it before trusting the configs)**

The parser is young (10.4+); the backend's habit of verifying PSR behavior empirically applies doubly. Baseline tags `v0.9.0`/`client/v0.9.0` make expected bumps unambiguous.

Run:
```bash
SCRATCH=$(mktemp -d)
git clone --quiet . "$SCRATCH/repo"
cd "$SCRATCH/repo"
git config user.email psr-test@example.invalid && git config user.name psr-test
git tag -a v0.9.0 -m v0.9.0 && git tag -a client/v0.9.0 -m client/v0.9.0
PSR=/tmp/rt-venv/bin/semantic-release

# T1: unscoped fix touching only client/ -> BOTH families bump patch
echo scratch > client/psr-scratch-t1.txt
git add client/psr-scratch-t1.txt && git commit -qm "fix: scratch client change"
echo "T1 client:   $($PSR -c ci/psr-client.toml  version --print-tag)"   # expect client/v0.9.1
echo "T1 provider: $($PSR -c ci/psr-provider.toml version --print-tag)"  # expect v0.9.1

# T2: unscoped feat touching only internal/ -> provider bumps minor, client no-op
echo scratch > internal/psr-scratch-t2.txt
git add internal/psr-scratch-t2.txt && git commit -qm "feat: scratch provider change"
echo "T2 client:   $($PSR -c ci/psr-client.toml  version --print-tag)"   # expect client/v0.9.1 (unchanged by T2)
echo "T2 provider: $($PSR -c ci/psr-provider.toml version --print-tag)"  # expect v0.10.0

# T3: CLIENT-scoped fix touching client/ -> provider must STILL see it (path
# attribution must not be overridden by a foreign scope)
git reset -q --hard v0.9.0
echo scratch > client/psr-scratch-t3.txt
git add client/psr-scratch-t3.txt && git commit -qm "fix(client): scoped scratch change"
echo "T3 client:   $($PSR -c ci/psr-client.toml  version --print-tag)"   # expect client/v0.9.1
echo "T3 provider: $($PSR -c ci/psr-provider.toml version --print-tag)"  # expect v0.9.1

# T4: no-bump behavior — docs-type commit only -> both print the CURRENT tag
git reset -q --hard v0.9.0
echo scratch > docs/psr-scratch-t4.txt
git add docs/psr-scratch-t4.txt && git commit -qm "docs: scratch docs change"
echo "T4 client:   $($PSR -c ci/psr-client.toml  version --print-tag)"   # expect client/v0.9.0
echo "T4 provider: $($PSR -c ci/psr-provider.toml version --print-tag)"  # expect v0.9.0
```

Expected: the eight printed tags match the inline comments exactly. `ci/release_tag.sh` (Task 3) depends on the T4 semantics — "no bump due ⇒ `--print-tag` prints the *current* tag" — so T4 failing is a hard stop, not a nice-to-have.

- [ ] **Step 4: Fallback ladder — apply ONLY if Step 3 fails, in this order, re-running Step 3 after each rung**

1. **T1/T2 wrong but errors mention paths:** relative-path base differs from the docs — strip every `../` prefix in both configs' `path_filters` (i.e. `"client/**"`, `"internal/**"`, …, resolved from the repo root / cwd).
2. **T3 wrong (provider missed the client-scoped commit):** delete the `scope_prefix` line from `ci/psr-provider.toml` (path-only attribution). If PSR then errors that `scope_prefix` is required, restore it and go to rung 3.
3. **T3 still wrong:** change `ci/psr-provider.toml` to `commit_parser = "conventional"` and delete its `[semantic_release.commit_parser_options]` table entirely. This over-releases (any `fix:`/`feat:` anywhere bumps the provider, even docs-only ones) but never *misses* a release — the safe direction. Record which rung was applied in the commit message and in the README task.

- [ ] **Step 5: Clean up the scratch clone and commit**

```bash
cd /home/bbruhn/Projects/leifwind/leifwind-stream/terraform-provider-leifwind && rm -rf "$SCRATCH"
git add ci/psr-client.toml ci/psr-provider.toml
git commit -m "ci: add path-scoped PSR configs for client/v* and v* tag families"
```

---

### Task 3: `ci/release_tag.sh` — componentized compute-only tag minting

**Files:**
- Create: `ci/release_tag.sh`

**Interfaces:**
- Consumes: `ci/psr-<component>.toml` (Task 2); `.release-tools-venv/` created by the CI base job (Task 4). CI-provided env: `CI_COMMIT_SHA`, `CI_COMMIT_REF_NAME`, `CI_JOB_TOKEN`, `CI_API_V4_URL`, `CI_PROJECT_ID`, `CI_PROJECT_URL`.
- Produces: invoked as `sh ci/release_tag.sh client` then `sh ci/release_tag.sh provider` (Task 4). Test hook: `RELEASE_TAG_DRY_RUN=1` computes + guards + prints the JSON payload without any API call.

**Provenance:** direct port of `../backend/ci/release_tag.sh` (LW-79, reviewed + battle-tested through v0.2.0). Deviations, each deliberate: (a) component parameter + per-component Release description; (b) tag names contain `/` → URL-encode for the Releases API path; (c) no future-dated "Upcoming Release" stub — on this repo the GitLab Release is bookkeeping (the consumption surface is GitHub for the provider, the git tag itself for the client), so the description is final at mint time; (d) `RELEASE_TAG_DRY_RUN` for local testing. Keep everything else — especially the lineage guard and the Release-object cross-check — verbatim in spirit.

- [ ] **Step 1: Write the script**

```sh
#!/bin/sh
# release:tag — compute the next version for ONE component (client|provider)
# with PSR (compute-only, path-scoped conventional-monorepo parser) and
# create the tag server-side via the GitLab Releases API. Provider tags
# (v*) trigger the goreleaser tag pipeline; client tags (client/v*)
# deliberately spawn no pipeline — a Go module tag is consumed straight
# from git by the module proxy.
# Ported from backend ci/release_tag.sh (LW-79); see the plan document
# docs/superpowers/plans/2026-07-16-dual-psr-release-automation.md for the
# list of deliberate deviations.
set -eu

COMPONENT="${1:?usage: release_tag.sh <client|provider>}"
CONFIG="ci/psr-${COMPONENT}.toml"
[ -f "$CONFIG" ] || { echo "FATAL: $CONFIG not found" >&2; exit 1; }
DRY="${RELEASE_TAG_DRY_RUN:-0}"

API="${CI_API_V4_URL:-}/projects/${CI_PROJECT_ID:-}"
PSR=".release-tools-venv/bin/semantic-release"
CZ=".release-tools-venv/bin/cz"

runbook() {
  echo "RUNBOOK: a red release:tag means A RELEASE MAY BE MISSING, not just a failed job." >&2
  echo "RUNBOOK: retry this job as a Maintainer (a Developer retry 403s on protected tags)." >&2
  echo "RUNBOOK: if this pipeline was superseded by a newer one, do NOT retry — the next" >&2
  echo "RUNBOOK: merge releases everything (bumps are delayed, never lost)." >&2
}

# Scrub commit-derived/remote text before echoing: ANSI escapes in a crafted
# subject could spoof job-log lines.
scrub() { LC_ALL=C tr -cd '[:print:]\n\t'; }

if [ "$DRY" = "0" ]; then
  # PSR refuses detached HEADs — put the ref back on a real branch at exactly
  # the pipeline SHA.
  git checkout -B "$CI_COMMIT_REF_NAME" "$CI_COMMIT_SHA"
  # MANDATORY, never remove: the runner fetches only refs/pipelines/<id> plus
  # the branch — tags are NOT fetched by default. Without them PSR mis-bases
  # the bump and the lineage guard below misjudges.
  git fetch --tags --force --quiet origin
fi

HEAD_SHA="${CI_COMMIT_SHA:-$(git rev-parse HEAD)}"

TAG=$($PSR -c "$CONFIG" version --print-tag) || {
  runbook
  echo "FATAL: 'semantic-release version --print-tag' failed for $COMPONENT" >&2
  exit 1
}
if [ -z "$TAG" ]; then
  runbook
  echo "FATAL: PSR printed nothing for $COMPONENT — refusing to guess (never treat empty output as a bump)" >&2
  exit 1
fi
# Tag names contain '/' for the client — URL-encode for Releases API paths.
ENC_TAG=$(jq -rn --arg t "$TAG" '$t|@uri')

log_no_release() {
  echo "No release for $COMPONENT: $TAG already exists ($1)."
  echo "Non-merge commits since $TAG that do not parse as conventional (warn-only; each is a potentially missed bump):"
  git log --no-merges --format='%h %s' "$TAG..HEAD" | scrub | while IFS= read -r line; do
    subject=${line#* }
    if ! $CZ check --message "$subject" >/dev/null 2>&1; then
      printf '  UNPARSEABLE: %s\n' "$line"
    fi
  done
}

# Disposition of an existing tag decides everything. PSR prints the CURRENT
# tag when no release is due (verified in the plan's Task 2 T4) — so tag
# existence + lineage decides, never version-string equality:
#   on lineage -> green no-op (no bump due; or the benign quick-succession
#                 race; or a superseded pipeline retried after its version
#                 shipped at a newer SHA)
#   unrelated  -> loud red (hijack, or two prerelease branches fighting over
#                 one rc sequence)
#   absent     -> mint it
# Returns 0 = benign no-op, 1 = tag absent; exits 1 = refuse.
tag_disposition() {
  existing=$(git rev-parse -q --verify "refs/tags/$TAG^{commit}") || return 1
  if [ "$existing" = "$HEAD_SHA" ] \
    || git merge-base --is-ancestor "$existing" "$HEAD_SHA" \
    || git merge-base --is-ancestor "$HEAD_SHA" "$existing"; then
    if [ "$DRY" = "0" ]; then
      # Cross-check: every automation-minted tag has a Release object (the
      # POST creates both atomically). A lineage tag WITHOUT one is a
      # hand-pushed tag or a half-finished rollback — bless neither silently.
      if ! curl --fail --silent --show-error --retry 3 --retry-all-errors \
          --header "JOB-TOKEN: $CI_JOB_TOKEN" "$API/releases/$ENC_TAG" >/dev/null; then
        runbook
        echo "FATAL: tag $TAG exists on this lineage but has NO Release object — hand-pushed" >&2
        echo "tag or half-finished rollback. Delete tag + Release together; never reuse the" >&2
        echo "version number (a bad release is superseded, not replaced). Tag info:" >&2
        git for-each-ref --format='%(refname:short) %(objecttype) %(taggername) %(taggerdate)' "refs/tags/$TAG" | scrub >&2
        exit 1
      fi
    fi
    log_no_release "at $existing, same lineage as $HEAD_SHA"
    return 0
  fi
  runbook
  echo "FATAL: tag $TAG exists at UNRELATED commit $existing — refusing (possible hijack, or" >&2
  echo "two prerelease branches fighting over one rc sequence: delete the orphaned rc tag or" >&2
  echo "land the branches sequentially)." >&2
  exit 1
}

if tag_disposition; then exit 0; fi

case "$COMPONENT" in
  client)
    NAME="client $TAG"
    DESC="Go client module release. Consume: go get gitlab.com/leifwind/stream/terraform-provider-leifwind/client@${TAG#client/}"
    ;;
  provider)
    NAME="terraform-provider-leifwind $TAG"
    DESC="Provider release — the tag pipeline publishes signed artifacts to GitHub: https://github.com/leifwind-io/terraform-provider-leifwind/releases/tag/$TAG (both registries ingest from there)."
    ;;
esac

# Injection-proof payload: commit-derived text must never be able to escape
# into JSON or shell — only jq --arg builds the body, sent from a file.
payload=$(mktemp)
jq -n \
  --arg tag_name "$TAG" \
  --arg tag_message "Release $TAG" \
  --arg ref "$HEAD_SHA" \
  --arg name "$NAME" \
  --arg description "$DESC" \
  '{tag_name: $tag_name, tag_message: $tag_message, ref: $ref, name: $name, description: $description}' \
  > "$payload"

if [ "$DRY" = "1" ]; then
  echo "DRY RUN ($COMPONENT) — would POST to <api>/releases:"
  cat "$payload"
  exit 0
fi

response=$(mktemp)
status=$(curl --silent --show-error --output "$response" --write-out '%{http_code}' \
  --header "JOB-TOKEN: $CI_JOB_TOKEN" \
  --header "Content-Type: application/json" \
  --request POST --data @"$payload" \
  "$API/releases") || status=000

if [ "$status" -ge 200 ] && [ "$status" -lt 300 ]; then
  echo "Created $TAG (annotated tag + Release entry); pipelines: $CI_PROJECT_URL/-/pipelines?ref=$ENC_TAG"
  exit 0
fi

# POST failed — if we lost the race between guard and POST, that's still benign.
echo "Releases API POST returned HTTP $status:" >&2
scrub < "$response" >&2 || true
echo >&2
git fetch --tags --force --quiet origin
if tag_disposition; then exit 0; fi
runbook
echo "FATAL: could not create $TAG — check whether the tag/Release now exists before treating this as a failed release." >&2
exit 1
```

- [ ] **Step 2: Static check**

Run: `shellcheck -s sh ci/release_tag.sh` (if shellcheck is unavailable: `sh -n ci/release_tag.sh`)
Expected: no errors (shellcheck info-level notes about intentional word-splitting are acceptable only if reviewed one by one).

- [ ] **Step 3: Dry-run test — mint path (tag absent)**

Reuse the Task 2 scratch-clone recipe (baseline tags `v0.9.0` + `client/v0.9.0`, one `fix: …` commit touching `client/`), then inside the scratch repo:

```bash
mkdir -p .release-tools-venv/bin
ln -s /tmp/rt-venv/bin/semantic-release .release-tools-venv/bin/semantic-release
ln -s /tmp/rt-venv/bin/cz .release-tools-venv/bin/cz
RELEASE_TAG_DRY_RUN=1 sh ci/release_tag.sh client
RELEASE_TAG_DRY_RUN=1 sh ci/release_tag.sh provider
```
Expected: each prints `DRY RUN (<component>) — would POST …` followed by a JSON object with `"tag_name": "client/v0.9.1"` / `"tag_name": "v0.9.1"`, correct `ref` (the scratch HEAD SHA), and the component-specific description. Exit code 0 both times.

- [ ] **Step 4: Dry-run test — no-op path (tag exists on lineage)**

In the same scratch repo: `git tag -a client/v0.9.1 -m x && git tag -a v0.9.1 -m x`, then re-run both dry-run commands from Step 3.
Expected: each prints `No release for <component>: <tag> already exists (…same lineage…)` and exits 0. (The refuse path — tag at an *unrelated* commit — is not locally constructible in a linear-history clone, since every commit is an ancestor of HEAD; that branch is covered by code review plus its backend provenance, where the same logic shipped v0.1.0–v0.2.0.)

- [ ] **Step 5: Clean up scratch, commit**

```bash
rm -rf "$SCRATCH"
git add ci/release_tag.sh
git commit -m "ci: add compute-only dual-tag minting script (backend LW-79 port)"
```

---

### Task 4: CI wiring — `.release-tools` base, `release:tag` job, workflow rules, squash-title lint

**Files:**
- Modify: `.gitlab-ci.yml` (workflow rules block ~lines 15–23; `commitlint` job ~lines 65–73; new blocks after `.go-cache`)

**Interfaces:**
- Consumes: `ci/release-tools.txt` (Task 1), `ci/release_tag.sh` (Task 3).
- Produces: `release:tag` job on `main` + `hotfix/*`|`rc/*` branch pipelines; workflow admits `v*` (anchored, rc-aware) tags and rc branches. `client/v*` tags remain unadmitted (no pipeline) — Task 6 documents this.

- [ ] **Step 1: Replace the workflow tag rule and add the rc-branch rule**

In the `workflow: rules:` block, replace:
```yaml
    # Unanchored (no trailing $) is deliberate, unlike the backend's anchored
    # regex: rc tags (e.g. v0.1.0-rc1) may also release here. See LW-84.
    - if: $CI_COMMIT_TAG =~ /^v\d+\.\d+\.\d+/
```
with:
```yaml
    # hotfix/rc branch pipelines mint prerelease tags via release:tag.
    # Accepted cost: an rc branch with an open MR runs branch + MR pipelines
    # per push — the usual dedup rule would kill exactly the pipeline
    # carrying release:tag. (Backend precedent, LW-79.)
    - if: $CI_COMMIT_BRANCH =~ /^(hotfix|rc)\//
    # Anchored + explicit rc suffix (backend parity; rc format -rc.N is
    # PSR's prerelease_token rendering). client/v* tags are deliberately
    # NOT admitted: a Go module tag is consumed straight from git by the
    # module proxy — there is nothing to build or publish for it.
    - if: $CI_COMMIT_TAG =~ /^v\d+\.\d+\.\d+(-rc\.\d+)?$/
```

- [ ] **Step 2: Add the `.release-tools` base and `release:tag` job** (place `.release-tools` after the `.go-cache` anchor; `release:tag` at the end of the file, after `release`)

```yaml
# Release tooling (commitizen / PSR) lives in a throwaway venv, hash-locked
# via ci/release-tools.txt — never in the Go images. Image digest-pinned:
# this base mints release tags, and the installer performing
# --require-hashes verification must itself be content-addressed.
# (Pattern ported from backend .release-tools, LW-79.)
.release-tools:
  image: ghcr.io/astral-sh/uv:0.11.26-python3.14-trixie-slim@sha256:d21c2dd538d409d050027f67cd09f0b84882cf59072cf77720b15e21f3fe6af5
  variables:
    GIT_DEPTH: "0" # full history: bump computation and tag discovery need it
    UV_CACHE_DIR: $CI_PROJECT_DIR/.uv-cache
    UV_LINK_MODE: copy
  cache:
    - key:
        files: [ci/release-tools.txt]
      paths: [.uv-cache]
  before_script:
    # git: PSR reads repo history; curl/jq: injection-proof Releases API
    # calls (payloads via jq --arg only). The uv image ships none of them.
    - apt-get update -qq && apt-get install -y --no-install-recommends git curl jq ca-certificates
    - uv venv .release-tools-venv
    - uv pip install --python .release-tools-venv/bin/python --require-hashes -r ci/release-tools.txt
```

```yaml
# Loop safety: only release:tag mints tags, and it runs only on BRANCH
# pipelines (rules key on $CI_COMMIT_BRANCH — unset in MR and tag
# pipelines; never $CI_COMMIT_REF_NAME, which would drag a branch named
# rc/x into its MR pipelines). The v* tag pipeline only publishes
# (release job); client/v* tags spawn no pipeline at all.
release:tag:
  extends: .release-tools
  stage: release
  needs:
    - {job: lint, artifacts: false}
    - {job: "test:client", artifacts: false}
    - {job: "test:acceptance", artifacts: false}
    # Unlike the current release job (LW-107), tags are gated on the vuln
    # scan from day one.
    - {job: govulncheck, artifacts: false}
  rules:
    - if: $CI_COMMIT_BRANCH == $CI_DEFAULT_BRANCH
    - if: $CI_COMMIT_BRANCH =~ /^(hotfix|rc)\//
  # Protects the job once started; pending jobs remain cancellable by
  # auto_cancel — acceptable: a superseding pipeline computes a superset bump.
  interruptible: false
  script:
    # Order is load-bearing: client first (the client module tag must exist
    # before any provider artifact that references it — LW-68), provider
    # second. A retry after a partial run is safe: an already-minted client
    # tag resolves to a green no-op via the lineage guard.
    - sh ci/release_tag.sh client
    - sh ci/release_tag.sh provider
```

- [ ] **Step 3: Close the squash-title gap in `commitlint`** — append to its `script:`:

```yaml
    # Squash gap: the squash-merge result subject is the MR title, which no
    # rev-range can see. A draft MR cannot merge, and marking it ready
    # removes the "Draft: " prefix — so lint the un-prefixed title (what
    # will land). (Backend precedent, LW-77.)
    - |
      TITLE="${CI_MERGE_REQUEST_TITLE#Draft:}"
      TITLE="${TITLE# }"
      echo "$TITLE" | npx --yes -p @commitlint/cli@19 -p @commitlint/config-conventional@19 commitlint --extends @commitlint/config-conventional
```
(Inside the `|` literal block every line is its own shell command — the `echo … | npx … commitlint …` pipe must stay on ONE line.)

- [ ] **Step 4: Validate the pipeline config**

Run:
```bash
set -a; source .env; set +a
command glab ci lint
```
Expected: `✓ CI/CD YAML is valid!` (never echo the sourced variables).

- [ ] **Step 5: Commit**

```bash
git add .gitlab-ci.yml
git commit -m "ci: mint client/v* and v* tags from branch pipelines via path-scoped PSR"
```

---

### Task 5: Harden the goreleaser release path

**Files:**
- Modify: `.gitlab-ci.yml` (`variables:` block ~line 35; `release-dry-run` ~lines 128–141; `release` ~lines 142–162)
- Modify: `.goreleaser.yml` (`signs:` block, lines 37–46)

**Interfaces:**
- Consumes: existing `release`/`release-dry-run` jobs; CI variables `GPG_PASSPHRASE`, `GPG_FINGERPRINT` (manual prerequisites).
- Produces: the `v*` tag pipeline that Task 7's rehearsal exercises end-to-end.

- [ ] **Step 1: Pin the goreleaser download by checksum**

Independently re-verify the pinned checksum before trusting it (fetched from the official `checksums.txt` on 2026-07-16; expect exactly the value below):
```bash
curl -fsSL https://github.com/goreleaser/goreleaser/releases/download/v2.15.4/checksums.txt | grep ' goreleaser_Linux_x86_64.tar.gz$'
```
Add to the top-level `variables:` block:
```yaml
  GORELEASER_VERSION: "2.15.4"
  GORELEASER_SHA256: "aae00c71a4a6d55e08cce9273a1516bdce33c1e07cffb7e502fa6fec4377dede"
```
In **both** `release-dry-run` and `release`, replace the `curl … | tar -xz …goreleaser` line with:
```yaml
    - curl -fsSL "https://github.com/goreleaser/goreleaser/releases/download/v${GORELEASER_VERSION}/goreleaser_Linux_x86_64.tar.gz" -o /tmp/goreleaser.tgz
    # Supply-chain gate: the binary that builds and signs release artifacts
    # must itself be content-addressed (backend .release-tools standard).
    - echo "${GORELEASER_SHA256}  /tmp/goreleaser.tgz" | sha256sum -c -
    - tar -xzf /tmp/goreleaser.tgz -C /usr/local/bin goreleaser
```

- [ ] **Step 2: Anchor the release job's tag rule** — in `release: rules:` replace `- if: $CI_COMMIT_TAG =~ /^v\d+\.\d+\.\d+/` with `- if: $CI_COMMIT_TAG =~ /^v\d+\.\d+\.\d+(-rc\.\d+)?$/` (must stay identical to the workflow rule from Task 4).

- [ ] **Step 3: Gate the release on govulncheck (LW-107)** — in `release: needs:` change
`needs: [lint, "test:client", "test:acceptance"]` to
`needs: [lint, "test:client", "test:acceptance", govulncheck]`.

- [ ] **Step 4: Make GPG signing work headless** — in `.goreleaser.yml` replace the `signs:` block with:

```yaml
signs:
  - artifacts: checksum
    # CI has no pinentry: --batch alone hangs/fails on a passphrase-protected
    # key. Loopback + passphrase on stdin (goreleaser `stdin:`) is the
    # documented headless pattern; the passphrase never appears in argv.
    stdin: "{{ .Env.GPG_PASSPHRASE }}"
    args:
      - "--batch"
      - "--pinentry-mode"
      - "loopback"
      - "--passphrase-fd"
      - "0"
      - "--local-user"
      - "{{ .Env.GPG_FINGERPRINT }}"
      - "--output"
      - "${signature}"
      - "--detach-sign"
      - "${artifact}"
```

- [ ] **Step 5: Flip `release-dry-run` to `GOWORK=off` (parity with the real release)**

First verify locally that the workspace-off build works (the `replace ./client` directive resolves the client from the repo, no tag needed):
```bash
GOWORK=off go build ./...
```
Expected: builds cleanly. If it fails, STOP and keep the dry-run as-is (record why in the commit message); otherwise in `release-dry-run` replace:
```yaml
    # NOTE: runs WITH go.work until client/v0.1.0 exists (LW-68 proves GOWORK=off)
    - goreleaser release --snapshot --skip=sign,publish --clean
```
with:
```yaml
    # GOWORK=off for parity with the real release job; `replace ./client`
    # resolves the client module in-repo, so no client tag is required.
    - GOWORK=off goreleaser release --snapshot --skip=sign,publish --clean
```

- [ ] **Step 6: Validate and commit**

Run: `set -a; source .env; set +a; command glab ci lint` → expected `✓ CI/CD YAML is valid!`
Run: `goreleaser check` (if goreleaser is installed locally; otherwise rely on the MR's `release-dry-run` job) → expected: config valid.

```bash
git add .gitlab-ci.yml .goreleaser.yml
git commit -m "ci: checksum-pin goreleaser, gate release on govulncheck, headless GPG signing"
```

---

### Task 6: Document the version logic and commit-message contract

**Files:**
- Modify: `README.md` (append a `## Releases & versioning` section)

**Interfaces:**
- Consumes: the behavior fixed in Tasks 2–5 (adjust wording if a Task 2 Step 4 fallback rung was applied).

- [ ] **Step 1: Append this section to `README.md`** (adjust only if Task 2's fallback changed attribution semantics):

```markdown
## Releases & versioning

There is no version number in this repository. Versions are **computed from
conventional commit messages** and minted as git tags by CI — your commit
subject *is* the version bump.

Two tag families, two release surfaces:

| Tag | Minted when a release-worthy commit touches | Release surface |
|---|---|---|
| `client/vX.Y.Z` | `client/**` (or scope `client`) | The git tag itself — Go consumers `go get …/client@vX.Y.Z` via the module proxy. No pipeline runs on these tags. |
| `vX.Y.Z` | anything buildable: `internal/**`, `client/**`, `main.go`, `go.mod`, `go.sum`, `.goreleaser.yml`, the registry manifest | goreleaser publishes signed archives to the GitHub mirror; registry.terraform.io and the OpenTofu registry ingest from there. |

`client/**` counts for **both** families on purpose: `go.mod` carries
`replace ./client`, so the provider binary always builds from the workspace
client — a client-only fix changes the shipped provider too.

Bump rules (python-semantic-release, configs in `ci/psr-*.toml`):
`fix:` → patch, `feat:` → minor, `feat!:`/`BREAKING CHANGE:` → major is
disabled while on 0.x (`major_on_zero = false`); `docs:`, `ci:`, `chore:`,
`test:`, `refactor:` → no release. Commits are linted on every MR
(including the squash title). Malformed subjects don't break the build at
release time — they are silently *not counted*, which means a missed
release; the MR lint is the real gate.

How it runs: on every `main` (and `hotfix/*`|`rc/*`) branch pipeline,
`release:tag` computes both next versions (compute-only PSR) and creates
due tags server-side via the Releases API — client first, then provider.
Prerelease branches mint `vX.Y.Z-rc.N`. A red `release:tag` job means **a
release may be missing** — see the RUNBOOK lines in the job log; retry as
Maintainer.
```

- [ ] **Step 2: Verify claims against the code** — re-read the final `ci/psr-provider.toml` `path_filters` list and confirm the README table lists exactly the same paths; confirm `major_on_zero`/`prerelease_token` wording matches the TOML.

- [ ] **Step 3: Commit**

```bash
git add README.md
git commit -m "docs: document dual-tag version computation and commit contract"
```

---

### Task 7: Rehearsal + bootstrap runbook (part manual — owner-held secrets)

**Files:** none (operational task; run after the MR is open, before merge).

**Interfaces:**
- Consumes: everything above, pushed as an MR branch.
- Produces: verified prerequisites, a full rc rehearsal (`v0.1.0-rc.1`), and — on merge — the real `v0.1.0` + `client/v0.1.0`.

- [ ] **Step 1: Verify GitLab prerequisites exist (read-only API, safe to run)**

```bash
set -a; source .env; set +a
P="leifwind%2Fstream%2Fterraform-provider-leifwind"
command glab api "projects/$P/variables" | jq -r '.[].key' | sort
command glab api "projects/$P/protected_tags" | jq -r '.[].name'
```
Expected: variables include `GPG_PRIVATE_KEY`, `GPG_PASSPHRASE`, `GPG_FINGERPRINT`, `GITHUB_MIRROR_TOKEN`; protected tags include `v*` and `client/v*`. **If anything is missing, STOP — the owner must create it (LW-68 checklist) before Step 2.** Also per LW-68: remove the stale inverse job-token allowlist entry on the backend project (backend→provider direction, added 2026-07-10).

- [ ] **Step 2: Full rehearsal on an rc branch (mints real rc tags — deliberate)**

```bash
git push origin HEAD:rc/bootstrap
```
Watch the `rc/bootstrap` branch pipeline: `release:tag` must mint `client/v0.1.0-rc.1` and `v0.1.0-rc.1` (whole untagged history is bump-worthy). The `v0.1.0-rc.1` tag pipeline must then run `release`: tag pushed to GitHub, goreleaser publishes a GitHub **prerelease** with per-arch zips, `…_manifest.json`, `SHA256SUMS` + binary `.sig`. Note: `release:tag` needs no protected variables (only `CI_JOB_TOKEN`), but the *tag* pipeline does — which is why tag protection had to include rc-matching `v*`.

- [ ] **Step 3: Rehearse the retry path (the failure you don't want to first meet on v0.1.0)**

Cancel the `release` job mid-run on the rc tag pipeline, then retry it. Expected: retry completes and the GitHub release ends up complete (goreleaser re-run against an existing partial release). If the retry errors on the existing release, record the exact behavior and add `release: mode: replace` under the `release:` key in `.goreleaser.yml`, push to the MR, delete the GitHub rc release + both rc tags, and repeat Step 2.

- [ ] **Step 4: Clean up the rehearsal**

Delete branch `rc/bootstrap`. Optionally delete the rc artifacts — GitHub rc release, GitLab Releases + both rc tags — if you don't want `v0.1.0-rc.1` visible on the registries later (registries list prereleases but never auto-select them). If deleting: delete Release *and* tag together, GitLab and GitHub both.

- [ ] **Step 5: Merge = first release**

Merge the MR (conventional squash title, non-bumping type is fine — history already carries the bump). Expected on the `main` pipeline: `release:tag` mints `client/v0.1.0` + `v0.1.0`; the `v0.1.0` tag pipeline publishes the GitHub release. Then proceed to registry onboarding (LW-68 checklist: signing key upload + publish on registry.terraform.io; issue forms for provider + signing key on `opentofu/registry`; `terraform init`/`tofu init` smoke tests).

---

## Out of scope (tracked elsewhere)

- LW-106 (root-module unit tests never run in CI), LW-108 (docs drift check), LW-102 (backend scanner gating), LW-111/LW-113 (client pre-tag hardening — **do it before merging this plan's MR if at all**, since `client/v0.1.0` freezes the public API), LW-114/LW-115 (minors), registry onboarding UI steps themselves (LW-68 tail).
