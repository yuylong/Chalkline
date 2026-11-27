package app

// identity.go：登录、PAT、成员管理用例（docs/modules/identity.md）。
// 密码与 PAT 明文不落库、不进日志；disabled 用户令牌立即失效。

import (
	"context"
	"net/mail"
	"strings"
	"time"

	"github.com/yuylong/Chalkline/internal/auth"
	"github.com/yuylong/Chalkline/internal/domain/identity"
	"github.com/yuylong/Chalkline/internal/store"
)

const jwtTTL = 12 * time.Hour

// UserView 是成员对外视图（不含 hash）。
type UserView struct {
	ID          string   `json:"id"`
	Username    string   `json:"username"`
	DisplayName string   `json:"displayName"`
	Email       string   `json:"email"`
	Roles       []string `json:"roles"`
	Status      string   `json:"status"`
	AuthSource  string   `json:"authSource"` // local / ldap / both
}

func viewUser(u identity.User) UserView {
	return UserView{
		ID: u.ID, Username: u.Username, DisplayName: u.DisplayName, Email: u.Email,
		Roles: store.FromRoles(u.Roles), Status: string(u.Status), AuthSource: string(u.AuthSource),
	}
}

// LoginResult 是登录产物：JWT + 用户信息。
type LoginResult struct {
	Token  string   `json:"token"`
	User   UserView `json:"user"`
	Expiry string   `json:"expiry"` // ISO 8601 UTC
}

// Login 用户名密码换 JWT；失败统一 401（不区分用户不存在与密码错误，不暴露存在性）。
// 认证按成员 auth_source 分支：local 仅本地 argon2id、ldap 仅 LDAP、
// both 双认证（本地或 LDAP 任一密码通过即可，docs/modules/identity.md）。
func (s *Service) Login(ctx context.Context, username, password string) (LoginResult, error) {
	u, err := store.UserByUsername(ctx, s.DB.Q(), username)
	if err != nil {
		return LoginResult{}, Unauthorized("用户名或密码错误")
	}
	if u.Status != identity.UserActive {
		return LoginResult{}, Unauthorized("用户名或密码错误")
	}
	ok, err := s.checkPassword(ctx, u, password, func() (bool, error) {
		return s.ldapCheck(ctx, u.Username, password)
	})
	if err != nil || !ok {
		return LoginResult{}, Unauthorized("用户名或密码错误")
	}
	expires := s.Now().Add(jwtTTL)
	token, err := s.JWT.Issue(u.ID, u.Username, store.FromRoles(u.Roles), jwtTTL)
	if err != nil {
		return LoginResult{}, err
	}
	return LoginResult{Token: token, User: viewUser(u), Expiry: expires.UTC().Format(time.RFC3339)}, nil
}

// checkPassword 按认证来源校验密码；ldap 是惰性求值的 LDAP 探针
// （local 成员不触发 LDAP 拨号；both 成员本地失败后才尝试 LDAP）。
// LDAP 基础设施错误一律按认证失败处理（不暴露目录状态，仅由探针侧记告警）。
func (s *Service) checkPassword(_ context.Context, u identity.User, password string, ldap func() (bool, error)) (bool, error) {
	if !u.AuthSource.UsesLDAP() { // local（含历史空值）
		return auth.VerifyPassword(password, u.PasswordHash)
	}
	if u.AuthSource == identity.AuthBoth {
		if ok, err := auth.VerifyPassword(password, u.PasswordHash); err == nil && ok {
			return true, nil
		}
	}
	ok, err := ldap()
	if err != nil {
		return false, nil
	}
	return ok, nil
}

// ldapCheck 解析生效 LDAP 配置并校验用户名/密码。
// LDAP 未装配/未启用/不可达一律按认证失败处理（不暴露目录状态），仅记告警日志。
func (s *Service) ldapCheck(ctx context.Context, username, password string) (bool, error) {
	if s.LDAP == nil {
		s.Log.Warn("LDAP 认证通道未装配，LDAP/双认证成员无法经 LDAP 登录")
		return false, nil
	}
	rt, ok, err := s.EffectiveLDAP(ctx)
	if err != nil {
		s.Log.Warn("LDAP 配置读取失败", "error", err)
		return false, nil
	}
	if !ok {
		s.Log.Warn("LDAP 未启用或配置不完整，LDAP/双认证成员登录被拒")
		return false, nil
	}
	passed, aerr := s.LDAP.Authenticate(ctx, rt, username, password)
	if aerr != nil {
		s.Log.Warn("LDAP 认证失败", "error", aerr)
		return false, nil
	}
	return passed, nil
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
	rows, err := store.ListPATsByUser(ctx, s.DB.Q(), actor.UserID)
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
	Password    string // local/both 必填；ldap 可留空（落随机不可匹配 hash）
	Roles       identity.Roles
	AuthSource  identity.AuthSource
}

// CreateUser 建成员（admin 专属）。认证来源决定登录校验路径：
// local/both 必须提供初始密码；ldap 成员本地密码置为随机值（不可匹配）。
func (s *Service) CreateUser(ctx context.Context, actor Actor, in CreateUserInput) (UserView, error) {
	if in.AuthSource == "" {
		in.AuthSource = identity.AuthLocal
	}
	if !in.AuthSource.Valid() {
		return UserView{}, Invalid("认证来源 %q 非法，应为 local / ldap / both", in.AuthSource)
	}
	var hash string
	var err error
	if strings.TrimSpace(in.Password) == "" {
		if !in.AuthSource.UsesLDAP() {
			return UserView{}, Invalid("本地/双认证成员必须设置初始密码")
		}
		// LDAP 成员：本地 hash 落随机值，杜绝空密码被本地路径匹配。
		if hash, err = auth.HashPassword(s.newID("ldap")); err != nil {
			return UserView{}, err
		}
	} else if hash, err = auth.HashPassword(in.Password); err != nil {
		return UserView{}, err
	}
	u, err := identity.NewUser(s.newID("usr"), in.Username, in.DisplayName, in.Email, hash, in.Roles, identity.UserActive)
	if err != nil {
		return UserView{}, MapError(err)
	}
	u.AuthSource = in.AuthSource
	err = s.DB.WithTx(ctx, func(q store.Querier) error {
		if err := store.InsertUser(ctx, q, u); err != nil {
			if store.IsDuplicate(err) {
				return Invalid("用户名或邮箱已存在")
			}
			return err
		}
		return audit(ctx, q, actor, "user.create", "user", u.ID,
			map[string]any{"username": u.Username, "roles": store.FromRoles(u.Roles),
				"auth_source": string(u.AuthSource)})
	})
	if err != nil {
		return UserView{}, err
	}
	return viewUser(u), nil
}

// ListUsers 成员列表（admin）。
func (s *Service) ListUsers(ctx context.Context, _ Actor) ([]UserView, error) {
	users, err := store.ListUsers(ctx, s.DB.Q())
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
	u, err := store.UserByID(ctx, s.DB.Q(), id)
	if err != nil {
		return UserView{}, wrapNotFound(err, "成员不存在")
	}
	return viewUser(u), nil
}

// UpdateUserInput 是改成员入参：显示名/邮箱/角色/状态/认证来源。
type UpdateUserInput struct {
	DisplayName string
	Email       string
	Roles       identity.Roles
	Status      identity.UserStatus
	AuthSource  identity.AuthSource
}

// UpdateUser 改显示名/邮箱/角色/启禁用/认证来源（admin）。角色变更同时影响
// JWT/PAT 生效判定（校验时查库）；认证来源变更即时影响登录路径。
func (s *Service) UpdateUser(ctx context.Context, actor Actor, id string, in UpdateUserInput) (UserView, error) {
	in.DisplayName = strings.TrimSpace(in.DisplayName)
	if in.DisplayName == "" {
		return UserView{}, Invalid("显示名不能为空")
	}
	if _, err := mail.ParseAddress(in.Email); err != nil {
		return UserView{}, Invalid("邮箱 %q 非法", in.Email)
	}
	if len(in.Roles) == 0 {
		return UserView{}, Invalid("至少需要一个角色")
	}
	for _, r := range in.Roles {
		if !r.Valid() {
			return UserView{}, Invalid("未知角色 %q", r)
		}
	}
	if !in.Status.Valid() {
		return UserView{}, Invalid("状态非法")
	}
	if in.AuthSource == "" {
		in.AuthSource = identity.AuthLocal
	}
	if !in.AuthSource.Valid() {
		return UserView{}, Invalid("认证来源 %q 非法，应为 local / ldap / both", in.AuthSource)
	}
	err := s.DB.WithTx(ctx, func(q store.Querier) error {
		if _, err := store.UserByID(ctx, q, id); err != nil {
			return wrapNotFound(err, "成员不存在")
		}
		if err := store.UpdateUser(ctx, q, id, in.DisplayName, in.Email, in.Roles, in.Status, in.AuthSource); err != nil {
			if store.IsDuplicate(err) {
				return Invalid("邮箱已存在")
			}
			return err
		}
		return audit(ctx, q, actor, "user.update", "user", id,
			map[string]any{"displayName": in.DisplayName, "email": in.Email,
				"roles": store.FromRoles(in.Roles), "status": string(in.Status),
				"auth_source": string(in.AuthSource)})
	})
	if err != nil {
		return UserView{}, err
	}
	u, err := store.UserByID(ctx, s.DB.Q(), id)
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
	n, err := store.UserCount(ctx, s.DB.Q())
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
	if err := store.InsertUser(ctx, s.DB.Q(), u); err != nil {
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
	u, err := store.UserByID(ctx, s.DB.Q(), actor.UserID)
	if err != nil {
		return WhoamiView{}, wrapNotFound(err, "成员不存在")
	}
	products, err := store.ListVisibleProducts(ctx, s.DB.Q(), actor.UserID, actor.IsAdmin)
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
