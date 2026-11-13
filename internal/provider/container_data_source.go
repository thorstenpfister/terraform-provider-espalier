package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	dschema "github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	rschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"

	"github.com/thorstenpfister/terraform-provider-espalier/internal/client"
)

type containerDataSource struct {
	client client.Client
}

var _ datasource.DataSourceWithConfigure = &containerDataSource{}

// NewContainerDataSource returns the espalier_container data source.
func NewContainerDataSource() datasource.DataSource { return &containerDataSource{} }

func (d *containerDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_container"
}

// Schema is the resource's, all computed but the name, so the two always
// describe a container the same way.
func (d *containerDataSource) Schema(ctx context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	var rs resource.SchemaResponse
	(&containerResource{}).Schema(ctx, resource.SchemaRequest{}, &rs)
	attrs := computedAttributes(rs.Schema.Attributes)
	attrs["name"] = dschema.StringAttribute{MarkdownDescription: "Name of the container.", Required: true}
	resp.Schema = dschema.Schema{
		MarkdownDescription: "A container of the container runtime, e.g. one created outside Terraform. Fails if it " +
			"does not exist. `image` is fully qualified and `memory` in the largest whole unit.",
		Attributes: attrs,
	}
}

func (d *containerDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = clientFrom(req.ProviderData, &resp.Diagnostics)
}

func (d *containerDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var cfg containerModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	c, err := d.client.Container(ctx, cfg.Name.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Reading container failed", err.Error())
		return
	}
	if c == nil {
		resp.Diagnostics.AddError("Container not found", "container "+cfg.Name.ValueString()+" does not exist")
		return
	}
	m := containerValue(ctx, c, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

// computedAttributes turns resource attributes into computed data source
// attributes with the same types and descriptions.
func computedAttributes(attrs map[string]rschema.Attribute) map[string]dschema.Attribute {
	out := make(map[string]dschema.Attribute, len(attrs))
	for name, a := range attrs {
		desc := a.GetMarkdownDescription()
		switch a := a.(type) {
		case rschema.StringAttribute:
			out[name] = dschema.StringAttribute{MarkdownDescription: desc, Computed: true}
		case rschema.Int64Attribute:
			out[name] = dschema.Int64Attribute{MarkdownDescription: desc, Computed: true}
		case rschema.BoolAttribute:
			out[name] = dschema.BoolAttribute{MarkdownDescription: desc, Computed: true}
		case rschema.ListAttribute:
			out[name] = dschema.ListAttribute{MarkdownDescription: desc, Computed: true, ElementType: a.ElementType}
		case rschema.SetAttribute:
			out[name] = dschema.SetAttribute{MarkdownDescription: desc, Computed: true, ElementType: a.ElementType}
		case rschema.MapAttribute:
			out[name] = dschema.MapAttribute{MarkdownDescription: desc, Computed: true, ElementType: a.ElementType}
		case rschema.ListNestedAttribute:
			out[name] = dschema.ListNestedAttribute{MarkdownDescription: desc, Computed: true,
				NestedObject: dschema.NestedAttributeObject{Attributes: computedAttributes(a.NestedObject.Attributes)}}
		default:
			panic(fmt.Sprintf("computedAttributes: unsupported attribute %s (%T)", name, a))
		}
	}
	return out
}
