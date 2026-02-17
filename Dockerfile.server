FROM golang:1.26-alpine AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /bin/pgmcp-server ./cmd/pgmcp-server

FROM alpine:3.23
RUN apk add --no-cache ca-certificates
COPY --from=builder /bin/pgmcp-server /usr/local/bin/pgmcp-server
ENTRYPOINT ["pgmcp-server"]
