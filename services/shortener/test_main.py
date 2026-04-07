"""Tests for the LinkPulse URL Shortener."""

import pytest
import pytest_asyncio
from unittest.mock import AsyncMock, patch
from httpx import AsyncClient, ASGITransport

from main import app, url_store, url_reverse


@pytest.fixture(autouse=True)
def clear_store():
    """Clear the URL store and reverse index before each test."""
    url_store.clear()
    url_reverse.clear()
    yield
    url_store.clear()
    url_reverse.clear()


@pytest.fixture
def mock_analytics():
    """Mock the analytics notification so no real HTTP calls are made."""
    with patch("main.notify_analytics", new_callable=AsyncMock) as mock:
        yield mock


@pytest_asyncio.fixture
async def client():
    transport = ASGITransport(app=app)
    async with AsyncClient(transport=transport, base_url="http://testserver") as ac:
        yield ac


@pytest.mark.asyncio
async def test_health(client: AsyncClient):
    resp = await client.get("/health")
    assert resp.status_code == 200
    data = resp.json()
    assert data["status"] == "ok"
    assert data["service"] == "shortener"


@pytest.mark.asyncio
async def test_shorten_url(client: AsyncClient, mock_analytics):
    resp = await client.post("/shorten", json={"url": "https://example.com/long-page"})
    assert resp.status_code == 200
    data = resp.json()
    assert "short_code" in data
    assert len(data["short_code"]) == 6
    assert data["short_url"].endswith(data["short_code"])
    # Analytics should have been notified
    mock_analytics.assert_awaited_once()
    call_payload = mock_analytics.call_args[0][0]
    assert call_payload["event"] == "url_created"
    assert call_payload["original_url"] == "https://example.com/long-page"


@pytest.mark.asyncio
async def test_shorten_invalid_url(client: AsyncClient, mock_analytics):
    resp = await client.post("/shorten", json={"url": "not-a-url"})
    assert resp.status_code == 422


@pytest.mark.asyncio
async def test_shorten_missing_field(client: AsyncClient, mock_analytics):
    resp = await client.post("/shorten", json={"link": "https://example.com"})
    assert resp.status_code == 422


@pytest.mark.asyncio
async def test_redirect(client: AsyncClient, mock_analytics):
    # Create a short URL first
    resp = await client.post("/shorten", json={"url": "https://example.com/target"})
    short_code = resp.json()["short_code"]
    mock_analytics.reset_mock()

    # Follow=False so we can inspect the 302 directly
    resp = await client.get(f"/{short_code}", follow_redirects=False)
    assert resp.status_code == 302
    assert resp.headers["location"] == "https://example.com/target"
    # Analytics click event
    mock_analytics.assert_awaited_once()
    call_payload = mock_analytics.call_args[0][0]
    assert call_payload["event"] == "click"
    assert call_payload["short_code"] == short_code


@pytest.mark.asyncio
async def test_redirect_not_found(client: AsyncClient, mock_analytics):
    resp = await client.get("/nonexistent", follow_redirects=False)
    assert resp.status_code == 404
    assert "not found" in resp.json()["detail"].lower()


@pytest.mark.asyncio
async def test_multiple_urls_get_unique_codes(client: AsyncClient, mock_analytics):
    codes = set()
    for i in range(10):
        resp = await client.post("/shorten", json={"url": f"https://example.com/{i}"})
        assert resp.status_code == 200
        codes.add(resp.json()["short_code"])
    assert len(codes) == 10


@pytest.mark.asyncio
async def test_duplicate_url_returns_same_code(client: AsyncClient, mock_analytics):
    """同一URLを2回送信した場合、同じショートコードが返されること。"""
    target_url = "https://example.com/duplicate-test"

    resp1 = await client.post("/shorten", json={"url": target_url})
    assert resp1.status_code == 200
    code1 = resp1.json()["short_code"]

    # 2回目は同じURLを送信
    resp2 = await client.post("/shorten", json={"url": target_url})
    assert resp2.status_code == 200
    code2 = resp2.json()["short_code"]

    # 同じショートコードが返されること
    assert code1 == code2
    assert resp1.json()["short_url"] == resp2.json()["short_url"]

    # analyticsへの通知は1回のみ（重複登録時は送信しない）
    assert mock_analytics.await_count == 1


@pytest.mark.asyncio
async def test_duplicate_url_does_not_add_extra_entries(client: AsyncClient, mock_analytics):
    """同一URLを複数回送信しても、ストアのエントリが増えないこと。"""
    from main import url_store as store

    target_url = "https://example.com/dedup-store-test"

    await client.post("/shorten", json={"url": target_url})
    count_after_first = len(store)

    await client.post("/shorten", json={"url": target_url})
    count_after_second = len(store)

    assert count_after_first == count_after_second


@pytest.mark.asyncio
async def test_shorten_url_at_max_length(client: AsyncClient, mock_analytics):
    """最大長ちょうどのURLは正常に短縮されること。"""
    from main import MAX_URL_LENGTH

    # https://example.com/ は21文字。残りをパスで埋める
    base = "https://example.com/"
    padding = "a" * (MAX_URL_LENGTH - len(base))
    long_url = base + padding
    assert len(long_url) == MAX_URL_LENGTH

    resp = await client.post("/shorten", json={"url": long_url})
    assert resp.status_code == 200
    assert "short_code" in resp.json()


@pytest.mark.asyncio
async def test_shorten_url_exceeds_max_length(client: AsyncClient, mock_analytics):
    """最大長を超えるURLは422エラーが返されること。"""
    from main import MAX_URL_LENGTH

    base = "https://example.com/"
    padding = "a" * (MAX_URL_LENGTH - len(base) + 1)
    too_long_url = base + padding
    assert len(too_long_url) == MAX_URL_LENGTH + 1

    resp = await client.post("/shorten", json={"url": too_long_url})
    assert resp.status_code == 422
    # analyticsへの通知は行われないこと
    mock_analytics.assert_not_awaited()
