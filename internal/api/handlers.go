package api

// handlers.go：REST handler 基座——只做参数转换与错误映射，业务规则全部在 app 层
//（docs/tech.md 硬性约束：路由层不访问数据库、不做判定）。

import (
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/yuylong/Chalkline/internal/app"
	"github.com/yuylong/Chalkline/internal/auth"
	"github.com/yuylong/Chalkline/internal/domain/identity"
)

type handlers struct {
	deps Deps
}

// actor 取出鉴权中间件注入的身份。
func (h *handlers) actor(r *http.Request) (app.Actor, error) {
	a, ok := auth.ActorFrom(r.Context())
	if !ok {
		return app.Actor{}, app.Unauthorized("未认证")
	}
	return a, nil
}

// writeAppError 输出 app.Error 统一包络（401/403/404/409/422/500…）。
func writeAppError(w http.ResponseWriter, err error) {
	aerr := app.AsError(err)
	WriteError(w, aerr.Status, aerr.Code, aerr.Message, aerr.GetDetails())
}

// decode 解析 JSON 请求体（限 1MB）。
func decode(r *http.Request, v any) error {
	defer func() { _, _ = io.Copy(io.Discard, r.Body) }()
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	if err := dec.Decode(v); err != nil {
		return app.Invalid("请求体 JSON 非法: %v", err)
	}
	return nil
}

// pathParam 取 chi 路径参数。
func pathParam(r *http.Request, key string) string {
	return chi.URLParam(r, key)
}

// parseRoles 解析并校验全局角色列表。
func parseRoles(in []string) (identity.Roles, error) {
	if len(in) == 0 {
		return nil, app.Invalid("roles 不可为空")
	}
	out := make(identity.Roles, 0, len(in))
	for _, s := range in {
		role := identity.Role(s)
		if !role.Valid() {
			return nil, app.Invalid("未知角色 %q", s)
		}
		out = append(out, role)
	}
	return out, nil
}

// parseDate 解析 YYYY-MM-DD 日期（UTC 存储，展示层转换）。
func parseDate(s string) (time.Time, error) {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return time.Time{}, app.Invalid("日期 %q 需为 YYYY-MM-DD", s)
	}
	return t, nil
}

// svc 取用例集合（readyGuard 已确保非 nil）。
func (h *handlers) svc() app.API { return h.deps.Svc() }
