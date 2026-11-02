# ⚡ GST Taxpayer Lookup & AI CAPTCHA Solver

A high-performance, lightweight Go microservice and modern web dashboard designed to fetch real-time Indian Goods and Services Tax (GST) taxpayer details directly from the official GST portal (`services.gst.gov.in`).

Featuring automated vision AI-powered CAPTCHA solving via **Ola Krutrim AI (`gemma-4-31b-it`)**, manual fallback solving, session security, and a protected RESTful API.

---

## ✨ Features

- **🤖 Automated AI CAPTCHA Solving**: Integrates with Ola Krutrim AI Vision API to automatically decode 6-digit numeric CAPTCHAs with automatic retry handling (up to 5 attempts).
- **🔄 Seamless Manual Fallback**: Built-in manual CAPTCHA solving with live CAPTCHA image streaming and cookie session persistence if AI is disabled or fails.
- **🔐 Robust Authentication & Security**:
  - Web dashboard protected by admin session cookies (`HttpOnly`, `SameSite=Lax`).
  - Programmatic REST API protected by configurable API keys.
  - Auto-generates a cryptographically secure API key if none is defined.
  - Supports `X-API-Key`, `Authorization: Bearer <token>`, and query parameter `?api_key=` auth.
- **⚡ Ultra Lightweight & Dependency-Free**:
  - Written in pure standard library Go (1.22+) without external runtime frameworks.
  - Multi-stage Docker build producing an ultra-small, lightning-fast Alpine image.
- **🎨 Modern Glassmorphism Dashboard**:
  - Dark-mode responsive web interface built with vanilla JS and CSS.
  - Displays GSTIN, Legal Name, Trade Name, Status (Active/Inactive), Taxpayer Type, Constitution of Business, Address, Jurisdiction, and E-Invoice enablement.
  - Live API key viewer with copy-to-clipboard functionality.
- **🐳 Production Ready**: Multi-stage `Dockerfile`, `docker-compose.yml`, and `/health` endpoints ready for deployment on Coolify, VPS, Kubernetes, or Cloudflare.

---

## 🛠️ Tech Stack

- **Backend**: Go 1.22+ (Native `net/http`, `net/http/cookiejar`, `crypto/tls`)
- **Frontend**: HTML5, Vanilla JavaScript, CSS3 (Modern Glassmorphism theme, FontAwesome 6, Google Fonts)
- **AI Solver**: Ola Krutrim Cloud API (`gemma-4-31b-it` vision model)
- **Containerization**: Docker & Docker Compose (Alpine Linux minimal runtime)

---

## 🚀 Quick Start

### Option 1: Run Locally with Go

#### Prerequisites
- [Go 1.22+](https://golang.org/dl/) installed.

#### Steps
1. **Clone the repository**:
   ```bash
   git clone <repository-url>
   cd GST
   ```

2. **Configure Environment Variables**:
   Copy the example `.env` file and customize your settings:
   ```bash
   cp .env.example .env
   ```

3. **Run the server**:
   ```bash
   go run main.go
   ```

4. **Access the application**:
   Open [http://localhost:4192](http://localhost:4192) in your browser.

---

### Option 2: Run with Docker Compose (Recommended)

1. **Configure Environment**:
   ```bash
   cp .env.example .env
   # Edit .env with your credentials
   ```

2. **Build and start the container**:
   ```bash
   docker compose up -d --build
   ```

3. **Check status and logs**:
   ```bash
   docker compose ps
   docker compose logs -f
   ```

4. **Access the app**:
   Navigate to [http://localhost:4192](http://localhost:4192).

---

## ⚙️ Environment Variables

Configure the application by creating a `.env` file in the root directory:

| Variable | Required | Default | Description |
| :--- | :---: | :---: | :--- |
| `PORT` | No | `4192` | Web server listening port |
| `ADMIN_USERNAME` | No | `admin` | Username for dashboard login |
| `ADMIN_PASSWORD` | Recommended | `{bcU$-kx%ndbO~qy` | Password for dashboard login |
| `APP_API_KEY` | Recommended | Auto-generated | Secret key required to access REST endpoints |
| `KRUTRIM_API_KEY` | Optional | *None* | Ola Krutrim API key for automated CAPTCHA decoding |

> **Note**: If `KRUTRIM_API_KEY` is omitted or left as default, the app automatically switches to manual CAPTCHA solving mode on the UI.

---

## 📡 REST API Documentation

All protected endpoints require authentication via either:
1. Header: `X-API-Key: <your_app_api_key>`
2. Header: `Authorization: Bearer <your_app_api_key>`
3. Query Param: `?api_key=<your_app_api_key>`
4. Active dashboard session cookie (`gst_session`)

---

### 1. Health Check
Checks service uptime and readiness.

```http
GET /health
GET /api/health
```

**Response (`200 OK`)**:
```json
{
  "status": "ok"
}
```

---

### 2. Search Taxpayer Details
Perform a GSTIN taxpayer search.

```http
POST /api/search
Content-Type: application/json
X-API-Key: your_app_api_key
```

#### Mode A: Automated AI Solving (`auto_solve: true`)
```json
{
  "gstin": "27AAPCS1084F1ZS",
  "auto_solve": true
}
```

**Success Response (`200 OK`)**:
```json
{
  "success": true,
  "method": "auto_ai",
  "captcha_used": "834192",
  "attempts": 1,
  "data": {
    "gstin": "27AAPCS1084F1ZS",
    "lgnm": "COMPANY LEGAL NAME",
    "tradeNam": "COMPANY TRADE NAME",
    "rgdt": "01/07/2017",
    "sts": "Active",
    "ctb": "Private Limited Company",
    "dty": "Tax Collector",
    "pradr": {
      "addr": {
        "bno": "101",
        "st": "Main Road",
        "loc": "Area",
        "dst": "District",
        "stcd": "State",
        "pncd": "400001"
      }
    },
    "einvoiceStatus": "Yes"
  }
}
```

---

#### Mode B: Manual Solving
Use manual mode when solving CAPTCHAs programmatically or through the fallback UI:

1. **Fetch CAPTCHA and Cookies**:
   ```http
   GET /api/captcha
   X-API-Key: your_app_api_key
   ```
   **Response (`200 OK`)**:
   ```json
   {
     "success": true,
     "captcha_image": "data:image/jpeg;base64,...",
     "cookies_b64": "ey..."
   }
   ```

2. **Submit Search with Captcha and Session Cookies**:
   ```http
   POST /api/search
   Content-Type: application/json
   X-API-Key: your_app_api_key

   {
     "gstin": "27AAPCS1084F1ZS",
     "captcha": "582910",
     "cookies_b64": "ey...",
     "auto_solve": false
   }
   ```

---

### 3. Authentication & Config Endpoints

| Endpoint | Method | Auth Required | Description |
| :--- | :---: | :---: | :--- |
| `/api/login` | `POST` | No | Authenticate user (`username`, `password`) and set session cookie |
| `/api/logout` | `POST` | No | Clear active session cookie |
| `/api/me` | `GET` | Cookie | Check current session authentication status |
| `/api/config` | `GET` | Yes | Get server configuration, Krutrim status, and active API Key |

---

## 🔒 Security Best Practices

1. **Change Default Admin Password**: Always change `ADMIN_PASSWORD` in your production `.env`.
2. **Explicit API Key**: Set an explicit, strong `APP_API_KEY` in `.env` so it persists across container rebuilds.
3. **Reverse Proxy & HTTPS**: Put the application behind a reverse proxy (e.g., Caddy, Nginx, Cloudflare Tunnels, or Coolify Traefik) to terminate SSL/TLS.

---

## 📁 Project Structure

```
GST/
├── Dockerfile              # Multi-stage minimal Alpine container build
├── docker-compose.yml      # Container orchestration with health checks
├── go.mod                  # Go module definition
├── main.go                 # Core server, API router, session & CAPTCHA logic
├── templates/
│   └── index.html          # Modern Dark-mode Glassmorphic web dashboard
├── .env.example            # Environment variables template
├── .dockerignore           # Exclusions for Docker build context
└── README.md               # Project documentation
```

---

## 📄 License

This project is licensed under the [MIT License](LICENSE).

