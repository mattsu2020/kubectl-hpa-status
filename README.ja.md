# kubectl-hpa-status

[![CI](https://github.com/mattsu2020/kubectl-hpa-status/actions/workflows/ci.yml/badge.svg)](https://github.com/mattsu2020/kubectl-hpa-status/actions/workflows/ci.yml)
[![CodeQL](https://github.com/mattsu2020/kubectl-hpa-status/actions/workflows/codeql.yml/badge.svg)](https://github.com/mattsu2020/kubectl-hpa-status/actions/workflows/codeql.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/mattsu2020/kubectl-hpa-status.svg)](https://pkg.go.dev/github.com/mattsu2020/kubectl-hpa-status)
[![Krew](https://img.shields.io/badge/krew-hpa--status-blue)](https://krew.sigs.k8s.io/plugins/)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)

![kubectl-hpa-status demo](images/demo.png)

既存の Kubernetes API シグナルを活用し、詳細なスケーリング分析とともに HorizontalPodAutoscaler (HPA) の状態を調査するための kubectl プラグインです。

## デモ

- スクリーンショット: [images/demo.png](images/demo.png)
- asciinema記録ソース: [docs/demo.cast](docs/demo.cast)

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
kubectl hpa status <hpa-name> --lang=ja
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

## ロードマップ
- [x] **インテグレーションテスト:** CI検証用の `kind` ベースE2Eテスト。
- [x] **デモのビジュアル化:** ドキュメントへのスクリーンショットの追加。
- [x] **Homebrew配布:** GoReleaserでHomebrew CaskとSBOMを生成。
- [ ] **インタラクティブTUIモニタ:** watchモードをリッチなダッシュボードへ強化。
- [ ] **バッチ分析機能:** 複数ネームスペースの全HPAを一括で診断するコマンド。
- [x] **Suggest/Fix機能:** `--suggest` / `--fix --apply` により、具体的なパッチ案と適用フローを表示。

## ライセンス

Apache-2.0
