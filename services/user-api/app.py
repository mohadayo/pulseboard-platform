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

    all_users = [{"id": u["id"], "email": u["email"], "name": u["name"]} for u in users_db.values()]
    page = all_users[offset:offset + limit]
    logger.info("Listing users: %d returned (total=%d limit=%d offset=%d)", len(page), len(all_users), limit, offset)
    return jsonify(page)


if __name__ == "__main__":
    port = int(os.getenv("USER_API_PORT", "5001"))
    logger.info("Starting user-api on port %d", port)
    app.run(host="0.0.0.0", port=port)
