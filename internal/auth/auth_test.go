package auth

// auth_test.go：鉴权原语单测——argon2id、JWT、PAT、两个入口中间件的互斥与失效规则。

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/yuylong/Chalkline/internal/domain/identity"
)

func TestPasswordHashAndVerify(t *testing.T) {
	hash, err := HashPassword("s3cret-pass")
	if err != nil {
		t.Fatalf("哈希失败: %v", err)
	}
	if hash == "" || hash == "s3cret-pass" {
		t.Fatalf("hash 不应为空或明文")
	}
	ok, err := VerifyPassword("s3cret-pass", hash)
	if err != nil || !ok {
		t.Fatalf("正确密码应通过: %v %v", ok, err)
	}
	ok, err = VerifyPassword("wrong", hash)
	if err != nil || ok {
		t.Fatalf("错误密码应失败")
	}
}

func TestJWTRoundTrip(t *testing.T) {
	m := NewJWTManager("test-secret")
	token, err := m.Issue("usr_1", "alice", []string{"pm", "dev"}, time.Hour)
	if err != nil {
		t.Fatalf("签发失败: %v", err)
	}
	claims, err := m.Verify(token)
	if err != nil {
		t.Fatalf("校验失败: %v", err)
	}
	if claims.Sub != "usr_1" || claims.Username != "alice" || len(claims.Roles) != 2 {
		t.Fatalf("声明不符: %+v", claims)
	}

	if _, err := NewJWTManager("other").Verify(token); err == nil {
		t.Fatalf("换密钥应校验失败")
	}
	tampered := token[:10] + string(token[10]^0x20) + token[11:]
	if _, err := m.Verify(tampered); err == nil {
		t.Fatalf("篡改令牌应校验失败")
	}
}

func TestJWTExpiry(t *testing.T) {
	m := NewJWTManager("s")
	m.now = func() time.Time { return time.Unix(1000, 0) }
	token, err := m.Issue("u", "u", []string{"dev"}, time.Minute)
	if err != nil {
		t.Fatalf("签发失败: %v", err)
	}
	if _, err := m.Verify(token); err != nil {
		t.Fatalf("未过期应通过: %v", err)
	}
	m.now = func() time.Time { return time.Unix(1000+61, 0) }
	if _, err := m.Verify(token); err == nil {
		t.Fatalf("过期令牌应失败")
	}
}

func TestPATShape(t *testing.T) {
	token, err := NewPAT()
	if err != nil {
		t.Fatalf("生成失败: %v", err)
	}
	if !IsPAT(token) {
		t.Fatalf("PAT 应被识别: %s", token)
	}
	if IsPAT("eyJhbGciOiJIUzI1NiJ9.payload.sig") {
		t.Fatalf("JWT 不应识别为 PAT")
	}
	h1, h2 := HashPAT(token), HashPAT(token)
	if h1 != h2 || len(h1) != 64 {
		t.Fatalf("hash 应稳定且为 sha256 hex")
	}
}

// fakeUsers 实现 IdentityProvider。
type fakeUsers struct {
	users   map[string]identity.User
	pats    map[string]PATInfo
	touched string
}

func (f fakeUsers) UserByID(_ context.Context, id string) (identity.User, error) {
	u, ok := f.users[id]
	if !ok {
		return identity.User{}, ErrUnauthorized
	}
	return u, nil
}

func (f fakeUsers) ResolvePAT(_ context.Context, hash string) (PATInfo, error) {
	info, ok := f.pats[hash]
	if !ok {
		return PATInfo{}, ErrUnauthorized
	}
	return info, nil
}

func (f *fakeUsers) TouchPAT(_ context.Context, hash string) error {
	f.touched = hash
	return nil
}

func newTestMiddleware() (*Middleware, *fakeUsers) {
	users := &fakeUsers{
		users: map[string]identity.User{
			"usr_active": {ID: "usr_active", Username: "active", Roles: identity.Roles{identity.RolePM}, Status: identity.UserActive},
			"usr_off":    {ID: "usr_off", Username: "off", Roles: identity.Roles{identity.RoleDev}, Status: identity.UserDisabled},
		},
		pats: map[string]PATInfo{},
	}
	return &Middleware{JWT: NewJWTManager("test"), Users: users}, users
}

func serve(mw func(http.Handler) http.Handler) *httptest.ResponseRecorder {
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	rec := httptest.NewRecorder()
	mw(ok).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	return rec
}

func TestMiddlewareJWTFlow(t *testing.T) {
	m, users := newTestMiddleware()

	// 无 Authorization → 401
	if code := serve(m.AuthenticateREST).Code; code != http.StatusUnauthorized {
		t.Fatalf("无凭证应 401，实际 %d", code)
	}
	// 带 PAT 访问 REST → 401（入口互斥）
	rec := httptest.NewRecorder()
	token, _ := NewPAT()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	m.AuthenticateREST(http.HandlerFunc(okHandler)).ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("PAT 访问 REST 应 401，实际 %d", rec.Code)
	}
	// 合法 JWT（active）→ 200
	jwt, _ := m.JWT.Issue("usr_active", "active", []string{"pm"}, time.Hour)
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+jwt)
	rec = httptest.NewRecorder()
	m.AuthenticateREST(http.HandlerFunc(okHandler)).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("合法 JWT 应 200，实际 %d", rec.Code)
	}
	// disabled 用户 → 401（令牌立即失效）
	jwt, _ = m.JWT.Issue("usr_off", "off", []string{"dev"}, time.Hour)
	users.users["usr_off"] = identity.User{ID: "usr_off", Roles: identity.Roles{identity.RoleDev}, Status: identity.UserDisabled}
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+jwt)
	rec = httptest.NewRecorder()
	m.AuthenticateREST(http.HandlerFunc(okHandler)).ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("disabled 用户应 401，实际 %d", rec.Code)
	}
}

func TestMiddlewarePATFlow(t *testing.T) {
	m, users := newTestMiddleware()
	pat, _ := NewPAT()
	hash := HashPAT(pat)
	users.pats[hash] = PATInfo{UserID: "usr_active", Expires: time.Now().Add(time.Hour)}

	// JWT 访问 MCP → 401
	jwt, _ := m.JWT.Issue("usr_active", "active", []string{"pm"}, time.Hour)
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set("Authorization", "Bearer "+jwt)
	rec := httptest.NewRecorder()
	m.AuthenticateMCP(http.HandlerFunc(okHandler)).ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("JWT 访问 MCP 应 401，实际 %d", rec.Code)
	}

	// 合法 PAT → 200 且 last_used 被更新
	req = httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set("Authorization", "Bearer "+pat)
	rec = httptest.NewRecorder()
	m.AuthenticateMCP(http.HandlerFunc(okHandler)).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("合法 PAT 应 200，实际 %d", rec.Code)
	}
	if users.touched != hash {
		t.Fatalf("last_used 应被更新")
	}

	// 吊销 → 401
	users.pats[hash] = PATInfo{UserID: "usr_active", Expires: time.Now().Add(time.Hour), Revoked: true}
	req = httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set("Authorization", "Bearer "+pat)
	rec = httptest.NewRecorder()
	m.AuthenticateMCP(http.HandlerFunc(okHandler)).ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("吊销 PAT 应 401，实际 %d", rec.Code)
	}
}

func TestRequireRole(t *testing.T) {
	guard := RequireRole(identity.RoleAdmin)
	rec := httptest.NewRecorder()
	guard(http.HandlerFunc(okHandler)).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("无身份应 401，实际 %d", rec.Code)
	}

	handler := guard(http.HandlerFunc(okHandler))
	// admin 恒过
	base := httptest.NewRequest(http.MethodGet, "/", nil)
	req := base.WithContext(WithActor(base.Context(), Actor{UserID: "u", Roles: identity.Roles{identity.RoleAdmin}}))
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("admin 应通过，实际 %d", rec.Code)
	}
	// 非 admin → 403
	base = httptest.NewRequest(http.MethodGet, "/", nil)
	req = base.WithContext(WithActor(base.Context(), Actor{UserID: "u", Roles: identity.Roles{identity.RolePM}}))
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("非 admin 应 403，实际 %d", rec.Code)
	}
}

func okHandler(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }
