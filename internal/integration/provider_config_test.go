package integration

// provider_config_test.go：运行时 provider 配置（管理台启停 / baseURL 覆盖）。

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/yuylong/Chalkline/internal/domain/workitem"
)

// TestRegistryConfigDisablesSnapshot 停用后不产生任何出站请求，降级为引用快照。
func TestRegistryConfigDisablesSnapshot(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"title": "不该被请求"}})
	}))
	defer srv.Close()

	reg := NewRegistry(NewOutlineProvider(NewSafeClient([]string{"127.0.0.1"}), srv.URL, "secret-token")).
		WithConfig(func(_ context.Context, _ workitem.ArtifactProvider) (ProviderConfig, bool) {
			return ProviderConfig{Enabled: false}, true
		})

	ref := outlineRef("https://outline.example.com/doc/doc-42", "doc-42")
	meta, err := reg.Snapshot(context.Background(), ref)
	if err != nil {
		t.Fatalf("停用时 Snapshot 不应报错: %v", err)
	}
	if meta.Title != "doc-42" {
		t.Fatalf("停用时应降级为引用尾段标题，得到 %q", meta.Title)
	}
	if hits.Load() != 0 {
		t.Fatalf("停用时不应出站，实际 %d 次", hits.Load())
	}
}

// TestRegistryConfigOverridesBaseURL 库内 baseURL 覆盖环境变量值，Snapshot 打到新地址。
func TestRegistryConfigOverridesBaseURL(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.URL.Query().Get("id") != "doc-42" {
			_, _ = w.Write([]byte(`{"error":"bad id"}`))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"title": "覆盖后的标题"}})
	}))
	defer srv.Close()

	// 构造时 baseURL 为空（环境变量未配置），仅靠运行时配置提供。
	reg := NewRegistry(NewOutlineProvider(NewSafeClient([]string{"127.0.0.1"}), "", "secret-token")).
		WithConfig(func(_ context.Context, _ workitem.ArtifactProvider) (ProviderConfig, bool) {
			return ProviderConfig{Enabled: true, BaseURL: srv.URL}, true
		})

	meta, err := reg.Snapshot(context.Background(), outlineRef("https://outline.example.com/doc/doc-42", "doc-42"))
	if err != nil {
		t.Fatalf("Snapshot 不应报错: %v", err)
	}
	if meta.Title != "覆盖后的标题" {
		t.Fatalf("应使用运行时 baseURL，得到 %q", meta.Title)
	}
	if hits.Load() != 1 {
		t.Fatalf("应恰好一次出站，实际 %d", hits.Load())
	}
}

// TestRegistryConfigKeepsLocalValidate 停用只跳过外部快照，私网地址校验仍执行。
func TestRegistryConfigKeepsLocalValidate(t *testing.T) {
	reg := NewRegistry(NewOutlineProvider(NewSafeClient(nil), "", "")).
		WithConfig(func(_ context.Context, _ workitem.ArtifactProvider) (ProviderConfig, bool) {
			return ProviderConfig{Enabled: false}, true
		})
	if err := reg.Validate(context.Background(), outlineRef("http://127.0.0.1/doc/x", "x")); err == nil {
		t.Fatal("停用后仍应拒绝私网/回环地址")
	}
}
