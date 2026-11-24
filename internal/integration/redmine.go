package integration

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/yuylong/Chalkline/internal/domain/workitem"
)

// RedmineProvider 对接 Redmine：单向出站（只取 issue 标题快照供展示），
// 不反向回写状态——流程状态的唯一事实来源是 Chalkline（integrations.md）。
type RedmineProvider struct {
	client  *safeClient
	baseURL string // 如 https://redmine.example.com
	token   string // CHALKLINE_REDMINE_TOKEN（API key）
}

func NewRedmineProvider(c *safeClient, baseURL, token string) *RedmineProvider {
	return &RedmineProvider{client: c, baseURL: strings.TrimRight(baseURL, "/"), token: token}
}

func (p *RedmineProvider) Kind() workitem.ArtifactProvider { return workitem.ProviderRedmine }

// WithBaseURL 返回覆盖 baseURL 的副本（管理台配置热应用，不修改共享实例）。
func (p *RedmineProvider) WithBaseURL(baseURL string) Provider {
	cp := *p
	cp.baseURL = strings.TrimRight(baseURL, "/")
	return &cp
}

// Validate：URL 合法（domain 已保证）+ 私网地址拦截；不出站、不要求 Redmine 实时可访问。
func (p *RedmineProvider) Validate(_ context.Context, ref workitem.ArtifactRef) error {
	if ref.URL == "" {
		return errRef("redmine 材料必须携带 URL")
	}
	if hostIsPrivate(ref.URL) {
		return errRef("redmine 材料 URL 指向内网/保留地址，拒绝挂接")
	}
	return nil
}

// Snapshot 尽力调 /issues/{id}.json 取标题；失败降级为引用快照。
func (p *RedmineProvider) Snapshot(ctx context.Context, ref workitem.ArtifactRef) (workitem.ArtifactMeta, error) {
	fallback := passthroughMeta(ref)
	if p.baseURL == "" || p.token == "" {
		return fallback, nil
	}
	id := redmineIssueID(ref)
	if id == "" {
		return fallback, nil
	}
	target := p.baseURL + "/issues/" + id + ".json"
	resp, err := p.client.doGet(ctx, target, map[string]string{"X-Redmine-API-Key": p.token})
	if err != nil {
		return fallback, nil
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil || resp.StatusCode != http.StatusOK {
		return fallback, nil
	}
	var out struct {
		Issue struct {
			Subject string `json:"subject"`
			Status  struct {
				Name string `json:"name"`
			} `json:"status"`
		} `json:"issue"`
	}
	if err := json.Unmarshal(body, &out); err != nil || out.Issue.Subject == "" {
		return fallback, nil
	}
	return workitem.ArtifactMeta{Title: out.Issue.Subject, Status: out.Issue.Status.Name}, nil
}

// redmineIssueID 取 issue 标识：优先 externalId，其次 URL 尾段数字。
func redmineIssueID(ref workitem.ArtifactRef) string {
	if ref.ExternalID != "" {
		return strings.TrimPrefix(ref.ExternalID, "#")
	}
	u, err := url.Parse(ref.URL)
	if err != nil {
		return ""
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) == 0 {
		return ""
	}
	tail := parts[len(parts)-1]
	tail = strings.TrimSuffix(tail, ".json")
	if _, err := strconv.Atoi(tail); err != nil {
		return ""
	}
	return tail
}
