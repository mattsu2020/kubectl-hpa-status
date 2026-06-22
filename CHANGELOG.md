# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Changed
- Multi-HPA `status NAME1 NAME2 ...` now produces **partial results**: a per-item fetch/build failure no longer aborts the whole batch. Successful items are rendered normally, and the failed item is surfaced as an error entry in the output.
- Exit code for multi-HPA runs now reflects the most severe per-item outcome: any build failure → exit `1` (`error`), otherwise any warning-health item → exit `2` (`warning`), otherwise exit `0`. Previously the first failing HPA aborted the whole run with no output.

### Changed (breaking)
- `status NAME1 NAME2 -o json` and `-o yaml` now emit a `StatusBatch` envelope instead of a bare `[]StatusReport` array. New shape:
  ```json
  {
    "apiVersion": "hpa-status/v1",
    "items": [
      {"namespace": "default", "name": "web", "status": "ok", "report": { ... }},
      {"namespace": "default", "name": "missing", "status": "error", "error": "HPA \"missing\" was not found ..."}
    ]
  }
  ```
  Single-HPA `status NAME -o json` is unchanged (still a bare `StatusReport`). Failed items have `status: "error"`, a non-empty `error` string, and no `report` field.

## [0.10.0] - 2026-06-13

### Added
- Added visualization, real-time monitoring, and explainability features across the analysis pipeline.
- Added a `bundle` subcommand and an incident report output format with policy guard, adapter diagnostics, and rich incident Markdown.
- Added readiness, rollout, scale-out, and controller-profile diagnosis (`--readiness-impact`, `--rollout`, `--scaleout-blockers`, `--controller-profile`, `--decision-trace`).
- Added a `why-not-scale` subcommand to diagnose scaling blockers.
- Added an `advisor` command with a `container-resource` subcommand.
- Added `ownership`, `fleet`, `readiness`, and `profile detect` subcommands plus a `policy init` command.
- Added flapping diagnosis with replica-range analysis and HPA conflict detection.
- Added estimated scaling reasons, HPA health-trend tracking, and enhanced stabilization display.
- Added GitOps and policy workflows to HPA comparison and analytics.
- Added multiple candidate configurations, pod-hours, and capped-duration metrics to the replay lab.
- Added `--explain` and assumption override flags to the assumptions command, plus a `--startup-context` flag.
- Added a dedicated roadmap document, a `make docs-check` README synchronization check, and five-minute quick start sections to both READMEs.

### Changed
- Renamed diagnostic confidence labels to an observed/estimated model.
- Refactored lint rules to use `context`; formatted struct field alignments and removed unused types.
- Expanded documentation: roadmap updates, jUnit/SARIF and carbon-cost notes, TUI manual links, and an expanded workflow gallery.

### Fixed
- Fixed deferred `Close`/`Chdir` error handling and explicit write-result handling in bundle Markdown rendering.
- Fixed the Japanese README CI badge repository URL and synchronized README content.

## [0.9.0] - 2026-06-08

### Added
- Added a `lint` command and enabled advisor flags in `doctor`.
- Added warmup analysis for post-scale-out readiness.
- Added churn detection, metric hints, VPA advisory, and a history view.

## [0.8.0] - 2026-06-08

### Added
- Added GitOps and metric-contract checks plus profile-based recommendations.
- Added a `capacity-plan` command for diagnosing `maxReplicas` safety.
- Added a scale-out blockers command with deep capacity analysis.
- Added a `--scale-path` flag to explain the HPA scaling path.
- Added a metrics-freshness analyzer (`--metrics-freshness`) for metric staleness diagnosis.
- Added a `policy` command and interactive TUI simulation views.
- Added a retrospective scaling timeline (`timeline` subcommand and `--since` flag).

### Changed
- Refactored `runList` into focused helper functions.
- Documented the doctor subcommand, the before/after `--explain` comparison, and simplified Krew install instructions.
- Bumped the Kubernetes dependency group and `codecov/codecov-action` from v5 to v6.

## [0.7.0] - 2026-06-06

### Added
- Added a What-If Simulator, Multi-Metric Trace, and Best-Practice Auditor.
- Exposed KEDA/VPA enrichment skip reasons in debug output.

### Changed
- Unified diagnostics, improved health weights, and hardened apply safety with client reuse.
- Introduced typed health states and signals and made diagnostic confidence a structured field.
- Split `AnalyzeWithOptions` into analysis phases and extracted `statusOptions.Normalize()` and rule-based suggestions.
- Refactored the options struct into sub-structs and narrowed helper functions.

### Fixed
- Improved metric matching, health scoring, and diagnostic accuracy.
- Met the CI coverage threshold and resolved golangci-lint findings.

## [0.6.0] - 2026-06-04

### Added
- Added simulation, capacity-context, and replay commands.
- Added comprehensive codebase improvements across six phases.

### Changed
- Slimmed the README to ~165 lines, moving details into `docs/`.

### Fixed
- Resolved golangci-lint errors from CI.

## [0.5.0] - 2026-06-02

### Added
- Added auto-detection of KEDA ScaledObject and VPA conflict enrichment.
- Added KEDA/VPA health penalties and `scaleTargetRef` validation.

### Changed
- Configured release-notes generation and included docs in release notes.
- Updated contributing guidelines, architecture, and the Japanese README.

## [0.4.0] - 2026-06-02

### Added
- Added shell completion support.
- Added configuration file support and a documented example config.
- Added JSON output schema documentation.
- Added cluster-wide resource context and deeper KEDA/VPA diagnostics.
- Added autoscaler diagnostic reports.
- Added TUI and large-cluster usability improvements.
- Added score-based tier labels to health visualization.

### Changed
- Expanded README and architecture documentation for command coverage, JSON schema, Krew status, and supported Kubernetes versions.
- Expanded HPA diagnostics and large-cluster UX.
- Deepened KEDA analysis and autoscaler troubleshooting output.
- Resolved golangci-lint findings before release.

## [0.3.0] - 2026-06-01

### Added
- Added `--suggest`, `--fix`, and `--apply` workflows with structured patch suggestions.
- Added health scores, richer list output, metric bars, and compact behavior summaries.
- Added Japanese text labels through `--lang=ja` / `-o ja`.
- Added Makefile targets for build, test, coverage, lint, E2E, and release checks.
- Added Dependabot and CodeQL workflows.
- Added Renovate configuration, govulncheck, and gosec CI checks.
- Added GoReleaser SBOM and Homebrew Cask metadata for the dedicated tap.
- Added `scan` and `list --problem` for cluster-wide HPA problem triage.
- Added `list --health-score <threshold>` for filtering HPAs by low health score.
- Added reusable status, list, and watch asciinema demo sources plus a comparison visual.
- Added a larger SVG screenshot gallery covering explain, list, watch, suggest, dry-run apply, Japanese output, JSON, and common failure states.
- Added architecture, security, RBAC, and richer issue/PR documentation.
- Added `version` subcommand for build metadata.
- Added practical `examples/` manifests for CPU/memory, behavior, custom metrics, and KEDA-style HPA scenarios.

### Changed
- Upgraded Kubernetes client libraries to `k8s.io/*` v0.35.0.
- Expanded README badges, demo links, installation examples, and development documentation.
- Clarified Krew command naming, dry-run modes, and explicit HPA analysis limitations in README and Krew caveats.
- Made `--apply` dry-run by default, with patch diff output and explicit `--dry-run=false` required for persistence.
- Added commit and build date to release version metadata.
- Added safety preconditions and warnings to structured suggestions, and made copy-paste patch commands dry-run by default.
- Expanded E2E command coverage for Japanese output and cluster-wide `scan`.
- Upload coverage to Codecov from CI while keeping coverage upload non-blocking.
- Expanded Japanese README coverage to match the English usage, safety, CI/CD, validation, and known-gap sections.
- Hardened HPA analysis nil handling and moved health score penalties into named constants.

### Fixed
- Fixed GoReleaser Homebrew and SBOM configuration issues.
- Fixed security scanner findings and lint failures across CLI, tests, and CI configuration.
- Fixed E2E handling for HPAs with ERROR health status.

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
