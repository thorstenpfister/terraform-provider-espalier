package provider

import (
	"context"
	"regexp"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"

	"github.com/thorstenpfister/terraform-provider-espalier/internal/client"
)

// The less common options of espalier_container: platform, user, root
// filesystem, DNS, init, Rosetta, SSH agent, /dev/shm, ulimits, published
// sockets, the registry scheme, and the VM: kernel, init image, runtime
// handler, virtualization and the protected paths. All but scheme replace
// the container when they change.

type dnsModel struct {
	Nameservers types.List   `tfsdk:"nameservers"`
	Domain      types.String `tfsdk:"domain"`
	Search      types.List   `tfsdk:"search_domains"`
	Options     types.List   `tfsdk:"options"`
}

type ulimitModel struct {
	Name types.String `tfsdk:"name"`
	Soft types.Int64  `tfsdk:"soft"`
	Hard types.Int64  `tfsdk:"hard"`
}

type socketModel struct {
	HostPath      types.String `tfsdk:"host_path"`
	ContainerPath types.String `tfsdk:"container_path"`
}

var (
	listOfStrings = types.ListType{ElemType: types.StringType}
	dnsType       = types.ObjectType{AttrTypes: map[string]attr.Type{
		"nameservers": listOfStrings, "domain": types.StringType, "search_domains": listOfStrings, "options": listOfStrings,
	}}
	ulimitType = types.ObjectType{AttrTypes: map[string]attr.Type{
		"name": types.StringType, "soft": types.Int64Type, "hard": types.Int64Type,
	}}
	socketType = types.ObjectType{AttrTypes: map[string]attr.Type{
		"host_path": types.StringType, "container_path": types.StringType,
	}}

	absolutePath = stringvalidator.RegexMatches(regexp.MustCompile(`^/`), "must be an absolute path")
	// A protected path list may start over from nothing with NONE.
	protectedPath = stringvalidator.RegexMatches(regexp.MustCompile(`^(/|NONE$)`), "must be an absolute path or NONE")
)

func optionAttributes() map[string]schema.Attribute {
	off := func(desc string) schema.BoolAttribute {
		return schema.BoolAttribute{
			MarkdownDescription: desc + " Defaults to `false`.",
			Optional:            true,
			Computed:            true,
			Default:             booldefault.StaticBool(false),
			PlanModifiers:       []planmodifier.Bool{boolplanmodifier.RequiresReplace()},
		}
	}
	nonEmpty := []validator.List{listvalidator.SizeAtLeast(1)}
	replaceString := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	replaceList := []planmodifier.List{listplanmodifier.RequiresReplace()}
	protectedPaths := func(what, defaults string) schema.ListAttribute {
		return schema.ListAttribute{
			MarkdownDescription: "**Experimental in the runtime.** Absolute paths in the container to " + what + ", in " +
				"addition to the runtime's defaults (" + defaults + "). An element `NONE` drops the defaults and the paths " +
				"before it, e.g. `[\"NONE\"]` for none at all.",
			ElementType:   types.StringType,
			Optional:      true,
			PlanModifiers: replaceList,
			Validators:    []validator.List{listvalidator.SizeAtLeast(1), listvalidator.ValueStringsAre(protectedPath)},
		}
	}
	return map[string]schema.Attribute{
		"platform": schema.StringAttribute{
			MarkdownDescription: "Platform of a multi-platform image (`os/arch[/variant]`, e.g. `linux/amd64`, which runs " +
				"with Rosetta). Defaults to the host's. The runtime records the variant: `linux/arm64` and `linux/arm64/v8` are the same.",
			Optional:      true,
			Computed:      true,
			PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}, // replaced in ModifyPlan
		},
		"user": schema.StringAttribute{
			MarkdownDescription: "User of the process, as `name`, `uid` or `uid:gid`. Defaults to the image's, else `0:0`.",
			Optional:            true,
			Computed:            true,
			PlanModifiers: []planmodifier.String{
				stringplanmodifier.RequiresReplaceIfConfigured(), stringplanmodifier.UseStateForUnknown(),
			},
		},
		"read_only": off("Mount the root filesystem read-only."),
		"init":      off("Run an init process that forwards signals and reaps zombies."),
		"rosetta":   off("Enable Rosetta, to run x86-64 binaries."),
		"ssh":       off("Forward the host's SSH agent socket into the container."),
		"no_dns": off("Leave the container without any DNS configuration (`--no-dns`). Conflicts with `dns`. " +
			"Without either, the runtime configures its own resolver."),
		"dns": schema.SingleNestedAttribute{
			MarkdownDescription: "Resolver configuration. The runtime's defaults apply to whatever is not set.",
			Optional:            true,
			Attributes: map[string]schema.Attribute{
				"nameservers": schema.ListAttribute{
					MarkdownDescription: "Nameserver IP addresses.", ElementType: types.StringType, Optional: true, Validators: nonEmpty,
				},
				"domain": schema.StringAttribute{MarkdownDescription: "Default domain.", Optional: true},
				"search_domains": schema.ListAttribute{
					MarkdownDescription: "Search domains.", ElementType: types.StringType, Optional: true, Validators: nonEmpty,
				},
				"options": schema.ListAttribute{
					MarkdownDescription: "Resolver options, e.g. `ndots:2`.", ElementType: types.StringType, Optional: true, Validators: nonEmpty,
				},
			},
		},
		"shm_size": schema.StringAttribute{
			MarkdownDescription: "Size of `/dev/shm`, with an optional `K`, `M`, `G`, `T` or `P` suffix. Defaults to the runtime's.",
			Optional:            true, // replaced in ModifyPlan
			Validators: []validator.String{
				stringvalidator.RegexMatches(sizePattern, "must be a number of bytes with an optional K, M, G, T or P suffix"),
			},
		},
		"ulimits": schema.ListNestedAttribute{
			MarkdownDescription: "Resource limits of the process.",
			Optional:            true,
			NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
				"name": schema.StringAttribute{
					MarkdownDescription: "Limit, as `ulimit` names it without `RLIMIT_`, e.g. `nofile` or `nproc`.",
					Required:            true,
					Validators:          []validator.String{stringvalidator.RegexMatches(regexp.MustCompile(`^[a-z]+$`), "must be a limit name in lower case, e.g. nofile")},
				},
				"soft": schema.Int64Attribute{MarkdownDescription: "Soft limit.", Required: true},
				"hard": schema.Int64Attribute{MarkdownDescription: "Hard limit.", Required: true},
			}},
		},
		"publish_sockets": schema.ListNestedAttribute{
			MarkdownDescription: "Unix sockets in the container to publish on the host.",
			Optional:            true,
			NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
				"host_path":      schema.StringAttribute{MarkdownDescription: "Absolute path of the socket on the host.", Required: true, Validators: []validator.String{absolutePath}},
				"container_path": schema.StringAttribute{MarkdownDescription: "Absolute path of the socket in the container.", Required: true, Validators: []validator.String{absolutePath}},
			}},
		},
		"kernel": schema.StringAttribute{
			MarkdownDescription: "Absolute host path of a custom kernel for the container's VM. Defaults to the " +
				"runtime's kernel (see `espalier_system`). The runtime does not report it, so a change outside Terraform " +
				"is not noticed, and import does not recover it.",
			Optional:      true,
			PlanModifiers: replaceString,
			Validators:    []validator.String{absolutePath},
		},
		"kernel_args": schema.ListAttribute{
			MarkdownDescription: "Arguments appended to the kernel command line, e.g. `[\"quiet\"]`. Not reported, like `kernel`.",
			ElementType:         types.StringType,
			Optional:            true,
			PlanModifiers:       replaceList,
			Validators:          nonEmpty,
		},
		"init_image": schema.StringAttribute{
			MarkdownDescription: "Image of the VM's init system, instead of the runtime's `vminit` image. Not reported, like `kernel`.",
			Optional:            true,
			PlanModifiers:       replaceString,
		},
		"runtime": schema.StringAttribute{
			MarkdownDescription: "Runtime handler (plugin) that runs the container. Defaults to `container-runtime-linux`.",
			Optional:            true,
			Computed:            true,
			PlanModifiers: []planmodifier.String{
				stringplanmodifier.RequiresReplaceIfConfigured(), stringplanmodifier.UseStateForUnknown(),
			},
		},
		"virtualization":  off("Expose virtualization to the container, for nested VMs; needs host and kernel support."),
		"read_only_paths": protectedPaths("mount read-only", "`/proc/sys`, `/proc/sysrq-trigger` and others"),
		"masked_paths":    protectedPaths("hide", "`/proc/kcore`, `/sys/firmware` and others"),
		"scheme": schema.StringAttribute{
			MarkdownDescription: "Registry scheme, `http` or `https`, for pulling the image if it is missing, e.g. " +
				"`http` for a local registry without TLS. Defaults to `https`. Only used when pulling: the runtime does " +
				"not record it, and changing it does not replace the container.",
			Optional:   true,
			Validators: []validator.String{stringvalidator.OneOf("http", "https")},
		},
	}
}

func validateOptions(ctx context.Context, cfg containerModel, diags *diag.Diagnostics) {
	if cfg.NoDNS.ValueBool() && !cfg.DNS.IsNull() {
		diags.AddAttributeError(path.Root("no_dns"), "Conflicting DNS settings", "Set either no_dns or dns, not both.")
	}
	var ls []ulimitModel
	if !cfg.Ulimits.IsUnknown() {
		diags.Append(cfg.Ulimits.ElementsAs(ctx, &ls, false)...)
	}
	for _, l := range ls {
		if !l.Soft.IsUnknown() && !l.Hard.IsUnknown() && l.Soft.ValueInt64() > l.Hard.ValueInt64() {
			diags.AddAttributeError(path.Root("ulimits"), "Invalid ulimit",
				"The soft limit of "+l.Name.ValueString()+" exceeds its hard limit.")
		}
	}
}

// optionsSpec adds the planned options to spec.
func optionsSpec(ctx context.Context, plan containerModel, spec *client.ContainerSpec, diags *diag.Diagnostics) {
	spec.Platform = plan.Platform.ValueString() // "" when unknown: the default
	spec.User = plan.User.ValueString()
	spec.ReadOnly, spec.Init, spec.Rosetta, spec.SSH, spec.NoDNS =
		plan.ReadOnly.ValueBool(), plan.Init.ValueBool(), plan.Rosetta.ValueBool(), plan.SSH.ValueBool(), plan.NoDNS.ValueBool()
	spec.ShmSize = plan.ShmSize.ValueString()
	spec.Scheme = plan.Scheme.ValueString()
	spec.Kernel, spec.InitImage, spec.Runtime = plan.Kernel.ValueString(), plan.InitImage.ValueString(), plan.Runtime.ValueString()
	spec.Virtualization = plan.Virtualization.ValueBool()
	diags.Append(plan.KernelArgs.ElementsAs(ctx, &spec.KernelArgs, false)...)
	diags.Append(plan.ReadOnlyPaths.ElementsAs(ctx, &spec.ReadOnlyPaths, false)...)
	diags.Append(plan.MaskedPaths.ElementsAs(ctx, &spec.MaskedPaths, false)...)
	if !plan.DNS.IsNull() {
		var d dnsModel
		diags.Append(plan.DNS.As(ctx, &d, basetypes.ObjectAsOptions{})...)
		spec.DNS.Domain = d.Domain.ValueString()
		diags.Append(d.Nameservers.ElementsAs(ctx, &spec.DNS.Nameservers, false)...)
		diags.Append(d.Search.ElementsAs(ctx, &spec.DNS.Search, false)...)
		diags.Append(d.Options.ElementsAs(ctx, &spec.DNS.Options, false)...)
	}
	var ls []ulimitModel
	diags.Append(plan.Ulimits.ElementsAs(ctx, &ls, false)...)
	for _, l := range ls {
		spec.Ulimits = append(spec.Ulimits, client.Ulimit{Name: l.Name.ValueString(), Soft: l.Soft.ValueInt64(), Hard: l.Hard.ValueInt64()})
	}
	var ss []socketModel
	diags.Append(plan.PublishSockets.ElementsAs(ctx, &ss, false)...)
	for _, s := range ss {
		spec.PublishSockets = append(spec.PublishSockets, client.Socket{HostPath: s.HostPath.ValueString(), ContainerPath: s.ContainerPath.ValueString()})
	}
}

// readOptions stores the container's options in m, as the runtime reports
// them. Empty collections are null, but for the protected paths, where
// empty means none and null the defaults.
func readOptions(ctx context.Context, c *client.Container, m *containerModel, diags *diag.Diagnostics) {
	var d diag.Diagnostics
	m.Platform = types.StringValue(c.Platform)
	m.User = types.StringValue(c.User)
	m.ReadOnly, m.Init, m.Rosetta, m.SSH, m.NoDNS =
		types.BoolValue(c.ReadOnly), types.BoolValue(c.Init), types.BoolValue(c.Rosetta), types.BoolValue(c.SSH), types.BoolValue(c.NoDNS)
	m.ShmSize = types.StringNull()
	if c.ShmSizeBytes != 0 {
		m.ShmSize = types.StringValue(formatSize(c.ShmSizeBytes))
	}
	m.Scheme = types.StringNull()
	m.Kernel, m.KernelArgs, m.InitImage = types.StringNull(), types.ListNull(types.StringType), types.StringNull()
	m.Runtime, m.Virtualization = types.StringValue(c.Runtime), types.BoolValue(c.Virtualization)
	m.ReadOnlyPaths, d = pathList(ctx, c.ReadOnlyPaths)
	diags.Append(d...)
	m.MaskedPaths, d = pathList(ctx, c.MaskedPaths)
	diags.Append(d...)

	m.DNS = types.ObjectNull(dnsType.AttrTypes)
	if dns := c.DNS; len(dns.Nameservers)+len(dns.Search)+len(dns.Options) > 0 || dns.Domain != "" {
		m.DNS, d = types.ObjectValueFrom(ctx, dnsType.AttrTypes, dnsModel{
			Nameservers: stringList(ctx, dns.Nameservers, diags),
			Domain:      nullIfEmpty(dns.Domain),
			Search:      stringList(ctx, dns.Search, diags),
			Options:     stringList(ctx, dns.Options, diags),
		})
		diags.Append(d...)
	}

	m.Ulimits = types.ListNull(ulimitType)
	if len(c.Ulimits) > 0 {
		ls := make([]ulimitModel, 0, len(c.Ulimits))
		for _, l := range c.Ulimits {
			ls = append(ls, ulimitModel{Name: types.StringValue(l.Name), Soft: types.Int64Value(l.Soft), Hard: types.Int64Value(l.Hard)})
		}
		m.Ulimits, d = types.ListValueFrom(ctx, ulimitType, ls)
		diags.Append(d...)
	}

	m.PublishSockets = types.ListNull(socketType)
	if len(c.PublishSockets) > 0 {
		ss := make([]socketModel, 0, len(c.PublishSockets))
		for _, s := range c.PublishSockets {
			ss = append(ss, socketModel{HostPath: types.StringValue(s.HostPath), ContainerPath: types.StringValue(s.ContainerPath)})
		}
		m.PublishSockets, d = types.ListValueFrom(ctx, socketType, ss)
		diags.Append(d...)
	}
}

// keepWrittenOptions keeps values from prior (plan or state) that the
// runtime reports differently but that mean the same, or not at all. The
// protected paths are reported with the defaults they were added to.
func keepWrittenOptions(prior containerModel, m *containerModel) {
	m.Kernel, m.KernelArgs, m.InitImage = prior.Kernel, prior.KernelArgs, prior.InitImage
	m.ReadOnlyPaths, m.MaskedPaths = prior.ReadOnlyPaths, prior.MaskedPaths
	if samePlatform(prior.Platform.ValueString(), m.Platform.ValueString()) {
		m.Platform = prior.Platform
	}
	if sameSize(prior.ShmSize.ValueString(), m.ShmSize.ValueString()) {
		m.ShmSize = prior.ShmSize
	}
	m.Scheme = prior.Scheme
}

// replaceChangedOptions replaces the container when an option changes. It
// runs after the attribute plan modifiers.
func replaceChangedOptions(plan, state containerModel, resp *resource.ModifyPlanResponse) {
	for name, values := range map[string][2]attr.Value{
		"dns":             {plan.DNS, state.DNS},
		"ulimits":         {plan.Ulimits, state.Ulimits},
		"publish_sockets": {plan.PublishSockets, state.PublishSockets},
	} {
		if !values[0].Equal(values[1]) {
			resp.RequiresReplace = append(resp.RequiresReplace, path.Root(name))
		}
	}
	if !plan.Platform.Equal(state.Platform) && (plan.Platform.IsUnknown() || !samePlatform(plan.Platform.ValueString(), state.Platform.ValueString())) {
		resp.RequiresReplace = append(resp.RequiresReplace, path.Root("platform"))
	}
	if !plan.ShmSize.Equal(state.ShmSize) && !sameSize(plan.ShmSize.ValueString(), state.ShmSize.ValueString()) {
		resp.RequiresReplace = append(resp.RequiresReplace, path.Root("shm_size"))
	}
}

// samePlatform reports whether two platforms are the same, where a missing
// variant matches any: the runtime records linux/arm64 as linux/arm64/v8.
func samePlatform(a, b string) bool {
	if a == "" || b == "" {
		return a == b
	}
	pa, pb := strings.Split(a, "/"), strings.Split(b, "/")
	if len(pa) < 2 || len(pb) < 2 || pa[0] != pb[0] || pa[1] != pb[1] {
		return false
	}
	return len(pa) == 2 || len(pb) == 2 || pa[2] == pb[2]
}

// sameSize reports whether two sizes are the same number of bytes; two
// unset sizes are the same.
func sameSize(a, b string) bool {
	if a == "" || b == "" {
		return a == b
	}
	sa, okA := parseSize(a)
	sb, okB := parseSize(b)
	return okA && okB && sa == sb
}

func pathList(ctx context.Context, paths []string) (types.List, diag.Diagnostics) {
	if paths == nil {
		return types.ListNull(types.StringType), nil
	}
	return types.ListValueFrom(ctx, types.StringType, paths)
}

func stringList(ctx context.Context, values []string, diags *diag.Diagnostics) types.List {
	if len(values) == 0 {
		return types.ListNull(types.StringType)
	}
	l, d := types.ListValueFrom(ctx, types.StringType, values)
	diags.Append(d...)
	return l
}
