# 模块：identity

> M1 范围。领域模型已于 M0 交付（`internal/domain/identity`）；本模块补齐鉴权与成员管理。模块索引见 [README.md](README.md)。

## 边界

负责：

- **全局身份**：User 实体与角色（`admin` / `pm` / `reviewer` / `dev` / `qa`）的管理与校验。
- **鉴权**：人用 JWT、MCP 用 PAT，二者共用同一套权限模型（见 [tech.md](../tech.md)）。
- **权限判定**：全局角色「会什么」的判定，以及与产品成员关系协作的对象级权限（产品隔离在 [catalog.md](catalog.md) 落地，本模块提供角色与中间件原语）。
- **审计关联**：所有写操作携带的 `actor_id` 由本模块的鉴权中间件注入请求上下文。

不负责：产品成员关系与产品级可见性（catalog）、业务对象的写权限细则（各模块在 app 层判定）、外部系统凭据（integration）。

## 对外接口

Go（被 api/mcp 调用）：

- `auth.Middleware`：解析 `Authorization`，注入 `actor_id` 与全局角色；MCP 路径走 PAT，其余走 JWT。
- `auth.RequireRole(roles...)` / `auth.IsAdmin`：全局角色守卫。
- `identity.Roles.HasAny(...)`：M0 已实现的角色包含判定（`admin` 恒真）。

REST（M1）：

| 端点 | 说明 | 权限 |
| --- | --- | --- |
| `POST /api/v1/auth/login` | 用户名密码换 JWT（按成员认证来源分支） | 公开 |
| `POST /api/v1/auth/pat` | 签发 PAT（MCP 接入用） | 登录用户 |
| `GET /api/v1/users` / `GET /api/v1/users/{id}` | 成员列表/详情 | `admin` |
| `POST /api/v1/users` | 建成员（可选认证来源） | `admin` |
| `PATCH /api/v1/users/{id}` | 改显示名 / 邮箱 / 角色 / 启禁用 / 认证来源 | `admin` |
| `POST /api/v1/users/{id}/password` | 重置密码 | `admin` 或本人 |
| `GET /api/v1/auth/ldap` / `PATCH /api/v1/auth/ldap` | LDAP 服务器配置读写 | `admin` |
| `POST /api/v1/auth/ldap/test` | 测试 LDAP 连通性 / 完整登录验证 | `admin` |

MCP：不直接暴露身份管理工具；`chalkline_whoami` 由本模块支撑（返回当前身份、角色、可见产品——可见产品列表来自 catalog）。

## 核心流程

- **登录**：按成员 `auth_source` 分支校验 → 签发短时效 JWT（含 `user_id`、`roles`）→ 返回给控制台。
  - `local`：仅本地 argon2id 校验，不触发 LDAP 拨号。
  - `ldap`：仅 LDAP 校验（拨号 → 服务账号/匿名绑定 → 按登录名检索 DN → DN+密码绑定）；本地 hash 为随机占位，不可匹配。
  - `both`：双认证——本地密码通过即登录；本地失败再尝试 LDAP，任一通过即可。
  - LDAP 未启用/未配置/不可达一律按「用户名或密码错误」401 处理（不暴露目录状态），仅记告警日志；管理台用「测试连接」做诊断。
- **MCP 鉴权**：请求带 `Authorization: Bearer <PAT>` → 中间件查 `personal_access_tokens`（比对 hash、未过期、未吊销）→ 注入 `actor_id`。PAT 不可用于控制台登录。
- **权限判定**：全局角色在入口中间件校验（如 MCP 写工具要求的角色）；对象级权限（是否某产品成员）由各模块 app 层在事务内调用 catalog 的成员检查，越权写 `403`、非成员访问 `404`。
- **LDAP 配置**：管理台维护服务器（host/port/TLS 模式/跳过证书校验/超时）、绑定账号（DN + 密码，密码 AES-GCM 加密落库、永不回显）、用户检索（Base DN + 含 `%s` 占位的过滤器）；保存即生效，无需重启。「测试连接」做拨号 + 绑定 + 试检索；「测试用户登录」可选做完整登录验证（密码只用于当次验证，不落库）。

## 数据

- `users`（M1 新建迁移）：`id` / `username` 唯一 / `display_name` / `email` 唯一 / `password_hash` / `roles`（数组或位掩码）/ `status` / `auth_source`（`local`/`ldap`/`both`，默认 `local`）/ `created_at` / `updated_at`。字段来自 [model.md](../domain/model.md)「User」。
- `personal_access_tokens`（M1 新建迁移）：`id` / `user_id` / `token_hash` / `name` / `last_used_at` / `expires_at` / `revoked_at` / `created_at`。**只存 hash，不存明文 token**。
- `ldap_configs`（LDAP 增量迁移 `0016`）：单行 `config_key='default'`——`enabled` / `host` / `port` / `tls_mode`（none/starttls/tls）/ `tls_skip_verify` / `bind_dn` / `bind_password_enc`（AES-GCM 密文，与 SMTP 密码同属红线 8 的管理台例外）/ `user_search_base` / `user_search_filter`（含 `%s` 占位）/ `timeout_seconds` / `updated_by` / `updated_at`。未写入行 = LDAP 未配置。
- `sessions`（可选，M1 评估是否需要）：若 JWT 无状态足够则不建；需服务端可吊销时再建。
- `audit_logs` 已由 M0 `0001_platform_baseline.sql` 建好，本模块的写操作写入。

> 表名与列遵循 [structure.md](../structure.md)：表复数 `snake_case`、列 `snake_case`。

## 不变量（本模块保证）

- 用户名匹配 `^[a-z0-9][a-z0-9_-]{1,31}$`，唯一且不可改（M0 已在校验函数实现，M1 在 store 层加唯一索引）。
- 密码只存 argon2id hash，禁止明文；PAT 只存 hash；LDAP 绑定密码只存 AES-GCM 密文，API 永不回显。
- `disabled` 用户的 JWT 与 PAT 立即失效（校验时查 `users.status`）。
- 全局角色判定中 `admin` 恒包含于任意角色集合（不变量 8 / 16 的基础）。
- 鉴权失败返回 `401`，与 `403`（已认证但无权）、`404`（不可见）严格区分。
- LDAP 登录名经 `EscapeFilter` 转义后代入检索过滤器，杜绝 LDAP 注入；空密码直接拒绝（LDAP 空密码 = 匿名绑定）。
- `ldap` 成员的本地 `password_hash` 为随机占位（不可匹配）；`local`/`both` 成员必须设置初始密码。
- LDAP 不可用不影响 `local` 成员登录；`both` 成员在 LDAP 不可用时仍可用本地密码登录。

## 验收标准

- [ ] 用户名密码登录成功返回 JWT，错误密码/禁用用户返回 401。
- [ ] PAT 可调用 MCP 端点；吊销或过期 PAT 返回 401。
- [ ] `admin` 可管理成员；非 admin 访问用户管理端点返回 403。
- [ ] 越权访问未加入产品的任意资源返回 404（与 catalog 联调）。
- [ ] 密码与 PAT 明文不出现在日志与数据库（审计验证）。
- [ ] `go vet` / `golangci-lint` / `go test`（含 testcontainers 集成测试）全绿。

## 任务拆解

> 按 [structure.md](../structure.md) 开发顺序：domain → migrations → store → app → api/mcp。可逐条挂为 TaskModule=identity 的任务。

1. `migrations/0003_identity.sql`：建 `users`、`personal_access_tokens` 表及索引（username/email 唯一）。
2. `internal/auth/pat.go`：PAT 签发（生成明文返回一次 + 存 hash）、校验、吊销。
3. `internal/auth/jwt.go`：JWT 签发与校验（含角色声明），密钥从环境变量读取。
4. `internal/auth/middleware.go`：统一鉴权中间件，按路径区分 JWT/PAT，注入 `actor_id`；`RequireRole` 守卫。
5. `internal/store` 增加 users 查询（sqlc 生成）。
6. `internal/app/identity`：登录、签发/吊销 PAT、用户 CRUD、改密、启禁用的用例编排（事务 + 审计）。
7. `internal/api`：上表 REST 端点 + DTO + 路由注册，接入鉴权中间件。
8. MCP 侧：`/mcp` 接入 PAT 鉴权；`chalkline_whoami` 返回身份与可见产品（联调 catalog）。
9. 集成测试：登录、PAT 生命周期、权限矩阵（含 401/403/404 区分）用 testcontainers 跑真实 PG。
10. 文档：本模块验收通过后把 `docs/modules/README.md` 的 identity 状态从「M1」更新为交付。

## 开放问题

- 是否需要服务端可吊销的会话表，还是 JWT 短时效 + PAT 显式吊销足够？→ 任务 3/4 评估。
- PAT 的 scope 粒度：先做「全权限 PAT」还是按角色裁剪？→ 先全权限，后续按需加 scope。
