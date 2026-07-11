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
	_ datasource.DataSource              = (*fieldsDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*fieldsDataSource)(nil)
)

// NewFieldsDataSource registers data.leifwind_fields.
func NewFieldsDataSource() datasource.DataSource { return &fieldsDataSource{} }

type fieldsDataSource struct {
	c *client.Client
}

// fieldItemModel is the per-item shape of leifwind_fields' "fields" list,
// per the task-24 brief: {id, name, data_type, connection_type,
// fragment_name} — the nested object carries all five computed attrs
// (no unique_key — the standing "include unique_key" deviation applies to
// the singular leifwind_field data source only, not this listing).
type fieldItemModel struct {
	ID             types.String `tfsdk:"id"`
	Name           types.String `tfsdk:"name"`
	DataType       types.String `tfsdk:"data_type"`
	ConnectionType types.String `tfsdk:"connection_type"`
	FragmentName   types.String `tfsdk:"fragment_name"`
}

type fieldsDSModel struct {
	ProjectID types.String     `tfsdk:"project_id"`
	EntityID  types.String     `tfsdk:"entity_id"`
	Pattern   types.String     `tfsdk:"pattern"`
	Fields    []fieldItemModel `tfsdk:"fields"`
}

func (d *fieldsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_fields"
}

func (d *fieldsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "List fields of an entity, optionally filtered by a name substring pattern.",
		Attributes: map[string]schema.Attribute{
			"project_id": schema.StringAttribute{Required: true, Description: "Owning project id (UUID)."},
			"entity_id":  schema.StringAttribute{Required: true, Description: "Owning entity id (UUID)."},
			"pattern":    schema.StringAttribute{Optional: true, Description: "Substring filter on the name (server-side ILIKE)."},
			"fields": schema.ListNestedAttribute{
				Computed: true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id":              schema.StringAttribute{Computed: true},
						"name":            schema.StringAttribute{Computed: true},
						"data_type":       schema.StringAttribute{Computed: true},
						"connection_type": schema.StringAttribute{Computed: true},
						"fragment_name":   schema.StringAttribute{Computed: true},
					},
				},
			},
		},
	}
}

func (d *fieldsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *fieldsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var cfg fieldsDSModel
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
	cfg.Fields = []fieldItemModel{}
	for f, err := range d.c.Metadata.IterFields(ctx, pid, eid, client.ListOpts{Pattern: cfg.Pattern.ValueString()}) {
		if err != nil {
			resp.Diagnostics.AddError("Listing fields failed", err.Error())
			return
		}
		item := fieldItemModel{
			ID:             types.StringValue(f.ObjectID.String()),
			Name:           types.StringValue(f.Name),
			DataType:       types.StringValue(string(f.Config.DataType)),
			ConnectionType: types.StringValue(string(f.Connection.Type)),
		}
		if f.Connection.Type == client.ConnectionFragment {
			item.FragmentName = types.StringValue(f.Connection.FragmentName)
		} else {
			item.FragmentName = types.StringNull()
		}
		cfg.Fields = append(cfg.Fields, item)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, cfg)...)
}
