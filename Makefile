MODULE     := github.com/husky-scheduler/husky
CLI        := husky
BIN_DIR    := bin
DIST_DIR   := dist

VERSION    ?= dev
COMMIT     ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
BUILD_DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS := -ldflags "\
	-X $(MODULE)/internal/version.Version=$(VERSION) \
	-X $(MODULE)/internal/version.Commit=$(COMMIT) \
	-X $(MODULE)/internal/version.BuildDate=$(BUILD_DATE)"

PLATFORMS := \
	linux/amd64 \
	linux/arm64 \
	darwin/amd64 \
	darwin/arm64 \
	windows/amd64

.PHONY: build web test lint clean run verify-cross dist package formula docs-install docs-dev docs-build help

## build: compile the husky binary into bin/
build: web
	@mkdir -p $(BIN_DIR)
	go build $(LDFLAGS) -o $(BIN_DIR)/$(CLI)    ./cmd/husky
	@echo "Built $(BIN_DIR)/$(CLI)"

## test: run all tests (Go + frontend)
test:
	go test -race -count=1 ./...
	cd web && npm ci --silent && npm test

## lint: run golangci-lint
lint:
	golangci-lint run ./...

## run: start the embedded daemon in the foreground (development)
run: build
	./$(BIN_DIR)/$(CLI) daemon run

## web: build the dashboard into internal/api/dashboard/
web:
	cd web && npm ci --silent && npm run build

## docs-install: install Docusaurus site dependencies
docs-install:
	cd docs-site && npm install

## docs-dev: run the Docusaurus docs site locally
docs-dev: docs-install
	cd docs-site && npm start

## docs-build: build the Docusaurus docs site
docs-build: docs-install
	cd docs-site && npm run build

## clean: remove build and dist artefacts
clean:
	rm -rf $(BIN_DIR) $(DIST_DIR)

## verify-cross: cross-compile husky for all supported platforms
verify-cross: web
	@mkdir -p $(DIST_DIR)/verify
	@$(foreach PLATFORM,$(PLATFORMS), \
		$(eval OS   := $(word 1,$(subst /, ,$(PLATFORM)))) \
		$(eval ARCH := $(word 2,$(subst /, ,$(PLATFORM)))) \
		$(eval EXT  := $(if $(filter windows,$(OS)),.exe,)) \
		$(eval DIR  := $(DIST_DIR)/verify/$(OS)_$(ARCH)) \
		echo "Verifying modernc.org/sqlite build on $(OS)/$(ARCH)" && \
		mkdir -p $(DIR) && \
		CGO_ENABLED=0 GOOS=$(OS) GOARCH=$(ARCH) go build $(LDFLAGS) -o $(DIR)/$(CLI)$(EXT)    ./cmd/husky ; \
	)
	@echo "Verified all cross-platform builds"

## dist: cross-compile and archive all supported platforms
dist: clean verify-cross
	@mkdir -p $(DIST_DIR)
	@$(foreach PLATFORM,$(PLATFORMS), \
		$(eval OS   := $(word 1,$(subst /, ,$(PLATFORM)))) \
		$(eval ARCH := $(word 2,$(subst /, ,$(PLATFORM)))) \
		$(eval EXT  := $(if $(filter windows,$(OS)),.exe,)) \
		$(eval DIR  := $(DIST_DIR)/$(OS)_$(ARCH)) \
		mkdir -p $(DIR) && \
		cp $(DIST_DIR)/verify/$(OS)_$(ARCH)/$(CLI)$(EXT)    $(DIR)/$(CLI)$(EXT) && \
		tar -czf $(DIST_DIR)/husky_$(OS)_$(ARCH).tar.gz -C $(DIR) . && \
		echo "  $(DIST_DIR)/husky_$(OS)_$(ARCH).tar.gz" ; \
	)
	@shasum -a 256 $(DIST_DIR)/husky_*.tar.gz > $(DIST_DIR)/checksums.txt
	@echo "  $(DIST_DIR)/checksums.txt"

## package: build release archives plus deb/rpm packages via GoReleaser snapshot mode
package:
	goreleaser release --snapshot --clean

## formula: render packaging/homebrew/husky.rb from dist/checksums.txt
formula: dist
	./scripts/render_homebrew_formula.sh $(VERSION) $(DIST_DIR)/checksums.txt packaging/homebrew/husky.rb packaging/homebrew/husky.rb.tmpl

## help: list available targets
help:
	@echo "Usage: make <target>"
	@sed -n 's/^## //p' $(MAKEFILE_LIST) | column -t -s ':'
