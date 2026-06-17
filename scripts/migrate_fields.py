#!/usr/bin/env python3
"""One-shot field migration for cmd/*.go. Run from repo root."""
import pathlib
import re
import sys

root = pathlib.Path(__file__).resolve().parent.parent / "cmd"

replacements = [
    (r"opts\.features\.noInterpret", "opts.NoInterpret"),
    (r"opts\.features\.contextForAI", "opts.ContextForAI"),
    (r"opts\.features\.diagnoseMetrics", "opts.DiagnoseMetrics"),
    (r"opts\.features\.metricsFreshness", "opts.MetricsFreshness"),
    (r"opts\.features\.metricContract", "opts.MetricContract"),
    (r"opts\.features\.adapterDiagnostics", "opts.AdapterDiagnostics"),
    (r"opts\.features\.metricHints", "opts.MetricHints"),
    (r"opts\.features\.checkResources", "opts.CheckResources"),
    (r"opts\.features\.explainPods", "opts.ExplainPods"),
    (r"opts\.features\.capacityContext", "opts.CapacityContext"),
    (r"opts\.features\.capacityHeadroom", "opts.CapacityHeadroom"),
    (r"opts\.features\.capacityDeep", "opts.CapacityDeep"),
    (r"opts\.features\.capacityPlan", "opts.CapacityPlan"),
    (r"opts\.features\.scalePath", "opts.ScalePath"),
    (r"opts\.features\.nodeAutoscaler", "opts.NodeAutoscaler"),
    (r"opts\.features\.scaleoutBlockers", "opts.ScaleoutBlockers"),
    (r"opts\.features\.controllerProfile", "opts.ControllerProfile"),
    (r"opts\.features\.decisionTrace", "opts.DecisionTrace"),
    (r"opts\.features\.gitopsCheck", "opts.GitOpsCheck"),
    (r"opts\.features\.churnDetect", "opts.ChurnDetect"),
    (r"opts\.features\.flappingAdvisor", "opts.FlappingAdvisor"),
    (r"opts\.features\.trendAnomaly", "opts.TrendAnomaly"),
    (r"opts\.features\.containerAdvisor", "opts.ContainerAdvisor"),
    (r"opts\.features\.behaviorAdvisor", "opts.BehaviorAdvisor"),
    (r"opts\.features\.readinessImpact", "opts.ReadinessImpact"),
    (r"opts\.features\.rolloutImpact", "opts.RolloutImpact"),
    (r"opts\.features\.hiddenFactors", "opts.HiddenFactors"),
    (r"opts\.features\.recommend", "opts.Recommend"),
    (r"opts\.features\.interpret", "opts.Interpret"),
    (r"opts\.features\.explain", "opts.Explain"),
    (r"opts\.features\.suggest", "opts.Suggest"),
    (r"opts\.features\.fix", "opts.Fix"),
    (r"opts\.features\.rollout", "opts.Rollout"),
    (r"opts\.features\.karpenter", "opts.Karpenter"),
    (r"local\.features\.", "local."),
    (r"bundleOpts\.features\.", "bundleOpts."),
    (r"scanOpts\.features\.", "scanOpts."),
    (r"clone\.features\.", "clone."),
    (r"o\.features\.", "o."),
    (r"opts\.noInterpret", "opts.NoInterpret"),
    (r"opts\.contextForAI", "opts.ContextForAI"),
    (r"opts\.diagnoseMetrics", "opts.DiagnoseMetrics"),
    (r"opts\.metricsFreshness", "opts.MetricsFreshness"),
    (r"opts\.metricContract", "opts.MetricContract"),
    (r"opts\.adapterDiagnostics", "opts.AdapterDiagnostics"),
    (r"opts\.metricHints", "opts.MetricHints"),
    (r"opts\.checkResources", "opts.CheckResources"),
    (r"opts\.explainPods", "opts.ExplainPods"),
    (r"opts\.capacityContext", "opts.CapacityContext"),
    (r"opts\.capacityHeadroom", "opts.CapacityHeadroom"),
    (r"opts\.capacityDeep", "opts.CapacityDeep"),
    (r"opts\.capacityPlan", "opts.CapacityPlan"),
    (r"opts\.scalePath", "opts.ScalePath"),
    (r"opts\.nodeAutoscaler", "opts.NodeAutoscaler"),
    (r"opts\.scaleoutBlockers", "opts.ScaleoutBlockers"),
    (r"opts\.controllerProfile", "opts.ControllerProfile"),
    (r"opts\.decisionTrace\b", "opts.DecisionTrace"),
    (r"opts\.gitopsCheck", "opts.GitOpsCheck"),
    (r"opts\.churnDetect", "opts.ChurnDetect"),
    (r"opts\.flappingAdvisor", "opts.FlappingAdvisor"),
    (r"opts\.trendAnomaly", "opts.TrendAnomaly"),
    (r"opts\.containerAdvisor", "opts.ContainerAdvisor"),
    (r"opts\.behaviorAdvisor", "opts.BehaviorAdvisor"),
    (r"opts\.readinessImpact", "opts.ReadinessImpact"),
    (r"opts\.rolloutImpact", "opts.RolloutImpact"),
    (r"opts\.hiddenFactors", "opts.HiddenFactors"),
    (r"opts\.recommend\b", "opts.Recommend"),
    (r"opts\.interpret\b", "opts.Interpret"),
    (r"opts\.explain\b", "opts.Explain"),
    (r"opts\.suggest\b", "opts.Suggest"),
    (r"opts\.fix\b", "opts.Fix"),
    (r"opts\.rollout\b", "opts.Rollout"),
    (r"opts\.karpenter", "opts.Karpenter"),
    (r"local\.namespace", "local.Namespace"),
    (r"local\.output", "local.Output"),
    (r"clone\.contextName", "clone.ContextName"),
    (r"scanOpts\.allNamespaces", "scanOpts.AllNamespaces"),
    (r"scanOpts\.problem", "scanOpts.Problem"),
    (r"scanOpts\.wide", "scanOpts.Wide"),
    (r"opts\.allNamespaces", "opts.AllNamespaces"),
    (r"opts\.contextName", "opts.ContextName"),
    (r"opts\.kubeconfig", "opts.Kubeconfig"),
    (r"opts\.namespace", "opts.Namespace"),
    (r"opts\.outputTemplates", "opts.OutputTemplates"),
    (r"opts\.clientOverride", "opts.ClientOverride"),
    (r"opts\.healthWeightOverrides", "opts.HealthWeightOverrides"),
    (r"opts\.healthWeights", "opts.HealthWeights"),
    (r"opts\.exportPatch", "opts.ExportPatch"),
    (r"opts\.chunkSize", "opts.ChunkSize"),
    (r"opts\.concurrency", "opts.Concurrency"),
    (r"opts\.allowPartial", "opts.AllowPartial"),
    (r"opts\.watchInterval", "opts.WatchInterval"),
    (r"opts\.watchTimeout", "opts.WatchTimeout"),
    (r"opts\.untilCondition", "opts.UntilCondition"),
    (r"opts\.healthScoreMin", "opts.HealthScoreMin"),
    (r"opts\.healthScoreMax", "opts.HealthScoreMax"),
    (r"opts\.gitopsDrift", "opts.GitOpsDrift"),
    (r"opts\.sortBy", "opts.SortBy"),
    (r"opts\.simulateMetric", "opts.SimulateMetric"),
    (r"opts\.simulateDuration", "opts.SimulateDuration"),
    (r"opts\.assumeProfile", "opts.AssumeProfile"),
    (r"opts\.controllerProfileFile", "opts.ControllerProfileFile"),
    (r"opts\.manifestPath", "opts.ManifestPath"),
    (r"opts\.decisionTraceFormat", "opts.DecisionTraceFormat"),
    (r"opts\.incidentTemplate", "opts.IncidentTemplate"),
    (r"opts\.policyGuardMode", "opts.PolicyGuardMode"),
    (r"opts\.policyGuard", "opts.PolicyGuard"),
    (r"opts\.targetMax", "opts.TargetMax"),
    (r"opts\.dryRun", "opts.DryRun"),
    (r"opts\.template", "opts.Template"),
    (r"opts\.selector", "opts.Selector"),
    (r"opts\.output", "opts.Output"),
    (r"opts\.config", "opts.Config"),
    (r"opts\.cluster", "opts.Cluster"),
    (r"opts\.dashboard", "opts.Dashboard"),
    (r"opts\.simulate", "opts.Simulate"),
    (r"opts\.conflicts", "opts.Conflicts"),
    (r"opts\.summary", "opts.Summary"),
    (r"opts\.problem", "opts.Problem"),
    (r"opts\.filter", "opts.Filter"),
    (r"opts\.export", "opts.Export"),
    (r"opts\.report", "opts.Report"),
    (r"opts\.format", "opts.Format"),
    (r"opts\.events", "opts.Events"),
    (r"opts\.watch", "opts.Watch"),
    (r"opts\.apply", "opts.Apply"),
    (r"opts\.color", "opts.Color"),
    (r"opts\.debug", "opts.Debug"),
    (r"opts\.diff", "opts.Diff"),
    (r"opts\.trend", "opts.Trend"),
    (r"opts\.wide", "opts.Wide"),
    (r"opts\.lang", "opts.Lang"),
    (r"opts\.keda", "opts.KEDA"),
    (r"opts\.burst", "opts.Burst"),
    (r"opts\.qps", "opts.QPS"),
    (r"opts\.in\b", "opts.In"),
    (r"opts\.yes", "opts.Yes"),
    (r"opts\.ask", "opts.Ask"),
    (r"opts\.vpa", "opts.VPA"),
    (r"opts\.trendSince", "opts.TrendSince"),
    (r"opts\.trendRetain", "opts.TrendRetain"),
    (r"o\.apply", "o.Apply"),
    (r"o\.diff", "o.Diff"),
    (r"o\.export", "o.Export"),
    (r"o\.exportPatch", "o.ExportPatch"),
    (r"o\.format", "o.Format"),
    (r"o\.decisionTraceFormat", "o.DecisionTraceFormat"),
    (r"o\.ask", "o.Ask"),
    (r"o\.trend", "o.Trend"),
    (r"o\.namespace", "o.Namespace"),
    (r"o\.suggest", "o.Suggest"),
    (r"o\.explain", "o.Explain"),
    (r"o\.interpret", "o.Interpret"),
    (r"o\.noInterpret", "o.NoInterpret"),
    (r"o\.recommend", "o.Recommend"),
    (r"o\.fix", "o.Fix"),
    (r"o\.contextForAI", "o.ContextForAI"),
    (r"o\.decisionTrace", "o.DecisionTrace"),
    (r"o\.hiddenFactors", "o.HiddenFactors"),
    (r"o\.diagnoseMetrics", "o.DiagnoseMetrics"),
    (r"o\.metricHints", "o.MetricHints"),
    (r"eventOption\{", "EventOption{"),
    (r"\.events\.enabled", ".Events.Enabled"),
    (r"\.events\.limit", ".Events.Limit"),
    (r"commonOptions: commonOptions\{", "Common: commonOptions{"),
    (r"statusOptions: statusOptions\{", "Status: statusOptions{"),
    (r"listOptions:   listOptions\{", "List: listOptions{"),
    (r"listOptions: listOptions\{", "List: listOptions{"),
    (r"watchOptions:   watchOptions\{", "Watch: watchOptions{"),
    (r"watchOptions: watchOptions\{", "Watch: watchOptions{"),
]

# Struct literal field renames (test helpers) — avoid touching unrelated structs by scoping to common patterns
literal_replacements = [
    (r"clientOverride:", "ClientOverride:"),
    (r"namespace:", "Namespace:"),
    (r"output:", "Output:"),
    (r"color:", "Color:"),
    (r"debug:", "Debug:"),
    (r"healthScoreMin:", "HealthScoreMin:"),
    (r"healthScoreMax:", "HealthScoreMax:"),
    (r"conflicts:", "Conflicts:"),
    (r"targetMax:", "TargetMax:"),
    (r"events:", "Events:"),
    (r"enabled:", "Enabled:"),
    (r", limit:", ", Limit:"),
    (r"capacityDeep:", "CapacityDeep:"),
    (r"capacityPlan:", "CapacityPlan:"),
    (r"interpret:", "Interpret:"),
]


def flatten_status_features(text: str) -> str:
    """Promote Status: statusOptions{ Events: ..., features: featureFlags{...} } to flat fields."""
    pattern = re.compile(
        r"Status: statusOptions\{\s*"
        r"(Events: EventOption\{[^}]*\},?\s*)?"
        r"features: featureFlags\{([^}]*)\}\s*,?\s*"
        r"\}",
        re.MULTILINE,
    )

    def repl(m):
        events = m.group(1) or ""
        inner = m.group(2).strip().rstrip(",")
        fields = []
        if events:
            fields.append(events.rstrip())
        for part in inner.split(","):
            part = part.strip()
            if not part:
                continue
            name, val = part.split(":", 1)
            fields.append(f"{name.strip().capitalize()}: {val.strip()}")
        return ", ".join(fields)

    return pattern.sub(repl, text)


def migrate_file(path: pathlib.Path) -> bool:
    text = path.read_text()
    orig = text
    for pat, repl in replacements + literal_replacements:
        text = re.sub(pat, repl, text)
    text = flatten_status_features(text)
    if text != orig:
        path.write_text(text)
        return True
    return False


def main() -> int:
    updated = 0
    for path in sorted(root.glob("*.go")):
        if migrate_file(path):
            print("updated", path.relative_to(path.parent.parent))
            updated += 1
    print(f"done: {updated} files updated")
    return 0


if __name__ == "__main__":
    sys.exit(main())