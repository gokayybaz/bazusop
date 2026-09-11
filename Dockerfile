# syntax=docker/dockerfile:1.7

FROM node:24-alpine AS web
WORKDIR /src
COPY web/package.json web/package-lock.json ./web/
RUN --mount=type=cache,target=/root/.npm cd web && npm ci
COPY web ./web
COPY internal/webui ./internal/webui
RUN cd web && npm run build

FROM golang:1.26-alpine AS backend
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY cmd ./cmd
COPY internal ./internal
COPY --from=web /src/internal/webui/dist ./internal/webui/dist
RUN --mount=type=cache,target=/root/.cache/go-build CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/bazusop-hub ./cmd/bazusop-hub

FROM alpine:3.22
RUN apk add --no-cache ca-certificates && addgroup -g 65532 bazusop && adduser -D -H -u 65532 -G bazusop bazusop
COPY --from=backend /out/bazusop-hub /usr/local/bin/bazusop-hub
ENV BAZUSOP_HTTP_ADDR=:8080
USER 65532:65532
EXPOSE 8080
HEALTHCHECK --interval=15s --timeout=3s --start-period=5s --retries=3 CMD wget -q -O - http://127.0.0.1:8080/api/v1/health || exit 1
ENTRYPOINT ["/usr/local/bin/bazusop-hub"]
