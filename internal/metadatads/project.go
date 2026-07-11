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

// projectDSModel is the leifwind_project (singular) data source model.
//
// Deviation from the task-23 brief (owner-adjudicated, "STANDING ADDITION"):
// the brief's schema only listed id/name. unique_key is added here as a
// Computed attribute, mirroring the leifwind_project resource which already
// exposes it.
type projectDSModel struct {
	ID        types.String `tfsdk:"id"`
	Name      types.String `tfsdk:"name"`
	UniqueKey types.String `tfsdk:"unique_key"`
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
			"unique_key": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Server-computed natural key (for projects: equals the name).",
			},
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
	cfg.UniqueKey = types.StringValue(p.UniqueKey)
	resp.Diagnostics.Append(resp.State.Set(ctx, cfg)...)
}
