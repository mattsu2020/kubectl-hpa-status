# Social Promotion Kit

Use these drafts when announcing a release, demo, or call for feedback. Replace bracketed placeholders before posting.

## Release Highlights Template

```text
kubectl-hpa-status [VERSION] is out.

What's new:
- [USER-FACING CHANGE 1]
- [USER-FACING CHANGE 2]
- [BUG FIX OR COMPATIBILITY NOTE]

Try:
kubectl krew install hpa-status
kubectl hpa_status status <hpa> -n <namespace> --explain

Demo: https://github.com/mattsu2020/kubectl-hpa-status
```

## X / Short Post

```text
kubectl-hpa-status helps explain HPA behavior from visible Kubernetes API signals:

- maxReplicas / minReplicas limits
- metrics failures
- scale-down stabilization
- multi-metric estimates
- dry-run-first fix suggestions

Demo and install:
https://github.com/mattsu2020/kubectl-hpa-status
```

## Reddit / Slack / Connpass

```text
I built kubectl-hpa-status, a kubectl plugin for investigating HorizontalPodAutoscaler behavior.

It focuses on operational questions:
- Why is this HPA not scaling?
- Is it capped by minReplicas/maxReplicas?
- Are metrics unavailable or stale?
- Is scale-down stabilization active?
- What is the safest next command to run?

Install:
kubectl krew install hpa-status

Example:
kubectl hpa_status status <hpa> -n <namespace> --explain --suggest

Feedback is especially useful for KEDA/custom metrics, multi-metric HPAs, and large-cluster workflows.
```

## Zenn / Blog Outline

```text
Title: kubectl-hpa-status で HPA の「なぜスケールしないのか」を読む

Sections:
1. kubectl describe hpa だけではつらい場面
2. status --explain で見えること
3. metrics unavailable / maxReplicas / stabilization の読み方
4. --suggest と dry-run-first な修正フロー
5. KEDA や multi-metric HPA での制約
6. インストール方法とフィードバック募集
```
