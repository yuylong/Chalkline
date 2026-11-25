package store

// users.go：平台成员与 PAT 查询（docs/modules/identity.md）。
// roles 存 TEXT[]，扫描为 []string 后由调用方转 identity.Roles。

import (
	"context"
	"strings"
	"time"

	"github.com/yuylong/Chalkline/internal/domain/identity"
)

func scanUser(row rowScanner) (identity.User, error) {
	var u identity.User
	var roles []string
	if err := row.Scan(&u.ID, &u.Username, &u.DisplayName, &u.Email, &u.PasswordHash, &roles, &u.Status); err != nil {
		return identity.User{}, err
	}
	u.Roles = ToRoles(roles)
	return u, nil
}

// ToRoles 把 TEXT[] 扫描结果转为全局角色集合。
func ToRoles(ss []string) identity.Roles {
	out := make(identity.Roles, len(ss))
	for i, s := range ss {
		out[i] = identity.Role(s)
	}
	return out
}

// FromRoles 把角色集合转为 TEXT[] 写入参数。
func FromRoles(rs identity.Roles) []string {
	out := make([]string, len(rs))
	for i, r := range rs {
		out[i] = string(r)
	}
	return out
}

const userCols = "id, username, display_name, email, password_hash, roles, status"

// InsertUser 写入成员；用户名/邮箱唯一冲突返回可判定的 duplicate 错误。
func InsertUser(ctx context.Context, q Querier, u identity.User) error {
	_, err := q.Exec(ctx,
		`INSERT INTO users (id, username, display_name, email, password_hash, roles, status)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		u.ID, u.Username, u.DisplayName, u.Email, u.PasswordHash, FromRoles(u.Roles), string(u.Status))
	return err
}

// UserByID 按 id 取成员。
func UserByID(ctx context.Context, q Querier, id string) (identity.User, error) {
	return scanUser(q.QueryRow(ctx, `SELECT `+userCols+` FROM users WHERE id = $1`, id))
}

// UserByUsername 按登录名取成员。
func UserByUsername(ctx context.Context, q Querier, username string) (identity.User, error) {
	return scanUser(q.QueryRow(ctx, `SELECT `+userCols+` FROM users WHERE username = $1`, username))
}

// ListUsers 全量成员列表（admin 端点用）。
func ListUsers(ctx context.Context, q Querier) ([]identity.User, error) {
	return scanAll(ctx, q, `SELECT `+userCols+` FROM users ORDER BY username`, scanUser)
}

// ListActiveUsersNotInProduct 列尚未加入某产品的启用成员。
// 供「添加产品成员」的下拉候选：产品 owner 无需知道 usr_ id，也不能看到全量用户
// （权限在 app 层按 canManageProduct 判定，这里只做受限投影，不读取 password_hash）。
func ListActiveUsersNotInProduct(ctx context.Context, q Querier, productID string) ([]identity.User, error) {
	return scanAll(ctx, q,
		`SELECT u.id, u.username, u.display_name, u.email, u.roles, u.status
		 FROM users u
		 WHERE u.status = $1
		   AND NOT EXISTS (
		     SELECT 1 FROM product_members pm WHERE pm.product_id = $2 AND pm.user_id = u.id)
		 ORDER BY u.username`,
		func(row rowScanner) (identity.User, error) {
			var u identity.User
			var roles []string
			if err := row.Scan(&u.ID, &u.Username, &u.DisplayName, &u.Email, &roles, &u.Status); err != nil {
				return identity.User{}, err
			}
			u.Roles = ToRoles(roles)
			return u, nil
		}, string(identity.UserActive), productID)
}

// EmailsOfUsers 批量取启用成员的邮箱（跳过停用成员与空邮箱，顺序稳定）。
// 邮件通知按「相关用户」投递时用：收件地址取自成员设置里的邮箱。
func EmailsOfUsers(ctx context.Context, q Querier, ids []string) ([]string, error) {
	ids = compactIDs(ids)
	if len(ids) == 0 {
		return nil, nil
	}
	return scanAll(ctx, q,
		`SELECT email FROM users
		 WHERE id = ANY($1) AND status = $2 AND email <> ''
		 ORDER BY id`,
		scanString, ids, string(identity.UserActive))
}

// ListUserIDsWithRole 列持指定全局角色的启用成员 id（评审升级的兜底受众）。
func ListUserIDsWithRole(ctx context.Context, q Querier, role identity.Role) ([]string, error) {
	return scanAll(ctx, q,
		`SELECT id FROM users WHERE status = $1 AND roles && ARRAY[$2]::text[] ORDER BY id`,
		scanString, string(identity.UserActive), string(role))
}

// compactIDs 去空、去重并保持首次出现顺序（同一成员可能命中多个受众来源）。
func compactIDs(ids []string) []string {
	seen := make(map[string]struct{}, len(ids))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

// UpdateUser 更新显示名、邮箱、角色与状态（不变量由 app 校验）。
func UpdateUser(ctx context.Context, q Querier, id, displayName, email string, roles identity.Roles, status identity.UserStatus) error {
	tag, err := q.Exec(ctx,
		`UPDATE users SET display_name = $2, email = $3, roles = $4, status = $5, updated_at = now() WHERE id = $1`,
		id, displayName, email, FromRoles(roles), string(status))
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// UpdateUserPassword 覆写密码 hash。
func UpdateUserPassword(ctx context.Context, q Querier, id, passwordHash string) error {
	tag, err := q.Exec(ctx, `UPDATE users SET password_hash = $2, updated_at = now() WHERE id = $1`, id, passwordHash)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// UserCount 成员总数（首启 bootstrap 判空用）。
func UserCount(ctx context.Context, q Querier) (int64, error) {
	var n int64
	err := q.QueryRow(ctx, `SELECT count(*) FROM users`).Scan(&n)
	return n, err
}

// PATRow 是 PAT 的存储形态。
type PATRow struct {
	ID         string
	UserID     string
	Name       string
	TokenHash  string
	LastUsedAt *time.Time
	ExpiresAt  time.Time
	RevokedAt  *time.Time
	CreatedAt  time.Time
}

// InsertPAT 写入 PAT（只存 hash）。
func InsertPAT(ctx context.Context, q Querier, p PATRow) error {
	_, err := q.Exec(ctx,
		`INSERT INTO personal_access_tokens (id, user_id, name, token_hash, expires_at)
		 VALUES ($1, $2, $3, $4, $5)`,
		p.ID, p.UserID, p.Name, p.TokenHash, p.ExpiresAt)
	return err
}

// ResolvePAT 按 hash 查 PAT。
func ResolvePAT(ctx context.Context, q Querier, tokenHash string) (PATRow, error) {
	var p PATRow
	err := q.QueryRow(ctx,
		`SELECT id, user_id, name, token_hash, last_used_at, expires_at, revoked_at, created_at
		 FROM personal_access_tokens WHERE token_hash = $1`, tokenHash).
		Scan(&p.ID, &p.UserID, &p.Name, &p.TokenHash, &p.LastUsedAt, &p.ExpiresAt, &p.RevokedAt, &p.CreatedAt)
	return p, err
}

// TouchPAT 尽力更新 last_used_at。
func TouchPAT(ctx context.Context, q Querier, tokenHash string) error {
	_, err := q.Exec(ctx, `UPDATE personal_access_tokens SET last_used_at = now() WHERE token_hash = $1`, tokenHash)
	return err
}

// ListPATsByUser 列出本人 PAT（不含 hash）。
func ListPATsByUser(ctx context.Context, q Querier, userID string) ([]PATRow, error) {
	return scanAll(ctx, q,
		`SELECT id, user_id, name, token_hash, last_used_at, expires_at, revoked_at, created_at
		 FROM personal_access_tokens WHERE user_id = $1 ORDER BY created_at DESC`,
		func(row rowScanner) (PATRow, error) {
			var p PATRow
			err := row.Scan(&p.ID, &p.UserID, &p.Name, &p.TokenHash, &p.LastUsedAt, &p.ExpiresAt, &p.RevokedAt, &p.CreatedAt)
			return p, err
		}, userID)
}

// RevokePAT 吊销 PAT；仅本人或 admin 可调（app 层校验）。
func RevokePAT(ctx context.Context, q Querier, id, userID string) error {
	tag, err := q.Exec(ctx,
		`UPDATE personal_access_tokens SET revoked_at = now() WHERE id = $1 AND user_id = $2 AND revoked_at IS NULL`,
		id, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
