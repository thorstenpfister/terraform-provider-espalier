package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-validators/boolvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/thorstenpfister/terraform-provider-espalier/internal/client"
)

type machineResource struct {
	client client.Client
}

type machineModel struct {
	ID             types.String `tfsdk:"id"`
	Name           types.String `tfsdk:"name"`
	Image          types.String `tfsdk:"image"`
	Platform       types.String `tfsdk:"platform"`
	User           types.String `tfsdk:"user"`
	UID            types.Int64  `tfsdk:"uid"`
	GID            types.Int64  `tfsdk:"gid"`
	Home           types.String `tfsdk:"home"`
	Scheme         types.String `tfsdk:"scheme"`
	Default        types.Bool   `tfsdk:"default"`
	CPUs           types.Int64  `tfsdk:"cpus"`
	Memory         types.String `tfsdk:"memory"`
	HomeMount      types.String `tfsdk:"home_mount"`
	Virtualization types.Bool   `tfsdk:"virtualization"`
	Kernel         types.String `tfsdk:"kernel"`
	Running        types.Bool   `tfsdk:"running"`
	Status         types.String `tfsdk:"status"`
	IPAddress      types.String `tfsdk:"ip_address"`
	Digest         types.String `tfsdk:"digest"`
	DiskSize       types.Int64  `tfsdk:"disk_size"`
}

var (
	_ resource.ResourceWithConfigure      = &machineResource{}
	_ resource.ResourceWithImportState    = &machineResource{}
	_ resource.ResourceWithModifyPlan     = &machineResource{}
	_ resource.ResourceWithValidateConfig = &machineResource{}
)

// NewMachineResource returns the espalier_machine resource.
func NewMachineResource() resource.Resource { return &machineResource{} }

func (r *machineResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_machine"
}

func (r *machineResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	known := []planmodifier.String{stringplanmodifier.UseStateForUnknown()}
	account := []planmodifier.String{stringplanmodifier.RequiresReplaceIfConfigured(), stringplanmodifier.UseStateForUnknown()}
	accountID := []planmodifier.Int64{int64planmodifier.RequiresReplaceIfConfigured(), int64planmodifier.UseStateForUnknown()}
	resp.Schema = schema.Schema{
		MarkdownDescription: "A container machine (`container machine create`): a long-lived Linux VM made from an " +
			"image, with an account for a host user and their home directory mounted. Changing the image, platform " +
			"or account replaces it; the settings (`cpus`, `memory`, `home_mount`, `virtualization`, `kernel`) change " +
			"in place and restart a running machine.\n\n" +
			"A new machine boots once with the runtime's defaults (home directory mounted read-write) before configured " +
			"settings are applied and it restarts.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{MarkdownDescription: "The machine's name.", Computed: true, PlanModifiers: known},
			"name": schema.StringAttribute{
				MarkdownDescription: "Name of the machine.",
				Required:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"image": schema.StringAttribute{
				MarkdownDescription: "Image reference, resolved like `espalier_image`'s `name`; pulled if missing.",
				Required:            true, // replaced in ModifyPlan
			},
			"platform": schema.StringAttribute{
				MarkdownDescription: "Platform of a multi-platform image (`os/arch[/variant]`). Defaults to `linux/arm64`.",
				Optional:            true,
				Computed:            true,
				PlanModifiers:       known, // replaced in ModifyPlan
			},
			"user": schema.StringAttribute{
				MarkdownDescription: "Account name in the machine. Defaults to the host user's.",
				Optional:            true, Computed: true, PlanModifiers: account,
			},
			"uid": schema.Int64Attribute{
				MarkdownDescription: "Account user ID. Defaults to the host user's.",
				Optional:            true, Computed: true, PlanModifiers: accountID,
			},
			"gid": schema.Int64Attribute{
				MarkdownDescription: "Account group ID. Defaults to the host user's.",
				Optional:            true, Computed: true, PlanModifiers: accountID,
			},
			"home": schema.StringAttribute{
				MarkdownDescription: "Home directory of the account. Defaults to `/home/<user>`.",
				Optional:            true, Computed: true, PlanModifiers: account,
				Validators: []validator.String{absolutePath},
			},
			"scheme": schema.StringAttribute{
				MarkdownDescription: "Registry scheme, `http` or `https`, for pulling the image. Defaults to `https`. " +
					"Only used when pulling: not recorded, and changing it does nothing.",
				Optional:   true,
				Validators: []validator.String{stringvalidator.OneOf("http", "https")},
			},
			"default": schema.BoolAttribute{
				MarkdownDescription: "Whether this is the default machine, which `container machine` commands use without " +
					"`--name`. Set to `true` to make it the default; to change the default, set it on another machine " +
					"(`false` cannot be set).",
				Optional:      true,
				Computed:      true,
				PlanModifiers: []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()},
				Validators:    []validator.Bool{boolvalidator.Equals(true)},
			},
			"cpus": schema.Int64Attribute{
				MarkdownDescription: "Number of CPUs. Defaults to the runtime's.",
				Optional:            true,
				Computed:            true,
				PlanModifiers:       []planmodifier.Int64{int64planmodifier.UseStateForUnknown()},
				Validators:          []validator.Int64{int64validator.AtLeast(1)},
			},
			"memory": schema.StringAttribute{
				MarkdownDescription: "Memory, at least `1G`, with an optional `K`, `M`, `G`, `T` or `P` suffix. Defaults to half the host's.",
				Optional:            true,
				Computed:            true,
				PlanModifiers:       known,
				Validators: []validator.String{
					stringvalidator.RegexMatches(sizePattern, "must be a number of bytes with an optional K, M, G, T or P suffix"),
				},
			},
			"home_mount": schema.StringAttribute{
				MarkdownDescription: "How the host user's home directory is mounted: `rw`, `ro` or `none`. Defaults to `rw`.",
				Optional:            true,
				Computed:            true,
				PlanModifiers:       known,
				Validators:          []validator.String{stringvalidator.OneOf("rw", "ro", "none")},
			},
			"virtualization": schema.BoolAttribute{
				MarkdownDescription: "Enable nested virtualization (Apple M3 or later, a kernel with KVM). The runtime " +
					"does not report it, so changes made outside Terraform go unnoticed.",
				Optional: true,
			},
			"kernel": schema.StringAttribute{
				MarkdownDescription: "Absolute path of a custom kernel. The runtime does not report it, so changes made " +
					"outside Terraform go unnoticed; removing it replaces the machine, as it cannot be reset.",
				Optional:   true,
				Validators: []validator.String{absolutePath},
			},
			"running": schema.BoolAttribute{
				MarkdownDescription: "Whether the machine should run. Changes start or stop it in place; a machine " +
					"stopped outside Terraform is started again. Defaults to `true`.",
				Optional: true,
				Computed: true,
				Default:  booldefault.StaticBool(true),
			},
			"status": schema.StringAttribute{MarkdownDescription: "Status as the runtime reports it, e.g. `running`.", Computed: true},
			"ip_address": schema.StringAttribute{
				MarkdownDescription: "IP address of the running machine; it may change when the machine restarts.",
				Computed:            true,
			},
			"digest":    schema.StringAttribute{MarkdownDescription: "Digest of the image.", Computed: true, PlanModifiers: known},
			"disk_size": schema.Int64Attribute{MarkdownDescription: "Disk space the machine uses, in bytes.", Computed: true},
		},
	}
}

func (r *machineResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var memory types.String
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("memory"), &memory)...)
	if b, ok := parseSize(memory.ValueString()); ok && b < 1<<30 {
		resp.Diagnostics.AddAttributeError(path.Root("memory"), "Invalid memory", "A machine needs at least 1G of memory, not "+memory.ValueString()+".")
	}
}

func (r *machineResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFrom(req.ProviderData, &resp.Diagnostics)
}

func (r *machineResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan machineModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	name := plan.Name.ValueString()
	spec := client.MachineSpec{
		Name:       name,
		Image:      plan.Image.ValueString(),
		Platform:   plan.Platform.ValueString(), // "" when unknown: the default
		User:       plan.User.ValueString(),
		Home:       plan.Home.ValueString(),
		Scheme:     plan.Scheme.ValueString(),
		SetDefault: plan.Default.ValueBool(),
	}
	if !plan.UID.IsUnknown() {
		spec.UID = plan.UID.ValueInt64Pointer()
	}
	if !plan.GID.IsUnknown() {
		spec.GID = plan.GID.ValueInt64Pointer()
	}
	if err := r.client.CreateMachine(ctx, spec); err != nil {
		resp.Diagnostics.AddError("Creating machine failed", err.Error())
		return
	}
	// Record the machine from here on, so Terraform taints rather than
	// loses it if a step fails.
	missing := func() {
		resp.Diagnostics.AddError("Creating machine failed", "machine "+name+" not found after creation")
	}
	settings := changedSettings(plan, machineModel{})
	if settings != (client.MachineSettings{}) {
		resp.Diagnostics.Append(r.apply(ctx, name, settings, true)...)
	}
	if !resp.Diagnostics.HasError() && !plan.Running.ValueBool() {
		if err := r.client.StopMachine(ctx, name); err != nil {
			resp.Diagnostics.AddError("Stopping machine failed", err.Error())
		}
	}
	r.read(ctx, name, plan, &resp.State, &resp.Diagnostics, missing)
}

func (r *machineResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state machineModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	r.read(ctx, state.ID.ValueString(), state, &resp.State, &resp.Diagnostics, func() { resp.State.RemoveResource(ctx) })
}

// Update applies changed settings (restarting a running machine), makes it
// the default, and starts or stops it.
func (r *machineResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state machineModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	name := plan.ID.ValueString()
	running, wantRunning := state.Status.ValueString() == "running", plan.Running.ValueBool()
	if settings := changedSettings(plan, state); settings != (client.MachineSettings{}) {
		resp.Diagnostics.Append(r.apply(ctx, name, settings, running && wantRunning)...)
		running = running && wantRunning
	}
	if plan.Default.ValueBool() && !state.Default.ValueBool() && !resp.Diagnostics.HasError() {
		if err := r.client.SetDefaultMachine(ctx, name); err != nil {
			resp.Diagnostics.AddError("Making the machine the default failed", err.Error())
		}
	}
	if !resp.Diagnostics.HasError() && running != wantRunning {
		var err error
		if wantRunning {
			err = r.client.StartMachine(ctx, name)
		} else {
			err = r.client.StopMachine(ctx, name)
		}
		if err != nil {
			resp.Diagnostics.AddError("Changing the machine's state failed", err.Error())
		}
	}
	r.read(ctx, name, plan, &resp.State, &resp.Diagnostics, func() {
		resp.Diagnostics.AddError("Updating machine failed", "machine "+name+" not found")
	})
}

func (r *machineResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state machineModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	m, err := r.client.Machine(ctx, state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Reading machine failed", err.Error())
		return
	}
	if m == nil {
		return // already gone
	}
	if err := r.client.DeleteMachine(ctx, m.Name); err != nil {
		resp.Diagnostics.AddError("Deleting machine failed", err.Error())
	}
}

func (r *machineResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}

// ModifyPlan replaces the machine when image or platform change, but not
// when only their spelling does, and when a kernel is removed, which the
// runtime cannot reset.
func (r *machineResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.State.Raw.IsNull() || req.Plan.Raw.IsNull() {
		return
	}
	var plan, state machineModel
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
	if !plan.Platform.Equal(state.Platform) && (plan.Platform.IsUnknown() || !samePlatform(plan.Platform.ValueString(), state.Platform.ValueString())) {
		resp.RequiresReplace = append(resp.RequiresReplace, path.Root("platform"))
	}
	if plan.Kernel.IsNull() && !state.Kernel.IsNull() {
		resp.RequiresReplace = append(resp.RequiresReplace, path.Root("kernel"))
	}
}

// apply changes settings and, if restart, restarts the machine so they take
// effect.
func (r *machineResource) apply(ctx context.Context, name string, s client.MachineSettings, restart bool) diag.Diagnostics {
	var diags diag.Diagnostics
	if err := r.client.SetMachine(ctx, name, s); err != nil {
		diags.AddError("Changing machine settings failed", err.Error())
		return diags
	}
	if !restart {
		return diags
	}
	if err := r.client.StopMachine(ctx, name); err != nil {
		diags.AddError("Restarting machine failed", err.Error())
		return diags
	}
	if err := r.client.StartMachine(ctx, name); err != nil {
		diags.AddError("Restarting machine failed", err.Error())
	}
	return diags
}

// changedSettings are the settings in plan that differ in meaning from
// prior; unknown values (not configured) are left as they are.
func changedSettings(plan, prior machineModel) client.MachineSettings {
	var s client.MachineSettings
	if !plan.CPUs.IsUnknown() && !plan.CPUs.IsNull() && !plan.CPUs.Equal(prior.CPUs) {
		s.CPUs = plan.CPUs.ValueInt64()
	}
	if !plan.Memory.IsUnknown() && !plan.Memory.IsNull() && !sameSize(plan.Memory.ValueString(), prior.Memory.ValueString()) {
		s.Memory = plan.Memory.ValueString()
	}
	if !plan.HomeMount.IsUnknown() && !plan.HomeMount.IsNull() && !plan.HomeMount.Equal(prior.HomeMount) {
		s.HomeMount = plan.HomeMount.ValueString()
	}
	// Unset means off, but only worth sending once it was on.
	if plan.Virtualization.ValueBool() != prior.Virtualization.ValueBool() {
		on := plan.Virtualization.ValueBool()
		s.Virtualization = &on
	}
	if !plan.Kernel.IsNull() && !plan.Kernel.Equal(prior.Kernel) {
		s.Kernel = plan.Kernel.ValueString()
	}
	return s
}

// read stores the named machine in st, or calls missing if it does not
// exist. prior is the plan or state: values written differently from how
// the runtime reports them are kept as written, and those it does not
// report (scheme, virtualization, kernel) are kept.
func (r *machineResource) read(ctx context.Context, name string, prior machineModel, st *tfsdk.State, diags *diag.Diagnostics, missing func()) {
	m, err := r.client.Machine(ctx, name)
	if err != nil {
		diags.AddError("Reading machine failed", err.Error())
		return
	}
	if m == nil {
		missing()
		return
	}
	v := machineModel{
		ID:             types.StringValue(m.Name),
		Name:           types.StringValue(m.Name),
		Image:          types.StringValue(m.Image),
		Platform:       types.StringValue(m.Platform),
		User:           types.StringValue(m.User),
		UID:            types.Int64Value(m.UID),
		GID:            types.Int64Value(m.GID),
		Home:           types.StringValue(m.Home),
		Scheme:         prior.Scheme,
		Default:        types.BoolValue(m.Default),
		CPUs:           types.Int64Value(m.CPUs),
		Memory:         types.StringValue(formatSize(m.MemoryBytes)),
		HomeMount:      types.StringValue(m.HomeMount),
		Virtualization: prior.Virtualization,
		Kernel:         prior.Kernel,
		Running:        types.BoolValue(m.Status == "running"),
		Status:         types.StringValue(m.Status),
		IPAddress:      nullIfEmpty(m.IPAddress),
		Digest:         types.StringValue(m.Digest),
		DiskSize:       types.Int64Value(m.DiskSize),
	}
	if !prior.Image.IsNull() && !prior.Image.IsUnknown() {
		resolved, err := r.client.ImageName(ctx, prior.Image.ValueString())
		if err != nil {
			diags.AddError("Reading machine failed", err.Error())
			return
		}
		if resolved == m.Image {
			v.Image = prior.Image
		}
	}
	if samePlatform(prior.Platform.ValueString(), m.Platform) {
		v.Platform = prior.Platform
	}
	if sameSize(prior.Memory.ValueString(), v.Memory.ValueString()) {
		v.Memory = prior.Memory
	}
	diags.Append(st.Set(ctx, &v)...)
}
