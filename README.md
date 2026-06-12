# safe-convert

A hardened microservice that converts documents to PDF using LibreOffice. Each conversion runs inside a freshly spawned, ephemeral Docker container with no network interface, no shell, dropped capabilities, and hard resource limits. The container is destroyed immediately after the PDF is returned.

**Not intended to be exposed to the internet.** It is a private sidecar, reachable only by a trusted upstream service over a closed internal network.

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

safe-convert exposes a single HTTP endpoint that accepts a document body and returns a PDF. It is not a general-purpose conversion tool — it is built around the assumption that it will process **untrusted, potentially malicious files** and must contain any exploit within the ephemeral worker container boundary.

---

## Threat Model

Processing arbitrary user-uploaded documents is one of the more dangerous operations a backend service can perform. The threats safe-convert is designed to contain:

**Malicious document exploits.** LibreOffice has a long CVE history. A crafted `.docx` or `.odt` file can trigger memory corruption in the parser. The blast radius of any such exploit must be confined to the ephemeral worker container and must not be able to pivot to the database, object storage, or any other internal service.

**Server-Side Request Forgery (SSRF).** LibreOffice supports macros, remote template fetching (DOCX `.dotx` references), linked OLE objects, and DDE fields. Without mitigation, any of these can cause the LibreOffice process to make outbound HTTP or DNS requests from inside your Docker network, potentially reaching internal APIs or cloud metadata endpoints (e.g. `169.254.169.254`).

**Data exfiltration via network.** Even without a full exploit, a document with an embedded remote image or linked stylesheet causes LibreOffice to contact an attacker-controlled server, leaking that the file was processed and potentially leaking environment timing data.

**Resource exhaustion.** A malformed file can cause LibreOffice to hang indefinitely or consume unbounded memory. Hard timeouts and container-level memory limits are enforced on every conversion.

**Docker socket abuse.** The API process needs access to the Docker daemon to spawn worker containers. Direct socket access would let it perform arbitrary Docker operations — pull any image, create privileged containers, mount host paths. A dedicated socket proxy intercepts every Docker API call and enforces a strict allowlist of permitted operations, with image pulls restricted to exactly the pinned worker image digest.

---

## Architecture

Three services are deployed together:

```
internet
    │
    ▼
[main_network]  — your existing Docker network
    │
    ▼
[safe-convert API]
    │  also attached to main_network (Supabase, Redis, etc.)
    │
    │  DOCKER_HOST=tcp://socket-proxy:2375
    │
[socket_proxy_network]  (internal: true — no external gateway)
    │
    ▼
[socket-proxy]  ── /var/run/docker.sock (read-only)
    │
    ▼
Docker daemon
    │
    ▼  (per request)
[ephemeral worker container]
    NetworkMode: none — no network interface whatsoever
    tmpfs /tmp       — in-memory, destroyed with the container
    Lifecycle: create → copy file in → start → wait → copy PDF out → remove
```

**`cmd/proxy` — socket proxy.** A custom Go reverse proxy that sits between the API and the Docker Unix socket. It enforces a strict allowlist of permitted Docker API method + path combinations, and restricts image pulls to exactly the pinned worker image digest baked in at build time. Everything else returns 403. The proxy lives on `socket_proxy_network`, which has no gateway — it cannot reach the internet or `main_network`.

**`cmd/api` — HTTP API.** The entry point for document conversion requests. Validates the `Content-Type` header against known document MIME types, enforces auth and size limits, and dispatches to Docker exclusively through the socket proxy. Streams the document into an ephemeral worker container via `CopyToContainer`, waits for exit, streams the resulting PDF out via `CopyFromContainer`, then destroys the container.

**`cmd/worker` — LibreOffice runner.** A minimal Go binary bundled into the worker Docker image. It reads `INPUT_FILE_PATH`, invokes LibreOffice headlessly, and copies the resulting PDF to `/output.pdf` on the container's writable layer so the API can retrieve it after the container exits.

---

## Supported Formats

Requests are validated by **`Content-Type` header** — not file extension. The following MIME types are accepted:

### Word Processing

| MIME Type | Extension |
|---|---|
| `application/vnd.openxmlformats-officedocument.wordprocessingml.document` | `.docx` |
| `application/vnd.openxmlformats-officedocument.wordprocessingml.template` | `.dotx` |
| `application/msword` | `.doc` |
| `application/vnd.oasis.opendocument.text` | `.odt` |
| `application/vnd.oasis.opendocument.text-template` | `.ott` |
| `application/rtf`, `text/rtf` | `.rtf` |
| `text/plain` | `.txt` |
| `text/html` | `.html` |

### Spreadsheets

| MIME Type | Extension |
|---|---|
| `application/vnd.openxmlformats-officedocument.spreadsheetml.sheet` | `.xlsx` |
| `application/vnd.openxmlformats-officedocument.spreadsheetml.template` | `.xltx` |
| `application/vnd.ms-excel` | `.xls` |
| `application/vnd.oasis.opendocument.spreadsheet` | `.ods` |
| `application/vnd.oasis.opendocument.spreadsheet-template` | `.ots` |
| `text/csv` | `.csv` |

### Presentations

| MIME Type | Extension |
|---|---|
| `application/vnd.openxmlformats-officedocument.presentationml.presentation` | `.pptx` |
| `application/vnd.openxmlformats-officedocument.presentationml.template` | `.potx` |
| `application/vnd.ms-powerpoint` | `.ppt` |
| `application/vnd.oasis.opendocument.presentation` | `.odp` |
| `application/vnd.oasis.opendocument.presentation-template` | `.otp` |

### Drawings & Diagrams

| MIME Type | Extension |
|---|---|
| `application/vnd.oasis.opendocument.graphics` | `.odg` |
| `image/svg+xml` | `.svg` |
| `application/vnd.visio` | `.vsd` |
| `application/vnd.ms-visio.drawing` | `.vsdx` |
| `application/vnd.ms-visio.drawing.macroenabled.12` | `.vsdm` |

### What safe-convert will not process

- **Images** (`.jpg`, `.png`, `.gif`, `.tiff`) — use ImageMagick or Ghostscript
- **Existing PDFs** — use Ghostscript for PDF-to-PDF operations
- **Audio, video, archives, executables** — entirely outside scope

---

## Environment Variables

Copy `.env.example` to `.env` and fill in all required values. Variables marked **Required** have no default and will cause the service to refuse to start if unset or invalid.

All variables are read by `cmd/api` unless otherwise noted.

### Authentication

| Variable | Required | Default | Description |
|---|---|---|---|
| `SAFE_CONVERT_ACCESS_TOKEN` | **Yes** | — | Bearer token callers must present on every request. Minimum 32 characters. Generate with `openssl rand -hex 32`. |

### HTTP Server

| Variable | Required | Default | Constraints | Description |
|---|---|---------|---|---|
| `SAFE_CONVERT_PORT` | No | `8080`  | 1–65535 | Port the API listens on inside the container. |
| `SAFE_CONVERT_READ_TIMEOUT_SECS` | No | `15`    | 5–300 | Server read deadline. Defends against slow-loris attacks. |
| `SAFE_CONVERT_WRITE_TIMEOUT_SECS` | No | `30`    | 5–600 | Server write deadline for streaming the PDF response. **Must be strictly greater than `READ_TIMEOUT_SECS`.** |
| `SAFE_CONVERT_SHUTDOWN_TIMEOUT_SECS` | No | `30`    | 1–120 | Graceful shutdown window on `SIGTERM` before forced exit. |

### File Handling

| Variable | Required | Default | Constraints | Description |
|---|---|---|---|---|
| `SAFE_CONVERT_MAX_FILE_SIZE_MB` | No | `5` | 1–500 | Maximum accepted upload size in megabytes. Requests exceeding this are rejected before reaching LibreOffice. |

### Conversion

| Variable | Required | Default | Constraints | Description |
|---|---|---|---|---|
| `SAFE_CONVERT_CONVERSION_TIMEOUT_SECS` | No | `30` | 5–300 | Hard deadline on a single LibreOffice invocation. The worker container is force-removed if it does not exit within this window. |
| `SAFE_CONVERT_CONTAINER_MEMORY_MB` | No | `512` | 128–4096 | Memory limit applied to each ephemeral worker container. |
| `SAFE_CONVERT_CONTAINER_CPU_COUNT` | No | `1` | 1–8 | CPU count allocated to each ephemeral worker container. |

### Observability

| Variable | Required | Default | Allowed Values | Description |
|---|---|---|---|---|
| `SAFE_CONVERT_LOG_LEVEL` | No | `info` | `debug` `info` `warn` `error` | Minimum log level. Never use `debug` in production — it may log file metadata. |
| `SAFE_CONVERT_LOG_FORMAT` | No | `json` | `json` `text` | `json` for production log aggregation (Loki, CloudWatch); `text` for local development. |

### Debug

| Variable | Required | Default | Description |
|---|---|---|---|
| `SAFE_CONVERT_DEBUG` | No | `false` | When `true`, worker containers are preserved after conversion for manual inspection instead of being destroyed. **Never use in production.** |

### Compose-Only

These variables are used by `docker-compose.yml` for port mapping and are not read by the Go services.

| Variable | Default | Description |
|---|---|---|
| `SAFE_CONVERT_HOST_PORT` | `8080` | Host-side port bound to the API container. Independent of `SAFE_CONVERT_PORT`. |

---

## Docker & Network Configuration

### Networks

The compose file declares two networks:

```yaml
networks:
  main_network:
    external: true           # your existing network; defined outside this compose file

  socket_proxy_network:
    driver: bridge
    internal: true           # no default gateway — zero external routing
```

`internal: true` on `socket_proxy_network` is the most important network-level security control in the deployment. Docker omits the default gateway on this bridge, making it physically impossible for containers attached to it to route packets to the internet or to `main_network`. This is a topology guarantee, not a firewall rule.

`main_network` must already exist before running `docker compose up`. Create it once:

```bash
docker network create main_network
```

### Deploying

```bash
# 1. Create the external network if it does not exist
docker network create main_network

# 2. Configure environment
cp .env.example .env
# Edit .env — set SAFE_CONVERT_ACCESS_TOKEN and any overrides

# 3. Start
docker compose pull
docker compose up -d

# 4. Confirm liveness
curl http://localhost:8080/health
```

---

## API Reference

### `POST /convert`

Convert a document to PDF.

**Authentication**

```
Authorization: Bearer <SAFE_CONVERT_ACCESS_TOKEN>
```

**Request**

Send the document as the raw request body. Set `Content-Type` to the document's MIME type (see [Supported Formats](#supported-formats)).

```bash
curl -X POST http://localhost:8080/convert \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/vnd.openxmlformats-officedocument.wordprocessingml.document" \
  --data-binary @document.docx \
  -o output.pdf
```

**Response — Success**

```
HTTP 200 OK
Content-Type: application/pdf
Content-Disposition: attachment; filename="converted.pdf"
```

Body is the raw PDF binary.

**Response — Errors**

| Status | Condition |
|---|---|
| `400 Bad Request` | `Content-Type` header is missing or malformed |
| `401 Unauthorized` | `Authorization` header is missing or the token does not match |
| `413 Content Too Large` | Body exceeds `SAFE_CONVERT_MAX_FILE_SIZE_MB` |
| `415 Unsupported Media Type` | `Content-Type` is not a recognised document MIME type |
| `500 Internal Server Error` | Conversion failed, container error, or Docker issue — check logs |

### `GET /health`

Liveness probe. Returns `200 OK` with body `ok`. No authentication required.

---

## Security Hardening Reference

| Measure | Threat Addressed | Enforcement Point |
|---|---|---|
| Worker containers with `NetworkMode: none` | SSRF; any network reach from LibreOffice | Docker container config (`cmd/api`) |
| `socket_proxy_network` with `internal: true` | Internet/`main_network` reach from socket-proxy | Docker network topology |
| Docker socket proxy with strict allowlist | Arbitrary Docker API calls from the API process | `cmd/proxy` route allowlist |
| Image pull restricted to pinned worker digest | Pulling arbitrary or malicious images via the proxy | `cmd/proxy` image check |
| Static bearer token | Unauthenticated access if network misconfigured | `cmd/api` HTTP middleware |
| `cap_drop: ALL` on all services | Privilege escalation via Linux capabilities | Docker Compose |
| `no-new-privileges: true` on all services | Setuid/setgid binary exploitation | Docker Compose `security_opt:` |
| Read-only root filesystem on API and proxy | Persistence of exploit artifacts on the service containers | Compose `read_only: true` |
| tmpfs `/tmp` in API container (64 MB) | File artifact accumulation on the API | Compose `tmpfs:` |
| tmpfs `/tmp` in each worker container (256 MB) | File artifact accumulation inside LibreOffice | Docker container config (`cmd/api`) |
| Memory and CPU limits on worker containers | Resource exhaustion, fork bombs | Docker container config (`cmd/api`) |
| Non-root user (UID 65534) in API and proxy images | Container escape via root-owned process | `build.api.Dockerfile`, `build.proxy.Dockerfile` |
| Non-root user (UID 10001) in worker image | Container escape via root-owned process | `build.worker.Dockerfile` |
| Scratch base image for API and proxy | No shell, no OS tools, no attack surface post-exploit | `build.api.Dockerfile`, `build.proxy.Dockerfile` |
| Shell binaries removed from worker image | Post-exploit lateral movement | `build.worker.Dockerfile` |
| Setuid/setgid bits removed from worker image | Privilege escalation via suid binaries | `build.worker.Dockerfile` |
| Per-invocation LibreOffice `UserInstallation` | State pollution between requests | `cmd/worker` |
| LibreOffice macros disabled | VBA/Basic macro execution | `registrymodifications.xcu` in worker image |
| LibreOffice remote content disabled | SSRF via linked images/templates | `registrymodifications.xcu` in worker image |
| LibreOffice DDE disabled | DDE field evaluation as SSRF vector | `registrymodifications.xcu` in worker image |
| LibreOffice Java disabled | JVM attack surface | `registrymodifications.xcu` in worker image |
| Hard conversion timeout with force-remove | Indefinite hang on a weaponised file | `cmd/api` Docker client |
| Content-Type MIME type allowlist | Unsupported or unexpected file types reaching LibreOffice | `cmd/api` request validation |
| `MaxBytesReader` size enforcement | Disk exhaustion; decompression bombs | `cmd/api` HTTP middleware |
| Worker containers destroyed after every conversion | File artifact accumulation; container state re-use | `cmd/api` Docker client (deferred) |

---

## Development

### Prerequisites

- Docker 24+
- Docker Compose v2
- Go 1.22+
- `openssl` (for token generation)

### Local Setup

```bash
# 1. Clone
git clone https://github.com/projdocs/safe-convert
cd safe-convert

# 2. Generate a local access token
openssl rand -hex 32

# 3. Configure
cp .env.example .env
# Edit .env: paste the token into SAFE_CONVERT_ACCESS_TOKEN
# Set SAFE_CONVERT_WRITE_TIMEOUT_SECS to a value greater than SAFE_CONVERT_READ_TIMEOUT_SECS

# 4. Create the external network
docker network create main_network

# 5. Build and run
docker compose up --build

# 6. Test a conversion
curl -X POST http://localhost:8080/convert \
  -H "Authorization: Bearer $(grep SAFE_CONVERT_ACCESS_TOKEN .env | cut -d= -f2)" \
  -H "Content-Type: application/vnd.openxmlformats-officedocument.wordprocessingml.document" \
  --data-binary @sample.docx \
  -o output.pdf
```

### Building Individual Images

The worker image digest is baked into the API and proxy binaries at build time via `-ldflags`. Build the worker image first so you have a reference to pass to the other builds:

```bash
# Build the worker image
docker build -f build.worker.Dockerfile -t ghcr.io/projdocs/safe-convert-worker:dev .

# Build the API with the worker image reference baked in
docker build -f build.api.Dockerfile \
  --build-arg WORKER_IMAGE=ghcr.io/projdocs/safe-convert-worker:dev \
  -t ghcr.io/projdocs/safe-convert-api:dev .

# Build the proxy with the same worker image reference
docker build -f build.proxy.Dockerfile \
  --build-arg WORKER_IMAGE=ghcr.io/projdocs/safe-convert-worker:dev \
  -t ghcr.io/projdocs/safe-convert-proxy:dev .
```

For production, use the full digest reference (e.g. `ghcr.io/projdocs/safe-convert-worker@sha256:…`) so the pinned image cannot be silently replaced.

### Logging

Set `SAFE_CONVERT_LOG_FORMAT=text` and `SAFE_CONVERT_LOG_LEVEL=debug` in `.env` for human-readable output during development. Do not use these settings in production.
