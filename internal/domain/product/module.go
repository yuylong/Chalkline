package product

import (
	"fmt"
	"regexp"
	"strings"
)

// MaxModuleDepth 是模块树深度上限（不变量 1：模块深度 ≤ 5）。
const MaxModuleDepth = 5

var pathSegmentRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// Module 是产品下的多级树，是任务归属的「块」（对应 docs/modules/ 一份设计文档）。
type Module struct {
	ID        string
	ProductID string
	ParentID  string // 为空即根模块
	Name      string // 同级内唯一
	Path      string // 物化路径，如 /catalog/task-tree，用于子树查询
	SortOrder int
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

// EnsureSiblingNameUnique 保证不变量 1：同级模块名唯一。
func EnsureSiblingNameUnique(siblings []Module, addingName string) error {
	for _, s := range siblings {
		if s.Name == addingName {
			return fmt.Errorf("%w: 同级已存在模块名 %q", ErrValidation, addingName)
		}
	}
	return nil
}
