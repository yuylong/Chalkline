package integration

import (
	"context"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// safeClient 是出站 HTTP 客户端：域名白名单 + 不跟随重定向 + 超时（防 SSRF，
// docs/modules/integration.md「出站安全」）。凭据只从环境变量注入，不经过本类型。
type safeClient struct {
	http *http.Client
	// allowlist 是允许出站的 host 后缀（如 git.example.com）；含 "*" 表示放行全部。
	// 空 = 默认拒绝一切出站（安全默认：未配置集成的部署不应有外呼行为）。
	allowlist []string
}

// NewSafeClient 构造出站客户端。allowlist 为空表示拒绝一切出站（安全默认）；
// 含 "*" 表示放行全部 host（仅限内网自签部署评估时使用）。
func NewSafeClient(allowlist []string) *safeClient {
	return &safeClient{
		http: &http.Client{
			Timeout: 10 * time.Second,
			// 禁止跟随任意重定向：30x 响应原样返回，由调用方决定（防 SSRF 跳转绕过白名单）。
			CheckRedirect: func(_ *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		allowlist: allowlist,
	}
}

// allowed 判断目标 host 是否在白名单内。
func (c *safeClient) allowed(target string) bool {
	u, err := url.Parse(target)
	if err != nil || u.Hostname() == "" {
		return false
	}
	for _, suffix := range c.allowlist {
		if suffix == "*" {
			return true
		}
		host := strings.ToLower(u.Hostname())
		if host == suffix || strings.HasSuffix(host, "."+suffix) {
			return true
		}
	}
	return false
}

// blockedHostError 供适配器在出站前统一报错。
func blockedHostError(target string) error {
	return errRef("目标 %s 不在出站白名单内（CHALKLINE_OUTBOUND_ALLOWLIST）", hostOf(target))
}

// do 在白名单校验后发起 GET；不允许的目标返回 errRef，不产生网络请求。
func (c *safeClient) doGet(ctx context.Context, target string, header map[string]string) (*http.Response, error) {
	if !c.allowed(target) {
		return nil, blockedHostError(target)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	for k, v := range header {
		req.Header.Set(k, v)
	}
	return c.http.Do(req)
}

func hostOf(target string) string {
	if u, err := url.Parse(target); err == nil {
		return u.Host
	}
	return target
}

// hostIsPrivate 判定内网/回环地址（额外防线：白名单放行也不外呼私有网段，
// 防止配置失误导致出站打到元数据服务等内网端点）。
func hostIsPrivate(target string) bool {
	u, err := url.Parse(target)
	if err != nil {
		return true // 解析失败按不可出站处理
	}
	host := u.Hostname()
	if host == "localhost" || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".local") {
		return true
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false // 域名：交给白名单
	}
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsUnspecified()
}
