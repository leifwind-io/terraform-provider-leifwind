# terraform-provider-leifwind Implementation Plan (LW-43)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Public Terraform/OpenTofu provider for the leifwind metadata API plus a standalone importable Go client, developed strictly TDD with all wire-touching tests running against the real containerized stack (ZITADEL → backend → PostgreSQL).

**Architecture:** Monorepo with two Go modules — `/` (provider, terraform-plugin-framework only) and `/client` (zero `terraform-plugin-*` deps, first-class public module). The provider talks to the backend exclusively through the client. A shared exported test fixture (`client/leifwindtest`) boots ZITADEL v4.15.3 + backend + PostgreSQL in testcontainers and mints real machine and delegated-user tokens.

**Tech Stack:** Go 1.25, terraform-plugin-framework v1.19.0, terraform-plugin-testing v1.16.0, testcontainers-go, google/uuid, golang-jwt/v5 (fixture only), toxiproxy (fault injection), goreleaser, tfplugindocs v0.25.0, GitLab CI (dind).

## Global Constraints

- Spec (authoritative): `docs/superpowers/specs/2026-07-10-lw43-terraform-provider-design.md`.
- Branch: `feature/lw-43-terraform-provider-leifwind-public-provider-for-the-metadata`. Conventional commits, small and atomic.
- Provider module `gitlab.com/leifwind/stream/terraform-provider-leifwind`; client module `gitlab.com/leifwind/stream/terraform-provider-leifwind/client` (own go.mod). Committed `go.work` uses both.
- Registry address `registry.terraform.io/leifwind-io/leifwind`; provider type name `leifwind`.
- License **MPL-2.0**. Every `.go` file starts with `// SPDX-License-Identifier: MPL-2.0` (goheader-enforced).
- **No `net/http` in `internal/...`** (depguard-enforced) — provider goes through `/client` only.
- **No mocked-backend tests.** Wire-touching tests run against the containerized stack. Pure in-process logic (JSON round-trips, error mapping, query encoding, import-ID parsing, config resolution) may be plain `go test`.
- Every task ends with `go build ./...` green in both modules and `golangci-lint run` clean; each task commits.
- Backend image `registry.gitlab.com/leifwind/stream/backend:edge` (constant, `// TODO(LW-68): pin semver`). Backend listens on **:8000**, healthz at `/healthz` (open), all `/metadata` and `/generic` routes require a Bearer JWT.
- ZITADEL `ghcr.io/zitadel/zitadel:v4.15.3`; both databases `postgres:18-alpine`.
- Backend API facts (verified against backend `main` @ `a828641`): POST = upsert (identity via `object_id` OR natural `unique_key`; immutable-field change → 422; unknown `object_id` → 404); `limit` ≤ 50; opaque `cursor` (embeds pattern+limit — follow-up pages send cursor only, `cursor: null` ⇒ last page); `pattern` = ILIKE substring on `name` (alnum + `_ - .`, ≤ 100 chars); every write accepts query param `dry_run` (transactional rollback); DELETE body `{"detail":"success"}`; cross-tenant → 404; missing/invalid token → 401; unset query params must be omitted (FastAPI rejects empty strings on int params).
- ZITADEL facts: token endpoint `{issuer}/oauth/v2/token`; client_credentials via HTTP basic auth; scopes `openid`, `urn:zitadel:iam:user:resourceowner`, `urn:zitadel:iam:org:project:id:{audience}:aud`; org claim `urn:zitadel:iam:user:resourceowner:id`; refresh 60 s before expiry.
- Container test commands run from the module that owns the test; always pass `-timeout 20m` (client) / `-timeout 45m` (acceptance). Acceptance env: `TF_ACC=1 TF_ACC_TERRAFORM_PATH=$(command -v tofu) TF_ACC_PROVIDER_HOST=registry.opentofu.org`.
- Local prerequisites (document, don't automate): docker daemon; one-time `docker login registry.gitlab.com` with a `read_registry` PAT; Go ≥ 1.25; `tofu` binary.

## File Structure

```
/                                    provider module
├── go.work                          use (., ./client)
├── main.go                          providerserver entry
├── .golangci.yml  .pre-commit-config.yaml  .gitlab-ci.yml  Makefile
├── .goreleaser.yml  terraform-registry-manifest.json  LICENSE  README.md
├── internal/provider/               provider.go, config.go (+tests)
├── internal/metadatares/            project.go, entity.go, field.go, import.go, exists.go (+unit tests)
├── internal/metadatads/             project.go, projects.go, entity.go, entities.go, field.go, fields.go, fragments.go
├── internal/acctest/                acctest.go, main_test.go, *_acc_test.go (ALL acceptance tests live here)
├── docs/                            tfplugindocs output
├── examples/                        provider/, resources/leifwind_*/, data-sources/leifwind_*/
└── client/                          client module
    ├── go.mod
    ├── client.go  auth.go  errors.go  models.go  opts.go  retry.go  metadata.go  generic.go
    ├── *_test.go                    blackbox tests (containerized)
    ├── README.md
    └── leifwindtest/                exported fixture: stack.go, zitadel.go, backend.go, org.go,
                                     usertoken.go, forged.go, toxiproxy.go (+tests)
```

**Interface conventions used throughout** (defined once here; every task's Interfaces block references these):
- `client.New(endpoint string, opts ...Option) (*Client, error)`; `Client{Metadata *MetadataService; Generic *GenericService}`.
- Options: `WithTokenSource(TokenSource)`, `WithHTTPClient(*http.Client)`, `WithUserAgent(string)`, `WithRetry(RetryConfig)`.
- `TokenSource interface { Token(ctx context.Context) (string, error) }`; `StaticToken(string)`; `ClientCredentials(issuer, clientID, clientSecret string, opts ...CredentialOption)`; `WithAudience(string)`, `WithCredentialClock(func() time.Time)` (CredentialOptions).
- Errors: `*APIError{StatusCode, Detail, Method, Path}`, sentinels `ErrNotFound`, `ErrConflict`, `ErrValidation`, `ErrUnauthenticated` via `errors.Is`.
- `ListOpts{Limit int; Pattern, Cursor string}`, `Page[T]{Objects []T; Cursor *string}`, `WriteOption` / `DryRun()`.
- Fixture: `leifwindtest.Start(t testing.TB, opts ...StackOption) *Stack`, `leifwindtest.StartMain(opts ...StackOption) (*Stack, func(), error)`, `Stack{BackendURL, ProxiedBackendURL, Issuer, Audience string}`, `(*Stack).NewOrg(t) *Org`, `Org{ID, MachineUserID, ClientID, ClientSecret string}`, `(*Org).Token(t, s) string`, `(*Org).TokenSource(s) client.TokenSource`, `(*Stack).UserToken(t, org) string`, `(*Stack).ForgedToken(t, org) string`, `WithToxiproxy()` StackOption, `(*Stack).Toxiproxy() *toxiproxy.Proxy`.

---

### Task 1: Branch, modules, license, workspace

**Files:**
- Create: `LICENSE`, `.gitignore`, `go.mod`, `client/go.mod`, `go.work`, `client/doc.go`, `internal/provider/doc.go`

**Interfaces:**
- Consumes: nothing (first task).
- Produces: the two-module workspace every later task builds in.

- [ ] **Step 1: Create the branch**

```bash
cd /home/bbruhn/Projects/leifwind/leifwind-stream/terraform-provider-leifwind
git checkout -b feature/lw-43-terraform-provider-leifwind-public-provider-for-the-metadata
```

- [ ] **Step 2: Fetch the MPL-2.0 license text**

```bash
curl -fsSL https://www.mozilla.org/media/MPL/2.0/index.txt -o LICENSE
head -1 LICENSE
```
Expected: `Mozilla Public License Version 2.0`

- [ ] **Step 3: Write module files**

`.gitignore`:
```
.env
dist/
*.tfstate*
.terraform/
```

`go.mod`:
```
module gitlab.com/leifwind/stream/terraform-provider-leifwind

go 1.25
```

`client/go.mod`:
```
module gitlab.com/leifwind/stream/terraform-provider-leifwind/client

go 1.25
```

`go.work`:
```
go 1.25

use (
	.
	./client
)
```

`client/doc.go`:
```go
// SPDX-License-Identifier: MPL-2.0

// Package client is a standalone Go client for the leifwind metadata API.
// It mirrors the semantics of the backend's Python client: upsert-style
// POSTs resolved by object_id or natural unique_key, cursor pagination,
// and ZITADEL bearer-token authentication.
package client
```

`internal/provider/doc.go`:
```go
// SPDX-License-Identifier: MPL-2.0

// Package provider implements the leifwind Terraform/OpenTofu provider.
package provider
```

- [ ] **Step 4: Verify both modules build**

```bash
go build ./... && (cd client && go build ./...)
```
Expected: exit 0, no output.

- [ ] **Step 5: Commit**

```bash
git add LICENSE .gitignore go.mod client/go.mod go.work client/doc.go internal/provider/doc.go
git commit -m "chore: bootstrap two-module workspace (provider + client), MPL-2.0"
```

---

### Task 2: Lint, pre-commit, Makefile

**Files:**
- Create: `.golangci.yml`, `.pre-commit-config.yaml`, `Makefile`

**Interfaces:**
- Consumes: Task 1 workspace.
- Produces: `make lint`, `make test`, `make testacc`, `make docs` targets used by every later task and CI.

- [ ] **Step 1: Write the failing check**

Lint must fail on a file without the SPDX header. Create a temporary probe:

```bash
cat > /tmp/probe.go.txt <<'EOF'
package client

var Probe = 1
EOF
cp /tmp/probe.go.txt client/probe.go
```

- [ ] **Step 2: Write `.golangci.yml`**

```yaml
version: "2"

run:
  timeout: 5m
  tests: true

linters:
  enable:
    - bodyclose
    - contextcheck
    - depguard
    - errcheck
    - errorlint
    - exhaustive
    - goheader
    - gosec
    - govet
    - nilerr
    - revive
    - staticcheck
    - unused
  settings:
    goheader:
      template: "SPDX-License-Identifier: MPL-2.0"
    depguard:
      rules:
        provider-no-net-http:
          files:
            - "**/internal/**"
          deny:
            - pkg: net/http
              desc: provider packages must use the client module (dogfooding rule, spec §fixed-decisions)
    exhaustive:
      default-signifies-exhaustive: true
  exclusions:
    rules:
      - path: _test\.go
        linters:
          - gosec

formatters:
  enable:
    - gofmt
    - goimports
```

- [ ] **Step 3: Verify lint fails on the probe, then remove it**

```bash
cd client && golangci-lint run ./... ; cd ..
```
Expected: FAIL — goheader error on `client/probe.go` (missing SPDX header).

```bash
rm client/probe.go
cd client && golangci-lint run ./... && cd ..
```
Expected: exit 0.

- [ ] **Step 4: Write `Makefile` and `.pre-commit-config.yaml`**

`Makefile`:
```make
GO_MODULES = . ./client

.PHONY: lint test testacc docs tidy

lint:
	golangci-lint run ./...
	cd client && golangci-lint run ./...

tidy:
	go mod tidy
	cd client && go mod tidy

test:
	cd client && go test ./... -v -timeout 20m

testacc:
	TF_ACC=1 TF_ACC_PROVIDER_HOST=registry.opentofu.org \
	TF_ACC_TERRAFORM_PATH=$$(command -v tofu) \
	go test ./internal/acctest/... -v -timeout 45m

docs:
	go run github.com/hashicorp/terraform-plugin-docs/cmd/tfplugindocs@v0.25.0 \
	  generate --provider-name leifwind
```

`.pre-commit-config.yaml`:
```yaml
repos:
  - repo: https://github.com/compilerla/conventional-pre-commit
    rev: v3.4.0
    hooks:
      - id: conventional-pre-commit
        stages: [commit-msg]
  - repo: local
    hooks:
      - id: gofmt
        name: gofmt
        entry: gofmt -l -w
        language: system
        types: [go]
      - id: golangci-lint
        name: golangci-lint
        entry: make lint
        language: system
        pass_filenames: false
        types: [go]
      - id: go-mod-tidy
        name: go mod tidy check
        entry: bash -c 'make tidy && git diff --exit-code go.mod client/go.mod'
        language: system
        pass_filenames: false
        files: go\.(mod|sum)$
```

- [ ] **Step 5: Verify and commit**

```bash
make lint && pre-commit install --hook-type commit-msg --hook-type pre-commit
git add .golangci.yml .pre-commit-config.yaml Makefile
git commit -m "chore: golangci-lint (strict set, MPL goheader, depguard), pre-commit, Makefile"
```
Expected: lint exit 0; commit accepted by the conventional hook.

---

### Task 3: GitLab CI skeleton

**Files:**
- Create: `.gitlab-ci.yml`

**Interfaces:**
- Consumes: Makefile targets from Task 2.
- Produces: `lint`, `commitlint`, `test:client`, `test:acceptance` jobs; the dind + registry-auth pattern reused by Task 28's release jobs.

- [ ] **Step 1: Write `.gitlab-ci.yml`**

```yaml
stages: [lint, test, release]

variables:
  GO_IMAGE: "golang:1.25"

lint:
  stage: lint
  image: golangci/golangci-lint:v2.4.0
  script:
    - golangci-lint run ./...
    - cd client && golangci-lint run ./...

commitlint:
  stage: lint
  image: node:22-alpine
  rules:
    - if: $CI_PIPELINE_SOURCE == "merge_request_event"
  script:
    - npx --yes -p @commitlint/cli@19 -p @commitlint/config-conventional@19
      commitlint --extends @commitlint/config-conventional
      --from "$CI_MERGE_REQUEST_DIFF_BASE_SHA" --to HEAD

.containers: &containers
  image: $GO_IMAGE
  services:
    - name: docker:27-dind
      alias: docker
  variables:
    DOCKER_HOST: tcp://docker:2375
    DOCKER_TLS_CERTDIR: ""
    TESTCONTAINERS_HOST_OVERRIDE: docker
  before_script:
    # registry auth for the private backend image (job-token allowlist on the backend project)
    - mkdir -p ~/.docker
    - >
      echo "{\"auths\":{\"registry.gitlab.com\":{\"auth\":\"$(echo -n gitlab-ci-token:$CI_JOB_TOKEN | base64 -w0)\"}}}"
      > ~/.docker/config.json

test:client:
  stage: test
  <<: *containers
  script:
    - cd client && go test ./... -v -timeout 30m

test:acceptance:
  stage: test
  <<: *containers
  script:
    - apt-get update -qq && apt-get install -y -qq unzip
    - curl -fsSL https://github.com/opentofu/opentofu/releases/download/v1.10.0/tofu_1.10.0_linux_amd64.zip -o /tmp/tofu.zip
    - unzip -o /tmp/tofu.zip -d /usr/local/bin tofu
    - TF_ACC=1 TF_ACC_PROVIDER_HOST=registry.opentofu.org
      TF_ACC_TERRAFORM_PATH=/usr/local/bin/tofu
      go test ./internal/acctest/... -v -timeout 45m
```

- [ ] **Step 2: Validate the YAML**

```bash
command glab ci lint 2>/dev/null || python3 -c "import yaml,sys; yaml.safe_load(open('.gitlab-ci.yml')); print('yaml ok')"
```
Expected: `yaml ok` (or glab's "Config is valid" if authenticated).

- [ ] **Step 3: Commit and push; verify pipeline**

```bash
git add .gitlab-ci.yml
git commit -m "chore(ci): lint + commitlint + containerized test jobs (dind, job-token registry auth)"
git push -u origin feature/lw-43-terraform-provider-leifwind-public-provider-for-the-metadata
```
Expected: pipeline runs; `lint` green; test jobs green (no tests yet ⇒ `go test` passes trivially). NOTE: `test:*` jobs stay red on image pull until the backend project's job-token allowlist entry exists (LW-68 prerequisite) — acceptable until Task 11 needs the image; flag it if still missing then.

---

### Task 4: Client models + wire format

**Files:**
- Create: `client/models.go`
- Test: `client/models_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `MetadataProject{ObjectID *uuid.UUID; Name, UniqueKey string}`, `MetadataEntity{+ProjectID uuid.UUID}`, `MetadataField{+EntityID uuid.UUID; Config FieldConfig; Connection Connection}`, `DataType` consts (`DataTypeText` … `DataTypeUUID`), `ConnectionType` consts (`ConnectionKey`, `ConnectionFragment`), `FieldConfig{DataType DataType}`, `Connection{Type ConnectionType; FragmentName string}`.

Wire-format facts (from backend pydantic, `metadata/{base,project,entity,field,field_types}.py`): every model serializes `metadata_type` (`"metadata_project"` / `"metadata_entity"` / `"metadata_field"`) and computed `unique_key`; `object_id` is `null` when unset (server output) and should be omitted by us on input; field `config` is `{"data_type":"TEXT"}` (8 enum values `TEXT INTEGER DECIMAL BOOLEAN DATE TIME TIMESTAMP UUID`); field `connection_type` is `{"connection_type":"KEY"}` or `{"connection_type":"FRAGMENT","fragment_name":"x"}`.

- [ ] **Step 1: Write the failing test**

`client/models_test.go`:
```go
// SPDX-License-Identifier: MPL-2.0

package client

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
)

// golden strings mirror the backend's pydantic serialization exactly
const goldenProject = `{"metadata_type":"metadata_project","object_id":"a2ff0efa-64ac-4499-b2a4-99b598ee1c9f","name":"proj_a","unique_key":"proj_a"}`

const goldenFieldFragment = `{"metadata_type":"metadata_field","object_id":null,` +
	`"project_id":"a2ff0efa-64ac-4499-b2a4-99b598ee1c9f",` +
	`"entity_id":"7e57d004-2b97-44e7-8f00-63d2c6b0a50e","name":"body",` +
	`"config":{"data_type":"TEXT"},` +
	`"connection_type":{"connection_type":"FRAGMENT","fragment_name":"content"},` +
	`"unique_key":"a2ff0efa-64ac-4499-b2a4-99b598ee1c9f:7e57d004-2b97-44e7-8f00-63d2c6b0a50e:body"}`

func TestProjectUnmarshalGolden(t *testing.T) {
	var p MetadataProject
	if err := json.Unmarshal([]byte(goldenProject), &p); err != nil {
		t.Fatal(err)
	}
	if p.Name != "proj_a" || p.UniqueKey != "proj_a" || p.ObjectID == nil {
		t.Fatalf("bad decode: %+v", p)
	}
}

func TestProjectMarshalEmitsTypeAndOmitsNilID(t *testing.T) {
	b, err := json.Marshal(MetadataProject{Name: "proj_a"})
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	if m["metadata_type"] != "metadata_project" {
		t.Fatalf("metadata_type missing: %s", b)
	}
	if _, present := m["object_id"]; present {
		t.Fatalf("nil object_id must be omitted on input: %s", b)
	}
}

func TestFieldFragmentRoundTrip(t *testing.T) {
	var f MetadataField
	if err := json.Unmarshal([]byte(goldenFieldFragment), &f); err != nil {
		t.Fatal(err)
	}
	if f.Connection.Type != ConnectionFragment || f.Connection.FragmentName != "content" {
		t.Fatalf("bad connection: %+v", f.Connection)
	}
	if f.Config.DataType != DataTypeText {
		t.Fatalf("bad config: %+v", f.Config)
	}
	b, err := json.Marshal(f)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	conn := m["connection_type"].(map[string]any)
	if conn["fragment_name"] != "content" {
		t.Fatalf("fragment_name lost: %s", b)
	}
}

func TestConnectionKeyOmitsFragmentName(t *testing.T) {
	b, err := json.Marshal(Connection{Type: ConnectionKey})
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `{"connection_type":"KEY"}` {
		t.Fatalf("got %s", b)
	}
}

func TestEntityRoundTrip(t *testing.T) {
	pid := uuid.New()
	e := MetadataEntity{ProjectID: pid, Name: "book"}
	b, _ := json.Marshal(e)
	var back MetadataEntity
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if back.ProjectID != pid || back.Name != "book" {
		t.Fatalf("round trip lost data: %+v", back)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd client && go get github.com/google/uuid@latest && go test ./... -run 'TestProject|TestField|TestConnection|TestEntity' -v
```
Expected: FAIL — `undefined: MetadataProject` (and friends).

- [ ] **Step 3: Write minimal implementation**

`client/models.go`:
```go
// SPDX-License-Identifier: MPL-2.0

package client

import (
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
)

// DataType is the backend field data type discriminator.
type DataType string

const (
	DataTypeText      DataType = "TEXT"
	DataTypeInteger   DataType = "INTEGER"
	DataTypeDecimal   DataType = "DECIMAL"
	DataTypeBoolean   DataType = "BOOLEAN"
	DataTypeDate      DataType = "DATE"
	DataTypeTime      DataType = "TIME"
	DataTypeTimestamp DataType = "TIMESTAMP"
	DataTypeUUID      DataType = "UUID"
)

// FieldConfig mirrors the pydantic discriminated union {"data_type": ...}.
type FieldConfig struct {
	DataType DataType `json:"data_type"`
}

// ConnectionType is the backend connection discriminator.
type ConnectionType string

const (
	ConnectionKey      ConnectionType = "KEY"
	ConnectionFragment ConnectionType = "FRAGMENT"
)

// Connection mirrors {"connection_type":"KEY"} /
// {"connection_type":"FRAGMENT","fragment_name":"x"}.
type Connection struct {
	Type         ConnectionType
	FragmentName string
}

type connectionWire struct {
	ConnectionType ConnectionType `json:"connection_type"`
	FragmentName   *string        `json:"fragment_name,omitempty"`
}

func (c Connection) MarshalJSON() ([]byte, error) {
	w := connectionWire{ConnectionType: c.Type}
	if c.Type == ConnectionFragment {
		if c.FragmentName == "" {
			return nil, fmt.Errorf("connection FRAGMENT requires FragmentName")
		}
		w.FragmentName = &c.FragmentName
	}
	return json.Marshal(w)
}

func (c *Connection) UnmarshalJSON(b []byte) error {
	var w connectionWire
	if err := json.Unmarshal(b, &w); err != nil {
		return err
	}
	c.Type = w.ConnectionType
	c.FragmentName = ""
	if w.FragmentName != nil {
		c.FragmentName = *w.FragmentName
	}
	return nil
}

// MetadataProject mirrors the backend MetadataProject model.
type MetadataProject struct {
	ObjectID  *uuid.UUID
	Name      string
	UniqueKey string // server-computed, read-only
}

type projectWire struct {
	MetadataType string     `json:"metadata_type"`
	ObjectID     *uuid.UUID `json:"object_id,omitempty"`
	Name         string     `json:"name"`
	UniqueKey    string     `json:"unique_key,omitempty"`
}

func (p MetadataProject) MarshalJSON() ([]byte, error) {
	return json.Marshal(projectWire{"metadata_project", p.ObjectID, p.Name, p.UniqueKey})
}

func (p *MetadataProject) UnmarshalJSON(b []byte) error {
	var w projectWire
	if err := json.Unmarshal(b, &w); err != nil {
		return err
	}
	*p = MetadataProject{ObjectID: w.ObjectID, Name: w.Name, UniqueKey: w.UniqueKey}
	return nil
}

// MetadataEntity mirrors the backend MetadataEntity model.
type MetadataEntity struct {
	ObjectID  *uuid.UUID
	ProjectID uuid.UUID
	Name      string
	UniqueKey string
}

type entityWire struct {
	MetadataType string     `json:"metadata_type"`
	ObjectID     *uuid.UUID `json:"object_id,omitempty"`
	ProjectID    uuid.UUID  `json:"project_id"`
	Name         string     `json:"name"`
	UniqueKey    string     `json:"unique_key,omitempty"`
}

func (e MetadataEntity) MarshalJSON() ([]byte, error) {
	return json.Marshal(entityWire{"metadata_entity", e.ObjectID, e.ProjectID, e.Name, e.UniqueKey})
}

func (e *MetadataEntity) UnmarshalJSON(b []byte) error {
	var w entityWire
	if err := json.Unmarshal(b, &w); err != nil {
		return err
	}
	*e = MetadataEntity{ObjectID: w.ObjectID, ProjectID: w.ProjectID, Name: w.Name, UniqueKey: w.UniqueKey}
	return nil
}

// MetadataField mirrors the backend MetadataField model.
type MetadataField struct {
	ObjectID   *uuid.UUID
	ProjectID  uuid.UUID
	EntityID   uuid.UUID
	Name       string
	Config     FieldConfig
	Connection Connection
	UniqueKey  string
}

type fieldWire struct {
	MetadataType string      `json:"metadata_type"`
	ObjectID     *uuid.UUID  `json:"object_id,omitempty"`
	ProjectID    uuid.UUID   `json:"project_id"`
	EntityID     uuid.UUID   `json:"entity_id"`
	Name         string      `json:"name"`
	Config       FieldConfig `json:"config"`
	Connection   Connection  `json:"connection_type"`
	UniqueKey    string      `json:"unique_key,omitempty"`
}

func (f MetadataField) MarshalJSON() ([]byte, error) {
	return json.Marshal(fieldWire{"metadata_field", f.ObjectID, f.ProjectID, f.EntityID, f.Name, f.Config, f.Connection, f.UniqueKey})
}

func (f *MetadataField) UnmarshalJSON(b []byte) error {
	var w fieldWire
	if err := json.Unmarshal(b, &w); err != nil {
		return err
	}
	*f = MetadataField{ObjectID: w.ObjectID, ProjectID: w.ProjectID, EntityID: w.EntityID,
		Name: w.Name, Config: w.Config, Connection: w.Connection, UniqueKey: w.UniqueKey}
	return nil
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
cd client && go test ./... -run 'TestProject|TestField|TestConnection|TestEntity' -v
```
Expected: PASS (5 tests).

- [ ] **Step 5: Commit**

```bash
git add client/models.go client/models_test.go client/go.mod client/go.sum
git commit -m "feat(client): metadata models with pydantic-exact wire format"
```

---

### Task 5: Client error model

**Files:**
- Create: `client/errors.go`
- Test: `client/errors_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `APIError{StatusCode int; Detail, Method, Path string}` with `Error()`/`Unwrap()`; sentinels `ErrNotFound`, `ErrConflict`, `ErrValidation`, `ErrUnauthenticated`; `newAPIError(method, path string, status int, body []byte) *APIError` (used by Task 12's request funnel).

- [ ] **Step 1: Write the failing test**

`client/errors_test.go`:
```go
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
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd client && go test ./... -run 'TestStatus|TestDetail|TestAPIError' -v
```
Expected: FAIL — `undefined: newAPIError`, `undefined: ErrNotFound`.

- [ ] **Step 3: Write minimal implementation**

`client/errors.go`:
```go
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
```

- [ ] **Step 4: Run test to verify it passes**

```bash
cd client && go test ./... -run 'TestStatus|TestDetail|TestAPIError' -v
```
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add client/errors.go client/errors_test.go
git commit -m "feat(client): APIError with sentinel unwrapping and FastAPI detail parsing"
```

---

### Task 6: List/write options + query encoding

**Files:**
- Create: `client/opts.go`
- Test: `client/opts_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `ListOpts{Limit int; Pattern, Cursor string}` with unexported `values() url.Values`; `Page[T any]{Objects []T; Cursor *string}` (JSON tags `objects`/`cursor`); `WriteOption`, `DryRun()`, unexported `writeValues(opts []WriteOption) url.Values`.

- [ ] **Step 1: Write the failing test**

`client/opts_test.go`:
```go
// SPDX-License-Identifier: MPL-2.0

package client

import "testing"

func TestListOptsOmitsZeroValues(t *testing.T) {
	if got := (ListOpts{}).values().Encode(); got != "" {
		t.Fatalf("zero opts must encode empty, got %q", got)
	}
	got := (ListOpts{Limit: 25, Pattern: "alpha", Cursor: "c1"}).values().Encode()
	want := "cursor=c1&limit=25&pattern=alpha"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestDryRunOption(t *testing.T) {
	if got := writeValues(nil).Encode(); got != "" {
		t.Fatalf("no opts must encode empty, got %q", got)
	}
	if got := writeValues([]WriteOption{DryRun()}).Encode(); got != "dry_run=true" {
		t.Fatalf("got %q", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd client && go test ./... -run 'TestListOpts|TestDryRun' -v
```
Expected: FAIL — `undefined: ListOpts`.

- [ ] **Step 3: Write minimal implementation**

`client/opts.go`:
```go
// SPDX-License-Identifier: MPL-2.0

package client

import (
	"net/url"
	"strconv"
)

// ListOpts controls list endpoints. Zero values are omitted from the query
// string (the backend rejects empty-string values on typed params). Limit
// must be ≤ 50 (backend MAX_PAGE_SIZE); the server default is 50.
type ListOpts struct {
	Limit   int
	Pattern string
	Cursor  string
}

func (o ListOpts) values() url.Values {
	v := url.Values{}
	if o.Limit > 0 {
		v.Set("limit", strconv.Itoa(o.Limit))
	}
	if o.Pattern != "" {
		v.Set("pattern", o.Pattern)
	}
	if o.Cursor != "" {
		v.Set("cursor", o.Cursor)
	}
	return v
}

// Page is one page of a cursor-paginated listing. Cursor == nil means the
// last page. The cursor is opaque and embeds pattern+limit — pass ONLY the
// cursor on follow-up calls.
type Page[T any] struct {
	Objects []T     `json:"objects"`
	Cursor  *string `json:"cursor"`
}

type writeSettings struct {
	dryRun bool
}

// WriteOption modifies write requests (upserts and deletes).
type WriteOption func(*writeSettings)

// DryRun makes the backend validate and then roll back the transaction.
func DryRun() WriteOption {
	return func(w *writeSettings) { w.dryRun = true }
}

func writeValues(opts []WriteOption) url.Values {
	var s writeSettings
	for _, o := range opts {
		o(&s)
	}
	v := url.Values{}
	if s.dryRun {
		v.Set("dry_run", "true")
	}
	return v
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
cd client && go test ./... -run 'TestListOpts|TestDryRun' -v
```
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add client/opts.go client/opts_test.go
git commit -m "feat(client): list/write options with zero-value query omission"
```

---

### Task 7: Fixture part 1 — ZITADEL stack boot

**Files:**
- Create: `client/leifwindtest/stack.go`, `client/leifwindtest/zitadel.go`
- Test: `client/leifwindtest/stack_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `Start(t testing.TB, opts ...StackOption) *Stack`, `StartMain(opts ...StackOption) (*Stack, func(), error)`, `Stack{Issuer, Audience string}` (+ unexported `mgmtPAT`, `defaultOrgID`, `network`, `zitadelAlias = "zitadel"`, `httpc *http.Client`), `(*Stack).mgmtDo(method, path, orgID string, body, out any) error` with 503-retry (used by Tasks 8/10).

This ports `backend/src/leifwind/stream/backend/testing.py:387-621` to Go. Port faithfully — every quirk there is empirical (distroless image, named volume for `/machinekey`, PAT via archive API, `ZITADEL_TLS_ENABLED=false`, `user=0`, EXTERNALPORT pre-selected free port, 503 settling retries).

- [ ] **Step 1: Write the failing test**

`client/leifwindtest/stack_test.go`:
```go
// SPDX-License-Identifier: MPL-2.0

package leifwindtest

import (
	"net/http"
	"testing"
)

func TestStackBootsZitadel(t *testing.T) {
	s := Start(t)
	resp, err := http.Get(s.Issuer + "/.well-known/openid-configuration")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("discovery returned %d", resp.StatusCode)
	}
	if s.Audience == "" {
		t.Fatal("audience (API project id) not set")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd client && go get github.com/testcontainers/testcontainers-go@latest && go test ./leifwindtest/ -run TestStackBootsZitadel -v -timeout 20m
```
Expected: FAIL — `undefined: Start`.

- [ ] **Step 3: Write minimal implementation**

`client/leifwindtest/stack.go`:
```go
// SPDX-License-Identifier: MPL-2.0

// Package leifwindtest boots a real leifwind stack (ZITADEL v4.15.3,
// backend, PostgreSQL) in testcontainers for blackbox tests. It is a
// public package: client consumers may use it for their own tests.
package leifwindtest

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/network"
)

// BackendImage is the backend under test.
// TODO(LW-68): pin semver once the backend cuts a release.
const BackendImage = "registry.gitlab.com/leifwind/stream/backend:edge"

const (
	zitadelImage  = "ghcr.io/zitadel/zitadel:v4.15.3"
	postgresImage = "postgres:18-alpine"
	zitadelAlias  = "zitadel"
	// dev/test-only masterkey, mirrors backend testing.py
	zitadelMasterkey = "MasterkeyNeedsToHave32Characters"
	patPath          = "/machinekey/bootstrap-pat.txt"
)

type stackSettings struct {
	toxiproxy bool
}

// StackOption configures Start/StartMain.
type StackOption func(*stackSettings)

// Stack is a running leifwind stack.
type Stack struct {
	Issuer            string // ZITADEL external URL (token iss)
	Audience          string // ZITADEL API project id (token aud)
	BackendURL        string // set by startBackend (Task 11)
	ProxiedBackendURL string // set by WithToxiproxy (Task 17)

	ctx          context.Context
	mgmtPAT      string
	defaultOrgID string
	net          *testcontainers.DockerNetwork
	zitadel      testcontainers.Container
	teardown     []func()
}

// Start boots the stack and registers cleanup on t.
func Start(t testing.TB, opts ...StackOption) *Stack {
	t.Helper()
	s, cleanup, err := StartMain(opts...)
	if err != nil {
		t.Fatalf("leifwindtest: %v", err)
	}
	t.Cleanup(cleanup)
	return s
}

// StartMain is the TestMain-friendly variant (no testing.TB required).
func StartMain(opts ...StackOption) (*Stack, func(), error) {
	var settings stackSettings
	for _, o := range opts {
		o(&settings)
	}
	ctx := context.Background()
	s := &Stack{ctx: ctx}

	net, err := network.New(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("network: %w", err)
	}
	s.net = net
	s.deferCleanup(func() { _ = net.Remove(ctx) })

	if err := s.startZitadel(); err != nil {
		s.cleanup()
		return nil, nil, err
	}
	if err := s.startBackend(settings.toxiproxy); err != nil {
		s.cleanup()
		return nil, nil, err
	}
	return s, s.cleanup, nil
}

func (s *Stack) deferCleanup(f func()) { s.teardown = append(s.teardown, f) }

func (s *Stack) cleanup() {
	for i := len(s.teardown) - 1; i >= 0; i-- {
		s.teardown[i]()
	}
	s.teardown = nil
}

func terminate(ctx context.Context, c testcontainers.Container) func() {
	return func() {
		tctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		_ = c.Terminate(tctx)
	}
}
```

`client/leifwindtest/zitadel.go` (backend startup lands in Task 11 — until then `startBackend` is a stub returning nil):
```go
// SPDX-License-Identifier: MPL-2.0

package leifwindtest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/container"
	"github.com/google/uuid"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

func freePort() (int, error) {
	l, err := net.Listen("tcp", ":0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

func (s *Stack) startBackend(withToxiproxy bool) error { return nil } // implemented in Task 11

func (s *Stack) startZitadel() error {
	ctx := s.ctx

	// zitadel-db
	zdb, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:    postgresImage,
			Networks: []string{s.net.Name},
			NetworkAliases: map[string][]string{s.net.Name: {"zitadel-db"}},
			Env: map[string]string{
				"POSTGRES_USER":     "zitadel",
				"POSTGRES_PASSWORD": "zitadel",
				"POSTGRES_DB":       "zitadel",
			},
			WaitingFor: wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).WithStartupTimeout(60 * time.Second),
		},
		Started: true,
	})
	if err != nil {
		return fmt.Errorf("zitadel-db: %w", err)
	}
	s.deferCleanup(terminate(ctx, zdb))

	// EXTERNALDOMAIN/PORT chicken-and-egg: pick the host port up front.
	provider, err := testcontainers.NewDockerProvider()
	if err != nil {
		return err
	}
	defer provider.Close()
	host, err := provider.DaemonHost(ctx)
	if err != nil {
		return err
	}
	port, err := freePort()
	if err != nil {
		return err
	}
	s.Issuer = fmt.Sprintf("http://%s:%d", host, port)

	volumeName := "zitadel-machinekey-" + uuid.NewString()[:12]

	zc, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:    zitadelImage,
			Networks: []string{s.net.Name},
			NetworkAliases: map[string][]string{s.net.Name: {zitadelAlias}},
			Cmd:      []string{"start-from-init", "--masterkeyFromEnv", "--tlsMode", "disabled"},
			Env: map[string]string{
				"ZITADEL_MASTERKEY":                                      zitadelMasterkey,
				"ZITADEL_DATABASE_POSTGRES_HOST":                         "zitadel-db",
				"ZITADEL_DATABASE_POSTGRES_PORT":                         "5432",
				"ZITADEL_DATABASE_POSTGRES_DATABASE":                     "zitadel",
				"ZITADEL_DATABASE_POSTGRES_USER_USERNAME":                "zitadel",
				"ZITADEL_DATABASE_POSTGRES_USER_PASSWORD":                "zitadel",
				"ZITADEL_DATABASE_POSTGRES_USER_SSL_MODE":                "disable",
				"ZITADEL_DATABASE_POSTGRES_ADMIN_USERNAME":               "zitadel",
				"ZITADEL_DATABASE_POSTGRES_ADMIN_PASSWORD":               "zitadel",
				"ZITADEL_DATABASE_POSTGRES_ADMIN_SSL_MODE":               "disable",
				"ZITADEL_EXTERNALDOMAIN":                                 host,
				"ZITADEL_EXTERNALPORT":                                   strconv.Itoa(port),
				"ZITADEL_EXTERNALSECURE":                                 "false",
				"ZITADEL_TLS_ENABLED":                                    "false",
				"ZITADEL_FIRSTINSTANCE_ORG_NAME":                         "leifwind-test",
				"ZITADEL_FIRSTINSTANCE_ORG_HUMAN_USERNAME":               "admin",
				"ZITADEL_FIRSTINSTANCE_ORG_HUMAN_PASSWORD":               "Password1!",
				"ZITADEL_FIRSTINSTANCE_ORG_MACHINE_MACHINE_USERNAME":     "bootstrap",
				"ZITADEL_FIRSTINSTANCE_ORG_MACHINE_MACHINE_NAME":         "bootstrap",
				"ZITADEL_FIRSTINSTANCE_PATPATH":                          patPath,
				"ZITADEL_FIRSTINSTANCE_ORG_MACHINE_PAT_EXPIRATIONDATE":   "2035-01-01T00:00:00Z",
			},
			HostConfigModifier: func(hc *container.HostConfig) {
				// container runs as root to create /machinekey (dev/test only);
				// named docker-managed volume — a host bind would resolve on the
				// daemon's filesystem under dind, silently breaking PAT readback.
				hc.Mounts = append(hc.Mounts, mount.Mount{
					Type: mount.TypeVolume, Source: volumeName, Target: "/machinekey",
				})
				hc.PortBindings = nat.PortMap{
					"8080/tcp": []nat.PortBinding{{HostIP: "0.0.0.0", HostPort: strconv.Itoa(port)}},
				}
			},
			ConfigModifier: func(c *container.Config) { c.User = "0" },
			ExposedPorts:   []string{"8080/tcp"},
		},
		Started: true,
	})
	if err != nil {
		return fmt.Errorf("zitadel: %w", err)
	}
	s.zitadel = zc
	s.deferCleanup(terminate(ctx, zc))
	s.deferCleanup(func() {
		if cli, err := testcontainers.NewDockerClientWithOpts(ctx); err == nil {
			_ = cli.VolumeRemove(ctx, volumeName, true)
			cli.Close()
		}
	})

	if err := s.waitZitadelReady(); err != nil {
		return err
	}
	pat, err := s.waitForPAT()
	if err != nil {
		return err
	}
	s.mgmtPAT = pat

	// default org + API project (its id is the OIDC audience)
	var org struct {
		Org struct {
			ID string `json:"id"`
		} `json:"org"`
	}
	if err := s.mgmtDo("GET", "/management/v1/orgs/me", "", nil, &org); err != nil {
		return fmt.Errorf("orgs/me: %w", err)
	}
	s.defaultOrgID = org.Org.ID

	var proj struct {
		ID string `json:"id"`
	}
	if err := s.mgmtDo("POST", "/management/v1/projects",
		"", map[string]string{"name": "leifwind-api"}, &proj); err != nil {
		return fmt.Errorf("create project: %w", err)
	}
	s.Audience = proj.ID
	return nil
}

// waitZitadelReady: native probe first (no HTTP/Host header), then discovery
// from the host (exercises instance resolution / EXTERNALDOMAIN config).
func (s *Stack) waitZitadelReady() error {
	deadline := time.Now().Add(120 * time.Second)
	for {
		code, _, err := s.zitadel.Exec(s.ctx, []string{"/app/zitadel", "ready"})
		if err == nil && code == 0 {
			break
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("zitadel ready probe timed out")
		}
		time.Sleep(2 * time.Second)
	}
	for {
		resp, err := http.Get(s.Issuer + "/.well-known/openid-configuration")
		if err == nil {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if resp.StatusCode == 200 {
				return nil
			}
			if resp.StatusCode == 404 && bytes.Contains(body, []byte("QUERY-")) {
				return fmt.Errorf("instance not found — domain misconfig: %s", body)
			}
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("zitadel discovery timed out")
		}
		time.Sleep(2 * time.Second)
	}
}

// waitForPAT reads the bootstrap PAT via the docker archive API
// (distroless image: no shell; daemon may be remote under dind).
func (s *Stack) waitForPAT() (string, error) {
	deadline := time.Now().Add(30 * time.Second)
	for {
		rc, err := s.zitadel.CopyFileFromContainer(s.ctx, patPath)
		if err == nil {
			b, rerr := io.ReadAll(rc)
			rc.Close()
			if rerr == nil && len(bytes.TrimSpace(b)) > 0 {
				return string(bytes.TrimSpace(b)), nil
			}
		}
		if time.Now().After(deadline) {
			return "", fmt.Errorf("PAT never appeared at %s", patPath)
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// mgmtDo calls a ZITADEL management/v2 API with the bootstrap PAT,
// retrying 503 for ~30s (the query side settles after 'ready').
func (s *Stack) mgmtDo(method, path, orgID string, body, out any) error {
	deadline := time.Now().Add(30 * time.Second)
	for {
		var rdr io.Reader
		if body != nil {
			b, err := json.Marshal(body)
			if err != nil {
				return err
			}
			rdr = bytes.NewReader(b)
		}
		req, err := http.NewRequest(method, s.Issuer+path, rdr)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+s.mgmtPAT)
		req.Header.Set("Content-Type", "application/json")
		if orgID != "" {
			req.Header.Set("x-zitadel-orgid", orgID)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return err
		}
		rb, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode == 503 && time.Now().Before(deadline) {
			time.Sleep(time.Second)
			continue
		}
		if resp.StatusCode >= 400 {
			return fmt.Errorf("%s %s: %d %s", method, path, resp.StatusCode, rb)
		}
		if out != nil {
			return json.Unmarshal(rb, out)
		}
		return nil
	}
}
```

Add the missing import `"github.com/docker/go-connections/nat"` to zitadel.go, then:

```bash
cd client && go mod tidy
```

- [ ] **Step 4: Run test to verify it passes**

```bash
cd client && go test ./leifwindtest/ -run TestStackBootsZitadel -v -timeout 20m
```
Expected: PASS (~60–90 s: pulls + boot). If the run fails pulling `postgres`/`zitadel`, check docker connectivity, not the code.

- [ ] **Step 5: Commit**

```bash
git add client/leifwindtest/ client/go.mod client/go.sum
git commit -m "feat(leifwindtest): ZITADEL v4.15.3 stack boot with bootstrap PAT and API project"
```

---

### Task 8: Fixture — per-test orgs and machine tokens

**Files:**
- Create: `client/leifwindtest/org.go`
- Test: `client/leifwindtest/org_test.go`

**Interfaces:**
- Consumes: `(*Stack).mgmtDo`, `Stack.Audience`, `Stack.defaultOrgID` (Task 7).
- Produces: `(*Stack).NewOrg(t testing.TB) *Org`; `Org{ID, MachineUserID, ClientID, ClientSecret string}`; `(*Org).Token(t testing.TB, s *Stack) string` (raw client_credentials fetch); unexported `fetchToken(issuer, clientID, clientSecret string, form url.Values) (string, int, error)` reused by Task 10; `decodeClaims(t, token) map[string]any` test helper (exported as `DecodeClaims` for reuse in acceptance tests).

Ports `_zitadel_create_org` (testing.py:623-671): create org via `POST /v2/organizations`, machine user via `POST /management/v1/users/machine` (header `x-zitadel-orgid: <org>`, `accessTokenType: ACCESS_TOKEN_TYPE_JWT`), secret via `PUT /management/v1/users/{id}/secret`, then grant the API project to the org via `POST /management/v1/projects/{projectID}/grants` (header `x-zitadel-orgid: <default org>`, body `{"grantedOrgId": ...}`) — without the grant the token endpoint rejects the project-audience scope.

- [ ] **Step 1: Write the failing test**

`client/leifwindtest/org_test.go`:
```go
// SPDX-License-Identifier: MPL-2.0

package leifwindtest

import (
	"strings"
	"testing"
)

func TestNewOrgMintsJWTWithOrgAndAudience(t *testing.T) {
	s := Start(t)
	org := s.NewOrg(t)
	tok := org.Token(t, s)
	if strings.Count(tok, ".") != 2 {
		t.Fatalf("expected a JWT (3 segments), got %q…", tok[:20])
	}
	claims := DecodeClaims(t, tok)
	if claims["urn:zitadel:iam:user:resourceowner:id"] != org.ID {
		t.Fatalf("resourceowner claim = %v, want %s", claims["urn:zitadel:iam:user:resourceowner:id"], org.ID)
	}
	aud, _ := claims["aud"].([]any)
	found := false
	for _, a := range aud {
		if a == s.Audience {
			found = true
		}
	}
	if !found {
		t.Fatalf("audience %s not in aud %v", s.Audience, aud)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd client && go test ./leifwindtest/ -run TestNewOrgMints -v -timeout 20m
```
Expected: FAIL — `undefined: (*Stack).NewOrg` (compile error).

- [ ] **Step 3: Write minimal implementation**

`client/leifwindtest/org.go`:
```go
// SPDX-License-Identifier: MPL-2.0

package leifwindtest

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// Org is a fresh tenant with one machine user (JWT access tokens).
type Org struct {
	ID            string
	MachineUserID string
	ClientID      string
	ClientSecret  string
}

// NewOrg creates an isolated org: org + machine user + client secret +
// API-project grant (required for the project-audience scope).
func (s *Stack) NewOrg(t testing.TB) *Org {
	t.Helper()
	name := "org-" + uuid.NewString()[:12]

	var created struct {
		OrganizationID string `json:"organizationId"`
	}
	if err := s.mgmtDo("POST", "/v2/organizations", "",
		map[string]string{"name": name}, &created); err != nil {
		t.Fatalf("create org: %v", err)
	}

	var user struct {
		UserID string `json:"userId"`
	}
	if err := s.mgmtDo("POST", "/management/v1/users/machine", created.OrganizationID,
		map[string]string{
			"userName":        "m2m-" + name,
			"name":            "m2m-" + name,
			"accessTokenType": "ACCESS_TOKEN_TYPE_JWT",
		}, &user); err != nil {
		t.Fatalf("create machine user: %v", err)
	}

	var secret struct {
		ClientID     string `json:"clientId"`
		ClientSecret string `json:"clientSecret"`
	}
	if err := s.mgmtDo("PUT", "/management/v1/users/"+user.UserID+"/secret",
		created.OrganizationID, map[string]string{}, &secret); err != nil {
		t.Fatalf("create secret: %v", err)
	}

	if err := s.mgmtDo("POST", "/management/v1/projects/"+s.Audience+"/grants",
		s.defaultOrgID, map[string]string{"grantedOrgId": created.OrganizationID}, nil); err != nil {
		t.Fatalf("grant project: %v", err)
	}

	return &Org{
		ID:            created.OrganizationID,
		MachineUserID: user.UserID,
		ClientID:      secret.ClientID,
		ClientSecret:  secret.ClientSecret,
	}
}

// Token fetches one raw machine access token (client_credentials).
func (o *Org) Token(t testing.TB, s *Stack) string {
	t.Helper()
	form := url.Values{
		"grant_type": {"client_credentials"},
		"scope": {strings.Join([]string{
			"openid",
			"urn:zitadel:iam:user:resourceowner",
			"urn:zitadel:iam:org:project:id:" + s.Audience + ":aud",
		}, " ")},
	}
	tok, status, err := fetchToken(s.Issuer, o.ClientID, o.ClientSecret, form)
	if err != nil || status != 200 {
		t.Fatalf("token fetch: status=%d err=%v", status, err)
	}
	return tok
}

func fetchToken(issuer, clientID, clientSecret string, form url.Values) (string, int, error) {
	req, err := http.NewRequest("POST", issuer+"/oauth/v2/token",
		strings.NewReader(form.Encode()))
	if err != nil {
		return "", 0, err
	}
	req.SetBasicAuth(clientID, clientSecret)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return "", resp.StatusCode, fmt.Errorf("token endpoint: %s", body)
	}
	var out struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", resp.StatusCode, err
	}
	return out.AccessToken, resp.StatusCode, nil
}

// DecodeClaims decodes a JWT payload WITHOUT verification (test helper).
func DecodeClaims(t testing.TB, token string) map[string]any {
	t.Helper()
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("not a JWT: %d segments", len(parts))
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	var claims map[string]any
	if err := json.Unmarshal(payload, &claims); err != nil {
		t.Fatalf("unmarshal claims: %v", err)
	}
	return claims
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
cd client && go test ./leifwindtest/ -run TestNewOrgMints -v -timeout 20m
```
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add client/leifwindtest/org.go client/leifwindtest/org_test.go
git commit -m "feat(leifwindtest): per-test orgs with machine users and project grants"
```

---

### Task 9: TokenSources — static + client_credentials

**Files:**
- Create: `client/auth.go`
- Test: `client/auth_test.go`

**Interfaces:**
- Consumes: fixture `Start`, `NewOrg` (Tasks 7–8).
- Produces: `TokenSource interface { Token(ctx context.Context) (string, error) }`; `StaticToken(string) TokenSource`; `ClientCredentials(issuer, clientID, clientSecret string, opts ...CredentialOption) TokenSource`; `WithAudience(string) CredentialOption`; `WithCredentialClock(func() time.Time) CredentialOption`; `WithCredentialHTTPClient(*http.Client) CredentialOption`.

Mirrors `client.py` `ClientCredentialsTokenProvider`: token endpoint `{issuer}/oauth/v2/token`, HTTP basic auth, scopes `openid` + `urn:zitadel:iam:user:resourceowner` (+ audience scope), cache, refresh 60 s before `expires_in` (default 3600), monotonic-ish via injected clock.

- [ ] **Step 1: Write the failing test**

`client/auth_test.go` (blackbox against the real fixture ZITADEL; a counting `http.RoundTripper` instruments OUR client — that is in-process instrumentation, not backend mocking):
```go
// SPDX-License-Identifier: MPL-2.0

package client_test

import (
	"context"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"gitlab.com/leifwind/stream/terraform-provider-leifwind/client"
	"gitlab.com/leifwind/stream/terraform-provider-leifwind/client/leifwindtest"
)

type countingTransport struct {
	calls atomic.Int32
	next  http.RoundTripper
}

func (c *countingTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	c.calls.Add(1)
	return c.next.RoundTrip(r)
}

func TestStaticToken(t *testing.T) {
	ts := client.StaticToken("abc")
	tok, err := ts.Token(context.Background())
	if err != nil || tok != "abc" {
		t.Fatalf("got %q, %v", tok, err)
	}
}

func TestClientCredentialsFetchesCachesAndRefreshes(t *testing.T) {
	s := leifwindtest.Start(t)
	org := s.NewOrg(t)

	now := time.Now()
	clock := func() time.Time { return now }
	ct := &countingTransport{next: http.DefaultTransport}
	ts := client.ClientCredentials(s.Issuer, org.ClientID, org.ClientSecret,
		client.WithAudience(s.Audience),
		client.WithCredentialClock(clock),
		client.WithCredentialHTTPClient(&http.Client{Transport: ct}))

	ctx := context.Background()
	tok1, err := ts.Token(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if tok1 == "" || ct.calls.Load() != 1 {
		t.Fatalf("first fetch: calls=%d", ct.calls.Load())
	}
	// cached: no second HTTP call
	tok2, _ := ts.Token(ctx)
	if tok2 != tok1 || ct.calls.Load() != 1 {
		t.Fatalf("expected cache hit, calls=%d", ct.calls.Load())
	}
	// advance past expires_in - 60s ⇒ refresh
	now = now.Add(2 * time.Hour)
	if _, err := ts.Token(ctx); err != nil {
		t.Fatal(err)
	}
	if ct.calls.Load() != 2 {
		t.Fatalf("expected refresh, calls=%d", ct.calls.Load())
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd client && go test ./... -run 'TestStaticToken|TestClientCredentials' -v -timeout 20m
```
Expected: FAIL — `undefined: client.StaticToken`.

- [ ] **Step 3: Write minimal implementation**

`client/auth.go`:
```go
// SPDX-License-Identifier: MPL-2.0

package client

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// TokenSource supplies the bearer token for every request. Implementations
// must be safe for concurrent use.
type TokenSource interface {
	Token(ctx context.Context) (string, error)
}

type staticToken string

func (s staticToken) Token(context.Context) (string, error) { return string(s), nil }

// StaticToken returns a TokenSource that always yields token (delegated /
// runner path: the caller owns acquisition and refresh).
func StaticToken(token string) TokenSource { return staticToken(token) }

const refreshMargin = 60 * time.Second

type ccSettings struct {
	audience string
	now      func() time.Time
	hc       *http.Client
}

// CredentialOption configures ClientCredentials.
type CredentialOption func(*ccSettings)

// WithAudience requests the ZITADEL project-audience scope.
func WithAudience(audience string) CredentialOption {
	return func(s *ccSettings) { s.audience = audience }
}

// WithCredentialClock injects the clock (tests).
func WithCredentialClock(now func() time.Time) CredentialOption {
	return func(s *ccSettings) { s.now = now }
}

// WithCredentialHTTPClient injects the HTTP client used for token fetches.
func WithCredentialHTTPClient(hc *http.Client) CredentialOption {
	return func(s *ccSettings) { s.hc = hc }
}

type ccTokenSource struct {
	issuer, clientID, clientSecret string
	scopes                         []string
	now                            func() time.Time
	hc                             *http.Client

	mu        sync.Mutex
	token     string
	expiresAt time.Time
}

// ClientCredentials returns an auto-refreshing M2M TokenSource against
// ZITADEL's token endpoint (mirrors the Python ClientCredentialsTokenProvider:
// basic auth, resourceowner + project-audience scopes, refresh 60s early).
func ClientCredentials(issuer, clientID, clientSecret string, opts ...CredentialOption) TokenSource {
	s := ccSettings{now: time.Now, hc: http.DefaultClient}
	for _, o := range opts {
		o(&s)
	}
	scopes := []string{"openid", "urn:zitadel:iam:user:resourceowner"}
	if s.audience != "" {
		scopes = append(scopes, "urn:zitadel:iam:org:project:id:"+s.audience+":aud")
	}
	return &ccTokenSource{
		issuer: strings.TrimRight(issuer, "/"), clientID: clientID, clientSecret: clientSecret,
		scopes: scopes, now: s.now, hc: s.hc,
	}
}

func (c *ccTokenSource) Token(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token != "" && c.now().Before(c.expiresAt.Add(-refreshMargin)) {
		return c.token, nil
	}
	form := url.Values{
		"grant_type": {"client_credentials"},
		"scope":      {strings.Join(c.scopes, " ")},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.issuer+"/oauth/v2/token", strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.SetBasicAuth(c.clientID, c.clientSecret)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.hc.Do(req)
	if err != nil {
		return "", fmt.Errorf("token endpoint: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", &APIError{StatusCode: resp.StatusCode, Detail: string(body),
			Method: http.MethodPost, Path: "/oauth/v2/token"}
	}
	var out struct {
		AccessToken string  `json:"access_token"`
		ExpiresIn   float64 `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", err
	}
	if out.ExpiresIn == 0 {
		out.ExpiresIn = 3600
	}
	c.token = out.AccessToken
	c.expiresAt = c.now().Add(time.Duration(out.ExpiresIn) * time.Second)
	return c.token, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
cd client && go test ./... -run 'TestStaticToken|TestClientCredentials' -v -timeout 20m
```
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add client/auth.go client/auth_test.go
git commit -m "feat(client): TokenSource with static and auto-refreshing client_credentials"
```

---

### Task 10: Fixture — delegated user tokens (RFC 8693) + forged tokens

**Files:**
- Create: `client/leifwindtest/usertoken.go`, `client/leifwindtest/forged.go`
- Test: `client/leifwindtest/usertoken_test.go`

**Interfaces:**
- Consumes: `mgmtDo`, `fetchToken`, `Org` (Tasks 7–8).
- Produces: `(*Stack).UserToken(t testing.TB, org *Org) string` (delegated user-shaped token: human `sub`, `email` claim); `(*Stack).ForgedToken(t testing.TB, org *Org) string` (valid-looking JWT signed by an unknown key — backend must 401).

UserToken sequence (ZITADEL v4.15.3 — token exchange is pre-GA behind a feature flag):
1. `PUT /v2/features/instance` body `{"oidcTokenExchange": true}` (idempotent; do once via sync.Once).
2. `PUT /admin/v1/policies/security` body `{"enableImpersonation": true}`.
3. `POST /v2/users/human` (header `x-zitadel-orgid: org.ID`) — username, profile, verified email, password.
4. Grant impersonator role to the org's machine user: `POST /management/v1/orgs/me/members` (header `x-zitadel-orgid: org.ID`) body `{"userId": org.MachineUserID, "roles": ["ORG_END_USER_IMPERSONATOR"]}`.
5. Actor token: client_credentials for the machine user (scope `openid`).
6. Exchange at `/oauth/v2/token`: `grant_type=urn:ietf:params:oauth:grant-type:token-exchange`, `subject_token=<humanUserID>`, `subject_token_type=urn:zitadel:params:oauth:token-type:user_id`, `actor_token=<machine token>`, `actor_token_type=urn:ietf:params:oauth:token-type:access_token`, `requested_token_type=urn:ietf:params:oauth:token-type:jwt`, scopes as in Org.Token — basic-auth'd with org.ClientID/ClientSecret.

**Known risk (spec "Risks"):** if v4.15.3 rejects the exchange despite the flag, DO NOT silently skip — fail, investigate, and only then apply the spec fallback (machine token labeled delegated-equivalent + documented `t.Skip`) as a conscious decision with the owner.

- [ ] **Step 1: Write the failing test**

`client/leifwindtest/usertoken_test.go`:
```go
// SPDX-License-Identifier: MPL-2.0

package leifwindtest

import "testing"

func TestUserTokenIsDelegatedUserShaped(t *testing.T) {
	s := Start(t)
	org := s.NewOrg(t)
	tok := s.UserToken(t, org)
	claims := DecodeClaims(t, tok)
	if claims["email"] == nil {
		t.Fatalf("delegated token must carry email claim, got claims: %v", claims)
	}
	if claims["urn:zitadel:iam:user:resourceowner:id"] != org.ID {
		t.Fatalf("wrong org claim: %v", claims["urn:zitadel:iam:user:resourceowner:id"])
	}
	if sub, _ := claims["sub"].(string); sub == org.MachineUserID {
		t.Fatal("sub must be the human user, not the machine actor")
	}
	if claims["act"] == nil {
		t.Log("note: no act claim present — acceptable, but check ZITADEL version behavior")
	}
}

func TestForgedTokenHasValidShape(t *testing.T) {
	s := Start(t)
	org := s.NewOrg(t)
	tok := s.ForgedToken(t, org)
	claims := DecodeClaims(t, tok)
	if claims["iss"] != s.Issuer {
		t.Fatalf("forged token should carry the real issuer, got %v", claims["iss"])
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd client && go get github.com/golang-jwt/jwt/v5@latest && go test ./leifwindtest/ -run 'TestUserToken|TestForgedToken' -v -timeout 20m
```
Expected: FAIL — `undefined: (*Stack).UserToken`.

- [ ] **Step 3: Write minimal implementation**

`client/leifwindtest/usertoken.go`:
```go
// SPDX-License-Identifier: MPL-2.0

package leifwindtest

import (
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
)

var exchangeSetup sync.Once

// UserToken mints a genuine delegated user token via RFC 8693 token
// exchange (user_id subject type): sub = a human user, email claim present.
// This is the token shape LW-44's runner forwards.
func (s *Stack) UserToken(t testing.TB, org *Org) string {
	t.Helper()

	exchangeSetup.Do(func() {
		// pre-GA on v4.15.3: enable the instance feature flag + impersonation
		if err := s.mgmtDo("PUT", "/v2/features/instance", "",
			map[string]any{"oidcTokenExchange": true}, nil); err != nil {
			t.Fatalf("enable oidc_token_exchange: %v", err)
		}
		if err := s.mgmtDo("PUT", "/admin/v1/policies/security", "",
			map[string]any{"enableImpersonation": true}, nil); err != nil {
			t.Fatalf("enable impersonation: %v", err)
		}
	})

	suffix := uuid.NewString()[:8]
	var human struct {
		UserID string `json:"userId"`
	}
	if err := s.mgmtDo("POST", "/v2/users/human", org.ID, map[string]any{
		"username": "alice-" + suffix,
		"profile":  map[string]string{"givenName": "Alice", "familyName": "Test"},
		"email":    map[string]any{"email": "alice-" + suffix + "@example.com", "isVerified": true},
		"password": map[string]any{"password": "Password1!", "changeRequired": false},
	}, &human); err != nil {
		t.Fatalf("create human user: %v", err)
	}

	if err := s.mgmtDo("POST", "/management/v1/orgs/me/members", org.ID,
		map[string]any{"userId": org.MachineUserID,
			"roles": []string{"ORG_END_USER_IMPERSONATOR"}}, nil); err != nil {
		t.Fatalf("grant impersonator role: %v", err)
	}

	actor, status, err := fetchToken(s.Issuer, org.ClientID, org.ClientSecret,
		url.Values{"grant_type": {"client_credentials"}, "scope": {"openid"}})
	if err != nil || status != 200 {
		t.Fatalf("actor token: status=%d err=%v", status, err)
	}

	form := url.Values{
		"grant_type":           {"urn:ietf:params:oauth:grant-type:token-exchange"},
		"subject_token":        {human.UserID},
		"subject_token_type":   {"urn:zitadel:params:oauth:token-type:user_id"},
		"actor_token":          {actor},
		"actor_token_type":     {"urn:ietf:params:oauth:token-type:access_token"},
		"requested_token_type": {"urn:ietf:params:oauth:token-type:jwt"},
		"scope": {strings.Join([]string{
			"openid", "email",
			"urn:zitadel:iam:user:resourceowner",
			"urn:zitadel:iam:org:project:id:" + s.Audience + ":aud",
		}, " ")},
	}
	tok, status, err := fetchToken(s.Issuer, org.ClientID, org.ClientSecret, form)
	if err != nil || status != 200 {
		t.Fatalf("token exchange failed (status=%d): %v — see spec 'Risks': pre-GA flag on v4.15.3; investigate before falling back", status, err)
	}
	return tok
}
```

`client/leifwindtest/forged.go`:
```go
// SPDX-License-Identifier: MPL-2.0

package leifwindtest

import (
	"crypto/rand"
	"crypto/rsa"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// ForgedToken returns an RS256 JWT with fully correct claims signed by a
// key that is NOT in ZITADEL's JWKS. The backend must reject it (401):
// mirrors the backend's own test_locally_forged_token_rejected.
func (s *Stack) ForgedToken(t testing.TB, org *Org) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	claims := jwt.MapClaims{
		"iss": s.Issuer,
		"aud": []string{s.Audience},
		"sub": "forged-user",
		"iat": now.Unix(),
		"exp": now.Add(5 * time.Minute).Unix(),
		"urn:zitadel:iam:user:resourceowner:id": org.ID,
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	tok.Header["kid"] = "forged-kid"
	signed, err := tok.SignedString(key)
	if err != nil {
		t.Fatal(err)
	}
	return signed
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
cd client && go test ./leifwindtest/ -run 'TestUserToken|TestForgedToken' -v -timeout 20m
```
Expected: PASS. If the exchange returns an OAuth error, STOP and investigate (flag name/endpoint changed between ZITADEL minor versions) before considering the spec fallback.

- [ ] **Step 5: Commit**

```bash
git add client/leifwindtest/usertoken.go client/leifwindtest/forged.go client/leifwindtest/usertoken_test.go client/go.mod client/go.sum
git commit -m "feat(leifwindtest): delegated user tokens via RFC 8693 exchange + forged tokens"
```

---

### Task 11: Fixture part 2 — backend + database containers

**Files:**
- Modify: `client/leifwindtest/zitadel.go` (remove the `startBackend` stub)
- Create: `client/leifwindtest/backend.go`
- Test: extend `client/leifwindtest/stack_test.go`

**Interfaces:**
- Consumes: Tasks 7–8.
- Produces: `Stack.BackendURL` (reachable, migrated, auth-enforced backend). Toxiproxy wiring arrives in Task 17 — `startBackend(withToxiproxy bool)` ignores the flag for now.

Backend env contract (backend `app.py` Settings + docker-compose): `POSTGRES_URL=postgresql://leifwind:leifwind@backend-db:5432/leifwind`, `SERIALIZER_SECRET_KEY`/`SERIALIZER_SALT` (any test values), `OIDC_ISSUER` = `Stack.Issuer` (must equal token `iss`), `OIDC_AUDIENCE` = `Stack.Audience`, and `OIDC_INTERNAL_BASE_URL=http://zitadel:8080` — the backend container cannot reach the host-published issuer URL, so the split-horizon override points it at the network alias while `iss` validation still matches. Port 8000, `/healthz` open, migrations run on startup (`AUTOMATIC_UPGRADE` defaults true).

- [ ] **Step 1: Write the failing test**

Append to `client/leifwindtest/stack_test.go`:
```go
func TestBackendEnforcesAuth(t *testing.T) {
	s := Start(t)
	org := s.NewOrg(t)

	resp, err := http.Get(s.BackendURL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("healthz: %d", resp.StatusCode)
	}

	resp, err = http.Get(s.BackendURL + "/metadata/projects")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Fatalf("unauthenticated list: want 401, got %d", resp.StatusCode)
	}

	req, _ := http.NewRequest("GET", s.BackendURL+"/metadata/projects", nil)
	req.Header.Set("Authorization", "Bearer "+org.Token(t, s))
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("authenticated list: want 200, got %d", resp.StatusCode)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd client && go test ./leifwindtest/ -run TestBackendEnforcesAuth -v -timeout 20m
```
Expected: FAIL — `Get "": unsupported protocol scheme ""` (BackendURL empty: stub).

- [ ] **Step 3: Write minimal implementation**

Delete the stub line `func (s *Stack) startBackend(withToxiproxy bool) error { return nil }` from `zitadel.go`, create `client/leifwindtest/backend.go`:
```go
// SPDX-License-Identifier: MPL-2.0

package leifwindtest

import (
	"fmt"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

func (s *Stack) startBackend(withToxiproxy bool) error {
	ctx := s.ctx

	bdb, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:          postgresImage,
			Networks:       []string{s.net.Name},
			NetworkAliases: map[string][]string{s.net.Name: {"backend-db"}},
			Env: map[string]string{
				"POSTGRES_USER":     "leifwind",
				"POSTGRES_PASSWORD": "leifwind",
				"POSTGRES_DB":       "leifwind",
			},
			WaitingFor: wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).WithStartupTimeout(60 * time.Second),
		},
		Started: true,
	})
	if err != nil {
		return fmt.Errorf("backend-db: %w", err)
	}
	s.deferCleanup(terminate(ctx, bdb))

	backend, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:          BackendImage,
			Networks:       []string{s.net.Name},
			NetworkAliases: map[string][]string{s.net.Name: {"backend"}},
			ExposedPorts:   []string{"8000/tcp"},
			Env: map[string]string{
				"POSTGRES_URL":           "postgresql://leifwind:leifwind@backend-db:5432/leifwind",
				"SERIALIZER_SECRET_KEY":  "test-secret",
				"SERIALIZER_SALT":        "test-salt",
				"OIDC_ISSUER":            s.Issuer,
				"OIDC_AUDIENCE":          s.Audience,
				"OIDC_INTERNAL_BASE_URL": "http://" + zitadelAlias + ":8080",
			},
			// healthz is open; migrations run on startup, allow time
			WaitingFor: wait.ForHTTP("/healthz").WithPort("8000/tcp").
				WithStartupTimeout(120 * time.Second),
		},
		Started: true,
	})
	if err != nil {
		return fmt.Errorf("backend (image %s — check registry login / LW-68 allowlist): %w", BackendImage, err)
	}
	s.deferCleanup(terminate(ctx, backend))

	host, err := backend.Host(ctx)
	if err != nil {
		return err
	}
	port, err := backend.MappedPort(ctx, "8000/tcp")
	if err != nil {
		return err
	}
	s.BackendURL = fmt.Sprintf("http://%s:%s", host, port.Port())

	_ = withToxiproxy // Task 17
	return nil
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
cd client && go test ./leifwindtest/ -run TestBackendEnforcesAuth -v -timeout 20m
```
Expected: PASS. Requires `docker login registry.gitlab.com` locally (read_registry PAT). If the pull fails with 403 in CI, the backend project's job-token allowlist entry is missing (LW-68).

- [ ] **Step 5: Commit**

```bash
git add client/leifwindtest/backend.go client/leifwindtest/zitadel.go client/leifwindtest/stack_test.go
git commit -m "feat(leifwindtest): backend + postgres containers with split-horizon OIDC wiring"
```

---

### Task 12: client.New + request funnel + project upsert/get

**Files:**
- Create: `client/client.go`, `client/metadata.go`, `client/generic.go` (service struct only)
- Test: `client/client_test.go`

**Interfaces:**
- Consumes: models (T4), errors (T5), opts (T6), TokenSource (T9), fixture (T7–11).
- Produces: `New(endpoint string, opts ...Option) (*Client, error)`; `Client{Metadata *MetadataService; Generic *GenericService}`; Options `WithTokenSource`, `WithHTTPClient`, `WithUserAgent`, `WithRetry(RetryConfig)`; `RetryConfig{MaxAttempts int; MinBackoff, MaxBackoff time.Duration}`; unexported `(c *Client) do(ctx, method, path string, query url.Values, body, out any) error` (single attempt — the retry loop wraps it in Task 17); `(s *MetadataService) UpsertProject`, `GetProject`. Also the shared client-test `TestMain` that all later client blackbox tests reuse.

- [ ] **Step 1: Write the failing test**

`client/client_test.go`:
```go
// SPDX-License-Identifier: MPL-2.0

package client_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"

	"github.com/google/uuid"
	"gitlab.com/leifwind/stream/terraform-provider-leifwind/client"
	"gitlab.com/leifwind/stream/terraform-provider-leifwind/client/leifwindtest"
)

// shared stack for ALL client blackbox tests (this file and later tasks')
var (
	sharedStack *leifwindtest.Stack
	stackErr    error
)

func TestMain(m *testing.M) {
	var cleanup func()
	sharedStack, cleanup, stackErr = leifwindtest.StartMain(leifwindtest.WithToxiproxy())
	code := m.Run()
	if cleanup != nil {
		cleanup()
	}
	os.Exit(code)
}

var orgMu sync.Mutex

// newTestClient returns a client bound to a FRESH org (tenant isolation).
func newTestClient(t *testing.T) (*client.Client, *leifwindtest.Org) {
	t.Helper()
	if stackErr != nil {
		t.Fatalf("stack: %v", stackErr)
	}
	orgMu.Lock()
	org := sharedStack.NewOrg(t)
	orgMu.Unlock()
	c, err := client.New(sharedStack.BackendURL,
		client.WithTokenSource(org.TokenSource(sharedStack)))
	if err != nil {
		t.Fatal(err)
	}
	return c, org
}

func TestNewRequiresEndpoint(t *testing.T) {
	if _, err := client.New(""); err == nil {
		t.Fatal("empty endpoint must error")
	}
}

func TestUpsertProjectCreatesAndAdopts(t *testing.T) {
	t.Parallel()
	c, _ := newTestClient(t)
	ctx := context.Background()

	created, err := c.Metadata.UpsertProject(ctx, client.MetadataProject{Name: "proj_a"})
	if err != nil {
		t.Fatal(err)
	}
	if created.ObjectID == nil || created.UniqueKey != "proj_a" {
		t.Fatalf("bad create result: %+v", created)
	}

	// same natural key, no object_id ⇒ adopt, same object_id back
	adopted, err := c.Metadata.UpsertProject(ctx, client.MetadataProject{Name: "proj_a"})
	if err != nil {
		t.Fatal(err)
	}
	if *adopted.ObjectID != *created.ObjectID {
		t.Fatalf("adopt returned different id: %s vs %s", adopted.ObjectID, created.ObjectID)
	}
}

func TestUpsertProjectImmutableRename(t *testing.T) {
	t.Parallel()
	c, _ := newTestClient(t)
	ctx := context.Background()
	p, err := c.Metadata.UpsertProject(ctx, client.MetadataProject{Name: "proj_b"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.Metadata.UpsertProject(ctx, client.MetadataProject{ObjectID: p.ObjectID, Name: "proj_b_renamed"})
	if !errors.Is(err, client.ErrValidation) {
		t.Fatalf("want ErrValidation, got %v", err)
	}
}

func TestGetProjectNotFound(t *testing.T) {
	t.Parallel()
	c, _ := newTestClient(t)
	_, err := c.Metadata.GetProject(context.Background(), uuid.New())
	if !errors.Is(err, client.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	var apiErr *client.APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != 404 {
		t.Fatalf("want APIError 404, got %v", err)
	}
}

var _ = fmt.Sprintf // keep fmt for later tasks extending this file
```

NOTE: `WithToxiproxy()` and `Org.TokenSource` don't exist yet — add the minimal `TokenSource` method now (it belongs to this task's deliverable) and a no-op `WithToxiproxy` StackOption stub completed in Task 17. Append to `client/leifwindtest/org.go`:
```go
// TokenSource returns an auto-refreshing client_credentials TokenSource
// for this org against the stack's ZITADEL.
func (o *Org) TokenSource(s *Stack) client.TokenSource {
	return client.ClientCredentials(s.Issuer, o.ClientID, o.ClientSecret,
		client.WithAudience(s.Audience))
}
```
and to `client/leifwindtest/stack.go`:
```go
// WithToxiproxy routes ProxiedBackendURL through a toxiproxy container
// (fault injection; fully wired in the retry task).
func WithToxiproxy() StackOption {
	return func(s *stackSettings) { s.toxiproxy = true }
}
```
(`leifwindtest` may import `client` — same module, no cycle: `client` package must NOT import `leifwindtest`.)

- [ ] **Step 2: Run test to verify it fails**

```bash
cd client && go test ./... -run 'TestNewRequires|TestUpsertProject|TestGetProject' -v -timeout 20m
```
Expected: FAIL — `undefined: client.New`.

- [ ] **Step 3: Write minimal implementation**

`client/client.go`:
```go
// SPDX-License-Identifier: MPL-2.0

package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Version is stamped into the default User-Agent.
const Version = "0.1.0-dev"

// RetryConfig bounds the retry loop (Task: retries). MaxAttempts 1 disables.
type RetryConfig struct {
	MaxAttempts int
	MinBackoff  time.Duration
	MaxBackoff  time.Duration
}

type settings struct {
	ts    TokenSource
	hc    *http.Client
	ua    string
	retry RetryConfig
}

// Option configures New.
type Option func(*settings)

// WithTokenSource sets the bearer-token source (required for authenticated APIs).
func WithTokenSource(ts TokenSource) Option { return func(s *settings) { s.ts = ts } }

// WithHTTPClient replaces the underlying *http.Client.
func WithHTTPClient(hc *http.Client) Option { return func(s *settings) { s.hc = hc } }

// WithUserAgent prepends a product token to the default User-Agent.
func WithUserAgent(ua string) Option { return func(s *settings) { s.ua = ua } }

// WithRetry overrides the default retry policy ({3, 250ms, 4s}).
func WithRetry(rc RetryConfig) Option { return func(s *settings) { s.retry = rc } }

// Client is a leifwind metadata API client. Safe for concurrent use.
type Client struct {
	Metadata *MetadataService
	Generic  *GenericService

	baseURL string
	hc      *http.Client
	ts      TokenSource
	ua      string
	retry   RetryConfig
}

// New creates a Client for the backend at endpoint.
func New(endpoint string, opts ...Option) (*Client, error) {
	if endpoint == "" {
		return nil, fmt.Errorf("client: endpoint is required")
	}
	s := settings{
		hc:    &http.Client{Timeout: 60 * time.Second},
		retry: RetryConfig{MaxAttempts: 3, MinBackoff: 250 * time.Millisecond, MaxBackoff: 4 * time.Second},
	}
	for _, o := range opts {
		o(&s)
	}
	ua := "terraform-provider-leifwind-client/" + Version
	if s.ua != "" {
		ua = s.ua + " " + ua
	}
	c := &Client{
		baseURL: strings.TrimRight(endpoint, "/"),
		hc:      s.hc, ts: s.ts, ua: ua, retry: s.retry,
	}
	c.Metadata = &MetadataService{c: c}
	c.Generic = &GenericService{c: c}
	return c, nil
}

// do performs one request (retry wrapping added in the retry task).
func (c *Client) do(ctx context.Context, method, path string, query url.Values, body, out any) error {
	return c.doOnce(ctx, method, path, query, body, out)
}

func (c *Client) doOnce(ctx context.Context, method, path string, query url.Values, body, out any) error {
	u := c.baseURL + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("%s %s: encode: %w", method, path, err)
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rdr)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", c.ua)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.ts != nil {
		tok, err := c.ts.Token(ctx)
		if err != nil {
			return fmt.Errorf("%s %s: token: %w", method, path, err)
		}
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	rb, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("%s %s: read body: %w", method, path, err)
	}
	if resp.StatusCode >= 400 {
		return newAPIError(method, path, resp.StatusCode, rb)
	}
	if out != nil {
		if err := json.Unmarshal(rb, out); err != nil {
			return fmt.Errorf("%s %s: decode: %w", method, path, err)
		}
	}
	return nil
}
```

`client/metadata.go`:
```go
// SPDX-License-Identifier: MPL-2.0

package client

import (
	"context"

	"github.com/google/uuid"
)

// MetadataService covers the /metadata control-plane endpoints.
type MetadataService struct {
	c *Client
}

// UpsertProject creates or adopts a project. Omit ObjectID to create-or-adopt
// by name; the response carries the canonical ObjectID and UniqueKey.
// Changing Name of an existing ObjectID returns ErrValidation (immutable).
func (s *MetadataService) UpsertProject(ctx context.Context, p MetadataProject, opts ...WriteOption) (MetadataProject, error) {
	var out MetadataProject
	err := s.c.do(ctx, "POST", "/metadata/projects", writeValues(opts), p, &out)
	return out, err
}

// GetProject fetches one project; ErrNotFound covers missing AND cross-tenant.
func (s *MetadataService) GetProject(ctx context.Context, projectID uuid.UUID) (MetadataProject, error) {
	var out MetadataProject
	err := s.c.do(ctx, "GET", "/metadata/projects/"+projectID.String(), nil, nil, &out)
	return out, err
}
```

`client/generic.go`:
```go
// SPDX-License-Identifier: MPL-2.0

package client

// GenericService covers the /generic data-plane read endpoints the
// provider needs (fragment schema names).
type GenericService struct {
	c *Client
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
cd client && go test ./... -run 'TestNewRequires|TestUpsertProject|TestGetProject' -v -timeout 20m
```
Expected: PASS (4 tests, one shared stack boot).

- [ ] **Step 5: Commit**

```bash
git add client/client.go client/metadata.go client/generic.go client/client_test.go client/leifwindtest/org.go client/leifwindtest/stack.go
git commit -m "feat(client): request funnel with project upsert/get, blackbox against real stack"
```

---

### Task 13: Project delete, dry-run, list, iter

**Files:**
- Modify: `client/metadata.go`
- Test: `client/metadata_projects_test.go`

**Interfaces:**
- Consumes: Task 12.
- Produces: `DeleteProject(ctx, uuid.UUID, ...WriteOption) error`; `ListProjects(ctx, ListOpts) (Page[MetadataProject], error)`; `IterProjects(ctx, ListOpts) iter.Seq2[MetadataProject, error]`; unexported generic helpers `listPage[T]`/`iterPages[T]` reused by entity/field tasks.

- [ ] **Step 1: Write the failing test**

`client/metadata_projects_test.go`:
```go
// SPDX-License-Identifier: MPL-2.0

package client_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"gitlab.com/leifwind/stream/terraform-provider-leifwind/client"
)

func TestDeleteProjectAndDryRun(t *testing.T) {
	t.Parallel()
	c, _ := newTestClient(t)
	ctx := context.Background()
	p, err := c.Metadata.UpsertProject(ctx, client.MetadataProject{Name: "del_me"})
	if err != nil {
		t.Fatal(err)
	}

	// dry run: validated but rolled back
	if err := c.Metadata.DeleteProject(ctx, *p.ObjectID, client.DryRun()); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Metadata.GetProject(ctx, *p.ObjectID); err != nil {
		t.Fatalf("dry-run delete must not delete: %v", err)
	}

	if err := c.Metadata.DeleteProject(ctx, *p.ObjectID); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Metadata.GetProject(ctx, *p.ObjectID); !errors.Is(err, client.ErrNotFound) {
		t.Fatalf("want ErrNotFound after delete, got %v", err)
	}
}

func TestListAndIterProjectsPaginate(t *testing.T) {
	t.Parallel()
	c, _ := newTestClient(t) // fresh org ⇒ only our projects
	ctx := context.Background()
	const total = 55 // > MAX_PAGE_SIZE to force >1 page even at limit 50
	for i := 0; i < total; i++ {
		if _, err := c.Metadata.UpsertProject(ctx,
			client.MetadataProject{Name: fmt.Sprintf("page_%02d", i)}); err != nil {
			t.Fatal(err)
		}
	}

	page, err := c.Metadata.ListProjects(ctx, client.ListOpts{Limit: 25})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Objects) != 25 || page.Cursor == nil {
		t.Fatalf("first page: %d objects, cursor=%v", len(page.Objects), page.Cursor)
	}

	// walk pages with cursor only (pattern/limit ride inside the cursor)
	seen := len(page.Objects)
	for page.Cursor != nil {
		page, err = c.Metadata.ListProjects(ctx, client.ListOpts{Cursor: *page.Cursor})
		if err != nil {
			t.Fatal(err)
		}
		seen += len(page.Objects)
	}
	if seen != total {
		t.Fatalf("page walk saw %d, want %d", seen, total)
	}

	count := 0
	for _, err := range c.Metadata.IterProjects(ctx, client.ListOpts{Limit: 20}) {
		if err != nil {
			t.Fatal(err)
		}
		count++
	}
	if count != total {
		t.Fatalf("iter saw %d, want %d", count, total)
	}
}

func TestListProjectsPattern(t *testing.T) {
	t.Parallel()
	c, _ := newTestClient(t)
	ctx := context.Background()
	for _, n := range []string{"alpha_one", "alpha_two", "beta_one"} {
		if _, err := c.Metadata.UpsertProject(ctx, client.MetadataProject{Name: n}); err != nil {
			t.Fatal(err)
		}
	}
	page, err := c.Metadata.ListProjects(ctx, client.ListOpts{Pattern: "alpha"})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Objects) != 2 {
		t.Fatalf("pattern alpha: got %d", len(page.Objects))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd client && go test ./... -run 'TestDeleteProject|TestListAndIter|TestListProjectsPattern' -v -timeout 20m
```
Expected: FAIL — `c.Metadata.DeleteProject undefined`.

- [ ] **Step 3: Write minimal implementation**

Append to `client/metadata.go`:
```go
// DeleteProject deletes a project; entities/fields cascade server-side and
// the per-project schema is dropped.
func (s *MetadataService) DeleteProject(ctx context.Context, projectID uuid.UUID, opts ...WriteOption) error {
	var out struct {
		Detail string `json:"detail"`
	}
	return s.c.do(ctx, "DELETE", "/metadata/projects/"+projectID.String(), writeValues(opts), nil, &out)
}

// ListProjects returns one page.
func (s *MetadataService) ListProjects(ctx context.Context, opts ListOpts) (Page[MetadataProject], error) {
	return listPage[MetadataProject](ctx, s.c, "/metadata/projects", opts)
}

// IterProjects auto-pages through all projects.
func (s *MetadataService) IterProjects(ctx context.Context, opts ListOpts) iter.Seq2[MetadataProject, error] {
	return iterPages(ctx, opts, func(ctx context.Context, o ListOpts) (Page[MetadataProject], error) {
		return s.ListProjects(ctx, o)
	})
}

func listPage[T any](ctx context.Context, c *Client, path string, opts ListOpts) (Page[T], error) {
	var out Page[T]
	err := c.do(ctx, "GET", path, opts.values(), nil, &out)
	return out, err
}

// iterPages mirrors the Python iter_* helpers: after the first page only
// the cursor is forwarded (pattern+limit are baked into it server-side).
func iterPages[T any](ctx context.Context, opts ListOpts, list func(context.Context, ListOpts) (Page[T], error)) iter.Seq2[T, error] {
	return func(yield func(T, error) bool) {
		for {
			page, err := list(ctx, opts)
			if err != nil {
				var zero T
				yield(zero, err)
				return
			}
			for _, obj := range page.Objects {
				if !yield(obj, nil) {
					return
				}
			}
			if page.Cursor == nil {
				return
			}
			opts = ListOpts{Cursor: *page.Cursor}
		}
	}
}
```
Add `"iter"` to the imports of `client/metadata.go`.

- [ ] **Step 4: Run test to verify it passes**

```bash
cd client && go test ./... -run 'TestDeleteProject|TestListAndIter|TestListProjectsPattern' -v -timeout 20m
```
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add client/metadata.go client/metadata_projects_test.go
git commit -m "feat(client): project delete/dry-run and cursor pagination (list + iter)"
```

---

### Task 14: Entity methods

**Files:**
- Modify: `client/metadata.go`
- Test: `client/metadata_entities_test.go`

**Interfaces:**
- Consumes: Tasks 12–13 (`do`, `listPage`, `iterPages`).
- Produces: `UpsertEntity(ctx, MetadataEntity, ...WriteOption) (MetadataEntity, error)` (URL project from `e.ProjectID`); `GetEntity(ctx, projectID, entityID uuid.UUID) (MetadataEntity, error)`; `DeleteEntity`; `ListEntities(ctx, projectID uuid.UUID, ListOpts)`; `IterEntities`.

- [ ] **Step 1: Write the failing test**

`client/metadata_entities_test.go`:
```go
// SPDX-License-Identifier: MPL-2.0

package client_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"gitlab.com/leifwind/stream/terraform-provider-leifwind/client"
)

func TestEntityLifecycle(t *testing.T) {
	t.Parallel()
	c, _ := newTestClient(t)
	ctx := context.Background()
	p, err := c.Metadata.UpsertProject(ctx, client.MetadataProject{Name: "ent_proj"})
	if err != nil {
		t.Fatal(err)
	}

	e, err := c.Metadata.UpsertEntity(ctx, client.MetadataEntity{ProjectID: *p.ObjectID, Name: "book"})
	if err != nil {
		t.Fatal(err)
	}
	if e.ObjectID == nil {
		t.Fatal("no object_id")
	}

	got, err := c.Metadata.GetEntity(ctx, *p.ObjectID, *e.ObjectID)
	if err != nil || got.Name != "book" {
		t.Fatalf("get: %+v, %v", got, err)
	}

	page, err := c.Metadata.ListEntities(ctx, *p.ObjectID, client.ListOpts{Pattern: "boo"})
	if err != nil || len(page.Objects) != 1 {
		t.Fatalf("list: %d, %v", len(page.Objects), err)
	}

	n := 0
	for _, err := range c.Metadata.IterEntities(ctx, *p.ObjectID, client.ListOpts{}) {
		if err != nil {
			t.Fatal(err)
		}
		n++
	}
	if n != 1 {
		t.Fatalf("iter: %d", n)
	}

	if err := c.Metadata.DeleteEntity(ctx, *p.ObjectID, *e.ObjectID); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Metadata.GetEntity(ctx, *p.ObjectID, *e.ObjectID); !errors.Is(err, client.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestUpsertEntityUnknownProject(t *testing.T) {
	t.Parallel()
	c, _ := newTestClient(t)
	_, err := c.Metadata.UpsertEntity(context.Background(),
		client.MetadataEntity{ProjectID: uuid.New(), Name: "orphan"})
	if !errors.Is(err, client.ErrNotFound) {
		t.Fatalf("want ErrNotFound (unknown/foreign project), got %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd client && go test ./... -run 'TestEntityLifecycle|TestUpsertEntityUnknown' -v -timeout 20m
```
Expected: FAIL — `c.Metadata.UpsertEntity undefined`.

- [ ] **Step 3: Write minimal implementation**

Append to `client/metadata.go`:
```go
// UpsertEntity creates or adopts an entity in e.ProjectID.
func (s *MetadataService) UpsertEntity(ctx context.Context, e MetadataEntity, opts ...WriteOption) (MetadataEntity, error) {
	var out MetadataEntity
	err := s.c.do(ctx, "POST", "/metadata/projects/"+e.ProjectID.String()+"/entities",
		writeValues(opts), e, &out)
	return out, err
}

// GetEntity fetches one entity.
func (s *MetadataService) GetEntity(ctx context.Context, projectID, entityID uuid.UUID) (MetadataEntity, error) {
	var out MetadataEntity
	err := s.c.do(ctx, "GET",
		"/metadata/projects/"+projectID.String()+"/entities/"+entityID.String(), nil, nil, &out)
	return out, err
}

// DeleteEntity deletes an entity; its fields cascade server-side.
func (s *MetadataService) DeleteEntity(ctx context.Context, projectID, entityID uuid.UUID, opts ...WriteOption) error {
	var out struct {
		Detail string `json:"detail"`
	}
	return s.c.do(ctx, "DELETE",
		"/metadata/projects/"+projectID.String()+"/entities/"+entityID.String(),
		writeValues(opts), nil, &out)
}

// ListEntities returns one page of a project's entities.
func (s *MetadataService) ListEntities(ctx context.Context, projectID uuid.UUID, opts ListOpts) (Page[MetadataEntity], error) {
	return listPage[MetadataEntity](ctx, s.c, "/metadata/projects/"+projectID.String()+"/entities", opts)
}

// IterEntities auto-pages through a project's entities.
func (s *MetadataService) IterEntities(ctx context.Context, projectID uuid.UUID, opts ListOpts) iter.Seq2[MetadataEntity, error] {
	return iterPages(ctx, opts, func(ctx context.Context, o ListOpts) (Page[MetadataEntity], error) {
		return s.ListEntities(ctx, projectID, o)
	})
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
cd client && go test ./... -run 'TestEntityLifecycle|TestUpsertEntityUnknown' -v -timeout 20m
```
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add client/metadata.go client/metadata_entities_test.go
git commit -m "feat(client): entity CRUD and pagination"
```

---

### Task 15: Field methods

**Files:**
- Modify: `client/metadata.go`
- Test: `client/metadata_fields_test.go`

**Interfaces:**
- Consumes: Tasks 12–14.
- Produces: `UpsertField(ctx, MetadataField, ...WriteOption) (MetadataField, error)` (URL from `f.ProjectID`+`f.EntityID`); `GetField(ctx, projectID, entityID, fieldID uuid.UUID)`; `DeleteField`; `ListFields(ctx, projectID, entityID uuid.UUID, ListOpts)`; `IterFields`.

- [ ] **Step 1: Write the failing test**

`client/metadata_fields_test.go`:
```go
// SPDX-License-Identifier: MPL-2.0

package client_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"gitlab.com/leifwind/stream/terraform-provider-leifwind/client"
)

func fieldFixture(t *testing.T) (*client.Client, uuid.UUID, uuid.UUID) {
	t.Helper()
	c, _ := newTestClient(t)
	ctx := context.Background()
	p, err := c.Metadata.UpsertProject(ctx, client.MetadataProject{Name: "fld_proj"})
	if err != nil {
		t.Fatal(err)
	}
	e, err := c.Metadata.UpsertEntity(ctx, client.MetadataEntity{ProjectID: *p.ObjectID, Name: "book"})
	if err != nil {
		t.Fatal(err)
	}
	return c, *p.ObjectID, *e.ObjectID
}

func TestFieldLifecycleKeyAndFragment(t *testing.T) {
	t.Parallel()
	c, pid, eid := fieldFixture(t)
	ctx := context.Background()

	key, err := c.Metadata.UpsertField(ctx, client.MetadataField{
		ProjectID: pid, EntityID: eid, Name: "title",
		Config:     client.FieldConfig{DataType: client.DataTypeText},
		Connection: client.Connection{Type: client.ConnectionKey},
	})
	if err != nil {
		t.Fatal(err)
	}

	frag, err := c.Metadata.UpsertField(ctx, client.MetadataField{
		ProjectID: pid, EntityID: eid, Name: "body",
		Config:     client.FieldConfig{DataType: client.DataTypeText},
		Connection: client.Connection{Type: client.ConnectionFragment, FragmentName: "content"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if frag.Connection.FragmentName != "content" {
		t.Fatalf("fragment_name lost: %+v", frag.Connection)
	}

	// fragment_name is the ONLY mutable field attribute
	frag.Connection.FragmentName = "content_v2"
	updated, err := c.Metadata.UpsertField(ctx, frag)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Connection.FragmentName != "content_v2" {
		t.Fatalf("fragment_name not updated: %+v", updated.Connection)
	}

	// data_type is immutable
	bad := key
	bad.Config.DataType = client.DataTypeInteger
	if _, err := c.Metadata.UpsertField(ctx, bad); !errors.Is(err, client.ErrValidation) {
		t.Fatalf("want ErrValidation on data_type change, got %v", err)
	}

	got, err := c.Metadata.GetField(ctx, pid, eid, *key.ObjectID)
	if err != nil || got.Name != "title" {
		t.Fatalf("get: %+v, %v", got, err)
	}

	page, err := c.Metadata.ListFields(ctx, pid, eid, client.ListOpts{})
	if err != nil || len(page.Objects) != 2 {
		t.Fatalf("list: %d, %v", len(page.Objects), err)
	}

	if err := c.Metadata.DeleteField(ctx, pid, eid, *key.ObjectID); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Metadata.GetField(ctx, pid, eid, *key.ObjectID); !errors.Is(err, client.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestListFieldsBogusEntity404(t *testing.T) {
	t.Parallel()
	c, pid, _ := fieldFixture(t)
	_, err := c.Metadata.ListFields(context.Background(), pid, uuid.New(), client.ListOpts{})
	if !errors.Is(err, client.ErrNotFound) {
		t.Fatalf("want ErrNotFound for bogus entity (not empty list), got %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd client && go test ./... -run 'TestFieldLifecycle|TestListFieldsBogus' -v -timeout 20m
```
Expected: FAIL — `c.Metadata.UpsertField undefined`.

- [ ] **Step 3: Write minimal implementation**

Append to `client/metadata.go`:
```go
// UpsertField creates or adopts a field. Only Connection.FragmentName is
// mutable on an existing field; data_type/connection_type changes → ErrValidation.
func (s *MetadataService) UpsertField(ctx context.Context, f MetadataField, opts ...WriteOption) (MetadataField, error) {
	var out MetadataField
	err := s.c.do(ctx, "POST",
		"/metadata/projects/"+f.ProjectID.String()+"/entities/"+f.EntityID.String()+"/fields",
		writeValues(opts), f, &out)
	return out, err
}

// GetField fetches one field.
func (s *MetadataService) GetField(ctx context.Context, projectID, entityID, fieldID uuid.UUID) (MetadataField, error) {
	var out MetadataField
	err := s.c.do(ctx, "GET",
		"/metadata/projects/"+projectID.String()+"/entities/"+entityID.String()+"/fields/"+fieldID.String(),
		nil, nil, &out)
	return out, err
}

// DeleteField deletes a field (drops the backing column server-side).
func (s *MetadataService) DeleteField(ctx context.Context, projectID, entityID, fieldID uuid.UUID, opts ...WriteOption) error {
	var out struct {
		Detail string `json:"detail"`
	}
	return s.c.do(ctx, "DELETE",
		"/metadata/projects/"+projectID.String()+"/entities/"+entityID.String()+"/fields/"+fieldID.String(),
		writeValues(opts), nil, &out)
}

// ListFields returns one page of an entity's fields (404 for bogus entities).
func (s *MetadataService) ListFields(ctx context.Context, projectID, entityID uuid.UUID, opts ListOpts) (Page[MetadataField], error) {
	return listPage[MetadataField](ctx, s.c,
		"/metadata/projects/"+projectID.String()+"/entities/"+entityID.String()+"/fields", opts)
}

// IterFields auto-pages through an entity's fields.
func (s *MetadataService) IterFields(ctx context.Context, projectID, entityID uuid.UUID, opts ListOpts) iter.Seq2[MetadataField, error] {
	return iterPages(ctx, opts, func(ctx context.Context, o ListOpts) (Page[MetadataField], error) {
		return s.ListFields(ctx, projectID, entityID, o)
	})
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
cd client && go test ./... -run 'TestFieldLifecycle|TestListFieldsBogus' -v -timeout 20m
```
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add client/metadata.go client/metadata_fields_test.go
git commit -m "feat(client): field CRUD with fragment semantics and immutability checks"
```

---

### Task 16: Generic service — entity fragments

**Files:**
- Modify: `client/generic.go`
- Test: `client/generic_test.go`

**Interfaces:**
- Consumes: Tasks 12, 15.
- Produces: `(s *GenericService) ListEntityFragments(ctx context.Context, projectID uuid.UUID, entityName string) ([]string, error)`.

- [ ] **Step 1: Write the failing test**

`client/generic_test.go`:
```go
// SPDX-License-Identifier: MPL-2.0

package client_test

import (
	"context"
	"sort"
	"testing"

	"gitlab.com/leifwind/stream/terraform-provider-leifwind/client"
)

func TestListEntityFragments(t *testing.T) {
	t.Parallel()
	c, _ := newTestClient(t)
	ctx := context.Background()
	p, err := c.Metadata.UpsertProject(ctx, client.MetadataProject{Name: "frag_proj"})
	if err != nil {
		t.Fatal(err)
	}
	e, err := c.Metadata.UpsertEntity(ctx, client.MetadataEntity{ProjectID: *p.ObjectID, Name: "doc"})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range []struct{ field, fragment string }{
		{"body", "content"}, {"meta", "annotations"},
	} {
		if _, err := c.Metadata.UpsertField(ctx, client.MetadataField{
			ProjectID: *p.ObjectID, EntityID: *e.ObjectID, Name: f.field,
			Config:     client.FieldConfig{DataType: client.DataTypeText},
			Connection: client.Connection{Type: client.ConnectionFragment, FragmentName: f.fragment},
		}); err != nil {
			t.Fatal(err)
		}
	}

	frags, err := c.Generic.ListEntityFragments(ctx, *p.ObjectID, "doc")
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(frags)
	if len(frags) != 2 || frags[0] != "annotations" || frags[1] != "content" {
		t.Fatalf("got %v", frags)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd client && go test ./... -run TestListEntityFragments -v -timeout 20m
```
Expected: FAIL — `c.Generic.ListEntityFragments undefined`.

- [ ] **Step 3: Write minimal implementation**

Append to `client/generic.go`:
```go
import (
	"context"

	"github.com/google/uuid"
)

// ListEntityFragments returns the fragment names of an entity (derived from
// its FRAGMENT-connection fields). entityName accepts a name or UUID string.
func (s *GenericService) ListEntityFragments(ctx context.Context, projectID uuid.UUID, entityName string) ([]string, error) {
	var out struct {
		Fragments []string `json:"fragments"`
	}
	err := s.c.do(ctx, "GET",
		"/generic/projects/"+projectID.String()+"/schemas/entities/"+entityName+"/fragments",
		nil, nil, &out)
	return out.Fragments, err
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
cd client && go test ./... -run TestListEntityFragments -v -timeout 20m
```
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add client/generic.go client/generic_test.go
git commit -m "feat(client): entity fragments read endpoint"
```

---

### Task 17: Retry engine + toxiproxy fault injection

**Files:**
- Create: `client/retry.go`, `client/leifwindtest/toxiproxy.go`
- Modify: `client/client.go` (route `do` through the retry loop)
- Test: `client/retry_test.go`

**Interfaces:**
- Consumes: everything client-side; fixture `WithToxiproxy` stub (T12).
- Produces: retry semantics (transport errors + 5xx, all verbs, DELETE-404 tolerance after a failed attempt, 4xx never retried, context-aware backoff); `Stack.ProxiedBackendURL`; `(*Stack).Toxiproxy() *toxiproxy.Client`.

Policy (spec OPEN #2): default `{MaxAttempts: 3, MinBackoff: 250ms, MaxBackoff: 4s}` with full jitter. Retryable: transport errors and status ≥ 500, every verb (upserts/deletes are idempotent by natural key). A DELETE that gets 404 on attempt > 1 is treated as success (the earlier attempt may have landed). 4xx is never retried.

- [ ] **Step 1: Write the failing test**

`client/retry_test.go`:
```go
// SPDX-License-Identifier: MPL-2.0

package client_test

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"gitlab.com/leifwind/stream/terraform-provider-leifwind/client"
)

// proxiedClient returns a client whose traffic crosses toxiproxy, plus the
// counting transport for attempt assertions.
func proxiedClient(t *testing.T, rc client.RetryConfig) (*client.Client, *countingTransport) {
	t.Helper()
	orgMu.Lock()
	org := sharedStack.NewOrg(t)
	orgMu.Unlock()
	ct := &countingTransport{next: http.DefaultTransport}
	c, err := client.New(sharedStack.ProxiedBackendURL,
		client.WithTokenSource(org.TokenSource(sharedStack)),
		client.WithHTTPClient(&http.Client{Transport: ct, Timeout: 30 * time.Second}),
		client.WithRetry(rc))
	if err != nil {
		t.Fatal(err)
	}
	return c, ct
}

func TestRetriesTransportErrorThenSucceeds(t *testing.T) {
	proxy := sharedStack.Toxiproxy() // shared proxy: no t.Parallel in this file
	c, ct := proxiedClient(t, client.RetryConfig{MaxAttempts: 5, MinBackoff: 300 * time.Millisecond, MaxBackoff: time.Second})

	if err := proxy.Disable(); err != nil {
		t.Fatal(err)
	}
	go func() {
		time.Sleep(700 * time.Millisecond)
		_ = proxy.Enable()
	}()
	_, err := c.Metadata.ListProjects(context.Background(), client.ListOpts{})
	if err != nil {
		t.Fatalf("expected recovery via retry, got %v", err)
	}
	if ct.calls.Load() < 2 {
		t.Fatalf("expected ≥2 attempts, got %d", ct.calls.Load())
	}
}

func TestNoRetryOn4xx(t *testing.T) {
	c, ct := proxiedClient(t, client.RetryConfig{MaxAttempts: 5, MinBackoff: 100 * time.Millisecond, MaxBackoff: time.Second})
	ctx := context.Background()
	p, err := c.Metadata.UpsertProject(ctx, client.MetadataProject{Name: "retry_proj"})
	if err != nil {
		t.Fatal(err)
	}
	before := ct.calls.Load()
	_, err = c.Metadata.UpsertProject(ctx, client.MetadataProject{ObjectID: p.ObjectID, Name: "renamed"})
	if !errors.Is(err, client.ErrValidation) {
		t.Fatalf("want ErrValidation, got %v", err)
	}
	if ct.calls.Load()-before != 1 {
		t.Fatalf("4xx must not retry: %d attempts", ct.calls.Load()-before)
	}
}

func TestContextCancelAbortsBackoff(t *testing.T) {
	proxy := sharedStack.Toxiproxy()
	c, _ := proxiedClient(t, client.RetryConfig{MaxAttempts: 10, MinBackoff: 2 * time.Second, MaxBackoff: 8 * time.Second})
	if err := proxy.Disable(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = proxy.Enable() }()

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := c.Metadata.ListProjects(ctx, client.ListOpts{})
	if err == nil {
		t.Fatal("expected error")
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("backoff ignored context: took %v", elapsed)
	}
}

func TestDeleteRetryTolerates404(t *testing.T) {
	proxy := sharedStack.Toxiproxy()
	c, _ := proxiedClient(t, client.RetryConfig{MaxAttempts: 4, MinBackoff: 400 * time.Millisecond, MaxBackoff: 2 * time.Second})
	ctx := context.Background()
	p, err := c.Metadata.UpsertProject(ctx, client.MetadataProject{Name: "del_retry"})
	if err != nil {
		t.Fatal(err)
	}

	// Truncate the RESPONSE of the next call: the backend processes the
	// DELETE, the client sees a transport error, the retry sees 404 —
	// which must be treated as success.
	toxic, err := proxy.AddToxic("truncate-down", "limit_data", "downstream", 1.0,
		map[string]any{"bytes": 1})
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		time.Sleep(600 * time.Millisecond)
		_ = proxy.RemoveToxic(toxic.Name)
	}()

	if err := c.Metadata.DeleteProject(ctx, *p.ObjectID); err != nil {
		t.Fatalf("DELETE retry must tolerate 404 after failed attempt, got %v", err)
	}
	if _, err := c.Metadata.GetProject(ctx, *p.ObjectID); !errors.Is(err, client.ErrNotFound) {
		t.Fatalf("project should be gone, got %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd client && go get github.com/Shopify/toxiproxy/v2/client@latest && go test ./... -run 'TestRetries|TestNoRetry|TestContextCancel|TestDeleteRetry' -v -timeout 20m
```
Expected: FAIL — `sharedStack.Toxiproxy undefined` / `ProxiedBackendURL` empty.

- [ ] **Step 3: Write minimal implementation**

`client/leifwindtest/toxiproxy.go`:
```go
// SPDX-License-Identifier: MPL-2.0

package leifwindtest

import (
	"fmt"
	"time"

	toxiproxy "github.com/Shopify/toxiproxy/v2/client"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

const toxiproxyImage = "ghcr.io/shopify/toxiproxy:2.12.0"

// Toxiproxy returns the control handle for the backend proxy.
// Panics unless the stack was started WithToxiproxy().
func (s *Stack) Toxiproxy() *toxiproxy.Proxy {
	if s.backendProxy == nil {
		panic("leifwindtest: stack started without WithToxiproxy()")
	}
	return s.backendProxy
}

func (s *Stack) startToxiproxy() error {
	ctx := s.ctx
	tp, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:          toxiproxyImage,
			Networks:       []string{s.net.Name},
			NetworkAliases: map[string][]string{s.net.Name: {"toxiproxy"}},
			ExposedPorts:   []string{"8474/tcp", "8666/tcp"},
			WaitingFor:     wait.ForHTTP("/version").WithPort("8474/tcp").WithStartupTimeout(60 * time.Second),
		},
		Started: true,
	})
	if err != nil {
		return fmt.Errorf("toxiproxy: %w", err)
	}
	s.deferCleanup(terminate(ctx, tp))

	host, err := tp.Host(ctx)
	if err != nil {
		return err
	}
	adminPort, err := tp.MappedPort(ctx, "8474/tcp")
	if err != nil {
		return err
	}
	dataPort, err := tp.MappedPort(ctx, "8666/tcp")
	if err != nil {
		return err
	}

	tpc := toxiproxy.NewClient(fmt.Sprintf("%s:%s", host, adminPort.Port()))
	proxy, err := tpc.CreateProxy("backend", "0.0.0.0:8666", "backend:8000")
	if err != nil {
		return fmt.Errorf("create proxy: %w", err)
	}
	s.backendProxy = proxy
	s.ProxiedBackendURL = fmt.Sprintf("http://%s:%s", host, dataPort.Port())
	return nil
}
```
In `stack.go`, add the field `backendProxy *toxiproxy.Proxy` to `Stack` (import the toxiproxy client), and at the end of `startBackend` in `backend.go` replace `_ = withToxiproxy // Task 17` with:
```go
	if withToxiproxy {
		if err := s.startToxiproxy(); err != nil {
			return err
		}
	}
	return nil
```

`client/retry.go`:
```go
// SPDX-License-Identifier: MPL-2.0

package client

import (
	"context"
	"errors"
	"math/rand/v2"
	"net/http"
	"net/url"
	"time"
)

// doRetry wraps doOnce: retries transport errors and 5xx for every verb
// (upserts/deletes are idempotent by natural-key design). 4xx never retries.
// A DELETE answered 404 on attempt > 1 is success: the earlier attempt may
// have been executed server-side before the connection failed.
func (c *Client) doRetry(ctx context.Context, method, path string, query url.Values, body, out any) error {
	max := c.retry.MaxAttempts
	if max < 1 {
		max = 1
	}
	var lastErr error
	for attempt := 1; attempt <= max; attempt++ {
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
		}
		lastErr = err
		if attempt == max {
			break
		}
		if err := sleepBackoff(ctx, c.retry, attempt); err != nil {
			return lastErr
		}
	}
	return lastErr
}

func sleepBackoff(ctx context.Context, rc RetryConfig, attempt int) error {
	backoff := rc.MinBackoff << (attempt - 1)
	if backoff > rc.MaxBackoff || backoff <= 0 {
		backoff = rc.MaxBackoff
	}
	// full jitter
	d := time.Duration(rand.Int64N(int64(backoff) + 1))
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
```
In `client/client.go`, change `do` to delegate:
```go
func (c *Client) do(ctx context.Context, method, path string, query url.Values, body, out any) error {
	return c.doRetry(ctx, method, path, query, body, out)
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
cd client && go test ./... -v -timeout 30m
```
Expected: PASS — the WHOLE client suite (regression check: retries must not break earlier tests).

- [ ] **Step 5: Commit**

```bash
git add client/retry.go client/client.go client/leifwindtest/toxiproxy.go client/leifwindtest/stack.go client/leifwindtest/backend.go client/retry_test.go client/go.mod client/go.sum
git commit -m "feat(client): idempotent retries with jittered backoff, proven via toxiproxy faults"
```

---

### Task 18: Provider config resolution (pure logic)

**Files:**
- Create: `internal/provider/config.go`
- Test: `internal/provider/config_test.go`

**Interfaces:**
- Consumes: nothing (pure logic, no framework types).
- Produces: `RawConfig{Endpoint, Token, Issuer, ClientID, ClientSecret, Audience *string}`; `Resolved{Endpoint, Token, Issuer, ClientID, ClientSecret, Audience string; UseM2M bool}`; `resolveConfig(raw RawConfig, getenv func(string) string) (Resolved, []string)` — second return is human-readable error strings (provider.Configure maps them to diagnostics). Env names: `LEIFWIND_ENDPOINT`, `LEIFWIND_TOKEN`, `LEIFWIND_OIDC_ISSUER`, `LEIFWIND_CLIENT_ID`, `LEIFWIND_CLIENT_SECRET`, `LEIFWIND_OIDC_AUDIENCE`.

Rules (spec §provider design): endpoint required (attr or env); `token` XOR the M2M trio; if any M2M attr is set, ALL of issuer/client_id/client_secret must resolve (audience optional); if neither path resolves → error listing both options with env names.

- [ ] **Step 1: Write the failing test**

`internal/provider/config_test.go`:
```go
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"strings"
	"testing"
)

func ptr(s string) *string { return &s }

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestResolveConfigStaticToken(t *testing.T) {
	r, errs := resolveConfig(RawConfig{Endpoint: ptr("https://api.example"), Token: ptr("tok")}, env(nil))
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if r.UseM2M || r.Token != "tok" || r.Endpoint != "https://api.example" {
		t.Fatalf("bad resolve: %+v", r)
	}
}

func TestResolveConfigEnvFallbacks(t *testing.T) {
	r, errs := resolveConfig(RawConfig{}, env(map[string]string{
		"LEIFWIND_ENDPOINT": "https://api.example",
		"LEIFWIND_TOKEN":    "envtok",
	}))
	if len(errs) != 0 || r.Token != "envtok" {
		t.Fatalf("env fallback failed: %+v %v", r, errs)
	}
}

func TestResolveConfigM2M(t *testing.T) {
	r, errs := resolveConfig(RawConfig{
		Endpoint: ptr("https://api.example"),
		Issuer:   ptr("https://auth.example"), ClientID: ptr("id"), ClientSecret: ptr("sec"),
		Audience: ptr("326102453042806786"),
	}, env(nil))
	if len(errs) != 0 || !r.UseM2M || r.Audience != "326102453042806786" {
		t.Fatalf("m2m resolve failed: %+v %v", r, errs)
	}
}

func TestResolveConfigMutualExclusion(t *testing.T) {
	_, errs := resolveConfig(RawConfig{
		Endpoint: ptr("https://api.example"), Token: ptr("tok"), Issuer: ptr("https://auth.example"),
	}, env(nil))
	if len(errs) != 1 || !strings.Contains(errs[0], "mutually exclusive") {
		t.Fatalf("want mutual-exclusion error, got %v", errs)
	}
}

func TestResolveConfigIncompleteM2M(t *testing.T) {
	_, errs := resolveConfig(RawConfig{
		Endpoint: ptr("https://api.example"), Issuer: ptr("https://auth.example"),
	}, env(nil))
	if len(errs) != 1 || !strings.Contains(errs[0], "LEIFWIND_CLIENT_ID") || !strings.Contains(errs[0], "LEIFWIND_CLIENT_SECRET") {
		t.Fatalf("want missing-attr error naming env vars, got %v", errs)
	}
}

func TestResolveConfigNothing(t *testing.T) {
	_, errs := resolveConfig(RawConfig{Endpoint: ptr("https://api.example")}, env(nil))
	if len(errs) != 1 || !strings.Contains(errs[0], "LEIFWIND_TOKEN") {
		t.Fatalf("want no-auth error, got %v", errs)
	}
}

func TestResolveConfigMissingEndpoint(t *testing.T) {
	_, errs := resolveConfig(RawConfig{Token: ptr("tok")}, env(nil))
	if len(errs) != 1 || !strings.Contains(errs[0], "LEIFWIND_ENDPOINT") {
		t.Fatalf("want endpoint error, got %v", errs)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/provider/ -v
```
Expected: FAIL — `undefined: resolveConfig`.

- [ ] **Step 3: Write minimal implementation**

`internal/provider/config.go`:
```go
// SPDX-License-Identifier: MPL-2.0

package provider

// RawConfig is the provider configuration before env-fallback resolution.
// nil = attribute not set in HCL.
type RawConfig struct {
	Endpoint     *string
	Token        *string
	Issuer       *string
	ClientID     *string
	ClientSecret *string
	Audience     *string
}

// Resolved is the effective configuration after env merging.
type Resolved struct {
	Endpoint     string
	Token        string
	Issuer       string
	ClientID     string
	ClientSecret string
	Audience     string
	UseM2M       bool
}

func pick(attr *string, envName string, getenv func(string) string) string {
	if attr != nil && *attr != "" {
		return *attr
	}
	return getenv(envName)
}

// resolveConfig merges attributes with LEIFWIND_* env fallbacks and
// validates: endpoint required; token XOR complete client_credentials trio.
func resolveConfig(raw RawConfig, getenv func(string) string) (Resolved, []string) {
	r := Resolved{
		Endpoint:     pick(raw.Endpoint, "LEIFWIND_ENDPOINT", getenv),
		Token:        pick(raw.Token, "LEIFWIND_TOKEN", getenv),
		Issuer:       pick(raw.Issuer, "LEIFWIND_OIDC_ISSUER", getenv),
		ClientID:     pick(raw.ClientID, "LEIFWIND_CLIENT_ID", getenv),
		ClientSecret: pick(raw.ClientSecret, "LEIFWIND_CLIENT_SECRET", getenv),
		Audience:     pick(raw.Audience, "LEIFWIND_OIDC_AUDIENCE", getenv),
	}
	var errs []string
	if r.Endpoint == "" {
		errs = append(errs, "endpoint is required: set the endpoint attribute or LEIFWIND_ENDPOINT")
	}
	anyM2M := r.Issuer != "" || r.ClientID != "" || r.ClientSecret != ""
	switch {
	case r.Token != "" && anyM2M:
		errs = append(errs, "token and issuer/client_id/client_secret are mutually exclusive: configure either a static token or client_credentials, not both")
	case r.Token != "":
		// static token path — ok
	case anyM2M:
		missing := ""
		if r.Issuer == "" {
			missing += " issuer (LEIFWIND_OIDC_ISSUER)"
		}
		if r.ClientID == "" {
			missing += " client_id (LEIFWIND_CLIENT_ID)"
		}
		if r.ClientSecret == "" {
			missing += " client_secret (LEIFWIND_CLIENT_SECRET)"
		}
		if missing != "" {
			errs = append(errs, "incomplete client_credentials configuration, missing:"+missing)
		} else {
			r.UseM2M = true
		}
	default:
		errs = append(errs, "no credentials: set token (LEIFWIND_TOKEN) or issuer/client_id/client_secret (LEIFWIND_OIDC_ISSUER, LEIFWIND_CLIENT_ID, LEIFWIND_CLIENT_SECRET)")
	}
	return r, errs
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
go test ./internal/provider/ -v
```
Expected: PASS (7 tests).

- [ ] **Step 5: Commit**

```bash
git add internal/provider/config.go internal/provider/config_test.go
git commit -m "feat(provider): config resolution with LEIFWIND_* env fallbacks and XOR validation"
```

---

### Task 19: main.go, provider schema/Configure, acctest harness

**Files:**
- Create: `main.go`, `internal/provider/provider.go`, `internal/acctest/acctest.go`, `internal/acctest/main_test.go`
- Modify: `go.mod` (framework deps + client requirement), `internal/provider/doc.go` (delete; superseded by provider.go)

**Interfaces:**
- Consumes: `resolveConfig` (T18), `client.New`/TokenSources (T9/T12), `leifwindtest.StartMain` (T7/T11).
- Produces: `provider.New(version string) func() provider.Provider` (registers resources/data sources added by later tasks); `acctest.ProtoV6ProviderFactories()`; `acctest.PreCheck(t)`; `acctest.Stack() *leifwindtest.Stack`; `acctest.NewOrg(t) *leifwindtest.Org`; `acctest.ProviderConfig(token string) string`; `acctest.ProviderConfigM2M(org *leifwindtest.Org) string`; the acceptance `TestMain` booting one shared stack when `TF_ACC=1`.

- [ ] **Step 1: Wire module dependencies**

The provider go.mod needs the client module. Until `client/v0.1.0` exists, pin a placeholder version — the committed `go.work` makes builds use the workspace copy; the release-time `GOWORK=off` proof is deferred to LW-68 (after the first client tag). 

```bash
go mod edit -require=gitlab.com/leifwind/stream/terraform-provider-leifwind/client@v0.0.0-00010101000000-000000000000
go get github.com/hashicorp/terraform-plugin-framework@v1.19.0
go get github.com/hashicorp/terraform-plugin-go@latest
go get github.com/hashicorp/terraform-plugin-testing@v1.16.0
go get github.com/google/uuid@latest
go mod tidy
```
Expected: `go mod tidy` succeeds (workspace resolves the client).

- [ ] **Step 2: Write the failing test**

`internal/acctest/main_test.go`:
```go
// SPDX-License-Identifier: MPL-2.0

package acctest

import (
	"fmt"
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	if os.Getenv("TF_ACC") == "" {
		// acceptance gate: plain `go test` skips container boot entirely
		os.Exit(m.Run())
	}
	if err := startShared(); err != nil {
		fmt.Fprintf(os.Stderr, "leifwindtest stack: %v\n", err)
		os.Exit(1)
	}
	code := m.Run()
	stopShared()
	os.Exit(code)
}

func TestProviderFactorySmoke(t *testing.T) {
	// compile-time smoke: the factory must produce a protocol-6 server
	if _, err := ProtoV6ProviderFactories()["leifwind"](); err != nil {
		t.Fatal(err)
	}
}
```

Run:
```bash
go test ./internal/acctest/ -run TestProviderFactorySmoke -v
```
Expected: FAIL — `undefined: ProtoV6ProviderFactories`.

- [ ] **Step 3: Write minimal implementation**

`main.go`:
```go
// SPDX-License-Identifier: MPL-2.0

package main

import (
	"context"
	"flag"
	"log"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"

	"gitlab.com/leifwind/stream/terraform-provider-leifwind/internal/provider"
)

// version is set by goreleaser via ldflags.
var version = "dev"

func main() {
	var debug bool
	flag.BoolVar(&debug, "debug", false, "run with support for debuggers like delve")
	flag.Parse()

	err := providerserver.Serve(context.Background(), provider.New(version), providerserver.ServeOpts{
		Address: "registry.terraform.io/leifwind-io/leifwind",
		Debug:   debug,
	})
	if err != nil {
		log.Fatal(err)
	}
}
```

`internal/provider/provider.go` (replaces doc.go — delete `internal/provider/doc.go`):
```go
// SPDX-License-Identifier: MPL-2.0

// Package provider implements the leifwind Terraform/OpenTofu provider.
package provider

import (
	"context"
	"os"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"gitlab.com/leifwind/stream/terraform-provider-leifwind/client"
)

var _ provider.Provider = (*leifwindProvider)(nil)

type leifwindProvider struct {
	version string
}

// New returns the provider factory used by main and by acceptance tests.
func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &leifwindProvider{version: version}
	}
}

type providerModel struct {
	Endpoint     types.String `tfsdk:"endpoint"`
	Token        types.String `tfsdk:"token"`
	Issuer       types.String `tfsdk:"issuer"`
	ClientID     types.String `tfsdk:"client_id"`
	ClientSecret types.String `tfsdk:"client_secret"`
	Audience     types.String `tfsdk:"audience"`
}

func (p *leifwindProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "leifwind"
	resp.Version = p.version
}

func (p *leifwindProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manage leifwind metadata (projects, entities, fields) via the metadata API.",
		Attributes: map[string]schema.Attribute{
			"endpoint": schema.StringAttribute{
				Optional:    true,
				Description: "Backend base URL. Falls back to LEIFWIND_ENDPOINT.",
			},
			"token": schema.StringAttribute{
				Optional:    true,
				Sensitive:   true,
				Description: "Static bearer token (delegated/runner path). Falls back to LEIFWIND_TOKEN. Mutually exclusive with issuer/client_id/client_secret.",
			},
			"issuer": schema.StringAttribute{
				Optional:    true,
				Description: "ZITADEL issuer URL for client_credentials. Falls back to LEIFWIND_OIDC_ISSUER.",
			},
			"client_id": schema.StringAttribute{
				Optional:    true,
				Description: "OAuth client id. Falls back to LEIFWIND_CLIENT_ID.",
			},
			"client_secret": schema.StringAttribute{
				Optional:    true,
				Sensitive:   true,
				Description: "OAuth client secret. Falls back to LEIFWIND_CLIENT_SECRET.",
			},
			"audience": schema.StringAttribute{
				Optional:    true,
				Description: "ZITADEL API project id (audience scope). Falls back to LEIFWIND_OIDC_AUDIENCE.",
			},
		},
	}
}

func (p *leifwindProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var model providerModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() {
		return
	}
	raw := RawConfig{
		Endpoint:     model.Endpoint.ValueStringPointer(),
		Token:        model.Token.ValueStringPointer(),
		Issuer:       model.Issuer.ValueStringPointer(),
		ClientID:     model.ClientID.ValueStringPointer(),
		ClientSecret: model.ClientSecret.ValueStringPointer(),
		Audience:     model.Audience.ValueStringPointer(),
	}
	resolved, errs := resolveConfig(raw, os.Getenv)
	for _, e := range errs {
		resp.Diagnostics.AddError("Invalid provider configuration", e)
	}
	if resp.Diagnostics.HasError() {
		return
	}

	var ts client.TokenSource
	if resolved.UseM2M {
		ccOpts := []client.CredentialOption{}
		if resolved.Audience != "" {
			ccOpts = append(ccOpts, client.WithAudience(resolved.Audience))
		}
		ts = client.ClientCredentials(resolved.Issuer, resolved.ClientID, resolved.ClientSecret, ccOpts...)
	} else {
		ts = client.StaticToken(resolved.Token)
	}

	c, err := client.New(resolved.Endpoint,
		client.WithTokenSource(ts),
		client.WithUserAgent("terraform-provider-leifwind/"+p.version))
	if err != nil {
		resp.Diagnostics.AddError("Failed to construct API client", err.Error())
		return
	}
	resp.ResourceData = c
	resp.DataSourceData = c
}

func (p *leifwindProvider) Resources(context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		// appended by resource tasks
	}
}

func (p *leifwindProvider) DataSources(context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		// appended by data-source tasks
	}
}
```

`internal/acctest/acctest.go`:
```go
// SPDX-License-Identifier: MPL-2.0

// Package acctest hosts ALL acceptance tests and their shared harness.
package acctest

import (
	"fmt"
	"os"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"

	"gitlab.com/leifwind/stream/terraform-provider-leifwind/client/leifwindtest"
	"gitlab.com/leifwind/stream/terraform-provider-leifwind/internal/provider"
)

var (
	shared        *leifwindtest.Stack
	sharedCleanup func()
	orgMu         sync.Mutex
)

func startShared() error {
	var err error
	shared, sharedCleanup, err = leifwindtest.StartMain()
	return err
}

func stopShared() {
	if sharedCleanup != nil {
		sharedCleanup()
	}
}

// Stack returns the shared containerized stack (TF_ACC runs only).
func Stack() *leifwindtest.Stack { return shared }

// NewOrg mints a fresh isolated tenant.
func NewOrg(t *testing.T) *leifwindtest.Org {
	t.Helper()
	orgMu.Lock()
	defer orgMu.Unlock()
	return shared.NewOrg(t)
}

// PreCheck gates a test on TF_ACC.
func PreCheck(t *testing.T) {
	t.Helper()
	if os.Getenv("TF_ACC") == "" {
		t.Skip("set TF_ACC=1 to run acceptance tests")
	}
}

// ProtoV6ProviderFactories serves the provider in-process (protocol 6).
func ProtoV6ProviderFactories() map[string]func() (tfprotov6.ProviderServer, error) {
	return map[string]func() (tfprotov6.ProviderServer, error){
		"leifwind": providerserver.NewProtocol6WithError(provider.New("acctest")()),
	}
}

// ProviderConfig renders a provider block with a static token.
func ProviderConfig(token string) string {
	return fmt.Sprintf(`
provider "leifwind" {
  endpoint = %q
  token    = %q
}
`, shared.BackendURL, token)
}

// ProviderConfigM2M renders a provider block using client_credentials.
func ProviderConfigM2M(org *leifwindtest.Org) string {
	return fmt.Sprintf(`
provider "leifwind" {
  endpoint      = %q
  issuer        = %q
  client_id     = %q
  client_secret = %q
  audience      = %q
}
`, shared.BackendURL, shared.Issuer, org.ClientID, org.ClientSecret, shared.Audience)
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go build ./... && go test ./internal/... -v
```
Expected: PASS (`TestProviderFactorySmoke`, config tests; no container boot — TF_ACC unset).

- [ ] **Step 5: Commit**

```bash
git rm internal/provider/doc.go
git add main.go internal/provider/provider.go internal/acctest/ go.mod go.sum
git commit -m "feat(provider): provider schema/Configure and shared acceptance harness"
```

---

### Task 20: leifwind_project resource

**Files:**
- Create: `internal/metadatares/doc.go`, `internal/metadatares/project.go`, `internal/metadatares/import.go`, `internal/metadatares/exists.go`
- Modify: `internal/provider/provider.go` (register resource)
- Test: `internal/metadatares/import_test.go`, `internal/acctest/project_acc_test.go`

**Interfaces:**
- Consumes: client API (T12–13), harness (T19).
- Produces: resource `leifwind_project`; helpers `parseImportUUIDs(raw string, parts int) ([]uuid.UUID, error)` and `findProjectByName(ctx, c, name) (*client.MetadataProject, error)` (reused by entity/field/data-source tasks).

- [ ] **Step 1: Write the failing tests**

`internal/metadatares/import_test.go` (pure logic):
```go
// SPDX-License-Identifier: MPL-2.0

package metadatares

import "testing"

func TestParseImportUUIDs(t *testing.T) {
	ids, err := parseImportUUIDs("a2ff0efa-64ac-4499-b2a4-99b598ee1c9f", 1)
	if err != nil || len(ids) != 1 {
		t.Fatalf("single: %v %v", ids, err)
	}
	ids, err = parseImportUUIDs("a2ff0efa-64ac-4499-b2a4-99b598ee1c9f/7e57d004-2b97-44e7-8f00-63d2c6b0a50e", 2)
	if err != nil || len(ids) != 2 {
		t.Fatalf("double: %v %v", ids, err)
	}
	if _, err := parseImportUUIDs("only-one", 2); err == nil {
		t.Fatal("want error on wrong part count")
	}
	if _, err := parseImportUUIDs("not-a-uuid/also-not", 2); err == nil {
		t.Fatal("want error on non-uuid parts")
	}
}
```

`internal/acctest/project_acc_test.go`:
```go
// SPDX-License-Identifier: MPL-2.0

package acctest

import (
	"context"
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"gitlab.com/leifwind/stream/terraform-provider-leifwind/client"
)

func TestAccProjectLifecycle(t *testing.T) {
	PreCheck(t)
	org := NewOrg(t)
	cfg := ProviderConfig(org.Token(t, Stack())) + `
resource "leifwind_project" "p" {
  name = "acc_project"
}
`
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: ProtoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: cfg,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("leifwind_project.p", "id"),
					resource.TestCheckResourceAttr("leifwind_project.p", "name", "acc_project"),
				),
			},
			{
				ResourceName:      "leifwind_project.p",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func TestAccProjectStrictCreate(t *testing.T) {
	PreCheck(t)
	org := NewOrg(t)
	tok := org.Token(t, Stack())

	// pre-create the same name out-of-band
	c, err := client.New(Stack().BackendURL, client.WithTokenSource(client.StaticToken(tok)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Metadata.UpsertProject(context.Background(),
		client.MetadataProject{Name: "acc_conflict"}); err != nil {
		t.Fatal(err)
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: ProtoV6ProviderFactories(),
		Steps: []resource.TestStep{{
			Config: ProviderConfig(tok) + `
resource "leifwind_project" "p" {
  name = "acc_conflict"
}
`,
			ExpectError: regexp.MustCompile(`already exists.*terraform import`),
		}},
	})
}

func TestAccProjectDriftRecreates(t *testing.T) {
	PreCheck(t)
	org := NewOrg(t)
	tok := org.Token(t, Stack())
	cfg := ProviderConfig(tok) + `
resource "leifwind_project" "p" {
  name = "acc_drift"
}
`
	c, err := client.New(Stack().BackendURL, client.WithTokenSource(client.StaticToken(tok)))
	if err != nil {
		t.Fatal(err)
	}
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: ProtoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{Config: cfg},
			{
				PreConfig: func() {
					// delete out-of-band: Read must RemoveResource, apply recreates
					page, err := c.Metadata.ListProjects(context.Background(), client.ListOpts{Pattern: "acc_drift"})
					if err != nil || len(page.Objects) != 1 {
						t.Fatalf("drift setup: %v %d", err, len(page.Objects))
					}
					if err := c.Metadata.DeleteProject(context.Background(), *page.Objects[0].ObjectID); err != nil {
						t.Fatal(err)
					}
				},
				Config: cfg,
				Check:  resource.TestCheckResourceAttrSet("leifwind_project.p", "id"),
			},
		},
	})
	_ = fmt.Sprintf
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/metadatares/ -v
TF_ACC=1 TF_ACC_PROVIDER_HOST=registry.opentofu.org TF_ACC_TERRAFORM_PATH=$(command -v tofu) go test ./internal/acctest/ -run TestAccProject -v -timeout 45m
```
Expected: FAIL — `undefined: parseImportUUIDs`; acceptance: `leifwind_project` not a managed resource type.

- [ ] **Step 3: Write minimal implementation**

`internal/metadatares/doc.go`:
```go
// SPDX-License-Identifier: MPL-2.0

// Package metadatares implements the leifwind_* managed resources.
package metadatares
```

`internal/metadatares/import.go`:
```go
// SPDX-License-Identifier: MPL-2.0

package metadatares

import (
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// parseImportUUIDs parses "<uuid>[/<uuid>[/<uuid>]]" import IDs.
func parseImportUUIDs(raw string, parts int) ([]uuid.UUID, error) {
	segs := strings.Split(raw, "/")
	if len(segs) != parts {
		return nil, fmt.Errorf("import ID must have %d '/'-separated UUID segments, got %d", parts, len(segs))
	}
	out := make([]uuid.UUID, 0, parts)
	for _, s := range segs {
		id, err := uuid.Parse(s)
		if err != nil {
			return nil, fmt.Errorf("import ID segment %q is not a UUID: %w", s, err)
		}
		out = append(out, id)
	}
	return out, nil
}
```

`internal/metadatares/exists.go`:
```go
// SPDX-License-Identifier: MPL-2.0

package metadatares

import (
	"context"

	"gitlab.com/leifwind/stream/terraform-provider-leifwind/client"
)

// findProjectByName resolves a project by EXACT name (the server pattern
// is a substring match, so filter client-side). nil = not found.
func findProjectByName(ctx context.Context, c *client.Client, name string) (*client.MetadataProject, error) {
	for p, err := range c.Metadata.IterProjects(ctx, client.ListOpts{Pattern: name}) {
		if err != nil {
			return nil, err
		}
		if p.Name == name {
			return &p, nil
		}
	}
	return nil, nil
}
```

`internal/metadatares/project.go`:
```go
// SPDX-License-Identifier: MPL-2.0

package metadatares

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"gitlab.com/leifwind/stream/terraform-provider-leifwind/client"
)

var (
	_ resource.Resource                = (*projectResource)(nil)
	_ resource.ResourceWithConfigure   = (*projectResource)(nil)
	_ resource.ResourceWithImportState = (*projectResource)(nil)
)

// NewProjectResource registers leifwind_project.
func NewProjectResource() resource.Resource { return &projectResource{} }

type projectResource struct {
	c *client.Client
}

type projectModel struct {
	ID   types.String `tfsdk:"id"`
	Name types.String `tfsdk:"name"`
}

func (r *projectResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_project"
}

func (r *projectResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A leifwind metadata project. The name is immutable (changes force replacement).",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Server-assigned object id (UUID).",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Project name (unique per organization).",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
		},
	}
}

func (r *projectResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider data", fmt.Sprintf("expected *client.Client, got %T", req.ProviderData))
		return
	}
	r.c = c
}

func (r *projectResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan projectModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	name := plan.Name.ValueString()

	// strict create: the backend POST is create-or-adopt; Terraform's
	// contract is that Create fails on pre-existing objects.
	existing, err := findProjectByName(ctx, r.c, name)
	if err != nil {
		resp.Diagnostics.AddError("Checking for existing project failed", err.Error())
		return
	}
	if existing != nil {
		resp.Diagnostics.AddError(
			"Project already exists",
			fmt.Sprintf("project %q already exists (object_id %s) — import it: terraform import leifwind_project.<name> %s",
				name, existing.ObjectID, existing.ObjectID))
		return
	}

	created, err := r.c.Metadata.UpsertProject(ctx, client.MetadataProject{Name: name})
	if err != nil {
		resp.Diagnostics.AddError("Creating project failed", err.Error())
		return
	}
	plan.ID = types.StringValue(created.ObjectID.String())
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *projectResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state projectModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	id, err := uuid.Parse(state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Invalid state id", err.Error())
		return
	}
	p, err := r.c.Metadata.GetProject(ctx, id)
	if errors.Is(err, client.ErrNotFound) {
		resp.State.RemoveResource(ctx) // drift or cross-org: recreate on next apply
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Reading project failed", err.Error())
		return
	}
	state.Name = types.StringValue(p.Name)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *projectResource) Update(_ context.Context, _ resource.UpdateRequest, resp *resource.UpdateResponse) {
	// unreachable: every attribute is Computed or RequiresReplace
	resp.Diagnostics.AddError("Update not supported", "all leifwind_project attributes force replacement")
}

func (r *projectResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state projectModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	id, err := uuid.Parse(state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Invalid state id", err.Error())
		return
	}
	// children cascade server-side (FK ON DELETE CASCADE + schema drop)
	if err := r.c.Metadata.DeleteProject(ctx, id); err != nil && !errors.Is(err, client.ErrNotFound) {
		resp.Diagnostics.AddError("Deleting project failed", err.Error())
	}
}

func (r *projectResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if _, err := parseImportUUIDs(req.ID, 1); err != nil {
		resp.Diagnostics.AddError("Invalid import ID", err.Error()+" (expected <project_id>)")
		return
	}
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
```

Register in `internal/provider/provider.go` — replace the `Resources` method body:
```go
func (p *leifwindProvider) Resources(context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		metadatares.NewProjectResource,
	}
}
```
and add the import `"gitlab.com/leifwind/stream/terraform-provider-leifwind/internal/metadatares"`.

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/metadatares/ -v
TF_ACC=1 TF_ACC_PROVIDER_HOST=registry.opentofu.org TF_ACC_TERRAFORM_PATH=$(command -v tofu) go test ./internal/acctest/ -run TestAccProject -v -timeout 45m
```
Expected: PASS (3 acceptance tests incl. import verify).

- [ ] **Step 5: Commit**

```bash
git add internal/metadatares/ internal/provider/provider.go internal/acctest/project_acc_test.go
git commit -m "feat(provider): leifwind_project resource with strict create and drift handling"
```

---

### Task 21: leifwind_entity resource

**Files:**
- Create: `internal/metadatares/entity.go`
- Modify: `internal/provider/provider.go` (register), `internal/metadatares/exists.go` (add findEntityByName)
- Test: `internal/acctest/entity_acc_test.go`

**Interfaces:**
- Consumes: T20 helpers, client entity methods (T14).
- Produces: resource `leifwind_entity` (attrs `id`, `project_id`, `name`; import `<project_id>/<entity_id>`); `findEntityByName(ctx, c, projectID, name) (*client.MetadataEntity, error)`.

- [ ] **Step 1: Write the failing test**

`internal/acctest/entity_acc_test.go`:
```go
// SPDX-License-Identifier: MPL-2.0

package acctest

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

func entityConfig(token string) string {
	return ProviderConfig(token) + `
resource "leifwind_project" "p" {
  name = "acc_ent_proj"
}

resource "leifwind_entity" "e" {
  project_id = leifwind_project.p.id
  name       = "book"
}
`
}

func TestAccEntityLifecycle(t *testing.T) {
	PreCheck(t)
	org := NewOrg(t)
	tok := org.Token(t, Stack())
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: ProtoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: entityConfig(tok),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("leifwind_entity.e", "id"),
					resource.TestCheckResourceAttr("leifwind_entity.e", "name", "book"),
				),
			},
			{
				ResourceName: "leifwind_entity.e",
				ImportState:  true,
				ImportStateIdFunc: func(s *terraform.State) (string, error) {
					rs := s.RootModule().Resources["leifwind_entity.e"]
					return fmt.Sprintf("%s/%s", rs.Primary.Attributes["project_id"], rs.Primary.ID), nil
				},
				ImportStateVerify: true,
			},
		},
	})
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
TF_ACC=1 TF_ACC_PROVIDER_HOST=registry.opentofu.org TF_ACC_TERRAFORM_PATH=$(command -v tofu) go test ./internal/acctest/ -run TestAccEntity -v -timeout 45m
```
Expected: FAIL — `leifwind_entity` not a managed resource type.

- [ ] **Step 3: Write minimal implementation**

Append to `internal/metadatares/exists.go`:
```go
// findEntityByName resolves an entity by EXACT name within a project.
func findEntityByName(ctx context.Context, c *client.Client, projectID uuid.UUID, name string) (*client.MetadataEntity, error) {
	for e, err := range c.Metadata.IterEntities(ctx, projectID, client.ListOpts{Pattern: name}) {
		if err != nil {
			return nil, err
		}
		if e.Name == name {
			return &e, nil
		}
	}
	return nil, nil
}
```
(add `"github.com/google/uuid"` to its imports)

`internal/metadatares/entity.go`:
```go
// SPDX-License-Identifier: MPL-2.0

package metadatares

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"gitlab.com/leifwind/stream/terraform-provider-leifwind/client"
)

var (
	_ resource.Resource                = (*entityResource)(nil)
	_ resource.ResourceWithConfigure   = (*entityResource)(nil)
	_ resource.ResourceWithImportState = (*entityResource)(nil)
)

// NewEntityResource registers leifwind_entity.
func NewEntityResource() resource.Resource { return &entityResource{} }

type entityResource struct {
	c *client.Client
}

type entityModel struct {
	ID        types.String `tfsdk:"id"`
	ProjectID types.String `tfsdk:"project_id"`
	Name      types.String `tfsdk:"name"`
}

func (r *entityResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_entity"
}

func (r *entityResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A leifwind metadata entity. project_id and name are immutable (changes force replacement).",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Server-assigned object id (UUID).",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"project_id": schema.StringAttribute{
				Required:    true,
				Description: "Owning project id (UUID).",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Entity name (unique per project).",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
		},
	}
}

func (r *entityResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider data", fmt.Sprintf("expected *client.Client, got %T", req.ProviderData))
		return
	}
	r.c = c
}

func (r *entityResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan entityModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	pid, err := uuid.Parse(plan.ProjectID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Invalid project_id", err.Error())
		return
	}
	name := plan.Name.ValueString()

	existing, err := findEntityByName(ctx, r.c, pid, name)
	if err != nil {
		resp.Diagnostics.AddError("Checking for existing entity failed", err.Error())
		return
	}
	if existing != nil {
		resp.Diagnostics.AddError(
			"Entity already exists",
			fmt.Sprintf("entity %q already exists in project %s (object_id %s) — import it: terraform import leifwind_entity.<name> %s/%s",
				name, pid, existing.ObjectID, pid, existing.ObjectID))
		return
	}

	created, err := r.c.Metadata.UpsertEntity(ctx, client.MetadataEntity{ProjectID: pid, Name: name})
	if err != nil {
		resp.Diagnostics.AddError("Creating entity failed", err.Error())
		return
	}
	plan.ID = types.StringValue(created.ObjectID.String())
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *entityResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state entityModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	pid, err := uuid.Parse(state.ProjectID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Invalid state project_id", err.Error())
		return
	}
	eid, err := uuid.Parse(state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Invalid state id", err.Error())
		return
	}
	e, err := r.c.Metadata.GetEntity(ctx, pid, eid)
	if errors.Is(err, client.ErrNotFound) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Reading entity failed", err.Error())
		return
	}
	state.Name = types.StringValue(e.Name)
	state.ProjectID = types.StringValue(e.ProjectID.String())
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *entityResource) Update(_ context.Context, _ resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError("Update not supported", "all leifwind_entity attributes force replacement")
}

func (r *entityResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state entityModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	pid, err := uuid.Parse(state.ProjectID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Invalid state project_id", err.Error())
		return
	}
	eid, err := uuid.Parse(state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Invalid state id", err.Error())
		return
	}
	if err := r.c.Metadata.DeleteEntity(ctx, pid, eid); err != nil && !errors.Is(err, client.ErrNotFound) {
		resp.Diagnostics.AddError("Deleting entity failed", err.Error())
	}
}

func (r *entityResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	ids, err := parseImportUUIDs(req.ID, 2)
	if err != nil {
		resp.Diagnostics.AddError("Invalid import ID", err.Error()+" (expected <project_id>/<entity_id>)")
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("project_id"), ids[0].String())...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), ids[1].String())...)
}
```

Register in `provider.go` `Resources`: add `metadatares.NewEntityResource,` after `NewProjectResource`.

- [ ] **Step 4: Run test to verify it passes**

```bash
TF_ACC=1 TF_ACC_PROVIDER_HOST=registry.opentofu.org TF_ACC_TERRAFORM_PATH=$(command -v tofu) go test ./internal/acctest/ -run TestAccEntity -v -timeout 45m
```
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/metadatares/entity.go internal/metadatares/exists.go internal/provider/provider.go internal/acctest/entity_acc_test.go
git commit -m "feat(provider): leifwind_entity resource with composite import"
```

---

### Task 22: leifwind_field resource

**Files:**
- Create: `internal/metadatares/field.go`
- Modify: `internal/provider/provider.go` (register), `internal/metadatares/exists.go` (add findFieldByName)
- Test: `internal/metadatares/field_test.go` (validator logic), `internal/acctest/field_acc_test.go`

**Interfaces:**
- Consumes: T20–21 helpers, client field methods (T15).
- Produces: resource `leifwind_field`: attrs `id`, `project_id`, `entity_id`, `name`, `data_type`, `connection_type`, `fragment_name` (only mutable one); import `<project_id>/<entity_id>/<field_id>`; `validateFieldCombination(connectionType, fragmentName string, fragmentSet bool) string` pure helper (empty = valid).

- [ ] **Step 1: Write the failing tests**

`internal/metadatares/field_test.go`:
```go
// SPDX-License-Identifier: MPL-2.0

package metadatares

import "testing"

func TestValidateFieldCombination(t *testing.T) {
	if msg := validateFieldCombination("FRAGMENT", "content", true); msg != "" {
		t.Fatalf("valid fragment rejected: %s", msg)
	}
	if msg := validateFieldCombination("KEY", "", false); msg != "" {
		t.Fatalf("valid key rejected: %s", msg)
	}
	if msg := validateFieldCombination("FRAGMENT", "", false); msg == "" {
		t.Fatal("FRAGMENT without fragment_name must be invalid")
	}
	if msg := validateFieldCombination("KEY", "content", true); msg == "" {
		t.Fatal("KEY with fragment_name must be invalid")
	}
}
```

`internal/acctest/field_acc_test.go`:
```go
// SPDX-License-Identifier: MPL-2.0

package acctest

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

func fieldConfig(token, fragmentName string) string {
	return ProviderConfig(token) + fmt.Sprintf(`
resource "leifwind_project" "p" {
  name = "acc_fld_proj"
}

resource "leifwind_entity" "e" {
  project_id = leifwind_project.p.id
  name       = "book"
}

resource "leifwind_field" "title" {
  project_id      = leifwind_project.p.id
  entity_id       = leifwind_entity.e.id
  name            = "title"
  data_type       = "TEXT"
  connection_type = "KEY"
}

resource "leifwind_field" "body" {
  project_id      = leifwind_project.p.id
  entity_id       = leifwind_entity.e.id
  name            = "body"
  data_type       = "TEXT"
  connection_type = "FRAGMENT"
  fragment_name   = %q
}
`, fragmentName)
}

func TestAccFieldLifecycleAndFragmentUpdate(t *testing.T) {
	PreCheck(t)
	org := NewOrg(t)
	tok := org.Token(t, Stack())
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: ProtoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: fieldConfig(tok, "content"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("leifwind_field.title", "id"),
					resource.TestCheckResourceAttr("leifwind_field.body", "fragment_name", "content"),
				),
			},
			{
				// fragment_name is updatable IN PLACE — assert no replacement
				Config: fieldConfig(tok, "content_v2"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("leifwind_field.body", plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.TestCheckResourceAttr("leifwind_field.body", "fragment_name", "content_v2"),
			},
			{
				ResourceName: "leifwind_field.title",
				ImportState:  true,
				ImportStateIdFunc: func(s *terraform.State) (string, error) {
					rs := s.RootModule().Resources["leifwind_field.title"]
					return fmt.Sprintf("%s/%s/%s",
						rs.Primary.Attributes["project_id"],
						rs.Primary.Attributes["entity_id"],
						rs.Primary.ID), nil
				},
				ImportStateVerify: true,
			},
		},
	})
}

func TestAccFieldFragmentValidation(t *testing.T) {
	PreCheck(t)
	org := NewOrg(t)
	tok := org.Token(t, Stack())
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: ProtoV6ProviderFactories(),
		Steps: []resource.TestStep{{
			Config: ProviderConfig(tok) + `
resource "leifwind_project" "p" {
  name = "acc_fld_bad"
}

resource "leifwind_entity" "e" {
  project_id = leifwind_project.p.id
  name       = "book"
}

resource "leifwind_field" "bad" {
  project_id      = leifwind_project.p.id
  entity_id       = leifwind_entity.e.id
  name            = "bad"
  data_type       = "TEXT"
  connection_type = "FRAGMENT"
}
`,
			ExpectError: regexp.MustCompile(`fragment_name is required`),
		}},
	})
}
```
Add import `"github.com/hashicorp/terraform-plugin-testing/plancheck"` to the acc test file.

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/metadatares/ -run TestValidateField -v
TF_ACC=1 TF_ACC_PROVIDER_HOST=registry.opentofu.org TF_ACC_TERRAFORM_PATH=$(command -v tofu) go test ./internal/acctest/ -run TestAccField -v -timeout 45m
```
Expected: FAIL — `undefined: validateFieldCombination`; `leifwind_field` unknown type.

- [ ] **Step 3: Write minimal implementation**

Append to `internal/metadatares/exists.go`:
```go
// findFieldByName resolves a field by EXACT name within an entity.
func findFieldByName(ctx context.Context, c *client.Client, projectID, entityID uuid.UUID, name string) (*client.MetadataField, error) {
	for f, err := range c.Metadata.IterFields(ctx, projectID, entityID, client.ListOpts{Pattern: name}) {
		if err != nil {
			return nil, err
		}
		if f.Name == name {
			return &f, nil
		}
	}
	return nil, nil
}
```

`internal/metadatares/field.go`:
```go
// SPDX-License-Identifier: MPL-2.0

package metadatares

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"

	"gitlab.com/leifwind/stream/terraform-provider-leifwind/client"
)

var (
	_ resource.Resource                     = (*fieldResource)(nil)
	_ resource.ResourceWithConfigure        = (*fieldResource)(nil)
	_ resource.ResourceWithImportState      = (*fieldResource)(nil)
	_ resource.ResourceWithValidateConfig   = (*fieldResource)(nil)
)

// NewFieldResource registers leifwind_field.
func NewFieldResource() resource.Resource { return &fieldResource{} }

type fieldResource struct {
	c *client.Client
}

type fieldModel struct {
	ID             types.String `tfsdk:"id"`
	ProjectID      types.String `tfsdk:"project_id"`
	EntityID       types.String `tfsdk:"entity_id"`
	Name           types.String `tfsdk:"name"`
	DataType       types.String `tfsdk:"data_type"`
	ConnectionType types.String `tfsdk:"connection_type"`
	FragmentName   types.String `tfsdk:"fragment_name"`
}

// validateFieldCombination returns "" when valid, else the error detail.
func validateFieldCombination(connectionType, fragmentName string, fragmentSet bool) string {
	if connectionType == string(client.ConnectionFragment) && (!fragmentSet || fragmentName == "") {
		return "fragment_name is required when connection_type is \"FRAGMENT\""
	}
	if connectionType == string(client.ConnectionKey) && fragmentSet {
		return "fragment_name must not be set when connection_type is \"KEY\""
	}
	return ""
}

func (r *fieldResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_field"
}

func (r *fieldResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A leifwind metadata field. Only fragment_name is updatable in place; every other attribute forces replacement.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Server-assigned object id (UUID).",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"project_id": schema.StringAttribute{
				Required:      true,
				Description:   "Owning project id (UUID).",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"entity_id": schema.StringAttribute{
				Required:      true,
				Description:   "Owning entity id (UUID).",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"name": schema.StringAttribute{
				Required:      true,
				Description:   "Field name (unique per entity).",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"data_type": schema.StringAttribute{
				Required:      true,
				Description:   "Data type (immutable).",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
				Validators: []validator.String{
					stringvalidator.OneOf("TEXT", "INTEGER", "DECIMAL", "BOOLEAN", "DATE", "TIME", "TIMESTAMP", "UUID"),
				},
			},
			"connection_type": schema.StringAttribute{
				Required:      true,
				Description:   "Connection type (immutable). FRAGMENT fields require fragment_name.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
				Validators: []validator.String{
					stringvalidator.OneOf("KEY", "FRAGMENT"),
				},
			},
			"fragment_name": schema.StringAttribute{
				Optional:    true,
				Description: "Fragment the field belongs to (FRAGMENT connection only). Updatable in place.",
			},
		},
	}
}

func (r *fieldResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var cfg fieldModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() || cfg.ConnectionType.IsUnknown() || cfg.FragmentName.IsUnknown() {
		return
	}
	if msg := validateFieldCombination(cfg.ConnectionType.ValueString(),
		cfg.FragmentName.ValueString(), !cfg.FragmentName.IsNull()); msg != "" {
		resp.Diagnostics.AddAttributeError(path.Root("fragment_name"), "Invalid field configuration", msg)
	}
}

func (r *fieldResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider data", fmt.Sprintf("expected *client.Client, got %T", req.ProviderData))
		return
	}
	r.c = c
}

func (m fieldModel) toClientField() (client.MetadataField, error) {
	pid, err := uuid.Parse(m.ProjectID.ValueString())
	if err != nil {
		return client.MetadataField{}, fmt.Errorf("project_id: %w", err)
	}
	eid, err := uuid.Parse(m.EntityID.ValueString())
	if err != nil {
		return client.MetadataField{}, fmt.Errorf("entity_id: %w", err)
	}
	f := client.MetadataField{
		ProjectID: pid, EntityID: eid,
		Name:   m.Name.ValueString(),
		Config: client.FieldConfig{DataType: client.DataType(m.DataType.ValueString())},
		Connection: client.Connection{
			Type:         client.ConnectionType(m.ConnectionType.ValueString()),
			FragmentName: m.FragmentName.ValueString(),
		},
	}
	if !m.ID.IsNull() && !m.ID.IsUnknown() {
		id, err := uuid.Parse(m.ID.ValueString())
		if err != nil {
			return client.MetadataField{}, fmt.Errorf("id: %w", err)
		}
		f.ObjectID = &id
	}
	return f, nil
}

func (r *fieldResource) modelFromClient(f client.MetadataField, m *fieldModel) {
	m.ID = types.StringValue(f.ObjectID.String())
	m.ProjectID = types.StringValue(f.ProjectID.String())
	m.EntityID = types.StringValue(f.EntityID.String())
	m.Name = types.StringValue(f.Name)
	m.DataType = types.StringValue(string(f.Config.DataType))
	m.ConnectionType = types.StringValue(string(f.Connection.Type))
	if f.Connection.Type == client.ConnectionFragment {
		m.FragmentName = types.StringValue(f.Connection.FragmentName)
	} else {
		m.FragmentName = types.StringNull()
	}
}

func (r *fieldResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan fieldModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	f, err := plan.toClientField()
	if err != nil {
		resp.Diagnostics.AddError("Invalid field configuration", err.Error())
		return
	}
	f.ObjectID = nil

	existing, err := findFieldByName(ctx, r.c, f.ProjectID, f.EntityID, f.Name)
	if err != nil {
		resp.Diagnostics.AddError("Checking for existing field failed", err.Error())
		return
	}
	if existing != nil {
		resp.Diagnostics.AddError(
			"Field already exists",
			fmt.Sprintf("field %q already exists (object_id %s) — import it: terraform import leifwind_field.<name> %s/%s/%s",
				f.Name, existing.ObjectID, f.ProjectID, f.EntityID, existing.ObjectID))
		return
	}

	created, err := r.c.Metadata.UpsertField(ctx, f)
	if err != nil {
		resp.Diagnostics.AddError("Creating field failed", err.Error())
		return
	}
	r.modelFromClient(created, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *fieldResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state fieldModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	f, err := state.toClientField()
	if err != nil {
		resp.Diagnostics.AddError("Invalid state", err.Error())
		return
	}
	got, err := r.c.Metadata.GetField(ctx, f.ProjectID, f.EntityID, *f.ObjectID)
	if errors.Is(err, client.ErrNotFound) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Reading field failed", err.Error())
		return
	}
	r.modelFromClient(got, &state)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *fieldResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	// only fragment_name reaches Update (everything else RequiresReplace)
	var plan fieldModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	f, err := plan.toClientField()
	if err != nil {
		resp.Diagnostics.AddError("Invalid field configuration", err.Error())
		return
	}
	updated, err := r.c.Metadata.UpsertField(ctx, f)
	if err != nil {
		resp.Diagnostics.AddError("Updating field failed", err.Error())
		return
	}
	r.modelFromClient(updated, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *fieldResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state fieldModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	f, err := state.toClientField()
	if err != nil {
		resp.Diagnostics.AddError("Invalid state", err.Error())
		return
	}
	if err := r.c.Metadata.DeleteField(ctx, f.ProjectID, f.EntityID, *f.ObjectID); err != nil && !errors.Is(err, client.ErrNotFound) {
		resp.Diagnostics.AddError("Deleting field failed", err.Error())
	}
}

func (r *fieldResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	ids, err := parseImportUUIDs(req.ID, 3)
	if err != nil {
		resp.Diagnostics.AddError("Invalid import ID", err.Error()+" (expected <project_id>/<entity_id>/<field_id>)")
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("project_id"), ids[0].String())...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("entity_id"), ids[1].String())...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), ids[2].String())...)
}
```

Register `metadatares.NewFieldResource,` in `provider.go`; run `go get github.com/hashicorp/terraform-plugin-framework-validators@latest && go mod tidy`.

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/metadatares/ -v
TF_ACC=1 TF_ACC_PROVIDER_HOST=registry.opentofu.org TF_ACC_TERRAFORM_PATH=$(command -v tofu) go test ./internal/acctest/ -run TestAccField -v -timeout 45m
```
Expected: PASS — incl. the plancheck proving fragment_name updates in place (no replace).

- [ ] **Step 5: Commit**

```bash
git add internal/metadatares/field.go internal/metadatares/field_test.go internal/metadatares/exists.go internal/provider/provider.go internal/acctest/field_acc_test.go go.mod go.sum
git commit -m "feat(provider): leifwind_field resource with fragment semantics and in-place fragment_name updates"
```

---

### Task 23: Data sources — project + projects

**Files:**
- Create: `internal/metadatads/doc.go`, `internal/metadatads/project.go`, `internal/metadatads/projects.go`
- Modify: `internal/provider/provider.go` (register)
- Test: `internal/acctest/project_ds_acc_test.go`

**Interfaces:**
- Consumes: client (T12–13), `findProjectByName` — move it (plus `findEntityByName`/`findFieldByName`) from `internal/metadatares/exists.go` into a tiny shared package `internal/lookup` in this task, exported as `lookup.ProjectByName`, `lookup.EntityByName`, `lookup.FieldByName`; update `metadatares` call sites.
- Produces: data sources `leifwind_project` (by `id` XOR `name`), `leifwind_projects` (optional `pattern` → `projects` list of `{id, name}`).

- [ ] **Step 1: Write the failing test**

`internal/acctest/project_ds_acc_test.go`:
```go
// SPDX-License-Identifier: MPL-2.0

package acctest

import (
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestAccProjectDataSourceByIDAndName(t *testing.T) {
	PreCheck(t)
	org := NewOrg(t)
	cfg := ProviderConfig(org.Token(t, Stack())) + `
resource "leifwind_project" "p" {
  name = "ds_project"
}

data "leifwind_project" "by_id" {
  id = leifwind_project.p.id
}

data "leifwind_project" "by_name" {
  name = leifwind_project.p.name
}

data "leifwind_projects" "all" {
  depends_on = [leifwind_project.p]
}

data "leifwind_projects" "filtered" {
  pattern    = "ds_pro"
  depends_on = [leifwind_project.p]
}
`
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: ProtoV6ProviderFactories(),
		Steps: []resource.TestStep{{
			Config: cfg,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("data.leifwind_project.by_id", "name", "ds_project"),
				resource.TestCheckResourceAttrPair("data.leifwind_project.by_name", "id", "leifwind_project.p", "id"),
				resource.TestCheckResourceAttr("data.leifwind_projects.all", "projects.#", "1"),
				resource.TestCheckResourceAttr("data.leifwind_projects.filtered", "projects.#", "1"),
				resource.TestCheckResourceAttr("data.leifwind_projects.filtered", "projects.0.name", "ds_project"),
			),
		}},
	})
}

func TestAccProjectDataSourceValidation(t *testing.T) {
	PreCheck(t)
	org := NewOrg(t)
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: ProtoV6ProviderFactories(),
		Steps: []resource.TestStep{{
			Config: ProviderConfig(org.Token(t, Stack())) + `
data "leifwind_project" "bad" {}
`,
			ExpectError: regexp.MustCompile(`Exactly one of these attributes must be configured`),
		}},
	})
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
TF_ACC=1 TF_ACC_PROVIDER_HOST=registry.opentofu.org TF_ACC_TERRAFORM_PATH=$(command -v tofu) go test ./internal/acctest/ -run TestAccProjectDataSource -v -timeout 45m
```
Expected: FAIL — `leifwind_project` is not an available data source.

- [ ] **Step 3: Write minimal implementation**

Create `internal/lookup/lookup.go` and move the three find helpers there (exported: `ProjectByName(ctx, c, name)`, `EntityByName(ctx, c, projectID, name)`, `FieldByName(ctx, c, projectID, entityID, name)` — bodies identical to Task 20–22's helpers). Delete `internal/metadatares/exists.go` and update `metadatares` to call `lookup.*`.

`internal/metadatads/doc.go`:
```go
// SPDX-License-Identifier: MPL-2.0

// Package metadatads implements the leifwind_* data sources.
package metadatads
```

`internal/metadatads/project.go`:
```go
// SPDX-License-Identifier: MPL-2.0

package metadatads

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/hashicorp/terraform-plugin-framework-validators/datasourcevalidator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"gitlab.com/leifwind/stream/terraform-provider-leifwind/client"
	"gitlab.com/leifwind/stream/terraform-provider-leifwind/internal/lookup"
)

var (
	_ datasource.DataSource                     = (*projectDataSource)(nil)
	_ datasource.DataSourceWithConfigure        = (*projectDataSource)(nil)
	_ datasource.DataSourceWithConfigValidators = (*projectDataSource)(nil)
)

// NewProjectDataSource registers data.leifwind_project.
func NewProjectDataSource() datasource.DataSource { return &projectDataSource{} }

type projectDataSource struct {
	c *client.Client
}

type projectDSModel struct {
	ID   types.String `tfsdk:"id"`
	Name types.String `tfsdk:"name"`
}

func (d *projectDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_project"
}

func (d *projectDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Look up a single project by id or exact name.",
		Attributes: map[string]schema.Attribute{
			"id":   schema.StringAttribute{Optional: true, Computed: true, Description: "Project object id (UUID)."},
			"name": schema.StringAttribute{Optional: true, Computed: true, Description: "Exact project name."},
		},
	}
}

func (d *projectDataSource) ConfigValidators(context.Context) []datasource.ConfigValidator {
	return []datasource.ConfigValidator{
		datasourcevalidator.ExactlyOneOf(path.MatchRoot("id"), path.MatchRoot("name")),
	}
}

func (d *projectDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider data", fmt.Sprintf("expected *client.Client, got %T", req.ProviderData))
		return
	}
	d.c = c
}

func (d *projectDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var cfg projectDSModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var p *client.MetadataProject
	if !cfg.ID.IsNull() {
		id, err := uuid.Parse(cfg.ID.ValueString())
		if err != nil {
			resp.Diagnostics.AddError("Invalid id", err.Error())
			return
		}
		got, err := d.c.Metadata.GetProject(ctx, id)
		if err != nil {
			resp.Diagnostics.AddError("Project not found", err.Error())
			return
		}
		p = &got
	} else {
		got, err := lookup.ProjectByName(ctx, d.c, cfg.Name.ValueString())
		if err != nil {
			resp.Diagnostics.AddError("Project lookup failed", err.Error())
			return
		}
		if got == nil {
			resp.Diagnostics.AddError("Project not found", fmt.Sprintf("no project named %q", cfg.Name.ValueString()))
			return
		}
		p = got
	}
	cfg.ID = types.StringValue(p.ObjectID.String())
	cfg.Name = types.StringValue(p.Name)
	resp.Diagnostics.Append(resp.State.Set(ctx, cfg)...)
}
```

`internal/metadatads/projects.go`:
```go
// SPDX-License-Identifier: MPL-2.0

package metadatads

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"gitlab.com/leifwind/stream/terraform-provider-leifwind/client"
)

var (
	_ datasource.DataSource              = (*projectsDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*projectsDataSource)(nil)
)

// NewProjectsDataSource registers data.leifwind_projects.
func NewProjectsDataSource() datasource.DataSource { return &projectsDataSource{} }

type projectsDataSource struct {
	c *client.Client
}

type projectsDSModel struct {
	Pattern  types.String     `tfsdk:"pattern"`
	Projects []projectDSModel `tfsdk:"projects"`
}

func (d *projectsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_projects"
}

func (d *projectsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "List projects, optionally filtered by a name substring pattern.",
		Attributes: map[string]schema.Attribute{
			"pattern": schema.StringAttribute{Optional: true, Description: "Substring filter on the name (server-side ILIKE)."},
			"projects": schema.ListNestedAttribute{
				Computed: true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id":   schema.StringAttribute{Computed: true},
						"name": schema.StringAttribute{Computed: true},
					},
				},
			},
		},
	}
}

func (d *projectsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider data", fmt.Sprintf("expected *client.Client, got %T", req.ProviderData))
		return
	}
	d.c = c
}

func (d *projectsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var cfg projectsDSModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	cfg.Projects = []projectDSModel{}
	for p, err := range d.c.Metadata.IterProjects(ctx, client.ListOpts{Pattern: cfg.Pattern.ValueString()}) {
		if err != nil {
			resp.Diagnostics.AddError("Listing projects failed", err.Error())
			return
		}
		cfg.Projects = append(cfg.Projects, projectDSModel{
			ID:   types.StringValue(p.ObjectID.String()),
			Name: types.StringValue(p.Name),
		})
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, cfg)...)
}
```

Register both in `provider.go` `DataSources` (import `metadatads`):
```go
	return []func() datasource.DataSource{
		metadatads.NewProjectDataSource,
		metadatads.NewProjectsDataSource,
	}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
go build ./... && TF_ACC=1 TF_ACC_PROVIDER_HOST=registry.opentofu.org TF_ACC_TERRAFORM_PATH=$(command -v tofu) go test ./internal/acctest/ -run TestAccProjectDataSource -v -timeout 45m
```
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/metadatads/ internal/lookup/ internal/metadatares/ internal/provider/provider.go internal/acctest/project_ds_acc_test.go
git commit -m "feat(datasource): leifwind_project and leifwind_projects with shared lookup package"
```

---

### Task 24: Data sources — entity/entities + field/fields

**Files:**
- Create: `internal/metadatads/entity.go`, `internal/metadatads/entities.go`, `internal/metadatads/field.go`, `internal/metadatads/fields.go`
- Modify: `internal/provider/provider.go` (register 4)
- Test: `internal/acctest/entity_field_ds_acc_test.go`

**Interfaces:**
- Consumes: T23 patterns, `lookup` package.
- Produces: `leifwind_entity` (by `project_id` + `id` XOR `name`), `leifwind_entities` (`project_id`, optional `pattern` → `entities []{id,name}`), `leifwind_field` (by `project_id`+`entity_id`+`id` XOR `name` → also `data_type`, `connection_type`, `fragment_name`), `leifwind_fields` (`project_id`+`entity_id`, optional `pattern` → `fields []{id,name,data_type,connection_type,fragment_name}`).

The four files follow EXACTLY the structure of Task 23's `project.go`/`projects.go` with these differences:
- entity DS model: `ID, ProjectID, Name types.String`; required `project_id`; `ExactlyOneOf(id, name)`; by-id → `GetEntity(pid, id)`; by-name → `lookup.EntityByName`.
- entities DS model: `ProjectID, Pattern types.String; Entities []entityItemModel{ID, Name}`; iterates `IterEntities(pid, ...)`.
- field DS model: `ID, ProjectID, EntityID, Name, DataType, ConnectionType, FragmentName types.String` (last three Computed only); by-id → `GetField`, by-name → `lookup.FieldByName`; populate `fragment_name` as null for KEY connections (same mapping as the field resource's `modelFromClient`).
- fields DS: iterates `IterFields(pid, eid, ...)`, nested object carries all five computed attrs.

Write each file completely (copy the Task 23 skeleton, adjust model/paths/lookups as specified). Register `NewEntityDataSource`, `NewEntitiesDataSource`, `NewFieldDataSource`, `NewFieldsDataSource` in `provider.go`.

- [ ] **Step 1: Write the failing test**

`internal/acctest/entity_field_ds_acc_test.go`:
```go
// SPDX-License-Identifier: MPL-2.0

package acctest

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestAccEntityAndFieldDataSources(t *testing.T) {
	PreCheck(t)
	org := NewOrg(t)
	cfg := ProviderConfig(org.Token(t, Stack())) + `
resource "leifwind_project" "p" {
  name = "ds_ef_proj"
}

resource "leifwind_entity" "e" {
  project_id = leifwind_project.p.id
  name       = "book"
}

resource "leifwind_field" "f" {
  project_id      = leifwind_project.p.id
  entity_id       = leifwind_entity.e.id
  name            = "body"
  data_type       = "TEXT"
  connection_type = "FRAGMENT"
  fragment_name   = "content"
}

data "leifwind_entity" "by_name" {
  project_id = leifwind_project.p.id
  name       = leifwind_entity.e.name
}

data "leifwind_entities" "all" {
  project_id = leifwind_project.p.id
  depends_on = [leifwind_entity.e]
}

data "leifwind_field" "by_name" {
  project_id = leifwind_project.p.id
  entity_id  = leifwind_entity.e.id
  name       = leifwind_field.f.name
}

data "leifwind_fields" "all" {
  project_id = leifwind_project.p.id
  entity_id  = leifwind_entity.e.id
  depends_on = [leifwind_field.f]
}
`
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: ProtoV6ProviderFactories(),
		Steps: []resource.TestStep{{
			Config: cfg,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttrPair("data.leifwind_entity.by_name", "id", "leifwind_entity.e", "id"),
				resource.TestCheckResourceAttr("data.leifwind_entities.all", "entities.#", "1"),
				resource.TestCheckResourceAttr("data.leifwind_field.by_name", "fragment_name", "content"),
				resource.TestCheckResourceAttr("data.leifwind_field.by_name", "connection_type", "FRAGMENT"),
				resource.TestCheckResourceAttr("data.leifwind_fields.all", "fields.#", "1"),
				resource.TestCheckResourceAttr("data.leifwind_fields.all", "fields.0.data_type", "TEXT"),
			),
		}},
	})
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
TF_ACC=1 TF_ACC_PROVIDER_HOST=registry.opentofu.org TF_ACC_TERRAFORM_PATH=$(command -v tofu) go test ./internal/acctest/ -run TestAccEntityAndFieldDataSources -v -timeout 45m
```
Expected: FAIL — `leifwind_entity` is not an available data source.

- [ ] **Step 3: Implement the four data sources** (full files per the structural spec above — same Configure/Metadata/Schema/Read shape as Task 23, models and lookups swapped).

- [ ] **Step 4: Run test to verify it passes**

```bash
TF_ACC=1 TF_ACC_PROVIDER_HOST=registry.opentofu.org TF_ACC_TERRAFORM_PATH=$(command -v tofu) go test ./internal/acctest/ -run TestAccEntityAndFieldDataSources -v -timeout 45m
```
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/metadatads/ internal/provider/provider.go internal/acctest/entity_field_ds_acc_test.go
git commit -m "feat(datasource): entity/entities and field/fields data sources"
```

---

### Task 25: Data source — leifwind_entity_fragments

**Files:**
- Create: `internal/metadatads/fragments.go`
- Modify: `internal/provider/provider.go` (register)
- Test: `internal/acctest/fragments_ds_acc_test.go`

**Interfaces:**
- Consumes: `Generic.ListEntityFragments` (T16).
- Produces: `leifwind_entity_fragments{project_id, entity_name → fragments []string}`.

- [ ] **Step 1: Write the failing test**

`internal/acctest/fragments_ds_acc_test.go`:
```go
// SPDX-License-Identifier: MPL-2.0

package acctest

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestAccEntityFragmentsDataSource(t *testing.T) {
	PreCheck(t)
	org := NewOrg(t)
	cfg := ProviderConfig(org.Token(t, Stack())) + `
resource "leifwind_project" "p" {
  name = "ds_frag_proj"
}

resource "leifwind_entity" "e" {
  project_id = leifwind_project.p.id
  name       = "doc"
}

resource "leifwind_field" "a" {
  project_id      = leifwind_project.p.id
  entity_id       = leifwind_entity.e.id
  name            = "body"
  data_type       = "TEXT"
  connection_type = "FRAGMENT"
  fragment_name   = "content"
}

data "leifwind_entity_fragments" "f" {
  project_id  = leifwind_project.p.id
  entity_name = leifwind_entity.e.name
  depends_on  = [leifwind_field.a]
}
`
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: ProtoV6ProviderFactories(),
		Steps: []resource.TestStep{{
			Config: cfg,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("data.leifwind_entity_fragments.f", "fragments.#", "1"),
				resource.TestCheckResourceAttr("data.leifwind_entity_fragments.f", "fragments.0", "content"),
			),
		}},
	})
}
```

- [ ] **Step 2: Run to verify it fails** — same acceptance command, `-run TestAccEntityFragments`. Expected: FAIL (unknown data source).

- [ ] **Step 3: Write minimal implementation**

`internal/metadatads/fragments.go`:
```go
// SPDX-License-Identifier: MPL-2.0

package metadatads

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"gitlab.com/leifwind/stream/terraform-provider-leifwind/client"
)

var (
	_ datasource.DataSource              = (*fragmentsDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*fragmentsDataSource)(nil)
)

// NewEntityFragmentsDataSource registers data.leifwind_entity_fragments.
func NewEntityFragmentsDataSource() datasource.DataSource { return &fragmentsDataSource{} }

type fragmentsDataSource struct {
	c *client.Client
}

type fragmentsDSModel struct {
	ProjectID  types.String `tfsdk:"project_id"`
	EntityName types.String `tfsdk:"entity_name"`
	Fragments  types.List   `tfsdk:"fragments"`
}

func (d *fragmentsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_entity_fragments"
}

func (d *fragmentsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Read-only fragment names of an entity (derived from its FRAGMENT-connection fields).",
		Attributes: map[string]schema.Attribute{
			"project_id":  schema.StringAttribute{Required: true, Description: "Project id (UUID)."},
			"entity_name": schema.StringAttribute{Required: true, Description: "Entity name (or UUID string)."},
			"fragments":   schema.ListAttribute{Computed: true, ElementType: types.StringType},
		},
	}
}

func (d *fragmentsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider data", fmt.Sprintf("expected *client.Client, got %T", req.ProviderData))
		return
	}
	d.c = c
}

func (d *fragmentsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var cfg fragmentsDSModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	pid, err := uuid.Parse(cfg.ProjectID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Invalid project_id", err.Error())
		return
	}
	frags, err := d.c.Generic.ListEntityFragments(ctx, pid, cfg.EntityName.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Listing fragments failed", err.Error())
		return
	}
	list, diags := types.ListValueFrom(ctx, types.StringType, frags)
	resp.Diagnostics.Append(diags...)
	cfg.Fragments = list
	resp.Diagnostics.Append(resp.State.Set(ctx, cfg)...)
}
```
Register `metadatads.NewEntityFragmentsDataSource,` in `provider.go`.

- [ ] **Step 4: Run to verify it passes** — same command. Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/metadatads/fragments.go internal/provider/provider.go internal/acctest/fragments_ds_acc_test.go
git commit -m "feat(datasource): leifwind_entity_fragments"
```

---

### Task 26: Acceptance — M2M and delegated-user auth paths

**Files:**
- Test: `internal/acctest/auth_paths_acc_test.go`

**Interfaces:**
- Consumes: `ProviderConfigM2M` (T19), `Stack().UserToken` (T10), project resource (T20).
- Produces: proof that both spec auth paths drive real applies. The delegated test satisfies the spec's "at least one acceptance run uses a user-scoped (delegated-style) token".

- [ ] **Step 1: Write the failing test**

`internal/acctest/auth_paths_acc_test.go`:
```go
// SPDX-License-Identifier: MPL-2.0

package acctest

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// TestAccM2MClientCredentials drives an apply through the provider's
// issuer/client_id/client_secret/audience block (auto-refreshing M2M).
func TestAccM2MClientCredentials(t *testing.T) {
	PreCheck(t)
	org := NewOrg(t)
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: ProtoV6ProviderFactories(),
		Steps: []resource.TestStep{{
			Config: ProviderConfigM2M(org) + `
resource "leifwind_project" "p" {
  name = "acc_m2m"
}
`,
			Check: resource.TestCheckResourceAttrSet("leifwind_project.p", "id"),
		}},
	})
}

// TestAccDelegatedUserToken drives an apply with a REAL user-scoped token
// (sub = human user, email claim) — the LW-44 runner pattern.
func TestAccDelegatedUserToken(t *testing.T) {
	PreCheck(t)
	org := NewOrg(t)
	userTok := Stack().UserToken(t, org)
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: ProtoV6ProviderFactories(),
		Steps: []resource.TestStep{{
			Config: ProviderConfig(userTok) + `
resource "leifwind_project" "p" {
  name = "acc_delegated"
}
`,
			Check: resource.TestCheckResourceAttrSet("leifwind_project.p", "id"),
		}},
	})
}
```

- [ ] **Step 2: Run to verify state** — both tests should PASS immediately if Tasks 10/19/20 are correct:
```bash
TF_ACC=1 TF_ACC_PROVIDER_HOST=registry.opentofu.org TF_ACC_TERRAFORM_PATH=$(command -v tofu) go test ./internal/acctest/ -run 'TestAccM2M|TestAccDelegated' -v -timeout 45m
```
If TestAccDelegatedUserToken fails at the exchange step, apply the Task 10 escalation (investigate, then conscious spec-fallback decision). This task is test-only; a first-run PASS is acceptable TDD here because the tests pin behavior produced by earlier tasks against regression.

- [ ] **Step 3: Commit**

```bash
git add internal/acctest/auth_paths_acc_test.go
git commit -m "test(acc): M2M client_credentials and delegated user-token auth paths"
```

---

### Task 27: Acceptance — negative auth matrix

**Files:**
- Test: `internal/acctest/auth_negative_acc_test.go`

**Interfaces:**
- Consumes: `ForgedToken` (T10), harness, resources/data sources.
- Produces: the spec's negative matrix: missing credentials, garbage token, forged token, cross-org 404, invalid import ID, expired-token attempt.

- [ ] **Step 1: Write the tests**

`internal/acctest/auth_negative_acc_test.go`:
```go
// SPDX-License-Identifier: MPL-2.0

package acctest

import (
	"context"
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"gitlab.com/leifwind/stream/terraform-provider-leifwind/client"
)

func TestAccMissingCredentials(t *testing.T) {
	PreCheck(t)
	// no token, no M2M block, and empty env (TF_ACC runner must not leak LEIFWIND_*)
	t.Setenv("LEIFWIND_TOKEN", "")
	cfg := fmt.Sprintf(`
provider "leifwind" {
  endpoint = %q
}

resource "leifwind_project" "p" {
  name = "never_created"
}
`, Stack().BackendURL)
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: ProtoV6ProviderFactories(),
		Steps: []resource.TestStep{{
			Config:      cfg,
			ExpectError: regexp.MustCompile(`no credentials`),
		}},
	})
}

func TestAccGarbageToken(t *testing.T) {
	PreCheck(t)
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: ProtoV6ProviderFactories(),
		Steps: []resource.TestStep{{
			Config: ProviderConfig("not-a-jwt") + `
resource "leifwind_project" "p" {
  name = "never_created"
}
`,
			ExpectError: regexp.MustCompile(`(?i)401|unauthenticated`),
		}},
	})
}

func TestAccForgedTokenRejected(t *testing.T) {
	PreCheck(t)
	org := NewOrg(t)
	forged := Stack().ForgedToken(t, org)
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: ProtoV6ProviderFactories(),
		Steps: []resource.TestStep{{
			Config: ProviderConfig(forged) + `
resource "leifwind_project" "p" {
  name = "never_created"
}
`,
			ExpectError: regexp.MustCompile(`(?i)401|unauthenticated`),
		}},
	})
}

// TestAccCrossOrgIsolation: a project of org A is a 404 for org B.
// The org-A project is created via the raw client (NOT a resource.Test,
// whose cleanup would destroy it before org B's step runs).
func TestAccCrossOrgIsolation(t *testing.T) {
	PreCheck(t)
	orgA := NewOrg(t)
	orgB := NewOrg(t)

	ca, err := client.New(Stack().BackendURL,
		client.WithTokenSource(client.StaticToken(orgA.Token(t, Stack()))))
	if err != nil {
		t.Fatal(err)
	}
	p, err := ca.Metadata.UpsertProject(context.Background(),
		client.MetadataProject{Name: "org_a_project"})
	if err != nil {
		t.Fatal(err)
	}

	// org B cannot see it — cross-tenant reads are 404, no existence oracle
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: ProtoV6ProviderFactories(),
		Steps: []resource.TestStep{{
			Config: ProviderConfig(orgB.Token(t, Stack())) + fmt.Sprintf(`
data "leifwind_project" "peek" {
  id = %q
}
`, p.ObjectID),
			ExpectError: regexp.MustCompile(`(?i)not found`),
		}},
	})
}

func TestAccInvalidImportID(t *testing.T) {
	PreCheck(t)
	org := NewOrg(t)
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: ProtoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: ProviderConfig(org.Token(t, Stack())) + `
resource "leifwind_entity" "e" {
  project_id = "00000000-0000-0000-0000-000000000000"
  name       = "never"
}
`,
				ResourceName:  "leifwind_entity.e",
				ImportState:   true,
				ImportStateId: "not-two-uuids",
				ExpectError:   regexp.MustCompile(`import ID must have 2`),
			},
		},
	})
}

// TestAccExpiredToken: plan A per spec — configure a minimal access-token
// lifetime via ZITADEL's admin OIDC settings and wait it out. If the
// instance rejects lifetimes short enough for CI (<2 min), skip with the
// documented spec fallback (expiry is enforced by the backend's JWT
// validation, already exercised backend-side; the provider's 401 surface
// is covered by TestAccForgedTokenRejected/TestAccGarbageToken).
func TestAccExpiredToken(t *testing.T) {
	PreCheck(t)
	t.Skip("expired-token via ZITADEL min-lifetime: feasibility check during implementation — " +
		"attempt PUT /admin/v1/settings/oidc with accessTokenLifetime=60s; if rejected/clamped, " +
		"keep this skip with reference to spec Risks (fallback documented)")
}
```
Add imports `"github.com/hashicorp/terraform-plugin-testing/terraform"` where used. IMPLEMENTATION NOTE: during this task, actually attempt the admin OIDC-settings call in a throwaway spike; if a ≤120 s lifetime is accepted, replace the `t.Skip` with the real wait-and-401 test before committing.

- [ ] **Step 2: Run the suite**

```bash
TF_ACC=1 TF_ACC_PROVIDER_HOST=registry.opentofu.org TF_ACC_TERRAFORM_PATH=$(command -v tofu) go test ./internal/acctest/ -run 'TestAccMissing|TestAccGarbage|TestAccForged|TestAccCrossOrg|TestAccInvalidImport|TestAccExpired' -v -timeout 45m
```
Expected: PASS (with TestAccExpiredToken skipped or implemented per the spike outcome).

- [ ] **Step 3: Commit**

```bash
git add internal/acctest/auth_negative_acc_test.go
git commit -m "test(acc): negative auth matrix (missing/garbage/forged token, cross-org, bad import)"
```

---

### Task 28: goreleaser, registry manifest, release CI

**Files:**
- Create: `.goreleaser.yml`, `terraform-registry-manifest.json`
- Modify: `.gitlab-ci.yml` (release-dry-run + release jobs)

**Interfaces:**
- Consumes: CI skeleton (T3).
- Produces: registry-compliant release artifacts; `release-dry-run` on MRs/main; `release` on protected `v*` tags.

- [ ] **Step 1: Write `terraform-registry-manifest.json`**

```json
{
  "version": 1,
  "metadata": {
    "protocol_versions": ["6.0"]
  }
}
```

- [ ] **Step 2: Write `.goreleaser.yml`** (hcloudgroup-derived; `use: git` changelog since goreleaser runs in GitLab CI; standard scaffold matrix INCLUDING windows/amd64 per spec)

```yaml
version: 2

before:
  hooks:
    - go mod tidy

builds:
  - env:
      - CGO_ENABLED=0
    mod_timestamp: "{{ .CommitTimestamp }}"
    flags:
      - -trimpath
    ldflags:
      - "-s -w -X main.version={{.Version}} -X main.commit={{.Commit}}"
    goos: [freebsd, windows, linux, darwin]
    goarch: [amd64, arm, arm64]
    ignore:
      - goos: darwin
        goarch: arm
      - goos: windows
        goarch: arm
      - goos: freebsd
        goarch: arm64
    binary: "{{ .ProjectName }}_v{{ .Version }}"

archives:
  - formats: [zip]
    name_template: "{{ .ProjectName }}_{{ .Version }}_{{ .Os }}_{{ .Arch }}"

checksum:
  extra_files:
    - glob: "terraform-registry-manifest.json"
      name_template: "{{ .ProjectName }}_{{ .Version }}_manifest.json"
  name_template: "{{ .ProjectName }}_{{ .Version }}_SHA256SUMS"
  algorithm: sha256

signs:
  - artifacts: checksum
    args:
      - "--batch"
      - "--local-user"
      - "{{ .Env.GPG_FINGERPRINT }}"
      - "--output"
      - "${signature}"
      - "--detach-sign"
      - "${artifact}"

release:
  extra_files:
    - glob: "terraform-registry-manifest.json"
      name_template: "{{ .ProjectName }}_{{ .Version }}_manifest.json"

changelog:
  use: git
  groups:
    - title: Features
      regexp: '^.*?feat(\(.+\))??!?:.+$'
      order: 100
    - title: Fixes
      regexp: '^.*?fix(\(.+\))??!?:.+$'
      order: 200
    - title: Other
      order: 999
  filters:
    exclude:
      - '^docs'
      - '^test'
      - '^ci'
      - '^chore'
```

- [ ] **Step 3: Extend `.gitlab-ci.yml`**

```yaml
release-dry-run:
  stage: release
  image: $GO_IMAGE
  rules:
    - if: $CI_PIPELINE_SOURCE == "merge_request_event"
    - if: $CI_COMMIT_BRANCH == "main"
  script:
    - curl -fsSL https://github.com/goreleaser/goreleaser/releases/latest/download/goreleaser_Linux_x86_64.tar.gz | tar -xz -C /usr/local/bin goreleaser
    - goreleaser check
    # NOTE: runs WITH go.work until client/v0.1.0 exists (LW-68 proves GOWORK=off)
    - goreleaser release --snapshot --skip=sign,publish --clean

release:
  stage: release
  image: $GO_IMAGE
  rules:
    - if: $CI_COMMIT_TAG =~ /^v\d+\.\d+\.\d+/
  needs: [lint, "test:client", "test:acceptance"]
  script:
    - apt-get update -qq && apt-get install -y -qq gnupg git
    - curl -fsSL https://github.com/goreleaser/goreleaser/releases/latest/download/goreleaser_Linux_x86_64.tar.gz | tar -xz -C /usr/local/bin goreleaser
    - echo "$GPG_PRIVATE_KEY" | gpg --batch --import
    # the tag must be on GitHub BEFORE goreleaser creates the release
    - git remote add github "https://oauth2:${GITHUB_MIRROR_TOKEN}@github.com/leifwind-io/terraform-provider-leifwind.git" || true
    - git push github "$CI_COMMIT_TAG"
    - GOWORK=off GITHUB_TOKEN="$GITHUB_MIRROR_TOKEN" goreleaser release --clean
```

- [ ] **Step 4: Verify locally**

```bash
goreleaser check
goreleaser release --snapshot --skip=sign,publish --clean
ls dist/ | head
```
Expected: `goreleaser check` valid; snapshot produces zips for all matrix targets + `terraform-provider-leifwind_*_manifest.json` staged; `dist/` is gitignored.

- [ ] **Step 5: Commit**

```bash
git add .goreleaser.yml terraform-registry-manifest.json .gitlab-ci.yml
git commit -m "chore(release): goreleaser with GPG signing, registry manifest, GitLab release jobs"
```

---

### Task 29: Docs + examples (tfplugindocs)

**Files:**
- Create: `examples/provider/provider.tf`, `examples/resources/leifwind_project/{resource.tf,import.sh}`, `examples/resources/leifwind_entity/{resource.tf,import.sh}`, `examples/resources/leifwind_field/{resource.tf,import.sh}`, `examples/data-sources/leifwind_project/data-source.tf`, `examples/data-sources/leifwind_projects/data-source.tf`, `examples/data-sources/leifwind_entity/data-source.tf`, `examples/data-sources/leifwind_entities/data-source.tf`, `examples/data-sources/leifwind_field/data-source.tf`, `examples/data-sources/leifwind_fields/data-source.tf`, `examples/data-sources/leifwind_entity_fragments/data-source.tf`, generated `docs/`

**Interfaces:**
- Consumes: all resources/data sources (T20–25).
- Produces: registry-rendered documentation.

- [ ] **Step 1: Write the examples**

`examples/provider/provider.tf`:
```hcl
terraform {
  required_providers {
    leifwind = {
      source = "leifwind-io/leifwind"
    }
  }
}

# Delegated/static token (runner path):
provider "leifwind" {
  endpoint = "https://api.leifwind.example"
  token    = var.leifwind_token # or LEIFWIND_TOKEN
}

# Alternative — client_credentials (operator/M2M path):
# provider "leifwind" {
#   endpoint      = "https://api.leifwind.example"
#   issuer        = "https://auth.leifwind.example"   # LEIFWIND_OIDC_ISSUER
#   client_id     = "…"                               # LEIFWIND_CLIENT_ID
#   client_secret = var.client_secret                 # LEIFWIND_CLIENT_SECRET
#   audience      = "326102453042806786"              # LEIFWIND_OIDC_AUDIENCE
# }
```

`examples/resources/leifwind_project/resource.tf`:
```hcl
resource "leifwind_project" "library" {
  name = "library"
}
```
`examples/resources/leifwind_project/import.sh`:
```sh
terraform import leifwind_project.library 00000000-0000-0000-0000-000000000000
```

`examples/resources/leifwind_entity/resource.tf`:
```hcl
resource "leifwind_entity" "book" {
  project_id = leifwind_project.library.id
  name       = "book"
}
```
`examples/resources/leifwind_entity/import.sh`:
```sh
terraform import leifwind_entity.book <project_id>/<entity_id>
```

`examples/resources/leifwind_field/resource.tf`:
```hcl
resource "leifwind_field" "title" {
  project_id      = leifwind_project.library.id
  entity_id       = leifwind_entity.book.id
  name            = "title"
  data_type       = "TEXT"
  connection_type = "KEY"
}

resource "leifwind_field" "body" {
  project_id      = leifwind_project.library.id
  entity_id       = leifwind_entity.book.id
  name            = "body"
  data_type       = "TEXT"
  connection_type = "FRAGMENT"
  fragment_name   = "content"
}
```
`examples/resources/leifwind_field/import.sh`:
```sh
terraform import leifwind_field.title <project_id>/<entity_id>/<field_id>
```

Data-source examples (one file each, mirroring the acceptance-test configs — by-id/by-name for singulars, `pattern` for plurals, `project_id`+`entity_name` for fragments).

- [ ] **Step 2: Generate docs**

```bash
make docs
ls docs/resources docs/data-sources
```
Expected: `docs/index.md`, `docs/resources/{project,entity,field}.md`, `docs/data-sources/{project,projects,entity,entities,field,fields,entity_fragments}.md`.

- [ ] **Step 3: Verify rendered content**

```bash
grep -l "fragment_name" docs/resources/field.md && grep -l "LEIFWIND_ENDPOINT" docs/index.md
```
Expected: both paths print (schema descriptions made it into the docs).

- [ ] **Step 4: Commit**

```bash
git add examples/ docs/
git commit -m "docs: tfplugindocs-generated docs with runnable examples"
```

---

### Task 30: READMEs + final verification sweep

**Files:**
- Create: `README.md`, `client/README.md`
- Modify: none

**Interfaces:**
- Consumes: everything.
- Produces: the repo's front doors; the DoD checklist evidence.

- [ ] **Step 1: Write `README.md`** — sections (write real content, not stubs):
1. What: provider for the leifwind metadata API; source address `leifwind-io/leifwind`; AGPL→MPL history not needed — license badge MPL-2.0.
2. Install: `required_providers` block for registry.terraform.io and note for OpenTofu registry.
3. Auth: both paths with env-var table (`LEIFWIND_ENDPOINT`, `LEIFWIND_TOKEN`, `LEIFWIND_OIDC_ISSUER`, `LEIFWIND_CLIENT_ID`, `LEIFWIND_CLIENT_SECRET`, `LEIFWIND_OIDC_AUDIENCE`), delegated-runner note (LW-44 pattern).
4. Development: two-module layout, `go.work`, `make lint/test/testacc/docs`, local prerequisites (docker, `docker login registry.gitlab.com` with read_registry PAT, Go ≥ 1.25, tofu), GitLab-primary/GitHub-mirror explanation.
5. Release: tag flow (`client/vX.Y.Z` then `vX.Y.Z`), CI does the rest; link LW-68 checklist.

- [ ] **Step 2: Write `client/README.md`** with the standalone example:

````markdown
# leifwind Go client

Standalone client for the leifwind metadata API — no Terraform required.

```go
package main

import (
	"context"
	"fmt"
	"log"

	"gitlab.com/leifwind/stream/terraform-provider-leifwind/client"
)

func main() {
	c, err := client.New("https://api.leifwind.example",
		client.WithTokenSource(client.ClientCredentials(
			"https://auth.leifwind.example", "client-id", "client-secret",
			client.WithAudience("326102453042806786"))))
	if err != nil {
		log.Fatal(err)
	}
	for p, err := range c.Metadata.IterProjects(context.Background(), client.ListOpts{}) {
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println(p.Name, p.ObjectID)
	}
}
```

Testing your own code against a real stack: see `leifwindtest` —
`stack, cleanup, _ := leifwindtest.StartMain()` boots ZITADEL + backend +
PostgreSQL in testcontainers and mints per-org tokens.
````

- [ ] **Step 3: Final verification sweep (Definition of Done)**

```bash
make lint
make test
make testacc
goreleaser check && goreleaser release --snapshot --skip=sign,publish --clean
make docs && git diff --exit-code docs/
```
Expected: ALL green; `git diff --exit-code docs/` proves docs are current. Record each command's outcome in the MR description.

- [ ] **Step 4: Commit, push, open MR**

```bash
git add README.md client/README.md
git commit -m "docs: root and client READMEs with standalone usage example"
git push origin feature/lw-43-terraform-provider-leifwind-public-provider-for-the-metadata
command glab mr create --fill --draft
```

- [ ] **Step 5: Close the loop outside the repo**
- Move LW-43 to In Progress at execution start (first task), and when the MR is up: comment on LW-43 summarizing implementation state + link the MR.
- Update the Notion refinement page "Implementation plan" section with this plan's path and commit hash.
- Remaining one-time steps (registry onboarding, semver pin flip, GOWORK=off release proof) live in LW-68 — do NOT do them here.

---

## Plan-wide notes

- **Ordering constraint:** Tasks 1–19 are sequential prerequisites for 20+; Tasks 20–22 sequential (shared helpers); 23–25 after 22; 26–27 after 25; 28–30 last.
- **go.mod placeholder pin (Task 19):** `client v0.0.0-00010101000000-…` + committed go.work is the dev-time stance; the spec's `GOWORK=off` release proof happens in LW-68 once `client/v0.1.0` is tagged. The release-dry-run job documents this with an inline NOTE.
- **Shared-stack discipline:** client tests share one stack via `TestMain` (`client_test.go`); acceptance tests share one via `internal/acctest/main_test.go`. Tests get isolation from per-test orgs, NOT per-test stacks. Toxiproxy tests must not run `t.Parallel()` (shared proxy state).
- **If the backend `edge` image breaks the suite** (backend main moved), that is the accepted risk of the temporary pin — check backend git log, and if needed pin `sha-<short>` temporarily; the semver flip is LW-68.
