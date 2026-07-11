// SPDX-License-Identifier: MPL-2.0

// Package provider implements the leifwind Terraform/OpenTofu provider.
package provider

import (
	"context"
	"os"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"gitlab.com/leifwind/stream/terraform-provider-leifwind/client"
)

var _ provider.Provider = (*leifwindProvider)(nil)

type leifwindProvider struct {
	version string
}

// New returns the provider factory used by main and by acceptance tests.
func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &leifwindProvider{version: version}
	}
}

type providerModel struct {
	Endpoint     types.String `tfsdk:"endpoint"`
	Token        types.String `tfsdk:"token"`
	Issuer       types.String `tfsdk:"issuer"`
	ClientID     types.String `tfsdk:"client_id"`
	ClientSecret types.String `tfsdk:"client_secret"`
	Audience     types.String `tfsdk:"audience"`
}

func (p *leifwindProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "leifwind"
	resp.Version = p.version
}

func (p *leifwindProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manage leifwind metadata (projects, entities, fields) via the metadata API.",
		Attributes: map[string]schema.Attribute{
			"endpoint": schema.StringAttribute{
				Optional:    true,
				Description: "Backend base URL. Falls back to LEIFWIND_ENDPOINT.",
			},
			"token": schema.StringAttribute{
				Optional:    true,
				Sensitive:   true,
				Description: "Static bearer token (delegated/runner path). Falls back to LEIFWIND_TOKEN. Mutually exclusive with issuer/client_id/client_secret.",
			},
			"issuer": schema.StringAttribute{
				Optional:    true,
				Description: "ZITADEL issuer URL for client_credentials. Falls back to LEIFWIND_OIDC_ISSUER.",
			},
			"client_id": schema.StringAttribute{
				Optional:    true,
				Description: "OAuth client id. Falls back to LEIFWIND_CLIENT_ID.",
			},
			"client_secret": schema.StringAttribute{
				Optional:    true,
				Sensitive:   true,
				Description: "OAuth client secret. Falls back to LEIFWIND_CLIENT_SECRET.",
			},
			"audience": schema.StringAttribute{
				Optional:    true,
				Description: "ZITADEL API project id (audience scope). Falls back to LEIFWIND_OIDC_AUDIENCE.",
			},
		},
	}
}

func (p *leifwindProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var model providerModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() {
		return
	}
	raw := RawConfig{
		Endpoint:     model.Endpoint.ValueStringPointer(),
		Token:        model.Token.ValueStringPointer(),
		Issuer:       model.Issuer.ValueStringPointer(),
		ClientID:     model.ClientID.ValueStringPointer(),
		ClientSecret: model.ClientSecret.ValueStringPointer(),
		Audience:     model.Audience.ValueStringPointer(),
	}
	resolved, errs := resolveConfig(raw, os.Getenv)
	for _, e := range errs {
		resp.Diagnostics.AddError("Invalid provider configuration", e)
	}
	if resp.Diagnostics.HasError() {
		return
	}

	var ts client.TokenSource
	if resolved.UseM2M {
		ccOpts := []client.CredentialOption{}
		if resolved.Audience != "" {
			ccOpts = append(ccOpts, client.WithAudience(resolved.Audience))
		}
		ts = client.ClientCredentials(resolved.Issuer, resolved.ClientID, resolved.ClientSecret, ccOpts...)
	} else {
		ts = client.StaticToken(resolved.Token)
	}

	c, err := client.New(resolved.Endpoint,
		client.WithTokenSource(ts),
		client.WithUserAgent("terraform-provider-leifwind/"+p.version))
	if err != nil {
		resp.Diagnostics.AddError("Failed to construct API client", err.Error())
		return
	}
	resp.ResourceData = c
	resp.DataSourceData = c
}

func (p *leifwindProvider) Resources(context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		// appended by resource tasks
	}
}

func (p *leifwindProvider) DataSources(context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		// appended by data-source tasks
	}
}
