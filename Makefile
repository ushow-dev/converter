# Makefile — точка входа для всех операций с проектом
# Использование: make <target>
# Список команд: make help

.PHONY: help setup up down build logs test-api test-worker lint clean-temp

# Цвета для вывода
GREEN  := \033[0;32m
YELLOW := \033[1;33m
NC     := \033[0m

help: ## Показать список доступных команд
	@echo ""
	@echo "Использование: make <команда>"
	@echo ""
	@awk 'BEGIN {FS = ":.*##"} /^[a-zA-Z_-]+:.*##/ { printf "  $(GREEN)%-18s$(NC) %s\n", $$1, $$2 }' $(MAKEFILE_LIST)
	@echo ""

# ─── Первоначальная настройка ───────────────────────────────────────────────

setup: ## Первоначальная настройка: git hooks + проверка окружения
	@echo "$(YELLOW)→ Установка git hooks...$(NC)"
	git config core.hooksPath .githooks
	@echo "$(GREEN)✓ Git hooks активированы (.githooks/)$(NC)"
	@echo ""
	@if [ ! -f .env ]; then \
		echo "$(YELLOW)→ Создание .env из шаблона...$(NC)"; \
		cp .env.example .env; \
		echo "$(GREEN)✓ .env создан. Заполните реальными значениями.$(NC)"; \
	else \
		echo "$(GREEN)✓ .env уже существует$(NC)"; \
	fi
	@echo ""
	@echo "$(GREEN)✓ Настройка завершена. Запустите: make up$(NC)"

# ─── Docker ─────────────────────────────────────────────────────────────────

up: ## Запустить все сервисы
	docker compose up -d

down: ## Остановить все сервисы
	docker compose down

build: ## Пересобрать Docker образы
	docker compose build

restart: ## Перезапустить сервисы (с пересборкой)
	docker compose down
	docker compose build
	docker compose up -d

logs: ## Показать логи api и worker (follow)
	docker compose logs -f api worker

logs-all: ## Показать логи всех сервисов (follow)
	docker compose logs -f

ps: ## Статус всех контейнеров
	docker compose ps

# ─── Разработка ─────────────────────────────────────────────────────────────

test-api: ## Запустить тесты api
	cd api && go test ./... -v

test-worker: ## Запустить тесты worker
	cd worker && go test ./... -v

test: test-api test-worker ## Запустить все тесты

lint: ## Запустить go vet для api и worker
	@echo "$(YELLOW)→ go vet api...$(NC)"
	cd api && go vet ./...
	@echo "$(YELLOW)→ go vet worker...$(NC)"
	cd worker && go vet ./...
	@echo "$(GREEN)✓ Проверок не обнаружено$(NC)"

build-api: ## Собрать бинарник api локально
	cd api && go build -o /tmp/api-bin ./cmd/api

build-worker: ## Собрать бинарник worker локально
	cd worker && go build -o /tmp/worker-bin ./cmd/worker

# ─── Обслуживание ───────────────────────────────────────────────────────────

clean-temp: ## Удалить временные файлы FFmpeg из media/temp/
	@echo "$(YELLOW)→ Удаление media/temp/*...$(NC)"
	find ./media/temp -mindepth 1 -maxdepth 1 -type d -exec rm -rf {} + 2>/dev/null || true
	@echo "$(GREEN)✓ Готово$(NC)"

migrate: ## Применить DB миграции (перезапуском api)
	docker compose restart api
	@echo "$(GREEN)✓ Миграции применены при старте api$(NC)"

adr: ## Создать новый ADR: make adr TITLE="название решения"
	@if [ -z "$(TITLE)" ]; then echo "Использование: make adr TITLE=\"название решения\""; exit 1; fi
	./scripts/new-adr.sh "$(TITLE)"

gen-secrets: ## Сгенерировать случайные секреты для .env
	@echo ""
	@echo "$(YELLOW)Скопируйте нужные значения в .env:$(NC)"
	@echo ""
	@echo "JWT_SECRET=$$(openssl rand -hex 32)"
	@echo "PLAYER_API_KEY=$$(openssl rand -hex 16)"
	@echo "MEDIA_SIGNING_KEY=$$(openssl rand -hex 32)"
	@echo "POSTGRES_PASSWORD=$$(openssl rand -base64 16 | tr -d '/+=' | head -c 20)"
	@echo ""
