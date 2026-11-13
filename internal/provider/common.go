package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/thorstenpfister/terraform-provider-espalier/internal/client"
)

// providerData is what Configure hands to resources and data sources.
type providerData struct {
	client  client.Client
	builder *builder
}

// dataFrom unpacks the provider data. It returns nil before the provider is
// configured.
func dataFrom(data any, diags *diag.Diagnostics) *providerData {
	if data == nil {
		return nil
	}
	d, ok := data.(*providerData)
	if !ok {
		diags.AddError("Unexpected provider data", fmt.Sprintf("got %T, want *providerData", data))
	}
	return d
}

// clientFrom unpacks the client from the provider data, or returns nil
// before the provider is configured.
func clientFrom(data any, diags *diag.Diagnostics) client.Client {
	if d := dataFrom(data, diags); d != nil {
		return d.client
	}
	return nil
}

// labelsValue converts labels read from the runtime. The runtime reports an
// empty set as {}; when none were configured (prior is null) that stays null
// so it does not show as a diff.
func labelsValue(ctx context.Context, labels map[string]string, prior types.Map, diags *diag.Diagnostics) types.Map {
	if len(labels) == 0 && prior.IsNull() {
		return types.MapNull(types.StringType)
	}
	v, d := types.MapValueFrom(ctx, types.StringType, labels)
	diags.Append(d...)
	return v
}
