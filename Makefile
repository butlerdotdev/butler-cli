# Butler CLI Makefile
# Build both butleradm and butlerctl

# Go settings
GOOS ?= $(shell go env GOOS)
GOARCH ?= $(shell go env GOARCH)
CGO_ENABLED := 0

# Version information
VERSION ?= v0.1.0-dev
GIT_COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_DATE := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")

# Build flags
LDFLAGS := -s -w \
	-X github.com/butlerdotdev/butler/internal/version.Version=$(VERSION) \
	-X github.com/butlerdotdev/butler/internal/version.GitCommit=$(GIT_COMMIT) \
	-X github.com/butlerdotdev/butler/internal/version.BuildDate=$(BUILD_DATE)

# Output directories
BIN_DIR := bin
DIST_DIR := dist

# Targets
.PHONY: all
all: build

.PHONY: build
build: butleradm butlerctl

.PHONY: butleradm
butleradm:
	@echo "Building butleradm..."
	CGO_ENABLED=$(CGO_ENABLED) GOOS=$(GOOS) GOARCH=$(GOARCH) \
		go build -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/butleradm ./cmd/butleradm

.PHONY: butlerctl
butlerctl:
	@echo "Building butlerctl..."
	CGO_ENABLED=$(CGO_ENABLED) GOOS=$(GOOS) GOARCH=$(GOARCH) \
		go build -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/butlerctl ./cmd/butlerctl

.PHONY: install
install: build
	@echo "Installing to /usr/local/bin..."
	sudo install -m 755 $(BIN_DIR)/butleradm /usr/local/bin/
	sudo install -m 755 $(BIN_DIR)/butlerctl /usr/local/bin/

.PHONY: install-local
install-local: build
	@echo "Installing to ~/bin..."
	mkdir -p ~/bin
	install -m 755 $(BIN_DIR)/butleradm ~/bin/
	install -m 755 $(BIN_DIR)/butlerctl ~/bin/

# Cross-compilation for releases
.PHONY: dist
dist: dist-linux dist-darwin dist-windows

.PHONY: dist-linux
dist-linux:
	@echo "Building for Linux..."
	GOOS=linux GOARCH=amd64 $(MAKE) build
	mkdir -p $(DIST_DIR)/linux-amd64
	mv $(BIN_DIR)/butleradm $(BIN_DIR)/butlerctl $(DIST_DIR)/linux-amd64/
	GOOS=linux GOARCH=arm64 $(MAKE) build
	mkdir -p $(DIST_DIR)/linux-arm64
	mv $(BIN_DIR)/butleradm $(BIN_DIR)/butlerctl $(DIST_DIR)/linux-arm64/

.PHONY: dist-darwin
dist-darwin:
	@echo "Building for macOS..."
	GOOS=darwin GOARCH=amd64 $(MAKE) build
	mkdir -p $(DIST_DIR)/darwin-amd64
	mv $(BIN_DIR)/butleradm $(BIN_DIR)/butlerctl $(DIST_DIR)/darwin-amd64/
	GOOS=darwin GOARCH=arm64 $(MAKE) build
	mkdir -p $(DIST_DIR)/darwin-arm64
	mv $(BIN_DIR)/butleradm $(BIN_DIR)/butlerctl $(DIST_DIR)/darwin-arm64/

.PHONY: dist-windows
dist-windows:
	@echo "Building for Windows..."
	GOOS=windows GOARCH=amd64 $(MAKE) build
	mkdir -p $(DIST_DIR)/windows-amd64
	mv $(BIN_DIR)/butleradm $(BIN_DIR)/butlerctl $(DIST_DIR)/windows-amd64/

# Development
.PHONY: run-adm
run-adm:
	go run ./cmd/butleradm $(ARGS)

.PHONY: run-ctl
run-ctl:
	go run ./cmd/butlerctl $(ARGS)

.PHONY: test
test:
	go test -v -race ./...

.PHONY: test-coverage
test-coverage:
	go test -v -race -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

.PHONY: lint
lint:
	golangci-lint run ./...

.PHONY: fmt
fmt:
	go fmt ./...
	goimports -w .

.PHONY: vet
vet:
	go vet ./...

.PHONY: tidy
tidy:
	go mod tidy

.PHONY: generate
generate:
	go generate ./...

# Documentation
.PHONY: docs
docs: build
	@echo "Generating CLI documentation..."
	$(BIN_DIR)/butleradm generate docs --output docs/cli/butleradm
	$(BIN_DIR)/butlerctl generate docs --output docs/cli/butlerctl

# Docker
.PHONY: docker-build
docker-build:
	docker build -t butler-cli:$(VERSION) .

# Clean
.PHONY: clean
clean:
	rm -rf $(BIN_DIR) $(DIST_DIR)
	go clean -cache

# Help
.PHONY: help
help:
	@echo "Butler CLI Makefile"
	@echo ""
	@echo "Targets:"
	@echo "  build        Build both butleradm and butlerctl"
	@echo "  butleradm    Build butleradm only"
	@echo "  butlerctl    Build butlerctl only"
	@echo "  install      Install to /usr/local/bin (requires sudo)"
	@echo "  install-local Install to ~/bin"
	@echo "  dist         Build for all platforms"
	@echo "  test         Run tests"
	@echo "  lint         Run linter"
	@echo "  fmt          Format code"
	@echo "  clean        Clean build artifacts"
	@echo ""
	@echo "Development:"
	@echo "  run-adm ARGS='...'  Run butleradm with arguments"
	@echo "  run-ctl ARGS='...'  Run butlerctl with arguments"
