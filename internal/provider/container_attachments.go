package provider

import (
	"context"
	"regexp"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/setvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/thorstenpfister/terraform-provider-espalier/internal/client"
)

// Networks, mounts, published ports and capabilities of espalier_container.
// They all replace the container when they change (see ModifyPlan).

type networkAttachmentModel struct {
	Network types.String `tfsdk:"network"`
	MAC     types.String `tfsdk:"mac_address"`
	MTU     types.Int64  `tfsdk:"mtu"`
	Aliases types.List   `tfsdk:"aliases"`
}

type mountModel struct {
	Type     types.String `tfsdk:"type"`
	Source   types.String `tfsdk:"source"`
	Target   types.String `tfsdk:"target"`
	ReadOnly types.Bool   `tfsdk:"read_only"`
}

type portModel struct {
	HostIP        types.String `tfsdk:"host_ip"`
	HostPort      types.Int64  `tfsdk:"host_port"`
	ContainerPort types.Int64  `tfsdk:"container_port"`
	Protocol      types.String `tfsdk:"protocol"`
}

var (
	networkAttachmentType = types.ObjectType{AttrTypes: map[string]attr.Type{
		"network": types.StringType, "mac_address": types.StringType, "mtu": types.Int64Type,
		"aliases": types.ListType{ElemType: types.StringType},
	}}
	mountType = types.ObjectType{AttrTypes: map[string]attr.Type{
		"type": types.StringType, "source": types.StringType, "target": types.StringType, "read_only": types.BoolType,
	}}
	portType = types.ObjectType{AttrTypes: map[string]attr.Type{
		"host_ip": types.StringType, "host_port": types.Int64Type, "container_port": types.Int64Type, "protocol": types.StringType,
	}}

	macPattern        = regexp.MustCompile(`^[0-9a-f]{2}(:[0-9a-f]{2}){5}$`)
	capabilityPattern = regexp.MustCompile(`^(CAP_[A-Z_]+|ALL)$`)
	port              = []validator.Int64{int64validator.Between(1, 65535)}
)

func attachmentAttributes() map[string]schema.Attribute {
	capability := []validator.Set{setvalidator.ValueStringsAre(stringvalidator.RegexMatches(capabilityPattern,
		"must be a capability as the runtime records it, e.g. CAP_NET_RAW, or ALL"))}
	return map[string]schema.Attribute{
		"networks": schema.ListNestedAttribute{
			MarkdownDescription: "Networks to attach, in order; the first is the primary one. Defaults to the runtime's " +
				"`default` network.",
			Optional:      true,
			Computed:      true,
			PlanModifiers: []planmodifier.List{listplanmodifier.UseStateForUnknown()},
			Validators:    []validator.List{listvalidator.SizeAtLeast(1)},
			NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
				"network": schema.StringAttribute{
					MarkdownDescription: "Network name, e.g. an `espalier_network`'s `name`.",
					Required:            true,
				},
				"mac_address": schema.StringAttribute{
					MarkdownDescription: "MAC address, in lower case. Assigned when the container starts if not set; " +
						"see `addresses` for the one in use.",
					Optional:   true,
					Validators: []validator.String{stringvalidator.RegexMatches(macPattern, "must be a MAC address in lower case")},
				},
				"mtu": schema.Int64Attribute{
					MarkdownDescription: "MTU of the interface. Defaults to the runtime's.",
					Optional:            true,
					Computed:            true,
					PlanModifiers:       []planmodifier.Int64{int64planmodifier.UseStateForUnknown()},
				},
				"aliases": schema.ListAttribute{
					MarkdownDescription: "Additional DNS names of the container on this network. They must be fully " +
						"qualified, e.g. `api.example.test`.",
					ElementType: types.StringType,
					Optional:    true,
				},
			}},
		},
		"mounts": schema.ListNestedAttribute{
			MarkdownDescription: "Filesystems to mount.",
			Optional:            true,
			NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
				"type": schema.StringAttribute{
					MarkdownDescription: "`bind` (a host directory), `volume` (an `espalier_volume`) or `tmpfs`.",
					Required:            true,
					Validators:          []validator.String{stringvalidator.OneOf(client.MountBind, client.MountVolume, client.MountTmpfs)},
				},
				"source": schema.StringAttribute{
					MarkdownDescription: "Absolute host path for `bind`, volume name for `volume`; not set for `tmpfs`.",
					Optional:            true,
				},
				"target": schema.StringAttribute{
					MarkdownDescription: "Absolute path inside the container.",
					Required:            true,
					Validators:          []validator.String{stringvalidator.RegexMatches(regexp.MustCompile(`^/`), "must be an absolute path")},
				},
				"read_only": schema.BoolAttribute{
					MarkdownDescription: "Mount read-only. Defaults to `false`.",
					Optional:            true,
					Computed:            true,
					Default:             booldefault.StaticBool(false),
				},
			}},
		},
		"ports": schema.ListNestedAttribute{
			MarkdownDescription: "Container ports to publish on the host.",
			Optional:            true,
			NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
				"host_ip": schema.StringAttribute{
					MarkdownDescription: "Host address to listen on. Defaults to `0.0.0.0`, all IPv4 addresses.",
					Optional:            true,
					Computed:            true,
					Default:             stringdefault.StaticString("0.0.0.0"),
				},
				"host_port":      schema.Int64Attribute{MarkdownDescription: "Host port.", Required: true, Validators: port},
				"container_port": schema.Int64Attribute{MarkdownDescription: "Container port.", Required: true, Validators: port},
				"protocol": schema.StringAttribute{
					MarkdownDescription: "`tcp` or `udp`. Defaults to `tcp`.",
					Optional:            true,
					Computed:            true,
					Default:             stringdefault.StaticString("tcp"),
					Validators:          []validator.String{stringvalidator.OneOf("tcp", "udp")},
				},
			}},
		},
		"cap_add": schema.SetAttribute{
			MarkdownDescription: "Linux capabilities to add, e.g. `CAP_NET_ADMIN`, or `ALL`.",
			ElementType:         types.StringType,
			Optional:            true,
			Validators:          capability,
		},
		"cap_drop": schema.SetAttribute{
			MarkdownDescription: "Linux capabilities to drop, e.g. `CAP_MKNOD`, or `ALL`.",
			ElementType:         types.StringType,
			Optional:            true,
			Validators:          capability,
		},
	}
}

// validateMounts checks what the schema cannot: which types take a source.
func validateMounts(ctx context.Context, mounts types.List, diags *diag.Diagnostics) {
	if mounts.IsNull() || mounts.IsUnknown() {
		return
	}
	var ms []mountModel
	diags.Append(mounts.ElementsAs(ctx, &ms, false)...)
	for _, m := range ms {
		if m.Type.IsUnknown() || m.Source.IsUnknown() {
			continue
		}
		at := path.Root("mounts")
		switch typ, src := m.Type.ValueString(), m.Source.ValueString(); {
		case typ == client.MountTmpfs && !m.Source.IsNull():
			diags.AddAttributeError(at, "Invalid mount", "A tmpfs mount ("+m.Target.ValueString()+") takes no source.")
		case typ != client.MountTmpfs && src == "":
			diags.AddAttributeError(at, "Invalid mount", "A "+typ+" mount ("+m.Target.ValueString()+") needs a source.")
		case typ == client.MountBind && src[0] != '/':
			diags.AddAttributeError(at, "Invalid mount", "A bind mount's source must be an absolute host path, not "+src+".")
		}
	}
}

// attachmentsSpec adds the planned attachments to spec.
func attachmentsSpec(ctx context.Context, plan containerModel, spec *client.ContainerSpec, diags *diag.Diagnostics) {
	if !plan.Networks.IsUnknown() {
		var ns []networkAttachmentModel
		diags.Append(plan.Networks.ElementsAs(ctx, &ns, false)...)
		for _, n := range ns {
			a := client.NetworkAttachment{Network: n.Network.ValueString(), MAC: n.MAC.ValueString(), MTU: n.MTU.ValueInt64()}
			diags.Append(n.Aliases.ElementsAs(ctx, &a.Aliases, false)...)
			spec.Networks = append(spec.Networks, a)
		}
	}
	var ms []mountModel
	diags.Append(plan.Mounts.ElementsAs(ctx, &ms, false)...)
	for _, m := range ms {
		spec.Mounts = append(spec.Mounts, client.Mount{
			Type: m.Type.ValueString(), Source: m.Source.ValueString(), Target: m.Target.ValueString(), ReadOnly: m.ReadOnly.ValueBool(),
		})
	}
	var ps []portModel
	diags.Append(plan.Ports.ElementsAs(ctx, &ps, false)...)
	for _, p := range ps {
		spec.Ports = append(spec.Ports, client.Port{
			HostIP: p.HostIP.ValueString(), HostPort: p.HostPort.ValueInt64(), ContainerPort: p.ContainerPort.ValueInt64(), Protocol: p.Protocol.ValueString(),
		})
	}
	diags.Append(plan.CapAdd.ElementsAs(ctx, &spec.CapAdd, false)...)
	diags.Append(plan.CapDrop.ElementsAs(ctx, &spec.CapDrop, false)...)
}

// readAttachments stores the container's attachments in m. Empty
// collections are null, as they are when not configured.
func readAttachments(ctx context.Context, c *client.Container, m *containerModel, diags *diag.Diagnostics) {
	var d diag.Diagnostics
	ns := make([]networkAttachmentModel, 0, len(c.Networks))
	for _, n := range c.Networks {
		aliases := types.ListNull(types.StringType)
		if len(n.Aliases) > 0 {
			aliases, d = types.ListValueFrom(ctx, types.StringType, n.Aliases)
			diags.Append(d...)
		}
		ns = append(ns, networkAttachmentModel{
			Network: types.StringValue(n.Network),
			MAC:     nullIfEmpty(n.MAC),
			MTU:     types.Int64Value(n.MTU),
			Aliases: aliases,
		})
	}
	m.Networks, d = types.ListValueFrom(ctx, networkAttachmentType, ns)
	diags.Append(d...)

	m.Mounts = types.ListNull(mountType)
	if len(c.Mounts) > 0 {
		ms := make([]mountModel, 0, len(c.Mounts))
		for _, mt := range c.Mounts {
			ms = append(ms, mountModel{
				Type: types.StringValue(mt.Type), Source: nullIfEmpty(mt.Source), Target: types.StringValue(mt.Target), ReadOnly: types.BoolValue(mt.ReadOnly),
			})
		}
		m.Mounts, d = types.ListValueFrom(ctx, mountType, ms)
		diags.Append(d...)
	}

	m.Ports = types.ListNull(portType)
	if len(c.Ports) > 0 {
		ps := make([]portModel, 0, len(c.Ports))
		for _, p := range c.Ports {
			ps = append(ps, portModel{
				HostIP: types.StringValue(p.HostIP), HostPort: types.Int64Value(p.HostPort),
				ContainerPort: types.Int64Value(p.ContainerPort), Protocol: types.StringValue(p.Protocol),
			})
		}
		m.Ports, d = types.ListValueFrom(ctx, portType, ps)
		diags.Append(d...)
	}

	m.CapAdd = stringSet(ctx, c.CapAdd, diags)
	m.CapDrop = stringSet(ctx, c.CapDrop, diags)
}

// replaceChangedAttachments replaces the container when an attachment
// changes. It runs after the attribute plan modifiers, so computed values
// (MTUs, defaults) are already filled in from state.
func replaceChangedAttachments(ctx context.Context, plan, state containerModel, resp *resource.ModifyPlanResponse) {
	for name, values := range map[string][2]attr.Value{
		"networks": {plan.Networks, state.Networks},
		"mounts":   {plan.Mounts, state.Mounts},
		"ports":    {plan.Ports, state.Ports},
		"cap_add":  {plan.CapAdd, state.CapAdd},
		"cap_drop": {plan.CapDrop, state.CapDrop},
	} {
		if !sameOrEmpty(ctx, values[0], values[1]) {
			resp.RequiresReplace = append(resp.RequiresReplace, path.Root(name))
		}
	}
}

func nullIfEmpty(s string) types.String {
	if s == "" {
		return types.StringNull()
	}
	return types.StringValue(s)
}

func stringSet(ctx context.Context, values []string, diags *diag.Diagnostics) types.Set {
	if len(values) == 0 {
		return types.SetNull(types.StringType)
	}
	s, d := types.SetValueFrom(ctx, types.StringType, values)
	diags.Append(d...)
	return s
}
