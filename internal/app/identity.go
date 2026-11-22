package app

// identity.go：登录、PAT、成员管理用例（docs/modules/identity.md）。
// 密码与 PAT 明文不落库、不进日志；disabled 用户令牌立即失效。

import (
	"context"
	"time"

	"github.com/yuylong/Chalkline/internal/auth"
	"github.com/yuylong/Chalkline/internal/domain/identity"
	"github.com/yuylong/Chalkline/internal/store"
)

const jwtTTL = 12 * time.Hour

// UserView 是成员对外视图（不含 hash）。
type UserView struct {
	ID          string         `json:"id"`
	Username    string         `json:"username"`
	DisplayName string         `json:"displayName"`
	Email       string         `json:"email"`
	Roles       []string       `json:"roles"`
	Status      string         `json:"status"`
}

func viewUser(u identity.User) UserView {
	return UserView{
		ID: u.ID, Username: u.Username, DisplayName: u.DisplayName, Email: u.Email,
		Roles: store.FromRoles(u.Roles), Status: string(u.Status),
	}
}

// LoginResult 是登录产物：JWT + 用户信息。
type LoginResult struct {
	Token  string   `json:"token"`
	User   UserView `json:"user"`
	Expiry string   `json:"expiry"` // ISO 8601 UTC
}

// Login 用户名密码换 JWT；失败统一 401（不区分用户不存在与密码错误，不暴露存在性）。
func (s *Service) Login(ctx context.Context, username, password string) (LoginResult, error) {
	u, err := store.UserByUsername(ctx, s.DB.q(), username)
	if err != nil {
		return LoginResult{}, Unauthorized("用户名或密码错误")
	}
	ok, err := auth.VerifyPassword(password, u.PasswordHash)
	if err != nil || !ok || u.Status != identity.UserActive {
		return LoginResult{}, Unauthorized("用户名或密码错误")
	}
	expires := s.Now().Add(jwtTTL)
	token, err := s.JWT.Issue(u.ID, u.Username, store.FromRoles(u.Roles), jwtTTL)
	if err != nil {
		return LoginResult{}, err
	}
	return LoginResult{Token: token, User: viewUser(u), Expiry: expires.UTC().Format(time.RFC3339)}, nil
}

// PATView 是 PAT 对外视图（不含 hash 与明文）。
type PATView struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	ExpiresAt string `json:"expiresAt"`
	Revoked   bool   `json:"revoked"`
	CreatedAt string `json:"createdAt"`
}

// PATCreated 签发产物：明文令牌只此一次返回。
type PATCreated struct {
	PATView
	Token string `json:"token"` // chkpat_…，请立即保存
}

// CreatePAT 签发 PAT（默认 90 天）。
func (s *Service) CreatePAT(ctx context.Context, actor Actor, name string, ttlDays int) (PATCreated, error) {
	if ttlDays <= 0 {
		ttlDays = 90
	}
	if ttlDays > 365 {
		return PATCreated{}, Invalid("PAT 有效期最长 365 天")
	}
	if name == "" {
		name = "default"
	}
	token, err := auth.NewPAT()
	if err != nil {
		return PATCreated{}, err
	}
	expires := s.Now().AddDate(0, 0, ttlDays)
	pat := store.PATRow{
		ID: s.newID("pat"), UserID: actor.UserID, Name: name,
		TokenHash: auth.HashPAT(token), ExpiresAt: expires,
	}
	err = s.DB.WithTx(ctx, func(q store.Querier) error {
		if err := store.InsertPAT(ctx, q, pat); err != nil {
			return err
		}
		return audit(ctx, q, actor, "pat.create", "user", actor.UserID, map[string]any{"patId": pat.ID})
	})
	if err != nil {
		return PATCreated{}, err
	}
	return PATCreated{
		PATView: PATView{ID: pat.ID, Name: name, ExpiresAt: expires.UTC().Format(time.RFC3339), CreatedAt: s.Now().UTC().Format(time.RFC3339)},
		Token:   token,
	}, nil
}

// ListPATs 列本人 PAT。
func (s *Service) ListPATs(ctx context.Context, actor Actor) ([]PATView, error) {
	rows, err := store.ListPATsByUser(ctx, s.DB.q(), actor.UserID)
	if err != nil {
		return nil, err
	}
	out := make([]PATView, 0, len(rows))
	for _, p := range rows {
		out = append(out, PATView{
			ID: p.ID, Name: p.Name,
			ExpiresAt: p.ExpiresAt.UTC().Format(time.RFC3339),
			Revoked:   p.RevokedAt != nil,
			CreatedAt: p.CreatedAt.UTC().Format(time.RFC3339),
		})
	}
	return out, nil
}

// RevokePAT 吊销 PAT（仅本人）。
func (s *Service) RevokePAT(ctx context.Context, actor Actor, patID string) error {
	return s.DB.WithTx(ctx, func(q store.Querier) error {
		if err := store.RevokePAT(ctx, q, patID, actor.UserID); err != nil {
			return wrapNotFound(err, "PAT 不存在")
		}
		return audit(ctx, q, actor, "pat.revoke", "user", actor.UserID, map[string]any{"patId": patID})
	})
}

// CreateUserInput 是建成员入参。
type CreateUserInput struct {
	Username    string
	DisplayName string
	Email       string
	Password    string
	Roles       identity.Roles
}

// CreateUser 建成员（admin 专属）。
func (s *Service) CreateUser(ctx context.Context, actor Actor, in CreateUserInput) (UserView, error) {
	hash, err := auth.HashPassword(in.Password)
	if err != nil {
		return UserView{}, err
	}
	u, err := identity.NewUser(s.newID("usr"), in.Username, in.DisplayName, in.Email, hash, in.Roles, identity.UserActive)
	if err != nil {
		return UserView{}, MapError(err)
	}
	err = s.DB.WithTx(ctx, func(q store.Querier) error {
		if err := store.InsertUser(ctx, q, u); err != nil {
			if store.IsDuplicate(err) {
				return Invalid("用户名或邮箱已存在")
			}
			return err
		}
		return audit(ctx, q, actor, "user.create", "user", u.ID,
			map[string]any{"username": u.Username, "roles": store.FromRoles(u.Roles)})
	})
	if err != nil {
		return UserView{}, err
	}
	return viewUser(u), nil
}

// ListUsers 成员列表（admin）。
func (s *Service) ListUsers(ctx context.Context, _ Actor) ([]UserView, error) {
	users, err := store.ListUsers(ctx, s.DB.q())
	if err != nil {
		return nil, err
	}
	out := make([]UserView, 0, len(users))
	for _, u := range users {
		out = append(out, viewUser(u))
	}
	return out, nil
}

// GetUser 成员详情（admin）。
func (s *Service) GetUser(ctx context.Context, _ Actor, id string) (UserView, error) {
	u, err := store.UserByID(ctx, s.DB.q(), id)
	if err != nil {
		return UserView{}, wrapNotFound(err, "成员不存在")
	}
	return viewUser(u), nil
}

// UpdateUser 改角色/启禁用（admin）。角色变更同时影响 JWT/PAT 生效判定（校验时查库）。
func (s *Service) UpdateUser(ctx context.Context, actor Actor, id string, roles identity.Roles, status identity.UserStatus) (UserView, error) {
	if len(roles) == 0 {
		return UserView{}, Invalid("至少需要一个角色")
	}
	for _, r := range roles {
		if !r.Valid() {
			return UserView{}, Invalid("未知角色 %q", r)
		}
	}
	if !status.Valid() {
		return UserView{}, Invalid("状态非法")
	}
	err := s.DB.WithTx(ctx, func(q store.Querier) error {
		if _, err := store.UserByID(ctx, q, id); err != nil {
			return wrapNotFound(err, "成员不存在")
		}
		if err := store.UpdateUser(ctx, q, id, roles, status); err != nil {
			return err
		}
		return audit(ctx, q, actor, "user.update", "user", id,
			map[string]any{"roles": store.FromRoles(roles), "status": string(status)})
	})
	if err != nil {
		return UserView{}, err
	}
	u, err := store.UserByID(ctx, s.DB.q(), id)
	if err != nil {
		return UserView{}, err
	}
	return viewUser(u), nil
}

// SetPassword 重置密码（admin 或本人）。
func (s *Service) SetPassword(ctx context.Context, actor Actor, id, newPassword string) error {
	if !actor.IsAdmin && actor.UserID != id {
		return RoleRequired("仅 admin 或本人可修改密码")
	}
	hash, err := auth.HashPassword(newPassword)
	if err != nil {
		return err
	}
	return s.DB.WithTx(ctx, func(q store.Querier) error {
		if _, err := store.UserByID(ctx, q, id); err != nil {
			return wrapNotFound(err, "成员不存在")
		}
		if err := store.UpdateUserPassword(ctx, q, id, hash); err != nil {
			return err
		}
		return audit(ctx, q, actor, "user.set_password", "user", id, nil)
	})
}

// BootstrapAdmin 首启引导：users 表为空且环境变量提供凭据时创建 admin。
// 已有成员则跳过（幂等）。
func (s *Service) BootstrapAdmin(ctx context.Context, username, password string) (bool, error) {
	if username == "" || password == "" {
		return false, nil
	}
	n, err := store.UserCount(ctx, s.DB.q())
	if err != nil {
		return false, err
	}
	if n > 0 {
		return false, nil
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		return false, err
	}
	u, err := identity.NewUser(s.newID("usr"), username, username, username+"@chalkline.local",
		hash, identity.Roles{identity.RoleAdmin}, identity.UserActive)
	if err != nil {
		return false, MapError(err)
	}
	if err := store.InsertUser(ctx, s.DB.q(), u); err != nil {
		if store.IsDuplicate(err) {
			return false, nil
		}
		return false, err
	}
	s.Log.Info("首启 admin 已创建", "username", username)
	return true, nil
}

// WhoamiView 是 chalkline_whoami 的返回契约：身份 + 可见产品。
type WhoamiView struct {
	User     UserView      `json:"user"`
	Products []ProductLite `json:"products"`
	Via      string        `json:"via"` // jwt | pat
}

// Whoami 返回当前身份与可见产品（可见产品列表来自 catalog，mcp.md whoami）。
func (s *Service) Whoami(ctx context.Context, actor Actor) (WhoamiView, error) {
	u, err := store.UserByID(ctx, s.DB.q(), actor.UserID)
	if err != nil {
		return WhoamiView{}, wrapNotFound(err, "成员不存在")
	}
	products, err := store.ListVisibleProducts(ctx, s.DB.q(), actor.UserID, actor.IsAdmin)
	if err != nil {
		return WhoamiView{}, err
	}
	lite := make([]ProductLite, 0, len(products))
	for _, p := range products {
		lite = append(lite, ProductLite{ID: p.ID, Key: p.Key, Name: p.Name, Status: string(p.Status)})
	}
	via := "jwt"
	if actor.ViaPAT {
		via = "pat"
	}
	return WhoamiView{User: viewUser(u), Products: lite, Via: via}, nil
}
