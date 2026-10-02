GO ?= go
VERSION ?= 0.1.0
GOLANGCI_LINT ?= golangci-lint
TERRAFORM ?= terraform
DOCKER ?= docker
DEV_COMPOSE := $(DOCKER) compose -f dev/compose.yml
BINARY := terraform-provider-crafty
PLATFORM := $(shell $(GO) env GOOS)_$(shell $(GO) env GOARCH)
PLUGIN_DIR ?= $(HOME)/.terraform.d/plugins/registry.terraform.io/bart-kochanowicz/crafty/$(VERSION)/$(PLATFORM)

.PHONY: build test fmt fmt-check lint check vet install clean
build:
	mkdir -p bin
	$(GO) build -ldflags "-X main.version=$(VERSION)" -o bin/$(BINARY) .
test:
	$(GO) test -race -count=1 ./...
fmt:
	$(GOLANGCI_LINT) fmt
	$(TERRAFORM) fmt -recursive examples
fmt-check:
	@test -z "$$(gofmt -l .)" || { echo "Go files were not formatted. Run make fmt and commit the changes."; exit 1; }
	$(TERRAFORM) fmt -check -recursive examples
lint:
	$(GOLANGCI_LINT) run
	$(GOLANGCI_LINT) fmt --diff --diff-colored=false
check: fmt-check lint test vet build
vet:
	$(GO) vet ./...
install: build
	mkdir -p "$(PLUGIN_DIR)"
	cp bin/$(BINARY) "$(PLUGIN_DIR)/$(BINARY)_v$(VERSION)"
clean:
	rm -rf bin

.PHONY: dev-up dev-down dev-status dev-logs dev-credentials dev-check dev-provider
dev-up:
	$(DEV_COMPOSE) up -d --wait --wait-timeout 240
dev-down:
	$(DEV_COMPOSE) down
dev-status:
	$(DEV_COMPOSE) ps
dev-logs:
	$(DEV_COMPOSE) logs -f --tail=100
dev-credentials:
	$(DEV_COMPOSE) exec crafty cat /crafty/app/config/default-creds.txt
dev-check:
	$(DEV_COMPOSE) config --quiet
dev-provider: build
	@mkdir -p bin
	@printf 'provider_installation {\n  dev_overrides {\n    "registry.terraform.io/bart-kochanowicz/crafty" = "%s/bin"\n  }\n  direct {}\n}\n' "$(CURDIR)" > bin/dev.tfrc
	@echo "Run: export TF_CLI_CONFIG_FILE=\"$(CURDIR)/bin/dev.tfrc\""

# Live acceptance tests use the existing disposable Compose environment.
.PHONY: test-acc
test-acc: dev-up
	@command -v "$(TERRAFORM)" >/dev/null
	TF_ACC=1 TF_ACC_TERRAFORM_PATH="$$(command -v "$(TERRAFORM)")" $(GO) test -v -race -count=1 -timeout 30m ./internal/provider -run '^TestAcc'

# Use a fresh crafty-provider-ci-* project; removes only that project's volumes.
.PHONY: test-acc-ci
test-acc-ci:
	TF_ACC_TERRAFORM_PATH="$$(command -v "$(TERRAFORM)")" GO="$(GO)" python3 dev/ci.py run

# Tools run at pinned versions without changing the provider's module dependencies.
TFPLUGINDOCS_VERSION := v0.25.0
GORELEASER_VERSION := v2.15.0
.PHONY: docs docs-check release-check release-snapshot
docs:
	$(GO) run github.com/hashicorp/terraform-plugin-docs/cmd/tfplugindocs@$(TFPLUGINDOCS_VERSION) generate --provider-name crafty
docs-check: docs
	git diff --exit-code -- docs
	@test -z "$$(git ls-files --others --exclude-standard docs)" || { echo "Generated documentation is untracked."; exit 1; }
release-check:
	$(GO) run github.com/goreleaser/goreleaser/v2@$(GORELEASER_VERSION) check
release-snapshot: release-check
	$(GO) run github.com/goreleaser/goreleaser/v2@$(GORELEASER_VERSION) release --snapshot --clean
