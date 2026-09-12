.PHONY: build build-web release release-native test test-go test-web dev-web container compose-up compose-down helm-lint clean

GOCACHE ?= /tmp/bazusop-go-cache
VERSION ?= dev
COMMIT ?= $(shell git rev-parse --short=12 HEAD 2>/dev/null || echo unknown)
BUILD_DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
VERSION_PACKAGE := github.com/gokayybaz/bazusop/internal/version
LDFLAGS := -s -w -X $(VERSION_PACKAGE).Version=$(VERSION) -X $(VERSION_PACKAGE).Commit=$(COMMIT) -X $(VERSION_PACKAGE).BuildDate=$(BUILD_DATE)

build: build-web
	mkdir -p bin
	GOCACHE=$(GOCACHE) go build -trimpath -ldflags="$(LDFLAGS)" -o bin/bazusop-hub ./cmd/bazusop-hub

build-web:
	cd web && npm run build

release:
	./scripts/build-release.sh "$(VERSION)"

release-native: release
	./scripts/build-native-packages.sh "$(VERSION)"

test: test-web test-go

test-go:
	GOCACHE=$(GOCACHE) go test ./...

test-web:
	cd web && npm test

dev-web:
	cd web && npm run dev

container:
	docker build --tag bazusop:local .

compose-up:
	docker compose up --detach --build

compose-down:
	docker compose down

helm-lint:
	docker run --rm -v "$(CURDIR):/work" alpine/helm:3.18.6 lint /work/deploy/helm/bazusop

clean:
	rm -rf bin web/dist internal/webui/dist/assets
