package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/OboardProject/oboard/internal/model"
	_ "modernc.org/sqlite"
)

// TestSwapMetricColumnsAddedToPreviousSchema opens a core and history database
// that predate swap telemetry and verifies the swap columns are added with zero
// defaults, existing rows survive, new swap values round-trip and reopening is
// idempotent.
func TestSwapMetricColumnsAddedToPreviousSchema(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "previous.sqlite")
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	previousServers := `create table servers (id integer primary key autoincrement, name text not null, agent_id text unique, agent_token_hash text, chain_secret text not null, enrollment_hash text, enrollment_expires_at text, entry_address text, public_ipv4 text not null default '', public_ipv6 text not null default '', region_code text not null default '', detected_region_code text not null default '', region_mode text not null default 'auto', entry_ip_mode text not null default 'auto', listen_ip text, ip_stack text not null default 'auto', udp_inbound_mode text not null default 'allow', mtu_mode text not null default 'detect', mtu_value integer not null default 0, mtu_probe_host text not null default '1.1.1.1', mtu_probe_port integer not null default 443, mtu_overhead_bytes integer not null default 0, stealth_enabled integer not null default 0, bbr_qdisc text not null default '', bbr_cake_bandwidth_mbit integer not null default 0, port_range_start integer not null default 10000, port_range_end integer not null default 20000, internal_port_range_start integer not null default 30000, internal_port_range_end integer not null default 59999, port_policy_revision integer not null default 1, status text not null, os text, distro_id text not null default '', distro_version text not null default '', distro_name text not null default '', libc text not null default '', service_manager text not null default '', package_manager text not null default '', arch text, kernel text, cpu text, cpu_cores integer not null default 0, memory_bytes integer not null default 0, cpu_usage_percent real not null default 0, memory_used_bytes integer not null default 0, memory_total_bytes integer not null default 0, agent_memory_bytes integer not null default 0, disk_bytes integer not null default 0, disk_total_bytes integer not null default 0, tcp_connection_count integer not null default 0, udp_connection_count integer not null default 0, process_count integer not null default 0, agent_version text not null default '', agent_build text not null default '', sing_box_version text, kernel_capabilities_json text not null default '[]', tcp_fastopen_state text not null default '', tcp_fastopen_value integer not null default 0, host_booted_at text, connection_audit_enabled integer not null default 0, display_tags_json text not null default '[]', last_seen_at text, created_at text not null, updated_at text not null)`
	if _, err := raw.ExecContext(ctx, previousServers); err != nil {
		t.Fatal(err)
	}
	if _, err := raw.ExecContext(ctx, `insert into servers(name,chain_secret,status,entry_address,listen_ip,os,kernel,cpu,arch,sing_box_version,memory_used_bytes,memory_total_bytes,created_at,updated_at) values('pre-swap','secret','online','203.0.113.10','0.0.0.0','linux','6.1.0','x86_64','amd64','1.14.0',512,1024,'2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}
	history, err := sql.Open("sqlite", HistoryPathFor(path))
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range historySchemaStatements {
		previous := statement
		for _, line := range []string{
			"\n\t\tswap_used_bytes integer not null default 0,",
			"\n\t\tswap_total_bytes integer not null default 0,",
			"\n\t\tswap_used_avg integer not null default 0,",
			"\n\t\tswap_total_avg integer not null default 0,",
		} {
			previous = strings.ReplaceAll(previous, line, "")
		}
		if strings.Contains(previous, "swap_") {
			t.Fatalf("previous history schema still contains swap: %s", previous)
		}
		if _, err := history.ExecContext(ctx, previous); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := history.ExecContext(ctx, `insert into server_metric_samples(server_id,cpu_usage_percent,memory_used_bytes,memory_total_bytes,sampled_at) values(1,5,100,1000,'2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if err := history.Close(); err != nil {
		t.Fatal(err)
	}

	for attempt := 0; attempt < 2; attempt++ {
		db, err := Open(path)
		if err != nil {
			t.Fatalf("open attempt %d: %v", attempt, err)
		}
		for _, column := range []struct{ table, name string }{
			{"server_metric_samples", "swap_used_bytes"}, {"server_metric_samples", "swap_total_bytes"},
			{"server_metric_hourly", "swap_used_avg"}, {"server_metric_hourly", "swap_total_avg"},
			{"server_metric_daily", "swap_used_avg"}, {"server_metric_daily", "swap_total_avg"},
		} {
			var count int
			if err := db.history.QueryRowContext(ctx, `select count(*) from pragma_table_info(?) where name=?`, column.table, column.name).Scan(&count); err != nil || count != 1 {
				t.Fatalf("attempt %d: %s.%s count=%d err=%v", attempt, column.table, column.name, count, err)
			}
		}
		var legacySwap uint64
		if err := db.history.QueryRowContext(ctx, `select swap_used_bytes+swap_total_bytes from server_metric_samples where sampled_at='2026-01-01T00:00:00Z'`).Scan(&legacySwap); err != nil || legacySwap != 0 {
			t.Fatalf("attempt %d: legacy sample swap=%d err=%v", attempt, legacySwap, err)
		}
		servers, err := db.ListServers(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(servers) != 1 || servers[0].Name != "pre-swap" || servers[0].MemoryUsedBytes != 512 {
			t.Fatalf("attempt %d: migrated row lost data: %+v", attempt, servers)
		}
		server := servers[0]
		if attempt == 0 {
			if server.SwapUsedBytes != 0 || server.SwapTotalBytes != 0 {
				t.Fatalf("existing server swap = %d/%d", server.SwapUsedBytes, server.SwapTotalBytes)
			}
			server.SwapUsedBytes, server.SwapTotalBytes = 64, 256
			seen := time.Now().UTC()
			server.LastSeenAt = &seen
			if err := db.UpdateServerRuntimeState(ctx, &server); err != nil {
				t.Fatal(err)
			}
		} else if server.SwapUsedBytes != 64 || server.SwapTotalBytes != 256 {
			t.Fatalf("swap did not survive reopening: %d/%d", server.SwapUsedBytes, server.SwapTotalBytes)
		}
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestSwapMetricsFlowIntoResourcePointsAndRollups(t *testing.T) {
	ctx := context.Background()
	db, err := Open(filepath.Join(t.TempDir(), "swap.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	server := &model.Server{Name: "swap-metrics", AgentID: "agent-swap", Status: model.ServerUnknown}
	if err := db.CreateServer(ctx, server); err != nil {
		t.Fatal(err)
	}
	hour := time.Now().UTC().Truncate(time.Hour).Add(-2 * time.Hour)
	for i, used := range []uint64{100, 300} {
		report := model.MetricReport{ReportID: "swap", SampledAt: hour.Add(time.Duration(i+1) * time.Minute), MemoryUsedBytes: 10, MemoryTotalBytes: 20, SwapUsedBytes: used, SwapTotalBytes: 1000}
		if inserted, err := db.SaveMetricReport(ctx, server.ID, report); err != nil || !inserted {
			t.Fatalf("save inserted=%v err=%v", inserted, err)
		}
	}
	points, err := db.ListServerResourceMetricPoints(ctx, server.ID, hour, time.Minute)
	if err != nil || len(points) != 2 || points[0].SwapUsedBytes != 100 || points[1].SwapUsedBytes != 300 || points[1].SwapTotalBytes != 1000 {
		t.Fatalf("raw swap points=%+v err=%v", points, err)
	}
	if _, err := db.BuildMetricHourly(ctx, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if _, err := db.BuildMetricDaily(ctx, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"server_metric_hourly", "server_metric_daily"} {
		bucket := hour.Format(time.RFC3339Nano)
		if table == "server_metric_daily" {
			bucket = hour.Format("2006-01-02")
		}
		var usedAvg, totalAvg uint64
		if err := db.history.QueryRowContext(ctx, `select swap_used_avg,swap_total_avg from `+table+` where server_id=? and bucket_start=?`, server.ID, bucket).Scan(&usedAvg, &totalAvg); err != nil {
			t.Fatal(err)
		}
		if usedAvg != 200 || totalAvg != 1000 {
			t.Fatalf("%s swap avg = %d/%d", table, usedAvg, totalAvg)
		}
	}
	aggregated, err := queryAggregatedMetricPoints(ctx, db.history, "server_metric_hourly", server.ID, hour, hour.Add(time.Hour))
	if err != nil || len(aggregated) != 1 || aggregated[0].SwapUsedBytes != 200 || aggregated[0].SwapTotalBytes != 1000 {
		t.Fatalf("aggregated swap points=%+v err=%v", aggregated, err)
	}
}
