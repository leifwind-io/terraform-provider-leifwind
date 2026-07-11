// SPDX-License-Identifier: MPL-2.0

package metadatares

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"gitlab.com/leifwind/stream/terraform-provider-leifwind/client"
	"gitlab.com/leifwind/stream/terraform-provider-leifwind/internal/lookup"
)

var (
	_ resource.Resource                   = (*fieldResource)(nil)
	_ resource.ResourceWithConfigure      = (*fieldResource)(nil)
	_ resource.ResourceWithImportState    = (*fieldResource)(nil)
	_ resource.ResourceWithValidateConfig = (*fieldResource)(nil)
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
	KeyFieldIDs    types.Set    `tfsdk:"key_field_ids"`
	UniqueKey      types.String `tfsdk:"unique_key"`
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
// unknown elements).
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

// keyFieldIDStrings returns the object ids (as strings) of the KEY fields in
// fields, skipping any with a nil ObjectID.
func keyFieldIDStrings(fields []client.MetadataField) []string {
	var out []string
	for _, ef := range fields {
		if ef.Connection.Type == client.ConnectionKey && ef.ObjectID != nil {
			out = append(out, ef.ObjectID.String())
		}
	}
	return out
}

func (r *fieldResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_field"
}

func (r *fieldResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A leifwind metadata field. Only fragment_name is updatable in place; every other attribute forces replacement.\n\n" +
			"FRAGMENT fields require a sibling KEY field on the entity (backend-enforced). Set `key_field_ids` " +
			"to the entity's KEY field ids so Terraform orders creation and destruction correctly. See the " +
			"`key_field_ids` attribute for the one case this does not cover (in-place replacement of an entity's " +
			"sole KEY field).",
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
			"key_field_ids": schema.SetAttribute{
				Optional:    true,
				ElementType: types.StringType,
				MarkdownDescription: "Ordering hint (config-only; never sent to the API). The object ids of " +
					"this entity's KEY fields, e.g. `[leifwind_field.title.id]`. **Required for FRAGMENT fields, " +
					"forbidden for KEY fields.** The backend requires a KEY field before FRAGMENT fields exist; " +
					"referencing the KEY field ids here makes Terraform create the KEY first and destroy it last, " +
					"without a manual `depends_on`. Reference **all** of the entity's KEY fields.",
			},
			"unique_key": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Server-computed natural key.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}

func (r *fieldResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var cfg fieldModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() || cfg.ConnectionType.IsUnknown() {
		return
	}
	if !cfg.FragmentName.IsUnknown() {
		if msg := validateFieldCombination(cfg.ConnectionType.ValueString(),
			cfg.FragmentName.ValueString(), !cfg.FragmentName.IsNull()); msg != "" {
			resp.Diagnostics.AddAttributeError(path.Root("fragment_name"), "Invalid field configuration", msg)
		}
	}
	if !cfg.KeyFieldIDs.IsUnknown() {
		// NOTE: deliberately Elements() here, not setToStrings(cfg.KeyFieldIDs).
		// A known set can still contain an individually-unknown element (e.g.
		// key_field_ids = [leifwind_field.title.id] before title is created);
		// setToStrings filters that element out and would misreport the set as
		// empty, producing a spurious "required" error on first apply. Raw
		// element count treats "present but unresolved" as present; the real
		// membership/emptiness check happens at apply time in
		// validateKeyFieldMembership once every id is resolved.
		if msg := validateKeyFieldIDsCombination(cfg.ConnectionType.ValueString(),
			!cfg.KeyFieldIDs.IsNull(), len(cfg.KeyFieldIDs.Elements()) == 0); msg != "" {
			resp.Diagnostics.AddAttributeError(path.Root("key_field_ids"), "Invalid field configuration", msg)
		}
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

// modelFromClient copies server-computed attributes from f into m.
// project_id and entity_id are deliberately NOT copied — m keeps the prior
// plan/state values (import sets them explicitly before Read, so this holds
// there too): server lowercases UUIDs and these are immutable inputs.
func (r *fieldResource) modelFromClient(f client.MetadataField, m *fieldModel) {
	m.ID = types.StringValue(f.ObjectID.String())
	m.Name = types.StringValue(f.Name)
	m.DataType = types.StringValue(string(f.Config.DataType))
	m.ConnectionType = types.StringValue(string(f.Connection.Type))
	if f.Connection.Type == client.ConnectionFragment {
		m.FragmentName = types.StringValue(f.Connection.FragmentName)
	} else {
		m.FragmentName = types.StringNull()
	}
	m.UniqueKey = types.StringValue(f.UniqueKey)
}

// validateKeyFieldMembership enforces the key_field_ids rules at apply time,
// appending diagnostics. FRAGMENT fields must reference a non-empty set, and
// every referenced id must be a KEY field of the same entity; KEY fields must
// not set it. A lookup failure is surfaced as a plain error (not attributed to
// key_field_ids). The graph edge guarantees the referenced KEY fields already
// exist by the time this runs.
func (r *fieldResource) validateKeyFieldMembership(ctx context.Context, plan fieldModel, f client.MetadataField, diags *diag.Diagnostics) {
	supplied := setToStrings(plan.KeyFieldIDs)
	if f.Connection.Type != client.ConnectionFragment {
		if len(supplied) > 0 {
			diags.AddAttributeError(path.Root("key_field_ids"), "Invalid key_field_ids",
				fmt.Sprintf("key_field_ids must not be set when connection_type is %q", f.Connection.Type))
		}
		return
	}
	if len(supplied) == 0 {
		diags.AddAttributeError(path.Root("key_field_ids"), "Invalid key_field_ids",
			"key_field_ids is required when connection_type is \"FRAGMENT\" (reference the entity's KEY field ids)")
		return
	}
	fields, err := lookup.EntityFields(ctx, r.c, f.ProjectID, f.EntityID)
	if err != nil {
		diags.AddError("Listing entity fields failed", err.Error())
		return
	}
	keyIDs := make(map[string]struct{})
	for _, id := range keyFieldIDStrings(fields) {
		keyIDs[id] = struct{}{}
	}
	if missing := missingKeyFieldIDs(supplied, keyIDs); len(missing) > 0 {
		diags.AddAttributeError(path.Root("key_field_ids"), "Invalid key_field_ids",
			fmt.Sprintf("key_field_ids: %v are not KEY fields of entity %s (reference the entity's KEY field ids)", missing, f.EntityID))
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

	// strict create: the backend POST is create-or-adopt; Terraform's
	// contract is that Create fails on pre-existing objects.
	existing, err := lookup.FieldByName(ctx, r.c, f.ProjectID, f.EntityID, f.Name)
	if err != nil {
		resp.Diagnostics.AddError("Checking for existing field failed", err.Error())
		return
	}
	if existing != nil {
		// NOTE: keep "already exists" and "terraform import" close together
		// (no long token, e.g. a UUID, between them) — the CLI diagnostic
		// renderer word-wraps long detail text, and a wrapped-in newline
		// would break `regexp` (default, non-DOTALL) matches such as
		// `already exists.*terraform import` used by acceptance tests.
		resp.Diagnostics.AddError(
			"Field already exists",
			fmt.Sprintf("field %q already exists — terraform import leifwind_field.<name> %s/%s/%s (object_id %s)",
				f.Name, f.ProjectID, f.EntityID, existing.ObjectID, existing.ObjectID))
		return
	}

	r.validateKeyFieldMembership(ctx, plan, f, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
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
		resp.State.RemoveResource(ctx) // drift or cross-entity: recreate on next apply
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
	r.validateKeyFieldMembership(ctx, plan, f, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
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
	for i := range fields {
		if fields[i].ObjectID != nil && *fields[i].ObjectID == ids[2] {
			self = &fields[i]
			break
		}
	}
	if self == nil {
		resp.Diagnostics.AddError("Field not found for import",
			fmt.Sprintf("no field %s on entity %s", ids[2], ids[1]))
		return
	}
	if self.Connection.Type == client.ConnectionFragment {
		keyStrs := keyFieldIDStrings(fields)
		keyElems := make([]attr.Value, 0, len(keyStrs))
		for _, id := range keyStrs {
			keyElems = append(keyElems, types.StringValue(id))
		}
		set, d := types.SetValue(types.StringType, keyElems)
		resp.Diagnostics.Append(d...)
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("key_field_ids"), set)...)
	}
}
