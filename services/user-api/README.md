# user-api

PulseBoard プラットフォームの **ユーザ管理サービス**。ユーザ登録・ログイン（JWT 発行）・プロフィール管理・退会・登録件数の各種集計 API を提供する Python / Flask 製マイクロサービス。

> このファイルは `services/user-api/` 配下だけで完結する情報（ディレクトリ構成・ローカル起動・テスト / lint・Docker 単体実行・サービス固有の環境変数）を集約する。
>
> プラットフォーム全体の構成図・サービス間連携・API 仕様（エンドポイント一覧・パラメータ・サンプル curl）はルート [`README.md`](../../README.md) および [`docs/`](../../docs/) 配下に一次情報があるため、本 README では再掲せずリンクで委譲する。

## 基本情報

| 項目 | 値 |
|------|-----|
| 責務 | ユーザ登録 / ログイン（JWT） / プロフィール管理 / 退会 / 登録件数集計 |
| 言語 / フレームワーク | Python 3.12+ / Flask |
| 公開ポート | `5001`（`USER_API_PORT` で上書き可） |
| エントリ | [`app.py`](./app.py) |
| Dockerfile | [`Dockerfile`](./Dockerfile) |
| 本番サーバ | Gunicorn（`--workers 2`、`Dockerfile` の `CMD` 参照） |
| lint | `flake8`（[`\.flake8`](./.flake8) で設定を一元化） |
| テスト | `pytest`（[`test_app.py`](./test_app.py) ほか） |

## ディレクトリ構成

```
services/user-api/
├── app.py                    # Flask アプリ本体（エントリ）
├── requirements.txt          # pip 依存
├── Dockerfile                # 非 root (UID 1000) で gunicorn を起動
├── .dockerignore             # テスト・ドット設定をイメージから除外
├── .flake8                   # Makefile / CI と共有される flake8 設定
├── .python-version           # ローカル Python バージョンピン
├── test_app.py               # エンドポイント単体テスト
├── test_middleware.py        # セキュリティヘッダ等のミドルウェアテスト
├── test_security_headers.py  # セキュリティレスポンスヘッダ付与の確認
└── test_log_level.py         # LOG_LEVEL のバリデーション / フォールバック
```

バージョンピンの詳細（`.python-version` / `.tool-versions` / `.nvmrc` / `.go-version` の関係）は [`docs/TESTING.md`](../../docs/TESTING.md) を参照。

## ローカル起動

### 開発用（Flask 開発サーバ）

```bash
cd services/user-api
pip install -r requirements.txt
python app.py
```

既定では `http://localhost:5001/` で待ち受ける。`USER_API_PORT=5101 python app.py` のように環境変数で上書き可能。

### 本番相当（Gunicorn、Dockerfile と同じ起動方式）

```bash
cd services/user-api
pip install -r requirements.txt
gunicorn --bind 0.0.0.0:5001 --workers 2 --access-logfile - app:app
```

## ヘルスチェック

```bash
curl -sf http://localhost:5001/health
```

`docker-compose.yml` からも 10 秒間隔で `/health` を呼び出す。`LOG_LEVEL=INFO`（既定）では `/health` のアクセスログが抑止され、Kubernetes / ロードバランサ probe のノイズを除去する。3 サービスまとめて診断するには `make health` を利用する。

## テスト

```bash
# このサービス単体で実行
cd services/user-api
pip install -r requirements.txt
pytest -v

# リポジトリルートから全サービス一括で実行
make test-python   # user-api のみ
make test          # 3 サービス全部（python / go / ts）
```

CI（[`.github/workflows/ci.yml`](../../.github/workflows/ci.yml)）も同じコマンドを実行する。テスト追加時のチェックリストは [`docs/TESTING.md`](../../docs/TESTING.md) を参照。

## Lint

```bash
# このサービス単体で実行
cd services/user-api
flake8 .

# リポジトリルートから
make lint-python
```

`flake8` の設定（`max-line-length = 120` ほか）は [`\.flake8`](./.flake8) に一元化しており、CLI オプションでの上書きは不要。CI と Makefile、ローカル実行のいずれも同じ設定を参照する。

## Docker 単体ビルド / 単体 run

`docker-compose.yml` 経由ではなく user-api 単体で動作確認する場合:

```bash
# ビルド
docker build -t pulseboard-user-api services/user-api/

# 単体起動（既定ポート 5001 をホストに公開）
docker run --rm -p 5001:5001 \
  -e JWT_SECRET=local-dev-secret \
  -e LOG_LEVEL=DEBUG \
  pulseboard-user-api
```

イメージは非 root ユーザ（UID 1000 の `app` ユーザ）で `gunicorn` を起動する。詳細は [`Dockerfile`](./Dockerfile) 冒頭のコメントを参照。

## サービス固有の環境変数

すべての変数は `.env` またはプロセス環境から読み込まれる。既定値は [`.env.example`](../../.env.example) と一致する。

| 変数 | 既定 | 用途 |
|------|------|------|
| `USER_API_PORT` | `5001` | listen ポート |
| `JWT_SECRET` | `pulseboard-dev-secret` | JWT 署名鍵（**本番では必ず上書き**） |
| `TOKEN_EXPIRY_HOURS` | `24` | 発行した JWT の有効期限（時間単位） |
| `MIN_PASSWORD_LENGTH` | `6` | 登録・パスワード変更時の最小文字数 |
| `MAX_NAME_LENGTH` | （`app.py` 参照） | `PATCH /api/users/me` で許容する `name` の最大長 |
| `USERS_DEFAULT_LIMIT` | `50` | `GET /api/users` の既定ページサイズ |
| `USERS_MAX_LIMIT` | `200` | `GET /api/users` の上限ページサイズ |
| `LOG_LEVEL` | `INFO` | `DEBUG` / `INFO` / `WARN` / `ERROR`。大文字小文字無視、不正値は `INFO` にフォールバック |

他 2 サービス（`analytics-engine` / `notification-service`）を含む全環境変数の一覧はルート [`README.md`](../../README.md) の Environment Variables 節および [`docs/SERVICES.md`](../../docs/SERVICES.md) を参照。

## API リファレンス

本サービスが提供するエンドポイント（`/api/users/register`、`/api/users/login`、`/api/users/me`、各種 `signups_by_*` 集計 など）の一覧とサンプル curl は、プラットフォーム全体の API を集約しているルート [`README.md`](../../README.md) の **User API** 節を単一情報源とする。ここでは再掲しない（二重管理を避けるため）。

## 関連ドキュメント

- ルート [`README.md`](../../README.md) — 特徴・技術スタック・全サービス共通のローカル開発入口・API Reference
- [`docs/SERVICES.md`](../../docs/SERVICES.md) — 3 サービス横断の概要表（言語・ポート・エントリ・テスト・環境変数）
- [`docs/ARCHITECTURE.md`](../../docs/ARCHITECTURE.md) — サービス構成・データフロー・データ保持方式・設計判断
- [`docs/TESTING.md`](../../docs/TESTING.md) — 3 サービス横断のテスト運用とバージョンピンの考え方
- [`docs/TROUBLESHOOTING.md`](../../docs/TROUBLESHOOTING.md) — 起動・接続エラーの切り分け手順
- [`docs/OBSERVABILITY.md`](../../docs/OBSERVABILITY.md) — ログ・メトリクス・SLO の運用方針
