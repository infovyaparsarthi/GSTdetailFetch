import os
import base64
import json
import requests
from fastapi import FastAPI, HTTPException, Request
from fastapi.responses import HTMLResponse, JSONResponse, FileResponse
from fastapi.staticfiles import StaticFiles
from fastapi.templating import Jinja2Templates
from pydantic import BaseModel
from typing import Optional

from gst_scraper import make_session, fetch_captcha, solve_captcha_with_krutrim, lookup_gstin, load_env_file

load_env_file()

app = FastAPI(
    title="GST Taxpayer Lookup API",
    description="Modern web wrapper for GST Taxpayer Details Search with AI CAPTCHA Solver",
    version="1.0.0"
)

# Template setup
templates_dir = os.path.join(os.path.dirname(__file__), "templates")


class SearchRequest(BaseModel):
    gstin: str
    captcha: Optional[str] = None
    cookies_b64: Optional[str] = None
    auto_solve: bool = True


def serialize_cookies(session: requests.Session) -> str:
    """Serialize session cookies into a base64 encoded JSON string."""
    cookies_dict = requests.utils.dict_from_cookiejar(session.cookies)
    return base64.b64encode(json.dumps(cookies_dict).encode("utf-8")).decode("utf-8")


def deserialize_cookies(cookies_b64: str) -> requests.cookies.RequestsCookieJar:
    """Deserialize base64 encoded JSON string back to CookieJar."""
    cookies_json = base64.b64decode(cookies_b64.encode("utf-8")).decode("utf-8")
    cookies_dict = json.loads(cookies_json)
    return requests.utils.cookiejar_from_dict(cookies_dict)


@app.get("/")
async def serve_ui():
    index_path = os.path.join(templates_dir, "index.html")
    return FileResponse(index_path)



@app.get("/api/config")
async def get_config():
    api_key = os.environ.get("KRUTRIM_API_KEY", "").strip()
    has_key = bool(api_key and api_key != "your_krutrim_api_key_here")
    return {
        "krutrim_available": has_key,
        "masked_key": (api_key[:6] + "..." + api_key[-4:]) if has_key and len(api_key) > 10 else ("***" if has_key else "")
    }


@app.get("/api/captcha")
async def get_fresh_captcha():
    """Fetch a fresh CAPTCHA image and return image data URL + serialized session cookies."""
    session = make_session()
    try:
        img, raw_bytes = fetch_captcha(session)
        b64_img = base64.b64encode(raw_bytes).decode("utf-8")
        cookies_b64 = serialize_cookies(session)
        return {
            "success": True,
            "captcha_image": f"data:image/jpeg;base64,{b64_img}",
            "cookies_b64": cookies_b64
        }
    except Exception as e:
        raise HTTPException(status_code=500, detail=f"Failed to fetch CAPTCHA from GST portal: {str(e)}")


@app.post("/api/search")
async def search_taxpayer(req: SearchRequest):
    gstin = req.gstin.strip().upper()
    if len(gstin) != 15:
        raise HTTPException(status_code=400, detail="GSTIN must be exactly 15 characters long.")

    api_key = os.environ.get("KRUTRIM_API_KEY", "").strip()
    has_key = bool(api_key and api_key != "your_krutrim_api_key_here")

    # If manual captcha was provided with existing session cookies
    if req.captcha and req.cookies_b64 and not req.auto_solve:
        session = make_session()
        session.cookies = deserialize_cookies(req.cookies_b64)
        try:
            data = lookup_gstin(session, gstin, req.captcha)
            if data.get("errorCode") == "SWEB_9000":
                return JSONResponse(
                    status_code=400,
                    content={"success": False, "error": "INVALID_CAPTCHA", "message": "Wrong CAPTCHA — rejected by GST server."}
                )
            return {"success": True, "data": data, "method": "manual"}
        except Exception as e:
            raise HTTPException(status_code=500, detail=f"Lookup failed: {str(e)}")

    # Automated solving flow (up to 5 attempts)
    if req.auto_solve:
        if not has_key:
            return JSONResponse(
                status_code=400,
                content={
                    "success": False,
                    "error": "NO_API_KEY",
                    "message": "Krutrim API key is missing. Please set KRUTRIM_API_KEY in .env or solve manually."
                }
            )

        session = make_session()
        max_attempts = 5
        last_error = "Could not decode CAPTCHA"

        for attempt in range(1, max_attempts + 1):
            try:
                img, raw_bytes = fetch_captcha(session)
                captcha_text = solve_captcha_with_krutrim(raw_bytes, api_key)
                if not captcha_text:
                    last_error = f"Attempt {attempt}: AI solver returned invalid response"
                    continue

                data = lookup_gstin(session, gstin, captcha_text)
                if data.get("errorCode") == "SWEB_9000":
                    last_error = f"Attempt {attempt}: GST server rejected AI solution ({captcha_text})"
                    continue

                # Successful lookup
                return {
                    "success": True,
                    "data": data,
                    "method": "auto_ai",
                    "captcha_used": captcha_text,
                    "attempts": attempt
                }
            except Exception as e:
                last_error = f"Attempt {attempt} exception: {str(e)}"
                continue

        # If all auto attempts failed, return fresh captcha for manual fallback
        try:
            fresh_img, fresh_bytes = fetch_captcha(session)
            b64_img = base64.b64encode(fresh_bytes).decode("utf-8")
            cookies_b64 = serialize_cookies(session)
            return JSONResponse(
                status_code=422,
                content={
                    "success": False,
                    "error": "AUTO_SOLVE_FAILED",
                    "message": f"Auto CAPTCHA solving failed after {max_attempts} attempts: {last_error}",
                    "captcha_image": f"data:image/jpeg;base64,{b64_img}",
                    "cookies_b64": cookies_b64
                }
            )
        except Exception:
            raise HTTPException(status_code=500, detail=f"Auto solve failed: {last_error}")

    raise HTTPException(status_code=400, detail="Invalid request parameters.")


if __name__ == "__main__":
    import uvicorn
    port = int(os.environ.get("PORT", 4192))
    uvicorn.run("app:app", host="0.0.0.0", port=port, reload=False)


