package app

// smtpconfig_test.go：邮箱配置的纯函数测试——视图合并（环境变量默认值 +
// 库内覆盖）、入参校验。密码永不回显由视图结构保证（无密码字段）。

import (
	"testing"
	"time"

	"github.com/yuylong/Chalkline/internal/store"
)

func smtpTestService() *Service {
	return &Service{SMTPDefaults: SMTPEnvDefaults{
		Host: "smtp.env.example", Port: 25, Username: "env-user",
		Password: "env-pass", From: "chalk@env.example",
		Recipients: []string{"pm@example.com"},
	}}
}

func TestSMTPViewEnvDefaults(t *testing.T) {
	s := smtpTestService()
	v := s.smtpConfigView(store.SMTPConfigRow{}, false)
	if !v.Enabled {
		t.Fatal("环境变量三要素齐全时默认启用")
	}
	if v.Host != "smtp.env.example" || v.From != "chalk@env.example" || v.Port != 25 {
		t.Fatalf("应展示环境变量默认值: %+v", v)
	}
	if !v.PasswordConfigured {
		t.Fatal("环境变量密码已配置应报 true")
	}
	if v.UpdatedAt != "" || v.UpdatedBy != "" {
		t.Fatal("未保存过不应有更新人/时间")
	}
}

func TestSMTPViewRowOverrides(t *testing.T) {
	s := smtpTestService()
	row := store.SMTPConfigRow{
		Enabled: false, Host: "smtp.console.example", Port: 465,
		Username: "console-user", PasswordEnc: "enc:v1:xxx", FromAddr: "chalk@console.example",
		UpdatedBy: "usr_1", UpdatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	v := s.smtpConfigView(row, true)
	if v.Enabled {
		t.Fatal("库内 enabled 应覆盖默认启用")
	}
	if v.Host != "smtp.console.example" || v.Port != 465 ||
		v.Username != "console-user" || v.From != "chalk@console.example" {
		t.Fatalf("库内非空字段应覆盖环境变量: %+v", v)
	}
	if !v.PasswordConfigured {
		t.Fatal("库内密文存在应报已配置")
	}
	if v.EnvHost != "smtp.env.example" {
		t.Fatal("环境变量默认值应保留为只读提示")
	}
}

func TestSMTPViewRowEmptyFallsBackToEnv(t *testing.T) {
	s := smtpTestService()
	row := store.SMTPConfigRow{Enabled: true, Port: 0} // 全部留空 = 沿用环境变量
	v := s.smtpConfigView(row, true)
	if v.Host != "smtp.env.example" || v.Port != 25 || v.From != "chalk@env.example" {
		t.Fatalf("空字段应回退环境变量: %+v", v)
	}
	if !v.PasswordConfigured {
		t.Fatal("库内无密文但环境变量有密码应报已配置")
	}
}

func TestSMTPViewNoEnvNoRow(t *testing.T) {
	s := &Service{}
	v := s.smtpConfigView(store.SMTPConfigRow{}, false)
	if v.Enabled {
		t.Fatal("环境变量未配置齐全时默认不启用")
	}
	if v.Port != 25 {
		t.Fatalf("端口缺省应为 25，实际 %d", v.Port)
	}
	if v.PasswordConfigured {
		t.Fatal("无任何密码来源应报未配置")
	}
	if v.EnvRecipients == nil {
		t.Fatal("收件人列表应为空切片而非 null")
	}
}

func TestValidateSMTPInput(t *testing.T) {
	if err := validateSMTPInput(SMTPConfigInput{}); err == nil {
		t.Fatal("空入参应报错")
	}
	str := func(v string) *string { return &v }
	num := func(v int) *int { return &v }
	bol := func(v bool) *bool { return &v }

	if err := validateSMTPInput(SMTPConfigInput{Host: str("smtp://x")}); err == nil {
		t.Fatal("带协议的 host 应报错")
	}
	if err := validateSMTPInput(SMTPConfigInput{Host: str("smtp.example.com")}); err != nil {
		t.Fatalf("纯主机名应通过: %v", err)
	}
	if err := validateSMTPInput(SMTPConfigInput{Port: num(65536)}); err == nil {
		t.Fatal("越界端口应报错")
	}
	if err := validateSMTPInput(SMTPConfigInput{Port: num(0)}); err != nil {
		t.Fatalf("0（默认 25）应通过: %v", err)
	}
	if err := validateSMTPInput(SMTPConfigInput{From: str("not-an-email")}); err == nil {
		t.Fatal("非法发件人应报错")
	}
	if err := validateSMTPInput(SMTPConfigInput{From: str("chalk@example.com")}); err != nil {
		t.Fatalf("合法发件人应通过: %v", err)
	}
	if err := validateSMTPInput(SMTPConfigInput{Enabled: bol(true)}); err != nil {
		t.Fatalf("仅改开关应通过: %v", err)
	}
}
