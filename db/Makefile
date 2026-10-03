ROOT_DIR := $(abspath $(dir $(lastword $(MAKEFILE_LIST)))/..)

.PHONY: migrate-up migrate-down migrate-down-all migrate-version migrate-force

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