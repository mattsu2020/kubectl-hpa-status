GO ?= go
GORELEASER ?= goreleaser
KUBECTL ?= kubectl

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

.PHONY: ci
ci: tidy build vet lint fmt-check facade-check test test-race coverage-check docs-check
	@echo "local CI checks passed"

.PHONY: tidy
tidy:
	$(GO) mod tidy
	@git diff --exit-code go.mod go.sum || (echo "go.mod/go.sum not tidy; run 'go mod tidy' and commit" && exit 1)

.PHONY: fmt
fmt:
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run --fix ./...; \
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

.PHONY: facade-check
facade-check:
	$(GO) run ./scripts/check-deprecated-facades

.PHONY: coverage
coverage:
	$(GO) test -coverprofile=$(COVERAGE_OUT) ./...
	$(GO) tool cover -func=$(COVERAGE_OUT)

# coverage-check always regenerates the profile: a file-based target would
# silently validate a stale coverage.out left over from an earlier run.
.PHONY: coverage-check
coverage-check: coverage
	bash scripts/check-coverage.sh $(COVERAGE_OUT)

.PHONY: docs-check
docs-check:
	bash scripts/check-readme-sync.sh

.PHONY: lint
lint:
	golangci-lint run ./...

.PHONY: vet
vet:
	$(GO) vet ./...

.PHONY: e2e
e2e:
	$(GO) test -v -tags=e2e ./test/e2e/...

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
