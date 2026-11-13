package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/thorstenpfister/terraform-provider-espalier/internal/client"
)

type imageResource struct {
	client client.Client
}

type imageModel struct {
	ID          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	Platform    types.String `tfsdk:"platform"`
	KeepLocally types.Bool   `tfsdk:"keep_locally"`
	Scheme      types.String `tfsdk:"scheme"`
	Digest      types.String `tfsdk:"digest"`
}

var (
	_ resource.ResourceWithConfigure   = &imageResource{}
	_ resource.ResourceWithImportState = &imageResource{}
)

// NewImageResource returns the espalier_image resource.
func NewImageResource() resource.Resource { return &imageResource{} }

func (r *imageResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_image"
}

func (r *imageResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	known := []planmodifier.String{stringplanmodifier.UseStateForUnknown()}
	resp.Schema = schema.Schema{
		MarkdownDescription: "An image pulled into the container runtime (`container image pull`). " +
			"An image removed outside Terraform is pulled again. A tag that moves upstream is not followed: " +
			"pin a digest (`name@sha256:…`) or change `name` to pull a new version.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Fully qualified reference the runtime stores the image under, " +
					"e.g. `docker.io/library/busybox:1.37` for `busybox:1.37`.",
				Computed:      true,
				PlanModifiers: known,
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Image reference to pull, with a tag or digest. Short names resolve against the " +
					"runtime's default registry (`registry.domain`, normally `docker.io`); no tag means `latest`.",
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"platform": schema.StringAttribute{
				MarkdownDescription: "Pull only this platform (`os/arch[/variant]`, e.g. `linux/arm64`). Pulls all platforms if not set.",
				Optional:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"keep_locally": schema.BoolAttribute{
				MarkdownDescription: "Keep the image in the runtime when the resource is destroyed, e.g. when other " +
					"configurations use it too. Defaults to `false`.",
				Optional: true,
				Computed: true,
				Default:  booldefault.StaticBool(false),
			},
			"scheme": schema.StringAttribute{
				MarkdownDescription: "Registry scheme, `http` or `https`, e.g. `http` for a local registry without TLS. " +
					"Defaults to `https`. Changing it does not pull again.",
				Optional:   true,
				Validators: []validator.String{stringvalidator.OneOf("http", "https")},
			},
			"digest": schema.StringAttribute{
				MarkdownDescription: "Digest of the pulled image index or manifest.",
				Computed:            true,
				PlanModifiers:       known,
			},
		},
	}
}

func (r *imageResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFrom(req.ProviderData, &resp.Diagnostics)
}

func (r *imageResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan imageModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	spec := client.ImageSpec{Reference: plan.Name.ValueString(), Platform: plan.Platform.ValueString(), Scheme: plan.Scheme.ValueString()}
	if err := r.client.PullImage(ctx, spec); err != nil {
		resp.Diagnostics.AddError("Pulling image failed", err.Error())
		return
	}
	img, err := r.client.Image(ctx, spec.Reference)
	if err != nil {
		resp.Diagnostics.AddError("Reading image failed", err.Error())
		return
	}
	if img == nil {
		resp.Diagnostics.AddError("Pulling image failed", "image "+spec.Reference+" not found after pulling")
		return
	}
	plan.ID = types.StringValue(img.Name)
	plan.Digest = types.StringValue(img.Digest)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *imageResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state imageModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	img, err := r.client.Image(ctx, state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Reading image failed", err.Error())
		return
	}
	if img == nil {
		resp.State.RemoveResource(ctx) // removed outside Terraform: pull again
		return
	}
	state.ID = types.StringValue(img.Name)
	state.Digest = types.StringValue(img.Digest)
	if state.KeepLocally.IsNull() {
		state.KeepLocally = types.BoolValue(false)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update only changes keep_locally and scheme; everything else replaces the
// image.
func (r *imageResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan imageModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *imageResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state imageModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() || state.KeepLocally.ValueBool() {
		return
	}
	img, err := r.client.Image(ctx, state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Reading image failed", err.Error())
		return
	}
	if img == nil {
		return // already gone
	}
	if err := r.client.DeleteImage(ctx, img.Name); err != nil {
		resp.Diagnostics.AddError("Deleting image failed", err.Error())
	}
}

// ImportState takes the reference as written in the configuration, so the
// imported name matches it.
func (r *imageResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("name"), req.ID)...)
}
