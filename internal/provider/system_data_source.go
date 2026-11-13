package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/diag"
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
	RegistryDomain   types.String `tfsdk:"registry_domain"`
	DNSDomain        types.String `tfsdk:"dns_domain"`
	VMInitImage      types.String `tfsdk:"vminit_image"`
	NetworkSubnet    types.String `tfsdk:"network_subnet"`
	NetworkSubnetV6  types.String `tfsdk:"network_subnet_v6"`
	Container        types.Object `tfsdk:"container_defaults"`
	Builder          types.Object `tfsdk:"builder_defaults"`
	Machine          types.Object `tfsdk:"machine_defaults"`
}

type containerDefaultsModel struct {
	CPUs   types.Int64  `tfsdk:"cpus"`
	Memory types.String `tfsdk:"memory"`
	DNS    types.Object `tfsdk:"dns"`
}

type builderDefaultsModel struct {
	CPUs    types.Int64  `tfsdk:"cpus"`
	Memory  types.String `tfsdk:"memory"`
	Rosetta types.Bool   `tfsdk:"rosetta"`
	Image   types.String `tfsdk:"image"`
}

type machineDefaultsModel struct {
	CPUs           types.Int64  `tfsdk:"cpus"`
	Memory         types.String `tfsdk:"memory"`
	HomeMount      types.String `tfsdk:"home_mount"`
	Virtualization types.Bool   `tfsdk:"virtualization"`
}

var (
	containerDefaultsType = types.ObjectType{AttrTypes: map[string]attr.Type{
		"cpus": types.Int64Type, "memory": types.StringType, "dns": dnsType,
	}}
	builderDefaultsType = types.ObjectType{AttrTypes: map[string]attr.Type{
		"cpus": types.Int64Type, "memory": types.StringType, "rosetta": types.BoolType, "image": types.StringType,
	}}
	machineDefaultsType = types.ObjectType{AttrTypes: map[string]attr.Type{
		"cpus": types.Int64Type, "memory": types.StringType, "home_mount": types.StringType, "virtualization": types.BoolType,
	}}
)

var _ datasource.DataSourceWithConfigure = &systemDataSource{}

// NewSystemDataSource returns the espalier_system data source.
func NewSystemDataSource() datasource.DataSource { return &systemDataSource{} }

func (d *systemDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_system"
}

func (d *systemDataSource) Schema(ctx context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	whenRunning := " Null while the runtime is stopped."
	str := func(desc string) schema.StringAttribute {
		return schema.StringAttribute{MarkdownDescription: desc + whenRunning, Computed: true}
	}
	resp.Schema = schema.Schema{
		MarkdownDescription: "The container runtime on this host: whether it runs, its versions, the default kernel and " +
			"the defaults from its `config.toml` (`container system property list`). " +
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
			"registry_domain":   str("Registry that short image references resolve to, e.g. `docker.io`."),
			"dns_domain":        str("Local DNS domain appended to container names; null if unset."),
			"vminit_image":      str("Default init image of container VMs."),
			"network_subnet":    str("Default IPv4 subnet for new networks; null if unset."),
			"network_subnet_v6": str("Default IPv6 prefix for new networks; null if unset."),
			"container_defaults": schema.SingleNestedAttribute{
				MarkdownDescription: "Defaults for containers that do not set them." + whenRunning,
				Computed:            true,
				Attributes: map[string]schema.Attribute{
					"cpus":   schema.Int64Attribute{MarkdownDescription: "CPUs.", Computed: true},
					"memory": schema.StringAttribute{MarkdownDescription: "Memory, in the largest whole unit, e.g. `1G`.", Computed: true},
					"dns":    dataSourceAttributes(ctx, &containerDataSource{})["dns"],
				},
			},
			"builder_defaults": schema.SingleNestedAttribute{
				MarkdownDescription: "The builder container's settings, unless the provider's `builder` overrides them." + whenRunning,
				Computed:            true,
				Attributes: map[string]schema.Attribute{
					"cpus":    schema.Int64Attribute{MarkdownDescription: "CPUs.", Computed: true},
					"memory":  schema.StringAttribute{MarkdownDescription: "Memory, e.g. `2G`.", Computed: true},
					"rosetta": schema.BoolAttribute{MarkdownDescription: "Whether builds can use Rosetta for x86-64.", Computed: true},
					"image":   schema.StringAttribute{MarkdownDescription: "Builder image.", Computed: true},
				},
			},
			"machine_defaults": schema.SingleNestedAttribute{
				MarkdownDescription: "Defaults for machines that do not set them." + whenRunning,
				Computed:            true,
				Attributes: map[string]schema.Attribute{
					"cpus":           schema.Int64Attribute{MarkdownDescription: "CPUs.", Computed: true},
					"memory":         schema.StringAttribute{MarkdownDescription: "Memory, e.g. `12G`.", Computed: true},
					"home_mount":     schema.StringAttribute{MarkdownDescription: "How the home directory is mounted.", Computed: true},
					"virtualization": schema.BoolAttribute{MarkdownDescription: "Nested virtualization.", Computed: true},
				},
			},
		},
	}
}

func (d *systemDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = clientFrom(req.ProviderData, &resp.Diagnostics)
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
		propertiesValue(ctx, sys.Properties, &state, &resp.Diagnostics)
	} else {
		for _, v := range []*types.String{
			&state.ServerVersion, &state.KernelBinaryPath, &state.KernelURL, &state.KernelDigest,
			&state.RegistryDomain, &state.DNSDomain, &state.VMInitImage, &state.NetworkSubnet, &state.NetworkSubnetV6,
		} {
			*v = types.StringNull()
		}
		state.Container = types.ObjectNull(containerDefaultsType.AttrTypes)
		state.Builder = types.ObjectNull(builderDefaultsType.AttrTypes)
		state.Machine = types.ObjectNull(machineDefaultsType.AttrTypes)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func propertiesValue(ctx context.Context, p client.Properties, m *systemModel, diags *diag.Diagnostics) {
	m.RegistryDomain = types.StringValue(p.RegistryDomain)
	m.DNSDomain = nullIfEmpty(p.DNSDomain)
	m.VMInitImage = types.StringValue(p.VMInitImage)
	m.NetworkSubnet = nullIfEmpty(p.NetworkSubnet)
	m.NetworkSubnetV6 = nullIfEmpty(p.NetworkSubnetV6)

	dns := types.ObjectNull(dnsType.AttrTypes)
	if d := p.Container.DNS; len(d.Nameservers)+len(d.Search)+len(d.Options) > 0 || d.Domain != "" {
		var ds diag.Diagnostics
		dns, ds = types.ObjectValueFrom(ctx, dnsType.AttrTypes, dnsModel{
			Nameservers: stringList(ctx, d.Nameservers, diags),
			Domain:      nullIfEmpty(d.Domain),
			Search:      stringList(ctx, d.Search, diags),
			Options:     stringList(ctx, d.Options, diags),
		})
		diags.Append(ds...)
	}
	for _, o := range []struct {
		dst   *types.Object
		typ   types.ObjectType
		value any
	}{
		{&m.Container, containerDefaultsType, containerDefaultsModel{
			CPUs: types.Int64Value(p.Container.CPUs), Memory: types.StringValue(formatSize(p.Container.MemoryBytes)), DNS: dns,
		}},
		{&m.Builder, builderDefaultsType, builderDefaultsModel{
			CPUs: types.Int64Value(p.Builder.CPUs), Memory: types.StringValue(formatSize(p.Builder.MemoryBytes)),
			Rosetta: types.BoolValue(p.Builder.Rosetta), Image: types.StringValue(p.Builder.Image),
		}},
		{&m.Machine, machineDefaultsType, machineDefaultsModel{
			CPUs: types.Int64Value(p.Machine.CPUs), Memory: types.StringValue(formatSize(p.Machine.MemoryBytes)),
			HomeMount: types.StringValue(p.Machine.HomeMount), Virtualization: types.BoolValue(p.Machine.Virtualization),
		}},
	} {
		v, d := types.ObjectValueFrom(ctx, o.typ.AttrTypes, o.value)
		diags.Append(d...)
		*o.dst = v
	}
}
