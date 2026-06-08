FROM golang:1.26-alpine AS builder

ARG WORKER_IMAGE

WORKDIR /build

COPY go.mod ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux \
    go build \
      -trimpath \
      -ldflags="-s -w -X 'main.WorkerImage=${WORKER_IMAGE}'" \
      -o /proxy \
      ./cmd/proxy

# ── Stage 2: runtime ──────────────────────────────────────────────
# FROM scratch — no shell, no OS, nothing but the static binary.
FROM scratch

COPY --from=builder /proxy /proxy

# Proxy listens on 2375 by default.
EXPOSE 2375

USER 65534:65534

ENTRYPOINT ["/proxy"]