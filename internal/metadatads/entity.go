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
	_ datasource.DataSource                     = (*entityDataSource)(nil)
	_ datasource.DataSourceWithConfigure        = (*entityDataSource)(nil)
	_ datasource.DataSourceWithConfigValidators = (*entityDataSource)(nil)
)

// NewEntityDataSource registers data.leifwind_entity.
func NewEntityDataSource() datasource.DataSource { return &entityDataSource{} }

type entityDataSource struct {
	c *client.Client
}

// entityDSModel is the leifwind_entity (singular) data source model.
//
// Deviation from the task-24 brief (owner-adjudicated, "STANDING ADDITION"):
// the brief's schema only listed id/project_id/name. unique_key is added
// here as a Computed attribute, mirroring the leifwind_entity resource
// which already exposes it.
type entityDSModel struct {
	ID        types.String `tfsdk:"id"`
	ProjectID types.String `tfsdk:"project_id"`
	Name      types.String `tfsdk:"name"`
	UniqueKey types.String `tfsdk:"unique_key"`
}

func (d *entityDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_entity"
}

func (d *entityDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Look up a single entity by id or exact name within a project.",
		Attributes: map[string]schema.Attribute{
			"id":         schema.StringAttribute{Optional: true, Computed: true, Description: "Entity object id (UUID)."},
			"project_id": schema.StringAttribute{Required: true, Description: "Owning project id (UUID)."},
			"name":       schema.StringAttribute{Optional: true, Computed: true, Description: "Exact entity name."},
			"unique_key": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Server-computed natural key.",
			},
		},
	}
}

func (d *entityDataSource) ConfigValidators(context.Context) []datasource.ConfigValidator {
	return []datasource.ConfigValidator{
		datasourcevalidator.ExactlyOneOf(path.MatchRoot("id"), path.MatchRoot("name")),
	}
}

func (d *entityDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *entityDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var cfg entityDSModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	pid, err := uuid.Parse(cfg.ProjectID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Invalid project_id", err.Error())
		return
	}
	var e *client.MetadataEntity
	if !cfg.ID.IsNull() {
		id, err := uuid.Parse(cfg.ID.ValueString())
		if err != nil {
			resp.Diagnostics.AddError("Invalid id", err.Error())
			return
		}
		got, err := d.c.Metadata.GetEntity(ctx, pid, id)
		if err != nil {
			resp.Diagnostics.AddError("Entity not found", err.Error())
			return
		}
		e = &got
	} else {
		got, err := lookup.EntityByName(ctx, d.c, pid, cfg.Name.ValueString())
		if err != nil {
			resp.Diagnostics.AddError("Entity lookup failed", err.Error())
			return
		}
		if got == nil {
			resp.Diagnostics.AddError("Entity not found", fmt.Sprintf("no entity named %q", cfg.Name.ValueString()))
			return
		}
		e = got
	}
	// keep the config values for id (when set) and project_id: server
	// lowercases UUIDs and these are immutable inputs (id is populated only
	// on the by-name path)
	if cfg.ID.IsNull() {
		cfg.ID = types.StringValue(e.ObjectID.String())
	}
	cfg.Name = types.StringValue(e.Name)
	cfg.UniqueKey = types.StringValue(e.UniqueKey)
	resp.Diagnostics.Append(resp.State.Set(ctx, cfg)...)
}
