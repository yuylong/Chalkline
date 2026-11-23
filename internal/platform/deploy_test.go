package platform

import (
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// 部署栈文件结构校验：compose 必须包含 postgres/api/web/nginx 四个服务
//（docs/structure.md），配套 nginx 配置与入口脚本必须存在。
func TestDeployStackFiles(t *testing.T) {
	data, err := os.ReadFile("../../deploy/docker-compose.yml")
	if err != nil {
		t.Fatalf("读取 compose: %v", err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(data, &doc); err != nil {
		t.Fatalf("compose YAML 解析失败: %v", err)
	}
	services, ok := doc["services"].(map[string]any)
	if !ok {
		t.Fatal("compose 缺少 services")
	}
	for _, name := range []string{"postgres", "api", "web", "nginx"} {
		if _, ok := services[name]; !ok {
			t.Errorf("compose 缺少服务 %s", name)
		}
	}
	if _, ok := doc["volumes"].(map[string]any); !ok {
		t.Fatal("compose 缺少 volumes（pgdata 持久化）")
	}

	for _, path := range []string{
		"../../deploy/Dockerfile",
		"../../deploy/web.Dockerfile",
		"../../deploy/nginx/nginx.conf",
		"../../deploy/nginx/web.conf",
		"../../deploy/nginx/entrypoint.sh",
		"../../deploy/.env.example",
		"../../web/index.html",
	} {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("部署文件缺失: %s: %v", path, err)
		}
	}

	// nginx.conf 必须终止 TLS 并反代 api 与 web。
	nginxConf, err := os.ReadFile("../../deploy/nginx/nginx.conf")
	if err != nil {
		t.Fatalf("读取 nginx.conf: %v", err)
	}
	conf := string(nginxConf)
	for _, fragment := range []string{"listen 443 ssl", "proxy_pass http://api:8080", "proxy_pass http://web:80"} {
		if !strings.Contains(conf, fragment) {
			t.Errorf("nginx.conf 缺少 %q", fragment)
		}
	}

	// web.conf 必须处理 SPA 路由回退（console.md「路由」）。
	webConf, err := os.ReadFile("../../deploy/nginx/web.conf")
	if err != nil {
		t.Fatalf("读取 web.conf: %v", err)
	}
	if !strings.Contains(string(webConf), "try_files $uri $uri/ /index.html") {
		t.Error("web.conf 缺少 SPA try_files 回退")
	}

	// web.Dockerfile 必须构建前端产物并交给 nginx 托管。
	webDockerfile, err := os.ReadFile("../../deploy/web.Dockerfile")
	if err != nil {
		t.Fatalf("读取 web.Dockerfile: %v", err)
	}
	webDF := string(webDockerfile)
	for _, fragment := range []string{"pnpm build", "nginx:1.31-alpine", "/usr/share/nginx/html"} {
		if !strings.Contains(webDF, fragment) {
			t.Errorf("web.Dockerfile 缺少 %q", fragment)
		}
	}
}
