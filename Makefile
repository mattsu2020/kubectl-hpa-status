GO ?= go
GORELEASER ?= goreleaser
KUBECTL ?= kubectl

BIN := kubectl-hpa-status
COVERAGE_OUT := coverage.out

.PHONY: build
build:
	$(GO) build -o $(BIN) .

.PHONY: test
test:
	$(GO) test ./...

.PHONY: test-race
test-race:
	$(GO) test -race ./...

.PHONY: ci
ci: build vet lint test test-race docs-check
	@echo "local CI checks passed"

.PHONY: tidy
tidy:
	$(GO) mod tidy
	@git diff --exit-code go.mod go.sum || (echo "go.mod/go.sum not tidy; run 'go mod tidy' and commit" && exit 1)

.PHONY: coverage
coverage:
	$(GO) test -coverprofile=$(COVERAGE_OUT) ./...
	$(GO) tool cover -func=$(COVERAGE_OUT)

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

.PHONY: krew
krew:
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
