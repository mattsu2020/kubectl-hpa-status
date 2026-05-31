# kubectl-hpa-status

[![CI](https://github.com/mattsu2020/kubectl-hpa-status/actions/workflows/ci.yml/badge.svg)](https://github.com/mattsu2020/kubectl-hpa-status/actions/workflows/ci.yml)
[![CodeQL](https://github.com/mattsu2020/kubectl-hpa-status/actions/workflows/codeql.yml/badge.svg)](https://github.com/mattsu2020/kubectl-hpa-status/actions/workflows/codeql.yml)
[![Release](https://github.com/mattsu2020/kubectl-hpa-status/actions/workflows/release.yml/badge.svg)](https://github.com/mattsu2020/kubectl-hpa-status/actions/workflows/release.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/mattsu2020/kubectl-hpa-status.svg)](https://pkg.go.dev/github.com/mattsu2020/kubectl-hpa-status)
[![Release](https://img.shields.io/github/v/release/mattsu2020/kubectl-hpa-status)](https://github.com/mattsu2020/kubectl-hpa-status/releases)
[![GoReleaser](https://img.shields.io/badge/release-GoReleaser-00add8)](https://goreleaser.com/)
[![golangci-lint](https://img.shields.io/badge/lint-golangci--lint-blue)](https://golangci-lint.run/)
[![Krew](https://img.shields.io/badge/krew-hpa--status-blue)](https://krew.sigs.k8s.io/plugins/)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)

![kubectl-hpa-status demo](images/demo.png)

既存の Kubernetes API シグナルを活用し、詳細なスケーリング分析とともに HorizontalPodAutoscaler (HPA) の状態を調査するための kubectl プラグインです。

English README: [README.md](README.md)

このツールは、HPA運用でよくある3つの疑問にすばやく答えます。

- このHPAは正常か、上限に張り付いているか、安定化中か、メトリクス取得に失敗しているか。
- どのConditionやメトリクスが現在の挙動を説明しているか。
- 次に実行すべきコマンドは何か、安全にdry-run検証できるか。

リポジトリ名とバイナリ名は `kubectl-hpa-status` です。`kubehpa_cli` は初期開発時の作業ディレクトリ名/愛称であり、リリース成果物、Go module path、インストールコマンドでは使いません。

## デモ

- スクリーンショット: [images/demo.png](images/demo.png)
- 比較画像: [images/describe-vs-hpa-status.svg](images/describe-vs-hpa-status.svg)
- status explainデモ: [docs/status-explain.cast](docs/status-explain.cast)
- wide listデモ: [docs/list-wide.cast](docs/list-wide.cast)
- watchデモ: [docs/watch.cast](docs/watch.cast)

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

## クイックスタート

```sh
kubectl hpa status <hpa-name> -n <namespace>
kubectl hpa status <hpa-name> --explain
kubectl hpa status <hpa-name> --suggest
kubectl hpa status <hpa-name> --fix --apply
kubectl hpa status <hpa-name> --fix --apply --dry-run=false
kubectl hpa status <hpa-name> --lang=ja
kubectl hpa status scan
kubectl hpa status list -A --problem
kubectl hpa status list -A --wide --sort-by=desired --filter=scaling-limited
kubectl hpa status ls -A -o json
kubectl hpa status <hpa-name> --watch --timeout=2m --until-condition=scaling-limited
kubectl hpa status <hpa-name> -o 'jsonpath={.analysis.summary}'
```

出力の読み方:

- `Summary` (要約) は、HPAステータスから導出された視覚的な状態です。
- `Recommended actions` (推奨アクション) は、ConditionやBehavior設定に基づいた運用上のヒントです。
- `Interpretation` (解釈) は診断上の推論であり、コントローラーの非公開な決定履歴そのものではありません。
- `confidence: high` (確信度: 高) は明示的なステータスフィールドに基づいていることを示し、`confidence: medium` (確信度: 中) はステータスと説明が一致しているものの、API自体が内部の詳細な理由を開示していないことを示します。

## インストール

### Krew (推奨)

```sh
kubectl krew install hpa-status
kubectl hpa status <hpa-name> -n <namespace>
kubectl hpa status list -A --wide
kubectl hpa status <hpa-name> --suggest
```

### Homebrew

```sh
brew install mattsu2020/tap/kubectl-hpa-status
kubectl-hpa-status list -A --wide
```

### 手動インストール

```sh
go mod tidy
go build -o kubectl-hpa-status .
chmod +x ./kubectl-hpa-status
sudo mv ./kubectl-hpa-status /usr/local/bin/
kubectl hpa status <hpa-name> -n <namespace>
```

## 開発

```sh
make build
make test
make coverage
make lint
make release-check
```

kindを使ったE2Eテスト:

```sh
kind create cluster --name hpa-status-dev
make e2e
kind delete cluster --name hpa-status-dev
```

## よくあるトラブルパターン

| 症状 | コマンド | 主なシグナル | 次の一手 |
| --- | --- | --- | --- |
| メトリクスが取れずスケールしない | `kubectl hpa status <name> --explain` | `ScalingActive=False`, Events | metrics-server または custom/external metrics adapter を確認 |
| レプリカ数が上限に張り付く | `kubectl hpa status <name> --suggest` | `ScalingLimited=True`, `desiredReplicas == maxReplicas` | 容量を確認し、提案されたmaxReplicasパッチをdry-run検証 |
| スケールダウンが遅い | `kubectl hpa status <name> --explain` | `ScaleDownStabilized`, `spec.behavior.scaleDown` | stabilization windowを待つか調整 |
| クラスタ全体を棚卸ししたい | `kubectl hpa status scan` | health score, issue, conditions | `ERROR` から優先して確認 |

## 互換性マトリクス

| 環境 | 状態 |
| --- | --- |
| HPA API `autoscaling/v2` | 必須 |
| Kubernetes v1.35.0 | 検証済み |
| kind上のmetrics-server v0.8.1 | 検証済み |
| custom/external metrics adapters | HPA statusに見える範囲で対応 |
| KEDA管理のHPA | HPAオブジェクトとして診断可能。KEDA固有分析は将来対応 |

## 安全な修正フロー

`--suggest` / `--fix --apply` は安全側に倒しています。

1. `--suggest` は `--dry-run=server` 付きの `kubectl patch` を表示します。
2. `--fix --apply` もデフォルトではserver-side dry-runで、適用前に差分を表示します。
3. 永続的に変更するには `--dry-run=false` が明示的に必要です。
4. maxReplicas引き上げ提案には、容量・quota・コスト・下流依存の確認を促す警告を出します。

## ロードマップ
- [x] **インテグレーションテスト:** CI検証用の `kind` ベースE2Eテスト。
- [x] **デモのビジュアル化:** ドキュメントへのスクリーンショットの追加。
- [x] **Homebrew配布:** GoReleaserでHomebrew CaskとSBOMを生成。
- [ ] **インタラクティブTUIモニタ:** watchモードをリッチなダッシュボードへ強化。
- [x] **バッチ分析機能:** `scan` / `list -A --problem` で問題のあるHPAを一括診断。
- [x] **Suggest/Fix機能:** `--suggest` / `--fix --apply` により、具体的なパッチ案と適用フローを表示。

## ライセンス

Apache-2.0
