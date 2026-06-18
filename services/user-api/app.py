import os
import re
import logging
import hashlib
import uuid
from datetime import datetime, timedelta, timezone

import jwt
from flask import Flask, request, jsonify
from flask_cors import CORS

app = Flask(__name__)
CORS(app)

logging.basicConfig(
    level=os.getenv("LOG_LEVEL", "INFO"),
    format="%(asctime)s [%(levelname)s] %(name)s: %(message)s",
)
logger = logging.getLogger("user-api")

SECRET_KEY = os.getenv("JWT_SECRET", "pulseboard-dev-secret")
TOKEN_EXPIRY_HOURS = int(os.getenv("TOKEN_EXPIRY_HOURS", "24"))

# 入力バリデーション設定（環境変数で上書き可能）。
EMAIL_REGEX = re.compile(r"^[^\s@]+@[^\s@]+\.[^\s@]+$")
MAX_EMAIL_LENGTH = 254  # RFC 5321
MIN_PASSWORD_LENGTH = max(1, int(os.getenv("MIN_PASSWORD_LENGTH", "6")))

# GET /api/users のページネーション既定値・上限。
USERS_DEFAULT_LIMIT = max(1, int(os.getenv("USERS_DEFAULT_LIMIT", "50")))
USERS_MAX_LIMIT = max(USERS_DEFAULT_LIMIT, int(os.getenv("USERS_MAX_LIMIT", "200")))
MAX_OFFSET = 1_000_000_000
# GET /api/users の検索文字列 q の最大長。クライアントが極端に長い文字列を渡してきた際の
# CPU 消費・ログ肥大を抑える。
MAX_SEARCH_LENGTH = max(1, int(os.getenv("MAX_SEARCH_LENGTH", "200")))
# `name` の最大長。register 時の型・長さ検証で使用する。
MAX_NAME_LENGTH = max(1, int(os.getenv("MAX_NAME_LENGTH", "100")))

ALLOWED_USER_SORT_FIELDS = {"email", "name", "created_at"}
ALLOWED_SORT_ORDERS = {"asc", "desc"}

users_db: dict[str, dict] = {}


def hash_password(password: str) -> str:
    return hashlib.sha256(password.encode()).hexdigest()


def normalize_email(raw: str) -> str:
    """メールアドレスを前後空白除去＋小文字化して正規化する。

    大文字小文字だけが異なる重複登録（Foo@x.com と foo@x.com）や、
    登録時と異なる大小文字でのログイン失敗を防ぐためにキーを統一する。
    """
    return raw.strip().lower()


def _parse_pagination_param(raw, default, min_value, max_value):
    """クエリ文字列を整数として検証する。

    未指定なら default、整数でない・範囲外なら None（呼び出し側が 400 を返す）。
    """
    if raw is None:
        return default
    if not re.fullmatch(r"-?\d+", raw):
        return None
    value = int(raw)
    if value < min_value or value > max_value:
        return None
    return value


@app.route("/health", methods=["GET"])
def health():
    logger.info("Health check requested")
    return jsonify({"status": "healthy", "service": "user-api", "timestamp": datetime.now(timezone.utc).isoformat()})


@app.route("/api/users/register", methods=["POST"])
def register():
    data = request.get_json(silent=True)
    if not data or not data.get("email") or not data.get("password"):
        logger.warning("Registration attempt with missing fields")
        return jsonify({"error": "Email and password are required"}), 400

    email_raw = data.get("email")
    password = data.get("password")
    if not isinstance(email_raw, str):
        return jsonify({"error": "Email must be a string"}), 400
    if not isinstance(password, str):
        return jsonify({"error": "Password must be a string"}), 400

    email = normalize_email(email_raw)
    if not email:
        return jsonify({"error": "Email must not be blank"}), 400
    if len(email) > MAX_EMAIL_LENGTH:
        return jsonify({"error": f"Email must be at most {MAX_EMAIL_LENGTH} characters"}), 400
    if not EMAIL_REGEX.match(email):
        logger.warning("Registration attempt with invalid email format")
        return jsonify({"error": "Invalid email format"}), 400
    if len(password) < MIN_PASSWORD_LENGTH:
        return jsonify({"error": f"Password must be at least {MIN_PASSWORD_LENGTH} characters"}), 400

    # `name` の型・長さを検証する。未指定なら "" を入れる。
    # 検証していないと数値・配列・オブジェクトが保存され、後段の
    # GET /api/users（`name.lower()` / sort 比較）で 500 を起こす。
    name_raw = data.get("name", "")
    if name_raw is None:
        name = ""
    elif not isinstance(name_raw, str):
        logger.warning("Registration attempt with non-string name")
        return jsonify({"error": "Name must be a string"}), 400
    else:
        name = name_raw.strip()
        if len(name) > MAX_NAME_LENGTH:
            return jsonify({"error": f"Name must be at most {MAX_NAME_LENGTH} characters"}), 400

    if email in users_db:
        logger.warning("Registration attempt with existing email: %s", email)
        return jsonify({"error": "User already exists"}), 409

    user_id = str(uuid.uuid4())
    users_db[email] = {
        "id": user_id,
        "email": email,
        "password": hash_password(password),
        "name": name,
        "created_at": datetime.now(timezone.utc).isoformat(),
    }
    logger.info("User registered: %s", email)
    return jsonify({"id": user_id, "email": email, "name": name}), 201


@app.route("/api/users/login", methods=["POST"])
def login():
    data = request.get_json(silent=True)
    if not data or not data.get("email") or not data.get("password"):
        logger.warning("Login attempt with missing fields")
        return jsonify({"error": "Email and password are required"}), 400

    email_raw = data.get("email")
    password = data.get("password")
    if not isinstance(email_raw, str) or not isinstance(password, str):
        logger.warning("Login attempt with non-string credentials")
        return jsonify({"error": "Invalid credentials"}), 401

    # 登録時と同じ正規化を施し、大小文字違いでもログインできるようにする。
    email = normalize_email(email_raw)
    user = users_db.get(email)
    if not user or user["password"] != hash_password(password):
        logger.warning("Failed login attempt for: %s", email)
        return jsonify({"error": "Invalid credentials"}), 401

    exp = datetime.now(timezone.utc) + timedelta(hours=TOKEN_EXPIRY_HOURS)
    token = jwt.encode(
        {"user_id": user["id"], "email": email, "exp": exp},
        SECRET_KEY,
        algorithm="HS256",
    )
    logger.info("User logged in: %s", email)
    return jsonify({"token": token, "user_id": user["id"]})


@app.route("/api/users/me", methods=["GET"])
def get_current_user():
    auth_header = request.headers.get("Authorization", "")
    if not auth_header.startswith("Bearer "):
        return jsonify({"error": "Authorization header required"}), 401

    try:
        payload = jwt.decode(auth_header[7:], SECRET_KEY, algorithms=["HS256"])
    except jwt.ExpiredSignatureError:
        return jsonify({"error": "Token expired"}), 401
    except jwt.InvalidTokenError:
        return jsonify({"error": "Invalid token"}), 401

    user = users_db.get(payload["email"])
    if not user:
        return jsonify({"error": "User not found"}), 404

    logger.info("Profile retrieved for: %s", payload["email"])
    return jsonify({"id": user["id"], "email": user["email"], "name": user["name"], "created_at": user["created_at"]})


def _parse_iso_datetime(raw, field):
    """`raw` を ISO 8601 文字列としてパースして UTC `datetime` に正規化する。

    `Z` 末尾は `+00:00` に置換してから `datetime.fromisoformat` に渡す。
    タイムゾーン無指定（naive）の入力は UTC として扱う（`created_at` も
    `datetime.now(timezone.utc).isoformat()` で書き込んでおり、UTC 同士の
    比較になるため）。

    戻り値は `(datetime|None, error_msg|None)`。
    - 未指定 (`None` または空白のみ): `(None, None)` — フィルタしない
    - パース失敗: `(None, "...")` — 呼び出し側で 400 を返す対象
    - 正常: `(datetime, None)`
    """
    if raw is None:
        return None, None
    if not isinstance(raw, str):
        return None, f"{field} must be a string"
    stripped = raw.strip()
    if not stripped:
        return None, None
    normalized = stripped[:-1] + "+00:00" if stripped.endswith("Z") else stripped
    try:
        parsed = datetime.fromisoformat(normalized)
    except ValueError:
        return None, f"{field} must be an ISO 8601 datetime"
    if parsed.tzinfo is None:
        parsed = parsed.replace(tzinfo=timezone.utc)
    return parsed, None


def _normalize_q(raw):
    """`q` パラメータを正規化する。

    戻り値は (正規化後の値, エラーメッセージ)。
    - None や trim 後が空 → (None, None) ：フィルタしない
    - 上限超過 → (None, ".. too long") ：呼び出し側で 400 を返す対象
    - 正常 → (lowercased, None)
    """
    if raw is None:
        return None, None
    if not isinstance(raw, str):
        return None, "q must be a string"
    stripped = raw.strip()
    if not stripped:
        return None, None
    if len(stripped) > MAX_SEARCH_LENGTH:
        return None, f"q must be at most {MAX_SEARCH_LENGTH} characters"
    return stripped.lower(), None


@app.route("/api/users/me/password", methods=["POST"])
def change_password():
    """ログイン中ユーザの現行 / 新規パスワードを受け取り、ハッシュを更新する。

    既存 `/api/users/me` と同様に `Authorization: Bearer <JWT>` を検証し、
    `current_password` が現在の保存ハッシュと一致した場合のみ `new_password`
    のハッシュで上書きする。`new_password` には登録時と同じ最低長
    (`MIN_PASSWORD_LENGTH`) を要求し、変更前と完全一致する `new_password` は
    400 で明示拒否する（誤操作・無意味なリセットを早期にユーザへ伝えるため）。

    返り値は `{"updated": true}` のみで、パスワード本体はログにもレスポンスにも
    含めない。既存 JWT は失効させない（本サービスにトークン無効化機構が無く、
    必要なら refresh エンドポイント追加を別 Issue で扱う）。
    """
    auth_header = request.headers.get("Authorization", "")
    if not auth_header.startswith("Bearer "):
        return jsonify({"error": "Authorization header required"}), 401

    try:
        payload = jwt.decode(auth_header[7:], SECRET_KEY, algorithms=["HS256"])
    except jwt.ExpiredSignatureError:
        return jsonify({"error": "Token expired"}), 401
    except jwt.InvalidTokenError:
        return jsonify({"error": "Invalid token"}), 401

    data = request.get_json(silent=True)
    if not data:
        return jsonify({"error": "current_password and new_password are required"}), 400

    current_raw = data.get("current_password")
    new_raw = data.get("new_password")
    if current_raw is None or new_raw is None:
        return jsonify({"error": "current_password and new_password are required"}), 400
    if not isinstance(current_raw, str) or not isinstance(new_raw, str):
        return jsonify({"error": "current_password and new_password must be strings"}), 400

    email = payload.get("email")
    user = users_db.get(email)
    if not user:
        # トークンは valid だが、登録ユーザが削除されている等の状況。
        logger.warning("Password change for missing user: %s", email)
        return jsonify({"error": "User not found"}), 404

    if user["password"] != hash_password(current_raw):
        logger.warning("Password change with wrong current password: %s", email)
        return jsonify({"error": "Current password is incorrect"}), 401

    if len(new_raw) < MIN_PASSWORD_LENGTH:
        return jsonify(
            {"error": f"new_password must be at least {MIN_PASSWORD_LENGTH} characters"},
        ), 400

    if new_raw == current_raw:
        return jsonify({"error": "new_password must differ from current_password"}), 400

    user["password"] = hash_password(new_raw)
    logger.info("User changed password: %s", email)
    return jsonify({"updated": True})


@app.route("/api/users/me", methods=["PATCH"])
def update_current_user():
    """ログイン中ユーザのプロフィール (現状 `name` のみ) を部分更新する。

    `Authorization: Bearer <JWT>` を検証した上で、JSON ボディの `name` を
    新しい表示名として保存する。`name` 以外のフィールドは無視する（将来の
    属性追加に備えて余計なキーは黙って破棄）。

    バリデーション規則:
    - `name` は string（数値・配列・オブジェクト等は 400）
    - 前後空白は登録時と同じく `strip()` で除去
    - 空文字も許可（表示名なしの状態に戻したいケース）
    - 長さは登録時と同じ `MAX_NAME_LENGTH` を上限

    戻り値は `GET /api/users/me` と同形 (`id` / `email` / `name` / `created_at`) で、
    更新後の状態をエコーする。
    """
    auth_header = request.headers.get("Authorization", "")
    if not auth_header.startswith("Bearer "):
        return jsonify({"error": "Authorization header required"}), 401

    try:
        payload = jwt.decode(auth_header[7:], SECRET_KEY, algorithms=["HS256"])
    except jwt.ExpiredSignatureError:
        return jsonify({"error": "Token expired"}), 401
    except jwt.InvalidTokenError:
        return jsonify({"error": "Invalid token"}), 401

    data = request.get_json(silent=True)
    if data is None or not isinstance(data, dict):
        return jsonify({"error": "Request body must be a JSON object"}), 400
    if "name" not in data:
        return jsonify({"error": "name is required"}), 400

    name_raw = data.get("name")
    if not isinstance(name_raw, str):
        return jsonify({"error": "Name must be a string"}), 400
    name = name_raw.strip()
    if len(name) > MAX_NAME_LENGTH:
        return jsonify({"error": f"Name must be at most {MAX_NAME_LENGTH} characters"}), 400

    email = payload.get("email")
    user = users_db.get(email)
    if not user:
        logger.warning("Profile update for missing user: %s", email)
        return jsonify({"error": "User not found"}), 404

    user["name"] = name
    logger.info("User updated profile: %s (name=%r)", email, name)
    return jsonify({
        "id": user["id"],
        "email": user["email"],
        "name": user["name"],
        "created_at": user["created_at"],
    })


@app.route("/api/users/me", methods=["DELETE"])
def delete_current_user():
    """ログイン中ユーザ自身のアカウントを退会（削除）する。

    `Authorization: Bearer <JWT>` を検証し、JSON ボディの `current_password` が
    現行ハッシュと一致した場合のみ `users_db` からエントリを削除する。
    トークンが漏洩した状況での誤削除・第三者削除を避けるため、パスワード再入力を
    明示的に必須化している（`change_password` と同じ二段階確認パターン）。

    削除後は同じメールアドレスでの `/api/users/register` が改めて成功する。
    既存 JWT の失効機構は本サービスに無いため、削除済みユーザのトークンは
    自然失効まで形式上有効だが、`/me` 系の各エンドポイントは `users_db.get`
    で 404 を返すため実害は無い（`change_password` と同じセマンティクス）。

    レスポンスは `{"deleted": true}` のみで、ユーザ識別子等は含めない。
    """
    auth_header = request.headers.get("Authorization", "")
    if not auth_header.startswith("Bearer "):
        return jsonify({"error": "Authorization header required"}), 401

    try:
        payload = jwt.decode(auth_header[7:], SECRET_KEY, algorithms=["HS256"])
    except jwt.ExpiredSignatureError:
        return jsonify({"error": "Token expired"}), 401
    except jwt.InvalidTokenError:
        return jsonify({"error": "Invalid token"}), 401

    data = request.get_json(silent=True)
    if data is None or not isinstance(data, dict):
        return jsonify({"error": "current_password is required"}), 400

    current_raw = data.get("current_password")
    if current_raw is None:
        return jsonify({"error": "current_password is required"}), 400
    if not isinstance(current_raw, str):
        return jsonify({"error": "current_password must be a string"}), 400

    email = payload.get("email")
    user = users_db.get(email)
    if not user:
        # トークン自体は valid だが対象ユーザが既に削除済みのケース（idempotent）。
        # 二重 DELETE で 500 にしないために 404 で明示する。
        logger.warning("Account deletion for missing user: %s", email)
        return jsonify({"error": "User not found"}), 404

    if user["password"] != hash_password(current_raw):
        logger.warning("Account deletion with wrong current password: %s", email)
        return jsonify({"error": "Current password is incorrect"}), 401

    del users_db[email]
    logger.info("User deleted account: %s", email)
    return jsonify({"deleted": True})


@app.route("/api/users/count", methods=["GET"])
def count_users():
    """フィルタ後のユーザ件数と最古／最新の登録時刻のみを返す軽量エンドポイント。

    `GET /api/users` はページングされたユーザ一覧を返すが、UI で「総数バッジ」や
    「登録期間の表示」だけを出したい場合、`limit=1` で叩いて `total` を読み取る運用は
    レスポンスにユーザ本体が含まれるため帯域・JSON サイズが無駄になる。本エンドポイントは
    `users` 配列を一切返さず、`total` / `oldest_created_at` / `newest_created_at` のみ
    返す。フィルタは `GET /api/users` と同じ `q` / `since` / `until` を流用する。

    Sort / pagination パラメータは無視する（count は順序・ページに依存しない）。
    フィルタ後 0 件の場合、`oldest_created_at` と `newest_created_at` は `null`。
    """
    q, q_err = _normalize_q(request.args.get("q"))
    if q_err is not None:
        logger.warning("Invalid q on count: %s", q_err)
        return jsonify({"error": q_err}), 400

    since_dt, since_err = _parse_iso_datetime(request.args.get("since"), "since")
    if since_err is not None:
        logger.warning("Invalid since on count: %s", since_err)
        return jsonify({"error": since_err}), 400
    until_dt, until_err = _parse_iso_datetime(request.args.get("until"), "until")
    if until_err is not None:
        logger.warning("Invalid until on count: %s", until_err)
        return jsonify({"error": until_err}), 400
    if since_dt is not None and until_dt is not None and since_dt > until_dt:
        logger.warning(
            "Invalid range on count: since=%s > until=%s",
            request.args.get("since"), request.args.get("until"),
        )
        return jsonify({"error": "since must be less than or equal to until"}), 400

    total = 0
    oldest_created_at = None
    newest_created_at = None
    for u in users_db.values():
        if q is not None:
            email_l = u.get("email", "").lower()
            name_l = (u.get("name") or "").lower()
            if q not in email_l and q not in name_l:
                continue
        created_raw = u.get("created_at") if isinstance(u.get("created_at"), str) else None
        if since_dt is not None or until_dt is not None:
            if created_raw is None:
                continue
            try:
                created_dt = datetime.fromisoformat(created_raw)
            except ValueError:
                continue
            if created_dt.tzinfo is None:
                created_dt = created_dt.replace(tzinfo=timezone.utc)
            if since_dt is not None and created_dt < since_dt:
                continue
            if until_dt is not None and created_dt > until_dt:
                continue

        total += 1
        # ISO 8601 文字列同士の lex 比較は UTC 統一前提で正しく動く
        # （register / update では `datetime.now(timezone.utc).isoformat()` で固定書込）。
        if created_raw is not None:
            if oldest_created_at is None or created_raw < oldest_created_at:
                oldest_created_at = created_raw
            if newest_created_at is None or created_raw > newest_created_at:
                newest_created_at = created_raw

    since_raw = request.args.get("since")
    until_raw = request.args.get("until")
    logger.info(
        "Count users: total=%d (q=%s since=%s until=%s)",
        total, q, since_raw, until_raw,
    )
    resp = {
        "total": total,
        "oldest_created_at": oldest_created_at,
        "newest_created_at": newest_created_at,
    }
    if since_raw is not None and since_raw.strip():
        resp["since"] = since_raw
    if until_raw is not None and until_raw.strip():
        resp["until"] = until_raw
    return jsonify(resp)


@app.route("/api/users/by_domain", methods=["GET"])
def users_by_domain():
    """メールアドレスのドメイン (`@` 以降) 別のユーザ集計を返す。

    `GET /api/users/count` が「総数だけ」を返すのに対し、本エンドポイントは
    ドメイン別の内訳を `count` 降順 (同 count はドメイン名昇順) で返す軽量集計。
    `/api/users` で全件取得してフロント側で抽出する運用に比べて、ペイロードと
    JSON エンコード時間を大幅に削減できる。

    フィルタは `/api/users/count` と同じ `q` / `since` / `until` を流用。
    Sort / pagination パラメータは無視する（集計結果は順序に依存しない）。

    メールアドレスに `@` が含まれない壊れたユーザは `unknown` ドメインに
    フォールバック集計する（運用上はデータ品質の指標になる）。
    """
    q, q_err = _normalize_q(request.args.get("q"))
    if q_err is not None:
        logger.warning("Invalid q on by_domain: %s", q_err)
        return jsonify({"error": q_err}), 400

    since_dt, since_err = _parse_iso_datetime(request.args.get("since"), "since")
    if since_err is not None:
        logger.warning("Invalid since on by_domain: %s", since_err)
        return jsonify({"error": since_err}), 400
    until_dt, until_err = _parse_iso_datetime(request.args.get("until"), "until")
    if until_err is not None:
        logger.warning("Invalid until on by_domain: %s", until_err)
        return jsonify({"error": until_err}), 400
    if since_dt is not None and until_dt is not None and since_dt > until_dt:
        logger.warning(
            "Invalid range on by_domain: since=%s > until=%s",
            request.args.get("since"), request.args.get("until"),
        )
        return jsonify({"error": "since must be less than or equal to until"}), 400

    counts: dict[str, int] = {}
    total = 0
    for u in users_db.values():
        if q is not None:
            email_l = u.get("email", "").lower()
            name_l = (u.get("name") or "").lower()
            if q not in email_l and q not in name_l:
                continue
        created_raw = u.get("created_at") if isinstance(u.get("created_at"), str) else None
        if since_dt is not None or until_dt is not None:
            if created_raw is None:
                continue
            try:
                created_dt = datetime.fromisoformat(created_raw)
            except ValueError:
                continue
            if created_dt.tzinfo is None:
                created_dt = created_dt.replace(tzinfo=timezone.utc)
            if since_dt is not None and created_dt < since_dt:
                continue
            if until_dt is not None and created_dt > until_dt:
                continue

        email = u.get("email", "")
        if isinstance(email, str) and "@" in email:
            # `normalize_email` と同じく lower-case で集計するため、`@` 以降は
            # 一度 lower してから取り出す（register 時点で正規化されている前提だが保険）。
            domain = email.lower().rsplit("@", 1)[1]
        else:
            domain = "unknown"
        counts[domain] = counts.get(domain, 0) + 1
        total += 1

    # count 降順、同 count はドメイン名昇順。
    sorted_items = sorted(counts.items(), key=lambda kv: (-kv[1], kv[0]))
    by_domain = [{"domain": d, "count": c} for d, c in sorted_items]

    since_raw = request.args.get("since")
    until_raw = request.args.get("until")
    logger.info(
        "Users by domain: total=%d distinct=%d (q=%s since=%s until=%s)",
        total, len(by_domain), q, since_raw, until_raw,
    )
    resp = {
        "total": total,
        "distinct_domains": len(by_domain),
        "by_domain": by_domain,
    }
    if since_raw is not None and since_raw.strip():
        resp["since"] = since_raw
    if until_raw is not None and until_raw.strip():
        resp["until"] = until_raw
    return jsonify(resp)


@app.route("/api/users", methods=["GET"])
def list_users():
    limit = _parse_pagination_param(request.args.get("limit"), USERS_DEFAULT_LIMIT, 1, USERS_MAX_LIMIT)
    if limit is None:
        logger.warning("Invalid limit: %s", request.args.get("limit"))
        return jsonify({"error": f"limit must be an integer between 1 and {USERS_MAX_LIMIT}"}), 400
    offset = _parse_pagination_param(request.args.get("offset"), 0, 0, MAX_OFFSET)
    if offset is None:
        logger.warning("Invalid offset: %s", request.args.get("offset"))
        return jsonify({"error": "offset must be a non-negative integer"}), 400

    q, q_err = _normalize_q(request.args.get("q"))
    if q_err is not None:
        logger.warning("Invalid q: %s", q_err)
        return jsonify({"error": q_err}), 400

    since_dt, since_err = _parse_iso_datetime(request.args.get("since"), "since")
    if since_err is not None:
        logger.warning("Invalid since: %s", since_err)
        return jsonify({"error": since_err}), 400
    until_dt, until_err = _parse_iso_datetime(request.args.get("until"), "until")
    if until_err is not None:
        logger.warning("Invalid until: %s", until_err)
        return jsonify({"error": until_err}), 400
    if since_dt is not None and until_dt is not None and since_dt > until_dt:
        logger.warning(
            "Invalid range: since=%s > until=%s",
            request.args.get("since"), request.args.get("until"),
        )
        return jsonify({"error": "since must be less than or equal to until"}), 400

    sort_field = request.args.get("sort", "created_at")
    if sort_field not in ALLOWED_USER_SORT_FIELDS:
        logger.warning("Invalid sort field: %s", sort_field)
        return jsonify({
            "error": f"sort must be one of: {', '.join(sorted(ALLOWED_USER_SORT_FIELDS))}",
        }), 400
    sort_order = request.args.get("order", "asc")
    if sort_order not in ALLOWED_SORT_ORDERS:
        logger.warning("Invalid sort order: %s", sort_order)
        return jsonify({
            "error": f"order must be one of: {', '.join(sorted(ALLOWED_SORT_ORDERS))}",
        }), 400

    matched = []
    for u in users_db.values():
        if q is not None:
            email_l = u.get("email", "").lower()
            name_l = (u.get("name") or "").lower()
            if q not in email_l and q not in name_l:
                continue
        if since_dt is not None or until_dt is not None:
            created_raw = u.get("created_at")
            if not isinstance(created_raw, str):
                # 壊れた created_at は時間フィルタ指定時に取り除く（保険）
                continue
            try:
                created_dt = datetime.fromisoformat(created_raw)
            except ValueError:
                continue
            if created_dt.tzinfo is None:
                created_dt = created_dt.replace(tzinfo=timezone.utc)
            if since_dt is not None and created_dt < since_dt:
                continue
            if until_dt is not None and created_dt > until_dt:
                continue
        matched.append(u)

    reverse = sort_order == "desc"
    matched.sort(key=lambda u: u.get(sort_field, ""), reverse=reverse)

    total = len(matched)
    page_records = matched[offset:offset + limit]
    page = [
        {
            "id": u["id"],
            "email": u["email"],
            "name": u["name"],
            "created_at": u["created_at"],
        }
        for u in page_records
    ]
    since_raw = request.args.get("since")
    until_raw = request.args.get("until")
    logger.info(
        "Listing users: %d returned (total=%d limit=%d offset=%d q=%s sort=%s order=%s since=%s until=%s)",
        len(page), total, limit, offset, q, sort_field, sort_order, since_raw, until_raw,
    )
    resp = {
        "users": page,
        "count": len(page),
        "total": total,
        "limit": limit,
        "offset": offset,
        "sort": sort_field,
        "order": sort_order,
    }
    # 指定されたときだけエコーする（未指定時の互換性のため None フィールドは含めない）
    if since_raw is not None and since_raw.strip():
        resp["since"] = since_raw
    if until_raw is not None and until_raw.strip():
        resp["until"] = until_raw
    return jsonify(resp)


@app.route("/api/users/signups_by_day", methods=["GET"])
def users_signups_by_day():
    """登録日 (YYYY-MM-DD, UTC) 別の新規ユーザ登録件数を時系列で返す。

    `/api/users/by_domain` がドメイン別の内訳を返すのと対になり、本エンドポイントは
    時系列の内訳を返す。UI のユーザ成長グラフ用途を意図しており、`/api/users` で
    全件取得してフロント側でビニングする運用に比べてペイロード・JSON エンコード時間を
    大幅に削減できる。

    フィルタは `/api/users/count` / `/api/users/by_domain` と同じ `q` / `since` / `until`。
    Sort / pagination パラメータは無視する。

    `by_day` は登録日の昇順で固定する（時系列グラフへそのまま流し込めるように）。
    `created_at` が壊れている／パースできないユーザは `unknown` 日として
    フォールバック集計する（`by_domain` の `unknown` ドメインと同じ思想）。
    """
    q, q_err = _normalize_q(request.args.get("q"))
    if q_err is not None:
        logger.warning("Invalid q on signups_by_day: %s", q_err)
        return jsonify({"error": q_err}), 400

    since_dt, since_err = _parse_iso_datetime(request.args.get("since"), "since")
    if since_err is not None:
        logger.warning("Invalid since on signups_by_day: %s", since_err)
        return jsonify({"error": since_err}), 400
    until_dt, until_err = _parse_iso_datetime(request.args.get("until"), "until")
    if until_err is not None:
        logger.warning("Invalid until on signups_by_day: %s", until_err)
        return jsonify({"error": until_err}), 400
    if since_dt is not None and until_dt is not None and since_dt > until_dt:
        logger.warning(
            "Invalid range on signups_by_day: since=%s > until=%s",
            request.args.get("since"), request.args.get("until"),
        )
        return jsonify({"error": "since must be less than or equal to until"}), 400

    counts: dict[str, int] = {}
    total = 0
    for u in users_db.values():
        if q is not None:
            email_l = u.get("email", "").lower()
            name_l = (u.get("name") or "").lower()
            if q not in email_l and q not in name_l:
                continue
        created_raw = u.get("created_at") if isinstance(u.get("created_at"), str) else None
        if since_dt is not None or until_dt is not None:
            if created_raw is None:
                continue
            try:
                created_dt_filter = datetime.fromisoformat(created_raw)
            except ValueError:
                continue
            if created_dt_filter.tzinfo is None:
                created_dt_filter = created_dt_filter.replace(tzinfo=timezone.utc)
            if since_dt is not None and created_dt_filter < since_dt:
                continue
            if until_dt is not None and created_dt_filter > until_dt:
                continue

        if created_raw is None:
            day = "unknown"
        else:
            try:
                created_dt = datetime.fromisoformat(created_raw)
            except ValueError:
                day = "unknown"
            else:
                if created_dt.tzinfo is None:
                    created_dt = created_dt.replace(tzinfo=timezone.utc)
                # UTC で正規化してから日付を取り出す。registers の created_at は
                # `datetime.now(timezone.utc).isoformat()` で書かれる前提だが、
                # 他 TZ 由来のデータが混じっても UTC へ変換してビニングする。
                day = created_dt.astimezone(timezone.utc).date().isoformat()
        counts[day] = counts.get(day, 0) + 1
        total += 1

    # 日付昇順で固定（時系列グラフへそのまま流せるように）。
    # `unknown` は ISO 日付 (`YYYY-MM-DD`) の lex 範囲 ("0-9") の後に来るため、
    # 自然に末尾に並ぶ（時系列軸上は「不明分」として独立表示できる）。
    sorted_items = sorted(counts.items(), key=lambda kv: kv[0])
    by_day = [{"day": d, "count": c} for d, c in sorted_items]

    since_raw = request.args.get("since")
    until_raw = request.args.get("until")
    logger.info(
        "Users signups by day: total=%d distinct=%d (q=%s since=%s until=%s)",
        total, len(by_day), q, since_raw, until_raw,
    )
    resp = {
        "total": total,
        "distinct_days": len(by_day),
        "by_day": by_day,
    }
    if since_raw is not None and since_raw.strip():
        resp["since"] = since_raw
    if until_raw is not None and until_raw.strip():
        resp["until"] = until_raw
    return jsonify(resp)


if __name__ == "__main__":
    port = int(os.getenv("USER_API_PORT", "5001"))
    logger.info("Starting user-api on port %d", port)
    app.run(host="0.0.0.0", port=port)
