# テスト（TESTING）

PulseBoard プラットフォームは **3 サービス** がそれぞれ別のテストランナー・lint ツールで検証されます。ローカル検証と CI (`.github/workflows/ci.yml`) の対応関係・テスト配置ルール・新規追加時の勘所をここに集約します。

Makefile / CI ワークフロー / `CONTRIBUTING.md` に散らばっていた情報の統合インデックスとして参照してください。

## サービスとテストランナー

| サービス | 言語 | テストランナー | Lint | バージョンピン | 代表コマンド（Makefile） |
|---|---|---|---|---|---|
| `services/user-api/` | Python | `pytest` | `flake8` | `.python-version` | `make test-python` / `make lint-python` |
| `services/analytics-engine/` | Go | 標準 `go test` | `go vet` | `.go-version` | `make test-go` / `make lint-go` |
| `services/notification-service/` | TypeScript (Node) | `jest`（`npm test`） | `eslint` | `.nvmrc` | `make test-ts` / `make lint-ts` |

### バージョンピン方針

各サービスは自分のディレクトリに **バージョンファイルを 1 箇所だけ** 持ちます。CI (`setup-python` / `setup-go` / `setup-node`) もこれらのファイルを参照する (`python-version-file` 等) ので、ローカル (`asdf` / `mise` / `pyenv` / `nvm`) と CI でバージョンがズレることはありません。

- Python 版の変更は `services/user-api/.python-version` のみを編集
- Go 版の変更は `services/analytics-engine/.go-version` のみを編集
- Node 版の変更は `services/notification-service/.nvmrc` のみを編集

### Lint 設定の集約先

- Python: `services/user-api/.flake8`（Makefile / CI は `flake8 .` を叩くだけ）
- Go: 標準 `go vet`（追加設定なし）
- TypeScript: `services/notification-service/.eslintrc.json`（`npx eslint src/` が対象）

## ローカルで CI と等価な検証を行う

CI (`.github/workflows/ci.yml`) は以下 4 ジョブで構成される：

| ジョブ | 並列/直列 | 内部ステップ |
|---|---|---|
| `test-python` | 3 本並列 | `pip install -r requirements.txt → flake8 . → pytest -v` |
| `test-go` | 3 本並列 | `go vet ./... → go test -v ./...` |
| `test-typescript` | 3 本並列 | `npm ci → tsc --noEmit → eslint src/ → npm test` |
| `docker-build` | 直列（上記 3 本に `needs`） | `docker compose build` |

Makefile のターゲットは CI の各ステップと 1:1 で対応します：

```sh
make test        # test-python + test-go + test-ts
make lint        # lint-python + lint-go + lint-ts
```

CI の `docker-build` 相当は Makefile では `make build` に相当します：

```sh
make build       # docker compose build
```

push 前の最終確認は以下 1 行で CI 失敗の多くを先取り検知できます：

```sh
make lint test build
```

## 新しいテストを追加する時のチェックリスト

### Python (`services/user-api/`)

- [ ] テストファイルは `test_*.py` または `*_test.py`（`pytest` のデフォルト discovery に合わせる）
- [ ] テスト用の追加依存は `requirements.txt`（本プロジェクトは dev / prod を分けていない）に追加し、CI がそのまま解決できる状態を保つ
- [ ] `flake8` の対象から外れないよう `.flake8` 設定（`services/user-api/.flake8`）の `exclude` に該当しないパスへ置く
- [ ] 外部 I/O（HTTP / DB / 時刻）はモック化し、CI での flakiness を避ける

### Go (`services/analytics-engine/`)

- [ ] テストファイル名は `<対象>_test.go`、関数は `TestXxx(t *testing.T)` の規約に従う
- [ ] テーブル駆動テストを基本とし、`t.Run(name, ...)` でサブテスト名を付ける
- [ ] `analytics-engine` は現状 `go.sum` を持たず標準ライブラリのみで動く。外部依存を追加する際は `go.sum` が生成されるため、CI の `cache-dependency-path` を `go.mod` から `go.sum` に切り替えること
- [ ] `go test -race ./...` もローカルで一度は走らせる

### TypeScript (`services/notification-service/`)

- [ ] テストは `src/**/*.test.ts` に配置する（`jest` のデフォルト設定）
- [ ] `tsc --noEmit` が走るため、テスト内の型もフル検査される。型を緩める時は `expect-error` コメントで局所化する
- [ ] モックは `jest.mock(...)` を使い、`beforeEach` で `jest.resetModules()` / `jest.clearAllMocks()` を呼んで副作用漏れを防ぐ
- [ ] `npm ci` はロックファイル厳密モードで実行される。依存追加時は `package-lock.json` のコミットを忘れない

### 共通

- [ ] CI の 3 ジョブすべてが新規テストで緑であることを `make test` でローカル確認してから push する
- [ ] テストが Docker Compose のサービス構成に依存する統合テストは `docker-compose.yml` と整合させ、必要なら `make up` でローカル環境を立ち上げてから走らせる

## タイムアウトとキャンセル挙動

- CI の各テストジョブは `timeout-minutes: 10`（`docker-build` のみ 20）で打ち切られる。これを超えるテストは **ユニットでなく統合 / E2E** として別パイプラインを検討する
- 同一 ref に短時間で連続 push した場合は、CI の `concurrency` 設定により進行中の古いジョブがキャンセルされる

## 関連ドキュメント

- [`../CONTRIBUTING.md`](../CONTRIBUTING.md) — ブランチ運用・コミット規則・レビューの流れ
- [`./ARCHITECTURE.md`](./ARCHITECTURE.md) — 3 サービスの責務とサービス間通信（テストの境界設計を考える時の参照先）
- [`./SERVICES.md`](./SERVICES.md) — サービスごとの詳細（ポート・エンドポイント・依存関係）
- [`./TROUBLESHOOTING.md`](./TROUBLESHOOTING.md) — テスト以外の運用で発生しがちな事象の切り分け
- [`./GLOSSARY.md`](./GLOSSARY.md) — テスト関連で登場する用語の定義
- [`../Makefile`](../Makefile) — 本ドキュメントが参照する全ターゲットの一次定義
- [`../.github/workflows/ci.yml`](../.github/workflows/ci.yml) — CI の一次定義
