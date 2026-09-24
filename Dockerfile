FROM golang:1.26-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /app/link-proxy ./cmd/server

FROM alpine:3.20

RUN apk --no-cache add ca-certificates tzdata
WORKDIR /app
COPY --from=builder /app/link-proxy /app/link-proxy

EXPOSE 8080
VOLUME ["/data"]

ENTRYPOINT ["/app/link-proxy"]
