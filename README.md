# 🚀 LinkProxy (`url-proxy`)

[![Go Version](https://img.shields.io/badge/Go-1.24+-00ADD8?style=flat&logo=go)](https://golang.org)
[![SQLite](https://img.shields.io/badge/SQLite-Pure--Go-003B57?style=flat&logo=sqlite)](https://modernc.org/sqlite)
[![Docker](https://img.shields.io/badge/Docker-Ready-2496ED?style=flat&logo=docker)](https://www.docker.com/)
[![License](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

**LinkProxy** is a lightweight, high-performance streaming HTTP proxy and link masking gateway written in Go. It converts arbitrary upstream file URLs into clean, stable, deterministic short URLs while streaming payloads with constant, bounded memory usage regardless of file size.

It comes equipped with an embedded HTMX admin dashboard, real-time traffic telemetry, byte-range streaming (seeking/resumable downloads), SSRF protection, deterministic URL canonicalization, and zero-CGO SQLite persistence.

---

## ✨ Features

- **⚡ Zero-Spike Streaming Pipeline**: Uses pooled `64 KB` buffers via `sync.Pool` to stream multi-gigabyte files with strictly bounded $O(1)$ memory overhead. Zero temporary disk writes.
- **⏯️ Full HTTP Range Request Support**: Passes `Range` and `Content-Range` headers transparently for media seeking (video/audio scrubbers) and interrupted download resumption (`curl -C -`).
- **🛡️ Built-in SSRF Protection**: Proactively validates every destination IP against loopback, private IPv4/IPv6 subnets, link-local, and non-HTTP protocols across upstream requests and redirect chains.
- **🔗 Smart URL Canonicalization & Deduplication**: Strips tracking parameters (`utm_*`, `fbclid`, `gclid`), sorts query params, cleans redundant path slashes, and generates collision-resistant 16-character Base62 IDs from SHA-256 hashes.
- **📊 Embedded HTMX + Tailwind Dashboard**: Track bandwidth served, access counts, file records with instant search and pagination, and view real-time request metrics—no external frontend build toolchain required.
- **💾 Pure-Go SQLite Persistence**: Powered by `modernc.org/sqlite` running in WAL mode with tuned PRAGMAs. Zero CGO dependencies for seamless cross-compilation and lightweight container footprints.
- **🏎️ Optional Redis Accelerator**: Caches file metadata and tracks fast counters when Redis is present. **Gracefully degrades** to pure SQLite if Redis is unreachable or omitted.
- **🔒 Authenticated Admin API**: Protected management endpoints using HTTP Basic Auth, while public streaming links remain fast and unencumbered.

---

## 🏗️ Architecture

```
                                  +---------------------------+
                                  |  Web Dashboard / Admin    |
                                  |    (HTMX + Tailwind)      |
                                  +-------------+-------------+
                                                | Basic Auth
                                                v
[ Client / Browser ] ----( HTTP GET /p/{id} )---> [ LinkProxy Gateway ]
                                                |
                                                +---> SQLite (proxy.db, WAL)
                                                +---> Redis (Optional Metadata Cache)
                                                |
                                                v (SSRF Filter & 64KB Buffer Pool)
                                  [ Upstream Remote Servers ]
                                     (S3, CDN, External Hosts)
```

---

## ⚡ Quickstart

### Option 1: Docker Compose (Recommended)

Run LinkProxy and an optional Redis cache in seconds:

```bash
git clone https://github.com/izzudd/url-proxy.git
cd url-proxy
docker compose up -d
```

Open your browser at **[http://localhost:8080/dashboard](http://localhost:8080/dashboard)**:
- **Username**: `admin`
- **Password**: `admin`

### Option 2: Run from Source

Make sure you have [Go](https://golang.org/dl/) installed:

```bash
# Clone the repository
git clone https://github.com/izzudd/url-proxy.git
cd url-proxy

# Run the server directly
go run ./cmd/server
```

The server will automatically create `proxy.db` in your current working directory and listen on port `8080`.

---

## 📖 How to Use

### 1. Register a URL

Send a `POST` request with the upstream target URL to generate a proxied link:

```bash
curl -X POST http://localhost:8080/api/proxy \
  -u admin:admin \
  -H "Content-Type: application/json" \
  -d '{"url":"https://images.unsplash.com/photo-1579546929518-9e396f3cc809"}'
```

**Response (`200 OK`):**
```json
{
  "id": "1y8XbA3p0Qz2",
  "proxy_url": "http://localhost:8080/p/1y8XbA3p0Qz2",
  "filename": "photo-1579546929518-9e396f3cc809.jpg",
  "content_type": "image/jpeg",
  "size": 1845203
}
```

### 2. Stream / Download Through the Proxy

Use the generated `proxy_url` to stream or download:

```bash
# Direct download with original filename
curl -O -J http://localhost:8080/p/1y8XbA3p0Qz2

# Resumable download or range query (first 1 KB)
curl -r 0-1023 http://localhost:8080/p/1y8XbA3p0Qz2 -o chunk.part
```

### 3. Explore the Dashboard

Navigate to `http://localhost:8080/dashboard` to:
- View live aggregated bandwidth, total registered links, and request counts.
- Search and filter proxied links by name, URL, or ID.
- Register new URLs directly via the web form.
- Review interactive API documentation under `/docs`.

---

## 🛠️ API Reference

| Endpoint | Method | Auth | Description |
| :--- | :--- | :--- | :--- |
| `/p/{id}` | `GET` | Public | Stream or download the file mapped to `{id}`. |
| `/health` | `GET` | Public | Healthcheck endpoint (`{"status": "ok"}`). |
| `/api/proxy` | `POST` | Basic Auth | Canonicalize, probe upstream, and register a new proxy URL. |
| `/dashboard` | `GET` | Basic Auth | Web UI dashboard. |
| `/docs` | `GET` | Basic Auth | Embedded interactive API documentation. |
| `/api/dashboard/stats` | `GET` | Basic Auth | HTMX fragment: live bandwidth & counts. |
| `/api/dashboard/chart` | `GET` | Basic Auth | JSON metrics for Chart.js traffic visualization. |
| `/api/dashboard/files` | `GET` | Basic Auth | HTMX fragment: paginated and searchable file list. |

---

## ⚙️ Configuration

LinkProxy is configured entirely through environment variables:

| Variable | Default | Description |
| :--- | :--- | :--- |
| `PORT` | `8080` | Port for the HTTP server to listen on. |
| `BASE_URL` | `http://localhost:8080` | Base public URL used when constructing generated short links. |
| `DB_PATH` | `proxy.db` | Filesystem path for the SQLite database. |
| `REDIS_ADDR` | `localhost:6379` | Optional Redis address. If unreachable, LinkProxy runs standalone. |
| `SMALL_FILE_LIMIT` | `2097152` (2 MB) | Threshold (bytes) under which small payloads can be cached in Redis. |
| `ADMIN_USER` | `admin` | Username for dashboard and management endpoints. |
| `ADMIN_PASS` | `admin` | Password for dashboard and management endpoints. |
| `TRUSTED_CLIENT_IP_HEADER` | `""` | Optional proxy header for client IP resolution (e.g. `CF-Connecting-IP`, `X-Forwarded-For`). |

---

## 🛡️ Security Details

- **SSRF Hardening**: All target hostnames are resolved before connection. Connections to RFC 1918 private subnets (`10.0.0.0/8`, `172.16.0.0/12`, `192.168.0.0/16`), loopback (`127.0.0.0/8`), link-local (`169.254.0.0/16`), and IPv6 private equivalents are blocked immediately. Upstream redirects are tracked and re-validated at every hop up to 10 redirects.
- **Deterministic Canonicalization**: URLs are normalized before hashing (scheme/host lowercased, default ports removed, path segments cleaned, tracking tokens deleted, query parameters deterministically sorted). This guarantees identical source links always share the exact same ID, preventing cache poisoning and database bloat.
- **Bounded Buffer Memory**: Streaming does not accumulate buffers in RAM and does not write temporary files to the disk, protecting against out-of-memory (OOM) denial-of-service attempts.

---

## 🧪 Testing

Run the test suite including canonicalization, database routines, and proxy streaming mocks:

```bash
go test -v ./...
```

Run tests with race detection:

```bash
go test -race ./...
```

---

## 📄 License

This project is licensed under the [MIT License](LICENSE).
