COMPOSE ?= docker compose
SERVICE ?= hookdesk

.PHONY: help up start stop down restart build logs status config dev test vet css

help: ## Tampilkan command yang tersedia
	@awk 'BEGIN {FS = ":.*##"; print "Hookdesk development commands:"} /^[a-zA-Z_-]+:.*##/ {printf "  %-12s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

up: ## Build image dan jalankan aplikasi di background
	$(COMPOSE) up -d --build

start: ## Jalankan container yang sudah pernah dibuat
	$(COMPOSE) start $(SERVICE)

stop: ## Stop aplikasi, data tetap tersimpan
	$(COMPOSE) stop $(SERVICE)

down: ## Stop dan hapus container/network, volume database tetap aman
	$(COMPOSE) down

restart: ## Restart aplikasi tanpa build ulang
	$(COMPOSE) restart $(SERVICE)

build: ## Build image Docker tanpa menjalankannya
	$(COMPOSE) build $(SERVICE)

logs: ## Ikuti log aplikasi
	$(COMPOSE) logs -f --tail=100 $(SERVICE)

status: ## Lihat status dan health container
	$(COMPOSE) ps

config: ## Validasi konfigurasi Compose dan environment
	$(COMPOSE) config

dev: ## Jalankan versi lokal dengan Go
	go run .

test: ## Jalankan test Go
	go test ./...

vet: ## Jalankan pemeriksaan statis Go
	go vet ./...

css: ## Build stylesheet Tailwind lokal
	npm run build:css
