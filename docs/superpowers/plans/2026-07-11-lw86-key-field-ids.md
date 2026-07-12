# key_field_ids KEY→FRAGMENT Ordering — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a required-for-FRAGMENT `key_field_ids` attribute to `leifwind_field` so consumers reference the entity's KEY field id(s), giving Terraform the dependency-graph edge that orders KEY-before-FRAGMENT create/destroy automatically — no manual `depends_on` — and retire the now-stale LW-70 workarounds.

**Architecture:** `leifwind_field` gains an `Optional` `set(string)` attribute `key_field_ids`. Because `leifwind_field.id` is `Computed`, the only way to populate it is a resource reference, which *is* the graph edge. `ValidateConfig` enforces the shape rule at plan time (present-for-FRAGMENT / absent-for-KEY) with a mandatory unknown-guard; `Create`/`Update` validate membership (each id is a real KEY field of the entity) at apply time via `ListFields`; `ImportState` seeds the value from the server. The attribute is never sent on the wire and `Read` never reconciles it.

**Tech Stack:** Go 1.25, `terraform-plugin-framework` v1.19, the repo's `/client` module, acceptance tests via `TF_ACC` + the `leifwindtest` containerized fixture (real backend + ZITADEL + Postgres).

**Spec:** `docs/superpowers/specs/2026-07-11-lw70-fragment-key-ordering-design.md`
**Ticket:** [LW-86](https://linear.app/leifwind/issue/LW-86). **Branch:** `feature/lw-86-terraform-provider-key_field_ids-attribute-for-automatic` (already checked out; the spec is already committed on it as `df78855`).

## Global Constraints

- Every Go file starts with `// SPDX-License-Identifier: MPL-2.0` (goheader-enforced).
- Provider code (non-`/client`) must not import `net/http` (depguard). Talk to the backend only through `r.c *client.Client`.
- Conventional-commit messages (commitlint-enforced); small atomic commits; strict TDD (test first).
- `key_field_ids` is **never** added to the upsert wire payload and **never** written by `modelFromClient` (it must survive as a config→state passthrough, exactly like `project_id`/`entity_id`).
- Backend enforcement is live in `../backend@24264c5`: FRAGMENT-create with no KEY → 422; delete of the entity's *last* KEY while FRAGMENT fields exist → 422; the old zero-fields-left 500 is fixed.
- Cleanup is **surgical**: only LW-70 KEY→FRAGMENT `depends_on` edges and `plantKeeperField` are removed. Legitimate data-source ordering `depends_on` (e.g. `depends_on = [leifwind_project.p]`, `[leifwind_entity.e]`, `[leifwind_field.a]`) stays.
- Unit tests: `go test ./internal/... ./client/...` (no `TF_ACC`). Acceptance: `make testacc` (needs docker + `tofu`; ~minutes per package).

---

## File structure

| File | Responsibility | Change |
|---|---|---|
| `internal/metadatares/field.go` | field resource: schema, validation, CRUD, import | Modify — new attribute, validators, apply-time validation, import seeding |
| `internal/metadatares/field_test.go` | pure-logic unit tests | Modify — add table tests for the two new pure helpers |
| `internal/lookup/lookup.go` | shared client-side lookup helpers | Modify — add `EntityFields` |
| `internal/acctest/field_acc_test.go` | field acceptance tests | Modify — dogfood `key_field_ids`, drop keeper, FRAGMENT import, new validation tests |
| `internal/acctest/fragments_ds_acc_test.go` | entity_fragments DS acceptance test | Modify — dogfood, drop keeper call |
| `internal/acctest/entity_field_ds_acc_test.go` | entity/field DS acceptance test | Modify — dogfood, drop keeper call |
| `examples/resources/leifwind_field/resource.tf` | doc example (feeds tfplugindocs) | Modify — dogfood |
| `docs/resources/field.md` | generated resource doc | Regenerate via `make docs` |
| `README.md`, `client/README.md` | user docs | Modify — remove stale LW-70 notes |
| `client/metadata_fields_test.go` | client field lifecycle test | Modify — refresh pre-fix 500 comments |
| `docs/superpowers/specs/2026-07-10-lw43-terraform-provider-design.md` | LW-43 design doc | Modify — two edits |

---

## Task 1: Model field, schema attribute, plan-time shape validation

Adds `key_field_ids` to the model + schema and enforces the shape rule (present-for-FRAGMENT / absent-for-KEY) in `ValidateConfig`, with the **mandatory** unknown-guard. Also drops the stale LW-70 `!> Warning` from the schema description (same `MarkdownDescription` we're editing).

**Files:**
- Modify: `internal/metadatares/field.go`
- Test: `internal/metadatares/field_test.go`

**Interfaces:**
- Produces: `validateKeyFieldIDsCombination(connectionType string, keyFieldsSet, keyFieldsEmpty bool) string` (returns "" when valid, else the error detail); `setToStrings(s types.Set) []string`; model field `KeyFieldIDs types.Set` (`tfsdk:"key_field_ids"`).

- [ ] **Step 1: Write the failing unit test** — append to `internal/metadatares/field_test.go`:

```go
func TestValidateKeyFieldIDsCombination(t *testing.T) {
	// FRAGMENT: key_field_ids required and non-empty
	if msg := validateKeyFieldIDsCombination("FRAGMENT", true, false); msg != "" {
		t.Fatalf("valid FRAGMENT rejected: %s", msg)
	}
	if msg := validateKeyFieldIDsCombination("FRAGMENT", false, false); msg == "" {
		t.Fatal("FRAGMENT without key_field_ids must be invalid")
	}
	if msg := validateKeyFieldIDsCombination("FRAGMENT", true, true); msg == "" {
		t.Fatal("FRAGMENT with empty key_field_ids must be invalid")
	}
	// KEY: key_field_ids forbidden
	if msg := validateKeyFieldIDsCombination("KEY", false, false); msg != "" {
		t.Fatalf("valid KEY rejected: %s", msg)
	}
	if msg := validateKeyFieldIDsCombination("KEY", true, false); msg == "" {
		t.Fatal("KEY with key_field_ids must be invalid")
	}
	if msg := validateKeyFieldIDsCombination("KEY", true, true); msg != "" {
		t.Fatalf("KEY with empty (non-null) key_field_ids should be tolerated: %s", msg)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/metadatares/ -run TestValidateKeyFieldIDsCombination`
Expected: FAIL — `undefined: validateKeyFieldIDsCombination`.

- [ ] **Step 3: Add the pure helpers** — in `internal/metadatares/field.go`, after `validateFieldCombination` (ends at line 58) add:

```go
// validateKeyFieldIDsCombination returns "" when valid, else the error detail.
// key_field_ids is a config-only ordering hint: required (non-empty) for
// FRAGMENT fields, forbidden for KEY fields.
func validateKeyFieldIDsCombination(connectionType string, keyFieldsSet, keyFieldsEmpty bool) string {
	switch connectionType {
	case string(client.ConnectionFragment):
		if !keyFieldsSet || keyFieldsEmpty {
			return "key_field_ids is required when connection_type is \"FRAGMENT\" " +
				"(reference the entity's KEY field ids, e.g. [leifwind_field.<key>.id])"
		}
	case string(client.ConnectionKey):
		if keyFieldsSet && !keyFieldsEmpty {
			return "key_field_ids must not be set when connection_type is \"KEY\""
		}
	}
	return ""
}

// setToStrings extracts the known string elements of a set (ignoring null /
// unknown elements). Used for both apply-time validation and the wire path.
func setToStrings(s types.Set) []string {
	elems := s.Elements()
	out := make([]string, 0, len(elems))
	for _, e := range elems {
		if sv, ok := e.(types.String); ok && !sv.IsNull() && !sv.IsUnknown() {
			out = append(out, sv.ValueString())
		}
	}
	return out
}
```

- [ ] **Step 4: Add the model field** — in `fieldModel` (struct at lines 38-47) add after `FragmentName`:

```go
	KeyFieldIDs    types.Set    `tfsdk:"key_field_ids"`
```

- [ ] **Step 5: Add the schema attribute** — in `Schema` (map at lines 72-122), after the `fragment_name` attribute (ends line 114) add:

```go
			"key_field_ids": schema.SetAttribute{
				Optional:    true,
				ElementType: types.StringType,
				MarkdownDescription: "Ordering hint (config-only; never sent to the API). The object ids of " +
					"this entity's KEY fields, e.g. `[leifwind_field.title.id]`. **Required for FRAGMENT fields, " +
					"forbidden for KEY fields.** The backend requires a KEY field before FRAGMENT fields exist; " +
					"referencing the KEY field ids here makes Terraform create the KEY first and destroy it last, " +
					"without a manual `depends_on`. Reference **all** of the entity's KEY fields.",
			},
```

- [ ] **Step 6: Drop the stale LW-70 warning from the resource description** — replace the `MarkdownDescription` block at lines 66-71:

```go
		MarkdownDescription: "A leifwind metadata field. Only fragment_name is updatable in place; every other attribute forces replacement.\n\n" +
			"!> **Warning:** destroying a Terraform configuration that owns *all* of an entity's fields " +
			"currently fails with a server 500 in `sync_entity_schema` when the last field is deleted " +
			"(backend bug LW-70). Until the backend fix ships, keep at least one field un-managed by this " +
			"configuration on each entity, or destroy the owning `leifwind_entity` instead of deleting every " +
			"one of its fields individually.",
```

with:

```go
		MarkdownDescription: "A leifwind metadata field. Only fragment_name is updatable in place; every other attribute forces replacement.\n\n" +
			"FRAGMENT fields require a sibling KEY field on the entity (backend-enforced). Set `key_field_ids` " +
			"to the entity's KEY field ids so Terraform orders creation and destruction correctly. See the " +
			"`key_field_ids` attribute for the one case this does not cover (in-place replacement of an entity's " +
			"sole KEY field).",
```

- [ ] **Step 7: Wire `ValidateConfig`** — replace the body of `ValidateConfig` (lines 126-136). Add the `KeyFieldIDs.IsUnknown()` guard **first**, then the key check:

```go
func (r *fieldResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var cfg fieldModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() || cfg.ConnectionType.IsUnknown() ||
		cfg.FragmentName.IsUnknown() || cfg.KeyFieldIDs.IsUnknown() {
		return
	}
	if msg := validateFieldCombination(cfg.ConnectionType.ValueString(),
		cfg.FragmentName.ValueString(), !cfg.FragmentName.IsNull()); msg != "" {
		resp.Diagnostics.AddAttributeError(path.Root("fragment_name"), "Invalid field configuration", msg)
	}
	if msg := validateKeyFieldIDsCombination(cfg.ConnectionType.ValueString(),
		!cfg.KeyFieldIDs.IsNull(), len(cfg.KeyFieldIDs.Elements()) == 0); msg != "" {
		resp.Diagnostics.AddAttributeError(path.Root("key_field_ids"), "Invalid field configuration", msg)
	}
}
```

> Why the guard is load-bearing: a `set(string)` whose element references a `Computed` id renders as a **wholly unknown set** at plan (`IsUnknown()==true`, `Elements()` empty) — cty can't keep a set known with an unknown element. Without the guard, the normal `key_field_ids = [leifwind_field.title.id]` case would hit `len(Elements()) == 0` and fire a spurious "required" error on every fresh FRAGMENT create.

- [ ] **Step 8: Verify it builds and unit tests pass**

Run: `go build ./... && go test ./internal/metadatares/`
Expected: build OK; `TestValidateKeyFieldIDsCombination` and `TestValidateFieldCombination` PASS.

- [ ] **Step 9: Commit**

```bash
git add internal/metadatares/field.go internal/metadatares/field_test.go
git commit -m "feat(field): add key_field_ids attribute with plan-time shape validation"
```

---

## Task 2: Membership-check pure helper

Adds the pure set-membership helper used by apply-time validation, unit-tested in isolation.

**Files:**
- Modify: `internal/metadatares/field.go`
- Test: `internal/metadatares/field_test.go`

**Interfaces:**
- Produces: `missingKeyFieldIDs(supplied []string, keyIDs map[string]struct{}) []string` (the supplied ids not present in `keyIDs`, preserving order; nil when all present).

- [ ] **Step 1: Write the failing unit test** — append to `internal/metadatares/field_test.go`:

```go
func TestMissingKeyFieldIDs(t *testing.T) {
	keys := map[string]struct{}{"a": {}, "b": {}}
	if got := missingKeyFieldIDs([]string{"a", "b"}, keys); got != nil {
		t.Fatalf("all present should be nil, got %v", got)
	}
	if got := missingKeyFieldIDs([]string{"a", "c"}, keys); len(got) != 1 || got[0] != "c" {
		t.Fatalf("want [c], got %v", got)
	}
	if got := missingKeyFieldIDs(nil, keys); got != nil {
		t.Fatalf("empty supplied should be nil, got %v", got)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/metadatares/ -run TestMissingKeyFieldIDs`
Expected: FAIL — `undefined: missingKeyFieldIDs`.

- [ ] **Step 3: Implement the helper** — in `internal/metadatares/field.go`, after `setToStrings`, add:

```go
// missingKeyFieldIDs returns the supplied ids that are not present in keyIDs
// (order preserved). nil when every supplied id is present.
func missingKeyFieldIDs(supplied []string, keyIDs map[string]struct{}) []string {
	var missing []string
	for _, id := range supplied {
		if _, ok := keyIDs[id]; !ok {
			missing = append(missing, id)
		}
	}
	return missing
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/metadatares/ -run TestMissingKeyFieldIDs`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/metadatares/field.go internal/metadatares/field_test.go
git commit -m "feat(field): add missingKeyFieldIDs membership helper"
```

---

## Task 3: `lookup.EntityFields`, apply-time validation, import seeding

Wires the I/O: a shared "list all fields of an entity" helper, apply-time membership validation in `Create`/`Update`, and server-seeding of `key_field_ids` in `ImportState`. (Behavior is exercised by the acceptance tests in Task 4; here we only build + keep unit tests green.)

**Files:**
- Modify: `internal/lookup/lookup.go`, `internal/metadatares/field.go`

**Interfaces:**
- Consumes: `client.MetadataService.IterFields(ctx, projectID, entityID, client.ListOpts) iter.Seq2[client.MetadataField, error]`; `client.ConnectionKey`/`ConnectionFragment`; `missingKeyFieldIDs`, `setToStrings` (Tasks 1–2).
- Produces: `lookup.EntityFields(ctx, c, projectID, entityID uuid.UUID) ([]client.MetadataField, error)`; `(*fieldResource).validateKeyFieldMembership(ctx, plan fieldModel, f client.MetadataField) error`.

- [ ] **Step 1: Add `lookup.EntityFields`** — in `internal/lookup/lookup.go`, after `FieldByName` (ends line 55) add:

```go
// EntityFields returns all fields of an entity (all pages). Used to validate
// key_field_ids membership and to seed it on import.
func EntityFields(ctx context.Context, c *client.Client, projectID, entityID uuid.UUID) ([]client.MetadataField, error) {
	var out []client.MetadataField
	for f, err := range c.Metadata.IterFields(ctx, projectID, entityID, client.ListOpts{}) {
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, nil
}
```

- [ ] **Step 2: Add the membership-validation method** — in `internal/metadatares/field.go`, after `modelFromClient` (ends line 193) add:

```go
// validateKeyFieldMembership verifies the FRAGMENT field's key_field_ids all
// reference existing KEY fields of the same entity. No-op for KEY fields
// (beyond rejecting a stray value). The graph edge guarantees the referenced
// KEY fields already exist by the time Create/Update runs, so a bad id here
// means a genuine mistake — surface it as a clear diagnostic rather than a
// downstream backend 422.
func (r *fieldResource) validateKeyFieldMembership(ctx context.Context, plan fieldModel, f client.MetadataField) error {
	supplied := setToStrings(plan.KeyFieldIDs)
	if f.Connection.Type != client.ConnectionFragment {
		if len(supplied) > 0 {
			return fmt.Errorf("key_field_ids must not be set when connection_type is %q", f.Connection.Type)
		}
		return nil
	}
	fields, err := lookup.EntityFields(ctx, r.c, f.ProjectID, f.EntityID)
	if err != nil {
		return err
	}
	keyIDs := make(map[string]struct{})
	for _, ef := range fields {
		if ef.Connection.Type == client.ConnectionKey && ef.ObjectID != nil {
			keyIDs[ef.ObjectID.String()] = struct{}{}
		}
	}
	if missing := missingKeyFieldIDs(supplied, keyIDs); len(missing) > 0 {
		return fmt.Errorf("key_field_ids: %v are not KEY fields of entity %s (reference the entity's KEY field ids)", missing, f.EntityID)
	}
	return nil
}
```

- [ ] **Step 3: Call it from `Create`** — in `Create`, insert the check immediately before the `UpsertField` call (currently line 228, `created, err := r.c.Metadata.UpsertField(ctx, f)`):

```go
	if err := r.validateKeyFieldMembership(ctx, plan, f); err != nil {
		resp.Diagnostics.AddAttributeError(path.Root("key_field_ids"), "Invalid key_field_ids", err.Error())
		return
	}

	created, err := r.c.Metadata.UpsertField(ctx, f)
```

- [ ] **Step 4: Call it from `Update`** — in `Update`, insert immediately before its `UpsertField` call (currently line 273, `updated, err := r.c.Metadata.UpsertField(ctx, f)`):

```go
	if err := r.validateKeyFieldMembership(ctx, plan, f); err != nil {
		resp.Diagnostics.AddAttributeError(path.Root("key_field_ids"), "Invalid key_field_ids", err.Error())
		return
	}

	updated, err := r.c.Metadata.UpsertField(ctx, f)
```

- [ ] **Step 5: Seed `key_field_ids` in `ImportState`** — replace the whole `ImportState` method (lines 298-307) with:

```go
func (r *fieldResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	ids, err := parseImportUUIDs(req.ID, 3)
	if err != nil {
		resp.Diagnostics.AddError("Invalid import ID", err.Error()+" (expected <project_id>/<entity_id>/<field_id>)")
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("project_id"), ids[0].String())...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("entity_id"), ids[1].String())...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), ids[2].String())...)
	if resp.Diagnostics.HasError() {
		return
	}

	// key_field_ids is config-only (never returned by GetField), so recover it
	// from the server for FRAGMENT fields: one ListFields call locates the
	// imported field (to read its connection_type) and collects the entity's
	// KEY field ids. KEY fields import with key_field_ids null.
	fields, err := lookup.EntityFields(ctx, r.c, ids[0], ids[1])
	if err != nil {
		resp.Diagnostics.AddError("Listing entity fields for import failed", err.Error())
		return
	}
	var self *client.MetadataField
	keyElems := make([]attr.Value, 0, len(fields))
	for i := range fields {
		ef := fields[i]
		if ef.ObjectID != nil && *ef.ObjectID == ids[2] {
			self = &fields[i]
		}
		if ef.Connection.Type == client.ConnectionKey && ef.ObjectID != nil {
			keyElems = append(keyElems, types.StringValue(ef.ObjectID.String()))
		}
	}
	if self == nil {
		resp.Diagnostics.AddError("Field not found for import",
			fmt.Sprintf("no field %s on entity %s", ids[2], ids[1]))
		return
	}
	if self.Connection.Type == client.ConnectionFragment {
		set, d := types.SetValue(types.StringType, keyElems)
		resp.Diagnostics.Append(d...)
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("key_field_ids"), set)...)
	}
}
```

- [ ] **Step 6: Add the `attr` import** — in the import block (lines 5-22) add, with the other framework imports:

```go
	"github.com/hashicorp/terraform-plugin-framework/attr"
```

- [ ] **Step 7: Verify build, vet, and unit tests**

Run: `go build ./... && go vet ./internal/... && go test ./internal/metadatares/ ./internal/lookup/`
Expected: build/vet clean; unit tests PASS. (Acceptance behavior is verified in Task 4.)

- [ ] **Step 8: Commit**

```bash
git add internal/lookup/lookup.go internal/metadatares/field.go
git commit -m "feat(field): validate key_field_ids membership on apply and seed it on import"
```

---

## Task 4: Dogfood the acceptance tests + new validation coverage

Replaces every LW-70 `depends_on` on a FRAGMENT field with `key_field_ids`, removes `plantKeeperField` (definition + all three call sites — they must go together or the package won't compile), switches the lifecycle import to the **FRAGMENT** field so seeding is actually exercised, and adds the shape + membership validation tests. This is the feature-coupled "dogfood" commit.

**Files:**
- Modify: `internal/acctest/field_acc_test.go`, `internal/acctest/fragments_ds_acc_test.go`, `internal/acctest/entity_field_ds_acc_test.go`

**Interfaces:**
- Consumes: the `key_field_ids` attribute (Tasks 1–3).

- [ ] **Step 1: Rewrite `fieldConfig` in `field_acc_test.go`** — replace lines 19-55 (the comment, function, and its body) with:

```go
func fieldConfig(token, fragmentName string) string {
	// The FRAGMENT field references the KEY field via key_field_ids, which
	// creates the Terraform graph edge that orders KEY-before-FRAGMENT on
	// create and FRAGMENT-before-KEY on destroy (both backend-enforced, LW-70).
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
  key_field_ids   = [leifwind_field.title.id]
}
`, fragmentName)
}
```

- [ ] **Step 2: Delete `plantKeeperField`** — remove the entire function and its doc comment, lines 57-93 of the original `field_acc_test.go`.

- [ ] **Step 3: Remove the keeper call + switch the lifecycle import to the FRAGMENT field** — in `TestAccFieldLifecycleAndFragmentUpdate`:
  - In the first step's `Check` (originally line 112), delete the line `plantKeeperField(t, tok, "leifwind_field.title"), // LW-70`.
  - Replace the import step (originally lines 125-136) so it imports `leifwind_field.body`:

```go
			{
				ResourceName: "leifwind_field.body",
				ImportState:  true,
				ImportStateIdFunc: func(s *terraform.State) (string, error) {
					rs := s.RootModule().Resources["leifwind_field.body"]
					return fmt.Sprintf("%s/%s/%s",
						rs.Primary.Attributes["project_id"],
						rs.Primary.Attributes["entity_id"],
						rs.Primary.ID), nil
				},
				ImportStateVerify: true,
			},
```

> This makes the import meaningful: `body`'s `key_field_ids` is seeded from the server (`{title.id}`) and must round-trip against the config value `[leifwind_field.title.id]` — no `ImportStateVerifyIgnore`. Importing `title` (the KEY) would pass vacuously since its `key_field_ids` is null either way.

- [ ] **Step 4: Add shape + membership validation tests** — append to `field_acc_test.go`:

```go
func TestAccFieldKeyFieldIDsRequired(t *testing.T) {
	PreCheck(t)
	t.Parallel()
	org := NewOrg(t)
	tok := org.Token(t, Stack())
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: ProtoV6ProviderFactories(),
		Steps: []resource.TestStep{{
			Config: ProviderConfig(tok) + `
resource "leifwind_project" "p" {
  name = "acc_fld_keyreq"
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
  fragment_name   = "content"
}
`,
			ExpectError: regexp.MustCompile(`key_field_ids is required`),
		}},
	})
}

func TestAccFieldKeyFieldIDsMembership(t *testing.T) {
	PreCheck(t)
	t.Parallel()
	org := NewOrg(t)
	tok := org.Token(t, Stack())
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: ProtoV6ProviderFactories(),
		Steps: []resource.TestStep{{
			// "extra" references a FRAGMENT field id (body) rather than a KEY
			// field id, so its Create must fail the membership check.
			Config: ProviderConfig(tok) + `
resource "leifwind_project" "p" {
  name = "acc_fld_keymember"
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
  fragment_name   = "content"
  key_field_ids   = [leifwind_field.title.id]
}

resource "leifwind_field" "extra" {
  project_id      = leifwind_project.p.id
  entity_id       = leifwind_entity.e.id
  name            = "extra"
  data_type       = "TEXT"
  connection_type = "FRAGMENT"
  fragment_name   = "content"
  key_field_ids   = [leifwind_field.body.id]
}
`,
			ExpectError: regexp.MustCompile(`not KEY fields of entity`),
		}},
	})
}
```

- [ ] **Step 5: Dogfood `fragments_ds_acc_test.go`** — replace the FRAGMENT field `a`'s `depends_on` line (line 58):

```go
  depends_on = [leifwind_field.title] # LW-70, see fieldConfig comment in field_acc_test.go
```

with:

```go
  key_field_ids = [leifwind_field.title.id]
```

Then delete the `plantKeeperField(t, tok, "leifwind_field.title"), // LW-70 destroy-safety, see field_acc_test.go` line (line 74). **Keep** `depends_on = [leifwind_field.a]` on the `data "leifwind_entity_fragments" "f"` block (line 64) — that's legitimate data-source ordering. Refresh the file's top doc-comment (lines 12-26) to describe the `key_field_ids` edge instead of the LW-70 bug.

- [ ] **Step 6: Dogfood `entity_field_ds_acc_test.go`** — replace the FRAGMENT field `f`'s `depends_on` line (line 61):

```go
  depends_on = [leifwind_field.title] # LW-70, see fieldConfig comment in field_acc_test.go
```

with:

```go
  key_field_ids = [leifwind_field.title.id]
```

Delete the `plantKeeperField(...)` line (line 98). **Keep** `pattern = "body"` on `data "leifwind_fields" "all"` (line 83) — it still isolates the list from the `title` KEY field — but drop the LW-70 wording from its inline comment (e.g. `# isolates from the title KEY field`). **Keep** the data-source `depends_on` edges (lines 71, 84). Refresh the top doc-comment (lines 12-18).

- [ ] **Step 7: Build the test package**

Run: `go vet ./internal/acctest/`
Expected: compiles (no `plantKeeperField`/undefined-symbol errors).

- [ ] **Step 8: Run the field acceptance tests**

Run:
```bash
TF_ACC=1 TF_ACC_PROVIDER_HOST=registry.opentofu.org TF_ACC_TERRAFORM_PATH=$(command -v tofu) \
  go test ./internal/acctest/ -v -timeout 45m \
  -run 'TestAccField|TestAccEntityFragments|TestAccEntityFieldDataSources'
```
Expected: PASS — lifecycle (incl. FRAGMENT import verifying `key_field_ids`), `TestAccFieldKeyFieldIDsRequired`, `TestAccFieldKeyFieldIDsMembership`, and both data-source tests. (Adjust `-run` names to the actual test function names in the two DS files if they differ.)

- [ ] **Step 9: Commit**

```bash
git add internal/acctest/field_acc_test.go internal/acctest/fragments_ds_acc_test.go internal/acctest/entity_field_ds_acc_test.go
git commit -m "test(field): dogfood key_field_ids in acceptance tests; drop LW-70 keeper"
```

---

## Task 5: Backend-fix-gated doc/comment cleanup

The genuinely independent LW-70 removals (not coupled to the feature). Separate commit per the spec's split.

**Files:**
- Modify: `README.md`, `client/README.md`, `client/metadata_fields_test.go`

- [ ] **Step 1: Root `README.md`** — remove the stale LW-70 note around line 79. Find the block mentioning `sync_entity_schema (LW-70). Until the backend fix ships, keep at least ...` and delete it (or, if it sits inside a broader field-usage paragraph, replace the LW-70 sentences with a one-liner: "FRAGMENT fields require a sibling KEY field; set `key_field_ids` to order them — see the `leifwind_field` docs."). Verify with `rg -n -i 'lw-70|sync_entity_schema' README.md` → no field-lifecycle-bug references remain.

- [ ] **Step 2: `client/README.md`** — remove the LW-70 note at lines 65-67 (`**Deleting the last field of an entity currently 500s** ... (LW-70). Keep at least one field alive ...`). The client has no `key_field_ids` concept, so just delete the stale warning. Verify: `rg -n -i 'lw-70|500s' client/README.md` → none.

- [ ] **Step 3: `client/metadata_fields_test.go`** — refresh the pre-fix comments (lines 85-97) to describe the enforced 422s instead of `backend:edge` 500s. Replace the comment at lines 85-87:

```go
	// LW-70 also 500s when a delete would leave the entity with zero fields
	// (sync_entity_schema: "no fields provided to get_table_structure"), so
	// keep one KEY field alive while both delete paths are exercised below.
```

with:

```go
	// The backend enforces KEY-before-FRAGMENT (LW-70): the entity's last KEY
	// field can't be deleted while a FRAGMENT sibling exists. Add a second KEY
	// field so the KEY delete below isn't the entity's last.
```

and the comment at lines 96-97:

```go
	// Delete FRAGMENT before KEY: deleting a KEY field while a FRAGMENT
	// sibling exists 500s on backend:edge (LW-70).
```

with:

```go
	// Delete FRAGMENT before KEY: deleting the entity's last KEY field while a
	// FRAGMENT sibling exists is rejected (422) by the backend (LW-70).
```

- [ ] **Step 4: Verify the client test still passes**

Run: `cd client && go test ./... -run TestFieldLifecycleKeyAndFragment -timeout 20m -p 1; cd ..`
Expected: PASS (comment-only change; behavior unchanged). If the fixture is unavailable locally, at minimum `cd client && go vet ./... && cd ..` must be clean.

- [ ] **Step 5: Commit**

```bash
git add README.md client/README.md client/metadata_fields_test.go
git commit -m "docs: retire LW-70 pre-fix workaround notes (backend fix is live)"
```

---

## Task 6: Example + regenerated resource docs

**Files:**
- Modify: `examples/resources/leifwind_field/resource.tf`
- Regenerate: `docs/resources/field.md`

- [ ] **Step 1: Update the example** — replace the `leifwind_field.body` block in `examples/resources/leifwind_field/resource.tf` (lines 9-22) with:

```hcl
resource "leifwind_field" "body" {
  project_id      = leifwind_project.library.id
  entity_id       = leifwind_entity.book.id
  name            = "body"
  data_type       = "TEXT"
  connection_type = "FRAGMENT"
  fragment_name   = "content"

  # A FRAGMENT field needs a sibling KEY field on the entity. Referencing the
  # KEY field's id here makes Terraform create it first and destroy it last —
  # no manual depends_on. List all of the entity's KEY fields.
  key_field_ids = [leifwind_field.title.id]
}
```

- [ ] **Step 2: Regenerate docs**

Run: `make docs`
Expected: `docs/resources/field.md` regenerated; `git diff --stat` shows it changed to include `key_field_ids`.

- [ ] **Step 3: Add the sole-KEY-replace limitation note to `docs/resources/field.md`** — tfplugindocs renders from schema descriptions, so the attribute doc comes through automatically, but the replacement caveat is prose. If the template supports it, add a short note after the generated attribute reference; otherwise add it to the `key_field_ids` `MarkdownDescription` in `field.go` and re-run `make docs`. Target text:

> Replacing an entity's **sole** KEY field in place (e.g. renaming it) is not covered: Terraform destroys the old KEY before creating the new one, and the backend rejects deleting the last KEY while FRAGMENT fields exist. Add the replacement KEY under a new resource address, repoint `key_field_ids`, then remove the old KEY as a separate step.

- [ ] **Step 4: Verify no stale example text**

Run: `rg -n -i 'lw-70|depends_on' examples/resources/leifwind_field/resource.tf`
Expected: no matches.

- [ ] **Step 5: Commit**

```bash
git add examples/resources/leifwind_field/resource.tf docs/resources/field.md internal/metadatares/field.go
git commit -m "docs(field): show key_field_ids in example and regenerate docs"
```

---

## Task 7: LW-43 design-doc edits

Keep the parent design doc consistent. (Two edits only — the doc has no LW-70 references, verified by grep.)

**Files:**
- Modify: `docs/superpowers/specs/2026-07-10-lw43-terraform-provider-design.md`

- [ ] **Step 1: Update the `leifwind_field` resource-table row** (line ~124). The row currently lists attributes and marks only `fragment_name` updatable. Add `key_field_ids` to the attribute list and note it is config-only / updatable / no-replace. Example replacement for the `leifwind_field` row:

```markdown
| `leifwind_field` | `id`, `project_id`, `entity_id`, `name`, `data_type`, `connection_type`, `fragment_name`, `key_field_ids` | all except `fragment_name`, `key_field_ids` | `fragment_name`, `key_field_ids` (config-only ordering hint) | `<project_id>/<entity_id>/<field_id>` |
```

- [ ] **Step 2: Extend the `ValidateConfig` bullet** (line ~127) — after the `fragment_name` rule, add:

```markdown
`key_field_ids` required iff `connection_type == "FRAGMENT"`, forbidden otherwise (config-only; the reference to the KEY field id(s) supplies the Terraform create/destroy ordering edge — LW-86); membership validated at apply.
```

- [ ] **Step 3: Verify**

Run: `rg -n 'key_field_ids' docs/superpowers/specs/2026-07-10-lw43-terraform-provider-design.md`
Expected: matches in the resource table and the ValidateConfig bullet.

- [ ] **Step 4: Commit**

```bash
git add docs/superpowers/specs/2026-07-10-lw43-terraform-provider-design.md
git commit -m "docs(lw-43): record key_field_ids on leifwind_field"
```

---

## Final verification

- [ ] **Lint both modules**: `make lint` → clean (goheader, depguard, etc.).
- [ ] **Unit tests**: `go test ./internal/... && cd client && go test ./... -run 'Validate|Model|Connection' -timeout 5m; cd ..` → PASS.
- [ ] **Full acceptance**: `make testacc` → all green against the fixed backend fixture.
- [ ] **No stale LW-70 workarounds**: `rg -n -i 'plantKeeper|lw-70' internal/ examples/ README.md client/README.md` → only historical spec/plan references (under `docs/`), no live `plantKeeperField`, no FRAGMENT `depends_on` LW-70 edges.
- [ ] **No accidental wire leakage**: confirm `toClientField` and `modelFromClient` still do not reference `KeyFieldIDs` (grep `KeyFieldIDs` in `field.go` — it should appear only in the model, schema, `ValidateConfig`, `validateKeyFieldMembership`, and `ImportState`, never in `toClientField`/`modelFromClient`).
- [ ] Update [LW-86](https://linear.app/leifwind/issue/LW-86) to In Review with a summary; open the MR.

## Notes for the executor

- **Intermediate state:** after Task 1 the existing acceptance configs (which lack `key_field_ids` on FRAGMENT fields) would fail `ValidateConfig`. That's expected; Tasks 1–3 gate on **unit tests + build**, and Task 4 restores acceptance green. Don't run `make testacc` between Tasks 1 and 4 and treat failures as regressions.
- **Line numbers** reference the pre-change files and drift as you edit — anchor on the quoted code, not the numbers.
- **DS test function names:** Steps 5–6 of Task 4 edit `fragments_ds_acc_test.go` and `entity_field_ds_acc_test.go`; confirm the actual `-run` test names before the acceptance run (they weren't renamed here).
- **`ListOpts{}`** with an empty `Pattern` lists all fields — `IterFields` auto-pages, so `EntityFields` returns every field regardless of count.
