package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// Proxy traffic is the share of a server's traffic that went through user
// proxy connections, counted from accepted traffic reports. The host counters
// (traffic_*_bytes, network_*_bps) keep measuring the whole NIC, which also
// carries speed tests, Agent/Controller sync and anything else on the host.

const (
	proxyRateMinSpan = 5 * time.Second
	proxyRateMaxSpan = 2 * time.Minute
	// A rate older than this is reported as zero: the Agent stops sending
	// traffic reports while nobody is using the server.
	proxyRateStaleAfter = 90 * time.Second
)

// ServerProxyTraffic is a server's proxy counter right after a ledger commit.
type ServerProxyTraffic struct {
	ServerID      int64
	UploadBytes   uint64
	DownloadBytes uint64
	UploadBPS     uint64
	DownloadBPS   uint64
}

var serverProxyTrafficColumns = []struct{ name, sql string }{
	{"proxy_period_key", `alter table server_telemetry add column proxy_period_key text not null default ''`},
	{"proxy_upload_bytes", `alter table server_telemetry add column proxy_upload_bytes integer not null default 0`},
	{"proxy_download_bytes", `alter table server_telemetry add column proxy_download_bytes integer not null default 0`},
	{"proxy_upload_bps", `alter table server_telemetry add column proxy_upload_bps integer not null default 0`},
	{"proxy_download_bps", `alter table server_telemetry add column proxy_download_bps integer not null default 0`},
	{"proxy_reported_at", `alter table server_telemetry add column proxy_reported_at text`},
}

// migrateServerProxyTraffic adds the proxy counters to an older
// server_telemetry table and seeds the current period from traffic_stats, so
// an upgraded panel does not show zero proxy traffic until the next period.
func (s *Store) migrateServerProxyTraffic(ctx context.Context) error {
	var existing int
	if err := s.db.QueryRowContext(ctx, `select count(*) from pragma_table_info('server_telemetry') where name='proxy_period_key'`).Scan(&existing); err != nil {
		return err
	}
	for _, column := range serverProxyTrafficColumns {
		if err := s.ensureColumn(ctx, "server_telemetry", column.name, column.sql); err != nil {
			return err
		}
	}
	if existing > 0 {
		return nil
	}
	_, err := s.db.ExecContext(ctx, `update server_telemetry set
		proxy_period_key = period_key,
		proxy_upload_bytes = coalesce((select sum(t.upload_bytes) from traffic_stats t where t.server_id = server_telemetry.server_id and t.created_at >= server_telemetry.period_start), 0),
		proxy_download_bytes = coalesce((select sum(t.download_bytes) from traffic_stats t where t.server_id = server_telemetry.server_id and t.created_at >= server_telemetry.period_start), 0)
		where period_key <> '' and period_start <> ''`)
	return err
}

// proxyPeriodTraffic hides a counter that still belongs to the previous
// period: the host period rolls over on the next health report, before any
// proxy traffic of the new period has been accepted.
func proxyPeriodTraffic(periodKey, proxyPeriodKey string, up, down uint64) (uint64, uint64) {
	if periodKey == "" || proxyPeriodKey != periodKey {
		return 0, 0
	}
	return up, down
}

func proxyRate(reportedAt *time.Time, up, down uint64, now time.Time) (uint64, uint64) {
	if reportedAt == nil || now.Sub(*reportedAt) > proxyRateStaleAfter {
		return 0, 0
	}
	return up, down
}

// recordServerProxyTrafficTx adds one ledger commit's accepted bytes to the
// server's proxy counters and derives the proxy rate from the time since the
// previous commit. span is the longest accepted report window and is only used
// for the first commit, when there is no previous one.
func recordServerProxyTrafficTx(tx trafficTx, ctx context.Context, serverID, up, down int64, span time.Duration, at time.Time) (*ServerProxyTraffic, error) {
	if up <= 0 && down <= 0 {
		return nil, nil
	}
	var previous sql.NullString
	err := tx.QueryRowContext(ctx, `select proxy_reported_at from server_telemetry where server_id=?`, serverID).Scan(&previous)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if last := parseNullTime(previous); last != nil && at.After(*last) {
		span = at.Sub(*last)
	}
	span = min(max(span, proxyRateMinSpan), proxyRateMaxSpan)
	seconds := span.Seconds()
	upBPS := int64(float64(max(up, 0)) / seconds)
	downBPS := int64(float64(max(down, 0)) / seconds)
	_, err = tx.ExecContext(ctx, `update server_telemetry set
		proxy_upload_bytes = case when proxy_period_key = period_key then proxy_upload_bytes else 0 end + ?,
		proxy_download_bytes = case when proxy_period_key = period_key then proxy_download_bytes else 0 end + ?,
		proxy_period_key = period_key,
		proxy_upload_bps = ?, proxy_download_bps = ?, proxy_reported_at = ?
		where server_id = ?`, max(up, 0), max(down, 0), upBPS, downBPS, at.UTC().Format(time.RFC3339Nano), serverID)
	if err != nil {
		return nil, err
	}
	out := &ServerProxyTraffic{ServerID: serverID, UploadBPS: uint64(upBPS), DownloadBPS: uint64(downBPS)}
	if err := tx.QueryRowContext(ctx, `select proxy_upload_bytes, proxy_download_bytes from server_telemetry where server_id=?`, serverID).Scan(&out.UploadBytes, &out.DownloadBytes); err != nil {
		return nil, err
	}
	return out, nil
}
