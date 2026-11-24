package integration

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/yuylong/Chalkline/internal/domain/workitem"
)

// OutlineProvider 对接 Outline 知识库（docs/domain/integrations.md：出站链接，
// 不拉正文，只取标题快照）。baseURL 与 token 只从环境变量读取，不落库。
type OutlineProvider struct {
	client  *safeClient
	baseURL string // 如 https://outline.example.com
	token   string // CHALKLINE_OUTLINE_TOKEN
}

func NewOutlineProvider(c *safeClient, baseURL, token string) *OutlineProvider {
	return &OutlineProvider{client: c, baseURL: strings.TrimRight(baseURL, "/"), token: token}
}

func (o *OutlineProvider) Kind() workitem.ArtifactProvider { return workitem.ProviderOutline }

// WithBaseURL 返回覆盖 baseURL 的副本（管理台配置热应用，不修改共享实例）。
func (o *OutlineProvider) WithBaseURL(baseURL string) Provider {
	cp := *o
	cp.baseURL = strings.TrimRight(baseURL, "/")
	return &cp
}

// Validate：URL 合法（domain 已保证）+ 私网地址拦截；不出站、不要求 Outline 实时可访问。
func (o *OutlineProvider) Validate(_ context.Context, ref workitem.ArtifactRef) error {
	if ref.URL == "" {
		return errRef("outline 材料必须携带 URL")
	}
	if hostIsPrivate(ref.URL) {
		return errRef("outline 材料 URL 指向内网/保留地址，拒绝挂接")
	}
	return nil
}

// Snapshot 尽力调 documents.info 取标题；未配置 baseURL/token、白名单不匹配、
// 网络失败均降级为引用快照（不阻断挂接，不变量 15）。
func (o *OutlineProvider) Snapshot(ctx context.Context, ref workitem.ArtifactRef) (workitem.ArtifactMeta, error) {
	fallback := passthroughMeta(ref)
	if o.baseURL == "" || o.token == "" {
		return fallback, nil
	}
	docID := outlineDocID(ref)
	if docID == "" {
		return fallback, nil
	}
	target := o.baseURL + "/api/documents.info?" + url.Values{
		"token": {o.token}, "id": {docID},
	}.Encode()
	resp, err := o.client.doGet(ctx, target, nil)
	if err != nil {
		return fallback, nil // 尽力而为：外部不可达不阻断
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil || resp.StatusCode != http.StatusOK {
		return fallback, nil
	}
	var out struct {
		Data struct {
			Title  string `json:"title"`
			URL    string `json:"url"`
			Update struct {
				CreatedAt string `json:"createdAt"`
			} `json:"update"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &out); err != nil || out.Data.Title == "" {
		return fallback, nil
	}
	return workitem.ArtifactMeta{Title: out.Data.Title, Version: docID}, nil
}

// outlineDocID 取文档标识：优先 externalId，其次 URL 路径中的 /doc/<uuid>。
func outlineDocID(ref workitem.ArtifactRef) string {
	if ref.ExternalID != "" {
		return ref.ExternalID
	}
	u, err := url.Parse(ref.URL)
	if err != nil {
		return ""
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	for i, p := range parts {
		if p == "doc" && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return ""
}
