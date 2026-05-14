import os
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

users_db: dict[str, dict] = {}


def hash_password(password: str) -> str:
    return hashlib.sha256(password.encode()).hexdigest()


@app.route("/health", methods=["GET"])
def health():
    logger.info("Health check requested")
    return jsonify({"status": "healthy", "service": "user-api", "timestamp": datetime.now(timezone.utc).isoformat()})


@app.route("/api/users/register", methods=["POST"])
def register():
    data = request.get_json()
    if not data or not data.get("email") or not data.get("password"):
        logger.warning("Registration attempt with missing fields")
        return jsonify({"error": "Email and password are required"}), 400

    email = data["email"]
    if email in users_db:
        logger.warning("Registration attempt with existing email: %s", email)
        return jsonify({"error": "User already exists"}), 409

    user_id = str(uuid.uuid4())
    users_db[email] = {
        "id": user_id,
        "email": email,
        "password": hash_password(data["password"]),
        "name": data.get("name", ""),
        "created_at": datetime.now(timezone.utc).isoformat(),
    }
    logger.info("User registered: %s", email)
    return jsonify({"id": user_id, "email": email, "name": data.get("name", "")}), 201


@app.route("/api/users/login", methods=["POST"])
def login():
    data = request.get_json()
    if not data or not data.get("email") or not data.get("password"):
        logger.warning("Login attempt with missing fields")
        return jsonify({"error": "Email and password are required"}), 400

    email = data["email"]
    user = users_db.get(email)
    if not user or user["password"] != hash_password(data["password"]):
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
    logger.info("Listing all users")
    return jsonify([{"id": u["id"], "email": u["email"], "name": u["name"]} for u in users_db.values()])


if __name__ == "__main__":
    port = int(os.getenv("USER_API_PORT", "5001"))
    logger.info("Starting user-api on port %d", port)
    app.run(host="0.0.0.0", port=port)
