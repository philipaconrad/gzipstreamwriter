# Copyright 2024 Philip Conrad. All rights reserved.
# Use of this source code is governed by an Apache2
# license that can be found in the LICENSE file.

GOLANGCI_LINT_VERSION := v1.61.0
YAML_LINT_VERSION := 0.32.1
YAML_LINT_FORMAT ?= auto

DOCKER_RUNNING ?= $(shell docker ps >/dev/null 2>&1 && echo 1 || echo 0)

# We use root because the windows build, invoked through the ci-go-build-windows
# target, installs the gcc mingw32 cross-compiler.
# For image, it's overridden, so that the built binary isn't root-owned.
DOCKER_UID ?= 0
DOCKER_GID ?= 0

ifeq ($(shell tty > /dev/null && echo 1 || echo 0), 1)
DOCKER_FLAGS := --rm -it
else
DOCKER_FLAGS := --rm
endif

DOCKER := docker
FUZZ_TIME ?= 1h

######################################################
#
# Development targets
#
######################################################

# If you update the 'all' target make sure the 'ci-release-test' target is consistent.
.PHONY: all
all: build test check

.PHONY: build
build: go-build

.PHONY: test
test: go-test

.PHONY: go-build
go-build:
	$(GO) build $(GO_TAGS) ./...

.PHONY: go-test
go-test:
	$(GO) test $(GO_TAGS) ./... -count=1

.PHONY: race-detector
race-detector: generate
	$(GO) test $(GO_TAGS) -race ./... -count=1

.PHONY: test-coverage
test-coverage:
	$(GO) test $(GO_TAGS) -coverprofile=coverage.txt -covermode=atomic ./... -count=1

.PHONY: check
check:
ifeq ($(DOCKER_RUNNING), 1)
	docker run --rm -v $(shell pwd):/app:ro,Z -w /app golangci/golangci-lint:latest golangci-lint run -v
else
	@echo "Docker not installed or running. Skipping golangci run."
endif

.PHONY: fmt
fmt:
ifeq ($(DOCKER_RUNNING), 1)
	docker run --rm -v $(shell pwd):/app:Z -w /app golangci/golangci-lint:${GOLANGCI_LINT_VERSION} golangci-lint run -v --fix
else
	@echo "Docker not installed or running. Skipping golangci run."
endif

.PHONY: fuzz
fuzz:
	go test ./... -fuzz FuzzCRC32Combine -fuzztime ${FUZZ_TIME} -v -run '^$'

# Kept for compatibility. Use `make fuzz` instead.
.PHONY: check-fuzz
check-fuzz: fuzz

.PHONY: check-yaml-tests
check-yaml-tests:
ifeq ($(DOCKER_RUNNING), 1)
	docker run --rm -v $(shell pwd):/data:ro,Z -w /data pipelinecomponents/yamllint:${YAML_LINT_VERSION} yamllint -f $(YAML_LINT_FORMAT) test/cases/testdata
else
	@echo "Docker not installed or running. Skipping yamllint run."
endif
