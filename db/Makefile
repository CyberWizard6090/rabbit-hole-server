ROOT_DIR := $(abspath $(dir $(lastword $(MAKEFILE_LIST)))/..)

.PHONY: migrate-up migrate-down migrate-down-all migrate-version migrate-force init-roles

migrate-up:
	cd "$(ROOT_DIR)" && go run ./cmd/migrate -action up

migrate-down:
	cd "$(ROOT_DIR)" && go run ./cmd/migrate -action down -steps 1

migrate-down-all:
	cd "$(ROOT_DIR)" && go run ./cmd/migrate -action down

migrate-version:
	cd "$(ROOT_DIR)" && go run ./cmd/migrate -action version

migrate-force:
	$(if $(strip $(VERSION)),,$(error Usage: make -C db migrate-force VERSION=1))
	cd "$(ROOT_DIR)" && go run ./cmd/migrate -action force -version $(VERSION)

init-roles:
	$(if $(strip $(DB_URL)),,$(error Usage: make -C db init-roles DB_URL=...))
	psql "$(DB_URL)" -v ON_ERROR_STOP=1 -f "$(CURDIR)/scripts/01-init-roles.sql"
