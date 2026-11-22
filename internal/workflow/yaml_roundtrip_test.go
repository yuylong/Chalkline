package workflow

// yaml_roundtrip_test.go：模板 YAML 往返（M3 导出路径的守卫——导出产物必须能再导入）。

import (
	"testing"

	"github.com/yuylong/Chalkline/internal/domain/identity"
	"github.com/yuylong/Chalkline/internal/domain/workitem"
	"gopkg.in/yaml.v3"
)

func TestDefinitionYAMLRoundTrip(t *testing.T) {
	builtins, err := LoadBuiltins()
	if err != nil {
		t.Fatalf("内置模板加载失败: %v", err)
	}
	if len(builtins) == 0 {
		t.Fatal("内置模板不应为空")
	}
	for _, def := range builtins {
		body, err := yaml.Marshal(def)
		if err != nil {
			t.Fatalf("模板 %s 序列化失败: %v", def.ID, err)
		}
		parsed, err := ParseYAML(body)
		if err != nil {
			t.Fatalf("导出的 YAML 应能再解析（模板 %s）: %v\n%s", def.ID, err, body)
		}
		if parsed.ID != def.ID || parsed.Scope != def.Scope ||
			parsed.Name != def.Name || parsed.Version != def.Version {
			t.Fatalf("往返后元数据不一致: %+v vs %+v", parsed, def)
		}
		if len(parsed.Nodes) != len(def.Nodes) || len(parsed.Edges) != len(def.Edges) {
			t.Fatalf("模板 %s 往返后节点/边数量不一致", def.ID)
		}
		// 门禁语义逐节点比对（roles / approvals / artifacts）。
		for i, n := range def.Nodes {
			pn := parsed.Nodes[i]
			if n.ID != pn.ID || n.Type != pn.Type {
				t.Fatalf("模板 %s 节点 %d 往返不一致: %+v vs %+v", def.ID, i, n, pn)
			}
			if (n.Gate == nil) != (pn.Gate == nil) {
				t.Fatalf("模板 %s 节点 %s 门禁丢失", def.ID, n.ID)
			}
			if n.Gate != nil {
				if n.Gate.Approvals != pn.Gate.Approvals ||
					len(n.Gate.Roles) != len(pn.Gate.Roles) {
					t.Fatalf("模板 %s 节点 %s 门禁语义往返不一致", def.ID, n.ID)
				}
			}
		}
	}
}

// TestDefinitionYAMLGateFields 门禁字段（含 outcome / allows_task_creation）不丢。
func TestDefinitionYAMLGateFields(t *testing.T) {
	def := Definition{
		ID: "wf-x", Name: "往返", Scope: workitem.ScopeTask, Version: 3,
		Settings: Settings{RequireChildrenDone: true},
		Nodes: []Node{
			{ID: "a", Name: "做", Type: NodeNormal},
			{ID: "done", Name: "完成", Type: NodeTerminal, Outcome: workitem.OutcomeDone},
		},
		Edges: []Edge{{From: "a", To: "done"}},
	}
	def.Nodes[0].Gate = &workitem.Gate{
		Roles:     []identity.Role{identity.RoleReviewer},
		Approvals: 2,
		Artifacts: []workitem.ArtifactRequirement{{Kind: workitem.KindSpec, Required: true}},
	}
	body, err := yaml.Marshal(def)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	parsed, err := ParseYAML(body)
	if err != nil {
		t.Fatalf("再解析失败: %v\n%s", err, body)
	}
	g := parsed.Nodes[0].Gate
	if g == nil || g.Approvals != 2 || len(g.Artifacts) != 1 ||
		g.Artifacts[0].Kind != workitem.KindSpec || !g.Artifacts[0].Required {
		t.Fatalf("门禁字段往返丢失: %+v", g)
	}
	if !parsed.Settings.RequireChildrenDone {
		t.Fatal("settings.require_children_done 往返丢失")
	}
	if parsed.Nodes[1].Outcome != workitem.OutcomeDone {
		t.Fatalf("终态 outcome 往返丢失: %+v", parsed.Nodes[1])
	}
}
