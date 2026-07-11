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
	"gitlab.com/leifwind/stream/terraform-provider-leifwind/internal/lookup"
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
	ID        types.String `tfsdk:"id"`
	Name      types.String `tfsdk:"name"`
	UniqueKey types.String `tfsdk:"unique_key"`
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
			"unique_key": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Server-computed natural key (for projects: equals the name).",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
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
	existing, err := lookup.ProjectByName(ctx, r.c, name)
	if err != nil {
		resp.Diagnostics.AddError("Checking for existing project failed", err.Error())
		return
	}
	if existing != nil {
		// NOTE: keep "already exists" and "terraform import" close together
		// (no long token, e.g. a UUID, between them) — the CLI diagnostic
		// renderer word-wraps long detail text, and a wrapped-in newline
		// would break `regexp` (default, non-DOTALL) matches such as
		// `already exists.*terraform import` used by acceptance tests.
		resp.Diagnostics.AddError(
			"Project already exists",
			fmt.Sprintf("project %q already exists — terraform import leifwind_project.<name> %s (object_id %s)",
				name, existing.ObjectID, existing.ObjectID))
		return
	}

	created, err := r.c.Metadata.UpsertProject(ctx, client.MetadataProject{Name: name})
	if err != nil {
		resp.Diagnostics.AddError("Creating project failed", err.Error())
		return
	}
	plan.ID = types.StringValue(created.ObjectID.String())
	plan.UniqueKey = types.StringValue(created.UniqueKey)
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
	state.UniqueKey = types.StringValue(p.UniqueKey)
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
