# ── Stage 1: build the worker binary ──────────────────────────────
FROM golang:1.26-alpine AS builder

WORKDIR /build

COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Build a fully static binary — no libc, no dynamic linker required.
# The hardened runtime stage has neither.
RUN CGO_ENABLED=0 GOOS=linux \
    go build \
      -trimpath \
      -ldflags="-s -w" \
      -o /worker \
      ./cmd/worker

# ── Stage 2: installer ────────────────────────────────────────────
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
    && apt-get clean \
    && rm -rf \
        /var/lib/apt/lists/* \
        /var/cache/apt/archives/* \
        /tmp/* \
        /var/tmp/*

RUN fc-cache -fv

# ── Stage 3: hardened runtime ─────────────────────────────────────
FROM debian:12-slim AS runtime

COPY --from=installer /usr/lib/libreoffice   /usr/lib/libreoffice
COPY --from=installer /usr/bin/libreoffice   /usr/bin/libreoffice
COPY --from=installer /usr/share/libreoffice /usr/share/libreoffice
COPY --from=installer /usr/share/fonts       /usr/share/fonts
COPY --from=installer /etc/fonts             /etc/fonts
COPY --from=installer /var/cache/fontconfig  /var/cache/fontconfig

# Copy the static worker binary — no runtime dependencies.
COPY --from=builder /worker /worker

RUN groupadd --gid 10001 svcgroup && \
    useradd \
      --uid 10001 \
      --gid 10001 \
      --no-create-home \
      --shell /usr/sbin/nologin \
      --comment "LibreOffice service account" \
      svcuser

# Remove setuid/setgid bits from every binary in the image.
RUN find / -xdev \( -perm -4000 -o -perm -2000 \) -exec chmod ug-s {} + 2>/dev/null || true

# Remove shells pulled in transitively.
RUN rm -f \
    /bin/sh \
    /bin/bash \
    /bin/dash \
    /usr/bin/sh \
    /usr/bin/bash \
    /usr/bin/perl \
    /usr/bin/python3 \
    2>/dev/null || true

USER svcuser

ENTRYPOINT ["/worker"]