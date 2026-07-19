# Dossier: hcloudgroup template patterns for terraform-provider-leifwind

Source (read-only template): `terraform-provider-hcloudgroup` — sibling checkout in the `leifwind-stream` workspace, not published; module path `github.com/chickeaterbanana/terraform-provider-hcloudgroup`
Spec: `docs/superpowers/specs/2026-07-10-lw43-terraform-provider-design.md` (LW-43, approved 2026-07-10).

Every "verbatim" block below is the exact template text. "ADAPT" notes state the leifwind delta.

---

## 1. `.goreleaser.yml` — full template text

```yaml
# Based on the hashicorp terraform-provider-scaffolding-framework template.
# https://github.com/hashicorp/terraform-provider-scaffolding-framework/blob/main/.goreleaser.yml
#
# This is what the Terraform Registry expects:
#   - One zip per (os, arch) pair, plus a SHA256SUMS file, plus a GPG
#     signature over SHA256SUMS.
#   - Manifest declaring protocol_versions = ["6.0"].
version: 2

before:
  hooks:
    - go mod tidy

builds:
  - env:
      - CGO_ENABLED=0
    mod_timestamp: '{{ .CommitTimestamp }}'
    flags:
      - -trimpath
    ldflags:
      - '-s -w -X main.version={{.Version}} -X main.commit={{.Commit}}'
    # Linux/macOS/FreeBSD only — exec.go uses syscall.Setpgid + syscall.Kill
    # for child-process management on action timeout, both Unix-specific.
    # Windows support is a v2 concern (README §2.1).
    goos:
      - linux
      - darwin
      - freebsd
    goarch:
      - amd64
      - arm
      - arm64
    # CI policy: every released binary must be exercised by smoke.
    # Drops:
    #   - 32-bit ARM (linux/freebsd/darwin): no GH-hosted runner, no
    #     cross-platform-actions support, no Hetzner offering.
    #   - darwin/amd64: macos-13 (Intel) runners on GH-hosted have
    #     unbounded queue waits; smoking it is impractical.
    ignore:
      - goos: darwin
        goarch: arm
      - goos: darwin
        goarch: amd64
      - goos: linux
        goarch: arm
      - goos: freebsd
        goarch: arm
    binary: '{{ .ProjectName }}_v{{ .Version }}'

archives:
  - formats: [zip]
    name_template: '{{ .ProjectName }}_{{ .Version }}_{{ .Os }}_{{ .Arch }}'

checksum:
  extra_files:
    - glob: 'terraform-registry-manifest.json'
      name_template: '{{ .ProjectName }}_{{ .Version }}_manifest.json'
  name_template: '{{ .ProjectName }}_{{ .Version }}_SHA256SUMS'
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

sboms:
  - artifacts: archive

release:
  extra_files:
    - glob: 'terraform-registry-manifest.json'
      name_template: '{{ .ProjectName }}_{{ .Version }}_manifest.json'

changelog:
  use: github
  sort: asc
  groups:
    - title: Features
      regexp: '^.*?feat(\([^)]+\))?!?:.+$'
      order: 0
    - title: Fixes
      regexp: '^.*?fix(\([^)]+\))?!?:.+$'
      order: 1
    - title: Dependencies
      regexp: '^.*?deps(\([^)]+\))?!?:.+$'
      order: 2
    - title: Other
      order: 999
  filters:
    exclude:
      - '^docs:'
      - '^test:'
      - '^ci:'
      - '^chore:'
```

**ADAPT for leifwind:**
- Project name: goreleaser derives `ProjectName` from the repo directory / `project_name`; set explicitly `project_name: terraform-provider-leifwind` (releases go to the GitHub mirror `github.com/leifwind-io/terraform-provider-leifwind`, registry address `leifwind-io/leifwind`).
- **GitLab CI usage:** release runs from GitLab CI, but artifacts publish to **GitHub Releases** on the mirror (both registries ingest from GitHub). Keep goreleaser's default GitHub publisher; the CI job exports `GITHUB_TOKEN=$GITHUB_MIRROR_TOKEN` and pushes the tag to GitHub before running goreleaser (spec CI/CD step 4). Do NOT switch goreleaser to its `gitlab:` release block.
- **Changelog: `use: git` instead of `use: github`** — the commits live in GitLab; goreleaser cannot read them via the GitHub API. Keep the same conventional-commit groups/filters.
- **Keep windows/amd64:** the Unix-syscall exclusion rationale (exec.go) does not apply to a pure-HTTP provider. Use the standard scaffold matrix: `goos: [linux, darwin, freebsd, windows]`, `goarch: [amd64, arm, arm64, '386']` with the scaffold's standard ignores (`darwin/386`, `darwin/arm`, `windows/arm`, `windows/arm64` optional) — spec: "standard scaffold set including windows/amd64; hcloudgroup's trimmed matrix existed only for its smoke-coverage policy". Delete the smoke-policy comment block.
- Add module awareness: release/dry-run jobs run with `GOWORK=off` (env in CI, not in this file) so the pinned `client/vX.Y.Z` is proven to build standalone.
- Keep: `version: 2`, CGO_ENABLED=0, trimpath, ldflags version/commit stamping, zip archives, SHA256SUMS + GPG detach-sign via `GPG_FINGERPRINT`, sboms, manifest copy in both `checksum.extra_files` and `release.extra_files`.

---

## 2. `.golangci.yml` — full template text

```yaml
version: "2"

run:
  timeout: 5m
  tests: true

linters:
  default: none
  enable:
    - errcheck
    - govet
    - staticcheck
    - unused
    # Security: catches command injection, weak crypto, unsafe path ops.
    # The provider runs operator-supplied shell commands; gosec is the
    # most important addition for a published provider.
    - gosec
    # Net: catches leaked HTTP response bodies. We don't open raw HTTP
    # clients today, but the linter is cheap and protects against
    # future regressions in any package that calls the hcloud SDK
    # directly.
    - bodyclose
    # Context: catches functions that lose the cancellation context.
    # The reconciler's cancel propagation matters across every retry
    # loop and goroutine.
    - contextcheck
    # License: enforces the MPL-2.0 SPDX header on every Go source file.
    - goheader
  exclusions:
    rules:
      # Acceptance fixtures (internal/acctest) legitimately ignore some
      # cleanup errors during tear-down. Don't exempt unit tests, where
      # `_ = touch(flag)`-style swallowing is a real bug.
      - path: internal/acctest/.*_test\.go
        linters:
          - errcheck
  settings:
    goheader:
      template: |-
        Copyright (c) {{ YEAR }} The terraform-provider-hcloudgroup Authors
        SPDX-License-Identifier: MPL-2.0

formatters:
  enable:
    - gofmt
    - goimports
```

**ADAPT for leifwind (spec §Toolchain — golangci-lint v2, strict from day one, over both modules):**
- goheader template: keep **MPL-2.0** (owner license revision 2026-07-10); change authors line to `Copyright (c) {{ YEAR }} The terraform-provider-leifwind Authors`.
- **ADD linters:** `revive`, `exhaustive`, `errorlint`, `nilerr` to the `enable` list (spec names all four explicitly). `exhaustive` matters for the `DataType`/`ConnectionType` enum switches in the client's union translation.
- **ADD depguard** rule banning `net/http` in provider packages (dogfooding: provider talks to the backend exclusively through `/client`):

```yaml
    - depguard
  settings:
    depguard:
      rules:
        no-net-http-in-provider:
          list-mode: lax
          files:
            - '**/internal/**'
          deny:
            - pkg: net/http
              desc: provider packages must use the leifwind client module, never net/http directly
```

  (`internal/` = provider packages; the `/client` module keeps `net/http`. Run golangci-lint once per module — the `/client` module's config omits this rule or the rule's `files` glob simply never matches there.)
- Keep: `default: none` allowlist style, `run.timeout: 5m`, `tests: true`, errcheck exclusion for `internal/acctest/.*_test\.go` teardown code, gofmt/goimports formatters.
- Both modules linted (root + `client/`); CI lint stage runs it over each.

---

## 3. `main.go` — verbatim

```go
// Copyright (c) 2026 The terraform-provider-hcloudgroup Authors
// SPDX-License-Identifier: MPL-2.0

// terraform-provider-hcloudgroup is the provider binary entry point. It
// hands off to the framework's providerserver, which speaks the
// terraform-plugin protocol on stdin/stdout. The Address must match the
// canonical registry path so terraform/tofu locate the provider via
// `required_providers`.
package main

import (
	"context"
	"flag"
	"log"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"

	"github.com/chickeaterbanana/terraform-provider-hcloudgroup/internal/provider"
)

// version is overwritten by goreleaser via -ldflags at release time.
var version = "dev"

func main() {
	var debug bool
	flag.BoolVar(&debug, "debug", false, "set to true to run the provider with support for debuggers")
	flag.Parse()

	opts := providerserver.ServeOpts{
		Address: "registry.terraform.io/chickeaterbanana/hcloudgroup",
		Debug:   debug,
	}
	if err := providerserver.Serve(context.Background(), provider.New(version), opts); err != nil {
		log.Fatal(err)
	}
}
```

**ADAPT:** import path `gitlab.com/leifwind/stream/terraform-provider-leifwind/internal/provider`; Address `registry.terraform.io/leifwind-io/leifwind` (spec's main.go note). Note the ldflags also stamp `main.commit` — either add `var commit = ""` or trim the ldflag.

---

## 4. `internal/provider/provider.go` — Configure pattern, verbatim

```go
// Copyright (c) 2026 The terraform-provider-hcloudgroup Authors
// SPDX-License-Identifier: MPL-2.0

// Package provider declares the hcloudgroup terraform/opentofu provider:
// schema, configuration, and resource registration.
package provider

import (
	"context"
	"os"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"

	"github.com/chickeaterbanana/terraform-provider-hcloudgroup/internal/hcloudx"
	"github.com/chickeaterbanana/terraform-provider-hcloudgroup/internal/servergroup"
)

// HCloudGroupProvider is the framework provider type.
type HCloudGroupProvider struct {
	Version string
}

// providerModel mirrors the provider's HCL schema. Token is sensitive;
// endpoint is optional and defaults to the public hcloud API.
type providerModel struct {
	HCloudToken    types.String `tfsdk:"hcloud_token"`
	HCloudEndpoint types.String `tfsdk:"hcloud_endpoint"`
}

// New returns a provider factory bound to a release version string. Used
// from main.go.
func New(version string) func() provider.Provider {
	return func() provider.Provider { return &HCloudGroupProvider{Version: version} }
}

// Metadata sets the provider type name (governs the resource type prefix
// "hcloudgroup_*").
func (p *HCloudGroupProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "hcloudgroup"
	resp.Version = p.Version
}

// Schema declares provider-level configuration.
func (p *HCloudGroupProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages groups of Hetzner Cloud servers as a single rolling-replace unit.",
		Attributes: map[string]schema.Attribute{
			"hcloud_token": schema.StringAttribute{
				Optional:    true,
				Sensitive:   true,
				Description: "Hetzner Cloud API token. Defaults to the HCLOUD_TOKEN env var.",
			},
			"hcloud_endpoint": schema.StringAttribute{
				Optional:    true,
				Description: "Hetzner Cloud API endpoint. Defaults to https://api.hetzner.cloud/v1.",
			},
		},
	}
}

// Configure constructs the hcloud client. It pulls the token from HCL
// first, falling back to the HCLOUD_TOKEN env var. Missing tokens are a
// configuration error.
func (p *HCloudGroupProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var cfg providerModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}

	token := cfg.HCloudToken.ValueString()
	if token == "" {
		token = os.Getenv("HCLOUD_TOKEN")
	}
	if token == "" {
		resp.Diagnostics.AddAttributeError(
			path.Root("hcloud_token"),
			"Missing hcloud_token",
			"Set the hcloud_token provider attribute or the HCLOUD_TOKEN environment variable.",
		)
		return
	}

	opts := []hcloud.ClientOption{hcloud.WithToken(token)}
	if ep := cfg.HCloudEndpoint.ValueString(); ep != "" {
		opts = append(opts, hcloud.WithEndpoint(ep))
	}

	client := hcloudx.NewReal(hcloud.NewClient(opts...))

	// Resources receive the hcloudx.Client interface; tests substitute a
	// fake by replacing the resource's Client field directly.
	var iface hcloudx.Client = client
	resp.ResourceData = iface
	resp.DataSourceData = iface
}

// Resources lists every concrete resource type the provider exposes.
func (p *HCloudGroupProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		servergroup.New,
	}
}

// DataSources is empty in v1; the read-only data source is a follow-up.
func (p *HCloudGroupProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return nil
}
```

**Pattern to carry over:** HCL-first-then-env fallback (`cfg.X.ValueString()` → `os.Getenv(...)`), `AddAttributeError(path.Root(...), title, "Set the X attribute or the Y environment variable.")` naming the missing env var, and passing one client value to both `resp.ResourceData` and `resp.DataSourceData`.

**ADAPT for leifwind:** TypeName `leifwind`; attributes `endpoint`, `token` (sensitive), `issuer`, `client_id`, `client_secret` (sensitive), `audience` — all Optional; env fallbacks `LEIFWIND_ENDPOINT`, `LEIFWIND_TOKEN`, `LEIFWIND_OIDC_ISSUER`, `LEIFWIND_CLIENT_ID`, `LEIFWIND_CLIENT_SECRET`, `LEIFWIND_OIDC_AUDIENCE`. Configure validates: endpoint must resolve; `token` XOR complete (`issuer`,`client_id`,`client_secret`) trio; build `*client.Client` via `client.New(endpoint, client.WithTokenSource(...), client.WithUserAgent("terraform-provider-leifwind/"+p.Version))` and wire it to ResourceData/DataSourceData. DataSources is non-empty (7 data sources per spec).

---

## 5. Resource patterns (`internal/servergroup/`)

### 5a. resource.go — type, compile-time interface checks, Metadata, Configure, ValidateConfig (verbatim)

```go
// Compile-time interface checks ensure the resource implements the
// framework optional interfaces we rely on.
var (
	_ resource.Resource                   = (*ServerGroupResource)(nil)
	_ resource.ResourceWithConfigure      = (*ServerGroupResource)(nil)
	_ resource.ResourceWithImportState    = (*ServerGroupResource)(nil)
	_ resource.ResourceWithValidateConfig = (*ServerGroupResource)(nil)
)

// ServerGroupResource is the framework resource type. The hcloud client
// is injected at Configure time from the provider.
type ServerGroupResource struct {
	Client hcloudx.Client
}

// New constructs the resource. Used in provider.Resources.
func New() resource.Resource { return &ServerGroupResource{} }

// Metadata sets the type name visible in HCL: hcloudgroup_server_group.
func (r *ServerGroupResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_server_group"
}

// Configure receives the shared hcloud client from the provider. It is
// called before any CRUD method; nil ProviderData means the framework is
// validating only and we should leave Client unset.
func (r *ServerGroupResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(hcloudx.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider data type",
			fmt.Sprintf("expected hcloudx.Client, got %T", req.ProviderData))
		return
	}
	r.Client = c
}
```

ValidateConfig pattern (plan-time config validation, verbatim) — leifwind reuses this shape for the `fragment_name` iff `connection_type == "FRAGMENT"` rule on `leifwind_field`:

```go
func (r *ServerGroupResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var m resourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if m.UserDataTemplate.IsNull() || m.UserDataTemplate.IsUnknown() {
		return
	}
	if err := tmpl.Parse(m.UserDataTemplate.ValueString()); err != nil {
		resp.Diagnostics.AddAttributeError(
			path.Root("user_data_template"),
			"Invalid user_data_template",
			err.Error(),
		)
	}
}
```

### 5b. schema.go — UseStateForUnknown on computed id, RequiresReplace, enum validator (verbatim snippets)

```go
"id": schema.StringAttribute{
	Computed: true,
	PlanModifiers: []planmodifier.String{
		stringplanmodifier.UseStateForUnknown(),
	},
},
"name": schema.StringAttribute{
	Required:    true,
	Description: "Group identifier. Embedded in server names and used as a label selector value.",
	Validators:  groupNameValidators(),
	PlanModifiers: []planmodifier.String{
		stringplanmodifier.RequiresReplace(),
	},
},
```

Enum-with-default pattern (leifwind's `data_type`/`connection_type` use `stringvalidator.OneOf` the same way, without the Default):

```go
"replace_method": schema.StringAttribute{
	Optional: true,
	Computed: true,
	Default:  stringdefault.StaticString(reconciler.ReplaceMethodCreateBeforeDestroy),
	...
	Validators: []validator.String{
		stringvalidator.OneOf(
			reconciler.ReplaceMethodCreateBeforeDestroy,
			reconciler.ReplaceMethodDestroyBeforeCreate,
		),
	},
},
```

Imports used by schema.go (framework packages to mirror):
`terraform-plugin-framework-validators/{int64validator,stringvalidator}`, `resource/schema`, `resource/schema/planmodifier`, `resource/schema/stringdefault`, `resource/schema/stringplanmodifier`, `schema/validator`, `types`.

**ADAPT:** leifwind maps server-side immutability to `RequiresReplace()` per the spec table — project: `name`; entity: `project_id`, `name`; field: everything except `fragment_name`. No `timeouts` blocks in v0 (drop the `timeouts.Block` import/usage). Flat string enums: `data_type` OneOf(`TEXT INTEGER DECIMAL BOOLEAN DATE TIME TIMESTAMP UUID`), `connection_type` OneOf(`KEY`, `FRAGMENT`).

### 5c. crud.go — CRUD shape (representative verbatim snippets)

Create prologue (plan → model → diagnostics guard):

```go
func (r *ServerGroupResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan resourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	...
	plan.ID = types.StringValue(group.Name)
	...
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
```

Read drift handling — `RemoveResource` when gone (leifwind: `errors.Is(err, client.ErrNotFound)` → `resp.State.RemoveResource(ctx); return`):

```go
	if shouldRemoveResource(observed, priorState) {
		resp.State.RemoveResource(ctx)
		return
	}
```

Post-import null-normalization comment pattern (Read must default null computed/optional attrs so `ImportStateVerify` doesn't diff):

```go
	if prior.ReplaceMethod.IsNull() || prior.ReplaceMethod.IsUnknown() {
		prior.ReplaceMethod = types.StringValue(reconciler.ReplaceMethodCreateBeforeDestroy)
	}
```

ImportState — template seeds attributes via `resp.State.SetAttribute` (multi-attribute import):

```go
func (r *ServerGroupResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if r.Client == nil {
		resp.Diagnostics.AddError("import: provider not configured",
			"the provider must be configured before importing; this typically means HCLOUD_TOKEN is missing")
		return
	}
	...
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, importNamePath(), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, importIDPath(), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("replicas"), int64(replicas))...)
}
```

**ADAPT:** `leifwind_project` uses the trivial framework passthrough
`resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)`;
`leifwind_entity` / `leifwind_field` parse composite IDs (`<project_id>/<entity_id>`, `<project_id>/<entity_id>/<field_id>`) and seed each attr with `SetAttribute` exactly as above. Import-ID parsing is a pure in-process unit test target (spec test layer 4).
Also ADAPT: **strict Create** — before POST, list by exact name and error "already exists — use terraform import" (spec deviation from raw upsert). Error mapping helper mirrors `appendApplyError` but switches on `client.APIError` sentinels.

### 5d. model.go — tfsdk model pattern (verbatim excerpt)

```go
// resourceModel mirrors the resource's HCL schema in Go. Field tags map
// to attribute names; nested blocks are pointers so absence is
// distinguishable from explicit-nil.
type resourceModel struct {
	ID   types.String `tfsdk:"id"`
	Name types.String `tfsdk:"name"`
	// Count maps to HCL "replicas" — Terraform reserves "count".
	Count              types.Int64  `tfsdk:"replicas"`
	Image              types.String `tfsdk:"image"`
	...
	Timeouts timeouts.Value `tfsdk:"timeouts"`
}
```

**ADAPT:** leifwind models are flat — e.g. field: `ID, ProjectID, EntityID, Name, DataType, ConnectionType, FragmentName` all `types.String`. No timeouts field (no timeouts blocks in v0). Conversion helpers (`modelToGroup` / `stateToSlotsValue` in the template) become model↔`client.MetadataField` translators, folding the flat `connection_type`/`fragment_name` into the client's `Connection` union.

### 5e. validators.go — custom validator pattern (verbatim excerpt)

```go
type groupNameValidator struct{}

func (groupNameValidator) Description(_ context.Context) string {
	return "must be a valid RFC 1123 label and short enough that group-slot-generation fits in 63 chars"
}
func (v groupNameValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (groupNameValidator) ValidateString(ctx context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	v := req.ConfigValue.ValueString()
	if !groupNameRE.MatchString(v) {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid group name",
			"name must match RFC 1123 label rules: lowercase a-z, 0-9, hyphen; must start and end with alphanumeric")
		return
	}
	...
}
```

Key conventions: struct-based validators satisfying `validator.String`/`validator.Map`; always early-return on `IsNull() || IsUnknown()`; `MarkdownDescription` delegates to `Description`; `AddAttributeError(req.Path, title, detail)`. leifwind's enum checks use the stock `stringvalidator.OneOf` (5b); custom validators only if name/pattern rules emerge. The fragment/connection cross-attribute rule lives in `ValidateConfig` (5a), not per-attribute validators.

---

## 6. acctest patterns (`internal/acctest/`)

### 6a. provider_factories.go — verbatim

```go
// ProviderName is the short name plugin-testing uses to register the
// in-process provider. The framework builds the full address as
// host/namespace/name; both terraform and opentofu resolve a bare
// "hcloudgroup" reference in HCL to "<host>/<namespace>/hcloudgroup".
//
// For opentofu compatibility, set TF_ACC_PROVIDER_HOST=registry.opentofu.org
// before running the suite — opentofu uses that as its default registry
// host, while terraform uses registry.terraform.io.
const ProviderName = "hcloudgroup"

// ProviderFactories returns the protov6 factory map every acceptance test
// passes to resource.Test. The provider is constructed in-process by the
// framework's providerserver, so terraform/tofu communicates over the
// plugin protocol just as it would with a published binary.
func ProviderFactories() map[string]func() (tfprotov6.ProviderServer, error) {
	return map[string]func() (tfprotov6.ProviderServer, error){
		ProviderName: providerserver.NewProtocol6WithError(provider.New("acctest")()),
	}
}
```

(imports: `terraform-plugin-framework/providerserver`, `terraform-plugin-go/tfprotov6`, own `internal/provider`.)
**ADAPT:** `ProviderName = "leifwind"`. The `TF_ACC_PROVIDER_HOST=registry.opentofu.org` note carries straight into leifwind's CI (spec sets exactly this in the acceptance job).

### 6b. gate.go — PreCheck pattern, verbatim

```go
const EnvHcloudToken = "HCLOUD_TOKEN"

// EnvAcceptance is the upstream framework's acceptance-test gate.
const EnvAcceptance = "TF_ACC"

var (
	gateOnce sync.Once
	gateMsg  string
	gateOK   bool
)

// PreCheck is the standard PreCheck function passed to resource.Test. It
// skips the test unless TF_ACC=1 and HCLOUD_TOKEN is set, with one
// stable message used across the suite.
func PreCheck(t *testing.T) {
	t.Helper()
	gateOnce.Do(func() {
		acc := os.Getenv(EnvAcceptance)
		token := os.Getenv(EnvHcloudToken)
		switch {
		case acc == "":
			gateMsg = fmt.Sprintf("acceptance tests skipped: set %s=1 to run", EnvAcceptance)
		case token == "":
			gateMsg = fmt.Sprintf("acceptance tests skipped: %s is empty (real Hetzner credentials required)", EnvHcloudToken)
		default:
			gateOK = true
		}
	})
	if !gateOK {
		t.Skip(gateMsg)
	}
}
```

**ADAPT:** leifwind's gate is only `TF_ACC=1` + docker availability (fixture self-bootstraps; no cloud token needed). Keep the `sync.Once` + stable-skip-message pattern.

### 6c. TestMain / fixture-lifecycle pattern (verbatim)

`main_test.go` (package-level suite teardown hook):

```go
func TestMain(m *testing.M) {
	code := m.Run()

	// Best-effort suite-level teardown. Tests that called acctest.Get(t)
	// are responsible for fixture lifecycle via t.Cleanup; this is a
	// final safety net.
	if os.Getenv(acctest.EnvAcceptance) != "" && os.Getenv(acctest.EnvHcloudToken) != "" {
		hc := hcloud.NewClient(hcloud.WithToken(os.Getenv(acctest.EnvHcloudToken)))
		_ = acctest.SweepLeftoverResources(context.Background(), hc)
		acctest.Teardown()
	}

	os.Exit(code)
}
```

`fixtures.go` reusable-TestMain + lazy shared-suite pattern:

```go
// Shared fixtures: created lazily on first use, torn down in TestMain.
var (
	fixOnce sync.Once
	fixErr  error
	shared  *Suite
)

// Get returns the shared suite, creating it on first call. Callers must
// have passed PreCheck before calling Get.
func Get(t *testing.T) *Suite {
	t.Helper()
	fixOnce.Do(func() {
		shared, fixErr = bootstrap(t)
	})
	if fixErr != nil {
		t.Fatalf("acctest fixture bootstrap failed: %v", fixErr)
	}
	return shared
}

// AccTestMain is the standard TestMain body for any test package that
// drives acceptance tests. ... Use it like:
//
//	func TestMain(m *testing.M) { acctest.AccTestMain(m) }
//
// If TF_ACC is unset (hermetic mode), no teardown runs.
func AccTestMain(m *testing.M) {
	code := m.Run()
	if os.Getenv(EnvAcceptance) != "" && os.Getenv(EnvHcloudToken) != "" {
		Teardown()
		hc := hcloud.NewClient(hcloud.WithToken(os.Getenv(EnvHcloudToken)))
		_ = SweepLeftoverResources(context.Background(), hc)
	}
	os.Exit(code)
}

// Teardown destroys the suite. Called from TestMain after all tests
// complete. Idempotent.
func Teardown() {
	if shared == nil {
		return
	}
	shared.destroy(context.Background())
	shared = nil
}
```

Also reusable: `RandName(t, prefix)` builds `tfacc-<prefix>-<lowercased-test-name>` unique per test.

**ADAPT:** leifwind's `internal/acctest` imports `client/leifwindtest` and the lazy `sync.Once` suite boots the containerized stack (ZITADEL + 2×Postgres + backend) instead of Hetzner fixtures; per-test fresh org/machine-user replaces `RandName`-style namespacing for tenancy, though `RandName` stays useful for object names. No sweeper needed — containers die with the test binary; keep `AccTestMain` shape for stack teardown. Note the template's contradiction to avoid copying blindly: hcloudgroup runs `-p 1 -parallel=1`; leifwind's spec wants `t.Parallel()` with per-test org isolation.

---

## 7. Makefile — template targets

```makefile
.PHONY: test testrace testacc lint sweep tidy docs smoke

# Hermetic unit + reconciler tests. Fast — gates every PR.
test:
	go test ./...

testrace:
	go test -race -count=1 ./...

# Acceptance tests against real Hetzner Cloud.
# Requires: HCLOUD_TOKEN, TF_ACC=1, ssh on PATH.
# `-p 1` serializes test-binary execution across packages because every
# binary's TestMain bootstraps + tears down the same shared fixtures.
# `-parallel=1` then serializes within one binary.
testacc:
	TF_ACC=1 go test -timeout 120m -p 1 -parallel=1 -count=1 ./...

lint:
	golangci-lint run

# Manual sweep — wipes any test fixtures left behind on the sandbox project.
sweep:
	TF_ACC=1 HCLOUD_SWEEP=1 go test -timeout 30m -count=1 ./internal/acctest -run '^TestSweep$$' -v

tidy:
	go mod tidy

# Regenerate the registry-format docs under docs/{index,resources,data-sources}.md
docs:
	tfplugindocs generate --provider-name hcloudgroup

RUNTIME ?= tofu
smoke:
	@test -n "$$HCLOUD_TOKEN" || (echo "HCLOUD_TOKEN unset" && exit 2)
	goreleaser build --single-target --snapshot --clean
	@GOOS=$$(go env GOOS) GOARCH=$$(go env GOARCH) sh ./.github/scripts/stage-provider.sh
	@SUFFIX="local-$$(date +%s)"; \
	  export TF_CLI_CONFIG_FILE="$$PWD/dist/dev_overrides.tfrc"; \
	  cd internal/smoketest && \
	  $(RUNTIME) init && \
	  $(RUNTIME) apply -auto-approve -var "suffix=$$SUFFIX" -var "image=debian-13" && \
	  $(RUNTIME) apply -auto-approve -var "suffix=$$SUFFIX" -var "image=debian-12"; \
	  rc=$$?; \
	  $(RUNTIME) destroy -auto-approve -var "suffix=$$SUFFIX" -var "image=debian-12" || true; \
	  exit $$rc
```

**ADAPT:** keep `test`, `testrace`, `lint`, `tidy`; `docs` → `tfplugindocs generate --provider-name leifwind`. `testacc` becomes container-based: `TF_ACC=1 go test -timeout 60m -count=1 ./...` (parallelism allowed per-org isolation; timeout right-sized for fixture boot). Two-module awareness: `test`/`lint`/`tidy` must run in both `.` and `./client` (go.work makes `go test ./...` cover only the root module's packages — add explicit `cd client && go test ./...` legs or `go test ./... ./client/...` via workspace). Drop `sweep` (no cloud sandbox) and `smoke` (or replace with a `GOWORK=off goreleaser build --single-target --snapshot` + local tofu dev_override run against a compose'd stack — v0 optional). RUNTIME defaults to `tofu` — keep.

---

## 8. `terraform-registry-manifest.json` — verbatim (use unchanged)

```json
{
  "version": 1,
  "metadata": {
    "protocol_versions": ["6.0"]
  }
}
```

---

## 9. go.mod — template versions (direct deps)

```text
module github.com/chickeaterbanana/terraform-provider-hcloudgroup

go 1.25.8

require (
	github.com/hashicorp/terraform-plugin-framework v1.19.0
	github.com/hashicorp/terraform-plugin-framework-timeouts v0.7.0
	github.com/hashicorp/terraform-plugin-framework-validators v0.19.0
	github.com/hashicorp/terraform-plugin-go v0.31.0
	github.com/hashicorp/terraform-plugin-testing v1.16.0
	github.com/hetznercloud/hcloud-go/v2 v2.39.0
	github.com/stretchr/testify v1.11.1
	golang.org/x/crypto v0.50.0
)
```

Notable indirects pinned by the template: terraform-plugin-log v0.10.0, terraform-plugin-sdk/v2 v2.40.0 (via plugin-testing), terraform-exec v0.25.1, hc-install v0.9.4.

**ADAPT for leifwind (root module `gitlab.com/leifwind/stream/terraform-provider-leifwind`, go 1.25):**
- Keep: terraform-plugin-framework v1.19.0, terraform-plugin-framework-validators v0.19.0, terraform-plugin-go v0.31.0, terraform-plugin-testing v1.16.0.
- Drop: terraform-plugin-framework-timeouts (no timeouts blocks in v0), hcloud-go, golang.org/x/crypto (was for SSH fixtures).
- Add: `gitlab.com/leifwind/stream/terraform-provider-leifwind/client` pinned to a **released** `client/vX.Y.Z` tag (bootstrap: `client/v0.1.0` first — LW-68); committed `go.work` covering `(., ./client)`; release jobs run `GOWORK=off`.
- `client/go.mod` (separate module): zero `terraform-plugin-*` deps; will need `github.com/google/uuid`, testcontainers-go (+ toxiproxy module) for `leifwindtest` — test-only heaviness lives in the client module, acceptable since `leifwindtest` is a public deliverable.
- pre-commit template (for completeness, verbatim): single hook `golangci-lint` `rev: v2.4.0` with `args: [--fix]` from `https://github.com/golangci/golangci-lint`. ADAPT: add conventional-commit message hook and a `go mod tidy` check per spec.
