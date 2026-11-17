package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/OboardProject/oboard/internal/model"
	_ "modernc.org/sqlite"
)

func TestCertificateIssuanceOriginUpgradesPreviousSchema(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "previous.sqlite")
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	previous := `create table certificates (id integer primary key autoincrement, name text not null, primary_domain text not null, domains_json text not null default '[]', wildcard integer not null default 0, challenge_type text not null, dns_credential_id integer references dns_credentials(id) on delete set null, issuance_server_id integer references servers(id) on delete set null, acme_ca text not null default 'letsencrypt', account_email text not null default '', google_eab_credential_id integer references google_eab_credentials(id) on delete restrict, eab_key_id text not null default '', eab_hmac_key_encrypted text not null default '', status text not null default 'pending', certificate_pem text not null default '', fullchain_pem text not null default '', private_key_encrypted text not null default '', revision text not null default '', not_before text, not_after text, auto_renew integer not null default 1, validation_records_json text not null default '[]', last_error text not null default '', last_issued_at text, last_renewal_attempt_at text, created_at text not null, updated_at text not null)`
	if _, err := raw.ExecContext(ctx, previous); err != nil {
		t.Fatal(err)
	}
	insert := `insert into certificates(name,primary_domain,domains_json,challenge_type,status,auto_renew,created_at,updated_at) values(?,?,?,?, 'ready',1,'2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')`
	rows := []struct {
		name, domain string
	}{
		{model.AutomaticCertificateNamePrefix + "old.example.com", "old.example.com"},
		{"operator", "manual.example.com"},
		{model.AutomaticCertificateNamePrefix + "renamed.example.com", "other.example.com"},
	}
	for _, row := range rows {
		if _, err := raw.ExecContext(ctx, insert, row.name, row.domain, `["`+row.domain+`"]`, model.CertificateChallengeDNS); err != nil {
			t.Fatal(err)
		}
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}

	for attempt := 0; attempt < 2; attempt++ {
		db, err := Open(path)
		if err != nil {
			t.Fatalf("open attempt %d: %v", attempt, err)
		}
		certificates, err := db.ListCertificates(ctx)
		if err != nil {
			t.Fatal(err)
		}
		wantCount := 3
		if attempt == 1 {
			wantCount = 4
		}
		if len(certificates) != wantCount {
			t.Fatalf("attempt %d: certificates=%d", attempt, len(certificates))
		}
		got := map[string]string{}
		for _, certificate := range certificates {
			got[certificate.PrimaryDomain] = certificate.IssuanceOrigin
		}
		if got["old.example.com"] != model.CertificateOriginInbound || got["manual.example.com"] != model.CertificateOriginManual || got["other.example.com"] != model.CertificateOriginManual {
			t.Fatalf("attempt %d: origins=%v", attempt, got)
		}
		if attempt == 1 && got["fresh.example.com"] != model.CertificateOriginInbound {
			t.Fatalf("fresh origin=%s", got["fresh.example.com"])
		}
		if attempt == 0 {
			created := &model.Certificate{Name: "fresh", PrimaryDomain: "fresh.example.com", Domains: []string{"fresh.example.com"}, ChallengeType: model.CertificateChallengeDNS, ACMECA: "letsencrypt", Status: model.CertificateStatusPending, IssuanceOrigin: model.CertificateOriginInbound}
			if err := db.CreateCertificate(ctx, created); err != nil {
				t.Fatal(err)
			}
			if created.IssuanceOrigin != model.CertificateOriginInbound {
				t.Fatalf("stored origin=%s", created.IssuanceOrigin)
			}
		}
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}
	}
}
