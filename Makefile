GO      := go
PKG     := ./cmd/preflight
BINARY  := tf-preflight
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT  := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
DATE    := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ" 2>/dev/null || echo "")
LDFLAGS := -ldflags "-s -w -X main.version=$(VERSION) -X main.gitCommit=$(COMMIT) -X main.buildDate=$(DATE)"
BIN_DIR := ./bin
INSTALL_DIR ?= $(HOME)/.local/bin

.PHONY: all build install install-system clean

all: build

build:
	@mkdir -p $(BIN_DIR)
	$(GO) build $(LDFLAGS) -o $(BIN_DIR)/$(BINARY) $(PKG)

install: build
	@mkdir -p "$(INSTALL_DIR)"
	@install -m 0755 $(BIN_DIR)/$(BINARY) "$(INSTALL_DIR)/$(BINARY)"
	@echo "Installed $(BINARY) to $(INSTALL_DIR)"
	@echo "If $(INSTALL_DIR) is not in PATH, add it:"
	@echo "  export PATH=\"$(INSTALL_DIR):$$PATH\""

install-system:
	@$(MAKE) install INSTALL_DIR=/usr/local/bin

clean:
	@rm -rf $(BIN_DIR)
