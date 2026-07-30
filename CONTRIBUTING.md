# Contributing / コントリビューションガイド

PulseBoard Platform への貢献に興味を持っていただきありがとうございます。
本ドキュメントは Issue 起票・ブランチ運用・ローカル検証・PR 提出までの流れを
まとめたものです。まずは [Code of Conduct](CODE_OF_CONDUCT.md) にも目を通して
ください。

---

## 目次

- [開発環境の準備](#開発環境の準備)
- [ブランチ運用とコミット規約](#ブランチ運用とコミット規約)
- [ローカルでのビルド・テスト・Lint](#ローカルでのビルドテストlint)
- [Pull Request の作成](#pull-request-の作成)
- [Issue の起票](#issue-の起票)
- [セキュリティ問題の報告](#セキュリティ問題の報告)
- [ライセンス](#ライセンス)

---

## 開発環境の準備

以下のいずれかを用意してください。

- Docker & Docker Compose（推奨。全サービスをまとめて起動できます）
- あるいは各サービスのランタイム
  - Python 3.12+ (`services/user-api`)
  - Go 1.22+ (`services/analytics-engine`)
  - Node.js 20+ (`services/notification-service`)

初回セットアップ:

```bash
git clone https://github.com/mohadayo/pulseboard-platform.git
cd pulseboard-platform
cp .env.example .env
make up          # docker compose up --build -d
make health      # 3 サービスの /health を確認
```

環境変数の一覧・意味は [`.env.example`](.env.example) と
[`README.md` の "Environment Variables"](README.md#environment-variables)
を参照してください。新しい環境変数を追加した場合は必ず `.env.example`
にもデフォルト値を追記してください。

## ブランチ運用とコミット規約

- ベースブランチは `main` です。`main` に直接 push せず、必ずトピック
  ブランチを切ってください。
- ブランチ名は目的を短く英語で表す形を推奨します。
  - 例: `feat/user-signups-by-week`, `fix/notification-400-on-invalid-json`,
    `chore/dependabot-config`, `docs/contributing`
- コミットメッセージは [Conventional Commits](https://www.conventionalcommits.org/ja/v1.0.0/)
  の型（`feat` / `fix` / `chore` / `docs` / `refactor` / `test` / `ci` など）を
  日本語プレフィックスとして使うと、既存の Issue / PR タイトルと揃います。
  - 例: `feat(user-api): 週次登録集計 /api/users/signups_by_week を追加`
  - 例: `chore(ci): concurrency グループと timeout-minutes を追加`
- 1 コミット 1 目的を意識し、レビューしやすい粒度で分割してください。

## ローカルでのビルド・テスト・Lint

ルートに用意している `Makefile` から、3 サービス横断で実行できます。

```bash
# ビルド / 起動 / 停止
make build
make up
make down

# テスト
make test           # 3 言語すべて
make test-python    # user-api (pytest)
make test-go        # analytics-engine (go test)
make test-ts        # notification-service (jest)

# Lint
make lint           # 3 言語すべて
make lint-python    # flake8
make lint-go        # go vet
make lint-ts        # eslint

# その他
make health         # /health エンドポイントの疎通確認
make clean          # コンテナ・イメージ・キャッシュを掃除
```

CI (`.github/workflows/ci.yml`) は `push` / `pull_request` (対 `main`) で
上記と同等のコマンドを実行します。PR 提出前にローカルで `make lint`
と `make test` を必ず緑にしてから push してください。

### 特定サービスだけを直接動かす

```bash
# user-api (Python / Flask)
cd services/user-api
pip install -r requirements.txt
pytest -v
flake8 --max-line-length=120 --exclude=__pycache__ .

# analytics-engine (Go)
cd services/analytics-engine
go vet ./...
go test -v ./...

# notification-service (TypeScript / Express)
cd services/notification-service
npm ci
npx tsc --noEmit
npm test
```

## Pull Request の作成

1. Issue が既にある場合はコメントで着手宣言をしてから作業を開始してください
   （重複作業防止）。無い場合は先に Issue を立てるか、PR 本文で背景を説明
   してください。
2. トピックブランチで作業し、こまめに小さくコミットしてください。
3. `make lint` / `make test` がローカルで緑になったことを確認してください。
4. PR を作成すると
   [`.github/PULL_REQUEST_TEMPLATE.md`](.github/PULL_REQUEST_TEMPLATE.md)
   のテンプレートが自動で挿入されます。以下を必ず埋めてください。
   - 変更概要 / 変更内容の詳細
   - `Closes #<issue番号>`（該当 Issue がある場合）
   - 影響範囲のチェックボックス
   - 動作確認手順
   - リスク・注意点
   - チェックリスト（Lint・テスト・ドキュメント・`.env.example` 更新など）
5. 大きめの変更・議論したい段階では **Draft PR** で作成し、レビュー準備が
   整ったら "Ready for review" に切り替えてください。
6. CI が緑で、[`.github/CODEOWNERS`](.github/CODEOWNERS) に基づく
   レビュアーの承認を得たらマージ可能です。マージ方式は **Squash and merge**
   を推奨しています（1 PR = 1 コミット）。

## Issue の起票

新しい Issue は以下のテンプレートから作成できます。

- [Bug report](.github/ISSUE_TEMPLATE/bug_report.md) — 不具合報告
- [Feature request](.github/ISSUE_TEMPLATE/feature_request.md) — 機能追加提案

起票前に既存の open / closed Issue と PR を検索し、重複が無いか確認して
ください。似た議題がある場合はそちらに追記するほうがコンテキストが集約
されます。

## セキュリティ問題の報告

脆弱性・セキュリティ関連の報告は **公開の Issue には書かず**、
[`SECURITY.md`](SECURITY.md) に記載の連絡経路（メール、または GitHub
Security Advisories）を利用してください。

## ライセンス

本リポジトリへのコントリビューションは [MIT License](LICENSE) の下で
公開されることに同意したものとみなされます。
