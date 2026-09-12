# syntax=docker/dockerfile:1.7

FROM node:24-alpine AS web
WORKDIR /src
COPY web/package.json web/package-lock.json ./web/
RUN --mount=type=cache,target=/root/.npm cd web && npm ci
COPY web ./web
COPY internal/webui ./internal/webui
RUN cd web && npm run build

FROM golang:1.26-alpine AS backend
ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_DATE=unknown
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY cmd ./cmd
COPY internal ./internal
COPY --from=web /src/internal/webui/dist ./internal/webui/dist
RUN --mount=type=cache,target=/root/.cache/go-build CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w -X github.com/gokayybaz/bazusop/internal/version.Version=${VERSION} -X github.com/gokayybaz/bazusop/internal/version.Commit=${COMMIT} -X github.com/gokayybaz/bazusop/internal/version.BuildDate=${BUILD_DATE}" -o /out/bazusop-hub ./cmd/bazusop-hub

FROM alpine:3.22
ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_DATE=unknown
LABEL org.opencontainers.image.title="bazUSOP Hub" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${COMMIT}" \
      org.opencontainers.image.created="${BUILD_DATE}"
RUN apk add --no-cache ca-certificates && addgroup -g 65532 bazusop && adduser -D -H -u 65532 -G bazusop bazusop
COPY --from=backend /out/bazusop-hub /usr/local/bin/bazusop-hub
ENV BAZUSOP_HTTP_ADDR=:8080
USER 65532:65532
EXPOSE 8080
HEALTHCHECK --interval=15s --timeout=3s --start-period=5s --retries=3 CMD wget -q -O - http://127.0.0.1:8080/api/v1/health || exit 1
ENTRYPOINT ["/usr/local/bin/bazusop-hub"]
