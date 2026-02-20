.PHONY: build-agent build-server build-all test test-short lint fmt vet tidy \
        docker-build docker-up docker-down clean

build-agent:
	go build -o bin/isthmus-agent ./cmd/isthmus-agent

build-server:
	go build -o bin/isthmus-server ./cmd/isthmus-server

build-all: build-agent build-server

test:
	go test -race -count=1 ./...

test-short:
	go test -short -race -count=1 ./...

lint: vet
	@which golangci-lint > /dev/null 2>&1 || { echo "Install golangci-lint: https://golangci-lint.run/welcome/install/"; exit 1; }
	golangci-lint run ./...

fmt:
	gofmt -w .

vet:
	go vet ./...

tidy:
	go mod tidy

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

docker-build:
	VERSION=$(VERSION) docker compose build

docker-up:
	docker compose up -d

docker-down:
	docker compose down -v

clean:
	rm -rf bin/
	docker compose down -v 2>/dev/null || true
