// Package provider implements the espalier provider for Apple's container
// runtime.
package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/thorstenpfister/terraform-provider-espalier/internal/client"
)

const defaultCLIPath = "container"

// testedCLIVersion is the container version this provider release was
// verified against; see "Compatibility" and "Upgrading the runtime" in the
// README.
const testedCLIVersion = "1.7.1"

type espalierProvider struct {
	version string
	client  client.Client // for tests; nil runs the CLI
}

type providerModel struct {
	CLIPath types.String  `tfsdk:"cli_path"`
	Builder *builderModel `tfsdk:"builder"`
}

type builderModel struct {
	CPUs        types.Int64  `tfsdk:"cpus"`
	Memory      types.String `tfsdk:"memory"`
	KeepRunning types.Bool   `tfsdk:"keep_running"`
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
			"builder": schema.SingleNestedAttribute{
				MarkdownDescription: "The runtime's builder container, which runs `espalier_image_build`s. The provider " +
					"starts it for a build and stops it again when no build is left running, unless it was already " +
					"running before. Setting `cpus` or `memory` recreates a running builder of another size.",
				Optional: true,
				Attributes: map[string]schema.Attribute{
					"cpus": schema.Int64Attribute{
						MarkdownDescription: "CPUs of the builder. Defaults to 2.",
						Optional:            true,
						Validators:          []validator.Int64{int64validator.AtLeast(1)},
					},
					"memory": schema.StringAttribute{
						MarkdownDescription: "Memory of the builder in bytes, with an optional `K`, `M`, `G`, `T` or `P` " +
							"suffix. Defaults to `2G`.",
						Optional: true,
						Validators: []validator.String{
							stringvalidator.RegexMatches(sizePattern, "must be a number of bytes with an optional K, M, G, T or P suffix"),
						},
					},
					"keep_running": schema.BoolAttribute{
						MarkdownDescription: "Keep the builder running after the builds, which makes the next run's " +
							"builds start a few seconds sooner. Defaults to `false`.",
						Optional: true,
					},
				},
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
	if p.client != nil {
		c = p.client
	}
	// A CLI that cannot be run is left to the first call that needs it.
	if v, err := c.Version(ctx); err == nil && v != testedCLIVersion {
		resp.Diagnostics.AddWarning("Untested container version", fmt.Sprintf(
			"container %s is installed, but this provider release was verified against %s. It may work, but the "+
				"provider reads the CLI's output, which can change between versions. The compatibility table lists "+
				"the release verified against yours: "+
				"https://github.com/thorstenpfister/terraform-provider-espalier#compatibility", v, testedCLIVersion))
	}
	data := &providerData{client: c, builder: &builder{}}
	if b := cfg.Builder; b != nil {
		data.builder.settings = client.BuilderSettings{CPUs: b.CPUs.ValueInt64(), Memory: b.Memory.ValueString()}
		data.builder.keepRunning = b.KeepRunning.ValueBool()
	}
	resp.DataSourceData = data
	resp.ResourceData = data
}

func (p *espalierProvider) Resources(context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewNetworkResource,
		NewVolumeResource,
		NewImageResource,
		NewImageTagResource,
		NewImageBuildResource,
		NewContainerResource,
		NewMachineResource,
		NewClusterResource,
	}
}

func (p *espalierProvider) DataSources(context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		NewSystemDataSource,
		NewImageDataSource,
		NewContainerDataSource,
		NewNetworksDataSource,
		NewVolumesDataSource,
		NewImagesDataSource,
		NewContainersDataSource,
		NewNetworkDataSource,
		NewVolumeDataSource,
		NewMachineDataSource,
		NewMachinesDataSource,
	}
}
