VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS  = -s -w -X openlimit/pkg/version.Version=$(VERSION)
BINARY   = bin/openlimit-gateway

.PHONY: run test lint build tidy docker-up docker-down release docker-push test-integration clean

run:
	@bash scripts/ensure-config.sh
	go run ./cmd/gateway

test:
	go test ./...

test-integration:
	go test -tags=integration -count=1 -v ./...

lint:
	gofmt -w $$(find . -name '*.go' -not -path './sessions/*')
	go vet ./...

build:
	go build -o $(BINARY) ./cmd/gateway

release:
	CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o $(BINARY) ./cmd/gateway
	@echo "Built $(BINARY) ($(VERSION))"

# Multi-arch Docker build and push to GHCR
docker-push:
	docker buildx build \
		--build-arg VERSION=$(VERSION) \
		--platform linux/amd64,linux/arm64 \
		-t ghcr.io/$(GHCR_REPO)/openlimit-gateway:$(VERSION) \
		-t ghcr.io/$(GHCR_REPO)/openlimit-gateway:latest \
		--push .

docker-up:
	docker compose -f deploy/docker-compose.yml up --build

docker-down:
	docker compose -f deploy/docker-compose.yml down

tidy:
	go mod tidy

clean:
	rm -rf bin/ dist/
