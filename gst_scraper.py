import os
import re
import json
import random
import base64
import requests
from PIL import Image
from io import BytesIO

# ─────────────────────────────────────────────
#  Environment / Config Loader
# ─────────────────────────────────────────────

def load_env_file(filepath=None):
    """Simple parser to load key=value pairs from a .env file if present."""
    if filepath is None:
        script_dir = os.path.dirname(os.path.abspath(__file__))
        filepath = os.path.join(script_dir, ".env")
    if not os.path.exists(filepath):
        return
    with open(filepath, "r", encoding="utf-8") as f:
        for line in f:
            line = line.strip()
            if not line or line.startswith("#"):
                continue
            if "=" in line:
                key, val = line.split("=", 1)
                key = key.strip()
                val = val.strip().strip("'\"")
                if key:
                    os.environ[key] = val

load_env_file()



# ─────────────────────────────────────────────
#  Helpers
# ─────────────────────────────────────────────

def make_session():
    session = requests.Session()
    session.headers.update({
        "User-Agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) "
                      "AppleWebKit/537.36 (KHTML, like Gecko) "
                      "Chrome/120.0.0.0 Safari/537.36",
        "Origin":  "https://services.gst.gov.in",
        "Referer": "https://services.gst.gov.in/services/searchtp",
    })
    return session


def fetch_captcha(session):
    """Download a fresh CAPTCHA and return the PIL Image + raw bytes."""
    rnd = random.random()
    url = f"https://services.gst.gov.in/services/captcha?rnd={rnd}"
    resp = session.get(url, timeout=10)
    resp.raise_for_status()
    raw = resp.content
    img = Image.open(BytesIO(raw))
    return img, raw


def show_captcha(img):
    """
    Display the CAPTCHA image in the default system viewer.
    The image is scaled up 4× so it's easier to read on screen.
    """
    scaled = img.resize((img.width * 4, img.height * 4), Image.NEAREST)
    scaled.show(title="GST CAPTCHA — type what you see")


def solve_captcha_with_krutrim(raw_bytes, api_key):
    """
    Call Ola Krutrim Vision API (gemma-4-31b-it) to solve the CAPTCHA.
    Returns a 6-digit string or None on failure.
    """
    if not api_key or api_key.strip() == "your_krutrim_api_key_here":
        return None

    b64_image = base64.b64encode(raw_bytes).decode("utf-8")
    image_data_url = f"data:image/jpeg;base64,{b64_image}"

    url = "https://cloud.olakrutrim.com/v1/chat/completions"
    headers = {
        "Authorization": f"Bearer {api_key.strip()}",
        "Content-Type": "application/json"
    }
    payload = {
        "model": "gemma-4-31b-it",
        "messages": [
            {
                "role": "user",
                "content": [
                    {
                        "type": "image_url",
                        "image_url": {
                            "url": image_data_url
                        }
                    },
                    {
                        "type": "text",
                        "text": "Decode this Captcha, there will be always number from 0-9 and it will have 6 digits, in the response just give the decoded number"
                    }
                ]
            }
        ]
    }

    try:
        resp = requests.post(url, headers=headers, json=payload, timeout=20)
        if resp.status_code != 200:
            print(f"  [!] Krutrim API returned HTTP status {resp.status_code}: {resp.text}")
            return None
        res_json = resp.json()
        content = res_json["choices"][0]["message"]["content"]
        # Extract digits from response
        digits = re.sub(r"\D", "", content)
        if len(digits) == 6:
            return digits
        print(f"  [!] Krutrim output raw: '{content.strip()}' (extracted digits: '{digits}')")
        return None
    except Exception as e:
        print(f"  [!] Krutrim API call exception: {e}")
        return None



def lookup_gstin(session, gstin, captcha_text):
    """POST the GSTIN + captcha and return the JSON response dict."""
    url     = "https://services.gst.gov.in/services/api/search/taxpayerDetails"
    payload = {"gstin": gstin.strip().upper(), "captcha": captcha_text.strip()}
    resp    = session.post(url, json=payload, timeout=10)
    resp.raise_for_status()
    return resp.json()


# ─────────────────────────────────────────────
#  Pretty-print the result
# ─────────────────────────────────────────────

FIELD_LABELS = {
    "gstin":             "GSTIN",
    "tradeNam":          "Trade Name",
    "lgnm":              "Legal Name",
    "ctb":               "Constitution of Business",
    "sts":               "GST Status",
    "dty":               "Taxpayer Type",
    "rgdt":              "Registration Date",
    "cxdt":              "Cancellation Date",
    "stj":               "State Jurisdiction",
    "ctj":               "Central Jurisdiction",
    "nba":               "Nature of Business",
    "lstupdt":           "Last Updated",
    "einvoiceStatus":    "E-Invoice Status",
    "adhrVFlag":         "Aadhaar Verified",
}


def print_details(data):
    print("\n" + "=" * 55)
    print("  GST TAXPAYER DETAILS")
    print("=" * 55)

    if "errorCode" in data:
        print(f"  [!] Server returned error: {data.get('errorCode')}")
        print(f"      Message : {data.get('message', 'No message')}")
        print("=" * 55)
        return

    for key, label in FIELD_LABELS.items():
        value = data.get(key)
        if value is None:
            continue
        # Lists → comma-separated
        if isinstance(value, list):
            value = ", ".join(str(v) for v in value)
        print(f"  {label:<28}: {value}")

    # Principal place of business address
    pradr = data.get("pradr") or {}
    addr_parts = pradr.get("adr", "")
    if addr_parts:
        print(f"  {'Principal Address':<28}: {addr_parts}")

    print("=" * 55)

    # Dump any unexpected keys in case the API adds new fields
    known = set(FIELD_LABELS.keys()) | {"pradr", "adadr", "errorCode", "message"}
    extras = {k: v for k, v in data.items() if k not in known}
    if extras:
        print("\n  [Additional raw data]")
        print(json.dumps(extras, indent=4))


# ─────────────────────────────────────────────
#  Main interactive loop
# ─────────────────────────────────────────────

def main():
    print("\n" + "=" * 55)
    print("  GST Taxpayer Lookup Tool")
    print("=" * 55)

    # Step 1 – ask for GSTIN
    while True:
        gstin = input("\n  Enter GSTIN (15 characters): ").strip().upper()
        if len(gstin) == 15:
            break
        print("  [!] A GSTIN must be exactly 15 characters. Please try again.")

    session = make_session()
    load_env_file()
    krutrim_api_key = os.environ.get("KRUTRIM_API_KEY", "").strip()


    # Step 2 – Automated CAPTCHA solving with Krutrim (up to 5 attempts)
    max_auto_attempts = 5
    auto_success = False

    if krutrim_api_key and krutrim_api_key != "your_krutrim_api_key_here":
        masked_key = krutrim_api_key[:6] + "..." + krutrim_api_key[-4:] if len(krutrim_api_key) > 10 else "***"
        print(f"\n  [*] Krutrim API key detected ({masked_key}). Attempting automated CAPTCHA solving (max {max_auto_attempts} attempts)...")

        for attempt in range(1, max_auto_attempts + 1):
            print(f"\n  [Auto Attempt {attempt}/{max_auto_attempts}] Fetching CAPTCHA image...")
            try:
                captcha_img, raw_bytes = fetch_captcha(session)
            except Exception as e:
                print(f"  [!] Could not fetch CAPTCHA: {e}")
                continue

            print("  [*] Requesting Krutrim AI to decode CAPTCHA...")
            captcha_text = solve_captcha_with_krutrim(raw_bytes, krutrim_api_key)

            if not captcha_text:
                print("  [!] Could not get valid 6-digit CAPTCHA solution from Krutrim AI.")
                continue

            print(f"  [*] AI decoded CAPTCHA: {captcha_text}")
            print("  Submitting...")

            try:
                data = lookup_gstin(session, gstin, captcha_text)
            except Exception as e:
                print(f"  [!] Request failed: {e}")
                continue

            if data.get("errorCode") == "SWEB_9000":
                print("  [!] GST Server rejected AI CAPTCHA solution.")
                continue

            # Success or valid API result returned
            print_details(data)
            auto_success = True
            break
    else:
        print("\n  [!] Krutrim API key is missing or not set in .env. Skipping automated solving.")

    if auto_success:
        return

    print("\n  [!] Automated CAPTCHA solving was unsuccessful or skipped.")
    print("  [*] Switching to manual CAPTCHA entry mode...")

    # Step 3 – Fallback: manual CAPTCHA solving loop
    while True:
        print("\n  Fetching CAPTCHA image...")
        try:
            captcha_img, raw_bytes = fetch_captcha(session)
        except Exception as e:
            print(f"  [!] Could not fetch CAPTCHA: {e}")
            retry = input("  Retry? (y/n): ").strip().lower()
            if retry != "y":
                return
            continue

        # Open the image in the system viewer
        show_captcha(captcha_img)
        print("  [*] The CAPTCHA image has been opened in your image viewer.")
        print("  [*] Look at the 6-digit number and type it below.")

        captcha_text = input("\n  Enter CAPTCHA (6 digits): ").strip()

        if len(captcha_text) != 6 or not captcha_text.isdigit():
            print("  [!] That doesn't look like 6 digits. Getting a fresh CAPTCHA...\n")
            continue

        print("\n  Submitting...")
        try:
            data = lookup_gstin(session, gstin, captcha_text)
        except requests.HTTPError as e:
            print(f"  [!] HTTP error: {e}")
            retry = input("  Try a new CAPTCHA? (y/n): ").strip().lower()
            if retry != "y":
                return
            continue
        except Exception as e:
            print(f"  [!] Request failed: {e}")
            return

        if data.get("errorCode") == "SWEB_9000":
            print("  [!] Wrong CAPTCHA — the server rejected it.")
            retry = input("  Try again with a new CAPTCHA? (y/n): ").strip().lower()
            if retry != "y":
                return
            continue

        print_details(data)
        break


if __name__ == "__main__":
    main()