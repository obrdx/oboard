package controller

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/OboardProject/oboard/internal/backup"
	"github.com/OboardProject/oboard/internal/model"
	"github.com/OboardProject/oboard/internal/security"
	"github.com/OboardProject/oboard/internal/store"
)

// Opt-in real HTTP load, with one persistent counter stream per Agent.
// One-second periods accelerate sampling; this is not a production acceptance run.
func TestTrafficHTTPLoad(t *testing.T) {
	if os.Getenv("OBOARD_TRAFFIC_LOAD") == "" {
		t.Skip("set OBOARD_TRAFFIC_LOAD for local load scenarios")
	}
	scenarios := []struct {
		name    string
		agents  int
		stagger bool
	}{{"burst35", 35, false}, {"phase35", 35, true}, {"phase100", 100, true}, {"interference35", 35, true}}
	for _, sc := range scenarios {
		if filter := os.Getenv("OBOARD_TRAFFIC_SCENARIO"); filter != "" && filter != sc.name {
			continue
		}
		t.Run(sc.name, func(t *testing.T) {
			ctx := context.Background()
			root := t.TempDir()
			db, err := store.Open(filepath.Join(root, "load.sqlite"))
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			servers := make([]*model.Server, sc.agents)
			users := make([]*model.User, sc.agents)
			inbounds := make([]*model.Inbound, sc.agents)
			for i := range servers {
				server := &model.Server{Name: fmt.Sprintf("load-%d", i), AgentID: fmt.Sprintf("load-agent-%d", i), AgentTokenHash: security.HashSecret("load-token"), Status: model.ServerOnline, ListenIP: "0.0.0.0", PortRangeStart: 10000, PortRangeEnd: 10010}
				if err := db.CreateServer(ctx, server); err != nil {
					t.Fatal(err)
				}
				servers[i] = server
				inbound := &model.Inbound{ServerID: server.ID, Name: "entry", Protocol: model.ProtocolVLESS, ListenIP: "0.0.0.0", Port: 10001, Enabled: true, ConfigJSON: "{}"}
				if err := db.CreateInbound(ctx, inbound); err != nil {
					t.Fatal(err)
				}
				inbounds[i] = inbound
				user := &model.User{Username: fmt.Sprintf("load-user-%d", i), PasswordHash: "unused", Role: model.RoleViewer, Status: "active", ProxyUUID: fmt.Sprintf("11111111-1111-4111-8111-%012d", i+1), ProxyPassword: "synthetic"}
				if err := db.CreateUser(ctx, user); err != nil {
					t.Fatal(err)
				}
				users[i] = user
				grantTestPlanInboundNode(t, db, user.ID, inbound.ID)
			}
			if n, _ := strconv.Atoi(os.Getenv("OBOARD_TRAFFIC_SEED_REPORTS")); n > 0 {
				at := time.Now()
				period := model.TrafficPeriod{UserID: users[0].ID, PeriodKey: "load-history", StartedAt: at.AddDate(-1, 0, 0), EndsAt: at.AddDate(-1, 0, 1)}
				for from := 0; from < n; from += 100 {
					reports := []model.TrafficReport{}
					for i := from; i < min(from+100, n); i++ {
						reports = append(reports, model.TrafficReport{ReportID: fmt.Sprintf("seed-%d", i), ServerID: servers[0].ID, UserID: users[0].ID, PeriodKey: period.PeriodKey, CounterSource: "core", StreamID: "seed-stream", CounterEpoch: "seed-epoch", FromUploadBytes: int64(i), ToUploadBytes: int64(i + 1), FromDownloadBytes: int64(i), ToDownloadBytes: int64(i + 1), StartedAt: period.StartedAt, EndedAt: period.EndsAt})
					}
					if _, err := db.CommitTrafficLedger(ctx, store.TrafficLedgerCommit{ServerID: servers[0].ID, Periods: store.TrafficPeriods(period), Reports: reports}); err != nil {
						t.Fatal(err)
					}
				}
			}
			app := newTestServer(db, "load-test-secret", "")
			httpServer := httptest.NewServer(app.Handler())
			defer httpServer.Close()
			client := httpServer.Client()
			defer client.CloseIdleConnections()
			var interferenceToken string
			if sc.name == "interference35" {
				hash, err := security.HashPassword("synthetic-load-password")
				if err != nil {
					t.Fatal(err)
				}
				admin := &model.User{Username: "load-admin", Role: model.RoleAdmin, Status: "active", PasswordHash: hash}
				if err := db.CreateUser(ctx, admin); err != nil {
					t.Fatal(err)
				}
				loginBody, _ := json.Marshal(map[string]any{"username": "load-admin", "password": "synthetic-load-password"})
				login, err := client.Post(httpServer.URL+"/api/v1/ui/auth/login", "application/json", bytes.NewReader(loginBody))
				if err != nil {
					t.Fatal(err)
				}
				var response struct{ Token string }
				decodeErr := json.NewDecoder(login.Body).Decode(&response)
				_ = login.Body.Close()
				if login.StatusCode != http.StatusOK || decodeErr != nil || response.Token == "" {
					t.Fatalf("login status=%d decode=%v", login.StatusCode, decodeErr)
				}
				interferenceToken = response.Token
			}
			var mu sync.Mutex
			latencies := []float64{}
			failures := 0
			var wg sync.WaitGroup
			start := time.Now().Add(100 * time.Millisecond)
			var before, after syscall.Rusage
			_ = syscall.Getrusage(syscall.RUSAGE_SELF, &before)
			for i := range servers {
				wg.Add(1)
				go func(i int) {
					defer wg.Done()
					offset := time.Duration(0)
					if sc.stagger {
						sum := sha256.Sum256([]byte(servers[i].AgentID + "\x00" + strconv.FormatInt(servers[i].ID, 10)))
						offset = time.Duration(binary.BigEndian.Uint64(sum[:8]) % uint64(time.Second/3))
					}
					for round := 0; round < 22; round++ {
						deadline := start.Add(time.Duration(round)*time.Second + offset)
						if wait := time.Until(deadline); wait > 0 {
							time.Sleep(wait)
						}
						body := ledgerTrafficBody(users[i].ID, inbounds[i].ID, fmt.Sprintf("load-%d-%d", i, round), int64(round*1024), int64((round+1)*1024), int64(round*1024), int64((round+1)*1024))
						encoded, _ := json.Marshal(body)
						req, _ := http.NewRequest(http.MethodPost, httpServer.URL+"/api/v1/agent/traffic-reports", bytes.NewReader(encoded))
						req.Header.Set("Content-Type", "application/json")
						req.Header.Set("X-Agent-ID", servers[i].AgentID)
						req.Header.Set("Authorization", "Bearer load-token")
						at := time.Now()
						resp, err := client.Do(req)
						ok := err == nil
						if resp != nil {
							payload, _ := io.ReadAll(resp.Body)
							_ = resp.Body.Close()
							var decoded struct {
								Accepted []struct{ Status string } `json:"accepted_reports"`
							}
							decodeErr := json.Unmarshal(payload, &decoded)
							ok = ok && resp.StatusCode == 200 && decodeErr == nil && len(decoded.Accepted) == 1 && decoded.Accepted[0].Status == "accepted"
						}
						elapsed := time.Since(at)
						mu.Lock()
						if !ok {
							failures++
						}
						if round >= 2 {
							latencies = append(latencies, float64(elapsed)/float64(time.Millisecond))
						}
						mu.Unlock()
					}
				}(i)
			}
			if sc.name == "interference35" {
				wg.Add(1)
				go func() {
					defer wg.Done()
					manager, err := backup.New(backup.Config{Root: filepath.Join(root, "backups"), DatabasePath: filepath.Join(root, "load.sqlite"), HistoryDatabasePath: db.HistoryPath(), MasterSecret: "load-test-secret", SourceVersion: "1.0.0", Snapshot: func(ctx context.Context, path string) error { return db.Backup(ctx, path, store.BackupOptions{}) }, HistorySnapshot: func(ctx context.Context, path string) error {
						return db.BackupHistory(ctx, path, store.BackupOptions{})
					}})
					if err != nil {
						t.Error(err)
						return
					}
					for i := 0; i < 4; i++ {
						if wait := time.Until(start.Add(time.Duration(i*5) * time.Second)); wait > 0 {
							time.Sleep(wait)
						}
						req, _ := http.NewRequest(http.MethodGet, httpServer.URL+"/api/v1/ui/servers", nil)
						req.Header.Set("Authorization", "Bearer "+interferenceToken)
						at := time.Now()
						resp, err := client.Do(req)
						if err != nil {
							t.Error(err)
							return
						}
						_, _ = io.Copy(io.Discard, resp.Body)
						_ = resp.Body.Close()
						t.Logf("interference UI status=%d duration_ms=%.3f", resp.StatusCode, float64(time.Since(at))/float64(time.Millisecond))
						if resp.StatusCode != 200 {
							t.Errorf("UI status=%d", resp.StatusCode)
						}
						if _, err := db.RunMaintenance(ctx, time.Now()); err != nil {
							t.Error(err)
						}
						archive, err := manager.Create(ctx, "synthetic-recovery-password")
						if err != nil {
							t.Error(err)
							return
						}
						_ = os.Remove(archive.Path)
					}
				}()
			}
			wg.Wait()
			_ = syscall.Getrusage(syscall.RUSAGE_SELF, &after)
			sort.Float64s(latencies)
			q := func(p float64) float64 { return latencies[int(float64(len(latencies)-1)*p)] }
			for _, user := range users {
				stored, err := db.GetUser(ctx, user.ID)
				if err != nil || stored.TrafficUsedBytes != 22*2048 {
					t.Fatalf("billing=%+v err=%v", stored, err)
				}
			}
			diag, err := db.GetStorageDiagnostics(ctx)
			if err != nil {
				t.Fatal(err)
			}
			raw, _ := json.Marshal(diag.Performance)
			t.Logf("scenario=%s agents=%d samples=%d window=20s warmup=2s period=1s p50_ms=%.3f p95_ms=%.3f p99_ms=%.3f max_ms=%.3f failures=%d unacked=%d cpu_user_before=%v cpu_user_after=%v cpu_sys_before=%v cpu_sys_after=%v maxrss=%d inblocks=%d outblocks=%d core_bytes=%d history_bytes=%d performance=%s", sc.name, sc.agents, len(latencies), q(.5), q(.95), q(.99), latencies[len(latencies)-1], failures, failures, before.Utime, after.Utime, before.Stime, after.Stime, after.Maxrss, after.Inblock-before.Inblock, after.Oublock-before.Oublock, diag.Core.DBBytes, diag.History.DBBytes, raw)
			if failures != 0 {
				t.Fatalf("%d failed/unacknowledged reports", failures)
			}
		})
	}
}
