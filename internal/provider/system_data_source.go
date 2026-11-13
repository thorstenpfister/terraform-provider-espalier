package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/thorstenpfister/terraform-provider-espalier/internal/client"
)

type systemDataSource struct {
	client client.Client
}

type systemModel struct {
	Running          types.Bool   `tfsdk:"running"`
	Status           types.String `tfsdk:"status"`
	ClientVersion    types.String `tfsdk:"client_version"`
	ServerVersion    types.String `tfsdk:"server_version"`
	KernelBinaryPath types.String `tfsdk:"kernel_binary_path"`
	KernelURL        types.String `tfsdk:"kernel_url"`
	KernelDigest     types.String `tfsdk:"kernel_digest"`
}

var _ datasource.DataSourceWithConfigure = &systemDataSource{}

// NewSystemDataSource returns the espalier_system data source.
func NewSystemDataSource() datasource.DataSource { return &systemDataSource{} }

func (d *systemDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_system"
}

func (d *systemDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	whenRunning := " Null while the runtime is stopped."
	resp.Schema = schema.Schema{
		MarkdownDescription: "The container runtime on this host: whether it runs, its versions and the default kernel. " +
			"A stopped runtime is reported, not an error, so configurations can check it as a precondition " +
			"(espalier never starts the runtime: run `container system start`).",
		Attributes: map[string]schema.Attribute{
			"running": schema.BoolAttribute{
				MarkdownDescription: "Whether the runtime's services are running.",
				Computed:            true,
			},
			"status": schema.StringAttribute{
				MarkdownDescription: "Status as reported by `container system status`, e.g. `running`, or `unregistered` after `container system stop`.",
				Computed:            true,
			},
			"client_version": schema.StringAttribute{
				MarkdownDescription: "Version of the `container` command line tool.",
				Computed:            true,
			},
			"server_version": schema.StringAttribute{
				MarkdownDescription: "Version of the runtime's API server." + whenRunning,
				Computed:            true,
			},
			"kernel_binary_path": schema.StringAttribute{
				MarkdownDescription: "Path of the default kernel inside its archive." + whenRunning,
				Computed:            true,
			},
			"kernel_url": schema.StringAttribute{
				MarkdownDescription: "Where the default kernel archive comes from." + whenRunning,
				Computed:            true,
			},
			"kernel_digest": schema.StringAttribute{
				MarkdownDescription: "Digest of the default kernel archive." + whenRunning,
				Computed:            true,
			},
		},
	}
}

func (d *systemDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(client.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider data", fmt.Sprintf("got %T, want client.Client", req.ProviderData))
		return
	}
	d.client = c
}

func (d *systemDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	sys, err := d.client.System(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Reading the container runtime failed", err.Error())
		return
	}
	state := systemModel{
		Running:       types.BoolValue(sys.Running),
		Status:        types.StringValue(sys.Status),
		ClientVersion: types.StringValue(sys.ClientVersion),
	}
	if sys.Running {
		state.ServerVersion = types.StringValue(sys.ServerVersion)
		state.KernelBinaryPath = types.StringValue(sys.Kernel.BinaryPath)
		state.KernelURL = types.StringValue(sys.Kernel.URL)
		state.KernelDigest = types.StringValue(sys.Kernel.Digest)
	} else {
		state.ServerVersion = types.StringNull()
		state.KernelBinaryPath = types.StringNull()
		state.KernelURL = types.StringNull()
		state.KernelDigest = types.StringNull()
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
