"""LinkPulse URL Shortener Microservice."""

import logging
import os
import string
import random

import httpx
from fastapi import FastAPI, HTTPException, Request
from fastapi.responses import RedirectResponse
from pydantic import BaseModel, HttpUrl, field_validator

logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s [%(levelname)s] %(name)s: %(message)s",
)
logger = logging.getLogger("shortener")

SHORTENER_HOST = os.getenv("SHORTENER_HOST", "0.0.0.0")
SHORTENER_PORT = int(os.getenv("SHORTENER_PORT", "8001"))
ANALYTICS_URL = os.getenv("ANALYTICS_URL", "http://analytics:8002")

CODE_LENGTH = 6
CODE_CHARS = string.ascii_letters + string.digits
MAX_URL_LENGTH = 2048

# In-memory URL store: short_code -> original URL
url_store: dict[str, str] = {}

# Reverse index: original URL -> short_code (重複登録防止用)
url_reverse: dict[str, str] = {}

app = FastAPI(title="LinkPulse Shortener", version="1.0.0")


class ShortenRequest(BaseModel):
    url: HttpUrl

    @field_validator("url")
    @classmethod
    def url_must_not_exceed_max_length(cls, v: HttpUrl) -> HttpUrl:
        if len(str(v)) > MAX_URL_LENGTH:
            raise ValueError(
                f"URL length exceeds maximum allowed length of {MAX_URL_LENGTH} characters"
            )
        return v


class ShortenResponse(BaseModel):
    short_code: str
    short_url: str


def generate_short_code() -> str:
    """Generate a unique 6-character alphanumeric short code."""
    while True:
        code = "".join(random.choices(CODE_CHARS, k=CODE_LENGTH))
        if code not in url_store:
            return code


def get_or_create_short_code(original_url: str) -> tuple[str, bool]:
    """既存のショートコードを返す、なければ新規生成する。

    Returns:
        (short_code, is_new): is_new=True のとき新規生成、False のとき既存コードを返した
    """
    if original_url in url_reverse:
        existing_code = url_reverse[original_url]
        logger.info("URL already shortened: %s -> %s", original_url, existing_code)
        return existing_code, False

    code = generate_short_code()
    url_store[code] = original_url
    url_reverse[original_url] = code
    return code, True


async def notify_analytics(payload: dict) -> None:
    """Fire-and-forget POST to the analytics service."""
    try:
        async with httpx.AsyncClient(timeout=5.0) as client:
            await client.post(f"{ANALYTICS_URL}/events", json=payload)
            logger.info("Analytics notified: %s", payload.get("event"))
    except Exception:
        logger.warning("Failed to notify analytics service", exc_info=True)


@app.get("/health")
async def health():
    return {"status": "ok", "service": "shortener"}


@app.post("/shorten", response_model=ShortenResponse)
async def shorten_url(body: ShortenRequest, request: Request):
    original_url = str(body.url)
    short_code, is_new = get_or_create_short_code(original_url)

    base_url = str(request.base_url).rstrip("/")
    short_url = f"{base_url}/{short_code}"

    if is_new:
        logger.info("Shortened %s -> %s", original_url, short_code)
        await notify_analytics(
            {"event": "url_created", "short_code": short_code, "original_url": original_url}
        )
    else:
        logger.info("Returning existing short code for %s -> %s", original_url, short_code)

    return ShortenResponse(short_code=short_code, short_url=short_url)


@app.get("/{short_code}")
async def redirect_to_url(short_code: str):
    original_url = url_store.get(short_code)
    if original_url is None:
        logger.warning("Short code not found: %s", short_code)
        raise HTTPException(status_code=404, detail=f"Short code '{short_code}' not found")

    logger.info("Redirecting %s -> %s", short_code, original_url)

    await notify_analytics({"event": "click", "short_code": short_code})

    return RedirectResponse(url=original_url, status_code=302)


if __name__ == "__main__":
    import uvicorn

    logger.info("Starting LinkPulse Shortener on %s:%s", SHORTENER_HOST, SHORTENER_PORT)
    uvicorn.run(app, host=SHORTENER_HOST, port=SHORTENER_PORT)
