package api

// router_test.go：路由与协议层测试——统一包络、鉴权链、登录端点。
// fakeAPI 嵌入 app.API 接口：未覆盖的方法调用会 panic，测试因此只走覆盖过的路径。

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/yuylong/Chalkline/internal/app"
	"github.com/yuylong/Chalkline/internal/auth"
	"github.com/yuylong/Chalkline/internal/domain/identity"
	"github.com/yuylong/Chalkline/internal/platform"
)

type fakeAPI struct {
	app.API
	loginFn func(ctx context.Context, username, password string) (app.LoginResult, error)
}

func (f fakeAPI) Login(ctx context.Context, username, password string) (app.LoginResult, error) {
	return f.loginFn(ctx, username, password)
}

func testDeps(loginFn func(context.Context, string, string) (app.LoginResult, error)) Deps {
	logger := platform.NewLogger("error")
	return Deps{
		Logger:  logger,
		Version: "test",
		Svc:     func() app.API { return fakeAPI{loginFn: loginFn} },
		AuthREST: func(next http.Handler) http.Handler {
			// 直通注入固定身份：鉴权链本身由 internal/auth 测试覆盖。
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				next.ServeHTTP(w, r.WithContext(auth.WithActor(r.Context(),
					app.Actor{UserID: "usr_1", Username: "t", Roles: identity.Roles{identity.RoleAdmin}, IsAdmin: true})))
			})
		},
		AuthMCP: func(next http.Handler) http.Handler { return next },
		Ready:   func() bool { return true },
		Ping:    func(*http.Request) error { return nil },
	}
}

func TestHealthzAlwaysOK(t *testing.T) {
	d := testDeps(nil)
	d.Ready = func() bool { return false } // healthz 不依赖数据库
	rec := httptest.NewRecorder()
	NewRouter(d).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("healthz 应恒 200，实际 %d", rec.Code)
	}
}

func TestReadyzThreeStates(t *testing.T) {
	ready := true
	pingOK := true
	d := testDeps(nil)
	d.Ready = func() bool { return ready }
	d.Ping = func(*http.Request) error {
		if !pingOK {
			return context.Canceled
		}
		return nil
	}
	r := NewRouter(d)

	get := func() int {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
		return rec.Code
	}
	if get() != http.StatusOK {
		t.Fatalf("就绪应 200")
	}
	ready = false
	if get() != http.StatusServiceUnavailable {
		t.Fatalf("未就绪应 503")
	}
	ready, pingOK = true, false
	if get() != http.StatusServiceUnavailable {
		t.Fatalf("数据库失联应 503")
	}
}

func TestInfo(t *testing.T) {
	rec := httptest.NewRecorder()
	NewRouter(testDeps(nil)).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/info", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("info 应 200，实际 %d", rec.Code)
	}
	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body["milestone"] != "M2" {
		t.Fatalf("milestone 应为 M2，实际 %v", body["milestone"])
	}
}

func TestNotFoundAndMethodNotAllowed(t *testing.T) {
	r := NewRouter(testDeps(nil))

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/nope", nil))
	if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), `"NOT_FOUND"`) {
		t.Fatalf("404 应统一包络，实际 %d %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/api/v1/info", nil))
	if rec.Code != http.StatusMethodNotAllowed || !strings.Contains(rec.Body.String(), `"INVALID_METHOD"`) {
		t.Fatalf("405 应统一包络，实际 %d %s", rec.Code, rec.Body.String())
	}
}

func TestLoginSuccessAndFailure(t *testing.T) {
	d := testDeps(func(_ context.Context, username, password string) (app.LoginResult, error) {
		if username == "alice" && password == "pw" {
			return app.LoginResult{Token: "jwt-xxx", User: app.UserView{ID: "usr_1", Username: "alice"}}, nil
		}
		return app.LoginResult{}, app.Unauthorized("用户名或密码错误")
	})
	r := NewRouter(d)

	post := func(body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(rec, req)
		return rec
	}

	rec := post(`{"username":"alice","password":"pw"}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "jwt-xxx") {
		t.Fatalf("登录成功应返回 token，实际 %d %s", rec.Code, rec.Body.String())
	}

	rec = post(`{"username":"alice","password":"bad"}`)
	if rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Body.String(), `"UNAUTHORIZED"`) {
		t.Fatalf("登录失败应 401 包络，实际 %d %s", rec.Code, rec.Body.String())
	}

	rec = post(`{bad-json`)
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), `"INVALID_PARAMETER"`) {
		t.Fatalf("非法请求体应 422，实际 %d %s", rec.Code, rec.Body.String())
	}
}

func TestRequestIDPassthrough(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.Header.Set("X-Request-ID", "rid-42")
	NewRouter(testDeps(nil)).ServeHTTP(rec, req)
	if rec.Header().Get("X-Request-ID") != "rid-42" {
		t.Fatalf("X-Request-ID 应透传")
	}
}

// TestRealAuthChain 用真实 auth 中间件验证 REST 入口的鉴权拒绝。
func TestRealAuthChain(t *testing.T) {
	jwt := auth.NewJWTManager("test-secret")
	mw := &auth.Middleware{JWT: jwt, Users: fakeIdentityProvider{}}
	d := testDeps(nil)
	d.AuthREST = mw.AuthenticateREST
	r := NewRouter(d)

	// 无凭证 → 401
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/products", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("无凭证应 401，实际 %d", rec.Code)
	}

	// 带合法 JWT（fake 用户 active）→ 到达 handler（fake admin 亦无妨）
	token, err := jwt.Issue("usr_1", "t", []string{"admin"}, time.Hour)
	if err != nil {
		t.Fatalf("签发失败: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/products", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code == http.StatusUnauthorized {
		t.Fatalf("合法 JWT 不应 401")
	}
}

type fakeIdentityProvider struct{}

func (fakeIdentityProvider) UserByID(_ context.Context, id string) (identity.User, error) {
	return identity.User{ID: id, Username: "t", Roles: identity.Roles{identity.RoleAdmin}, Status: identity.UserActive}, nil
}
func (fakeIdentityProvider) ResolvePAT(context.Context, string) (auth.PATInfo, error) {
	return auth.PATInfo{}, auth.ErrUnauthorized
}
func (fakeIdentityProvider) TouchPAT(context.Context, string) error { return nil }
