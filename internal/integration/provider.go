// Package integration 是外部系统适配层：只做协议转换与引用校验，
// 不写领域状态、不做双向同步（docs/modules/integration.md）。
// domain / api / mcp 不得直连外部系统（tech.md 硬性约束 9）——本包是被 app 层
// 经依赖倒置调用的唯一出站通道。
package integration

import (
	"context"
	"fmt"

	"github.com/yuylong/Chalkline/internal/domain/workitem"
)

// Provider 是外部系统适配器契约（docs/domain/integrations.md「适配器接口」）。
//
// Validate 只校验引用存在与格式合法，不要求外部系统实时可访问——
// 外围系统挂了不该阻断评审（不变量 15 的前置语义）。
// Snapshot 尽力取标题/版本/状态快照，仅用于展示与审计，失败不阻断挂接。
type Provider interface {
	Kind() workitem.ArtifactProvider
	Validate(ctx context.Context, ref workitem.ArtifactRef) error
	Snapshot(ctx context.Context, ref workitem.ArtifactRef) (workitem.ArtifactMeta, error)
}

// ProviderConfig 是运行时可变的 provider 配置（管理台维护，存库但不含凭据）。
type ProviderConfig struct {
	// Enabled 为 false 时跳过外部快照（材料引用仍有效，只做本地格式校验）。
	Enabled bool
	// BaseURL 覆盖构造时的环境变量值；空表示沿用环境变量。
	BaseURL string
}

// ConfigResolver 按 provider 解析当前配置（found=false 表示未配置，按默认启用处理）。
// 实现由 server 组装层注入，读取 store 而不让本包依赖存储层。
type ConfigResolver func(ctx context.Context, kind workitem.ArtifactProvider) (cfg ProviderConfig, found bool)

// baseURLSetter 让 provider 以不可变副本方式覆盖 baseURL（避免并发写共享实例）。
type baseURLSetter interface {
	WithBaseURL(baseURL string) Provider
}

// Registry 按 provider 路由到适配器；未注册的 provider 走 passthrough
// （file / url / inline 无外部系统，格式校验已由 domain.ValidateRef 完成）。
type Registry struct {
	providers map[workitem.ArtifactProvider]Provider
	// config 为空时恒按启用处理（单测与未接入管理台的部署）。
	config ConfigResolver
}

// NewRegistry 注册全部适配器；重复 Kind 以最后注册为准。
func NewRegistry(ps ...Provider) *Registry {
	r := &Registry{providers: make(map[workitem.ArtifactProvider]Provider, len(ps))}
	for _, p := range ps {
		r.providers[p.Kind()] = p
	}
	return r
}

// WithConfig 注入运行时配置解析器（server 组装层调用）。
func (r *Registry) WithConfig(resolve ConfigResolver) *Registry {
	r.config = resolve
	return r
}

// Get 取适配器；ok=false 表示该 provider 无外部行为。
func (r *Registry) Get(p workitem.ArtifactProvider) (Provider, bool) {
	pro, ok := r.providers[p]
	return pro, ok
}

// lookup 解析 provider 当前配置；未接入配置源时返回启用 + 无覆盖。
func (r *Registry) lookup(ctx context.Context, p workitem.ArtifactProvider) ProviderConfig {
	if r.config == nil {
		return ProviderConfig{Enabled: true}
	}
	if cfg, ok := r.config(ctx, p); ok {
		return cfg
	}
	return ProviderConfig{Enabled: true}
}

// Validate 校验引用；实现方不得出站到任意目标（出站须过 safeClient 白名单）。
// 停用只跳过外部快照——本地格式与私网/回环校验属于安全底线，始终执行。
func (r *Registry) Validate(ctx context.Context, ref workitem.ArtifactRef) error {
	p, ok := r.Get(ref.Provider)
	if !ok {
		return nil
	}
	return p.Validate(ctx, ref)
}

// Snapshot 尽力取快照；未注册 provider、停用或失败均降级为引用自身信息（不报错）。
func (r *Registry) Snapshot(ctx context.Context, ref workitem.ArtifactRef) (workitem.ArtifactMeta, error) {
	p, ok := r.Get(ref.Provider)
	if !ok {
		return passthroughMeta(ref), nil
	}
	cfg := r.lookup(ctx, ref.Provider)
	if !cfg.Enabled {
		return passthroughMeta(ref), nil
	}
	if cfg.BaseURL != "" {
		if setter, ok := p.(baseURLSetter); ok {
			p = setter.WithBaseURL(cfg.BaseURL)
		}
	}
	return p.Snapshot(ctx, ref)
}

// passthroughMeta 无外部系统时的快照：引用自带标题或 URL 尾段。
func passthroughMeta(ref workitem.ArtifactRef) workitem.ArtifactMeta {
	if ref.Title != "" {
		return workitem.ArtifactMeta{Title: ref.Title}
	}
	if ref.URL == "" {
		return workitem.ArtifactMeta{}
	}
	// inline / file 材料无 URL：不猜标题。
	return workitem.ArtifactMeta{Title: tailOfURL(ref.URL)}
}

// tailOfURL 取 URL 路径最后一段作为兜底展示标题。
func tailOfURL(raw string) string {
	for i := len(raw) - 1; i >= 0; i-- {
		if raw[i] == '/' {
			if i == len(raw)-1 {
				return tailOfURL(raw[:i]) // 结尾斜杠：递归去尾
			}
			return raw[i+1:]
		}
	}
	return raw
}

// errRef 是适配器侧引用错误（app 层翻译为 INVALID_ARTIFACT_REF / 422）。
func errRef(format string, args ...any) error {
	return fmt.Errorf("integration: "+format, args...)
}
