package main

import (
	"context"
	"flag"
	"log"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"

	"github.com/thorstenpfister/terraform-provider-espalier/internal/provider"
)

// Generates docs/ from the schema, examples/ and templates/.
//go:generate go tool tfplugindocs generate --provider-name espalier

// Set by goreleaser.
var version = "dev"

func main() {
	var debug bool
	flag.BoolVar(&debug, "debug", false, "run with support for debuggers like delve")
	flag.Parse()

	err := providerserver.Serve(context.Background(), provider.New(version), providerserver.ServeOpts{
		Address: "registry.terraform.io/thorstenpfister/espalier",
		Debug:   debug,
	})
	if err != nil {
		log.Fatal(err)
	}
}
