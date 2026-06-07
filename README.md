# safe-convert

A hardened, isolated microservice that converts documents to PDF using LibreOffice. Designed to be deployed as an internal Docker sidecar with no public internet exposure——callable only by a trusted API service over a private Docker network (with no other services attached, unless necessary).

---

## Table of Contents

- [Overview](#overview)
- [Threat Model](#threat-model)
- [Architecture](#architecture)
- [Supported Formats](#supported-formats)
- [Environment Variables](#environment-variables)
- [Docker & Network Configuration](#docker--network-configuration)
- [API Reference](#api-reference)
- [Security Hardening Reference](#security-hardening-reference)
- [Development](#development)

---

## Overview

safe-convert exposes a single HTTP endpoint that accepts a document upload and returns a PDF. It is not a general-purpose conversion tool——it is built around the assumption that it will process **untrusted, potentially malicious files** and must contain any exploit within its own container boundary.

**It is not intended to be exposed to the internet.** It lives on an isolated Docker network, reachable only by your own API.

---

## Threat Model

Processing arbitrary user-uploaded documents is one of the more dangerous operations a backend service can perform. The threats safe-convert is designed to contain are:

**Malicious document exploits.** LibreOffice has a long CVE history. A crafted `.docx` or `.odt` file can trigger memory corruption in the parser. The blast radius of any such exploit must be contained to the safe-convert container alone and must not be able to pivot to the database, object storage, or any other internal service.

**Server-Side Request Forgery (SSRF).** LibreOffice supports macros, remote template fetching (DOCX remote `.dotx` references), linked OLE objects, and DDE fields. Without mitigation, any of these can cause the LibreOffice process to make outbound HTTP or DNS requests from inside your Docker network, potentially reaching your database, internal APIs, or cloud metadata endpoints (e.g. `169.254.169.254`).

**Data exfiltration via network.** Even without a full exploit, a document with an embedded remote image or linked stylesheet will cause LibreOffice to contact an attacker-controlled server, leaking that the file was processed and potentially leaking environment timing data.

**Resource exhaustion.** A malformed file can cause LibreOffice to hang indefinitely or consume unbounded memory. Hard timeouts and memory limits are enforced at both the process and container level.

---

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│  main_network  (your existing Docker network)                   │
│                                                                  │
│   [Supabase]  [Redis]  [Other Services]  [Go API] ◀────────────┼── internet
│                                                │                 │
└────────────────────────────────────────────────┼────────────────┘
                                                 │ (only the Go API
                                                 │  has a foot here)
┌────────────────────────────────────────────────┼────────────────┐
│  lo_isolation  (internal: true)                │                │
│                                                ▼                │
│                                          [Go API]               │
│                                               │                 │
│                                               ▼                 │
│                                      [safe-convert]             │
│                                                                  │
│   No gateway. Cannot reach main_network or the internet.        │
└─────────────────────────────────────────────────────────────────┘
```

The Go API is the sole bridge between the two networks. The safe-convert container has no interface on `main_network`——it cannot address the database, object storage, or any other service by name or IP, because those names and IPs do not exist from its perspective.

The Go API:
- Receives file uploads from the internet on `main_network`
- Forwards them to safe-convert over `lo_isolation`
- Retrieves the PDF and returns it to the caller
- Writes the PDF to S3 / Supabase Storage as needed

safe-convert:
- Only ever speaks to the Go API
- Has no credentials, no database access, no object storage access
- Converts the file and returns the raw PDF bytes

---

## Supported Formats

LibreOffice can convert the following input formats to PDF:

### Word Processing
| Format | Extensions |
|---|---|
| Microsoft Word | `.doc` `.docx` `.dot` `.dotx` |
| Rich Text Format | `.rtf` |
| OpenDocument Text | `.odt` `.ott` |
| Plain Text | `.txt` |
| HTML | `.html` `.htm` |

### Spreadsheets
| Format | Extensions |
|---|---|
| Microsoft Excel | `.xls` `.xlsx` `.xlsm` `.xlt` `.xltx` |
| OpenDocument Spreadsheet | `.ods` `.ots` |
| CSV / TSV | `.csv` `.tsv` |

### Presentations
| Format | Extensions |
|---|---|
| Microsoft PowerPoint | `.ppt` `.pptx` `.pot` `.potx` |
| OpenDocument Presentation | `.odp` `.otp` |

### Drawings
| Format | Extensions |
|---|---|
| OpenDocument Drawing | `.odg` |
| Microsoft Visio | `.vsd` `.vsdx` |
| SVG | `.svg` |

> **Note:** Only extensions listed in `SAFE_CONVERT_ALLOWED_EXTENSIONS` will be accepted, regardless of what LibreOffice supports. The default allowlist is conservative. Extend it deliberately.

### What safe-convert will not process
- **Images** (`.jpg`, `.png`, `.gif`, `.tiff`) — use ImageMagick or Ghostscript
- **Existing PDFs** — use Ghostscript for PDF-to-PDF operations
- **Audio, video, archives, executables** — entirely outside scope

---

## Environment Variables

Copy `.env.example` to `.env` and fill in all required values. Variables marked **Required** have no default and will cause the service to exit at startup if unset.

### Authentication

| Variable | Required | Default | Description |
|---|---|---|---|
| `SAFE_CONVERT_ACCESS_TOKEN` | **Yes** | — | Bearer token the caller must present in every request. Generate with `openssl rand -hex 32`. |

### File Handling

| Variable | Required | Default | Description |
|---|---|---|---|
| `SAFE_CONVERT_MAX_FILE_SIZE_MB` | No | `5` | Maximum accepted upload size in megabytes. Requests exceeding this are rejected with `413` before the file is written to disk. |
| `SAFE_CONVERT_ALLOWED_EXTENSIONS` | No | (empty — rejects all) | Comma-separated allowlist of permitted input extensions without dots (e.g. `docx,odt,xlsx`). If unset or empty, all uploads are rejected. Fails closed. |
| `SAFE_CONVERT_INPUT_DIR` | No | `/tmp/safe-convert/in` | Staging directory for uploaded files. Must be on tmpfs. Cleaned after every request. |
| `SAFE_CONVERT_OUTPUT_DIR` | No | `/tmp/safe-convert/out` | Directory where LibreOffice writes the PDF output. Must be on tmpfs. Cleaned after every request. |

### Conversion Behaviour

| Variable | Required | Default | Description |
|---|---|---|---|
| `SAFE_CONVERT_CONVERSION_TIMEOUT_SECS` | No | `30` | Hard deadline on a single LibreOffice invocation. Process is sent `SIGKILL` if it does not exit within this window. |
| `SAFE_CONVERT_MAX_CONCURRENT_CONVERSIONS` | No | `1` | Semaphore limit on parallel LibreOffice processes. LibreOffice can consume 500 MB+ per conversion; do not raise this without load testing. |

### HTTP Server

| Variable | Required | Default | Description |
|---|---|---|---|
| `SAFE_CONVERT_PORT` | No | `8080` | Port the HTTP service listens on inside the container. |
| `SAFE_CONVERT_READ_TIMEOUT_SECS` | No | `60` | Server read deadline including the uploaded file body. Defends against slow-loris attacks. |
| `SAFE_CONVERT_WRITE_TIMEOUT_SECS` | No | `90` | Server write deadline for streaming the PDF response. Must exceed `CONVERSION_TIMEOUT_SECS`. |
| `SAFE_CONVERT_SHUTDOWN_TIMEOUT_SECS` | No | `30` | Graceful shutdown window on `SIGTERM` before forced exit. |

### Observability

| Variable | Required | Default | Description |
|---|---|---|---|
| `SAFE_CONVERT_LOG_LEVEL` | No | `info` | Minimum log level. One of: `debug`, `info`, `warn`, `error`. Never use `debug` in production——it may log file metadata. |
| `SAFE_CONVERT_LOG_FORMAT` | No | `json` | Log format. `json` for production aggregation (Loki, CloudWatch); `text` for local development. |

---

## Docker & Network Configuration

### Networks

Two Docker networks are required:

```yaml
networks:
  main_network:
    external: true          # your existing network; defined elsewhere

  lo_isolation:
    driver: bridge
    internal: true          # no default gateway; zero external routing
```

The `internal: true` flag is the most important security control in the entire deployment. It instructs Docker to omit the default gateway on the `lo_isolation` bridge, making it physically impossible for containers on it to route packets outside the network. This is a topology guarantee, not a firewall rule.

### Service Definitions

```yaml
services:
  api:
    image: your-api-image
    networks:
      - main_network          # reaches Supabase, Redis, S3, etc.
      - lo_isolation          # reaches safe-convert only
    environment:
      - SAFE_CONVERT_URL=http://safe-convert:8080
      - SAFE_CONVERT_ACCESS_TOKEN=${SAFE_CONVERT_ACCESS_TOKEN}

  safe-convert:
    image: your-safe-convert-image
    networks:
      - lo_isolation          # sole network attachment — cannot see main_network
    env_file: .env
    read_only: true
    tmpfs:
      - /tmp/safe-convert/in:size=512m,mode=1777
      - /tmp/safe-convert/out:size=512m,mode=1777
      - /home/svcuser/.config:size=64m
    cap_drop:
      - ALL
    security_opt:
      - no-new-privileges:true
    user: "10001:10001"
    mem_limit: 1g
    cpus: "1.0"
    ulimits:
      nproc: 64
      nofile:
        soft: 1024
        hard: 2048
```

### Why Two Networks

The Go API has one network interface on `main_network` and one on `lo_isolation`. This makes it the **sole bridge** between the trusted internal network and the isolated conversion sidecar.

From the safe-convert container's perspective, Supabase, Redis, and every other service on `main_network` do not exist. Docker's internal DNS does not resolve their names, and there is no IP route to reach them even by address.

A compromised safe-convert container can only address the Go API over `lo_isolation`——which is why the bearer token provides a meaningful second layer: even if network isolation were somehow misconfigured, an unauthenticated request to any endpoint would be rejected.

---

## API Reference

### `POST /convert`

Convert a document to PDF.

**Authentication**

```
Authorization: Bearer <SAFE_CONVERT_ACCESS_TOKEN>
```

**Request**

```
Content-Type: multipart/form-data
```

| Field | Type | Description |
|---|---|---|
| `file` | File | The document to convert. Extension must be in `SAFE_CONVERT_ALLOWED_EXTENSIONS`. |

**Response — Success**

```
HTTP 200 OK
Content-Type: application/pdf
Content-Disposition: attachment; filename="<original-stem>.pdf"
```

Body is the raw PDF binary.

**Response — Errors**

| Status | Condition |
|---|---|
| `400 Bad Request` | Missing `file` field, or no filename provided |
| `401 Unauthorized` | Missing or invalid `Authorization` header |
| `413 Content Too Large` | File exceeds `SAFE_CONVERT_MAX_FILE_SIZE_MB` |
| `415 Unsupported Media Type` | Extension not in `SAFE_CONVERT_ALLOWED_EXTENSIONS` |
| `422 Unprocessable Entity` | LibreOffice exited non-zero (corrupt or unreadable file) |
| `408 Request Timeout` | LibreOffice exceeded `SAFE_CONVERT_CONVERSION_TIMEOUT_SECS` |
| `503 Service Unavailable` | Concurrency limit reached; caller should retry with backoff |
| `500 Internal Server Error` | Unexpected failure; check logs |

### `GET /health`

Liveness probe. Returns `200 OK` with body `ok`. No authentication required.

---

## Security Hardening Reference

This section documents every hardening measure applied, the threat it addresses, and where it is enforced.

| Measure | Threat Addressed | Enforcement Point |
|---|---|---|
| `lo_isolation` network with `internal: true` | SSRF; lateral movement to DB/S3 | Docker network topology |
| safe-convert attached to `lo_isolation` only | Any network reach to `main_network` services | Docker Compose `networks:` |
| Static bearer token | Unauthenticated access if network misconfigured | Go HTTP middleware |
| Non-root user (UID 10001) | Container escape via root-owned processes | `Dockerfile` + Compose `user:` |
| `cap_drop: ALL` | Privilege escalation via Linux capabilities | Docker Compose |
| `no-new-privileges: true` | Setuid/setgid binary exploitation | Docker Compose `security_opt:` |
| Read-only root filesystem | Persistence of exploit artifacts | Compose `read_only: true` |
| tmpfs for input/output/config | File artifacts cannot survive container restart | Compose `tmpfs:` |
| Memory and CPU limits | Resource exhaustion / fork bombs | Compose `mem_limit:` / `cpus:` |
| `ulimits` on nproc / nofile | Fork bombs; file descriptor exhaustion | Compose `ulimits:` |
| No shell in final image | Post-exploit lateral movement | `Dockerfile` (`rm -f /bin/sh /bin/bash`) |
| No package manager in final image | Installing attacker tools post-exploit | Multi-stage `Dockerfile` |
| Setuid/setgid bits removed | Privilege escalation via suid binaries | `Dockerfile` (`find / -perm -4000`) |
| Per-request ephemeral `UserInstallation` | LibreOffice state pollution between requests | Go conversion handler |
| LibreOffice macros disabled | VBA/Basic macro execution | `registrymodifications.xcu` |
| LibreOffice remote content disabled | SSRF via linked images/templates | `registrymodifications.xcu` |
| LibreOffice DDE disabled | DDE field evaluation as SSRF vector | `registrymodifications.xcu` |
| LibreOffice Java disabled | JVM attack surface | `registrymodifications.xcu` |
| Hard conversion timeout (`SIGKILL`) | Indefinite hang on weaponised file | `exec.CommandContext` |
| Extension allowlist | Processing of unexpected file types | Go request validation |
| Max file size enforcement | Disk exhaustion; decompression bombs | Go request validation |
| Input/output cleaned after every request | File artifacts accumulating on tmpfs | Go conversion handler (deferred) |

---

## Development

### Prerequisites

- Docker 24+
- Docker Compose v2
- Go 1.22+
- `openssl` (for token generation)

### Local Setup

```bash
# 1. Clone the repository
git clone https://github.com/projdocs/safe-convert
cd safe-convert

# 2. Generate a local access token
openssl rand -hex 32

# 3. Configure environment
cp .env.example .env
# Edit .env — paste the token into SAFE_CONVERT_ACCESS_TOKEN

# 4. Build and run
docker compose up --build

# 5. Test a conversion
curl -s \
  -H "Authorization: Bearer $(grep ACCESS_TOKEN .env | cut -d= -f2)" \
  -F "file=@/path/to/document.docx" \
  http://localhost:8080/convert \
  -o output.pdf
```

### Running Without Docker (for Go development)

LibreOffice must be installed locally.

```bash
go run ./cmd/server
```

### Logging

In development, set `SAFE_CONVERT_LOG_FORMAT=text` and `SAFE_CONVERT_LOG_LEVEL=debug` in `.env` for human-readable output. Do not use these settings in production.
