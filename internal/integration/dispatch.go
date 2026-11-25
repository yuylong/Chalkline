package integration

// dispatch.go：成果物分发与流程记录的出站实现（docs/domain/integrations.md「迭代 Provider」）。
//
// 分工：
//   - 成果物 provider：把材料按迭代约定的目录结构写到外部系统（inline 由 app 层直接存储）；
//   - 流程记录 provider：为节点/任务建票，并把节点动作作为回复追加到票上。
//
// 所有出站都过 safeClient 白名单（防 SSRF），凭据只来自环境变量，错误信息不携带凭据。

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"

	"github.com/yuylong/Chalkline/internal/domain/workitem"
)

// DispatchCredentials 是分发所需的凭据（只来自环境变量，不落库）。
type DispatchCredentials struct {
	OutlineToken string
	RedmineToken string
	GitToken     string
	SVNToken     string
}

// Dispatcher 实现 app.ArtifactDistributor 与 app.ProcessRecorder。
type Dispatcher struct {
	client *safeClient
	cred   DispatchCredentials
}

// NewDispatcher 构造分发器。
func NewDispatcher(c *safeClient, cred DispatchCredentials) *Dispatcher {
	return &Dispatcher{client: c, cred: cred}
}

// PushArtifact 把成果物写到迭代绑定的外部 provider，只返回摘要与链接。
func (d *Dispatcher) PushArtifact(ctx context.Context, b workitem.ProviderBinding,
	art workitem.ArtifactRef, meta workitem.ArtifactMeta, dir string) (workitem.PushedArtifact, error) {
	switch b.Provider {
	case workitem.ProviderOutline:
		return d.pushOutline(ctx, b, art, meta)
	case workitem.ProviderGit:
		return d.pushGit(ctx, b, art, meta, dir)
	case workitem.ProviderSVN:
		return d.pushSVN(ctx, b, art, meta, dir)
	}
	return workitem.PushedArtifact{}, errRef("provider %q 不支持成果物分发", b.Provider)
}

// ---- Outline ----

// pushOutline 在绑定的 collection 下建文档；标题用「迭代 / 节点 / 类型」表达目录层级
// （Outline 没有目录概念，集合 + 标题约定即其组织方式）。
func (d *Dispatcher) pushOutline(ctx context.Context, b workitem.ProviderBinding,
	art workitem.ArtifactRef, meta workitem.ArtifactMeta) (workitem.PushedArtifact, error) {
	if d.cred.OutlineToken == "" {
		return workitem.PushedArtifact{}, errRef("Outline 凭据未配置（CHALKLINE_OUTLINE_TOKEN）")
	}
	title := outlineTitle(art, meta)
	text := art.Content
	if strings.TrimSpace(text) == "" {
		text = fmt.Sprintf("外部成果物引用：%s", art.URL)
		if art.URL == "" {
			text = "（无正文，详见 Chalkline 材料记录）"
		}
	}
	body := map[string]any{
		"title": title, "text": text, "publish": true, "collectionId": b.Target,
	}
	status, raw, err := d.send(ctx, http.MethodPost, joinURL(b.BaseURL, "api/documents.create"),
		map[string]string{"Authorization": "Bearer " + d.cred.OutlineToken}, body)
	if err != nil {
		return workitem.PushedArtifact{}, err
	}
	if status != http.StatusOK && status != http.StatusCreated {
		return workitem.PushedArtifact{}, errRef("Outline 建文档失败（HTTP %d）：%s", status, snippet(raw))
	}
	var out struct {
		Data struct {
			ID  string `json:"id"`
			URL string `json:"url"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return workitem.PushedArtifact{}, errRef("Outline 响应无法解析：%s", snippet(raw))
	}
	link := out.Data.URL
	if strings.HasPrefix(link, "/") {
		link = joinURL(b.BaseURL, link)
	}
	return workitem.PushedArtifact{ExternalID: out.Data.ID, URL: link, Summary: title}, nil
}

// outlineTitle 生成文档标题：<前缀>/<类型>，前缀取材料标题或快照标题。
func outlineTitle(art workitem.ArtifactRef, meta workitem.ArtifactMeta) string {
	name := strings.TrimSpace(art.Title)
	if name == "" {
		name = strings.TrimSpace(meta.Title)
	}
	if name == "" {
		name = string(art.Kind)
	}
	return fmt.Sprintf("%s · %s", name, art.Kind)
}

// ---- Git（GitLab 兼容 commits API）----

// pushGit 提交单个文件到仓库约定路径（文件已存在时自动改用 update）。
func (d *Dispatcher) pushGit(ctx context.Context, b workitem.ProviderBinding,
	art workitem.ArtifactRef, meta workitem.ArtifactMeta, dir string) (workitem.PushedArtifact, error) {
	if d.cred.GitToken == "" {
		return workitem.PushedArtifact{}, errRef("Git 凭据未配置（CHALKLINE_GIT_TOKEN）")
	}
	branch := strings.TrimSpace(b.Branch)
	if branch == "" {
		branch = "main"
	}
	filePath := artifactFilePath(dir, art.Kind)
	target := joinURL(b.BaseURL, "api/v4/projects/"+url.PathEscape(b.Target)+"/repository/commits")
	commit := func(action string) (int, []byte, error) {
		body := map[string]any{
			"branch":         branch,
			"commit_message": fmt.Sprintf("chore(chalkline): %s %s", art.NodeID, art.Kind),
			"actions": []map[string]any{{
				"action": action, "file_path": filePath, "content": pushContent(art, meta),
			}},
		}
		return d.send(ctx, http.MethodPost, target,
			map[string]string{"PRIVATE-TOKEN": d.cred.GitToken}, body)
	}
	status, raw, err := commit("create")
	if err != nil {
		return workitem.PushedArtifact{}, err
	}
	if status == http.StatusBadRequest { // 文件已存在：改用 update 覆盖
		status, raw, err = commit("update")
		if err != nil {
			return workitem.PushedArtifact{}, err
		}
	}
	if status != http.StatusCreated && status != http.StatusOK {
		return workitem.PushedArtifact{}, errRef("Git 提交失败（HTTP %d）：%s", status, snippet(raw))
	}
	var out struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(raw, &out)
	return workitem.PushedArtifact{
		ExternalID: out.ID,
		URL:        joinURL(b.BaseURL, b.Target),
		Summary:    filePath,
	}, nil
}

// ---- SVN（WebDAV：MKCOL 目录 + PUT 文件）----

// pushSVN 通过 WebDAV 写入约定路径；已存在的目录返回 405 视为成功。
func (d *Dispatcher) pushSVN(ctx context.Context, b workitem.ProviderBinding,
	art workitem.ArtifactRef, meta workitem.ArtifactMeta, dir string) (workitem.PushedArtifact, error) {
	headers := map[string]string{}
	if d.cred.SVNToken != "" {
		headers["Authorization"] = "Bearer " + d.cred.SVNToken
	}
	segments := []string{}
	if strings.TrimSpace(b.Target) != "" {
		segments = append(segments, strings.Trim(b.Target, "/"))
	}
	if dir != "" {
		segments = append(segments, strings.Trim(dir, "/"))
	}
	prefix := ""
	for _, seg := range segments {
		prefix = path.Join(prefix, seg)
		status, raw, err := d.send(ctx, "MKCOL", joinURL(b.BaseURL, prefix)+"/", headers, nil)
		if err != nil {
			return workitem.PushedArtifact{}, err
		}
		// 405 = 已存在；201/200 = 新建成功。
		if status != http.StatusCreated && status != http.StatusOK && status != http.StatusMethodNotAllowed {
			return workitem.PushedArtifact{}, errRef("SVN 建目录失败（HTTP %d）：%s", status, snippet(raw))
		}
	}
	filePath := artifactFilePath(prefix, art.Kind)
	status, raw, err := d.send(ctx, http.MethodPut, joinURL(b.BaseURL, filePath),
		withContentType(headers, "text/markdown; charset=utf-8"), []byte(pushContent(art, meta)))
	if err != nil {
		return workitem.PushedArtifact{}, err
	}
	if status != http.StatusCreated && status != http.StatusOK && status != http.StatusNoContent {
		return workitem.PushedArtifact{}, errRef("SVN 写文件失败（HTTP %d）：%s", status, snippet(raw))
	}
	return workitem.PushedArtifact{
		ExternalID: filePath,
		URL:        joinURL(b.BaseURL, filePath),
		Summary:    filePath,
	}, nil
}

// ---- 流程记录（Redmine）----

// EnsureTicket 在绑定的 Redmine project 下建票。
func (d *Dispatcher) EnsureTicket(ctx context.Context, b workitem.ProviderBinding,
	subject workitem.ProcessSubject) (workitem.ProcessTicket, error) {
	if d.cred.RedmineToken == "" {
		return workitem.ProcessTicket{}, errRef("Redmine 凭据未配置（CHALKLINE_REDMINE_TOKEN）")
	}
	body := map[string]any{"issue": map[string]any{
		"project_id":  b.Target,
		"subject":     subject.Title,
		"description": subject.Description,
	}}
	status, raw, err := d.send(ctx, http.MethodPost,
		joinURL(b.BaseURL, "projects/"+url.PathEscape(b.Target)+"/issues.json"),
		map[string]string{"X-Redmine-API-Key": d.cred.RedmineToken}, body)
	if err != nil {
		return workitem.ProcessTicket{}, err
	}
	if status != http.StatusCreated && status != http.StatusOK {
		return workitem.ProcessTicket{}, errRef("Redmine 建票失败（HTTP %d）：%s", status, snippet(raw))
	}
	var out struct {
		Issue struct {
			ID int64 `json:"id"`
		} `json:"issue"`
	}
	if err := json.Unmarshal(raw, &out); err != nil || out.Issue.ID == 0 {
		return workitem.ProcessTicket{}, errRef("Redmine 建票响应无法解析：%s", snippet(raw))
	}
	id := fmt.Sprintf("%d", out.Issue.ID)
	return workitem.ProcessTicket{ExternalID: id, URL: joinURL(b.BaseURL, "issues/"+id)}, nil
}

// CommentTicket 向票追加一条回复（节点动作留痕）。
func (d *Dispatcher) CommentTicket(ctx context.Context, b workitem.ProviderBinding,
	ticket workitem.ProcessTicket, note workitem.ProcessNote) error {
	if d.cred.RedmineToken == "" {
		return errRef("Redmine 凭据未配置（CHALKLINE_REDMINE_TOKEN）")
	}
	body := map[string]any{"issue": map[string]any{"notes": note.Body}}
	status, raw, err := d.send(ctx, http.MethodPut,
		joinURL(b.BaseURL, "issues/"+url.PathEscape(ticket.ExternalID)+".json"),
		map[string]string{"X-Redmine-API-Key": d.cred.RedmineToken}, body)
	if err != nil {
		return err
	}
	if status != http.StatusOK && status != http.StatusCreated && status != http.StatusNoContent {
		return errRef("Redmine 追加回复失败（HTTP %d）：%s", status, snippet(raw))
	}
	return nil
}

// ---- 公共 ----

// send 序列化 JSON（body 为 []byte 时原样发送）并返回状态与响应体。
func (d *Dispatcher) send(ctx context.Context, method, target string, header map[string]string,
	body any) (int, []byte, error) {
	var payload []byte
	if body != nil {
		if raw, ok := body.([]byte); ok {
			payload = raw
		} else {
			encoded, err := json.Marshal(body)
			if err != nil {
				return 0, nil, err
			}
			payload = encoded
		}
	}
	resp, err := d.client.do(ctx, method, target, header, payload)
	if err != nil {
		return 0, nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return resp.StatusCode, nil, err
	}
	return resp.StatusCode, raw, nil
}

// pushContent 是写入外部系统的正文：inline 用原文，外部引用用摘要 + 链接。
func pushContent(art workitem.ArtifactRef, meta workitem.ArtifactMeta) string {
	if strings.TrimSpace(art.Content) != "" {
		return art.Content
	}
	var b strings.Builder
	b.WriteString("> 由 Chalkline 分发的外部成果物引用\n\n")
	if art.URL != "" {
		b.WriteString("- 链接：" + art.URL + "\n")
	}
	if art.ExternalID != "" {
		b.WriteString("- 外部标识：" + art.ExternalID + "\n")
	}
	if meta.Title != "" {
		b.WriteString("- 标题：" + meta.Title + "\n")
	}
	if meta.Version != "" {
		b.WriteString("- 版本：" + meta.Version + "\n")
	}
	if meta.Status != "" {
		b.WriteString("- 状态：" + meta.Status + "\n")
	}
	return b.String()
}

// artifactFilePath 拼出 <目录>/<类型>.md；目录为空时退化为 <类型>.md。
func artifactFilePath(dir string, kind workitem.ArtifactKind) string {
	name := string(kind) + ".md"
	if strings.TrimSpace(dir) == "" {
		return name
	}
	return path.Join(strings.Trim(dir, "/"), name)
}

func joinURL(base, p string) string {
	base = strings.TrimRight(base, "/")
	p = strings.TrimLeft(p, "/")
	if p == "" {
		return base
	}
	return base + "/" + p
}

func withContentType(header map[string]string, ct string) map[string]string {
	out := make(map[string]string, len(header)+1)
	for k, v := range header {
		out[k] = v
	}
	out["Content-Type"] = ct
	return out
}

// snippet 截断响应体用于错误展示（避免把大响应或敏感内容写进日志）。
func snippet(raw []byte) string {
	s := strings.TrimSpace(string(raw))
	if len(s) > 200 {
		return s[:200] + "…"
	}
	return s
}
