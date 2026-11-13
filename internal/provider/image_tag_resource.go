package provider

import (
	"context"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/thorstenpfister/terraform-provider-espalier/internal/client"
)

type imageTagResource struct {
	client client.Client
}

type imageTagModel struct {
	ID     types.String `tfsdk:"id"`
	Source types.String `tfsdk:"source"`
	Target types.String `tfsdk:"target"`
	Digest types.String `tfsdk:"digest"`
}

var (
	_ resource.ResourceWithConfigure   = &imageTagResource{}
	_ resource.ResourceWithImportState = &imageTagResource{}
)

// NewImageTagResource returns the espalier_image_tag resource.
func NewImageTagResource() resource.Resource { return &imageTagResource{} }

func (r *imageTagResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_image_tag"
}

func (r *imageTagResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	known := []planmodifier.String{stringplanmodifier.UseStateForUnknown()}
	resp.Schema = schema.Schema{
		MarkdownDescription: "Another name for a local image (`container image tag`). Destroying it removes only that " +
			"name, not the image. If the tag no longer points at its source, e.g. because the source was pulled " +
			"again, it is tagged again.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Fully qualified reference of the tag, e.g. `docker.io/library/app:dev` for `app:dev`.",
				Computed:            true,
				PlanModifiers:       known,
			},
			"source": schema.StringAttribute{
				MarkdownDescription: "Reference of the image to tag, e.g. an `espalier_image`'s `name`. It must be present locally.",
				Required:            true,
				PlanModifiers:       replace,
			},
			"target": schema.StringAttribute{
				MarkdownDescription: "The new reference, resolved like `espalier_image`'s `name`.",
				Required:            true,
				PlanModifiers:       replace,
			},
			"digest": schema.StringAttribute{
				MarkdownDescription: "Digest the tag points at.",
				Computed:            true,
				PlanModifiers:       known,
			},
		},
	}
	resp.Schema.Attributes = withSchemaRules(resp.Schema.Attributes)
}

func (r *imageTagResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFrom(req.ProviderData, &resp.Diagnostics)
}

func (r *imageTagResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan imageTagModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	source, target := plan.Source.ValueString(), plan.Target.ValueString()
	same, err := r.sameName(ctx, source, target)
	if err != nil {
		resp.Diagnostics.AddError("Resolving image failed", err.Error())
		return
	}
	if same {
		resp.Diagnostics.AddAttributeError(path.Root("target"), "Invalid tag", "target "+target+" names the source image itself.")
		return
	}
	if err := r.client.TagImage(ctx, source, target); err != nil {
		resp.Diagnostics.AddError("Tagging image failed", err.Error())
		return
	}
	r.read(ctx, plan, &resp.State, &resp.Diagnostics, func() {
		resp.Diagnostics.AddError("Tagging image failed", "tag "+target+" not found after tagging")
	})
}

func (r *imageTagResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state imageTagModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	// Removed, or no longer the source's: tag again.
	r.read(ctx, state, &resp.State, &resp.Diagnostics, func() { resp.State.RemoveResource(ctx) })
}

// Update is never called: every argument replaces the tag.
func (r *imageTagResource) Update(context.Context, resource.UpdateRequest, *resource.UpdateResponse) {
}

func (r *imageTagResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state imageTagModel
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
		return // already gone
	}
	if err := r.client.DeleteImage(ctx, img.Name); err != nil {
		resp.Diagnostics.AddError("Deleting tag failed", err.Error())
	}
}

// ImportState takes "source,target", both as written in the configuration.
func (r *imageTagResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	source, target, ok := strings.Cut(req.ID, ",")
	if !ok || source == "" || target == "" {
		resp.Diagnostics.AddError("Invalid import ID", "expected source,target, e.g. busybox:1.37,app:dev; got "+req.ID)
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("source"), source)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("target"), target)...)
}

// read stores the tag in st, or calls missing if it does not exist or no
// longer points at its source. prior is the plan or state; source and
// target are kept as written.
func (r *imageTagResource) read(ctx context.Context, prior imageTagModel, st *tfsdk.State, diags *diag.Diagnostics, missing func()) {
	tag, err := r.client.Image(ctx, prior.Target.ValueString())
	if err != nil {
		diags.AddError("Reading image failed", err.Error())
		return
	}
	src, err := r.client.Image(ctx, prior.Source.ValueString())
	if err != nil {
		diags.AddError("Reading image failed", err.Error())
		return
	}
	// A source removed since is no reason to tag again: the tag still
	// names what it was created from.
	if tag == nil || src != nil && src.Digest != tag.Digest {
		missing()
		return
	}
	prior.ID = types.StringValue(tag.Name)
	prior.Digest = types.StringValue(tag.Digest)
	diags.Append(st.Set(ctx, &prior)...)
}

func (r *imageTagResource) sameName(ctx context.Context, a, b string) (bool, error) {
	na, err := r.client.ImageName(ctx, a)
	if err != nil {
		return false, err
	}
	nb, err := r.client.ImageName(ctx, b)
	return na == nb, err
}
