# DNS 统一管理平台
# 常用命令：make up / build / test / lint / clean

ENV_FILE := .env
BIN := bin/dnsctl

.PHONY: up down dev-api dev-web env build build-web test lint clean

# ============================================================
# 一键运行：生成随机密钥 → 启动 MySQL/Redis → 初始化 → 启动服务
# ============================================================
up: env
	@docker rm -f dns-mysql dns-redis >/dev/null 2>&1 || true
	@docker compose up -d --wait
	@if [ ! -f web/dist/index.html ]; then \
		echo "前端产物不存在，构建中..."; \
		$(MAKE) build-web; \
	fi
	@echo ""
	@echo "▶ 启动服务: http://localhost:8080 "
	go run ./cmd/dnsctl serve --addr :8080

# 生成 .env（MySQL/Redis 随机密码 + 应用密钥）
env:
	@if [ ! -f $(ENV_FILE) ]; then \
		echo "生成随机密钥到 $(ENV_FILE)"; \
		echo "DNS_DB_HOST=127.0.0.1" > $(ENV_FILE); \
		echo "DNS_DB_PORT=3307" >> $(ENV_FILE); \
		echo "DNS_DB_NAME=dns_manager" >> $(ENV_FILE); \
		echo "DNS_DB_USER=root" >> $(ENV_FILE); \
		echo "DNS_DB_PASS=$$(openssl rand -hex 16)" >> $(ENV_FILE); \
		echo "DNS_REDIS_HOST=127.0.0.1" >> $(ENV_FILE); \
		echo "DNS_REDIS_PORT=6379" >> $(ENV_FILE); \
		echo "DNS_REDIS_AUTH=$$(openssl rand -hex 16)" >> $(ENV_FILE); \
		echo "DNS_SECRET_KEY=$$(openssl rand -hex 32)" >> $(ENV_FILE); \
		echo "DNS_CRON_KEY=$$(openssl rand -hex 16)" >> $(ENV_FILE); \
		echo "DNS_ADMIN_USER=admin" >> $(ENV_FILE); \
		echo "DNS_ADMIN_PASS=admin123" >> $(ENV_FILE); \
		echo "" >> $(ENV_FILE); \
		echo "# AI 诊断助手（可选，填入 Key 后启用）" >> $(ENV_FILE); \
		echo "DNS_AI_BASE_URL=https://api.deepseek.com/v1" >> $(ENV_FILE); \
		echo "DNS_AI_MODEL=deepseek-chat" >> $(ENV_FILE); \
		echo "DNS_AI_API_KEY=" >> $(ENV_FILE); \
	fi

# 停止容器（不删数据）
down:
	docker compose down

# ============================================================
# 开发模式（前后端分离）
# ============================================================
# 后端 mock API（监听 8080，带 CORS，不需要 MySQL/Redis）
dev-api:
	go run ./cmd/dnsctl dev

# 前端 Vite（监听 5173，/api 代理到 8080）
dev-web:
	cd web && npm run dev

# ============================================================
# 一键构建：前端产物 + 单二进制
# ============================================================
build: build-web
	go build -o $(BIN) ./cmd/dnsctl

build-web:
	cd web && npm install && npm run build

# ============================================================
# 测试与检查
# ============================================================
test:
	go test ./...

lint:
	go vet ./...

# ============================================================
# 清理：删除产物与容器（含数据卷）
# ============================================================
clean:
	rm -rf bin web/dist
	docker compose down -v
