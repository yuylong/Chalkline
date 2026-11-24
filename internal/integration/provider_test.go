package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/yuylong/Chalkline/internal/domain/workitem"
)

func gitPRRef(url string) workitem.ArtifactRef {
	return workitem.ArtifactRef{
		Kind: workitem.KindPR, Provider: workitem.ProviderGit,
		URL: url, NodeID: "node_dev", SubmittedBy: "usr_1",
	}
}

func outlineRef(url, externalID string) workitem.ArtifactRef {
	return workitem.ArtifactRef{
		Kind: workitem.KindSpec, Provider: workitem.ProviderOutline,
		URL: url, ExternalID: externalID, NodeID: "node_design", SubmittedBy: "usr_1",
	}
}

// TestRegistryRoutesByProvider 验证注册表按 provider 路由且未注册 provider 降级。
func TestRegistryRoutesByProvider(t *testing.T) {
	r := NewRegistry(NewGitProvider(NewSafeClient(nil)), NewSVNProvider(NewSafeClient(nil)))
	if _, ok := r.Get(workitem.ProviderGit); !ok {
		t.Fatal("git 未注册")
	}
	if _, ok := r.Get(workitem.ProviderOutline); ok {
		t.Fatal("outline 不应注册")
	}
	// 未注册 provider：Validate 通过、Snapshot 降级为 URL 尾段。
	if err := r.Validate(context.Background(), outlineRef("https://x.example.com/doc/abc", "")); err != nil {
		t.Fatalf("未注册 provider 不应报错: %v", err)
	}
	meta, err := r.Snapshot(context.Background(), outlineRef("https://x.example.com/doc/abc", ""))
	if err != nil || meta.Title != "abc" {
		t.Fatalf("降级快照失败: %v %+v", err, meta)
	}
}

// TestGitValidateRejectsPrivateHosts Validate 只做本地校验：私网/回环/元数据地址拒绝
// （SSRF 底线），公网任意 host 均放行（白名单只约束 Snapshot 出站，见下一测试）。
func TestGitValidateRejectsPrivateHosts(t *testing.T) {
	all := NewGitProvider(NewSafeClient([]string{"*"}))
	ctx := context.Background()

	for _, raw := range []string{
		"https://git.example.com/team/x/-/merge_requests/1",
		"https://evil.com/x",
		"http://some.host:8080/pr/1",
	} {
		if err := all.Validate(ctx, gitPRRef(raw)); err != nil {
			t.Fatalf("公网 URL 应通过本地校验: %v", err)
		}
	}
	for _, raw := range []string{
		"http://127.0.0.1:8080/pr/1",
		"http://169.254.169.254/latest/meta-data",
		"http://10.1.2.3/x",
		"http://localhost/x",
	} {
		if err := all.Validate(ctx, gitPRRef(raw)); err == nil {
			t.Fatalf("内网/保留地址应拒绝: %s", raw)
		}
	}
}

// TestSnapshotNoOutboundByDefault 白名单为空（默认）时 Snapshot 不产生任何网络请求
// （Validate 不出站，Snapshot 过白名单被拦后降级为引用快照）。
func TestSnapshotNoOutboundByDefault(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		_, _ = w.Write([]byte(`{"data":{"title":"不该被请求"}}`))
	}))
	defer srv.Close()

	r := NewRegistry(
		NewGitProvider(NewSafeClient(nil)),
		NewOutlineProvider(NewSafeClient(nil), srv.URL, "token"),
		NewRedmineProvider(NewSafeClient(nil), srv.URL, "token"),
	)
	ctx := context.Background()
	refs := []workitem.ArtifactRef{
		gitPRRef("https://git.example.com/mr/1"),
		outlineRef("https://outline.example.com/doc/abc", "abc"),
		{Kind: workitem.KindFile, Provider: workitem.ProviderRedmine,
			URL: "https://redmine.example.com/issues/9", ExternalID: "9", NodeID: "n", SubmittedBy: "u"},
	}
	for _, ref := range refs {
		if err := r.Validate(ctx, ref); err != nil {
			t.Fatalf("默认（白名单空）Validate 应只做本地校验: %v", err)
		}
		if _, err := r.Snapshot(ctx, ref); err != nil {
			t.Fatalf("Snapshot 应降级而非报错: %v", err)
		}
	}
	if got := hits.Load(); got != 0 {
		t.Fatalf("白名单空时不应有出站请求，实际 %d 次", got)
	}
}

// TestOutlineSnapshotFetchesTitle 白名单命中时 Outline Snapshot 调 API 取标题。
func TestOutlineSnapshotFetchesTitle(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.URL.Query().Get("token") != "secret-token" || r.URL.Query().Get("id") != "doc-42" {
			_, _ = w.Write([]byte(`{"error":"bad request"}`))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{"title": "网关设计文档"},
		})
	}))
	defer srv.Close()

	o := NewOutlineProvider(NewSafeClient([]string{"127.0.0.1"}), srv.URL, "secret-token")
	meta, err := o.Snapshot(context.Background(), outlineRef(srv.URL+"/doc/doc-42", "doc-42"))
	if err != nil {
		t.Fatalf("Snapshot 不应报错: %v", err)
	}
	if meta.Title != "网关设计文档" {
		t.Fatalf("应取到 API 标题，得到 %q", meta.Title)
	}
	if hits.Load() != 1 {
		t.Fatalf("应恰好一次 API 调用，实际 %d", hits.Load())
	}
}

// TestRedmineSnapshotFetchesSubject 白名单命中时 Redmine Snapshot 取 issue 标题与状态；
// externalId 带 # 前缀也能解析。
func TestRedmineSnapshotFetchesSubject(t *testing.T) {
	var gotPath atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath.Store(r.URL.Path)
		if r.Header.Get("X-Redmine-API-Key") != "key-1" {
			_, _ = w.Write([]byte(`{}`))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issue": map[string]any{
				"subject": "修复登录超时",
				"status":  map[string]any{"name": "In Progress"},
			},
		})
	}))
	defer srv.Close()

	p := NewRedmineProvider(NewSafeClient([]string{"127.0.0.1"}), srv.URL, "key-1")
	ref := workitem.ArtifactRef{
		Kind: workitem.KindFile, Provider: workitem.ProviderRedmine,
		URL: srv.URL + "/issues/7", ExternalID: "#7", NodeID: "n", SubmittedBy: "u",
	}
	meta, err := p.Snapshot(context.Background(), ref)
	if err != nil {
		t.Fatalf("Snapshot 不应报错: %v", err)
	}
	if meta.Title != "修复登录超时" || meta.Status != "In Progress" {
		t.Fatalf("快照不符: %+v", meta)
	}
	if gotPath.Load() != "/issues/7.json" {
		t.Fatalf("请求路径不符: %v", gotPath.Load())
	}
}

// TestSnapshotDegradesOnFailure 外部 500 时降级为引用快照而非报错（不变量 15）。
func TestSnapshotDegradesOnFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	o := NewOutlineProvider(NewSafeClient([]string{"127.0.0.1"}), srv.URL, "token")
	meta, err := o.Snapshot(context.Background(), outlineRef(srv.URL+"/doc/abc", "abc"))
	if err != nil {
		t.Fatalf("外部失败应降级: %v", err)
	}
	if meta.Title != "abc" {
		t.Fatalf("降级标题应为 URL 尾段: %+v", meta)
	}
}

// TestSafeClientRefusesRedirect 出站不跟随重定向（防 SSRF 跳转绕过）。
func TestSafeClientRefusesRedirect(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/start" {
			http.Redirect(w, r, "/landing", http.StatusFound)
			return
		}
		_, _ = fmt.Fprint(w, "landed")
	}))
	defer srv.Close()

	c := NewSafeClient([]string{"127.0.0.1"})
	resp, err := c.doGet(context.Background(), srv.URL+"/start", nil)
	if err != nil {
		t.Fatalf("请求不应失败: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("应原样返回 302 而非跟随，得到 %d", resp.StatusCode)
	}
}

// TestTailOfURL 标题兜底解析。
func TestTailOfURL(t *testing.T) {
	cases := map[string]string{
		"https://x.com/a/b/c": "c",
		"https://x.com/a/":    "a",
		"https://x.com":       "x.com",
	}
	for in, want := range cases {
		if got := tailOfURL(in); got != want {
			t.Errorf("tailOfURL(%q) = %q, want %q", in, got, want)
		}
	}
}
