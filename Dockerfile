# Encorr in Docker (Linux: amd64 and arm64).
#
#   docker run -d --name encorr -p 5150:5150 -v encorr-data:/config ghcr.io/fith-g1ggle/encorr:latest
#
# Settings and data live in /config: keep it on a volume so updates keep them.
# See docs/DOCKER.md for a Portainer / docker compose stack.

# Build on the machine's own platform and cross-compile: fast, no emulation.
FROM --platform=$BUILDPLATFORM golang:1.24-alpine AS build
ARG TARGETOS TARGETARCH
ARG VERSION=dev
WORKDIR /src
ENV GOFLAGS=-mod=mod CGO_ENABLED=0
COPY go.mod go.su[m] ./
RUN go mod download
COPY . .
# The TMDB key comes in as a build secret, so it never shows in the image's
# history (it's still inside the program, as in the Windows download).
RUN --mount=type=secret,id=tmdb \
    TMDB_KEY="$(cat /run/secrets/tmdb 2>/dev/null || true)"; \
    GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath \
      -ldflags "-s -w -X github.com/FiTH-g1gGLe/encorr/internal/app.Version=${VERSION} -X github.com/FiTH-g1gGLe/encorr/internal/tmdb.BuiltInKey=${TMDB_KEY}" \
      -o /out/encorr ./cmd/encorr

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata \
 && adduser -D -H -u 1000 encorr \
 && mkdir -p /config && chown 1000:1000 /config
COPY --from=build /out/encorr /usr/local/bin/encorr
# Set TZ (e.g. Europe/London) so schedules and dates use your time.
ENV ENCORR_CONFIG=/config ENCORR_DOCKER=1
VOLUME /config
EXPOSE 5150
# Runs as user 1000 (the first user on most Linux systems). To use another,
# set "user:" in compose and make sure that user can write to /config.
USER 1000:1000
HEALTHCHECK --interval=1m --timeout=5s --start-period=30s --retries=3 \
  CMD wget -qO- http://127.0.0.1:5150/api/v1/status >/dev/null || exit 1
ENTRYPOINT ["encorr", "-no-browser", "-port", "5150"]
