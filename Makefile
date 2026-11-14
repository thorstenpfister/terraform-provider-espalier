.DEFAULT_GOAL := help
.PHONY: help build install test testacc testacc-tofu lint docs release-check tested-container-version

help: ## Show targets
	@awk 'BEGIN {FS = ":.*## "} /^[a-z-]+:.*## / {printf "  %-14s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

build: ## Compile the provider
	go build ./...

install: ## Install the provider binary into $GOBIN (for dev_overrides)
	go install .

test: ## Unit tests (no runtime needed)
	go test ./...

testacc: ## Acceptance tests against the local runtime, with Terraform
	TF_ACC=1 go test ./... -v -count=1 -timeout 30m

testacc-tofu: ## Acceptance tests against the local runtime, with OpenTofu
	TF_ACC=1 TF_ACC_TERRAFORM_PATH="$$(command -v tofu)" TF_ACC_PROVIDER_HOST=registry.opentofu.org \
		go test ./... -v -count=1 -timeout 30m

lint: ## golangci-lint
	golangci-lint run

docs: ## Regenerate docs/ (tfplugindocs)
	go generate ./...

release-check: ## Validate the release config and build an unsigned snapshot into dist/
	goreleaser check
	TESTED_CONTAINER_VERSION=$$($(MAKE) -s tested-container-version) goreleaser release --snapshot --clean --skip=sign,publish

tested-container-version: ## Print the container version this release is verified against
	@sed -n 's/^const testedCLIVersion = "\(.*\)"$$/\1/p' internal/provider/provider.go
