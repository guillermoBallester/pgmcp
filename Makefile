.PHONY: build build-agent build-server build-all test test-short lint fmt vet tidy docker-build demo-up demo-down clean

BINARY := pg-mcp
PKG    := ./...

build:
	go build -o $(BINARY) ./cmd/pg-mcp

build-agent:
	go build -o pgmcp-agent ./cmd/pgmcp-agent

build-server:
	go build -o pgmcp-server ./cmd/pgmcp-server

build-all: build build-agent build-server

test:
	go test -race -count=1 $(PKG)

test-short:
	go test -short -race -count=1 $(PKG)

lint: vet
	@which golangci-lint > /dev/null 2>&1 || { echo "Install golangci-lint: https://golangci-lint.run/welcome/install/"; exit 1; }
	golangci-lint run $(PKG)

fmt:
	gofmt -w .

vet:
	go vet $(PKG)

tidy:
	go mod tidy

docker-build:
	docker compose build

demo-up:
	docker compose -f examples/demo/docker-compose.yml up -d

demo-down:
	docker compose -f examples/demo/docker-compose.yml down -v

clean:
	rm -f $(BINARY) pgmcp-agent pgmcp-server
	docker compose -f examples/demo/docker-compose.yml down -v 2>/dev/null || true
