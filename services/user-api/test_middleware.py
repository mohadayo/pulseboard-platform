import pytest
from app import app, users_db


@pytest.fixture
def client():
    """`test_app.py` と独立した fixture。

    既存 `tests` 構成 (`test_app.py` 内に同名 `client` fixture が定義済み) と
    競合せず、middleware の挙動だけを切り出して検証する。
    """
    app.config["TESTING"] = True
    users_db.clear()
    with app.test_client() as c:
        yield c


def test_access_log_middleware_attaches_response_time_header_on_2xx(client):
    """全レスポンスに `X-Response-Time-Ms` が付くこと（成功レスポンス側）。"""
    resp = client.get("/health")
    assert resp.status_code == 200
    header = resp.headers.get("X-Response-Time-Ms")
    assert header is not None, "middleware should attach X-Response-Time-Ms"
    assert float(header) >= 0.0


def test_access_log_middleware_runs_on_4xx(client):
    """バリデーションエラー (4xx) でも middleware は実行され、ヘッダが付与されること。

    既存のハンドラ内 ``logger.warning`` だけでは「どのリクエストが 4xx で返って
    どれだけかかったか」を一貫した軸で観測できなかった。`after_request` は
    HTTPException 経路でも走るため、middleware 由来のレスポンス時間ヘッダが
    必ず付くことを担保する。
    """
    resp = client.post("/api/users/register", json={"email": "a@b.com"})
    assert resp.status_code == 400
    assert "X-Response-Time-Ms" in resp.headers
    assert float(resp.headers["X-Response-Time-Ms"]) >= 0.0
