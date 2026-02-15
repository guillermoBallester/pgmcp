FROM golang:1.26-alpine AS builder

RUN apk add --no-cache gcc musl-dev

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=1 go build -o /bin/pgmcp-agent ./cmd/pgmcp-agent

FROM alpine:3.23
RUN apk add --no-cache ca-certificates
COPY --from=builder /bin/pgmcp-agent /usr/local/bin/pgmcp-agent
ENTRYPOINT ["pgmcp-agent"]
