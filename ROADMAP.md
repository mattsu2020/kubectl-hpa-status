# Roadmap

This roadmap tracks planned work that is visible to users and contributors. It is intentionally separate from the README so the README stays focused on installation and daily usage.

## Near Term

- **TUI batch apply workflow:** Add in-TUI suggestion review and safe-confirmed apply for multiple selected HPAs, equivalent to `list --problem --fix --apply`.
- **Health score explainability:** Add deeper score breakdowns in `--explain` and the TUI, including the rule name, evidence, and point deduction for each penalty.
- **E2E scenario coverage:** Expand kind E2E coverage for multi-metric HPAs, behavior policies, KEDA-style external metrics, VPA conflict detection, and stabilization boundary cases.
- **README sync quality gate:** Keep `README.md` and `README.ja.md` structurally aligned through `make docs-check` and CI.

## Medium Term

- **Informer-based watch:** Add an opt-in informer update path for large clusters alongside the current polling mode.
- **KEP-6111 structured decision adapter:** Maintain an adapter boundary that can map future structured HPA decision fields into the existing analysis model without rewriting the CLI output layer.

## Recently Added

- **Durable decision recording:** `record` writes JSONL HPA snapshots and `timeline --from-record` replays them after Events expire.
- **Preflight and impact commands:** `preflight`, `behavior`, and `estimate` cover capacity validation, behavior visualization, and rough cost impact.
- **Metrics adapter probe:** `metrics probe` combines freshness, contract, adapter diagnostics, and metric hints for custom/external metrics.
- **CI/report outputs:** `lint -o github` emits GitHub Actions annotations and `scan --summary --report markdown|html` produces cluster summary reports.

## Release and Supply Chain

- Add cosign signing for release artifacts.
- Add SLSA provenance for release builds.
- Keep SBOM output in GoReleaser releases.
- Use pre-releases for experimental workflows and reserve stable releases for validated user-facing behavior.

## Community

- Label small, verifiable issues with `good first issue`.
- Keep contribution scopes explicit: target file or command, expected behavior, and validation command.
- Publish release highlights with user-facing changes, risks, and upgrade impact rather than commit hashes only.
