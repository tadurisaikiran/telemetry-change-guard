# syntax=docker/dockerfile:1.20@sha256:26147acbda4f14c5add9946e2fd2ed543fc402884fd75146bd342a7f6271dc1d

# Both stages are pinned to multi-architecture manifest digests. Update the
# matching values in release/metadata.env and re-run the distribution checks
# whenever either base is intentionally refreshed.
FROM --platform=$BUILDPLATFORM golang:1.27.0-bookworm@sha256:ded31c68586d2e49e760acc2e65a884b23d032e9bbbed0ae0c55abd3fcaf4452 AS build

ARG TARGETOS
ARG TARGETARCH
ARG TCG_VERSION=dev
ARG TCG_COMMIT=unknown
ARG TCG_BUILD_DATE=unknown
ARG TCG_DIRTY=unknown

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
COPY adapters ./adapters
COPY pkg ./pkg

RUN CGO_ENABLED=0 GOOS="${TARGETOS}" GOARCH="${TARGETARCH}" \
    go build \
      -buildvcs=false \
      -trimpath \
      -ldflags="-s -w \
        -X github.com/tadurisaikiran/telemetry-change-guard/internal/version.Version=${TCG_VERSION} \
        -X github.com/tadurisaikiran/telemetry-change-guard/internal/version.Commit=${TCG_COMMIT} \
        -X github.com/tadurisaikiran/telemetry-change-guard/internal/version.Date=${TCG_BUILD_DATE} \
        -X github.com/tadurisaikiran/telemetry-change-guard/internal/version.Dirty=${TCG_DIRTY}" \
      -o /out/telemetry-change-guard \
      ./cmd/telemetry-change-guard

FROM gcr.io/distroless/static-debian13:nonroot@sha256:1c2c046bc09ed40fad370b599a0b1ae7987f55b01e247cf27a7c27cd97e5bbc7

ARG TCG_VERSION=dev
ARG TCG_COMMIT=unknown
ARG TCG_BUILD_DATE=unknown

LABEL org.opencontainers.image.title="Telemetry Change Guard" \
      org.opencontainers.image.description="Deterministic telemetry contract change-impact analysis" \
      org.opencontainers.image.url="https://github.com/tadurisaikiran/telemetry-change-guard" \
      org.opencontainers.image.source="https://github.com/tadurisaikiran/telemetry-change-guard" \
      org.opencontainers.image.documentation="https://github.com/tadurisaikiran/telemetry-change-guard/blob/main/docs/INSTALLATION.md" \
      org.opencontainers.image.licenses="Apache-2.0" \
      org.opencontainers.image.version="${TCG_VERSION}" \
      org.opencontainers.image.revision="${TCG_COMMIT}" \
      org.opencontainers.image.created="${TCG_BUILD_DATE}"

COPY --from=build --chown=nonroot:nonroot /out/telemetry-change-guard /telemetry-change-guard

USER nonroot:nonroot
WORKDIR /workspace
ENTRYPOINT ["/telemetry-change-guard"]
CMD ["--help"]
