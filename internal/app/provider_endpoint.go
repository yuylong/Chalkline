package app

// provider_endpoint.go：全局 Provider 连接目录的用例（docs/domain/integrations.md「连接目录」）。
// admin 增删改查多套外部系统地址（多个 git / svn / redmine / outline）；成员可读已启用
// 目录用于迭代绑定预填。目录只存地址与展示名，不存任何凭据（红线 8）。

import (
	"context"
	"strings"
	"time"

	"github.com/yuylong/Chalkline/internal/store"
)

// ProviderEndpointView 是连接目录视图（admin 管理台）。
type ProviderEndpointView struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	KindLabel string `json:"kindLabel"`
	Label     string `json:"label"`
	BaseURL   string `json:"baseUrl"`
	Note      string `json:"note,omitempty"`
	Enabled   bool   `json:"enabled"`
	UpdatedBy string `json:"updatedBy,omitempty"`
	UpdatedAt string `json:"updatedAt,omitempty"`
}

// ProviderEndpointInput 是目录新增入参。
type ProviderEndpointInput struct {
	Kind    string
	Label   string
	BaseURL string
	Note    string
	Enabled bool
}

// ProviderEndpointUpdateInput 是目录更新入参（nil 即不改）。
type ProviderEndpointUpdateInput struct {
	Label   *string
	BaseURL *string
	Note    *string
	Enabled *bool
}

// providerKindLabels 是平台认识的外部系统类型与展示名（与 workitem.ArtifactProvider
// 对齐，不含 inline——inline 是平台内置存储，不是外部服务器）。
var providerKindLabels = map[string]string{
	"git":     "Git / GitLab / Gitea",
	"svn":     "SVN",
	"outline": "Outline",
	"redmine": "Redmine",
}

// knownProviderKind 报告 kind 是否为已知外部系统类型。
func knownProviderKind(kind string) bool {
	_, ok := providerKindLabels[kind]
	return ok
}

// providerKindLabel 取类型展示名（找不到时退化为 kind 本身）。
func (s *Service) providerKindLabel(kind string) string {
	if label, ok := providerKindLabels[kind]; ok {
		return label
	}
	return kind
}

// ListProviderEndpoints 列连接目录：admin 看全部（含停用），成员只看已启用。
func (s *Service) ListProviderEndpoints(ctx context.Context, actor Actor) ([]ProviderEndpointView, error) {
	var rows []store.ProviderEndpointRow
	var err error
	if actor.IsAdmin {
		rows, err = store.ListProviderEndpoints(ctx, s.DB.Q())
	} else {
		rows, err = store.ListEnabledProviderEndpoints(ctx, s.DB.Q())
	}
	if err != nil {
		return nil, err
	}
	out := make([]ProviderEndpointView, 0, len(rows))
	for _, r := range rows {
		out = append(out, s.providerEndpointView(r))
	}
	return out, nil
}

func (s *Service) providerEndpointView(r store.ProviderEndpointRow) ProviderEndpointView {
	v := ProviderEndpointView{
		ID: r.ID, Kind: r.Kind, KindLabel: s.providerKindLabel(r.Kind),
		Label: r.Label, BaseURL: r.BaseURL, Note: r.Note, Enabled: r.Enabled,
	}
	if !r.UpdatedAt.IsZero() {
		v.UpdatedAt = r.UpdatedAt.UTC().Format(time.RFC3339)
	}
	return v
}

// validateProviderEndpoint 校验目录入参：kind 必须是已知 provider，
// baseURL 必须为 http(s) 绝对地址且不得内嵌凭据。
func (s *Service) validateProviderEndpoint(kind, baseURL string) error {
	if !knownProviderKind(kind) {
		return Invalid("未知 provider %q（可选：git / svn / outline / redmine）", kind)
	}
	v, err := normalizeBaseURL(baseURL)
	if err != nil {
		return err
	}
	if v == "" {
		return Invalid("连接地址不可为空")
	}
	return nil
}

// CreateProviderEndpoint 新增目录行（admin，写审计）。
func (s *Service) CreateProviderEndpoint(ctx context.Context, actor Actor,
	in ProviderEndpointInput) (ProviderEndpointView, error) {
	if !actor.IsAdmin {
		return ProviderEndpointView{}, RoleRequired("连接目录管理需要 admin 角色")
	}
	kind := strings.TrimSpace(in.Kind)
	label := strings.TrimSpace(in.Label)
	if label == "" {
		return ProviderEndpointView{}, Invalid("展示名不可为空")
	}
	baseURL, err := normalizeBaseURL(in.BaseURL)
	if err != nil {
		return ProviderEndpointView{}, err
	}
	if err := s.validateProviderEndpoint(kind, baseURL); err != nil {
		return ProviderEndpointView{}, err
	}
	row := store.ProviderEndpointRow{
		ID: s.newID("pep"), Kind: kind, Label: label, BaseURL: baseURL,
		Note: strings.TrimSpace(in.Note), Enabled: true, CreatedBy: actor.UserID,
	}
	err = s.DB.WithTx(ctx, func(q store.Querier) error {
		if err := store.InsertProviderEndpoint(ctx, q, row); err != nil {
			if store.IsDuplicate(err) {
				return Invalid("该 provider 已存在同地址的目录项")
			}
			return err
		}
		return audit(ctx, q, actor, "integration.endpoint.create", "provider_endpoint", row.ID,
			map[string]any{"kind": kind, "label": label, "baseUrl": baseURL})
	})
	if err != nil {
		return ProviderEndpointView{}, err
	}
	return s.providerEndpointView(row), nil
}

// UpdateProviderEndpoint 改目录行（admin，写审计）。
func (s *Service) UpdateProviderEndpoint(ctx context.Context, actor Actor, id string,
	in ProviderEndpointUpdateInput) (ProviderEndpointView, error) {
	if !actor.IsAdmin {
		return ProviderEndpointView{}, RoleRequired("连接目录管理需要 admin 角色")
	}
	if in.Label == nil && in.BaseURL == nil && in.Note == nil && in.Enabled == nil {
		return ProviderEndpointView{}, Invalid("至少提供一个待更新字段")
	}
	if in.Label != nil && strings.TrimSpace(*in.Label) == "" {
		return ProviderEndpointView{}, Invalid("展示名不可为空")
	}
	if in.BaseURL != nil {
		v, err := normalizeBaseURL(*in.BaseURL)
		if err != nil {
			return ProviderEndpointView{}, err
		}
		if v == "" {
			return ProviderEndpointView{}, Invalid("连接地址不可为空")
		}
		in.BaseURL = &v
	}
	var out ProviderEndpointView
	err := s.DB.WithTx(ctx, func(q store.Querier) error {
		if err := store.UpdateProviderEndpoint(ctx, q, id, store.ProviderEndpointUpdate{
			Label: in.Label, BaseURL: in.BaseURL, Note: in.Note, Enabled: in.Enabled,
		}); err != nil {
			return wrapNotFound(err, "目录项不存在")
		}
		if err := audit(ctx, q, actor, "integration.endpoint.update", "provider_endpoint", id,
			map[string]any{"label": in.Label, "baseUrl": in.BaseURL, "enabled": in.Enabled}); err != nil {
			return err
		}
		row, err := store.ProviderEndpointByID(ctx, q, id)
		if err != nil {
			return err
		}
		out = s.providerEndpointView(row)
		return nil
	})
	return out, err
}

// DeleteProviderEndpoint 删目录行（admin，写审计）。
func (s *Service) DeleteProviderEndpoint(ctx context.Context, actor Actor, id string) error {
	if !actor.IsAdmin {
		return RoleRequired("连接目录管理需要 admin 角色")
	}
	return s.DB.WithTx(ctx, func(q store.Querier) error {
		if err := store.DeleteProviderEndpoint(ctx, q, id); err != nil {
			return wrapNotFound(err, "目录项不存在")
		}
		return audit(ctx, q, actor, "integration.endpoint.delete", "provider_endpoint", id, nil)
	})
}
