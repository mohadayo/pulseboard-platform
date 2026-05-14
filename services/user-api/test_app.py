import json
import pytest
from app import app, users_db


@pytest.fixture
def client():
    app.config["TESTING"] = True
    users_db.clear()
    with app.test_client() as c:
        yield c


def test_health(client):
    resp = client.get("/health")
    assert resp.status_code == 200
    data = resp.get_json()
    assert data["status"] == "healthy"
    assert data["service"] == "user-api"


def test_register_success(client):
    resp = client.post("/api/users/register", json={"email": "a@b.com", "password": "pass123", "name": "Alice"})
    assert resp.status_code == 201
    data = resp.get_json()
    assert data["email"] == "a@b.com"
    assert data["name"] == "Alice"
    assert "id" in data


def test_register_missing_fields(client):
    resp = client.post("/api/users/register", json={"email": "a@b.com"})
    assert resp.status_code == 400


def test_register_duplicate(client):
    client.post("/api/users/register", json={"email": "a@b.com", "password": "pass123"})
    resp = client.post("/api/users/register", json={"email": "a@b.com", "password": "pass456"})
    assert resp.status_code == 409


def test_login_success(client):
    client.post("/api/users/register", json={"email": "a@b.com", "password": "pass123"})
    resp = client.post("/api/users/login", json={"email": "a@b.com", "password": "pass123"})
    assert resp.status_code == 200
    data = resp.get_json()
    assert "token" in data
    assert "user_id" in data


def test_login_wrong_password(client):
    client.post("/api/users/register", json={"email": "a@b.com", "password": "pass123"})
    resp = client.post("/api/users/login", json={"email": "a@b.com", "password": "wrong"})
    assert resp.status_code == 401


def test_login_missing_fields(client):
    resp = client.post("/api/users/login", json={})
    assert resp.status_code == 400


def test_get_me(client):
    client.post("/api/users/register", json={"email": "a@b.com", "password": "pass123", "name": "Alice"})
    login_resp = client.post("/api/users/login", json={"email": "a@b.com", "password": "pass123"})
    token = login_resp.get_json()["token"]
    resp = client.get("/api/users/me", headers={"Authorization": f"Bearer {token}"})
    assert resp.status_code == 200
    data = resp.get_json()
    assert data["email"] == "a@b.com"
    assert data["name"] == "Alice"


def test_get_me_no_auth(client):
    resp = client.get("/api/users/me")
    assert resp.status_code == 401


def test_get_me_invalid_token(client):
    resp = client.get("/api/users/me", headers={"Authorization": "Bearer invalidtoken"})
    assert resp.status_code == 401


def test_list_users(client):
    client.post("/api/users/register", json={"email": "a@b.com", "password": "pass123", "name": "Alice"})
    client.post("/api/users/register", json={"email": "b@c.com", "password": "pass456", "name": "Bob"})
    resp = client.get("/api/users")
    assert resp.status_code == 200
    data = resp.get_json()
    assert len(data) == 2
