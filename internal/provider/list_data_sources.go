package provider

import (
	"context"
	"slices"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	dschema "github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/thorstenpfister/terraform-provider-espalier/internal/client"
)

// listDataSource lists all objects of one kind, e.g. espalier_networks.
// Each element has the attributes of the kind's resource or data source.
type listDataSource struct {
	client     client.Client
	kind       string // also the list attribute, e.g. "networks"
	what       string // for descriptions, e.g. "networks"
	filterable bool   // has labels to filter by
	attributes func(ctx context.Context) map[string]dschema.Attribute
	list       func(ctx context.Context, c client.Client, diags *diag.Diagnostics) []listItem
}

type listItem struct {
	labels map[string]string
	value  any // the element's model
}

var _ datasource.DataSourceWithConfigure = &listDataSource{}

// NewNetworksDataSource returns the espalier_networks data source.
func NewNetworksDataSource() datasource.DataSource {
	return &listDataSource{
		kind: "networks", what: "networks", filterable: true,
		attributes: func(ctx context.Context) map[string]dschema.Attribute {
			return resourceAttributes(ctx, &networkResource{})
		},
		list: func(ctx context.Context, c client.Client, diags *diag.Diagnostics) []listItem {
			networks, err := c.Networks(ctx)
			if err != nil {
				diags.AddError("Listing networks failed", err.Error())
			}
			items := make([]listItem, 0, len(networks))
			for _, n := range networks {
				items = append(items, listItem{n.Labels, networkValue(ctx, &n, networkModel{}, diags)})
			}
			return items
		},
	}
}

// NewVolumesDataSource returns the espalier_volumes data source.
func NewVolumesDataSource() datasource.DataSource {
	return &listDataSource{
		kind: "volumes", what: "volumes", filterable: true,
		attributes: func(ctx context.Context) map[string]dschema.Attribute {
			return resourceAttributes(ctx, &volumeResource{})
		},
		list: func(ctx context.Context, c client.Client, diags *diag.Diagnostics) []listItem {
			volumes, err := c.Volumes(ctx)
			if err != nil {
				diags.AddError("Listing volumes failed", err.Error())
			}
			items := make([]listItem, 0, len(volumes))
			for _, v := range volumes {
				items = append(items, listItem{v.Labels, volumeValue(ctx, &v, volumeModel{}, diags)})
			}
			return items
		},
	}
}

// NewContainersDataSource returns the espalier_containers data source.
func NewContainersDataSource() datasource.DataSource {
	return &listDataSource{
		kind: "containers", what: "containers, running or not,", filterable: true,
		attributes: func(ctx context.Context) map[string]dschema.Attribute {
			return dataSourceAttributes(ctx, &containerDataSource{})
		},
		list: func(ctx context.Context, c client.Client, diags *diag.Diagnostics) []listItem {
			containers, err := c.Containers(ctx)
			if err != nil {
				diags.AddError("Listing containers failed", err.Error())
			}
			items := make([]listItem, 0, len(containers))
			for _, ctr := range containers {
				items = append(items, listItem{ctr.Labels, containerValue(ctx, &ctr, diags)})
			}
			return items
		},
	}
}

// NewImagesDataSource returns the espalier_images data source.
func NewImagesDataSource() datasource.DataSource {
	return &listDataSource{
		kind: "images", what: "local images",
		attributes: func(ctx context.Context) map[string]dschema.Attribute {
			attrs := dataSourceAttributes(ctx, &imageDataSource{})
			attrs["name"] = dschema.StringAttribute{MarkdownDescription: "Fully qualified reference, the same as `id`.", Computed: true}
			return attrs
		},
		list: func(ctx context.Context, c client.Client, diags *diag.Diagnostics) []listItem {
			images, err := c.Images(ctx)
			if err != nil {
				diags.AddError("Listing images failed", err.Error())
			}
			items := make([]listItem, 0, len(images))
			for _, img := range images {
				platforms, d := types.ListValueFrom(ctx, types.StringType, img.Platforms)
				diags.Append(d...)
				items = append(items, listItem{nil, imageDataModel{
					Name: types.StringValue(img.Name), ID: types.StringValue(img.Name),
					Digest: types.StringValue(img.Digest), Platforms: platforms,
				}})
			}
			return items
		},
	}
}

func (d *listDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_" + d.kind
}

func (d *listDataSource) Schema(ctx context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	attrs := map[string]dschema.Attribute{
		d.kind: dschema.ListNestedAttribute{
			MarkdownDescription: "The " + d.what + " ordered by name.",
			Computed:            true,
			NestedObject:        dschema.NestedAttributeObject{Attributes: d.attributes(ctx)},
		},
	}
	desc := "All " + d.what + " in the container runtime."
	if d.filterable {
		desc = "All " + d.what + " in the container runtime, optionally only those with certain labels."
		attrs["labels"] = dschema.MapAttribute{
			MarkdownDescription: "Only list " + d.kind + " that have all of these labels, with these values.",
			ElementType:         types.StringType,
			Optional:            true,
		}
	}
	resp.Schema = dschema.Schema{MarkdownDescription: desc, Attributes: attrs}
}

func (d *listDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = clientFrom(req.ProviderData, &resp.Diagnostics)
}

func (d *listDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var want map[string]string
	if d.filterable {
		var labels types.Map
		resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("labels"), &labels)...)
		resp.Diagnostics.Append(labels.ElementsAs(ctx, &want, false)...)
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("labels"), labels)...)
	}
	items := d.list(ctx, d.client, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	attrTypes := map[string]attr.Type{}
	for name, a := range d.attributes(ctx) {
		attrTypes[name] = a.GetType()
	}
	values := []attr.Value{}
	for _, item := range items {
		if !hasLabels(item.labels, want) {
			continue
		}
		v, diags := types.ObjectValueFrom(ctx, attrTypes, item.value)
		resp.Diagnostics.Append(diags...)
		values = append(values, v)
	}
	sortByName(values)
	list, diags := types.ListValue(types.ObjectType{AttrTypes: attrTypes}, values)
	resp.Diagnostics.Append(diags...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root(d.kind), list)...)
}

func hasLabels(labels, want map[string]string) bool {
	for k, v := range want {
		if got, ok := labels[k]; !ok || got != v {
			return false
		}
	}
	return true
}

// sortByName orders objects by their name attribute; the runtime lists in
// no particular order.
func sortByName(values []attr.Value) {
	name := func(v attr.Value) string {
		return v.(types.Object).Attributes()["name"].(types.String).ValueString()
	}
	slices.SortFunc(values, func(a, b attr.Value) int { return strings.Compare(name(a), name(b)) })
}

// resourceAttributes are a resource's attributes, all computed.
func resourceAttributes(ctx context.Context, r resource.Resource) map[string]dschema.Attribute {
	var resp resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &resp)
	return computedAttributes(resp.Schema.Attributes)
}

// dataSourceAttributes are a data source's attributes, all computed: its
// lookup arguments become results.
func dataSourceAttributes(ctx context.Context, d datasource.DataSource) map[string]dschema.Attribute {
	var resp datasource.SchemaResponse
	d.Schema(ctx, datasource.SchemaRequest{}, &resp)
	attrs := resp.Schema.Attributes
	if name, ok := attrs["name"].(dschema.StringAttribute); ok && name.Required {
		attrs["name"] = dschema.StringAttribute{MarkdownDescription: name.MarkdownDescription, Computed: true}
	}
	return attrs
}
