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
	"strings"

	"github.com/yuylong/Chalkline/internal/domain/workitem"
	"github.com/yuylong/Chalkline/internal/store"
)

// IterationProviderView 是绑定视图（含可点击的连接地址）。
type IterationProviderView struct {
	Category      string `json:"category"`
	CategoryLabel string `json:"categoryLabel"`
	Provider      string `json:"provider"`
	BaseURL       string `json:"baseUrl,omitempty"`
	Target        string `json:"target,omitempty"`
	DirTemplate   string `json:"dirTemplate,omitempty"`
	Branch        string `json:"branch,omitempty"`
	LinkURL       string `json:"linkUrl,omitempty"`
}

// IterationProviderInput 是绑定入参。
type IterationProviderInput struct {
	Category    string `json:"category"`
	Provider    string `json:"provider"`
	BaseURL     string `json:"baseUrl"`
	Target      string `json:"target"`
	DirTemplate string `json:"dirTemplate"`
	Branch      string `json:"branch"`
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
		if err := store.ReplaceIterationProviders(ctx, q, iterationID, s.providerRows(iterationID, actor.UserID, bindings)); err != nil {
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
		Items:       providerViews(rows),
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

// iterationBindings 取迭代绑定（供成果物分发与流程记录使用）。
func (s *Service) iterationBindings(ctx context.Context, q store.Querier,
	iterationID string) ([]workitem.ProviderBinding, error) {
	rows, err := store.ListIterationProviders(ctx, q, iterationID)
	if err != nil {
		return nil, err
	}
	out := make([]workitem.ProviderBinding, 0, len(rows))
	for _, r := range rows {
		out = append(out, workitem.ProviderBinding{
			Category:    workitem.ProviderCategory(r.Category),
			Provider:    workitem.ArtifactProvider(r.Provider),
			BaseURL:     r.BaseURL,
			Target:      r.Target,
			DirTemplate: r.DirTemplate,
			Branch:      r.Branch,
		})
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

// providerRows 把领域绑定转成存储行（连接信息随迭代固化）。
func (s *Service) providerRows(iterationID, createdBy string, bs []workitem.ProviderBinding) []store.IterationProviderRow {
	out := make([]store.IterationProviderRow, 0, len(bs))
	for _, b := range bs {
		out = append(out, store.IterationProviderRow{
			ID:          s.newID("itp"),
			IterationID: iterationID,
			Category:    string(b.Category),
			Provider:    string(b.Provider),
			BaseURL:     b.BaseURL,
			Target:      b.Target,
			DirTemplate: b.DirTemplate,
			Branch:      b.Branch,
			CreatedBy:   createdBy,
		})
	}
	return out
}

// providerViews 把存储行转成视图（排序与存储一致）。
func providerViews(rows []store.IterationProviderRow) []IterationProviderView {
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
		out = append(out, IterationProviderView{
			Category:      r.Category,
			CategoryLabel: b.Category.Label(),
			Provider:      r.Provider,
			BaseURL:       r.BaseURL,
			Target:        r.Target,
			DirTemplate:   r.DirTemplate,
			Branch:        r.Branch,
			LinkURL:       b.LinkURL(),
		})
	}
	return out
}

// insertIterationBindings 在建迭代时写入绑定（成果物必选）。
func (s *Service) insertIterationBindings(ctx context.Context, q store.Querier, actor Actor,
	iterationID string, inputs []IterationProviderInput) error {
	bindings, err := normalizeBindings(inputs)
	if err != nil {
		return err
	}
	return store.ReplaceIterationProviders(ctx, q, iterationID, s.providerRows(iterationID, actor.UserID, bindings))
}
