package app

// integration.go：provider 连接地址的共用校验。
// 全局 provider 配置只有一张「服务器列表」（provider_endpoints，见 provider_endpoint.go）：
// 没有「默认 git / 默认 svn / 默认 outline / 默认 redmine」的概念，任何选择 provider 的
// 地方都从列表选取或手填地址；凭据仍只从环境变量 / 个人凭据读取，不落目录（红线 8）。

import (
	"net/url"
	"strings"
)

// normalizeBaseURL 校验收紧后的 baseURL：空串表示未填写；
// 非空必须是 http(s) 绝对地址，且不得内嵌凭据（凭据只走环境变量）。
func normalizeBaseURL(raw string) (string, error) {
	v := strings.TrimSpace(raw)
	if v == "" {
		return "", nil
	}
	u, err := url.Parse(v)
	if err != nil || u.Host == "" {
		return "", Invalid("baseUrl 不是合法 URL")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", Invalid("baseUrl 必须使用 http/https")
	}
	if u.User != nil {
		return "", Invalid("baseUrl 不得内嵌凭据（token 只经环境变量配置）")
	}
	return strings.TrimRight(v, "/"), nil
}
