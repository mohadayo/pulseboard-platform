# FAQ (よくある質問)

PulseBoard Platform (`user-api` / `analytics-engine` / `notification-service`)
の利用・開発でよく寄せられる質問をまとめています。

- 全体構成: [ARCHITECTURE.md](./ARCHITECTURE.md)
- 個別の問題への対処: [TROUBLESHOOTING.md](./TROUBLESHOOTING.md)

## プロジェクトについて

### Q. PulseBoard Platform とは何ですか？

複数の microservice からなるダッシュボードプラットフォームで、
以下 3 つのサービスから構成されます:

- **user-api** (Python): ユーザ管理・認証
- **analytics-engine** (Go): 集計エンジン
- **notification-service** (TypeScript/Node.js): 通知配信

詳細は [ARCHITECTURE.md](./ARCHITECTURE.md) を参照してください。

### Q. 対象ユーザは？

社内プロダクトのメトリクス集約と通知配信をまとめて扱いたい
プロダクトチーム / SRE を想定しています。

## セットアップについて

### Q. ローカルで起動するには？

```sh
cp .env.example .env
docker compose up -d
```

3 サービスすべてが一度に起動します。
詳細な手順は [../README.md](../README.md) を参照してください。

### Q. 使用する言語バージョンは？

`.tool-versions` (`asdf` / `mise` 互換) で固定しています:

- Python 3.12
- Go 1.22
- Node.js 20

CI (`.github/workflows/ci.yml`) と揃えるため、必ずこのバージョンを使用してください。

## 開発について

### Q. どのブランチに PR を出しますか？

デフォルトブランチ (`main`) に対して PR を作成してください。
ブランチ名は以下のプレフィックスで始めてください:

- `feat/` — 新機能
- `fix/` — バグ修正
- `docs/` — ドキュメントのみ
- `chore/` — その他
- `refactor/` — 挙動を変えないリファクタ
- `test/` — テスト

詳細は [../CONTRIBUTING.md](../CONTRIBUTING.md) を参照してください。

### Q. サービスごとのテストは？

CI と同じコマンドを流してください:

- `services/user-api`: `pytest -v`
- `services/analytics-engine`: `go test -v ./...`
- `services/notification-service`: `npm test`

`Makefile` にサービス横断ターゲットが用意されている場合は
`make test` で全サービス一括実行できます。

### Q. lint はどう実行しますか？

- `services/user-api`: `flake8` (`.flake8` に設定)
- `services/analytics-engine`: `go vet ./...`
- `services/notification-service`: `npm run lint` (`.eslintrc.json` に設定)

`.editorconfig` / `.gitattributes` により行末・改行コードは統一されています。

### Q. 各サービスのポートは？

`.env.example` と `docker-compose.yml` を参照してください。
競合したときの対処は [TROUBLESHOOTING.md](./TROUBLESHOOTING.md) を参照。

## セキュリティ・運用について

### Q. 脆弱性を発見したときの報告先は？

[SECURITY.md](../SECURITY.md) に記載の連絡先へ非公開でご報告ください。
GitHub の Public Issue には投稿しないでください。

### Q. 依存パッケージの更新方針は？

Dependabot を有効化しており、Python / Node.js / Go / GitHub Actions /
Docker それぞれの依存を週次で自動チェックしています。
セキュリティアップデートは即マージ、通常のマイナー更新は動作確認後に
マージする運用です。

## その他

### Q. 不具合を報告したい・機能要望を出したい

`.github/ISSUE_TEMPLATE/` の該当テンプレートを使ってご報告ください。
再現手順・環境情報を記入いただけると調査がスムーズです。

### Q. サポートを受けたい

一次窓口は [SECURITY.md](../SECURITY.md) と
[CONTRIBUTING.md](../CONTRIBUTING.md) に記載しています。
