"""Tests for the LinkPulse URL Shortener."""

import pytest
import pytest_asyncio
from unittest.mock import AsyncMock, patch
from httpx import AsyncClient, ASGITransport

from main import app, url_store


@pytest.fixture(autouse=True)
def clear_store():
    """Clear the URL store before each test."""
    url_store.clear()
    yield
    url_store.clear()


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
