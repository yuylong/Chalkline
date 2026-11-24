package app

// catalog.go：产品、成员、模块树、文档用例（docs/modules/catalog.md）。
// 产品隔离：一切读写先过成员关系（tech.md 约束 11），非成员 404。

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/yuylong/Chalkline/internal/domain/identity"
	"github.com/yuylong/Chalkline/internal/domain/product"
	"github.com/yuylong/Chalkline/internal/store"
)

// ProductLite 是轻量产品引用。
type ProductLite struct {
	ID     string `json:"id"`
	Key    string `json:"key"`
	Name   string `json:"name"`
	Status string `json:"status"`
}

// ProductView 是产品对外视图。
type ProductView struct {
	ProductLite
	Description                string `json:"description"`
	DefaultIterationWorkflowID string `json:"defaultIterationWorkflowId"`
	DefaultTaskWorkflowID      string `json:"defaultTaskWorkflowId"`
	CreatedBy                  string `json:"createdBy"`
	CreatedAt                  string `json:"createdAt"`
}

func viewProduct(p store.ProductRow) ProductView {
	return ProductView{
		ProductLite:                ProductLite{ID: p.ID, Key: p.Key, Name: p.Name, Status: string(p.Status)},
		Description:                p.Description,
		DefaultIterationWorkflowID: p.DefaultIterationWorkflowID,
		DefaultTaskWorkflowID:      p.DefaultTaskWorkflowID,
		CreatedBy:                  p.CreatedBy,
		CreatedAt:                  p.CreatedAt.UTC().Format(time.RFC3339),
	}
}

// CreateProductInput 建产品入参。
type CreateProductInput struct {
	Key         string // 2-8 大写，不可改
	Name        string
	Description string
}

// CreateProduct 建产品：全局 pm 或 admin；创建人自动成为 owner（不变量 17 基础）。
func (s *Service) CreateProduct(ctx context.Context, actor Actor, in CreateProductInput) (ProductView, error) {
	if !actor.Roles.HasAny(identity.RolePM) {
		return ProductView{}, RoleRequired("需要 pm 或 admin 角色")
	}
	p, err := product.New(s.newID("prd"), product.Key(in.Key), in.Name, in.Description, actor.UserID)
	if err != nil {
		return ProductView{}, MapError(err)
	}
	row := store.ProductRow{
		ID: p.ID, Key: string(p.Key), Name: p.Name, Description: p.Description,
		Status:                     product.StatusActive,
		DefaultIterationWorkflowID: "wf-iteration-lifecycle", // 内置默认模板
		DefaultTaskWorkflowID:      "wf-task-standard",
		CreatedBy:                  p.CreatedBy,
	}
	err = s.DB.WithTx(ctx, func(q store.Querier) error {
		if err := store.InsertProduct(ctx, q, row); err != nil {
			if store.IsDuplicate(err) {
				return Invalid("产品 key %q 已存在", in.Key)
			}
			return err
		}
		owner, err := product.NewMember(p.ID, actor.UserID, product.MemberOwner, actor.UserID)
		if err != nil {
			return MapError(err)
		}
		if err := store.UpsertMember(ctx, q, owner.ProductID, owner.UserID, owner.MemberRole, owner.CreatedBy); err != nil {
			return err
		}
		return audit(ctx, q, actor, "product.create", "product", p.ID, map[string]any{"key": in.Key})
	})
	if err != nil {
		return ProductView{}, err
	}
	return viewProduct(row), nil
}

// MemberView 是产品成员视图。
type MemberView struct {
	UserID      string `json:"userId"`
	Username    string `json:"username"`
	DisplayName string `json:"displayName"`
	Role        string `json:"role"`
	CreatedAt   string `json:"createdAt"`
}

// ModuleView 是模块节点视图（组树用）。
type ModuleView struct {
	ID        string       `json:"id"`
	ProductID string       `json:"productId"`
	ParentID  string       `json:"parentId,omitempty"`
	Name      string       `json:"name"`
	Path      string       `json:"path"`
	SortOrder int          `json:"sortOrder"`
	Children  []ModuleView `json:"children,omitempty"`
}

func viewModule(m store.ModuleRow) ModuleView {
	v := ModuleView{ID: m.ID, ProductID: m.ProductID, Name: m.Name, Path: m.Path, SortOrder: m.SortOrder}
	if m.ParentID != nil {
		v.ParentID = *m.ParentID
	}
	return v
}

// DocumentView 是文档视图。
type DocumentView struct {
	OwnerType string `json:"ownerType"`
	OwnerID   string `json:"ownerId"`
	Content   string `json:"content"`
	Version   int    `json:"version"`
}

// ProductDetail 是产品详情：模块树、成员、文档。
type ProductDetail struct {
	ProductView
	Members  []MemberView  `json:"members"`
	Modules  []ModuleView  `json:"modules"` // 树根数组
	Document *DocumentView `json:"document,omitempty"`
}

// GetProduct 产品详情：成员或 admin，否则 404。
func (s *Service) GetProduct(ctx context.Context, actor Actor, id string) (ProductDetail, error) {
	return s.loadProductDetail(ctx, actor, id)
}

func (s *Service) loadProductDetail(ctx context.Context, actor Actor, id string) (ProductDetail, error) {
	q := s.DB.Q()
	var detail ProductDetail
	p, err := store.ProductByID(ctx, q, id)
	if err != nil {
		return detail, wrapNotFound(err, "产品不存在或不可见")
	}
	if _, err := s.mustView(ctx, q, actor, id); err != nil {
		return detail, err
	}
	detail.ProductView = viewProduct(p)

	members, err := store.ListMembers(ctx, q, id)
	if err != nil {
		return detail, err
	}
	for _, m := range members {
		detail.Members = append(detail.Members, MemberView{
			UserID: m.UserID, Username: m.Username, DisplayName: m.DisplayName,
			Role: string(m.MemberRole), CreatedAt: m.CreatedAt.UTC().Format(time.RFC3339),
		})
	}

	modules, err := store.ListModulesByProduct(ctx, q, id)
	if err != nil {
		return detail, err
	}
	detail.Modules = buildModuleTree(modules)

	if doc, err := store.GetDocument(ctx, q, "product", id); err == nil {
		detail.Document = &DocumentView{OwnerType: "product", OwnerID: id, Content: doc.Content, Version: doc.Version}
	}
	return detail, nil
}

// buildModuleTree 由平铺行组树（物化路径已排序）。
func buildModuleTree(rows []store.ModuleRow) []ModuleView {
	type node struct {
		view     ModuleView
		children []*node
	}
	byID := map[string]*node{}
	for _, m := range rows {
		v := viewModule(m)
		byID[m.ID] = &node{view: v}
	}
	var roots []*node
	for _, m := range rows {
		n := byID[m.ID]
		if m.ParentID != nil {
			if p, ok := byID[*m.ParentID]; ok {
				p.children = append(p.children, n)
				continue
			}
		}
		roots = append(roots, n)
	}
	var render func(n *node) ModuleView
	render = func(n *node) ModuleView {
		v := n.view
		for _, c := range n.children {
			v.Children = append(v.Children, render(c))
		}
		return v
	}
	out := make([]ModuleView, 0, len(roots))
	for _, r := range roots {
		out = append(out, render(r))
	}
	return out
}

// ProductUpdateInput 产品可更新字段。
type ProductUpdateInput struct {
	Name                       *string
	Description                *string
	DefaultIterationWorkflowID *string
	DefaultTaskWorkflowID      *string
	Archive                    bool // true → archived
}

// UpdateProduct 改名/默认模板/归档：产品 owner 或 admin。
func (s *Service) UpdateProduct(ctx context.Context, actor Actor, id string, in ProductUpdateInput) (ProductView, error) {
	err := s.DB.WithTx(ctx, func(q store.Querier) error {
		p, err := store.ProductByID(ctx, q, id)
		if err != nil {
			return wrapNotFound(err, "产品不存在或不可见")
		}
		mc, err := s.mustView(ctx, q, actor, id)
		if err != nil {
			return err
		}
		if err := canManageProduct(mc); err != nil {
			return err
		}
		if in.Archive && p.Status == product.StatusActive {
			// 归档守卫（M3）：仍有未收尾迭代的产品不可归档，防止规模化管理误伤在途工作。
			open, err := store.CountOpenIterations(ctx, q, id)
			if err != nil {
				return err
			}
			if open > 0 {
				return &Error{Status: http.StatusConflict, Code: "INVALID_TRANSITION",
					Message: "产品仍有未收尾迭代（planned/active），先收尾或取消迭代再归档"}
			}
			dom := productFromRow(p)
			if err := dom.Archive(); err != nil {
				return MapError(err)
			}
			archived := product.StatusArchived
			if err := store.UpdateProduct(ctx, q, id, store.ProductUpdate{Status: &archived}); err != nil {
				return err
			}
		}
		if in.Name != nil || in.Description != nil ||
			in.DefaultIterationWorkflowID != nil || in.DefaultTaskWorkflowID != nil {
			if err := store.UpdateProduct(ctx, q, id, store.ProductUpdate{
				Name: in.Name, Description: in.Description,
				DefaultIterationWorkflowID: in.DefaultIterationWorkflowID,
				DefaultTaskWorkflowID:      in.DefaultTaskWorkflowID,
			}); err != nil {
				return err
			}
		}
		return audit(ctx, q, actor, "product.update", "product", id, map[string]any{"archive": in.Archive})
	})
	if err != nil {
		return ProductView{}, err
	}
	p, err := store.ProductByID(ctx, s.DB.Q(), id)
	if err != nil {
		return ProductView{}, err
	}
	return viewProduct(p), nil
}

func productFromRow(p store.ProductRow) product.Product {
	return product.Product{
		ID: p.ID, Key: product.Key(p.Key), Name: p.Name, Description: p.Description,
		Status: p.Status, CreatedBy: p.CreatedBy,
	}
}

// ListMembers 列产品成员：产品成员或 admin。
func (s *Service) ListMembers(ctx context.Context, actor Actor, productID string) ([]MemberView, error) {
	q := s.DB.Q()
	if _, err := s.mustView(ctx, q, actor, productID); err != nil {
		return nil, err
	}
	rows, err := store.ListMembers(ctx, q, productID)
	if err != nil {
		return nil, err
	}
	out := make([]MemberView, 0, len(rows))
	for _, m := range rows {
		out = append(out, MemberView{
			UserID: m.UserID, Username: m.Username, DisplayName: m.DisplayName,
			Role: string(m.MemberRole), CreatedAt: m.CreatedAt.UTC().Format(time.RFC3339),
		})
	}
	return out, nil
}

// ListMemberCandidates 列「可加入本产品的成员」候选（产品 owner / admin）。
// 控制台添加成员改为下拉选择，避免要求用户手填 usr_ 前缀的 id；
// 判定与 UpsertMember 一致：先过产品可见性（非成员 404），再要 owner / admin。
func (s *Service) ListMemberCandidates(ctx context.Context, actor Actor, productID string) ([]UserView, error) {
	q := s.DB.Q()
	mc, err := s.mustView(ctx, q, actor, productID)
	if err != nil {
		return nil, err
	}
	if err := canManageProduct(mc); err != nil {
		return nil, err
	}
	users, err := store.ListActiveUsersNotInProduct(ctx, q, productID)
	if err != nil {
		return nil, err
	}
	out := make([]UserView, 0, len(users))
	for _, u := range users {
		out = append(out, viewUser(u))
	}
	return out, nil
}

// UpsertMember 加入/调角色：产品 owner 或 admin；须保留至少一个 owner（不变量 17）。
func (s *Service) UpsertMember(ctx context.Context, actor Actor, productID, userID string, role product.MemberRole) error {
	return s.DB.WithTx(ctx, func(q store.Querier) error {
		if _, err := store.ProductByID(ctx, q, productID); err != nil {
			return wrapNotFound(err, "产品不存在或不可见")
		}
		mc, err := s.mustView(ctx, q, actor, productID)
		if err != nil {
			return err
		}
		if err := canManageProduct(mc); err != nil {
			return err
		}
		target, err := store.UserByID(ctx, q, userID)
		if err != nil {
			return wrapNotFound(err, "成员不存在")
		}
		if target.Status != identity.UserActive {
			return Invalid("目标成员已禁用")
		}
		member, err := product.NewMember(productID, userID, role, actor.UserID)
		if err != nil {
			return MapError(err)
		}
		if err := store.UpsertMember(ctx, q, member.ProductID, member.UserID, member.MemberRole, member.CreatedBy); err != nil {
			return err
		}
		return audit(ctx, q, actor, "product.upsert_member", "product", productID,
			map[string]any{"userId": userID, "role": string(role)})
	})
}

// RemoveMember 移除成员：owner/admin；最后一个 owner 不可移；
// 其未终结任务须先转派（不变量 17）。
func (s *Service) RemoveMember(ctx context.Context, actor Actor, productID, userID string) error {
	return s.DB.WithTx(ctx, func(q store.Querier) error {
		mc, err := s.mustView(ctx, q, actor, productID)
		if err != nil {
			return err
		}
		if err := canManageProduct(mc); err != nil {
			return err
		}
		members, err := store.ListMembersLite(ctx, q, productID)
		if err != nil {
			return err
		}
		if err := product.EnsureOwnerRetained(members, userID); err != nil {
			return MapError(err)
		}
		open, err := store.CountOpenTasksByAssigneeInProduct(ctx, q, productID, userID)
		if err != nil {
			return err
		}
		if open > 0 {
			return &Error{Status: 409, Code: "GATE_BLOCKED",
				Message: fmt.Sprintf("成员在产品内仍有 %d 个未终结任务，需先转派（不变量 17）", open)}
		}
		if err := store.RemoveMember(ctx, q, productID, userID); err != nil {
			return wrapNotFound(err, "成员不在产品内")
		}
		return audit(ctx, q, actor, "product.remove_member", "product", productID, map[string]any{"userId": userID})
	})
}

// CreateModule 建模块：产品 owner/member 且持 pm，或 admin；同级名唯一（不变量 1）。
func (s *Service) CreateModule(ctx context.Context, actor Actor, productID, parentID, name string, sortOrder int) (ModuleView, error) {
	var out ModuleView
	err := s.DB.WithTx(ctx, func(q store.Querier) error {
		mc, err := s.mustView(ctx, q, actor, productID)
		if err != nil {
			return err
		}
		if err := canCreateInProduct(mc, actor); err != nil {
			return err
		}
		var parentPath string
		var parentPtr *string
		if parentID != "" {
			parent, err := store.ModuleByID(ctx, q, parentID)
			if err != nil {
				return wrapNotFound(err, "父模块不存在")
			}
			if parent.ProductID != productID {
				return Invalid("父模块与产品不一致")
			}
			parentPath = parent.Path
			p := parentID
			parentPtr = &p
		}
		if err := product.ValidateModuleName(name); err != nil {
			return MapError(err)
		}
		siblings, err := store.SiblingNames(ctx, q, productID, parentPtr)
		if err != nil {
			return err
		}
		for _, s := range siblings {
			if s == name {
				return Invalid("同级已存在模块名 %q", name)
			}
		}
		path, err := product.ChildPath(parentPath, name)
		if err != nil {
			// name 已前置校验，此处失败即深度越限（不变量 1）。
			return TreeDepthExceeded("模块", product.MaxModuleDepth, product.MaxModuleDepth)
		}
		m, err := product.NewModule(s.newID("mod"), productID, parentID, name, sortOrder)
		if err != nil {
			return MapError(err)
		}
		row := store.ModuleRow{ID: m.ID, ProductID: m.ProductID, ParentID: parentPtr, Name: m.Name, Path: path, SortOrder: m.SortOrder}
		if err := store.InsertModule(ctx, q, row); err != nil {
			if store.IsDuplicate(err) {
				return Invalid("同级已存在模块名 %q", name)
			}
			return err
		}
		out = viewModule(row)
		return audit(ctx, q, actor, "module.create", "module", m.ID, map[string]any{"name": name})
	})
	return out, err
}

// UpdateModuleInput 模块更新：改名/移动子树。
type UpdateModuleInput struct {
	Name     *string
	ParentID *string // nil 不动；"" 提升为根；有值换父
}

// UpdateModule 改名/移动子树：整棵子树重算 path（不变量 1；不许移到自身子孙下）。
// in.ParentID：nil 不动；"" 提升为根；其他值换父。
func (s *Service) UpdateModule(ctx context.Context, actor Actor, productID, moduleID string, in UpdateModuleInput) (ModuleView, error) {
	var out ModuleView
	err := s.DB.WithTx(ctx, func(q store.Querier) error {
		mc, err := s.mustView(ctx, q, actor, productID)
		if err != nil {
			return err
		}
		if err := canCreateInProduct(mc, actor); err != nil {
			return err
		}
		m, err := store.ModuleByID(ctx, q, moduleID)
		if err != nil {
			return wrapNotFound(err, "模块不存在")
		}
		if m.ProductID != productID {
			return NotFound("模块不存在或不可见")
		}

		name := m.Name
		if in.Name != nil {
			name = *in.Name
			if err := product.ValidateModuleName(name); err != nil {
				return MapError(err)
			}
		}

		// 解析新父：nil 不动；"" 提根；其他换父（同产品 + 环检测）。
		newParent := m.ParentID
		moving := false
		if in.ParentID != nil {
			moving = true
			switch *in.ParentID {
			case "":
				newParent = nil
			default:
				parent, err := store.ModuleByID(ctx, q, *in.ParentID)
				if err != nil {
					return wrapNotFound(err, "目标父模块不存在")
				}
				if parent.ProductID != productID {
					return Invalid("目标父模块与产品不一致")
				}
				if parent.ID == moduleID || inSubtree(parent.Path, m.Path) {
					return Invalid("不能把模块移动到自身子树下")
				}
				p := *in.ParentID
				newParent = &p
			}
		}
		renaming := in.Name != nil && name != m.Name
		if !moving && !renaming {
			out = viewModule(m)
			return nil // 幂等 no-op
		}

		// 新物化路径。
		newParentPath := ""
		if newParent != nil {
			parent, err := store.ModuleByID(ctx, q, *newParent)
			if err != nil {
				return wrapNotFound(err, "父模块不存在")
			}
			newParentPath = parent.Path
		}
		newPath, err := product.ChildPath(newParentPath, name)
		if err != nil {
			return TreeDepthExceeded("模块", product.MaxModuleDepth, product.MaxModuleDepth)
		}

		// 目标父级同级名唯一（排除自身：自身仍在原父级下，仅换父/改名后进入新位置）。
		siblings, err := store.SiblingNames(ctx, q, productID, newParent)
		if err != nil {
			return err
		}
		for _, sn := range siblings {
			if sn == name && !samePosition(m, newParent) {
				return Invalid("同级已存在模块名 %q", name)
			}
		}

		oldPath := m.Path
		if err := store.MoveModuleSubtree(ctx, q, productID, moduleID, newParent, newPath); err != nil {
			return err
		}
		if newPath != oldPath {
			if err := store.RepathSubtree(ctx, q, productID, oldPath, newPath); err != nil {
				return err
			}
		}
		m.Path = newPath
		m.Name = name
		m.ParentID = newParent
		out = viewModule(m)
		return audit(ctx, q, actor, "module.update", "module", moduleID,
			map[string]any{"path": m.Path, "name": m.Name})
	})
	return out, err
}

// inSubtree 报告 path 是否位于 prefix 子树下（含相等由调用方另行判断）。
func inSubtree(path, prefix string) bool {
	return len(path) > len(prefix) && path[:len(prefix)] == prefix && path[len(prefix)] == '/'
}

func samePosition(m store.ModuleRow, newParent *string) bool {
	if newParent == nil && m.ParentID == nil {
		return true
	}
	if newParent == nil || m.ParentID == nil {
		return false
	}
	return *newParent == *m.ParentID
}

// ListModules 模块树：产品成员或 admin。
func (s *Service) ListModules(ctx context.Context, actor Actor, productID string) ([]ModuleView, error) {
	q := s.DB.Q()
	if _, err := s.mustView(ctx, q, actor, productID); err != nil {
		return nil, err
	}
	rows, err := store.ListModulesByProduct(ctx, q, productID)
	if err != nil {
		return nil, err
	}
	return buildModuleTree(rows), nil
}

// UpsertDocumentInput 文档保存入参。
type UpsertDocumentInput struct {
	OwnerType string // product | iteration | task
	OwnerID   string
	Content   string
	Version   int // 乐观锁：首次传 0
}

// UpsertDocument 保存轻量文档（写权限与所属对象一致）。
func (s *Service) UpsertDocument(ctx context.Context, actor Actor, in UpsertDocumentInput) (DocumentView, error) {
	var out DocumentView
	err := s.DB.WithTx(ctx, func(q store.Querier) error {
		productID, err := s.ownerProductID(ctx, q, in.OwnerType, in.OwnerID)
		if err != nil {
			return err
		}
		mc, err := s.mustView(ctx, q, actor, productID)
		if err != nil {
			return err
		}
		// 写文档按「修改对象字段」权限：owner / pm 成员 / admin。
		switch in.OwnerType {
		case "product":
			if err := canManageProduct(mc); err != nil {
				return err
			}
		case "iteration", "task":
			assignee := ""
			if in.OwnerType == "task" {
				t, err := store.TaskByID(ctx, q, in.OwnerID)
				if err != nil {
					return wrapNotFound(err, "对象不存在或不可见")
				}
				assignee = t.AssigneeID
			}
			if err := canWorkTask(mc, actor, assignee); err != nil {
				return err
			}
		default:
			return Invalid("owner_type 非法")
		}
		version, err := store.UpsertDocument(ctx, q, s.newID("doc"), in.OwnerType, in.OwnerID, in.Content, in.Version)
		if err != nil {
			if err == store.ErrVersionConflict {
				return stateConflict()
			}
			return err
		}
		out = DocumentView{OwnerType: in.OwnerType, OwnerID: in.OwnerID, Content: in.Content, Version: version}
		return audit(ctx, q, actor, "document.upsert", in.OwnerType, in.OwnerID,
			map[string]any{"version": version})
	})
	return out, err
}

// GetDocument 读文档：可见即可读。
func (s *Service) GetDocument(ctx context.Context, actor Actor, ownerType, ownerID string) (DocumentView, error) {
	q := s.DB.Q()
	productID, err := s.ownerProductID(ctx, q, ownerType, ownerID)
	if err != nil {
		return DocumentView{}, err
	}
	if _, err := s.mustView(ctx, q, actor, productID); err != nil {
		return DocumentView{}, err
	}
	doc, err := store.GetDocument(ctx, q, ownerType, ownerID)
	if err != nil {
		return DocumentView{}, wrapNotFound(err, "文档不存在")
	}
	return DocumentView{OwnerType: ownerType, OwnerID: ownerID, Content: doc.Content, Version: doc.Version}, nil
}

// ownerProductID 由文档归属推导产品，同时完成存在性检查。
func (s *Service) ownerProductID(ctx context.Context, q store.Querier, ownerType, ownerID string) (string, error) {
	switch ownerType {
	case "product":
		p, err := store.ProductByID(ctx, q, ownerID)
		if err != nil {
			return "", wrapNotFound(err, "对象不存在或不可见")
		}
		return p.ID, nil
	case "iteration":
		it, err := store.IterationByID(ctx, q, ownerID)
		if err != nil {
			return "", wrapNotFound(err, "对象不存在或不可见")
		}
		return it.ProductID, nil
	case "task":
		t, err := store.TaskByID(ctx, q, ownerID)
		if err != nil {
			return "", wrapNotFound(err, "对象不存在或不可见")
		}
		return t.ProductID, nil
	}
	return "", Invalid("owner_type 非法")
}
