# syntax=docker/dockerfile:1.7

FROM --platform=$BUILDPLATFORM golang:1.27.0-bookworm@sha256:484ef6066fa69acb059fdfeda7ba2b8f7391f2ef6abc6f9b8411e669ebd56466 AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY cmd ./cmd
COPY internal ./internal

ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_DATE=unknown
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -tags timetzdata -trimpath \
    -ldflags="-s -w -X github.com/j4v3l/SkyFeed/internal/app.Version=${VERSION} -X github.com/j4v3l/SkyFeed/internal/app.Commit=${COMMIT} -X github.com/j4v3l/SkyFeed/internal/app.BuildDate=${BUILD_DATE}" \
    -o /out/skyfeed ./cmd/skyfeed && \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -tags timetzdata -trimpath \
    -ldflags="-s -w -X github.com/j4v3l/SkyFeed/internal/app.Version=${VERSION} -X github.com/j4v3l/SkyFeed/internal/app.Commit=${COMMIT} -X github.com/j4v3l/SkyFeed/internal/app.BuildDate=${BUILD_DATE}" \
    -o /out/skyfeed-agent ./cmd/skyfeed-agent

FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab

COPY --from=build /out/skyfeed /skyfeed
COPY --from=build /out/skyfeed-agent /skyfeed-agent

USER nonroot:nonroot
EXPOSE 9090
ENTRYPOINT ["/skyfeed"]
CMD ["run"]
