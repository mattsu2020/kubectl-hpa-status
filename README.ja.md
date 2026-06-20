# kubectl-hpa-status

[![CI](https://github.com/mattsu2020/kubectl-hpa-status/actions/workflows/ci.yml/badge.svg)](https://github.com/mattsu2020/kubectl-hpa-status/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/mattsu2020/kubectl-hpa-status.svg)](https://pkg.go.dev/github.com/mattsu2020/kubectl-hpa-status)
[![Go Report Card](https://goreportcard.com/badge/github.com/mattsu2020/kubectl-hpa-status)](https://goreportcard.com/report/github.com/mattsu2020/kubectl-hpa-status)
[![Release](https://img.shields.io/github/v/release/mattsu2020/kubectl-hpa-status)](https://github.com/mattsu2020/kubectl-hpa-status/releases)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)

<details>
<summary><strong>その他のバッジ</strong></summary>
<br>

[![CodeQL](https://github.com/mattsu2020/kubectl-hpa-status/actions/workflows/codeql.yml/badge.svg)](https://github.com/mattsu2020/kubectl-hpa-status/actions/workflows/codeql.yml)
[![Release workflow](https://github.com/mattsu2020/kubectl-hpa-status/actions/workflows/release.yml/badge.svg)](https://github.com/mattsu2020/kubectl-hpa-status/actions/workflows/release.yml)
[![Stars](https://img.shields.io/github/stars/mattsu2020/kubectl-hpa-status?style=social)](https://github.com/mattsu2020/kubectl-hpa-status/stargazers)
[![GoReleaser](https://img.shields.io/badge/release-GoReleaser-00add8)](https://goreleaser.com/)
[![golangci-lint](https://img.shields.io/badge/lint-golangci--lint-blue)](https://golangci-lint.run/)
[![Krew](https://img.shields.io/badge/krew-hpa--status-blue)](https://krew.sigs.k8s.io/plugins/)
[![Kubernetes](https://img.shields.io/badge/kubernetes-autoscaling%2Fv2-326ce5)](https://kubernetes.io/docs/tasks/run-application/horizontal-pod-autoscale/)
[![Codecov](https://codecov.io/gh/mattsu2020/kubectl-hpa-status/branch/main/graph/badge.svg)](https://codecov.io/gh/mattsu2020/kubectl-hpa-status)

</details>

![kubectl-hpa-status demo](images/demo.png)

既存の Kubernetes API シグナルを活用し、詳細なスケーリング分析とともに HorizontalPodAutoscaler (HPA) の状態を調査するための kubectl プラグインです。

English README: [README.md](README.md)

> **注記**: Krew 経由でインストールした場合は `kubectl hpa_status`（アンダースコア形式）を使用してください。本 README では `kubectl hpa status` を推奨形式として記載していますが、動作しない場合は `kubectl hpa_status` に置き換えてください。

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

## デモ

![kubectl describe hpa と kubectl-hpa-status の比較](images/describe-vs-hpa-status.svg)

| ワークフロー | 画像 | 録画 |
| --- | --- | --- |
| `status --explain` | [status-explain.svg](images/status-explain.svg) | [cast](docs/status-explain.cast) |
| `doctor` 包括診断 | [doctor.svg](images/doctor.svg) | [cast](docs/doctor.cast) |
| `list -A --wide --problem` | [list-wide.svg](images/list-wide.svg) | [cast](docs/list-wide.cast) |
| `scan` クラスタ診断 | [scan-demo.svg](images/scan-demo.svg) | [cast](docs/scan.cast) |
| `timeline --since=30m` | [timeline.svg](images/timeline.svg) | [cast](docs/timeline.cast) |
| `recommend` ベストプラクティス監査 | [recommend.svg](images/recommend.svg) | [cast](docs/recommend.cast) |
| `--simulate-metric` シミュレーション | [simulate.svg](images/simulate.svg) | [cast](docs/simulate.cast) |
| TUI インタラクティブダッシュボード | [tui.svg](images/tui.svg) | [cast](docs/tui.cast) |
| `watch --interval 5s` | [watch-mode.svg](images/watch-mode.svg) | [cast](docs/watch.cast) |
| `--suggest` → `--fix --apply` | [apply-diff.svg](images/apply-diff.svg) | [cast](docs/fix-flow.cast) |
| 日本語ラベル (`--lang=ja`) | [ja-output.svg](images/ja-output.svg) | |
| JSON出力 | [json-output.svg](images/json-output.svg) | |
| メトリクス取得失敗 | [metrics-failure.svg](images/metrics-failure.svg) | |
| スケールダウン安定化 | [stabilized-output.svg](images/stabilized-output.svg) | |
| 複数メトリクス推定 | [multi-metric-output.svg](images/multi-metric-output.svg) | |

## 5分で始める

使い捨ての namespace とサンプル HPA から始めます。

```sh
kubectl apply -f examples/cpu-memory-hpa.yaml
kubectl hpa_status status web-multi -n hpa-status-examples --explain
kubectl hpa_status status web-multi -n hpa-status-examples --suggest
kubectl hpa_status list -n hpa-status-examples --wide
```

まだインストールしていない場合は、このリポジトリ内で `kubectl hpa_status` を `go run .` に置き換えて実行できます。

```sh
go run . status web-multi -n hpa-status-examples --explain
```

## インストール

### Krew（推奨）

```sh
kubectl krew install hpa-status
```

```sh
kubectl hpa_status status <hpa-name> -n <namespace>
kubectl hpa_status list -A --wide
kubectl hpa_status <hpa-name> --suggest
```

Krew はプラグインを `hpa-status` として登録し、`kubectl hpa_status`（アンダースコア形式）で検出されます。本 README では対応環境で `kubectl hpa status` を推奨形式として記載しています。動作しない場合は `kubectl hpa_status status <hpa-name>` または `kubectl-hpa-status status <hpa-name>` を使用してください。

### Homebrew

```sh
brew install mattsu2020/kubectl-hpa-status/kubectl-hpa-status
kubectl-hpa-status list -A --wide
```

### 手動インストール

```sh
go build -o kubectl-hpa-status .
sudo mv ./kubectl-hpa-status /usr/local/bin/
kubectl hpa status <hpa-name> -n <namespace>
```

RBAC 権限については [docs/rbac.yaml](docs/rbac.yaml) を参照してください。

### 必要要件

- **Kubernetes 1.26+**（`autoscaling/v2` 安定API）— 正式サポートかつ E2E テスト対象のバージョン帯です。API 自体は 1.23+ から存在し、プラグインが動作する可能性はありますが、それより古いバージョンは CI マトリクスの対象外です。完全な互換性マトリクスは [docs/reference.md](docs/reference.md) を参照してください。
- kubeconfig が設定された kubectl
- metrics-server（CPU/メモリメトリクス用）またはカスタム/外部メトリクスアダプター

### status の深度ティア

`status` は層構造になっており、通常実行は高速かつ制限付き RBAC 環境でも動作します。各ティアは API 読み取りを追加するので、疑問に答えられる最も浅いティアを選んでください:

| コマンド | 読み取り | 使いどき |
| --- | --- | --- |
| `status <hpa>` | HPA 本体のみ | 高速なヘルスチェック。RBAC 制限・監査環境向け |
| `status <hpa> --explain` | + コンディション、イベント、スケールターゲット Pod | 「なぜこの挙動か？」の調査 |
| `status <hpa> --explain-pods` | + Pod 単位の readiness とリソース要求 | Pod レベルの診断 |
| `status <hpa> --deep` | + キャパシティ、ロールアウト、アダプタ診断 | スケールアウト問題の網羅調査 |
| `status <hpa> --no-enrich` | HPA 本体のみ（明示） | 他フラグが設定されていても HPA のみ強制。エイリアス `--hpa-only` |

`--no-enrich`/`--hpa-only` と `--deep` は `--analysis-profile` の値（`--analysis-profile deep`）としても利用できます。通常の `status` 実行は HPA オブジェクトのみを読むため、Pod/Deployment の権限は不要になりました。

## 代表コマンド

```sh
# 1. 詳細なステータスと解釈・次のアクションを表示
kubectl hpa_status status <hpa> -n <ns> --explain

# 2. スケール失敗時の包括診断
kubectl hpa_status doctor <hpa> -n <ns>

# 3. クラスタ全体の問題のあるHPA一覧
kubectl hpa_status list -A --problem

# 4. kubectl patchコマンドとして修正提案を表示
kubectl hpa_status status <hpa> --suggest

# 5. クラスタ全体のHPA問題をスキャン
kubectl hpa_status scan
```

## 例

実践的なサンプルマニフェストは [examples/](examples/) にあります。

```sh
kubectl apply -f examples/cpu-memory-hpa.yaml
kubectl hpa status web-multi -n hpa-status-examples --explain --suggest
kubectl hpa status list -n hpa-status-examples --wide
```

## ドキュメント

| ドキュメント | 内容 |
| --- | --- |
| [Usage Guide](docs/usage.md) | フラグ参照、設定ファイル、ヘルススコア、TUIキーバインド、JSONPath例 |
| [TUI Manual](docs/tui.md) | インタラクティブダッシュボードの運用フロー、ショートカット、エクスポート方針、トラブルシューティング |
| [Reference](docs/reference.md) | Doctor、Safe Fix Flow、マルチメトリクストレース、Simulator、Auditor、Timeline、トラブルシューティング |
| [Troubleshooting](docs/troubleshooting.md) | 症状/コマンド表、FAQ |
| [Roadmap](ROADMAP.md) | TUI、メトリクス、KEP-6111、サプライチェーン強化の計画 |
| [Promotion Kit](docs/social-promotion.md) | X、Reddit、Slack、Connpass、Zenn 向けのリリース告知テンプレート |

## コミュニティとプロモーション

- HPA 運用に役立った場合は、リポジトリの Star や Fork をお願いします。
- デモ画像 [images/demo.png](images/demo.png) と [images/](images/) のスクリーンショット集を紹介素材として利用できます。
- リリースやデモを告知するときは [docs/social-promotion.md](docs/social-promotion.md) のテンプレートを使えます。
- GitHub Discussions が有効化されたら、バグ報告にはしづらい質問、運用パターン、トラブルシューティング共有に利用してください。

## ロードマップ

現在のロードマップは [ROADMAP.md](ROADMAP.md) で管理しています。直近の優先事項は、TUI 内 batch apply、ヘルススコア説明性の向上、E2E カバレッジ拡充、KEP-6111 対応準備、リリースのサプライチェーン強化です。

## 開発

```sh
make build
make test
make coverage
make docs-check
make lint
```

- [ARCHITECTURE.md](ARCHITECTURE.md)
- [SECURITY.md](SECURITY.md)
- [CONTRIBUTING.md](CONTRIBUTING.md)

## ライセンス

Apache-2.0
