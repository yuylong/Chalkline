package workflow

import (
	"errors"
	"strings"
	"testing"

	"github.com/yuylong/Chalkline/internal/domain/identity"
	"github.com/yuylong/Chalkline/internal/domain/workitem"
)

func validTaskYAML() string {
	return `
id: wf-test
name: 测试模板
scope: task
version: 1
nodes:
  - { id: a, name: A, type: normal }
  - { id: b, name: B, type: normal, gate: { approvals: 1, roles: [reviewer], artifacts: [{ kind: spec, required: true }] } }
  - { id: done, name: 完成, type: terminal }
edges:
  - { from: a, to: b }
  - { from: b, to: done }
`
}

func mustParse(t *testing.T, yaml string) Definition {
	t.Helper()
	def, err := ParseYAML([]byte(yaml))
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	return def
}

func TestParseYAMLValid(t *testing.T) {
	def := mustParse(t, validTaskYAML())
	if def.ID != "wf-test" || def.Scope != workitem.ScopeTask {
		t.Fatalf("解析结果异常: %+v", def)
	}
	if len(def.Nodes) != 3 || len(def.Edges) != 2 {
		t.Fatalf("节点/边数量异常: %d/%d", len(def.Nodes), len(def.Edges))
	}
	// 单终态缺省 outcome = done。
	if def.TerminalNodes()[0].Outcome != workitem.OutcomeDone {
		t.Errorf("单终态 outcome 应缺省为 done")
	}
}

func TestParseYAMLInvalid(t *testing.T) {
	cases := map[string]string{
		"环": `
id: wf-c
name: 环
scope: task
version: 1
nodes:
  - { id: a, name: A, type: normal }
  - { id: b, name: B, type: normal }
  - { id: done, name: D, type: terminal }
edges:
  - { from: a, to: b }
  - { from: b, to: a }
  - { from: b, to: done }
`,
		"边引用不存在节点": `
id: wf-e
name: 边
scope: task
version: 1
nodes:
  - { id: a, name: A, type: normal }
  - { id: done, name: D, type: terminal }
edges:
  - { from: a, to: ghost }
  - { from: a, to: done }
`,
		"节点id重复": `
id: wf-d
name: 重复
scope: task
version: 1
nodes:
  - { id: a, name: A, type: normal }
  - { id: a, name: A2, type: normal }
  - { id: done, name: D, type: terminal }
edges:
  - { from: a, to: done }
`,
		"缺少终态": `
id: wf-t
name: 无终态
scope: task
version: 1
nodes:
  - { id: a, name: A, type: normal }
edges:
  - { from: a, to: a }
`,
		"终态带出边": `
id: wf-o
name: 终态出边
scope: task
version: 1
nodes:
  - { id: a, name: A, type: normal }
  - { id: done, name: D, type: terminal }
  - { id: b, name: B, type: normal }
edges:
  - { from: a, to: done }
  - { from: done, to: b }
  - { from: b, to: done }
`,
		"非终态无出边": `
id: wf-x
name: 悬空
scope: task
version: 1
nodes:
  - { id: c, name: C, type: normal }
  - { id: a, name: A, type: normal }
  - { id: done, name: D, type: terminal }
edges:
  - { from: c, to: a }
  - { from: c, to: done }
`,
		"自环": `
id: wf-s
name: 自环
scope: task
version: 1
nodes:
  - { id: a, name: A, type: normal }
  - { id: done, name: D, type: terminal }
edges:
  - { from: a, to: a }
  - { from: a, to: done }
`,
		"task作用域声明iteration属性": `
id: wf-scope
name: 越界
scope: task
version: 1
nodes:
  - { id: a, name: A, type: normal, allows_task_creation: true }
  - { id: done, name: D, type: terminal }
edges:
  - { from: a, to: done }
`,
		"task作用域声明allows_task_kinds": `
id: wf-scope-kinds
name: 越界白名单
scope: task
version: 1
nodes:
  - { id: a, name: A, type: normal, allows_task_kinds: [dev] }
  - { id: done, name: D, type: terminal }
edges:
  - { from: a, to: done }
`,
		"iteration作用域白名单类型非法": `
id: wf-bad-kind
name: 非法类型
scope: iteration
version: 1
nodes:
  - { id: a, name: A, type: normal, allows_task_kinds: [wizard] }
  - { id: done, name: D, type: terminal }
edges:
  - { from: a, to: done }
`,
		"门禁角色非法": `
id: wf-r
name: 角色
scope: task
version: 1
nodes:
  - { id: a, name: A, type: normal, gate: { approvals: 1, roles: [wizard] } }
  - { id: done, name: D, type: terminal }
edges:
  - { from: a, to: done }
`,
		"门禁approvals为零": `
id: wf-z
name: 零审批
scope: task
version: 1
nodes:
  - { id: a, name: A, type: normal, gate: { approvals: 0, roles: [reviewer] } }
  - { id: done, name: D, type: terminal }
edges:
  - { from: a, to: done }
`,
		"多终态缺outcome": `
id: wf-m
name: 多终态
scope: task
version: 1
nodes:
  - { id: a, name: A, type: normal }
  - { id: done, name: D, type: terminal }
  - { id: cancelled, name: C, type: terminal }
edges:
  - { from: a, to: done }
  - { from: a, to: cancelled }
`,
	}
	for name, yaml := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := ParseYAML([]byte(yaml))
			if err == nil {
				t.Fatalf("期望校验失败，但通过了")
			}
			if !errors.Is(err, ErrInvalidDefinition) {
				t.Errorf("错误应包装 ErrInvalidDefinition: %v", err)
			}
			if !strings.Contains(err.Error(), "workflow:") {
				t.Errorf("错误消息应可读: %v", err)
			}
		})
	}
}

func TestMultiStartAccepted(t *testing.T) {
	// 多起点（多条并行主线）是合法 DAG：文档未限定单一起点，
	// 且 DAG 中所有节点必然可达自某个入度为 0 的起点。
	mustParse(t, `
id: wf-ms
name: 多起点
scope: iteration
version: 1
nodes:
  - { id: a, name: A, type: normal }
  - { id: b, name: B, type: normal }
  - { id: join, name: 汇合, type: normal }
  - { id: done, name: 完成, type: terminal }
edges:
  - { from: a, to: join }
  - { from: b, to: join }
  - { from: join, to: done }
`)
}

func TestAllowsTaskKind(t *testing.T) {
	// allows_task_creation: true 是「全部类型」的语法糖（旧模板兼容）；
	// allows_task_kinds 是显式白名单；两者都无 = 不允许任何类型。
	def := mustParse(t, `
id: wf-kinds
name: 类型白名单
scope: iteration
version: 1
nodes:
  - { id: sugar, name: 语法糖, type: normal, allows_task_creation: true }
  - { id: list, name: 白名单, type: normal, allows_task_kinds: [dev, bug] }
  - { id: none, name: 不允许, type: normal }
  - { id: done, name: 完成, type: terminal }
edges:
  - { from: sugar, to: list }
  - { from: list,   to: none }
  - { from: none,   to: done }
`)
	sugar, _ := def.Node("sugar")
	if !sugar.AllowsTaskKind("dev") || !sugar.AllowsTaskKind("bug") || !sugar.AllowsTaskKind("test") {
		t.Error("allows_task_creation: true 应允许全部类型")
	}
	list, _ := def.Node("list")
	if !list.AllowsTaskKind("dev") || !list.AllowsTaskKind("bug") {
		t.Error("白名单应包含 dev / bug")
	}
	if list.AllowsTaskKind("test") || list.AllowsTaskKind("") {
		t.Error("白名单外的类型应被拒绝")
	}
	none, _ := def.Node("none")
	if none.AllowsTaskKind("dev") {
		t.Error("未声明白名单的节点应拒绝一切类型")
	}
}

func TestNormalizeGateModeDefaults(t *testing.T) {
	def := mustParse(t, `
id: wf-n
name: 默认mode
scope: task
version: 1
nodes:
  - { id: a, name: A, type: normal, gate: { approvals: 1, roles: [reviewer] } }
  - { id: done, name: D, type: terminal }
edges:
  - { from: a, to: done }
`)
	n, _ := def.Node("a")
	if n.Gate.Mode != workitem.GateModeAll {
		t.Errorf("gate.mode 缺省应为 all，实际 %q", n.Gate.Mode)
	}
}

func TestRolesSerialize(t *testing.T) {	// 角色从 YAML 字符串解析为 identity.Role。
	def := mustParse(t, validTaskYAML())
	n, _ := def.Node("b")
	if len(n.Gate.Roles) != 1 || n.Gate.Roles[0] != identity.RoleReviewer {
		t.Fatalf("角色解析异常: %+v", n.Gate.Roles)
	}
	req, declared := n.Gate.RequiresArtifact(workitem.KindSpec)
	if !req || !declared {
		t.Error("spec 应为必填材料")
	}
	_, declared = n.Gate.RequiresArtifact(workitem.KindPreReview)
	if declared {
		t.Error("模板未声明 pre_review")
	}
}
