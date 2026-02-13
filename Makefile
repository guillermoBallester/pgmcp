.PHONY: build test test-integration lint fmt vet tidy docker-build docker-up docker-down clean

BINARY := pg-mcp
PKG    := ./...

build:
	go build -o $(BINARY) ./cmd/pg-mcp

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

docker-up:
	docker compose up -d

docker-down:
	docker compose down -v

clean:
	rm -f $(BINARY)
	docker compose down -v 2>/dev/null || true
