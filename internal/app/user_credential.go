package app

// user_credential.go：成员个人 provider 凭据的用例（docs/domain/integrations.md「个人凭据」）。
//
// 目的：分发成果物 / 流程记录时优先以提交人本人的 Key 出站，外部系统可见
// 「对应员工提交」。未配置时回退迭代级凭据 → 服务端环境变量（公共 Key），
// 并在迭代绑定视图上给出告警提示。
// 安全：Secret 只以 AES-GCM 密文落库（platform.Seal），API 永不回显（红线 8 例外，
// 与 SMTP 密码 / 迭代凭据同源）；审计只记「是否更换」，绝不写 Key 本身。

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/yuylong/Chalkline/internal/domain/workitem"
	"github.com/yuylong/Chalkline/internal/platform"
	"github.com/yuylong/Chalkline/internal/store"
)

// UserCredentialView 是个人凭据视图（只报是否已配置，永不回显）。
type UserCredentialView struct {
	ID            string `json:"id"`
	Provider      string `json:"provider"`
	ProviderLabel string `json:"providerLabel"`
	BaseURL       string `json:"baseUrl,omitempty"` // 空 = 适用于该 provider 的默认地址
	AuthType      string `json:"authType,omitempty"`
	AuthUsername  string `json:"authUsername,omitempty"`
	// SecretConfigured 表示本人已保存 Key（密文）。
	SecretConfigured bool   `json:"secretConfigured"`
	UpdatedAt        string `json:"updatedAt,omitempty"`
}

// UserCredentialInput 是个人凭据保存入参（只写不读）。
type UserCredentialInput struct {
	Provider     string
	BaseURL      string
	AuthType     string
	AuthUsername string
	// AuthSecret 留空 = 保持库内已有密文。
	AuthSecret string
}

// ListMyCredentials 列本人全部个人凭据（登录成员即可，只看自己的）。
func (s *Service) ListMyCredentials(ctx context.Context, actor Actor) ([]UserCredentialView, error) {
	rows, err := store.ListUserProviderCredentials(ctx, s.DB.Q(), actor.UserID)
	if err != nil {
		return nil, err
	}
	out := make([]UserCredentialView, 0, len(rows))
	for _, r := range rows {
		out = append(out, s.userCredentialView(r))
	}
	return out, nil
}

func (s *Service) userCredentialView(r store.UserProviderCredentialRow) UserCredentialView {
	v := UserCredentialView{
		ID: r.ID, Provider: r.Provider, ProviderLabel: s.providerKindLabel(r.Provider),
		BaseURL: r.BaseURL, AuthType: r.AuthType, AuthUsername: r.AuthUsername,
		SecretConfigured: r.SecretEnc != "",
	}
	if !r.UpdatedAt.IsZero() {
		v.UpdatedAt = r.UpdatedAt.UTC().Format(time.RFC3339)
	}
	return v
}

// SetMyCredential 保存（或轮换）本人的一条个人凭据。
// authType 留空 = 清除认证方式（回退公共凭据）；Secret 留空 = 保持库内已有密文。
func (s *Service) SetMyCredential(ctx context.Context, actor Actor, in UserCredentialInput) (UserCredentialView, error) {
	provider := strings.TrimSpace(in.Provider)
	if _, ok := s.providerDefault(provider); !ok {
		return UserCredentialView{}, Invalid("未知 provider %q", provider)
	}
	baseURL, err := normalizeBaseURL(in.BaseURL)
	if err != nil {
		return UserCredentialView{}, err
	}
	auth := workitem.ProviderAuth{
		Type:     workitem.ProviderAuthType(strings.TrimSpace(in.AuthType)),
		Username: strings.TrimSpace(in.AuthUsername),
		Secret:   strings.TrimSpace(in.AuthSecret),
	}
	if err := auth.ValidateForRotation(); err != nil {
		return UserCredentialView{}, Invalid("%s", err.Error())
	}
	enc := ""
	if auth.Secret != "" {
		sealed, serr := platform.Seal(s.SecretKey, auth.Secret)
		if serr != nil {
			return UserCredentialView{}, invalidSecretKey(serr)
		}
		enc = sealed
	}
	row := store.UserProviderCredentialRow{
		ID: s.newID("upc"), UserID: actor.UserID, Provider: provider, BaseURL: baseURL,
		AuthType: string(auth.Type), AuthUsername: auth.Username, SecretEnc: enc,
	}
	err = s.DB.WithTx(ctx, func(q store.Querier) error {
		if err := store.UpsertUserProviderCredential(ctx, q, row); err != nil {
			return err
		}
		// 审计只记方式与「是否更换了密钥」，绝不写 Key 本身。
		return audit(ctx, q, actor, "user.credential.set", "user_credential",
			provider+"|"+baseURL,
			map[string]any{"provider": provider, "baseUrl": baseURL,
				"authType": string(auth.Type), "secretSet": auth.Secret != ""})
	})
	if err != nil {
		return UserCredentialView{}, err
	}
	// 回读组装视图（Upsert 后 updated_at 已刷新）。
	saved, err := store.UserProviderCredential(ctx, s.DB.Q(), actor.UserID, provider, baseURL)
	if err != nil {
		return UserCredentialView{}, err
	}
	return s.userCredentialView(saved), nil
}

// DeleteMyCredential 删除本人的一条个人凭据（回退公共凭据出站）。
func (s *Service) DeleteMyCredential(ctx context.Context, actor Actor, provider, baseURL string) error {
	provider = strings.TrimSpace(provider)
	if _, ok := s.providerDefault(provider); !ok {
		return Invalid("未知 provider %q", provider)
	}
	baseURL, err := normalizeBaseURL(baseURL)
	if err != nil {
		return err
	}
	return s.DB.WithTx(ctx, func(q store.Querier) error {
		if err := store.DeleteUserProviderCredential(ctx, q, actor.UserID, provider, baseURL); err != nil {
			return wrapNotFound(err, "个人凭据不存在")
		}
		return audit(ctx, q, actor, "user.credential.delete", "user_credential",
			provider+"|"+baseURL, map[string]any{"provider": provider, "baseUrl": baseURL})
	})
}

// personalCredentialFor 取提交人在指定 provider + baseURL 上的个人凭据；
// 未配置（或解密失败）时返回 ok=false，由调用方回退公共凭据。
// 解密失败（密钥变更/密文损坏）按「未配置」降级并告警，不阻断流程。
func (s *Service) personalCredentialFor(ctx context.Context, q store.Querier,
	userID string, b workitem.ProviderBinding) (workitem.ProviderAuth, bool) {
	if userID == "" || !b.IsExternal() {
		return workitem.ProviderAuth{}, false
	}
	row, err := store.UserProviderCredential(ctx, q, userID, string(b.Provider), b.BaseURL)
	if err != nil {
		return workitem.ProviderAuth{}, false // 未配置：回退公共凭据
	}
	if row.SecretEnc == "" || row.AuthType == "" {
		return workitem.ProviderAuth{}, false
	}
	plain, err := platform.Open(s.SecretKey, row.SecretEnc)
	if err != nil {
		s.Log.Warn("个人 provider 凭据解密失败，按未配置降级",
			"user", userID, "provider", b.Provider, "error", err)
		return workitem.ProviderAuth{}, false
	}
	return workitem.ProviderAuth{
		Type:     workitem.ProviderAuthType(row.AuthType),
		Username: row.AuthUsername,
		Secret:   plain,
	}, true
}

// personalCredentialConfigured 只报「是否已配置」（不解密），供绑定视图标注告警。
func (s *Service) personalCredentialConfigured(ctx context.Context, q store.Querier,
	userID string, b workitem.ProviderBinding) (bool, error) {
	if userID == "" || !b.IsExternal() {
		return false, nil
	}
	row, err := store.UserProviderCredential(ctx, q, userID, string(b.Provider), b.BaseURL)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return false, nil
		}
		return false, err
	}
	return row.SecretEnc != "" && row.AuthType != "", nil
}
