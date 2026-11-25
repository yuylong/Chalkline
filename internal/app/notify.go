package app

// notify.go：邮件通知收件人解析（通知按「相关用户」投递，docs/domain/integrations.md）。
// 事件不再群发到一组固定邮箱，而是按事件语义解析相关成员：任务负责人、迭代创建人、
// 当前节点的合格评审人、产品 owner、评审升级兜底的 admin。收件地址取自成员设置里的
// 邮箱（users.email）。CHALKLINE_NOTIFY_EMAILS 只在解析不出任何相关用户时兜底。
// 依赖方向：notify.SMTP.Recipients 由 server 组装层注入本层解析结果（app 不 import integration）。

import (
	"context"
	"errors"

	"github.com/yuylong/Chalkline/internal/domain/identity"
	"github.com/yuylong/Chalkline/internal/domain/product"
	"github.com/yuylong/Chalkline/internal/domain/workitem"
	"github.com/yuylong/Chalkline/internal/store"
	"github.com/yuylong/Chalkline/internal/workflow"
)

// notifyAudience 描述某类事件的相关用户群体（多选，结果按成员去重）。
type notifyAudience struct {
	// Responsible 工作项责任人：任务负责人（指派事件优先取事件里的 assignee）、迭代创建人。
	Responsible bool
	// Reviewers 当前节点的合格评审人（按实例模板的 gate.roles 解析）。
	Reviewers bool
	// Owners 产品 owner。
	Owners bool
	// Admins 平台 admin（评审升级的兜底受众）。
	Admins bool
	// IterationAssignees 迭代内全部任务的负责人（迭代收尾时通知参与者）。
	IterationAssignees bool
}

// notifyAudiences 是事件 → 相关用户映射（与 notify 包可发邮件的事件集合一致）。
var notifyAudiences = map[string]notifyAudience{
	"node.ready":       {Responsible: true},
	"node.passed":      {Responsible: true, Owners: true},
	"node.rejected":    {Responsible: true, Owners: true},
	"gate.pending":     {Responsible: true, Reviewers: true},
	"gate.overdue":     {Responsible: true, Reviewers: true},
	"gate.escalated":   {Responsible: true, Reviewers: true, Owners: true, Admins: true},
	"task.assigned":    {Responsible: true},
	"iteration.closed": {Responsible: true, Owners: true, IterationAssignees: true},
}

// NotifyRecipients 按事件解析邮件收件人邮箱；空结果表示没有相关用户。
func (s *Service) NotifyRecipients(ctx context.Context, ev store.OutboxRow) ([]string, error) {
	aud, ok := notifyAudiences[ev.EventType]
	if !ok {
		return nil, nil
	}
	wtype := workitem.Scope(ev.SubjectType)
	if wtype != workitem.ScopeTask && wtype != workitem.ScopeIteration {
		return nil, nil
	}
	q := s.DB.Q()
	productID, err := store.ProductIDOfSubject(ctx, q, ev.SubjectType, ev.SubjectID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, nil // 工作项已删除：事件不再有相关用户
		}
		return nil, err
	}

	var ids []string
	if aud.Responsible {
		list, err := s.notifyResponsibleIDs(ctx, q, wtype, ev)
		if err != nil {
			return nil, err
		}
		ids = append(ids, list...)
	}
	if aud.IterationAssignees && wtype == workitem.ScopeIteration {
		list, err := store.ListIterationAssigneeIDs(ctx, q, ev.SubjectID)
		if err != nil {
			return nil, err
		}
		ids = append(ids, list...)
	}
	if aud.Reviewers {
		list, err := s.notifyReviewerIDs(ctx, q, wtype, ev, productID)
		if err != nil {
			return nil, err
		}
		ids = append(ids, list...)
	}
	if aud.Owners {
		list, err := store.ListProductMemberUserIDs(ctx, q, productID,
			[]string{string(product.MemberOwner)})
		if err != nil {
			return nil, err
		}
		ids = append(ids, list...)
	}
	if aud.Admins {
		list, err := store.ListUserIDsWithRole(ctx, q, identity.RoleAdmin)
		if err != nil {
			return nil, err
		}
		ids = append(ids, list...)
	}
	return store.EmailsOfUsers(ctx, q, ids)
}

// notifyResponsibleIDs 取工作项责任人：任务 = 负责人（指派事件用事件里的被指派人），
// 迭代 = 创建人（迭代无「负责人」字段，创建人即推进者）。
func (s *Service) notifyResponsibleIDs(ctx context.Context, q store.Querier,
	wtype workitem.Scope, ev store.OutboxRow) ([]string, error) {
	switch wtype {
	case workitem.ScopeTask:
		if ev.EventType == "task.assigned" {
			if id := payloadString(ev.Payload, "assignee"); id != "" {
				return []string{id}, nil
			}
		}
		t, err := store.TaskByID(ctx, q, ev.SubjectID)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return nil, nil
			}
			return nil, err
		}
		return []string{t.AssigneeID}, nil
	case workitem.ScopeIteration:
		it, err := store.IterationByID(ctx, q, ev.SubjectID)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return nil, nil
			}
			return nil, err
		}
		return []string{it.CreatedBy}, nil
	}
	return nil, nil
}

// notifyReviewerIDs 按实例模板解析当前节点的合格评审人（gate.roles ∩ 产品非 guest 成员）。
func (s *Service) notifyReviewerIDs(ctx context.Context, q store.Querier,
	wtype workitem.Scope, ev store.OutboxRow, productID string) ([]string, error) {
	nodeID := payloadString(ev.Payload, "node")
	if nodeID == "" {
		return nil, nil
	}
	row, err := store.InstanceByWorkItem(ctx, q, wtype, ev.SubjectID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, nil // 无实例（工作项未建流程）：无评审人可通知
		}
		return nil, err
	}
	def, err := workflow.ParseJSON(row.DefinitionJSON)
	if err != nil {
		return nil, nil // 定义损坏：不影响其余受众
	}
	node, ok := def.Node(nodeID)
	if !ok || node.Gate == nil || len(node.Gate.Roles) == 0 {
		return nil, nil
	}
	candidates, err := store.ListEligibleReviewers(ctx, q, productID, roleNames(node.Gate.Roles))
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(candidates))
	for _, c := range candidates {
		out = append(out, c.UserID)
	}
	return out, nil
}

// payloadString 读 outbox 载荷里的字符串字段（JSON 解出为 string）。
func payloadString(payload map[string]any, key string) string {
	v, ok := payload[key].(string)
	if !ok {
		return ""
	}
	return v
}
