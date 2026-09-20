# Этап сборки (Build stage)
FROM golang:1.25-alpine AS builder

WORKDIR /app

# Кэширование зависимостей Go
COPY go.mod go.sum ./
RUN go mod download

# Копирование исходного кода приложения
COPY . .

# Сборка оптимизированного бинарного файла сервера
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /app/server ./cmd/server
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /app/migrate ./cmd/migrate

# Финальный образ для запуска
FROM alpine:3.19

RUN apk --no-cache add ca-certificates tzdata

WORKDIR /app

# Копирование бинарника с встроенными миграциями
COPY --from=builder /app/server .
COPY --from=builder /app/migrate .

EXPOSE 8080

CMD ["./server"]