package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/thorstenpfister/terraform-provider-espalier/internal/client"
)

// clientFrom unpacks the client that Configure handed to resources and data
// sources. It returns nil before the provider is configured.
func clientFrom(providerData any, diags *diag.Diagnostics) client.Client {
	if providerData == nil {
		return nil
	}
	c, ok := providerData.(client.Client)
	if !ok {
		diags.AddError("Unexpected provider data", fmt.Sprintf("got %T, want client.Client", providerData))
	}
	return c
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
