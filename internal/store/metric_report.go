package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/OboardProject/oboard/internal/model"
)

// SaveMetricReport records an Agent-captured historical sample without changing
// live server state, traffic baselines, last-seen timestamps, or connectivity.
// A report is considered accepted when its fixed UTC-minute slot is inserted or
// already contains an equivalent live or replayed sample.
func (s *Store) SaveMetricReport(ctx context.Context, serverID int64, report model.MetricReport) (bool, error) {
	if serverID <= 0 {
		return false, errors.New("metric report requires a server id")
	}
	slotStart := report.SampledAt.UTC().Truncate(time.Minute)
	slotEnd := slotStart.Add(time.Minute)
	// Prefix bounds include fractional samples in the first second of a minute;
	// RFC3339's trailing Z would sort after the decimal point in those samples.
	startKey := slotStart.Format("2006-01-02T15:04:05")
	endKey := slotEnd.Format("2006-01-02T15:04:05")
	var historyEnabled int
	if err := s.db.QueryRowContext(ctx, `select resource_history_enabled from server_telemetry where server_id=?`, serverID).Scan(&historyEnabled); err != nil {
		return false, err
	}
	cpu, memoryUsed, memoryTotal := report.CPUUsagePercent, report.MemoryUsedBytes, report.MemoryTotalBytes
	diskUsed, diskTotal := report.DiskUsedBytes, report.DiskTotalBytes
	tcpConnections, udpConnections, processes := report.TCPConnectionCount, report.UDPConnectionCount, report.ProcessCount
	if historyEnabled == 0 {
		cpu, memoryUsed, memoryTotal = 0, 0, 0
		diskUsed, diskTotal = 0, 0
		tcpConnections, udpConnections, processes = 0, 0, 0
	}
	history, err := s.historyDB()
	if err != nil {
		return false, err
	}
	result, err := history.ExecContext(ctx, `insert into server_metric_samples(server_id,cpu_usage_percent,memory_used_bytes,memory_total_bytes,disk_used_bytes,disk_total_bytes,tcp_connection_count,udp_connection_count,process_count,resource_recorded,network_upload_bps,network_download_bps,traffic_upload_bytes,traffic_download_bytes,connectivity_available,connectivity_latency_ms,sampled_at)
select ?,?,?,?,?,?,?,?,?,?,?,?,0,0,-1,0,?
where not exists(select 1 from server_metric_samples where server_id=? and sampled_at>=? and sampled_at<?)`,
		serverID, cpu, memoryUsed, memoryTotal, diskUsed, diskTotal, tcpConnections, udpConnections, processes, historyEnabled, report.NetworkUploadBPS, report.NetworkDownloadBPS, report.SampledAt.UTC().Format(time.RFC3339Nano), serverID, startKey, endKey)
	if err != nil {
		return false, err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	if rows == 1 {
		return true, nil
	}
	var equivalent int
	if err := history.QueryRowContext(ctx, `select exists(select 1 from server_metric_samples where server_id=? and sampled_at>=? and sampled_at<?)`, serverID, startKey, endKey).Scan(&equivalent); err != nil {
		return false, err
	}
	if equivalent != 1 {
		return false, sql.ErrNoRows
	}
	return false, nil
}
