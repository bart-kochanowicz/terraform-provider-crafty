GO ?= go
VERSION ?= 0.1.0
GOLANGCI_LINT ?= golangci-lint
TERRAFORM ?= terraform
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
