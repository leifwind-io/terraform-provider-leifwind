// SPDX-License-Identifier: MPL-2.0

package metadatares

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
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
// unknown elements). Used for both apply-time validation and the wire path.
//
//nolint:unused // consumed starting Task 2 (LW-86 plan); introduced now per task-1 brief interface contract
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
