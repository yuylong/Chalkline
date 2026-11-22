package store

// users.go：平台成员与 PAT 查询（docs/modules/identity.md）。
// roles 存 TEXT[]，扫描为 []string 后由调用方转 identity.Roles。

import (
	"context"
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

// UpdateUser 更新角色与状态（重置角色须至少保留一个合法角色，由 app 校验）。
func UpdateUser(ctx context.Context, q Querier, id string, roles identity.Roles, status identity.UserStatus) error {
	tag, err := q.Exec(ctx, `UPDATE users SET roles = $2, status = $3, updated_at = now() WHERE id = $1`,
		id, FromRoles(roles), string(status))
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
