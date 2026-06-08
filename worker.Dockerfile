# ── Stage 1: build the worker binary ──────────────────────────────
FROM golang:1.26-alpine AS builder

WORKDIR /build

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux \
    go build \
      -trimpath \
      -ldflags="-s -w" \
      -o /worker \
      ./cmd/worker

# ── Stage 2: installer ──────────────────────────────────────────────
FROM debian:12-slim AS installer

RUN apt-get update && \
    apt-get install -y --no-install-recommends \
        libreoffice-writer \
        libreoffice-calc \
        libreoffice-impress \
        libreoffice-draw \
        fonts-liberation \
        fonts-liberation2 \
        fonts-noto-core \
        fonts-crosextra-carlito \
        fonts-crosextra-caladea \
        libxinerama1 \
        libxrandr2 \
        libxrender1 \
        fontconfig \
    && fc-cache -fv \
    && apt-get clean \
    && rm -rf \
        /var/lib/apt/lists/* \
        /var/cache/apt/archives/* \
        /tmp/* \
        /var/tmp/*

# ── Stage 3: hardened runtime ─────────────────────────────────────
FROM installer AS runner

COPY --from=builder /worker /worker

RUN groupadd --gid 10001 svcgroup && \
    useradd \
      --uid 10001 \
      --gid 10001 \
      --no-create-home \
      --shell /usr/sbin/nologin \
      --comment "LibreOffice service account" \
      svcuser

# Pre-create /output.pdf owned by svcuser with mode 600.
# svcuser can write to this exact file but cannot create any other
# files on the root filesystem — principle of least privilege.
RUN touch /output.pdf && \
    chown svcuser:svcgroup /output.pdf && \
    chmod 600 /output.pdf

# Remove setuid/setgid bits from every binary in the image.
RUN find / -xdev \( -perm -4000 -o -perm -2000 \) -exec chmod ug-s {} + 2>/dev/null || true

RUN rm -f \
    /bin/bash \
    /usr/bin/bash \
    /usr/bin/perl \
    /usr/bin/python3 \
    2>/dev/null || true

USER svcuser

ENTRYPOINT ["/worker"]