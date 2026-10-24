package controller

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/OboardProject/oboard/internal/model"
)

func TestCertificateRenewalRetryWindow(t *testing.T) {
	ctx := context.Background()
	db := openControllerAutomationTestStore(t)
	srv := newTestServer(db, "test-secret", "")
	server := model.Server{Name: "renewal", AgentID: "renewal", Status: model.ServerOnline}
	if err := db.CreateServer(ctx, &server); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	oldAttempt, recentAttempt := now.Add(-13*time.Hour), now.Add(-time.Hour)
	cases := []struct {
		name, status, challenge string
		days                    int
		auto                    bool
		attempt                 *time.Time
		want                    bool
	}{
		{"failed renewal retries", model.CertificateStatusFailed, model.CertificateChallengeHTTP, 10, true, &oldAttempt, true},
		{"failed renewal waits", model.CertificateStatusFailed, model.CertificateChallengeHTTP, 10, true, &recentAttempt, false},
		{"expired renewal retries", model.CertificateStatusFailed, model.CertificateChallengeHTTP, -1, true, &oldAttempt, true},
		{"failed outside window", model.CertificateStatusFailed, model.CertificateChallengeHTTP, 15, true, &oldAttempt, false},
		{"skip cooldown survives restart", model.CertificateStatusReady, model.CertificateChallengeHTTP, 10, true, &recentAttempt, false},
		{"disabled renewal", model.CertificateStatusFailed, model.CertificateChallengeHTTP, 10, false, &oldAttempt, false},
		{"manual DNS", model.CertificateStatusFailed, model.CertificateChallengeDNSManual, 10, true, &oldAttempt, false},
		{"imported", model.CertificateStatusReady, "imported", 10, true, nil, false},
		{"already issuing", model.CertificateStatusIssuing, model.CertificateChallengeHTTP, 10, true, &oldAttempt, false},
		{"waiting DNS", model.CertificateStatusAwaitingDNS, model.CertificateChallengeDNSManual, 10, true, &oldAttempt, false},
	}
	ids := make([]int64, len(cases))
	for i, tc := range cases {
		expiry := now.Add(time.Duration(tc.days) * 24 * time.Hour)
		domain := fmt.Sprintf("renew-%d.example.com", i)
		cert := model.Certificate{Name: tc.name, PrimaryDomain: domain, Domains: []string{domain}, ChallengeType: tc.challenge, IssuanceServerID: &server.ID, ACMECA: "letsencrypt", Status: tc.status, NotAfter: &expiry, AutoRenew: tc.auto, LastRenewalAttemptAt: tc.attempt}
		if err := db.CreateCertificate(ctx, &cert); err != nil {
			t.Fatal(err)
		}
		ids[i] = cert.ID
	}
	// A new Server instance uses persisted attempt times, not in-memory cooldowns.
	srv = newTestServer(db, "test-secret", "")
	srv.renewCertificates(ctx)
	for i, tc := range cases {
		cert, err := db.GetCertificate(ctx, ids[i])
		if err != nil {
			t.Fatal(err)
		}
		wantStatus := tc.status
		if tc.want {
			wantStatus = model.CertificateStatusIssuing
		}
		if cert.Status != wantStatus {
			t.Errorf("%s: status=%s want=%s", tc.name, cert.Status, wantStatus)
		}
		if tc.want && (cert.LastRenewalAttemptAt == nil || cert.LastRenewalAttemptAt.Before(now)) {
			t.Errorf("%s: attempt not recorded", tc.name)
		}
	}
	tasks, err := db.ListTasksByServer(ctx, server.ID, 100)
	if err != nil || len(tasks) != 2 {
		t.Fatalf("tasks=%d err=%v", len(tasks), err)
	}
	srv.renewCertificates(ctx)
	tasks, err = db.ListTasksByServer(ctx, server.ID, 100)
	if err != nil || len(tasks) != 2 {
		t.Fatalf("duplicate renewal tasks=%d err=%v", len(tasks), err)
	}
}

func TestCertificateRenewalSkipRequiresUsableMaterial(t *testing.T) {
	for _, tc := range []struct {
		name    string
		expiry  time.Duration
		missing bool
		failure string
		want    string
	}{
		{"valid existing certificate", 10 * 24 * time.Hour, false, "Skipping. Next renewal time is: 2099-01-01T00:00:00Z", model.CertificateStatusReady},
		{"expired existing certificate", -time.Hour, false, "Skipping. Next renewal time is: 2099-01-01T00:00:00Z", model.CertificateStatusFailed},
		{"missing material", 10 * 24 * time.Hour, true, "Skipping. Next renewal time is: 2099-01-01T00:00:00Z", model.CertificateStatusFailed},
		{"real failure stays visible", 10 * 24 * time.Hour, false, "DNS validation failed", model.CertificateStatusFailed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			db := openControllerAutomationTestStore(t)
			srv := newTestServer(db, "test-secret", "")
			cert := &model.Certificate{Name: tc.name, PrimaryDomain: "renew.example.com", Domains: []string{"renew.example.com"}, ChallengeType: model.CertificateChallengeDNS, ACMECA: "letsencrypt", Status: model.CertificateStatusPending, AutoRenew: true}
			if err := db.CreateCertificate(ctx, cert); err != nil {
				t.Fatal(err)
			}
			if !tc.missing {
				pem, key := testCertificateMaterial(t, cert.Domains, time.Now().Add(tc.expiry))
				if err := srv.storeCertificateMaterial(ctx, cert, pem, pem, key, certificateMaterialPolicy{}); err != nil {
					t.Fatal(err)
				}
			}
			revision := cert.Revision
			now := time.Now().UTC()
			cert.LastRenewalAttemptAt = &now
			cert.Status = model.CertificateStatusIssuing
			srv.markCertificateIssueFailed(ctx, cert, errors.New(tc.failure))
			stored, err := db.GetCertificate(ctx, cert.ID)
			if err != nil {
				t.Fatal(err)
			}
			if stored.Status != tc.want || stored.Revision != revision {
				t.Fatalf("status=%s revision=%s", stored.Status, stored.Revision)
			}
			if tc.want == model.CertificateStatusReady && stored.LastError != "" {
				t.Fatalf("skip reported failure: %s", stored.LastError)
			}
			if tc.want == model.CertificateStatusFailed && !strings.Contains(stored.LastError, tc.failure) {
				t.Fatalf("failure was lost: %s", stored.LastError)
			}
			if stored.LastRenewalAttemptAt == nil || !stored.LastRenewalAttemptAt.Equal(now) {
				t.Fatal("cooldown timestamp lost")
			}
			stored.Status = model.CertificateStatusIssuing
			if err := db.UpdateCertificate(ctx, stored); err != nil {
				t.Fatal(err)
			}
			srv.notifyHTTPCertificateTaskFailure(ctx, model.AgentTask{PayloadJSON: fmt.Sprintf(`{"certificate_id":%d}`, cert.ID)}, tc.failure)
			httpStored, err := db.GetCertificate(ctx, cert.ID)
			if err != nil || httpStored.Status != tc.want {
				t.Fatalf("HTTP failure status=%#v err=%v", httpStored, err)
			}
		})
	}
}
