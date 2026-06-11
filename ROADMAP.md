# Roadmap

This roadmap tracks planned work that is visible to users and contributors. It is intentionally separate from the README so the README stays focused on installation and daily usage.

## Near Term

- **TUI batch apply workflow:** Add in-TUI suggestion review and safe-confirmed apply for multiple selected HPAs, equivalent to `list --problem --fix --apply`.
- **Health score explainability:** Add deeper score breakdowns in `--explain` and the TUI, including the rule name, evidence, and point deduction for each penalty.
- **E2E scenario coverage:** Expand kind E2E coverage for multi-metric HPAs, behavior policies, KEDA-style external metrics, VPA conflict detection, and stabilization boundary cases.
- **README sync quality gate:** Keep `README.md` and `README.ja.md` structurally aligned through `make docs-check` and CI.

## Medium Term

- **Custom / external metrics deep dive:** Add adapter-specific estimation and Prometheus/custom metrics verification hints beyond API discovery and HPA-visible freshness signals.
- **Report summary enhancement:** Add cluster-wide summary, bottom-N health scores, and recommended actions to generated reports.
- **Informer-based watch:** Add an opt-in informer update path for large clusters alongside the current polling mode.
- **KEP-6111 structured decision adapter:** Maintain an adapter boundary that can map future structured HPA decision fields into the existing analysis model without rewriting the CLI output layer.

## Release and Supply Chain

- Add cosign signing for release artifacts.
- Add SLSA provenance for release builds.
- Keep SBOM output in GoReleaser releases.
- Use pre-releases for experimental workflows and reserve stable releases for validated user-facing behavior.

## Community

- Label small, verifiable issues with `good first issue`.
- Keep contribution scopes explicit: target file or command, expected behavior, and validation command.
- Publish release highlights with user-facing changes, risks, and upgrade impact rather than commit hashes only.
