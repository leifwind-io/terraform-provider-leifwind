# Ledger: full-repository review — terraform-provider-leifwind

- **Date:** 2026-07-14
- **Reviewed revision:** `ae04371` (main) — clean worktree state; the user's
  uncommitted local changes (README.md, client/metadata_fields_test.go,
  docs/resources/project.md, internal/metadatares/project.go) are NOT part of
  this review.
- **Method:** dynamic Workflow. One independent Opus reviewer per review unit
  (98 units, 107 tracked files; lockfiles paired with their go.mod, example
  dirs grouped). Per-area Fable meta-reviewer verifies/dedupes each area's
  file reviews. Final synthesis by the session (Fable).
- **Severity rubric:** Critical (must fix) / Important (should fix) / Minor
  (nice to have) — superpowers requesting-code-review calibration.
- **Outputs:** report.md (this dir), Linear backlog items for actionable
  findings, Notion page for the decision/documentation record.
- **Recovery:** workflow runId + journal.jsonl under the session transcript
  dir are the source of truth for agent results; this ledger is the
  human-readable record. Trust ledger + journal over recollection after
  compaction.

- **Workflow run:** `wf_386951e1-03f` (task wej6vcdb9; first attempt
  `wf_e98061f3-a6f` failed at t=0 — args did not reach the script, units now
  inlined in the script file)

## Status

- [x] Inventory (98 units, 9 areas)
- [x] File reviews (Opus, per unit) — 98/98 complete, 0 errors
- [x] Area meta-reviews (Fable, per area) — 9/9 complete; 56 findings confirmed (0 critical / 6 important / 50 minor), 21 file-review claims refuted
- [ ] Synthesis + report
- [ ] Linear backlog items
- [ ] Notion documentation page

## Review units

### acctest (11 units)

- [x] internal-acctest-acctest-go: internal/acctest/acctest.go — clean
- [x] internal-acctest-auth_negative_acc_test-go: internal/acctest/auth_negative_acc_test.go — 1 finding(s)
- [x] internal-acctest-auth_paths_acc_test-go: internal/acctest/auth_paths_acc_test.go — clean
- [x] internal-acctest-entity_acc_test-go: internal/acctest/entity_acc_test.go — 2 finding(s)
- [x] internal-acctest-entity_field_ds_acc_test-go: internal/acctest/entity_field_ds_acc_test.go — clean
- [x] internal-acctest-field_acc_test-go: internal/acctest/field_acc_test.go — 1 finding(s)
- [x] internal-acctest-field_from_ds_acc_test-go: internal/acctest/field_from_ds_acc_test.go — clean
- [x] internal-acctest-fragments_ds_acc_test-go: internal/acctest/fragments_ds_acc_test.go — clean
- [x] internal-acctest-main_test-go: internal/acctest/main_test.go — clean
- [x] internal-acctest-project_acc_test-go: internal/acctest/project_acc_test.go — 2 finding(s)
- [x] internal-acctest-project_ds_acc_test-go: internal/acctest/project_ds_acc_test.go — 3 finding(s)

### build-ci (8 units)

- [x] -gitignore: .gitignore — clean
- [x] -gitlab-ci-yml: .gitlab-ci.yml — 5 finding(s)
- [x] -golangci-yml: .golangci.yml — 3 finding(s)
- [x] -goreleaser-yml: .goreleaser.yml — 2 finding(s)
- [x] -pre-commit-config-yaml: .pre-commit-config.yaml — 2 finding(s)
- [x] LICENSE: LICENSE — clean
- [x] Makefile: Makefile — 3 finding(s)
- [x] terraform-registry-manifest-json: terraform-registry-manifest.json — clean

### client (21 units)

- [x] client-gomod: client/go.mod, client/go.sum — clean
- [x] client-README-md: client/README.md — clean
- [x] client-auth-go: client/auth.go — 2 finding(s)
- [x] client-auth_test-go: client/auth_test.go — clean
- [x] client-client-go: client/client.go — 4 finding(s)
- [x] client-client_test-go: client/client_test.go — 2 finding(s)
- [x] client-doc-go: client/doc.go — clean
- [x] client-errors-go: client/errors.go — clean
- [x] client-errors_test-go: client/errors_test.go — clean
- [x] client-generic-go: client/generic.go — clean
- [x] client-generic_test-go: client/generic_test.go — clean
- [x] client-metadata-go: client/metadata.go — clean
- [x] client-metadata_entities_test-go: client/metadata_entities_test.go — clean
- [x] client-metadata_fields_test-go: client/metadata_fields_test.go — 1 finding(s)
- [x] client-metadata_projects_test-go: client/metadata_projects_test.go — clean
- [x] client-models-go: client/models.go — 3 finding(s)
- [x] client-models_test-go: client/models_test.go — clean
- [x] client-opts-go: client/opts.go — clean
- [x] client-opts_test-go: client/opts_test.go — clean
- [x] client-retry-go: client/retry.go — 2 finding(s)
- [x] client-retry_test-go: client/retry_test.go — 3 finding(s)

### datasources (8 units)

- [x] internal-metadatads-doc-go: internal/metadatads/doc.go — clean
- [x] internal-metadatads-entities-go: internal/metadatads/entities.go — 2 finding(s)
- [x] internal-metadatads-entity-go: internal/metadatads/entity.go — clean
- [x] internal-metadatads-field-go: internal/metadatads/field.go — clean
- [x] internal-metadatads-fields-go: internal/metadatads/fields.go — clean
- [x] internal-metadatads-fragments-go: internal/metadatads/fragments.go — 2 finding(s)
- [x] internal-metadatads-project-go: internal/metadatads/project.go — clean
- [x] internal-metadatads-projects-go: internal/metadatads/projects.go — clean

### docs-examples (23 units)

- [x] ex-leifwind_entities-ds: examples/data-sources/leifwind_entities/data-source.tf — clean
- [x] ex-leifwind_entity-ds: examples/data-sources/leifwind_entity/data-source.tf — clean
- [x] ex-leifwind_entity_fragments-ds: examples/data-sources/leifwind_entity_fragments/data-source.tf — clean
- [x] ex-leifwind_field-ds: examples/data-sources/leifwind_field/data-source.tf — clean
- [x] ex-leifwind_fields-ds: examples/data-sources/leifwind_fields/data-source.tf — clean
- [x] ex-leifwind_project-ds: examples/data-sources/leifwind_project/data-source.tf — clean
- [x] ex-leifwind_projects-ds: examples/data-sources/leifwind_projects/data-source.tf — clean
- [x] ex-provider: examples/provider/provider.tf — clean
- [x] ex-leifwind_entity: examples/resources/leifwind_entity/import.sh, examples/resources/leifwind_entity/resource.tf — clean
- [x] ex-leifwind_field: examples/resources/leifwind_field/import.sh, examples/resources/leifwind_field/resource.tf — clean
- [x] ex-leifwind_project: examples/resources/leifwind_project/import.sh, examples/resources/leifwind_project/resource.tf — clean
- [x] README-md: README.md — clean
- [x] docs-data-sources-entities-md: docs/data-sources/entities.md — clean
- [x] docs-data-sources-entity-md: docs/data-sources/entity.md — 1 finding(s)
- [x] docs-data-sources-entity_fragments-md: docs/data-sources/entity_fragments.md — 1 finding(s)
- [x] docs-data-sources-field-md: docs/data-sources/field.md — 1 finding(s)
- [x] docs-data-sources-fields-md: docs/data-sources/fields.md — clean
- [x] docs-data-sources-project-md: docs/data-sources/project.md — clean
- [x] docs-data-sources-projects-md: docs/data-sources/projects.md — clean
- [x] docs-index-md: docs/index.md — clean
- [x] docs-resources-entity-md: docs/resources/entity.md — clean
- [x] docs-resources-field-md: docs/resources/field.md — 1 finding(s)
- [x] docs-resources-project-md: docs/resources/project.md — clean

### leifwindtest (12 units)

- [x] client-leifwindtest-backend-go: client/leifwindtest/backend.go — clean
- [x] client-leifwindtest-forged-go: client/leifwindtest/forged.go — clean
- [x] client-leifwindtest-main_test-go: client/leifwindtest/main_test.go — clean
- [x] client-leifwindtest-oidc_settings-go: client/leifwindtest/oidc_settings.go — 1 finding(s)
- [x] client-leifwindtest-org-go: client/leifwindtest/org.go — clean
- [x] client-leifwindtest-org_test-go: client/leifwindtest/org_test.go — 2 finding(s)
- [x] client-leifwindtest-stack-go: client/leifwindtest/stack.go — 2 finding(s)
- [x] client-leifwindtest-stack_test-go: client/leifwindtest/stack_test.go — clean
- [x] client-leifwindtest-toxiproxy-go: client/leifwindtest/toxiproxy.go — 3 finding(s)
- [x] client-leifwindtest-usertoken-go: client/leifwindtest/usertoken.go — 3 finding(s)
- [x] client-leifwindtest-usertoken_test-go: client/leifwindtest/usertoken_test.go — 2 finding(s)
- [x] client-leifwindtest-zitadel-go: client/leifwindtest/zitadel.go — 3 finding(s)

### planning (1 units)

- [x] planning-docs: docs/superpowers/plans/2026-07-10-lw43-terraform-provider-leifwind.md, docs/superpowers/plans/2026-07-11-lw86-key-field-ids.md, docs/superpowers/specs/2026-07-10-lw43-terraform-provider-design.md, docs/superpowers/specs/2026-07-11-lw70-fragment-key-ordering-design.md — 3 finding(s)

### provider-core (7 units)

- [x] root-gomod: go.mod, go.sum — 1 finding(s)
- [x] go-work: go.work, go.work.sum — clean
- [x] internal-lookup-lookup-go: internal/lookup/lookup.go — clean
- [x] internal-provider-config-go: internal/provider/config.go — 2 finding(s)
- [x] internal-provider-config_test-go: internal/provider/config_test.go — 3 finding(s)
- [x] internal-provider-provider-go: internal/provider/provider.go — clean
- [x] main-go: main.go — 1 finding(s)

### resources (7 units)

- [x] internal-metadatares-doc-go: internal/metadatares/doc.go — clean
- [x] internal-metadatares-entity-go: internal/metadatares/entity.go — clean
- [x] internal-metadatares-field-go: internal/metadatares/field.go — clean
- [x] internal-metadatares-field_test-go: internal/metadatares/field_test.go — clean
- [x] internal-metadatares-import-go: internal/metadatares/import.go — clean
- [x] internal-metadatares-import_test-go: internal/metadatares/import_test.go — clean
- [x] internal-metadatares-project-go: internal/metadatares/project.go — clean