package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

func testDeps() Deps {
	return Deps{
		Logger:  slog.New(slog.DiscardHandler),
		Version: "test",
		Ready:   func() bool { return true },
		Ping:    func(*http.Request) error { return nil },
	}
}

func get(t *testing.T, h http.Handler, path string) (int, map[string]any) {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	return rec.Code, body
}

func TestHealthzAlwaysOK(t *testing.T) {
	h := NewRouter(testDeps())
	code, body := get(t, h, "/healthz")
	if code != http.StatusOK || body["status"] != "ok" {
		t.Fatalf("healthz = %d %v", code, body)
	}
}

func TestReadyzReflectsDBState(t *testing.T) {
	// 数据库未就绪 → 503。
	d := testDeps()
	d.Ready = func() bool { return false }
	code, body := get(t, NewRouter(d), "/readyz")
	if code != http.StatusServiceUnavailable || body["status"] != "unavailable" {
		t.Fatalf("readyz = %d %v", code, body)
	}
	// 就绪但 ping 失败 → 503。
	d.Ready = func() bool { return true }
	d.Ping = func(*http.Request) error { return errors.New("connection reset") }
	code, _ = get(t, NewRouter(d), "/readyz")
	if code != http.StatusServiceUnavailable {
		t.Fatalf("ping 失败应 503, 实际 %d", code)
	}
	// 完全就绪 → 200。
	d.Ping = func(*http.Request) error { return nil }
	code, body = get(t, NewRouter(d), "/readyz")
	if code != http.StatusOK || body["status"] != "ready" {
		t.Fatalf("readyz = %d %v", code, body)
	}
}

func TestInfo(t *testing.T) {
	code, body := get(t, NewRouter(testDeps()), "/api/v1/info")
	if code != http.StatusOK || body["name"] != "chalkline" || body["milestone"] != "M0" {
		t.Fatalf("info = %d %v", code, body)
	}
}

func TestUniformErrorEnvelope(t *testing.T) {
	h := NewRouter(testDeps())
	// 404：统一包络 {error:{code,message}}。
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/nope", nil))
	var body struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("响应非 JSON: %v", err)
	}
	if rec.Code != http.StatusNotFound || body.Error.Code != "NOT_FOUND" {
		t.Fatalf("404 包络异常: %d %+v", rec.Code, body)
	}
	// 405。
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/healthz", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("405 期望, 实际 %d", rec.Code)
	}
}

func TestRequestIDHeader(t *testing.T) {
	h := NewRouter(testDeps())
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Header().Get("X-Request-ID") == "" {
		t.Fatal("每个请求都应携带 X-Request-ID")
	}
}
