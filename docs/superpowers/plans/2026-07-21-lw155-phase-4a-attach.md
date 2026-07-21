# LW-155 Phase 4a: leifwindtest.Attach() + template-based acceptance CI — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Adopt the backend's reusable test stack in `terraform-provider-leifwind`: add `leifwindtest.Attach()` (build a `Stack` from the `LW_TEST_*` env contract, no containers), convert `test:acceptance` to attach mode on the shared `.leifwind-stack` template (no DinD), solve backend reachability for cross-project consumers, and batch in three LW-85 hardening items.

**Architecture:** `Attach()`/`AttachMain()` fill the existing `Stack` struct (`Issuer`, `Audience`, `BackendURL`, `mgmtPAT`, `defaultOrgID`) from `stack.env` variables with a no-op teardown; `Start()` stays for standalone/toxiproxy use. In CI the backend runs **in-script via `uvx` from the private PyPI index** (project 84102869, package `leifwind-stream-backend[server]`) — it cannot be a `services:` entry because its `OIDC_AUDIENCE` is the ZITADEL project id that `leifwind-stack seed` creates *after* services boot, and a services-mode job has no Docker daemon. That in-script pattern is codified as a new `.leifwind-stack-backend` fragment in the backend repo's template (Phase 4a owns those edits).

**Tech Stack:** Go 1.25.8 (two-module repo: root + `./client`), GitLab CI `services:` + `!reference` splices, uv/uvx, OpenTofu 1.10.0, backend `leifwind-stack` CLI (contract v1).

## Global Constraints

- Stack contract: `LW_STACK_CONTRACT_VERSION` major must equal `"1"` — string comparison of the pre-`.` segment (mirror of backend `stack/contract.py::check_contract_version`); error wordings mirrored from `StackContractError` messages quoted in Task 3.
- `client/leifwindtest` is a **public** package: every new exported symbol needs a godoc comment; stdlib-only (no new deps in `client/go.mod`).
- Backend facts (verified 2026-07-21 via GitLab API): template `ci/leifwind-stack.gitlab-ci.yml` IS on backend `main` (commit `95ea094a`); backend project id `84102869`; `leifwind-stream-backend` latest published PyPI version `0.4.0`, console script `leifwind-stack`, extras `[server]` (uvicorn, psycopg-pool, pydantic-settings, pyjwt, jinja2, itsdangerous), `requires-python >= 3.14` (uv auto-downloads the interpreter).
- The local backend checkout at `../backend` has a **stale `main`** (stack harness only in its worktrees). Work against `origin/main` after `git fetch`; do not trust the stale working tree.
- Two repos, two MRs: backend template edits (Task 1) merge first; the provider `include:` targets `ref: main` (4a owns the template — 4b is the stream that pins a SHA). During development the provider include may point at the backend feature branch; flip to `main` before merge.
- Coordination: only edit `ci/leifwind-stack.gitlab-ci.yml` and `docs/stack.md` in the backend repo — nothing else there (Phase 1/4b streams are in flight).
- `test:client` keeps DinD/testcontainers unchanged (toxiproxy needs container control); `-p 1` stays.
- GitLab CLI: `set -a; source .env; set +a; command glab …` (never print the token, never read `.env` in cleartext).
- Commit messages: conventional commits, as in repo history (`feat(...)`, `ci(...)`, `docs(...)`).

**Provider env-contract keys consumed by `Attach()`** (written by `leifwind-stack seed`, `seed.py::_build_values`):

| Key | → `Stack` field | Note |
|---|---|---|
| `LW_TEST_ZITADEL_ISSUER_URL` | `Issuer` | trailing `/` trimmed |
| `LW_TEST_ZITADEL_PROJECT_ID` | `Audience` | = OIDC audience |
| `LW_TEST_BACKEND_URL` | `BackendURL` | |
| `LW_TEST_ZITADEL_MGMT_PAT` | `mgmtPAT` | unexported |
| `LW_TEST_ZITADEL_DEFAULT_ORG_ID` | `defaultOrgID` | unexported |

LW-85 status check (verified in code): member-grant 409-tolerance in `UserToken` already landed (LW-110, `usertoken.go:126-135`) — **no work**, covered by existing `TestUserTokenTwiceSameOrg`. Remaining LW-85 items are Tasks 5 and 6.

---

### Task 1: Backend repo — `.leifwind-stack-backend` fragment + sibling-consumption docs

**Files:**
- Modify: `<backend>/ci/leifwind-stack.gitlab-ci.yml` (from `origin/main`, commit `95ea094a`)
- Modify: `<backend>/docs/stack.md` (section "Consuming the stack from a sibling pipeline")

**Interfaces:**
- Consumes: existing `.leifwind-stack` services block and `.leifwind-stack-seed` script (zitadel-only seed → `stack.env`).
- Produces: `!reference [.leifwind-stack-backend, script]` — starts the published backend on `http://127.0.0.1:8000` and rewrites `stack.env` via the full seed. Variables `LEIFWIND_BACKEND_PKG_VERSION` (default `0.4.0`). Consumers must set `UV_INDEX`, `UV_INDEX_LEIFWIND_BACKEND_USERNAME/PASSWORD` and have `uv`, `libpq5`, `curl` available (documented in the fragment header + docs/stack.md).

- [ ] **Step 1: Branch in the backend repo off origin/main**

```bash
cd /home/bbruhn/Projects/leifwind/leifwind-stream/backend
git fetch origin
git worktree add .claude/worktrees/lw-155-stack-backend-fragment -b feature/lw-155-stack-backend-fragment origin/main
cd .claude/worktrees/lw-155-stack-backend-fragment
```

Expected: worktree on a branch tracking `origin/main` (which HAS `ci/leifwind-stack.gitlab-ci.yml`).

- [ ] **Step 2: Append the `.leifwind-stack-backend` fragment to `ci/leifwind-stack.gitlab-ci.yml`**

Append verbatim (after `.leifwind-stack-seed`):

```yaml

# Backend-as-a-script for cross-project consumers (LW-155, spec §6 Phase 4a).
# The backend cannot be a `services:` entry: a service boots before the job
# script runs, so it cannot learn the seeded OIDC audience — the audience IS
# the ZITADEL project id that `leifwind-stack seed` creates. And a
# services-mode job has no Docker daemon, so the backend *image*
# (LEIFWIND_BACKEND_IMAGE above) is unusable here; the published PyPI package
# is the transport instead. Consumer prerequisites:
#   - `uv` on PATH and libpq5 + curl installed (uv fetches Python >= 3.14
#     itself):
#       apt-get install -y libpq5 curl && curl -LsSf https://astral.sh/uv/install.sh | sh
#   - The private index wired for uvx (job token must be allowlisted on this
#     project — Settings > CI/CD > Job token permissions):
#       UV_INDEX: leifwind-backend=https://gitlab.com/api/v4/projects/84102869/packages/pypi/simple
#       UV_INDEX_LEIFWIND_BACKEND_USERNAME: gitlab-ci-token
#       UV_INDEX_LEIFWIND_BACKEND_PASSWORD: $CI_JOB_TOKEN
#   - `!reference [.leifwind-stack-seed, script]` already ran (stack.env
#     exists with the zitadel-only values).
# After this fragment, re-source stack.env: the full seed rewrites it with
# the real backend URL and the seeded org/project ids.
.leifwind-stack-backend:
  variables:
    # Pinned published backend package (PyPI twin of LEIFWIND_BACKEND_IMAGE).
    LEIFWIND_BACKEND_PKG_VERSION: "0.4.0"
  script:
    - set -a; . ./stack.env; set +a
    - export POSTGRES_URL=postgresql://leifwind:leifwind@stack-db:5432/leifwind
    - export SERIALIZER_SECRET_KEY=ci-stack SERIALIZER_SALT=ci-stack
    - export OIDC_ISSUER=http://zitadel:8080
    - export OIDC_AUDIENCE="$LW_TEST_ZITADEL_PROJECT_ID"
    - uvx --from "leifwind-stream-backend[server]==${LEIFWIND_BACKEND_PKG_VERSION}"
        uvicorn leifwind.stream.backend.main:app
        --host 127.0.0.1 --port 8000 > backend.log 2>&1 &
    - |
      ok=0; for i in $(seq 1 90); do
        curl -fsS http://127.0.0.1:8000/healthz >/dev/null 2>&1 && { ok=1; break; }
        sleep 2
      done
      test $ok -eq 1 || { echo 'backend /healthz timed out' >&2; tail -50 backend.log >&2; exit 1; }
    - uvx --from "leifwind-stream-backend[server]==${LEIFWIND_BACKEND_PKG_VERSION}"
        leifwind-stack seed
        --issuer http://zitadel:8080
        --backend-url http://127.0.0.1:8000
        --pat-file /builds/bootstrap-pat.txt
        --postgres-host stack-db --postgres-port 5432
        --postgres-user leifwind --postgres-password leifwind
        --postgres-database leifwind
        --network-name ci-per-build -o stack.env
```

Design notes locked in here:
- `uvicorn` (not `fastapi run`): uvicorn is pinned in the package's `[server]` extra, so the executable is guaranteed present; the backend's own `loadtest` job uses `fastapi run` from a checkout, which a sibling doesn't have. Env config mirrors that job exactly (`POSTGRES_URL`, `SERIALIZER_*`, `OIDC_ISSUER=http://zitadel:8080` so `iss` matches `LW_TEST_TOKEN_URL`, `OIDC_AUDIENCE=$LW_TEST_ZITADEL_PROJECT_ID`).
- The full (non-`--zitadel-only`) seed is idempotent and rewrites `stack.env` with `LW_TEST_BACKEND_URL=http://127.0.0.1:8000` + seeded org/client values.

- [ ] **Step 3: LW-153 template item — resolve the `rm -f /builds/bootstrap-pat.txt` proposal as docs, not code**

Do **not** add `rm -f /builds/bootstrap-pat.txt` as the first `.leifwind-stack-seed` line. LW-153's safety argument ("start-from-init rewrites the file before discovery answers") does not hold in sequence: the ZITADEL service writes the PAT exactly once during this job's first-init, concurrently with `before_script`; if the `rm` runs *after* that write (slow `uv sync`, cold cache), it deletes a PAT that is never rewritten and `wait --pat-file` times out after 180 s. The unconditional fix is a freshness check inside `leifwind-stack wait` (CLI change, out of 4a scope). Instead:

Add to `docs/stack.md`, at the end of the "PAT handoff via `/builds`" section:

```markdown
> **Self-hosted runner caveat:** on a runner with a *reused* builds volume, a
> stale `/builds/bootstrap-pat.txt` from a previous job could theoretically
> satisfy `wait --pat-file` before this job's ZITADEL finishes first-init.
> SaaS runners get a fresh `/builds` per job and are not affected. Deleting
> the file at script start is NOT safe — it races against this job's own PAT
> write. If you run reused-volume runners, isolate `/builds` per job, or wait
> for the freshness check planned in `leifwind-stack wait` (LW-153).
```

- [ ] **Step 4: Document the sibling wiring in `docs/stack.md`**

In the "Consuming the stack from a sibling pipeline" section, extend the existing splice example with the backend fragment and the index wiring (append after the existing example):

```markdown
### Consumers that need the backend API itself (e.g. provider TF_ACC)

The backend cannot be a `services:` entry (see the comment in
`ci/leifwind-stack.gitlab-ci.yml` — the OIDC audience is created by the
seed). Splice `.leifwind-stack-backend` after the seed fragment and re-source
`stack.env`:

​```yaml
my-acceptance-job:
  variables:
    FF_NETWORK_PER_BUILD: "true"   # service-to-service DNS (zitadel -> zitadel-db)
    UV_INDEX: leifwind-backend=https://gitlab.com/api/v4/projects/84102869/packages/pypi/simple
    UV_INDEX_LEIFWIND_BACKEND_USERNAME: gitlab-ci-token
    UV_INDEX_LEIFWIND_BACKEND_PASSWORD: $CI_JOB_TOKEN
  services:
    - !reference [.leifwind-stack, services]
  before_script:
    - apt-get update -qq && apt-get install -y -qq libpq5 curl
    - curl -LsSf https://astral.sh/uv/install.sh | sh
    - export PATH="$HOME/.local/bin:$PATH"
  script:
    - !reference [.leifwind-stack-seed, script]
    - !reference [.leifwind-stack-backend, script]
    - set -a; . ./stack.env; set +a
    - <your test command>
​```

`uvx` resolves `leifwind-stream-backend` from this project's PyPI registry;
the consuming project's `CI_JOB_TOKEN` must be allowlisted here
(Settings > CI/CD > Job token permissions). Non-index dependencies fall back
to public PyPI.
```

(Remove the `​` zero-width guards around the inner fence when pasting — they only keep this plan's fence intact.)

- [ ] **Step 5: Validate YAML and run the repo's pre-commit hook**

```bash
uvx pre-commit run --files ci/leifwind-stack.gitlab-ci.yml docs/stack.md || pre-commit run --files ci/leifwind-stack.gitlab-ci.yml docs/stack.md
python3 -c "import yaml,sys; yaml.safe_load(open('ci/leifwind-stack.gitlab-ci.yml')); print('yaml ok')" 2>/dev/null || uvx --from pyyaml python -c "import yaml; yaml.safe_load(open('ci/leifwind-stack.gitlab-ci.yml')); print('yaml ok')"
```

Expected: `yaml ok` (note: the repo's `check-yaml` uses `--unsafe` for `*.gitlab-ci.yml` because of `!reference` tags — if plain safe_load rejects the `!reference` tag, that is expected; rely on pre-commit).

- [ ] **Step 6: Commit and push the backend branch, open draft MR**

```bash
git add ci/leifwind-stack.gitlab-ci.yml docs/stack.md
git commit -m "ci(stack): .leifwind-stack-backend fragment — in-script backend for sibling consumers (LW-155)"
git push -u origin feature/lw-155-stack-backend-fragment
set -a; source .env; set +a; command glab mr create --draft --title "Draft: ci(stack): backend-as-script fragment for sibling consumers (LW-155)" --description "Adds .leifwind-stack-backend + sibling docs (UV_INDEX wiring, LW-153 PAT-guard resolution). Consumed by terraform-provider-leifwind test:acceptance." --target-branch main
```

Expected: draft MR in `leifwind/stream/backend`. Do not merge yet — the provider spike (Task 8) validates it first; the provider's `include:` points at this branch until then.

---

### Task 2: Lazy shared-stack boot in `client/leifwindtest/main_test.go`

Hermetic unit tests (Tasks 3-6) live in the `leifwindtest` package, whose `TestMain` currently boots a Docker stack unconditionally — lazy boot makes `go test -run TestContract...` runnable without Docker.

**Files:**
- Modify: `client/leifwindtest/main_test.go`

**Interfaces:**
- Consumes: `StartMain()` (unchanged).
- Produces: `sharedStack(t testing.TB) *Stack` — same name/signature as today, now boots on first call via `sync.Once`. Docker-needing tests are unchanged.

- [ ] **Step 1: Rewrite `main_test.go` with lazy boot**

Preserve the existing package/file comments (e.g. the "each Start(t) boot costs ~60-90s" note); the structural replacement is:

```go
package leifwindtest

import (
	"os"
	"sync"
	"testing"
)

// The package shares one booted stack across tests (each boot costs ~60-90s).
// Boot is lazy: hermetic tests (contract/attach unit tests) must run without
// Docker, so the stack boots on the first sharedStack call, not in TestMain.
var (
	mainOnce    sync.Once
	mainStack   *Stack
	mainCleanup func()
	mainErr     error
)

func sharedStack(t testing.TB) *Stack {
	t.Helper()
	mainOnce.Do(func() {
		mainStack, mainCleanup, mainErr = StartMain()
	})
	if mainErr != nil {
		t.Fatalf("shared stack boot: %v", mainErr)
	}
	return mainStack
}

func TestMain(m *testing.M) {
	code := m.Run()
	if mainCleanup != nil {
		mainCleanup()
	}
	os.Exit(code)
}
```

- [ ] **Step 2: Verify no-Docker run stays hermetic**

```bash
cd client && DOCKER_HOST=tcp://127.0.0.1:1 go test ./leifwindtest -run TestNoSuchTest -v
```

Expected: `ok ... [no tests to run]` in < 5 s, no container boot attempt.

- [ ] **Step 3: Verify compile + vet**

```bash
cd client && go vet ./... && go build ./...
```

Expected: clean. (Full Docker suite runs in Task 10's verification.)

- [ ] **Step 4: Commit**

```bash
git add client/leifwindtest/main_test.go
git commit -m "test(leifwindtest): lazy shared-stack boot so hermetic tests run without Docker"
```

---

### Task 3: Contract helpers (`contract.go`)

**Files:**
- Create: `client/leifwindtest/contract.go`
- Test: `client/leifwindtest/contract_test.go`

**Interfaces:**
- Produces: `type ContractError struct{ Msg string }` (exported — consumers can `errors.As` it), `requireEnv(getenv func(string) string, key string) (string, error)`, `checkContractVersion(getenv func(string) string) error`, `const stackContractMajor = "1"`. Task 4 consumes all three.

- [ ] **Step 1: Write the failing tests**

`client/leifwindtest/contract_test.go`:

```go
package leifwindtest

import (
	"errors"
	"strings"
	"testing"
)

func mapEnv(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestRequireEnvMissingKeyIsContractError(t *testing.T) {
	_, err := requireEnv(mapEnv(nil), "LW_TEST_ZITADEL_MGMT_PAT")
	var ce *ContractError
	if !errors.As(err, &ce) {
		t.Fatalf("want ContractError, got %T: %v", err, err)
	}
	for _, want := range []string{"LW_TEST_ZITADEL_MGMT_PAT", "make stack-seed"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q should contain %q", err, want)
		}
	}
}

func TestRequireEnvPresent(t *testing.T) {
	v, err := requireEnv(mapEnv(map[string]string{"K": "v"}), "K")
	if err != nil || v != "v" {
		t.Fatalf("got (%q, %v)", v, err)
	}
}

func TestCheckContractVersion(t *testing.T) {
	cases := []struct {
		name, version string
		wantErr       string // "" = ok
	}{
		{"exact", "1", ""},
		{"minor", "1.2", ""},
		{"missing", "", "LW_STACK_CONTRACT_VERSION is missing"},
		{"major2", "2.0", "incompatible"},
		{"majorText", "x.1", "incompatible"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := map[string]string{}
			if tc.version != "" {
				env["LW_STACK_CONTRACT_VERSION"] = tc.version
			}
			err := checkContractVersion(mapEnv(env))
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("want ok, got %v", err)
				}
				return
			}
			var ce *ContractError
			if !errors.As(err, &ce) || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("want ContractError containing %q, got %v", tc.wantErr, err)
			}
		})
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd client && DOCKER_HOST=tcp://127.0.0.1:1 go test ./leifwindtest -run 'TestRequireEnv|TestCheckContractVersion' -v
```

Expected: compile FAIL (`undefined: requireEnv`, `ContractError`, ...).

- [ ] **Step 3: Implement `contract.go`**

```go
package leifwindtest

import (
	"fmt"
	"strings"
)

// stackContractMajor is the major version of the stack.env / LW_TEST_* attach
// contract this package speaks. It must track the backend's
// stack/contract.py STACK_CONTRACT_VERSION.
const stackContractMajor = "1"

// ContractError reports a missing or version-incompatible LW_TEST_* attach
// environment. It mirrors the backend's StackContractError: a misconfigured
// attach run must fail with a clear contract error, not a nil-field panic.
type ContractError struct{ Msg string }

func (e *ContractError) Error() string { return e.Msg }

func contractErrorf(format string, args ...any) *ContractError {
	return &ContractError{Msg: fmt.Sprintf(format, args...)}
}

// requireEnv fetches key via getenv, treating empty as missing (Go cannot
// distinguish unset from empty; no contract value is legitimately empty).
func requireEnv(getenv func(string) string, key string) (string, error) {
	v := getenv(key)
	if v == "" {
		return "", contractErrorf(
			"%s is missing from the LW_TEST_* attach environment — source a complete stack.env written by `make stack-seed`",
			key)
	}
	return v, nil
}

func checkContractVersion(getenv func(string) string) error {
	version := getenv("LW_STACK_CONTRACT_VERSION")
	if version == "" {
		return contractErrorf(
			"LW_TEST_* attach variables are set but LW_STACK_CONTRACT_VERSION is missing — source a stack.env written by `make stack-seed`")
	}
	if major, _, _ := strings.Cut(version, "."); major != stackContractMajor {
		return contractErrorf(
			"stack.env contract version %s is incompatible with this consumer (speaks major %s)",
			version, stackContractMajor)
	}
	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
cd client && DOCKER_HOST=tcp://127.0.0.1:1 go test ./leifwindtest -run 'TestRequireEnv|TestCheckContractVersion' -v
```

Expected: PASS, < 5 s, no Docker.

- [ ] **Step 5: Commit**

```bash
git add client/leifwindtest/contract.go client/leifwindtest/contract_test.go
git commit -m "feat(leifwindtest): LW_TEST_* contract helpers mirroring backend StackContractError (LW-155)"
```

---

### Task 4: `Attach()` / `AttachMain()`

**Files:**
- Create: `client/leifwindtest/attach.go`
- Test: `client/leifwindtest/attach_test.go`

**Interfaces:**
- Consumes: `requireEnv`, `checkContractVersion` (Task 3); `Stack` fields incl. unexported `ctx`, `mgmtPAT`, `defaultOrgID`; `(*Stack).cleanup`.
- Produces: `Attach(t testing.TB) *Stack`, `AttachMain() (*Stack, func(), error)`, and unexported `attachFromEnv(getenv func(string) string) (*Stack, error)` (Tasks 5/6 use it to build hermetic test Stacks; Task 7's acctest uses `AttachMain`).

- [ ] **Step 1: Write the failing tests**

`client/leifwindtest/attach_test.go`:

```go
package leifwindtest

import (
	"errors"
	"strings"
	"testing"
)

func attachEnvFixture() map[string]string {
	return map[string]string{
		"LW_STACK_CONTRACT_VERSION":       "1",
		"LW_TEST_ZITADEL_ISSUER_URL":      "http://localhost:8081/",
		"LW_TEST_ZITADEL_PROJECT_ID":      "3141592653589793",
		"LW_TEST_BACKEND_URL":             "http://localhost:8080",
		"LW_TEST_ZITADEL_MGMT_PAT":        "pat-secret",
		"LW_TEST_ZITADEL_DEFAULT_ORG_ID":  "2718281828459045",
	}
}

func TestAttachFromEnvFillsStack(t *testing.T) {
	s, err := attachFromEnv(mapEnv(attachEnvFixture()))
	if err != nil {
		t.Fatal(err)
	}
	if s.Issuer != "http://localhost:8081" { // trailing slash trimmed
		t.Errorf("Issuer = %q", s.Issuer)
	}
	if s.Audience != "3141592653589793" || s.BackendURL != "http://localhost:8080" {
		t.Errorf("Audience/BackendURL = %q/%q", s.Audience, s.BackendURL)
	}
	if s.mgmtPAT != "pat-secret" || s.defaultOrgID != "2718281828459045" {
		t.Error("unexported PAT / default org not filled")
	}
	if s.ctx == nil {
		t.Error("ctx must be non-nil")
	}
	s.cleanup() // no-op teardown must not panic
}

func TestAttachFromEnvMissingKey(t *testing.T) {
	for key := range attachEnvFixture() {
		if key == "LW_STACK_CONTRACT_VERSION" {
			continue // covered by version tests
		}
		t.Run(key, func(t *testing.T) {
			env := attachEnvFixture()
			delete(env, key)
			_, err := attachFromEnv(mapEnv(env))
			var ce *ContractError
			if !errors.As(err, &ce) || !strings.Contains(err.Error(), key) {
				t.Fatalf("want ContractError naming %s, got %v", key, err)
			}
		})
	}
}

func TestAttachFromEnvVersionMismatch(t *testing.T) {
	env := attachEnvFixture()
	env["LW_STACK_CONTRACT_VERSION"] = "2.0"
	if _, err := attachFromEnv(mapEnv(env)); err == nil ||
		!strings.Contains(err.Error(), "incompatible") {
		t.Fatalf("want incompatible-version error, got %v", err)
	}
}

func TestAttachReadsProcessEnv(t *testing.T) {
	for k, v := range attachEnvFixture() {
		t.Setenv(k, v)
	}
	s := Attach(t)
	if s.Issuer != "http://localhost:8081" {
		t.Errorf("Issuer = %q", s.Issuer)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd client && DOCKER_HOST=tcp://127.0.0.1:1 go test ./leifwindtest -run TestAttach -v
```

Expected: compile FAIL (`undefined: attachFromEnv`, `Attach`).

- [ ] **Step 3: Implement `attach.go`**

```go
package leifwindtest

import (
	"context"
	"os"
	"strings"
	"testing"
)

// Attach builds a Stack from the LW_TEST_* environment contract (stack.env,
// written by the backend's `leifwind-stack seed`) instead of booting
// containers. The attached stack is owned by whoever started it — `make -C
// ../backend stack-up stack-seed` locally, or a CI `services:` block — so
// teardown is a no-op and nothing is terminated on cleanup.
//
// The contract's major version (LW_STACK_CONTRACT_VERSION) is checked
// against this package and Attach fails fast on drift or on any missing
// variable. WithToxiproxy is unavailable in attach mode: fault injection
// needs container control, keep Start for that.
func Attach(t testing.TB) *Stack {
	t.Helper()
	s, _, err := AttachMain()
	if err != nil {
		t.Fatalf("leifwindtest.Attach: %v", err)
	}
	return s
}

// AttachMain is the TestMain-friendly variant of Attach, symmetric with
// StartMain. The returned cleanup is safe to call and does nothing.
func AttachMain() (*Stack, func(), error) {
	s, err := attachFromEnv(os.Getenv)
	if err != nil {
		return nil, func() {}, err
	}
	return s, s.cleanup, nil
}

func attachFromEnv(getenv func(string) string) (*Stack, error) {
	if err := checkContractVersion(getenv); err != nil {
		return nil, err
	}
	s := &Stack{ctx: context.Background()}
	for _, f := range []struct {
		key string
		dst *string
	}{
		{"LW_TEST_ZITADEL_ISSUER_URL", &s.Issuer},
		{"LW_TEST_ZITADEL_PROJECT_ID", &s.Audience},
		{"LW_TEST_BACKEND_URL", &s.BackendURL},
		{"LW_TEST_ZITADEL_MGMT_PAT", &s.mgmtPAT},
		{"LW_TEST_ZITADEL_DEFAULT_ORG_ID", &s.defaultOrgID},
	} {
		v, err := requireEnv(getenv, f.key)
		if err != nil {
			return nil, err
		}
		*f.dst = v
	}
	s.Issuer = strings.TrimRight(s.Issuer, "/")
	return s, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
cd client && DOCKER_HOST=tcp://127.0.0.1:1 go test ./leifwindtest -run TestAttach -v
```

Expected: PASS, < 5 s, no Docker.

- [ ] **Step 5: Commit**

```bash
git add client/leifwindtest/attach.go client/leifwindtest/attach_test.go
git commit -m "feat(leifwindtest): Attach()/AttachMain() — fill Stack from the LW_TEST_* contract, no-op teardown (LW-155)"
```

---

### Task 5: LW-85 — context/timeout threading in `mgmtDo` and `fetchToken`

Today the only bound on fixture HTTP calls is the package-level `httpClient` 15 s timeout (`zitadel.go:27`); `mgmtDo`'s 503-retry loop and `fetchToken` ignore `s.ctx` entirely, so a cancelled stack context cannot stop them.

**Files:**
- Modify: `client/leifwindtest/zitadel.go` (`mgmtDo`, ~line 228)
- Modify: `client/leifwindtest/org.go` (`fetchToken` ~line 128 and its callers, e.g. `Org.Token` poll ~line 111)
- Modify: `client/leifwindtest/usertoken.go` (fetchToken call sites in the exchange flow)
- Test: `client/leifwindtest/httpctx_test.go` (new)

**Interfaces:**
- Consumes: `attachFromEnv` (Task 4) to build a hermetic Stack pointed at `httptest`.
- Produces: `fetchToken(ctx context.Context, issuer, clientID, clientSecret string, form url.Values) (string, int, error)` — new leading `ctx` param; `mgmtDo` signature unchanged but honors `s.ctx`.

- [ ] **Step 1: Write the failing test**

`client/leifwindtest/httpctx_test.go`:

```go
package leifwindtest

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

// blockingServer returns a server whose handler blocks until the test ends,
// plus an attach-mode Stack pointed at it.
func blockingServer(t *testing.T) (*httptest.Server, *Stack) {
	t.Helper()
	done := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-done
	}))
	t.Cleanup(func() { close(done); srv.Close() })
	env := attachEnvFixture()
	env["LW_TEST_ZITADEL_ISSUER_URL"] = srv.URL
	s, err := attachFromEnv(mapEnv(env))
	if err != nil {
		t.Fatal(err)
	}
	return srv, s
}

func TestMgmtDoHonorsStackContext(t *testing.T) {
	_, s := blockingServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	s.ctx = ctx
	start := time.Now()
	err := s.mgmtDo("GET", "/management/v1/ping", "", nil, nil)
	if err == nil {
		t.Fatal("want error from cancelled context")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("mgmtDo ignored ctx cancellation, took %v", elapsed)
	}
}

func TestFetchTokenHonorsContext(t *testing.T) {
	srv, _ := blockingServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, _, err := fetchToken(ctx, srv.URL, "id", "secret", url.Values{})
	if err == nil {
		t.Fatal("want error from cancelled context")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("fetchToken ignored ctx cancellation, took %v", elapsed)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd client && DOCKER_HOST=tcp://127.0.0.1:1 go test ./leifwindtest -run 'TestMgmtDoHonors|TestFetchTokenHonors' -v
```

Expected: compile FAIL (fetchToken has no ctx param) — after adapting the fetchToken call to the old signature it would still FAIL on timing (both calls block ~15 s on httpClient timeout, > the 100 ms deadline check... the `> 5s` assertion catches it). The compile failure alone is sufficient evidence.

- [ ] **Step 3: Thread contexts through**

In `mgmtDo` (`zitadel.go`): build every request with `http.NewRequestWithContext(s.ctx, method, url, body)` instead of `http.NewRequest(...)`, and make the 503-retry loop abort early on cancellation — where it currently sleeps between attempts, replace `time.Sleep(time.Second)` with:

```go
		select {
		case <-s.ctx.Done():
			return s.ctx.Err()
		case <-time.After(time.Second):
		}
```

In `org.go`, change `fetchToken` to take a leading context:

```go
func fetchToken(ctx context.Context, issuer, clientID, clientSecret string, form url.Values) (string, int, error) {
```

and build its request with `http.NewRequestWithContext(ctx, ...)`. Update every call site to pass the stack context:

```bash
rg -n 'fetchToken\(' client/leifwindtest
```

— each caller has a `*Stack` in scope (`Org.Token(t, s)` poll loop, `TokenSource`, `UserToken` exchange): pass `s.ctx`. Where a bare poll-deadline loop wraps the call (e.g. `Org.Token`'s 30 s `Secret.NotExisting` retry), keep the loop's own deadline logic unchanged — only the per-request ctx is new.

- [ ] **Step 4: Run tests to verify they pass**

```bash
cd client && DOCKER_HOST=tcp://127.0.0.1:1 go test ./leifwindtest -run 'TestMgmtDoHonors|TestFetchTokenHonors' -v && go vet ./...
```

Expected: PASS in well under 5 s each; vet clean.

- [ ] **Step 5: Commit**

```bash
git add client/leifwindtest
git commit -m "fix(leifwindtest): thread Stack ctx through mgmtDo/fetchToken so hung sockets cannot outlive the caller (LW-85)"
```

---

### Task 6: LW-85 — `exchangeSetup` sync.Once poisoning fix

A failed token-exchange setup currently marks the `sync.Once` done; the post-`Do` guard (`usertoken.go:108-111`) turns that into a clear failure, but every later `UserToken` call on that Stack is permanently dead even for transient causes. Replace the Once with a mutex + success flag so failed setup is retried.

**Files:**
- Modify: `client/leifwindtest/stack.go` (Stack fields ~lines 62-69)
- Modify: `client/leifwindtest/usertoken.go` (setup block ~lines 55-111)
- Test: `client/leifwindtest/usertoken_retry_test.go` (new)

**Interfaces:**
- Consumes: `attachFromEnv` for hermetic Stacks; `mgmtDo` (Task 5 version).
- Produces: `(*Stack).setupTokenExchange() error` (unexported); Stack fields `exchangeMu sync.Mutex`, `exchangeReady bool` replacing `exchangeSetup sync.Once`. `UserToken`'s public signature unchanged.

- [ ] **Step 1: Write the failing test**

`client/leifwindtest/usertoken_retry_test.go`:

```go
package leifwindtest

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

// A failed exchange setup must not poison the Stack: the next call retries.
func TestExchangeSetupRetriesAfterFailure(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	env := attachEnvFixture()
	env["LW_TEST_ZITADEL_ISSUER_URL"] = srv.URL
	s, err := attachFromEnv(mapEnv(env))
	if err != nil {
		t.Fatal(err)
	}

	if err := s.setupTokenExchange(); err == nil {
		t.Fatal("first setup against a 500-ing server must fail")
	}
	after := calls.Load()
	if err := s.setupTokenExchange(); err == nil {
		t.Fatal("second setup must also fail")
	}
	if calls.Load() == after {
		t.Fatal("second setup made no HTTP calls — sync.Once poisoning is back")
	}
	if s.exchangeReady {
		t.Fatal("exchangeReady must stay false after failures")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd client && DOCKER_HOST=tcp://127.0.0.1:1 go test ./leifwindtest -run TestExchangeSetupRetries -v
```

Expected: compile FAIL (`undefined: s.setupTokenExchange`, `s.exchangeReady`).

- [ ] **Step 3: Restructure the setup**

In `stack.go`, replace the `exchangeSetup sync.Once` field (keep and adapt the existing per-Stack rationale comment):

```go
	// exchangeMu/exchangeReady and exchangeApp* back UserToken's
	// one-time-per-Stack RFC 8693 setup (feature flag + impersonation policy
	// + token-exchange OIDC app). Per-Stack, not package-level: each Stack is
	// its own ZITADEL instance/project. A mutex + flag instead of sync.Once:
	// a FAILED setup must not poison the Stack — the next UserToken call
	// retries (LW-85).
	exchangeMu              sync.Mutex
	exchangeReady           bool
	exchangeAppClientID     string
	exchangeAppClientSecret string
```

In `usertoken.go`, extract the body of the current `s.exchangeSetup.Do(func() {...})` closure into a method, converting every `t.Fatalf(...)` inside it to `return fmt.Errorf(...)`:

```go
// setupTokenExchange performs the one-time-per-Stack RFC 8693 prerequisites
// and stores the exchange app credentials. Caller holds exchangeMu.
func (s *Stack) setupTokenExchange() error {
	// ... existing closure body, t.Fatalf("...: %v", err) -> return fmt.Errorf("...: %w", err)
}
```

and replace the `Do` call plus the now-redundant empty-clientID guard (`usertoken.go:108-111`) with:

```go
	s.exchangeMu.Lock()
	if !s.exchangeReady {
		if err := s.setupTokenExchange(); err != nil {
			s.exchangeMu.Unlock()
			t.Fatalf("token-exchange setup: %v", err)
		}
		s.exchangeReady = true
	}
	s.exchangeMu.Unlock()
```

(Reads of `exchangeAppClientID/Secret` after this block are safe: they are written only under the mutex before `exchangeReady` flips.)

- [ ] **Step 4: Run tests to verify they pass**

```bash
cd client && DOCKER_HOST=tcp://127.0.0.1:1 go test ./leifwindtest -run 'TestExchangeSetupRetries|TestAttach|TestRequireEnv|TestCheckContractVersion|TestMgmtDoHonors|TestFetchTokenHonors' -v && go vet ./...
```

Expected: all PASS without Docker; vet clean.

- [ ] **Step 5: Commit**

```bash
git add client/leifwindtest
git commit -m "fix(leifwindtest): retry token-exchange setup after failure instead of poisoning sync.Once (LW-85)"
```

---

### Task 7: acctest — attach/boot dispatch + dedicated-stack skip

**Files:**
- Modify: `internal/acctest/main_test.go` (TestMain / startShared, lines ~11-23)
- Modify: `internal/acctest/acctest.go` (add `attachMode()` helper near the globals, ~line 19)
- Modify: `internal/acctest/auth_negative_acc_test.go` (`TestAccExpiredToken`, ~line 168)

**Interfaces:**
- Consumes: `leifwindtest.AttachMain()` (Task 4), `leifwindtest.StartMain()`.
- Produces: `attachMode() bool` (package-internal — all acceptance tests live in this package). No test-facing API changes; `Stack()`, `NewOrg()`, `ProviderConfig*` unchanged.

- [ ] **Step 1: Add the dispatch**

In `internal/acctest/acctest.go`, next to the `shared` globals:

```go
// attachMode reports whether the LW_TEST_* attach contract is in the
// environment (a sourced stack.env) — the same dispatch key the backend's
// Python fixtures use. Attach: shared long-lived stack, seconds per run.
// Boot: testcontainers, the local default.
func attachMode() bool {
	return os.Getenv("LW_TEST_ZITADEL_ISSUER_URL") != ""
}
```

In `internal/acctest/main_test.go`, inside `startShared()` (keep its current signature and error handling), branch before the existing `StartMain` call:

```go
	if attachMode() {
		s, cleanup, err := leifwindtest.AttachMain()
		if err != nil {
			return err
		}
		shared, sharedCleanup = s, cleanup
		return nil
	}
```

(Adapt the assignment to `startShared`'s actual shape — the current body assigns the same two globals from `leifwindtest.StartMain()`; only the constructor differs.)

- [ ] **Step 2: Skip the instance-mutating test in attach mode**

In `TestAccExpiredToken` (`auth_negative_acc_test.go`), as the first statement after `PreCheck`/existing guards:

```go
	if attachMode() {
		// SetAccessTokenLifetime is instance-wide: unsafe against a shared
		// attached instance, and Start(t) needs Docker which the attach-mode
		// CI job doesn't have. Runs in test:acceptance:boot instead.
		t.Skip("needs a dedicated boot stack; covered by test:acceptance:boot")
	}
```

- [ ] **Step 3: Verify — compile + boot-mode smoke + (if a local stack is up) attach-mode run**

```bash
go vet ./... && go build ./...
# Boot mode unchanged (skips instantly without TF_ACC):
go test ./internal/acctest/... -run TestAccExpiredToken -v
```

Expected: vet/build clean; without `TF_ACC` the suite exits without booting anything (TestMain gate). If the backend stack harness is available locally, additionally:

```bash
make -C ../backend stack-up stack-seed
set -a; . ../backend/stack.env; set +a
TF_ACC=1 TF_ACC_PROVIDER_HOST=registry.opentofu.org TF_ACC_TERRAFORM_PATH=$(command -v tofu) \
  go test ./internal/acctest/... -v -timeout 20m
```

Expected: suite green in attach mode, `TestAccExpiredToken` reported SKIP. (If `../backend` main is still stale locally, `git -C ../backend pull` first; if the harness can't run locally, CI in Task 8 is the gate.)

- [ ] **Step 4: Commit**

```bash
git add internal/acctest
git commit -m "test(acctest): dispatch shared stack to Attach when LW_TEST_* env present; skip instance-wide test in attach mode (LW-155)"
```

---

### Task 8: Provider CI — attach-mode `test:acceptance` + `test:acceptance:boot`

**Files:**
- Modify: `.gitlab-ci.yml` (include block lines 4-6; `test:acceptance` lines 105-115; new `test:acceptance:boot` after it)

**Interfaces:**
- Consumes: backend template `.leifwind-stack` (services), `.leifwind-stack-seed`, `.leifwind-stack-backend` (Task 1); `attachMode` dispatch (Task 7).
- Produces: per-MR attach job named `test:acceptance` (name unchanged — the tag-pipeline `release` job's `needs: ["test:acceptance", ...]` keeps working); boot-mode job `test:acceptance:boot` on schedules/manual.

- [ ] **Step 1: Add the cross-project include**

Extend the existing `include:` block (initially pointing at the Task 1 branch; **flip `ref:` to `main` once the backend MR merges — pre-merge checklist item in Step 5**):

```yaml
include:
  - template: Jobs/SAST.gitlab-ci.yml
  - template: Jobs/Secret-Detection.gitlab-ci.yml
  - project: leifwind/stream/backend
    ref: feature/lw-155-stack-backend-fragment   # TODO(LW-155): -> main before merge
    file: ci/leifwind-stack.gitlab-ci.yml
```

- [ ] **Step 2: Replace `test:acceptance` with the attach-mode job**

```yaml
# Per-MR: attach mode against GitLab services — no DinD; ZITADEL boots in
# parallel with the tofu/uv installs (LW-155). Boot mode lives on in
# test:acceptance:boot below and stays the local default.
test:acceptance:
  stage: test
  image: $GO_IMAGE
  needs: []
  variables:
    # Per-build network: service-to-service DNS (zitadel -> zitadel-db) and
    # stable aliases from the build container.
    FF_NETWORK_PER_BUILD: "true"
    # Private index for the published backend package (see backend
    # docs/stack.md; our CI_JOB_TOKEN is allowlisted on the backend project).
    UV_INDEX: leifwind-backend=https://gitlab.com/api/v4/projects/84102869/packages/pypi/simple
    UV_INDEX_LEIFWIND_BACKEND_USERNAME: gitlab-ci-token
    UV_INDEX_LEIFWIND_BACKEND_PASSWORD: $CI_JOB_TOKEN
  services:
    - !reference [.leifwind-stack, services]
  cache:
    - *go-cache
  before_script:
    - apt-get update -qq && apt-get install -y -qq unzip libpq5 curl ca-certificates
    - curl -fsSL https://github.com/opentofu/opentofu/releases/download/v1.10.0/tofu_1.10.0_linux_amd64.zip -o /tmp/tofu.zip
    - unzip -o /tmp/tofu.zip -d /usr/local/bin tofu
    - curl -LsSf https://astral.sh/uv/install.sh | sh
    - export PATH="$HOME/.local/bin:$PATH"
  script:
    - !reference [.leifwind-stack-seed, script]
    - !reference [.leifwind-stack-backend, script]
    - set -a; . ./stack.env; set +a
    - TF_ACC=1 TF_ACC_PROVIDER_HOST=registry.opentofu.org
      TF_ACC_TERRAFORM_PATH=/usr/local/bin/tofu
      go test ./internal/acctest/... -v -timeout 45m
```

- [ ] **Step 3: Add the boot-mode keeper job**

Directly below, reusing the existing `.containers` DinD anchor:

```yaml
# Boot mode kept alive so it can't rot (same reasoning as the backend's
# test:boot, LW-152): full suite incl. the dedicated Start(t) tests that
# attach mode skips (instance-wide OIDC settings). Scheduled + manual.
test:acceptance:boot:
  stage: test
  <<: *containers
  needs: []
  rules:
    - if: $CI_PIPELINE_SOURCE == "schedule"
    - if: $CI_PIPELINE_SOURCE == "web"
      when: manual
      allow_failure: true
  script:
    - apt-get update -qq && apt-get install -y -qq unzip
    - curl -fsSL https://github.com/opentofu/opentofu/releases/download/v1.10.0/tofu_1.10.0_linux_amd64.zip -o /tmp/tofu.zip
    - unzip -o /tmp/tofu.zip -d /usr/local/bin tofu
    - TF_ACC=1 TF_ACC_PROVIDER_HOST=registry.opentofu.org
      TF_ACC_TERRAFORM_PATH=/usr/local/bin/tofu
      go test ./internal/acctest/... -v -timeout 45m
```

- [ ] **Step 4: Spike — push and iterate on the pipeline (the empirical risks live here)**

```bash
git add .gitlab-ci.yml
git commit -m "ci: test:acceptance in attach mode on .leifwind-stack (no DinD); boot mode -> scheduled test:acceptance:boot (LW-155)"
git push -u origin <branch>
set -a; source .env; set +a; command glab ci status --live
```

Watch for, in order:
1. **Template include resolves** (cross-project include permission).
2. **uvx resolves `leifwind-stream-backend==0.4.0`** from the private index. On 401/403: the provider project's job token must be allowlisted on the backend project (Settings > CI/CD > Job token permissions — it already is for the container registry; package registry uses the same allowlist). On "no matching distribution": check `UV_INDEX` spelling and that uv picked up Python 3.14.
3. **uvicorn starts the backend** — if `/healthz` times out, `backend.log` tail is printed by the fragment; likely causes: missing `libpq5`, or the app needs an entrypoint other than `leifwind.stream.backend.main:app` (verify against backend `loadtest` job which uses the same module path).
4. **PAT handoff**: `wait --pat-file /builds/bootstrap-pat.txt` — docker-executor mounts `/builds` into services; validated by Phase 0b, should just work with `FF_NETWORK_PER_BUILD`.
5. **TF_ACC suite green, `TestAccExpiredToken` SKIP.**

Record the wall-clock delta vs. the last DinD run of `test:acceptance` in the MR description (LW-152 measured; keep the habit).

- [ ] **Step 5: Pre-merge checklist (blocking)**

- Backend MR (Task 1) merged to backend `main`.
- Provider include `ref:` flipped from the feature branch to `main`; pipeline re-run green.
- A scheduled pipeline exists for this project (GitLab UI: CI/CD > Schedules — e.g. nightly) so `test:acceptance:boot` actually runs; note it in the MR if it had to be created.
- `test:client` still green (untouched DinD path — toxiproxy acceptance criterion).

---

### Task 9: Docs — local attach flow + template consumption

**Files:**
- Modify: `README.md` (under `## Development`; `### Local prerequisites` line ~111, `### Common tasks` line ~122)

**Interfaces:**
- Consumes: everything shipped above; no code.

- [ ] **Step 1: Add an attach-mode section to the README**

Insert after `### Common tasks` (line ~122-130 block):

```markdown
### Fast acceptance iteration: attach mode

The acceptance suite has two ways to get a stack. **Boot mode** (default):
`leifwindtest` boots ZITADEL + backend + Postgres via testcontainers on every
run — hermetic, needs Docker, costs ~60-90 s per package. **Attach mode**:
tests attach to an already-running stack via the `LW_TEST_*` environment
contract (`stack.env`), dropping the per-run cost to seconds.

    # once: boot + seed the shared stack from the backend repo
    make -C ../backend stack-up stack-seed

    # per iteration
    set -a; . ../backend/stack.env; set +a
    make testacc

The dispatch is automatic: when `LW_TEST_ZITADEL_ISSUER_URL` is set (i.e.
stack.env is sourced), the suite attaches; otherwise it boots containers.
`leifwindtest.Attach()` fails fast with a contract error if `stack.env` is
incomplete or its `LW_STACK_CONTRACT_VERSION` major differs from the one the
package speaks (currently 1).

Not everything attaches: tests that mutate instance-wide ZITADEL state (and
anything using `WithToxiproxy`) need a dedicated booted stack and skip in
attach mode — CI runs them in the scheduled `test:acceptance:boot` job. In
per-MR CI, `test:acceptance` runs in attach mode against GitLab `services:`
(no Docker-in-Docker); see `.gitlab-ci.yml` and the backend's
`docs/stack.md` for the `.leifwind-stack` template contract.
```

- [ ] **Step 2: Mention Docker is now optional for attach-mode acceptance runs**

In `### Local prerequisites` (line ~111), amend the Docker bullet: Docker is required for `make test` (client/toxiproxy suite) and boot-mode `make testacc`; attach mode instead requires a running backend stack (`make -C ../backend stack-up stack-seed`).

- [ ] **Step 3: Verify docs render + commit**

```bash
grep -n "attach mode" README.md
git add README.md
git commit -m "docs: local attach-mode acceptance flow (LW-155)"
```

---

### Task 10: Finish — full verification, MRs, Linear

- [ ] **Step 1: Full local verification**

```bash
make lint                       # golangci-lint both modules
go test ./... -timeout 5m       # root-module unit tests (acctest gated off without TF_ACC)
cd client && DOCKER_HOST=tcp://127.0.0.1:1 go test ./leifwindtest -run 'TestRequireEnv|TestCheckContractVersion|TestAttach|TestMgmtDoHonors|TestFetchTokenHonors|TestExchangeSetupRetries' -v && cd ..
# With Docker available (proves Start()/toxiproxy path intact — acceptance criterion):
make test
```

Expected: all green. `make test` runs the full client suite incl. `TestUserTokenTwiceSameOrg` (integration cover for Tasks 5/6) and toxiproxy fault injection.

- [ ] **Step 2: Provider MR**

Push, `command glab mr create --draft` targeting `main`, description linking LW-155 + the backend MR, recording: the wall-clock delta, the backend-reachability design decision (in-script uvx, why not `services:`), and the LW-153 `rm -f` deviation (docs instead of code, race rationale).

- [ ] **Step 3: Linear bookkeeping**

- LW-155: attach both MRs, move to In Progress/Review; tick acceptance checkboxes as they land.
- LW-153: comment that the `rm -f` first-line proposal races with the service's own PAT write (analysis in this plan/MR) and needs a `wait` freshness check instead; the `UV_INDEX_*` documentation item is done by the backend MR.
- LW-85: comment that timeouts + Once-poisoning + member-grant items are closed by this MR (member-grant was already fixed as LW-110); remaining LW-85 items (`orgMu` non-deferred unlock, `tok[:20]` panic, LW-70 workaround revert) stay open there.

---

## Self-Review Notes

- **Spec coverage vs. LW-155 acceptance:** `Attach()` contract-checked with no-op teardown → Tasks 3-4. `TF_ACC` green in attach, no DinD → Tasks 7-8. `test:client` toxiproxy still green on `Start()`/DinD → untouched + verified Task 10. Backend reachability documented and working → Tasks 1, 8. Local attach flow in README → Task 9. LW-152 carried-forward "template consumed by ≥1 sibling" → Task 8. Folded LW-153 template items → Task 1 Steps 3-4 (with a reasoned deviation on `rm -f`). LW-85 batch → Tasks 5-6 (+ member-grant verified already fixed).
- **Known unknowns pushed to the spike (Task 8 Step 4):** exact `startShared` body shape (Task 7 adapts), `mgmtDo`/`fetchToken` internals differ slightly from the sketches (Tasks 5-6 describe the delta precisely and tests pin behavior), uvicorn entrypoint and UV index auth (fragment prints `backend.log` on failure).
- **Type consistency:** `AttachMain() (*Stack, func(), error)` used identically in Tasks 4 and 7; `fetchToken(ctx, issuer, clientID, clientSecret, form)` consistent across Tasks 5-6 tests; `attachEnvFixture`/`mapEnv` defined in Tasks 3-4 and reused in 5-6.
