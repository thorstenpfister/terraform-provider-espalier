package provider

import (
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
)

// testAccProviders serves the provider in-process to acceptance tests, which
// run against the local runtime (TF_ACC=1, see the Makefile).
var testAccProviders = map[string]func() (tfprotov6.ProviderServer, error){
	"espalier": providerserver.NewProtocol6WithError(New("test")()),
}
