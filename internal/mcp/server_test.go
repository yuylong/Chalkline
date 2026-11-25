package mcp

// server_test.go：MCP 协议层测试——stateless streamable HTTP、工具清单、
// PAT 身份注入与 mcp.md 错误包络。

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yuylong/Chalkline/internal/app"
	"github.com/yuylong/Chalkline/internal/auth"
	"github.com/yuylong/Chalkline/internal/domain/identity"
	"github.com/yuylong/Chalkline/internal/platform"
)

type fakeSvc struct {
	app.API
	whoamiFn func(ctx context.Context, actor app.Actor) (app.WhoamiView, error)
}

func (f fakeSvc) Whoami(ctx context.Context, actor app.Actor) (app.WhoamiView, error) {
	return f.whoamiFn(ctx, actor)
}

func newTestHandler(whoamiFn func(context.Context, app.Actor) (app.WhoamiView, error)) http.Handler {
	d := Deps{
		Logger: platform.NewLogger("error"),
		Svc:    func() app.API { return fakeSvc{whoamiFn: whoamiFn} },
	}
	// 测试注入固定 PAT 身份（真实 PAT 校验链由 internal/auth 覆盖）。
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(auth.WithActor(r.Context(),
				app.Actor{UserID: "usr_1", Username: "agent", Roles: identity.Roles{identity.RoleDev}, ViaPAT: true})))
		})
	}(NewHandler(d))
}

// callMCP 发一次 JSON-RPC POST 并解析响应（JSON 或 SSE 均可）。
func callMCP(t *testing.T, h http.Handler, method string, params map[string]any) (int, map[string]any) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	contentType := rec.Header().Get("Content-Type")
	payload := rec.Body.String()
	if strings.Contains(contentType, "text/event-stream") {
		// 取首个 data: 行。
		for _, line := range strings.Split(payload, "\n") {
			if strings.HasPrefix(line, "data:") {
				payload = strings.TrimSpace(strings.TrimPrefix(line, "data:"))
				break
			}
		}
	}
	var out map[string]any
	_ = json.Unmarshal([]byte(payload), &out)
	return rec.Code, out
}

// unwrapResult 解开 JSON-RPC result 层。
func unwrapResult(t *testing.T, out map[string]any) map[string]any {
	t.Helper()
	res, ok := out["result"].(map[string]any)
	if !ok {
		t.Fatalf("响应缺 result: %v", out)
	}
	return res
}

func TestToolsListContract(t *testing.T) {
	h := newTestHandler(func(context.Context, app.Actor) (app.WhoamiView, error) {
		return app.WhoamiView{}, nil
	})
	code, out := callMCP(t, h, "tools/list", map[string]any{})
	if code != http.StatusOK {
		t.Fatalf("tools/list 应 200，实际 %d: %v", code, out)
	}
	result := unwrapResult(t, out)
	tools, ok := result["tools"].([]any)
	if !ok {
		t.Fatalf("响应缺 tools: %v", out)
	}
	want := map[string]bool{
		// 读（12）
		"chalkline_whoami": false, "chalkline_list_products": false,
		"chalkline_get_product": false, "chalkline_list_iterations": false,
		"chalkline_list_tasks": false, "chalkline_get_task": false,
		"chalkline_get_next_action": false, "chalkline_get_workflow": false,
		"chalkline_list_workflows": false, "chalkline_get_pending_reviews": false,
		"chalkline_list_product_members": false, "chalkline_get_iteration_providers": false,
		// 写（13）
		"chalkline_create_iteration": false, "chalkline_create_task": false,
		"chalkline_decompose_task": false, "chalkline_assign_task": false,
		"chalkline_link_module": false, "chalkline_start_step": false,
		"chalkline_submit_step": false, "chalkline_attach_artifact": false,
		"chalkline_review_step": false, "chalkline_set_iteration_providers": false,
		"chalkline_set_iteration_provider_credential": false,
		"chalkline_upsert_product_member": false,
		"chalkline_remove_product_member": false,
	}
	for _, tv := range tools {
		m, _ := tv.(map[string]any)
		name, _ := m["name"].(string)
		if seen, ok := want[name]; !ok {
			t.Fatalf("未预期的工具 %q", name)
		} else if seen {
			t.Fatalf("工具 %q 重复注册", name)
		}
		want[name] = true
		if desc, _ := m["description"].(string); desc == "" {
			t.Fatalf("工具 %q 缺 description", name)
		}
	}
	for name, seen := range want {
		if !seen {
			t.Fatalf("工具 %q 未注册", name)
		}
	}
	if len(tools) != len(want) {
		t.Fatalf("工具数 %d ≠ 契约 %d", len(tools), len(want))
	}
}

func TestWhoamiToolCall(t *testing.T) {
	h := newTestHandler(func(_ context.Context, actor app.Actor) (app.WhoamiView, error) {
		return app.WhoamiView{
			User: app.UserView{ID: actor.UserID, Username: actor.Username},
			Via:  "pat",
		}, nil
	})
	code, out := callMCP(t, h, "tools/call", map[string]any{
		"name": "chalkline_whoami", "arguments": map[string]any{},
	})
	if code != http.StatusOK {
		t.Fatalf("tools/call 应 200，实际 %d: %v", code, out)
	}
	result := unwrapResult(t, out)
	if result["isError"] == true {
		t.Fatalf("whoami 不应报错: %v", out)
	}
	if !strings.Contains(rawJSON(result), "agent") {
		t.Fatalf("结构化结果应含身份: %v", result)
	}
}

func TestErrorEnvelopeFromAppError(t *testing.T) {
	h := newTestHandler(func(context.Context, app.Actor) (app.WhoamiView, error) {
		return app.WhoamiView{}, app.NotFound("成员不存在")
	})
	code, out := callMCP(t, h, "tools/call", map[string]any{
		"name": "chalkline_whoami", "arguments": map[string]any{},
	})
	if code != http.StatusOK {
		t.Fatalf("工具级错误仍应 HTTP 200（IsError 承载），实际 %d", code)
	}
	result := unwrapResult(t, out)
	content, _ := result["content"].([]any)
	if len(content) == 0 {
		t.Fatalf("错误结果应含 content: %v", result)
	}
	first, _ := content[0].(map[string]any)
	text, _ := first["text"].(string)
	if result["isError"] != true ||
		!strings.Contains(text, `"code":"NOT_FOUND"`) ||
		!strings.Contains(text, "成员不存在") {
		t.Fatalf("错误应为统一包络文本，实际: %s", text)
	}
}

func TestUnknownWorkItemTypeRejected(t *testing.T) {
	h := newTestHandler(func(context.Context, app.Actor) (app.WhoamiView, error) {
		return app.WhoamiView{}, nil
	})
	code, out := callMCP(t, h, "tools/call", map[string]any{
		"name":      "chalkline_get_next_action",
		"arguments": map[string]any{"workItemType": "order", "workItemId": "x"},
	})
	if code != http.StatusOK {
		t.Fatalf("实际 %d: %v", code, out)
	}
	if !strings.Contains(rawJSON(unwrapResult(t, out)), "INVALID_PARAMETER") {
		t.Fatalf("非法 workItemType 应 INVALID_PARAMETER: %v", out)
	}
}

func rawJSON(v map[string]any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
