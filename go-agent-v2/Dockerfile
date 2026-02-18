# syntax=docker/dockerfile:1

# ========================================
# Stage 1: Build
# ========================================
FROM golang:1.25-alpine AS builder

RUN apk add --no-cache git ca-certificates

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /bin/app-server ./cmd/server/main.go

# ========================================
# Stage 2: Runtime
# ========================================
FROM alpine:3.21

RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -S app && adduser -S app -G app

COPY --from=builder /bin/app-server /usr/local/bin/app-server
COPY migrations/ /app/migrations/

USER app
WORKDIR /app

EXPOSE 4500

ENTRYPOINT ["app-server"]
