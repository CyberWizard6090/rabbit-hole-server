.PHONY: help build run test lint clean docker-up docker-down docker-logs db-create db-up db-down

APP_NAME = rabbit-hole-server
BUILD_DIR = bin
MAIN_PATH = cmd/server/main.go

help: ## Показать справку по доступным командам
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-15s\033[0m %s\n", $$1, $$2}'

# ==============================================================================
# Разработка и Сборка (Go)
# ==============================================================================

build: ## Скомпилировать бинарник сервера
	@mkdir -p $(BUILD_DIR)
	go build -o $(BUILD_DIR)/$(APP_NAME) $(MAIN_PATH)

run: ## Накатить миграции и запустить Go-сервер локально
	go run ./cmd/migrate -action up
	go run $(MAIN_PATH)

test: ## Запустить все тесты
	go test -v -race -cover ./...

lint: ## Запустить линтер
	golangci-lint run

clean: ## Удалить скомпилированные бинарники
	rm -rf $(BUILD_DIR)

# ==============================================================================
# Инфраструктура (Docker Compose)
# ==============================================================================

docker-up: ## Поднять всю инфраструктуру (Postgres -> Migrator -> API)
	docker compose up -d --build

docker-down: ## Остановить и удалить все контейнеры и тома (volumes)
	docker compose down -v

docker-logs: ## Посмотреть логи всех сервисов в реальном времени
	docker compose logs -f

# ==============================================================================
# Миграции БД (golang-migrate)
# ==============================================================================

db-create: ## Создать новую пару файлов миграций (пример: make db-create name=add_user_avatar)
	@if [ -z "$(name)" ]; then echo "❌ Ошибка: укажите имя миграции. Пример: make db-create name=add_user_avatar"; exit 1; fi
	migrate create -ext sql -dir db/migrations -seq $(name)

db-up: ## Накатить все новые миграции на локальную БД
	@$(MAKE) -C db migrate-up

db-down: ## Откатить ровно 1 последнюю миграцию
	@$(MAKE) -C db migrate-down