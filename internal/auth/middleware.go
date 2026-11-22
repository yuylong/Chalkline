package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/yuylong/Chalkline/internal/domain/identity"
)

// Actor 是请求级身份，由鉴权中间件注入 context；
// 所有写操作经它携带 actor_id 进事务与审计（不变量 13）。
type Actor struct {
	UserID   string
	Username string
	Roles    identity.Roles
	IsAdmin  bool
	ViaPAT   bool // PAT 仅可调用 MCP；JWT 仅可访问 REST
}

type ctxKey struct{}

// WithActor 把身份放进 context。
func WithActor(ctx context.Context, a Actor) context.Context {
	return context.WithValue(ctx, ctxKey{}, a)
}

// ActorFrom 取出身份；未鉴权请求返回 false。
func ActorFrom(ctx context.Context) (Actor, bool) {
	a, ok := ctx.Value(ctxKey{}).(Actor)
	return a, ok
}

// IdentityProvider 是鉴权所需的存储查询，由 internal/store 实现
//（auth 经接口依赖存储，不直连数据库，docs/structure.md）。
type IdentityProvider interface {
	UserByID(ctx context.Context, id string) (identity.User, error)
	ResolvePAT(ctx context.Context, tokenHash string) (PATInfo, error)
	TouchPAT(ctx context.Context, tokenHash string) error
}

// PATInfo 是 PAT 解析结论。
type PATInfo struct {
	UserID   string
	Expires  time.Time
	Revoked  bool
}

// ErrUnauthorized 鉴权失败（401 包络由中间件输出）。
var ErrUnauthorized = errors.New("未认证或凭证失效")

// Middleware 组装两个入口的鉴权：REST 走 JWT，MCP 走 PAT。
type Middleware struct {
	JWT   *JWTManager
	Users IdentityProvider
}

func (m *Middleware) write401(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusUnauthorized)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]any{"code": "UNAUTHORIZED", "message": msg},
	})
}

// AuthenticateREST 校验 JWT；disabled 用户立即失效（不变量：JWT 校验时查 users.status）。
func (m *Middleware) AuthenticateREST(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims, ok := m.bearerJWT(r)
		if !ok {
			m.write401(w, ErrUnauthorized.Error())
			return
		}
		user, err := m.Users.UserByID(r.Context(), claims.Sub)
		if err != nil || user.Status != identity.UserActive {
			m.write401(w, ErrUnauthorized.Error())
			return
		}
		ctx := WithActor(r.Context(), Actor{
			UserID: user.ID, Username: user.Username,
			Roles: user.Roles, IsAdmin: user.Roles.HasAny(identity.RoleAdmin),
		})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// AuthenticateMCP 仅接受 PAT（JWT 不可调用 MCP，PAT 不可访问 REST——入口互斥）。
func (m *Middleware) AuthenticateMCP(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, ok := bearer(r)
		if !ok || !IsPAT(token) {
			m.write401(w, "MCP 端点仅接受 PAT（Authorization: Bearer chkpat_…）")
			return
		}
		hash := HashPAT(token)
		info, err := m.Users.ResolvePAT(r.Context(), hash)
		if err != nil || info.Revoked || time.Now().After(info.Expires) {
			m.write401(w, ErrUnauthorized.Error())
			return
		}
		user, err := m.Users.UserByID(r.Context(), info.UserID)
		if err != nil || user.Status != identity.UserActive {
			m.write401(w, ErrUnauthorized.Error())
			return
		}
		_ = m.Users.TouchPAT(r.Context(), hash) // last_used 尽力更新，不阻断请求
		ctx := WithActor(r.Context(), Actor{
			UserID: user.ID, Username: user.Username,
			Roles: user.Roles, IsAdmin: user.Roles.HasAny(identity.RoleAdmin), ViaPAT: true,
		})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequireREST 是 REST 专用中间件链。
func (m *Middleware) RequireREST(next http.Handler) http.Handler { return m.AuthenticateREST(next) }

// RequireMCP 是 MCP 专用中间件链。
func (m *Middleware) RequireMCP(next http.Handler) http.Handler { return m.AuthenticateMCP(next) }

// RequireRole 全局角色守卫：无任一所需角色即 403 ROLE_REQUIRED（admin 恒过）。
func RequireRole(roles ...identity.Role) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			actor, ok := ActorFrom(r.Context())
			if !ok {
				w.Header().Set("Content-Type", "application/json; charset=utf-8")
				w.WriteHeader(http.StatusUnauthorized)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"error": map[string]any{"code": "UNAUTHORIZED", "message": ErrUnauthorized.Error()},
				})
				return
			}
			if !actor.Roles.HasAny(roles...) {
				w.Header().Set("Content-Type", "application/json; charset=utf-8")
				w.WriteHeader(http.StatusForbidden)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"error": map[string]any{"code": "ROLE_REQUIRED", "message": "需要角色 " + joinRoles(roles)},
				})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func (m *Middleware) bearerJWT(r *http.Request) (JWTClaims, bool) {
	token, ok := bearer(r)
	if !ok || IsPAT(token) {
		return JWTClaims{}, false
	}
	claims, err := m.JWT.Verify(token)
	if err != nil {
		return JWTClaims{}, false
	}
	return claims, true
}

func bearer(r *http.Request) (string, bool) {
	h := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if len(h) <= len(prefix) || h[:len(prefix)] != prefix {
		return "", false
	}
	return h[len(prefix):], true
}

func joinRoles(roles []identity.Role) string {
	out := ""
	for i, r := range roles {
		if i > 0 {
			out += ","
		}
		out += string(r)
	}
	return out
}
