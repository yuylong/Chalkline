package app

// iteration_provider.go：迭代级 provider 绑定的用例（docs/domain/integrations.md「迭代 Provider」）。
//
// 三类绑定：
//   - 成果物（artifact）：必选且唯一，inline 表示直接存平台内，外部则只回填摘要与链接；
//   - 流程记录（process）：可选，如 Redmine，为节点/任务建票并追加回复；
//   - 代码库（code）：可选且可多个，供成果物关联代码引用。
//
// 冻结规则：迭代创建/初始化阶段可改；一旦该迭代（含其下任务）提交过成果物即冻结（不变量 22）。

import (
	"context"
	"errors"
	"strings"

	"github.com/yuylong/Chalkline/internal/domain/workitem"
	"github.com/yuylong/Chalkline/internal/platform"
	"github.com/yuylong/Chalkline/internal/store"
)

// IterationProviderView 是绑定视图（含可点击的连接地址；凭据只报是否已配置）。
type IterationProviderView struct {
	ID            string `json:"id"`
	Category      string `json:"category"`
	CategoryLabel string `json:"categoryLabel"`
	Provider      string `json:"provider"`
	BaseURL       string `json:"baseUrl,omitempty"`
	Target        string `json:"target,omitempty"`
	DirTemplate   string `json:"dirTemplate,omitempty"`
	Branch        string `json:"branch,omitempty"`
	LinkURL       string `json:"linkUrl,omitempty"`
	// AuthType：''（未配置）/ token / basic。
	AuthType     string `json:"authType,omitempty"`
	AuthUsername string `json:"authUsername,omitempty"`
	// SecretConfigured 表示迭代级已保存凭据（密文）；EnvSecretConfigured 表示服务端环境变量已配置。
	SecretConfigured    bool `json:"secretConfigured"`
	EnvSecretConfigured bool `json:"envSecretConfigured"`
}

// IterationProviderInput 是绑定入参。
type IterationProviderInput struct {
	Category     string `json:"category"`
	Provider     string `json:"provider"`
	BaseURL      string `json:"baseUrl"`
	Target       string `json:"target"`
	DirTemplate  string `json:"dirTemplate"`
	Branch       string `json:"branch"`
	AuthType     string `json:"authType"`
	AuthUsername string `json:"authUsername"`
	// AuthSecret 只写不读：留空 = 保持库内已有凭据。
	AuthSecret string `json:"authSecret"`
}

// ProviderCredentialInput 是凭据轮换入参（只写不读）。
type ProviderCredentialInput struct {
	AuthType     string
	AuthUsername string
	AuthSecret   string
}

// IterationProvidersView 是整套绑定与冻结状态。
type IterationProvidersView struct {
	IterationID  string                  `json:"iterationId"`
	Items        []IterationProviderView `json:"items"`
	Locked       bool                    `json:"locked"`
	Editable     bool                    `json:"editable"`
	LockedReason string                  `json:"lockedReason,omitempty"`
}

// GetIterationProviders 读取迭代绑定（成员或 admin）。
func (s *Service) GetIterationProviders(ctx context.Context, actor Actor, iterationID string) (IterationProvidersView, error) {
	q := s.DB.Q()
	it, err := store.IterationByID(ctx, q, iterationID)
	if err != nil {
		return IterationProvidersView{}, wrapNotFound(err, "迭代不存在或不可见")
	}
	if _, err := s.mustView(ctx, q, actor, it.ProductID); err != nil {
		return IterationProvidersView{}, err
	}
	return s.iterationProvidersView(ctx, q, it)
}

// SetIterationProviders 整组替换迭代绑定（产品成员持 pm / 产品 owner / admin）。
func (s *Service) SetIterationProviders(ctx context.Context, actor Actor, iterationID string,
	inputs []IterationProviderInput) (IterationProvidersView, error) {
	var out IterationProvidersView
	err := s.DB.WithTx(ctx, func(q store.Querier) error {
		it, err := store.IterationByID(ctx, q, iterationID)
		if err != nil {
			return wrapNotFound(err, "迭代不存在或不可见")
		}
		mc, err := s.mustView(ctx, q, actor, it.ProductID)
		if err != nil {
			return err
		}
		if err := canCreateInProduct(mc, actor); err != nil {
			return err
		}
		if err := ensureProvidersMutable(ctx, q, it); err != nil {
			return err
		}
		bindings, err := normalizeBindings(inputs)
		if err != nil {
			return err
		}
		existing, err := store.ListIterationProviders(ctx, q, iterationID)
		if err != nil {
			return err
		}
		if err := ensureNewCredentials(bindings, existing); err != nil {
			return err
		}
		rows, err := s.providerRows(iterationID, actor.UserID, bindings)
		if err != nil {
			return err
		}
		if err := store.SyncIterationProviders(ctx, q, iterationID, rows); err != nil {
			return err
		}
		if err := audit(ctx, q, actor, "iteration.providers.update", "iteration", iterationID,
			map[string]any{"count": len(bindings)}); err != nil {
			return err
		}
		out, err = s.iterationProvidersView(ctx, q, it)
		return err
	})
	return out, err
}

// ensureProvidersMutable 校验绑定仍可修改：迭代未终结，且尚未提交过成果物。
func ensureProvidersMutable(ctx context.Context, q store.Querier, it store.IterationRow) error {
	if it.Status.Terminal() {
		return ProvidersLocked("迭代已处于终态，provider 绑定不可再修改")
	}
	submitted, err := store.IterationArtifactsSubmitted(ctx, q, it.ID)
	if err != nil {
		return err
	}
	if submitted {
		return ProvidersLocked("该迭代已提交成果物，provider 绑定已冻结（不变量 22）")
	}
	return nil
}

// iterationProvidersView 组装视图：绑定 + 冻结/可编辑状态。
func (s *Service) iterationProvidersView(ctx context.Context, q store.Querier,
	it store.IterationRow) (IterationProvidersView, error) {
	rows, err := store.ListIterationProviders(ctx, q, it.ID)
	if err != nil {
		return IterationProvidersView{}, err
	}
	submitted, err := store.IterationArtifactsSubmitted(ctx, q, it.ID)
	if err != nil {
		return IterationProvidersView{}, err
	}
	view := IterationProvidersView{
		IterationID: it.ID,
		Items:       s.providerViews(rows),
		Locked:      submitted || it.Status.Terminal(),
		Editable:    !submitted && !it.Status.Terminal(),
	}
	switch {
	case it.Status.Terminal():
		view.LockedReason = "迭代已处于终态"
	case submitted:
		view.LockedReason = "已提交成果物，绑定冻结"
	}
	return view, nil
}

// iterationBindings 取迭代绑定（供成果物分发与流程记录使用），并按需解密凭据。
// 解密失败（密钥变更/密文损坏）按「未配置」降级并告警，不阻断流程。
func (s *Service) iterationBindings(ctx context.Context, q store.Querier,
	iterationID string) ([]workitem.ProviderBinding, error) {
	rows, err := store.ListIterationProviders(ctx, q, iterationID)
	if err != nil {
		return nil, err
	}
	out := make([]workitem.ProviderBinding, 0, len(rows))
	for _, r := range rows {
		b := workitem.ProviderBinding{
			Category:    workitem.ProviderCategory(r.Category),
			Provider:    workitem.ArtifactProvider(r.Provider),
			BaseURL:     r.BaseURL,
			Target:      r.Target,
			DirTemplate: r.DirTemplate,
			Branch:      r.Branch,
			Auth: workitem.ProviderAuth{
				Type:     workitem.ProviderAuthType(r.AuthType),
				Username: r.AuthUsername,
			},
		}
		if r.AuthSecretEnc != "" {
			if plain, err := platform.Open(s.SecretKey, r.AuthSecretEnc); err == nil {
				b.Auth.Secret = plain
			} else {
				s.Log.Warn("迭代 provider 凭据解密失败，按未配置降级",
					"iteration", iterationID, "provider", r.Provider, "error", err)
				b.Auth = workitem.ProviderAuth{}
			}
		}
		out = append(out, b)
	}
	return out, nil
}

// iterationIDOfWorkItem 把任务/迭代回溯到所属迭代 id。
func (s *Service) iterationIDOfWorkItem(ctx context.Context, q store.Querier,
	wtype workitem.Scope, workItemID string) (string, error) {
	if wtype == workitem.ScopeIteration {
		return workItemID, nil
	}
	t, err := store.TaskByID(ctx, q, workItemID)
	if err != nil {
		return "", wrapNotFound(err, "任务不存在")
	}
	return t.IterationID, nil
}

// normalizeBindings 规范化入参并校验整套绑定（成果物必选且唯一）。
func normalizeBindings(inputs []IterationProviderInput) ([]workitem.ProviderBinding, error) {
	out := make([]workitem.ProviderBinding, 0, len(inputs))
	for _, in := range inputs {
		category := workitem.ProviderCategory(strings.TrimSpace(in.Category))
		b := workitem.ProviderBinding{
			Category:    category,
			Provider:    workitem.ArtifactProvider(strings.TrimSpace(in.Provider)),
			BaseURL:     strings.TrimRight(strings.TrimSpace(in.BaseURL), "/"),
			Target:      strings.TrimSpace(in.Target),
			DirTemplate: strings.TrimSpace(in.DirTemplate),
			Branch:      strings.TrimSpace(in.Branch),
			Auth: workitem.ProviderAuth{
				Type:     workitem.ProviderAuthType(strings.TrimSpace(in.AuthType)),
				Username: strings.TrimSpace(in.AuthUsername),
				Secret:   strings.TrimSpace(in.AuthSecret),
			},
		}
		if b.DirTemplate == "" && b.Provider != workitem.ProviderInline &&
			(category == workitem.CategoryArtifact || category == workitem.CategoryCode) &&
			(b.Provider == workitem.ProviderGit || b.Provider == workitem.ProviderSVN) {
			b.DirTemplate = workitem.DefaultArtifactDirTemplate
		}
		out = append(out, b)
	}
	if err := workitem.ValidateBindings(out); err != nil {
		return nil, Invalid("%s", err.Error())
	}
	return out, nil
}

// providerRows 把领域绑定转成存储行（连接信息随迭代固化，凭据加密后落库）。
// 未填写新 Secret 时 AuthSecretEnc 留空，由存储层保留库内已有密文。
func (s *Service) providerRows(iterationID, createdBy string,
	bs []workitem.ProviderBinding) ([]store.IterationProviderRow, error) {
	out := make([]store.IterationProviderRow, 0, len(bs))
	for _, b := range bs {
		enc := ""
		if b.Auth.Secret != "" {
			sealed, err := platform.Seal(s.SecretKey, b.Auth.Secret)
			if err != nil {
				return nil, invalidSecretKey(err)
			}
			enc = sealed
		}
		out = append(out, store.IterationProviderRow{
			ID:            s.newID("itp"),
			IterationID:   iterationID,
			Category:      string(b.Category),
			Provider:      string(b.Provider),
			BaseURL:       b.BaseURL,
			Target:        b.Target,
			DirTemplate:   b.DirTemplate,
			Branch:        b.Branch,
			AuthType:      string(b.Auth.Type),
			AuthUsername:  b.Auth.Username,
			AuthSecretEnc: enc,
			CreatedBy:     createdBy,
		})
	}
	return out, nil
}

// invalidSecretKey 把加密密钥缺失翻译为可展示错误（与 SMTP 密码保存一致）。
func invalidSecretKey(err error) error {
	if errors.Is(err, platform.ErrSecretKeyMissing) {
		return Invalid("未配置加密密钥（CHALKLINE_SECRET_KEY），无法保存 provider 凭据")
	}
	return err
}

// providerViews 把存储行转成视图（凭据只报是否已配置，永不回显）。
func (s *Service) providerViews(rows []store.IterationProviderRow) []IterationProviderView {
	out := make([]IterationProviderView, 0, len(rows))
	for _, r := range rows {
		b := workitem.ProviderBinding{
			Category:    workitem.ProviderCategory(r.Category),
			Provider:    workitem.ArtifactProvider(r.Provider),
			BaseURL:     r.BaseURL,
			Target:      r.Target,
			DirTemplate: r.DirTemplate,
			Branch:      r.Branch,
		}
		envConfigured := false
		if def, ok := s.providerDefault(r.Provider); ok {
			envConfigured = def.TokenConfigured
		}
		out = append(out, IterationProviderView{
			ID:                  r.ID,
			Category:            r.Category,
			CategoryLabel:       b.Category.Label(),
			Provider:            r.Provider,
			BaseURL:             r.BaseURL,
			Target:              r.Target,
			DirTemplate:         r.DirTemplate,
			Branch:              r.Branch,
			LinkURL:             b.LinkURL(),
			AuthType:            r.AuthType,
			AuthUsername:        r.AuthUsername,
			SecretConfigured:    r.AuthSecretEnc != "",
			EnvSecretConfigured: envConfigured,
		})
	}
	return out
}

// SetIterationProviderCredential 轮换迭代 provider 的写权限凭据。
//
// 与结构配置（SetIterationProviders）分开：即使绑定已冻结或迭代已终结，也允许更换 Key /
// 用户名密码——公司会定期轮换凭据。留空 Secret 表示沿用库内已有密文。
func (s *Service) SetIterationProviderCredential(ctx context.Context, actor Actor,
	iterationID, providerID string, in ProviderCredentialInput) (IterationProvidersView, error) {
	var out IterationProvidersView
	err := s.DB.WithTx(ctx, func(q store.Querier) error {
		it, err := store.IterationByID(ctx, q, iterationID)
		if err != nil {
			return wrapNotFound(err, "迭代不存在或不可见")
		}
		mc, err := s.mustView(ctx, q, actor, it.ProductID)
		if err != nil {
			return err
		}
		if err := canCreateInProduct(mc, actor); err != nil {
			return err
		}
		row, err := store.IterationProviderByID(ctx, q, iterationID, providerID)
		if err != nil {
			return wrapNotFound(err, "provider 绑定不存在")
		}
		if row.Provider == string(workitem.ProviderInline) {
			return Invalid("inline 成果物 provider 不需要认证信息")
		}
		auth := workitem.ProviderAuth{
			Type:     workitem.ProviderAuthType(strings.TrimSpace(in.AuthType)),
			Username: strings.TrimSpace(in.AuthUsername),
			Secret:   strings.TrimSpace(in.AuthSecret),
		}
		if err := auth.ValidateForRotation(); err != nil {
			return Invalid("%s", err.Error())
		}
		enc := ""
		if auth.Secret != "" {
			sealed, err := platform.Seal(s.SecretKey, auth.Secret)
			if err != nil {
				return invalidSecretKey(err)
			}
			enc = sealed
		}
		if err := store.UpdateIterationProviderCredential(ctx, q, providerID,
			string(auth.Type), auth.Username, enc); err != nil {
			return err
		}
		// 审计只记录方式与「是否更换了密钥」，绝不写密钥本身。
		if err := audit(ctx, q, actor, "iteration.providers.credential", "iteration", iterationID,
			map[string]any{
				"provider": row.Provider, "category": row.Category,
				"authType": string(auth.Type), "secretSet": auth.Secret != "",
			}); err != nil {
			return err
		}
		out, err = s.iterationProvidersView(ctx, q, it)
		return err
	})
	return out, err
}

// providerKeyOf 是绑定唯一键（与表上的 UNIQUE 约束一致）。
func providerKeyOf(category, provider, target string) string {
	return category + "\x00" + provider + "\x00" + target
}

// ensureNewCredentials 校验「首次配置认证方式时必须给出 Key」：
// 未提交 Secret 时，只有库内已有可沿用密文（同 category/provider/target）才放行。
func ensureNewCredentials(bindings []workitem.ProviderBinding, existing []store.IterationProviderRow) error {
	have := make(map[string]bool, len(existing))
	for _, r := range existing {
		have[providerKeyOf(r.Category, r.Provider, r.Target)] = r.AuthSecretEnc != ""
	}
	for _, b := range bindings {
		if b.Auth.Type == workitem.AuthNone || b.Auth.Secret != "" {
			continue
		}
		if have[providerKeyOf(string(b.Category), string(b.Provider), b.Target)] {
			continue
		}
		return Invalid("%s provider %q 选择了 %s 认证，需要填写 Key", b.Category.Label(), b.Provider, b.Auth.Type)
	}
	return nil
}

// insertIterationBindings 在建迭代时写入绑定（成果物必选）。
func (s *Service) insertIterationBindings(ctx context.Context, q store.Querier, actor Actor,
	iterationID string, inputs []IterationProviderInput) error {
	bindings, err := normalizeBindings(inputs)
	if err != nil {
		return err
	}
	rows, err := s.providerRows(iterationID, actor.UserID, bindings)
	if err != nil {
		return err
	}
	if err := ensureNewCredentials(bindings, nil); err != nil {
		return err
	}
	return store.SyncIterationProviders(ctx, q, iterationID, rows)
}
