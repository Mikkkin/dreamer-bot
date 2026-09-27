# Developer shortcuts. Docker is the supported way to run the bot; these
# targets build and check it natively.

GO       ?= go
BUN      ?= bun
VERSION  ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
PLATFORMS ?= linux/amd64,linux/arm64
IMAGE    ?= dreamer-bot

.PHONY: all web build run test vet lint vuln check image image-multiarch dev-url clean

all: check build

## web: build the Mini App and copy it next to the Go embed directive
web:
	cd web && $(BUN) install --frozen-lockfile && $(BUN) run build
	find internal/webapp/dist -mindepth 1 ! -name .gitkeep -exec rm -rf {} +
	cp -R web/dist/. internal/webapp/dist/

## build: compile a static binary into ./bin (run `make web` first for the UI)
build:
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags="-s -w -X main.version=$(VERSION)" -o bin/dreamer ./cmd/dreamer

## run: run the binary natively with settings from .env and data in ./data
run: build
	set -a && . ./.env && set +a && DATA_DIR=./data ./bin/dreamer

test:
	$(GO) test -race ./...
	cd web && $(BUN) test

vet:
	$(GO) vet ./...
	@test -z "$$(gofmt -l cmd internal)" || (gofmt -l cmd internal && exit 1)

lint:
	$(GO) run honnef.co/go/tools/cmd/staticcheck@2026.2.1 ./...

vuln:
	$(GO) run golang.org/x/vuln/cmd/govulncheck@v1.8.0 ./...

## check: everything CI runs
check: vet lint test vuln
	cd web && $(BUN) run typecheck

## image: build the image for the local architecture
image:
	docker build --build-arg VERSION=$(VERSION) -t $(IMAGE):$(VERSION) -t $(IMAGE):latest .

## image-multiarch: build for amd64 and arm64 (add PUSH=1 to push to a registry)
image-multiarch:
	docker buildx build --platform $(PLATFORMS) --build-arg VERSION=$(VERSION) \
		-t $(IMAGE):$(VERSION) $(if $(PUSH),--push,) .

## dev-url: print a signed local Mini App URL for browser testing (USER_ID=<your id>)
dev-url:
	set -a && . ./.env && set +a && cd web && $(BUN) run scripts/dev-url.ts $(USER_ID)

clean:
	rm -rf bin web/dist
	find internal/webapp/dist -mindepth 1 ! -name .gitkeep -exec rm -rf {} +
