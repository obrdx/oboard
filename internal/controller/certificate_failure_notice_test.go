package controller

import (
	"context"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/OboardProject/oboard/internal/model"
)

func TestCertificateFailureSummary(t *testing.T) {
	cases := []struct{ log, want string }{
		{"complete ACME DNS challenge: Renewing using Le_API=https://example.com\nARI suggestedWindow: tomorrow\nSkipping. Next renewal time is: 2026-10-24T06:14:53Z\nAdd '--force' to force renewal.", "本次操作已跳过"},
		{"externalAccountRequired: secret", "外部账号绑定异常"},
		{"urn:ietf:params:acme:error:rateLimited", "限流"},
		{"urn:ietf:params:acme:error:caa", "CAA"},
		{"DNS credential is unavailable or not verified", "DNS 凭据"},
		{"NXDOMAIN looking up TXT", "无法解析"},
		{"SERVFAIL looking up TXT", "DNS 查询失败"},
		{"Incorrect TXT record found", "TXT 验证记录"},
		{"DNS validation failed", "DNS 验证未通过"},
		{"connect: connection refused", "连接被拒绝"},
		{"context deadline exceeded", "超时"},
		{"HTTP-01 验证未通过", "80 端口"},
		{"urn:ietf:params:acme:error:unauthorized", "域名验证未通过"},
		{"x509: certificate signed by unknown authority", "TLS"},
		{"permission denied", "目录权限"},
		{"no space left on device", "磁盘空间不足"},
		{"context canceled", "已中断"},
		{"unexpected error: private-value", "暂未识别"},
		{"account eab001 registered successfully\nunknown failure", "暂未识别"},
		{"", "暂未识别"},
	}
	for _, tc := range cases {
		got := certificateFailureSummary(tc.log)
		if !strings.Contains(got, tc.want) || strings.Contains(got, "private-value") || strings.Contains(got, "\n") {
			t.Errorf("summary(%q) = %q, want %q", tc.log, got, tc.want)
		}
	}
}

func TestTelegramCertificateLogSnapshot(t *testing.T) {
	ctx := context.Background()
	db := openControllerAutomationTestStore(t)
	srv := newTestServer(db, "test-secret", "")
	admin := &model.User{Username: "admin", PasswordHash: "unused", Role: model.RoleAdmin, Status: "active", ProxyUUID: "11111111-1111-4111-8111-111111111112", ProxyPassword: "unused"}
	if err := db.CreateUser(ctx, admin); err != nil {
		t.Fatal(err)
	}
	principal := userAutomationPrincipal(t, db, admin.ID)
	specs := telegramNotificationActionSpecs(telegramActionInput{Event: notificationCertificateFailed, Notice: "简洁通知", Context: notificationContext{CertificateID: 3, FailureLog: "本次失败记录"}, CertificateIssueAllowed: true})
	if len(specs) != 2 || specs[0].Action != "view_certificate_log" || specs[0].Label != "查看详细日志" {
		t.Fatalf("specs = %#v", specs)
	}
	payload := specs[0].Payload
	display, err := srv.executeTelegramAction(ctx, *admin, model.RoleAdmin, principal, payload, "")
	if err != nil || !strings.Contains(display, "本次失败记录") {
		t.Fatalf("display = %q, err = %v", display, err)
	}
	if _, err := srv.executeTelegramAction(ctx, *admin, model.RoleViewer, principal, payload, ""); err == nil {
		t.Fatal("non-admin could read log")
	}
	srv.telegramMarkupEdit = func(_ context.Context, _, _ int64, text, markup string) error {
		if text != display || !strings.Contains(markup, "收起日志") {
			t.Fatalf("expanded message = %q %q", text, markup)
		}
		return nil
	}
	if err := srv.presentTelegramActionMessage(ctx, "unused", 1, 1, payload, display); err != nil {
		t.Fatal(err)
	}
	payload.Action = "hide_certificate_log"
	folded, err := srv.executeTelegramAction(ctx, *admin, model.RoleAdmin, principal, payload, "")
	if err != nil || folded != "简洁通知" {
		t.Fatalf("folded = %q, err = %v", folded, err)
	}
	long := telegramCertificateLogText(strings.Repeat("日志", 4000) + "最后原因")
	if !utf8.ValidString(long) || len([]rune(long)) > telegramActionTextLimit || !strings.HasSuffix(long, "最后原因") || !strings.Contains(long, "仅显示日志末尾") {
		t.Fatalf("bad log truncation: %d", len([]rune(long)))
	}
}
