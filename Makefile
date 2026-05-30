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

.PHONY: coverage
coverage:
	$(GO) test -coverprofile=$(COVERAGE_OUT) ./...
	$(GO) tool cover -func=$(COVERAGE_OUT)

.PHONY: lint
lint:
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
release-check:
	$(GORELEASER) check

.PHONY: release
release:
	$(GORELEASER) release --clean

.PHONY: clean
clean:
	$(GO) clean
	rm -f $(BIN) $(COVERAGE_OUT)
