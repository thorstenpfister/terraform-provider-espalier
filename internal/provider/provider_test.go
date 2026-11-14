package provider

import (
	"context"
	"errors"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/thorstenpfister/terraform-provider-espalier/internal/client"
)

// testAccProviders serves the provider in-process to acceptance tests, which
// run against the local runtime (TF_ACC=1, see the Makefile).
var testAccProviders = map[string]func() (tfprotov6.ProviderServer, error){
	"espalier": providerserver.NewProtocol6WithError(New("test")()),
}

type versionClient struct {
	client.Client
	version string
	err     error
}

func (c versionClient) Version(context.Context) (string, error) { return c.version, c.err }

// Another container version than the tested one warns; a CLI that cannot be
// run does not, as the first call that needs it fails with its own error.
func TestConfigureWarnsOnUntestedVersion(t *testing.T) {
	for name, tc := range map[string]struct {
		client   versionClient
		warnings int
	}{
		"tested":   {client: versionClient{version: testedCLIVersion}},
		"untested": {client: versionClient{version: "0.0.1"}, warnings: 1},
		"no CLI":   {client: versionClient{err: errors.New("executable file not found")}},
	} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			p := &espalierProvider{client: tc.client}
			var sr provider.SchemaResponse
			p.Schema(ctx, provider.SchemaRequest{}, &sr)
			typ := sr.Schema.Type().TerraformType(ctx).(tftypes.Object)
			attrs := map[string]tftypes.Value{}
			for name, at := range typ.AttributeTypes {
				attrs[name] = tftypes.NewValue(at, nil)
			}
			req := provider.ConfigureRequest{Config: tfsdk.Config{Schema: sr.Schema, Raw: tftypes.NewValue(typ, attrs)}}
			var resp provider.ConfigureResponse
			p.Configure(ctx, req, &resp)
			if resp.Diagnostics.HasError() {
				t.Fatal(resp.Diagnostics)
			}
			if got := resp.Diagnostics.WarningsCount(); got != tc.warnings {
				t.Errorf("%d warnings, want %d: %v", got, tc.warnings, resp.Diagnostics)
			}
		})
	}
}
