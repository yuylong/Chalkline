package integration

import (
	"context"

	"github.com/yuylong/Chalkline/internal/domain/workitem"
)

// GitProvider 校验 Git 平台（GitLab / Gitea / GitHub）的 pr 材料。
// Chalkline 只存链接不克隆仓库（integrations.md「同步方向」），不出站拉取；
// Validate 只做本地校验：URL 格式（domain 已保证）+ 私网地址拦截。
type GitProvider struct{ client *safeClient }

func NewGitProvider(c *safeClient) *GitProvider { return &GitProvider{client: c} }

func (g *GitProvider) Kind() workitem.ArtifactProvider { return workitem.ProviderGit }

func (g *GitProvider) Validate(_ context.Context, ref workitem.ArtifactRef) error {
	return validateReachableHost(g.client, ref, "pr")
}

// Snapshot 不外呼（各 git 平台 API 形态各异，且门禁不依赖外部状态）；
// 返回引用快照：标题缺省时用 URL 尾段，便于控制台与审计展示。
func (g *GitProvider) Snapshot(_ context.Context, ref workitem.ArtifactRef) (workitem.ArtifactMeta, error) {
	return passthroughMeta(ref), nil
}

// SVNProvider 与 Git 同语义：只存链接（svn:// 私有协议地址不在支持范围，
// 材料引用统一走 http(s) 仓库浏览地址）。
type SVNProvider struct{ client *safeClient }

func NewSVNProvider(c *safeClient) *SVNProvider { return &SVNProvider{client: c} }

func (s *SVNProvider) Kind() workitem.ArtifactProvider { return workitem.ProviderSVN }

func (s *SVNProvider) Validate(_ context.Context, ref workitem.ArtifactRef) error {
	return validateReachableHost(s.client, ref, "pr")
}

func (s *SVNProvider) Snapshot(_ context.Context, ref workitem.ArtifactRef) (workitem.ArtifactMeta, error) {
	return passthroughMeta(ref), nil
}

// validateReachableHost 供 git/svn：URL 已由 domain.ValidateRef 保证绝对 http(s)，
// 本层补充私网/回环地址拦截（SSRF 底线：PR 材料指向云元数据等内网端点一律拒绝挂接）。
// 白名单只约束出站（Snapshot），Validate 本身不出站——未配置集成的部署不拒绝合法链接。
func validateReachableHost(_ *safeClient, ref workitem.ArtifactRef, kindLabel string) error {
	if ref.URL == "" {
		return errRef("%s 材料必须携带 URL", kindLabel)
	}
	if hostIsPrivate(ref.URL) {
		return errRef("%s 材料 URL 指向内网/保留地址，拒绝挂接", kindLabel)
	}
	return nil
}
