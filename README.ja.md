# kubectl-hpa-status

[![CI](https://github.com/mattsu2020/kubectl-hpa-status/actions/workflows/ci.yml/badge.svg)](https://github.com/mattsu2020/kubectl-hpa-status/actions/workflows/ci.yml)
[![CodeQL](https://github.com/mattsu2020/kubectl-hpa-status/actions/workflows/codeql.yml/badge.svg)](https://github.com/mattsu2020/kubectl-hpa-status/actions/workflows/codeql.yml)
[![Release](https://github.com/mattsu2020/kubectl-hpa-status/actions/workflows/release.yml/badge.svg)](https://github.com/mattsu2020/kubectl-hpa-status/actions/workflows/release.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/mattsu2020/kubectl-hpa-status.svg)](https://pkg.go.dev/github.com/mattsu2020/kubectl-hpa-status)
[![Go Report Card](https://goreportcard.com/badge/github.com/mattsu2020/kubectl-hpa-status)](https://goreportcard.com/report/github.com/mattsu2020/kubectl-hpa-status)
[![Stars](https://img.shields.io/github/stars/mattsu2020/kubectl-hpa-status?style=social)](https://github.com/mattsu2020/kubectl-hpa-status/stargazers)
[![Release](https://img.shields.io/github/v/release/mattsu2020/kubectl-hpa-status)](https://github.com/mattsu2020/kubectl-hpa-status/releases)
[![GoReleaser](https://img.shields.io/badge/release-GoReleaser-00add8)](https://goreleaser.com/)
[![golangci-lint](https://img.shields.io/badge/lint-golangci--lint-blue)](https://golangci-lint.run/)
[![Krew](https://img.shields.io/badge/krew-hpa--status-blue)](https://krew.sigs.k8s.io/plugins/)
[![Kubernetes](https://img.shields.io/badge/kubernetes-autoscaling%2Fv2-326ce5)](https://kubernetes.io/docs/tasks/run-application/horizontal-pod-autoscale/)
[![Codecov](https://codecov.io/gh/mattsu2020/kubectl-hpa-status/branch/main/graph/badge.svg)](https://codecov.io/gh/mattsu2020/kubectl-hpa-status)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)

![kubectl-hpa-status demo](images/demo.png)

既存の Kubernetes API シグナルを活用し、詳細なスケーリング分析とともに HorizontalPodAutoscaler (HPA) の状態を調査するための kubectl プラグインです。

English README: [README.md](README.md)

ドキュメント同期メモ: リリース時の一次情報は `README.md` です。ユーザー向けフラグ、インストール手順、例を変更した場合は `README.ja.md` も同期してください。

このツールは、HPA運用でよくある3つの疑問にすばやく答えます。

- このHPAは正常か、上限に張り付いているか、安定化中か、メトリクス取得に失敗しているか。
- どのConditionやメトリクスが現在の挙動を説明しているか。
- 次に実行すべきコマンドは何か、安全にdry-run検証できるか。

## Before / After

<table>
<tr>
<th>Before: 生の <code>kubectl describe hpa</code></th>
<th>After: <code>kubectl hpa status --explain</code></th>
</tr>
<tr>
<td>
<pre><code>Name: web
Namespace: production
Metrics: cpu: 92% / 60%
Min replicas: 2
Max replicas: 10
Deployment pods: 10 current / 10 desired
Conditions:
  AbleToScale=True
  ScalingActive=True
  ScalingLimited=True
Events:
  SuccessfulRescale New size: 10</code></pre>
</td>
<td>
<pre><code>web production
Summary: maxReplicasの上限に到達
Replicas: 10 current / 10 desired
CPU: 92% / 60% target

Interpretation:
- HPAはさらに増やしたいが、maxReplicas=10で制限されています。
- ScalingActive=Trueのため、メトリクスは取得できています。

Recommended actions:
- 容量を確認し、--suggestでmaxReplicas引き上げ案をdry-runします。</code></pre>
</td>
</tr>
</table>

リポジトリ名とバイナリ名は `kubectl-hpa-status` です。`kubehpa_cli` は初期開発時の作業ディレクトリ名/愛称であり、リリース成果物、Go module path、インストールコマンドでは使いません。

## デモ

- スクリーンショット: [images/demo.png](images/demo.png)
- 比較画像: [images/describe-vs-hpa-status.svg](images/describe-vs-hpa-status.svg)
- status explainデモ: [docs/status-explain.cast](docs/status-explain.cast)
- wide listデモ: [docs/list-wide.cast](docs/list-wide.cast)
- watchデモ: [docs/watch.cast](docs/watch.cast)
- `--explain` から `--suggest`、`--fix --apply` までの流れ: [docs/fix-flow.cast](docs/fix-flow.cast)
- Zenn記事ドラフト: [docs/zenn-hpa-status-ja.md](docs/zenn-hpa-status-ja.md)

![kubectl describe hpa と kubectl-hpa-status の比較](images/describe-vs-hpa-status.svg)

| ワークフロー | 画像 |
| --- | --- |
| `status --explain` | [status-explain.svg](images/status-explain.svg) |
| `list -A --wide --problem` | [list-wide.svg](images/list-wide.svg) |
| `watch --interval 5s` | [watch-mode.svg](images/watch-mode.svg) |
| `--suggest` dry-runコマンド | [suggest-dry-run.svg](images/suggest-dry-run.svg) |
| `--fix --apply` 差分確認 | [apply-diff.svg](images/apply-diff.svg) |
| 日本語ラベル | [ja-output.svg](images/ja-output.svg) |
| `scan` クラスタ診断 | [scan-output.svg](images/scan-output.svg) |
| JSON出力 | [json-output.svg](images/json-output.svg) |
| メトリクス取得失敗 | [metrics-failure.svg](images/metrics-failure.svg) |
| スケールダウン安定化 | [stabilized-output.svg](images/stabilized-output.svg) |
| 複数メトリクス推定 | [multi-metric-output.svg](images/multi-metric-output.svg) |

Social preview画像の元ファイル: [images/social-preview.svg](images/social-preview.svg)

### なぜ `kubectl-hpa-status` を使うべきなのか？

| 機能 | `kubectl describe hpa` | `kubectl hpa status` (本プラグイン) |
| --- | --- | --- |
| **焦点** | 生のステータスとスペックのダンプ | 多角的な診断と推奨アクションの提示 |
| **スケーリング要約** | 標準的なK8sのConditionテキスト | 明確な運用方針の要約表示 |
| **制限の検出** | 生の最小/最大レプリカ数の表示 | `maxReplicas` に達した際の上限キャップの自動説明 |
| **複数メトリクス診断** | 各ターゲットを個別に列挙 | 最も影響の大きいメトリクスを推測してハイライト |
| **安定化ウィンドウの警告** | 明示的には追跡されない | アクティブなスケールダウン安定化時間を検知し待機時間を推奨 |
| **Watchモード** | 外部の `watch` コマンドが必要（差分表示なし） | 前回の状態との差分をハイライトする組込Watch |
| **推奨ガイド** | なし | *なぜ* その状態なのかを説明し、設定の修正案を提案 |

### オペレーター向けワークフロー比較

| タスク | `kubectl describe hpa` の場合 | `kubectl hpa status` の場合 | 短縮できる時間 |
| --- | --- | --- | --- |
| 1つのHPAがスケールしない理由を探す | Conditions、Events、メトリクス、レプリカ欄を手で読む | `status <name> --explain` が原因候補、根拠、次の確認をまとめる | 障害対応中の数分 |
| クラスタ全体の上限到達を探す | namespaceごとにdescribe/listしてdesired/current/maxを比較 | `list -A --problem --sort-by problem` や `scan` が問題HPAを優先表示 | namespace単位の手作業を削減 |
| Metrics unavailableを診断する | Eventsからresource/custom/externalのどれかを推測 | `--diagnose-metrics` がメトリクス別の確認点を出す | 初動調査を短縮 |
| スケールダウン遅延を説明する | condition reason、behavior、時刻を手で突き合わせる | text/TUIで安定化状態と残り待機目安を表示 | 不要な設定変更を避けやすい |
| 引き継ぎレポートを作る | describe出力を貼り、手で注釈を書く | `--report markdown` / `--report html` で構造化レポートを生成 | 定例・監査・障害報告の手間を削減 |
| 安全に修正案を検証する | patchコマンドとdry-runを自分で組み立てる | `--suggest` / `--fix --apply` がdry-run優先のコマンドと警告を出す | patchミスを減らす |

## クイックスタート

```sh
kubectl hpa status <hpa-name> -n <namespace>
kubectl hpa status doctor <hpa-name> -n <namespace>
kubectl hpa status timeline <hpa-name> --since=30m
kubectl hpa status <hpa-name> --explain
kubectl hpa status <hpa-name> --suggest
kubectl hpa status <hpa-name> --fix --apply
kubectl hpa status <hpa-name> --fix --apply --dry-run=false
kubectl hpa status <hpa-name> --lang=ja
kubectl hpa status <hpa-name> --debug
kubectl hpa status hpa-a hpa-b -n production
kubectl hpa status scan
kubectl hpa status list -A --problem
kubectl hpa status list -A --wide --sort-by=desired --filter=scaling-limited
kubectl hpa status list -A --selector='app=web,tier!=canary'
kubectl hpa status ls -A -o json
kubectl hpa status scan --apply --yes
kubectl hpa status <hpa-name> --watch --timeout=2m --until-condition=scaling-limited
kubectl hpa status <hpa-name> -o 'jsonpath={.analysis.summary}'
```

### doctor コマンド

HPAがスケールしないとき、HPAオブジェクト単体ではなく周辺要因までまとめて見る初動コマンドです。

```sh
kubectl hpa status doctor <hpa-name> -n <namespace>
```

`doctor` は `--explain`, `--diagnose-metrics`, `--metrics-freshness`, `--check-resources`, `--explain-pods`, `--capacity-context`, 最近のEvents、KEDA参照を束ねます。

| 観点 | 見るもの | 出力例 |
| --- | --- | --- |
| Metrics | metrics-server、custom metrics、external metrics | `External metric http_requests is unavailable` |
| Target workload | Deployment、StatefulSet、ReplicaSet | `Pods are Pending; HPA wants 8 replicas but only 3 Ready` |
| Pod状態 | Pending、CrashLoopBackOff、NotReady | `Scale-out blocked by image pull error` |
| Resource requests | CPU/memory request不足 | `CPU utilization target cannot work because container has no cpu request` |
| Events | HPA、Pod、Deployment events | `FailedGetResourceMetric seen 5 times in 10m` |
| KEDA | ScaledObject、trigger health | `KEDA trigger inactive or auth error` |

出力の読み方:

- `Summary` (要約) は、HPAステータスから導出された視覚的な状態です。
- `Recommended actions` (推奨アクション) は、ConditionやBehavior設定に基づいた運用上のヒントです。
- `Interpretation` (解釈) は診断上の推論であり、コントローラーの非公開な決定履歴そのものではありません。
- `confidence: high` (確信度: 高) は明示的なステータスフィールドに基づいていることを示し、`confidence: medium` (確信度: 中) はステータスと説明が一致しているものの、API自体が内部の詳細な理由を開示していないことを示します。
- 複数メトリクス時の「勝者」は推定として表示します。現行のHPA statusはメトリクスごとの推奨レプリカ数や最終選択を公開しないため、targetからの距離が最も大きい可視メトリクスを強調します。

よく見るべきシグナル:

- `ScalingActive=False`: metrics-server、custom metrics adapter、external metrics adapterを確認します。
- `ScalingLimited=True`: `minReplicas`、`maxReplicas`、target utilizationを確認します。
- `ScaleDownStabilized`: `spec.behavior.scaleDown.stabilizationWindowSeconds` と安定化ウィンドウを確認します。
- 出力が古い場合: `status.observedGeneration` と `metadata.generation` を比較します。

インストール直後のhelp出力例:

```text
Inspect HorizontalPodAutoscaler status

Usage:
  kubectl-hpa-status [flags]
  kubectl-hpa-status [command]

Available Commands:
  analyze     Analyze one HPA using visible Kubernetes API signals
  completion  Generate shell completion
  doctor      Diagnose HPA scaling failures across metrics, workload, pods, resources, events, and KEDA
  list        List HPAs and highlight visible issues
  scan        Scan all namespaces for HPAs with visible problems
  status      Show concise status for one HPA
  timeline    Show HPA scaling decisions over time
  watch       Watch one HPA status

Common flags include -n/--namespace, -A/--all-namespaces, -o/--output,
--events, --explain, --watch, --interval, --timeout, --since, and --until-condition.
```

## インストール

### Krew (推奨)

```sh
# 公式krew-indexからインストール
kubectl krew install hpa-status
```

```sh
kubectl hpa status <hpa-name> -n <namespace>
kubectl hpa status list -A --wide
kubectl hpa status <hpa-name> --suggest
```

未リリースのマニフェスト変更をローカルで検証する場合は、`.krew.yaml` から直接インストールしてください。

Krewではプラグイン名は `hpa-status` として入ります。kubectlはハイフンを含む
プラグインを `kubectl hpa_status` として検出できます。
**重要: Krewで入れた場合は通常 `kubectl hpa status <name>` ではなく
`kubectl hpa_status status <name>` を使います。** このREADMEでは、kubectlのnested plugin discoveryが対応している環境向けに
`kubectl hpa status` を推奨形として書いています。動かない場合は
`kubectl hpa_status status <hpa-name>` または
`kubectl-hpa-status status <hpa-name>` を使ってください。

### Homebrew

```sh
brew install mattsu2020/kubectl-hpa-status/kubectl-hpa-status
kubectl-hpa-status list -A --wide
```

Homebrew Caskは `.goreleaser.yml` の `homebrew_casks` で専用Tapへの更新を自動化しています。リリース前には `make release-check` でGoReleaser設定を検証します。

### 手動インストール

```sh
go mod tidy
go build -o kubectl-hpa-status .
chmod +x ./kubectl-hpa-status
sudo mv ./kubectl-hpa-status /usr/local/bin/
kubectl hpa status <hpa-name> -n <namespace>
```

読み取り専用RBACと、`--apply --dry-run=false` 用のpatch権限例は
[docs/rbac.yaml](docs/rbac.yaml) を参照してください。

最小の読み取り権限は次の通りです:

- `autoscaling/v2` `horizontalpodautoscalers` の `get`, `list`, `watch`
- core `events` の `list`, `watch`
- not-ready replicaの補足表示に使う `deployments`, `statefulsets`, `replicasets` の `get`（任意）
- `--keda` 使用時の KEDA `scaledobjects` の `get`, `list`（任意）

通常の診断に書き込み権限は不要です。HPAの `patch` は明示的に
`--apply --dry-run=false` を使う運用だけに付与してください。

Go module path、GitHubリポジトリ、リリースメタデータ、ユーザー向けバイナリ名は
すべて `github.com/mattsu2020/kubectl-hpa-status` / `kubectl-hpa-status`
に統一されています。

### 要件

- **Kubernetes 1.26+** （autoscaling/v2 は1.23でGA、1.26で安定API）
- kubectl が kubeconfig で設定済み
- metrics-server（CPU/メモリメトリクス用）またはカスタム/外部メトリクスアダプター

## サンプル

実用的なサンプルマニフェストは [examples/](examples/) にあります。

| 例 | 内容 |
| --- | --- |
| [cpu-memory-hpa.yaml](examples/cpu-memory-hpa.yaml) | CPU + Memoryの複数メトリクスHPA |
| [behavior-hpa.yaml](examples/behavior-hpa.yaml) | scaleUp/scaleDownポリシーとstabilization window |
| [custom-metrics-hpa.yaml](examples/custom-metrics-hpa.yaml) | custom metrics adapter向けのObject metric例 |
| [keda-style-hpa.yaml](examples/keda-style-hpa.yaml) | KEDA風ラベルとExternal metricを持つHPA |

```sh
kubectl apply -f examples/cpu-memory-hpa.yaml
kubectl hpa status web-multi -n hpa-status-examples --explain --suggest
kubectl hpa status list -n hpa-status-examples --wide
kubectl delete namespace hpa-status-examples
```

## 使い方

```sh
kubectl hpa status <hpa-name> [<hpa-name>...] [-n namespace] [--context context] [--events=false]
kubectl hpa status doctor <hpa-name> -n <namespace>
kubectl hpa status timeline <hpa-name> --since=30m
kubectl hpa status <hpa-name> --watch --interval 5s
kubectl hpa status <hpa-name> --watch --dashboard
kubectl hpa status <hpa-name> --watch --timeout 2m --until-condition scaling-limited
kubectl hpa status analyze <hpa-name> [<hpa-name>...]  # 非推奨; 代わりに status --explain を使用してください
kubectl hpa status list [-A] [--selector app=web] [--sort-by health-score] [--min-score 60] [--filter scaling-limited]
kubectl hpa status list -A --problem
kubectl hpa status scan --selector app=web
kubectl hpa status ls [-A] --wide
kubectl hpa status watch <hpa-name> --interval 5s
kubectl hpa-status __complete status ""
```

直接バイナリとしても実行できます。

```sh
kubectl-hpa-status analyze <hpa-name> -n <namespace>
kubectl-hpa-status status <hpa-name> -n <namespace>
kubectl-hpa-status doctor <hpa-name> -n <namespace>
kubectl-hpa-status timeline <hpa-name> --since=30m
kubectl-hpa-status status <hpa-name> --suggest
kubectl-hpa-status status <hpa-name> --fix --apply
kubectl-hpa-status status <hpa-name> --fix --apply --dry-run=false
kubectl-hpa-status scan
kubectl-hpa-status list -A
kubectl-hpa-status completion zsh
kubectl-hpa-status completion powershell
# メトリクス経路の診断
kubectl-hpa-status status <hpa-name> --diagnose-metrics
# Pod resource requestとHPA targetの整合性確認
kubectl-hpa-status status <hpa-name> --check-resources
# Markdownレポート生成
kubectl-hpa-status status <hpa-name> --report markdown
```

詳細フラグ:

| フラグ | 対象 | 説明 |
| --- | --- | --- |
| `-n, --namespace` | 全コマンド | `-A` 未指定時に読むnamespace。kubeconfigのnamespace、なければ `default`。 |
| `-A, --all-namespaces` | `list`, `scan`, 補完 | 全namespaceのHPAを一覧。 |
| `-l, --selector` | `list`, `scan` | HPA一覧APIに渡すラベルセレクタ。例: `app=web,tier!=canary`。 |
| `--context`, `--kubeconfig`, `--cluster` | 全コマンド | kubeconfig選択。 |
| `--config <file>` | 全コマンド | YAML/JSON設定ファイルを読み込み。省略時は存在すれば `~/.kube/hpa-status.yaml`。 |
| `--chunk-size` | `list`, `scan`, `tui` | Kubernetes list APIのページサイズ。デフォルトは500。0でページング無効。 |
| `--health-weight name=value` | 分析系コマンド全般 | health scoreのペナルティをCLIから上書き。繰り返し指定可。名前は `scalingInactive`, `unableToScale`, `scalingLimited`, `implicitMaxReplicas`, `scaleDownStabilized`, `atMinimumReplicas`。 |
| `-o table|wide|json|yaml|jsonpath=...|template=...` | status, doctor, analyze, list, scan | 出力形式。単一/複数HPAのYAML出力にも対応。 |
| `--wide` | table出力 | target、min、max、desired-current差分など追加列を表示。 |
| `--sort-by namespace|name|current|desired|diff|health-score|issue|problem` | `list`, `scan` | list出力のソート。`problem` は低スコアとレプリカ差分を優先。 |
| `--filter all|ok|error|limited|scaling-limited|issue` | `list`, `scan` | healthまたはissue文字列で絞り込み。 |
| `--health-score`, `--max-score` | `list`, `scan` | health scoreが指定値以下のHPAだけ表示。 |
| `--min-score` | `list`, `scan` | health scoreが指定値以上のHPAだけ表示。 |
| `--problem` | `list`, `scan` | 問題が見えるHPAだけ表示。 |
| `--color auto|always|never` | text出力 | 端末カラー制御。 |
| `--interpret` | `status` | compact statusに診断解釈を含める。 |
| `--explain` | `status`, `doctor`, `analyze` | 詳細な解釈と推奨アクションを含める。`doctor` ではデフォルトで有効。 |
| `--suggest`, `--recommend` | `status`, `doctor`, `analyze` | 安全に見える修正案を `kubectl patch` として表示。`--recommend` は `--suggest` のエイリアスです。 |
| `--fix` | `status`, `doctor`, `analyze` | より強い修正計画と適用可能なpatchを表示。 |
| `--diff` | `status`, `doctor`, `analyze` | 提案されたHPA spec patchのフィールド単位差分を表示。 |
| `--apply` | `status`, `doctor`, `analyze`, `list`, `scan` | デフォルトでserver-side dry-runとしてHPA patchを検証。`list` では `--problem`, `--filter`, score filter のいずれかと組み合わせます。 |
| `--dry-run=false` | `--apply` フロー | 永続変更。`-y` なしでは差分表示後に確認プロンプトあり。 |
| `--keda` | `status`, `doctor`, `analyze` | KEDA管理HPAで対応するScaledObjectを参照し、trigger/condition文脈を追加。CRDがある場合はデフォルトで有効。 |
| `--debug`, `-v` | `status`, `doctor`, `analyze`, `list` | metric ratio、health score、condition根拠など内部計算を表示。 |
| `--lang=ja`, `-o ja` | text出力 | 日本語ラベルで表示。 |
| `--no-interpret` | `status`, `doctor`, `analyze` | 解釈を省き、ステータス由来のデータのみ表示。 |
| `--events=false` | `status`, `doctor`, `analyze` | 最近のEventsを省略。 |
| `--events=3` | `status`, `doctor`, `analyze` | 最新3件のHPA Eventsを表示。 |
| `--diagnose-metrics` | `status`, `doctor`, `analyze` | メトリクス種別ごとの取得状況、adapter/APIService確認のヒント、次の確認手順を表示。`doctor` ではデフォルトで有効。 |
| `--check-resources` | `status`, `doctor`, `analyze` | HPA対象Podのresource request/limitとtarget utilizationの整合性を確認。`doctor` ではデフォルトで有効。 |
| `--report markdown\|html` | `status`, `doctor`, `list` | 単体またはクラスタ全体の診断レポートをMarkdown/HTMLで生成。 |
| `--watch --interval 5s` | `status`, `watch` | 単一HPAを定期更新。watchはHPA名1つだけ対応。 |
| `--dashboard` | `watch` | コンパクトなターミナルダッシュボード形式で表示。 |
| `--timeout 2m` | watchモード | 指定時間でwatch停止。 |
| `--until-condition scaling-limited` | watchモード | 正規化したCondition typeが出たらwatch停止。 |
| `--since=30m` | `timeline` | 過去のEventからスケーリングタイムラインを再構成。`30m`、`1h` など指定可能。 |
| `--version` | root | バージョンを表示。 |

### ヘルススコア

各HPAには0〜100のヘルススコアが付与されます。スコアは100から始まり、検出された問題に応じてペナルティが差し引かれます:

| 控除項目 | スコアへの影響 |
|-----------|-------------|
| メトリクス取得不可 (`ScalingActive=False`) | -45 |
| スケール不可能 (`AbleToScale!=True`) | -35 |
| min/maxReplicasによるスケーリング制限 | -25 |
| 暗黙的にmaxReplicasに到達 | -20 |
| スケールダウン安定化ウィンドウがアクティブ | -10 |
| minReplicasで稼働中 | -5 |

**ヘルス状態**: `OK` → `STABILIZED` → `LIMITED` → `ERROR` (悪化順)。`--health-score`、`--min-score`、`--max-score` でスコア範囲によるフィルタリングが可能です。

シェル補完:

- `kubectl-hpa-status completion bash|zsh|fish|powershell` で補完スクリプトを生成できます。
- Cobra標準の `__complete` に対応しており、`kubectl hpa-status __complete status ""` のような補完問い合わせでHPA名候補を返します。
- `status` / `doctor` / `analyze` / `watch` のHPA名補完は現在のnamespaceを使います。`-A` 指定時は `namespace/name` 形式で候補を返します。

設定ファイル:

`--config` を省略した場合、存在すれば `~/.kube/hpa-status.yaml` を読み込みます。設定値はフラグのデフォルトとしてだけ使われ、明示したCLIフラグが常に優先されます。

設定ファイル `~/.kube/hpa-status.yaml` がサポートされており、すべてのフラグのデフォルト値を設定できます。上記の設定ファイル例を参照してください。

```yaml
namespace: production
lang: ja
color: auto
events: 5
selector: app.kubernetes.io/part-of=my-service
sortBy: problem
maxScore: 80
dashboard: false
chunkSize: 500
templates:
  hpa-names:
    type: go-template
    template: '{{ range .Items }}{{ .Namespace }}/{{ .Name }}{{ "\n" }}{{ end }}'
  summaries:
    type: jsonpath
    template: '{.analysis.summary}'
healthWeights:
  scalingInactive: 45
  unableToScale: 35
  scalingLimited: 25
```

対応Kubernetesバージョン:

- **Kubernetes 1.26以降が必要です。** 本プラグインは `autoscaling/v2` を使用しており、同APIはKubernetes 1.23でGA、1.26以降で安定APIとなっています。
- Runtime target: `autoscaling/v2` `HorizontalPodAutoscaler` を提供するクラスタ
- 想定対応範囲: Kubernetes v1.30〜v1.35 の `autoscaling/v2`
- 検証済みクラスタ: Kubernetes v1.35.0 + metrics-server v0.8.1
- Client libraries: `k8s.io/client-go` / `k8s.io/api` v0.35.0

このプラグインが読む情報:

- `autoscaling/v2` `HorizontalPodAutoscaler`
- `status.currentReplicas`
- `status.desiredReplicas`
- `status.currentMetrics`
- `status.conditions`
- `status.observedGeneration` が存在する場合
- `spec.behavior` が存在する場合
- 直近のHPA Events

HPA controllerの内部意思決定ロジックを再実装するものではありません。

### JSONPath / テンプレート出力例

```sh
# HPA名とヘルススコアを一覧
kubectl hpa status list -A -o jsonpath='{range .items[*]}{.namespace}/{.name} {.healthScore}{"\n"}{end}'

# ヘルススコア80未満のHPAのみ抽出
kubectl hpa status list -A -o jsonpath='{range .items[?(@.healthScore<80)]}{.namespace}/{.name} {.health}{"\n"}{end}'

# KEDA ScaledObject名を取得
kubectl hpa status <hpa> -o jsonpath='{.analysis.keda.scaledObjectName}'

# VPA競合警告を取得
kubectl hpa status <hpa> --vpa -o jsonpath='{.analysis.vpaConflict.warning}'

# 構造化解釈エントリを出力
kubectl hpa status <hpa> -o jsonpath='{range .analysis.structuredInterpretation[*]}{.severity} {.text}{"\n"}{end}'

# 自動化用にJSONで出力
kubectl hpa status list -A -o json | jq '.items[] | {name, namespace, healthScore, issue}'
```

完全なJSONスキーマは [docs/output-schema.json](docs/output-schema.json) を参照してください。

## マルチメトリクスディシジョンディープトレース

HPAが複数のメトリクス（CPU + メモリ + カスタムなど）を持つ場合、最終的なスケーリング決定をどのメトリクスが主導したかを判別するのは困難です。**メトリクスディシジョントレース**は、メトリクスごとの内訳を提供します：

- 各メトリクスのターゲットに対する現在の比率
- メトリクスがトレランスバンド（デフォルト10%）内にあるかどうか
- 各メトリクスの推定レプリカへの影響
- どのメトリクスが「勝者」である可能性が高いか、およびその信頼度
- 安定化ウィンドウとトレランスがスケーリング決定に与える影響

```sh
kubectl hpa status <hpa-name> --explain --debug
```

トレース出力には以下が含まれます：

- **メトリクスごとのエントリ**：比率、ターゲットからの距離、レプリカ影響推定、希望方向（up/down/none）
- **勝者の検出**と信頼度（maxReplicasに達していない場合は中、maxReplicasに達している場合は低（勝者を確実に特定できないため））
- **安定化の影響**：スケールダウンが抑制されているかどうか、および推定残り待機時間
- **トレランスの影響**：トレランスバンドによって抑制されているメトリクスのリスト
- **選択ポリシー**：動作仕様で`Max`、`Min`、または`Least`のいずれが設定されているか

これは、可視的な `currentMetrics` と `spec.metrics` に基づくベストエフォート推定です。Kubernetes HPA APIは、メトリクスごとのレプリカ推奨値やコントローラーの最終メトリクス選択を公開していません。

## What-If スケーリングシミュレーター

`--simulate-metric` フラグを使用すると、メトリクス値が変化した場合にHPAがどのように動作するかを、クラスタの状態を変更せずにプレビューできます。

```sh
# CPU使用率80%をシミュレート
kubectl hpa status web --simulate-metric cpu=80%

# メモリ4Giをシミュレート
kubectl hpa status web --simulate-metric memory=4Gi

# http_requestsが20%増加した場合をシミュレート
kubectl hpa status web --simulate-metric http_requests=+20%

# 複数メトリクスのシミュレーションを組み合わせる
kubectl hpa status web --simulate-metric cpu=80% --simulate-metric memory=4Gi
```

シミュレーターは分析の現在のメトリクス値を上書きし、以下を表示します：

- ヘルススコアがどのように変化するか
- 新しい推定希望レプリカ数
- シミュレーション値に基づく更新された解釈と推奨事項

すべてのシミュレーションはクライアント側のみで実行されます。Kubernetes APIサーバーに変更は送信されません。

## ベストプラクティス監査

`recommend` サブコマンドは、HPA設定を組み込みのベストプラクティスルールに対して監査し、スコア付きのコンプライアンスレポートを生成します。

```sh
kubectl hpa status recommend <hpa-name>
```

監査は9つのルールを評価します：

| ルール | 深刻度 | 確認内容 |
| --- | --- | --- |
| 安定化ウィンドウ | Warning | スケールダウン安定化ウィンドウが未設定または過度に長い |
| レプリカ範囲 | Critical | `minReplicas` が低すぎる（0を含む）または `maxReplicas` が不必要に高い |
| ビヘイビアポリシー | Warning | scale-up または scale-down ビヘイビアポリシーが未設定 |
| メトリクスカバレッジ | Warning | HPAにメトリクスが定義されていない、または単一メトリクスタイプのみ使用 |
| トレランス | Info | メトリクスがデフォルトのトレランスバンド内（無駄なメトリクスの可能性） |
| ゼロへのスケール | Critical | コールドスタートの考慮なしに `minReplicas=0` が設定されている |
| リソースリクエスト | Warning | HPAが依存するターゲットPodにリソースリクエストが未設定 |
| KEDA設定 | Info | トリガーまたは認証の問題がある可能性のあるKEDA管理HPA |
| ターゲット使用率 | Warning | ターゲット使用率が推奨範囲外（高すぎるまたは低すぎる）に設定されている |

コンプライアンススコアは100から始まり、以下の通り減算されます：

- **Critical**: 各-20
- **Warning**: 各-10
- **Info**: 減算なし

出力例：

```text
HPA default/web
Target: Deployment/web
Compliance Score: 70/100
Summary: Found 1 critical, 2 warnings, 0 informational findings

Audit Findings:
  [CRITICAL] minReplicas is set to 0 without scale-to-zero safeguards
  [WARNING] No scale-down behavior policy configured; defaults may cause rapid scale-down
  [WARNING] Target CPU utilization (95%) is above recommended maximum (85%)
```

## 開発

```sh
make build
make test
make coverage
make lint
make release-check
```

設計・セキュリティ・コントリビューション方針:

- [ARCHITECTURE.md](ARCHITECTURE.md)
- [SECURITY.md](SECURITY.md)
- [CONTRIBUTING.md](CONTRIBUTING.md)

kindを使ったE2Eテスト:

```sh
kind create cluster --name hpa-status-dev
make e2e
kind delete cluster --name hpa-status-dev
```

## インタラクティブ TUI

クラスタ全体のHPAをリアルタイムに監視するインタラクティブダッシュボードを起動:

```sh
kubectl hpa status tui          # 現在のネームスペース
kubectl hpa status tui -A       # 全ネームスペース
kubectl hpa status web --watch --dashboard
```

キーバインド:

| キー | アクション |
| --- | --- |
| `↑` / `k` | カーソルを上に移動 |
| `↓` / `j` | カーソルを下に移動 |
| `Enter` | HPA詳細ビューを開く |
| `Esc` | 戻る / ヘルプを閉じる |
| `/` | 名前・ネームスペース・ヘルス状態・課題でフィルタ |
| `S` | ソート順を切替: name → health-score → issue → namespace |
| `g` | 最初の問題ありHPA（health ≠ OK）にジャンプ |
| `r` | 今すぐデータを更新 |
| `p` | 自動更新の一時停止 / 再開 |
| `m` | メトリクス別診断の詳細ビューを開く |
| `space` | HPAを選択/選択解除 |
| `a` / `A` | 表示中HPAの一括選択 / 全解除 |
| `s` | 選択したHPAに対するCLI一括applyフローの案内 |
| `?` | キーバインドヘルプの表示切替 |
| `q` / `Ctrl+c` | 終了 |

ダッシュボードはデフォルトで5秒ごとに自動更新し、`--interval` で間隔を変更できます。対話端末で `--watch --dashboard` を指定すると、選択したHPAの詳細ビューからTUIを起動します。パイプや録画などの非対話出力では、従来どおりコンパクトなテキストダッシュボードを出力します。フィルタは複数フィールドに対する部分一致を受け付けます。`g` キーで注意が必要な最初のHPAに素早くジャンプできます。`m` でメトリクス別診断を確認し、`space` で複数HPAを選択してからCLIの一括applyワークフローへ進めます。

## トラブルシューティングパターン

| 症状 | コマンド | 主なシグナル | 次の一手 |
| --- | --- | --- | --- |
| スケール失敗の理由が分からない | `kubectl hpa status doctor <name>` | メトリクス診断、対象workload、Pod状態、resource requests、Events、KEDA | 障害初動ではここから確認 |
| いつスケーリングが変わったか知りたい | `kubectl hpa status timeline <name> --since=30m` | rescale events、メトリクス失敗、上限制限、stabilization | チューニング前にインシデントの流れを再構成 |
| メトリクスが取れずスケールしない | `kubectl hpa status doctor <name>` | `ScalingActive=False`, メトリクス診断, Events | metrics-server または custom/external metrics adapter を確認 |
| metrics-serverが遅い/再起動直後 | `kubectl hpa status <name> --explain --events=10` | 古い `currentMetrics`, `FailedGetResourceMetric`, 古い `lastTransitionTime` | scrape間隔分待ち、`kubectl top pods` とmetrics-serverログを確認 |
| レプリカ数が上限に張り付く | `kubectl hpa status <name> --suggest` | `ScalingLimited=True`, `desiredReplicas == maxReplicas` | 容量を確認し、提案されたmaxReplicasパッチをdry-run検証 |
| スケールダウンが遅い | `kubectl hpa status <name> --explain` | `ScaleDownStabilized`, `spec.behavior.scaleDown` | stabilization windowを待つか調整 |
| KEDA/External Metricsが止まる | `kubectl hpa status <name> --keda --explain --suggest` | KEDA label、External metric、ScaledObject condition、`FailedGetExternalMetric` | external metrics API、KEDA ScaledObject、TriggerAuthentication、operatorログ、metric selectorを確認 |
| Object metricの意味が分かりにくい | `kubectl hpa status <name> --explain` | `Object <kind>/<name>`, ratio | 対象Objectの値とPod単位の負荷を分けて確認 |
| HPAはscale upしたいがPodがPending | `kubectl hpa status <name> --explain` | Pending/Unschedulable Pod | node容量、Cluster Autoscaler/Karpenterイベント、quota、affinity、taintを確認 |
| VPAとHPAがCPU/Memoryを同時管理 | `kubectl hpa status <name> --vpa --explain` | VPA updateMode、controlled resources、recommendation | VPAをrecommender用途に寄せるか、CPU/Memoryの所有を片方へ寄せる |
| tolerance付近で増減しない | `kubectl hpa status <name> --explain --debug` | ratioが1.02〜1.10付近、desired=current | sustainedな圧力か確認し、必要ならHPAConfigurableTolerance利用を検討 |
| クラスタ全体を棚卸ししたい | `kubectl hpa status scan` | health score, issue, conditions | `ERROR` から優先して確認 |
| サマリーに `[STALE STATUS]` が表示 | `kubectl hpa status <name> --explain` | `observedGeneration < metadata.generation` | HPA controllerの再調整を待機; kube-controller-managerの健全性を確認 |
| KEDA管理HPAで外部メトリクスが古い | `kubectl hpa status <name> --keda --explain` | `currentMetrics` に外部メトリクス欠落、KEDAトリガー状態 | `kubectl get scaledobject -n <ns>` を確認、keda-operator Podログ、TriggerAuthenticationを確認 |
| minReplicas=0 コールドスタート遅延 | `kubectl hpa status <name> --explain` | `ScaleToZero` 表示、即時スケールアップなし | 仕様通りの動作; scale-to-zero後の最初のメトリック評価にポーリング間隔分の遅延が発生 |
| すべてのメトリクスが `<unknown>` | `kubectl hpa status doctor <name>` | メトリクス別ヘルスチェック、status欠落 | metrics-server、custom metrics adapter登録、APIServiceの健全性を確認 |
| HPA target utilizationが期待と違う | `kubectl hpa status doctor <name>` | resource request警告、request未設定、target mismatch | Pod templateのresource requestsを確認。HPA utilizationはusage/requestで計算される |
| 障害レポートが必要 | `kubectl hpa status <name> --report markdown` | 全セクション入りの単体レポート | Slack、Notion、障害管理ツールへ共有 |
| クラスタ全体の健全性サマリーが必要 | `kubectl hpa status list -A --report markdown` | クラスタ全体レポート、health score、上位issue | オンコール引き継ぎやPlatform定例レビューで共有 |
| 複数HPAを一括修正 | `kubectl hpa status list -A --problem --fix --apply` | 全パッチのサマリーテーブル | バッチサマリーを確認し、一度の確認で全適用 |

### FAQ

**複数メトリクスHPAで、どのメトリクスが勝ったか分かりますか?** 完全には分かりません。現行APIから見える `currentMetrics` と `spec.metrics` で推定しますが、メトリクスごとの推奨レプリカ数、missing metricの保守的補正、最終選択は公開されていません。

**`Metrics unavailable` と表示されたら最初に何を実行すべきですか?** `kubectl hpa status doctor <name>` から始めてください。`doctor` は `--explain`, `--diagnose-metrics`, `--metrics-freshness`, `--check-resources`, `--explain-pods`, `--capacity-context`, Events, KEDA参照を束ねます。CPU/メモリなら `kubectl top pods` とmetrics-serverを確認します。custom/external metricsならadapterの `APIService`、adapterログ、metric selectorの意味を確認します。

**Stabilization windowで止まっているかどうかは分かりますか?** `kubectl hpa status <name> --explain` またはTUI/watchビューを使います。HPA status、behavior policy、直近Eventsから見える範囲で `ScaleDownStabilized` と安定化ウィンドウのタイミングを表示します。

**Krew後に `kubectl hpa status` が動かないのはなぜですか?** Krewはハイフンを含むプラグイン名をunderscore経由で公開します。`kubectl plugin list` で `hpa-status` が見える場合は `kubectl hpa_status status <name>` を使ってください。

**Conditionが正常に見えるのにLIMITEDになるのはなぜですか?** クラスタによって `ScalingLimited` の反映が遅れる/出ない場合があります。`current == desired == maxReplicas` のような明示的なレプリカ状況も見て、軽いimplicit maxReplicasペナルティを適用します。

## 互換性マトリクス

Kubernetes v1.26からv1.36までを検証対象範囲としています。本プラグインは `autoscaling/v2` を使用しており、同APIはKubernetes 1.23でGA、1.26以降で安定APIとなっています。将来のKubernetesでも `autoscaling/v2` が提供される限り動作する想定です。

| 環境 | 状態 |
| --- | --- |
| HPA API `autoscaling/v2` | 必須 |
| Kubernetes v1.26 - v1.36 | 検証済み・サポート対象 |
| kind上のmetrics-server v0.8.1 | 検証済み |
| custom/external metrics adapters | HPA statusに見える範囲で対応。ratioとselector解釈はbest-effortで、adapter内部状態は直接検査しません |
| KEDA 2.0+ (`keda.sh/v1alpha1`) | KEDA管理HPAを自動検出。`--keda` でScaledObjectを参照し、trigger種別、metric名、threshold、current value、auth ref、polling interval、cooldown、fallback設定を表示 |
| VPA 0.9+ (`autoscaling.k8s.io/v1`) | `--vpa` で同一targetのCPU/Memory重複管理を検出し、VPA CRDがあれば見えているrecommendationを表示 |
| Shell Completion | bash、zsh、fish、PowerShellに対応。HPA名、namespace、contextの動的補完を含む |

## 検証済み環境

- kind: v0.31.0
- kind node image: `kindest/node:v1.35.0`
- Kubernetes server: v1.35.0
- kubectl: v1.36.1
- metrics-server: v0.8.1
- HPA API: `autoscaling/v2`

metrics-serverはupstream release manifestにkind向けの
`--kubelet-insecure-tls` オプションを加えて検証しています。

## 安全な修正フロー

`--suggest` / `--fix --apply` は安全側に倒しています。

```text
観測する
  kubectl hpa status <name> --explain --events=5
      |
提案だけ確認する
  kubectl hpa status <name> --suggest
      |
server-side dry-runで検証する
  kubectl hpa status <name> --fix --apply
      |
差分、desiredReplicas、警告を確認する
      |
永続反映する
  kubectl hpa status <name> --fix --apply --dry-run=false
```

1. `--suggest` は `--dry-run=server` 付きの `kubectl patch` を表示します。
2. `--fix --apply` もデフォルトではserver-side dry-runで、適用前に `status.desiredReplicas` と変更対象の差分を表示します。
3. 永続的に変更するには `--dry-run=false` が明示的に必要です。
4. maxReplicas、behavior、tolerance提案には、容量・quota・コスト・feature gate・下流依存の確認を促す警告を出します。
5. External/Object metricsは、adapterや対象Objectの状態確認を優先し、ステータスだけで危険な自動削除patchは出しません。

dry-runモードの違い:

- `--dry-run=server`: Kubernetes API serverにpatchを送り、admissionやdefaulting込みで検証します。ただし永続化しません。
- `--dry-run=client`: kubectlローカル側だけで検証するため、server-side admissionの挙動を見逃す可能性があります。
- `kubectl-hpa-status --apply` はデフォルトでserver-side dry-runです。永続変更には `--dry-run=false` が必要です。

## Limitations

- Kubernetes HPA APIは、controller内部の正確なscaling decision traceを公開していません。
- 複数メトリクス時の「勝者」判定は、見えている `currentMetrics` と `spec.metrics` からのbest-effort推定です。
- tolerance、missing metricsの保守的処理、not-ready pods、stabilizationの内部recommendation historyはHPA statusだけでは完全には見えません。
- Eventsは直近の文脈として有用ですが、永続的な構造化decision logとしては扱いません。

## CI/CD

| Workflow | 目的 |
| --- | --- |
| [ci.yml](.github/workflows/ci.yml) | `go test`、coverage、govulncheck、gosec、golangci-lint、kind E2E |
| [codeql.yml](.github/workflows/codeql.yml) | CodeQL静的解析 |
| [release.yml](.github/workflows/release.yml) | GoReleaserによるバイナリ、SBOM、Homebrew Cask Tap更新、Krew release bot |

CI実行時にcoverageをCodecovへアップロードします。リリース時のHomebrew更新は
専用Tap [mattsu2020/homebrew-kubectl-hpa-status](https://github.com/mattsu2020/homebrew-kubectl-hpa-status)
を使います。
E2Eは Kubernetes 1.26 / 1.28 / 1.30 / 最新追跡kind image のmatrixで実行し、
対応範囲の `autoscaling/v2` 互換性を継続確認します。

## Validation matrix

| ケース | 既存シグナルで説明可能か | 使用するシグナル | 残る曖昧さ |
| --- | --- | --- | --- |
| CPUがtargetを超えScaleUp | だいたい可能 | `currentMetrics`, `desiredReplicas`, Events | 低 |
| CPUがtarget未満でScaleDown | だいたい可能 | `currentMetrics`, `desiredReplicas`, Events | 低 |
| `maxReplicas` に制限 | 可能 | `ScalingLimited`, `maxReplicas` | 低 |
| メトリクス取得失敗 | 可能 | `ScalingActive=False`, Events | 低 |
| 複数メトリクスの最終勝者 | 一部難しい | `currentMetrics`, `spec.metrics` | 中 |
| ScaleDown stabilization | 一部可能 | `AbleToScale`, condition reason, Events | 中 |
| toleranceによるno-scale | 難しい | `currentMetrics`, `desiredReplicas` | 中から高 |
| missing metrics / not-ready podsの影響 | 難しい | 現状のstatusでは不足 | 高 |

Eventsは直近の診断コンテキストとして有用ですが、このPOCでは安定した意思決定記録としては扱いません。

### 過去スケーリングタイムライン

HPA decision timelineとして、`timeline --since` で過去のスケーリング判断を推定表示:

```bash
kubectl hpa status timeline web -n production --since=30m
```

出力:

```text
HPA Scaling Timeline: web (production)  since 30m ago

21:05:00 CPU 92% > target 60%     desired 3 -> 5
21:06:00 ScalingLimited=True      capped by maxReplicas=5
21:10:00 FailedGetResourceMetric  metrics unavailable
21:15:00 ScaleDownStabilized      scale-down suppressed, ~180s remaining

Note: Best-effort reconstruction from Kubernetes events and current HPA status.
```

制限事項:
- HPAコントローラの内部判断履歴はKubernetes APIから完全には見えない
- 複数メトリクスの勝者判定は推定
- 判断時点の正確なメトリクス値は利用不可
- イベントを生成しなかった抑制済みの判断は表示されない場合がある
- Kubernetes Eventsは通常〜1時間で期限切れとなるため、それ以上の `--since` は結果が空になる可能性がある

全出力フォーマット対応: `--since=30m -o json`, `--since=30m -o yaml`, `--since=30m --report markdown`, `--since=30m --report html`。

## 出力例

List view:

```text
NAMESPACE            NAME                             CURRENT  DESIRED  HEALTH              SCORE    ISSUE                            SUMMARY
default              web                              3        5        🟢 Healthy          100                                       HPA currently wants to scale up.
default              api                              2        2        🔴 ERROR            55       ERROR: FailedGetResourceMetric   HPA cannot currently compute a scaling recommendation from metrics.
```

複数メトリクスHPA:

```text
HPA default/web-multi
Target: Deployment/web-multi
Replicas: current=5 desired=5 min=2 max=5
Health score: 🔴 ScalingLimited 75/100
Summary: HPA is at maxReplicas.

Metrics:
  - Resource cpu current=0% target=80% note="current value is below target"
  - Resource memory current=68% target=50% note="current value is above target"

Recommended actions:
  - HPA is capped at maxReplicas; raise maxReplicas or reduce load/target utilization if more capacity is expected.

Recommended commands:
  - Raise maxReplicas: The HPA is capped at maxReplicas=5. Raising it to 10 allows the controller to add capacity if metrics still require it. (risk: medium)
    $ kubectl patch hpa web-multi -n default --type=merge -p '{"spec":{"maxReplicas":10}}'

Interpretation:
  - [confidence: high] ScalingLimited reports that the visible desired replica count is constrained by maxReplicas.
  - [confidence: medium] Among visible resource utilization metrics, memory has the largest distance from target (ratio 1.360).
  - [confidence: high] This is only an impact estimate; the API does not expose per-metric replica recommendations or the final metric winner.
```

## Findings

既存のHPAシグナルだけでも、以下はかなり説明できます。

- `ScalingActive=False`、condition reason、直近Eventsによるメトリクス取得失敗
- `ScalingLimited=True`、condition reason、`desiredReplicas == maxReplicas` による上限到達
- `currentReplicas` と `desiredReplicas` による見えているScaleUp / ScaleDown方向
- `ScaleDownStabilized` のようなcondition reasonで表面化しているScaleDown stabilization

一方で、現在のHPA statusだけでは安定して断定しづらいものもあります。

- 複数メトリクスHPAで、最終的にどのmetricが推奨値を決めたか
- no-scaleがtolerance由来なのか、roundingや保守的なmetric処理由来なのか
- missing metricsやnot-ready podsが内部推奨値へどう影響したか
- stabilizationに使われた内部recommendation history

## Known Gaps

このプラグインは、HPA status、metrics、conditions、eventsから推論できることを表示します。
controller内部の中間計算や非公開の意思決定履歴を知っているわけではありません。
解釈行にはconfidenceを付け、直接観測できる事実と弱い推論を区別します。

## ロードマップ
- [x] **インテグレーションテスト:** CI検証用の `kind` ベースE2Eテスト。
- [x] **デモのビジュアル化:** ドキュメントへのスクリーンショットの追加。
- [x] **Homebrew配布:** GoReleaserで専用TapのHomebrew CaskとSBOMを生成。
- [x] **インタラクティブTUIモニター:** watchモードをリッチなターミナルダッシュボードに拡張。
- [x] **バッチ分析機能:** `scan` / `list -A --problem` で問題のあるHPAを一括診断。
- [x] **Selector / 複数HPA指定:** `list` / `scan` の `--selector` と `status hpa-a hpa-b` に対応。
- [x] **Suggest/Fix機能:** `--suggest` / `--fix --apply` により、具体的なパッチ案と適用フローを表示。
- [x] **KEDA ScaledObject参照:** `--keda` でScaledObjectを参照し、trigger/conditionの追加文脈を表示。
- [x] **シェル補完:** bash/zsh/fish/powershell向けにフラグ、namespace、context、HPA名の補完を生成。
- [x] **KEDA連携の強化:** trigger種別、metric名、threshold、current value、auth ref、HPA metricとの対応を表示。
- [x] **安定化ウィンドウのカウントダウン:** TUIとテキスト出力で残り時間と視覚的な進捗を表示。
- [x] **メトリクス経路診断:** `--diagnose-metrics` でメトリクス別のヘルスチェックと修復ヒントを表示。
- [x] **Resource整合性チェック:** `--check-resources` でHPA targetとPod resource requests/limitsの整合性を確認。
- [x] **Doctorコマンド:** `doctor <name>` でメトリクス、workload、Pod、resource、Events、capacity、KEDA診断を束ねて障害初動に使える。
- [x] **レポート出力:** `--report markdown` / `--report html` で単体・一覧の診断レポートを生成。
- [x] **TUI複数選択:** TUIで `space` / `a` / `A` による複数選択と、選択HPAに対するCLI一括apply導線を表示。
- [x] **マルチメトリクスディシジョンディープトレース:** メトリクスごとの分析とトレランス/安定化の影響表示。
- [x] **What-If スケーリングシミュレーター:** `--simulate-metric` でメトリクス値の変化をプレビュー。
- [x] **ベストプラクティス監査:** `recommend` サブコマンドでHPA設定のコンプライアンススコア付き監査。
- [x] **過去スケーリングタイムライン:** `timeline --since=30m` でKubernetes Eventsから過去のスケーリング判断を再構成。
- [ ] **TUIバッチapplyワークフロー:** CLIの `list --problem --fix --apply` と同等の、TUI内での複数HPA suggestと安全確認付きapplyを追加。
- [ ] **Custom / External Metrics深掘り:** HPA statusに見える範囲を超え、APIService健全性、adapter推定、Prometheus/custom metricsの確認ヒントを追加。
- [ ] **レポートサマリー強化:** クラスタ全体サマリー、health score下位トップN、推奨アクション一覧を追加。
- [ ] **InformerベースWatch:** 現行のポーリングを維持しつつ、大規模クラスタ向けにopt-inのinformer更新を検討。
- [ ] **KEP-6111 structured decision adapter:** 将来の構造化HPA decision fieldを既存Analysisへ変換する小さなアダプタ境界を維持。
- [ ] **Supply-chain強化:** GoReleaserにSLSA provenanceとcosign署名を追加し、エンタープライズ導入時の検証性を高める。

## ライセンス

Apache-2.0
