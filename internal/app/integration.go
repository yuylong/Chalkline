package app

// integration.go：集成 provider 的可视化配置用例（docs/modules/integration.md M3）。
// 只管理开关与 baseURL——凭据仍只从环境变量读取、不落库（红线 8）；
// 配置变更由 integration.Registry 在运行时解析（server 组装层注入 ConfigResolver）。

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"time"

	"github.com/yuylong/Chalkline/internal/store"
)

// ProviderDefault 是环境变量提供的 provider 默认配置。
// TokenConfigured 只表示凭据是否已配置，绝不返回凭据本身。
type ProviderDefault struct {
	Kind            string
	Label           string
	BaseURL         string
	TokenRequired   bool
	TokenConfigured bool
}

// ProviderConfigView 是管理台看到的 provider 配置（合并环境变量默认值与库内覆盖）。
type ProviderConfigView struct {
	Kind            string `json:"kind"`
	Label           string `json:"label"`
	Enabled         bool   `json:"enabled"`
	BaseURL         string `json:"baseUrl"`    // 生效值（库内覆盖优先）
	EnvBaseURL      string `json:"envBaseUrl"` // 环境变量默认值（只读提示）
	TokenRequired   bool   `json:"tokenRequired"`
	TokenConfigured bool   `json:"tokenConfigured"`
	UpdatedBy       string `json:"updatedBy,omitempty"`
	UpdatedAt       string `json:"updatedAt,omitempty"`
}

// ProviderConfigInput 是配置更新入参（nil 即不改）。
type ProviderConfigInput struct {
	Enabled *bool
	BaseURL *string
}

// ProviderOptionView 是供成员选型的全局连接默认值（只含地址，不含任何凭据）。
type ProviderOptionView struct {
	Kind    string `json:"kind"`
	Label   string `json:"label"`
	BaseURL string `json:"baseUrl"`
	Enabled bool   `json:"enabled"`
}

// ListProviderOptions 列出全局连接默认值，供迭代创建/配置时预填 baseURL。
// 任意登录成员可读：只暴露已启用的 provider 与地址，凭据是否配置等信息仍仅 admin 可见。
func (s *Service) ListProviderOptions(ctx context.Context, _ Actor) ([]ProviderOptionView, error) {
	rows, err := store.ListProviderConfigs(ctx, s.DB.Q())
	if err != nil {
		return nil, err
	}
	merged := mergeProviderConfigs(s.ProviderDefaults, rows)
	out := make([]ProviderOptionView, 0, len(merged))
	for _, v := range merged {
		if !v.Enabled {
			continue
		}
		out = append(out, ProviderOptionView{
			Kind: v.Kind, Label: v.Label, BaseURL: v.BaseURL, Enabled: v.Enabled,
		})
	}
	return out, nil
}

// ListProviderConfigs 列出全部 provider 配置（admin）。
func (s *Service) ListProviderConfigs(ctx context.Context, actor Actor) ([]ProviderConfigView, error) {
	if !actor.IsAdmin {
		return nil, RoleRequired("需要 admin 管理集成配置")
	}
	rows, err := store.ListProviderConfigs(ctx, s.DB.Q())
	if err != nil {
		return nil, err
	}
	return mergeProviderConfigs(s.ProviderDefaults, rows), nil
}

// UpdateProviderConfig 保存 provider 开关与 baseURL（admin，写审计）。
func (s *Service) UpdateProviderConfig(ctx context.Context, actor Actor, kind string,
	in ProviderConfigInput) (ProviderConfigView, error) {
	if !actor.IsAdmin {
		return ProviderConfigView{}, RoleRequired("需要 admin 管理集成配置")
	}
	def, ok := s.providerDefault(kind)
	if !ok {
		return ProviderConfigView{}, NotFound("provider %q 不存在", kind)
	}
	if in.Enabled == nil && in.BaseURL == nil {
		return ProviderConfigView{}, Invalid("enabled 与 baseUrl 至少提供一个")
	}

	var (
		enabled   = true
		baseURL   string
		updatedAt time.Time
	)
	err := s.DB.WithTx(ctx, func(q store.Querier) error {
		if cur, err := store.ProviderConfigByKind(ctx, q, kind); err == nil {
			enabled, baseURL = cur.Enabled, cur.BaseURL
		} else if !errors.Is(err, store.ErrNotFound) {
			return err
		}
		if in.Enabled != nil {
			enabled = *in.Enabled
		}
		if in.BaseURL != nil {
			v, err := normalizeBaseURL(*in.BaseURL)
			if err != nil {
				return err
			}
			baseURL = v
		}
		if err := store.UpsertProviderConfig(ctx, q, kind, enabled, baseURL, actor.UserID); err != nil {
			return err
		}
		if err := audit(ctx, q, actor, "integration.provider.update", "provider", kind,
			map[string]any{"enabled": enabled, "baseUrl": baseURL}); err != nil {
			return err
		}
		updatedAt = s.Now().UTC()
		return nil
	})
	if err != nil {
		return ProviderConfigView{}, err
	}
	return providerConfigView(def, store.ProviderConfigRow{
		Provider: kind, Enabled: enabled, BaseURL: baseURL,
		UpdatedBy: actor.UserID, UpdatedAt: updatedAt,
	}, true), nil
}

// providerDefault 按 kind 找环境变量默认配置。
func (s *Service) providerDefault(kind string) (ProviderDefault, bool) {
	for _, d := range s.ProviderDefaults {
		if d.Kind == kind {
			return d, true
		}
	}
	return ProviderDefault{}, false
}

// mergeProviderConfigs 把库内覆盖合并到环境变量默认值上。
func mergeProviderConfigs(defs []ProviderDefault, rows []store.ProviderConfigRow) []ProviderConfigView {
	byKind := make(map[string]store.ProviderConfigRow, len(rows))
	for _, r := range rows {
		byKind[r.Provider] = r
	}
	out := make([]ProviderConfigView, 0, len(defs))
	for _, d := range defs {
		if r, ok := byKind[d.Kind]; ok {
			out = append(out, providerConfigView(d, r, true))
		} else {
			out = append(out, providerConfigView(d, store.ProviderConfigRow{}, false))
		}
	}
	return out
}

// providerConfigView 合并单条：库内 baseURL 非空时覆盖环境变量值。
func providerConfigView(def ProviderDefault, row store.ProviderConfigRow, stored bool) ProviderConfigView {
	v := ProviderConfigView{
		Kind:            def.Kind,
		Label:           def.Label,
		Enabled:         true,
		BaseURL:         def.BaseURL,
		EnvBaseURL:      def.BaseURL,
		TokenRequired:   def.TokenRequired,
		TokenConfigured: def.TokenConfigured,
	}
	if stored {
		v.Enabled = row.Enabled
		if row.BaseURL != "" {
			v.BaseURL = row.BaseURL
		}
		v.UpdatedBy = row.UpdatedBy
		if !row.UpdatedAt.IsZero() {
			v.UpdatedAt = row.UpdatedAt.UTC().Format(time.RFC3339)
		}
	}
	return v
}

// normalizeBaseURL 校验收紧后的 baseURL：空串表示沿用环境变量；
// 非空必须是 http(s) 绝对地址，且不得内嵌凭据（凭据只走环境变量）。
func normalizeBaseURL(raw string) (string, error) {
	v := strings.TrimSpace(raw)
	if v == "" {
		return "", nil
	}
	u, err := url.Parse(v)
	if err != nil || u.Host == "" {
		return "", Invalid("baseUrl 不是合法 URL")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", Invalid("baseUrl 必须使用 http/https")
	}
	if u.User != nil {
		return "", Invalid("baseUrl 不得内嵌凭据（token 只经环境变量配置）")
	}
	return strings.TrimRight(v, "/"), nil
}
