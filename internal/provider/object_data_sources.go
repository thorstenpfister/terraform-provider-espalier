package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	dschema "github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/thorstenpfister/terraform-provider-espalier/internal/client"
)

// objectDataSource looks up one existing object of a kind by name, e.g.
// espalier_network, and describes it with its resource's attributes.
type objectDataSource struct {
	client     client.Client
	kind       string // e.g. "network"
	what       string // for descriptions, e.g. "A network"
	attributes func(ctx context.Context) map[string]dschema.Attribute
	// get returns the object's model, or nil if it does not exist.
	get func(ctx context.Context, c client.Client, name string, diags *diag.Diagnostics) any
}

var _ datasource.DataSourceWithConfigure = &objectDataSource{}

// NewNetworkDataSource returns the espalier_network data source.
func NewNetworkDataSource() datasource.DataSource {
	return &objectDataSource{
		kind: "network", what: "A network",
		attributes: func(ctx context.Context) map[string]dschema.Attribute {
			return resourceAttributes(ctx, &networkResource{})
		},
		get: func(ctx context.Context, c client.Client, name string, diags *diag.Diagnostics) any {
			n, err := c.Network(ctx, name)
			if err != nil {
				diags.AddError("Reading network failed", err.Error())
			}
			if n == nil {
				return nil
			}
			return networkValue(ctx, n, networkModel{}, diags)
		},
	}
}

// NewVolumeDataSource returns the espalier_volume data source.
func NewVolumeDataSource() datasource.DataSource {
	return &objectDataSource{
		kind: "volume", what: "A volume",
		attributes: func(ctx context.Context) map[string]dschema.Attribute {
			return resourceAttributes(ctx, &volumeResource{})
		},
		get: func(ctx context.Context, c client.Client, name string, diags *diag.Diagnostics) any {
			v, err := c.Volume(ctx, name)
			if err != nil {
				diags.AddError("Reading volume failed", err.Error())
			}
			if v == nil {
				return nil
			}
			return volumeValue(ctx, v, volumeModel{}, diags)
		},
	}
}

// NewMachineDataSource returns the espalier_machine data source.
func NewMachineDataSource() datasource.DataSource {
	return &objectDataSource{
		kind: "machine", what: "A container machine",
		attributes: machineDataAttributes,
		get: func(ctx context.Context, c client.Client, name string, diags *diag.Diagnostics) any {
			m, err := c.Machine(ctx, name)
			if err != nil {
				diags.AddError("Reading machine failed", err.Error())
			}
			if m == nil {
				return nil
			}
			return machineValue(m)
		},
	}
}

// machineDataAttributes are the machine resource's, with what the runtime
// does not report marked as such.
func machineDataAttributes(ctx context.Context) map[string]dschema.Attribute {
	attrs := resourceAttributes(ctx, &machineResource{})
	for name, what := range map[string]string{"scheme": "registry scheme", "virtualization": "nested virtualization setting", "kernel": "kernel"} {
		desc := "Always null: the runtime does not report the " + what + "."
		if _, ok := attrs[name].(dschema.BoolAttribute); ok {
			attrs[name] = dschema.BoolAttribute{MarkdownDescription: desc, Computed: true}
		} else {
			attrs[name] = dschema.StringAttribute{MarkdownDescription: desc, Computed: true}
		}
	}
	return attrs
}

func (d *objectDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_" + d.kind
}

func (d *objectDataSource) Schema(ctx context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	attrs := d.attributes(ctx)
	attrs["name"] = dschema.StringAttribute{MarkdownDescription: "Name of the " + d.kind + ".", Required: true}
	resp.Schema = dschema.Schema{
		MarkdownDescription: d.what + " of the container runtime, e.g. one created outside Terraform, as the " +
			"`espalier_" + d.kind + "` resource describes it. Fails if it does not exist.",
		Attributes: attrs,
	}
}

func (d *objectDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = clientFrom(req.ProviderData, &resp.Diagnostics)
}

func (d *objectDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var name types.String
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("name"), &name)...)
	if resp.Diagnostics.HasError() {
		return
	}
	v := d.get(ctx, d.client, name.ValueString(), &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	if v == nil {
		resp.Diagnostics.AddError(d.what+" not found", d.kind+" "+name.ValueString()+" does not exist")
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, v)...)
}
