package controller

import "strings"

func certificateFailureSummary(detail string) string {
	text := strings.ToLower(detail)
	has := func(parts ...string) bool {
		for _, part := range parts {
			if strings.Contains(text, part) {
				return true
			}
		}
		return false
	}
	switch {
	case has("skipping. next renewal time is:"):
		return "尚未到 ACME 续期时间，本次操作已跳过。请到续期时间后重试；这不代表现有证书已失效。"
	case has("externalaccountrequired", "invalid external account", "eab 信息无法读取", "eab credentials", "invalid hmac", "eab key id"):
		return "签发机构的外部账号绑定异常，请检查 EAB Key ID 与 HMAC Key。"
	case has("ratelimited", "rate limit", "too many requests", "too many certificates", "too many failed authorizations"):
		return "已触发签发机构限流，请按日志中的等待时间重试。"
	case has("urn:ietf:params:acme:error:caa", "caa record", "caa policy", "caa forbids"):
		return "域名的 CAA 记录不允许当前签发机构签发，请检查 CAA 配置。"
	case has("dns credential", "invalid api token", "authentication error", "invalid api key", "invalid access key", "accessdenied"):
		return "DNS 凭据不可用或权限不足，请检查凭据及域名授权。"
	case has("nxdomain", "no such host"):
		return "DNS 名称无法解析，请检查域名、权威 DNS 和解析记录。"
	case has("servfail"):
		return "DNS 查询失败，请检查权威 DNS 与 DNSSEC 配置后重试。"
	case has("incorrect txt", "no txt record", "dns validation failed", "dns problem", "txt record is not correct"):
		return "DNS 验证未通过，请确认 TXT 验证记录正确且已生效。"
	case has("connection refused"):
		return "连接被拒绝，请检查目标服务、端口和防火墙。"
	case has("timeout", "timed out", "deadline exceeded"):
		return "请求或验证超时，请检查网络与验证记录后重试。"
	case has("http-01 验证未通过", "invalid response from", "http-01 validation failed"):
		return "HTTP-01 验证未通过，请检查域名解析、80 端口和验证路径。"
	case has("urn:ietf:params:acme:error:unauthorized", "authorization failed", "challenge failed"):
		return "域名验证未通过，请查看日志确认验证方式及失败原因。"
	case has("certificate has expired", "x509:", "tls handshake"):
		return "TLS 连接验证失败，请检查系统时间、信任证书和网络。"
	case has("permission denied", "read-only file system"):
		return "无法读写签发文件，请检查目录权限和文件系统状态。"
	case has("no space left on device"):
		return "磁盘空间不足，请清理空间后重试。"
	case has("context canceled", "context cancelled"):
		return "签发操作已中断，请确认服务状态后重试。"
	default:
		return "暂未识别具体原因，请查看证书签发日志后处理。"
	}
}

func telegramCertificateLogText(detail string) string {
	const header = "本次证书签发日志（已脱敏）\n"
	const truncated = "受消息长度限制，仅显示日志末尾。\n"
	runes := []rune(strings.TrimSpace(detail))
	budget := telegramActionTextLimit - len([]rune(header))
	if len(runes) > budget {
		budget -= len([]rune(truncated))
		return header + truncated + string(runes[len(runes)-budget:])
	}
	return header + string(runes)
}
