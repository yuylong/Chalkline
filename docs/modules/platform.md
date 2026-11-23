# 模块：platform

> M0 已交付。模块索引见 [README.md](README.md)。

## 边界

负责：

- 配置加载与校验（`internal/platform/config.go`）：地址、数据库连接串、日志级别、超时与重试参数。
- 结构化日志（`internal/platform/logging.go`）：slog JSON handler，`request_id` 贯穿访问日志。
- 数据库基础设施（`internal/store`）：pgx 连接池、goose 内嵌迁移、内置工作流模板种子。
- HTTP 骨架（`internal/api`）：路由、请求日志 / Recover / RequestID 中间件、统一错误包络。
- 应用引导（`internal/app` + `cmd/chalklined`）：后台 DB 初始化重试循环、优雅停机。
- 部署栈（`deploy/` + `web/index.html`）：docker compose、多阶段 Dockerfile、nginx TLS 终止、首启自签证书。

不负责：业务路由与用例（M1 起各模块自挂）、鉴权中间件（identity，M1）、领域校验（domain 层）、集成适配（M2+）。

## 对外接口

REST（M0 全集）：

| 端点 | 行为 |
| --- | --- |
| `GET /healthz` | 恒 200 `{"status":"ok"}`，进程存活探针 |
| `GET /readyz` | DB 初始化完成且可 Ping → 200 `ready`；否则 503 `unavailable` |
| `GET /api/v1/info` | `{"name":"chalkline","version":"...","api":"v1","milestone":"M0"}` |

未匹配路径返回 404 `NOT_FOUND`，未匹配方法返回 405 `INVALID_METHOD`，均为统一包络 `{"error":{code,message}}`（对齐 docs/domain/mcp.md 错误码）。

Go 接口（被上层使用）：

- `config.Load()` → `Config`；必填项缺失返回错误并拒绝启动。
- `logging.NewLogger(level)` → `*slog.Logger`。
- `store.New(ctx, url, logger)` / `db.Migrate(ctx)` / `db.SeedBuiltinWorkflows(ctx)` / `db.Ping(ctx)` / `db.Close()`。
- `api.NewRouter(api.Deps{Logger, Version, Ready, Ping})` → `http.Handler`。
- `app.Run(ctx)`：组装以上全部，监听信号优雅退出（10s 停机窗口）。

## 核心流程

启动（`app.Run`）：

1. `config.Load` 校验配置，失败即退出。
2. 起日志，开 HTTP server；此刻 `/healthz` 已可用。
3. 后台循环：`store.New` → `Migrate` → `SeedBuiltinWorkflows`；失败记 ERROR 日志并按 `DB_RETRY_INTERVAL`（默认 5s）重试，DB 就绪前 `/readyz` 503。
4. 收到 SIGINT/SIGTERM：10s 优雅停机，随后关闭连接池。

部署（`deploy/`）：`docker compose up` 起 postgres / api / web / nginx 四服务；nginx 终止 TLS（首启无证书时 entrypoint 自动 openssl 自签），`/` 走 web 容器托管的前端静态产物（M2 起为 React SPA，容器内 nginx `try_files` 回退），`/api/` 与 `/mcp` 反代 api:8080。环境变量经 `deploy/.env` 注入。

80 端口预留了 HTTP-01 校验路径 `/.well-known/acme-challenge/`（`root /etc/nginx/certs/acme`，由 `entrypoint.sh` 创建目录），供 webroot 模式签发证书时使用。未启用 ACME 时该目录为空，其余请求仍 301 至 HTTPS，对内网部署与自签证书流程无影响；目录缺失仅返回 404，不影响 nginx 启动。

## 数据

- `migrations/0001_platform_baseline.sql`：`audit_logs`、`outbox_events`。M1 起写入审计，M2 起 outbox 由投递器消费（`0007_integration.sql` 补 `next_attempt_at` 支撑退避）。
- `migrations/0002_workflows.sql`：`workflows`（`scope/name/version` 唯一，`definition` jsonb 存校验后的定义）。
- 种子：5 个内置模板 `ON CONFLICT (id) DO NOTHING`，不覆盖入库后的用户修改。

## 不变量

- 必填配置缺失即拒绝启动，无静默缺省。
- 迁移只前进：goose Up 全量应用，Down 为 no-op。
- 无时间即 UTC（日志、审计时间戳）。
- 访问日志必须携带 `request_id`（透传 `X-Request-ID` 或生成 uuid）。

## 验收标准

- [x] `go vet ./...`、`golangci-lint run`、`go test ./...` 全绿。
- [x] 二进制起服后 `/healthz` `/readyz` `/api/v1/info` 行为符合上表（本地冒烟通过）。
- [x] DB 不可用时进程存活、`/readyz` 503、重试循环有日志。
- [x] `deploy/` 四服务 + TLS 配置齐备，结构由 `internal/platform/deploy_test.go` 校验（容器内实测需在有 docker 的环境执行 `make up`）。
- [x] 内置 5 模板首启入库。

## 开放问题

- 生产证书替换流程（当前仅自签）→ M3 规模化收敛。
- `audit_logs` / `outbox_events` 的具体写入方与格式 → **已定**：审计由 `app` 事务内 `store.InsertAudit` 写入；outbox 由 `app` 写事件、`integration/notify.Dispatcher` 异步消费投递。

## M3 增量：规模化配置与迁移

> 已交付。

- **新配置**（环境变量，见 `deploy/.env.example`）：`CHALKLINE_REVIEW_SLA`（默认 24h）、`CHALKLINE_REVIEW_ESCALATION`（默认 3×SLA）、`CHALKLINE_REVIEW_SCAN_INTERVAL`（默认 10m）、`CHALKLINE_EVENTS_INTERVAL`（默认 3s）。
- **迁移 `0008_scale.sql`**：`webhook_subscriptions`（订阅管理）、`gate_reminders`（评审提醒水位）、`audit_logs` 补 `created_at` 索引（时间范围检索）。
- **后台任务**：数据库就绪后 server 额外启动两个 goroutine——评审 SLA 监控器（`app.RunReviewSLA`）与 outbox 事件尾随（SSE 投递源）；二者都只读业务状态、只写水位/事件，不触碰节点状态。
- 生产证书替换流程仍为手动替换 `deploy/certs/` 下文件（compose 挂载），未做自动化——单团队部署下收益有限，保留为运维手册事项。若需 Let's Encrypt，nginx 已预留 HTTP-01 校验路径，可手动以 webroot 模式签发后换入证书文件（未集成自动续期）。
