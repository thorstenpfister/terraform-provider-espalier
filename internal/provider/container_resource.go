package provider

import (
	"context"
	"fmt"
	"maps"
	"regexp"
	"strconv"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/mapplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/thorstenpfister/terraform-provider-espalier/internal/client"
)

type containerResource struct {
	client client.Client
}

type containerModel struct {
	ID        types.String `tfsdk:"id"`
	Name      types.String `tfsdk:"name"`
	Image     types.String `tfsdk:"image"`
	Command   types.List   `tfsdk:"command"`
	Env       types.Map    `tfsdk:"env"`
	Workdir   types.String `tfsdk:"workdir"`
	CPUs      types.Int64  `tfsdk:"cpus"`
	Memory    types.String `tfsdk:"memory"`
	Labels    types.Map    `tfsdk:"labels"`
	Networks  types.List   `tfsdk:"networks"`
	Mounts    types.List   `tfsdk:"mounts"`
	Ports     types.List   `tfsdk:"ports"`
	CapAdd    types.Set    `tfsdk:"cap_add"`
	CapDrop   types.Set    `tfsdk:"cap_drop"`
	Running   types.Bool   `tfsdk:"running"`
	State     types.String `tfsdk:"state"`
	Addresses types.List   `tfsdk:"addresses"`
}

type addressModel struct {
	Network     types.String `tfsdk:"network"`
	IPv4        types.String `tfsdk:"ipv4_address"`
	IPv4Gateway types.String `tfsdk:"ipv4_gateway"`
	IPv6        types.String `tfsdk:"ipv6_address"`
	MAC         types.String `tfsdk:"mac_address"`
}

var addressType = types.ObjectType{AttrTypes: map[string]attr.Type{
	"network":      types.StringType,
	"ipv4_address": types.StringType,
	"ipv4_gateway": types.StringType,
	"ipv6_address": types.StringType,
	"mac_address":  types.StringType,
}}

var sizePattern = regexp.MustCompile(`^([0-9]+)([KMGTP]?)$`)

var (
	_ resource.ResourceWithConfigure      = &containerResource{}
	_ resource.ResourceWithImportState    = &containerResource{}
	_ resource.ResourceWithModifyPlan     = &containerResource{}
	_ resource.ResourceWithValidateConfig = &containerResource{}
)

// NewContainerResource returns the espalier_container resource.
func NewContainerResource() resource.Resource { return &containerResource{} }

func (r *containerResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_container"
}

func (r *containerResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	// The runtime cannot change a container: arguments replace it, except
	// running. Defaults the image or runtime fill in are computed.
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{
		MarkdownDescription: "A container of the container runtime (`container create`), each a lightweight VM. " +
			"Changing any argument except `running` replaces the container. Its addresses may change whenever it restarts.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The container's name.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Name of the container, also its hostname.",
				Required:            true,
				PlanModifiers:       replace,
			},
			"image": schema.StringAttribute{
				MarkdownDescription: "Image reference, resolved like `espalier_image`'s `name`. The runtime pulls it " +
					"if it is missing; use `espalier_image` to control pulling and pin versions.",
				Required: true, // replaced in ModifyPlan
			},
			"command": schema.ListAttribute{
				MarkdownDescription: "The process to run, as a full argument list (its first element replaces the " +
					"image's entrypoint). Defaults to the image's entrypoint and command.",
				ElementType: types.StringType,
				Optional:    true,
				Computed:    true,
				PlanModifiers: []planmodifier.List{
					listplanmodifier.RequiresReplaceIfConfigured(), listplanmodifier.UseStateForUnknown(),
				},
			},
			"env": schema.MapAttribute{
				MarkdownDescription: "Environment variables, on top of the image's environment.",
				ElementType:         types.StringType,
				Optional:            true,
				PlanModifiers:       []planmodifier.Map{mapplanmodifier.RequiresReplace()},
			},
			"workdir": schema.StringAttribute{
				MarkdownDescription: "Working directory of the process. Defaults to the image's, else `/`.",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplaceIfConfigured(), stringplanmodifier.UseStateForUnknown(),
				},
			},
			"cpus": schema.Int64Attribute{
				MarkdownDescription: "Number of CPUs. Defaults to the runtime's `container.cpus` property.",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.RequiresReplaceIfConfigured(), int64planmodifier.UseStateForUnknown(),
				},
			},
			"memory": schema.StringAttribute{
				MarkdownDescription: "Memory in bytes, with an optional `K`, `M`, `G`, `T` or `P` suffix, e.g. `512M`. " +
					"Defaults to the runtime's `container.memory` property.",
				Optional:      true,
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}, // replaced in ModifyPlan
				Validators: []validator.String{
					stringvalidator.RegexMatches(sizePattern, "must be a number of bytes with an optional K, M, G, T or P suffix"),
				},
			},
			"labels": schema.MapAttribute{
				MarkdownDescription: "Labels attached to the container.",
				ElementType:         types.StringType,
				Optional:            true,
				PlanModifiers:       []planmodifier.Map{mapplanmodifier.RequiresReplace()},
			},
			"running": schema.BoolAttribute{
				MarkdownDescription: "Whether the container should run. Changes start or stop it in place; a container " +
					"stopped outside Terraform is started again. Defaults to `true`.",
				Optional: true,
				Computed: true,
				Default:  booldefault.StaticBool(true),
			},
			"state": schema.StringAttribute{
				MarkdownDescription: "State as the runtime reports it, e.g. `running` or `stopped`.",
				Computed:            true,
			},
			"addresses": schema.ListNestedAttribute{
				MarkdownDescription: "Addresses of the running container, one per network. Empty while it is stopped; " +
					"they may change when it restarts.",
				Computed: true,
				NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
					"network":      schema.StringAttribute{MarkdownDescription: "Network name.", Computed: true},
					"ipv4_address": schema.StringAttribute{MarkdownDescription: "IPv4 address with prefix length.", Computed: true},
					"ipv4_gateway": schema.StringAttribute{MarkdownDescription: "IPv4 gateway.", Computed: true},
					"ipv6_address": schema.StringAttribute{MarkdownDescription: "IPv6 address with prefix length.", Computed: true},
					"mac_address":  schema.StringAttribute{MarkdownDescription: "MAC address.", Computed: true},
				}},
			},
		},
	}
	maps.Copy(resp.Schema.Attributes, attachmentAttributes())
}

func (r *containerResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var mounts types.List
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("mounts"), &mounts)...)
	validateMounts(ctx, mounts, &resp.Diagnostics)
}

func (r *containerResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFrom(req.ProviderData, &resp.Diagnostics)
}

func (r *containerResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan containerModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	spec := client.ContainerSpec{
		Name:    plan.Name.ValueString(),
		Image:   plan.Image.ValueString(),
		Workdir: plan.Workdir.ValueString(), // "" when unknown: the default
		CPUs:    plan.CPUs.ValueInt64(),
		Memory:  plan.Memory.ValueString(),
	}
	if !plan.Command.IsUnknown() {
		resp.Diagnostics.Append(plan.Command.ElementsAs(ctx, &spec.Command, false)...)
	}
	resp.Diagnostics.Append(plan.Env.ElementsAs(ctx, &spec.Env, false)...)
	resp.Diagnostics.Append(plan.Labels.ElementsAs(ctx, &spec.Labels, false)...)
	attachmentsSpec(ctx, plan, &spec, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.CreateContainer(ctx, spec); err != nil {
		resp.Diagnostics.AddError("Creating container failed", err.Error())
		return
	}
	missing := func() {
		resp.Diagnostics.AddError("Creating container failed", "container "+spec.Name+" not found after creation")
	}
	if plan.Running.ValueBool() {
		if err := r.client.StartContainer(ctx, spec.Name); err != nil {
			// Record the container, so Terraform taints it rather than losing it.
			resp.Diagnostics.AddError("Starting container failed", err.Error())
		}
	}
	r.read(ctx, spec.Name, plan, &resp.State, &resp.Diagnostics, missing)
}

func (r *containerResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state containerModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	// Deleted outside Terraform: drop it from state so it is planned again.
	r.read(ctx, state.ID.ValueString(), state, &resp.State, &resp.Diagnostics, func() { resp.State.RemoveResource(ctx) })
}

// Update starts or stops the container, or records an image or memory
// written differently but meaning the same; every other change replaces it.
func (r *containerResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state containerModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	name := plan.ID.ValueString()
	if !plan.Running.Equal(state.Running) {
		var err error
		if plan.Running.ValueBool() {
			err = r.client.StartContainer(ctx, name)
		} else {
			err = r.client.StopContainer(ctx, name)
		}
		if err != nil {
			resp.Diagnostics.AddError("Changing the container's state failed", err.Error())
			return
		}
	}
	r.read(ctx, name, plan, &resp.State, &resp.Diagnostics, func() {
		resp.Diagnostics.AddError("Changing the container's state failed", "container "+name+" not found")
	})
}

func (r *containerResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state containerModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	c, err := r.client.Container(ctx, state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Reading container failed", err.Error())
		return
	}
	if c == nil {
		return // already gone
	}
	if err := r.client.DeleteContainer(ctx, c.Name); err != nil {
		resp.Diagnostics.AddError("Deleting container failed", err.Error())
	}
}

func (r *containerResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}

// read stores the named container in st, or calls missing if it does not
// exist. prior is the plan or state: values written differently from how
// the runtime reports them (image, memory, unset maps) are kept as written.
func (r *containerResource) read(ctx context.Context, name string, prior containerModel, st *tfsdk.State, diags *diag.Diagnostics, missing func()) {
	c, err := r.client.Container(ctx, name)
	if err != nil {
		diags.AddError("Reading container failed", err.Error())
		return
	}
	if c == nil {
		missing()
		return
	}
	m := containerValue(ctx, c, diags)
	m.Env = labelsValue(ctx, c.Env, prior.Env, diags)
	m.Labels = labelsValue(ctx, c.Labels, prior.Labels, diags)
	if !prior.Image.IsNull() && !prior.Image.IsUnknown() {
		resolved, err := r.client.ImageName(ctx, prior.Image.ValueString())
		if err != nil {
			diags.AddError("Reading container failed", err.Error())
			return
		}
		if resolved == c.Image {
			m.Image = prior.Image
		}
	}
	if b, ok := parseSize(prior.Memory.ValueString()); ok && b == c.MemoryBytes {
		m.Memory = prior.Memory
	}
	diags.Append(st.Set(ctx, &m)...)
}

// containerValue is the container as the runtime reports it: the image
// fully qualified, memory in the largest whole unit, empty maps and
// collections null.
func containerValue(ctx context.Context, c *client.Container, diags *diag.Diagnostics) containerModel {
	m := containerModel{
		ID:      types.StringValue(c.Name),
		Name:    types.StringValue(c.Name),
		Image:   types.StringValue(c.Image),
		Workdir: types.StringValue(c.Workdir),
		CPUs:    types.Int64Value(c.CPUs),
		Memory:  types.StringValue(formatSize(c.MemoryBytes)),
		Env:     labelsValue(ctx, c.Env, types.MapNull(types.StringType), diags),
		Labels:  labelsValue(ctx, c.Labels, types.MapNull(types.StringType), diags),
		Running: types.BoolValue(c.State == "running"),
		State:   types.StringValue(c.State),
	}
	readAttachments(ctx, c, &m, diags)
	var d diag.Diagnostics
	m.Command, d = types.ListValueFrom(ctx, types.StringType, c.Command)
	diags.Append(d...)
	addresses := make([]addressModel, 0, len(c.Addresses))
	for _, a := range c.Addresses {
		addresses = append(addresses, addressModel{
			Network:     types.StringValue(a.Network),
			IPv4:        types.StringValue(a.IPv4),
			IPv4Gateway: types.StringValue(a.IPv4Gateway),
			IPv6:        types.StringValue(a.IPv6),
			MAC:         types.StringValue(a.MAC),
		})
	}
	m.Addresses, d = types.ListValueFrom(ctx, addressType, addresses)
	diags.Append(d...)
	return m
}

// parseSize reads a size like the runtime does: bytes with an optional
// binary K, M, G, T or P suffix.
func parseSize(s string) (int64, bool) {
	m := sizePattern.FindStringSubmatch(s)
	if m == nil {
		return 0, false
	}
	n, err := strconv.ParseInt(m[1], 10, 64)
	if err != nil {
		return 0, false
	}
	shift := map[string]uint{"": 0, "K": 10, "M": 20, "G": 30, "T": 40, "P": 50}[m[2]]
	return n << shift, true
}

// formatSize writes bytes in the largest unit that divides them.
func formatSize(b int64) string {
	for _, u := range []struct {
		suffix string
		shift  uint
	}{{"P", 50}, {"T", 40}, {"G", 30}, {"M", 20}, {"K", 10}} {
		if b != 0 && b%(1<<u.shift) == 0 {
			return fmt.Sprintf("%d%s", b>>u.shift, u.suffix)
		}
	}
	return strconv.FormatInt(b, 10)
}

// ModifyPlan replaces the container when image or memory change, but not
// when only their spelling does, e.g. `busybox:1.37` and
// `docker.io/library/busybox:1.37`, or `1G` and `1024M`: the new spelling is
// recorded in place. (Attribute plan modifiers cannot resolve images: they
// have no configured client.)
func (r *containerResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.State.Raw.IsNull() || req.Plan.Raw.IsNull() {
		return
	}
	var plan, state containerModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !plan.Image.Equal(state.Image) {
		same := false
		if !plan.Image.IsUnknown() {
			a, err := r.client.ImageName(ctx, state.Image.ValueString())
			if err != nil {
				resp.Diagnostics.AddError("Resolving image failed", err.Error())
				return
			}
			b, err := r.client.ImageName(ctx, plan.Image.ValueString())
			if err != nil {
				resp.Diagnostics.AddError("Resolving image failed", err.Error())
				return
			}
			same = a == b
		}
		if !same {
			resp.RequiresReplace = append(resp.RequiresReplace, path.Root("image"))
		}
	}
	replaceChangedAttachments(plan, state, resp)
	if !plan.Memory.Equal(state.Memory) {
		a, okA := parseSize(state.Memory.ValueString())
		b, okB := parseSize(plan.Memory.ValueString())
		if !okA || !okB || a != b {
			resp.RequiresReplace = append(resp.RequiresReplace, path.Root("memory"))
		}
	}
	if len(resp.RequiresReplace) == 0 {
		keepObserved(ctx, plan, state, req.State, resp)
	}

}

// keepObserved keeps state and addresses as known when nothing changes.
// Terraform proposes null for unset nested attributes such as networks,
// which the framework takes for a change and so marks all computed values
// unknown; UseStateForUnknown restores networks, this restores the rest.
func keepObserved(ctx context.Context, plan, state containerModel, prior tfsdk.State, resp *resource.ModifyPlanResponse) {
	plan.State, plan.Addresses = state.State, state.Addresses
	unchanged := resp.Plan
	resp.Diagnostics.Append(unchanged.Set(ctx, &plan)...)
	if unchanged.Raw.Equal(prior.Raw) {
		resp.Plan = unchanged
	}
}
