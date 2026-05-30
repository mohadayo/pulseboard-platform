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
    assert data["total"] == 2
    assert data["count"] == 2
    assert len(data["users"]) == 2
    assert data["sort"] == "created_at"
    assert data["order"] == "asc"
    # created_at が返却に含まれること（pagination で並び替え可能なため）
    assert all("created_at" in u for u in data["users"])


def test_register_normalizes_email_case(client):
    resp = client.post("/api/users/register", json={"email": "Alice@Example.com", "password": "pass123"})
    assert resp.status_code == 201
    assert resp.get_json()["email"] == "alice@example.com"


def test_register_normalizes_email_whitespace(client):
    resp = client.post("/api/users/register", json={"email": "  bob@example.com  ", "password": "pass123"})
    assert resp.status_code == 201
    assert resp.get_json()["email"] == "bob@example.com"


def test_register_rejects_case_duplicate(client):
    client.post("/api/users/register", json={"email": "user@example.com", "password": "pass123"})
    resp = client.post("/api/users/register", json={"email": "USER@EXAMPLE.COM", "password": "pass456"})
    assert resp.status_code == 409


def test_login_is_case_insensitive(client):
    client.post("/api/users/register", json={"email": "user@example.com", "password": "pass123"})
    resp = client.post("/api/users/login", json={"email": "User@Example.com", "password": "pass123"})
    assert resp.status_code == 200
    assert "token" in resp.get_json()


def test_register_invalid_email_format(client):
    resp = client.post("/api/users/register", json={"email": "not-an-email", "password": "pass123"})
    assert resp.status_code == 400


def test_register_blank_email(client):
    resp = client.post("/api/users/register", json={"email": "   ", "password": "pass123"})
    assert resp.status_code == 400


def test_register_password_too_short(client):
    resp = client.post("/api/users/register", json={"email": "short@example.com", "password": "x"})
    assert resp.status_code == 400


def test_register_non_string_email(client):
    resp = client.post("/api/users/register", json={"email": 123, "password": "pass123"})
    assert resp.status_code == 400


def test_list_users_pagination(client):
    for i in range(5):
        client.post("/api/users/register", json={"email": f"u{i}@example.com", "password": "pass123"})
    resp = client.get("/api/users?limit=2&offset=1")
    assert resp.status_code == 200
    data = resp.get_json()
    assert len(data["users"]) == 2
    assert data["count"] == 2
    assert data["total"] == 5
    assert data["limit"] == 2
    assert data["offset"] == 1


def test_list_users_q_filters_by_email(client):
    client.post("/api/users/register", json={"email": "alice@example.com", "password": "pass123", "name": "Alice"})
    client.post("/api/users/register", json={"email": "bob@example.com", "password": "pass123", "name": "Bob"})
    client.post("/api/users/register", json={"email": "carol@other.org", "password": "pass123", "name": "Carol"})
    resp = client.get("/api/users?q=example.com")
    assert resp.status_code == 200
    data = resp.get_json()
    assert data["total"] == 2
    emails = sorted(u["email"] for u in data["users"])
    assert emails == ["alice@example.com", "bob@example.com"]


def test_list_users_q_filters_by_name_case_insensitive(client):
    client.post("/api/users/register", json={"email": "a@example.com", "password": "pass123", "name": "Alice"})
    client.post("/api/users/register", json={"email": "b@example.com", "password": "pass123", "name": "alfred"})
    client.post("/api/users/register", json={"email": "c@example.com", "password": "pass123", "name": "Bob"})
    resp = client.get("/api/users?q=AL")
    assert resp.status_code == 200
    data = resp.get_json()
    assert data["total"] == 2


def test_list_users_q_blank_is_ignored(client):
    client.post("/api/users/register", json={"email": "a@example.com", "password": "pass123", "name": "Alice"})
    resp = client.get("/api/users?q=   ")
    assert resp.status_code == 200
    data = resp.get_json()
    assert data["total"] == 1


def test_list_users_q_too_long(client):
    resp = client.get("/api/users?q=" + "x" * 9999)
    assert resp.status_code == 400


def test_list_users_sort_email_asc(client):
    client.post("/api/users/register", json={"email": "c@example.com", "password": "pass123", "name": "Carol"})
    client.post("/api/users/register", json={"email": "a@example.com", "password": "pass123", "name": "Alice"})
    client.post("/api/users/register", json={"email": "b@example.com", "password": "pass123", "name": "Bob"})
    resp = client.get("/api/users?sort=email&order=asc")
    assert resp.status_code == 200
    data = resp.get_json()
    assert [u["email"] for u in data["users"]] == [
        "a@example.com",
        "b@example.com",
        "c@example.com",
    ]


def test_list_users_sort_name_desc(client):
    client.post("/api/users/register", json={"email": "a@example.com", "password": "pass123", "name": "Alice"})
    client.post("/api/users/register", json={"email": "b@example.com", "password": "pass123", "name": "Carol"})
    client.post("/api/users/register", json={"email": "c@example.com", "password": "pass123", "name": "Bob"})
    resp = client.get("/api/users?sort=name&order=desc")
    assert resp.status_code == 200
    data = resp.get_json()
    assert [u["name"] for u in data["users"]] == ["Carol", "Bob", "Alice"]


def test_list_users_invalid_sort_field(client):
    resp = client.get("/api/users?sort=password")
    assert resp.status_code == 400


def test_list_users_invalid_order(client):
    resp = client.get("/api/users?order=random")
    assert resp.status_code == 400


def test_list_users_q_and_sort_combine(client):
    client.post("/api/users/register", json={"email": "alice@example.com", "password": "pass123", "name": "Alice"})
    client.post("/api/users/register", json={"email": "bob@example.com", "password": "pass123", "name": "Bob"})
    client.post("/api/users/register", json={"email": "carol@other.org", "password": "pass123", "name": "Carol"})
    resp = client.get("/api/users?q=example.com&sort=email&order=desc")
    assert resp.status_code == 200
    data = resp.get_json()
    assert [u["email"] for u in data["users"]] == [
        "bob@example.com",
        "alice@example.com",
    ]


def test_list_users_invalid_limit(client):
    resp = client.get("/api/users?limit=abc")
    assert resp.status_code == 400


def test_list_users_limit_too_large(client):
    resp = client.get("/api/users?limit=99999")
    assert resp.status_code == 400


def test_list_users_negative_offset(client):
    resp = client.get("/api/users?offset=-1")
    assert resp.status_code == 400
