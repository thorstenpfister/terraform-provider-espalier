# terraform-provider-espalier

A Terraform and OpenTofu provider for **Apple's [`container`](https://github.com/apple/container)
runtime** on macOS: manage its networks, volumes, images and containers
declaratively, for any local workload.

> **Why "espalier"?** An espalier is a fruit tree trained to grow flat along a
> frame: structure declared for something that grows on its own, which is
> what a configuration does for containers. The name also keeps clear of
> Apple's trademarks.

espalier is not affiliated with or endorsed by Apple Inc.

## Status

Early development. Available today:

| Kind | Name | What |
|---|---|---|
| Data source | `espalier_system` | Runtime running?, client and server version, default kernel |
| Resource | `espalier_network` | NAT or host-only network, optional fixed subnets, labels; import by name |

Planned for v1: resources `espalier_volume`,
`espalier_image` and `espalier_container` (with import), and data sources
`espalier_image` and `espalier_container`.

## Requirements

- macOS on Apple silicon with the `container` CLI installed and started
  (`container system start`)
- Terraform ≥ 1.5 or OpenTofu ≥ 1.6
- Go (see `go.mod`) to build

## Usage

```hcl
terraform {
  required_providers {
    espalier = {
      source = "thorstenpfister/espalier"
    }
  }
}

data "espalier_system" "this" {}
```

Until the provider is published, build it and point Terraform at the binary
with a [`dev_overrides`](https://developer.hashicorp.com/terraform/cli/config/config-file#development-overrides-for-provider-developers)
block for `registry.terraform.io/thorstenpfister/espalier` (OpenTofu:
`registry.opentofu.org/thorstenpfister/espalier`):

```sh
make install   # installs into $(go env GOPATH)/bin
```

## How it works

The provider talks to the runtime only through a small client interface
(`internal/client`). Today that interface is implemented with the
`container` CLI and its JSON output, so a native API client can replace it
later without changing the provider.

## Development

```sh
make test          # unit tests, no runtime needed
make testacc       # acceptance tests against the local runtime (Terraform)
make testacc-tofu  # the same with OpenTofu
make lint
make docs          # regenerate docs/ from schema, examples/ and templates/
```

Acceptance tests run against your local runtime. CI runners usually lack
the nested virtualization the runtime needs, so they run locally.

## License

Apache License 2.0, like Apple's `container`. See [LICENSE](LICENSE).
