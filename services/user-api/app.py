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

    if email in users_db:
        logger.warning("Registration attempt with existing email: %s", email)
        return jsonify({"error": "User already exists"}), 409

    user_id = str(uuid.uuid4())
    users_db[email] = {
        "id": user_id,
        "email": email,
        "password": hash_password(password),
        "name": data.get("name", ""),
        "created_at": datetime.now(timezone.utc).isoformat(),
    }
    logger.info("User registered: %s", email)
    return jsonify({"id": user_id, "email": email, "name": data.get("name", "")}), 201


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
    logger.info(
        "Listing users: %d returned (total=%d limit=%d offset=%d q=%s sort=%s order=%s)",
        len(page), total, limit, offset, q, sort_field, sort_order,
    )
    return jsonify({
        "users": page,
        "count": len(page),
        "total": total,
        "limit": limit,
        "offset": offset,
        "sort": sort_field,
        "order": sort_order,
    })


if __name__ == "__main__":
    port = int(os.getenv("USER_API_PORT", "5001"))
    logger.info("Starting user-api on port %d", port)
    app.run(host="0.0.0.0", port=port)
