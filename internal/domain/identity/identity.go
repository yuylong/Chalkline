// Package identity 承载平台成员：全局角色、用户实体与其不变量。
// 对应 docs/domain/model.md 的 User 一节。
package identity

import (
	"errors"
	"fmt"
	"net/mail"
	"regexp"
	"strings"
)

// Role 是全局角色，描述成员「会什么」（docs/domain/model.md）。
// 产品内角色（owner/member/guest）见 domain/product 包，二者共同决定权限。
type Role string

const (
	RoleAdmin    Role = "admin"    // 成员管理、产品管理、所有评审权
	RolePM       Role = "pm"       // 建产品/模块/迭代/任务、指派、发起评审
	RoleReviewer Role = "reviewer" // 在门禁处确认或驳回
	RoleDev      Role = "dev"      // 推进被指派的任务、上传材料
	RoleQA       Role = "qa"       // 测试设计、执行与报告
)

var roleSet = map[Role]bool{
	RoleAdmin: true, RolePM: true, RoleReviewer: true, RoleDev: true, RoleQA: true,
}

// Valid 报告角色是否为已定义值。禁止用 string 裸传角色（docs/tech.md）。
func (r Role) Valid() bool { return roleSet[r] }

// Roles 是角色集合，可多选。
type Roles []Role

// ParseRoles 解析逗号分隔的角色串，任一非法即报错。
func ParseRoles(s string) (Roles, error) {
	if strings.TrimSpace(s) == "" {
		return nil, errors.New("角色列表为空")
	}
	parts := strings.Split(s, ",")
	out := make(Roles, 0, len(parts))
	for _, p := range parts {
		r := Role(strings.TrimSpace(p))
		if !r.Valid() {
			return nil, fmt.Errorf("未知角色 %q", p)
		}
		out = append(out, r)
	}
	return out, nil
}

// HasAny 报告集合与参数是否存在交集；admin 恒有权（docs/domain/model.md 对象级权限）。
func (rs Roles) HasAny(need ...Role) bool {
	for _, own := range rs {
		if own == RoleAdmin {
			return true
		}
		for _, n := range need {
			if own == n {
				return true
			}
		}
	}
	return false
}

// UserStatus 是成员状态。
type UserStatus string

const (
	UserActive   UserStatus = "active"
	UserDisabled UserStatus = "disabled"
)

func (s UserStatus) Valid() bool { return s == UserActive || s == UserDisabled }

// AuthSource 是成员的认证来源（docs/modules/identity.md「LDAP 认证」）。
// local：仅本地 argon2id 密码；ldap：仅 LDAP 服务器；both：双认证，
// 本地或 LDAP 任一密码通过即可登录。
type AuthSource string

const (
	AuthLocal AuthSource = "local"
	AuthLDAP  AuthSource = "ldap"
	AuthBoth  AuthSource = "both"
)

var authSourceSet = map[AuthSource]bool{
	AuthLocal: true, AuthLDAP: true, AuthBoth: true,
}

// Valid 报告认证来源是否为已定义值。
func (a AuthSource) Valid() bool { return authSourceSet[a] }

// UsesLDAP 报告该认证来源是否参与 LDAP 校验（ldap 与 both）。
func (a AuthSource) UsesLDAP() bool { return a == AuthLDAP || a == AuthBoth }

// User 是平台成员。单部署 = 一个团队，用户角色为全局。
type User struct {
	ID           string
	Username     string // 唯一登录名
	DisplayName  string
	Email        string // 唯一
	PasswordHash string // argon2id，由 internal/auth 负责哈希，本包只存不解析
	Roles        Roles
	Status       UserStatus
	AuthSource   AuthSource // 空值按 local 处理（历史行默认 local）
}

var usernameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{1,31}$`)

// ErrValidation 表示实体违反不变量，message 可直接展示。
var ErrValidation = errors.New("identity: 实体校验失败")

// NewUser 构造并校验成员。password_hash 由 auth 层生成后传入；
// authSource 空值归一化为 local。
func NewUser(id, username, displayName, email, passwordHash string, roles Roles, status UserStatus) (User, error) {
	u := User{ID: id, Username: username, DisplayName: displayName, Email: email,
		PasswordHash: passwordHash, Roles: roles, Status: status}
	if u.AuthSource == "" {
		u.AuthSource = AuthLocal
	}
	if err := u.Validate(); err != nil {
		return User{}, err
	}
	return u, nil
}

// Validate 检查所有字段级不变量。
func (u User) Validate() error {
	if u.ID == "" {
		return fmt.Errorf("%w: id 为空", ErrValidation)
	}
	if !usernameRe.MatchString(u.Username) {
		return fmt.Errorf("%w: username %q 需为 2-32 位小写字母数字及 -_，且以字母数字开头", ErrValidation, u.Username)
	}
	if strings.TrimSpace(u.DisplayName) == "" {
		return fmt.Errorf("%w: display_name 为空", ErrValidation)
	}
	if _, err := mail.ParseAddress(u.Email); err != nil {
		return fmt.Errorf("%w: email %q 非法", ErrValidation, u.Email)
	}
	if u.PasswordHash == "" {
		return fmt.Errorf("%w: password_hash 为空", ErrValidation)
	}
	if len(u.Roles) == 0 {
		return fmt.Errorf("%w: 至少需要一个角色", ErrValidation)
	}
	for _, r := range u.Roles {
		if !r.Valid() {
			return fmt.Errorf("%w: 未知角色 %q", ErrValidation, r)
		}
	}
	if !u.Status.Valid() {
		return fmt.Errorf("%w: 状态非法 %q", ErrValidation, u.Status)
	}
	if u.AuthSource == "" {
		u.AuthSource = AuthLocal // 历史行与直接构造的零值归一化
	}
	if !u.AuthSource.Valid() {
		return fmt.Errorf("%w: 认证来源非法 %q", ErrValidation, u.AuthSource)
	}
	return nil
}
