# PulseBoard Platform ロードマップ

このドキュメントは PulseBoard Platform の **これから** を示すエントリポイントである。
既に完了した作業は [`CHANGELOG.md`](../CHANGELOG.md) を、現在の設計と運用方針は
[`docs/ARCHITECTURE.md`](ARCHITECTURE.md) / [`docs/RUNBOOK.md`](RUNBOOK.md) を参照すること。

> ロードマップは "宣言" であり "約束" ではない。優先順位は Issue tracker の状況・
> コントリビュータの興味・実装コストによって流動的に変わる。順序や粒度の
> 議論は GitHub Discussions または該当 Issue にて歓迎する。

## 現在地 (Snapshot)

- **構成**: 3 サービス構成のマイクロサービスプラットフォーム
  - `services/user-api` (Python / Flask, `:5001`) — 登録・認証 (JWT)・プロフィール・集計
  - `services/analytics-engine` (Go / net/http, `:5002`) — イベント収集・時系列集計
  - `services/notification-service` (TypeScript / Express, `:5003`) — 多チャネル通知
- **データストア**: すべて **in-memory** (プロセス再起動で揮発)。永続化層は未接続。
- **オーケストレーション**: `docker-compose.yml` によるローカル開発。Kubernetes マニフェストや Helm チャートは未提供。
- **CI**: GitHub Actions で 3 言語のテスト・lint・compose build を実行。
- **ドキュメント**: [`docs/`](.) 配下に ARCHITECTURE / OBSERVABILITY / RUNBOOK / TROUBLESHOOTING / FAQ / GLOSSARY が整備済み。

## Now (短期 / この四半期)

現在着手中もしくは直近で着手予定のもの。**新規コントリビュータ歓迎ゾーン。**

| テーマ | 概要 |
|--------|------|
| ドキュメント整備 | ROADMAP / FAQ / TROUBLESHOOTING の継続拡充。新規参入者の学習コスト低減。 |
| 開発者体験 (DX) | `.editorconfig` / Dependabot / gitleaks 等のリポジトリメタ設定の充足。 |
| 集計 API の粒度追加 | `signups_by_*` / `events_by_*` / `notifications/by_*` の追加粒度 (四半期・営業日など)。 |
| Health check の拡張 | 各サービスの `/health` に依存関係情報 (Go/Node/Python バージョン、uptime) を含める検討。 |
| テストカバレッジ | 3 サービスの境界系テスト (境界値・エラーパス・並行性) の追加。 |

## Next (中期 / 半年前後)

Now が一段落した後に着手したいもの。**設計議論歓迎ゾーン。**

| テーマ | 概要 |
|--------|------|
| 永続化層の導入 | 現状の in-memory ストアを PostgreSQL / Redis / SQLite のいずれかで永続化する。サービスごとの選定は Issue で議論。 |
| 認証の強化 | Refresh Token / トークン失効リスト / パスワードリセットフロー。JWT のみに依存しない設計。 |
| 通知チャネルの実装 | 現状 `email` / `sms` / `push` は列挙のみ。実 provider (SES / Twilio / FCM 等) の adapter 実装。 |
| Analytics のバッチ集計 | 大量イベント時のリアルタイム集計コスト削減。事前集計テーブルの検討。 |
| OpenAPI / AsyncAPI 化 | 各サービスの API を機械可読な形式で公開し、SDK 自動生成に道を拓く。 |
| Metric エクスポート | `/metrics` (Prometheus format) の実装。`docs/OBSERVABILITY.md` の SLO/SLI と接続。 |

## Later (長期 / 1 年以降または要議論)

現時点では優先度が低い、または前提となる技術選定が終わっていないもの。

| テーマ | 概要 |
|--------|------|
| サービス間 gRPC 化 | 内部通信を HTTP から gRPC に切り替え、schema-first にする。 |
| Kubernetes デプロイ | Helm chart / Kustomize overlay の提供。マルチ環境デプロイ。 |
| Event streaming バックボーン | Kafka / NATS 等を経由した非同期メッセージング。 |
| フロントエンド SPA | 現状はバックエンドのみ。ダッシュボード UI の同梱可否を検討する (Non-goals も参照)。 |
| マルチリージョン対応 | データ複製戦略と consistency モデルの決定。 |

## Non-goals (意図的にやらないこと)

無駄な PR や議論のズレを防ぐため、**現時点では扱わない** ことを明示する。
これらは Later で再検討する可能性はあるが、現在の PR / Issue の対象外とする。

- **マルチテナント SaaS 化**: 単一組織向けの platform であり、テナント分離機構は導入しない。
- **フロントエンド SPA の同梱**: `pulseboard-platform` リポジトリはバックエンド API 群のみを扱う。UI は別リポジトリで扱う想定。
- **プロダクション grade の in-memory 継続**: 永続化は Next フェーズで導入する前提であり、"in-memory のまま最適化する" 系の提案は受け入れない。
- **独自認証プロトコルの発明**: OAuth 2.0 / OIDC / JWT の標準にのっとる。独自プロトコルは採用しない。
- **サービス間の同期呼び出しの多用**: サービス境界はイベント駆動または明示的な public API のみとする。内部同期呼び出しの追加は原則行わない。

## 貢献指針

- Now のテーマは **軽量な PR** で貢献しやすい。既存 Issue に紐付けるか、新規 Issue を先に作ってから PR を送るのが望ましい。
- Next / Later のテーマは **設計 Issue** を先に立ち上げ、方針合意を得てから実装 PR に進む。
- Non-goals に該当する変更は原則クローズされる。該当するかどうかの判断が難しい場合は、まず Discussion / Issue で確認すること。
- すべての PR は [`CONTRIBUTING.md`](../CONTRIBUTING.md) と [`CODE_OF_CONDUCT.md`](../CODE_OF_CONDUCT.md) に従うこと。

## 更新方針

- 大きな方針転換 (例: Non-goals からの除外、Later → Next への昇格) は PR で議論し、レビュワーの合意を経て反映する。
- 完了した項目は本ドキュメントから削除し、[`CHANGELOG.md`](../CHANGELOG.md) 側に記録する (完了項目の二重管理を避ける)。
- テーブルの粒度は "1 テーマ = 1 行" を目安とし、詳細は個別 Issue にリンクする形で膨張を避ける。
