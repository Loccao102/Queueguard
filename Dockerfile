FROM golang:1.24-alpine AS builder

WORKDIR /app
COPY go.mod ./
# COPY go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /queueguard ./cmd/server/main.go
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /mock-origin ./cmd/mock-origin/main.go

FROM alpine:latest
RUN apk --no-cache add ca-certificates tzdata
WORKDIR /app
COPY --from=builder /queueguard /app/queueguard
COPY --from=builder /mock-origin /app/mock-origin

EXPOSE 8000 8080
ENTRYPOINT ["/app/queueguard"]

