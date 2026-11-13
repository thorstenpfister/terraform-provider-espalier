package provider

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"gopkg.in/yaml.v3"

	"github.com/thorstenpfister/terraform-provider-espalier/internal/client"
)

// defaultClusterCreateTimeout bounds `cluster create`, which waits for the
// nodes to become ready and never gives up on its own.
const defaultClusterCreateTimeout = 15 * time.Minute

// clusterRunning and clusterDegraded are the values of state.
const (
	clusterRunning  = "running"
	clusterDegraded = "degraded"
)

type clusterResource struct {
	client client.Client
}

type clusterModel struct {
	ID                   types.String   `tfsdk:"id"`
	Name                 types.String   `tfsdk:"name"`
	NodeImage            types.String   `tfsdk:"node_image"`
	CPUs                 types.Int64    `tfsdk:"cpus"`
	Memory               types.String   `tfsdk:"memory"`
	Workers              types.List     `tfsdk:"workers"`
	CNI                  types.String   `tfsdk:"cni"`
	SharedImageStore     types.Bool     `tfsdk:"shared_image_store"`
	Scheme               types.String   `tfsdk:"scheme"`
	KubernetesVersion    types.String   `tfsdk:"kubernetes_version"`
	State                types.String   `tfsdk:"state"`
	Nodes                types.List     `tfsdk:"nodes"`
	APIServer            types.String   `tfsdk:"api_server"`
	Kubeconfig           types.String   `tfsdk:"kubeconfig"`
	ClientCertificate    types.String   `tfsdk:"client_certificate"`
	ClientKey            types.String   `tfsdk:"client_key"`
	ClusterCACertificate types.String   `tfsdk:"cluster_ca_certificate"`
	Timeouts             timeouts.Value `tfsdk:"timeouts"`
}

type workerModel struct {
	Count  types.Int64  `tfsdk:"count"`
	CPUs   types.Int64  `tfsdk:"cpus"`
	Memory types.String `tfsdk:"memory"`
}

type clusterNodeModel struct {
	Name    types.String `tfsdk:"name"`
	Role    types.String `tfsdk:"role"`
	Address types.String `tfsdk:"address"`
	State   types.String `tfsdk:"state"`
}

var (
	workerType = types.ObjectType{AttrTypes: map[string]attr.Type{
		"count": types.Int64Type, "cpus": types.Int64Type, "memory": types.StringType,
	}}
	clusterNodeType = types.ObjectType{AttrTypes: map[string]attr.Type{
		"name": types.StringType, "role": types.StringType, "address": types.StringType, "state": types.StringType,
	}}
)

var (
	_ resource.ResourceWithConfigure   = &clusterResource{}
	_ resource.ResourceWithImportState = &clusterResource{}
	_ resource.ResourceWithModifyPlan  = &clusterResource{}
)

// NewClusterResource returns the espalier_cluster resource.
func NewClusterResource() resource.Resource { return &clusterResource{} }

func (r *clusterResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_cluster"
}

func (r *clusterResource) Schema(ctx context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	known := []planmodifier.String{stringplanmodifier.UseStateForUnknown()}
	sensitive := func(desc string) schema.StringAttribute {
		return schema.StringAttribute{MarkdownDescription: desc, Computed: true, Sensitive: true, PlanModifiers: known}
	}
	size := []validator.String{
		stringvalidator.RegexMatches(sizePattern, "must be a number of bytes with an optional K, M, G, T or P suffix"),
	}
	resp.Schema = schema.Schema{
		MarkdownDescription: "A local Kubernetes cluster (`container cluster`, experimental in the runtime): a " +
			"control-plane node and optional workers, each a container. The runtime cannot resize or restart a " +
			"cluster, so every change of its shape replaces it. A cluster whose nodes stopped or are missing, e.g. " +
			"after the runtime restarted, is `degraded` and replaced on the next apply.\n\n" +
			"The provider never touches `~/.kube/config`; write `kubeconfig` to a file yourself, or configure the " +
			"`kubernetes` and `helm` providers from `api_server` and the credentials. The API server is published " +
			"on all host addresses.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Name of the cluster.", Computed: true, PlanModifiers: known,
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Name of the cluster, also the control-plane node's; workers are `<name>-worker-<n>`.",
				Required:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"node_image": schema.StringAttribute{
				MarkdownDescription: "Node image. Defaults to the `kindest/node` image the runtime pins; the runtime " +
					"reports it by digest.",
				Optional: true,
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplaceIfConfigured(), stringplanmodifier.UseStateForUnknown(),
				},
			},
			"cpus": schema.Int64Attribute{
				MarkdownDescription: "CPUs of the control-plane node. Defaults to a quarter of the host's, at least 2.",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.RequiresReplaceIfConfigured(), int64planmodifier.UseStateForUnknown(),
				},
				Validators: []validator.Int64{int64validator.AtLeast(1)},
			},
			"memory": schema.StringAttribute{
				MarkdownDescription: "Memory of the control-plane node, with an optional `K`, `M`, `G`, `T` or `P` " +
					"suffix. Defaults to a quarter of the host's, at least `2G`.",
				Optional: true,
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplaceIf(func(_ context.Context, req planmodifier.StringRequest, resp *stringplanmodifier.RequiresReplaceIfFuncResponse) {
						resp.RequiresReplace = !req.ConfigValue.IsNull() && !sameSize(req.PlanValue.ValueString(), req.StateValue.ValueString())
					}, "Replaces the cluster unless the size is the same.", "Replaces the cluster unless the size is the same."),
					stringplanmodifier.UseStateForUnknown(),
				},
				Validators: size,
			},
			"workers": schema.ListNestedAttribute{
				MarkdownDescription: "Groups of equally sized worker nodes. Without workers, the control-plane node " +
					"runs workloads too.",
				Optional:      true,
				PlanModifiers: replaceList,
				NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
					"count": schema.Int64Attribute{
						MarkdownDescription: "Number of workers. Defaults to 1.",
						Optional:            true,
						Computed:            true,
						Default:             int64default.StaticInt64(1),
						Validators:          []validator.Int64{int64validator.AtLeast(1)},
					},
					"cpus": schema.Int64Attribute{
						MarkdownDescription: "CPUs of each worker. Defaults to a quarter of the host's, at least 2.",
						Optional:            true,
						Validators:          []validator.Int64{int64validator.AtLeast(1)},
					},
					"memory": schema.StringAttribute{
						MarkdownDescription: "Memory of each worker. Defaults to a quarter of the host's, at least `2G`.",
						Optional:            true,
						Validators:          size,
					},
				}},
			},
			"cni": schema.StringAttribute{
				MarkdownDescription: "Path of a CNI manifest to apply, or `NONE` to install none, e.g. to install " +
					"Cilium yourself. Defaults to kindnet. Creating waits until the nodes are ready, so a manifest that " +
					"never gets them there runs into the create timeout.",
				Optional:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"shared_image_store": schema.BoolAttribute{
				MarkdownDescription: "Let nodes pull images from the runtime's local image store. Needs the runtime's " +
					"`cluster-shim`, which Homebrew builds of `container` lack. Defaults to `false`.",
				Optional:      true,
				Computed:      true,
				Default:       booldefault.StaticBool(false),
				PlanModifiers: []planmodifier.Bool{boolplanmodifier.RequiresReplace()},
			},
			"scheme": schema.StringAttribute{
				MarkdownDescription: "Registry scheme, `http` or `https`, for pulling the node image. Defaults to " +
					"`https`. Not recorded by the runtime; changing it does not replace the cluster.",
				Optional:   true,
				Validators: []validator.String{stringvalidator.OneOf("http", "https")},
			},
			"kubernetes_version": schema.StringAttribute{
				MarkdownDescription: "Kubernetes version of the nodes, e.g. `v1.35.5`.", Computed: true, PlanModifiers: known,
			},
			"state": schema.StringAttribute{
				MarkdownDescription: "`running`, or `degraded` if a node is not running or missing; a degraded cluster " +
					"is replaced.",
				Computed: true,
			},
			"nodes": schema.ListNestedAttribute{
				MarkdownDescription: "The node containers, control plane first.",
				Computed:            true,
				PlanModifiers:       []planmodifier.List{listplanmodifier.UseStateForUnknown()},
				NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
					"name":    schema.StringAttribute{MarkdownDescription: "Name of the node container.", Computed: true},
					"role":    schema.StringAttribute{MarkdownDescription: "`control-plane` or `worker`.", Computed: true},
					"address": schema.StringAttribute{MarkdownDescription: "IPv4 address on the default network.", Computed: true},
					"state":   schema.StringAttribute{MarkdownDescription: "State of the container, e.g. `running`.", Computed: true},
				}},
			},
			"api_server":             schema.StringAttribute{MarkdownDescription: "URL of the API server on the host, e.g. `https://127.0.0.1:6445`.", Computed: true, PlanModifiers: known},
			"kubeconfig":             sensitive("Kubeconfig with admin credentials, its context named after the cluster."),
			"client_certificate":     sensitive("PEM client certificate of the admin user."),
			"client_key":             sensitive("PEM private key of the admin user."),
			"cluster_ca_certificate": sensitive("PEM certificate of the cluster's CA."),
		},
		Blocks: map[string]schema.Block{
			"timeouts": timeouts.Block(ctx, timeouts.Opts{Create: true}),
		},
	}
	resp.Schema.Attributes = withSchemaRules(resp.Schema.Attributes)
}

func (r *clusterResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFrom(req.ProviderData, &resp.Diagnostics)
}

// ModifyPlan replaces a degraded cluster: it plans state back to running
// and asks for a replacement on that change.
func (r *clusterResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.Plan.Raw.IsNull() || req.State.Raw.IsNull() {
		return
	}
	var state types.String
	resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("state"), &state)...)
	resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("state"), clusterRunning)...)
	if state.ValueString() != clusterRunning {
		resp.RequiresReplace = append(resp.RequiresReplace, path.Root("state"))
	}
}

func (r *clusterResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan clusterModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	timeout, diags := plan.Timeouts.Create(ctx, defaultClusterCreateTimeout)
	resp.Diagnostics.Append(diags...)
	spec := client.ClusterSpec{
		Name:             plan.Name.ValueString(),
		NodeImage:        plan.NodeImage.ValueString(),
		CPUs:             plan.CPUs.ValueInt64(),
		Memory:           plan.Memory.ValueString(),
		CNI:              plan.CNI.ValueString(),
		SharedImageStore: plan.SharedImageStore.ValueBool(),
		Scheme:           plan.Scheme.ValueString(),
	}
	var workers []workerModel
	resp.Diagnostics.Append(plan.Workers.ElementsAs(ctx, &workers, false)...)
	for _, w := range workers {
		spec.Workers = append(spec.Workers, client.WorkerGroup{Count: w.Count.ValueInt64(), CPUs: w.CPUs.ValueInt64(), Memory: w.Memory.ValueString()})
	}
	if resp.Diagnostics.HasError() {
		return
	}
	createCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if err := r.client.CreateCluster(createCtx, spec); err != nil {
		if errors.Is(createCtx.Err(), context.DeadlineExceeded) {
			err = fmt.Errorf("not ready after %s (see timeouts): %w", timeout, err)
		}
		// Leave nothing half-made behind; it would block the next attempt.
		if delErr := r.deleteIfExists(context.WithoutCancel(ctx), spec.Name); delErr != nil {
			err = errors.Join(err, delErr)
		}
		resp.Diagnostics.AddError("Creating cluster failed", err.Error())
		return
	}
	r.read(ctx, plan, &resp.State, &resp.Diagnostics, func() {
		resp.Diagnostics.AddError("Creating cluster failed", "cluster "+spec.Name+" not found after creating it")
	})
}

func (r *clusterResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state clusterModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	r.read(ctx, state, &resp.State, &resp.Diagnostics, func() { resp.State.RemoveResource(ctx) })
}

// Update only changes scheme and timeouts; everything else replaces the
// cluster.
func (r *clusterResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan clusterModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *clusterResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state clusterModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.deleteIfExists(ctx, state.Name.ValueString()); err != nil {
		resp.Diagnostics.AddError("Deleting cluster failed", err.Error())
	}
}

func (r *clusterResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("name"), req.ID)...)
}

func (r *clusterResource) deleteIfExists(ctx context.Context, name string) error {
	cl, err := r.client.Cluster(ctx, name)
	if err != nil || cl == nil {
		return err
	}
	return r.client.DeleteCluster(ctx, name)
}

// read stores the cluster in st, or calls missing if it does not exist.
// prior is the plan or state: what was written is kept where the runtime
// reports it differently or not at all. A prior without workers or cni is
// an import, or a cluster created without them, which then has no workers
// and reports no CNI either.
func (r *clusterResource) read(ctx context.Context, prior clusterModel, st *tfsdk.State, diags *diag.Diagnostics, missing func()) {
	name := prior.Name.ValueString()
	cl, err := r.client.Cluster(ctx, name)
	if err != nil {
		diags.AddError("Reading cluster failed", err.Error())
		return
	}
	if cl == nil {
		missing()
		return
	}
	m := prior
	m.ID = types.StringValue(cl.Name)
	m.KubernetesVersion = types.StringValue(cl.KubernetesVersion)
	m.SharedImageStore = types.BoolValue(cl.SharedImageStore)
	if m.CNI.IsNull() {
		m.CNI = nullIfEmpty(cl.CNI)
	}

	controlPlane, workers := splitNodes(cl.Nodes)
	nodes := make([]clusterNodeModel, 0, len(cl.Nodes))
	for _, n := range cl.Nodes {
		nodes = append(nodes, clusterNodeModel{
			Name: types.StringValue(n.Name), Role: types.StringValue(n.Role),
			Address: types.StringValue(n.Address), State: types.StringValue(n.State),
		})
	}
	var d diag.Diagnostics
	m.Nodes, d = types.ListValueFrom(ctx, clusterNodeType, nodes)
	diags.Append(d...)

	if controlPlane != nil {
		if m.NodeImage.IsNull() || m.NodeImage.IsUnknown() {
			m.NodeImage = types.StringValue(controlPlane.Image)
		}
		m.CPUs = types.Int64Value(controlPlane.CPUs)
		if b, ok := parseSize(m.Memory.ValueString()); !ok || b != controlPlane.MemoryBytes {
			m.Memory = types.StringValue(formatSize(controlPlane.MemoryBytes))
		}
	}

	if m.Workers.IsNull() {
		m.Workers, d = workerGroups(ctx, workers)
		diags.Append(d...)
	}
	var groups []workerModel
	diags.Append(m.Workers.ElementsAs(ctx, &groups, false)...)
	want := int64(0)
	for _, g := range groups {
		want += g.Count.ValueInt64()
	}

	m.State = types.StringValue(clusterDegraded)
	if clusterHealthy(cl, want) {
		m.State = types.StringValue(clusterRunning)
		// A degraded cluster's API server may be gone: keep the last
		// credentials until it is replaced.
		r.readKubeconfig(ctx, name, &m, diags)
	}
	for _, v := range []*types.String{&m.APIServer, &m.Kubeconfig, &m.ClientCertificate, &m.ClientKey, &m.ClusterCACertificate} {
		if v.IsUnknown() { // created degraded
			*v = types.StringNull()
		}
	}
	diags.Append(st.Set(ctx, &m)...)
	keepEmptyCollections(ctx, st, &prior, diags)
}

func splitNodes(nodes []client.ClusterNode) (controlPlane *client.ClusterNode, workers []client.ClusterNode) {
	for _, n := range nodes {
		if n.Role == "control-plane" {
			controlPlane = &n
		} else {
			workers = append(workers, n)
		}
	}
	return controlPlane, workers
}

// clusterHealthy reports whether the cluster and all its nodes run, with a
// control plane and the expected number of workers. After the runtime
// restarted, nodes are stopped and the cluster does not recover.
func clusterHealthy(cl *client.Cluster, workers int64) bool {
	controlPlane, ws := splitNodes(cl.Nodes)
	if cl.State != clusterRunning || controlPlane == nil || int64(len(ws)) != workers {
		return false
	}
	for _, n := range cl.Nodes {
		if n.State != clusterRunning {
			return false
		}
	}
	return true
}

// workerGroups groups worker nodes of the same size, in order: what the
// cluster was created with, with the sizes made explicit.
func workerGroups(ctx context.Context, workers []client.ClusterNode) (types.List, diag.Diagnostics) {
	if len(workers) == 0 {
		return types.ListNull(workerType), nil
	}
	var groups []workerModel
	for _, w := range workers {
		memory := formatSize(w.MemoryBytes)
		if n := len(groups); n > 0 && groups[n-1].CPUs.ValueInt64() == w.CPUs && groups[n-1].Memory.ValueString() == memory {
			groups[n-1].Count = types.Int64Value(groups[n-1].Count.ValueInt64() + 1)
			continue
		}
		groups = append(groups, workerModel{Count: types.Int64Value(1), CPUs: types.Int64Value(w.CPUs), Memory: types.StringValue(memory)})
	}
	return types.ListValueFrom(ctx, workerType, groups)
}

// kubeconfig is the part of a kubeconfig the credentials come from.
type kubeconfig struct {
	Clusters []struct {
		Cluster struct {
			Server                   string `yaml:"server"`
			CertificateAuthorityData string `yaml:"certificate-authority-data"`
		} `yaml:"cluster"`
	} `yaml:"clusters"`
	Users []struct {
		User struct {
			ClientCertificateData string `yaml:"client-certificate-data"`
			ClientKeyData         string `yaml:"client-key-data"`
		} `yaml:"user"`
	} `yaml:"users"`
}

func (r *clusterResource) readKubeconfig(ctx context.Context, name string, m *clusterModel, diags *diag.Diagnostics) {
	raw, err := r.client.ClusterKubeconfig(ctx, name)
	if err != nil {
		diags.AddError("Reading kubeconfig failed", err.Error())
		return
	}
	var kc kubeconfig
	if err := yaml.Unmarshal(raw, &kc); err != nil || len(kc.Clusters) != 1 || len(kc.Users) != 1 {
		diags.AddError("Reading kubeconfig failed", fmt.Sprintf("unexpected kubeconfig for cluster %s (%v)", name, err))
		return
	}
	pem := func(data string) types.String {
		b, err := base64.StdEncoding.DecodeString(data)
		if err != nil {
			diags.AddError("Reading kubeconfig failed", err.Error())
		}
		return types.StringValue(string(b))
	}
	m.Kubeconfig = types.StringValue(string(raw))
	m.APIServer = types.StringValue(kc.Clusters[0].Cluster.Server)
	m.ClusterCACertificate = pem(kc.Clusters[0].Cluster.CertificateAuthorityData)
	m.ClientCertificate = pem(kc.Users[0].User.ClientCertificateData)
	m.ClientKey = pem(kc.Users[0].User.ClientKeyData)
}
