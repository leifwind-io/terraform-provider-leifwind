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
	_ datasource.DataSource                     = (*fieldDataSource)(nil)
	_ datasource.DataSourceWithConfigure        = (*fieldDataSource)(nil)
	_ datasource.DataSourceWithConfigValidators = (*fieldDataSource)(nil)
)

// NewFieldDataSource registers data.leifwind_field.
func NewFieldDataSource() datasource.DataSource { return &fieldDataSource{} }

type fieldDataSource struct {
	c *client.Client
}

// fieldDSModel is the leifwind_field (singular) data source model.
//
// Deviation from the task-24 brief (owner-adjudicated, "STANDING ADDITION"):
// the brief's schema only listed id/project_id/entity_id/name plus the
// three Computed-only attrs (data_type/connection_type/fragment_name).
// unique_key is added here as a Computed attribute, mirroring the
// leifwind_field resource which already exposes it.
type fieldDSModel struct {
	ID             types.String `tfsdk:"id"`
	ProjectID      types.String `tfsdk:"project_id"`
	EntityID       types.String `tfsdk:"entity_id"`
	Name           types.String `tfsdk:"name"`
	DataType       types.String `tfsdk:"data_type"`
	ConnectionType types.String `tfsdk:"connection_type"`
	FragmentName   types.String `tfsdk:"fragment_name"`
	UniqueKey      types.String `tfsdk:"unique_key"`
}

func (d *fieldDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_field"
}

func (d *fieldDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Look up a single field by id or exact name within a project/entity.",
		Attributes: map[string]schema.Attribute{
			"id":              schema.StringAttribute{Optional: true, Computed: true, Description: "Field object id (UUID)."},
			"project_id":      schema.StringAttribute{Required: true, Description: "Owning project id (UUID)."},
			"entity_id":       schema.StringAttribute{Required: true, Description: "Owning entity id (UUID)."},
			"name":            schema.StringAttribute{Optional: true, Computed: true, Description: "Exact field name."},
			"data_type":       schema.StringAttribute{Computed: true, Description: "Data type."},
			"connection_type": schema.StringAttribute{Computed: true, Description: "Connection type."},
			"fragment_name":   schema.StringAttribute{Computed: true, Description: "Fragment the field belongs to (FRAGMENT connection only)."},
			"unique_key": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Server-computed natural key.",
			},
		},
	}
}

func (d *fieldDataSource) ConfigValidators(context.Context) []datasource.ConfigValidator {
	return []datasource.ConfigValidator{
		datasourcevalidator.ExactlyOneOf(path.MatchRoot("id"), path.MatchRoot("name")),
	}
}

func (d *fieldDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *fieldDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var cfg fieldDSModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	pid, err := uuid.Parse(cfg.ProjectID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Invalid project_id", err.Error())
		return
	}
	eid, err := uuid.Parse(cfg.EntityID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Invalid entity_id", err.Error())
		return
	}
	var f *client.MetadataField
	if !cfg.ID.IsNull() {
		id, err := uuid.Parse(cfg.ID.ValueString())
		if err != nil {
			resp.Diagnostics.AddError("Invalid id", err.Error())
			return
		}
		got, err := d.c.Metadata.GetField(ctx, pid, eid, id)
		if err != nil {
			resp.Diagnostics.AddError("Field not found", err.Error())
			return
		}
		f = &got
	} else {
		got, err := lookup.FieldByName(ctx, d.c, pid, eid, cfg.Name.ValueString())
		if err != nil {
			resp.Diagnostics.AddError("Field lookup failed", err.Error())
			return
		}
		if got == nil {
			resp.Diagnostics.AddError("Field not found", fmt.Sprintf("no field named %q", cfg.Name.ValueString()))
			return
		}
		f = got
	}
	cfg.ID = types.StringValue(f.ObjectID.String())
	cfg.ProjectID = types.StringValue(f.ProjectID.String())
	cfg.EntityID = types.StringValue(f.EntityID.String())
	cfg.Name = types.StringValue(f.Name)
	cfg.DataType = types.StringValue(string(f.Config.DataType))
	cfg.ConnectionType = types.StringValue(string(f.Connection.Type))
	if f.Connection.Type == client.ConnectionFragment {
		cfg.FragmentName = types.StringValue(f.Connection.FragmentName)
	} else {
		cfg.FragmentName = types.StringNull()
	}
	cfg.UniqueKey = types.StringValue(f.UniqueKey)
	resp.Diagnostics.Append(resp.State.Set(ctx, cfg)...)
}
