export PATH := /opt/homebrew/bin:$(PATH)
DATABASE_URL ?= postgres://axiomos:axiomos@localhost:5439/axiomos?sslmode=disable
PLATFORM_ADMIN_EMAIL ?= admin@axiomos.local
PLATFORM_ADMIN_PASSWORD ?= admin1234
# 本地开发的通知接收端、代码平台都在 localhost 上；出网默认只许公网（见 README）
AXIOMOS_ALLOW_PRIVATE_EGRESS ?= 1

.PHONY: db test run web build

db:            ## 启动本地 PostgreSQL
	docker compose up -d db

test:          ## 后端测试（有 DATABASE_URL 时含数据库集成测试）
	go test ./...

run:           ## 运行后端（含迁移）；可带 PLATFORM_ADMIN_EMAIL / PLATFORM_ADMIN_PASSWORD
	DATABASE_URL=$(DATABASE_URL) PLATFORM_ADMIN_EMAIL=$(PLATFORM_ADMIN_EMAIL) PLATFORM_ADMIN_PASSWORD=$(PLATFORM_ADMIN_PASSWORD) AXIOMOS_ALLOW_PRIVATE_EGRESS=$(AXIOMOS_ALLOW_PRIVATE_EGRESS) go run ./cmd/axiomd

web:           ## 运行前端开发服务器
	cd web && pnpm dev

build:         ## 构建前端并把后端打成单个二进制
	cd web && pnpm build
	go build -o bin/axiomd ./cmd/axiomd
