.PHONY: build build-web test test-go test-web dev-web clean

GOCACHE ?= /tmp/bazusop-go-cache

build: build-web
	mkdir -p bin
	GOCACHE=$(GOCACHE) go build -trimpath -o bin/bazusop-hub ./cmd/bazusop-hub

build-web:
	cd web && npm run build

test: test-web test-go

test-go:
	GOCACHE=$(GOCACHE) go test ./...

test-web:
	cd web && npm test

dev-web:
	cd web && npm run dev

clean:
	rm -rf bin web/dist internal/webui/dist/assets

