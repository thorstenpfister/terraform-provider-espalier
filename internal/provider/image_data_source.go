package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/thorstenpfister/terraform-provider-espalier/internal/client"
)

type imageDataSource struct {
	client client.Client
}

type imageDataModel struct {
	Name      types.String `tfsdk:"name"`
	ID        types.String `tfsdk:"id"`
	Digest    types.String `tfsdk:"digest"`
	Platforms types.List   `tfsdk:"platforms"`
}

var _ datasource.DataSourceWithConfigure = &imageDataSource{}

// NewImageDataSource returns the espalier_image data source.
func NewImageDataSource() datasource.DataSource { return &imageDataSource{} }

func (d *imageDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_image"
}

func (d *imageDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "An image already present in the container runtime, e.g. one built or pulled outside Terraform. " +
			"Fails if the image is not present; use the `espalier_image` resource to pull it.",
		Attributes: map[string]schema.Attribute{
			"name": schema.StringAttribute{
				MarkdownDescription: "Image reference, resolved like the `espalier_image` resource's `name`.",
				Required:            true,
			},
			"id": schema.StringAttribute{
				MarkdownDescription: "Fully qualified reference the runtime stores the image under.",
				Computed:            true,
			},
			"digest": schema.StringAttribute{
				MarkdownDescription: "Digest of the image index or manifest.",
				Computed:            true,
			},
			"platforms": schema.ListAttribute{
				MarkdownDescription: "Platforms of the image (`os/arch[/variant]`).",
				ElementType:         types.StringType,
				Computed:            true,
			},
		},
	}
}

func (d *imageDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = clientFrom(req.ProviderData, &resp.Diagnostics)
}

func (d *imageDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var cfg imageDataModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	img, err := d.client.Image(ctx, cfg.Name.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Reading image failed", err.Error())
		return
	}
	if img == nil {
		resp.Diagnostics.AddError("Image not found", "image "+cfg.Name.ValueString()+" is not present in the container runtime")
		return
	}
	platforms, diags := types.ListValueFrom(ctx, types.StringType, img.Platforms)
	resp.Diagnostics.Append(diags...)
	cfg.ID = types.StringValue(img.Name)
	cfg.Digest = types.StringValue(img.Digest)
	cfg.Platforms = platforms
	resp.Diagnostics.Append(resp.State.Set(ctx, &cfg)...)
}
