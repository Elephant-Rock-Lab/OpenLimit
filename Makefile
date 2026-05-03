.PHONY: run test lint build tidy docker-up docker-down

run:
	@bash scripts/ensure-config.sh
	go run ./cmd/gateway

test:
	go test ./...

lint:
	gofmt -w $$(find . -name '*.go' -not -path './sessions/*')
	go vet ./...

build:
	go build -o bin/openlimit-gateway ./cmd/gateway

tidy:
	go mod tidy

docker-up:
	docker compose -f deploy/docker-compose.yml up --build

docker-down:
	docker compose -f deploy/docker-compose.yml down
