package app

// distribute.go：迭代级 provider 的运行时分发（docs/domain/integrations.md「迭代 Provider」）。
//
// 事务提交后执行，全部尽力而为：失败只写 integration_syncs 回执，不改变节点状态、
// 不影响门禁判定（不变量 15）。外部 provider 只回填摘要与链接，不复制正文。

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/yuylong/Chalkline/internal/domain/workitem"
	"github.com/yuylong/Chalkline/internal/store"
	"github.com/yuylong/Chalkline/internal/workflow"
)

// ArtifactDistributor 把成果物分发到迭代绑定的外部 provider。
// 由 integration.Dispatcher 实现，server 组装层注入（app 不 import integration）。
type ArtifactDistributor interface {
	PushArtifact(ctx context.Context, b workitem.ProviderBinding, art workitem.ArtifactRef,
		meta workitem.ArtifactMeta, dir string) (workitem.PushedArtifact, error)
}

// ProcessRecorder 把节点/任务动作记录到流程记录 provider（如 Redmine）。
type ProcessRecorder interface {
	EnsureTicket(ctx context.Context, b workitem.ProviderBinding, subject workitem.ProcessSubject) (workitem.ProcessTicket, error)
	CommentTicket(ctx context.Context, b workitem.ProviderBinding, ticket workitem.ProcessTicket, note workitem.ProcessNote) error
}

// async 执行事务后的分发：注入 Async 时后台执行，否则同步执行（单测确定性）。
func (s *Service) async(fn func()) {
	if fn == nil {
		return
	}
	if s.Async != nil {
		s.Async(fn)
		return
	}
	fn()
}

// artifactToDistribute 是待分发的成果物（事务内收集，事务后推送）。
type artifactToDistribute struct {
	ID       string
	Ref      workitem.ArtifactRef
	NodeName string
}

// processAction 是待记录的一次流程动作。
type processAction struct {
	WorkItemType workitem.Scope
	WorkItemID   string
	NodeID       string
	NodeName     string
	NodeTitle    string // 建票标题（仅节点动作使用）
	Key          string // 幂等键：同一事实只推一次
	Body         string
}

// ---- 成果物分发 ----

// dispatchArtifacts 事务提交后分发成果物。
func (s *Service) dispatchArtifacts(wtype workitem.Scope, workItemID string, items []artifactToDistribute) {
	if s.Distributor == nil || len(items) == 0 {
		return
	}
	s.async(func() {
		ctx := context.Background()
		q := s.DB.Q()
		iterationID, err := s.iterationIDOfWorkItem(ctx, q, wtype, workItemID)
		if err != nil {
			s.Log.Warn("成果物分发跳过：无法定位迭代", "workItem", workItemID, "error", err)
			return
		}
		bindings, err := s.iterationBindings(ctx, q, iterationID)
		if err != nil {
			s.Log.Warn("成果物分发跳过：读取绑定失败", "iteration", iterationID, "error", err)
			return
		}
		binding, ok := workitem.ArtifactBinding(bindings)
		if !ok || binding.Provider == workitem.ProviderInline {
			return // 未配置或 inline：成果物已存平台内
		}
		it, err := store.IterationByID(ctx, q, iterationID)
		if err != nil {
			return
		}
		for _, a := range items {
			s.dispatchOne(ctx, iterationID, it.Number, binding, a)
		}
	})
}

// dispatchOne 分发单条成果物：只有「内容交给 Chalkline」的材料才外推。
// 已带外部 URL 的引用说明成果物本来就在外部系统里，不重复创建。
func (s *Service) dispatchOne(ctx context.Context, iterationID, iterationNumber string,
	binding workitem.ProviderBinding, item artifactToDistribute) {
	if item.Ref.Provider != workitem.ProviderInline && item.Ref.Provider != binding.Provider {
		return
	}
	if strings.TrimSpace(item.Ref.URL) != "" {
		return
	}
	q := s.DB.Q()
	syncID, claimed, err := store.ClaimIntegrationSync(ctx, q, store.IntegrationSyncRow{
		IterationID: iterationID, Category: string(workitem.CategoryArtifact),
		Provider: string(binding.Provider), SubjectType: "artifact", SubjectID: item.ID, Action: "push",
	})
	if err != nil {
		s.Log.Warn("成果物分发抢占失败", "artifact", item.ID, "error", err)
		return
	}
	if !claimed {
		return // 幂等：已成功推送过
	}
	dir := binding.DirPath(map[string]string{
		"iteration": iterationNumber,
		"node":      item.NodeName,
		"kind":      string(item.Ref.Kind),
	})
	pushed, err := s.Distributor.PushArtifact(ctx, binding, item.Ref, workitem.ArtifactMeta{}, dir)
	if err != nil {
		_ = store.FinishIntegrationSync(ctx, q, syncID, "failed", "", "", "", err.Error())
		s.Log.Warn("成果物分发失败（不阻断流程）", "artifact", item.ID,
			"provider", binding.Provider, "error", err)
		return
	}
	_ = store.FinishIntegrationSync(ctx, q, syncID, "ok", pushed.ExternalID, pushed.URL, pushed.Summary, "")
	if err := store.SetArtifactDispatch(ctx, q, item.ID, string(binding.Provider), pushed.URL, pushed.ExternalID); err != nil {
		s.Log.Warn("成果物回执写入失败", "artifact", item.ID, "error", err)
	}
	_, _ = store.UpsertExternalLink(ctx, q, store.ExternalLinkRow{
		SubjectType: "artifact", SubjectID: item.ID, Provider: string(binding.Provider),
		ExternalID: pushed.ExternalID, URL: pushed.URL, CreatedBy: item.Ref.SubmittedBy,
	})
}

// ---- 流程记录 ----

// recordProcess 事务提交后把节点/任务动作写到流程记录 provider。
func (s *Service) recordProcess(actions ...processAction) {
	if s.Process == nil || len(actions) == 0 {
		return
	}
	s.async(func() {
		ctx := context.Background()
		for _, a := range actions {
			if a.Body == "" {
				continue
			}
			s.recordOneProcess(ctx, a)
		}
	})
}

func (s *Service) recordOneProcess(ctx context.Context, a processAction) {
	q := s.DB.Q()
	iterationID, err := s.iterationIDOfWorkItem(ctx, q, a.WorkItemType, a.WorkItemID)
	if err != nil {
		s.Log.Warn("流程记录跳过：无法定位迭代", "workItem", a.WorkItemID, "error", err)
		return
	}
	bindings, err := s.iterationBindings(ctx, q, iterationID)
	if err != nil {
		return
	}
	binding, ok := workitem.ProcessBinding(bindings)
	if !ok {
		return
	}
	subject, err := s.processSubject(ctx, q, a, iterationID)
	if err != nil {
		s.Log.Warn("流程记录跳过：组装主体失败", "workItem", a.WorkItemID, "error", err)
		return
	}
	ticket, ok, err := s.ensureProcessTicket(ctx, q, iterationID, binding, subject)
	if err != nil {
		s.Log.Warn("流程记录建票失败（不阻断流程）", "subject", subject.ID,
			"provider", binding.Provider, "error", err)
		return
	}
	if !ok || ticket.ExternalID == "" {
		return
	}
	syncID, claimed, err := store.ClaimIntegrationSync(ctx, q, store.IntegrationSyncRow{
		IterationID: iterationID, Category: string(workitem.CategoryProcess),
		Provider: string(binding.Provider), SubjectType: subject.Type, SubjectID: subject.ID,
		Action: "comment:" + a.Key,
	})
	if err != nil || !claimed {
		return
	}
	note := workitem.ProcessNote{Title: subject.Title, Body: a.Body, Key: a.Key}
	if err := s.Process.CommentTicket(ctx, binding, ticket, note); err != nil {
		_ = store.FinishIntegrationSync(ctx, q, syncID, "failed", ticket.ExternalID, ticket.URL, "", err.Error())
		s.Log.Warn("流程记录追加回复失败（不阻断流程）", "subject", subject.ID, "error", err)
		return
	}
	_ = store.FinishIntegrationSync(ctx, q, syncID, "ok", ticket.ExternalID, ticket.URL, a.Key, "")
}

// processSubject 决定动作落在哪张票上：
//   - 迭代节点：每个节点一张票（subject_id = 迭代:节点）；
//   - 任务节点：任务一张票（subject_id = 任务），回复里带上节点名。
func (s *Service) processSubject(ctx context.Context, q store.Querier,
	a processAction, iterationID string) (workitem.ProcessSubject, error) {
	if a.WorkItemType == workitem.ScopeTask {
		t, err := store.TaskByID(ctx, q, a.WorkItemID)
		if err != nil {
			return workitem.ProcessSubject{}, err
		}
		return workitem.ProcessSubject{
			Type: "task", ID: t.ID, Title: fmt.Sprintf("%s %s", t.Number, t.Title),
			Description: t.Description,
		}, nil
	}
	it, err := store.IterationByID(ctx, q, iterationID)
	if err != nil {
		return workitem.ProcessSubject{}, err
	}
	title := strings.TrimSpace(a.NodeTitle)
	if title == "" {
		title = a.NodeName
	}
	return workitem.ProcessSubject{
		Type: "node", ID: iterationID + ":" + a.NodeID,
		Title:       fmt.Sprintf("%s %s · %s", it.Number, it.Name, title),
		Description: fmt.Sprintf("迭代 %s %s 的阶段「%s」流程记录。", it.Number, it.Name, title),
	}, nil
}

// ensureProcessTicket 取或建票；建票经幂等抢占，避免并发重复创建。
func (s *Service) ensureProcessTicket(ctx context.Context, q store.Querier, iterationID string,
	b workitem.ProviderBinding, subject workitem.ProcessSubject) (workitem.ProcessTicket, bool, error) {
	link, err := store.ExternalLinkBySubject(ctx, q, subject.Type, subject.ID, string(b.Provider))
	if err == nil && link.ExternalID != "" {
		return workitem.ProcessTicket{ExternalID: link.ExternalID, URL: link.URL}, true, nil
	}
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return workitem.ProcessTicket{}, false, err
	}
	syncID, claimed, err := store.ClaimIntegrationSync(ctx, q, store.IntegrationSyncRow{
		IterationID: iterationID, Category: string(workitem.CategoryProcess),
		Provider: string(b.Provider), SubjectType: subject.Type, SubjectID: subject.ID, Action: "ticket",
	})
	if err != nil {
		return workitem.ProcessTicket{}, false, err
	}
	if !claimed {
		// 另一请求已建成：复用其链接。
		link, err = store.ExternalLinkBySubject(ctx, q, subject.Type, subject.ID, string(b.Provider))
		if err != nil {
			return workitem.ProcessTicket{}, false, err
		}
		return workitem.ProcessTicket{ExternalID: link.ExternalID, URL: link.URL}, true, nil
	}
	ticket, err := s.Process.EnsureTicket(ctx, b, subject)
	if err != nil {
		_ = store.FinishIntegrationSync(ctx, q, syncID, "failed", "", "", "", err.Error())
		return workitem.ProcessTicket{}, false, err
	}
	_ = store.FinishIntegrationSync(ctx, q, syncID, "ok", ticket.ExternalID, ticket.URL, subject.Title, "")
	if _, err := store.UpsertExternalLink(ctx, q, store.ExternalLinkRow{
		SubjectType: subject.Type, SubjectID: subject.ID, Provider: string(b.Provider),
		ExternalID: ticket.ExternalID, URL: ticket.URL, CreatedBy: "",
	}); err != nil {
		s.Log.Warn("流程记录关联写入失败", "subject", subject.ID, "error", err)
	}
	return ticket, true, nil
}

// processBody 组装流程记录的回复正文（只含元数据，不携带成果物正文）。
func processBody(actor Actor, action string, lines ...string) string {
	var b strings.Builder
	b.WriteString("### " + action + "\n\n")
	for _, l := range lines {
		if strings.TrimSpace(l) != "" {
			b.WriteString("- " + strings.TrimSpace(l) + "\n")
		}
	}
	if actor.Username != "" {
		b.WriteString("- 操作者：" + actor.Username + "\n")
	}
	b.WriteString("\n> 由 Chalkline 单向推送，状态以 Chalkline 为准。\n")
	return b.String()
}

// checkArtifactBindings 校验材料来源是否符合迭代绑定（不变量 22）。
// 未配置绑定的历史迭代不改变既有行为。
func (s *Service) checkArtifactBindings(ctx context.Context, q store.Querier,
	wtype workitem.Scope, workItemID string, refs []workitem.ArtifactRef) error {
	if len(refs) == 0 {
		return nil
	}
	iterationID, err := s.iterationIDOfWorkItem(ctx, q, wtype, workItemID)
	if err != nil {
		return err
	}
	bindings, err := s.iterationBindings(ctx, q, iterationID)
	if err != nil {
		return err
	}
	for _, ref := range refs {
		if err := workitem.CheckArtifactProvider(bindings, ref.Kind, ref.Provider); err != nil {
			return InvalidArtifactRef(err)
		}
	}
	return nil
}

// summarizeRef 外部 provider 只落摘要：正文不外存（正文留在外部系统，平台只保留引用）。
func summarizeRef(ref workitem.ArtifactRef) workitem.ArtifactRef {
	if ref.Provider == workitem.ProviderInline {
		return ref
	}
	ref.Content = ""
	return ref
}

// nodeNameOf 取节点名（缺失时退化为节点 id）。
func nodeNameOf(inst *workflow.Instance, nodeID string) string {
	if inst == nil {
		return nodeID
	}
	if n, ok := inst.Definition.Node(nodeID); ok && n.Name != "" {
		return n.Name
	}
	return nodeID
}

// ---- 流程动作构造（幂等键取节点状态版本，重试不重复推送）----

func startProcessAction(actor Actor, wtype workitem.Scope, workItemID, nodeID, nodeName string,
	version int64) processAction {
	return processAction{
		WorkItemType: wtype, WorkItemID: workItemID, NodeID: nodeID, NodeName: nodeName,
		Key:  fmt.Sprintf("start:%s:%d", nodeID, version),
		Body: processBody(actor, "开始节点", "节点："+nodeName),
	}
}

func submitProcessAction(actor Actor, wtype workitem.Scope, workItemID, nodeID, nodeName string,
	version int64, artifactCount int, status workitem.NodeStatus, note string) processAction {
	lines := []string{
		"节点：" + nodeName,
		fmt.Sprintf("成果物：%d 件", artifactCount),
		"节点状态：" + string(status),
	}
	if strings.TrimSpace(note) != "" {
		lines = append(lines, "说明："+strings.TrimSpace(note))
	}
	return processAction{
		WorkItemType: wtype, WorkItemID: workItemID, NodeID: nodeID, NodeName: nodeName,
		Key:  fmt.Sprintf("submit:%s:%d", nodeID, version),
		Body: processBody(actor, "提交节点", lines...),
	}
}

func reviewProcessAction(actor Actor, wtype workitem.Scope, workItemID, nodeID, nodeName string,
	version int64, decision workitem.Decision, comment string) processAction {
	action := "门禁通过"
	if decision == workitem.DecisionReject {
		action = "门禁驳回"
	}
	lines := []string{"节点：" + nodeName, "决策：" + string(decision)}
	if strings.TrimSpace(comment) != "" {
		lines = append(lines, "意见："+strings.TrimSpace(comment))
	}
	return processAction{
		WorkItemType: wtype, WorkItemID: workItemID, NodeID: nodeID, NodeName: nodeName,
		Key:  fmt.Sprintf("review:%s:%d", nodeID, version),
		Body: processBody(actor, action, lines...),
	}
}

func attachProcessAction(actor Actor, wtype workitem.Scope, workItemID, nodeID, nodeName string,
	ref workitem.ArtifactRef) processAction {
	sum := sha256.Sum256([]byte(string(ref.Kind) + "|" + string(ref.Provider) + "|" +
		ref.URL + "|" + ref.ExternalID + "|" + ref.Content))
	return processAction{
		WorkItemType: wtype, WorkItemID: workItemID, NodeID: nodeID, NodeName: nodeName,
		Key: "attach:" + nodeID + ":" + hex.EncodeToString(sum[:6]),
		Body: processBody(actor, "补挂成果物", "节点："+nodeName,
			"类型："+string(ref.Kind), "来源："+string(ref.Provider)),
	}
}

func taskCreateProcessAction(actor Actor, taskID, number, title string) processAction {
	return processAction{
		WorkItemType: workitem.ScopeTask, WorkItemID: taskID,
		Key:  "task.create:" + taskID,
		Body: processBody(actor, "创建任务", "编号："+number, "标题："+title),
	}
}
