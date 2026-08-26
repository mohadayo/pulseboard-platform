"""``_security_headers`` after_request middleware の回帰テスト。

全応答に付く 3 ヘッダを、成功系 (200) / バリデーション 4xx / 未知パス 404 の
3 経路で固定することで、「特定ルートだけ抜ける」リグレッションを検出する。
"""

import pytest
from app import app, users_db


@pytest.fixture
def client():
    """他テストと同じく users_db をリセットして独立実行できる fixture。"""
    app.config["TESTING"] = True
    users_db.clear()
    with app.test_client() as c:
        yield c


def _assert_security_headers(response):
    assert response.headers.get("X-Content-Type-Options") == "nosniff"
    assert response.headers.get("X-Frame-Options") == "DENY"
    assert response.headers.get("Referrer-Policy") == "no-referrer"


def test_security_headers_are_set_on_200(client):
    """/health (200) にすべてのセキュリティヘッダが付くこと。"""
    resp = client.get("/health")
    assert resp.status_code == 200
    _assert_security_headers(resp)


def test_security_headers_are_set_on_400(client):
    """POST /api/users/register のバリデーション失敗 (400) でもヘッダが付くこと。

    Flask では ``after_request`` は 4xx/5xx 応答でも実行されるため、
    エラー経路でセキュリティヘッダが漏れないことを回帰確認する。
    """
    resp = client.post("/api/users/register", json={"email": "a@b.com"})
    assert resp.status_code == 400
    _assert_security_headers(resp)


def test_security_headers_are_set_on_404(client):
    """未定義ルート (404) の Flask デフォルトハンドラでもヘッダが付くこと。"""
    resp = client.get("/definitely-not-a-real-endpoint-please")
    assert resp.status_code == 404
    _assert_security_headers(resp)


def test_security_headers_coexist_with_response_time_header(client):
    """``_access_log_end`` の ``X-Response-Time-Ms`` と共存すること。

    ``after_request`` フックを 2 つ挿入しているため、両方のヘッダが同じ応答に
    乗ることを確認する（後段のフックが前段のヘッダを消していないこと）。
    """
    resp = client.get("/health")
    assert resp.status_code == 200
    _assert_security_headers(resp)
    assert "X-Response-Time-Ms" in resp.headers
    assert float(resp.headers["X-Response-Time-Ms"]) >= 0.0
