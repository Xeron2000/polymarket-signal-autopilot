# syntax=docker/dockerfile:1.7

FROM --platform=$BUILDPLATFORM golang:1.22-bookworm AS builder
WORKDIR /src

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY . .

ARG TARGETOS=linux
ARG TARGETARCH=amd64

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags='-s -w' -o /out/api ./cmd/api && \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags='-s -w' -o /out/worker ./cmd/worker && \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags='-s -w' -o /out/assetpicker ./cmd/assetpicker

FROM gcr.io/distroless/static-debian12:nonroot AS api
WORKDIR /app
COPY --from=builder /out/api /app/api
EXPOSE 8080
ENTRYPOINT ["/app/api"]

FROM gcr.io/distroless/static-debian12:nonroot AS worker
WORKDIR /app
COPY --from=builder /out/worker /app/worker
EXPOSE 9091
ENTRYPOINT ["/app/worker"]

FROM gcr.io/distroless/static-debian12:nonroot AS assetpicker
WORKDIR /app
COPY --from=builder /out/assetpicker /app/assetpicker
ENTRYPOINT ["/app/assetpicker"]
