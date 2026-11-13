package provider

import (
	"context"
	"maps"
	"path/filepath"
	"slices"

	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/thorstenpfister/terraform-provider-espalier/internal/client"
)

type imageBuildResource struct {
	client  client.Client
	builder *builder
}

type imageBuildModel struct {
	ID          types.String `tfsdk:"id"`
	Context     types.String `tfsdk:"context"`
	Dockerfile  types.String `tfsdk:"dockerfile"`
	Tags        types.List   `tfsdk:"tags"`
	BuildArgs   types.Map    `tfsdk:"build_args"`
	Target      types.String `tfsdk:"target"`
	Platforms   types.List   `tfsdk:"platforms"`
	Labels      types.Map    `tfsdk:"labels"`
	Secrets     types.Map    `tfsdk:"secrets"`
	SSH         types.Bool   `tfsdk:"ssh"`
	NoCache     types.Bool   `tfsdk:"no_cache"`
	Pull        types.Bool   `tfsdk:"pull"`
	Triggers    types.Map    `tfsdk:"triggers"`
	HashContext types.Bool   `tfsdk:"hash_context"`
	KeepLocally types.Bool   `tfsdk:"keep_locally"`
	ContextHash types.String `tfsdk:"context_hash"`
	Digest      types.String `tfsdk:"digest"`
}

type buildSecretModel struct {
	Env  types.String `tfsdk:"env"`
	File types.String `tfsdk:"file"`
}

// rebuildOn are the arguments whose change rebuilds the image; the others
// only change how it is built or kept.
var rebuildOn = []string{"context", "dockerfile", "tags", "build_args", "target", "platforms", "labels", "secrets", "ssh", "triggers"}

var (
	_ resource.ResourceWithConfigure  = &imageBuildResource{}
	_ resource.ResourceWithModifyPlan = &imageBuildResource{}
)

// NewImageBuildResource returns the espalier_image_build resource.
func NewImageBuildResource() resource.Resource { return &imageBuildResource{} }

func (r *imageBuildResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_image_build"
}

func (r *imageBuildResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "An image built from a Dockerfile with the runtime's builder (`container build`). " +
			"It is rebuilt in place, under the same tags, when the build context, the Dockerfile, the arguments " +
			"that shape the image or `triggers` change, and when a tag is removed or moved outside Terraform. " +
			"See the provider's `builder` settings for the builder container.\n\n" +
			"Containers that use the image by tag are not recreated by a rebuild; add " +
			"`replace_triggered_by` on its `digest` to them.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Fully qualified reference of the first tag, e.g. `docker.io/library/app:dev` for `app:dev`.",
				Computed:            true,
			},
			"context": schema.StringAttribute{
				MarkdownDescription: "Build context directory, e.g. `\"${path.module}/app\"`.",
				Required:            true,
			},
			"dockerfile": schema.StringAttribute{
				MarkdownDescription: "Path of the Dockerfile; a relative path is relative to `context`. Defaults to " +
					"`Dockerfile` in `context`. It may be outside `context`.",
				Optional: true,
			},
			"tags": schema.ListAttribute{
				MarkdownDescription: "References to build the image as, resolved like `espalier_image`'s `name`.",
				ElementType:         types.StringType,
				Required:            true,
				Validators:          []validator.List{listvalidator.SizeAtLeast(1), listvalidator.UniqueValues()},
			},
			"build_args": schema.MapAttribute{
				MarkdownDescription: "Build-time variables (`ARG`).",
				ElementType:         types.StringType,
				Optional:            true,
			},
			"target": schema.StringAttribute{
				MarkdownDescription: "Stage to build. Defaults to the last.",
				Optional:            true,
			},
			"platforms": schema.ListAttribute{
				MarkdownDescription: "Platforms to build for (`os/arch[/variant]`), e.g. `[\"linux/arm64\", \"linux/amd64\"]` " +
					"for a multi-platform image. Defaults to the host's.",
				ElementType: types.StringType,
				Optional:    true,
				Validators:  []validator.List{listvalidator.UniqueValues()},
			},
			"labels": schema.MapAttribute{
				MarkdownDescription: "Labels added to the image.",
				ElementType:         types.StringType,
				Optional:            true,
			},
			"secrets": schema.MapNestedAttribute{
				MarkdownDescription: "Secrets the build can mount (`RUN --mount=type=secret,id=…`), by ID, read from a host " +
					"environment variable or file when building. Their values never pass through Terraform, so " +
					"changing one does not rebuild: change `triggers` to rebuild.",
				Optional: true,
				NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
					"env": schema.StringAttribute{
						MarkdownDescription: "Name of an environment variable of the Terraform process.",
						Optional:            true,
						Validators: []validator.String{
							stringvalidator.ExactlyOneOf(path.MatchRelative().AtParent().AtName("file")),
						},
					},
					"file": schema.StringAttribute{
						MarkdownDescription: "Path of a file.",
						Optional:            true,
					},
				}},
			},
			"ssh": schema.BoolAttribute{
				MarkdownDescription: "Forward the host's SSH agent to the build (`RUN --mount=type=ssh`).",
				Optional:            true,
			},
			"no_cache": schema.BoolAttribute{
				MarkdownDescription: "Build without the builder's cache. Changing it does not rebuild.",
				Optional:            true,
			},
			"pull": schema.BoolAttribute{
				MarkdownDescription: "Pull base images even if they are present. Changing it does not rebuild.",
				Optional:            true,
			},
			"triggers": schema.MapAttribute{
				MarkdownDescription: "Arbitrary values that rebuild the image when they change, e.g. " +
					"`{ rebuild = \"2\" }` to force a rebuild, or a hash of something the build fetches.",
				ElementType: types.StringType,
				Optional:    true,
			},
			"hash_context": schema.BoolAttribute{
				MarkdownDescription: "Rebuild when the content of `context` changes, by hashing it on every plan. " +
					"Exclude large directories it need not read in a `.dockerignore`, or set `false` and rebuild with " +
					"`triggers`. Defaults to `true`.",
				Optional: true,
				Computed: true,
				Default:  booldefault.StaticBool(true),
			},
			"keep_locally": schema.BoolAttribute{
				MarkdownDescription: "Keep the image's tags in the runtime when the resource is destroyed or a tag is " +
					"removed from `tags`. Defaults to `false`.",
				Optional: true,
				Computed: true,
				Default:  booldefault.StaticBool(false),
			},
			"context_hash": schema.StringAttribute{
				MarkdownDescription: "Hash of the Dockerfile and the context, if `hash_context` is set.",
				Computed:            true,
			},
			"digest": schema.StringAttribute{
				MarkdownDescription: "Digest of the built image index.",
				Computed:            true,
			},
		},
	}
	resp.Schema.Attributes = withSchemaRules(resp.Schema.Attributes)
}

func (r *imageBuildResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if d := dataFrom(req.ProviderData, &resp.Diagnostics); d != nil {
		r.client, r.builder = d.client, d.builder
	}
}

// ModifyPlan hashes the context and plans a rebuild, an unknown digest, when
// the hash, an argument in rebuildOn or a tag (see Read) changed.
func (r *imageBuildResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.Plan.Raw.IsNull() {
		return // destroy
	}
	var plan imageBuildModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	plan.ContextHash = planContextHash(plan, &resp.Diagnostics)
	if req.State.Raw.IsNull() {
		resp.Diagnostics.Append(resp.Plan.Set(ctx, &plan)...)
		return
	}
	var state imageBuildModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	rebuild := state.Digest.ValueString() == "" ||
		plan.ContextHash.IsUnknown() ||
		!plan.ContextHash.IsNull() && !state.ContextHash.IsNull() && !plan.ContextHash.Equal(state.ContextHash)
	for _, name := range rebuildOn {
		var planned, prior attr.Value
		resp.Diagnostics.Append(req.Plan.GetAttribute(ctx, path.Root(name), &planned)...)
		resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root(name), &prior)...)
		rebuild = rebuild || !sameOrEmpty(ctx, planned, prior)
	}
	plan.ID, plan.Digest = state.ID, state.Digest
	if rebuild {
		plan.Digest = types.StringUnknown()
		if !firstTag(plan).Equal(firstTag(state)) {
			plan.ID = types.StringUnknown()
		}
	}
	resp.Diagnostics.Append(resp.Plan.Set(ctx, &plan)...)
}

// planContextHash is the context hash, unknown while the context or the
// Dockerfile are, and null without hash_context.
func planContextHash(m imageBuildModel, diags *diag.Diagnostics) types.String {
	switch {
	case m.HashContext.IsUnknown() || m.Context.IsUnknown() || m.Dockerfile.IsUnknown():
		return types.StringUnknown()
	case !m.HashContext.ValueBool():
		return types.StringNull()
	}
	h, err := contextHash(m.Context.ValueString(), dockerfilePath(m))
	if err != nil {
		diags.AddAttributeError(path.Root("context"), "Hashing the build context failed", err.Error())
		return types.StringUnknown()
	}
	return types.StringValue(h)
}

func dockerfilePath(m imageBuildModel) string {
	df := m.Dockerfile.ValueString()
	if df == "" {
		df = "Dockerfile"
	}
	if filepath.IsAbs(df) {
		return df
	}
	return filepath.Join(m.Context.ValueString(), df)
}

func firstTag(m imageBuildModel) attr.Value {
	if m.Tags.IsUnknown() || len(m.Tags.Elements()) == 0 {
		return types.StringUnknown()
	}
	return m.Tags.Elements()[0]
}

func (r *imageBuildResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan imageBuildModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	r.build(ctx, &plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *imageBuildResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state imageBuildModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var tags []string
	resp.Diagnostics.Append(state.Tags.ElementsAs(ctx, &tags, false)...)
	for _, tag := range tags {
		img, err := r.client.Image(ctx, tag)
		if err != nil {
			resp.Diagnostics.AddError("Reading image failed", err.Error())
			return
		}
		// Removed or moved outside Terraform: an empty digest plans a rebuild.
		if img == nil || img.Digest != state.Digest.ValueString() {
			state.Digest = types.StringValue("")
			break
		}
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update rebuilds if ModifyPlan planned it, and removes tags dropped from
// tags.
func (r *imageBuildResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state imageBuildModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if plan.Digest.IsUnknown() {
		r.build(ctx, &plan, &resp.Diagnostics)
		if resp.Diagnostics.HasError() {
			return
		}
		if !plan.KeepLocally.ValueBool() {
			r.deleteTags(ctx, state, plan.Tags, &resp.Diagnostics)
		}
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *imageBuildResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state imageBuildModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() || state.KeepLocally.ValueBool() {
		return
	}
	r.deleteTags(ctx, state, types.ListNull(types.StringType), &resp.Diagnostics)
}

// build builds the image as planned in m and sets its id and digest.
func (r *imageBuildResource) build(ctx context.Context, m *imageBuildModel, diags *diag.Diagnostics) {
	if m.ContextHash.IsUnknown() {
		m.ContextHash = planContextHash(*m, diags)
	}
	spec := client.BuildSpec{
		Context:    m.Context.ValueString(),
		Dockerfile: dockerfilePath(*m),
		Target:     m.Target.ValueString(),
		SSH:        m.SSH.ValueBool(),
		NoCache:    m.NoCache.ValueBool(),
		Pull:       m.Pull.ValueBool(),
	}
	diags.Append(m.Tags.ElementsAs(ctx, &spec.Tags, false)...)
	diags.Append(m.Platforms.ElementsAs(ctx, &spec.Platforms, false)...)
	diags.Append(m.BuildArgs.ElementsAs(ctx, &spec.BuildArgs, false)...)
	diags.Append(m.Labels.ElementsAs(ctx, &spec.Labels, false)...)
	var secrets map[string]buildSecretModel
	diags.Append(m.Secrets.ElementsAs(ctx, &secrets, false)...)
	for _, id := range slices.Sorted(maps.Keys(secrets)) {
		s := secrets[id]
		spec.Secrets = append(spec.Secrets, client.BuildSecret{ID: id, Env: s.Env.ValueString(), File: s.File.ValueString()})
	}
	if diags.HasError() {
		return
	}
	err := r.builder.build(ctx, r.client, func() error { return r.client.BuildImage(ctx, spec) })
	if err != nil {
		diags.AddError("Building image failed", err.Error())
		return
	}
	img, err := r.client.Image(ctx, spec.Tags[0])
	if err != nil {
		diags.AddError("Reading image failed", err.Error())
		return
	}
	if img == nil {
		diags.AddError("Building image failed", "image "+spec.Tags[0]+" not found after building")
		return
	}
	m.ID = types.StringValue(img.Name)
	m.Digest = types.StringValue(img.Digest)
}

// deleteTags deletes the tags of the image in state that are not in keep.
// Tags that point at another image by now are left alone.
func (r *imageBuildResource) deleteTags(ctx context.Context, state imageBuildModel, keep types.List, diags *diag.Diagnostics) {
	var tags, kept []string
	diags.Append(state.Tags.ElementsAs(ctx, &tags, false)...)
	diags.Append(keep.ElementsAs(ctx, &kept, false)...)
	keepNames := map[string]bool{}
	for _, tag := range kept {
		name, err := r.client.ImageName(ctx, tag)
		if err != nil {
			diags.AddError("Resolving image failed", err.Error())
			return
		}
		keepNames[name] = true
	}
	for _, tag := range tags {
		img, err := r.client.Image(ctx, tag)
		if err != nil {
			diags.AddError("Reading image failed", err.Error())
			return
		}
		if img == nil || keepNames[img.Name] || img.Digest != state.Digest.ValueString() {
			continue
		}
		if err := r.client.DeleteImage(ctx, img.Name); err != nil {
			diags.AddError("Deleting image failed", err.Error())
		}
	}
}
