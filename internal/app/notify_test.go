package app

// notify_test.go：邮件收件人画像（事件 → 相关用户）与事件载荷解析。
// 收件人邮箱本身来自 users.email（DB 查询），此处覆盖不依赖数据库的判定部分。

import (
	"context"
	"testing"

	"github.com/yuylong/Chalkline/internal/store"
)

// TestNotifyAudiences 事件 → 相关用户群体（任务进展、迭代进展、门禁与升级）。
func TestNotifyAudiences(t *testing.T) {
	cases := []struct {
		event string
		want  notifyAudience
	}{
		{"node.ready", notifyAudience{Responsible: true}},
		{"node.passed", notifyAudience{Responsible: true, Owners: true}},
		{"node.rejected", notifyAudience{Responsible: true, Owners: true}},
		{"gate.pending", notifyAudience{Responsible: true, Reviewers: true}},
		{"gate.overdue", notifyAudience{Responsible: true, Reviewers: true}},
		{"gate.escalated", notifyAudience{Responsible: true, Reviewers: true, Owners: true, Admins: true}},
		{"task.assigned", notifyAudience{Responsible: true}},
		{"iteration.closed", notifyAudience{Responsible: true, Owners: true, IterationAssignees: true}},
	}
	if got := len(notifyAudiences); got != len(cases) {
		t.Fatalf("邮件事件数应为 %d，实际 %d", len(cases), got)
	}
	for _, c := range cases {
		if got := notifyAudiences[c.event]; got != c.want {
			t.Fatalf("%s 相关用户应为 %+v，实际 %+v", c.event, c.want, got)
		}
	}
	if _, ok := notifyAudiences["webhook.updated"]; ok {
		t.Fatal("非邮件事件不应有相关用户画像")
	}
}

// TestNotifyRecipientsSkipsUnrelated 非邮件事件与非工作项主体直接返回空（不查库）。
func TestNotifyRecipientsSkipsUnrelated(t *testing.T) {
	s := &Service{} // 无数据库：命中提前返回分支才不会 panic
	ctx := context.Background()

	got, err := s.NotifyRecipients(ctx, store.OutboxRow{
		EventType: "webhook.updated", SubjectType: "webhook", SubjectID: "whk_1"})
	if err != nil || got != nil {
		t.Fatalf("非邮件事件应返回空: %v %v", got, err)
	}

	got, err = s.NotifyRecipients(ctx, store.OutboxRow{
		EventType: "gate.pending", SubjectType: "product", SubjectID: "prd_1"})
	if err != nil || got != nil {
		t.Fatalf("非工作项主体应返回空: %v %v", got, err)
	}
}

func TestPayloadString(t *testing.T) {
	payload := map[string]any{"node": "node_design", "level": 2, "assignee": nil}
	if got := payloadString(payload, "node"); got != "node_design" {
		t.Fatalf("应读出字符串字段，实际 %q", got)
	}
	for _, key := range []string{"missing", "level", "assignee"} {
		if got := payloadString(payload, key); got != "" {
			t.Fatalf("%s 非字符串字段应返回空，实际 %q", key, got)
		}
	}
}
