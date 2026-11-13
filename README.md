# terraform-provider-espalier

A Terraform and OpenTofu provider for **Apple's [`container`](https://github.com/apple/container)
runtime** on macOS: manage its networks, volumes, images, containers, machines
and local Kubernetes clusters declaratively.

> **Why "espalier"?** An espalier is a fruit tree trained to grow flat along a
> frame: structure declared for something that grows on its own, which is
> what a configuration does for containers. The name also keeps clear of
> Apple's trademarks.

espalier is not affiliated with or endorsed by Apple Inc.

## Status

Available today:

| Kind | Name | What |
|---|---|---|
| Data source | `espalier_system` | Runtime running?, client and server version, default kernel, `config.toml` defaults (registry domain, container/builder/machine defaults, network subnets, vminit image) |
| Resource | `espalier_network` | NAT or host-only network, optional fixed subnets, labels, plugin and plugin options; import by name |
| Resource | `espalier_volume` | Disk-image volume, optional size, labels, driver options; import by name |
| Resource | `espalier_image` | Pulled image, optional platform, `http` registries, `keep_locally`; import by reference |
| Data source | `espalier_image` | Image already in the runtime: digest, platforms |
| Resource | `espalier_image_build` | Image built from a Dockerfile: tags, build args, target, platforms, labels, secrets from host env/files, SSH agent; rebuilt in place on context changes (hashed, honouring `.dockerignore`), `triggers` or tag drift; the builder is stopped afterwards unless the provider's `builder.keep_running` is set |
| Resource | `espalier_image_tag` | Another name for a local image, re-tagged if it drifts from its source; import by `source,target` |
| Resource | `espalier_container` | Image, entrypoint and command (arguments, as in Docker), env, workdir, CPUs, memory, labels; networks (MAC, MTU, aliases), bind/volume/tmpfs mounts, published ports, capabilities; platform, user, read-only root, DNS, init, terminal (TTY), Rosetta, SSH agent, `/dev/shm`, ulimits, published sockets, `http` registries; VM: custom kernel and arguments, init image, runtime handler, nested virtualization, read-only/masked paths (experimental); running state, addresses; import by name |
| Data source | `espalier_container` | Existing container, as the resource describes it |
| Resource | `espalier_machine` | Container machine (long-lived dev VM): image, platform, account, CPUs, memory, home mount, nested virtualization, kernel, default machine, running state; settings change in place; import by name |
| Resource | `espalier_cluster` | Local Kubernetes cluster (experimental in the runtime): node image, control-plane size, worker groups, CNI or none; kubeconfig and admin credentials for the `kubernetes`/`helm` providers, never touching `~/.kube/config`; create timeout that cleans up; replaced when degraded (e.g. after a runtime restart); import by name |
| Data sources | `espalier_network`, `espalier_volume`, `espalier_machine` | One existing object by name, as its resource describes it |
| Data sources | `espalier_networks`, `espalier_volumes`, `espalier_containers`, `espalier_images`, `espalier_machines` | Everything of one kind, sorted by name; filter by labels (networks, volumes, containers) |

Blocked upstream: registry logins (`container registry login`). The runtime stores the
credentials, but pulls then fail to read them from the Keychain (`status -25308`, see
[apple/container#1733](https://github.com/apple/container/issues/1733) and
[#1310](https://github.com/apple/container/issues/1310)), at least with the Homebrew build.
`espalier_registry_login` follows once that works.

Out of scope: host-wide settings that no single configuration owns, such as the default kernel
(`container system kernel set`), and local DNS domains (`container system dns create`), which
need root. Imperative commands (`exec`, `cp`, `logs`, `prune`, …) have no place in a declarative
provider.

Early development: nothing is released yet.

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

Arguments follow a few rules, so modules can pass variables straight
through:

- An empty list, set or map means the same as leaving it out, and so does a
  block such as `dns` with nothing but empty values. Switching between the
  two never replaces anything.
- Where leaving an argument out means a default (e.g. `command`,
  `entrypoint`, `networks`), an empty list is rejected: use `null`.
- Empty strings, negative numbers and `null` elements in collections are
  rejected when validating.
- Values known only after another resource is applied work everywhere; a
  change of one replaces what it would replace if known.

## Development

```sh
make test          # unit tests: no runtime needed, but Terraform on PATH
make testacc       # acceptance tests against the local runtime (Terraform)
make testacc-tofu  # the same with OpenTofu
make lint
make docs          # regenerate docs/ from schema, examples/ and templates/
```

Acceptance tests run against your local runtime. CI runners usually lack
the nested virtualization the runtime needs, so they run locally. A full run
takes about five minutes per tool, needs network access to pull small images
from Docker Hub (and the runtime's `kindest/node` image once), and room for a
two-node cluster of 2 CPUs and 2 GB each. The tests leave the host as they
found it: they refuse to run rather than delete an image of yours they would
remove, delete only images they pulled, restart a builder they stopped with
its size, and skip what would change your default machine.

### Upgrading the runtime

The provider is verified against **`container` 1.7.1**, and
`TestAccTestedVersion` fails on any other version, so an upgrade gets a
deliberate check:

1. Run `make testacc` and `make testacc-tofu`. Output formats and behaviour
   are checked against the real CLI there; the client's unit-test fixtures are
   snapshots of the tested version.
2. Review what was written for that version: `grep -rn 1.7.1 internal/` lists
   the output formats the client decodes and the workarounds (e.g. `build
   --quiet` hanging without a terminal).
3. Check whether upstream fixed what this provider works around or leaves out:
   registry logins ([#1733](https://github.com/apple/container/issues/1733)),
   `--shared-image-store` in Homebrew builds, `--quiet` builds.
4. Update `testedCLIVersion` in `internal/provider/host_test.go` and the
   version above.

## Releasing

Releases are built by goreleaser (`.goreleaser.yml`) in GitHub Actions
when a `v*` tag is pushed: a darwin/arm64 zip, a `SHA256SUMS` file signed
with the project's GPG key, and the registry manifest, as a draft GitHub
release. `make release-check` validates the configuration and builds an
unsigned snapshot into `dist/`. Every push also runs the unit tests, lint
and a check that `docs/` is up to date (`.github/workflows/test.yml`).

### One-time setup

1. A dedicated RSA signing key (the Terraform Registry accepts only RSA and
   DSA keys), its passphrase kept in the macOS Keychain:

   ```sh
   security add-generic-password -a "$USER" -s espalier-release-gpg -w "$(openssl rand -base64 32)"
   gpg --batch --pinentry-mode loopback --passphrase "$(security find-generic-password -s espalier-release-gpg -w)" \
     --quick-generate-key "espalier releases <you@example.com>" rsa4096 sign 3y
   ```

   Back up the secret key offline: losing it means a new key in both
   registries.
2. Repository secrets for the release workflow:

   ```sh
   FPR=$(gpg --list-secret-keys --with-colons "espalier releases" | awk -F: '/^fpr/ {print $10; exit}')
   gpg --batch --pinentry-mode loopback --passphrase "$(security find-generic-password -s espalier-release-gpg -w)" \
     --armor --export-secret-keys "$FPR" | gh secret set GPG_PRIVATE_KEY
   security find-generic-password -s espalier-release-gpg -w | gh secret set GPG_PASSPHRASE
   ```

3. With the repository public, register the provider and the public key
   (`gpg --armor --export "$FPR"`):
   - Terraform Registry: sign in at registry.terraform.io with GitHub, add
     the key under *User Settings → Signing Keys*, then *Publish → Provider*
     and select this repository.
   - OpenTofu Registry: open the *Submit new Provider* and *Submit new
     Provider Signing Key* issues at
     [opentofu/registry](https://github.com/opentofu/registry/issues/new/choose).

### Each release

1. On `main`, both acceptance suites green (`make testacc`,
   `make testacc-tofu`) on the tested runtime version.
2. `git tag vX.Y.Z && git push origin vX.Y.Z`; the release workflow creates
   a draft.
3. Review the draft (four assets: zip, `SHA256SUMS`, its `.sig`, manifest)
   and publish it. The Terraform Registry picks it up through its webhook,
   the OpenTofu Registry within the hour.

## License

Apache License 2.0, like Apple's `container`. See [LICENSE](LICENSE).
