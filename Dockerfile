# ==========================================
# 1. Базовый этап (Общий)
# ==========================================
FROM golang:1.25-alpine AS base

WORKDIR /app
RUN apk add --no-cache git ca-certificates tzdata

COPY go.mod go.sum ./
RUN go mod download

# ==========================================
# 2. Среда РАЗРАБОТКИ (Development)
# ==========================================
FROM base AS dev

ENV GOTOOLCHAIN=auto
RUN go install github.com/air-verse/air@v1.61.7

COPY . .

# Команда по умолчанию (переопределяется в compose)
CMD ["air"]

# ==========================================
# 3. Сборка бинарников для Prod (Builder)
# ==========================================
FROM base AS builder

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-w -s" -o /app/server ./cmd/server
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-w -s" -o /app/migrate ./cmd/migrate

# ==========================================
# 4. Среда Production
# ==========================================
FROM alpine:3.22 AS prod

RUN apk --no-cache add ca-certificates tzdata \
    && addgroup -S app && adduser -S -G app app

WORKDIR /app

COPY --from=builder /app/server .
COPY --from=builder /app/migrate .

USER app

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
    CMD wget -q -O /dev/null "http://127.0.0.1:${PORT:-8080}/healthz" || exit 1

CMD ["./server"]