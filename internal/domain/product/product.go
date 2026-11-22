// Package product 承载顶层容器：产品、产品成员（可见性依据）、模块树。
// 对应 docs/domain/model.md 的 Product / ProductMember / Module 一节。
package product

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// Key 是产品编号前缀（如 CHK），用于任务编号 CHK-12；创建后不可改。
type Key string

var keyRe = regexp.MustCompile(`^[A-Z]{2,8}$`)

// Valid 报告 key 是否为 2–8 位大写字母（docs/domain/model.md）。
func (k Key) Valid() bool { return keyRe.MatchString(string(k)) }

// Status 是产品状态。
type Status string

const (
	StatusActive   Status = "active"
	StatusArchived Status = "archived"
)

func (s Status) Valid() bool { return s == StatusActive || s == StatusArchived }

// Product 是顶层容器。产品本身不跑工作流：产品是迭代的序列。
type Product struct {
	ID                          string
	Key                         Key
	Name                        string
	Description                 string
	Status                      Status
	DefaultIterationWorkflowID  string // 新建迭代时的默认生命周期模板
	DefaultTaskWorkflowID       string // 新建任务时的默认模板
	CreatedBy                   string // 创建人，自动成为该产品的 owner
}

// ErrValidation 表示实体违反不变量。
var ErrValidation = errors.New("product: 实体校验失败")

// New 构造并校验产品。
func New(id string, key Key, name, description, createdBy string) (Product, error) {
	p := Product{ID: id, Key: key, Name: name, Description: description,
		Status: StatusActive, CreatedBy: createdBy}
	if err := p.Validate(); err != nil {
		return Product{}, err
	}
	return p, nil
}

// Validate 检查字段级不变量。
func (p Product) Validate() error {
	if p.ID == "" {
		return fmt.Errorf("%w: id 为空", ErrValidation)
	}
	if !p.Key.Valid() {
		return fmt.Errorf("%w: key %q 需为 2-8 位大写字母且不可改", ErrValidation, p.Key)
	}
	if strings.TrimSpace(p.Name) == "" {
		return fmt.Errorf("%w: name 为空", ErrValidation)
	}
	if p.CreatedBy == "" {
		return fmt.Errorf("%w: created_by 为空", ErrValidation)
	}
	return nil
}

// Archive 将产品归档。归档后成员记录保留，只读可查历史。
func (p *Product) Archive() error {
	if p.Status != StatusActive {
		return fmt.Errorf("%w: 仅 active 产品可归档", ErrValidation)
	}
	p.Status = StatusArchived
	return nil
}
