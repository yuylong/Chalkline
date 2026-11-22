// Package platform 提供横切基础设施：配置、日志、数据库迁移。
// 它可以被所有层使用，但不得反向 import 业务包（见 docs/structure.md 分层边界）。
package platform

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// Config 是 chalklined 的全部启动配置。
// 只从环境变量读取（docker compose 注入），无配置文件（docs/tech.md：环境变量 + .env）。
type Config struct {
	// HTTP 监听地址，默认 ":8080"。
	Addr string
	// 数据库连接串，必填；例如 postgres://chalk:pass@localhost:5432/chalkline?sslmode=disable。
	DatabaseURL string
	// 日志级别：debug | info | warn | error，默认 info。
	LogLevel string
	// 数据库初始化失败后的重试间隔，默认 5s。
	DBRetryInterval time.Duration
	// HTTP 服务器读/写超时，默认 30s / 60s。
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
	// JWT 签名密钥；未设置时进程启动生成临时密钥并告警（重启后会话失效，生产必须设置）。
	JWTSecret string
	// 首启 admin 引导凭据（可选）：users 表为空且二者非空时自动创建 admin。
	AdminUsername string
	AdminPassword string
}

// Load 从环境变量构造配置并校验必填项。
func Load() (Config, error) {
	cfg := Config{
		Addr:            envString("CHALKLINE_ADDR", ":8080"),
		DatabaseURL:     os.Getenv("CHALKLINE_DATABASE_URL"),
		LogLevel:        strings.ToLower(envString("CHALKLINE_LOG_LEVEL", "info")),
		DBRetryInterval: envDuration("CHALKLINE_DB_RETRY_INTERVAL", 5*time.Second),
		ReadTimeout:     envDuration("CHALKLINE_READ_TIMEOUT", 30*time.Second),
		WriteTimeout:    envDuration("CHALKLINE_WRITE_TIMEOUT", 60*time.Second),
		JWTSecret:       os.Getenv("CHALKLINE_JWT_SECRET"),
		AdminUsername:   os.Getenv("CHALKLINE_ADMIN_USERNAME"),
		AdminPassword:   os.Getenv("CHALKLINE_ADMIN_PASSWORD"),
	}
	if cfg.DatabaseURL == "" {
		return Config{}, fmt.Errorf("CHALKLINE_DATABASE_URL 未设置")
	}
	switch cfg.LogLevel {
	case "debug", "info", "warn", "error":
	default:
		return Config{}, fmt.Errorf("CHALKLINE_LOG_LEVEL 非法: %q", cfg.LogLevel)
	}
	return cfg, nil
}

func envString(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envDuration(key string, def time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return def
	}
	return d
}
