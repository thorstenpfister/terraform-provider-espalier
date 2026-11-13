package provider

import (
	"context"
	"regexp"

	"github.com/hashicorp/terraform-plugin-framework-validators/mapvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/thorstenpfister/terraform-provider-espalier/internal/client"
)

type volumeResource struct {
	client client.Client
}

type volumeModel struct {
	ID        types.String `tfsdk:"id"`
	Name      types.String `tfsdk:"name"`
	Size      types.String `tfsdk:"size"`
	Labels    types.Map    `tfsdk:"labels"`
	Options   types.Map    `tfsdk:"options"`
	SizeBytes types.Int64  `tfsdk:"size_bytes"`
	Format    types.String `tfsdk:"format"`
	Source    types.String `tfsdk:"source"`
}

var (
	_ resource.ResourceWithConfigure   = &volumeResource{}
	_ resource.ResourceWithImportState = &volumeResource{}
)

// NewVolumeResource returns the espalier_volume resource.
func NewVolumeResource() resource.Resource { return &volumeResource{} }

func (r *volumeResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_volume"
}

func (r *volumeResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	known := []planmodifier.String{stringplanmodifier.UseStateForUnknown()}
	resp.Schema = schema.Schema{
		MarkdownDescription: "A volume of the container runtime (`container volume create`): a disk image on the host " +
			"that containers mount. The runtime cannot change a volume, so changing any argument replaces it, " +
			"and with it the data.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The volume's name.",
				Computed:            true,
				PlanModifiers:       known,
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Name of the volume.",
				Required:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"size": schema.StringAttribute{
				MarkdownDescription: "Size in bytes, with an optional `K`, `M`, `G`, `T` or `P` suffix, e.g. `2G`. " +
					"If not set, the runtime creates a sparse 512 GiB image.",
				Optional:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
				Validators: []validator.String{
					stringvalidator.RegexMatches(regexp.MustCompile(`^[0-9]+[KMGTP]?$`), "must be a number of bytes with an optional K, M, G, T or P suffix"),
				},
			},
			"labels": schema.MapAttribute{
				MarkdownDescription: "Labels attached to the volume.",
				ElementType:         types.StringType,
				Optional:            true,
				PlanModifiers:       replaceMap,
			},
			"options": schema.MapAttribute{
				MarkdownDescription: "Driver-specific options (`--opt key=value`). Use `size` for the size, not an option.",
				ElementType:         types.StringType,
				Optional:            true,
				PlanModifiers:       replaceMap,
				Validators:          []validator.Map{mapvalidator.KeysAre(stringvalidator.NoneOf("size"))},
			},
			"size_bytes": schema.Int64Attribute{
				MarkdownDescription: "Size of the volume in bytes.",
				Computed:            true,
				PlanModifiers:       []planmodifier.Int64{int64planmodifier.UseStateForUnknown()},
			},
			"format": schema.StringAttribute{
				MarkdownDescription: "Filesystem of the volume, e.g. `ext4`.",
				Computed:            true,
				PlanModifiers:       known,
			},
			"source": schema.StringAttribute{
				MarkdownDescription: "Host path of the volume's disk image.",
				Computed:            true,
				PlanModifiers:       known,
			},
		},
	}
	resp.Schema.Attributes = withSchemaRules(resp.Schema.Attributes)
}

func (r *volumeResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFrom(req.ProviderData, &resp.Diagnostics)
}

func (r *volumeResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan volumeModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	spec := client.VolumeSpec{Name: plan.Name.ValueString(), Size: plan.Size.ValueString()}
	resp.Diagnostics.Append(plan.Labels.ElementsAs(ctx, &spec.Labels, false)...)
	resp.Diagnostics.Append(plan.Options.ElementsAs(ctx, &spec.Options, false)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.CreateVolume(ctx, spec); err != nil {
		resp.Diagnostics.AddError("Creating volume failed", err.Error())
		return
	}
	r.read(ctx, spec.Name, plan, &resp.State, &resp.Diagnostics, func() {
		resp.Diagnostics.AddError("Creating volume failed", "volume "+spec.Name+" not found after creation")
	})
}

func (r *volumeResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state volumeModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	// Deleted outside Terraform: drop it from state so it is planned again.
	r.read(ctx, state.ID.ValueString(), state, &resp.State, &resp.Diagnostics, func() { resp.State.RemoveResource(ctx) })
}

// Update is never called: every argument requires replacement.
func (r *volumeResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	// Every other change replaces it; only an empty collection written in
	// place of an unset one (or back) gets here.
	resp.State.Raw = req.Plan.Raw
}

func (r *volumeResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state volumeModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	v, err := r.client.Volume(ctx, state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Reading volume failed", err.Error())
		return
	}
	if v == nil {
		return // already gone
	}
	if err := r.client.DeleteVolume(ctx, v.Name); err != nil {
		resp.Diagnostics.AddError("Deleting volume failed", err.Error())
	}
}

func (r *volumeResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}

// read stores the named volume in st, or calls missing if it does not exist.
// prior is the plan or state, for maps left unset (see labelsValue).
func (r *volumeResource) read(ctx context.Context, name string, prior volumeModel, st *tfsdk.State, diags *diag.Diagnostics, missing func()) {
	v, err := r.client.Volume(ctx, name)
	if err != nil {
		diags.AddError("Reading volume failed", err.Error())
		return
	}
	if v == nil {
		missing()
		return
	}
	m := volumeValue(ctx, v, prior, diags)
	diags.Append(st.Set(ctx, &m)...)
	keepEmptyCollections(ctx, st, &prior, diags)
}

// volumeValue is the volume as the runtime reports it; maps left unset in
// prior (the plan or state) stay null when empty.
func volumeValue(ctx context.Context, v *client.Volume, prior volumeModel, diags *diag.Diagnostics) volumeModel {
	m := volumeModel{
		ID:        types.StringValue(v.Name),
		Name:      types.StringValue(v.Name),
		Size:      types.StringNull(),
		Labels:    labelsValue(ctx, v.Labels, prior.Labels, diags),
		Options:   labelsValue(ctx, v.Options, prior.Options, diags),
		SizeBytes: types.Int64Value(v.SizeBytes),
		Format:    types.StringValue(v.Format),
		Source:    types.StringValue(v.Source),
	}
	if v.Size != "" {
		m.Size = types.StringValue(v.Size)
	}
	return m
}
