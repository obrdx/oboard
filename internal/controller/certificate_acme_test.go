package controller

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/OboardProject/oboard/internal/model"
)

func TestRunACMESerializesSharedHomeAndRetriesReusedNonce(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "order")
	countPath := filepath.Join(dir, "count")
	script := filepath.Join(dir, "acme.sh")
	body := fmt.Sprintf(`#!/bin/sh
printf 'start\n' >> %q
sleep 0.05
printf 'end\n' >> %q
n=0
if [ -f %q ]; then n=$(cat %q); fi
n=$((n + 1))
printf '%%s\n' "$n" > %q
if [ $((n %% 3)) -ne 0 ]; then
  printf 'urn:ietf:params:acme:error:badNonce: This nonce has already been used.\n'
  exit 1
fi
printf 'Cert success.\n'
exit 0
`, logPath, logPath, countPath, countPath, countPath)
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	previous := acmeNonceRetryDelay
	acmeNonceRetryDelay = 0
	t.Cleanup(func() { acmeNonceRetryDelay = previous })

	db := openControllerAutomationTestStore(t)
	srv := newTestServer(db, "test-secret", "")
	srv.acmeCommand = script
	srv.acmeHome = filepath.Join(dir, "home")

	var wg sync.WaitGroup
	errCh := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			output, err := srv.runACME(context.Background())
			if err != nil || !strings.Contains(output, "Cert success.") {
				errCh <- fmt.Errorf("output=%q err=%v", output, err)
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Error(err)
	}
	order, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(order) != strings.Repeat("start\nend\n", 6) {
		t.Fatalf("acme processes overlapped:\n%s", order)
	}
	count, err := os.ReadFile(countPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(count)) != "6" {
		t.Fatalf("nonce retries=%q want 6", strings.TrimSpace(string(count)))
	}
}

func TestRenewalArchivesExpiredUnusedInboundCertificates(t *testing.T) {
	ctx := context.Background()
	db := openControllerAutomationTestStore(t)
	srv := newTestServer(db, "test-secret", "")
	server := model.Server{Name: "archive", AgentID: "archive-agent", Status: model.ServerOnline}
	if err := db.CreateServer(ctx, &server); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	soon := now.Add(10 * 24 * time.Hour)
	expired := now.Add(-time.Hour)
	makeCert := func(name, domain, origin string, expiry time.Time) model.Certificate {
		t.Helper()
		certificate := model.Certificate{Name: name, PrimaryDomain: domain, Domains: []string{domain}, ChallengeType: model.CertificateChallengeHTTP, IssuanceServerID: &server.ID, ACMECA: "letsencrypt", Status: model.CertificateStatusReady, NotAfter: &expiry, AutoRenew: true, IssuanceOrigin: origin}
		if err := db.CreateCertificate(ctx, &certificate); err != nil {
			t.Fatal(err)
		}
		return certificate
	}
	unusedExpired := makeCert("自动证书 gone.example.com", "gone.example.com", model.CertificateOriginInbound, expired)
	unusedValid := makeCert("自动证书 later.example.com", "later.example.com", model.CertificateOriginInbound, soon)
	manualExpired := makeCert("operator", "manual.example.com", model.CertificateOriginManual, expired)
	used := makeCert("自动证书 live.example.com", "live.example.com", model.CertificateOriginInbound, soon)
	disabled := makeCert("自动证书 paused.example.com", "paused.example.com", model.CertificateOriginInbound, expired)

	inbound := model.Inbound{ServerID: server.ID, Name: "live", Protocol: model.ProtocolAnyTLS, ListenIP: "0.0.0.0", Port: 443, TLS: true, DNSDomain: "live.example.com", Enabled: true}
	if err := db.CreateInbound(ctx, &inbound); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertInboundCertificateBinding(ctx, &model.InboundCertificateBinding{InboundID: inbound.ID, CertificateID: &used.ID, Mode: model.CertificateModeAuto, ServerName: "live.example.com"}); err != nil {
		t.Fatal(err)
	}
	paused := model.Inbound{ServerID: server.ID, Name: "paused", Protocol: model.ProtocolAnyTLS, ListenIP: "0.0.0.0", Port: 8443, TLS: true, DNSDomain: "paused.example.com", Enabled: false}
	if err := db.CreateInbound(ctx, &paused); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertInboundCertificateBinding(ctx, &model.InboundCertificateBinding{InboundID: paused.ID, CertificateID: &disabled.ID, Mode: model.CertificateModeExplicit, ServerName: "paused.example.com"}); err != nil {
		t.Fatal(err)
	}

	srv.renewCertificates(ctx)

	assertStatus := func(id int64, status string, renew bool) {
		t.Helper()
		certificate, err := db.GetCertificate(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if certificate.Status != status || certificate.AutoRenew != renew {
			t.Fatalf("%s: status=%s auto_renew=%v", certificate.PrimaryDomain, certificate.Status, certificate.AutoRenew)
		}
	}
	assertStatus(unusedExpired.ID, model.CertificateStatusArchived, false)
	assertStatus(unusedValid.ID, model.CertificateStatusReady, true)
	assertStatus(manualExpired.ID, model.CertificateStatusIssuing, true)
	assertStatus(used.ID, model.CertificateStatusIssuing, true)
	assertStatus(disabled.ID, model.CertificateStatusIssuing, true)
}

func TestFailedNonceCertificateRetriesOnPrepare(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	countPath := filepath.Join(dir, "count")
	script := filepath.Join(dir, "acme.sh")
	body := fmt.Sprintf(`#!/bin/sh
n=0
if [ -f %q ]; then n=$(cat %q); fi
n=$((n + 1))
printf '%%s\n' "$n" > %q
printf 'badNonce: This nonce has already been used.\n'
exit 1
`, countPath, countPath, countPath)
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	previous := acmeNonceRetryDelay
	acmeNonceRetryDelay = 0
	t.Cleanup(func() { acmeNonceRetryDelay = previous })

	db := openControllerAutomationTestStore(t)
	srv := newTestServer(db, "test-secret", "")
	srv.acmeCommand = script
	srv.acmeHome = filepath.Join(dir, "home")
	dns := model.DNSCredential{Name: "nonce", Provider: model.DNSProviderCloudflare, ZoneName: "example.com", ConfigEncrypted: "secret", Enabled: true}
	if err := db.CreateDNSCredential(ctx, &dns); err != nil {
		t.Fatal(err)
	}
	credential := dns.ID
	recent := time.Now().UTC().Add(-time.Minute)
	old := time.Now().UTC().Add(-11 * time.Minute)
	recentCert := &model.Certificate{Name: model.AutomaticCertificateNamePrefix + "recent.example.com", PrimaryDomain: "recent.example.com", Domains: []string{"recent.example.com"}, ChallengeType: model.CertificateChallengeDNS, DNSCredentialID: &credential, ACMECA: "letsencrypt", Status: model.CertificateStatusFailed, AutoRenew: true, IssuanceOrigin: model.CertificateOriginInbound, LastError: "badNonce: This nonce has already been used.", LastRenewalAttemptAt: &recent}
	oldCert := &model.Certificate{Name: model.AutomaticCertificateNamePrefix + "old.example.com", PrimaryDomain: "old.example.com", Domains: []string{"old.example.com"}, ChallengeType: model.CertificateChallengeDNS, DNSCredentialID: &credential, ACMECA: "letsencrypt", Status: model.CertificateStatusFailed, AutoRenew: true, IssuanceOrigin: model.CertificateOriginInbound, LastError: "badNonce: This nonce has already been used.", LastRenewalAttemptAt: &old, NotAfter: &old}
	for _, certificate := range []*model.Certificate{recentCert, oldCert} {
		if err := db.CreateCertificate(ctx, certificate); err != nil {
			t.Fatal(err)
		}
	}
	inbound := model.Inbound{ServerID: 1, DNSCredentialID: &credential, CertificateMode: model.CertificateModeExact}
	settings := map[string]string{}
	if err := srv.ensureManagedCertificateIssue(ctx, inbound, model.CertificateModeExact, "recent.example.com", []model.Certificate{*recentCert}, settings); err == nil || !strings.Contains(err.Error(), "badNonce") {
		t.Fatalf("recent failure was retried: %v", err)
	}
	if _, err := os.Stat(countPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("recent failure started acme.sh")
	}
	err := srv.ensureManagedCertificateIssue(ctx, inbound, model.CertificateModeExact, "old.example.com", []model.Certificate{*oldCert}, settings)
	if err == nil || !errors.Is(err, errCertificateProvisioning) {
		t.Fatalf("old failure did not start: %v", err)
	}
	waitForCertificateStatus(t, db, oldCert.ID, model.CertificateStatusFailed)
	count, readErr := os.ReadFile(countPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if strings.TrimSpace(string(count)) != "3" {
		t.Fatalf("retries=%q want 3", strings.TrimSpace(string(count)))
	}
}

func TestDNSManualResumeDropsPreviousOrderLink(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	home := filepath.Join(dir, "home")
	domain := "*.example.com"
	confDir := filepath.Join(home, domain+"_ecc")
	if err := os.MkdirAll(confDir, 0o700); err != nil {
		t.Fatal(err)
	}
	confPath := filepath.Join(confDir, domain+".conf")
	previous := "Le_Domain='*.example.com'\nLe_OrderFinalize='https://ca.test/order/new/finalize'\nLe_LinkOrder='https://ca.test/order/old'\nLe_Keylength='ec-256'\n"
	if err := os.WriteFile(confPath, []byte(previous), 0o600); err != nil {
		t.Fatal(err)
	}
	seenPath := filepath.Join(dir, "seen")
	script := filepath.Join(dir, "acme.sh")
	body := fmt.Sprintf(`#!/bin/sh
for arg in "$@"; do
  if [ "$arg" = "--renew" ]; then
    cat %q > %q
  fi
done
printf 'Signing error: wrong status\n'
exit 1
`, confPath, seenPath)
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	db := openControllerAutomationTestStore(t)
	srv := newTestServer(db, "test-secret", "")
	srv.acmeCommand = script
	srv.acmeHome = home
	certificate := &model.Certificate{PrimaryDomain: domain, Domains: []string{domain}, ChallengeType: model.CertificateChallengeDNSManual, ACMECA: "google"}
	if err := srv.runDNSCertificateIssue(ctx, certificate, true, true); err == nil {
		t.Fatal("expected failing fake acme.sh")
	}
	seen, err := os.ReadFile(seenPath)
	if err != nil {
		t.Fatal(err)
	}
	want := "Le_Domain='*.example.com'\nLe_OrderFinalize='https://ca.test/order/new/finalize'\nLe_Keylength='ec-256'\n"
	if string(seen) != want {
		t.Fatalf("resume saw config:\n%s\nwant:\n%s", seen, want)
	}
	info, err := os.Stat(confPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("config mode=%v want 0600", info.Mode().Perm())
	}
}
