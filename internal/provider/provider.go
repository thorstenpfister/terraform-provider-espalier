// Package provider implements the espalier provider for Apple's container
// runtime.
package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/thorstenpfister/terraform-provider-espalier/internal/client"
)

const defaultCLIPath = "container"

type espalierProvider struct {
	version string
}

type providerModel struct {
	CLIPath types.String `tfsdk:"cli_path"`
}

var _ provider.Provider = &espalierProvider{}

// New returns a provider factory for the given release version.
func New(version string) func() provider.Provider {
	return func() provider.Provider { return &espalierProvider{version: version} }
}

func (p *espalierProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "espalier"
	resp.Version = p.version
}

func (p *espalierProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages resources of Apple's `container` runtime on macOS through its command line tool.",
		Attributes: map[string]schema.Attribute{
			"cli_path": schema.StringAttribute{
				MarkdownDescription: "Path to the `container` command line tool. Defaults to `container`, looked up on `PATH`.",
				Optional:            true,
			},
		},
	}
}

func (p *espalierProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var cfg providerModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	path := defaultCLIPath
	if !cfg.CLIPath.IsNull() && !cfg.CLIPath.IsUnknown() {
		path = cfg.CLIPath.ValueString()
	}
	var c client.Client = client.CLI{Run: client.ExecRunner(path)}
	resp.DataSourceData = c
	resp.ResourceData = c
}

func (p *espalierProvider) Resources(context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewNetworkResource,
		NewVolumeResource,
	}
}

func (p *espalierProvider) DataSources(context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		NewSystemDataSource,
	}
}
