package product

import (
	"fmt"
	"regexp"
	"strings"
)

// MaxModuleDepth 是模块树深度上限（不变量 1：模块深度 ≤ 5）。
const MaxModuleDepth = 5

// 模块基本信息上限（docs/domain/model.md Module 字段表）。
const (
	MaxModuleDescription = 2000 // 功能简介字符上限
	MaxModuleTechStack   = 20   // 技术栈标签数上限
	MaxModuleTechTagLen  = 32   // 单个技术栈标签长度上限
	MaxModuleAssignees   = 20   // 开发者/测试者人数上限
)

var pathSegmentRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// Module 是产品下的多级树，是任务归属的「块」（对应 docs/modules/ 一份设计文档）。
type Module struct {
	ID        string
	ProductID string
	ParentID  string // 为空即根模块
	Name      string // 同级内唯一
	Path      string // 物化路径，如 /catalog/task-tree，用于子树查询
	SortOrder int
	// 基本信息（0016 迁移）：人员字段存 user_id，空即未指定。
	Description string   // 功能简介
	TechStack   []string // 技术栈标签（多选）
	OwnerID     string   // 主要责任人
	Developers  []string // 开发者
	Testers     []string // 测试者
}

// ValidateModuleName 检查模块名：非空且可作为路径段（同级唯一性由存储层约束）。
func ValidateModuleName(name string) error {
	if !pathSegmentRe.MatchString(name) {
		return fmt.Errorf("%w: 模块名 %q 需为小写字母数字与连字符，且以字母数字开头", ErrValidation, name)
	}
	return nil
}

// Depth 由物化路径推导层级深度（根模块深度为 1）。
func Depth(path string) (int, error) {
	if !strings.HasPrefix(path, "/") {
		return 0, fmt.Errorf("%w: 物化路径 %q 必须以 / 开头", ErrValidation, path)
	}
	trimmed := strings.TrimSuffix(path, "/")
	if trimmed == "" {
		return 0, fmt.Errorf("%w: 物化路径 %q 为空", ErrValidation, path)
	}
	d := strings.Count(trimmed, "/")
	return d, nil
}

// ValidatePath 校验物化路径格式与深度上限。
func ValidatePath(path string) error {
	d, err := Depth(path)
	if err != nil {
		return err
	}
	if d > MaxModuleDepth {
		return fmt.Errorf("%w: 模块深度 %d 超过上限 %d", ErrValidation, d, MaxModuleDepth)
	}
	for _, seg := range strings.Split(strings.TrimSuffix(path, "/"), "/")[1:] {
		if !pathSegmentRe.MatchString(seg) {
			return fmt.Errorf("%w: 路径段 %q 非法", ErrValidation, seg)
		}
	}
	return nil
}

// ChildPath 计算子模块的物化路径；同时校验深度上限（模块移动时整棵子树重算 path）。
func ChildPath(parentPath, name string) (string, error) {
	if err := ValidateModuleName(name); err != nil {
		return "", err
	}
	var base string
	switch parentPath {
	case "":
		base = ""
	default:
		if err := ValidatePath(parentPath); err != nil {
			return "", err
		}
		base = strings.TrimSuffix(parentPath, "/")
	}
	child := base + "/" + name
	if d, err := Depth(child); err != nil {
		return "", err
	} else if d > MaxModuleDepth {
		return "", fmt.Errorf("%w: 模块深度 %d 超过上限 %d", ErrValidation, d, MaxModuleDepth)
	}
	return child, nil
}

// NewModule 构造并校验模块。
func NewModule(id, productID, parentID, name string, sortOrder int) (Module, error) {
	if err := ValidateModuleName(name); err != nil {
		return Module{}, err
	}
	m := Module{ID: id, ProductID: productID, ParentID: parentID, Name: name, SortOrder: sortOrder}
	if m.ID == "" || m.ProductID == "" {
		return Module{}, fmt.Errorf("%w: id 与 product_id 不可为空", ErrValidation)
	}
	return m, nil
}

// ValidateModuleInfo 校验模块基本信息：简介长度、技术栈标签、人员 id 集合。
// 人员存在性由 app 层查库校验（domain 不依赖存储）。
func ValidateModuleInfo(desc string, techStack, developers, testers []string) error {
	if len([]rune(desc)) > MaxModuleDescription {
		return fmt.Errorf("%w: 功能简介超过 %d 字", ErrValidation, MaxModuleDescription)
	}
	if len(techStack) > MaxModuleTechStack {
		return fmt.Errorf("%w: 技术栈标签最多 %d 个", ErrValidation, MaxModuleTechStack)
	}
	seen := map[string]bool{}
	for _, tag := range techStack {
		if tag == "" || len([]rune(tag)) > MaxModuleTechTagLen {
			return fmt.Errorf("%w: 技术栈标签 %q 需为 1-%d 字", ErrValidation, tag, MaxModuleTechTagLen)
		}
		if seen[tag] {
			return fmt.Errorf("%w: 技术栈标签 %q 重复", ErrValidation, tag)
		}
		seen[tag] = true
	}
	for _, group := range [][]string{developers, testers} {
		if len(group) > MaxModuleAssignees {
			return fmt.Errorf("%w: 开发者/测试者最多 %d 人", ErrValidation, MaxModuleAssignees)
		}
		ids := map[string]bool{}
		for _, uid := range group {
			if uid == "" {
				return fmt.Errorf("%w: 人员 id 不可为空", ErrValidation)
			}
			if ids[uid] {
				return fmt.Errorf("%w: 人员 %q 重复", ErrValidation, uid)
			}
			ids[uid] = true
		}
	}
	return nil
}

// EnsureSiblingNameUnique 保证不变量 1：同级模块名唯一。
func EnsureSiblingNameUnique(siblings []Module, addingName string) error {
	for _, s := range siblings {
		if s.Name == addingName {
			return fmt.Errorf("%w: 同级已存在模块名 %q", ErrValidation, addingName)
		}
	}
	return nil
}
