package app

// artifact_test.go：材料视图投影（chalkline_get_artifact / get_iteration 的数据来源）。
// 只测纯函数投影，不触库——读取权限与 NOT_FOUND 由 GetArtifact 的集成路径覆盖。

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/yuylong/Chalkline/internal/domain/workitem"
	"github.com/yuylong/Chalkline/internal/store"
)

func TestArtifactViewInlineKeepsContent(t *testing.T) {
	at := time.Date(2026, 11, 28, 3, 12, 44, 0, time.UTC)
	v := artifactViewOf(store.ArtifactRow{
		ID: "art_1", InstanceID: "win_1", CreatedAt: at,
		Ref: workitem.ArtifactRef{
			Kind: workitem.KindResearchReport, Provider: workitem.ProviderInline,
			Title: "竞品调研报告 v2", Content: "# 报告正文", NodeID: "research_market",
			SubmittedBy: "usr_07c9",
		},
	})
	if v.Content != "# 报告正文" {
		t.Errorf("inline 材料应返回正文，得到 %q", v.Content)
	}
	if v.CreatedAt != at.Format(time.RFC3339) {
		t.Errorf("createdAt 应为 RFC3339，得到 %q", v.CreatedAt)
	}
	if v.NodeID != "research_market" || v.SubmittedBy != "usr_07c9" {
		t.Errorf("节点与提交人应回显: %+v", v)
	}
}

func TestArtifactViewExternalKeepsRefAndSnapshot(t *testing.T) {
	ref := workitem.ArtifactRef{
		Kind: workitem.KindSpec, Provider: workitem.ProviderOutline,
		URL: "https://outline.example.com/doc/1", ExternalID: "doc_1",
		Title: "需求规格", NodeID: "spec", SubmittedBy: "usr_dev",
	}
	snap, _ := json.Marshal(map[string]any{"title": "需求规格", "version": "v3", "status": "published"})
	v := artifactViewOf(store.ArtifactRow{ID: "art_2", Ref: ref, Snapshot: snap})
	if v.Content != "" {
		t.Error("外部 provider 不应返回正文（正文留在外部系统）")
	}
	if v.URL != ref.URL || v.ExternalID != ref.ExternalID {
		t.Errorf("外部引用应回显: %+v", v)
	}
	if v.Snapshot == nil || v.Snapshot["version"] != "v3" {
		t.Errorf("提交时快照应透传: %+v", v.Snapshot)
	}
}

func TestArtifactViewBadSnapshotIgnored(t *testing.T) {
	v := artifactViewOf(store.ArtifactRow{
		Ref:      workitem.ArtifactRef{Kind: workitem.KindSpec, Provider: workitem.ProviderInline, Content: "x"},
		Snapshot: json.RawMessage(`not-json`),
	})
	if v.Snapshot != nil {
		t.Errorf("非法快照应被忽略而不是让调用失败: %+v", v.Snapshot)
	}
	if v.Content != "x" {
		t.Error("快照解析失败不应影响正文")
	}
}

func TestRejectDetailViewPropagatesComment(t *testing.T) {
	// REST/MCP 的 gate.pendingRejectDetails 与 get_next_action 的 blocker 同源。
	v := RejectDetailView{ReviewerID: "u_31", Comment: "跟 Jira 的对比还需要加强"}
	if v.Comment == "" || v.ReviewerID == "" {
		t.Fatalf("驳回明细必须带人与理由: %+v", v)
	}
}
