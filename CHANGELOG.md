# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- Added `--suggest`, `--fix`, and `--apply` workflows with structured patch suggestions.
- Added health scores, richer list output, metric bars, and compact behavior summaries.
- Added Japanese text labels through `--lang=ja` / `-o ja`.
- Added Makefile targets for build, test, coverage, lint, E2E, and release checks.
- Added Dependabot and CodeQL workflows.
- Added Renovate configuration, govulncheck, and gosec CI checks.
- Added GoReleaser SBOM and Homebrew Cask metadata for the dedicated tap.
- Added `scan` and `list --problem` for cluster-wide HPA problem triage.
- Added reusable status, list, and watch asciinema demo sources plus a comparison visual.
- Added a larger SVG screenshot gallery covering explain, list, watch, suggest, dry-run apply, Japanese output, JSON, and common failure states.
- Added architecture, security, RBAC, and richer issue/PR documentation.
- Added `version` subcommand for build metadata.

### Changed
- Upgraded Kubernetes client libraries to `k8s.io/*` v0.35.0.
- Expanded README badges, demo links, installation examples, and development documentation.
- Made `--apply` dry-run by default, with patch diff output and explicit `--dry-run=false` required for persistence.
- Added commit and build date to release version metadata.
- Added safety preconditions and warnings to structured suggestions, and made copy-paste patch commands dry-run by default.
- Expanded E2E command coverage for Japanese output and cluster-wide `scan`.
- Upload coverage to Codecov from CI while keeping coverage upload non-blocking.

## [0.2.0] - 2026-05-30

### Added
- **Multi-HPA Watch Mode:** Added support for periodically watching all HPAs or multiple HPAs using `kubectl hpa status list --watch` or the `-w` shorthand.
- **Robust Color Table Rendering:** Handled ANSI escape character length dynamically with `lipgloss.Width` to fix column alignment issues in colored output.
- **Enhanced Non-Resource Metric Parsing:** Added ratio and note calculations for Pods, Object, and External metric sources using `resource.Quantity` ratios.
- **Sorting Enhancements:** Added support to sort by current-desired difference (`--sort-by=diff`) and resource age (`--sort-by=age`).
- **Comprehensive E2E integration test suite:** Added `test/e2e/e2e_test.go` running on a temporary local `kind` cluster context.
- **Phase 2 Edge-Case Unit Tests:** Covered 10% HPA tolerance boundaries, maxReplicas multi-metric winner cases, and custom stabilization windows.
- **CI/CD Workflow Improvements:** Added a automated `kind` cluster setup and E2E testing to the GitHub Actions workflow.

### Fixed
- Prioritized issues in `NewListItem` to bubble up `ERROR` and `LIMITED` conditions cleanly in list output.
- Escaped percent formatting in test assertions.

## [0.1.0] - 2026-05-24

### Added
- Initial proof-of-concept release.
- Interactive status analysis of HPA scaling parameters based on K8s API signals.
- Single HPA watch, list filters, and basic YAML/JSON format output support.
