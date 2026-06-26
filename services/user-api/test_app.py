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


def _register_user_with_created_at(email: str, created_at: str) -> None:
    """`users_db` 直接書き込みで `created_at` を制御するヘルパ。

    POST /api/users/register は `created_at` を `now(UTC)` で書き込むため、
    過去日時のレコードを再現したい時系列フィルタテストではここで上書きする。
    """
    users_db[email] = {
        "id": f"id-{email}",
        "email": email,
        "password": "x",
        "name": email.split("@")[0].capitalize(),
        "created_at": created_at,
    }


def test_list_users_filters_by_since(client):
    _register_user_with_created_at("old@example.com", "2024-01-01T00:00:00+00:00")
    _register_user_with_created_at("new@example.com", "2024-06-01T00:00:00+00:00")

    resp = client.get("/api/users?since=2024-03-01T00:00:00Z")
    assert resp.status_code == 200
    data = resp.get_json()
    emails = sorted(u["email"] for u in data["users"])
    assert emails == ["new@example.com"]
    assert data["total"] == 1
    assert data["since"] == "2024-03-01T00:00:00Z"


def test_list_users_filters_by_until(client):
    _register_user_with_created_at("old@example.com", "2024-01-01T00:00:00+00:00")
    _register_user_with_created_at("new@example.com", "2024-06-01T00:00:00+00:00")

    resp = client.get("/api/users?until=2024-03-01T00:00:00Z")
    assert resp.status_code == 200
    data = resp.get_json()
    emails = sorted(u["email"] for u in data["users"])
    assert emails == ["old@example.com"]
    assert data["total"] == 1
    assert data["until"] == "2024-03-01T00:00:00Z"


def test_list_users_filters_by_since_and_until(client):
    _register_user_with_created_at("a@example.com", "2024-01-01T00:00:00+00:00")
    _register_user_with_created_at("b@example.com", "2024-06-15T00:00:00+00:00")
    _register_user_with_created_at("c@example.com", "2024-12-01T00:00:00+00:00")

    resp = client.get(
        "/api/users?since=2024-03-01T00:00:00Z&until=2024-09-01T00:00:00Z",
    )
    assert resp.status_code == 200
    data = resp.get_json()
    emails = sorted(u["email"] for u in data["users"])
    assert emails == ["b@example.com"]
    assert data["since"] == "2024-03-01T00:00:00Z"
    assert data["until"] == "2024-09-01T00:00:00Z"


def test_list_users_since_combines_with_q(client):
    _register_user_with_created_at("old.web@example.com", "2024-01-01T00:00:00+00:00")
    _register_user_with_created_at("new.web@example.com", "2024-12-01T00:00:00+00:00")
    _register_user_with_created_at("new.db@example.com", "2024-12-01T00:00:00+00:00")

    resp = client.get("/api/users?since=2024-06-01T00:00:00Z&q=web")
    assert resp.status_code == 200
    data = resp.get_json()
    emails = sorted(u["email"] for u in data["users"])
    assert emails == ["new.web@example.com"]


def test_list_users_invalid_since(client):
    resp = client.get("/api/users?since=not-a-date")
    assert resp.status_code == 400
    assert "since" in resp.get_json()["error"]


def test_list_users_invalid_until(client):
    resp = client.get("/api/users?until=2024-13-99")
    assert resp.status_code == 400
    assert "until" in resp.get_json()["error"]


def test_list_users_since_after_until_is_400(client):
    resp = client.get(
        "/api/users?since=2024-09-01T00:00:00Z&until=2024-03-01T00:00:00Z",
    )
    assert resp.status_code == 400
    assert "less than or equal" in resp.get_json()["error"]


def test_list_users_since_without_timezone_treated_as_utc(client):
    _register_user_with_created_at("a@example.com", "2024-06-15T00:00:00+00:00")
    # タイムゾーン無指定は UTC として扱われ、2024-01-01 以降は a を含む
    resp = client.get("/api/users?since=2024-01-01T00:00:00")
    assert resp.status_code == 200
    data = resp.get_json()
    emails = sorted(u["email"] for u in data["users"])
    assert emails == ["a@example.com"]


def test_list_users_does_not_echo_since_when_unspecified(client):
    # 未指定なら since / until フィールドはレスポンスに含めない（互換性のため）
    resp = client.get("/api/users")
    assert resp.status_code == 200
    data = resp.get_json()
    assert "since" not in data
    assert "until" not in data


def test_list_users_blank_since_is_ignored(client):
    # 空白のみの since はフィルタ無効（後方互換）
    _register_user_with_created_at("a@example.com", "2024-01-01T00:00:00+00:00")
    resp = client.get("/api/users?since=%20%20")
    assert resp.status_code == 200
    data = resp.get_json()
    assert data["total"] == 1


def test_register_rejects_non_string_name(client):
    # name に数値を渡しても 201 で受理されると、後段の GET /api/users が
    # `name.lower()` で 500 する。明示的に 400 で拒否する。
    resp = client.post(
        "/api/users/register",
        json={"email": "a@b.com", "password": "pass123", "name": 42},
    )
    assert resp.status_code == 400
    assert "Name" in resp.get_json()["error"]


def test_register_rejects_list_name(client):
    resp = client.post(
        "/api/users/register",
        json={"email": "a@b.com", "password": "pass123", "name": ["A", "B"]},
    )
    assert resp.status_code == 400


def test_register_rejects_dict_name(client):
    resp = client.post(
        "/api/users/register",
        json={"email": "a@b.com", "password": "pass123", "name": {"first": "A"}},
    )
    assert resp.status_code == 400


def test_register_rejects_name_too_long(client):
    # 既定 MAX_NAME_LENGTH=100。境界として 101 文字は弾かれる。
    long_name = "a" * 101
    resp = client.post(
        "/api/users/register",
        json={"email": "a@b.com", "password": "pass123", "name": long_name},
    )
    assert resp.status_code == 400
    assert "100" in resp.get_json()["error"]


def test_register_accepts_max_length_name(client):
    # 境界値 100 文字は受理されること（境界の上側だけ弾く）。
    name = "a" * 100
    resp = client.post(
        "/api/users/register",
        json={"email": "a@b.com", "password": "pass123", "name": name},
    )
    assert resp.status_code == 201
    assert resp.get_json()["name"] == name


def test_register_trims_name_whitespace(client):
    # 前後空白は除去して保存される。
    resp = client.post(
        "/api/users/register",
        json={"email": "a@b.com", "password": "pass123", "name": "  Alice  "},
    )
    assert resp.status_code == 201
    assert resp.get_json()["name"] == "Alice"


def test_register_accepts_null_name_as_empty(client):
    # JSON null は "" 扱い（既存挙動：name 未指定と同等）。
    resp = client.post(
        "/api/users/register",
        json={"email": "a@b.com", "password": "pass123", "name": None},
    )
    assert resp.status_code == 201
    assert resp.get_json()["name"] == ""


def test_list_users_with_q_does_not_500_after_register(client):
    # 回帰テスト：name 検証が無いと、name=int で登録 → q 検索が 500 になる。
    # 修正後は登録自体が 400 で弾かれるので、後段の検索は安全に動く。
    client.post(
        "/api/users/register",
        json={"email": "valid@example.com", "password": "pass123", "name": "Valid"},
    )
    # `name` を弾く例（先に投入した valid ユーザーには影響なし）
    bad = client.post(
        "/api/users/register",
        json={"email": "bad@example.com", "password": "pass123", "name": 42},
    )
    assert bad.status_code == 400

    resp = client.get("/api/users?q=val")
    assert resp.status_code == 200
    data = resp.get_json()
    assert data["total"] == 1
    assert data["users"][0]["email"] == "valid@example.com"


# ---------------------------------------------------------------------------
# POST /api/users/me/password — パスワード変更
# ---------------------------------------------------------------------------


def _register_and_login(client, email="a@b.com", password="pass123", name="Alice"):
    client.post("/api/users/register", json={
        "email": email, "password": password, "name": name,
    })
    resp = client.post("/api/users/login", json={"email": email, "password": password})
    return resp.get_json()["token"]


def test_change_password_success(client):
    token = _register_and_login(client)
    resp = client.post(
        "/api/users/me/password",
        headers={"Authorization": f"Bearer {token}"},
        json={"current_password": "pass123", "new_password": "newpass456"},
    )
    assert resp.status_code == 200
    assert resp.get_json() == {"updated": True}

    # 旧パスワードではログインできない
    bad = client.post("/api/users/login", json={"email": "a@b.com", "password": "pass123"})
    assert bad.status_code == 401
    # 新パスワードではログインできる
    ok = client.post("/api/users/login", json={"email": "a@b.com", "password": "newpass456"})
    assert ok.status_code == 200


def test_change_password_no_auth(client):
    resp = client.post(
        "/api/users/me/password",
        json={"current_password": "pass123", "new_password": "newpass456"},
    )
    assert resp.status_code == 401


def test_change_password_invalid_token(client):
    resp = client.post(
        "/api/users/me/password",
        headers={"Authorization": "Bearer invalidtoken"},
        json={"current_password": "pass123", "new_password": "newpass456"},
    )
    assert resp.status_code == 401


def test_change_password_missing_body(client):
    token = _register_and_login(client)
    resp = client.post(
        "/api/users/me/password",
        headers={"Authorization": f"Bearer {token}"},
    )
    assert resp.status_code == 400


def test_change_password_missing_current(client):
    token = _register_and_login(client)
    resp = client.post(
        "/api/users/me/password",
        headers={"Authorization": f"Bearer {token}"},
        json={"new_password": "newpass456"},
    )
    assert resp.status_code == 400


def test_change_password_missing_new(client):
    token = _register_and_login(client)
    resp = client.post(
        "/api/users/me/password",
        headers={"Authorization": f"Bearer {token}"},
        json={"current_password": "pass123"},
    )
    assert resp.status_code == 400


def test_change_password_non_string_current(client):
    token = _register_and_login(client)
    resp = client.post(
        "/api/users/me/password",
        headers={"Authorization": f"Bearer {token}"},
        json={"current_password": 12345, "new_password": "newpass456"},
    )
    assert resp.status_code == 400


def test_change_password_non_string_new(client):
    token = _register_and_login(client)
    resp = client.post(
        "/api/users/me/password",
        headers={"Authorization": f"Bearer {token}"},
        json={"current_password": "pass123", "new_password": 99999},
    )
    assert resp.status_code == 400


def test_change_password_wrong_current(client):
    token = _register_and_login(client)
    resp = client.post(
        "/api/users/me/password",
        headers={"Authorization": f"Bearer {token}"},
        json={"current_password": "wrongpass", "new_password": "newpass456"},
    )
    assert resp.status_code == 401
    assert "Current password" in resp.get_json()["error"]


def test_change_password_new_too_short(client):
    token = _register_and_login(client)
    # MIN_PASSWORD_LENGTH=6 が既定なので 5 文字以下は拒否される
    resp = client.post(
        "/api/users/me/password",
        headers={"Authorization": f"Bearer {token}"},
        json={"current_password": "pass123", "new_password": "abc"},
    )
    assert resp.status_code == 400
    assert "at least" in resp.get_json()["error"]


def test_change_password_new_same_as_current(client):
    token = _register_and_login(client)
    resp = client.post(
        "/api/users/me/password",
        headers={"Authorization": f"Bearer {token}"},
        json={"current_password": "pass123", "new_password": "pass123"},
    )
    assert resp.status_code == 400
    assert "differ" in resp.get_json()["error"]


def test_change_password_user_not_found(client):
    # 一度ログインしてトークンを得てから、users_db からユーザを削除する。
    token = _register_and_login(client)
    users_db.clear()
    resp = client.post(
        "/api/users/me/password",
        headers={"Authorization": f"Bearer {token}"},
        json={"current_password": "pass123", "new_password": "newpass456"},
    )
    assert resp.status_code == 404


def test_change_password_does_not_leak_password_in_response(client):
    token = _register_and_login(client)
    resp = client.post(
        "/api/users/me/password",
        headers={"Authorization": f"Bearer {token}"},
        json={"current_password": "pass123", "new_password": "newpass456"},
    )
    body = resp.get_json()
    # レスポンスに password / new_password などのフィールドが含まれていないこと
    assert "password" not in body
    assert "current_password" not in body
    assert "new_password" not in body
    assert "hash" not in body


# ---------------------------------------------------------------------------
# PATCH /api/users/me — プロフィール（name）更新
# ---------------------------------------------------------------------------


def test_update_me_success(client):
    token = _register_and_login(client, name="Alice")
    resp = client.patch(
        "/api/users/me",
        headers={"Authorization": f"Bearer {token}"},
        json={"name": "Alice Wonderland"},
    )
    assert resp.status_code == 200
    body = resp.get_json()
    assert body["email"] == "a@b.com"
    assert body["name"] == "Alice Wonderland"
    assert "id" in body
    assert "created_at" in body

    # 永続化されていること（再取得しても新しい name）
    me = client.get("/api/users/me", headers={"Authorization": f"Bearer {token}"})
    assert me.get_json()["name"] == "Alice Wonderland"


def test_update_me_strips_whitespace(client):
    token = _register_and_login(client)
    resp = client.patch(
        "/api/users/me",
        headers={"Authorization": f"Bearer {token}"},
        json={"name": "  Bob  "},
    )
    assert resp.status_code == 200
    assert resp.get_json()["name"] == "Bob"


def test_update_me_accepts_empty_name(client):
    # 表示名を消す UX を許可する（登録時も "" を許可しているため整合）
    token = _register_and_login(client, name="Alice")
    resp = client.patch(
        "/api/users/me",
        headers={"Authorization": f"Bearer {token}"},
        json={"name": ""},
    )
    assert resp.status_code == 200
    assert resp.get_json()["name"] == ""


def test_update_me_requires_auth(client):
    resp = client.patch("/api/users/me", json={"name": "x"})
    assert resp.status_code == 401


def test_update_me_invalid_token(client):
    resp = client.patch(
        "/api/users/me",
        headers={"Authorization": "Bearer invalidtoken"},
        json={"name": "x"},
    )
    assert resp.status_code == 401


def test_update_me_missing_body(client):
    token = _register_and_login(client)
    # JSON ボディ無し
    resp = client.patch(
        "/api/users/me",
        headers={"Authorization": f"Bearer {token}"},
    )
    assert resp.status_code == 400


def test_update_me_missing_name_field(client):
    token = _register_and_login(client)
    resp = client.patch(
        "/api/users/me",
        headers={"Authorization": f"Bearer {token}"},
        json={"foo": "bar"},
    )
    assert resp.status_code == 400
    assert "name" in resp.get_json()["error"]


def test_update_me_rejects_non_string_name(client):
    token = _register_and_login(client)
    resp = client.patch(
        "/api/users/me",
        headers={"Authorization": f"Bearer {token}"},
        json={"name": 42},
    )
    assert resp.status_code == 400
    assert "string" in resp.get_json()["error"].lower()


def test_update_me_rejects_too_long_name(client):
    token = _register_and_login(client)
    # MAX_NAME_LENGTH=100 (デフォルト)
    long_name = "a" * 101
    resp = client.patch(
        "/api/users/me",
        headers={"Authorization": f"Bearer {token}"},
        json={"name": long_name},
    )
    assert resp.status_code == 400
    assert "100 characters" in resp.get_json()["error"]


def test_update_me_ignores_email_and_password(client):
    # `name` 以外のフィールドは黙って破棄。email / password の改変は許さない。
    token = _register_and_login(client, email="orig@x.com")
    resp = client.patch(
        "/api/users/me",
        headers={"Authorization": f"Bearer {token}"},
        json={"name": "New", "email": "hacked@x.com", "password": "evil"},
    )
    assert resp.status_code == 200
    body = resp.get_json()
    # email は変わらない
    assert body["email"] == "orig@x.com"
    # password も保存ハッシュは変わらない（旧 password でログインできる）
    login = client.post(
        "/api/users/login",
        json={"email": "orig@x.com", "password": "pass123"},
    )
    assert login.status_code == 200


def test_update_me_for_deleted_user_returns_404(client):
    token = _register_and_login(client)
    # JWT は有効だが users_db からユーザーを直接削除して、404 経路を踏ませる
    users_db.clear()
    resp = client.patch(
        "/api/users/me",
        headers={"Authorization": f"Bearer {token}"},
        json={"name": "x"},
    )
    assert resp.status_code == 404


# ---------------------------------------------------------------------------
# DELETE /api/users/me — アカウント自己退会
# ---------------------------------------------------------------------------


def test_delete_me_success(client):
    token = _register_and_login(client)
    resp = client.delete(
        "/api/users/me",
        headers={"Authorization": f"Bearer {token}"},
        json={"current_password": "pass123"},
    )
    assert resp.status_code == 200
    assert resp.get_json() == {"deleted": True}

    # 削除後は同じ email でログインできない
    login = client.post(
        "/api/users/login",
        json={"email": "a@b.com", "password": "pass123"},
    )
    assert login.status_code == 401

    # `users_db` からも消えている
    assert "a@b.com" not in users_db


def test_delete_me_allows_reregistration_with_same_email(client):
    # 削除後、同じメールアドレスでの再登録が成功すること（idempotent な lifecycle）。
    token = _register_and_login(client)
    client.delete(
        "/api/users/me",
        headers={"Authorization": f"Bearer {token}"},
        json={"current_password": "pass123"},
    )
    resp = client.post(
        "/api/users/register",
        json={"email": "a@b.com", "password": "freshpass", "name": "New"},
    )
    assert resp.status_code == 201


def test_delete_me_no_auth(client):
    resp = client.delete(
        "/api/users/me",
        json={"current_password": "pass123"},
    )
    assert resp.status_code == 401


def test_delete_me_invalid_token(client):
    resp = client.delete(
        "/api/users/me",
        headers={"Authorization": "Bearer invalidtoken"},
        json={"current_password": "pass123"},
    )
    assert resp.status_code == 401


def test_delete_me_missing_body(client):
    token = _register_and_login(client)
    resp = client.delete(
        "/api/users/me",
        headers={"Authorization": f"Bearer {token}"},
    )
    # current_password 必須なので 400
    assert resp.status_code == 400


def test_delete_me_missing_current_password(client):
    token = _register_and_login(client)
    resp = client.delete(
        "/api/users/me",
        headers={"Authorization": f"Bearer {token}"},
        json={"foo": "bar"},
    )
    assert resp.status_code == 400


def test_delete_me_non_string_current_password(client):
    token = _register_and_login(client)
    resp = client.delete(
        "/api/users/me",
        headers={"Authorization": f"Bearer {token}"},
        json={"current_password": 12345},
    )
    assert resp.status_code == 400


def test_delete_me_wrong_current_password(client):
    token = _register_and_login(client)
    resp = client.delete(
        "/api/users/me",
        headers={"Authorization": f"Bearer {token}"},
        json={"current_password": "wrongpass"},
    )
    assert resp.status_code == 401
    assert "Current password" in resp.get_json()["error"]
    # 認証失敗なのでユーザは残っている
    assert "a@b.com" in users_db


def test_delete_me_for_already_deleted_user_returns_404(client):
    # 二重 DELETE で 500 にならないこと（idempotent な失敗）。
    token = _register_and_login(client)
    users_db.clear()
    resp = client.delete(
        "/api/users/me",
        headers={"Authorization": f"Bearer {token}"},
        json={"current_password": "pass123"},
    )
    assert resp.status_code == 404


def test_delete_me_does_not_affect_other_users(client):
    # 別ユーザは削除されないこと（自分の email キーだけが消える）。
    token_alice = _register_and_login(
        client, email="alice@x.com", password="alicepw1", name="Alice",
    )
    _register_and_login(client, email="bob@x.com", password="bobpw123", name="Bob")
    resp = client.delete(
        "/api/users/me",
        headers={"Authorization": f"Bearer {token_alice}"},
        json={"current_password": "alicepw1"},
    )
    assert resp.status_code == 200
    assert "alice@x.com" not in users_db
    assert "bob@x.com" in users_db


def test_delete_me_response_does_not_leak_password(client):
    token = _register_and_login(client)
    resp = client.delete(
        "/api/users/me",
        headers={"Authorization": f"Bearer {token}"},
        json={"current_password": "pass123"},
    )
    body = resp.get_json()
    # current_password / password / hash 等のフィールドが漏れていないこと
    assert "password" not in body
    assert "current_password" not in body
    assert "hash" not in body
    assert "email" not in body


# ---- /api/users/count ----

def test_count_users_empty(client):
    resp = client.get("/api/users/count")
    assert resp.status_code == 200
    body = resp.get_json()
    assert body == {
        "total": 0,
        "oldest_created_at": None,
        "newest_created_at": None,
    }


def test_count_users_basic(client):
    _register_user_with_created_at("a@example.com", "2024-01-01T00:00:00+00:00")
    _register_user_with_created_at("b@example.com", "2024-06-01T00:00:00+00:00")
    _register_user_with_created_at("c@example.com", "2024-12-01T00:00:00+00:00")
    resp = client.get("/api/users/count")
    assert resp.status_code == 200
    body = resp.get_json()
    assert body["total"] == 3
    assert body["oldest_created_at"] == "2024-01-01T00:00:00+00:00"
    assert body["newest_created_at"] == "2024-12-01T00:00:00+00:00"


def test_count_users_filters_by_q_email(client):
    _register_user_with_created_at("alice@example.com", "2024-01-01T00:00:00+00:00")
    _register_user_with_created_at("bob@example.com", "2024-02-01T00:00:00+00:00")
    resp = client.get("/api/users/count?q=alice")
    body = resp.get_json()
    assert body["total"] == 1


def test_count_users_filters_by_q_name(client):
    _register_user_with_created_at("a@example.com", "2024-01-01T00:00:00+00:00")
    users_db["a@example.com"]["name"] = "Charlie"
    _register_user_with_created_at("b@example.com", "2024-02-01T00:00:00+00:00")
    users_db["b@example.com"]["name"] = "Dave"
    resp = client.get("/api/users/count?q=charlie")
    body = resp.get_json()
    assert body["total"] == 1


def test_count_users_q_case_insensitive(client):
    _register_user_with_created_at("Alice@Example.com", "2024-01-01T00:00:00+00:00")
    resp = client.get("/api/users/count?q=ALICE")
    body = resp.get_json()
    # email は register 経由でなく直接書き込みのため、normalize されていないが小文字化比較される
    assert body["total"] == 1


def test_count_users_filters_by_since(client):
    _register_user_with_created_at("old@example.com", "2024-01-01T00:00:00+00:00")
    _register_user_with_created_at("new@example.com", "2024-06-01T00:00:00+00:00")
    resp = client.get("/api/users/count?since=2024-03-01T00:00:00Z")
    body = resp.get_json()
    assert body["total"] == 1
    assert body["oldest_created_at"] == "2024-06-01T00:00:00+00:00"
    assert body["newest_created_at"] == "2024-06-01T00:00:00+00:00"
    assert body["since"] == "2024-03-01T00:00:00Z"


def test_count_users_filters_by_until(client):
    _register_user_with_created_at("old@example.com", "2024-01-01T00:00:00+00:00")
    _register_user_with_created_at("new@example.com", "2024-06-01T00:00:00+00:00")
    resp = client.get("/api/users/count?until=2024-03-01T00:00:00Z")
    body = resp.get_json()
    assert body["total"] == 1
    assert body["oldest_created_at"] == "2024-01-01T00:00:00+00:00"
    assert body["until"] == "2024-03-01T00:00:00Z"


def test_count_users_filters_by_range(client):
    _register_user_with_created_at("a@example.com", "2024-01-01T00:00:00+00:00")
    _register_user_with_created_at("b@example.com", "2024-06-01T00:00:00+00:00")
    _register_user_with_created_at("c@example.com", "2024-12-01T00:00:00+00:00")
    resp = client.get(
        "/api/users/count?since=2024-03-01T00:00:00Z&until=2024-09-01T00:00:00Z"
    )
    body = resp.get_json()
    assert body["total"] == 1
    assert body["oldest_created_at"] == "2024-06-01T00:00:00+00:00"


def test_count_users_q_combined_with_since(client):
    _register_user_with_created_at("alice@example.com", "2024-01-01T00:00:00+00:00")
    _register_user_with_created_at("alice2@example.com", "2024-06-01T00:00:00+00:00")
    _register_user_with_created_at("bob@example.com", "2024-06-01T00:00:00+00:00")
    resp = client.get("/api/users/count?q=alice&since=2024-03-01T00:00:00Z")
    body = resp.get_json()
    assert body["total"] == 1


def test_count_users_rejects_invalid_since(client):
    resp = client.get("/api/users/count?since=not-a-date")
    assert resp.status_code == 400


def test_count_users_rejects_since_after_until(client):
    resp = client.get(
        "/api/users/count?since=2024-12-01T00:00:00Z&until=2024-01-01T00:00:00Z"
    )
    assert resp.status_code == 400


def test_count_users_rejects_overlong_q(client):
    too_long = "a" * 1000
    resp = client.get(f"/api/users/count?q={too_long}")
    assert resp.status_code == 400


def test_count_users_ignores_pagination_params(client):
    # count は limit / offset / sort / order を解釈しない（あっても 200 を返し、
    # total は常にフィルタ後の全件数）。
    _register_user_with_created_at("a@example.com", "2024-01-01T00:00:00+00:00")
    _register_user_with_created_at("b@example.com", "2024-02-01T00:00:00+00:00")
    _register_user_with_created_at("c@example.com", "2024-03-01T00:00:00+00:00")
    resp = client.get("/api/users/count?limit=1&offset=99999&sort=created_at&order=desc")
    assert resp.status_code == 200
    body = resp.get_json()
    assert body["total"] == 3


def test_count_users_blank_q_treated_as_unfiltered(client):
    _register_user_with_created_at("a@example.com", "2024-01-01T00:00:00+00:00")
    _register_user_with_created_at("b@example.com", "2024-02-01T00:00:00+00:00")
    resp = client.get("/api/users/count?q=%20")
    assert resp.status_code == 200
    body = resp.get_json()
    assert body["total"] == 2


def test_count_users_no_filter_returns_no_echo_fields(client):
    _register_user_with_created_at("a@example.com", "2024-01-01T00:00:00+00:00")
    resp = client.get("/api/users/count")
    body = resp.get_json()
    assert "since" not in body
    assert "until" not in body


# ---- /api/users/by_domain ----


def test_by_domain_empty(client):
    resp = client.get("/api/users/by_domain")
    assert resp.status_code == 200
    body = resp.get_json()
    assert body == {"total": 0, "distinct_domains": 0, "by_domain": []}


def test_by_domain_basic_count_descending(client):
    _register_user_with_created_at("a@example.com", "2024-01-01T00:00:00+00:00")
    _register_user_with_created_at("b@example.com", "2024-02-01T00:00:00+00:00")
    _register_user_with_created_at("c@example.com", "2024-03-01T00:00:00+00:00")
    _register_user_with_created_at("d@other.org", "2024-04-01T00:00:00+00:00")
    _register_user_with_created_at("e@zzz.io", "2024-05-01T00:00:00+00:00")
    resp = client.get("/api/users/by_domain")
    assert resp.status_code == 200
    body = resp.get_json()
    assert body["total"] == 5
    assert body["distinct_domains"] == 3
    # count 降順、同 count はドメイン名昇順
    assert body["by_domain"] == [
        {"domain": "example.com", "count": 3},
        {"domain": "other.org", "count": 1},
        {"domain": "zzz.io", "count": 1},
    ]


def test_by_domain_normalizes_case(client):
    # 大文字混じりの email でもドメインは小文字化して集計される。
    _register_user_with_created_at("Alice@Example.COM", "2024-01-01T00:00:00+00:00")
    _register_user_with_created_at("bob@example.com", "2024-02-01T00:00:00+00:00")
    resp = client.get("/api/users/by_domain")
    body = resp.get_json()
    assert body["distinct_domains"] == 1
    assert body["by_domain"] == [{"domain": "example.com", "count": 2}]


def test_by_domain_fallback_for_emails_without_at(client):
    # `@` を含まない壊れた email は "unknown" にフォールバックする。
    users_db["broken-email-1"] = {
        "id": "id-1",
        "email": "broken-email-1",
        "password": "x",
        "name": "Broken",
        "created_at": "2024-01-01T00:00:00+00:00",
    }
    _register_user_with_created_at("ok@example.com", "2024-02-01T00:00:00+00:00")
    resp = client.get("/api/users/by_domain")
    body = resp.get_json()
    assert body["total"] == 2
    domains = {item["domain"]: item["count"] for item in body["by_domain"]}
    assert domains == {"example.com": 1, "unknown": 1}


def test_by_domain_filters_by_q_email(client):
    _register_user_with_created_at("alice@example.com", "2024-01-01T00:00:00+00:00")
    _register_user_with_created_at("bob@example.com", "2024-02-01T00:00:00+00:00")
    _register_user_with_created_at("alice@other.org", "2024-03-01T00:00:00+00:00")
    resp = client.get("/api/users/by_domain?q=alice")
    body = resp.get_json()
    assert body["total"] == 2
    domains = {item["domain"]: item["count"] for item in body["by_domain"]}
    assert domains == {"example.com": 1, "other.org": 1}


def test_by_domain_filters_by_since(client):
    _register_user_with_created_at("a@example.com", "2024-01-01T00:00:00+00:00")
    _register_user_with_created_at("b@example.com", "2024-06-01T00:00:00+00:00")
    _register_user_with_created_at("c@other.org", "2024-06-15T00:00:00+00:00")
    resp = client.get("/api/users/by_domain?since=2024-03-01T00:00:00Z")
    body = resp.get_json()
    assert body["total"] == 2
    assert body["since"] == "2024-03-01T00:00:00Z"


def test_by_domain_filters_by_until(client):
    _register_user_with_created_at("a@example.com", "2024-01-01T00:00:00+00:00")
    _register_user_with_created_at("b@example.com", "2024-06-01T00:00:00+00:00")
    resp = client.get("/api/users/by_domain?until=2024-03-01T00:00:00Z")
    body = resp.get_json()
    assert body["total"] == 1
    assert body["until"] == "2024-03-01T00:00:00Z"


def test_by_domain_rejects_invalid_since(client):
    resp = client.get("/api/users/by_domain?since=not-a-date")
    assert resp.status_code == 400


def test_by_domain_rejects_since_after_until(client):
    resp = client.get(
        "/api/users/by_domain?since=2024-12-01T00:00:00Z&until=2024-01-01T00:00:00Z"
    )
    assert resp.status_code == 400


def test_by_domain_rejects_overlong_q(client):
    too_long = "a" * 1000
    resp = client.get(f"/api/users/by_domain?q={too_long}")
    assert resp.status_code == 400


def test_by_domain_ignores_pagination_params(client):
    # by_domain は limit / offset / sort / order を解釈しない。
    _register_user_with_created_at("a@example.com", "2024-01-01T00:00:00+00:00")
    _register_user_with_created_at("b@example.com", "2024-02-01T00:00:00+00:00")
    resp = client.get("/api/users/by_domain?limit=1&offset=99999&sort=created_at&order=desc")
    assert resp.status_code == 200
    body = resp.get_json()
    assert body["total"] == 2


def test_by_domain_no_filter_returns_no_echo_fields(client):
    _register_user_with_created_at("a@example.com", "2024-01-01T00:00:00+00:00")
    resp = client.get("/api/users/by_domain")
    body = resp.get_json()
    assert "since" not in body
    assert "until" not in body


def test_by_domain_tie_break_by_domain_name(client):
    # 同 count のドメインはドメイン名昇順で並ぶ。
    _register_user_with_created_at("u1@zzz.io", "2024-01-01T00:00:00+00:00")
    _register_user_with_created_at("u2@aaa.io", "2024-02-01T00:00:00+00:00")
    _register_user_with_created_at("u3@mmm.io", "2024-03-01T00:00:00+00:00")
    resp = client.get("/api/users/by_domain")
    body = resp.get_json()
    domains = [item["domain"] for item in body["by_domain"]]
    assert domains == ["aaa.io", "mmm.io", "zzz.io"]


# ---- /api/users/signups_by_day ----


def test_signups_by_day_empty(client):
    resp = client.get("/api/users/signups_by_day")
    assert resp.status_code == 200
    body = resp.get_json()
    assert body == {"total": 0, "distinct_days": 0, "by_day": []}


def test_signups_by_day_basic_chronological_order(client):
    _register_user_with_created_at("a@example.com", "2024-01-15T10:00:00+00:00")
    _register_user_with_created_at("b@example.com", "2024-01-15T23:59:00+00:00")
    _register_user_with_created_at("c@example.com", "2024-01-16T00:01:00+00:00")
    _register_user_with_created_at("d@example.com", "2024-02-01T05:00:00+00:00")
    resp = client.get("/api/users/signups_by_day")
    assert resp.status_code == 200
    body = resp.get_json()
    assert body["total"] == 4
    assert body["distinct_days"] == 3
    assert body["by_day"] == [
        {"day": "2024-01-15", "count": 2},
        {"day": "2024-01-16", "count": 1},
        {"day": "2024-02-01", "count": 1},
    ]


def test_signups_by_day_normalizes_to_utc_date(client):
    # tz オフセット付き created_at は UTC に変換してから日付ビニングされる。
    # 2024-01-01T23:30:00+09:00 -> UTC は 2024-01-01T14:30:00Z -> day = "2024-01-01"
    _register_user_with_created_at("jp@example.com", "2024-01-01T23:30:00+09:00")
    # 2024-01-01T02:30:00-09:00 -> UTC は 2024-01-01T11:30:00Z -> day = "2024-01-01"
    _register_user_with_created_at("us@example.com", "2024-01-01T02:30:00-09:00")
    resp = client.get("/api/users/signups_by_day")
    body = resp.get_json()
    assert body["by_day"] == [{"day": "2024-01-01", "count": 2}]


def test_signups_by_day_fallback_unknown_for_broken_created_at(client):
    _register_user_with_created_at("ok@example.com", "2024-01-01T00:00:00+00:00")
    users_db["broken@example.com"] = {
        "id": "id-broken",
        "email": "broken@example.com",
        "password": "x",
        "name": "Broken",
        "created_at": "not-an-iso-date",
    }
    resp = client.get("/api/users/signups_by_day")
    body = resp.get_json()
    counts = {item["day"]: item["count"] for item in body["by_day"]}
    assert counts == {"2024-01-01": 1, "unknown": 1}


def test_signups_by_day_filters_by_q(client):
    _register_user_with_created_at("alice@example.com", "2024-01-01T00:00:00+00:00")
    _register_user_with_created_at("bob@example.com", "2024-01-02T00:00:00+00:00")
    resp = client.get("/api/users/signups_by_day?q=alice")
    body = resp.get_json()
    assert body["total"] == 1
    assert body["by_day"] == [{"day": "2024-01-01", "count": 1}]


def test_signups_by_day_filters_by_since(client):
    _register_user_with_created_at("old@example.com", "2024-01-01T00:00:00+00:00")
    _register_user_with_created_at("new@example.com", "2024-06-01T00:00:00+00:00")
    resp = client.get("/api/users/signups_by_day?since=2024-03-01T00:00:00Z")
    body = resp.get_json()
    assert body["total"] == 1
    assert body["by_day"] == [{"day": "2024-06-01", "count": 1}]


def test_signups_by_day_filters_by_until(client):
    _register_user_with_created_at("old@example.com", "2024-01-01T00:00:00+00:00")
    _register_user_with_created_at("new@example.com", "2024-06-01T00:00:00+00:00")
    resp = client.get("/api/users/signups_by_day?until=2024-03-01T00:00:00Z")
    body = resp.get_json()
    assert body["total"] == 1
    assert body["by_day"] == [{"day": "2024-01-01", "count": 1}]


def test_signups_by_day_rejects_invalid_since(client):
    resp = client.get("/api/users/signups_by_day?since=not-a-date")
    assert resp.status_code == 400


def test_signups_by_day_rejects_since_after_until(client):
    resp = client.get(
        "/api/users/signups_by_day?since=2024-12-01T00:00:00Z&until=2024-01-01T00:00:00Z"
    )
    assert resp.status_code == 400


def test_signups_by_day_rejects_overlong_q(client):
    too_long = "a" * 1000
    resp = client.get(f"/api/users/signups_by_day?q={too_long}")
    assert resp.status_code == 400


def test_signups_by_day_ignores_pagination_params(client):
    # signups_by_day は limit / offset / sort / order を解釈しない。
    _register_user_with_created_at("u1@example.com", "2024-01-01T00:00:00+00:00")
    _register_user_with_created_at("u2@example.com", "2024-01-02T00:00:00+00:00")
    resp = client.get(
        "/api/users/signups_by_day?limit=1&offset=99999&sort=created_at&order=desc"
    )
    body = resp.get_json()
    assert body["total"] == 2
    assert len(body["by_day"]) == 2


def test_signups_by_day_no_filter_returns_no_echo_fields(client):
    _register_user_with_created_at("u1@example.com", "2024-01-01T00:00:00+00:00")
    resp = client.get("/api/users/signups_by_day")
    body = resp.get_json()
    assert "since" not in body
    assert "until" not in body


def test_signups_by_day_echoes_since_until_when_provided(client):
    _register_user_with_created_at("u1@example.com", "2024-01-01T00:00:00+00:00")
    resp = client.get(
        "/api/users/signups_by_day?since=2023-12-01T00:00:00Z&until=2024-12-01T00:00:00Z"
    )
    body = resp.get_json()
    assert body["since"] == "2023-12-01T00:00:00Z"
    assert body["until"] == "2024-12-01T00:00:00Z"


def test_signups_by_day_unknown_sorts_after_dated_days(client):
    # `unknown` ラベルは ISO 日付の後に並ぶ（lex 比較で "0..9" の後）。
    _register_user_with_created_at("ok@example.com", "2024-06-01T00:00:00+00:00")
    users_db["broken@example.com"] = {
        "id": "id-broken",
        "email": "broken@example.com",
        "password": "x",
        "name": "Broken",
        "created_at": "not-an-iso-date",
    }
    resp = client.get("/api/users/signups_by_day")
    body = resp.get_json()
    days = [item["day"] for item in body["by_day"]]
    assert days == ["2024-06-01", "unknown"]


# ---- /api/users/signups_by_month ----


def test_signups_by_month_empty(client):
    resp = client.get("/api/users/signups_by_month")
    assert resp.status_code == 200
    body = resp.get_json()
    assert body == {"total": 0, "distinct_months": 0, "by_month": []}


def test_signups_by_month_basic_chronological_order(client):
    _register_user_with_created_at("a@example.com", "2024-01-15T10:00:00+00:00")
    _register_user_with_created_at("b@example.com", "2024-01-31T23:59:00+00:00")
    _register_user_with_created_at("c@example.com", "2024-02-01T00:01:00+00:00")
    _register_user_with_created_at("d@example.com", "2024-06-15T05:00:00+00:00")
    _register_user_with_created_at("e@example.com", "2025-01-01T00:00:00+00:00")
    resp = client.get("/api/users/signups_by_month")
    assert resp.status_code == 200
    body = resp.get_json()
    assert body["total"] == 5
    assert body["distinct_months"] == 4
    assert body["by_month"] == [
        {"month": "2024-01", "count": 2},
        {"month": "2024-02", "count": 1},
        {"month": "2024-06", "count": 1},
        {"month": "2025-01", "count": 1},
    ]


def test_signups_by_month_normalizes_to_utc_month(client):
    # tz オフセット付き created_at は UTC に変換してから月ビニングされる。
    # 2024-01-31T23:30:00+09:00 -> UTC は 2024-01-31T14:30:00Z -> month = "2024-01"
    _register_user_with_created_at("jp@example.com", "2024-01-31T23:30:00+09:00")
    # 2024-02-01T02:30:00-09:00 -> UTC は 2024-02-01T11:30:00Z -> month = "2024-02"
    # → 月境界をまたぐ tz オフセットでも UTC で正規化される。
    _register_user_with_created_at("us@example.com", "2024-02-01T02:30:00-09:00")
    resp = client.get("/api/users/signups_by_month")
    body = resp.get_json()
    assert body["by_month"] == [
        {"month": "2024-01", "count": 1},
        {"month": "2024-02", "count": 1},
    ]


def test_signups_by_month_fallback_unknown_for_broken_created_at(client):
    _register_user_with_created_at("ok@example.com", "2024-01-15T00:00:00+00:00")
    users_db["broken@example.com"] = {
        "id": "id-broken",
        "email": "broken@example.com",
        "password": "x",
        "name": "Broken",
        "created_at": "not-an-iso-date",
    }
    resp = client.get("/api/users/signups_by_month")
    body = resp.get_json()
    counts = {item["month"]: item["count"] for item in body["by_month"]}
    assert counts == {"2024-01": 1, "unknown": 1}


def test_signups_by_month_filters_by_q(client):
    _register_user_with_created_at("alice@example.com", "2024-01-15T00:00:00+00:00")
    _register_user_with_created_at("bob@example.com", "2024-02-15T00:00:00+00:00")
    resp = client.get("/api/users/signups_by_month?q=alice")
    body = resp.get_json()
    assert body["total"] == 1
    assert body["by_month"] == [{"month": "2024-01", "count": 1}]


def test_signups_by_month_filters_by_since(client):
    _register_user_with_created_at("old@example.com", "2024-01-15T00:00:00+00:00")
    _register_user_with_created_at("new@example.com", "2024-06-15T00:00:00+00:00")
    resp = client.get("/api/users/signups_by_month?since=2024-03-01T00:00:00Z")
    body = resp.get_json()
    assert body["total"] == 1
    assert body["by_month"] == [{"month": "2024-06", "count": 1}]


def test_signups_by_month_filters_by_until(client):
    _register_user_with_created_at("old@example.com", "2024-01-15T00:00:00+00:00")
    _register_user_with_created_at("new@example.com", "2024-06-15T00:00:00+00:00")
    resp = client.get("/api/users/signups_by_month?until=2024-03-01T00:00:00Z")
    body = resp.get_json()
    assert body["total"] == 1
    assert body["by_month"] == [{"month": "2024-01", "count": 1}]


def test_signups_by_month_rejects_invalid_since(client):
    resp = client.get("/api/users/signups_by_month?since=not-a-date")
    assert resp.status_code == 400


def test_signups_by_month_rejects_since_after_until(client):
    resp = client.get(
        "/api/users/signups_by_month?since=2024-12-01T00:00:00Z&until=2024-01-01T00:00:00Z"
    )
    assert resp.status_code == 400


def test_signups_by_month_rejects_overlong_q(client):
    too_long = "a" * 1000
    resp = client.get(f"/api/users/signups_by_month?q={too_long}")
    assert resp.status_code == 400


def test_signups_by_month_ignores_pagination_params(client):
    _register_user_with_created_at("u1@example.com", "2024-01-15T00:00:00+00:00")
    _register_user_with_created_at("u2@example.com", "2024-02-15T00:00:00+00:00")
    resp = client.get(
        "/api/users/signups_by_month?limit=1&offset=99999&sort=created_at&order=desc"
    )
    body = resp.get_json()
    assert body["total"] == 2
    assert len(body["by_month"]) == 2


def test_signups_by_month_no_filter_returns_no_echo_fields(client):
    _register_user_with_created_at("u1@example.com", "2024-01-01T00:00:00+00:00")
    resp = client.get("/api/users/signups_by_month")
    body = resp.get_json()
    assert "since" not in body
    assert "until" not in body


def test_signups_by_month_echoes_since_until_when_provided(client):
    _register_user_with_created_at("u1@example.com", "2024-01-15T00:00:00+00:00")
    resp = client.get(
        "/api/users/signups_by_month?since=2023-12-01T00:00:00Z&until=2024-12-01T00:00:00Z"
    )
    body = resp.get_json()
    assert body["since"] == "2023-12-01T00:00:00Z"
    assert body["until"] == "2024-12-01T00:00:00Z"


def test_signups_by_month_unknown_sorts_after_dated_months(client):
    _register_user_with_created_at("ok@example.com", "2024-06-15T00:00:00+00:00")
    users_db["broken@example.com"] = {
        "id": "id-broken",
        "email": "broken@example.com",
        "password": "x",
        "name": "Broken",
        "created_at": "not-an-iso-date",
    }
    resp = client.get("/api/users/signups_by_month")
    body = resp.get_json()
    months = [item["month"] for item in body["by_month"]]
    assert months == ["2024-06", "unknown"]


# ---- /api/users/signups_by_day_of_week ----


def test_signups_by_day_of_week_empty(client):
    resp = client.get("/api/users/signups_by_day_of_week")
    assert resp.status_code == 200
    body = resp.get_json()
    assert body == {"total": 0, "distinct_days_of_week": 0, "by_day_of_week": []}


def test_signups_by_day_of_week_basic_chronological_order(client):
    # ISO 8601: 1=月曜 〜 7=日曜
    # 2024-01-01 は月曜 (1)、2024-01-02 は火曜 (2)、2024-01-07 は日曜 (7)
    _register_user_with_created_at("mon1@example.com", "2024-01-01T10:00:00+00:00")
    _register_user_with_created_at("mon2@example.com", "2024-01-08T10:00:00+00:00")
    _register_user_with_created_at("tue@example.com", "2024-01-02T10:00:00+00:00")
    _register_user_with_created_at("sun@example.com", "2024-01-07T10:00:00+00:00")
    resp = client.get("/api/users/signups_by_day_of_week")
    assert resp.status_code == 200
    body = resp.get_json()
    assert body["total"] == 4
    assert body["distinct_days_of_week"] == 3
    assert body["by_day_of_week"] == [
        {"day_of_week": "1", "count": 2},
        {"day_of_week": "2", "count": 1},
        {"day_of_week": "7", "count": 1},
    ]


def test_signups_by_day_of_week_normalizes_to_utc(client):
    # 2024-01-01 (月曜) 23:30+09:00 -> UTC 14:30 同日 -> 月曜 (1)
    _register_user_with_created_at("jp@example.com", "2024-01-01T23:30:00+09:00")
    # 2024-01-02 (火曜) 02:30-09:00 -> UTC 11:30 同日 -> 火曜 (2)
    _register_user_with_created_at("us@example.com", "2024-01-02T02:30:00-09:00")
    # tz 越境で曜日が変わるケース: 2024-01-08 (月曜) 02:00+09:00 -> UTC は
    # 2024-01-07 (日曜) 17:00 -> 日曜 (7)
    _register_user_with_created_at("jp2@example.com", "2024-01-08T02:00:00+09:00")
    resp = client.get("/api/users/signups_by_day_of_week")
    body = resp.get_json()
    assert body["by_day_of_week"] == [
        {"day_of_week": "1", "count": 1},
        {"day_of_week": "2", "count": 1},
        {"day_of_week": "7", "count": 1},
    ]


def test_signups_by_day_of_week_fallback_unknown_for_broken_created_at(client):
    _register_user_with_created_at("ok@example.com", "2024-01-01T00:00:00+00:00")
    users_db["broken@example.com"] = {
        "id": "id-broken",
        "email": "broken@example.com",
        "password": "x",
        "name": "Broken",
        "created_at": "not-an-iso-date",
    }
    resp = client.get("/api/users/signups_by_day_of_week")
    body = resp.get_json()
    counts = {item["day_of_week"]: item["count"] for item in body["by_day_of_week"]}
    assert counts == {"1": 1, "unknown": 1}


def test_signups_by_day_of_week_filters_by_q(client):
    _register_user_with_created_at("alice@example.com", "2024-01-01T00:00:00+00:00")
    _register_user_with_created_at("bob@example.com", "2024-01-02T00:00:00+00:00")
    resp = client.get("/api/users/signups_by_day_of_week?q=alice")
    body = resp.get_json()
    assert body["total"] == 1
    assert body["by_day_of_week"] == [{"day_of_week": "1", "count": 1}]


def test_signups_by_day_of_week_filters_by_since(client):
    _register_user_with_created_at("old@example.com", "2024-01-01T00:00:00+00:00")
    _register_user_with_created_at("new@example.com", "2024-06-15T00:00:00+00:00")
    # 2024-06-15 は土曜 (6)
    resp = client.get("/api/users/signups_by_day_of_week?since=2024-03-01T00:00:00Z")
    body = resp.get_json()
    assert body["total"] == 1
    assert body["by_day_of_week"] == [{"day_of_week": "6", "count": 1}]


def test_signups_by_day_of_week_filters_by_until(client):
    _register_user_with_created_at("old@example.com", "2024-01-01T00:00:00+00:00")
    _register_user_with_created_at("new@example.com", "2024-06-15T00:00:00+00:00")
    # 2024-01-01 は月曜 (1)
    resp = client.get("/api/users/signups_by_day_of_week?until=2024-03-01T00:00:00Z")
    body = resp.get_json()
    assert body["total"] == 1
    assert body["by_day_of_week"] == [{"day_of_week": "1", "count": 1}]


def test_signups_by_day_of_week_rejects_invalid_since(client):
    resp = client.get("/api/users/signups_by_day_of_week?since=not-a-date")
    assert resp.status_code == 400


def test_signups_by_day_of_week_rejects_since_after_until(client):
    resp = client.get(
        "/api/users/signups_by_day_of_week?since=2024-12-01T00:00:00Z&until=2024-01-01T00:00:00Z"
    )
    assert resp.status_code == 400


def test_signups_by_day_of_week_rejects_overlong_q(client):
    too_long = "a" * 1000
    resp = client.get(f"/api/users/signups_by_day_of_week?q={too_long}")
    assert resp.status_code == 400


def test_signups_by_day_of_week_ignores_pagination_params(client):
    _register_user_with_created_at("u1@example.com", "2024-01-01T00:00:00+00:00")
    _register_user_with_created_at("u2@example.com", "2024-01-02T00:00:00+00:00")
    resp = client.get(
        "/api/users/signups_by_day_of_week?limit=1&offset=99999&sort=created_at&order=desc"
    )
    body = resp.get_json()
    assert body["total"] == 2
    assert len(body["by_day_of_week"]) == 2


def test_signups_by_day_of_week_no_filter_returns_no_echo_fields(client):
    _register_user_with_created_at("u1@example.com", "2024-01-01T00:00:00+00:00")
    resp = client.get("/api/users/signups_by_day_of_week")
    body = resp.get_json()
    assert "since" not in body
    assert "until" not in body


def test_signups_by_day_of_week_echoes_since_until_when_provided(client):
    _register_user_with_created_at("u1@example.com", "2024-01-15T00:00:00+00:00")
    resp = client.get(
        "/api/users/signups_by_day_of_week?since=2023-12-01T00:00:00Z&until=2024-12-01T00:00:00Z"
    )
    body = resp.get_json()
    assert body["since"] == "2023-12-01T00:00:00Z"
    assert body["until"] == "2024-12-01T00:00:00Z"


def test_signups_by_day_of_week_unknown_sorts_after_dated_days(client):
    _register_user_with_created_at("ok@example.com", "2024-01-01T00:00:00+00:00")
    users_db["broken@example.com"] = {
        "id": "id-broken",
        "email": "broken@example.com",
        "password": "x",
        "name": "Broken",
        "created_at": "not-an-iso-date",
    }
    resp = client.get("/api/users/signups_by_day_of_week")
    body = resp.get_json()
    days = [item["day_of_week"] for item in body["by_day_of_week"]]
    assert days == ["1", "unknown"]


# ---- /api/users/signups_by_hour_of_day ----


def test_signups_by_hour_of_day_empty(client):
    resp = client.get("/api/users/signups_by_hour_of_day")
    assert resp.status_code == 200
    body = resp.get_json()
    assert body == {"total": 0, "distinct_hours": 0, "by_hour_of_day": []}


def test_signups_by_hour_of_day_basic_chronological_order(client):
    _register_user_with_created_at("a@example.com", "2024-01-01T00:30:00+00:00")
    _register_user_with_created_at("b@example.com", "2024-01-01T09:15:00+00:00")
    _register_user_with_created_at("c@example.com", "2024-01-02T09:45:00+00:00")
    _register_user_with_created_at("d@example.com", "2024-01-03T23:00:00+00:00")
    resp = client.get("/api/users/signups_by_hour_of_day")
    assert resp.status_code == 200
    body = resp.get_json()
    assert body["total"] == 4
    assert body["distinct_hours"] == 3
    # キーは 2 桁数字なので lex 順 = 時刻順
    assert body["by_hour_of_day"] == [
        {"hour": "00", "count": 1},
        {"hour": "09", "count": 2},
        {"hour": "23", "count": 1},
    ]


def test_signups_by_hour_of_day_normalizes_to_utc(client):
    # 2024-01-01T01:30:00+09:00 -> UTC 2023-12-31T16:30:00 -> hour="16"
    _register_user_with_created_at("jp@example.com", "2024-01-01T01:30:00+09:00")
    # 2024-01-01T03:00:00-05:00 -> UTC 2024-01-01T08:00:00 -> hour="08"
    _register_user_with_created_at("us@example.com", "2024-01-01T03:00:00-05:00")
    resp = client.get("/api/users/signups_by_hour_of_day")
    body = resp.get_json()
    counts = {item["hour"]: item["count"] for item in body["by_hour_of_day"]}
    assert counts == {"16": 1, "08": 1}


def test_signups_by_hour_of_day_naive_treated_as_utc(client):
    # tz 情報なしは UTC として扱う（signups_by_day_of_week と同じ）
    _register_user_with_created_at("naive@example.com", "2024-01-01T14:30:00")
    resp = client.get("/api/users/signups_by_hour_of_day")
    body = resp.get_json()
    assert body["by_hour_of_day"] == [{"hour": "14", "count": 1}]


def test_signups_by_hour_of_day_fallback_unknown_for_broken_created_at(client):
    _register_user_with_created_at("ok@example.com", "2024-01-01T05:00:00+00:00")
    users_db["broken@example.com"] = {
        "id": "id-broken",
        "email": "broken@example.com",
        "password": "x",
        "name": "Broken",
        "created_at": "not-an-iso-date",
    }
    resp = client.get("/api/users/signups_by_hour_of_day")
    body = resp.get_json()
    counts = {item["hour"]: item["count"] for item in body["by_hour_of_day"]}
    assert counts == {"05": 1, "unknown": 1}


def test_signups_by_hour_of_day_filters_by_q(client):
    _register_user_with_created_at("alice@example.com", "2024-01-01T03:00:00+00:00")
    _register_user_with_created_at("bob@example.com", "2024-01-01T04:00:00+00:00")
    resp = client.get("/api/users/signups_by_hour_of_day?q=alice")
    body = resp.get_json()
    assert body["total"] == 1
    assert body["by_hour_of_day"] == [{"hour": "03", "count": 1}]


def test_signups_by_hour_of_day_filters_by_since(client):
    _register_user_with_created_at("old@example.com", "2024-01-01T01:00:00+00:00")
    _register_user_with_created_at("new@example.com", "2024-06-15T12:00:00+00:00")
    resp = client.get("/api/users/signups_by_hour_of_day?since=2024-03-01T00:00:00Z")
    body = resp.get_json()
    assert body["total"] == 1
    assert body["by_hour_of_day"] == [{"hour": "12", "count": 1}]


def test_signups_by_hour_of_day_filters_by_until(client):
    _register_user_with_created_at("old@example.com", "2024-01-01T01:00:00+00:00")
    _register_user_with_created_at("new@example.com", "2024-06-15T12:00:00+00:00")
    resp = client.get("/api/users/signups_by_hour_of_day?until=2024-03-01T00:00:00Z")
    body = resp.get_json()
    assert body["total"] == 1
    assert body["by_hour_of_day"] == [{"hour": "01", "count": 1}]


def test_signups_by_hour_of_day_rejects_invalid_since(client):
    resp = client.get("/api/users/signups_by_hour_of_day?since=not-a-date")
    assert resp.status_code == 400


def test_signups_by_hour_of_day_rejects_since_after_until(client):
    resp = client.get(
        "/api/users/signups_by_hour_of_day?since=2024-12-01T00:00:00Z&until=2024-01-01T00:00:00Z"
    )
    assert resp.status_code == 400


def test_signups_by_hour_of_day_rejects_overlong_q(client):
    too_long = "a" * 1000
    resp = client.get(f"/api/users/signups_by_hour_of_day?q={too_long}")
    assert resp.status_code == 400


def test_signups_by_hour_of_day_ignores_pagination_params(client):
    _register_user_with_created_at("u1@example.com", "2024-01-01T01:00:00+00:00")
    _register_user_with_created_at("u2@example.com", "2024-01-01T02:00:00+00:00")
    resp = client.get(
        "/api/users/signups_by_hour_of_day?limit=1&offset=99999&sort=created_at&order=desc"
    )
    body = resp.get_json()
    assert body["total"] == 2
    assert len(body["by_hour_of_day"]) == 2


def test_signups_by_hour_of_day_no_filter_returns_no_echo_fields(client):
    _register_user_with_created_at("u1@example.com", "2024-01-01T01:00:00+00:00")
    resp = client.get("/api/users/signups_by_hour_of_day")
    body = resp.get_json()
    assert "since" not in body
    assert "until" not in body


def test_signups_by_hour_of_day_echoes_since_until_when_provided(client):
    _register_user_with_created_at("u1@example.com", "2024-01-15T01:00:00+00:00")
    resp = client.get(
        "/api/users/signups_by_hour_of_day?since=2023-12-01T00:00:00Z&until=2024-12-01T00:00:00Z"
    )
    body = resp.get_json()
    assert body["since"] == "2023-12-01T00:00:00Z"
    assert body["until"] == "2024-12-01T00:00:00Z"


def test_signups_by_hour_of_day_unknown_sorts_after_dated_hours(client):
    _register_user_with_created_at("ok@example.com", "2024-01-01T00:00:00+00:00")
    users_db["broken@example.com"] = {
        "id": "id-broken",
        "email": "broken@example.com",
        "password": "x",
        "name": "Broken",
        "created_at": "not-an-iso-date",
    }
    resp = client.get("/api/users/signups_by_hour_of_day")
    body = resp.get_json()
    hours = [item["hour"] for item in body["by_hour_of_day"]]
    assert hours == ["00", "unknown"]


def test_signups_by_hour_of_day_full_24_hour_distribution(client):
    # 0:00, 6:00, 12:00, 18:00 を別々の日付で登録 → 4 つの distinct な時刻
    _register_user_with_created_at("h00@example.com", "2024-01-01T00:00:00+00:00")
    _register_user_with_created_at("h06@example.com", "2024-01-02T06:00:00+00:00")
    _register_user_with_created_at("h12@example.com", "2024-01-03T12:00:00+00:00")
    _register_user_with_created_at("h18@example.com", "2024-01-04T18:00:00+00:00")
    resp = client.get("/api/users/signups_by_hour_of_day")
    body = resp.get_json()
    assert body["total"] == 4
    assert body["distinct_hours"] == 4
    assert body["by_hour_of_day"] == [
        {"hour": "00", "count": 1},
        {"hour": "06", "count": 1},
        {"hour": "12", "count": 1},
        {"hour": "18", "count": 1},
    ]
