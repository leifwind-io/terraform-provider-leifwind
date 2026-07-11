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
	UniqueKey types.String `tfsdk:"unique_key"`
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

	// strict create: the backend POST is create-or-adopt; Terraform's
	// contract is that Create fails on pre-existing objects.
	existing, err := findEntityByName(ctx, r.c, pid, name)
	if err != nil {
		resp.Diagnostics.AddError("Checking for existing entity failed", err.Error())
		return
	}
	if existing != nil {
		// NOTE: keep "already exists" and "terraform import" close together
		// (no long token, e.g. a UUID, between them) — the CLI diagnostic
		// renderer word-wraps long detail text, and a wrapped-in newline
		// would break `regexp` (default, non-DOTALL) matches such as
		// `already exists.*terraform import` used by acceptance tests.
		resp.Diagnostics.AddError(
			"Entity already exists",
			fmt.Sprintf("entity %q already exists — terraform import leifwind_entity.<name> %s/%s (project %s, object_id %s)",
				name, pid, existing.ObjectID, pid, existing.ObjectID))
		return
	}

	created, err := r.c.Metadata.UpsertEntity(ctx, client.MetadataEntity{ProjectID: pid, Name: name})
	if err != nil {
		resp.Diagnostics.AddError("Creating entity failed", err.Error())
		return
	}
	plan.ID = types.StringValue(created.ObjectID.String())
	plan.UniqueKey = types.StringValue(created.UniqueKey)
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
		resp.State.RemoveResource(ctx) // drift or cross-project: recreate on next apply
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Reading entity failed", err.Error())
		return
	}
	state.Name = types.StringValue(e.Name)
	state.ProjectID = types.StringValue(e.ProjectID.String())
	state.UniqueKey = types.StringValue(e.UniqueKey)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *entityResource) Update(_ context.Context, _ resource.UpdateRequest, resp *resource.UpdateResponse) {
	// unreachable: every attribute is Computed or RequiresReplace
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
