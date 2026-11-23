GO ?= go
GORELEASER ?= goreleaser
KUBECTL ?= kubectl
# Pinned to the same version the CI workflows install so a contributor's
# local lint findings match CI. Keep in sync with .github/workflows/ci.yml
# and release.yml (Renovate keeps all three aligned).
GOLANGCI_LINT_VERSION ?= v2.12.2

BIN := kubectl-hpa-status
COVERAGE_OUT := coverage.out
# Match release stripping (-s -w) so local binaries stay closer to release size.
# Override with LDFLAGS= for debug builds that need full symbols.
LDFLAGS ?= -s -w

.PHONY: build
build:
	$(GO) build -ldflags="$(LDFLAGS)" -o $(BIN) .

.PHONY: test
test:
	$(GO) test -coverprofile=$(COVERAGE_OUT) ./...
	@$(GO) tool cover -func=$(COVERAGE_OUT) > /dev/null

.PHONY: test-race
test-race:
	$(GO) test -race -covermode=atomic ./...

# test-ci is the single combined pass the CI workflow and `make ci` use:
# every test executed once, under the race detector, with the coverage
# profile written in the same run.
.PHONY: test-ci
test-ci:
	$(GO) test -race -coverprofile=$(COVERAGE_OUT) -covermode=atomic ./...
	@$(GO) tool cover -func=$(COVERAGE_OUT) > /dev/null

.PHONY: ci
ci: tidy build vet lint fmt-check test-ci coverage-check docs-check
	@echo "local CI checks passed"

.PHONY: tidy
tidy:
	$(GO) mod tidy
	@git diff --exit-code go.mod go.sum || (echo "go.mod/go.sum not tidy; run 'go mod tidy' and commit" && exit 1)

.PHONY: fmt
fmt:
	@if command -v golangci-lint >/dev/null 2>&1; then \
		bash scripts/run-golangci-lint.sh run --fix ./...; \
	else \
		echo "golangci-lint not found; falling back to gofmt (install golangci-lint for full fixing)"; \
		gofmt -w .; \
	fi

.PHONY: fmt-check
fmt-check:
	@out=$$(gofmt -l .); \
	if [ -n "$$out" ]; then \
		echo "gofmt would modify the following files; run 'make fmt' and commit:"; \
		echo "$$out"; \
		exit 1; \
	fi
	@if command -v golangci-lint >/dev/null 2>&1; then \
		bash scripts/run-golangci-lint.sh fmt --diff ./... || (echo "golangci-lint fmt would reformat files (usually import grouping); run 'make fmt' and commit" && exit 1); \
	else \
		echo "golangci-lint not found; verified gofmt only (install golangci-lint to also check import grouping)"; \
	fi

.PHONY: coverage
coverage:
	$(GO) test -coverprofile=$(COVERAGE_OUT) ./...
	$(GO) tool cover -func=$(COVERAGE_OUT)

# coverage-check validates the profile produced by `test-ci` (or `test`).
# It regenerates the profile only when it is missing, so `make ci` runs the
# suite exactly once. An existing profile that predates the current sources is
# rejected rather than trusted: run `make test-ci` to refresh it deliberately.
.PHONY: coverage-check
coverage-check:
	@if [ ! -f $(COVERAGE_OUT) ]; then \
		$(GO) test -coverprofile=$(COVERAGE_OUT) ./...; \
	else \
		stale=$$(find . -name '*.go' -newer $(COVERAGE_OUT) -not -path './.git/*' | head -1); \
		if [ -n "$$stale" ]; then \
			echo "$(COVERAGE_OUT) is older than $$stale; run 'make test-ci' to refresh it before checking coverage"; \
			exit 1; \
		fi; \
	fi
	bash scripts/check-coverage.sh $(COVERAGE_OUT)

.PHONY: docs-check
docs-check:
	bash scripts/check-readme-sync.sh

.PHONY: lint
# The linter runs through scripts/run-golangci-lint.sh, which lines the Go
# toolchain up with the one golangci-lint was built with (a newer local Go
# makes the linter panic while loading build-tagged dependency files).
lint:
	@if command -v golangci-lint >/dev/null 2>&1; then \
		found=v$$(golangci-lint version 2>/dev/null | head -1 | grep -oE '[0-9]+\.[0-9]+\.[0-9]+' | head -1); \
		if [ "$$found" != "$(GOLANGCI_LINT_VERSION)" ]; then \
			echo "::warning::golangci-lint $$found found, CI pins $(GOLANGCI_LINT_VERSION); findings may differ from CI"; \
		fi; \
		bash scripts/run-golangci-lint.sh run ./...; \
	else \
		echo "::error::golangci-lint not found. CI runs $(GOLANGCI_LINT_VERSION); install it (https://golangci-lint.run/welcome/install/) or use 'make ci' inside the dev container."; \
		exit 1; \
	fi

.PHONY: vet
vet:
	$(GO) vet ./...

.PHONY: e2e
e2e:
	$(GO) test -v -tags=e2e ./test/e2e/...

# e2e-kind drives scripts/e2e-kind.sh: creates a throwaway kind cluster,
# installs metrics-server (plus KEDA/VPA CRDs via INSTALL_KEDA/INSTALL_VPA),
# exercises the built binary against it, and tears the cluster down.
.PHONY: e2e-kind
e2e-kind:
	bash scripts/e2e-kind.sh

.PHONY: dev
dev: build
	./$(BIN) --help

# snapshot builds an unsigned local release with goreleaser (no publishing).
.PHONY: snapshot
snapshot:
	$(GORELEASER) release --snapshot --clean --skip=publish

.PHONY: release-check
release-check: docs-check
	$(GORELEASER) check

.PHONY: release
release:
	$(GORELEASER) release --clean

.PHONY: clean
clean:
	$(GO) clean
	rm -f $(BIN) $(COVERAGE_OUT)
