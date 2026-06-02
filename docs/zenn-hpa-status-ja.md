---
title: "kubectl describe hpa だけではつらいHPA調査を kubectl-hpa-status で短くする"
emoji: "📈"
type: "tech"
topics: ["kubernetes", "kubectl", "hpa", "autoscaling", "sre"]
published: false
---

この記事は、HorizontalPodAutoscaler (HPA) の調査で `kubectl describe hpa`
を読み解く時間を減らすための kubectl plugin、`kubectl-hpa-status` の紹介です。

## 何が分かるか

`kubectl-hpa-status` は、既存の Kubernetes API に見えている HPA status、
conditions、currentMetrics、events、behavior 設定を読み、次の疑問に答えます。

- HPA は健康か、上限に張り付いているか、メトリクス取得に失敗しているか。
- 複数メトリクスのうち、どれが最も強く影響していそうか。
- 次にどのコマンドで確認し、どの修正案を dry-run すべきか。

内部の HPA controller decision trace を知っているわけではありません。
見えている status からの推論には confidence を付け、断定できない部分は
明示的に best-effort として表示します。

## describe hpa との違い

`kubectl describe hpa` は raw data を確認するには便利です。一方、障害対応中は
Conditions、Events、current/desired/maxReplicas、metrics、behavior を
手で突き合わせる必要があります。

`kubectl-hpa-status` は次のように、運用判断に近い形へまとめます。

```sh
kubectl hpa status web -n production --explain
kubectl hpa status web -n production --suggest
kubectl hpa status list -A --problem --sort-by problem
```

比較画像はリポジトリの
`images/describe-vs-hpa-status.svg` を参照してください。

## よくある調査パターン

### HPA がスケールしない

```sh
kubectl hpa status web -n production --explain --events=10
```

`ScalingActive=False`、`FailedGetResourceMetric`、`FailedGetExternalMetric`、
`observedGeneration` の遅れなどを確認します。

### Metrics unavailable

```sh
kubectl hpa status web -n production --diagnose-metrics
```

CPU/Memory の場合は metrics-server と `kubectl top pods` を確認します。
Custom/External metrics の場合は adapter の `APIService`、adapter logs、
metric selector の意味を確認します。

### ScaleDownStabilized で下がらない

```sh
kubectl hpa status web -n production --explain
kubectl hpa status web -n production --watch --dashboard
```

`spec.behavior.scaleDown.stabilizationWindowSeconds` と、見えている condition
reason から、待つべき状態なのか、設定を見直すべき状態なのかを判断します。

### クラスタ全体の棚卸し

```sh
kubectl hpa status list -A --problem --wide
kubectl hpa status scan
kubectl hpa status list -A --report markdown
```

health score が低い HPA、`ScalingLimited`、メトリクス取得失敗を優先して確認できます。
Markdown/HTML レポートはオンコール引き継ぎや定例レビューにも使えます。

## 安全な修正フロー

`--suggest` と `--fix --apply` は dry-run を優先します。

```sh
kubectl hpa status web -n production --suggest
kubectl hpa status web -n production --fix --apply
kubectl hpa status web -n production --fix --apply --dry-run=false
```

永続反映には `--dry-run=false` が必要です。maxReplicas を上げる提案では、
容量、quota、コスト、下流依存を確認するための警告も表示します。

## インストール

Krew、Homebrew、手動ビルドに対応しています。

```sh
brew install mattsu2020/kubectl-hpa-status/kubectl-hpa-status
kubectl-hpa-status list -A --wide
```

Krew 経由では kubectl の plugin discovery により呼び出し形式が変わることがあります。
インストール後は次で確認してください。

```sh
kubectl plugin list
```

## まとめ

HPA 調査では、raw data を読むだけでなく、どの情報を根拠に次の一手へ進むかが重要です。
`kubectl-hpa-status` は `kubectl describe hpa` を置き換えるというより、
describe で見える情報を運用判断に近い形へ整理するためのツールです。

詳しくは README と日本語 README を参照してください。

- `README.md`
- `README.ja.md`
