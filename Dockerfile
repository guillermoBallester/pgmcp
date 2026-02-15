FROM golang:1.25-alpine AS builder

RUN apk add --no-cache gcc musl-dev

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=1 go build -o /bin/pg-mcp ./cmd/pg-mcp

FROM alpine:3.21
RUN apk add --no-cache ca-certificates
COPY --from=builder /bin/pg-mcp /usr/local/bin/pg-mcp
ENTRYPOINT ["pg-mcp"]
