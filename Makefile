# Chalkline 开发工作流常用命令。
VERSION ?= $(shell git describe --tags --always 2>/dev/null || echo dev)
LD := -ldflags "-s -w -X github.com/yuylong/Chalkline/internal/server.Version=$(VERSION)"
COMPOSE := docker compose --project-directory deploy -f deploy/docker-compose.yml

.PHONY: help build run test vet lint tidy up down logs ps clean

help: ## 列出常用目标
	@grep -E '^[a-zA-Z_-]+:.*## ' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  %-10s %s\n", $$1, $$2}'

build: ## 编译 chalklined 到 bin/
	CGO_ENABLED=0 go build -trimpath $(LD) -o bin/chalklined ./cmd/chalklined

run: ## 本地运行（需 CHALKLINE_DATABASE_URL）
	go run ./cmd/chalklined

test: ## 全量单元测试
	go test ./...

vet: ## go vet
	go vet ./...

lint: ## golangci-lint
	golangci-lint run

tidy: ## 整理依赖
	go mod tidy

up: ## 启动部署栈（postgres + api + web + nginx）
	$(COMPOSE) up -d --build

down: ## 停止部署栈
	$(COMPOSE) down

logs: ## 跟踪 api 日志
	$(COMPOSE) logs -f api

ps: ## 查看部署栈状态
	$(COMPOSE) ps

clean: ## 清理构建产物
	rm -rf bin
