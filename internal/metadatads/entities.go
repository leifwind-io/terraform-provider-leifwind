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
	_ datasource.DataSource              = (*entitiesDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*entitiesDataSource)(nil)
)

// NewEntitiesDataSource registers data.leifwind_entities.
func NewEntitiesDataSource() datasource.DataSource { return &entitiesDataSource{} }

type entitiesDataSource struct {
	c *client.Client
}

// entityItemModel is the per-item shape of leifwind_entities' "entities"
// list, per the task-24 brief: {id, name} (no unique_key — the standing
// "include unique_key" deviation applies to the singular leifwind_entity
// data source only, not this listing).
type entityItemModel struct {
	ID   types.String `tfsdk:"id"`
	Name types.String `tfsdk:"name"`
}

type entitiesDSModel struct {
	ProjectID types.String      `tfsdk:"project_id"`
	Pattern   types.String      `tfsdk:"pattern"`
	Entities  []entityItemModel `tfsdk:"entities"`
}

func (d *entitiesDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_entities"
}

func (d *entitiesDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "List entities in a project, optionally filtered by a name substring pattern.",
		Attributes: map[string]schema.Attribute{
			"project_id": schema.StringAttribute{Required: true, Description: "Owning project id (UUID)."},
			"pattern":    schema.StringAttribute{Optional: true, Description: "Substring filter on the name (server-side ILIKE)."},
			"entities": schema.ListNestedAttribute{
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

func (d *entitiesDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *entitiesDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var cfg entitiesDSModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	pid, err := uuid.Parse(cfg.ProjectID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Invalid project_id", err.Error())
		return
	}
	cfg.Entities = []entityItemModel{}
	for e, err := range d.c.Metadata.IterEntities(ctx, pid, client.ListOpts{Pattern: cfg.Pattern.ValueString()}) {
		if err != nil {
			resp.Diagnostics.AddError("Listing entities failed", err.Error())
			return
		}
		cfg.Entities = append(cfg.Entities, entityItemModel{
			ID:   types.StringValue(e.ObjectID.String()),
			Name: types.StringValue(e.Name),
		})
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, cfg)...)
}
