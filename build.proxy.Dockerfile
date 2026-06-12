FROM golang:1.26-alpine AS builder

ARG WORKER_IMAGE

WORKDIR /build

COPY go.mod ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux \
    go build \
      -trimpath \
      -ldflags="-s -w -X 'github.com/projdocs/safe-convert/internal/docker.WorkerImage=${WORKER_IMAGE}'" \
      -o /proxy \
      ./cmd/proxy

# ── Stage 2: runtime ──────────────────────────────────────────────
FROM scratch

COPY --from=builder /proxy /proxy

ENTRYPOINT ["/proxy"]