// SPDX-License-Identifier: MPL-2.0

package metadatads

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"gitlab.com/leifwind/stream/terraform-provider-leifwind/client"
)

var (
	_ datasource.DataSource              = (*fragmentsDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*fragmentsDataSource)(nil)
)

// NewEntityFragmentsDataSource registers data.leifwind_entity_fragments.
func NewEntityFragmentsDataSource() datasource.DataSource { return &fragmentsDataSource{} }

type fragmentsDataSource struct {
	c *client.Client
}

type fragmentsDSModel struct {
	ProjectID  types.String `tfsdk:"project_id"`
	EntityName types.String `tfsdk:"entity_name"`
	Fragments  types.List   `tfsdk:"fragments"`
}

func (d *fragmentsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_entity_fragments"
}

func (d *fragmentsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Read-only fragment names of an entity (derived from its FRAGMENT-connection fields).",
		Attributes: map[string]schema.Attribute{
			"project_id":  schema.StringAttribute{Required: true, Description: "Project id (UUID)."},
			"entity_name": schema.StringAttribute{Required: true, Description: "Entity name (or UUID string)."},
			"fragments":   schema.ListAttribute{Computed: true, ElementType: types.StringType},
		},
	}
}

func (d *fragmentsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *fragmentsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var cfg fragmentsDSModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	pid, err := uuid.Parse(cfg.ProjectID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Invalid project_id", err.Error())
		return
	}
	frags, err := d.c.Generic.ListEntityFragments(ctx, pid, cfg.EntityName.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Listing fragments failed", err.Error())
		return
	}
	list, diags := types.ListValueFrom(ctx, types.StringType, frags)
	resp.Diagnostics.Append(diags...)
	cfg.Fragments = list
	resp.Diagnostics.Append(resp.State.Set(ctx, cfg)...)
}
