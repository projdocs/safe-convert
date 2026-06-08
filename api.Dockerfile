FROM golang:1.26-alpine AS build

ARG WORKER_IMAGE

WORKDIR /build

COPY go.mod go.sum ./
RUN go mod download && go mod verify

COPY . .

RUN CGO_ENABLED=0 GOOS=linux \
    go build \
      -trimpath \
      -buildvcs=false \
      -ldflags="-s -w -X 'github.com/projdocs/safe-convert/internal/docker.WorkerImage=${WORKER_IMAGE}'" \
      -o /api \
      ./cmd/api

FROM scratch

COPY --from=build /api /api

USER 65534:65534

ENTRYPOINT ["/api"]