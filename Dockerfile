FROM golang:1.26-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /gateway ./cmd/gateway

FROM alpine:3.20

RUN apk add --no-cache ca-certificates
COPY --from=builder /gateway /gateway
COPY migrations/ /migrations/

ENV MIGRATIONS_PATH=/migrations

EXPOSE 8080 9090

ENTRYPOINT ["/gateway"]
