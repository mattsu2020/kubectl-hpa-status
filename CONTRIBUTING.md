# Contributing

Thanks for helping improve `kubectl-hpa-status`.

## Quick Start (5 minutes)

```bash
# 1. Clone and build
git clone https://github.com/mattsu2020/kubectl-hpa-status.git
cd kubectl-hpa-status
make build

# 2. Run tests
make test

# 3. Try it locally (requires a kubeconfig pointing to a cluster)
./kubectl-hpa-status list -A --wide

# 4. Run with a specific HPA
./kubectl-hpa-status status my-hpa --explain

# 5. Run E2E tests (requires kind)
kind create cluster
make e2e
kind delete cluster
```

Prerequisites: Go 1.25+, kubectl, a Kubernetes cluster (or kind for E2E).

## Development

```sh
make build
make test
make coverage
make lint
make release-check
```

Run the plugin locally:

```sh
./kubectl-hpa-status status <hpa-name> -n <namespace>
./kubectl-hpa-status list -A
./kubectl-hpa-status list -A --sort-by desired --filter scaling-limited
./kubectl-hpa-status scan
./kubectl-hpa-status status <hpa-name> --watch --timeout 2m
```

For cluster-backed validation, point `kubectl` at a disposable cluster and run:

```sh
kind create cluster --name hpa-status-dev
make e2e
kind delete cluster --name hpa-status-dev
```

When changing `--suggest`, `--fix`, or `--apply`, keep the workflow safe by
default. `--apply` must show the proposed patch diff, run as dry-run unless
`--dry-run=false` is explicitly set, and require confirmation unless `-y` is
provided.

## Adding interpretation rules

Interpretation rules live in `pkg/hpa/analysis.go`.
Concrete patch suggestions live in `pkg/hpa/suggestions.go`.

When adding a rule:

- prefer explicit HPA status fields over Event message parsing
- add a confidence label when the output is inferential
- avoid claiming the HPA controller's private intermediate recommendation
- add or update a focused unit test in `pkg/hpa/analysis_test.go`
- add command behavior tests in `cmd/root_integration_test.go` when flags or apply behavior change
- document any new user-facing output in `README.md`

For list output changes, update `pkg/hpa/text.go` and cover the table behavior
with tests. For command flags, add tests in `cmd/root_test.go` when the behavior
can be checked without a live cluster.

## Good first contribution areas

- Documentation: keep `README.md` and `README.ja.md` aligned when flags,
  examples, install paths, or limitations change.
- Translation: improve Japanese wording in `README.ja.md` and user-facing
  labels without changing command semantics.
- Community content: turn troubleshooting patterns into short blog posts,
  demo recordings, or release notes. Good candidates are `Metrics unavailable`,
  `ScaleDownStabilized`, KEDA external metrics, and cluster-wide `scan`.
- Testdata: add focused manifests under `testdata/` or `examples/` for KEDA,
  custom/external metrics, stabilization windows, and not-ready scale targets.
- Analysis tests: cover edge cases in `pkg/hpa/analysis.go` and
  `pkg/hpa/suggestions.go`, especially cases where HPA status is ambiguous.
- UX tests: add command-level tests for new flags, sorting, filtering, output
  formats, and completion behavior.

When opening issues for first-time contributors, prefer small, verifiable
scopes and add the `good first issue` label. Include:

- the file or command to change
- the expected user-visible behavior
- the validation command, such as `make test`, `make coverage`, or a specific
  `kubectl-hpa-status` invocation
- whether Japanese and English documentation both need updates

## Krew manifest

The Krew plugin name is intentionally `hpa-status`. Keep `.krew.yaml`,
GoReleaser archive names, and README install commands aligned when release
metadata changes.

## Commit style

Use Conventional Commits where practical:

```text
feat: add hpa list command
fix: avoid treating inactive desiredReplicas as scale down
test: cover tolerance-like no-scale interpretation
```

## Pull requests

Include:

- the user-visible behavior changed
- how it was tested
- any remaining HPA API ambiguity the implementation intentionally avoids claiming
