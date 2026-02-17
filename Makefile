.PHONY: build-agent build-server build-all test test-short lint fmt vet tidy \
        docker-build docker-up docker-down demo-up demo-down clean \
        run-server run-agent run-tunnel

build-agent:
	go build -o bin/pgmcp-agent ./cmd/pgmcp-agent

build-server:
	go build -o bin/pgmcp-server ./cmd/pgmcp-server

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

docker-build:
	docker compose build

docker-up:
	docker compose up -d

docker-down:
	docker compose down -v

demo-up:
	docker compose -f examples/demo/docker-compose.yml up -d

demo-down:
	docker compose -f examples/demo/docker-compose.yml down -v

# --- RNAcentral tunnel test ---
# Usage:
#   Terminal 1: make run-server
#   Terminal 2: make run-agent
# Or both at once (server backgrounded):
#   make run-tunnel
#
# Override DATABASE_URL to point at a different DB:
#   make run-agent DATABASE_URL="postgresql://..."

TUNNEL_API_KEY  ?= dev-test-key
LISTEN_ADDR     ?= :8080
DATABASE_URL    ?= postgresql://reader:NWDMCE5xdipIjRrp@hh-pgsql-public.ebi.ac.uk:5432/pfmegrnargs

run-server: build-server
	API_KEYS=$(TUNNEL_API_KEY) LISTEN_ADDR=$(LISTEN_ADDR) ./bin/pgmcp-server

run-agent: build-agent
	DATABASE_URL=$(DATABASE_URL) \
	TUNNEL_URL=ws://localhost$(LISTEN_ADDR)/tunnel \
	API_KEY=$(TUNNEL_API_KEY) \
	SCHEMAS=rnacen \
	./bin/pgmcp-agent

run-tunnel: build-all
	@echo "Starting server on $(LISTEN_ADDR)..."
	@API_KEYS=$(TUNNEL_API_KEY) LISTEN_ADDR=$(LISTEN_ADDR) ./bin/pgmcp-server & \
	SERVER_PID=$$!; \
	sleep 1; \
	echo "Starting agent (DB: RNAcentral)..."; \
	DATABASE_URL=$(DATABASE_URL) \
	TUNNEL_URL=ws://localhost$(LISTEN_ADDR)/tunnel \
	API_KEY=$(TUNNEL_API_KEY) \
	SCHEMAS=rnacen \
	./bin/pgmcp-agent; \
	kill $$SERVER_PID 2>/dev/null || true

clean:
	rm -rf bin/
	docker compose down -v 2>/dev/null || true
	docker compose -f examples/demo/docker-compose.yml down -v 2>/dev/null || true
