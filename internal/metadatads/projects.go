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

// projectSummary is the per-item shape of leifwind_projects' "projects" list,
// per the task-23 brief: {id, name} (no unique_key — the standing "include
// unique_key" deviation applies to the singular leifwind_project data source
// only, not this listing).
type projectSummary struct {
	ID   types.String `tfsdk:"id"`
	Name types.String `tfsdk:"name"`
}

type projectsDSModel struct {
	Pattern  types.String     `tfsdk:"pattern"`
	Projects []projectSummary `tfsdk:"projects"`
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
	cfg.Projects = []projectSummary{}
	for p, err := range d.c.Metadata.IterProjects(ctx, client.ListOpts{Pattern: cfg.Pattern.ValueString()}) {
		if err != nil {
			resp.Diagnostics.AddError("Listing projects failed", err.Error())
			return
		}
		cfg.Projects = append(cfg.Projects, projectSummary{
			ID:   types.StringValue(p.ObjectID.String()),
			Name: types.StringValue(p.Name),
		})
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, cfg)...)
}
