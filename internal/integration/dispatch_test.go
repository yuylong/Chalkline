package integration

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/yuylong/Chalkline/internal/domain/workitem"
)

// newTestDispatcher 构造允许全部出站的分发器（只用于 httptest 桩）。
func newTestDispatcher(cred DispatchCredentials) *Dispatcher {
	return NewDispatcher(NewSafeClient([]string{"*"}), cred)
}

func TestPushArtifactOutline(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/documents.create" {
			t.Errorf("路径 = %q", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer tok" {
			t.Errorf("缺少 Bearer 凭据：%q", r.Header.Get("Authorization"))
		}
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &got)
		_, _ = io.WriteString(w, `{"data":{"id":"doc-1","url":"/doc/doc-1"}}`)
	}))
	defer srv.Close()

	d := newTestDispatcher(DispatchCredentials{OutlineToken: "tok"})
	b := workitem.ProviderBinding{Category: workitem.CategoryArtifact, Provider: workitem.ProviderOutline,
		BaseURL: srv.URL, Target: "col-1"}
	// 有 ParentExternalID：复用已建好的「迭代/节点」父文档，不再新建容器。
	res, err := d.PushArtifact(t.Context(), b,
		workitem.ArtifactRef{Kind: workitem.KindSpec, Title: "需求规格", Content: "# 正文"},
		workitem.ArtifactMeta{}, workitem.DispatchTarget{
			IterationNumber: "CHK-IT1", NodeName: "设计", Version: 2, ParentExternalID: "parent-1",
		})
	if err != nil {
		t.Fatalf("推送失败：%v", err)
	}
	if res.ExternalID != "doc-1" || res.URL != srv.URL+"/doc/doc-1" {
		t.Fatalf("回执 = %+v", res)
	}
	if res.ParentExternalID != "parent-1" {
		t.Errorf("回执应带回父文档 id：%+v", res)
	}
	if got["collectionId"] != "col-1" || got["publish"] != true {
		t.Fatalf("请求体 = %+v", got)
	}
	// 标题必须由机器写入版本号，历史轮次才分得清。
	if got["title"] != "需求规格 · spec · v2" {
		t.Errorf("标题 = %q，期望带版本号", got["title"])
	}
	// 父文档必须是「迭代 / 节点」容器。
	if got["parentDocumentId"] != "parent-1" {
		t.Errorf("parentDocumentId = %v", got["parentDocumentId"])
	}
	// 路径是「容器 / 文档」的面包屑，供控制台直接展示文档落在哪里。
	if res.Path != "CHK-IT1 / 设计 / 需求规格 · spec · v2" {
		t.Errorf("Path = %q", res.Path)
	}
	if !strings.HasSuffix(res.ParentURL, "/doc/parent-1") {
		t.Errorf("ParentURL = %q", res.ParentURL)
	}
	text, _ := got["text"].(string)
	if !strings.Contains(text, "# 正文") || !strings.Contains(text, "CHK-IT1 / 设计") ||
		!strings.Contains(text, "第 2 版") {
		t.Errorf("正文应含原内容与来源标注：%q", text)
	}
}

// 无 ParentExternalID 时先按标题在 collection 内找容器；找不到就建一篇。
func TestPushArtifactOutlineCreatesNodeContainer(t *testing.T) {
	var created []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		switch r.URL.Path {
		case "/api/documents.list":
			// 空集合：容器不存在，需要新建。
			_, _ = io.WriteString(w, `{"data":[]}`)
		case "/api/documents.create":
			created = append(created, body)
			_, _ = io.WriteString(w, `{"data":{"id":"new-doc","url":"/doc/new-doc"}}`)
		default:
			t.Errorf("未预期路径 %q", r.URL.Path)
		}
	}))
	defer srv.Close()

	d := newTestDispatcher(DispatchCredentials{OutlineToken: "tok"})
	b := workitem.ProviderBinding{Provider: workitem.ProviderOutline, BaseURL: srv.URL, Target: "col-1"}
	res, err := d.PushArtifact(t.Context(), b,
		workitem.ArtifactRef{Kind: workitem.KindSpec, Title: "需求规格", Content: "# 正文"},
		workitem.ArtifactMeta{}, workitem.DispatchTarget{IterationNumber: "CHK-IT1", NodeName: "设计", Version: 1})
	if err != nil {
		t.Fatalf("推送失败：%v", err)
	}
	if len(created) != 2 {
		t.Fatalf("应依次建容器与材料两篇文档，实际 %d 次", len(created))
	}
	if created[0]["title"] != "CHK-IT1 / 设计" || created[0]["parentDocumentId"] != nil {
		t.Errorf("容器文档 = %+v", created[0])
	}
	if created[1]["parentDocumentId"] != "new-doc" {
		t.Errorf("材料文档应挂在容器下：%+v", created[1])
	}
	if created[1]["title"] != "需求规格 · spec · v1" {
		t.Errorf("材料标题 = %q", created[1]["title"])
	}
	if res.ParentExternalID != "new-doc" {
		t.Errorf("回执应返回新建的容器 id：%+v", res)
	}
}

// 容器已存在时复用，不重复建。
func TestPushArtifactOutlineReusesNodeContainer(t *testing.T) {
	var creates int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/documents.list":
			_, _ = io.WriteString(w, `{"data":[{"id":"exist-1","title":"CHK-IT1 / 设计","parentDocumentId":null}]}`)
		case "/api/documents.create":
			creates++
			_, _ = io.WriteString(w, `{"data":{"id":"child-1","url":"/doc/child-1"}}`)
		}
	}))
	defer srv.Close()

	d := newTestDispatcher(DispatchCredentials{OutlineToken: "tok"})
	b := workitem.ProviderBinding{Provider: workitem.ProviderOutline, BaseURL: srv.URL, Target: "col-1"}
	res, err := d.PushArtifact(t.Context(), b,
		workitem.ArtifactRef{Kind: workitem.KindSpec, Content: "# 正文"},
		workitem.ArtifactMeta{}, workitem.DispatchTarget{IterationNumber: "CHK-IT1", NodeName: "设计", Version: 3})
	if err != nil {
		t.Fatalf("推送失败：%v", err)
	}
	if creates != 1 {
		t.Errorf("只应创建材料文档（复用已有容器），实际 create %d 次", creates)
	}
	if res.ParentExternalID != "exist-1" {
		t.Errorf("应复用已有容器：%+v", res)
	}
}

func TestPushArtifactOutlineWithoutToken(t *testing.T) {
	d := newTestDispatcher(DispatchCredentials{})
	_, err := d.PushArtifact(t.Context(),
		workitem.ProviderBinding{Provider: workitem.ProviderOutline, BaseURL: "https://x.example.com", Target: "c"},
		workitem.ArtifactRef{Kind: workitem.KindSpec}, workitem.ArtifactMeta{}, workitem.DispatchTarget{})
	if err == nil {
		t.Fatal("未配置凭据时应失败")
	}
}

func TestPushArtifactGitRetriesWithUpdate(t *testing.T) {
	var calls int32
	var secondBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/api/v4/projects/team/repo/repository/commits") {
			t.Errorf("路径 = %q", r.URL.Path)
		}
		if r.Header.Get("PRIVATE-TOKEN") != "gtok" {
			t.Errorf("缺少 PRIVATE-TOKEN")
		}
		n := atomic.AddInt32(&calls, 1)
		raw, _ := io.ReadAll(r.Body)
		if n == 1 {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"message":"file exists"}`)
			return
		}
		_ = json.Unmarshal(raw, &secondBody)
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"id":"abc123"}`)
	}))
	defer srv.Close()

	d := newTestDispatcher(DispatchCredentials{GitToken: "gtok"})
	b := workitem.ProviderBinding{Provider: workitem.ProviderGit, BaseURL: srv.URL,
		Target: "team/repo", DirTemplate: "chalkline/{iteration}/{node}"}
	res, err := d.PushArtifact(t.Context(), b,
		workitem.ArtifactRef{Kind: workitem.KindDesign, NodeID: "design", Content: "# 设计"},
		workitem.ArtifactMeta{}, workitem.DispatchTarget{Dir: "chalkline/CHK-IT1/设计"})
	if err != nil {
		t.Fatalf("推送失败：%v", err)
	}
	if atomic.LoadInt32(&calls) != 2 {
		t.Fatalf("应重试一次，实际调用 %d 次", calls)
	}
	actions, _ := secondBody["actions"].([]any)
	if len(actions) != 1 {
		t.Fatalf("actions = %+v", secondBody)
	}
	first, _ := actions[0].(map[string]any)
	if first["action"] != "update" || first["file_path"] != "chalkline/CHK-IT1/设计/design.md" {
		t.Fatalf("第二次请求 = %+v", first)
	}
	// 历史由 git 自己的 commit 历史表达：回执必须带该 sha 的 permalink + 文件路径。
	if res.ExternalID != "abc123" {
		t.Fatalf("回执 = %+v", res)
	}
	if res.Path != "chalkline/CHK-IT1/设计/design.md" {
		t.Errorf("Path = %q", res.Path)
	}
	if !strings.Contains(res.URL, "/-/blob/abc123/") {
		t.Errorf("URL 应为该 sha 的 permalink：%q", res.URL)
	}
}

func TestPushArtifactSVN(t *testing.T) {
	var mkcol, put []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "MKCOL":
			mkcol = append(mkcol, r.URL.Path)
			w.WriteHeader(http.StatusCreated)
		case http.MethodPut:
			put = append(put, r.URL.Path)
			body, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(body), "# 用例") {
				t.Errorf("PUT 正文 = %q", body)
			}
			w.WriteHeader(http.StatusCreated)
		default:
			t.Errorf("未预期方法 %s", r.Method)
		}
	}))
	defer srv.Close()

	d := newTestDispatcher(DispatchCredentials{SVNToken: "stok"})
	b := workitem.ProviderBinding{Provider: workitem.ProviderSVN, BaseURL: srv.URL,
		Target: "repo/trunk", DirTemplate: "chalkline/{iteration}"}
	res, err := d.PushArtifact(t.Context(), b,
		workitem.ArtifactRef{Kind: workitem.KindTestCases, Content: "# 用例"},
		workitem.ArtifactMeta{}, workitem.DispatchTarget{Dir: "chalkline/CHK-IT1"})
	if err != nil {
		t.Fatalf("推送失败：%v", err)
	}
	if len(mkcol) == 0 || !strings.HasSuffix(mkcol[len(mkcol)-1], "/chalkline/CHK-IT1/") {
		t.Fatalf("MKCOL 路径 = %v", mkcol)
	}
	if len(put) != 1 || !strings.HasSuffix(put[0], "/chalkline/CHK-IT1/test_cases.md") {
		t.Fatalf("PUT 路径 = %v", put)
	}
	if !strings.HasSuffix(res.Summary, "test_cases.md") {
		t.Fatalf("回执 = %+v", res)
	}
}

func TestProcessTicketAndComment(t *testing.T) {
	var notes string
	var issueBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Redmine-API-Key") != "rtok" {
			t.Errorf("缺少 Redmine 凭据")
		}
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/projects/chalk/issues.json":
			raw, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(raw, &issueBody)
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{"issue":{"id":42}}`)
		case r.Method == http.MethodPut && r.URL.Path == "/issues/42.json":
			var body struct {
				Issue struct {
					Notes string `json:"notes"`
				} `json:"issue"`
			}
			raw, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(raw, &body)
			notes = body.Issue.Notes
			_, _ = io.WriteString(w, `{}`)
		default:
			t.Errorf("未预期请求 %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	d := newTestDispatcher(DispatchCredentials{RedmineToken: "rtok"})
	b := workitem.ProviderBinding{Provider: workitem.ProviderRedmine, BaseURL: srv.URL, Target: "chalk"}
	ticket, err := d.EnsureTicket(t.Context(), b, workitem.ProcessSubject{
		Type: "node", ID: "itr_1:n1", Title: "CHK-IT1 迭代 · 设计", Description: "阶段流程记录",
	})
	if err != nil {
		t.Fatalf("建票失败：%v", err)
	}
	if ticket.ExternalID != "42" || ticket.URL != srv.URL+"/issues/42" {
		t.Fatalf("票 = %+v", ticket)
	}
	issue, _ := issueBody["issue"].(map[string]any)
	if issue["project_id"] != "chalk" || issue["subject"] != "CHK-IT1 迭代 · 设计" {
		t.Fatalf("建票请求 = %+v", issueBody)
	}
	if err := d.CommentTicket(t.Context(), b, ticket, workitem.ProcessNote{Body: "### 开始节点"}); err != nil {
		t.Fatalf("追加回复失败：%v", err)
	}
	if notes != "### 开始节点" {
		t.Fatalf("回复正文 = %q", notes)
	}
}

func TestPushArtifactBlockedByAllowlist(t *testing.T) {
	d := NewDispatcher(NewSafeClient(nil), DispatchCredentials{OutlineToken: "tok"})
	_, err := d.PushArtifact(t.Context(),
		workitem.ProviderBinding{Provider: workitem.ProviderOutline, BaseURL: "https://outline.example.com", Target: "c"},
		workitem.ArtifactRef{Kind: workitem.KindSpec, Content: "x"}, workitem.ArtifactMeta{}, workitem.DispatchTarget{})
	if err == nil {
		t.Fatal("白名单为空时应拒绝出站")
	}
}

func TestPushArtifactUsesBindingToken(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_, _ = io.WriteString(w, `{"data":{"id":"d1","url":"/doc/d1"}}`)
	}))
	defer srv.Close()

	// 环境变量未配置凭据：只用迭代级 Key 也必须能推送。
	d := newTestDispatcher(DispatchCredentials{})
	b := workitem.ProviderBinding{
		Category: workitem.CategoryArtifact, Provider: workitem.ProviderOutline,
		BaseURL: srv.URL, Target: "col",
		Auth: workitem.ProviderAuth{Type: workitem.AuthToken, Secret: "iter-token"},
	}
	if _, err := d.PushArtifact(t.Context(), b,
		workitem.ArtifactRef{Kind: workitem.KindSpec, Content: "# 正文"}, workitem.ArtifactMeta{}, workitem.DispatchTarget{Dir: "dir"}); err != nil {
		t.Fatalf("推送失败：%v", err)
	}
	if gotAuth != "Bearer iter-token" {
		t.Fatalf("Authorization = %q", gotAuth)
	}
}

func TestPushArtifactUsesBasicAuth(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	d := newTestDispatcher(DispatchCredentials{})
	b := workitem.ProviderBinding{
		Provider: workitem.ProviderSVN, BaseURL: srv.URL, Target: "repo",
		Auth: workitem.ProviderAuth{Type: workitem.AuthBasic, Username: "alice", Secret: "s3cret"},
	}
	if _, err := d.PushArtifact(t.Context(), b,
		workitem.ArtifactRef{Kind: workitem.KindTestCases, Content: "x"}, workitem.ArtifactMeta{}, workitem.DispatchTarget{Dir: "dir"}); err != nil {
		t.Fatalf("推送失败：%v", err)
	}
	want := "Basic " + base64.StdEncoding.EncodeToString([]byte("alice:s3cret"))
	if gotAuth != want {
		t.Fatalf("Authorization = %q，期望 %q", gotAuth, want)
	}
}

func TestProcessTicketUsesBindingToken(t *testing.T) {
	var gotKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("X-Redmine-API-Key")
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"issue":{"id":7}}`)
	}))
	defer srv.Close()

	d := newTestDispatcher(DispatchCredentials{})
	b := workitem.ProviderBinding{Provider: workitem.ProviderRedmine, BaseURL: srv.URL, Target: "chalk",
		Auth: workitem.ProviderAuth{Type: workitem.AuthToken, Secret: "redmine-key"}}
	if _, err := d.EnsureTicket(t.Context(), b, workitem.ProcessSubject{Type: "task", ID: "tsk_1", Title: "T"}); err != nil {
		t.Fatalf("建票失败：%v", err)
	}
	if gotKey != "redmine-key" {
		t.Fatalf("X-Redmine-API-Key = %q", gotKey)
	}
}

func TestAuthHeadersRequiresConfiguredSecret(t *testing.T) {
	d := newTestDispatcher(DispatchCredentials{})
	if _, err := d.authHeaders(workitem.ProviderBinding{Provider: workitem.ProviderOutline,
		Auth: workitem.ProviderAuth{Type: workitem.AuthToken}}); err == nil {
		t.Fatal("token 认证缺少 Secret 时应报错")
	}
	if _, err := d.authHeaders(workitem.ProviderBinding{Provider: workitem.ProviderGit,
		Auth: workitem.ProviderAuth{Type: workitem.AuthBasic, Username: "u"}}); err == nil {
		t.Fatal("basic 认证缺少密码时应报错")
	}
}
