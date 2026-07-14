# Security Policy

## 対応バージョン

`main` ブランチのみサポート対象です。過去のタグ・ビルドに対するセキュリティ修正のバックポートは行いません。

## 脆弱性の報告

セキュリティに関わる問題は **公開 Issue に投稿しないでください**。
GitHub の [Security Advisories](https://github.com/mohadayo/pulseboard-platform/security/advisories/new) 経由で
非公開で報告してください。

### 報告に含めてほしい内容

- 対象コミット SHA / タグ
- 対象サービス (`services/user-api` / `services/analytics-engine` / `services/notification-service` / `docker-compose`)
- 再現手順（可能なら最小 HTTP リクエスト / 設定ファイル例）
- 想定される影響（機密漏洩・改ざん・DoS・権限昇格 等）
- （任意）修正案・PoC

24〜72 時間以内に一次応答することを目標とします。

## 脅威モデル

PulseBoard Platform は複数マイクロサービス（Python の `user-api`、Go の `analytics-engine`、
TypeScript の `notification-service`）を Docker Compose で連携させたユーザー管理・分析・通知基盤です。
以下のカテゴリを主要な脅威として扱います。

1. **認証情報の漏洩** — `user-api` からのユーザー情報流出、通知チャネルへの意図しない配信
2. **入力バリデーション回避** — API に細工リクエストを送りエラー系パスやパーサを崩す
3. **依存パッケージの既知脆弱性** — Python/Node.js/Go/Docker ベースイメージ経由の CVE
4. **設定漏洩** — 秘密鍵・API トークン・DB 認証情報のリポジトリ / ログ / イメージへの混入
5. **ネットワーク境界侵害** — 内部サービスが誤って外部公開ポートに晒される
6. **DoS 相当のリソース枯渇** — 上限のないリクエスト受付・過大なペイロード

## 設計上の防御ライン

### CI ゲート

- Python: `flake8` + `pytest`
- Go: `go vet` + `go test`
- TypeScript: `tsc --noEmit` + `npm test`
- Docker: `docker compose build`
- 全ジョブが緑になるまで PR をマージしない運用

### コンテナ境界

- 各サービスは独立した Dockerfile で最小権限イメージを構築
- `docker-compose.yml` で公開ポートを明示的に定義し、意図しないポート露出を防止
- 秘密情報は環境変数として注入し、イメージや Git 履歴に含めない

## セキュリティに影響する PR のレビュー観点

以下の変更を含む PR は最低 1 名のセキュリティレビューを必須とします：

- 認証・認可ロジック（`user-api` のセッション管理、`notification-service` の配信認可 等）
- 入力パーサ・シリアライザ（JSON / YAML / HTTP ヘッダ処理）
- 外部通信先 (`http.Get` / `requests.get` / `fetch` の URL 生成)
- Docker イメージのベース・実行ユーザ (`USER` 指定) の変更
- `.env.example` / `docker-compose.yml` の環境変数・ポート追加削除
- CI ワークフロー (`.github/workflows/*.yml`) の権限昇格 (`permissions:` / `secrets:` 追加)

対応するテストの追加・更新を伴わない防御ラインの緩和は原則としてマージしません。

## 開発時のシークレット管理

- `.env.example` は雛形のみを含み、実際の値は各開発者ローカルの `.env` にのみ配置する
- `.env` は `.gitignore` に含まれており、リポジトリにはコミットしない
- 万一シークレットがコミットされた場合は、直ちに該当キーをローテーションした上で
  上記 Security Advisories 経由で報告してください（履歴からの完全除去だけでは無効化されません）
