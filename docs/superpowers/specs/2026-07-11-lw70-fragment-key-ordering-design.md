# LW-86 Design: `key_field_ids` — provider-enforced KEY→FRAGMENT ordering

**Status:** Refined — awaiting owner approval, then implementation plan.
**Date:** 2026-07-11
**Ticket:** [LW-86](https://linear.app/leifwind/issue/LW-86) (parent [LW-41](https://linear.app/leifwind/issue/LW-41); follow-up to [LW-70](https://linear.app/leifwind/issue/LW-70), builds on [LW-43](https://linear.app/leifwind/issue/LW-43))
**Backend contract verified against:** `../backend` @ `24264c5` (`fix(lw-70): require a KEY field before FRAGMENT fields exist`).

---

## Context

[LW-70](https://linear.app/leifwind/issue/LW-70) fixed two backend 500s by making the KEY↔FRAGMENT relationship an **enforced ordering constraint** in both directions. Confirmed in `../backend/src/leifwind/stream/backend/metadata/api.py`:

- **Create** (`upsert_field`, ~line 715): creating a `FRAGMENT` field on an entity with no `KEY` field →
  `422 entity has no KEY field; create a KEY field before adding FRAGMENT fields`. Any single KEY field satisfies the check.
- **Delete** (`delete_field`, ~line 817): deleting an entity's **last** `KEY` field while `FRAGMENT` fields still exist →
  `422 cannot delete the entity's last KEY field while FRAGMENT fields exist; delete the FRAGMENT fields first`. Deleting a non-last KEY, or any KEY once no fragments remain, is allowed.
- The old **"delete the last field → 500"** bug (`sync_entity_schema`: *no fields provided to get_table_structure*) is **also fixed** — the resync now tolerates an empty field list and drops the entity table symmetric to its lazy creation.

### The Terraform problem

`leifwind_field` is a **field-level** resource (per the LW-43 decision: fragments are field attributes, no separate `leifwind_fragment` resource). A `FRAGMENT` field references its entity (`entity_id = leifwind_entity.book.id`) but has **no config-level reference to the sibling KEY field(s)**. So Terraform's dependency graph contains no KEY→FRAGMENT edge, and is free to:

- create a FRAGMENT field before the entity's KEY field → backend 422 (create rule), or
- during destroy, delete the last KEY field before the FRAGMENT fields → backend 422 (delete rule).

**A provider cannot fix this by itself.** Terraform builds the dependency graph from *config-level references* before any provider code runs; a provider has no hook to inject graph edges. The only way to get correct, automatic ordering while keeping field-level resources is a **config-level reference the consumer supplies**.

Today the only workaround is a hand-written `depends_on = [leifwind_field.<key>]` on every FRAGMENT field — the consumer must know the rule and remember it on each fragment. The provider's own acceptance test does exactly this (`internal/acctest/field_acc_test.go`, `fieldConfig`). We want to remove that burden from every consumer.

## Goal

Make the KEY→FRAGMENT ordering **automatic and impossible to forget**, without a manual `depends_on`, while keeping field-level resources.

Non-goals: changing the wire protocol (the API neither needs nor accepts this input); a `leifwind_fragment` resource; validating cross-resource KEY/entity correctness at plan time (not possible — apply-time validation covers it instead, see Design).

## Alternatives considered and rejected

- **Entity-level aggregate resource** — model an entity's KEY+FRAGMENT fields as nested blocks of one `leifwind_entity`-ish resource, so the provider sequences KEY-before-FRAGMENT internally in Go and Terraform's graph never sees individual fields. Sidesteps the "consumer supplies a required id list" mechanism entirely. **Rejected:** it abandons the field-level granularity already shipped in LW-43 (one resource per field, independent import ids, per-field lifecycle), is a far larger blast radius (new resource, new state shape, migration story), and re-implements dependency ordering that Terraform already does well. `key_field_ids` is an additive, localized change by comparison.
- **Read-reconciles `key_field_ids` from the server** — have `Read` re-derive the value from the entity's current KEY fields. **Rejected:** it fights any config that references a subset of keys (perpetual diffs) and couples a fragment's steady-state to sibling resources' server state. We consult the server only where there's no config to honor (import) and to validate (apply). Config drives steady state.
- **Documentation-only (`depends_on`)** — keep the status quo and just document the rule better. **Rejected:** it's the exact burden we're removing; nothing makes it unforgettable, and the failure mode (a nondeterministic apply/destroy 422) is confusing and intermittent.

## Design

### The mechanism (why a required reference works)

`leifwind_field.id` is a **`Computed`** server-assigned UUID. A fresh Terraform config therefore *cannot* hard-code a KEY field's id — the only way to obtain it is a reference: `leifwind_field.title.id`. A reference **is** a dependency-graph edge. So an attribute on the FRAGMENT field that must be populated with KEY field ids gives us, for free:

- **create** ordering: the referenced KEY field is created before the FRAGMENT, and
- **destroy** ordering: the FRAGMENT is destroyed before the referenced KEY field(s).

Making the attribute **required for FRAGMENT fields** turns "remember to add `depends_on`" into a schema/validation error you cannot skip. The requiredness makes it unforgettable; the reference makes it automatic. This is strictly better than documenting `depends_on`, and it is the *only* construct that achieves automatic ordering for field-level resources.

### The attribute

New attribute on `leifwind_field`:

```hcl
resource "leifwind_field" "title" {
  entity_id       = leifwind_entity.book.id
  name            = "title"
  data_type       = "TEXT"
  connection_type = "KEY"
}

resource "leifwind_field" "body" {
  entity_id       = leifwind_entity.book.id
  name            = "body"
  data_type       = "TEXT"
  connection_type = "FRAGMENT"
  fragment_name   = "content"
  key_field_ids   = [leifwind_field.title.id]   # required for FRAGMENT; see rule below
}
```

- **Name:** `key_field_ids` (owner-selected).
- **Type:** `set(string)` — order-independent and de-duplicated; it is a pure ordering hint, so element order carries no meaning and a `set` avoids spurious reorder diffs.
- **Documented rule:** populate with **all** KEY fields of the same entity. Simple, teachable, correct for create and for whole-fragment/whole-entity destroy, and the value that import round-trips cleanly. (Technically one KEY suffices for the backend checks, but "all keys" is the rule we document — no nuance for consumers, and it matches the import seed. The one case it does **not** cover — in-place replacement of an entity's sole KEY — is inherent to `RequiresReplace`, not to this rule; see Limitations.)

### Schema shape (mirrors the existing `fragment_name` pattern)

- **`Optional: true`** — *not* schema-`Required`. The same resource type serves KEY fields, which must **not** set it. "Required for FRAGMENT" is enforced in `ValidateConfig` (plan) + `Create`/`Update` (apply).
- **No `RequiresReplace`** and no server-round-tripping plan modifier. It never appears in the upsert payload; changing it is state-only *except* that, having a diff, it still routes through the resource's `Update` (which issues an idempotent `UpsertField` — see lifecycle below). "State-only" describes its wire contribution, not that `Update` is skipped.
- **Validation (plan-time, `ValidateConfig`)** extends the existing `validateFieldCombination`:
  - `connection_type == "FRAGMENT"` → `key_field_ids` must be present and non-empty; else error on `path.Root("key_field_ids")`.
  - `connection_type == "KEY"` → `key_field_ids` must be null/empty; else error.
  - **The unknown guard is mandatory and must be evaluated FIRST.** Return early when `connection_type` **or** `key_field_ids` is unknown. The current guard (`field.go:129`) only checks `ConnectionType`/`FragmentName`; it **must** add `|| cfg.KeyFieldIDs.IsUnknown()`.
  - **Load-bearing correction to the naïve reasoning:** a `set(string)` whose elements reference a `Computed` id renders as a **wholly unknown set** — Terraform core (cty) cannot keep a set known when an element is unknown, because an unknown element could dedupe against a known one and make cardinality indeterminate. So for the normal `key_field_ids = [leifwind_field.title.id]` case at plan time, `KeyFieldIDs.IsUnknown() == true` and `Elements()` is **empty**. The requiredness check is therefore *skipped* on a fresh FRAGMENT create (correct — the early-return handles it), **not** passed by a "known non-empty set". If an implementer drops/reorders the `IsUnknown()` guard, `len(Elements()) == 0` fires a spurious *"key_field_ids required"* error on every fresh FRAGMENT create. (A `list` would render `[(known after apply)]` and behave as the old prose claimed — but `set` is the right type for diff behavior, and this guard is its price.)
  - **Consequence to document:** plan-time requiredness only catches the **null (omitted)** and **known-empty (`[]`)** cases. A supplied reference defers the "present" guarantee to apply — which is exactly the case we want caught (omission), so this is fine; the apply-time validation below covers wrong/non-KEY ids.
  - `Create`/`Update` also re-assert the KEY-must-not-set-it rule at apply (defensive: `ValidateConfig` skips it if `key_field_ids` was unknown on a KEY field).

### Wire / lifecycle behavior

- **Never sent to the API.** `toClientField` ignores it (the backend has no such field). It is not part of the upsert payload on `Create`/`Update`.
- **`Read` does not reconcile it.** `modelFromClient` does **not** overwrite it — exactly like `project_id`/`entity_id` today — so a steady-state `Read` keeps the configured value and never produces drift. (Read intentionally does *not* re-derive it from the server: doing so would fight a config that references a subset of keys and create perpetual diffs. Config drives it; the server is consulted only to fill the import gap and to validate — see below.)
- **`Update`:** unchanged in effect. A lone `key_field_ids` change (no server-visible attribute changed) still routes through `Update` → an idempotent `UpsertField` with the same `fragment_name`; harmless. (No special-casing needed.)
- **Import recovers it from the server.** `ImportState` has `project_id`/`entity_id` (from the `<project_id>/<entity_id>/<field_id>` import ID). It calls `ListFields(project_id, entity_id)` **once**, using that single result to (a) locate the imported field by id and read its `connection_type`, and (b) collect the entity's `KEY` field ids. If the imported field is a `FRAGMENT`, it seeds `key_field_ids` with all of the entity's `KEY` field ids. (A `KEY` field imports with `key_field_ids` null. If the id isn't in the list → error, mirroring `Read`'s not-found handling. A `ListFields` error → `AddError` + return, no partial seed.) The subsequent `Read` leaves the seeded value intact (set element order is irrelevant — state serializes sets canonically by value).
  - **Round-trip caveat (reconciles with the subset allowance):** import seeds **all** KEY ids, so `ImportStateVerify` passes cleanly only when the config references the **full** KEY set — i.e. it follows the documented "reference all keys" rule. A config that deliberately references a *strict subset* (permitted per the completeness-is-advisory rule below) will show a benign post-import cardinality diff, **not** an error. The acceptance test's import config references the full set, so it verifies with no `ImportStateVerifyIgnore`; a subset consumer who wants a clean import either references all keys or adds `ImportStateVerifyIgnore`.

### Apply-time validation (existence + KEY + same entity)

`ValidateConfig` (plan-time, no client, values may be unknown) can only enforce the *shape* rule above — present-for-FRAGMENT / absent-for-KEY. It cannot check that the supplied ids are real KEY fields, because for a fresh create the ids are unknown at plan and a resource has no read access to other resources' state.

`Create` and `Update` *can*: the graph edge guarantees the referenced KEY fields already exist, so their ids are known. Before the upsert, the provider calls `ListFields(project_id, entity_id)`, builds the set of the entity's `KEY` field ids, and verifies **every** supplied `key_field_ids` value is in it. A value that is missing (wrong entity, deleted field) or belongs to a non-KEY field → a clear diagnostic naming the offending id, e.g. *"key_field_ids: `<uuid>` is not a KEY field of entity `<entity_id>`"*, instead of a downstream backend 422 or a silently-misleading graph edge. One `ListFields` call per FRAGMENT create/update; entities carry few fields, so the cost is negligible.

This validates **membership** (each id is an existing KEY field of this entity), not **completeness** (that you listed *all* of them). Completeness stays documented guidance rather than a hard error: a fragment that references at least one KEY of its entity is already safe in both directions (the referenced key is always created before, and destroyed after, the fragment, so a fragment never outlives its last key), and hard-enforcing "all keys" would wrongly break configs when a new KEY is added to the entity later.

### Limitations (documented honestly)

- **In-place replacement of an entity's *sole* KEY field is not covered — and cannot be by this mechanism.** `name`/`data_type`/`connection_type` carry `RequiresReplace` (`field.go:81-110`). Terraform's default replace order is destroy-old-then-create-new, so replacing the only KEY of an entity that has FRAGMENT fields deletes the last KEY row while fragments still exist → the backend's delete-side 422. `key_field_ids` orders the *fragment's* create/destroy relative to the key; it cannot widen a *key's* replacement window. This is **pre-existing** (true today with a manual `depends_on` too), not introduced here — but it means the "safe in both directions" framing above holds for create and whole-entity/whole-fragment destroy, **not** for sole-KEY replace. Documented workarounds: add the replacement KEY under a **new resource address**, repoint the fragment's `key_field_ids`, then remove the old KEY as a separate step (always safe); `lifecycle { create_before_destroy = true }` helps only a pure rename and still collides with strict-Create's by-name existence check on a same-name replace. This goes in `docs/resources/field.md`.
- **Plan-time vs apply-time:** for a fresh create the id values are unknown at plan, so the membership validation runs at **apply**, not plan. `terraform plan` won't flag a bad id; `apply` will, before the write reaches the backend.
- **Completeness is advisory:** the provider does not force you to list every KEY field. The documented rule ("reference all KEY fields of the entity") is the simple, always-safe recipe (and the one import round-trips cleanly); referencing a strict subset is still ordering-correct but is the consumer's call.
- **Safety net, not the sole guard:** the backend enforces the KEY↔FRAGMENT invariant independently (it protects out-of-band/non-Terraform callers too). `key_field_ids` is an **apply-success / ergonomics** feature — it turns would-be failed applies and hand-written `depends_on` into automatic correct ordering — not the only thing preventing data corruption.

## LW-70 workaround cleanup (in scope)

The LW-70 fix is confirmed live in `../backend` (commit `24264c5`), so the pre-fix workarounds carried in this repo are now stale and are removed as part of this change. The full inventory (verified by `rg -i 'lw-70|depends_on|plantKeeper'` across the repo) — **surgical: only the LW-70 KEY→FRAGMENT `depends_on` edges and the keeper are touched; legitimate data-source-ordering `depends_on` (e.g. `depends_on = [leifwind_project.p]`, `[leifwind_entity.e]`) stays**:

- **`internal/metadatares/field.go:67-71`** — remove the `!> **Warning**` block in the resource `MarkdownDescription` (the "destroying all of an entity's fields 500s" warning); the zero-fields 500 is fixed.
- **`internal/acctest/field_acc_test.go`** — remove `plantKeeperField` (lines 57-93) and its step-level `Check` wiring at line 112 (there is **no** `CheckDestroy` in this file — don't go looking for one). Replace the LW-70 `depends_on = [leifwind_field.title]` in `fieldConfig` (line 52) with `key_field_ids = [leifwind_field.title.id]` (dogfood). Update the LW-70 comment block at lines 20-26/57-65.
- **`internal/acctest/fragments_ds_acc_test.go`** — replace the LW-70 `depends_on = [leifwind_field.title]` (line 58) with `key_field_ids`, remove the `plantKeeperField` call (line 74), and update the LW-70 doc-comment (lines 12-26). Keep the data-source `depends_on = [leifwind_field.a]` (line 64) — unrelated ordering.
- **`internal/acctest/entity_field_ds_acc_test.go`** — same treatment: LW-70 `depends_on` (line 61) → `key_field_ids`, remove `plantKeeperField` (line 98), update comments (lines 12-18) and the line-83 LW-70 deviation note. Keep the data-source `depends_on` edges (lines 71, 84).
- **`examples/resources/leifwind_field/resource.tf:17-21`** — replace the LW-70 `depends_on` + comment with `key_field_ids = [leifwind_field.title.id]`; this example feeds the generated `docs/resources/field.md`.
- **`README.md:79`** (root) and **`client/README.md:65-67`** — remove/replace the LW-70 "keep at least one field alive" note (two separate files).
- **`client/metadata_fields_test.go:85-97`** — update the comments describing the pre-fix `backend:edge` 500 behavior to reflect the enforced 422s. (The test body's delete-fragment-before-key order stays correct — it now satisfies the enforced rule rather than dodging a 500.)
- **generated `docs/resources/field.md`** — regenerated via `tfplugindocs` from the updated schema + example; shows `key_field_ids` and documents the rule + the sole-KEY-replace limitation.

**Commit split (per the "small atomic commits" rule).** These have two different triggers, so they go in separate commits: (1) *backend-fix-gated pure removals* — the `field.go` warning, `plantKeeperField`, the README notes, the test-comment fixes — depend only on the fix being live and are independent of whether `key_field_ids` ships; (2) *feature-coupled dogfood* — swapping the acctest/example `depends_on` for `key_field_ids` — is coupled to the attribute landing. Splitting means a feature revert doesn't drag out the safety-relevant removals, and vice versa.

Each removal is gated on the acceptance suite (which runs against the fixed backend via the `leifwindtest` fixture) staying green; anything that unexpectedly still fails is logged and kept rather than force-removed.

## Edits to the LW-43 design doc

`docs/superpowers/specs/2026-07-10-lw43-terraform-provider-design.md` is updated so it doesn't contradict this change:

- The `leifwind_field` resource row in the "Resources" table (line ~124) gains `key_field_ids` (updatable/no-replace, config-only).
- The `ValidateConfig` bullet (line ~127) gains the `key_field_ids` rule alongside the `fragment_name` rule.

(The LW-43 doc contains **no** LW-70 / 500 / workaround references to update — verified by grep; the only `500` hit is an unrelated toxiproxy/HTTP-5xx retry note. So only the two edits above apply.)

## Test strategy

Consistent with LW-43's blackbox-through-the-stack approach:

1. **Pure in-process (`ValidateConfig`)** — table test: FRAGMENT without `key_field_ids` → error; FRAGMENT with non-empty `key_field_ids` → ok; KEY with `key_field_ids` → error; KEY without → ok; unknown `connection_type`/`key_field_ids` → no error (deferred).
2. **Apply-time membership validation (acceptance)** — a FRAGMENT field whose `key_field_ids` references a **non-KEY** field of the entity (e.g. another FRAGMENT field's id) → `Create` fails with the provider diagnostic naming the offending id (asserted via `ExpectError`). A reference to a **KEY** field of the entity passes.
3. **Acceptance lifecycle (`TF_ACC`, real `tofu` + `leifwindtest` fixture)** — the FRAGMENT field is ordered after the KEY field **solely** via `key_field_ids` (no `depends_on` in the field config): create → read → update `fragment_name` in place → import → destroy, all green against the fixed backend. Destroy exercises the FRAGMENT-before-last-KEY ordering that the backend now enforces.
   - **Import must target the FRAGMENT field (`leifwind_field.body`), not the KEY (`title`), to be meaningful.** The existing test imports `leifwind_field.title` (`field_acc_test.go:126`), whose `key_field_ids` is null before *and* after import — so it would pass **vacuously** regardless of whether server-seeding works. Add/switch to importing `body` so `ImportStateVerify` actually exercises the `ListFields` seed of `key_field_ids` (plain `ImportStateVerify`, no ignore, since the config references the full KEY set).
4. **(Optional) Multi-KEY subset round-trip** — one case with two KEY fields where the FRAGMENT references only one: confirm the documented behavior — apply/destroy still ordering-correct, and a post-import `ImportStateVerify` shows a benign cardinality diff (seed-all vs reference-one) rather than an error. Guards the "completeness is advisory" + import-caveat claims from silently regressing.

## Deliverables

- This spec.
- `key_field_ids` attribute in `internal/metadatares/field.go`: schema (Optional set(string), no RequiresReplace) + `ValidateConfig` shape rule **with the mandatory `KeyFieldIDs.IsUnknown()` guard evaluated first**, `Create`/`Update` apply-time membership validation via `ListFields` (run *before* `UpsertField`; error → return, no partial write), and `ImportState` server-seeding for FRAGMENT fields (single `ListFields` call; error → return).
- LW-70 workaround cleanup across the full inventory above: `field.go`, `field_acc_test.go`, `fragments_ds_acc_test.go`, `entity_field_ds_acc_test.go`, `examples/resources/leifwind_field/resource.tf`, root `README.md`, `client/README.md`, `client/metadata_fields_test.go` — in two commits (backend-fix-gated removals vs feature-coupled dogfood).
- Example + regenerated `docs/resources/field.md` for `leifwind_field` (incl. the sole-KEY-replace limitation note).
- LW-43 design-doc edits (two, listed above).
- Unit + acceptance tests as above, including the FRAGMENT-field import.

## Definition of done

- `ValidateConfig` unit tests pass (incl. the fresh-FRAGMENT-with-reference case that must **not** error); acceptance lifecycle green against the fixed backend with **no LW-70 KEY→FRAGMENT `depends_on`** left in any field/fragment config (legitimate data-source `depends_on` may remain); the meaningful FRAGMENT-field import verifies clean.
- No LW-70 KEY→FRAGMENT `depends_on`, no `plantKeeperField`, and no stale LW-70 500/"keep a field alive" text remains anywhere in the repo (`rg -i 'lw-70|plantKeeper'` returns only historical spec/changelog references, if any).
- Examples and regenerated `docs/resources/field.md` show and explain `key_field_ids` and document the sole-KEY-replace limitation.
- Small atomic conventional commits on branch `feature/lw-86-...` (cleanup split by trigger as above); strict TDD.
