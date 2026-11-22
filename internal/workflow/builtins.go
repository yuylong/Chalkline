package workflow

import (
	"embed"
	"fmt"
	"sort"
)

// builtinFS 嵌入内置模板。内置模板只是初始化数据，与自建模板完全等价：
// 首次启动写入数据库后可编辑、复制、另建、设默认（docs/domain/workflow.md）。
//
//go:embed builtins/*.yaml
var builtinFS embed.FS

// BuiltinNames 返回内置模板文件名，按字典序。
func BuiltinNames() []string {
	entries, err := builtinFS.ReadDir("builtins")
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		out = append(out, e.Name())
	}
	sort.Strings(out)
	return out
}

// LoadBuiltin 解析并校验一份内置模板；M1 的种子数据用它写入 workflows 表。
func LoadBuiltin(name string) (Definition, error) {
	data, err := builtinFS.ReadFile("builtins/" + name)
	if err != nil {
		return Definition{}, fmt.Errorf("workflow: 内置模板 %s 不存在", name)
	}
	def, err := ParseYAML(data)
	if err != nil {
		return Definition{}, fmt.Errorf("workflow: 内置模板 %s 非法: %w", name, err)
	}
	return def, nil
}

// LoadBuiltins 解析全部内置模板。
func LoadBuiltins() (map[string]Definition, error) {
	out := map[string]Definition{}
	for _, name := range BuiltinNames() {
		def, err := LoadBuiltin(name)
		if err != nil {
			return nil, err
		}
		out[name] = def
	}
	return out, nil
}
