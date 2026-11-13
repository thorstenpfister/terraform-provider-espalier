package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/mapplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/thorstenpfister/terraform-provider-espalier/internal/client"
)

type networkResource struct {
	client client.Client
}

type networkModel struct {
	ID       types.String `tfsdk:"id"`
	Name     types.String `tfsdk:"name"`
	Internal types.Bool   `tfsdk:"internal"`
	Subnet   types.String `tfsdk:"subnet"`
	SubnetV6 types.String `tfsdk:"subnet_v6"`
	Labels   types.Map    `tfsdk:"labels"`
	Plugin   types.String `tfsdk:"plugin"`
	Options  types.Map    `tfsdk:"options"`
	Gateway  types.String `tfsdk:"gateway"`
}

var (
	_ resource.ResourceWithConfigure   = &networkResource{}
	_ resource.ResourceWithImportState = &networkResource{}
)

// NewNetworkResource returns the espalier_network resource.
func NewNetworkResource() resource.Resource { return &networkResource{} }

func (r *networkResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_network"
}

func (r *networkResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	// The runtime cannot change a network, so every argument replaces it.
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	chosen := []planmodifier.String{stringplanmodifier.RequiresReplaceIfConfigured(), stringplanmodifier.UseStateForUnknown()}
	resp.Schema = schema.Schema{
		MarkdownDescription: "A network of the container runtime (`container network create`). " +
			"Changing any argument replaces the network. A subnet the runtime chose may change when the runtime restarts; " +
			"set `subnet` to keep it fixed.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The network's name.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Name of the network.",
				Required:            true,
				PlanModifiers:       replace,
			},
			"internal": schema.BoolAttribute{
				MarkdownDescription: "Host-only network without a route off the host (`--internal`). Defaults to `false` (NAT).",
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(false),
				PlanModifiers:       []planmodifier.Bool{boolplanmodifier.RequiresReplace()},
			},
			"subnet": schema.StringAttribute{
				MarkdownDescription: "IPv4 subnet in CIDR notation. Chosen by the runtime if not set.",
				Optional:            true,
				Computed:            true,
				PlanModifiers:       chosen,
			},
			"subnet_v6": schema.StringAttribute{
				MarkdownDescription: "IPv6 prefix in CIDR notation. Chosen by the runtime if not set.",
				Optional:            true,
				Computed:            true,
				PlanModifiers:       chosen,
			},
			"labels": schema.MapAttribute{
				MarkdownDescription: "Labels attached to the network.",
				ElementType:         types.StringType,
				Optional:            true,
				PlanModifiers:       []planmodifier.Map{mapplanmodifier.RequiresReplace()},
			},
			"plugin": schema.StringAttribute{
				MarkdownDescription: "Network plugin (`--plugin`). Defaults to the runtime's, `container-network-vmnet`.",
				Optional:            true,
				Computed:            true,
				PlanModifiers:       chosen,
			},
			"options": schema.MapAttribute{
				MarkdownDescription: "Plugin-specific options (`--option key=value`).",
				ElementType:         types.StringType,
				Optional:            true,
				PlanModifiers:       []planmodifier.Map{mapplanmodifier.RequiresReplace()},
			},
			"gateway": schema.StringAttribute{
				MarkdownDescription: "IPv4 gateway address of the network.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

func (r *networkResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFrom(req.ProviderData, &resp.Diagnostics)
}

func (r *networkResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan networkModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	spec := client.NetworkSpec{
		Name:     plan.Name.ValueString(),
		Internal: plan.Internal.ValueBool(),
		Subnet:   plan.Subnet.ValueString(), // "" when unknown: the runtime chooses
		SubnetV6: plan.SubnetV6.ValueString(),
	}
	resp.Diagnostics.Append(plan.Labels.ElementsAs(ctx, &spec.Labels, false)...)
	resp.Diagnostics.Append(plan.Options.ElementsAs(ctx, &spec.Options, false)...)
	spec.Plugin = plan.Plugin.ValueString() // "" when unknown: the default
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.CreateNetwork(ctx, spec); err != nil {
		resp.Diagnostics.AddError("Creating network failed", err.Error())
		return
	}
	r.read(ctx, spec.Name, plan, &resp.State, &resp.Diagnostics, func() {
		resp.Diagnostics.AddError("Creating network failed", "network "+spec.Name+" not found after creation")
	})
}

func (r *networkResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state networkModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	// Deleted outside Terraform: drop it from state so it is planned again.
	r.read(ctx, state.ID.ValueString(), state, &resp.State, &resp.Diagnostics, func() { resp.State.RemoveResource(ctx) })
}

// Update is never called: every argument requires replacement.
func (r *networkResource) Update(context.Context, resource.UpdateRequest, *resource.UpdateResponse) {}

func (r *networkResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state networkModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	n, err := r.client.Network(ctx, state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Reading network failed", err.Error())
		return
	}
	if n == nil {
		return // already gone
	}
	if err := r.client.DeleteNetwork(ctx, n.Name); err != nil {
		resp.Diagnostics.AddError("Deleting network failed", err.Error())
	}
}

func (r *networkResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}

// read stores the named network in st, or calls missing if it does not
// exist. prior is the plan or state, for maps left unset (see labelsValue).
func (r *networkResource) read(ctx context.Context, name string, prior networkModel, st *tfsdk.State, diags *diag.Diagnostics, missing func()) {
	n, err := r.client.Network(ctx, name)
	if err != nil {
		diags.AddError("Reading network failed", err.Error())
		return
	}
	if n == nil {
		missing()
		return
	}
	m := networkModel{
		ID:       types.StringValue(n.Name),
		Name:     types.StringValue(n.Name),
		Internal: types.BoolValue(n.Internal),
		Subnet:   types.StringValue(n.Subnet),
		SubnetV6: types.StringValue(n.SubnetV6),
		Gateway:  types.StringValue(n.Gateway),
		Labels:   labelsValue(ctx, n.Labels, prior.Labels, diags),
		Plugin:   types.StringValue(n.Plugin),
		Options:  labelsValue(ctx, n.Options, prior.Options, diags),
	}
	diags.Append(st.Set(ctx, &m)...)
}
